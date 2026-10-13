# sub2api-so 二开变更记录

> 规则：二开变更（无论是否经 `sub2api-upstream-sync` 技能）都必须记录于本文件，新条目追加到文件末尾，每条含：日期、功能描述、改动文件/模块、是否影响与上游合并。
> 基准：`UPSTREAM-BASE`（单行完整哈希）只在完整上游同步后前移；手动移植只记本文件、不改基准（唯一例外见 10-13 收口条目）。
> 整理记录：2026-10-12 与 2026-10-13 两次压缩，历史条目保留要点；逐文件合并明细、测试输出与真机排障过程见 git 历史（精简前 HEAD=`45cf4c6`）。

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

- **09-09~09-11 前端 UI/UX 重构**：确立 ACCOUNT_TABLE_UI_SPEC 基线（账号表格 dense、行高 48–54px、表单 label 左/控件右）；仪表盘 Bento 栅格与运维 SLA 色标；间距批量降档（118 文件 683 处）。合并：UI 层大面积二开，同步时多为三路合并。
- **09-14 仪表盘与用量统计**：用户/管理员仪表盘统一为 `GatewayDashboard`（scope prop，后端零改动）；使用记录拆出「用量统计」页（`/usage-stats`，无费用口径）。合并：`GatewayDashboard.vue` 高冲突。
- **09-17~09-22 仪表盘扩展**：全局默认时间范围「今天」；「Token 请求统计 · 今日」表；「API 密钥用量统计」板块（缓存命中率前端计算）；管理台移除五大数据区块（接口保留）。09-18 运维监控移除 7 个区块（仅 `OpsDashboard.vue`，上游可整文件覆盖）。
- **09-26→10-13 账号用量窗口（多次迭代，最终回归上游）**：09-26 微胶囊化 → 10-08 双行严整布局 → 10-09 去底色 → 10-12 恢复底色 → **10-13 回归上游 v0.2.15 原版**（每窗口纵排 `UsageProgressBar`，删除 `AccountUsagePopover` 与 238px 规格文档）。合并：`UsageProgressBar.vue` 与上游仅差 visibilitychange 一处；`AccountUsageCell.vue`、`CNProviderQuotaCell.vue` 与上游同构，冲突面小。

### 2026-10

