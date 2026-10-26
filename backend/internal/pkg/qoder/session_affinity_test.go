//go:build unit

package qoder

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSessionExecutor 内存版 Redis：支持 EVAL(ASSIGN 脚本) / GET / SET / EXPIRE。
type fakeSessionExecutor struct {
	mu   sync.Mutex
	data map[string]string
	// failAll 非 nil 时所有命令返回该错误（模拟 Redis 不可达）。
	failAll error
	// evalCalls 记录 EVAL 调用的 key 数，便于断言原子分配被走。
	evalCalls int
}

func newFakeSessionExecutor() *fakeSessionExecutor {
	return &fakeSessionExecutor{data: map[string]string{}}
}

func (f *fakeSessionExecutor) Do(_ context.Context, args ...string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failAll != nil {
		return nil, f.failAll
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	switch args[0] {
	case "EVAL":
		// 形如 EVAL <script> <n> <key...> <uuid> <ttl>
		f.evalCalls++
		if len(args) < 4 {
			return nil, errors.New("bad EVAL")
		}
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return nil, err
		}
		keys := args[3 : 3+n]
		rest := args[3+n:]
		if len(rest) != 2 {
			return nil, errors.New("bad EVAL args")
		}
		fresh, ttl := rest[0], rest[1]
		// ASSIGN 语义：最长前缀命中；写回 KEYS[1] 与 KEYS[-1]。
		selected := fresh
		matched := -1
		for i := len(keys) - 1; i >= 0; i-- {
			if v, ok := f.data[keys[i]]; ok {
				selected = v
				matched = i
				break
			}
		}
		_ = ttl
		f.data[keys[0]] = selected
		if len(keys) > 1 {
			f.data[keys[len(keys)-1]] = selected
		}
		_ = matched
		return selected, nil
	case "GET":
		if len(args) != 2 {
			return nil, errors.New("bad GET")
		}
		v, ok := f.data[args[1]]
		if !ok {
			return nil, nil
		}
		return v, nil
	case "SET":
		if len(args) < 3 {
			return nil, errors.New("bad SET")
		}
		f.data[args[1]] = args[2]
		return "OK", nil
	default:
		return nil, errors.New("unsupported command " + args[0])
	}
}

// testSessionMessages 构造带 user 锚点的消息列表。
func testSessionMessages(prompt string) []any {
	return []any{
		map[string]any{"role": "system", "content": "private synthetic system"},
		map[string]any{"role": "user", "content": prompt},
	}
}

func testScope() AffinityScope {
	return AffinityScope{
		TenantScope:      "api-key-1",
		AccountUID:       "acct-a",
		Realm:            "intl",
		ModelKey:         "ultimate",
		ClientSessionKey: "client-1",
		Context:          map[string]any{"tools": []any{"diff"}},
	}
}

func TestAffinityKeysSystemOnlyHasNoAnchor(t *testing.T) {
	secret := []byte("test-secret")
	// 只有 system：无锚点，不建映射（避免大量无关会话共享 system 就合并）。
	msgs := []any{map[string]any{"role": "system", "content": "private synthetic system"}}
	if keys := affinityKeys(secret, msgs, testScope()); keys != nil {
		t.Fatalf("system-only should have no keys: %v", keys)
	}
	// user 消息 content 为空也不算锚点。
	msgs2 := []any{map[string]any{"role": "user", "content": ""}}
	if keys := affinityKeys(secret, msgs2, testScope()); keys != nil {
		t.Fatalf("empty user should have no keys: %v", keys)
	}
	// user-only 可以建映射。
	msgs3 := testSessionMessages("hello")
	if keys := affinityKeys(secret, msgs3, testScope()); len(keys) == 0 {
		t.Fatal("user message should create keys")
	}
}

