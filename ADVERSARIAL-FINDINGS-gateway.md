# Adversarial Findings - Gateway Core / Proxy / Billing / Concurrency / Rate Limit / Failover / Inflight / API Key Billing

范围：sub2api-so 后端 API 网关核心、代理、计费、并发控制、限流、失败转移、inflight 预留、API Key 计费模块。本次仅代码审查与测试运行，未修改业务代码。

## 测试执行

- `go test -tags=unit -run 'Gateway|Failover|Concurrency|ApiKey|KeyBilling|Inflight|Billing' ./internal/...`
- 结果：handler/service/repository/middleware 大部分通过；service 包存在 2 个失败，均为复合分组所有权解析期望与当前实现不一致（`TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants`、`TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership`），未见 panic。具体失败：`resolveCompositeModelOwnership` 对多平台账号映射返回 `Matched=true` 且从不设置 `Ambiguous`，与测试期望的 `Ambiguous=true` 相反。

## Findings

### F1. 内容审核在服务异常时 fail-open，可绕过审核
- 严重度：High
- 位置：`backend/internal/handler/content_moderation_helper.go:41-66` (`runContentModeration`)
- 描述：`svc.Check` 返回 error 时直接 `return nil`，调用方据此视为未命中审核规则并放行请求。审核引擎、Redis/DB、网络抖动或配置错误都会导致审核完全失效，攻击者可通过触发审核依赖故障或在审核服务不可用窗口发送违规内容。
- 预期 vs 实际：预期审核失败时至少 fail-closed 或按策略阻断/记录；实际直接放行。
- 影响：内容审核绕过，策略失效。
- 建议：区分可重试与不可用错误；审核服务不可用时按配置 fail-closed 或降级到本地关键词/规则；记录审计并告警。

### F2. inflight 余额预留默认 fail-open，未定价模型/Redis 故障时不预留
- 严重度：Medium/High
- 位置：`backend/internal/handler/gateway_inflight_reservation.go:80-85`；`backend/internal/service/billing_inflight_reservation.go:257-278`
- 描述：`reserveInflightBalanceCtx` 在无法定价且未配置 `fail_closed_on_unpriced` 时放行；`reserveInflight` 在余额读取失败、Redis 执行失败时也放行且不登记预留。并发请求可在预检瞬间看到相同余额，集体透支；未定价/免费估算模型完全无预留。
- 预期 vs 实际：预期在途预留覆盖所有计费路径或至少对高风险未定价模型 fail-closed；实际默认 fail-open。
- 影响：余额模式下并发透支、超额计费。
- 建议：对无法定价的模型默认拒绝或使用保守上限；Redis/余额读取失败时按配置 fail-closed；为预留失败增加审计。

### F3. API Key 额度/速率限制检查 fail-open，存在 TOCTOU 超额
- 严重度：Medium/High
- 位置：`backend/internal/service/billing_cache_service.go:576-597` (`checkAPIKeyRateLimits`)；`backend/internal/service/api_key_service.go:1158-1203` (`UpdateQuotaUsed`)
- 描述：无缓存时 loader 为 nil 或 DB 错误直接 `return nil`；缓存未命中 DB 失败也放行；`UpdateQuotaUsed` 在 GetByID/Update 失败时吞错。预检与扣减非原子，高并发下两个请求可同时通过限额检查后才扣减，导致超额。
- 预期 vs 实际：预期限额检查与扣减原子或至少失败时阻断；实际 fail-open 且存在竞态。
- 影响：API Key 额度/窗口限额被突破，资源滥用。
- 建议：关键路径 fail-closed；使用原子条件扣减（CAS）或数据库乐观锁；更新失败时重试/告警。

### F4. 复合分组模型所有权 `Ambiguous` 从未设置，OAuth 歧义请求不被拒绝
- 严重度：High
- 位置：`backend/internal/service/gateway_service.go:1499-1545` (`resolveCompositeModelOwnership`)
- 描述：`CompositeModelOwnership.Ambiguous` 在该函数中从未赋值，恒为 false。下游 `composite_route_resolver.go:168` 的歧义分支（含 `CompositeRouteReasonOAuthAmbiguous`）永远不触发，`openai_gateway_handler.go`/路由层的 OAuth 歧义拒绝逻辑失效。多平台账号映射会被当作可调度集合，OpenAI 网关 OAuth/setup-token 凭据可能被路由到默认 Anthropic handler，导致转发失败或绕过预期拒绝。
- 预期 vs 实际：预期多平台/歧义所有权正确标记 `Ambiguous`；实际永远 false，测试 `TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants` 失败。
- 影响：复合分组路由错误、OAuth 歧义请求未被拒绝。
- 建议：按所有权语义设置 `Ambiguous`（如多平台且非同族/无法唯一归属时），并修正测试/缓存失效；确保 OAuth 歧义拒绝路径可达。

### F5. OAuth 429 同账号重试不受 retryLimit 约束，可能放大请求
- 严重度：Medium
- 位置：`backend/internal/handler/failover_loop.go:97-102` (`sameAccountRetryAllowed`)
- 描述：当 `failoverErr.SameAccountRetryDeadline` 非零（OAuth 429 路径）时直接 `return true`，忽略 `retryLimit` 与 `SameAccountRetryMax`。在上游持续 429 且 deadline 未到时，请求会在同一账号上无限重试，每次等待退避，占用连接与上游配额。
- 预期 vs 实际：预期重试次数有硬上限；实际仅受时间窗口约束。
- 影响：请求放大、上游压力、客户端长时间阻塞。
- 建议：为 OAuth 429 路径增加最大重试次数/总等待上限，或在 deadline 内仍限制次数。

