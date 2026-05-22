package strategy

import (
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestLendingBot_CalculatePeriod_UsesConfiguredIntermediatePeriods(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			MinDailyLendRate: 0.02,
			LoanPeriodThresholds: map[int]float64{
				30:  0.03,
				60:  0.035,
				90:  0.04,
				120: 0.05,
			},
		},
	}

	tests := []struct {
		name      string
		dailyRate float64
		expected  int
	}{
		{name: "below thirty day threshold uses default period", dailyRate: 0.00029, expected: 2},
		{name: "thirty day threshold", dailyRate: 0.00030, expected: 30},
		{name: "sixty day threshold", dailyRate: 0.00035, expected: 60},
		{name: "ninety day threshold", dailyRate: 0.00040, expected: 90},
		{name: "one twenty day threshold", dailyRate: 0.00050, expected: 120},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			period := bot.calculatePeriod(tt.dailyRate)
			if period != tt.expected {
				t.Fatalf("expected period %d, got %d", tt.expected, period)
			}
		})
	}
}
