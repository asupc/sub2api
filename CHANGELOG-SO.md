# sub2api-so 二开变更记录

> 规则：二开变更（无论是否经 `sub2api-upstream-sync` 技能）都必须记录于本文件，新条目追加到文件末尾，每条含：日期、功能描述、改动文件/模块、是否影响与上游合并。
> 基准：`UPSTREAM-BASE`（单行完整哈希）只在完整上游同步后前移；手动移植只记本文件、不改基准（唯一例外见 10-13 收口条目）。
> 整理记录：2026-10-12 / 10-13 / 10-15 三次压缩，只保留功能事实、关键决策、合并冲突面与已知缺口；根因推导、测试输出、逐提交哈希与逐文件清单见 git 历史（本次压缩前 HEAD=`f5aa70c`，10-13 压缩前 HEAD=`45cf4c6`）。

## 平台移植源同步（PORTER-BASE 2026-10-16）

- **来源**：qoder2api-hub `b850c7e..5acef5e`（1 提交）；trae-workbuddy-switch `7364861..8edb86d`（21 提交）；workbuddy 无更新。
- **qoder（建议移植）**：导入 issue #33 图像单 part dict 修复——`flattenContentText` 现把单个 dict 形态的 content 当单元素列表处理，避免图像 dict 被 str() 字符串化；对应 commit `5acef5e`。
- **trae（可选）**：致 issue #28 variant 收敛——在 `trae.ParseVariant` 兼容 `trae_merged` / `global_trae_merged` 别名并映射到既有 TraeCN/GlobalTraeCode（端点/OAuthLine 不变），避免 cred 库/DB 契约漂移；commit `8edb86d` 等中的 merged variant 引入。UI/账户发现层（本地客户端遍历）未迁移，架构不适用。
- **评估未移植（记录避免重复建议）**：
  - Trae 90a84df latencyMs/totalMs 语义拆分（观测层重构，与 sub2api 自有日志链不同）。
  - Trae f273cd6 账号池×模型限流探测（新功能，sub2api 有自身模型管理/网关，无直接缺陷）。
  - Trae 412de09 账号池冷却原因门控（sub2api Trae 路径未使用 buddy pool，该缺陷不适用）。
  - Trae c8a492d/1286252 客户端一程序位收敛的 UI/本地账户发现（sub2api 无本地客户端遍历）。
- **改动文件**：`backend/internal/pkg/qoder/baseprompt.go`、`backend/internal/pkg/trae/variant.go`、`PORTER-BASE`、`CHANGELOG-SO.md`。
- **基准推进**：PORTER-BASE 的 qoder 行更新至 `5acef5e…`、trae 行更新至 `8edb86d…`（workbuddy 不变）。
- **与上游合并影响**：不涉及 sub2api-upstream-sync 的 UPSTREAM-BASE。

## 平台移植源同步（PORTER-BASE 2026-10-21，部分执行）

- **来源**：qoder2api-hub `5acef5e..91f17c2`（多提交）；trae-workbuddy-switch `8edb86d..cbd2bf9`（多提交）；workbuddy 无更新。
- **已移植（第一批）**：Qoder 风控身份形态对齐（issue #61/#64）——`fingerprint.DeriveID` 改为带连字符 UUID 形状（版本位 4/变体位 a，`QD_DEVICE_ID_LEGACY=1` 可保留旧 32-hex），`GenerateRequestID` 改为每请求纯随机 32-hex，新增 `DeriveMachineHostname`；`sign.CosySession` 新增 `MachineHostname`，SSE 路径在 `QD_COSY_OS_HEADERS` 默认开启时补发 `cosy-machineos` / `cosy-machinehostname`。对应黄金向量与测试已更新。
- **已移植（第二批）**：Qoder 出站体 #59/#60/#63 相关——`normalizeTools` 丢弃 `namespace`/`web_search` 工具类型；`BuildAgentChatBody` 不再重复下发 `image_urls`/`chat_context.imageUrls` 的整份 Base64；新增 `QD_MAX_BODY_MB`（默认 29MB）超限返回 `ErrPayloadTooLarge`。对应测试已更新。
- **已移植（第三批）**：Qoder 共享组织配额（issue #39/#67）——`QuotaUsage` 解析 `orgResourcePackage/sharedQuota/...`，新增 `Shared*` 与 `EffectiveRemain`（remain + 可用共享 remain，无限池不计入占位 remaining）；`QoderQuotaSnapshot` 新增共享字段并将 `Remain` 设为有效余额。对应测试 PASS。
- **改动文件**：`backend/internal/pkg/qoder/fingerprint.go`、`fingerprint_test.go`、`sign.go`、`sign_test.go`、`accounts_test.go`。
- **评估未移植/待后续批次**：Qoder 共享组织配额、工具调用/桥接稳定性、队列/冷却/4xx、模型目录与缓存、运营/UI 增强；Trae OAuth 区域与无客户端部署、Max Mode/上下文窗口、积分到期候选字段等仍待实施。PORTER-BASE 暂不推进，待全部选定项有明确结论后再更新。

## 当前状态（2026-10-13 核实）`

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

### 2026-10-16 测评计划独立菜单页、停用计划手动执行与多平台测评适配

- 新增独立菜单页 `/admin/benchmark-plans`（侧栏「测评计划」，`beaker` 图标）：创建表单 + 计划表格（名称/模型/账号/状态/下次运行/最近运行/操作）从 `ModelBenchmarkView` 拆出；原页面保留指标卡、排行、能力拆解、历史曲线与 Run 记录，并新增跳转入口。新增 `ModelBenchmarkPlansView.vue` 与 `ModelBenchmarkPlansView.spec.ts`。
- 创建表单精简：移除独立的「模型搜索」输入框与「平台」下拉，模型/账号下拉改用 `components/common/Select.vue`（搜索集成在下拉内，`searchable` 自动 >5 项启用）；模型下拉始终持有全量清单，保证选中项不因过滤而退化为 placeholder。
- 手动执行改为「确认弹窗 + 成功 toast」：`ConfirmDialog` 提示即将发起的上游请求规模（48 × 采样次数），成功后 `showSuccess` 提示已开始执行；删除计划同样换成 `ConfirmDialog`。手动执行按钮文案改为「手动执行」。
- 后端支持停用计划手动执行一次：新增 `manual_trigger_at` 一次性排队列，`QueueRun` 不再拒绝停用计划，runner 通过 `ClaimDuePlans` 的 `((enabled AND next_run_at<=now) OR manual_trigger_at IS NOT NULL)` 谓词原子领取并在同事务清空标记；`enabled` 语义保持纯净（手动执行不会顺带启用 cron）。迁移 `244_so_model_benchmark_manual_trigger.sql` 同时把 `idx_model_benchmark_plans_due` 与 `idx_model_benchmark_plans_claim_recovery` 的 `WHERE` 条件去掉 `enabled`（停用计划被手动触发后崩溃同样需要租约恢复），并新增 `idx_model_benchmark_plans_manual`。已领取即清空，崩溃不重放该次触发（一次性语义）。
- 修复 WorkBuddy 无法创建测评计划：`IsSupportedModelBenchmarkTarget` 的源码集成平台分支此前只列了 Qoder/Trae，漏掉 WorkBuddy（`IsSourceIntegratedPlatform` 随后一律拒绝），其下游 `executeSourceIntegrated` 分派同样只有两个平台。现按 `IsSourceIntegratedPlatform` 统一覆盖三平台 OAuth（出站占位 token 由 `GetOpenAIProtocolAPIKey` 按 trae=jwt、workbuddy/qoder=access_token 分派）。
- 新增 OpenAI OAuth/Codex 测评支持：`IsSupportedModelBenchmarkTarget` 允许 `PlatformOpenAI && IsOpenAIOAuthLike()`（含 setup-token），凭据判定为 Agent Identity 或非空 access token；执行分派复用 `AccountTestService.RunBenchmarkPrompt` 的 Responses 流式通道。`processOpenAIStream` 补齐两处缺口：终态 `response.completed` 的 `response.usage` 上报为 `benchmark_usage` 事件（此前只发 content，测评拿不到 Token）；无 `output_text.delta` 时用 `response.output` 兜底取文，避免整题答案被当空内容。
- OpenAI OAuth 测评的两个端点约束处理：`max_output_tokens` 属 `openAICodexOAuthUnsupportedFields`（ChatGPT 内部 Codex 端点不接受，转发路径会剥离而测试路径不经该 transform，下发会被 400），故仅对非 OAuth 路径下发；`instructions` 在测评上下文换为极简 JSON 契约（`modelBenchmarkInstructions`），避免 Codex base prompt 把「只返回一个 JSON 对象」的评测要求顶掉、抬高 `format_error` 比例。
- 测评不再改写生产账号状态：`testOpenAIAccountConnection` 的 429 冷却（`reconcileOpenAI429State`）与 401 永久错误（`SetError`）在 benchmark 上下文下跳过——48 题串行批任务中单题失败不应把账号打入冷却或 error 态。
- `RunBenchmarkPrompt` 的题目此前在 OpenAI Responses 路径被忽略（`createOpenAITestPayload` 硬编码 `"hi"`）；现新增 `createOpenAITestPayloadWithPrompt` 支持传入真实题目与输出预算，普通探测路径保持 `"hi"` + 不下发 `max_output_tokens` 的既有语义。Qoder/Trae 的 benchmark max tokens 硬编码 4096 统一为 `defaultModelBenchmarkMaxOutputTokens` 常量。
- 模型清单页（`/admin/models`）新增「最近测试分数」列：模型卡片统计区展示最近一次成功/部分成功 run 的能力分（未测评显示「未测评」），tooltip 标注测评时间与套件版本；新增排序切换（默认按分数降序，未测评排最后）。后端 `ModelManifestItem` 增加 `benchmark_score` / `benchmark_suite_version` / `benchmark_scored_at`，由 `ModelBenchmarkRepository.LatestScores`（`DISTINCT ON (model_id) ... ORDER BY model_id, created_at DESC`）提供，经窄接口 `ModelManifestBenchmarkScoreSource` 在 `ProvideModelBenchmarkService` 内装配注入。**取数刻意不做套件版本过滤**：目的是展示「最近一次」分数，若按当前 `1.1.0` 过滤会让中文化前（`1.0.0`）的历史分数全部不可见；跨版本可比性由前端版本标注提示。
- 验证：后端 `go build ./...`、`go test -tags=unit ./internal/service ./internal/repository ./migrations ./internal/handler/admin` 通过；新增 `TestModelBenchmarkRegistrySupportsIntegratedPlatformsAndOpenAIOAuth`（9 组目标矩阵）、`TestAccountTestService_Benchmark*`（题目下发 / max_output_tokens 取舍 / usage 上报 / 401 不改账号状态）、`TestModelBenchmarkQueueRun*`（停用计划可排队且不被启用）、`TestModelBenchmarkManualTriggerMigrationIsIdempotent`。前端 vitest 288 文件 / 2128 用例、`typecheck`、`lint:check`、`build`（含 i18n key 完整性）通过。迁移 244 已在本地库应用并核对列与索引。
- 合并：新增文件为 `ModelBenchmarkPlansView.vue`（+ spec）、`244_so_model_benchmark_manual_trigger.sql`、`model_benchmark_queue_run_test.go`；改动面 `model_benchmark_executor.go`、`model_benchmark_types.go`、`model_benchmark_service.go`、`model_benchmark_repo.go`、`account_test_service.go`、`domain_constants.go`、`model_manifest_service.go`、`wire.go`、`model_benchmark_test.go`、`account_test_service_openai_test.go`、迁移测试、`ModelBenchmarkView.vue`、`ModelManifestView.vue`、`AppSidebar.vue`、`router/index.ts`、admin API 与 i18n barrel 属手工合并面。
- 已知边界：本地库 openai 平台账号（21 apikey + 511 oauth）均为软删除状态，OpenAI 平台端到端验证需在账号未删除的环境执行；WorkBuddy 计划已可通过目标矩阵（本地账号 #568 具备 access_token）。

