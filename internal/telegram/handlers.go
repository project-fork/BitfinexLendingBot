package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/formatting"
)

// handleRate 处理利率查询指令
func (b *Bot) handleRate(chatID int64) {
	rate, err := b.bitfinexClient.GetCurrentFundingRate(b.config.GetFundingSymbol())
	if err != nil {
		b.sendMessage(chatID, "取得贷出利率失败")
		return
	}

	thresholdInfo := ""
	if b.config.NotifyRateThreshold > 0 {
		thresholdInfo = fmt.Sprintf("\n目前设定的阈值为: %.4f%%", b.config.NotifyRateThreshold)
	}

	message := fmt.Sprintf("目前贷出利率: %.4f%%%s",
		b.rateConverter.DecimalDailyToPercentageDaily(rate), thresholdInfo)
	b.sendMessage(chatID, message)
}

// handleCheck 处理利率检查指令
func (b *Bot) handleCheck(chatID int64) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借贷机器人未初始化")
		return
	}

	// 使用新的K线基础检查方法
	exceeded, percentageRate, err := b.lendingBot.CheckRateThreshold()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 取得利率数据失败: %v", err))
		return
	}

	replyMsg := fmt.Sprintf("📊 利率阈值检查报告\n\n")
	replyMsg += fmt.Sprintf("🎯 检查方式: 5分钟K线最近12根高点\n")
	replyMsg += fmt.Sprintf("📈 最高利率: %.4f%%\n", percentageRate)

	replyMsg += fmt.Sprintf("🎚️ 设定阈值: %.4f%%\n\n", b.config.NotifyRateThreshold)

	if exceeded {
		replyMsg += "⚠️ 注意: 最近1小时最高利率已超过阈值!"
	} else {
		replyMsg += "✅ 最近1小时最高利率低于阈值"
	}

	b.sendMessage(chatID, replyMsg)
}

// handleStatus 处理状态查询指令
func (b *Bot) handleStatus(chatID int64) {
	// 获取剩余金额
	availableFunds, err := b.bitfinexClient.GetFundingBalance(strings.ToUpper(b.config.Currency))
	var balanceInfo string
	if err != nil {
		balanceInfo = fmt.Sprintf("剩余金额: 获取失败 (%v)", err)
	} else {
		balanceInfo = fmt.Sprintf("💰 资金状况:\n总余额: %.2f %s",
			availableFunds, b.config.Currency)
	}

	statusMsg := fmt.Sprintf("📊 系统状态报告\n\n%s\n\n💱 基本设定:\n币种: %s\n最小贷出金额: %.2f\n最大贷出金额: %.2f",
		balanceInfo, b.config.Currency, b.config.MinLoan, b.config.MaxLoan)

	// 添加保留金额信息
	if b.config.ReserveAmount > 0 {
		statusMsg += fmt.Sprintf("\n保留金额: %.2f", b.config.ReserveAmount)
	} else {
		statusMsg += "\n保留金额: 未设置"
	}

	// 添加机器人运行参数
	statusMsg += fmt.Sprintf("\n\n⚙️ 机器人参数:")
	statusMsg += fmt.Sprintf("\n单次下单限制: %d", b.config.OrderLimit)
	if b.config.LoanDays > 0 {
		statusMsg += fmt.Sprintf("\n固定借贷天数: %d 天", b.config.LoanDays)
	} else {
		statusMsg += "\n固定借贷天数: 自动判断"
	}
	if b.config.IsMinDailyLendRateFRR() {
		statusMsg += fmt.Sprintf("\n最低日利率: %s (FRR 挂单模式)", b.config.GetMinDailyRateDisplay())
	} else {
		statusMsg += fmt.Sprintf("\n最低日利率: %s", b.config.GetMinDailyRateDisplay())
	}
	statusMsg += fmt.Sprintf("\n执行间隔: %d 分钟", b.config.MinutesRun)

	// 添加运行模式信息
	if b.config.TestMode {
		statusMsg += fmt.Sprintf("\n\n🧪 运行模式: 测试模式 (模拟交易)")
	} else {
		statusMsg += fmt.Sprintf("\n\n🚀 运行模式: 正式模式 (真实交易)")
	}

	// 添加高额持有策略信息
	statusMsg += fmt.Sprintf("\n\n💎 高额持有策略:")
	if b.config.HighHoldAmount > 0 {
		statusMsg += fmt.Sprintf("\n金额: %.2f %s", b.config.HighHoldAmount, b.config.Currency)
		statusMsg += fmt.Sprintf("\n日利率: %.4f%%", b.config.HighHoldRate)
		statusMsg += fmt.Sprintf("\n订单数量: %d", b.config.HighHoldOrders)
	} else {
		statusMsg += "\n未启用"
	}

	// 添加当前策略信息
	statusMsg += fmt.Sprintf("\n\n🎯 当前策略:")
	if b.config.EnableKlineStrategy {
		statusMsg += fmt.Sprintf("\nK线策略 (启用)")
		statusMsg += fmt.Sprintf("\n时间框架: %s", b.config.KlineTimeFrame)
		statusMsg += fmt.Sprintf("\n周期数: %d", b.config.KlinePeriod)
		statusMsg += fmt.Sprintf("\n加成: %.1f%%", b.config.KlineSpreadPercent)
	} else if b.config.EnableSmartStrategy {
		statusMsg += fmt.Sprintf("\n智能策略 (启用)")
		statusMsg += fmt.Sprintf("\n利率范围增加: %.1f%%", b.config.RateRangeIncreasePercent*100)
	} else {
		statusMsg += fmt.Sprintf("\n传统策略 (启用)")
	}

	// 添加利率范围增加百分比 (对所有策略都适用)
	if b.config.RateRangeIncreasePercent > 0 {
		statusMsg += fmt.Sprintf("\n📊 利率范围增加: %.1f%%", b.config.RateRangeIncreasePercent*100)
	}

	statusMsg += fmt.Sprintf("\n\n💡 使用 /strategy 查看详细策略状态")

	b.sendMessage(chatID, statusMsg)
}

