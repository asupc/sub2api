# 模型能力测评实施计划（Model Capability Benchmark）

> 状态：v7 难度强化题库（核心题硬化 + D6 扩规模 + D7 复合题）；历史 v1/v2/v4 审核与执行证据保留在后文，v4 验证见 `MODEL_CAPABILITY_BENCHMARK_CODING_V4.md`，v7 验证见 `MODEL_CAPABILITY_BENCHMARK_QUESTION_AUDIT.md` 第 11 节。
> 日期：2026-10-17（v7 升级）；真实模型难度校准与生产验收尚未执行。
> 可视化预览：`mockups/model-capability-benchmark-preview.html`

## 1. 定位与核心结论

新增后台能力 **模型能力测评**，对指定「模型 + 账号」定时执行高难度、程序可判定的题目，并输出：

1. **能力分 / Intelligence Score**：只由正确性计算，不被 Token 用量或耗时污染。
2. **效率分 / Efficiency Score**：独立衡量输出 Token、总 Token、耗时与估算成本。
3. **综合评级 / Composite Score**：默认 `85% 能力分 + 10% Token 效率 + 5% 耗时效率`，可配置，且必须与能力分分列展示。
4. **历史曲线**：按套件版本、评分版本、公共试卷批次、输出预算、超时、采样数、模型、账号与时间范围展示趋势。
5. **测量质量**：分开展示有效完成率、格式合规率、预算耗尽、排名资格与能力域覆盖；排名资格仅作诊断标注，不构成排名门槛。

不把该分数称为 IQ。它是“特定题集、公共试卷批次、评分版本、预算与账号路径下的相对能力表现”。

## 2. 目标与非目标

### Goals

- 支持定时和手动触发模型测评。
- 题目难度面向当前强模型：`hard-v1 v7.0.0` 覆盖 106 个题型、单卷 106 题、9 个能力域，编程与算法权重合计 46%；D3-D7 仍为配置标签，待真实模型校准。
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
- 不用聚合均值掩盖最新表现；排名按题库版本展示，取每模型最新一次成功/部分成功 run 的成绩。

## 3. 题库设计：`hard-v1`

### 3.1 套件规模

| 套件 / 版本 | 题型 | 题量 | 状态 |
|---|---:|---:|---|
| `hard-v1` suite `7.0.0` / scoring `7.0.0` | 106 | 106 | 当前难度强化测评（核心题硬化 + D6 扩规模 + D7 复合题） |
| `hard-v1` suite `6.0.0` / scoring `6.0.0` | 98 | 98 | 历史 v6 难度强化测评 |
| `hard-v1` suite `4.0.0` / scoring `4.0.0` | 80 | 80 | 历史编程与算法强化测评 |
| `hard-v1` suite `3.0.0` / scoring `3.0.0` | 56 | 56 | 历史单实例中文题库 |
| `hard-v1` suite `2.0.0` / scoring `2.0.0` | 56 | 112 | 历史双实例题库 |
| `hard-v1` suite `1.3.0` / scoring `1.1.0` | 24 | 48 | 历史修正版，仅用于隔离旧证据 |
| `hard-v1` 1.1.0 / 1.2.0 | 24 | 48 | 含已知题库缺陷，不作能力结论 |

公共试卷批次默认 `baseline-v2`。批次、采样序号、题型与变体共同生成确定性实例；同一批次和采样序号下，不同模型拿到同题。采样序号 1..3 生成三份独立试卷，重复执行同序号主要衡量稳定性。

### 3.2 能力域与权重

| 能力域 | 题型数 | 题数 | 类别权重 | 难度分布（题型） |
|---|---:|---:|---:|---|
| 抽象归纳 abstraction | 6 | 6 | 10% | D4×4，D5×2 |
| 材料与数据 understanding | 6 | 6 | 8% | D4×4，D5×2 |
| 形式逻辑 logic | 12 | 12 | 12% | D4×4，D5×4，D6×3，D7×1 |
| 因果与概率 causal | 4 | 4 | 5% | D4×1，D5×3 |
| 算法与数据结构 algorithms | 25 | 25 | 18% | D3×1，D4×3，D5×11，D6×8，D7×2 |
| 编程与代码修复 code | 32 | 32 | 28% | D3×1，D4×6，D5×18，D6×5，D7×2 |
| 规划调度 planning | 11 | 11 | 8% | D4×2，D5×5，D6×2，D7×2 |
| 数学组合 math | 7 | 7 | 5% | D4×3，D5×3，D7×1 |
| 精确指令 instruction | 3 | 3 | 6% | D4×2，D5×1 |
| **合计** | **106** | **106** | **100%** | D3×2，D4×29，D5×49，D6×18，D7×8 |

