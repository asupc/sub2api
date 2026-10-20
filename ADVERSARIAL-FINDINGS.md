# Sub2API-SO 全功能对抗性测试 — 问题清单（终版）

- 生成日期：2026-10-17
- 范围：`sub2api-so` 全栈（后端 Go + 前端 Vue 3）
- 方法：6 个功能模块并行对抗性代码审查 + 单元测试运行；随后单 agent 核实（`ADVERSARIAL-VERIFY.md`）；主会话对存疑项独立复核（本地代码定位 + Docker `golang:1.27.2` 运行时验证）；最后交叉验证解决方案（`ADVERSARIAL-SOLUTIONS-VERIFY.md`）并据此修正本清单与 `ADVERSARIAL-SOLUTIONS.md`。
- 子报告：
  - `ADVERSARIAL-FINDINGS-auth.md`
  - `ADVERSARIAL-FINDINGS-gateway.md`
  - `ADVERSARIAL-FINDINGS-platform.md`
  - `ADVERSARIAL-FINDINGS-admin-data.md`
  - `ADVERSARIAL-FINDINGS-payment-etc.md`
  - `ADVERSARIAL-FINDINGS-frontend.md`

## 状态定义
- **CONFIRMED**：代码行为与描述一致，经主会话独立复核（含必要运行时验证）。
- **CONFIRMED-ADJUSTED**：问题存在，但严重度/定位/影响已按复核结果修正。
- **PLAUSIBLE**：静态推断成立，需并发/运行时复现确认。
- **REJECTED**：经复核不成立或为既定设计，不作为缺陷处理（保留记录避免重复提出）。

## 统计（已排除 REJECTED）
- 表格条目：64（`GW-04/COMP-01` 一行含 2 个 ID，展开后唯一 ID 65）
- High：4
- Medium：19
- Low：32
- Info：9
- REJECTED：4（PAY-08、PAY-12、PLAT-07、PLAT-10）
- 其中 PLAUSIBLE（需运行时复现）：GW-09、PLAT-11

