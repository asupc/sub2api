//go:build unit

package qoder

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// timeNowDate 今日正午的 epoch 秒（保证 CheckinStatus 的"今日已签"判定稳定）。
func timeNowDate() int64 {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, now.Location()).Unix()
}

func TestCredentialsRoundTrip(t *testing.T) {
	m := map[string]string{
		"access_token":      "dt-acc",
		"refresh_token":     "drt-ref",
		"token_kind":        "device",
		"expires_at":        "1760000000",
		"realm":             "intl",
		"uid":               "u-1",
		"nickname":          "测试号",
		"user_type":         "personal_professional_trial",
		"organization_id":   "org",
		"organization_name": "组织",
		"machine_id":        "mid",
		"machine_token":     "mtok",
	}
	c := ParseCredentials(m)
	if c.AccessToken != "dt-acc" || c.RefreshToken != "drt-ref" || c.TokenKind != "device" ||
		c.ExpiresAt != "1760000000" || c.Realm != "intl" || c.UID != "u-1" ||
		c.Nickname != "测试号" || c.UserType != "personal_professional_trial" ||
		c.OrganizationID != "org" || c.OrganizationName != "组织" ||
		c.MachineID != "mid" || c.MachineToken != "mtok" {
		t.Fatalf("parse: %+v", c)
	}
	back := c.ToMap()
	for k, v := range m {
		if back[k] != v {
			t.Fatalf("roundtrip key %s: %q != %q", k, back[k], v)
		}
	}
	if len(back) != len(m) {
		t.Fatalf("map size: %d", len(back))
	}
	if c.NormalizedRealm() != "intl" {
		t.Fatal("normalized realm")
	}
	if (Credentials{Realm: "bogus"}).NormalizedRealm() != "cn" {
		t.Fatal("bogus realm must fall back to cn")
	}
	// Identity 默认 user_type
	id := (Credentials{UID: "u", AccessToken: "t"}).Identity()
	if id.UserType != DefaultUserType {
		t.Fatalf("default user type: %q", id.UserType)
	}
}

func TestOpenAPIHeaders(t *testing.T) {
	creds := Credentials{AccessToken: "dt-x", Realm: "cn", UID: "u-42"}
	h := creds.OpenAPIHeaders()
	if h.Get("Authorization") != "Bearer dt-x" {
		t.Fatalf("auth = %q", h.Get("Authorization"))
	}
	if h.Get("User-Agent") != ClientUA {
		t.Fatalf("ua = %q", h.Get("User-Agent"))
	}
	if h.Get("X-Machine-ID") != DeriveID("u-42", "machine") ||
		h.Get("X-Session-ID") != DeriveID("u-42", "session") {
		t.Fatal("machine/session ids must be derived")
	}
	// X-Request-ID：稳定前缀 + 6 位微秒后缀
	rid := h.Get("X-Request-ID")
	if !strings.HasPrefix(rid, DeriveID("u-42", "req")+"-") || len(rid) != len(DeriveID("u-42", "req"))+7 {
		t.Fatalf("request id = %q", rid)
	}
	if h.Get("Origin") != "https://qoder.com.cn" || h.Get("Referer") != "https://qoder.com.cn/" {
		t.Fatalf("origin/referer: %q %q", h.Get("Origin"), h.Get("Referer"))
	}
}

func TestDesktopHeaders(t *testing.T) {
	// 无 machine_token：不发任何机器头（issue #10）
	creds := Credentials{AccessToken: "dt-x", Realm: "cn", UID: "u-42"}
	h := creds.DesktopHeaders()
	if h.Get("User-Agent") != "Qoder" {
		t.Fatalf("ua = %q", h.Get("User-Agent"))
	}
	if h.Get("cosy-clienttype") != "10" || h.Get("cosy-version") != "0.4.3" {
		t.Fatalf("desktop headers: %q %q", h.Get("cosy-clienttype"), h.Get("cosy-version"))
	}
	for _, k := range []string{"cosy-machineid", "cosy-machinetoken", "cosy-machinetype",
		"cosy-machinecode", "cosy-machineos", "cosy-machinehostname"} {
		if h.Get(k) != "" {
			t.Fatalf("derived machine headers must be omitted, got %s=%q", k, h.Get(k))
		}
	}
	// 存有真实 machine_token：发全套六头
	creds.MachineToken = "real-native-token"
	creds.MachineID = "native-mid"
	h2 := creds.DesktopHeaders()
	if h2.Get("cosy-machineid") != "native-mid" || h2.Get("cosy-machinetoken") != "real-native-token" {
		t.Fatalf("native machine headers: %q %q", h2.Get("cosy-machineid"), h2.Get("cosy-machinetoken"))
	}
	if h2.Get("cosy-machineos") != "x86_64_win32" || h2.Get("cosy-machinehostname") != "DESKTOP-QODER" {
		t.Fatal("machine os/hostname")
	}
}

func TestUserinfoAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathUserinfo {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer dt-ui" {
			t.Errorf("auth: %q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "u-9", "name": "昵称", "user_type": "personal_pro",
			"organization_id": "o1", "organization_name": "组织名"})
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)

	client := NewClient(nil)
	ui, err := client.Userinfo(context.Background(), Credentials{AccessToken: "dt-ui", Realm: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if ui.ID != "u-9" || ui.Name != "昵称" || ui.UserType != "personal_pro" ||
		ui.OrganizationID != "o1" || ui.OrganizationName != "组织名" {
		t.Fatalf("userinfo: %+v", ui)
	}
}

func TestQuotaUsageAggregation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != PathQuotaUsage {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer dt-q" {
			t.Errorf("auth header")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userQuota":            map[string]any{"remaining": 100.5, "used": 50, "total": 150},
			"addOnQuota":           map[string]any{"remaining": 10, "used": 0, "total": 10},
			"isQuotaExceeded":      false,
			"totalUsagePercentage": 37.5,
			"expiresAt":            1800000000000, // ms
		})
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)

	client := NewClient(nil)
	q, err := client.QuotaUsage(context.Background(), Credentials{AccessToken: "dt-q", Realm: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if q.Remain != 110 || q.Used != 50 || q.Size != 160 {
		t.Fatalf("aggregate: %+v", q)
	}
	if q.Exceeded {
		t.Fatal("exceeded")
	}
	if q.ExpiresAt != 1800000000 {
		t.Fatalf("expires ms->s: %d", q.ExpiresAt)
	}
	if len(q.Packages) != 2 || q.Packages[0].Name != "基础额度" ||
		q.Packages[1].Name != "赠送/签到额度" {
		t.Fatalf("packages: %+v", q.Packages)
	}
}

func TestCheckinLifecycle(t *testing.T) {
	statusCalled := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PathCheckinStatus:
			statusCalled++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "CLAIMABLE", "rewardCredits": 100,
				"currentStreakDays": 3, "totalClaimDays": 10,
				"lastClaimedAt": 0})
		case PathCheckinClaim:
			if r.Method != http.MethodPost {
				t.Errorf("claim method %s", r.Method)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "rewardCredits": 100})
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)

	client := NewClient(nil)
	creds := Credentials{AccessToken: "dt-c", Realm: "cn", UID: "u-c"}
	st, err := client.CheckinStatus(context.Background(), creds)
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != "CLAIMABLE" || !st.Active || st.TodayCheckedIn ||
		st.StreakDays != 3 || st.RewardCredits != 100 {
		t.Fatalf("status: %+v", st)
	}
	res, err := client.Checkin(context.Background(), creds)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Already || res.RewardCredits != 100 {
		t.Fatalf("checkin: %+v", res)
	}
}

func TestCheckinAlreadyAndUnavailable(t *testing.T) {
	// 已签到（CLAIMED + 今日 lastClaimedAt）
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		today := timeNowDate()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "CLAIMED", "lastClaimedAt": today})
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)
	client := NewClient(nil)
	res, err := client.Checkin(context.Background(), Credentials{AccessToken: "t", Realm: "cn"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || !res.Already {
		t.Fatalf("already: %+v", res)
	}

	// 接口不存在（404）
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv2.Close()
	withRealm(t, "intl", srv2.URL)
	if _, err := client.CheckinStatus(context.Background(),
		Credentials{AccessToken: "t", Realm: "intl"}); err != ErrCheckinUnavailable {
		t.Fatalf("intl checkin should be unavailable: %v", err)
	}
	res2, err := client.Checkin(context.Background(), Credentials{AccessToken: "t", Realm: "intl"})
	if err != nil || !res2.OK || !res2.Unavailable {
		t.Fatalf("unavailable checkin: %+v %v", res2, err)
	}
}

func TestProActions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case PathProEligibility:
			_ = json.NewEncoder(w).Encode(map[string]any{"eligible": true})
		case PathProClaim:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)
	client := NewClient(nil)
	creds := Credentials{AccessToken: "t", Realm: "cn"}
	ok, err := client.ProEligibility(context.Background(), creds)
	if err != nil || !ok {
		t.Fatalf("eligibility: %v %v", ok, err)
	}
	res, err := client.ProClaim(context.Background(), creds)
	if err != nil || !res.OK {
		t.Fatalf("claim: %+v %v", res, err)
	}
}

