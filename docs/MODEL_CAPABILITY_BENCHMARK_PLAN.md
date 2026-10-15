# 模型能力测评实施计划（Model Capability Benchmark）

> 状态：MVP 已实施，尚未做真实模型验收；剩余缺口见 OpenSpec `tasks.md` 与 `implementation-evidence.md`。
> 日期：2026-10-15
> 可视化预览：`mockups/model-capability-benchmark-preview.html`

## 1. 定位与核心结论

新增后台能力 **模型能力测评**，对指定「模型 + 账号」定时执行高难度、程序可判定的题目，并输出：

1. **能力分 / Intelligence Score**：只由正确性计算，不被 Token 用量或耗时污染。
2. **效率分 / Efficiency Score**：独立衡量输出 Token、总 Token、耗时与估算成本。
3. **综合评级 / Composite Score**：默认 `85% 能力分 + 10% Token 效率 + 5% 耗时效率`，可配置，且必须与能力分分列展示。
4. **历史曲线**：按套件版本、模型、账号、时间范围展示能力分、分类分、Token、耗时、成本趋势，并支持多模型对比。

不把该分数称为 IQ。它是“特定题集、特定评分版本、特定账号路径下的相对能力表现”。

## 2. 目标与非目标

### Goals

- 支持定时和手动触发模型测评。
- 题目难度面向当前强模型：`hard-v1` 全部为难度 3-5，不包含送分题。
- 所有答案由服务端程序自动判定，不使用 LLM-as-judge。
- 记录输入 / 输出 / 推理 / 缓存 Token、总耗时、估算成本和错误类型。
- 保留每次 run 与每题明细，支持历史曲线和多模型对比。
- 从现有模型清单选择模型与账号，记录 requested model 与 upstream model。
- 控制测评成本与并发，默认关闭，避免升级后自动产生上游流量。
- 与现有 scheduled account test 并存但不混表、不混语义。

### Non-Goals

- 不测评图像、语音、视频、Realtime 模型。
- 不执行模型生成的代码；代码题只做静态推演、缺陷定位或复杂度判定。
- 不把账号健康检查结果当作能力分。
- 不使用公开基准原题打包进仓库，避免版权与数据污染。
- 不做人工评分、人工复核流和 LLM 裁判。
- 不用单次高分下结论；排名默认基于多次成功 run。

## 3. 题库设计：`hard-v1`

### 3.1 套件规模

| 套件 | 题量 | 难度 | 用途 |
|---|---:|---|---|
| `hard-v1` | 48 | D3-D5 | 默认正式测评 |
| `ultra-v1` | 96（Phase 2） | D4-D5 | 旗舰模型细分排名 |
| `spot-check-v1` | 12（Phase 2） | D3-D4 | 低成本巡检，不用于正式排名 |

第一版只实现 `hard-v1`。

### 3.2 题量分布

| 能力维度 | 题量 | 类别权重 | 难度分布 |
|---|---:|---:|---|
| 高级数学与组合推理 | 10 | 22% | D3×3，D4×5，D5×2 |
| 算法、复杂度与不变量 | 10 | 24% | D3×3，D4×4，D5×3 |
| 多约束逻辑与形式推理 | 8 | 18% | D3×2，D4×4，D5×2 |
| 代码缺陷与符号执行 | 8 | 18% | D3×2，D4×4，D5×2 |
| 规划、调度与图论 | 6 | 10% | D3×2，D4×3，D5×1 |
| 精确指令与抗干扰 | 6 | 8% | D3×2，D4×3，D5×1 |

难度权重：

- D3 = 1.0
- D4 = 1.5
- D5 = 2.2

最终题目权重 = 类别权重归一系数 × 难度权重，运行前由题库加载器计算并写入 run item 快照。

### 3.3 题目家族与动态参数

`hard-v1` 采用 **24 个题目家族 × 每家族 2 个动态实例 = 48 题**：

- 每个家族有固定考察目标、checker 和输出 schema。
- 每次 run 使用 `plan_id + run_id + item_family + suite_salt` 生成确定性随机参数。
- 数字、集合、图边、约束条件、变量名可变，标准答案由 Go 生成器同步计算。
- 数据库保存 `family_id`、`suite_version`、`scoring_version`、`prompt_hash`，不保存服务端 salt。
- 同一 run 内可复现，跨 run 不完全重复，降低缓存与题库泄漏影响。