// handlePendingOffers 处理未成交订单查询指令
func (b *Bot) handlePendingOffers(chatID int64) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借贷机器人未初始化")
		return
	}

	offers, err := b.lendingBot.ListPendingFundingOffers()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 获取未成交订单失败: %v", err))
		return
	}

	if len(offers) == 0 {
		b.sendMessage(chatID, "📭 当前没有未成交订单")
		return
	}

	if strings.EqualFold(b.config.NotificationFormat, "aligned") {
		b.sendMessage(chatID, b.buildAlignedPendingOffersMessage(offers))
		return
	}

	message := "📭 当前未成交订单\n\n"
	totalAmount := 0.0
	trackedCount := 0

	displayCount := len(offers)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		offer := offers[i]
		totalAmount += offer.Amount

		orderType := "手动挂单"
		if offer.IsTracked {
			orderType = "程序追踪"
			trackedCount++
		}

		message += fmt.Sprintf("📊 订单 #%d (ID: %d)\n", i+1, offer.ID)
		message += fmt.Sprintf("💵 金额: %.2f %s\n", offer.Amount, b.config.Currency)
		message += fmt.Sprintf("📈 日利率: %.4f%%\n", b.rateConverter.DecimalToPercentage(offer.Rate))
		message += fmt.Sprintf("⏰ 期间: %d 天\n", offer.Period)
		message += fmt.Sprintf("🔖 类型: %s\n\n", orderType)
	}

	for i := displayCount; i < len(offers); i++ {
		totalAmount += offers[i].Amount
		if offers[i].IsTracked {
			trackedCount++
		}
	}

	manualCount := len(offers) - trackedCount
	if len(offers) > 10 {
		message += fmt.Sprintf("... 还有 %d 个订单未显示\n\n", len(offers)-10)
	}

	message += "📊 统计信息:\n"
	message += fmt.Sprintf("总订单数: %d\n", len(offers))
	message += fmt.Sprintf("总金额: %.2f %s\n", totalAmount, b.config.Currency)
	message += fmt.Sprintf("程序追踪: %d\n", trackedCount)
	message += fmt.Sprintf("手动挂单: %d", manualCount)

	b.sendMessage(chatID, message)
}

func (b *Bot) buildAlignedPendingOffersMessage(offers []*bitfinex.PendingFundingOffer) string {
	message := "📭 当前未成交订单\n\n"
	totalAmount := 0.0
	trackedCount := 0

	displayCount := len(offers)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		offer := offers[i]
		totalAmount += offer.Amount

		orderType := "手动挂单"
		if offer.IsTracked {
			orderType = "程序追踪"
			trackedCount++
		}

		message += fmt.Sprintf("📊 订单 #%d (ID: %d)\n", i+1, offer.ID)
		message += formatting.BuildAlignedBlock([][2]string{
			{"金额", formatting.FormatCurrency(offer.Amount, b.config.Currency, b.config.NotificationFormat)},
			{"日利率", fmt.Sprintf("%.4f%%", b.rateConverter.DecimalToPercentage(offer.Rate))},
			{"期间", fmt.Sprintf("%d 天", offer.Period)},
			{"类型", orderType},
		})
		message += "\n\n"
	}

	for i := displayCount; i < len(offers); i++ {
		totalAmount += offers[i].Amount
		if offers[i].IsTracked {
			trackedCount++
		}
	}

	manualCount := len(offers) - trackedCount
	if len(offers) > 10 {
		message += fmt.Sprintf("... 还有 %d 个订单未显示\n\n", len(offers)-10)
	}

	message += "📊 统计信息\n"
	message += formatting.BuildAlignedBlock([][2]string{
		{"总订单数", fmt.Sprintf("%d", len(offers))},
		{"总金额", formatting.FormatCurrency(totalAmount, b.config.Currency, b.config.NotificationFormat)},
		{"程序追踪", fmt.Sprintf("%d", trackedCount)},
		{"手动挂单", fmt.Sprintf("%d", manualCount)},
	})

	return message
}

