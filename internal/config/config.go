package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/errors"
	"github.com/spf13/viper"
)

// Config 應用程式配置結構
type Config struct {
	// API 配置
	BitfinexApiKey    string `mapstructure:"BITFINEX_API_KEY"`
	BitfinexSecretKey string `mapstructure:"BITFINEX_SECRET_KEY"`

	// 基本設定
	Currency   string `mapstructure:"CURRENCY"`
	OrderLimit int    `mapstructure:"ORDER_LIMIT"`

	RunOnlyOnNewCredits bool `mapstructure:"RUN_ONLY_ON_NEW_CREDITS"` // 是否僅在滿足觸發條件時執行（新借貸訂單或餘額顯著變化）
	// 如果 RunOnlyOnNewCredits 設定 true 則 MinutesRun 無用
	MinutesRun int `mapstructure:"MINUTES_RUN"` // 每隔幾分鐘清除訂單重新產生新訂單

	// 貸出限制
	MinLoan  float64 `mapstructure:"MIN_LOAN"`
	MaxLoan  float64 `mapstructure:"MAX_LOAN"`
	LoanDays int     `mapstructure:"LOAN_DAYS"` // 固定借貸天數，0 代表依策略自動決定

	// 利率策略
	MinDailyLendRate              any     `mapstructure:"MIN_DAILY_LEND_RATE"` // 支援數值或 "FRR"
	SpreadLend                    int     `mapstructure:"SPREAD_LEND"`         // 分散單最大目標筆數
	GapBottom                     float64 `mapstructure:"GAP_BOTTOM"`
	GapTop                        float64 `mapstructure:"GAP_TOP"`
	ThirtyDayLendRateThreshold    float64 `mapstructure:"THIRTY_DAY_LEND_RATE_THRESHOLD"`
	OneTwentyDayLendRateThreshold float64 `mapstructure:"ONE_TWENTY_DAY_LEND_RATE_THRESHOLD"`
	RateBonus                     float64 `mapstructure:"RATE_BONUS"`

	// 高額持有策略
	HighHoldRate   float64 `mapstructure:"HIGH_HOLD_RATE"`
	HighHoldAmount float64 `mapstructure:"HIGH_HOLD_AMOUNT"`
	HighHoldOrders int     `mapstructure:"HIGH_HOLD_ORDERS"`

	// Telegram 設定
	TelegramBotToken  string `mapstructure:"TELEGRAM_BOT_TOKEN"`
	TelegramAuthToken string `mapstructure:"TELEGRAM_AUTH_TOKEN"`

	// 通知設定
	NotifyRateThreshold float64 `mapstructure:"NOTIFY_RATE_THRESHOLD"`
	ReserveAmount       float64 `mapstructure:"RESERVE_AMOUNT"`

	// 智能策略設定
	EnableSmartStrategy      bool    `mapstructure:"ENABLE_SMART_STRATEGY"`
	VolatilityThreshold      float64 `mapstructure:"VOLATILITY_THRESHOLD"`
	MaxRateMultiplier        float64 `mapstructure:"MAX_RATE_MULTIPLIER"`
	MinRateMultiplier        float64 `mapstructure:"MIN_RATE_MULTIPLIER"`
	FundingBookRateUndercut  float64 `mapstructure:"FUNDING_BOOK_RATE_UNDERCUT"`  // Funding Book 利率下調量，百分比格式
	RateRangeIncreasePercent float64 `mapstructure:"RATE_RANGE_INCREASE_PERCENT"` // 利率範圍增加百分比

	// K線策略設定
	EnableKlineStrategy bool    `mapstructure:"ENABLE_KLINE_STRATEGY"` // 啟用K線策略
	KlineTimeFrame      string  `mapstructure:"KLINE_TIME_FRAME"`      // K線時間框架，預設15m
	KlinePeriod         int     `mapstructure:"KLINE_PERIOD"`          // K線週期數量，預設24（6小時）
	KlineSpreadPercent  float64 `mapstructure:"KLINE_SPREAD_PERCENT"`  // K線最高點加成百分比，預設0%
	KlineSmoothMethod   string  `mapstructure:"KLINE_SMOOTH_METHOD"`   // K線利率平滑方法：max, sma, ema, hla, p90

	// 測試模式設定
	TestMode bool `mapstructure:"TEST_MODE"`

	// 借貸通知設定
	LastLendingCheckTime int64   // 上次檢查借貸訂單的時間戳
	LastAvailableBalance float64 // 上次檢查時的可用餘額
	LendingCheckMinutes  int     `mapstructure:"LENDING_CHECK_MINUTES"` // 借貸訂單檢查間隔（分鐘）
	SeenFundingCreditIDs map[int64]struct{}
}

