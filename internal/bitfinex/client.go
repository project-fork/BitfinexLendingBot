package bitfinex

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"

	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/common"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/fundingoffer"
	"github.com/bitfinexcom/bitfinex-api-go/v2/rest"

	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/errors"
)

// Client Bitfinex API 客户端封装
type Client struct {
	restClient *rest.Client
}

// NewClient 创建新的 Bitfinex 客户端
func NewClient(apiKey, secretKey string) *Client {
	nonceGenerator, err := newPersistentNonceGenerator(defaultNonceStateFilePath())
	if err != nil {
		nonceGenerator = nil
	}

	client := rest.NewClient()
	if nonceGenerator != nil {
		client = rest.NewClientWithURLNonce("https://api-pub.bitfinex.com/v2/", nonceGenerator)
	}
	client = client.Credentials(apiKey, secretKey)

	if nonceGenerator == nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to initialize persistent nonce generator, falling back to default nonce behavior\n")
	}

	return &Client{
		restClient: client,
	}
}

// FundingOffer 代表一个资金贷出订单
type FundingOffer struct {
	ID     int64
	Amount float64
	Rate   float64 // 日利率（小数格式）
	Period int
}

// PendingFundingOffer 代表一个未成交资金贷出订单及其追踪状态
type PendingFundingOffer struct {
	FundingOffer
	IsTracked bool
}

// FundingOfferCancelSummary 代表取消未成交订单的结果统计
type FundingOfferCancelSummary struct {
	Total     int
	Cancelled int
	Skipped   int
	Failed    int
}

// Wallet 代表钱包信息
type Wallet struct {
	Currency  string
	Type      string
	Balance   float64
	Available float64
}

// FundingBookEntry 代表资金订单簿条目
type FundingBookEntry struct {
	Rate   float64 // 日利率（小数格式）
	Amount float64
	Period int
	Count  int
}

// FundingCredit 代表活跃的借贷订单
type FundingCredit struct {
	ID         int64
	Symbol     string
	Amount     float64
	RateType   string  // 利率类型（frr/fixed）
	Rate       float64 // 日利率（小数格式）
	RateReal   float64 // 实际日利率（FRR 用）
	Period     int64   // 期间（天）
	MTSCreated int64   // 创建时间戳（毫秒）
	MTSOpened  int64   // 开始时间戳（毫秒）
	Status     string  // 状态
}

// EffectiveDailyRate 返回可用的日利率（FRR 使用 RateReal）
func (fc *FundingCredit) EffectiveDailyRate() float64 {
	if fc == nil {
		return 0
	}
	if strings.EqualFold(fc.RateType, "frr") && fc.RateReal > 0 {
		return fc.RateReal
	}
	if fc.Rate > 0 {
		return fc.Rate
	}
	if fc.RateReal > 0 {
		return fc.RateReal
	}
	return 0
}

// Candle 代表 K 线数据
type Candle struct {
	MTS    int64   // 时间戳（毫秒）
	Open   float64 // 开盘价
	Close  float64 // 收盘价
	High   float64 // 最高价
	Low    float64 // 最低价
	Volume float64 // 成交量
}

// GetFundingOffers 获取未完成的资金贷出订单
func (c *Client) GetFundingOffers(symbol string) ([]*FundingOffer, error) {
	offers, err := c.restClient.Funding.Offers(symbol)
	if err != nil {
		// 处理特殊的空响应错误
		if strings.Contains(err.Error(), "data slice too short for funding offer") {
			return []*FundingOffer{}, nil
		}
		return nil, errors.NewAPIError("failed to get funding offers", err)
	}

	// 处理空响应或无数据的情况
	if offers == nil || offers.Snapshot == nil || len(offers.Snapshot) == 0 {
		return []*FundingOffer{}, nil
	}

	result := make([]*FundingOffer, 0, len(offers.Snapshot))
	for _, offer := range offers.Snapshot {
		// 添加安全检查，防止空数据导致panic
		if offer == nil {
			continue
		}
		result = append(result, &FundingOffer{
			ID:     offer.ID,
			Amount: offer.Amount,
			Rate:   offer.Rate, // API 已返回日利率
			Period: int(offer.Period),
		})
	}

	return result, nil
}

// CancelFundingOffer 取消资金贷出订单
func (c *Client) CancelFundingOffer(offerID int64) error {
	cancelReq := &fundingoffer.CancelRequest{
		ID: offerID,
	}

	_, err := c.restClient.Funding.CancelOffer(cancelReq)
	if err != nil {
		return errors.NewOrderError("failed to cancel funding offer", err)
	}

	return nil
}

