# Sub2API-SO 对抗性测试 — 详细解决方案（终版）

- 对应清单：`ADVERSARIAL-FINDINGS.md`（终版，64 个表格条目 / 展开后 65 个唯一 ID；`GW-04/COMP-01` 合并为一章）
- 交叉验证：`ADVERSARIAL-VERIFY.md`（清单核实）、`ADVERSARIAL-SOLUTIONS-VERIFY.md`（方案核实）
- 去重说明：`PLAT-02` 与 `PAY-09` 为 `GW-04/COMP-01` 的重复根因，统一在 `GW-04/COMP-01` 章节处理；4 条 REJECTED（PAY-08、PAY-12、PLAT-07、PLAT-10）不提供修复方案。
- 原则：最小改动、分层正确（handler → service → repository）、默认安全但保留配置开关、事务与原子方法落在 repository、不破坏二开既定语义（多平台同族合并、退款负值、30 天滚动窗口等）

## 优先级
- **P0（立即）**：GW-01、GW-02、ADMIN-07、FE-01
- **P1（本迭代）**：AUTH-02~05、GW-03、GW-05、GW-06、GW-07、PAY-01、PAY-03、PAY-07、ADMIN-02~05、FE-02、FE-05、FE-07、FE-08
- **P2（规划）**：其余 Low/Info（含 PAY-05）与需产品决策项（GW-04/COMP-01、PLAT-01、PAY-06、PAY-11）

---

## P0

### GW-01 内容审核 fail-open（High）
- **根因**：错误在 service 层被吞掉：
  - `ContentModerationService.Check` 在 `s == nil || settingRepo == nil || repo == nil`、`loadRuntimeSnapshot` 失败、功能关闭时均 `return allow, nil`（`service/content_moderation.go:824-853`）。
  - `checkSync` 在 `callModeration` 出错时 `return allow`（同文件 `:1090-1112`）。
  - handler 层 `runContentModeration` 的 `err != nil` 分支因此几乎不可达（`content_moderation_helper.go:61-67`）。
- **方案**（必须改 service 层）：
  1. `ContentModerationConfig` 增加 `FailClosed bool`（JSON/config 字段 `fail_closed`），默认 **false** 保持现有部署行为；生产建议开启。
  2. 仅在 `cfg.Enabled && cfg.Mode == pre_block` 且 `FailClosed` 为真时，将“配置加载失败 / 审计引擎调用失败 / 依赖不可用”转换为 `Decision{Allowed:false, Blocked:true, Action:block, StatusCode:cfg.BlockStatus}`。
  3. `async`/`log_only` 模式始终 fail-open（审核是旁路，不应阻断请求）。
  4. 两类失败分别计数打点（`config_load_failed` / `engine_call_failed`），沿用现有 `recordPreBlockSyncMetric(..., ContentModerationActionError)`。
  5. handler 层保留 `err` 分支，但改为按同一策略返回 503/403，而不是 `nil`。
- **涉及文件**：`service/content_moderation.go`、`handler/content_moderation_helper.go`、`admin` 设置视图（新增开关）。
- **测试**：单测用 stub 让 `loadRuntimeSnapshot` 返回错误、让 `callModeration` 返回错误，断言 pre_block+FailClosed 拒绝、async 放行。
- **兼容性**：默认关闭，无行为变更；开关随配置热更新。

### GW-02 inflight 预留 fail-open（High）
- **根因**：`reserveInflight`（`service/billing_inflight_reservation.go:250-278`）在以下情况直接放行且不登记预留：
  - `estimate <= 0`（含 `priced=true` 但估算为 0）
  - `GetUserBalance` 失败
  - `ReserveInflightBalance` 返回错误
  - 已有配置 `billing.inflight_reservation.fail_closed_on_unpriced`（默认 false，仅覆盖“未定价”）
- **方案**：
  1. 复用现有 `FailClosedOnUnpriced`，不改默认值；在 `handler/gateway_inflight_reservation.go:80-85` 的“无法定价”分支，当其开启时拒绝（当前已部分实现，需确认 estimate=0 路径也走同一判断）。
  2. 新增 `InflightReservationConfig.FailClosedOnCacheError bool`（`mapstructure:"fail_closed_on_cache_error"`，默认 false）：Redis/余额读取失败时按配置拒绝 `503`，并在 handler 映射为 `ErrInsufficientBalance`/服务不可用。
  3. `estimate <= 0` 且 `priced` 为真时，用 `MaxReservationUSD` 或 `DefaultMaxTokens × 输出单价` 的保守下限兜底；仍无法估算则按 `FailClosedOnUnpriced` 处理。
  4. 拒绝路径写审计/指标（`inflight_reservation_rejected_total{reason}`）。
- **涉及文件**：`service/billing_inflight_reservation.go`、`handler/gateway_inflight_reservation.go`、`config/config.go`（含 `viper.SetDefault`）。
- **测试**：并发压测（同一用户余额仅够 1 个请求，发起 10 个并发，断言放行数 ≤ 1）；Redis 故障注入断言按开关拒绝。
- **注意**：多实例下预留由 Redis 原子完成，方案不引入本地状态。

### ADMIN-07 Setup 打印管理员密码（High）
- **根因**：`internal/setup/setup.go:445` `fmt.Printf("Generated admin password (one-time): %s\n", ...)`，容器日志/common runtime log 会持久化。
- **方案**（最小改动 + 兼容首次部署）：
  1. 默认**不打印密码**；改为写入 `cfg.Admin.PasswordFile`（默认 `<data_dir>/initial-admin-password.txt`），权限 `0600`，并在 stdout 只打印“密码已写入 <路径>，首次登录后请删除”。
  2. 增加显式 flag/env `SETUP_PRINT_ADMIN_PASSWORD=1`（或 `--print-admin-password`）时才打印，供临时/CI 使用。
  3. 首次成功登录后删除该文件（或在 `RecordSuccessfulLogin` 首次调用时清理）。
  4. 更新部署文档（docker-compose/README）说明取值方式。
