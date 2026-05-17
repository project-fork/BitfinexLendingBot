package strategy

import (
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
)

func TestFindNewCreditsDetectsUnseenIDEvenWithOldTimestamp(t *testing.T) {
	seen := map[int64]struct{}{
		1001: {},
	}
	credits := []*bitfinex.FundingCredit{
		{ID: 1001, MTSOpened: 900},
		{ID: 1002, MTSOpened: 900},
	}

	newCredits := findNewCredits(credits, seen, 1000)

	if len(newCredits) != 1 {
		t.Fatalf("expected 1 new credit, got %d", len(newCredits))
	}
	if newCredits[0].ID != 1002 {
		t.Fatalf("expected unseen credit ID 1002, got %d", newCredits[0].ID)
	}
}

func TestFindNewCreditsIgnoresAlreadySeenCredit(t *testing.T) {
	seen := map[int64]struct{}{
		1001: {},
	}
	credits := []*bitfinex.FundingCredit{
		{ID: 1001, MTSOpened: 1200},
	}

	newCredits := findNewCredits(credits, seen, 1000)

	if len(newCredits) != 0 {
		t.Fatalf("expected no new credits, got %d", len(newCredits))
	}
}