最终题目权重先在能力域内按题型数量归一，再乘难度权重（D3=1.0、D4=1.5、D5=2.2、D6=3.0、D7=4.0）。因此每个能力域总分占比等于上表类别权重，不因题型数量重复放大。

### 3.3 公共试卷与动态参数

- 每个题型有固定考察目标、checker、输出 schema；每份试卷生成一个实例，采样序号 1..3 生成独立试卷。测试额外覆盖两个变体。
- 实例输入由比较键、采样序号、题型、变体与套件 salt 派生；不使用 plan/run/account ID，消除“不同模型不同题”的混杂。
- 图结构、字符串、权重、条件、选项标签和干扰材料可变；核心题型也避免只替换案例编号。
- 数据库保存 `family_id`、`category`、`difficulty`、`prompt_hash`、比较键与预算快照，不保存服务端 salt 或标准答案。
- 同一 run 内可复现；不同比较键或采样序号生成独立试卷，降低缓存与题库泄漏影响。

### 3.4 v2 新增能力域

1. **抽象归纳**：仿射规则、符号替换、序列变换、网格对称、关系复合、有限状态机。
2. **材料与数据**：账本 reconciliation、多跳记录、单位换算、加权比例、长上下文版本关联、版本化政策。
3. **形式逻辑与信息充分性**：偏序关系、Horn 规则闭包、量词真值、约束蕴含、最小不一致子集、缺失信息。
4. **因果与概率**：干预期望、个体反事实、贝叶斯后验、辛普森分组/汇总差异。
5. **代码与纠错**：循环边界、逆波兰表达式、错误程序反例、证明首错步骤。
6. **规划调度**：依赖最早完成、二维资源背包、时间窗路线、库存订货策略。
7. **精确指令**：抗干扰提取、嵌套筛选与输出形状约束。

原 24 个核心题型保留，但拓扑图、KMP 字符串、区间权重、并发安全/竞态、不变量选项、贪心面额、持钥陈述、清理注册顺序、Pareto 输入与数值稳定性选项均改为动态生成。

v4 进一步增加 16 个编程和 8 个算法题型：内存别名、defer 返回值、事件循环、事务隔离、CAS 全调度计数、死锁传播、脏页 LRU、污点不动点、字节码控制流、补码溢出、ABI 布局、程序补丁、最短反例和 SQL NULL；算法增加负环、最大流、匹配计数、SCC、加权编辑距离、LIS 计数、区间更新和带钥匙最短路。完整题型与契约见 `MODEL_CAPABILITY_BENCHMARK_CODING_V4.md`。

### 3.5 输出契约

每题要求模型只输出一个 JSON 对象，例如：

```json
{"answer": {"value": 51, "first_merge_left_index": 0}}
```

服务端判定规则：

- 普通正确性题接受最后一个合法 JSON 对象中的 `answer`，容忍外层 code fence 和说明文字；数组、对象中的 JSON 数值按数值语义比较，整数判分不经过 float64 舍入。
- `strict-schema` 是精确指令题：整个响应必须是仅含 `answer` 的 JSON 对象，不接受说明文字、code fence、重复键或额外字段；答案结构和值必须全部匹配，不给字段部分分。
- 非法 JSON：格式部分不得分；若关键字段缺失，该题不得分。
- 网络失败、上游不可用、超时：标记 `execution_failed`，按最终计分原则以 0 分进入能力分分母（2026-10-17 起，取代早期「不计 0 污染」规则）。
- 答案错误：能力分按 0 处理。
- 多字段题允许字段部分得分，但每题得分上限为 1。

### 3.6 Checker 类型

