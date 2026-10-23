# sub2api-so 二开变更记录

> 规则：二开变更（无论是否经 `sub2api-upstream-sync` 技能）都必须记录于本文件，新条目追加到文件末尾，每条含：日期、功能描述、改动文件/模块、是否影响与上游合并。
> 基准：`UPSTREAM-BASE`（单行完整哈希）只在完整上游同步后前移；手动移植只记本文件、不改基准（唯一例外见 10-13 收口条目）。
> 整理记录：2026-10-12 / 10-13 / 10-15 / 10-23 四次压缩，只保留功能事实、关键决策、合并冲突面与已知缺口；根因推导、测试输出、逐提交哈希与逐文件清单见 git 历史（10-15 压缩前 HEAD=`f5aa70c`，10-13 压缩前 HEAD=`45cf4c6`；10-23 压缩将模型测评历史收敛为单条，压缩前 HEAD=`82fc11f`）。

## 当前状态（2026-10-23 核实）

| 项 | 状态 |
|---|---|
| 基准 | `UPSTREAM-BASE`=`b0a02a33`（v0.2.17）；`VERSION`=0.2.17 |
| 后端 | 代码 = v0.2.17 + 二开（三平台源码集成、Antigravity 直接模型模式、账号/计费/网关定制，逐项见时间线）；模型能力测评已于 10-23 整体移除 |
| 前端 | 二开：功能入口裁剪 + UI/UX 体系重构 + 仪表盘/用量统计/账号表格增强；模型能力中心收敛为模型清单单页（无评分） |
| 数据库 | 官方迁移序列终态（裁剪期 drop 的表由 `deploy/restore-upstream-schema.sql` 恢复，可重放幂等）；二开迁移命名 `NNN_so_*.sql`，与上游编号隔离；测评三表由 `250_so_drop_model_benchmark.sql` 清除 |
| 验证基线 | 2026-10-23：后端 `go build` 通过，unit 全量失败仅 5 个已知存量（Gemini thinking variant / 复合分组族 ×4，10-21 条目有清单）；前端 typecheck + vitest（286 文件 / 2124 用例）+ build 全绿 |

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
- **pnpm 与 lock**：`package.json` 的 `pnpm.overrides` 曾未回写 lockfile，`--frozen-lockfile` 报 `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`——2026-10-21 已在容器内以 pnpm 9.15.9 重新生成 lock（settings 段补录 overrides）修复；根 Dockerfile 硬编码 pnpm@9 与 `deploy/Dockerfile.bundle`（corepack pnpm@9→pnpm@11.9.0 为唯一差异）是两套构建路径，上游同步后需对照 diff 保持；`pnpm.overrides` 在 pnpm 10+ 是死配置。
- **fnOS 文件权限**：NAS 工作区 fs mode 恒为 000（git index 的 mode 与之无关）——Dockerfile `COPY` 进镜像后不可读/不可执行，须显式 chmod（`Dockerfile.bundle` 入口 `chmod 755`、根 Dockerfile resources `chmod -R a+rX`）；`resolve-version.sh` 的 git 执行位已修（10-21），但 NAS 本地构建仍须传 `--build-arg VERSION=…` 绕过。
- **Nexus 私有镜像**：陈旧元数据可能报 `ERR_PNPM_NO_MATCHING_VERSION`，小版本升级可按传递依赖一致原则手改 lock 三处（importers/packages/snapshots）后 `pnpm install --frozen-lockfile`。
- **上游自带 spec 回归**：上游偶发改实现未更新自家测试（v0.2.14/0.2.15 各两例），以内容为准适配，并在条目中注明。
- **前端改动**：须 `pnpm build` 重建 dist 后重启容器才可见（so-dev 为编译期内嵌）。
- `psql` 连接用 `127.0.0.1`（localhost 有 IPv6 问题）；`frontend/pnpm-workspace.yaml` 的 `allowBuilds` 将 esbuild/vue-demi 置 true。

## 开发与部署环境

- **so-dev Docker 栈**（`deploy/docker-compose.so-dev.yml`）：backend `go run -tags embed` 热跑（golang:1.27.2）+ PG16 + Redis7；后端 `127.0.0.1:8088`，`AUTO_SETUP=true`；`frontend/.env.development.local`（gitignored）设 `VITE_DEV_PROXY_TARGET=http://127.0.0.1:8088`。
- **本机构建**：工作区根 `./build-so.sh`（前端 pnpm build → Docker go build → 单文件二进制；版本号从 `UPSTREAM-BASE` 推导 tag）/ `release-so.ps1`；本机无 Go。旧构建产物（`backend/bin`、`backend/dist`）仍报告 0.2.15，需重新构建。
- **离线打包**：`deploy/build-docker-tar.sh`（docker save tar + sha256 + `dist/docker-bundle/`：镜像 + `docker-compose.bundle.yml` + .env.example + README；tar 产出移至工作区根，`TAR_MOVE_DIR` 可覆盖）；PG/Redis 独立命名卷；已出 v0.2.13/v0.2.15 包并冒烟通过。
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
| v0.2.17 | 10-16 | `4d9ac3e4 → b0a02a33` | 48/153 | 支付链路整体跳过；composite OAuth 歧义路由、GPT-5.5 retirement、Gemini 4 Argon、共享容量 429 退避等；11 个冲突手工裁决（见当日条目） |

## 平台移植源同步（PORTER-BASE）

