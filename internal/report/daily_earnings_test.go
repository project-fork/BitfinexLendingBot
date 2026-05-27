package report

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

func TestDailyEarningsReportBuildsSummary(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 5, 27, 9, 35, 0, 0, loc)

	builder := NewDailyEarningsReportBuilder(&config.Config{
		Currency:           "usd",
		NotificationFormat: "classic",
	})

	credits := []*bitfinex.FundingCredit{
		{
			ID:        1,
			Amount:    1000,
			RateType:  "FIXED",
			Rate:      0.0003,
			Period:    120,
			MTSOpened: time.Date(2026, 5, 26, 0, 0, 0, 0, loc).UnixMilli(),
			Status:    "ACTIVE",
		},
		{
			ID:        2,
			Amount:    500,
			RateType:  "FIXED",
			Rate:      0.0004,
			Period:    120,
			MTSOpened: time.Date(2026, 5, 26, 12, 0, 0, 0, loc).UnixMilli(),
			Status:    "CLOSED",
		},
	}

	ledgers := []*bitfinex.LedgerEntry{
		{ID: 1, Currency: "USD", MTS: time.Date(2026, 5, 27, 9, 0, 0, 0, loc).UnixMilli(), Amount: 0.40, Description: "Margin Funding Payment on wallet funding"},
		{ID: 2, Currency: "USD", MTS: time.Date(2026, 5, 25, 9, 0, 0, 0, loc).UnixMilli(), Amount: 0.30, Description: "Interest Payment"},
		{ID: 3, Currency: "USD", MTS: time.Date(2026, 5, 24, 9, 0, 0, 0, loc).UnixMilli(), Amount: 0.20, Description: "Interest Payment"},
		{ID: 4, Currency: "USD", MTS: time.Date(2026, 5, 2, 9, 0, 0, 0, loc).UnixMilli(), Amount: 0.10, Description: "Interest Payment"},
		{ID: 5, Currency: "USD", MTS: time.Date(2026, 1, 15, 9, 0, 0, 0, loc).UnixMilli(), Amount: 0.50, Description: "Interest Payment"},
	}

	message, summary, err := builder.Build(now, credits, ledgers)
	if err != nil {
		t.Fatalf("expected no build error, got %v", err)
	}

	if summary.YesterdayEstimated <= 0 {
		t.Fatalf("expected yesterday estimate > 0, got %f", summary.YesterdayEstimated)
	}
	if !almostEqual(summary.YesterdayActual, 0.40) {
		t.Fatalf("expected yesterday actual 0.40, got %f", summary.YesterdayActual)
	}
	if !almostEqual(summary.Last7DaysActual, 0.90) {
		t.Fatalf("expected last 7 days actual 0.90, got %f", summary.Last7DaysActual)
	}
	if !almostEqual(summary.ThisWeekActual, 0.70) {
		t.Fatalf("expected this week actual 0.70, got %f", summary.ThisWeekActual)
	}
	if !almostEqual(summary.ThisMonthActual, 1.00) {
		t.Fatalf("expected this month actual 1.00, got %f", summary.ThisMonthActual)
	}
	if !almostEqual(summary.ThisYearActual, 1.50) {
		t.Fatalf("expected this year actual 1.50, got %f", summary.ThisYearActual)
	}

	expectedFragments := []string{
		"每日收益报告（2026-05-27）",
		"昨日收益对比",
		"贷出订单数",
		"贷出总金额",
		"预估收益",
		"实际收益",
		"收益汇总",
		"近 7 天",
		"本周",
		"本月",
		"本年",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(message, fragment) {
			t.Fatalf("expected message to contain %q, got:\n%s", fragment, message)
		}
	}
}

func almostEqual(got float64, want float64) bool {
	return math.Abs(got-want) < 0.000001
}

func TestSelectCreditsForYesterdayEstimate_IncludesActiveAndYesterdayClosedCredits(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 5, 27, 9, 35, 0, 0, loc)

	active := []*bitfinex.FundingCredit{
		{ID: 1, Status: "ACTIVE", Amount: 100, MTSOpened: time.Date(2026, 5, 20, 0, 0, 0, 0, loc).UnixMilli()},
	}
	history := []*bitfinex.FundingCredit{
		{ID: 2, Status: "CLOSED", Amount: 200, MTSOpened: time.Date(2026, 5, 26, 8, 0, 0, 0, loc).UnixMilli(), MTSUpdated: time.Date(2026, 5, 26, 20, 0, 0, 0, loc).UnixMilli()},
		{ID: 3, Status: "CLOSED", Amount: 300, MTSOpened: time.Date(2026, 5, 25, 8, 0, 0, 0, loc).UnixMilli(), MTSUpdated: time.Date(2026, 5, 25, 20, 0, 0, 0, loc).UnixMilli()},
	}

	credits := SelectCreditsForYesterdayEstimate(now, active, history)
	if len(credits) != 2 {
		t.Fatalf("expected 2 credits, got %d", len(credits))
	}
	if credits[0].ID != 1 || credits[1].ID != 2 {
		t.Fatalf("unexpected credits selected: %+v", credits)
	}
}

func TestSelectCreditsForYesterdayEstimate_ExcludesYesterdayUpdatedButStillActiveHistoryCredits(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 5, 27, 9, 35, 0, 0, loc)

	history := []*bitfinex.FundingCredit{
		{ID: 2, Status: "CLOSED", Amount: 200, MTSOpened: time.Date(2026, 5, 26, 8, 0, 0, 0, loc).UnixMilli(), MTSUpdated: time.Date(2026, 5, 26, 20, 0, 0, 0, loc).UnixMilli()},
		{ID: 3, Status: "ACTIVE", Amount: 300, MTSOpened: time.Date(2026, 5, 26, 7, 0, 0, 0, loc).UnixMilli(), MTSUpdated: time.Date(2026, 5, 26, 18, 0, 0, 0, loc).UnixMilli()},
	}

	credits := SelectCreditsForYesterdayEstimate(now, nil, history)
	if len(credits) != 1 {
		t.Fatalf("expected only closed history credit to be selected, got %d", len(credits))
	}
	if credits[0].ID != 2 {
		t.Fatalf("expected credit 2 to be selected, got %+v", credits)
	}
}

func TestDailyEarningsReportBuildsSummary_UsesUpdatedTimeForClosedCreditEstimate(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 5, 27, 9, 35, 0, 0, loc)

	builder := NewDailyEarningsReportBuilder(&config.Config{
		Currency:           "usd",
		NotificationFormat: "classic",
	})

	credits := []*bitfinex.FundingCredit{
		{
			ID:         1,
			Amount:     1000,
			RateType:   "FIXED",
			Rate:       0.0003,
			Period:     120,
			MTSOpened:  time.Date(2026, 5, 26, 0, 0, 0, 0, loc).UnixMilli(),
			MTSUpdated: time.Date(2026, 5, 26, 12, 0, 0, 0, loc).UnixMilli(),
			Status:     "CLOSED",
		},
	}

	_, summary, err := builder.Build(now, credits, nil)
	if err != nil {
		t.Fatalf("expected no build error, got %v", err)
	}

	want := 1000 * 0.0003 * 0.5 * fundingFeeMultiplier
	if !almostEqual(summary.YesterdayEstimated, want) {
		t.Fatalf("expected yesterday estimate %.6f, got %.6f", want, summary.YesterdayEstimated)
	}
}
