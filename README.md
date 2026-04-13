# BitfinexLendingBot 綠葉放貸機器人

自動化的 Bitfinex 放貸機器人，支援傳統策略、智能策略、K 線策略、FRR 掛單模式、Telegram 控制與借貸通知。

目前版本：`v2.1.0`

## ✨ 主要功能

- 🔄 **自動放貸**：依市場狀況自動建立放貸訂單
- 🎯 **多策略切換**：支援傳統策略、智能策略、K 線策略
- ⚡ **觸發式執行**：可改為只在新借貸成交或可用餘額顯著變化時重跑策略
- 📈 **FRR 掛單模式**：`MIN_DAILY_LEND_RATE` 可設為 `FRR`
- 🛡️ **訂單追蹤保護**：只取消程式追蹤到的掛單，避免誤取消手動建立的訂單
- 📱 **Telegram 控制台**：可查詢狀態、策略、借貸單與動態調整參數
- 🧪 **測試模式**：可先模擬策略與日誌，再切換正式交易

## 🚀 快速開始

### 安裝與執行

```bash
# 準備設定檔
cp config.yaml.example config.yaml

# 編譯
go build -o bitfinex-lending-bot

# 執行（預設讀取 config.yaml）
./bitfinex-lending-bot

# 指定設定檔
./bitfinex-lending-bot -c config.yaml
```

### 基本配置

```yaml
BITFINEX_API_KEY: "your_api_key_here"
BITFINEX_SECRET_KEY: "your_secret_key_here"
CURRENCY: "USD"
MIN_LOAN: 150
LOAN_DAYS: 0
MIN_DAILY_LEND_RATE: 0.02
```

## 📋 主要配置參數

### 🔑 API 設定

```yaml
BITFINEX_API_KEY: "xxxxxxxxxx"
BITFINEX_SECRET_KEY: "xxxxxxxxxx"
```

### ⚙️ 基本設定

```yaml
CURRENCY: "USD"                  # 放貸幣種
ORDER_LIMIT: 3                   # 單次執行最多建立幾筆訂單
RUN_ONLY_ON_NEW_CREDITS: false   # true 時改為觸發式執行
MINUTES_RUN: 15                  # 定時模式下的主流程間隔（分鐘）
MIN_LOAN: 150                    # 單筆最小貸出金額
MAX_LOAN: 155                    # 單筆最大貸出金額，0 或未設為不限制
LOAN_DAYS: 0                     # 固定借貸天數，0 為自動判斷
RESERVE_AMOUNT: 100              # 保留不參與借貸的資金
LENDING_CHECK_MINUTES: 10        # 借貸檢查間隔（分鐘）
TEST_MODE: true                  # 測試模式
```

### 📈 利率策略設定

```yaml
MIN_DAILY_LEND_RATE: 0.038       # 可設數值或 FRR
SPREAD_LEND: 30                  # 分散單最大目標筆數
GAP_BOTTOM: 10                   # 掛單深度下限
GAP_TOP: 5000                    # 掛單深度上限
THIRTY_DAY_LEND_RATE_THRESHOLD: 0.04
ONE_TWENTY_DAY_LEND_RATE_THRESHOLD: 0.045
RATE_BONUS: 0.002                # 沒有未完成掛單時的利率加成
```

`MIN_DAILY_LEND_RATE: FRR` 時，分散單會使用 FRR 掛單模式；高額持有單仍維持 `HIGH_HOLD_RATE` 固定利率。
`SPREAD_LEND` 是分散單的最大目標筆數，實際筆數還會受到 `ORDER_LIMIT`、高額持有已占用筆數、`MIN_LOAN`、`MAX_LOAN` 與剩餘資金影響。

### 💎 高額持有策略

```yaml
HIGH_HOLD_RATE: 0.1
HIGH_HOLD_AMOUNT: 155
HIGH_HOLD_ORDERS: 1
```

### 🧠 智能策略

```yaml
ENABLE_SMART_STRATEGY: true
VOLATILITY_THRESHOLD: 0.002
MAX_RATE_MULTIPLIER: 2.0
MIN_RATE_MULTIPLIER: 0.8
RATE_RANGE_INCREASE_PERCENT: 0.2
```

### 📊 K 線策略

```yaml
ENABLE_KLINE_STRATEGY: false
KLINE_TIME_FRAME: "15m"
KLINE_PERIOD: 24
KLINE_SPREAD_PERCENT: 0
KLINE_SMOOTH_METHOD: "ema"       # max / sma / ema / hla / p90
```

