package strategy

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	internalerrors "github.com/kfrico/BitfinexLendingBot/internal/errors"
	"github.com/kfrico/BitfinexLendingBot/internal/formatting"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/storage"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

// LendingBot 贷出机器人
type LendingBot struct {
	config                  *config.Config
	client                  fundingClient
	rateConverter           *rates.Converter
	simpleStrategy          *SimpleStrategy
	smartStrategy           *SmartStrategy
	orderTracker            *tracker.BotOrderTracker
	notifyCallback          func(string) error // Telegram 通知回调函数
	logger                  *log.Logger
	lastCheckErr            string
	errNotified             bool
	lastCredits             map[int64]*bitfinex.FundingCredit
	lastDecisionMu          sync.RWMutex
	lastDecision            *StrategyDecisionSummary
	executionMu             sync.Mutex
	lastExecutionAt         time.Time
	recentOrderFingerprints map[string]time.Time
	marketAnalyzer          *MarketAnalyzer
	marketHistoryStore      *marketHistoryStore
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
	logger := log.New(os.Stderr, "", log.LstdFlags)
	analyzer := NewMarketAnalyzer()
	bot := &LendingBot{
		config:                  cfg,
		client:                  client,
		rateConverter:           rates.NewConverter(),
		orderTracker:            tracker.NewBotOrderTracker(),
		simpleStrategy:          NewSimpleStrategyWithAnalyzer(cfg, analyzer),
		smartStrategy:           NewSmartStrategyWithAnalyzer(cfg, analyzer),
		logger:                  logger,
		recentOrderFingerprints: make(map[string]time.Time),
		marketAnalyzer:          analyzer,
		marketHistoryStore:      newMarketHistoryStore(storage.DefaultDataFilePath(), logger),
	}
	bot.restoreMarketHistory(time.Now())
	return bot
}

func (lb *LendingBot) restoreMarketHistory(now time.Time) {
	if lb == nil || lb.marketHistoryStore == nil {
		return
	}
	lb.marketHistoryStore.load(lb.config.GetFundingSymbol(), lb.marketAnalyzer, now)
}

func (lb *LendingBot) persistMarketHistory() {
	if lb == nil || lb.marketHistoryStore == nil {
		return
	}
	lb.marketHistoryStore.save(lb.config.GetFundingSymbol(), lb.marketAnalyzer)
}

// SetLogger 设置日志记录器
func (lb *LendingBot) SetLogger(logger *log.Logger) {
	if logger == nil {
		return
	}
	lb.logger = logger
	lb.simpleStrategy.SetLogger(logger)
	lb.smartStrategy.SetLogger(logger)
}

