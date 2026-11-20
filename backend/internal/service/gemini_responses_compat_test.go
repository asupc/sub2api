package service

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newTestClaudeWriter(t *testing.T) (*gin.Context, *httptest.ResponseRecorder, *claudeToResponsesWriter) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	writer := newClaudeToResponsesWriter(ctx.Writer, "gemini-4-hydrogen", true)
	return ctx, recorder, writer
}

func TestClaudeToResponsesWriterTranslatesStream(t *testing.T) {
	_, recorder, writer := newTestClaudeWriter(t)

	events := []string{
		`event: message_start` + "\n" + `data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"gemini-4-hydrogen","content":[],"usage":{"input_tokens":11,"output_tokens":0}}}` + "\n\n",
		`event: content_block_start` + "\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n",
		`event: content_block_delta` + "\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"OK"}}` + "\n\n",
		`event: content_block_stop` + "\n" + `data: {"type":"content_block_stop","index":0}` + "\n\n",
		`event: message_delta` + "\n" + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n",
		`event: message_stop` + "\n" + `data: {"type":"message_stop"}` + "\n\n",
	}
	for _, event := range events {
		if _, err := writer.Write([]byte(event)); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	writer.finish()

	body := recorder.Body.String()
	for _, expected := range []string{"event: response.created", "response.output_text.delta", "response.completed"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %q in translated stream:\n%s", expected, body)
		}
	}
	if strings.Contains(body, "message_start") {
		t.Fatalf("anthropic event leaked into responses stream:\n%s", body)
	}
}

func TestClaudeToResponsesWriterTranslatesNonStreamJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Writer.Header().Set("Content-Type", "application/json")
	writer := newClaudeToResponsesWriter(ctx.Writer, "gemini-4-hydrogen", false)

	payload := `{"id":"msg_1","type":"message","role":"assistant","model":"gemini-4-hydrogen","content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
	writer.finish()

	body := recorder.Body.String()
	if !strings.Contains(body, `"object":"response"`) {
		t.Fatalf("expected responses object, got: %s", body)
	}
	if !strings.Contains(body, `"output"`) {
		t.Fatalf("expected output array, got: %s", body)
	}
}
