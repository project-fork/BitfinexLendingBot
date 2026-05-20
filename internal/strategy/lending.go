package strategy

import (
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

// LendingBot 贷出机器人
type LendingBot struct {
	config         *config.Config
	client         fundingClient
	rateConverter  *rates.Converter
	smartStrategy  *SmartStrategy
	orderTracker   *tracker.BotOrderTracker
	notifyCallback func(string) error // Telegram 通知回调函数
	logger         *log.Logger
}

type fundingClient interface {
	GetFundingBook(symbol string, limit int) ([]*bitfinex.FundingBookEntry, error)
	GetFundingOffers(symbol string) ([]*bitfinex.FundingOffer, error)
	CancelFundingOffer(offerID int64) error
	GetFundingBalance(currency string) (float64, error)
	SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error)
	SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error)
	GetFundingCandles(symbol string, timeFrame string, limit int) ([]*bitfinex.Candle, error)
	GetFundingCredits(symbol string) ([]*bitfinex.FundingCredit, error)
	GetCurrentFundingRate(symbol string) (float64, error)
}

// NewLendingBot 创建新的贷出机器人
func NewLendingBot(cfg *config.Config, client *bitfinex.Client) *LendingBot {
	return &LendingBot{
		config:        cfg,
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		smartStrategy: NewSmartStrategy(cfg),
		logger:        log.New(os.Stderr, "", log.LstdFlags),
	}
}

// SetLogger 设置日志记录器
func (lb *LendingBot) SetLogger(logger *log.Logger) {
	if logger == nil {
		return
	}
	lb.logger = logger
	lb.smartStrategy.SetLogger(logger)
}

func (lb *LendingBot) getLogger() *log.Logger {
	if lb.logger == nil {
		lb.logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	if lb.smartStrategy != nil {
		lb.smartStrategy.SetLogger(lb.logger)
	}
	return lb.logger
}

// LoanOffer 代表一个贷出订单
type LoanOffer struct {
	Amount float64
	Rate   float64 // 日利率（小数格式）
	Period int
	UseFRR bool // 是否使用 FRR 挂单模式
}

// Execute 执行机器人主要逻辑（默认不取消既有未成交订单）
func (lb *LendingBot) Execute() error {
	return lb.execute(false)
}

// ExecuteWithOfferCancellation 执行机器人主要逻辑，并先取消程序追踪到的未成交订单。
func (lb *LendingBot) ExecuteWithOfferCancellation() error {
	return lb.execute(true)
}

func (lb *LendingBot) execute(cancelTrackedOffers bool) error {
	logger := lb.getLogger()
	logger.Println("开始执行贷出机器人...")

	// 清理旧的订单记录（避免记忆体泄漏）
	lb.orderTracker.CleanOldOrders(24 * time.Hour)

	hasPendingOrders, err := lb.hasTrackedPendingOffers()
	if err != nil {
		logger.Printf("获取未成交订单失败: %v", err)
		return err
	}

	if cancelTrackedOffers {
		// 取消程序创建的未完成订单
		logger.Println("取消程序创建的未完成订单...")
		hasPendingOrders, err = lb.cancelAllOffers()
		if err != nil {
			logger.Printf("取消订单失败: %v", err)
			return err
		}

		// 等待订单取消完成
		time.Sleep(constants.RetryDelay)
	} else if hasPendingOrders {
		logger.Println("检测到程序追踪的未成交订单，本轮保留现有订单并继续补单")
	} else {
		logger.Println("目前没有程序追踪的未成交订单")
	}

	// 获取可用资金
	logger.Println("取得可用额度...")
	fundsAvailable, err := lb.getAvailableFunds()
	if err != nil {
		logger.Printf("取得余额错误: %v", err)
		return err
	}
	logger.Printf("Currency: %s  Available: %f", lb.config.Currency, fundsAvailable)

	// 扣除保留金额
	if lb.config.ReserveAmount > 0 {
		fundsAvailable = math.Max(0, fundsAvailable-lb.config.ReserveAmount)
		logger.Printf("扣除保留金额后可用: %f", fundsAvailable)
	}

	// 检查可用资金
	if fundsAvailable < lb.config.MinLoan {
		logger.Println("可用资金小于最小贷出额，不进行操作")
		return nil
	}

	// 获取市场数据
	fundingBook, err := lb.client.GetFundingBook(lb.config.GetFundingSymbol(), constants.MaxPriceLevels)
	if err != nil {
		logger.Printf("取得 Funding Book 错误: %v", err)
		logger.Println("使用fallback模式，仅使用最小利率策略")
		// 使用空的funding book，策略会自动使用最小利率
		fundingBook = []*bitfinex.FundingBookEntry{}
	}

	// 根据配置选择策略
	var loanOffers []*LoanOffer
	if lb.config.EnableKlineStrategy {
		logger.Println("使用K线策略计算贷出订单...")
		loanOffers = lb.calculateKlineOffers(fundsAvailable)
	} else if lb.config.EnableSmartStrategy {
		logger.Println("使用智能策略计算贷出订单...")
		loanOffers = lb.smartStrategy.CalculateSmartOffers(fundsAvailable, fundingBook)
	} else {
		logger.Println("使用传统策略计算贷出订单...")
		loanOffers = lb.calculateLoanOffers(fundsAvailable, fundingBook)
	}

	// 下单
	return lb.placeLoanOffers(loanOffers, hasPendingOrders)
}

func (lb *LendingBot) hasTrackedPendingOffers() (bool, error) {
	offers, err := lb.ListPendingFundingOffers()
	if err != nil {
		return false, err
	}

	for _, offer := range offers {
		if offer != nil && offer.IsTracked {
			return true, nil
		}
	}

	return false, nil
}

// cancelAllOffers 取消程序创建的未完成订单
func (lb *LendingBot) cancelAllOffers() (bool, error) {
	summary, err := lb.CancelPendingFundingOffers(false)
	if err != nil {
		return false, err
	}

	return summary.Cancelled > 0, nil
}

// ListPendingFundingOffers 获取当前币种的未成交订单，并标记是否为程序追踪订单。
func (lb *LendingBot) ListPendingFundingOffers() ([]*bitfinex.PendingFundingOffer, error) {
	offers, err := lb.client.GetFundingOffers(lb.config.GetFundingSymbol())
	if err != nil {
		return nil, err
	}

	result := make([]*bitfinex.PendingFundingOffer, 0, len(offers))
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		result = append(result, &bitfinex.PendingFundingOffer{
			FundingOffer: *offer,
			IsTracked:    lb.orderTracker.IsTrackedOrder(offer.ID),
		})
	}

	return result, nil
}

