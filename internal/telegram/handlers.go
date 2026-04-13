package telegram

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/constants"
)

// handleRate 處理利率查詢指令
func (b *Bot) handleRate(chatID int64) {
	rate, err := b.bitfinexClient.GetCurrentFundingRate(b.config.GetFundingSymbol())
	if err != nil {
		b.sendMessage(chatID, "取得貸出利率失敗")
		return
	}

	thresholdInfo := ""
	if b.config.NotifyRateThreshold > 0 {
		thresholdInfo = fmt.Sprintf("\n目前設定的閾值為: %.4f%%", b.config.NotifyRateThreshold)
	}

	message := fmt.Sprintf("目前貸出利率: %.4f%%%s",
		b.rateConverter.DecimalDailyToPercentageDaily(rate), thresholdInfo)
	b.sendMessage(chatID, message)
}

// handleCheck 處理利率檢查指令
func (b *Bot) handleCheck(chatID int64) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借貸機器人未初始化")
		return
	}

	// 使用新的K線基礎檢查方法
	exceeded, percentageRate, err := b.lendingBot.CheckRateThreshold()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 取得利率數據失敗: %v", err))
		return
	}

	replyMsg := fmt.Sprintf("📊 利率閾值檢查報告\n\n")
	replyMsg += fmt.Sprintf("🎯 檢查方式: 5分鐘K線最近12根高點\n")
	replyMsg += fmt.Sprintf("📈 最高利率: %.4f%%\n", percentageRate)

	replyMsg += fmt.Sprintf("🎚️ 設定閾值: %.4f%%\n\n", b.config.NotifyRateThreshold)

	if exceeded {
		replyMsg += "⚠️ 注意: 最近1小時最高利率已超過閾值!"
	} else {
		replyMsg += "✅ 最近1小時最高利率低於閾值"
	}

	b.sendMessage(chatID, replyMsg)
}

// handleStatus 處理狀態查詢指令
func (b *Bot) handleStatus(chatID int64) {
	// 獲取剩餘金額
	availableFunds, err := b.bitfinexClient.GetFundingBalance(strings.ToUpper(b.config.Currency))
	var balanceInfo string
	if err != nil {
		balanceInfo = fmt.Sprintf("剩餘金額: 獲取失敗 (%v)", err)
	} else {
		balanceInfo = fmt.Sprintf("💰 資金狀況:\n總餘額: %.2f %s",
			availableFunds, b.config.Currency)
	}

	statusMsg := fmt.Sprintf("📊 系統狀態報告\n\n%s\n\n💱 基本設定:\n幣種: %s\n最小貸出金額: %.2f\n最大貸出金額: %.2f",
		balanceInfo, b.config.Currency, b.config.MinLoan, b.config.MaxLoan)

	// 添加保留金額信息
	if b.config.ReserveAmount > 0 {
		statusMsg += fmt.Sprintf("\n保留金額: %.2f", b.config.ReserveAmount)
	} else {
		statusMsg += "\n保留金額: 未設置"
	}

	// 添加機器人運行參數
	statusMsg += fmt.Sprintf("\n\n⚙️ 機器人參數:")
	statusMsg += fmt.Sprintf("\n單次下單限制: %d", b.config.OrderLimit)
	if b.config.LoanDays > 0 {
		statusMsg += fmt.Sprintf("\n固定借貸天數: %d 天", b.config.LoanDays)
	} else {
		statusMsg += "\n固定借貸天數: 自動判斷"
	}
	if b.config.IsMinDailyLendRateFRR() {
		statusMsg += fmt.Sprintf("\n最低日利率: %s (FRR 掛單模式)", b.config.GetMinDailyRateDisplay())
	} else {
		statusMsg += fmt.Sprintf("\n最低日利率: %s", b.config.GetMinDailyRateDisplay())
	}
	statusMsg += fmt.Sprintf("\n執行間隔: %d 分鐘", b.config.MinutesRun)

	// 添加運行模式信息
	if b.config.TestMode {
		statusMsg += fmt.Sprintf("\n\n🧪 運行模式: 測試模式 (模擬交易)")
	} else {
		statusMsg += fmt.Sprintf("\n\n🚀 運行模式: 正式模式 (真實交易)")
	}

	// 添加高額持有策略信息
	statusMsg += fmt.Sprintf("\n\n💎 高額持有策略:")
	if b.config.HighHoldAmount > 0 {
		statusMsg += fmt.Sprintf("\n金額: %.2f %s", b.config.HighHoldAmount, b.config.Currency)
		statusMsg += fmt.Sprintf("\n日利率: %.4f%%", b.config.HighHoldRate)
		statusMsg += fmt.Sprintf("\n訂單數量: %d", b.config.HighHoldOrders)
	} else {
		statusMsg += "\n未啟用"
	}

	// 添加當前策略信息
	statusMsg += fmt.Sprintf("\n\n🎯 當前策略:")
	if b.config.EnableKlineStrategy {
		statusMsg += fmt.Sprintf("\nK線策略 (啟用)")
		statusMsg += fmt.Sprintf("\n時間框架: %s", b.config.KlineTimeFrame)
		statusMsg += fmt.Sprintf("\n週期數: %d", b.config.KlinePeriod)
		statusMsg += fmt.Sprintf("\n加成: %.1f%%", b.config.KlineSpreadPercent)
	} else if b.config.EnableSmartStrategy {
		statusMsg += fmt.Sprintf("\n智能策略 (啟用)")
		statusMsg += fmt.Sprintf("\n利率範圍增加: %.1f%%", b.config.RateRangeIncreasePercent*100)
	} else {
		statusMsg += fmt.Sprintf("\n傳統策略 (啟用)")
	}

	// 添加利率範圍增加百分比 (對所有策略都適用)
	if b.config.RateRangeIncreasePercent > 0 {
		statusMsg += fmt.Sprintf("\n📊 利率範圍增加: %.1f%%", b.config.RateRangeIncreasePercent*100)
	}

	statusMsg += fmt.Sprintf("\n\n💡 使用 /strategy 查看詳細策略狀態")

	b.sendMessage(chatID, statusMsg)
}

