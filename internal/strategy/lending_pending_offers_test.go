package strategy

import (
	"errors"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

type stubFundingClient struct {
	offers              []*bitfinex.FundingOffer
	cancelledOfferIDs   []int64
	cancelFailures      map[int64]error
	getFundingOffersErr error
}

func (s *stubFundingClient) GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error) {
	return nil, nil
}

func (s *stubFundingClient) GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error) {
	if s.getFundingOffersErr != nil {
		return nil, s.getFundingOffersErr
	}
	return s.offers, nil
}

func (s *stubFundingClient) CancelFundingOffer(offerID int64) error {
	if err := s.cancelFailures[offerID]; err != nil {
		return err
	}
	s.cancelledOfferIDs = append(s.cancelledOfferIDs, offerID)
	return nil
}

func (s *stubFundingClient) GetFundingBalance(currency string) (float64, error) {
	return 0, nil
}

func (s *stubFundingClient) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	return 0, nil
}

func (s *stubFundingClient) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	return 0, nil
}

func (s *stubFundingClient) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error) {
	return nil, nil
}

func (s *stubFundingClient) GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error) {
	return nil, nil
}

func (s *stubFundingClient) GetCurrentFundingRate(symbol string) (float64, error) {
	return 0, nil
}

func TestListPendingFundingOffers_MarksTrackedOrders(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD", IncludeManualPendingOffersInStrategyFunds: true},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	offers, err := bot.ListPendingFundingOffers()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(offers) != 2 {
		t.Fatalf("expected 2 offers, got %d", len(offers))
	}
	if !offers[0].IsTracked {
		t.Fatal("expected first offer to be tracked")
	}
	if offers[1].IsTracked {
		t.Fatal("expected second offer to be untracked")
	}
}

func TestListPendingFundingOffers_DefaultReturnsTrackedAndManualOrders(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	offers, err := bot.ListPendingFundingOffers()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(offers) != 2 {
		t.Fatalf("expected tracked and manual offers by default, got %d", len(offers))
	}
	if !offers[0].IsTracked || offers[0].ID != 101 {
		t.Fatalf("expected first offer to be tracked 101, got %+v", offers[0])
	}
	if offers[1].IsTracked || offers[1].ID != 202 {
		t.Fatalf("expected second offer to be manual 202, got %+v", offers[1])
	}
}

func TestListPendingFundingOffersByVisibility_OverridesConfig(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	allOffers, err := bot.listPendingFundingOffersByVisibility(pendingOfferVisibilityAll)
	if err != nil {
		t.Fatalf("expected no error listing all offers, got %v", err)
	}
	if len(allOffers) != 2 {
		t.Fatalf("expected 2 offers when forcing all visibility, got %d", len(allOffers))
	}

	bot.config.IncludeManualPendingOffersInStrategyFunds = true
	trackedOffers, err := bot.listPendingFundingOffersByVisibility(pendingOfferVisibilityTrackedOnly)
	if err != nil {
		t.Fatalf("expected no error listing tracked offers, got %v", err)
	}
	if len(trackedOffers) != 1 {
		t.Fatalf("expected 1 tracked offer when forcing tracked-only visibility, got %d", len(trackedOffers))
	}
	if !trackedOffers[0].IsTracked || trackedOffers[0].ID != 101 {
		t.Fatalf("expected tracked offer 101, got %+v", trackedOffers[0])
	}
}

func TestListStrategyVisiblePendingFundingOffers_DefaultOnlyReturnsTrackedOrders(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	offers, err := bot.listStrategyVisiblePendingFundingOffers()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("expected only tracked offer in default strategy-visible scope, got %d", len(offers))
	}
	if !offers[0].IsTracked || offers[0].ID != 101 {
		t.Fatalf("expected tracked offer 101, got %+v", offers[0])
	}
}

func TestListStrategyVisiblePendingFundingOffers_RespectsManualIncludeConfig(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD", IncludeManualPendingOffersInStrategyFunds: true},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	offers, err := bot.listStrategyVisiblePendingFundingOffers()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(offers) != 2 {
		t.Fatalf("expected tracked and manual offers in configured strategy-visible scope, got %d", len(offers))
	}
}

func TestCancelPendingFundingOffers_DefaultOnlyCancelsTracked(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
		cancelFailures: map[int64]error{},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	summary, err := bot.CancelPendingFundingOffers(false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary.Total != 1 || summary.Cancelled != 1 || summary.Skipped != 0 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(client.cancelledOfferIDs) != 1 || client.cancelledOfferIDs[0] != 101 {
		t.Fatalf("expected only tracked order 101 to be cancelled, got %v", client.cancelledOfferIDs)
	}
	if bot.orderTracker.IsTrackedOrder(101) {
		t.Fatal("expected cancelled tracked order to be removed from tracker")
	}
}

func TestCancelPendingFundingOffers_AllCancelsTrackedAndManual(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
		cancelFailures: map[int64]error{},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	summary, err := bot.CancelPendingFundingOffers(true)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary.Total != 2 || summary.Cancelled != 2 || summary.Skipped != 0 || summary.Failed != 0 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(client.cancelledOfferIDs) != 2 {
		t.Fatalf("expected both orders to be cancelled, got %v", client.cancelledOfferIDs)
	}
}

func TestCancelPendingFundingOffers_CountsFailuresAndContinues(t *testing.T) {
	client := &stubFundingClient{
		offers: []*bitfinex.FundingOffer{
			{ID: 101, Amount: 100, Rate: 0.0003, Period: 2},
			{ID: 202, Amount: 200, Rate: 0.0004, Period: 30},
		},
		cancelFailures: map[int64]error{
			101: errors.New("cancel failed"),
		},
	}
	orderTracker := tracker.NewBotOrderTracker()
	orderTracker.TrackOrder(101)
	orderTracker.TrackOrder(202)

	bot := &LendingBot{
		config:        &config.Config{Currency: "USD"},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  orderTracker,
	}

	summary, err := bot.CancelPendingFundingOffers(false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary.Total != 2 || summary.Cancelled != 1 || summary.Skipped != 0 || summary.Failed != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if len(client.cancelledOfferIDs) != 1 || client.cancelledOfferIDs[0] != 202 {
		t.Fatalf("expected order 202 to be cancelled after failure, got %v", client.cancelledOfferIDs)
	}
	if !bot.orderTracker.IsTrackedOrder(101) {
		t.Fatal("expected failed cancellation to remain tracked")
	}
}