// SubmitFundingOffer 提交新的资金贷出订单
func (c *Client) SubmitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool) (int64, error) {
	return c.submitFundingOffer(symbol, amount, dailyRate, period, hidden, constants.OfferTypeLIMIT)
}

// SubmitFundingOfferFRR 提交 FRR 型资金贷出订单（delta = 0）
func (c *Client) SubmitFundingOfferFRR(symbol string, amount float64, period int, hidden bool) (int64, error) {
	return c.submitFundingOffer(symbol, amount, constants.DefaultFRRDelta, period, hidden, constants.OfferTypeFRRDeltaVar)
}

func (c *Client) submitFundingOffer(symbol string, amount float64, dailyRate float64, period int, hidden bool, offerType string) (int64, error) {
	offerReq := &fundingoffer.SubmitRequest{
		Type:   offerType,
		Symbol: symbol,
		Amount: amount,
		Rate:   dailyRate, // v2 API 使用日利率
		Period: int64(period),
		Hidden: hidden,
	}

	resp, err := c.restClient.Funding.SubmitOffer(offerReq)
	if err != nil {
		return 0, errors.NewOrderError("failed to submit funding offer", err)
	}

	// 从 notification 中提取 funding offer 资讯
	if resp.NotifyInfo == nil {
		return 0, errors.NewOrderError("funding offer response contains no order info", nil)
	}

	// 使用反射从结构体中提取 ID 字段
	if orderID, hasID := extractIDFromStruct(resp.NotifyInfo); hasID {
		return orderID, nil
	}

	return 0, errors.NewOrderError("unable to extract order ID from funding offer response", nil)
}

// GetWallets 获取钱包信息
func (c *Client) GetWallets() ([]*Wallet, error) {
	wallets, err := c.restClient.Wallet.Wallet()
	if err != nil {
		return nil, errors.NewAPIError("failed to get wallets", err)
	}

	result := make([]*Wallet, 0, len(wallets.Snapshot))
	for _, w := range wallets.Snapshot {
		result = append(result, &Wallet{
			Currency:  w.Currency,
			Type:      w.Type,
			Balance:   w.Balance,
			Available: w.BalanceAvailable,
		})
	}

	return result, nil
}

// GetFundingBalance 获取指定币种的资金钱包余额
func (c *Client) GetFundingBalance(currency string) (float64, error) {
	wallets, err := c.GetWallets()
	if err != nil {
		return 0, err
	}

	for _, wallet := range wallets {
		if wallet.Currency == currency && wallet.Type == constants.WalletTypeFunding {
			return wallet.Available, nil
		}
	}

	return 0, nil
}

// GetFundingBook 获取资金订单簿
func (c *Client) GetFundingBook(symbol string, limit int) ([]*FundingBookEntry, error) {
	if limit > constants.MaxPriceLevels {
		limit = constants.MaxPriceLevels
	}
	if limit <= 0 {
		limit = constants.DefaultPriceLevels
	}

	book, err := c.restClient.Book.All(symbol, common.PrecisionRawBook, limit)
	if err != nil {
		return nil, errors.NewAPIError("failed to get funding book", err)
	}

	if len(book.Snapshot) == 0 {
		return []*FundingBookEntry{}, nil
	}

	result := make([]*FundingBookEntry, 0, len(book.Snapshot))
	for _, entry := range book.Snapshot {
		result = append(result, &FundingBookEntry{
			Rate:   entry.Rate, // API 已返回日利率
			Amount: entry.Amount,
			Period: int(entry.Period),
			Count:  int(entry.Count),
		})
	}

	return result, nil
}

// GetCurrentFundingRate 获取当前资金利率（Flash Return Rate）
func (c *Client) GetCurrentFundingRate(symbol string) (float64, error) {
	// 使用 ticker API 获取真正的当前 funding rate (FRR)
	url := fmt.Sprintf("https://api-pub.bitfinex.com/v2/ticker/%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return 0, errors.NewAPIError("failed to get funding ticker", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, errors.NewAPIError(fmt.Sprintf("API returned status code %d", resp.StatusCode), nil)
	}

	var tickerData []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tickerData); err != nil {
		return 0, errors.NewAPIError("failed to decode ticker response", err)
	}

	// 检查响应数据格式
	if len(tickerData) < 1 {
		return 0, errors.NewAPIError("invalid ticker response format", nil)
	}

	// 对于 funding symbols，FRR (Flash Return Rate) 依官方文件在索引 0
	if frr, ok := tickerData[0].(float64); ok {
		return frr, nil
	}
	// 容错：若索引 0 不可解析，尝试索引 1
	if len(tickerData) > 1 {
		if frr, ok := tickerData[1].(float64); ok {
			return frr, nil
		}
	}

	return 0, errors.NewAPIError("failed to parse FRR from ticker", nil)
}