// handleSetThreshold 處理設置閾值指令
func (b *Bot) handleSetThreshold(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /threshold [數值] 格式")
		return
	}

	threshold, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || threshold <= 0 {
		b.sendMessage(chatID, "請輸入有效的正數值")
		return
	}

	b.config.NotifyRateThreshold = threshold
	b.sendMessage(chatID, fmt.Sprintf("閾值已設定為: %.4f%%", threshold))
}

// handleSetReserve 處理設置保留金額指令
func (b *Bot) handleSetReserve(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /reserve [數值] 格式")
		return
	}

	reserve, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || reserve < 0 {
		b.sendMessage(chatID, "請輸入有效的非負數值")
		return
	}

	b.config.ReserveAmount = reserve
	b.sendMessage(chatID, fmt.Sprintf("保留金額已設定為: %.2f", reserve))
}

// handleSetOrderLimit 處理設置訂單限制指令
func (b *Bot) handleSetOrderLimit(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /orderlimit [數值] 格式")
		return
	}

	limit, err := strconv.Atoi(parts[1])
	if err != nil || limit < 0 {
		b.sendMessage(chatID, "請輸入有效的非負整數")
		return
	}

	b.config.OrderLimit = limit
	b.sendMessage(chatID, fmt.Sprintf("單次執行最大下單數量限制已設定為: %d", limit))
}

// handleSetLoanDays 處理設置固定借貸天數指令
func (b *Bot) handleSetLoanDays(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /loandays [數值] 格式\n提示: 設置為 0 表示自動判斷")
		return
	}

	days, err := strconv.Atoi(parts[1])
	if err != nil || days < 0 {
		b.sendMessage(chatID, "請輸入有效的非負整數\n提示: 設置為 0 表示自動判斷")
		return
	}

	if days == 1 || days > constants.Period120Days {
		b.sendMessage(chatID, "借貸天數必須是 0，或介於 2 到 120 之間的整數")
		return
	}

	b.config.LoanDays = days
	if days == 0 {
		b.sendMessage(chatID, "固定借貸天數已設為: 自動判斷")
		return
	}

	b.sendMessage(chatID, fmt.Sprintf("固定借貸天數已設定為: %d 天", days))
}

