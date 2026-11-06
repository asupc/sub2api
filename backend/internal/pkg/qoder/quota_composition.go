package qoder

// 额度构成归一化（qoder2api-hub a76dfa0 + c25db50 / issue #141 系列）。
//
// 背景：/api/v2/quota/usage 的额度散落在多个容器字段里（userQuota 套餐内、
// addOnQuota 资源包、dedicatedResourcePackages SOTA 专属、orgResourcePackage
// 共享池……），旧聚合只认前两种——INTL 实测 SOTA 在 dedicatedResourcePackages
//（数组，展示名在 displayLabels[].valueI18n），漏识别时合计少算 2000+。
//
// 策略（hub normalize_quota_payload 同款）：广度优先遍历响应 JSON，命中字段名
// 的 dict/数字归一成 bucket（QuotaBucket）；同一 kind 的多个对象都保留（资源包
// 可能有多份，各有有效期）；available=false 只展示不计入合计；合计排除 shared
// 桶（共享池单列，避免与个人额度重复计数）。

import (
	"strconv"
	"strings"
	"sync"
)

// QuotaBucket 额度构成的一个桶。
type QuotaBucket struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // plan | sota | pack | shared
	Remain    int    `json:"remain"`
	Used      int    `json:"used"`
	Size      int    `json:"size"`
	Available bool   `json:"available,omitempty"` // false = 过期/停用，只展示不计入
	ExpiresAt int64  `json:"expires_at,omitempty"`
}

// quotaBucketDefs 字段名匹配表（顺序即优先级，hub _QUOTA_BUCKETS 同款）。
var quotaBucketDefs = []struct {
	keys  []string // 小写字段名匹配
	kind  string
	label string
}{
	{[]string{"userquota", "planquota", "subscriptionquota", "basequota", "plan"}, "plan", "套餐内 Credits"},
	{[]string{"sota", "sotacredits", "sotaquota", "sotaspecial"}, "sota", "SOTA 专属积分"},
	{[]string{"addonquota", "resourcepackage", "resourcepackages", "addon", "package", "packages", "bonus"}, "pack", "资源包"},
	// INTL 实测：SOTA 专属积分在 dedicatedResourcePackages 里（数组），名字在
	// displayLabels[].valueI18n（zh-CN: "SOTA 专属积分"）。
	{[]string{"dedicatedresourcepackages", "dedicatedresourcepackage", "exclusivepackages", "sotapackages"}, "sota", "专属积分包"},
	{[]string{"orgresourcepackage", "sharedquota", "teamquota", "poolquota", "sharedcredits"}, "shared", "共享额度池"},
}

// bucketKindFor 字段名归到哪个桶（无匹配返回空）。
func bucketKindFor(key string) (kind, label string) {
	low := strings.ToLower(key)
	for _, def := range quotaBucketDefs {
		for _, k := range def.keys {
			if low == k {
				return def.kind, def.label
			}
		}
	}
	return "", ""
}

// quotaNumOf map 值转数字（缺失返回 ok=false）。
func quotaNumOf(m map[string]any, keys ...string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return v, true
		case string:
			if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}

// bucketFrom 把上游任意额度对象归一成 QuotaBucket（hub _bucket_from 同款）。
func bucketFrom(src any, kind, name string) *QuotaBucket {
	switch v := src.(type) {
	case float64:
		return &QuotaBucket{Name: name, Kind: kind, Remain: int(v), Used: 0, Size: int(v), Available: true}
	case map[string]any:
	default:
		return nil
	}
	m := src.(map[string]any)
	remain, okR := quotaNumOf(m, "remaining")
	if !okR {
		remain, okR = quotaNumOf(m, "remain")
	}
	if !okR {
		remain, okR = quotaNumOf(m, "balance")
	}
	used, okU := quotaNumOf(m, "used")
	size, okS := quotaNumOf(m, "total")
	if !okS {
		size, okS = quotaNumOf(m, "cap")
	}
	if !okS {
		size, okS = quotaNumOf(m, "size")
	}
	if !okS && okR && okU {
		size = remain + used
		okS = true
	}
	if !okR && okS && okU {
		if r := size - used; r > 0 || size > used {
			remain = r
			if remain < 0 {
				remain = 0
			}
		}
		okR = true
	}
	if !okR && !okS {
		return nil
	}
	// 上游把展示名放在 displayLabels[].valueI18n（zh-CN / en-US），优先用
	// 它——「SOTA 专属积分」这类活动包能显示成人话，而不是容器名。
	label := ""
	if dls, ok := m["displayLabels"].([]any); ok {
		for _, raw := range dls {
			dl, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if s, _ := dl["dimension"].(string); s != "title" {
				continue
			}
			i18n, _ := dl["valueI18n"].(map[string]any)
			if i18n != nil {
				if s, _ := i18n["zh-CN"].(string); strings.TrimSpace(s) != "" {
					label = strings.TrimSpace(s)
					break
				}
				if s, _ := i18n["en-US"].(string); strings.TrimSpace(s) != "" {
					label = strings.TrimSpace(s)
					break
				}
			}
			if s, _ := dl["value"].(string); strings.TrimSpace(s) != "" {
				label = strings.TrimSpace(s)
				break
			}
		}
	}
	if label == "" {
		label = strings.TrimSpace(str(m["name"]))
	}
	available := true
	if v, ok := m["available"]; ok {
		switch t := v.(type) {
		case bool:
			available = t
		case float64:
			available = t != 0
		case nil:
			available = false
		}
	}
	out := &QuotaBucket{
		Name:      label,
		Kind:      kind,
		Remain:    int(remain),
		Used:      int(used),
		Size:      int(size),
		Available: available,
	}
	if out.Name == "" {
		out.Name = name
	}
	for _, k := range [...]string{"expiresAt", "expireAt", "expires_at", "expiredAt", "validUntil", "dueAt"} {
		if v, ok := m[k]; ok {
			out.ExpiresAt = normalizeEpoch(v)
			break
		}
	}
	return out
}