### 2026-10-16 模型能力测评多计划并行执行

- runner 从「每 tick 领取 1 个 plan 串行执行」改为「每 tick 领取全部 due plan（limit 100，防御性上限）并行执行」：`ClaimDuePlans` 领取后逐 plan 起 goroutine，`WaitGroup` 等待全部完成再释放 tick 锁（保持 tick 不重叠语义）。每个 plan 独立占用 45 分钟执行预算（`context.WithTimeout` 移入 goroutine 内），不因并行共享而缩水。领取本身仍是 `FOR UPDATE SKIP LOCKED` 原子领取，多实例安全不变。
- 明确决策：同账号多 plan 并行不做互斥——不同模型通常绑不同账号，无限流冲突；偶发同账号由计划排期自行规避。原「全局并发 1」仅为 MVP 成本兜底，非机制限制。
- `runScheduled` 的 10 秒固定延迟抽为包级变量 `modelBenchmarkRunnerTickDelay`，测试中缩短为 1ms。
- 设计文档 `docs/MODEL_CAPABILITY_BENCHMARK_PLAN.md` §7.2 与风险表同步更新（并发 1 → 全部并行，风险表移除「全局并发 1」防线）。
- 验证：Docker 容器 `go test -tags=unit -count=1 ./internal/service/` 全绿（194.9s）；新增 `model_benchmark_runner_test.go`（全部 due plan 均执行且仅一次 / 无 due plan 不产生执行 / 领取上限覆盖全部 due plan）。
- 合并：`model_benchmark_runner.go`、`model_benchmark_runner_test.go`（新增）、`docs/MODEL_CAPABILITY_BENCHMARK_PLAN.md`。不影响与上游合并（上游无此模块）。

### 2026-10-16 模型测评 UI 重构与双标题消除

- **双标题消除**：`ModelBenchmarkPlansView.vue`（测评计划）与 `ModelBenchmarkView.vue`（模型能力测评）移除页内手写的冗余 `<h1>` 标题与副标题，由系统的 `AppHeader` 统一接管路由元标题，解决一屏重复多级大标题的视觉问题。
- **全宽表格卡片重构**：`ModelBenchmarkPlansView.vue` 废除原左侧 42% 常驻表单 + 右侧 58% 挤压表格的双栏布局，改造为全宽卡片表格；计划名称、模型、账号、Cron、状态、下次运行、最近运行及操作列舒展对齐，操作按钮横向整齐排列。
- **新增测评计划模态框编辑与创建**：新建表单与编辑表单收进 `BaseDialog` 模态弹窗；操作列补充「编辑」按钮，支持回显并修改已有计划的所有配置项（名称、模型、账号、Cron、采样次数、max tokens、超时时间、是否启用），提交调用现有 `updatePlan` 接口更新并刷新列表。
- **验证**：前端 vitest、typecheck、i18n completeness 检查及全量生产打包 `pnpm build` 均 100% 通过。
- **合并**：改动面仅在 `frontend/src/views/admin/ModelBenchmarkPlansView.vue`、`ModelBenchmarkView.vue`、`i18n/locales/zh/admin/modelBenchmark.ts` 及单测文件；上游无此模块，不影响与上游合并。

### 2026-10-16 修复 constrained-permutations 题目期望答案公式错误

- **缺陷**：hard-v1 套件的 `constrained-permutations` 族题面为「前 k 个命名元素都必须保持原位置」，正确答案应为 (n-k)!（其余 n-k 个元素自由排列），但生成器误算 n!/k!（`model_benchmark_suite.go` 中 `for i := fixed+1; i <= n` 连乘）——该公式对应的是「保持原相对顺序」约束。导致本地 10 个 run × 2 variant 中所有正常执行的作答（模型均答出正确的 (n−k)!：120/720/5040/40320/362880）全部被 integer checker 误判 0 分。
- **修复**：连乘改为 `for i := 2; i <= n-fixed`（即 (n-k)!），题面与 salt 不变（同 plan/run 的 prompt_hash 不变，仅判分期望修正）；`ModelBenchmarkSuiteVersion` 1.1.0 → 1.2.0（题目语义变化，历史 run 分数与新题不可比，靠 suite_version 区分；模型清单页「最近测试分数」刻意不过滤版本，行为不受影响）。
- **回归测试**：新增 `TestModelBenchmarkConstrainedPermutationsAnswer`——遍历 200 个 seed 生成的实例，从题面提取 n、k 独立验证 expected == (n-k)!；`TestModelBenchmarkSuiteGeneratesHardV1` 的版本断言同步 1.2.0。
- **验证**：Docker 容器 `go test -tags=unit -count=1 -run 'TestModelBenchmark' ./internal/service/` 全绿（含新增回归测试）。
- **合并**：改动面 `model_benchmark_suite.go`、`model_benchmark_test.go`；上游无此模块，不影响与上游合并。历史 run 的该题 0 分记录不回溯重算，需要干净分数请重跑计划。

### 2026-10-16 模型测评全部题型与判分正确性核实

- **范围**：核实 `hard-v1` 全部 24 题型、2 变体（48 题），保留本轮开始前的排列公式修复与已有工作区变更；记录见 `docs/MODEL_CAPABILITY_BENCHMARK_QUESTION_AUDIT.md`。
- **错误答案**：区间合并改为真正的最优区间 DP（n=7：168→74）；容斥交集由乘积改为 lcm（1..70 对 6/7/15：46→48）；持钥推理移除随机期望，按条件唯一推导。
- **题意与能力考察**：补齐 KMP pi 定义、模逆元规范代表、令牌不足拒绝策略、数组形状、调度下标与整数数组字典序；补全布尔赋值、不变量和数值稳定性题的约束与选项；去除并发题/区间下标的答案提示，清理顺序题改为推导 Go defer 逆序。
- **判分**：普通题按最后一个完整 JSON 对象提取答案；用 json.Number 保留整数精度，支持等价小数/指数整数和嵌套数值数组，区分字符串/数值；严格格式题检查整个响应、重复键和完整结构，禁止额外字段及部分得分；多字段错误原因补齐。
- **版本**：题库 `1.2.0→1.3.0`、评分 `1.0.0→1.1.0`；run 写入实际执行版本，避免旧计划把新结果标为旧版。历史 run 不自动改分，不自动调用模型重跑。
- **模块**：`backend/internal/service/model_benchmark_{suite,types,service}.go`、既有 `model_benchmark_test.go` 与新增 `model_benchmark_audit_test.go`；题库方案和核实记录同步更新；`.gitignore` 对核实记录单独放行以便纳入版本管理。
- **验证**：新增从题面独立复算的 24×2×128=6144 个实例、33 个判分样例、区间全部规模、循环不变量与 256-bit 浮点参考边界。业务层、管理接口、仓储单元测试和 `go build ./...` 均通过；`git diff --check` 通过，golangci-lint 未安装未运行。详细命令状态见核实记录；真实模型与生产验收未执行。
- **合并影响**：测评纯二开模块及文档；无 schema/迁移变更，不推进 `UPSTREAM-BASE`，不修改上游只读仓库。

### 2026-10-16 模型能力测评 v2：56 题型 / 112 题与公共比较批次