// handleSetMinDailyRate 處理設置最低日利率指令
func (b *Bot) handleSetMinDailyRate(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /mindailylendrate [數值|FRR] 格式")
		return
	}

	input := strings.TrimSpace(parts[1])
	if strings.EqualFold(input, constants.MinDailyRateModeFRR) {
		b.config.MinDailyLendRate = constants.MinDailyRateModeFRR
		b.sendMessage(chatID, "最低每日貸出利率已設定為: FRR（將使用 FRR 模式掛單）")
		return
	}

	rate, err := strconv.ParseFloat(input, 64)
	if err != nil || rate <= 0 {
		b.sendMessage(chatID, "請輸入有效的正數值")
		return
	}

	if !b.rateConverter.ValidatePercentageRate(rate) {
		b.sendMessage(chatID, "利率超出有效範圍 (0-7%)")
		return
	}

	b.config.MinDailyLendRate = rate
	b.sendMessage(chatID, fmt.Sprintf("最低每日貸出利率已設定為: %.4f%%", rate))
}

// handleSetMinLoan 處理設置最小貸出金額指令
func (b *Bot) handleSetMinLoan(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /minloan [數值] 格式")
		return
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || amount <= 0 {
		b.sendMessage(chatID, "請輸入有效的正數值")
		return
	}

	// 檢查是否小於等於最大貸出金額
	if b.config.MaxLoan > 0 && amount > b.config.MaxLoan {
		b.sendMessage(chatID, fmt.Sprintf("最小貸出金額不能大於最大貸出金額 (%.2f %s)", b.config.MaxLoan, b.config.Currency))
		return
	}

	b.config.MinLoan = amount
	b.sendMessage(chatID, fmt.Sprintf("✅ 最小貸出金額已設定為: %.2f %s", amount, b.config.Currency))
}

// handleSetMaxLoan 處理設置最大貸出金額指令
func (b *Bot) handleSetMaxLoan(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /maxloan [數值] 格式\n提示: 設置為 0 表示無限制")
		return
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || amount < 0 {
		b.sendMessage(chatID, "請輸入有效的非負數值\n提示: 設置為 0 表示無限制")
		return
	}

	// 檢查是否大於等於最小貸出金額
	if amount > 0 && amount < b.config.MinLoan {
		b.sendMessage(chatID, fmt.Sprintf("最大貸出金額不能小於最小貸出金額 (%.2f %s)", b.config.MinLoan, b.config.Currency))
		return
	}

	b.config.MaxLoan = amount

	if amount == 0 {
		b.sendMessage(chatID, "✅ 最大貸出金額已設定為: 無限制")
	} else {
		b.sendMessage(chatID, fmt.Sprintf("✅ 最大貸出金額已設定為: %.2f %s", amount, b.config.Currency))
	}
}

// handleSetHighHoldRate 處理設置高額持有利率指令
func (b *Bot) handleSetHighHoldRate(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /highholdrate [數值] 格式")
		return
	}

	rate, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || rate <= 0 {
		b.sendMessage(chatID, "請輸入有效的正數值")
		return
	}

	if !b.rateConverter.ValidatePercentageRate(rate) {
		b.sendMessage(chatID, "利率超出有效範圍 (0-7%)")
		return
	}

	b.config.HighHoldRate = rate
	b.sendMessage(chatID, fmt.Sprintf("高額持有策略的日利率已設定為: %.4f%%", rate))
}

// handleSetHighHoldAmount 處理設置高額持有金額指令
func (b *Bot) handleSetHighHoldAmount(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /highholdamount [數值] 格式\n提示: 設置為 0 可關閉高額持有策略")
		return
	}

	amount, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || amount < 0 {
		b.sendMessage(chatID, "請輸入有效的非負數值\n提示: 設置為 0 可關閉高額持有策略")
		return
	}

	b.config.HighHoldAmount = amount

	if amount == 0 {
		b.sendMessage(chatID, "✅ 高額持有策略已關閉\n高額持有金額已設定為: 0.00")
	} else {
		b.sendMessage(chatID, fmt.Sprintf("✅ 高額持有策略已啟用\n高額持有金額已設定為: %.2f %s", amount, b.config.Currency))
	}
}

