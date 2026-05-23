package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/urfave/cli"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/strategy"
	"github.com/kfrico/BitfinexLendingBot/internal/telegram"
)

// Application 应用程序主结构
type Application struct {
	config         *config.Config
	bfxClient      *bitfinex.Client
	telegramBot    *telegram.Bot
	lendingBot     *strategy.LendingBot
	rateConverter  *rates.Converter
	mainLogger     *log.Logger
	lendingLogger  *log.Logger
	hourlyLogger   *log.Logger
	telegramLogger *log.Logger

	// 并发控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 主任务执行追踪
	mainTaskRunCount int
	mainTaskMu       sync.Mutex
	mainTaskRunning  bool
}

var errMainTaskAlreadyRunning = errors.New("main task already running")

// NewApplication 创建新的应用程序实例
func NewApplication(configPath string) (*Application, error) {
	// 载入配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// 创建 Bitfinex 客户端
	bfxClient := bitfinex.NewClient(cfg.BitfinexApiKey, cfg.BitfinexSecretKey)

	var telegramBot *telegram.Bot
	if cfg.IsTelegramEnabled() {
		telegramBot, err = telegram.NewBot(cfg, bfxClient)
		if err != nil {
			log.Printf("⚠️ Telegram 初始化失败，已降级为禁用模式: %v", err)
		}
	} else {
		log.Printf("ℹ️ Telegram 已禁用: %s", cfg.TelegramDisabledReason())
	}

	// 创建贷出机器人
	lendingBot := strategy.NewLendingBot(cfg, bfxClient)

	// 创建利率转换器
	rateConverter := rates.NewConverter()

	// 创建 context 和 cancel 函数
	ctx, cancel := context.WithCancel(context.Background())

	app := &Application{
		config:         cfg,
		bfxClient:      bfxClient,
		telegramBot:    telegramBot,
		lendingBot:     lendingBot,
		rateConverter:  rateConverter,
		mainLogger:     newPrefixedLogger("MainTask", os.Stderr),
		lendingLogger:  newPrefixedLogger("LendingCheck", os.Stderr),
		hourlyLogger:   newPrefixedLogger("RateCheck", os.Stderr),
		telegramLogger: newPrefixedLogger("TelegramBot", os.Stderr),
		ctx:            ctx,
		cancel:         cancel,
	}

	lendingBot.SetLogger(app.mainLogger)

	if telegramBot != nil {
		telegramBot.SetLogger(app.telegramLogger)
		telegramBot.SetRestartCallback(app.handleRestart)
		telegramBot.SetRunCallback(app.handleRun)
		telegramBot.SetLendingBot(lendingBot)
		lendingBot.SetNotifyCallback(telegramBot.SendNotification)
	}

	return app, nil
}

// Run 运行应用程序
func (app *Application) Run() error {
	// 显示运行模式
	if app.config.TestMode {
		log.Println("🧪 === 测试模式启动 ===")
		log.Println("🧪 不会执行真实的下单操作")
		log.Println("🧪 但会执行真实的取消操作")
	} else {
		log.Println("🚀 === 正式模式启动 ===")
		log.Println("🚀 将执行真实的交易操作")
	}

	app.logStartupSelfCheck()

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动所有 goroutines
	app.startWorkers()

	log.Printf("Scheduler started at: %v", time.Now())
	if app.config.RunOnlyOnNewCredits {
		log.Printf("⚙️ 执行模式: 触发条件执行（新借贷订单或余额变化）")
	} else {
		log.Printf("⚙️ 执行模式: 定时执行，间隔: %d 分钟", app.config.MinutesRun)
	}
	log.Printf("💰 借贷检查间隔: %d 分钟", app.config.LendingCheckMinutes)
	log.Printf("📊 利率检查: 每小时")
	log.Println("🔄 按 Ctrl+C 优雅关闭...")

	// 等待信号或 context 取消
	select {
	case sig := <-sigChan:
		log.Printf("收到信号 %v，开始优雅关闭...", sig)
	case <-app.ctx.Done():
		log.Println("Context 被取消，开始关闭...")
	}

	return app.shutdown()
}

