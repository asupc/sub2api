# sub2api-so 二开变更记录

> 规则：二开变更（无论是否经 `sub2api-upstream-sync` 技能）都必须记录于本文件，新条目追加到文件末尾，每条含：日期、功能描述、改动文件/模块、是否影响与上游合并。
> 基准：`UPSTREAM-BASE`（单行完整哈希）只在完整上游同步后前移；手动移植只记本文件、不改基准（唯一例外见 10-13 收口条目）。
> 整理记录：2026-10-12 / 10-13 / 10-15 / 10-23 / 10-26 五次压缩，只保留功能事实、关键决策、合并冲突面与已知缺口；根因推导、测试输出、逐提交哈希与逐文件清单见 git 历史（压缩前 HEAD：10-26=`145baaa`、10-23=`82fc11f`、10-15=`f5aa70c`、10-13=`45cf4c6`；10-23 起模型测评历史收敛为单条）。10-26 压缩把构建/部署流水收敛为「构建链与部署流水」节，PORTER-BASE 历史批次按功能事实收敛。

## 当前状态（2026-11-17 核实）

| 项 | 状态 |
|---|---|
| 上游基准 | `UPSTREAM-BASE`=`15871f90`（v0.2.22）；`backend/cmd/server/VERSION`=0.2.22 |
| 移植源基准 | `PORTER-BASE` 文件：workbuddy `6e1e548`（workbuddy2api-hub v1.10.11）、qoder `70ba58b`（v1.7.30）、trae `dfa5bf7`（2026.11.51421）（2026-11-06 推进） |
| 后端 | 代码 = v0.2.20 + 二开（三平台源码集成、Antigravity 直接模型模式、复合分组同族多平台调度、推理档位全量透传、OpenCode reasoning 回放收口、账号/计费/网关定制，逐项见时间线）；模型能力测评已于 10-23 整体移除 |
| 前端 | 二开：功能入口裁剪 + UI/UX 体系重构 + 仪表盘/用量统计/账号表格增强；模型能力中心收敛为模型清单单页（无评分）；用量统计含「按 API 统计用量」与模型分布折叠 |
| 数据库 | 官方迁移序列终态（裁剪期 drop 的表由 `deploy/restore-upstream-schema.sql` 恢复，可重放幂等）；二开迁移命名 `NNN_so_*.sql`；上游 v0.2.22 已占用 251（`251_group_force_openai_ultrafast.sql`、`251_group_usage_rollup_nonblocking_build.sql`），后续二开迁移从 252 起 |
| 验证基线 | 最近完整验证：2026-11-11（同步 v0.2.21 后）后端 build/vet 全通过、unit 仅 6 个已知存量失败（Gemini thinking variant ×2 子例 / 复合分组族 ×2 / antigravity 目录跳过 / effort 透传遗留 / prompt-audit 路由覆盖）；前端 typecheck + vitest（297 文件 / 2343 用例）+ build 全绿。**2026-11-17 同步 v0.2.22 时本机无 go/docker/pnpm，未复跑，需按下方条目命令补验** |
| 线上实例 | `sub2api:0.2.17-so-20261026-2`（本机 Compose `/vol2/1000/docker/sub2api/`；旧 tag 均保留作回滚点） |

- **功能边界**：支付永久不同步（2026-10-08 用户指示；上游 #7881 EasyPay 回调签名修复未同步，启用支付前需补）；en 语言包永久跳过；前端已删入口的官方功能视为隐藏（后端接口与表随官方保留）。
- **运行约束**：`step_up_enabled` 禁止开启（二开无 TOTP UI）；生产 `config.yaml` 的 `totp.encryption_key` 必须保留现值（支付/备份/插件 AES 加密复用）。
- **待办**：`GetUserUsageTrend` 的 `metric` 参数后端已同步，自研仪表盘 UI 未接入（`GatewayDashboard.vue` 仅透传 `allow-metric-toggle`）。

## 前端裁剪范围（同步时「本地已删」即跳过；上游命中的改动由技能标记 MISSING_IN_SO）

- 渠道管理（渠道监控保留）、支付、公告、兑换码、优惠码、订阅管理、模型广场、可用渠道页
- OAuth 登录（GitHub/Google/LinuxDo/WeChat/DingTalk/OIDC）、TOTP/Passkey/Step-up、代理/IP 管理（ProxiesView、ProxySelector）
- 语言切换（固定 zh）、KeysView IP 黑白名单、UsersView 订阅列（`include_subscriptions` 保留）、登录/注册仅邮箱密码（`aff_code` 透传保留）
- `skills/sub2api-admin` CLI 移除 redeem-codes 命令组

## 同步口径与高冲突面

- **口径**：按文件三路判定——二开未改则覆盖；改过则 `git merge-file`；上游新增文件先查是否引用已裁剪模块。
- **仪表盘**：`components/dashboard/GatewayDashboard.vue`（自研 admin/user 双 scope；user 视角绝不触达 admin 接口）。
- **账号 UI**：`CreateAccountModal.vue`（二开 `platformOptions` 紧凑网格与上游清单驱动的 `extraMultiProtocolPlatforms` 并存，新增主平台需手工维护数组）、`EditAccountModal.vue`、`AccountUsageCell.vue`、`CNProviderQuotaCell.vue`、`OpenAIQuotaResetCell.vue`（popover props）、`PlatformOpsQuotaCell.vue`、`UsageProgressBar.vue`（与上游仅差 visibilitychange 一处）、`AccountActionMenu.vue`（三平台重新授权按钮已隐藏）。
- **网关 CC 漏斗**：`openai_gateway_cc_pipeline.go`（sendCCUpstreamRequest / resolveCCFallbackTarget）、`openai_gateway_chat_completions_raw.go`、`openai_gateway_forward.go`（协议分流 switch）、`openai_gateway_{responses,messages}_chat_fallback.go`、`openai_gateway_service.go`、`openai_gateway_usage.go`（RecordUsage 计费与 credits）。
- **账号与限流**：`account.go`、`account_test_service.go`、`ratelimit_service.go`（临时不可调度 scope 与 401 规则）、`temp_unsched.go`、`admin_account.go`、`account_handler.go`、`token_refresh_service.go`、`upstream_models.go`、`antigravity_gateway_service.go`、`gemini_v1beta_handler.go`、`account_usage_service.go`（Antigravity `force` 主动刷新）。
- **计费与模型**：`billing_service.go`、`pricing_service.go`、`gateway_request.go`、`gateway_claude_oauth_body.go`、`gateway_upstream_request.go`、`pkg/claude/constants.go`、`pkg/apicompat/*`、`model_prices_and_context_window.json`、`useModelWhitelist.ts`。
- **推理与回放**：`apicompat/anthropic_to_responses.go`、`openai_compat_model.go`、`openai_gateway_messages{,_chat_fallback}.go`、`openai_gateway_request_body.go`（OpenCode sanitizer + 通用 input 过滤）、`openai_gateway_passthrough.go`——请求 effort 全量透传与 OpenCode reasoning 整条剔除两条二开语义需在同步时保留。
- **用量统计**：`usage_handler.go`、`routes/user.go`、`usage_service.go`、`usage_log_repo_apikey_stats.go`（ByUser 查询）、`usage_handler_apikey_stats_test.go`、`ModelDistributionChart.vue` 折叠与 `UsageApiKeyStatsCard.vue`/`ApiKeyUsageTable.vue`。
- **装配与设置**：`cmd/server/wire.go`/`wire_gen.go`、`handler/handler.go`、`handler/wire.go`、`handler/gateway_handler.go`、`server/routes/admin.go`、settings 管线（setting_parse/update/view、dto/settings）、`platformCatalog.builtin.json`、`router/index.ts`、`AppSidebar.vue`、`AccountsView.vue`、`SettingsView.vue`、`stores/adminSettings.ts`。
- **群组/审核**：`group_repo.go`、`setting_repo.go`、`content_moderation.go`、`GroupBadge.vue`、`AccountGroupsCell.vue`、`Select.vue`、`ChannelStatusV1View.vue`。
- **后端移植触点**：功能 A `GET /admin/dashboard/api-keys-usage-stats`（`usage_log_repo_apikey_stats.go`、`dashboard_handler.go`、`dashboard_query_cache.go`、`dashboard_service.go`、`routes/admin.go` 等）；功能 B `openai-token-stats` 增强（today 自然日 + `total_tokens`/`cache_hit_rate`：`ops_dashboard_handler.go`、`ops_repo_openai_token_stats.go`）；`cmd/server/main.go` 嵌入 IANA tzdata（Windows 裸跑必需）。
- **其它结构性二开**：`components/admin/channel/types.ts` 内联 `ChannelModelPricing` 等类型（上游 channels API 已删）；全站 dense 紧凑化（118 文件，确定性映射可重放，上游改样式时按规则重放）；UI 基准规范 `docs/ACCOUNT_TABLE_UI_SPEC.md`。

## 已知坑点

- **后端测试**：`backend/config.yaml`、`backend/data/config.yaml` 会污染 viper，`go test` 前临时挪走、测后恢复（`internal/config` 的存量失败即此因）。
- **上游拉取**：`git pull --ff-only` 不取 tag，必须 `git fetch --tags`（否则版本推导滞后一档）；上游 v0.2.7 前曾 force push，遇 `--ff-only` 分叉先 cherry 比对，再 `reset --hard` 对齐。
- **pnpm 与 lock**：`frontend/pnpm-workspace.yaml` 必须含 `packages: ['.']`（缺了 pnpm 9 直接报 `packages field missing or empty`，10-21 首败原因之一）；`package.json#pnpm.overrides` 必须回写进 lockfile（10-21 已用 pnpm 9.15.9 重生成 `pnpm-lock.yaml`，`--frozen-lockfile` 才通过）；pnpm 10+ 视 `package.json#pnpm.overrides` 为死配置，只认 `pnpm-workspace.yaml#overrides`。
- **Dockerfile 分叉**：根 `Dockerfile`（pnpm@9）是**唯一有效构建路径**；`deploy/Dockerfile.bundle`（pnpm@11.9.0）与 pnpm 9 格式 lock 不兼容且缺 resources 权限修复，自 10-21 起**已失效**（文件头有 🛑 警告），仅 `build-docker-tar.sh` 离线打包路径待对齐后启用。
- **fnOS 文件权限**：NAS 工作区 fs mode 恒为 000（git index 的 mode 与之无关）——Dockerfile `COPY` 进镜像后不可读/不可执行，须显式 chmod（`Dockerfile.bundle` 入口 `chmod 755`、根 Dockerfile resources `chmod -R a+rX`）；`resolve-version.sh` 的 git 执行位已修（10-21），但 NAS 本地构建仍须传 `--build-arg VERSION=…` 绕过。
- **迁移 checksum**：`migrations_runner.go` 的 `migrationChecksumCompatibilityRules` 用于放行生产库已应用的历史 checksum（10-20 的 `245_so_model_benchmark_comparison.sql` 规则已随测评移除删除）；换二进制前先对全库审计文件侧 checksum，差异必须显式加规则，否则启动即拒。
- **Nexus 私有镜像**：陈旧元数据可能报 `ERR_PNPM_NO_MATCHING_VERSION`，小版本升级可按传递依赖一致原则手改 lock 三处（importers/packages/snapshots）后 `pnpm install --frozen-lockfile`。
- **上游自带 spec 回归**：上游偶发改实现未更新自家测试（v0.2.14/0.2.15 各两例），以内容为准适配，并在条目中注明。
- **前端改动**：须 `pnpm build` 重建 dist 后重启容器才可见（so-dev 为编译期内嵌）。
- `psql` 连接用 `127.0.0.1`（localhost 有 IPv6 问题）；`frontend/pnpm-workspace.yaml` 的 `allowBuilds` 将 esbuild/vue-demi 置 true。

## 构建链与部署流水

- **构建路径**：NAS 与 CI 一律用根 `Dockerfile`（`deploy/build-and-deploy-nas.sh` 默认即它）；`deploy/Dockerfile.bundle` 已失效（见坑点），激活前须先与根 Dockerfile 对齐 pnpm 版本与 lock 策略。
- **so-dev Docker 栈**（`deploy/docker-compose.so-dev.yml`）：backend `go run -tags embed` 热跑（golang:1.27.2）+ PG16 + Redis7；后端 `127.0.0.1:8088`，`AUTO_SETUP=true`；`frontend/.env.development.local`（gitignored）设 `VITE_DEV_PROXY_TARGET=http://127.0.0.1:8088`。
- **本机构建/发布**：工作区根 `./build-so.sh`（前端 pnpm build → Docker go build → 单文件二进制；版本号从 `UPSTREAM-BASE` 推导 tag）/ `release-so.ps1`（Windows x64 zip + checksums）；本机无 Go。
- **离线分发包**：`deploy/build-docker-tar.sh`（docker save tar + sha256 + `dist/docker-bundle/`：镜像 + `docker-compose.bundle.yml` + .env.example + README；tar 产出移至工作区根，`TAR_MOVE_DIR` 可覆盖）；PG/Redis 独立命名卷；已出 v0.2.13/v0.2.15 包并冒烟通过。
- **NAS 一键更新**：`deploy/build-and-deploy-nas.sh`（10-26 新增）——构建镜像（tag `sub2api:<版本>-so-<日期>`，同日重复构建自动追加 `-2/-3…`，旧 tag 永不覆盖）→ `docker save` 归档到 `/vol2/1000/DockerImages/`（terminal-buddy 容器内该目录 ro，脚本自动经临时容器从宿主侧写入 tar+sha256）→ 部署前 `pg_dump` 到 `data/backup_predeploy_*.sql` → 备份并改写 compose 的 `image:` → `docker compose up -d` → 验证 healthy + `/health`（打印回滚指引）。支持 `-y`/`TAR_ONLY=1`/`TAG=`/`VERSION=`/`COMMIT=`/`GZIP=`/`TAR_DIR=`/`SKIP_DB_BACKUP=`。
- **生产部署铁律**（换官方系后端二进制时）：停机 → `pg_dump` → **先** `psql -f deploy/restore-upstream-schema.sql`（抽查 `to_regclass('public.channel_model_pricing')` 非空）→ **后** 换二进制启动观察迁移重放 → 冒烟。回滚 = 换回旧 so 二进制（drop 迁移有记录不重跑），数据回滚用备份。⚠️ 2026-09-24 首启失败教训：顺序颠倒会因官方迁移缺表启动即退。
- **构建/部署事故（10-20/10-21）**：① fnOS mode 000 使镜像内 `chmod +x` 只得 111（`docker-entrypoint.sh`）→ 改 `chmod 755`；② 生产库 245 迁移 checksum 漂移拒启 → 兼容规则放行（已随测评删除）；③ 全新缓存下根 Dockerfile 四连败：workspace 缺 `packages`、lock 未回写 overrides、`resolve-version.sh` 执行位丢失、`/app/resources` 000 权限 → 均已修复（`git update-index --chmod=+x`、`RUN chmod -R a+rX`）。
- **镜像序列（本机 Compose，均保留回滚）**：`0.2.17-pre-effort-20261020`（原始）→ `0.2.17-so-20261021`（qoder 三批）→ `0.2.17-so-20261023`/`-2`（测评移除；部署前 pg_dump 三张测评表 27MB 留档（`data/backup_model_benchmark_20261023.sql`））→ `0.2.17-so-20261026`（OpenCode 修复）→ `0.2.17-so-20261026-2`（用量统计，当前线上）。每次容器重建会触发 fnOS SMB 共享丢失 bug，守护脚本数秒~数分钟内自动恢复（见全局约定）。

## 同步历史（上游基准推进）

| 版本 | 日期 | 区间（BASE → NEW） | 规模（提交/文件） | 要点 |
|---|---|---|---|---|
| v0.2.4 | 09-09/10 | 建立于 `5de5e2be`；VERSION → `98d86915` | — | 基于 v0.2.4 完整复制建立，随即前端裁剪 |
| v0.2.5 | 09-15 | `98d86915 → 881f3202` | 197/447 | OpenCode 平台、站点类型/订阅开关、API Key 批量编辑 |
| v0.2.6 | 09-18 | `881f3202 → 8b69738d` | 55/132 | Codex 292 ticket（v0.2.7 被上游移除） |
| v0.2.7 | 09-19 | `8b69738d → 1a9d49e1` | 19/118 | 上游 force push 重写历史（reset --hard 对齐）；Seedance Ark 视频 |
| v0.2.8 | 09-23 | `1a9d49e1 → a3eb7ef3` | 150+/473 | OpenCode Go 用量窗口、GPT-6 Sol/Luna、Opus 5.5、Grok 4.7（历次最大） |
| 后端基线重置 | 09-24 | 后端整体换回官方 v0.2.8（`99747d0`→`396a91e`→`c567ec5`→`007e6c2`） | — | 后端结构性裁剪使同步成本集中于后端冲突；前端保持二开 |
| v0.2.9 | 09-28 | `a3eb7ef3 → 9a62841f` | 70/117 | 分组白名单 `*`、客户端断开统一 499 |
| v0.2.10 | 10-03 | `9a62841f → a60a2954` | 30/118 | Sonnet 5.5、Claude 重置额度状态、风控白名单 |
| v0.2.11 | 10-03 | `a60a2954 → 42bc7f6c` | 22/90 | Claude 重置额度兑换、API Key 创建限制、计费在途余额预留、GPT-6.1 Sol |
| v0.2.13 | 10-03 | `42bc7f6c → b8dece90`（跨 v0.2.12） | 37/157 | TypeSafe 平台、充值赠送阶梯（仅后端）；跳过 19 个（支付前端/en 包/ChannelsView/.github） |
| v0.2.14 | 10-08 | `b8dece90 → 5fc0e48` | ~90/180 | ops TPS 百分位、setup 安全修复 #7850、vue 3.5.43；支付 8 文件跳过 |
| v0.2.15 | 10-09 | `5fc0e48 → 3a6fd1c9` | 20/140 | Cline/Command Code 平台、provider profile 重构、平台清单驱动；go 1.27.2 |
| v0.2.16（部分） | 10-13 | `3a6fd1c9 → 4d9ac3e4`（收口） | 51 非合并/213 | 移植 17 提交；未纳入项视为永久放弃（见 10-13 条目） |
| v0.2.17 | 10-16 | `4d9ac3e4 → b0a02a33` | 48/153 | 支付链路整体跳过；composite OAuth 歧义路由、GPT-5.5 retirement、Gemini 4 Argon、共享容量 429 退避等；11 个冲突手工裁决（见当日条目） |
| v0.2.19 | 10-29 | `b0a02a33 → a3298b46`（跨 v0.2.18） | 121/354 | 范围授权管理员密钥、Grok 目录诊断、按张计费、Gemini 生图开关/同账号重试可配/2.x 清单下线、流断开计费族、Vertex API Key 补齐（v0.2.17 漏项）、GPT-6.1 Terra、Go 1.27.3；15 冲突手工裁决（见当日条目） |

## 平台移植源同步（PORTER-BASE）

