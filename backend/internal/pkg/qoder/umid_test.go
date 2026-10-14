package qoder

// umid_test.go 二开新增：原生身份桥（umid.go）与 DesktopHeaders 真身份注入
// 的单元测试。组件调用经 runRuntimeInfoFn 注入 stub（Windows 无法伪造 ELF
// 组件）；缓存状态经 resetUmidState 隔离，测试间互不串扰。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// resetUmidState 清空进程内身份缓存并注入组件 stub；返回还原函数。
func resetUmidState(t *testing.T, sample func(uid string) MachineIdentity) {
	t.Helper()
	umidMu.Lock()
	saved := umidMemCache
	umidMemCache = map[string]umidCacheEntry{}
	umidMu.Unlock()
	savedFn := runRuntimeInfoFn
	runRuntimeInfoFn = sample
	t.Setenv("QD_UMID_DIR", t.TempDir())
	t.Cleanup(func() {
		umidMu.Lock()
		umidMemCache = saved
		umidMu.Unlock()
		runRuntimeInfoFn = savedFn
	})
}

// seedUmidDiskCache 预置落盘身份缓存（缓存优先路径：命中不调组件）。
func seedUmidDiskCache(t *testing.T, realm string, ident MachineIdentity) {
	t.Helper()
	doc := umidDiskDoc{Version: 1, Realms: map[string]umidDiskEntry{
		realm: {MachineToken: ident.MachineToken, MachineType: ident.MachineType,
			MachineCode: ident.MachineCode, CachedAt: time.Now()},
	}}
	blob, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(umidCachePath(), blob, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNativeMachineIdentityCacheFirst(t *testing.T) {
	calls := 0
	ident := MachineIdentity{MachineToken: "mt-1", MachineType: "KVM", MachineCode: "mc-1"}
	resetUmidState(t, func(string) MachineIdentity {
		calls++
		return ident
	})
	got := NativeMachineIdentity("intl", "u1")
	if got != ident {
		t.Fatalf("ident = %+v", got)
	}
	// 第二次：进程内缓存命中，不再调组件。
	if again := NativeMachineIdentity("intl", "u2"); again != ident {
		t.Fatalf("cached ident = %+v", again)
	}
	if calls != 1 {
		t.Fatalf("component calls = %d, want 1", calls)
	}
}

func TestNativeMachineIdentityDiskCacheHitsWithoutComponent(t *testing.T) {
	ident := MachineIdentity{MachineToken: "mt-disk", MachineType: "Docker", MachineCode: "mc-disk"}
	resetUmidState(t, func(string) MachineIdentity {
		t.Fatal("落盘缓存命中时不应调用组件")
		return MachineIdentity{}
	})
	seedUmidDiskCache(t, "intl", ident)
	if got := NativeMachineIdentity("intl", "u1"); got != ident {
		t.Fatalf("ident = %+v", got)
	}
}

func TestNativeMachineIdentityFallsBackToExpiredDiskCache(t *testing.T) {
	// 组件彻底失败（返回空）→ 回退落盘缓存（哪怕过期）。
	ident := MachineIdentity{MachineToken: "mt-old", MachineType: "KVM", MachineCode: "mc-old"}
	resetUmidState(t, func(string) MachineIdentity { return MachineIdentity{} })
	seedUmidDiskCache(t, "cn", ident)
	if got := NativeMachineIdentity("cn", "u1"); got != ident {
		t.Fatalf("fallback ident = %+v", got)
	}
}

func TestDesktopHeadersIdentitySources(t *testing.T) {
	t.Run("native 优先且注入真值", func(t *testing.T) {
		ident := MachineIdentity{MachineToken: "mt-native", MachineType: "KVM", MachineCode: "mc-native"}
		resetUmidState(t, func(string) MachineIdentity { return ident })
		creds := Credentials{Realm: "intl", UID: "u-h", AccessToken: "t",
			MachineToken: "mt-imported"}
		h, source := creds.DesktopHeadersIdentity()
		if source != MachineIdentityNative {
			t.Fatalf("source = %q", source)
		}
		if h.Get("cosy-machinetoken") != "mt-native" ||
			h.Get("cosy-machinetype") != "KVM" ||
			h.Get("cosy-machinecode") != "mc-native" {
			t.Fatalf("machine headers: %v %v %v",
				h.Get("cosy-machinetoken"), h.Get("cosy-machinetype"), h.Get("cosy-machinecode"))
		}
		if h.Get("cosy-machineid") != DeriveID("u-h", "machine") {
			t.Fatalf("machineid = %q", h.Get("cosy-machineid"))
		}
		if h.Get("User-Agent") != "Qoder" || h.Get("cosy-clienttype") != DesktopClientType {
			t.Fatalf("desktop base headers missing: %q %q", h.Get("User-Agent"), h.Get("cosy-clienttype"))
		}
	})
	t.Run("无原生回退导入凭证", func(t *testing.T) {
		resetUmidState(t, func(string) MachineIdentity { return MachineIdentity{} })
		creds := Credentials{Realm: "cn", UID: "u-h", AccessToken: "t",
			MachineToken: "mt-imported"}
		h, source := creds.DesktopHeadersIdentity()
		if source != MachineIdentityImported {
			t.Fatalf("source = %q", source)
		}
		if h.Get("cosy-machinetoken") != "mt-imported" ||
			h.Get("cosy-machinetype") != DeriveMachineType("u-h") ||
			h.Get("cosy-machinecode") != DeriveID("u-h", "machinecode") {
			t.Fatalf("imported headers: %v %v %v",
				h.Get("cosy-machinetoken"), h.Get("cosy-machinetype"), h.Get("cosy-machinecode"))
		}
	})
	t.Run("都无则一个机器头都不发", func(t *testing.T) {
		resetUmidState(t, func(string) MachineIdentity { return MachineIdentity{} })
		creds := Credentials{Realm: "cn", UID: "u-h", AccessToken: "t"}
		h, source := creds.DesktopHeadersIdentity()
		if source != MachineIdentityOmitted {
			t.Fatalf("source = %q", source)
		}
		for _, key := range []string{"cosy-machineid", "cosy-machinetoken",
			"cosy-machinetype", "cosy-machinecode", "cosy-machineos", "cosy-machinehostname"} {
			if v := h.Get(key); v != "" {
				t.Fatalf("machine header %s must be absent, got %q", key, v)
			}
		}
		// 薄包装保持一致。
		if _, s := creds.DesktopHeadersIdentity(); s != source {
			t.Fatal("DesktopHeaders wrapper diverges")
		}
	})
}

func TestCampaignsIdentitySelfHeal(t *testing.T) {
	// 自愈会删组件种子 $HOME/.config/.locale_cfg——隔离 HOME，避免碰到真实文件。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	first := MachineIdentity{MachineToken: "mt-rejected", MachineType: "KVM", MachineCode: "mc-rejected"}
	fresh := MachineIdentity{MachineToken: "mt-fresh", MachineType: "KVM", MachineCode: "mc-fresh"}
	var mu sync.Mutex
	componentCalls := 0
	resetUmidState(t, func(string) MachineIdentity {
		mu.Lock()
		defer mu.Unlock()
		// 模拟真实组件：同种子返回同一身份；自愈删种子后的再次调用必换新。
		componentCalls++
		if componentCalls == 1 {
			return first
		}
		return fresh
	})

	requests := 0
	var lastMachineToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		lastMachineToken = r.Header.Get("cosy-machinetoken")
		// 第一次（被拒身份）：showCampaign=false；自愈换新身份后的重试：true。
		if requests == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"showCampaign": false, "campaigns": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"showCampaign": true, "claimable": true,
			"campaigns": []any{map[string]any{
				"campaignId": "c1", "campaignKey": "daily100", "claimStatus": "CLAIMABLE",
				"actionType": "CLAIM_BENEFIT",
				"benefit":    map[string]any{"kind": "CREDITS", "amount": 100}}}})
	}))
	defer srv.Close()
	withRealm(t, "intl", srv.URL)
	client := NewClient(nil)
	creds := Credentials{Realm: "intl", UID: "u-h", AccessToken: "t"}

	st, err := client.Campaigns(context.Background(), creds)
	if err != nil {
		t.Fatal(err)
	}
	if !st.OK || !st.ShowCampaign {
		t.Fatalf("self-heal result: %+v", st)
	}
	if st.MachineHeaders != MachineIdentityNative {
		t.Fatalf("machine headers source: %q", st.MachineHeaders)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
	if lastMachineToken != fresh.MachineToken {
		t.Fatalf("retry must carry fresh identity, got %q", lastMachineToken)
	}
}

func TestUmidSeedPathUnderHome(t *testing.T) {
	p := identitySeedPath()
	if p == "" {
		t.Skip("no HOME")
	}
	if filepath.Base(p) != ".locale_cfg" {
		t.Fatalf("seed path = %q", p)
	}
}
