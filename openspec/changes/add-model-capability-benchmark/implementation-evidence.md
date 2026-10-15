# Implementation Evidence

> 日期：2026-10-15。证据只覆盖本变更；`PASS` 不外推到生产、真实模型或业务验收。

## 已实现范围

- 数据库迁移：`backend/migrations/243_so_model_capability_benchmark.sql`
- Repository：plans / runs / items / history / rankings / claim / lease / prune
- Service：`hard-v1` 48 题生成、程序评分、指标聚合、计划管理、目标过滤
- Executor：Phase 1 OpenAI-compatible Chat Completions API Key 直连，采集 usage / latency / cost
- Runner：独立 cron、全局并发 1、PostgreSQL `FOR UPDATE SKIP LOCKED`、panic / error 释放
- Admin API：`/admin/model-benchmarks/*`
- 前端：`/admin/model-benchmarks` 页面、API client、路由、侧栏入口、zh i18n、历史曲线和 Run 详情
- 静态可视化原型：`mockups/model-capability-benchmark-preview.html`

## 后端验证

| 检查 | 结果 | 说明 |
|---|---|---|
| `go test -tags=unit ./internal/service -run 'Test(ModelBenchmark|GeneratedBenchmark)' -count=1` | PASS | 覆盖 48 题生成、跨 run 变化、标准答案自校验、评分、usage、执行失败语义 |
| `go test -tags=unit ./internal/repository -run TestModelBenchmarkRepository -count=1` | PASS | 覆盖原子领取、租约与过期 claim 恢复 |
| `go test -tags=unit ./internal/handler/admin -run TestWriteModelBenchmarkError -count=1` | PASS | 覆盖错误状态映射 |
| `go test -tags=unit ./migrations -run TestModelCapabilityBenchmark -count=1` | PASS | 覆盖幂等迁移关键约束 |
| `go test -tags=integration ./internal/repository -run TestModelBenchmarkMigrationCreatesCoreTables -count=1` | PASS | 集成库中三张核心表存在 |
| `go test -tags=integration ./internal/repository -count=1` | PASS | 本变更所在 repository integration 包复跑通过 |
| `go build ./...` | PASS | 后端全部包编译通过 |
| `go test -tags=unit ./...` | FAIL（非新增） | 仅 `TestCommandCodeGatewayPassesThroughCatalogProtocols`、`TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` 失败；在临时 clean worktree `HEAD=19b583d` 上运行同包全量同样失败， targeted 单独运行通过，判定为既有全量顺序/状态污染问题 |
| `go test -tags=integration ./...` | FAIL（1 项波动后复跑通过） | `TestOpsOutputTPSDistribution` 因 `idx_usage_logs_request_id_api_key_unique` duplicate key 失败；该测试单独复跑通过，`./internal/repository` integration 全包复跑通过，判定为并发测试数据波动 |
| `golangci-lint run ./...` | BLOCKED（0 issues） | v2.13.0 + Go 1.27.2 输出 `0 issues`，但 typechecking context 无法读取 Go export data v5，并叠加快照中既有 handler 类型上下文错误，进程退出 1 |

## 前端验证

| 检查 | 结果 | 说明 |
|---|---|---|
| `pnpm test:run` | PASS | 286 files / 2122 tests |
| `pnpm typecheck` | PASS | vue-tsc 通过 |
| `pnpm lint:check` | PASS（1 warning） | 既有 `DeviceAuthFlow.spec.ts` unused import warning |
| `pnpm build` | PASS | i18n check + vue-tsc + Vite build 通过 |

## 未执行 / 未通过

- 未执行真实模型、真实账号、真实数据库生产迁移和生产部署验收。
- 未执行 48 题 fixture 服务层 fake E2E；已有替代证据为 48 题标准答案自校验 + 单题 fake upstream 执行测试。
- 未补齐 429 / 5xx / 超时 / 响应截断矩阵与前端页面专项交互测试。
- 全量 backend unit / integration 与 lint 因上述既有问题不能给出全绿结论。

## 关键实现边界

1. Phase 1 只支持 OpenAI-compatible Chat Completions API Key 账号：openai / Kimi / Zhipu / Deepseek / MiniMax / OpenCode / Command Code / Cline。
2. OpenAI OAuth/Codex、Claude/Anthropic、Gemini、Grok、Antigravity、WorkBuddy/Qoder/Trae、TypeSafe 与媒体/Realtime 目标在服务端拒绝。
3. 新计划默认停用；启用或手动排队后才会产生上游请求。
4. `reasoning_tokens`、cache token、first token 是 nullable 观测字段；缺失只降低 coverage，不算请求失败。
5. 能力分只按正确性计算；usage 缺失时效率分与综合评级不可比。
- 2026-10-15 收口补充：`go vet ./...` 通过；最终代码复跑 `go build ./...` 与模型测评专项单测均通过。
