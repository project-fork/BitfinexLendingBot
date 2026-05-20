# BitfinexLendingBot 绿叶放贷机器人

自动化的 Bitfinex 放贷机器人，支持传统策略、智能策略、K 线策略、FRR 挂单模式、Telegram 控制与借贷通知。

目前版本：`v2.1.0`

## ✨ 主要功能

- 🔄 **自动放贷**：依市场状况自动建立放贷订单
- 🎯 **多策略切换**：支持传统策略、智能策略、K 线策略
- ⚡ **触发式执行**：可改为只在新借贷成交或可用余额显着变化时重跑策略
- 📈 **FRR 挂单模式**：`MIN_DAILY_LEND_RATE` 可设为 `FRR`
- 🛡️ **订单追踪保护**：只取消程序追踪到的挂单，避免误取消手动建立的订单
- 📱 **Telegram 控制台**：可查询状态、策略、借贷单与动态调整参数
- 🧪 **测试模式**：可先模拟策略与日志，再切换正式交易

## 🚀 快速开始

### 安装与执行

```bash
# 准备设定档
cp config.yaml.example config.yaml

# 编译
go build -o bitfinex-lending-bot

# 执行（默认读取 config.yaml）
./bitfinex-lending-bot

# 指定设定档
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

## 📋 主要配置参数

### 🔑 API 设定

```yaml
BITFINEX_API_KEY: "xxxxxxxxxx"
BITFINEX_SECRET_KEY: "xxxxxxxxxx"
```

### ⚙️ 基本设定

```yaml
CURRENCY: "USD"                  # 放贷币种
ORDER_LIMIT: 3                   # 单次执行最多建立几笔订单
RUN_ONLY_ON_NEW_CREDITS: false   # true 时改为触发式执行
MINUTES_RUN: 15                  # 定时模式下的主流程间隔（分钟）
MIN_LOAN: 150                    # 单笔最小贷出金额
MAX_LOAN: 155                    # 单笔最大贷出金额，0 或未设为不限制
LOAN_DAYS: 0                     # 固定借贷天数，0 为自动判断
RESERVE_AMOUNT: 100              # 保留不参与借贷的资金
LENDING_CHECK_MINUTES: 10        # 借贷检查间隔（分钟）
TEST_MODE: true                  # 测试模式
```

### 📈 利率策略设定

```yaml
MIN_DAILY_LEND_RATE: 0.038       # 可设数值或 FRR
SPREAD_LEND: 30                  # 分散单最大目标笔数
GAP_BOTTOM: 10                   # 挂单深度下限
GAP_TOP: 5000                    # 挂单深度上限
THIRTY_DAY_LEND_RATE_THRESHOLD: 0.04
SIXTY_DAY_LEND_RATE_THRESHOLD: 0.042
NINETY_DAY_LEND_RATE_THRESHOLD: 0.044
ONE_TWENTY_DAY_LEND_RATE_THRESHOLD: 0.045
RATE_BONUS: 0.002                # 没有未完成挂单时的利率加成
```

`MIN_DAILY_LEND_RATE: FRR` 时，分散单会使用 FRR 挂单模式；高额持有单仍维持 `HIGH_HOLD_RATE` 固定利率。
`SPREAD_LEND` 是分散单的最大目标笔数，实际笔数还会受到 `ORDER_LIMIT`、高额持有已占用笔数、`MIN_LOAN`、`MAX_LOAN` 与剩余资金影响。

### 💎 高额持有策略

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

### 📊 K 线策略

```yaml
ENABLE_KLINE_STRATEGY: false
KLINE_TIME_FRAME: "15m"
KLINE_PERIOD: 24
KLINE_SPREAD_PERCENT: 0
KLINE_SMOOTH_METHOD: "ema"       # max / sma / ema / hla / p90
```

### 📱 Telegram 设定

```yaml
TELEGRAM_BOT_TOKEN: "xxxxxxxxxx"
TELEGRAM_AUTH_TOKEN: "your_auth"
NOTIFY_RATE_THRESHOLD: 0.1
```

## 🎯 策略与执行模式

### 策略优先级

1. **K 线策略**
2. **智能策略**
3. **传统策略**

### 定时模式

- `RUN_ONLY_ON_NEW_CREDITS: false`
- 依 `MINUTES_RUN` 周期性重跑主流程

### 触发模式

- `RUN_ONLY_ON_NEW_CREDITS: true`
- 启动时先执行一次初始化
- 后续只有在下列条件成立时才重跑主流程：
  - 发现新的借贷成交
  - 可用余额显着增加

### 订单追踪与安全性

- 默认调度执行不会自动取消未成交订单，而是保留现有挂单并继续补单
- `/restart` 会取消程序追踪到的未成交订单后再重新执行策略
- `/run` 会保留现有未成交订单直接重新执行策略
- 手动建立、未被追踪到的挂单不会被自动取消

## 📱 Telegram 指令

### 验证

```text
/auth                              - 开始验证流程
```

### 查询

```text
/rate                              - 显示当前贷出利率和阈值
/check                             - 检查利率是否超过阈值
/status                            - 显示系统状态
/strategy                          - 显示目前策略与优先级
/lending                           - 查看活跃借贷订单
/offers                            - 查看当前未成交订单（含程序追踪标记）
```

### 参数调整

```text
/threshold [数值]                  - 设定利率通知阈值
/reserve [数值]                    - 设定保留金额
/orderlimit [数值]                 - 设定单次执行下单上限
/loandays [数值]                   - 设定固定借贷天数（0 为自动）
/mindailylendrate [数值|FRR]       - 设定最低日利率或 FRR 模式
/minloan [数值]                    - 设定单笔最小贷出金额
/maxloan [数值]                    - 设定单笔最大贷出金额（0 为不限制）
/highholdrate [数值]               - 设定高额持有利率
/highholdamount [数值]             - 设定高额持有金额（0 为关闭）
/highholdorders [数值]             - 设定高额持有订单数
/raterangeincrease [数值]          - 设定利率范围增加百分比
/smoothmethod [方法]               - 设定 K 线平滑方法
```

### 策略切换

```text
/klinestrategy on/off              - 切换 K 线策略
/smartstrategy on/off              - 切换智能策略
```

### 控制

```text
/restart                           - 取消程序追踪到的未成交订单后重新执行策略
/run                               - 重新执行策略并保留现有未成交订单
/canceloffers [all]                - 取消未成交订单，默认仅取消程序追踪订单；加 all 取消全部
/help                              - 显示指令说明
```

## 📊 调度器架构

应用程序包含三个独立调度器：

1. **主要任务**
   - 定时模式下依 `MINUTES_RUN` 执行
   - 触发模式下只在初始化与触发条件成立时执行

2. **借贷检查**
   - 依 `LENDING_CHECK_MINUTES` 检查新借贷成交
   - 追踪可用余额变化
   - 发送 Telegram 借贷通知

3. **每小时利率检查**
   - 使用最近 12 根 5 分钟 K 线高点检查利率阈值
   - 超过阈值时发送 Telegram 通知

## ⚠️ 注意事项

1. 需要 Bitfinex API 交易权限。
2. 首次使用建议先开启 `TEST_MODE: true`。
3. FRR 模式只影响分散单，高额持有单仍使用固定利率。
4. 触发模式不会依 `MINUTES_RUN` 定时重跑。
5. 建议定期检查 Telegram 状态与借贷单内容。

## 📚 相关文件

- [借贷通知功能说明](LENDING_NOTIFICATION.md)
- [智能策略详细说明](SMART_STRATEGY.md)
- [K 线策略示例设定](kline_strategy_example.yaml)

## 🆕 更新日志

### v2.1.0

- ✨ 新增 `RUN_ONLY_ON_NEW_CREDITS`，可依新借贷成交或余额变化触发主流程
- 🛡️ 新增订单追踪机制，取消挂单时只处理程序追踪到的订单
- 📈 `MIN_DAILY_LEND_RATE` 新增 `FRR` 模式，分散单可使用 FRR 挂单
- 🔧 修正 FRR 借贷单的利率显示、收益计算与通知内容
- 📱 更新 Telegram `/status`、`/lending`、`/mindailylendrate` 对 FRR 的支持

### v2.0.3

- 🔧 调整利率检查逻辑与 API 取值

### v2.0.2

- ⚙️ 新增 `/minloan` 与 `/maxloan` 指令

### v2.0.1

- 🚀 初始版本释出
