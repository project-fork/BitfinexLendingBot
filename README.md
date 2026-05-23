# BitfinexLendingBot 绿叶放贷机器人

自动化的 Bitfinex 放贷机器人，支持传统策略、简单策略、智能策略、K 线策略、FRR 挂单模式，以及可选启用的 Telegram 控制与借贷通知。

目前版本：`v2.1.0`

## ✨ 主要功能

- 🔄 **自动放贷**：依市场状况自动建立放贷订单
- 🎯 **多策略切换**：支持传统策略、简单策略、智能策略、K 线策略
- ⚡ **触发式执行**：可改为只在新借贷成交或可用余额显着变化时重跑策略
- 📈 **FRR 挂单模式**：`MIN_DAILY_LEND_RATE` 可设为 `FRR`
- 🛡️ **订单追踪保护**：只取消程序追踪到的挂单，避免误取消手动建立的订单
- 📱 **Telegram 控制台**：可查询状态、策略、借贷单与动态调整参数
- 💾 **运行时参数持久化**：Telegram 改参会写入 `data.json`，重启后自动恢复
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
LOAN_PERIOD_THRESHOLDS:
  30: 0.04
  60: 0.042
  90: 0.044
  120: 0.045
RATE_BONUS: 0.002                # 没有未完成挂单时的利率加成
```

`MIN_DAILY_LEND_RATE: FRR` 时，分散单会使用 FRR 挂单模式；高额持有单仍维持 `HIGH_HOLD_RATE` 固定利率。
`SPREAD_LEND` 是分散单的最大目标笔数，实际笔数还会受到 `ORDER_LIMIT`、高额持有已占用笔数、`MIN_LOAN`、`MAX_LOAN` 与剩余资金影响。
`GAP_BOTTOM` / `GAP_TOP` 现在统一表示 Funding Book 单边 ask 档位的索引范围，不再按混合双边原始 `R0` 深度理解。

### 💎 高额持有策略

```yaml
HIGH_HOLD_RATE: 0.1
HIGH_HOLD_AMOUNT: 155
HIGH_HOLD_ORDERS: 1
```

### 🧠 智能策略

```yaml
STRATEGY: "smart"                # traditional / simple / smart / kline
VOLATILITY_THRESHOLD: 0.002
MAX_RATE_MULTIPLIER: 2.0
MIN_RATE_MULTIPLIER: 0.8
RATE_RANGE_INCREASE_PERCENT: 0.2
```

`STRATEGY: "simple"` 会启用“高额持有优先 + 剩余资金补单”的执行兼容策略；`STRATEGY: "smart"` 则恢复为以资金配比为核心的自适应智能策略。

### 🛡️ 运行期风控

```yaml
EXECUTION_COOLDOWN_SECONDS: 30
MIN_EXECUTABLE_FUNDS: 0
ORDER_FINGERPRINT_TTL_SECONDS: 120
```

- `EXECUTION_COOLDOWN_SECONDS`：限制主策略两次执行之间的最短间隔，防止短时间重复触发
- `MIN_EXECUTABLE_FUNDS`：若本轮可用资金或生成订单总额低于该阈值，则直接跳过
- `ORDER_FINGERPRINT_TTL_SECONDS`：对相同金额/利率/期限/类型的订单启用短窗口幂等保护，防止近似重复下单
- Telegram `/run` 与 `/restart` 这类人工触发默认会豁免执行冷却时间；自动调度与自动触发仍会受冷却保护

### 📊 K 线策略

```yaml
STRATEGY: "kline"
KLINE_TIME_FRAME: "15m"
KLINE_PERIOD: 24
KLINE_SPREAD_PERCENT: 0
KLINE_SMOOTH_METHOD: "ema"       # max / sma / ema / hla / p90
```

### 📱 Telegram 设定（可选）

```yaml
TELEGRAM_BOT_TOKEN: "xxxxxxxxxx"
TELEGRAM_AUTH_TOKEN: "your_auth"
NOTIFY_RATE_THRESHOLD: 0.1
```

同时配置 `TELEGRAM_BOT_TOKEN` 和 `TELEGRAM_AUTH_TOKEN` 时，Telegram 控制台与通知功能才会启用。若任一缺失，程序会自动降级为“无 Telegram 模式”，但主策略、借贷检查与利率检查仍会继续运行。

Telegram 启用后，通过 `/threshold`、`/reserve`、`/orderlimit`、`/loandays`、`/mindailylendrate`、`/minloan`、`/maxloan`、`/highholdrate`、`/highholdamount`、`/highholdorders`、`/raterangeincrease`、`/smoothmethod` 与策略切换指令修改的运行时参数，会持久化到程序目录下的 `data.json`，重启后优先恢复最近一次修改值。

当前明确允许运行时修改的参数包括：
- `NOTIFY_RATE_THRESHOLD`
- `RESERVE_AMOUNT`
- `ORDER_LIMIT`
- `LOAN_DAYS`
- `MIN_DAILY_LEND_RATE`
- `MIN_LOAN`
- `MAX_LOAN`
- `HIGH_HOLD_RATE`
- `HIGH_HOLD_AMOUNT`
- `HIGH_HOLD_ORDERS`
- `RATE_RANGE_INCREASE_PERCENT`
- `STRATEGY`
- `KLINE_SMOOTH_METHOD`

以下参数当前定义为仅启动时读取，不建议通过运行期控制面变更：
- API 凭证与 Telegram 凭证
- `CURRENCY`
- 调度相关参数：`RUN_ONLY_ON_NEW_CREDITS`、`MINUTES_RUN`、`LENDING_CHECK_MINUTES`
- Funding Book 深度边界与定价基础参数：`GAP_BOTTOM`、`GAP_TOP`、`FUNDING_BOOK_RATE_UNDERCUT`
- K 线主输入参数：`KLINE_TIME_FRAME`、`KLINE_PERIOD`、`KLINE_SPREAD_PERCENT`
- 分析基线参数：`LOAN_PERIOD_THRESHOLDS`、`VOLATILITY_THRESHOLD`、`MAX_RATE_MULTIPLIER`、`MIN_RATE_MULTIPLIER`
- 输出与运行模式参数：`NOTIFICATION_FORMAT`、`TEST_MODE`

## 🎯 策略与执行模式

### 策略优先级

- `STRATEGY: "kline"`：K 线策略
- `STRATEGY: "simple"`：简单策略
- `STRATEGY: "smart"`：智能策略
- `STRATEGY: "traditional"`：传统策略

### Funding Book 语义

- Funding Book 在入口层会先做单边归一化，默认只使用 `Amount > 0` 的可贷出 ask 侧参与策略
- `GAP_BOTTOM` / `GAP_TOP` 仅表示这个单边 ask 列表里的索引范围
- 传统策略的分散挂单会从 `GAP_BOTTOM` 开始，在这个索引区间内按确定性深度推进，最后一笔锚定到 `GAP_TOP` 或当前可用上界
- 智能策略的深度来源说明现在会直接绑定到实际采样索引，避免日志里出现“看起来读取了某个深度、但定价实际引用的是另一组采样结果”的误导
- simple / smart 的趋势分析、波动率快照、市场竞争分析和递增利率计算现在统一基于 `GAP_BOTTOM ~ GAP_TOP` 对应的单边区间视图，不再混用整本 Funding Book 与采样子集
- 如果 API 返回异常导致单边过滤后为空，会暂时回退到原始 entries，避免直接因为空簿而中断

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

### Telegram 降级行为

- 未配置 Telegram 时：主程序正常启动，放贷策略、借贷检查、利率检查继续运行
- 未配置 Telegram 时：Telegram 控制指令、主动通知、运行时远程改参不可用
- Telegram 已配置但初始化失败时：程序会记录告警并自动降级为无 Telegram 模式，不阻断主流程

### Bitfinex 请求失败边界

- `Funding Book`、`K 线 Candles` 属于关键定价输入；获取失败时，本轮相关策略会中止，不再静默退化后继续下单
- `GetCurrentFundingRate` 这类主要用于展示、通知补值或 FRR 展示的请求，失败时只会降级显示并记录日志，不影响主流程继续运行
- Bitfinex 公共行情请求现在统一带超时，并区分 `timeout`、`rate limit`、`HTTP status`、`decode` 等错误类型，便于日志定位

### 启动期自检日志

- 程序启动时会输出一段自检摘要，包含 Funding Symbol、当前策略、执行模式、最低日利率、借贷天数、Telegram 状态、通知格式
- 自检日志会额外提示当前关键风险，例如正式模式运行、Telegram 已禁用、`ORDER_LIMIT=0`、FRR 模式、高额持有策略关闭、关键市场数据失败会中止下单
- 自检摘要不会打印 Telegram token、Bitfinex API key 等敏感信息

### 策略决策摘要

- 每次主策略执行结束后，都会输出一条统一的“策略决策摘要”日志
- 摘要会包含本轮策略类型、可用资金、Funding Book 输入来源、生成订单数量、实际尝试/成功/跳过/失败统计、FRR/固定利率订单数量、RATE_BONUS 应用次数、利率范围、金额范围、期限集合和关键备注
- 摘要还会补充“为什么这样下单”的关键解释字段，包括资金来源、深度来源、利率来源、期限来源和执行层决策来源
- 这份摘要目前先输出到日志，后续可直接复用于 Telegram 诊断指令或历史决策快照

### 策略回放输入格式

- 现在代码层已提供最小可用的 Funding Book 回放输入格式，可用于在测试或后续工具中重现传统、简单、智能策略输出
- 回放输入字段最少包含：
  - `strategy`
  - `funds_available`
  - `has_pending_orders`
  - `funding_book`
- 回放输出至少包含：
  - `offers`
  - `decision_summary`
- 当前版本只覆盖 Funding Book 驱动策略，不包含 K 线策略；K 线策略后续需单独补 candle 输入格式
- 回放模式会复用现有 Funding Book 单边归一化规则，因此 mixed-side 原始输入也会先收敛到可贷出侧再计算

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
/configsummary                     - 显示当前运行配置摘要（不含敏感信息）
/decisionsummary                   - 显示最近一次策略决策摘要
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
/simplestrategy on/off             - 切换简单策略
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
