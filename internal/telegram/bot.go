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

// LendingBot interface 用于避免循环依赖
type LendingBot interface {
	GetActiveLendingCredits() ([]*bitfinex.FundingCredit, error)
	CheckRateThreshold() (bool, float64, error)
}

// Bot Telegram 机器人封装
type Bot struct {
	api                 *tgbotapi.BotAPI
	config              *config.Config
	bitfinexClient      *bitfinex.Client
	rateConverter       *rates.Converter
	authenticatedChatID int64
	chatIDMutex         sync.Mutex
	dataFilePath        string
	restartCallback     func() error // 重启回调函数
	lendingBot          LendingBot   // 借贷机器人引用
}

// NewBot 创建新的 Telegram 机器人
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

// Start 启动 Telegram 机器人
func (b *Bot) Start() {
	// 创建一个永不取消的 context
	ctx := context.Background()
	b.StartWithContext(ctx)
}

// StartWithContext 启动支持 context 的 Telegram 机器人
func (b *Bot) StartWithContext(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Telegram 机器人收到停止信号")
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
				log.Println("Telegram 机器人在重试等待中收到停止信号")
				return
			case <-time.After(constants.TelegramRetryDelay):
				continue
			}
		}

		// 处理更新，直到 channel 关闭或 context 取消
		for {
			select {
			case <-ctx.Done():
				log.Println("Telegram 机器人在处理更新时收到停止信号")
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
		// 使用 context 支持的重试延迟
		select {
		case <-ctx.Done():
			log.Println("Telegram 机器人在重试前收到停止信号")
			return
		case <-time.After(constants.TelegramRetryDelay):
			continue
		}
	}
}

// handleMessage 处理 Telegram 消息
func (b *Bot) handleMessage(message *tgbotapi.Message) {
	chatID := message.Chat.ID
	text := message.Text

	// 处理身份验证
	if !b.isAuthenticated(chatID) {
		b.handleAuthentication(chatID, text)
		return
	}

	// 处理已验证用户的指令
	b.handleCommand(chatID, text)
}

// isAuthenticated 检查是否已验证
func (b *Bot) isAuthenticated(chatID int64) bool {
	b.chatIDMutex.Lock()
	defer b.chatIDMutex.Unlock()
	return b.authenticatedChatID == chatID
}

// setAuthenticated 设置已验证的聊天ID
func (b *Bot) setAuthenticated(chatID int64) {
	b.chatIDMutex.Lock()
	defer b.chatIDMutex.Unlock()
	b.authenticatedChatID = chatID
	b.saveAuthenticatedChatIDLocked()
}

// getAuthenticatedChatID 获取已验证的聊天ID
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

// sendMessage 发送消息
func (b *Bot) sendMessage(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
}

// SendNotification 发送通知（公开方法供外部调用）
func (b *Bot) SendNotification(message string) error {
	chatID := b.GetAuthenticatedChatID()
	if chatID == 0 {
		return fmt.Errorf("no authenticated chat ID")
	}
	return b.sendMessage(chatID, message)
}

// SetRestartCallback 设置重启回调函数
func (b *Bot) SetRestartCallback(callback func() error) {
	b.restartCallback = callback
}

// SetLendingBot 设置借贷机器人引用
func (b *Bot) SetLendingBot(lendingBot LendingBot) {
	b.lendingBot = lendingBot
}

// handleAuthentication 处理身份验证
func (b *Bot) handleAuthentication(chatID int64, text string) {
	switch text {
	case "/auth":
		b.sendMessage(chatID, "请输入验证 token：")
	case b.config.TelegramAuthToken:
		b.setAuthenticated(chatID)
		b.sendMessage(chatID, "验证成功，现在可以传送指令了")
	default:
		b.sendMessage(chatID, "请先进行验证，输入 /auth 开始验证流程")
	}
}

// handleCommand 处理指令
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
		b.sendMessage(chatID, "无效的指令，输入 /help 查看所有可用指令")
	}
}

// handleHelp 处理帮助指令
func (b *Bot) handleHelp(chatID int64) {
	helpText := `可用指令:

📊 查询指令:
/rate - 显示当前贷出利率和阈值
/check - 检查贷出利率是否超过阈值
/status - 显示系统状态
/strategy - 显示当前策略状态
/lending - 查看当前活跃的借贷订单

⚙️ 设置指令:
/threshold [数值] - 设置利率通知阈值
/reserve [数值] - 设置不参与借贷的保留金额
/orderlimit [数值] - 设置单次执行最大下单数量限制
/loandays [数值] - 设置固定借贷天数 (设为0使用自动判断)
/mindailylendrate [数值|FRR] - 设置最低每日贷出利率（FRR 为浮动利率模式）
/minloan [数值] - 设置单笔最小贷出金额
/maxloan [数值] - 设置单笔最大贷出金额 (设为0无限制)
/highholdrate [数值] - 设置高额持有策略的日利率
/highholdamount [数值] - 设置高额持有策略的金额 (设为0关闭)
/highholdorders [数值] - 设置高额持有策略的订单数量
/raterangeincrease [数值] - 设置利率范围增加百分比 (0-100%)

🧠 策略指令:
/klinestrategy on - 启用K线策略 (最高优先级)
/klinestrategy off - 停用K线策略
/smartstrategy on - 启用智能策略 (中等优先级)
/smartstrategy off - 停用智能策略
/smoothmethod [方法] - 设置K线利率平滑方法 (max/sma/ema/hla/p90)

🔄 控制指令:
/restart - 手动重新启动，清除所有订单，重新运行
/help - 显示此帮助消息

💡 策略优先级: K线策略 > 智能策略 > 传统策略`

	b.sendMessage(chatID, helpText)
}
