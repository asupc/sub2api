package handler

// gateway_workbuddy_speech.go 二开：WorkBuddy 官方语音端点（issue #277）。
//
//	POST /v1/audio/speech          OpenAI TTS 契约：{model?, input, voice, response_format?}
//	POST /v1/audio/transcriptions  OpenAI ASR 契约：multipart 字段 file
//
// 上游协议见 pkg/workbuddy/speech.go。TTS 只出 WAV（PCM s16le/24kHz 单声道封装），
// 其余 response_format 明确 400——不谎报转码能力。ASR 入口限制 3MB（panel 08cb935），
// 整包按 3MB+64KiB 钳制并清理 multipart 临时文件；response_format 支持 json/text
//（panel 026f9c0/d5ce7de）。

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/workbuddy"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	// workbuddySpeechModelLabel 记账/日志标识（语音不是模型对话，但需要可区分的模型列）。
	workbuddySpeechModelLabel = "workbuddy-tts"
	workbuddyASRModelLabel    = "workbuddy-asr"
	// workbuddySpeechBodyLimit TTS 请求体上限（文本输入，1MB 富余）。
	workbuddySpeechBodyLimit = 1 << 20
	// workbuddyASRBodyLimit ASR 入口 3MB（panel 08cb935：超限在上游之前挡掉）。
	workbuddyASRBodyLimit = 3 << 20
	// workbuddyASRBodyOverhead multipart 头/边界/其它小字段的余量：整包上限按
	// 3MB + 64KiB 判（panel 026f9c0），否则恰好 3MB 的音频会被边界开销挤成超限。
	workbuddyASRBodyOverhead = 64 << 10
)

// workbuddySpeechRequest TTS 请求（OpenAI 契约的子集）。
type workbuddySpeechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	Voice          string `json:"voice"`
	ResponseFormat string `json:"response_format"`
}

// WorkbuddySpeech 处理 POST /v1/audio/speech。
func (h *GatewayHandler) WorkbuddySpeech(c *gin.Context) {
	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "API key required")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformWorkbuddy {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "speech is only supported for workbuddy groups")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, workbuddySpeechBodyLimit))
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "failed to read request body")
		return
	}
	var req workbuddySpeechRequest
	if err := json.Unmarshal(body, &req); err != nil || req.Input == "" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "request body must be a JSON object with a non-empty input")
		return
	}
	format := req.ResponseFormat
	if format == "" {
		format = "wav"
	}
	if format != "wav" {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "only response_format=wav is supported")
		return
	}

	if !h.prepareWorkbuddyAudioBilling(c, apiKey, workbuddySpeechModelLabel, req.Input, true, requestStart) {
		return
	}

	reqLog := requestLogger(c, "handler.gateway.workbuddy_speech",
		zap.Int64("user_id", apiKey.User.ID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.String("model", workbuddySpeechModelLabel),
	)
	pcm, fail := h.openAIGatewayService.RunWorkbuddySpeech(c.Request.Context(), c, apiKey.GroupID, req.Input, req.Voice)
	if fail != nil {
		releaseWorkbuddyAudioInflight(c)
		reqLog.Warn("gateway.workbuddy_speech.failed", zap.Int("status", fail.Status), zap.String("message", fail.Msg))
		h.errorResponse(c, fail.Status, "api_error", fail.Msg)
		return
	}
	h.recordWorkbuddyAudioUsage(c, apiKey, workbuddySpeechModelLabel, "/v2/audio/speech", requestStart)

	c.Header("Content-Type", "audio/wav")
	c.Header("Content-Length", strconv.Itoa(len(pcm)+44))
	c.Status(http.StatusOK)
	_, _ = c.Writer.Write(wrapPCMAsWAV(pcm, workbuddy.TTSSampleRate, 1, 16))
}