func TestAffinityKeysScopeIsolation(t *testing.T) {
	secret := []byte("test-secret")
	base := affinityKeys(secret, testSessionMessages("hi"), testScope())
	if len(base) == 0 {
		t.Fatal("no keys")
	}
	variants := []struct {
		name  string
		scope AffinityScope
	}{
		{"account", AffinityScope{"api-key-1", "acct-b", "intl", "ultimate", "client-1", testScope().Context}},
		{"realm", AffinityScope{"api-key-1", "acct-a", "cn", "ultimate", "client-1", testScope().Context}},
		{"model", AffinityScope{"api-key-1", "acct-a", "intl", "fast", "client-1", testScope().Context}},
		{"conversation", AffinityScope{"api-key-1", "acct-a", "intl", "ultimate", "other", testScope().Context}},
		{"tenant", AffinityScope{"api-key-2", "acct-a", "intl", "ultimate", "client-1", testScope().Context}},
		{"tools", AffinityScope{"api-key-1", "acct-a", "intl", "ultimate", "client-1",
			map[string]any{"tools": []any{"other"}}}},
	}
	for _, v := range variants {
		keys := affinityKeys(secret, testSessionMessages("hi"), v.scope)
		if len(keys) == 0 {
			t.Fatalf("%s: no keys", v.name)
		}
		// 作用域不同 → 同前缀的最终键必须不同。
		if keys[len(keys)-1] == base[len(base)-1] {
			t.Fatalf("%s: scope not isolated", v.name)
		}
	}
	// 同作用域 + 同前缀 → 同键（稳定）。
	again := affinityKeys(secret, testSessionMessages("hi"), testScope())
	if again[len(again)-1] != base[len(base)-1] {
		t.Fatal("same scope+prefix should produce stable key")
	}
}

func TestAffinityKeysDoNotContainPrompt(t *testing.T) {
	secret := []byte("test-secret")
	keys := affinityKeys(secret, testSessionMessages("private synthetic prompt"), testScope())
	for _, k := range keys {
		if strings.Contains(k, "private") || strings.Contains(k, "synthetic") {
			t.Fatalf("key leaks prompt: %s", k)
		}
		if !strings.HasPrefix(k, "qoder:session:v2:{") {
			t.Fatalf("key shape: %s", k)
		}
	}
	// scope 哈希也不能回显 tenant/conversation 明文。
	if strings.Contains(keys[0], "api-key-1") || strings.Contains(keys[0], "client-1") {
		t.Fatalf("key leaks scope identifiers: %s", keys[0])
	}
}

func TestAffinityKeysLongConversationBounded(t *testing.T) {
	secret := []byte("test-secret")
	msgs := []any{map[string]any{"role": "user", "content": "first"}}
	for i := 0; i < 200; i++ {
		msgs = append(msgs,
			map[string]any{"role": "assistant", "content": "a" + strconv.Itoa(i)},
			map[string]any{"role": "user", "content": "u" + strconv.Itoa(i)},
		)
	}
	keys := affinityKeys(secret, msgs, testScope())
	if len(keys) > 128 {
		t.Fatalf("keys not bounded: %d", len(keys))
	}
	if len(keys) < 128 {
		t.Fatalf("expected full 128 for long conversation: %d", len(keys))
	}
	// 锚点（最短前缀）必须保留。
	first := affinityKeys(secret, msgs[:2], testScope())
	if keys[0] != first[0] {
		t.Fatal("anchor key must be preserved")
	}
}

func TestMemorySessionStoreReusesGrowingPrefix(t *testing.T) {
	store := NewMemorySessionStore(time.Hour, 4096)
	secret := []byte("test-secret")
	scope := testScope()
	head := testSessionMessages("first question")
	tail := append(append([]any{}, head...),
		map[string]any{"role": "assistant", "content": "OK"},
		map[string]any{"role": "user", "content": "second question"},
	)

	id1, err := store.Resolve(affinityKeys(secret, head, scope))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := store.Resolve(affinityKeys(secret, tail, scope))
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" || id1 != id2 {
		t.Fatalf("growing prefix should reuse session: %q vs %q", id1, id2)
	}
	// 再加一轮仍复用。
	tail2 := append(append([]any{}, tail...), map[string]any{"role": "user", "content": "third"})
	id3, _ := store.Resolve(affinityKeys(secret, tail2, scope))
	if id3 != id1 {
		t.Fatalf("third round: %q vs %q", id3, id1)
	}
}

func TestMemorySessionStoreIsolation(t *testing.T) {
	store := NewMemorySessionStore(time.Hour, 4096)
	secret := []byte("test-secret")
	base := testScope()
	head := testSessionMessages("hi")

	id1, _ := store.Resolve(affinityKeys(secret, head, base))
	// 换账号不共享。
	other := base
	other.AccountUID = "acct-b"
	id2, _ := store.Resolve(affinityKeys(secret, head, other))
	if id2 == id1 {
		t.Fatal("different account shared session")
	}
	// 换模型不共享。
	other = base
	other.ModelKey = "fast"
	id3, _ := store.Resolve(affinityKeys(secret, head, other))
	if id3 == id1 {
		t.Fatal("different model shared session")
	}
	// 换客户端会话不共享。
	other = base
	other.ClientSessionKey = "client-2"
	id4, _ := store.Resolve(affinityKeys(secret, head, other))
	if id4 == id1 {
		t.Fatal("different conversation shared session")
	}
	// 换 tenant 不共享。
	other = base
	other.TenantScope = "api-key-2"
	id5, _ := store.Resolve(affinityKeys(secret, head, other))
	if id5 == id1 {
		t.Fatal("different tenant shared session")
	}
}

