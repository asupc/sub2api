//go:build unit

package qoder

import (
	"strings"
	"testing"
	"time"
)

// resetMaxOutLimits 清空学习缓存（每个用例独立）。
func resetMaxOutLimits(t *testing.T) {
	t.Helper()
	maxOutMu.Lock()
	maxOutLimits = map[string]maxOutLimitEntry{}
	maxOutMu.Unlock()
	t.Cleanup(func() {
		maxOutMu.Lock()
		maxOutLimits = map[string]maxOutLimitEntry{}
		maxOutMu.Unlock()
	})
}

func TestLearnMaxOutputLimit(t *testing.T) {
	resetMaxOutLimits(t)
	detail := `data: {"error":{"code":"invalid_parameter_error","message":` +
		`"Range of max_tokens should be [1, 131072]"}}`
	if got := LearnMaxOutputLimit(detail, "DeepSeek-Flash", "cn"); got != 131072 {
		t.Fatalf("learned = %d, want 131072", got)
	}
	if got := MaxOutputLimit("DeepSeek-Flash", "cn"); got != 131072 {
		t.Fatalf("lookup = %d, want 131072", got)
	}
	// 非该格式的错误不影响
	if got := LearnMaxOutputLimit("some other error", "x", "cn"); got != 0 {
		t.Fatalf("unrelated error learned %d", got)
	}
	// 未知模型不猜
	if got := MaxOutputLimit("unknown-model", "cn"); got != 0 {
		t.Fatalf("unknown model limit = %d, want 0", got)
	}
}

func TestLearnMaxOutputLimitAliasesAndTTL(t *testing.T) {
	resetMaxOutLimits(t)
	// 同名别名（ResolveUpstreamKey 能解析到同一个 key）也要能查到。
	// 用真实存在的显示名/键对：静态目录里 "Qwen3.8-Max" <-> qmodel_38max。
	detail := `Range of max_tokens should be [1, 32768]`
	if got := LearnMaxOutputLimit(detail, "qmodel_38max", "cn"); got != 32768 {
		t.Fatalf("learned = %d, want 32768", got)
	}
	if got := MaxOutputLimit("Qwen3.8-Max", "cn"); got != 32768 {
		t.Fatalf("alias lookup = %d, want 32768", got)
	}
	// 7 天 TTL：把缓存时间戳拨回 8 天前，应失效（不再猜）。
	maxOutMu.Lock()
	for k, v := range maxOutLimits {
		v.at = time.Now().Add(-8 * 24 * time.Hour)
		maxOutLimits[k] = v
	}
	maxOutMu.Unlock()
	if got := MaxOutputLimit("qmodel_38max", "cn"); got != 0 {
		t.Fatalf("after TTL limit = %d, want 0", got)
	}
}

func TestClampMaxTokens(t *testing.T) {
	resetMaxOutLimits(t)
	detail := `Range of max_tokens should be [1, 131072]`
	LearnMaxOutputLimit(detail, "DeepSeek-Flash", "cn")

	if got, clamped := ClampMaxTokens(200000, "DeepSeek-Flash", "cn"); got != 131072 || !clamped {
		t.Fatalf("over-limit: got %d clamped=%v", got, clamped)
	}
	if got, clamped := ClampMaxTokens(131072, "DeepSeek-Flash", "cn"); got != 131072 || clamped {
		t.Fatalf("equal-limit: got %d clamped=%v", got, clamped)
	}
	if got, clamped := ClampMaxTokens(256, "DeepSeek-Flash", "cn"); got != 256 || clamped {
		t.Fatalf("in-range: got %d clamped=%v", got, clamped)
	}
	// 未学过的模型不压（不猜 131072）
	if got, clamped := ClampMaxTokens(200000, "unknown-model", "cn"); got != 200000 || clamped {
		t.Fatalf("unknown model: got %d clamped=%v", got, clamped)
	}
	// 0/负值 → 1，始终生效（不依赖是否学到上限）
	if got, clamped := ClampMaxTokens(0, "unknown-model", "cn"); got != 1 || !clamped {
		t.Fatalf("zero: got %d clamped=%v", got, clamped)
	}
	if got, clamped := ClampMaxTokens(-5, "DeepSeek-Flash", "cn"); got != 1 || !clamped {
		t.Fatalf("negative: got %d clamped=%v", got, clamped)
	}
}

func TestMaxOutputLimitRealmIsolation(t *testing.T) {
	resetMaxOutLimits(t)
	LearnMaxOutputLimit(`Range of max_tokens should be [1, 1000]`, "m1", "cn")
	if got := MaxOutputLimit("m1", "intl"); got != 0 {
		t.Fatalf("intl should not see cn learning: %d", got)
	}
	if got := MaxOutputLimit("m1", "cn"); got != 1000 {
		t.Fatalf("cn lookup = %d, want 1000", got)
	}
	// 无 realm 时跨区查找（/v1/models 条目场景）。
	if got := MaxOutputLimit("m1", ""); got != 1000 {
		t.Fatalf("cross-realm lookup = %d, want 1000", got)
	}
}

