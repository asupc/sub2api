# ADVERSARIAL-FINDINGS / ADVERSARIAL-SOLUTIONS 终版交叉验证报告

- 验证日期：2026-10-17
- 验证对象：
  - `ADVERSARIAL-FINDINGS.md`（终版问题清单，doc 内自称 64 条有效）
  - `ADVERSARIAL-SOLUTIONS.md`（终版解决方案，doc 内自称对应 61 条有效）
- 方法：逐条 Read/Grep 定位 + 关键路径 Docker `golang:1.27.2` 运行时复核（IPv4-mapped 归一化）+ 文档 ID 集合机器比对；未修改任何现有文档或业务代码。
- 说明：本报告只验证"修订后的终版是否真实、可靠、自洽"，不重新设计修复方案。

## 结论

**可以判定为"基本真实可靠"，但不能判定为"完全自洽"**——需先修正文档内部的计数/索引矛盾，并按下列问题清单修正 3 处方案事实错误（PAY-04/GW-06/ADMIN-08）与 1 条严重度（PLAT-08）。

- **代码事实核查：通过。** 23 条 High/Medium 与全部 33 条 Low + 8 条 Info 逐条核对，代码行为均与描述一致（Low/Info 抽查要求 15 条，实际全查 41 条）。所有引用行号偏差均在 ±10 行内（最大偏差 8 行：GW-02 代码块起点 249 vs 清单 257；其余 0-4 行），无需按行号偏移扣分。
- **方案引用事实核查：通过。** 任务列出的 10 项具体事实全部核对为真（`setup.go:445` 打印密码、`sanitizeSvg` + `dompurify`、`extractAnthropicSSEDataLine` 兼容无空格、`Use` 的 `WHERE status='unused'` 乐观锁、`ent/schema/user.go` 无 token_version、`usage_billing_dedup` migration、`fail_closed_on_unpriced` 配置、`scheduled_test_handler.go` 直接 `c.JSON` service 结构、`concurrency_service.go` fail-open 位置、`validateJWTForAdmin` 的 `IsAdmin` 校验）。
- **4 条 REJECTED 判断复核：全部仍然成立**（PAY-08 已用 Docker 运行时再次验证；PAY-12 路由+中间件证据充分；PLAT-07 / PLAT-10 代码与测试均支持原判断）。
- **主要问题（均为文档层面，非代码判断错误）：**
  1. **统计口径矛盾（高）**：FINDINGS 第 21 行"唯一问题条目：64"按表格行计等于 4+19+33+8=64（`GW-04/COMP-01` 为一行），但按唯一 ID 计是 **65**；SOLUTIONS 第 3 行"61 条有效问题"无论按行还是按 ID 都不成立（实际 64 章节 / 65 有效 ID）。
  2. **方案 P2 优先级列表未点名 PAY-05（中）**：PAY-05 有独立方案段落（SOLUTIONS 第 353-361 行），但第 10 行 P2 清单只列了 GW-04/COMP-01、PLAT-01、PAY-06、PAY-11，实施时易漏。
  3. **PLAT-02 / PAY-09 的"已合并"去向未在终版标注（中）**：两条为 GW-04/COMP-01 的 DUPLICATE，终版未再提及（处理正确），但只能从旧 VERIFY 报告推断，建议在 FINDINGS"关键修正"表注明。
  4. **方案可执行性 3 处 + 严重度 1 处**：PAY-04 引用不存在的 `amount_minor` 字段；GW-06 把管理面 `PanelRateLimiter` 误当作 `RateLimiter` 的落点；ADMIN-08 重复建议已存在的保留期；PLAT-08 的"部分调用点未走 sendQoderChat"已不成立，应降级为 Info/REJECTED。详见后文问题清单第 2-5 条。GW-06 原被归为低，但其"网关入口无落点"属事实错误，已提升为中。
  5. **低优先表述偏差**：GW-04/COMP-01"死代码"应注明"仅测试可达"；FE-03/FE-09/AUTH-01/PLAT-05 的行号或计数有 ±2 至 ±3 的轻微偏差（不影响结论）。

## 逐条核对表

说明：
- `PASS` = 描述、位置、行为一致；
- `PASS-ADJUST` = 行为成立但行号/表述需微调（已在"问题"列给出应改成什么）；
- 行号偏差 >±10 行的单独标注。

### High（4 条，全部 PASS）