func TestMemorySessionStoreTTLExpiry(t *testing.T) {
	// ttl 会夹取到最小 60s；直接改内部过期时刻模拟过期。
	store := NewMemorySessionStore(time.Hour, 4096)
	secret := []byte("test-secret")
	scope := testScope()
	head := testSessionMessages("hi")

	id1, _ := store.Resolve(affinityKeys(secret, head, scope))
	// 把所有条目拨到已过期。
	store.mu.Lock()
	for k, v := range store.entries {
		v.expireAt = time.Now().Add(-time.Second)
		store.entries[k] = v
	}
	store.mu.Unlock()
	id2, _ := store.Resolve(affinityKeys(secret, head, scope))
	if id2 == id1 {
		t.Fatal("expired mapping should produce fresh session")
	}
}

func TestMemorySessionStoreLRUEviction(t *testing.T) {
	store := NewMemorySessionStore(time.Hour, 2)
	secret := []byte("test-secret")
	scope := testScope()

	// 写入两条不同前缀的映射，再写第三条应淘汰最旧。
	for i, prompt := range []string{"a", "b", "c"} {
		msgs := testSessionMessages(prompt)
		if _, err := store.Resolve(affinityKeys(secret, msgs, scope)); err != nil {
			t.Fatal(err)
		}
		store.mu.Lock()
		n := len(store.entries)
		store.mu.Unlock()
		if n > 2 {
			t.Fatalf("step %d: entries not bounded: %d", i, n)
		}
	}
	// 最早 "a" 的映射应已被淘汰，重新 resolve 得到新 ID。
	store.mu.Lock()
	n := len(store.entries)
	store.mu.Unlock()
	if n != 2 {
		t.Fatalf("expected 2 entries after eviction, got %d", n)
	}
}

func TestMemorySessionStoreConcurrentCreatorsAgree(t *testing.T) {
	store := NewMemorySessionStore(time.Hour, 4096)
	secret := []byte("test-secret")
	scope := testScope()
	keys := affinityKeys(secret, testSessionMessages("shared"), scope)

	const workers = 24
	results := make([]string, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id, err := store.Resolve(keys)
			if err != nil {
				t.Errorf("worker %d: %v", idx, err)
				return
			}
			results[idx] = id
		}(i)
	}
	wg.Wait()
	for i := 1; i < workers; i++ {
		if results[i] != results[0] {
			t.Fatalf("concurrent creators disagreed: %q vs %q", results[i], results[0])
		}
	}
}

func TestMemorySessionStoreLongestPrefixWins(t *testing.T) {
	store := NewMemorySessionStore(time.Hour, 4096)
	secret := []byte("test-secret")
	scope := testScope()
	head := testSessionMessages("first")
	tail := append(append([]any{}, head...),
		map[string]any{"role": "assistant", "content": "OK"},
		map[string]any{"role": "user", "content": "second"},
	)
	longKeys := affinityKeys(secret, tail, scope)
	if len(longKeys) < 2 {
		t.Fatalf("need multiple prefix keys, got %d", len(longKeys))
	}
	anchorVal := "11111111-1111-4111-8111-111111111111"
	tailVal := "49eb7237-af62-4cc7-85cc-7f112f249b9f"
	now := time.Now().Add(time.Hour)
	// 锚点（最短前缀）与完整前缀写入不同值：最长命中必须赢。
	store.mu.Lock()
	store.putLocked(longKeys[0], anchorVal, now)
	store.putLocked(longKeys[len(longKeys)-1], tailVal, now)
	store.mu.Unlock()
	got, err := store.Resolve(longKeys)
	if err != nil {
		t.Fatal(err)
	}
	if got != tailVal {
		t.Fatalf("longest prefix must win: got %q want %q", got, tailVal)
	}
	// 只有锚点键的短前缀会话会共享同一 session（源语义：锚点=会话起点，
	// 写回锚点即让同起点的短会话复用，刻意如此）。
	shortKeys := affinityKeys(secret, head, scope)
	if len(shortKeys) == 0 {
		t.Fatal("short keys empty")
	}
	got2, _ := store.Resolve(shortKeys)
	if got2 != tailVal {
		t.Fatalf("anchor write-back shares session: got %q want %q", got2, tailVal)
	}
}

