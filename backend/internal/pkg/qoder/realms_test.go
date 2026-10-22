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