### 3.4 高难度题型示例

以下为题型家族，不直接作为固定原题入库：

1. **模运算与数论**：大整数幂取模、逆元、同余方程组，输出 JSON 数值。
2. **组合计数**：受限排列、环染色、容斥计数，要求精确整数。
3. **概率与期望**：马尔可夫链期望步数或吸收概率，输出分数或小数并带容差。
4. **动态规划**：带字典序 tie-break 的背包 / 区间 DP，输出最优值与决策序列。
5. **图算法**：带负权检测的最短路、最小生成树唯一性、强连通缩点结果。
6. **贪心反例**：判断给定贪心策略是否产生最优解，并给出反例编号。
7. **复杂度分析**：递归式、均摊复杂度、最坏与平均差异，输出复杂度枚举值。
8. **程序不变量**：循环不变量选择，多字段部分计分。
9. **符号执行**：给定分支与输入域，输出可达路径编号集合。
10. **代码缺陷定位**：并发竞态、整数溢出、off-by-one、时间比较错误，输出缺陷类型与行号。
11. **多约束满足**：时间、资源、优先级、互斥约束，输出完整排序。
12. **调度优化**：单机 / 双机 job scheduling，输出 makespan 与顺序。
13. **依赖解析**：循环依赖检测与拓扑序 tie-break。
14. **逻辑证明**：判断蕴含、矛盾、可满足，输出 SAT model 或 UNSAT certificate type。
15. **信息更新**：公共知识 / 声明一致性推理，输出唯一可能集合。
16. **矩阵推理**：稀疏矩阵乘法结果的非零坐标集合。
17. **字符串算法**：KMP 失配函数、后缀数组片段、最小编辑路径。
18. **数值稳定性**：识别浮点误差来源并输出稳定公式编号。
19. **资源边界**：流量整形 / token bucket 状态推演，输出多时刻容量。
20. **异常路径**：错误包装、临时资源清理顺序，输出最终错误链。
21. **精确格式**：在干扰文本中输出严格 JSON，不允许额外字段。
22. **自校验**：模型需先验证自身答案是否满足全部约束，再输出最终字段。
23. **反直觉陷阱**：包含看似可简化但不可简化的条件，防止公式套用。
24. **多目标权衡**：在约束下选择 Pareto 最优方案集合。

### 3.5 输出契约

每题要求模型只输出一个 JSON 对象，例如：

```json
{
  "answer": {"value": 73920, "path": ["B", "A", "D", "C"]},
  "check": {"constraint_1": true, "constraint_2": false},
  "confidence": 0.82
}
```

服务端判定规则：

- 只接受最后一个合法 JSON 对象，忽略外层 code fence。
- 先做 JSON Schema 校验，再做 checker 判定。
- 非法 JSON：格式部分不得分；若关键字段缺失，该题不得分。
- 网络失败、上游不可用、超时：标记 `execution_failed`，不得按答错计 0 污染能力分。
- 答案错误：能力分按 0 处理。
- 多字段题允许字段部分得分，但每题得分上限为 1。

### 3.6 Checker 类型

| Checker | 用途 |
|---|---|
| `exact` | 归一化字符串 / 枚举精确匹配 |
| `integer` | 大整数精确匹配 |
| `number_tolerance` | 数值与小数容差 |
| `set` | 无序集合 |
| `sequence` | 有序序列 |
| `fields` | 多字段部分得分 |
| `boolean_matrix` | 布尔矩阵 |
| `schema_only` | 结构与约束字段校验 |

所有 scorer 必须是纯 Go 函数，不访问 DB、网络或全局时钟，便于确定性单测。

## 4. 评分与指标设计

## 4.1 能力分

```text
item_score = earned_points / possible_points
raw_score = Σ(item_score × item_weight) / Σ(item_weight)
intelligence_score = round(raw_score × 100, 1)
```

分类分：

```text
category_score = Σ(category item earned weight) / Σ(category item total weight) × 100
```

能力分只受答案正确性影响，不受耗时与 Token 影响。

## 4.2 Token 与耗时指标

每题必须记录：

| 指标 | 字段 |
|---|---|
| 输入 Token | `input_tokens` |
| 输出 Token | `output_tokens` |
| 推理 Token | `reasoning_tokens` |
| 缓存读 / 写 | `cache_read_tokens`, `cache_write_tokens` |
| 总 Token | `total_tokens` |
| 总耗时 | `latency_ms` |
| 首 Token（若流式 fallback） | `first_token_ms` |
| 估算成本 | `estimated_cost` |
| Usage 覆盖 | `usage_source: upstream / missing`，另存 `usage_field_coverage` |

