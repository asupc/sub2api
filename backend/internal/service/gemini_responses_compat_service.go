package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
)

// ForwardAsResponses serves OpenAI Responses API clients (Codex, Codex TUI) on
// the Gemini platform. The Gemini compat service speaks Anthropic Messages
// internally, so the request is translated Responses -> Anthropic here, fed
// through Forward, and the Claude SSE/JSON output is translated back to
// Responses while it streams.
func (s *GeminiMessagesCompatService) ForwardAsResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	_ *ParsedRequest,
) (*ForwardResult, error) {
	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(body, &responsesReq); err != nil {
		return nil, s.writeGeminiResponsesError(c, http.StatusBadRequest, "invalid_request_error", "Failed to parse request body")
	}
	if strings.TrimSpace(responsesReq.Model) == "" {
		return nil, s.writeGeminiResponsesError(c, http.StatusBadRequest, "invalid_request_error", "model is required")
	}

	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(&responsesReq)
	if err != nil {
		return nil, s.writeGeminiResponsesError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
	}
	anthropicReq.Stream = responsesReq.Stream
	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	translator := newClaudeToResponsesWriter(c.Writer, responsesReq.Model, responsesReq.Stream)
	originalWriter := c.Writer
	c.Writer = translator
	result, forwardErr := s.Forward(ctx, c, account, anthropicBody)
	c.Writer = originalWriter
	translator.finish()

	if result == nil {
		return nil, forwardErr
	}
	if result.Model == "" {
		result.Model = responsesReq.Model
	}
	result.Stream = responsesReq.Stream
	return result, forwardErr
}

func (s *GeminiMessagesCompatService) writeGeminiResponsesError(c *gin.Context, status int, code, message string) error {
	writeResponsesError(c, status, code, message)
	return fmt.Errorf("%s", message)
}

type claudeResponsesWriterMode int

const (
	claudeResponsesModeUnknown claudeResponsesWriterMode = iota
	claudeResponsesModeSSE
	claudeResponsesModeJSON
	claudeResponsesModePassthrough
)

// claudeToResponsesWriter sits between the Gemini compat service (which emits
// Anthropic Messages JSON/SSE) and the Responses client. It reuses Forward's
// retries, error mapping, usage accounting and thinking signatures, and only
// rewrites the wire format.
type claudeToResponsesWriter struct {
	gin.ResponseWriter

	mode   claudeResponsesWriterMode
	status int

	state    *apicompat.AnthropicEventToResponsesState
	usage    ClaudeUsage
	sseBuf   bytes.Buffer
	jsonBuf  bytes.Buffer
	finished bool
}

func newClaudeToResponsesWriter(w gin.ResponseWriter, model string, _ bool) *claudeToResponsesWriter {
	state := apicompat.NewAnthropicEventToResponsesState()
	state.Model = model
	return &claudeToResponsesWriter{
		ResponseWriter: w,
		status:         http.StatusOK,
		state:          state,
	}
}