- **当前基准（2026-11-06，三仓库齐推进）**：workbuddy `6e1e548`（v1.10.11）、qoder `70ba58b`（v1.7.30）、trae `dfa5bf7`（2026.11.51421）。**已移植 7 项**（用户全选；qoder 4 + trae 1 + qoder 可选 2）：
  - **qoder session_id 窗口级稳定（`86b83bd` #136）**：修上游前缀缓存每轮归零（面板 98%↔0% 抖动，缓存段系数 8%、同段历史差 12 倍价格）——`conversation_identity.go` 新增 `conversationWindowKey`（显式键优先，否则「模型 + 首条 user 前 400 字」→ `win:sha1`；**不吃 system**——system 常带 cwd/git 状态/时间等每轮变化上下文）与 `upstreamConversation.windowID`；`AssignSessionID` 加 fallback 参数（窗口级兜底，空串保持随机 UUID），`BuildAgentChatBody` 传 `conv.windowID`（拿不到回落 conv.id，源 window_id or id）。QD_SESSION_CACHE 前缀命中仍优先。**行为变化**：默认（缓存关）session_id 由每请求随机改为窗口级稳定（`baseprompt_test.go` 旧断言同步改写）。
  - **qoder 排队/额度错误文案中性化（`5d4e31b` #137 部分）**：状态码 429 已有（classifyQoderUpstreamFailure），补文案——`errors.go` 新增 `NeutralizeAuthCodes`（独立 401/403 → HTTP_401/HTTP_403，RE2 无环视手写扫描）与排队/额度耗尽/每日上限三个专门分支（中文原因 + 英文分类 hint，DSH/pi-ai 按错误文字 `/\b(401|403)\b/`→AUTH 误判「API Key 无效」）；`qoderClientErrorPayload` 的 error.code 统一 classify 收口（hub _client_error_status）。
  - **qoder 伪 XML `<tool_call>` 回读还原（`e6e7622` #140）**：上游把工具调用写成 `<tool_call> Name {json} </tool_call>` 正文文本时（DSH 现场实测）还原成结构化 tool_calls。新增 `tool_call_tags.go`：四形态解析（A JSON 对象/B `Bash {…}`/C 属性名/D `Bash, {…}`，RE2 无前瞻改为词法匹配+后续字符手工判定）、三重防误伤守卫（未声明 tools 不还原/名字须命中声明集/无 marker 时整段就是一个块）、`toolTagCandidate`（流式候选）；`RecoverToolTags` 独立帧迭代器挂在 marker 链之后（hub 管道同序——marker 前缀暂留会把标签开头切进散文先行放出，需下游重新接住；含 ``` 围栏奇偶判定防代码示例误还原、usage 垫底帧、finish_reason 改写）；`stillLeakCandidate` 补 marker 后伪 XML 候选；`finishStream`/`AggregateStream` 流末与非流式兜底。
  - **qoder 工具参数坏 JSON 修复（`70ba58b` 下半）**：`escapeUnescapedQuotes`（字符串内未转义双引号状态机补转义）+ `repairArgsJSON`（严格→转义裸引号→去尾随逗号→叠加四候选）——`ParseLeakedToolCalls` 的 arguments 非法 JSON 先修复再判失败（不再整轮丢弃）；伪 XML 块解析共用（`loadsLenient`）。GuardTruncatedToolArgs（截断守卫）不动——截断的后半段修不回来。**首字超时 504 换号（同提交上半）不移植**：本侧 504→502 default→通用 failover 链已换号，hub 的问题（同账号退避重试 3 次）是其重试循环结构特有。
  - **trae usage 缓存命中量（`a2b7cc4` + `7b1e6d5` #154）**：`pkg/trae/sse.go` TokenUsage 新增 Cached（四形态解析：DeepSeek `prompt_cache_hit_tokens`/OpenAI `prompt_tokens_details.cached_tokens`/Anthropic `cache_read_input_tokens`/裸 `cached_tokens`，脏数据钳到 prompt）；`ToOpenAI` 仅 cached>0 时补 `prompt_tokens_details`（缺键=上游没报或没命中，多一个恒在键会让严格校验客户端拒帧）。**Anthropic 出口（7b1e6d5）无需改动**：CC 链 `apicompat.chatUsageToAnthropicUsage` 已有同款换算（input_tokens = prompt − cached − cache_creation、cache_read 单列），此前只是上游命中量在 pkg/trae 层就丢了——本项打通后链路自然接通。
  - **qoder 会话缓存保温（`22bd512`，QD_CACHE_KEEPALIVE 默认关）**：上游前缀缓存实测 ~5 分钟闲置过期（闲置后首发全量重写 0% 命中全价，124k≈137 credits vs 命中≈13.8）。新增 `pkg/qoder/keepalive.go`（内存 LRU 默认 2 会话、json 往返深拷贝快照、tick 巡检闲置≥INTERVAL(240s 默认)发保温请求、≥MAX_AGE(1800s)淘汰、sender 失败淘汰、20s 后台循环）+ `service/qoder_keepalive.go`（sender 走 applyQoderChatBody + COSY 签名 + sendQoderChat 同链，stream=false + max_tokens=1，clientSessionKey 传登记 session_id 使窗口键原样回填；observe 挂 cc_pipeline qoder wrap 成功后，懒启动）。上游对同会话高频重发的风控容忍未知，从小参数起步。
  - **qoder 额度构成四桶 + 端点探测（`a76dfa0`+`c25db50` 等 #141 系列）**：`accounts.go` QuotaUsage 新增 Composition/Source——新增 `quota_composition.go`：BFS 归一化（字段名匹配五组桶表：plan/sota/pack/shared，**SOTA 含 dedicatedResourcePackages 族**——INTL 实测 SOTA 在此容器、旧聚合完全漏识别合计少算 2000+；displayLabels[].valueI18n 取展示名；available=false 只展示不计入；shared 单列不重复计数）覆盖合计；端点自动探测（404/405/410 换候选路径矩阵 11 项、命中进程内记忆）。runBalance 快照 extra["qd_quota"] 自动带 composition。
  - **改动文件**：`pkg/qoder/{conversation_identity,session_affinity,baseprompt,errors,sse,accounts}.go` + 新增 `{tool_call_tags,keepalive,quota_composition}.go`；`service/{openai_gateway_qoder,openai_gateway_cc_pipeline}.go` + 新增 `qoder_keepalive.go`；`pkg/trae/sse.go`。测试：更新 `{baseprompt,errors,session_affinity,porter_sync}_test.go`/`pkg/trae/sse_test.go`，新增 `{tool_call_tags,keepalive,quota_composition}_test.go`。**合并影响**：全部为二开文件（无上游同步冲突面）；不触碰 `UPSTREAM-BASE`。
  - **评估未移植（原因，勿重复评估）**：workbuddy v1.10.11 内 #796 sanitize 改写（`Main branch`/`To give feedback` 两条**已与 hub 最终形态一致**，10-29 同步时已按新形态移植）；#799 413 先回复再排空（Python BaseHTTPRequestHandler socket 层，Gin/net/http 框架自管理）；#774 live 免费倍率不覆盖付费价（本侧 ModelInfo 无 credits 概念、兜底表无可覆盖对象）；#718 小池长会话粘滞（hub page 多页轮换机制本侧不存在）；#707 账号分组面板/#794 CodeArts（已裁定）/#798/#704 更新器；`95a7a03` 国际版每日活跃总开关（前置 wb_webagent ACP 功能属 10-29 待续批次未移植）；`d70bb0f` 风控连坐暂停自定义时长（hub 单进程池整池策略，本侧单号 wb_restricted 已有、多渠道 DB 调度无对应概念）；`57a3990` 11-128 取证开关（诊断类，同 7fe04ad 裁定）；`7fd245f` checkpoint 锁忙跳过（JSONL 并发，Go 落 DB 免疫）；`14bd75d` 登录出口代理槽（hub 代理槽体系专属）；`47c6728`/`43be883`/`cfb542c` 部署/面板；qoder `70ba58b` 上半（见上）；`2d5a299` docker；trae `12ee13f` 账本命中率（桌面面板）、`0d16680` 网关密钥管理（桌面端专属，同 issue #76 系列裁定）、`3703501` 面板 UI、版本号/文档/测试类。
- **上一基准（2026-11-04，三仓库齐推进）**：workbuddy `ff47b07`（v1.10.9）、qoder `2611b71`（v1.7.22）、trae `5cf62da`（2026.11.41007）。workbuddy 侧为 v1.9.0→v1.10.9 约 110 提交的累积评估（上次基准后 hub 持续更新未再评估，本次一并纳入）。**已移植 6 项**（用户选定；CodeArts 第三 realm 与媒体系列经用户裁定不移植）：
  - **sanitize 指纹族整句中性化（workbuddy `ecb95a6` #743 + `4df31cf` #733）**：Codex CLI 与 Claude Code 自述句由「CLI→CLI tool 拼写改写」升级为**整句替换中性句**——hub 实测拼写改写后仍保留平台身份串、照样命中 11-128/400；`sanitize.go` 删除两条旧改写、新增 `sanitizeClaudeCodeIntroRe`（含 `['’]` 直/弯引号双形态与 `(?: tool)?`/`(?: Code)?` 变体）与 `sanitizeCodexIntroRe`（`[^.\n]*\.` 覆盖句中内容），hasFingerprint 补小写混排锚点 `you are claude code,`（预检表只登记首字母大写形态）；正则字面量经脚本从 hub 源文件字节级提取并断言一致。
  - **normalizeTools 工具名校验（workbuddy `b04868e`/#726，400/11133）**：`tool_schema.go` 归一化尾部新增 `dropUnusableTools`——`function.name` 必须 `^[A-Za-z0-9_-]{1,64}$`、无 function 对象的条目（纯服务端声明如 `{"type":"web_search"}` 被强改 type 后空手送上游即 11133）整条丢弃（只丢不改名，防 tool_calls 名失同步）、同名留第一条、全丢删 tools 键；本侧旧实现确认存在同款缺陷。
  - **成长任务扩充 + Buddy 盲盒（workbuddy `937c179`）**：`pkg/workbuddy/tasks.go` 新增小程序链式任务族闭环 `RunMPSequentialTasks`（mp 口径列表→接取→判据事件上报→有界回读→领奖；事件按任务码四类分派：mp 对话/专家对话 WorkspaceBuilder/云端定时任务/GLM5.2 模型对话，未知 `Sequential_Tasks_` 前缀按对话类宽容处理；上报走 `/v2/report` + mp 产品头族 `X-Client-Product: workbuddy-mp`/`X-Client-Platform: mp-weixin`/`X-Platform: wechatmp` + `mpEventBase` 底座字段伪装微信小程序环境；仅 CN realm）与盲盒 `RunBuddyOpen`（`/v2/activity/growth/buddy/quota` 查 affordable/max_open_count → 钳制开盒数 + 幂等 client_token 开 `/open`，**注意盲盒端点带 /v2 前缀与 tasks 族同形、与 travel 族裸路径不同**）；`workbuddy_ops_service.go` runGrowth 追加两者（盲盒 affordable<=0 为正常空闲静默跳过）。
  - **qoder 测试连接免费模型探活（`e1cbb42` #134）**：`DefaultQoderTestModel` 由 `Auto` 改为 `Qwen3.8-Flash`（=qfmodel，cn/intl 目录均 price_factor 0.0）——Auto 非免费，零额度账号测试必然 403 code 112、误报凭证坏；`testQoderAccountConnection` 403 分支补额度类拒绝防御（`qoder.IsQuotaReject` 命中 112/110 时只报结论、不 `SetError` 误标认证失败）。
  - **qoder 工具残片 lenient 兜底（`2611b71` #135）**：`ChatStreamOptions` 新增 `ToolEchoLenient`，`GuardTruncatedToolArgs` lenient 时流末参数非法 JSON 不再 fail-closed 而按正常完成收尾（保留已产出正文、正常发 [DONE]），避免 Codex 类客户端收到失败后静默结束对话；开关为环境变量 `QODER_TOOL_ECHO_MODE=lenient`（默认 strict，对应 hub 的 QD_TOOL_ECHO_MODE；`qoder.ToolEchoLenient()` 进程级缓存）；非流式 `AggregateStream` 本就无此守卫天然 lenient。
  - **trae 临期窗口 14 天（`4a590cb` #144）**：`pkg/trae/usage.go` `expiringSoonWindow` 由 7 天改 14 天——trae-switch 复核确认 Trae 后端 `EXPIRING_SOON_SECS` 实为 14 天，旧 7 天口径漏标 10~14 天内到期的包；测试补 10 天（应命中）/20 天（不应命中）断言钉住。
  - **改动文件**：`pkg/workbuddy/{sanitize,tool_schema,tasks}.go`、`pkg/qoder/sse.go`、`pkg/trae/usage.go`、`service/{domain_constants,openai_gateway_qoder,workbuddy_ops_service}.go`；测试更新 `{sanitize,payload_norm,client,tasks}_test.go`、`pkg/qoder/porter_sync_test.go`、`pkg/trae/usage_test.go`。**合并影响**：全部为二开文件（无上游同步冲突面）；不触碰 `UPSTREAM-BASE`。
  - **用户明确跳过（不再建议）**：CodeArts 第三 realm 全量支持（hub `aaa8976` 等 ~20 提交、`wb_codearts.py` 2952 行——本侧无任何实现，属全新平台接入，用户裁定不需要）；workbuddy 媒体系列（生图 + 异步生视频，v1.10.1~v1.10.6 ~25 提交——workbuddy 网关本侧仅 chat，无媒体出站需求）。
  - **评估未移植（原因，勿重复评估）**：429 绝对恢复时刻（`0ed5386`，hub 自有网关对下游的响应头增强，本侧 ratelimit/错误信封体系自有）；deepseek_flash_force_sg 及其废弃（`9f43846`→`42dbc90`/`91d509d`，hub 加了又删，本侧目录实时拉 /v3/config 天然跟上，静态兜底表只含 efforts 无 credits）；responses namespace 父路径 + tool_choice 回写（`ef479f7`——本侧 `flattenNamespaceTools` 不改名、nsMap 未消费，tool_choice 裸名天然对上，架构免疫）；账本原子追加/日限 offset 校验（`53c8697`/`b700692`，Python JSONL 并发缺陷，Go 全量落 DB 免疫）；错误正文缓存/SSE 终态停读（`97df705`/`1bf70e5`，hub 默认关闭的 Python http 流开关）；#743 的 proxy_slot/proxy_exit 用量行归因（hub JSONL 日志体系专属）；qoder 积分三段拆分/cached_reported/full_price 统计（`090cfae`/`2611b71` 部分，面板名义成本展示，本侧计费走自身价目表）；qoder #131/#133 行为级回归套件（测试体系自有）；qoder #132 更新器 can_apply 门控（hub 自更新器专属）；trae 网关接入地址补 /v1（`7699dd8`，trae-switch 桌面端网关页 UI）；hub 面板 UI 系列（控制台外壳/导航/登录/配色 ~20 提交）、launcher 端口（`0ade011`）、删除账号清任务快照（`d233b17`，DB 级联免疫）、出站身份诊断脚本（`7fe04ad`）、README/release/CI 系列；desktop 专家任务族扩充（expert_use_* 新 specs 与 jump_url 动态专家提取，`937c179` 部分——用户选定范围为 mp 任务族与盲盒，desktop 专家链未点名，待有需求再评估）。
- **上一基准（2026-10-29）**：workbuddy 移植源切换至 `workbuddy2api-hub`（Python，ardeyouxipianyi/workbuddy2api-hub，v1.9.0 @ `a6a967ef`，持续维护）；workbuddy2api-panel 退役为历史（末版 `73a7560e`，2026-10-21 起停止维护）。本次为**全面对照移植**（差距分析与逐项决策见 `docs/workbuddy-hub-sync-2026-10-29.md`）：
  - **出站协议保活（pkg/workbuddy）**：sanitize 升级——通用身份句正则（7 动词表；作用域收口 system/developer/assistant，user/tool 用户数据不改写，hub #533/#537/#550/#574）、DSH GUI 描述段句尾语序改写（#579）、DSH 文件引用段补词（#683）、OmO Sisyphus-Junior 指纹（#62）、tool arguments 非字符串→JSON 字符串 / null→`{}`（#446，11101）；`tool_choice="none"` **保留工具声明**（删声明会把模型调用降级成 DSML 文本、Agent 原地空转，hub v1.5.7）；`normalize_stop`（单字符串→单元素数组、空值删字段，11101）；tools Schema 规整（functions→tools、顶层 name 工具包装、parameters 递归修复 type/properties/required/类型别名，11129）；工具配对修复扩为三段链 merge 拆条合并 → repack 结果归批（含 #420 插队消息后移）→ 孤儿清理；`max_tokens` 缺省补目录 maxOutputTokens（#121，防 Codex 缺省 32k 截断）。
  - **出站身份头**：`X-Product` 修正为官方值 `SaaS`（旧 `WorkBuddy` 为自创组合）；版本常量按 realm 更新至2026-10-18 抓包核对值（CN 桌面 5.8.3 / global 桌面 5.8.4 / 内置 CLI 2.161.0）；global UA 平台段修正为官方 applicationName `workbuddy-ai`；新增 `X-Private-Data`（官方桌面默认 true，`WB_PRIVATE_DATA` 可覆盖，hub #516）；新增桌面 chat 兼容档 `WB_DESKTOP_CHAT_COMPAT=1`（hub #643：仅 chat 去浏览器系头、补 OpenAI Node SDK x-stainless-* 指纹族与 B3 parent/b3/traceparent；计费 REST、刷新、请求体、设备 token 不变，非 TLS 指纹模拟）；reasoning_effort 档位映射按账号模型目录快照（SupportedReasoningLevels/DefaultReasoningLevel）改写非法档位（#542，canDisableThinking 回落依赖上游字段暂缺、保守透传）。
  - **错误语义**：模型级 5xx 不熔断整池——5xx body 带业务码 11102/11129/11134/11150/11158 归模型级（不罚号不记 fails；11158 照旧写 (账号,模型) 锁；普通 500 走通用 failover，hub #499）；401 真失效判定收紧——仅 JSON 业务体或带网关 `WWW-Authenticate` 触发强制刷新，中间层 HTML 401 不罚号透传（hub #440/#691）；成功响应清零 session dead 容忍计数（hub #630 note_success 同口径）；5xx failover `>=500` 全覆盖核实为自有等价（#288）。
  - **运营核实与裁定**：签到失败语义已有等价（`IsAlreadyCheckin` 只认「已签到/already」，10001「请求处理中」失败回执如实记失败，hub #596 之坑不存在）；账号 tier 读计费摘要（#505）、keepalive 48h 提前量（#600）、成长任务手动领取（#507）评估为自有等价/无对应需求不移植；**国际版每日活跃网页会话通道（hub wb_webagent ACP 驱动 + 预算候选序列 + #649/#645/#623 配套）为独立大功能，列入待续批次未移植**。
  - **改动文件**：`pkg/workbuddy/{sanitize,client,tool_pairing,headers,errors}.go` + 新增 `tool_schema.go`；`service/openai_gateway_{workbuddy,cc_pipeline,chat_completions_raw}.go`；测试新增 `payload_norm_test.go`、`headers_compat_test.go`、`service/workbuddy_error_semantics_test.go`，更新 `{sanitize,client,headers,openai_gateway_workbuddy}_test.go`。**合并冲突面**：`openai_gateway_cc_pipeline.go`（workbuddy 分支两处小改）、`client.go` PrepareChatBody 管道与 `normalizeToolChoice`；其余纯二开新增文件。验证：pkg/workbuddy 全量 unit 全绿；service 仅剩6 个已知存量失败、与 HEAD 基线一致。
  - **注意**：sanitize 指纹串/正则字面量经脚本从移植源文件**字节级提取**生成（工作区工具输出对特殊字符存在渲染失真，人工转写会静默改坏指纹）；sanitize 品牌词缩减与积分到期取最早维持 2026-10-22 用户跳过裁定（不再建议）。
- **上一基准（2026-10-26，panel 末版）**：workbuddy `73a7560e`（上游 2026-10-21 声明停止维护——官方收紧非官方客户端识别，后续协议跟进需自行评估；仓库已移至 `/vol2/1000/code/workbuddy2api-panel/original/`）、qoder `0426c334`（v1.7.6）、trae `1707fae1`。
- **历史批次（均已并入后续基准，逐项见 git 历史）**：10-16 首批 qoder #33 图像单 part dict、trae #28 变体别名；10-21 三批 qoder 风控身份（UUID 形状/随机请求 ID/machineos 头）、出站体（丢 `namespace`/`web_search`、去重复 Base64、`QD_MAX_BODY_MB` 29MB）、共享组织配额；10-23 批次见下。
- **10-23 已移植（qoder，`49f5c94`）**：出站头族对齐 qodercli 1.1.75 / 桌面端 0.4.8（COSYVersion、UA=undici、business 三头、data-policy 小写、machineos 恒发、openapi UA 按区域、intl 网关 api3 主选）；结构化白名单补 deepseek/dfmodel 与 keep_reasoning；intl 签到机器头兜底降档（`QD_INTL_MACHINE_FALLBACK`）；runtime-info 环境码 cn=0/intl=3 + 落盘校验；回声 marker 变体正则/重复 marker/DSML 闭标签/散文同帧；出站孤儿工具清洗 + 结构化 400/422 降级记忆（6h，错误路径写入不重发）；多模态 parts 出站（图片真正传上游）；模型目录 is_free/峰谷价元数据（字段随 sync preview JSON 下发，前端展示列未加）。**已移植（trae，`e890c97`）**：JWT 续期协议修复（1.107.1 体形态、account_base、六步变体链、10101/040004 分类、PlatformCode 按线派生、无私钥不发 proof）；Max Mode（`pkg/trae/catalog.go` 重放 batch_get_detail_param，24h 进程内缓存单飞、max>dev 注入 is_max_mode、`ModelEntry` 增 `MaxMode`、**不落盘**）。
- **10-26 已移植（workbuddy）**：错误分类分流（11-128 通道拒绝不罚号换号 / 11140 账号限制置 `wb_restricted` / 11158 模型权限归 (账号,模型) 24h 锁 / 6021 queue.full 渠道繁忙合成 503）；refresh 单飞（UID singleflight + 12153 重读 DB 自救）；出站体双防御（reasoning 片段剔除 + 会话头三元组恒发——**推翻 10-22 跳过裁定**，国际端点缺 X-Conversation-ID 已直接 400）；任务补领清扫（`RunTaskSweep` 4h，治积分卡住）；补偿先查后领（6h，`IncludeDisabled`）；联网搜索（`pkg/workbuddy/search.go` + `POST /v1/search/messages` Anthropic 兼容壳，走账号池选号）；企业切换会话门控（`IsEnterpriseSession`，行为类任务跳过、签到不动）；`WB_CN_CHAT_BASE` 可配；/v1/messages 图片默认丢弃留痕（`SO_ANTHROPIC_FORWARD_IMAGES=1` 打开；Qoder 侧保留 detail 档位）。
- **10-26 已移植（qoder/trae）**：max_tokens 出站钳制 + 上限学习（从 400 文案学 (区域,模型) 上限，7 天进程内缓存，模板默认 32768 也参与钳制）；intl 活动平台第二入口（`OpenAPICandidates` 候选链 env 主基址→center.qoder.sh→openapi.{domain}→website，仅 intl 加 center；仅连接失败/404/405/410 换基址 + 404 免探缓存 6h）；官方积分消耗（`FetchCreditUsage` 走 credits-summary/heatmap，账号级 5min TTL，接进 `ProbeQoderQuota` 快照 total/peak/近 30 天 daily）；前缀会话亲和（`session_affinity.go`，opt-in `QD_SESSION_CACHE=off/memory/redis`，hash 走 `QD_SESSION_HASH_SECRET`）；trae 仓 `76a97c0` 图片丢弃留痕（WB/Trae 共用）。
- **用户明确跳过（不再建议）**：workbuddy sanitize DeepSeek Harness 改写 + 积分到期取最早（`66a33ab`/`67c544`，2026-10-22 裁定）。
- **评估未移植（原因，勿重复评估）**：qoder #77/#83 换区 failover（按账号路由无请求级 realm）、#81 /agents/models、#86 only_account、v1.6.3/v1.6.4 用量洞察/面板 UI、Pro 升级包（已有 runProWelfare 等价）、#100 Python 异常族（Go 免疫）；workbuddy models.hidden（有 model_mapping 等价）、裸模型名跨域回退、软冷却 reason/6004 双时钟/缓存告警（自有展示体系）、按地区轮询/DNS/ProxyFromEnvironment/手机端/账号备注/积分历史（自有调度与 Docker 形态）、企业判定面板徽标（已有 Shared* 与自有 UI）；trae 账号池系列（RAII/excluded/Retry-After）、限额护栏/pacing/在途闸门/历史削减（自有 402 停调度）、issue #76 密钥权限系列（自有 Key/订阅体系）、DSML #66/排队/首帧/CRLF（同代或更新实现）、/v1/responses（已有桥）、模型限流探测联动、无客户端部署（so 即服务器 OAuth 流）、runtime_log/macOS 钥匙串/免责确认/「计入网关」语义表（桌面端专属）；排队可见/快速失败（池化冷却换号语义更优）。

## 变更时间线

### 2026-09

- **09-09~09-11 前端 UI/UX 重构**：确立 `docs/ACCOUNT_TABLE_UI_SPEC.md` 基线（账号表格 dense、行高 48–54px、表单 label 左/控件右）；仪表盘 Bento 栅格与运维 SLA 色标；全站间距降档 dense 化（118 文件，确定性映射可重放）。合并：UI 层大面积二开，同步多为三路合并。
- **09-14 仪表盘与用量统计**：用户/管理员仪表盘统一为 `GatewayDashboard`（scope prop，后端零改动）；使用记录拆出「用量统计」页（`/usage-stats`，无费用口径）。合并：`GatewayDashboard.vue` 高冲突。
- **09-17~09-22 仪表盘/运维扩展**：全局默认时间范围「今天」；「Token 请求统计 · 今日」表；「API 密钥用量统计」板块；管理台与运维监控移除多区块（接口保留；`OpsDashboard.vue` 上游可整文件覆盖）。合并：`GatewayDashboard.vue` 高冲突。
- **09-26→10-13 账号用量窗口**：多轮迭代（微胶囊 → 双行严整 → 去底色 → 恢复），**最终回归上游 v0.2.15 原版**（每窗口纵排 `UsageProgressBar`，`AccountUsagePopover` 与 238px 规格文档已删）。合并：`UsageProgressBar.vue` 与上游仅差 visibilitychange 一处；`AccountUsageCell.vue`、`CNProviderQuotaCell.vue` 与上游同构，冲突面小。

