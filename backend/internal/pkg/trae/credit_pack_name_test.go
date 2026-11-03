//go:build unit

package trae

import "testing"

// TestParseCreditPackagesPackageNameFromTopLevel 包名必须优先取 pack 顶层
// group_name / display_desc（移植 trae-workbuddy-switch 27250bd / issue #143）：
// 线上 entitlement_base_info 里没有 package_name，旧实现命中率恒 0，
// 全部显示成「未命名包」。
func TestParseCreditPackagesPackageNameFromTopLevel(t *testing.T) {
	now := int64(1_700_000_000)
	packs := []any{
		map[string]any{
			"entitlement_base_info": map[string]any{
				"quota": map[string]any{"credits_limit": 100.0},
			},
			"group_name":   "月卡",
			"display_desc": "月度积分包",
			"usage":        map[string]any{"credits_amount": 30.0},
			"expire_time":  float64(now + 3*24*3600),
		},
		map[string]any{
			"entitlement_base_info": map[string]any{
				"quota": map[string]any{"credits_limit": 500.0},
			},
			"display_desc": "年卡",
			"usage":        map[string]any{"credits_amount": 50.0},
		},
		map[string]any{
			"entitlement_base_info": map[string]any{
				"package_name": "旧版包",
				"quota":        map[string]any{"credits_limit": 10.0},
			},
			"usage": map[string]any{"credits_amount": 1.0},
		},
	}
	pkgs := ParseCreditPackages(packs, now)
	if len(pkgs) != 3 {
		t.Fatalf("packages = %d", len(pkgs))
	}
	if pkgs[0].PackageName != "月卡" {
		t.Fatalf("group_name priority: %q", pkgs[0].PackageName)
	}
	if pkgs[1].PackageName != "年卡" {
		t.Fatalf("display_desc fallback: %q", pkgs[1].PackageName)
	}
	if pkgs[2].PackageName != "旧版包" {
		t.Fatalf("entitlement_base_info compat fallback: %q", pkgs[2].PackageName)
	}
}

// TestParseCreditPackagesBlankName 纯空白的包名视为无名（与源 Rust 的
// 最终 filter(!trim.is_empty()) 对齐），不得把空白当成有效名称展示。
func TestParseCreditPackagesBlankName(t *testing.T) {
	now := int64(1_700_000_000)
	packs := []any{
		map[string]any{
			"entitlement_base_info": map[string]any{
				"package_name": "   ",
				"quota":        map[string]any{"credits_limit": 10.0},
			},
			"group_name": "",
			"usage":      map[string]any{"credits_amount": 1.0},
		},
	}
	pkgs := ParseCreditPackages(packs, now)
	if len(pkgs) != 1 {
		t.Fatalf("packages = %d", len(pkgs))
	}
	if pkgs[0].PackageName != "" {
		t.Fatalf("blank package name should be empty, got %q", pkgs[0].PackageName)
	}
}
