package strategy

import (
	"fmt"
	"sort"
	"strings"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
)

// ReplayInput 定义策略回放的最小输入结构。
// 第一阶段仅覆盖 Funding Book 驱动策略，不包含 K 线 candles。
type ReplayInput struct {
	Strategy         string                       `json:"strategy"`
	FundsAvailable   float64                      `json:"funds_available"`
	HasPendingOrders bool                         `json:"has_pending_orders"`
	FundingBook      []*bitfinex.FundingBookEntry `json:"funding_book"`
}

// ReplayOutput 代表一次策略回放结果。
type ReplayOutput struct {
	Offers          []*LoanOffer             `json:"offers"`
	DecisionSummary *StrategyDecisionSummary `json:"decision_summary"`
}

// ReplayStrategy 使用给定输入直接重放 Funding Book 驱动策略结果。
func (lb *LendingBot) ReplayStrategy(input ReplayInput) (*ReplayOutput, error) {
	if lb == nil || lb.config == nil {
		return nil, fmt.Errorf("lending bot config is not initialized")
	}

	replayStrategy := strings.TrimSpace(input.Strategy)
	if replayStrategy == "" {
		replayStrategy = lb.config.GetStrategy()
	}
	if replayStrategy == config.StrategyKline {
		return nil, fmt.Errorf("kline strategy replay requires candle inputs and is not supported by funding-book replay")
	}

	cfgSnapshot := *lb.config
	if lb.config.LoanPeriodThresholds != nil {
		cfgSnapshot.LoanPeriodThresholds = make(map[int]float64, len(lb.config.LoanPeriodThresholds))
		for days, threshold := range lb.config.LoanPeriodThresholds {
			cfgSnapshot.LoanPeriodThresholds[days] = threshold
		}
	}
	cfgSnapshot.SetStrategy(replayStrategy)

	fundingBook := cloneFundingBookEntries(input.FundingBook)
	fundingBook = normalizeReplayFundingBook(fundingBook)

	decisionSummary := &StrategyDecisionSummary{
		Strategy:           cfgSnapshot.GetStrategy(),
		FundingSymbol:      cfgSnapshot.GetFundingSymbol(),
		FundsAvailable:     input.FundsAvailable,
		ReserveAmount:      cfgSnapshot.ReserveAmount,
		HasPendingOrders:   input.HasPendingOrders,
		FundingBookEntries: len(fundingBook),
		FundingBookSource:  "replay_input",
		Notes:              []string{"基于回放输入生成，不会真实下单"},
	}

	if input.FundsAvailable < cfgSnapshot.MinLoan {
		decisionSummary.Notes = append(decisionSummary.Notes,
			fmt.Sprintf("回放资金 %.4f 低于最小下单金额 %.4f", input.FundsAvailable, cfgSnapshot.MinLoan))
		return &ReplayOutput{
			Offers:          nil,
			DecisionSummary: decisionSummary,
		}, nil
	}

	replayBot := &LendingBot{
		config:         &cfgSnapshot,
		rateConverter:  rates.NewConverter(),
		simpleStrategy: NewSimpleStrategy(&cfgSnapshot),
		smartStrategy:  NewSmartStrategy(&cfgSnapshot),
		logger:         lb.getLogger(),
	}
	replayBot.simpleStrategy.SetLogger(replayBot.logger)
	replayBot.smartStrategy.SetLogger(replayBot.logger)

	var offers []*LoanOffer
	switch cfgSnapshot.GetStrategy() {
	case config.StrategySimple:
		offers = replayBot.simpleStrategy.CalculateOffers(input.FundsAvailable, fundingBook)
	case config.StrategySmart:
		offers = replayBot.smartStrategy.CalculateSmartOffers(input.FundsAvailable, fundingBook)
	default:
		offers = replayBot.calculateLoanOffers(input.FundsAvailable, fundingBook)
	}

	decisionSummary.RequestedOfferCount = len(offers)
	populateDecisionSummaryFromOffers(decisionSummary, offers, replayBot.rateConverter)
	applyReplayExecutionSummary(&cfgSnapshot, decisionSummary, offers, input.HasPendingOrders)

	return &ReplayOutput{
		Offers:          cloneLoanOffers(offers),
		DecisionSummary: cloneStrategyDecisionSummary(decisionSummary),
	}, nil
}

func normalizeReplayFundingBook(fundingBook []*bitfinex.FundingBookEntry) []*bitfinex.FundingBookEntry {
	if len(fundingBook) == 0 {
		return nil
	}

	askSide := make([]*bitfinex.FundingBookEntry, 0, len(fundingBook))
	for _, entry := range fundingBook {
		if entry == nil {
			continue
		}
		if entry.Amount > 0 {
			askSide = append(askSide, entry)
		}
	}

	if len(askSide) == 0 {
		askSide = append(askSide, fundingBook...)
	}

	sort.SliceStable(askSide, func(i, j int) bool {
		if askSide[i].Rate == askSide[j].Rate {
			return askSide[i].Amount < askSide[j].Amount
		}
		return askSide[i].Rate < askSide[j].Rate
	})

	return askSide
}

func cloneFundingBookEntries(entries []*bitfinex.FundingBookEntry) []*bitfinex.FundingBookEntry {
	if len(entries) == 0 {
		return nil
	}

	cloned := make([]*bitfinex.FundingBookEntry, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		copied := *entry
		cloned = append(cloned, &copied)
	}
	return cloned
}

func cloneLoanOffers(offers []*LoanOffer) []*LoanOffer {
	if len(offers) == 0 {
		return nil
	}

	cloned := make([]*LoanOffer, 0, len(offers))
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		copied := *offer
		cloned = append(cloned, &copied)
	}
	return cloned
}

func applyReplayExecutionSummary(cfg *config.Config, summary *StrategyDecisionSummary, offers []*LoanOffer, hasPendingOrders bool) {
	if cfg == nil || summary == nil {
		return
	}

	summary.AttemptedOfferCount = len(offers)
	summary.SuccessfulOfferCount = len(offers)
	for _, offer := range offers {
		if offer == nil {
			continue
		}
		if offer.UseFRR {
			summary.FRROfferCount++
			if offer.Reason.ExecutionDecision == "" {
				offer.Reason.ExecutionDecision = "回放模式：FRR 挂单，未真实下单"
			}
		} else {
			summary.FixedRateOfferCount++
			if hasPendingOrders {
				if offer.Reason.ExecutionDecision == "" {
					offer.Reason.ExecutionDecision = "回放模式：存在既有待处理订单，不追加 RATE_BONUS"
				}
			} else {
				summary.RateBonusAppliedCount++
				if offer.Reason.ExecutionDecision == "" {
					offer.Reason.ExecutionDecision = fmt.Sprintf("回放模式：无既有待处理订单，理论上会追加 RATE_BONUS %.6f%%", cfg.RateBonus)
				}
			}
		}
	}

	populateDecisionSummaryFromOffers(summary, offers, rates.NewConverter())
}
