package qoder

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
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
	// VPCInstance 企业 VPC 实例名（空 = 公网）。非空时 OpenAPI/Gateway/Website
	// 已替换为实例专属域，且不再有公网故障回退。
	VPCInstance string
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

// VPCConfig 企业 VPC 实例的三个官方主机（qoder_accounts.py parse_vpc_endpoint）。
type VPCConfig struct {
	Instance string
	Domain   string
	Website  string
	OpenAPI  string
	Gateway  string
}

const vpcDomainSuffix = "vpc.qoder.com.cn"

var vpcInstanceRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// ParseVPCEndpoint 解析 QD_CN_VPC_ENDPOINT / 账号 vpc_endpoint：接受实例名
// （acme）或三个服务域之一（acme[-openapi|-gateway].vpc.qoder.com.cn）。
// 路径、端口、凭据、query、fragment、首尾空白一律拒绝，避免只改掉流程里的
// 一部分端点。
func ParseVPCEndpoint(value string) (*VPCConfig, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(value)
	if raw != value || strings.ContainsAny(raw, "\r\n\t") {
		return nil, fmt.Errorf("invalid VPC endpoint: surrounding whitespace or control characters")
	}
	target := raw
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("invalid VPC endpoint: malformed host or port")
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" ||
		parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" ||
		parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("invalid VPC endpoint: expected an instance name or a domain under %s", vpcDomainSuffix)
	}
	host := strings.ToLower(parsed.Hostname())
	instance := host
	if strings.HasSuffix(host, "."+vpcDomainSuffix) {
		instance = strings.TrimSuffix(host, "."+vpcDomainSuffix)
		instance = strings.TrimSuffix(instance, "-openapi")
		instance = strings.TrimSuffix(instance, "-gateway")
	} else if strings.Contains(host, ".") {
		return nil, fmt.Errorf("invalid VPC endpoint: expected an instance name or a domain under %s", vpcDomainSuffix)
	}
	if !vpcInstanceRe.MatchString(instance) {
		return nil, fmt.Errorf("invalid VPC endpoint: bad instance name %q", instance)
	}
	domain := instance + "." + vpcDomainSuffix
	return &VPCConfig{
		Instance: instance,
		Domain:   domain,
		Website:  "https://" + domain,
		OpenAPI:  "https://" + instance + "-openapi." + vpcDomainSuffix,
		Gateway:  "https://" + instance + "-gateway." + vpcDomainSuffix,
	}, nil
}

var (
	globalVPC    *VPCConfig
	globalVPCErr string
)

func init() {
	cfg, err := ParseVPCEndpoint(os.Getenv("QD_CN_VPC_ENDPOINT"))
	if err != nil {
		globalVPCErr = err.Error()
		return
	}
	globalVPC = cfg
}

// VPCConfigError 环境变量 QD_CN_VPC_ENDPOINT 的解析错误（空 = 无错误）。
func VPCConfigError() string { return globalVPCErr }

// vpcRealmConfig 在 cn 公网配置基础上替换企业 VPC 三个域；企业实例只能
// 访问实例专属域，公网故障回退全部移除。
func vpcRealmConfig(vpc *VPCConfig) *RealmConfig {
	base := *RealmConfigs["cn"]
	base.OpenAPI = vpc.OpenAPI
	base.Gateway = vpc.Gateway
	base.Website = vpc.Website
	base.Domain = vpc.Domain
	base.GatewayFallbacks = nil
	base.VPCInstance = vpc.Instance
	return &base
}

// publicRealmConfig 公网配置的副本。
func publicRealmConfig(realm string) *RealmConfig {
	if cfg, ok := RealmConfigs[realm]; ok {
		cp := *cfg
		return &cp
	}
	cp := *RealmConfigs["cn"]
	return &cp
}