// CancelPendingFundingOffers 取消当前币种的未成交订单。
// includeAll 为 false 时，仅取消程序追踪到的订单；为 true 时取消全部未成交订单。
func (lb *LendingBot) CancelPendingFundingOffers(includeAll bool) (*bitfinex.FundingOfferCancelSummary, error) {
	offers, err := lb.ListPendingFundingOffers()
	if err != nil {
		return nil, err
	}

	summary := &bitfinex.FundingOfferCancelSummary{
		Total: len(offers),
	}
	if len(offers) == 0 {
		lb.getLogger().Println("目前没有未完成的订单")
		return summary, nil
	}

	for _, offer := range offers {
		if !includeAll && !offer.IsTracked {
			lb.getLogger().Printf("跳过手动创建的订单 ID: %d", offer.ID)
			summary.Skipped++
			continue
		}

		if err := lb.client.CancelFundingOffer(offer.ID); err != nil {
			lb.getLogger().Printf("取消程序订单失败: %v", err)
			summary.Failed++
		} else {
			lb.getLogger().Printf("成功取消程序订单 ID: %d", offer.ID)
			lb.orderTracker.RemoveOrder(offer.ID)
			summary.Cancelled++
		}
	}

	if summary.Cancelled == 0 {
		lb.getLogger().Println("没有程序创建的订单需要取消")
	}

	return summary, nil
}

// getAvailableFunds 获取可用资金
func (lb *LendingBot) getAvailableFunds() (float64, error) {
	return lb.client.GetFundingBalance(strings.ToUpper(lb.config.Currency))
}

