package main

import (
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/book"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/common"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/fundingoffer"
	"github.com/bitfinexcom/bitfinex-api-go/v2/rest"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/spf13/viper"
	"github.com/urfave/cli"
)

// envStruct 储存应用程序的环境变数设定
type envStruct struct {
	BitfinexApiKey                string  `mapstructure:"BITFINEX_API_KEY" json:"BITFINEX_API_KEY"`                                     // Bitfinex API 金钥
	BitfinexSecretKey             string  `mapstructure:"BITFINEX_SECRET_KEY" json:"BITFINEX_SECRET_KEY"`                               // Bitfinex API 密钥
	Currency                      string  `mapstructure:"CURRENCY" json:"CURRENCY"`                                                     // 交易币种 (例如：USD, BTC, ETH)
	OrderLimit                    int     `mapstructure:"ORDER_LIMIT" json:"ORDER_LIMIT"`                                               // 单次执行最大下单数量限制
	MinutesRun                    int     `mapstructure:"MINUTES_RUN" json:"MINUTES_RUN"`                                               // 机器人执行间隔时间 (分钟)
	MinLoan                       float64 `mapstructure:"MIN_LOAN" json:"MIN_LOAN"`                                                     // 最小贷出金额
	MaxLoan                       float64 `mapstructure:"MAX_LOAN" json:"MAX_LOAN"`                                                     // 最大贷出金额限制
	MinDailyLendRate              float64 `mapstructure:"MIN_DAILY_LEND_RATE" json:"MIN_DAILY_LEND_RATE"`                               // 最低每日贷出利率
	SpreadLend                    int     `mapstructure:"SPREAD_LEND" json:"SPREAD_LEND"`                                               // 资金分散贷出的笔数
	GapBottom                     float64 `mapstructure:"GAP_BOTTOM" json:"GAP_BOTTOM"`                                                 // 利率阶梯的底部区间
	GapTop                        float64 `mapstructure:"GAP_TOP" json:"GAP_TOP"`                                                       // 利率阶梯的顶部区间
	ThirtyDayLendRateThreshold    float64 `mapstructure:"THIRTY_DAY_LEND_RATE_THRESHOLD" json:"THIRTY_DAY_LEND_RATE_THRESHOLD"`         // 触发30天期贷出的日利率阈值
	OneTwentyDayLendRateThreshold float64 `mapstructure:"ONE_TWENTY_DAY_LEND_RATE_THRESHOLD" json:"ONE_TWENTY_DAY_LEND_RATE_THRESHOLD"` // 触发120天期贷出的日利率阈值
	HighHoldRate                  float64 `mapstructure:"HIGH_HOLD_RATE" json:"HIGH_HOLD_RATE"`                                         // 高额持有策略的日利率
	HighHoldAmount                float64 `mapstructure:"HIGH_HOLD_AMOUNT" json:"HIGH_HOLD_AMOUNT"`                                     // 高额持有策略的金额
	HighHoldOrders                int     `mapstructure:"HIGH_HOLD_ORDERS" json:"HIGH_HOLD_ORDERS"`                                     // 高额持有策略的订单数量
	RateBonus                     float64 `mapstructure:"RATE_BONUS" json:"RATE_BONUS"`                                                 // 无挂单时的利率加成
	TelegramBotToken              string  `mapstructure:"TELEGRAM_BOT_TOKEN" json:"TELEGRAM_BOT_TOKEN"`                                 // Telegram 机器人 Token
	TelegramAuthToken             string  `mapstructure:"TELEGRAM_AUTH_TOKEN" json:"TELEGRAM_AUTH_TOKEN"`                               // Telegram 验证 Token
	NotifyRateThreshold           float64 `mapstructure:"NOTIFY_RATE_THRESHOLD" json:"NOTIFY_RATE_THRESHOLD"`                           // 利率通知阈值
	ReserveAmount                 float64 `mapstructure:"RESERVE_AMOUNT" json:"RESERVE_AMOUNT"`                                         // 保留金额，不参与借贷
}