- **2026-10-16（首批入库，已并入后续基准）**：qoder→`b850c7e`（issue #33 图像单 part dict：`flattenContentText` 单 dict 按单元素列表处理）；trae `7364861..8edb86d`（issue #28 variant 别名 `trae_merged`/`global_trae_merged` 收敛映射到既有 TraeCN/GlobalTraeCode）。评估未移植：trae latency 语义拆分、账号池×模型限流、账号池冷却门控、本地客户端发现（架构不适用）。
- **2026-10-21（部分执行，并入 10-23 批次）**：qoder `5acef5e..91f17c2`——①风控身份形态对齐（#61/#64：`DeriveID` 带连字符 UUID 形状、`QD_DEVICE_ID_LEGACY` 保留旧 32-hex、请求 ID 纯随机、`DeriveMachineHostname` + cosy-machineos/machinehostname 头）；②出站体对齐（#59/#60/#63：`normalizeTools` 丢 `namespace`/`web_search`、不再重复下发整份 Base64、`QD_MAX_BODY_MB` 默认 29MB）；③共享组织配额（#39/#67：`Shared*` 字段与 `EffectiveRemain`）。trae `8edb86d..cbd2bf9` 仅评估未移植。PORTER-BASE 当次未推进。
- **2026-10-23（当前基准）**：
  - **来源**：qoder2api-hub `5acef5e..4e46ccd`（61 提交，v1.5.1→v1.6.9，含 10-21 已评估三批）；trae-workbuddy-switch `8edb86d..a1711fd`（259 提交，其中 issue #76 密钥权限系列 ~40 个不适用）；workbuddy2api-panel 本机无仓库跳过（基准不动）。
  - **已移植（qoder，commit 49f5c94）**：
    - 出站头族对齐 qodercli 1.1.75/桌面端 0.4.8（`7fb9281` #75 + `879b336`）：COSYVersion 1.1.75（QD_COSY_VERSION 覆盖）、UA=undici、business 三头、data-policy 小写、所有请求带 machineos（auto 识别）、openapi UA 按区域、intl 网关 api3 主选（QD_INTL_GATEWAY）。
    - 结构化白名单补 deepseek/dfmodel（`370948e` + trae仓 `5f08418` #72）；keep_reasoning 复用 IsDeepSeekModel。
    - intl 签到最小机器头兜底（`d6dd0d4` v1.6.9）：omitted+showCampaign=false 降档重试一次（QD_INTL_MACHINE_FALLBACK）；提速项不移植（so 无逐基址重试与面板同步等待，架构差异）。
    - runtime-info 环境码 cn=0/intl=3 + 落盘 environment 校验（#75）。
    - 回声 marker 变体正则识别 + 重复 marker + DSML 闭标签前缀 + 散文同帧（`370948e`）。
    - 出站清洗孤儿工具历史 + 结构化 400/422 降级记忆（6h，区域+模型，错误路径写入不重发——源的同账号文本重发在 CC pipeline 换号循环外，代价高于每 TTL 一次 400 体验，`5f08418` 语义取舍）。
    - 多模态 parts 出站（`36a3ff` 前半）：带图消息 content 保留结构化 parts，图片真正传上游。
    - 模型目录 is_free/峰谷价元数据（promotion 峰值×折扣；`69fa6fc` 数据面；前端展示列未加，字段已随 sync preview JSON 下发）。
  - **已移植（trae，commit e890c97）**：
    - JWT 续期协议修复（`74b702a`）：客户端 1.107.1 体形态（ClientSecret 空串 + DeviceInfo 12 字段 + IDEVersion，与 AuthCode 交换同一 deviceInfoPayload 派生点）、account_base 端点、六步变体链（含另一条产品线兜底）、10101/040004 参数无效分类、PlatformCode 按线派生、无私钥不发 proof。so 周期刷新链已有 account_refresh_failed Warn 日志（源仓的吞错缺陷不存在）。
    - Max Mode（`b49dee2`/`5c3e51a` #53 三版）：`pkg/trae/catalog.go` 重放 batch_get_detail_param（24h TTL 进程内缓存、单飞、静默失败、endpoint 可注入测试），max>dev 注入 is_max_mode（prompt_max_tokens 恒 168000），ModelEntry 增 MaxMode；**不落盘**（源落盘 ~/.buddy-switch，so 容器无对应约定，重启重拉一次可接受）。
  - **用户明确跳过（不再建议）**：workbuddy 会话头三件套恒发（`092c5e3`）与 sanitize DeepSeek Harness 改写 + 积分到期取最早（`66a33ab`/`67c544`）——2026-10-22 用户裁定不移植。
  - **评估未移植（原因）**：qoder #77/#83 换区 failover（请求级 realm 概念，so 按账号路由无对应路径）；#81 /agents/models（面板内部接口缺陷，so 无此接口）；#86 账号测试 only_account（so 测试本就指定账号）；v1.6.3/v1.6.4 用量洞察/web 工具与面板 UI 系列（so 有自身体系）；Pro 升级包领取（`1b2a3b7`——so 已有 runProWelfare 等价实现）；trae issue #76 密钥权限系列（自有网关 Key 体系，so 有自身 API Key/订阅体系）；trae 账号池系列（RAII/excluded/Retry-After，同 10-16 口径）；trae 限额护栏/pacing/在途闸门/历史削减（so 有自身 402 停调度）；DSML #66 系列/排队/首帧/CRLF（so 同代或更新实现）；无客户端部署（so 即服务器 OAuth 流）；稳定会话 ID（已有账号固定会话头）。
  - **改动文件**：`pkg/qoder/{baseprompt,fingerprint,realms,sign,sse,accounts,umid,models}.go` 及测试；`service/openai_gateway_qoder{,_test}.go`；`pkg/trae/{oauth,refresh,payload,models}.go`、`pkg/trae/catalog.go`（新增）及测试；`service/openai_gateway_trae.go`。
  - **验证**：`go build ./...` 全绿；`go vet -tags=unit ./internal/...` 通过；单测——pkg/qoder、pkg/trae、其余包全绿，service 5 个失败为**存量**（Gemini thinking variant / 复合分组族，stash 基线验证与本次失败清单完全一致，与移植无关，待单独排查）；黄金向量（sign payload/sig、UA、网关顺序、data-policy、DesktopVersion 0.4.8）已按新协议更新。
  - **基准推进**：PORTER-BASE qoder 行 → `4e46ccd9…`、trae 行 → `a1711fdc…`（workbuddy 不变）。
  - **与上游合并影响**：不涉及 sub2api-upstream-sync 的 UPSTREAM-BASE；改动全部位于二开文件与测试。