- **涉及文件**：`internal/setup/setup.go`、`config/config.go`、部署文档。
- **测试**：断言 stdout 不含密码；文件存在且权限正确；登录后文件被删除。

### FE-01 首页 HTML `v-html` 无净化（High）
- **根因**：`frontend/src/views/HomeView.vue:11-12` 直接 `v-html="homeContent"`，`homeContent` 来自公共设置 `home_content`。
- **方案**：
  1. 引入已存在的依赖 `dompurify`（`frontend/package.json:26`）与现成封装 `frontend/src/utils/sanitize.ts`，新增 `sanitizeHtml()`（`USE_PROFILES: { html: true }`，可按需配置 `ADD_ATTR: ['target']`）。
  2. 渲染改为 `v-html="sanitizedHomeContent"`。
  3. 增加开关 `home_content_sanitize`（公共设置，默认 **true**）：管理员若确需 `<script>`/`<iframe>` 自定义首页，可显式关闭；关闭时前端展示风险提示（admin 设置页）。
  4. `isHomeContentUrl` 分支保持 iframe 模式不变（iframe 本身跨域隔离）。
- **涉及文件**：`views/HomeView.vue`、`utils/sanitize.ts`、`i18n`（管理员提示文案）。
- **测试**：Vitest 用 `<img src=x onerror=alert(1)>`、`<script>` 断言被剥离；`http(s)` URL 仍走 iframe。
- **注意**：DOMPurify 会移除内联事件与 `<script>`，属预期安全收敛。

---

## P1（Medium）

### AUTH-02 邮箱枚举
- **根因**：`RegisterWithVerification`、`SendVerifyCode`、`SendVerifyCodeAsync` 返回 `ErrEmailExists`（`auth_service.go:216-218,323-325,363-366`）。
- **方案**：
  1. 公开入口统一返回 `200 {"message":"如果该邮箱可注册，我们已发送验证码"}`，不区分存在/不存在。
  2. 保留“发信前查重”以节约发信配额（现有注释 `:357` 的意图）——即内部仍短路，但**响应与耗时对齐**（存在时执行同量级的 dummy 分支 + 固定 200-400ms 抖动）。
  3. 注册提交阶段保留真正的 `ErrEmailExists`（此时用户已完成邮箱所有权验证，不构成匿名枚举）。
  4. 增加 IP/邮箱维度的发码频率限制（已有 `validateRegistrationEmailQuota`，确认覆盖）。
- **涉及**：`service/auth_service.go`、`handler/auth_handler.go`、前端注册页提示。
- **测试**：相同响应体、响应时间差 < 阈值；数据库查询路径不因分支产生可观测差异。

### AUTH-03 空 BindingHash 绕过会话绑定
- **根因**：`middleware/session_binding.go:68-73` 对 `claims.BindingHash == ""` 直接放行。
- **方案**：
  1. 在 `JWTClaims` 增加 `BindingVersion int`（新签发为 1；旧 token 为 0）。
  2. 中间件：`BindingVersion == 0` 视为旧 token，仅在配置 `security.session_binding.legacy_grace_days > 0` 且 token `IssuedAt` 在宽限期内放行；否则 401 并撤销 refresh 家族。
  3. 刷新轮转时用新版 claims 重签，天然补齐指纹。
  4. 增加配置项与启动日志（宽限期剩余时间），便于运维判断。
- **涉及**：`service/auth_service.go`（claims 与新签发）、`middleware/session_binding.go`、`config`。
- **测试**：旧 token 在宽限期内/外分别放行/拒绝；刷新后新 token 绑定生效。

### AUTH-04 撤销所有会话不撤销 Access JWT
- **根因**：`RevokeAllUserTokens` 仅清 refresh sessions；`TokenVersion` 由 `email+password_hash` 指纹推导，`users` 表无独立列（`auth_service.go:1895-1911`）。
- **方案**（两阶段，避免大改 DB）：
  1. 短期：维持现状，文档明确“Access Token 短 TTL（`access_token_expire_minutes`）是撤销边界”。
  2. 推荐：新增 `users.token_version BIGINT NOT NULL DEFAULT 0` 列（migration `250_so_user_token_version.sql`），`resolvedTokenVersion` 优先读列值；`RevokeAllUserTokens` 执行 `UPDATE users SET token_version = token_version + 1`。改密路径同步自增。
  3. 中间件已有 `claims.TokenVersion != user.TokenVersion` 校验，无需改。
  4. 迁移期间旧 token（无列值）按现有指纹推导值处理，保证不误杀。
- **涉及**：`migrations/`、`ent/schema/user.go` + `go generate ./ent`、`service/auth_service.go`、`service/user_service.go`。
- **测试**：撤销后旧 access token 立即 401；刷新 token 同步失效；改密/撤销并发安全。

### AUTH-05 2FA 临时会话失败路径可重放
- **根因**：`Login2FA` 仅在全部步骤成功后 `DeleteLoginSession`（`handler/auth_handler.go:412-413`）；`GetByID`/backend mode/pending OAuth 绑定任一失败都保留会话。
- **方案**：
  1. TOTP 验证通过后，立即将临时会话标记为 `consumed`（`SETNX` 或删除），并保留一个短 TTL（60s）的“已完成上下文”用于 pending OAuth 绑定重试。
  2. 失败路径（用户查询/绑定失败）也删除临时 token，返回统一错误，要求重新登录；避免无限重试。
  3. `PendingOAuthBind` 流程单独用自己的 `pendingSession` 重试语义，不复用 2FA 临时 token。