### 2026-10 上旬与中旬

- **10-03 管理控制台增强**：资源健康大盘（账号池/用户生态/密钥态势/在线新鲜度）、流量走势（Token/请求数双模式）、分组负载分布（`GroupUsageStats`，`include_group_stats`）。
- **10-06/10-11 离线打包**：`build-docker-tar.sh`、`Dockerfile.bundle`、`docker-compose.bundle.yml`（见「构建链与部署流水」；bundle 已于 10-21 失效）。
- **10-12 管理控制台布局**：admin 仪表盘「模型分布 / 分组使用分布」1:1 → 3:1。合并：`GatewayDashboard.vue`。
- **10-12 三平台源码集成（workbuddy / qoder / trae）**：四批（登记与运营基建 → 网关/OAuth/运营/端点 → 回归收尾 → 前端接入）+ 修复系列。`pkg/{trae,workbuddy,qoder}`、`{workbuddy,qoder,trae}_ops_service.go`、`*_oauth_service.go`、`forms/{WorkBuddy,Qoder,Trae}Form.vue`、`DeviceAuthFlow.vue`、`api/admin/platformOps.ts` 为纯新增；触点文件全部收录于「同步口径与高冲突面」节，需手工合并。
  - **登记与菜单**：`domain/constants.go`、`platforms.go`、`provider_profile.go`（三平台 by_inbound + 仅 chat_completions 端点；账号 `api_protocol=adaptive` 收敛到 CC 路径，Claude Code 经既有 CC×anthropic 链可用）；侧边栏「账号管理」分组化（`/admin/accounts/p/:platform`，设置键 `account_menu_hidden_platforms`）。
  - **网关**：平台分叉收敛在 `openai_gateway_cc_pipeline.go` 的 `sendCCUpstreamRequest`（体改写/头注入/发送/响应包装，三条 raw CC 路径共用）与 `resolveCCFallbackTarget`、`rawChatCompletionsURL`；平台文件 `openai_gateway_{workbuddy,qoder,trae}.go`；错误分派 `openai_gateway_source_integrated_errors.go`（先平台分类，后通用 failover）。
  - **OAuth/凭据**：`{workbuddy,qoder,trae}_oauth_service.go`（设备/PKCE 会话 TTL 15min、刷新链、`*TokenRefresher` 到期前 30min）+ `handler/admin/*_oauth_handler.go`；trae 虚拟设备私钥存 `credentials.device_private_key`；凭据归一化接入 `admin_account.go`。
  - **运营任务**：`service/platform_ops.go`（任务描述/每日定点/`ops_state`/注册表）+ 三平台 ops service；平台级调度设置存 `settings.platform_ops_config`；端点 `/admin/platform-ops/platforms/:platform/config`（GET/PUT）、`/admin/platform-ops/accounts/:id/tasks/:task`；文档 `docs/platform-ops-tasks.md`。
  - **额度与降级**：快照 `extra["wb_quota"]`/`extra["qd_quota"]`/`extra["trae_quota"]`；workbuddy 402/14018、qoder 402 → `error(*_no_credit)` 停调度，签到/余额成功后自动恢复。
  - **测试与模型目录**：测试连接走真实转发路径；默认测试模型登记在 provider profile（`DefaultQoderTestModel=Auto`）；qoder 对外模型清单用官方显示名（`CatalogEntry.PublicID()`，账号 model_mapping 大小写敏感）；`SyncUpstreamModelsPreview` 支持 credentials 直传预览上游模型。
  - **错误处置**：qoder 10605 排队按 429 冷却不罚号（建流前/流内首帧/测试连接三路径）、单号池 429/排队不冻结唯一账号（`qoderSingleAccountPool`）；workbuddy WAF 403 软冷却不罚号、上游指纹 11-128 → `pkg/workbuddy/sanitize.go` 清洗（Claude Code 身份句、反探测数字）；trae 上游必填 `app_version_code`；两条 CC 回退路径接入平台错误分类。
- **10-13 账号菜单两项**：① 添加账号按平台菜单预选平台（`CreateAccountModal.vue` 新增 `defaultPlatform` prop，`AccountsView.vue` 透传路由平台）；② 设置键 `account_menu_default_platform`（默认空）——侧边栏父项直达该平台列表，被黑名单隐藏的平台仍保留入口。合并：两文件上游同名需手工合并。
- **10-13 qoder/workbuddy 今日积分**：迁移 `242_so_account_daily_credits.sql`（`account_daily_credits`，主键 `(account_id, day)`，级联删除）；usage 捕获 `credits`/`credit`，`RecordUsage` 落库前按日累计；列表「今日 N」胶囊（`WindowStats.credits`，批量查询无 N+1）。合并：`openai_gateway_usage.go`、`openai_gateway_response_handling.go`（首次触碰，各一处小改动）；迁移、仓库与测试为新增。
- **10-13 Antigravity 直接模型模式 + 测试/编辑修复**：空映射 = 直通（不再自动注入 `DefaultAntigravityModelMapping`；预设按钮与 `/admin/antigravity/model-mapping` 保留，存量映射账号不受影响）；测试请求去掉 `maxOutputTokens:1`（修 thinking 模型「假成功 + 空白」）；直接模式测试走上游 live 目录；编辑弹窗修复「保存后模型列表被清空」。合并：`account.go`、`antigravity_gateway_service.go`、`account_test_service.go`、`account_handler.go`、两个 Modal 需手工合并。
- **10-13 qoder 工具历史结构化直传 + 畸形回声兜底**：`StructuredToolHistoryEnabled`（env `QD_STRUCTURED_TOOL_HISTORY`=off/on/auto，auto 限 cn + 模型前缀白名单）；`leakedMalformedEcho` 吞畸形回声（流末/流中/非流式三处）。CN 真机实测通过；INTL 因上游排队未实测（补验后再考虑放开 auto）。合并：纯二开文件。
- **10-13 Claude Haiku 5.5 全链路接入（issue #7919）**：模型识别（`pkg/claude/constants.go`、`IsHaiku55`）；价卡（`model_prices_and_context_window.json` 含 >100k 绝对价，三源阶梯一致）；协议转换（`pkg/apicompat`：强制 tool_choice、采样锁定、effort=none 映射）；网关校验（`gateway_request.go`：adaptive thinking 必选、disabled 仅 high 及以下）；前端白名单与预设。合并：上游同名文件手工合并；`gateway_request_haiku55_test.go` 新增。
- **10-13 GPT-6.1 Sol 订阅长上下文附加费豁免**：OAuth 类账号请求 GPT-6.1 Sol 不收模型价卡长上下文阶梯（API Key 账号仍按 API 价卡）；豁免按**实际发送模型**判定（`openAISubscriptionLongContextExempt`）；`CostInput.DisableModelLongContextPricing` 仅关阶梯、不改自定义区间价。合并：`billing_service.go`、`openai_gateway_usage.go` 手工合并。
- **10-13 Responses input 校验失败降级 + codex 上下文 500k**：auto 模式上游 400 拒绝结构化 Responses input（`isConvertedResponsesInputStringRejection`）时自动降级 raw chat completions 重试；GPT-6.1 Sol 生成上下文默认 500k（上游显式元数据优先）。合并：`openai_gateway_chat_completions.go`、`openai_codex_models_service.go` 手工合并。
- **10-13 内容审核分组删除竞态修复**：删除分组时清理审核配置 `group_ids`（jsonb 原子改写，保留顺序与其它字段）；写入侧 `SetContentModerationConfig` 事务内锁存活分组；锁序统一 groups → settings。合并：`group_repo.go`、`setting_repo.go`、`content_moderation.go` 手工合并。
- **10-13 前端交互修复 4 项**：分组名完整换行（`GroupBadge.vue` `truncate` prop）；后台标签页倒计时停摆（`UsageProgressBar.vue` 监听 `visibilitychange`）；输入法组合态方向键被抢（`Select.vue` `isComposing` 守卫）；渠道状态手动/自动刷新统一（`ChannelStatusV1View.vue`）。合并：上游同名文件手工合并。
- **10-13 同步收口 v0.2.16（部分移植）**：上游 `3a6fd1c9` → `4d9ac3e4`（51 非合并提交/213 文件），移植 17 个（两批，明细见 git 历史）；人工解决 `openai_gateway_forward.go`（保留二开协议分流 switch，仅补 `startTime`）与 `openai_gateway_workbuddy_test.go`（补上游新签名 `time.Now()`）；第二批 12 文件三路合并无冲突。
  - **永久放弃（勿再当作待同步项）**：支付 1、净 diff 为零的依赖补丁 2、Antigravity 1、分组多语言 2、Gemini 显式缓存/Vertex/AQ Key 17、中等价值项 10、版本提交 1（逐提交哈希见压缩前 git 版本）。
  - **基准前移（一次性例外）**：`UPSTREAM-BASE` 前移至 `4d9ac3e4`，VERSION 置 0.2.16——二者必须同步，否则 `build-so.sh` 推导版本与 VERSION 矛盾。被放弃提交的改动已计入基准、二开侧不含它们，后续三路合并保持二开现状；仅与之相邻的后续上游改动可能冲突，需人工判断。
- **10-14 修复 Antigravity 已删模型回流 /v1/models**：`resolveModelMapping` 对**非空** `model_mapping` 的账号运行时回填 18 条 flash 自映射 + 3 条 pro 别名，被删模型回流 `/v1/models`、Codex 清单、`/v1beta/models`、composite 与 admin 分组候选。移除回填（`ensureAntigravityDefaultPassthroughs` 系成为孤儿已删）；`applyAntigravityGemini31ProAliases` 只改写已配置键、不再新增；变体解析并入 `mapAntigravityModel`（转发、调度准入、限流键三处同口径），只配变体未配裸名的账号仍接受裸名请求。横向排查：Grok/GoogleOne 注入只在空映射分支，其余 10 平台配置 1 条返回 1 条，本 bug 为 Antigravity 独有。合并：`account.go`、`antigravity_gateway_service.go` 手工合并。
- **10-14 workbuddy 移植 tool_pairing 配对清理（修 11133/11148 会话顶死）**：来源 workbuddy2api-panel `535e19e`（PR #148）。新增 `pkg/workbuddy/tool_pairing.go` `CleanupOrphanToolCalls`（孤儿 tool_call↔tool 两侧对称裁剪 + 空 `function.name` 脏条目否决；无工具流量零改动），挂接 `PrepareChatBody` 覆盖网关转发/测试连接/运营任务全部出站路径。根因：流式异常早停产出 `name:""` 条目被客户端写进历史，下轮转发上游判 400 11133（整条会话每轮 503 报废）；只删调用侧会留孤儿 tool 结果 → 11148，故整函数移植。未移植 `mergeAdjacentToolCalls`/`repackToolResultBlocks`（上游 deepseek-only 场景），已知缺口：拆条/插队报文形态仍可能 11148。合并：纯二开新增文件 + `client.go` 局部插入。
- **10-14 Qoder 签到闭环改造（修「提示成功但积分不到账」）**：根因 `runCheckin` 只打旧 sash `/daily-check-in`（CN DISABLED、INTL 404），跳过分支返回 OK → 前端弹成功但零入账；官方每日领取已迁到活动平台。参照 qoder2api-hub 重建：`CampaignCheckin` 编排（列活动 → 分类 → 逐个领 CLAIMABLE CREDITS；`nextCheckinWindow` 按 UTC+8 每日 10:00 名额刷新；earned 只计真实新增防虚增）；`runCheckin` 活动平台优先，仅列表查询失败才走旧 sash 接口；BLOCKED 记 6h 冷却持久化 `extra["qd_campaign_blocked"]`（多号同机防风控）；checkin Hours 默认 `9,21→10,21`；`runCampaigns` 排除 CREDITS 类。连带基建：`OpsStateEntry` 加 `Note`、`RunOne` 签名 `error → (string, error)`（三平台 15 个 RunOne 适配）、RunTask 返回 `{done,note}`、前端 toast `runSuccessNote`。已知限制：跳过态仍记 LastOK 属权威语义。合并：纯二开模块，无冲突面。
- **10-14 workbuddy/qoder 签到状态胶囊**：用量列（`PlatformOpsQuotaCell.vue`）每日签到三态——今日成功绿「已签到」（title 含 note+时间）、跑过未成功红「签到失败」（title 含 last_err）、其余灰；数据读 `extra.ops_state.checkin`（零后端改动）；「今天」按账号时区（`extra.ops_timezone` ?? Asia/Shanghai）`Intl` 判定，与后端 `sameLocalDay` 同口径；trae 不展示。合并：本就在高冲突面清单内。
- **10-14 Qoder intl 双缺陷修复**：**A 模型路由三处漂移**——① 出站头从不发 `x-model-key`/`x-model-source`（修复：签名头注入时从原始 OpenAI 体解析模型、带 realm 归一为上游 key）；② `ResolveUpstreamKey` 丢 realm（跨区显示名歧义）；③ 目录元数据固定 cn 优先（双区同 key 元数据不同，intl 请求体按 cn 快照填错）。②③ 修复在 `pkg/qoder/baseprompt.go`。**B intl 签到缺真实 UMID 机器身份**（派生六头被活动平台整条过滤）——新增 `pkg/qoder/umid.go`（runtime-info 组件发现 → 调用取真身份，进程内 30min + 落盘双层缓存；`DesktopHeaders` 原生身份优先、导入凭证 machine_token 次之、无真身份不发六头；`Campaigns` 自愈清种子换身份重试一次）；`deploy/Dockerfile` 运行时依赖加 `gcompat libstdc++ libgcc`（组件为 glibc ELF，musl 需兼容层）；文档 `docs/qoder-umid.md`。已知限制（服务端策略）：机器身份机器级，intl 按设备去重，每设备每日仅 1 个 intl 账号可领。合并：全部位于二开文件。
- **10-15 模型清单与账号供给透视面板（Model Manifest）**：「模型 ⟷ 账号」倒排查询（方案 `docs/MODEL_MANIFEST_PLAN.md`，UI 对齐 `mockups/model-manifest-preview.html`）——某模型谁在供给/账号状态、某账号提供哪些模型、哪些模型全员断供。后端 `service/model_manifest_service.go`（一次全量加载内存倒排聚合，0 上游网络请求；模型解析优先级 `model_mapping` keys（跳过通配符）→ `extra.upstream_model_metadata` 快照 → 平台默认清单兜底；断供 = 可调度账号数为 0）+ `handler/admin/model_manifest_handler.go` + `GET /api/v1/admin/models/manifest`；前端指标卡、按模型/按账号双视角、客户端过滤 + 路由 + 侧边栏入口 + i18n。装配：`wire_gen.go` 手工接线（该文件不可 wire 重新生成——ProviderSet 缺 workbuddy provider 属既有状态）。合并：新增文件为主，触点小段插入，低冲突。（10-23 起入口为模型能力中心单页，`ModelManifestView.vue` 薄壳已删。）
- **10-15 Docker 打包脚本 tar 移至工作区根**：`deploy/build-docker-tar.sh` 打包后将 tar 及 `.sha256` 移到工作区根（`TAR_MOVE_DIR` 可覆盖）；`dist/docker-bundle/` 不再自包含，包内 README 的 `docker load -i` 需到工作区根取 tar。合并：纯二开脚本。
- **10-15 账号测试模型列表与白名单对齐**：`GetAvailableModels`（`GET /admin/accounts/:id/models`，测试弹窗与定时测试面板共用）原先三分支忽略账号已配置的 `model_mapping`，下拉展示平台全集——可选中账号实际不服务的模型。修复：helper `storedModelWhitelistIDs`（读原始 `credentials.model_mapping`，跳过空键与 `*` 通配符；`GetModelMapping` 对 Grok/GoogleOne 注入默认映射故不可用），三平台 OAuth（`integratedPlatformTestModels`）、Claude OAuth/SetupToken、Gemini OAuth 含 GoogleOne 三分支统一「白名单非空 → 映射请求侧 keys；空 → 平台目录」；通配符映射视为无法枚举回落目录。其余分支横向核实本就对齐。合并：`account_handler.go` 小段插入。
- **10-15 清理死代码 account/AccountTestModal**：`components/account/AccountTestModal.vue` 为 `components/admin/account/` 版本（AccountsView 实际使用）的旧副本，无视图引用，连同 spec 与 barrel 导出行删除。
- **10-15 /admin/usage 分页启用快速跳页**：`Pagination` 组件内置跳页输入框（`showJump` 默认 false，上游无页面启用），主日志表与错误 tab（`OpsErrorLogTable` 内部分页）两处启用；`OpsErrorLogTable` 亦被 `/admin/ops` 复用，同步获得跳页。合并：模板单属性插入，三路合并可自动处理。
- **10-15 qoder 出站平铺工具嵌套归一化（porter 同步）**：修平铺 function 工具出站 400（移植 qoder2api-hub `2fc18d6` / PR #30）——客户端直打 `/v1/chat/completions` 带平铺工具（`{"type":"function","name":...}`）时 `BuildAgentChatBody` 原样透传，上游要求 `tools[].function` 对象而 400。新增 `normalizeTools`（幂等，已嵌套原样透传、非对象条目丢弃、`strict` 保留、缺失 `description`/`parameters` 补默认）在 `BuildAgentChatBody` 的 tools 覆写处统一调用。responses/anthropic 桥产物本就是嵌套形态，不受影响。合并：纯二开文件。连带补 `pkg/qoder/umid_test.go` 缺失的 `//go:build unit` 标签。

### 模型能力测评（2026-10-15 ~ 10-17，已于 10-23 整体移除）

- 曾建立 so 私有模型能力测评：`hard-v1` 动态题库（24→106 题型、难度 D3→D7、评分版本 1.0.0→7.0.0、程序自动判分）、计划/手动执行/排行/模型全景 UI（最终为「模型能力中心」四 Tab 单页）；10-16 起去账号化走公共网关回环（`model_benchmark_settings` 选回环 Key，迁移 243~247），计分原则「执行失败与未执行题均 0 分计入分母」，排名开放。
- 账号测试链路曾植入 benchmark 感知分支（真实题目/输出预算/instructions/usage 事件/429/401 不改账号状态）；**10-23 移除时已全部还原为普通探测语义**。题库演进、判分规则与 UI 迭代的逐版本记录见 git 历史（压缩前 HEAD=`82fc11f`）。

### 2026-10-16 复合分组同族多平台统一调度（所有权多平台从 fail-closed 改为合并池）

- **背景**：同一公开模型名被多个平台的可调度账号在 `model_mapping` 显式认领时，上游 fail-closed 拒绝（"model is exposed by multiple provider platforms" → 503，请求不出网关），跨供应商容量无法互备。
- **语义**：所有权解析（`resolveCompositeModelOwnership`）返回排序去重的平台集合；集合成员同属一个网关族（`domain.UsesOpenAIGateway` 二分，typesafe 属 GatewayHandler 族）时，两条调度链的候选池合并为全部认领平台的认领账号，负载均衡与账号级 failover 跨平台生效；**跨族歧义保留 fail-closed**（Reason 提示配置显式 composite 路由 + `composite_model_ownership_cross_family` 告警）。显式路由 > 所有权 > 前缀探测器的优先级不变。
- **实现要点**：`CompositeModelOwnership` 删 `TargetPlatform/Ambiguous` 改 `TargetPlatforms []string`，ctx 新增集合 key `ResolvedTargetPlatforms`（多平台时不写单值）；新增 `ContextWithSelectedAccountPlatform`，在 11 处选号成功点把实际账号平台回填为单值 `ResolvedTargetPlatform`——`QuotaPlatform`、渠道费率、ops 平台归因等 20+ 处单值读取自动正确；Gateway 族 `resolveSchedulingPlatforms`/`listSchedulableAccountsForPlatforms`（逐平台复用快照桶后按账号 ID 合并去重）；OpenAI 族 `openAIPlatformConstraint` 贯穿 eligibility/recheck/legacy 链，`platformOverride ...string` 语义升级为「全部值=候选集」；计费 `CheckBillingEligibility` 按 `checkUserPlatformQuotaEligibilityAnyOf` 预检（任一候选平台有余量即放行），计量仍按回填后的实际平台。
- **入口门**：`getGroupPlatform` 集合时返回排序首成员作族代表（仅 handler 链分发）；grok 专属门与 openai-only 门改交集判定 `resolvedPlatformSetIntersects`；images 多平台时交集恰 {openai}/{grok} 才放行；gemini v1beta 中间件集合含 gemini 时塌缩单值；WS 守卫集合全部成员 ∈ {openai,grok} 才放行。
- **已知限制**：/v1beta 入口多平台集合塌缩为 gemini 单池；Codex 能力广告维持跨平台别名 fail-closed；在途预留估算对多平台 fail-open；OpenAI WS 多平台集合 transport 放宽为 Any 由调度器按账号能力匹配。
- **合并影响**：composite 解析/调度链为二开密集区（`gateway_scheduling.go`/`openai_account_scheduler.go`/`openai_gateway_scheduling.go` 与上游高冲突面重合），上游同步时按本条目语义重放；不推进 `UPSTREAM-BASE`。

### 2026-10-16 三平台重新授权 URL + Antigravity 用量主动刷新

- WorkBuddy/Qoder/Trae 的「重新授权」入口不再复用 Claude 授权链接（`AccountActionMenu` 中隐藏三平台按钮，其凭据走平台专用 OAuth/刷新链路）；`getAntigravityUsage` 支持 `force` 绕过 3 分钟缓存，前端 Antigravity 用量单元格新增主动查询按钮（`loadActiveUsage`）。改动：`AccountActionMenu.vue`、`AccountUsageCell.vue`、`account_usage_service.go`。合并：二开增量。

### 同步 v0.2.17（2026-10-16）