- **题库扩充**：`hard-v1` 题库 `2.0.0`、评分 `2.0.0`，从 24 题型/48 题扩展为 56 题型/112 题。新增抽象归纳、材料与数据、信息充分性、因果与概率、代码纠错和复杂规划 32 个原创题型；原核心题型的图、字符串、权重、并发对照、逻辑选项和输入结构也动态化。
- **可比性**：新增 `comparison_key`（默认 `baseline-v2`）；题目由比较键 + 采样序号 + 题型 + 变体生成，不同模型同键同卷。run 持久化比较键、输出预算、单题超时、采样数和质量指标；旧 run 默认 `legacy`，不回溯改分。
- **测量质量**：有效完成率、格式合规率、预算耗尽、能力域覆盖与排名资格分开展示；默认排行要求同版本/同卷/同预算/同超时/同采样、完成率 >=90%、各域覆盖 >=80%、至少 3 次，并显示分数标准差。OpenAI OAuth/Codex 的 provider-managed 预算结果仅诊断。
- **判分**：新增 witness checker，验证反例、最小不一致子集、资源分配和路线方案的可行性与最优值，接受等价正确解；直连与流式路径识别 length/max_tokens/content_filter/incomplete，截断不按答错计。
- **执行与数据**：run 总预算、claim 租约和收尾超时按 112 题 × 采样 × 单题超时计算；新增非破坏性迁移 `245_so_model_benchmark_comparison.sql`，只加比较键、预算快照、质量 JSON、索引和数值精度。WorkBuddy/Qoder/Trae 测试路径传递计划输出预算。
- **模块**：`model_benchmark_{suite,extended_suite,types,service,executor,runner}.go`、repository/handler、`account_test_service.go` 与三平台网关、245 迁移、前端测评 API/两页面/i18n/测试、方案与核实文档。
- **验证**：原 24 + 新增 32 题型共 14336 个独立复算实例 PASS；受影响后端 unit、迁移测试、`go build ./...`、隔离 PostgreSQL 迁移/仓储/排行/长租约测试、前端 8 个 Vitest、typecheck、指定 ESLint、`pnpm build`、Mocked API 桌面/移动浏览器验收均 PASS；`git diff --check` PASS。golangci-lint 未安装未运行；真实模型、难度校准和生产部署 NOT_RUN。
- **合并影响**：模型测评为二开模块；新增迁移和 account 测试路径有小段上游冲突面，不推进 `UPSTREAM-BASE`，不修改上游只读仓库。

### 2026-10-16 测评计划页错误提示修复（[object Object]）

- **问题**：测评计划有任务执行时编辑保存，后端返回 409 "model benchmark plan is running or not found"，但前端 catch 用 `error instanceof Error ? error.message : String(error)`，而 API 拦截器 reject 的是普通对象（非 Error 实例），`String()` 显示为 `[object Object]`。
- **修复**：`ModelBenchmarkPlansView.vue` 全部 5 处 catch 改用项目统一的 `extractApiErrorMessage`，正确显示后端 message（含加载、编辑保存、启停、手动执行、删除）。
- **模块**：`frontend/src/views/admin/ModelBenchmarkPlansView.vue`；纯前端展示层，无后端/迁移变更。
- **验证**：该页 Vitest（4 用例）与 `pnpm typecheck` 通过。
- **合并影响**：无（二开页面，上游无此文件）。

### 2026-10-16 修复 OpenCode Go 模型测评 MissingSessionID 400

- **问题**：模型测评（benchmark executor）对 OpenCode Go 账号走通用 Chat Completions 直连路径，该路径既不带 `x-opencode-session` 头（官方上游 2026-09-05 起强制，缺失即 400 `MissingSessionID`），也不收敛规范客户端 UA，且不按模型分流——muse-spark/gpt/grok 系列按官方端点表仅在 Responses 端点提供，直连 `/chat/completions` 本身就选错端点。账号连接测试路径此前已修复（协议路由 + UA + session 头齐全），测评路径漏同步。
- **修复**：`ModelBenchmarkExecutor.Execute` 将 OpenCode Go 账号并入流式捕获通道（`executeSourceIntegrated` → `AccountTestService.RunBenchmarkPrompt`），即真实转发路径：按模型协议路由（muse-spark→Responses 等）、UA 收敛、session 头生成全部复用既有逻辑。其余平台（kimi/zhipu/deepseek/minimax/cline 等）仍走通用直连。
- **模块**：`backend/internal/service/model_benchmark_executor.go`（分流条件 + 注释）、`model_benchmark_test.go`（新增 `TestModelBenchmarkOpenCodeGoUsesStreamingChannel` 回归测试）。
- **验证**：`internal/service` 全包 `go test -tags=unit` 通过（195s）。
- **合并影响**：模型测评为二开模块；`model_benchmark_test.go` 上游无此文件。不推进 `UPSTREAM-BASE`。

### 2026-10-16 测评计划表单布局统一

- **调整**：新建/编辑测评计划弹窗中,名称与公共试卷批次原先独占整行(公共试卷批次 label 间距与帮助文字样式也不同),观感与双列区不一致;现将两字段并入双列 grid,表单 8 字段两两并排(名称/模型、账号/Cron、采样数/输出预算、单题超时/公共试卷批次),公共试卷批次帮助文字降为 11px 小字(与模型平台提示同款式)。
- **模块**：`frontend/src/views/admin/ModelBenchmarkPlansView.vue`;纯模板布局调整,无逻辑/接口变更。
- **验证**：该页 Vitest(4 用例)与 `pnpm typecheck` 通过。
- **合并影响**：无(二开页面,上游无此文件)。

### 2026-10-16 模型测评排名规则调整（跑过即上榜 / 题库可切换 / 取最新成绩）

- **调整**：排名接口去掉全部门槛——ranking_eligible 资格、至少 3 次执行、同批次（comparison_key）与同预算三元组精确匹配、AVG+标准差聚合；改为按题库三元组（suite_id/suite_version/scoring_version）过滤，每模型取最新一次 success/partial run 的成绩（窗口函数 ROW_NUMBER，同分平局按 run_count DESC、model_id ASC）。`ranking_eligible` 仍在 run 聚合时计算并在执行记录中展示，转为纯诊断标注。provider-managed budget（max_output_tokens=0）run 全部入榜，前端行内以「由上游管理」徽标提示可比性。
- **新接口**：`GET /admin/model-benchmarks/rankings/suites`（列出存在可排名执行的题库版本组合）；`GET /rankings` 参数改为 `?suite_id&suite_version&scoring_version&limit`（缺省编译期最新题库），旧参数静默忽略。API 契约变更：分数均值→最新值、删 `score_stddev`、`run_count` 语义改为该题库累计合格执行次数。
- **前端**：排行卡片新增题库下拉（内置套件标「最新」，历史题库按最近执行时间排列，切换只刷新排名区）；批次/预算筛选表单只作用于执行记录与历史曲线，不影响排名；删 ±标准差展示。
- **模块**：`backend/internal/service/model_benchmark_types.go`（Ranking 删 ScoreStddev、接口签名、新增 RankingSuite DTO 与 RankingFilters）、`model_benchmark_service.go`（合并 ListRankings/ListComparisonRankings、新增 ListRankingSuites）、`backend/internal/repository/model_benchmark_repo.go`（SQL 重写 + ListRankingSuites）、`backend/internal/handler/admin/model_benchmark_handler.go`、`backend/internal/server/routes/admin.go`；`frontend/src/api/admin/modelBenchmarks.ts`、`views/admin/ModelBenchmarkView.vue`、`i18n/locales/zh/admin/modelBenchmark.ts`；测试 `model_benchmark_comparison_test.go`（断言重写 + 历史题库覆盖）、`ModelBenchmarkView.spec.ts`（mock 补 listRankingSuites、用例重写）；文档 `docs/MODEL_CAPABILITY_BENCHMARK_PLAN.md`（5 处规则描述 + 修订记录）、openspec spec.md scenario 修订。无数据库迁移。
- **验证**：后端 Docker golang:1.27.2 `go build` + `go test -tags=unit`（repository/service/handler/server/routes 全绿）；前端 Vitest（4 用例）+ typecheck + build（含 i18n 完整性检查）通过。
- **合并影响**：模型测评为二开模块，上游无对应文件；不推进 `UPSTREAM-BASE`。

### 2026-10-16 模型能力测评全景重构（以模型为中心 / 双视角分离 / 专属透视抽屉）

- **背景与痛点**：原有评测大盘以低频批次运维表单占据首屏黄金位置；多维能力柱状图硬编码取自全局最后一次 Run，与选定模型脱节误导；历史折线图受后端强制默认值锁死导致历史点缺失；列表页无法按模型过滤与下钻。
- **架构重塑**：
  - **双视角分离**：引入 Tab 导航，解耦「模型能力中心（Leaderboard & Capability Matrix，核心视角）」与「评测执行流水（Runs Table，运维流水视角）」。
  - **模型全景透视抽屉 (Model Detail Drawer)**：榜单支持实时模糊搜索与多指标排序，点击任意模型行/卡片立即滑出全景抽屉，聚合展示该模型的：最新评分指标、9 维能力画像雷达图、历史评分/延迟/Token 演进折线图、该模型专属历史 Runs 列表以及快捷【立即评测此模型】。
  - **折线图与雷达图**：开发轻量纯 SVG 响应式雷达图（`RadarChart.vue`）与交互式折线走势图（`TrendLineChart.vue`，支持指标切换与时间轴 Tooltip），零重型第三方库负担。
  - **单次 Run 深度检视**：单次评测详情抽取为独立模态框（`RunDetailModal.vue`），支持题目用例按状态/类别筛选并展开完整 Prompt 与模型思维链推理（Reasoning）。
