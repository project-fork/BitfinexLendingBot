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

// SmartStrategy 智能策略引擎。
type SmartStrategy struct {
	config   *config.Config
	analyzer *MarketAnalyzer
	logger   *log.Logger
}

// NewSmartStrategy 创建智能策略引擎。
func NewSmartStrategy(cfg *config.Config) *SmartStrategy {
	return &SmartStrategy{
		config:   cfg,
		analyzer: NewMarketAnalyzer(),
		logger:   log.New(os.Stderr, "", log.LstdFlags),
	}
}

// SetLogger 设置日志记录器。
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

// CalculateSmartOffers 计算智能贷出订单。
func (ss *SmartStrategy) CalculateSmartOffers(fundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
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
		totalVolume := ss.calculateTotalVolume(analysisBook)
		ss.analyzer.AddRateSnapshot(currentRate, totalVolume)
	}

	marketCondition := ss.analyzer.AnalyzeMarket(analysisBook)
	ss.getLogger().Printf("市场状况 - 趋势: %s, 波动率: %.6f, 利率比例: %.2f",
		marketCondition.Trend, marketCondition.Volatility, marketCondition.RateRatio)

	highHoldRatio, spreadRatio := ss.calculateOptimalAllocation(marketCondition)
	splitFundsAvailable := fundsAvailable
	highHoldAmount := fundsAvailable * highHoldRatio
	spreadAmount := fundsAvailable * spreadRatio

	ss.getLogger().Printf("资金配置 - 高额持有: %.2f%% (%.2f), 分散贷出: %.2f%% (%.2f)",
		highHoldRatio*100, highHoldAmount, spreadRatio*100, spreadAmount)

	if ss.config.HighHoldAmount > ss.config.MinLoan && highHoldAmount >= ss.config.HighHoldAmount {
		highHoldOffers := ss.calculateSmartHighHoldOffers(&splitFundsAvailable, marketCondition, analysisBook)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	if splitFundsAvailable >= ss.config.MinLoan {
		spreadOffers := ss.calculateSmartSpreadOffers(splitFundsAvailable, fundingBook, analysisBook, marketCondition)
		loanOffers = append(loanOffers, spreadOffers...)
	}

	return loanOffers
}

func (ss *SmartStrategy) calculateOptimalAllocation(condition *MarketCondition) (highHoldRatio, spreadRatio float64) {
	return calculateOptimalAllocation(ss.config, condition)
}

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

	if highHold < ss.config.MinLoan {
		return offers
	}

	dynamicRate := ss.calculateDynamicHighHoldRate(condition, fundingBook)
	period := ss.calculateSmartPeriod(dynamicRate, condition)

	possibleOrders := int(*splitFundsAvailable / highHold)
	actualOrders := int(math.Min(float64(ordersCount), float64(possibleOrders)))

	ss.getLogger().Printf("智能高额持有 - 动态利率: %.4f%%, 期间: %d天, 订单数: %d",
		dynamicRate*100, period, actualOrders)

	for i := 0; i < actualOrders; i++ {
		if *splitFundsAvailable < highHold {
			break
		}

		offers = append(offers, &LoanOffer{
			Amount: highHold,
			Rate:   dynamicRate,
			Period: period,
			UseFRR: false,
			Reason: LoanOfferReason{
				FundSource:   "智能策略高额持有额度",
				DepthSource:  "不使用 Funding Book 深度",
				RateSource:   "智能策略动态高额持有利率",
				PeriodSource: "智能策略智能期限决策",
			},
		})
		*splitFundsAvailable -= highHold
	}

	return offers
}

func (ss *SmartStrategy) calculateDynamicHighHoldRate(condition *MarketCondition, fundingBook []*bitfinex.FundingBookEntry) float64 {
	return calculateDynamicHighHoldRate(ss.config, condition, fundingBook)
}

