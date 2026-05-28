package config

import (
	"reflect"
	"testing"
)

func TestRuntimeConfigService_SetMinLoan(t *testing.T) {
	service := NewRuntimeConfigService(&Config{MinLoan: 150, MaxLoan: 300})

	if err := service.SetMinLoan(200); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := service.Config().MinLoan; got != 200 {
		t.Fatalf("expected min loan 200, got %f", got)
	}
}

func TestRuntimeConfigService_SetMinLoan_RejectsAboveMaxLoan(t *testing.T) {
	service := NewRuntimeConfigService(&Config{MinLoan: 150, MaxLoan: 300})

	if err := service.SetMinLoan(301); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeConfigService_SetMaxLoan_RejectsBelowMinLoan(t *testing.T) {
	service := NewRuntimeConfigService(&Config{MinLoan: 150, MaxLoan: 300})

	if err := service.SetMaxLoan(149); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeConfigService_SetStrategy(t *testing.T) {
	service := NewRuntimeConfigService(&Config{})

	if err := service.SetStrategy(StrategySmart); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got := service.Config().GetStrategy(); got != StrategySmart {
		t.Fatalf("expected strategy %q, got %q", StrategySmart, got)
	}
}

func TestRuntimeConfigService_SetKlineSmoothMethod_Invalid(t *testing.T) {
	service := NewRuntimeConfigService(&Config{})

	if err := service.SetKlineSmoothMethod("bad"); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRuntimeConfigService_GetLendingCheckState_ReturnsCopy(t *testing.T) {
	service := NewRuntimeConfigService(&Config{})
	service.UpdateLendingCheckState(100, 50, map[int64]struct{}{1: {}})

	_, _, seen := service.GetLendingCheckState()
	delete(seen, 1)

	_, _, stored := service.GetLendingCheckState()
	if _, ok := stored[1]; !ok {
		t.Fatal("expected internal seen map to remain unchanged")
	}
}

func TestRuntimeMutableConfigKeys(t *testing.T) {
	expected := []string{
		"HIGH_HOLD_AMOUNT",
		"HIGH_HOLD_ORDERS",
		"HIGH_HOLD_RATE",
		"KLINE_SMOOTH_METHOD",
		"LOAN_DAYS",
		"MAX_LOAN",
		"MIN_DAILY_LEND_RATE",
		"MIN_LOAN",
		"NOTIFY_RATE_THRESHOLD",
		"ORDER_LIMIT",
		"RATE_RANGE_INCREASE_PERCENT",
		"RESERVE_AMOUNT",
		"STRATEGY",
	}

	if got := RuntimeMutableConfigKeys(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected runtime mutable keys %v, got %v", expected, got)
	}
}

func TestStartupOnlyConfigKeys(t *testing.T) {
	expected := []string{
		"BITFINEX_API_KEY",
		"BITFINEX_SECRET_KEY",
		"CURRENCY",
		"DAILY_EARNINGS_REPORT",
		"FUNDING_BOOK_RATE_UNDERCUT",
		"GAP_BOTTOM",
		"GAP_TOP",
		"KLINE_PERIOD",
		"KLINE_SPREAD_PERCENT",
		"KLINE_TIME_FRAME",
		"LENDING_CHECK_MINUTES",
		"LOAN_PERIOD_THRESHOLDS",
		"MAX_RATE_MULTIPLIER",
		"MINUTES_RUN",
		"MIN_RATE_MULTIPLIER",
		"NOTIFICATION_FORMAT",
		"RUN_ONLY_ON_NEW_CREDITS",
		"TELEGRAM_AUTH_TOKEN",
		"TELEGRAM_BOT_TOKEN",
		"TEST_MODE",
		"VOLATILITY_THRESHOLD",
	}

	if got := StartupOnlyConfigKeys(); !reflect.DeepEqual(got, expected) {
		t.Fatalf("expected startup-only keys %v, got %v", expected, got)
	}
}