func TestCampaignsAndClaim(t *testing.T) {
	var authHeader, clientType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		clientType = r.Header.Get("cosy-clienttype")
		switch r.URL.Path {
		case PathCampaigns:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"showCampaign": true, "claimable": true, "campaignUrl": "https://qoder.com.cn/act",
				"campaigns": []any{map[string]any{
					"campaignId": "c1", "campaignKey": "daily100", "actionType": "CLAIM_BENEFIT",
					"claimStatus": "CLAIMABLE", "startAt": 1700000000, "endAt": 1799999999,
					"benefit": map[string]any{"kind": "CREDITS", "amount": 100},
					"placements": []any{map[string]any{"content": map[string]any{
						"zh": map[string]any{"title": "每日领取", "description": "每日 100 积分",
							"detailUrl": "https://d", "buttonText": "领取"}}}},
				}}})
		case "/sash/api/v1/me/campaigns/c1/claim":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "CLAIMED", "grantId": "g1", "replayed": false,
				"benefit": map[string]any{"amount": 100}})
		case "/sash/api/v1/me/campaigns/c1/reward":
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "CLAIMED"})
		default:
			t.Errorf("path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)
	client := NewClient(nil)
	creds := Credentials{AccessToken: "dt-camp", Realm: "cn", UID: "u-camp"}

	st, err := client.Campaigns(context.Background(), creds)
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer dt-camp" || clientType != "10" {
		t.Fatalf("campaigns headers: %q %q", authHeader, clientType)
	}
	if !st.OK || !st.ShowCampaign || !st.Claimable || len(st.Campaigns) != 1 {
		t.Fatalf("campaigns: %+v", st)
	}
	item := st.Campaigns[0]
	if item.CampaignID != "c1" || item.ActionType != "CLAIM_BENEFIT" ||
		item.ClaimStatus != "CLAIMABLE" || item.BenefitKind != "CREDITS" ||
		item.BenefitAmount != 100 || item.TitleZh != "每日领取" ||
		item.DescZh != "每日 100 积分" || item.DetailURL != "https://d" ||
		item.ButtonText != "领取" || item.StartAt != 1700000000 {
		t.Fatalf("item: %+v", item)
	}
	if st.MachineHeaders != "omitted" {
		t.Fatalf("machine headers state: %q", st.MachineHeaders)
	}

	claim, err := client.ClaimCampaign(context.Background(), creds, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if !claim.OK || claim.Status != "CLAIMED" || claim.Amount != 100 || claim.GrantID != "g1" {
		t.Fatalf("claim: %+v", claim)
	}
	reward, err := client.CampaignReward(context.Background(), creds, "c1")
	if err != nil || len(reward) == 0 {
		t.Fatalf("reward: %v", err)
	}
}

func TestClaimCampaignBlockedAndReplayed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "BLOCKED", "failureCode": "SAME_PERSON_ALREADY_CLAIMED"})
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)
	client := NewClient(nil)
	claim, err := client.ClaimCampaign(context.Background(),
		Credentials{AccessToken: "t", Realm: "cn"}, "cx")
	if err != nil {
		t.Fatal(err)
	}
	if claim.OK || !claim.Blocked || claim.FailureCode != "SAME_PERSON_ALREADY_CLAIMED" {
		t.Fatalf("blocked: %+v", claim)
	}

	// 409 -> 幂等已领取
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`ALREADY_CLAIMED`))
	}))
	defer srv2.Close()
	withRealm(t, "cn", srv2.URL)
	claim2, err := client.ClaimCampaign(context.Background(),
		Credentials{AccessToken: "t", Realm: "cn"}, "cy")
	if err != nil {
		t.Fatal(err)
	}
	if !claim2.OK || !claim2.Replayed || claim2.Status != "CLAIMED" {
		t.Fatalf("replayed: %+v", claim2)
	}
}

