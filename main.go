package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
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

// NewApplication 创建新的应用程序实例
func NewApplication(configPath string) (*Application, error) {
	// 载入配置
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// 创建 Bitfinex 客户端
	bfxClient := bitfinex.NewClient(cfg.BitfinexApiKey, cfg.BitfinexSecretKey)

	// 创建 Telegram 机器人
	telegramBot, err := telegram.NewBot(cfg, bfxClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
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
		hourlyLogger:   newPrefixedLogger("HourlyRateCheck", os.Stderr),
		telegramLogger: newPrefixedLogger("TelegramBot", os.Stderr),
		ctx:            ctx,
		cancel:         cancel,
	}

	telegramBot.SetLogger(app.telegramLogger)
	lendingBot.SetLogger(app.mainLogger)

	// 设置 Telegram bot 控制回调
	telegramBot.SetRestartCallback(app.handleRestart)
	telegramBot.SetRunCallback(app.handleRun)

	// 设置借贷机器人的通知回调
	lendingBot.SetNotifyCallback(telegramBot.SendNotification)

	// 设置 Telegram bot 的借贷机器人引用
	telegramBot.SetLendingBot(lendingBot)

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

// startWorkers 启动所有工作 goroutines
func (app *Application) startWorkers() {
	// 启动 Telegram 机器人
	app.wg.Add(1)
	go app.runWorker("TelegramBot", func() {
		defer app.wg.Done()
		app.telegramBot.StartWithContext(app.ctx)
	})

	// 启动每小时利率检查
	app.wg.Add(1)
	go app.runWorker("HourlyRateCheck", func() {
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

	logger.Printf("启动工作任务: %s", name)
	worker()
	logger.Printf("工作任务 %s 已结束", name)
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
	case "HourlyRateCheck":
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
	app.mainTaskMu.Lock()
	app.mainTaskRunCount++
	runID := app.mainTaskRunCount
	app.mainTaskRunning = true
	app.mainTaskMu.Unlock()
	defer func() {
		app.mainTaskMu.Lock()
		app.mainTaskRunning = false
		app.mainTaskMu.Unlock()
	}()

	app.mainLogger.Println("============================================================")
	app.mainLogger.Printf("🔁 开始重跑主策略 #%d", runID)
	app.mainLogger.Printf("📍 触发来源: %s", trigger)
	app.mainLogger.Printf("🕒 触发时间: %s", time.Now().Format("2006-01-02 15:04:05"))
	app.mainLogger.Println("============================================================")

	if err := app.lendingBot.Execute(); err != nil {
		app.mainLogger.Printf("❌ 主策略 #%d 执行失败（触发来源: %s）: %v", runID, trigger, err)
		app.mainLogger.Println("============================================================")
		app.mainLogger.Printf("🔚 结束主策略 #%d（失败）", runID)
		app.mainLogger.Println("============================================================")
		return
	}

	app.mainLogger.Println("============================================================")
	app.mainLogger.Printf("✅ 结束主策略 #%d（成功）", runID)
	app.mainLogger.Println("============================================================")
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

		if err := app.telegramBot.SendNotification(message); err != nil {
			app.hourlyLogger.Printf("发送 Telegram 通知失败: %v", err)
		} else {
			app.hourlyLogger.Printf("成功发送利率提醒")
		}
	} else {
		app.hourlyLogger.Println("最近1小时最高利率低于阈值，无需发送通知")
	}
}

// handleRestart 处理重启请求
func (app *Application) handleRestart() error {
	log.Println("收到重启请求，开始执行重启逻辑...")

	// 执行主要任务（先取消程序追踪到的未成交订单）
	app.executeMainTaskWithCancel("Telegram /restart 手动触发")

	log.Println("重启完成！")
	return nil
}

// handleRun 处理保留未成交订单直接重跑请求
func (app *Application) handleRun() error {
	log.Println("收到直接重跑请求，开始执行重跑逻辑...")

	app.executeMainTask("Telegram /run 手动触发")

	log.Println("直接重跑完成！")
	return nil
}

func (app *Application) executeMainTaskWithCancel(trigger string) {
	app.mainTaskMu.Lock()
	app.mainTaskRunCount++
	runID := app.mainTaskRunCount
	app.mainTaskRunning = true
	app.mainTaskMu.Unlock()
	defer func() {
		app.mainTaskMu.Lock()
		app.mainTaskRunning = false
		app.mainTaskMu.Unlock()
	}()

	app.mainLogger.Println("============================================================")
	app.mainLogger.Printf("🔁 开始重跑主策略 #%d", runID)
	app.mainLogger.Printf("📍 触发来源: %s", trigger)
	app.mainLogger.Printf("🕒 触发时间: %s", time.Now().Format("2006-01-02 15:04:05"))
	app.mainLogger.Println("============================================================")

	if err := app.lendingBot.ExecuteWithOfferCancellation(); err != nil {
		app.mainLogger.Printf("❌ 主策略 #%d 执行失败（触发来源: %s）: %v", runID, trigger, err)
		app.mainLogger.Println("============================================================")
		app.mainLogger.Printf("🔚 结束主策略 #%d（失败）", runID)
		app.mainLogger.Println("============================================================")
		return
	}

	app.mainLogger.Println("============================================================")
	app.mainLogger.Printf("✅ 结束主策略 #%d（成功）", runID)
	app.mainLogger.Println("============================================================")
}

// scheduleLendingCheck 调度借贷订单检查
func (app *Application) scheduleLendingCheck() {
	// 先执行第一次检查
	app.executeLendingCheck()

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
