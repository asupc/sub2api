//go:build unit

package qoder

import (
	"strings"
	"testing"
)

// 常量逐字对照 qoder_accounts.py L39-80 REALM_CONFIGS。
func TestRealmConfigsGolden(t *testing.T) {
	cn := RealmConfigs["cn"]
	if cn.OpenAPI != "https://openapi.qoder.com.cn" || cn.Gateway != "https://gateway.qoder.com.cn" {
		t.Fatalf("cn endpoints: %+v", cn)
	}
	if cn.ClientID != "1c5e33e1-364d-4ce6-b02c-acaa81274a5c" {
		t.Fatalf("cn client id: %s", cn.ClientID)
	}
	if cn.RedirectURI != "qoder-work-cn://" || cn.Domain != "qoder.com.cn" {
		t.Fatalf("cn redirect/domain: %+v", cn)
	}
	if cn.UA != "QoderWork/1.1.75" {
		t.Fatalf("cn ua: %s", cn.UA)
	}
	if !cn.SendClientID || !cn.SendRedirectURI || !cn.NonceDashed {
		t.Fatal("cn oauth flags: client_id+redirect_uri+dashed nonce")
	}

	intl := RealmConfigs["intl"]
	if intl.OpenAPI != "https://openapi.qoder.sh" || intl.Gateway != "https://api3.qoder.sh" {
		t.Fatalf("intl endpoints: %+v", intl)
	}
	if len(intl.GatewayFallbacks) != 2 ||
		intl.GatewayFallbacks[0] != "https://api2.qoder.sh" ||
		intl.GatewayFallbacks[1] != "https://api1.qoder.sh" {
		t.Fatalf("intl fallbacks: %+v", intl.GatewayFallbacks)
	}
	if intl.ClientID != "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb" {
		t.Fatalf("intl client id: %s", intl.ClientID)
	}
	if intl.RedirectURI != "qoder://aicoding.aicoding-agent/login-success" {
		t.Fatalf("intl redirect uri: %s", intl.RedirectURI)
	}
	if intl.UA != "Qoder/1.1.75" || intl.Domain != "qoder.com" {
		t.Fatalf("intl ua/domain: %+v", intl)
	}
	if !intl.SendClientID || intl.SendRedirectURI || intl.NonceDashed {
		t.Fatal("intl oauth flags: client_id, no redirect_uri, hex nonce")
	}
}

func TestRealmFallbacks(t *testing.T) {
	if Realm("unknown") != RealmConfigs["cn"] {
		t.Fatal("unknown realm must fall back to cn")
	}
	if !ValidRealm("intl") || ValidRealm("us") {
		t.Fatal("ValidRealm")
	}
	if got := GatewayCandidates("intl"); len(got) != 3 || got[0] != "https://api3.qoder.sh" {
		t.Fatalf("intl candidates: %+v", got)
	}
	if got := GatewayCandidates("cn"); len(got) != 1 {
		t.Fatalf("cn candidates: %+v", got)
	}
}

func TestDetectRealmFromDomain(t *testing.T) {
	cases := map[string]string{
		"qoder.com":    "intl",
		"QODER.SH":     "intl",
		"qoder.com.cn": "cn",
		"example.com":  "cn",
		"":             "cn",
	}
	for domain, want := range cases {
		if got := DetectRealmFromDomain(domain); got != want {
			t.Fatalf("DetectRealmFromDomain(%q) = %q, want %q", domain, got, want)
		}
	}
}

func TestSessionDeadMarkers(t *testing.T) {
	if !SessionDead(`{"code":"TOKEN_EXPIRE"}`) {
		t.Fatal("TOKEN_EXPIRE must be dead")
	}
	if !SessionDead("error 12153 queued") {
		t.Fatal("12153 must be dead")
	}
	if !SessionDead("Offline user session not found") {
		t.Fatal("offline marker must be dead")
	}
	if SessionDead("some transient error") {
		t.Fatal("ordinary error must not be dead")
	}
}

