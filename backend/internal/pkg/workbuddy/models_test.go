//go:build unit

package workbuddy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// enterpriseModelsPayload 企业模型端点样例（agents[cli] 过滤 + 非对话剔除）。
const enterpriseModelsPayload = `{"code":0,"data":{
  "models":[
    {"id":"glm-5.2","name":"GLM-5.2","maxOutputTokens":16384,"reasoning":{"supportedEfforts":["high","xhigh"],"defaultEffort":"high"}},
    {"id":"hy3","name":"HY3","maxOutputTokens":8192},
    {"id":"nes-embed","name":"嵌入模型","maxOutputTokens":8192},
    {"id":"tiny-out","name":"Tiny","maxOutputTokens":128},
    {"id":"img-gen","name":"图片生成","maxOutputTokens":4096,"tags":["text-to-image"]},
    {"id":"deepseek-v4.1-flash","name":"DS Flash","maxOutputTokens":32768}
  ],
  "agents":[{"name":"cli","models":["glm-5.2","nes-embed","tiny-out","img-gen","deepseek-v4.1-flash"]}]
}}`

// v3ConfigPayload /v3/config 样例（含试用横幅）。
const v3ConfigPayload = `{"code":0,"data":{
  "models":[
    {"id":"glm-5.2","name":"GLM-5.2-v3","maxOutputTokens":16384,"reasoning":{"supportedEfforts":["high","xhigh","max"],"defaultEffort":"high"}},
    {"id":"gpt-5.5","name":"GPT-5.5","maxOutputTokens":65536},
    {"id":"hy4-preview","name":"HY4 Preview","maxOutputTokens":32768}
  ],
  "productFeaturesConfig":{"ModelTrialBanner":{"banners":[{"modelId":"hy4-preview-f","targetModelId":"hy4-preview"}]}}
}}`

// TestFetchModelsCN CN 双路并发合并：v3 为主、企业端点补缺、cli 面过滤、非对话剔除。
func TestFetchModelsCN(t *testing.T) {
	seenV3 := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/config", func(w http.ResponseWriter, r *http.Request) {
		seenV3++
		if r.Header.Get("User-Agent") != codeBuddyIDEUA {
			t.Errorf("CN v3 探测应带 IDE UA，got %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v3ConfigPayload))
	})
	mux.HandleFunc("/console/enterprises/personal/models", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer at" {
			t.Errorf("企业端点应带 Bearer，got %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(enterpriseModelsPayload))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	out, err := c.FetchModels(context.Background(), cnCred())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if seenV3 == 0 {
		t.Error("v3 探测未发起")
	}
	byID := map[string]ModelInfo{}
	for _, mi := range out {
		byID[mi.ID] = mi
	}
	// 并集：v3（glm-5.2 权威 + gpt-5.5 + hy4-preview）+ 企业补缺（deepseek-v4.1-flash）。
	for _, id := range []string{"glm-5.2", "gpt-5.5", "hy4-preview", "hy4-preview-f", "deepseek-v4.1-flash"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("并集缺模型 %q，got %v", id, idsOf(out))
		}
	}
	// 剔除：非对话（nes-/tiny/图片生成）与 cli 面外模型（hy3 不在 agents[cli]）。
	for _, id := range []string{"nes-embed", "tiny-out", "img-gen", "hy3"} {
		if _, ok := byID[id]; ok {
			t.Errorf("不应包含 %q", id)
		}
	}
	// v3 权威：glm-5.2 的 Efforts 取 v3 版（3 档）。
	if got := byID["glm-5.2"].Efforts; len(got) != 3 {
		t.Errorf("glm-5.2 efforts=%v want v3 三档", got)
	}
	// 远端缺 efforts 的模型按 CN 静态表兜底（hy4-preview → ['high']）。
	if got := byID["hy4-preview"].Efforts; len(got) != 1 || got[0] != "high" {
		t.Errorf("hy4-preview 静态兜底 efforts=%v", got)
	}
	// 试用横幅模型继承目标条目（展示名随目标条目；efforts 继承远端原始值，
	// 静态兜底按别名 id 查表不命中，保持空）。
	if byID["hy4-preview-f"].Display != "HY4 Preview" {
		t.Errorf("hy4-preview-f 应继承目标条目展示名，got %+v", byID["hy4-preview-f"])
	}
}

// idsOf 提取 ID 列表（错误信息用）。
func idsOf(infos []ModelInfo) []string {
	out := make([]string, 0, len(infos))
	for _, mi := range infos {
		out = append(out, mi.ID)
	}
	return out
}