- **区间**：`UPSTREAM-BASE 4d9ac3e4... -> b0a02a33...`（v0.2.16 → v0.2.17），48 commits / 153 upstream files。
- **策略**：用户明确「支付链路不用，不同步」，`backend/payment/`、webhook、支付前端与测试全部跳过；其余 selective sync（110 文件覆盖/merge，32 跳过，11 个冲突手工裁决）。
- **优先同步项**：composite OAuth 歧义路由拒绝含 OpenAI 网关 OAuth 的组合；GPT-5.5 retirement、Responses `created_at` 修补、Anthropic base URL honoring、min output token floor、Codex import workspace 隔离、web_search emulation、account eligibility；Gemini/Antigravity 的 compat thinking、Vertex SA 已删除处理、共享容量 429 同号退避、Gemini 4 Argon catalog/pricing、tool_result 图片、native functionCall/thoughtSignature、v1beta 路由；Zhipu GLM 5h/weekly 窗口修正、account websearch config、模型与计费价格表；前端 `useModelWhitelist`、Antigravity/AccountModal 兼容修改（AccountTableFilters 保持二开 dense 版）。
- **冲突裁决要点**：`account.go` 保留「Antigravity 空映射直通/非空不强注默认条目」并带入上游 `antigravityDefaultPassthroughModels`；`antigravity_gateway_service.go` 重命名 `buildGeminiTestRequestWithPrompt`、文本路径移除 `maxOutputTokens:1`、保留上游 `toolConfig VALIDATED`；`antigravity_gemini_thinking_variant*.go` 采用上游变体解析；`composite_*`/`gateway_service.go` 保持二开多平台集合语义并叠加上游 OAuth 歧义标记；`vertex_service_account.go` 带入上游错误类型、移除 so 未实现的 Express mode 依赖；`gemini_messages_compat_service.go` 保留 `isVertexSharedCapacity429`/`handleGeminiThirdParty429`、恢复 `parseGeminiRetryDelayResetTime`。
- **测试适配**：`openai_model_retirement_test.go`（platform constraint 类型）、`pricing_service_test.go`（`CacheStorageCostPerTokenPerHour` + raw entry + conversion）、补 `pricing_service.go` 字段。
- **版本号**：`VERSION`=0.2.17；`UPSTREAM-BASE`=`b0a02a3304a0a78cdd003ddda1dd23ba02c03084`。
- **跳过**：支付链路、README/sponsor/logo、en locale、`AccountTableFilters.spec.ts`、PaymentView/StripePaymentView/AccountTestModal 路径差异、部分 cropped-module 测试。

### 2026-10-17 Antigravity 用量改走 retrieveUserQuotaSummary

- **问题/根因**：旧实现读 `v1internal:fetchAvailableModels` 的每模型 `quotaInfo.remainingFraction`，该接口对全部模型恒返回 1（是模型目录能力信息，不是账号用量），列表用量长期显示 0%。真实用量在 `v1internal:retrieveUserQuotaSummary`：按额度组（`Gemini Models` / `Claude and GPT models`）返回 buckets，每组含 5h 与 weekly 窗口（`remainingFraction` + `resetTime`）；CLIProxyAPI 生态同源。
- **修复**：新增 `antigravity.Client.RetrieveUserQuotaSummary`（prod → daily URL 回退、403 归 `ForbiddenError`、1 MiB 读取上限）；`AntigravityQuotaFetcher.FetchQuota` 改走该接口，`UsageInfo` 新增 `antigravity_quota_groups`（key：`gemini`/`claude-gpt`）；`FiveHour` 兼容字段取首个 5h 桶；`fetchAvailableModels` 保留给模型目录同步；使用率改为四舍五入。前端按额度组渲染四条进度条（5h 在前）+ i18n `usageWindow.gemini`；渠道监控 `usageQuotaTiers` 优先消费 `AntigravityQuotaGroups`。
- **合并影响**：涉及上游同名 `antigravity_quota_fetcher.go`/`account_usage_service.go`/`channel_monitor_quota_fetcher.go`/`pkg/antigravity/client.go`，需按二开语义手工合并；`UPSTREAM-BASE` 不变。

### 2026-10-20 推理档位全量透传（废除 max→xhigh 白名单）

- **问题/根因**：Anthropic `--effort max` 经通用桥接先映射为 `xhigh`，再由 `openAICompatAnthropicReasoningEffort` 仅对白名单模型恢复 `max`；`cn:deepseek-v4.1-flash` 未命中前缀 → 出站 `xhigh`（上游支持 low/high/max，会再降 high）。经用户确认改为**全量透传**，不再维护白名单。
- **修复**：删除 `supportsOpenAIReasoningEffortMax` 与事后恢复逻辑；`mapAnthropicEffortToResponses` 原样返回；`normalizeOpenAIReasoningEffort` 对 `xhigh`/`max` 原样返回（`extrahigh` 仍归一 xhigh）；模型名后缀 `-xhigh` 不经 max 中转。群组级 `max_reasoning_effort` 上限/映射策略不变。
- **合并影响**：`apicompat/anthropic_to_responses.go`、`openai_compat_model.go`、`openai_gateway_messages{,_chat_fallback}.go`、`openai_gateway_request_body.go` 为上游同名文件，同步时需保留全量透传语义；`UPSTREAM-BASE` 不变。

### 2026-10-23 移除模型能力测评功能（保留模型清单）

- **范围**：整体移除 benchmark，仅保留模型清单（不再展示评分）。后端删除 `model_benchmark_*` service（22 文件）/repository/handler/路由 `/admin/model-benchmarks/*`/设置项/账号测试挂钩，`model_manifest_service.go` 剥离评分注入；前端删除测评页面/组件/API/i18n，`ModelCapabilityCenterView.vue` 收敛为仅清单、`ManifestTab.vue` 删评分列；文档与 openspec 提案一并删除。
- **迁移**：`250_so_drop_model_benchmark.sql`——既有库 DROP 三张表并清空 243~249 的 `schema_migrations` 记录，全新库空操作，幂等（临时 PG16 双场景验证）。升级镜像会自动执行，需留档先备份三表（已备份 27MB）。
- **合并影响**：无（so 私有功能，上游无对应文件）。**编号占用**：250 已占用，`ADVERSARIAL-SOLUTIONS.md` 的 AUTH-04（`250_so_user_token_version.sql`）顺延 251+。

### 2026-10-26 OpenCode（Zen）多轮会话 400：历史 reasoning 缺 summary

- **问题/根因**：`muse-spark-1.3-contributor` 第二轮起 `/v1/messages` 报 400 `input[3] missing required field summary`。Zen muse-spark 走 `/zen/v1/responses`，Meta Responses 协议要求回放的历史 `type:"reasoning"` 必带 `summary`（允许空数组），裸 reasoning / 顺序不合规同样 400；so 的 `anthropicAssistantToResponses` 只回填 `encrypted_content` 不写 `summary`。上游 #7309 文本一致且未修。
- **修复**：新增 `SanitizeOpenCodeResponsesReasoningReplay`（`openai_gateway_request_body.go`）按「keep it whole or drop it」整条删除 `input` 内所有 reasoning item，保留 function_call / function_call_output 配对；只改 `input`，其余字段经 `decodeOpenAIJSONUseNumber`/`marshalOpenAIUpstreamJSON` 原样保留。由 `normalizeOpenCodeUpstreamReasoningReplay` 在 `buildUpstreamRequest`（普通转发）与 `buildUpstreamRequestOpenAIPassthrough`（透传构造）收口，门控：OpenCode 平台账号或指向官方 `opencode.ai` 主机；覆盖 `/v1/messages`、`/v1/chat/completions`、`/v1/responses`。其他平台 reasoning 回放不变。顺带抽出通用 `dropOpenAIInputItemsWhere`（跨模式 failover 清洗逐字节等价）；新增测试 `openai_opencode_reasoning_replay_test.go`（普通/透传构造、官方主机门控回归 + 进程内端到端）。
- **合并影响**：`openai_gateway_request_body.go`、`openai_gateway_forward.go`、`openai_gateway_passthrough.go` 为上游同名文件，同步需保留收口语义；上游修复 #7309 后应复核并优先采用上游方案。`UPSTREAM-BASE` 不变。

### 2026-10-26 用量统计：模型分布折叠 + 按 API 统计用量（含审查修复）

- **功能**：① 模型分布列表去掉纵向滚动条，默认 Top 6 + 「展开全部/收起」切换，环图始终统计全量；② 管理端/用户端用量统计页新增「按 API 统计用量」板块（与管理控制台口径一致）——后端新增 `GET /api/v1/usage/dashboard/api-keys-usage-stats`（缺省今天、limit 50 上限 200，限定当前用户），形状与管理端 `api-keys-usage-stats` 一致；前端类型迁入 `@/types` 共用，新增 `ApiKeyUsageTable.vue` + `UsageApiKeyStatsCard.vue`（scope=admin/user 分流，user 隐藏归属列），日期变化与刷新联动。
- **审查修复（11 处）**：`DashboardAPIKeysUsageStats` 只解析日期（`parseAPIKeyStatsDateRange`：end_date-only 起点取当日、start>end/非法日期 400、`AddDate` 日历日推进兼容 DST，修夏令时漏/多计一小时与 end_date 回显错日）；`GetAPIKeyUsageStatsByUser` 明细子查询补 `u.user_id` 过滤、改用命名返回值使 `Close()` 失败可回传；前端密钥卡片达到 limit 时显式提示「仅显示前 N 个」、卡片在首屏统计完成后挂载（消除重复请求）、展开工具条仅在正常态渲染、模型分布恢复 `overflow-x-auto` 与明细限高、`modelsExpanded` 在数据源/日期/指标变化时复位。
- **验证**：新增 `usage_handler_apikey_stats_test.go`（end_date-only、start_date-only、America/New_York 夏令时 25 小时日、倒序区间/非法日期 400、过滤参数被忽略）；后端 handler/repository 单测全绿；前端 vitest 287 文件 2131 用例全绿，typecheck/ESLint 通过。
- **合并影响**：`usage_handler.go`、`routes/user.go`、`usage_service.go`、`usage_log_repo_apikey_stats.go` 为上游同名文件，同步需保留 ByUser 查询与 handler；其余为 so 私有。`UPSTREAM-BASE` 不变。

### 同步 v0.2.19（2026-10-29，跨 v0.2.18）

- **区间与规模**：`b0a02a33 → a3298b46`（121 提交 / 354 文件，+30277/-1217）。二开侧：117 个未改文件直接覆盖、129 个上游新增复制（另 4 个新增文件跳过/适配）、76 个三路自动合并、15 个冲突手工裁决、13 个二开已裁剪文件跳过。版本号 `VERSION`→0.2.19（tag 推导一致）。
- **同步功能**：① 按操作/资源范围授权的管理员 API 密钥（`pkg/adminaccess` + `custom_admin_key_*` 全链路 + 迁移 247 + `AdminAccessKeysSettings.vue` + zh 语言包 + `docs/admin-api-keys.md`，step-up 依赖按二开裁剪改直调）；② Grok 账号模型目录诊断（`grok_model_catalog*`/`grok_model_diagnostics` + `GrokModelDiagnosticsPanel.vue`）；③ 多图按张计费（`image_billing_count` + 迁移 248）与 Gemini 生图分组开关（迁移 246）；④ Gemini `/v1beta/models` 声明映射应答（`DeclaredModelIDs`/`DeclaredModelsCover` 移植进 `gemini_messages_compat_service.go`，handler 保留二开 `SelectAccountForAIStudioEndpoints` 命名）；⑤ Gemini 同账号重试可配置（`gateway.max_same_account_retries_gemini` + `geminiMaxAttemptsPerAccount`）；⑥ Gemini 默认清单刷新（2.x 全线下线，`DefaultTestModel`→`gemini-3.5-flash`）；⑦ 流式客户端断开按已收集用量计费族；⑧ 计费修复（Grok 4.7 Fast 官方价、DeepSeek V4 Pro 保留、cache hit 计为缓存读取、按端点/计价模式限定按张计费）；⑨ OpenAI/Codex 修复（透传非流式 SSE 无 Content-Type、overload 重试保持、非流式 JSON 返回、GPT-6.1 Terra）；⑩ 限流/安全（Cloudflare 1010 不惩罚、国产频率型 429 秒级冷却、管理员保护规则 400、分页 offset 溢出、超大 multipart 拒绝、`golang.org/x/net` v0.62.0 / dompurify 3.4.17）；⑪ 性能（配额累加按周期维度选语句、调度快照常驻内存、限流快照 COW、模型映射签名零分配、singleflight 复查）。
- **存量缺口补齐（v0.2.17 选择性同步漏项）**：`vertex_service_account.go` 覆盖为上游新版，补入 `IsVertexAPIKey`/`buildVertexGeminiAccountURL`/`setVertexGeminiAuth`（Vertex API Key Express 模式），并把 `gemini_messages_compat_service.go`（×2 处）与 `gemini_chat_completions_compat_service.go`（×1 处）的服务账号分支改为统一走上述构造/认证函数（原实现无条件要求 tokenProvider，API Key 账号无法转发）。
- **冲突裁决（15 文件）**：README×3 保留二开赞助商清单不并入上游新增行；ent 生成代码×3 以「`schema/group.go` 合入 `image_count_billing` 后 `go generate ./ent` 全量重生成」替代手工合并；`gemini_v1beta_handler.go` 上游声明映射逻辑+二开方法名；`gemini_messages_compat_service.go`/`billing_service.go` 显式缓存相关维持二开裁剪（不引入 `boundCacheUpstreamPassthroughError`/`CacheStoragePricePerTokenHour`）但保留上游 `geminiMaxAttemptsPerAccount`；`upstream_models.go` 按上游重构为 `fetchUpstreamModelListRequest`（4 返回值 + Grok 目录观测）并保留二开 workbuddy/qoder/trae 三平台分支；前端 5 文件保留二开裁剪与 dense 样式、并入上游新功能（`supportsAccountQuota`、过滤持久化且路由平台参数优先、`AdminAccessKeysSettings` 挂载 `space-y-4`、移除 step-up/TOTP 依赖）。
- **跳过项**：13 个二开已裁剪文件（`.github/workflows`、en 语言包、`ChannelsView.vue`、`gemini_cached_content.go` 等）；上游新增 `gemini_cached_content_bound_retry_test.go`（依赖裁剪符号 `GeminiExplicitCacheUpstreamCredentialKey`）与 `ChannelsView.webSearchEmulation.spec.ts`（视图已裁剪）；`upstream_models.go` 的 `fetchVertexGeminiUpstreamModels` 分支（属已裁剪的 `gemini_models_endpoints.go`，Vertex 账号模型同步维持通用路径，行为与 HEAD 一致）；`routes/payment.go` 按三路规则覆盖（仅新增 admin payment 组全局限流中间件，非支付业务链路，如需维持支付零变更可单独回退该文件）。
- **测试适配**：`GrokModelDiagnosticsPanel.spec.ts`/`AdminAccessKeysSettings.spec.ts` en/zh 双语收敛为 zh 单语（删英文用例与 en 导入、补 zh 文案完整性用例、移除 step-up mock）；`gateway_models_test.go` 静态清单断言 `gemini-2.0-flash`→`gemini-3-flash`；`account_handler_available_models_test.go` plain oauth fixture `gemini-2.5-pro`→`gemini-3-flash`（2.x 移出 `DefaultModels`，Google One 清单仍保留 2.5）；`openai_gpt61_terra_test.go` 删 `openAICompatAnthropicReasoningEffort` 断言（该函数属 10-20 透传改造删除范围）；新增 `gemini_vertex_api_key_test_helpers_test.go`（自上游裁剪测试文件提取两个构造辅助，供生图开关/断开计费测试复用）。
- **构建链**：Go 1.27.2→1.27.3（go.mod 随上游；根/`backend`/`deploy` Dockerfile 与 so-dev compose 同步换 `golang:1.27.3`）；`pnpm-lock.yaml` 直接采用上游版（package.json 二开未改，`--frozen-lockfile` 校验通过）。
- **验证**：后端 `go build`/`go vet`/`go test -tags=unit ./...` 失败仅 6 个存量项（`TestApplyOpenAICompatModelNormalization`[10-20 透传遗留] / `TestResolveGeminiThinkingVariant` / 复合分组族 ×2 / `TestEveryGatewayPOSTRouteIsClassifiedForPromptAuditCoverage` / `TestConfiguredUpstreamModelsForCapabilitySyncSkipsInjectedAntigravityPassthroughs`，经 HEAD 工作树基线比对确认同步前即失败）；`TestTraeOpsService_Registration` 全量并行下偶发（全局注册表覆盖式设计，单跑必过，非本次引入）。前端 typecheck + build（i18n 完整性 + vue-tsc + vite）全绿、vitest 290 文件 / 2265 用例全过。版本号 tag 推导 0.2.19 == VERSION 0.2.19。ent 重生成后仅预期 9 文件变更。gofmt 漂移集与 HEAD 一致（无新增）。
- **环境备注（NAS）**：前端 node_modules 在宿主 fs mode 000 下 esbuild 无法执行，`install`/`build`/`test` 一律在 `node:24-alpine` 容器执行（`corepack prepare pnpm@9.15.9 --activate` 固定版本，pnpm 12 会因 overrides 报错）；构建需挂整个 `sub2api-so`（LegalDocumentView 引用 `docs/legal/*.md`）；docker 挂载必须用 `/vol2` 宿主路径（`/tmp` 在 terminal-buddy 容器内，宿主 daemon 不可见）。

### 2026-11-03 移植源同步（qoder v1.7.6→v1.7.14、trae 2026.10.26→11.03）

- **区间**：qoder `0426c334 → 449e0da`（39 提交，v1.7.7→v1.7.14，含 PR #104~#131）；trae-workbuddy-switch `1707fae1 → 930d149`（约 130 提交，含 P13 降级链、跨平台降级、issue #87/#88/#100~#154）。**已移植 8 项**（用户全选缺陷修复类；功能增强类经用户裁定「暂不需要」）：
- **qoder 出站体（`pkg/qoder/baseprompt.go` + 新增 `conversation_identity.go`）**：① `business.model_key` 计费归因 + `business.version` 对齐 COSYVersion（PR #119/f686228：缺失时上游把 kmodel_latest 类静默记成 auto）；② 会话身份稳定化（747f83a + db99d43 #128）：`chat_record_id`/`business.id`/`begin_at` 按「显式会话键（头 > 请求体）+ 本轮提问摘要（最后一条 user + 系统 + 模型，`turn:`）」在 LRU（4096/6h）内复用——同一次提问内的工具步进同一条，用户下次提问换新 id；`request_id` 仍逐请求唯一，`QD_CONVERSATION_SHARE=0/false/no` 可关闭，`QD_CONVERSATION_TTL` 可覆盖空闲有效期；③ namespace 容器（显式 `type:"namespace"` 或省略 type + children）递归展开（深度 4，同名留首个）替代整块丢弃（6fdc252 #127）。
- **qoder 模型解析（`pkg/qoder/models.go`）**：动态显示名索引（e53ca20/PR #119）——`FetchModels` 合并结果注册「display_name→key」，`ResolveUpstreamKey` 顺序变为 别名→冻结显示名→动态显示名；补 `kimi-k3.1-pro→kmodel_latest` 离线别名兜底。官方改名窗口期客户端新名不再被原样当上游 key 透传。
- **qoder 错误分类（`pkg/qoder/errors.go` + `service/openai_gateway_qoder.go`）**：额度类 403 识别（1033662 #108）——112/pricingUrl → 冷却 30min，110/Billing daily count exceeded → 冷却到本地 0 点（封顶 6h）；建流前与流内两条处置路径均长冷却换号、不刷新 token、不禁用账号，单号池不被压成 3s；对外分类为 429 而非 401。
- **qoder SSE 守卫族（`pkg/qoder/sse.go`，7f749f5/f10c627/a59f89e）**：① hold 期间到达的 usage 垫底帧（含空 `{}`）缓存到流末、跟在收尾帧后补发（此前整轮被吞）；② 收尾帧被扣且无缓冲时先放行再透传 usage，恢复 `[finish]→[usage]` 标准顺序；③ 超限 fail-open 放行前补 marker-only 判定，命中即 fail-closed（新增 `failErr`）；hold 帧计数改为只计空转帧、判定候选前剥离尾部 DSML 闭合标签（源 #36 后续）；④ 新增 `GuardNamelessToolCalls`（#124 同批）：name 未到先扣住、到达后一次发出 id+name+累计参数、流末无名则丢弃并把 tool_calls 降级 stop；⑤ 新增 `GuardTruncatedToolArgs`（#112）：流末「见过 tool_call 增量 + 无 finish_reason + 参数非法 JSON」→ 返回错误让客户端重试；⑥ 合成正文帧搬运同帧 `tool_calls`/`reasoning_content` 增量（#124）；工具名集合递归包含 namespace 子工具（与出站展平同口径）。
- **qoder 签到（`pkg/qoder/accounts.go`）**：活动列表空结果/无可领项时重试两次（0.8s/2.0s），避免一次抖动丢掉当天签到（fc8d46e #130 第 1 项；同人去重盖章本侧已有等价实现，不重复移植）。
- **trae 积分包（`pkg/trae/usage.go`）**：包名改取 pack 顶层 `group_name`→`display_desc`，`entitlement_base_info.package_name/name` 仅作兼容回退（27250bd #143；线上顶层字段才存在，旧实现命中率恒 0 → 全部「未命名包」）。
- **评估未移植（本次裁定）**：qoder effort clamp 双向化（本侧 `NormalizeReasoningEffort` 已双向）、额度 403 之外的面板/SSRF/白名单/Web 工具记账/i18n（面板专属或 Go 免疫）、签到 SAME_PERSON 盖章（已有 6h 冷却等价）、调度器 fired 持久化（自有 ops 调度）；trae 保活窗口/令牌家族兜底（本侧 30min 滑窗+持续轮询+缺过期即判刷+job 兜底 24h，架构免疫）、Anthropic 出口 usage 补齐与 Responses namespace/并行工具合并（本侧链路已完整/桥层已等价）、模型降级链 P13、跨平台降级、账号级 auto_failover、顺序选号档、冷却指纹自证、会话导出/导入、逐资源包明细、记忆迁移、桌面端 session/钥匙串/UI/CI 类。可选增强（上下文窗口可配置 #106、模型图片能力内置表 #142、粘性键读入站会话头 #150、模型降级链）经用户裁定本次不做。
- **改动文件**：`pkg/qoder/{baseprompt,models,errors,sse,accounts}.go`、新增 `pkg/qoder/conversation_identity.go`、`pkg/trae/usage.go`、`service/openai_gateway_qoder.go`；测试新增 `pkg/qoder/porter_sync_test.go`、`pkg/trae/credit_pack_name_test.go`，更新 `pkg/qoder/baseprompt_test.go`（request_id 与 chat_record_id 分离断言）。**合并影响**：全部为二开文件（`pkg/qoder`/`pkg/trae`/`openai_gateway_qoder.go` 均在三平台源码集成时新增），无上游同步冲突面；不触碰 `UPSTREAM-BASE`。`PORTER-BASE`：qoder → `449e0daa`、trae → `930d1495`。
- **验证**：本机 Docker `golang:1.27.3`（go.mod 实际要求 1.27.3）`go build ./...` 通过、`go vet` 目标包通过；`go test -tags=unit ./internal/...` 仅 6 个存量失败（与基线清单一致），`pkg/qoder`、`pkg/trae` 与 service 的 Qoder/Trae 相关用例全绿。
- **独立验证与修正（同日，提交 `57bf968`）**：两个只读 subagent 逐项对照源实现并做变异验证，修正 9 处：① 补移植同区间漏项 `db99d43`（会话按「一次提问」切分：最后一条 user + `turn:`，显式键追加提问摘要）；② `QD_CONVERSATION_SHARE` 兼容 `false/False/no`，新增 `QD_CONVERSATION_TTL`；③ 入站会话头（`X-Conversation-Id`/`Conversation-Id`/`X-Session-Id`/`Session-Id`）优先于请求体；④ 补 `nameless` tool-call 守卫；⑤ usage 帧接受空 `{}` 字典；⑥ hold 帧计数只计空转帧、候选判定前剥离尾部 DSML 闭合标签；⑦ namespace 识别补「省略 type + children」形态；⑧ 工具名集合递归含 namespace 子工具；⑨ 错误分类顺序对齐处置函数（排队→额度→凭证失效），trae 纯空白包名按无名处理。测试由 7 个用例扩至 18 个（按提问切分、头键优先、禁用变体、TTL 覆盖与边界、空 usage 帧序、marker-only fail-closed、同帧增量搬运、nameless 两路径、NewChatStream 接线、深度上限、110 下界、未知类型 namespace、空白包名）。
- **已知偏差（接受，不再重复评估）**：动态显示名只在 `FetchModels`（账号模型同步/测试）时注册，未移植源请求路径的离线补注册（本侧无落盘目录缓存，已知改名由 `kimi-k3.1-pro` 别名兜底）；建流前 failover 错误仍携上游 403（流内合成状态为 429，源亦 re-raise 403）；`ClassifyEnvelopeError` 在生产代码无调用（额度处置不经它）。

