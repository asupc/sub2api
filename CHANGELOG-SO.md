# sub2api-so 二开变更记录

> 规则：二开变更（无论是否经 `sub2api-upstream-sync` 技能）都必须记录于本文件，新条目追加到文件末尾，每条含：日期、功能描述、改动文件/模块、是否影响与上游合并。
> 基准：`UPSTREAM-BASE`（单行完整哈希）只在完整上游同步后前移；手动移植只记本文件、不改基准（唯一例外见 10-13 收口条目）。
> 整理记录：2026-10-12 / 10-13 / 10-15 三次压缩，只保留功能事实、关键决策、合并冲突面与已知缺口；根因推导、测试输出、逐提交哈希与逐文件清单见 git 历史（本次压缩前 HEAD=`f5aa70c`，10-13 压缩前 HEAD=`45cf4c6`）。

## 当前状态（2026-10-13 核实）

| 项 | 状态 |
|---|---|
| 基准 | `UPSTREAM-BASE`=`4d9ac3e4`（v0.2.16）；`VERSION`=0.2.16（与 `build-so.sh` 推导一致） |
| 后端 | 代码 = v0.2.15 + v0.2.16 子集（17 提交）+ 二开（三平台源码集成、Antigravity 直接模型模式、账号/计费/网关定制，逐项见时间线） |
| 前端 | 二开：功能入口裁剪 + UI/UX 体系重构 + 仪表盘/用量统计/账号表格增强 |
| 数据库 | 官方迁移序列终态（裁剪期 drop 的表由 `deploy/restore-upstream-schema.sql` 恢复，可重放幂等）；二开迁移命名 `NNN_so_*.sql`，与上游编号隔离 |
| 验证基线 | 2026-10-13：后端 `go build` + `go test -tags=unit ./...`（61 包）全绿；前端 typecheck + vitest（286 文件 / 2114 用例）+ build 全绿 |

- **功能边界**：支付永久不同步（2026-10-08 用户指示；上游 #7881 EasyPay 回调签名修复未同步，启用支付前需补）；en 语言包永久跳过；前端已删入口的官方功能视为隐藏（后端接口与表随官方保留）。
- **运行约束**：`step_up_enabled` 禁止开启（二开无 TOTP UI）；生产 `config.yaml` 的 `totp.encryption_key` 必须保留现值（支付/备份/插件 AES 加密复用）。
- **待办**：`GetUserUsageTrend` 的 `metric` 参数后端已同步，自研仪表盘 UI 未接入。

## 前端裁剪范围（同步时「本地已删」即跳过；上游命中的改动由技能标记 MISSING_IN_SO）

- 渠道管理（渠道监控保留）、支付、公告、兑换码、优惠码、订阅管理、模型广场、可用渠道页
- OAuth 登录（GitHub/Google/LinuxDo/WeChat/DingTalk/OIDC）、TOTP/Passkey/Step-up、代理/IP 管理（ProxiesView、ProxySelector）
- 语言切换（固定 zh）、KeysView IP 黑白名单、UsersView 订阅列（`include_subscriptions` 保留）、登录/注册仅邮箱密码（`aff_code` 透传保留）
- `skills/sub2api-admin` CLI 移除 redeem-codes 命令组

## 同步口径与高冲突面