// TestFetchModelsGlobal global 多路 UA 探测 + 企业端点家族。
func TestFetchModelsGlobal(t *testing.T) {
	var v3Hits, entV2, entConsole atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/v3/config", func(w http.ResponseWriter, r *http.Request) {
		v3Hits.Add(1)
		ua := r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		// 桌面 UA 下发独有模型；IDE/CLI 下发子集。
		if ua == defaultWorkBuddyUA(globalCred()) {
			_, _ = w.Write([]byte(`{"code":0,"data":{"models":[
			  {"id":"gpt-6-sol","name":"GPT-6 Sol","maxOutputTokens":65536},
			  {"id":"glm-5.2","name":"GLM-5.2","maxOutputTokens":16384,"reasoning":{"supportedEfforts":["high","xhigh"]}}
			]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"models":[
		  {"id":"o4-mini","name":"O4 Mini","maxOutputTokens":32768},
		  {"id":"deepseek-v4.1-flash","name":"DS Flash","maxOutputTokens":32768}
		]}}`))
	})
	mux.HandleFunc("/v2/enterprises/personal/models", func(w http.ResponseWriter, r *http.Request) {
		entV2.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"models":[{"id":"gpt-5.3-codex","name":"Codex","maxOutputTokens":65536}]}}`))
	})
	mux.HandleFunc("/console/enterprises/personal/models", func(w http.ResponseWriter, r *http.Request) {
		entConsole.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	out, err := c.FetchModels(context.Background(), globalCred())
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if v3Hits.Load() != 3 {
		t.Errorf("global 应三路 UA 探测 /v3/config，got %d", v3Hits.Load())
	}
	if entV2.Load() != 1 || entConsole.Load() != 0 {
		t.Errorf("企业家族应首选 /v2，got v2=%d console=%d", entV2.Load(), entConsole.Load())
	}
	byID := map[string]ModelInfo{}
	for _, mi := range out {
		byID[mi.ID] = mi
	}
	// 并集：桌面主路（gpt-6-sol/glm-5.2）+ IDE/CLI 补缺（o4-mini/deepseek）+ 企业补缺（gpt-5.3-codex）。
	for _, id := range []string{"gpt-6-sol", "glm-5.2", "o4-mini", "deepseek-v4.1-flash", "gpt-5.3-codex"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("并集缺 %q，got %v", id, idsOf(out))
		}
	}
	// global 静态兜底：gpt-5.3-codex 无远端 efforts → ['medium']。
	if got := byID["gpt-5.3-codex"].Efforts; len(got) != 1 || got[0] != "medium" {
		t.Errorf("gpt-5.3-codex 静态兜底=%v", got)
	}
}

// TestFetchModelsBothFail 两路全失败返回错误。
func TestFetchModelsBothFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := newTestClient(srv)
	if _, err := c.FetchModels(context.Background(), cnCred()); err == nil {
		t.Error("两路全失败应返回错误")
	}
}

// TestFallbackModels 静态兜底清单。
func TestFallbackModels(t *testing.T) {
	cn := FallbackModels(RealmCN)
	if len(cn) != len(cnEffortFallback) {
		t.Errorf("CN 兜底 %d 条 want %d", len(cn), len(cnEffortFallback))
	}
	found := false
	for _, mi := range cn {
		if mi.ID == "glm-5.2" {
			found = true
			if len(mi.Efforts) != 2 || mi.Efforts[0] != "high" {
				t.Errorf("glm-5.2 efforts=%v", mi.Efforts)
			}
		}
	}
	if !found {
		t.Error("CN 兜底缺 glm-5.2")
	}
	gl := FallbackModels(RealmGlobal)
	if len(gl) != len(GlobalModelNames) {
		t.Errorf("global 兜底 %d 条 want %d", len(gl), len(GlobalModelNames))
	}
	// deepseek-v4.1-flash 两域档位刻意不同：CN 三档 / global 单档。
	var cnDS, glDS []string
	for _, mi := range cn {
		if mi.ID == "deepseek-v4.1-flash" {
			cnDS = mi.Efforts
		}
	}
	for _, mi := range gl {
		if mi.ID == "deepseek-v4.1-flash" {
			glDS = mi.Efforts
		}
	}
	if len(cnDS) != 3 || len(glDS) != 1 || glDS[0] != "high" {
		t.Errorf("deepseek-v4.1-flash CN=%v global=%v（两域档位应不同）", cnDS, glDS)
	}
}
