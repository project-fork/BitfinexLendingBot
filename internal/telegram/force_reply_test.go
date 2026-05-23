package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

func newForceReplyTestBot() (*Bot, *[]string, *[]tgbotapi.Chattable) {
	messages := []string{}
	chattables := []tgbotapi.Chattable{}
	cfg := &config.Config{
		Currency:         "USD",
		MinLoan:          150,
		MaxLoan:          0,
		HighHoldOrders:   1,
		MinDailyLendRate: 0.02,
	}

	bot := &Bot{
		config:        cfg,
		runtimeConfig: config.NewRuntimeConfigService(cfg),
		rateConverter: rates.NewConverter(),
		sendMessageFunc: func(chatID int64, text string) error {
			messages = append(messages, text)
			return nil
		},
		sendChattableFunc: func(c tgbotapi.Chattable) error {
			chattables = append(chattables, c)
			return nil
		},
	}

	return bot, &messages, &chattables
}

func TestHandleCommand_ThresholdWithoutArgumentRequestsForceReply(t *testing.T) {
	bot, messages, chattables := newForceReplyTestBot()

	bot.handleCommand(123, "/threshold")

	if len(*messages) != 0 {
		t.Fatalf("expected no plain-text immediate error, got %v", *messages)
	}
	if len(*chattables) != 1 {
		t.Fatalf("expected 1 force-reply prompt, got %d", len(*chattables))
	}

	msg, ok := (*chattables)[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("expected MessageConfig, got %T", (*chattables)[0])
	}
	if !strings.Contains(msg.Text, "/threshold") {
		t.Fatalf("expected prompt to mention /threshold, got %q", msg.Text)
	}
	replyMarkup, ok := msg.ReplyMarkup.(tgbotapi.ForceReply)
	if !ok || !replyMarkup.ForceReply {
		t.Fatalf("expected ForceReply markup, got %#v", msg.ReplyMarkup)
	}
}

func TestHandleMessage_ReplyToThresholdPromptAppliesValue(t *testing.T) {
	bot, messages, _ := newForceReplyTestBot()
	bot.setAuthenticated(123)

	bot.handleMessage(&tgbotapi.Message{
		Text: "/threshold",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	bot.handleMessage(&tgbotapi.Message{
		Text: "0.03",
		Chat: &tgbotapi.Chat{ID: 123},
		ReplyToMessage: &tgbotapi.Message{
			Text:      "请回复 /threshold 的参数值",
			MessageID: 1,
		},
	})

	if bot.config.NotifyRateThreshold != 0.03 {
		t.Fatalf("expected threshold to be updated, got %f", bot.config.NotifyRateThreshold)
	}
	if len(*messages) == 0 || !strings.Contains((*messages)[len(*messages)-1], "阈值已设定为") {
		t.Fatalf("expected success message, got %v", *messages)
	}
}

func TestHandleCommand_MindailylendrateWithoutArgumentRequestsForceReply(t *testing.T) {
	bot, _, chattables := newForceReplyTestBot()

	bot.handleCommand(123, "/mindailylendrate")

	if len(*chattables) != 1 {
		t.Fatalf("expected 1 force-reply prompt, got %d", len(*chattables))
	}
	msg := (*chattables)[0].(tgbotapi.MessageConfig)
	if !strings.Contains(msg.Text, "FRR") {
		t.Fatalf("expected prompt to mention FRR option, got %q", msg.Text)
	}
}
