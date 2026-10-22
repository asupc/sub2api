package trae

// catalog.go 二开新增：Trae 官方模型目录（batch_get_detail_param）重放，
// 移植自 buddy-switch-gateway src/trae/catalog.rs（issue #53 三版）。
//
// 客户端清单只有 context_window_size.default，max 恒为 null ⇒ 想声明
// 「这个模型在 Max Mode 下能装多少」没有数据源。客户端自己选模型走
// batch_get_detail_param，返回里每个模型带 context_window_tokens {dev, max}：
//   - dev：__dev 通道（网关所用通道）实际可用窗口；
//   - max：Max Mode 上限（可能不存在，各模型不同：glm-5.3 200k→1M、
//     mimo-v2.6-pro 200k→981504、Doubao-Seed-2.1-Turbo 上限仍是 256k）。
//
// 判定规则：目录里 max > dev ⇒ 该模型支持 Max Mode（出站注入 is_max_mode，
// 清单声明 max）；否则声明 dev。目录拉不到 ⇒ 回落默认窗口，**不猜数字**——
// 任何「统一 1M」的假设都会虚报窗口。
//
// 缓存取舍：进程内 24h TTL（源实现落盘 ~/.buddy-switch/…，so 容器部署没有
// 对应数据目录约定，重启重拉一次的成本可接受；拉取失败静默沿用旧缓存）。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)



// CatalogEndpoint 上游目录接口（客户端模型选择器用的那一支）。
const CatalogEndpoint = "https://api5-normal.mchost.guru/api/ide/v1/batch_get_detail_param"

// catalogEndpoint 请求目标（包级变量：测试注入 httptest server）。
var catalogEndpoint = CatalogEndpoint

// catalogTTL 目录缓存有效期（24 小时）。
const catalogTTL = 24 * time.Hour

// CatalogEntry 单个模型的上下文口径（只留声明所需的两个数）。
type CatalogEntry struct {
	// ID 上游 config_name（= 客户端清单里的模型名）。
	ID string `json:"id"`
	// Dev __dev 通道实际窗口；nil = 目录未给。
	Dev *int64 `json:"dev,omitempty"`
	// Max Max Mode 上限；nil = 不支持 / 目录未给。
	Max *int64 `json:"max,omitempty"`
}

// ModelCatalog 一个区域的目录快照。
type ModelCatalog struct {
	FetchedAt time.Time      `json:"fetched_at"`
	Entries   []CatalogEntry `json:"entries"`
}

var (
	catalogMu   sync.Mutex
	catalogMem  = map[string]*ModelCatalog{} // region → 快照
	catalogPull = map[string]bool{}          // 正在同步的区域（单飞）
)

// ContextFor 某个模型的 (dev, max)（同步，供热路径出站注入与清单构造）。
// 目录尚未同步或模型不在目录 ⇒ ok=false（调用方回落默认窗口，不猜数字）。
func ContextFor(region Region, model string) (dev, max *int64, ok bool) {
	catalogMu.Lock()
	defer catalogMu.Unlock()
	cat, has := catalogMem[string(region)]
	if !has {
		return nil, nil, false
	}
	low := strings.ToLower(strings.TrimSpace(model))
	for i := range cat.Entries {
		if strings.ToLower(cat.Entries[i].ID) == low {
			return cat.Entries[i].Dev, cat.Entries[i].Max, true
		}
	}
	return nil, nil, false
}

// MaxModeWindow 该模型的 Max Mode 口径 (dev, max)：目录命中且 max > dev 才
// 返回 true（支持 Max Mode）。
func MaxModeWindow(region Region, model string) (int64, int64, bool) {
	dev, max, ok := ContextFor(region, model)
	if !ok || dev == nil || max == nil || *max <= *dev {
		return 0, 0, false
	}
	return *dev, *max, true
}

// EnsureCatalog 目录过期才联网刷新（静默失败：沿用旧缓存或维持无目录）。
// jwt 为一个可用账号的 Cloud-IDE-JWT；region 为该账号所属区域。
func EnsureCatalog(ctx context.Context, httpClient *http.Client, region Region, jwt string) {
	catalogMu.Lock()
	if cat, has := catalogMem[string(region)]; has && time.Since(cat.FetchedAt) < catalogTTL {
		catalogMu.Unlock()
		return
	}
	if catalogPull[string(region)] {
		catalogMu.Unlock()
		return
	}
	catalogPull[string(region)] = true
	catalogMu.Unlock()

	defer func() {
		catalogMu.Lock()
		delete(catalogPull, string(region))
		catalogMu.Unlock()
	}()

	cat, err := SyncModelCatalog(ctx, httpClient, jwt)
	if err != nil {
		return
	}
	catalogMu.Lock()
	catalogMem[string(region)] = cat
	catalogMu.Unlock()
}

