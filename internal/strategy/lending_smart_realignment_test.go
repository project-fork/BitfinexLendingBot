package strategy

import (
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

type smartRealignmentFundingClient struct {
	fundingBook        []*bitfinex.FundingBookEntry
	balance            float64
	offers             []*bitfinex.FundingOffer
	cancelErrByOfferID map[int64]error
	cancelledOfferIDs  []int64
	submittedOfferIDs  []int64
	submittedAmounts   []float64
	submittedRates     []float64
	submittedPeriods   []int
	submittedFRRCounts int
}

func (s *smartRealignmentFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	return s.fundingBook, nil
}

func (s *smartRealignmentFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	return s.offers, nil
}

func (s *smartRealignmentFundingClient) CancelFundingOffer(offerID int64) error {
	s.cancelledOfferIDs = append(s.cancelledOfferIDs, offerID)
	if s.cancelErrByOfferID != nil {
		if err, ok := s.cancelErrByOfferID[offerID]; ok {
			return err
		}
	}
	return nil
}

func (s *smartRealignmentFundingClient) GetFundingBalance(currency string) (float64, error) {
	return s.balance, nil
}

func (s *smartRealignmentFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	orderID := int64(len(s.submittedOfferIDs) + 1000)
	s.submittedOfferIDs = append(s.submittedOfferIDs, orderID)
	s.submittedAmounts = append(s.submittedAmounts, amount)
	s.submittedRates = append(s.submittedRates, dailyRate)
	s.submittedPeriods = append(s.submittedPeriods, period)
	return orderID, nil
}

func (s *smartRealignmentFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	s.submittedFRRCounts++
	orderID := int64(len(s.submittedOfferIDs) + 1000)
	s.submittedOfferIDs = append(s.submittedOfferIDs, orderID)
	s.submittedAmounts = append(s.submittedAmounts, amount)
	s.submittedPeriods = append(s.submittedPeriods, period)
	return orderID, nil
}

func (s *smartRealignmentFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	return nil, nil
}

func (s *smartRealignmentFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *smartRealignmentFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func newSmartRealignmentBot(t *testing.T, client *smartRealignmentFundingClient, cfg *config.Config) *LendingBot {
	t.Helper()

	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	if cfg.Strategy == "" {
		cfg.Strategy = config.StrategySmart
	}

	return &LendingBot{
		config:                  cfg,
		client:                  client,
		rateConverter:           rates.NewConverter(),
		orderTracker:            tracker.NewBotOrderTrackerWithDataFile(t.TempDir() + "/data.json"),
		simpleStrategy:          NewSimpleStrategy(cfg),
		smartStrategy:           NewSmartStrategy(cfg),
		recentOrderFingerprints: make(map[string]time.Time),
	}
}

func TestExecute_SmartStrategySkipsRealignmentWhenPendingOffersTooNew(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 150,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0.00030,
				Period:     30,
				MTSCreated: now.Add(-10 * time.Minute).UnixMilli(),
			},
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               2,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		TestMode:                 false,
	}
	bot := newSmartRealignmentBot(t, client, cfg)
	bot.orderTracker.TrackOrder(101)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.cancelledOfferIDs) != 0 {
		t.Fatalf("expected no cancellation for fresh pending offers, got %v", client.cancelledOfferIDs)
	}
	if len(client.submittedOfferIDs) != 0 {
		t.Fatalf("expected no replacement submits for fresh pending offers, got %v", client.submittedOfferIDs)
	}
}

func TestExecute_SmartStrategyRealignsUsingAvailableFundsPlusVisiblePendingAmounts(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 150,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0.00030,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               2,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		TestMode:                 false,
	}
	bot := newSmartRealignmentBot(t, client, cfg)
	bot.orderTracker.TrackOrder(101)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.cancelledOfferIDs) != 1 || client.cancelledOfferIDs[0] != 101 {
		t.Fatalf("expected tracked pending offer 101 to be cancelled, got %v", client.cancelledOfferIDs)
	}
	if len(client.submittedOfferIDs) != 2 {
		t.Fatalf("expected 2 replacement offers from total funds 350, got %d", len(client.submittedOfferIDs))
	}

	totalSubmitted := 0.0
	for _, amount := range client.submittedAmounts {
		totalSubmitted += amount
	}
	if totalSubmitted != 350 {
		t.Fatalf("expected replacement offer total 350.00, got %.2f", totalSubmitted)
	}
}

func TestExecute_SmartStrategySkipsRealignmentWhenVisibleOffersContainFRR(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 150,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0,
				Period:     30,
				Type:       "FRRDELTAVAR",
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               2,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		TestMode:                 false,
		IncludeManualPendingOffersInStrategyFunds: true,
	}
	bot := newSmartRealignmentBot(t, client, cfg)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.cancelledOfferIDs) != 0 {
		t.Fatalf("expected FRR pending offer to block realignment cancellation, got %v", client.cancelledOfferIDs)
	}
	if len(client.submittedOfferIDs) != 0 {
		t.Fatalf("expected FRR pending offer to block replacement submits, got %v", client.submittedOfferIDs)
	}
}

