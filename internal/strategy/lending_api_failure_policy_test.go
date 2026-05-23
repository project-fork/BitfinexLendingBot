package strategy

import (
	"errors"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

type failurePolicyFundingClient struct {
	fundingBookErr error
	candlesErr     error
	fundingBook    []*bitfinex.FundingBookEntry
	candles        []*bitfinex.Candle
	balance        float64
}

func (s *failurePolicyFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	if s.fundingBookErr != nil {
		return nil, s.fundingBookErr
	}
	return s.fundingBook, nil
}

func (s *failurePolicyFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	return nil, nil
}

func (s *failurePolicyFundingClient) CancelFundingOffer(offerID int64) error {
	return nil
}

func (s *failurePolicyFundingClient) GetFundingBalance(currency string) (float64, error) {
	return s.balance, nil
}

func (s *failurePolicyFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	return 1, nil
}

func (s *failurePolicyFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	return 1, nil
}

func (s *failurePolicyFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	if s.candlesErr != nil {
		return nil, s.candlesErr
	}
	return s.candles, nil
}

func (s *failurePolicyFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *failurePolicyFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func TestExecute_StopsWhenFundingBookFailsForNonKlineStrategy(t *testing.T) {
	client := &failurePolicyFundingClient{
		fundingBookErr: errors.New("funding book timeout"),
		balance:        500,
	}
	bot := &LendingBot{
		config: &config.Config{
			Currency:         "USD",
			Strategy:         config.StrategyTraditional,
			MinLoan:          150,
			MinDailyLendRate: 0.02,
			SpreadLend:       3,
			GapBottom:        0,
			GapTop:           2,
			HighHoldRate:     0.05,
			HighHoldAmount:   0,
			HighHoldOrders:   1,
		},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
	}

	err := bot.Execute()
	if err == nil {
		t.Fatal("expected execution to stop when funding book fails")
	}
}

func TestCalculateKlineOffers_ReturnsErrorWhenCandlesFail(t *testing.T) {
	client := &failurePolicyFundingClient{
		candlesErr: errors.New("candles timeout"),
	}
	bot := &LendingBot{
		config: &config.Config{
			Currency:           "USD",
			Strategy:           config.StrategyKline,
			MinLoan:            150,
			MinDailyLendRate:   0.02,
			KlineTimeFrame:     "15m",
			KlinePeriod:        24,
			KlineSmoothMethod:  "ema",
			KlineSpreadPercent: 0,
			HighHoldRate:       0.05,
			HighHoldAmount:     0,
			HighHoldOrders:     1,
		},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
	}

	_, err := bot.calculateKlineOffers(500)
	if err == nil {
		t.Fatal("expected kline offer calculation to fail when candles request fails")
	}
}

func TestCalculateKlineOffers_ReturnsErrorWhenCandlesEmpty(t *testing.T) {
	client := &failurePolicyFundingClient{
		candles: []*bitfinex.Candle{},
	}
	bot := &LendingBot{
		config: &config.Config{
			Currency:           "USD",
			Strategy:           config.StrategyKline,
			MinLoan:            150,
			MinDailyLendRate:   0.02,
			KlineTimeFrame:     "15m",
			KlinePeriod:        24,
			KlineSmoothMethod:  "ema",
			KlineSpreadPercent: 0,
			HighHoldRate:       0.05,
			HighHoldAmount:     0,
			HighHoldOrders:     1,
		},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
	}

	_, err := bot.calculateKlineOffers(500)
	if err == nil {
		t.Fatal("expected kline offer calculation to fail when candles response is empty")
	}
}
