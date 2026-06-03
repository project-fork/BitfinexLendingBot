package telegram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestBotPersistsAuthenticatedChatID(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")

	bot := NewBotWithDataFileForTest(dataFile)
	bot.setAuthenticated(123456789)

	reloaded := NewBotWithDataFileForTest(dataFile)
	if got := reloaded.GetAuthenticatedChatID(); got != 123456789 {
		t.Fatalf("expected authenticated chat ID to be restored, got %d", got)
	}
}

func TestBotPersistsAuthenticatedChatIDWithoutRemovingTrackedOrders(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")
	initial := map[string]any{
		"tracked_orders": map[string]string{
			"4944501971": "2026-05-17T17:30:12+08:00",
		},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("failed to marshal initial data: %v", err)
	}
	if err := os.WriteFile(dataFile, data, 0600); err != nil {
		t.Fatalf("failed to write initial data: %v", err)
	}

	bot := NewBotWithDataFileForTest(dataFile)
	bot.setAuthenticated(123456789)

	updated, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatalf("failed to read updated data: %v", err)
	}
	var parsed struct {
		TrackedOrders map[string]string `json:"tracked_orders"`
		Telegram      struct {
			AuthenticatedChatID int64 `json:"authenticated_chat_id"`
		} `json:"telegram"`
	}
	if err := json.Unmarshal(updated, &parsed); err != nil {
		t.Fatalf("failed to parse updated data: %v", err)
	}
	if _, exists := parsed.TrackedOrders["4944501971"]; !exists {
		t.Fatal("expected existing tracked order to remain after saving telegram auth")
	}
	if parsed.Telegram.AuthenticatedChatID != 123456789 {
		t.Fatalf("expected authenticated chat ID to be saved, got %d", parsed.Telegram.AuthenticatedChatID)
	}
}

func TestBotPersistsRuntimeConfigAndRestoresOnReload(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")
	cfg := &config.Config{
		Currency:                 "USD",
		MinLoan:                  150,
		MaxLoan:                  500,
		HighHoldRate:             0.05,
		HighHoldOrders:           1,
		MinDailyLendRate:         0.02,
		RateRangeIncreasePercent: 0.2,
		KlineSmoothMethod:        "ema",
	}

	bot := &Bot{
		config:        cfg,
		runtimeConfig: config.NewRuntimeConfigService(cfg),
		dataFilePath:  dataFile,
	}

	if err := bot.updateRuntimeConfig(func(runtimeConfig *config.RuntimeConfigService) error {
		if err := runtimeConfig.SetNotifyRateThreshold(0.045); err != nil {
			return err
		}
		if err := runtimeConfig.SetReserveAmount(100); err != nil {
			return err
		}
		if err := runtimeConfig.SetLoanDays(0); err != nil {
			return err
		}
		if err := runtimeConfig.SetMinDailyLendRate("FRR"); err != nil {
			return err
		}
		if err := runtimeConfig.SetHighHoldAmount(2000); err != nil {
			return err
		}
		if err := runtimeConfig.SetRateRangeIncreasePercent(0.35); err != nil {
			return err
		}
		if err := runtimeConfig.SetStrategy(config.StrategySmart); err != nil {
			return err
		}
		return runtimeConfig.SetKlineSmoothMethod("p90")
	}); err != nil {
		t.Fatalf("expected runtime config to persist, got error: %v", err)
	}

	reloadedCfg := &config.Config{
		Currency:                 "USD",
		MinLoan:                  150,
		MaxLoan:                  500,
		HighHoldRate:             0.05,
		HighHoldOrders:           1,
		MinDailyLendRate:         0.02,
		RateRangeIncreasePercent: 0.2,
		KlineSmoothMethod:        "ema",
	}
	reloaded := &Bot{
		config:        reloadedCfg,
		runtimeConfig: config.NewRuntimeConfigService(reloadedCfg),
		dataFilePath:  dataFile,
	}
	reloaded.loadPersistentData()

	if got := reloaded.config.NotifyRateThreshold; got != 0.045 {
		t.Fatalf("expected notify threshold 0.045, got %f", got)
	}
	if got := reloaded.config.ReserveAmount; got != 100 {
		t.Fatalf("expected reserve amount 100, got %f", got)
	}
	if got := reloaded.config.GetMinDailyRateDisplay(); got != "FRR" {
		t.Fatalf("expected min daily lend rate FRR, got %q", got)
	}
	if got := reloaded.config.HighHoldAmount; got != 2000 {
		t.Fatalf("expected high hold amount 2000, got %f", got)
	}
	if got := reloaded.config.RateRangeIncreasePercent; got != 0.35 {
		t.Fatalf("expected rate range increase 0.35, got %f", got)
	}
	if got := reloaded.config.GetStrategy(); got != config.StrategySmart {
		t.Fatalf("expected strategy %q, got %q", config.StrategySmart, got)
	}
	if got := reloaded.config.KlineSmoothMethod; got != "p90" {
		t.Fatalf("expected kline smooth method p90, got %q", got)
	}
}