func (app *Application) logStartupSelfCheck() {
	if app == nil || app.config == nil {
		return
	}

	log.Println("🔎 === 启动自检摘要 ===")
	log.Printf("📌 Funding Symbol: %s", app.config.GetFundingSymbol())
	log.Printf("📌 策略模式: %s", describeStrategyMode(app.config))
	log.Printf("📌 执行模式: %s", describeRunMode(app.config))
	log.Printf("📌 最低日利率: %s", app.config.GetMinDailyRateDisplay())
	log.Printf("📌 借贷天数: %s", describeLoanDays(app.config))
	log.Printf("📌 单次下单限制: %s", describeOrderLimit(app.config))
	log.Printf("📌 Telegram 状态: %s", describeTelegramStartupState(app))
	log.Printf("📌 通知格式: %s", describeNotificationFormat(app.config))

	for _, warning := range collectStartupWarnings(app) {
		log.Printf("⚠️ %s", warning)
	}

	log.Println("🔎 === 启动自检结束 ===")
}

func describeStrategyMode(cfg *config.Config) string {
	if cfg == nil {
		return "未知"
	}

	switch cfg.GetStrategy() {
	case config.StrategyKline:
		return fmt.Sprintf("kline（%s / %d 根 / 平滑=%s）", cfg.KlineTimeFrame, cfg.KlinePeriod, cfg.KlineSmoothMethod)
	case config.StrategySimple:
		return "simple"
	case config.StrategySmart:
		return "smart"
	default:
		return "traditional"
	}
}

func describeRunMode(cfg *config.Config) string {
	if cfg == nil {
		return "未知"
	}
	if cfg.RunOnlyOnNewCredits {
		return "触发条件执行（新借贷订单或余额变化）"
	}
	return fmt.Sprintf("定时执行（每 %d 分钟）", cfg.MinutesRun)
}

func describeLoanDays(cfg *config.Config) string {
	if cfg == nil {
		return "未知"
	}
	if cfg.LoanDays == 0 {
		return "自动判断"
	}
	return fmt.Sprintf("%d 天", cfg.LoanDays)
}

func describeOrderLimit(cfg *config.Config) string {
	if cfg == nil {
		return "未知"
	}
	if cfg.OrderLimit == 0 {
		return "不限制"
	}
	return fmt.Sprintf("%d", cfg.OrderLimit)
}

func describeNotificationFormat(cfg *config.Config) string {
	if cfg == nil {
		return "未知"
	}
	if strings.TrimSpace(cfg.NotificationFormat) == "" {
		return "classic"
	}
	return cfg.NotificationFormat
}

func describeTelegramStartupState(app *Application) string {
	if app == nil || app.config == nil {
		return "未知"
	}
	if app.telegramBot != nil {
		return "已启用"
	}
	return "已禁用（" + app.config.TelegramDisabledReason() + "）"
}

func collectStartupWarnings(app *Application) []string {
	if app == nil || app.config == nil {
		return nil
	}

	warnings := make([]string, 0)
	cfg := app.config

	if !cfg.TestMode {
		warnings = append(warnings, "当前为正式模式，下单和取消操作都会真实生效")
	}
	if app.telegramBot == nil {
		warnings = append(warnings, "Telegram 控制与通知不可用，运行期无法远程查看状态或改参")
	}
	if cfg.OrderLimit == 0 {
		warnings = append(warnings, "ORDER_LIMIT=0，单次执行下单数量不受限制，请确认这是预期行为")
	}
	if cfg.ReserveAmount > 0 {
		warnings = append(warnings, fmt.Sprintf("已启用保留金额 %.2f %s，实际参与放贷的可用余额会先扣减这部分金额", cfg.ReserveAmount, strings.ToUpper(cfg.Currency)))
	}
	if cfg.IsMinDailyLendRateFRR() {
		warnings = append(warnings, "MIN_DAILY_LEND_RATE 当前为 FRR 模式，分散单会按 FRR 逻辑报价")
	}
	if cfg.HighHoldAmount <= 0 {
		warnings = append(warnings, "高额持有策略当前等同关闭，将主要依赖分散单策略")
	}
	if cfg.IsKlineStrategy() {
		warnings = append(warnings, "K 线策略依赖 Bitfinex Candles 数据，若 K 线请求失败，本轮会直接中止下单")
	} else {
		warnings = append(warnings, "当前策略依赖 Funding Book 定价，若 Funding Book 请求失败，本轮会直接中止下单")
	}

	return warnings
}

