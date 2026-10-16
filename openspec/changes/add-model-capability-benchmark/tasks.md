# Tasks

> 状态：v2 已实施（56 题型 / 112 题 / 公共比较批次）。真实模型难度校准、生产部署、传输层单次重试、CSV 导出和完整错误矩阵仍未执行；未完成项保持未勾选。

## 1. 方案冻结

- [x] 1.1 确认 `hard-v1` 48 题规模、24 个题目家族、D3-D5 难度分布和类别权重
- [x] 1.2 确认默认综合评级权重 `85% 能力分 / 10% 可见输出 Token 效率 / 5% 耗时效率`
- [x] 1.3 确认默认 Cron `0 4 * * 1`、全局并发 1 和单题超时默认 90 秒
- [x] 1.4 确认第一阶段支持的平台与协议范围：openai / Kimi / Zhipu / DeepSeek / MiniMax / OpenCode / Command Code / Cline 的 Chat Completions API Key 账号；其他平台服务端拒绝
- [x] 1.5 核对实施时最大迁移号并确认迁移文件名 `243_so_model_capability_benchmark.sql`

## 2. 数据模型与 Repository

- [x] 2.1 新增 `model_benchmark_plans`、`model_benchmark_runs`、`model_benchmark_run_items` 迁移
- [x] 2.2 添加 due plan 原子领取、running/claim_expires_at 租约、run 创建、item 写入、状态流转、滞留恢复和保留清理 SQL
- [x] 2.3 添加版本隔离、历史查询、排行聚合和指标汇总索引
- [x] 2.4 增加 migration 幂等、Repository 原子领取与集成库表存在测试
- [x] 2.5 断言计划默认停用且升级不自动触发请求

## 3. 题库与评分内核

- [x] 3.1 定义 suite、family、item、checker、difficulty、weight 和 usage budget 类型
- [x] 3.2 实现 24 个高难度题目家族和确定性参数生成器
- [x] 3.3 实现标准答案生成、跨 run 变化与 prompt hash
- [x] 3.4 实现 JSON 提取、格式校验、归一化和 checker（integer / tolerance / set / sequence / fields / exact / schema）
- [x] 3.5 实现能力分、分类分、Token 效率、耗时效率、综合评级与 run 状态规则
- [x] 3.6 覆盖 48 题标准答案自校验、非法 JSON、部分得分、集合 / 序列 / 字段、usage 捕获与执行失败不计答错

## 4. 请求执行器

- [x] 4.1 抽象账号数据源与 HTTPUpstream，支持注入 fake upstream
- [x] 4.2 实现 Phase 1 非流式 Chat Completions 请求与每题独立 timeout
- [x] 4.3 采集 usage、latency、nullable reasoning/cache token、usage field coverage 和估算成本；first token 保留 nullable
- [x] 4.4 支持温度 0、stream=false、禁用外部工具语义（不传 tools）、5xx/传输错误单次重试
- [ ] 4.5 补齐 429 / 5xx / 超时 / 响应截断错误矩阵测试

## 5. 定时 Runner

- [x] 5.1 实现独立 cron runner 与生命周期启停
- [x] 5.2 实现 due plan 扫描和 PostgreSQL `FOR UPDATE SKIP LOCKED` 领取
- [x] 5.3 实现全局并发 1、单 plan 串行执行和 run 总超时
- [x] 5.4 实现优雅停机、partial 标记、过期 claim 恢复和 panic 后释放计划
- [ ] 5.5 补齐 panic 恢复、shutdown 与多计划排队专项测试

## 6. 管理 API

- [x] 6.1 实现 suites、targets、plans CRUD、手动排队、runs、items、history、rankings API（CSV export 移入后续增强）
- [x] 6.2 校验 Cron、采样数、Token 上限、超时、保留数量和账号模型匹配
- [x] 6.3 支持 run 分页、模型 / 状态 / 计划过滤，history 的时间 / 模型 / 账号过滤和版本隔离
- [x] 6.4 API 挂载现有 Admin 鉴权，错误响应不包含标准答案或凭据
- [x] 6.5 增加 unsupported / invalid / conflict / internal 错误状态测试
- [ ] 6.6 补齐 handler 参数、权限和分页端到端测试