var (
	env    envStruct
	client *rest.Client
	bot    *tgbotapi.BotAPI
)

// MarginBotConf 设定机器人运作参数
type MarginBotConf struct {
	MinDailyLendRate              float64
	SpreadLend                    int
	GapBottom                     float64
	GapTop                        float64
	ThirtyDayLendRateThreshold    float64
	OneTwentyDayLendRateThreshold float64
	HighHoldRate                  float64
	HighHoldAmount                float64
	HighHoldOrders                int
	MinLoan                       float64
	MaxLoan                       float64
}

// MarginBotLoanOffer 贷出订单资讯
type MarginBotLoanOffer struct {
	Amount float64
	Rate   float64
	Period int
}

// MarginBotLoanOffers 多笔贷出订单阵列
type MarginBotLoanOffers []MarginBotLoanOffer

// 全局验证映射表改为单一聊天ID
var authenticatedChatID int64
var chatIDMutex sync.Mutex

func main() {
	app := cli.NewApp()
	app.Name = "bitfindex-bot"
	app.Version = "v0.0.1"

	// 设定 CLI 参数
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "config, c",
			Value:  "config.yaml",
			Usage:  "app config",
			EnvVar: "CONFIG_PATH",
		},
	}

	app.Action = runApp

	// 执行 CLI
	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}

// runApp 为主要的执行流程
func runApp(c *cli.Context) {
	loadConfig(c.String("config"))
	initBitfinexClient()
	initTelegramBot()

	go handleTelegramMessages()

	// 启动每小时06分的贷出利率检查
	go scheduleHourlyTask(6, checkLendRate)

	log.Println("ENV:", env)
	log.Println("Config 设定成功")

	fmt.Println("Scheduler started at:", time.Now())
	scheduleTask(env.MinutesRun, botRun)

	select {} // 阻塞主程序，使其持续执行
}

// loadConfig 读取并解析设定文件及环境变数
func loadConfig(configPath string) {
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		panic(err)
	}

	if err := viper.Unmarshal(&env); err != nil {
		panic(err)
	}
}

// initBitfinexClient 初始化 Bitfinex 客户端
func initBitfinexClient() {
	client = rest.NewClient().Credentials(env.BitfinexApiKey, env.BitfinexSecretKey)
}

// initTelegramBot 初始化 Telegram bot 客户端
func initTelegramBot() {
	var err error
	bot, err = tgbotapi.NewBotAPI(env.TelegramBotToken)
	if err != nil {
		log.Panic(err)
	}

	log.Printf("Authorized on account %s", bot.Self.UserName)
}

// scheduleTask 定时执行任务，每 n 分钟执行一次
func scheduleTask(minutes int, task func()) {
	// 先执行第一次
	task()

	ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		task()
	}
}

// scheduleHourlyTask 在每小时的指定分钟执行任务
func scheduleHourlyTask(minute int, task func()) {
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), minute, 0, 0, now.Location())
		if now.After(next) || now.Equal(next) {
			next = next.Add(time.Hour)
		}

		delay := next.Sub(now)
		log.Printf("下次执行时间: %s, 等待时间: %s", next.Format("2006-01-02 15:04:05"), delay)

		time.Sleep(delay)
		task()
	}
}

