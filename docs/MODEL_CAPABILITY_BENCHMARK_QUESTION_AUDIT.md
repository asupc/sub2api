# 模型能力测评题库正确性核实记录

- 核实日期：2026-10-16（Asia/Shanghai）。
- 范围：`hard-v1` 全部 24 个题型 × 2 个变体 = 48 题，包含动态参数、题面、标准答案、输出约定、checker 与实际运行版本标签。
- 修订版本：第 1-6 节记录历史题库 `1.3.0` / 评分 `1.1.0` 的修复；第 7-9 节记录当前 v2 题库 `2.0.0` / 评分 `2.0.0`。
- 证据边界：代码与确定性测试核实；没有调用真实模型、修改历史 run 或验证生产部署。本轮核实前工作区已有的固定位置排列公式修复一并复核并保留。

## 1. 已复现的答案错误与歧义

| 问题 | 核实证据 | 修订 |
|---|---|---|
| 固定位置排列数错误（本轮开始前已有修复） | n 个不同元素中指定 k 个固定原位置，剩余元素之间任意一一对应，计数为 `(n-k)!`；`n!/k!` 不适用 | 保留已有公式修复；新增基于排列状态枚举的独立复算 |
| `interval-dp` 未计算最优合并成本 | n=7、值 1..7，穷举合法相邻合并序列得到 74；旧答案为 168。全部可能的 n=6/7/8/9，正确成本分别为 51/74/102/135，旧值为 105/168/252/360 | 使用真正的区间 DP；计算最优成本及所有最优方案中可作为首次合并的最小原始左下标；去除题面直接给出下标 0 的提示 |
| `inclusion-exclusion` 错把交集周期当乘积 | 在 1..70 中不能被 6、7、15 整除的整数共 48 个，旧答案为 46；6 和 15 不互质，交集周期是 lcm(6,15)=30 | 所有二重、三重交集使用最小公倍数 |
| `epistemic-deduction` 标准答案随机选择 | 旧题要求恰好一人持钥、Ben 无钥，且 Ben/Cai 持钥状态不同，只能推出 Cai；已复现旧期望却为 Ben | 答案由条件决定；变体 1“不同”唯一推出 Cai，变体 2“相同”唯一推出 Ada |
| `token-bucket` 未定义令牌不足时如何处理 | 初始 5、容量 8、补充 1、请求 [4,3,3]，旧实现输出 [1,2,0,1,0,1]；若按整次请求拒绝，应是 [1,2,2,3,0,1]。旧题没有提供选择这两种语义的依据 | 明确动作时刻、补充上限和“足额才扣除，不足拒绝且桶状态不变”，实现同步修改 |
| 布尔赋值条件不完整 | 旧题条件没有定义 q 的取值，却要求 q=false；题面还直接给出答案 | 变体分别要求 XOR 为真且 p→q / q→p 为假；穷举 4 种赋值，每题恰有一个解 |
| 不变量、数值稳定性缺少定义 | 旧题只有选项名称，没有 lower-bound 循环、候选不变量或待计算公式，无法从题面验证标准答案 | 给出循环、边界和每个选项的数学含义；给出 binary64 下的具体根式及三个候选公式 |
| 多题泄露答案或考察目标不成立 | 并发题直接给出缺陷类型和 safe=false；清理顺序题要求照抄题面顺序 | 并发题只给缺陷枚举及字段语义；清理顺序改为根据 Go defer 注册顺序推导执行顺序 |
| 输出定义不唯一 | KMP“失配函数”存在不同表定义；模逆元存在多个同余代表；坐标集合未明确 JSON 形状；调度未明确下标与字典序 | 明确 pi 的数学定义、最小非负逆元、字符串数组、0-based 作业下标及整数数组比较规则 |

## 2. 全部题型逐项核实

下表的“独立复算”从题面提取输入，不读取生成器的 `Expected` 来构造答案，再比较标准答案并经过实际 checker 判分。对于语义选择题，另用定义、穷举状态或高精度计算验证选项成立。

