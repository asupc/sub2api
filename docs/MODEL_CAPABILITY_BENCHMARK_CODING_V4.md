# 模型能力测评 v4：编程与算法强化

日期：2026-10-17（Asia/Shanghai）。本次升级根据用户“提升难度、加大编程能力测评”的要求实施。

## 版本、题量与评分契约

`hard-v1` 题库及评分均升级为 `4.0.0`，更换确定性种子盐。单份试卷由 v3 的 56 题增加至 80 题，每个题型生成一个实例；采样 1..3 对应 80/160/240 次请求。公共 `comparison_key` 保持原配置，相同版本、比较键和采样序号得到相同试卷。旧 run 及分数保留，版本隔离沿用现有实现。

新增 16 个编程题型和 8 个算法题型，均标 D5。D3/D4/D5 是设计标签，真实难度与模型区分度仍须真实模型校准。

| 能力域 | 题型数/单卷题数 | 类别权重 |
|---|---:|---:|
| 编程与代码修复 code | 25 | 40% |
| 算法与数据结构 algorithms | 15 | 20% |
| 形式逻辑 logic | 8 | 8% |
| 抽象归纳 abstraction | 6 | 7% |
| 数学组合 math | 6 | 6% |
| 规划调度 planning | 7 | 6% |
| 材料与数据 understanding | 6 | 5% |
| 精确指令 instruction | 3 | 4% |
| 因果与概率 causal | 4 | 4% |
| 合计 | 80 | 100% |

领域内继续按难度归一，避免重复乘入题量。新增结构化结果按完整答案判分；程序补丁和最小反例按语义验证，接受符合契约的等价解。

## 新增编程题型

| 题型 | 考察内容与答案契约 |
|---|---|
| slice-alias-trace | Go 全切片、append 扩容、共享底层数组与交错写入；返回 base/a/b/c 最终数组 |
| defer-unwind-trace | Go 命名返回值、defer 参数快照、闭包和逆序执行；返回结果与日志 |
| event-loop-trace | 同步任务、FIFO 微任务、微任务递归排队、每个计时器后的检查点；返回完整日志 |
| snapshot-write-skew | 共同快照、first-committer-wins 写冲突、业务约束；返回提交集、最终余额与约束状态 |
| serializability-graph | 读写冲突优先图、传递依赖与环成员；返回串行化判断与环成员 |
| cas-interleavings | 多线程读/CAS/失败重读的全部合法交错；返回失败次数对应的完整调度计数 |
| lock-cycle-members | 非可重入锁、等待环、自等待与级联阻塞；返回环成员及受阻线程 |
| lru-writeback | LRU 命中、淘汰、脏页写回；返回缺页数、写回数、缓存顺序和脏页 |
| taint-dataflow | 含环控制流的多源污点传播与 kill，求最小不动点；返回每节点位集 |
| bytecode-control-flow | 栈式字节码、循环、条件分支、局部变量；返回局部变量和执行步数 |
| signed-overflow-trace | 有限位宽补码、乘加溢出、算术/逻辑右移；返回各步有符号值 |
| struct-layout-padding | 明示 ABI 下字段、数组与尾部对齐；返回偏移、大小和填充字节数 |
| repair-lower-bound | 升降序、严格/非严格边界下补全二分程序的比较和边界更新；穷举有限输入域验证补丁 |
| repair-range-sum | 修复带循环下标的区间和公式；返回前缀下标与回绕公式参数，完整域验证 |
| minimal-bug-witness | 为有负数的错误滑窗程序构造最短反例；允许任意满足最短性和失效性质的输入 |
| sql-null-antijoin | SQL NULL 三值逻辑、NOT IN 与 NOT EXISTS 的反连接差异；返回两个有序结果集 |

## 新增算法题型

| 题型 | 考察内容与答案契约 |
|---|---|
| negative-cycle-influence | 从起点可达的负环及其下游影响；返回不可达/负无穷/有限最短距离 |
| maxflow-mincut | 有向容量网络、所有最小割；返回最大流值和最小割数量 |
| bipartite-matching | 最大二分匹配及最大匹配方案数；返回规模与数量 |
| scc-condensation | 强连通分量、凝聚 DAG、增边成强连通的最小数量；返回规范分量和最少增边数 |
| weighted-edit-distance | 非均匀替换/插入/删除与相邻交换；返回明示 OSA 递推下的最小成本 |
| lis-count | 含重复值序列的严格 LIS 长度与按下标计数；返回长度、数量 |
| range-update-query | 区间增加、闭区间和查询的交错；返回全部查询结果 |
| bitmask-shortest-path | 带钥匙、门、权重、单向边与零成本的状态空间最短路；返回到终点最短成本或 -1 |

## 验证与证据边界