- **涉及**：`service/totp_service.go`（登录会话原子消费）、`handler/auth_handler.go`。
- **测试**：TOTP 通过后强制绑定失败，再次提交同一 `temp_token` 返回 401/400；正常流程仍成功。

### GW-03 API Key 限流 fail-open + TOCTOU
- **根因**：
  - `checkAPIKeyRateLimits` 在 cache 未配置 loader、DB 读取失败时 `return nil`（`billing_cache_service.go:576-597`）。
  - 用量通过 `QueueUpdateAPIKeyRateLimitUsage` 异步累加，预检与扣减天然非原子（同文件 `:690-699`）。
- **方案**：
  1. **预检与扣减合一**：在 Redis 用 Lua 脚本实现“读取窗口用量 + 判断限额 + 预占本次估算成本”，窗口键沿用现有 5h/1d/7d key；失败返回明确错误。若暂不引入 Lua，则至少在请求结束的用量提交路径做“原子累加后复核”，超限写审计并对后续请求熔断（软限）。
  2. 区分故障类型：Redis 不可用默认保持 fail-open（可用性优先），但增加 `billing.api_key_rate_limit.fail_closed_on_cache_error`（默认 false）；**DB 错误时**在 loader 存在的情况下返回错误（不得静默放行）。
  3. 记录 `rate_limit_check_failed_total` 与 `rate_limit_overage_total` 指标。
- **涉及**：`service/billing_cache_service.go`、`service/api_key_service.go`、`config`。
- **测试**：并发 100 请求、限额 10，断言最终放行 ≤ 10 + 估算误差；DB 错误注入断言按配置拒绝。

### GW-05 OAuth 429 同账号重试无上限
- **根因**：`sameAccountRetryAllowed` 在 `SameAccountRetryDeadline` 非零时直接 `return true`（`handler/failover_loop.go:97-101`）。
- **方案**：
  1. 保留 deadline 语义，但叠加硬上限：`retryCount < max(SameAccountRetryMax, 配置 same_account_retry_hard_limit)`（默认 5）。
  2. 同时限制累计等待时间（`≤ 2 × deadline 剩余` 或配置上限），避免长时间阻塞。
  3. 超限后转入账号切换/失败返回，并记录 `same_account_retry_exhausted`。
- **涉及**：`handler/failover_loop.go`、`config`（新增上限）。
- **测试**：模拟持续 429 且 deadline 很长，断言重试次数 ≤ 硬上限。

### GW-06 限流失败策略
- **根因**：`LimitWithOptions` 默认 `RateLimitFailOpen`（`middleware/rate_limiter.go:127-149`）；auth 路由已显式 `RateLimitFailClose`（`routes/auth.go:35-97`），但管理面使用的 `PanelRateLimiter` 固定 fail-open（`middleware/panel_rate_limit.go:34,90-95`）。
- **现状澄清**：网关 API 入口**不使用** `middleware.RateLimiter`（`routes/gateway.go` 只有 API Key 认证、模型白名单、body limit 等），网关侧限流由 API Key 用量/并发控制承担；因此本方案不新增“网关 IP 限流”。
- **方案**：
  1. 为管理面 `PanelRateLimiter` 增加 `fail_closed_on_redis_error` 开关（默认 false），Redis 错误时按配置返回 503 而非放行。
  2. 审计其余 `Limit/LimitWithOptions` 调用点（`routes/payment.go:74` 公开订单校验等），按入口性质显式选择 `FailureMode`；公开支付查询保持 fail-open（注释已说明原因）。
  3. `clientIPForRateLimit` 已复用 `GetSecurityClientIP`（`rate_limiter.go:113-121`），补充 trusted proxies 启动校验：若 `trusted_proxies` 为空且检测到反代头，打印显式警告。
- **涉及**：`middleware/panel_rate_limit.go`、`server/routes/payment.go`、`config`。
- **测试**：Redis 故障注入下管理面/公开支付路由行为符合配置。

### GW-07 计费同步回退失败后无持久化重试
- **根因**：队列满时同步回退（`billing_cache_service.go:383`），但同步回退本身失败只记日志；`UpdateQuotaUsed` 的 `GetByID/Update` 失败也返回 nil（`api_key_service.go:1185-1200`）。
- **方案**：
  1. 新增 `billing_outbox` 表（`id, kind, payload jsonb, attempts, next_retry_at, last_error, created_at`）或复用现有队列持久化能力；同步回退失败时写入 outbox。
  2. 后台 worker 指数退避重试，成功/超过最大次数（死信）打点告警。
  3. **不向已产生上游成本的请求返回错误**（避免客户端重复请求造成二次计费）；只保证最终一致。
  4. `UpdateQuotaUsed` 在原子递增失败时写 outbox，而非静默返回 nil。
- **涉及**：`migrations/`、`service/billing_cache_service.go`、`service/api_key_service.go`、新 worker。
- **测试**：DB 故障注入后恢复，断言用量补齐；幂等（重复回放不重复扣）。

### PAY-01 Webhook 实例选择早于验签
- **根因**：`extractOutTradeNo` 解析未验签 body 选择 provider 实例（`handler/payment_webhook_handler.go:86-116`）；`GetWebhookProviders` 按订单绑定实例（`service/payment_webhook_provider.go:32-66`），多实例时禁止 registry 回退。
- **方案**：
  1. 保持“按 out_trade_no 查订单 → 锁定订单绑定的 provider instance → 用该实例验签”的顺序（现状已如此），并明确：**验签失败不得回退到其他实例**（现状符合）。
  2. 加固：out_trade_no 解析失败或订单不存在时，若该 provider 存在多个启用实例，直接 400（不回退 registry），并记录 `webhook_instance_ambiguous` 审计。
  3. 验签成功后，再次校验 `notification.OrderID` 与查到的订单 `out_trade_no` 完全一致（现 `confirmPayment` 按订单 ID 履约，需确认此处无旁路）。
  4. 增加多实例场景测试：伪造订单号 + 错误签名必须失败，且不泄露“订单是否存在”。