| ID | 结论 | 代码事实核对 |
|---|---|---|
| GW-01 | PASS | `content_moderation_helper.go:61-67` 的 `err != nil` 分支 return nil；`content_moderation.go:826-844`（不可用/加载失败）、`:1072` 转 `checkSync`、`:1085-1109`（审计调用失败 return allow）与描述一致。清单写 `:824-845,1075-1115`，实际关键行 824/835-845/1075-1110，偏差在 ±10 内。 |
| GW-02 | PASS | `gateway_inflight_reservation.go:80-85`（未定价且 fail_closed 才拒，`estimate<=0` 直接放行）；`billing_inflight_reservation.go:249-278`（estimate<=0 / 余额读失败 / Reserve 失败均 fail-open）。清单写 `:257-278`，实际 249-278（起点偏差 8 行，PASS）。现有 `fail_closed_on_unpriced` 在 `config.go:945-947` 与 `:2138`。 |
| ADMIN-07 | PASS | `setup/setup.go:445` 精确命中 `fmt.Printf("Generated admin password (one-time): %s\n", ...)`，且仅 `passwordGenerated` 时打印。补充：CLI 向导模式（`--setup`）要求用户手输密码，不走该打印；该行影响的是 Web 安装/`AUTO_SETUP` 生成密码的路径（方案按此理解正确）。 |
| FE-01 | PASS | `HomeView.vue:11-12` 注释"admin-only setting, XSS risk is acceptable" + `v-html="homeContent"`，无净化。 |

### Medium（19 条，全部 PASS）

| ID | 结论 | 代码事实核对 |
|---|---|---|
| AUTH-02 | PASS | `auth_service.go:216-218`（Register）、`:323-325`（SendVerifyCode）、`:363-366`（SendVerifyCodeAsync）三处 `ErrEmailExists`；`:357` 注释"避免批量脚本消耗发信配额"存在。 |
| AUTH-03 | PASS | `session_binding.go:68-73`：功能关闭或 `claims.BindingHash == ""` 直接放行。 |
| AUTH-04 | PASS | `auth_service.go:1895-1911` 注释明确"users 表没有 token_version 列，resolvedTokenVersion 由 email+password_hash 指纹推导"，仅清 refresh sessions；`user_service.go:1036-1045` 改密只写 password_hash。 |
| AUTH-05 | PASS | `auth_handler.go:301-413`：TOTP 通过（331）后，用户查询/backend mode/pending OAuth 绑定任一失败均直接 return，`DeleteLoginSession` 只在 412-413 全部成功后执行，失败路径可重放。 |
| GW-03 | PASS | `billing_cache_service.go:575-633`：cache 为 nil 且 loader 为 nil → nil；DB 读失败 → nil（:583、:597）；`api_key_service.go:1158-1203` 异步 `QueueUpdateAPIKeyRateLimitUsage`（:691-700）与预检非原子。 |
| GW-05 | PASS | `failover_loop.go:79-105`：`SameAccountRetryMax>0` 时有上限；`SameAccountRetryDeadline` 非零时 `return true`（:99-101），OAuth 429 由 `openai_gateway_upstream_errors.go:396-399` 注入 deadline，确实不受 retryLimit 约束。 |
| GW-06 | PASS-ADJUST | `middleware/rate_limiter.go:127-149` 默认 fail-open 与 auth 路由显式 fail-close（`routes/auth.go:35-97`）均属实。**但"网关入口"当前不使用该 RateLimiter**（网关仅用 API Key 用量/并发限流；管理面用 `PanelRateLimiter`，Redis 异常固定 fail-open，`panel_rate_limit.go:34,90-95`）。清单位置只列 `routes/auth.go`，没有夸大到网关，可保留；方案描述需按问题清单第 3 条修正。 |
| GW-07 | PASS | `billing_cache_service.go:192-214`（队列入队失败返回 false）、`:378-397`（同步回退失败仅 log）、`:690-699`（限流用量异步无回退）；`api_key_service.go:1185-1200`（GetByID/Update 失败 return nil）。 |
| PAY-01 | PASS | `payment_webhook_handler.go:86-116`：先 `extractOutTradeNo`（:88）→ `GetWebhookProviders`（:90）→ 验签（:106）；`payment_webhook_provider.go:32-83` 按订单绑定实例，`webhookRegistryFallbackAllowed` 仅 count<=1（:112）。 |
| PAY-03 | PASS | `payment_fulfillment.go:150-187`：`toPaid` 的 WHERE 允许 `cancelled`（无时间限制）与 `expired`（`UpdatedAtGTE(grace)`，grace=5min，`payment_service.go:42`）；恢复后写 `ORDER_RECOVERED` 并履约（:171-186）。 |
| PAY-07 | PASS | `announcement_service.go:71-210`：Create/Update 仅 `strings.TrimSpace`，无任何 HTML/Markdown 清洗；前端确认无公告渲染组件（仅 i18n key）。 |
| ADMIN-02 | PASS | `admin_user.go:162-178`：`ensureNotLastAdmin` 先 `ListWithFilters` 计数（:165-176），`UpdateUser` 在 :260-263 再单独更新，读-写非原子（文件注释自认竞态）。 |
| ADMIN-03 | PASS | `admin_account.go:1312-1327`：先 `ListShadowsByParent`，逐个 `Delete`，再删母账号，无事务。 |
| ADMIN-04 | PASS | `admin_account.go:1338-1355`：5 个 repo 调用顺序执行（ClearError/ClearRateLimit/ClearAntigravityQuotaScopes/ClearModelRateLimits/ClearTempUnschedulable），无事务。 |
| ADMIN-05 | PASS | `admin_proxy.go:172-205`：`CountAccountsByProxyID` 与 `Delete` 之间无原子保护；单条删除路径（约 160-170）同样先计数。 |
| FE-02 | PASS | `embedded-url.ts:29-30` 将 `authToken` 写入 query（常量 `EMBEDDED_AUTH_TOKEN_QUERY_KEY`）；`CustomPageView.vue:245-254` 传 `authStore.token`（:250）。附带风险 `src_url=window.location.href`（:40）仍在。 |
| FE-05 | PASS | `stores/auth.ts:54-60`（启动读 localStorage）、`:214`（refresh 写 localStorage）、`:225`（access 写 localStorage）；`setAuthFromResponse` 存两 token。 |
| FE-07 | PASS | `router/index.ts:613-628`：`fetchPublicSettings` 失败仅 `console.warn`，随后 `publicSettingsLoaded` 为 false 时两个拦截条件均不成立 → 放行；`requiresRiskControl` 页面可直达。 |
| FE-08 | PASS | `ImageUpload.vue:140-146`：SVG 模式 `readAsText` 后直接 `emit('update:modelValue', text.trim())`；`sanitizeSvg` 仅用于 `sanitizedValue`（:115-117）预览。 |

