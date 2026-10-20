# Adversarial Findings - Payment / Subscription / Redeem / Quota / Channel Monitor / Batch Image / Composite / Announcement

范围：sub2api-so 后端支付、订阅、兑换码、额度、渠道监控 v2、批量图像/异步任务、复合分组、公告、配额视图模块。本次仅代码审查与测试运行，未修改业务代码。

## 测试执行

- `go test -tags=unit -run 'Payment|Subscription|Redeem|Quota|ChannelMonitor|Composite|BatchImage|Announcement' ./internal/...`
- 结果：大部分通过；service 包有 2 个失败，均为测试期望字符串与当前实现返回文案不一致（`TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants`、`TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership`），不是运行时 panic。

## Findings

### F1. Webhook 服务商实例选择发生在验签之前，且信任未验签 body
- 严重度：High
- 位置：`backend/internal/handler/payment_webhook_handler.go:147-171` (`extractOutTradeNo`)，调用点 `:86-99`
- 描述：`extractOutTradeNo` 直接解析原始 webhook body 取出 `out_trade_no` / `merchant_order_id`，再用它选择 provider 实例。攻击者可在未签名/无效签名请求中填入任意订单号，诱导服务端选择特定 provider 实例后再验签；在多实例场景下可能造成实例混淆、错误密钥/配置上下文，或触发额外日志/分支。
- 建议：实例选择应基于不可伪造的字段（如 provider 指定的 merchant id / 实例标识）或在验签成功后重新解析；对无法可靠提取时应走单实例/拒绝路径，不能让攻击者控制路由。

### F2. 支付通知存在 legacy `sub2_<id>` 订单 ID 回退，允许按数字 ID 命中订单
- 严重度：Medium
- 位置：`backend/internal/service/payment_fulfillment.go:44-56`, `59-76`
- 描述：`HandlePaymentNotification` 在 `out_trade_no` 查不到时，若 payload 为 `sub2_<id>` 且为 NotFound，则直接按数据库 ID 调用 `confirmPayment`。攻击者若能获得/猜测订单 ID，并配合可验签通知，可能绕过 out_trade_no 的唯一性/隔离语义，针对特定订单发起支付确认流程。
- 建议：移除或严格限制 legacy 回退（仅迁移窗口内、仅特定 provider/环境），并要求订单状态与通知 trade_no/out_trade_no 严格匹配。

### F3. 已取消/已过期订单可在宽限期内被 webhook 重新标记为已支付并履约
- 严重度：Medium
- 位置：`backend/internal/service/payment_fulfillment.go:150-187`, `189-214`
- 描述：`toPaid` 允许 `cancelled` 或 `expired`（且更新时间在 5 分钟宽限内）订单转为 `paid`；`alreadyProcessed` 对 expired 也记录审计但不履约。管理员取消或系统过期后的订单若在宽限内收到支付成功通知，仍会被恢复并执行履约，可能与取消语义冲突。
- 建议：明确取消/过期订单的最终性；如需支持迟到支付，应走单独恢复流程并要求额外校验/人工确认。

### F4. 支付金额容差对所有 <=2 位小数货币统一使用 0.01，允许小额短付
- 严重度：Low/Medium
- 位置：`backend/internal/service/payment_fulfillment.go:104-125`, `backend/internal/service/payment_service.go:42-47`
- 描述：`paymentAmountToleranceForCurrency` 对 minor unit <= 2 的货币（包括 USD/EUR 等）使用 `amountToleranceCNY=0.01`。对于大额订单，攻击者/上游四舍五入差异可能允许最多约 1 分钱短付仍通过；高并发/高频可累积。
- 建议：按货币最小单位/比例设置容差，或改为基于 minor unit 的整数金额比较，避免浮点容差。

### F5. Redis 不可用时兑换锁与失败计数降级为无保护
- 严重度：High
- 位置：`backend/internal/service/redeem_service.go:335-390`
- 描述：`checkRedeemRateLimit` 在 cache 错误时返回 nil；`acquireRedeemLock` 在 cache 为 nil 或 Acquire 错误时返回 true。Redis 故障或不可用时，兑换码并发使用保护与失败次数限制完全失效，可能允许同一码重复兑换或暴力尝试。
- 建议：Redis 不可用时应 fail-closed 或至少依赖数据库乐观锁；rate limit 失败不应默认放行。

