package strategy

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

func TestCalculateSpreadOffers_LogsPricingDecision(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			MinDailyLendRate:              0.03,
			SpreadLend:                    6,
			GapBottom:                     2,
			GapTop:                        18,
			MinLoan:                       150,
			MaxLoan:                       2000,
			ThirtyDayLendRateThreshold:    0.03,
			SixtyDayLendRateThreshold:     0.035,
			NinetyDayLendRateThreshold:    0.04,
			OneTwentyDayLendRateThreshold: 0.045,
		},
		rateConverter: rates.NewConverter(),
	}

	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00025},
		{Rate: 0.00026},
		{Rate: 0.00027},
		{Rate: 0.00028},
		{Rate: 0.00029},
		{Rate: 0.000295},
	}

	logs := captureLogs(func() {
		offers := bot.calculateSpreadOffers(431.985884, fundingBook, 10)
		if len(offers) != 2 {
			t.Fatalf("expected 2 offers, got %d", len(offers))
		}
	})

	expectedFragments := []string{
		"分散策略 - 剩余资金",
		"实际拆单数: 2",
		"低于最低利率 0.030000%",
		"期限决策 - 利率 0.030000% 达到 30天阈值 0.030000%",
		"分散订单 #1",
		"分散订单 #2",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(logs, fragment) {
			t.Fatalf("expected logs to contain %q, got:\n%s", fragment, logs)
		}
	}
}

func TestPlaceLoanOffers_LogsRateBonusDecision(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			MinLoan:    150,
			RateBonus:  0.001,
			TestMode:   true,
			Currency:   "USD",
			OrderLimit: 10,
		},
		rateConverter: rates.NewConverter(),
	}

	offers := []*LoanOffer{
		{Amount: 215.99, Rate: 0.0003, Period: 30},
	}

	logs := captureLogs(func() {
		if err := bot.placeLoanOffers(offers, false); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	if !strings.Contains(logs, "下单决策 - 无既有待处理订单") {
		t.Fatalf("expected rate bonus decision log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "RATE_BONUS 0.001000%") {
		t.Fatalf("expected RATE_BONUS value in logs, got:\n%s", logs)
	}
}

func captureLogs(fn func()) string {
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	reader, writer := io.Pipe()
	log.SetOutput(writer)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	fn()

	_ = writer.Close()
	<-done
	return builder.String()
}