## 变更时间线

### 2026-09

- **09-09~09-11 前端 UI/UX 重构**：确立 `docs/ACCOUNT_TABLE_UI_SPEC.md` 基线（账号表格 dense、行高 48–54px、表单 label 左/控件右）；仪表盘 Bento 栅格与运维 SLA 色标；全站间距降档 dense 化（118 文件，确定性映射可重放）。合并：UI 层大面积二开，同步多为三路合并。
- **09-14 仪表盘与用量统计**：用户/管理员仪表盘统一为 `GatewayDashboard`（scope prop，后端零改动）；使用记录拆出「用量统计」页（`/usage-stats`，无费用口径）。合并：`GatewayDashboard.vue` 高冲突。
- **09-17~09-22 仪表盘/运维扩展**：全局默认时间范围「今天」；「Token 请求统计 · 今日」表；「API 密钥用量统计」板块；管理台与运维监控移除多区块（接口保留；`OpsDashboard.vue` 上游可整文件覆盖）。合并：`GatewayDashboard.vue` 高冲突。
- **09-26→10-13 账号用量窗口**：多轮迭代（微胶囊 → 双行严整 → 去底色 → 恢复），**最终回归上游 v0.2.15 原版**（每窗口纵排 `UsageProgressBar`，`AccountUsagePopover` 与 238px 规格文档已删）。合并：`UsageProgressBar.vue` 与上游仅差 visibilitychange 一处；`AccountUsageCell.vue`、`CNProviderQuotaCell.vue` 与上游同构，冲突面小。

### 2026-10 上旬与中旬

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
- **10-13 同步收口 v0.2.16（部分移植）**：上游 `3a6fd1c9` → `4d9ac3e4`（51 非合并提交/213 文件），移植 17 个（两批，明细见 git 历史）；人工解决 `openai_gateway_forward.go`（保留二开协议分流 switch，仅补 `startTime`）与 `openai_gateway_workbuddy_test.go`（补上游新签名 `time.Now()`）；第二批 12 文件三路合并无冲突。
  - **永久放弃（勿再当作待同步项）**：支付 1、净 diff 为零的依赖补丁 2、Antigravity 1、分组多语言 2、Gemini 显式缓存/Vertex/AQ Key 17、中等价值项 10、版本提交 1（逐提交哈希见压缩前 git 版本）。
  - **基准前移（一次性例外）**：`UPSTREAM-BASE` 前移至 `4d9ac3e4`，VERSION 置 0.2.16——二者必须同步，否则 `build-so.sh` 推导版本与 VERSION 矛盾。被放弃提交的改动已计入基准、二开侧不含它们，后续三路合并保持二开现状；仅与之相邻的后续上游改动可能冲突，需人工判断。