// handleSetHighHoldOrders 處理設置高額持有訂單數量指令
func (b *Bot) handleSetHighHoldOrders(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /highholdorders [數值] 格式")
		return
	}

	orders, err := strconv.Atoi(parts[1])
	if err != nil || orders < 1 {
		b.sendMessage(chatID, "請輸入有效的正整數")
		return
	}

	b.config.HighHoldOrders = orders
	b.sendMessage(chatID, fmt.Sprintf("高額持有訂單數量已設定為: %d", orders))
}

// handleSetRateRangeIncrease 處理設置利率範圍增加百分比指令
func (b *Bot) handleSetRateRangeIncrease(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /raterangeincrease [數值] 格式")
		return
	}

	percentage, err := strconv.ParseFloat(parts[1], 64)
	if err != nil || percentage <= 0 {
		b.sendMessage(chatID, "請輸入有效的正數值")
		return
	}

	// 驗證範圍 (0-100%)
	if percentage > 100.0 {
		b.sendMessage(chatID, "利率範圍增加百分比不能超過 100%")
		return
	}

	// 轉換為小數形式 (0-1.0)
	decimalValue := percentage / 100.0

	b.config.RateRangeIncreasePercent = decimalValue
	b.sendMessage(chatID, fmt.Sprintf("利率範圍增加百分比已設定為: %.2f%% (%.4f)", percentage, decimalValue))
}

// handleRestart 處理重啟指令
func (b *Bot) handleRestart(chatID int64) {
	b.sendMessage(chatID, "🔄 開始手動重啟...")

	if b.restartCallback == nil {
		b.sendMessage(chatID, "❌ 重啟功能未初始化，請聯繫管理員")
		return
	}

	// 執行重啟邏輯
	err := b.restartCallback()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 重啟失敗: %v", err))
		return
	}

	b.sendMessage(chatID, "✅ 重啟完成！所有訂單已清除並重新下單")
}

