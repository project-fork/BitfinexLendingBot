package strategy

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/kfrico/BitfinexLendingBot/internal/bitfinex"
	"github.com/kfrico/BitfinexLendingBot/internal/config"
	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/rates"
	"github.com/kfrico/BitfinexLendingBot/internal/tracker"
)

// LendingBot 貸出機器人
type LendingBot struct {
	config         *config.Config
	client         *bitfinex.Client
	rateConverter  *rates.Converter
	smartStrategy  *SmartStrategy
	orderTracker   *tracker.BotOrderTracker
	notifyCallback func(string) error // Telegram 通知回調函數
}

// NewLendingBot 創建新的貸出機器人
func NewLendingBot(cfg *config.Config, client *bitfinex.Client) *LendingBot {
	return &LendingBot{
		config:        cfg,
		client:        client,
		rateConverter: rates.NewConverter(),
		orderTracker:  tracker.NewBotOrderTracker(),
		smartStrategy: NewSmartStrategy(cfg),
	}
}

// LoanOffer 代表一個貸出訂單
type LoanOffer struct {
	Amount float64
	Rate   float64 // 日利率（小數格式）
	Period int
	UseFRR bool // 是否使用 FRR 掛單模式
}

// Execute 執行機器人主要邏輯
func (lb *LendingBot) Execute() error {
	log.Println("開始執行貸出機器人...")

	// 清理舊的訂單記錄（避免記憶體洩漏）
	lb.orderTracker.CleanOldOrders(24 * time.Hour)

	// 取消程式創建的未完成訂單
	log.Println("取消程式創建的未完成訂單...")
	hasPendingOrders, err := lb.cancelAllOffers()
	if err != nil {
		log.Printf("取消訂單失敗: %v", err)
		return err
	}

	// 等待訂單取消完成
	time.Sleep(constants.RetryDelay)

	// 獲取可用資金
	log.Println("取得可用額度...")
	fundsAvailable, err := lb.getAvailableFunds()
	if err != nil {
		log.Printf("取得餘額錯誤: %v", err)
		return err
	}
	log.Printf("Currency: %s  Available: %f", lb.config.Currency, fundsAvailable)

	// 扣除保留金額
	if lb.config.ReserveAmount > 0 {
		fundsAvailable = math.Max(0, fundsAvailable-lb.config.ReserveAmount)
		log.Printf("扣除保留金額後可用: %f", fundsAvailable)
	}

	// 檢查可用資金
	if fundsAvailable < lb.config.MinLoan {
		log.Println("可用資金小於最小貸出額，不進行操作")
		return nil
	}

	// 獲取市場數據
	fundingBook, err := lb.client.GetFundingBook(lb.config.GetFundingSymbol(), constants.MaxPriceLevels)
	if err != nil {
		log.Printf("取得 Funding Book 錯誤: %v", err)
		log.Println("使用fallback模式，僅使用最小利率策略")
		// 使用空的funding book，策略會自動使用最小利率
		fundingBook = []*bitfinex.FundingBookEntry{}
	}

	// 根據配置選擇策略
	var loanOffers []*LoanOffer
	if lb.config.EnableKlineStrategy {
		log.Println("使用K線策略計算貸出訂單...")
		loanOffers = lb.calculateKlineOffers(fundsAvailable)
	} else if lb.config.EnableSmartStrategy {
		log.Println("使用智能策略計算貸出訂單...")
		loanOffers = lb.smartStrategy.CalculateSmartOffers(fundsAvailable, fundingBook)
	} else {
		log.Println("使用傳統策略計算貸出訂單...")
		loanOffers = lb.calculateLoanOffers(fundsAvailable, fundingBook)
	}

	// 下單
	return lb.placeLoanOffers(loanOffers, hasPendingOrders)
}

// cancelAllOffers 取消程式創建的未完成訂單
func (lb *LendingBot) cancelAllOffers() (bool, error) {
	offers, err := lb.client.GetFundingOffers(lb.config.GetFundingSymbol())
	if err != nil {
		return false, err
	}

	if len(offers) == 0 {
		log.Println("目前沒有未完成的訂單")
		return false, nil
	}

	cancelledCount := 0
	for _, offer := range offers {
		// 只取消程式追蹤的訂單
		if !lb.orderTracker.IsTrackedOrder(offer.ID) {
			log.Printf("跳過手動創建的訂單 ID: %d", offer.ID)
			continue
		}

		if err := lb.client.CancelFundingOffer(offer.ID); err != nil {
			log.Printf("取消程式訂單失敗: %v", err)
		} else {
			log.Printf("成功取消程式訂單 ID: %d", offer.ID)
			lb.orderTracker.RemoveOrder(offer.ID) // 從追蹤中移除
			cancelledCount++
		}
	}

	if cancelledCount == 0 {
		log.Println("沒有程式創建的訂單需要取消")
	}

	return cancelledCount > 0, nil
}