| # | 题型 | 正确答案/唯一性依据 | 独立复算与处理结果 |
|---|---|---|---|
| 1 | `modular-exponentiation` | 非负模幂唯一 | 使用 int64 二进制快速幂，对照生成器 big.Int.Exp；中间乘积在范围内 |
| 2 | `crt-remainder` | 两个不同区间的质数模数互质，在乘积范围内解唯一 | 按第一个同余的等差数列搜索第二个条件，对照逐整数搜索 |
| 3 | `constrained-permutations` | `(n-k)!` | 用元素到位置的子集状态计数复算；保留已有修复 |
| 4 | `shortest-path` | 最短距离，再按节点序列字典序消除并列 | Floyd-Warshall 距离与按距离还原字典序路径，对照生成器 DFS |
| 5 | `topological-order` | 每一步选择入度为 0 的最小字母，得到 A,B,D,C,E | 穷举节点排列并过滤全部依赖边，取字典序最小合法排列 |
| 6 | `interval-dp` | 相邻合并区间递推；同成本下按首次合并原始左下标消除并列 | 穷举合并动作序列并缓存剩余状态，独立复算成本和首次下标；已修复 |
| 7 | `cycle-detection` | 变体 1 有 2→3→4→2，变体 2 无环 | 用传递闭包检查对角线可达性 |
| 8 | `kmp-prefix` | 每个前缀的最长相等真前缀/后缀长度 | 逐一枚举前后缀长度，对照线性前缀函数算法；已澄清定义 |
| 9 | `sparse-matrix` | 1-based 坐标满足全部行列和模约束 | 枚举整个 7×7 矩阵，而生成器枚举候选子区域；明确 JSON 字符串数组 |
| 10 | `token-bucket` | 按题面 6 个明确动作模拟 | 独立按每个请求/补充事件模拟，包含令牌不足分支；已消除歧义 |
| 11 | `modular-inverse` | 质数模数下 a 非零，范围 [0,m) 内逆元唯一 | 穷举候选并检验 a*x mod m=1，对照 big.Int.ModInverse；限定最小非负代表 |
| 12 | `markov-expected-steps` | 从 S 开始，E=1+pE，即 E=1/(1-p) | 独立求生存概率级数 1+p+p²+…；题面明确起点、转移及绝对容差 |
| 13 | `inclusion-exclusion` | 交集周期由最小公倍数确定 | 直接逐整数检查所有整除条件，对照容斥结果；已修复 |
| 14 | `two-machine-schedule` | 枚举分配后最小化最大负载，按整数数组字典序消除并列 | 枚举作业排列与分割点，对照位掩码子集算法；明确不可抢占与下标语义 |
| 15 | `concurrent-race` | 写入与接收前的并发读取之间缺少 happens-before 排序 | 对照 Go 内存模型；核实缺陷类型、safe 与写入行号；去除答案提示 |
| 16 | `pseudocode-trace` | 按明确奇偶规则追踪至 1，排除起始值、包含终点 | 独立模拟全部生成起点范围；补全 Collatz 规则 |
| 17 | `loop-invariant` | 已排除区间满足 <target / >=target，且 0<=lo<=hi<=n | 穷举长度 0..6 的非降数组（值域 {-1,0,1}）及目标 -2..2，验证初始化、保持、终止与返回值；已补齐题意 |
| 18 | `greedy-counterexample` | 贪心 4+1+1 共 3 枚，最优 3+3 共 2 枚，差 1 | 动态规划复算最少币数；明确硬币无限及贪心策略 |
| 19 | `truth-assignment` | 蕴含为假要求前件真、后件假，同时满足 XOR | 穷举 4 种布尔赋值并断言唯一解；已重写完整约束 |
| 20 | `strict-schema` | token、enabled、count 与外层 answer 结构全部匹配 | 解析题面完整目标 JSON；额外字段、缺失字段、重复键、错误类型、前后说明和围栏均有回归；已修复 |
| 21 | `epistemic-deduction` | 按所有为真的陈述枚举持钥者 | 穷举 Ada/Ben/Cai 的三种唯一持有情况，断言恰有一个解；已修复 |
| 22 | `error-chain` | 同一函数内 defer 按注册的逆序执行 | 对题面注册列表逆序求解，并对照 Go 规范；已从照抄顺序改为推演 |
| 23 | `pareto-frontier` | 成本和延迟均最小化，保留不被支配的 5 个二元组 | 解析题面元组逐对检查支配关系，排除 [9,8]；明确优化方向 |
| 24 | `numerical-stability` | 根式差与倒数形式实数等价，倒数形式避免近数相减 | x=10^16..10^20，以 256-bit 高精度直接计算原式作为参考；正确公式相对误差 <=1e-14，binary64 直接相减结果为 0；已补齐公式 |

