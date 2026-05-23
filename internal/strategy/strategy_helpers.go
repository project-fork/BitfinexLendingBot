package strategy

import (
	"log"
	"math"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
)

func getFundingBookIndexRange(cfg *config.Config, fundingBook []*bitfinex.FundingBookEntry) (int, int) {
	if len(fundingBook) == 0 {
		return 0, 0
	}

	maxIndex := len(fundingBook) - 1
	bottom := 0
	top := maxIndex

	if cfg == nil || cfg.GapTop <= cfg.GapBottom || cfg.GapTop <= 0 {
		return bottom, top
	}

	if cfg.GapBottom > 0 {
		bottom = int(math.Floor(cfg.GapBottom))
	}
	if cfg.GapTop > 0 {
		top = int(math.Floor(cfg.GapTop))
	}

	if bottom < 0 {
		bottom = 0
	}
	if top < 0 {
		top = 0
	}
	if bottom > maxIndex {
		bottom = maxIndex
	}
	if top > maxIndex {
		top = maxIndex
	}
	if top < bottom {
		top = bottom
	}

	return bottom, top
}

func buildDepthSampleIndexes(cfg *config.Config, fundingBook []*bitfinex.FundingBookEntry, totalOrders int) []int {
	if len(fundingBook) == 0 || totalOrders <= 0 {
		return nil
	}

	bottom, top := getFundingBookIndexRange(cfg, fundingBook)
	indexes := make([]int, 0, totalOrders)
	if totalOrders == 1 || top == bottom {
		for i := 0; i < totalOrders; i++ {
			indexes = append(indexes, bottom)
		}
		return indexes
	}

	rangeWidth := top - bottom
	for i := 0; i < totalOrders; i++ {
		position := float64(i) * float64(rangeWidth) / float64(totalOrders-1)
		index := bottom + int(math.Round(position))
		if index < bottom {
			index = bottom
		}
		if index > top {
			index = top
		}
		indexes = append(indexes, index)
	}

	return indexes
}

func buildTraditionalDepthProgressionIndexes(cfg *config.Config, fundingBook []*bitfinex.FundingBookEntry, totalOrders int) []int {
	if len(fundingBook) == 0 || totalOrders <= 0 {
		return nil
	}

	bottom, top := getFundingBookIndexRange(cfg, fundingBook)
	indexes := make([]int, 0, totalOrders)
	if totalOrders == 1 || top == bottom {
		for i := 0; i < totalOrders; i++ {
			indexes = append(indexes, bottom)
		}
		return indexes
	}

	rangeWidth := top - bottom
	for i := 0; i < totalOrders; i++ {
		position := float64(i) / float64(totalOrders-1)
		index := bottom + int(math.Floor(position*float64(rangeWidth)))
		if i == totalOrders-1 {
			index = top
		}
		if index < bottom {
			index = bottom
		}
		if index > top {
			index = top
		}
		indexes = append(indexes, index)
	}

	return indexes
}

func selectFundingBookEntriesByIndexes(fundingBook []*bitfinex.FundingBookEntry, indexes []int) []*bitfinex.FundingBookEntry {
	if len(fundingBook) == 0 || len(indexes) == 0 {
		return nil
	}

	selected := make([]*bitfinex.FundingBookEntry, 0, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(fundingBook) {
			continue
		}
		selected = append(selected, fundingBook[index])
	}

	return selected
}

func selectFundingBookEntriesByRange(cfg *config.Config, fundingBook []*bitfinex.FundingBookEntry) []*bitfinex.FundingBookEntry {
	if len(fundingBook) == 0 {
		return nil
	}

	bottom, top := getFundingBookIndexRange(cfg, fundingBook)
	selected := make([]*bitfinex.FundingBookEntry, 0, top-bottom+1)
	for index := bottom; index <= top; index++ {
		if index < 0 || index >= len(fundingBook) {
			continue
		}
		selected = append(selected, fundingBook[index])
	}
	return selected
}

func getSafeDepthIndex(indexes []int, orderIndex int) int {
	if len(indexes) == 0 {
		return 0
	}
	if orderIndex < 0 {
		return indexes[0]
	}
	if orderIndex >= len(indexes) {
		return indexes[len(indexes)-1]
	}
	return indexes[orderIndex]
}

func selectFundingBookEntryByOrder(indexes []int, fundingBook []*bitfinex.FundingBookEntry, orderIndex int) (*bitfinex.FundingBookEntry, int) {
	if len(fundingBook) == 0 || len(indexes) == 0 {
		return nil, 0
	}

	depthIndex := getSafeDepthIndex(indexes, orderIndex)
	if depthIndex < 0 || depthIndex >= len(fundingBook) {
		return nil, depthIndex
	}
	return fundingBook[depthIndex], depthIndex
}

func calculateOptimalAllocation(cfg *config.Config, condition *MarketCondition) (highHoldRatio, spreadRatio float64) {
	baseHighHold := 0.5

	switch condition.Trend {
	case "rising":
		baseHighHold = 0.3
	case "falling":
		baseHighHold = 0.7
	}

	if condition.Volatility > cfg.VolatilityThreshold {
		baseHighHold += 0.1
	}

	if condition.RateRatio > 1.2 {
		baseHighHold += 0.1
	} else if condition.RateRatio < 0.8 {
		baseHighHold -= 0.1
	}

	baseHighHold = math.Max(0.2, math.Min(0.8, baseHighHold))
	return baseHighHold, 1.0 - baseHighHold
}

