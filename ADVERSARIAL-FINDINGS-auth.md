# 对抗性测试报告 - 认证与用户安全

## 范围

后端认证/用户/会话/OAuth/验证码/权限中间件模块，主要包括：

- `backend/internal/handler/auth_handler.go`
- `backend/internal/handler/user_handler.go`
- `backend/internal/handler/admin/user_handler.go`
- `backend/internal/service/auth_service.go`
- `backend/internal/service/user_service.go`
- `backend/internal/service/totp_service.go`
- `backend/internal/server/middleware/jwt_auth.go`
- `backend/internal/server/middleware/admin_auth.go`
- `backend/internal/server/middleware/session_binding.go`
- `backend/internal/server/middleware/backend_mode_guard.go`
- `backend/internal/handler/auth_oauth_pending_flow.go`

未修改任何代码，仅产出报告。

## 发现列表

### [Medium] 登录接口重复签发并丢弃一个无状态 Access Token

- 文件: `backend/internal/service/auth_service.go:535-564`；`backend/internal/handler/auth_handler.go:252-257`
- 类别: 逻辑/安全
- 复现/触发条件: `AuthService.Login` 内部调用 `GenerateToken` 生成并返回一个 Access Token，但 `AuthHandler.Login` 将其赋值给 `_` 并改用 `respondWithTokenPair` 重新生成 Token 对。该被丢弃的 JWT 仍是签名合法、未被撤销的 Access Token，有效期直至过期。
- 预期 vs 实际: 预期登录只签发客户端实际使用的 Token；实际每次登录都会额外签发一个可被使用的 Access Token，且无法追踪/撤销。
- 影响: 增加攻击面（额外有效会话令牌）、审计与会话管理失真；在 2FA 场景下更敏感。
- 建议: 移除 `Login` 中的 `GenerateToken` 调用，或让 Handler 直接使用其返回值，避免重复签发。

### [Medium] 邮箱存在性可通过发送验证码/注册接口枚举

- 文件: `backend/internal/service/auth_service.go:211-218`；`backend/internal/service/auth_service.go:314-366`
- 类别: 信息泄露
- 复现/触发条件: `RegisterWithVerification` 返回 `ErrEmailExists`，`SendVerifyCode`/`SendVerifyCodeAsync` 同样返回 `ErrEmailExists`。攻击者可对目标邮箱调用接口并根据错误码判断账号是否存在。相比之下 `ForgotPassword` 已做统一响应防枚举。
- 预期 vs 实际: 预期所有公开认证入口不泄露账号存在性；实际注册/验证码发送入口泄露。
- 影响: 用户枚举，便于后续撞库、钓鱼或定向攻击。
- 建议: 对公开入口返回统一的“已发送/处理中”响应，或对邮箱存在与不存在执行相同分支与耗时。

### [Medium] 会话指纹为空的旧 Token 永久绕过会话绑定校验

- 文件: `backend/internal/server/middleware/session_binding.go:61-73`
- 类别: 安全
- 复现/触发条件: `enforceSessionBinding` 在 `claims.BindingHash == ""` 时直接放行。任何在会话绑定功能启用前签发、或因上下文缺失未写入指纹的 Access Token，即使 IP/UA 完全变化也不会被撤销。
- 预期 vs 实际: 预期会话绑定开启后所有活跃会话都受指纹约束；实际空指纹 Token 可跨网络/设备使用直至过期。
- 影响: Token 泄露后可从任意网络环境使用，削弱会话绑定防护。
- 建议: 对空指纹 Token 强制在下次刷新时补齐绑定；或在中间件侧对空指纹且非兼容期 Token 拒绝。

### [Medium] “撤销所有会话”不撤销已签发的 Access JWT

- 文件: `backend/internal/service/auth_service.go:1895-1911`；`backend/internal/handler/auth_handler.go:751-767`
- 类别: 安全/设计
- 复现/触发条件: `RevokeAllUserTokens` 仅清理 Refresh Token 集合，不改变 Access Token 的签名材料（`users` 表无 `token_version` 列，密码未变时指纹不变）。已签发 Access Token 在过期前仍可用。
- 预期 vs 实际: 用户主动“登出所有设备”或安全事件响应时代理期望 Access Token 立即失效；实际仅 Refresh Token 失效。
- 影响: 被盗 Access Token 在其短有效期窗口内仍可访问。
- 建议: 文档明确短有效期假设；如需强撤销，引入持久化 token version/黑名单或缩短 Access Token TTL。

### [Medium] 2FA 临时登录会话在中间失败时不清除，可重复使用

- 文件: `backend/internal/handler/auth_handler.go:301-420`
- 类别: 并发/逻辑
- 复现/触发条件: `Login2FA` 在校验 TOTP 后执行用户查询、后端模式检查、Pending OAuth 绑定等步骤；只有全部成功后才 `DeleteLoginSession`。Pending OAuth 绑定失败或后续错误返回时，临时 Token 仍有效，可再次提交。
- 预期 vs 实际: 预期一次性临时会话在验证后尽快失效；实际失败路径保留会话。
- 影响: 增加重放/暴力尝试窗口；在 Pending OAuth 流程中可能放大绑定失败重试。
- 建议: TOTP 验证通过后尽快删除或标记会话已消费；失败时也执行删除。

