package strategy

import (
	"math"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
)

// MarketAnalyzer 市场分析器
type MarketAnalyzer struct {
	rateHistory    []RateSnapshot
	maxHistorySize int
}

// RateSnapshot 利率快照
type RateSnapshot struct {
	Rate      float64
	Timestamp time.Time
	Volume    float64
}

// MarketCondition 市场状况
type MarketCondition struct {
	Trend          string  // "rising", "falling", "stable"
	Volatility     float64 // 波动率
	LiquidityDepth int     // 流动性深度
	AvgRate        float64 // 平均利率
	RateRatio      float64 // 当前利率/平均利率
}

// NewMarketAnalyzer 创建市场分析器
func NewMarketAnalyzer() *MarketAnalyzer {
	return &MarketAnalyzer{
		rateHistory:    make([]RateSnapshot, 0),
		maxHistorySize: 48, // 保留48个数据点 (12小时，每15分钟一次)
	}
}

// AddRateSnapshot 添加利率快照
func (ma *MarketAnalyzer) AddRateSnapshot(rate float64, volume float64) {
	snapshot := RateSnapshot{
		Rate:      rate,
		Timestamp: time.Now(),
		Volume:    volume,
	}

	ma.rateHistory = append(ma.rateHistory, snapshot)

	// 保持历史数据大小限制
	if len(ma.rateHistory) > ma.maxHistorySize {
		ma.rateHistory = ma.rateHistory[1:]
	}
}

// AnalyzeMarket 分析市场状况
func (ma *MarketAnalyzer) AnalyzeMarket(fundingBook []*bitfinex.FundingBookEntry) *MarketCondition {
	if len(ma.rateHistory) < 3 {
		// 数据不足，返回默认状况
		return &MarketCondition{
			Trend:          "stable",
			Volatility:     0.0,
			LiquidityDepth: len(fundingBook),
			AvgRate:        0.0,
			RateRatio:      1.0,
		}
	}

	avgRate := ma.calculateAverageRate()
	volatility := ma.calculateVolatility()
	trend := ma.determineTrend()
	currentRate := ma.rateHistory[len(ma.rateHistory)-1].Rate
	rateRatio := 1.0
	if avgRate > 0 {
		rateRatio = currentRate / avgRate
	}

	return &MarketCondition{
		Trend:          trend,
		Volatility:     volatility,
		LiquidityDepth: len(fundingBook),
		AvgRate:        avgRate,
		RateRatio:      rateRatio,
	}
}

// calculateAverageRate 计算平均利率
func (ma *MarketAnalyzer) calculateAverageRate() float64 {
	if len(ma.rateHistory) == 0 {
		return 0.0
	}

	var sum float64
	for _, snapshot := range ma.rateHistory {
		sum += snapshot.Rate
	}

	return sum / float64(len(ma.rateHistory))
}

// calculateVolatility 计算波动率
func (ma *MarketAnalyzer) calculateVolatility() float64 {
	if len(ma.rateHistory) < 2 {
		return 0.0
	}

	avgRate := ma.calculateAverageRate()
	var sumSquaredDiff float64

	for _, snapshot := range ma.rateHistory {
		diff := snapshot.Rate - avgRate
		sumSquaredDiff += diff * diff
	}

	variance := sumSquaredDiff / float64(len(ma.rateHistory))
	return math.Sqrt(variance)
}

// determineTrend 判断趋势
func (ma *MarketAnalyzer) determineTrend() string {
	if len(ma.rateHistory) < 6 {
		return "stable"
	}

	// 取最近6个点进行趋势分析
	recentHistory := ma.rateHistory[len(ma.rateHistory)-6:]

	var upCount, downCount int
	for i := 1; i < len(recentHistory); i++ {
		diff := recentHistory[i].Rate - recentHistory[i-1].Rate
		threshold := 0.0001 // 0.01%的变化阈值

		if diff > threshold {
			upCount++
		} else if diff < -threshold {
			downCount++
		}
	}

	if upCount >= 4 {
		return "rising"
	} else if downCount >= 4 {
		return "falling"
	}

	return "stable"
}

// AnalyzeCompetition 分析竞争对手
func (ma *MarketAnalyzer) AnalyzeCompetition(fundingBook []*bitfinex.FundingBookEntry) float64 {
	if len(fundingBook) < 10 {
		return 0.0
	}

	// 分析前10层的平均利率差
	var totalSpread float64
	validSpreads := 0

	for i := 0; i < 9 && i < len(fundingBook)-1; i++ {
		spread := fundingBook[i+1].Rate - fundingBook[i].Rate
		if spread > 0 {
			totalSpread += spread
			validSpreads++
		}
	}

	if validSpreads == 0 {
		return 0.0
	}

	avgSpread := totalSpread / float64(validSpreads)

	// 建议利率：略高于当前最佳利率
	return fundingBook[0].Rate + avgSpread*0.3
}

// GetOptimalDepthRange 获取最佳深度范围
func (ma *MarketAnalyzer) GetOptimalDepthRange(fundsAvailable float64, condition *MarketCondition) (bottom, top float64) {
	// 基础范围根据资金量调整
	baseBottom := 10.0
	baseTop := 1000.0

	if fundsAvailable > 1000 {
		baseBottom = 5.0
		baseTop = 3000.0
	} else if fundsAvailable > 500 {
		baseBottom = 8.0
		baseTop = 2000.0
	}

	// 根据市场状况调整
	switch condition.Trend {
	case "rising":
		// 利率上升时缩小范围，提升竞争力
		baseBottom *= 1.2
		baseTop *= 0.8
	case "falling":
		// 利率下降时扩大范围，分散风险
		baseBottom *= 0.8
		baseTop *= 1.2
	}

	// 根据波动率调整 (使用配置的波动率阈值)
	if condition.Volatility > 0.001 { // 高波动
		baseTop *= 1.3 // 扩大范围应对波动
	}

	return baseBottom, baseTop
}
