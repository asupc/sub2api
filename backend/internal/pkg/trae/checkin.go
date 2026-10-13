package trae

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// CheckinStatusResult 签到状态预检结果（POST checkin_credits/status，体 `{}`）。
type CheckinStatusResult struct {
	// HTTPStatus 原始 HTTP 状态码（网络失败为 0）。
	HTTPStatus int `json:"http_status"`
	// Code 业务码（0 = 成功；nil 形态用 CodeValid 区分）。
	Code    int64 `json:"code"`
	HasCode bool  `json:"has_code"`
	// CheckedIn 今日是否已签到。
	CheckedIn bool `json:"checked_in"`
	// Credits 签到可获得的积分额度。
	Credits int64 `json:"credits"`
	// Message 上游文案。
	Message string `json:"message"`
}

// OK 业务是否成功（code == 0）。
func (r *CheckinStatusResult) OK() bool {
	return r.HasCode && r.Code == 0
}

// ClaimResult 签到 claim 结果（POST checkin_credits/claim，体 `{}`）。
type ClaimResult struct {
	// HTTPStatus 原始 HTTP 状态码（网络失败为 0，可据此分类错误 / 判断可重试）。
	HTTPStatus int `json:"http_status"`
	// Code 业务码；HasCode=false 且 HTTPStatus=0 表示网络层失败（可重试）。
	Code    int64 `json:"code"`
	HasCode bool  `json:"has_code"`
	// Message 上游文案。
	Message string `json:"message"`
}

// OK 业务是否成功（code == 0）。
func (r *ClaimResult) OK() bool {
	return r.HasCode && r.Code == 0
}

// CheckinStatus 调状态接口做预检：判断今日是否已签到、签到可得额度。
// 预检失败不阻断 claim——预检只是优化，上游状态接口偶发不可用时直接 claim 仍可能成功。
func CheckinStatus(ctx context.Context, httpClient *http.Client, variant Variant,
	jwtValue string, device DeviceIdentity) (*CheckinStatusResult, error) {

	status, body, err := postAccountBaseParsed(ctx, httpClient, variant, CheckinStatusPath, jwtValue, device)
	if err != nil {
		return nil, err
	}
	result := &CheckinStatusResult{HTTPStatus: status}
	if code, ok := body["code"].(float64); ok {
		result.Code, result.HasCode = int64(code), true
	}
	result.CheckedIn, _ = body["checked_in"].(bool)
	if credits, ok := body["credits"].(float64); ok {
		result.Credits = int64(credits)
	}
	result.Message, _ = body["message"].(string)
	if !result.OK() && result.Message == "" {
		result.Message = fmt.Sprintf("HTTP %d", status)
	}
	return result, nil
}

// CheckinClaim 调签到接口执行签到。
//
// 仅网络层异常值得重试（业务失败重试是有害的：「套餐额度限制」「会话失效」
// 这类结果不会因重试而改变，反而成倍增加上游请求、触发风控）——
// 调用方可通过 HTTPStatus == 0 && !HasCode 判定网络层失败。
func CheckinClaim(ctx context.Context, httpClient *http.Client, variant Variant,
	jwtValue string, device DeviceIdentity) (*ClaimResult, error) {

	status, body := postAccountBase(ctx, httpClient, variant, CheckinPath, jwtValue, device)
	if status == 0 {
		// 网络层失败：无业务码，调用方据此判定「可重试」。
		return &ClaimResult{HTTPStatus: 0, Message: body}, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		snippet := body
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		// 非 JSON 响应视为业务失败（避免被当成网络错误反复重试）。
		return &ClaimResult{
			HTTPStatus: status,
			Message:    fmt.Sprintf("HTTP %d: 非 JSON 响应: %s", status, snippet),
		}, nil
	}
	result := &ClaimResult{HTTPStatus: status}
	if code, ok := parsed["code"].(float64); ok {
		result.Code, result.HasCode = int64(code), true
	}
	result.Message, _ = parsed["message"].(string)
	if result.Message == "" {
		result.Message = fmt.Sprintf("HTTP %d", status)
	}
	return result, nil
}