// handleStrategyStatus 處理策略狀態查詢指令
func (b *Bot) handleStrategyStatus(chatID int64) {
	var strategyType string
	var strategyPriority string

	// 根據策略優先級確定當前啟用的策略
	if b.config.EnableKlineStrategy {
		strategyType = "K線策略 (啟用)"
		strategyPriority = "最高優先級"
	} else if b.config.EnableSmartStrategy {
		strategyType = "智能策略 (啟用)"
		strategyPriority = "中等優先級"
	} else {
		strategyType = "傳統策略 (啟用)"
		strategyPriority = "預設策略"
	}

	statusMsg := fmt.Sprintf("📊 當前策略狀態\n策略類型: %s\n優先級: %s", strategyType, strategyPriority)

	// K線策略設定
	if b.config.EnableKlineStrategy {
		statusMsg += fmt.Sprintf("\n\n📈 K線策略設定:")
		statusMsg += fmt.Sprintf("\n時間框架: %s", b.config.KlineTimeFrame)
		statusMsg += fmt.Sprintf("\nK線週期數: %d", b.config.KlinePeriod)
		statusMsg += fmt.Sprintf("\n加成百分比: %.1f%%", b.config.KlineSpreadPercent)

		// 添加平滑方法信息
		smoothMethodDesc := getSmoothMethodDescription(b.config.KlineSmoothMethod)
		statusMsg += fmt.Sprintf("\n利率平滑方法: %s - %s", b.config.KlineSmoothMethod, smoothMethodDesc)

		// 計算分析時間範圍
		var timeRange string
		switch b.config.KlineTimeFrame {
		case "5m":
			minutes := float64(b.config.KlinePeriod) * 5
			timeRange = fmt.Sprintf("%.1f分鐘", minutes)
		case "15m":
			hours := float64(b.config.KlinePeriod) * 0.25
			timeRange = fmt.Sprintf("%.1f小時", hours)
		case "30m":
			hours := float64(b.config.KlinePeriod) * 0.5
			timeRange = fmt.Sprintf("%.1f小時", hours)
		case "1h":
			timeRange = fmt.Sprintf("%d小時", b.config.KlinePeriod)
		case "3h":
			hours := b.config.KlinePeriod * 3
			timeRange = fmt.Sprintf("%d小時", hours)
		case "6h":
			hours := b.config.KlinePeriod * 6
			timeRange = fmt.Sprintf("%d小時", hours)
		case "12h":
			days := float64(b.config.KlinePeriod) * 0.5
			timeRange = fmt.Sprintf("%.1f天", days)
		case "1D":
			timeRange = fmt.Sprintf("%d天", b.config.KlinePeriod)
		default:
			timeRange = "未知"
		}
		statusMsg += fmt.Sprintf("\n分析時間範圍: %s", timeRange)

		statusMsg += fmt.Sprintf("\n\nK線策略功能:")
		statusMsg += fmt.Sprintf("\n✅ 基於真實市場K線數據")
		statusMsg += fmt.Sprintf("\n✅ 自動找尋最高利率")
		statusMsg += fmt.Sprintf("\n✅ 智能加成計算")
		statusMsg += fmt.Sprintf("\n✅ 分散風險貸出")
		statusMsg += fmt.Sprintf("\n✅ 自動回退機制")

		// 添加策略建議
		statusMsg += fmt.Sprintf("\n\n📋 時間框架建議:")
		statusMsg += fmt.Sprintf("\n⚡ 短期: 15m-30m (快速反應)")
		statusMsg += fmt.Sprintf("\n⚖️ 中期: 1h-3h (平衡策略)")
		statusMsg += fmt.Sprintf("\n🛡️ 長期: 6h-1D (穩定策略)")
	} else if b.config.EnableSmartStrategy {
		statusMsg += fmt.Sprintf("\n\n🧠 智能策略設定:")
		statusMsg += fmt.Sprintf("\n波動率閾值: %.4f", b.config.VolatilityThreshold)
		statusMsg += fmt.Sprintf("\n最大利率倍數: %.1fx", b.config.MaxRateMultiplier)
		statusMsg += fmt.Sprintf("\n最小利率倍數: %.1fx", b.config.MinRateMultiplier)
		statusMsg += fmt.Sprintf("\n利率範圍增加: %.1f%%", b.config.RateRangeIncreasePercent*100)

		// 添加建議值提示
		statusMsg += fmt.Sprintf("\n\n📋 參數建議值:")
		statusMsg += fmt.Sprintf("\n🛡️ 保守: 波動率 0.001, 最大倍數 1.5x, 最小倍數 0.9x")
		statusMsg += fmt.Sprintf("\n⚖️ 平衡: 波動率 0.002, 最大倍數 2.0x, 最小倍數 0.8x")
		statusMsg += fmt.Sprintf("\n⚡ 激進: 波動率 0.003, 最大倍數 3.0x, 最小倍數 0.7x")

		statusMsg += fmt.Sprintf("\n\n智能功能:")
		statusMsg += fmt.Sprintf("\n✅ 動態利率調整")
		statusMsg += fmt.Sprintf("\n✅ 市場趨勢分析")
		statusMsg += fmt.Sprintf("\n✅ 智能期間選擇")
		statusMsg += fmt.Sprintf("\n✅ 競爭對手分析")
		statusMsg += fmt.Sprintf("\n✅ 自適應資金配置")
	} else {
		statusMsg += fmt.Sprintf("\n\n⚙️ 傳統策略設定:")
		statusMsg += fmt.Sprintf("\n固定高額持有利率: %.4f%%", b.config.HighHoldRate)
		statusMsg += fmt.Sprintf("\n固定分散貸出參數")
		statusMsg += fmt.Sprintf("\n固定期間選擇邏輯")
	}

	// 顯示策略優先級順序
	statusMsg += fmt.Sprintf("\n\n🔄 策略優先級順序:")
	statusMsg += fmt.Sprintf("\n1️⃣ K線策略 (%s)", getStrategyStatus(b.config.EnableKlineStrategy))
	statusMsg += fmt.Sprintf("\n2️⃣ 智能策略 (%s)", getStrategyStatus(b.config.EnableSmartStrategy))
	statusMsg += fmt.Sprintf("\n3️⃣ 傳統策略 (預設)")

	statusMsg += fmt.Sprintf("\n\n💡 提示: 使用指令切換策略")
	statusMsg += fmt.Sprintf("\n/klinestrategy on/off - 切換K線策略")
	statusMsg += fmt.Sprintf("\n/smartstrategy on/off - 切換智能策略")

	b.sendMessage(chatID, statusMsg)
}

