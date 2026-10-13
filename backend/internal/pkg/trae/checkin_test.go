//go:build unit

package trae

import (
	"context"
	"net/http"
	"testing"
)

func TestCheckinStatusRequestAndParse(t *testing.T) {
	server, captures := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"ok","checked_in":true,"credits":50}`))
	})
	client := redirectClient(t, server)

	result, err := CheckinStatus(context.Background(), client, VariantSoloCN, "jwt-1", DeviceIdentity{DeviceID: "d"})
	if err != nil {
		t.Fatal(err)
	}
	capture := (*captures)[0]
	if capture.method != http.MethodPost || capture.path != "/trae/api/v2/ug/checkin_credits/status" {
		t.Errorf("请求 = %s %s", capture.method, capture.path)
	}
	if capture.body != "{}" {
		t.Errorf("body = %q, want {}", capture.body)
	}
	if !result.OK() || !result.CheckedIn || result.Credits != 50 || result.Message != "ok" {
		t.Errorf("result = %+v", result)
	}
}

func TestCheckinStatusBusinessFailure(t *testing.T) {
	server, _ := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":1005,"message":"plan limit"}`))
	})
	client := redirectClient(t, server)
	result, err := CheckinStatus(context.Background(), client, VariantTraeCN, "jwt", DeviceIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() {
		t.Error("code=1005 不应成功")
	}
	if result.Message != "plan limit" {
		t.Errorf("message = %q", result.Message)
	}
}

func TestCheckinClaimRequestAndParse(t *testing.T) {
	server, captures := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"message":"签到成功"}`))
	})
	client := redirectClient(t, server)

	result, err := CheckinClaim(context.Background(), client, VariantSoloGlobal, "jwt-1", DeviceIdentity{DeviceID: "d"})
	if err != nil {
		t.Fatal(err)
	}
	capture := (*captures)[0]
	if capture.method != http.MethodPost || capture.path != "/trae/api/v2/ug/checkin_credits/claim" {
		t.Errorf("请求 = %s %s", capture.method, capture.path)
	}
	if capture.body != "{}" {
		t.Errorf("body = %q, want {}", capture.body)
	}
	if got := capture.header.Get("Authorization"); got != "Cloud-IDE-JWT jwt-1" {
		t.Errorf("Authorization = %q", got)
	}
	if !result.OK() || result.Message != "签到成功" {
		t.Errorf("result = %+v", result)
	}
}

// TestCheckinClaimFailureClassification claim 失败 → 错误分类 → 冷却时长。
func TestCheckinClaimFailureClassification(t *testing.T) {
	server, _ := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"message":"rate limited"}`))
	})
	client := redirectClient(t, server)
	result, err := CheckinClaim(context.Background(), client, VariantSoloCN, "jwt", DeviceIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() {
		t.Error("429 不应成功")
	}
	if result.HTTPStatus != http.StatusTooManyRequests {
		t.Errorf("http_status = %d", result.HTTPStatus)
	}
	errorType, cooldown := ClassifyError(result.HTTPStatus, &result.Code)
	if errorType != "SoftRate" || cooldown != 60 {
		t.Errorf("classify = (%s, %d)", errorType, cooldown)
	}
}

// TestCheckinClaimNonJSONBody 非 JSON 响应视为业务失败（HTTPStatus != 0），
// 避免被当成网络错误反复重试。
func TestCheckinClaimNonJSONBody(t *testing.T) {
	server, _ := serveAccountBase(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>bad gateway</html>"))
	})
	client := redirectClient(t, server)
	result, err := CheckinClaim(context.Background(), client, VariantSoloCN, "jwt", DeviceIdentity{})
	if err != nil {
		t.Fatal(err)
	}
	if result.OK() || result.HTTPStatus == 0 || result.HasCode {
		t.Errorf("非 JSON 响应应为业务失败: %+v", result)
	}
	if result.Message == "" {
		t.Error("应带可读错误文案")
	}
}
