package telegram

import (
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

func newPendingInputTestBot() (*Bot, *[]string, *[]tgbotapi.Chattable) {
	messages := []string{}
	chattables := []tgbotapi.Chattable{}
	nextMessageID := 1
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
		sendChattableWithResponseFunc: func(c tgbotapi.Chattable) (tgbotapi.Message, error) {
			chattables = append(chattables, c)
			msg := tgbotapi.Message{
				MessageID: nextMessageID,
				Chat:      &tgbotapi.Chat{ID: 123},
			}
			nextMessageID++
			return msg, nil
		},
		answerCallbackFunc: func(config tgbotapi.CallbackConfig) error {
			messages = append(messages, config.Text)
			return nil
		},
	}

	return bot, &messages, &chattables
}

func TestHandleCommand_ThresholdWithoutArgumentRequestsSingleInlinePrompt(t *testing.T) {
	bot, messages, chattables := newPendingInputTestBot()

	bot.handleCommand(123, "/threshold")

	if len(*messages) != 0 {
		t.Fatalf("expected no plain-text immediate error, got %v", *messages)
	}
	if len(*chattables) != 2 {
		t.Fatalf("expected message send and inline markup edit, got %d", len(*chattables))
	}

	msg, ok := (*chattables)[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("expected MessageConfig, got %T", (*chattables)[0])
	}
	if !strings.Contains(msg.Text, "请发送 /threshold") {
		t.Fatalf("expected prompt to mention /threshold send flow, got %q", msg.Text)
	}
	if _, ok := msg.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup); !ok {
		t.Fatalf("expected inline keyboard markup on prompt message, got %T", msg.ReplyMarkup)
	}

	edit, ok := (*chattables)[1].(tgbotapi.EditMessageReplyMarkupConfig)
	if !ok {
		t.Fatalf("expected EditMessageReplyMarkupConfig, got %T", (*chattables)[1])
	}
	if len(edit.ReplyMarkup.InlineKeyboard) != 1 || len(edit.ReplyMarkup.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected single cancel inline button, got %#v", edit.ReplyMarkup)
	}
	button := edit.ReplyMarkup.InlineKeyboard[0][0]
	if button.Text != "取消设置" || button.CallbackData == nil || !strings.HasPrefix(*button.CallbackData, "cancel:pendingreply:") {
		t.Fatalf("expected cancel pending reply button with token, got %#v", button)
	}
}

func TestHandleMessage_NextPlainTextAppliesPendingValue(t *testing.T) {
	bot, messages, _ := newPendingInputTestBot()
	bot.setAuthenticated(123)

	bot.handleMessage(&tgbotapi.Message{
		Text: "/threshold",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	bot.handleMessage(&tgbotapi.Message{
		Text: "0.03",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	if bot.config.NotifyRateThreshold != 0.03 {
		t.Fatalf("expected threshold to be updated, got %f", bot.config.NotifyRateThreshold)
	}
	if len(*messages) == 0 || !strings.Contains((*messages)[len(*messages)-1], "利率通知阈值已设定为") {
		t.Fatalf("expected success message, got %v", *messages)
	}
}

func TestHandleCommand_MindailylendrateWithoutArgumentRequestsSingleInlinePrompt(t *testing.T) {
	bot, _, chattables := newPendingInputTestBot()

	bot.handleCommand(123, "/mindailylendrate")

	if len(*chattables) != 2 {
		t.Fatalf("expected message send and inline markup edit, got %d", len(*chattables))
	}
	msg := (*chattables)[0].(tgbotapi.MessageConfig)
	if !strings.Contains(msg.Text, "FRR") {
		t.Fatalf("expected prompt to mention FRR option, got %q", msg.Text)
	}
}

func TestHandleCallbackQuery_CancelPendingReplyClearsWaitingCommand(t *testing.T) {
	bot, messages, chattables := newPendingInputTestBot()
	bot.setAuthenticated(123)

	bot.handleMessage(&tgbotapi.Message{
		Text: "/threshold",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	reply, ok := bot.getPendingReplyState(123)
	if !ok {
		t.Fatal("expected pending reply before cancel callback")
	}

	edit, ok := (*chattables)[1].(tgbotapi.EditMessageReplyMarkupConfig)
	if !ok {
		t.Fatalf("expected EditMessageReplyMarkupConfig, got %T", (*chattables)[1])
	}
	callbackData := *edit.ReplyMarkup.InlineKeyboard[0][0].CallbackData

	bot.handleCallbackQuery(&tgbotapi.CallbackQuery{
		ID:   "callback-id",
		Data: callbackData,
		Message: &tgbotapi.Message{
			Chat:      &tgbotapi.Chat{ID: 123},
			MessageID: reply.PromptMessageID,
		},
	})

	if _, ok := bot.getPendingReplyState(123); ok {
		t.Fatal("expected pending reply to be cleared after cancel callback")
	}

	bot.handleMessage(&tgbotapi.Message{
		Text: "0.03",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	if bot.config.NotifyRateThreshold == 0.03 {
		t.Fatal("expected canceled pending reply not to apply value")
	}
	if len(*messages) == 0 || !strings.Contains((*messages)[len(*messages)-1], "无效的指令") {
		t.Fatalf("expected normal command handling after cancel, got %v", *messages)
	}
}

func TestResolvePendingReplyExpiredSendsTimeoutMessage(t *testing.T) {
	bot, messages, chattables := newPendingInputTestBot()
	bot.setAuthenticated(123)

	bot.handleMessage(&tgbotapi.Message{
		Text: "/threshold",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	reply, ok := bot.getPendingReplyState(123)
	if !ok {
		t.Fatal("expected pending reply before expiry resolution")
	}

	bot.resolvePendingReply(123, reply, pendingReplyStatusExpired)

	if _, ok := bot.getPendingReplyState(123); ok {
		t.Fatal("expected expired pending reply to be cleared")
	}
	if len(*messages) == 0 || !strings.Contains((*messages)[len(*messages)-1], "待输入已超时失效") {
		t.Fatalf("expected timeout message, got %v", *messages)
	}
	if len(*chattables) < 3 {
		t.Fatalf("expected prompt edits after expiry, got %d chattables", len(*chattables))
	}
	editText, ok := (*chattables)[2].(tgbotapi.EditMessageTextConfig)
	if !ok {
		t.Fatalf("expected EditMessageTextConfig for expiry update, got %T", (*chattables)[2])
	}
	if !strings.Contains(editText.Text, "本次设置已超时") {
		t.Fatalf("expected expiry notice in edited prompt, got %q", editText.Text)
	}
}

func TestHandleMessage_ExpiredPendingReplyFallsBackToNormalCommand(t *testing.T) {
	bot, messages, _ := newPendingInputTestBot()
	bot.setAuthenticated(123)

	reply := bot.setPendingReply(123, "/threshold", -time.Second, 1, "prompt")
	bot.resolvePendingReply(123, reply, pendingReplyStatusExpired)

	bot.handleMessage(&tgbotapi.Message{
		Text: "0.03",
		Chat: &tgbotapi.Chat{ID: 123},
	})

	if bot.config.NotifyRateThreshold == 0.03 {
		t.Fatal("expected expired pending reply not to apply value")
	}
	if len(*messages) == 0 || !strings.Contains((*messages)[len(*messages)-1], "无效的指令") {
		t.Fatalf("expected expired input to fall back to normal command handling, got %v", *messages)
	}
}
