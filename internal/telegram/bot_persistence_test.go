package telegram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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
