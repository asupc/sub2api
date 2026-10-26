//go:build unit

package service

// workbuddy_search_service_test.go 单元测试：搜索轮转的换号判定与客户端出口映射。
// 上游客户端契约测试在 pkg/workbuddy/search_test.go，协议壳测试在
// handler/gateway_workbuddy_search_test.go。

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
)

func TestWorkbuddySearchShouldRotate(t *testing.T) {
	fo := &UpstreamFailoverError{StatusCode: 429}
	cases := []struct {
		name     string
		kind     workbuddy.ErrorKind
		handled  bool
		failover *UpstreamFailoverError
		want     bool
	}{
		// 已接管 + 换号（账号级处置完成：积分耗尽/限流/SessionDead/WAF/token 失效）。
		{"hard_credit", workbuddy.KindHardCredit, true, fo, true},
		{"soft_rate", workbuddy.KindSoftRate, true, fo, true},
		{"session_dead", workbuddy.KindSessionDead, true, fo, true},
		{"waf_block", workbuddy.KindWafBlock, true, fo, true},
		{"account_restricted", workbuddy.KindAccountFault, true, fo, true},
		{"channel_refused", workbuddy.KindChannelRefused, true, fo, true},
		// 已接管 + 不换号（原文透传：11102 / 14017 / 6021 fail-fast）。
		{"model_blocked", workbuddy.KindModelBlocked, true, nil, false},
		{"account_fault_passthrough", workbuddy.KindAccountFault, true, nil, false},
		{"upstream_busy", workbuddy.KindUpstreamBusy, true, nil, false},
		// 未接管：请求级拒绝不轮转（换号只会重放同一拒绝）。
		{"content_blocked", workbuddy.KindContentBlocked, false, nil, false},
		{"bad_params", workbuddy.KindBadParams, false, nil, false},
		{"prompt_too_long", workbuddy.KindPromptTooLong, false, nil, false},
		{"image_invalid", workbuddy.KindImageInvalid, false, nil, false},
		// 未接管的其余错误（5xx/未知）→ 换号。
		{"server", workbuddy.KindServer, false, nil, true},
		{"not_found", workbuddy.KindNotFound, false, nil, true},
		{"client_other", workbuddy.KindClient, false, nil, true},
	}
	for _, tc := range cases {
		if got := workbuddySearchShouldRotate(tc.kind, tc.handled, tc.failover); got != tc.want {
			t.Errorf("%s: got=%v want=%v", tc.name, got, tc.want)
		}
	}
}

func TestWorkbuddySearchClientError(t *testing.T) {
	cases := []struct {
		name       string
		ue         *workbuddy.Error
		wantStatus int
	}{
		{"限流映射 429", &workbuddy.Error{Kind: workbuddy.KindSoftRate, Status: 429, Msg: "slow"}, http.StatusTooManyRequests},
		{"限流 402 变体也映射 429", &workbuddy.Error{Kind: workbuddy.KindSoftRate, Status: 402, Msg: "slow"}, http.StatusTooManyRequests},
		{"渠道满载映射 503", &workbuddy.Error{Kind: workbuddy.KindUpstreamBusy, Status: 429, Msg: "queue full"}, http.StatusServiceUnavailable},
		{"4xx 原文透传", &workbuddy.Error{Kind: workbuddy.KindClient, Status: 400, Msg: "bad"}, http.StatusBadRequest},
		{"401 透传", &workbuddy.Error{Kind: workbuddy.KindClient, Status: 401, Msg: "unauth"}, http.StatusUnauthorized},
		{"5xx 映射 503", &workbuddy.Error{Kind: workbuddy.KindServer, Status: 502, Msg: "oops"}, http.StatusServiceUnavailable},
		{"空 Msg 兜底文案", &workbuddy.Error{Kind: workbuddy.KindServer, Status: 500, Msg: ""}, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		got := workbuddySearchClientError(tc.ue)
		if got.Status != tc.wantStatus {
			t.Errorf("%s: status=%d want %d", tc.name, got.Status, tc.wantStatus)
		}
		if got.Msg == "" {
			t.Errorf("%s: Msg 不应为空", tc.name)
		}
	}
}