新增测试必须从公开题面数据独立复算，覆盖 128 个 seed × 2 个变体，并检验错误答案。补丁覆盖有限完整输入域和错误补丁；反例覆盖等价最短解、非反例、越界、非最短输入。既有公共试卷、领域归一、运行预算和完整测评测试同步按 80 题验证。

本次验证不调用真实模型。编程题采用静态推演和受限语义补丁；不执行模型生成的任意代码，因此不能将分数等同于真实仓库工程交付能力。

## 独立答案复算方式

新编程题的 oracle 使用独立堆与切片视图、命名返回值代数求值、分层事件遍历、逐来源污点可达性、带状态队列的 CAS 调度枚举、时间戳缓存模型及 SQL 三值逻辑。算法 oracle 分别使用 Floyd-Warshall、全割枚举、匹配回溯、Kosaraju、前缀状态图最短路、全部下标子集枚举、直接区间模拟和状态图 Bellman-Ford，与生成器的参考实现形成交叉检查。所有输入从公开题面解析，不使用 `Expected` 构造答案。

新增 24 题型各覆盖 128 个 seed × 2 个变体，共 6,144 个正确性实例及 6,144 个变异错误答案。旧 24 核心题型与 32 扩展题型的独立复算也通过；三组共有 80 × 128 × 2 = 20,480 个实例。完整受限补丁语言覆盖二分 288 种和区间和 432 种组合，合计 720 种，并验证字段缺失、额外字段、非整数、无效比较、非终止、越界和语义错误。最短反例测试验证多个等价正确解以及非反例、非最短、超出值域等错误输入。

## 2026-10-17 执行证据

后端使用本机已缓存的 `golang:1.27.2` Docker 镜像，源码以只读挂载进行测试和构建；格式化阶段仅挂载修改目标。前端在 `frontend/` 使用 pnpm。日志保存在工作区 `tmp/model-benchmark-v4/`，该目录不入库。

| 命令/核对 | 结果 | 证据 |
|---|---|---|
| `go test -tags=unit ./internal/service -run '^TestModelBenchmark' -count=1 -timeout=8m` | PASS | `backend-focused.log`；包含全部新旧题型独立复算、负例、公共试卷、权重与预算 |
| `go test -tags=unit ./internal/service ./internal/handler/admin ./internal/repository ./migrations -run 'ModelBenchmark' -count=1 -timeout=5m` | PASS，4 包全部通过 | `backend-final.log` |
| `go build ./...` | PASS，最终代码复跑通过 | `backend-build-final.log` |
| `go vet -tags=unit ./internal/service ./internal/repository ./internal/handler/admin ./migrations` | PASS | `backend-vet.log` |
| `pnpm exec vitest run src/views/admin/__tests__/ModelBenchmarkView.spec.ts src/views/admin/__tests__/ModelBenchmarkPlansView.spec.ts` | PASS，8 项 | `frontend-focused.log`；验证 v4/80 题、40%/20% 权重和 80×采样成本提示 |
| `pnpm exec vitest run src/i18n/__tests__/localeKeyCompleteness.spec.ts` | PASS，2 项 | 初轮三文件 10 项通过，`pnpm build` 再次包含该检查 |
| `pnpm exec eslint src/views/admin/model-capability/tabs/OverviewTab.vue src/i18n/locales/zh/admin/modelBenchmark.ts src/views/admin/__tests__/ModelBenchmarkView.spec.ts src/views/admin/__tests__/ModelBenchmarkPlansView.spec.ts` | PASS | `frontend-eslint.log` |
| `pnpm build` | PASS，含 i18n、vue-tsc 与 Vite | `frontend-build.log` |
| `golangci-lint run --timeout=5m ./internal/service ./internal/repository` | BLOCKED，已执行且退出 1，未取得有效 lint 结论 | `backend-lint.log`；v2.13.0（Go 1.27.0 构建）读取 Go 1.27.2 导出数据报 `export data version 5 is greater than maximum supported version 4`，随后出现类型加载错误 |
| `git diff --check` / `UPSTREAM-BASE` 核对 | PASS | 无空白错误，基准文件与 HEAD 一致；上游只读仓库工作区干净 |
| 真实模型难度校准、任意生成代码运行、真实数据库集成、浏览器视觉验收、生产部署 | NOT_RUN | 本次仅静态题库和本地单测/构建验收；UI 通过组件测试 |

相关包第一次测试记录在 `backend-related.log`：已有未提交的推理字段改动将计划 Scan 增至 22 列，旧 sqlmock fixture 仅提供 20 列，复现 `sql: expected 20 destination arguments in Scan, not 22`。测试桩已补齐并验证字段透传；代码核对同时发现 `UpdatePlan` 的 `$16/$17` 没有传入对应参数、RETURNING 缺两列，已作最小修复并添加 `TestModelBenchmarkRepositoryUpdatePlanKeepsReasoningFields`。最终四包复跑通过，未覆盖或撤销原有推理级别功能。
