package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/storage"
)

// LendingBot interface 用於避免循環依賴
type LendingBot interface {
	GetActiveLendingCredits() ([]*bitfinex.FundingCredit, error)
	CheckRateThreshold() (bool, float64, error)
}

// Bot Telegram 機器人封裝
type Bot struct {
	api                 *tgbotapi.BotAPI
	config              *config.Config
	bitfinexClient      *bitfinex.Client
	rateConverter       *rates.Converter
	authenticatedChatID int64
	chatIDMutex         sync.Mutex
	dataFilePath        string
	restartCallback     func() error // 重啟回調函數
	lendingBot          LendingBot   // 借貸機器人引用
}

// NewBot 創建新的 Telegram 機器人
func NewBot(cfg *config.Config, bfxClient *bitfinex.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	log.Printf("Authorized on account %s", api.Self.UserName)

	bot := &Bot{
		api:            api,
		config:         cfg,
		bitfinexClient: bfxClient,
		rateConverter:  rates.NewConverter(),
		dataFilePath:   storage.DefaultDataFilePath(),
	}
	bot.loadAuthenticatedChatID()
	return bot, nil
}

// Start 啟動 Telegram 機器人
func (b *Bot) Start() {
	// 創建一個永不取消的 context
	ctx := context.Background()
	b.StartWithContext(ctx)
}

// StartWithContext 啟動支持 context 的 Telegram 機器人
func (b *Bot) StartWithContext(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Telegram 機器人收到停止信號")
			return
		default:
		}

		u := tgbotapi.NewUpdate(0)
		u.Timeout = int(constants.TelegramUpdateTimeout.Seconds())

		updates, err := b.api.GetUpdatesChan(u)
		if err != nil {
			log.Printf("Failed to get updates, retrying in %v: %v", constants.TelegramRetryDelay, err)

			// 使用 context 支持的 sleep
			select {
			case <-ctx.Done():
				log.Println("Telegram 機器人在重試等待中收到停止信號")
				return
			case <-time.After(constants.TelegramRetryDelay):
				continue
			}
		}

		// 處理更新，直到 channel 關閉或 context 取消
		for {
			select {
			case <-ctx.Done():
				log.Println("Telegram 機器人在處理更新時收到停止信號")
				return
			case update, ok := <-updates:
				if !ok {
					log.Printf("Update channel closed, retrying in %v...", constants.TelegramRetryDelay)
					goto retry
				}

				if update.Message == nil {
					continue
				}

				go b.handleMessage(update.Message)
			}
		}

	retry:
		// 使用 context 支持的重試延遲
		select {
		case <-ctx.Done():
			log.Println("Telegram 機器人在重試前收到停止信號")
			return
		case <-time.After(constants.TelegramRetryDelay):
			continue
		}
	}
}

// handleMessage 處理 Telegram 訊息
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	chatID := message.Chat.ID
	text := message.Text

	// 處理身份驗證
	if !b.isAuthenticated(chatID) {
		b.handleAuthentication(chatID, text)
		return
	}

	// 處理已驗證用戶的指令
	b.handleCommand(chatID, text)
}

// isAuthenticated 檢查是否已驗證
func (b *Bot) isAuthenticated(chatID int64) bool {
	b.chatIDMutex.Lock()
	defer b.chatIDMutex.Unlock()
	return b.authenticatedChatID == chatID
}

// setAuthenticated 設置已驗證的聊天ID
func (b *Bot) setAuthenticated(chatID int64) {
	b.chatIDMutex.Lock()
	defer b.chatIDMutex.Unlock()
	b.authenticatedChatID = chatID
	b.saveAuthenticatedChatIDLocked()
}

// getAuthenticatedChatID 獲取已驗證的聊天ID
func (b *Bot) GetAuthenticatedChatID() int64 {
	b.chatIDMutex.Lock()
	defer b.chatIDMutex.Unlock()
	return b.authenticatedChatID
}

func (b *Bot) loadAuthenticatedChatID() {
	if b.dataFilePath == "" {
		return
	}
	state := storage.LoadData(b.dataFilePath)
	if state.Telegram.AuthenticatedChatID == 0 {
		return
	}

	b.chatIDMutex.Lock()
	defer b.chatIDMutex.Unlock()
	b.authenticatedChatID = state.Telegram.AuthenticatedChatID
}

func (b *Bot) saveAuthenticatedChatIDLocked() {
	if b.dataFilePath == "" {
		return
	}
	state := storage.LoadData(b.dataFilePath)
	state.Telegram.AuthenticatedChatID = b.authenticatedChatID
	storage.SaveData(b.dataFilePath, state)
}

// sendMessage 發送訊息
func (b *Bot) sendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
}

// SendNotification 發送通知（公開方法供外部調用）
func (b *Bot) SendNotification(message string) error {
	chatID := b.GetAuthenticatedChatID()
	if chatID == 0 {
		return fmt.Errorf("no authenticated chat ID")
	}
	return b.sendMessage(chatID, message)
}

