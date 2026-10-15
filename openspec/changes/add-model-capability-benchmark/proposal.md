## Why

现有 scheduled account test 只验证「账号 + 模型」连通性，无法比较模型在推理、算法、代码缺陷定位和精确指令方面的高难度能力，也无法持续观察 Token 用量、耗时、成本和分数变化。管理员需要一个可定时执行、程序自动判定、可审计且可与历史趋势对比的模型能力测评能力。

直接复用 scheduled test 会混淆健康检查和能力评估：前者关心上游是否可用，后者关心答案正确性、输出效率和分数可信度。因此需要独立计划、执行、评分、指标和历史模型。

## What Changes

- 新增默认关闭的模型能力测评计划，目标维度为「模型 + 账号」，记录 requested model 与 upstream model。
- 新增内置高难度套件 `hard-v1`：24 个题目家族 × 2 个动态实例，共 48 题，难度 D3-D5。
- 新增程序化评分：JSON Schema、精确值、整数、数值容差、集合、序列、多字段部分得分；不使用 LLM-as-judge。
- 新增能力分、效率分与综合评级；能力分不受 Token 与耗时影响，效率分独立计算。
- 新增逐题与逐 run 指标：输入 / 输出 / 推理 / 缓存 Token、总耗时、first token、估算成本、usage 覆盖来源。
- 新增独立定时 runner：Cron 扫描、PostgreSQL 原子领取、单题超时、全局低并发、优雅停机。
- 新增 runs / items 持久化、历史曲线 API 和多模型排行。
- 新增后台页面 `/admin/model-benchmarks`，包含总览、排行、历史曲线、能力拆解、题库预览、计划管理和 run 详情。
- 新增数据库迁移 `243_so_model_capability_benchmark.sql`（实施前重新核对当时最大迁移号）。
- 不改变现有 scheduled account test 表、语义和 API。

## Capabilities

### New Capabilities

- `model-capability-benchmark`: 定义高难度题集、动态题目、程序评分、指标采集、定时执行、历史曲线和管理控制台行为。

### Modified Capabilities

无。现有 scheduled account test 保持不变。

## Impact

- **后端**：新增 model benchmark service / scorer / executor / runner / repository / admin handler；不把评分逻辑写入 handler。
- **数据库**：新增 plans、runs、run items 三张表及索引；不改现有账号测试表。
- **管理 API**：新增 `/admin/model-benchmarks/*`，全部挂在 Admin 鉴权后。
- **前端**：新增页面、路由、API client、类型和 zh i18n。
- **上游流量**：计划默认关闭；启用后每个 run 固定产生题目数 × 采样次数个上游请求。
- **公平性**：同一计划固定模型与账号；历史曲线必须绑定 suite version 与 scoring version。
- **可观测性**：usage 缺失必须显式标记，不得虚构 Token 或效率分。
