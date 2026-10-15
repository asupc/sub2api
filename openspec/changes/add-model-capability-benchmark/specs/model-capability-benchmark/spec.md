## Purpose

提供可定时执行、服务端程序自动判定、支持高难度题集、效率指标与历史趋势的模型能力测评能力，并保持其与账号健康检查的语义隔离。

## ADDED Requirements

### Requirement: 第一版仅开放受支持的文本 Chat Completions 测评目标
系统 MUST 维护模型测评适配支持矩阵。第一版只允许创建通过 OpenAI 兼容 Chat Completions 直连的 API Key 账号目标，覆盖 openai、Kimi、Zhipu、DeepSeek、MiniMax、OpenCode、Command Code 与 Cline；OpenAI OAuth/Codex、Claude/Anthropic、Gemini、Grok、Antigravity、WorkBuddy/Qoder/Trae、TypeSafe 与媒体/Realtime 模型 MUST 在服务端拒绝创建并在管理端显示不支持。
#### Scenario: 不支持的平台
- **WHEN** 管理员选择 Phase 1 支持矩阵之外的账号或模型创建计划
- **THEN** 系统 MUST 返回 400，错误码为 `MODEL_BENCHMARK_UNSUPPORTED_TARGET`
- **THEN** 系统 MUST NOT 保存计划或发起请求

### Requirement: 系统提供默认关闭的模型能力测评计划
系统 SHALL 支持管理员创建绑定 `model_id + account_id + suite_id` 的模型能力测评计划，并包含 Cron、采样次数、单题 max tokens、单题 timeout、结果保留数量和启用状态。新计划 MUST 默认停用；系统升级后 MUST NOT 自动发起测评请求。计划 MUST 记录创建时生效的 `suite_version` 与 `scoring_version`。

#### Scenario: 默认不产生上游流量
- **WHEN** 管理员创建测评计划且未显式启用
- **THEN** 系统 MUST 保存计划但不调度执行
- **THEN** 系统 MUST 不发起任何上游模型请求

#### Scenario: 计划绑定固定账号
- **WHEN** 计划执行
- **THEN** 系统 MUST 使用计划指定的账号和模型发起测评请求
- **THEN** 系统 MUST 记录 requested model、upstream model、账号 ID 与平台信息

### Requirement: hard-v1 提供高难度动态题集
系统 SHALL 内置 `hard-v1` 套件，包含 24 个题目家族和每个家族 2 个动态实例，共 48 题；题目难度 MUST 限于 D3-D5。题目家族 MUST 覆盖高级数学与组合、算法与复杂度、多约束逻辑、代码缺陷与符号执行、规划调度图论、精确指令与抗干扰。每个运行实例 MUST 由确定性参数生成，且标准答案 MUST 由服务端生成器同步计算。

#### Scenario: 动态生成同一 run 内可复现
- **WHEN** 同一个 run 生成题目实例
- **THEN** 相同输入生成的题目参数和标准答案 MUST 确定性一致
- **THEN** 系统 MUST 保存题目家族、难度、权重和 prompt hash

#### Scenario: 避免固定公开原题
- **WHEN** 生成题目
- **THEN** 系统 MUST NOT 直接复制公开基准原题作为固定题干
- **THEN** 数字、图结构、约束、集合或变量参数 MUST 可跨 run 变化

### Requirement: 服务端程序自动判定答案
系统 MUST 使用服务端纯程序逻辑判定每题答案，支持 JSON Schema、精确值、整数、数值容差、无序集合、有序序列和多字段部分得分。系统 MUST NOT 使用 LLM-as-judge。非法 JSON 或关键字段缺失 MUST 只影响对应题目得分；上游不可用、网络失败和超时 MUST 标记为执行失败，不得按模型答错处理。

#### Scenario: 数值容差
- **WHEN** 题目 checker 为 `number_tolerance` 且答案在配置容差内
- **THEN** 该题 MUST 获得满分

#### Scenario: 上游请求失败
- **WHEN** 单题请求因网络错误或上游不可用未得到答案
- **THEN** 系统 MUST 记录 execution failed
- **THEN** 该题 MUST NOT计入模型答错
- **THEN** run MUST 标记 failed 或 partial

### Requirement: 能力分与效率分分离
系统 SHALL 输出能力分、效率分和综合评级。能力分 MUST 只由答案正确性计算，MUST NOT 被 Token 用量或耗时改变。效率分的默认可比项 MUST 基于可见输出 Token 与耗时预算计算；总 Token、推理 Token、缓存 Token 和成本 MUST 作为独立观测指标展示。综合评级默认 MUST 使用 `85% 能力分 + 10% Token 效率 + 5% 耗时效率`，且管理端 MUST 同时展示三者，不得只用综合评级替代能力分。

