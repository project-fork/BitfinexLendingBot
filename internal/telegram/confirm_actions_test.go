package telegram

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

type recordedCallback struct {
	Text      string
	ShowAlert bool
}

func newConfirmTestBot() (*Bot, *[]string, *[]tgbotapi.Chattable, *[]recordedCallback, *int, *stubLendingBot) {
	messages := []string{}
	chattables := []tgbotapi.Chattable{}
	callbacks := []recordedCallback{}
	restartCalls := 0
	lb := &stubLendingBot{
		cancelSummary: &bitfinex.FundingOfferCancelSummary{
			Total:     2,
			Cancelled: 1,
			Skipped:   1,
			Failed:    0,
		},
	}

	bot := &Bot{
		config:        &config.Config{Currency: "USD"},
		rateConverter: rates.NewConverter(),
		lendingBot:    lb,
		restartCallback: func() error {
			restartCalls++
			return nil
		},
		sendMessageFunc: func(chatID int64, text string) error {
			messages = append(messages, text)
			return nil
		},
		sendChattableFunc: func(c tgbotapi.Chattable) error {
			chattables = append(chattables, c)
			return nil
		},
		answerCallbackFunc: func(config tgbotapi.CallbackConfig) error {
			callbacks = append(callbacks, recordedCallback{
				Text:      config.Text,
				ShowAlert: config.ShowAlert,
			})
			return nil
		},
	}

	return bot, &messages, &chattables, &callbacks, &restartCalls, lb
}

func TestHandleEarnings_InvokesManualReportCallback(t *testing.T) {
	messages := []string{}
	callbackRuns := 0
	bot := &Bot{
		config:        &config.Config{Currency: "USD"},
		rateConverter: rates.NewConverter(),
		earningsCallback: func() error {
			callbackRuns++
			return nil
		},
		sendMessageFunc: func(chatID int64, text string) error {
			messages = append(messages, text)
			return nil
		},
	}

	bot.handleEarnings(123)

	if callbackRuns != 1 {
		t.Fatalf("expected earnings callback to run once, got %d", callbackRuns)
	}
	if len(messages) != 2 || !strings.Contains(messages[1], "发送完成") {
		t.Fatalf("expected start and success messages, got %v", messages)
	}
}

func TestHandleEarningsPreview_InvokesPreviewCallback(t *testing.T) {
	messages := []string{}
	callbackRuns := 0
	bot := &Bot{
		config:        &config.Config{Currency: "USD"},
		rateConverter: rates.NewConverter(),
		earningsPreviewCallback: func() error {
			callbackRuns++
			return nil
		},
		sendMessageFunc: func(chatID int64, text string) error {
			messages = append(messages, text)
			return nil
		},
	}

	bot.handleEarningsPreview(123)

	if callbackRuns != 1 {
		t.Fatalf("expected earnings preview callback to run once, got %d", callbackRuns)
	}
	if len(messages) != 2 || !strings.Contains(messages[1], "预览发送完成") {
		t.Fatalf("expected start and success messages, got %v", messages)
	}
}

func TestHandleRestart_SendsConfirmationKeyboard(t *testing.T) {
	bot, _, chattables, _, restartCalls, _ := newConfirmTestBot()

	bot.handleRestart(123)

	if *restartCalls != 0 {
		t.Fatal("expected restart not to run before confirmation")
	}
	if len(*chattables) != 1 {
		t.Fatalf("expected 1 confirmation message, got %d", len(*chattables))
	}

	msg, ok := (*chattables)[0].(tgbotapi.MessageConfig)
	if !ok {
		t.Fatalf("expected MessageConfig, got %T", (*chattables)[0])
	}
	if !strings.Contains(msg.Text, "是否继续") {
		t.Fatalf("expected confirmation text, got %q", msg.Text)
	}

	markup, ok := msg.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected inline keyboard markup, got %T", msg.ReplyMarkup)
	}
	if len(markup.InlineKeyboard) == 0 || len(markup.InlineKeyboard[0]) < 2 {
		t.Fatalf("expected confirm and cancel buttons, got %+v", markup.InlineKeyboard)
	}
}