// calculateLoanOffers 计算贷出订单
func (lb *LendingBot) calculateLoanOffers(fundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var loanOffers []*LoanOffer

	// 检查可用资金
	if fundsAvailable < lb.config.MinLoan {
		return loanOffers
	}

	splitFundsAvailable := fundsAvailable
	lb.getLogger().Printf("策略输入 - 可用资金: %.4f, 最小单笔: %.4f, 最大单笔: %.4f, Funding Book层数: %d",
		fundsAvailable, lb.config.MinLoan, lb.config.MaxLoan, len(fundingBook))

	// 高额持有策略
	if lb.config.HighHoldAmount > lb.config.MinLoan || splitFundsAvailable >= lb.config.MinLoan {
		highHoldOffers := lb.calculateHighHoldOffers(&splitFundsAvailable)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	// 分散贷出策略
	if splitFundsAvailable >= lb.config.MinLoan {
		remainingSlots := lb.getRemainingOrderSlots(len(loanOffers))
		if remainingSlots != 0 {
			spreadOffers := lb.calculateSpreadOffers(splitFundsAvailable, fundingBook, remainingSlots)
			loanOffers = append(loanOffers, spreadOffers...)
		}
	}

	return loanOffers
}

// calculateHighHoldOffers 计算高额持有订单
func (lb *LendingBot) calculateHighHoldOffers(splitFundsAvailable *float64) []*LoanOffer {
	var offers []*LoanOffer

	ordersCount := lb.config.HighHoldOrders
	if ordersCount <= 0 {
		ordersCount = 1
	}

	highHold := lb.config.HighHoldAmount
	if *splitFundsAvailable < highHold {
		highHold = *splitFundsAvailable
	}
	if lb.config.MaxLoan > 0 && highHold > lb.config.MaxLoan {
		highHold = lb.config.MaxLoan
	}
	highHold = floorToCents(highHold)

	if highHold < lb.config.MinLoan {
		return offers
	}

	possibleOrders := int(*splitFundsAvailable / highHold)
	actualOrders := int(math.Min(float64(ordersCount), float64(possibleOrders)))
	lb.getLogger().Printf("高额持有策略 - 目标单数: %d, 可下单数: %d, 单笔金额: %.4f, 固定利率: %.6f%%",
		ordersCount, actualOrders, highHold, decimalDailyRateToPercent(lb.config.GetHighHoldRateDecimal()))

	for i := 0; i < actualOrders; i++ {
		if *splitFundsAvailable < highHold {
			break
		}

		offer := &LoanOffer{
			Amount: highHold,
			Rate:   lb.config.GetHighHoldRateDecimal(),
			Period: lb.config.GetLoanPeriod(constants.Period120Days),
			UseFRR: false, // 高额持有单固定走一般利率单
		}
		offers = append(offers, offer)
		*splitFundsAvailable -= highHold
	}

	return offers
}

// calculateSpreadOffers 计算分散贷出订单
func (lb *LendingBot) calculateSpreadOffers(splitFundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry, maxOrders int) []*LoanOffer {
	var offers []*LoanOffer
	useFRR := lb.config.IsMinDailyLendRateFRR()

	numSplits := lb.config.SpreadLend
	if maxOrders > 0 && numSplits > maxOrders {
		numSplits = maxOrders
	}
	if numSplits <= 0 || splitFundsAvailable < lb.config.MinLoan {
		return offers
	}

	orderAmounts := buildOrderAmounts(splitFundsAvailable, numSplits, lb.config.MinLoan, lb.config.MaxLoan)
	if len(orderAmounts) == 0 {
		return offers
	}
	lb.getLogger().Printf("分散策略 - 剩余资金: %.4f, 目标拆单数: %d, 实际拆单数: %d, 金额分配: %v",
		splitFundsAvailable, numSplits, len(orderAmounts), orderAmounts)

	// 计算利率递增量
	gapClimb := (lb.config.GapTop - lb.config.GapBottom) / float64(len(orderAmounts))
	nextLend := lb.config.GapBottom

	depthIndex := 0
	minDailyRate := lb.config.GetMinDailyRateDecimal()
	lb.getLogger().Printf("分散策略 - GAP_BOTTOM: %.2f, GAP_TOP: %.2f, gapClimb: %.4f, 最低日利率: %.6f%%, FRR模式: %v",
		lb.config.GapBottom, lb.config.GapTop, gapClimb, decimalDailyRateToPercent(minDailyRate), useFRR)

	for idx, allocAmount := range orderAmounts {
		// 累计市场量至指定利率区间（仅在有funding book数据时）
		if len(fundingBook) > 0 {
			for float64(depthIndex) < nextLend && depthIndex < len(fundingBook)-1 {
				depthIndex++
			}
		}

		if allocAmount < lb.config.MinLoan {
			break
		}

		// 计算利率
		var rate float64
		var marketRate float64
		rateDecision := "使用最低利率（无funding book数据）"
		if len(fundingBook) > 0 && depthIndex < len(fundingBook) {
			marketRate = fundingBook[depthIndex].Rate
			if marketRate < minDailyRate {
				rate = minDailyRate
				rateDecision = fmt.Sprintf("市场利率 %.6f%% 低于最低利率 %.6f%%，改用最低利率",
					decimalDailyRateToPercent(marketRate),
					decimalDailyRateToPercent(minDailyRate),
				)
			} else {
				rate = marketRate
				rateDecision = fmt.Sprintf("使用 funding book 第 %d 档市场利率 %.6f%%",
					depthIndex,
					decimalDailyRateToPercent(marketRate),
				)
			}
		} else {
			// 无funding book数据时使用最小利率
			rate = minDailyRate
		}

		// 计算期间
		period := lb.calculatePeriod(rate)
		lb.getLogger().Printf("分散订单 #%d - 目标深度: %.2f, 实际深度索引: %d, 市场利率: %.6f%%, 决策: %s, 挂单利率: %.6f%%, 金额: %.4f, 期限: %d天",
			idx+1,
			nextLend,
			depthIndex,
			decimalDailyRateToPercent(marketRate),
			rateDecision,
			decimalDailyRateToPercent(rate),
			allocAmount,
			period,
		)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR, // 分散单依 MIN_DAILY_LEND_RATE 是否为 FRR 决定
		}
		offers = append(offers, offer)

		nextLend += gapClimb
	}

	return offers
}