- **涉及**：`handler/payment_webhook_handler.go`、`service/payment_webhook_provider.go`、`service/payment_fulfillment.go`。
- **测试**：多实例 EasyPay 场景矩阵。

### PAY-03 已取消/过期订单可被恢复
- **根因**：`toPaid` 的 WHERE 允许 `cancelled`（无时间限制）与 `expired`（5 分钟宽限）转 `paid`（`payment_fulfillment.go:150-187`）。
- **方案**：
  1. `cancelled` 视为终态：仅允许 `pending → paid`；取消订单收到支付成功回调时，写 `PAYMENT_AFTER_CANCEL` 审计并进入**人工退款/对账队列**，不自动履约。
  2. `expired` 的 5 分钟宽限保留（支付渠道延迟属常见），但宽限期外同样进入对账队列；宽限期内履约需额外记录 `ORDER_RECOVERED`（现有）。
  3. 取消流程先 `checkPaid` 再取消的竞态：取消前用条件更新 `WHERE status='pending'`，失败（已 paid）则不取消。
- **涉及**：`service/payment_fulfillment.go`、`service/payment_order_lifecycle.go`。
- **测试**：取消后回调不履约；过期 5 分钟内/外行为分别断言。

### PAY-07 公告内容无清洗
- **根因**：`AnnouncementService.Create/Update` 仅 trim（`announcement_service.go:71-210`）；前端当前无公告渲染组件（仅 i18n key），API 可能被第三方客户端消费。
- **方案**：
  1. 后端在 Create/Update 时做白名单清洗：采用保守的 Markdown 子集（粗体/斜体/链接/列表/代码）或纯文本；若必须富文本，使用 `bluemonday`/白名单 HTML sanitizer（Go 侧）。
  2. 存储清洗后的内容，原始内容如需保留放入独立字段（`content_raw`，仅管理员可见）。
  3. 前端未来渲染时必须使用净化后的字段。
  4. 迁移现有数据：后台任务批量清洗并记录差异。
- **涉及**：新增依赖（`github.com/microcosm-cc/bluemonday`）、`service/announcement_service.go`、`migrations/`（可选字段）。
- **测试**：`<script>`/`onerror`/`javascript:` 链接被剥离；正常 Markdown 保留。

### ADMIN-02 最后管理员保护 TOCTOU
- **根因**：`ensureNotLastAdmin` 先 `ListWithFilters` 计数，再单独更新（`service/admin_user.go:162-178`）。
- **方案**：
  1. 在 repository 新增原子方法 `DemoteAdminIfNotLast(ctx, userID int64) error`：
     ```sql
     UPDATE users SET role='user'
     WHERE id=$1 AND role='admin'
       AND (SELECT count(*) FROM users WHERE role='admin' AND deleted_at IS NULL) > 1;
     ```
     受影响行数为 0 时返回“last admin”错误。
  2. service 调用该原子方法，删除先读后写。
  3. 同时把“不能自降级”校验下沉到 service（见 ADMIN-12）。
- **涉及**：`repository/user_repo.go`（或 service 接口新增）、`service/admin_user.go`。
- **测试**：并发两个管理员互相降级，断言至少保留 1 个 admin。

### ADMIN-03 DeleteAccount 非原子
- **根因**：`DeleteAccount` 逐个删影子账号再删母账号，无事务（`service/admin_account.go:1312-1327`）。
- **方案**：
  1. 在 repository 新增 `DeleteAccountCascade(ctx, id int64) error`，内部使用 `dbent.TxFromContext`/`withTx`：
     - `DELETE FROM accounts WHERE parent_account_id=$1`
     - `DELETE FROM accounts WHERE id=$1`
     - 相关关联清理（若 FK 未级联）在同一事务。
  2. service 只调用一个方法；`runtimeBlocker` 内存清理放在事务提交成功后执行（不可回滚，明确为 best-effort）。
- **涉及**：`repository/account_repo.go`、`service/admin_account.go`。
- **测试**：删除中途注入失败，断言无残留影子账号。

### ADMIN-04 ClearAccountError 多写无事务
- **根因**：5 个 repository 方法顺序调用（`admin_account.go:1338-1355`）。
- **方案**：
  1. repository 新增 `ClearAccountErrorState(ctx, id int64) error`，单事务内完成 `clear error/rate_limit/antigravity_quota_scopes/model_rate_limits/temp_unschedulable`。
  2. `runtimeBlocker.ClearAccountSchedulingBlock` 在事务提交后调用；失败仅告警（内存态会在下次调度自然刷新）。
- **涉及**：`repository/account_repo.go`、`service/admin_account.go`。
- **测试**：部分失败回滚；成功后所有标志清零。

### ADMIN-05 BatchDeleteProxies TOCTOU
- **根因**：`CountAccountsByProxyID` 检查后 `Delete`（`admin_proxy.go:172-205`）。
- **方案**：
  1. repository 新增 `DeleteProxyIfUnused(ctx, id int64) (bool, error)`：
     ```sql
     DELETE FROM proxies p
     WHERE p.id=$1 AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.proxy_id=$1);
     ```
     返回受影响行数，0 表示使用中（与并发分配原子互斥）。
  2. `BatchDeleteProxies` 改用该方法，保留逐条 skipped 语义与错误信息。
- **涉及**：`repository/proxy_repo.go`、`service/admin_proxy.go`。
- **测试**：并发“分配账号到代理”与“删除代理”，断言不会删除使用中的代理。