- **后端适配**：
  - `ModelBenchmarkRanking` 补充 `category_scores` 字段，榜单查询一次性带出 9 维能力分布。
  - 放宽 `ListHistory` 逻辑：当指定 `model_id` 时不强制锁死默认批次与预算条件，使得能获取单模型的全量演进历史。
  - 新增单模型全景透视接口：`GET /admin/model-benchmarks/models/:model_id/overview`（返回排名、最新Run、关联计划、历史趋势、最近10次Runs）。
- **模块**：
  - 后端：`internal/service/model_benchmark_types.go`、`internal/service/model_benchmark_service.go`、`internal/repository/model_benchmark_repo.go`、`internal/handler/admin/model_benchmark_handler.go`、`internal/server/routes/admin.go`。
  - 前端：`api/admin/modelBenchmarks.ts`、`views/admin/ModelBenchmarkView.vue`、`views/admin/components/benchmark/{ModelLeaderboard,ModelDetailDrawer,RadarChart,TrendLineChart,RunsTable,RunDetailModal}.vue`、`i18n/locales/zh/admin/modelBenchmark.ts`。
  - 测试：更新 `ModelBenchmarkView.spec.ts` 适配新架构与抽屉下钻用例。
- **验证**：后端 Docker 单元测试通过；前端 `pnpm check:i18n` + `pnpm typecheck` + `pnpm test:run` + `pnpm build` 全绿。
- **合并影响**：模型测评为二开模块，上游无对应文件；不推进 `UPSTREAM-BASE`。


### 2026-10-16 测评计划去账号化：改走公共网关回环

- **背景**：测评目标矩阵按平台逐个适配（此前 WorkBuddy/Antigravity 相继漏列，"有账号选不了"反复出现）。用户决策：计划不再绑定账号，直接走公共网关由调度统一选号，一次性覆盖全部平台。
- **架构**：executor 不再直连上游、不再复用账号测试通道，改为 HTTP 回环打自身网关 `http://127.0.0.1:{server.port}/v1/chat/completions`（Bearer 测评 Key）——认证、分组白名单、复合路由、调度、平台协议转换、流式 usage 标准化与计费全部走真实链路。载荷为标准 OpenAI Chat Completions（真实题目、temperature=0、max_tokens、stream + include_usage）；SSE 聚合 delta 文本 / finish_reason / usage。内部函数调用方案已评估并否决：转发与 failover 编排深度耦合 `*gin.Context`，复刻即双份链路。
- **测评身份设置**：新增 `model_benchmark_settings` 设置项（Rectifier 四步模式），存回环认证用 API Key ID；保存校验 Key 存在、启用、未过期且已绑定分组（未分组 Key 会被网关 `RequireGroupAssignment` 拦截）。API Key 明文存储可经 `GetKeyAndOwnerID` 取回。新端点 `GET/PUT /admin/model-benchmarks/settings`。
- **目标矩阵改为模型级**：`IsSupportedModelBenchmarkTarget`（按账号）删除，替换为 `isModelBenchmarkSupportedModel`（有可调度账号 + 非图像/视频生成模型）。`ListTargets` 不再逐账号过滤，任何平台的模型均可测评。
- **数据库**：迁移 `246_so_model_benchmark_gateway_mode.sql`——`plans.account_id`（原带 ON DELETE CASCADE 外键，删账号会级联删计划，隐患一并消除）与索引、`runs.account_id` 删除；`upstream_model_id` 语义改为网关响应回写模型名（请求模型名兜底）。
- **代价说明（真实链路）**：测评流量按所选 Key 正常计费、记用量、占用户并发槽与 RPM；建议专建测评分组 + Key。多计划并行时共享该用户的并发/RPM 配额。
- **旧通道代码保留未删**：`AccountTestService.RunBenchmarkPrompt` 与各平台测试链路的 benchmark 感知载荷/事件（qoder/trae/openai-oauth 适配）解除调用后保留——普通"测试连接"路径共存无冲突，且属上游同步敏感区，不做无谓删改。
- **模块**：`backend/migrations/246_*.sql`、`internal/repository/model_benchmark_repo.go`、`internal/service/model_benchmark_{types,service,executor}.go`、`setting_features.go`、`settings_view.go`、`domain_constants.go`、`wire.go`、`cmd/server/wire_gen.go`、`internal/handler/admin/model_benchmark_handler.go`、`internal/server/routes/admin.go`；前端 `api/admin/modelBenchmarks.ts`、`views/admin/ModelBenchmarkPlansView.vue`（删账号列/下拉/校验，新增测评设置卡片：分组→Key 两级下拉）、`components/benchmark/RunDetailModal.vue`（删账号徽标）、`i18n/locales/zh/admin/modelBenchmark.ts`；测试 `model_benchmark_test.go`（回环 httptest 网关 + 模型级矩阵 + 设置校验）、`model_benchmark_execution_budget_test.go`、`model_benchmark_{audit,queue_run,runner}_test.go`、`model_benchmark_migration_test.go`、`model_benchmark_comparison_test.go`、`model_benchmark_repo_test.go`、`ModelBenchmarkPlansView.spec.ts`、`ModelBenchmarkView.spec.ts`。
- **验证**：后端 Docker golang:1.27.2 `go build ./...`、`go vet -tags=unit ./...`、`go test -tags=unit`（service/repository/migrations/handler 全绿；config 包 3 个用例为容器环境既有失败，stash 基线复现确认与本次无关）；前端全量 Vitest 288 文件 / 2131 用例全过（exit 1 来自 `KeyUsageView.spec.ts` 自身动画帧 teardown 竞态的 2 个 unhandled error，与测评文件零依赖、单跑该 spec 即通过，属既有问题）+ typecheck + build（含 i18n 完整性检查）通过；迁移 246 已在本地库应用并核对（两表 account_id 列与索引已消除）。
- **合并影响**：模型测评为二开模块，上游无对应文件；wire_gen.go 为生成物（provider 签名收窄后同步更新）；不推进 `UPSTREAM-BASE`。

### 2026-10-16 复合分组同族多平台统一调度（所有权多平台从 fail-closed 改为合并池）

- **背景**：同一公开模型名被多个平台的可调度账号在 `model_mapping` 显式认领时（生产实例：`deepseek-v4-pro` 同时映射于 qoder 与 workbuddy 账号，两侧上游各自健康），上游设计 fail-closed 拒绝（"model is exposed by multiple provider platforms" → 503 no available accounts，请求不出网关），跨供应商容量无法互备。
- **语义变更**：所有权解析（`resolveCompositeModelOwnership`）返回排序去重的平台集合；集合成员同属一个网关族（`domain.UsesOpenAIGateway` 二分，typesafe 属 GatewayHandler 族）时，两条调度链的候选池合并为全部认领平台的认领账号，负载均衡与账号级 failover 跨平台生效；**跨族歧义保留 fail-closed**（一条请求只能进一条协议链），Reason 追加 "configure an explicit composite route" 提示并打 `composite_model_ownership_cross_family` 告警日志。显式路由 > 所有权 > 前缀探测器的优先级不变。
- **实现**：
  - 解析层：`CompositeModelOwnership` 删 `TargetPlatform/Ambiguous` 改 `TargetPlatforms []string`；`CompositeRouteDecision` 新增 `TargetPlatforms`（单平台决策仍只填 `TargetPlatform`，既有消费者零改动）；ctx 新增集合 key `ResolvedTargetPlatforms`，多平台时**不写单值**。
  - 回填（计费零改动核心）：新增 `ContextWithSelectedAccountPlatform`，在 11 处选号成功点（Gateway 族 8 处 ProfitGate 相邻 + OpenAI 族 Responses/Messages/chat_completions/WS）把实际账号平台回填为单值 `ResolvedTargetPlatform`——`QuotaPlatform`、渠道费率、ops 平台归因等 20+ 处单值读取自动正确；failover 重选每次重写。账号不在候选集合内时不回填（防假归属）。
  - Gateway 族池：`resolveSchedulingPlatforms`/`listSchedulableAccountsForPlatforms`（逐平台复用既有单平台快照桶后按账号 ID 合并去重，**快照桶/Mode 零结构改动**）；`isAccountAllowedForPlatforms`；`SelectAccountWithLoadAwareness`/`SelectAccountForModelWithExclusions`/`selectAccountForModelWithPlatform` 平台参数切片化；粘性判定与模型路由（`anyModelRoutingAppliesToPlatforms` 并集）随集合泛化。
  - OpenAI 族池：新增 `openAIPlatformConstraint` 贯穿 eligibility/recheck/legacy 链；`SelectAccountWithSchedulerForCapability` 的 `platformOverride ...string` 语义升级为"全部值=候选集"（既有单值调用点零改动）；`OpenAIAccountScheduleRequest` 增 `Platforms`+`AllowsPlatform`；`listSchedulableAccountsForConstraint` 多平台合并。归属闸门（`source==account_model` 仅显式认领账号可服务）按账号判定，多平台池天然延续。
  - 入口：`getGroupPlatform` 集合时返回排序首成员作族代表（仅 handler 链分发，不用于池过滤）；grok 专属门（voice/realtime/web_search/x_search/video edit/extension）与 openai-only 门（embeddings）改交集判定 `resolvedPlatformSetIntersects`；images 多平台时交集恰 {openai}/{grok} 才放行、含两者保守 404；gemini v1beta 中间件集合含 gemini 时塌缩单值。handler 前置门（`ensureCompositeTargetPlatform`/`compositeTargetPlatformAllowed`/`compositeTargetPlatformResolved`）支持集合（探测器不得覆盖集合、白名单交集放行）；WS 守卫集合全部成员 ∈ {openai,grok} 才放行；grok/CN dispatch 豁免改"集合含任一成员"。
  - 计费准入：`CheckBillingEligibility` 内部在"平台为空且 ctx 携带集合"时按 `checkUserPlatformQuotaEligibilityAnyOf` 预检——任一候选平台有余量即放行、全部拒绝才拒绝（调用点零改动）；计量仍按回填后的实际平台，`RecordUsage` 既有 composite→account.Platform 兜底保留。