// botRun 执行机器人主程序逻辑
func botRun() {
	fmt.Println("取消所有未完成订单...")
	hasPendingOrders := cancelAllOffers()

	// 暂停几秒避免取消订单的金额还没归还
	time.Sleep(5 * time.Second)

	fmt.Println("取得可用额度...")
	fundsAvailable, err := getAvailableFunds(env.Currency)
	if err != nil {
		fmt.Println("取得余额错误:", err)
		return
	}
	fmt.Printf("Currency: %s  Available: %f \n", env.Currency, fundsAvailable)

	// 扣除保留金额
	if env.ReserveAmount > 0 {
		fundsAvailable = math.Max(0, fundsAvailable-env.ReserveAmount)
		fmt.Printf("扣除保留金额后可用: %f \n", fundsAvailable)
	}

	// 若扣除保留金额后可用资金小于最小贷出额，则不进行操作
	if fundsAvailable < env.MinLoan {
		fmt.Println("可用资金小于最小贷出额，不进行操作")
		return
	}

	// 取得目前 Funding Book (Lendbook)
	fundingSymbol := "f" + strings.ToUpper(env.Currency)                         // 转换为 funding symbol (例如：fUSD)
	lendbook, err := client.Book.All(fundingSymbol, common.PrecisionRawBook, 25) // 使用 R0 精度和默认价格水平
	if err != nil {
		fmt.Println("取得 Funding Book 错误:", err)
		return
	}

	// 依据机器人逻辑配置，算出要下单的贷出列表
	loanOffers := marginBotGetLoanOffers(
		fundsAvailable,
		lendbook,
		MarginBotConf{
			MinDailyLendRate:              env.MinDailyLendRate,
			SpreadLend:                    env.SpreadLend,
			GapBottom:                     env.GapBottom,
			GapTop:                        env.GapTop,
			ThirtyDayLendRateThreshold:    env.ThirtyDayLendRateThreshold,
			OneTwentyDayLendRateThreshold: env.OneTwentyDayLendRateThreshold,
			HighHoldRate:                  env.HighHoldRate,
			HighHoldAmount:                env.HighHoldAmount,
			HighHoldOrders:                env.HighHoldOrders,
			MinLoan:                       env.MinLoan,
			MaxLoan:                       env.MaxLoan,
		},
	)

	// 依照算出的贷出订单逐笔下单，且控制在 OrderLimit 以内
	placeLoanOffers(loanOffers, env.OrderLimit, hasPendingOrders)
}

// cancelAllOffers 取消所有未完成订单
func cancelAllOffers() (hasPendingOrders bool) {
	hasPendingOrders = false

	fundingSymbol := "f" + strings.ToUpper(env.Currency) // 转换为 funding symbol
	offers, err := client.Funding.Offers(fundingSymbol)
	if err != nil {
		fmt.Println("取得未完成订单失败:", err)
		return hasPendingOrders
	}

	// 检查是否有订单数据
	if offers != nil && len(offers.Snapshot) > 0 {
		for _, offer := range offers.Snapshot {
			hasPendingOrders = true

			// 取消订单
			cancelReq := &fundingoffer.CancelRequest{
				ID: offer.ID,
			}
			_, err := client.Funding.CancelOffer(cancelReq)
			if err != nil {
				fmt.Println("取消订单失败:", err)
			} else {
				fmt.Printf("成功取消订单 ID: %d\n", offer.ID)
			}
		}
	} else {
		fmt.Println("目前没有未完成的订单")
	}

	return hasPendingOrders
}

// getAvailableFunds 取得指定币别的可用余额
func getAvailableFunds(currency string) (float64, error) {
	wallets, err := client.Wallet.Wallet()
	if err != nil {
		return 0, err
	}

	// 在 v2 API 中，我们需要寻找 funding 钱包类型
	for _, wallet := range wallets.Snapshot {
		if wallet.Currency == strings.ToUpper(currency) && wallet.Type == "funding" {
			return wallet.BalanceAvailable, nil
		}
	}
	return 0, nil
}

// placeLoanOffers 依照产生的贷出订单阵列逐笔下单
func placeLoanOffers(loanOffers MarginBotLoanOffers, orderLimit int, hasPendingOrders bool) {
	orderCount := 0
	for _, o := range loanOffers {
		if orderLimit != 0 && orderCount >= orderLimit {
			break
		}

		if !hasPendingOrders {
			o.Rate = (o.Rate/365 + env.RateBonus) * 365
		}

		fmt.Printf("下单 => Rate: %.6f, Amount: %.4f, Period: %d \n", o.Rate/365, o.Amount, o.Period)

		// 创建 funding offer 请求
		fundingSymbol := "f" + strings.ToUpper(env.Currency)
		offerReq := &fundingoffer.SubmitRequest{
			Type:   "LIMIT",
			Symbol: fundingSymbol,
			Amount: o.Amount,
			Rate:   o.Rate / 365, // v2 API 使用日利率
			Period: int64(o.Period),
			Hidden: false,
		}

		_, err := client.Funding.SubmitOffer(offerReq)
		if err != nil {
			fmt.Println("下订单失败:", err)
		} else {
			orderCount++
		}
	}
}