执行策略：

- 优先使用非流式请求，便于解析完整 JSON 和 usage。
- 上游不支持非流式或只在流式 chunk 返回 usage 时，使用流式 fallback 并聚合 usage。
- 无法取得 usage 时不得虚构；标记 `missing`，该 run 的效率分显示为不可比。`reasoning_tokens`、cache token 与 first token 为 nullable 字段：服务端必须尝试解析，但上游不返回该字段时仅降低 coverage，不视为请求失败。
- 温度设为 0（支持时），禁用工具、联网检索和外部资源。
- 只对传输层错误做一次重试；答案错误不重试。

## 4.3 效率分与综合评级

固定预算由题库定义，例如每题 `expected_output_tokens` 与 `expected_latency_ms`：

```text
visible_output_efficiency = clamp(expected_output_tokens / actual_output_tokens × 100, 0, 100)
latency_efficiency = clamp(expected_latency_ms / actual_latency_ms × 100, 0, 100)
efficiency_score = visible_output_efficiency × 0.70 + latency_efficiency × 0.30
```

默认综合评级：

```text
composite_score = intelligence_score × 0.85
                + token_efficiency × 0.10
                + latency_efficiency × 0.05
```

约束：

- 综合评级必须与能力分分列展示，不得替代能力分。
- 效率分只在 usage 覆盖完整且成功作答题数达到阈值时计算。
- 历史曲线必须能单独查看能力分、Token、耗时、成本，避免只看综合分造成误读。
- 网络导致的极端耗时可标记 outlier，但不自动剔除，剔除策略需管理员明确操作。

## 5. 历史曲线与可视化

### 5.1 曲线指标

历史页面支持：

1. 能力分趋势。
2. 数学 / 算法 / 逻辑 / 代码 / 规划 / 指令分类分趋势。
3. 输出 Token 与总 Token 趋势。
4. 耗时趋势：平均值、P95、最大值。
5. 估算成本趋势。
6. 成功率、超时率、上游错误率。
7. 多模型对比曲线。
8. 同模型多账号对比曲线。

### 5.2 过滤与交互

- 时间范围：24h / 7d / 30d / 90d / custom。
- 套件：`hard-v1`，必须绑定 `suite_version + scoring_version`。
- 模型：单选或多选。
- 账号：全部 / 指定账号。
- 状态：仅成功 / 包含 partial。
- 图表交互：显示数值点、异常 run 标记、导出 CSV、打开对应 run 详情。
- Sparkline：排行列表中展示最近 10 次能力分走势。

### 5.3 可信度规则

- 单次 run 显示 `single_run` 标识，不参与默认排名。
- 默认排名取最近 3 次成功 run 的加权平均：最新 3、次新 2、更早 1。
- 分数波动超过 8 点时显示波动提示。
- 套件或评分版本变化后必须开启新的历史序列，不做跨版本直接比较。

## 6. 数据库设计

新增迁移建议：`backend/migrations/243_so_model_capability_benchmark.sql`。

> 二开迁移沿用 `NNN_so_*.sql` 命名，不占用上游编号。实施前必须重新核对当时最大迁移号。

### `model_benchmark_plans`

```text
id
name
suite_id
suite_version
scoring_version
model_id
account_id
cron_expression
enabled
sample_count
max_output_tokens
timeout_seconds
max_results
last_run_at
next_run_at
created_at
updated_at
```

### `model_benchmark_runs`

```text
id
plan_id
suite_id
suite_version
scoring_version
model_id
upstream_model_id
account_id
status                -- queued/running/success/partial/failed/canceled
intelligence_score
efficiency_score
composite_score
category_scores       -- JSONB
item_count
answered_count
correct_count
input_tokens
output_tokens
total_tokens
estimated_cost
avg_latency_ms
p95_latency_ms
error_message
usage_source
started_at
finished_at
created_at
```

### `model_benchmark_run_items`

```text
id
run_id
item_id
family_id
category
difficulty
weight
prompt_hash
response_text
status
score
earned_points
possible_points
checker_type
input_tokens
output_tokens
reasoning_tokens
cache_read_tokens
cache_write_tokens
total_tokens
estimated_cost
latency_ms
first_token_ms
error_message
created_at
```

