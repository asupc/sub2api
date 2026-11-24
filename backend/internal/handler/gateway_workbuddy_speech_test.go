//go:build unit

package handler

import "testing"

// TestNormalizeWorkbuddyASRResponseFormat OpenAI ASR 只支持 json/text；需要时间戳的
// 格式明确拒绝，不能静默回 JSON（workbuddy2api-panel d5ce7de / issue #277）。
func TestNormalizeWorkbuddyASRResponseFormat(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"", "", true},
		{"json", "json", true},
		{" JSON ", "json", true},
		{"text", "text", true},
		{"Text", "text", true},
		{"srt", "srt", false},
		{"vtt", "vtt", false},
		{"verbose_json", "verbose_json", false},
	}
	for _, tc := range cases {
		got, ok := normalizeWorkbuddyASRResponseFormat(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: got=(%q,%v) want=(%q,%v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