// getAvailableFunds 獲取可用資金
func (lb *LendingBot) getAvailableFunds() (float64, error) {
	return lb.client.GetFundingBalance(strings.ToUpper(lb.config.Currency))
}

// calculateLoanOffers 計算貸出訂單
func (lb *LendingBot) calculateLoanOffers(fundsAvailable float64, fundingBook []*bitfinex.FundingBookEntry) []*LoanOffer {
	var loanOffers []*LoanOffer

	// 檢查可用資金
	if fundsAvailable < lb.config.MinLoan {
		return loanOffers
	}

	splitFundsAvailable := fundsAvailable

	// 高額持有策略
	if lb.config.HighHoldAmount > lb.config.MinLoan {
		highHoldOffers := lb.calculateHighHoldOffers(&splitFundsAvailable)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	// 分散貸出策略
	if splitFundsAvailable >= lb.config.MinLoan {
		remainingSlots := lb.getRemainingOrderSlots(len(loanOffers))
		if remainingSlots != 0 {
			spreadOffers := lb.calculateSpreadOffers(splitFundsAvailable, fundingBook, remainingSlots)
			loanOffers = append(loanOffers, spreadOffers...)
		}
	}

	return loanOffers
}

// calculateHighHoldOffers 計算高額持有訂單
func (lb *LendingBot) calculateHighHoldOffers(splitFundsAvailable *float64) []*LoanOffer {
	var offers []*LoanOffer

	ordersCount := lb.config.HighHoldOrders
	if ordersCount <= 0 {
		ordersCount = 1
	}

	highHold := lb.config.HighHoldAmount
	if lb.config.MaxLoan > 0 && highHold > lb.config.MaxLoan {
		highHold = lb.config.MaxLoan
	}
	highHold = floorToCents(highHold)

	possibleOrders := int(*splitFundsAvailable / highHold)
	actualOrders := int(math.Min(float64(ordersCount), float64(possibleOrders)))

	for i := 0; i < actualOrders; i++ {
		if *splitFundsAvailable < highHold {
			break
		}

		offer := &LoanOffer{
			Amount: highHold,
			Rate:   lb.config.GetHighHoldRateDecimal(),
			Period: lb.config.GetLoanPeriod(constants.Period120Days),
			UseFRR: false, // 高額持有單固定走一般利率單
		}
		offers = append(offers, offer)
		*splitFundsAvailable -= highHold
	}

	return offers
}

// calculateSpreadOffers 計算分散貸出訂單
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

	// 計算利率遞增量
	gapClimb := (lb.config.GapTop - lb.config.GapBottom) / float64(len(orderAmounts))
	nextLend := lb.config.GapBottom

	depthIndex := 0
	minDailyRate := lb.config.GetMinDailyRateDecimal()

	for _, allocAmount := range orderAmounts {
		// 累計市場量至指定利率區間（僅在有funding book數據時）
		if len(fundingBook) > 0 {
			for float64(depthIndex) < nextLend && depthIndex < len(fundingBook)-1 {
				depthIndex++
			}
		}

		if allocAmount < lb.config.MinLoan {
			break
		}

		// 計算利率
		var rate float64
		if len(fundingBook) > 0 && depthIndex < len(fundingBook) {
			marketRate := fundingBook[depthIndex].Rate
			if marketRate < minDailyRate {
				rate = minDailyRate
			} else {
				rate = marketRate
			}
		} else {
			// 無funding book數據時使用最小利率
			rate = minDailyRate
		}

		// 計算期間
		period := lb.calculatePeriod(rate)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR, // 分散單依 MIN_DAILY_LEND_RATE 是否為 FRR 決定
		}
		offers = append(offers, offer)

		nextLend += gapClimb
	}

	return offers
}

