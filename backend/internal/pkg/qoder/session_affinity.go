package qoder

// session_affinity.go 二开新增：前缀会话亲和（opt-in），移植自 qoder2api-hub
// commit 9119ef6 的 qoder_session_cache.py + qoder_proxy.py assign_prefix_session。
//
// 语义：相同出站消息前缀优先复用已有的上游 session_id。保存的是
// 「前缀摘要 → 上游 session UUID」的路由索引，**不是**模型 KV Cache，也不保存
// 对话正文、上游访问 token 或 API Key 明文——Redis 键只存 HMAC 摘要，值是随机
// UUID。实际推理缓存由上游控制，稳定 session 不保证每次缓存命中。
//
// 哈希口径（源 keys() L107-135）：
//   - 锚点 = 第一条非空 user 消息（system-only 前缀被大量无关会话共享，不能只凭
//     共同 system 合并会话）；
//   - 作用域 = [tenant_scope, account_uid, realm, model_key, client_session_key,
//     context] 的规范化 JSON → HMAC-SHA256 截 32 hex（跨 API Key/账号/区域/模型/
//     显式客户端会话/工具与参数配置互不共享；HMAC 避免离线猜私有 prompt）；
//   - 消息流 = 逐条规范化 JSON + 8 字节大端长度前缀累积进 HMAC；
//   - 键 = "qoder:session:v2:{scope_digest}:{state_digest}"，超过 128 条时保留
//     锚点 + 最新 127 条（保住锚点与最有用的长前缀候选，限制命令体积）。
//
// 分配口径（源 _assign / Lua ASSIGN）：按**最长命中前缀**取值，未命中生成新
// UUID；把选中值写回锚点键与当前完整前缀键（TTL 续期），中间命中键也续期。
//
// 与源的差异（刻意，均已在下方注明）：
//   - hash_secret 源取面板持久化密钥（settings.json），so 无对应凭证库：改用
//     env QD_SESSION_HASH_SECRET；缺省时退进程内随机密钥（内存后端完全够用；
//     多进程共享 Redis 必须配该 env，否则各进程键空间不互通）。
//   - Redis 后端：源是零依赖 RESP2 客户端。so 侧改为 SessionExecutor 接口注入
//     （保持 pkg/qoder 纯标准库优先），生产默认仍走本文件的 stdlib RESP2 实现。
//   - 源把 tenant_scope（API Key 标识）写进作用域；so 的 BuildAgentChatBody 拿不到
//     API Key 上下文，经可选 AffinityScope 传入，缺省为空（账号/区域/模型/客户端
//     会话/工具与参数仍各自隔离，见 keys）。

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// 作用域与键派生
// ---------------------------------------------------------------------------

// AffinityScope 会话亲和的作用域隔离维度（源 keys() 的 scope 元组）。
// 任一维度不同即视为不同会话族，互不共享 session_id。
type AffinityScope struct {
	// TenantScope API Key 标识（so 侧调用方按需传入；空 = 不按 Key 隔离）。
	TenantScope string
	// AccountUID 上游账号 UID（换号后重新查找该账号自己的映射）。
	AccountUID string
	// Realm 区域（cn/intl）。
	Realm string
	// ModelKey 上游模型 key（建议传解析后的 key，而非显示名）。
	ModelKey string
	// ClientSessionKey 客户端显式会话 ID（X-Conversation-Id 等；空 = 同 Key 下
	// 完全相同的初始前缀可共享，需要独立对话时由调用方显式传）。
	ClientSessionKey string
	// Context 工具/参数/agent 配置摘要（源 {"tools","parameters","agent_id"}）。
	Context map[string]any
}

