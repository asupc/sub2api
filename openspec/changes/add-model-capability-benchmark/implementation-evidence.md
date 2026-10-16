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

## 2026-10-15 本地真实模型测评记录

环境：Docker Compose 本地环境（backend `127.0.0.1:8088`，PostgreSQL/Redis healthy）。执行 `hard-v1` 48 题单采样；为避免后续自动扣费，4 个临时计划测评后已全部停用。

| 模型 | 账号 / Run | 状态 | 能力分 | 效率分 | 综合评级 | Token | 耗时 |
|---|---|---|---:|---:|---:|---:|---:|
| glm-5.3-flash | #565 / run 7 | partial，41/48 有效作答 | 78.8 | 95.03 | 81.27 | in 2,977 / out 12,226 / total 15,203 | avg 6.70s，P95 13.13s |
| glm-5.3 | #565 / run 8 | partial，42/48 有效作答 | 79.2 | 97.53 | 81.97 | in 3,074 / out 11,282 / total 14,356 | avg 6.04s，P95 9.25s |
| Qwen3.8-Flash | #571 / run 11 | partial，45/48 有效作答 | 75.4 | 85.45 | 76.96 | in 119,760 / out 25,639 / total 145,399 | avg 11.95s，P95 40.44s |
| MiniMax-M3 | Trae #570 / run 12 | 不可信 partial，仅 3/48 有效作答 | 62.1（仅 answered 口径，不排名） | missing | missing | in 738 / out 1,084 / total 1,822 | avg 3.26s，但 45 题上游失败 |

补充：

- glm-5.3-flash：33 correct / 8 incorrect / 7 execution failed；失败主要为 `upstream returned empty content`。
- glm-5.3：34 correct / 8 incorrect / 6 execution failed；1 次超时，5 次空内容。
- Qwen3.8-Flash：35 correct / 7 incorrect / 3 format error / 3 timeout；Qoder usage 覆盖完整，但当前价格目录缺失，cost 显示 0 仅表示未计价。
- MiniMax-M3：45 题报 `上游返回了未描述的错误`；随后 Trae quota 查询返回 `total=0`，本轮结果不纳入有效排名。
- DeepSeek-Flash：Qoder #569 两次探测均在自动等待 30 秒重试后返回 `RESOURCE_LIMIT / serviceAvailable=false`；账号剩余额度显示 1457，但模型服务当前不可用，因此未启动 48 题完整测评。
- 执行中修复并验证：hard-v1 随机素数生成 nil panic、runner 重入并发、Qoder/Trae usage 捕获、Qoder 流式读超时回收、部分 usage 的效率分口径。

## 2026-10-16 题库中文化

- 48 个题目题干与输出契约全部改为中文；JSON 键、数学变量、代码标识与 checker 枚举保留英文技术标识。
- `hard-v1` suite version 提升为 `1.1.0`，scoring version 保持 `1.0.0`；中英文题库不混合排名或混合绘制可比历史曲线。
- 新增单测断言每个生成 prompt 均包含中文，并修正“跨 run 变化”测试按 item ID 对比多 run hash 集合。
- `go test -tags=unit ./internal/service -run 'Test(ModelBenchmarkSuite|GeneratedBenchmark)' -count=1` 通过。

## 2026-10-16 模型选择跟随模型清单

- Backend target 列表改为直接复用 `ModelManifestService.GetManifest()`，再按 active/schedulable/benchmark 支持矩阵过滤账号。
- 前端新建计划模型下拉直接读取模型清单 API，展示全量模型并支持搜索与平台过滤；仅账号下拉限定为可测评账号，不支持模型显示明确提示并禁用提交。
- 验证：`go test -tags=unit ./internal/service -run 'TestModelBenchmark...'`、`go test -tags=unit ./cmd/server`、`go build ./...`、`pnpm vitest run src/views/admin/__tests__/ModelBenchmarkView.spec.ts`、`pnpm typecheck`、指定文件 ESLint、`pnpm build` 均通过。