func TestUserPlanAction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"plan_tier_name": "Pro Trial"})
	}))
	defer srv.Close()
	withRealm(t, "cn", srv.URL)
	client := NewClient(nil)
	plan, err := client.UserPlan(context.Background(), Credentials{AccessToken: "t", Realm: "cn"})
	if err != nil || plan != "Pro Trial" {
		t.Fatalf("plan: %q %v", plan, err)
	}
}

// campaignTestCreds 活动链路测试用凭据。
func campaignTestCreds() Credentials {
	return Credentials{AccessToken: "dt-camp", Realm: "cn", UID: "u-camp"}
}

// campaignServer 按路径路由的 campaigns 假服务器（记录请求行，供断言）。
func campaignServer(t *testing.T, routes map[string]http.HandlerFunc) (*Client, *[]string) {
	t.Helper()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if h, ok := routes[r.URL.Path]; ok {
			h(w, r)
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	withRealm(t, "cn", srv.URL)
	return NewClient(nil), &calls
}

func jsonHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestCampaignCheckinClaimSuccess(t *testing.T) {
	client, calls := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"claimable":true,"campaigns":[
			{"campaignId":"c1","campaignKey":"daily100","actionType":"CLAIM_BENEFIT",
				"claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100},
				"placements":[{"content":{"zh":{"title":"每日领取"}}}]}]}`),
		"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(
			`{"status":"CLAIMED","replayed":false,"benefit":{"amount":100}}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Claimed) != 1 || out.Claimed[0] != "每日领取" || out.Earned != 100 {
		t.Fatalf("outcome: %+v", out)
	}
	if out.Message != "活动领取成功 +100 Credits（每日领取）" {
		t.Fatalf("message: %q", out.Message)
	}
	if out.NextAvailableNote == "" {
		t.Fatal("next window must be given unconditionally")
	}
	if len(*calls) != 2 {
		t.Fatalf("calls: %v", *calls)
	}
}

// TestCampaignCheckinClaimedWithViewDetails #25 回归：领取后 actionType 被降级
// VIEW_DETAILS，CLAIMED + amount>0 必须归 already，不能误报"暂无活动"。
func TestCampaignCheckinClaimedWithViewDetails(t *testing.T) {
	client, _ := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"c1","campaignKey":"daily100","actionType":"VIEW_DETAILS",
				"claimStatus":"CLAIMED","benefit":{"kind":"CREDITS","amount":100},
				"placements":[{"content":{"zh":{"title":"每日领取"}}}]}]}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Already) != 1 || out.Already[0] != "每日领取" || len(out.Views) != 0 {
		t.Fatalf("outcome: %+v", out)
	}
	if out.Message != "今日活动奖励已领取（每日领取）" {
		t.Fatalf("message: %q", out.Message)
	}
}

// TestCampaignCheckinCooldown 冷却命中不再 POST claim（同人去重防风控）。
func TestCampaignCheckinCooldown(t *testing.T) {
	client, calls := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"c1","campaignKey":"daily100","actionType":"CLAIM_BENEFIT",
				"claimStatus":"CLAIMABLE","benefit":{"kind":"CREDITS","amount":100}}]}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"},
		BlockedUntil: map[string]int64{"c1": time.Now().Unix() + 3600},
		ClaimGap:     time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Blocked) != 1 || len(out.Claimed) != 0 {
		t.Fatalf("outcome: %+v", out)
	}
	if !strings.Contains(out.Message, "同人已领取") {
		t.Fatalf("message: %q", out.Message)
	}
	for _, c := range *calls {
		if strings.HasPrefix(c, "POST") {
			t.Fatalf("cooldown must not claim: %v", *calls)
		}
	}
}

// TestCampaignCheckinBlockedMarks claim 返回 BLOCKED → 记 6h 冷却标记。
func TestCampaignCheckinBlockedMarks(t *testing.T) {
	client, _ := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"c1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE",
				"benefit":{"kind":"CREDITS","amount":100}}]}`),
		"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(
			`{"status":"BLOCKED","failureCode":"SAME_PERSON_ALREADY_CLAIMED"}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if exp := out.BlockedMarks["c1"]; exp < time.Now().Unix()+5*3600 {
		t.Fatalf("blocked marks: %+v", out.BlockedMarks)
	}
}

