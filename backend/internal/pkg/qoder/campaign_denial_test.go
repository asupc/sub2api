//go:build unit

package qoder

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func claimStatusHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func dailyCampaignList() http.HandlerFunc {
	return jsonHandler(`{"showCampaign":true,"campaigns":[
		{"campaignId":"c1","campaignKey":"daily","actionType":"CLAIM_BENEFIT",
		 "claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100},
		 "placements":[{"content":{"zh":{"title":"每日领取"}}}]}]}`)
}

func runCampaignCheckin(t *testing.T, routes map[string]http.HandlerFunc) *CampaignCheckinOutcome {
	t.Helper()
	client, _ := campaignServer(t, routes)
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// 裸 409 不能猜成「今日已领取」（qoder2api-hub 958f14f / issue #167）。
func TestCampaignCheckinAmbiguous409IsError(t *testing.T) {
	out := runCampaignCheckin(t, map[string]http.HandlerFunc{
		PathCampaigns:                        dailyCampaignList(),
		"/sash/api/v1/me/campaigns/c1/claim": claimStatusHandler(http.StatusConflict, `{"message":"not confirmed"}`),
	})
	if out.OK || len(out.Already) != 0 || len(out.Claimed) != 0 {
		t.Fatalf("ambiguous 409 must not look claimed: %+v", out)
	}
	if len(out.Errors) != 1 || !strings.Contains(out.Message, "未成功") {
		t.Fatalf("errors/message: %+v", out)
	}
}

// 错误体里明确 ALREADY_CLAIMED 才按幂等成功。
func TestCampaignCheckinExplicitReplayOn409(t *testing.T) {
	out := runCampaignCheckin(t, map[string]http.HandlerFunc{
		PathCampaigns: dailyCampaignList(),
		"/sash/api/v1/me/campaigns/c1/claim": claimStatusHandler(http.StatusConflict,
			`{"errorCode":"ALREADY_CLAIMED"}`),
	})
	if !out.OK || len(out.Already) != 1 {
		t.Fatalf("explicit replay: %+v", out)
	}
}

// 错误体里的同人标记按 BLOCKED 记冷却。
func TestCampaignCheckinSamePersonOnError(t *testing.T) {
	out := runCampaignCheckin(t, map[string]http.HandlerFunc{
		PathCampaigns: dailyCampaignList(),
		"/sash/api/v1/me/campaigns/c1/claim": claimStatusHandler(http.StatusForbidden,
			`{"failureCode":"SAME_PERSON_ALREADY_CLAIMED"}`),
	})
	if len(out.Blocked) != 1 || len(out.BlockedMarks) != 1 {
		t.Fatalf("same person: %+v", out)
	}
	if exp := out.BlockedMarks["c1"]; exp < time.Now().Unix()+5*3600 {
		t.Fatalf("blocked marks: %+v", out.BlockedMarks)
	}
}

// 已知失败码（风控）归错误并带码，不能伪装成「新人任务」。
func TestCampaignCheckinRiskFailureIsError(t *testing.T) {
	out := runCampaignCheckin(t, map[string]http.HandlerFunc{
		PathCampaigns: dailyCampaignList(),
		"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(
			`{"status":"BLOCKED","failureCode":"RISK_BLOCKED"}`),
	})
	if len(out.Errors) != 1 || len(out.Locked) != 0 {
		t.Fatalf("risk failure: %+v", out)
	}
	if !strings.Contains(out.Message, "风控") || strings.Contains(out.Message, "新人任务") {
		t.Fatalf("message: %q", out.Message)
	}
}

// 活动列表里不可领取原因是风控/未开始时归错误。
func TestCampaignCheckinUnavailableRiskReasonIsError(t *testing.T) {
	out := runCampaignCheckin(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"r1","campaignKey":"risk","actionType":"CLAIM_BENEFIT",
			 "claimStatus":"NOT_CLAIMABLE","unavailableReason":"RISK_BLOCKED",
			 "benefit":{"kind":"CREDITS","amount":100}}]}`),
	})
	if len(out.Errors) != 1 || len(out.Locked) != 0 || len(out.Pending) != 0 {
		t.Fatalf("unavailable risk: %+v", out)
	}
	if !strings.Contains(out.Message, "RISK_BLOCKED") {
		t.Fatalf("message: %q", out.Message)
	}
}

// 只有 status=BLOCKED、没有业务码：上游未确认，归错误而不是同人拦截。
func TestCampaignCheckinGenericBlockedStatusIsError(t *testing.T) {
	out := runCampaignCheckin(t, map[string]http.HandlerFunc{
		PathCampaigns:                        dailyCampaignList(),
		"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(`{"status":"BLOCKED"}`),
	})
	if len(out.Blocked) != 0 || len(out.Errors) != 1 {
		t.Fatalf("generic blocked: %+v", out)
	}
	if !strings.Contains(out.Message, "上游未确认领取") {
		t.Fatalf("message: %q", out.Message)
	}
}

// 名额发完 / 成就未完成仍按 pending / locked（幂等正常出口）。
func TestCampaignCheckinKnownWaitingCodesStayWaiting(t *testing.T) {
	for _, tc := range []struct {
		code string
		want func(*CampaignCheckinOutcome) bool
	}{
		{"REDEMPTION_CODE_OUT_OF_STOCK", func(o *CampaignCheckinOutcome) bool { return len(o.Pending) == 1 }},
		{"ACHIEVEMENT_NOT_COMPLETED", func(o *CampaignCheckinOutcome) bool { return len(o.Locked) == 1 }},
	} {
		t.Run(tc.code, func(t *testing.T) {
			out := runCampaignCheckin(t, map[string]http.HandlerFunc{
				PathCampaigns: dailyCampaignList(),
				"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(
					`{"status":"NOT_ELIGIBLE","failureCode":"` + tc.code + `"}`),
			})
			if !out.OK || len(out.Errors) != 0 || !tc.want(out) {
				t.Fatalf("%s: %+v", tc.code, out)
			}
		})
	}
}