func (lb *LendingBot) getLogger() *log.Logger {
	if lb.logger == nil {
		lb.logger = log.New(os.Stderr, "", log.LstdFlags)
	}
	if lb.simpleStrategy != nil {
		lb.simpleStrategy.SetLogger(lb.logger)
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
	Reason LoanOfferReason
}

type LoanOfferReason struct {
	FundSource        string
	DepthSource       string
	RateSource        string
	PeriodSource      string
	ExecutionDecision string
}

type StrategyDecisionSummary struct {
	Strategy              string
	FundingSymbol         string
	FundsAvailable        float64
	ReserveAmount         float64
	HasPendingOrders      bool
	FundingBookEntries    int
	FundingBookSource     string
	RequestedOfferCount   int
	AttemptedOfferCount   int
	SuccessfulOfferCount  int
	SkippedOfferCount     int
	FailedOfferCount      int
	FRROfferCount         int
	FixedRateOfferCount   int
	RateBonusAppliedCount int
	MinOfferRatePercent   float64
	MaxOfferRatePercent   float64
	MinOfferAmount        float64
	MaxOfferAmount        float64
	LoanPeriods           []int
	FundSources           []string
	DepthSources          []string
	RateSources           []string
	PeriodSources         []string
	ExecutionDecisions    []string
	TriggerSource         string
	CooldownBypassed      bool
	SkipReason            string
	Notes                 []string
}

type placeLoanOffersResult struct {
	AttemptedOfferCount   int
	SuccessfulOfferCount  int
	SkippedOfferCount     int
	FailedOfferCount      int
	FRROfferCount         int
	FixedRateOfferCount   int
	RateBonusAppliedCount int
}

const smartPendingOfferRealignmentMinimumAge = 30 * time.Minute
const pendingOfferAmountTolerance = 0.01

type pendingOfferVisibility int

const (
	pendingOfferVisibilityConfigured pendingOfferVisibility = iota
	pendingOfferVisibilityAll
	pendingOfferVisibilityTrackedOnly
)

// Execute 执行机器人主要逻辑（默认不取消既有未成交订单）
func (lb *LendingBot) Execute() error {
	return lb.execute(false, "自动触发", false)
}

// ExecuteWithOfferCancellation 执行机器人主要逻辑，并先取消程序追踪到的未成交订单。
func (lb *LendingBot) ExecuteWithOfferCancellation() error {
	return lb.execute(true, "自动触发", false)
}

func (lb *LendingBot) ExecuteManual(triggerSource string, cancelTrackedOffers bool) error {
	if strings.TrimSpace(triggerSource) == "" {
		triggerSource = "手动触发"
	}
	return lb.execute(cancelTrackedOffers, triggerSource, true)
}

func (lb *LendingBot) execute(cancelTrackedOffers bool, triggerSource string, bypassCooldown bool) error {
	logger := lb.getLogger()
	logger.Println("开始执行贷出机器人...")

	if blocked, remaining := lb.shouldBlockByCooldown(bypassCooldown); blocked {
		logger.Printf("执行冷却中，跳过本轮主策略，剩余冷却时间: %s", remaining.Round(time.Second))
		lb.storeAndLogDecisionSummary(logger, &StrategyDecisionSummary{
			Strategy:         lb.config.GetStrategy(),
			FundingSymbol:    lb.config.GetFundingSymbol(),
			ReserveAmount:    lb.config.ReserveAmount,
			TriggerSource:    triggerSource,
			CooldownBypassed: bypassCooldown,
			SkipReason:       fmt.Sprintf("命中执行冷却，剩余 %s", remaining.Round(time.Second)),
			Notes:            []string{fmt.Sprintf("执行冷却中，剩余冷却时间 %s", remaining.Round(time.Second))},
		})
		return nil
	}

	// 清理旧的订单记录（避免记忆体泄漏）
	lb.orderTracker.CleanOldOrders(24 * time.Hour)
	lb.cleanRecentOrderFingerprints()

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
	logger.Printf("可用余额 %s", formatBalanceDisplay(lb.config.Currency, fundsAvailable))

	// 扣除保留金额
	if lb.config.ReserveAmount > 0 {
		fundsAvailable = math.Max(0, fundsAvailable-lb.config.ReserveAmount)
		logger.Printf("扣除保留金额后可用: %f", fundsAvailable)
	}

	decisionSummary := &StrategyDecisionSummary{
		Strategy:          lb.config.GetStrategy(),
		FundingSymbol:     lb.config.GetFundingSymbol(),
		FundsAvailable:    fundsAvailable,
		ReserveAmount:     lb.config.ReserveAmount,
		HasPendingOrders:  hasPendingOrders,
		FundingBookSource: "not_used",
		TriggerSource:     triggerSource,
		CooldownBypassed:  bypassCooldown,
	}

	// 检查可用资金
	if fundsAvailable < lb.config.MinLoan {
		logger.Println("可用资金小于最小贷出额，不进行操作")
		decisionSummary.SkipReason = "可用资金低于最小下单金额"
		decisionSummary.Notes = append(decisionSummary.Notes, fmt.Sprintf("可用资金 %.4f 低于最小下单金额 %.4f，本轮不下单", fundsAvailable, lb.config.MinLoan))
		lb.storeAndLogDecisionSummary(logger, decisionSummary)
		return nil
	}
	if lb.config.MinExecutableFunds > 0 && fundsAvailable < lb.config.MinExecutableFunds {
		logger.Printf("可用资金 %.4f 低于最小执行资金阈值 %.4f，本轮跳过", fundsAvailable, lb.config.MinExecutableFunds)
		decisionSummary.SkipReason = "可用资金低于最小执行资金阈值"
		decisionSummary.Notes = append(decisionSummary.Notes, fmt.Sprintf("可用资金 %.4f 低于最小执行资金阈值 %.4f，本轮不下单", fundsAvailable, lb.config.MinExecutableFunds))
		lb.storeAndLogDecisionSummary(logger, decisionSummary)
		return nil
	}

	// 获取市场数据
	fundingBook, err := lb.client.GetFundingBook(lb.config.GetFundingSymbol(), constants.MaxPriceLevels)
	if err != nil {
		logger.Printf("取得 Funding Book 错误: %v", err)
		if lb.config.IsKlineStrategy() {
			logger.Println("当前为 K 线策略，Funding Book 失败不影响本轮定价，继续使用 K 线数据")
			decisionSummary.FundingBookSource = "unavailable_but_not_required"
			decisionSummary.Notes = append(decisionSummary.Notes, "Funding Book 请求失败，但当前为 K 线策略，本轮继续使用 K 线数据")
		} else {
			logger.Println("Funding Book 属于关键定价输入，本轮中止下单")
			decisionSummary.FundingBookSource = "required_but_unavailable"
			decisionSummary.SkipReason = "Funding Book 请求失败"
			decisionSummary.Notes = append(decisionSummary.Notes, "Funding Book 请求失败，本轮已中止下单")
			lb.storeAndLogDecisionSummary(logger, decisionSummary)
			return err
		}
	} else {
		decisionSummary.FundingBookEntries = len(fundingBook)
		if lb.config.IsKlineStrategy() {
			decisionSummary.FundingBookSource = "available_but_unused"
		} else {
			decisionSummary.FundingBookSource = "required_and_used"
		}
	}

	// 根据配置选择策略
	var loanOffers []*LoanOffer
	switch lb.config.GetStrategy() {
	case config.StrategyKline:
		logger.Println("使用K线策略计算贷出订单...")
		loanOffers, err = lb.calculateKlineOffers(fundsAvailable)
		if err != nil {
			logger.Printf("K线策略计算失败: %v", err)
			decisionSummary.SkipReason = "K线策略计算失败"
			decisionSummary.Notes = append(decisionSummary.Notes, fmt.Sprintf("K线策略计算失败: %v", err))
			lb.storeAndLogDecisionSummary(logger, decisionSummary)
			return err
		}
	case config.StrategySimple:
		logger.Println("使用简单策略计算贷出订单...")
		loanOffers = lb.simpleStrategy.CalculateOffers(fundsAvailable, fundingBook)
	case config.StrategySmart:
		logger.Println("使用智能策略计算贷出订单...")
		loanOffers, hasPendingOrders, err = lb.calculateSmartOffersWithRealignment(fundsAvailable, fundingBook, hasPendingOrders, decisionSummary)
		if err != nil {
			decisionSummary.SkipReason = "智能策略重对齐评估失败"
			decisionSummary.Notes = append(decisionSummary.Notes, fmt.Sprintf("智能策略重对齐评估失败: %v", err))
			lb.storeAndLogDecisionSummary(logger, decisionSummary)
			return err
		}
	default:
		logger.Println("使用传统策略计算贷出订单...")
		loanOffers = lb.calculateLoanOffers(fundsAvailable, fundingBook)
	}

	decisionSummary.RequestedOfferCount = len(loanOffers)
	populateDecisionSummaryFromOffers(decisionSummary, loanOffers, lb.rateConverter)
	if lb.config.MinExecutableFunds > 0 && decisionSummary.MaxOfferAmount > 0 && sumOfferAmounts(loanOffers) < lb.config.MinExecutableFunds {
		totalOfferAmount := sumOfferAmounts(loanOffers)
		logger.Printf("生成订单总额 %.4f 低于最小执行资金阈值 %.4f，本轮跳过", totalOfferAmount, lb.config.MinExecutableFunds)
		decisionSummary.SkipReason = "生成订单总额低于最小执行资金阈值"
		decisionSummary.Notes = append(decisionSummary.Notes, fmt.Sprintf("生成订单总额 %.4f 低于最小执行资金阈值 %.4f，本轮不下单", totalOfferAmount, lb.config.MinExecutableFunds))
		lb.storeAndLogDecisionSummary(logger, decisionSummary)
		return nil
	}

	// 下单
	placeResult, err := lb.placeLoanOffers(loanOffers, hasPendingOrders)
	if err != nil {
		decisionSummary.SkipReason = "下单阶段失败"
		decisionSummary.Notes = append(decisionSummary.Notes, fmt.Sprintf("下单阶段失败: %v", err))
		lb.storeAndLogDecisionSummary(logger, decisionSummary)
		return err
	}
	decisionSummary.AttemptedOfferCount = placeResult.AttemptedOfferCount
	decisionSummary.SuccessfulOfferCount = placeResult.SuccessfulOfferCount
	decisionSummary.SkippedOfferCount = placeResult.SkippedOfferCount
	decisionSummary.FailedOfferCount = placeResult.FailedOfferCount
	decisionSummary.FRROfferCount = placeResult.FRROfferCount
	decisionSummary.FixedRateOfferCount = placeResult.FixedRateOfferCount
	decisionSummary.RateBonusAppliedCount = placeResult.RateBonusAppliedCount
	lb.storeAndLogDecisionSummary(logger, decisionSummary)
	lb.markExecutionCompleted()
	return nil
}

func (lb *LendingBot) hasTrackedPendingOffers() (bool, error) {
	offers, err := lb.listPendingFundingOffersByVisibility(pendingOfferVisibilityTrackedOnly)
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
	return lb.listPendingFundingOffersByVisibility(pendingOfferVisibilityAll)
}

func (lb *LendingBot) listStrategyVisiblePendingFundingOffers() ([]*bitfinex.PendingFundingOffer, error) {
	return lb.listPendingFundingOffersByVisibility(pendingOfferVisibilityConfigured)
}

func (lb *LendingBot) listPendingFundingOffersByVisibility(visibility pendingOfferVisibility) ([]*bitfinex.PendingFundingOffer, error) {
	offers, err := lb.client.GetFundingOffers(lb.config.GetFundingSymbol())
	if err != nil {
		return nil, err
	}

	result := make([]*bitfinex.PendingFundingOffer, 0, len(offers))
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		pendingOffer := &bitfinex.PendingFundingOffer{
			FundingOffer: *offer,
			IsTracked:    lb.orderTracker.IsTrackedOrder(offer.ID),
		}
		if !lb.shouldIncludePendingOffer(pendingOffer, visibility) {
			continue
		}
		result = append(result, pendingOffer)
	}

	return result, nil
}

func (lb *LendingBot) shouldIncludePendingOffer(offer *bitfinex.PendingFundingOffer, visibility pendingOfferVisibility) bool {
	if offer == nil {
		return false
	}

	switch visibility {
	case pendingOfferVisibilityAll:
		return true
	case pendingOfferVisibilityTrackedOnly:
		return offer.IsTracked
	default:
		if lb == nil || lb.config == nil {
			return offer.IsTracked
		}
		if lb.config.IncludeManualPendingOffersInStrategyFunds {
			return true
		}
		return offer.IsTracked
	}
}