// calculatePeriod 根據利率計算貸出期間
func (lb *LendingBot) calculatePeriod(dailyRate float64) int {
	if lb.config.LoanDays > 0 {
		return lb.config.LoanDays
	}

	oneTwentyThreshold := lb.config.GetOneTwentyDayThresholdDecimal()
	ninetyThreshold := lb.config.GetNinetyDayThresholdDecimal()
	sixtyThreshold := lb.config.GetSixtyDayThresholdDecimal()
	thirtyThreshold := lb.config.GetThirtyDayThresholdDecimal()

	if lb.config.OneTwentyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, oneTwentyThreshold) {
		return constants.Period120Days
	} else if lb.config.NinetyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, ninetyThreshold) {
		return constants.Period90Days
	} else if lb.config.SixtyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, sixtyThreshold) {
		return constants.Period60Days
	} else if lb.config.ThirtyDayLendRateThreshold > 0 && rateMeetsThreshold(dailyRate, thirtyThreshold) {
		return constants.Period30Days
	} else {
		return constants.DefaultPeriodDays
	}
}

func rateMeetsThreshold(rate float64, threshold float64) bool {
	const tolerance = 1e-12
	return rate+tolerance >= threshold
}

// placeLoanOffers 下單
func (lb *LendingBot) placeLoanOffers(loanOffers []*LoanOffer, hasPendingOrders bool) error {
	orderCount := 0
	fundingSymbol := lb.config.GetFundingSymbol()
	if lb.config.IsMinDailyLendRateFRR() {
		log.Printf("MIN_DAILY_LEND_RATE=%s，分散單使用 FRR 模式；高額持有單維持固定利率", constants.MinDailyRateModeFRR)
	}

	for _, offer := range loanOffers {
		if lb.config.OrderLimit != 0 && orderCount >= lb.config.OrderLimit {
			break
		}

		offer.Amount = floorToCents(offer.Amount)
		if offer.Amount < lb.config.MinLoan {
			log.Printf("跳過無效金額: %.4f", offer.Amount)
			continue
		}

		if offer.UseFRR {
			frrPeriod := lb.config.GetLoanPeriod(constants.Period120Days)

			if lb.config.TestMode {
				log.Printf("🧪 [測試模式] 模擬下單 => Type: %s, Amount: %.4f, Period: %d (參考Rate: %.6f%%)",
					constants.OfferTypeFRRDeltaVar,
					offer.Amount,
					frrPeriod,
					lb.rateConverter.DecimalToPercentage(offer.Rate),
				)
				orderCount++
			} else {
				log.Printf("下單 => Type: %s, Amount: %.4f, Period: %d (參考Rate: %.6f%%)",
					constants.OfferTypeFRRDeltaVar,
					offer.Amount,
					frrPeriod,
					lb.rateConverter.DecimalToPercentage(offer.Rate),
				)

				orderID, err := lb.client.SubmitFundingOfferFRR(fundingSymbol, offer.Amount, frrPeriod, false)
				if err != nil {
					log.Printf("下訂單失敗: %v", err)
				} else {
					// 追蹤程式創建的訂單
					lb.orderTracker.TrackOrder(orderID)
					log.Printf("成功創建訂單 ID: %d，已加入追蹤", orderID)
					orderCount++
				}
			}

			continue
		}

		rate := offer.Rate
		if !hasPendingOrders {
			// 添加利率加成
			rate += lb.rateConverter.PercentageToDecimal(lb.config.RateBonus)
		}

		// 驗證利率
		if !lb.rateConverter.ValidateDailyRate(rate) {
			log.Printf("跳過無效利率: %.6f", rate)
			continue
		}

		if lb.config.TestMode {
			// 測試模式：只記錄不真的下單
			log.Printf("🧪 [測試模式] 模擬下單 => Rate: %.6f%%, Amount: %.4f, Period: %d",
				lb.rateConverter.DecimalToPercentage(rate), offer.Amount, offer.Period)
			orderCount++
		} else {
			// 正式模式：真的下單
			log.Printf("下單 => Rate: %.6f%%, Amount: %.4f, Period: %d",
				lb.rateConverter.DecimalToPercentage(rate), offer.Amount, offer.Period)

			orderID, err := lb.client.SubmitFundingOffer(fundingSymbol, offer.Amount, rate, offer.Period, false)
			if err != nil {
				log.Printf("下訂單失敗: %v", err)
			} else {
				// 追蹤程式創建的訂單
				lb.orderTracker.TrackOrder(orderID)
				log.Printf("成功創建訂單 ID: %d，已加入追蹤", orderID)
				orderCount++
			}
		}
	}

	return nil
}