索引：

- plans：`(enabled, next_run_at) WHERE enabled`
- runs：`(plan_id, created_at DESC)`、`(suite_id, suite_version, model_id, created_at DESC)`
- run items：`(run_id, item_id)`、`(run_id, category)`

## 7. 后端设计

### 7.1 模块

```text
backend/internal/service/model_benchmark_types.go
backend/internal/service/model_benchmark_suite.go
backend/internal/service/model_benchmark_scorer.go
backend/internal/service/model_benchmark_executor.go
backend/internal/service/model_benchmark_runner.go
backend/internal/service/model_benchmark_service.go
backend/internal/repository/model_benchmark_repo.go
backend/internal/handler/admin/model_benchmark_handler.go
```

职责：

- `suite.go`：加载内置题目家族、生成动态实例、计算 suite hash。
- `scorer.go`：纯函数答案判定与分数聚合。
- `executor.go`：发送单题请求、采集 usage 与 latency。
- `runner.go`：定时扫描、原子领取、执行 run、写结果。
- `service.go`：CRUD、手动运行、历史查询、排行查询。
- repository 只做 SQL，不包含评分逻辑。

### 7.2 调度与并发

- 独立 cron runner，不修改现有 scheduled account test 语义。
- 每分钟扫描 due plan。
- 使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取，防止多实例重复执行。
- 全局 benchmark 并发默认 1，可配置上限 2。
- 单 plan 内题目默认串行，避免打爆账号限流。
- 每题独立 timeout，run 总 timeout 为题数 × 单题 timeout + 固定裕量。
- 优雅停机时未完成 run 标记 `partial`，不伪装 success。
- 计划默认 `enabled=false`。

### 7.3 API

```text
GET    /admin/model-benchmarks/suites
POST   /admin/model-benchmarks/plans
GET    /admin/model-benchmarks/plans
PUT    /admin/model-benchmarks/plans/:id
DELETE /admin/model-benchmarks/plans/:id
POST   /admin/model-benchmarks/plans/:id/run
GET    /admin/model-benchmarks/runs
GET    /admin/model-benchmarks/runs/:id
GET    /admin/model-benchmarks/runs/:id/items
GET    /admin/model-benchmarks/history
GET    /admin/model-benchmarks/rankings
GET    /admin/model-benchmarks/export
```

最后一项应为 `/admin/model-benchmarks/export`；实施时以路由注册文件为准，避免拼写漂移。

## 8. 前端设计

新增页面：`/admin/model-benchmarks`。

模块：

1. **总览卡片**：最近 run、平均能力分、Token 用量、平均耗时、估算成本、活跃计划。
2. **模型排行**：能力分、效率分、综合评级、输出 Token、耗时、成本、最近趋势 sparkline。
3. **历史曲线**：多指标切换、多模型对比、异常点标记、run 详情跳转。
4. **能力拆解**：六类分类分条形图 / 雷达图。
5. **题库预览**：题目家族、难度、checker、类别权重；不展示标准答案。
6. **计划管理**：模型、账号、套件、Cron、采样数、Token 上限、超时、启用状态。
7. **Run 详情**：状态、分数、Token、耗时、每题输出、错误与判定原因。

## 9. 实施阶段

### Phase 0：方案确认

- [ ] 确认 `hard-v1` 48 题规模与难度分布。
- [ ] 确认默认综合评级权重 `85 / 10 / 5`。
- [ ] 确认默认 Cron（建议每周一次）与全局并发（建议 1）。
- [x] 确认第一阶段支持的模型平台范围：仅支持 OpenAI 网关下可通过 Chat Completions 直连的 API Key 账号（openai / Kimi / Zhipu / DeepSeek / MiniMax / OpenCode / Command Code / Cline）。OpenAI OAuth/Codex、Claude/Anthropic、Gemini、Grok、Antigravity、WorkBuddy/Qoder/Trae、TypeSafe 与媒体/Realtime 模型先显式禁用，待 Phase 2 适配。

### Phase 1：数据与评分内核

- [ ] 新增 SQL migration 与 repository。
- [ ] 实现题目家族、动态生成器和 suite hash。
- [ ] 实现 checker 与分数聚合。
- [ ] 单测覆盖非法 JSON、部分得分、数值容差、集合、序列、usage 缺失。

### Phase 2：执行器