| Checker | 用途 |
|---|---|
| `exact` | 归一化字符串 / 枚举精确匹配 |
| `integer` | int64 范围内整数精确匹配，解码保留数值精度 |
| `number_tolerance` | 数值与小数容差 |
| `set` | 无序集合 |
| `sequence` | 有序序列 |
| `fields` | 多字段部分得分 |
| `schema_only` | 精确结构与约束字段校验 |
| `witness` | 验证模型给出的反例 / 调度 / 不一致子集本身是否可行且达到最优值，接受所有等价正确解 |
| `coding_patch` | 仅解释受限比较符与整数参数，对题面声明的完整有限输入域验证补丁的功能、终止及边界 |
| `coding_witness` | 检验反例的输入域、真实失效性质和全域最短长度，接受任意等价最短反例 |

所有 scorer 必须是纯 Go 函数，不访问 DB、网络或全局时钟，便于确定性单测。多字段题仅 `fields` 给部分分；`schema_only` 和 witness 均为全对或全错。

### 3.7 题库正确性核实（2026-10-16）

题目、标准答案、比较语义或 checker 变更必须升级对应版本。当前题库为 `7.0.0`、评分为 `7.0.0`；run 记录实际执行版本、比较键和预算快照，不能沿用旧计划创建时的标签。旧 run 保留原始证据，标为 `legacy` 比较键，不自动回溯改分或发起上游测评。

原 24 题型与新增 32 题型的题意、唯一性、答案推导、独立复算及边界回归见 [题库正确性核实记录](MODEL_CAPABILITY_BENCHMARK_QUESTION_AUDIT.md)。v7 的核心题硬化、D6 扩规模与 8 个 D7 复合题型的独立复算记录见该文件第 11 节。核实题库正确性不等同于证明全部题目达到 D3-D7 的测量难度，也不等同于真实模型验收。

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

### 4.4 测量质量与排名资格

```text
valid_answered = answered - format_error
completion_rate = valid_answered / planned_items
format_compliance_rate = valid_answered / answered
```

- 网络失败、上游失败、预算耗尽、内容过滤标记 `execution_failed`，按最终计分原则以 0 分计入能力分分母；完成率/覆盖率仍只统计有效作答。
- 排名资格不看完成率/覆盖率（2026-10-17 起）：任何完成率都参与排名；执行中断时未执行的题在收尾聚合时合成 `execution_failed` 计分行（不落库），按 0 分计入分母（「未完成就不得分」）。覆盖率仅作诊断展示。排名资格仅剩两个门槛：套件规模不匹配（`suite_mismatch`）与 provider-managed budget。
- OpenAI OAuth/Codex 无法统一下发输出预算时，`max_output_tokens=0`，结果标记 provider-managed budget；此类 run 同样参与排名，行内以「由上游管理」徽标提示可比性风险。
- 排名按题库三元组（suite/scoring 版本）过滤，无批次、预算、覆盖与最少次数门槛；每模型取最新一次成功/部分成功 run 的分数，`run_count` 展示该题库下的累计合格执行次数。

## 5. 历史曲线与可视化

### 5.1 曲线指标

历史页面支持：

1. 能力分趋势。
2. 九个能力域分类分趋势：抽象归纳、材料与数据、逻辑、因果概率、算法、代码、规划、数学、指令。
3. 输出 Token 与总 Token 趋势。
4. 耗时趋势：平均值、P95、最大值。
5. 估算成本趋势。
6. 成功率、超时率、上游错误率。
7. 多模型对比曲线。
8. 同模型多账号对比曲线。

### 5.2 过滤与交互

- 时间范围：24h / 7d / 30d / 90d / custom。
- 套件：`hard-v1`，必须绑定 `suite_version + scoring_version + comparison_key`。
- 模型：单选或多选。
- 比较条件：输出预算、单题超时、采样数；诊断 run 可查看但不混入固定预算排名。
- 账号：全部 / 指定账号。
- 状态：仅成功 / 包含 partial。
- 图表交互：显示数值点、异常 run 标记、导出 CSV、打开对应 run 详情。
- Sparkline：排行列表中展示最近 10 次能力分走势。