// TestCampaignCheckinPendingLocked 名额发完 → pending；成就未完成 → locked。
func TestCampaignCheckinPendingLocked(t *testing.T) {
	client, _ := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"p1","campaignKey":"ka","actionType":"CLAIM_BENEFIT","claimStatus":"NOT_CLAIMABLE",
				"unavailableReason":"REDEMPTION_CODE_OUT_OF_STOCK","benefit":{"kind":"CREDITS","amount":100}},
			{"campaignId":"l1","campaignKey":"kb","actionType":"CLAIM_BENEFIT","claimStatus":"NOT_CLAIMABLE",
				"unavailableReason":"ACHIEVEMENT_NOT_COMPLETED","requiredAchievementKey":"ach-1",
				"benefit":{"kind":"CREDITS","amount":100}}]}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Pending) != 1 || out.Pending[0] != "ka" ||
		len(out.Locked) != 1 || out.Locked[0] != "kb" {
		t.Fatalf("outcome: %+v", out)
	}
	if !strings.Contains(out.Message, "名额已发完") || !strings.Contains(out.Message, "新人任务") {
		t.Fatalf("message: %q", out.Message)
	}
}

// TestCampaignCheckinUnavailable 区域无活动平台（404）→ OK=false（service 兜底）。
func TestCampaignCheckinUnavailable(t *testing.T) {
	client, _ := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.OK || out.Available || out.Error == "" {
		t.Fatalf("outcome: %+v", out)
	}
}

// TestCampaignCheckinOnlyKinds 券类（COUPON）不在 only_kinds → 完全跳过。
func TestCampaignCheckinOnlyKinds(t *testing.T) {
	client, calls := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"v1","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE",
				"benefit":{"kind":"COUPON","amount":1}},
			{"campaignId":"c1","campaignKey":"daily","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE",
				"benefit":{"kind":"credits","amount":100}}]}`),
		"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(`{"status":"CLAIMED","benefit":{"amount":100}}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Claimed) != 1 || out.Earned != 100 {
		t.Fatalf("outcome: %+v", out)
	}
	// 只对 CREDITS 条目 c1 发起领取；券类 v1 被 only_kinds 跳过（无对应 claim POST）。
	if len(*calls) != 2 || (*calls)[1] != "POST /sash/api/v1/me/campaigns/c1/claim" {
		t.Fatalf("coupon must be skipped: %v", *calls)
	}
}

// TestCampaignCheckinReplayedCountsNotEarned 幂等回放计入 already，Earned 不增。
func TestCampaignCheckinReplayedCountsNotEarned(t *testing.T) {
	client, _ := campaignServer(t, map[string]http.HandlerFunc{
		PathCampaigns: jsonHandler(`{"showCampaign":true,"campaigns":[
			{"campaignId":"c1","campaignKey":"daily","actionType":"CLAIM_BENEFIT","claimStatus":"CLAIMABLE",
				"benefit":{"kind":"CREDITS","amount":100}}]}`),
		"/sash/api/v1/me/campaigns/c1/claim": jsonHandler(
			`{"status":"CLAIMED","replayed":true,"benefit":{"amount":100}}`),
	})
	out, err := client.CampaignCheckin(context.Background(), CampaignCheckinInput{
		Creds: campaignTestCreds(), OnlyKinds: []string{"", "CREDITS"}, ClaimGap: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Earned != 0 || len(out.Already) != 1 || len(out.Claimed) != 0 {
		t.Fatalf("outcome: %+v", out)
	}
}

func TestNextCheckinWindow(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	before := time.Date(2026, 10, 14, 9, 59, 0, 0, loc)
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, loc)
	after := time.Date(2026, 10, 14, 21, 30, 0, 0, loc)

	ts, note := nextCheckinWindow(before)
	if want := time.Date(2026, 10, 14, 10, 0, 0, 0, loc).Unix(); ts != want {
		t.Fatalf("before 10:00 must give today: %d != %d", ts, want)
	}
	if note != "10-14 10:00（UTC+8）" {
		t.Fatalf("note: %q", note)
	}
	// 到达即算下一轮（不返回已经到点的时刻）。
	ts2, _ := nextCheckinWindow(at)
	if want := time.Date(2026, 10, 15, 10, 0, 0, 0, loc).Unix(); ts2 != want {
		t.Fatalf("at 10:00 must give tomorrow: %d != %d", ts2, want)
	}
	ts3, _ := nextCheckinWindow(after)
	if want := time.Date(2026, 10, 15, 10, 0, 0, 0, loc).Unix(); ts3 != want {
		t.Fatalf("after 10:00 must give tomorrow: %d != %d", ts3, want)
	}
}