func calculateDynamicHighHoldRate(cfg *config.Config, condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) float64 {
	baseRate := cfg.GetHighHoldRateDecimal()
	if len(fundingBook) == 0 {
		return baseRate
	}

	marketRate := fundingBook[0].Rate

	switch condition.Trend {
	case "rising":
		dynamicRate := math.Min(marketRate*0.85, baseRate*cfg.MaxRateMultiplier)
		return math.Max(baseRate*cfg.MinRateMultiplier, dynamicRate)
	case "falling":
		return math.Max(baseRate*cfg.MinRateMultiplier, baseRate)
	default:
		if marketRate > baseRate*1.5 {
			adjustedRate := math.Min(marketRate*0.8, baseRate*cfg.MaxRateMultiplier)
			return math.Max(baseRate*cfg.MinRateMultiplier, adjustedRate)
		}
		return math.Max(baseRate*cfg.MinRateMultiplier, baseRate)
	}
}

func calculateProgressiveRate(logger *log.Logger, cfg *config.Config, fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int, totalOrders int) float64 {
	if len(fundingBook) == 0 {
		return calculateSyntheticRate(logger, cfg, orderIndex, minDailyRate, condition)
	}

	var rates []float64
	for _, entry := range fundingBook {
		if entry.Rate >= minDailyRate {
			rates = append(rates, entry.Rate)
		}
	}

	if len(rates) == 0 {
		baseRate := minDailyRate
		increment := baseRate * cfg.RateRangeIncreasePercent * float64(orderIndex)
		return baseRate + increment
	}

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

	logger.Printf("Funding Book 利率分析 - 有效利率数量: %d, 原始范围: %.6f%%-%.6f%%",
		len(rates), minRate*100, maxRate*100)

	if minRate < minDailyRate {
		minRate = minDailyRate
	}

	if totalOrders > 1 {
		rateRange := maxRate - minRate
		if rateRange < minRate*constants.SmallRateChangePercent {
			maxRate = minRate * (1.0 + cfg.RateRangeIncreasePercent)
			rateRange = maxRate - minRate
			logger.Printf("利率范围太小，使用人工范围: %.6f%%-%.6f%%", minRate*100, maxRate*100)
		}

		step := rateRange / float64(totalOrders-1)
		progressiveRate := minRate + step*float64(orderIndex)

		logger.Printf("递增利率计算 - 订单索引: %d, 利率范围: %.6f%%-%.6f%%, 步长: %.6f%%, 递增利率: %.6f%%",
			orderIndex, minRate*100, maxRate*100, step*100, progressiveRate*100)

		return undercutFundingBookRate(cfg, progressiveRate, minDailyRate)
	}

	return undercutFundingBookRate(cfg, minRate, minDailyRate)
}

func undercutFundingBookRate(cfg *config.Config, rate float64, minDailyRate float64) float64 {
	undercut := cfg.FundingBookRateUndercut / constants.PercentageToDecimal
	return math.Max(minDailyRate, rate-undercut)
}

func calculateSyntheticRate(logger *log.Logger, cfg *config.Config, depthIndex int, minDailyRate float64, condition *MarketCondition) float64 {
	maxRate := minDailyRate * cfg.MaxRateMultiplier

	switch condition.Trend {
	case "rising":
		maxRate = minDailyRate * (cfg.MaxRateMultiplier * 0.8)
	case "falling":
		maxRate = minDailyRate * (cfg.MaxRateMultiplier * 1.2)
	}

	totalSteps := 20.0
	rateStep := (maxRate - minDailyRate) / totalSteps

	syntheticRate := minDailyRate + float64(depthIndex)*rateStep
	microAdjustment := rateStep * 0.1 * float64(depthIndex%3) / 3.0
	syntheticRate += microAdjustment

	if syntheticRate < minDailyRate {
		syntheticRate = minDailyRate
	}
	if syntheticRate > maxRate {
		syntheticRate = maxRate
	}

	logger.Printf("合成利率计算 - 深度索引: %d, 基础利率: %.6f%%, 合成利率: %.6f%%, 趋势: %s",
		depthIndex, minDailyRate*100, syntheticRate*100, condition.Trend)

	return syntheticRate
}

func calculateSmartPeriod(cfg *config.Config, dailyRate float64, condition *MarketCondition) int {
	if cfg.LoanDays > 0 {
		return cfg.LoanDays
	}

	basePeriod := constants.DefaultPeriodDays
	for _, threshold := range cfg.GetSortedLoanPeriodThresholdsDesc() {
		if rateMeetsThreshold(dailyRate, threshold.ThresholdDecimal) {
			basePeriod = threshold.Days
			break
		}
	}

	switch condition.Trend {
	case "rising":
		if basePeriod > constants.Period30Days {
			basePeriod = constants.Period30Days
		} else if basePeriod == constants.Period30Days {
			basePeriod = constants.DefaultPeriodDays
		}
	case "falling":
		if dailyRate > condition.AvgRate*1.1 && basePeriod == constants.DefaultPeriodDays {
			basePeriod = constants.Period30Days
		} else if dailyRate > condition.AvgRate*1.1 && basePeriod == constants.Period30Days {
			basePeriod = constants.Period60Days
		}
	}

	if condition.Volatility > cfg.VolatilityThreshold*1.5 && basePeriod > constants.Period30Days {
		basePeriod = constants.Period30Days
	}

	return basePeriod
}

func getRemainingOrderSlots(orderLimit int, existingOffers int) int {
	if orderLimit <= 0 {
		return -1
	}

	remaining := orderLimit - existingOffers
	if remaining < 0 {
		return 0
	}

	return remaining
}

func calculateTotalVolume(fundingBook []*bitfinex.FundingBookEntry) float64 {
	var totalVolume float64
	maxEntries := 10

	for i, entry := range fundingBook {
		if i >= maxEntries {
			break
		}
		totalVolume += math.Abs(entry.Amount)
	}

	return totalVolume
}