// affinityKeys 计算前缀哈希键列表（源 PrefixSessionCache.keys）。
// messages 为出站 body 的 messages；无锚点（无非空 user 消息）返回 nil。
func affinityKeys(secret []byte, messages []any, scope AffinityScope) []string {
	// 锚点：第一条 role=user 且 content 非空的消息（1-based，含自身）。
	anchor := 0
	for i, m := range messages {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if mm["role"] != "user" {
			continue
		}
		if s, _ := mm["content"].(string); s == "" {
			continue
		}
		if mm["content"] == nil {
			continue
		}
		anchor = i + 1
		break
	}
	if anchor == 0 {
		return nil
	}

	scopeJSON := canonicalJSON([]any{
		scope.TenantScope, scope.AccountUID, scope.Realm, scope.ModelKey,
		scope.ClientSessionKey, scope.Context,
	})
	scopeDigest := hmacHex(secret, scopeJSON)

	state := hmac.New(sha256.New, secret)
	out := make([]string, 0, len(messages)-anchor+1)
	for index, message := range messages {
		blob := canonicalJSON(message)
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(blob)))
		_, _ = state.Write(lenBuf[:])
		_, _ = state.Write(blob)
		if index+1 >= anchor {
			out = append(out, fmt.Sprintf("qoder:session:v2:{%s}:%x", scopeDigest, state.Sum(nil)))
		}
	}
	// 限制命令体积：保住锚点（最短前缀）与最新 127 条（最有用的长前缀候选）。
	if len(out) > 128 {
		tail := make([]string, 0, 128)
		tail = append(tail, out[0])
		tail = append(tail, out[len(out)-127:]...)
		out = tail
	}
	return out
}

// canonicalJSON 规范化 JSON（map 键排序、紧凑分隔符），对齐 Python
// json.dumps(..., sort_keys=True, separators=(",", ":"))。仅用作 HMAC 输入，
// 无需与 Python 进程互通（缓存键只要求本部署内稳定）。
func canonicalJSON(v any) []byte {
	b, err := json.Marshal(canonicalize(v))
	if err != nil {
		return []byte("null")
	}
	return b
}

// canonicalize 递归规整：map 键排序由 encoding/json 保证（Go 对 map[string]any
// 的 Marshal 按键排序），这里只需把非 map/slice 原样透传。
func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = canonicalize(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = canonicalize(val)
		}
		return out
	default:
		return v
	}
}

func hmacHex(secret, msg []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(msg)
	return fmt.Sprintf("%x", mac.Sum(nil))[:32]
}

// ---------------------------------------------------------------------------
// AffinityStore 契约
// ---------------------------------------------------------------------------

// AffinityStore 前缀会话亲和存储：把 (作用域, 消息前缀) 哈希映射到稳定 session UUID。
type AffinityStore interface {
	// Backend 后端名（"memory" / "redis"），用于用量记录里的 session_affinity 标记。
	Backend() string
	// Resolve 按 keys（短前缀→长前缀）选最长命中并写回锚点与当前前缀；未命中生成
	// 并写入新 UUID。keys 为空时返回空串（调用方回落随机 session）。
	Resolve(keys []string) (string, error)
}

// SessionExecutor Redis 命令执行抽象（测试注入假实现；生产为 RESP2 客户端）。
// 参数为 RESP2 命令的各 token，返回简单字符串/整数/批量串/数组的 Go 表示。
type SessionExecutor interface {
	Do(ctx context.Context, args ...string) (any, error)
}

// ---------------------------------------------------------------------------
// 内存后端（LRU + TTL）
// ---------------------------------------------------------------------------

// MemorySessionStore 有界、线程安全的 TTL/LRU 映射（源 MemorySessionCache）。
type MemorySessionStore struct {
	ttl        time.Duration
	maxEntries int

	mu      sync.Mutex
	entries map[string]memoryEntry
	order   []string // LRU：order[0] 最旧
}

type memoryEntry struct {
	value    string
	expireAt time.Time
}

// NewMemorySessionStore 创建内存后端。ttl/maxEntries 已按源口径夹取
// （ttl 60–86400s，maxEntries 1–100000）。
func NewMemorySessionStore(ttl time.Duration, maxEntries int) *MemorySessionStore {
	if ttl < 60*time.Second {
		ttl = 60 * time.Second
	}
	if ttl > 24*time.Hour {
		ttl = 24 * time.Hour
	}
	if maxEntries < 1 {
		maxEntries = 1
	}
	if maxEntries > 100000 {
		maxEntries = 100000
	}
	return &MemorySessionStore{
		ttl:        ttl,
		maxEntries: maxEntries,
		entries:    map[string]memoryEntry{},
	}
}