func (b *Bot) handlePendingReply(message *tgbotapi.Message) bool {
	if message == nil || message.Chat == nil {
		return false
	}

	command, ok := b.getPendingReply(message.Chat.ID)
	if !ok {
		return false
	}

	if message.ReplyToMessage == nil {
		return false
	}

	b.clearPendingReply(message.Chat.ID)
	b.handleCommand(message.Chat.ID, fmt.Sprintf("%s %s", command, strings.TrimSpace(message.Text)))
	return true
}

func (b *Bot) requestCommandArgument(chatID int64, command string, prompt string) {
	b.setPendingReply(chatID, command)
	msg := tgbotapi.NewMessage(chatID, prompt)
	msg.ReplyMarkup = tgbotapi.ForceReply{ForceReply: true}
	if err := b.sendChattable(msg); err != nil {
		_ = b.sendMessage(chatID, "❌ 发送参数输入提示失败")
	}
}

// handleSetThreshold 处理设置阈值指令
func (b *Bot) handleSetThreshold(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/threshold", "请回复 /threshold 的参数值，例如 0.03")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /threshold [数值] 格式")
		return
	}

	threshold, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || threshold <= 0 {
		b.sendMessage(chatID, "请输入有效的正数值")
		return
	}

	b.config.NotifyRateThreshold = threshold
	b.sendMessage(chatID, fmt.Sprintf("阈值已设定为: %.4f%%", threshold))
}

// handleSetReserve 处理设置保留金额指令
func (b *Bot) handleSetReserve(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/reserve", "请回复 /reserve 的参数值，例如 100")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /reserve [数值] 格式")
		return
	}

	reserve, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || reserve < 0 {
		b.sendMessage(chatID, "请输入有效的非负数值")
		return
	}

	b.config.ReserveAmount = reserve
	b.sendMessage(chatID, fmt.Sprintf("保留金额已设定为: %.2f", reserve))
}

// handleSetOrderLimit 处理设置订单限制指令
func (b *Bot) handleSetOrderLimit(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/orderlimit", "请回复 /orderlimit 的参数值，例如 3")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /orderlimit [数值] 格式")
		return
	}

	limit, err := strconv.Atoi(parts[1])
	if err != nil || limit < 0 {
		b.sendMessage(chatID, "请输入有效的非负整数")
		return
	}

	b.config.OrderLimit = limit
	b.sendMessage(chatID, fmt.Sprintf("单次执行最大下单数量限制已设定为: %d", limit))
}

// handleSetLoanDays 处理设置固定借贷天数指令
func (b *Bot) handleSetLoanDays(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/loandays", "请回复 /loandays 的参数值，例如 30（或 0 表示自动）")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /loandays [数值] 格式\n提示: 设置为 0 表示自动判断")
		return
	}

	days, err := strconv.Atoi(parts[1])
	if err != nil || days < 0 {
		b.sendMessage(chatID, "请输入有效的非负整数\n提示: 设置为 0 表示自动判断")
		return
	}

	if days == 1 || days > constants.Period120Days {
		b.sendMessage(chatID, "借贷天数必须是 0，或介于 2 到 120 之间的整数")
		return
	}

	b.config.LoanDays = days
	if days == 0 {
		b.sendMessage(chatID, "固定借贷天数已设为: 自动判断")
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("固定借贷天数已设定为: %d 天", days))
}

// handleSetMinDailyRate 处理设置最低日利率指令
func (b *Bot) handleSetMinDailyRate(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/mindailylendrate", "请回复 /mindailylendrate 的参数值，例如 0.03 或 FRR")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /mindailylendrate [数值|FRR] 格式")
		return
	}

	input := strings.TrimSpace(parts[1])
	if strings.EqualFold(input, constants.MinDailyRateModeFRR) {
		b.config.MinDailyLendRate = constants.MinDailyRateModeFRR
		b.sendMessage(chatID, "最低每日贷出利率已设定为: FRR（将使用 FRR 模式挂单）")
		return
	}

	rate, err := strconv.ParseFloat(input, 64)
	if err != nil || rate <= 0 {
		b.sendMessage(chatID, "请输入有效的正数值")
		return
	}

	if !b.rateConverter.ValidatePercentageRate(rate) {
		b.sendMessage(chatID, "利率超出有效范围 (0-7%)")
		return
	}

	b.config.MinDailyLendRate = rate
	b.sendMessage(chatID, fmt.Sprintf("最低每日贷出利率已设定为: %.4f%%", rate))
}