## 关键修正（相对初版清单）
| 条目 | 初版 | 终版 | 修正依据 |
|---|---|---|---|
| PAY-12 | Medium | **REJECTED** | `UpdateConfig` 仅注册在 `admin.Group("/channel-monitor-v2")`（`routes/admin.go:946`），admin 组已挂 `adminAuth`，`validateJWTForAdmin` 校验 `IsAdmin()`（`admin_auth.go:204-208`）；用户端仅 GET 只读路由（`routes/user.go:146-157`）。 |
| PAY-08 | Medium | **REJECTED** | 运行时验证：`net.ParseIP("::ffff:127.0.0.1")` 被 Go 归一化为 4 字节 IPv4，`IsLoopback`/`IPNet.Contains` 均命中；`::ffff:169.254.169.254`、`::ffff:10.0.0.1` 同样命中现有阻断规则。 |
| PLAT-02 / PAY-09 | Medium | **合并入 GW-04/COMP-01** | 两条为同一根因（`resolveCompositeModelOwnership` 与 resolver 的 `Ambiguous` 语义），终版不再单列，修复时并入 GW-04/COMP-01 处理。 |
| PLAT-08 | Low | **Info** | `sendCCUpstreamRequest` 的 qoder 分支（`cc_pipeline.go:349-354`）已统一调用 `sendQoderChat`（含换主机重试），账号测试与 CC pipeline 均覆盖；剩余缺口仅为 `qoderChatURL` 解析失败时不尝试其他候选主机。 |
| PLAT-07 | Low | **REJECTED** | 当前 `choiceOf`（`index > maxChoice`）与 `Finish`（`i <= maxChoice`）已正确覆盖 index 0；建议的 `>=` 反而引入 off-by-one。 |
| PLAT-10 | Low | **REJECTED** | `budget 0 → low` 是文件内注释明确的设计（上游无“无思考”变体，取最浅档并原样透传 budget），测试也期望 low。 |
| GW-04/COMP-01 | High | **Low** | 失败测试是上游 v0.2.17 带入的旧语义（期望 `Ambiguous:true`），与二开“同网关族多平台合并调度”冲突（`CHANGELOG-SO.md` 2026-10-16 条目）。`Ambiguous` 分支在**生产路径**不可达（仅测试可达），但跨族歧义仍由 `CompositePlatformsShareGatewayFamily` fail-closed 兜底。需产品决策 + 修测试，而非设置字段。 |
| PLAT-01 | Medium | **Low** | 根因是二开有意删除 `ensureAntigravityDefaultPassthroughs` 运行时注入（`account.go:680-688`），失败测试期望旧行为。属既定设计下的测试过期；仅“持久化裸名映射 + 仅配置 -high”时 low 请求退化为 high，影响可接受。 |
| AUTH-01 | Medium | **Info** | 多余 JWT 仅在 `AuthHandler.Login` 局部变量中被丢弃，未返回给客户端，无可利用面；仅为 CPU 浪费与误导性代码。 |
| ADMIN-01 | Medium | **Low** | 全部 handler 经 DTO 映射，未发现直接序列化 `dbent.User`；`ent/user.go` 为生成代码，JSON tag 为潜在风险而非已暴露漏洞。 |
| ADMIN-06 | Medium | **Low** | API Key 采用明文存储设计（`ent/schema/api_key.go` `field.String("key")`），列表仅返回调用者本人/管理员可见的 key；前端 `KeysView.vue` 依赖列表值复制。非跨用户泄露。 |
| ADMIN-08 | Medium | **Low** | 已有现场 TOTP、禁止 admin API key、清空后留痕（`audit_log_handler.go:104-168`），且已有 `audit_log_retention_days`（默认 180 天）与每日清理；真正缺口是 `ClearAll` 不受保留期约束、可一次抹掉近期证据，且无双控/归档。 |
| PLAT-04 | Medium | **Low** | usage 字段只累加不递减，`int(v) > 0` 跳过的 0 值最终结果与默认 0 相同；实际影响仅为“显式 0 不覆盖旧值”，无累计错误。 |
| PLAT-05 | Medium | **Low** | `consumeClaudeData` 在 `event.Type == ""` 时回退使用 event 行类型；仅当上游省略 JSON `type` 且跨 chunk 拆行时误判。 |
| PLAT-06 | Medium | **Low** | Anthropic SSE 标准为 `data: ` 带空格；`extractSSEUsage` 精确匹配仅影响非标准上游。 |
| PLAT-09 | Medium | **Low** | Gemini `functionCall/functionResponse` 协议本身无 id，剥离是正确行为；请求侧 `toolUseIDToName` 已做 name 关联。 |
| PAY-02 | Medium | **Low** | legacy `sub2_<id>` 回退仍要求 provider 验签成功；攻击者无法在无签名密钥情况下构造有效通知。 |
| PAY-04 | Low | **Low** | 0.01 容差为既定产品语义；仅小额短付风险，且需支付渠道配合。 |
| PAY-06 | Medium | **Low** | 负值是明确退款功能（`redeem_service.go:206` 注释），不应全局拒绝；仅需权限/类型约束。 |
| FE-03 | High | **Low** | 模板中 `href`/`iframe` 仅在 `isValidUrl`（要求 http/https 前缀）为真时渲染（`CustomPageView.vue:80,97-121`），`javascript:` 不会进入绑定分支。 |
| GW-11 | Medium | **Info** | Responses WS 首帧与每个 `response.create` 均校验模型（`openai_gateway_handler.go:2576-2585,3068-3078`），Grok Realtime query model 由中间件校验。 |
| GW-10 | Low | **Info** | 已有 `usage_billing_dedup(request_id, api_key_id, request_fingerprint)` 幂等表（`migrations/071`），重复计费需先验证指纹覆盖。 |
| AUTH-09 | Low | **Low** | 项目已有 `GetSecurityClientIP` + trusted proxies，但 `isRequestHTTPS` 仍直接读头；属复用既有机制的小修。 |

## 发现总表

### High
| ID | 模块 | 标题 | 位置 | 状态 |
|---|---|---|---|---|
| GW-01 | 网关 | 内容审核在服务/配置异常时 fail-open | `handler/content_moderation_helper.go:61-67`；`service/content_moderation.go:824-845,1075-1115` | CONFIRMED |
| GW-02 | 网关 | inflight 余额预留对未定价模型/Redis 故障 fail-open | `handler/gateway_inflight_reservation.go:80-85`；`service/billing_inflight_reservation.go:257-278` | CONFIRMED |
| ADMIN-07 | 管理 | Setup 将生成的管理员密码打印到 stdout | `internal/setup/setup.go:445` | CONFIRMED |
| FE-01 | 前端 | 首页管理配置 HTML 经 `v-html` 无净化渲染 | `frontend/src/views/HomeView.vue:11-12` | CONFIRMED |

