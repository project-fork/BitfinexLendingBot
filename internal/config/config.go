package config

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

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
	MinDailyLendRate     any             `mapstructure:"MIN_DAILY_LEND_RATE"` // 支持数值或 "FRR"
	SpreadLend           int             `mapstructure:"SPREAD_LEND"`         // 分散单最大目标笔数
	GapBottom            float64         `mapstructure:"GAP_BOTTOM"`
	GapTop               float64         `mapstructure:"GAP_TOP"`
	LoanPeriodThresholds map[int]float64 `mapstructure:"LOAN_PERIOD_THRESHOLDS"` // key=天数, value=触发该天数的日利率阈值（百分比）
	RateBonus            float64         `mapstructure:"RATE_BONUS"`

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
	DailyEarningsReport DailyEarningsReportConfig `mapstructure:"DAILY_EARNINGS_REPORT"`

	// 策略设定
	Strategy                 string  `mapstructure:"STRATEGY"`
	VolatilityThreshold      float64 `mapstructure:"VOLATILITY_THRESHOLD"`
	MaxRateMultiplier        float64 `mapstructure:"MAX_RATE_MULTIPLIER"`
	MinRateMultiplier        float64 `mapstructure:"MIN_RATE_MULTIPLIER"`
	FundingBookRateUndercut  float64 `mapstructure:"FUNDING_BOOK_RATE_UNDERCUT"`  // Funding Book 利率下调量，百分比格式
	RateRangeIncreasePercent float64 `mapstructure:"RATE_RANGE_INCREASE_PERCENT"` // 利率范围增加百分比

	// K线策略设定
	KlineTimeFrame     string  `mapstructure:"KLINE_TIME_FRAME"`     // K线时间框架，默认15m
	KlinePeriod        int     `mapstructure:"KLINE_PERIOD"`         // K线周期数量，默认24（6小时）
	KlineSpreadPercent float64 `mapstructure:"KLINE_SPREAD_PERCENT"` // K线最高点加成百分比，默认0%
	KlineSmoothMethod  string  `mapstructure:"KLINE_SMOOTH_METHOD"`  // K线利率平滑方法：max, sma, ema, hla, p90

	// 测试模式设定
	TestMode bool `mapstructure:"TEST_MODE"`

	// 借贷通知设定
	LastLendingCheckTime int64   // 上次检查借贷订单的时间戳
	LastAvailableBalance float64 // 上次检查时的可用余额
	LendingCheckMinutes  int     `mapstructure:"LENDING_CHECK_MINUTES"` // 借贷订单检查间隔（分钟）
	SeenFundingCreditIDs map[int64]struct{}

	// 运行期风控保护
	ExecutionCooldownSeconds int     `mapstructure:"EXECUTION_COOLDOWN_SECONDS"`    // 主策略执行冷却时间（秒）
	MinExecutableFunds       float64 `mapstructure:"MIN_EXECUTABLE_FUNDS"`          // 本轮允许继续生成/提交订单的最小总资金阈值
	OrderFingerprintTTL      int     `mapstructure:"ORDER_FINGERPRINT_TTL_SECONDS"` // 相同订单指纹的幂等保护窗口（秒）
	IncludeManualPendingOffersInStrategyFunds bool `mapstructure:"INCLUDE_MANUAL_PENDING_OFFERS_IN_STRATEGY_FUNDS"` // 统一未成交订单接口是否包含手动挂单
}

