package qoder

import "strings"

// 双区域常量（qoder_accounts.py L39-80 REALM_CONFIGS 直译，逆向自官方
// 桌面/CLI 客户端）。has_checkin 等提示位不再作为门控（能力改为运行时探测），
// 故不保留；home_dir/app_dir 属本机桌面客户端路径，服务器场景不适用。
type RealmConfig struct {
	Name string
	// OpenAPI 业务域基址（纯 Bearer，无 COSY 签名）。
	OpenAPI string
	// Gateway 推理主机（intl 主选 api1，api2/api3 为官方故障切换域名）。
	Gateway          string
	GatewayFallbacks []string
	Website          string
	ClientID         string
	RedirectURI      string
	Domain           string
	UA               string
	// SendClientID / SendRedirectURI / NonceDashed 为 OAuth 设备授权
	// URL 的双区参数差异标志：
	//   CN  : client_id + machine_id + redirect_uri，nonce 为带横线 uuid
	//   Intl: client_id + machine_id（无 redirect_uri），nonce 为 32-hex
	SendClientID    bool
	SendRedirectURI bool
	NonceDashed     bool
}

// RealmConfigs 双区配置（键 "cn" / "intl"）。
var RealmConfigs = map[string]*RealmConfig{
	"cn": {
		Name:            "国内版 (China)",
		OpenAPI:         "https://openapi.qoder.com.cn",
		Gateway:         "https://gateway.qoder.com.cn",
		Website:         "https://qoder.com.cn",
		ClientID:        "1c5e33e1-364d-4ce6-b02c-acaa81274a5c",
		RedirectURI:     "qoder-work-cn://",
		Domain:          "qoder.com.cn",
		UA:              "QoderWork/1.1.64",
		SendClientID:    true,
		SendRedirectURI: true,
		NonceDashed:     true,
	},
	"intl": {
		Name:    "国际版 (Global)",
		OpenAPI: "https://openapi.qoder.sh",
		Gateway: "https://api1.qoder.sh",
		// 推理主机取自 0.4.3 客户端的 endpoint 缓存/内置候选：
		// api1 主选，api2/api3 为官方故障切换域名，连不上时按顺序切换。
		GatewayFallbacks: []string{"https://api2.qoder.sh", "https://api3.qoder.sh"},
		Website:          "https://qoder.com",
		ClientID:         "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb",
		RedirectURI:      "qoder://aicoding.aicoding-agent/login-success",
		Domain:           "qoder.com",
		UA:               "Qoder/1.1.64",
		SendClientID:     true,
		SendRedirectURI:  false,
		NonceDashed:      false,
	},
}

// Realm 取区域配置；未知区域回落 cn（qoder_accounts.py L806-807）。
func Realm(name string) *RealmConfig {
	if cfg, ok := RealmConfigs[name]; ok {
		return cfg
	}
	return RealmConfigs["cn"]
}

// ValidRealm 报告 name 是否为合法区域名。
func ValidRealm(name string) bool {
	_, ok := RealmConfigs[name]
	return ok
}

// GatewayCandidates 该区域的推理主机候选列表（主选 + 故障切换域名）。
// 签名只覆盖 path，同一请求换主机后签名依旧有效（qoder_accounts.py L810-820）。
func GatewayCandidates(realm string) []string {
	cfg := Realm(realm)
	out := []string{cfg.Gateway}
	for _, host := range cfg.GatewayFallbacks {
		if host != "" && !slicesContains(out, host) {
			out = append(out, host)
		}
	}
	return out
}

func slicesContains(list []string, v string) bool {
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

// DetectRealmFromDomain 按账号域名推断区域（qoder_accounts.py L823-828）。
func DetectRealmFromDomain(domain string) string {
	d := strings.ToLower(domain)
	if strings.Contains(d, "qoder.sh") ||
		(strings.HasSuffix(d, "qoder.com") && !strings.Contains(d, "qoder.com.cn")) ||
		strings.Contains(d, "qoder.com/") {
		return "intl"
	}
	return "cn"
}

// 业务端点常量（qoder_accounts.py L89-108：全部挂 openapi 基址，纯 Bearer，
// 无 COSY 签名；qoder_proxy.py L239-241 为推理/模型清单的网关端点）。
const (
	PathDevicePoll     = "/api/v1/deviceToken/poll"
	PathDeviceRefresh  = "/api/v1/deviceToken/refresh"
	PathJobExchange    = "/api/v1/jobToken/exchange"
	PathJobRefresh     = "/api/v1/jobToken/refresh"
	PathUserinfo       = "/api/v1/userinfo"
	PathQuotaUsage     = "/api/v2/quota/usage"
	PathUserPlan       = "/api/v2/user/plan"
	PathCheckinStatus  = "/sash/api/v1/me/daily-check-in/status"
	PathCheckinClaim   = "/sash/api/v1/me/daily-check-in/claim"
	PathProEligibility = "/sash/api/v1/me/pro-upgrade/eligibility"
	PathProClaim       = "/sash/api/v1/me/pro-upgrade/claim"
	PathCampaigns      = "/sash/api/v1/me/campaigns"

	// ChatPath 推理端点（qoder_proxy.py L239-240）。
	ChatPath = "/algo/api/v2/service/pro/sse/agent_chat_generation" +
		"?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
	// ModelsPath 模型清单端点（qoder_proxy.py L241）。
	ModelsPath = "/algo/api/v2/model/list?Encode=1"
)

// CampaignRewardPath 单个活动的奖励查询端点（qoder_accounts.py L107）。
func CampaignRewardPath(campaignID string) string {
	return "/sash/api/v1/me/campaigns/" + campaignID + "/reward"
}

// CampaignClaimPath 单个活动的领取端点（qoder_accounts.py L108）。
func CampaignClaimPath(campaignID string) string {
	return "/sash/api/v1/me/campaigns/" + campaignID + "/claim"
}

// CLIENT_UA openapi 域统一 User-Agent（qoder_accounts.py L82）。
const ClientUA = "Go-http-client/2.0"

// LoginTTLSeconds 设备授权登录窗口有效期（qoder_accounts.py L83）。
const LoginTTLSeconds = 600

// 桌面端专用请求头常量（qoder_accounts.py L110-114：活动平台必需；
// CLI=5 / QoderWork=6 / 桌面端=10）。
const (
	DesktopClientType    = "10"
	DesktopClientVersion = "0.4.3"
	MachineOS            = "x86_64_win32"
	MachineHostname      = "DESKTOP-QODER"
)

// sessionDeadMarkers 会话死亡标记（qoder_accounts.py L798）：上游主动吊销
// 离线会话，刷新已无意义，需要重新登录。
var sessionDeadMarkers = []string{"TOKEN_EXPIRE", "12153", "Offline user session not found"}

// SessionDead 报告错误文本是否命中会话死亡标记（qoder_accounts.py L801-803）。
func SessionDead(msg string) bool {
	for _, m := range sessionDeadMarkers {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}
