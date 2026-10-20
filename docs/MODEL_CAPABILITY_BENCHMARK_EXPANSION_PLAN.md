# 模型能力测评扩充实施方案

日期：2026-10-16。用户已确认补充并要求尽可能补全；本方案作为当前实施基线。

## 目标与范围

将 `hard-v1` 从 24 家族/48 题扩展至 56 家族/112 题，题库版本 2.0.0、评分 2.0.0。增加 32 个原创、可程序验证的家族；保留既有题库正确性修复。题目难度标签需要真实模型校准，不宣称为智商测试或已完成真实模型验收。

新增家族：

- 抽象归纳：affine-rule、symbolic-substitution、sequence-transduction、grid-transform、relation-composition、finite-state-inference。
- 逻辑与信息充分性：relational-order、conditional-rules、quantified-logic、constraint-entailment、inconsistent-statements、missing-information。
- 因果与概率：causal-intervention、causal-counterfactual、bayes-diagnosis、simpson-confounding。
- 材料与数据：ledger-reconciliation、multi-hop-records、unit-conversion、weighted-rate、long-context-join、versioned-policy。
- 代码与纠错：trace-loop-boundary、expression-evaluation、bug-counterexample、proof-error。
- 规划：dependency-schedule、resource-allocation、route-with-windows、inventory-policy。
- 指令：distractor-extraction、nested-constraints。

## 实现约束

1. 所有写入只在 sub2api-so；保留已有未提交工作，不提交、不推送、不部署、不调用真实模型。CHANGELOG-SO.md 记录功能、文件和合并影响；UPSTREAM-BASE 不变。
2. handler→service→repository 分层；不新增执行模型代码、外部工具或 LLM 裁判，不引入依赖。新增题数据均由确定性生成器产生，输入、答案、求解器不接触网络和数据库。
3. 抽象归纳限定规则空间、核验唯一目标输出；逻辑题区分 entailed/refuted/unknown/inconsistent；多解优化题按性质验证正确性，避免因等价方案误判。
4. 公共 `comparison_key` 默认为 baseline-v2，计划及 run 持久化。生成仅依赖比较键、采样序号、题型和变体，同键同版本的模型得到同题；不同采样生成独立实例。run 保存输出预算与超时快照。
5. 排行按套件/评分/比较键/预算隔离；完成率至少 90%、各能力域至少 80% 题目取得有效回答、无格式和截断缺失造成样本选择的极端偏差方可入榜。完成率、格式错误、预算耗尽与领域分分开展示；低覆盖保留诊断分但排除正式排行。
6. 直连响应读取 finish_reason，预算截断与内容过滤记 execution_failed 并保留响应、usage、耗时；原始输出不因截断被当作错误答案。新计划默认 2048 输出 tokens，已有计划预算保持可见并由用户调整。
7. 题量扩展后的 run 超时与领取租约按题量、采样和单题预算计算，租约覆盖最大执行窗口及收尾裕量。不再由固定 30/45 分钟提前截断或导致重复领取。
8. 类别按实际题型归一计算，再按类别权重汇总；不重复把题型数量乘入领域权重。格式合规和完成率单列，保留每题失败原因。

## 文件与阶段

- 先完成本方案与审核记录；实施后更新既有 MODEL_CAPABILITY_BENCHMARK_PLAN.md、QUESTION_AUDIT.md 和 CHANGELOG-SO.md。
- 题库：model_benchmark_suite.go，新增 model_benchmark_extended_suite.go；测试独立从题面数据解题，含唯一性、反例、语义变换与负例。
- 评分/执行：model_benchmark_types.go、executor.go、service.go、runner.go；适配既有测试与新增边界回归。
- 数据：新增非破坏性 SQL 迁移，计划/run 比较键与 run 预算快照；model_benchmark_repo.go 读写、排行和历史过滤、tests。现有表为自定义 SQL，不改 Ent schema。旧 run 比较键默认 legacy，避免将旧随机卷标成公共卷；旧计划默认为 baseline-v2。
- API/UI：handler、frontend api 类型、两个测评页面、当前仅中文的 locales（不恢复已裁剪英文）；显示题量、领域、比较键、预算、完成率与排行条件，维护既有模型选择/手动排队交互。
- 验证：Go 专项/受影响包单测、独立答案复算、迁移测试、go build；pnpm 页面单测、typecheck、lint 与 build；用模拟 API 浏览器验收界面，真实模型验收标 NOT_RUN。

## 发布与回退

本轮只交付代码和验证证据。部署时由启动迁移先加列再加载新实现；旧 run 不重算。迁移仅加列/索引，回退旧二进制可忽略新列，保留历史证据。不同版本和预算的成绩不合并。无前端依赖或锁文件变更。

## 验收标准

- 56 家族/112 题，所有家族有明确输入、schema、可核对标准答案和独立负例测试。
- 同比较键跨 plan/run 可复现；不同键/采样覆盖真正变化的数据；旧快照保留。
- 错误答案、截断、格式错误、网络失败能区分；低覆盖不入榜；合理等价解可得分。
- 新领域可在界面展示，比较键可编辑/筛选，预算和完成率透明。
- 文档记录全部测试 PASS/FAIL/NOT_RUN，不把模拟浏览器和仓库测试当作真实模型验证。