// getStrategyStatus 獲取策略狀態文字
func getStrategyStatus(enabled bool) string {
	if enabled {
		return "啟用"
	}
	return "停用"
}

// handleToggleSmartStrategy 處理智能策略切換指令
func (b *Bot) handleToggleSmartStrategy(chatID int64, enable bool) {
	b.config.EnableSmartStrategy = enable

	var message string
	if enable {
		// 如果啟用智能策略，自動關閉K線策略
		b.config.EnableKlineStrategy = false

		message = "✅ 智能策略已啟用\n\n智能功能:\n🧠 動態利率調整\n📈 市場趨勢分析\n⏰ 智能期間選擇\n🏆 競爭對手分析\n💰 自適應資金配置\n\nK線策略已自動停用\n下次執行時將使用智能策略"
	} else {
		message = "❌ 智能策略已停用\n\n已切換回其他策略:\n"
		if b.config.EnableKlineStrategy {
			message += "📈 K線策略 (已啟用)\n"
		} else {
			message += "⚙️ 傳統策略 (預設)\n"
		}
		message += "\n下次執行時將使用相應策略"
	}

	b.sendMessage(chatID, message)
}

// handleToggleKlineStrategy 處理K線策略切換指令
func (b *Bot) handleToggleKlineStrategy(chatID int64, enable bool) {
	b.config.EnableKlineStrategy = enable

	var message string
	if enable {
		// 如果啟用K線策略，自動關閉智能策略
		b.config.EnableSmartStrategy = false

		message = "✅ K線策略已啟用\n\n📈 K線策略功能:\n🎯 基於真實市場K線數據\n📊 自動找尋最高利率\n💡 智能加成計算\n🔄 分散風險貸出\n🛡️ 自動回退機制\n\n"
		message += fmt.Sprintf("⚙️ 當前設定:\n")
		message += fmt.Sprintf("時間框架: %s\n", b.config.KlineTimeFrame)
		message += fmt.Sprintf("K線週期: %d\n", b.config.KlinePeriod)
		message += fmt.Sprintf("加成百分比: %.1f%%\n", b.config.KlineSpreadPercent)
		message += "\n智能策略已自動停用\n下次執行時將使用K線策略"
	} else {
		message = "❌ K線策略已停用\n\n已切換回其他策略:\n"
		if b.config.EnableSmartStrategy {
			message += "🧠 智能策略 (已啟用)\n"
		} else {
			message += "⚙️ 傳統策略 (預設)\n"
		}
		message += "\n下次執行時將使用相應策略"
	}

	b.sendMessage(chatID, message)
}