### Medium
| ID | 模块 | 标题 | 位置 | 状态 |
|---|---|---|---|---|
| AUTH-02 | 认证 | 注册/验证码接口可通过 `ErrEmailExists` 枚举邮箱 | `service/auth_service.go:216-218,323-325,363-366` | CONFIRMED |
| AUTH-03 | 认证 | 空 `BindingHash` 旧 Token 永久绕过会话绑定 | `middleware/session_binding.go:68-73` | CONFIRMED |
| AUTH-04 | 认证 | “撤销所有会话”不撤销已签发 Access JWT | `service/auth_service.go:1895-1911` | CONFIRMED |
| AUTH-05 | 认证 | 2FA 临时会话在后续失败路径不清除，可重放 | `handler/auth_handler.go:301-413` | CONFIRMED |
| GW-03 | 网关 | API Key 限流预检 fail-open + 预检/扣减 TOCTOU | `service/billing_cache_service.go:575-633`；`api_key_service.go:1158-1203` | CONFIRMED |
| GW-05 | 网关 | OAuth 429 同账号重试不受 retryLimit 约束 | `handler/failover_loop.go:79-105` | CONFIRMED |
| GW-06 | 网关 | 限流默认 fail-open；网关入口未显式选择失败策略 | `middleware/rate_limiter.go:129-150`；`server/routes/auth.go:36-69` | CONFIRMED |
| GW-07 | 网关 | 计费/用量同步回退失败后无持久化重试（静默丢失） | `service/billing_cache_service.go:192-214,378-397,690-699`；`api_key_service.go:1185-1200` | CONFIRMED |
| PAY-01 | 支付 | Webhook 在验签前用未验签 body 选择 provider 实例 | `handler/payment_webhook_handler.go:86-116`；`service/payment_webhook_provider.go:32-83` | CONFIRMED |
| PAY-03 | 支付 | 已取消订单无时间限制、已过期订单宽限期内可被恢复为已支付 | `service/payment_fulfillment.go:150-214` | CONFIRMED |
| PAY-07 | 公告 | 公告内容原样存储，后端无 HTML/Markdown 清洗 | `service/announcement_service.go:71-210` | CONFIRMED |
| ADMIN-02 | 管理 | 最后管理员保护读-写 TOCTOU | `service/admin_user.go:162-178` | CONFIRMED |
| ADMIN-03 | 管理 | `DeleteAccount` 影子/母账号删除非原子 | `service/admin_account.go:1312-1327` | CONFIRMED |
| ADMIN-04 | 管理 | `ClearAccountError` 多次写入无事务 | `service/admin_account.go:1338-1355` | CONFIRMED |
| ADMIN-05 | 管理 | `BatchDeleteProxies` 在“使用中检查”与删除之间 TOCTOU | `service/admin_proxy.go:172-205` | CONFIRMED |
| FE-02 | 前端 | Access Token 被拼进第三方 iframe URL query | `frontend/src/utils/embedded-url.ts:29-30`；`views/user/CustomPageView.vue:245-254` | CONFIRMED |
| FE-05 | 前端 | Access/Refresh Token 均持久化在 localStorage | `frontend/src/stores/auth.ts:49-57,214-226` | CONFIRMED |
| FE-07 | 前端 | 风控路由守卫在公共设置加载失败时 fail-open | `frontend/src/router/index.ts:613-628` | CONFIRMED |
| FE-08 | 前端 | SVG 上传原样 emit，净化仅用于本组件预览 | `frontend/src/components/common/ImageUpload.vue:140-146` | CONFIRMED |

