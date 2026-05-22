package telegram

import (
	"errors"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

type stubLendingBot struct {
	pendingOffers       []*bitfinex.PendingFundingOffer
	pendingOffersErr    error
	cancelSummary       *bitfinex.FundingOfferCancelSummary
	cancelErr           error
	cancelIncludeAllArg []bool
}

func (s *stubLendingBot) GetActiveLendingCredits() ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *stubLendingBot) CheckRateThreshold() (bool, float64, error) {
	return false, 0, nil
}

func (s *stubLendingBot) ListPendingFundingOffers() ([]*bitfinex.PendingFundingOffer, error) {
	if s.pendingOffersErr != nil {
		return nil, s.pendingOffersErr
	}
	return s.pendingOffers, nil
}

func (s *stubLendingBot) CancelPendingFundingOffers(includeAll bool) (*bitfinex.FundingOfferCancelSummary, error) {
	s.cancelIncludeAllArg = append(s.cancelIncludeAllArg, includeAll)
	if s.cancelErr != nil {
		return nil, s.cancelErr
	}
	return s.cancelSummary, nil
}

func newTestBotWithMessages(lendingBot LendingBot) (*Bot, *[]string, *[]tgbotapi.Chattable) {
	messages := []string{}
	chattables := []tgbotapi.Chattable{}
	bot := &Bot{
		config:        &config.Config{Currency: "USD"},
		rateConverter: rates.NewConverter(),
		lendingBot:    lendingBot,
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

func TestHandlePendingOffers_ShowsTrackedAndManualCounts(t *testing.T) {
	lb := &stubLendingBot{
		pendingOffers: []*bitfinex.PendingFundingOffer{
			{
				FundingOffer: bitfinex.FundingOffer{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
				IsTracked:    true,
			},
			{
				FundingOffer: bitfinex.FundingOffer{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
				IsTracked:    false,
			},
		},
	}
	bot, messages, _ := newTestBotWithMessages(lb)

	bot.handlePendingOffers(1)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	message := (*messages)[0]
	expectedFragments := []string{
		"📭 当前未成交订单",
		"ID: 101",
		"类型: 程序追踪",
		"ID: 202",
		"类型: 手动挂单",
		"总订单数: 2",
		"程序追踪: 1",
		"手动挂单: 1",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(message, fragment) {
			t.Fatalf("expected message to contain %q, got:\n%s", fragment, message)
		}
	}
}

func TestHandlePendingOffers_UsesAlignedFormatWhenConfigured(t *testing.T) {
	lb := &stubLendingBot{
		pendingOffers: []*bitfinex.PendingFundingOffer{
			{
				FundingOffer: bitfinex.FundingOffer{ID: 101, Amount: 434.62, Rate: 0.00039, Period: 120},
				IsTracked:    false,
			},
		},
	}
	bot, messages, _ := newTestBotWithMessages(lb)
	bot.config.NotificationFormat = "aligned"

	bot.handlePendingOffers(1)

	if len(*messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(*messages))
	}
	message := (*messages)[0]
	expectedFragments := []string{
		"<pre>金额　：$434.62",
		"日利率：0.0390%",
		"期间　：120 天",
		"类型　：手动挂单",
		"总金额　：$434.62",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(message, fragment) {
			t.Fatalf("expected aligned message to contain %q, got:\n%s", fragment, message)
		}
	}
}

func TestHandlePendingOffers_ReportsError(t *testing.T) {
	lb := &stubLendingBot{pendingOffersErr: errors.New("boom")}
	bot, messages, _ := newTestBotWithMessages(lb)

	bot.handlePendingOffers(1)

	if len(*messages) != 1 || !strings.Contains((*messages)[0], "获取未成交订单失败") {
		t.Fatalf("expected error message, got %v", *messages)
	}
}

func TestHandleCancelPendingOffers_DefaultRequestsConfirmation(t *testing.T) {
	lb := &stubLendingBot{
		cancelSummary: &bitfinex.FundingOfferCancelSummary{
			Total:     3,
			Cancelled: 1,
			Skipped:   2,
			Failed:    0,
		},
	}
	bot, messages, chattables := newTestBotWithMessages(lb)

	bot.handleCancelPendingOffers(1, "/canceloffers")

	if len(lb.cancelIncludeAllArg) != 0 {
		t.Fatalf("expected no cancellation before confirmation, got %v", lb.cancelIncludeAllArg)
	}
	if len(*messages) != 0 {
		t.Fatalf("expected no plain-text message before confirmation, got %v", *messages)
	}
	if len(*chattables) != 1 {
		t.Fatalf("expected 1 confirmation message, got %d", len(*chattables))
	}
	msg := (*chattables)[0].(tgbotapi.MessageConfig)
	if !strings.Contains(msg.Text, "将取消程序追踪的未成交订单") {
		t.Fatalf("unexpected confirmation message:\n%s", msg.Text)
	}
}

func TestHandleCancelPendingOffers_AllRequestsConfirmation(t *testing.T) {
	lb := &stubLendingBot{
		cancelSummary: &bitfinex.FundingOfferCancelSummary{
			Total:     2,
			Cancelled: 2,
			Skipped:   0,
			Failed:    0,
		},
	}
	bot, messages, chattables := newTestBotWithMessages(lb)

	bot.handleCancelPendingOffers(1, "/canceloffers all")

	if len(lb.cancelIncludeAllArg) != 0 {
		t.Fatalf("expected no cancellation before confirmation, got %v", lb.cancelIncludeAllArg)
	}
	if len(*messages) != 0 {
		t.Fatalf("expected no plain-text message before confirmation, got %v", *messages)
	}
	if len(*chattables) != 1 {
		t.Fatalf("expected 1 confirmation message, got %d", len(*chattables))
	}
	msg := (*chattables)[0].(tgbotapi.MessageConfig)
	if !strings.Contains(msg.Text, "包括手动挂单") {
		t.Fatalf("expected all-cancel warning, got %q", msg.Text)
	}
}

func TestHandleCancelPendingOffers_RejectsUnknownArgument(t *testing.T) {
	lb := &stubLendingBot{}
	bot, messages, _ := newTestBotWithMessages(lb)

	bot.handleCancelPendingOffers(1, "/canceloffers invalid")

	if len(lb.cancelIncludeAllArg) != 0 {
		t.Fatalf("expected no cancellation call, got %v", lb.cancelIncludeAllArg)
	}
	if len(*messages) != 1 || !strings.Contains((*messages)[0], "格式错误") {
		t.Fatalf("expected format error message, got %v", *messages)
	}
}
