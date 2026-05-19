package main

import (
	"context"
	"io"
	"log"
	"strings"
	"testing"

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

func TestRunWorker_UsesTaskLoggerPrefix(t *testing.T) {
	var builder strings.Builder
	app := &Application{
		mainLogger: newPrefixedLogger("MainTask", &builder),
	}

	app.runWorker("MainTask", func() {})

	logs := builder.String()
	if !strings.Contains(logs, "[MainTask] 启动工作任务: MainTask") {
		t.Fatalf("expected prefixed start log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "[MainTask] 工作任务 MainTask 已结束") {
		t.Fatalf("expected prefixed finish log, got:\n%s", logs)
	}
}
