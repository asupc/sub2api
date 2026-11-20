package service

// workbuddy_speech_service.go 二开：WorkBuddy 官方语音端点（TTS / ASR）的账号轮转
// 执行（移植自 workbuddy2api-panel internal/server/audio.go + upstream/speech.go，
// issue #277）。
//
// 与 RunWorkbuddySearch 同骨架；失败处置复用 applyWorkbuddyUpstreamErrorKind，
// 保证限流/风控/积分耗尽的账号状态与 chat 一致。

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	"github.com/gin-gonic/gin"
)

// WorkbuddySpeechError 客户端视角的出口错误。
type WorkbuddySpeechError struct {
	Status int
	Msg    string
}

const (
	workbuddySpeechMaxAttempts = 3
	// workbuddySpeechTimeout TTS 是流式 SSE（长文本可能几十秒），给足超时。
	workbuddySpeechTimeout = 120 * time.Second
	workbuddyASRTimeout    = 60 * time.Second
)

// RunWorkbuddySpeech 在一个 workbuddy 账号上合成语音，返回 PCM（s16le/24kHz）。
func (s *OpenAIGatewayService) RunWorkbuddySpeech(ctx context.Context, c *gin.Context, groupID *int64, text, voice string) ([]byte, *WorkbuddySpeechError) {
	var pcm []byte
	fail := s.runWorkbuddyAudioOnce(ctx, c, groupID, workbuddySpeechTimeout, func(ctx context.Context, client *workbuddy.Client, cred workbuddy.Credentials) error {
		out, err := client.Speech(ctx, cred, text, voice)
		if err == nil {
			pcm = out
		}
		return err
	})
	return pcm, fail
}

// RunWorkbuddyTranscribe 在一个 workbuddy 账号上做语音转文本。
func (s *OpenAIGatewayService) RunWorkbuddyTranscribe(ctx context.Context, c *gin.Context, groupID *int64, filename, contentType string, audio []byte) (string, *WorkbuddySpeechError) {
	var text string
	fail := s.runWorkbuddyAudioOnce(ctx, c, groupID, workbuddyASRTimeout, func(ctx context.Context, client *workbuddy.Client, cred workbuddy.Credentials) error {
		out, err := client.Transcribe(ctx, cred, filename, contentType, audio)
		if err == nil {
			text = out
		}
		return err
	})
	return text, fail
}

// runWorkbuddyAudioOnce 账号轮转骨架：同一请求内同一账号只试一次。
func (s *OpenAIGatewayService) runWorkbuddyAudioOnce(
	ctx context.Context,
	c *gin.Context,
	groupID *int64,
	timeout time.Duration,
	fn func(context.Context, *workbuddy.Client, workbuddy.Credentials) error,
) *WorkbuddySpeechError {
	failed := make(map[int64]struct{}, workbuddySpeechMaxAttempts)
	var lastFail *WorkbuddySpeechError

	for attempt := 0; attempt < workbuddySpeechMaxAttempts; attempt++ {
		selection, _, err := s.SelectAccountWithSchedulerForCapability(
			ctx, groupID, "", "", "", failed,
			OpenAIUpstreamTransportAny, "", false, false, false,
			PlatformWorkbuddy,
		)
		if err != nil || selection == nil || selection.Account == nil {
			break
		}
		account := selection.Account
		failed[account.ID] = struct{}{}

		release := selection.ReleaseFunc
		if !selection.Acquired || release == nil {
			acq, aerr := s.tryAcquireAccountSlot(ctx, account.ID, account.Concurrency)
			if aerr != nil || acq == nil || !acq.Acquired {
				continue
			}
			release = acq.ReleaseFunc
		}

		cred, cerr := parseWorkbuddyCredentials(account)
		if cerr != nil {
			if release != nil {
				release()
			}
			slog.Warn("workbuddy speech credentials invalid", "account_id", account.ID, "error", cerr)
			lastFail = &WorkbuddySpeechError{Status: http.StatusServiceUnavailable, Msg: "workbuddy account credentials are invalid"}
			continue
		}
		client := newWorkbuddyUpstreamClient(upstreamModelsProxyURL(account), timeout)
		serr := fn(ctx, client, cred)
		if release != nil {
			release()
		}
		if serr == nil {
			return nil
		}

		var ue *workbuddy.Error
		if !errors.As(serr, &ue) {
			slog.Warn("workbuddy speech transport failure", "account_id", account.ID, "error", serr)
			lastFail = &WorkbuddySpeechError{Status: http.StatusServiceUnavailable, Msg: "workbuddy speech upstream temporarily unreachable"}
			continue
		}
		resp := &http.Response{StatusCode: ue.Status, Header: http.Header{}}
		handled, foErr := s.applyWorkbuddyUpstreamErrorKind(ctx, c, account, ue.Kind, resp, []byte(ue.RawBody), ue.Msg, "")
		if handled && foErr == nil {
			// 确定性请求级错误（11151/11152/11102 等）：不换号重放。
			lastFail = &WorkbuddySpeechError{Status: clientStatusForSpeech(ue.Status), Msg: ue.Msg}
			break
		}
		if !handled && ue.Status >= 400 && ue.Status < 500 {
			lastFail = &WorkbuddySpeechError{Status: clientStatusForSpeech(ue.Status), Msg: ue.Msg}
			break
		}
		lastFail = &WorkbuddySpeechError{Status: http.StatusServiceUnavailable, Msg: "workbuddy speech upstream error, please retry later"}
	}

	if lastFail != nil {
		return lastFail
	}
	return &WorkbuddySpeechError{Status: http.StatusServiceUnavailable, Msg: "all workbuddy accounts are temporarily unavailable, please retry later"}
}

// clientStatusForSpeech 把上游状态收敛成客户端可识别的出口状态。
func clientStatusForSpeech(status int) int {
	switch {
	case status == http.StatusTooManyRequests:
		return http.StatusTooManyRequests
	case status >= 400 && status < 500:
		return http.StatusBadRequest
	default:
		return http.StatusBadGateway
	}
}