func (lb *LendingBot) calculateSmartOffersWithRealignment(
	fundsAvailable float64,
	fundingBook []*bitfinex.FundingBookEntry,
	hasPendingOrders bool,
	decisionSummary *StrategyDecisionSummary,
) ([]*LoanOffer, bool, error) {
	visibleOffers, err := lb.listStrategyVisiblePendingFundingOffers()
	if err != nil {
		return nil, hasPendingOrders, err
	}

	visiblePendingAmount := sumPendingOfferAmounts(visibleOffers)
	totalFundsForTarget := floorToCents(fundsAvailable + visiblePendingAmount)
	targetOffers := lb.smartStrategy.CalculateSmartOffers(totalFundsForTarget, fundingBook)

	if len(visibleOffers) == 0 {
		return targetOffers, hasPendingOrders, nil
	}

	if containsUnsupportedRealignmentOfferType(visibleOffers) {
		if decisionSummary != nil {
			decisionSummary.Notes = append(decisionSummary.Notes, "智能策略重对齐跳过：当前可见未成交订单包含 FRR 或其他不支持自动重对齐的类型")
		}
		return nil, hasPendingOrders, nil
	}

	if !lb.areAllPendingOffersOldEnough(visibleOffers, smartPendingOfferRealignmentMinimumAge) {
		if decisionSummary != nil {
			decisionSummary.Notes = append(decisionSummary.Notes,
				fmt.Sprintf("智能策略重对齐跳过：当前可见未成交订单未满 %s", smartPendingOfferRealignmentMinimumAge.Round(time.Minute)))
		}
		return nil, true, nil
	}

	currentOffers := pendingOffersToLoanOffers(visibleOffers)
	if pendingOffersEquivalentToLoanOffers(currentOffers, targetOffers) {
		if decisionSummary != nil {
			decisionSummary.Notes = append(decisionSummary.Notes, "智能策略重对齐跳过：当前可见未成交订单已与目标订单一致")
		}
		return nil, true, nil
	}

	cancelSummary, err := lb.CancelPendingFundingOffers(lb.shouldIncludeManualPendingOffers())
	if err != nil {
		return nil, hasPendingOrders, err
	}
	if cancelSummary.Failed > 0 {
		if decisionSummary != nil {
			decisionSummary.Notes = append(decisionSummary.Notes,
				fmt.Sprintf("智能策略重对齐跳过：撤单结果 %d 成功，%d 失败，保留现有挂单避免新旧订单并存", cancelSummary.Cancelled, cancelSummary.Failed))
		}
		return nil, true, nil
	}
	if decisionSummary != nil {
		decisionSummary.Notes = append(decisionSummary.Notes,
			fmt.Sprintf("智能策略重对齐：已取消 %d 笔未成交订单，将按目标订单整组重挂", cancelSummary.Cancelled))
	}
	return targetOffers, true, nil
}

func (lb *LendingBot) shouldIncludeManualPendingOffers() bool {
	return lb != nil && lb.config != nil && lb.config.IncludeManualPendingOffersInStrategyFunds
}

func (lb *LendingBot) areAllPendingOffersOldEnough(offers []*bitfinex.PendingFundingOffer, minAge time.Duration) bool {
	if len(offers) == 0 {
		return false
	}

	now := time.Now()
	for _, offer := range offers {
		createdAt, ok := lb.pendingOfferCreatedAt(offer)
		if !ok || now.Sub(createdAt) < minAge {
			return false
		}
	}
	return true
}

func (lb *LendingBot) pendingOfferCreatedAt(offer *bitfinex.PendingFundingOffer) (time.Time, bool) {
	if offer == nil {
		return time.Time{}, false
	}
	if offer.MTSCreated > 0 {
		return time.UnixMilli(offer.MTSCreated), true
	}
	if lb != nil && lb.orderTracker != nil && offer.IsTracked {
		createdAt, ok := lb.orderTracker.GetOrderCreatedAt(offer.ID)
		if ok && !createdAt.IsZero() {
			return createdAt, true
		}
	}
	return time.Time{}, false
}

func containsUnsupportedRealignmentOfferType(offers []*bitfinex.PendingFundingOffer) bool {
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(offer.Type), constants.OfferTypeFRRDeltaVar) {
			return true
		}
	}
	return false
}

