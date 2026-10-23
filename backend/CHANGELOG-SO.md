
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
