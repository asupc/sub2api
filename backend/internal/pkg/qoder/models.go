package qoder

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// 静态模型目录快照（qoder_catalog_intl.json / qoder_catalog_cn.json）：
// 由官方客户端模型目录缓存（QMC 解密）逐字段原样导出，双区清单不同。
// 运行时优先级：动态 /algo/api/v2/model/list > 本静态快照。
//
//go:embed assets/qoder_catalog_cn.json
var catalogCNJSON []byte

//go:embed assets/qoder_catalog_intl.json
var catalogIntlJSON []byte

// ThinkingConfig 思考开关与档位（目录 thinking_config 字段子集）。
type ThinkingConfig struct {
	Enabled *ThinkingEnabled `json:"enabled"`
}

// ThinkingEnabled 档位表 {low..xhigh, is_default}。
type ThinkingEnabled struct {
	Efforts map[string]EffortCfg `json:"efforts"`
}

// EffortCfg 单档位配置（只用 is_default 标注）。
type EffortCfg struct {
	IsDefault bool `json:"is_default"`
}

// CatalogEntry 目录条目（本包消费的字段子集；原始条目其余字段不裁剪语义，
// 动态刷新时按「有值字段增量覆盖」合并，见 mergeCatalog）。
type CatalogEntry struct {
	Key            string          `json:"key"`
	DisplayName    string          `json:"display_name"`
	Enable         bool            `json:"enable"`
	MaxInputTokens int             `json:"max_input_tokens"`
	IsVL           bool            `json:"is_vl"`
	IsReasoning    bool            `json:"is_reasoning"`
	Thinking       *ThinkingConfig `json:"thinking_config"`
	// MaxOutputTokens 输出上限（issue #101）：官方 catalog 通常没有此字段，
	// 仅动态接口偶发带 maxOutputTokens / max_output_tokens 时才有值；0 = 未知，
	// 不得猜默认值（已知才钳制，见 maxout.go）。
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
	// IsFree / 峰谷价（官方 promotion）：运营展示用元数据，不参与网关计费
	//（so 计费走自身价目表）。动态目录刷新时覆盖，静态快照缺省。
	IsFree            *bool    `json:"is_free,omitempty"`
	PriceFactor       *float64 `json:"price_factor,omitempty"`
	PriceFactorPeak   *float64 `json:"price_factor_peak,omitempty"`
	PriceFactorValley *float64 `json:"price_factor_valley,omitempty"`
}

// DisplayID 对外展示 id：「上游 key (官方模型名)」（qoder_catalog.py L1573-1579）。
func (e CatalogEntry) DisplayID() string {
	if e.DisplayName != "" && e.DisplayName != e.Key {
		return e.Key + " (" + e.DisplayName + ")"
	}
	return e.Key
}

// PublicID 对外模型清单 id：优先官方显示名（display_name），缺失或与 key
// 相同时回落上游 key（动态目录新增条目可能缺 display_name）。对齐 qoder2api
// /v1/models 的人类可读 ID 口径（model_entry L1994-2002）；区别于 DisplayID
// 的「key (Name)」展示形态。以目录 display_name 为准，不用 officialLabels
// （zh.label 快照与目录有出入，如 mmodel 目录=MiniMax-M2.7 / label=M3）。
func (e CatalogEntry) PublicID() string {
	if name := strings.TrimSpace(e.DisplayName); name != "" && name != e.Key {
		return name
	}
	return e.Key
}

// EffectiveMaxOutputTokens 该条目对外声明的输出上限（issue #101）：catalog 自带
// 或已学到的；未知返回 0——不猜、不输出（源 max_output_limit 语义：已知才报）。
func (e CatalogEntry) EffectiveMaxOutputTokens(realm string) int {
	if e.MaxOutputTokens > 0 {
		return e.MaxOutputTokens
	}
	return MaxOutputLimit(e.Key, realm)
}

// ModelEntryOutputFields /v1/models 条目补充字段（issue #101，源 model_entry
// L3003-3010）：已知输出上限时输出 max_output_tokens 与 max_completion_tokens
// （客户端两个字段名都认）；未学到则不返回该字段，不猜默认值。
func ModelEntryOutputFields(model, realm string) map[string]any {
	limit := MaxOutputLimit(model, realm)
	if limit <= 0 {
		return nil
	}
	return map[string]any{
		"max_output_tokens":     limit,
		"max_completion_tokens": limit,
	}
}