// handleSetMinLoan 处理设置最小贷出金额指令
func (b *Bot) handleSetMinLoan(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/minloan", "请回复 /minloan 的参数值，例如 150")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /minloan [数值] 格式")
		return
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || amount <= 0 {
		b.sendMessage(chatID, "请输入有效的正数值")
		return
	}

	// 检查是否小于等于最大贷出金额
	if b.config.MaxLoan > 0 && amount > b.config.MaxLoan {
		b.sendMessage(chatID, fmt.Sprintf("最小贷出金额不能大于最大贷出金额 (%.2f %s)", b.config.MaxLoan, b.config.Currency))
		return
	}

	b.config.MinLoan = amount
	b.sendMessage(chatID, fmt.Sprintf("✅ 最小贷出金额已设定为: %.2f %s", amount, b.config.Currency))
}

// handleSetMaxLoan 处理设置最大贷出金额指令
func (b *Bot) handleSetMaxLoan(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/maxloan", "请回复 /maxloan 的参数值，例如 500（或 0 表示无限制）")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /maxloan [数值] 格式\n提示: 设置为 0 表示无限制")
		return
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || amount < 0 {
		b.sendMessage(chatID, "请输入有效的非负数值\n提示: 设置为 0 表示无限制")
		return
	}

	// 检查是否大于等于最小贷出金额
	if amount > 0 && amount < b.config.MinLoan {
		b.sendMessage(chatID, fmt.Sprintf("最大贷出金额不能小于最小贷出金额 (%.2f %s)", b.config.MinLoan, b.config.Currency))
		return
	}

	b.config.MaxLoan = amount

	if amount == 0 {
		b.sendMessage(chatID, "✅ 最大贷出金额已设定为: 无限制")
	} else {
		b.sendMessage(chatID, fmt.Sprintf("✅ 最大贷出金额已设定为: %.2f %s", amount, b.config.Currency))
	}
}

// handleSetHighHoldRate 处理设置高额持有利率指令
func (b *Bot) handleSetHighHoldRate(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/highholdrate", "请回复 /highholdrate 的参数值，例如 0.05")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /highholdrate [数值] 格式")
		return
	}

	rate, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || rate <= 0 {
		b.sendMessage(chatID, "请输入有效的正数值")
		return
	}

	if !b.rateConverter.ValidatePercentageRate(rate) {
		b.sendMessage(chatID, "利率超出有效范围 (0-7%)")
		return
	}

	b.config.HighHoldRate = rate
	b.sendMessage(chatID, fmt.Sprintf("高额持有策略的日利率已设定为: %.4f%%", rate))
}

// handleSetHighHoldAmount 处理设置高额持有金额指令
func (b *Bot) handleSetHighHoldAmount(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/highholdamount", "请回复 /highholdamount 的参数值，例如 1000（或 0 关闭）")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /highholdamount [数值] 格式\n提示: 设置为 0 可关闭高额持有策略")
		return
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || amount < 0 {
		b.sendMessage(chatID, "请输入有效的非负数值\n提示: 设置为 0 可关闭高额持有策略")
		return
	}

	b.config.HighHoldAmount = amount

	if amount == 0 {
		b.sendMessage(chatID, "✅ 高额持有策略已关闭\n高额持有金额已设定为: 0.00")
	} else {
		b.sendMessage(chatID, fmt.Sprintf("✅ 高额持有策略已启用\n高额持有金额已设定为: %.2f %s", amount, b.config.Currency))
	}
}

// handleSetHighHoldOrders 处理设置高额持有订单数量指令
func (b *Bot) handleSetHighHoldOrders(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/highholdorders", "请回复 /highholdorders 的参数值，例如 3")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /highholdorders [数值] 格式")
		return
	}

	orders, err := strconv.Atoi(parts[1])
	if err != nil || orders < 1 {
		b.sendMessage(chatID, "请输入有效的正整数")
		return
	}

	b.config.HighHoldOrders = orders
	b.sendMessage(chatID, fmt.Sprintf("高额持有订单数量已设定为: %d", orders))
}

// handleSetRateRangeIncrease 处理设置利率范围增加百分比指令
func (b *Bot) handleSetRateRangeIncrease(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) == 1 {
		b.requestCommandArgument(chatID, "/raterangeincrease", "请回复 /raterangeincrease 的参数值，例如 10")
		return
	}
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /raterangeincrease [数值] 格式")
		return
	}

	percentage, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || percentage <= 0 {
		b.sendMessage(chatID, "请输入有效的正数值")
		return
	}

	// 验证范围 (0-100%)
	if percentage > 100.0 {
		b.sendMessage(chatID, "利率范围增加百分比不能超过 100%")
		return
	}

	// 转换为小数形式 (0-1.0)
	decimalValue := percentage / 100.0

	b.config.RateRangeIncreasePercent = decimalValue
	b.sendMessage(chatID, fmt.Sprintf("利率范围增加百分比已设定为: %.2f%% (%.4f)", percentage, decimalValue))
}