// marginBotGetLoanOffers 计算并生成贷出订单清单
func marginBotGetLoanOffers(
	fundsAvailable float64,
	lendbook *book.Snapshot,
	conf MarginBotConf,
) (loanOffers MarginBotLoanOffers) {

	// 如果可用资金小于最小贷出额，则不进行操作
	if fundsAvailable < conf.MinLoan {
		return
	}

	// 初始化可分配资金
	splitFundsAvailable := fundsAvailable

	// 高持有策略: 若 HighHoldAmount 大于最小贷出额，则执行高额持有策略
	if conf.HighHoldAmount > conf.MinLoan {
		// 检查高额持有订单数量设定
		ordersCount := conf.HighHoldOrders
		if ordersCount <= 0 {
			ordersCount = 1 // 如果未设置订单数量或无效值，则默认为 1 笔
		}

		// 订单金额
		highHold := conf.HighHoldAmount

		// 若设定了 MaxLoan，且 highHold 大于 MaxLoan，则裁切为 MaxLoan
		if conf.MaxLoan > 0 && highHold > conf.MaxLoan {
			highHold = conf.MaxLoan
		}

		// 创建多笔相同金额的高额持有订单
		// 计算实际可以创建的订单数量（基于可用资金）
		possibleOrders := int(splitFundsAvailable / highHold)
		actualOrders := math.Min(float64(ordersCount), float64(possibleOrders))

		// 下订单
		for i := 0; i < int(actualOrders); i++ {
			// 确保每笔金额不超过剩余资金
			if splitFundsAvailable < highHold {
				break
			}

			// 创建订单
			tmp := MarginBotLoanOffer{
				Amount: highHold,
				Rate:   conf.HighHoldRate / 100 * 365, // 配置文件中是百分比，转换为年化利率
				Period: 120,                           // 固定贷出 120 天
			}
			loanOffers = append(loanOffers, tmp)
			splitFundsAvailable -= highHold
		}
	}

	// 分割资金成多笔贷出
	numSplits := conf.SpreadLend
	if numSplits <= 0 || splitFundsAvailable < conf.MinLoan {
		return
	}

	// 计算每笔贷出金额 (初始)
	amtEach := splitFundsAvailable / float64(numSplits)
	amtEach = float64(int64(amtEach*100)) / 100.0 // 保留小数点后两位

	// 若每笔金额小于最小贷出额，尝试调降分割数
	for amtEach <= conf.MinLoan && numSplits > 1 {
		numSplits--
		amtEach = splitFundsAvailable / float64(numSplits)
		amtEach = float64(int64(amtEach*100)) / 100.0
	}
	if numSplits <= 0 {
		return
	}

	// 计算利率递增量
	gapClimb := (conf.GapTop - conf.GapBottom) / float64(numSplits)
	nextLend := conf.GapBottom

	// 以市场深度遍历，计算对应利率
	depthIndex := 0

	for numSplits > 0 {
		// 累计市场量至指定利率区间
		for float64(depthIndex) < nextLend && depthIndex < len(lendbook.Snapshot)-1 {
			depthIndex++
		}

		tmp := MarginBotLoanOffer{}

		// 依照计算出的 amtEach 与 MaxLoan 进行裁切
		allocAmount := amtEach
		// 若有设定 MaxLoan，且 allocAmount 大于 MaxLoan，则调整为 MaxLoan
		if conf.MaxLoan > 0 && allocAmount > conf.MaxLoan {
			allocAmount = conf.MaxLoan
		}
		tmp.Amount = allocAmount

		// 若计算后的金额仍小于 MinLoan, 则不需要下单
		if tmp.Amount < conf.MinLoan {
			break
		}

		// 依据市场利率 vs 最低日利率
		// 在 v2 API 中，book.Book.Rate 已经是日利率
		dailyRate := lendbook.Snapshot[depthIndex].Rate
		minDailyRate := conf.MinDailyLendRate / 100 // 配置文件中是百分比
		if dailyRate < minDailyRate {
			tmp.Rate = minDailyRate * 365 // 储存为年化利率以保持兼容性
		} else {
			tmp.Rate = dailyRate * 365 // 转换为年化利率以保持兼容性
		}

		if conf.OneTwentyDayLendRateThreshold > 0 && tmp.Rate >= (conf.OneTwentyDayLendRateThreshold/100)*365 {
			tmp.Period = 120 // 若市场年化利率高于阈值，则将订单期间设定为 120 天
		} else if conf.ThirtyDayLendRateThreshold > 0 && tmp.Rate >= (conf.ThirtyDayLendRateThreshold/100)*365 {
			tmp.Period = 30 // 若市场年化利率高于阈值，则将订单期间设定为 30 天
		} else {
			tmp.Period = 2 // 若市场年化利率低于阈值，则将订单期间设定为 2 天
		}

		loanOffers = append(loanOffers, tmp)
		nextLend += gapClimb
		numSplits--
	}

	return
}