// LoadConfig 從文件加載配置
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, errors.NewConfigError("failed to read config file", err)
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, errors.NewConfigError("failed to unmarshal config", err)
	}

	// 設置智能策略參數的預設值
	config.setSmartStrategyDefaults()

	// 設置K線策略參數的預設值
	config.setKlineStrategyDefaults()

	// 設置借貸檢查間隔的預設值
	config.setLendingCheckDefaults()

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate 驗證配置有效性
func (c *Config) Validate() error {
	if c.BitfinexApiKey == "" || c.BitfinexApiKey == "your_api_key_here" {
		return errors.NewValidationError("BITFINEX_API_KEY is required and must be set to your actual API key")
	}
	if c.BitfinexSecretKey == "" || c.BitfinexSecretKey == "your_secret_key_here" {
		return errors.NewValidationError("BITFINEX_SECRET_KEY is required and must be set to your actual secret key")
	}
	if c.Currency == "" {
		return errors.NewValidationError("CURRENCY is required")
	}
	if c.MinLoan <= 0 {
		return errors.NewValidationError("MIN_LOAN must be positive")
	}
	if c.MaxLoan > 0 && c.MaxLoan < c.MinLoan {
		return errors.NewValidationError("MAX_LOAN cannot be less than MIN_LOAN")
	}
	if c.LoanDays < 0 {
		return errors.NewValidationError("LOAN_DAYS cannot be negative")
	}
	if c.LoanDays > constants.Period120Days {
		return errors.NewValidationError("LOAN_DAYS cannot be greater than 120")
	}
	if c.LoanDays == 1 {
		return errors.NewValidationError("LOAN_DAYS must be 0 or between 2 and 120")
	}
	minDailyRate, useFRR, err := c.parseMinDailyLendRate()
	if err != nil {
		return err
	}
	if !useFRR && minDailyRate <= 0 {
		return errors.NewValidationError("MIN_DAILY_LEND_RATE must be positive or FRR")
	}
	if c.SpreadLend <= 0 {
		return errors.NewValidationError("SPREAD_LEND must be positive")
	}
	if c.GapBottom < 0 || c.GapTop < 0 || c.GapTop <= c.GapBottom {
		return errors.NewValidationError("invalid GAP_BOTTOM or GAP_TOP values")
	}

	// 驗證智能策略參數
	if c.EnableSmartStrategy {
		if c.VolatilityThreshold <= 0 || c.VolatilityThreshold > 0.01 {
			return errors.NewValidationError("VOLATILITY_THRESHOLD must be between 0 and 0.01")
		}
		if c.MaxRateMultiplier <= 1.0 || c.MaxRateMultiplier > 5.0 {
			return errors.NewValidationError("MAX_RATE_MULTIPLIER must be between 1.0 and 5.0")
		}
		if c.MinRateMultiplier < 0.1 || c.MinRateMultiplier >= 1.0 {
			return errors.NewValidationError("MIN_RATE_MULTIPLIER must be between 0.1 and 1.0")
		}
		if c.MinRateMultiplier >= c.MaxRateMultiplier {
			return errors.NewValidationError("MIN_RATE_MULTIPLIER must be less than MAX_RATE_MULTIPLIER")
		}
		if c.RateRangeIncreasePercent <= 0 || c.RateRangeIncreasePercent > 1.0 {
			return errors.NewValidationError("RATE_RANGE_INCREASE_PERCENT must be between 0 and 1.0 (0-100%)")
		}
		if c.FundingBookRateUndercut < 0 || c.FundingBookRateUndercut > 0.01 {
			return errors.NewValidationError("FUNDING_BOOK_RATE_UNDERCUT must be between 0 and 0.01")
		}
	}

	// 驗證K線策略參數
	if c.EnableKlineStrategy {
		if c.KlineTimeFrame == "" {
			return errors.NewValidationError("KLINE_TIME_FRAME is required when ENABLE_KLINE_STRATEGY is true")
		}
		if c.KlinePeriod <= 0 {
			return errors.NewValidationError("KLINE_PERIOD must be positive")
		}
		if c.KlineSpreadPercent < 0 || c.KlineSpreadPercent > 100 {
			return errors.NewValidationError("KLINE_SPREAD_PERCENT must be between 0 and 100")
		}
		// 驗證平滑方法
		validMethods := []string{"max", "sma", "ema", "hla", "p90"}
		isValidMethod := false
		for _, method := range validMethods {
			if c.KlineSmoothMethod == method {
				isValidMethod = true
				break
			}
		}
		if !isValidMethod {
			return errors.NewValidationError("KLINE_SMOOTH_METHOD must be one of: max, sma, ema, hla, p90")
		}
	}

	// 驗證借貸檢查間隔
	if c.LendingCheckMinutes <= 0 {
		return errors.NewValidationError("LENDING_CHECK_MINUTES must be positive")
	}

	return nil
}

