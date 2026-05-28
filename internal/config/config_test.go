package config

import "testing"

func TestDailyEarningsDefaults(t *testing.T) {
	cfg := &Config{}

	cfg.setDailyEarningsDefaults()

	if !cfg.IsDailyEarningsReportEnabled() {
		t.Fatal("expected daily earnings report to default to enabled")
	}
	if got := cfg.GetDailyEarningsTriggerTime(); got != "09:35" {
		t.Fatalf("expected default trigger time 09:35, got %q", got)
	}
	if got := cfg.GetDailyEarningsTimezone(); got != "Asia/Shanghai" {
		t.Fatalf("expected default timezone Asia/Shanghai, got %q", got)
	}
}

func TestGetDailyEarningsTriggerClock(t *testing.T) {
	cfg := &Config{
		DailyEarningsReport: DailyEarningsReportConfig{
			TriggerTime: "17:05",
		},
	}

	hour, minute, err := cfg.GetDailyEarningsTriggerClock()
	if err != nil {
		t.Fatalf("expected valid trigger time, got %v", err)
	}
	if hour != 17 || minute != 5 {
		t.Fatalf("expected 17:05, got %02d:%02d", hour, minute)
	}
}

func TestGetDailyEarningsTriggerClock_RejectsInvalidFormat(t *testing.T) {
	cfg := &Config{
		DailyEarningsReport: DailyEarningsReportConfig{
			TriggerTime: "9:35",
		},
	}

	if _, _, err := cfg.GetDailyEarningsTriggerClock(); err == nil {
		t.Fatal("expected invalid trigger time format to fail")
	}
}

func TestGetDailyEarningsLocation(t *testing.T) {
	cfg := &Config{
		DailyEarningsReport: DailyEarningsReportConfig{
			Timezone: "UTC",
		},
	}

	loc, err := cfg.GetDailyEarningsLocation()
	if err != nil {
		t.Fatalf("expected valid timezone, got %v", err)
	}
	if got := loc.String(); got != "UTC" {
		t.Fatalf("expected UTC location, got %q", got)
	}
}

func TestGetDailyEarningsLocation_RejectsInvalidTimezone(t *testing.T) {
	cfg := &Config{
		DailyEarningsReport: DailyEarningsReportConfig{
			Timezone: "Mars/Base",
		},
	}

	if _, err := cfg.GetDailyEarningsLocation(); err == nil {
		t.Fatal("expected invalid timezone to fail")
	}
}