func handleTelegramMessages() {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, err := bot.GetUpdatesChan(u)
	if err != nil {
		log.Panic(err)
	}

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		text := update.Message.Text

		// 处理身份验证
		isAuthenticated := getAuthenticatedChatID() == chatID

		// 处理验证过程
		if text == "/auth" {
			msg := tgbotapi.NewMessage(chatID, "请输入验证 token：")
			bot.Send(msg)
			continue
		} else if text == env.TelegramAuthToken {
			setAuthenticatedChatID(chatID)
			msg := tgbotapi.NewMessage(chatID, "验证成功，现在可以传送指令了")
			bot.Send(msg)
			continue
		} else if !isAuthenticated {
			msg := tgbotapi.NewMessage(chatID, "请先进行验证，输入 /auth 开始验证流程")
			bot.Send(msg)
			continue
		}

		// 处理已验证用户的指令
		switch {
		case text == "/help" || text == "/start":
			// 显示帮助讯息
			helpText := `可用指令:
/rate - 显示当前贷出利率和阈值
/check - 检查贷出利率是否超过阈值
/threshold [数值] - 设置利率通知阈值
/reserve [数值] - 设置不参与借贷的保留金额
/orderlimit [数值] - 设置单次执行最大下单数量限制
/mindailylendrate [数值] - 设置最低每日贷出利率
/highholdrate [数值] - 设置高额持有策略的日利率
/highholdamount [数值] - 设置高额持有策略的金额
/highholdorders [数值] - 设置高额持有策略的订单数量
/status - 显示系统状态
/help - 显示此帮助讯息
/restart - 手动重新启动，清除所有订单，重新运行`
			msg := tgbotapi.NewMessage(chatID, helpText)
			bot.Send(msg)

		// 手动重新启动，清除所有订单，重新运行
		case text == "/restart":
			botRun()
			msg := tgbotapi.NewMessage(chatID, "机器人已重新启动，清除所有订单，重新运行")
			bot.Send(msg)
		case text == "/rate":
			// 显示当前贷出利率
			rate, err := getLendRate()
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, "取得贷出利率失败")
				bot.Send(msg)
			} else {
				thresholdInfo := ""
				if env.NotifyRateThreshold > 0 {
					thresholdInfo = fmt.Sprintf("\n目前设定的阈值为: %.4f%%", env.NotifyRateThreshold)
				}
				msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("目前贷出利率: %.4f%%%s", rate*100, thresholdInfo))
				bot.Send(msg)
			}
		case text == "/check":
			// 执行检查并获取结果
			rate, err := getLendRate()
			if err != nil {
				msg := tgbotapi.NewMessage(chatID, "取得贷出利率失败")
				bot.Send(msg)
				continue
			}

			log.Printf("手动检查: 当前贷出利率: %.4f", rate)

			replyMsg := fmt.Sprintf("当前贷出利率: %.4f%%\n阈值: %.4f%%", rate*100, env.NotifyRateThreshold)

			if rate*100 > env.NotifyRateThreshold {
				replyMsg += "\n⚠️ 注意: 当前利率已超过阈值!"
			} else {
				replyMsg += "\n✓ 当前利率低于阈值"
			}

			msg := tgbotapi.NewMessage(chatID, replyMsg)
			bot.Send(msg)

		case strings.HasPrefix(text, "/threshold "):
			// 设置阈值
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /threshold [数值] 格式")
				bot.Send(msg)
				continue
			}

			threshold, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || threshold <= 0 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的正数值")
				bot.Send(msg)
				continue
			}

			env.NotifyRateThreshold = threshold

			// 理想情况下应该将新阈值保存到配置文件中
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("阈值已设定为: %.4f%%", threshold))
			bot.Send(msg)

		case strings.HasPrefix(text, "/reserve "):
			// 设置保留金额
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /reserve [数值] 格式")
				bot.Send(msg)
				continue
			}

			reserve, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || reserve < 0 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的非负数值")
				bot.Send(msg)
				continue
			}

			env.ReserveAmount = reserve

			// 理想情况下应该将新保留金额保存到配置文件中
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("保留金额已设定为: %.2f", reserve))
			bot.Send(msg)

		case strings.HasPrefix(text, "/orderlimit "):
			// 设置单次执行最大下单数量限制
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /orderlimit [数值] 格式")
				bot.Send(msg)
				continue
			}

			limit, err := strconv.Atoi(parts[1])
			if err != nil || limit < 0 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的非负整数")
				bot.Send(msg)
				continue
			}

			env.OrderLimit = limit

			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("单次执行最大下单数量限制已设定为: %d", limit))
			bot.Send(msg)

		case strings.HasPrefix(text, "/mindailylendrate "):
			// 设置最低每日贷出利率
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /mindailylendrate [数值] 格式")
				bot.Send(msg)
				continue
			}

			rate, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || rate <= 0 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的正数值")
				bot.Send(msg)
				continue
			}

			env.MinDailyLendRate = rate

			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("最低每日贷出利率已设定为: %.4f%%", rate))
			bot.Send(msg)

		case strings.HasPrefix(text, "/highholdrate "):
			// 设置高额持有策略的日利率
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /highholdrate [数值] 格式")
				bot.Send(msg)
				continue
			}

			rate, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || rate <= 0 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的正数值")
				bot.Send(msg)
				continue
			}

			env.HighHoldRate = rate

			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("高额持有策略的日利率已设定为: %.4f%%", rate))
			bot.Send(msg)

		case strings.HasPrefix(text, "/highholdamount "):
			// 设置高额持有策略的金额
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /highholdamount [数值] 格式")
				bot.Send(msg)
				continue
			}

			amount, err := strconv.ParseFloat(parts[1], 64)
			if err != nil || amount <= 0 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的正数值")
				bot.Send(msg)
				continue
			}

			env.HighHoldAmount = amount

			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("高额持有策略的金额已设定为: %.2f", amount))
			bot.Send(msg)

		case strings.HasPrefix(text, "/highholdorders "):
			// 设置高额持有订单数量
			parts := strings.Split(text, " ")
			if len(parts) != 2 {
				msg := tgbotapi.NewMessage(chatID, "格式错误，请使用 /highholdorders [数值] 格式")
				bot.Send(msg)
				continue
			}

			orders, err := strconv.Atoi(parts[1])
			if err != nil || orders < 1 {
				msg := tgbotapi.NewMessage(chatID, "请输入有效的正整数")
				bot.Send(msg)
				continue
			}

			env.HighHoldOrders = orders

			// 理想情况下应该将新设定保存到配置文件中
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("高额持有订单数量已设定为: %d", orders))
			bot.Send(msg)

		case text == "/status":
			statusMsg := fmt.Sprintf("目前系统状态正常\n币种: %s\n最小贷出金额: %.2f\n最大贷出金额: %.2f", env.Currency, env.MinLoan, env.MaxLoan)

			// 添加保留金额信息
			if env.ReserveAmount > 0 {
				statusMsg += fmt.Sprintf("\n保留金额: %.2f", env.ReserveAmount)
			} else {
				statusMsg += "\n未设置保留金额"
			}

			// 添加机器人运行参数
			statusMsg += fmt.Sprintf("\n\n机器人运行参数:")
			statusMsg += fmt.Sprintf("\n单次执行最大下单数量限制: %d", env.OrderLimit)
			statusMsg += fmt.Sprintf("\n最低每日贷出利率: %.4f%%", env.MinDailyLendRate)

			// 添加高额持有策略信息
			statusMsg += fmt.Sprintf("\n\n高额持有策略:")
			if env.HighHoldAmount > 0 {
				statusMsg += fmt.Sprintf("\n金额: %.2f", env.HighHoldAmount)
				statusMsg += fmt.Sprintf("\n日利率: %.4f%%", env.HighHoldRate)
				statusMsg += fmt.Sprintf("\n订单数量: %d", env.HighHoldOrders)
			} else {
				statusMsg += "\n未启用高额持有策略"
			}

			msg := tgbotapi.NewMessage(chatID, statusMsg)
			bot.Send(msg)

		default:
			msg := tgbotapi.NewMessage(chatID, "无效的指令，输入 /help 查看所有可用指令")
			bot.Send(msg)
		}
	}
}

