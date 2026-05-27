# 每日收益报告 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现每日 `09:35` 自动发送 Telegram 收益报告，包含昨日预估收益、昨日实际到账收益，以及周/月/年汇总。

**Architecture:** 采用独立收益报告服务负责数据查询与文本组装，调度器负责按北京时间触发，`data.json` 负责去重。数据层分别接入 `Funding Credits History` 与 `Ledgers`，并复用现有 Telegram 发送链路和主任务互斥锁，避免私有 API 并发导致 nonce 冲突。

**Tech Stack:** Go, Bitfinex v2 REST SDK, existing Telegram bot, existing storage/data.json, time/date handling.

---

### Task 1: 扩展持久化状态，记录收益报告去重信息

**Files:**
- Modify: `internal/storage/data_file.go`
- Test: `internal/storage/*_test.go`（如需新增）

- [x] **Step 1: Write the failing test**

```go
func TestDataFilePersistsDailyEarningsState(t *testing.T) {
    // round-trip last_report_date/last_report_at
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage -run TestDataFilePersistsDailyEarningsState -v`
Expected: fail because fields do not exist yet.

- [x] **Step 3: Write minimal implementation**

Add a `DailyEarningsData` struct to `Data` and persist `last_report_date` / `last_report_at`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage -run TestDataFilePersistsDailyEarningsState -v`
Expected: PASS

### Task 2: 接入 Bitfinex 历史接口

**Files:**
- Modify: `internal/bitfinex/client.go`
- Test: `internal/bitfinex/*_test.go`（如需新增）

- [x] **Step 1: Write the failing test**

```go
func TestClientExposesFundingCreditsHistoryAndLedgers(t *testing.T) {
    // verify wrapper methods exist and call the right sdk methods
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bitfinex -run TestClientExposesFundingCreditsHistoryAndLedgers -v`
Expected: fail because wrappers do not exist yet.

- [x] **Step 3: Write minimal implementation**

Expose helper methods for `Funding Credits History` and `Ledgers` on the project client layer.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/bitfinex -run TestClientExposesFundingCreditsHistoryAndLedgers -v`
Expected: PASS

### Task 3: 实现收益报告计算服务

**Files:**
- Create: `internal/report/daily_earnings.go`
- Create: `internal/report/daily_earnings_test.go`
- Modify: `main.go`

- [x] **Step 1: Write the failing test**

```go
func TestDailyEarningsReportBuildsSummary(t *testing.T) {
    // cover yesterday estimate + ledger summary formatting
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/report -run TestDailyEarningsReportBuildsSummary -v`
Expected: fail because package does not exist yet.

- [x] **Step 3: Write minimal implementation**

Implement:
- yesterday 预估收益计算
- yesterday 实际到账收益汇总
- 7天 / 本周 / 本月 / 本年汇总
- Telegram 文本拼装

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/report -run TestDailyEarningsReportBuildsSummary -v`
Expected: PASS

- [x] **Step 5: 修正昨日结束 credit 的语义细节**

补充并通过以下回归测试：
- `TestSelectCreditsForYesterdayEstimate_ExcludesYesterdayUpdatedButStillActiveHistoryCredits`
- `TestDailyEarningsReportBuildsSummary_UsesUpdatedTimeForClosedCreditEstimate`

修正内容：
- history credit 只有在关闭状态且 `MTSUpdated` 落在昨天窗口时才参与昨日预估
- 已结束 credit 的昨日活跃时长按真实结束时间 `MTSUpdated` 计算

### Task 4: 增加每日调度器与主任务互斥

**Files:**
- Modify: `main.go`
- Test: `main_test.go` 或 `internal/..._test.go`

- [x] **Step 1: Write the failing test**

```go
func TestDailyEarningsSchedulerComputesNext0935(t *testing.T) {
    // verify trigger time in server timezone
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./... -run TestDailyEarningsSchedulerComputesNext0935 -v`
Expected: fail because scheduler does not exist yet.

- [x] **Step 3: Write minimal implementation**

Add a daily scheduler and a `dailyEarningsRunning` guard that shares `mainTaskMu`.

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./... -run TestDailyEarningsSchedulerComputesNext0935 -v`
Expected: PASS

### Task 5: 完成消息发送、去重和验证

**Files:**
- Modify: `main.go`
- Modify: `internal/storage/data_file.go`
- Modify: `internal/telegram/bot.go`（如需复用发送格式）
- Test: `go test ./...`

- [x] **Step 1: Wire daily earnings report send path**
- [x] **Step 2: Persist success date and skip duplicates**
- [x] **Step 3: Verify `wallet=funding` + `category` filtering behavior**
- [x] **Step 4: Run full tests**

Run: `go test ./...`
Expected: PASS

- [x] **Step 5: 修正 Telegram 未启用时的去重状态**

补充并通过回归测试：
- `TestSendDailyEarningsReport_DoesNotMarkSentWhenTelegramDisabled`

修正内容：
- 只有 Telegram 实际发送成功后才写入 `last_report_date/last_report_at`
- Telegram 未启用时仅记录日志，不写入“已发送”状态

- [x] **Step 6: 增加手动验证入口**

新增 Telegram 指令：
- `/earnings`：手动发送正式收益报告，遵守当天去重
- `/earningspreview`：发送收益报告预览，不写入已发送状态

验证覆盖：
- 命令已注册并出现在帮助/命令列表
- 命令正确调用收益报告正式发送与预览发送回调
- 预览发送不会写入 `last_report_date/last_report_at`

### 当前实现结论

- [x] 本地代码实现完成
- [x] 自动化测试完成
- [x] 真实 Bitfinex 账户验证 `category=28` 利息账本口径
- [x] 真实 Telegram 发送链路验证
- [ ] 真实 Telegram `09:35` 定时投递验证

### 真实接口验证结论（2026-05-27）

- 使用新的 Bitfinex API key 做一次性只读验证，未启动完整 bot
- `wallet=funding + category=28` 成功返回真实利息账本
- 真实 ledger 描述不是 `interest`，而是 `Margin Funding Payment on wallet funding`
- 因此本地过滤规则已扩展为同时接受：
  - `interest`
  - `margin funding payment`
- “昨天收益”的实际到账口径已确认应按“今天入账”窗口统计
  - 例如 `2026-05-27 09:35` 发送收益报告时，`昨日实际到账收益` 统计 `2026-05-27 00:00:00` 到 `2026-05-28 00:00:00` 的已入账记录
- 已使用真实 Telegram `chat_id=5488788297` 成功发送一条收益报告预览消息，确认 Telegram token、chat id 与消息格式链路可用

---

**Execution note:** 优先按任务 1 -> 3 -> 4 -> 5 实施，任务 2 可与任务 3 并行或在任务 3 前完成接口封装。