// handleRestart 处理重启指令
func (b *Bot) handleRestart(chatID int64) {
	if b.restartCallback == nil {
		b.sendMessage(chatID, "❌ 重启功能未初始化，请联系管理员")
		return
	}

	b.sendDangerousActionConfirmation(
		chatID,
		"确认重跑",
		"⚠️ 将取消程序追踪的未成交订单，并重新执行策略。\n\n是否继续？",
		"confirm:restart",
		"cancel:restart",
	)
}

// handleRun 处理保留未成交订单直接重跑指令
func (b *Bot) handleRun(chatID int64) {
	b.sendMessage(chatID, "🔄 开始重新执行策略，将保留现有未成交订单...")

	if b.runCallback == nil {
		b.sendMessage(chatID, "❌ 直接重跑功能未初始化，请联系管理员")
		return
	}

	err := b.runCallback()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 直接重跑失败: %v", err))
		return
	}

	b.sendMessage(chatID, "✅ 重跑完成！已保留现有未成交订单，并继续按策略执行")
}

// handleCancelPendingOffers 处理取消未成交订单指令
func (b *Bot) handleCancelPendingOffers(chatID int64, text string) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借贷机器人未初始化")
		return
	}

	parts := strings.Fields(text)
	includeAll := false
	if len(parts) > 2 || (len(parts) == 2 && !strings.EqualFold(parts[1], "all")) {
		b.sendMessage(chatID, "格式错误，请使用 /canceloffers 或 /canceloffers all")
		return
	}
	if len(parts) == 2 {
		includeAll = true
	}

	title := "确认取消"
	message := "⚠️ 将取消程序追踪的未成交订单。\n\n是否继续？"
	confirmData := "confirm:canceloffers:tracked"
	cancelData := "cancel:canceloffers:tracked"
	if includeAll {
		title = "确认取消全部"
		message = "⚠️ 将取消全部未成交订单，包括手动挂单。\n\n是否继续？"
		confirmData = "confirm:canceloffers:all"
		cancelData = "cancel:canceloffers:all"
	}

	b.sendDangerousActionConfirmation(chatID, title, message, confirmData, cancelData)
}

func (b *Bot) handleCallbackQuery(query *tgbotapi.CallbackQuery) {
	if query == nil {
		return
	}

	switch query.Data {
	case "confirm:restart":
		_ = b.answerCallback(tgbotapi.NewCallbackWithAlert(query.ID, "已确认，开始执行重跑"))
		if query.Message != nil {
			b.handleConfirmedRestart(query.Message.Chat.ID)
		}
	case "confirm:canceloffers:tracked":
		_ = b.answerCallback(tgbotapi.NewCallbackWithAlert(query.ID, "已确认，开始取消程序追踪订单"))
		if query.Message != nil {
			b.executeConfirmedCancelOffers(query.Message.Chat.ID, false)
		}
	case "confirm:canceloffers:all":
		_ = b.answerCallback(tgbotapi.NewCallbackWithAlert(query.ID, "已确认，开始取消全部未成交订单"))
		if query.Message != nil {
			b.executeConfirmedCancelOffers(query.Message.Chat.ID, true)
		}
	case "cancel:restart", "cancel:canceloffers:tracked", "cancel:canceloffers:all":
		_ = b.answerCallback(tgbotapi.NewCallbackWithAlert(query.ID, "操作已取消"))
	default:
		_ = b.answerCallback(tgbotapi.NewCallback(query.ID, "未知操作"))
	}
}

func (b *Bot) sendDangerousActionConfirmation(chatID int64, title string, text string, confirmData string, cancelData string) {
	msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("🔐 %s\n\n%s", title, text))
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("确认执行", confirmData),
			tgbotapi.NewInlineKeyboardButtonData("取消", cancelData),
		),
	)

	if err := b.sendChattable(msg); err != nil {
		_ = b.sendMessage(chatID, "❌ 发送确认消息失败")
	}
}

func (b *Bot) handleConfirmedRestart(chatID int64) {
	if b.restartCallback == nil {
		b.sendMessage(chatID, "❌ 重启功能未初始化，请联系管理员")
		return
	}

	if err := b.restartCallback(); err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 重启失败: %v", err))
		return
	}

	b.sendMessage(chatID, "✅ 重跑完成！程序追踪的未成交订单已取消，并已重新执行策略")
}

func (b *Bot) executeConfirmedCancelOffers(chatID int64, includeAll bool) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借贷机器人未初始化")
		return
	}

	summary, err := b.lendingBot.CancelPendingFundingOffers(includeAll)
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 取消未成交订单失败: %v", err))
		return
	}

	scopeText := "取消程序追踪的未成交订单"
	if includeAll {
		scopeText = "取消全部未成交订单"
	}

	message := fmt.Sprintf("✅ %s完成\n\n", scopeText)
	message += fmt.Sprintf("总订单数: %d\n", summary.Total)
	message += fmt.Sprintf("已取消: %d\n", summary.Cancelled)
	message += fmt.Sprintf("已跳过: %d\n", summary.Skipped)
	message += fmt.Sprintf("失败: %d", summary.Failed)

	b.sendMessage(chatID, message)
}