### FE-02 Token 拼进 iframe URL query
- **根因**：`buildEmbeddedUrl` 将 `authStore.token` 写入 `token` query（`utils/embedded-url.ts:29-30`）。
- **方案**（分阶段，第三方嵌入页无法用 cookie）：
  1. **短期**：改为一次性短时 token（后端新增 `POST /api/v1/auth/embed-token`，TTL 60s、单次使用、仅限该用户），并设置 `Referrer-Policy: no-referrer`（meta/响应头）；iframe 仍通过 query 传递一次性 token，泄露窗口从小时级降到秒级。
  2. **中期**：改用 `postMessage` 握手：父页面在 iframe `load` 后发送 token（`targetOrigin` 精确匹配），嵌入页回传 ready；旧 query 方式保留一个版本作为回退。
  3. 在嵌入 URL 中移除 `src_url`（当前会泄漏父页面完整 URL，可能含其他参数）。
- **涉及**：`frontend/src/utils/embedded-url.ts`、`views/user/CustomPageView.vue`、后端新增 embed-token 接口、嵌入页协议文档。
- **测试**：URL 中不再出现长期 access token；一次性 token 二次使用失败。

### FE-05 Token 存 localStorage
- **根因**：`stores/auth.ts` 持久化 access/refresh token（`:49-57,214-226`）。
- **方案**（分阶段，涉及后端）：
  1. **短期**：access token 仅内存保存（刷新页面重新静默刷新），refresh token 迁移到 `HttpOnly; Secure; SameSite=Lax` cookie（后端 set/clear）；这需要后端 refresh/logout 接口支持 cookie 模式。
  2. 前端 `authStore` 去掉 refresh token 的 localStorage 读写，保留 `user` 与过期时间等非敏感字段。
  3. 兼容期：后端同时接受 body 与 cookie 中的 refresh token，灰度后再移除 body 模式。
  4. 评估 WebSocket/iframe 场景（WS 升级无法带 cookie 跨域时仍用短时 token）。
- **涉及**：`stores/auth.ts`、`api/client.ts`、后端 auth handler（refresh/logout cookie 支持）、CORS/CSRF 配置。
- **测试**：XSS 模拟脚本无法读到 token；刷新页面会话保持；登出清 cookie。

### FE-07 风控守卫 fail-open
- **根因**：`fetchPublicSettings` 失败仅 `console.warn`，随后因 `publicSettingsLoaded` 为 false 跳过拦截（`router/index.ts:613-628`）。
- **方案**：
  1. 失败时重试一次（短退避）；仍失败则使用 `cachedPublicSettings`（本地缓存/`sessionStorage`）。
  2. 无缓存且页面 `meta.requiresRiskControl` 为真时 **fail-closed**：跳转到错误/重试页，而不是放行。
  3. 非风控页面不受影响（避免全站不可用）。
- **涉及**：`router/index.ts`、`stores/app.ts`（设置缓存）。
- **测试**：mock fetch 失败，断言风控页被阻断、普通页可访问。

### FE-08 SVG 上传未净化即 emit
- **根因**：`ImageUpload.vue` SVG 模式 `reader.readAsText` 后直接 `emit('update:modelValue', text.trim())`（`:140-146`），`sanitizeSvg` 只用于本组件预览。
- **方案**：
  1. emit 前调用 `sanitizeSvg(text)`（`utils/sanitize.ts` 已存在）；若净化失败/结果为空则报错并拒绝。
  2. 保留原始文件内容仅用于可选的服务端扫描（当前无此需求，可不做）。
  3. 补 Vitest：含 `<script>`、`onload`、外部 `xlink:href` 的 SVG 被净化。
- **涉及**：`components/common/ImageUpload.vue`。
- **兼容性**：会移除部分高级 SVG 特性（filter/use 已由 `svgFilters: true` 保留），需在 UI 提示“SVG 会进行安全净化”。

---

## P2（Low/Info 与需决策项）

### AUTH-01 登录重复签发 Access Token（Info）
- `AuthHandler.Login` 中 `_ = token`（`auth_handler.go:252-257`）。方案：`AuthService.Login` 改为只校验返回 `*User`，由 handler 统一 `respondWithTokenPair`；OAuth 调用方（`LoginOrRegisterOAuth`）单独保留签发路径。纯清理，无安全影响。

### AUTH-06 BackendModeAuthGuard 子串匹配过宽（Low）
- `backend_mode_guard.go:73` `strings.Contains(path, "/auth/oauth/pending/")` 改为显式路由前缀集合（如 `/api/v1/auth/oauth/pending/{provider}/...` 的实际注册路径），并补测试：未知 pending 路径默认拒绝。

### AUTH-07 注册提交跳过人机验证（Low）
- 在发码阶段已将 captcha 验证结果与邮箱绑定（一次性票据，Redis `captcha_ticket:<email>` TTL 10min）；注册提交校验该票据存在并消费。避免仅依赖发码阶段。

### AUTH-08 密码重置先消费 Token（Low）
- 顺序改为：校验 token 格式/签名 → 查用户 → 校验用户状态 → 原子消费 token（`DELETE ... WHERE token_hash=$1 RETURNING ...`）。若用户不存在，直接返回统一“token 无效”，不消费。

### AUTH-09 isRequestHTTPS 信任转发头（Low）
- `auth_linuxdo_oauth.go:1012-1018` 改为复用 `ippkg.GetSecurityClientIP` 同一套可信代理判断；或至少在 `isRequestHTTPS` 中检查 `c.Request.RemoteAddr` 是否属于 `server.trusted_proxies` 后才读取 `X-Forwarded-Proto`。

### AUTH-10 上下文缺失签发无绑定 Token（Low）
- 保留内部调用（OAuth/测试）可用，但新增 `GenerateTokenWithBindingRequired`；HTTP 入口（登录/刷新/2FA）必须使用该函数，缺失绑定上下文时返回错误。避免全局强制导致内部任务失败。

