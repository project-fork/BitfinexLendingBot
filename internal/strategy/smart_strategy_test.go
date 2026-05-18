package strategy

import (
	"math"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
)

const floatTolerance = 1e-9

func TestSmartStrategy_CalculateOptimalAllocation(t *testing.T) {
	strategy := NewSmartStrategy(&config.Config{
		VolatilityThreshold: 0.002,
	})

	tests := []struct {
		name             string
		condition        *MarketCondition
		expectedHighHold float64
		expectedSpread   float64
	}{
		{
			name: "rising trend",
			condition: &MarketCondition{
				Trend:      "rising",
				Volatility: 0.001,
				RateRatio:  1.0,
			},
			expectedHighHold: 0.3,
			expectedSpread:   0.7,
		},
		{
			name: "falling trend",
			condition: &MarketCondition{
				Trend:      "falling",
				Volatility: 0.001,
				RateRatio:  1.0,
			},
			expectedHighHold: 0.7,
			expectedSpread:   0.3,
		},
		{
			name: "stable trend",
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				RateRatio:  1.0,
			},
			expectedHighHold: 0.5,
			expectedSpread:   0.5,
		},
		{
			name: "high volatility",
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.003, // 高于阈值
				RateRatio:  1.0,
			},
			expectedHighHold: 0.6, // 0.5 + 0.1
			expectedSpread:   0.4,
		},
		{
			name: "high rate ratio",
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				RateRatio:  1.3, // 高于 1.2
			},
			expectedHighHold: 0.6, // 0.5 + 0.1
			expectedSpread:   0.4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			highHold, spread := strategy.calculateOptimalAllocation(tt.condition)

			if math.Abs(highHold-tt.expectedHighHold) > floatTolerance {
				t.Errorf("Expected highHold %f, got %f", tt.expectedHighHold, highHold)
			}
			if math.Abs(spread-tt.expectedSpread) > floatTolerance {
				t.Errorf("Expected spread %f, got %f", tt.expectedSpread, spread)
			}
		})
	}
}

func TestSmartStrategy_CalculateProgressiveRate(t *testing.T) {
	strategy := NewSmartStrategy(&config.Config{
		MaxRateMultiplier: 2.0,
		MinRateMultiplier: 0.8,
	})

	condition := &MarketCondition{
		Trend:      "stable",
		Volatility: 0.001,
		AvgRate:    0.0003,
	}

	tests := []struct {
		name         string
		fundingBook  []*bitfinex.FundingBookEntry
		minDailyRate float64
		orderIndex   int
		totalOrders  int
		expectMin    float64
		expectMax    float64
	}{
		{
			name:         "empty funding book",
			fundingBook:  []*bitfinex.FundingBookEntry{},
			minDailyRate: 0.0002,
			orderIndex:   0,
			totalOrders:  3,
			expectMin:    0.0002,
			expectMax:    0.0004, // 应该使用合成利率
		},
		{
			name: "with funding book data",
			fundingBook: []*bitfinex.FundingBookEntry{
				{Rate: 0.0003, Amount: 1000},
				{Rate: 0.0005, Amount: 2000},
				{Rate: 0.0007, Amount: 1500},
			},
			minDailyRate: 0.0002,
			orderIndex:   1,
			totalOrders:  3,
			expectMin:    0.0003,
			expectMax:    0.0007,
		},
		{
			name: "rates below minimum",
			fundingBook: []*bitfinex.FundingBookEntry{
				{Rate: 0.0001, Amount: 1000}, // 低于最小利率
			},
			minDailyRate: 0.0002,
			orderIndex:   0,
			totalOrders:  2,
			expectMin:    0.0002,
			expectMax:    0.0003,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := strategy.calculateProgressiveRate(tt.fundingBook, tt.minDailyRate, condition, tt.orderIndex, tt.totalOrders)

			if rate < tt.expectMin {
				t.Errorf("Rate %f is below expected minimum %f", rate, tt.expectMin)
			}
			if rate > tt.expectMax {
				t.Errorf("Rate %f is above expected maximum %f", rate, tt.expectMax)
			}
		})
	}
}