- **10-14 修复 Antigravity 已删模型回流 /v1/models**：`resolveModelMapping` 对**非空** `model_mapping` 的账号运行时回填 18 条 flash 自映射 + 3 条 pro 别名，被删模型回流 `/v1/models`、Codex 清单、`/v1beta/models`、composite 与 admin 分组候选。移除回填（`ensureAntigravityDefaultPassthroughs` 系成为孤儿已删）；`applyAntigravityGemini31ProAliases` 只改写已配置键、不再新增；变体解析并入 `mapAntigravityModel`（转发、调度准入、限流键三处同口径），只配变体未配裸名的账号仍接受裸名请求。横向排查：Grok/GoogleOne 注入只在空映射分支，其余 10 平台配置 1 条返回 1 条，本 bug 为 Antigravity 独有。合并：`account.go`、`antigravity_gateway_service.go` 手工合并。
- **10-14 workbuddy 移植 tool_pairing 配对清理（修 11133/11148 会话顶死）**：来源 workbuddy2api-panel `535e19e`（PR #148，线上 11133）。新增 `pkg/workbuddy/tool_pairing.go` `CleanupOrphanToolCalls`（孤儿 tool_call↔tool 两侧对称裁剪 + 空 `function.name` 脏条目否决；无工具流量零改动），挂接 `PrepareChatBody` 覆盖网关转发/测试连接/运营任务全部出站路径。根因：流式异常早停产出 `name:""` 条目被客户端写进历史，下轮转发上游判 400 11133（整条会话每轮 503 报废）；只删调用侧会留孤儿 tool 结果 → 11148，故整函数移植。未移植 `mergeAdjacentToolCalls`/`repackToolResultBlocks`（上游 deepseek-only 场景），已知缺口：拆条/插队报文形态仍可能 11148。合并：纯二开新增文件 + `client.go` 局部插入。
- **10-14 Qoder 签到闭环改造（修「提示成功但积分不到账」）**：根因 `runCheckin` 只打旧 sash `/daily-check-in`（CN DISABLED、INTL 404），跳过分支返回 OK → 前端弹成功但零入账；官方每日领取已迁到活动平台。参照 qoder2api-hub 重建：`CampaignCheckin` 编排（列活动 → 分类 → 逐个领 CLAIMABLE CREDITS；`nextCheckinWindow` 按 UTC+8 每日 10:00 名额刷新；earned 只计真实新增防虚增）；`runCheckin` 活动平台优先，仅列表查询失败才走旧 sash 接口；BLOCKED 记 6h 冷却持久化 `extra["qd_campaign_blocked"]`（多号同机防风控）；checkin Hours 默认 `9,21→10,21`；`runCampaigns` 排除 CREDITS 类（与 checkin 零重叠）。连带基建：`OpsStateEntry` 加 `Note`、`RunOne` 签名 `error → (string, error)`（三平台 15 个 RunOne 适配）、RunTask 返回 `{done,note}`、前端 toast `runSuccessNote`。已知限制：跳过态仍记 LastOK 属权威语义（改判失败会退避重试每天打上游），本次只修可见性。合并：纯二开模块，无冲突面。
- **10-14 workbuddy/qoder 签到状态胶囊**：用量列（`PlatformOpsQuotaCell.vue`）每日签到三态——今日成功绿「已签到」（title 含 note+时间）、跑过未成功红「签到失败」（title 含 last_err）、其余灰；数据读 `extra.ops_state.checkin`（零后端改动）；「今天」按账号时区（`extra.ops_timezone` ?? Asia/Shanghai）`Intl` 判定，与后端 `sameLocalDay` 同口径；trae 不展示。合并：本就在高冲突面清单内。
- **10-14 Qoder intl 双缺陷修复**：**A 模型路由三处漂移**——① 出站头从不发 `x-model-key`/`x-model-source`（修复：签名头注入时从原始 OpenAI 体解析模型、带 realm 归一为上游 key）；② `ResolveUpstreamKey` 丢 realm（跨区显示名歧义）；③ 目录元数据固定 cn 优先（双区同 key 元数据不同，intl 请求体按 cn 快照填错）。②③ 修复在 `pkg/qoder/baseprompt.go`。**B intl 签到缺真实 UMID 机器身份**（派生六头被活动平台整条过滤）——新增 `pkg/qoder/umid.go`（runtime-info 组件发现 → 调用取真身份，进程内 30min + 落盘双层缓存；`DesktopHeaders` 原生身份优先、导入凭证 machine_token 次之、无真身份不发六头；`Campaigns` 自愈清种子换身份重试一次）；`deploy/Dockerfile` 运行时依赖加 `gcompat libstdc++ libgcc`（组件为 glibc ELF，musl 需兼容层）；文档 `docs/qoder-umid.md`。已知限制（服务端策略）：机器身份机器级，intl 按设备去重，每设备每日仅 1 个 intl 账号可领。合并：全部位于二开文件。
- **10-15 模型清单与账号供给透视面板（Model Manifest）**：「模型 ⟷ 账号」倒排查询（方案 `docs/MODEL_MANIFEST_PLAN.md`，UI 对齐 `mockups/model-manifest-preview.html`）——某模型谁在供给/账号状态、某账号提供哪些模型、哪些模型全员断供。后端 `service/model_manifest_service.go`（一次全量加载内存倒排聚合，0 上游网络请求；模型解析优先级 `model_mapping` keys（跳过通配符）→ `extra.upstream_model_metadata` 快照 → 平台默认清单兜底；断供 = 可调度账号数为 0）+ `handler/admin/model_manifest_handler.go` + `GET /api/v1/admin/models/manifest`；前端指标卡、按模型/按账号双视角、客户端过滤 + 路由 + 侧边栏入口 + i18n。装配：`wire_gen.go` 手工接线（该文件不可 wire 重新生成——ProviderSet 缺 workbuddy provider 属既有状态）。合并：新增文件为主，触点小段插入，低冲突。（10-23 起入口为模型能力中心单页，`ModelManifestView.vue` 薄壳已删。）
- **10-15 Docker 打包脚本 tar 移至工作区根**：`deploy/build-docker-tar.sh` 打包后将 tar 及 `.sha256` 移到工作区根（`TAR_MOVE_DIR` 可覆盖）；`dist/docker-bundle/` 不再自包含，包内 README 的 `docker load -i` 需到工作区根取 tar。合并：纯二开脚本。
- **10-15 账号测试模型列表与白名单对齐**：`GetAvailableModels`（`GET /admin/accounts/:id/models`，测试弹窗与定时测试面板共用）原先三分支忽略账号已配置的 `model_mapping`，下拉展示平台全集——可选中账号实际不服务的模型。修复：helper `storedModelWhitelistIDs`（读原始 `credentials.model_mapping`，跳过空键与 `*` 通配符；`GetModelMapping` 对 Grok/GoogleOne 注入默认映射故不可用），三平台 OAuth（`integratedPlatformTestModels`）、Claude OAuth/SetupToken、Gemini OAuth 含 GoogleOne 三分支统一「白名单非空 → 映射请求侧 keys；空 → 平台目录」；通配符映射视为无法枚举回落目录。其余分支横向核实本就对齐。合并：`account_handler.go` 小段插入。
- **10-15 清理死代码 account/AccountTestModal**：`components/account/AccountTestModal.vue` 为 `components/admin/account/` 版本（AccountsView 实际使用）的旧副本，无视图引用，连同 spec 与 barrel 导出行删除。
- **10-15 /admin/usage 分页启用快速跳页**：`Pagination` 组件内置跳页输入框（`showJump` 默认 false，上游无页面启用），主日志表与错误 tab（`OpsErrorLogTable` 内部分页）两处启用；`OpsErrorLogTable` 亦被 `/admin/ops` 复用，同步获得跳页。合并：模板单属性插入，三路合并可自动处理。
- **10-15 qoder 出站平铺工具嵌套归一化（porter 同步）**：修平铺 function 工具出站 400（移植 qoder2api-hub `2fc18d6` / PR #30）——客户端直打 `/v1/chat/completions` 带平铺工具（`{"type":"function","name":...}`）时 `BuildAgentChatBody` 原样透传，上游要求 `tools[].function` 对象而 400。新增 `normalizeTools`（幂等，已嵌套原样透传、非对象条目丢弃、`strict` 保留、缺失 `description`/`parameters` 补默认）在 `BuildAgentChatBody` 的 tools 覆写处统一调用。responses/anthropic 桥产物本就是嵌套形态，不受影响。合并：纯二开文件。连带补 `pkg/qoder/umid_test.go` 缺失的 `//go:build unit` 标签。