### Low（33 条，全部核对，PASS / PASS-ADJUST）

| ID | 结论 | 代码事实核对 |
|---|---|---|
| AUTH-06 | PASS | `backend_mode_guard.go:73` `strings.Contains(path, "/auth/oauth/pending/")` 精确命中。 |
| AUTH-07 | PASS | `auth_service.go:396-405`：邮箱验证开启且 verifyCode 非空时跳过 captcha 复查。 |
| AUTH-08 | PASS | `auth_service.go:1615-1638`：先 `ConsumePasswordResetToken`（:1626）再 `GetByEmail`（:1630）。 |
| AUTH-09 | PASS | `auth_linuxdo_oauth.go:1012-1018`（实测函数在 1012-1017）：直接读 `X-Forwarded-Proto`，TLS 非空则 true。 |
| AUTH-10 | PASS | `auth_service.go:1408-1417`：`GenerateToken` 注释与实现均允许"上下文缺失生成不带绑定的 token"（`sessionBindingHashFromContext` 返回空）。 |
| GW-08 | PASS | `concurrency_service.go:417-449`（TrackAPIKeySlot 失败返回 no-op release）、`:495-508`（IncrementWaitCount：cache nil 或 Redis 错误均 `return true`）。 |
| ADMIN-10 | PASS | `scheduled_test_handler.go:40-55,144-162` 直接 `c.JSON(http.StatusOK, plans/results)`；`service.ScheduledTestResult.ResponseText` 有 `json:"response_text"`（`scheduled_test_port.go:24-34`），确会原样输出上游响应文本。 |
| GW-04/COMP-01 | PASS-ADJUST | `gateway_service.go:1499-1545` 只写 `TargetPlatforms`/`Matched`，从不写 `Ambiguous`；resolver `:142-176` 的同族合并/跨族拒绝分支存在；测试 `composite_platform_test.go:110-129` 仍期望旧 `Ambiguous:true`（该测试由上游 v0.2.17 引入，`d19265ddb` 已确认是 `UPSTREAM-BASE b0a02a33` 的祖先）。**"死代码"应注明"生产路径不可达、测试路径可达"**（见问题 6）。 |
| GW-09 | PASS（PLAUSIBLE） | `billing_inflight_reservation.go:79-129`：`Acquire` CAS 前读 `refs`，`Release` 直接 `refs.Store(0)`；窗口确实存在于 `cur>0` 与 Store(0) 之间。运行期未复现，PLAUSIBLE 标注正确。 |
| GW-12 | PASS | `gateway_handler.go:1194-1218`（OpenAI 用 `availableModels != nil` 区分，其余平台 `len>0` 才用，否则回落默认列表）；`gateway_service.go:1455-1478`（`!hasAnyMapping` 时 OpenAI 返回默认目录、其余返回 nil）。 |
| PLAT-01 | PASS | `account.go:680-688` 注释"Antigravity 非空映射不再注入默认条目"；`ensureAntigravityDefaultPassthroughs` 在 `:792` 只有定义无调用；`antigravity_gemini_thinking_variant.go:120-184` 的降级顺序确实会在"仅配置 -high"时把 low 请求落到 high。测试期望注入（`antigravity_gemini_thinking_variant_test.go:81,92-97`）与实现冲突，与描述一致。 |
| PLAT-03 | PASS | `composite_route_resolver.go:14-17`（两个 reason 常量）、`:162-167`（跨族详细文案）；测试 `composite_route_resolver_test.go:240` 仍期望简短 `"model is exposed by multiple provider platforms"`。注意位置 `162-167` 是跨族分支，`168-176` 才是 `Ambiguous` 分支，清单引用无歧义。 |
| PLAT-04 | PASS | `antigravity_gateway_upstream.go:329-349`：四个 usage 字段均 `int(v) > 0` 才赋值。 |
| PLAT-05 | PASS | `antigravity_gateway_compat_stream.go:196-207`：`eventType` 为函数局部变量，但 `consumeClaudeEvents` 的输入是 `StreamingProcessor` 输出（每个事件都是 `event:...\ndata:...\n\n`），且 `consumeClaudeData` 在 `event.Type == ""` 时回退 event 行类型——"跨事件残留"实际只在非标准上游/分块拼接时出现，严重度 Low 合理。 |
| PLAT-06 | PASS | `antigravity_gateway_upstream.go:309-313` 精确 `HasPrefix(line, "data: ")`；`gateway_anthropic_passthrough.go:611-623` 的 `extractAnthropicSSEDataLine` 用 `HasPrefix("data:")` + 跳过空格/Tab，兼容无空格，方案复用建议正确。 |
| PLAT-08 | PASS-ADJUST | 两个缺口坐标属实：`openai_gateway_qoder.go:825`（账号测试 `qoderChatURL` 后经 `sendCCUpstreamRequest` 发送）、`openai_gateway_cc_pipeline.go:190`（`resolveCCFallbackTarget` 解析 URL，实际发送在 `:354`）。**但 `sendCCUpstreamRequest` 的 qoder 分支（`cc_pipeline.go:349-354`）对两处调用点都已走 `sendQoderChat`（含换主机重试）**，唯一差异是 `qoderChatURL` 解析阶段失败不会换候选主机。清单 PLAT-08 的"部分调用点未走 sendQoderChat"表述已不准确；方案"让这两处走 sendQoderChat"属重复建议。建议复核后降级为 Info/REJECTED（见问题 5）。 |
| PLAT-09 | PASS | `gemini_messages_compat_service.go:3493-3521` 的 `stripGeminiFunctionIDs` 删除 `functionCall/functionResponse` 的 `id`（:3494 注释说明防上游拒绝）；响应侧 `:2380,3067` 随机生成 `toolu_`，与描述一致。 |
| PLAT-11 | PASS（PLAUSIBLE） | `InvalidateAvailableModelsCache`（`gateway_service.go:1599`）全仓无生产调用方（仅 `gateway_hotpath_optimization_test.go`）；缓存 TTL 由 `resolveModelsListCacheTTL` 默认 15s、配置上限 30s（`config.go:3691` 校验 10-30），与描述一致。 |
| PAY-02 | PASS | `payment_fulfillment.go:44-76`：only on `IsNotFound` + `sub2_` 前缀解析；`orderIDPrefix = "sub2_"`（`payment_service.go:49`）。 |
| PAY-05 | PASS | `redeem_service.go:334-384`（`checkRedeemRateLimit` 与 `acquireRedeemLock` 在 Redis 错误/cache nil 时放行）、`redeem_code_repo.go:324-340`（`Use` 用 `IDEQ + StatusEQ(StatusUnused)`，affected==0 报 used）；事务在 `redeem_service.go:470-480`，`Use` 调用在 :482 左右。清单引用 `:470-495` 准确。 |
| PAY-04 | PASS-ADJUST | `payment_fulfillment.go:119-125` 容差：minorUnit<=2 统一 0.01；`payment/currency.go:67-115` 的 `CurrencyMinorUnit`/`AmountToMinorUnit` 存在。**方案引用 `amount_minor` 字段不存在**（见问题 2）。 |
| PAY-06 | PASS | `redeem_service.go:200-210,264-270`：仅拒绝 `Value == 0`，注释"支持负数用于退款"；`CreateCode` 同样只拒 0。 |
| PAY-10 | PASS | `batch_image.go:371-416`：非终态 `to == failed` 直接 true；终态仅允许 `output_deleted`，与描述一致。 |
| ADMIN-01 | PASS-ADJUST | `ent/user.go:29` `PasswordHash string \`json:"password_hash,omitempty"\``（清单写 29 正确）；DTO 映射确实不含 PasswordHash，未发现 handler 直接序列化 `dbent.User`（`auth_dingtalk_oauth.go` 等只取实体字段后转 service.User）。 |
| ADMIN-06 | PASS | `dto/mappers.go:80-87` 的 `APIKeyFromService` 原样赋 `Key: k.Key`（:87）；管理端用户/分组 key 列表（`admin/user_handler.go:432-443`、`admin/group_handler.go:976-987`）均经该 DTO，明文返回。 |
| ADMIN-08 | PASS-ADJUST | `audit_log_handler.go:104-168`：现场 TOTP + 禁止 admin API key + `ClearAll` 同步留痕（`audit_log_service.go:104-129`）均属实。**但"缺保留期"不成立**：系统已有 `audit_log_retention_days`（默认 180，`setting_features.go:262-280`，后台 `runRetentionLoop` 每日清理）。真正的缺口是 `ClearAll` 用 `TruncateAll` 一键清空、不受保留期约束。严重度保持 Low 可以，方案第 392 行应改为"复用已有保留期 + 禁止清空最近 N 天 + 归档表/双控"。 |
| ADMIN-09 | PASS | `migrations_runner.go:74-102`（兼容规则表，含 054/061/109/…/220 共 18 条）、`:486-497`（双端 checksum 校验）；方案"加到期版本/日期"可行。 |
| ADMIN-11 | PASS | `promo_handler.go:32,41`：`MaxUses int \`binding:"min=0"\``，注释"0=无限"；Create（:97-107）直接透传。 |
| ADMIN-12 | PASS | `admin/user_handler.go:326-331`（自降级拦截仅在 handler）+ `admin_user.go:197,253-265`（service 只做"最后管理员"检查，无 actor 概念）；`UpdateUser` 无 actor 参数，方案改签名可行（需同步测试 stub，AGENTS.md 已有该坑点提示）。 |
| FE-03 | PASS-ADJUST | `embedded-url.ts:41-44` catch 返回原始 `baseUrl`（清单写 41-43，实际 43-45，偏差 2 行）；模板 `CustomPageView.vue:80,97-121` 确实用 `isValidUrl`（http/https 前缀）过滤，`javascript:` 不会进入 href/iframe 绑定，清单"防御性缺口"定性正确。 |
| FE-04 | PASS | `KeyUsageView.vue:557-600` 的 `triggerRingAnimation` 在 `nextTick` 内启动新 RAF/定时器，`onUnmounted`（:955-958）只取消当前 handle；nextTick 回调无卸载判断，存在卸载后启动新 RAF 的窗口。 |
| FE-06 | PASS | `router/index.ts:510-513`：`path === allowedPath || path.startsWith(allowedPath)`，`/loginX` 会被 `/login` 前缀命中。 |
| FE-11 | PASS | `HomeView.vue:496-499`：`startsWith('http://') || startsWith('https://')` 即判为 iframe URL，无 `new URL` 解析。 |

