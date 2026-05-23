package telegram

import (
	"strings"
	"testing"
)

func TestHandleConfigSummary_SendsSummaryText(t *testing.T) {
	lb := &stubLendingBot{
		configSummaryText: "⚙️ 运行配置摘要\n策略: smart",
	}
	bot, messages, _ := newTestBotWithMessages(lb)

	bot.handleConfigSummary(1)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0] != lb.configSummaryText {
		t.Fatalf("expected config summary %q, got %q", lb.configSummaryText, (*messages)[0])
	}
}

func TestHandleDecisionSummary_SendsSummaryText(t *testing.T) {
	lb := &stubLendingBot{
		decisionSummaryText: "📘 最近一次策略决策摘要\n策略: traditional",
	}
	bot, messages, _ := newTestBotWithMessages(lb)

	bot.handleDecisionSummary(1)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	if (*messages)[0] != lb.decisionSummaryText {
		t.Fatalf("expected decision summary %q, got %q", lb.decisionSummaryText, (*messages)[0])
	}
}

func TestHandleHelp_IncludesDiagnosticCommands(t *testing.T) {
	bot, messages, _ := newTestBotWithMessages(&stubLendingBot{})

	bot.handleHelp(1)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}

	helpText := (*messages)[0]
	for _, fragment := range []string{"/configsummary", "/decisionsummary"} {
		if !strings.Contains(helpText, fragment) {
			t.Fatalf("expected help text to contain %q, got:\n%s", fragment, helpText)
		}
	}
}
