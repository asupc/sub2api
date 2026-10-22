//go:build unit

package trae

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseModelCatalog(t *testing.T) {
	raw := `{"function_configs":[
		{"config_info_list":[
			{"config_name":"glm-5.3","context_window_tokens":{"dev":200000,"max":1000000}},
			{"config_name":"glm-5.3","context_window_tokens":{"dev":116000,"max":900000}},
			{"config_name":"doubao-seed-2.1-turbo","context_window_tokens":{"dev":256000,"max":256000}},
			{"config_name":"custom_internal","context_window_tokens":{"dev":1,"max":2}},
			{"config_name":"no window"}
		]},
		{"config_info_list":[
			{"config_name":"Mimo-V2.6-Pro","context_window_tokens":{"dev":"200000","max":"981504"}}
		]}
	]}`
	cat, err := ParseModelCatalog([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]CatalogEntry{}
	for _, e := range cat.Entries {
		byID[strings.ToLower(e.ID)] = e
	}
	// MaxModeWindow 走进程内缓存：装进缓存后再查。
	ResetModelCatalogForTest()
	defer ResetModelCatalogForTest()
	catalogMem["cn"] = cat
	if len(byID) != 3 {
		t.Fatalf("entries = %d（自定义条目应剔除）", len(byID))
	}
	glm := byID["glm-5.3"]
	if glm.Dev == nil || *glm.Dev != 200000 || glm.Max == nil || *glm.Max != 1000000 {
		t.Fatalf("glm 应取多视图最大值: %+v", glm)
	}
	mimo := byID["mimo-v2.6-pro"]
	if mimo.Max == nil || *mimo.Max != 981504 {
		t.Fatalf("数字字符串应可解析: %+v", mimo)
	}
	// max == dev ⇒ 不支持 Max Mode。
	if _, _, ok := MaxModeWindow("cn", "doubao-seed-2.1-turbo"); ok {
		t.Fatal("max == dev 不支持 Max Mode")
	}
	if dev, max, ok := MaxModeWindow("cn", "glm-5.3"); !ok || dev != 200000 || max != 1000000 {
		t.Fatal("glm-5.3 应支持 Max Mode 200k→1M")
	}
	if _, _, ok := MaxModeWindow("cn", " glm-5.3 "); !ok {
		t.Fatal("模型名应做 trim+小写归一后命中")
	}
}

func TestEnsureCatalogFetchAndCache(t *testing.T) {
	ResetModelCatalogForTest()
	defer ResetModelCatalogForTest()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("x-ide-token") != "jwt-1" {
			t.Errorf("x-ide-token = %q", r.Header.Get("x-ide-token"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, has := body["functions"]; !has {
			t.Error("缺 functions 字段")
		}
		_, _ = w.Write([]byte(`{"function_configs":[{"config_info_list":[
			{"config_name":"glm-5.3","context_window_tokens":{"dev":200000,"max":1000000}}]}]}`))
	}))
	defer srv.Close()
	savedEndpoint := catalogEndpoint
	catalogEndpoint = srv.URL
	defer func() { catalogEndpoint = savedEndpoint }()
	EnsureCatalog(context.Background(), srv.Client(), RegionCN, "Cloud-IDE-JWT jwt-1")
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if dev, max, ok := ContextFor(RegionCN, "GLM-5.3"); !ok || dev == nil || *dev != 200000 || *max != 1000000 {
		t.Fatal("目录应进内存缓存")
	}
	// TTL 内重复 ensure 不再请求。
	EnsureCatalog(context.Background(), srv.Client(), RegionCN, "jwt-1")
	if calls != 1 {
		t.Fatalf("TTL 内不应重复请求, calls = %d", calls)
	}
	// 区域隔离。
	if _, _, ok := ContextFor(RegionIntl, "glm-5.3"); ok {
		t.Fatal("区域应隔离")
	}
}

func TestPrepareLLMChatBodyMaxModeInjection(t *testing.T) {
	ResetModelCatalogForTest()
	defer ResetModelCatalogForTest()
	// 目录未同步：不注入（不猜）。
	body := PrepareLLMChatBody([]byte(`{"model":"glm-5.3","messages":[]}`), VariantTraeCN, "glm-5.3", "u", "d", "m")
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	if _, has := parsed["is_max_mode"]; has {
		t.Fatal("目录未同步不得注入 is_max_mode")
	}
	// 命中目录且 max > dev：注入。
	cat := &ModelCatalog{Entries: []CatalogEntry{{ID: "glm-5.3", Dev: devPtr(200000), Max: maxPtr(1000000)}}}
	catalogMem["cn"] = cat
	body = PrepareLLMChatBody([]byte(`{"model":"glm-5.3","messages":[]}`), VariantTraeCN, "glm-5.3", "u", "d", "m")
	_ = json.Unmarshal(body, &parsed)
	if parsed["is_max_mode"] != true {
		t.Fatal("支持 Max Mode 的模型应注入 is_max_mode")
	}
	// max == dev：不注入（注意用全新 map——json.Unmarshal 对已有 map 是合并语义）。
	catalogMem["cn"] = &ModelCatalog{Entries: []CatalogEntry{{ID: "doubao", Dev: devPtr(256000), Max: maxPtr(256000)}}}
	body = PrepareLLMChatBody([]byte(`{"model":"doubao","messages":[]}`), VariantTraeCN, "doubao", "u", "d", "m")
	parsed = nil
	_ = json.Unmarshal(body, &parsed)
	if _, has := parsed["is_max_mode"]; has {
		t.Fatal("max == dev 不得注入 is_max_mode")
	}
}

func devPtr(v int64) *int64  { return &v }
func maxPtr(v int64) *int64  { return &v }
