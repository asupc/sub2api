# ADVERSARIAL-FINDINGS 核实报告

- 核实日期：2026-10-17
- 范围：`sub2api-so` 全栈
- 方法：代码定位（Read/Grep）+ 关键单元测试复现（Docker golang:1.27.2）
- 未修改业务代码与主清单

## 汇总

- 主清单唯一 ID 数：71（另有占位符 `FE-?`，非真实条目）
- `CONFIRMED`：64
- `PLAUSIBLE`：5（GW-09、GW-10、PLAT-06、PLAT-07、PLAT-11）
- `DUPLICATE`：2（PLAT-02、PAY-09，与 COMP-01/GW-04 同一根因）
- `FALSE`：0（无条目被判定为完全不实；存在定位/描述偏差见下）
- 复现测试：`TestResolveCompositeModelOwnershipFlagsOpenAIGatewayOAuthClaimants`、`TestCompositeRouteResolverDoesNotGuessCrossFamilyAccountOwnership`、`TestResolveGeminiThinkingVariant`（2 子用例）均按报告描述失败；`TestConfiguredUpstreamModelsForCapabilitySyncSkipsInjectedAntigravityPassthroughs` 未单独复跑，但相关代码路径存在。

## 逐条 verdict

### 认证（AUTH）

| ID | Verdict | 证据 |
|---|---|---|
| AUTH-01 | CONFIRMED | `auth_service.go:535-564` Login 调用 GenerateToken；`auth_handler.go:252-257` `_ = token` 后 `respondWithTokenPair` 重签 |
| AUTH-02 | CONFIRMED | `auth_service.go:211-218` 返回 `ErrEmailExists`；SendVerifyCode 路径同 |
| AUTH-03 | CONFIRMED | `session_binding.go:71-73` `claims.BindingHash == ""` 直接放行 |
| AUTH-04 | CONFIRMED | `auth_service.go:1902-1911` 仅 RevokeAllUserSessions，无 Access JWT 撤销 |
| AUTH-05 | CONFIRMED | `auth_handler.go:331-413` TOTP 通过后多步失败才在 412-413 删除会话 |
| AUTH-06 | CONFIRMED | `backend_mode_guard.go:73` `strings.Contains(path, "/auth/oauth/pending/")` |
| AUTH-07 | CONFIRMED | `auth_service.go:399-403` 邮箱验证开启且有验证码时跳过 captcha |
| AUTH-08 | CONFIRMED | `auth_service.go:1625-1638` 先 ConsumePasswordResetToken 再 GetByEmail |
| AUTH-09 | CONFIRMED | `auth_linuxdo_oauth.go:1012-1018` 直接读 X-Forwarded-Proto |
| AUTH-10 | CONFIRMED | `auth_service.go:1411-1417` 上下文缺失时 bindingHash 为空 |
| AUTH-11 | CONFIRMED | `admin_auth.go:121-154` 固定 GetFirstAdmin |

### 网关（GW）

| ID | Verdict | 证据 |
|---|---|---|
| GW-01 | CONFIRMED | `content_moderation_helper.go:61-67` err 时 return nil |
| GW-02 | CONFIRMED | `gateway_inflight_reservation.go:80-85`；`billing_inflight_reservation.go:257-278` fail-open |
| GW-03 | CONFIRMED | `billing_cache_service.go:576-598`；`api_key_service.go:1185-1200` |
| GW-04/COMP-01 | CONFIRMED | `gateway_service.go:1499-1545` Ambiguous 从未赋值；测试失败证实 |
| GW-05 | CONFIRMED | `failover_loop.go:97-101` deadline 非零直接 true |
| GW-06 | CONFIRMED | `rate_limiter.go:132-149` 默认 fail-open |
| GW-07 | CONFIRMED | `api_key_service.go:1185-1200`；`billing_cache_service.go:596-598` 吞错 |
| GW-08 | CONFIRMED | `concurrency_service.go:495-508` fail-open |
| GW-09 | PLAUSIBLE | `billing_inflight_reservation.go:79-130` 引用计数竞态窗口（代码结构支持，未复现） |
| GW-10 | PLAUSIBLE | `gateway_handler.go:938-1110` 重复计费风险（依赖幂等键） |
| GW-11 | CONFIRMED | `group_model_allowlist.go:45-49` WS 路径跳过 |
| GW-12 | CONFIRMED | `gateway_handler.go:1194-1219` 空映射回落默认列表 |

