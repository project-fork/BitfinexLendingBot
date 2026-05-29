package bitfinex

import (
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/common"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/fundingcredit"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/fundingoffer"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/ledger"
	"github.com/bitfinexcom/bitfinex-api-go/pkg/models/wallet"
	"github.com/bitfinexcom/bitfinex-api-go/v2/rest"

	"github.com/kfrico/BitfinexLendingBot/internal/constants"
	"github.com/kfrico/BitfinexLendingBot/internal/errors"
)

const (
	bitfinexPublicAPIBaseURL = "https://api-pub.bitfinex.com/v2/"
	bitfinexRequestTimeout   = 15 * time.Second
	privateReadMaxAttempts   = 2
	privateReadRetryDelay    = 300 * time.Millisecond
)

// Client Bitfinex API 客户端封装
type Client struct {
	restClient *rest.Client
	httpClient *http.Client
	baseURL    string
}

// NewClient 创建新的 Bitfinex 客户端
func NewClient(apiKey, secretKey string) *Client {
	httpClient := &http.Client{Timeout: bitfinexRequestTimeout}
	nonceGenerator, err := newPersistentNonceGenerator(defaultNonceStateFilePath())
	if err != nil {
		nonceGenerator = nil
	}

	httpDo := func(c *http.Client, req *http.Request) (*http.Response, error) {
		return httpClient.Do(req)
	}

	client := rest.NewClientWithURLHttpDo(bitfinexPublicAPIBaseURL, httpDo)
	if nonceGenerator != nil {
		client = rest.NewClientWithURLHttpDoNonce(bitfinexPublicAPIBaseURL, httpDo, nonceGenerator)
	}
	client = client.Credentials(apiKey, secretKey)

	if nonceGenerator == nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: failed to initialize persistent nonce generator, falling back to default nonce behavior\n")
	}

	return &Client{
		restClient: client,
		httpClient: httpClient,
		baseURL:    bitfinexPublicAPIBaseURL,
	}
}

// FundingOffer 代表一个资金贷出订单
type FundingOffer struct {
	ID         int64
	Amount     float64
	Rate       float64 // 日利率（小数格式）
	Period     int
	Type       string
	MTSCreated int64
	MTSUpdated int64
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
	MTSUpdated int64   // 更新时间戳（毫秒）
	MTSOpened  int64   // 开始时间戳（毫秒）
	Status     string  // 状态
}

// LedgerEntry 代表历史账本条目
type LedgerEntry struct {
	ID          int64
	Currency    string
	MTS         int64
	Amount      float64
	Balance     float64
	Description string
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
		return nil, classifyBitfinexError("failed to get funding offers", err)
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
			ID:         offer.ID,
			Amount:     offer.Amount,
			Rate:       offer.Rate, // API 已返回日利率
			Period:     int(offer.Period),
			Type:       offer.Type,
			MTSCreated: offer.MTSCreated,
			MTSUpdated: offer.MTSUpdated,
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
	var wallets *wallet.Snapshot
	err := c.retryPrivateRead("get wallets", func() error {
		var callErr error
		wallets, callErr = c.restClient.Wallet.Wallet()
		return callErr
	})
	if err != nil {
		return nil, err
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
		return nil, classifyBitfinexError("failed to get funding book", err)
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

	return normalizeFundingBookEntries(result), nil
}

func normalizeFundingBookEntries(entries []*FundingBookEntry) []*FundingBookEntry {
	if len(entries) == 0 {
		return []*FundingBookEntry{}
	}

	askSide := make([]*FundingBookEntry, 0, len(entries))
	for _, entry := range entries {
		if entry == nil {
			continue
		}
		// Bitfinex funding raw book 中，Amount > 0 表示 ask 侧，机器人应只消费可贷出挂单侧。
		if entry.Amount > 0 {
			askSide = append(askSide, entry)
		}
	}

	if len(askSide) == 0 {
		askSide = append(askSide, entries...)
	}

	sort.SliceStable(askSide, func(i, j int) bool {
		if askSide[i] == nil {
			return false
		}
		if askSide[j] == nil {
			return true
		}
		if askSide[i].Rate == askSide[j].Rate {
			return askSide[i].Amount < askSide[j].Amount
		}
		return askSide[i].Rate < askSide[j].Rate
	})

	return askSide
}

// GetCurrentFundingRate 获取当前资金利率（Flash Return Rate）
func (c *Client) GetCurrentFundingRate(symbol string) (float64, error) {
	resp, err := c.getPublic("ticker/"+symbol, nil)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	var tickerData []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&tickerData); err != nil {
		return 0, errors.NewAPIDecodeError("failed to decode ticker response", err)
	}

	if len(tickerData) < 1 {
		return 0, errors.NewAPIDecodeError("invalid ticker response format", nil)
	}

	if frr, ok := tickerData[0].(float64); ok {
		return frr, nil
	}
	if len(tickerData) > 1 {
		if frr, ok := tickerData[1].(float64); ok {
			return frr, nil
		}
	}

	return 0, errors.NewAPIDecodeError("failed to parse FRR from ticker response", nil)
}