// calculatePeriod 根据利率计算贷出期间
func (lb *LendingBot) calculatePeriod(dailyRate float64) int {
	if lb.config.LoanDays > 0 {
		lb.getLogger().Printf("期限决策 - 固定期限模式，利率 %.6f%% 使用配置天数 %d",
			decimalDailyRateToPercent(dailyRate), lb.config.LoanDays)
		return lb.config.LoanDays
	}

	oneTwentyThreshold := lb.config.GetOneTwentyDayThresholdDecimal()
	ninetyThreshold := lb.config.GetNinetyDayThresholdDecimal()
	sixtyThreshold := lb.config.GetSixtyDayThresholdDecimal()
	thirtyThreshold := lb.config.GetThirtyDayThresholdDecimal()

	if lb.config.OneTwentyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, oneTwentyThreshold) {
		lb.getLogger().Printf("期限决策 - 利率 %.6f%% 达到 120天阈值 %.6f%%，使用 120 天",
			decimalDailyRateToPercent(dailyRate), lb.config.OneTwentyDayLendRateThreshold)
		return constants.Period120Days
	} else if lb.config.NinetyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, ninetyThreshold) {
		lb.getLogger().Printf("期限决策 - 利率 %.6f%% 达到 90天阈值 %.6f%%，使用 90 天",
			decimalDailyRateToPercent(dailyRate), lb.config.NinetyDayLendRateThreshold)
		return constants.Period90Days
	} else if lb.config.SixtyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, sixtyThreshold) {
		lb.getLogger().Printf("期限决策 - 利率 %.6f%% 达到 60天阈值 %.6f%%，使用 60 天",
			decimalDailyRateToPercent(dailyRate), lb.config.SixtyDayLendRateThreshold)
		return constants.Period60Days
	} else if lb.config.ThirtyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, thirtyThreshold) {
		lb.getLogger().Printf("期限决策 - 利率 %.6f%% 达到 30天阈值 %.6f%%，使用 30 天",
			decimalDailyRateToPercent(dailyRate), lb.config.ThirtyDayLendRateThreshold)
		return constants.Period30Days
	} else {
		lb.getLogger().Printf("期限决策 - 利率 %.6f%% 未达任何长期限阈值，使用默认 %d 天",
			decimalDailyRateToPercent(dailyRate), constants.DefaultPeriodDays)
		return constants.DefaultPeriodDays
	}
}

func rateMeetsThreshold(rate float64, threshold float64) bool {
	const tolerance = 1e-12
	return rate+tolerance >= threshold
}

