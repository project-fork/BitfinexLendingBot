package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
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
	ListPendingFundingOffers() ([]*bitfinex.PendingFundingOffer, error)
	CancelPendingFundingOffers(includeAll bool) (*bitfinex.FundingOfferCancelSummary, error)
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
	restartCallback     func() error // 取消订单后重跑回调函数
	runCallback         func() error // 保留未成交订单直接重跑回调函数
	lendingBot          LendingBot   // 借贷机器人引用
	logger              *log.Logger
	sendMessageFunc     func(chatID int64, text string) error
	sendChattableFunc   func(c tgbotapi.Chattable) error
	answerCallbackFunc  func(config tgbotapi.CallbackConfig) error
	pendingReplies      map[int64]string
	pendingRepliesMu    sync.Mutex
}

type telegramCommand struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// NewBot 创建新的 Telegram 机器人
func NewBot(cfg *config.Config, bfxClient *bitfinex.Client) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}

	bot := &Bot{
		api:            api,
		config:         cfg,
		bitfinexClient: bfxClient,
		rateConverter:  rates.NewConverter(),
		dataFilePath:   storage.DefaultDataFilePath(),
		logger:         log.New(os.Stderr, "[TelegramBot] ", log.LstdFlags|log.Lmsgprefix),
		pendingReplies: make(map[int64]string),
	}
	bot.logger.Printf("Authorized on account %s", api.Self.UserName)
	bot.loadAuthenticatedChatID()
	if err := bot.registerCommands(); err != nil {
		bot.logger.Printf("注册 Telegram 命令失败: %v", err)
	}
	return bot, nil
}

func buildTelegramCommands() []telegramCommand {
	return []telegramCommand{
		{Command: "help", Description: "帮助 | 显示帮助消息"},
		{Command: "start", Description: "帮助 | 显示帮助消息"},

		{Command: "rate", Description: "查询 | 显示当前贷出利率"},
		{Command: "check", Description: "查询 | 检查利率阈值"},
		{Command: "status", Description: "查询 | 显示系统状态"},
		{Command: "strategy", Description: "查询 | 显示当前策略"},
		{Command: "lending", Description: "查询 | 查看活跃借贷"},
		{Command: "offers", Description: "查询 | 查看未成交订单"},

		{Command: "threshold", Description: "设置 | 利率通知阈值"},
		{Command: "reserve", Description: "设置 | 保留金额"},
		{Command: "orderlimit", Description: "设置 | 单次下单限制"},
		{Command: "loandays", Description: "设置 | 固定借贷天数"},
		{Command: "mindailylendrate", Description: "设置 | 最低每日利率"},
		{Command: "minloan", Description: "设置 | 单笔最小金额"},
		{Command: "maxloan", Description: "设置 | 单笔最大金额"},
		{Command: "highholdrate", Description: "设置 | 高额持有利率"},
		{Command: "highholdamount", Description: "设置 | 高额持有金额"},
		{Command: "highholdorders", Description: "设置 | 高额持有订单数"},
		{Command: "raterangeincrease", Description: "设置 | 利率范围增加"},
		{Command: "smoothmethod", Description: "设置 | K线平滑方法"},

		{Command: "smartstrategy", Description: "策略 | 切换智能策略"},
		{Command: "klinestrategy", Description: "策略 | 切换K线策略"},

		{Command: "restart", Description: "控制 | 取消追踪订单后重跑"},
		{Command: "run", Description: "控制 | 保留订单直接重跑"},
		{Command: "canceloffers", Description: "控制 | 取消未成交订单"},
	}
}

func buildSetMyCommandsParams() (url.Values, error) {
	rawCommands, err := json.Marshal(buildTelegramCommands())
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("commands", string(rawCommands))
	return params, nil
}

func (b *Bot) registerCommands() error {
	if b.api == nil {
		return nil
	}

	params, err := buildSetMyCommandsParams()
	if err != nil {
		return err
	}

	_, err = b.api.MakeRequest("setMyCommands", params)
	return err
}

// SetLogger 设置日志记录器
func (b *Bot) SetLogger(logger *log.Logger) {
	if logger == nil {
		return
	}
	b.logger = logger
}