- **口径**：按文件三路判定——二开未改则覆盖；改过则 `git merge-file`；上游新增文件先查是否引用已裁剪模块。
- **仪表盘**：`components/dashboard/GatewayDashboard.vue`（自研 admin/user 双 scope；user 视角绝不触达 admin 接口）。
- **账号 UI**：`CreateAccountModal.vue`（二开 `platformOptions` 紧凑网格与上游清单驱动的 `extraMultiProtocolPlatforms` 并存，新增主平台需手工维护数组）、`EditAccountModal.vue`、`AccountUsageCell.vue`、`CNProviderQuotaCell.vue`、`OpenAIQuotaResetCell.vue`（popover props）、`PlatformOpsQuotaCell.vue`、`UsageProgressBar.vue`（与上游仅差 visibilitychange 一处）。
- **网关 CC 漏斗**：`openai_gateway_cc_pipeline.go`（sendCCUpstreamRequest / resolveCCFallbackTarget）、`openai_gateway_chat_completions_raw.go`、`openai_gateway_forward.go`（协议分流 switch）、`openai_gateway_{responses,messages}_chat_fallback.go`、`openai_gateway_service.go`、`openai_gateway_usage.go`（RecordUsage 计费与 credits）。
- **账号与限流**：`account.go`、`account_test_service.go`、`ratelimit_service.go`（临时不可调度 scope 与 401 规则）、`temp_unsched.go`、`admin_account.go`、`account_handler.go`、`token_refresh_service.go`、`upstream_models.go`、`antigravity_gateway_service.go`、`gemini_v1beta_handler.go`。
- **计费与模型**：`billing_service.go`、`pricing_service.go`、`gateway_request.go`、`gateway_claude_oauth_body.go`、`gateway_upstream_request.go`、`pkg/claude/constants.go`、`pkg/apicompat/*`、`model_prices_and_context_window.json`、`useModelWhitelist.ts`。
- **装配与设置**：`cmd/server/wire.go`/`wire_gen.go`、`handler/handler.go`、`handler/wire.go`、`handler/gateway_handler.go`、`server/routes/admin.go`、settings 管线（setting_parse/update/view、dto/settings）、`platformCatalog.builtin.json`、`router/index.ts`、`AppSidebar.vue`、`AccountsView.vue`、`SettingsView.vue`、`stores/adminSettings.ts`。
- **群组/审核**：`group_repo.go`、`setting_repo.go`、`content_moderation.go`、`GroupBadge.vue`、`AccountGroupsCell.vue`、`Select.vue`、`ChannelStatusV1View.vue`。
- **后端移植触点**：功能 A `GET /admin/dashboard/api-keys-usage-stats`（`usage_log_repo_apikey_stats.go`、`dashboard_handler.go`、`dashboard_query_cache.go`、`dashboard_service.go`、`routes/admin.go` 等）；功能 B `openai-token-stats` 增强（today 自然日 + `total_tokens`/`cache_hit_rate`：`ops_dashboard_handler.go`、`ops_repo_openai_token_stats.go`）；`cmd/server/main.go` 嵌入 IANA tzdata（Windows 裸跑必需）。
- **其它结构性二开**：`components/admin/channel/types.ts` 内联 `ChannelModelPricing` 等类型（上游 channels API 已删）；全站 dense 紧凑化（118 文件，确定性映射可重放，上游改样式时按规则重放）；UI 基准规范 `docs/ACCOUNT_TABLE_UI_SPEC.md`。

## 已知坑点

- **后端测试**：`backend/config.yaml`、`backend/data/config.yaml` 会污染 viper，`go test` 前临时挪走、测后恢复（`internal/config` 的存量失败即此因）。
- **上游拉取**：`git pull --ff-only` 不取 tag，必须 `git fetch --tags`（否则版本推导滞后一档）；上游 v0.2.7 前曾 force push，遇 `--ff-only` 分叉先 cherry 比对，再 `reset --hard` 对齐。
- **pnpm 与 lock**：根 Dockerfile 硬编码 pnpm@9，与 so lock（无 overrides 块）互斥（`ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`）。解法是镜像构建走 `deploy/Dockerfile.bundle`（唯一差异 corepack pnpm@9→pnpm@11.9.0，上游同步后需对照 diff 保持）；package.json 的 `pnpm.overrides` 在 pnpm 10+ 是死配置。
- **Nexus 私有镜像**：陈旧元数据可能报 `ERR_PNPM_NO_MATCHING_VERSION`，小版本升级可按传递依赖一致原则手改 lock 三处（importers/packages/snapshots）后 `pnpm install --frozen-lockfile`。
- **上游自带 spec 回归**：上游偶发改实现未更新自家测试（v0.2.14/0.2.15 各两例），以内容为准适配，并在条目中注明。
- **前端改动**：须 `pnpm build` 重建 dist 后重启容器才可见（so-dev 为编译期内嵌）。
- `psql` 连接用 `127.0.0.1`（localhost 有 IPv6 问题）；`frontend/pnpm-workspace.yaml` 的 `allowBuilds` 将 esbuild/vue-demi 置 true。

## 开发与部署环境