- **已知限制**：/v1beta 入口多平台集合塌缩为 gemini 单池；Codex 能力广告（`openai_codex_model_metadata`）维持跨平台别名 fail-closed（仅 text、不广告推理等级）；在途预留估算对多平台 fail-open（空平台桶→无账号基准）；OpenAI WS 多平台集合 transport 放宽为 Any 由调度器按账号能力匹配。
- **验证**：Docker golang:1.27.2 `go build ./...` 通过；`go test -tags=unit` 全绿（service 195s + handler/admin/server/routes/repository）。新增测试：`TestCompositeRouteResolverMergesSameFamilyMultiPlatformOwnership` / `...DoesNotGuessCrossFamilyAccountOwnership`（解析器）、`TestCompositeTargetPlatformMiddlewareMultiPlatformOwnershipWritesSetNotSingle` / `TestGetGroupPlatformMultiPlatformSetReturnsFamilyRepresentative` / `TestResolvedPlatformSetIntersectsGatesMultiPlatformRequests`（入口）、`TestGatewayService_SelectAccountForModelWithExclusions_CompositeMultiPlatformPool`（Gateway 族跨平台 failover+归属闸门）、`TestOpenAIGatewayService_SelectAccountWithScheduler_MultiPlatformUnifiedPool`（OpenAI 族）、`TestContextWithSelectedAccountPlatformBackfillsActualPlatform`（回填）。生产冒烟（部署后执行）：deepseek-v4-pro 双平台负载均衡、单平台故障转移、粘性、usage_log 平台归属、跨族 503+提示日志。
- **合并影响**：composite 解析/调度链为二开密集区（`gateway_scheduling.go`/`openai_account_scheduler.go`/`openai_gateway_scheduling.go` 与上游高冲突面重合），上游同步时按本条目语义重放；不推进 `UPSTREAM-BASE`。

### 2026-10-16 三平台重新授权授权 URL / 模型测评时间展示 / Antigravity 用量刷新修正

- **重新授权授权 URL 修正**：WorkBuddy/Qoder/Trae 平台账号的"重新授权"入口不再复用 Claude 授权链接，隐藏 `AccountActionMenu` 中这三平台的重新授权按钮（其凭据走平台专用 OAuth/刷新链路）；其它平台保持各自分支。
- **模型测评时间展示**：模型能力中心排行榜的"最近运行时间"与专属评测历史记录统一展示到秒（`ModelLeaderboard`/`ModelDetailDrawer`/`RunsTable` 的 `formatTime` 改为含 `second` 的本地化日期时间）。
- **Antigravity 用量刷新**：`getAntigravityUsage` 支持 `force` 绕过 3 分钟缓存并交由主动查询链路；前端 Antigravity 用量单元格新增单独的主动查询按钮（`loadActiveUsage`），避免用量窗口长期不刷新或刷新结果失真。
- **改动文件**：`frontend/src/components/admin/account/AccountActionMenu.vue`、`frontend/src/views/admin/components/benchmark/{ModelLeaderboard,ModelDetailDrawer,RunsTable}.vue`、`frontend/src/components/account/AccountUsageCell.vue`、`backend/internal/service/account_usage_service.go`。
- **合并影响**：均为二开增量；不推进 `UPSTREAM-BASE`。


## 同步 v0.2.17（2026-10-16）

- **区间**：`UPSTREAM-BASE 4d9ac3e4... -> b0a02a33...`（v0.2.16 → v0.2.17），48 commits / 153 upstream files。
- **本次策略**：用户明确“支付链路不用，不同步”，故 `backend/payment/`、webhook、支付前端、支付测试等路径全部跳过；其余按建议 selective sync（110 文件覆盖/merge，32 跳过，11 个冲突手工裁决）。
- **优先同步项**：
  - 安全路由：composite OAuth 歧义路由拒绝含 OpenAI 网关 OAuth 的组合（`composite_route_resolver`、`gateway_service`、`gateway_handler`）。
  - OpenAI 兼容：GPT-5.5 retirement、Responses `created_at` 修补、Anthropic base URL honoring、可配置 min output token floor、Codex import workspace 隔离、web_search emulation、account eligibility。
  - Gemini/Antigravity：compat thinking、Vertex SA 已删除处理、共享容量 429 同号退避、错误策略单查、Gemini 4 Argon catalog/pricing、tool_result 图片、native functionCall/thoughtSignature、v1beta 路由。
  - Zhipu GLM Coding Plan 5h/weekly 窗口修正、account websearch config、config example、模型与计费价格表。
  - 前端：`useModelWhitelist`、Antigravity/AccountModal 兼容修改、AccountTableFilters 保持二开 dense 版（上游布局改动未覆盖）。
- **冲突裁决要点**：
  - `account.go`：保留二开“Antigravity 空映射直通 / 非空映射不强注默认条目”语义；同时带入上游 `antigravityDefaultPassthroughModels` + `isAntigravityInjectedDefaultPassthrough`。
  - `antigravity_gateway_service.go`：函数重命名为 `buildGeminiTestRequestWithPrompt`，整合图片模型分支；文本路径移除 `maxOutputTokens: 1`（防 thinking 假成功），保留上游 `toolConfig VALIDATED`。
  - `antigravity_gemini_thinking_variant*.go`：采用上游变体解析逻辑与测试。
  - `composite_*`、`gateway_service.go`：保持二开多平台候选集合语义，叠加上游 OpenAI Gateway OAuth 歧义标记。
  - `vertex_service_account.go`：带入 `vertexTokenRequestError` / `vertexServiceAccountNotFoundError`，移除 so 未实现的 Express mode 依赖。
  - `gemini_messages_compat_service.go`：保留上游 `isVertexSharedCapacity429` / `handleGeminiThirdParty429`，恢复 `parseGeminiRetryDelayResetTime`。
- **测试适配**：`openai_model_retirement_test.go`（platform constraint 类型），`pricing_service_test.go`（`LiteLLMModelPricing.CacheStorageCostPerTokenPerHour` + raw entry + conversion），补 `pricing_service.go` 字段。
- **验证**：`go build ./...`、`go vet -tags=unit ./...` 通过；`pnpm build`（i18n + vue-tsc）通过。
- **版本号**：`VERSION` 已同步为 `0.2.17`；`UPSTREAM-BASE` 推进至 `b0a02a3304a0a78cdd003ddda1dd23ba02c03084`。
- **跳过/未同步**：支付链路（用户声明不使用）、README/sponsor/logo、en locale 包（so 已裁剪）、`AccountTableFilters.spec.ts`（上游删除、so 无）、 PaymentView/StripePaymentView/AccountTestModal 路径差异、部分 cropped-module 测试。

### 2026-10-16 模型测评执行器：传输层失败单次重试
- **描述**：模型能力测评执行器在遇到传输层错误（HTTP client 错误、429、5xx、超时）时自动进行一次重试；4xx 业务错误、格式错误、预算耗尽、内容过滤不重试，避免把可快速失败的请求拖长。终局耗时仍记录为完整两次请求链路长度，避免把重试耗时隐去。
- **改动文件**：`backend/internal/service/model_benchmark_executor.go`。
- **与上游合并影响**：无（so 私有功能，不影响上游）。

## 模型能力中心单页融合（2026-10-16）

- **功能描述**：将「模型清单」「测评计划」「模型能力测评」合并为单页 `模型能力中心`，采用 3 Tab（模型能力测评 / 模型清单 / 测评计划），统一状态保留；旧路由深链重定向到对应 tab。
- **改动文件/模块**：
  - `frontend/src/views/admin/model-capability/ModelCapabilityCenterView.vue`（新壳）
  - `frontend/src/views/admin/model-capability/tabs/{OverviewTab,ManifestTab,PlansTab}.vue`（从原 views 抽出主体）
  - `frontend/src/views/admin/{ModelManifestView,ModelBenchmarkView,ModelBenchmarkPlansView}.vue`（薄包装）
  - `frontend/src/router/index.ts`（新增 `/admin/model-capability`，旧路由 redirect）
  - `frontend/src/components/layout/AppSidebar.vue`（3 菜单合并为 1）
  - `frontend/src/i18n/locales/zh/common.ts`（nav.modelCapabilityCenter）
  - `docs/model-capability-center-design.html`、`mockups/model-capability-center-ui.html`
- **与上游合并影响**：纯前端信息架构重排；backend API/schema 不动；旧 URL 仍可通过 redirect 访问，影响只限前端 admin 视图测试/路由断言。

## 模型能力测评新增推理级别（2026-10-17）

- **功能描述**：测评计划支持用户自定义推理级别（reasoning_effort），执行时按配置注入请求；未设置则不传，保持上游默认。支持 `reasoning_effort` / `reasoning_level` 两种字段别名。
- **改动文件/模块**：`backend/internal/service/model_benchmark_types.go`、`model_benchmark_service.go`、`model_benchmark_executor.go`；`backend/internal/handler/admin/model_benchmark_handler.go`；`backend/internal/repository/model_benchmark_repo.go`；`backend/migrations/247_so_model_benchmark_reasoning_level.sql`；`frontend/src/api/admin/modelBenchmarks.ts`、`frontend/src/views/admin/model-capability/tabs/PlansTab.vue`、`frontend/src/i18n/locales/zh/admin/modelBenchmark.ts`。
- **与上游合并影响**：纯二开改动，不影响 UPSTREAM-BASE。

