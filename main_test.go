package main

import (
	"context"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestPrefixedLogger_AddsTaskPrefix(t *testing.T) {
	var builder strings.Builder
	logger := newPrefixedLogger("MainTask", &builder)

	logger.Println("hello")

	if !strings.Contains(builder.String(), "[MainTask] hello") {
		t.Fatalf("expected prefixed log output, got %q", builder.String())
	}
}

func TestExecuteLendingCheck_SkipsWhileMainTaskRunning(t *testing.T) {
	reader, writer := io.Pipe()
	app := &Application{
		config: &config.Config{
			RunOnlyOnNewCredits: true,
		},
		ctx:           context.Background(),
		lendingLogger: log.New(writer, "", log.LstdFlags),
	}
	app.mainTaskRunning = true

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	app.executeLendingCheck()
	_ = writer.Close()
	<-done

	logs := builder.String()
	if !strings.Contains(logs, "主任务执行中，跳过借贷检查") {
		t.Fatalf("expected skip log, got:\n%s", logs)
	}
}

func TestScheduleLendingCheck_DoesNotRunImmediatelyOnStartup(t *testing.T) {
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	app := &Application{
		config: &config.Config{
			LendingCheckMinutes: 10,
		},
		ctx:           ctx,
		lendingLogger: log.New(writer, "", log.LstdFlags),
	}

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	finished := make(chan struct{})
	go func() {
		app.scheduleLendingCheck()
		close(finished)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	_ = writer.Close()
	<-finished
	<-done

	logs := builder.String()
	if strings.Contains(logs, "主任务执行中，跳过借贷检查") || strings.Contains(logs, "检查借贷订单失败") || strings.Contains(logs, "满足执行触发条件") {
		t.Fatalf("expected no immediate lending check logs on startup, got:\n%s", logs)
	}
}

func TestRunWorker_UsesTaskLoggerPrefix(t *testing.T) {
	var builder strings.Builder
	app := &Application{
		mainLogger: newPrefixedLogger("MainTask", &builder),
	}

	app.runWorker("MainTask", func() {})

	logs := builder.String()
	if !strings.Contains(logs, "[MainTask] ============================") {
		t.Fatalf("expected prefixed start log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "[MainTask] 启动工作任务: MainTask") {
		t.Fatalf("expected prefixed start message, got:\n%s", logs)
	}
	if !strings.Contains(logs, "[MainTask] ---------------------------") {
		t.Fatalf("expected boundary separator, got:\n%s", logs)
	}
	if !strings.Contains(logs, "[MainTask] 工作任务 MainTask 已结束") {
		t.Fatalf("expected prefixed finish log, got:\n%s", logs)
	}
}

func TestBeginMainTask_RejectsConcurrentExecution(t *testing.T) {
	var builder strings.Builder
	app := &Application{
		mainLogger: newPrefixedLogger("MainTask", &builder),
	}

	if !app.beginMainTask("首次触发") {
		t.Fatal("expected first execution to start")
	}
	defer app.endMainTask()

	if app.beginMainTask("并发触发") {
		t.Fatal("expected concurrent execution to be rejected")
	}

	if !strings.Contains(builder.String(), "已有任务运行中") {
		t.Fatalf("expected skip log, got:\n%s", builder.String())
	}
}

func TestBeginMainTask_RejectsWhileLendingCheckRunning(t *testing.T) {
	var builder strings.Builder
	app := &Application{
		mainLogger: newPrefixedLogger("MainTask", &builder),
	}
	app.lendingCheckRunning = true

	if app.beginMainTask("借贷检查触发") {
		t.Fatal("expected main task to be rejected while lending check is running")
	}

	if !strings.Contains(builder.String(), "借贷检查运行中") {
		t.Fatalf("expected lending-check skip log, got:\n%s", builder.String())
	}
}

func TestBeginLendingCheck_RejectsDuplicateExecution(t *testing.T) {
	reader, writer := io.Pipe()
	app := &Application{
		lendingLogger: log.New(writer, "", log.LstdFlags),
	}
	app.lendingCheckRunning = true

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	if app.beginLendingCheck() {
		t.Fatal("expected duplicate lending check to be rejected")
	}

	_ = writer.Close()
	<-done

	if !strings.Contains(builder.String(), "借贷检查执行中，跳过重复检查") {
		t.Fatalf("expected duplicate lending-check log, got:\n%s", builder.String())
	}
}

func TestLogTaskBoundary_UsesDirectionalSeparators(t *testing.T) {
	var builder strings.Builder
	logger := newPrefixedLogger("RateCheck", &builder)

	logTaskBoundary(logger, boundaryStart, "启动工作任务: RateCheck")
	logTaskBoundary(logger, boundaryEnd, "工作任务 RateCheck 已结束")

	logs := builder.String()
	for _, fragment := range []string{
		"[RateCheck] ================================",
		"[RateCheck] 启动工作任务: RateCheck",
		"[RateCheck] --------------------------------",
		"[RateCheck] 工作任务 RateCheck 已结束",
	} {
		if !strings.Contains(logs, fragment) {
			t.Fatalf("expected boundary log to contain %q, got:\n%s", fragment, logs)
		}
	}
}

func TestNewApplication_AllowsTelegramToBeDisabled(t *testing.T) {
	configFile, err := os.CreateTemp("", "bitfinex-config-*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp config: %v", err)
	}
	defer os.Remove(configFile.Name())

	content := `
BITFINEX_API_KEY: "test_api_key"
BITFINEX_SECRET_KEY: "test_secret_key"
CURRENCY: "USD"
MIN_LOAN: 150
MAX_LOAN: 500
MIN_DAILY_LEND_RATE: 0.02
SPREAD_LEND: 30
GAP_BOTTOM: 10
GAP_TOP: 5000
STRATEGY: "smart"
VOLATILITY_THRESHOLD: 0.002
MAX_RATE_MULTIPLIER: 2.0
MIN_RATE_MULTIPLIER: 0.8
RATE_RANGE_INCREASE_PERCENT: 0.2
LENDING_CHECK_MINUTES: 10
`
	if _, err := configFile.WriteString(content); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	if err := configFile.Close(); err != nil {
		t.Fatalf("failed to close temp config: %v", err)
	}

	app, err := NewApplication(configFile.Name())
	if err != nil {
		t.Fatalf("expected app creation without telegram, got error: %v", err)
	}
	if app.telegramBot != nil {
		t.Fatal("expected telegram bot to be nil when telegram is disabled")
	}
}

func TestSendTelegramNotification_SkipsWhenTelegramDisabled(t *testing.T) {
	var builder strings.Builder
	app := &Application{
		hourlyLogger: log.New(&builder, "", 0),
	}

	app.sendTelegramNotification(app.hourlyLogger, "利率提醒", "test")

	if !strings.Contains(builder.String(), "Telegram 未启用，跳过利率提醒发送") {
		t.Fatalf("expected telegram disabled log, got: %q", builder.String())
	}
}

func TestLogStartupSelfCheck_IncludesSummaryAndWarnings(t *testing.T) {
	var builder strings.Builder
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&builder)
	log.SetFlags(0)
	defer log.SetOutput(originalWriter)
	defer log.SetFlags(originalFlags)

	app := &Application{
		config: &config.Config{
			Currency:            "usd",
			Strategy:            config.StrategyKline,
			KlineTimeFrame:      "15m",
			KlinePeriod:         24,
			KlineSmoothMethod:   "ema",
			RunOnlyOnNewCredits: true,
			MinDailyLendRate:    "FRR",
			OrderLimit:          0,
			ReserveAmount:       100,
			TestMode:            false,
			HighHoldAmount:      0,
		},
		telegramBot: nil,
	}

	app.logStartupSelfCheck()

	logs := builder.String()
	if !strings.Contains(logs, "启动自检摘要") {
		t.Fatalf("expected startup self-check header, got:\n%s", logs)
	}
	if !strings.Contains(logs, "策略模式: kline（15m / 24 根 / 平滑=ema）") {
		t.Fatalf("expected strategy summary, got:\n%s", logs)
	}
	if !strings.Contains(logs, "执行模式: 触发条件执行") {
		t.Fatalf("expected run mode summary, got:\n%s", logs)
	}
	if !strings.Contains(logs, "Telegram 状态: 已禁用") {
		t.Fatalf("expected telegram status summary, got:\n%s", logs)
	}
	if !strings.Contains(logs, "当前为正式模式，下单和取消操作都会真实生效") {
		t.Fatalf("expected production warning, got:\n%s", logs)
	}
	if !strings.Contains(logs, "ORDER_LIMIT=0") {
		t.Fatalf("expected order limit warning, got:\n%s", logs)
	}
	if !strings.Contains(logs, "K 线策略依赖 Bitfinex Candles 数据") {
		t.Fatalf("expected kline dependency warning, got:\n%s", logs)
	}
}

func TestLogStartupSelfCheck_DoesNotExposeSensitiveTokens(t *testing.T) {
	var builder strings.Builder
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&builder)
	log.SetFlags(0)
	defer log.SetOutput(originalWriter)
	defer log.SetFlags(originalFlags)

	app := &Application{
		config: &config.Config{
			Currency:          "usd",
			Strategy:          config.StrategyTraditional,
			MinDailyLendRate:  0.02,
			TelegramBotToken:  "secret-bot-token",
			TelegramAuthToken: "secret-auth-token",
			TestMode:          true,
			HighHoldAmount:    1000,
			HighHoldOrders:    1,
		},
		telegramBot: nil,
	}

	app.logStartupSelfCheck()

	logs := builder.String()
	if strings.Contains(logs, "secret-bot-token") || strings.Contains(logs, "secret-auth-token") {
		t.Fatalf("expected startup self-check to avoid sensitive tokens, got:\n%s", logs)
	}
}