// nonChatFilters chat 场景过滤（qoder_proxy.py L1750-1752）。
var (
	nonChatModels   = map[string]bool{"lite": true}
	nonChatPrefixes = []string{"codewise-", "completion-"}
	nonChatSuffixes = []string{"-image-alpha", "-image-alpha-edit", "-taco-completion"}
)

// isChatModel 报告 key 是否属于 chat 场景（qoder_proxy.py L1755-1764）。
func isChatModel(key string) bool {
	if key == "" || nonChatModels[key] {
		return false
	}
	for _, p := range nonChatPrefixes {
		if strings.HasPrefix(key, p) {
			return false
		}
	}
	for _, s := range nonChatSuffixes {
		if strings.HasSuffix(key, s) {
			return false
		}
	}
	return true
}

type catalogSnapshot struct {
	once    sync.Once
	entries []CatalogEntry
	err     error
}

var (
	staticCN   catalogSnapshot
	staticIntl catalogSnapshot
)

// StaticModels 该区域的静态快照清单（realm 非 "intl" 时按 "cn" 处理）。
func StaticModels(realm string) ([]CatalogEntry, error) {
	if realm == "intl" {
		staticIntl.once.Do(func() {
			staticIntl.entries, staticIntl.err = parseCatalog(catalogIntlJSON)
		})
		return staticIntl.entries, staticIntl.err
	}
	staticCN.once.Do(func() {
		staticCN.entries, staticCN.err = parseCatalog(catalogCNJSON)
	})
	return staticCN.entries, staticCN.err
}

func parseCatalog(data []byte) ([]CatalogEntry, error) {
	var entries []CatalogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("qoder: parse model catalog: %w", err)
	}
	return entries, nil
}

// modelAliases 客户端友好名 / 官方显示名 -> 上游 model key
// （qoder_catalog.py L1591-1619 MODEL_ALIASES 直译）。
var modelAliases = map[string]string{
	// Qwen
	"auto": "auto", "qoder-auto": "auto",
	"qwen3.8-max":         "qmodel_38max",
	"qwen3.8-max-preview": "qmodel_38max",
	"qmodel_preview":      "qmodel_38max", // 旧 key -> 新 key
	"qwen3.8-flash":       "qfmodel",
	"qwen3.7-max":         "qmodel_latest",
	"qwen3.7-plus":        "qmodel",
	"qwen3.7-flash":       "q37fmodel",
	"qwen3.6-flash":       "q36fmodel",
	"deepseek-v4-pro":     "dmodel",
	"deepseek-v4-flash":   "dfmodel",
	"deepseek-flash":      "dfmodel",
	"glm-5.3":             "gmodel",
	"glm-5.3-flash":       "gfmodel",
	"glm-5.2":             "gm51model",
	"kimi-k3":             "kmodel_latest",
	"kimi-k2.8-preview":   "kmodel",
	"kimi-k2.7":           "kmodel",
	"kimi-k2.7-code":      "kmodel",
	"minimax-m3":          "mmodel",
	"minimax-m2.7":        "mmodel",
	"minimax-m2.5":        "mmodel",
}

// officialLabels 官方本地化显示名（zh.label，如 Ultimate -> 极致），
// 逐字取自 qoder_catalog.py L1713 _EMBEDDED_MODEL_TEXT 的 labels 快照。
var officialLabels = map[string]string{
	"auto": "Auto", "lite": "轻量", "cmodel": "Cantus", "dmodel": "DeepSeek-V4-Pro",
	"gmodel": "GLM-5.3", "kmodel": "Kimi-K2.7-Code", "mmodel": "MiniMax-M3",
	"qmodel": "Qwen3.7-Plus", "dfmodel": "DeepSeek-V4-Flash", "gfmodel": "GLM-5.3-Flash",
	"qfmodel": "Qwen3.8-Flash", "ultimate": "极致", "efficient": "经济",
	"performance": "性能", "qmodel_38max": "Qwen3.8-Max", "kmodel_latest": "Kimi-K3",
	"qmodel_latest": "Qwen3.7-Max",
}