### Info（8 条，全部核对，PASS / PASS-ADJUST）

| ID | 结论 | 代码事实核对 |
|---|---|---|
| AUTH-01 | PASS | `auth_service.go:557-563`（Login 内 GenerateToken 后返回）；`auth_handler.go:252-257` `_ = token` 后 `respondWithTokenPair` 重新签发（:124）。 |
| AUTH-11 | PASS | `admin_auth.go:121-154`：验证 admin API key 后 `GetFirstAdmin` 固定映射（:140-149），无 key 主体区分。 |
| GW-10 | PASS | `usage_billing_repo.go:66-110`：`INSERT ... ON CONFLICT (request_id, api_key_id) DO NOTHING` + archive 表二次校验，指纹冲突返回 `ErrUsageBillingRequestConflict`；`migrations/071_add_usage_billing_dedup.sql` 存在（列名与清单一致）。 |
| GW-11 | PASS | `group_model_allowlist.go:45-49,95-104`：对三条 Responses WS 路由跳过中间件，由 handler 首帧（`openai_gateway_handler.go:2580-2585`）与后续 turn（:3073-3078）逐帧校验；Grok Realtime query model 在 `group_model_allowlist.go` 的 query 兜底（约 :75-78）覆盖。 |
| PAY-11 | PASS | `quotaview/helpers.go:77-82`：`NeedsMonthlyReset` 为 `now-start >= 30d` 滚动窗口，注释"与订阅模式一致"；billing 缓存 `nextMonthlyResetFrom`（`billing_cache_service.go`）与 `user_platform_quota_repo.go:307-315` 同口径，三处一致属实。 |
| FE-09 | PASS-ADJUST | 实测 `: any` = 268、`as any` = 159（清单写 271/159；`: any` 差 3，可能因代码变动或统计口径含 `:any` 无空格，实测 `:any` 另有 6 处）。数量级结论不变。 |
| FE-10 | PASS | `CustomPageView.vue:388-411` 的 `injectCopyButtons` 每次对 `pre` 添加匿名 `addEventListener('click')`，无移除逻辑（按钮随 innerHTML 替换被 GC，风险低，与清单一致）。 |
| FE-12 | PASS | `HomeView.vue:502` `document.documentElement` 在 setup 顶层直接访问；`KeyUsageView.vue` 的 `document` 访问也在 setup 期。 |