func TestRedisSessionStoreAssign(t *testing.T) {
	exec := newFakeSessionExecutor()
	store := NewRedisSessionStore(exec, time.Hour)
	secret := []byte("test-secret")
	scope := testScope()
	head := testSessionMessages("first question")
	tail := append(append([]any{}, head...),
		map[string]any{"role": "assistant", "content": "OK"},
		map[string]any{"role": "user", "content": "second"},
	)

	id1, err := store.Resolve(affinityKeys(secret, head, scope))
	if err != nil {
		t.Fatal(err)
	}
	id2, err := store.Resolve(affinityKeys(secret, tail, scope))
	if err != nil {
		t.Fatal(err)
	}
	if id1 == "" || id1 != id2 {
		t.Fatalf("redis growing prefix: %q vs %q", id1, id2)
	}
	if exec.evalCalls == 0 {
		t.Fatal("EVAL not used")
	}
	// 键里只存哈希与 UUID，不含明文。
	for k, v := range exec.data {
		if strings.Contains(k, "private") || strings.Contains(v, "private") {
			t.Fatalf("redis stores plaintext: %q=%q", k, v)
		}
	}
	// 同 hash tag（同槽）。
	var tag string
	for k := range exec.data {
		i := strings.Index(k, "{")
		j := strings.Index(k, "}")
		if i < 0 || j < 0 {
			t.Fatalf("key missing hash tag: %s", k)
		}
		if tag == "" {
			tag = k[i+1 : j]
		} else if k[i+1:j] != tag {
			t.Fatalf("hash slot mismatch: %s vs %s", tag, k[i+1:j])
		}
	}
}

func TestRedisSessionStoreDifferentScopesDoNotShare(t *testing.T) {
	exec := newFakeSessionExecutor()
	store := NewRedisSessionStore(exec, time.Hour)
	secret := []byte("test-secret")
	base := testScope()
	head := testSessionMessages("hi")

	id1, _ := store.Resolve(affinityKeys(secret, head, base))
	other := base
	other.AccountUID = "acct-b"
	id2, _ := store.Resolve(affinityKeys(secret, head, other))
	if id1 == id2 {
		t.Fatal("redis scope leak")
	}
}

func TestRedisSessionStoreErrorDoesNotPanic(t *testing.T) {
	exec := newFakeSessionExecutor()
	exec.failAll = errors.New("connection refused")
	store := NewRedisSessionStore(exec, time.Hour)
	secret := []byte("test-secret")
	if _, err := store.Resolve(affinityKeys(secret, testSessionMessages("hi"), testScope())); err == nil {
		t.Fatal("expected error from failing redis")
	}
}

func TestSessionCacheConfigFromEnv(t *testing.T) {
	// 默认 off（无 Redis URL）。
	t.Setenv("QD_SESSION_CACHE", "")
	t.Setenv("QD_SESSION_REDIS_URL", "")
	cfg, err := SessionCacheConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "off" {
		t.Fatalf("default mode = %q, want off", cfg.Mode)
	}
	// 配了 Redis URL 时缺省 redis。
	t.Setenv("QD_SESSION_REDIS_URL", "redis://localhost:6379/0")
	cfg, err = SessionCacheConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "redis" {
		t.Fatalf("url mode = %q, want redis", cfg.Mode)
	}
	// 显式 off 优先。
	t.Setenv("QD_SESSION_CACHE", "off")
	cfg, err = SessionCacheConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != "off" {
		t.Fatalf("explicit off = %q", cfg.Mode)
	}
	// 非法值报错。
	t.Setenv("QD_SESSION_CACHE", "bogus")
	if _, err := SessionCacheConfigFromEnv(); err == nil {
		t.Fatal("expected error for bogus mode")
	}
	// TTL 与别名。
	t.Setenv("QD_SESSION_CACHE", "memory")
	t.Setenv("QD_SESSION_CACHE_TTL", "7200")
	t.Setenv("QD_SESSION_REDIS_TTL", "111")
	cfg, _ = SessionCacheConfigFromEnv()
	if cfg.TTL != 7200*time.Second {
		t.Fatalf("ttl = %v", cfg.TTL)
	}
	t.Setenv("QD_SESSION_CACHE_TTL", "")
	cfg, _ = SessionCacheConfigFromEnv()
	if cfg.TTL != 111*time.Second {
		t.Fatalf("alias ttl = %v", cfg.TTL)
	}
	t.Setenv("QD_SESSION_CACHE_MAX_ENTRIES", "10")
	cfg, _ = SessionCacheConfigFromEnv()
	if cfg.MaxEntries != 10 {
		t.Fatalf("max entries = %d", cfg.MaxEntries)
	}
}