### Low
| ID | 模块 | 标题 | 位置 | 状态 |
|---|---|---|---|---|
| AUTH-06 | 认证 | `BackendModeAuthGuard` 用子串匹配放行 pending OAuth 路径 | `middleware/backend_mode_guard.go:73` | CONFIRMED |
| AUTH-07 | 认证 | 邮箱验证开启时注册提交跳过人机验证码二次校验 | `service/auth_service.go:396-405` | CONFIRMED |
| AUTH-08 | 认证 | 密码重置先消费 Token 再检查用户存在 | `service/auth_service.go:1625-1638` | CONFIRMED |
| AUTH-09 | 认证 | `isRequestHTTPS` 直接信任 `X-Forwarded-Proto`（未复用可信代理解析） | `handler/auth_linuxdo_oauth.go:1012-1018` | CONFIRMED-ADJUSTED |
| AUTH-10 | 认证 | 上下文缺失时签发无会话绑定 Token | `service/auth_service.go:1408-1417` | CONFIRMED |
| GW-08 | 网关 | 并发等待计数/API Key 槽位在 Redis 不可用时 fail-open | `service/concurrency_service.go:417-455,495-508` | CONFIRMED |
| ADMIN-10 | 管理 | 计划任务 handler 直接序列化 service 结构（可能含上游响应文本） | `handler/admin/scheduled_test_handler.go:40-55,144-162` | CONFIRMED |
| GW-04/COMP-01 | 网关/复合 | `Ambiguous` 分支在**生产路径**不可达（仅测试可达）；失败测试与二开同族合并语义冲突 | `service/gateway_service.go:1499-1545`；`composite_route_resolver.go:142-176`；`composite_platform_test.go:110-129` | CONFIRMED-ADJUSTED |
| GW-09 | 网关 | inflight 引用计数 `Acquire` 与 `Release` 之间竞态窗口 | `service/billing_inflight_reservation.go:79-129` | PLAUSIBLE |
| GW-12 | 网关 | 空账号映射时 `/v1/models` 回落平台默认模型列表 | `handler/gateway_handler.go:1194-1218`；`service/gateway_service.go:1455-1478` | CONFIRMED |
| PLAT-01 | 平台 | 持久化裸名映射 + 仅配置部分变体时，low 请求退化 high | `service/antigravity_gemini_thinking_variant.go:120-184`；`account.go:680-688` | CONFIRMED-ADJUSTED |
| PLAT-03 | 平台 | 跨族所有权 reason 文案与测试期望不一致 | `service/composite_route_resolver.go:14-17,162-167` | CONFIRMED |
| PLAT-04 | 平台 | `extractSSEUsage` 以 `>0` 跳过零值 | `service/antigravity_gateway_upstream.go:329-349` | CONFIRMED-ADJUSTED |
| PLAT-05 | 平台 | Claude SSE `eventType` 跨事件残留 | `service/antigravity_gateway_compat_stream.go:196-207` | CONFIRMED-ADJUSTED |
| PLAT-06 | 平台 | `extractSSEUsage` 仅识别 `data: `（带空格） | `service/antigravity_gateway_upstream.go:309-313` | CONFIRMED-ADJUSTED |
| PLAT-09 | 平台 | Gemini compat 剥离函数调用 id（协议无 id） | `service/gemini_messages_compat_service.go:3493-3521` | CONFIRMED-ADJUSTED |
| PLAT-11 | 平台 | 复合所有权缓存 TTL 内可能过期，且无生产失效调用 | `service/gateway_service.go:1499-1545,1599-1652` | PLAUSIBLE |
| PAY-02 | 支付 | legacy `sub2_<id>` 订单 ID 回退 | `service/payment_fulfillment.go:44-76` | CONFIRMED-ADJUSTED |
| PAY-05 | 兑换 | Redis 不可用时兑换锁与失败计数降级（DB 乐观锁仍生效） | `service/redeem_service.go:334-384`；`repository/redeem_code_repo.go:324-340` | CONFIRMED-ADJUSTED |
| PAY-04 | 支付 | 金额容差对 <=2 位小数货币统一 0.01 | `service/payment_fulfillment.go:119-125`；`payment/currency.go:67-115` | CONFIRMED-ADJUSTED |
| PAY-06 | 兑换 | 兑换码创建允许负值（退款为既定功能） | `service/redeem_service.go:205-210,264-270` | CONFIRMED-ADJUSTED |
| PAY-10 | 图像 | 批量图像状态机允许任意状态转 failed、终态转 output_deleted | `service/batch_image.go:371-416` | CONFIRMED |
| ADMIN-01 | 管理 | 生成的 Ent `User` 带 `password_hash` JSON tag（潜在） | `ent/user.go:29` | CONFIRMED-ADJUSTED |
| ADMIN-06 | 管理 | API Key 列表返回完整明文 key | `handler/dto/mappers.go:80-87` | CONFIRMED-ADJUSTED |
| ADMIN-08 | 管理 | `ClearAll` 可绕过节流保留期一次清空近期审计（无双控/归档） | `handler/admin/audit_log_handler.go:104-168` | CONFIRMED-ADJUSTED |
| ADMIN-09 | 管理 | 迁移 checksum 兼容例外削弱不可变性 | `repository/migrations_runner.go:74-102` | CONFIRMED |
| ADMIN-11 | 管理 | Promo `MaxUses=0` 表示无限 | `handler/admin/promo_handler.go:32-45` | CONFIRMED-ADJUSTED |
| ADMIN-12 | 管理 | 管理员自降级保护只在 handler 层 | `handler/admin/user_handler.go:326-331`；`service/admin_user.go:197-290` | CONFIRMED |
| FE-03 | 前端 | `buildEmbeddedUrl` catch 分支返回原始 URL（防御性缺口） | `utils/embedded-url.ts:43-45` | CONFIRMED-ADJUSTED |
| FE-04 | 前端 | KeyUsageView `nextTick` 后仍可能启动新 RAF | `views/KeyUsageView.vue:560-600,955-958` | CONFIRMED |
| FE-06 | 前端 | 后端模式路由白名单用无边界 `startsWith` | `router/index.ts:510-513` | CONFIRMED |
| FE-11 | 前端 | 首页 iframe URL 仅前缀判断 | `views/HomeView.vue:496-499` | CONFIRMED |