### REJECTED（4 条，全部维持）

| ID | 结论 | 复核证据 |
|---|---|---|
| PAY-08 | 维持 REJECTED | Docker `golang:1.27.2` 实测：`net.ParseIP("::ffff:127.0.0.1")` → `127.0.0.1`（To4 非空，IsLoopback=true）；`::ffff:169.254.169.254` → link-local 命中；`::ffff:10.0.0.1`/`::ffff:192.168.1.1` → `IPNet.Contains` 命中；`::ffff:8.8.8.8` 正确放行。`isPrivateIP`（`channel_monitor_ssrf.go:69-82`）对 16 字节 `net.IP` 的 CIDR Contains 同样成立。 |
| PAY-12 | 维持 REJECTED | `routes/admin.go:27` admin 组 `Use(adminAuth)`；`routes/admin.go:946-953` 仅 admin 组注册 `PUT /channel-monitor-v2/config`；`admin_auth.go:204-208` `validateJWTForAdmin` 显式 `user.IsAdmin()`；`routes/user.go:146-155` 用户端仅 6 个 GET。handler 本身无额外角色校验，但路由层已保证。 |
| PLAT-07 | 维持 REJECTED | `openai_gateway_workbuddy.go:171` `!sawChoice || index > maxChoice` 更新上限；`:266` `for i := 0; i <= a.maxChoice; i++`，index 0 正确覆盖；改 `>=` 确会漏掉最大 index 或产生越界语义。 |
| PLAT-10 | 维持 REJECTED | `antigravity_gemini_thinking_variant.go:76-85`：`budget <= lowMax` → low，注释明确"含 0（关闭思考）：上游没有无思考变体，取最浅档，budget 本身仍原样透传"；测试 `antigravity_gemini_thinking_variant_test.go:34` 期望 low。 |