// handleStrategyStatus 处理策略状态查询指令
func (b *Bot) handleStrategyStatus(chatID int64) {
	var strategyType string
	var strategyPriority string

	// 根据策略优先级确定当前启用的策略
	if b.config.EnableKlineStrategy {
		strategyType = "K线策略 (启用)"
		strategyPriority = "最高优先级"
	} else if b.config.EnableSmartStrategy {
		strategyType = "智能策略 (启用)"
		strategyPriority = "中等优先级"
	} else {
		strategyType = "传统策略 (启用)"
		strategyPriority = "默认策略"
	}

	statusMsg := fmt.Sprintf("📊 当前策略状态\n策略类型: %s\n优先级: %s", strategyType, strategyPriority)

	// K线策略设定
	if b.config.EnableKlineStrategy {
		statusMsg += fmt.Sprintf("\n\n📈 K线策略设定:")
		statusMsg += fmt.Sprintf("\n时间框架: %s", b.config.KlineTimeFrame)
		statusMsg += fmt.Sprintf("\nK线周期数: %d", b.config.KlinePeriod)
		statusMsg += fmt.Sprintf("\n加成百分比: %.1f%%", b.config.KlineSpreadPercent)

		// 添加平滑方法信息
		smoothMethodDesc := getSmoothMethodDescription(b.config.KlineSmoothMethod)
		statusMsg += fmt.Sprintf("\n利率平滑方法: %s - %s", b.config.KlineSmoothMethod, smoothMethodDesc)

		// 计算分析时间范围
		var timeRange string
		switch b.config.KlineTimeFrame {
		case "5m":
			minutes := float64(b.config.KlinePeriod) * 5
			timeRange = fmt.Sprintf("%.1f分钟", minutes)
		case "15m":
			hours := float64(b.config.KlinePeriod) * 0.25
			timeRange = fmt.Sprintf("%.1f小时", hours)
		case "30m":
			hours := float64(b.config.KlinePeriod) * 0.5
			timeRange = fmt.Sprintf("%.1f小时", hours)
		case "1h":
			timeRange = fmt.Sprintf("%d小时", b.config.KlinePeriod)
		case "3h":
			hours := b.config.KlinePeriod * 3
			timeRange = fmt.Sprintf("%d小时", hours)
		case "6h":
			hours := b.config.KlinePeriod * 6
			timeRange = fmt.Sprintf("%d小时", hours)
		case "12h":
			days := float64(b.config.KlinePeriod) * 0.5
			timeRange = fmt.Sprintf("%.1f天", days)
		case "1D":
			timeRange = fmt.Sprintf("%d天", b.config.KlinePeriod)
		default:
			timeRange = "未知"
		}
		statusMsg += fmt.Sprintf("\n分析时间范围: %s", timeRange)

		statusMsg += fmt.Sprintf("\n\nK线策略功能:")
		statusMsg += fmt.Sprintf("\n✅ 基于真实市场K线数据")
		statusMsg += fmt.Sprintf("\n✅ 自动找寻最高利率")
		statusMsg += fmt.Sprintf("\n✅ 智能加成计算")
		statusMsg += fmt.Sprintf("\n✅ 分散风险贷出")
		statusMsg += fmt.Sprintf("\n✅ 自动回退机制")

		// 添加策略建议
		statusMsg += fmt.Sprintf("\n\n📋 时间框架建议:")
		statusMsg += fmt.Sprintf("\n⚡ 短期: 15m-30m (快速反应)")
		statusMsg += fmt.Sprintf("\n⚖️ 中期: 1h-3h (平衡策略)")
		statusMsg += fmt.Sprintf("\n🛡️ 长期: 6h-1D (稳定策略)")
	} else if b.config.EnableSmartStrategy {
		statusMsg += fmt.Sprintf("\n\n🧠 智能策略设定:")
		statusMsg += fmt.Sprintf("\n波动率阈值: %.4f", b.config.VolatilityThreshold)
		statusMsg += fmt.Sprintf("\n最大利率倍数: %.1fx", b.config.MaxRateMultiplier)
		statusMsg += fmt.Sprintf("\n最小利率倍数: %.1fx", b.config.MinRateMultiplier)
		statusMsg += fmt.Sprintf("\n利率范围增加: %.1f%%", b.config.RateRangeIncreasePercent*100)

		// 添加建议值提示
		statusMsg += fmt.Sprintf("\n\n📋 参数建议值:")
		statusMsg += fmt.Sprintf("\n🛡️ 保守: 波动率 0.001, 最大倍数 1.5x, 最小倍数 0.9x")
		statusMsg += fmt.Sprintf("\n⚖️ 平衡: 波动率 0.002, 最大倍数 2.0x, 最小倍数 0.8x")
		statusMsg += fmt.Sprintf("\n⚡ 激进: 波动率 0.003, 最大倍数 3.0x, 最小倍数 0.7x")

		statusMsg += fmt.Sprintf("\n\n智能功能:")
		statusMsg += fmt.Sprintf("\n✅ 动态利率调整")
		statusMsg += fmt.Sprintf("\n✅ 市场趋势分析")
		statusMsg += fmt.Sprintf("\n✅ 智能期间选择")
		statusMsg += fmt.Sprintf("\n✅ 竞争对手分析")
		statusMsg += fmt.Sprintf("\n✅ 自适应资金配置")
	} else {
		statusMsg += fmt.Sprintf("\n\n⚙️ 传统策略设定:")
		statusMsg += fmt.Sprintf("\n固定高额持有利率: %.4f%%", b.config.HighHoldRate)
		statusMsg += fmt.Sprintf("\n固定分散贷出参数")
		statusMsg += fmt.Sprintf("\n固定期间选择逻辑")
	}

	// 显示策略优先级顺序
	statusMsg += fmt.Sprintf("\n\n🔄 策略优先级顺序:")
	statusMsg += fmt.Sprintf("\n1️⃣ K线策略 (%s)", getStrategyStatus(b.config.EnableKlineStrategy))
	statusMsg += fmt.Sprintf("\n2️⃣ 智能策略 (%s)", getStrategyStatus(b.config.EnableSmartStrategy))
	statusMsg += fmt.Sprintf("\n3️⃣ 传统策略 (默认)")

	statusMsg += fmt.Sprintf("\n\n💡 提示: 使用指令切换策略")
	statusMsg += fmt.Sprintf("\n/klinestrategy on/off - 切换K线策略")
	statusMsg += fmt.Sprintf("\n/smartstrategy on/off - 切换智能策略")

	b.sendMessage(chatID, statusMsg)
}

