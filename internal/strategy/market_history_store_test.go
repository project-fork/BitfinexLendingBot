package strategy

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/storage"
)

func TestMarketHistoryStore_SaveAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, storage.DataFileName)
	logger := log.New(os.Stderr, "", 0)
	store := newMarketHistoryStore(dataFile, logger)
	analyzer := NewMarketAnalyzer()
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)

	analyzer.RestoreRateHistory([]RateSnapshot{
		{Rate: 0.00031, Volume: 100, Timestamp: now.Add(-2 * time.Minute)},
		{Rate: 0.00032, Volume: 101, Timestamp: now.Add(-1 * time.Minute)},
	}, now)

	store.save("fUSD", analyzer)

	state := storage.LoadData(dataFile)
	if state.MarketHistory.FundingSymbol != "fUSD" {
		t.Fatalf("expected funding symbol fUSD, got %q", state.MarketHistory.FundingSymbol)
	}
	if len(state.MarketHistory.Snapshots) != 2 {
		t.Fatalf("expected 2 persisted snapshots, got %d", len(state.MarketHistory.Snapshots))
	}

	restored := NewMarketAnalyzer()
	store.load("fUSD", restored, now)
	history := restored.ExportRateHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 restored snapshots, got %d", len(history))
	}
	if history[0].Rate != 0.00031 || history[1].Rate != 0.00032 {
		t.Fatalf("unexpected restored rates: %#v", history)
	}
}

func TestMarketHistoryStore_LoadSkipsMismatchedFundingSymbol(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, storage.DataFileName)
	if err := storage.UpdateData(dataFile, func(state *storage.Data) {
		state.MarketHistory = storage.MarketHistoryData{
			FundingSymbol: "fBTC",
			Snapshots: []storage.MarketSnapshotData{
				{Rate: 0.00031, Volume: 100, Timestamp: time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)},
			},
		}
	}); err != nil {
		t.Fatalf("failed to seed data file: %v", err)
	}

	store := newMarketHistoryStore(dataFile, log.New(os.Stderr, "", 0))
	restored := NewMarketAnalyzer()
	store.load("fUSD", restored, time.Date(2026, 5, 27, 12, 1, 0, 0, time.UTC))
	if len(restored.ExportRateHistory()) != 0 {
		t.Fatalf("expected no restored history when funding symbol mismatches")
	}
}

func TestLendingBot_RestoresSharedMarketHistoryOnStartup(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, storage.DataFileName)
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	if err := storage.UpdateData(dataFile, func(state *storage.Data) {
		state.MarketHistory = storage.MarketHistoryData{
			FundingSymbol: "fUSD",
			Snapshots: []storage.MarketSnapshotData{
				{Rate: 0.00021, Volume: 80, Timestamp: now.Add(-2 * time.Minute)},
				{Rate: 0.00022, Volume: 82, Timestamp: now.Add(-1 * time.Minute)},
			},
		}
	}); err != nil {
		t.Fatalf("failed to seed data file: %v", err)
	}

	cfg := &config.Config{Currency: "usd"}
	analyzer := NewMarketAnalyzer()
	bot := &LendingBot{
		config:             cfg,
		logger:             log.New(os.Stderr, "", 0),
		marketAnalyzer:     analyzer,
		simpleStrategy:     NewSimpleStrategyWithAnalyzer(cfg, analyzer),
		smartStrategy:      NewSmartStrategyWithAnalyzer(cfg, analyzer),
		marketHistoryStore: newMarketHistoryStore(dataFile, log.New(os.Stderr, "", 0)),
	}

	bot.restoreMarketHistory(now)

	history := analyzer.ExportRateHistory()
	if len(history) != 2 {
		t.Fatalf("expected restored analyzer history length 2, got %d", len(history))
	}
	if len(bot.simpleStrategy.analyzer.ExportRateHistory()) != 2 {
		t.Fatalf("expected simple strategy to share restored analyzer history")
	}
	if len(bot.smartStrategy.analyzer.ExportRateHistory()) != 2 {
		t.Fatalf("expected smart strategy to share restored analyzer history")
	}
}