### F6. 兑换码创建允许负值，可生成扣减余额/并发数的码
- 严重度：Medium
- 位置：`backend/internal/service/redeem_service.go:200-277` (`GenerateCodes`, `CreateCode`)
- 描述：仅拒绝 `Value == 0`（邀请码除外），允许负数。虽然兑换流程支持负数扣减，但若管理接口或集成调用被滥用，可创建扣减用户余额/并发额度的兑换码，造成资源扣除或拒绝服务。
- 建议：对管理创建接口强制 value 范围/符号校验，并区分“退款码”权限；负值创建应仅限高权限/审计场景。

### F7. 公告内容原样存储，无 XSS 清洗
- 严重度：Medium
- 位置：`backend/internal/service/announcement_service.go:71-134`, `136-210`
- 描述：`Create/Update` 仅 trim 空白并校验非空，不对 `Content` 做 HTML/脚本清洗。若前端以 HTML 渲染公告，管理员或被控管理员账号可注入脚本，影响所有可见用户。
- 建议：后端存储前做白名单 HTML 清洗或强制纯文本/Markdown；前端渲染必须转义或使用安全渲染。

### F8. 渠道监控 SSRF 防护未覆盖 IPv4-mapped IPv6
- 严重度：Medium
- 位置：`backend/internal/service/channel_monitor_ssrf.go:69-82`, `114-152`
- 描述：`isPrivateIP` 对 IPv4-mapped IPv6（如 `::ffff:127.0.0.1`）可能不识别为私网，且 blocked CIDR 列表未包含映射段。攻击者可能通过 IPv4-mapped IPv6 主机名/字面量绕过私网/loopback/metadata 拒绝策略，触发 SSRF。
- 建议：在 `isPrivateIP` 中先将 IPv4-mapped IPv6 转换为 IPv4 再判断；blocked CIDR 增加映射段。

### F9. 复合分组路由在所有权解析失败时回退到模型名猜测，可能误路由
- 严重度：Medium
- 位置：`backend/internal/service/composite_route_resolver.go:134-206`
- 描述：当 `modelOwnershipResolver` 返回错误时，仅对不可识别模型报错；可识别模型继续执行，最终可能回退到 `DetectModelPlatform(model)` 猜测平台。在账号目录暂时不可用时，请求可能被路由到错误网关/平台，造成跨平台误发或鉴权失败。
- 建议：所有者解析失败时对需要账号目录的请求 fail-closed，或明确降级策略与可观测性，避免静默猜测。

### F10. 批量图像状态机允许任意非终态转为 failed，且终态可转 output_deleted
- 严重度：Low/Medium
- 位置：`backend/internal/service/batch_image.go:371-416` (`CanTransitionBatchImageJob`)
- 描述：`to == failed` 对任意 from 状态返回 true；终态仅允许转 `output_deleted`。若状态转换入口缺少角色/所有权校验，攻击者或并发 worker 可能将进行中的任务强制置为 failed，或在完成/取消/失败后触发输出删除流程，影响计费/清理。
- 建议：状态转换应由受控 worker/admin 路径触发，并记录操作者；限制 failed 转换的来源状态与权限。

### F11. 配额视图月度重置使用 30 天滚动窗口，与自然月语义可能不一致
- 严重度：Low
- 位置：`backend/internal/handler/quotaview/helpers.go:76-104`
- 描述：`NeedsMonthlyReset` 与 `NextMonthlyResetTimeFrom` 使用 `start + 30*24h` 滚动窗口，而非自然月。若产品/计费约定为自然月，展示的重置时间和实际扣费窗口可能不一致，导致用户看到错误的剩余额度。
- 建议：确认产品语义；若应为自然月，统一 billing/repo/view 的窗口计算。

### F12. 渠道监控 v2 配置更新 handler 不在 handler 内校验管理员角色
- 严重度：Medium
- 位置：`backend/internal/handler/channel_monitor_v2_handler.go:45-70`
- 描述：`UpdateConfig` 仅要求已认证 subject，不检查 `RoleAdmin`；管理员判定仅用于 Dimensions/Errors 等共享 handler。若路由注册将 `UpdateConfig` 暴露给普通用户路由或中间件配置错误，非管理员可修改全局监控配置。
- 建议：在 handler 中显式校验 admin 角色，或拆分 admin/user handler，避免依赖路由层。

## 说明

- 本次未修改代码。
- 建议优先处理 F1、F5、F8、F12（安全边界/权限/SSRF）。
