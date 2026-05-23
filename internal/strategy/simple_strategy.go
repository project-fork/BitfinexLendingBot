package strategy

import (
	"fmt"
	"log"
	"math"
	"os"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
)

// SimpleStrategy 代表偏执行兼容、补单优先的简化策略。
type SimpleStrategy struct {
	config   *config.Config
	analyzer *MarketAnalyzer
	logger   *log.Logger
}

func NewSimpleStrategy(cfg *config.Config) *SimpleStrategy {
	return &SimpleStrategy{
		config:   cfg,
		analyzer: NewMarketAnalyzer(),
		logger:   log.New(os.Stderr, "", log.LstdFlags),
	}
}

func (ss *SimpleStrategy) SetLogger(logger *log.Logger) {
	if logger == nil {
		return
	}
	ss.logger = logger
}

func (ss *SimpleStrategy) getLogger() *log.Logger {
	if ss.logger == nil {
		ss.logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	return ss.logger
}

func (ss *SimpleStrategy) CalculateOffers(fundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var loanOffers []*LoanOffer

	if fundsAvailable < ss.config.MinLoan {
		return loanOffers
	}

	analysisBook := selectFundingBookEntriesByRange(ss.config, fundingBook)
	if len(analysisBook) == 0 {
		analysisBook = fundingBook
	}

	if len(analysisBook) > 0 {
		currentRate := analysisBook[0].Rate
		totalVolume := calculateTotalVolume(analysisBook)
		ss.analyzer.AddRateSnapshot(currentRate, totalVolume)
	}

	marketCondition := ss.analyzer.AnalyzeMarket(analysisBook)
	ss.getLogger().Printf("简单策略市场状况 - 趋势: %s, 波动率: %.6f, 利率比例: %.2f",
		marketCondition.Trend, marketCondition.Volatility, marketCondition.RateRatio)

	splitFundsAvailable := fundsAvailable

	if ss.config.HighHoldAmount > ss.config.MinLoan || splitFundsAvailable >= ss.config.MinLoan {
		highHoldOffers := ss.calculateHighHoldOffers(&splitFundsAvailable, marketCondition, analysisBook)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	_, spreadRatio := calculateOptimalAllocation(ss.config, marketCondition)
	spreadAmount := splitFundsAvailable * spreadRatio
	ss.getLogger().Printf("简单策略剩余资金配置 - 高额持有优先后余额: %.2f, 分散贷出参考比例: %.2f%% (%.2f)",
		splitFundsAvailable, spreadRatio*100, spreadAmount)

	if splitFundsAvailable >= ss.config.MinLoan {
		remainingSlots := getRemainingOrderSlots(ss.config.OrderLimit, len(loanOffers))
		if remainingSlots != 0 {
			spreadOffers := ss.calculateSpreadOffers(splitFundsAvailable, fundingBook, analysisBook, marketCondition, remainingSlots)
			loanOffers = append(loanOffers, spreadOffers...)
		}
	}

	return loanOffers
}

func (ss *SimpleStrategy) calculateHighHoldOffers(splitFundsAvailable *float64, condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
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

	dynamicRate := calculateDynamicHighHoldRate(ss.config, condition, fundingBook)
	period := calculateSmartPeriod(ss.config, dynamicRate, condition)

	possibleOrders := int(*splitFundsAvailable / highHold)
	actualOrders := int(math.Min(float64(ordersCount), float64(possibleOrders)))

	ss.getLogger().Printf("简单策略高额持有 - 动态利率: %.4f%%, 期间: %d天, 订单数: %d",
		dynamicRate*100, period, actualOrders)

	for i := 0; i < actualOrders; i++ {
		if *splitFundsAvailable < highHold {
			break
		}

		offer := &LoanOffer{
			Amount: highHold,
			Rate:   dynamicRate,
			Period: period,
			UseFRR: false,
			Reason: LoanOfferReason{
				FundSource:   "简单策略高额持有额度",
				DepthSource:  "不使用 Funding Book 深度",
				RateSource:   "简单策略动态高额持有利率",
				PeriodSource: "简单策略智能期限决策",
			},
		}
		offers = append(offers, offer)
		*splitFundsAvailable -= highHold
	}

	return offers
}

func (ss *SimpleStrategy) calculateSpreadOffers(splitFundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry, analysisBook []*bitfinex.FundingBookEntry, condition *MarketCondition, maxOrders int) []*LoanOffer {
	var offers []*LoanOffer
	useFRR := ss.config.IsMinDailyLendRateFRR()

	numSplits := ss.config.SpreadLend
	if maxOrders > 0 && numSplits > maxOrders {
		numSplits = maxOrders
	}
	if numSplits <= 0 || splitFundsAvailable < ss.config.MinLoan {
		return offers
	}

	if condition.Volatility > ss.config.VolatilityThreshold {
		numSplits = int(float64(numSplits) * constants.ReducedSplitsMultiplier)
	}

	orderAmounts := buildOrderAmounts(splitFundsAvailable, numSplits, ss.config.MinLoan, ss.config.MaxLoan)
	if len(orderAmounts) == 0 {
		return offers
	}

	minDailyRate := ss.config.GetMinDailyRateDecimal()
	depthIndexes := buildDepthSampleIndexes(ss.config, fundingBook, len(orderAmounts))
	if len(analysisBook) == 0 {
		analysisBook = fundingBook
	}
	rangeBottom, rangeTop := getFundingBookIndexRange(ss.config, fundingBook)

	ss.getLogger().Printf("简单策略分散策略 - 实际分散笔数: %d, 配置索引范围: %d-%d, Funding Book数据: %d笔",
		len(orderAmounts), rangeBottom, rangeTop, len(fundingBook))

	orderIndex := 0
	totalOriginalSplits := len(orderAmounts)

	for _, allocAmount := range orderAmounts {
		currentDepthIndex := 0
		if len(depthIndexes) > orderIndex {
			currentDepthIndex = depthIndexes[orderIndex]
		}

		if allocAmount < ss.config.MinLoan {
			break
		}

		rate := calculateProgressiveRate(ss.getLogger(), ss.config, analysisBook, minDailyRate, condition, orderIndex, totalOriginalSplits)
		period := calculateSmartPeriod(ss.config, rate, condition)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR,
			Reason: LoanOfferReason{
				FundSource:   fmt.Sprintf("简单策略分散资金，第 %d 笔", len(offers)+1),
				DepthSource:  describeDepthSource(fundingBook, currentDepthIndex),
				RateSource:   "简单策略递增利率计算",
				PeriodSource: "简单策略智能期限决策",
			},
		}
		offers = append(offers, offer)

		ss.getLogger().Printf("简单策略订单 #%d - 利率: %.6f%%, 金额: %.2f, 期间: %d天, 深度索引: %d",
			len(offers), rate*100, allocAmount, period, currentDepthIndex)

		orderIndex++
	}

	return offers
}