// SyncModelCatalog 发一次目录请求并解析（失败返回错误，不触碰缓存）。
func SyncModelCatalog(ctx context.Context, httpClient *http.Client, jwt string) (*ModelCatalog, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	jwt = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(jwt), "Cloud-IDE-JWT "))
	if jwt == "" {
		return nil, fmt.Errorf("trae catalog: 缺少可用 JWT")
	}
	body := map[string]any{
		"functions": []string{"builder", "builder_v3", "chat_v3", "code_reviewer",
			"code_review_summary", "refactor", "solo_agent"},
		"agent_type":          "solo_agent",
		"current_config_info": map[string]any{"config_name": "", "is_custom_model": false},
		"mode_type":           0,
		"access_type":         0,
		"ab_force_vids":       "",
		"ab_autotest_advanced_mode": 0,
		"show_custom_model":         true,
	}
	blob, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, catalogEndpoint, strings.NewReader(string(blob)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Request-Traffic-Type", "prod")
	req.Header.Set("User-Agent", "TraeClient/TTNet")
	req.Header.Set("x-app-id", ChatAppID)
	req.Header.Set("x-app-version", "default")
	req.Header.Set("x-app-version-code", IDEVersionCode)
	req.Header.Set("x-bridge-transport", "aha")
	req.Header.Set("x-device-brand", "CREFG-XX")
	req.Header.Set("x-device-cpu", "Intel")
	req.Header.Set("x-device-id", "0")
	req.Header.Set("x-device-type", "windows")
	req.Header.Set("x-ide-token", jwt)
	req.Header.Set("x-ide-version", IDEVersion)
	req.Header.Set("x-ide-version-code", IDEVersionCode)
	req.Header.Set("x-ide-version-type", "stable")
	req.Header.Set("x-lgw-req-sdk-type", "3")
	req.Header.Set("x-machine-id", "0")
	req.Header.Set("x-os-version", "Windows 11 Home China")
	req.Header.Set("package-type", "stable_cn")
	req.Header.Set("x-lscbd-aid", "787976")
	req.Header.Set("x-lscbd-platform", "windows")
	req.Header.Set("app-version", IDEVersion)
	req.Header.Set("x-ss-dp", "787976")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("trae catalog: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("trae catalog: HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("trae catalog: %w", err)
	}
	return ParseModelCatalog(raw)
}

// ParseModelCatalog 解析 batch_get_detail_param 响应。
//
// 每个模型会在多个 function 视图里重复出现且数值可能不同（chat_v3 的 dev 是
// 116k、solo_agent 是 200k）——只关心「这个模型最多能装多少」⇒ 逐字段取
// **最大值**，避免被某个子系统视图（summary / fast_apply 等）拉低。
// 自定义 / 内部条目（custom_ 前缀、含空格）不进对外清单。
func ParseModelCatalog(raw []byte) (*ModelCatalog, error) {
	var root struct {
		FunctionConfigs []struct {
			ConfigInfoList []struct {
				ConfigName          string `json:"config_name"`
				ContextWindowTokens struct {
					Dev json.Number `json:"dev"`
					Max json.Number `json:"max"`
				} `json:"context_window_tokens"`
			} `json:"config_info_list"`
		} `json:"function_configs"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("trae catalog: 解析失败: %w", err)
	}
	merged := map[string]*CatalogEntry{}
	for _, cfg := range root.FunctionConfigs {
		for _, item := range cfg.ConfigInfoList {
			id := strings.TrimSpace(item.ConfigName)
			if id == "" || strings.HasPrefix(id, "custom_") || strings.Contains(id, " ") {
				continue
			}
			dev := numberPtr(item.ContextWindowTokens.Dev)
			max := numberPtr(item.ContextWindowTokens.Max)
			if dev == nil && max == nil {
				continue
			}
			key := strings.ToLower(id)
			entry, has := merged[key]
			if !has {
				entry = &CatalogEntry{ID: id}
				merged[key] = entry
			}
			entry.Dev = pickLarger(entry.Dev, dev)
			entry.Max = pickLarger(entry.Max, max)
		}
	}
	if len(merged) == 0 {
		return nil, fmt.Errorf("trae catalog: 官方目录没有返回任何模型")
	}
	out := &ModelCatalog{FetchedAt: time.Now()}
	for _, entry := range merged {
		out.Entries = append(out.Entries, *entry)
	}
	return out, nil
}

// numberPtr JSON 数字（或数字字符串）转 int64 指针；空 / 非数字返回 nil。
func numberPtr(n json.Number) *int64 {
	s := strings.TrimSpace(string(n))
	if s == "" || s == "null" {
		return nil
	}
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return nil
	}
	return &v
}

// pickLarger 两个可选值取较大者（任一为 nil 时取另一者）。
func pickLarger(current, next *int64) *int64 {
	if current == nil {
		return next
	}
	if next == nil {
		return current
	}
	if *next > *current {
		return next
	}
	return current
}

// ResetModelCatalogForTest 清空目录缓存（测试隔离用）。
func ResetModelCatalogForTest() {
	catalogMu.Lock()
	catalogMem = map[string]*ModelCatalog{}
	catalogPull = map[string]bool{}
	catalogMu.Unlock()
}
