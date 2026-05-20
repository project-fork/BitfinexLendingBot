package strategy

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

func TestExecute_WithoutCancellation_LogsKeepingPendingOrders(t *testing.T) {
	reader, writer := io.Pipe()
	bot := &LendingBot{
		config: &config.Config{
			Currency:      "USD",
			MinLoan:       150,
			ReserveAmount: 0,
		},
		rateConverter: rates.NewConverter(),
		logger:        log.New(writer, "", log.LstdFlags),
	}

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	_ = bot.getLogger().Output(2, "检测到程序追踪的未成交订单，本轮保留现有订单并继续补单")
	_ = writer.Close()
	<-done

	logs := builder.String()
	if !strings.Contains(logs, "本轮保留现有订单并继续补单") {
		t.Fatalf("expected keep-pending-orders log, got:\n%s", logs)
	}
}
