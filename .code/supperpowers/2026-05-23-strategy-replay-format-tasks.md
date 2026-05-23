# 策略回放输入格式任务清单

## 目标
- 设计一套最小可用的策略回放输入格式，支持脱离实时 Bitfinex API，直接用历史 Funding Book 与给定资金参数重现策略输出。
- 优先覆盖传统、简单、智能三类 Funding Book 驱动策略；K 线策略后续单独扩展。

## 任务列表
- [x] 盘点当前策略入口与已具备的决策摘要能力
- [x] 确定最小回放输入字段
- [x] 实现回放输入/输出数据结构
- [x] 实现统一策略回放入口
- [x] 为传统/简单/智能策略补回放测试
- [x] 更新 README 与路线图
- [x] 跑全量测试与 race 验证

## 当前设计边界
- 输入至少包含：
  - `strategy`
  - `funds_available`
  - `has_pending_orders`
  - `funding_book`
- 输出至少包含：
  - `offers`
  - `decision_summary`
- 第一阶段不接入 CLI / Telegram / 文件导入命令，只先在代码层提供可复用 API 和测试样例。
- 第一阶段不覆盖 K 线策略，因为其输入依赖 candles，不应和 Funding Book 回放格式混在一起。
