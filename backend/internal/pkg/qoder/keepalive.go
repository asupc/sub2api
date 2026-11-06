package qoder

// 会话缓存保温（cache keepalive，qoder2api-hub 22bd512 / qoder_keepalive.py）
// ——可选优化，默认关闭。
//
// 背景：上游对 prompt 前缀的缓存有闲置过期行为，实测约 5 分钟未被命中即失效
// （与阿里云百炼显式缓存公开语义一致：5 分钟、命中续期）。会话闲置超过该
// 窗口后，下一发请求全量重写——0% 命中、全价积分（124k prompt ≈ 137 credits，
// 命中时 ≈ 13.8）。#136（session_id 窗口级）修的是「会话键每提问轮换」，与
// 本机制（闲置过期）正交，网关无法消除，只能缓解。
//
// 机制：网关在成功转发一条真实对话请求后，把客户端原始 payload 按**最终
// session_id** 记录在内存里（只保留最近 MAX_CONVS 个，超出 LRU 淘汰）。后台
// 每 20s 巡检：会话闲置 >= INTERVAL 秒且未超过 MAX_AGE 时，用同 payload 重发
// 一次 stream=false、max_tokens=1 的最小请求——prompt 前缀逐字节相同，命中
// 上游缓存并续期，消耗 ≈ 一次缓存读。
//
// 边界：默认 QD_CACHE_KEEPALIVE=0（不开、不存任何东西）；只存 payload 不落盘；
// 发送函数由 service 层注册（ConfigureKeepalive），本模块不 import service
// （无环依赖，KeepaliveTick 可用假 sender 单测）。上游对「同会话高频重复
// 请求」的风控容忍度未知：从小 INTERVAL、少会话起步观察再调大。

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// keepaliveEntry 一条已登记会话的快照。
type keepaliveEntry struct {
	payload   map[string]any
	sessionID string
	accountID int64
	realm     string
	model     string
	at        time.Time
	pings     int
}

// KeepaliveEntry 对外暴露的登记条目（sender 入参；payload 为深拷贝快照）。
type KeepaliveEntry struct {
	Payload   map[string]any
	SessionID string
	AccountID int64
	Realm     string
	Model     string
	Pings     int
}

// KeepaliveSender 保温发送函数（service 层注册；失败返回 error，tick 淘汰该会话）。
type KeepaliveSender func(entry KeepaliveEntry) error

var (
	keepaliveMu      sync.Mutex
	keepaliveEntries = map[string]*keepaliveEntry{} // sessionID -> entry
	keepaliveOrder   []string                       // LRU 序（尾最新）
	keepaliveSender  KeepaliveSender
	keepaliveStats   = map[string]int{"pings_sent": 0, "pings_failed": 0, "stale_drops": 0}
)

