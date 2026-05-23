package telegram

import "github.com/kfrico/BitfinexLendingBot/internal/rates"

func NewBotWithDataFileForTest(dataFilePath string) *Bot {
	bot := &Bot{
		rateConverter: rates.NewConverter(),
		dataFilePath:  dataFilePath,
	}
	bot.loadPersistentData()
	return bot
}
