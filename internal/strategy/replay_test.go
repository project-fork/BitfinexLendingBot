package strategy

import (
	"strings"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func newReplayTestBot(strategyName string) *LendingBot {
	cfg := &config.Config{
		Currency:         "USD",
		Strategy:         strategyName,
		MinDailyLendRate: 0.03,
		SpreadLend:       3,
		GapBottom:        0,
		GapTop:           2,
		MinLoan:          150,
		MaxLoan:          1000,
		OrderLimit:       10,
		TestMode:         true,
		LoanPeriodThresholds: map[int]float64{
			30: 0.03,
			60: 0.035,
		},
		HighHoldRate:             0.05,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		FundingBookRateUndercut:  0,
		RateRangeIncreasePercent: 0.2,
		RateBonus:                0.02,
	}

	return NewLendingBot(cfg, nil)
}

func TestReplayStrategy_RejectsKlineStrategy(t *testing.T) {
	bot := newReplayTestBot(config.StrategyKline)

	_, err := bot.ReplayStrategy(ReplayInput{
		Strategy:       config.StrategyKline,
		FundsAvailable: 500,
	})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected kline replay unsupported error, got %v", err)
	}
}

func TestReplayStrategy_NormalizesFundingBookAndBuildsSummary(t *testing.T) {
	bot := newReplayTestBot(config.StrategyTraditional)

	result, err := bot.ReplayStrategy(ReplayInput{
		Strategy:         config.StrategyTraditional,
		FundsAvailable:   450,
		HasPendingOrders: false,
		FundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0005, Amount: 500},
			{Rate: 0.00025, Amount: -1000},
			{Rate: 0.0006, Amount: 800},
			{Rate: 0.00026, Amount: -1200},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil || result.DecisionSummary == nil {
		t.Fatal("expected replay output with decision summary")
	}
	if result.DecisionSummary.FundingBookEntries != 2 {
		t.Fatalf("expected normalized funding book entries 2, got %d", result.DecisionSummary.FundingBookEntries)
	}
	if result.DecisionSummary.FundingBookSource != "replay_input" {
		t.Fatalf("expected replay book source, got %q", result.DecisionSummary.FundingBookSource)
	}
	if len(result.Offers) == 0 {
		t.Fatal("expected replay offers")
	}
	for i, offer := range result.Offers {
		if offer.Rate < 0.0003 {
			t.Fatalf("expected replay offer %d rate >= 0.0003, got %.8f", i, offer.Rate)
		}
	}
}

func TestReplayStrategy_SupportsSimpleAndSmartStrategies(t *testing.T) {
	tests := []string{
		config.StrategySimple,
		config.StrategySmart,
	}

	for _, strategyName := range tests {
		bot := newReplayTestBot(strategyName)

		result, err := bot.ReplayStrategy(ReplayInput{
			Strategy:         strategyName,
			FundsAvailable:   600,
			HasPendingOrders: true,
			FundingBook: []*bitfinex.FundingBookEntry{
				{Rate: 0.0003, Amount: 1000},
				{Rate: 0.0004, Amount: 1500},
				{Rate: 0.0005, Amount: 1800},
			},
		})
		if err != nil {
			t.Fatalf("strategy %s expected no error, got %v", strategyName, err)
		}
		if result == nil || result.DecisionSummary == nil {
			t.Fatalf("strategy %s expected replay result", strategyName)
		}
		if result.DecisionSummary.Strategy != strategyName {
			t.Fatalf("strategy %s expected summary strategy %q, got %q", strategyName, strategyName, result.DecisionSummary.Strategy)
		}
		if len(result.Offers) == 0 {
			t.Fatalf("strategy %s expected offers", strategyName)
		}
		if result.DecisionSummary.SuccessfulOfferCount != len(result.Offers) {
			t.Fatalf("strategy %s expected successful offers %d, got %d", strategyName, len(result.Offers), result.DecisionSummary.SuccessfulOfferCount)
		}
	}
}

func TestReplayStrategy_ReturnsEmptyWhenFundsBelowMinLoan(t *testing.T) {
	bot := newReplayTestBot(config.StrategyTraditional)

	result, err := bot.ReplayStrategy(ReplayInput{
		FundsAvailable: 100,
		FundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0003, Amount: 1000},
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil || result.DecisionSummary == nil {
		t.Fatal("expected replay output with summary")
	}
	if len(result.Offers) != 0 {
		t.Fatalf("expected no offers, got %d", len(result.Offers))
	}
	if !strings.Contains(strings.Join(result.DecisionSummary.Notes, " "), "低于最小下单金额") {
		t.Fatalf("expected insufficient funds note, got %+v", result.DecisionSummary.Notes)
	}
}
