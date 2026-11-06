//go:build unit

package qoder

// 会话缓存保温（qoder2api-hub 22bd512）：登记 / 深拷贝隔离 / LRU / tick
// 到期发送与超龄淘汰 / 失败淘汰（参照 hub _test_cache_keepalive.py）。

import (
	"testing"
	"time"
)

func TestKeepaliveDisabledByDefault(t *testing.T) {
	t.Setenv("QD_CACHE_KEEPALIVE", "")
	if KeepaliveEnabled() {
		t.Fatal("default must be disabled")
	}
	if KeepaliveObserve(map[string]any{"model": "m"}, "sess-1", 1, "cn", "m") {
		t.Fatal("disabled observe must return false")
	}
}

func TestKeepaliveObserveDeepCopyAndLRU(t *testing.T) {
	t.Setenv("QD_CACHE_KEEPALIVE", "1")
	t.Setenv("QD_CACHE_KEEPALIVE_MAX_CONVS", "2")
	ResetKeepaliveForTest()
	t.Cleanup(ResetKeepaliveForTest)

	payload := map[string]any{"model": "m", "messages": []any{"a"}}
	if !KeepaliveObserve(payload, "sess-1", 1, "cn", "m") {
		t.Fatal("observe must succeed when enabled")
	}
	// 深拷贝隔离：改原 map 不影响快照。
	payload["model"] = "changed"
	st := KeepaliveStatus()
	entries := st["entries"].([]map[string]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v", entries)
	}
	// LRU：第 3 条挤掉最旧的 sess-1。
	KeepaliveObserve(payload, "sess-2", 1, "cn", "m")
	KeepaliveObserve(payload, "sess-3", 1, "cn", "m")
	if !KeepaliveDrop("sess-1") {
		t.Log("sess-1 already evicted by LRU (ok)")
	}
	if KeepaliveDrop("sess-1") {
		t.Fatal("double drop must be false")
	}
}

func TestKeepaliveTickPingStaleAndFailure(t *testing.T) {
	t.Setenv("QD_CACHE_KEEPALIVE", "1")
	t.Setenv("QD_CACHE_KEEPALIVE_INTERVAL", "30")
	t.Setenv("QD_CACHE_KEEPALIVE_MAX_AGE", "60")
	t.Setenv("QD_CACHE_KEEPALIVE_MAX_CONVS", "8")
	ResetKeepaliveForTest()
	t.Cleanup(ResetKeepaliveForTest)

	var sentEntries []KeepaliveEntry
	ConfigureKeepalive(func(e KeepaliveEntry) error {
		sentEntries = append(sentEntries, e)
		return nil
	})
	now := time.Now()
	// ping-me 正常闲置；stale-me 预置为早已登记（将超龄）；fresh 刚活跃。
	KeepaliveObserve(map[string]any{"model": "m"}, "ping-me", 1, "cn", "m", now)
	KeepaliveObserve(map[string]any{"model": "m"}, "stale-me", 2, "cn", "m", now.Add(-120*time.Second))
	KeepaliveObserve(map[string]any{"model": "m"}, "fresh", 3, "cn", "m", now.Add(-5*time.Second))

	// 未到期：不发送；stale-me 预置 120s 前登记，闲置 130s >= max_age 60 →
	// 首次巡检即超龄淘汰。
	s, f, st := KeepaliveTick(now.Add(10 * time.Second))
	if s != 0 || f != 0 || st != 1 {
		t.Fatalf("too early: sent=%d failed=%d stale=%d", s, f, st)
	}
	// 到期（T=now+40s）：ping-me 闲置 40s（>=30 发送）；fresh 刷新后不动。
	// （stale-me 已在上一轮淘汰。）
	KeepaliveObserve(map[string]any{"model": "m"}, "fresh", 3, "cn", "m", now.Add(39*time.Second))
	s, f, st = KeepaliveTick(now.Add(40 * time.Second))
	if s != 1 || f != 0 || st != 0 {
		t.Fatalf("due: sent=%d failed=%d stale=%d", s, f, st)
	}
	if len(sentEntries) != 1 || sentEntries[0].SessionID != "ping-me" {
		t.Fatalf("sent = %+v", sentEntries)
	}
	// 发送成功后会话刷新：紧接着再 tick 不重发。
	s, _, _ = KeepaliveTick(now.Add(41 * time.Second))
	if s != 0 {
		t.Fatalf("just pinged session must not re-send: %d", s)
	}
	// sender 失败 → 淘汰（下轮不再发）。80s 时 ping-me（闲置 40s）与 fresh
	//（闲置 41s）都到期，双双失败被淘汰。
	ConfigureKeepalive(func(e KeepaliveEntry) error { return errBoom })
	s, f, _ = KeepaliveTick(now.Add(80 * time.Second))
	if s != 0 || f != 2 {
		t.Fatalf("failure path: sent=%d failed=%d", s, f)
	}
	s, _, _ = KeepaliveTick(now.Add(160 * time.Second))
	if s != 0 {
		t.Fatal("failed sessions must be evicted")
	}
}

var errBoom = &keepaliveTestError{}

type keepaliveTestError struct{}

func (*keepaliveTestError) Error() string { return "boom" }