// placeLoanOffers 下单
func (lb *LendingBot) placeLoanOffers(loanOffers []*LoanOffer, hasPendingOrders bool) error {
	orderCount := 0
	fundingSymbol := lb.config.GetFundingSymbol()
	if lb.config.IsMinDailyLendRateFRR() {
		lb.getLogger().Printf("MIN_DAILY_LEND_RATE=%s，分散单使用 FRR 模式；高额持有单维持固定利率", constants.MinDailyRateModeFRR)
	}

	for _, offer := range loanOffers {
		if lb.config.OrderLimit != 0 && orderCount >= lb.config.OrderLimit {
			break
		}

		offer.Amount = floorToCents(offer.Amount)
		if offer.Amount < lb.config.MinLoan {
			lb.getLogger().Printf("跳过无效金额: %.4f", offer.Amount)
			continue
		}

		if offer.UseFRR {
			frrPeriod := lb.config.GetLoanPeriod(constants.Period120Days)

			if lb.config.TestMode {
				lb.getLogger().Printf("🧪 [测试模式] 模拟下单 => Type: %s, Amount: %.4f, Period: %d (参考Rate: %.6f%%)",
					constants.OfferTypeFRRDeltaVar,
					offer.Amount,
					frrPeriod,
					lb.rateConverter.DecimalToPercentage(offer.Rate),
				)
				orderCount++
			} else {
				lb.getLogger().Printf("下单 => Type: %s, Amount: %.4f, Period: %d (参考Rate: %.6f%%)",
					constants.OfferTypeFRRDeltaVar,
					offer.Amount,
					frrPeriod,
					lb.rateConverter.DecimalToPercentage(offer.Rate),
				)

				orderID, err := lb.client.SubmitFundingOfferFRR(fundingSymbol, offer.Amount, frrPeriod, false)
				if err != nil {
					lb.getLogger().Printf("下订单失败: %v", err)
				} else {
					// 追踪程序创建的订单
					lb.orderTracker.TrackOrder(orderID)
					lb.getLogger().Printf("成功创建订单 ID: %d，已加入追踪", orderID)
					orderCount++
				}
			}

			continue
		}

		rate := offer.Rate
		if !hasPendingOrders {
			// 添加利率加成
			lb.getLogger().Printf("下单决策 - 无既有待处理订单，对 %.6f%% 加上 RATE_BONUS %.6f%%",
				decimalDailyRateToPercent(rate), lb.config.RateBonus)
			rate += lb.rateConverter.PercentageToDecimal(lb.config.RateBonus)
		} else {
			lb.getLogger().Printf("下单决策 - 有既有待处理订单，不加 RATE_BONUS，维持 %.6f%%",
				decimalDailyRateToPercent(rate))
		}

		// 验证利率
		if !lb.rateConverter.ValidateDailyRate(rate) {
			lb.getLogger().Printf("跳过无效利率: %.6f", rate)
			continue
		}

		if lb.config.TestMode {
			// 测试模式：只记录不真的下单
			lb.getLogger().Printf("🧪 [测试模式] 模拟下单 => Rate: %.6f%%, Amount: %.4f, Period: %d",
				lb.rateConverter.DecimalToPercentage(rate), offer.Amount, offer.Period)
			orderCount++
		} else {
			// 正式模式：真的下单
			lb.getLogger().Printf("下单 => Rate: %.6f%%, Amount: %.4f, Period: %d",
				lb.rateConverter.DecimalToPercentage(rate), offer.Amount, offer.Period)

			orderID, err := lb.client.SubmitFundingOffer(fundingSymbol, offer.Amount, rate, offer.Period, false)
			if err != nil {
				lb.getLogger().Printf("下订单失败: %v", err)
			} else {
				// 追踪程序创建的订单
				lb.orderTracker.TrackOrder(orderID)
				lb.getLogger().Printf("成功创建订单 ID: %d，已加入追踪", orderID)
				orderCount++
			}
		}
	}

	return nil
}

func decimalDailyRateToPercent(rate float64) float64 {
	return rate * constants.PercentageToDecimal
}

// CheckRateThreshold 检查利率是否超过阈值（基于5分钟K线最近12根高点）
func (lb *LendingBot) CheckRateThreshold() (bool, float64, error) {
	// 获取5分钟K线数据（12根，相当于1小时）
	candles, err := lb.client.GetFundingCandles(
		lb.config.GetFundingSymbol(),
		"5m",
		12,
	)
	if err != nil {
		return false, 0, err
	}

	// 找到最近12根K线中的最高利率
	highestRate := lb.findMaxRate(candles)
	percentageRate := lb.rateConverter.DecimalDailyToPercentageDaily(highestRate)
	exceeded := percentageRate > lb.config.NotifyRateThreshold

	lb.getLogger().Printf("K线阈值检查 - 最近12根5分钟K线最高利率: %.4f%%, 阈值: %.4f%%, 超过: %v",
		percentageRate, lb.config.NotifyRateThreshold, exceeded)

	return exceeded, percentageRate, nil
}

// SetNotifyCallback 设置 Telegram 通知回调函数
func (lb *LendingBot) SetNotifyCallback(callback func(string) error) {
	lb.notifyCallback = callback
}

