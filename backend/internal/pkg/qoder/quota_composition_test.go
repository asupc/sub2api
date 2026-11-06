//go:build unit

package qoder

// 额度构成归一化（qoder2api-hub a76dfa0 + c25db50 / issue #141 系列）：
// SOTA 在 dedicatedResourcePackages、displayLabels 取名、available=false
// 只展示不计入、合计钉住官方实测数字（剩余 3761 / 总量 3800 / 已用 39）。

import (
	"encoding/json"
	"testing"
)

// hub c25db50 的官方实测样本：INTL /api/v2/quota/usage 的 SOTA 在
// dedicatedResourcePackages（数组 + displayLabels zh-CN「SOTA 专属积分」）。
// 官方口径钉住：面板旧口径只报「资源包 1,800」，实际 1,800 + 1,961 = 3,761
//（总量 3,800 = 1,800 + 2,000；已用 39）。
const sotaSample = `{
  "userQuota": {"remaining": 0, "used": 0, "total": 0},
  "addOnQuota": {"remaining": 1800, "used": 0, "total": 1800},
  "isQuotaExceeded": false,
  "totalUsagePercentage": 1.0,
  "dedicatedResourcePackages": [{
    "total": 2000, "used": 39, "remaining": 1961, "expiresAt": 1794146889433,
    "displayLabels": [{"dimension": "title",
                       "valueI18n": {"zh-CN": "SOTA 专属积分"}}]
  }]
}`

func TestNormalizeQuotaPayloadSOTA(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(sotaSample), &payload); err != nil {
		t.Fatal(err)
	}
	buckets := normalizeQuotaPayload(payload)
	var plan, sota int
	var sotaBucket *QuotaBucket
	for i := range buckets {
		b := &buckets[i]
		switch b.Kind {
		case "plan":
			plan++
		case "sota":
			sota++
			sotaBucket = b
		}
	}
	if plan == 0 || sota == 0 {
		t.Fatalf("buckets = %+v", buckets)
	}
	// 展示名取 displayLabels 的 zh-CN，而不是容器名。
	if sotaBucket.Name != "SOTA 专属积分" {
		t.Fatalf("sota name = %q", sotaBucket.Name)
	}
	remain, used, size := quotaCompositionTotals(buckets)
	if remain != 3761 || size != 3800 || used != 39 {
		t.Fatalf("totals = remain %d / used %d / size %d, want 3761/39/3800（官方口径）", remain, used, size)
	}
	if sotaBucket.ExpiresAt <= 0 {
		t.Fatalf("sota expiry = %d", sotaBucket.ExpiresAt)
	}
}

// available=false 的活动包只展示不计入合计（过期/停用不该算余额）。
func TestNormalizeQuotaPayloadUnavailableExcluded(t *testing.T) {
	// JSON 解码构造（数字为 float64，与真实响应解码形态一致）。
	const doc = `{
	  "userQuota": {"remaining": 100, "used": 10, "total": 110},
	  "addOnQuota": [
	    {"remaining": 50, "used": 5, "total": 55, "available": false},
	    {"remaining": 20, "used": 0, "total": 20, "available": true}
	  ]
	}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(doc), &payload); err != nil {
		t.Fatal(err)
	}
	buckets := normalizeQuotaPayload(payload)
	remain, _, size := quotaCompositionTotals(buckets)
	if remain != 120 || size != 130 {
		t.Fatalf("totals = %d/%d, want 120/130（排除 available=false）", remain, size)
	}
	var sawUnavailable bool
	for _, b := range buckets {
		if !b.Available {
			sawUnavailable = true
		}
	}
	if !sawUnavailable {
		t.Fatal("unavailable bucket should still be listed (display-only)")
	}
}

// shared 桶单列：不进个人合计（与 Shared* 字段口径一致，避免重复计数）。
func TestNormalizeQuotaPayloadSharedExcluded(t *testing.T) {
	const doc = `{
	  "userQuota": {"remaining": 100, "used": 0, "total": 100},
	  "orgResourcePackage": {"remaining": 500, "used": 0, "total": 500}
	}`
	var payload map[string]any
	if err := json.Unmarshal([]byte(doc), &payload); err != nil {
		t.Fatal(err)
	}
	buckets := normalizeQuotaPayload(payload)
	remain, _, _ := quotaCompositionTotals(buckets)
	if remain != 100 {
		t.Fatalf("totals = %d, want 100（shared 不计入）", remain)
	}
	var sawShared bool
	for _, b := range buckets {
		if b.Kind == "shared" {
			sawShared = true
		}
	}
	if !sawShared {
		t.Fatal("shared bucket should be present in composition")
	}
}

// 无可识别桶 → nil（端点不适用判定）。
func TestNormalizeQuotaPayloadUnrecognized(t *testing.T) {
	if got := normalizeQuotaPayload(map[string]any{"foo": "bar"}); got != nil {
		t.Fatalf("unrecognized = %+v", got)
	}
}

// 端点候选矩阵推进。
func TestQuotaPathCandidates(t *testing.T) {
	next, ok := nextQuotaPathCandidate("/api/v2/quota/usage")
	if !ok || next != "/api/v1/quota/usage" {
		t.Fatalf("next = %q ok=%v", next, ok)
	}
	if _, ok := nextQuotaPathCandidate("/sash/api/v1/me/usage"); ok {
		t.Fatal("last candidate has no next")
	}
}
