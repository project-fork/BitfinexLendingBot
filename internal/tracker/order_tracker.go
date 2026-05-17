package tracker

import (
	"sync"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/storage"
)

// BotOrderTracker tracks orders created by the bot.
type BotOrderTracker struct {
	mu            sync.RWMutex
	createdOrders map[int64]time.Time // orderID -> created time
	botStartTime  time.Time
	dataFilePath  string
}

// NewBotOrderTracker creates a tracker backed by data.json next to the binary.
func NewBotOrderTracker() *BotOrderTracker {
	return NewBotOrderTrackerWithDataFile(storage.DefaultDataFilePath())
}

// NewBotOrderTrackerWithDataFile creates a tracker backed by the given data file.
func NewBotOrderTrackerWithDataFile(dataFilePath string) *BotOrderTracker {
	t := &BotOrderTracker{
		createdOrders: make(map[int64]time.Time),
		botStartTime:  time.Now(),
		dataFilePath:  dataFilePath,
	}
	t.load()
	return t
}

// TrackOrder records an order created by the bot.
func (t *BotOrderTracker) TrackOrder(orderID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.createdOrders[orderID] = time.Now()
	t.saveLocked()
}

// IsTrackedOrder checks whether an order was created by the bot.
func (t *BotOrderTracker) IsTrackedOrder(orderID int64) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()

	_, exists := t.createdOrders[orderID]
	return exists
}

// RemoveOrder removes a tracked order after it completes or is cancelled.
func (t *BotOrderTracker) RemoveOrder(orderID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.createdOrders, orderID)
	t.saveLocked()
}

// GetTrackedOrders returns all tracked order IDs.
func (t *BotOrderTracker) GetTrackedOrders() []int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	orders := make([]int64, 0, len(t.createdOrders))
	for orderID := range t.createdOrders {
		orders = append(orders, orderID)
	}
	return orders
}

// CleanOldOrders removes stale tracking records to prevent unbounded growth.
func (t *BotOrderTracker) CleanOldOrders(maxAge time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	changed := false
	now := time.Now()
	for orderID, createdTime := range t.createdOrders {
		if now.Sub(createdTime) > maxAge {
			delete(t.createdOrders, orderID)
			changed = true
		}
	}
	if changed {
		t.saveLocked()
	}
}

// GetOrderCount returns the number of tracked orders.
func (t *BotOrderTracker) GetOrderCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.createdOrders)
}

// GetBotStartTime returns the bot start time.
func (t *BotOrderTracker) GetBotStartTime() time.Time {
	return t.botStartTime
}

func (t *BotOrderTracker) load() {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := storage.LoadData(t.dataFilePath)
	if state.TrackedOrders == nil {
		return
	}
	t.createdOrders = state.TrackedOrders
}

func (t *BotOrderTracker) saveLocked() {
	if t.dataFilePath == "" {
		return
	}

	state := storage.LoadData(t.dataFilePath)
	state.TrackedOrders = t.createdOrders
	storage.SaveData(t.dataFilePath, state)
}