## 方案引用事实核对（任务第 3 项）

| 事实 | 结论 |
|---|---|
| `internal/setup/setup.go` 打印密码 | **正确**，`backend/internal/setup/setup.go:444-446`（清单/方案写 445 精确命中）。 |
| `frontend/src/utils/sanitize.ts` 存在 `sanitizeSvg` | **正确**，`:3-6` `DOMPurify.sanitize(svg, { USE_PROFILES: { svg: true, svgFilters: true } })`。 |
| `dompurify` 在 package.json | **正确**，`frontend/package.json:26` `"dompurify": "^3.4.14"`。 |
| `extractAnthropicSSEDataLine` 位置与兼容性 | **正确**，`service/gateway_anthropic_passthrough.go:611-623`，`HasPrefix("data:")` 后跳过空格/Tab，无空格可用。 |
| `redeem_code_repo.go` 的 `Use` 乐观锁 | **正确**，`repository/redeem_code_repo.go:324-340`，`IDEQ + StatusEQ(StatusUnused)`，affected==0 → `ErrRedeemCodeUsed`。 |
| `ent/schema/user.go` 无 token_version | **正确**（grep 无命中）；方案新增 `250_so_user_token_version.sql` 编号可用（当前最大 249）。 |
| `usage_billing_dedup` migration 存在 | **正确**，`migrations/071_add_usage_billing_dedup.sql`，列 `(request_id, api_key_id, request_fingerprint)` + 唯一索引；另有 072/073 归档/BRIN。 |
| `billing.inflight_reservation.fail_closed_on_unpriced` 配置存在 | **正确**，`config.go:945-947` + `viper.SetDefault(..., false)`（:2138）；运行时由 `InflightReservationFailClosedOnUnpriced()` 读取。 |
| `scheduled_test_handler.go` 直接 c.JSON service 结构 | **正确**，`:52,82,126,159`（方案写 52,82,126,159，与实测一致）；service 结构带 json tag。 |
| `concurrency_service.go` fail-open 位置 | **正确**，`:495-508`（IncrementWaitCount）、`:417-449`（TrackAPIKeySlot）；清单 GW-08 写 "417-455,495-508" 与实测一致。 |
| `validateJWTForAdmin` 有 IsAdmin 校验 | **正确**，`server/middleware/admin_auth.go:204-208`。 |