## 模型能力测评 v4：编程与算法强化（2026-10-17）

- **功能描述**：`hard-v1` 题库和评分均由 `3.0.0` 升级至 `4.0.0`，更换种子盐，单卷从 56 题型/56 题增至 80 题型/80 题。新增 16 个编程题型和 8 个算法题型，均为设计难度 D5，D5 总数增至 49；真实难度仍待真实模型校准。
- **编程比重**：编程领域从 9 题/12% 评分权重提高至 25 题/40%；算法从 7 题/12% 提高至 15 题/20%。合计 40 题、60% 权重，其他七域继续保留且领域权重总和为 100%。
- **测评内容**：增加 Go 切片别名/defer、事件循环、MVCC 写偏斜、冲突串行化、CAS 全调度计数、死锁传播、脏页 LRU、污点不动点、字节码、补码溢出、ABI 布局、二分及区间和补丁、最短反例、SQL NULL；算法增加负环影响、最大流最小割、最大匹配计数、SCC、加权编辑距离、LIS 计数、区间更新和带钥匙最短路。
- **判分与展示**：新增受限 `coding_patch` 和 `coding_witness` 判分器，完整有限输入域验证补丁并接受任意等价最短反例，不执行任意模型代码。页面显示题库版本、实际编程/算法权重，成本提示按 80×采样数更新；新旧成绩沿用版本隔离，现有计划参数保留。
- **改动文件/模块**：新增 `backend/internal/service/model_benchmark_coding_{suite,algorithms,checker}.go` 及对应独立 oracle/负例测试；更新 `model_benchmark_suite.go`、`model_benchmark_extended_suite.go`、`model_benchmark_types.go` 和题量/覆盖/预算测试；更新前端 `OverviewTab.vue`、`modelBenchmark.ts` 与两页面测试；更新 `docs/MODEL_CAPABILITY_BENCHMARK_{PLAN,QUESTION_AUDIT}.md`，新增 `MODEL_CAPABILITY_BENCHMARK_CODING_V4.md` 并在 `.gitignore` 放行该文档。
- **兼容修复**：保留工作区已有推理级别改动，补齐 `model_benchmark_repo.go` 的 `UpdatePlan` 推理字段参数及 RETURNING 列；`model_benchmark_repo_test.go` 同步 22 列 fixture 并补更新回归，关闭相关包测试中发现的 20/22 列不匹配。
- **验证**：新增 24 题型 × 128 seed × 2 变体 = 6,144 个公开题面独立复算实例及错误答案探针；受限补丁语言 720 种组合、等价最短反例、领域权重归一与 80 题预算专项通过。四包测评单测、go build、go vet、前端 8 项页面测试、2 项 i18n 测试、改动文件 ESLint 及前端构建通过。golangci-lint v2.13 因不兼容 Go 导出数据格式而 BLOCKED；完整证据见 v4 文档。真实模型校准与生产部署未执行。
- **与上游合并影响**：纯二开题库、判分、管理页面及文档增量；不新增依赖或数据库迁移，`UPSTREAM-BASE` 不变，上游只读仓库不变。

## 模型中心页面调整（2026-10-17）

- **功能描述**：菜单名称"模型能力中心"改为"模型中心"；模型能力中心页面取消二级 Tab，改为一级 Tab：模型清单、模型排名、测评计划、测评流水；测评设置显示已配置 Key 的名称，并回显所选分组与 API Key。
- **改动文件/模块**：`frontend/src/components/layout/AppSidebar.vue`（菜单 label）、`frontend/src/i18n/locales/zh/common.ts`、`frontend/src/i18n/locales/zh/admin/modelBenchmark.ts`、`frontend/src/router/index.ts`、`frontend/src/views/admin/model-capability/ModelCapabilityCenterView.vue`、`frontend/src/views/admin/model-capability/tabs/OverviewTab.vue`、`frontend/src/views/admin/model-capability/tabs/PlansTab.vue`、`frontend/src/views/admin/__tests__/ModelBenchmarkPlansView.spec.ts`。
- **与上游合并影响**：纯前端二开改动，`UPSTREAM-BASE` 不变。

## 模型中心字段展示补全（2026-10-17）

- **功能描述**：测评计划列表展示编辑表单的全部字段（采样次数、输出预算、推理级别、超时、max_results 等）；模型排名与测评流水新增模型推理强度展示；后端排名查询与 DTO 同步返回 `reasoning_effort`/`reasoning_level`。
- **改动文件/模块**：`backend/internal/service/model_benchmark_types.go`、`backend/internal/repository/model_benchmark_repo.go`、`frontend/src/api/admin/modelBenchmarks.ts`、`frontend/src/views/admin/model-capability/tabs/PlansTab.vue`、`frontend/src/views/admin/components/benchmark/ModelLeaderboard.vue`、`frontend/src/views/admin/components/benchmark/RunsTable.vue`。
- **与上游合并影响**：纯二开改动，`UPSTREAM-BASE` 不变。

## 模型能力测评升级（2026-10-17）

- **功能**：hard-v1 题库从 v4.0.0（80 题型）升级到 v5.0.0（98 题型），新增 18 个 D6 难度题型（大图算法、长链模拟、SAT/约束、多步规划、博弈论），difficulty 扩展至 6；输出预算默认从 4096 放开到 32768。
- **改动文件**：backend/internal/service/model_benchmark_d6_suite.go（新建）、model_benchmark_d6_test.go（新建）、model_benchmark_suite.go、model_benchmark_types.go、model_benchmark_extended_suite.go、model_benchmark_coding_algorithms.go、domain_constants.go、model_benchmark_service.go、frontend/src/views/admin/model-capability/tabs/PlansTab.vue
- **与上游合并影响**：不涉及 UPSTREAM-BASE。

## 测评设置统一管理采样次数与单题 max tokens（2026-10-17）

- **功能描述**：采样次数（1-3）与单题 max tokens（128-32768）从计划级参数提升为「测评设置」全局统一配置：设置保存后回填所有测评计划（`ApplyPlanBudgetDefaults`），创建计划时以设置值为准（忽略请求传入值），编辑计划不再接受这两项修改。计划新建/编辑表单中两项改为只读回显（disabled），并在设置卡片新增两个数字输入与 API Key 一并保存；未重新选择 Key 时保存沿用已配置 Key（避免误清除）。执行链路零改动（run 仍从 plan 拷贝快照，`max_tokens` 仍取 `plan.MaxOutputTokens`）。手动执行确认与成本提示的采样数改读全局设置。
- **改动文件/模块**：`backend/internal/service/settings_view.go`（`ModelBenchmarkSettings` 加 `sample_count`/`max_output_tokens`，默认 1/8192）、`setting_features.go`（读取兜底旧 JSON）、`model_benchmark_types.go`（仓储接口加 `ApplyPlanBudgetDefaults`）、`model_benchmark_service.go`（`UpdateBenchmarkSettings` 校验并回填、`CreatePlan` 套用设置值、`UpdatePlan` 移除两项合并）、`backend/internal/repository/model_benchmark_repo.go`（全表回填实现）；`frontend/src/api/admin/modelBenchmarks.ts`、`frontend/src/views/admin/model-capability/tabs/PlansTab.vue`、`frontend/src/i18n/locales/zh/admin/modelBenchmark.ts`；测试 `model_benchmark_test.go`（settings 校验/回填/CreatePlan 套用）、`ModelBenchmarkPlansView.spec.ts`（保存载荷与只读断言）。
- **与上游合并影响**：纯二开改动（测评模块本身为二开），`UPSTREAM-BASE` 不变。

## 修复测评设置保存 max_output_tokens=32768 时 500（2026-10-17）

- **功能描述**：`PUT /admin/model-benchmarks/settings` 传入 `max_output_tokens=32768`（或计划写入 >4096）时触发 PostgreSQL `model_benchmark_plans_max_output_tokens_check`（上限 4096）/`model_benchmark_plans_timeout_seconds_check`（上限 600）违反，`ApplyPlanBudgetDefaults` 返回 500。新增迁移 `248_so_model_benchmark_budget_limits.sql` 将两约束放宽为服务层/前端允许的 128-32768 / 10-900，与 `ModelBenchmarkSettings` 校验及计划校验对齐，避免合法配置保存失败。
- **改动文件/模块**：`backend/migrations/248_so_model_benchmark_budget_limits.sql`；`CHANGELOG-SO.md`。
- **与上游合并影响**：纯二开改动（测评模块为二开），`UPSTREAM-BASE` 不变。


## 模型能力测评 v6：难度强化（2026-10-17）

- 套件版本 5.0.0 → 6.0.0，盐值更新；权重从 code 40% 调整为 28%，提升 logic/abstraction/understanding/planning 权重。
- D6 与 coding 题型扩大数据规模（TSP、SAT、多维背包、LRU、冲突图、溢出、LIS、矩阵链等），减少可直接模拟/记忆的题目。
- 修复数据泄露：epistemic 题不再公开 keys，改为公开 seen_counts 推理；Nim 题不再直接暴露 XOR 公式，要求返回最优首着。
- 评分严格化：任何不匹配的正确答案均不得分；witness 题仍接受等价最优解，但增加规模与约束。
- 改动文件：backend/internal/service/model_benchmark_suite.go、model_benchmark_d6_suite.go、model_benchmark_coding_suite.go、model_benchmark_d6_test.go、model_benchmark_coding_test.go、model_benchmark_test.go；前端测试同步版本/权重。
- 是否影响与上游合并：否，纯二开测评模块。