// getStrategyStatus 获取策略状态文字
func getStrategyStatus(enabled bool) string {
	if enabled {
		return "启用"
	}
	return "停用"
}

// handleToggleSmartStrategy 处理智能策略切换指令
func (b *Bot) handleToggleSmartStrategy(chatID int64, enable bool) {
	b.config.EnableSmartStrategy = enable

	var message string
	if enable {
		// 如果启用智能策略，自动关闭K线策略
		b.config.EnableKlineStrategy = false

		message = "✅ 智能策略已启用\n\n智能功能:\n🧠 动态利率调整\n📈 市场趋势分析\n⏰ 智能期间选择\n🏆 竞争对手分析\n💰 自适应资金配置\n\nK线策略已自动停用\n下次执行时将使用智能策略"
	} else {
		message = "❌ 智能策略已停用\n\n已切换回其他策略:\n"
		if b.config.EnableKlineStrategy {
			message += "📈 K线策略 (已启用)\n"
		} else {
			message += "⚙️ 传统策略 (默认)\n"
		}
		message += "\n下次执行时将使用相应策略"
	}

	b.sendMessage(chatID, message)
}

// handleToggleKlineStrategy 处理K线策略切换指令
func (b *Bot) handleToggleKlineStrategy(chatID int64, enable bool) {
	b.config.EnableKlineStrategy = enable

	var message string
	if enable {
		// 如果启用K线策略，自动关闭智能策略
		b.config.EnableSmartStrategy = false

		message = "✅ K线策略已启用\n\n📈 K线策略功能:\n🎯 基于真实市场K线数据\n📊 自动找寻最高利率\n💡 智能加成计算\n🔄 分散风险贷出\n🛡️ 自动回退机制\n\n"
		message += fmt.Sprintf("⚙️ 当前设定:\n")
		message += fmt.Sprintf("时间框架: %s\n", b.config.KlineTimeFrame)
		message += fmt.Sprintf("K线周期: %d\n", b.config.KlinePeriod)
		message += fmt.Sprintf("加成百分比: %.1f%%\n", b.config.KlineSpreadPercent)
		message += "\n智能策略已自动停用\n下次执行时将使用K线策略"
	} else {
		message = "❌ K线策略已停用\n\n已切换回其他策略:\n"
		if b.config.EnableSmartStrategy {
			message += "🧠 智能策略 (已启用)\n"
		} else {
			message += "⚙️ 传统策略 (默认)\n"
		}
		message += "\n下次执行时将使用相应策略"
	}

	b.sendMessage(chatID, message)
}