### 2026-11-06 trae 积分耗尽处置、流内错误入看板与对话会话身份稳定化

- **问题/根因（线上实证，NAS 生产实例）**：账号 `98760`（id=608）积分归零后，网关仍继续调度 27 分钟、52 次 400~700ms 的立即拒绝（`usage_logs` 里 input/output 全 0），`accounts` 状态始终 `active`、`ops_error_logs` 零记录。根因三点：
  1. `handleTraeUpstreamStreamError` 只处理合成 401，积分类错误仅打一行 slog；而全部请求走 `/v1/messages` + stream，命中的正是这条路径。`classifyTraeUpstreamFailure` 的 429 映射只在非流式聚合路径写回 `resp.StatusCode` 才生效。
  2. trae 流内错误不进 `ops_error_logs`（wire 状态已固化为 200，通用错误链采集不到），失败在面板上完全隐形。
  3. `pkg/trae/payload.go` 的 `conversation_id` / `session_id` / `project_id` 恒为**每请求随机 UUID**——移植源 `88ed652` 的「对话复用稳定会话 ID」未随三平台集成搬入（相对基准的漏搬缺陷，已补齐）。**注意：该项与本批次「缓存不命中」不是因果关系**——真机对照实验（见下「缓存验证结论」）证明稳定会话 ID + `prompt_cache_key` 均无法让上游回报命中量。
- **修复**：
  - **`trae_no_credit` 罚停与自动恢复**：流内错误命中严格耗尽标记（口径同移植源 `HARD_CREDIT_MARKERS`，16 条中英短语；刻意不含裸 `quota`/`credit`/`insufficient`，避免「配额查询失败」类误伤）→ 快照清零 + `SetError(trae_no_credit)` + `Schedulable=false`；`storeTraeQuotaSnapshot` 落库后统一做恢复判定（额度 > 0 且确系本逻辑罚停才解冻），签到 / 周期刷新 / 手动查询三条路径共用一份语义。非流式聚合路径同样接入（此前只写状态、账号状态不动）。
  - **流内错误入看板**：`markTraeStreamErrorForOps` 经 `MarkOpsStreamFailure` + `appendOpsUpstreamError` 登记（先记上游尝试归因再标记，保证快照可见）；错误类型按积分耗尽 / 鉴权 / 限流 / 参数错分档，`CountTowardsSLA=true`。
  - **会话身份稳定化**：移植 `conversation_identity_source` / `content_fingerprint` / `stable_conversation_id`——标识优先级 `prompt_cache_key` > `metadata.session_id` > 内容指纹（system + 首条 user）> `user`（固定安装 ID 类的 `user` 必须垫底，否则会把该客户端全部对话塌缩成同一会话号）；SHA256 前 16 字节 → v4 位形 UUID，`conv:`/`sess:`/`proj:` 各一把盐，`uid` 混入哈希做跨账号隔离。取不到指纹才退回随机 UUID。
  - 顺带修正 `credits_monitor` 的临期文案（判据早已是 14 天，日志/note 仍写「7 天内到期」），并让它复用 `storeTraeQuotaSnapshot` 而非自己再查一遍上游。
- **改动文件**：`pkg/trae/payload.go`（会话身份派生）、`service/openai_gateway_trae.go`（错误处置 / ops 登记 / 非流式接线）、`service/trae_oauth_service.go`（`TraeNoCreditReason`、`zeroTraeQuotaExtra`、`recoverTraeNoCredit`）、`service/trae_ops_service.go`（`runCreditsMonitor` 收口）、`service/openai_gateway_cc_pipeline.go`（`wrapTraeChatResponse` 传入 gin 上下文）。测试：新增 `service/openai_gateway_trae_credit_test.go`（10 用例），`pkg/trae/payload_test.go` 增 4 个会话身份护栏用例并修正原「每次随机」断言。
- **合并影响**：全部为二开文件（`pkg/trae`/`service/openai_gateway_trae.go` 等均在三平台源码集成时新增），无上游同步冲突面；不触碰 `UPSTREAM-BASE`，`PORTER-BASE` 亦不变（本次为补齐基准 `dfa5bf7` 内已含的 `88ed652` 移植缺口，非新基准）。
- **缓存验证结论（2026-11-06 真机对照实验，已定论）**：用生产账号真实凭据直连上游 `llm_utils_chat` 做 A/B：
  - 实验 1：4254 token 大前缀（20047 字 system），同一组稳定 `conversation_id`/`session_id`/`project_id` 连发两次 → 两次均 `cache_read_input_tokens:0`（上游 `metadata` 帧回显了同一个 `session_id`，证明稳定 ID 确实送达并被接受）；
  - 实验 2（对照）：再注入源 `outbound/mod.rs` 第 6 步的 `prompt_cache_key`（`wb2a-<uid8>-<sha256(uid|conv)[..16]>`）→ 仍为 0。
  - 上游 `token_usage` 帧**确实携带** `cache_creation_input_tokens` / `cache_read_input_tokens` 两个键（so 侧 `cachedTokensOf` 已认），且 `metadata.prompt_cache_pool_record` 恒为 `null`。
  - **结论**：不是解析缺陷、不是会话 ID 缺陷——是上游（SOLO CN / `ep-2026...` 后端）对当前流量**未启用前缀缓存计费**。源作者 11-05 本机实测其网关 200 条日志同样 0 条命中（其 96.33% 样本来自 WorkBuddy 客户端 jsonl，非 trae），与本结论一致。
  - **未做**：国际版变体（`core-normal.trae.ai`）对照、官方 IDE 真机抓包对比（需额外投入与账号，待定）。
- **遗留**：`usage_refresh` 快照目前仍不参与调度门控（6 小时盲窗期内若上游未报耗尽文案，账号不会被拦）——该语义变更单独评估。

### 同步 v0.2.20（2026-11-07）

- **区间与规模**：`a3298b46 → 6b4b37db`（107 提交 / 458 文件，+29283/-1455）。二开侧：204 个未改文件直接覆盖（+1 上游已删文件同步删除）、142 个上游新增复制（另 3 个新增文件因引用已裁剪模块跳过）、97 个三路自动合并（77 零冲突 + 20 冲突手工裁决）。版本号 `VERSION`→0.2.20（tag `v0.2.20` 推导一致）。
- **同步功能**：① **MCP 管理服务端**（`internal/managementmcp/*` 新增模块、`mcp_oauth_service.go`/`mcp_oauth_handler.go`/`repository/mcp_oauth.go`、迁移 `249_mcp_oauth.sql`、`routes/mcp.go`、`docs/mcp.md`；前端 `MCPView.vue`/`MCPOAuthAuthorizeView.vue`/`api/mcp.ts`/zh 语言包 `mcp.ts`/侧边栏 MCP 入口与 `mcp` 功能开关）；② **注册邮箱精确黑名单**（收件箱级归一：Gmail 点号、googlemail 域、`+` 后缀；`registration_email_blacklist.go`/`admin_user_email_blacklist.go`、`registration_email_blacklist` 设置项、`RegistrationEmailBlacklistEditor.vue`、可批量禁用命中的已注册用户）；③ **OpenAI 生图尺寸档位分流**（1K/2K/4K，`openai_image_size_routing.go`、`selectAccountWithSchedulerBySize` 尺寸 pass、`OpenAIImageSizeTiersField.vue`、GPT Image 2.6 原生链路）；④ **Antigravity 额度按组展示**（`retrieveUserQuotaSummaryFromURL`、Pro 计划 Argon 独立行、INTERNAL 500 惩罚限定图片模型、账号测试发送管理员提示词）；⑤ **网关资源安全**（`pkg/httputil/*` body budget/read timeout/write timeout/upstream drain、`middleware/request_body_memory.go`、`request_body_read_failure.go`、`sse_transport_limits.go`、`session_memory_cache.go`）；⑥ **性能热路径**（thinking 块过滤免解码、Codex bootstrap/compaction 免全量解码、Gemini 流式前缀免拷贝、auth 快照读隐私门、RPM override 免逐请求查询、JSON 拷贝削减）；⑦ **管理密钥统计与全权模式**（`custom_admin_key_statistics.go`、`custom_admin_key_openai.go`、全权兼容模式需输入确认短语、权限中英文展示）；⑧ **一批修复**（Grok Voice body 读失败分类、Codex WS 每轮首 token、WebSocket HTTP 桥指纹收敛、Antigravity 直连 Gemini 流式保留/identity 前置语/stale identity 中和/`responseJsonSchema`→`responseSchema`、Cloudflare REST messages 免 beta、Gemini 工具结果图片、Responses 消息内容字段保留与缓冲 reasoning、GPT-6.1 Terra effort 校验与订阅长上下文附加费豁免、Claude setup-token 周额度、Kimi 并发 403 短冷却、Codex 指纹、错误透传规则可覆盖内置 413、开放空 instructions 回显、用户 CSV 导出分页 1000 条按 Retry-After 重试、MCP client 注册无状态化、brotli 1.2.7/klauspost 1.20.2 修 govulncheck、vue-i18n v11）。
- **冲突裁决（20 文件）**：`wire_gen.go` 补上游新增 `balanceNotifyService`/`digestSessionStore` 与 MCP OAuth 装配（`ProvideAuthService`+`ProvideHandlers` 加参、`provideCleanup` 形参补两项），未引入二开已裁剪的 `geminiCachedContentService`；`openai_gateway_handler.go` 以二开 HEAD 版重新三路合并（首轮自动合并丢失上游 3 个新函数：`mayContainCodexCallOutputCandidate`/`applyOpenAIFailoverPassthroughRule`/`handleForwardRequestBodyMemoryOverload`），生图尺寸路由判定改为「集合含 openai」（`openAICapabilityPlatformFor(requestPlatforms)`），cyber session block 改用上游 `CyberSessionBlock{ScopeKey,Keys,TranscriptLengths}`（保留二开 scope 逻辑）；`antigravity_gateway_service.go` 账号测试全面采用上游「发管理员提示词 + `antigravityTestMaxOutputTokens=256`」；`gemini_messages_compat_service{,_test}.go` 采用上游 `multimodalToolResults` 工具结果图片（补齐 `toolImageParts`/`extractClaudeToolResultImageParts`，属 v0.2.17 起二开存量缺口）与 `convertClaudeMessagesToGeminiGenerateContent(body, model)` 新签名；`openai_gateway_request_body.go` 保留二开全量透传（不恢复 `supportsOpenAIReasoningEffortMax`），函数随上游改名 `validateGPT61CompatRequest`；`account_usage_service.go`/`antigravity_quota_fetcher.go` 保留二开额度组实现（`Buckets` 口径，`RetrieveUserQuotaSummary` 三返回值），删除上游同名重复类型；`model_rate_limit.go` 恢复 `isAntigravityGeminiImageModel` 与两处判断（上游 v0.2.20 图片模型 INTERNAL 500 独立冷却依赖）；`ops_upstream_context.go`/`ops_error_logger.go` 恢复 `SetOpsLocalErrorType`/`MarkOpsRequestScopedError` 机制（Google 格式错误无 type 字段需按 ops 口径归类）；`gemini_v1beta_handler.go` 补 `googleErrorWithType`/`googlePlatformError`/`googleClientErrorType`；前端 `AccountUsageCell.vue` 保留二开额度组渲染（`antigravityQuotaRows`），`AdminAccessKeysSettings.vue` 以上游新版重放「移除 TOTP step-up」裁剪，`SettingsView.vue` 只并入邮箱黑名单编辑器（不并入 promoCode/invitationCode），`router/index.ts` 并入 `/settings/mcp` 与 `/oauth/mcp/authorize`（仍裁剪 `/available-channels`），`AppSidebar.vue` 保留 BeakerIcon 并并入 MCPIcon 与 MCP 入口，`featureFlags.ts` 并入 `mcp` 开关，`types/index.ts` 保留二开 `AntigravityQuotaGroup{key,display_name,buckets}` 并补 `AntigravityQuotaWindow`，zh 语言包并入 `emailBlacklist` 与额度组标签（跳过 promoCode/invitationCode）。
- **跳过项**：`.github/workflows/*`（3，二开无 CI 目录）、`backend/internal/handler/gemini_cached_contents_handler.go`（依赖已裁剪的显式缓存链路）、`frontend/src/components/payment/__tests__/SubscriptionPlanCard.spec.ts`（支付前端永久不同步）、en 语言包 5 文件 + `locales/en/mcp.ts`（en 永久跳过）、`MCPLoginRedirect.spec.ts`（引用已裁剪的 OAuth 登录组件）、`AdminAccessKeysSettings.i18n.spec.ts`（引用 en 语言包）、`backend/internal/pkg/antigravity/quota_summary.go`（与二开 `client.go` 实现重复，二开版含 ForbiddenError/原始响应/日志，保留二开的 `FromURL` 方法移植）、上游新增 `antigravity_quota_groups_test.go`（测已裁的上游实现，二开 `antigravity_quota_fetcher_test.go` 已覆盖同语义）、`request_body_read_log_test.go` 中 `TestGeminiRequestBodyReadTimeoutClassifiedAsClient`（依赖已裁的 `readGeminiCachedContentBody` 与未引入的 `ops_error_logger_google_test.go` 辅助）、`MCPOAuthAuthorizeView.spec.ts` 两个 step-up 用例（二开无 TOTP UI，视图已按裁剪改直调）。
- **测试适配**：`AdminAccessKeysSettings.spec.ts` mock 补 `te`/`locale`（上游改用 `useAdminAccessKeyPermissions`，`te` 统一 false 走 `unknownPermission` 回退，与 zh 单语口径一致）；`AppSidebar.spec.ts` 只保留 MCP 入口用例（二开无订阅/购买入口）；`AccountUsageCell.spec.ts` 上游「额度组 5h/weekly」用例改写为二开 `buckets` 结构（断言 `gemini 5h`/`gemini 7d`/`claude 5h`/`claude 7d` 顺序与回退口径）；`gemini_messages_compat_service_test.go` 采用上游新签名用例。
- **依赖**：`go.mod` 随上游新增 `MoeForge/moego`、`modelcontextprotocol/go-sdk v1.4.1`、`brotli v1.2.7`、`klauspost/compress v1.20.2` 等；`go mod tidy` 后 `go.sum` 同步。`package.json` 二开未改，`pnpm-lock.yaml` 采用上游版并 `pnpm install` 校验。
- **验证**：后端 `go build ./...`、`go vet ./...`、`go vet -tags=unit ./...` 全通过；`go test -tags=unit ./...` 失败 6 项与基线完全一致（`TestResolveGeminiThinkingVariant` ×2 子例 / `TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants` / `TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership` / `TestConfiguredUpstreamModelsForCapabilitySyncSkipsInjectedAntigravityPassthroughs` / `TestApplyOpenAICompatModelNormalization` / `TestEveryGatewayPOSTRouteIsClassifiedForPromptAuditCoverage`），无新增失败。前端 `pnpm typecheck` 通过、`pnpm test:run` 297 文件 / 2329 用例全过、`pnpm build` 成功。版本号一致性：新基准 `git describe --tags 6b4b37db` → `v0.2.20`，`VERSION`=0.2.20 == 上游 `VERSION@6b4b37db`（同步前基准 `a3298b46` → `v0.2.19`）。支付链路与 en 语言包零变更（`git status` 复核）。
- **遗留**：`src/views/auth/__tests__/MCPOAuthAuthorizeView.spec.ts` 中「administrator write access always needs two-factor verification」用例保留但仅断言 `adminWriteHint` 文案展示（视图已无 step-up 拦截，提示文案仍展示，后续可视需要移除该 hint）；二开 `QuotaGroup`/`QuotaBucket` 与上游 `QuotaSummary*` 命名并存（前者为二开主实现，后者仅在 `FromURL` 兼容方法内部使用）。

### 修复 Docker 构建 pnpm frozen 失配（2026-11-09）

- **现象**：本机执行 `update-local.sh`（`docker build` 根 `Dockerfile`）在 `[frontend-builder 5/8]` 失败：`ERR_PNPM_LOCKFILE_CONFIG_MISMATCH — The current "overrides" configuration doesn't match the value found in the lockfile`。
- **根因**：`24c6ba9`（同步 v0.2.20）采用的 `frontend/pnpm-lock.yaml` 丢失了 `overrides:` 段（`package.json` 有 7 条 `pnpm.overrides`，lockfile 一条都没有，`postcss` specifier 仍是未覆盖的 `^8.4.32`）。本机 pnpm 12.16.1 不读 `package.json#pnpm.overrides`（pnpm 10+ 视其为死配置，只认 `pnpm-workspace.yaml#overrides`），同步流程里本机 `pnpm install` 校验时把 lockfile 洗成了无 overrides 版本；而根 `Dockerfile` 固定 `pnpm@9`，pnpm 9 仍从 `package.json` 读 overrides 并严格比对 lockfile → 必然失配。历史 `2a53a42` 曾专门用 pnpm 9.15.9 重生成补回该段（10-21 四连败之一），本次为同类问题复发。仅手工往 lockfile 头部插 `overrides:` 不够：pnpm 9 会把覆盖应用到 importers 的 specifier（如 `postcss` 变为 `>=8.5.18`），仍报 `ERR_PNPM_OUTDATED_LOCKFILE`，必须由 pnpm 9 完整重生成。
- **修复**：① 用 `corepack pnpm@9.15.9 install --lockfile-only` 重生成 `frontend/pnpm-lock.yaml`（`lockfileVersion: '9.0'`，含完整 7 条 overrides，`postcss` 8.5.29 等解析结果与上游 v0.2.20 版结构一致，仅 registry 新版本差异）；② `frontend/pnpm-workspace.yaml` 同步补 `overrides:` 段（与 `package.json` 逐项一致），使 pnpm 9 与 pnpm 12 都读到同一份覆盖配置，防止本机再次 `pnpm install` 洗掉 lockfile 的 overrides。`package.json` 不改（保留给 pnpm 9 与上游同步口径）。
- **改动文件**：`frontend/pnpm-lock.yaml`、`frontend/pnpm-workspace.yaml`、`CHANGELOG-SO.md`（本条）。均为构建配置，无业务代码。
- **合并影响**：无上游同步冲突面（上游 `pnpm-workspace.yaml` 为空文件、本就是二开新增；lockfile 同步时按「采用上游版」口径覆盖即可，但**覆盖后必须用 pnpm 9 重生成**，见 286 行同步记录的改进点）。不触碰 `UPSTREAM-BASE` / `PORTER-BASE`。
- **验证**：模拟根 `Dockerfile` 全顺序（干净目录 → `pnpm@9.15.9 install --frozen-lockfile` → `pnpm run build`）全通过；另在隔离目录用 pnpm 9 全量 `install --frozen-lockfile` 通过。本机 pnpm 12 重装 `node_modules` 后 `typecheck` / `test:run`（297 文件 / 2329 用例）/ `build` 全绿，且 lockfile 的 overrides 段保持完好（双写防复发验证）；pnpm 9 与 pnpm 12 的 `--frozen-lockfile` 交叉校验均通过。
- **约定补充**：今后改任一 overrides 项必须同时改 `package.json` 与 `pnpm-workspace.yaml`，并用 **pnpm 9** 重生成 lockfile；本机如需 `pnpm install` 后应复核 `pnpm-lock.yaml` 的 `overrides:` 段是否仍在。

### 同步 v0.2.21（2026-11-11）

