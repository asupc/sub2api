//go:build unit

package trae

import "testing"

// TestVariantEndpointsPerRegion 四变体的区域端点 / client_id / function / package_type
// 派生表断言（逐字对齐 Rust 源 variant.rs 的表）。
func TestVariantEndpointsPerRegion(t *testing.T) {
	cases := []struct {
		variant     Variant
		region      Region
		accountBase string
		icubeBase   string
		agentHost   string
		wsBase      string
		consoleBase string
		packageType string
		oauthLine   OAuthLine
		clientID    string
		function    string
	}{
		{
			variant: VariantSoloCN, region: RegionCN,
			accountBase: "https://api.trae.cn", icubeBase: "https://api.trae.com.cn",
			agentHost: "https://trae-api-cn.mchost.guru", wsBase: "wss://trae-ws-cn.mchost.guru/custom_model",
			consoleBase: "https://www.trae.cn", packageType: "SOLO_CN",
			oauthLine: OAuthLineSolo, clientID: "en1oxy7wnw8j9n", function: "solo_work_lite",
		},
		{
			variant: VariantTraeCN, region: RegionCN,
			accountBase: "https://api.trae.cn", icubeBase: "https://api.trae.com.cn",
			agentHost: "https://trae-api-cn.mchost.guru", wsBase: "wss://trae-ws-cn.mchost.guru/custom_model",
			consoleBase: "https://www.trae.cn", packageType: "TRAE_CN",
			oauthLine: OAuthLineTrae, clientID: "ono9krqynydwx5", function: "chat_v3",
		},
		{
			variant: VariantSoloGlobal, region: RegionIntl,
			accountBase: "https://grow-normal.trae.ai", icubeBase: "https://icube-normal.trae.ai",
			agentHost: "https://core-normal.trae.ai", wsBase: "wss://wss-normal.trae.ai/custom_model",
			consoleBase: "https://www.trae.ai", packageType: "SOLO_I18N",
			oauthLine: OAuthLineSolo, clientID: "en1oxy7wnw8j9n", function: "solo_work_lite",
		},
		{
			variant: VariantTraeGlobal, region: RegionIntl,
			accountBase: "https://grow-normal.trae.ai", icubeBase: "https://icube-normal.trae.ai",
			agentHost: "https://core-normal.trae.ai", wsBase: "wss://wss-normal.trae.ai/custom_model",
			consoleBase: "https://www.trae.ai", packageType: "TRAE_I18N",
			oauthLine: OAuthLineTrae, clientID: "ono9krqynydwx5", function: "chat_v3",
		},
	}
	for _, tc := range cases {
		if got := tc.variant.Region(); got != tc.region {
			t.Errorf("%s region = %s, want %s", tc.variant, got, tc.region)
		}
		ep := tc.variant.Endpoints()
		if ep.AccountBase != tc.accountBase {
			t.Errorf("%s account_base = %s, want %s", tc.variant, ep.AccountBase, tc.accountBase)
		}
		if ep.IcubeBase != tc.icubeBase {
			t.Errorf("%s icube_base = %s, want %s", tc.variant, ep.IcubeBase, tc.icubeBase)
		}
		if ep.AgentHost != tc.agentHost {
			t.Errorf("%s agent_host = %s, want %s", tc.variant, ep.AgentHost, tc.agentHost)
		}
		if ep.WSBase != tc.wsBase {
			t.Errorf("%s ws_base = %s, want %s", tc.variant, ep.WSBase, tc.wsBase)
		}
		if ep.ConsoleBase != tc.consoleBase {
			t.Errorf("%s console_base = %s, want %s", tc.variant, ep.ConsoleBase, tc.consoleBase)
		}
		if got := tc.variant.PackageType(); got != tc.packageType {
			t.Errorf("%s package_type = %s, want %s", tc.variant, got, tc.packageType)
		}
		if got := tc.variant.OAuthLine(); got != tc.oauthLine {
			t.Errorf("%s oauth_line = %s, want %s", tc.variant, got, tc.oauthLine)
		}
		if got := tc.variant.OAuthLine().DefaultClientID(); got != tc.clientID {
			t.Errorf("%s client_id = %s, want %s", tc.variant, got, tc.clientID)
		}
		if got := FunctionFor(tc.variant); got != tc.function {
			t.Errorf("%s function = %s, want %s", tc.variant, got, tc.function)
		}
	}
}

