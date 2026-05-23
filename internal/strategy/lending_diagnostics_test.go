package strategy

import (
	"strings"
	"testing"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

func TestBuildRuntimeConfigSummaryText_IncludesKeyFields(t *testing.T) {
	bot := &LendingBot{
		config: &config.Config{
			Currency:                 "USD",
			Strategy:                 config.StrategySmart,
			RunOnlyOnNewCredits:      true,
			MinutesRun:               15,
			MinLoan:                  150,
			MaxLoan:                  500,
			LoanDays:                 0,
			MinDailyLendRate:         "FRR",
			ReserveAmount:            100,
			NotifyRateThreshold:      0.03,
			OrderLimit:               0,
			TelegramBotToken:         "",
			TelegramAuthToken:        "",
			NotificationFormat:       "aligned",
			TestMode:                 true,
			HighHoldAmount:           300,
			HighHoldOrders:           2,
			HighHoldRate:             0.08,
			RateBonus:                0.02,
			RateRangeIncreasePercent: 0.15,
			FundingBookRateUndercut:  0.0001,
			LoanPeriodThresholds: map[int]float64{
				30: 0.03,
				60: 0.04,
			},
		},
	}

	text := bot.BuildRuntimeConfigSummaryText()
	expectedFragments := []string{
		"⚙️ 运行配置摘要",
		"Funding Symbol: fUSD",
		"策略: smart",
		"执行模式: 触发条件执行",
		"最低日利率: FRR",
		"单次下单限制: 不限制",
		"Telegram: 已禁用",
		"Loan Period Thresholds: 60天>=0.0400% | 30天>=0.0300%",
	}
	for _, fragment := range expectedFragments {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected summary to contain %q, got:\n%s", fragment, text)
		}
	}
}

func TestExecute_StoresLastDecisionSummary(t *testing.T) {
	client := &summaryFundingClient{
		fundingBook: []*bitfinex.FundingBookEntry{
			{Rate: 0.0003, Amount: 500, Period: 30},
			{Rate: 0.00031, Amount: 800, Period: 30},
			{Rate: 0.00032, Amount: 1200, Period: 30},
		},
	}

	bot := &LendingBot{
		config: &config.Config{
			Currency:         "USD",
			Strategy:         config.StrategyTraditional,
			MinDailyLendRate: 0.03,
			SpreadLend:       3,
			GapBottom:        0,
			GapTop:           2,
			MinLoan:          150,
			MaxLoan:          2000,
			OrderLimit:       10,
			TestMode:         true,
			LoanPeriodThresholds: map[int]float64{
				30: 0.03,
			},
			HighHoldRate:   0.05,
			HighHoldAmount: 0,
			HighHoldOrders: 1,
		},
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
	}

	if err := bot.Execute(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	summary := bot.GetLastDecisionSummary()
	if summary == nil {
		t.Fatal("expected last decision summary to be stored")
	}
	if summary.Strategy != config.StrategyTraditional {
		t.Fatalf("expected strategy %q, got %q", config.StrategyTraditional, summary.Strategy)
	}
	if summary.SuccessfulOfferCount == 0 {
		t.Fatalf("expected successful offers to be recorded, got %+v", summary)
	}

	text := bot.BuildDecisionSummaryText()
	for _, fragment := range []string{
		"📘 最近一次策略决策摘要",
		"【概览】",
		"策略: traditional",
		"触发来源: 自动触发",
		"冷却豁免: 否",
		"跳过原因: 无",
		"【资金与盘口】",
		"Funding Book 来源: required_and_used",
		"【下单结果】",
		"【决策依据】",
		"【备注】",
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("expected decision summary text to contain %q, got:\n%s", fragment, text)
		}
	}
}
