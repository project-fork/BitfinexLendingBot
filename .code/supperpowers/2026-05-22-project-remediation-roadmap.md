# BitfinexLendingBot 修复与演进路线图

> 面向后续执行的任务拆解文档。优先处理会影响真实下单正确性、并发安全性和运行稳定性的事项，再处理可维护性与增强功能。

## 总目标
- 先消除“可能下错单 / 重复下单 / 运行中参数失控”这三类高风险问题。
- 再统一策略语义与配置治理，提升系统可预测性与可回放性。
- 最后补足运维友好性、可观测性与长期扩展能力。

## 优先级总览

### P0 立即处理
- 主任务串行化，避免 `/run`、`/restart`、定时调度、借贷触发并发执行。
- Funding Book 单边语义归一化，避免混用原始订单簿两侧数据。
- 统一 `GAP_BOTTOM / GAP_TOP` 语义，确保日志、策略、配置含义一致。
- 运行时配置更新收口，避免 Telegram 命令直接改共享配置对象。

### P1 尽快处理
- 运行时配置持久化，保证机器人重启后参数不回退。
- [x] Telegram 依赖关系梳理，明确“必需启用”还是“可选禁用”。
- 外部请求超时、重试、降级策略补齐。
- 为关键策略增加回放测试与集成测试。

### P2 后续增强
- 增加策略决策快照与解释能力。
- 增加运行期风控阈值、冷却时间和幂等保护。
- 增加策略模拟/回测输入输出格式，便于长期优化。

## 第一阶段：执行安全与配置治理

### 目标
- 同一时刻只允许一个主策略执行实例。
- 所有运行时改参都经过统一校验、统一写入入口。

### 涉及文件
- 修改 `main.go`
- 修改 `internal/telegram/bot.go`
- 修改 `internal/telegram/handlers.go`
- 修改 `internal/config/config.go`
- 新增 `internal/config/runtime_config.go`
- 新增 `internal/config/runtime_config_test.go`

### 任务清单
- [x] 为主任务执行增加单飞保护，明确“已有任务执行中”时的新请求处理策略。
- [x] 统一 `/run`、`/restart`、定时调度、借贷触发调度的执行入口。
- [x] 从 `telegram handlers` 中移除对 `b.config` 的分散直接赋值。
- [x] 建立运行时配置服务，负责参数解析、业务约束校验、原子更新。
- [x] 明确哪些参数允许运行时修改，哪些参数只能启动时读取。
- [x] 为运行时配置服务补充单元测试，覆盖边界值与非法输入。
- [x] 为并发执行入口补充测试，验证重复触发不会并发下单。

### 验证建议
- 运行 `go test ./internal/config ./internal/telegram ./internal/strategy`
- 运行 `go test -race ./...`
- 人工验证同时触发 `/run` 与定时任务时，只执行一轮主策略

## 第二阶段：策略语义校正

### 目标
- 让 Funding Book 的解释方式与 Bitfinex 数据语义一致。
- 让传统策略、简单策略、智能策略在“深度”和“利率来源”上保持一致的业务定义。

### 涉及文件
- 修改 `internal/bitfinex/client.go`
- 修改 `internal/strategy/lending.go`
- 修改 `internal/strategy/simple_strategy.go`
- 修改 `internal/strategy/smart_strategy.go`
- 修改 `internal/strategy/strategy_helpers.go`
- 修改 `internal/strategy/market_analyzer.go`
- 新增或扩展 `internal/strategy/*_test.go`
- 修改 `README.md`
- 修改 `config.yaml.example`

### 任务清单
- [x] 先确认 Funding Book 原始返回数据的单边判定规则，沉淀为代码注释和测试样例。
- [x] 在 API 层或策略入口层完成单边过滤/归一化，禁止混合两侧数据直接参与策略。
- [x] 明确定义 `GAP_BOTTOM / GAP_TOP` 是“数组索引”、“累计量深度”还是“价格阶梯区间”。
- [x] 按统一定义重写传统分散挂单的深度推进算法。
- [x] 清理智能策略中只写日志不参与定价的伪深度变量，避免误导。
- [x] 统一 `calculateProgressiveRate()`、市场竞争分析、趋势分析的输入前提。
- [x] 用固定 Funding Book 样本为传统/简单/智能策略补回放测试。
- [x] 更新 README 与示例配置，避免用户继续按旧语义配置参数。