func (w *claudeToResponsesWriter) WriteHeader(status int) {
	if status > 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *claudeToResponsesWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

func (w *claudeToResponsesWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if w.mode == claudeResponsesModeUnknown {
		w.mode = w.detectMode()
	}
	switch w.mode {
	case claudeResponsesModeSSE:
		w.sseBuf.Write(p)
		return len(p), w.flushSSE()
	case claudeResponsesModeJSON:
		w.jsonBuf.Write(p)
		return len(p), nil
	default:
		return w.ResponseWriter.Write(p)
	}
}

// WriteString must route through Write: fmt.Fprintf prefers io.StringWriter,
// which would otherwise skip the Responses translation entirely.
func (w *claudeToResponsesWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *claudeToResponsesWriter) detectMode() claudeResponsesWriterMode {
	contentType := strings.ToLower(w.ResponseWriter.Header().Get("Content-Type"))
	switch {
	case strings.Contains(contentType, "text/event-stream"):
		return claudeResponsesModeSSE
	case strings.Contains(contentType, "application/json"):
		return claudeResponsesModeJSON
	default:
		return claudeResponsesModePassthrough
	}
}

// flushSSE consumes complete SSE events ("event: ...\ndata: ...\n\n") from the
// buffer and writes the translated Responses events downstream.
func (w *claudeToResponsesWriter) flushSSE() error {
	for {
		raw := w.sseBuf.Bytes()
		sep := bytes.Index(raw, []byte("\n\n"))
		if sep < 0 {
			return nil
		}
		block := append([]byte(nil), raw[:sep]...)
		w.sseBuf.Next(sep + 2)
		if err := w.translateSSEBlock(block); err != nil {
			return err
		}
	}
}

func (w *claudeToResponsesWriter) translateSSEBlock(block []byte) error {
	payload, eventType := parseClaudeSSEBlock(block)
	if payload == "" {
		return nil
	}
	var event apicompat.AnthropicStreamEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return nil
	}
	if event.Type == "" {
		event.Type = eventType
	}
	if event.Type == "message_start" && event.Message != nil {
		mergeAnthropicUsage(&w.usage, event.Message.Usage)
	}
	if event.Type == "message_delta" && event.Usage != nil {
		mergeAnthropicUsage(&w.usage, *event.Usage)
	}
	syncAnthropicResponsesUsage(w.state, w.usage)
	normalizeAnthropicEventUsageForResponses(&event, w.usage)

	for _, evt := range apicompat.AnthropicEventToResponsesEvents(&event, w.state) {
		encoded, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		if _, err := fmt.Fprintf(w.ResponseWriter, "event: %s\ndata: %s\n\n", evt.Type, encoded); err != nil {
			return err
		}
	}
	w.ResponseWriter.Flush()
	return nil
}

func parseClaudeSSEBlock(block []byte) (data, eventType string) {
	for _, line := range strings.Split(string(block), "\n") {
		line = strings.TrimRight(line, "\r")
		if payload, ok := parseAnthropicSSEField(line, "event"); ok {
			eventType = payload
			continue
		}
		if payload, ok := parseAnthropicSSEField(line, "data"); ok {
			data = payload
		}
	}
	return data, eventType
}

// finish flushes the terminal Responses events for streams and converts buffered
// JSON (success or error) emitted by Forward.
func (w *claudeToResponsesWriter) finish() {
	if w.finished {
		return
	}
	w.finished = true
	switch w.mode {
	case claudeResponsesModeSSE:
		for _, evt := range apicompat.FinalizeAnthropicResponsesStream(w.state) {
			sse, err := apicompat.ResponsesEventToSSE(evt)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprint(w.ResponseWriter, sse)
		}
		w.ResponseWriter.Flush()
	case claudeResponsesModeJSON:
		w.writeJSONResponse()
	}
}

func (w *claudeToResponsesWriter) writeJSONResponse() {
	body := bytes.TrimSpace(w.jsonBuf.Bytes())
	if len(body) == 0 {
		return
	}

	if w.status >= http.StatusBadRequest {
		var anthropicErr struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		code := "server_error"
		message := strings.TrimSpace(string(body))
		if err := json.Unmarshal(body, &anthropicErr); err == nil && strings.TrimSpace(anthropicErr.Error.Message) != "" {
			message = anthropicErr.Error.Message
			if anthropicErr.Error.Type != "" {
				code = anthropicErr.Error.Type
			}
		}
		payload, _ := json.Marshal(gin.H{"error": gin.H{"code": code, "message": message}})
		w.ResponseWriter.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.ResponseWriter.Write(payload)
		return
	}

	var anthropicResp apicompat.AnthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		_, _ = w.ResponseWriter.Write(body)
		return
	}
	converted := apicompat.AnthropicToResponsesResponse(&anthropicResp)
	encoded, err := json.Marshal(converted)
	if err != nil {
		_, _ = w.ResponseWriter.Write(body)
		return
	}
	_, _ = w.ResponseWriter.Write(encoded)
}