// CancelPendingFundingOffers 取消当前币种的未成交订单。
// includeAll 为 false 时，仅取消程序追踪到的订单；为 true 时取消全部未成交订单。
func (lb *LendingBot) CancelPendingFundingOffers(includeAll bool) (*bitfinex.FundingOfferCancelSummary, error) {
	visibility := pendingOfferVisibilityTrackedOnly
	if includeAll {
		visibility = pendingOfferVisibilityAll
	}

	offers, err := lb.listPendingFundingOffersByVisibility(visibility)
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
		lb.getLogger().Println("没有符合条件的未成交订单需要取消")
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
			Reason: LoanOfferReason{
				FundSource:   "高额持有额度",
				DepthSource:  "不使用 Funding Book 深度",
				RateSource:   fmt.Sprintf("固定高额持有利率 %.6f%%", decimalDailyRateToPercent(lb.config.GetHighHoldRateDecimal())),
				PeriodSource: describeTraditionalPeriodSource(lb.config, lb.config.GetHighHoldRateDecimal(), lb.config.GetLoanPeriod(constants.Period120Days)),
			},
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

	minDailyRate := lb.config.GetMinDailyRateDecimal()
	depthIndexes := buildTraditionalDepthProgressionIndexes(lb.config, fundingBook, len(orderAmounts))
	rangeBottom, rangeTop := getFundingBookIndexRange(lb.config, fundingBook)
	lb.getLogger().Printf("分散策略 - GAP_BOTTOM: %.2f, GAP_TOP: %.2f, 索引范围: %d-%d, 最低日利率: %.6f%%, FRR模式: %v",
		lb.config.GapBottom, lb.config.GapTop, rangeBottom, rangeTop, decimalDailyRateToPercent(minDailyRate), useFRR)
	lb.getLogger().Printf("分散策略 - 传统深度推进索引: %v", depthIndexes)

	for idx, allocAmount := range orderAmounts {
		if allocAmount < lb.config.MinLoan {
			break
		}

		// 计算利率
		var rate float64
		var marketRate float64
		depthIndex := 0
		rateDecision := "使用最低利率（无funding book数据）"
		if len(depthIndexes) > idx {
			depthIndex = depthIndexes[idx]
		}
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
		lb.getLogger().Printf("分散订单 #%d - 目标深度索引: %d, 实际深度索引: %d, 市场利率: %.6f%%, 决策: %s, 挂单利率: %.6f%%, 金额: %.4f, 期限: %d天",
			idx+1,
			depthIndex,
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
			Reason: LoanOfferReason{
				FundSource:   fmt.Sprintf("分散贷出资金，第 %d 笔", idx+1),
				DepthSource:  describeDepthSource(fundingBook, depthIndex),
				RateSource:   rateDecision,
				PeriodSource: describeTraditionalPeriodSource(lb.config, rate, period),
			},
		}
		offers = append(offers, offer)
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

	for _, threshold := range lb.config.GetSortedLoanPeriodThresholdsDesc() {
		if rateMeetsThreshold(dailyRate, threshold.ThresholdDecimal) {
			lb.getLogger().Printf("期限决策 - 利率 %.6f%% 达到 %d天阈值 %.6f%%，使用 %d 天",
				decimalDailyRateToPercent(dailyRate), threshold.Days, threshold.ThresholdPercent, threshold.Days)
			return threshold.Days
		}
	}

	lb.getLogger().Printf("期限决策 - 利率 %.6f%% 未达任何长期限阈值，使用默认 %d 天",
		decimalDailyRateToPercent(dailyRate), constants.DefaultPeriodDays)
	return constants.DefaultPeriodDays
}

func rateMeetsThreshold(rate float64, threshold float64) bool {
	const tolerance = 1e-12
	return rate+tolerance >= threshold
}

// placeLoanOffers 下单
func (lb *LendingBot) placeLoanOffers(loanOffers []*LoanOffer, hasPendingOrders bool) (*placeLoanOffersResult, error) {
	orderCount := 0
	result := &placeLoanOffersResult{}
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
			result.SkippedOfferCount++
			continue
		}

		result.AttemptedOfferCount++

		if offer.UseFRR {
			result.FRROfferCount++
			offer.Reason.ExecutionDecision = "FRR 挂单，执行层不额外加 RATE_BONUS"
			frrPeriod := lb.config.GetLoanPeriod(constants.Period120Days)
			if lb.shouldSkipByFingerprint(offer, offer.Rate) {
				lb.getLogger().Printf("跳过重复 FRR 订单指纹: amount=%.4f period=%d", offer.Amount, frrPeriod)
				result.SkippedOfferCount++
				continue
			}

			if lb.config.TestMode {
				lb.getLogger().Printf("🧪 [测试模式] 模拟下单 => Type: %s, Amount: %.4f, Period: %d (参考Rate: %.6f%%)",
					constants.OfferTypeFRRDeltaVar,
					offer.Amount,
					frrPeriod,
					lb.rateConverter.DecimalToPercentage(offer.Rate),
				)
				orderCount++
				result.SuccessfulOfferCount++
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
					result.FailedOfferCount++
				} else {
					// 追踪程序创建的订单
					lb.orderTracker.TrackOrder(orderID)
					lb.getLogger().Printf("成功创建订单 ID: %d，已加入追踪", orderID)
					orderCount++
					result.SuccessfulOfferCount++
				}
			}

			continue
		}

		result.FixedRateOfferCount++
		rate := offer.Rate
		if !hasPendingOrders {
			// 添加利率加成
			lb.getLogger().Printf("下单决策 - 无既有待处理订单，对 %.6f%% 加上 RATE_BONUS %.6f%%",
				decimalDailyRateToPercent(rate), lb.config.RateBonus)
			rate += lb.rateConverter.PercentageToDecimal(lb.config.RateBonus)
			result.RateBonusAppliedCount++
			offer.Reason.ExecutionDecision = fmt.Sprintf("无既有待处理订单，执行层追加 RATE_BONUS %.6f%%", lb.config.RateBonus)
		} else {
			lb.getLogger().Printf("下单决策 - 有既有待处理订单，不加 RATE_BONUS，维持 %.6f%%",
				decimalDailyRateToPercent(rate))
			offer.Reason.ExecutionDecision = "有既有待处理订单，执行层不追加 RATE_BONUS"
		}
		if lb.shouldSkipByFingerprint(offer, rate) {
			lb.getLogger().Printf("跳过重复固定利率订单指纹: amount=%.4f rate=%.6f%% period=%d",
				offer.Amount,
				lb.rateConverter.DecimalToPercentage(rate),
				offer.Period,
			)
			result.SkippedOfferCount++
			continue
		}

		// 验证利率
		if !lb.rateConverter.ValidateDailyRate(rate) {
			lb.getLogger().Printf("跳过无效利率: %.6f", rate)
			result.SkippedOfferCount++
			continue
		}

		if lb.config.TestMode {
			// 测试模式：只记录不真的下单
			lb.getLogger().Printf("🧪 [测试模式] 模拟下单 => Rate: %.6f%%, Amount: %.4f, Period: %d",
				lb.rateConverter.DecimalToPercentage(rate), offer.Amount, offer.Period)
			orderCount++
			result.SuccessfulOfferCount++
		} else {
			// 正式模式：真的下单
			lb.getLogger().Printf("下单 => Rate: %.6f%%, Amount: %.4f, Period: %d",
				lb.rateConverter.DecimalToPercentage(rate), offer.Amount, offer.Period)

			orderID, err := lb.client.SubmitFundingOffer(fundingSymbol, offer.Amount, rate, offer.Period, false)
			if err != nil {
				lb.getLogger().Printf("下订单失败: %v", err)
				result.FailedOfferCount++
			} else {
				// 追踪程序创建的订单
				lb.orderTracker.TrackOrder(orderID)
				lb.getLogger().Printf("成功创建订单 ID: %d，已加入追踪", orderID)
				orderCount++
				result.SuccessfulOfferCount++
			}
		}
	}

	return result, nil
}

func decimalDailyRateToPercent(rate float64) float64 {
	return rate * constants.PercentageToDecimal
}

func populateDecisionSummaryFromOffers(summary *StrategyDecisionSummary, loanOffers []*LoanOffer, rateConverter *rates.Converter) {
	if summary == nil || len(loanOffers) == 0 || rateConverter == nil {
		return
	}

	minRate := 0.0
	maxRate := 0.0
	minAmount := 0.0
	maxAmount := 0.0
	periodSet := make(map[int]struct{})
	fundSourceSet := make(map[string]struct{})
	depthSourceSet := make(map[string]struct{})
	rateSourceSet := make(map[string]struct{})
	periodSourceSet := make(map[string]struct{})
	executionDecisionSet := make(map[string]struct{})

	for i, offer := range loanOffers {
		if offer == nil {
			continue
		}

		ratePercent := rateConverter.DecimalToPercentage(offer.Rate)
		if i == 0 || ratePercent < minRate {
			minRate = ratePercent
		}
		if i == 0 || ratePercent > maxRate {
			maxRate = ratePercent
		}
		if i == 0 || offer.Amount < minAmount {
			minAmount = offer.Amount
		}
		if i == 0 || offer.Amount > maxAmount {
			maxAmount = offer.Amount
		}
		periodSet[offer.Period] = struct{}{}
		if offer.Reason.FundSource != "" {
			fundSourceSet[offer.Reason.FundSource] = struct{}{}
		}
		if offer.Reason.DepthSource != "" {
			depthSourceSet[offer.Reason.DepthSource] = struct{}{}
		}
		if offer.Reason.RateSource != "" {
			rateSourceSet[offer.Reason.RateSource] = struct{}{}
		}
		if offer.Reason.PeriodSource != "" {
			periodSourceSet[offer.Reason.PeriodSource] = struct{}{}
		}
		if offer.Reason.ExecutionDecision != "" {
			executionDecisionSet[offer.Reason.ExecutionDecision] = struct{}{}
		}
	}

	summary.MinOfferRatePercent = minRate
	summary.MaxOfferRatePercent = maxRate
	summary.MinOfferAmount = minAmount
	summary.MaxOfferAmount = maxAmount
	summary.LoanPeriods = make([]int, 0, len(periodSet))
	for period := range periodSet {
		summary.LoanPeriods = append(summary.LoanPeriods, period)
	}
	sort.Ints(summary.LoanPeriods)
	summary.FundSources = mapKeysToSortedSlice(fundSourceSet)
	summary.DepthSources = mapKeysToSortedSlice(depthSourceSet)
	summary.RateSources = mapKeysToSortedSlice(rateSourceSet)
	summary.PeriodSources = mapKeysToSortedSlice(periodSourceSet)
	summary.ExecutionDecisions = mapKeysToSortedSlice(executionDecisionSet)
}

