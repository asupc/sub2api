package service

// qoder 会话缓存保温的 service 侧接线（qoder2api-hub 22bd512，QD_CACHE_KEEPALIVE，
// 默认关闭）：observe 挂在 qoder 出站成功点，sender 走与真实转发相同的
// BuildAgentChatBody + COSY 签名 + 网关主机故障切换链，用同 payload、同
// session_id 重发 stream=false + max_tokens=1 的最小请求为上游前缀缓存续期。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/qoder"
	"go.uber.org/zap"
)

// qoderKeepaliveLoopOnce 懒启动后台巡检（第一次 observe 时启动一次）。
var (
	qoderKeepaliveLoopOnce sync.Once
	qoderKeepaliveLoopStop func()
)

// startQoderKeepaliveLoop 幂等启动保温巡检（20s tick 在 pkg 内）。
func (s *OpenAIGatewayService) startQoderKeepaliveLoop() {
	qoderKeepaliveLoopOnce.Do(func() {
		qoder.ConfigureKeepalive(s.qoderKeepaliveSend)
		qoderKeepaliveLoopStop = qoder.StartKeepaliveLoop()
		logger.L().Info("qoder cache keepalive on",
			zap.Int64("interval_s", int64(qoder.KeepaliveInterval().Seconds())),
			zap.Int64("max_age_s", int64(qoder.KeepaliveMaxAge().Seconds())),
			zap.Int("max_convs", qoder.KeepaliveMaxConvs()),
		)
	})
}

// observeQoderKeepalive 在 qoder 出站成功后登记会话（默认关闭时零开销）。
// encodedBody 为 qoder_encode 后的出站体（decode 提取最终 session_id），
// origBody 为改写前的原始 OpenAI 请求体（保温 payload 快照）。
func (s *OpenAIGatewayService) observeQoderKeepalive(account *Account, encodedBody, origBody []byte) {
	if !qoder.KeepaliveEnabled() || account == nil {
		return
	}
	s.startQoderKeepaliveLoop()
	decoded, err := qoder.QoderDecode(string(encodedBody))
	if err != nil {
		return
	}
	var bodyObj map[string]any
	if json.Unmarshal(decoded, &bodyObj) != nil {
		return
	}
	sessionID, _ := bodyObj["session_id"].(string)
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	var payload map[string]any
	if json.Unmarshal(origBody, &payload) != nil {
		return
	}
	qoder.KeepaliveObserve(payload, sessionID, account.ID, qoderAccountRealm(account), "")
}

// qoderKeepaliveSend 保温一次：同 payload、同账号、同 session_id，
// stream=false + max_tokens=1（hub _keepalive_send 同款）。clientSessionKey 传
// 登记时的最终 session_id——BuildAgentChatBody 的窗口键显式键优先原样回填，
// 与原请求同一会话身份；前缀逐字节相同 → 命中上游缓存并续期，消耗 ≈ 一次
// 缓存读。失败返回 error 由 tick 淘汰该会话。
func (s *OpenAIGatewayService) qoderKeepaliveSend(entry qoder.KeepaliveEntry) error {
	if s == nil || s.accountRepo == nil {
		return fmt.Errorf("keepalive: gateway service unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	account, err := s.accountRepo.GetByID(ctx, entry.AccountID)
	if err != nil || account == nil || account.Platform != PlatformQoder {
		return fmt.Errorf("keepalive: account %d unavailable: %v", entry.AccountID, err)
	}
	if _, err := qoderAccountCredentials(account); err != nil {
		return fmt.Errorf("keepalive: account %d credentials: %w", account.ID, err)
	}
	targetURL, err := qoderChatURL(account)
	if err != nil {
		return fmt.Errorf("keepalive: chat url: %w", err)
	}
	payload, err := qoder.DeepCopyJSONMap(entry.Payload)
	if err != nil {
		return fmt.Errorf("keepalive: payload snapshot: %w", err)
	}
	payload["stream"] = false
	payload["max_tokens"] = 1
	origBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("keepalive: payload marshal: %w", err)
	}
	encoded, err := applyQoderChatBody(account, origBody, entry.SessionID)
	if err != nil {
		return fmt.Errorf("keepalive: build body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader([]byte(encoded)))
	if err != nil {
		return fmt.Errorf("keepalive: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := applyQoderChatTransport(req, account, []byte(encoded), origBody); err != nil {
		return fmt.Errorf("keepalive: cosy transport: %w", err)
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	resp, err := s.sendQoderChat(ctx, account, req, []byte(encoded), proxyURL)
	if err != nil {
		return fmt.Errorf("keepalive: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("keepalive: upstream http %d: %s", resp.StatusCode, string(body))
	}
	// 私有 SSE 信封已在分叉内包装；聚合只为确认请求走通（usage 不入账，
	// 消耗 ≈ 一次缓存读，由账号侧额度自然反映）。
	_, aggErr := qoder.AggregateStream(resp.Body, entry.Model, &qoder.UsageHolder{}, nil)
	if aggErr != nil {
		return fmt.Errorf("keepalive: aggregate: %w", aggErr)
	}
	logger.L().Debug("qoder cache keepalive ping sent",
		zap.Int64("account_id", account.ID),
		zap.String("session_id", shortKeepaliveID(entry.SessionID)),
		zap.Int("pings", entry.Pings+1),
	)
	return nil
}

// shortKeepaliveID 会话 ID 日志脱敏（前 16 字符）。
func shortKeepaliveID(sid string) string {
	if len(sid) > 16 {
		return sid[:16]
	}
	return sid
}
