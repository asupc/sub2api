package qoder

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"time"
)

// 上游会话身份稳定化（移植 qoder2api-hub 747f83a / issue #128）。
//
// 上游用量面板按请求体里的会话身份归组「一条对话」（看起来按 chat_record_id）。
// 官方客户端一次对话内 chat_record_id / business.id / begin_at 不变，而网关
// 此前每请求新造 —— 一个 agent 回合的几十次工具步进被记成几十条「<1min 的对话」，
// 迅速撞上 code 110 的每日计费次数上限。同一客户端会话复用同一身份与 begin_at；
// request_id / request_set_id 仍逐请求唯一（计费/限额按请求计数，不能被并掉）。
//
// 会话键优先取客户端显式值（conversation_id / session_id / metadata.conversation_id），
// 缺失时退化为「模型 + 系统提示 + 首条 user 消息」摘要；都拿不到则保持旧行为（每请求新造）。
// QD_CONVERSATION_SHARE=0 可关闭。

const (
	// conversationTTLSecs 会话身份在缓存中的有效期（源 _CONV_TTL_SECS = 6h）。
	conversationTTLSecs int64 = 6 * 3600
	// conversationMaxEntries LRU 容量上限（源 _CONV_MAX_ENTRIES = 4096）。
	conversationMaxEntries = 4096
)

// upstreamConversation 一次上游会话的稳定身份。
type upstreamConversation struct {
	key     string
	id      string
	beginAt int64
	at      int64 // unix 秒，最后一次使用时间
}

var (
	conversationMu  sync.Mutex
	conversationLRU = list.New()
	// conversationIndex 会话键 → LRU 元素（值为 *upstreamConversation）。
	conversationIndex = map[string]*list.Element{}

	// conversationClock 测试可替换的时钟。
	conversationClock = time.Now
)

// conversationShareEnabled QD_CONVERSATION_SHARE=0 关闭会话身份共享。
func conversationShareEnabled() bool {
	return strings.TrimSpace(os.Getenv("QD_CONVERSATION_SHARE")) != "0"
}

// conversationKeyFromHistory 无显式会话键时的退化判据：模型 + 系统提示 + 首条
// user 消息的 sha1 摘要（源 _conversation_key_from_history）。系统提示取前 200
// 字、首条 user 取前 400 字（与源一致）。
func conversationKeyFromHistory(req map[string]any) string {
	if req == nil {
		return ""
	}
	msgs, _ := req["messages"].([]any)
	if len(msgs) == 0 {
		return ""
	}
	firstUser, system := "", ""
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		text := messageText(m["content"])
		if role == "system" && system == "" {
			system = headN(text, 200)
		} else if role == "user" && firstUser == "" {
			firstUser = headN(text, 400)
			break
		}
	}
	if firstUser == "" {
		return ""
	}
	model, _ := req["model"].(string)
	sum := sha1.Sum([]byte(model + "\x00" + system + "\x00" + firstUser))
	return "hist:" + hex.EncodeToString(sum[:])
}

// messageText 取消息正文的纯文本形态（字符串或 parts 数组拼接）。
func messageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := pm["text"].(string); t != "" {
				if b.Len() > 0 {
					b.WriteByte(' ')
				}
				b.WriteString(t)
			}
		}
		return b.String()
	}
	return ""
}

// headN 按字符（rune）截断，避免多字节字符被腰斩。
func headN(s string, n int) string {
	if n <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ConversationIdentity 返回本请求所属的上游会话身份（源 conversation_identity）。
// clientSessionKey 为空时退化为历史摘要；两者都拿不到返回 nil（调用方保持旧行为）。
// 关闭开关时也返回 nil。
func ConversationIdentity(clientSessionKey string, req map[string]any) *upstreamConversation {
	if !conversationShareEnabled() {
		return nil
	}
	key := strings.TrimSpace(clientSessionKey)
	if key == "" {
		key = conversationKeyFromHistory(req)
	}
	if key == "" {
		return nil
	}
	now := conversationClock().Unix()
	conversationMu.Lock()
	defer conversationMu.Unlock()
	if el, ok := conversationIndex[key]; ok {
		entry := el.Value.(*upstreamConversation)
		if now-entry.at <= conversationTTLSecs {
			entry.at = now
			conversationLRU.MoveToFront(el)
			return &upstreamConversation{id: entry.id, beginAt: entry.beginAt}
		}
		conversationLRU.Remove(el)
		delete(conversationIndex, key)
	}
	entry := &upstreamConversation{key: key, id: UUID4(), beginAt: now * 1000, at: now}
	conversationIndex[key] = conversationLRU.PushFront(entry)
	for conversationLRU.Len() > conversationMaxEntries {
		oldest := conversationLRU.Back()
		if oldest == nil {
			break
		}
		conversationLRU.Remove(oldest)
		if old, ok := oldest.Value.(*upstreamConversation); ok {
			delete(conversationIndex, old.key)
		}
	}
	return &upstreamConversation{id: entry.id, beginAt: entry.beginAt}
}