- **so-dev Docker 栈**（`deploy/docker-compose.so-dev.yml`）：backend `go run -tags embed` 热跑（golang:1.27.2）+ PG16 + Redis7；后端 `127.0.0.1:8088`，`AUTO_SETUP=true`；`frontend/.env.development.local`（gitignored）设 `VITE_DEV_PROXY_TARGET=http://127.0.0.1:8088`。
- **本机构建**：工作区根 `./build-so.sh`（前端 pnpm build → Docker go build → 单文件二进制；版本号从 `UPSTREAM-BASE` 推导 tag）/ `release-so.ps1`；本机无 Go。旧构建产物（`backend/bin`、`backend/dist`）仍报告 0.2.15，需重新构建。
- **离线打包**：`deploy/build-docker-tar.sh`（docker save tar + sha256 + `dist/docker-bundle/`：镜像 + `docker-compose.bundle.yml` + .env.example + README）；PG/Redis 独立命名卷；已出 v0.2.13/v0.2.15 包并冒烟通过。
- **生产部署铁律**（换官方系后端二进制时）：停机 → `pg_dump` → **先** `psql -f deploy/restore-upstream-schema.sql`（抽查 `to_regclass('public.channel_model_pricing')` 非空）→ **后** 换二进制启动观察迁移重放 → 冒烟。回滚 = 换回旧 so 二进制（drop 迁移有记录不重跑），数据回滚用备份。⚠️ 2026-09-24 首启失败教训：顺序颠倒会因官方迁移缺表启动即退。

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

## 变更时间线

### 2026-09

- **09-09~09-11 前端 UI/UX 重构**：确立 `docs/ACCOUNT_TABLE_UI_SPEC.md` 基线（账号表格 dense、行高 48–54px、表单 label 左/控件右）；仪表盘 Bento 栅格与运维 SLA 色标；全站间距降档 dense 化（118 文件，确定性映射可重放）。合并：UI 层大面积二开，同步多为三路合并。
- **09-14 仪表盘与用量统计**：用户/管理员仪表盘统一为 `GatewayDashboard`（scope prop，后端零改动）；使用记录拆出「用量统计」页（`/usage-stats`，无费用口径）。合并：`GatewayDashboard.vue` 高冲突。
- **09-17~09-22 仪表盘/运维扩展**：全局默认时间范围「今天」；「Token 请求统计 · 今日」表；「API 密钥用量统计」板块；管理台与运维监控移除多区块（接口保留；`OpsDashboard.vue` 上游可整文件覆盖）。合并：`GatewayDashboard.vue` 高冲突。
- **09-26→10-13 账号用量窗口**：多轮迭代（微胶囊 → 双行严整 → 去底色 → 恢复），**最终回归上游 v0.2.15 原版**（每窗口纵排 `UsageProgressBar`，`AccountUsagePopover` 与 238px 规格文档已删）。合并：`UsageProgressBar.vue` 与上游仅差 visibilitychange 一处；`AccountUsageCell.vue`、`CNProviderQuotaCell.vue` 与上游同构，冲突面小。

### 2026-10

