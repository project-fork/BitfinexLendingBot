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
	reader, writer := io.Pipe()
	bot := &LendingBot{
		config: &config.Config{
			MinDailyLendRate: 0.03,
			SpreadLend:       6,
			GapBottom:        2,
			GapTop:           18,
			MinLoan:          150,
			MaxLoan:          2000,
			LoanPeriodThresholds: map[int]float64{
				30:  0.03,
				60:  0.035,
				90:  0.04,
				120: 0.045,
			},
		},
		rateConverter: rates.NewConverter(),
		logger:        log.New(writer, "", log.LstdFlags),
	}

	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00025},
		{Rate: 0.00026},
		{Rate: 0.00027},
		{Rate: 0.00028},
		{Rate: 0.00029},
		{Rate: 0.000295},
	}

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	offers := bot.calculateSpreadOffers(431.985884, fundingBook, 10)
	if len(offers) != 2 {
		t.Fatalf("expected 2 offers, got %d", len(offers))
	}
	_ = writer.Close()
	<-done
	logs := builder.String()

	expectedFragments := []string{
		"分散策略 - 剩余资金",
		"实际拆单数: 2",
		"索引范围: 2-5",
		"传统深度推进索引: [2 5]",
		"低于最低利率 0.030000%",
		"期限决策 - 利率 0.030000% 达到 30天阈值 0.030000%，使用 30 天",
		"分散订单 #1",
		"分散订单 #2",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(logs, fragment) {
			t.Fatalf("expected logs to contain %q, got:\n%s", fragment, logs)
		}
	}
}

func TestCalculateSpreadOffers_UsesTraditionalDepthProgressionIndexes(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			MinDailyLendRate:        0.02,
			SpreadLend:              4,
			GapBottom:               1,
			GapTop:                  4,
			MinLoan:                 150,
			MaxLoan:                 1000,
			FundingBookRateUndercut: 0,
			LoanPeriodThresholds: map[int]float64{
				30: 0.02,
			},
		},
		rateConverter: rates.NewConverter(),
	}

	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00020},
		{Rate: 0.00021},
		{Rate: 0.00022},
		{Rate: 0.00023},
		{Rate: 0.00024},
		{Rate: 0.00025},
	}

	offers := bot.calculateSpreadOffers(800, fundingBook, 10)
	if len(offers) != 4 {
		t.Fatalf("expected 4 offers, got %d", len(offers))
	}

	expectedRates := []float64{0.00021, 0.00022, 0.00023, 0.00024}
	for i, expectedRate := range expectedRates {
		if offers[i].Rate != expectedRate {
			t.Fatalf("expected offer %d rate %.8f, got %.8f", i, expectedRate, offers[i].Rate)
		}
	}

	expectedDepths := []string{
		"Funding Book 深度索引 1",
		"Funding Book 深度索引 2",
		"Funding Book 深度索引 3",
		"Funding Book 深度索引 4",
	}
	for i, expectedDepth := range expectedDepths {
		if offers[i].Reason.DepthSource != expectedDepth {
			t.Fatalf("expected offer %d depth source %q, got %q", i, expectedDepth, offers[i].Reason.DepthSource)
		}
	}
}

func TestPlaceLoanOffers_LogsRateBonusDecision(t *testing.T) {
	reader, writer := io.Pipe()
	bot := &LendingBot{
		config: &config.Config{
			MinLoan:    150,
			RateBonus:  0.001,
			TestMode:   true,
			Currency:   "USD",
			OrderLimit: 10,
		},
		rateConverter: rates.NewConverter(),
		logger:        log.New(writer, "", log.LstdFlags),
	}

	offers := []*LoanOffer{
		{Amount: 215.99, Rate: 0.0003, Period: 30},
	}

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	if _, err := bot.placeLoanOffers(offers, false); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_ = writer.Close()
	<-done
	logs := builder.String()

	if !strings.Contains(logs, "下单决策 - 无既有待处理订单") {
		t.Fatalf("expected rate bonus decision log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "RATE_BONUS 0.001000%") {
		t.Fatalf("expected RATE_BONUS value in logs, got:\n%s", logs)
	}
}

func TestLogStrategyDecisionSummary_LogsStructuredSummary(t *testing.T) {
	reader, writer := io.Pipe()
	logger := log.New(writer, "", log.LstdFlags)

	summary := &StrategyDecisionSummary{
		Strategy:              "smart",
		FundingSymbol:         "fUSD",
		TriggerSource:         "Telegram /run 手动触发",
		CooldownBypassed:      true,
		SkipReason:            "无",
		FundsAvailable:        431.985884,
		ReserveAmount:         100,
		HasPendingOrders:      true,
		FundingBookSource:     "required_and_used",
		FundingBookEntries:    25,
		RequestedOfferCount:   3,
		AttemptedOfferCount:   3,
		SuccessfulOfferCount:  2,
		SkippedOfferCount:     1,
		FailedOfferCount:      0,
		FRROfferCount:         1,
		FixedRateOfferCount:   2,
		RateBonusAppliedCount: 0,
		MinOfferRatePercent:   0.03,
		MaxOfferRatePercent:   0.04,
		MinOfferAmount:        150,
		MaxOfferAmount:        200,
		LoanPeriods:           []int{30, 120},
		Notes:                 []string{"Funding Book 已用于定价"},
	}

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	logStrategyDecisionSummary(logger, summary)
	_ = writer.Close()
	<-done

	logs := builder.String()
	expectedFragments := []string{
		"策略决策摘要 | strategy=smart",
		"symbol=fUSD",
		"trigger=Telegram /run 手动触发",
		"cooldown_bypassed=true",
		"skip_reason=无",
		"requested=3",
		"success=2",
		"frr=1",
		"rate_range=0.030000%~0.040000%",
		"periods=30,120",
		"fund_sources=",
		"depth_sources=",
		"rate_sources=",
		"period_sources=",
		"execution_decisions=",
		"notes=Funding Book 已用于定价",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(logs, fragment) {
			t.Fatalf("expected logs to contain %q, got:\n%s", fragment, logs)
		}
	}
}
