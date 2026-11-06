package qoder

import (
	"container/list"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"strconv"
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
// 会话键优先取客户端显式值（conversation_id / session_id / metadata.conversation_id）
// 并追加「本轮提问」摘要（最后一条 user + 系统提示 + 模型），使「一次对话 = 一次
// 用户提问 + 后续工具步进」；无显式键时直接用该摘要；都拿不到则保持旧行为（每请求新造）。
// QD_CONVERSATION_SHARE=0/false/no 可关闭，QD_CONVERSATION_TTL 覆盖空闲有效期（秒）。

const (
	// conversationTTLDefault 会话身份在缓存中的默认有效期（源 _CONV_TTL_SECS =
	// 6h，QD_CONVERSATION_TTL 可覆盖，单位秒）。
	conversationTTLDefault int64 = 6 * 3600
	// conversationMaxEntries LRU 容量上限（源 _CONV_MAX_ENTRIES = 4096）。
	conversationMaxEntries = 4096
)

// upstreamConversation 一次上游会话的稳定身份。
type upstreamConversation struct {
	key string
	id  string
	// windowID 窗口级 id（整段对话恒定，hub #136）：喂 session_id，保证上游
	// prompt 缓存按 session_id 命中；与 id（按一次提问，用量归组）解耦——
	// 两者曾是一个键，v1.7.17 把 id 改成一次提问一个后 session_id 跟着变，
	// 每轮新提问上游缓存归零（面板 98%↔0% 循环，同段历史差 12 倍价格）。
	windowID string
	beginAt  int64
	at       int64 // unix 秒，最后一次使用时间
}

var (
	conversationMu  sync.Mutex
	conversationLRU = list.New()
	// conversationIndex 会话键 → LRU 元素（值为 *upstreamConversation）。
	conversationIndex = map[string]*list.Element{}

	// conversationClock 测试可替换的时钟。
	conversationClock = time.Now
)

// conversationShareEnabled QD_CONVERSATION_SHARE=0/false/no（不分大小写）关闭共享。
func conversationShareEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("QD_CONVERSATION_SHARE"))) {
	case "0", "false", "no":
		return false
	}
	return true
}

// conversationTTL 会话身份空闲有效期（QD_CONVERSATION_TTL 秒，缺省 6h）。
func conversationTTL() int64 {
	if v := strings.TrimSpace(os.Getenv("QD_CONVERSATION_TTL")); v != "" {
		if secs, err := strconv.ParseFloat(v, 64); err == nil && secs > 0 {
			return int64(secs)
		}
	}
	return conversationTTLDefault
}

// conversationKeyFromHistory 会话判据：**最后一条** user 消息 + 系统提示 + 模型
// （源 issue #128 后续 db99d43）。
//
// 为什么不是首条：Codex 全量回放历史，若用首条，整个窗口（可能几小时）永远是
// 同一个 id —— 用量面板里就成了一条超长对话。用最后一条 user 消息：同一次对话
// 内（工具步进时）它不变，用户下一次提问才变，正好等于「一次对话 = 一次用户
// 提问 + 后续工具步进」。系统提示取前 200 字、最后一条 user 取前 400 字。
func conversationKeyFromHistory(req map[string]any) string {
	if req == nil {
		return ""
	}
	msgs, _ := req["messages"].([]any)
	if len(msgs) == 0 {
		return ""
	}
	lastUser, system := "", ""
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := m["role"].(string)
		text := messageText(m["content"])
		if role == "system" && system == "" {
			system = headN(text, 200)
		} else if role == "user" {
			lastUser = headN(text, 400) // 一直覆盖到最后一条
		}
	}
	if lastUser == "" {
		return ""
	}
	model, _ := req["model"].(string)
	sum := sha1.Sum([]byte(model + "\x00" + system + "\x00" + lastUser))
	return "turn:" + hex.EncodeToString(sum[:])
}

// conversationWindowKey 窗口级会话键（整段对话恒定，hub #136）：显式键优先，
// 否则「模型 + 首条 user」。**不吃 system**——system 里常带每轮都会变的运行时
// 上下文（cwd / git 状态 / 当前时间），把它算进键会让窗口键逐轮变化 →
// session_id 逐轮更换 → 上游 prompt 缓存永远打不中（#136 的第二层原因）。
// 该键喂 session_id（缓存命中），与提问键 conversationKeyFromHistory（喂
// business.id / chat_record_id 的用量归组）解耦。
func conversationWindowKey(clientSessionKey string, req map[string]any) string {
	if key := strings.TrimSpace(clientSessionKey); key != "" {
		return key
	}
	if req == nil {
		return ""
	}
	msgs, _ := req["messages"].([]any)
	firstUser := ""
	for _, raw := range msgs {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role != "user" {
			continue
		}
		// 空内容 user 不作锚（源：空串继续找下一条，取第一条有内容的 user）。
		if text := messageText(m["content"]); text != "" {
			firstUser = headN(text, 400)
			break
		}
	}
	if firstUser == "" {
		return ""
	}
	model, _ := req["model"].(string)
	sum := sha1.Sum([]byte(model + "\x00" + firstUser))
	return "win:" + hex.EncodeToString(sum[:])
}

// messageText 取消息正文的纯文本形态（字符串或 parts 数组拼接）。
func messageText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		// 源用空格 join **所有** dict part 的 text（含空串）——保持摘要口径一致。
		parts := make([]string, 0, len(v))
		for _, p := range v {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			t, _ := pm["text"].(string)
			parts = append(parts, t)
		}
		return strings.Join(parts, " ")
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

// ConversationIdentity 返回本请求所属的上游会话身份（源 conversation_identity，
// hub #128 / #136）。
// clientSessionKey 为空时退化为历史摘要；两者都拿不到返回 nil（调用方保持旧行为）。
// 关闭开关时也返回 nil。windowID 为窗口级 id（#136，喂 session_id 兜底）；窗口键
// 拿不到时为空串，调用方回落 id（提问级，一次提问内仍稳定——源 window_id or id）。
func ConversationIdentity(clientSessionKey string, req map[string]any) *upstreamConversation {
	if !conversationShareEnabled() {
		return nil
	}
	key := strings.TrimSpace(clientSessionKey)
	turn := conversationKeyFromHistory(req)
	if key != "" {
		// issue #128 后续：窗口级显式键会把几小时的会话并成一条，面板看不出单次
		// 对话时长。这里在显式键后追加「本轮提问」摘要 —— 同一次提问内的工具
		// 步进仍归同一条，用户下次提问才换 id。
		if turn != "" {
			key = key + "|" + turn
		}
	} else {
		key = turn
	}
	if key == "" {
		return nil
	}
	// #136：窗口键确定性派生（同键同值），现算即可，不入 LRU。
	windowID := conversationWindowKey(clientSessionKey, req)
	now := conversationClock().Unix()
	ttl := conversationTTL()
	conversationMu.Lock()
	defer conversationMu.Unlock()
	if el, ok := conversationIndex[key]; ok {
		entry := el.Value.(*upstreamConversation)
		if now-entry.at <= ttl {
			entry.at = now
			conversationLRU.MoveToFront(el)
			return &upstreamConversation{id: entry.id, windowID: windowID, beginAt: entry.beginAt}
		}
		conversationLRU.Remove(el)
		delete(conversationIndex, key)
	}
	entry := &upstreamConversation{key: key, id: UUID4(), windowID: windowID, beginAt: now * 1000, at: now}
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
	return &upstreamConversation{id: entry.id, windowID: entry.windowID, beginAt: entry.beginAt}
}
