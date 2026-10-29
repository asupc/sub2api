# sub2api-so 二开变更记录

> 规则：二开变更（无论是否经 `sub2api-upstream-sync` 技能）都必须记录于本文件，新条目追加到文件末尾，每条含：日期、功能描述、改动文件/模块、是否影响与上游合并。
> 基准：`UPSTREAM-BASE`（单行完整哈希）只在完整上游同步后前移；手动移植只记本文件、不改基准（唯一例外见 10-13 收口条目）。
> 整理记录：2026-10-12 / 10-13 / 10-15 / 10-23 / 10-26 五次压缩，只保留功能事实、关键决策、合并冲突面与已知缺口；根因推导、测试输出、逐提交哈希与逐文件清单见 git 历史（压缩前 HEAD：10-26=`145baaa`、10-23=`82fc11f`、10-15=`f5aa70c`、10-13=`45cf4c6`；10-23 起模型测评历史收敛为单条）。10-26 压缩把构建/部署流水收敛为「构建链与部署流水」节，PORTER-BASE 历史批次按功能事实收敛。

## 当前状态（2026-10-26 核实）

| 项 | 状态 |
|---|---|
| 上游基准 | `UPSTREAM-BASE`=`a3298b46`（v0.2.19）；`backend/cmd/server/VERSION`=0.2.19 |
| 移植源基准 | `PORTER-BASE` 文件：workbuddy `73a7560e`（上游 10-21 起停止维护）、qoder `0426c334`、trae `1707fae1` |
| 后端 | 代码 = v0.2.19 + 二开（三平台源码集成、Antigravity 直接模型模式、复合分组同族多平台调度、推理档位全量透传、OpenCode reasoning 回放收口、账号/计费/网关定制，逐项见时间线）；模型能力测评已于 10-23 整体移除 |
| 前端 | 二开：功能入口裁剪 + UI/UX 体系重构 + 仪表盘/用量统计/账号表格增强；模型能力中心收敛为模型清单单页（无评分）；用量统计含「按 API 统计用量」与模型分布折叠 |
| 数据库 | 官方迁移序列终态（裁剪期 drop 的表由 `deploy/restore-upstream-schema.sql` 恢复，可重放幂等）；二开迁移命名 `NNN_so_*.sql`，最新占用 250（测评表 DROP），后续从 251+ 起 |
| 验证基线 | 2026-10-29（同步 v0.2.19 后）：后端 `go build`/`go vet`/unit 仅 6 个已知存量失败（Gemini thinking variant / 复合分组族 ×2 / prompt-audit 路由覆盖 / antigravity 目录跳过 / effort 透传遗留，与 HEAD 基线一致）+ Trae 注册用例并行偶发；前端 typecheck + vitest（290 文件 / 2265 用例）+ build 全绿 |
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

- **当前基准（2026-10-26）**：workbuddy `73a7560e`（上游 2026-10-21 声明停止维护——官方收紧非官方客户端识别，后续协议跟进需自行评估；仓库已移至 `/vol2/1000/code/workbuddy2api-panel/original/`）、qoder `0426c334`（v1.7.6）、trae `1707fae1`。
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