// startWorkers 启动所有工作 goroutines
func (app *Application) startWorkers() {
	if app.telegramBot != nil {
		app.wg.Add(1)
		go app.runWorker("TelegramBot", func() {
			defer app.wg.Done()
			app.telegramBot.StartWithContext(app.ctx)
		})
	}

	// 启动每小时利率检查
	app.wg.Add(1)
	go app.runWorker("RateCheck", func() {
		defer app.wg.Done()
		app.scheduleHourlyRateCheck()
	})

	// 启动借贷订单检查
	app.wg.Add(1)
	go app.runWorker("LendingCheck", func() {
		defer app.wg.Done()
		app.scheduleLendingCheck()
	})

	// 启动主要业务逻辑调度
	app.wg.Add(1)
	go app.runWorker("MainTask", func() {
		defer app.wg.Done()
		app.scheduleMainTask()
	})
}

// runWorker 安全运行工作任务
func (app *Application) runWorker(name string, worker func()) {
	logger := app.getTaskLogger(name)
	defer func() {
		if r := recover(); r != nil {
			logger.Printf("工作任务 %s 发生 panic: %v", name, r)
			// 可以在这里添加重启逻辑
		}
	}()

	logTaskBoundary(logger, boundaryStart, fmt.Sprintf("启动工作任务: %s", name))
	worker()
	logTaskBoundary(logger, boundaryEnd, fmt.Sprintf("工作任务 %s 已结束", name))
}

func (app *Application) getTaskLogger(name string) *log.Logger {
	switch name {
	case "MainTask":
		if app.mainLogger != nil {
			return app.mainLogger
		}
	case "LendingCheck":
		if app.lendingLogger != nil {
			return app.lendingLogger
		}
	case "RateCheck":
		if app.hourlyLogger != nil {
			return app.hourlyLogger
		}
	case "TelegramBot":
		if app.telegramLogger != nil {
			return app.telegramLogger
		}
	}
	return log.Default()
}

// shutdown 优雅关闭应用程序
func (app *Application) shutdown() error {
	log.Println("正在关闭应用程序...")

	// 取消 context
	app.cancel()

	// 等待所有 goroutines 结束，设置超时
	done := make(chan struct{})
	go func() {
		app.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("所有工作任务已优雅结束")
	case <-time.After(constants.ShutdownTimeout):
		log.Println("等待超时，强制结束")
	}

	log.Println("应用程序已关闭")
	return nil
}

// scheduleMainTask 调度主要任务
func (app *Application) scheduleMainTask() {
	// 如果启用了仅在触发条件时执行的模式（新借贷订单或余额变化），则不进行定时执行
	if app.config.RunOnlyOnNewCredits {
		app.mainLogger.Println("启用了触发条件执行模式（新借贷订单或余额变化），主要任务将由检查触发")
		// 先执行第一次初始化
		app.executeMainTask("启动初始化")

		// 等待 context 取消
		<-app.ctx.Done()
		app.mainLogger.Println("主要任务调度器收到停止信号")
		return
	}

	// 传统的定时执行模式
	app.mainLogger.Printf("启用定时执行模式，间隔: %d 分钟", app.config.MinutesRun)
	// 先执行第一次
	app.executeMainTask("启动初始化")

	ticker := time.NewTicker(time.Duration(app.config.MinutesRun) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			app.mainLogger.Println("主要任务调度器收到停止信号")
			return
		case <-ticker.C:
			app.executeMainTask(fmt.Sprintf("定时触发（每 %d 分钟）", app.config.MinutesRun))
		}
	}
}

// executeMainTask 执行主要任务
func (app *Application) executeMainTask(trigger string) {
	if !app.beginMainTask(trigger) {
		return
	}
	defer app.endMainTask()
	app.executeMainTaskBody(trigger, false)
}