### 📱 Telegram 設定

```yaml
TELEGRAM_BOT_TOKEN: "xxxxxxxxxx"
TELEGRAM_AUTH_TOKEN: "your_auth"
NOTIFY_RATE_THRESHOLD: 0.1
```

## 🎯 策略與執行模式

### 策略優先級

1. **K 線策略**
2. **智能策略**
3. **傳統策略**

### 定時模式

- `RUN_ONLY_ON_NEW_CREDITS: false`
- 依 `MINUTES_RUN` 週期性重跑主流程

### 觸發模式

- `RUN_ONLY_ON_NEW_CREDITS: true`
- 啟動時先執行一次初始化
- 後續只有在下列條件成立時才重跑主流程：
  - 發現新的借貸成交
  - 可用餘額顯著增加

### 訂單追蹤與安全性

- 主流程只會取消程式本次執行期間追蹤到的未完成訂單
- 手動建立、未被追蹤到的掛單不會被自動取消
- `/restart` 會重新執行策略，但同樣只處理程式追蹤到的訂單

## 📱 Telegram 指令

### 驗證

```text
/auth                              - 開始驗證流程
```

### 查詢

```text
/rate                              - 顯示當前貸出利率和閾值
/check                             - 檢查利率是否超過閾值
/status                            - 顯示系統狀態
/strategy                          - 顯示目前策略與優先級
/lending                           - 查看活躍借貸訂單
```

### 參數調整

```text
/threshold [數值]                  - 設定利率通知閾值
/reserve [數值]                    - 設定保留金額
/orderlimit [數值]                 - 設定單次執行下單上限
/loandays [數值]                   - 設定固定借貸天數（0 為自動）
/mindailylendrate [數值|FRR]       - 設定最低日利率或 FRR 模式
/minloan [數值]                    - 設定單筆最小貸出金額
/maxloan [數值]                    - 設定單筆最大貸出金額（0 為不限制）
/highholdrate [數值]               - 設定高額持有利率
/highholdamount [數值]             - 設定高額持有金額（0 為關閉）
/highholdorders [數值]             - 設定高額持有訂單數
/raterangeincrease [數值]          - 設定利率範圍增加百分比
/smoothmethod [方法]               - 設定 K 線平滑方法
```

### 策略切換

```text
/klinestrategy on/off              - 切換 K 線策略
/smartstrategy on/off              - 切換智能策略
```

### 控制

```text
/restart                           - 重新執行策略
/help                              - 顯示指令說明
```

## 📊 調度器架構

應用程式包含三個獨立調度器：

1. **主要任務**
   - 定時模式下依 `MINUTES_RUN` 執行
   - 觸發模式下只在初始化與觸發條件成立時執行

2. **借貸檢查**
   - 依 `LENDING_CHECK_MINUTES` 檢查新借貸成交
   - 追蹤可用餘額變化
   - 發送 Telegram 借貸通知

3. **每小時利率檢查**
   - 使用最近 12 根 5 分鐘 K 線高點檢查利率閾值
   - 超過閾值時發送 Telegram 通知

## ⚠️ 注意事項

1. 需要 Bitfinex API 交易權限。
2. 首次使用建議先開啟 `TEST_MODE: true`。
3. FRR 模式只影響分散單，高額持有單仍使用固定利率。
4. 觸發模式不會依 `MINUTES_RUN` 定時重跑。
5. 建議定期檢查 Telegram 狀態與借貸單內容。

## 📚 相關文件

- [借貸通知功能說明](LENDING_NOTIFICATION.md)
- [智能策略詳細說明](SMART_STRATEGY.md)
- [K 線策略範例設定](kline_strategy_example.yaml)

## 🆕 更新日誌

### v2.1.0

- ✨ 新增 `RUN_ONLY_ON_NEW_CREDITS`，可依新借貸成交或餘額變化觸發主流程
- 🛡️ 新增訂單追蹤機制，取消掛單時只處理程式追蹤到的訂單
- 📈 `MIN_DAILY_LEND_RATE` 新增 `FRR` 模式，分散單可使用 FRR 掛單
- 🔧 修正 FRR 借貸單的利率顯示、收益計算與通知內容
- 📱 更新 Telegram `/status`、`/lending`、`/mindailylendrate` 對 FRR 的支援

### v2.0.3

- 🔧 調整利率檢查邏輯與 API 取值

### v2.0.2

- ⚙️ 新增 `/minloan` 與 `/maxloan` 指令

### v2.0.1

- 🚀 初始版本釋出
