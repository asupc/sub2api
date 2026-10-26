
### 2026-10-16 模型测评题库 v3：难度爬升与版本升级
- **描述**：将 `hard-v1` 套件/评分版本从 2.0.0 提升到 3.0.0；对既有 56 个题族增加实例规模与干扰项，包括更大的图矩阵、数据记录集、约束域、操作序列和数据集规模，降低 ceiling 效应、提升高分模型之间的区分度。所有预期答案继续由独立 oracle 与原 suite 测试验证，旧 run 按版本隔离，不回溯改分。
- **改动文件**：`backend/internal/service/model_benchmark_suite.go`、`backend/internal/service/model_benchmark_extended_suite.go`、`backend/internal/service/model_benchmark_test.go`、`backend/internal/service/model_benchmark_extended_test.go`。
- **与上游合并影响**：无（so 私有功能，不影响上游）。

### 2026-10-16 模型测评题库 v3.0.0-quick：从 112 题减为 56 题
- **描述**：将 `hard-v1` 的每题型变体数从 2 个减少为 1 个，单次测评题量从 112 减少到 56，缩短单次测评时间与成本。评分版本保持 3.0.0 ; 同时修正权重与聚合中的变体数量假设；权重重归一化仍保持 category 总权重不变。
- **改动文件**：`backend/internal/service/model_benchmark_suite.go`、`backend/internal/service/model_benchmark_types.go`、`backend/internal/service/model_benchmark_service.go`、相关测试文件。
- **与上游合并影响**：无。

### 2026-10-23 移除模型能力测评功能（保留模型清单）
- **描述**：删除 benchmark 全部后端实现（service 22 文件 / repository / handler / 路由 `/admin/model-benchmarks/*` / 设置项 / wire 装配 / 账号测试挂钩），`model_manifest_service.go` 剥离评分注入。新增 `250_so_drop_model_benchmark.sql`：既有库 DROP `model_benchmark_{plans,runs,run_items}` 并清空 243~249 的迁移记录，全新库为空操作。
- **改动文件**：见根 `CHANGELOG-SO.md` 同日条目（完整清单）。
- **与上游合并影响**：无。

### 2026-10-26 用量统计：模型分布去滚动条 + 按 API 统计用量

- **需求**：① 模型分布列表不再出现纵向滚动条，默认展示 6 条，提供"展开全部"查看全量；② 用户和管理员用量统计页新增"按 API 统计用量"板块，与管理控制台 API 密钥用量统计口径一致。
- **描述**：
  - 模型分布（`ModelDistributionChart.vue`，管理端/用户端用量页、管理控制台、账号用量弹窗共用）：移除列表容器 `max-h-48 overflow-auto`，默认展示 Top 6，超出时右下角"展开全部（N）▾ / 收起 ▴"切换；环图始终统计全量模型，不受折叠影响。文案新增 `common.expandAll/collapseAll`。消费榜（ranking）视图保留滚动不变。
  - 新增用户端接口 `GET /api/v1/usage/dashboard/api-keys-usage-stats`（start_date/end_date 缺省今天，limit 缺省 50 上限 200），限定当前用户 `u.user_id`，返回形状与管理端 `api-keys-usage-stats` 完全一致（含每密钥模型明细、缓存命中率、平均 Tokens/秒）。路由挂在 `/usage` 组，复用 Heavy 限流。
  - 前端：密钥用量类型（`ApiKeyUsageStatItem` / `ApiKeyModelUsageStat`）从 `api/admin/dashboard.ts` 迁入 `@/types` 共用（原路径 re-export 兼容）；新增纯展示组件 `ApiKeyUsageTable.vue`（表格+展开明细+展开全部）与数据卡片 `UsageApiKeyStatsCard.vue`（scope=admin 走管理端接口，user 走新用户接口，隐藏归属用户列），接入管理端/用户端 `UsageStatsView.vue`，日期变化与顶部"刷新"（reloadKey 通道）联动加载。管理控制台原 `ApiKeyUsageStats.vue` 未动。
- **改动文件/模块**：后端 `usage_handler.go`（新增 handler）、`routes/user.go`（注册路由）、`usage_service.go`、`account_usage_service.go`（接口）、`usage_log_repo_apikey_stats.go`（新增 ByUser 查询）、`api_contract_test.go` / `account_usage_service_batch_test.go`（桩补方法）、`usage_log_repo_apikey_stats_test.go`（新增 sqlmock 用例）。前端 `types/index.ts`、`api/{usage,admin/dashboard}.ts`、`components/dashboard/{ApiKeyUsageTable,UsageApiKeyStatsCard}.vue`（新增）、`ModelDistributionChart.vue`、`views/{admin,user}/UsageStatsView.vue`、i18n `common.ts` / `dashboard.ts`（`usage.apiKeyUsage*`）、新增 `ApiKeyUsageTable.spec.ts` + 模型分布展开用例。
- **验证**：后端 `go build ./...`、`go vet` 通过；仓库单测 3 个 PASS；handler 包全绿；service 包 5 个失败为 10-23 已知存量（Gemini thinking variant / 复合分组族）。前端 typecheck + eslint + 相关 55 用例通过；全量 vitest 287 文件 2131 用例全绿。
- **与上游合并影响**：`usage_handler.go`、`routes/user.go`、`usage_service.go` 为上游同名文件，下次同步需保留 ByUser 查询与 handler；其余（新组件、新卡片、模型分布折叠）均为 so 私有。`UPSTREAM-BASE` 不变。

### 2026-10-26 用量统计：同日代码审查修复（后端）

- **修复**：`DashboardAPIKeysUsageStats` 只解析日期（`parseAPIKeyStatsDateRange`：end_date-only 起点取当日、start>end/非法日期 400、AddDate 日历日推进兼容 DST）；`GetAPIKeyUsageStatsByUser` 明细查询补 `u.user_id` 过滤、改用命名返回值使 Close 失败可回传。
- **测试**：新增 `usage_handler_apikey_stats_test.go`；handler/repository 包单测与 vet 全绿。
- **与上游合并影响**：同根 `CHANGELOG-SO.md` 同日条目；`UPSTREAM-BASE` 不变。