// handleLendingCredits 处理借贷订单查看指令
func (b *Bot) handleLendingCredits(chatID int64) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借贷机器人未初始化")
		return
	}

	credits, err := b.lendingBot.GetActiveLendingCredits()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 获取借贷订单失败: %v", err))
		return
	}

	if len(credits) == 0 {
		b.sendMessage(chatID, "📭 目前没有活跃的借贷订单")
		return
	}

	message := "💰 当前活跃的借贷订单\n\n"

	frrFallbackRate := 0.0
	for _, credit := range credits {
		if credit.EffectiveDailyRate() == 0 {
			rate, err := b.bitfinexClient.GetCurrentFundingRate(b.config.GetFundingSymbol())
			if err != nil {
				break
			}
			frrFallbackRate = rate
			break
		}
	}

	// 先计算所有订单的统计信息
	totalAmount := 0.0
	totalDailyEarnings := 0.0
	totalPeriodEarnings := 0.0

	for _, credit := range credits {
		effectiveRate := credit.EffectiveDailyRate()
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		dailyEarnings := credit.Amount * effectiveRate
		periodEarnings := dailyEarnings * float64(credit.Period)

		totalAmount += credit.Amount
		totalDailyEarnings += dailyEarnings
		totalPeriodEarnings += periodEarnings
	}

	// 限制显示数量，避免消息过长
	displayCount := len(credits)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		credit := credits[i]

		// 计算收益
		rawRate := credit.EffectiveDailyRate()
		effectiveRate := rawRate
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		dailyEarnings := credit.Amount * effectiveRate
		periodEarnings := dailyEarnings * float64(credit.Period)

		// 格式化开始时间
		openTime := time.Unix(credit.MTSOpened/1000, 0)

		message += fmt.Sprintf("📊 订单 #%d (ID: %d)\n", i+1, credit.ID)
		message += fmt.Sprintf("💵 金额: %.2f %s\n", credit.Amount, b.config.Currency)
		message += fmt.Sprintf("📈 日利率: %.4f%%\n", b.rateConverter.DecimalToPercentage(effectiveRate))
		if strings.EqualFold(credit.RateType, "frr") || (rawRate == 0 && frrFallbackRate > 0) {
			message += "🔖 来源: FRR\n"
		}
		message += fmt.Sprintf("💰 日收益: %.4f %s\n", dailyEarnings, b.config.Currency)
		message += fmt.Sprintf("⏰ 期间: %d 天\n", credit.Period)
		message += fmt.Sprintf("💎 期间总收益: %.4f %s\n", periodEarnings, b.config.Currency)
		message += fmt.Sprintf("🕐 开始时间: %s\n", openTime.Format("2006-01-02 15:04:05"))
		message += fmt.Sprintf("📊 状态: %s\n", credit.Status)
		message += "\n"
	}

	if len(credits) > 10 {
		message += fmt.Sprintf("... 还有 %d 个订单未显示\n\n", len(credits)-10)
	}

	// 添加统计信息
	message += fmt.Sprintf("📊 统计信息:\n")
	message += fmt.Sprintf("📦 总订单数: %d\n", len(credits))
	message += fmt.Sprintf("💵 总借出金额: %.2f %s\n", totalAmount, b.config.Currency)
	message += fmt.Sprintf("💰 每日总收益: %.4f %s\n", totalDailyEarnings, b.config.Currency)

	if len(credits) <= 10 {
		message += fmt.Sprintf("💎 总期间收益: %.4f %s\n", totalPeriodEarnings, b.config.Currency)
	}

	// 计算年化收益率
	if totalAmount > 0 {
		annualRate := (totalDailyEarnings / totalAmount) * 365 * 100
		message += fmt.Sprintf("📈 年化收益率: %.2f%%", annualRate)
	}

	b.sendMessage(chatID, message)
}

// handleSetSmoothMethod 处理设置平滑方法指令
func (b *Bot) handleSetSmoothMethod(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式错误，请使用 /smoothmethod [方法] 格式\n\n可用方法:\nmax - 最高值 (激进)\nsma - 简单移动平均 (保守)\nema - 指数移动平均 (平滑敏感)\nhla - 高低点平均 (平衡)\np90 - 90百分位数 (避免极值)")
		return
	}

	method := strings.ToLower(parts[1])
	validMethods := map[string]string{
		"max": "最高值 (激进)",
		"sma": "简单移动平均 (保守)",
		"ema": "指数移动平均 (平滑敏感)",
		"hla": "高低点平均 (平衡)",
		"p90": "90百分位数 (避免极值)",
	}

	description, isValid := validMethods[method]
	if !isValid {
		b.sendMessage(chatID, "无效的平滑方法，可用方法:\nmax - 最高值 (激进)\nsma - 简单移动平均 (保守)\nema - 指数移动平均 (平滑敏感)\nhla - 高低点平均 (平衡)\np90 - 90百分位数 (避免极值)")
		return
	}

	b.config.KlineSmoothMethod = method
	b.sendMessage(chatID, fmt.Sprintf("✅ K线利率平滑方法已设定为: %s - %s\n\n下次执行K线策略时将使用新的平滑方法", method, description))
}

// getSmoothMethodDescription 获取平滑方法的描述
func getSmoothMethodDescription(method string) string {
	descriptions := map[string]string{
		"max": "最高值 (激进)",
		"sma": "简单移动平均 (保守)",
		"ema": "指数移动平均 (平滑敏感)",
		"hla": "高低点平均 (平衡)",
		"p90": "90百分位数 (避免极值)",
	}

	if desc, exists := descriptions[method]; exists {
		return desc
	}
	return "未知方法"
}