// keepaliveEnvInt 读环境变量整数（lo/hi 钳制；缺省/非法回落默认）。
func keepaliveEnvInt(name string, def, lo, hi int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// KeepaliveEnabled QD_CACHE_KEEPALIVE，默认关闭（0/false/no/off 均为关）。
func KeepaliveEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("QD_CACHE_KEEPALIVE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// KeepaliveInterval 闲置多久后保温一次（QD_CACHE_KEEPALIVE_INTERVAL 秒，默认
// 240——必须小于上游 ~5min TTL）。
func KeepaliveInterval() time.Duration {
	return time.Duration(keepaliveEnvInt("QD_CACHE_KEEPALIVE_INTERVAL", 240, 30, 86400)) * time.Second
}

// KeepaliveMaxAge 闲置超过该时长的会话不再保温（人早走了，省下这笔读费用；
// QD_CACHE_KEEPALIVE_MAX_AGE 秒，默认 1800）。
func KeepaliveMaxAge() time.Duration {
	return time.Duration(keepaliveEnvInt("QD_CACHE_KEEPALIVE_MAX_AGE", 1800, 60, 86400)) * time.Second
}

// KeepaliveMaxConvs 内存里最多保留几个会话的 payload（LRU，
// QD_CACHE_KEEPALIVE_MAX_CONVS，默认 2）。
func KeepaliveMaxConvs() int {
	return keepaliveEnvInt("QD_CACHE_KEEPALIVE_MAX_CONVS", 2, 1, 64)
}

// ConfigureKeepalive 注册保温发送函数。
func ConfigureKeepalive(sender KeepaliveSender) {
	keepaliveMu.Lock()
	defer keepaliveMu.Unlock()
	keepaliveSender = sender
}

// KeepaliveObserve 登记一条刚成功转发的真实对话请求（json 往返深拷贝——之后
// 无论网关/客户端怎么改它，保温用的都是登记时的快照，前缀稳定性由快照保证）。
// 关闭或 payload 不可序列化时静默返回 false。可选 now 供测试注入时钟。
func KeepaliveObserve(payload map[string]any, sessionID string, accountID int64, realm, model string, now ...time.Time) bool {
	if !KeepaliveEnabled() || strings.TrimSpace(sessionID) == "" || payload == nil {
		return false
	}
	stored, err := DeepCopyJSONMap(payload)
	if err != nil {
		return false
	}
	at := time.Now()
	if len(now) > 0 {
		at = now[0]
	}
	keepaliveMu.Lock()
	defer keepaliveMu.Unlock()
	keepaliveRemoveLocked(sessionID)
	keepaliveEntries[sessionID] = &keepaliveEntry{
		payload:   stored,
		sessionID: sessionID,
		accountID: accountID,
		realm:     realm,
		model:     model,
		at:        at,
	}
	keepaliveOrder = append(keepaliveOrder, sessionID)
	keepaliveEvictLocked()
	return true
}

// KeepaliveDrop 显式淘汰一个会话（账号不可用等场景）。
func KeepaliveDrop(sessionID string) bool {
	keepaliveMu.Lock()
	defer keepaliveMu.Unlock()
	return keepaliveRemoveLocked(sessionID)
}

// keepaliveRemoveLocked 从 LRU 摘除（调用方持锁）。
func keepaliveRemoveLocked(sessionID string) bool {
	if _, ok := keepaliveEntries[sessionID]; !ok {
		return false
	}
	delete(keepaliveEntries, sessionID)
	for i, sid := range keepaliveOrder {
		if sid == sessionID {
			keepaliveOrder = append(keepaliveOrder[:i], keepaliveOrder[i+1:]...)
			break
		}
	}
	return true
}

// keepaliveTouchLocked 移到 LRU 尾部（调用方持锁）。
func keepaliveTouchLocked(sessionID string) {
	for i, sid := range keepaliveOrder {
		if sid == sessionID {
			keepaliveOrder = append(keepaliveOrder[:i], keepaliveOrder[i+1:]...)
			keepaliveOrder = append(keepaliveOrder, sessionID)
			return
		}
	}
}

// keepaliveEvictLocked 超出容量淘汰最旧（调用方持锁）。
func keepaliveEvictLocked() {
	for len(keepaliveEntries) > KeepaliveMaxConvs() && len(keepaliveOrder) > 0 {
		oldest := keepaliveOrder[0]
		keepaliveRemoveLocked(oldest)
	}
}

// KeepaliveStatus 只读快照（不含 payload，供诊断/日志用）。
func KeepaliveStatus() map[string]any {
	keepaliveMu.Lock()
	defer keepaliveMu.Unlock()
	now := time.Now()
	entries := make([]map[string]any, 0, len(keepaliveEntries))
	for _, sid := range keepaliveOrder {
		e, ok := keepaliveEntries[sid]
		if !ok {
			continue
		}
		short := sid
		if len(short) > 16 {
			short = short[:16]
		}
		entries = append(entries, map[string]any{
			"session_id": short,
			"account_id": e.accountID,
			"model":      e.model,
			"idle_s":     now.Sub(e.at).Round(time.Millisecond) / time.Millisecond,
			"pings":      e.pings,
		})
	}
	stats := make(map[string]int, len(keepaliveStats))
	for k, v := range keepaliveStats {
		stats[k] = v
	}
	return map[string]any{
		"enabled":    KeepaliveEnabled(),
		"interval_s": int(KeepaliveInterval().Seconds()),
		"max_age_s":  int(KeepaliveMaxAge().Seconds()),
		"max_convs":  KeepaliveMaxConvs(),
		"entries":    entries,
		"stats":      stats,
	}
}

// KeepaliveTick 巡检一次：到期会话发保温请求，超龄淘汰。永不 panic；
// sender 失败视为该会话不可保温（账号下线/上游拒绝）——淘汰它，下一发
// 真实请求会重新登记。返回 (sent, failed, stale)。
func KeepaliveTick(now time.Time) (sent, failed, stale int) {
	type action struct {
		sid string
		act string // "ping" | "stale"
	}
	keepaliveMu.Lock()
	due := make([]action, 0, len(keepaliveEntries))
	for sid, e := range keepaliveEntries {
		age := now.Sub(e.at)
		if age >= KeepaliveMaxAge() {
			due = append(due, action{sid, "stale"})
		} else if age >= KeepaliveInterval() && keepaliveSender != nil {
			due = append(due, action{sid, "ping"})
		}
	}
	sender := keepaliveSender
	for _, d := range due {
		e, ok := keepaliveEntries[d.sid]
		if !ok {
			continue
		}
		// 先摘下：成功放回，失败即淘汰。
		keepaliveRemoveLocked(d.sid)
		if d.act == "stale" {
			stale++
			continue
		}
		// 快照与摘取之间 observe() 可能刚刷新过它：没到点就放回去。
		if now.Sub(e.at) < KeepaliveInterval() {
			keepaliveEntries[d.sid] = e
			keepaliveOrder = append(keepaliveOrder, d.sid)
			continue
		}
		payloadCopy, err := DeepCopyJSONMap(e.payload)
		if err != nil || sender == nil {
			failed++
			continue
		}
		entry := KeepaliveEntry{
			Payload:   payloadCopy,
			SessionID: e.sessionID,
			AccountID: e.accountID,
			Realm:     e.realm,
			Model:     e.model,
			Pings:     e.pings,
		}
		// 锁外发送（sender 走网络，持锁会阻塞 observe）。
		keepaliveMu.Unlock()
		sendErr := sender(entry)
		keepaliveMu.Lock()
		if sendErr != nil {
			failed++
			continue
		}
		e.at = now
		e.pings++
		keepaliveEntries[d.sid] = e
		keepaliveOrder = append(keepaliveOrder, d.sid)
		sent++
	}
	keepaliveStats["pings_sent"] += sent
	keepaliveStats["pings_failed"] += failed
	keepaliveStats["stale_drops"] += stale
	keepaliveMu.Unlock()
	return sent, failed, stale
}

// StartKeepaliveLoop 启动后台巡检（每 20s 一次；幂等由调用方 sync.Once 保证）。
// 返回停止函数。
func StartKeepaliveLoop() (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// 巡检永不 panic 打崩进程（hub _loop 的 try/except 同款语义）。
				func() {
					defer func() { _ = recover() }()
					_, _, _ = KeepaliveTick(time.Now())
				}()
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
	}
}

// ResetKeepaliveForTest 清空全局状态（测试用）。
func ResetKeepaliveForTest() {
	keepaliveMu.Lock()
	defer keepaliveMu.Unlock()
	keepaliveEntries = map[string]*keepaliveEntry{}
	keepaliveOrder = nil
	keepaliveSender = nil
	keepaliveStats = map[string]int{"pings_sent": 0, "pings_failed": 0, "stale_drops": 0}
}

// DeepCopyJSONMap json 往返深拷贝（与源 json.loads(json.dumps(payload)) 同款；
// service 层保温发送前也用它做快照隔离）。
func DeepCopyJSONMap(m map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