// GetFundingSymbol 獲取 funding symbol
func (c *Config) GetFundingSymbol() string {
	return constants.FundingSymbolPrefix + strings.ToUpper(c.Currency)
}

// GetMinDailyRateDecimal 獲取最低日利率（小數格式）
func (c *Config) GetMinDailyRateDecimal() float64 {
	minDailyRate, useFRR, err := c.parseMinDailyLendRate()
	if err != nil || useFRR {
		return 0
	}
	return minDailyRate / constants.PercentageToDecimal
}

// GetMinDailyRatePercentage 獲取最低日利率（百分比）
func (c *Config) GetMinDailyRatePercentage() float64 {
	minDailyRate, useFRR, err := c.parseMinDailyLendRate()
	if err != nil || useFRR {
		return 0
	}
	return minDailyRate
}

// GetMinDailyRateDisplay 獲取最低日利率顯示文字
func (c *Config) GetMinDailyRateDisplay() string {
	if c.IsMinDailyLendRateFRR() {
		return constants.MinDailyRateModeFRR
	}
	return fmt.Sprintf("%.4f%%", c.GetMinDailyRatePercentage())
}

// IsMinDailyLendRateFRR 檢查最低日利率是否設定為 FRR 模式
func (c *Config) IsMinDailyLendRateFRR() bool {
	_, useFRR, err := c.parseMinDailyLendRate()
	return err == nil && useFRR
}

// parseMinDailyLendRate 解析最低日利率配置（百分比）
func (c *Config) parseMinDailyLendRate() (float64, bool, error) {
	switch value := c.MinDailyLendRate.(type) {
	case nil:
		return 0, false, errors.NewValidationError("MIN_DAILY_LEND_RATE is required")
	case string:
		trimmedValue := strings.TrimSpace(value)
		if trimmedValue == "" {
			return 0, false, errors.NewValidationError("MIN_DAILY_LEND_RATE is required")
		}
		if strings.EqualFold(trimmedValue, constants.MinDailyRateModeFRR) {
			return 0, true, nil
		}
		rate, err := strconv.ParseFloat(trimmedValue, 64)
		if err != nil {
			return 0, false, errors.NewValidationError("MIN_DAILY_LEND_RATE must be a positive number or FRR")
		}
		return rate, false, nil
	case float64:
		return value, false, nil
	case float32:
		return float64(value), false, nil
	case int:
		return float64(value), false, nil
	case int8:
		return float64(value), false, nil
	case int16:
		return float64(value), false, nil
	case int32:
		return float64(value), false, nil
	case int64:
		return float64(value), false, nil
	case uint:
		return float64(value), false, nil
	case uint8:
		return float64(value), false, nil
	case uint16:
		return float64(value), false, nil
	case uint32:
		return float64(value), false, nil
	case uint64:
		return float64(value), false, nil
	default:
		return 0, false, errors.NewValidationError("MIN_DAILY_LEND_RATE must be a positive number or FRR")
	}
}

