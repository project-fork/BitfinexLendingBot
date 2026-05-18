package rates

import (
	"math"
	"testing"
)

func TestConverter_DecimalToPercentage(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name     string
		decimal  float64
		expected float64
	}{
		{
			name:     "zero",
			decimal:  0.0,
			expected: 0.0,
		},
		{
			name:     "small decimal",
			decimal:  0.0002,
			expected: 0.02,
		},
		{
			name:     "standard decimal",
			decimal:  0.05,
			expected: 5.0,
		},
		{
			name:     "one",
			decimal:  1.0,
			expected: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.DecimalToPercentage(tt.decimal)
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("DecimalToPercentage(%f) = %f, expected %f", tt.decimal, result, tt.expected)
			}
		})
	}
}

func TestConverter_PercentageToDecimal(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name       string
		percentage float64
		expected   float64
	}{
		{
			name:       "zero",
			percentage: 0.0,
			expected:   0.0,
		},
		{
			name:       "small percentage",
			percentage: 0.02,
			expected:   0.0002,
		},
		{
			name:       "standard percentage",
			percentage: 5.0,
			expected:   0.05,
		},
		{
			name:       "hundred percent",
			percentage: 100.0,
			expected:   1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.PercentageToDecimal(tt.percentage)
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("PercentageToDecimal(%f) = %f, expected %f", tt.percentage, result, tt.expected)
			}
		})
	}
}

func TestConverter_DailyToAnnual(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name      string
		dailyRate float64
		expected  float64
	}{
		{
			name:      "zero",
			dailyRate: 0.0,
			expected:  0.0,
		},
		{
			name:      "small daily rate",
			dailyRate: 0.0002,
			expected:  0.073, // 0.0002 * 365
		},
		{
			name:      "standard daily rate",
			dailyRate: 0.001,
			expected:  0.365, // 0.001 * 365
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.DailyToAnnual(tt.dailyRate)
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("DailyToAnnual(%f) = %f, expected %f", tt.dailyRate, result, tt.expected)
			}
		})
	}
}

func TestConverter_AnnualToDaily(t *testing.T) {
	converter := NewConverter()

	tests := []struct {
		name       string
		annualRate float64
		expected   float64
	}{
		{
			name:       "zero",
			annualRate: 0.0,
			expected:   0.0,
		},
		{
			name:       "small annual rate",
			annualRate: 0.073,
			expected:   0.0002, // 0.073 / 365
		},
		{
			name:       "standard annual rate",
			annualRate: 0.365,
			expected:   0.001, // 0.365 / 365
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := converter.AnnualToDaily(tt.annualRate)
			if math.Abs(result-tt.expected) > 1e-9 {
				t.Errorf("AnnualToDaily(%f) = %f, expected %f", tt.annualRate, result, tt.expected)
			}
		})
	}
}

// 测试转换的一致性（往返转换）
func TestConverter_Consistency(t *testing.T) {
	converter := NewConverter()

	testValues := []float64{0.0, 0.0001, 0.001, 0.01, 0.1, 1.0}

	for _, value := range testValues {
		t.Run("decimal_percentage_consistency", func(t *testing.T) {
			// decimal -> percentage -> decimal
			percentage := converter.DecimalToPercentage(value)
			backToDecimal := converter.PercentageToDecimal(percentage)

			if math.Abs(value-backToDecimal) > 1e-9 {
				t.Errorf("Inconsistent conversion: %f -> %f -> %f", value, percentage, backToDecimal)
			}
		})

		t.Run("daily_annual_consistency", func(t *testing.T) {
			// daily -> annual -> daily
			annual := converter.DailyToAnnual(value)
			backToDaily := converter.AnnualToDaily(annual)

			if math.Abs(value-backToDaily) > 1e-9 {
				t.Errorf("Inconsistent conversion: %f -> %f -> %f", value, annual, backToDaily)
			}
		})
	}
}
