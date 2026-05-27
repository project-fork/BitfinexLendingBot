package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDataFilePersistsDailyEarningsState(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, DataFileName)
	now := time.Date(2026, 5, 27, 9, 35, 0, 0, time.FixedZone("CST", 8*3600))

	if err := UpdateData(dataFile, func(state *Data) {
		state.DailyEarnings.LastReportDate = "2026-05-27"
		state.DailyEarnings.LastReportAt = now
	}); err != nil {
		t.Fatalf("failed to persist daily earnings state: %v", err)
	}

	state := LoadData(dataFile)
	if state.DailyEarnings.LastReportDate != "2026-05-27" {
		t.Fatalf("expected last_report_date to round-trip, got %q", state.DailyEarnings.LastReportDate)
	}
	if !state.DailyEarnings.LastReportAt.Equal(now) {
		t.Fatalf("expected last_report_at to round-trip, got %v", state.DailyEarnings.LastReportAt)
	}
}