func (app *Application) beginMainTask(trigger string) bool {
	app.mainTaskMu.Lock()
	if app.mainTaskRunning {
		app.mainTaskMu.Unlock()
		app.mainLogger.Printf("⏭️ 跳过主策略执行，已有任务运行中，触发来源: %s", trigger)
		return false
	}
	app.mainTaskRunCount++
	runID := app.mainTaskRunCount
	app.mainTaskRunning = true
	app.mainTaskMu.Unlock()

	logTaskBoundary(
		app.mainLogger,
		boundaryStart,
		fmt.Sprintf("🔁 开始重跑主策略 #%d", runID),
		fmt.Sprintf("📍 触发来源: %s", trigger),
		fmt.Sprintf("🕒 触发时间: %s", time.Now().Format("2006-01-02 15:04:05")),
	)
	return true
}

func (app *Application) endMainTask() {
	app.mainTaskMu.Lock()
	app.mainTaskRunning = false
	app.mainTaskMu.Unlock()
}

func (app *Application) executeMainTaskBody(trigger string, cancelTrackedOffers bool) {
	app.mainTaskMu.Lock()
	runID := app.mainTaskRunCount
	app.mainTaskMu.Unlock()

	var err error
	if cancelTrackedOffers {
		if strings.Contains(trigger, "手动触发") {
			err = app.lendingBot.ExecuteManual(trigger, true)
		} else {
			err = app.lendingBot.ExecuteWithOfferCancellation()
		}
	} else {
		if strings.Contains(trigger, "手动触发") {
			err = app.lendingBot.ExecuteManual(trigger, false)
		} else {
			err = app.lendingBot.Execute()
		}
	}

	if err != nil {
		app.mainLogger.Printf("❌ 主策略 #%d 执行失败（触发来源: %s）: %v", runID, trigger, err)
		logTaskBoundary(app.mainLogger, boundaryEnd, fmt.Sprintf("🔚 结束主策略 #%d（失败）", runID))
		return
	}

	logTaskBoundary(app.mainLogger, boundaryEnd, fmt.Sprintf("✅ 结束主策略 #%d（成功）", runID))
}

type taskBoundaryPhase int

const (
	boundaryStart taskBoundaryPhase = iota
	boundaryEnd
)

func logTaskBoundary(logger *log.Logger, phase taskBoundaryPhase, lines ...string) {
	if logger == nil {
		logger = log.Default()
	}

	maxWidth := 0
	for _, line := range lines {
		if len(line) > maxWidth {
			maxWidth = len(line)
		}
	}

	if maxWidth == 0 {
		maxWidth = 8
	}

	lineWidth := maxWidth + 8
	topChar := "="
	bottomChar := "-"
	if phase == boundaryEnd {
		topChar = "-"
		bottomChar = "="
	}

	logger.Println(strings.Repeat(topChar, lineWidth))
	for _, line := range lines {
		logger.Println(line)
	}
	logger.Println(strings.Repeat(bottomChar, lineWidth))
}

// scheduleHourlyRateCheck 调度每小时利率检查
func (app *Application) scheduleHourlyRateCheck() {
	for {
		select {
		case <-app.ctx.Done():
			app.hourlyLogger.Println("利率检查调度器收到停止信号")
			return
		default:
		}

		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), constants.HourlyCheckMinute, 0, 0, now.Location())
		if now.After(next) || now.Equal(next) {
			next = next.Add(time.Hour)
		}

		delay := next.Sub(now)
		app.hourlyLogger.Printf("下次执行时间: %s, 等待时间: %s", next.Format("2006-01-02 15:04:05"), delay)

		// 使用 context 支持的 sleep
		select {
		case <-app.ctx.Done():
			app.hourlyLogger.Println("利率检查调度器在等待中收到停止信号")
			return
		case <-time.After(delay):
			app.checkRateThreshold()
		}
	}
}

