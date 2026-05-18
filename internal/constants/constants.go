package constants

import "time"

// API 相关常量
const (
	FundingSymbolPrefix  = "f"
	WalletTypeFunding    = "funding"
	OfferTypeLIMIT       = "LIMIT"
	OfferTypeFRRDeltaVar = "FRRDELTAVAR"
	DefaultPeriodDays    = 2
	Period30Days         = 30
	Period60Days         = 60
	Period90Days         = 90
	Period120Days        = 120
)

// 利率转换常量
const (
	DaysPerYear         = 365
	PercentageToDecimal = 100.0
	DefaultFRRDelta     = 0.0
	MinDailyRateModeFRR = "FRR"
)

// 默认配置值
const (
	DefaultPriceLevels = 25
	MaxPriceLevels     = 100 // Bitfinex 最大允许值，根据 API 文档只能是 1、25 或 100
	DefaultOrderLimit  = 3
	DefaultMinutesRun  = 15
)

// 时间相关常量
const (
	DefaultTimeout    = 30 * time.Second
	RetryDelay        = 5 * time.Second
	HourlyCheckMinute = 6
	ShutdownTimeout   = 10 * time.Second
)

// Telegram 相关常量
const (
	TelegramCommandPrefix = "/"
	MaxMessageLength      = 4096
	TelegramRetryDelay    = 3 * time.Second
	TelegramUpdateTimeout = 60 * time.Second
	MaxConcurrentMessages = 10
)

// 智能策略默认值
const (
	DefaultVolatilityThreshold     = 0.002    // 0.2% 日利率波动阈值
	DefaultMaxRateMultiplier       = 2.0      // 最大2倍基础利率
	DefaultMinRateMultiplier       = 0.8      // 最小0.8倍基础利率
	DefaultFundingBookRateUndercut = 0.000001 // Funding Book 利率下调量，百分比格式

	// 建议值范围
	RecommendedVolatilityMin = 0.001 // 保守用户建议值
	RecommendedVolatilityMax = 0.003 // 激进用户建议值
	RecommendedMaxRateMin    = 1.5   // 保守用户建议值
	RecommendedMaxRateMax    = 3.0   // 激进用户建议值
	RecommendedMinRateMin    = 0.7   // 激进用户建议值
	RecommendedMinRateMax    = 0.9   // 保守用户建议值
)

// 显示和处理限制
const (
	MaxDisplayOrders         = 5    // 最多显示的订单数量
	SmallRateChangePercent   = 0.01 // 1% 小变化阈值
	RateRangeIncreasePercent = 0.1  // 10% 利率范围增加 (默认值，可在配置中覆盖)
	MaxHistorySize           = 100  // 最大历史记录大小
	ReducedSplitsMultiplier  = 0.7  // 高波动时分割数减少倍数
)