### AUTH-11 管理员 API Key 固定映射首个管理员（Info）
- 最小改动：在审计日志 `AuthMethod=admin_api_key` 时记录 key 指纹（`sha256(key)[:8]`）与名称；完整多主体映射需要表结构，暂不做。

### GW-04/COMP-01 Ambiguous 死代码与失败测试（Low，需产品决策）
- **现状**：`resolveCompositeModelOwnership` 不再设置 `Ambiguous`（`gateway_service.go:1499-1545`），resolver 的 `ownership.Ambiguous` 分支（`composite_route_resolver.go:168-176`）不可达；跨族仍由 `CompositePlatformsShareGatewayFamily` fail-closed。
- **方案（二选一，需确认）**：
  - A（推荐，保持二开语义）：删除 `Ambiguous` 字段与 resolver 死分支，更新两个失败测试为“同族合并、跨族拒绝”的新语义（`TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership` 的 reason 断言同步更新）。
  - B（恢复上游语义）：任何多平台都标记 `Ambiguous`，但会破坏 `CHANGELOG-SO.md` 2026-10-16 的多平台统一调度，需同步回退调度/计费/WS 多处逻辑，成本高。
- **注意**：不要只“设置 Ambiguous=true”而不改下游，否则同族合并功能失效。

### GW-09 inflight 引用计数竞态（PLAUSIBLE）
- 窗口：`Acquire` 读到 `refs>0` 后、`Release` 将 `refs.Store(0)` 之间。
- 方案：增加 `released atomic.Bool`；`Acquire` 在 CAS 前后都检查 `released`，`Release` 先 `released.Store(true)` 再置 0；或把 refs 改为带状态位的 64 位 CAS（`released` 最高位）。
- 验证：`go test -race` + 并发 `Acquire/HandlerDone/Release` 压测。

### GW-10 流中断重复计费（Info）
- 已有 `usage_billing_dedup(request_id, api_key_id, request_fingerprint)`（`migrations/071`）。方案：确认 `RequestFingerprint` 的构造包含客户端 request id/本地 request id（`gateway_usage_billing.go`），并为“流中断后客户端重试”补集成测试；`ErrUsageBillingRequestConflict` 需明确告警而非静默忽略。

### GW-11 WS 白名单（Info）
- 现状已逐帧校验（`openai_gateway_handler.go:2576-2585,3068-3078`）。方案：补一条“WS 路由清单与中间件跳过条件一致性”的测试，防止未来新增 WS 路由遗漏。

### GW-08 并发/等待计数 Redis 不可用 fail-open（Low）
- **根因**：`IncrementWaitCount`（`concurrency_service.go:495-508`）在 `cache == nil` 或 Redis 错误时返回 `true` 放行；`TrackAPIKeySlot`（`:417-455`）失败返回 no-op release，不记录槽位。
- **方案**：
  1. 增加 `concurrency.fail_closed_on_cache_error`（默认 false）。开启后 `IncrementWaitCount` 在 Redis 错误时返回 `false`（按队列已满处理），请求走 429/排队失败路径。
  2. `TrackAPIKeySlot` 是统计用途，保持 best-effort，但增加 `api_key_slot_track_failed_total` 指标与告警阈值。
  3. 明确多实例下本地兜底不能替代全局并发上限（文档说明），避免误以为降级后仍全局生效。
- **涉及**：`service/concurrency_service.go`、`config/config.go`。
- **测试**：Redis 故障注入，断言按开关拒绝/放行，并产生指标。

### ADMIN-10 计划任务 handler 直接序列化 service 结构（Low）
- **根因**：`scheduled_test_handler.go:52,82,126,159` 直接 `c.JSON(..., plans/results)`，`ScheduledTestResult.ResponseText` 可能含上游返回内容与内部字段。
- **方案**：
  1. 新增 DTO（`ScheduledTestPlanDTO`、`ScheduledTestResultDTO`），显式列出字段；`ResponseText` 截断（如 4KB）并可选掩码。
  2. handler 统一经 DTO 返回，避免 schema 漂移与意外字段暴露。
- **涉及**：`handler/admin/scheduled_test_handler.go`、`handler/dto/`。
- **测试**：响应字段白名单断言。

### GW-12 空映射回落默认模型列表（Low）
- `gateway_handler.go:1194-1218` 与 `gateway_service.go:1455-1478`：OpenAI 分支用 `availableModels != nil` 区分“空但有权威目录”与“nil 用默认”。方案：明确各平台空映射语义（建议：有账号但无映射时仍返回默认目录，无账号时返回空），补测试覆盖 retired 模型过滤与空目录。

### PLAT-01 Antigravity thinking 变体退化（Low，需决策）
- 根因：二开删除运行时注入 `ensureAntigravityDefaultPassthroughs`（`account.go:680-688`），测试仍期望注入行为。
- 方案（二选一）：
  - A（推荐）：更新测试为“显式映射即完整口径”；在 `resolveGeminiThinkingVariantForLevel` 中，对“裸名自映射 + 存在部分变体”的情况，仅按已配置变体降级（现状已如此），文档说明 low 请求可能退化 high。
  - B：恢复注入，但会让已删除模型重新出现在 `/v1/models`，需产品确认。
- 不建议只改变体解析逻辑（无法命中根因）。

### PLAT-03 跨族 reason 文案（Low）
- `composite_route_resolver.go:14-17,162-167`：跨族 reason 比测试期望更详细。保留详细文案（对客户端更有指导性），更新 `composite_route_resolver_test.go:240` 的期望字符串；`CompositeRouteReasonAmbiguous` 与 `...OAuthAmbiguous` 的语义保持不变。

### PLAT-04 SSE usage 跳过零值（Low）
- `antigravity_gateway_upstream.go:329-349`：改为“字段存在才赋值”（`if v, ok := ...; ok { usage.X = int(v) }`），不要笼统保留旧值；注意 `message_delta` 只带 `output_tokens` 时不能把输入侧清零。