type DailyEarningsReportConfig struct {
	Enabled     *bool  `mapstructure:"ENABLED"`
	TriggerTime string `mapstructure:"TRIGGER_TIME"`
	Timezone    string `mapstructure:"TIMEZONE"`
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

	// 设置收益报告调度默认值
	config.setDailyEarningsDefaults()

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
	for days, threshold := range c.LoanPeriodThresholds {
		if days < 2 || days > constants.Period120Days {
			return errors.NewValidationError("LOAN_PERIOD_THRESHOLDS keys must be between 2 and 120")
		}
		if threshold <= 0 {
			return errors.NewValidationError("LOAN_PERIOD_THRESHOLDS values must be positive")
		}
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

	switch c.GetStrategy() {
	case StrategyTraditional, StrategySimple, StrategySmart, StrategyKline:
	default:
		return errors.NewValidationError("STRATEGY must be one of: traditional, simple, smart, kline")
	}

	// 验证智能/简单策略参数
	if c.UsesAdaptiveStrategyParams() {
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
	if c.IsKlineStrategy() {
		if c.KlineTimeFrame == "" {
			return errors.NewValidationError("KLINE_TIME_FRAME is required when STRATEGY is kline")
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
	if c.ExecutionCooldownSeconds < 0 {
		return errors.NewValidationError("EXECUTION_COOLDOWN_SECONDS must be non-negative")
	}
	if c.MinExecutableFunds < 0 {
		return errors.NewValidationError("MIN_EXECUTABLE_FUNDS must be non-negative")
	}
	if c.OrderFingerprintTTL < 0 {
		return errors.NewValidationError("ORDER_FINGERPRINT_TTL_SECONDS must be non-negative")
	}
	if c.NotificationFormat != "" && c.NotificationFormat != "classic" && c.NotificationFormat != "aligned" {
		return errors.NewValidationError("NOTIFICATION_FORMAT must be one of: classic, aligned")
	}
	if _, _, err := c.GetDailyEarningsTriggerClock(); err != nil {
		return err
	}
	if _, err := c.GetDailyEarningsLocation(); err != nil {
		return errors.NewValidationError("DAILY_EARNINGS_REPORT.TIMEZONE must be a valid IANA time zone")
	}

	return nil
}

// GetFundingSymbol 获取 funding symbol
func (c *Config) GetFundingSymbol() string {
	return constants.FundingSymbolPrefix + strings.ToUpper(c.Currency)
}

const (
	StrategyTraditional = "traditional"
	StrategySimple      = "simple"
	StrategySmart       = "smart"
	StrategyKline       = "kline"
)

func (c *Config) GetStrategy() string {
	strategy := strings.ToLower(strings.TrimSpace(c.Strategy))
	if strategy == "" {
		return StrategyTraditional
	}
	return strategy
}

func (c *Config) SetStrategy(strategy string) {
	c.Strategy = strings.ToLower(strings.TrimSpace(strategy))
}

func (c *Config) IsTraditionalStrategy() bool {
	return c.GetStrategy() == StrategyTraditional
}

func (c *Config) IsSimpleStrategy() bool {
	return c.GetStrategy() == StrategySimple
}

func (c *Config) IsSmartStrategy() bool {
	return c.GetStrategy() == StrategySmart
}

func (c *Config) IsKlineStrategy() bool {
	return c.GetStrategy() == StrategyKline
}

func (c *Config) HasTelegramBotToken() bool {
	return strings.TrimSpace(c.TelegramBotToken) != ""
}

func (c *Config) HasTelegramAuthToken() bool {
	return strings.TrimSpace(c.TelegramAuthToken) != ""
}

func (c *Config) IsTelegramEnabled() bool {
	return c.HasTelegramBotToken() && c.HasTelegramAuthToken()
}

func (c *Config) IsDailyEarningsReportEnabled() bool {
	if c == nil || c.DailyEarningsReport.Enabled == nil {
		return true
	}
	return *c.DailyEarningsReport.Enabled
}

func (c *Config) GetDailyEarningsTriggerTime() string {
	if c == nil {
		return "09:35"
	}
	triggerTime := strings.TrimSpace(c.DailyEarningsReport.TriggerTime)
	if triggerTime == "" {
		return "09:35"
	}
	return triggerTime
}

func (c *Config) GetDailyEarningsTimezone() string {
	if c == nil {
		return "Asia/Shanghai"
	}
	timezone := strings.TrimSpace(c.DailyEarningsReport.Timezone)
	if timezone == "" {
		return "Asia/Shanghai"
	}
	return timezone
}

func (c *Config) GetDailyEarningsLocation() (*time.Location, error) {
	return time.LoadLocation(c.GetDailyEarningsTimezone())
}

func (c *Config) GetDailyEarningsTriggerClock() (int, int, error) {
	triggerTime := c.GetDailyEarningsTriggerTime()
	parts := strings.Split(triggerTime, ":")
	if len(parts) != 2 {
		return 0, 0, errors.NewValidationError("DAILY_EARNINGS_REPORT.TRIGGER_TIME must be in HH:MM format")
	}
	if len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, errors.NewValidationError("DAILY_EARNINGS_REPORT.TRIGGER_TIME must be in HH:MM format")
	}

	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, errors.NewValidationError("DAILY_EARNINGS_REPORT.TRIGGER_TIME must be in HH:MM format")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, errors.NewValidationError("DAILY_EARNINGS_REPORT.TRIGGER_TIME must be in HH:MM format")
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, errors.NewValidationError("DAILY_EARNINGS_REPORT.TRIGGER_TIME must be in HH:MM format")
	}

	return hour, minute, nil
}

func (c *Config) TelegramDisabledReason() string {
	hasBotToken := c.HasTelegramBotToken()
	hasAuthToken := c.HasTelegramAuthToken()

	switch {
	case hasBotToken && hasAuthToken:
		return ""
	case !hasBotToken && !hasAuthToken:
		return "未配置 TELEGRAM_BOT_TOKEN 和 TELEGRAM_AUTH_TOKEN"
	case !hasBotToken:
		return "缺少 TELEGRAM_BOT_TOKEN"
	default:
		return "缺少 TELEGRAM_AUTH_TOKEN"
	}
}

func (c *Config) UsesAdaptiveStrategyParams() bool {
	return c.IsSimpleStrategy() || c.IsSmartStrategy()
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

// GetLoanPeriod 回传实际借贷天数；若未固定设定则回传 fallback。
func (c *Config) GetLoanPeriod(fallback int) int {
	if c.LoanDays > 0 {
		return c.LoanDays
	}
	return fallback
}

type LoanPeriodThreshold struct {
	Days             int
	ThresholdPercent float64
	ThresholdDecimal float64
}

func (c *Config) GetSortedLoanPeriodThresholdsDesc() []LoanPeriodThreshold {
	result := make([]LoanPeriodThreshold, 0, len(c.LoanPeriodThresholds))
	for days, threshold := range c.LoanPeriodThresholds {
		if days < 2 || days > constants.Period120Days || threshold <= 0 {
			continue
		}
		result = append(result, LoanPeriodThreshold{
			Days:             days,
			ThresholdPercent: threshold,
			ThresholdDecimal: threshold / constants.PercentageToDecimal,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Days > result[j].Days
	})

	return result
}

// setSmartStrategyDefaults 设置智能策略参数的默认值
func (c *Config) setSmartStrategyDefaults() {
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

// setKlineStrategyDefaults 设置K线策略参数的默认值
func (c *Config) setKlineStrategyDefaults() {
	// 如果K线策略启用但参数为空，设置默认值
	if c.IsKlineStrategy() {
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
	if c.ExecutionCooldownSeconds == 0 {
		c.ExecutionCooldownSeconds = 30
	}
	if c.OrderFingerprintTTL == 0 {
		c.OrderFingerprintTTL = 120
	}
	if c.NotificationFormat == "" {
		c.NotificationFormat = "classic"
	}
}

func (c *Config) setDailyEarningsDefaults() {
	if c.DailyEarningsReport.Enabled == nil {
		enabled := true
		c.DailyEarningsReport.Enabled = &enabled
	}
	if strings.TrimSpace(c.DailyEarningsReport.TriggerTime) == "" {
		c.DailyEarningsReport.TriggerTime = "09:35"
	}
	if strings.TrimSpace(c.DailyEarningsReport.Timezone) == "" {
		c.DailyEarningsReport.Timezone = "Asia/Shanghai"
	}
}
