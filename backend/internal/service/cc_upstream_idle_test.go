//go:build unit

package service

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// TestCCUpstreamIdleBodyTimesOut 上游静默不动时，读必须被 idle watchdog 打断并
// 返回可识别的错误（workbuddy2api-panel ab1ded7）。
func TestCCUpstreamIdleBodyTimesOut(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	body := newCCUpstreamIdleBody(pr, 80*time.Millisecond)

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, 16)
		_, err := body.Read(buf)
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, errCCUpstreamStreamIdle) {
			t.Fatalf("err=%v want idle timeout", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("idle read did not unblock")
	}
	_ = body.Close()
}

// TestCCUpstreamIdleBodyKeepsActiveStream 持续有数据时不触发超时。
func TestCCUpstreamIdleBodyKeepsActiveStream(t *testing.T) {
	pr, pw := io.Pipe()
	body := newCCUpstreamIdleBody(pr, 250*time.Millisecond)

	go func() {
		for i := 0; i < 5; i++ {
			_, _ = pw.Write([]byte("chunk"))
			time.Sleep(40 * time.Millisecond)
		}
		_ = pw.Close()
	}()

	var got int
	buf := make([]byte, 64)
	deadline := time.After(2 * time.Second)
	for {
		n, err := body.Read(buf)
		got += n
		if err != nil {
			if errors.Is(err, errCCUpstreamStreamIdle) {
				t.Fatalf("active stream hit idle timeout after %d bytes", got)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("stream did not finish")
		default:
		}
	}
	if got != 25 {
		t.Fatalf("read %d bytes want 25", got)
	}
}

// TestCCUpstreamStreamIdlePolicy workbuddy 默认 60s、环境变量可覆盖；其余平台
// 沿用 gateway.stream_data_interval_timeout。
func TestCCUpstreamStreamIdlePolicy(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{StreamDataIntervalTimeout: 180}}}
	wb := &Account{Platform: PlatformWorkbuddy}
	if got := svc.ccUpstreamStreamIdle(wb); got != 60*time.Second {
		t.Fatalf("workbuddy CN default=%s want 60s", got)
	}
	// panel 只为 CN 加 60s 闸门；global 回落通用配置。
	global := &Account{Platform: PlatformWorkbuddy, Credentials: map[string]any{"access_token": "at", "realm": "global"}}
	if got := svc.ccUpstreamStreamIdle(global); got != 180*time.Second {
		t.Fatalf("workbuddy global default=%s want configured 180s", got)
	}
	t.Setenv("WB_STREAM_IDLE_TIMEOUT_SECS", "25")
	if got := svc.ccUpstreamStreamIdle(wb); got != 25*time.Second {
		t.Fatalf("workbuddy override=%s want 25s", got)
	}
	t.Setenv("WB_STREAM_IDLE_TIMEOUT_SECS", "0")
	if got := svc.ccUpstreamStreamIdle(wb); got != 0 {
		t.Fatalf("workbuddy disabled=%s want 0", got)
	}
	qd := &Account{Platform: PlatformQoder}
	if got := svc.ccUpstreamStreamIdle(qd); got != 180*time.Second {
		t.Fatalf("qoder configured=%s want 180s", got)
	}
	if got := (&OpenAIGatewayService{}).ccUpstreamStreamIdle(qd); got != 0 {
		t.Fatalf("nil cfg must disable, got %s", got)
	}
}