- **10-03 管理控制台增强**：资源健康大盘（账号池/用户生态/密钥态势/在线新鲜度）、流量走势（Token/请求数双模式）、分组负载分布（`GroupUsageStats`，`include_group_stats`）。
- **10-06/10-11 离线打包**：`build-docker-tar.sh`、`Dockerfile.bundle`、`docker-compose.bundle.yml`（见「开发与部署环境」）。
- **10-12 管理控制台布局**：admin 仪表盘「模型分布 / 分组使用分布」1:1 → 3:1。合并：`GatewayDashboard.vue`。
- **10-12 三平台源码集成（workbuddy / qoder / trae）**：四批（登记与运营基建 → 网关/OAuth/运营/端点 → 回归收尾 → 前端接入）+ 修复系列。`pkg/{trae,workbuddy,qoder}`、`{workbuddy,qoder,trae}_ops_service.go`、`*_oauth_service.go`、`forms/{WorkBuddy,Qoder,Trae}Form.vue`、`DeviceAuthFlow.vue`、`api/admin/platformOps.ts` 为纯新增；触点文件全部收录于「同步口径与高冲突面」节，需手工合并。
  - **登记与菜单**：`domain/constants.go`、`platforms.go`、`provider_profile.go`（三平台 by_inbound + 仅 chat_completions 端点；账号 `api_protocol=adaptive` 收敛到 CC 路径，Claude Code 经既有 CC×anthropic 链可用）；侧边栏「账号管理」分组化（`/admin/accounts/p/:platform`，设置键 `account_menu_hidden_platforms`）。
  - **网关**：平台分叉收敛在 `openai_gateway_cc_pipeline.go` 的 `sendCCUpstreamRequest`（体改写/头注入/发送/响应包装，三条 raw CC 路径共用）与 `resolveCCFallbackTarget`、`rawChatCompletionsURL`；平台文件 `openai_gateway_{workbuddy,qoder,trae}.go`；错误分派 `openai_gateway_source_integrated_errors.go`（先平台分类，后通用 failover）。
  - **OAuth/凭据**：`{workbuddy,qoder,trae}_oauth_service.go`（设备/PKCE 会话 TTL 15min、刷新链、`*TokenRefresher` 到期前 30min）+ `handler/admin/*_oauth_handler.go`；trae 虚拟设备私钥存 `credentials.device_private_key`；凭据归一化接入 `admin_account.go`。
  - **运营任务**：`service/platform_ops.go`（任务描述/每日定点/`ops_state`/注册表）+ 三平台 ops service；平台级调度设置存 `settings.platform_ops_config`；端点 `/admin/platform-ops/platforms/:platform/config`（GET/PUT）、`/admin/platform-ops/accounts/:id/tasks/:task`；文档 `docs/platform-ops-tasks.md`。
  - **额度与降级**：快照 `extra["wb_quota"]`/`extra["qd_quota"]`/`extra["trae_quota"]`；workbuddy 402/14018、qoder 402 → `error(*_no_credit)` 停调度，签到/余额成功后自动恢复。
  - **测试与模型目录**：测试连接走真实转发路径；默认测试模型登记在 provider profile（`DefaultQoderTestModel=Auto`）；qoder 对外模型清单用官方显示名（`CatalogEntry.PublicID()`，账号 model_mapping 大小写敏感）；`SyncUpstreamModelsPreview` 支持 credentials 直传预览上游模型。
  - **错误处置**：qoder 10605 排队按 429 冷却不罚号（建流前/流内首帧/测试连接三路径）、单号池 429/排队不冻结唯一账号（`qoderSingleAccountPool`）；workbuddy WAF 403 软冷却不罚号、上游指纹 11128 → `pkg/workbuddy/sanitize.go` 清洗（Claude Code 身份句、反探测数字）；trae 上游必填 `app_version_code`；两条 CC 回退路径接入平台错误分类。