## 当前进展补充
- [x] 运行时配置持久化已完成，Telegram 改参会写入 `data.json` 并在重启后恢复。
- [x] `tracked_orders`、Telegram 鉴权信息、运行时配置已统一走同一持久化文件并保持兼容。
- [x] Telegram 当前已定义为可选模块：缺少 token 或初始化失败时会自动降级，不阻断主策略执行。
- [x] Bitfinex 关键市场数据请求已统一 timeout 和错误分类；关键定价输入失败时会中止本轮下单。
- [x] 启动期自检日志已补齐，会输出关键运行状态、降级状态与风险提示，同时避免泄露敏感配置。
- [x] 每次策略执行的结构化决策摘要已补齐，后续可直接复用于 Telegram 诊断或历史决策快照。
- [x] “为什么这样下单”的关键解释字段已补齐，可直接追踪资金来源、深度来源、利率来源、期限来源和执行层决策来源。
- [x] 基础运行期风控已补齐，包含执行冷却时间、最小执行资金阈值和短窗口订单幂等保护。
- [x] 第 2 批运行期风控增强已补齐：人工触发默认豁免执行冷却，且最近一次策略决策摘要会结构化记录触发来源、冷却豁免与跳过原因。
- [x] Funding Book 单边归一化、传统深度推进重写、simple/smart 输入前提统一、智能策略深度表达清理与最小策略回放格式已全部落地，当前剩余主要是任务文档与后续增强项同步。

### 验证建议
- 用构造的单边 Funding Book 样本跑策略测试，验证每笔订单的金额、利率、期限。
- 对比修复前后日志，确认“配置深度”与“实际定价依据”一致。

## 第三阶段：稳定性与外部依赖治理

### 目标
- 提高网络异常、API 抖动、Telegram 配置不完整时的容错能力。

### 涉及文件
- 修改 `internal/bitfinex/client.go`
- 修改 `main.go`
- 修改 `internal/config/config.go`
- 修改 `README.md`
- 可选新增 `internal/bitfinex/client_test.go`

### 任务清单
- [x] 为 `GetCurrentFundingRate()` 等直接网络请求增加 timeout 和更清晰的错误分类。
- [x] 明确 Telegram 是启动必需项还是可选模块，并统一到配置校验层。
- [x] 为 API 失败场景定义降级策略，区分“允许 fallback”与“必须中止下单”。
- [x] 补充启动期自检日志，启动时直接输出关键配置状态与风险提示。
- [x] 为关键 API 失败路径增加测试或 stub 验证。

### 验证建议
- 人工模拟无 Telegram Token、Funding Book 获取失败、Ticker 超时等场景。
- 确认错误信息能明确告诉使用者“还能运行什么、不能运行什么”。

## 第四阶段：可观测性与策略演进

### 目标
- 让每次执行都可解释、可追溯、可复盘。

### 涉及文件
- 修改 `internal/strategy/lending.go`
- 修改 `internal/strategy/simple_strategy.go`
- 修改 `internal/strategy/smart_strategy.go`
- 修改 `internal/telegram/handlers.go`
- 新增 `docs` 或 `.code/supperpowers` 下的样例说明文档

### 任务清单
- [x] 为每次策略执行生成结构化决策摘要。
- [x] 增加“为什么这样下单”的关键字段输出，如资金来源、深度来源、利率来源、期限来源。
- [x] 设计可回放输入格式，支持拿历史 Funding Book 快速重现策略结果。
- [x] 为 Telegram 增加只读诊断指令，如当前配置摘要、最近一次策略决策摘要。

## 建议执行顺序
1. 第一阶段：执行安全与配置治理
2. 第二阶段：策略语义校正
3. 第三阶段：稳定性与外部依赖治理
4. 第四阶段：可观测性与策略演进

## 我建议的首个实施批次
- 批次 1A：主任务串行化 + `/run` `/restart` 并发保护
- 批次 1B：运行时配置服务收口 + Telegram 改参统一校验
- 批次 2A：Funding Book 单边归一化
- 批次 2B：`GAP_BOTTOM / GAP_TOP` 语义统一与策略测试回补

## 当前判断
- 如果现在直接开始改策略而不先做串行化和配置治理，后续验证会非常混乱。
- 如果只修并发，不修 Funding Book 语义，机器人仍可能“稳定地下错单”。
- 所以最佳顺序不是“先挑最容易的改”，而是“先把执行面收紧，再修策略正确性”。