- **区间与规模**：`6b4b37db → 530c5e3a`（83 提交 / 202 文件，+6702/-559）。二开侧：87 个未改文件直接覆盖、34 个上游新增复制、42 个三路自动合并、11 个冲突手工裁决（另同步删除上游移除的 `assets/partners/logos/lanox.jpg`）。版本号 `VERSION`→0.2.21（tag `v0.2.21` 一致）。
- **同步功能**：① **Claude Fable / Mythos 5.2 网关支持**（`pkg/claude/constants.go`、`effort_catalog.go`、`pkg/apicompat` 请求/响应转换、定价兜底）；② **OpenAI/Codex 一批修复**（新增 `openai_page_context.go` 页面上下文规范化、Responses session seed 跨工具循环稳定、封存函数调用元数据、非法 UTF-8 请求体拒绝、cache-write TTL 计费与迁移 `250_openai_cache_write_ttl.sql`、WebSocket 首帧 TTFT、native image 错误状态）；③ **网关内存预算**按 cgroup 上限自动计算（新增 `pkg/sysutil/memory.go`，`os.Root` 限定挂载读取）；④ **调度/认证/限流修复**（刷新令牌原子轮换、账号 hydrate 失败释放 slot、容量降级匹配真实错误字段、Pro Argon 冷却隔离）；⑤ **Antigravity** 用量查询按钮（`force` 绕过缓存）与工具 functionResponses 连续性；⑥ **性能与运维**（API 密钥列表不再等待 30 天用量 + 迁移 `249_add_usage_logs_api_key_cost_index_notx.sql` 覆盖索引、审计任务入队即唤醒 worker、系统设置保存按钮底部吸附浮标）；⑦ **前端**：账号用量单元、KeysView 用量提示、UsageTable、UseKeyModal、AccountsView 刷新保留行等；⑧ 文档 `docs/OPENAI_IMAGES_CODEX.md`。
- **冲突裁决（11 文件）**：`account_usage_service.go` 采用上游 `cachedAntigravityUsage` 辅助函数（与二开内联 `force` 缓存语义等价，并由上游新测试覆盖）；`openai_gateway_service.go`/`openai_gateway_response_handling.go` 保留二开 `Credits` 字段并融入上游 `CacheCreation30m/12hInputTokens`；`openai_gateway_messages_chat_fallback.go` 保留二开「不重写 ReasoningEffort」语义，仅并入上游 GPT-6.1 Sol 清空 Temperature/TopP；`AccountUsageCell.vue` 融合上游 `data-testid="antigravity-active-query"` 与 `v-if="!loading"`（保留二开 flex 布局）；`SettingsView.vue`/zh `admin/settings.ts`/zh `dashboard.ts` 冲突块均为二开裁剪的支付页/可用渠道，保持二开现状；README 三语言冲突保留二开赞助商与图片文档裁剪。
- **渠道类型最小合入**：为保留的 `GroupsView` 分组定价，`components/admin/channel/types.ts` 与 zh `admin/channels.ts` 的 `cache_write_12h_price` 字段/i18n 增量照常合入（真渠道 UI `ChannelsView`/`IntervalRow`/`PricingEntryCard` 仍按裁剪跳过）。
- **测试适配**：上游新增 `TestCachedAntigravityUsage_*` 依赖 helper，随之引入；`AccountUsageCell.spec.ts` 上游「query button」用例数据改为二开 `buckets` 口径；`AccountsView.lite.spec.ts` 依赖 `@refresh` 事件，在 `AccountsView.vue` 补 `@refresh="handleManualRefresh"` 绑定（二开 UI 无刷新按钮，行为不变）。
- **跳过项**：支付链路（`internal/payment/*`、`payment_config_service.go`、`PaymentView.vue`、`providerConfig.ts`、`rechargeBonus.ts`、支付 provider 测试）永久不同步；渠道 UI（`ChannelsView.vue`、`IntervalRow.vue`、`PricingEntryCard.vue`、`api/(admin/)channels.ts`、`SupportedModelChip.vue`）按裁剪跳过；模型广场（`api/modelPlaza.ts`、`PlazaModelPricingTable.vue`）；`utils/pricing.ts` 与 en 语言包（已删）。
- **验证**：后端 `go build ./...`、`go vet ./...`、`go vet -tags=unit ./...` 全通过；`go test -tags=unit ./...` 失败 6 项与基线完全一致（Gemini thinking ×2 子例 / 复合分组族 ×2 / antigravity 目录跳过 / effort 透传 / prompt-audit 路由覆盖），无新增失败。前端 `pnpm typecheck`、`pnpm test:run`（297 文件 / 2343 用例）、`pnpm build` 全通过。版本号：`VERSION`=0.2.21 == 上游 `VERSION@530c5e3a`，tag `v0.2.21`。支付链路与 en 语言包零变更（`git status` 复核）。


### 部署脚本去掉交互确认（2026-11-11）

- **变更**：`deploy/build-and-deploy-nas.sh` 移除「确认执行？[y/N]」交互提示（原第 4 步），执行即自动走完整个流程；部署前 `pg_dump` 数据库备份本就默认开启，现在随流程自动执行、不再有任何确认环节（仅 `SKIP_DB_BACKUP=1` 可显式跳过）。`-y/--yes` 保留为兼容空参数。
- **改动文件**：`deploy/build-and-deploy-nas.sh`（头部用法说明同步更新）、`CHANGELOG-SO.md`（本条）。
- **合并影响**：无（`deploy/` 为二开部署脚本，不触碰 `UPSTREAM-BASE` / `PORTER-BASE`）。
- **验证**：`bash -n deploy/build-and-deploy-nas.sh` 语法检查通过。

### 移植源切换：WorkBuddy 改以 workbuddy2api-panel 为准（2026-11-17）

- **变更**：`.claude/skills/sub2api-porter-sync/SKILL.md` 将 WorkBuddy 权威移植源由 `workbuddy2api-hub`（Python）改为 `workbuddy2api-panel`（Go，本机 `D:\workbuddy2api-panel`，`misakano7545/workbuddy2api-panel`）。同步重写固定路径表、模块映射（panel 的 `internal/upstream/*`、`internal/pool/`、`internal/scheduler/`、`internal/panel/`）与检测步骤示例，删除「hub 克隆不存在」的询问分支，补充「panel 历史重发布、旧哈希不可用」「提交由 AI agent 辅助、须读实际代码」两条评估约束。
- **基准**：`PORTER-BASE` workbuddy 行由 `workbuddy2api-hub 6e1e548` 改为 `workbuddy2api-panel a19c78f`（2026-11-06，按时间对齐上次 workbuddy 评估时点；两仓库无共同祖先，无法按提交映射）。panel 克隆历史于 2026-09-12 重新发布（根提交 `fd64aed`），CHANGELOG 中历史哈希（`73a7560e`、`535e19e`、`6e1e548`）在本克隆不存在，只能按「提交日期 + 标题」定位。
- **待办**：panel `a19c78f..27b7458` 共 177 个提交尚未按本 skill 评估，下次运行 `sub2api-porter-sync` 从该区间开始，不得视为已评估。
- **改动文件**：`.claude/skills/sub2api-porter-sync/SKILL.md`（未纳入版本控制）、`CLAUDE.md`（外部仓库说明同步）、`PORTER-BASE`、`CHANGELOG-SO.md`（本条）。
- **合并影响**：无（流程文档与移植基准；不触碰 `UPSTREAM-BASE`，无上游代码冲突面）。
- **验证**：skill 内已无 `workbuddy2api-hub` 作为源路径的残留（仅保留历史说明）；`PORTER-BASE` 仓库名与本地目录 basename 一致，可被 skill 的 `awk` 反查命中。

### 平台移植源同步：qoder / trae 全量 + WorkBuddy panel 高优先级项（2026-11-17）

- **区间与规模**：qoder2api-hub `70ba58b → f0b41e4`（36 提交 / 28 非合并）；trae-workbuddy-switch `dfa5bf7 → febc1b7`（84 提交 / 81 非合并）；WorkBuddy 权威源已按同日 skill/CLAUDE.md 变更切到 `workbuddy2api-panel`，本轮评估 `a19c78f` 后的高优先级子集（panel HEAD `27b7458`，区间共 177 提交，其余大项未评估）。用户本轮全程选择全部建议项。
- **qoder 已移植**：
  - **工具调用 index 归一化（`fa44d07` #157）**：`pkg/qoder/sse.go` 新增 `NormalizeToolCallIndices`（按 id 认领槽位、无主 index 分配稳定槽位号），在 `RecoverLeakedToolCalls` 与 `AggregateStream` 前置；修上游并行调用全部复用 index=0 时被合并/参数串坏。
  - **Kimi 原生工具与结构化历史（`2fad95d` #153）**：`baseprompt.go` 新增 `IsKimiModel`（kmodel/kmodel_latest/显示名）、`kimiToolProtocolPrompt`（字节级取自源文件）、`applyKimiToolProtocol`（仅带工具且非 `tool_choice=none` 时追加）；Kimi 默认走结构化工具历史。
  - **工具回声 fail-closed 与伪调用守卫（`4be8084`）**：截断的泄漏回声在声明了 tools 时不再静默按成功收尾（流式与非流式都返回错误）；新增正文具名伪调用守卫 `GuardUnstructuredToolEcho` / `containsNamedToolEcho`（排除代码围栏示例）；结构化历史默认出口改为 cn+intl，新增 `QD_STRUCTURED_TOOL_HISTORY_REALMS` 可缩小范围。
  - **Anthropic 工具参数规范化（`a858922` #143）**：`apicompat` 新增 `normalizeAnthropicHistoryToolInput`（解开历史里多编码一层的 input）与 `normalizeAnthropicToolUseInput`（非对象参数包成 `{"value": ...}`）；流式「对象形态 arguments 缓冲到块尾」未移植（本侧 CC 上游均发字符串增量）。
  - **刷新 415 form 回退（`e8e5300` #182）**：`pkg/qoder/oauth.go` 先按上次成功编码发；仅 415 才在 JSON/form 间重试一次并记住成功编码；401/403/400 不重试。
  - **QoDuck 目录条目（`c9d4ccb`）**：`assets/qoder_catalog_{cn,intl}.json` 补 `lmodel`（QoDuck，免费 app-only）。
  - **企业 VPC 专属域（`5599074`+`0b0885f`+`b29401d`）**：`pkg/qoder/realms.go` 新增 `ParseVPCEndpoint` / `RealmConfigFor` / `GatewayCandidatesFor` / `OpenAPICandidatesFor`；支持 `QD_CN_VPC_ENDPOINT` 部署级与账号 `vpc_endpoint` 覆盖；VPC 实例只用专属域、不回退公网。`Credentials` 增 `vpc_endpoint`；OAuth login-start/PAT 导入、token 刷新、模型目录、chat 端点全部接线；`QoderForm.vue` 增国内版 VPC 输入。
  - **Anthropic thinking 开关（`9a27ae2` #146）**：`apicompat` 新增 `ANTHROPIC_THINKING_MODE=off|empty`（默认 empty，保持既有空签名 thinking 输出；off 不输出 thinking 块）。源为按 Key/面板粒度，本侧无对应粒度，改为进程级环境变量。
  - **改动文件**：`pkg/qoder/{sse,baseprompt,oauth,models,accounts,realms}.go`、`pkg/qoder/assets/qoder_catalog_{cn,intl}.json`、`pkg/apicompat/{chatcompletions_anthropic_bridge,responses_to_anthropic}.go`、`service/{openai_gateway_qoder,qoder_oauth_service,openai_models_list,openai_upstream_client_error,openai_gateway_upstream_errors}.go`、`handler/admin/qoder_oauth_handler.go`、`frontend/src/{api/admin/platformOps.ts,components/account/forms/QoderForm.vue,i18n/locales/zh/admin/accounts.ts}`。
- **trae 已移植**：
  - **工具调用跨帧累加（`ce0a2c4` #172）**：`pkg/trae/sse.go` 新增 `toolCallAccumulator`（按上游 index/id 定位槽位、name/arguments 去重累加、缺 id 补 `call_N`、流式稳定 index）；流式与非流式都改用累加器；有工具调用时 `finish_reason` 归一为 `tool_calls`。
  - **过期包不计入可用余额（`0f0bf61`）**：`BuildEntitlements` 只累加未过期包，新增 `ExpiredRemaining` 单独报告。
  - **临期窗口 14→7 天（`25f5982`）**：`pkg/trae/usage.go` 窗口与 `trae_ops_service.go` 文案常量统一为 7 天（推翻 2026-11-04 的 14 天裁定，源已再次变更）。
  - **上下文超限翻译（`6f21bfe`）**：`isOpenAIContextWindowError` 识别 `prompt is too long`；`writeOpenAIUpstreamClientError` 给客户端可识别的 `context window exceeded:` 前缀；`/v1/models` 客户端条目上下文窗口只降不升，上限 900k。
  - **改动文件**：`pkg/trae/{sse,usage}.go`、`service/{trae_ops_service,openai_gateway_upstream_errors,openai_upstream_client_error,openai_models_list}.go`。
- **WorkBuddy panel 已移植（高优先级子集）**：
  - **指纹版本（`2fa6876`）**：`pkg/workbuddy/headers.go` CN 桌面 5.11.3 / global 5.9.4；CLI 按 realm 2.176.0 / 2.165.0。`/v2/report` 桌面 commit/releaseDate 三元组本侧未实现（当前 report 事件不带这两个字段），记为不适用。
  - **sanitize 注入身份指令收窄（`767056c`）**：新增 `scrubInjectedIdentity` / `scrubInjectedIdentityContent`，只清洗 assistant 正文与 reasoning_content/reasoning；system/user 的用户人设不改；带点身份名与 `<model_identity>` 标签处理按源实现。
  - **工具结果倒置重排（`27b7458`）**：`pkg/workbuddy/tool_pairing.go` 新增 `reorderEarlyToolResults`，`RepairToolPairing` 改为 reorderEarly→merge→repack→cleanup，修 11148 `tool_call_sequence_broken`。
  - **签到回执语义（`03a50ce`）**：`client.go` 拆出 `doJSONRaw`；`billing.go` 新增 `billingMeterRaw` / `checkinReceiptError`，code=0 的失败回执返回错误，已签到回执仍可被 `IsAlreadyCheckin` 识别；`errors.go` 新增失败硬词表，失败硬词压过 already（10001 不再无条件当已签到）。
- **评估未移植（原因，勿重复评估）**：
  - qoder：`f0b41e4` 排队观测/诊断端点（panel 诊断专属；本侧已有 queued 分类与冷却换号）；`53fb020` agents 模板/routing 配置缓存（本侧无 agents 面板/routing workbench）；credits 展示/额度单元格（本侧 quota composition 已覆盖）；PID1/Windows 绑定/更新器/chunked body/UI。
  - trae：`0a4176a` 会话身份 v2（system+首条 user 指纹，与本侧「排除每轮变化的 system」设计冲突；本侧无历史裁剪）；`0959e76` 签到 10:05/窗口补跑（本侧调度器仅整点，10/21 点两轮且 21 点兜底）；`796c596`/`9b9380f` 暂时性刷新不误标（本侧仅 SessionDead 落错误）；`7b458c9` 上游模型清单（`pkg/trae/catalog.go` 已直连 batch_get_detail_param）；`995e575` 1.5MB 裁剪（本侧不裁剪历史）；`7b4208d`/`1e67583`/`0786010` Responses 状态隔离（本侧无 Response 状态存储）；`febc1b7` 统一网关斜杠 ID（本侧模型 ID 自带目录）；`9b6d1f0` 按模型默认思考档、`3c9f93d` `/v1/models` 思考档位（可选增强，用户本轮未选）；桌面端风控/记忆迁移/会话副本/UI/CI。
  - WorkBuddy：panel `a19c78f..27b7458` 其余约 170 提交（安全续写/救援体系、预算与免费号模型分级、媒体/3D/MCP、面板 UI/auth/proxy 等）本轮未评估/未选，**不得视为已评估**；`PORTER-BASE` workbuddy 行保持 `a19c78f`。
- **验证**：本机 `go` / `docker` / `pnpm` 均不可用，**未执行编译、gofmt、后端单测与前端 typecheck/vitest**，仅做静态审阅。建议在有工具链的环境执行：
  - `cd backend && gofmt -l internal/pkg/qoder internal/pkg/trae internal/pkg/workbuddy internal/pkg/apicompat internal/service internal/handler`
  - `go build ./... && go vet ./...`
  - 测试前临时移走 `backend/config.yaml`、`backend/data/config.yaml`；`go test -tags=unit ./internal/pkg/qoder/... ./internal/pkg/trae/... ./internal/pkg/workbuddy/... ./internal/pkg/apicompat/... ./internal/service/...`；全量 `go test -tags=unit ./...` 与 6 个存量失败基线对比。
  - `cd frontend && pnpm typecheck && pnpm exec vitest run src/components/account/forms/__tests__/platformForms.spec.ts`。
- **合并影响**：`pkg/apicompat/*`、`service/openai_models_list.go`、`service/openai_upstream_client_error.go`、`service/openai_gateway_upstream_errors.go` 属上游同步共面文件，后续上游同步需按二开语义手工合并；qoder/trae/workbuddy 其余文件为纯二开。**不触碰 `UPSTREAM-BASE` / `VERSION`**。
- **PORTER-BASE**：qoder → `f0b41e4`，trae → `febc1b7`（两项本轮建议项均有明确结论）；workbuddy 保持 `a19c78f`（panel 区间仍有大量未评估项）。

### 同步 v0.2.22（2026-11-17）

- **区间与规模**：`530c5e3a → 15871f90`（186 提交 / 107 非合并 / 339 文件，+20705/-1634），v0.2.21 → v0.2.22。二开侧：151 个未改文件直接覆盖、71 个上游新增复制（另 2 个新增跳过）、80 个三路自动合并、16 个冲突手工裁决、5 个上游修改文件跳过、14 个二开已裁剪文件跳过。版本号 `VERSION`→0.2.22（tag `v0.2.22` 一致）。
- **同步功能（按主题）**：
  - **分组强制 OpenAI Ultra Fast**：`groups.force_openai_ultrafast` 新字段（迁移 `251_group_force_openai_ultrafast.sql`、ent schema/生成代码、DTO/handler/service、`GroupsView.vue` 创建/编辑开关与 `BulkEditAccountModal.vue`、zh 文案与类型）；按模型门控（仅 GPT-6 Astra / GPT-6.1 Sol），Ultrafast > Fast > 客户端，Ultrafast 按标准价 6 倍计费且不受「免费 Fast」影响。
  - **OpenAI 透传按模型筛选**：账号 `extra.openai_passthrough_model_filter`（调度快照投影、`IsModelSupportedByAccount`、`/v1/models`、账号表单/批量编辑与 i18n）。
  - **Vertex 多区域**：新增 `vertex_locations.go`（`us`/`eu` 区域 host、按请求区域 +10% 上游价倍数、模型级区域覆盖）；URL 构造统一走 `buildVertexGeminiAccountURL(..., cachedContentLocation)`；账号统计与 token 计费按实际区域加价。
  - **Gemini**：Gemini 4 Carbon 目录/兜底价/别名（`normalizeGeminiThinkingTierAlias`）、Gemini 4 Hydrogen 计费、兼容入口 `response_format` 结构化输出、并行同名工具调用跨帧边界、原生请求按稳定前缀生成粘性会话哈希（`gemini_session.go`）、Vertex 账号模型目录按区域。
  - **计费与用量**：同一用户扣费按用户通道合并结算（`usage_billing_lane.go` + `usage_billing_batch_repo.go` + wire）；DeepSeek `override_file` 显式非负单价不再叠加高峰倍率（`tokenPriceOverrides`）；分组日汇总重建与保留期清理不再阻塞 `usage_logs` 写入（迁移 `251_group_usage_rollup_nonblocking_build.sql`）；按账户筛选时模型/端点分布用实际扣费；Chat 回退路径保留响应模型观测；硅基流动 Qwen3-Embedding/Reranker 兜底价；高峰倍率测试钟修复；分组费率/RPM 同步事务化。
  - **流式与网关**：上游 TCP keepalive 探测死连接（`http_upstream_keepalive*.go`）；keepalive 事件先转发、keepalive 后错误按 SSE 返回、Messages 桥首个内容前换号、compat 空流提交后释放粘性账号、客户端断开后仍写回粘性会话；Anthropic 原生端点/模型观测；`count_tokens` 透传去重头；保留 `metadata.user_id`；composite 只在 `message_start` 改写公开模型名并恢复响应模型；Anthropic 兼容路由的 effort 策略；首次输出内存回退；OpenAI tool schema Python 正则清洗、`patternProperties` 删除后放宽封闭对象、Codex additional_tools ID 保留、fork prompt cache key、密封调用内容保留。
  - **调度/限流/认证**：调度快照按 bucket 合并加载、冷却账号保留在快照成员、`openai_capabilities` 与透传筛选进投影；Claude 并发令牌轮换后恢复请求、Fable `7d_oi` 窗口恢复解除模型限制、weekly usage 跨 5h 滚动保留、Anthropic quota 窗口起点对齐、Kimi 新月度配额解析；登录邮箱身份回填失败不再 panic。
  - **各平台修复**：Antigravity Carbon thinking、schema items 类型匹配、流式思考与首字超时锚点、工具 schema 清洗；Bedrock 按错误语义分类并保留流中断 usage、渠道 Bedrock 兼容开关按布尔持久化；Grok server-side search 调用不入 tool calls、媒体错误消息与顶层错误解析隔离；websearch 账号模式覆盖全部渠道平台。
  - **安全/Web**：settings 注入 HTML 禁止缓存、模型白名单拒绝通配符、并发等待 ping 可设 0 禁用、请求体热点路径零拷贝与 `json_escape_probe`。
- **冲突裁决（16 文件）**：
  - `backend/Dockerfile`、`deploy/Dockerfile`：取上游 Go 1.27.4。
  - `cmd/server/wire_gen.go`：保留二开裁剪（不引入 `geminiCachedContentService`），并入 `usageBillingLaneCombiner` 与 `usageRepository` 通道装配。
  - `ent/schema/group.go` 及生成代码：`mutation.go` 字段容量 68（二开无 `i18n` + 上游 `force_openai_ultrafast`）；`ent/runtime/runtime.go` 14 处描述符索引按「先去 `i18n`、再插 ultrafast」重算，`force_openai_ultrafast=50`、`free_openai_fast=51`，后续顺延。
  - `handler/gemini_v1beta_handler.go`：采用上游稳定前缀内容哈希 `GenerateGeminiContentSessionHash`，保持二开无 `cachedContentBinding`。
  - `repository/scheduler_cache.go`：保留二开删除的显式缓存投影键，并入上游 `openai_capabilities`。
  - `service/account_test_service.go`：服务账号请求改用上游 `setVertexGeminiAuth` + 5 参 URL（修正合并后残留的 `accessToken` 引用）。
  - `service/gateway_service.go`：`ForwardResult` 增加 `UpstreamPriceMultiplier`/`upstreamPriceFactor`，保持无 `CacheStorageTokenHours`。
  - `service/gateway_usage_billing.go`：账号统计成本叠加 `upstreamPriceFactor`，新增 `upstreamPriceFactorApplies`/`applyUpstreamPriceFactor`；保持二开删除的显式缓存存储计费族。
  - `service/upstream_models.go`：保持二开删除 `buildVertexPublisherModelsURL`（随显式缓存裁剪）。
  - `service/vertex_service_account_test.go`：采用上游测试（5 参 URL、API Key 认证断言）。
  - `deploy/config.example.yaml`：保持删除显式缓存说明，并入 DeepSeek override 定价注释。
  - `AccountUsageCell.vue`：不引入 `isOfficialGeminiBaseUrl`（显式缓存裁剪），并入 `formatDateTime` 与额度观测新鲜度 UI。
  - `EndpointDistributionChart.vue`：合并二开 `showCost` 与上游 `showAccountCost`（表头/单元格/colspan/默认值）。
  - `UsageView.vue`：保持二开已移除的图表区（上游仅图表参数变化）。
  - `README*.md`：上游仅新增赞助商 DataEyesAI，跳过。