### 模型能力测评（2026-10-15 ~ 10-17，已于 10-23 整体移除）

- 曾建立 so 私有的模型能力测评（benchmark）：`hard-v1` 动态题库（24→80→106 题型、难度 D3→D7、套件/评分版本演进 1.0.0→7.0.0、程序自动判分含 witness/coding_patch 判分器）、计划/手动执行/排行/模型全景 UI（最终形态为「模型能力中心」四 Tab 单页）；10-16 起去账号化改走公共网关回环（`model_benchmark_settings` 选回环 Key，迁移 243~247，采样数/max tokens 全局统一）；计分最终原则「执行失败与未执行题均 0 分计入分母」、排名完全开放（跑过即上榜、取最新成绩）。迁移 248 放宽预算约束、249 放宽难度 CHECK 至 3-7。
- 账号测试链路曾为测评植入 benchmark 感知分支（真实题目/输出预算/instructions/usage 事件/429/401 不改账号状态），上游同步敏感；移除时已全部还原为普通探测语义（见 10-23 条目）。
- 全部功能已于 **2026-10-23 整体移除**（代码/表/文档全清，迁移 250 DROP 三表并清 243~249 记录），详见当日条目；题库演进、判分规则与 UI 重构的逐版本记录见 git 历史与本次压缩前版本（HEAD=`82fc11f`）。

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
- **合并影响**：composite 解析/调度链为二开密集区（`gateway_scheduling.go`/`openai_account_scheduler.go`/`openai_gateway_scheduling.go` 与上游高冲突面重合），上游同步时按本条目语义重放；不推进 `UPSTREAM-BASE`。

### 2026-10-16 三平台重新授权 URL + Antigravity 用量主动刷新

- WorkBuddy/Qoder/Trae 平台账号的"重新授权"入口不再复用 Claude 授权链接，隐藏 `AccountActionMenu` 中这三平台的重新授权按钮（其凭据走平台专用 OAuth/刷新链路）；其它平台保持各自分支。
- `getAntigravityUsage` 支持 `force` 绕过 3 分钟缓存并交由主动查询链路；前端 Antigravity 用量单元格新增单独的主动查询按钮（`loadActiveUsage`），避免用量窗口长期不刷新或刷新结果失真。
- **改动文件**：`frontend/src/components/admin/account/AccountActionMenu.vue`、`frontend/src/components/account/AccountUsageCell.vue`、`backend/internal/service/account_usage_service.go`。
- **合并影响**：均为二开增量；不推进 `UPSTREAM-BASE`。

### 同步 v0.2.17（2026-10-16）

- **区间**：`UPSTREAM-BASE 4d9ac3e4... -> b0a02a33...`（v0.2.16 → v0.2.17），48 commits / 153 upstream files。
- **本次策略**：用户明确"支付链路不用，不同步"，故 `backend/payment/`、webhook、支付前端、支付测试等路径全部跳过；其余按建议 selective sync（110 文件覆盖/merge，32 跳过，11 个冲突手工裁决）。
- **优先同步项**：
  - 安全路由：composite OAuth 歧义路由拒绝含 OpenAI 网关 OAuth 的组合（`composite_route_resolver`、`gateway_service`、`gateway_handler`）。
  - OpenAI 兼容：GPT-5.5 retirement、Responses `created_at` 修补、Anthropic base URL honoring、可配置 min output token floor、Codex import workspace 隔离、web_search emulation、account eligibility。
  - Gemini/Antigravity：compat thinking、Vertex SA 已删除处理、共享容量 429 同号退避、错误策略单查、Gemini 4 Argon catalog/pricing、tool_result 图片、native functionCall/thoughtSignature、v1beta 路由。
  - Zhipu GLM Coding Plan 5h/weekly 窗口修正、account websearch config、config example、模型与计费价格表。
  - 前端：`useModelWhitelist`、Antigravity/AccountModal 兼容修改、AccountTableFilters 保持二开 dense 版（上游布局改动未覆盖）。