// GetFundingCredits 获取活跃的借贷订单
func (c *Client) GetFundingCredits(symbol string) ([]*FundingCredit, error) {
	var credits *fundingcredit.Snapshot
	err := c.retryPrivateRead("get funding credits", func() error {
		var callErr error
		credits, callErr = c.restClient.Funding.Credits(symbol)
		return callErr
	})
	if err != nil {
		return nil, err
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
			MTSUpdated: credit.MTSUpdated,
			MTSOpened:  credit.MTSOpened,
			Status:     credit.Status,
		})
	}

	return result, nil
}

// GetFundingCreditsHistory 获取历史借贷订单
func (c *Client) GetFundingCreditsHistory(symbol string) ([]*FundingCredit, error) {
	var credits *fundingcredit.Snapshot
	err := c.retryPrivateRead("get funding credits history", func() error {
		var callErr error
		credits, callErr = c.restClient.Funding.CreditsHistory(symbol)
		return callErr
	})
	if err != nil {
		return nil, err
	}

	if credits == nil || credits.Snapshot == nil || len(credits.Snapshot) == 0 {
		return []*FundingCredit{}, nil
	}

	result := make([]*FundingCredit, 0, len(credits.Snapshot))
	for _, credit := range credits.Snapshot {
		if credit == nil {
			continue
		}
		result = append(result, &FundingCredit{
			ID:         credit.ID,
			Symbol:     credit.Symbol,
			Amount:     credit.Amount,
			RateType:   credit.RateType,
			Rate:       credit.Rate,
			RateReal:   credit.RateReal,
			Period:     credit.Period,
			MTSCreated: credit.MTSCreated,
			MTSUpdated: credit.MTSUpdated,
			MTSOpened:  credit.MTSOpened,
			Status:     credit.Status,
		})
	}

	return result, nil
}

// GetLedgers 获取历史账本条目
func (c *Client) GetLedgers(currency string, start int64, end int64, max int32) ([]*LedgerEntry, error) {
	var ledgers *ledger.Snapshot
	err := c.retryPrivateRead("get ledgers", func() error {
		var callErr error
		ledgers, callErr = c.restClient.Ledgers.Ledgers(currency, start, end, max)
		return callErr
	})
	if err != nil {
		return nil, err
	}

	if ledgers == nil || ledgers.Snapshot == nil || len(ledgers.Snapshot) == 0 {
		return []*LedgerEntry{}, nil
	}

	result := make([]*LedgerEntry, 0, len(ledgers.Snapshot))
	for _, entry := range ledgers.Snapshot {
		if entry == nil {
			continue
		}
		result = append(result, &LedgerEntry{
			ID:          entry.ID,
			Currency:    entry.Currency,
			MTS:         entry.MTS,
			Amount:      entry.Amount,
			Balance:     entry.Balance,
			Description: entry.Description,
		})
	}

	return result, nil
}

// GetLedgersFiltered 获取带 wallet/category 过滤条件的历史账本条目
func (c *Client) GetLedgersFiltered(currency string, start int64, end int64, max int32, wallet string, category int32) ([]*LedgerEntry, error) {
	if max > 2500 {
		return nil, fmt.Errorf("Max request limit:%d, got: %d", 2500, max)
	}

	payload := map[string]interface{}{
		"start":  start,
		"end":    end,
		"limit":  max,
		"wallet": wallet,
	}
	if category != 0 {
		payload["category"] = category
	}

	req, err := c.restClient.NewAuthenticatedRequestWithData(common.PermissionRead, path.Join("ledgers", currency, "hist"), payload)
	if err != nil {
		return nil, err
	}

	var raw []interface{}
	err = c.retryPrivateRead("get filtered ledgers", func() error {
		var callErr error
		raw, callErr = c.restClient.Request(req)
		return callErr
	})
	if err != nil {
		return nil, err
	}

	snapshot, err := ledger.SnapshotFromRaw(raw, ledger.FromRaw)
	if err != nil {
		return nil, classifyBitfinexError("failed to decode filtered ledgers", err)
	}
	if snapshot == nil || snapshot.Snapshot == nil || len(snapshot.Snapshot) == 0 {
		return []*LedgerEntry{}, nil
	}

	result := make([]*LedgerEntry, 0, len(snapshot.Snapshot))
	for _, entry := range snapshot.Snapshot {
		if entry == nil {
			continue
		}
		result = append(result, &LedgerEntry{
			ID:          entry.ID,
			Currency:    entry.Currency,
			MTS:         entry.MTS,
			Amount:      entry.Amount,
			Balance:     entry.Balance,
			Description: entry.Description,
		})
	}

	return result, nil
}

