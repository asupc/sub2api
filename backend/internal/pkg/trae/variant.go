// Package trae 实现 Trae 上游协议的 Go 移植：产品线变体表、Cloud-IDE-JWT 解析、
// OAuth 设备授权流、llm_utils_chat 请求体构造、SOLO 私有 SSE 到 OpenAI SSE 的
// 转换、积分与签到接口，以及 sub2api credentials JSONB 契约。
//
// 移植自 trae-workbuddy-switch（Rust）的 buddy-switch-core/src/modules/trae 与
// buddy-switch-gateway/src/trae，常量逐字对齐源码（出处注释随行标注）。
// 本包只依赖标准库，所有网络调用通过参数注入的 *http.Client 完成。
package trae

import "strings"

// Variant 标识一条「产品线 × 区域」的程序位（四值）。
//
// 字符串值对齐 sub2api credentials 契约（trae_cn|solo_cn|trae_global|solo_global），
// 与 Rust 源枚举的对应关系（源 variant.rs）：
//
//	trae_cn      ← TraeVariant::Trae           （TraeCode IDE 线，国内）
//	solo_cn      ← TraeVariant::TraeWork       （TRAE SOLO / Trae Work 线，国内）
//	trae_global  ← TraeVariant::GlobalTraeCode （TraeCode IDE 线，国际）
//	solo_global  ← TraeVariant::Global         （TRAE SOLO / Trae Work 线，国际）
type Variant string

const (
	VariantTraeCN     Variant = "trae_cn"
	VariantSoloCN     Variant = "solo_cn"
	VariantTraeGlobal Variant = "trae_global"
	VariantSoloGlobal Variant = "solo_global"
)

// AllVariants 全部四个程序位。
func AllVariants() []Variant {
	return []Variant{VariantSoloCN, VariantTraeCN, VariantSoloGlobal, VariantTraeGlobal}
}

// Region 持久化区域。源码实测结论：产品线不改变端点，region 才改变端点。
type Region string

const (
	RegionCN   Region = "cn"
	RegionIntl Region = "intl"
)

// Region 返回该程序位所属区域。
func (v Variant) Region() Region {
	switch v {
	case VariantTraeCN, VariantSoloCN:
		return RegionCN
	case VariantTraeGlobal, VariantSoloGlobal:
		return RegionIntl
	}
	return RegionCN
}

// DisplayName 对外展示名。
func (v Variant) DisplayName() string {
	if s, ok := specTable[v]; ok {
		return s.DisplayName
	}
	return string(v)
}

// PackageType 客户端 product.json 的 packageType。
func (v Variant) PackageType() string {
	if s, ok := specTable[v]; ok {
		return s.PackageType
	}
	return ""
}

// OAuthLine 返回该程序位的授权页产品线（决定 auth_from / client_id / hide_saas_login /
// PlatformCode）。判定依据逐字照抄客户端 main.js 的 packageType 分派：
// SOLO_CN|SOLO_I18N|SOLO_CN_ENTERPRISE → SOLO_Lite（solo），其余 → TRAE（trae）。
// 授权页产品线与区域正交：两条区域的 SOLO 线共用同一把钥匙。
func (v Variant) OAuthLine() OAuthLine {
	return OAuthLineFromPackageType(v.PackageType())
}

// EndpointSet 一套区域端点（取值来源：客户端 product.json 的 bootConfig.<能力>.trae.<regionKey>）。
type EndpointSet struct {
	// AccountBase 账号中心 / 签到 / 积分基址（bootConfig.account.trae.*）。
	AccountBase string
	// IcubeBase iCube / 市场基址（bootConfig.iCube.*）。
	IcubeBase string
	// AgentHost 对话（agent）网关基址（bootConfig.agent.trae.*，与 IcubeBase 不同主机）。
	AgentHost string
	// WSBase WebSocket 基址（bootConfig.ws.trae.*）。
	WSBase string
	// ConsoleBase 授权页（网页控制台）域（bootConfig.consoleHost）。是网页域而非 API 域，
	// 授权页完整地址 = ConsoleBase + "/authorization"，必须按区域分家。
	ConsoleBase string
}

