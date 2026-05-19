package strategy

import (
	"log"
	"math"
	"os"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
)

// SmartStrategy 智能策略引擎
type SmartStrategy struct {
	config   *config.Config
	analyzer *MarketAnalyzer
	logger   *log.Logger
}

// NewSmartStrategy 创建智能策略引擎
func NewSmartStrategy(cfg *config.Config) *SmartStrategy {
	return &SmartStrategy{
		config:   cfg,
		analyzer: NewMarketAnalyzer(),
		logger:   log.New(os.Stderr, "", log.LstdFlags),
	}
}

// SetLogger 设置日志记录器
func (ss *SmartStrategy) SetLogger(logger *log.Logger) {
	if logger == nil {
		return
	}
	ss.logger = logger
}

func (ss *SmartStrategy) getLogger() *log.Logger {
	if ss.logger == nil {
		ss.logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return ss.logger
}

// CalculateSmartOffers 计算智能贷出订单
func (ss *SmartStrategy) CalculateSmartOffers(fundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var loanOffers []*LoanOffer

	if fundsAvailable < ss.config.MinLoan {
		return loanOffers
	}

	// 添加市场数据到分析器
	if len(fundingBook) > 0 {
		currentRate := fundingBook[0].Rate
		totalVolume := ss.calculateTotalVolume(fundingBook)
		ss.analyzer.AddRateSnapshot(currentRate, totalVolume)
	}

	// 分析市场状况
	marketCondition := ss.analyzer.AnalyzeMarket(fundingBook)
	ss.getLogger().Printf("市场状况 - 趋势: %s, 波动率: %.6f, 利率比例: %.2f",
		marketCondition.Trend, marketCondition.Volatility, marketCondition.RateRatio)

	splitFundsAvailable := fundsAvailable

	// 高额持有策略（动态利率）
	if ss.config.HighHoldAmount > ss.config.MinLoan || splitFundsAvailable >= ss.config.MinLoan {
		highHoldOffers := ss.calculateSmartHighHoldOffers(&splitFundsAvailable, marketCondition, fundingBook)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	// 对高额持有之后的剩余资金做智能分配，用于分散单深度/激进度判断。
	_, spreadRatio := ss.calculateOptimalAllocation(marketCondition)
	spreadAmount := splitFundsAvailable * spreadRatio
	ss.getLogger().Printf("剩余资金配置 - 高额持有优先后余额: %.2f, 分散贷出参考比例: %.2f%% (%.2f)",
		splitFundsAvailable, spreadRatio*100, spreadAmount)

	// 分散贷出策略（智能优化）
	if splitFundsAvailable >= ss.config.MinLoan {
		remainingSlots := ss.getRemainingOrderSlots(len(loanOffers))
		if remainingSlots != 0 {
			spreadOffers := ss.calculateSmartSpreadOffers(splitFundsAvailable, fundingBook, marketCondition, remainingSlots)
			loanOffers = append(loanOffers, spreadOffers...)
		}
	}

	return loanOffers
}

// calculateOptimalAllocation 计算最佳资金配置
func (ss *SmartStrategy) calculateOptimalAllocation(condition *MarketCondition) (highHoldRatio, spreadRatio float64) {
	baseHighHold := 0.5 // 基础50%配置

	switch condition.Trend {
	case "rising":
		// 利率上升趋势，减少固定利率配置
		baseHighHold = 0.3
	case "falling":
		// 利率下降趋势，增加固定利率配置
		baseHighHold = 0.7
	}

	// 根据波动率调整
	if condition.Volatility > ss.config.VolatilityThreshold {
		// 高波动性，偏向稳定策略
		baseHighHold += 0.1
	}

	// 根据利率比例调整
	if condition.RateRatio > 1.2 {
		// 当前利率明显高于平均，偏向锁定长期
		baseHighHold += 0.1
	} else if condition.RateRatio < 0.8 {
		// 当前利率明显低于平均，偏向灵活短期
		baseHighHold -= 0.1
	}

	// 确保在合理范围内
	baseHighHold = math.Max(0.2, math.Min(0.8, baseHighHold))

	return baseHighHold, 1.0 - baseHighHold
}

// calculateSmartHighHoldOffers 计算智能高额持有订单
func (ss *SmartStrategy) calculateSmartHighHoldOffers(splitFundsAvailable *float64, condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var offers []*LoanOffer

	ordersCount := ss.config.HighHoldOrders
	if ordersCount <= 0 {
		ordersCount = 1
	}

	highHold := ss.config.HighHoldAmount
	if *splitFundsAvailable < highHold {
		highHold = *splitFundsAvailable
	}
	if ss.config.MaxLoan > 0 && highHold > ss.config.MaxLoan {
		highHold = ss.config.MaxLoan
	}
	highHold = floorToCents(highHold)

	if highHold < ss.config.MinLoan {
		return offers
	}

	// 计算动态利率
	dynamicRate := ss.calculateDynamicHighHoldRate(condition, fundingBook)

	// 智能期间选择
	period := ss.calculateSmartPeriod(dynamicRate, condition)

	possibleOrders := int(*splitFundsAvailable / highHold)
	actualOrders := int(math.Min(float64(ordersCount), float64(possibleOrders)))

	ss.getLogger().Printf("智能高额持有 - 动态利率: %.4f%%, 期间: %d天, 订单数: %d",
		dynamicRate*100, period, actualOrders)

	for i := 0; i < actualOrders; i++ {
		if *splitFundsAvailable < highHold {
			break
		}

		offer := &LoanOffer{
			Amount: highHold,
			Rate:   dynamicRate,
			Period: period,
			UseFRR: false, // 高额持有单固定走一般利率单
		}
		offers = append(offers, offer)
		*splitFundsAvailable -= highHold
	}

	return offers
}

// calculateDynamicHighHoldRate 计算动态高额持有利率
func (ss *SmartStrategy) calculateDynamicHighHoldRate(condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) float64 {
	baseRate := ss.config.GetHighHoldRateDecimal()

	// 如果没有市场数据，使用基础利率
	if len(fundingBook) == 0 {
		return baseRate
	}

	marketRate := fundingBook[0].Rate

	// 根据市场状况调整
	switch condition.Trend {
	case "rising":
		// 利率上升趋势，提高高额持有利率但保持竞争力
		dynamicRate := math.Min(marketRate*0.85, baseRate*ss.config.MaxRateMultiplier)
		return math.Max(baseRate*ss.config.MinRateMultiplier, dynamicRate)

	case "falling":
		// 利率下降趋势，保守使用基础利率，但不低于最小倍数
		return math.Max(baseRate*ss.config.MinRateMultiplier, baseRate)

	default: // stable
		// 稳定市场，根据市场利率适度调整
		if marketRate > baseRate*1.5 {
			adjustedRate := math.Min(marketRate*0.8, baseRate*ss.config.MaxRateMultiplier)
			return math.Max(baseRate*ss.config.MinRateMultiplier, adjustedRate)
		}
		return math.Max(baseRate*ss.config.MinRateMultiplier, baseRate)
	}
}

// calculateSmartSpreadOffers 计算智能分散贷出订单
func (ss *SmartStrategy) calculateSmartSpreadOffers(splitFundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry, condition *MarketCondition, maxOrders int) []*LoanOffer {
	var offers []*LoanOffer
	useFRR := ss.config.IsMinDailyLendRateFRR()

	numSplits := ss.config.SpreadLend
	if maxOrders > 0 && numSplits > maxOrders {
		numSplits = maxOrders
	}
	if numSplits <= 0 || splitFundsAvailable < ss.config.MinLoan {
		return offers
	}

	// 根据市场状况调整分散单最大目标笔数
	if condition.Volatility > ss.config.VolatilityThreshold {
		// 高波动时减少分割，提升竞争力
		numSplits = int(float64(numSplits) * constants.ReducedSplitsMultiplier)
	}

	orderAmounts := buildOrderAmounts(splitFundsAvailable, numSplits, ss.config.MinLoan, ss.config.MaxLoan)
	if len(orderAmounts) == 0 {
		return offers
	}

	// 动态深度范围
	gapBottom, gapTop := ss.analyzer.GetOptimalDepthRange(splitFundsAvailable, condition)

	// 计算利率递增量
	gapClimb := (gapTop - gapBottom) / float64(len(orderAmounts))
	nextLend := gapBottom

	minDailyRate := ss.config.GetMinDailyRateDecimal()

	ss.getLogger().Printf("智能分散策略 - 实际分散笔数: %d, 深度范围: %.0f-%.0f, Funding Book数据: %d笔",
		len(orderAmounts), gapBottom, gapTop, len(fundingBook))

	orderIndex := 0 // 订单索引，用于确保每个订单有不同的索引
	totalOriginalSplits := len(orderAmounts)

	for _, allocAmount := range orderAmounts {
		var currentDepthIndex int

		if len(fundingBook) > 0 {
			// 使用订单索引均匀分布在 funding book 中
			// 使用原始分割数来计算，而不是递减中的 numSplits
			if totalOriginalSplits > 1 {
				currentDepthIndex = (orderIndex * (len(fundingBook) - 1)) / (totalOriginalSplits - 1)
			} else {
				currentDepthIndex = 0
			}

			// 确保索引在有效范围内
			if currentDepthIndex >= len(fundingBook) {
				currentDepthIndex = len(fundingBook) - 1
			}
			if currentDepthIndex < 0 {
				currentDepthIndex = 0
			}
		} else {
			// 没有funding book时，使用订单索引作为虚拟深度
			currentDepthIndex = orderIndex
		}

		if allocAmount < ss.config.MinLoan {
			break
		}

		// 智能利率计算 - 基于 funding book 数据创建递增利率序列
		rate := ss.calculateProgressiveRate(fundingBook, minDailyRate, condition, orderIndex, totalOriginalSplits)

		// 智能期间选择
		period := ss.calculateSmartPeriod(rate, condition)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR, // 分散单依 MIN_DAILY_LEND_RATE 是否为 FRR 决定
		}
		offers = append(offers, offer)

		ss.getLogger().Printf("智能订单 #%d - 利率: %.6f%%, 金额: %.2f, 期间: %d天, 深度索引: %d",
			len(offers), rate*100, allocAmount, period, currentDepthIndex)

		nextLend += gapClimb
		orderIndex++ // 增加订单索引确保下一个订单有不同的深度索引
	}

	return offers
}

func (ss *SmartStrategy) getRemainingOrderSlots(existingOffers int) int {
	if ss.config.OrderLimit <= 0 {
		return -1
	}

	remaining := ss.config.OrderLimit - existingOffers
	if remaining < 0 {
		return 0
	}

	return remaining
}

// calculateSmartRate 计算智能利率
func (ss *SmartStrategy) calculateSmartRate(depthIndex int, fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int) float64 {
	var rate float64

	if len(fundingBook) > 0 && depthIndex < len(fundingBook) {
		// 使用实际市场数据
		marketRate := fundingBook[depthIndex].Rate

		// 竞争分析优化
		competitiveRate := ss.analyzer.AnalyzeCompetition(fundingBook)
		if competitiveRate > 0 && competitiveRate > marketRate {
			marketRate = competitiveRate
		}

		// 如果市场利率低于最小利率，使用最小利率作为基础，但添加订单索引递增
		if marketRate < minDailyRate {
			// 使用最小利率 + 基于订单索引的小幅递增来确保差异化
			rate = minDailyRate + (minDailyRate * 0.01 * float64(orderIndex))
		} else {
			rate = marketRate
		}

		ss.getLogger().Printf("市场数据利率计算 - 深度索引: %d, 市场利率: %.6f%%, 最终利率: %.6f%%",
			depthIndex, fundingBook[depthIndex].Rate*100, rate*100)
	} else {
		// 深度超出范围时，使用合成利率
		rate = ss.calculateSyntheticRate(depthIndex, minDailyRate, condition)
	}

	// 根据市场趋势微调
	switch condition.Trend {
	case "rising":
		// 利率上升时稍微提高利率保持竞争力
		rate *= 1.01
	case "falling":
		// 利率下降时保持原利率
		break
	}

	return rate
}

// calculateProgressiveRate 计算递增利率序列
func (ss *SmartStrategy) calculateProgressiveRate(fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int, totalOrders int) float64 {
	if len(fundingBook) == 0 {
		// 无市场数据时使用合成利率
		return ss.calculateSyntheticRate(orderIndex, minDailyRate, condition)
	}

	// 分析 funding book 中的利率分布
	var rates []float64
	for _, entry := range fundingBook {
		if entry.Rate >= minDailyRate {
			rates = append(rates, entry.Rate)
		}
	}

	if len(rates) == 0 {
		// 没有符合最小利率的数据，使用合成利率
		baseRate := minDailyRate
		increment := baseRate * ss.config.RateRangeIncreasePercent * float64(orderIndex)
		return baseRate + increment
	}

	// 找出利率范围
	minRate := rates[0]
	maxRate := rates[0]
	for _, rate := range rates {
		if rate < minRate {
			minRate = rate
		}
		if rate > maxRate {
			maxRate = rate
		}
	}

	ss.getLogger().Printf("Funding Book 利率分析 - 有效利率数量: %d, 原始范围: %.6f%%-%.6f%%",
		len(rates), minRate*100, maxRate*100)

	// 确保最小利率不低于配置的最小利率
	if minRate < minDailyRate {
		minRate = minDailyRate
	}

	// 创建递增利率序列
	if totalOrders > 1 {
		rateRange := maxRate - minRate

		// 如果利率范围太小（所有利率相同），则人工创建递增范围
		if rateRange < minRate*constants.SmallRateChangePercent {
			// 使用基础利率创建递增范围
			maxRate = minRate * (1.0 + ss.config.RateRangeIncreasePercent)
			rateRange = maxRate - minRate
			ss.getLogger().Printf("利率范围太小，使用人工范围: %.6f%%-%.6f%%", minRate*100, maxRate*100)
		}

		step := rateRange / float64(totalOrders-1)
		progressiveRate := minRate + step*float64(orderIndex)

		ss.getLogger().Printf("递增利率计算 - 订单索引: %d, 利率范围: %.6f%%-%.6f%%, 步长: %.6f%%, 递增利率: %.6f%%",
			orderIndex, minRate*100, maxRate*100, step*100, progressiveRate*100)

		return ss.undercutFundingBookRate(progressiveRate, minDailyRate)
	} else {
		return ss.undercutFundingBookRate(minRate, minDailyRate)
	}
}

func (ss *SmartStrategy) undercutFundingBookRate(rate float64, minDailyRate float64) float64 {
	// Funding book rates are decimal daily rates. The config uses percentage format,
	// so FUNDING_BOOK_RATE_UNDERCUT=0.000001 makes 0.030137% become 0.030136%.
	undercut := ss.config.FundingBookRateUndercut / constants.PercentageToDecimal
	return math.Max(minDailyRate, rate-undercut)
}

// calculateSyntheticRate 计算合成利率（当无市场数据时）
func (ss *SmartStrategy) calculateSyntheticRate(depthIndex int, minDailyRate float64, condition *MarketCondition) float64 {
	// 基于深度索引创建利率阶梯
	// 利率范围：最小利率 到 最小利率 × 配置的最大倍数
	maxRate := minDailyRate * ss.config.MaxRateMultiplier

	// 根据市场状况调整利率范围
	switch condition.Trend {
	case "rising":
		// 利率上升趋势，使用更积极的利率范围
		maxRate = minDailyRate * (ss.config.MaxRateMultiplier * 0.8)
	case "falling":
		// 利率下降趋势，使用更保守的利率范围
		maxRate = minDailyRate * (ss.config.MaxRateMultiplier * 1.2)
	}

	// 根据深度索引计算利率增量，确保每个索引都有不同的利率
	// 使用更细致的阶梯，确保差异化
	totalSteps := 20.0 // 20个基础层级
	rateStep := (maxRate - minDailyRate) / totalSteps

	// 为每个深度索引计算唯一利率
	syntheticRate := minDailyRate + float64(depthIndex)*rateStep

	// 添加微小的随机变化以确保完全不同
	// 基于深度索引的位置添加细微调整
	microAdjustment := rateStep * 0.1 * float64(depthIndex%3) / 3.0
	syntheticRate += microAdjustment

	// 确保在合理范围内
	if syntheticRate < minDailyRate {
		syntheticRate = minDailyRate
	}
	if syntheticRate > maxRate {
		syntheticRate = maxRate
	}

	ss.getLogger().Printf("合成利率计算 - 深度索引: %d, 基础利率: %.6f%%, 合成利率: %.6f%%, 趋势: %s",
		depthIndex, minDailyRate*100, syntheticRate*100, condition.Trend)

	return syntheticRate
}

// calculateSmartPeriod 计算智能期间
func (ss *SmartStrategy) calculateSmartPeriod(dailyRate float64, condition *MarketCondition) int {
	if ss.config.LoanDays > 0 {
		return ss.config.LoanDays
	}

	oneTwentyThreshold := ss.config.GetOneTwentyDayThresholdDecimal()
	ninetyThreshold := ss.config.GetNinetyDayThresholdDecimal()
	sixtyThreshold := ss.config.GetSixtyDayThresholdDecimal()
	thirtyThreshold := ss.config.GetThirtyDayThresholdDecimal()

	// 基础期间逻辑
	basePeriod := constants.DefaultPeriodDays
	if ss.config.OneTwentyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, oneTwentyThreshold) {
		basePeriod = constants.Period120Days
	} else if ss.config.NinetyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, ninetyThreshold) {
		basePeriod = constants.Period90Days
	} else if ss.config.SixtyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, sixtyThreshold) {
		basePeriod = constants.Period60Days
	} else if ss.config.ThirtyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, thirtyThreshold) {
		basePeriod = constants.Period30Days
	}

	// 根据市场状况智能调整
	switch condition.Trend {
	case "rising":
		// 利率上升趋势，偏向短期以便重新定价
		if basePeriod > constants.Period30Days {
			basePeriod = constants.Period30Days
		} else if basePeriod == constants.Period30Days {
			basePeriod = constants.DefaultPeriodDays
		}

	case "falling":
		// 利率下降趋势，锁定当前较高利率
		if dailyRate > condition.AvgRate*1.1 && basePeriod == constants.DefaultPeriodDays {
			basePeriod = constants.Period30Days
		} else if dailyRate > condition.AvgRate*1.1 && basePeriod == constants.Period30Days {
			basePeriod = constants.Period60Days
		}
	}

	// 高波动环境偏向短期 (使用配置的波动率阈值)
	if condition.Volatility > ss.config.VolatilityThreshold*1.5 && basePeriod > constants.Period30Days {
		basePeriod = constants.Period30Days
	}

	return basePeriod
}

// calculateTotalVolume 计算总成交量
func (ss *SmartStrategy) calculateTotalVolume(fundingBook []*bitfinex.FundingBookEntry) float64 {
	var totalVolume float64
	maxEntries := 10 // 只计算前10层，基于市场深度分析最佳实践

	for i, entry := range fundingBook {
		if i >= maxEntries {
			break
		}
		totalVolume += math.Abs(entry.Amount)
	}

	return totalVolume
}