func TestSmartStrategy_CalculateProgressiveRate_UndercutsFundingBookRate(t *testing.T) {
	strategy := NewSmartStrategy(&config.Config{
		RateRangeIncreasePercent: 0.2,
		FundingBookRateUndercut:  0.000001,
	})
	condition := &MarketCondition{Trend: "stable"}
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00030137, Amount: 1000},
	}

	rate := strategy.calculateProgressiveRate(fundingBook, 0.0003, condition, 0, 1)
	expected := 0.00030136

	if math.Abs(rate-expected) > floatTolerance {
		t.Fatalf("expected undercut rate %.8f, got %.8f", expected, rate)
	}
}

func TestSmartStrategy_CalculateProgressiveRate_UsesConfiguredFundingBookRateUndercut(t *testing.T) {
	strategy := NewSmartStrategy(&config.Config{
		RateRangeIncreasePercent: 0.2,
		FundingBookRateUndercut:  0.000002,
	})
	condition := &MarketCondition{Trend: "stable"}
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.00030137, Amount: 1000},
	}

	rate := strategy.calculateProgressiveRate(fundingBook, 0.0003, condition, 0, 1)
	expected := 0.00030135

	if math.Abs(rate-expected) > floatTolerance {
		t.Fatalf("expected configured undercut rate %.8f, got %.8f", expected, rate)
	}
}

func TestSmartStrategy_CalculateProgressiveRate_DoesNotUndercutBelowMinimum(t *testing.T) {
	strategy := NewSmartStrategy(&config.Config{
		RateRangeIncreasePercent: 0.2,
		FundingBookRateUndercut:  0.000001,
	})
	condition := &MarketCondition{Trend: "stable"}
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.000300005, Amount: 1000},
	}

	rate := strategy.calculateProgressiveRate(fundingBook, 0.0003, condition, 0, 1)
	expected := 0.0003

	if math.Abs(rate-expected) > floatTolerance {
		t.Fatalf("expected minimum rate %.8f, got %.8f", expected, rate)
	}
}

func TestSmartStrategy_CalculateSmartPeriod(t *testing.T) {
	cfg := &config.Config{
		ThirtyDayLendRateThreshold:    0.04,
		SixtyDayLendRateThreshold:     0.042,
		NinetyDayLendRateThreshold:    0.044,
		OneTwentyDayLendRateThreshold: 0.045,
		VolatilityThreshold:           0.002,
	}
	strategy := NewSmartStrategy(cfg)

	tests := []struct {
		name      string
		dailyRate float64
		condition *MarketCondition
		expected  int
	}{
		{
			name:      "low rate stable market",
			dailyRate: 0.0003, // 0.03% 日利率
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				AvgRate:    0.0003,
			},
			expected: 2, // 默认期间
		},
		{
			name:      "medium rate triggers 30 day",
			dailyRate: 0.0004, // 0.04% 日利率
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				AvgRate:    0.0003,
			},
			expected: 30,
		},
		{
			name:      "sixty day threshold",
			dailyRate: 0.00042, // 0.042% daily rate
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				AvgRate:    0.0003,
			},
			expected: 60,
		},
		{
			name:      "ninety day threshold",
			dailyRate: 0.00044, // 0.044% daily rate
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				AvgRate:    0.0003,
			},
			expected: 90,
		},
		{
			name:      "high rate triggers 120 day",
			dailyRate: 0.00045, // 0.045% 日利率
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.001,
				AvgRate:    0.0003,
			},
			expected: 120,
		},
		{
			name:      "rising trend prefers shorter period",
			dailyRate: 0.00045, // 0.045% 日利率
			condition: &MarketCondition{
				Trend:      "rising",
				Volatility: 0.001,
				AvgRate:    0.0003,
			},
			expected: 30, // 120 -> 30 因为上升趋势
		},
		{
			name:      "high volatility prefers shorter period",
			dailyRate: 0.00045, // 0.045% 日利率
			condition: &MarketCondition{
				Trend:      "stable",
				Volatility: 0.004, // 高波动
				AvgRate:    0.0003,
			},
			expected: 30, // 120 -> 30 因为高波动
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			period := strategy.calculateSmartPeriod(tt.dailyRate, tt.condition)
			if period != tt.expected {
				t.Errorf("Expected period %d, got %d", tt.expected, period)
			}
		})
	}
}