func logStrategyDecisionSummary(logger *log.Logger, summary *StrategyDecisionSummary) {
	if logger == nil || summary == nil {
		return
	}

	periodParts := make([]string, 0, len(summary.LoanPeriods))
	for _, period := range summary.LoanPeriods {
		periodParts = append(periodParts, fmt.Sprintf("%d", period))
	}

	noteText := "无"
	if len(summary.Notes) > 0 {
		noteText = strings.Join(summary.Notes, "；")
	}
	fundSourceText := joinOrDefault(summary.FundSources)
	depthSourceText := joinOrDefault(summary.DepthSources)
	rateSourceText := joinOrDefault(summary.RateSources)
	periodSourceText := joinOrDefault(summary.PeriodSources)
	executionDecisionText := joinOrDefault(summary.ExecutionDecisions)

	logger.Println("策略决策摘要")
	logger.Printf("  概览: 策略=%s | Funding Symbol=%s | 触发来源=%s | 冷却豁免=%s | 跳过原因=%s",
		summary.Strategy,
		summary.FundingSymbol,
		defaultString(summary.TriggerSource, "自动触发"),
		boolText(summary.CooldownBypassed),
		defaultString(summary.SkipReason, "无"),
	)
	logger.Printf("  资金与盘口: 可用资金=%.4f | 保留金额=%.4f | 已有待处理订单=%s | Funding Book 来源=%s | Funding Book 档位数=%d",
		summary.FundsAvailable,
		summary.ReserveAmount,
		boolText(summary.HasPendingOrders),
		summary.FundingBookSource,
		summary.FundingBookEntries,
	)
	logger.Printf("  下单结果: 请求=%d | 尝试/成功/跳过/失败=%d/%d/%d/%d | FRR/固定=%d/%d | RATE_BONUS追加=%d | 利率范围=%.6f%%~%.6f%% | 金额范围=%.4f~%.4f | 期限=%s",
		summary.RequestedOfferCount,
		summary.AttemptedOfferCount,
		summary.SuccessfulOfferCount,
		summary.SkippedOfferCount,
		summary.FailedOfferCount,
		summary.FRROfferCount,
		summary.FixedRateOfferCount,
		summary.RateBonusAppliedCount,
		summary.MinOfferRatePercent,
		summary.MaxOfferRatePercent,
		summary.MinOfferAmount,
		summary.MaxOfferAmount,
		joinOrDefault(periodParts),
	)
	logger.Printf("  决策依据: 资金来源=%s | 深度来源=%s | 利率来源=%s | 期限来源=%s | 执行决策=%s",
		fundSourceText,
		depthSourceText,
		rateSourceText,
		periodSourceText,
		executionDecisionText,
	)
	logger.Printf("  备注: %s", noteText)
}

func describeDepthSource(fundingBook []*bitfinex.FundingBookEntry, depthIndex int) string {
	if len(fundingBook) == 0 {
		return "无 Funding Book，使用最小利率或合成逻辑"
	}
	if depthIndex < 0 || depthIndex >= len(fundingBook) {
		return fmt.Sprintf("Funding Book 深度索引 %d 超出范围，使用边界近似", depthIndex)
	}
	return fmt.Sprintf("Funding Book 深度索引 %d", depthIndex)
}

func describeTraditionalPeriodSource(cfg *config.Config, rate float64, period int) string {
	if cfg == nil {
		return fmt.Sprintf("期限 %d 天", period)
	}
	if cfg.LoanDays > 0 {
		return fmt.Sprintf("固定配置 LOAN_DAYS=%d", cfg.LoanDays)
	}
	for _, threshold := range cfg.GetSortedLoanPeriodThresholdsDesc() {
		if rateMeetsThreshold(rate, threshold.ThresholdDecimal) && threshold.Days == period {
			return fmt.Sprintf("达到 %d 天阈值 %.6f%%", threshold.Days, threshold.ThresholdPercent)
		}
	}
	return fmt.Sprintf("未达到更长期限阈值，使用默认 %d 天", period)
}

func mapKeysToSortedSlice(source map[string]struct{}) []string {
	if len(source) == 0 {
		return nil
	}
	result := make([]string, 0, len(source))
	for key := range source {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func joinOrDefault(values []string) string {
	if len(values) == 0 {
		return "无"
	}
	return strings.Join(values, " | ")
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func sumOfferAmounts(offers []*LoanOffer) float64 {
	total := 0.0
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		total += offer.Amount
	}
	return total
}

func sumPendingOfferAmounts(offers []*bitfinex.PendingFundingOffer) float64 {
	total := 0.0
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		total += floorToCents(offer.Amount)
	}
	return floorToCents(total)
}

func pendingOffersToLoanOffers(offers []*bitfinex.PendingFundingOffer) []*LoanOffer {
	if len(offers) == 0 {
		return nil
	}

	converted := make([]*LoanOffer, 0, len(offers))
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		converted = append(converted, &LoanOffer{
			Amount: floorToCents(offer.Amount),
			Rate:   offer.Rate,
			Period: offer.Period,
		})
	}
	return converted
}

func pendingOffersEquivalentToLoanOffers(current []*LoanOffer, target []*LoanOffer) bool {
	if len(current) != len(target) {
		return false
	}

	normalizedCurrent := normalizeLoanOffersForComparison(current)
	normalizedTarget := normalizeLoanOffersForComparison(target)
	for i := range normalizedCurrent {
		if normalizedCurrent[i].UseFRR != normalizedTarget[i].UseFRR {
			return false
		}
		if normalizedCurrent[i].Period != normalizedTarget[i].Period {
			return false
		}
		if normalizedCurrent[i].Rate != normalizedTarget[i].Rate {
			return false
		}
		if math.Abs(normalizedCurrent[i].Amount-normalizedTarget[i].Amount) > pendingOfferAmountTolerance {
			return false
		}
	}
	return true
}

func normalizeLoanOffersForComparison(offers []*LoanOffer) []*LoanOffer {
	if len(offers) == 0 {
		return nil
	}

	normalized := make([]*LoanOffer, 0, len(offers))
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		copied := *offer
		copied.Amount = floorToCents(copied.Amount)
		normalized = append(normalized, &copied)
	}

	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].Rate != normalized[j].Rate {
			return normalized[i].Rate < normalized[j].Rate
		}
		if normalized[i].Period != normalized[j].Period {
			return normalized[i].Period < normalized[j].Period
		}
		if normalized[i].Amount != normalized[j].Amount {
			return normalized[i].Amount < normalized[j].Amount
		}
		if normalized[i].UseFRR != normalized[j].UseFRR {
			return !normalized[i].UseFRR && normalized[j].UseFRR
		}
		return false
	})

	return normalized
}

func (lb *LendingBot) shouldBlockByCooldown(bypassCooldown bool) (bool, time.Duration) {
	if lb == nil || lb.config == nil || lb.config.ExecutionCooldownSeconds <= 0 || bypassCooldown {
		return false, 0
	}

	lb.executionMu.Lock()
	defer lb.executionMu.Unlock()

	if lb.lastExecutionAt.IsZero() {
		return false, 0
	}

	cooldown := time.Duration(lb.config.ExecutionCooldownSeconds) * time.Second
	elapsed := time.Since(lb.lastExecutionAt)
	if elapsed >= cooldown {
		return false, 0
	}

	return true, cooldown - elapsed
}