// Backend 后端名。
func (s *MemorySessionStore) Backend() string { return "memory" }

// Resolve 最长前缀命中 → 复用；否则生成新 UUID 并写回锚点与当前前缀。
// 语义与源 MemorySessionCache._assign 逐条对齐（含中间命中键续期、LRU 淘汰）。
func (s *MemorySessionStore) Resolve(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	selected := ""
	var matched string
	for i := len(keys) - 1; i >= 0; i-- {
		key := keys[i]
		entry, ok := s.entries[key]
		if !ok {
			continue
		}
		if !entry.expireAt.After(now) {
			delete(s.entries, key)
			s.removeOrder(key)
			continue
		}
		selected = entry.value
		matched = key
		break
	}
	if selected == "" {
		selected = UUID4()
	}
	// 写回锚点 + 当前完整前缀（去重），中间命中键也续期——与 Redis 事务一致。
	writes := make([]string, 0, 3)
	if matched != "" {
		writes = append(writes, matched)
	}
	writes = append(writes, keys[0])
	if len(keys) > 1 {
		writes = append(writes, keys[len(keys)-1])
	}
	seen := map[string]bool{}
	for _, key := range writes {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		s.putLocked(key, selected, now.Add(s.ttl))
	}
	for len(s.entries) > s.maxEntries {
		s.evictOldestLocked()
	}
	return selected, nil
}

// putLocked 写入/更新并移到 LRU 尾部。
func (s *MemorySessionStore) putLocked(key, value string, expireAt time.Time) {
	if _, ok := s.entries[key]; ok {
		s.removeOrder(key)
	}
	s.entries[key] = memoryEntry{value: value, expireAt: expireAt}
	s.order = append(s.order, key)
}

func (s *MemorySessionStore) removeOrder(key string) {
	for i, k := range s.order {
		if k == key {
			s.order = append(s.order[:i], s.order[i+1:]...)
			return
		}
	}
}

func (s *MemorySessionStore) evictOldestLocked() {
	if len(s.order) == 0 {
		return
	}
	oldest := s.order[0]
	s.order = s.order[1:]
	delete(s.entries, oldest)
}

// ---------------------------------------------------------------------------
// Redis 后端（最长前缀原子选值）
// ---------------------------------------------------------------------------

// redisAssignScript 一次事务内按最长前缀选值并声明锚点与当前前缀（源 ASSIGN）。
// 所有键共用 hash tag {}，保证同槽；本客户端不跟随 Cluster MOVED/ASK 重定向。
const redisAssignScript = `
local selected = false
local matched = 0
for i = #KEYS, 1, -1 do
    local value = redis.call('GET', KEYS[i])
    if value then
        selected = value
        matched = i
        break
    end
end
if not selected then selected = ARGV[1] end
redis.call('SET', KEYS[1], selected, 'EX', ARGV[2])
if #KEYS > 1 then
    redis.call('SET', KEYS[#KEYS], selected, 'EX', ARGV[2])
end
if matched > 1 and matched < #KEYS then
    redis.call('EXPIRE', KEYS[matched], ARGV[2])
end
return selected
`

// RedisSessionStore Redis 后端（源 PrefixSessionCache，Redis Lua 原子分配）。
type RedisSessionStore struct {
	exec SessionExecutor
	ttl  time.Duration
}

// NewRedisSessionStore 创建 Redis 后端。ttl 同内存后端夹取口径。
func NewRedisSessionStore(exec SessionExecutor, ttl time.Duration) *RedisSessionStore {
	if ttl < 60*time.Second {
		ttl = 60 * time.Second
	}
	if ttl > 24*time.Hour {
		ttl = 24 * time.Hour
	}
	return &RedisSessionStore{exec: exec, ttl: ttl}
}

// Backend 后端名。
func (s *RedisSessionStore) Backend() string { return "redis" }

