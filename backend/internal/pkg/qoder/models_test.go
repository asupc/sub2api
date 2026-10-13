//go:build unit

package qoder

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticCatalogsLoad(t *testing.T) {
	for _, realm := range []string{"cn", "intl"} {
		entries, err := StaticModels(realm)
		if err != nil {
			t.Fatalf("static %s: %v", realm, err)
		}
		if len(entries) < 5 {
			t.Fatalf("static %s too small: %d", realm, len(entries))
		}
		keys := map[string]bool{}
		for _, e := range entries {
			if e.Key == "" {
				t.Fatal("entry without key")
			}
			keys[e.Key] = true
		}
		if !keys["auto"] || !keys["qmodel"] {
			t.Fatalf("%s missing core models: %v", realm, keys)
		}
	}
	// 双区清单差异（qoder_catalog.py L1557-1560）
	intlKeys := realmKeySet(t, "intl")
	cnKeys := realmKeySet(t, "cn")
	for _, k := range []string{"ultimate", "performance", "efficient", "cmodel", "smodel"} {
		if !intlKeys[k] {
			t.Fatalf("intl should include %s", k)
		}
		if cnKeys[k] {
			t.Fatalf("cn should NOT include %s", k)
		}
	}
	for _, k := range []string{"q37fmodel", "gm51model"} {
		if !cnKeys[k] {
			t.Fatalf("cn should include %s", k)
		}
		if intlKeys[k] {
			t.Fatalf("intl should NOT include %s", k)
		}
	}
}

func realmKeySet(t *testing.T, realm string) map[string]bool {
	t.Helper()
	entries, err := StaticModels(realm)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, e := range entries {
		out[e.Key] = true
	}
	return out
}

func TestResolveUpstreamKey(t *testing.T) {
	cases := []struct {
		in    string
		realm string
		want  string
	}{
		{"", "cn", "auto"},
		{"auto", "cn", "auto"},
		{"qoder-auto", "intl", "auto"},
		{"qwen3.8-max", "cn", "qmodel_38max"},
		{"Qwen3.8-Flash", "intl", "qfmodel"},                 // 显示名（不分大小写）
		{"qmodel_38max (Qwen3.8-Max)", "cn", "qmodel_38max"}, // 展示 id 形式
		{"deepseek-v4-flash", "cn", "dfmodel"},
		{"dfmodel (DeepSeek-V4-Flash)", "intl", "dfmodel"},
		{"glm-5.3", "cn", "gmodel"},
		{"kimi-k2.7-code", "cn", "kmodel"},
		{"minimax-m3", "intl", "mmodel"},
		{"极致", "intl", "ultimate"}, // 官方本地化名（zh.label）
		{"kimi-k2.8-preview", "cn", "kmodel"},
		{"some/unknown-model", "cn", "some/unknown-model"}, // 未知名原样透传（含 /）
	}
	for _, c := range cases {
		if got := ResolveUpstreamKey(c.in, c.realm); got != c.want {
			t.Fatalf("ResolveUpstreamKey(%q, %q) = %q, want %q", c.in, c.realm, got, c.want)
		}
	}
	// 区域 key 集合校验：cn 别名解析到 intl 独占 key 时仍按别名表返回
	// （跨区由调用方拦截）
	if got := ResolveUpstreamKey("ultimate", "cn"); got != "ultimate" {
		t.Fatalf("alias in cn realm: %q", got)
	}
}

func TestCatalogEntryPublicID(t *testing.T) {
	// 目录真实条目：显示名优先
	for _, e := range []struct{ key, want string }{
		{"qmodel_38max", "Qwen3.8-Max"},
		{"auto", "Auto"},
	} {
		entry := findByKey(t, mustStatic(t, "cn"), e.key)
		if got := entry.PublicID(); got != e.want {
			t.Fatalf("PublicID(%s) = %q, want %q", e.key, got, e.want)
		}
	}
	// 回落：显示名缺失 / 与 key 相同 / 仅空白
	for _, entry := range []CatalogEntry{
		{Key: "qmodel", DisplayName: ""},
		{Key: "qmodel", DisplayName: "qmodel"},
		{Key: "qmodel", DisplayName: "   "},
	} {
		if got := entry.PublicID(); got != entry.Key {
			t.Fatalf("PublicID(%+v) = %q, want key %q", entry, got, entry.Key)
		}
	}
	// 带空白显示名 Trim 后生效
	if got := (CatalogEntry{Key: "qmodel", DisplayName: " Qwen3.7-Plus "}).PublicID(); got != "Qwen3.7-Plus" {
		t.Fatalf("PublicID trim = %q", got)
	}
}

