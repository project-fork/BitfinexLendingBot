package strategy

import (
	"errors"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

func TestFindNewCreditsDetectsUnseenIDEvenWithOldTimestamp(t *testing.T) {
	seen := map[int64]struct{}{
		1001: {},
	}
	credits := []*bitfinex.FundingCredit{
		{ID: 1001, MTSOpened: 900},
		{ID: 1002, MTSOpened: 900},
	}

	newCredits := findNewCredits(credits, seen, 1000)

	if len(newCredits) != 1 {
		t.Fatalf("expected 1 new credit, got %d", len(newCredits))
	}
	if newCredits[0].ID != 1002 {
		t.Fatalf("expected unseen credit ID 1002, got %d", newCredits[0].ID)
	}
}

func TestFindNewCreditsIgnoresAlreadySeenCredit(t *testing.T) {
	seen := map[int64]struct{}{
		1001: {},
	}
	credits := []*bitfinex.FundingCredit{
		{ID: 1001, MTSOpened: 1200},
	}

	newCredits := findNewCredits(credits, seen, 1000)

	if len(newCredits) != 0 {
		t.Fatalf("expected no new credits, got %d", len(newCredits))
	}
}

type stubNotificationFundingClient struct {
	getFundingBalanceResult float64
	getFundingBalanceErr    error
	getFundingCredits       []*bitfinex.FundingCredit
	getFundingCreditsErr    error
}

func (s *stubNotificationFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	return nil, nil
}

func (s *stubNotificationFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	return nil, nil
}

func (s *stubNotificationFundingClient) CancelFundingOffer(offerID int64) error {
	return nil
}

func (s *stubNotificationFundingClient) GetFundingBalance(currency string) (float64, error) {
	if s.getFundingBalanceErr != nil {
		return 0, s.getFundingBalanceErr
	}
	return s.getFundingBalanceResult, nil
}

func (s *stubNotificationFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	return 0, nil
}

func (s *stubNotificationFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	return 0, nil
}

func (s *stubNotificationFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	return nil, nil
}

func (s *stubNotificationFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	if s.getFundingCreditsErr != nil {
		return nil, s.getFundingCreditsErr
	}
	return s.getFundingCredits, nil
}

func (s *stubNotificationFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func TestCheckNewLendingCredits_SendsFailureNotificationOncePerErrorBurst(t *testing.T) {
	client := &stubNotificationFundingClient{
		getFundingBalanceErr: errors.New("nonce: small"),
	}

	bot := &LendingBot{
		config:        &config.Config{Currency: "usd"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		logger:        log.New(os.Stderr, "", log.LstdFlags),
	}

	var messages []string
	bot.SetNotifyCallback(func(message string) error {
		messages = append(messages, message)
		return nil
	})

	_, _ = bot.CheckNewLendingCredits()
	_, _ = bot.CheckNewLendingCredits()

	if len(messages) != 1 {
		t.Fatalf("expected exactly 1 failure notification for repeated errors, got %d (%v)", len(messages), messages)
	}
	if !strings.Contains(messages[0], "借贷检查连续失败") {
		t.Fatalf("expected failure notification message, got %q", messages[0])
	}
	if !strings.Contains(messages[0], "nonce: small") {
		t.Fatalf("expected original error in notification, got %q", messages[0])
	}
}

func TestCheckNewLendingCredits_ResetsFailureNotificationAfterRecovery(t *testing.T) {
	client := &stubNotificationFundingClient{
		getFundingBalanceErr: errors.New("temporary failure"),
	}

	bot := &LendingBot{
		config:        &config.Config{Currency: "usd"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		logger:        log.New(os.Stderr, "", log.LstdFlags),
	}

	var messages []string
	bot.SetNotifyCallback(func(message string) error {
		messages = append(messages, message)
		return nil
	})

	_, _ = bot.CheckNewLendingCredits()

	client.getFundingBalanceErr = nil
	client.getFundingBalanceResult = 0
	client.getFundingCredits = []*bitfinex.FundingCredit{}
	_, _ = bot.CheckNewLendingCredits()

	client.getFundingBalanceErr = errors.New("temporary failure")
	_, _ = bot.CheckNewLendingCredits()

	if len(messages) != 2 {
		t.Fatalf("expected notifications before and after recovery, got %d (%v)", len(messages), messages)
	}
}

func TestCheckNewLendingCredits_SendsReturnedNotificationWhenCreditDisappears(t *testing.T) {
	client := &stubNotificationFundingClient{
		getFundingBalanceResult: 0,
		getFundingCredits: []*bitfinex.FundingCredit{
			{ID: 1001, Amount: 150, Rate: 0.0004, Period: 30, MTSOpened: 1700000000000},
		},
	}

	bot := &LendingBot{
		config:        &config.Config{Currency: "usd"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		logger:        log.New(os.Stderr, "", log.LstdFlags),
	}

	var messages []string
	bot.SetNotifyCallback(func(message string) error {
		messages = append(messages, message)
		return nil
	})

	_, _ = bot.CheckNewLendingCredits()

	client.getFundingCredits = []*bitfinex.FundingCredit{}
	_, _ = bot.CheckNewLendingCredits()

	if len(messages) != 1 {
		t.Fatalf("expected 1 returned notification, got %d (%v)", len(messages), messages)
	}
	if !strings.Contains(messages[0], "贷出已结束/返还通知") {
		t.Fatalf("expected returned notification title, got %q", messages[0])
	}
	if !strings.Contains(messages[0], "ID: 1001") {
		t.Fatalf("expected returned credit ID in notification, got %q", messages[0])
	}
}

func TestCheckNewLendingCredits_DoesNotSendReturnedNotificationOnInitialSnapshot(t *testing.T) {
	client := &stubNotificationFundingClient{
		getFundingBalanceResult: 0,
		getFundingCredits: []*bitfinex.FundingCredit{
			{ID: 1001, Amount: 150, Rate: 0.0004, Period: 30, MTSOpened: 1700000000000},
		},
	}

	bot := &LendingBot{
		config:        &config.Config{Currency: "usd"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		logger:        log.New(os.Stderr, "", log.LstdFlags),
	}

	var messages []string
	bot.SetNotifyCallback(func(message string) error {
		messages = append(messages, message)
		return nil
	})

	_, _ = bot.CheckNewLendingCredits()

	if len(messages) != 0 {
		t.Fatalf("expected no notifications during initial snapshot, got %d (%v)", len(messages), messages)
	}
}

func TestSendLendingNotification_UsesAlignedFormatAndDollarAmount(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			Currency:           "usd",
			NotificationFormat: "aligned",
		},
		client:        &stubNotificationFundingClient{},
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		logger:        log.New(os.Stderr, "", log.LstdFlags),
	}

	var messages []string
	bot.SetNotifyCallback(func(message string) error {
		messages = append(messages, message)
		return nil
	})

	err := bot.sendLendingNotification([]*bitfinex.FundingCredit{
		{ID: 1001, Amount: 434.62, Rate: 0.00039, Period: 120, MTSOpened: 1700000000000},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(messages))
	}

	message := messages[0]
	expectedFragments := []string{
		"<pre>金额　　：$434.62",
		"日利率　：0.0390%",
		"期间　　：120 天",
		"预期收益：$20.34",
		"总金额　　：$434.62",
		"总预期收益：$20.34",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(message, fragment) {
			t.Fatalf("expected message to contain %q, got:\n%s", fragment, message)
		}
	}
}