func TestEndpointConstants(t *testing.T) {
	checks := map[string]string{
		PathDevicePoll:     "/api/v1/deviceToken/poll",
		PathDeviceRefresh:  "/api/v1/deviceToken/refresh",
		PathJobExchange:    "/api/v1/jobToken/exchange",
		PathJobRefresh:     "/api/v1/jobToken/refresh",
		PathUserinfo:       "/api/v1/userinfo",
		PathQuotaUsage:     "/api/v2/quota/usage",
		PathUserPlan:       "/api/v2/user/plan",
		PathCheckinStatus:  "/sash/api/v1/me/daily-check-in/status",
		PathCheckinClaim:   "/sash/api/v1/me/daily-check-in/claim",
		PathProEligibility: "/sash/api/v1/me/pro-upgrade/eligibility",
		PathProClaim:       "/sash/api/v1/me/pro-upgrade/claim",
		PathCampaigns:      "/sash/api/v1/me/campaigns",
	}
	for got, want := range checks {
		if got != want {
			t.Fatalf("endpoint drift: %q != %q", got, want)
		}
	}
	if !strings.HasPrefix(ChatPath, "/algo/api/v2/service/pro/sse/agent_chat_generation?") {
		t.Fatalf("chat path: %s", ChatPath)
	}
	if !strings.Contains(ChatPath, "Encode=1") || !strings.Contains(ChatPath, "AgentId=agent_common") {
		t.Fatalf("chat path params: %s", ChatPath)
	}
	if ModelsPath != "/algo/api/v2/model/list?Encode=1" {
		t.Fatalf("models path: %s", ModelsPath)
	}
	if CampaignRewardPath("c1") != "/sash/api/v1/me/campaigns/c1/reward" ||
		CampaignClaimPath("c1") != "/sash/api/v1/me/campaigns/c1/claim" {
		t.Fatal("campaign paths")
	}
	if ClientUA != "Go-http-client/2.0" || DesktopClientType != "10" ||
		DesktopClientVersion != "0.4.8" || LoginTTLSeconds != 600 {
		t.Fatal("client constants drift")
	}
	if cosyMachineOS() == "" || machineHostname() == "" {
		t.Fatal("machine os/hostname resolver must be non-empty on this host")
	}
}

// OpenAPICandidates 候选顺序与 intl 专属 center.qoder.sh（源 openapi_candidates）。
func TestOpenAPICandidates(t *testing.T) {
	// intl：主基址 -> center.qoder.sh -> openapi.{domain} -> website。
	intl := OpenAPICandidates("intl")
	if len(intl) < 3 {
		t.Fatalf("intl candidates too few: %v", intl)
	}
	if intl[0] != "https://openapi.qoder.sh" {
		t.Fatalf("intl primary: %v", intl)
	}
	center := -1
	openapiDomain := -1
	website := -1
	for i, h := range intl {
		switch h {
		case "https://center.qoder.sh":
			center = i
		case "https://openapi.qoder.com":
			openapiDomain = i
		case "https://qoder.com":
			website = i
		}
	}
	if center < 0 || openapiDomain < 0 || website < 0 {
		t.Fatalf("intl missing entries: %v", intl)
	}
	// center 必须排在官网域名之前（issue: 回退候选）。
	if center > openapiDomain || center > website {
		t.Fatalf("center.qoder.sh must precede website: %v", intl)
	}

	// cn：无 center.qoder.sh（实测 401，故仅 intl）。
	cn := OpenAPICandidates("cn")
	for _, h := range cn {
		if h == "https://center.qoder.sh" {
			t.Fatalf("cn must not include center.qoder.sh: %v", cn)
		}
	}
	if cn[0] != "https://openapi.qoder.com.cn" {
		t.Fatalf("cn primary: %v", cn)
	}

	// env 覆盖主基址（QD_{REALM}_OPENAPI）。
	t.Setenv("QD_INTL_OPENAPI", "https://proxy.example/")
	got := OpenAPICandidates("intl")
	if got[0] != "https://proxy.example" {
		t.Fatalf("env override: %v", got)
	}
	// 去重：主基址与 openapi.{domain} 相同（cn）时不重复。
	seen := map[string]bool{}
	for _, h := range cn {
		if seen[h] {
			t.Fatalf("duplicate candidate: %v", cn)
		}
		seen[h] = true
	}
}

// 404 免探缓存：命中后不再探基址，TTL 后重探。
func TestOpenAPIPathMissingCache(t *testing.T) {
	resetOpenAPIMissingCache()
	if OpenAPIPathMissing("intl", "/sash/x") {
		t.Fatal("should start empty")
	}
	markOpenAPIPathMissing("intl", "/sash/x")
	if !OpenAPIPathMissing("intl", "/sash/x") {
		t.Fatal("should be marked missing")
	}
	if OpenAPIPathMissing("cn", "/sash/x") {
		t.Fatal("must be realm-scoped")
	}
	resetOpenAPIMissingCache()
	if OpenAPIPathMissing("intl", "/sash/x") {
		t.Fatal("reset must clear")
	}
}
