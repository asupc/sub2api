package qoder

// maxout.go 二开新增：上游输出上限（max_tokens）学习缓存与出站钳制。
//
// 移植来源：qoder2api-hub commit 0426c33 的 issue #101 部分
// （qoder_proxy.py learn_max_output_limit / clamp_max_tokens / max_output_limit）。
//
// 背景：官方 catalog 不带 maxOutputTokens，客户端只能踩坑试出来——超过上限时上游回
//	400 invalid_parameter_error: Range of max_tokens should be [1, 131072]
// 这里从错误文本解析 N，按 (realm, model) 记住并缓存，之后出站请求直接 clamp
// （并在 /v1/models 条目暴露 max_output_tokens），客户端不必再猜。
//
// 与源的差异（刻意）：
//   - 源落盘 max-output-limits.json 以便跨进程共享；so 容器部署没有落盘约定
//     （参照 pkg/trae/catalog.go 的 24h 进程内缓存先例），只做进程内缓存，
//     TTL 同源 7 天。容器重启后需重新学一次，成本可接受。
//   - 出站钳制补一条源没有的归一：客户端显式给 0/负值时压到 1——上游 Range
//     恒为 [1, N]，非正数必 400；该归一不依赖是否学到上限（"始终生效"）。
//   - 上限未学到时不猜（不预设 131072），保持源"已知才钳制"语义。

import (
	"log"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	// maxOutLimitTTL 学到的上限缓存有效期（源 _MAX_OUT_LIMIT_TTL = 7 * 86400）。
	maxOutLimitTTL = 7 * 24 * time.Hour
)

// maxTokensRangeRe 上游 400 文本里的输出上限声明（源 _MAX_TOKENS_RANGE_RE）。
var maxTokensRangeRe = regexp.MustCompile(`(?i)Range of max_tokens should be \[1,\s*(\d+)\]`)

type maxOutLimitEntry struct {
	limit int
	at    time.Time
}

var (
	maxOutMu     sync.Mutex
	maxOutLimits = map[string]maxOutLimitEntry{} // "realm\x00model" -> 上限
)

// maxOutKey 缓存键：(realm, model)。
func maxOutKey(realm, model string) string {
	return realm + "\x00" + model
}

// LearnMaxOutputLimit 从上游 400 文本里学「该模型输出上限」，返回学到的 N
// （没学到返回 0）。同源：命中的 model 及其 ResolveUpstreamKey 别名一起记住，
// 之后两个名字都能查到。
func LearnMaxOutputLimit(detail, model, realm string) int {
	text := detail
	if text == "" {
		return 0
	}
	m := maxTokensRangeRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	limit, err := strconv.Atoi(m[1])
	if err != nil || limit <= 0 {
		return 0
	}
	if realm == "" {
		realm = "cn"
	}
	keys := []string{maxOutKey(realm, model)}
	if up := ResolveUpstreamKey(model, realm); up != "" && up != model {
		keys = append(keys, maxOutKey(realm, up))
	}
	maxOutMu.Lock()
	prev, ok := maxOutLimits[keys[0]]
	if ok && prev.limit == limit {
		maxOutMu.Unlock()
		return limit
	}
	now := time.Now()
	for _, k := range keys {
		maxOutLimits[k] = maxOutLimitEntry{limit: limit, at: now}
	}
	maxOutMu.Unlock()
	log.Printf("[qoder-maxout] WARN learned max_tokens limit for %s/%s = %d (clamping from now on)",
		realm, model, limit)
	return limit
}

// MaxOutputLimit 已学到的输出上限（含 catalog 自带的 max_output_tokens）；
// 未知返回 0——调用方不得猜默认值。model 可为上游 key 或 PublicID（官方显示名）。
// realm 为空时跨两区查找（/v1/models 条目场景拿不到区域）。
func MaxOutputLimit(model, realm string) int {
	// 名字归一：PublicID（显示名）→ 上游 key，学到的上限与 catalog 都按 key 存。
	keys := []string{model}
	if realm != "" {
		if up := ResolveUpstreamKey(model, realm); up != "" && up != model {
			keys = append(keys, up)
		}
	} else {
		for _, r := range []string{"cn", "intl"} {
			if up := ResolveUpstreamKey(model, r); up != "" && up != model && !slicesContains(keys, up) {
				keys = append(keys, up)
			}
		}
	}
	now := time.Now()
	maxOutMu.Lock()
	for _, cand := range keys {
		realms := []string{realm}
		if realm == "" {
			realms = []string{"cn", "intl"}
		}
		for _, r := range realms {
			if hit, ok := maxOutLimits[maxOutKey(r, cand)]; ok && now.Sub(hit.at) < maxOutLimitTTL {
				maxOutMu.Unlock()
				return hit.limit
			}
		}
	}
	maxOutMu.Unlock()
	// catalog 自带动态上限（maxOutputTokens / max_output_tokens / max_output）。
	realms := []string{realm}
	if realm == "" {
		realms = []string{"cn", "intl"}
	}
	for _, r := range realms {
		for _, cand := range keys {
			if v := findCatalogEntry(r, cand).MaxOutputTokens; v > 0 {
				return v
			}
		}
	}
	return 0
}

// ClampMaxTokens 出站 max_tokens 钳制（返回钳制后的值与是否发生钳制）：
//   - 0/负值 → 1（上游 Range 恒为 [1, N]，始终生效，不依赖是否学到上限）；
//   - 超已知上限 → 上限；
//   - 其余原样（未学到上限时不猜、不压）。
//
// 钳制命中打 Warn 日志（含 model 与两值），便于定位是哪个客户端在踩上限。
func ClampMaxTokens(value int64, model, realm string) (int64, bool) {
	out := value
	clamped := false
	if out <= 0 {
		out = 1
		clamped = true
	}
	if limit := MaxOutputLimit(model, realm); limit > 0 && out > int64(limit) {
		out = int64(limit)
		clamped = true
	}
	if clamped {
		log.Printf("[qoder-maxout] WARN max_tokens %d clamped to %d for %s/%s",
			value, out, realm, model)
	}
	return out, clamped
}
