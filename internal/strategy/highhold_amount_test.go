package strategy

import (
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestCalculateHighHoldOffers_UsesAvailableBalanceWhenBelowThreshold(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			MinLoan:        150.0,
			MaxLoan:        1000.0,
			HighHoldAmount: 500.0,
			HighHoldOrders: 1,
			HighHoldRate:   0.1,
		},
	}

	fundsAvailable := 400.0
	offers := bot.calculateHighHoldOffers(&fundsAvailable)

	if len(offers) != 1 {
		t.Fatalf("expected 1 high hold offer, got %d", len(offers))
	}
	if offers[0].Amount != 400.0 {
		t.Fatalf("expected offer amount 400.00, got %.2f", offers[0].Amount)
	}
	if fundsAvailable != 0 {
		t.Fatalf("expected remaining funds 0.00, got %.2f", fundsAvailable)
	}
}
