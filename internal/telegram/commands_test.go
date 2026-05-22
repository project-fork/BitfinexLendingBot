package telegram

import (
	"encoding/json"
	"testing"
)

func TestBuildTelegramCommands_IncludesSupportedCommands(t *testing.T) {
	commands := buildTelegramCommands()

	required := map[string]string{
		"help":           "帮助 | 显示帮助消息",
		"status":         "查询 | 显示系统状态",
		"strategy":       "查询 | 显示当前策略",
		"lending":        "查询 | 查看活跃借贷",
		"offers":         "查询 | 查看未成交订单",
		"simplestrategy": "策略 | 切换简单策略",
		"canceloffers":   "控制 | 取消未成交订单",
		"restart":        "控制 | 取消追踪订单后重跑",
		"run":            "控制 | 保留订单直接重跑",
	}

	if len(commands) < len(required) {
		t.Fatalf("expected at least %d commands, got %d", len(required), len(commands))
	}

	index := make(map[string]string, len(commands))
	for _, command := range commands {
		index[command.Command] = command.Description
	}

	for name, description := range required {
		if got, ok := index[name]; !ok {
			t.Fatalf("expected command %q to be registered", name)
		} else if got != description {
			t.Fatalf("expected description %q for %q, got %q", description, name, got)
		}
	}
}

func TestBuildTelegramCommands_UsesGroupedOrdering(t *testing.T) {
	commands := buildTelegramCommands()

	expectedPrefix := []string{
		"help",
		"start",
		"rate",
		"check",
		"status",
		"strategy",
		"lending",
		"offers",
	}

	if len(commands) < len(expectedPrefix) {
		t.Fatalf("expected at least %d commands, got %d", len(expectedPrefix), len(commands))
	}

	for i, command := range expectedPrefix {
		if commands[i].Command != command {
			t.Fatalf("expected command %q at position %d, got %q", command, i, commands[i].Command)
		}
	}
}

func TestBuildSetMyCommandsParams_EncodesCommandsPayload(t *testing.T) {
	params, err := buildSetMyCommandsParams()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	rawCommands := params.Get("commands")
	if rawCommands == "" {
		t.Fatal("expected commands payload to be present")
	}

	var commands []telegramCommand
	if err := json.Unmarshal([]byte(rawCommands), &commands); err != nil {
		t.Fatalf("expected valid JSON commands payload, got %v", err)
	}

	if len(commands) == 0 {
		t.Fatal("expected non-empty commands payload")
	}
}

func TestBuildSetMyCommandsParams_ReturnsCommandPayload(t *testing.T) {
	params, err := buildSetMyCommandsParams()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if params.Get("commands") == "" {
		t.Fatal("expected commands payload")
	}
}
