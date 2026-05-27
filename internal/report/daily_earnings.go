package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/formatting"
)

const fundingFeeMultiplier = 0.85

type DailyEarningsSummary struct {
	ParticipatingCredits int
	ParticipatingAmount  float64
	YesterdayEstimated   float64
	YesterdayActual      float64
	Last7DaysActual      float64
	ThisWeekActual       float64
	ThisMonthActual      float64
	ThisYearActual       float64
}

type DailyEarningsReportBuilder struct {
	config *config.Config
}

func NewDailyEarningsReportBuilder(cfg *config.Config) *DailyEarningsReportBuilder {
	return &DailyEarningsReportBuilder{config: cfg}
}

func (b *DailyEarningsReportBuilder) Build(now time.Time, credits []*bitfinex.FundingCredit, ledgers []*bitfinex.LedgerEntry) (string, DailyEarningsSummary, error) {
	summary := DailyEarningsSummary{}
	if b == nil || b.config == nil {
		return "", summary, fmt.Errorf("daily earnings report builder requires config")
	}

	yesterdayStart, todayStart := dayBounds(now)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	weekStart := startOfWeek(todayStart)
	monthStart := time.Date(todayStart.Year(), todayStart.Month(), 1, 0, 0, 0, 0, todayStart.Location())
	yearStart := time.Date(todayStart.Year(), time.January, 1, 0, 0, 0, 0, todayStart.Location())
	last7DaysStart := todayStart.AddDate(0, 0, -6)

	for _, credit := range credits {
		if credit == nil {
			continue
		}

		activeSeconds := activeSecondsInWindow(credit, yesterdayStart, todayStart)
		if activeSeconds <= 0 {
			continue
		}

		summary.ParticipatingCredits++
		summary.ParticipatingAmount += credit.Amount
		summary.YesterdayEstimated += credit.Amount * credit.EffectiveDailyRate() * (float64(activeSeconds) / 86400.0) * fundingFeeMultiplier
	}

	for _, entry := range ledgers {
		if entry == nil {
			continue
		}
		if !isInterestLedger(entry) {
			continue
		}
		entryTime := time.UnixMilli(entry.MTS).In(todayStart.Location())
		amount := entry.Amount

		// 昨天收益通常会在今天入账，因此“昨日实际到账”统计今天的入账窗口。
		if !entryTime.Before(todayStart) && entryTime.Before(tomorrowStart) {
			summary.YesterdayActual += amount
		}
		if !entryTime.Before(last7DaysStart) && entryTime.Before(tomorrowStart) {
			summary.Last7DaysActual += amount
		}
		if !entryTime.Before(weekStart) && entryTime.Before(tomorrowStart) {
			summary.ThisWeekActual += amount
		}
		if !entryTime.Before(monthStart) && entryTime.Before(tomorrowStart) {
			summary.ThisMonthActual += amount
		}
		if !entryTime.Before(yearStart) && entryTime.Before(tomorrowStart) {
			summary.ThisYearActual += amount
		}
	}

	return b.buildMessage(now, summary), summary, nil
}

func SelectCreditsForYesterdayEstimate(now time.Time, active []*bitfinex.FundingCredit, history []*bitfinex.FundingCredit) []*bitfinex.FundingCredit {
	yesterdayStart, todayStart := dayBounds(now)
	selected := make([]*bitfinex.FundingCredit, 0, len(active)+len(history))
	seen := make(map[int64]struct{}, len(active)+len(history))

	for _, credit := range active {
		if credit == nil {
			continue
		}
		selected = append(selected, credit)
		seen[credit.ID] = struct{}{}
	}

	for _, credit := range history {
		if credit == nil {
			continue
		}
		if _, ok := seen[credit.ID]; ok {
			continue
		}
		if credit.MTSUpdated == 0 {
			continue
		}
		if !isClosedCreditStatus(credit.Status) {
			continue
		}
		updatedAt := time.UnixMilli(credit.MTSUpdated).In(now.Location())
		if !updatedAt.Before(yesterdayStart) && updatedAt.Before(todayStart) {
			selected = append(selected, credit)
			seen[credit.ID] = struct{}{}
		}
	}

	return selected
}