func (b *Bot) getLogger() *log.Logger {
	if b.logger == nil {
		b.logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return b.logger
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
			b.getLogger().Println("Telegram 机器人收到停止信号")
			return
		default:
		}

		u := tgbotapi.NewUpdate(0)
		u.Timeout = int(constants.TelegramUpdateTimeout.Seconds())

		updates, err := b.api.GetUpdatesChan(u)
		if err != nil {
			b.getLogger().Printf("Failed to get updates, retrying in %v: %v", constants.TelegramRetryDelay, err)

			// 使用 context 支持的 sleep
			select {
			case <-ctx.Done():
				b.getLogger().Println("Telegram 机器人在重试等待中收到停止信号")
				return
			case <-time.After(constants.TelegramRetryDelay):
				continue
			}
		}

		// 处理更新，直到 channel 关闭或 context 取消
		for {
			select {
			case <-ctx.Done():
				b.getLogger().Println("Telegram 机器人在处理更新时收到停止信号")
				return
			case update, ok := <-updates:
				if !ok {
					b.getLogger().Printf("Update channel closed, retrying in %v...", constants.TelegramRetryDelay)
					goto retry
				}

				if update.CallbackQuery != nil {
					go b.handleCallbackQuery(update.CallbackQuery)
					continue
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
			b.getLogger().Println("Telegram 机器人在重试前收到停止信号")
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

	if b.handlePendingReply(message) {
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
	if b.sendMessageFunc != nil {
		return b.sendMessageFunc(chatID, text)
	}
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := b.api.Send(msg)
	return err
}

func (b *Bot) sendChattable(c tgbotapi.Chattable) error {
	if b.sendChattableFunc != nil {
		return b.sendChattableFunc(c)
	}
	_, err := b.api.Send(c)
	return err
}

func (b *Bot) answerCallback(config tgbotapi.CallbackConfig) error {
	if b.answerCallbackFunc != nil {
		return b.answerCallbackFunc(config)
	}
	_, err := b.api.AnswerCallbackQuery(config)
	return err
}

func (b *Bot) setPendingReply(chatID int64, command string) {
	b.pendingRepliesMu.Lock()
	defer b.pendingRepliesMu.Unlock()
	if b.pendingReplies == nil {
		b.pendingReplies = make(map[int64]string)
	}
	b.pendingReplies[chatID] = command
}

func (b *Bot) getPendingReply(chatID int64) (string, bool) {
	b.pendingRepliesMu.Lock()
	defer b.pendingRepliesMu.Unlock()
	command, ok := b.pendingReplies[chatID]
	return command, ok
}

func (b *Bot) clearPendingReply(chatID int64) {
	b.pendingRepliesMu.Lock()
	defer b.pendingRepliesMu.Unlock()
	delete(b.pendingReplies, chatID)
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

// SetRunCallback 设置保留未成交订单直接重跑的回调函数
func (b *Bot) SetRunCallback(callback func() error) {
	b.runCallback = callback
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
	case text == "/run":
		b.handleRun(chatID)
	case text == "/rate":
		b.handleRate(chatID)
	case text == "/check":
		b.handleCheck(chatID)
	case text == "/status":
		b.handleStatus(chatID)
	case text == "/threshold" || strings.HasPrefix(text, "/threshold "):
		b.handleSetThreshold(chatID, text)
	case text == "/reserve" || strings.HasPrefix(text, "/reserve "):
		b.handleSetReserve(chatID, text)
	case text == "/orderlimit" || strings.HasPrefix(text, "/orderlimit "):
		b.handleSetOrderLimit(chatID, text)
	case text == "/loandays" || strings.HasPrefix(text, "/loandays "):
		b.handleSetLoanDays(chatID, text)
	case text == "/mindailylendrate" || strings.HasPrefix(text, "/mindailylendrate "):
		b.handleSetMinDailyRate(chatID, text)
	case text == "/minloan" || strings.HasPrefix(text, "/minloan "):
		b.handleSetMinLoan(chatID, text)
	case text == "/maxloan" || strings.HasPrefix(text, "/maxloan "):
		b.handleSetMaxLoan(chatID, text)
	case text == "/highholdrate" || strings.HasPrefix(text, "/highholdrate "):
		b.handleSetHighHoldRate(chatID, text)
	case text == "/highholdamount" || strings.HasPrefix(text, "/highholdamount "):
		b.handleSetHighHoldAmount(chatID, text)
	case text == "/highholdorders" || strings.HasPrefix(text, "/highholdorders "):
		b.handleSetHighHoldOrders(chatID, text)
	case text == "/raterangeincrease" || strings.HasPrefix(text, "/raterangeincrease "):
		b.handleSetRateRangeIncrease(chatID, text)
	case text == "/offers":
		b.handlePendingOffers(chatID)
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
	case text == "/canceloffers" || strings.HasPrefix(text, "/canceloffers "):
		b.handleCancelPendingOffers(chatID, text)
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
/offers - 查看当前未成交订单（含程序追踪标记）

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
/restart - 取消程序追踪到的未成交订单后重新执行策略
/run - 直接重新执行策略，保留现有未成交订单
/canceloffers [all] - 取消未成交订单，默认仅取消程序追踪订单；加 all 取消全部
/help - 显示此帮助消息

💡 策略优先级: K线策略 > 智能策略 > 传统策略`

	b.sendMessage(chatID, helpText)
}