// CheckRateThreshold 檢查利率是否超過閾值（基於5分鐘K線最近12根高點）
func (lb *LendingBot) CheckRateThreshold() (bool, float64, error) {
	// 獲取5分鐘K線數據（12根，相當於1小時）
	candles, err := lb.client.GetFundingCandles(
		lb.config.GetFundingSymbol(),
		"5m",
		12,
	)
	if err != nil {
		return false, 0, err
	}

	// 找到最近12根K線中的最高利率
	highestRate := lb.findMaxRate(candles)
	percentageRate := lb.rateConverter.DecimalDailyToPercentageDaily(highestRate)
	exceeded := percentageRate > lb.config.NotifyRateThreshold

	log.Printf("K線閾值檢查 - 最近12根5分鐘K線最高利率: %.4f%%, 閾值: %.4f%%, 超過: %v",
		percentageRate, lb.config.NotifyRateThreshold, exceeded)

	return exceeded, percentageRate, nil
}

// SetNotifyCallback 設置 Telegram 通知回調函數
func (lb *LendingBot) SetNotifyCallback(callback func(string) error) {
	lb.notifyCallback = callback
}

// CheckNewLendingCredits 檢查新的借貸訂單和餘額變化，決定是否需要重新執行策略
func (lb *LendingBot) CheckNewLendingCredits() (bool, error) {
	log.Println("檢查執行觸發條件（新借貸訂單、餘額變化）...")

	// 獲取當前可用餘額
	currentBalance, err := lb.getAvailableFunds()
	if err != nil {
		log.Printf("獲取餘額失敗: %v", err)
		return false, err
	}

	// 獲取當前活躍的借貸訂單
	credits, err := lb.client.GetFundingCredits(lb.config.GetFundingSymbol())
	if err != nil {
		log.Printf("獲取借貸訂單失敗: %v", err)
		return false, err
	}

	// 獲取當前時間戳（毫秒）
	currentTime := time.Now().UnixNano() / int64(time.Millisecond)

	// 如果這是第一次檢查，初始化時間戳和餘額但不觸發執行
	if lb.config.LastLendingCheckTime == 0 {
		log.Printf("首次檢查，發現 %d 個現有借貸訂單，餘額: %.2f，初始化檢查參數", len(credits), currentBalance)
		lb.config.LastLendingCheckTime = currentTime
		lb.config.LastAvailableBalance = currentBalance
		lb.config.SeenFundingCreditIDs = buildSeenCreditIDs(credits)
		return false, nil
	}

	shouldExecute := false
	var reasons []string

	// 檢查1: 是否有新的借貸訂單
	newCredits := findNewCredits(credits, lb.config.SeenFundingCreditIDs, lb.config.LastLendingCheckTime)

	if len(newCredits) > 0 {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("發現 %d 個新的借貸訂單", len(newCredits)))
		// 發送借貸通知
		if err := lb.sendLendingNotification(newCredits); err != nil {
			log.Printf("發送借貸通知失敗: %v", err)
		}
	}

	// 檢查2: 餘額是否顯著增加
	lastBalance := lb.config.LastAvailableBalance
	balanceIncrease := currentBalance - lastBalance

	// 設定觸發閾值：餘額增加超過10%或超過最小貸出金額
	increaseThreshold := math.Max(lastBalance*0.1, lb.config.MinLoan)

	if balanceIncrease > increaseThreshold {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("餘額顯著增加: %.2f -> %.2f (+%.2f)", lastBalance, currentBalance, balanceIncrease))
		if len(newCredits) == 0 {
			lb.sendBalanceChangeNotification(lastBalance, currentBalance, balanceIncrease)
		}
	}

	// 檢查3: 從零餘額恢復
	if lastBalance == 0 && currentBalance > lb.config.MinLoan {
		shouldExecute = true
		reasons = append(reasons, fmt.Sprintf("從零餘額恢復: %.2f -> %.2f", lastBalance, currentBalance))
	}

	// 更新檢查參數
	lb.config.LastLendingCheckTime = currentTime
	lb.config.LastAvailableBalance = currentBalance
	lb.config.SeenFundingCreditIDs = buildSeenCreditIDs(credits)

	if shouldExecute {
		log.Printf("觸發策略執行，原因: %s", strings.Join(reasons, "; "))
		return true, nil
	}

	log.Printf("無需執行策略，餘額: %.2f (上次: %.2f)，無新借貸訂單", currentBalance, lastBalance)
	return false, nil
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

