package tracker

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBotOrderTrackerPersistsTrackedOrders(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")

	tracker := NewBotOrderTrackerWithDataFile(dataFile)
	tracker.TrackOrder(12345)

	reloaded := NewBotOrderTrackerWithDataFile(dataFile)
	if !reloaded.IsTrackedOrder(12345) {
		t.Fatal("expected tracked order to be loaded from data file")
	}
}

func TestBotOrderTrackerPersistsRemovedOrders(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")

	tracker := NewBotOrderTrackerWithDataFile(dataFile)
	tracker.TrackOrder(12345)
	tracker.RemoveOrder(12345)

	reloaded := NewBotOrderTrackerWithDataFile(dataFile)
	if reloaded.IsTrackedOrder(12345) {
		t.Fatal("expected removed order to be absent after reloading data file")
	}
}

func TestBotOrderTrackerStartsEmptyWhenDataFileMissing(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")

	tracker := NewBotOrderTrackerWithDataFile(dataFile)

	if tracker.GetOrderCount() != 0 {
		t.Fatalf("expected empty tracker, got %d orders", tracker.GetOrderCount())
	}
}

func TestBotOrderTrackerPersistsCleanedOldOrders(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")

	tracker := NewBotOrderTrackerWithDataFile(dataFile)
	tracker.createdOrders[12345] = time.Now().Add(-48 * time.Hour)
	tracker.createdOrders[67890] = time.Now()
	tracker.CleanOldOrders(24 * time.Hour)

	reloaded := NewBotOrderTrackerWithDataFile(dataFile)
	if reloaded.IsTrackedOrder(12345) {
		t.Fatal("expected old order to be removed from data file")
	}
	if !reloaded.IsTrackedOrder(67890) {
		t.Fatal("expected fresh order to remain in data file")
	}
}

func TestDefaultOrderTrackerDataFileName(t *testing.T) {
	t.Setenv("BITFINEX_LENDING_BOT_DATA_FILE", "")

	tracker := NewBotOrderTracker()
	if filepath.Base(tracker.dataFilePath) != "data.json" {
		t.Fatalf("expected default data file name data.json, got %s", tracker.dataFilePath)
	}
}

func TestBotOrderTrackerIgnoresCorruptDataFile(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")
	if err := os.WriteFile(dataFile, []byte("{not-json"), 0600); err != nil {
		t.Fatalf("failed to write corrupt data file: %v", err)
	}

	tracker := NewBotOrderTrackerWithDataFile(dataFile)

	if tracker.GetOrderCount() != 0 {
		t.Fatalf("expected corrupt data file to load as empty tracker, got %d orders", tracker.GetOrderCount())
	}
}