- [ ] 实现非流式优先请求与流式 fallback。
- [ ] 采集 usage、latency、first token、错误分类。
- [ ] 支持每题独立 timeout 与传输层单次重试。
- [ ] 用 httptest / fake upstream 覆盖成功、失败、超时、格式错误。

### Phase 3：调度器

- [ ] 实现独立 cron runner。
- [ ] 实现 due plan 原子领取。
- [ ] 实现优雅停机和 partial 状态。
- [ ] 集成测试覆盖重复领取、多计划、失败恢复。

### Phase 4：管理 API

- [ ] 实现 plans / runs / items / history / rankings API。
- [ ] 增加参数校验、分页、排序、导出。
- [ ] Handler 测试覆盖权限、非法参数和错误响应。

### Phase 5：前端页面

- [ ] 新增 API client 与类型。
- [ ] 新增模型测评页面和路由。
- [ ] 实现排行、曲线、分类拆解、计划管理、run 详情。
- [ ] 补齐 zh i18n 与 Vitest。

### Phase 6：验收与记录

- [ ] 后端 unit / integration / lint。
- [ ] 前端 test / typecheck / build。
- [ ] 本地数据库迁移重复执行测试。
- [ ] 用 fixture 上游完成完整 fake E2E。
- [ ] 真实模型仅做小样本冒烟，明确标记环境与账号。
- [ ] 更新 `CHANGELOG-SO.md`。

## 10. 验收标准

1. 默认安装后不自动发起任何测评请求。
2. 创建计划后，能按 Cron 和手动触发执行。
3. 48 题全部生成动态实例并保存 `prompt_hash`。
4. 答案评分完全由服务端程序完成。
5. Token、耗时、成本指标逐题与逐 run 可见。
6. usage 缺失时明确显示 `missing`，不虚构效率分。
7. 历史曲线能按套件版本、模型、账号过滤，并展示多模型对比。
8. 套件或评分版本变化不会与旧曲线混算。
9. 上游不可用不会把能力分误判为 0。
10. 多实例调度不重复执行同一 due plan。
11. 管理端能从 run 明细解释每一题得分原因。

## 11. 风险与控制

| 风险 | 控制措施 |
|---|---|
| 题目泄漏或缓存命中 | 动态参数、prompt hash、套件版本轮换 |
| 上游 usage 缺失 | 标记 missing，效率分不可比 |
| 网络耗时干扰模型能力 | 能力分与效率分分离 |
| 账号路径影响公平性 | 固定模型 + 账号，展示平台与映射目标 |
| 成本失控 | 默认关闭、题量上限、全局并发 1、预算提示 |
| 多实例重复执行 | `FOR UPDATE SKIP LOCKED` |
| 题库过拟合 | 保留多套生成家族，后续引入新版本 |
| 历史误读 | suite/scoring 版本隔离，显示 single_run 与波动 |

## 12. UI 原型说明

打开：

```text
mockups/model-capability-benchmark-preview.html
```

预览包含：

- 深色后台页面结构。
- 总览指标卡。
- 模型排行与 sparkline。
- 能力 / Token / 耗时 / 成本历史曲线切换。
- 六类能力拆解。
- 高难度题库家族表。
- 测评计划表单与执行状态。
- Run 明细抽屉。

该 HTML 为纯静态可视化原型，不访问网络、不包含真实数据，不代表已实现功能。



## 审核记录

### 2026-10-15 首轮自查（送确认前）

**结论**：有条件通过。整体架构与项目现有 Gin + Service + Repository、SQL migration、Vue 3 管理端模式相符，HTML 原型可渲染；但以下阻塞项需在确认或实施前收敛，避免第一版能力契约不可实现或调度语义不完整。

#### 问题登记表