func TestBotPersistsMinDailyLendRateAsRawValue(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")
	cfg := &config.Config{
		Currency:         "USD",
		MinLoan:          150,
		MaxLoan:          500,
		HighHoldRate:     0.05,
		HighHoldOrders:   1,
		MinDailyLendRate: 0.02,
	}

	bot := &Bot{
		config:        cfg,
		runtimeConfig: config.NewRuntimeConfigService(cfg),
		dataFilePath:  dataFile,
	}

	if err := bot.updateRuntimeConfig(func(runtimeConfig *config.RuntimeConfigService) error {
		return runtimeConfig.SetMinDailyLendRate(0.031)
	}); err != nil {
		t.Fatalf("expected runtime config to persist, got error: %v", err)
	}

	updated, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatalf("failed to read updated data: %v", err)
	}
	var parsed struct {
		RuntimeConfig struct {
			MinDailyLendRate *string `json:"min_daily_lend_rate"`
		} `json:"runtime_config"`
	}
	if err := json.Unmarshal(updated, &parsed); err != nil {
		t.Fatalf("failed to parse updated data: %v", err)
	}
	if parsed.RuntimeConfig.MinDailyLendRate == nil {
		t.Fatal("expected min_daily_lend_rate to be persisted")
	}
	if got := *parsed.RuntimeConfig.MinDailyLendRate; got != "0.0310" {
		t.Fatalf("expected min_daily_lend_rate to persist raw value 0.0310, got %q", got)
	}
}

func TestBotLoadsLegacyPercentMinDailyLendRate(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")
	initial := map[string]any{
		"runtime_config": map[string]any{
			"min_daily_lend_rate": "0.0310%",
		},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("failed to marshal initial data: %v", err)
	}
	if err := os.WriteFile(dataFile, data, 0600); err != nil {
		t.Fatalf("failed to write initial data: %v", err)
	}

	cfg := &config.Config{
		Currency:         "USD",
		MinLoan:          150,
		MaxLoan:          500,
		HighHoldRate:     0.05,
		HighHoldOrders:   1,
		MinDailyLendRate: 0.02,
	}
	bot := &Bot{
		config:        cfg,
		runtimeConfig: config.NewRuntimeConfigService(cfg),
		dataFilePath:  dataFile,
	}

	bot.loadPersistentData()

	if got := bot.config.GetMinDailyRateDisplay(); got != "0.0310%" {
		t.Fatalf("expected legacy percent min daily rate to restore as 0.0310%%, got %q", got)
	}
}

func TestBotPersistsRuntimeConfigWithoutRemovingTrackedOrdersOrAuthChatID(t *testing.T) {
	dataFile := filepath.Join(t.TempDir(), "data.json")
	initial := map[string]any{
		"tracked_orders": map[string]string{
			"4944501971": "2026-05-17T17:30:12+08:00",
		},
		"telegram": map[string]any{
			"authenticated_chat_id": 123456789,
		},
	}
	data, err := json.Marshal(initial)
	if err != nil {
		t.Fatalf("failed to marshal initial data: %v", err)
	}
	if err := os.WriteFile(dataFile, data, 0600); err != nil {
		t.Fatalf("failed to write initial data: %v", err)
	}

	cfg := &config.Config{
		Currency:                 "USD",
		MinLoan:                  150,
		MaxLoan:                  500,
		HighHoldRate:             0.05,
		HighHoldOrders:           1,
		MinDailyLendRate:         0.02,
		RateRangeIncreasePercent: 0.2,
		KlineSmoothMethod:        "ema",
	}
	bot := &Bot{
		config:        cfg,
		runtimeConfig: config.NewRuntimeConfigService(cfg),
		dataFilePath:  dataFile,
	}

	if err := bot.updateRuntimeConfig(func(runtimeConfig *config.RuntimeConfigService) error {
		return runtimeConfig.SetOrderLimit(7)
	}); err != nil {
		t.Fatalf("expected runtime config to persist, got error: %v", err)
	}

	updated, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatalf("failed to read updated data: %v", err)
	}
	var parsed struct {
		TrackedOrders map[string]string `json:"tracked_orders"`
		Telegram      struct {
			AuthenticatedChatID int64 `json:"authenticated_chat_id"`
		} `json:"telegram"`
		RuntimeConfig struct {
			OrderLimit *int `json:"order_limit"`
		} `json:"runtime_config"`
	}
	if err := json.Unmarshal(updated, &parsed); err != nil {
		t.Fatalf("failed to parse updated data: %v", err)
	}
	if _, exists := parsed.TrackedOrders["4944501971"]; !exists {
		t.Fatal("expected existing tracked order to remain after saving runtime config")
	}
	if parsed.Telegram.AuthenticatedChatID != 123456789 {
		t.Fatalf("expected authenticated chat ID to remain, got %d", parsed.Telegram.AuthenticatedChatID)
	}
	if parsed.RuntimeConfig.OrderLimit == nil || *parsed.RuntimeConfig.OrderLimit != 7 {
		t.Fatalf("expected runtime config order limit 7, got %#v", parsed.RuntimeConfig.OrderLimit)
	}
}
