package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/report"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	client := bitfinex.NewClient(cfg.BitfinexApiKey, cfg.BitfinexSecretKey)
	now := time.Now().In(time.FixedZone("CST", 8*3600))

	activeCredits, err := client.GetFundingCredits(cfg.GetFundingSymbol())
	if err != nil {
		log.Fatal(err)
	}

	historyCredits, err := client.GetFundingCreditsHistory(cfg.GetFundingSymbol())
	if err != nil {
		log.Fatal(err)
	}

	windowStart, windowEnd := reportWindow(now)
	ledgers, err := client.GetLedgersFiltered(strings.ToUpper(cfg.Currency), windowStart.UnixMilli(), windowEnd.UnixMilli(), 2500, "funding", 28)
	if err != nil {
		log.Fatal(err)
	}

	selectedCredits := report.SelectCreditsForYesterdayEstimate(now, activeCredits, historyCredits)
	builder := report.NewDailyEarningsReportBuilder(cfg)
	message, _, err := builder.Build(now, selectedCredits, ledgers)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(message)
}

func reportWindow(now time.Time) (time.Time, time.Time) {
	loc := now.Location()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	yearStart := time.Date(now.Year(), time.January, 1, 0, 0, 0, 0, loc)
	if yearStart.Before(todayStart.AddDate(0, 0, -6)) {
		return yearStart, tomorrowStart
	}
	return todayStart.AddDate(0, 0, -6), tomorrowStart
}
