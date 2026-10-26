package qoder

import (
	"os"
	"strings"
)

// 双区域常量（qoder_accounts.py L39-80 REALM_CONFIGS 直译，逆向自官方
// 桌面/CLI 客户端）。has_checkin 等提示位不再作为门控（能力改为运行时探测），
// 故不保留；home_dir/app_dir 属本机桌面客户端路径，服务器场景不适用。
type RealmConfig struct {
	Name string
	// OpenAPI 业务域基址（纯 Bearer，无 COSY 签名）。
	OpenAPI string
	// Gateway 推理主选主机（intl 为 api3，备选见 GatewayFallbacks）。
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
		UA:              "QoderWork/1.1.75",
		SendClientID:    true,
		SendRedirectURI: true,
		NonceDashed:     true,
	},
	"intl": {
		Name:    "国际版 (Global)",
		OpenAPI: "https://openapi.qoder.sh",
		// 推理主机拆成两个集群——inference=api3、securityInference=api2；api1
		// 已不在官方候选首位（本机实测也常年超时）。按官方端点缓存顺序对齐：
		// api3 主、api2/api1 备（issue #61 时代的 api1 主选已过时）。
		// QD_INTL_GATEWAY 可覆盖主选。
		Gateway:         intlGateway(),
		GatewayFallbacks: []string{"https://api2.qoder.sh", "https://api1.qoder.sh"},
		Website:          "https://qoder.com",
		ClientID:         "e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb",
		RedirectURI:      "qoder://aicoding.aicoding-agent/login-success",
		Domain:           "qoder.com",
		UA:               "Qoder/1.1.75",
		SendClientID:     true,
		SendRedirectURI:  false,
		NonceDashed:      false,
	},
}

// intlGateway intl 推理主选（QD_INTL_GATEWAY 覆盖，默认 api3）。
func intlGateway() string {
	if v := strings.TrimSpace(os.Getenv("QD_INTL_GATEWAY")); v != "" {
		return v
	}
	return "https://api3.qoder.sh"
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

// OpenAPICandidates 该区域 openapi 基址候选（qoder_accounts.py openapi_candidates，
// issue: 国际版签不到到）。
//
// 官方把业务端点都挂在 openapi 基址上；国际版历史上是 openapi.qoder.sh，但该域名
// 在部分网络/时期不可达，表现为「活动列表拿不到 -> 签到无效」。这里给出候选顺序，
// 调用方**只在连接失败或 404 时**换下一个（见 doJSONOpenAPI），业务错误不重试。
//
// 候选顺序（源 L935-954 逐条）：
//  1. env QD_{REALM}_OPENAPI（运维覆盖主基址）；
//  2. 配置主基址（cfg.OpenAPI）；
//  3. intl 专属回退 https://center.qoder.sh（0.4.3 客户端仍引用；实测 intl=200 /
//     cn=401，故仅 intl 加入）——放在官网域名之前；
//  4. https://openapi.{domain}；
//  5. 官网 website。
func OpenAPICandidates(realm string) []string {
	cfg := Realm(realm)
	r := realm
	if !ValidRealm(r) {
		r = "cn"
	}
	out := make([]string, 0, 5)
	if env := strings.TrimSpace(os.Getenv("QD_" + strings.ToUpper(r) + "_OPENAPI")); env != "" {
		out = append(out, strings.TrimRight(env, "/"))
	}
	if base := strings.TrimRight(cfg.OpenAPI, "/"); base != "" {
		out = append(out, base)
	}
	extra := []string{}
	if r == "intl" {
		extra = append(extra, "https://center.qoder.sh")
	}
	tail := extra
	if d := strings.TrimSpace(cfg.Domain); d != "" {
		tail = append(tail, "https://openapi."+d)
	}
	if w := strings.TrimRight(cfg.Website, "/"); w != "" {
		tail = append(tail, w)
	}
	for _, host := range tail {
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
	// 官方积分消耗（0.4.3 客户端新增，cn/intl 实测均 200，issue: 官方积分消耗）：
	//   summary -> {totalCredits, peakCredits, peakDate, unit}
	//   heatmap -> {items: [{date, value}...]}（近 365 天，按天积分消耗）
	PathCreditsSummary = "/sash/api/v1/ai-conversations/credits-summary"
	PathCreditsHeatmap = "/sash/api/v1/ai-conversations/credits-heatmap"

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
	DesktopClientType = "10"
	// DesktopClientVersion 官方桌面端版本（desktop 链路 cosy-version 用；
	// 0.4.8 对齐 #75 实测的官方主进程，QD_DESKTOP_VERSION 经
	// desktopClientVersion() 覆盖）。
	DesktopClientVersion = "0.4.8"
)

// desktopClientVersion 桌面端版本（QD_DESKTOP_VERSION 可覆盖，空值回落内置）。
func desktopClientVersion() string {
	if v := strings.TrimSpace(os.Getenv("QD_DESKTOP_VERSION")); v != "" {
		return v
	}
	return DesktopClientVersion
}

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