### [Medium] `BackendModeAuthGuard` 以子串匹配放行所有 pending OAuth 路径

- 文件: `backend/internal/server/middleware/backend_mode_guard.go:73`
- 类别: 权限
- 复现/触发条件: `backendModeAllowsAuthPath` 对包含 `/auth/oauth/pending/` 的任意路径直接放行，再依赖各 Handler 内部检查。若未来新增 pending 路径且遗漏内部 `ensureBackendModeAllows*` 检查，将直接绕过后端模式限制。
- 预期 vs 实际: 预期中间件层最小放行集合；实际基于宽泛子串匹配。
- 影响: 防御纵深不足，新增接口时易引入权限绕过。
- 建议: 改为显式路径白名单，或在中间件层统一校验。

### [Low] 邮箱验证开启时注册跳过验证码二次校验

- 文件: `backend/internal/service/auth_service.go:396-405`
- 类别: 安全
- 复现/触发条件: `VerifyCaptchaForRegister` 在 `IsEmailVerifyEnabled` 且 `verifyCode != ""` 时直接返回 nil，不再校验 Turnstile/腾讯/阿里云验证码。
- 预期 vs 实际: 预期注册提交仍需满足当前启用的人机验证；实际仅依赖发码阶段的验证码。
- 影响: 若验证码发送与注册提交分离，人机验证强度下降；需确保发码阶段验证码不可复用/不可绕过。
- 建议: 在注册提交阶段至少校验一次性验证码票据，或明确该设计并加强发码阶段防护。

### [Low] 密码重置先消费 Token 再检查用户存在

- 文件: `backend/internal/service/auth_service.go:1625-1638`
- 类别: 逻辑
- 复现/触发条件: `ResetPassword` 先 `ConsumePasswordResetToken`，再 `GetByEmail`；若用户已被删除，Token 已被消费且返回 `ErrInvalidResetToken`，用户无法重试。
- 预期 vs 实际: 预期无效用户不消耗有效 Token；实际消耗后再报错。
- 影响: 低概率下的用户体验与重放防护差异；不构成直接安全漏洞。
- 建议: 先查询用户与状态，确认有效后再消费 Token。

### [Low] `isRequestHTTPS` 信任客户端 `X-Forwarded-Proto` 头

- 文件: `backend/internal/handler/auth_linuxdo_oauth.go:1012-1018`
- 类别: 安全/配置
- 复现/触发条件: 当服务未处于可信反代之后时，攻击者可伪造 `X-Forwarded-Proto: https`，影响 `Secure` Cookie 标记与 HTTPS 判定。
- 预期 vs 实际: 预期仅信任可信代理链；实际直接读取请求头。
- 影响: Cookie 安全属性可能被降级或提升，影响会话保护。
- 建议: 仅在可信代理配置下信任该头，或使用 Gin 的 TrustedProxies。

### [Low] 单 Access Token 生成函数接受空会话上下文时签发无绑定 Token

- 文件: `backend/internal/service/auth_service.go:1408-1417`；`backend/internal/service/auth_service.go:1420-1451`
- 类别: 安全
- 复现/触发条件: `GenerateToken`/`generateAccessToken` 在会话绑定上下文缺失时生成 `BindingHash` 为空的 Access Token，该 Token 将永久绕过会话绑定（见前述发现）。
- 预期 vs 实际: 预期所有新签发 Token 都携带会话指纹；实际上下文缺失时无指纹。
- 影响: 与空指纹绕过叠加，降低会话绑定有效性。
- 建议: 在签发路径强制要求会话绑定上下文，缺失时失败而非静默签发无绑定 Token。

### [Info] 管理员 API Key 认证固定映射到首个管理员

- 文件: `backend/internal/server/middleware/admin_auth.go:121-154`
- 类别: 可用性/审计
- 复现/触发条件: `validateAdminAPIKey` 始终使用 `userService.GetFirstAdmin` 作为认证主体，不区分使用 API Key 的管理员。
- 预期 vs 实际: 预期机器凭证可映射到具体管理员或独立主体；实际所有 API Key 调用都以首个管理员身份审计。
- 影响: 审计追踪粒度不足。
- 建议: 记录 API Key 使用主体或支持多管理员 API Key。

## 测试执行结果

在 `sub2api-so/backend` 下通过 Docker `golang:1.27.2` 执行：

```bash
go test -tags=unit -run 'Auth|User|Session|OAuth|Captcha|JWT' ./internal/...
```

认证相关子包测试整体通过（`internal/service` 中 `TestAuth*`、`TestAuthService*` 等通过）。全量 `./internal/...` 中 `internal/service` 因环境相关用例（Gemini OAuth 驱动存储等）失败，与本次认证范围无直接关联。`go vet` 未单独执行。

## 结论

认证模块整体结构完整，密码重置、Refresh Token 轮转、邀请码并发占用等有较好防护。主要风险集中在：额外有效 Access Token 签发、邮箱存在性枚举、会话绑定空指纹绕过、撤销会话不覆盖 Access Token、2FA 临时会话失败路径不清除、后端模式中间件放行过宽。建议优先处理 Medium 级别项。