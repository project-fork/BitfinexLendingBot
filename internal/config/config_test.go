package config

import (
	"os"
	"testing"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				BitfinexApiKey:           "test_api_key",
				BitfinexSecretKey:        "test_secret_key",
				Currency:                 "USD",
				MinLoan:                  150.0,
				MaxLoan:                  1000.0,
				LoanDays:                 30,
				MinDailyLendRate:         0.02,
				SpreadLend:               30,
				GapBottom:                10,
				GapTop:                   5000,
				EnableSmartStrategy:      true,
				VolatilityThreshold:      0.002,
				MaxRateMultiplier:        2.0,
				MinRateMultiplier:        0.8,
				RateRangeIncreasePercent: 0.2,
				LendingCheckMinutes:      10,
			},
			wantErr: false,
		},
		{
			name: "valid config with FRR min daily rate",
			config: Config{
				BitfinexApiKey:           "test_api_key",
				BitfinexSecretKey:        "test_secret_key",
				Currency:                 "USD",
				MinLoan:                  150.0,
				MaxLoan:                  1000.0,
				LoanDays:                 0,
				MinDailyLendRate:         "FRR",
				SpreadLend:               30,
				GapBottom:                10,
				GapTop:                   5000,
				EnableSmartStrategy:      true,
				VolatilityThreshold:      0.002,
				MaxRateMultiplier:        2.0,
				MinRateMultiplier:        0.8,
				RateRangeIncreasePercent: 0.2,
				LendingCheckMinutes:      10,
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: Config{
				BitfinexApiKey:      "",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             150.0,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "placeholder API key",
			config: Config{
				BitfinexApiKey:      "your_api_key_here",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             150.0,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid min loan",
			config: Config{
				BitfinexApiKey:      "test_api_key",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             -150.0,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "max loan less than min loan",
			config: Config{
				BitfinexApiKey:      "test_api_key",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             1000.0,
				MaxLoan:             150.0,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid smart strategy config",
			config: Config{
				BitfinexApiKey:      "test_api_key",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             150.0,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				EnableSmartStrategy: true,
				VolatilityThreshold: 0.02, // 太大
				MaxRateMultiplier:   2.0,
				MinRateMultiplier:   0.8,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid min daily rate string",
			config: Config{
				BitfinexApiKey:      "test_api_key",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             150.0,
				MinDailyLendRate:    "INVALID",
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid loan days one day",
			config: Config{
				BitfinexApiKey:      "test_api_key",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             150.0,
				LoanDays:            1,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid loan days above max",
			config: Config{
				BitfinexApiKey:      "test_api_key",
				BitfinexSecretKey:   "test_secret_key",
				Currency:            "USD",
				MinLoan:             150.0,
				LoanDays:            121,
				MinDailyLendRate:    0.02,
				SpreadLend:          30,
				GapBottom:           10,
				GapTop:              5000,
				LendingCheckMinutes: 10,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// 创建测试配置文件
	testConfigContent := `
BITFINEX_API_KEY: "test_api_key"
BITFINEX_SECRET_KEY: "test_secret_key"
CURRENCY: "USD"
MIN_LOAN: 150.0
LOAN_DAYS: 30
MIN_DAILY_LEND_RATE: 0.02
SPREAD_LEND: 30
GAP_BOTTOM: 10
GAP_TOP: 5000
LENDING_CHECK_MINUTES: 10
SIXTY_DAY_LEND_RATE_THRESHOLD: 0.035
NINETY_DAY_LEND_RATE_THRESHOLD: 0.04
FUNDING_BOOK_RATE_UNDERCUT: 0.000002
`

	// 创建临时配置文件
	tmpFile, err := os.CreateTemp("", "test_config_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testConfigContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	// 测试加载配置
	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	// 验证配置值
	if config.BitfinexApiKey != "test_api_key" {
		t.Errorf("Expected BitfinexApiKey to be 'test_api_key', got '%s'", config.BitfinexApiKey)
	}
	if config.Currency != "USD" {
		t.Errorf("Expected Currency to be 'USD', got '%s'", config.Currency)
	}
	if config.MinLoan != 150.0 {
		t.Errorf("Expected MinLoan to be 150.0, got %f", config.MinLoan)
	}
	if config.LoanDays != 30 {
		t.Errorf("Expected LoanDays to be 30, got %d", config.LoanDays)
	}
	if config.SixtyDayLendRateThreshold != 0.035 {
		t.Errorf("Expected SixtyDayLendRateThreshold to be 0.035, got %f", config.SixtyDayLendRateThreshold)
	}
	if config.NinetyDayLendRateThreshold != 0.04 {
		t.Errorf("Expected NinetyDayLendRateThreshold to be 0.04, got %f", config.NinetyDayLendRateThreshold)
	}
	if config.FundingBookRateUndercut != 0.000002 {
		t.Errorf("Expected FundingBookRateUndercut to be 0.000002, got %.8f", config.FundingBookRateUndercut)
	}
}

func TestLoadConfigWithFRRMinDailyRate(t *testing.T) {
	testConfigContent := `
BITFINEX_API_KEY: "test_api_key"
BITFINEX_SECRET_KEY: "test_secret_key"
CURRENCY: "USD"
MIN_LOAN: 150.0
MIN_DAILY_LEND_RATE: FRR
SPREAD_LEND: 30
GAP_BOTTOM: 10
GAP_TOP: 5000
LENDING_CHECK_MINUTES: 10
`

	tmpFile, err := os.CreateTemp("", "test_config_frr_*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testConfigContent); err != nil {
		t.Fatalf("Failed to write to temp file: %v", err)
	}
	tmpFile.Close()

	config, err := LoadConfig(tmpFile.Name())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}

	if !config.IsMinDailyLendRateFRR() {
		t.Errorf("Expected FRR mode to be enabled from config")
	}
}

func TestGetFundingSymbol(t *testing.T) {
	config := &Config{Currency: "USD"}
	expected := "fUSD"
	result := config.GetFundingSymbol()
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestGetMinDailyRateDecimal(t *testing.T) {
	config := &Config{MinDailyLendRate: 0.02}
	expected := 0.0002
	result := config.GetMinDailyRateDecimal()
	if result != expected {
		t.Errorf("Expected %f, got %f", expected, result)
	}
}

func TestGetMinDailyRateDecimal_FRR(t *testing.T) {
	config := &Config{MinDailyLendRate: "FRR"}
	expected := 0.0
	result := config.GetMinDailyRateDecimal()
	if result != expected {
		t.Errorf("Expected %f, got %f", expected, result)
	}
	if !config.IsMinDailyLendRateFRR() {
		t.Errorf("Expected IsMinDailyLendRateFRR() to be true")
	}
}

func TestGetLoanPeriod(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		fallback int
		expected int
	}{
		{
			name:     "uses configured fixed loan days",
			config:   Config{LoanDays: 30},
			fallback: 2,
			expected: 30,
		},
		{
			name:     "uses fallback when auto mode",
			config:   Config{LoanDays: 0},
			fallback: 120,
			expected: 120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetLoanPeriod(tt.fallback); got != tt.expected {
				t.Fatalf("Expected %d, got %d", tt.expected, got)
			}
		})
	}
}