// CheckNewLendingCredits 检查新的借贷订单和余额变化，决定是否需要重新执行策略
func (lb *LendingBot) CheckNewLendingCredits() (bool, error) {
	lb.getLogger().Println("检查执行触发条件（新借贷订单、余额变化）...")

	// 获取当前可用余额
	currentBalance, err := lb.getAvailableFunds()
	if err != nil {
		lb.getLogger().Printf("获取余额失败: %v", err)
		return false, err
	}

	// 获取当前活跃的借贷订单
	credits, err := lb.client.GetFundingCredits(lb.config.GetFundingSymbol())
	if err != nil {
		lb.getLogger().Printf("获取借贷订单失败: %v", err)
		return false, err
	}

	// 获取当前时间戳（毫秒）
	currentTime := time.Now().UnixNano() / int64(time.Millisecond)

	// 如果这是第一次检查，初始化时间戳和余额但不触发执行
	if lb.config.LastLendingCheckTime == 0 {
		lb.getLogger().Printf("首次检查，发现 %d 个现有借贷订单，余额: %.2f，初始化检查参数", len(credits), currentBalance)
		lb.config.LastLendingCheckTime = currentTime
		lb.config.LastAvailableBalance = currentBalance
		lb.config.SeenFundingCreditIDs = buildSeenCreditIDs(credits)
		return false, nil
	}

	shouldExecute := false
	var reasons []string

	// 检查1: 是否有新的借贷订单
	newCredits := findNewCredits(credits, lb.config.SeenFundingCreditIDs, lb.config.LastLendingCheckTime)

	if len(newCredits) > 0 {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("发现 %d 个新的借贷订单", len(newCredits)))
		// 发送借贷通知
		if err := lb.sendLendingNotification(newCredits); err != nil {
			lb.getLogger().Printf("发送借贷通知失败: %v", err)
		}
	}

	// 检查2: 余额是否显着增加
	lastBalance := lb.config.LastAvailableBalance
	balanceIncrease := currentBalance - lastBalance

	// 设定触发阈值：余额增加超过10%或超过最小贷出金额
	increaseThreshold := math.Max(lastBalance*0.1, lb.config.MinLoan)

	if balanceIncrease > increaseThreshold {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("余额显着增加: %.2f -> %.2f (+%.2f)", lastBalance, currentBalance, balanceIncrease))
		if len(newCredits) == 0 {
			lb.sendBalanceChangeNotification(lastBalance, currentBalance, balanceIncrease)
		}
	}

	// 检查3: 从零余额恢复
	if lastBalance == 0 && currentBalance > lb.config.MinLoan {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("从零余额恢复: %.2f -> %.2f", lastBalance, currentBalance))
	}

	// 更新检查参数
	lb.config.LastLendingCheckTime = currentTime
	lb.config.LastAvailableBalance = currentBalance
	lb.config.SeenFundingCreditIDs = buildSeenCreditIDs(credits)

	if shouldExecute {
		lb.getLogger().Printf("触发策略执行，原因: %s", strings.Join(reasons, "; "))
		return true, nil
	}

	lb.getLogger().Printf("无需执行策略，余额: %.2f (上次: %.2f)，无新借贷订单", currentBalance, lastBalance)
	return false, nil
}

func buildSeenCreditIDs(credits []*bitfinex.FundingCredit) map[int64]struct{} {
	seen := make(map[int64]struct{}, len(credits))
	for _, credit := range credits {
		if credit == nil || credit.ID == 0 {
			continue
		}
		seen[credit.ID] = struct{}{}
	}
	return seen
}

func findNewCredits(credits []*bitfinex.FundingCredit, seen map[int64]struct{}, lastCheckTime int64) []*bitfinex.FundingCredit {
	var newCredits []*bitfinex.FundingCredit
	for _, credit := range credits {
		if credit == nil {
			continue
		}
		if credit.ID != 0 {
			if _, exists := seen[credit.ID]; !exists {
				newCredits = append(newCredits, credit)
				continue
			}
		}
		if credit.ID == 0 && credit.MTSOpened > lastCheckTime {
			newCredits = append(newCredits, credit)
		}
	}
	return newCredits
}

func (lb *LendingBot) sendBalanceChangeNotification(lastBalance, currentBalance, balanceIncrease float64) {
	if lb.notifyCallback == nil {
		lb.getLogger().Println("Telegram 通知回调未设置，跳过余额变化通知")
		return
	}

	message := fmt.Sprintf("💰 可用余额变化通知\n\n余额: %.2f -> %.2f %s\n增加: %.2f %s\n\n未从 Funding Credits API 识别到新的成交 ID，请检查 Bitfinex 借贷状态。",
		lastBalance,
		currentBalance,
		lb.config.Currency,
		balanceIncrease,
		lb.config.Currency)

	if err := lb.notifyCallback(message); err != nil {
		lb.getLogger().Printf("发送余额变化通知失败: %v", err)
		return
	}

	lb.getLogger().Println("余额变化通知发送成功")
}