func TestAssignSessionIDDisabledKeepsRandom(t *testing.T) {
	// 显式 off：每次随机（即使同前缀）。
	t.Setenv("QD_SESSION_CACHE", "off")
	ResetAffinityStoreForTest()
	t.Cleanup(ResetAffinityStoreForTest)

	body := map[string]any{"messages": testSessionMessages("hi")}
	id1, mark1 := AssignSessionID(body, testScope())
	if mark1 != "random" {
		t.Fatalf("mark = %q, want random", mark1)
	}
	id2, _ := AssignSessionID(body, testScope())
	if id1 == id2 {
		t.Fatal("disabled cache must produce fresh session each time")
	}
	// 前一个失败账号残留的 session_id 必须被重置（源 assign_prefix_session 注释）。
	body["session_id"] = "stale-id-from-failed-account"
	id3, _ := AssignSessionID(body, testScope())
	if id3 == "stale-id-from-failed-account" {
		t.Fatal("stale session id must be reset")
	}
}

func TestAssignSessionIDEnabledReuses(t *testing.T) {
	t.Setenv("QD_SESSION_CACHE", "memory")
	t.Setenv("QD_SESSION_CACHE_TTL", "3600")
	t.Setenv("QD_SESSION_CACHE_MAX_ENTRIES", "64")
	ResetAffinityStoreForTest()
	t.Cleanup(ResetAffinityStoreForTest)

	body := map[string]any{"messages": testSessionMessages("hi")}
	id1, mark1 := AssignSessionID(body, testScope())
	if mark1 != "memory-prefix" {
		t.Fatalf("mark = %q, want memory-prefix", mark1)
	}
	id2, _ := AssignSessionID(body, testScope())
	if id1 == "" || id1 != id2 {
		t.Fatalf("enabled cache should reuse: %q vs %q", id1, id2)
	}
	// 换作用域不复用。
	other := testScope()
	other.AccountUID = "acct-z"
	id3, _ := AssignSessionID(body, other)
	if id3 == id1 {
		t.Fatal("scope change must not reuse")
	}
	// 无 user 锚点 → 随机。
	sysBody := map[string]any{"messages": []any{
		map[string]any{"role": "system", "content": "sys"}}}
	id4, mark4 := AssignSessionID(sysBody, testScope())
	if mark4 != "random" {
		t.Fatalf("system-only mark = %q, want random", mark4)
	}
	if id4 == "" {
		t.Fatal("system-only must still assign a session id")
	}
}

func TestAssignSessionIDCacheFailureKeepsInferring(t *testing.T) {
	// Redis 不可达：回退随机，不 panic、不返回错误。
	t.Setenv("QD_SESSION_CACHE", "redis")
	t.Setenv("QD_SESSION_REDIS_URL", "redis://127.0.0.1:1/0")
	t.Setenv("QD_SESSION_REDIS_TIMEOUT", "0.05")
	ResetAffinityStoreForTest()
	t.Cleanup(ResetAffinityStoreForTest)

	body := map[string]any{"messages": testSessionMessages("hi")}
	id, mark := AssignSessionID(body, testScope())
	if id == "" {
		t.Fatal("must assign session id even on cache failure")
	}
	if mark != "random" {
		t.Fatalf("mark = %q, want random on failure", mark)
	}
	if _, ok := body["session_id"].(string); !ok {
		t.Fatal("body session_id must be set")
	}
}

func TestNewRESP2ExecutorFromURL(t *testing.T) {
	e, err := NewRESP2ExecutorFromURL("redis://:secret@db.example:6380/3", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if e.host != "db.example" || e.port != 6380 || e.database != 3 || e.password != "secret" {
		t.Fatalf("parsed: %+v", e)
	}
	e2, err := NewRESP2ExecutorFromURL("rediss://user:pass@h/15", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !e2.tls || e2.username != "user" || e2.database != 15 {
		t.Fatalf("parsed tls: %+v", e2)
	}
	if _, err := NewRESP2ExecutorFromURL("http://x", time.Second); err == nil {
		t.Fatal("http scheme must be rejected")
	}
	if _, err := NewRESP2ExecutorFromURL("redis://h/16", time.Second); err == nil {
		t.Fatal("db 16 must be rejected")
	}
	if _, err := NewRESP2ExecutorFromURL("redis://", time.Second); err == nil {
		t.Fatal("missing host must be rejected")
	}
}
