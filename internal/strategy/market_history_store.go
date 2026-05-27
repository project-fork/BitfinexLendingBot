package strategy

import (
	"log"
	"os"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/storage"
)

type marketHistoryStore struct {
	dataFilePath string
	logger       *log.Logger
}

func newMarketHistoryStore(dataFilePath string, logger *log.Logger) *marketHistoryStore {
	return &marketHistoryStore{
		dataFilePath: dataFilePath,
		logger:       logger,
	}
}

func (s *marketHistoryStore) load(fundingSymbol string, analyzer *MarketAnalyzer, now time.Time) {
	if s == nil || s.dataFilePath == "" || analyzer == nil {
		return
	}

	state := storage.LoadData(s.dataFilePath)
	history := state.MarketHistory
	if history.FundingSymbol == "" || history.FundingSymbol != fundingSymbol {
		return
	}

	snapshots := make([]RateSnapshot, 0, len(history.Snapshots))
	for _, snapshot := range history.Snapshots {
		snapshots = append(snapshots, RateSnapshot{
			Rate:      snapshot.Rate,
			Volume:    snapshot.Volume,
			Timestamp: snapshot.Timestamp,
		})
	}
	analyzer.RestoreRateHistory(snapshots, now)
}

func (s *marketHistoryStore) save(fundingSymbol string, analyzer *MarketAnalyzer) {
	if s == nil || s.dataFilePath == "" || analyzer == nil {
		return
	}

	snapshots := analyzer.ExportRateHistory()
	persisted := make([]storage.MarketSnapshotData, 0, len(snapshots))
	for _, snapshot := range snapshots {
		persisted = append(persisted, storage.MarketSnapshotData{
			Rate:      snapshot.Rate,
			Volume:    snapshot.Volume,
			Timestamp: snapshot.Timestamp,
		})
	}

	if err := storage.UpdateData(s.dataFilePath, func(state *storage.Data) {
		state.MarketHistory = storage.MarketHistoryData{
			FundingSymbol: fundingSymbol,
			Snapshots:     persisted,
		}
	}); err != nil && s.logger != nil {
		s.logger.Printf("保存市场历史失败: %v", err)
	}
}

func persistSharedMarketHistory(cfg *config.Config, analyzer *MarketAnalyzer) {
	if cfg == nil || analyzer == nil {
		return
	}

	store := newMarketHistoryStore(storage.DefaultDataFilePath(), log.New(os.Stderr, "", log.LstdFlags))
	store.save(cfg.GetFundingSymbol(), analyzer)
}