// sendLendingNotification 发送借贷订单通知
func (lb *LendingBot) sendLendingNotification(credits []*bitfinex.FundingCredit) error {
	if lb.notifyCallback == nil {
		lb.getLogger().Println("Telegram 通知回调未设置，跳过通知")
		return nil
	}

	message := "💰 新的借贷订单通知\n\n"

	frrFallbackRate := 0.0
	for _, credit := range credits {
		if credit.EffectiveDailyRate() == 0 {
			rate, err := lb.client.GetCurrentFundingRate(lb.config.GetFundingSymbol())
			if err != nil {
				lb.getLogger().Printf("取得 FRR 利率失败: %v", err)
				break
			}
			frrFallbackRate = rate
			break
		}
	}

	// 先计算所有订单的统计信息
	totalAmount := 0.0
	totalEarnings := 0.0

	for _, credit := range credits {
		effectiveRate := credit.EffectiveDailyRate()
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		dailyEarnings := credit.Amount * effectiveRate
		periodEarnings := dailyEarnings * float64(credit.Period)
		totalAmount += credit.Amount
		totalEarnings += periodEarnings
	}

	// 显示详细信息（最多显示配置数量的订单）
	for i, credit := range credits {
		if i >= constants.MaxDisplayOrders {
			remaining := len(credits) - constants.MaxDisplayOrders
			message += fmt.Sprintf("... 还有 %d 个订单\n", remaining)
			break
		}

		// 计算预期收益（日利率 * 金额 * 期间）
		effectiveRate := credit.EffectiveDailyRate()
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		dailyEarnings := credit.Amount * effectiveRate
		periodEarnings := dailyEarnings * float64(credit.Period)

		// 格式化开始时间
		openTime := time.Unix(credit.MTSOpened/1000, 0)

		message += fmt.Sprintf("📊 订单 #%d\n", i+1)
		message += fmt.Sprintf("💵 金额: %.2f %s\n", credit.Amount, lb.config.Currency)
		message += fmt.Sprintf("📈 日利率: %.4f%%\n", lb.rateConverter.DecimalToPercentage(effectiveRate))
		message += fmt.Sprintf("📈 年利率: %.4f%%\n", lb.rateConverter.DecimalToPercentage(effectiveRate)*constants.DaysPerYear)
		message += fmt.Sprintf("⏰ 期间: %d 天\n", credit.Period)
		message += fmt.Sprintf("💰 预期收益: %.4f %s\n", periodEarnings, lb.config.Currency)
		message += fmt.Sprintf("🕐 开始时间: %s\n", openTime.Format("2006-01-02 15:04:05"))
		message += "\n"
	}

	// 添加统计信息
	message += fmt.Sprintf("📊 统计信息:\n")
	message += fmt.Sprintf("📦 总数量: %d 个订单\n", len(credits))
	message += fmt.Sprintf("💵 总金额: %.2f %s\n", totalAmount, lb.config.Currency)
	message += fmt.Sprintf("💰 总预期收益: %.4f %s\n", totalEarnings, lb.config.Currency)

	// 尝试发送通知，如果失败（例如 Telegram 未认证）只记录日志但不返回错误
	if err := lb.notifyCallback(message); err != nil {
		lb.getLogger().Printf("发送借贷订单通知失败: %v", err)
		lb.getLogger().Println("新借贷订单通知内容:")
		lb.getLogger().Println(message)
		return nil // 不返回错误，避免影响主程序执行
	}

	lb.getLogger().Println("借贷订单通知发送成功")
	return nil
}

// GetActiveLendingCredits 获取活跃借贷订单（供 Telegram 指令使用）
func (lb *LendingBot) GetActiveLendingCredits() ([]*bitfinex.FundingCredit, error) {
	return lb.client.GetFundingCredits(lb.config.GetFundingSymbol())
}

