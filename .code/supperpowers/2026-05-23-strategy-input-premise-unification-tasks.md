# 策略输入前提统一任务清单

## 目标
- 统一 `calculateProgressiveRate()`、趋势分析、波动率快照与市场分析所使用的 Funding Book 输入前提
- 避免 simple / smart 策略中同时混用“完整 Funding Book”“配置深度区间”“采样条目”三种不同口径

## 任务列表
- [x] 复核 simple / smart / market analyzer 当前的 Funding Book 输入方式
- [x] 提炼统一的配置深度区间视图
- [x] 让 simple / smart 的市场分析、高额持有动态利率、递增利率统一基于该视图
- [x] 补充回归测试并更新 README / roadmap
- [x] 跑全量测试与 race 验证

## 当前实现方向
- 将 `GAP_BOTTOM / GAP_TOP` 对应的连续单边区间视为策略分析输入视图
- simple / smart 的趋势分析、波动率、当前利率快照和递增利率计算统一基于这份视图
- 订单展示层保留“当前订单映射到哪个深度索引”的说明，但不再与分析输入口径脱节
