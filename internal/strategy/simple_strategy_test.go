package strategy

import (
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestSimpleStrategy_CalculateOffers_UsesAvailableBalanceForHighHold(t *testing.T) {
	cfg := &config.Config{
		MinLoan:                       150.0,
		MaxLoan:                       1000.0,
		SpreadLend:                    3,
		HighHoldAmount:                500.0,
		HighHoldOrders:                1,
		HighHoldRate:                  0.1,
		MinDailyLendRate:              0.02,
		ThirtyDayLendRateThreshold:    0.04,
		OneTwentyDayLendRateThreshold: 0.045,
		EnableSimpleStrategy:          true,
		VolatilityThreshold:           0.002,
		MaxRateMultiplier:             2.0,
		MinRateMultiplier:             0.8,
	}

	strategy := NewSimpleStrategy(cfg)
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.0003, Amount: 1000},
		{Rate: 0.0004, Amount: 2000},
	}

	offers := strategy.CalculateOffers(400.0, fundingBook)
	if len(offers) != 1 {
		t.Fatalf("expected 1 offer, got %d", len(offers))
	}
	if offers[0].Amount != 400.0 {
		t.Fatalf("expected high hold amount 400.00, got %.2f", offers[0].Amount)
	}
}

func TestSimpleStrategy_CalculateOffers_RespectsOrderLimitAfterHighHold(t *testing.T) {
	cfg := &config.Config{
		MinLoan:                       150.0,
		MaxLoan:                       300.0,
		SpreadLend:                    15,
		OrderLimit:                    4,
		HighHoldAmount:                300.0,
		HighHoldOrders:                1,
		HighHoldRate:                  0.1,
		MinDailyLendRate:              0.02,
		ThirtyDayLendRateThreshold:    0.04,
		OneTwentyDayLendRateThreshold: 0.045,
		EnableSimpleStrategy:          true,
		VolatilityThreshold:           0.002,
		MaxRateMultiplier:             2.0,
		MinRateMultiplier:             0.8,
	}

	strategy := NewSimpleStrategy(cfg)
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.0003, Amount: 1000},
		{Rate: 0.0004, Amount: 2000},
		{Rate: 0.0005, Amount: 1500},
	}

	offers := strategy.CalculateOffers(1097.0, fundingBook)
	if len(offers) != 4 {
		t.Fatalf("expected 4 offers, got %d", len(offers))
	}

	expectedAmounts := []float64{300.0, 265.67, 265.67, 265.66}
	for i, expected := range expectedAmounts {
		if offers[i].Amount != expected {
			t.Fatalf("expected offer %d amount %.2f, got %.2f", i, expected, offers[i].Amount)
		}
	}
}