- **10-13 账号菜单两项**：① 添加账号按平台菜单预选平台（`CreateAccountModal.vue` 新增 `defaultPlatform` prop，`AccountsView.vue` 透传路由平台）；② 设置键 `account_menu_default_platform`（默认空）——侧边栏父项直达该平台列表，被黑名单隐藏的平台仍保留入口。合并：两文件上游同名需手工合并。
- **10-13 qoder/workbuddy 今日积分**：迁移 `242_so_account_daily_credits.sql`（`account_daily_credits`，主键 `(account_id, day)`，级联删除）；usage 捕获 `credits`/`credit`，`RecordUsage` 落库前按日累计；列表「今日 N」胶囊（`WindowStats.credits`，批量查询无 N+1）。合并：`openai_gateway_usage.go`、`openai_gateway_response_handling.go`（首次触碰，各一处小改动）；迁移、仓库与测试为新增。
- **10-13 Antigravity 直接模型模式 + 测试/编辑修复**：空映射 = 直通（不再自动注入 `DefaultAntigravityModelMapping`；预设按钮与 `/admin/antigravity/model-mapping` 保留，存量映射账号不受影响）；测试请求去掉 `maxOutputTokens:1`（修 thinking 模型「假成功 + 空白」）；直接模式测试走上游 live 目录；编辑弹窗修复「保存后模型列表被清空」。合并：`account.go`、`antigravity_gateway_service.go`、`account_test_service.go`、`account_handler.go`、两个 Modal 需手工合并。
- **10-13 qoder 工具历史结构化直传 + 畸形回声兜底**：`StructuredToolHistoryEnabled`（env `QD_STRUCTURED_TOOL_HISTORY`=off/on/auto，auto 限 cn + 模型前缀白名单）；`leakedMalformedEcho` 吞畸形回声（流末/流中/非流式三处）。CN 真机实测通过；INTL 因上游排队未实测（补验后再考虑放开 auto）。合并：纯二开文件。
- **10-13 Claude Haiku 5.5 全链路接入（issue #7919）**：模型识别（`pkg/claude/constants.go`、`IsHaiku55`）；价卡（`model_prices_and_context_window.json` 含 >100k 绝对价，三源阶梯一致）；协议转换（`pkg/apicompat`：强制 tool_choice、采样锁定、effort=none 映射）；网关校验（`gateway_request.go`：adaptive thinking 必选、disabled 仅 high 及以下）；前端白名单与预设。合并：上游同名文件手工合并；`gateway_request_haiku55_test.go` 新增。
- **10-13 GPT-6.1 Sol 订阅长上下文附加费豁免**：OAuth 类账号请求 GPT-6.1 Sol 不收模型价卡长上下文阶梯（API Key 账号仍按 API 价卡）；豁免按**实际发送模型**判定（`openAISubscriptionLongContextExempt`）；`CostInput.DisableModelLongContextPricing` 仅关阶梯、不改自定义区间价。合并：`billing_service.go`、`openai_gateway_usage.go` 手工合并。
- **10-13 Responses input 校验失败降级 + codex 上下文 500k**：auto 模式上游 400 拒绝结构化 Responses input（`isConvertedResponsesInputStringRejection`）时自动降级 raw chat completions 重试；GPT-6.1 Sol 生成上下文默认 500k（上游显式元数据优先）。合并：`openai_gateway_chat_completions.go`、`openai_codex_models_service.go` 手工合并。
- **10-13 内容审核分组删除竞态修复**：删除分组时清理审核配置 `group_ids`（jsonb 原子改写，保留顺序与其它字段）；写入侧 `SetContentModerationConfig` 事务内锁存活分组；锁序统一 groups → settings。合并：`group_repo.go`、`setting_repo.go`、`content_moderation.go` 手工合并。
- **10-13 前端交互修复 4 项**：分组名完整换行（`GroupBadge.vue` `truncate` prop）；后台标签页倒计时停摆（`UsageProgressBar.vue` 监听 `visibilitychange`）；输入法组合态方向键被抢（`Select.vue` `isComposing` 守卫）；渠道状态手动/自动刷新统一（`ChannelStatusV1View.vue`）。合并：上游同名文件手工合并。
- **10-13 同步收口 v0.2.16（部分移植）**：上游 `3a6fd1c9` → `4d9ac3e4`（51 非合并提交/213 文件），移植 17 个（两批，明细见 git 历史）；人工解决 `openai_gateway_forward.go`（保留二开协议分流 switch，仅补 `startTime`）与 `openai_gateway_workbuddy_test.go`（补上游新签名 `time.Now()`）；第二批 12 文件三路合并无冲突。验证：后端 build/vet/unit（61 包）+ 前端 typecheck/vitest/build 全绿（integration 需数据库未跑）。
  - **永久放弃（勿再当作待同步项）**：支付 1、净 diff 为零的依赖补丁 2、Antigravity 1、分组多语言 2、Gemini 显式缓存/Vertex/AQ Key 17、中等价值项 10、版本提交 1（逐提交哈希见压缩前 git 版本）。
  - **基准前移（一次性例外）**：`UPSTREAM-BASE` 前移至 `4d9ac3e4`，VERSION 置 0.2.16——二者必须同步，否则 `build-so.sh` 推导版本与 VERSION 矛盾。被放弃提交的改动已计入基准、二开侧不含它们，后续三路合并保持二开现状；仅与之相邻的后续上游改动可能冲突，需人工判断。