// Resolve 走 EVAL 原子分配（源 _assign）。
func (s *RedisSessionStore) Resolve(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", nil
	}
	args := make([]string, 0, len(keys)+5)
	args = append(args, "EVAL", redisAssignScript, strconv.Itoa(len(keys)))
	args = append(args, keys...)
	args = append(args, UUID4(), strconv.Itoa(int(s.ttl/time.Second)))
	reply, err := s.exec.Do(context.Background(), args...)
	if err != nil {
		return "", err
	}
	switch v := reply.(type) {
	case string:
		if v == "" {
			return "", errors.New("qoder: empty session id from redis")
		}
		return v, nil
	case []byte:
		if len(v) == 0 {
			return "", errors.New("qoder: empty session id from redis")
		}
		return string(v), nil
	default:
		return "", fmt.Errorf("qoder: unexpected redis reply type %T", reply)
	}
}

// ---------------------------------------------------------------------------
// stdlib RESP2 客户端（源 RedisClient 的 Go 对应物，无第三方依赖）
// ---------------------------------------------------------------------------

// RESP2Executor 每次命令一条短连接的 RESP2 客户端（AUTH/SELECT 后发命令）。
type RESP2Executor struct {
	host     string
	port     int
	database int
	username string
	password string
	tls      bool
	timeout  time.Duration
}

// NewRESP2ExecutorFromURL 解析 redis:// / rediss:// URL。
func NewRESP2ExecutorFromURL(raw string, timeout time.Duration) (*RESP2Executor, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("qoder: parse session redis url: %w", err)
	}
	if u.Scheme != "redis" && u.Scheme != "rediss" {
		return nil, errors.New("qoder: QD_SESSION_REDIS_URL must be redis:// or rediss://")
	}
	if u.Hostname() == "" {
		return nil, errors.New("qoder: QD_SESSION_REDIS_URL missing host")
	}
	port := 6379
	if p := u.Port(); p != "" {
		if n, cerr := strconv.Atoi(p); cerr == nil && n > 0 {
			port = n
		}
	}
	db := 0
	if seg := strings.Trim(u.Path, "/"); seg != "" {
		if n, cerr := strconv.Atoi(seg); cerr == nil {
			if n < 0 || n > 15 {
				return nil, errors.New("qoder: redis database must be between 0 and 15")
			}
			db = n
		}
	}
	exec := &RESP2Executor{
		host:     u.Hostname(),
		port:     port,
		database: db,
		tls:      u.Scheme == "rediss",
		timeout:  timeout,
	}
	if u.User != nil {
		exec.username = u.User.Username()
		exec.password, _ = u.User.Password()
	}
	if exec.timeout <= 0 {
		exec.timeout = 400 * time.Millisecond
	}
	return exec, nil
}

// Do 执行单条命令（每命令一连接，对齐源 execute 的短连接语义）。
func (e *RESP2Executor) Do(ctx context.Context, args ...string) (any, error) {
	if len(args) == 0 {
		return nil, errors.New("qoder: empty redis command")
	}
	dialer := &net.Dialer{Timeout: e.timeout}
	addr := net.JoinHostPort(e.host, strconv.Itoa(e.port))
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(e.timeout))
	if e.tls {
		tcfg := &tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12}
		tconn := tls.Client(conn, tcfg)
		if err := tconn.Handshake(); err != nil {
			return nil, err
		}
		defer tconn.Close()
		conn = tconn
		_ = conn.SetDeadline(time.Now().Add(e.timeout))
	}
	reader := bufio.NewReader(conn)
	if e.password != "" {
		auth := []string{"AUTH", e.password}
		if e.username != "" {
			auth = []string{"AUTH", e.username, e.password}
		}
		if err := writeCommand(conn, auth); err != nil {
			return nil, err
		}
		if reply, err := readReply(reader); err != nil {
			return nil, err
		} else if s, _ := reply.(string); s != "OK" {
			return nil, errors.New("qoder: redis authentication failed")
		}
	}
	if e.database != 0 {
		if err := writeCommand(conn, []string{"SELECT", strconv.Itoa(e.database)}); err != nil {
			return nil, err
		}
		if reply, err := readReply(reader); err != nil {
			return nil, err
		} else if s, _ := reply.(string); s != "OK" {
			return nil, errors.New("qoder: redis database selection failed")
		}
	}
	if err := writeCommand(conn, args); err != nil {
		return nil, err
	}
	return readReply(reader)
}