- **10-03 管理控制台增强**：资源健康大盘（账号池/用户生态/密钥态势/在线新鲜度）、流量走势（Token/请求数双模式）、分组负载分布（`GroupUsageStats`，`include_group_stats`）。
- **10-06 / 10-11 离线打包**：`build-docker-tar.sh`、`Dockerfile.bundle`、`docker-compose.bundle.yml`（见「开发与部署环境」）；10-11 补齐 golang:1.27.2 镜像版本。
- **10-12 管理控制台布局**：admin 仪表盘「模型分布 / 分组使用分布」由 1:1 改为 3:1。合并：`GatewayDashboard.vue`。
- **10-12 三平台源码集成（workbuddy / qoder / trae）**：四批（登记与运营基建 → 网关/OAuth/运营/端点 → 回归收尾 → 前端接入）加修复系列。
  - **登记与菜单**：`domain/constants.go`、`platforms.go`、`service/domain_constants.go`、`provider_profile.go`（三平台 by_inbound + 仅 chat_completions 端点；账号 `api_protocol=adaptive` 收敛到 CC 路径，Claude Code 经既有 CC×anthropic 链可用）；侧边栏「账号管理」分组化（`/admin/accounts/p/:platform`，设置键 `account_menu_hidden_platforms`）。
  - **网关**：平台分叉收敛在 `openai_gateway_cc_pipeline.go` 的 sendCCUpstreamRequest（体改写/头注入/发送/响应包装，三条 raw CC 路径共用）与 `resolveCCFallbackTarget`、`rawChatCompletionsURL`；平台文件 `openai_gateway_{workbuddy,qoder,trae}.go`；错误分派 `openai_gateway_source_integrated_errors.go`（先平台分类，后通用 failover）。
  - **OAuth/凭据**：`{workbuddy,qoder,trae}_oauth_service.go`（设备/PKCE 会话 TTL 15min；刷新链；凭据归一化接入 `admin_account.go`；`*TokenRefresher` 到期前 30min）；`handler/admin/{workbuddy,qoder,trae}_oauth_handler.go`；trae 虚拟设备私钥存 `credentials.device_private_key`。
  - **运营任务**：`service/platform_ops.go`（任务描述/每日定点/`ops_state`/注册表）+ `{workbuddy,qoder,trae}_ops_service.go`；平台级调度设置存 `settings.platform_ops_config`（账号级实现已被替代）；端点 `/admin/platform-ops/platforms/:platform/config`（GET/PUT）、`/admin/platform-ops/accounts/:id/tasks/:task`；文档 `docs/platform-ops-tasks.md`。
  - **额度与降级**：快照 `extra["wb_quota"]`、`extra["qd_quota"]`、`extra["trae_quota"]`；workbuddy 402/14018、qoder 402 → `error(*_no_credit)` 停调度，签到/余额成功后自动恢复。
  - **测试与模型目录**：测试连接走真实转发路径（`testWorkbuddyAccountConnection` / `testQoderAccountConnection`）；默认测试模型登记在 provider profile（`DefaultWorkbuddyTestModel`、`DefaultQoderTestModel=Auto`、`DefaultTraeTestModel`）；qoder 对外模型清单改用官方显示名（`CatalogEntry.PublicID()`，账号 model_mapping 大小写敏感）；创建阶段可预览上游模型（`SyncUpstreamModelsPreview` 支持 credentials 直传）。
  - **错误处置修复**：qoder 10605 排队按 429 冷却不罚号（建流前/流内首帧/测试连接三路径）；单号池 429/排队不冻结唯一账号（`qoderSingleAccountPool`）；workbuddy WAF 403 软冷却不罚号；两条 CC 回退路径接入平台错误分类；workbuddy 上游指纹 11128 → `pkg/workbuddy/sanitize.go` 清洗（Claude Code 身份句、反探测数字等）；trae 上游必填 `app_version_code`（漂移修复）。
  - **前端**：`DeviceAuthFlow.vue`、`forms/{WorkBuddyForm,QoderForm,TraeForm}.vue`（建号可选模型白名单）、`PlatformOpsQuotaCell.vue`、`PlatformOpsTasksPanel.vue`（调度设置面板）、`api/admin/platformOps.ts`；建号默认并发/优先级 10/10。
  - **合并**：`openai_gateway_cc_pipeline.go`、`openai_gateway_chat_completions_raw.go`、`openai_gateway_service.go`、`account.go`、`admin_account.go`、`account_service.go`、`account_handler.go`、`token_refresh_service.go`、`upstream_models.go`、`provider_profile.go`、`wire.go`/`wire_gen.go`、`handler.go`、`routes/admin.go`、`gateway_handler.go`、`platformCatalog.builtin.json`、`AppSidebar.vue`、`AccountsView.vue`、`SettingsView.vue`、`adminSettings.ts`、设置管线需手工合并；`pkg/{trae,workbuddy,qoder}`、`*_ops_service.go`、`*_oauth_service.go`、`forms/*.vue` 为纯新增。
