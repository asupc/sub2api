package service

// workbuddy_search_service.go 二开新增：WorkBuddy 联网搜索的轮转内核。
//
// 移植自 workbuddy2api-panel internal/server/search.go（runSearch，commit 4f31005），
// 但账号池语义换成 sub2api 的调度体系：
//   - 选号：SelectAccountWithSchedulerForCapability + platformOverride=workbuddy
//     （与 CC 漏斗 workbuddy chat 同一机制，见 openai_gateway_handler 的
//     requestPlatforms 传参），失败账号进 excludedIDs 换号；
//   - 槽位：与 chat 共用账号并发槽（搜索也是真实上游调用，不占名额会让搜索
//     流量绕过 MaxConcurrency 限流）；
//   - token 预刷新：chat 请求路径没有预刷新（只在 401/403 触发
//     triggerWorkbuddyRequestPathRefresh 强制刷新，由后台 TokenRefreshService
//     保活），搜索同样不预刷新，刷新触发点在共享的错误处置里；
//   - 错误处置：applyWorkbuddyUpstreamErrorKind 复用 chat 的 handleWorkbuddyUpstreamError
//     语义（积分耗尽/限流/SessionDead/WAF/token 失效各自冷却或罚号），失败换号重试；
//   - 全池失败：按 WorkbuddySearchError 出口返回，由 handler 渲染成协议错误形态。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	"github.com/gin-gonic/gin"
)

// workbuddySearchMaxAttempts 单次搜索最多尝试次数（含首发）。对齐源项目
// MaxRotate 默认 3：搜索是同步请求（pkg 侧 20s 超时），3 次已是分钟级延迟上限，
// 再多只会把客户端拖死而不是提高成功率。
const workbuddySearchMaxAttempts = 3

// workbuddySearchClientTimeout 出站 http.Client 总超时。必须大于 pkg 侧
// searchTimeout（20s）并留余量：若 client 超时先触发，上游挂起会被归为传输层
// 故障（不罚号），换号循环会拿同一类超时空转烧完所有账号。
const workbuddySearchClientTimeout = 30 * time.Second

// WorkbuddySearchError 搜索轮转全部失败后的出口状态。
// Status/Msg 供 handler 映射成 Anthropic 错误形态（rate_limit_error/api_error）。
type WorkbuddySearchError struct {
	Status int
	Msg    string
}

// RunWorkbuddySearch 带账号轮转执行一次 WorkBuddy 联网搜索。
//
// 轮转语义与源项目 runSearch 一致：选号 → 占槽 → 上游调用 → 按错误类别处置
// （冷却/罚号/强制刷新）→ 换号重试；换号重试属于同一请求，复用同一组会话头
// （meta 在循环外构造）。
//
// 返回 (outcome, nil) 成功；(nil, fail) 时 fail 携带出口状态码与信息。
// 传输层故障（非上游业务返回）同样换号，但不按业务错误罚号。
func (s *OpenAIGatewayService) RunWorkbuddySearch(
	ctx context.Context,
	c *gin.Context,
	groupID *int64,
	query string,
	maxResults int,
	meta workbuddy.ChatMeta,
) (*workbuddy.SearchOutcome, *WorkbuddySearchError) {
	failed := make(map[int64]struct{}, workbuddySearchMaxAttempts)
	var lastFail *WorkbuddySearchError

	for attempt := 0; attempt < workbuddySearchMaxAttempts; attempt++ {
		selection, _, err := s.SelectAccountWithSchedulerForCapability(
			ctx,
			groupID,
			"", // previousResponseID：搜索无续链
			"", // sessionHash：搜索不做粘性（每次独立）
			"", // requestedModel：搜索没有模型维度
			failed,
			OpenAIUpstreamTransportAny,
			"",    // requiredCapability：搜索端点无能力要求
			false, // requireCompact
			false, // previousResponseCanMove
			false, // useUpstreamTokenCost：搜索不计 token 成本，不预取上游费率
			PlatformWorkbuddy,
		)
		if err != nil || selection == nil || selection.Account == nil {
			break // 池内已无候选（含首发失败）
		}
		account := selection.Account
		failed[account.ID] = struct{}{} // 同一请求内同一账号不重复尝试

		release := selection.ReleaseFunc
		if !selection.Acquired || release == nil {
			acq, aerr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
			if aerr != nil || acq == nil || !acq.Acquired {
				continue // 槽位抢不到：换下一个号（同步搜索不排队等待）
			}
			release = acq.ReleaseFunc
		}

		out, fail, rotate := s.executeWorkbuddySearchOnce(ctx, c, account, query, maxResults, meta)
		if release != nil {
			release()
		}
		if fail == nil {
			return out, nil
		}
		lastFail = fail
		if !rotate {
			break
		}
	}

	if lastFail != nil {
		return nil, lastFail
	}
	// 选不到号 / 全部槽位不可用：与源项目同口径的池子空出口。
	return nil, &WorkbuddySearchError{
		Status: http.StatusServiceUnavailable,
		Msg:    "all workbuddy accounts are temporarily unavailable, please retry later",
	}
}