// normalizeQuotaPayload 把任意区域的额度响应归一成构成列表（hub
// normalize_quota_payload 同款 BFS 遍历 + 去重）。一个可识别桶都没有时返回
// nil（调用方据此判「这个端点不适用」）。合计排除 shared 与 available=false。
func normalizeQuotaPayload(payload any) []QuotaBucket {
	var found []QuotaBucket
	type sig struct {
		kind      string
		size      int
		remain    int
		expiresAt int64
	}
	seen := map[sig]bool{}
	queue := []any{payload}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		switch v := node.(type) {
		case map[string]any:
			for key, val := range v {
				kind, label := bucketKindFor(key)
				if kind != "" {
					items := []any{val}
					if list, ok := val.([]any); ok {
						items = list
					}
					for idx, item := range items {
						switch item.(type) {
						case map[string]any, float64:
						default:
							continue
						}
						b := bucketFrom(item, kind, label)
						if b == nil {
							continue
						}
						if len(items) > 1 {
							b.Name = b.Name + " #" + strconv.Itoa(idx+1)
						}
						s := sig{b.Kind, b.Size, b.Remain, b.ExpiresAt}
						if !seen[s] {
							seen[s] = true
							found = append(found, *b)
						}
					}
				}
				switch val.(type) {
				case map[string]any, []any:
					queue = append(queue, val)
				}
			}
		case []any:
			for _, item := range v {
				switch item.(type) {
				case map[string]any, []any:
					queue = append(queue, item)
				}
			}
		}
	}
	return found
}

// quotaCompositionTotals 构成合计（排除 shared 与 available=false，hub 同款）。
func quotaCompositionTotals(buckets []QuotaBucket) (remain, used, size int) {
	for _, b := range buckets {
		if b.Kind == "shared" || !b.Available {
			continue
		}
		remain += b.Remain
		used += b.Used
		size += b.Size
	}
	return remain, used, size
}

// 最早到期（含 effective 时间，个人桶 only）——快照展示用。
func quotaEarliestExpiry(buckets []QuotaBucket) int64 {
	var best int64
	for _, b := range buckets {
		if b.Kind == "shared" || !b.Available || b.ExpiresAt <= 0 {
			continue
		}
		if best == 0 || b.ExpiresAt < best {
			best = b.ExpiresAt
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// 端点自动探测（hub probe_credits_endpoints 的运行时等价：404/405/410 时按
// 候选矩阵换路径，命中进程内记忆）。
// ---------------------------------------------------------------------------

// quotaPathCandidates 候选路径矩阵（hub CREDITS_PATHS 同款，首项为现有主路径）。
var quotaPathCandidates = []string{
	"/api/v2/quota/usage",
	"/api/v1/quota/usage",
	"/api/v2/quota",
	"/api/v2/user/quota",
	"/api/v2/me/quota",
	"/api/v2/credits/usage",
	"/api/v2/credits",
	"/api/v2/billing/quota",
	"/sash/api/v1/me/quota",
	"/sash/api/v1/me/credits",
	"/sash/api/v1/me/usage",
}

var (
	quotaPathMu    sync.Mutex
	quotaPathCache = map[string]string{} // realm -> 命中路径
)

// quotaUsagePath 取该区域当前应用的额度路径（缓存命中优先，缺省主路径）。
func quotaUsagePath(realm string) string {
	quotaPathMu.Lock()
	defer quotaPathMu.Unlock()
	if p, ok := quotaPathCache[realm]; ok {
		return p
	}
	return PathQuotaUsage
}

// rememberQuotaPath 记忆命中路径（同区域后续直连）。
func rememberQuotaPath(realm, path string) {
	quotaPathMu.Lock()
	defer quotaPathMu.Unlock()
	quotaPathCache[realm] = path
}

// nextQuotaPathCandidate 返回当前路径在候选矩阵中的下一个（无下一个返回
// false）。
func nextQuotaPathCandidate(cur string) (string, bool) {
	for i, p := range quotaPathCandidates {
		if p == cur && i+1 < len(quotaPathCandidates) {
			return quotaPathCandidates[i+1], true
		}
	}
	return "", false
}

// resetQuotaPathCacheForTest 清空路径缓存（测试用）。
func resetQuotaPathCacheForTest() {
	quotaPathMu.Lock()
	defer quotaPathMu.Unlock()
	quotaPathCache = map[string]string{}
}

// str 字符串值小工具。
func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