func TestHandleCancelPendingOffers_SendsConfirmationKeyboard(t *testing.T) {
	bot, _, chattables, _, _, lb := newConfirmTestBot()

	bot.handleCancelPendingOffers(123, "/canceloffers all")

	if len(lb.cancelIncludeAllArg) != 0 {
		t.Fatal("expected no cancellation before confirmation")
	}
	if len(*chattables) != 1 {
		t.Fatalf("expected 1 confirmation message, got %d", len(*chattables))
	}
	msg := (*chattables)[0].(tgbotapi.MessageConfig)
	if !strings.Contains(msg.Text, "包括手动挂单") {
		t.Fatalf("expected all-cancel warning text, got %q", msg.Text)
	}
}

func TestHandleCallbackQuery_ConfirmRestartExecutesAction(t *testing.T) {
	bot, messages, _, callbacks, restartCalls, _ := newConfirmTestBot()

	bot.handleCallbackQuery(&tgbotapi.CallbackQuery{
		ID: "cb-1",
		Message: &tgbotapi.Message{
			MessageID: 77,
			Chat:      &tgbotapi.Chat{ID: 123},
		},
		Data: "confirm:restart",
	})

	if *restartCalls != 1 {
		t.Fatalf("expected restart to run once, got %d", *restartCalls)
	}
	if len(*callbacks) != 1 || !(*callbacks)[0].ShowAlert {
		t.Fatalf("expected alert callback response, got %+v", *callbacks)
	}
	if len(*messages) != 1 || !strings.Contains((*messages)[0], "重跑完成") {
		t.Fatalf("expected completion message, got %v", *messages)
	}
}

func TestHandleCallbackQuery_ConfirmCancelOffersExecutesAction(t *testing.T) {
	bot, messages, _, callbacks, _, lb := newConfirmTestBot()

	bot.handleCallbackQuery(&tgbotapi.CallbackQuery{
		ID: "cb-2",
		Message: &tgbotapi.Message{
			MessageID: 88,
			Chat:      &tgbotapi.Chat{ID: 123},
		},
		Data: "confirm:canceloffers:all",
	})

	if len(lb.cancelIncludeAllArg) != 1 || !lb.cancelIncludeAllArg[0] {
		t.Fatalf("expected cancel all to execute, got %v", lb.cancelIncludeAllArg)
	}
	if len(*callbacks) != 1 || !(*callbacks)[0].ShowAlert {
		t.Fatalf("expected alert callback response, got %+v", *callbacks)
	}
	if len(*messages) != 1 || !strings.Contains((*messages)[0], "取消全部未成交订单完成") {
		t.Fatalf("expected cancel summary message, got %v", *messages)
	}
}

func TestHandleCallbackQuery_CancelOnlyShowsAlert(t *testing.T) {
	bot, messages, _, callbacks, restartCalls, lb := newConfirmTestBot()

	bot.handleCallbackQuery(&tgbotapi.CallbackQuery{
		ID: "cb-3",
		Message: &tgbotapi.Message{
			MessageID: 99,
			Chat:      &tgbotapi.Chat{ID: 123},
		},
		Data: "cancel:restart",
	})

	if *restartCalls != 0 {
		t.Fatal("expected restart not to run on cancel")
	}
	if len(lb.cancelIncludeAllArg) != 0 {
		t.Fatal("expected no offer cancellation on cancel action")
	}
	if len(*messages) != 0 {
		t.Fatalf("expected no follow-up message, got %v", *messages)
	}
	if len(*callbacks) != 1 || !strings.Contains((*callbacks)[0].Text, "已取消") {
		t.Fatalf("expected cancel alert, got %+v", *callbacks)
	}
}