## 3. 判分核实与修订

1. 普通正确性题按最后一个合法 JSON 对象判定，容忍围栏/说明文字，正确处理多个 JSON 对象和字符串里的花括号。末尾对象缺少 answer 时，不回退到更早的答案。
2. 使用 `json.Number` 保留数字精度。大于 2^53 的两个不同整数不再因 float64 舍入被误判相同；保留 int64 边界，拒绝溢出和非整数。42、42.0、4.2e1 按相同整数判定，嵌套下标数组同理。
3. 字符串与数字不再交叉混同，例如数字 42 不能通过字符串枚举答案 "42"。
4. 集合忽略顺序但不接受重复项替代缺失项；序列保留顺序；多字段题保留字段部分分并提供确定性的错误字段说明。
5. 精确格式题检查整个响应，仅允许外层 answer；递归拒绝额外字段、缺失字段、重复键、错误值和错误类型，不因部分字段正确而给部分分。
6. 每次 run 记录当前真正生成和评分的版本，防止旧计划把新题库结果标为旧版；计划创建时的原始版本元数据不回写。

## 4. 验证记录

修复前已运行回归并观察到失败：

- `TestModelBenchmarkIndependentAnswers/(interval-dp|inclusion-exclusion|epistemic-deduction|token-bucket)`：复现第 1 节列出的成本、整除计数、随机持钥者和不足令牌差异。
- `TestModelBenchmarkScoringAudit`：复现大整数混同、字符串/数字混同、多对象提取失败、严格格式限制失效。
- `TestModelBenchmarkRunRecordsExecutedVersion`：旧计划版本 1.1.0 被直接复制到实际执行新实现的 run。

修复后验证：

| 检查 | 覆盖范围 | 结果 |
|---|---|---|
| `go test -tags=unit ./internal/service -run 'Test(ModelBenchmark|GeneratedBenchmark)' -count=1` | 24×2×128=6144 个独立复算实例及测评专项测试 | PASS |
| `go test -tags=unit ./internal/service ./internal/handler/admin ./internal/repository -count=1` | 受影响业务层及管理接口/仓储单元测试；包含新增 33 个判分样例、所有区间规模、不变量与浮点边界 | PASS（service 195.800s；admin 0.447s；repository 9.121s） |
| `go build ./...` | 后端编译 | PASS |
| `git diff --check` | 改动空白与冲突标记检查 | PASS |
| `golangci-lint` | lint | NOT_RUN（容器未安装命令；不替代为通过结论） |
| 真实模型重跑/生产验收 | 外部模型、运行时部署 | NOT_RUN（本轮没有发起真实测评请求） |

测试运行环境：现有开发容器 `sub2api-backend-dev`，Go `go1.27.2 linux/amd64`，使用挂载的 `sub2api-so/backend` 源码。仓库测试通过不代表生产实例已加载新代码。

## 5. 历史分数与后续使用

> 本节写作时当前版本为 1.3.0，仅描述那次修复后的过渡状态；该状态已被第 7-9 节的 v2 取代。

