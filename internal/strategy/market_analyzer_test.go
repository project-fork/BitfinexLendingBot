package strategy

import (
	"math"
	"testing"
	"time"

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

func TestMarketAnalyzer_RestoreRateHistory_FiltersInvalidAndClampsToMaxSize(t *testing.T) {
	analyzer := NewMarketAnalyzer()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	snapshots := make([]RateSnapshot, 0, 55)
	snapshots = append(snapshots,
		RateSnapshot{Rate: -1, Volume: 10, Timestamp: now.Add(-time.Hour)},
		RateSnapshot{Rate: 0.0003, Volume: 10, Timestamp: time.Time{}},
		RateSnapshot{Rate: 0.0003, Volume: 10, Timestamp: now.Add(2 * time.Minute)},
	)
	for i := 0; i < 52; i++ {
		snapshots = append(snapshots, RateSnapshot{
			Rate:      0.0001 + float64(i)*0.000001,
			Volume:    float64(i + 1),
			Timestamp: now.Add(time.Duration(-52+i) * time.Minute),
		})
	}

	analyzer.RestoreRateHistory(snapshots, now)

	history := analyzer.ExportRateHistory()
	if len(history) != 48 {
		t.Fatalf("expected 48 snapshots after restore, got %d", len(history))
	}
	if history[0].Timestamp != now.Add(-48*time.Minute) {
		t.Fatalf("expected oldest retained snapshot at %s, got %s", now.Add(-48*time.Minute), history[0].Timestamp)
	}
	if history[len(history)-1].Timestamp != now.Add(-1*time.Minute) {
		t.Fatalf("expected newest retained snapshot at %s, got %s", now.Add(-1*time.Minute), history[len(history)-1].Timestamp)
	}
}