### 平台（PLAT）

| ID | Verdict | 证据 |
|---|---|---|
| PLAT-01 | CONFIRMED | `antigravity_gemini_thinking_variant.go`；测试 2 子用例失败 |
| PLAT-02 | DUPLICATE | 同 COMP-01/GW-04 根因 |
| PLAT-03 | CONFIRMED | `composite_route_resolver.go` reason 文案与测试期望不一致（测试失败证实） |
| PLAT-04 | CONFIRMED | `antigravity_gateway_upstream.go:329-339` `> 0` 跳过零值 |
| PLAT-05 | CONFIRMED | `antigravity_gateway_compat_stream.go:196-207` eventType 跨事件残留 |
| PLAT-06 | PLAUSIBLE | `openai_sse_data.go` accumulator 使用 `extractOpenAISSEDataLine`（已兼容无空格）；精确 `data: ` 仅见 `antigravity_gateway_upstream.go:310-313`，报告定位/描述部分不准确 |
| PLAT-07 | PLAUSIBLE | `openai_gateway_workbuddy.go:255-287` maxChoice/maxToolCall 边界 |
| PLAT-08 | CONFIRMED | `openai_gateway_qoder.go:63-68` 仅用首个候选 |
| PLAT-09 | CONFIRMED | `gemini_messages_compat_service.go:3493-3521` 删除 function id |
| PLAT-10 | CONFIRMED | `antigravity_gemini_thinking_variant.go:76-85` budget 0 映射 low |
| PLAT-11 | PLAUSIBLE | `gateway_service.go:1505-1512` 缓存可能过期 |

### 支付/兑换/监控等（PAY）

| ID | Verdict | 证据 |
|---|---|---|
| PAY-01 | CONFIRMED | `payment_webhook_handler.go:86-171` 先 extractOutTradeNo 再验签 |
| PAY-02 | CONFIRMED | `payment_fulfillment.go:44-76` legacy `sub2_<id>` 回退 |
| PAY-03 | CONFIRMED | `payment_fulfillment.go:150-214` cancelled/expired 宽限恢复 |
| PAY-04 | CONFIRMED | `payment_fulfillment.go:119-125`；`payment_service.go:42-47` |
| PAY-05 | CONFIRMED | `redeem_service.go:335-390` fail-open |
| PAY-06 | CONFIRMED | `redeem_service.go:200-277` 仅拒绝 0 |
| PAY-07 | CONFIRMED | `announcement_service.go:71-210` 无清洗 |
| PAY-08 | CONFIRMED | `channel_monitor_ssrf.go:69-152` 未处理 IPv4-mapped IPv6 |
| PAY-09 | DUPLICATE | 同 COMP-01 根因 |
| PAY-10 | CONFIRMED | `batch_image.go:371-416` CanTransition |
| PAY-11 | CONFIRMED | `quotaview/helpers.go:76-104` 30 天滚动窗口 |
| PAY-12 | CONFIRMED | `channel_monitor_v2_handler.go:45-70` 无 admin 校验 |

### 管理后台（ADMIN）

| ID | Verdict | 证据 |
|---|---|---|
| ADMIN-01 | CONFIRMED | `ent/user.go:29` password_hash JSON tag |
| ADMIN-02 | CONFIRMED | `admin_user.go:165-178` ensureNotLastAdmin TOCTOU |
| ADMIN-03 | CONFIRMED | `admin_account.go:1312-1327` DeleteAccount 非原子 |
| ADMIN-04 | CONFIRMED | `admin_account.go:1338-1355` ClearAccountError 多写 |
| ADMIN-05 | CONFIRMED | `admin_proxy.go:172-205` BatchDeleteProxies TOCTOU |
| ADMIN-06 | CONFIRMED | `dto/mappers.go:80-87` Key 明文返回 |
| ADMIN-07 | CONFIRMED | `setup/setup.go:445` 打印密码 |
| ADMIN-08 | CONFIRMED | `internal/handler/admin/audit_log_handler.go:115` Clear |
| ADMIN-09 | CONFIRMED | `migrations_runner.go:74-206` checksum 兼容例外 |
| ADMIN-10 | CONFIRMED | `scheduled_test_handler.go:52/82/126` 直接序列化 |
| ADMIN-11 | CONFIRMED | `promo_handler.go:32` MaxUses min=0 |
| ADMIN-12 | CONFIRMED | `admin/user_handler.go:327` 自检仅 handler |