// CN 端点（实测值，两条 CN 产品线逐字相同——产品线不改变端点，region 才改变）。
// 出处：variant.rs TRAE_WORK_SPEC/TRAE_CN_SPEC 的 cn_endpoints。
var cnEndpoints = EndpointSet{
	AccountBase: "https://api.trae.cn",
	IcubeBase:   "https://api.trae.com.cn",
	AgentHost:   "https://trae-api-cn.mchost.guru",
	WSBase:      "wss://trae-ws-cn.mchost.guru/custom_model",
	ConsoleBase: "https://www.trae.cn",
}

// 国际端点（国际版客户端自述值；主机名权威，但源码标注接口路径从未对真实上游跑通）。
// 出处：variant.rs GLOBAL_ENDPOINTS。
var intlEndpoints = EndpointSet{
	AccountBase: "https://grow-normal.trae.ai",
	IcubeBase:   "https://icube-normal.trae.ai",
	AgentHost:   "https://core-normal.trae.ai",
	WSBase:      "wss://wss-normal.trae.ai/custom_model",
	ConsoleBase: "https://www.trae.ai",
}

// Endpoints 返回该程序位实际使用的端点集（按区域查表）。
func (v Variant) Endpoints() EndpointSet {
	if v.Region() == RegionIntl {
		return intlEndpoints
	}
	return cnEndpoints
}

// Spec 一个程序位的全部差异描述。
type Spec struct {
	Variant     Variant
	DisplayName string
	NameAlias   string // 客户端 product.json 的 nameAlias（官方自称）
	PackageType string
	OAuthLine   OAuthLine
	Function    string // llm_utils_chat 的 function 字段（上游按它做模型白名单）
}

// specTable 四程序位派生表。packageType / function / 授权线逐字取自源码：
//   - packageType：SOLO_CN / TRAE_CN / SOLO_I18N / TRAE_I18N（TRAE_I18N 源码标注为推断值）
//   - function：solo_work_lite（TraeWork 家族）/ chat_v3（TraeCode 家族），按程序分家不按区域
var specTable = map[Variant]Spec{
	VariantSoloCN: {
		Variant:     VariantSoloCN,
		DisplayName: "Trae Work",
		NameAlias:   "TraeWork CN",
		PackageType: "SOLO_CN",
		OAuthLine:   OAuthLineSolo,
		Function:    FunctionSoloWork,
	},
	VariantTraeCN: {
		Variant:     VariantTraeCN,
		DisplayName: "Trae CN",
		NameAlias:   "TraeCode CN",
		PackageType: "TRAE_CN",
		OAuthLine:   OAuthLineTrae,
		Function:    FunctionChatV3,
	},
	VariantSoloGlobal: {
		Variant:     VariantSoloGlobal,
		DisplayName: "Trae Work",
		NameAlias:   "TraeWork",
		PackageType: "SOLO_I18N",
		OAuthLine:   OAuthLineSolo,
		Function:    FunctionSoloWork,
	},
	VariantTraeGlobal: {
		Variant:     VariantTraeGlobal,
		DisplayName: "Trae AI",
		NameAlias:   "TraeCode",
		PackageType: "TRAE_I18N",
		OAuthLine:   OAuthLineTrae,
		Function:    FunctionChatV3,
	},
}

// SpecFor 取变体描述符。
func SpecFor(v Variant) (Spec, bool) {
	s, ok := specTable[v]
	return s, ok
}

// llm_utils_chat 的 function 字段取值（出处：gateway mod.rs）。
// 上游按 function 做模型白名单：写错线会让另一条产品线的模型全部 4001 param is invalid。
const (
	// FunctionSoloWork TraeWork（SOLO）家族的 chat 分组。
	FunctionSoloWork = "solo_work_lite"
	// FunctionChatV3 TraeCode（IDE）家族的 chat 分组。
	FunctionChatV3 = "chat_v3"
)

// FunctionFor 取该程序位该用的 function（判据是程序而不是区域）。
func FunctionFor(v Variant) string {
	if s, ok := specTable[v]; ok {
		return s.Function
	}
	return FunctionChatV3
}

// OAuthLine 授权页的产品线（客户端 iCubeApp.authConfig 的两把钥匙）。
type OAuthLine string

