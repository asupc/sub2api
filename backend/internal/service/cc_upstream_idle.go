package service

// cc_upstream_idle.go 二开：raw CC 上游字节流 idle 超时。
//
// workbuddy / qoder / trae 三条 raw CC 路径（CC / Responses / Anthropic 入站回退）
// 共用 sendCCUpstreamRequest，其读循环只有 scanner 没有读超时：上游静默卡住时
// 请求会一直挂到客户端断开。移植 workbuddy2api-panel ab1ded7 的
// stream_with_idle_timeout 语义——按「相邻字节块」计 idle，超时关闭上游 body 并
// 让读循环拿到明确错误，避免把静默卡死伪装成正常完成。
//
// workbuddy 默认 60s（panel 实测口径，仅 CN；WB_STREAM_IDLE_TIMEOUT_SECS 可覆盖
// 两个区域，0 关闭）；其余平台/区域沿用 gateway.stream_data_interval_timeout
//（未配置时关闭）。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// errCCUpstreamStreamIdle 上游流 idle 超时（读循环据此与普通断流区分）。
var errCCUpstreamStreamIdle = errors.New("upstream stream idle timeout")

// workbuddyStreamIdleDefault workbuddy 上游字节 idle 缺省值（panel ab1ded7）。
const workbuddyStreamIdleDefault = 60 * time.Second

// ccUpstreamStreamIdle 返回该账号 raw CC 流的上游字节 idle 超时；0 表示禁用。
func (s *OpenAIGatewayService) ccUpstreamStreamIdle(account *Account) time.Duration {
	if account != nil && account.Platform == PlatformWorkbuddy {
		if raw := strings.TrimSpace(os.Getenv("WB_STREAM_IDLE_TIMEOUT_SECS")); raw != "" {
			if secs, err := strconv.Atoi(raw); err == nil {
				if secs <= 0 {
					return 0
				}
				return time.Duration(secs) * time.Second
			}
		}
		// panel 只为 CN 打开 60s 闸门；global 走通用配置，避免误杀长静默流。
		if cred, err := parseWorkbuddyCredentials(account); err != nil || !cred.IsGlobal() {
			return workbuddyStreamIdleDefault
		}
	}
	if s != nil && s.cfg != nil && s.cfg.Gateway.StreamDataIntervalTimeout > 0 {
		return time.Duration(s.cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	return 0
}

// ccUpstreamIdleBody 在底层 read closer 上按「上次成功读取」计 idle。
type ccUpstreamIdleBody struct {
	body    io.ReadCloser
	timeout time.Duration

	lastRead atomic.Int64 // UnixNano
	bytes    atomic.Int64
	timedOut atomic.Bool

	done      chan struct{}
	closeOnce sync.Once
}

// newCCUpstreamIdleBody 包装 body；timeout <= 0 时原样返回。
func newCCUpstreamIdleBody(body io.ReadCloser, timeout time.Duration) io.ReadCloser {
	if body == nil || timeout <= 0 {
		return body
	}
	b := &ccUpstreamIdleBody{body: body, timeout: timeout, done: make(chan struct{})}
	b.lastRead.Store(time.Now().UnixNano())
	go b.watch()
	return b
}

func (b *ccUpstreamIdleBody) watch() {
	// 轮询间隔取超时的 1/4，夹在 [50ms, 1s]：既能在超时后尽快关闭，又不会空转。
	interval := b.timeout / 4
	if interval > time.Second {
		interval = time.Second
	}
	if interval < 50*time.Millisecond {
		interval = 50 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, b.lastRead.Load())) >= b.timeout {
				b.timedOut.Store(true)
				_ = b.body.Close()
				return
			}
		}
	}
}

func (b *ccUpstreamIdleBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.lastRead.Store(time.Now().UnixNano())
		b.bytes.Add(int64(n))
	}
	if err != nil && b.timedOut.Load() {
		return n, fmt.Errorf("%w after %s idle (read %d bytes): %v",
			errCCUpstreamStreamIdle, b.timeout, b.bytes.Load(), err)
	}
	return n, err
}

func (b *ccUpstreamIdleBody) Close() error {
	b.closeOnce.Do(func() { close(b.done) })
	return b.body.Close()
}