func TestSmartStrategy_CalculateSmartPeriod_FixedLoanDays(t *testing.T) {
	cfg := &config.Config{
		LoanDays:            30,
		VolatilityThreshold: 0.002,
	}
	strategy := NewSmartStrategy(cfg)

	period := strategy.calculateSmartPeriod(0.00045, &MarketCondition{
		Trend:      "rising",
		Volatility: 0.004,
		AvgRate:    0.0003,
	})

	if period != 30 {
		t.Fatalf("Expected fixed period 30, got %d", period)
	}
}

func TestSmartStrategy_CalculateSmartOffers(t *testing.T) {
	cfg := &config.Config{
		MinLoan:                       150.0,
		MaxLoan:                       1000.0,
		SpreadLend:                    3,
		HighHoldAmount:                500.0,
		HighHoldOrders:                1,
		HighHoldRate:                  0.1,
		MinDailyLendRate:              0.02,
		ThirtyDayLendRateThreshold:    0.04,
		OneTwentyDayLendRateThreshold: 0.045,
		EnableSmartStrategy:           true,
		VolatilityThreshold:           0.002,
		MaxRateMultiplier:             2.0,
		MinRateMultiplier:             0.8,
	}
	strategy := NewSmartStrategy(cfg)

	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.0003, Amount: 1000},
		{Rate: 0.0004, Amount: 2000},
		{Rate: 0.0005, Amount: 1500},
	}

	tests := []struct {
		name           string
		fundsAvailable float64
		expectedOffers int
	}{
		{
			name:           "insufficient funds",
			fundsAvailable: 100.0, // 低于 MinLoan
			expectedOffers: 0,
		},
		{
			name:           "sufficient funds for both strategies",
			fundsAvailable: 2000.0,
			expectedOffers: 4, // 1 high hold + 3 spread offers
		},
		{
			name:           "sufficient funds for spread only",
			fundsAvailable: 400.0, // 不足以做高额持有
			expectedOffers: 2,     // 只有 spread offers (400/3 约等于每笔133，少于150所以只能分2笔)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offers := strategy.CalculateSmartOffers(tt.fundsAvailable, fundingBook)
			if len(offers) != tt.expectedOffers {
				t.Errorf("Expected %d offers, got %d", tt.expectedOffers, len(offers))
			}

			// 验证所有 offers 都有有效值
			for i, offer := range offers {
				if offer.Amount <= 0 {
					t.Errorf("Offer %d has invalid amount: %f", i, offer.Amount)
				}
				if offer.Rate <= 0 {
					t.Errorf("Offer %d has invalid rate: %f", i, offer.Rate)
				}
				if offer.Period <= 0 {
					t.Errorf("Offer %d has invalid period: %d", i, offer.Period)
				}
			}

			if tt.name == "sufficient funds for both strategies" {
				expectedAmounts := []float64{500.0, 500.0, 500.0, 500.0}
				for i, expected := range expectedAmounts {
					if offers[i].Amount != expected {
						t.Errorf("Expected offer %d amount %f, got %f", i, expected, offers[i].Amount)
					}
				}
			}
		})
	}
}

func TestSmartStrategy_OrderLimitReservesSlotsForHighHold(t *testing.T) {
	cfg := &config.Config{
		MinLoan:                       150.0,
		MaxLoan:                       300.0,
		SpreadLend:                    15,
		OrderLimit:                    4,
		HighHoldAmount:                300.0,
		HighHoldOrders:                1,
		HighHoldRate:                  0.1,
		MinDailyLendRate:              0.02,
		ThirtyDayLendRateThreshold:    0.04,
		OneTwentyDayLendRateThreshold: 0.045,
		EnableSmartStrategy:           true,
		VolatilityThreshold:           0.002,
		MaxRateMultiplier:             2.0,
		MinRateMultiplier:             0.8,
	}

	strategy := NewSmartStrategy(cfg)
	fundingBook := []*bitfinex.FundingBookEntry{
		{Rate: 0.0003, Amount: 1000},
		{Rate: 0.0004, Amount: 2000},
		{Rate: 0.0005, Amount: 1500},
	}

	offers := strategy.CalculateSmartOffers(1097.0, fundingBook)
	if len(offers) != 4 {
		t.Fatalf("Expected 4 offers, got %d", len(offers))
	}

	expectedAmounts := []float64{300.0, 265.67, 265.67, 265.66}
	for i, expected := range expectedAmounts {
		if offers[i].Amount != expected {
			t.Fatalf("Expected offer %d amount %f, got %f", i, expected, offers[i].Amount)
		}
	}
}
