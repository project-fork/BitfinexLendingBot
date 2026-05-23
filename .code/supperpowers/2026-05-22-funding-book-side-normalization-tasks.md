# Funding Book 单边归一化任务清单

## 目标
- 在数据入口层完成 Funding Book 单边归一化，禁止策略直接消费混合两侧的原始 `R0`。
- 保持对现有策略改动尽可能集中，优先降低行为分叉和维护成本。

## 任务列表
- [x] 确认历史结论与当前代码中 Funding Book 的使用方式
- [x] 设计单边过滤规则并确定放置层级
- [x] 实现 Funding Book 单边归一化
- [x] 为 Bitfinex 层补充归一化单元测试
- [x] 为策略层补充混侧输入回归测试
- [x] 跑全量测试与 race 验证

## 当前实现方向
- 在 `internal/bitfinex/client.go` 的 `GetFundingBook()` 返回前统一做单边过滤。
- 默认保留“可贷出挂单侧”，避免策略继续把 `Amount > 0` 和 `Amount < 0` 当成同一个深度梯子。
- 先不重写 `GAP_BOTTOM / GAP_TOP` 语义，这一步只先修正输入数据面。