// WorkbuddyTranscription 处理 POST /v1/audio/transcriptions（multipart）。
func (h *GatewayHandler) WorkbuddyTranscription(c *gin.Context) {
	requestStart := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "API key required")
		return
	}
	if apiKey.Group == nil || apiKey.Group.Platform != service.PlatformWorkbuddy {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "transcriptions are only supported for workbuddy groups")
		return
	}
	// 入口挡 3MB：先钳住 body 再解析，超过的部分不会落到 /tmp（panel 026f9c0；
	// multipart 边界/其它小字段留 64KiB 余量，文件本身仍按 3MB 判）。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, workbuddyASRBodyLimit+workbuddyASRBodyOverhead)
	fileHeader, err := c.FormFile("file")
	if c.Request.MultipartForm != nil {
		// ParseMultipartForm 超过内存阈值的分片会写临时文件：无论成功失败都清掉，
		// 避免每次超限上传在 /tmp 留一个 multipart-* 文件。
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "audio file exceeds the 3MB limit")
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "multipart field 'file' is required")
		return
	}
	if fileHeader.Size > workbuddyASRBodyLimit {
		h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "audio file exceeds the 3MB limit")
		return
	}
	f, err := fileHeader.Open()
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "failed to read uploaded file")
		return
	}
	defer f.Close()
	audio, err := io.ReadAll(io.LimitReader(f, workbuddyASRBodyLimit+1))
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "failed to read uploaded file")
		return
	}
	if len(audio) > workbuddyASRBodyLimit {
		h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", "audio file exceeds the 3MB limit")
		return
	}

	// OpenAI 的 response_format：json（默认）/ text 可选；srt/vtt/verbose_json 要
	// 时间戳，上游不返回，明确拒绝而不是静默回 JSON（panel d5ce7de / issue #277）。
	responseFormat, formatOK := normalizeWorkbuddyASRResponseFormat(c.Request.FormValue("response_format"))
	if !formatOK {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error",
			"response_format "+responseFormat+" is not supported: upstream does not return timestamps; use json or text")
		return
	}

	if !h.prepareWorkbuddyAudioBilling(c, apiKey, workbuddyASRModelLabel, "", false, requestStart) {
		return
	}

	reqLog := requestLogger(c, "handler.gateway.workbuddy_transcription",
		zap.Int64("user_id", apiKey.User.ID),
		zap.Int64("api_key_id", apiKey.ID),
		zap.String("model", workbuddyASRModelLabel),
	)
	contentType := fileHeader.Header.Get("Content-Type")
	text, fail := h.openAIGatewayService.RunWorkbuddyTranscribe(c.Request.Context(), c, apiKey.GroupID, fileHeader.Filename, contentType, audio)
	if fail != nil {
		releaseWorkbuddyAudioInflight(c)
		reqLog.Warn("gateway.workbuddy_transcription.failed", zap.Int("status", fail.Status), zap.String("message", fail.Msg))
		h.errorResponse(c, fail.Status, "api_error", fail.Msg)
		return
	}
	h.recordWorkbuddyAudioUsage(c, apiKey, workbuddyASRModelLabel, "/agenttool/v1/asr", requestStart)
	if responseFormat == "text" {
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(text))
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}

// normalizeWorkbuddyASRResponseFormat 归一合并校验 ASR response_format。
// ok=false 表示上游不支持该格式（srt/vtt/verbose_json 要时间戳）。
func normalizeWorkbuddyASRResponseFormat(raw string) (format string, ok bool) {
	switch f := strings.ToLower(strings.TrimSpace(raw)); f {
	case "", "json", "text":
		return f, true
	default:
		return f, false
	}
}