// GetHighHoldRateDecimal 獲取高額持有利率（小數格式）
func (c *Config) GetHighHoldRateDecimal() float64 {
	return c.HighHoldRate / constants.PercentageToDecimal
}

// GetThirtyDayThresholdDecimal 獲取30天閾值（小數格式）
func (c *Config) GetThirtyDayThresholdDecimal() float64 {
	return c.ThirtyDayLendRateThreshold / constants.PercentageToDecimal
}

// GetOneTwentyDayThresholdDecimal 獲取120天閾值（小數格式）
func (c *Config) GetOneTwentyDayThresholdDecimal() float64 {
	return c.OneTwentyDayLendRateThreshold / constants.PercentageToDecimal
}

// GetLoanPeriod 回傳實際借貸天數；若未固定設定則回傳 fallback。
func (c *Config) GetLoanPeriod(fallback int) int {
	if c.LoanDays > 0 {
		return c.LoanDays
	}
	return fallback
}

// setSmartStrategyDefaults 設置智能策略參數的預設值
func (c *Config) setSmartStrategyDefaults() {
	// 如果智能策略啟用但參數為零，設置建議的預設值
	if c.EnableSmartStrategy {
		if c.VolatilityThreshold == 0 {
			c.VolatilityThreshold = constants.DefaultVolatilityThreshold
		}
		if c.MaxRateMultiplier == 0 {
			c.MaxRateMultiplier = constants.DefaultMaxRateMultiplier
		}
		if c.MinRateMultiplier == 0 {
			c.MinRateMultiplier = constants.DefaultMinRateMultiplier
		}
		if c.RateRangeIncreasePercent == 0 {
			c.RateRangeIncreasePercent = constants.RateRangeIncreasePercent
		}
		if c.FundingBookRateUndercut == 0 {
			c.FundingBookRateUndercut = constants.DefaultFundingBookRateUndercut
		}
	} else {
		// 如果智能策略未啟用，確保參數有預設值以防止驗證錯誤
		if c.VolatilityThreshold == 0 {
			c.VolatilityThreshold = constants.DefaultVolatilityThreshold
		}
		if c.MaxRateMultiplier == 0 {
			c.MaxRateMultiplier = constants.DefaultMaxRateMultiplier
		}
		if c.MinRateMultiplier == 0 {
			c.MinRateMultiplier = constants.DefaultMinRateMultiplier
		}
		if c.RateRangeIncreasePercent == 0 {
			c.RateRangeIncreasePercent = constants.RateRangeIncreasePercent
		}
		if c.FundingBookRateUndercut == 0 {
			c.FundingBookRateUndercut = constants.DefaultFundingBookRateUndercut
		}
	}
}

// setKlineStrategyDefaults 設置K線策略參數的預設值
func (c *Config) setKlineStrategyDefaults() {
	// 如果K線策略啟用但參數為空，設置預設值
	if c.EnableKlineStrategy {
		if c.KlineTimeFrame == "" {
			c.KlineTimeFrame = "15m"
		}
		if c.KlinePeriod == 0 {
			c.KlinePeriod = 24 // 6小時的15分鐘K線
		}
		if c.KlineSpreadPercent == 0 {
			c.KlineSpreadPercent = 0.0 // 0%加成
		}
		if c.KlineSmoothMethod == "" {
			c.KlineSmoothMethod = "ema" // 預設使用指數移動平均
		}
	}
}

// setLendingCheckDefaults 設置借貸檢查間隔的預設值
func (c *Config) setLendingCheckDefaults() {
	// 如果未設置借貸檢查間隔，預設為 10 分鐘
	if c.LendingCheckMinutes == 0 {
		c.LendingCheckMinutes = 10
	}
}
