package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/errors"
	"github.com/spf13/viper"
)

// Config 应用程序配置结构
type Config struct {
	// API 配置
	BitfinexApiKey    string `mapstructure:"BITFINEX_API_KEY"`
	BitfinexSecretKey string `mapstructure:"BITFINEX_SECRET_KEY"`

	// 基本设定
	Currency   string `mapstructure:"CURRENCY"`
	OrderLimit int    `mapstructure:"ORDER_LIMIT"`

	RunOnlyOnNewCredits bool `mapstructure:"RUN_ONLY_ON_NEW_CREDITS"` // 是否仅在满足触发条件时执行（新借贷订单或余额显着变化）
	// 如果 RunOnlyOnNewCredits 设定 true 则 MinutesRun 无用
	MinutesRun int `mapstructure:"MINUTES_RUN"` // 每隔几分钟清除订单重新产生新订单

	// 贷出限制
	MinLoan  float64 `mapstructure:"MIN_LOAN"`
	MaxLoan  float64 `mapstructure:"MAX_LOAN"`
	LoanDays int     `mapstructure:"LOAN_DAYS"` // 固定借贷天数，0 代表依策略自动决定

	// 利率策略
	MinDailyLendRate              any     `mapstructure:"MIN_DAILY_LEND_RATE"` // 支持数值或 "FRR"
	SpreadLend                    int     `mapstructure:"SPREAD_LEND"`         // 分散单最大目标笔数
	GapBottom                     float64 `mapstructure:"GAP_BOTTOM"`
	GapTop                        float64 `mapstructure:"GAP_TOP"`
	ThirtyDayLendRateThreshold    float64 `mapstructure:"THIRTY_DAY_LEND_RATE_THRESHOLD"`
	SixtyDayLendRateThreshold     float64 `mapstructure:"SIXTY_DAY_LEND_RATE_THRESHOLD"`
	NinetyDayLendRateThreshold    float64 `mapstructure:"NINETY_DAY_LEND_RATE_THRESHOLD"`
	OneTwentyDayLendRateThreshold float64 `mapstructure:"ONE_TWENTY_DAY_LEND_RATE_THRESHOLD"`
	RateBonus                     float64 `mapstructure:"RATE_BONUS"`

	// 高额持有策略
	HighHoldRate   float64 `mapstructure:"HIGH_HOLD_RATE"`
	HighHoldAmount float64 `mapstructure:"HIGH_HOLD_AMOUNT"`
	HighHoldOrders int     `mapstructure:"HIGH_HOLD_ORDERS"`

	// Telegram 设定
	TelegramBotToken  string `mapstructure:"TELEGRAM_BOT_TOKEN"`
	TelegramAuthToken string `mapstructure:"TELEGRAM_AUTH_TOKEN"`

	// 通知设定
	NotifyRateThreshold float64 `mapstructure:"NOTIFY_RATE_THRESHOLD"`
	ReserveAmount       float64 `mapstructure:"RESERVE_AMOUNT"`
	NotificationFormat  string  `mapstructure:"NOTIFICATION_FORMAT"`

	// 智能策略设定
	EnableSmartStrategy      bool    `mapstructure:"ENABLE_SMART_STRATEGY"`
	VolatilityThreshold      float64 `mapstructure:"VOLATILITY_THRESHOLD"`
	MaxRateMultiplier        float64 `mapstructure:"MAX_RATE_MULTIPLIER"`
	MinRateMultiplier        float64 `mapstructure:"MIN_RATE_MULTIPLIER"`
	FundingBookRateUndercut  float64 `mapstructure:"FUNDING_BOOK_RATE_UNDERCUT"`  // Funding Book 利率下调量，百分比格式
	RateRangeIncreasePercent float64 `mapstructure:"RATE_RANGE_INCREASE_PERCENT"` // 利率范围增加百分比

	// K线策略设定
	EnableKlineStrategy bool    `mapstructure:"ENABLE_KLINE_STRATEGY"` // 启用K线策略
	KlineTimeFrame      string  `mapstructure:"KLINE_TIME_FRAME"`      // K线时间框架，默认15m
	KlinePeriod         int     `mapstructure:"KLINE_PERIOD"`          // K线周期数量，默认24（6小时）
	KlineSpreadPercent  float64 `mapstructure:"KLINE_SPREAD_PERCENT"`  // K线最高点加成百分比，默认0%
	KlineSmoothMethod   string  `mapstructure:"KLINE_SMOOTH_METHOD"`   // K线利率平滑方法：max, sma, ema, hla, p90

	// 测试模式设定
	TestMode bool `mapstructure:"TEST_MODE"`

	// 借贷通知设定
	LastLendingCheckTime int64   // 上次检查借贷订单的时间戳
	LastAvailableBalance float64 // 上次检查时的可用余额
	LendingCheckMinutes  int     `mapstructure:"LENDING_CHECK_MINUTES"` // 借贷订单检查间隔（分钟）
	SeenFundingCreditIDs map[int64]struct{}
}