func getLendRate() (float64, error) {
	// 在 v2 API 中，我们使用 funding book 来获取当前利率
	fundingSymbol := "f" + strings.ToUpper(env.Currency)
	book, err := client.Book.All(fundingSymbol, common.PrecisionRawBook, 25) // 使用 R0 精度

	if err != nil {
		return 0, err
	}

	if len(book.Snapshot) == 0 {
		return 0, fmt.Errorf("no funding book data available")
	}

	// 取得第一个 ask (贷出) 利率
	return book.Snapshot[0].Rate, nil
}

// checkLendRate 检查贷出利率是否超过阈值，并在超过时发送通知
func checkLendRate() {
	log.Println("定时检查贷出利率...")

	// 获取当前贷出利率
	rate, err := getLendRate()
	if err != nil {
		log.Printf("取得贷出利率失败: %v", err)
		return
	}

	log.Printf("当前贷出利率: %.4f%%, 阈值: %.4f%%", rate*100, env.NotifyRateThreshold)

	// 检查是否需要发送通知
	if rate*100 > env.NotifyRateThreshold {
		chatID := getAuthenticatedChatID()
		if chatID == 0 {
			log.Println("尚未设定聊天ID，无法发送通知")
			return
		}

		notifyMsg := fmt.Sprintf("⚠️ 定时检查提醒: 目前贷出利率 %.4f%% 已超过阈值 %.4f%%", rate*100, env.NotifyRateThreshold)
		msg := tgbotapi.NewMessage(chatID, notifyMsg)

		if _, err := bot.Send(msg); err != nil {
			log.Printf("发送 Telegram 通知失败: %v", err)
		} else {
			log.Printf("成功发送利率提醒至聊天ID: %d", chatID)
		}
	} else {
		log.Println("当前利率低于阈值，无需发送通知")
	}
}

// setAuthenticatedChatID 设置已验证的单一聊天ID
func setAuthenticatedChatID(chatID int64) {
	chatIDMutex.Lock()
	authenticatedChatID = chatID
	chatIDMutex.Unlock()
}

// getAuthenticatedChatID 获取已验证的单一聊天ID
func getAuthenticatedChatID() int64 {
	chatIDMutex.Lock()
	defer chatIDMutex.Unlock()

	return authenticatedChatID
}