// SetRestartCallback 設置重啟回調函數
func (b *Bot) SetRestartCallback(callback func() error) {
	b.restartCallback = callback
}

// SetLendingBot 設置借貸機器人引用
func (b *Bot) SetLendingBot(lendingBot LendingBot) {
	b.lendingBot = lendingBot
}

// handleAuthentication 處理身份驗證
func (b *Bot) handleAuthentication(chatID int64, text string) {
	switch text {
	case "/auth":
		b.sendMessage(chatID, "請輸入驗證 token：")
	case b.config.TelegramAuthToken:
		b.setAuthenticated(chatID)
		b.sendMessage(chatID, "驗證成功，現在可以傳送指令了")
	default:
		b.sendMessage(chatID, "請先進行驗證，輸入 /auth 開始驗證流程")
	}
}

// handleCommand 處理指令
func (b *Bot) handleCommand(chatID int64, text string) {
	switch {
	case text == "/help" || text == "/start":
		b.handleHelp(chatID)
	case text == "/restart":
		b.handleRestart(chatID)
	case text == "/rate":
		b.handleRate(chatID)
	case text == "/check":
		b.handleCheck(chatID)
	case text == "/status":
		b.handleStatus(chatID)
	case strings.HasPrefix(text, "/threshold "):
		b.handleSetThreshold(chatID, text)
	case strings.HasPrefix(text, "/reserve "):
		b.handleSetReserve(chatID, text)
	case strings.HasPrefix(text, "/orderlimit "):
		b.handleSetOrderLimit(chatID, text)
	case strings.HasPrefix(text, "/loandays "):
		b.handleSetLoanDays(chatID, text)
	case strings.HasPrefix(text, "/mindailylendrate "):
		b.handleSetMinDailyRate(chatID, text)
	case strings.HasPrefix(text, "/minloan "):
		b.handleSetMinLoan(chatID, text)
	case strings.HasPrefix(text, "/maxloan "):
		b.handleSetMaxLoan(chatID, text)
	case strings.HasPrefix(text, "/highholdrate "):
		b.handleSetHighHoldRate(chatID, text)
	case strings.HasPrefix(text, "/highholdamount "):
		b.handleSetHighHoldAmount(chatID, text)
	case strings.HasPrefix(text, "/highholdorders "):
		b.handleSetHighHoldOrders(chatID, text)
	case strings.HasPrefix(text, "/raterangeincrease "):
		b.handleSetRateRangeIncrease(chatID, text)
	case text == "/strategy":
		b.handleStrategyStatus(chatID)
	case text == "/smartstrategy on":
		b.handleToggleSmartStrategy(chatID, true)
	case text == "/smartstrategy off":
		b.handleToggleSmartStrategy(chatID, false)
	case text == "/klinestrategy on":
		b.handleToggleKlineStrategy(chatID, true)
	case text == "/klinestrategy off":
		b.handleToggleKlineStrategy(chatID, false)
	case strings.HasPrefix(text, "/smoothmethod "):
		b.handleSetSmoothMethod(chatID, text)
	case text == "/lending":
		b.handleLendingCredits(chatID)
	default:
		b.sendMessage(chatID, "無效的指令，輸入 /help 查看所有可用指令")
	}
}

// handleHelp 處理幫助指令
func (b *Bot) handleHelp(chatID int64) {
	helpText := `可用指令:

📊 查詢指令:
/rate - 顯示當前貸出利率和閾值
/check - 檢查貸出利率是否超過閾值
/status - 顯示系統狀態
/strategy - 顯示當前策略狀態
/lending - 查看當前活躍的借貸訂單

⚙️ 設置指令:
/threshold [數值] - 設置利率通知閾值
/reserve [數值] - 設置不參與借貸的保留金額
/orderlimit [數值] - 設置單次執行最大下單數量限制
/loandays [數值] - 設置固定借貸天數 (設為0使用自動判斷)
/mindailylendrate [數值|FRR] - 設置最低每日貸出利率（FRR 為浮動利率模式）
/minloan [數值] - 設置單筆最小貸出金額
/maxloan [數值] - 設置單筆最大貸出金額 (設為0無限制)
/highholdrate [數值] - 設置高額持有策略的日利率
/highholdamount [數值] - 設置高額持有策略的金額 (設為0關閉)
/highholdorders [數值] - 設置高額持有策略的訂單數量
/raterangeincrease [數值] - 設置利率範圍增加百分比 (0-100%)

🧠 策略指令:
/klinestrategy on - 啟用K線策略 (最高優先級)
/klinestrategy off - 停用K線策略
/smartstrategy on - 啟用智能策略 (中等優先級)
/smartstrategy off - 停用智能策略
/smoothmethod [方法] - 設置K線利率平滑方法 (max/sma/ema/hla/p90)

🔄 控制指令:
/restart - 手動重新啟動，清除所有訂單，重新運行
/help - 顯示此幫助訊息

💡 策略優先級: K線策略 > 智能策略 > 傳統策略`

	b.sendMessage(chatID, helpText)
}
