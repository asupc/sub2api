// speech.go 官方语音能力（移植自 workbuddy2api-panel internal/upstream/speech.go，
// issue #277；协议取自官方桌面端包内 server.js 的静态反查 + 2026-11-20 真号实测
// TTS→ASR 闭环）：
//
//	POST {chatBase}/v2/audio/speech    文本转语音（SSE，PCM base64 分片）
//	POST {chatBase}/agenttool/v1/asr   语音转文本（multipart 字段 audio）
//
// 与 chat 同域（chatBase，realm 决定 CN/global）同头（ChatHeaders）。
// 官方还有 WebSocket 流式 ASR（/clientcap/v2/asr/stream），本网关不做。
package workbuddy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

const (
	// SpeechPath 文本转语音（SSE）。
	SpeechPath = "/v2/audio/speech"
	// ASRPath 语音转文本（multipart，非流式）。
	ASRPath = "/agenttool/v1/asr"

	// TTSModel 官方客户端写死的模型（不在 /v1/models 目录里，是独立端点）。
	TTSModel = "hunyuan-tts-2.5"
	// TTSDefaultVoice 官方默认音色；客户端可覆盖。
	TTSDefaultVoice = "f191_female_cn_tts2.5_flash"
	// TTSSampleRate 官方固定 24kHz；回程 PCM 是 s16le 单声道，转 WAV 时必须用它。
	TTSSampleRate = 24000
)

// Speech 合成语音，返回 PCM（s16le 单声道，采样率 TTSSampleRate）。
// 封装格式属于对外契约，由调用方按 response_format 决定。
func (c *Client) Speech(ctx context.Context, cred Credentials, text, voice string) ([]byte, error) {
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("speech input is empty")
	}
	if voice == "" {
		voice = TTSDefaultVoice
	}
	body, err := json.Marshal(map[string]any{
		"model":  TTSModel,
		"stream": true,
		"speech": map[string]any{
			"input":           text,
			"voice":           voice,
			"response_format": "pcm",
			"sample_rate":     TTSSampleRate,
		},
	})
	if err != nil {
		return nil, err
	}
	endpoint := c.chatBase(cred) + SpeechPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	ChatHeaders(req, cred, ChatMeta{})
	// ChatHeaders 会把 Accept 覆盖成 application/json（chat 的官方口径）。TTS 实测
	// 上游不看 Accept、照样回 SSE，但声明与实收对齐才能让中间层不加缓冲。
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.httpc().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, speechUpstreamError(resp.StatusCode, raw)
	}
	// SSE 分片可能比默认 64KiB 行长得多（一帧 base64 PCM 就有几十 KB）。
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var pcm []byte
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var ev struct {
			Type  string `json:"type"`
			Audio struct {
				Data string `json:"data"`
			} `json:"audio"`
			Error *struct {
				Message string `json:"message"`
				Code    any    `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil, fmt.Errorf("tts stream returned invalid json: %w", err)
		}
		// 上游可以在 SSE 里回错误帧（`{"type":"error","error":{...}}`）；不认它就会
		// 一路掉到 "completed without audio"，把上游的原因咽掉。
		if ev.Error != nil && (ev.Error.Message != "" || ev.Error.Code != nil) {
			return nil, &Error{
				Kind:   classifyString(http.StatusBadGateway, payload),
				Status: http.StatusBadGateway,
				Msg:    fmt.Sprintf("tts stream error: %s", truncate(ev.Error.Message, 160)),
			}
		}
		switch ev.Type {
		case "speech.audio.delta":
			chunk, derr := base64.StdEncoding.DecodeString(ev.Audio.Data)
			if derr != nil {
				return nil, fmt.Errorf("tts audio delta is not base64: %w", derr)
			}
			pcm = append(pcm, chunk...)
		case "speech.audio.done":
			return pcm, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read tts stream: %w", err)
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("tts stream completed without audio")
	}
	// speech.audio.done 是唯一的「音频完整」标记。上游 SSE 被中间层提前切断时
	// EOF 前可能已经产出半段 PCM（2026-11-20 复测 132480 vs 136320 字节，
	// ASR 识别成乱码），当成功返回会让客户端拿到 200 + 残缺录音
	// （workbuddy2api-panel d5ce7de / issue #277）。交给上层换号重试。
	return nil, &Error{
		Kind:   KindServer,
		Status: http.StatusBadGateway,
		Msg:    fmt.Sprintf("tts stream truncated after %d bytes: no speech.audio.done", len(pcm)),
	}
}

// Transcribe 语音转文本，返回上游识别文本。
// 上游要 multipart 字段 `audio`（带文件名与 mime），回 {code,text}；code 非 0 即失败。
func (c *Client) Transcribe(ctx context.Context, cred Credentials, filename, contentType string, audio []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if filename == "" {
		filename = "audio.wav"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	part, err := mw.CreatePart(map[string][]string{
		"Content-Disposition": {`form-data; name="audio"; filename="` + filename + `"`},
		"Content-Type":        {contentType},
	})
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	endpoint := c.chatBase(cred) + ASRPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
	if err != nil {
		return "", err
	}
	ChatHeaders(req, cred, ChatMeta{})
	// commonHeaders 给所有请求写死 Content-Type: application/json，multipart 的
	// boundary 被它抹掉后上游只能报 15052「failed to parse multipart form」——
	// 必须在 ChatHeaders 之后设回来（panel e1280ca）。
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.httpc().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", speechUpstreamError(resp.StatusCode, raw)
	}
	var env struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("asr response is not json: %w", err)
	}
	if env.Code != 0 {
		// 把业务码按 codeMarker 认的形态一起交给分类：有些码（15053）只能靠码本身判，
		// 该码是请求级（音频内容问题），不罚号不轮转（panel 08cb935）。
		mark := fmt.Sprintf(`"code":%d %s`, env.Code, env.Msg)
		kind := classifyString(resp.StatusCode, mark)
		if kind == KindNone {
			kind = KindBadParams
		}
		return "", &Error{Kind: kind, Status: resp.StatusCode,
			Msg: fmt.Sprintf("asr code=%d msg=%s", env.Code, truncate(env.Msg, 160))}
	}
	return env.Text, nil
}

// speechUpstreamError 把 ≥400 的响应体翻成带分类的 *Error。
func speechUpstreamError(status int, raw []byte) error {
	kind := classifyString(status, string(raw))
	if kind == KindNone {
		kind = KindClient
	}
	return &Error{Kind: kind, Status: status, Msg: truncate(string(raw), 200)}
}