func (lb *LendingBot) markExecutionCompleted() {
	if lb == nil {
		return
	}

	lb.executionMu.Lock()
	defer lb.executionMu.Unlock()
	lb.lastExecutionAt = time.Now()
}

func (lb *LendingBot) cleanRecentOrderFingerprints() {
	if lb == nil || lb.config == nil || lb.config.OrderFingerprintTTL <= 0 {
		return
	}

	lb.executionMu.Lock()
	defer lb.executionMu.Unlock()

	ttl := time.Duration(lb.config.OrderFingerprintTTL) * time.Second
	now := time.Now()
	for fingerprint, createdAt := range lb.recentOrderFingerprints {
		if now.Sub(createdAt) > ttl {
			delete(lb.recentOrderFingerprints, fingerprint)
		}
	}
}

func buildOrderFingerprint(offer *LoanOffer, effectiveRate float64) string {
	if offer == nil {
		return ""
	}
	raw := fmt.Sprintf("%.2f|%.8f|%d|%t", floorToCents(offer.Amount), effectiveRate, offer.Period, offer.UseFRR)
	sum := sha1.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func (lb *LendingBot) shouldSkipByFingerprint(offer *LoanOffer, effectiveRate float64) bool {
	if lb == nil || lb.config == nil || lb.config.OrderFingerprintTTL <= 0 {
		return false
	}

	fingerprint := buildOrderFingerprint(offer, effectiveRate)
	if fingerprint == "" {
		return false
	}

	lb.executionMu.Lock()
	defer lb.executionMu.Unlock()

	now := time.Now()
	ttl := time.Duration(lb.config.OrderFingerprintTTL) * time.Second
	if createdAt, ok := lb.recentOrderFingerprints[fingerprint]; ok && now.Sub(createdAt) <= ttl {
		return true
	}

	lb.recentOrderFingerprints[fingerprint] = now
	return false
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]int, len(values))
	copy(cloned, values)
	return cloned
}

func cloneStrategyDecisionSummary(summary *StrategyDecisionSummary) *StrategyDecisionSummary {
	if summary == nil {
		return nil
	}

	cloned := *summary
	cloned.LoanPeriods = cloneIntSlice(summary.LoanPeriods)
	cloned.FundSources = cloneStringSlice(summary.FundSources)
	cloned.DepthSources = cloneStringSlice(summary.DepthSources)
	cloned.RateSources = cloneStringSlice(summary.RateSources)
	cloned.PeriodSources = cloneStringSlice(summary.PeriodSources)
	cloned.ExecutionDecisions = cloneStringSlice(summary.ExecutionDecisions)
	cloned.Notes = cloneStringSlice(summary.Notes)
	return &cloned
}

func (lb *LendingBot) setLastDecisionSummary(summary *StrategyDecisionSummary) {
	lb.lastDecisionMu.Lock()
	defer lb.lastDecisionMu.Unlock()
	lb.lastDecision = cloneStrategyDecisionSummary(summary)
}

func (lb *LendingBot) storeAndLogDecisionSummary(logger *log.Logger, summary *StrategyDecisionSummary) {
	if summary == nil {
		return
	}
	lb.setLastDecisionSummary(summary)
	logStrategyDecisionSummary(logger, summary)
}

// GetLastDecisionSummary 返回最近一次策略执行摘要的快照。
func (lb *LendingBot) GetLastDecisionSummary() *StrategyDecisionSummary {
	lb.lastDecisionMu.RLock()
	defer lb.lastDecisionMu.RUnlock()
	return cloneStrategyDecisionSummary(lb.lastDecision)
}

// BuildDecisionSummaryText 构建适合 Telegram 展示的最近一次策略执行摘要。
func (lb *LendingBot) BuildDecisionSummaryText() string {
	summary := lb.GetLastDecisionSummary()
	if summary == nil {
		return "📭 尚无策略执行摘要"
	}

	periodParts := make([]string, 0, len(summary.LoanPeriods))
	for _, period := range summary.LoanPeriods {
		periodParts = append(periodParts, fmt.Sprintf("%d", period))
	}

	return fmt.Sprintf(
		"📘 最近一次策略决策摘要\n\n【概览】\n策略: %s\nFunding Symbol: %s\n触发来源: %s\n冷却豁免: %s\n跳过原因: %s\n\n【资金与盘口】\n可用资金: %.4f\n保留金额: %.4f\n已有待处理订单: %s\nFunding Book 来源: %s\nFunding Book 档位数: %d\n\n【下单结果】\n请求订单数: %d\n尝试/成功/跳过/失败: %d/%d/%d/%d\nFRR/固定利率: %d/%d\n执行层追加 RATE_BONUS 次数: %d\n利率范围: %.6f%% ~ %.6f%%\n金额范围: %.4f ~ %.4f\n期限: %s\n\n【决策依据】\n资金来源: %s\n深度来源: %s\n利率来源: %s\n期限来源: %s\n执行决策: %s\n\n【备注】\n%s",
		summary.Strategy,
		summary.FundingSymbol,
		defaultString(summary.TriggerSource, "自动触发"),
		boolText(summary.CooldownBypassed),
		defaultString(summary.SkipReason, "无"),
		summary.FundsAvailable,
		summary.ReserveAmount,
		boolText(summary.HasPendingOrders),
		summary.FundingBookSource,
		summary.FundingBookEntries,
		summary.RequestedOfferCount,
		summary.AttemptedOfferCount,
		summary.SuccessfulOfferCount,
		summary.SkippedOfferCount,
		summary.FailedOfferCount,
		summary.FRROfferCount,
		summary.FixedRateOfferCount,
		summary.RateBonusAppliedCount,
		summary.MinOfferRatePercent,
		summary.MaxOfferRatePercent,
		summary.MinOfferAmount,
		summary.MaxOfferAmount,
		joinOrDefault(periodParts),
		joinOrDefault(summary.FundSources),
		joinOrDefault(summary.DepthSources),
		joinOrDefault(summary.RateSources),
		joinOrDefault(summary.PeriodSources),
		joinOrDefault(summary.ExecutionDecisions),
		joinOrDefault(summary.Notes),
	)
}

func boolText(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func formatBalanceDisplay(currency string, amount float64) string {
	symbol := currencySymbol(currency)
	if symbol != "" {
		return fmt.Sprintf("%s%.6f", symbol, amount)
	}
	return fmt.Sprintf("%s %.6f", strings.ToUpper(strings.TrimSpace(currency)), amount)
}

func currencySymbol(currency string) string {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USD":
		return "$"
	case "EUR":
		return "€"
	case "GBP":
		return "£"
	case "JPY":
		return "¥"
	default:
		return ""
	}
}