// executeWorkbuddySearchOnce 在指定账号上执行一次搜索。
//
// 返回 (outcome, fail, rotate)：
//   - 成功 (out, nil, _)；
//   - 失败 (nil, fail, rotate)——fail.Status 为**客户端视角**的出口状态码
//     （4xx 透传 / 限流映射 429 / 其余 503），账号处置已在本函数内完成；
//     rotate 表示是否应换号重试。
//
// 换号判定复用 handleWorkbuddyUpstreamError 的接管语义：
//   - handled && failoverErr == nil（11102 模型封禁 / 11140 UA 风控）→ 不换号，
//     按上游原文透传；
//   - handled && failoverErr != nil（积分耗尽/限流/SessionDead/WAF/token 失效）
//     → 账号已冷却或罚号，换号；
//   - 未接管的请求级错误（内容拦截/参数错/prompt 过长等）→ 不换号（换号只会
//     对所有账号重放同一拒绝）；
//   - 未接管的其余错误（5xx/未知）→ 换号（对齐通用 failover 链语义）。
func (s *OpenAIGatewayService) executeWorkbuddySearchOnce(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	query string,
	maxResults int,
	meta workbuddy.ChatMeta,
) (*workbuddy.SearchOutcome, *WorkbuddySearchError, bool) {
	cred, err := parseWorkbuddyCredentials(account)
	if err != nil {
		// 凭证不完整是账号故障：置 error 供运维处理，本次换号。
		slog.Warn("workbuddy search credentials invalid", "account_id", account.ID, "error", err)
		if s.accountRepo != nil {
			_ = s.accountRepo.SetError(ctx, account.ID, "wb_search_credentials_invalid: "+err.Error())
		}
		return nil, &WorkbuddySearchError{
			Status: http.StatusServiceUnavailable,
			Msg:    "workbuddy account credentials are invalid",
		}, true
	}

	client := newWorkbuddyUpstreamClient(upstreamModelsProxyURL(account), workbuddySearchClientTimeout)
	out, serr := client.Search(ctx, cred, query, maxResults, meta)
	if serr == nil {
		return out, nil, false
	}

	var ue *workbuddy.Error
	if !errors.As(serr, &ue) {
		// 传输层故障（连接失败/读体中断/本地序列化错）：不按业务错误罚号，
		// 只换号重试；出口统一 503，不把本地网络栈细节透传给客户端。
		slog.Warn("workbuddy search transport failure", "account_id", account.ID, "error", serr)
		return nil, &WorkbuddySearchError{
			Status: http.StatusServiceUnavailable,
			Msg:    "workbuddy search upstream temporarily unreachable",
		}, true
	}

	// 与 chat 共享同一套账号状态映射（积分耗尽/限流/SessionDead/WAF/401 触发刷新）。
	// upstreamModel 传空：搜索没有模型维度，不写模型级冷却。
	resp := &http.Response{StatusCode: ue.Status, Header: http.Header{}}
	if ue.RetryAfter > 0 {
		resp.Header.Set("Retry-After", fmt.Sprintf("%d", int(ue.RetryAfter.Seconds())))
	}
	handled, failoverErr := s.applyWorkbuddyUpstreamErrorKind(
		ctx, c, account, ue.Kind, resp, []byte(ue.RawBody), ue.Msg, "",
	)
	rotate := workbuddySearchShouldRotate(ue.Kind, handled, failoverErr)
	return nil, workbuddySearchClientError(ue), rotate
}

// workbuddySearchShouldRotate 判定失败后是否换号重试（语义见
// executeWorkbuddySearchOnce 注释）。
func workbuddySearchShouldRotate(kind workbuddy.ErrorKind, handled bool, failoverErr *UpstreamFailoverError) bool {
	if handled {
		// 已接管：failoverErr != nil 表示账号级处置完成（冷却/罚号）须换号；
		// nil 表示不换号、原文透传（11102 / 11140）。
		return failoverErr != nil
	}
	// 未接管：请求级拒绝对每个账号都一样，换号只会放大上游压力。
	switch kind {
	case workbuddy.KindContentBlocked, workbuddy.KindBadParams,
		workbuddy.KindPromptTooLong, workbuddy.KindImageInvalid:
		return false
	default:
		return true
	}
}

// workbuddySearchClientError 把上游 *workbuddy.Error 映射为客户端视角的出口状态：
//   - 限流语义 → 429（无论上游用 429 还是业务码表达）；
//   - 6021 渠道容量满载（KindUpstreamBusy）→ 503（chat 路径同样合成 503；
//     429 会被客户端误当自身 key 限流）；
//   - 4xx 原文透传状态码；
//   - 其余（5xx）→ 503，提示稍后重试。
//
// 与源项目 runSearch 的末端出口同口径（限流映射 429，4xx 透传，其余 503）。
func workbuddySearchClientError(ue *workbuddy.Error) *WorkbuddySearchError {
	msg := ue.Msg
	if msg == "" {
		msg = "workbuddy search failed"
	}
	switch {
	case ue.Kind == workbuddy.KindSoftRate:
		return &WorkbuddySearchError{Status: http.StatusTooManyRequests, Msg: msg}
	case ue.Kind == workbuddy.KindUpstreamBusy:
		return &WorkbuddySearchError{Status: http.StatusServiceUnavailable, Msg: msg}
	case ue.Status >= 400 && ue.Status < 500:
		return &WorkbuddySearchError{Status: ue.Status, Msg: msg}
	default:
		return &WorkbuddySearchError{Status: http.StatusServiceUnavailable, Msg: msg}
	}
}
