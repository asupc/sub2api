# 三平台运营任务（platform-ops）

> 适用于二开新增的三个源码集成平台：**WorkBuddy**（腾讯 CodeBuddy）、**Qoder**（阿里）、**Trae**。
> 移植自 workbuddy2api-panel / qoder2api-hub / trae-workbuddy-switch 的定时运营能力。

## 架构

- 共享基建：`backend/internal/service/platform_ops.go`
  - `PlatformOpsTask{ID, Platform, Name, Hours, Interval, RunOne, IncludeDisabled}` 任务描述
  - `RunPlatformOpsTask`：账号级互斥锁 + `extra["ops_state"][taskID]` 状态记录（LastRun/LastOK/LastErr/Streak/Retries/NextRun）+ 当日失败退避（10min→30min，最多 2 次，跨日重置）
  - `nextDailyFire(now, hours, loc)`：每日定点计算（移植自 workbuddy2api-panel scheduler），时区默认 Asia/Shanghai，账号 `extra["ops_timezone"]`（IANA 名）可覆盖
  - `PlatformOpsRunner` 注册表：各平台 OpsService 构造时自注册
- 每平台一个 OpsService（`<platform>_ops_service.go`），克隆 CNProviderBalanceCheckService 的 Start/Stop/runOnce + ticker 骨架（tick 1min，按定点/NextRun 判定到期）。**当前构造即启动，无配置开关**（已知遗留）。
- 通用 HTTP 端点（`handler/admin/platform_ops_handler.go`，无需为任务写路由）：
  - `GET  /api/v1/admin/platform-ops/tasks` — 全平台任务目录
  - `GET  /api/v1/admin/platform-ops/accounts/:id` — 单账号全部任务描述 + 最近状态
  - `POST /api/v1/admin/platform-ops/accounts/:id/tasks/:task` — 手动执行（业务失败返回 `{done:false, error}`，状态已落库）

## 任务清单（时刻为账号时区）

### WorkBuddy（`workbuddy_ops_service.go`，7 项）

| 任务 ID | 名称 | 时刻 | 说明 |
|---|---|---|---|
| checkin | 每日签到 | 09, 21 | DailyCheckin（幂等）；成功后刷新余额（解冻 `wb_no_credit` 罚停） |
| travel | 猫猫旅行 | 09, 21 | 派出 + 领奖闭环 |
| activity | 活跃上报 | 10 | ReportChatActivity |
| keepalive | token 保活 | 22 | RefreshToken 空跑 |
| blackcat | 夜猫子 | 23 | 仅夜间窗口内执行 |
| growth | 成长任务 | 01 | 一键完成（内置 chat_5/first_buddy/black_cat 动作；其余 14 个桌面事件类任务未移植，pkg 层留 ReportChatActivity 扩展通道）；企业账号跳过 |
| balance | 余额刷新 | 6h 间隔 | → `extra["wb_quota"]`（remain/total/expiring_at） |

### Qoder（`qoder_ops_service.go`，5 项）

| 任务 ID | 名称 | 时刻 | 说明 |
|---|---|---|---|
| checkin | 每日签到 | 09, 21 | 领 Credits（幂等：已领/未开放按成功）；成功后刷额度解冻 `qd_no_credit` |
| pro_welfare | Pro 福利包 | 09, 21 | eligibility → claim（无资格/已领按成功） |
| campaigns | 活动扫描 | 10 | 仅 CLAIMABLE 条目领取（幂等） |
| keepalive | token 保活 | 22 | RefreshAccountToken（PAT 账号经 personal_token 重交换）；IncludeDisabled |
| balance | 额度刷新 | 6h 间隔 | QuotaUsage + UserPlan → `extra["qd_quota"]`（remain/used/total/plan） |

### Trae（`trae_ops_service.go`，3 项）

| 任务 ID | 名称 | 时刻 | 说明 |
|---|---|---|---|
| checkin | 每日签到 | 09 | 签到状态预检 + claim |
| credits_monitor | 积分到期监控 | 10 | 临期账号写 `expiring_soon` 标记 + 日志告警 |
| usage_refresh | 积分额度刷新 | 6h 间隔 | → `extra["trae_quota"]` |

## 额度快照（extra 键）

| 平台 | extra 键 | 字段 |
|---|---|---|
| workbuddy | `wb_quota` | remain / total / expiring_at / updated_at |
| qoder | `qd_quota` | remain / used / total / plan / updated_at |
| trae | `trae_quota` | 积分与到期信息（含 `expiring_soon`） |

手动查询端点：`GET /api/v1/admin/{workbuddy|qoder|trae}/accounts/:id/quota`（查询同时落快照）。

## 今日积分消耗（二开统计）

账号列表 usage 列的三平台额度胶囊旁会显示「今日 N」——该账号当日经网关消耗的积分合计。

- **数据来源**：上游每个响应的 usage 字段（qoder 复数 `credits`、workbuddy 单数 `credit`），网关在 `RecordUsage` 时累加，与余额快照（remain/total）是不同口径：一个是**消耗**、一个是**剩余**。
- **存储**：`account_daily_credits(account_id, day, credits)`（迁移 `242_so_account_daily_credits.sql`），按 `timezone.Today()`（缺省 Asia/Shanghai）分日；账号删除级联清理。
- **下发**：经既有 `POST /api/v1/admin/accounts/today-stats/batch` 的 `WindowStats.credits` 字段（仅 qoder/workbuddy 有值；该接口有 30s 快照缓存，展示最多滞后 30s）。
- **注意**：仅统计经本网关的流量；直连上游或参考项目产生的消耗不计入。计费失败/超时的请求若未记录 usage，也不会累加（best-effort 语义）。

## 与上游同步的关系

`platform_ops.go` / `platform_ops_handler.go` / 三平台 ops 服务与 wire 接线均为纯新增文件，上游同步时无冲突面；`token_refresh_service.go`、`handler/handler.go`、`handler/wire.go`、`cmd/server/wire_gen.go`、`server/routes/admin.go` 的三平台注册行需手工合并（详见 CHANGELOG-SO.md 各批次条目）。
