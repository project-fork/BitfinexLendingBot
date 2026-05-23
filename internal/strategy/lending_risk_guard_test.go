package strategy

import (
	"testing"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

type riskGuardFundingClient struct {
	fundingBook  []*bitfinex.FundingBookEntry
	balance      float64
	submitCount  int
	submitRates  []float64
	submitFRRCnt int
}

func (s *riskGuardFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	return s.fundingBook, nil
}

func (s *riskGuardFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	return nil, nil
}

func (s *riskGuardFundingClient) CancelFundingOffer(offerID int64) error {
	return nil
}

func (s *riskGuardFundingClient) GetFundingBalance(currency string) (float64, error) {
	return s.balance, nil
}

func (s *riskGuardFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	s.submitCount++
	s.submitRates = append(s.submitRates, dailyRate)
	return int64(s.submitCount), nil
}

func (s *riskGuardFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	s.submitFRRCnt++
	return int64(s.submitFRRCnt), nil
}

func (s *riskGuardFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	return nil, nil
}

func (s *riskGuardFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *riskGuardFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func newRiskGuardBot(client *riskGuardFundingClient) *LendingBot {
	return &LendingBot{
		config: &config.Config{
			Currency:                 "USD",
			Strategy:                 config.StrategyTraditional,
			MinLoan:                  150,
			MaxLoan:                  1000,
			MinDailyLendRate:         0.03,
			SpreadLend:               3,
			GapBottom:                0,
			GapTop:                   2,
			LoanPeriodThresholds:     map[int]float64{30: 0.03},
			HighHoldRate:             0.05,
			HighHoldAmount:           0,
			HighHoldOrders:           1,
			ExecutionCooldownSeconds: 30,
			OrderFingerprintTTL:      120,
		},
		client:                  client,
		rateConverter:           rates.NewConverter(),
		orderTracker:            tracker.NewBotOrderTracker(),
		simpleStrategy:          NewSimpleStrategy(&config.Config{}),
		smartStrategy:           NewSmartStrategy(&config.Config{}),
		recentOrderFingerprints: make(map[string]time.Time),
	}
}

func TestExecute_SkipsDuringCooldown(t *testing.T) {
	client := &riskGuardFundingClient{
		balance: 500,
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0003, Amount: 1000},
			{Rate: 0.0004, Amount: 1500},
		},
	}
	bot := newRiskGuardBot(client)
	bot.lastExecutionAt = time.Now()

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.submitCount != 0 {
		t.Fatalf("expected cooldown to block submission, got %d", client.submitCount)
	}
	summary := bot.GetLastDecisionSummary()
	if summary == nil || summary.SkipReason == "" {
		t.Fatal("expected cooldown skip reason to be recorded")
	}
}

func TestExecute_SkipsWhenFundsBelowMinExecutableFunds(t *testing.T) {
	client := &riskGuardFundingClient{
		balance: 500,
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0003, Amount: 1000},
			{Rate: 0.0004, Amount: 1500},
		},
	}
	bot := newRiskGuardBot(client)
	bot.config.ExecutionCooldownSeconds = 0
	bot.config.MinExecutableFunds = 600

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.submitCount != 0 {
		t.Fatalf("expected min executable funds to block submission, got %d", client.submitCount)
	}
}

func TestPlaceLoanOffers_SkipsDuplicateFingerprintsWithinTTL(t *testing.T) {
	client := &riskGuardFundingClient{}
	bot := newRiskGuardBot(client)
	bot.config.ExecutionCooldownSeconds = 0
	bot.config.TestMode = false

	offers := []*LoanOffer{
		{Amount: 200, Rate: 0.0003, Period: 30},
		{Amount: 200, Rate: 0.0003, Period: 30},
	}

	result, err := bot.placeLoanOffers(offers, true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.submitCount != 1 {
		t.Fatalf("expected only one submit due to idempotency guard, got %d", client.submitCount)
	}
	if result.SkippedOfferCount != 1 {
		t.Fatalf("expected one skipped duplicate offer, got %d", result.SkippedOfferCount)
	}
}

func TestExecuteManual_BypassesCooldown(t *testing.T) {
	client := &riskGuardFundingClient{
		balance: 500,
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0003, Amount: 1000},
			{Rate: 0.0004, Amount: 1500},
		},
	}
	bot := newRiskGuardBot(client)
	bot.lastExecutionAt = time.Now()
	bot.config.TestMode = false

	if err := bot.ExecuteManual("Telegram /run 手动触发", false); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client.submitCount == 0 {
		t.Fatal("expected manual execution to bypass cooldown and submit offers")
	}
	summary := bot.GetLastDecisionSummary()
	if summary == nil || !summary.CooldownBypassed {
		t.Fatal("expected decision summary to record cooldown bypass")
	}
	if summary.TriggerSource != "Telegram /run 手动触发" {
		t.Fatalf("expected trigger source to be recorded, got %q", summary.TriggerSource)
	}
}