// prepareWorkbuddyAudioBilling 计费资格 + 在途预留 + （文本类）安全审计。
// 返回 false 表示已写回错误响应。
func (h *GatewayHandler) prepareWorkbuddyAudioBilling(c *gin.Context, apiKey *service.APIKey, modelLabel, auditText string, audit bool, requestStart time.Time) bool {
	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	setOpsRequestContext(c, modelLabel, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))

	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		status, code, message, retryAfter := billingErrorDetails(err)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return false
	}
	inflightDone, inflightErr := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription, service.InflightEstimateRequest{
		Model: modelLabel,
		Kind:  service.InflightEstimatePerRequest,
	})
	if inflightErr != nil {
		status, code, message, retryAfter := billingErrorDetails(inflightErr)
		if retryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
		}
		h.errorResponse(c, status, code, message)
		return false
	}
	c.Set("workbuddy_audio_inflight_done", inflightDone)
	if audit && auditText != "" {
		auditBody, _ := json.Marshal(map[string]any{
			"messages": []map[string]any{{"role": "user", "content": auditText}},
		})
		if decision := h.checkSecurityAudit(c, requestLogger(c, "handler.gateway.workbuddy_speech_audit"), apiKey, subject, service.ContentModerationProtocolOpenAIChat, modelLabel, auditBody); decision != nil && !decision.AllowNextStage {
			status := decision.HTTPStatus
			if status == 0 {
				status = http.StatusForbidden
			}
			code := decision.ErrorCode
			if code == "" {
				code = "content_policy_violation"
			}
			msg := decision.ClientMessage
			if msg == "" {
				msg = "Request blocked by content policy"
			}
			h.errorResponse(c, status, code, msg)
			return false
		}
	}
	return true
}

// releaseWorkbuddyAudioInflight 释放在途预留（成功记账与失败路径都调用一次）。
func releaseWorkbuddyAudioInflight(c *gin.Context) {
	if v, ok := c.Get("workbuddy_audio_inflight_done"); ok {
		if done, _ := v.(func()); done != nil {
			done()
			c.Set("workbuddy_audio_inflight_done", nil)
		}
	}
}

// recordWorkbuddyAudioUsage 记账（按次请求口径，无 token 消耗）并释放在途预留。
func (h *GatewayHandler) recordWorkbuddyAudioUsage(c *gin.Context, apiKey *service.APIKey, modelLabel, upstreamEndpoint string, requestStart time.Time) {
	defer releaseWorkbuddyAudioInflight(c)
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	userAgent := c.GetHeader("User-Agent")
	clientIP := ip.GetClientIP(c)
	inboundEndpoint := GetInboundEndpoint(c)
	requestID := modelLabel + ":" + uuid.NewString()
	quotaPlatform := service.QuotaPlatform(c.Request.Context(), apiKey)
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, &service.RecordUsageInput{
			Result: &service.ForwardResult{
				RequestID: requestID,
				Model:     modelLabel,
				Duration:  time.Since(requestStart),
			},
			APIKey:             apiKey,
			User:               apiKey.User,
			Subscription:       subscription,
			InboundEndpoint:    inboundEndpoint,
			UpstreamEndpoint:   upstreamEndpoint,
			UserAgent:          userAgent,
			IPAddress:          clientIP,
			RequestPayloadHash: service.HashUsageRequestPayload([]byte(requestID)),
			APIKeyService:      h.apiKeyService,
			QuotaPlatform:      quotaPlatform,
		}); err != nil {
			logger.L().With(
				zap.String("component", "handler.gateway.workbuddy_speech"),
				zap.Int64("user_id", apiKey.User.ID),
				zap.Int64("api_key_id", apiKey.ID),
			).Error("gateway.workbuddy_speech.record_usage_failed", zap.Error(err))
		}
	})
}

// wrapPCMAsWAV 给裸 PCM 加 44 字节 RIFF/WAVE 头（s16le 单声道）。
func wrapPCMAsWAV(pcm []byte, sampleRate, channels, bitsPerSample int) []byte {
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	out := make([]byte, 44, 44+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm)))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(out[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(out[34:36], uint16(bitsPerSample))
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm)))
	return append(out, pcm...)
}