// BuildRuntimeConfigSummaryText 构建适合 Telegram 展示的运行配置摘要。
func (lb *LendingBot) BuildRuntimeConfigSummaryText() string {
	if lb == nil || lb.config == nil {
		return "❌ 配置不可用"
	}

	cfg := *lb.config
	if lb.config.LoanPeriodThresholds != nil {
		cfg.LoanPeriodThresholds = make(map[int]float64, len(lb.config.LoanPeriodThresholds))
		for days, threshold := range lb.config.LoanPeriodThresholds {
			cfg.LoanPeriodThresholds[days] = threshold
		}
	}

	telegramState := "已启用"
	if !cfg.IsTelegramEnabled() {
		telegramState = "已禁用（" + cfg.TelegramDisabledReason() + "）"
	}

	runMode := fmt.Sprintf("定时执行（每 %d 分钟）", cfg.MinutesRun)
	if cfg.RunOnlyOnNewCredits {
		runMode = "触发条件执行（新借贷订单或余额变化）"
	}

	orderLimit := fmt.Sprintf("%d", cfg.OrderLimit)
	if cfg.OrderLimit == 0 {
		orderLimit = "不限制"
	}

	loanDays := "自动判断"
	if cfg.LoanDays > 0 {
		loanDays = fmt.Sprintf("%d 天", cfg.LoanDays)
	}

	highHoldState := "关闭"
	if cfg.HighHoldAmount > 0 {
		highHoldState = fmt.Sprintf("%.2f %s x %d @ %.4f%%", cfg.HighHoldAmount, strings.ToUpper(cfg.Currency), cfg.HighHoldOrders, cfg.HighHoldRate)
	}

	return fmt.Sprintf(
		"⚙️ 运行配置摘要\n\nFunding Symbol: %s\n策略: %s\n执行模式: %s\n最低日利率: %s\n借贷天数: %s\n单次下单限制: %s\n单笔金额范围: %.2f ~ %s\n保留金额: %.2f %s\n通知阈值: %.4f%%\nTelegram: %s\n通知格式: %s\n测试模式: %t\n高额持有: %s\nRate Bonus: %.6f%%\nRate Range Increase: %.2f%%\nFunding Book UnderCut: %.6f%%\nLoan Period Thresholds: %s",
		cfg.GetFundingSymbol(),
		cfg.GetStrategy(),
		runMode,
		cfg.GetMinDailyRateDisplay(),
		loanDays,
		orderLimit,
		cfg.MinLoan,
		formatMaxLoanText(cfg.MaxLoan),
		cfg.ReserveAmount,
		strings.ToUpper(cfg.Currency),
		cfg.NotifyRateThreshold,
		telegramState,
		cfg.NotificationFormat,
		cfg.TestMode,
		highHoldState,
		cfg.RateBonus,
		cfg.RateRangeIncreasePercent*100,
		cfg.FundingBookRateUndercut,
		formatLoanPeriodThresholds(cfg.GetSortedLoanPeriodThresholdsDesc()),
	)
}

func formatMaxLoanText(maxLoan float64) string {
	if maxLoan <= 0 {
		return "不限"
	}
	return fmt.Sprintf("%.2f", maxLoan)
}