- **测试适配（裁剪模块引用）**：`gemini_messages_compat_service.go` 删除 `geminiCachedContentLocationInBody` 调用（区域固定取模型默认位置）；`gemini_carbon_pricing_test.go` 删除 `CacheStoragePricePerTokenHour` 断言；`vertex_locations_test.go` 删除 3 个依赖显式缓存/`buildVertexPublisherModelsURL` 的用例；`groupsOpenAIFast.spec.ts` 的 `[zh, en]` 收敛为 `[zh]`（en 语言包永久裁剪）。新增 `ChannelsView.bedrockCompat.spec.ts` 因视图已裁剪整体跳过。
- **跳过项**：`.github/workflows/*`（3，二开无 CI 目录）；`backend/internal/payment/provider/alipay{,_test}.go`（支付链路永久不同步）；README×3 与 `assets/partners/logos/DataEyesAI.png`（赞助商）；`ChannelsView.bedrockCompat.spec.ts`（渠道管理视图已裁剪）；14 个 `MISSING_IN_SO`（Gemini 显式缓存/模型端点系列、en 语言包、ChannelsView）。
- **依赖/构建链**：Go 1.27.3→1.27.4（`backend/go.mod`、根/`backend`/`deploy` Dockerfile、`deploy/docker-compose.so-dev.yml`；`github.com/google/jsonschema-go` 转为直接依赖）。`package.json`/`pnpm-lock.yaml` 本区间上游无变更，无需重生成。
- **验证**：本机无 `go` / `docker` / `pnpm`，**未执行编译、gofmt、后端单测与前端 typecheck/vitest/build**。已做静态核查：无冲突标记残留、无二开已裁符号引用（`GeminiCachedContent*`/`GeminiExplicitCache*`/`isOfficialGeminiBaseUrl`/`ProxySelector`/en 语言包）、`git diff --check` 无空白错误、变更文件无重复函数、ent 索引与字段容量手工复核。建议在有工具链环境执行：
  - `cd backend && gofmt -l internal cmd ent | head`；`go build ./... && go vet ./... && go vet -tags=unit ./...`
  - 测试前临时移走 `backend/config.yaml`、`backend/data/config.yaml`；`go test -tags=unit ./...` 与 6 项存量失败基线对比。
  - `cd frontend && pnpm typecheck && pnpm test:run && pnpm build`。
- **版本号一致性**：`git describe --tags --abbrev=0 15871f90` → `v0.2.22`；`backend/cmd/server/VERSION`=0.2.22；`UPSTREAM-BASE`=`15871f904056f548dd874a1d069ffc6cdf777b78`。
- **合并影响**：支付链路与 en 语言包零变更（`git status` 复核）；Gemini 显式缓存、渠道管理视图继续维持二开裁剪。新迁移占用 251，二开迁移下次从 252 起。

### 修复 v0.2.22 同步遗留的编译错误（2026-11-17）

- **问题**：v0.2.22 同步时本机无 Go 工具链，未执行编译验证，首次镜像构建暴露两处编译错误：
  - `internal/pkg/qoder/realms.go:328`：`OpenAPICandidatesFor` 签名为 `([]string, error)`，末尾分支误写为 `return out`，补回 `nil`。
  - `cmd/server/wire_gen.go:383`：`provideCleanup` 调用漏传 `traeOpsService`（函数体 625 行的 `traeOps.Stop()` 已在使用），同时触发 `declared and not used` 与「参数不足」两条错误。
- **改动文件**：`backend/internal/pkg/qoder/realms.go`、`backend/cmd/server/wire_gen.go`、`CHANGELOG-SO.md`（本条）。
- **验证**：`docker build --target backend-builder`（复用 BuildKit 的 `sub2api-gomod` / `sub2api-gobuild` 缓存）编译通过；完整镜像 `sub2api:0.2.22-so-20261117` 构建成功，经 `deploy/build-and-deploy-nas.sh` 部署到 NAS，容器 `Up (healthy)`、`/health` 返回 200。
- **合并影响**：无（纯编译修复，不改变上游语义；`wire_gen.go` 继续维持二开手工接线）。

### 同步 v0.2.23（2026-11-18）

- **区间与规模**：`15871f90 → 2af3f1e1`（32 提交 / 20 非合并 / 90 文件，+5251/-453），v0.2.22 → v0.2.23。二开侧：39 个未改文件直接覆盖、16 个上游新增复制、27 个三路自动合并、2 个冲突手工裁决、6 个跳过（en 语言包 ×2、已裁剪渠道管理前端 ×4）。版本号 `VERSION`→0.2.23（tag `v0.2.23` 一致）。
- **同步功能（按主题）**：
  - **GPT-6.2 Sol / Astra**：新增 `openai.GPT62ModelID` 与两份 Codex 目录（`codex_gpt62_sol.json` / `codex_gpt62_astra.json`）；模型元数据、Codex 目录服务、模型别名、转换层、`useModelWhitelist`、`UseKeyModal` 的 OpenCode 配置全部接入。计费新增 Astra / Sol fallback 价（Sol 复用 6.1 Sol 静态价）、Fast 2 倍、缓存写溢价、`pricing_service` 兜底条目与 `model_prices_and_context_window.json` 条目。
  - **Ultra Fast 计费与渠道倍率**：Ultrafast 倍率按请求时点计算（GPT-6.2 在 2027-03-01 08:00 UTC 前 3 倍，之后 6 倍，`openAIUltrafastMultiplier`）；分组强制 Ultrafast 对 GPT-6.2 先校验 Pro 套餐 / 账号目录档位（`gpt62PlanIncludesUltrafast`），不可用时按强制 Fast 或跟随客户端；渠道新增 `ultrafast_multiplier`（迁移 `252_channel_pricing_ultrafast_multiplier.sql`、`ChannelModelPricing` / repository / handler / 校验、前端 `PricingEntryCard` / `types` / zh 文案）。
  - **缓存用量计费对齐**：apicompat `AnthropicUsage` 增加 `CacheCreationInputTokensPresent` / `CacheReadInputTokensPresent`，跨 Anthropic ⇄ Responses ⇄ CC 转换保留显式零值；`ClaudeUsage` / `OpenAIUsage` 新增 `billingUsageTokens()` 统一互斥桶组装，聚合缓存写缺失时用 5m+1h 明细回填，新增 `allocateCacheCreationBillingTokens` 把聚合余量分配给 5m；账号统计与 OpenAI `usage_logs` 同步 5m/1h 明细。
  - **流式性能与空闲超时**：原生 CC 流式透传改为「等待上游读取前 flush」（`flushBeforeUpstreamRead`，合并多行写）；DeepSeek reasoning 占位与 Ollama Cloud reasoning 回填改为单遍 `setStringFieldInJSONObjects`（`openai_json_object_field_edit.go`）；流空闲计时改用 `time.Timer` 按最后读取时刻重置；OpenAI 首批输出前空闲超时进入 failover（计入 first-output 预算，`SafeToFailoverAfterWrite`）；Anthropic 流终态 usage 优先取 event 级 usage。
  - **计费入口统一**：`CalculateCostUnified` 成为统一计费入口（替代 `calculateCostWithServiceTierPolicy`），`getModelPricingAt` 支持按计费时点取价。
- **冲突裁决（2 文件）**：
  - `service/gateway_usage_billing.go`：账号统计 token 改用上游 `result.Usage.billingUsageTokens()`，但保留二开删除的显式缓存存储计费族——`cost.TotalCost` 不扣 `cacheStorageCost`，不引入 `cacheStoragePrice` / `isCacheStorageOnlyResult` / `CacheStoragePriced` / `addCacheStorageToAccountStatsCost`（与 v0.2.22 裁决一致）。
  - `service/openai_gateway_service.go`：保留二开 `Credits` 字段，并入上游私有字段 `cacheCreationInputTokensPresent`。
- **跳过项**：`frontend/src/i18n/locales/en/admin/{channels,overview}.ts`（en 语言包永久跳过）；`views/admin/ChannelsView.vue`、`views/admin/__tests__/ChannelsView.ultrafastPricing.spec.ts`、`api/admin/channels.ts`、`components/admin/channel/__tests__/PricingEntryCard.modelDefaultPrice.spec.ts`（渠道管理前端已永久裁剪）。
- **测试适配**：上游 Go 测试原样并入；`PricingEntryCard.timePricing.spec.ts` 保留二开已移除的默认价自动填充用例删除状态，仅并入 `ultrafast_multiplier` 字段。`views/admin/__tests__/groupsOpenAIFast.spec.ts`（二开本地 spec）原先断言 `ultrafastHint` 含 `service_tier=ultrafast`，上游 v0.2.23 重写该文案后不再包含此片段，已按新行为改为断言含 `Ultra Fast`，并保留 `GPT-6 Astra` / `GPT-6.1 Sol` 模型名断言。
- **依赖/构建链**：`backend/go.mod`、`frontend/package.json` / `pnpm-lock.yaml` 在本区间净零变更（Go 1.27.4 与 follow-redirects 补丁已在 v0.2.22 基准内），无需 `go mod tidy` 或重生成 lock。新迁移占用 252，二开迁移下次从 **253** 起。
- **验证**：
  - 后端：`docker run golang:1.27.4-alpine go build ./...` 通过；`go vet ./...` 通过。
  - `go test -tags=unit ./...`：10 个失败，均与 HEAD 基线一致。除 CLAUDE.md 原列的 6 项（Gemini thinking variant、复合分组族 ×2、antigravity 目录跳过、effort 透传、prompt-audit 路由覆盖）外，另 4 项（`TestNormalizeAnthropicHistoryToolInputUnwrapsDoubleEncoding`、qoder `TestRealmFallbacks` / `TestRecoverTruncatedEchoSwallowed`、`TestSchedulerMetadataKeepsOpenAICapabilities`）已用 HEAD 干净 worktree 逐项复跑确认为存量失败。本次同步未引入新失败；CLAUDE.md 的测试基线已由 6 项校正为实际 10 项。
  - 前端：`pnpm install --frozen-lockfile`（pnpm 9.15.9）成功；`pnpm typecheck` 通过；`pnpm test:run` 298 文件 / 2371 用例全部通过（修正 `groupsOpenAIFast.spec.ts` 的过期断言后）；`pnpm build` 通过，产物写入 `backend/internal/web/dist/`。
  - 版本号一致性：`git describe --tags 2af3f1e1` → `v0.2.23`；`backend/cmd/server/VERSION`=0.2.23；`UPSTREAM-BASE`=`2af3f1e15c98c893a89255a037f551046883b8c0`。
- **合并影响**：支付链路与 en 语言包零变更（`git status` 复核）；显式缓存存储计费、渠道管理视图继续维持二开裁剪。

### Gemini / Antigravity 平台 Agent 接入修复（2026-11-18）

- **问题**：Gemini（AI Studio / Code Assist OAuth）与 Antigravity 两个平台在管理台「测试」时正常，但 Claude Code / Codex 等 agent 接入后报错。生产 `ops_error_logs` 定位到两类确定性失败：
  - `/v1/messages` + `gemini-4-hydrogen`：上游 400 `Invalid JSON payload received. Unknown name "propertyNames" at 'tools[0].function_declarations[...]'`；
  - `/v1/responses` + `gemini-4-hydrogen`：`account_platform=gemini` 被 `GatewayService.ForwardAsResponses` 当作 Anthropic 上游处理，实际打到 Gemini `/v1beta/models`，返回 404（Codex TUI 无法使用 Gemini 模型）。
- **根因与修复**：
  - **工具 Schema 清洗**（`service/gemini_messages_compat_service.go`）：原 `cleanToolSchemaNode` 保留 `propertyNames`、`additionalItems`、`unevaluated*`、`dependencies`、`dependentSchemas`、`if/then/else`、`not`、`contains`、`allOf`、`oneOf` 等 AI Studio Schema proto 不认识的 JSON Schema 关键字，agent 工具定义（Claude Code / MCP）一旦带 `propertyNames` 即整单 400。新增 `geminiUnsupportedSchemaKeywords` 黑名单在任意深度剔除；`allOf` 合并进父节点、`oneOf` 折叠为 `anyOf`、`prefixItems` 取首项降级为 `items`、`const`→`enum` 语义保留。
  - **Agent 多轮结构**（同上 + `pkg/antigravity/request_transformer.go`）：Claude 客户端常出现「tool_result 用户轮 + 下一条用户提示」连续同角色轮次；Gemini 两个入口现统一合并相邻同角色 contents、把 `functionResponse` 强制归入 user 轮、丢弃结尾只含 thinking 的 model 轮（对应 CLIProxyAPI 的 `EnsureGeminiBoundaryUserContent` / `DropGeminiTrailingThoughtOnlyContent` / `normalizeAntigravityGeminiFunctionResponseRoles` 语义）。Antigravity 端 `buildContents` 同步实现。
  - **`/v1/responses` 接入 Gemini 平台**（新增 `service/gemini_responses_compat_service.go` + `handler/gateway_handler_responses.go` 分支）：Responses → Anthropic（`apicompat.ResponsesToAnthropicRequest`）→ 复用 `GeminiMessagesCompatService.Forward` 的调度/重试/错误/用量/思考签名逻辑 → 用 `claudeToResponsesWriter` 包裹 `c.Writer`，把 Claude SSE/JSON 实时翻译回 Responses 事件（`AnthropicEventToResponsesEvents` + `FinalizeAnthropicResponsesStream`）。非流式 JSON 与错误体同样转换，保持计费 `ForwardResult` 不变。
- **测试适配**：`TestCleanToolSchema_TraversesRetainedSchemaValuedKeywords` 改为断言这些关键字被剔除；`TestCleanToolSchema_PreservesNestedOneOfCombinator` 改为断言 `oneOf`→`anyOf` 且分支 `const`→`enum` 语义保留；`TestConvertClaudeMessagesToGemini_DropsHistoryThinkingBlocks` 期望从 4 条 contents 改为 3 条（思考-only 轮被丢弃后相邻 user 轮合并）。新增 `gemini_tool_compat_test.go`、`gemini_responses_compat_test.go`、`pkg/antigravity/contents_normalization_test.go`。
- **验证**：
  - 对真实 AI Studio（账号 617 apikey）直连验证：带 `propertyNames` 的 Bash/Glob/Agent 工具请求清洗后上游 200；Claude Code 形状的多轮 tool_use/tool_result + 历史 thinking 请求 200；Responses→Anthropic→Gemini 链路（含 Codex `shell` 工具）200，上游返回原生 `thoughtSignature`。
  - `go build ./...`、`go vet ./internal/service/ ./internal/handler/ ./internal/pkg/antigravity/`、`gofmt` 通过；`go test -tags=unit ./...` 仅余 CLAUDE.md 记录的 10 项存量失败（另有 `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` 单跑失败，属时间敏感用例，与本改动无关）。
  - **Gemini 部署验证**（NAS，镜像 `sub2api:0.2.23-so-20261118-2`，容器 healthy）：`/v1/messages` + `propertyNames` 工具 → 200 `OK`；Claude Code 形状多轮（外部 thinking 签名 + tool_result 与后续用户提示合并）流式 → `tool_use Read` 正常；`/v1/responses` 纯文本 → 标准 Responses 事件链；`/v1/responses` + `shell` 工具 → `function_call` 参数与 `call_id` 完整；`usage_logs` Gemini 记录 `upstream_endpoint=/v1beta/models` 计费正常；`/v1/responses/input_tokens` 返回 404 走本地估算。最终镜像 `-4` 复测通过。
  - **Antigravity 实机验证**（账号 615 临时解除删除并加入分组 20，镜像 `-2` / `-3` / `-4`）：`/v1/messages` 多轮 agent（外部 thinking 签名 + tool_result 与后续提示合并）流式 → `tool_use Read` 参数正确；`/v1/responses` + `shell` 工具 → `function_call` 完整；`usage_logs` 两条记录 `upstream_endpoint=/v1internal:streamGenerateContent` 计费正常。验证后账号 615 保持启用并保留在分组 20（原 `deleted_at` 已清除），使 Antigravity 平台可直接使用；如需停用，在管理台移除分组或软删账号即可。
  - **实机暴露的两个 Antigravity 隐患（本轮一并修复）**：
    1. 客户端回放的**外部/截断 thought_signature** 会被上游以 `400 Invalid value at 'request.contents[..].part.thought_signature' (TYPE_BYTES)` 拒绝，靠签名整流器重试才成功（`ops_error_logs` 可见 `Recovered upstream error 400`）。新增 `sanitizeGeminiThoughtSignature`（`pkg/antigravity/request_transformer.go`）与 `isPlausibleGeminiThoughtSignature`（Gemini 原生路径）：非 base64 或长度 <40 的签名替换为 `skip_thought_signature_validator`，Gemini 目标一次成功，不再消耗一次上游重试。
    2. 请求路径 OAuth 刷新 8s 超时会把一次慢刷新变成 `502 Failed to get upstream access token`，并让账号临时不可调度 10 分钟。`antigravityRequestRefreshTimeout` 提到 30s，且 `context.DeadlineExceeded`/`Canceled` 不再标记临时不可调度（交给后台 refresher 重试）。
  - **Antigravity 账号侧说明**：`retrieveUserQuotaSummary` 会返回 `SUBSCRIPTION_REQUIRED / #3501`，但 `streamGenerateContent` 实测可用；此前的 429 是 5h 配额窗口耗尽，窗口重置后恢复。账号 615 已解除软删并加入分组 20，验证后保持启用状态。
- **合并影响**：涉及高冲突文件 `gemini_messages_compat_service.go`（与上游同函数冲突时以上述清洗语义为准）；`UPSTREAM-BASE` / `VERSION` 不推进。Antigravity 侧改动限于 `request_transformer.go` 的 contents/签名组装与 `antigravity_token_provider.go` 的超时策略，未改动上游身份注入。

### 平台移植源同步：workbuddy 协议硬化 + qoder 签到/官方搜索 + trae 缓存三态（2026-11-20）

- **区间与规模**：workbuddy2api-panel `a19c78f → 08cb9351`（218 提交，含 11-17 未评估批次）；qoder2api-hub `f0b41e4 → 25d05944`（10 提交）；trae-workbuddy-switch `febc1b7 → ebcaead1`（10 提交）。三仓库均 `pull --ff-only` 成功、工作区干净。
- **WorkBuddy 已移植**：
  - **X-Client-\* 身份头（`96265d7`/`a0b9f87`/`77ca3fe`）**：`headers.go` 新增 `injectClientIdentityHeaders`——国内版全链路发 `X-Client-Version`（跟随 clientVersion）/`X-Client-Platform: windows`/`X-Client-Product: WorkBuddy`，国际版一个不发（5.9.4 分发包无该注入机制）。挂 CommonHeaders（chat/refresh/v3-config）与 BillingHeaders（report/billing/签到）。X-Device-Token 仍只按账号配置注入。
  - **tools schema 深度硬化（`615dc75`/`d6301fe`/`abed839`/`733942a`）**：重写 `tool_schema.go` 的 `deepNormalizeSchema`——数据关键字（default/const/enum/example(s)）不递归；`dependentRequired`/`dependencies`/`dependentSchemas` 按容器处理（键是属性名）；null 仅非数据键剔除；数值/布尔/字符串关键字取值类型纠偏；顶层 `$ref` 按 `$defs` 展开（展开失败删键）；`pattern` 的 `\_`→`_`、`\uXXXX`→`\x{XXXX}`（译后仍编不过则丢弃该 pattern）；`required` 去重；容器子树只递归子值。sanitize 之外的语义字段不再被误改写。
  - **工具结果重排按最近声明配对（`e14a3d0`）**：`tool_pairing.go` 的 `reorderEarlyToolResults` 改成正序「已声明未应答」计数 + 就近配对，跨轮复用 `call_0` 的历史不再被整段挪位（第一轮调用落空导致 11148）。
  - **空 text 段补位 + reasoning 镜像（`f987bac`/`a798364`）**：新增 `empty_content.go`——`guardEmptyTextParts` 对 parts 里空/缺失/null 的 `type:"text"` 补一个空格（含「字符串 content 被上游嗅探成 parts」的形态），只补值不删段；`mirrorUpstreamReasoning` 把 Kimi/GLM/MiniMax/MiMo/Step/Space-Bunny 的 assistant `reasoning_content` 镜像进空 `reasoning`（不新建字段、不覆盖已有值）。
  - **错误分类收紧（`4830686`/`d3edea9`/`b35a8d0`）**：11134 供应商过载无论状态码归 `upstream_busy`（不再 5xx 熔断整池/换号），并新增 `ParseBodyRetryAfter` 读 `retry_policy.retry_after` 回填 `Retry-After`；11152（工具名不合规/重名）、11151（空 text 段）归请求级 `bad_params` fail-fast 400，不罚号不轮转；11103（Backend not supported）归模型级；11140 判据加入账号专属限制文书（`账号存在异常行为`/`unusual activity`），仍要求 code 11140。
  - **兜底 system 不再声明身份（`e4e6f5c`）**：global 域首条非 system 时注入的文本改为行为约定（`Follow the user's instructions and respond in the user's language.`），两处（pkg client + service 复刻）同步。
  - **内联思考标签（`771c887`）**：新增 `pkg/workbuddy/think_tag.go`（`<think:SUF>…</think:SUF>` 跨片拆分，仅在 content 以 `<think:` 开头时启用），在 `/v1/messages` CC 回退链（流式/非流式）对 workbuddy 账号把内容拆到 `reasoning_content`。
  - **官方语音端点（`923dbec`/`e1280ca`/`269b92e`/`08cb935`，issue #277）**：新增 `pkg/workbuddy/speech.go`（TTS `POST {chatBase}/v2/audio/speech` SSE PCM、ASR multipart `POST {chatBase}/agenttool/v1/asr`，ASR 的 Content-Type 在 ChatHeaders 之后重设 boundary、SSE error 帧识别、15053 归请求级）；新增 `POST /v1/audio/speech`（仅 WAV，PCM 24kHz 单声道封 RIFF）与 `POST /v1/audio/transcriptions`（3MB 入口上限），service 层 `workbuddy_speech_service.go` 复用账号轮转与 `applyWorkbuddyUpstreamErrorKind` 账号处置。**计费**：按次请求记账（无 token），未接入按字符/秒计价。
- **qoder 已移植**：
  - **签到判据 + 专属包过滤（trae `4a634d6` / PR #210）**：新增 `campaign_daily.go`（`campaignIsDailyCheckinAt`：积分类 + 窗口内 + 一次性促销词优先排除 + 每日词/24h 窗口判定）；`CampaignCheckin` 的 CLAIMED 分支只把每日活动计入 already，历史已领一次性体验包归 views；按活动 id 去重。`quota_composition.go` 的桶对 `expired`/状态含 EXPIRED·INACTIVE·DISABLED/已过到期时间的包标 `Available=false`，不再计入合计。
  - **官方 Web 搜索（`7c5ac53`/`09ee227` 的核心子集）**：新增 `pkg/qoder/web_tools.go`——CN 走 gateway 域、INTL 走 `center.qoder.sh`（`QD_INTL_WEB_TOOLS_ORIGIN` 可覆盖），外层 `{"payload","encodeVersion":"1"}` 经 `QoderEncode` 编码后按**编码 body** 做 COSY 签名；`WebSearch` 做域名过滤/去重/5 条上限/摘要裁剪，`WebFetch` 做 http(s)、无凭据、公网解析与最终地址复检；VPC 账号明确拒绝。新增 `RunQoderWebSearch`（账号轮转）与 `POST /v1/search/messages` 的 qoder 分派（与 workbuddy 搜索共用路由，响应同为 Anthropic `web_search_tool_result` 契约）。