const (
	// OAuthLineSolo SOLO 产品线（packageType ∈ {SOLO_CN, SOLO_I18N, SOLO_CN_ENTERPRISE}）。
	OAuthLineSolo OAuthLine = "solo"
	// OAuthLineTrae TRAE / IDE 产品线（其余 packageType）。
	OAuthLineTrae OAuthLine = "trae"
)

// OAuthLineFromPackageType 客户端的 packageType → 产品线分派（逐字照抄，含企业版取值）。
// 未登记的取值一律落 TRAE 线，与客户端 ? : 的 else 分支同向。
func OAuthLineFromPackageType(packageType string) OAuthLine {
	switch packageType {
	case "SOLO_CN", "SOLO_I18N", "SOLO_CN_ENTERPRISE":
		return OAuthLineSolo
	}
	return OAuthLineTrae
}

// AuthFrom 授权页 auth_from 参数（客户端同名字符串）。
func (l OAuthLine) AuthFrom() string {
	if l == OAuthLineSolo {
		return "solo"
	}
	return "trae"
}

// DefaultClientID 内置 client_id 默认值——客户端 product.json 的
// iCubeApp.authConfig.<线>.stable（两条区域的客户端取值逐字相同，故只按产品线分）。
// ⚠️ 拿另一条线的值去发登录，授权页会停在 billing status 后不回跳。
func (l OAuthLine) DefaultClientID() string {
	if l == OAuthLineSolo {
		// 出处：variant.rs OAuthLine::default_client_id（SOLO.stable）
		return "en1oxy7wnw8j9n"
	}
	// 出处：variant.rs OAuthLine::default_client_id（TRAE.stable）
	return "ono9krqynydwx5"
}

// HideSaasLogin 是否追加 hide_saas_login=true（auth_from 的从属参数，不单独配置）。
func (l OAuthLine) HideSaasLogin() bool {
	return l == OAuthLineSolo
}

// PlatformCode DeviceInfo.PlatformCode（同样是产品线级取值，与 auth_from 同一判定）。
// SOLO 线报 SOLO_PC、IDE 线报 IDE_PC；写错会触发上游 20403/040036 Token device not match。
func (l OAuthLine) PlatformCode() string {
	if l == OAuthLineSolo {
		return "SOLO_PC"
	}
	return "IDE_PC"
}

// parseAliases 小写别名 → 变体。覆盖源 TraeVariant::parse 的全部取值，
// 使 Rust 侧持久化的字符串（trae_work / global / global_trae_code 等）也能读回。
var parseAliases = map[string]Variant{
	// solo_cn（源 TraeWork）
	"solo_cn": VariantSoloCN, "trae_work": VariantSoloCN, "traework": VariantSoloCN,
	"trae work": VariantSoloCN, "work": VariantSoloCN, "solo": VariantSoloCN,
	"trae solo": VariantSoloCN, "trae solo cn": VariantSoloCN, "cn": VariantSoloCN,
	// trae_cn（源 Trae）
	"trae_cn": VariantTraeCN, "traecn": VariantTraeCN, "trae cn": VariantTraeCN,
	"ide": VariantTraeCN, "trae": VariantTraeCN, "trae_code": VariantTraeCN,
	"traecode": VariantTraeCN,
	// solo_global（源 Global）
	"solo_global": VariantSoloGlobal, "global": VariantSoloGlobal,
	"intl": VariantSoloGlobal, "international": VariantSoloGlobal,
	// trae_global（源 GlobalTraeCode）。注意：源码曾把 "trae_global" 作为区域标识
	// 归入 Global（SOLO 线），但它是 sub2api credentials 的契约值（TraeCode 国际线），
	// 契约优先——源别名让位。
	"trae_global": VariantTraeGlobal, "global_trae_code": VariantTraeGlobal,
	"global_traecode": VariantTraeGlobal, "trae_code_global": VariantTraeGlobal,
	"traecodeglobal": VariantTraeGlobal, "trae global": VariantTraeGlobal,
}

// ParseVariant 从字符串解析变体（大小写不敏感，接受源项目的常见别名）。
// 第二个返回值表示是否识别成功；不认识的输入绝不硬判成某个变体。
func ParseVariant(s string) (Variant, bool) {
	key := strings.ToLower(strings.TrimSpace(s))
	v, ok := parseAliases[key]
	return v, ok
}

// String 实现 fmt.Stringer，返回契约字符串。
func (v Variant) String() string {
	return string(v)
}
