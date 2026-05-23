package strategy

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

type summaryFundingClient struct {
	fundingBook []*bitfinex.FundingBookEntry
	submitted   int
}

func (s *summaryFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	return s.fundingBook, nil
}

func (s *summaryFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	return nil, nil
}

func (s *summaryFundingClient) CancelFundingOffer(offerID int64) error {
	return nil
}

func (s *summaryFundingClient) GetFundingBalance(currency string) (float64, error) {
	return 431.985884, nil
}

func (s *summaryFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	s.submitted++
	return int64(s.submitted), nil
}

func (s *summaryFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	s.submitted++
	return int64(s.submitted), nil
}

func (s *summaryFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	return nil, nil
}

func (s *summaryFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *summaryFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func TestExecute_LogsStrategyDecisionSummary(t *testing.T) {
	reader, writer := io.Pipe()
	client := &summaryFundingClient{
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0003, Amount: 500, Period: 30},
			{Rate: 0.00031, Amount: 800, Period: 30},
			{Rate: 0.00032, Amount: 1200, Period: 30},
		},
	}

	bot := &LendingBot{
		config: &config.Config{
			Currency:         "USD",
			Strategy:         config.StrategyTraditional,
			MinDailyLendRate: 0.03,
			SpreadLend:       3,
			GapBottom:        0,
			GapTop:           2,
			MinLoan:          150,
			MaxLoan:          2000,
			OrderLimit:       10,
			TestMode:         true,
			LoanPeriodThresholds: map[int]float64{
				30: 0.03,
			},
			HighHoldRate:   0.05,
			HighHoldAmount: 0,
			HighHoldOrders: 1,
		},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		logger:        log.New(writer, "", log.LstdFlags),
	}

	var builder strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&builder, reader)
		close(done)
	}()

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	_ = writer.Close()
	<-done

	logs := builder.String()
	if !strings.Contains(logs, "策略决策摘要 | strategy=traditional") {
		t.Fatalf("expected decision summary log, got:\n%s", logs)
	}
	if !strings.Contains(logs, "book_source=required_and_used") {
		t.Fatalf("expected funding book source in summary, got:\n%s", logs)
	}
	if !strings.Contains(logs, "requested=") || !strings.Contains(logs, "success=") {
		t.Fatalf("expected offer counters in summary, got:\n%s", logs)
	}
	if !strings.Contains(logs, "fund_sources=") || !strings.Contains(logs, "rate_sources=") || !strings.Contains(logs, "period_sources=") {
		t.Fatalf("expected explanation sources in summary, got:\n%s", logs)
	}
}