// calculateKlineOffers 基于K线数据计算贷出订单
func (lb *LendingBot) calculateKlineOffers(fundsAvailable float64) []*LoanOffer {
	var loanOffers []*LoanOffer

	// 检查可用资金
	if fundsAvailable < lb.config.MinLoan {
		return loanOffers
	}

	// 获取K线数据
	candles, _ := lb.client.GetFundingCandles(
		lb.config.GetFundingSymbol(),
		lb.config.KlineTimeFrame,
		lb.config.KlinePeriod,
	)

	// 找到最近期间内的最高利率
	highestRate := lb.findHighestRateFromCandles(candles)
	lb.getLogger().Printf("K线数据分析：最高利率 %.6f%%", lb.rateConverter.DecimalToPercentage(highestRate))

	// 计算目标利率（最高利率 + 加成）
	spreadMultiplier := 1.0 + (lb.config.KlineSpreadPercent / 100.0)
	targetRate := highestRate * spreadMultiplier

	// 确保不低于最小利率
	minDailyRate := lb.config.GetMinDailyRateDecimal()
	if targetRate < minDailyRate {
		targetRate = minDailyRate
		lb.getLogger().Printf("目标利率低于最小利率，使用最小利率: %.6f%%", lb.rateConverter.DecimalToPercentage(targetRate))
	}

	lb.getLogger().Printf("K线策略目标利率: %.6f%% (加成: %.1f%%)",
		lb.rateConverter.DecimalToPercentage(targetRate),
		lb.config.KlineSpreadPercent)

	splitFundsAvailable := fundsAvailable

	// 高额持有策略
	if lb.config.HighHoldAmount > lb.config.MinLoan || splitFundsAvailable >= lb.config.MinLoan {
		highHoldOffers := lb.calculateHighHoldOffers(&splitFundsAvailable)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	// 使用目标利率创建分散订单
	if splitFundsAvailable >= lb.config.MinLoan {
		remainingSlots := lb.getRemainingOrderSlots(len(loanOffers))
		if remainingSlots != 0 {
			klineOffers := lb.calculateKlineSpreadOffers(splitFundsAvailable, targetRate, remainingSlots)
			loanOffers = append(loanOffers, klineOffers...)
		}
	}

	return loanOffers
}

// findHighestRateFromCandles 从K线数据中找到最高利率
func (lb *LendingBot) findHighestRateFromCandles(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	// 根据配置选择平滑方法
	switch lb.config.KlineSmoothMethod {
	case "max":
		return lb.findMaxRate(candles)
	case "sma":
		return lb.calculateSMA(candles)
	case "ema":
		return lb.calculateEMAHigh(candles)
	case "hla":
		return lb.calculateHighLowAverage(candles)
	case "p90":
		return lb.calculate90Percentile(candles)
	default:
		lb.getLogger().Printf("未知的平滑方法: %s，使用默认的 EMA", lb.config.KlineSmoothMethod)
		return lb.calculateEMAHigh(candles)
	}
}

// findMaxRate 找到最高利率（原始方法）
func (lb *LendingBot) findMaxRate(candles []*bitfinex.Candle) float64 {
	highestRate := candles[0].High
	for _, candle := range candles {
		if candle.High > highestRate {
			highestRate = candle.High
		}
	}
	return highestRate
}

// calculateSMA 计算收盘价的简单移动平均
func (lb *LendingBot) calculateSMA(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	sum := 0.0
	for _, candle := range candles {
		sum += candle.Close
	}
	return sum / float64(len(candles))
}

// calculateEMAHigh 计算高点的指数移动平均
func (lb *LendingBot) calculateEMAHigh(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	// EMA 系数，期间越长系数越小
	alpha := 2.0 / (float64(len(candles)) + 1.0)
	ema := candles[0].High

	for i := 1; i < len(candles); i++ {
		ema = alpha*candles[i].High + (1-alpha)*ema
	}

	return ema
}

// calculateHighLowAverage 计算高低点平均
func (lb *LendingBot) calculateHighLowAverage(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	sumHigh := 0.0
	sumLow := 0.0
	for _, candle := range candles {
		sumHigh += candle.High
		sumLow += candle.Low
	}

	avgHigh := sumHigh / float64(len(candles))
	avgLow := sumLow / float64(len(candles))

	// 取高低点平均的平均（偏向高点一些）
	return (avgHigh + avgLow) / 2.0
}

// calculate90Percentile 计算90百分位数
func (lb *LendingBot) calculate90Percentile(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	// 收集所有高点
	highs := make([]float64, len(candles))
	for i, candle := range candles {
		highs[i] = candle.High
	}

	// 简单排序
	for i := 0; i < len(highs); i++ {
		for j := i + 1; j < len(highs); j++ {
			if highs[i] > highs[j] {
				highs[i], highs[j] = highs[j], highs[i]
			}
		}
	}

	// 计算90百分位数的索引
	index := int(float64(len(highs)) * 0.9)
	if index >= len(highs) {
		index = len(highs) - 1
	}

	return highs[index]
}

// calculateKlineSpreadOffers 基于K线目标利率计算分散订单
func (lb *LendingBot) calculateKlineSpreadOffers(fundsAvailable float64, targetRate float64, maxOrders int) []*LoanOffer {
	var offers []*LoanOffer
	useFRR := lb.config.IsMinDailyLendRateFRR()

	numSplits := lb.config.SpreadLend
	if maxOrders > 0 && numSplits > maxOrders {
		numSplits = maxOrders
	}
	if numSplits <= 0 || fundsAvailable < lb.config.MinLoan {
		return offers
	}

	orderAmounts := buildOrderAmounts(fundsAvailable, numSplits, lb.config.MinLoan, lb.config.MaxLoan)
	if len(orderAmounts) == 0 {
		return offers
	}

	// 创建订单，使用目标利率为基准，微调以分散风险
	for i, allocAmount := range orderAmounts {
		if allocAmount < lb.config.MinLoan {
			break
		}

		rate := targetRate * (1 + (float64(i) * lb.config.RateRangeIncreasePercent))

		// 确保利率不低于最小利率
		minDailyRate := lb.config.GetMinDailyRateDecimal()
		if rate < minDailyRate {
			rate = minDailyRate
		}

		// 计算期间
		period := lb.calculatePeriod(rate)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR, // K线分散单依 MIN_DAILY_LEND_RATE 是否为 FRR 决定
		}
		offers = append(offers, offer)
	}

	return offers
}

func (lb *LendingBot) getRemainingOrderSlots(existingOffers int) int {
	if lb.config.OrderLimit <= 0 {
		return -1
	}

	remaining := lb.config.OrderLimit - existingOffers
	if remaining < 0 {
		return 0
	}

	return remaining
}