- **trae 已移植**：
  - **缓存命中三态（`db4754a`）**：`pkg/trae/sse.go` 的 `TokenUsage.Cached` 改为 `*uint64`（nil=未观测、&0=实测未命中）；解析 `metadata.prompt_cache_pool_record`（缺失=未回报、null=明确无池、对象/数组=已挂池），`mergeTokenUsage` 按「无池时 0 不算可测 miss、非零优先、未观测沿用上一帧」合并；`ToOpenAI` 观测到就写 `prompt_tokens_details.cached_tokens`（含 0），未观测缺键。`Aggregate` 同步。
- **评估未移植（原因，勿重复评估）**：
  - workbuddy：**救援/续写体系（narration/toolonly/continue 约 40 提交、约 1500 行）本轮未实现**——属对 CC 流的行为层改写，需在共享管道逐段接入并配套回归，单轮风险过高，留待独立批次；3D 生成 + MCP（`557b3d5`，媒体系列 2026-11-04 裁定）；`$wbEncrypted` 桌面 auth 文件（本侧账号走 OAuth 落库）；10105 活跃会话/webagent（未移植该通道）；`pool`/`proxypool`/`panel`/`updater`/`logstore`/导入预览/模型规则/实时任务卡（面板与号池形态专属）；`/v1/tools/search|fetch` 别名（本侧 API 面不同）；会话粘性派生键（本侧无 pool sticky）；SSE 缓冲读空判据（`a98ab36`，本侧 scanner 语义不同）。
  - qoder：**Responses 托管工具（`web_search_call`/`open_page`、调用预算、来源引用）与外部搜索后端（SearXNG/Tavily/Brave/Serper/自定义）本轮未实现**——托管工具需要在 Responses 回退链上做多轮工具执行与事件生成，外部后端还需面板配置与密钥隔离，单轮风险过高；`adeadcb`/`deb48f8` 面板关停、`25d0594` 调度暂停（本侧 ops 调度自有）、`44b6f33` 流收尾/模型身份（Go 传输层与 `UpstreamModel` 已覆盖）、webFetch 端点（客户端已实现，端点待接）。
  - trae：`ebcaead` 会话管理与同步恢复、`83504e5` 到期分钟档、`80b6bc6` Windows 线程判活、`b741ae8` 开学季移除、`54c8e96` 设置页去重（桌面端专属）；`4a634d6` 已按 qoder 侧移植。
- **改动文件**：`pkg/workbuddy/{headers,errors,client,tool_schema,tool_pairing,empty_content,think_tag,speech}.go`、`pkg/qoder/{accounts,quota_composition,campaign_daily,web_tools}.go`、`pkg/trae/sse.go`、`pkg/apicompat/{types,chatcompletions_anthropic_bridge}.go`、`service/{openai_gateway_workbuddy,openai_gateway_messages_chat_fallback,openai_gateway_chat_completions_raw,openai_gateway_cc_pipeline,sse_frame_split,qoder_web_tools_service,workbuddy_speech_service,openai_gateway_workbuddy_thinktag}.go`、`handler/{gateway_qoder_search,gateway_workbuddy_speech}.go`、`server/routes/gateway.go`。测试：新增 `porter_sync_2026_11_20_test.go`、`campaign_daily_test.go`、`sse_frame_split_test.go`；更新 `tasks_test.go`（X-Client-Platform 按取值判 mp）、`payload_norm_test.go`（11134 归 upstream_busy）、`trae/sse_test.go`、`apicompat` 输出格式用例、`quota_composition_test.go`（到期时间改远期）。
- **验证**：`go build ./...`、`go vet ./...`、`gofmt` 通过；`go test -tags=unit ./internal/pkg/workbuddy/ ./internal/pkg/trae/ ./internal/pkg/qoder/ ./internal/pkg/apicompat/ ./internal/service/ ./internal/handler/ ./internal/server/` 全绿（qoder/apicompat 仅剩 CLAUDE.md 记录的存量失败）。全量 `go test -tags=unit ./...` 与 HEAD 基线对比：存量失败 10 项不变；`TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort` 在 HEAD 与本次均约 4/5 概率失败（时间敏感，存量）；`TestCommandCodeGatewayPassesThroughCatalogProtocols` 全量跑偶发、单跑通过（用例顺序污染）。本机无 pnpm，**前端未改动也未验证**。
- **合并影响**：`pkg/apicompat/types.go`、`chatcompletions_anthropic_bridge.go`、`service/openai_gateway_cc_pipeline.go`、`openai_gateway_chat_completions_raw.go`、`openai_gateway_messages_chat_fallback.go`、`server/routes/gateway.go` 属上游同步共面文件，后续上游同步需按本文语义手工合并（output_config.format 转发、SSE 粘帧拆分、messages 回退签名新增 account 参数）。其余为纯二开新增/修改。**不触碰 `UPSTREAM-BASE` / `VERSION`**。
- **PORTER-BASE**：trae → `ebcaead1`（本轮建议项均有结论）；qoder 保持 `f0b41e4`、workbuddy 保持 `a19c78f`（各有明确未完成的大项：qoder 托管工具/外部后端、workbuddy 救援体系）。

### 同步 v0.2.24（2026-11-20）

- **区间与规模**：`2af3f1e1 → 328db26f`（110 提交 / 190 文件，+11853/-610）。上游 `pull --ff-only` + `fetch --tags` 成功；`describe` → `v0.2.24`，与 `backend/cmd/server/VERSION`（0.2.24）一致。二开侧：86 文件直接覆盖、52 个上游新增文件并入、46 个本地改动文件三路合并（38 自动、8 冲突手工裁决）、6 个文件跳过。
- **同步功能（按主题）**：
  - **请求体安全**：新增 `pkg/requestmodel/conflict.go` + `server/middleware/request_field_conflict.go`，拒绝顶层与嵌套对象中键重复且取值冲突的 JSON 体（多次绕过审计后收口），multipart 拒绝 RFC 2231 扩展字段名；`routes/gateway.go` 挂载新中间件；`pkg/httputil/body_read_error.go`、`request_body_limit.go`、`request_body_read_log.go` 收敛读体错误分类。
  - **Web 搜索**：`pkg/websearch/exa.go` 新增 Exa 后端（配置/`websearch_config.go`/前端 SettingsView）； Responses→Anthropic 返回 `web_search_tool_result` 的来源（`apicompat/responses_web_search_sources.go`）。
  - **协议转换**：`apicompat/anthropic_tool_search.go`（ToolSearch → Codex OAuth 原生 `tool_search`）；tool_use.input 保证为 JSON 对象；保留 forced tools / 结果文本 / 保留名；容忍非字符串 `action.url`、跳过未完成的 web_search；`stop` 与 Kimi 动态工具定义不再在转换中丢失；DeepSeek Anthropic 历史带 thinking 块（`openai_gateway_deepseek_anthropic_thinking.go`）；Claude 5.5 thinking 适配（`service/claude55_thinking.go` + `pkg/claude/sampling.go`）；Gemini 尾部 part 拆分（见冲突裁决）。
  - **Responses 缓冲链路**：新增 `openai_gateway_buffered_hosted_items.go` 与配套测试，修复 buffered 模式下的 hosted items、服务端搜索、工具调用重建、空终态输出、流式与非流式一致性。
  - **OpenAI 网关**：上游请求超时纳入 failover；alpha/search 工具级 403 不再把 OAuth 账号打入惩罚；OAuth 凭据轮换防竞态与超时；VS Code IDE 上下文透传；按轮记录 Responses reasoning effort；图片 usage 单行 SSE；大请求体在 handler 之后不再被固定；Kimi 动态工具保留。性能：tool-schema 扫描短路、namespace 剥离不再整体复制、请求体单次 root walk 解析、图片意图预检（`image_generation_intent.go`）。
  - **Antigravity**：额度改按账号实际转发端点查询；裸 429 在重试循环结束后只冷却一个账号（`antigravity_bare429_scope.go`，请求级作用域）；保留下游 Gemini SSE 注释帧。
  - **账号/计费/调度**：typesafe 账号 model mapping 作用于 System One；新用户默认平台配额（`service/user_platform_quota_defaults.go`，wire 注入 `NewAdminService`）；账号测试对拒收 temperature 的模型省略该字段；批量编辑透传模型过滤需显式 opt-in；Claude OAuth / Setup Token 窗口成本估算；账号用量快照刷新；调度账号缓存不再展开分组；ops 平均 token 速率按请求时长加权；grok 接受五种编辑来源并按 xAI usage success count 计费。
  - **其他**：Anthropic 元数据长上下文模型曝光（`service/anthropic_models_list.go`）；Claude Code compaction 请求写 5m 缓存 TTL；前端模型 ID 复制按钮、频繁手动刷新 token 提示、Ultra Fast 文案。
- **冲突裁决（8 文件）**：
  - `pkg/apicompat/responses_to_anthropic.go`：二开 `parseAnthropicToolArguments` / `normalizeAnthropicHistoryToolInput` / `normalizeAnthropicToolUseInput`（qoder2api-hub 双重编码解开）全部保留，并并入上游新增的 `anthropicToolUseInput`（function_call 非对象参数补 `{}`、custom_tool_call 包 `{"input":...}`）；上游调用点改用其新函数，二开桥接文件（`chatcompletions_anthropic_bridge.go`）继续用二开 helper。
  - `server/routes/composite_platform_test.go`：两侧都在文件末尾追加测试，全部保留（二开多平台所有权用例 + 上游 Claude 5.5 thinking 适配用例）。
  - `service/antigravity_quota_fetcher.go`：保留二开语义——只查 `retrieveUserQuotaSummary`（`fetchAvailableModels` 的 quotaInfo 恒为 100%，不参与展示），**并入**上游“按账号实际转发端点查询”的修复，改用二开已有的 `RetrieveUserQuotaSummaryFromURL(ctx, baseURL, ...)`；`Raw` 由重新序列化 summary 填充（FromURL 不回传原始 JSON）。`buildUsageInfo`/`buildAntigravityQuotaGroups` 保持二开分组（Buckets）实现。
  - `service/antigravity_quota_fetcher_test.go`：import 合并 `strconv`（上游）+ `strings`（二开）；上游 `TestFetchQuota_QueriesForwardEndpoint` 适配二开语义——只断言 `retrieveUserQuotaSummary` 落在哪个端点（不再断言 `fetchAvailableModels`），分组断言改用 `Buckets[0]`（上游为 `Windows[0]`）。
  - `service/gemini_messages_compat_service.go`：保留二开 `normalizeGeminiContents`（同角色合并 / functionResponse 归 user / 丢弃尾部 thinking-only model 轮），把上游“尾部非 functionResponse part 拆成独立 user 轮”的修复**移植进该函数**（合并之后再拆），语义与上游一致。
  - `frontend/src/components/account/AccountUsageCell.vue`：import 采用上游新名 `buildAccountUsageRefreshKey`，保留二开 `AntigravityQuotaGroup` 类型引入。
  - `frontend/src/components/admin/account/AccountActionMenu.vue`：保留二开紧凑样式（`px-3.5 py-1.5 text-xs`），并入上游 `:title="t('admin.accounts.refreshTokenHint')"`。
  - `frontend/src/i18n/locales/zh/dashboard.ts`：`modelPlaza` 整块继续删除（模型广场前端已裁剪），不并入上游新增的 `table.copyModelId`。
- **测试适配（合并后）**：
  - `service/gateway_usage_billing.go`：二开把上游的 `recordUsageBillingModel` 内联在 `recordUsageCore` 中，上游新测试（`TestForwardSystemOneAppliesAccountModelMapping`）仍调用该方法——按上游原样**重新抽出** `recordUsageBillingModel` 方法并由 `recordUsageCore` 调用，行为等价（composite 别名 + 无价兜底顺序不变）。
  - `service/gemini_tool_compat_test.go`：`TestConvertClaudeMessagesMergesConsecutiveUserTurns` 由 3 轮改为 4 轮（尾部用户文本独立成轮），与上游“functionResponse 之后不再带其它 part”的修复一致。
  - `service/model_protocol_catalog_test.go`：**修复存量 flake**——用例用 `time.Now().UnixNano()` 生成 base URL，Windows 时钟粒度下连续调用会取到同一值，导致目录缓存 key 相撞、前一用例的成功目录污染“目录不可用”分支（`TestCommandCodeGatewayPassesThroughCatalogProtocols` 全量偶发失败）。改为进程内原子自增后缀。
- **跳过项**：`frontend/src/i18n/locales/en/admin/{accounts,overview,settings}.ts`、`frontend/src/i18n/locales/en/dashboard.ts`（en 语言包永久跳过）；`frontend/src/components/modelPlaza/PlazaModelPricingTable.vue` 与其 spec（模型广场前端永久裁剪）。对应 zh 文案 `admin.accounts.refreshTokenHint` 已随 zh 包并入。
- **依赖/构建链**：`go.mod` / `go.sum` / `package.json` / `pnpm-lock.yaml` 本区间零变更；无 ent schema 与迁移变更（二开迁移仍从 253 起）。
- **验证**：
  - 后端：`go build ./...`、`go vet ./...` 通过。`go test -tags=unit ./...` 与 HEAD（`3ad7ad1`）干净 worktree 逐项对比：失败集合完全一致（11 项：CLAUDE.md 记录的 10 项 + `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort`，后者在 HEAD 同样失败）。HEAD 全量跑还会命中上述 `TestCommandCodeGatewayPassesThroughCatalogProtocols` 时钟 flake，本次修复后不再复现。
  - 前端：本机无 pnpm，改用 `node_modules/.bin/{vue-tsc,vitest,vite}` 等价执行——`vue-tsc --noEmit` 通过；`vitest run` 298 文件 / 2409 用例全部通过；i18n 完整性检查通过；`vite build` 通过，产物写入 `backend/internal/web/dist/`。
  - 版本号一致性：`describe 328db26f` → `v0.2.24`；`backend/cmd/server/VERSION`=0.2.24；`backend/scripts/resolve-version.sh` 输出 0.2.24；`UPSTREAM-BASE`=`328db26f450edbdf9f8d4344a3ea903ac6396842`。
- **合并影响**：支付链路与 en 语言包零变更（`git status` 复核）；模型广场前端维持裁剪；`wire_gen.go` 的手工接线（三平台 provider）保留，仅按上游新增 `serviceUserPlatformQuotaRepository` 参数。高冲突面（`openai_gateway_*`、`gemini_messages_compat_service.go`、`antigravity_quota_fetcher.go`、`gateway_usage_billing.go`）已按上述语义合入，下轮同步需沿用。

### 平台移植源同步：workbuddy 语音/回放与企业额度 + qoder 签到拒绝语义（2026-11-23）

- **区间与规模**：workbuddy2api-panel `08cb9351 → d221c721`（14 提交；PORTER-BASE 基准仍为 `a19c78f`，含 11-20 已评估批次）；qoder2api-hub `25d05944 → 285d98be`（5 提交；基准仍为 `f0b41e4`）；trae-workbuddy-switch `ebcaead1 → ae9d92f3`（15 提交）。三仓库均 `pull --ff-only` 成功、工作区干净。
- **WorkBuddy 已移植**：
  - **11102/14401 归请求级（`d629a8c`）**：`pkg/workbuddy/errors.go` 的模型级判定新增 `route config not found`（14401，图像模型同族答复），与 11102 同归「非 11158」分支——服务层已有的 11102 不换号处置覆盖 14401，不再落通用 `KindClient` 白轮全池。`ModelBlockReasonFor` 对 14401 复用 `"11102"` reason。
  - **TTS 截断检测（`d5ce7de`/`c405254`，issue #277）**：`pkg/workbuddy/speech.go` 以 `speech.audio.done` 作为唯一完整标记；读到 EOF 或 `[DONE]` 仍未见 done、但已产出 PCM 时返回 `KindServer` 错误，交给语音服务换号重试，不再把中间层切断的半段音频当 200 成功。
  - **ASR response_format 与体积钳制（`d5ce7de`/`026f9c0`）**：`handler/gateway_workbuddy_speech.go` 支持 `response_format=json/text`（text 回 `text/plain`，srt/vtt/verbose_json 明确 400）；整包上限改为 3MB+64KiB（文件本身仍 3MB，避免恰好 3MB 的音频被 multipart 边界挤成超限），`MaxBytesError` 回 413，解析后 `MultipartForm.RemoveAll()` 清理临时文件。
  - **Responses 回放合并与 reasoning（`9ad206b`/PR #281）**：`pkg/apicompat/chatcompletions_responses_bridge.go` 的 `appendAssistantToolCall` 在合并同轮 assistant 文字与工具调用时**追加**第二段 reasoning（相同内容去重）；新增 `mergeAssistant` 轮次边界状态，`phase=final_answer` 与 compaction/未知条目之后的 function_call 不再并进上一轮 assistant。并行调用的第二条不再重复追加同一段思考。
  - **企业额度口径（trae 仓 `70bf9de`/`7e277b7`，改 WorkBuddy 模块）**：`pkg/workbuddy/billing.go` 企业端点响应缺 `credit`/`used_num` 时返回错误（余额未知），不再按 0 已用伪报满额；企业额度请求删除 `X-Enterprise-Id`/`X-Tenant-Id`，租户归属由服务端按 Bearer 推导。
- **Qoder 已移植**：
  - **签到/活动领取拒绝语义（`958f14f`/issue #167）**：`pkg/qoder/accounts.go` 的 `ClaimCampaign` 不再把任意 409 当「已领取」——仅错误体明确 `ALREADY_CLAIMED`/`REPLAYED` 才幂等成功；`SAME_PERSON_ALREADY_CLAIMED` 归同人拦截；其它已知失败码（风控/活动未开始等）返回带码结果。成功路径只在无 failureCode、`success != false` 且状态为 CLAIMED/GRANTED/SUCCESS 时 OK；裸 `status=BLOCKED` 归「上游未确认领取」。不可领取原因中除名额发完/成就未完成外，已知失败码归错误而不是「新人任务」；消息优先展示错误（「活动领取未成功」）。
  - **运营兜底隔离**：`service/qoder_ops_service.go` 仅在活动列表查询失败（`camp.Error`）时回落旧 sash Checkin；领取阶段被拒且无任何成功/已领/同人拦截时返回 `errQoderCampaignClaimRefused` 作为任务失败，不再回旧接口重复领取；部分成功时 note 追加未领取项。
  - **可选省略默认 system prompt（`f57c56e`/`1a9ae79` 的网关等价物）**：`pkg/qoder/baseprompt.go` 新增 `QD_OMIT_DEFAULT_SYSTEM_PROMPT=1/true`：客户端未提供 system 时省略模板自带 Qoder 提示词；客户端显式 system 始终优先。默认关闭，行为不变。
- **三平台通用**：
  - **raw CC 上游流 idle 超时（trae 仓 `ab1ded7` 的 `stream_with_idle_timeout` 语义）**：新增 `service/cc_upstream_idle.go`，在 `sendCCUpstreamRequest` 返回前包装 `resp.Body`——按相邻字节块计 idle，超时关闭上游 body 并返回 `errCCUpstreamStreamIdle`。workbuddy CN 默认 60s（与 panel 同口径；`WB_STREAM_IDLE_TIMEOUT_SECS` 对两区域可覆盖，0 关闭），workbuddy global 与 qoder/trae 沿用 `gateway.stream_data_interval_timeout`（未配置时关闭）。此前 raw CC 读循环只有 scanner，无读超时。
- **评估未移植（原因，勿重复评估）**：
  - workbuddy `d221c72` 同域重名优惠模型把 promo 标签并进 name：本侧 `/v1/models` 只回 id/object/created/owned_by，不暴露 display name 与 promo 字段，无重名歧义来源。
  - workbuddy `24e59f8`/`4221f2b`/`c27d731`/`e2d0eb6`（视频/MCP generate_video）：媒体系列 2026-11-04 用户裁定跳过。
  - workbuddy `be99749` 跨账号缓存探针、`c84b6f2` 测试夹具时间、README 更新：诊断/测试/文档。
  - qoder `1a9ae79` 面板 settings.json 写入保护、`285d98b` 发版、`431c3f0` 合并提交：本侧无该面板。
  - trae `ab1ded7` 其余桌面端项（本地快照凭据同步、会话切换复核、逐时统计、进程探测、api_guard 通配域）、`fb2eebf`/`13ae2ee`/`ae9d92f`/`3411f52`/`0e23f87`/`5f0448d`/`f187f4f`/`a0e6390`/`7b7b363` 发版/CI/桌面 UI：桌面端专属。
  - trae `5d41e02` 企业/教育版额度读取：本侧 `billing.go` 已有企业端点（本次只补 `70bf9de`/`7e277b7` 两处修正）。trae `3762249` Qoder 业务按会话聚合：本侧 `conversation_identity.go` + `baseprompt.go` 已有稳定 business.id/begin_at/name（#128）。
  - 上次（11-20）记录的 workbuddy 救援/续写体系、qoder Responses 托管工具/外部检索后端仍按原口径暂不实现，不重复评估。
- **改动文件**：`pkg/workbuddy/{errors,speech,billing}.go`；`pkg/qoder/{accounts,baseprompt}.go`；`pkg/apicompat/chatcompletions_responses_bridge.go`；`handler/gateway_workbuddy_speech.go`；`service/{openai_gateway_cc_pipeline,qoder_ops_service}.go` + 新增 `cc_upstream_idle.go`。测试：新增 `workbuddy/speech_test.go`、`qoder/campaign_denial_test.go`、`apicompat/chatcompletions_responses_bridge_turn_merge_test.go`、`service/cc_upstream_idle_test.go`、`handler/gateway_workbuddy_speech_test.go`；更新 `workbuddy/{errors,billing}_test.go`、`qoder/{baseprompt,accounts}_test.go`。
- **验证**：`go build ./...`、`go vet ./...`、`gofmt` 通过；定向 unit（workbuddy/qoder/apicompat/service/handler 的新增与受影响用例）全绿。全量 `go test -tags=unit ./...` 与 HEAD 基线对比：失败集合保持 CLAUDE.md 记录的 11 项（10 项 + `TestOllamaProbeCallback_StaleLongDoesNotOverrideNewShort`），无新增回归；qoder 包的存量 `TestRecoverTruncatedEchoSwallowed` panic 会中止该包后续用例，新增用例已用 `-run` 单独验证通过。本机无 pnpm；本轮未改前端。
- **合并影响**：`pkg/apicompat/chatcompletions_responses_bridge.go` 与 `service/openai_gateway_cc_pipeline.go` 属上游同步共面文件（后续上游同步需保留：同轮合并/reasoning 追加、`mergeAssistant` 边界、idle body 包装）；其余为纯二开文件。**不触碰 `UPSTREAM-BASE` / `VERSION`**。
- **PORTER-BASE**：trae → `ae9d92f3`（本轮建议项均有结论）；workbuddy 保持 `a19c78f`、qoder 保持 `f0b41e4`——11-20 记录的两个大项（workbuddy 救援/续写体系、qoder Responses 托管工具）仍属「以后再说」，按基准规则不推进。