## 测评计分最终原则：执行失败计 0 分进入分母（2026-10-17）

- **功能描述**：确立「只要未作对就不得分」为测评计分最终原则：`execution_failed` 题（网络/上游/沙箱失败、预算截断、内容过滤）不再被排除在能力分之外，而是以 0 分计入能力分与分类分分母（`AggregateModelBenchmarkRun` 中分母累加移到执行失败跳过之前）。完成率、格式合规率、能力域覆盖率、`AnsweredCount`、用量诊断（`usageComplete`）仍只统计有效作答——残缺 run 的排名资格门槛（完成率 ≥90%、各域覆盖 ≥80%）不变。背景：生产 Run #149 两题 execution_failed 能力分仍 100，暴露旧规则（CHANGELOG 前文「上游失败不计为答错」「执行失败不计入能力分分母」）与用户预期不符，经确认废除旧规则。
- **改动文件/模块**：`backend/internal/service/model_benchmark_types.go`（聚合函数注释与分母逻辑）、`backend/internal/service/model_benchmark_test.go`（`TestModelBenchmarkAggregateExecutionFailureDoesNotBecomeZeroIntelligence` 重写为 `...CountsAsMissed`，断言 2/5=40）、`docs/MODEL_CAPABILITY_BENCHMARK_PLAN.md`（2 处计分规则）、`docs/MODEL_CAPABILITY_BENCHMARK_EXPANSION_PLAN.md`（1 处验收描述）。
- **与上游合并影响**：纯二开改动（测评模块为二开），`UPSTREAM-BASE` 不变。

## 测评排名完全开放：任何完成率都参与排名、未执行题计 0 分（2026-10-17）

- **功能描述**：落实计分最终原则的第二步——「任何完成率都参与排名，未完成就不得分」：①`AggregateModelBenchmarkRun` 移除完成率 ≥90% 与能力域覆盖 ≥80% 两道排名资格门禁（覆盖率仅作诊断展示），原 `insufficient_coverage` 原因废弃，套件规模不匹配改标 `suite_mismatch`（仅剩它与 provider-managed budget 两个资格门槛）；②`finishRun` 收尾聚合前新增 `padUnexecutedBenchmarkItems`：执行中断/超时导致计划内未产生作答记录的题合成 `execution_failed` 计分行（`not_executed`，不落库），以 0 分计入能力分/分类分分母；③`finishRun` 不再因执行出错直接取消排名资格——残缺 run 的成绩由 0 分分母自然拉低。完成率、格式合规率、`AnsweredCount` 语义不变（仍只统计有效作答）；同时拆除能力分计算的 `answered > 0` 门槛——全未作答的 run 能力分记 0（而非空值），分母恒为全部计划题权重，任何原因未完成的题都不会把分母缩小为 0 或缺失。
- **改动文件/模块**：`backend/internal/service/model_benchmark_types.go`（排名资格逻辑）、`backend/internal/service/model_benchmark_service.go`（pad 函数 + finishRun 调用与 execErr 分支）、`backend/internal/service/model_benchmark_extended_test.go`（覆盖测试断言改为资格开放/分数拉低，新增 `TestModelBenchmarkUnexecutedPlannedItemsScoreZero`）、`docs/MODEL_CAPABILITY_BENCHMARK_PLAN.md`、`docs/MODEL_CAPABILITY_BENCHMARK_EXPANSION_PLAN.md`。
- **与上游合并影响**：纯二开改动（测评模块为二开），`UPSTREAM-BASE` 不变。

## 模型能力测评 v7：核心题硬化 + D6 扩规模 + D7 复合题型（2026-10-17）

- **功能描述**：`hard-v1` 题库与评分从 `6.0.0` 升级到 `7.0.0`（新种子盐），单卷从 98 题增至 106 题，难度标签扩展到 D7（权重 4.0）。两类改动同时执行：
  1. **现有题型硬化**：`modular-exponentiation`（9~10 位参数）、`constrained-permutations`（n=12..15）、`shortest-path`（9 节点 13 边）、`topological-order`（10 节点小根堆 Kahn）、`cycle-detection`（改为返回环成员数组，消除布尔盲猜）、`kmp-prefix`（28..34 字符）、`sparse-matrix`（14×14 双模条件）、`token-bucket`（14 动作）、`interval-dp`（n=11..14）、`pseudocode-trace`（Collatz 起点 10 万级）、`markov-expected-steps`（两状态链）、`greedy-counterexample`（四枚面额+DP）、`pareto-frontier`（14 项）、`truth-assignment`（5 变量结构化子句数据块）、`two-machine-schedule`（14 作业）。
  2. **D6 扩规模**：TSP n=14、SAT 18 变量 72 子句、多维背包 24 项 5 维、LRU 120 次访问、冲突图 12 事务 80 操作、16 位寄存器 80 次运算、LIS 80 元素、拓扑计数 12 节点、区间 DP 16 堆、矩阵链 10 矩阵、8 点时间窗路线等。
  3. **新增 8 个 D7 复合题型**：`d7-dfa-minimize`、`d7-regex-complement`（algorithms）、`d7-permanent-mod`（math）、`d7-vector-clock`、`d7-crdt-merge`（code）、`d7-list-scheduling`、`d7-planning-min-steps`（planning）、`d7-datalog-fixpoint`（logic）。全部复用既有 checker，不执行模型生成的代码。
- **判分与兼容**：能力域权重沿用 v6（code 28%、algorithms 18% 等）；`d7-datalog-fixpoint` 归入 logic、`d7-permanent-mod` 归入 math，其余 D7 题型计入对应既有能力域。旧 run 按版本隔离，不回溯改分。
- **改动文件/模块**：`backend/internal/service/model_benchmark_suite.go`（版本/盐/核心题型硬化）、`model_benchmark_d7_suite.go`（新建）、`model_benchmark_d7_test.go`（新建）、`model_benchmark_d6_suite.go`（分发到 D7）、`model_benchmark_types.go`（D7 难度权重）、`model_benchmark_audit_test.go`（核心题独立 oracle 同步）、`model_benchmark_test.go`、`model_benchmark_extended_test.go`、`model_benchmark_coding_test.go`；前端 `ModelBenchmarkView.spec.ts`、`ModelBenchmarkPlansView.spec.ts`（版本/题量/权重/成本提示断言）；文档 `MODEL_CAPABILITY_BENCHMARK_PLAN.md`、`MODEL_CAPABILITY_BENCHMARK_QUESTION_AUDIT.md`（新增第 11 节）。
- **验证**：`go test -tags=unit ./internal/service -run TestModelBenchmark -count=1 -timeout=25m` PASS；`go build ./...`、`go vet -tags=unit ./internal/service` PASS；前端 2 个测评页面 Vitest 共 9 项 PASS。真实模型难度校准与生产部署 NOT_RUN。
- **与上游合并影响**：纯二开题库、判分与文档变更；不新增依赖、不新增数据库迁移，`UPSTREAM-BASE` 不变。

## 模型能力测评：修复 D6/D7 作答行被难度约束拒绝导致测评中断（2026-10-17）

- **问题现象**：测评页面「题目作答明细与思维链」显示 15 / 15 全部正确，但 Run 能力分只有 13.1 分（有效完成率 14.2%）。
- **根因**：`243_so_model_capability_benchmark.sql` 建表时题库仅到 D5，`model_benchmark_run_items.difficulty` 的 CHECK 写死为 `BETWEEN 3 AND 5`；v5.0.0 起新增 D6、v7.0.0 起新增 D7 题型后约束未同步放宽。题目按 ID 字典序执行，前 15 题（a*~c* 的 D3-D5）正常落库，第 16 题（d6-*，难度 6）INSERT 时触发 `model_benchmark_run_items_difficulty_check` 违反，`ExecutePlan` 中断；`finishRun` 的 `padUnexecutedBenchmarkItems` 把剩余 91 题按「未执行=0 分」计入分母，故 15 题全对仍只有 13.1 分。历史 run #156/#154/#137 同样触发该错误（此前「未完成按 0 分」逻辑未上线时分母仅含已答题，故显示 100 分，属虚高）。
- **修复**：新增迁移 `249_so_model_benchmark_difficulty_range.sql`，DROP 并重建约束为 `CHECK (difficulty BETWEEN 3 AND 7)`（不改 243，迁移有 checksum 校验）；`RunDetailModal.vue` 徽标由「筛选条数 / 已落库条数」改为「已作答 X / 共 Y 题」，并在存在未作答题时显示「N 题未作答 · 按 0 分计入」提示，避免把 15/15 误读为全卷全对。
- **改动文件/模块**：`backend/migrations/249_so_model_benchmark_difficulty_range.sql`（新建）、`backend/migrations/model_benchmark_migration_test.go`、`frontend/src/views/admin/components/benchmark/RunDetailModal.vue`、`frontend/src/i18n/locales/zh/admin/modelBenchmark.ts`。
- **验证**：`go test -tags=unit ./migrations/ -run 'TestModel.*Migration'` PASS；`go test -tags=unit ./internal/service/ -run 'TestModelBenchmark'` PASS；前端 `vue-tsc --noEmit`、ESLint（改动文件）、`vitest ModelBenchmarkView/ModelBenchmarkPlansView` 共 9 项 PASS。生产环境需部署新二进制（迁移随启动执行）后重跑测评才能得到真实分数；历史 run 的分数不回溯。
- **与上游合并影响**：纯二开改动（测评模块为二开），`UPSTREAM-BASE` 不变。

