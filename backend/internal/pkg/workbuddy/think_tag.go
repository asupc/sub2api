// think_tag.go 把漏进正文的思考控制标签摘出来（panel 771c887 / issue #194）。
//
// hy4 的 vLLM reasoning parser 偶发把整段 CoT 当 content 下发：usage 里
// reasoning_tokens=0，正文首尾带模型自己的控制 token `<think:6124c78e>…</think:6124c78e>`
// （实测 31 条 assistant 消息里 4 条）。Anthropic 客户端只认 content_block.type=thinking，
// 内联标签一律当正文显示；上游 parser 修不了，网关至少不该把控制 token 当正文发出去。
//
// 只在 content 以 `<think:` 开头时启用（正文中间提到这几个字不重写）。
// shortcut: 只认带后缀的 `<think:SUF>` 形态，不带冒号的裸 `<think>` 不处理。
package workbuddy

import "strings"

const (
	thinkOpen  = "<think:"
	thinkClose = "</think:"
)

// ThinkTag 流式/整段正文的思考标签拆分器。Feed 喂增量，Flush 收尾。
type ThinkTag struct {
	decided bool   // 已判定这段 content 是否内联思考
	on      bool   // 启用摘除
	inTag   bool   // 当前位置在标签内
	pend    string // 尾部可能是半个标签，等下一片
}

// Feed 吃一片正文，返回本片里的思考与正文（按原顺序拼进各自通道）。
func (t *ThinkTag) Feed(s string) (think, body string) {
	if s == "" {
		return "", ""
	}
	t.pend += s
	if !t.decided {
		// 开头还没看够：先等一片，别把 "<thi" 当正文发出去。
		if len(t.pend) < len(thinkOpen) && strings.HasPrefix(thinkOpen, t.pend) {
			return "", ""
		}
		t.decided = true
		if t.on = strings.HasPrefix(t.pend, thinkOpen); !t.on {
			out := t.pend
			t.pend = ""
			return "", out
		}
	}
	if !t.on {
		out := t.pend
		t.pend = ""
		return "", out
	}
	return t.scan(false)
}

// Flush 吐出尾部残留。流结束（或非流式吃完一次）时必须调用，否则最后几个
// 字符会丢。
func (t *ThinkTag) Flush() (think, body string) {
	if t.pend == "" {
		return "", ""
	}
	if !t.decided {
		t.decided = true
		if t.on = strings.HasPrefix(t.pend, thinkOpen); !t.on {
			out := t.pend
			t.pend = ""
			return "", out
		}
	}
	if !t.on {
		out := t.pend
		t.pend = ""
		return "", out
	}
	return t.scan(true)
}

// Active 报告拆分器是否已判定为启用（调用方据此决定是否还有理由继续喂）。
func (t *ThinkTag) Active() bool { return t.on }

// scan 消费 t.pend。flush=false 时把可能是半个标签的尾巴留在 pend 里等下一片。
func (t *ThinkTag) scan(flush bool) (think, body string) {
	var tb, bb strings.Builder
	emit := func(s string) {
		if s == "" {
			return
		}
		if t.inTag {
			tb.WriteString(s)
		} else {
			bb.WriteString(s)
		}
	}
	for {
		p := t.pend
		idx, closing := -1, false
		for _, m := range []struct {
			tag     string
			closing bool
		}{{thinkOpen, false}, {thinkClose, true}} {
			if k := strings.Index(p, m.tag); k >= 0 && (idx < 0 || k < idx) {
				idx, closing = k, m.closing
			}
		}
		if idx < 0 {
			keep := 0
			if !flush {
				// 尾巴可能是下一个标签的前半截（最长 len(</think:)-1）。
				for n := min(len(p), len(thinkClose)-1); n > 0; n-- {
					tail := p[len(p)-n:]
					if strings.HasPrefix(thinkClose, tail) || strings.HasPrefix(thinkOpen, tail) {
						keep = n
						break
					}
				}
			}
			emit(p[:len(p)-keep])
			t.pend = p[len(p)-keep:]
			return tb.String(), bb.String()
		}
		emit(p[:idx])
		rest := p[idx:]
		gt := strings.IndexByte(rest, '>')
		if gt < 0 {
			// 标签还没收全：留到下一片。flush 时上游已经结束了，按正文发出去，
			// 少一次改写好过丢内容。
			if flush {
				emit(rest)
				t.pend = ""
			} else {
				t.pend = rest
			}
			return tb.String(), bb.String()
		}
		t.pend = rest[gt+1:]
		t.inTag = !closing
	}
}