- **10-14 修复 Antigravity 已删模型回流 /v1/models**：`resolveModelMapping` 对**非空** `model_mapping` 的账号运行时回填 18 条 flash 自映射 + 3 条 pro 别名，被删模型回流 `/v1/models`、Codex 清单、`/v1beta/models`、composite 与 admin 分组候选。移除回填（`ensureAntigravityDefaultPassthroughs` 系成为孤儿已删）；`applyAntigravityGemini31ProAliases` 只改写已配置键、不再新增；变体解析并入 `mapAntigravityModel`（转发、调度准入、限流键三处同口径），只配变体未配裸名的账号仍接受裸名请求。横向排查：Grok/GoogleOne 注入只在空映射分支，其余 10 平台配置 1 条返回 1 条，本 bug 为 Antigravity 独有。合并：`account.go`、`antigravity_gateway_service.go` 手工合并。
- **10-14 workbuddy 移植 tool_pairing 配对清理（修 11133/11148 会话顶死）**：来源 workbuddy2api-panel `535e19e`（PR #148，线上 11133）。新增 `pkg/workbuddy/tool_pairing.go` `CleanupOrphanToolCalls`（孤儿 tool_call↔tool 两侧对称裁剪 + 空 `function.name` 脏条目否决；无工具流量零改动），挂接 `PrepareChatBody` 覆盖网关转发/测试连接/运营任务全部出站路径。根因：流式异常早停产出 `name:""` 条目被客户端写进历史，下轮转发上游判 400 11133（整条会话每轮 503 报废）；只删调用侧会留孤儿 tool 结果 → 11148，故整函数移植。未移植 `mergeAdjacentToolCalls`/`repackToolResultBlocks`（上游 deepseek-only 场景），已知缺口：拆条/插队报文形态仍可能 11148。合并：纯二开新增文件 + `client.go` 局部插入。
- **10-14 Qoder 签到闭环改造（修「提示成功但积分不到账」）**：根因 `runCheckin` 只打旧 sash `/daily-check-in`（CN DISABLED、INTL 404），跳过分支返回 OK → 前端弹成功但零入账；官方每日领取已迁到活动平台。参照 qoder2api-hub 重建：`CampaignCheckin` 编排（列活动 → 分类 → 逐个领 CLAIMABLE CREDITS；`nextCheckinWindow` 按 UTC+8 每日 10:00 名额刷新；earned 只计真实新增防虚增）；`runCheckin` 活动平台优先，仅列表查询失败才走旧 sash 接口；BLOCKED 记 6h 冷却持久化 `extra["qd_campaign_blocked"]`（多号同机防风控）；checkin Hours 默认 `9,21→10,21`；`runCampaigns` 排除 CREDITS 类（与 checkin 零重叠）。连带基建：`OpsStateEntry` 加 `Note`、`RunOne` 签名 `error → (string, error)`（三平台 15 个 RunOne 适配）、RunTask 返回 `{done,note}`、前端 toast `runSuccessNote`。已知限制：跳过态仍记 LastOK 属权威语义（改判失败会退避重试每天打上游），本次只修可见性。合并：纯二开模块，无冲突面。
- **10-14 workbuddy/qoder 签到状态胶囊**：用量列（`PlatformOpsQuotaCell.vue`）每日签到三态——今日成功绿「已签到」（title 含 note+时间）、跑过未成功红「签到失败」（title 含 last_err）、其余灰；数据读 `extra.ops_state.checkin`（零后端改动）；「今天」按账号时区（`extra.ops_timezone` ?? Asia/Shanghai）`Intl` 判定，与后端 `sameLocalDay` 同口径；trae 不展示。合并：本就在高冲突面清单内。
- **10-14 Qoder intl 双缺陷修复**：**A 模型路由三处漂移**——① 出站头从不发 `x-model-key`/`x-model-source`（修复：签名头注入时从原始 OpenAI 体解析模型、带 realm 归一为上游 key）；② `ResolveUpstreamKey` 丢 realm（跨区显示名歧义）；③ 目录元数据固定 cn 优先（双区同 key 元数据不同，intl 请求体按 cn 快照填错）。②③ 修复在 `pkg/qoder/baseprompt.go`。**B intl 签到缺真实 UMID 机器身份**（派生六头被活动平台整条过滤）——新增 `pkg/qoder/umid.go`（runtime-info 组件发现 → 调用取真身份，进程内 30min + 落盘双层缓存；`DesktopHeaders` 原生身份优先、导入凭证 machine_token 次之、无真身份不发六头；`Campaigns` 自愈清种子换身份重试一次）；`deploy/Dockerfile` 运行时依赖加 `gcompat libstdc++ libgcc`（组件为 glibc ELF，musl 需兼容层）；文档 `docs/qoder-umid.md`。已知限制（服务端策略）：机器身份机器级，intl 按设备去重，每设备每日仅 1 个 intl 账号可领。合并：全部位于二开文件。
- **10-15 模型清单与账号供给透视面板（Model Manifest）**：「模型 ⟷ 账号」倒排查询（方案 `docs/MODEL_MANIFEST_PLAN.md`，UI 对齐 `mockups/model-manifest-preview.html`）——某模型谁在供给/账号状态、某账号提供哪些模型、哪些模型全员断供。后端 `service/model_manifest_service.go`（一次全量加载内存倒排聚合，0 上游网络请求；模型解析优先级 `model_mapping` keys（跳过通配符）→ `extra.upstream_model_metadata` 快照 → 平台默认清单兜底；断供 = 可调度账号数为 0）+ `handler/admin/model_manifest_handler.go` + `GET /api/v1/admin/models/manifest`；前端 `views/admin/ModelManifestView.vue`（指标卡、按模型/按账号双视角、客户端过滤）+ 路由 `/admin/models` + 侧边栏入口 + i18n。装配：`wire_gen.go` 手工接线（该文件不可 wire 重新生成——ProviderSet 缺 workbuddy provider 属既有状态）。合并：新增文件为主，触点小段插入，低冲突。
- **10-15 Docker 打包脚本 tar 移至工作区根**：`deploy/build-docker-tar.sh` 打包后将 tar 及 `.sha256` 移到工作区根（`TAR_MOVE_DIR` 可覆盖）；`dist/docker-bundle/` 不再自包含，包内 README 的 `docker load -i` 需到工作区根取 tar。合并：纯二开脚本。
- **10-15 账号测试模型列表与白名单对齐**：`GetAvailableModels`（`GET /admin/accounts/:id/models`，测试弹窗与定时测试面板共用）原先三分支忽略账号已配置的 `model_mapping`，下拉展示平台全集——可选中账号实际不服务的模型。修复：helper `storedModelWhitelistIDs`（读原始 `credentials.model_mapping`，跳过空键与 `*` 通配符；`GetModelMapping` 对 Grok/GoogleOne 注入默认映射故不可用），三平台 OAuth（`integratedPlatformTestModels`）、Claude OAuth/SetupToken、Gemini OAuth 含 GoogleOne 三分支统一「白名单非空 → 映射请求侧 keys；空 → 平台目录」；通配符映射视为无法枚举回落目录。其余分支横向核实本就对齐。合并：`account_handler.go` 小段插入。
- **10-15 清理死代码 account/AccountTestModal**：`components/account/AccountTestModal.vue` 为 `components/admin/account/` 版本（AccountsView 实际使用）的旧副本，无视图引用，连同 spec 与 barrel 导出行删除。
- **10-15 /admin/usage 分页启用快速跳页**：`Pagination` 组件内置跳页输入框（`showJump` 默认 false，上游无页面启用），主日志表与错误 tab（`OpsErrorLogTable` 内部分页）两处启用；`OpsErrorLogTable` 亦被 `/admin/ops` 复用，同步获得跳页。合并：模板单属性插入，三路合并可自动处理。