// RealmConfigFor 取区域配置并应用企业 VPC 覆盖。
//
//	vpc == ""      继承部署级 QD_CN_VPC_ENDPOINT（cn）
//	vpc == "public"/"-" 显式使用公网（即使部署级已配 VPC）
//	其它           解析为实例名/专属域（仅 cn）
func RealmConfigFor(realm, vpc string) (*RealmConfig, error) {
	realm = strings.TrimSpace(realm)
	if !ValidRealm(realm) {
		realm = "cn"
	}
	override := strings.TrimSpace(vpc)
	if override == "public" || override == "-" {
		return publicRealmConfig(realm), nil
	}
	if override == "" {
		if realm == "cn" && globalVPC != nil {
			return vpcRealmConfig(globalVPC), nil
		}
		if realm == "cn" && globalVPCErr != "" {
			return nil, fmt.Errorf("invalid QD_CN_VPC_ENDPOINT: %s", globalVPCErr)
		}
		return publicRealmConfig(realm), nil
	}
	if realm != "cn" {
		return nil, fmt.Errorf("VPC is only available for CN accounts")
	}
	parsed, err := ParseVPCEndpoint(override)
	if err != nil {
		return nil, err
	}
	if parsed == nil {
		return publicRealmConfig(realm), nil
	}
	return vpcRealmConfig(parsed), nil
}

// realmConfigForCredentials 账号级配置；VPC 无效时回落公网（调用方应在建号
// 时校验，避免把企业凭证发到公网）。
func realmConfigForCredentials(creds Credentials) *RealmConfig {
	cfg, err := RealmConfigFor(creds.NormalizedRealm(), creds.VPCEndpoint)
	if err != nil {
		return publicRealmConfig(creds.NormalizedRealm())
	}
	return cfg
}

// Realm 取区域配置（部署级 VPC 生效；账号级覆盖用 RealmConfigFor）。
// 未知区域回落 cn（qoder_accounts.py L806-807）。
func Realm(name string) *RealmConfig {
	realm := strings.TrimSpace(name)
	if !ValidRealm(realm) {
		realm = "cn"
	}
	if realm == "cn" && globalVPC != nil {
		return vpcRealmConfig(globalVPC)
	}
	return publicRealmConfig(realm)
}

// ValidRealm 报告 name 是否为合法区域名。
func ValidRealm(name string) bool {
	_, ok := RealmConfigs[name]
	return ok
}

// GatewayCandidates 该区域的推理主机候选列表（主选 + 故障切换域名）。
// 签名只覆盖 path，同一请求换主机后签名依旧有效（qoder_accounts.py L810-820）。
func GatewayCandidates(realm string) []string {
	out, _ := GatewayCandidatesFor(realm, "")
	return out
}

// GatewayCandidatesFor 账号级候选；VPC 实例只有实例专属推理域，不回退公网。
func GatewayCandidatesFor(realm, vpc string) ([]string, error) {
	cfg, err := RealmConfigFor(realm, vpc)
	if err != nil {
		return nil, err
	}
	out := []string{cfg.Gateway}
	for _, host := range cfg.GatewayFallbacks {
		if host != "" && !slicesContains(out, host) {
			out = append(out, host)
		}
	}
	return out, nil
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
	out, _ := OpenAPICandidatesFor(realm, "")
	return out
}

// OpenAPICandidatesFor 账号级 openapi 候选；VPC 实例只返回专属域（绝不把
// 企业凭证发往公网）。
func OpenAPICandidatesFor(realm, vpc string) ([]string, error) {
	cfg, err := RealmConfigFor(realm, vpc)
	if err != nil {
		return nil, err
	}
	r := realm
	if !ValidRealm(r) {
		r = "cn"
	}
	out := make([]string, 0, 5)
	if cfg.VPCInstance != "" {
		base := strings.TrimRight(cfg.OpenAPI, "/")
		if base != "" {
			return []string{base}, nil
		}
		return nil, fmt.Errorf("invalid VPC endpoint: openapi base is empty")
	}
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