### 5.3 可信度规则

- 默认排名取每模型最新一次成功/部分成功 run 的成绩，跑过即可上榜，无最少次数门槛。
- 排名默认展示最新题库，可切换至历史题库版本；跨题库版本不做直接比较。
- 分数波动超过 8 点时显示波动提示。
- 套件、评分版本、比较键或预算变化后必须开启新的历史序列，不做跨版本直接比较。

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
- 每分钟扫描 due plan，使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取。
- 每个 tick 领取全部 due plan 并行执行；同账号并行不做互斥，由计划排期自行规避。
- 单 plan 内题目默认串行，避免打爆账号限流。
- 每题独立 timeout；run 预算为 `106 × sample_count × timeout + 2 分钟` 收尾裕量。
- 领取租约覆盖完整 run 预算 + 1 分钟，不因 106 题长 run 被其他实例重复领取。
- 服务停机取消执行上下文；已完成题目与 partial 证据用独立收尾超时持久化，不伪装 success。

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
GET    /admin/model-benchmarks/history?comparison_key&max_output_tokens&timeout_seconds&sample_count
GET    /admin/model-benchmarks/rankings?suite_id&suite_version&scoring_version&limit
GET    /admin/model-benchmarks/rankings/suites
GET    /admin/model-benchmarks/export
```

最后一项应为 `/admin/model-benchmarks/export`；实施时以路由注册文件为准，避免拼写漂移。

## 8. 前端设计

新增页面：`/admin/model-benchmarks`。

模块：

1. **总览卡片**：最近 run、平均能力分、Token 用量、平均耗时、估算成本、活跃计划。
2. **模型排行**：能力分、效率分、综合评级、输出 Token、耗时、成本、最近趋势 sparkline。
3. **历史曲线**：多指标切换、多模型对比、异常点标记、run 详情跳转。
4. **能力拆解**：九类分类分条形图。
5. **比较条件**：公共试卷批次、输出预算、超时与采样数；诊断与合格结果分开展示。
6. **题库预览**：题目家族、难度、checker、类别权重；不展示标准答案。
7. **计划管理**：模型、账号、套件、Cron、比较键、采样数、Token 上限、超时、启用状态。
8. **Run 详情**：状态、分数、完成率、格式合规率、预算耗尽、排名资格、Token、耗时、每题输出、错误与判定原因。

## 9. 实施阶段

### Phase 0：方案确认

- [x] 确认 `hard-v1` v2 为 56 题型 / 112 题；难度标签待真实模型校准。
- [x] 确认默认综合评级权重 `85 / 10 / 5`。
- [x] 确认默认 Cron 为每周一次；全局并发为每 tick 全部 due plan 并行（2026-10-16）。
- [x] 支持矩阵：openai / Kimi / Zhipu / DeepSeek / MiniMax / OpenCode / Command Code / Cline 的 OpenAI 兼容 Chat Completions API Key；二开 WorkBuddy / Qoder / Trae OAuth 经真实源码集成路径；OpenAI OAuth/Codex 经 Responses 流式通道（预算由上游管理，仅诊断）。Claude/Anthropic、Gemini、Grok、Antigravity、TypeSafe 与媒体/Realtime 模型仍服务端拒绝。

### Phase 1：数据与评分内核

- [x] 新增 SQL migration 与 repository。
- [x] 实现题目家族、动态生成器和 suite hash。
- [x] 实现 checker 与分数聚合。
- [x] 单测覆盖非法 JSON、部分得分、数值容差、集合、序列、usage 缺失。

### Phase 2：执行器

- [x] 实现非流式优先请求与流式 fallback。
- [x] 采集 usage、latency、first token、错误分类。
- [ ] 支持每题独立 timeout 与传输层单次重试（timeout 已实施；传输层单次重试仍未实施）。
- [x] 用 httptest / fake upstream 覆盖成功、失败、超时、格式错误。

### Phase 3：调度器

- [x] 实现独立 cron runner。
- [x] 实现 due plan 原子领取。
- [x] 实现优雅停机和 partial 状态。
- [x] 集成测试覆盖重复领取、多计划、失败恢复。

### Phase 4：管理 API

- [x] 实现 plans / runs / items / history / rankings API。
- [x] 增加参数校验、分页与排序；导出仍未实施。
- [x] Handler 测试覆盖错误响应；权限沿用 admin 路由中间件。

### Phase 5：前端页面

- [x] 新增 API client 与类型。
- [x] 新增模型测评页面和路由。
- [x] 实现排行、曲线、分类拆解、计划管理、run 详情。
- [x] 补齐 zh i18n 与 Vitest。

### Phase 6：验收与记录

- [ ] 后端 unit / integration / lint（当前 unit 与 build 已通过；integration 和 lint 见最终核实记录）。
- [x] 前端指定 Vitest / typecheck / build 已通过。
- [x] 本地隔离 PostgreSQL 迁移重复执行测试已通过。
- [ ] 用 fixture 上游完成完整 fake E2E。
- [ ] 真实模型仅做小样本冒烟，明确标记环境与账号。
- [x] 更新 `CHANGELOG-SO.md`。

## 10. 验收标准

1. 默认安装后不自动发起任何测评请求。
2. 创建计划后，能按 Cron 和手动触发执行。
3. 106 题全部生成动态实例并保存 `prompt_hash`。
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
| 成本失控 | 默认关闭、题量上限、预算提示 |
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

### 2026-10-16 v2 扩充复核记录

**结论**：通过。此次复核覆盖 56 题型 / 112 题、公共比较批次、预算隔离、覆盖率门槛、非破坏性迁移、执行时限、流式截断证据、管理端 UI 和移动端布局。历史 MVP 记录中“前端专项测试未完成”的结论已被本轮 8 个 Vitest、typecheck、ESLint、build 和 Mocked API Chromium 验收取代；CSV 导出、传输层单次重试、真实模型难度校准和生产部署仍为未完成项。

- B-02 的支持矩阵按 v2 更新：直接 Chat Completions API Key、二开 WorkBuddy/Qoder/Trae OAuth 真实转发路径、OpenAI OAuth/Codex Responses 流式诊断通道；其余仍服务端拒绝。
- E01-E04 的完整闭合证据见 `MODEL_CAPABILITY_BENCHMARK_EXPANSION_PLAN.md` 实施结果。
- 旧 run 不迁移、不改分、不自动发起请求；`comparison_key=legacy` 与新公共卷隔离。

### 2026-10-16 排名规则修订记录

**结论**：排名改为「跑过即上榜、按题库切换、取最新成绩」。去掉 ranking_eligible / 最少 3 次 / 同批次同预算匹配与 AVG+标准差聚合；`ranking_eligible` 仍在 run 聚合时计算并展示，转为纯诊断标注。新增 `GET /rankings/suites` 题库列表接口，`GET /rankings` 改为 `?suite_id&suite_version&scoring_version&limit`（缺省编译期最新题库）；provider-managed budget（`max_output_tokens=0`）run 全部入榜，行内以「由上游管理」徽标提示。前端排行卡片新增题库下拉（内置套件标「最新」），批次/预算筛选表单只作用于执行记录与历史曲线。

### 2026-10-16 去账号化修订记录（公共网关回环）

**结论**：本文件前文关于「模型 + 账号」绑定、B-02 平台适配矩阵、`account_id` 字段（plans/runs/历史过滤）的描述全部作废。测评计划不再绑定账号，执行改为 HTTP 回环打自身网关 `http://127.0.0.1:{server.port}/v1/chat/completions`（Bearer 测评专用 API Key，经 `model_benchmark_settings` 设置项选择，Key 须已绑定分组）：认证、分组白名单、调度选号、平台协议转换、流式 usage 标准化与计费全部走网关真实链路，任何平台一次覆盖。目标矩阵改为模型级（有可调度账号 + 非图像/视频生成模型）。迁移 `246` 删除 plans/runs 的 `account_id` 列（顺带消除 plans 账号外键的级联删除隐患）。代价：测评流量按所选 Key 正常计费、记用量、占并发槽与 RPM，建议专建测评分组 + Key。旧账号级执行通道（`RunBenchmarkPrompt` 及各平台测试链路 benchmark 感知代码）解除调用后保留。

