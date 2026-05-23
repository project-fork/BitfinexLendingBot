# 智能策略深度表达清理任务清单

## 目标
- 清理智能策略中只用于日志展示、但不直接作为定价输入的伪深度变量
- 保证日志、`LoanOffer.Reason.DepthSource` 与实际定价依据一致，避免误导复盘

## 任务列表
- [x] 复核智能策略中深度索引、采样条目与利率计算之间的关系
- [x] 重构智能策略深度传递方式，使展示口径和定价依据对齐
- [x] 补充智能策略深度表达回归测试
- [x] 更新 README 与路线图
- [x] 跑全量测试与 race 验证

## 当前判断
- `currentDepthIndex` 当前主要用于日志和 `Reason.DepthSource`
- 真正参与定价的是 `sampledEntries + orderIndex`
- 因此需要把“展示的深度来源”绑定到实际传入 `calculateProgressiveRate()` 的采样结果，而不是仅绑定原始索引占位变量
