//go:build unit

package service

// sse_frame_split_test.go 2026-11-20 移植（workbuddy2api-panel afb5f22 / #197）。

import "testing"

func TestSplitConcatenatedJSON(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		want  int
		valid bool
	}{
		{"two objects", `{"a":1}{"b":2}`, 2, true},
		{"whitespace separated", `{"a":1} {"b":2}` + "\n", 2, true},
		{"braces in strings", `{"a":"}{"}{"b":2}`, 2, true},
		{"escaped quote", `{"a":"x\"}{"}{"b":2}`, 2, true},
		{"single object", `{"a":1}`, 1, false},
		{"truncated", `{"a":1}{"b":`, 0, false},
		{"garbage prefix", `xx{"a":1}`, 0, false},
		{"plain text", `hello`, 0, false},
	}
	for _, c := range cases {
		parts, ok := splitConcatenatedJSON(c.in)
		if ok != c.valid {
			t.Errorf("%s: ok=%v want %v", c.name, ok, c.valid)
			continue
		}
		if ok && len(parts) != c.want {
			t.Errorf("%s: parts=%d want %d", c.name, len(parts), c.want)
		}
	}
}

func TestExpandSSEDataLine(t *testing.T) {
	lines := expandSSEDataLine(`data: {"a":1}{"b":2}`)
	if len(lines) != 3 {
		t.Fatalf("lines = %#v", lines)
	}
	if lines[0] != `data: {"a":1}` || lines[1] != "" || lines[2] != `data: {"b":2}` {
		t.Fatalf("split shape = %#v", lines)
	}
	// 合法 JSON / [DONE] / 非 data 行原样返回。
	for _, in := range []string{`data: {"a":1}`, `data: [DONE]`, `event: message`, ``} {
		got := expandSSEDataLine(in)
		if len(got) != 1 || got[0] != in {
			t.Fatalf("%q must be untouched, got %#v", in, got)
		}
	}
}