// GetFundingCandles 获取资金 K 线数据
func (c *Client) GetFundingCandles(symbol string, timeFrame string, limit int) ([]*Candle, error) {
	if limit <= 0 {
		return []*Candle{}, nil
	}

	resp, err := c.getPublic("candles/trade:"+timeFrame+":"+symbol+":a30:p2:p30/hist", map[string]string{
		"limit": fmt.Sprintf("%d", limit),
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 解析响应
	var rawData [][]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawData); err != nil {
		return nil, errors.NewAPIDecodeError("failed to decode candles response", err)
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

func (c *Client) getPublic(path string, query map[string]string) (*http.Response, error) {
	baseURL := c.baseURL
	if baseURL == "" {
		baseURL = bitfinexPublicAPIBaseURL
	}

	url := strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.NewAPIError("failed to build public request", err)
	}

	if len(query) > 0 {
		values := req.URL.Query()
		for key, value := range query {
			values.Set(key, value)
		}
		req.URL.RawQuery = values.Encode()
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: bitfinexRequestTimeout}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, classifyBitfinexError("failed to execute public request", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return resp, nil
	}

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return nil, classifyBitfinexHTTPStatusError(path, resp.StatusCode, body)
}

func classifyBitfinexHTTPStatusError(path string, statusCode int, body []byte) error {
	message := fmt.Sprintf("Bitfinex public API %s returned HTTP %d", path, statusCode)
	if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
		message = fmt.Sprintf("%s: %s", message, trimmed)
	}
	if statusCode == http.StatusTooManyRequests {
		return errors.NewRateLimitError(message, nil)
	}
	return errors.NewAPIHTTPStatusError(message, nil)
}

func (c *Client) retryPrivateRead(message string, call func() error) error {
	var lastErr error
	for attempt := 1; attempt <= privateReadMaxAttempts; attempt++ {
		lastErr = call()
		if lastErr == nil {
			return nil
		}
		if strings.Contains(lastErr.Error(), "data slice too short") {
			return nil
		}

		classifiedErr := classifyBitfinexError("failed to "+message, lastErr)
		if !errors.HasCode(classifiedErr, errors.ErrCodeAPITimeout) {
			return classifiedErr
		}
		if attempt == privateReadMaxAttempts {
			return errors.NewAPITimeoutError(
				fmt.Sprintf("failed to %s after %d attempt(s)", message, attempt),
				lastErr,
			)
		}

		time.Sleep(privateReadRetryDelay * time.Duration(attempt))
	}

	return classifyBitfinexError("failed to "+message, lastErr)
}

func classifyBitfinexError(message string, err error) error {
	if err == nil {
		return nil
	}

	var netErr net.Error
	lowerErr := strings.ToLower(err.Error())
	if isBitfinexAuthenticationError(lowerErr) {
		return errors.NewAuthenticationError(message, err)
	}
	if strings.Contains(lowerErr, "rate limit") || strings.Contains(lowerErr, "ratelimit") || strings.Contains(lowerErr, "too many requests") {
		return errors.NewRateLimitError(message, err)
	}
	if stderrors.As(err, &netErr) && netErr.Timeout() {
		return errors.NewAPITimeoutError(message, err)
	}
	if stderrors.As(err, &netErr) && netErr.Temporary() {
		return errors.NewAPIError(message, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "context deadline exceeded") {
		return errors.NewAPITimeoutError(message, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "client.timeout exceeded") {
		return errors.NewAPITimeoutError(message, err)
	}
	var errorResponse *rest.ErrorResponse
	if stderrors.As(err, &errorResponse) {
		if errorResponse.Response != nil && errorResponse.Response.Response != nil && errorResponse.Response.Response.StatusCode == http.StatusTooManyRequests {
			return errors.NewRateLimitError(message, err)
		}
		return errors.NewAPIHTTPStatusError(message, err)
	}
	return errors.NewAPIError(message, err)
}

func isBitfinexAuthenticationError(lowerErr string) bool {
	authMarkers := []string{
		"apikey invalid",
		"api key invalid",
		"digest invalid",
		"nonce: small",
		"invalid x-bfx-apikey",
		"invalid x-bfx-payload",
		"invalid x-bfx-signature",
	}
	for _, marker := range authMarkers {
		if strings.Contains(lowerErr, marker) {
			return true
		}
	}
	return false
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