## 清单与方案 ID 对应性（任务第 5 项）

- FINDINGS 表格行：68 行 = High 4 + Medium 19 + Low 33 + Info 8 + REJECTED 4；其中 Low 表的 `GW-04/COMP-01` 一行含两个 ID，展开后唯一 ID 为 **69**（含 REJECTED），排除 REJECTED 后 **65**。
- SOLUTIONS 章节：**64** 个（含 `GW-04/COMP-01` 合并章节），排除 REJECTED 后覆盖 FINDINGS 的全部 65 个有效 ID（`GW-04` 与 `COMP-01` 共用一章）。
- **一一对应结论：有效 ID 全覆盖，无"有清单无方案"；REJECTED 4 条在方案中无章节（正确）。**
- 需修正的仅是两份文档各自头部的统计数字（FINDINGS "64"、SOLUTIONS "61"），以及 SOLUTIONS P2 列表补 PAY-05；另需按"发现的问题清单"修正 PAY-04/GW-06/ADMIN-08 的方案表述并复核 PLAT-08 严重度。
- 旧报告中的 `PLAT-02`、`PAY-09` 已被合并进 `GW-04/COMP-01`，终版两份文档未单列，属正确处理；建议在 FINDINGS 的"关键修正"表加一行注明，便于追溯（问题 3）。

## 发现的问题清单（按严重度）

### 高（文档自洽性，必须改）

1. **统计数字三处不一致**
   - FINDINGS 第 21 行"唯一问题条目：64" → 按"唯一 ID"应为 **65**（4+19+33+8=64 个表格条目，`GW-04/COMP-01` 一行含 2 个 ID）。建议改为"表格条目 64 / 唯一 ID 65"。
   - SOLUTIONS 第 3 行"对应清单（终版，61 条有效问题）" → 应为 **64 个章节 / 65 个有效 ID**。
   - SOLUTIONS 第 7-10 行的 P0/P1/P2 分类合计为 64 章节（P0 4 + P1 19 + P2 41），与"61"也不符；P2 描述还需补列 PAY-05（见下条）。
   - 建议统一表述："有效表格条目 64（展开后 65 个唯一 ID）；方案章节 64（GW-04/COMP-01 合并为一章）"。

### 中（方案可执行性）

2. **PAY-04 方案引用不存在的 `amount_minor` 字段**（SOLUTIONS 第 367 行）。`ent/schema/payment_order.go` 金额为 `field.Float("amount"/"pay_amount")` + `decimal(20,2)`，无 minor 列。应改为"用 `payment.AmountToMinorUnit(payment.FormatAmountForCurrency(o.PayAmount, currency), currency)` 现场换算后比较"。
3. **GW-06 方案第 1 步的"网关 API Key 维度"在 `RateLimiter` 上没有落点**（SOLUTIONS 第 133 行）。网关路由不使用 `middleware.RateLimiter`；管理面使用 `PanelRateLimiter`（固定 fail-open）。应改为："管理面 `PanelRateLimiter` 增加 fail-closed 开关；网关侧维持 API Key 用量/并发限流，不引入 IP 限流"。
4. **ADMIN-08 方案部分重复已有能力**（SOLUTIONS 第 392 行）。`audit_log_retention_days` 配置与每日清理循环均已存在（默认 180 天，`setting_features.go:262-280`、`audit_log_service.go:187-235`），"增加保留期"应改为"复用既有保留期"；真正的缺口只剩"`ClearAll` 不受保留期约束、可一次清空近期日志"与"无双人审批/归档"，方案应聚焦这两点。
5. **PLAT-08 应复核降级（实质性发现）**：`sendCCUpstreamRequest`（`openai_gateway_cc_pipeline.go:349-354`）在 `account.Platform == PlatformQoder` 时统一调用 `sendQoderChat`（含换主机重试），而账号测试（`openai_gateway_qoder.go:825`）与 CC/Responses/Anthropic 回退路径（`cc_pipeline.go:190,354`）都经该函数发送，**因此"部分调用点未走 sendQoderChat"的缺口已不存在**；剩余差异仅限 `qoderChatURL` 解析失败时不换候选主机（极小）。建议将 FINDINGS PLAT-08 改为 Info 或移入 REJECTED，并删除 SOLUTIONS 第 344-345 行的重复方案。