- **10-13 添加账号按平台菜单预选平台**：`CreateAccountModal.vue` 新增 `defaultPlatform` prop（经 `applyDefaultPlatform` 调用对应 `select()`），`AccountsView.vue` 透传路由平台。合并：两文件上游同名需手工合并。
- **10-13 账号管理菜单默认平台**：设置键 `account_menu_default_platform`（默认空）；侧边栏父项直达该平台列表；默认平台即使被黑名单隐藏也保留入口。合并：`domain_constants.go`、`settings_view.go`、`setting_*`、`AppSidebar.vue`、`SettingsView.vue` 等既有二开触点。
- **10-13 qoder/workbuddy 今日积分**：迁移 `242_so_account_daily_credits.sql`（`account_daily_credits`，主键 `(account_id, day)`，级联删除）；捕获 usage `credits`/`credit`，`RecordUsage` 落库前按日累计；列表「今日 N」胶囊（`WindowStats.credits`，批量查询无 N+1）。合并：`openai_gateway_usage.go`、`openai_gateway_response_handling.go`（首次触碰，各一处小改动）；迁移、仓库与测试为新增。
- **10-13 Antigravity 直接模型模式 + 测试/编辑修复**：空映射 = 直通（不再自动注入 `DefaultAntigravityModelMapping`；预设按钮与 `/admin/antigravity/model-mapping` 保留，存量映射账号不受影响）；测试请求去掉 `maxOutputTokens:1`（修复 thinking 模型「假成功 + 空白」）；测试弹窗对直接模式走上游 live 目录；编辑弹窗修复「保存后模型列表被清空」。合并：`account.go`、`antigravity_gateway_service.go`、`account_test_service.go`、`gemini_v1beta_handler.go`、`account_handler.go`、`EditAccountModal.vue`、`CreateAccountModal.vue` 需手工合并。
- **10-13 qoder 工具历史结构化直传 + 畸形回声兜底**：`pkg/qoder/baseprompt.go` 的 `StructuredToolHistoryEnabled`（env `QD_STRUCTURED_TOOL_HISTORY`=off/on/auto，auto 限 cn + 模型前缀白名单）；`pkg/qoder/sse.go` 的 `leakedMalformedEcho` 吞掉畸形回声（流末/流中/非流式三处）。CN 真机实测通过；INTL 因上游排队未实测（补验后再考虑放开 auto）。合并：纯二开文件。
- **10-13 Claude Haiku 5.5 全链路接入（issue #7919）**：模型识别（`pkg/claude/constants.go`、`effort_catalog.go` 的 `IsHaiku55`）；价卡（`model_prices_and_context_window.json` 含 >100k 绝对价，`pricing_service.go`/`billing_service.go` 三源阶梯一致，提交 `75c37c1`）；协议转换（`pkg/apicompat`：强制 tool_choice、采样锁定、effort=none 映射，提交 `01d1090`）；网关校验（`gateway_request.go`：adaptive thinking 必选、disabled 仅 high 及以下，提交 `a4f85c6`）；前端白名单与预设（`1c752c7`）。合并：上游同名文件需手工合并；`gateway_request_haiku55_test.go` 为新增。
- **10-13 GPT-6.1 Sol 订阅请求豁免长上下文附加费**（提交 `75c37c1`）：OAuth 类账号请求 GPT-6.1 Sol 不收模型价卡长上下文阶梯，API Key 账号仍按 API 价卡；豁免按**实际发送模型**判定（`openAISubscriptionLongContextExempt`），`CostInput.DisableModelLongContextPricing` 仅关阶梯、不改自定义区间价。合并：`billing_service.go`、`openai_gateway_usage.go` 需手工合并。
- **10-13 Responses input 校验失败降级 + codex 上下文 500k**（提交 `285f4a3`）：auto 模式下上游 400 拒绝结构化 Responses input（`isConvertedResponsesInputStringRejection`）时，自动降级到 raw chat completions 重试；GPT-6.1 Sol 生成上下文默认 500k（上游显式元数据优先）。合并：`openai_gateway_chat_completions.go`、`openai_codex_models_service.go` 需手工合并。
- **10-13 内容审核分组删除竞态修复**（提交 `674e3a9`）：删除分组时清理审核配置里的 `group_ids`（jsonb 原子改写，保留顺序与其它字段）；写入侧新增 `SetContentModerationConfig`，事务内锁存活分组；锁序统一为 groups → settings。合并：`group_repo.go`、`setting_repo.go`、`content_moderation.go` 需手工合并。
- **10-13 前端交互修复 4 项**（提交 `d5714be`、`1c752c7`）：分组名完整换行（`GroupBadge.vue` 新增 `truncate` prop）；后台标签页倒计时停摆（`UsageProgressBar.vue` 监听 `visibilitychange`）；输入法组合态方向键被抢（`Select.vue` `isComposing` 守卫）；渠道状态手动/自动刷新统一（`ChannelStatusV1View.vue`）。合并：上游同名文件需手工合并；新增 spec 无冲突面。
- **10-13 同步收口 v0.2.16（部分移植）**：上游区间 v0.2.15（`3a6fd1c9`）→ v0.2.16（NEW=`4d9ac3e4`，即 tag `01842576` 之后的 VERSION 同步提交）；BASE..NEW 共 51 个非合并提交、213 个文件。
  - **移植 17 提交**：第一批 11 个（`a56720855` `27de27ac6` `983cf297d` `d56f5e787` `26f70c6a8` `c4bdc4847` `8e3d75b57` `66b7ce08e` `071858126` `43768680d` `d01628975`，已入库 `75c37c1`～`45cf4c6`）；第二批 6 个（依赖 `5a25507dc` `a46249aac` `c15d858ec`，原暂缓 `fc82e8310` `2023c290e` `63800c50d`，未提交）。
  - **人工解决与适配**：`openai_gateway_forward.go`（`c15d858ec` 两处冲突：保留二开协议分流 switch，仅补 `startTime`）；`openai_gateway_workbuddy_test.go` 调用补 `time.Now()`（上游签名变更）。
  - **未纳入（永久放弃，勿再当作待同步项）**：支付 `c4224045d`；依赖补丁 `daa791574` `f7f1112cd`（区间净 diff 为零）；Antigravity `cfbbfd6b3`；分组多语言 `9a5133310` `8b74741ef`；Gemini 显式缓存/Vertex/AQ Key 共 17 个（`d5a802f84` `3bfd40f5a` `3abd64658` `64331a979` `81e6ec534` `2588675ce` `1c68f9996` `6f9b02f12` `05d7946af` `f0ee427f0` `4426593e9` `daebb7a80` `26b31a83e` `ba2a71733` `492c63059` `43a3c197c` `5753552b9`）；其余 10 个中等价值项（`f48866958` `69d6d106f` `ba280b3d5` `bd2f53b85` `29b2d721f` `ce6242fca` `0f189af8d` `c9e4b267b` `2bfc49477` `b669dc6da`）；版本提交 `4d9ac3e48`（VERSION 直接设定）。
  - **基准与版本**：`UPSTREAM-BASE` 前移至 `4d9ac3e4`，VERSION 置 0.2.16。二者必须同步，否则 `build-so.sh` 推导的二进制版本号与 VERSION 矛盾。这是 AGENTS.md「手动移植不前移基准」规则的一次性例外。被放弃提交的改动已计入基准、二开侧不含它们，后续三路合并会保持二开现状；只有与之相邻的后续上游改动可能冲突，需人工判断。
  - **合并**：第二批 12 个文件三路合并无冲突（`account.go`、`ratelimit_service.go`、`CreateAccountModal.vue`、`EditAccountModal.vue`、`EditAccountModal.spec.ts`、`zh/admin/accounts.ts`、`types/index.ts`、`openai_gateway_cc_pipeline.go`、`openai_gateway_chat_completions.go`、`openai_gateway_chat_completions_raw.go`、`openai_gateway_responses_chat_fallback.go`、`openai_gateway_count_tokens.go`）；删除行逐行核对可由所选上游提交解释。
  - **验证**：版本一致性 ✓；后端 `go build` / `go vet` / `go test -tags=unit ./...` 全绿（61 包）；前端 typecheck / vitest（2114 用例）/ build 全绿。未运行 `integration` 标签测试（需数据库）；`gofmt -l` 仅列 8 个既有 qoder/workbuddy 文件（非本次改动，待单独清理）。