// checkRateThreshold 检查利率阈值
func (app *Application) checkRateThreshold() {
	app.hourlyLogger.Println("定时检查贷出利率（基于5分钟K线12根高点）...")

	exceeded, percentageRate, err := app.lendingBot.CheckRateThreshold()
	if err != nil {
		app.hourlyLogger.Printf("取得利率数据失败: %v", err)
		return
	}

	app.hourlyLogger.Printf("最近1小时最高利率: %.4f%%, 阈值: %.4f%%", percentageRate, app.config.NotifyRateThreshold)

	if exceeded {
		message := fmt.Sprintf("⚠️ 定时检查提醒: 最近1小时最高利率 %.4f%% 已超过阈值 %.4f%%\n\n📊 检查方式: 5分钟K线最近12根高点分析",
			percentageRate, app.config.NotifyRateThreshold)

		app.sendTelegramNotification(app.hourlyLogger, "利率提醒", message)
	} else {
		app.hourlyLogger.Println("最近1小时最高利率低于阈值，无需发送通知")
	}
}

func (app *Application) sendTelegramNotification(logger *log.Logger, notificationType string, message string) {
	if logger == nil {
		logger = log.Default()
	}

	if app.telegramBot == nil {
		logger.Printf("Telegram 未启用，跳过%s发送", notificationType)
		return
	}

	if err := app.telegramBot.SendNotification(message); err != nil {
		logger.Printf("发送 Telegram %s失败: %v", notificationType, err)
		return
	}

	logger.Printf("成功发送%s", notificationType)
}

// handleRestart 处理重启请求
func (app *Application) handleRestart() error {
	log.Println("收到重启请求，开始执行重启逻辑...")

	if !app.beginMainTask("Telegram /restart 手动触发") {
		return errMainTaskAlreadyRunning
	}
	defer app.endMainTask()
	app.executeMainTaskBody("Telegram /restart 手动触发", true)

	log.Println("重启完成！")
	return nil
}

// handleRun 处理保留未成交订单直接重跑请求
func (app *Application) handleRun() error {
	log.Println("收到直接重跑请求，开始执行重跑逻辑...")

	if !app.beginMainTask("Telegram /run 手动触发") {
		return errMainTaskAlreadyRunning
	}
	defer app.endMainTask()
	app.executeMainTaskBody("Telegram /run 手动触发", false)

	log.Println("直接重跑完成！")
	return nil
}

// scheduleLendingCheck 调度借贷订单检查
func (app *Application) scheduleLendingCheck() {
	ticker := time.NewTicker(time.Duration(app.config.LendingCheckMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-app.ctx.Done():
			log.Println("借贷检查调度器收到停止信号")
			return
		case <-ticker.C:
			app.executeLendingCheck()
		}
	}
}

// executeLendingCheck 执行借贷订单检查
func (app *Application) executeLendingCheck() {
	app.mainTaskMu.Lock()
	mainTaskRunning := app.mainTaskRunning
	app.mainTaskMu.Unlock()

	if mainTaskRunning {
		app.lendingLogger.Println("主任务执行中，跳过借贷检查")
		return
	}

	hasNewCredits, err := app.lendingBot.CheckNewLendingCredits()
	if err != nil {
		app.lendingLogger.Printf("检查借贷订单失败: %v", err)
		return
	}

	// 如果启用了触发条件执行模式，且满足触发条件（新借贷订单或余额变化），触发主要任务执行
	if app.config.RunOnlyOnNewCredits && hasNewCredits {
		app.lendingLogger.Println("满足执行触发条件，触发主要任务执行")
		app.executeMainTask("借贷检查触发（新借贷订单或余额变化）")
	}
}

func main() {
	app := cli.NewApp()
	app.Name = "bitfinex-lending-bot"
	app.Version = "v2.1.0"
	app.Usage = "Automated Bitfinex lending bot with v2 API"

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "config, c",
			Value:  "config.yaml",
			Usage:  "Configuration file path",
			EnvVar: "CONFIG_PATH",
		},
	}

	app.Action = func(c *cli.Context) error {
		configPath := c.String("config")

		application, err := NewApplication(configPath)
		if err != nil {
			log.Fatalf("Failed to create application: %v", err)
		}

		return application.Run()
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("Application error: %v", err)
	}
}