func TestModelEntryOutputFields(t *testing.T) {
	resetMaxOutLimits(t)
	if fields := ModelEntryOutputFields("DeepSeek-Flash", "cn"); fields != nil {
		t.Fatalf("unknown model should not emit fields: %v", fields)
	}
	LearnMaxOutputLimit(`Range of max_tokens should be [1, 131072]`, "DeepSeek-Flash", "cn")
	fields := ModelEntryOutputFields("DeepSeek-Flash", "cn")
	if fields == nil {
		t.Fatal("learned model should emit fields")
	}
	if fields["max_output_tokens"] != 131072 || fields["max_completion_tokens"] != 131072 {
		t.Fatalf("fields: %v", fields)
	}
	// PublicID 口径也能查到（显示名 -> key）。
	if fields := ModelEntryOutputFields("Qwen3.8-Max", "cn"); fields != nil {
		// 未学到 Qwen 上限时不输出——不猜。
		t.Fatalf("unlearned public id should not emit: %v", fields)
	}
}

func TestCatalogEntryEffectiveMaxOutputTokens(t *testing.T) {
	resetMaxOutLimits(t)
	e := CatalogEntry{Key: "k1"}
	if got := e.EffectiveMaxOutputTokens("cn"); got != 0 {
		t.Fatalf("empty catalog entry = %d, want 0", got)
	}
	// catalog 自带的优先。
	e2 := CatalogEntry{Key: "k2", MaxOutputTokens: 4096}
	if got := e2.EffectiveMaxOutputTokens("cn"); got != 4096 {
		t.Fatalf("catalog value = %d, want 4096", got)
	}
	// catalog 缺失时回落学到的。
	LearnMaxOutputLimit(`Range of max_tokens should be [1, 2048]`, "k3", "cn")
	e3 := CatalogEntry{Key: "k3"}
	if got := e3.EffectiveMaxOutputTokens("cn"); got != 2048 {
		t.Fatalf("learned fallback = %d, want 2048", got)
	}
}

func TestLearnMaxOutputLimitRegexVariants(t *testing.T) {
	resetMaxOutLimits(t)
	cases := []struct {
		detail string
		want   int
	}{
		{`Range of max_tokens should be [1, 131072]`, 131072},
		{`range of max_tokens should be [1,  65536]`, 65536}, // 大小写 + 双空格
		{`invalid_parameter_error: Range of max_tokens should be [1, 1]`, 1},
		{`Range of max_tokens should be [1, 0]`, 0}, // 非正上限不学
		{`no limit here`, 0},
	}
	for _, tc := range cases {
		resetMaxOutLimits(t)
		got := LearnMaxOutputLimit(tc.detail, "m", "cn")
		if got != tc.want {
			t.Fatalf("detail %q: got %d want %d", tc.detail, got, tc.want)
		}
	}
}

func TestLearnMaxOutputLimitIdempotent(t *testing.T) {
	resetMaxOutLimits(t)
	detail := `Range of max_tokens should be [1, 1000]`
	if got := LearnMaxOutputLimit(detail, "m", "cn"); got != 1000 {
		t.Fatal(got)
	}
	// 重复学习同值不改变缓存时间（避免无限续期掩盖过期）。
	maxOutMu.Lock()
	before := maxOutLimits[maxOutKey("cn", "m")].at
	maxOutMu.Unlock()
	time.Sleep(2 * time.Millisecond)
	if got := LearnMaxOutputLimit(detail, "m", "cn"); got != 1000 {
		t.Fatal(got)
	}
	maxOutMu.Lock()
	after := maxOutLimits[maxOutKey("cn", "m")].at
	maxOutMu.Unlock()
	if !after.Equal(before) {
		t.Fatalf("re-learn refreshed timestamp: %v -> %v", before, after)
	}
	// 学到不同值时更新。
	if got := LearnMaxOutputLimit(`Range of max_tokens should be [1, 2000]`, "m", "cn"); got != 2000 {
		t.Fatal(got)
	}
	if got := MaxOutputLimit("m", "cn"); got != 2000 {
		t.Fatalf("updated limit = %d, want 2000", got)
	}
}

func TestMaxOutputLimitEmptyInputs(t *testing.T) {
	resetMaxOutLimits(t)
	if got := MaxOutputLimit("", ""); got != 0 {
		t.Fatalf("empty inputs = %d", got)
	}
	if got := LearnMaxOutputLimit("", "m", "cn"); got != 0 {
		t.Fatalf("empty detail = %d", got)
	}
	// 空 model 也能学（源允许 model=""，按 (realm,"") 存）。
	if got := LearnMaxOutputLimit(`Range of max_tokens should be [1, 99]`, "", "cn"); got != 99 {
		t.Fatalf("empty model learn = %d", got)
	}
	if got := MaxOutputLimit("", "cn"); got != 99 {
		t.Fatalf("empty model lookup = %d", got)
	}
}

func TestClampMaxTokensLogsOnClamp(t *testing.T) {
	// 仅验证函数返回语义；日志内容由 log.Printf 输出，测试不捕获。
	resetMaxOutLimits(t)
	LearnMaxOutputLimit(`Range of max_tokens should be [1, 10]`, "m", "cn")
	if got, _ := ClampMaxTokens(100, "m", "cn"); got != 10 {
		t.Fatal(got)
	}
	if !strings.Contains("x", "x") {
		t.Fatal("unreachable")
	}
}
