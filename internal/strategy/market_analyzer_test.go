package strategy

import (
	"math"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestSelectFundingBookEntriesByRange_UsesConfiguredGapIndexes(t *testing.T) {
	cfg := &config.Config{
		GapBottom: 1,
		GapTop:    3,
	}
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00020},
		{Rate: 0.00021},
		{Rate: 0.00022},
		{Rate: 0.00023},
		{Rate: 0.00024},
	}

	selected := selectFundingBookEntriesByRange(cfg, fundingBook)
	if len(selected) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(selected))
	}
	expectedRates := []float64{0.00021, 0.00022, 0.00023}
	for i, expected := range expectedRates {
		if math.Abs(selected[i].Rate-expected) > floatTolerance {
			t.Fatalf("expected rate %.8f at %d, got %.8f", expected, i, selected[i].Rate)
		}
	}
}

func TestMarketAnalyzer_AnalyzeCompetition_UsesProvidedView(t *testing.T) {
	analyzer := NewMarketAnalyzer()
	view := []*bitfinex.FundingBookEntry{
		{Rate: 0.00050},
		{Rate: 0.00060},
		{Rate: 0.00070},
		{Rate: 0.00080},
		{Rate: 0.00090},
		{Rate: 0.00100},
		{Rate: 0.00110},
		{Rate: 0.00120},
		{Rate: 0.00130},
		{Rate: 0.00140},
	}

	got := analyzer.AnalyzeCompetition(view)
	expected := 0.00053
	if math.Abs(got-expected) > floatTolerance {
		t.Fatalf("expected competitive rate %.8f, got %.8f", expected, got)
	}
}