### 前端（FE）

| ID | Verdict | 证据 |
|---|---|---|
| FE-01 | CONFIRMED | `HomeView.vue:12` v-html 无净化 |
| FE-02 | CONFIRMED | `embedded-url.ts:29-30` token 进 query |
| FE-03 | CONFIRMED | `CustomPageView.vue:101` href 未校验；buildEmbeddedUrl catch 返回原始 |
| FE-04 | CONFIRMED | `KeyUsageView.vue:560-593` RAF/定时器未完全取消 |
| FE-05 | CONFIRMED | `stores/auth.ts` localStorage 存 token |
| FE-06 | CONFIRMED | `router/index.ts:511-513` 前缀匹配 |
| FE-07 | CONFIRMED | `router/index.ts:613-628` 设置加载失败 fail-open |
| FE-08 | CONFIRMED | `ImageUpload.vue:140-146` SVG 原样 emit |
| FE-09 | CONFIRMED | 全局 `: any` 271、`as any` 159（报告称 267/155，略有偏差） |
| FE-10 | CONFIRMED | `CustomPageView.vue:388-397` 监听器未移除 |
| FE-11 | CONFIRMED | `HomeView.vue:496-499` 仅前缀校验 |
| FE-12 | CONFIRMED | `HomeView.vue:502/532/535`、`KeyUsageView.vue` 直接 window/document |

### 复合（COMP）

| ID | Verdict | 证据 |
|---|---|---|
| COMP-01 | CONFIRMED | 同 GW-04 |

## 发现的不准确之处

1. **主清单统计与表格不一致**：主清单称 "High 8 / Medium 32 / Low 23 / Info 1，总计 71"；实际表格数据行约为 High 8、Medium 38、Low 22、Info 1（AUTH-11），另含占位符 `FE-?`。建议修正统计或注明占位符不计入。
2. **PLAT-06 定位/描述偏差**：报告写 `openai_sse_data.go`，但该文件的 accumulator 已通过 `extractOpenAISSEDataLine` 兼容 `data:` 无空格；精确要求 `data: ` 的是 `antigravity_gateway_upstream.go:310-313` 的 `extractSSEUsage`。建议调整位置与描述，避免误导。
3. **FE-09 数量偏差**：报告称 267 `: any` / 155 `as any`，实测 271 / 159（差异小，代码可能已变动）。
4. **ADMIN-08 路径偏差**：报告写 `backend/handler/admin/audit_log_handler.go`，实际为 `backend/internal/handler/admin/audit_log_handler.go`（少 `internal/`）。
5. **ADMIN-07 路径**：报告写 `setup/setup.go`，实际 `backend/internal/setup/setup.go`。
6. **重复条目**：PLAT-02、PAY-09 与 GW-04/COMP-01 同一根因，主清单已说明合并视图；核实认为可继续保留为关联引用，但修复时应统一处理。
7. **PLAT-03 / PLAT-10 属于"行为 vs 测试期望"差异**：代码行为与报告描述一致，但分别对应更具体的错误文案与 0→low 的既定语义，修复前需确认产品预期，不宜直接按测试期望改。

## 需要修改主清单的建议

- 修正统计数字与表格一致，或删除占位符 `FE-?` 行。
- 补充 `PLAT-06` 的准确定位（`antigravity_gateway_upstream.go`）。
- 将 `GW-09`、`GW-10`、`PLAT-07`、`PLAT-11` 标注为"需运行时/并发复现，当前为静态推断"。
- 对 `PLAT-03`、`PLAT-10` 增加"需确认产品语义"备注。
- 无 FALSE 条目，主清单内容整体可靠；建议优先处理 High 级别 8 条。