func mustStatic(t *testing.T, realm string) []CatalogEntry {
	t.Helper()
	entries, err := StaticModels(realm)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestNormalizeReasoningEffort(t *testing.T) {
	entries, err := StaticModels("intl")
	if err != nil {
		t.Fatal(err)
	}
	var ultimate, auto CatalogEntry
	for _, e := range entries {
		if e.Key == "ultimate" {
			ultimate = e
		}
		if e.Key == "auto" {
			auto = e
		}
	}
	// ultimate 支持 low/medium/high/xhigh/max（默认 high）
	if v, note := ultimate.NormalizeReasoningEffort("high"); v != "high" || note != "" {
		t.Fatalf("supported level: %q %q", v, note)
	}
	if v, _ := ultimate.NormalizeReasoningEffort("none"); v != "none" {
		t.Fatalf("off value: %q", v)
	}
	if v, note := ultimate.NormalizeReasoningEffort("low"); v != "low" || note != "" {
		t.Fatalf("low: %q %q", v, note)
	}
	// 未知档位 -> 默认档 high
	if v, note := ultimate.NormalizeReasoningEffort("bogus"); v != "high" || note == "" {
		t.Fatalf("unknown level: %q %q", v, note)
	}
	// 不支持的 xhigh 之上没有档位；minimal 未支持 -> 最近档
	if v, _ := ultimate.NormalizeReasoningEffort("minimal"); v != "low" {
		t.Fatalf("minimal -> nearest: %q", v)
	}
	// auto 无 thinking_config -> 原样透传
	if v, note := auto.NormalizeReasoningEffort("weird"); v != "weird" || note != "" {
		t.Fatalf("router passthrough: %q %q", v, note)
	}
}

func TestMergeCatalogDynamicWins(t *testing.T) {
	static, err := StaticModels("intl")
	if err != nil {
		t.Fatal(err)
	}
	// 动态返回 2 条：一条覆盖静态（max_input_tokens 变化），一条静态独有
	// （应被过滤），一条动态新增
	dynamic := []map[string]any{
		{"key": "auto", "max_input_tokens": 123, "enable": false},
		{"key": "brand-new", "display_name": "Brand New"},
	}
	merged := mergeCatalog(static, dynamic)
	byKey := map[string]CatalogEntry{}
	for _, e := range merged {
		byKey[e.Key] = e
	}
	if len(merged) != 2 {
		t.Fatalf("merged len = %d (dynamic-only set must govern): %+v", len(merged), merged)
	}
	if byKey["auto"].MaxInputTokens != 123 {
		t.Fatalf("dynamic override: %+v", byKey["auto"])
	}
	if byKey["brand-new"].DisplayName != "Brand New" {
		t.Fatalf("dynamic-only entry: %+v", byKey["brand-new"])
	}
	// 动态为空 -> 静态全量
	if all := mergeCatalog(static, nil); len(all) != len(static) {
		t.Fatalf("empty dynamic must keep static: %d vs %d", len(all), len(static))
	}
	// null 值不覆盖静态
	staticQ := findByKey(t, static, "ultimate")
	dynamic2 := []map[string]any{
		{"key": "ultimate", "display_name": nil, "thinking_config": nil},
	}
	merged2 := mergeCatalog(static, dynamic2)
	m := findByKey(t, merged2, "ultimate")
	if m.Thinking == nil || m.DisplayName != staticQ.DisplayName {
		t.Fatalf("null must not override: %+v", m)
	}
}

func findByKey(t *testing.T, entries []CatalogEntry, key string) CatalogEntry {
	t.Helper()
	for _, e := range entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("key %s not found", key)
	return CatalogEntry{}
}

func TestFetchModelsSignedGETBody(t *testing.T) {
	InvalidateModelsCache()
	var gotMethod, gotBody, gotAuth, gotAccept string
	var gotQuery bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		buf := make([]byte, 64)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		gotAuth = r.Header.Get("authorization")
		gotAccept = r.Header.Get("accept")
		gotQuery = r.URL.Query().Get("Encode") == "1" &&
			r.URL.Path == "/algo/api/v2/model/list"
		_, _ = w.Write([]byte(`{"chat":[{"key":"auto","display_name":"Auto","max_input_tokens":999}]}`))
	}))
	defer srv.Close()
	origIntl := RealmConfigs["intl"]
	RealmConfigs["intl"] = &RealmConfig{OpenAPI: "https://openapi.qoder.sh",
		Gateway: srv.URL, Website: "https://qoder.com"}
	t.Cleanup(func() { RealmConfigs["intl"] = origIntl })

	sess, err := NewCosySession(goldIdentity())
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(nil)
	entries, err := client.FetchModels(context.Background(), "intl", sess)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Fatalf("method = %s", gotMethod)
	}
	if gotBody != QoderEncode([]byte("{}")) {
		t.Fatalf("GET must carry the signed body, got %q", gotBody)
	}
	if !strings.HasPrefix(gotAuth, "Bearer COSY.") {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if gotAccept != "application/json" {
		t.Fatalf("accept = %q", gotAccept)
	}
	if !gotQuery {
		t.Fatal("models endpoint query")
	}
	if len(entries) != 1 || entries[0].Key != "auto" || entries[0].MaxInputTokens != 999 {
		t.Fatalf("entries: %+v", entries)
	}
	// TTL 缓存：第二次不再打上游
	srv.Close()
	entries2, err := client.FetchModels(context.Background(), "intl", nil)
	if err != nil || len(entries2) != 1 {
		t.Fatalf("cached fetch: %v %+v", err, entries2)
	}
	InvalidateModelsCache()
}
