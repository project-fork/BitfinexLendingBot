package strategy

import (
	"log"
	"math"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
)

// SmartStrategy 智能策略引擎
type SmartStrategy struct {
	config   *config.Config
	analyzer *MarketAnalyzer
}

// NewSmartStrategy 創建智能策略引擎
func NewSmartStrategy(cfg *config.Config) *SmartStrategy {
	return &SmartStrategy{
		config:   cfg,
		analyzer: NewMarketAnalyzer(),
	}
}

// CalculateSmartOffers 計算智能貸出訂單
func (ss *SmartStrategy) CalculateSmartOffers(fundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var loanOffers []*LoanOffer

	if fundsAvailable < ss.config.MinLoan {
		return loanOffers
	}

	// 添加市場數據到分析器
	if len(fundingBook) > 0 {
		currentRate := fundingBook[0].Rate
		totalVolume := ss.calculateTotalVolume(fundingBook)
		ss.analyzer.AddRateSnapshot(currentRate, totalVolume)
	}

	// 分析市場狀況
	marketCondition := ss.analyzer.AnalyzeMarket(fundingBook)
	log.Printf("市場狀況 - 趨勢: %s, 波動率: %.6f, 利率比例: %.2f",
		marketCondition.Trend, marketCondition.Volatility, marketCondition.RateRatio)

	// 動態資金配置
	highHoldRatio, spreadRatio := ss.calculateOptimalAllocation(marketCondition)

	splitFundsAvailable := fundsAvailable
	highHoldAmount := fundsAvailable * highHoldRatio
	spreadAmount := fundsAvailable * spreadRatio

	log.Printf("資金配置 - 高額持有: %.2f%% (%.2f), 分散貸出: %.2f%% (%.2f)",
		highHoldRatio*100, highHoldAmount, spreadRatio*100, spreadAmount)

	// 高額持有策略（動態利率）
	if ss.config.HighHoldAmount > ss.config.MinLoan && highHoldAmount >= ss.config.HighHoldAmount {
		highHoldOffers := ss.calculateSmartHighHoldOffers(&splitFundsAvailable, marketCondition, fundingBook)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	// 分散貸出策略（智能優化）
	if splitFundsAvailable >= ss.config.MinLoan {
		remainingSlots := ss.getRemainingOrderSlots(len(loanOffers))
		if remainingSlots != 0 {
			spreadOffers := ss.calculateSmartSpreadOffers(splitFundsAvailable, fundingBook, marketCondition, remainingSlots)
			loanOffers = append(loanOffers, spreadOffers...)
		}
	}

	return loanOffers
}

// calculateOptimalAllocation 計算最佳資金配置
func (ss *SmartStrategy) calculateOptimalAllocation(condition *MarketCondition) (highHoldRatio, spreadRatio float64) {
	baseHighHold := 0.5 // 基礎50%配置

	switch condition.Trend {
	case "rising":
		// 利率上升趨勢，減少固定利率配置
		baseHighHold = 0.3
	case "falling":
		// 利率下降趨勢，增加固定利率配置
		baseHighHold = 0.7
	}

	// 根據波動率調整
	if condition.Volatility > ss.config.VolatilityThreshold {
		// 高波動性，偏向穩定策略
		baseHighHold += 0.1
	}

	// 根據利率比例調整
	if condition.RateRatio > 1.2 {
		// 當前利率明顯高於平均，偏向鎖定長期
		baseHighHold += 0.1
	} else if condition.RateRatio < 0.8 {
		// 當前利率明顯低於平均，偏向靈活短期
		baseHighHold -= 0.1
	}

	// 確保在合理範圍內
	baseHighHold = math.Max(0.2, math.Min(0.8, baseHighHold))

	return baseHighHold, 1.0 - baseHighHold
}

// calculateSmartHighHoldOffers 計算智能高額持有訂單
func (ss *SmartStrategy) calculateSmartHighHoldOffers(splitFundsAvailable *float64, condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var offers []*LoanOffer

	ordersCount := ss.config.HighHoldOrders
	if ordersCount <= 0 {
		ordersCount = 1
	}

	highHold := ss.config.HighHoldAmount
	if ss.config.MaxLoan > 0 && highHold > ss.config.MaxLoan {
		highHold = ss.config.MaxLoan
	}
	highHold = floorToCents(highHold)

	// 計算動態利率
	dynamicRate := ss.calculateDynamicHighHoldRate(condition, fundingBook)

	// 智能期間選擇
	period := ss.calculateSmartPeriod(dynamicRate, condition)

	possibleOrders := int(*splitFundsAvailable / highHold)
	actualOrders := int(math.Min(float64(ordersCount), float64(possibleOrders)))

	log.Printf("智能高額持有 - 動態利率: %.4f%%, 期間: %d天, 訂單數: %d",
		dynamicRate*100, period, actualOrders)

	for i := 0; i < actualOrders; i++ {
		if *splitFundsAvailable < highHold {
			break
		}

		offer := &LoanOffer{
			Amount: highHold,
			Rate:   dynamicRate,
			Period: period,
			UseFRR: false, // 高額持有單固定走一般利率單
		}
		offers = append(offers, offer)
		*splitFundsAvailable -= highHold
	}

	return offers
}

// calculateDynamicHighHoldRate 計算動態高額持有利率
func (ss *SmartStrategy) calculateDynamicHighHoldRate(condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) float64 {
	baseRate := ss.config.GetHighHoldRateDecimal()

	// 如果沒有市場數據，使用基礎利率
	if len(fundingBook) == 0 {
		return baseRate
	}

	marketRate := fundingBook[0].Rate

	// 根據市場狀況調整
	switch condition.Trend {
	case "rising":
		// 利率上升趨勢，提高高額持有利率但保持競爭力
		dynamicRate := math.Min(marketRate*0.85, baseRate*ss.config.MaxRateMultiplier)
		return math.Max(baseRate*ss.config.MinRateMultiplier, dynamicRate)

	case "falling":
		// 利率下降趨勢，保守使用基礎利率，但不低於最小倍數
		return math.Max(baseRate*ss.config.MinRateMultiplier, baseRate)

	default: // stable
		// 穩定市場，根據市場利率適度調整
		if marketRate > baseRate*1.5 {
			adjustedRate := math.Min(marketRate*0.8, baseRate*ss.config.MaxRateMultiplier)
			return math.Max(baseRate*ss.config.MinRateMultiplier, adjustedRate)
		}
		return math.Max(baseRate*ss.config.MinRateMultiplier, baseRate)
	}
}

// calculateSmartSpreadOffers 計算智能分散貸出訂單
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

	// 根據市場狀況調整分散單最大目標筆數
	if condition.Volatility > ss.config.VolatilityThreshold {
		// 高波動時減少分割，提升競爭力
		numSplits = int(float64(numSplits) * constants.ReducedSplitsMultiplier)
	}

	orderAmounts := buildOrderAmounts(splitFundsAvailable, numSplits, ss.config.MinLoan, ss.config.MaxLoan)
	if len(orderAmounts) == 0 {
		return offers
	}

	// 動態深度範圍
	gapBottom, gapTop := ss.analyzer.GetOptimalDepthRange(splitFundsAvailable, condition)

	// 計算利率遞增量
	gapClimb := (gapTop - gapBottom) / float64(len(orderAmounts))
	nextLend := gapBottom

	minDailyRate := ss.config.GetMinDailyRateDecimal()

	log.Printf("智能分散策略 - 實際分散筆數: %d, 深度範圍: %.0f-%.0f, Funding Book數據: %d筆",
		len(orderAmounts), gapBottom, gapTop, len(fundingBook))

	orderIndex := 0 // 訂單索引，用於確保每個訂單有不同的索引
	totalOriginalSplits := len(orderAmounts)

	for _, allocAmount := range orderAmounts {
		var currentDepthIndex int

		if len(fundingBook) > 0 {
			// 使用訂單索引均勻分布在 funding book 中
			// 使用原始分割數來計算，而不是遞減中的 numSplits
			if totalOriginalSplits > 1 {
				currentDepthIndex = (orderIndex * (len(fundingBook) - 1)) / (totalOriginalSplits - 1)
			} else {
				currentDepthIndex = 0
			}

			// 確保索引在有效範圍內
			if currentDepthIndex >= len(fundingBook) {
				currentDepthIndex = len(fundingBook) - 1
			}
			if currentDepthIndex < 0 {
				currentDepthIndex = 0
			}
		} else {
			// 沒有funding book時，使用訂單索引作為虛擬深度
			currentDepthIndex = orderIndex
		}

		if allocAmount < ss.config.MinLoan {
			break
		}

		// 智能利率計算 - 基於 funding book 數據創建遞增利率序列
		rate := ss.calculateProgressiveRate(fundingBook, minDailyRate, condition, orderIndex, totalOriginalSplits)

		// 智能期間選擇
		period := ss.calculateSmartPeriod(rate, condition)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR, // 分散單依 MIN_DAILY_LEND_RATE 是否為 FRR 決定
		}
		offers = append(offers, offer)

		log.Printf("智能訂單 #%d - 利率: %.6f%%, 金額: %.2f, 期間: %d天, 深度索引: %d",
			len(offers), rate*100, allocAmount, period, currentDepthIndex)

		nextLend += gapClimb
		orderIndex++ // 增加訂單索引確保下一個訂單有不同的深度索引
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

// calculateSmartRate 計算智能利率
func (ss *SmartStrategy) calculateSmartRate(depthIndex int, fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int) float64 {
	var rate float64

	if len(fundingBook) > 0 && depthIndex < len(fundingBook) {
		// 使用實際市場數據
		marketRate := fundingBook[depthIndex].Rate

		// 競爭分析優化
		competitiveRate := ss.analyzer.AnalyzeCompetition(fundingBook)
		if competitiveRate > 0 && competitiveRate > marketRate {
			marketRate = competitiveRate
		}

		// 如果市場利率低於最小利率，使用最小利率作為基礎，但添加訂單索引遞增
		if marketRate < minDailyRate {
			// 使用最小利率 + 基於訂單索引的小幅遞增來確保差異化
			rate = minDailyRate + (minDailyRate * 0.01 * float64(orderIndex))
		} else {
			rate = marketRate
		}

		log.Printf("市場數據利率計算 - 深度索引: %d, 市場利率: %.6f%%, 最終利率: %.6f%%",
			depthIndex, fundingBook[depthIndex].Rate*100, rate*100)
	} else {
		// 深度超出範圍時，使用合成利率
		rate = ss.calculateSyntheticRate(depthIndex, minDailyRate, condition)
	}

	// 根據市場趨勢微調
	switch condition.Trend {
	case "rising":
		// 利率上升時稍微提高利率保持競爭力
		rate *= 1.01
	case "falling":
		// 利率下降時保持原利率
		break
	}

	return rate
}

// calculateProgressiveRate 計算遞增利率序列
func (ss *SmartStrategy) calculateProgressiveRate(fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int, totalOrders int) float64 {
	if len(fundingBook) == 0 {
		// 無市場數據時使用合成利率
		return ss.calculateSyntheticRate(orderIndex, minDailyRate, condition)
	}

	// 分析 funding book 中的利率分佈
	var rates []float64
	for _, entry := range fundingBook {
		if entry.Rate >= minDailyRate {
			rates = append(rates, entry.Rate)
		}
	}

	if len(rates) == 0 {
		// 沒有符合最小利率的數據，使用合成利率
		baseRate := minDailyRate
		increment := baseRate * ss.config.RateRangeIncreasePercent * float64(orderIndex)
		return baseRate + increment
	}

	// 找出利率範圍
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

	log.Printf("Funding Book 利率分析 - 有效利率數量: %d, 原始範圍: %.6f%%-%.6f%%",
		len(rates), minRate*100, maxRate*100)

	// 確保最小利率不低於配置的最小利率
	if minRate < minDailyRate {
		minRate = minDailyRate
	}

	// 創建遞增利率序列
	if totalOrders > 1 {
		rateRange := maxRate - minRate

		// 如果利率範圍太小（所有利率相同），則人工創建遞增範圍
		if rateRange < minRate*constants.SmallRateChangePercent {
			// 使用基礎利率創建遞增範圍
			maxRate = minRate * (1.0 + ss.config.RateRangeIncreasePercent)
			rateRange = maxRate - minRate
			log.Printf("利率範圍太小，使用人工範圍: %.6f%%-%.6f%%", minRate*100, maxRate*100)
		}

		step := rateRange / float64(totalOrders-1)
		progressiveRate := minRate + step*float64(orderIndex)

		log.Printf("遞增利率計算 - 訂單索引: %d, 利率範圍: %.6f%%-%.6f%%, 步長: %.6f%%, 遞增利率: %.6f%%",
			orderIndex, minRate*100, maxRate*100, step*100, progressiveRate*100)

		return progressiveRate
	} else {
		return minRate
	}
}

// calculateSyntheticRate 計算合成利率（當無市場數據時）
func (ss *SmartStrategy) calculateSyntheticRate(depthIndex int, minDailyRate float64, condition *MarketCondition) float64 {
	// 基於深度索引創建利率階梯
	// 利率範圍：最小利率 到 最小利率 × 配置的最大倍數
	maxRate := minDailyRate * ss.config.MaxRateMultiplier

	// 根據市場狀況調整利率範圍
	switch condition.Trend {
	case "rising":
		// 利率上升趨勢，使用更積極的利率範圍
		maxRate = minDailyRate * (ss.config.MaxRateMultiplier * 0.8)
	case "falling":
		// 利率下降趨勢，使用更保守的利率範圍
		maxRate = minDailyRate * (ss.config.MaxRateMultiplier * 1.2)
	}

	// 根據深度索引計算利率增量，確保每個索引都有不同的利率
	// 使用更細緻的階梯，確保差異化
	totalSteps := 20.0 // 20個基礎層級
	rateStep := (maxRate - minDailyRate) / totalSteps

	// 為每個深度索引計算唯一利率
	syntheticRate := minDailyRate + float64(depthIndex)*rateStep

	// 添加微小的隨機變化以確保完全不同
	// 基於深度索引的位置添加細微調整
	microAdjustment := rateStep * 0.1 * float64(depthIndex%3) / 3.0
	syntheticRate += microAdjustment

	// 確保在合理範圍內
	if syntheticRate < minDailyRate {
		syntheticRate = minDailyRate
	}
	if syntheticRate > maxRate {
		syntheticRate = maxRate
	}

	log.Printf("合成利率計算 - 深度索引: %d, 基礎利率: %.6f%%, 合成利率: %.6f%%, 趨勢: %s",
		depthIndex, minDailyRate*100, syntheticRate*100, condition.Trend)

	return syntheticRate
}

// calculateSmartPeriod 計算智能期間
func (ss *SmartStrategy) calculateSmartPeriod(dailyRate float64, condition *MarketCondition) int {
	if ss.config.LoanDays > 0 {
		return ss.config.LoanDays
	}

	oneTwentyThreshold := ss.config.GetOneTwentyDayThresholdDecimal()
	thirtyThreshold := ss.config.GetThirtyDayThresholdDecimal()

	// 基礎期間邏輯
	basePeriod := constants.DefaultPeriodDays
	if ss.config.OneTwentyDayLendRateThreshold > 0 && dailyRate >= oneTwentyThreshold {
		basePeriod = constants.Period120Days
	} else if ss.config.ThirtyDayLendRateThreshold > 0 && dailyRate >= thirtyThreshold {
		basePeriod = constants.Period30Days
	}

	// 根據市場狀況智能調整
	switch condition.Trend {
	case "rising":
		// 利率上升趨勢，偏向短期以便重新定價
		if basePeriod == constants.Period120Days {
			basePeriod = constants.Period30Days
		} else if basePeriod == constants.Period30Days {
			basePeriod = constants.DefaultPeriodDays
		}

	case "falling":
		// 利率下降趨勢，鎖定當前較高利率
		if dailyRate > condition.AvgRate*1.1 && basePeriod == constants.DefaultPeriodDays {
			basePeriod = constants.Period30Days
		}
	}

	// 高波動環境偏向短期 (使用配置的波動率閾值)
	if condition.Volatility > ss.config.VolatilityThreshold*1.5 && basePeriod > constants.Period30Days {
		basePeriod = constants.Period30Days
	}

	return basePeriod
}

// calculateTotalVolume 計算總成交量
func (ss *SmartStrategy) calculateTotalVolume(fundingBook []*bitfinex.FundingBookEntry) float64 {
	var totalVolume float64
	maxEntries := 10 // 只計算前10層，基於市場深度分析最佳實踐

	for i, entry := range fundingBook {
		if i >= maxEntries {
			break
		}
		totalVolume += math.Abs(entry.Amount)
	}

	return totalVolume
}