// resolveRealmKeySet 该区域全部上游 key 集合。
func resolveRealmKeySet(realm string) map[string]bool {
	entries, err := StaticModels(realm)
	if err != nil {
		return nil
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		out[e.Key] = true
	}
	return out
}

// displayIndex 官方显示名（小写）-> key（qoder_catalog.py L1622-1633）。
func displayIndex(realm string) map[string]string {
	entries, err := StaticModels(realm)
	if err != nil {
		return nil
	}
	idx := map[string]string{}
	for _, e := range entries {
		name := strings.ToLower(strings.TrimSpace(e.DisplayName))
		if name != "" {
			if _, ok := idx[name]; !ok {
				idx[name] = e.Key
			}
		}
	}
	return idx
}

// ResolveUpstreamKey 把客户端请求的模型名解析为上游 model key
// （qoder_catalog.py L1636-1689）。接受：官方 key、展示 id「key (Name)」、
// 人类可读别名、官方显示名（不分大小写）、官方本地化名（zh.label）、
// "vendor/model" 形态的尾段。realm 为空时不做区域 key 集合校验。
func ResolveUpstreamKey(model, realm string) string {
	if model == "" {
		return "auto"
	}
	m := strings.TrimSpace(model)
	var keys map[string]bool
	if realm != "" {
		keys = resolveRealmKeySet(realm)
	}

	// 展示 id 形式: "qmodel_38max (Qwen3.8-Max)"
	mKey := m
	if i := strings.LastIndex(m, " ("); i >= 0 && strings.HasSuffix(m, ")") {
		if head := strings.TrimSpace(m[:i]); head != "" {
			mKey = head
		}
	}

	if keys != nil && keys[mKey] {
		return mKey
	}
	low := strings.ToLower(mKey)
	if v, ok := modelAliases[mKey]; ok {
		return v
	}
	if v, ok := modelAliases[low]; ok {
		return v
	}
	// 官方显示名（display_name / 括号内名）
	dispIdx := displayIndex("cn")
	if intlIdx := displayIndex("intl"); intlIdx != nil {
		for k, v := range intlIdx {
			if _, exists := dispIdx[k]; !exists {
				dispIdx[k] = v
			}
		}
	}
	tail := low
	if i := strings.LastIndex(low, " ("); i >= 0 {
		tail = strings.TrimSuffix(low[i+2:], ")")
	}
	if v, ok := dispIdx[tail]; ok {
		return v
	}
	if v, ok := dispIdx[low]; ok {
		return v
	}
	// 官方本地化名（zh.label）
	target := low
	if tail != low {
		target = tail
	}
	for lk, lv := range officialLabels {
		if lk != "" && strings.ToLower(strings.TrimSpace(lv)) == target {
			if keys == nil || keys[lk] ||
				resolveRealmKeySet("cn")[lk] || resolveRealmKeySet("intl")[lk] {
				return lk
			}
		}
	}
	if keys != nil && keys[mKey] {
		return mKey // 官方 key 但非本区域（跨区由调用方校验拦截）
	}
	if i := strings.LastIndex(low, "/"); i >= 0 {
		part := low[i+1:]
		if v, ok := modelAliases[part]; ok {
			return v
		}
		if v, ok := dispIdx[part]; ok {
			return v
		}
	}
	return mKey
}