## 7. 前端控制台

- [x] 7.1 新增 API client、类型和 zh i18n
- [x] 7.2 新增 `/admin/model-benchmarks` 路由与侧栏入口
- [x] 7.3 实现总览指标卡与模型排行
- [x] 7.4 实现历史曲线：能力 / 效率 / Token / 耗时 / 成本指标切换和多模型对比
- [x] 7.5 实现能力拆解、计划管理和 run 详情
- [x] 7.6 展示能力分、效率分、综合评级、Token、耗时、成本和 usage missing 状态
- [ ] 7.7 增加页面交互、空态、错误态、i18n 和权限态专项 Vitest
- [ ] 7.8 后续增强：排行 sparkline、时间范围选择、题库家族预览、CSV 导出与版本切换提示

## 8. 验证与收口

- [ ] 8.1 后端 `go test -tags=unit ./...`：当前有 2 个既有全量顺序失败，专项模型测评测试通过；见 `implementation-evidence.md`
- [ ] 8.2 后端 `go test -tags=integration ./...`：全量曾出现 1 个既有并发 duplicate-key 波动，repository integration 复跑通过；见 `implementation-evidence.md`
- [ ] 8.3 `golangci-lint run ./...`：工具输出 0 issues 但因 Go 1.27 export data / 既有类型检查上下文退出 1；见 `implementation-evidence.md`
- [x] 8.4 前端 `pnpm test:run`：286 files / 2122 tests 通过
- [x] 8.5 前端 `pnpm typecheck`、`pnpm lint:check`、`pnpm build` 通过（lint 有 1 个既有 warning）
- [ ] 8.6 用 fixture 上游完成 48 题服务层 fake E2E（当前已有 48 题标准答案自校验 + 单题执行器 fake 测试）
- [ ] 8.7 真实模型小样本冒烟需单独记录环境、账号、时间和成本，不与 fixture 证据混写
- [x] 8.8 更新 `CHANGELOG-SO.md`，标注影响上游合并的文件
- [x] 8.9 复核首轮审核记录 B-01/B-02/B-03 与 S-01/S-02/S-03 均已按方案收敛

## 9. v2 智力能力扩充（2026-10-16）

- [x] 9.1 新增 32 个原创题型，扩展为 56 题型 / 112 题 / 9 个能力域
- [x] 9.2 动态化原核心题型的图、字符串、权重、并发对照、逻辑选项与输入结构
- [x] 9.3 新增抽象归纳、材料与数据、信息充分性、因果概率、代码纠错与复杂规划覆盖
- [x] 9.4 实现 `comparison_key` 公共试卷、采样序号与同题复现；旧 run 标记 `legacy`
- [x] 9.5 run 持久化输出预算、超时、采样数、完成率、格式合规率、预算耗尽与能力域覆盖
- [x] 9.6 排名按版本、公共卷、预算、超时、采样数隔离，并施加至少 3 次 / 90% 总覆盖 / 80% 域覆盖门槛
- [x] 9.7 新增 witness checker，接受满足可行性与最优性的等价反例 / 子集 / 路线
- [x] 9.8 识别直连与流式通道的 length / max_tokens / content_filter / incomplete，不把截断按答错计
- [x] 9.9 WorkBuddy / Qoder / Trae 传递计划预算；OpenAI OAuth 标记 provider-managed 诊断
- [x] 9.10 245 迁移增加比较键、预算快照、质量 JSON、索引与数值精度，并验证幂等 / legacy / 长租约
- [x] 9.11 管理 API 与前端支持比较条件、九域能力、质量指标、诊断 run 和移动端布局
- [x] 9.12 原 24 + 新增 32 题型共 14336 个生成实例独立复算通过
- [ ] 9.13 真实模型 v2 小样本冒烟、难度校准与多模型重跑
- [ ] 9.14 生产部署与真实数据库迁移验收