### 低（表述精确性）

6. **GW-04/COMP-01 的"死代码"应限定为"生产路径死代码"**：`Ambiguous` 字段仍被 `TestCompositeRouteResolverFlagsOAuthAmbiguousOwnership` 等测试构造命中（`composite_route_resolver.go:168-176` 可达），且字段本身在 `composite_model_route.go:37`。建议 FINDINGS 第 97 行与 SOLUTIONS 第 288 行补"（仅测试可达）"。
7. **FE-03 行号偏差**：`embedded-url.ts` catch 分支实测 `43-45`（清单写 41-43，偏差 2 行，PASS）。
8. **FE-09 计数偏差**：实测 `: any` 268（清单 271）、`as any` 159（一致）。
9. **AUTH-01 位置偏差**：`auth_service.go` Login 的 GenerateToken 实际 `557-561`（清单写 556-564，PASS）。
10. **PLAT-05 描述可更精确**：`eventType` 是 `consumeClaudeEvents` 的局部变量而非结构体字段，"跨事件残留"仅在处理器未输出 `event:` 行或分块异常时发生；当前处理器 `formatSSE` 恒输出 `event:`（`pkg/antigravity/stream_transformer.go:575`），影响面确实很小，Low 合理。
11. （通过项，无需修改）**AUTH-04 方案 `250_so_user_token_version.sql` 编号可用**：当前最大迁移为 `249_so_model_benchmark_difficulty_range.sql`，`250_so_` 前缀未被占用。
12. （通过项，无需修改）**AUTH-07 方案前提成立**：`VerifyCaptchaForRegister` 在"邮箱验证开启且 verifyCode 非空"时跳过 captcha；确认 `VerifyCode` 成功后即删除验证码，方案提出的"一次性票据"需新增，当前无 `captcha_ticket:<email>` 机制（方案已说明新增，正确）。

## 统计一致性核对

| 口径 | FINDINGS 声明 | 实测 | 结论 |
|---|---|---|---|
| High 行数 | 4 | 4 | 一致 |
| Medium 行数 | 19 | 19 | 一致 |
| Low 行数 | 33 | 33 | 一致 |
| Info 行数 | 8 | 8 | 一致 |
| REJECTED | 4 | 4 | 一致 |
| "唯一问题条目" | 64 | 表格条目 64 / 唯一 ID **65** | **口径不自洽（`GW-04/COMP-01` 一行展开为 2 个 ID）** |
| 表格行数 | — | 68 | 与"64 有效 + 4 REJECTED"按行计算一致 |
| SOLUTIONS 声明 | 61 条 | 章节 64（P0 4 / P1 19 / P2 41），覆盖 65 个有效 ID | **声明数字错误** |
| 方案覆盖 | "已按终版重写" | 65/65 有效 ID 全覆盖 | 一致（仅统计数字写错） |
| PLAUSIBLE | GW-09、PLAT-11 | 与 FINDINGS 第 27 行一致 | 一致 |

## 最终判定

- **代码真实性：通过。** 修订后的终版清单在代码层面没有发现新的幻觉或误报；4 条 REJECTED 判断经独立复核（含 Docker 运行时）全部成立；PAY-05、GW-08、ADMIN-10 三条补充条目均能在代码中定位到对应实现。
- **文档可靠性：有条件通过。** 需修正统计口径（FINDINGS 标注"表格条目 64 / 唯一 ID 65"、SOLUTIONS "61"→"64 章节/65 ID"、P2 补 PAY-05）、1 处不存在的字段引用（PAY-04 `amount_minor`）、1 处方案落点错误（GW-06 管理面/网关限流混淆）、1 处重复建议（ADMIN-08 保留期），并将 PLAT-08 降级为 Info/REJECTED（`sendCCUpstreamRequest` 的 qoder 分支已统一走 `sendQoderChat`）。
- **建议：** 完成上述文档修订后，可判定终版清单与方案为"真实、可靠、自洽"，并按其中 P0/P1 清单进入修复实施。