// effortRank 思考档位的官方词表顺序（qoder_proxy.py L2393-2394）。
var effortRank = map[string]int{"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "max": 6}

var effortOffValues = map[string]bool{
	"none": true, "off": true, "disabled": true, "disable": true,
	"false": true, "0": true, "no": true,
}

// rankedEfforts 按官方词表顺序返回模型支持的档位。
func rankedEfforts(t *ThinkingConfig) []string {
	if t == nil || t.Enabled == nil {
		return nil
	}
	out := make([]string, 0, len(t.Enabled.Efforts))
	for e := range t.Enabled.Efforts {
		if _, ok := effortRank[e]; ok {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return effortRank[out[i]] < effortRank[out[j]] })
	return out
}

// SupportedEfforts 该模型官方支持的思考档位；空列表表示只有"开/关"。
func (e CatalogEntry) SupportedEfforts() []string {
	return rankedEfforts(e.Thinking)
}

// DefaultEffort 该模型的默认思考档位（is_default 标注项），无则取中间档。
func (e CatalogEntry) DefaultEffort() string {
	if e.Thinking != nil && e.Thinking.Enabled != nil {
		for lvl, cfg := range e.Thinking.Enabled.Efforts {
			if cfg.IsDefault {
				return lvl
			}
		}
	}
	sup := e.SupportedEfforts()
	if len(sup) > 0 {
		return sup[len(sup)/2]
	}
	return ""
}

// NormalizeReasoningEffort 把请求里的思考档位归一化到该模型官方支持的档位
// 集合（qoder_proxy.py L2421-2464；上游对不支持的档位不报错、直接忽略）。
// 返回 (value, note)：value 为空串表示不下发 reasoning_effort。
func (e CatalogEntry) NormalizeReasoningEffort(effort string) (string, string) {
	v := strings.ToLower(strings.TrimSpace(effort))
	if v == "" {
		return "", ""
	}
	if effortOffValues[v] {
		return "none", ""
	}
	if e.Thinking == nil {
		// 目录里没有该模型的思考配置（路由器/未知模型）：不猜测，原样透传
		return v, ""
	}
	sup := rankedEfforts(e.Thinking)
	if len(sup) == 0 {
		return "", "dropped (model has no effort levels; use none to disable)"
	}
	for _, s := range sup {
		if s == v {
			return v, ""
		}
	}
	def := e.DefaultEffort()
	if _, ok := effortRank[v]; !ok {
		if def == "" {
			def = sup[0]
		}
		return def, "unknown level -> " + def
	}
	want := effortRank[v]
	defRank := want
	if r, ok := effortRank[def]; ok {
		defRank = r
	}
	// 同距时偏向模型默认档（对齐 Python 元组排序 key：(档位距, 默认档距)）
	best, bestD1, bestD2 := sup[0], 1<<30, 1<<30
	for _, cand := range sup {
		d1 := effortRank[cand] - want
		if d1 < 0 {
			d1 = -d1
		}
		d2 := effortRank[cand] - defRank
		if d2 < 0 {
			d2 = -d2
		}
		if d1 < bestD1 || (d1 == bestD1 && d2 < bestD2) {
			best, bestD1, bestD2 = cand, d1, d2
		}
	}
	return best, "unsupported level " + v + " -> " + best
}

// modelsCacheTTL 动态清单进程级缓存 TTL（qoder_proxy.py L1871，300s，与 token 无关）。
const modelsCacheTTL = 300 * time.Second

type modelsCacheEntry struct {
	at      time.Time
	entries []CatalogEntry
}

var (
	modelsCacheMu sync.Mutex
	modelsCache   = map[string]modelsCacheEntry{}
)

// FetchModels 动态拉取模型清单并合并静态快照（read_dynamic_models L1883 +
// merge_catalog L1771 + fetch_models L1867）：
//
//   - GET {gateway}/algo/api/v2/model/list?Encode=1，**GET 需携带与签名一致的
//     qoder_encode("{}") 作为 body**（服务端校验签名与 body 一致，裸 GET 403）；
//   - 主机按官方候选顺序尝试（intl api1 → api2 → api3，签名只覆盖 path，
//     切换主机不影响签名）；
//   - 结果进程级 TTL 缓存（按 realm，token 无关）；动态失败时回退静态快照。
func (c *Client) FetchModels(ctx context.Context, realm string, sess *CosySession) ([]CatalogEntry, error) {
	modelsCacheMu.Lock()
	if hit, ok := modelsCache[realm]; ok && time.Since(hit.at) < modelsCacheTTL {
		entries := hit.entries
		modelsCacheMu.Unlock()
		return entries, nil
	}
	modelsCacheMu.Unlock()

	dynamic, err := c.fetchDynamicModels(ctx, realm, sess)
	if err != nil {
		// 动态失败：回退静态快照全量（不写缓存，下次仍尝试动态）
		static, serr := StaticModels(realm)
		if serr != nil {
			return nil, fmt.Errorf("qoder: dynamic model list: %v; static catalog: %w", err, serr)
		}
		return static, nil
	}
	merged := mergeCatalog(staticOrdered(realm), dynamic)

	modelsCacheMu.Lock()
	modelsCache[realm] = modelsCacheEntry{at: time.Now(), entries: merged}
	modelsCacheMu.Unlock()
	return merged, nil
}

// staticOrdered 静态快照（UI 顺序即文件顺序），供 merge 打底。
func staticOrdered(realm string) []CatalogEntry {
	entries, err := StaticModels(realm)
	if err != nil {
		return nil
	}
	return entries
}

// fetchDynamicModels COSY 签名拉取 /algo/api/v2/model/list（chat scene）。
// 返回动态接口的原始条目列表（未合并）。
func (c *Client) fetchDynamicModels(ctx context.Context, realm string, sess *CosySession) ([]map[string]any, error) {
	if sess == nil {
		return nil, fmt.Errorf("qoder: fetch models requires cosy session")
	}
	signBody := QoderEncode([]byte("{}"))
	var payload any
	var lastErr error
	for _, host := range GatewayCandidates(realm) {
		rawURL := host + ModelsPath
		headers := sess.Headers(signBody, rawURL, "", false, "application/json")
		headers.Set("User-Agent", ClientUA)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, strings.NewReader(signBody))
		if err != nil {
			return nil, err
		}
		req.Header = headers
		resp, err := c.http().Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, clip(string(body), 200))
			continue
		}
		// 源实现（read_dynamic_models）对响应直接 json.loads：响应为明文 JSON。
		// 这里再做一层防御：若解析结果是字符串（编码载荷），qoder_decode 后重解。
		if err := json.Unmarshal(body, &payload); err != nil {
			lastErr = err
			continue
		}
		if s, ok := payload.(string); ok {
			decoded, derr := QoderDecode(s)
			if derr != nil {
				lastErr = derr
				continue
			}
			if err := json.Unmarshal(decoded, &payload); err != nil {
				lastErr = err
				continue
			}
		}
		break
	}
	pm, ok := payload.(map[string]any)
	if !ok {
		if lastErr == nil {
			lastErr = fmt.Errorf("qoder: no gateway candidate succeeded")
		}
		return nil, lastErr
	}
	chat, _ := pm["chat"].([]any)
	out := make([]map[string]any, 0, len(chat))
	for _, item := range chat {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if key, _ := m["key"].(string); key == "" {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// dynamicEntryFields 指针字段区分「JSON 里缺失」与「显式 false」，
// 保证动态条目只有真实出现的字段才覆盖静态打底值。
type dynamicEntryFields struct {
	Key            *string         `json:"key"`
	DisplayName    *string         `json:"display_name"`
	Enable         *bool           `json:"enable"`
	MaxInputTokens *int            `json:"max_input_tokens"`
	IsVL           *bool           `json:"is_vl"`
	IsReasoning    *bool           `json:"is_reasoning"`
	Thinking       *ThinkingConfig `json:"thinking_config"`
	IsFree         *bool           `json:"is_free"`
	PriceFactor    *float64        `json:"price_factor"`
	// MaxOutputTokens 动态接口偶发的输出上限（issue #101，三种拼写兼容）。
	MaxOutputTokens *int `json:"max_output_tokens"`
	// MaxOutputTokensCamel 官方 camelCase 拼写（maxOutputTokens）。
	MaxOutputTokensCamel *int `json:"maxOutputTokens"`
	// MaxOutput 源兼容的第三种拼写（max_output）。
	MaxOutput *int `json:"max_output"`
	// OriginalPriceFactor 峰值回落口径之一（promotion 缺失时）。
	OriginalPriceFactor *float64         `json:"original_price_factor"`
	Promotion           *promotionFields `json:"promotion"`
}

// promotionFields 官方 promotion（峰谷价窗口与折扣；model_entry 峰谷价同款）。
type promotionFields struct {
	BeforePriceFactor *float64 `json:"before_promotion_price_factor"`
	DiscountFactor    *float64 `json:"discount_factor"`
}

// valleyPrice 低谷价 = 峰值 × 折扣（官方 promotion 的定义，与快照抓取时刻
// 无关）；缺折扣或折扣非法（<=0 / >1）时回落当前价——旧实现直接把当前
// price_factor 当低谷价，快照在非低谷时段抓取时报出的低谷价会等于峰价。
func valleyPrice(peak *float64, promo *promotionFields, now *float64) *float64 {
	if peak == nil {
		return nil
	}
	if promo != nil && promo.DiscountFactor != nil {
		if d := *promo.DiscountFactor; d > 0 && d <= 1 {
			v := math.Round(*peak*d*10000) / 10000
			return &v
		}
	}
	if now != nil {
		return now
	}
	return nil
}

// dropNulls 递归剔除 null 值字段（动态接口对部分条目返回 null，如
// context_config/thinking_config；静默 null 会把静态快照里的真实值抹掉）。
func dropNulls(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		if sub, ok := v.(map[string]any); ok {
			out[k] = dropNulls(sub)
			continue
		}
		out[k] = v
	}
	return out
}

// mergeCatalog 静态目录打底（补元数据），清单以动态接口为准
// （qoder_proxy.py L1771-1820）：动态返回 15 条就不额外塞静态独有的条目；
// 动态为空时回退静态全量；enable 等状态不做裁剪。输出按静态快照顺序，
// 动态独有条目追加在尾部。
func mergeCatalog(static []CatalogEntry, dynamic []map[string]any) []CatalogEntry {
	merged := map[string]CatalogEntry{}
	order := make([]string, 0, len(static))
	for _, e := range static {
		if isChatModel(e.Key) {
			merged[e.Key] = e
			order = append(order, e.Key)
		}
	}
	var primaryKeys map[string]bool
	if len(dynamic) > 0 {
		primaryKeys = map[string]bool{}
	}
	for _, d := range dynamic {
		key, _ := d["key"].(string)
		if !isChatModel(key) {
			continue
		}
		primaryKeys[key] = true
		base, ok := merged[key]
		if !ok {
			merged[key] = overlayEntry(CatalogEntry{Key: key}, d)
			order = append(order, key)
			continue
		}
		merged[key] = overlayEntry(base, d)
	}
	out := make([]CatalogEntry, 0, len(order))
	for _, key := range order {
		if primaryKeys != nil && !primaryKeys[key] {
			continue // 静态独有、官方此刻未列出的条目不外塞
		}
		out = append(out, merged[key])
	}
	return out
}

// overlayEntry 用动态条目的有值字段增量覆盖静态条目（null 值剔除后，
// 仅真实出现的字段生效）。
func overlayEntry(base CatalogEntry, raw map[string]any) CatalogEntry {
	b, _ := json.Marshal(dropNulls(raw))
	var f dynamicEntryFields
	_ = json.Unmarshal(b, &f)
	out := base
	if f.Key != nil && *f.Key != "" {
		out.Key = *f.Key
	}
	if f.DisplayName != nil && *f.DisplayName != "" {
		out.DisplayName = *f.DisplayName
	}
	if f.Enable != nil {
		out.Enable = *f.Enable
	}
	if f.MaxInputTokens != nil {
		out.MaxInputTokens = *f.MaxInputTokens
	}
	// 输出上限（issue #101）：动态接口三种拼写取第一个有值的（max_output_tokens /
	// maxOutputTokens / max_output），对齐源 catalog_meta_for 的取值顺序。
	for _, v := range []*int{f.MaxOutputTokens, f.MaxOutputTokensCamel, f.MaxOutput} {
		if v != nil && *v > 0 {
			out.MaxOutputTokens = *v
			break
		}
	}
	if f.IsVL != nil {
		out.IsVL = *f.IsVL
	}
	if f.IsReasoning != nil {
		out.IsReasoning = *f.IsReasoning
	}
	if f.Thinking != nil {
		out.Thinking = f.Thinking
	}
	if f.IsFree != nil {
		out.IsFree = f.IsFree
	}
	if f.PriceFactor != nil {
		out.PriceFactor = f.PriceFactor
	}
	// 峰谷价：peak = promotion.before_promotion_price_factor（缺失回落
	// original_price_factor）；valley = peak × discount。
	peak := f.OriginalPriceFactor
	if f.Promotion != nil && f.Promotion.BeforePriceFactor != nil {
		peak = f.Promotion.BeforePriceFactor
	}
	if peak != nil {
		out.PriceFactorPeak = peak
		if v := valleyPrice(peak, f.Promotion, f.PriceFactor); v != nil {
			out.PriceFactorValley = v
		}
	}
	return out
}

// InvalidateModelsCache 清空动态清单缓存（测试与强制刷新用）。
func InvalidateModelsCache() {
	modelsCacheMu.Lock()
	modelsCache = map[string]modelsCacheEntry{}
	modelsCacheMu.Unlock()
}