func TestPendingOffersEquivalentToLoanOffers_IgnoresOrderAndAllowsCentTolerance(t *testing.T) {
	current := []*LoanOffer{
		{Amount: 200.00, Rate: 0.00040, Period: 30},
		{Amount: 149.99, Rate: 0.00030, Period: 2},
	}
	target := []*LoanOffer{
		{Amount: 150.00, Rate: 0.00030, Period: 2},
		{Amount: 200.00, Rate: 0.00040, Period: 30},
	}

	if !pendingOffersEquivalentToLoanOffers(current, target) {
		t.Fatal("expected reordered offers within $0.01 tolerance to be treated as equivalent")
	}
}

func TestPendingOffersEquivalentToLoanOffers_RejectsAmountDifferenceBeyondTolerance(t *testing.T) {
	current := []*LoanOffer{
		{Amount: 149.98, Rate: 0.00030, Period: 2},
	}
	target := []*LoanOffer{
		{Amount: 150.00, Rate: 0.00030, Period: 2},
	}

	if pendingOffersEquivalentToLoanOffers(current, target) {
		t.Fatal("expected amount difference beyond $0.01 tolerance to be treated as mismatch")
	}
}

func TestExecute_SmartStrategyRealignsIncludingManualVisibleOffersWhenConfigured(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 150,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0.00030,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
			{
				ID:         202,
				Amount:     150,
				Rate:       0.00031,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               3,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		TestMode:                 false,
		IncludeManualPendingOffersInStrategyFunds: true,
	}
	bot := newSmartRealignmentBot(t, client, cfg)
	bot.orderTracker.TrackOrder(101)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.cancelledOfferIDs) != 2 {
		t.Fatalf("expected both tracked and manual visible offers to be cancelled, got %v", client.cancelledOfferIDs)
	}

	totalSubmitted := 0.0
	for _, amount := range client.submittedAmounts {
		totalSubmitted += amount
	}
	if totalSubmitted != 500 {
		t.Fatalf("expected replacement offer total 500.00, got %.2f", totalSubmitted)
	}
}

func TestExecute_SmartStrategySkipsRealignmentWhenNormalizedOffersAlreadyMatch(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 0,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0.00040,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
			{
				ID:         202,
				Amount:     150,
				Rate:       0.00030,
				Period:     2,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               2,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		TestMode:                 false,
	}
	bot := newSmartRealignmentBot(t, client, cfg)
	bot.orderTracker.TrackOrder(101)
	bot.orderTracker.TrackOrder(202)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.cancelledOfferIDs) != 0 {
		t.Fatalf("expected no cancellation when normalized pending offers already match target, got %v", client.cancelledOfferIDs)
	}
	if len(client.submittedOfferIDs) != 0 {
		t.Fatalf("expected no replacement submits when normalized pending offers already match target, got %v", client.submittedOfferIDs)
	}
}

func TestExecute_SmartStrategySkipsReplacementWhenAnyCancellationFails(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 150,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0.00030,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
			{
				ID:         202,
				Amount:     150,
				Rate:       0.00031,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
		},
		cancelErrByOfferID: map[int64]error{
			202: errors.New("cancel failed"),
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               2,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		TestMode:                 false,
		IncludeManualPendingOffersInStrategyFunds: true,
	}
	bot := newSmartRealignmentBot(t, client, cfg)
	bot.orderTracker.TrackOrder(101)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.cancelledOfferIDs) != 2 {
		t.Fatalf("expected both visible offers to be attempted for cancellation, got %v", client.cancelledOfferIDs)
	}
	if len(client.submittedOfferIDs) != 0 {
		t.Fatalf("expected no replacement submits when any cancellation fails, got %v", client.submittedOfferIDs)
	}
}

func TestExecute_SmartStrategyRealignmentReplacementDoesNotApplyRateBonus(t *testing.T) {
	now := time.Now()
	client := &smartRealignmentFundingClient{
		balance: 150,
		offers: []*bitfinex.FundingOffer{
			{
				ID:         101,
				Amount:     200,
				Rate:       0.00030,
				Period:     30,
				MTSCreated: now.Add(-31 * time.Minute).UnixMilli(),
			},
		},
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.00030, Amount: 1000, Period: 30},
			{Rate: 0.00040, Amount: 1500, Period: 30},
		},
	}

	cfg := &config.Config{
		Currency:                 "USD",
		Strategy:                 config.StrategySmart,
		MinLoan:                  150,
		MaxLoan:                  1000,
		SpreadLend:               2,
		HighHoldAmount:           0,
		HighHoldOrders:           1,
		HighHoldRate:             0.05,
		MinDailyLendRate:         0.02,
		LoanPeriodThresholds:     map[int]float64{30: 0.03},
		VolatilityThreshold:      0.002,
		MaxRateMultiplier:        2.0,
		MinRateMultiplier:        0.8,
		RateRangeIncreasePercent: 0.2,
		OrderFingerprintTTL:      120,
		RateBonus:                0.10,
		TestMode:                 false,
	}
	bot := newSmartRealignmentBot(t, client, cfg)
	bot.orderTracker.TrackOrder(101)

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	gotRates := append([]float64(nil), client.submittedRates...)
	sort.Float64s(gotRates)
	wantRates := []float64{0.00030, 0.00040}
	sort.Float64s(wantRates)
	if len(gotRates) != len(wantRates) {
		t.Fatalf("expected %d replacement rates, got %d", len(wantRates), len(gotRates))
	}
	for i := range wantRates {
		if gotRates[i] != wantRates[i] {
			t.Fatalf("expected replacement rates %v without RATE_BONUS, got %v", wantRates, gotRates)
		}
	}
}