func writeCommand(conn net.Conn, parts []string) error {
	var buf []byte
	buf = append(buf, '*')
	buf = strconv.AppendInt(buf, int64(len(parts)), 10)
	buf = append(buf, '\r', '\n')
	for _, p := range parts {
		buf = append(buf, '$')
		buf = strconv.AppendInt(buf, int64(len(p)), 10)
		buf = append(buf, '\r', '\n')
		buf = append(buf, p...)
		buf = append(buf, '\r', '\n')
	}
	_, err := conn.Write(buf)
	return err
}

// readReply 解析 RESP2 回复（+ 简单串 / - 错误 / : 整数 / $ 批量串 / * 数组）。
func readReply(r *bufio.Reader) (any, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	switch kind {
	case '+':
		return line, nil
	case '-':
		return nil, errors.New("redis: " + line)
	case ':':
		return strconv.ParseInt(line, 10, 64)
	case '$':
		size, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if size < 0 {
			return nil, nil
		}
		if size > 1024*1024 {
			return nil, errors.New("redis: invalid bulk length")
		}
		payload := make([]byte, size+2)
		if _, err := ioReadFull(r, payload); err != nil {
			return nil, err
		}
		return string(payload[:size]), nil
	case '*':
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, nil
		}
		if count > 10000 {
			return nil, errors.New("redis: invalid array length")
		}
		out := make([]any, 0, count)
		for i := 0; i < count; i++ {
			item, err := readReply(r)
			if err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, errors.New("redis: unsupported reply type")
	}
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// ---------------------------------------------------------------------------
// opt-in 配置（环境变量名与源对齐）
// ---------------------------------------------------------------------------

// SessionCacheConfig 前缀会话亲和的运行时配置。
type SessionCacheConfig struct {
	Mode       string // off / memory / redis
	TTL        time.Duration
	MaxEntries int
	RedisURL   string
	Timeout    time.Duration
}

// SessionCacheConfigFromEnv 读环境变量（源 prefix_session_cache）：
//
//	QD_SESSION_CACHE           off / memory / redis（缺省 off；配了 Redis URL 时
//	                           缺省 redis；显式 off 优先）
//	QD_SESSION_CACHE_TTL       映射有效期秒，60–86400，缺省 3600（命中后续期）
//	QD_SESSION_CACHE_MAX_ENTRIES  内存后端条数上限，1–100000，缺省 4096
//	QD_SESSION_REDIS_URL       redis://[:password@]host:6379/0 或 rediss://
//	QD_SESSION_REDIS_TIMEOUT   每次连接/读写超时秒，缺省 0.4
//	QD_SESSION_REDIS_TTL       Redis 初版别名；QD_SESSION_CACHE_TTL 优先
func SessionCacheConfigFromEnv() (SessionCacheConfig, error) {
	cfg := SessionCacheConfig{
		TTL:        3600 * time.Second,
		MaxEntries: 4096,
		Timeout:    400 * time.Millisecond,
	}
	cfg.RedisURL = strings.TrimSpace(os.Getenv("QD_SESSION_REDIS_URL"))
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("QD_SESSION_CACHE")))
	if mode == "" {
		if cfg.RedisURL != "" {
			mode = "redis"
		} else {
			mode = "off"
		}
	}
	switch mode {
	case "off", "memory", "redis":
		cfg.Mode = mode
	default:
		return cfg, errors.New("qoder: QD_SESSION_CACHE must be off, memory or redis")
	}
	if raw := strings.TrimSpace(os.Getenv("QD_SESSION_CACHE_TTL")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.TTL = time.Duration(n) * time.Second
		}
	} else if raw := strings.TrimSpace(os.Getenv("QD_SESSION_REDIS_TTL")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.TTL = time.Duration(n) * time.Second
		}
	}
	if raw := strings.TrimSpace(os.Getenv("QD_SESSION_CACHE_MAX_ENTRIES")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.MaxEntries = n
		}
	}
	if raw := strings.TrimSpace(os.Getenv("QD_SESSION_REDIS_TIMEOUT")); raw != "" {
		if f, err := strconv.ParseFloat(raw, 64); err == nil && f > 0 {
			cfg.Timeout = time.Duration(f * float64(time.Second))
		}
	}
	return cfg, nil
}

