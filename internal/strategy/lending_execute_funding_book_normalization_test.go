package strategy

import (
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

type executeNormalizationFundingClient struct {
	fundingBook   []*bitfinex.FundingBookEntry
	submittedRate []float64
}

func (s *executeNormalizationFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	return s.fundingBook, nil
}

func (s *executeNormalizationFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	return nil, nil
}

func (s *executeNormalizationFundingClient) CancelFundingOffer(offerID int64) error {
	return nil
}

func (s *executeNormalizationFundingClient) GetFundingBalance(currency string) (float64, error) {
	return 431.985884, nil
}

func (s *executeNormalizationFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	s.submittedRate = append(s.submittedRate, dailyRate)
	return int64(len(s.submittedRate)), nil
}

func (s *executeNormalizationFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	return 0, nil
}

func (s *executeNormalizationFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	return nil, nil
}

func (s *executeNormalizationFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *executeNormalizationFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func TestExecute_UsesSingleBookSideForPricing(t *testing.T) {
	client := &executeNormalizationFundingClient{
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0005, Amount: 500},
			{Rate: 0.00025, Amount: -1000},
			{Rate: 0.0006, Amount: 800},
			{Rate: 0.00026, Amount: -1200},
			{Rate: 0.00027, Amount: -1500},
		},
	}

	bot := &LendingBot{
		config: &config.Config{
			Currency:         "USD",
			MinDailyLendRate: 0.03,
			SpreadLend:       6,
			GapBottom:        2,
			GapTop:           18,
			MinLoan:          150,
			MaxLoan:          2000,
			LoanPeriodThresholds: map[int]float64{
				30:  0.03,
				60:  0.035,
				90:  0.04,
				120: 0.045,
			},
		},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
	}

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(client.submittedRate) == 0 {
		t.Fatal("expected submitted offers")
	}

	for i, rate := range client.submittedRate {
		if rate < 0.0003 {
			t.Fatalf("expected submitted rate %d to stay above configured minimum, got %.8f", i, rate)
		}
	}
}