### PLAT-05 Claude SSE eventType 残留（Low）
- `antigravity_gateway_compat_stream.go:196-207`：按 SSE 规范在空行重置 `eventType`；`data:` 行处理完后如无 `event:` 行，`consumeClaudeData` 应回退到 payload 内 `type`（现有回退逻辑保留）。

### PLAT-06 `data:` 无空格变体（Low）
- `antigravity_gateway_upstream.go:309-313`：复用已有 `extractAnthropicSSEDataLine`（`gateway_anthropic_passthrough.go:611-623`），替换 `strings.HasPrefix(line, "data: ")`。

### PLAT-08 Qoder 候选主机解析失败（Info）
- **现状澄清**：`sendCCUpstreamRequest` 的 qoder 分支已统一调用 `sendQoderChat`（`openai_gateway_cc_pipeline.go:349-354`），账号测试（`openai_gateway_qoder.go:825`）与 CC pipeline 的发送均覆盖换主机重试。原先“部分调用点未走 sendQoderChat”的判断不成立。
- **剩余缺口**：`qoderChatURL`（`openai_gateway_qoder.go:63-68`）只返回首个候选，解析/构造阶段失败时不会尝试其他候选主机（发送阶段的传输失败已有重试）。
- **方案**（可选，非必须）：让 `qoderChatURL` 返回候选列表，或在解析失败时用 `qoderNextGatewayRequest` 的候选序重试构造；若当前无实际失败案例，可仅补日志与指标，不做代码改动。

### PLAT-09 Gemini 函数调用 ID（Low）
- 请求侧剥离 id 是协议要求（`gemini_messages_compat_service.go:3494` 注释）；响应侧应基于 `toolUseIDToName` 的 name 映射生成稳定 id（而非随机 `toolu_`），并保持请求/响应映射生命周期一致。

### PLAT-11 复合所有权缓存（PLAUSIBLE）
- 默认 TTL 15s/上限 30s，`InvalidateAvailableModelsCache` 无生产调用方（仅测试）。方案：在账号/分组/模型映射变更的 admin 服务中调用 `InvalidateAvailableModelsCache`（账号增删改、分组模型配置、模型映射批量导入）；TTL 保持。

### PAY-05 兑换锁/失败计数 Redis 降级（Low）
- **根因**：`checkRedeemRateLimit` 在 cache 错误时返回 nil；`acquireRedeemLock` 在 cache 为 nil 或 Acquire 出错时返回 true（`redeem_service.go:334-384`）。
- **现状澄清**：并发兑换本身已由数据库乐观锁兜底——`redeem_code_repo.go:324-340` 的 `WHERE status='unused'` 原子标记 + 事务（`redeem_service.go:470-495`），因此不会重复兑换。真正缺口是 Redis 故障时**失败尝试次数不限流**，可被暴力尝试。
- **方案**：
  1. 保持 DB 乐观锁为唯一正确性保障（不改为 fail-closed，避免 Redis 故障导致兑换不可用）。
  2. 失败计数增加数据库兜底：`redeem_attempts(user_id, window_start, count)` 表或复用现有失败审计表，Redis 不可用时按 DB 计数限流。
  3. 或新增 `redeem.fail_closed_on_cache_error`（默认 false），供高安全部署选择。
- **涉及**：`service/redeem_service.go`、`repository/redeem_code_repo.go`、`migrations/`（若新增计数表）。
- **测试**：Redis 故障下并发兑换同一码只有 1 次成功；连续失败达到阈值后按配置拒绝。

### PAY-02 legacy `sub2_<id>` 回退（Low）
- 保留兼容但加约束：仅当 `payment.legacy_order_id_fallback_enabled=true`（默认 false，迁移期开启）且订单创建时间早于迁移截止时间时生效；否则返回 `ErrOrderNotFound`。补审计日志。

### PAY-04 金额容差（Low）
- **根因**：`payment_fulfillment.go:119-125` 对 minor unit ≤2 的货币统一使用 0.01 容差。
- **现状**：`ent/schema/payment_order.go` 金额为 `field.Float("amount"/"pay_amount")`（decimal(20,2)），**不存在 `amount_minor` 字段**，不能直接做整数列比较。
- **方案**：在 `confirmPayment` 中把双方金额字符串化后经 `payment.AmountToMinorUnit(fmt.Sprintf("%.2f", paid), currency)` 与 `payment.AmountToMinorUnit(fmt.Sprintf("%.2f", order.Amount), currency)` 现场换算为 minor unit 比较，差值为 0（或按币种最小单位允许 ≤1）才通过；避免浮点累计误差。若后续需要审计精度，再考虑新增 `amount_minor BIGINT` 列（需 migration 与回填，成本较高，非本项必需）。
- **涉及**：`service/payment_fulfillment.go`、`internal/payment/currency.go`。
- **测试**：各币种金额在 0.001 级差异下的通过/拒绝矩阵。

### PAY-06 兑换码负值（Low，需权限约束）
- 负值是退款功能，不能全局拒绝。方案：`GenerateRedeemCodesRequest`/`CreateAndRedeemCodeRequest` 对 `value < 0` 增加：
  1. 仅允许 `type=balance/subscription`（退款语义明确）；
  2. 要求管理员具备高权限（如 `admin` 且通过 step-up 2FA）；
  3. 强制填写 `notes` 并写审计。
- 公开兑换接口本身不创建码，无需改。

### PAY-10 批量图像状态机（Low）
- `batch_image.go:371-416`：区分调用来源——系统 worker 允许任意非终态 → `failed`；用户/管理接口仅允许 `pending/processing → cancelled/failed`；`output_deleted` 仅允许从终态（completed/failed/cancelled）进入，并记录操作者。