## 审核记录

### 2026-10-16 实施前核对

结论：通过。此次为独立的计划核对阶段；用户已授权实施，核对完成后进入实施阶段。

| 编号 | 级别 | 位置 | 核对事项与处理 | 状态 |
|---|---|---|---|---|
| E01 | 阻塞 | 执行时限 | 当前 executor 单题预算可达 600 秒、采样可达 3；112 题不能沿用 30 分钟租约/45 分钟外层执行窗口。方案已要求按单计划计算时限与租约 | 实施验收项 |
| E02 | 阻塞 | 比较语义 | 当前 plan/run 种子不同，同名模型跨计划不可直接比较；需比较键和预算快照，历史 run 必须标为 legacy | 实施验收项 |
| E03 | 阻塞 | 统计分母 | 提前结束时当前 ItemCount 只统计已尝试题，可能把少量成功误当完整答卷；计划必须预存应答题数，完成率以应答题数为分母 | 实施验收项 |
| E04 | 建议 | 新题唯一性 | 规则归纳采用有限候选族；unknown、矛盾、多解优化均需独立枚举与负例验证，不能把标准答案自测当独立复算 | 实施验收项 |
| E05 | 提示 | 成本/验收 | 完整卷 112 题将增加请求数；页面清楚展示规模、采样和预算。仅本地模拟验收，不调用真实模型或宣称难度已校准 | 已纳入 |

核实台账（均已读到方法体）：GenerateModelBenchmarkItems/seed、全部原题生成、ModelBenchmarkScoreAnswer/JSON/checker、AggregateModelBenchmarkRun、CreatePlan/UpdatePlan/ExecutePlan/finishRun、executor 两条路径、runner 领取与外层时限、repository Create/Update/scan/history/rank/claim、handler request/history/rank、前端 API 类型与两页面加载/表单、243/244 SQL 及相关测试。计划/代码/数据层职责一致；无认证放宽、新依赖、任意代码执行或破坏性迁移。

外部事实：模型真实能力和协议预算差异未核实，列为真实模型校准阶段；无其他点名对象未覆盖。七维度结论：完整性、架构、安全、数据、验证、部署和遗漏已覆盖，以上 E01-E04 为实施必须关闭的验收事项。

## 实施结果（2026-10-16）

已完成 v2 扩充，不改变既有计划默认关闭语义，不调用真实模型，不回溯修改历史 run。

| 验收项 | 结果 |
|---|---|
| 56 个题型、112 题、9 个能力域 | PASS，`GetModelBenchmarkSuite()` / `GenerateModelBenchmarkComparisonItems()` 与测试断言一致 |
| 原 24 题型 + 新增 32 题型独立复算 | PASS，共 14336 个生成实例；新增测试从模型可见 JSON 数据块独立求解 |
| 核心题型实例变化 | PASS，32 个公共批次下每个 item 均出现多个 prompt hash；拓扑、KMP、区间、并发、逻辑、规划等结构变化 |
| 公共试卷与预算隔离 | PASS，同 key/sample 同题；不同 key/sample 变化；run 保存比较键、预算、超时、采样数 |
| witness 等价正确解 | PASS，资源子集、时间窗路线、最小不一致子集、程序反例按可行性/最优性验证 |
| 覆盖率与排名门槛 | PASS（2026-10-17 调整），任何完成率都参与排名；覆盖率仅作诊断展示，未执行题合成计分行按 0 分进分母 |
| 预算截断 / 内容过滤 | PASS，直连 `finish_reason` 与流式终态事件均分类为 execution_failed，按 0 分计入能力分分母（2026-10-17 计分原则调整） |
| 集成平台预算 | PASS（本地 fake/stream 测试），WorkBuddy/Qoder/Trae 传递计划预算；OpenAI OAuth 标记 provider-managed |
| run/claim 时限 | PASS，112 × samples × timeout + 2 分钟；claim lease 覆盖完整预算 + 1 分钟；停机取消执行上下文 |
| 迁移与仓储 | PASS，245 仅加列/索引/精度；旧 run 默认 legacy；隔离 PostgreSQL 验证迁移、排行、历史、长租约与精度 |
| 管理 API / UI | PASS，比较键与预算过滤、九域能力、完成率、格式合规、预算耗尽、排名资格、诊断 run 均可见 |
| 移动端 / 浏览器 | PASS，Mocked API Chromium 桌面 + 390px 移动端，无页面错误和文档横向溢出 |

审核问题关闭状态：

- E01 已关闭：run 预算、claim 租约和收尾超时按完整题量计算。
- E02 已关闭：公共比较键与预算快照持久化，legacy run 不混入新公共卷。
- E03 已关闭：run 创建时预存计划题数，完成率以计划题数为分母。
- E04 已关闭：全部新增题型有独立 oracle、负例探针和唯一性/可行性检查。
- E05 已关闭：页面显示 112 题成本、采样与比较条件；真实模型校准仍明确 NOT_RUN。

最终命令证据见 `MODEL_CAPABILITY_BENCHMARK_QUESTION_AUDIT.md` 第 9 节。浏览器证据在 `tmp/model-benchmark-v2-browser/`（该目录按仓库规则不入库，仅保留本轮工作区证据）。