### Info
| ID | 模块 | 标题 | 位置 | 状态 |
|---|---|---|---|---|
| AUTH-01 | 认证 | 登录路径重复签发并丢弃一个 Access Token（未返回客户端） | `service/auth_service.go:557-561`；`handler/auth_handler.go:252-257` | CONFIRMED-ADJUSTED |
| AUTH-11 | 认证 | 管理员 API Key 认证固定映射到首个管理员 | `middleware/admin_auth.go:140-153` | CONFIRMED |
| PLAT-08 | 平台 | Qoder `qoderChatURL` 解析失败时不尝试其他候选主机（发送层已重试） | `service/openai_gateway_qoder.go:63-68`；`openai_gateway_cc_pipeline.go:349-354` | CONFIRMED-ADJUSTED |
| GW-10 | 网关 | 流中断/客户端重试的重复计费风险（已有 dedup 表） | `repository/usage_billing_repo.go:66-110` | CONFIRMED-ADJUSTED |
| GW-11 | 网关 | WS 白名单由 handler 逐帧校验（中间件跳过） | `middleware/group_model_allowlist.go:45-49,95-104` | CONFIRMED-ADJUSTED |
| PAY-11 | 配额 | 配额视图月度重置为 30 天滚动窗口（既定语义） | `handler/quotaview/helpers.go:76-104` | CONFIRMED-ADJUSTED |
| FE-09 | 前端 | `any` 滥用（`: any` 268 / `as any` 159） | 全局 | CONFIRMED-ADJUSTED |
| FE-10 | 前端 | Markdown 复制按钮监听器未显式移除 | `views/user/CustomPageView.vue:388-411` | CONFIRMED |
| FE-12 | 前端 | setup 顶层直接访问 `window`/`document` | `views/HomeView.vue:502`；`views/KeyUsageView.vue` | CONFIRMED |

### REJECTED（不成立 / 既定设计）
| ID | 初版标题 | 不成立依据 |
|---|---|---|
| PAY-08 | 渠道监控 SSRF 未覆盖 IPv4-mapped IPv6 | Go `net.ParseIP` 已归一化；运行时验证 loopback/link-local/RFC1918 全部命中现有规则。 |
| PAY-12 | 渠道监控 v2 `UpdateConfig` 无管理员校验 | 路由仅挂在带 `adminAuth` 的 admin 组，`validateJWTForAdmin` 显式校验 `IsAdmin()`；用户端无写路由。 |
| PLAT-07 | Workbuddy 聚合器 `>=` 边界缺陷 | 现有 `>` / `<=` 组合正确；建议的 `>=` 会引入 off-by-one。 |
| PLAT-10 | Gemini thinking budget 0 → low 是缺陷 | 文件注释明确“上游无无思考变体，取最浅档并原样透传 budget”，测试亦期望 low。 |

## 测试基线
- 后端在 Docker `golang:1.27.2` 下复现的失败用例：
  - `TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants`（旧语义 vs 二开同族合并）
  - `TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership`（reason 文案）
  - `TestResolveGeminiThinkingVariant/injected_default_variants_are_usable`（默认透传注入已删除）
  - `TestResolveGeminiThinkingVariant/persisted_default_bare_identity_passthrough_still_resolves_when_variants_exist`（同上）
- 前端：`pnpm typecheck` 通过；`pnpm test:run` 有 KeyUsageView 动画帧 teardown 未处理异常（与 FE-04 同源）。

## 结论
- 真实且需修复的 High 共 4 条：GW-01、GW-02、ADMIN-07、FE-01。
- Medium 19 条集中在认证会话、网关失败策略、支付状态机、管理后台原子性、前端 token 存储。
- Low 32 条、Info 9 条；PLAT-08 经末轮复核降为 Info（发送层已重试）。
- PLAT-02 / PAY-09 为 GW-04/COMP-01 的重复根因，终版合并处理。
- 初版中 4 条为误报/既定设计，已剔除；7 条严重度下调，避免误导修复优先级。
- 所有修复方案见 `ADVERSARIAL-SOLUTIONS.md`（已按本终版清单重写）。
