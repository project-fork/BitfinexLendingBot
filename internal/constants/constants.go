package constants

import "time"

// API 相關常量
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

// 利率轉換常量
const (
	DaysPerYear         = 365
	PercentageToDecimal = 100.0
	DefaultFRRDelta     = 0.0
	MinDailyRateModeFRR = "FRR"
)

// 默認配置值
const (
	DefaultPriceLevels = 25
	MaxPriceLevels     = 100 // Bitfinex 最大允許值，根據 API 文檔只能是 1、25 或 100
	DefaultOrderLimit  = 3
	DefaultMinutesRun  = 15
)

// 時間相關常量
const (
	DefaultTimeout    = 30 * time.Second
	RetryDelay        = 5 * time.Second
	HourlyCheckMinute = 6
	ShutdownTimeout   = 10 * time.Second
)

// Telegram 相關常量
const (
	TelegramCommandPrefix = "/"
	MaxMessageLength      = 4096
	TelegramRetryDelay    = 3 * time.Second
	TelegramUpdateTimeout = 60 * time.Second
	MaxConcurrentMessages = 10
)

// 智能策略預設值
const (
	DefaultVolatilityThreshold     = 0.002    // 0.2% 日利率波動閾值
	DefaultMaxRateMultiplier       = 2.0      // 最大2倍基礎利率
	DefaultMinRateMultiplier       = 0.8      // 最小0.8倍基礎利率
	DefaultFundingBookRateUndercut = 0.000001 // Funding Book 利率下調量，百分比格式

	// 建議值範圍
	RecommendedVolatilityMin = 0.001 // 保守用戶建議值
	RecommendedVolatilityMax = 0.003 // 激進用戶建議值
	RecommendedMaxRateMin    = 1.5   // 保守用戶建議值
	RecommendedMaxRateMax    = 3.0   // 激進用戶建議值
	RecommendedMinRateMin    = 0.7   // 激進用戶建議值
	RecommendedMinRateMax    = 0.9   // 保守用戶建議值
)

// 顯示和處理限制
const (
	MaxDisplayOrders         = 5    // 最多顯示的訂單數量
	SmallRateChangePercent   = 0.01 // 1% 小變化閾值
	RateRangeIncreasePercent = 0.1  // 10% 利率範圍增加 (預設值，可在配置中覆蓋)
	MaxHistorySize           = 100  // 最大歷史記錄大小
	ReducedSplitsMultiplier  = 0.7  // 高波動時分割數減少倍數
)