func (ss *SmartStrategy) calculateSmartSpreadOffers(splitFundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry, analysisBook []*bitfinex.FundingBookEntry, condition *MarketCondition) []*LoanOffer {
	var offers []*LoanOffer
	useFRR := ss.config.IsMinDailyLendRateFRR()

	numSplits := ss.config.SpreadLend
	if numSplits <= 0 || splitFundsAvailable < ss.config.MinLoan {
		return offers
	}

	if condition.Volatility > ss.config.VolatilityThreshold {
		numSplits = int(float64(numSplits) * constants.ReducedSplitsMultiplier)
	}
	if numSplits <= 0 {
		return offers
	}

	amtEach := floorToCents(splitFundsAvailable / float64(numSplits))
	for amtEach < ss.config.MinLoan && numSplits > 1 {
		numSplits--
		amtEach = floorToCents(splitFundsAvailable / float64(numSplits))
	}
	if numSplits <= 0 || amtEach < ss.config.MinLoan {
		return offers
	}

	minDailyRate := ss.config.GetMinDailyRateDecimal()
	depthIndexes := buildDepthSampleIndexes(ss.config, fundingBook, numSplits)
	if len(analysisBook) == 0 {
		analysisBook = fundingBook
	}
	rangeBottom, rangeTop := getFundingBookIndexRange(ss.config, fundingBook)

	ss.getLogger().Printf("智能分散策略 - 分割数: %d, 配置索引范围: %d-%d, Funding Book数据: %d笔",
		numSplits, rangeBottom, rangeTop, len(fundingBook))
	ss.getLogger().Printf("智能分散策略 - 实际采样索引: %v", depthIndexes)

	remainingFunds := splitFundsAvailable
	totalOriginalSplits := numSplits
	orderIndex := 0

	for numSplits > 0 && remainingFunds >= ss.config.MinLoan {
		sampledEntry, currentDepthIndex := selectFundingBookEntryByOrder(depthIndexes, fundingBook, orderIndex)

		allocAmount := amtEach
		if numSplits == 1 {
			allocAmount = floorToCents(remainingFunds)
		}
		if ss.config.MaxLoan > 0 && allocAmount > ss.config.MaxLoan {
			allocAmount = ss.config.MaxLoan
		}
		allocAmount = floorToCents(allocAmount)
		if allocAmount < ss.config.MinLoan {
			break
		}

		rate := ss.calculateProgressiveRate(analysisBook, minDailyRate, condition, orderIndex, totalOriginalSplits)
		period := ss.calculateSmartPeriod(rate, condition)
		depthSource := describeDepthSource(fundingBook, currentDepthIndex)
		if sampledEntry == nil {
			depthSource = "无有效 Funding Book 采样条目，使用合成或聚合逻辑"
		}

		offers = append(offers, &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR,
			Reason: LoanOfferReason{
				FundSource:   fmt.Sprintf("智能策略分散资金，第 %d 笔", len(offers)+1),
				DepthSource:  depthSource,
				RateSource:   "智能策略递增利率计算",
				PeriodSource: "智能策略智能期限决策",
			},
		})

		ss.getLogger().Printf("智能订单 #%d - 利率: %.6f%%, 金额: %.2f, 期间: %d天, 深度索引: %d",
			len(offers), rate*100, allocAmount, period, currentDepthIndex)

		remainingFunds = floorToCents(remainingFunds - allocAmount)
		orderIndex++
		numSplits--
	}

	return offers
}

func (ss *SmartStrategy) calculateSmartRate(depthIndex int, fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int) float64 {
	var rate float64

	if len(fundingBook) > 0 && depthIndex < len(fundingBook) {
		marketRate := fundingBook[depthIndex].Rate

		competitiveRate := ss.analyzer.AnalyzeCompetition(fundingBook)
		if competitiveRate > 0 && competitiveRate > marketRate {
			marketRate = competitiveRate
		}

		if marketRate < minDailyRate {
			rate = minDailyRate + (minDailyRate * constants.SmallRateChangePercent * float64(orderIndex))
		} else {
			rate = marketRate
		}

		ss.getLogger().Printf("市场数据利率计算 - 深度索引: %d, 市场利率: %.6f%%, 最终利率: %.6f%%",
			depthIndex, fundingBook[depthIndex].Rate*100, rate*100)
	} else {
		rate = ss.calculateSyntheticRate(depthIndex, minDailyRate, condition)
	}

	switch condition.Trend {
	case "rising":
		rate *= 1.01
	case "falling":
	}

	return rate
}

func (ss *SmartStrategy) calculateProgressiveRate(fundingBook []*bitfinex.FundingBookEntry, minDailyRate float64, condition *MarketCondition, orderIndex int, totalOrders int) float64 {
	return calculateProgressiveRate(ss.getLogger(), ss.config, fundingBook, minDailyRate, condition, orderIndex, totalOrders)
}

func (ss *SmartStrategy) undercutFundingBookRate(rate float64, minDailyRate float64) float64 {
	return undercutFundingBookRate(ss.config, rate, minDailyRate)
}

func (ss *SmartStrategy) calculateSyntheticRate(depthIndex int, minDailyRate float64, condition *MarketCondition) float64 {
	return calculateSyntheticRate(ss.getLogger(), ss.config, depthIndex, minDailyRate, condition)
}

func (ss *SmartStrategy) calculateSmartPeriod(dailyRate float64, condition *MarketCondition) int {
	return calculateSmartPeriod(ss.config, dailyRate, condition)
}

func (ss *SmartStrategy) calculateTotalVolume(fundingBook []*bitfinex.FundingBookEntry) float64 {
	return calculateTotalVolume(fundingBook)
}
