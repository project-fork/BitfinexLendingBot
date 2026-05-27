package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/report"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		configPath = os.Args[1]
	}

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}

	client := bitfinex.NewClient(cfg.BitfinexApiKey, cfg.BitfinexSecretKey)
	now := time.Now().In(time.FixedZone("CST", 8*3600))

	activeCredits, err := client.GetFundingCredits(cfg.GetFundingSymbol())
	if err != nil {
		log.Fatalf("get active credits failed: %v", err)
	}

	historyCredits, err := client.GetFundingCreditsHistory(cfg.GetFundingSymbol())
	if err != nil {
		log.Fatalf("get history credits failed: %v", err)
	}

	windowStart, windowEnd := reportWindow(now)
	ledgers, err := client.GetLedgersFiltered(strings.ToUpper(cfg.Currency), windowStart.UnixMilli(), windowEnd.UnixMilli(), 2500, "funding", 28)
	if err != nil {
		log.Fatalf("get filtered ledgers failed: %v", err)
	}

	selectedCredits := report.SelectCreditsForYesterdayEstimate(now, activeCredits, historyCredits)
	builder := report.NewDailyEarningsReportBuilder(cfg)
	_, summary, err := builder.Build(now, selectedCredits, ledgers)
	if err != nil {
		log.Fatalf("build summary failed: %v", err)
	}

	fmt.Printf("NOW: %s\n", now.Format(time.RFC3339))
	fmt.Printf("WINDOW_START: %s\n", windowStart.Format(time.RFC3339))
	fmt.Printf("WINDOW_END: %s\n", windowEnd.Format(time.RFC3339))
	fmt.Printf("ACTIVE_CREDITS: %d\n", len(activeCredits))
	fmt.Printf("HISTORY_CREDITS: %d\n", len(historyCredits))
	fmt.Printf("SELECTED_CREDITS: %d\n", len(selectedCredits))
	fmt.Printf("LEDGERS_FILTERED: %d\n", len(ledgers))
	fmt.Printf("YESTERDAY_ESTIMATED: %.8f\n", summary.YesterdayEstimated)
	fmt.Printf("YESTERDAY_ACTUAL: %.8f\n", summary.YesterdayActual)
	fmt.Printf("LAST_7_DAYS_ACTUAL: %.8f\n", summary.Last7DaysActual)
	fmt.Printf("THIS_WEEK_ACTUAL: %.8f\n", summary.ThisWeekActual)
	fmt.Printf("THIS_MONTH_ACTUAL: %.8f\n", summary.ThisMonthActual)
	fmt.Printf("THIS_YEAR_ACTUAL: %.8f\n", summary.ThisYearActual)
	fmt.Printf("PARTICIPATING_AMOUNT: %.8f\n", summary.ParticipatingAmount)

	printSelectedCredits(now, selectedCredits)
	printLedgerSamples(ledgers)
}

func reportWindow(now time.Time) (time.Time, time.Time) {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	yearStart := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	if yearStart.Before(todayStart.AddDate(0, 0, -6)) {
		return yearStart, tomorrowStart
	}
	return todayStart.AddDate(0, 0, -6), tomorrowStart
}

func printSelectedCredits(now time.Time, credits []*bitfinex.FundingCredit) {
	fmt.Println("SELECTED_CREDIT_SAMPLES:")
	if len(credits) == 0 {
		fmt.Println("  <none>")
		return
	}

	sortedCredits := make([]*bitfinex.FundingCredit, 0, len(credits))
	for _, credit := range credits {
		if credit != nil {
			sortedCredits = append(sortedCredits, credit)
		}
	}
	sort.Slice(sortedCredits, func(i, j int) bool {
		return sortedCredits[i].ID < sortedCredits[j].ID
	})

	limit := 8
	if len(sortedCredits) < limit {
		limit = len(sortedCredits)
	}
	for i := 0; i < limit; i++ {
		credit := sortedCredits[i]
		fmt.Printf(
			"  ID=%d STATUS=%s AMOUNT=%.8f RATE=%.8f PERIOD=%d OPEN=%s UPDATED=%s\n",
			credit.ID,
			credit.Status,
			credit.Amount,
			credit.EffectiveDailyRate(),
			credit.Period,
			formatMillis(credit.MTSOpened, now.Location()),
			formatMillis(credit.MTSUpdated, now.Location()),
		)
	}
}

func printLedgerSamples(ledgers []*bitfinex.LedgerEntry) {
	fmt.Println("LEDGER_SAMPLES:")
	if len(ledgers) == 0 {
		fmt.Println("  <none>")
		return
	}

	sort.Slice(ledgers, func(i, j int) bool {
		if ledgers[i] == nil {
			return false
		}
		if ledgers[j] == nil {
			return true
		}
		return ledgers[i].MTS > ledgers[j].MTS
	})

	limit := 12
	if len(ledgers) < limit {
		limit = len(ledgers)
	}
	for i := 0; i < limit; i++ {
		entry := ledgers[i]
		if entry == nil {
			continue
		}
		fmt.Printf(
			"  ID=%d MTS=%s AMOUNT=%.8f DESC=%q\n",
			entry.ID,
			formatMillis(entry.MTS, time.FixedZone("CST", 8*3600)),
			entry.Amount,
			entry.Description,
		)
	}
}

func formatMillis(mts int64, loc *time.Location) string {
	if mts == 0 {
		return "-"
	}
	return time.UnixMilli(mts).In(loc).Format("2006-01-02 15:04:05")
}