- 历史 run 不回溯改写，以保留当时题面、响应和判定证据。
- 题库 1.1.0 的固定位置排列错误，以及本轮发现的区间 DP、非互质容斥和随机答案错误，会污染历史能力分。仅修过排列题的 1.2.0 仍未消除另外三项错误；这些旧分数不应当作可靠的新旧能力变化证据。
- 部署新实现后，重新执行需要比较的模型计划，以题库 1.3.0、评分 1.1.0 的结果进行同版本比较。排行榜和默认历史查询按版本过滤；模型清单的最近分数接口保留旧记录并携带其原始版本。
- 本核实验证题意、答案及判分行为，不提供题目难度校准、覆盖面充分性、抗题库记忆或综合智力结论。部分题型只变化案例编号，仍存在重复内容；不能把 48 个条目视为 48 个独立能力测量维度。

## 6. 公式与语言语义参考

以下资料仅用于定义和推导交叉核对；修复是否生效以本仓库独立测试证据为准。

- [NIST DLMF §27.15：中国剩余定理](https://dlmf.nist.gov/27.15)：互质模数与唯一同余解。
- [MIT 6.042：Inclusion-Exclusion](https://courses.csail.mit.edu/6.042/past-devel/archive/spring03/handouts/lectures/lec19.pdf)：集合交并计数；整除集合的交集由公倍数刻画。
- [Go Memory Model](https://go.dev/ref/mem)：数据竞争与通道的同步先后关系。
- [Go Language Specification：Defer statements](https://go.dev/ref/spec#Defer_statements)：defer 注册与逆序执行。
- [Goldberg：What Every Computer Scientist Should Know About Floating-Point Arithmetic](https://docs.oracle.com/cd/E19957-01/806-3568/ncg_goldberg.html)：舍入及相近数相减的消减误差。
- [University of Hawaiʻi：String Matching / KMP](https://courses.ics.hawaii.edu/ReviewICS311/morea/230.string-matching/reading-notes.html)：前缀函数的最长真前后缀定义。
- [RFC 2697](https://www.rfc-editor.org/rfc/rfc2697.html)：令牌是否足够影响是否扣除；本题的时刻与单桶拒绝规则以修订后明确题面为准。

## 7. v2 新增 32 个题型的正确性核实（2026-10-16）

新增题目均为原创、确定性生成、机器可判定；题面 JSON 数据块只包含模型可见事实，不包含隐藏参数。独立复算读取该公开数据块，用与生成器不同的枚举、动态规划、因果人群计数或语义推理求解，再通过实际 checker 判分，并附带一个错误答案负例探针。

| # | 题型 | 唯一性 / 正确性依据 | 独立复算方法 |
|---|---|---|---|
| 25 | `affine-rule` | 示例限定 a∈2..7、b∈-7..8，枚举全部候选规则并断言唯一输出 | 穷举 240 个 `(a,b)`，重算 query |
| 26 | `symbolic-substitution` | A/B/C/D 到 w/x/y/z 的一一对应，24 个排列中示例唯一确定输出 | 全排列枚举并验证示例 |
| 27 | `sequence-transduction` | 规则空间限定反转、循环移位 k∈0..2、增量 d∈-2..2；示例唯一确定输出 | 30 个候选规则全枚举 |
| 28 | `grid-transform` | 八种刚性对称；3x3 示例唯一确定 4x4 query 的输出 | 独立坐标映射枚举八种对称 |
| 29 | `relation-composition` | 两个确定性函数按显式操作序列复合，结果唯一 | 从 start 逐步查表复合 |
| 30 | `finite-state-inference` | 16 个二字符转移表；空串与长度 1..3 全部 15 个示例确定输出等价类 | 枚举 16 个转移表并断言唯一输出 |
| 31 | `relational-order` | 偏序条件在所有满足约束的全序上投影 | 穷举 6 个对象全排列，判断 before/after/unknown |
| 32 | `conditional-rules` | Horn 规则最小不动点唯一 | 独立递归可达性求解 |
| 33 | `quantified-logic` | 有限完整真值表上 forall/exists 语义确定 | 逐行检查全称/存在量词 |
| 34 | `constraint-entailment` | x,y,z∈0..3 全枚举可判定 entailed/refuted/unknown/inconsistent | 64 个赋值全枚举 |
| 35 | `inconsistent-statements` | 输出最小不一致下标集合；witness checker 只要求同样最小且确无解 | 独立按子集大小递增枚举 0..10 |
| 36 | `missing-information` | 缺失字段不当作 0；枚举 x,y 全部可能值判断 unique/unknown/inconsistent | 穷举 domain 0..5 |
| 37 | `causal-intervention` | intervene 替换 X 方程，observe 条件于 X=value；U 分布保持 | 100 个个体加权人群直接平均 |
| 38 | `causal-counterfactual` | 观测固定个体 U，反事实保持 U 不变 | 由观测方程解 U，断言唯一后代入 new_x |
| 39 | `bayes-diagnosis` | 贝叶斯公式；生成概率均为整数百分比 | 10,000 人计数人群交叉表 |
| 40 | `simpson-confounding` | 分层成功率与汇总成功率均为观测统计，不作曲果断言 | 分层比例与交叉乘法比较汇总比例 |
| 41 | `ledger-reconciliation` | void 记录忽略；收入/支出净额唯一 | 独立累计 credits/debits |
| 42 | `multi-hop-records` | 精确编号三次关联，干扰记录不参与 | 逐表查找 |
| 43 | `unit-conversion` | 千米/米与分钟/秒换算后求平均速度 | 独立单位换算 |
| 44 | `weighted-rate` | 合并样本比例按总数加权，不是组比例平均 | 权重分子分母直接合并 |
| 45 | `long-context-join` | 每仓取未取消的最大版本，无法把多版本相加 | 按版本降序稳定排序后取每仓首条 |
| 46 | `versioned-policy` | 下单日选择最大 effective_day 的已生效政策 | 政策按生效日降序匹配 |
| 47 | `trace-loop-boundary` | 伪代码循环、continue 与求和语义显式 | 独立按步长和模数模拟 |
| 48 | `expression-evaluation` | 逆波兰表达式求值唯一 | 独立栈求值 |
| 49 | `bug-counterexample` | 返回任意能使 buggy lower-bound 与正确结果不同的案例；正确结果用标准 lower-bound | 每案例独立枚举首个 >=target 与 buggy 结果比较 |
| 50 | `proof-error` | 线性方程等价变形有确定交叉乘法判据；要求首个错误 | 独立等价性交叉检验 |
| 51 | `dependency-schedule` | DAG 依赖下无限机器最早完成时间唯一 | 记忆化 DFS 求最早完成 |
| 52 | `resource-allocation` | 0/1 背包三维约束最大化 value；witness checker 接受任意达到最优值的可行子集 | 独立子集树枚举 |
| 53 | `route-with-windows` | 4 点访问排列仅 24 种；早到等待、晚于窗口不可行，最小化完成时刻 | 独立子集 DP 保存最早完成与路径 |
| 54 | `inventory-policy` | 到货、需求、丢失销售、阈值订货与 lead time 的顺序显式 | 逐日事件模拟 |
| 55 | `distractor-extraction` | 材料中 quoted_text 是数据不是指令；唯一 target 记录 | 遍历 role/id 提取 token |
| 56 | `nested-constraints` | 筛选、排序、截断和输出字段均显式 | 独立重复选择最大分/字典序结果 |

核心 24 题型在 v2 中进一步动态化：拓扑图、环边、KMP 字符串、区间权重、并发安全/竞态对照、不变量选项、贪心面额、布尔蕴含方向、持钥陈述、defer 注册顺序、Pareto 输入和数值稳定性选项均跨实例变化。独立复算测试继续检查原 24 题型，避免新增题型破坏既有答案。

## 8. v2 比较与判分语义核实

1. `GenerateModelBenchmarkComparisonItems(key, sample)` 只依赖比较键与采样序号；同键同序号跨 plan/run 完全同题，不同键或采样序号生成不同试卷。数据库同时保存 run 的比较键、输出预算、单题超时与采样数。
2. 能力分按题型难度和九个能力域权重归一计算；每个能力域总分占比等于配置权重，不因题型数量重复放大。
3. 提前结束时以 run 创建时写入的计划题数为分母，不再把已尝试题数误当完整卷；有效完成率排除格式错误。
4. 传输失败、预算耗尽、内容过滤不按答错计分；直连 Chat Completions 读取 `finish_reason`，流式通道额外捕获终态 stop/incomplete 事件。
5. WorkBuddy / Qoder / Trae 集成路径传递计划输出预算；OpenAI OAuth/Codex 因协议不接受统一下发预算，记录 `max_output_tokens=0` 并标记 provider-managed，仅诊断不进入固定预算排名。
6. 默认排名过滤 suite/scoring、比较键、输出预算、超时、采样数，要求至少 3 次、总有效完成率 >=90%、每个能力域覆盖 >=80%，并显示分数标准差。
7. 多解优化题使用 witness checker：验证候选方案可行、目标值达到独立参考最优值，接受不同正确子集/路径/最小不一致集合，避免把等价正确答案误判为错误。

## 9. v2 验证与证据边界

| 检查 | 覆盖 | 结果 |
|---|---|---|
| `go test -tags=unit ./internal/service -run 'Test(ModelBenchmark|GeneratedBenchmark)' -count=1` | 原 24 题型 6144 实例 + 新增 32 题型 8192 实例，共 14336 个独立复算；公共试卷、权重、覆盖、witness、预算、截断 | PASS |
| `go test -tags=unit ./internal/service ./internal/repository ./internal/handler/admin ./migrations -count=1` | 受影响业务层、仓储、管理 API、迁移内容测试 | PASS |
| isolated PostgreSQL fixture: `SUB2API_BENCHMARK_FIXTURE_DSN=... go test -tags=unit ./internal/repository -run '^TestModelBenchmarkComparisonPostgres$' -count=1` | 243/244/245 顺序执行、245 二次执行、legacy 默认值、比较键/预算过滤、排名标准差、长租约、精度列 | PASS |
| `go build ./...` | 后端编译 | PASS |
| `pnpm --dir frontend typecheck` | 前端类型 | PASS |
| `pnpm --dir frontend exec vitest run ...ModelBenchmarkView.spec.ts ...ModelBenchmarkPlansView.spec.ts` | 8 个前端用例：共享比较条件、新能力域、诊断可见性、计划比较键 | PASS |
| `pnpm --dir frontend exec eslint <指定文件>` | 本次测评前端改动 | PASS |
| `pnpm --dir frontend build` | i18n key、类型与生产构建 | PASS |
| Mocked API Chromium desktop/mobile | 比较条件过滤、公共批次保存、九域能力、诊断 run、移动端无横向溢出、无页面错误；截图在 `tmp/model-benchmark-v2-browser/` | PASS（仅模拟 API，不代表真实后端/模型） |
| `go test -tags=integration ./internal/repository -run '^TestModelBenchmarkMigrationCreatesCoreTables$' -count=1` | isolated PostgreSQL integration: `SUB2API_INTEGRATION_EXTERNAL_DSN=... go test -tags=integration ./internal/repository -run '^TestModelBenchmarkMigrationCreatesCoreTables$' -count=1` | 启动迁移链创建三张核心表并包含比较键与预算/质量列 | PASS |
| `git diff --check` | 空白与冲突标记 | PASS |
| `golangci-lint` | lint | NOT_RUN（容器未安装命令） |
| 真实模型重跑 / 难度校准 / 生产部署 | 外部模型、真实账号、生产实例 | NOT_RUN |

测试环境：后端容器 `sub2api-backend-dev`（Go 1.27.2）与隔离 `postgres:16-alpine` fixture；前端使用仓库 pnpm 依赖。真实模型分数、题目难度校准和生产迁移状态不能由上述本地测试推出。