### 2026-10-15 模型能力测评方案与 UI 原型

- 新增模型能力测评实施计划：高难度 `hard-v1` 题库（48 题 / D3-D5）、程序自动评分、Token/耗时/成本指标、效率分与综合评级、历史曲线和定时执行设计。文件：`docs/MODEL_CAPABILITY_BENCHMARK_PLAN.md`、`openspec/changes/add-model-capability-benchmark/`。
- 新增纯静态可视化原型：模型排行、能力拆解、历史曲线、题库家族、计划表单和 Run 详情。文件：`mockups/model-capability-benchmark-preview.html`。
- 本条仅为方案与原型，不包含后端/前端功能实现；不影响与上游合并的运行时代码。

### 2026-10-15 qoder 出站平铺工具嵌套归一化（porter 同步）

- **修 qoder 平铺 function 工具出站 400**（移植 qoder2api-hub `2fc18d6` / PR #30）：客户端直打 `/v1/chat/completions` 带平铺工具（`{"type":"function","name":...}`）时 `BuildAgentChatBody` 原样透传，上游要求 `tools[].function` 对象而 400（`'function' is a required property, expected an object - 'tools.N'`）。修复：新增 `normalizeTools`（幂等，已嵌套原样透传、非对象条目丢弃、`strict` 保留、缺失 `description`/`parameters` 补默认），在 `BuildAgentChatBody` 的 tools 覆写处统一调用——所有路径共用这处出站咽喉，对齐 Python 侧把 `normalize_tools` 移进 `build_qoder_body`。responses/anthropic 桥产物本就是嵌套形态，不受影响。合并：纯二开文件。
- 连带补 `pkg/qoder/umid_test.go` 缺失的 `//go:build unit` 标签（包内其余测试文件均有，缺它时无 tag 的 `go vet ./...` 编译不到 `oauth_test.go` 的 `withRealm`）。
- **porter 评估未移植**：qoder `ac87c69` agent 一键配置器（改网关本机文件、loopback-only，与服务端多用户网关架构不符）；trae `7364861` 账号池恢复出口（平台账号状态本有管理端启用出口）、`20e9e90` no_credential 按因分档（`ErrNoAvailableAccounts` 已带 `summarizeSelectionFailureStats` 八维分档）、`f4db208` 流式日志 token 恒 0（usage 挂流末 chunk、平台流结束后记账，架构免疫）、`789ea7c`/`14e5c28`/`6d11182`/`358fabd`/`d655dc6`（测试/部署/桌面端路径/构建/会话指纹缓存，均不适用）。PORTER-BASE 首次入库并推进：qoder→`b850c7e`、trae→`7364861`；workbuddy 无增量。