func (lb *LendingBot) sendBalanceChangeNotification(lastBalance, currentBalance, balanceIncrease float64) {
	if lb.notifyCallback == nil {
		log.Println("Telegram 通知回調未設置，跳過餘額變化通知")
		return
	}

	message := fmt.Sprintf("💰 可用餘額變化通知\n\n餘額: %.2f -> %.2f %s\n增加: %.2f %s\n\n未從 Funding Credits API 識別到新的成交 ID，請檢查 Bitfinex 借貸狀態。",
		lastBalance,
		currentBalance,
		lb.config.Currency,
		balanceIncrease,
		lb.config.Currency)

	if err := lb.notifyCallback(message); err != nil {
		log.Printf("發送餘額變化通知失敗: %v", err)
		return
	}

	log.Println("餘額變化通知發送成功")
}

// sendLendingNotification 發送借貸訂單通知
func (lb *LendingBot) sendLendingNotification(credits []*bitfinex.FundingCredit) error {
	if lb.notifyCallback == nil {
		log.Println("Telegram 通知回調未設置，跳過通知")
		return nil
	}

	message := "💰 新的借貸訂單通知\n\n"

	frrFallbackRate := 0.0
	for _, credit := range credits {
		if credit.EffectiveDailyRate() == 0 {
			rate, err := lb.client.GetCurrentFundingRate(lb.config.GetFundingSymbol())
			if err != nil {
				log.Printf("取得 FRR 利率失敗: %v", err)
				break
			}
			frrFallbackRate = rate
			break
		}
	}

	// 先計算所有訂單的統計信息
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

	// 顯示詳細信息（最多顯示配置數量的訂單）
	for i, credit := range credits {
		if i >= constants.MaxDisplayOrders {
			remaining := len(credits) - constants.MaxDisplayOrders
			message += fmt.Sprintf("... 還有 %d 個訂單\n", remaining)
			break
		}

		// 計算預期收益（日利率 * 金額 * 期間）
		effectiveRate := credit.EffectiveDailyRate()
		if effectiveRate == 0 && frrFallbackRate > 0 {
			effectiveRate = frrFallbackRate
		}
		dailyEarnings := credit.Amount * effectiveRate
		periodEarnings := dailyEarnings * float64(credit.Period)

		// 格式化開始時間
		openTime := time.Unix(credit.MTSOpened/1000, 0)

		message += fmt.Sprintf("📊 訂單 #%d\n", i+1)
		message += fmt.Sprintf("💵 金額: %.2f %s\n", credit.Amount, lb.config.Currency)
		message += fmt.Sprintf("📈 日利率: %.4f%%\n", lb.rateConverter.DecimalToPercentage(effectiveRate))
		message += fmt.Sprintf("📈 年利率: %.4f%%\n", lb.rateConverter.DecimalToPercentage(effectiveRate)*constants.DaysPerYear)
		message += fmt.Sprintf("⏰ 期間: %d 天\n", credit.Period)
		message += fmt.Sprintf("💰 預期收益: %.4f %s\n", periodEarnings, lb.config.Currency)
		message += fmt.Sprintf("🕐 開始時間: %s\n", openTime.Format("2006-01-02 15:04:05"))
		message += "\n"
	}

	// 添加統計信息
	message += fmt.Sprintf("📊 統計信息:\n")
	message += fmt.Sprintf("📦 總數量: %d 個訂單\n", len(credits))
	message += fmt.Sprintf("💵 總金額: %.2f %s\n", totalAmount, lb.config.Currency)
	message += fmt.Sprintf("💰 總預期收益: %.4f %s\n", totalEarnings, lb.config.Currency)

	// 嘗試發送通知，如果失敗（例如 Telegram 未認證）只記錄日誌但不返回錯誤
	if err := lb.notifyCallback(message); err != nil {
		log.Printf("發送借貸訂單通知失敗: %v", err)
		log.Println("新借貸訂單通知內容:")
		log.Println(message)
		return nil // 不返回錯誤，避免影響主程序執行
	}

	log.Println("借貸訂單通知發送成功")
	return nil
}

// GetActiveLendingCredits 獲取活躍借貸訂單（供 Telegram 指令使用）
func (lb *LendingBot) GetActiveLendingCredits() ([]*bitfinex.FundingCredit, error) {
	return lb.client.GetFundingCredits(lb.config.GetFundingSymbol())
}