func formatLoanPeriodThresholds(thresholds []config.LoanPeriodThreshold) string {
	if len(thresholds) == 0 {
		return "无"
	}

	parts := make([]string, 0, len(thresholds))
	for _, threshold := range thresholds {
		parts = append(parts, fmt.Sprintf("%d天>=%.4f%%", threshold.Days, threshold.ThresholdPercent))
	}
	return strings.Join(parts, " | ")
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
		lb.notifyLendingCheckFailure(err)
		return false, err
	}

	// 获取当前活跃的借贷订单
	credits, err := lb.client.GetFundingCredits(lb.config.GetFundingSymbol())
	if err != nil {
		lb.getLogger().Printf("获取借贷订单失败: %v", err)
		lb.notifyLendingCheckFailure(err)
		return false, err
	}

	lb.resetLendingCheckFailureState()

	// 获取当前时间戳（毫秒）
	currentTime := time.Now().UnixNano() / int64(time.Millisecond)

	// 如果这是第一次检查，初始化时间戳和余额但不触发执行
	if lb.config.LastLendingCheckTime == 0 {
		lb.getLogger().Printf("首次检查，发现 %d 个现有借贷订单，余额: %.2f，初始化检查参数", len(credits), currentBalance)
		lb.config.LastLendingCheckTime = currentTime
		lb.config.LastAvailableBalance = currentBalance
		lb.config.SeenFundingCreditIDs = buildSeenCreditIDs(credits)
		lb.lastCredits = buildCreditSnapshot(credits)
		return false, nil
	}

	shouldExecute := false
	var reasons []string

	// 检查1: 是否有新的借贷订单
	newCredits := findNewCredits(credits, lb.config.SeenFundingCreditIDs, lb.config.LastLendingCheckTime)
	returnedCredits := findReturnedCredits(lb.lastCredits, credits)

	if len(newCredits) > 0 {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("发现 %d 个新的借贷订单", len(newCredits)))
		// 发送借贷通知
		if err := lb.sendLendingNotification(newCredits); err != nil {
			lb.getLogger().Printf("发送借贷通知失败: %v", err)
		}
	}

	if len(returnedCredits) > 0 {
		reasons = append(reasons, fmt.Sprintf("发现 %d 个贷出已结束/返还订单", len(returnedCredits)))
		if err := lb.sendReturnedLendingNotification(returnedCredits); err != nil {
			lb.getLogger().Printf("发送贷出已结束/返还通知失败: %v", err)
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
	lb.lastCredits = buildCreditSnapshot(credits)

	if shouldExecute {
		lb.getLogger().Printf("触发策略执行，原因: %s", strings.Join(reasons, "; "))
		return true, nil
	}

	lb.getLogger().Printf("无需执行策略，余额: %.2f (上次: %.2f)，无新借贷订单", currentBalance, lastBalance)
	return false, nil
}

func (lb *LendingBot) notifyLendingCheckFailure(err error) {
	if err == nil {
		return
	}

	errMsg := err.Error()
	if lb.errNotified && lb.lastCheckErr == errMsg {
		return
	}

	lb.lastCheckErr = errMsg
	lb.errNotified = true

	if lb.notifyCallback == nil {
		lb.getLogger().Println("Telegram 通知回调未设置，跳过借贷检查失败通知")
		return
	}

	message := fmt.Sprintf("⚠️ 借贷检查连续失败\n\n原因: %s\n\n%s", errMsg, lendingCheckFailureGuidance(err))
	if notifyErr := lb.notifyCallback(message); notifyErr != nil {
		lb.getLogger().Printf("发送借贷检查失败通知失败: %v", notifyErr)
	}
}

func lendingCheckFailureGuidance(err error) string {
	switch {
	case internalerrors.HasCode(err, internalerrors.ErrCodeAPITimeout):
		return "机器人暂时无法确认新的贷出成交或余额变化。这次更像是 Bitfinex 私有 API 或网络链路短时超时，建议优先观察是否自动恢复；若频繁出现，再检查服务器到 Bitfinex 的网络质量。"
	case internalerrors.HasCode(err, internalerrors.ErrCodeAuthentication):
		return "机器人暂时无法确认新的贷出成交或余额变化，请检查 Bitfinex API key、权限配置、nonce 状态与是否存在多实例共用同一组 key。"
	default:
		return "机器人暂时无法确认新的贷出成交或余额变化，请结合错误类型检查 Bitfinex API 可用性、认证配置以及服务器运行状态。"
	}
}

func (lb *LendingBot) resetLendingCheckFailureState() {
	lb.lastCheckErr = ""
	lb.errNotified = false
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

func buildCreditSnapshot(credits []*bitfinex.FundingCredit) map[int64]*bitfinex.FundingCredit {
	snapshot := make(map[int64]*bitfinex.FundingCredit, len(credits))
	for _, credit := range credits {
		if credit == nil || credit.ID == 0 {
			continue
		}
		copied := *credit
		snapshot[credit.ID] = &copied
	}
	return snapshot
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

func findReturnedCredits(previous map[int64]*bitfinex.FundingCredit, current []*bitfinex.FundingCredit) []*bitfinex.FundingCredit {
	if len(previous) == 0 {
		return nil
	}

	currentIDs := buildSeenCreditIDs(current)
	returned := make([]*bitfinex.FundingCredit, 0)
	for id, credit := range previous {
		if _, exists := currentIDs[id]; !exists && credit != nil {
			returned = append(returned, credit)
		}
	}
	return returned
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

	if strings.EqualFold(lb.config.NotificationFormat, "aligned") {
		return lb.sendAlignedLendingNotification("💰 新的借贷订单通知", credits, true)
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

func (lb *LendingBot) sendReturnedLendingNotification(credits []*bitfinex.FundingCredit) error {
	if lb.notifyCallback == nil {
		lb.getLogger().Println("Telegram 通知回调未设置，跳过贷出已结束/返还通知")
		return nil
	}

	if strings.EqualFold(lb.config.NotificationFormat, "aligned") {
		return lb.sendAlignedLendingNotification("💸 贷出已结束/返还通知", credits, false)
	}

	message := "💸 贷出已结束/返还通知\n\n"
	totalAmount := 0.0

	for i, credit := range credits {
		if i >= constants.MaxDisplayOrders {
			remaining := len(credits) - constants.MaxDisplayOrders
			message += fmt.Sprintf("... 还有 %d 个订单\n", remaining)
			break
		}

		effectiveRate := credit.EffectiveDailyRate()
		openTime := time.Unix(credit.MTSOpened/1000, 0)
		totalAmount += credit.Amount

		message += fmt.Sprintf("📊 订单 #%d (ID: %d)\n", i+1, credit.ID)
		message += fmt.Sprintf("💵 金额: %.2f %s\n", credit.Amount, lb.config.Currency)
		message += fmt.Sprintf("📈 日利率: %.4f%%\n", lb.rateConverter.DecimalToPercentage(effectiveRate))
		message += fmt.Sprintf("⏰ 期间: %d 天\n", credit.Period)
		message += fmt.Sprintf("🕐 开始时间: %s\n", openTime.Format("2006-01-02 15:04:05"))
		message += fmt.Sprintf("🧾 状态: %s\n", credit.Status)
		message += fmt.Sprintf("🔔 检测时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
		message += "\n"
	}

	for i := constants.MaxDisplayOrders; i < len(credits); i++ {
		if credits[i] != nil {
			totalAmount += credits[i].Amount
		}
	}

	message += fmt.Sprintf("📊 统计信息:\n")
	message += fmt.Sprintf("📦 总数量: %d 个订单\n", len(credits))
	message += fmt.Sprintf("💵 总金额: %.2f %s\n", totalAmount, lb.config.Currency)

	if err := lb.notifyCallback(message); err != nil {
		lb.getLogger().Printf("发送贷出已结束/返还通知失败: %v", err)
		lb.getLogger().Println("贷出已结束/返还通知内容:")
		lb.getLogger().Println(message)
		return nil
	}

	lb.getLogger().Println("贷出已结束/返还通知发送成功")
	return nil
}

func (lb *LendingBot) sendAlignedLendingNotification(title string, credits []*bitfinex.FundingCredit, includeEarnings bool) error {
	message := title + "\n\n"

	frrFallbackRate := 0.0
	for _, credit := range credits {
		if credit != nil && credit.EffectiveDailyRate() == 0 {
			rate, err := lb.client.GetCurrentFundingRate(lb.config.GetFundingSymbol())
			if err != nil {
				lb.getLogger().Printf("取得 FRR 利率失败: %v", err)
				break
			}
			frrFallbackRate = rate
			break
		}
	}

	totalAmount := 0.0
	totalEarnings := 0.0
	detectionTime := time.Now().Format("2006-01-02 15:04:05")

	for i, credit := range credits {
		if credit == nil {
			continue
		}
		if i >= constants.MaxDisplayOrders {
			remaining := len(credits) - constants.MaxDisplayOrders
			message += fmt.Sprintf("... 还有 %d 个订单\n", remaining)
			break
		}

		effectiveRate := credit.EffectiveDailyRate()
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		periodEarnings := credit.Amount * effectiveRate * float64(credit.Period)
		totalAmount += credit.Amount
		totalEarnings += periodEarnings

		rows := [][2]string{
			{"金额", formatting.FormatCurrency(credit.Amount, lb.config.Currency, lb.config.NotificationFormat)},
			{"日利率", fmt.Sprintf("%.4f%%", lb.rateConverter.DecimalToPercentage(effectiveRate))},
			{"期间", fmt.Sprintf("%d 天", credit.Period)},
		}
		if includeEarnings {
			rows = append(rows,
				[2]string{"预期收益", formatting.FormatCurrency(periodEarnings, lb.config.Currency, lb.config.NotificationFormat)},
				[2]string{"开始时间", time.Unix(credit.MTSOpened/1000, 0).Format("2006-01-02 15:04:05")},
			)
		} else {
			rows = append(rows,
				[2]string{"开始时间", time.Unix(credit.MTSOpened/1000, 0).Format("2006-01-02 15:04:05")},
				[2]string{"检测时间", detectionTime},
			)
		}

		message += fmt.Sprintf("📊 订单 #%d (ID: %d)\n", i+1, credit.ID)
		message += formatting.BuildAlignedBlock(rows)
		message += "\n\n"
	}

	summaryRows := [][2]string{
		{"总订单数", fmt.Sprintf("%d", len(credits))},
		{"总金额", formatting.FormatCurrency(totalAmount, lb.config.Currency, lb.config.NotificationFormat)},
	}
	if includeEarnings {
		summaryRows = append(summaryRows, [2]string{"总预期收益", formatting.FormatCurrency(totalEarnings, lb.config.Currency, lb.config.NotificationFormat)})
	}

	message += "📊 统计信息\n"
	message += formatting.BuildAlignedBlock(summaryRows)

	if err := lb.notifyCallback(message); err != nil {
		lb.getLogger().Printf("发送对齐格式通知失败: %v", err)
		lb.getLogger().Println("对齐格式通知内容:")
		lb.getLogger().Println(message)
		return nil
	}

	lb.getLogger().Println("对齐格式通知发送成功")
	return nil
}

// GetActiveLendingCredits 获取活跃借贷订单（供 Telegram 指令使用）
func (lb *LendingBot) GetActiveLendingCredits() ([]*bitfinex.FundingCredit, error) {
	return lb.client.GetFundingCredits(lb.config.GetFundingSymbol())
}

// calculateKlineOffers 基于K线数据计算贷出订单
func (lb *LendingBot) calculateKlineOffers(fundsAvailable float64) ([]*LoanOffer, error) {
	var loanOffers []*LoanOffer

	// 检查可用资金
	if fundsAvailable < lb.config.MinLoan {
		return loanOffers, nil
	}

	// 获取K线数据
	candles, err := lb.client.GetFundingCandles(
		lb.config.GetFundingSymbol(),
		lb.config.KlineTimeFrame,
		lb.config.KlinePeriod,
	)
	if err != nil {
		lb.getLogger().Printf("取得 K 线数据失败: %v", err)
		return nil, err
	}
	if len(candles) == 0 {
		return nil, fmt.Errorf("kline strategy requires candles but received empty response")
	}

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

	return loanOffers, nil
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
			Reason: LoanOfferReason{
				FundSource:   fmt.Sprintf("K 线策略分散资金，第 %d 笔", i+1),
				DepthSource:  "不使用 Funding Book 深度，基于 K 线目标利率分散",
				RateSource:   fmt.Sprintf("K 线目标利率 %.6f%% 加上第 %d 笔递增", decimalDailyRateToPercent(targetRate), i+1),
				PeriodSource: describeTraditionalPeriodSource(lb.config, rate, period),
			},
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