## Antigravity 用量查询改为 retrieveUserQuotaSummary（2026-10-17）

- **问题现象**：账号列表里 Antigravity 账号的用量长期显示 0%（进度条永远满格/空白），刷新后仍是错误数据。
- **根因（真实账号实测）**：旧实现调用 `v1internal:fetchAvailableModels` 读取每个模型的 `quotaInfo.remainingFraction`。该接口对全部 34 个模型恒返回 `remainingFraction: 1`（即使账号实际已消耗额度），且它返回的是模型目录能力信息，不是账号用量。真实用量在 `v1internal:retrieveUserQuotaSummary`：按额度组（`Gemini Models` / `Claude and GPT models`）返回 `buckets[]`，每组包含 `5h` 与 `weekly` 两个窗口，各自带 `remainingFraction` 与 `resetTime`。CLIProxyAPI 生态（CPA-Manager-Plus 管理面板、lamplighter 插件）均以该接口为额度来源。
- **修复**：新增 `antigravity.Client.RetrieveUserQuotaSummary`（prod → daily URL 回退、403 归类为 ForbiddenError、1 MiB 读取上限）；`AntigravityQuotaFetcher.FetchQuota` 改走该接口，`UsageInfo` 新增 `antigravity_quota_groups`（组 key：`gemini` / `claude-gpt`，每桶含 window/utilization/reset_time）；`FiveHour` 兼容字段取首个 5h 桶。`fetchAvailableModels` 保留给模型目录同步（`upstream_models.go`），`antigravity_quota` 旧字段保留但注释标明不可作为用量来源。使用率由截断改为四舍五入（避免 `1-0.8` 算成 19%）。403 错误文案通过新增的 `ForbiddenError.Operation` 保持旧输出兼容。
- **前端**：Antigravity 单元格改为按额度组渲染 `Gemini 5h/7d`、`Claude 5h/7d` 四条进度条（`label-width="auto"`，5h 在前），保留旧 `antigravity_quota` 回退分支；刷新按钮由 `justify-end` 改为 `justify-start`（靠左）；新增 i18n key `usageWindow.gemini`。
- **渠道监控**：`usageQuotaTiers` 优先消费 `AntigravityQuotaGroups`（Window=5h/weekly，Label=组 key），旧 per-model `total/*` tier 仅保留给历史数据。
- **改动文件/模块**：`backend/internal/pkg/antigravity/client.go`、`backend/internal/service/antigravity_quota_fetcher.go`、`account_usage_service.go`、`channel_monitor_quota_fetcher.go`、`antigravity_quota_fetcher_test.go`、`channel_monitor_quota_fetcher_test.go`；`frontend/src/types/index.ts`、`components/account/AccountUsageCell.vue`、`components/account/__tests__/AccountUsageCell.spec.ts`、`i18n/locales/zh/admin/accounts.ts`。
- **验证**：真实账号直连 prod 端点跑通新链路（Gemini weekly 剩余 0.9387 → 6%，5h 0%，Claude 组独立）；`go build ./...`、`go test -tags=unit ./internal/service ./internal/pkg/antigravity` 相关用例 PASS；前端 `vue-tsc --noEmit`、ESLint、`vitest AccountUsageCell/UsageProgressBar` 72 项 PASS。
- **与上游合并影响**：涉及上游同名文件 `antigravity_quota_fetcher.go` / `account_usage_service.go` / `channel_monitor_quota_fetcher.go` / `pkg/antigravity/client.go`，下次上游同步需按二开语义手工合并；`UPSTREAM-BASE` 不变。

## 推理档位全量透传：取消 max→xhigh 的模型白名单与降级（2026-10-20）

- **问题现象**：Claude Code `--effort max` 经 sub2api 转发到 WorkBuddy2API 的 `cn:deepseek-v4.1-flash` 时，`usage_logs.requested_reasoning_effort=max`，但实际出站 `reasoning_effort=xhigh`。WorkBuddy CN 档位表中该模型支持 `low/high/max`，xhigh 不在支持列表，上游会再降到 high，最终推理强度低于请求值。
- **根因**：通用 Anthropic→OpenAI（Responses / Chat Completions）桥接先把 Anthropic `max` 映射成 `xhigh`，再由 `openAICompatAnthropicReasoningEffort` 仅对白名单模型恢复 `max`；`cn:` 命名空间前缀使 `cn:deepseek-v4.1-flash` 未命中 `deepseek-v4*`/`deepseek-flash` 前缀，max 被保留为 xhigh。经用户确认改为全量透传，不再维护模型白名单。
- **修复**：①删除 `supportsOpenAIReasoningEffortMax` 白名单与 `openAICompatAnthropicReasoningEffort` 事后恢复逻辑；②`mapAnthropicEffortToResponses` 原样返回 effort；③`normalizeOpenAIReasoningEffort` 对 `xhigh`、`max` 分别原样返回（`extrahigh` 仍归一为 xhigh）；④模型名后缀推导的 `-xhigh` 不再经 `max` 中转，直接保留 xhigh。群组级 `max_reasoning_effort` 上限/映射策略保持不变（管理端显式配置，仍可按配置降级或拒绝）。
- **改动文件/模块**：`backend/internal/pkg/apicompat/anthropic_to_responses.go`、`backend/internal/service/openai_compat_model.go`、`backend/internal/service/openai_gateway_messages.go`、`backend/internal/service/openai_gateway_messages_chat_fallback.go`、`backend/internal/service/openai_gateway_request_body.go`；对应测试 `anthropic_responses_test.go`、`chatcompletions_anthropic_bridge_test.go`、`openai_compat_model_test.go`、`openai_gateway_messages_chat_fallback_test.go`、`openai_gateway_service_hotpath_test.go`、`openai_gpt56_max_test.go`、`openai_reasoning_effort_candidates_test.go`、`openai_ws_v2_passthrough_adapter_effort_test.go`、`gateway_forward_as_chat_completions_test.go`、`gateway_forward_as_responses_test.go`。
- **验证**：Go 1.27.2 容器（`golang:1.27.2`）内 `gofmt -l` 无输出；`go test -tags=unit ./internal/pkg/apicompat` PASS；`go test -tags=unit ./internal/service -run "TestNormalizeOpenAIReasoningEffortPassesThrough|TestExtractOpenAIReasoningEffortFromBody|TestExtractOpenAIReasoningEffortFromBodyModelCandidates|TestForwardAsAnthropic_PreservesMaxForFinalGPT56ResponsesModel|TestForwardAsAnthropic_NormalizesRoutingAndEffortForGpt54XHigh|TestForwardAsAnthropic_ForceChatCompletionsPreservesFinalModelReasoningEffort|TestWSPassthroughUsageMeta"` PASS。全量 `./internal/service` 另有 4 个与本改动无关的既有失败（`TestResolveGeminiThinkingVariant`、`TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants`、`TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership`、`TestConfiguredUpstreamModelsForCapabilitySyncSkipsInjectedAntigravityPassthroughs`），未在本改动中处理。
- **与上游合并影响**：涉及上游同名文件，下次 `sub2api-upstream-sync` 需保留“请求 effort 全量透传”的二开语义；`UPSTREAM-BASE` 不变。

## 2026-10-20 部署修复：Dockerfile.bundle 入口脚本权限 + 245 迁移 checksum 兼容（2026-10-20）

- **事故背景**：推理档位透传改动首次部署两次失败并已即时回滚（累计中断约 6 分钟）：① fnOS 工作区源码文件 mode 恒为 000，`COPY` 进镜像后 `RUN chmod +x` 仅得 111（无读位），`/bin/sh` 无法打开 `/app/docker-entrypoint.sh`，容器循环重启；② 入口权限修复后，新二进制因 `245_so_model_benchmark_comparison.sql` checksum 漂移拒绝启动（生产库执行的是 2026-10-18 镜像构建时的早期版本，仓库其后又有修改）。
- **修复一（构建）**：`deploy/Dockerfile.bundle` 末段 `RUN chmod +x` 改为 `RUN chmod 755`，与源文件 mode 解耦（文件头差异说明由两处更新为三处）。
- **修复二（迁移）**：按项目既有 `migrationChecksumCompatibilityRules` 机制为 245 增加兼容规则（file=4fa469de…，db=df71b36e…）。部署前已对生产库全量审计（TrimSpace+SHA256 口径）：仓库 300 个迁移中仅 245 一处真实漂移；DB 另有 4 条 `238/239/240/241_so_drop_*` 记录其文件不在仓库与镜像中，runner 按「仅遍历文件侧」逻辑容忍，无需处理。
- **部署验证**：新镜像先以生产环境变量 + 数据目录副本做独立容器冒烟（连真实库启动至 healthy、迁移校验通过、无错误日志），再 `docker compose up -d --force-recreate` 替换；正式容器 healthy、`/health` ok、版本 `0.2.17 (commit 18e0078, built 2026-10-20T05:04:44Z)`。回滚备份镜像：`sub2api:0.2.17-pre-effort-20261020`（原始）、`…-effort-broken-20261020`（入口权限故障）、`…-effort-broken2-20261020`（checksum 故障）。
- **改动文件/模块**：`backend/internal/repository/migrations_runner.go`（245 兼容规则）、`deploy/Dockerfile.bundle`（chmod 755 + 头注释）。
- **与上游合并影响**：`migrations_runner.go` 与根 `Dockerfile` 均为上游同名文件/副本，下次同步需保留二开语义；`UPSTREAM-BASE` 不变。