func (b *DailyEarningsReportBuilder) buildMessage(now time.Time, summary DailyEarningsSummary) string {
	if strings.EqualFold(b.config.NotificationFormat, "aligned") {
		return b.buildAlignedMessage(now, summary)
	}

	currency := b.config.Currency
	message := fmt.Sprintf("每日收益报告（%s）\n\n", now.Format("2006-01-02"))
	message += "昨日收益对比\n"
	message += fmt.Sprintf("贷出订单数：%d\n", summary.ParticipatingCredits)
	message += fmt.Sprintf("贷出总金额：%s\n", formatting.FormatCurrency(summary.ParticipatingAmount, currency, b.config.NotificationFormat))
	message += fmt.Sprintf("预估收益：%s\n", formatting.FormatCurrency(summary.YesterdayEstimated, currency, b.config.NotificationFormat))
	message += fmt.Sprintf("实际收益：%s\n", formatting.FormatCurrency(summary.YesterdayActual, currency, b.config.NotificationFormat))
	message += fmt.Sprintf("收益差额：%s\n\n", formatting.FormatCurrency(summary.YesterdayActual-summary.YesterdayEstimated, currency, b.config.NotificationFormat))
	message += "收益汇总\n"
	message += fmt.Sprintf("近 7 天：%s\n", formatting.FormatCurrency(summary.Last7DaysActual, currency, b.config.NotificationFormat))
	message += fmt.Sprintf("本周：%s\n", formatting.FormatCurrency(summary.ThisWeekActual, currency, b.config.NotificationFormat))
	message += fmt.Sprintf("本月：%s\n", formatting.FormatCurrency(summary.ThisMonthActual, currency, b.config.NotificationFormat))
	message += fmt.Sprintf("本年：%s\n", formatting.FormatCurrency(summary.ThisYearActual, currency, b.config.NotificationFormat))
	return message
}

func (b *DailyEarningsReportBuilder) buildAlignedMessage(now time.Time, summary DailyEarningsSummary) string {
	currency := b.config.Currency
	message := fmt.Sprintf("每日收益报告（%s）\n\n", now.Format("2006-01-02"))
	message += "昨日收益对比\n"
	message += formatting.BuildAlignedBlock([][2]string{
		{"贷出订单数", fmt.Sprintf("%d", summary.ParticipatingCredits)},
		{"贷出总金额", formatting.FormatCurrency(summary.ParticipatingAmount, currency, b.config.NotificationFormat)},
		{"预估收益", formatting.FormatCurrency(summary.YesterdayEstimated, currency, b.config.NotificationFormat)},
		{"实际收益", formatting.FormatCurrency(summary.YesterdayActual, currency, b.config.NotificationFormat)},
		{"收益差额", formatting.FormatCurrency(summary.YesterdayActual-summary.YesterdayEstimated, currency, b.config.NotificationFormat)},
	})
	message += "\n\n收益汇总\n"
	message += formatting.BuildAlignedBlock([][2]string{
		{"近 7 天", formatting.FormatCurrency(summary.Last7DaysActual, currency, b.config.NotificationFormat)},
		{"本周", formatting.FormatCurrency(summary.ThisWeekActual, currency, b.config.NotificationFormat)},
		{"本月", formatting.FormatCurrency(summary.ThisMonthActual, currency, b.config.NotificationFormat)},
		{"本年", formatting.FormatCurrency(summary.ThisYearActual, currency, b.config.NotificationFormat)},
	})
	return message
}

func dayBounds(now time.Time) (time.Time, time.Time) {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return todayStart.AddDate(0, 0, -1), todayStart
}

func startOfWeek(day time.Time) time.Time {
	weekday := int(day.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return day.AddDate(0, 0, -(weekday - 1))
}

func activeSecondsInWindow(credit *bitfinex.FundingCredit, start time.Time, end time.Time) int64 {
	if credit == nil {
		return 0
	}

	openTime := time.UnixMilli(credit.MTSOpened).In(start.Location())
	if openTime.IsZero() {
		openTime = time.UnixMilli(credit.MTSCreated).In(start.Location())
	}

	closeTime := end
	if !strings.EqualFold(credit.Status, "ACTIVE") {
		closeTime = nonActiveCreditCloseTime(credit, openTime, end)
	}

	if openTime.Before(start) {
		openTime = start
	}
	if closeTime.After(end) {
		closeTime = end
	}
	if !closeTime.After(openTime) {
		return 0
	}
	return int64(closeTime.Sub(openTime).Seconds())
}

func nonActiveCreditCloseTime(credit *bitfinex.FundingCredit, openTime time.Time, fallback time.Time) time.Time {
	closeTime := fallback
	if credit == nil {
		return closeTime
	}

	if credit.MTSUpdated > 0 {
		updatedAt := time.UnixMilli(credit.MTSUpdated).In(openTime.Location())
		if updatedAt.After(openTime) {
			return updatedAt
		}
	}

	if credit.Period > 0 {
		periodEnd := openTime.Add(time.Duration(credit.Period) * 24 * time.Hour)
		if periodEnd.After(openTime) {
			return periodEnd
		}
	}

	return closeTime
}

func isClosedCreditStatus(status string) bool {
	status = strings.TrimSpace(strings.ToUpper(status))
	switch status {
	case "CLOSED", "CANCELLED":
		return true
	default:
		return false
	}
}

func isInterestLedger(entry *bitfinex.LedgerEntry) bool {
	if entry == nil {
		return false
	}
	description := strings.ToLower(strings.TrimSpace(entry.Description))
	if description == "" {
		return false
	}
	return strings.Contains(description, "interest") ||
		strings.Contains(description, "margin funding payment")
}