// sessionHashSecret 缓存键的 HMAC 密钥：env QD_SESSION_HASH_SECRET 优先；
// 缺省退进程内随机（内存后端够用；多进程 Redis 共享必须配 env，否则键空间不互通）。
// 源取面板持久化密钥，so 无对应凭证库——见文件头差异说明。
func sessionHashSecret() []byte {
	if s := strings.TrimSpace(os.Getenv("QD_SESSION_HASH_SECRET")); s != "" {
		return []byte(s)
	}
	sessionSecretOnce.Do(func() {
		sessionSecretValue = []byte(UUID4() + UUID4())
	})
	return sessionSecretValue
}

var (
	sessionSecretOnce  sync.Once
	sessionSecretValue []byte
)

// ---------------------------------------------------------------------------
// 组装与默认实例
// ---------------------------------------------------------------------------

// affinityStoreOnce 进程级默认 store（按 env 配置惰性构造一次）。
var (
	affinityStoreOnce sync.Once
	affinityStore     AffinityStore
	affinityStoreErr  error
)

// DefaultAffinityStore 返回进程级 store；模式为 off 时返回 nil（调用方回落随机
// session）。配置错误同样返回 nil + err（不阻断推理）。
func DefaultAffinityStore() (AffinityStore, error) {
	affinityStoreOnce.Do(func() {
		affinityStore, affinityStoreErr = buildAffinityStore(nil)
	})
	return affinityStore, affinityStoreErr
}

// buildAffinityStore 按配置构造 store；exec 非 nil 时用于 Redis 后端（测试注入）。
func buildAffinityStore(exec SessionExecutor) (AffinityStore, error) {
	cfg, err := SessionCacheConfigFromEnv()
	if err != nil {
		return nil, err
	}
	switch cfg.Mode {
	case "off":
		return nil, nil
	case "memory":
		return NewMemorySessionStore(cfg.TTL, cfg.MaxEntries), nil
	case "redis":
		if exec == nil {
			exec, err = NewRESP2ExecutorFromURL(cfg.RedisURL, cfg.Timeout)
			if err != nil {
				return nil, err
			}
		}
		return NewRedisSessionStore(exec, cfg.TTL), nil
	default:
		return nil, fmt.Errorf("qoder: unknown session cache mode %q", cfg.Mode)
	}
}

// ResetAffinityStoreForTest 清空进程级 store（测试用）。
func ResetAffinityStoreForTest() {
	affinityStoreOnce = sync.Once{}
	affinityStore = nil
	affinityStoreErr = nil
}

// AssignSessionID 在 body 上分配 session_id（源 assign_prefix_session）：
//
//	始终先重置为随机 UUID（前一个失败账号可能已占过 ID）；亲和可用时按前缀
//	哈希复用稳定 UUID，失败也只回落随机——可选优化不得打断推理，且日志不打印
//	连接地址 / 密码 / 消息 / 异常详情。
//
// 返回 (session_id, affinity 标记)：标记用于用量记录（memory-prefix /
// redis-prefix / random）。
func AssignSessionID(body map[string]any, scope AffinityScope) (string, string) {
	body["session_id"] = UUID4()
	mark := "random"
	store, err := DefaultAffinityStore()
	if err != nil || store == nil {
		return body["session_id"].(string), mark
	}
	messages := anyList(body["messages"])
	keys := affinityKeys(sessionHashSecret(), messages, scope)
	if len(keys) == 0 {
		return body["session_id"].(string), mark
	}
	chosen, rerr := store.Resolve(keys)
	if rerr != nil || chosen == "" {
		// 缓存故障不打断推理；不记录异常详情（可能带连接串/密码）。
		return body["session_id"].(string), mark
	}
	body["session_id"] = chosen
	return chosen, store.Backend() + "-prefix"
}