// LoadConfig 从文件加载配置
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

	// 设置智能策略参数的默认值
	config.setSmartStrategyDefaults()

	// 设置K线策略参数的默认值
	config.setKlineStrategyDefaults()

	// 设置借贷检查间隔的默认值
	config.setLendingCheckDefaults()

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// Validate 验证配置有效性
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

	// 验证智能策略参数
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

	// 验证K线策略参数
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
		// 验证平滑方法
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

	// 验证借贷检查间隔
	if c.LendingCheckMinutes <= 0 {
		return errors.NewValidationError("LENDING_CHECK_MINUTES must be positive")
	}
	if c.NotificationFormat != "" && c.NotificationFormat != "classic" && c.NotificationFormat != "aligned" {
		return errors.NewValidationError("NOTIFICATION_FORMAT must be one of: classic, aligned")
	}

	return nil
}

// GetFundingSymbol 获取 funding symbol
func (c *Config) GetFundingSymbol() string {
	return constants.FundingSymbolPrefix + strings.ToUpper(c.Currency)
}

// GetMinDailyRateDecimal 获取最低日利率（小数格式）
func (c *Config) GetMinDailyRateDecimal() float64 {
	minDailyRate, useFRR, err := c.parseMinDailyLendRate()
	if err != nil || useFRR {
		return 0
	}
	return minDailyRate / constants.PercentageToDecimal
}

// GetMinDailyRatePercentage 获取最低日利率（百分比）
func (c *Config) GetMinDailyRatePercentage() float64 {
	minDailyRate, useFRR, err := c.parseMinDailyLendRate()
	if err != nil || useFRR {
		return 0
	}
	return minDailyRate
}

// GetMinDailyRateDisplay 获取最低日利率显示文字
func (c *Config) GetMinDailyRateDisplay() string {
	if c.IsMinDailyLendRateFRR() {
		return constants.MinDailyRateModeFRR
	}
	return fmt.Sprintf("%.4f%%", c.GetMinDailyRatePercentage())
}

// IsMinDailyLendRateFRR 检查最低日利率是否设定为 FRR 模式
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

// GetHighHoldRateDecimal 获取高额持有利率（小数格式）
func (c *Config) GetHighHoldRateDecimal() float64 {
	return c.HighHoldRate / constants.PercentageToDecimal
}

// GetThirtyDayThresholdDecimal 获取30天阈值（小数格式）
func (c *Config) GetThirtyDayThresholdDecimal() float64 {
	return c.ThirtyDayLendRateThreshold / constants.PercentageToDecimal
}

// GetSixtyDayThresholdDecimal 获取60天阈值（小数格式）
func (c *Config) GetSixtyDayThresholdDecimal() float64 {
	return c.SixtyDayLendRateThreshold / constants.PercentageToDecimal
}

// GetNinetyDayThresholdDecimal 获取90天阈值（小数格式）
func (c *Config) GetNinetyDayThresholdDecimal() float64 {
	return c.NinetyDayLendRateThreshold / constants.PercentageToDecimal
}

// GetOneTwentyDayThresholdDecimal 获取120天阈值（小数格式）
func (c *Config) GetOneTwentyDayThresholdDecimal() float64 {
	return c.OneTwentyDayLendRateThreshold / constants.PercentageToDecimal
}

// GetLoanPeriod 回传实际借贷天数；若未固定设定则回传 fallback。
func (c *Config) GetLoanPeriod(fallback int) int {
	if c.LoanDays > 0 {
		return c.LoanDays
	}
	return fallback
}

// setSmartStrategyDefaults 设置智能策略参数的默认值
func (c *Config) setSmartStrategyDefaults() {
	// 如果智能策略启用但参数为零，设置建议的默认值
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
		// 如果智能策略未启用，确保参数有默认值以防止验证错误
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

// setKlineStrategyDefaults 设置K线策略参数的默认值
func (c *Config) setKlineStrategyDefaults() {
	// 如果K线策略启用但参数为空，设置默认值
	if c.EnableKlineStrategy {
		if c.KlineTimeFrame == "" {
			c.KlineTimeFrame = "15m"
		}
		if c.KlinePeriod == 0 {
			c.KlinePeriod = 24 // 6小时的15分钟K线
		}
		if c.KlineSpreadPercent == 0 {
			c.KlineSpreadPercent = 0.0 // 0%加成
		}
		if c.KlineSmoothMethod == "" {
			c.KlineSmoothMethod = "ema" // 默认使用指数移动平均
		}
	}
}

// setLendingCheckDefaults 设置借贷检查间隔的默认值
func (c *Config) setLendingCheckDefaults() {
	// 如果未设置借贷检查间隔，默认为 10 分钟
	if c.LendingCheckMinutes == 0 {
		c.LendingCheckMinutes = 10
	}
	if c.NotificationFormat == "" {
		c.NotificationFormat = "classic"
	}
}