### F6. 限流默认 fail-open 且 IP 解析依赖可信代理配置
- 严重度：Medium
- 位置：`backend/internal/middleware/rate_limiter.go:132-149` (`LimitWithOptions`)；`backend/internal/middleware/rate_limiter.go:116-121` (`clientIPForRateLimit`)
- 描述：Redis 错误默认放行（`RateLimitFailOpen`）；客户端 IP 依赖 `ippkg.GetSecurityClientIP` 与 trusted proxies 配置，若反代/可信代理配置错误，所有用户可能坍缩到同一 IP 桶（整体误拦截）或被解析为代理地址（限流失效）。
- 预期 vs 实际：预期限流在 Redis 故障时可按策略 fail-closed，且 IP 解析稳定；实际默认 fail-open 且对代理配置敏感。
- 影响：限流绕过或全局误杀。
- 建议：关键入口默认 fail-closed 或增加本地兜底；校验/文档化 trusted proxies；增加 IP 解析单测。

### F7. 计费/用量更新错误被吞，可能导致漏计费
- 严重度：Medium
- 位置：`backend/internal/service/api_key_service.go:1185-1200` (`UpdateQuotaUsed`)；`backend/internal/service/billing_cache_service.go:596-598`
- 描述：`UpdateQuotaUsed` 在 GetByID/Update 失败时直接返回 nil；缓存更新 DB 失败放行。异步计费任务若部分失败，上游已消耗的请求可能漏记或额度未更新，且无重试/补偿。
- 预期 vs 实际：预期计费失败可重试/补偿并告警；实际静默吞错。
- 影响：漏计费/额度不一致。
- 建议：计费任务失败进入重试队列或死信；关键更新失败返回错误并告警；增加幂等去重。

### F8. 用户并发/等待计数在 Redis 不可用时 fail-open
- 严重度：Low/Medium
- 位置：`backend/internal/service/concurrency_service.go:494-508` (`IncrementWaitCount`)；`backend/internal/service/concurrency_service.go:417-440` (`TrackAPIKeySlot`)
- 描述：Redis 不可用或 cache 为 nil 时直接放行/返回 no-op，不记录槽位。Redis 故障期间并发限制与 API Key 活跃计数完全失效，可能造成上游过载与统计失真。
- 预期 vs 实际：预期故障时至少有本地/保守限制；实际完全放行。
- 影响：并发超限、上游过载。
- 建议：增加本地令牌桶/信号量兜底；Redis 恢复后同步；告警。

### F9. inflight 预留续期/释放竞态与引用计数边界
- 严重度：Low/Medium
- 位置：`backend/internal/service/billing_inflight_reservation.go:79-130` (`Acquire`/`HandlerDone`/`decRef`/`Release`)
- 描述：`Acquire` 在 `refs<=0` 时返回 no-op；`HandlerDone` 通过 `stopOnce` 调用 `decRef`。若计费任务在 handler 已结束前完成并释放引用，可能触发 `Release` 提前释放 Redis 预留，而 handler 仍持有引用； Conversely, `Release` 强制 `refs.Store(0)`，可能与并发 `Acquire` CAS 竞争。极端时序下预留可能提前释放或泄漏至 TTL。
- 预期 vs 实际：预期引用计数严格保证“handler 结束且计费完成才释放”；实际存在竞态窗口。
- 影响：预留提前释放导致并发透支，或泄漏至 TTL。
- 建议：用原子状态机/互斥保护 Acquire 与 Release；增加并发单测与 race detector。

### F10. 失败转移在流已写入时仍可能提交部分 usage，存在重复计费风险
- 严重度：Low/Medium
- 位置：`backend/internal/handler/gateway_handler.go:1105-1110` (`submitForwardUsage` on error path)；`backend/internal/handler/gateway_handler.go:938-994`
- 描述：错误路径若 `result != nil` 会提交 usage 记录，而 failover 错误恒定 `result=nil` 不重复；但流中断场景可能同时触发 handler defer 的 inflight 释放与异步计费，若上游重试/客户端重试，幂等键（request payload hash）可能重复，造成重复计费。
- 预期 vs 实际：预期 usage 记录具备幂等去重；实际依赖 payload hash，可能重复。
- 影响：重复计费。
- 建议：引入请求级幂等 ID（request_id/trace_id）去重；计费任务去重。

### F11. API Key 模型白名单 WebSocket 路径跳过中间件校验
- 严重度：Medium
- 位置：`backend/internal/server/middleware/group_model_allowlist.go:45-49` (`isResponsesWebSocketRoute`)
- 描述：Responses WebSocket 入口在中间件层跳过模型白名单校验，依赖 handler 逐帧校验。若 handler 某一帧/路径未校验或升级请求伪造，可能绕过模型白名单。
- 预期 vs 实际：预期所有入口统一校验；实际 WS 路径依赖后续 handler。
- 影响：模型白名单绕过风险。
- 建议：在 WS 升级与首帧处强制校验；增加覆盖测试。

### F12. 可用渠道/模型列表在账号映射为空时回落默认列表，可能暴露未授权模型
- 严重度：Low
- 位置：`backend/internal/handler/gateway_handler.go:1194-1219` (`Models`)；`backend/internal/handler/gateway_handler.go:1457-1468` (`modelListingSource`)
- 描述：当账号映射为空时回落平台默认模型列表；结合模型白名单过滤前的来源选择，可能在特定平台/空映射场景暴露默认模型集合，与实际可调度账号不一致。
- 预期 vs 实际：预期仅暴露实际可调度模型；实际可能回落默认列表。
- 影响：信息泄露/误导。
- 建议：白名单/调度双重校验；空映射时返回空而非默认（或明确产品语义）。

## 说明

- 本次未修改代码。
- 建议优先处理 F1、F2、F3、F4、F5（安全边界/计费/路由）。