// calculateKlineOffers 基於K線數據計算貸出訂單
func (lb *LendingBot) calculateKlineOffers(fundsAvailable float64) []*LoanOffer {
	var loanOffers []*LoanOffer

	// 檢查可用資金
	if fundsAvailable < lb.config.MinLoan {
		return loanOffers
	}

	// 獲取K線數據
	candles, _ := lb.client.GetFundingCandles(
		lb.config.GetFundingSymbol(),
		lb.config.KlineTimeFrame,
		lb.config.KlinePeriod,
	)

	// 找到最近期間內的最高利率
	highestRate := lb.findHighestRateFromCandles(candles)
	log.Printf("K線數據分析：最高利率 %.6f%%", lb.rateConverter.DecimalToPercentage(highestRate))

	// 計算目標利率（最高利率 + 加成）
	spreadMultiplier := 1.0 + (lb.config.KlineSpreadPercent / 100.0)
	targetRate := highestRate * spreadMultiplier

	// 確保不低於最小利率
	minDailyRate := lb.config.GetMinDailyRateDecimal()
	if targetRate < minDailyRate {
		targetRate = minDailyRate
		log.Printf("目標利率低於最小利率，使用最小利率: %.6f%%", lb.rateConverter.DecimalToPercentage(targetRate))
	}

	log.Printf("K線策略目標利率: %.6f%% (加成: %.1f%%)",
		lb.rateConverter.DecimalToPercentage(targetRate),
		lb.config.KlineSpreadPercent)

	splitFundsAvailable := fundsAvailable

	// 高額持有策略
	if lb.config.HighHoldAmount > lb.config.MinLoan {
		highHoldOffers := lb.calculateHighHoldOffers(&splitFundsAvailable)
		loanOffers = append(loanOffers, highHoldOffers...)
	}

	// 使用目標利率創建分散訂單
	if splitFundsAvailable >= lb.config.MinLoan {
		remainingSlots := lb.getRemainingOrderSlots(len(loanOffers))
		if remainingSlots != 0 {
			klineOffers := lb.calculateKlineSpreadOffers(splitFundsAvailable, targetRate, remainingSlots)
			loanOffers = append(loanOffers, klineOffers...)
		}
	}

	return loanOffers
}

// findHighestRateFromCandles 從K線數據中找到最高利率
func (lb *LendingBot) findHighestRateFromCandles(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	// 根據配置選擇平滑方法
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
		log.Printf("未知的平滑方法: %s，使用預設的 EMA", lb.config.KlineSmoothMethod)
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

// calculateSMA 計算收盤價的簡單移動平均
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

// calculateEMAHigh 計算高點的指數移動平均
func (lb *LendingBot) calculateEMAHigh(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	// EMA 係數，期間越長係數越小
	alpha := 2.0 / (float64(len(candles)) + 1.0)
	ema := candles[0].High

	for i := 1; i < len(candles); i++ {
		ema = alpha*candles[i].High + (1-alpha)*ema
	}

	return ema
}

// calculateHighLowAverage 計算高低點平均
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

	// 取高低點平均的平均（偏向高點一些）
	return (avgHigh + avgLow) / 2.0
}

// calculate90Percentile 計算90百分位數
func (lb *LendingBot) calculate90Percentile(candles []*bitfinex.Candle) float64 {
	if len(candles) == 0 {
		return lb.config.GetMinDailyRateDecimal()
	}

	// 收集所有高點
	highs := make([]float64, len(candles))
	for i, candle := range candles {
		highs[i] = candle.High
	}

	// 簡單排序
	for i := 0; i < len(highs); i++ {
		for j := i + 1; j < len(highs); j++ {
			if highs[i] > highs[j] {
				highs[i], highs[j] = highs[j], highs[i]
			}
		}
	}

	// 計算90百分位數的索引
	index := int(float64(len(highs)) * 0.9)
	if index >= len(highs) {
		index = len(highs) - 1
	}

	return highs[index]
}

// calculateKlineSpreadOffers 基於K線目標利率計算分散訂單
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

	// 創建訂單，使用目標利率為基準，微調以分散風險
	for i, allocAmount := range orderAmounts {
		if allocAmount < lb.config.MinLoan {
			break
		}

		rate := targetRate * (1 + (float64(i) * lb.config.RateRangeIncreasePercent))

		// 確保利率不低於最小利率
		minDailyRate := lb.config.GetMinDailyRateDecimal()
		if rate < minDailyRate {
			rate = minDailyRate
		}

		// 計算期間
		period := lb.calculatePeriod(rate)

		offer := &LoanOffer{
			Amount: allocAmount,
			Rate:   rate,
			Period: period,
			UseFRR: useFRR, // K線分散單依 MIN_DAILY_LEND_RATE 是否為 FRR 決定
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