- **冲突裁决要点**：
  - `account.go`：保留二开"Antigravity 空映射直通 / 非空映射不强注默认条目"语义；同时带入上游 `antigravityDefaultPassthroughModels` + `isAntigravityInjectedDefaultPassthrough`。
  - `antigravity_gateway_service.go`：函数重命名为 `buildGeminiTestRequestWithPrompt`，整合图片模型分支；文本路径移除 `maxOutputTokens: 1`（防 thinking 假成功），保留上游 `toolConfig VALIDATED`。
  - `antigravity_gemini_thinking_variant*.go`：采用上游变体解析逻辑与测试。
  - `composite_*`、`gateway_service.go`：保持二开多平台候选集合语义，叠加上游 OpenAI Gateway OAuth 歧义标记。
  - `vertex_service_account.go`：带入 `vertexTokenRequestError` / `vertexServiceAccountNotFoundError`，移除 so 未实现的 Express mode 依赖。
  - `gemini_messages_compat_service.go`：保留上游 `isVertexSharedCapacity429` / `handleGeminiThirdParty429`，恢复 `parseGeminiRetryDelayResetTime`。
- **测试适配**：`openai_model_retirement_test.go`（platform constraint 类型），`pricing_service_test.go`（`LiteLLMModelPricing.CacheStorageCostPerTokenPerHour` + raw entry + conversion），补 `pricing_service.go` 字段。
- **版本号**：`VERSION` 已同步为 `0.2.17`；`UPSTREAM-BASE` 推进至 `b0a02a3304a0a78cdd003ddda1dd23ba02c03084`。
- **跳过/未同步**：支付链路（用户声明不使用）、README/sponsor/logo、en locale 包（so 已裁剪）、`AccountTableFilters.spec.ts`（上游删除、so 无）、PaymentView/StripePaymentView/AccountTestModal 路径差异、部分 cropped-module 测试。

### 2026-10-17 Antigravity 用量查询改为 retrieveUserQuotaSummary

- **问题现象**：账号列表里 Antigravity 账号的用量长期显示 0%（进度条永远满格/空白），刷新后仍是错误数据。
- **根因（真实账号实测）**：旧实现调用 `v1internal:fetchAvailableModels` 读取每个模型的 `quotaInfo.remainingFraction`。该接口对全部 34 个模型恒返回 `remainingFraction: 1`（即使账号实际已消耗额度），且它返回的是模型目录能力信息，不是账号用量。真实用量在 `v1internal:retrieveUserQuotaSummary`：按额度组（`Gemini Models` / `Claude and GPT models`）返回 `buckets[]`，每组包含 `5h` 与 `weekly` 两个窗口，各自带 `remainingFraction` 与 `resetTime`。CLIProxyAPI 生态（CPA-Manager-Plus 管理面板、lamplighter 插件）均以该接口为额度来源。
- **修复**：新增 `antigravity.Client.RetrieveUserQuotaSummary`（prod → daily URL 回退、403 归类为 ForbiddenError、1 MiB 读取上限）；`AntigravityQuotaFetcher.FetchQuota` 改走该接口，`UsageInfo` 新增 `antigravity_quota_groups`（组 key：`gemini` / `claude-gpt`，每桶含 window/utilization/reset_time）；`FiveHour` 兼容字段取首个 5h 桶。`fetchAvailableModels` 保留给模型目录同步（`upstream_models.go`），`antigravity_quota` 旧字段保留但注释标明不可作为用量来源。使用率由截断改为四舍五入（避免 `1-0.8` 算成 19%）。403 错误文案通过新增的 `ForbiddenError.Operation` 保持旧输出兼容。
- **前端**：Antigravity 单元格改为按额度组渲染 `Gemini 5h/7d`、`Claude 5h/7d` 四条进度条（`label-width="auto"`，5h 在前），保留旧 `antigravity_quota` 回退分支；刷新按钮由 `justify-end` 改为 `justify-start`（靠左）；新增 i18n key `usageWindow.gemini`。
- **渠道监控**：`usageQuotaTiers` 优先消费 `AntigravityQuotaGroups`（Window=5h/weekly，Label=组 key），旧 per-model `total/*` tier 仅保留给历史数据。
- **改动文件/模块**：`backend/internal/pkg/antigravity/client.go`、`backend/internal/service/antigravity_quota_fetcher.go`、`account_usage_service.go`、`channel_monitor_quota_fetcher.go`、`antigravity_quota_fetcher_test.go`、`channel_monitor_quota_fetcher_test.go`；`frontend/src/types/index.ts`、`components/account/AccountUsageCell.vue`、`components/account/__tests__/AccountUsageCell.spec.ts`、`i18n/locales/zh/admin/accounts.ts`。
- **验证**：真实账号直连 prod 端点跑通新链路（Gemini weekly 剩余 0.9387 → 6%，5h 0%，Claude 组独立）；`go build ./...`、`go test -tags=unit ./internal/service ./internal/pkg/antigravity` 相关用例 PASS；前端 `vue-tsc --noEmit`、ESLint、`vitest AccountUsageCell/UsageProgressBar` 72 项 PASS。
- **与上游合并影响**：涉及上游同名文件 `antigravity_quota_fetcher.go` / `account_usage_service.go` / `channel_monitor_quota_fetcher.go` / `pkg/antigravity/client.go`，下次上游同步需按二开语义手工合并；`UPSTREAM-BASE` 不变。

### 2026-10-20 推理档位全量透传：取消 max→xhigh 的模型白名单与降级