### 2026-10-15 模型能力测评 MVP

- 新增高难度模型能力测评 `hard-v1`（24 题族 × 2 动态实例 = 48 题，D3-D5），程序自动判定数学 / 算法 / 逻辑 / 代码 / 规划 / 指令能力，输出能力分、分类分、可见输出 Token 效率、耗时效率、综合评级与逐题证据；上游失败不计为答错。
- 新增定时与手动排队执行：计划默认停用，全局并发 1，单计划串行、单题超时、PostgreSQL `FOR UPDATE SKIP LOCKED` 原子领取、running/claim 租约恢复与 panic 释放。
- 新增 Token / 推理 / 缓存 / 总 Token、耗时、usage 覆盖、估算成本与历史曲线持久化；usage 缺失显式 `missing`，不虚构效率分。
- 新增迁移 `243_so_model_capability_benchmark.sql`、Repository、Service、Executor、Runner、Admin API `/admin/model-benchmarks/*` 与前端 `/admin/model-benchmarks` 控制台（指标卡、排行、能力拆解、多指标历史曲线、计划管理、Run 详情）。Phase 1 仅支持 openai / Kimi / Zhipu / DeepSeek / MiniMax / OpenCode / Command Code / Cline 的 Chat Completions API Key 账号，其他目标服务端拒绝。
- 验证：后端专项单测、repository integration、`go build ./...` 通过；前端 vitest 286/2122、typecheck、lint、build 通过。全量 backend unit 存在 2 个可复现既有失败，integration 有 1 个复跑通过的历史波动，lint 工具 0 issues 但 typechecking context 退出 1；明细见 `openspec/changes/add-model-capability-benchmark/implementation-evidence.md`。
- 合并：新增文件为主；`wire*.go`、`handler.go`、`routes/admin.go`、`AppSidebar.vue`、`router/index.ts`、admin API/i18n barrel 需手工合并。

### 2026-10-15 模型能力测评本地实测与适配修复

- 新增 Qoder / Trae OAuth 模型测评适配：复用真实平台转发链路，捕获 OpenAI 兼容 usage、逐题文本与耗时；Qoder 流式读增加 context 取消回收，Trae 支持指定 benchmark max tokens。
- 修复 hard-v1 随机素数题在合数模数下 `ModInverse` nil panic；runner 增加 `TryLock` 防止 cron 重入并发；usage 覆盖达到 80% 有效作答时计算效率分，执行失败不计入能力分分母。
- 本地 48 题实测：`glm-5.3-flash` 能力分 78.8 / 效率 95.03，`glm-5.3` 能力分 79.2 / 效率 97.53，`Qwen3.8-Flash` 能力分 75.4 / 效率 85.45；`MiniMax-M3` 因 Trae quota 为 0 仅 3/48 有效作答不排名，`DeepSeek-Flash` 因 Qoder `RESOURCE_LIMIT` 未完整执行。实测后 4 个临时计划均已停用，避免后续自动扣费。

### 2026-10-16 模型能力测评题库中文化

- `hard-v1` 全部 48 个动态题目提示改为中文题干与中文输出契约，技术标识、JSON 键和程序判定枚举保留英文以确保自动评分稳定。套件版本从 `1.0.0` 提升到 `1.1.0`，避免中英文题库历史曲线混算；评分版本保持 `1.0.0`。合并：`model_benchmark_suite.go`、`model_benchmark_test.go`。

### 2026-10-16 测评计划模型选择跟随模型清单

- 后端 `/admin/model-benchmarks/targets` 改为直接复用 `ModelManifestService.GetManifest()` 的模型清单结果，再过滤当前可测评账号，消除双份清单聚合逻辑。合并：`model_benchmark_service.go`、`wire*.go`。
- 前端「新建测评计划」模型下拉改为直接读取 `/admin/models/manifest` 全量模型清单，支持按模型 ID / 显示名搜索和平台过滤；同时保留可测评账号过滤，选中暂不支持模型时显示“当前模型暂无可测评账号”并阻止提交。新增 `ModelBenchmarkView.spec.ts` 覆盖全量清单来源、默认选中可测评模型、搜索与不支持提示。