// handleLendingCredits 處理借貸訂單查看指令
func (b *Bot) handleLendingCredits(chatID int64) {
	if b.lendingBot == nil {
		b.sendMessage(chatID, "❌ 借貸機器人未初始化")
		return
	}

	credits, err := b.lendingBot.GetActiveLendingCredits()
	if err != nil {
		b.sendMessage(chatID, fmt.Sprintf("❌ 獲取借貸訂單失敗: %v", err))
		return
	}

	if len(credits) == 0 {
		b.sendMessage(chatID, "📭 目前沒有活躍的借貸訂單")
		return
	}

	message := "💰 當前活躍的借貸訂單\n\n"

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

	// 先計算所有訂單的統計信息
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

	// 限制顯示數量，避免消息過長
	displayCount := len(credits)
	if displayCount > 10 {
		displayCount = 10
	}

	for i := 0; i < displayCount; i++ {
		credit := credits[i]

		// 計算收益
		rawRate := credit.EffectiveDailyRate()
		effectiveRate := rawRate
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		dailyEarnings := credit.Amount * effectiveRate
		periodEarnings := dailyEarnings * float64(credit.Period)

		// 格式化開始時間
		openTime := time.Unix(credit.MTSOpened/1000, 0)

		message += fmt.Sprintf("📊 訂單 #%d (ID: %d)\n", i+1, credit.ID)
		message += fmt.Sprintf("💵 金額: %.2f %s\n", credit.Amount, b.config.Currency)
		message += fmt.Sprintf("📈 日利率: %.4f%%\n", b.rateConverter.DecimalToPercentage(effectiveRate))
		if strings.EqualFold(credit.RateType, "frr") || (rawRate == 0 && frrFallbackRate > 0) {
			message += "🔖 來源: FRR\n"
		}
		message += fmt.Sprintf("💰 日收益: %.4f %s\n", dailyEarnings, b.config.Currency)
		message += fmt.Sprintf("⏰ 期間: %d 天\n", credit.Period)
		message += fmt.Sprintf("💎 期間總收益: %.4f %s\n", periodEarnings, b.config.Currency)
		message += fmt.Sprintf("🕐 開始時間: %s\n", openTime.Format("2006-01-02 15:04:05"))
		message += fmt.Sprintf("📊 狀態: %s\n", credit.Status)
		message += "\n"
	}

	if len(credits) > 10 {
		message += fmt.Sprintf("... 還有 %d 個訂單未顯示\n\n", len(credits)-10)
	}

	// 添加統計信息
	message += fmt.Sprintf("📊 統計信息:\n")
	message += fmt.Sprintf("📦 總訂單數: %d\n", len(credits))
	message += fmt.Sprintf("💵 總借出金額: %.2f %s\n", totalAmount, b.config.Currency)
	message += fmt.Sprintf("💰 每日總收益: %.4f %s\n", totalDailyEarnings, b.config.Currency)

	if len(credits) <= 10 {
		message += fmt.Sprintf("💎 總期間收益: %.4f %s\n", totalPeriodEarnings, b.config.Currency)
	}

	// 計算年化收益率
	if totalAmount > 0 {
		annualRate := (totalDailyEarnings / totalAmount) * 365 * 100
		message += fmt.Sprintf("📈 年化收益率: %.2f%%", annualRate)
	}

	b.sendMessage(chatID, message)
}

// handleSetSmoothMethod 處理設置平滑方法指令
func (b *Bot) handleSetSmoothMethod(chatID int64, text string) {
	parts := strings.Split(text, " ")
	if len(parts) != 2 {
		b.sendMessage(chatID, "格式錯誤，請使用 /smoothmethod [方法] 格式\n\n可用方法:\nmax - 最高值 (激進)\nsma - 簡單移動平均 (保守)\nema - 指數移動平均 (平滑敏感)\nhla - 高低點平均 (平衡)\np90 - 90百分位數 (避免極值)")
		return
	}

	method := strings.ToLower(parts[1])
	validMethods := map[string]string{
		"max": "最高值 (激進)",
		"sma": "簡單移動平均 (保守)",
		"ema": "指數移動平均 (平滑敏感)",
		"hla": "高低點平均 (平衡)",
		"p90": "90百分位數 (避免極值)",
	}

	description, isValid := validMethods[method]
	if !isValid {
		b.sendMessage(chatID, "無效的平滑方法，可用方法:\nmax - 最高值 (激進)\nsma - 簡單移動平均 (保守)\nema - 指數移動平均 (平滑敏感)\nhla - 高低點平均 (平衡)\np90 - 90百分位數 (避免極值)")
		return
	}

	b.config.KlineSmoothMethod = method
	b.sendMessage(chatID, fmt.Sprintf("✅ K線利率平滑方法已設定為: %s - %s\n\n下次執行K線策略時將使用新的平滑方法", method, description))
}

// getSmoothMethodDescription 獲取平滑方法的描述
func getSmoothMethodDescription(method string) string {
	descriptions := map[string]string{
		"max": "最高值 (激進)",
		"sma": "簡單移動平均 (保守)",
		"ema": "指數移動平均 (平滑敏感)",
		"hla": "高低點平均 (平衡)",
		"p90": "90百分位數 (避免極值)",
	}

	if desc, exists := descriptions[method]; exists {
		return desc
	}
	return "未知方法"
}