- **问题现象**：Claude Code `--effort max` 经 sub2api 转发到 WorkBuddy2API 的 `cn:deepseek-v4.1-flash` 时，`usage_logs.requested_reasoning_effort=max`，但实际出站 `reasoning_effort=xhigh`。WorkBuddy CN 档位表中该模型支持 `low/high/max`，xhigh 不在支持列表，上游会再降到 high，最终推理强度低于请求值。
- **根因**：通用 Anthropic→OpenAI（Responses / Chat Completions）桥接先把 Anthropic `max` 映射成 `xhigh`，再由 `openAICompatAnthropicReasoningEffort` 仅对白名单模型恢复 `max`；`cn:` 命名空间前缀使 `cn:deepseek-v4.1-flash` 未命中 `deepseek-v4*`/`deepseek-flash` 前缀，max 被保留为 xhigh。经用户确认改为全量透传，不再维护模型白名单。
- **修复**：①删除 `supportsOpenAIReasoningEffortMax` 白名单与 `openAICompatAnthropicReasoningEffort` 事后恢复逻辑；②`mapAnthropicEffortToResponses` 原样返回 effort；③`normalizeOpenAIReasoningEffort` 对 `xhigh`、`max` 分别原样返回（`extrahigh` 仍归一为 xhigh）；④模型名后缀推导的 `-xhigh` 不再经 `max` 中转，直接保留 xhigh。群组级 `max_reasoning_effort` 上限/映射策略保持不变（管理端显式配置，仍可按配置降级或拒绝）。
- **改动文件/模块**：`backend/internal/pkg/apicompat/anthropic_to_responses.go`、`backend/internal/service/openai_compat_model.go`、`openai_gateway_messages.go`、`openai_gateway_messages_chat_fallback.go`、`openai_gateway_request_body.go` 及对应测试。
- **验证**：容器内 `gofmt -l` 无输出；`go test -tags=unit ./internal/pkg/apicompat` 与 service 相关定向用例 PASS；全量 `./internal/service` 另有 4~5 个与本改动无关的既有失败（Gemini thinking variant / 复合分组族），未在本改动中处理。
- **与上游合并影响**：涉及上游同名文件，下次 `sub2api-upstream-sync` 需保留"请求 effort 全量透传"的二开语义；`UPSTREAM-BASE` 不变。

### 2026-10-20 部署修复：Dockerfile.bundle 入口脚本权限 + 245 迁移 checksum 兼容

- **事故背景**：推理档位透传改动首次部署两次失败并已即时回滚（累计中断约 6 分钟）：① fnOS 工作区源码文件 mode 恒为 000，`COPY` 进镜像后 `RUN chmod +x` 仅得 111（无读位），`/bin/sh` 无法打开 `/app/docker-entrypoint.sh`，容器循环重启；② 入口权限修复后，新二进制因 `245_so_model_benchmark_comparison.sql` checksum 漂移拒绝启动（生产库执行的是 2026-10-18 镜像构建时的早期版本，仓库其后又有修改）。
- **修复一（构建）**：`deploy/Dockerfile.bundle` 末段 `RUN chmod +x` 改为 `RUN chmod 755`，与源文件 mode 解耦。
- **修复二（迁移）**：按项目既有 `migrationChecksumCompatibilityRules` 机制为 245 增加兼容规则放行历史 checksum。部署前已对生产库全量审计：仓库 300 个迁移中仅 245 一处真实漂移；DB 另有 4 条 `238/239/240/241_so_drop_*` 记录其文件不在仓库与镜像中，runner 按「仅遍历文件侧」逻辑容忍，无需处理。（注：245 规则已随测评功能移除而删除，2026-10-23。）
- **部署验证**：新镜像先以生产环境变量 + 数据目录副本做独立容器冒烟（连真实库启动至 healthy、迁移校验通过、无错误日志），再 `docker compose up -d --force-recreate` 替换；正式容器 healthy、`/health` ok。回滚备份镜像：`sub2api:0.2.17-pre-effort-20261020`（原始）、`…-effort-broken-20261020`（入口权限故障）、`…-effort-broken2-20261020`（checksum 故障）。
- **改动文件/模块**：`backend/internal/repository/migrations_runner.go`（245 兼容规则，已删）、`deploy/Dockerfile.bundle`（chmod 755 + 头注释）。
- **与上游合并影响**：`migrations_runner.go` 与根 `Dockerfile` 均为上游同名文件/副本，下次同步需保留二开语义；`UPSTREAM-BASE` 不变。

### 2026-10-21 构建链修复：根 Dockerfile 在 NAS 全新缓存下的四处失败（含 qoder 第三批部署）

