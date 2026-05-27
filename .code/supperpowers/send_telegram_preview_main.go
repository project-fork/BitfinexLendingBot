package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/report"
)

const previewChatID int64 = 5488788297

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

	bot, err := tgbotapi.NewBotAPI(cfg.TelegramBotToken)
	if err != nil {
		log.Fatal(err)
	}

	msg := tgbotapi.NewMessage(previewChatID, "[预览发送]\n\n"+message)
	if strings.Contains(msg.Text, "<pre>") || strings.Contains(msg.Text, "<b>") || strings.Contains(msg.Text, "<i>") || strings.Contains(msg.Text, "<code>") {
		msg.ParseMode = tgbotapi.ModeHTML
	}

	sent, err := bot.Send(msg)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("TELEGRAM_SENT_OK chat_id=%d message_id=%d\n", sent.Chat.ID, sent.MessageID)
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