// TestOAuthLineDerivations 授权页产品线三件取值两两不同（按线分家等于没分），
// 且 packageType 分派逐字对齐客户端（含企业版取值）。
func TestOAuthLineDerivations(t *testing.T) {
	if OAuthLineSolo.AuthFrom() != "solo" || OAuthLineTrae.AuthFrom() != "trae" {
		t.Errorf("auth_from 取值漂了: solo=%s trae=%s", OAuthLineSolo.AuthFrom(), OAuthLineTrae.AuthFrom())
	}
	if !OAuthLineSolo.HideSaasLogin() || OAuthLineTrae.HideSaasLogin() {
		t.Error("hide_saas_login 必须只在 SOLO 线为 true")
	}
	if OAuthLineSolo.PlatformCode() != "SOLO_PC" || OAuthLineTrae.PlatformCode() != "IDE_PC" {
		t.Errorf("platform_code 漂了: solo=%s trae=%s", OAuthLineSolo.PlatformCode(), OAuthLineTrae.PlatformCode())
	}
	for _, pt := range []string{"SOLO_CN", "SOLO_I18N", "SOLO_CN_ENTERPRISE"} {
		if OAuthLineFromPackageType(pt) != OAuthLineSolo {
			t.Errorf("%s 应落 SOLO 线", pt)
		}
	}
	for _, pt := range []string{"TRAE_CN", "TRAE_I18N", "TRAE_CN_ENTERPRISE", ""} {
		if OAuthLineFromPackageType(pt) != OAuthLineTrae {
			t.Errorf("%q 应落 TRAE 线", pt)
		}
	}
}

// TestParseVariant 别名解析：契约四值 + 源项目的持久化字符串都能读回。
func TestParseVariant(t *testing.T) {
	aliases := map[string]Variant{
		// 契约值
		"trae_cn": VariantTraeCN, "solo_cn": VariantSoloCN,
		"trae_global": VariantTraeGlobal, "solo_global": VariantSoloGlobal,
		// 源字符串（大小写不敏感）
		"trae_work": VariantSoloCN, "TRAE SOLO CN": VariantSoloCN, "solo": VariantSoloCN, "cn": VariantSoloCN,
		"trae": VariantTraeCN, "ide": VariantTraeCN, "trae_code": VariantTraeCN,
		"global": VariantSoloGlobal, "intl": VariantSoloGlobal, "international": VariantSoloGlobal,
		"global_trae_code": VariantTraeGlobal, "trae_code_global": VariantTraeGlobal,
		"traecodeglobal": VariantTraeGlobal, "trae global": VariantTraeGlobal,
	}
	for input, want := range aliases {
		got, ok := ParseVariant(input)
		if !ok || got != want {
			t.Errorf("ParseVariant(%q) = %s(ok=%v), want %s", input, got, ok, want)
		}
	}
	for _, bad := range []string{"", "doubao", "unknown"} {
		if _, ok := ParseVariant(bad); ok {
			t.Errorf("ParseVariant(%q) 不应识别", bad)
		}
	}
}

// TestSpecTableCoversAll 四个程序位全部登记且 DisplayName 非空。
func TestSpecTableCoversAll(t *testing.T) {
	for _, v := range AllVariants() {
		spec, ok := SpecFor(v)
		if !ok {
			t.Fatalf("%s 缺少 spec", v)
		}
		if spec.DisplayName == "" || spec.Function == "" || spec.PackageType == "" {
			t.Errorf("%s spec 字段不完整: %+v", v, spec)
		}
		// 国际版两个程序位与国内同线共用 client_id，但与另一条线必须不同。
		if spec.OAuthLine.DefaultClientID() == "" {
			t.Errorf("%s client_id 为空", v)
		}
	}
}