- **事故背景**：在 fnOS NAS 上构建 `sub2api:0.2.17-so-20261021`（含 qoder 三批移植）时，node 基础镜像更新导致前端层缓存全失效，暴露出 10-13「开源」提交引入的构建矛盾——此前所有"成功"构建均依赖 Docker 缓存掩盖，CI 全新环境同样会失败。
- **修复一（pnpm workspace）**：`frontend/pnpm-workspace.yaml` 只有 pnpm 10 语法的 `allowBuilds:` 缺 `packages:` 字段，pnpm 9.15.9 直接报 `packages field missing or empty`；补 `packages: ['.']`。
- **修复二（lockfile 与 overrides 失配）**：`package.json` 的 `pnpm.overrides`（10-13 引入）从未回写进 lockfile，`--frozen-lockfile` 报 `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`；在容器内以 pnpm 9.15.9 重新生成 `pnpm-lock.yaml`（settings 段补录 overrides，postcss 收敛至 8.5.29，37 增 52 删的最小 diff）。
- **修复三（执行位丢失）**：`backend/scripts/resolve-version.sh` 在二开 git init 时丢了执行位（git 100644，上游 100755），`./scripts/resolve-version.sh` 报 Permission denied；`git update-index --chmod=+x` 恢复。注意 fnOS fs mode 恒为 000，本地构建仍须传 `--build-arg VERSION=…` 绕过该脚本，git 修复仅惠及 CI/正常机器。
- **修复四（resources 000 权限）**：根 `Dockerfile` 的 `COPY … /app/resources` 继承 fnOS fs 的 000 mode，镜像内非 root 用户读取报 permission denied（启动日志 `[Pricing] Fallback merge skipped`；与 10-20 `Dockerfile.bundle` entrypoint 修复同一性质），补 `RUN chmod -R a+rX /app/resources`。
- **部署验证**：新镜像 `sub2api:0.2.17-so-20261021`（VERSION=0.2.17 / commit ed3b52a）经 `/vol2/1000/docker/sub2api/docker-compose.yml` 部署，容器 healthy、`/health` 200、Pricing 257 模型加载、三平台运营任务全部拉起；回滚镜像 `sub2api:0.2.17` 保留。
- **改动文件/模块**：`frontend/pnpm-workspace.yaml`、`frontend/pnpm-lock.yaml`、`backend/scripts/resolve-version.sh`（仅 git mode）、根 `Dockerfile`。
- **与上游合并影响**：`pnpm-workspace.yaml` 为二开独有文件；`pnpm-lock.yaml` 与根 `Dockerfile` 为上游同名文件，下次同步需保留 chmod 二开语义；`UPSTREAM-BASE` 不变。

### 2026-10-23 移除模型能力测评功能（保留模型清单）

- **描述**：整体移除模型能力测评（benchmark）功能，仅保留模型清单（模型⟷账号供给透视），且清单不再展示模型评分。删除内容覆盖后端服务/仓储/处理器/路由/设置项/账号测试挂钩、前端页面/组件/API/i18n、数据库表与迁移记录、设计稿与 openspec 提案。
- **改动文件**：
  - 后端删除：`internal/service/model_benchmark_*.go`（22 个，含题库 D3-D7/执行器/Runner/服务/类型）、`internal/repository/model_benchmark_repo*.go`、`internal/handler/admin/model_benchmark_handler*.go`、`migrations/243~249_so_model_benchmark*.sql` 与迁移测试。
  - 后端修改：`cmd/server/wire{,_gen,_gen_test}.go`、`internal/handler/{handler,wire}.go`、`internal/service/wire.go`、`internal/repository/wire.go`、`internal/server/routes/admin.go`（路由组 `/admin/model-benchmarks/*` 整体移除）、`setting_features.go`/`settings_view.go`/`domain_constants.go`（测评设置与常量）、`migrations_runner.go`（245 checksum 兼容规则随文件删除）、`model_manifest_service.go`（剥离 BenchmarkScore 注入）、`account_test_service{,_logging}.go`（移除 RunBenchmarkPrompt/benchmark_usage/benchmark_finish/benchmark 上下文与输出预算挂钩，恢复探测默认语义）、`openai_gateway_{qoder,trae,workbuddy}.go`（移除测评 payload 分支）、`account_test_service_openai_test.go`（删 3 个 benchmark 测试，保留探测默认值回归）。
  - 新增迁移：`250_so_drop_model_benchmark.sql`——既有库 DROP 三张 model_benchmark 表并清空 243~249 的 schema_migrations 记录；全新库为空操作，幂等。
  - 前端删除：`views/admin/ModelBenchmark{,Plans}View.vue`、`components/benchmark/`（6 组件）、`model-capability/tabs/{Overview,Plans}Tab.vue`、`api/admin/modelBenchmarks.ts`、`i18n zh/admin/modelBenchmark.ts`、相关 spec；`ModelManifestView.vue`（已被能力中心取代的死薄壳）。
  - 前端修改：`ModelCapabilityCenterView.vue`（仅剩清单，无 Tab）、`router/index.ts`（删 2 条测评 redirect，meta key 改 modelManifest）、`ManifestTab.vue`（删评分列/分数排序/tooltip）、`api/admin/{index,models}.ts`、i18n `modelManifest.ts`/`common.ts`（删 benchmark 键）、`AppSidebar.vue` 注释。
  - 文档删除：`docs/MODEL_CAPABILITY_BENCHMARK_*.md`（4 个）、`openspec/changes/add-model-capability-benchmark/`、`mockups/model-capability-benchmark-*` 与 `model-capability-center-ui.html`。
- **验证**：后端 `go build ./...` 通过；受影响包 unit 测试通过（service 包 5 个失败为存量——Gemini thinking variant/复合分组族，HEAD 基线 worktree 复跑证实与本次无关）；repository/handler/cmd/server/server 全绿。前端 vitest 286 文件 2124 测试全绿、`vue-tsc` 通过、`pnpm build`（含 i18n 完整性检查）通过。迁移 250 经临时 PG16 容器双场景验证（旧库清表清记录、新库幂等空操作）。
- **部署注意**：升级到含本变更的镜像后，启动时迁移 250 会自动 DROP 测评表（数据不保留）；如需留档测评历史请先备份 `model_benchmark_*` 三表。
- **与上游合并影响**：无（benchmark 全部为 so 私有功能，上游无对应文件；删除降低后续同步冲突面）。
- **编号占用提醒**：迁移 250 已被本 DROP 迁移占用；`ADVERSARIAL-SOLUTIONS.md` 中 AUTH-04（`250_so_user_token_version.sql`）等待实施方案实施时顺延使用 251+。