#### Scenario: 高分但输出超预算
- **WHEN** 模型全部答对但输出 Token 显著超过预算
- **THEN** 能力分 MUST 为 100
- **THEN** 可见输出 Token 效率分和综合评级 MUST 下降

#### Scenario: 答案错误但响应很快
- **WHEN** 模型快速返回错误答案
- **THEN** 耗时效率可以是高分
- **THEN** 能力分仍 MUST 按错误答案计 0

### Requirement: 系统记录逐题资源用量与耗时
系统 MUST 为每道成功获得响应的题目尝试解析并记录可用的输入 Token、输出 Token、推理 Token、缓存读写 Token、总 Token、总耗时、估算成本、usage 来源和 usage 字段覆盖。usage 缺失时系统 MUST 标记 `missing`，MUST NOT 虚构数值；该 run 的效率分 MUST 显示为不可比。推理 Token、缓存 Token 和 first token latency MUST 使用 nullable 字段；上游不返回对应字段时 MUST 仅降低 coverage，MUST NOT 视为请求失败。

#### Scenario: 上游返回 usage
- **WHEN** 上游响应包含 usage 字段
- **THEN** 系统 MUST 解析并逐题落库
- **THEN** run 汇总 MUST 使用同一 usage 来源口径

#### Scenario: 上程缺少 usage
- **WHEN** 响应没有可用 usage
- **THEN** 系统 MUST 记录 usage source 为 missing
- **THEN** 系统 MUST NOT 生成可比较的效率分

### Requirement: 定时执行具备原子领取、租约恢复与低并发控制
系统 SHALL 提供独立模型能力测评 runner，按每分钟扫描 due plan 并按 Cron 调度。多实例部署时系统 MUST 使用 PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取，并在领取事务中将计划置为 running、写入 `claim_expires_at`、推进 `next_run_at`。进程崩溃后的过期 running 计划 MUST 能被重新领取或标记失败，MUST NOT 永久滞留。系统 MUST 支持全局测评并发上限，默认并发为 1；单计划内题目默认串行。每题 MUST 拥有独立 timeout。

#### Scenario: 多实例同时扫描
- **WHEN** 两个实例同时发现同一 due plan
- **THEN** 只有一个实例 MUST 成功领取并执行
- **THEN** 另一个实例 MUST 不发起该 plan 的上游请求

#### Scenario: 单题超时
- **WHEN** 某题执行超过计划配置的单题 timeout
- **THEN** 系统 MUST 取消该题请求并记录 timeout
- **THEN** 后续题目按 run 策略继续或停止，且状态 MUST 可追溯

### Requirement: 历史曲线必须按版本隔离
系统 MUST 保存每次 run 的总分、分类分、Token、耗时、成本、成功率和状态，并提供历史曲线查询。历史比较 MUST 绑定相同 `suite_id + suite_version + scoring_version`。管理端 MUST 支持能力分、分类分、输出 Token、总 Token、平均耗时、P95 耗时、估算成本和多模型对比曲线。

#### Scenario: 套件版本升级
- **WHEN** 套件或评分版本变化
- **THEN** 新旧 run MUST NOT 默认聚合到同一条可比曲线
- **THEN** 管理端 MUST 明确标识版本切换

#### Scenario: 单次 run
- **WHEN** 某模型只有一个成功 run
- **THEN** 曲线可以展示该点
- **THEN** 默认排名 MUST 标记其为 single run 而不是稳定排名

### Requirement: 管理端提供可视化测评控制台
系统 MUST 在后台提供模型能力测评页面，包含总览指标、模型排行、历史曲线、能力拆解、题库预览、计划管理、run 详情和逐题判定原因。排行 MUST 展示能力分、效率分、综合评级、Token、耗时、成本和最近趋势。run 详情 MUST 能解释每题得分、Token、耗时和错误类型。

#### Scenario: 查看错误答案原因
- **WHEN** 管理员打开某题详情
- **THEN** 系统必须展示模型输出、checker 类型、得分和错误原因摘要
- **THEN** 系统 MUST NOT 向普通用户接口暴露标准答案

#### Scenario: 多模型对比
- **WHEN** 管理员选择多个模型和同一指标
- **THEN** 历史曲线 MUST 以不同颜色区分模型并展示图例与数值点