| 编号 | 严重级 | 位置 | 问题与建议 | 状态 |
|---|---|---|---|---|
| B-01 | 🔴 阻塞 | §4.2、§6、Spec「逐题资源用量」 | “每题必须记录 reasoning/cache token”对部分上游不可实现：现有代码仅从 OpenAI/xAI 兼容 `*_details.reasoning_tokens` 等字段提取，Claude/其他协议 coverage 不同。应改为“必须尝试解析并落 nullable 字段 + usage field coverage”，字段缺失不得视为执行失败。 | 已修复待复核 |
| B-02 | 🔴 阻塞 | Phase 0、§7 | 第一版平台/协议支持矩阵未定。现有 `AccountTestConnection` 平台分支很多，且并非所有测试路径都消费 prompt；若不先定义 text chat benchmark adapter 的支持范围，会出现计划可创建但执行无语义的模型。 | 已修复待复核 |
| B-03 | 🔴 阻塞 | §6、§7.2 | plans 表未设计领取状态、租约或“领取时推进 next_run_at”的事务语义，无法支撑 `FOR UPDATE SKIP LOCKED` 原子领取；需补 `running_status/claim_expires_at` 或等价领取字段与恢复规则。 | 已修复待复核 |
| S-01 | 🟡 建议 | §4.3 | 效率分文字提到“输出 Token、总 Token”，公式只使用输出 Token；需明确是否计入 input/reasoning/cache，或拆成 visible output efficiency 与 total token cost。 | 已修复待复核 |
| S-02 | 🟡 建议 | §7.3、§9 Phase 2 | 估算成本未说明复用 `BillingService.CalculateCostUnified` 时的模型名、分组、渠道定价、倍率与失败回退策略。应补成本口径：优先 upstream model + group/channel resolver，失败标记 `pricing_missing`。 | 已修复待复核 |
| S-03 | 🟡 建议 | §6、§11 | Run item 只保存 `prompt_hash`，若要解释分数和复现题目，需要决定是否保存脱敏 prompt snapshot、参数 seed 版本与最大保留期。 | 已修复待复核 |
| T-01 | 🟢 提示 | §8 | 新路由 `/admin/models` 与侧栏模式已存在，模型测评页面可按同路径接入；无需新建交互范式。 | 无需修改 |

#### 核实台账

- **已核实到方法体**：
  - `ScheduledTestRunnerService.runScheduled`：每分钟扫描、现有 5 分钟超时与 10 worker 语义，证明不能直接复用于长测评。
  - `scheduledTestPlanRepository.ListDue`：现有 due 查询无原子领取/租约。
  - `ModelManifestService.GetManifest` 与 `manifestAccountModels`：现有模型清单可提供模型/账号候选来源。
  - `AccountTestService.TestAccountConnection` / `RunTestBackground`：现有测试以 SSE 捕获为主，平台分支多，prompt 消费不一致。
  - `BillingService.CalculateCostUnified` / `CalculateCost`：已有统一成本计算入口，但需要计划补充 resolver 与失败口径。
- **已核实文件/路由**：
  - `backend/migrations/066_add_scheduled_test_tables.sql`：现有 scheduled test schema。
  - `backend/migrations/` 当前最大编号为 `242`，计划中的 `243_so_...` 尚未冲突。
  - `frontend/src/router/index.ts` 已有 `/admin/models` Admin 路由；`AppSidebar.vue` 已挂模型清单入口。
  - HTML 原型经 headless Edge 渲染，排行、sparkline 与历史曲线 DOM 均生成。
- **未核实**：真实各平台上游 usage 字段覆盖率；应在实施 Phase 2 用 fake response 矩阵和真实小样本冒烟分别证明，不能在方案阶段直接断言全覆盖。
- **未覆盖**：未运行后端/前端测试，因为本次仅新增计划、OpenSpec 草案和静态 HTML 原型，未改运行时代码。


### 2026-10-15 复核记录（MVP 实施后）

**结论**：B-01 / B-02 / B-03 已按方案收敛；S-01 / S-02 / S-03 已同步落入实现与证据边界。真实模型验收、部分错误矩阵、前端页面专项测试和 CSV 导出仍保持未完成状态。

- B-01：逐题 usage 字段为 nullable，新增 `usage_field_coverage`，缺失不视为请求失败。
- B-02：Phase 1 支持矩阵固定为 openai / Kimi / Zhipu / Deepseek / MiniMax / OpenCode / Command Code / Cline 的 Chat Completions API Key 账号，其余服务端返回 `MODEL_BENCHMARK_UNSUPPORTED_TARGET`。
- B-03：plans 增加 `running_status` 与 `claim_expires_at`，Repository 使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取，runner 负责过期恢复与 panic / error 释放。
- S-01：效率分明确使用“可见输出 Token 效率”，总 Token / 推理 / 缓存 / 成本独立展示。
- S-02：成本优先使用现有 BillingService 定价；缺价显式 `pricing_source=missing`，不虚构成本。
- S-03：run item 保存脱敏 prompt snapshot、prompt hash、family / checker / weight 与错误原因，并随 `max_results` 清理。