### PAY-11 配额 30 天窗口（Info，需产品确认）
- 计费缓存、repository、配额视图三处一致使用 30 天滚动窗口，属既定语义。若产品要求自然月，需三处联动 + 迁移窗口起点；否则保持现状并文档化。

### ADMIN-01 Ent password_hash JSON tag（Low）
- 生成代码不可手改。方案：不改 `ent/user.go`；增加静态检查/测试，禁止 handler 直接 `c.JSON` 序列化 `dbent.User`（现有 DTO 映射已规避）；如确需改 tag，通过 ent 模板或 `entc` 注解统一处理。

### ADMIN-06 API Key 明文列表（Low）
- 保持“本人可见完整 key”（前端复制功能依赖），但：
  1. 管理员跨用户列表（`admin/user_handler.go:440`、`admin/group_handler.go:984`）改为掩码（`sk-...abcd`）；
  2. 需要完整值时使用独立的、写审计的“查看 key”接口（仅 owner）；
  3. 列表响应缓存/日志不得记录 key。

### ADMIN-08 审计日志清空（Low）
- **现状澄清**：已有现场 TOTP、禁止 admin API key、清空后同步留痕（`audit_log_handler.go:104-168`、`audit_log_service.go:104-129`）；系统也已有 `audit_log_retention_days`（默认 180 天）与每日清理循环（`setting_features.go:262-280`、`audit_log_service.go:187-235`）。因此**不需要新增保留期能力**。
- **真正缺口**：`ClearAll`/`TruncateAll` 一键清空不受保留期约束，可一次抹掉近期证据；且无双人审批/归档。
- **方案**：
  1. `TruncateAll` 改为按时间分批删除，仅删除超过 `audit_log_retention_days` 的部分（与自动清理同边界）；近 N 天日志禁止通过该接口删除。
  2. 清空前强制导出（返回导出下载链接/一次性 token）或要求第二管理员审批（dual control）。
  3. 归档表（可选）：删除前将行写入 `audit_logs_archive`。
- **涉及**：`service/audit_log_service.go`、`handler/admin/audit_log_handler.go`、`repository/audit_log_repo.go`。
- **测试**：近 N 天记录无法被清空；超期记录可清理且留痕。

### ADMIN-09 迁移 checksum 例外（Low）
- `migrations_runner.go:74-102`：为兼容规则增加“到期版本/日期”，到期后自动失效并启动告警；提供一次性对齐脚本；新迁移禁止加入白名单。

### ADMIN-11 Promo MaxUses=0（Low）
- 保留 0=无限，但增加互斥字段 `unlimited bool`：创建时若 `max_uses=0` 必须显式 `unlimited=true`，否则校验失败；前端表单同步。

### ADMIN-12 自降级保护仅在 handler（Low）
- 新增 `UpdateUserAs(ctx, actorID, targetID, input)` 或在 service 方法签名中加入 `actorUserID`；把“不能自降级”与“最后管理员”校验移入 service，handler 只传 actor。

### FE-03 buildEmbeddedUrl 原始 URL 回退（Low）
- `utils/embedded-url.ts:41-43`：catch 分支返回 `''` 而非原 URL；新增 `isHttpUrl()`（`new URL` + scheme 白名单）在 builder 内校验，非 http/https 直接返回空。模板已用 `isValidUrl` 过滤，此为纵深防御。

### FE-04 KeyUsageView RAF（Low）
- `nextTick` 回调内检查 `isUnmounted` 标志；`cancelRingAnimation` 递归取消所有已登记 handle（`Set<number>`）；`onUnmounted` 先置标志再取消。

### FE-06 路由白名单前缀（Low）
- `router/index.ts:510-513` 改为 `path === allowed || path.startsWith(allowed + '/')`；`/legal` 等含子路由的前缀保留段边界。

### FE-09 any 滥用（Info）
- 增量治理：先处理 API 响应类型（`src/api/*`）与 store；ESLint 开 `@typescript-eslint/no-explicit-any` 为 warn，新增代码禁止。

### FE-10 复制按钮监听器（Info）
- 改为容器级事件委托（`click` 判断 `.copy-btn`），或在重建 DOM 前移除。

### FE-11 首页 iframe URL（Low）
- `HomeView.vue:496-499` 用 `new URL()` 解析并限制 `http/https`；与 FE-01 的 `home_content` 判断统一。

### FE-12 window/document 直接访问（Info）
- SPA + jsdom 环境下风险低；仅在模块顶层访问处加 `typeof window !== 'undefined'` 守卫。

---

## 实施顺序建议
1. **第一批（低风险、无产品决策）**：GW-01（service 层开关）、GW-02（复用既有配置 + 新开关）、ADMIN-07、FE-01（Sanitize + 开关）、FE-03/FE-06/FE-11、PLAT-03/04/05/06、AUTH-01/08。
2. **第二批（需要事务/迁移）**：ADMIN-02~05（repository 原子方法）、AUTH-04（token_version 列）、GW-07（outbox）、AUTH-03（binding version）。
3. **第三批（需要产品/协议决策）**：GW-04/COMP-01、PLAT-01、PAY-06、PAY-11、FE-02/FE-05（嵌入/令牌协议）。
4. **第四批（并发验证）**：GW-09、GW-10、PLAT-11、GW-03（Lua 原子限流）。

## 验证门禁
- 后端：`go test -tags=unit ./...`、`go test -race ./internal/service/...`（GW-09）、`go vet`。
- 前端：`pnpm typecheck`、`pnpm test:run`、`pnpm lint:check`。
- 安全回归：XSS payload（FE-01/FE-08/PAY-07）、权限矩阵（PAY-01/ADMIN-02/ADMIN-12）、并发压测（GW-02/GW-03/GW-09/ADMIN-02/ADMIN-05）。