// GetFundingCredits 获取活跃的借贷订单
func (c *Client) GetFundingCredits(symbol string) ([]*FundingCredit, error) {
	credits, err := c.restClient.Funding.Credits(symbol)
	if err != nil {
		// 处理特殊的空响应错误
		if strings.Contains(err.Error(), "data slice too short") {
			return []*FundingCredit{}, nil
		}
		return nil, errors.NewAPIError("failed to get funding credits", err)
	}

	// 处理空响应或无数据的情况
	if credits == nil || credits.Snapshot == nil || len(credits.Snapshot) == 0 {
		return []*FundingCredit{}, nil
	}

	result := make([]*FundingCredit, 0, len(credits.Snapshot))
	for _, credit := range credits.Snapshot {
		// 添加安全检查，防止空数据导致panic
		if credit == nil {
			continue
		}
		result = append(result, &FundingCredit{
			ID:         credit.ID,
			Symbol:     credit.Symbol,
			Amount:     credit.Amount,
			RateType:   credit.RateType,
			Rate:       credit.Rate, // API 已返回日利率
			RateReal:   credit.RateReal,
			Period:     credit.Period,
			MTSCreated: credit.MTSCreated,
			MTSOpened:  credit.MTSOpened,
			Status:     credit.Status,
		})
	}

	return result, nil
}

// GetFundingCandles 获取资金 K 线数据
func (c *Client) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*Candle, error) {
	// 构建 candle key，格式: trade:15m:fUSD:a30:p2:p30
	candleKey := fmt.Sprintf("trade:%s:%s:a30:p2:p30", timeFrame, symbol)

	// 构建 API URL
	url := fmt.Sprintf("https://api-pub.bitfinex.com/v2/candles/%s/hist?limit=%d", candleKey, limit)

	// 发送 HTTP 请求
	resp, err := http.Get(url)
	if err != nil {
		return nil, errors.NewAPIError("failed to get funding candles", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.NewAPIError(fmt.Sprintf("API returned status code %d", resp.StatusCode), nil)
	}

	// 解析响应
	var rawData [][]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return nil, errors.NewAPIError("failed to decode candles response", err)
	}

	// 转换为 Candle 结构
	candles := make([]*Candle, 0, len(rawData))
	for _, raw := range rawData {
		if len(raw) != 6 {
			continue // 跳过无效数据
		}

		// 安全地转换每个字段
		mts, ok := raw[0].(float64)
		if !ok {
			continue
		}

		open, ok := raw[1].(float64)
		if !ok {
			continue
		}

		close, ok := raw[2].(float64)
		if !ok {
			continue
		}

		high, ok := raw[3].(float64)
		if !ok {
			continue
		}

		low, ok := raw[4].(float64)
		if !ok {
			continue
		}

		volume, ok := raw[5].(float64)
		if !ok {
			continue
		}

		candle := &Candle{
			MTS:    int64(mts),
			Open:   open,
			Close:  close,
			High:   high,
			Low:    low,
			Volume: volume,
		}
		candles = append(candles, candle)
	}

	return candles, nil
}

// extractIDFromStruct 使用反射从结构体中提取ID字段
func extractIDFromStruct(v interface{}) (int64, bool) {
	rv := reflect.ValueOf(v)

	// 如果是指针，获取其指向的值
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return 0, false
		}
		rv = rv.Elem()
	}

	// 必须是结构体
	if rv.Kind() != reflect.Struct {
		return 0, false
	}

	// 尝试查找 ID 字段
	idField := rv.FieldByName("ID")
	if !idField.IsValid() {
		return 0, false
	}

	// 检查字段类型并转换
	switch idField.Kind() {
	case reflect.Int64:
		return idField.Int(), true
	case reflect.Int:
		return int64(idField.Int()), true
	case reflect.Int32:
		return int64(idField.Int()), true
	default:
		return 0, false
	}
}
