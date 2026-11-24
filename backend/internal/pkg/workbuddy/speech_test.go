//go:build unit

package workbuddy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseSpeechServer 构造只回指定 SSE 帧的 TTS 上游。
func sseSpeechServer(t *testing.T, frames []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != SpeechPath {
			t.Errorf("tts path=%s want %s", r.URL.Path, SpeechPath)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range frames {
			_, _ = w.Write([]byte(frame))
		}
	}))
}

func deltaFrame(t *testing.T, pcm []byte) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":  "speech.audio.delta",
		"audio": map[string]string{"data": base64.StdEncoding.EncodeToString(pcm)},
	})
	if err != nil {
		t.Fatal(err)
	}
	return "data: " + string(payload) + "\n\n"
}

// TestSpeechReturnsAudioOnDone 正常流：delta + speech.audio.done → 完整 PCM。
func TestSpeechReturnsAudioOnDone(t *testing.T) {
	srv := sseSpeechServer(t, []string{
		deltaFrame(t, []byte{0x01, 0x02}),
		deltaFrame(t, []byte{0x03}),
		`data: {"type":"speech.audio.done"}` + "\n\n",
	})
	defer srv.Close()
	c := newTestClient(srv)

	pcm, err := c.Speech(context.Background(), cnCred(), "hello", "")
	if err != nil {
		t.Fatal(err)
	}
	if string(pcm) != string([]byte{0x01, 0x02, 0x03}) {
		t.Fatalf("pcm=%v", pcm)
	}
}

// TestSpeechTruncatedWithoutDone 回归 panel d5ce7de：EOF 前已产出半段 PCM 但没见到
// speech.audio.done，必须报错交给上层换号，不能把残缺音频当 200 成功。
func TestSpeechTruncatedWithoutDone(t *testing.T) {
	srv := sseSpeechServer(t, []string{
		deltaFrame(t, []byte("half")),
		"data: [DONE]\n\n",
	})
	defer srv.Close()
	c := newTestClient(srv)

	pcm, err := c.Speech(context.Background(), cnCred(), "hello", "")
	if err == nil {
		t.Fatalf("truncated stream must fail, got %d bytes", len(pcm))
	}
	if !strings.Contains(err.Error(), "no speech.audio.done") {
		t.Fatalf("error=%v", err)
	}
}

// TestSpeechErrorFrame 上游 SSE 错误帧按带分类的 *Error 返回（不吞原因）。
func TestSpeechErrorFrame(t *testing.T) {
	srv := sseSpeechServer(t, []string{
		`data: {"type":"error","error":{"message":"voice not configured","code":11151}}` + "\n\n",
	})
	defer srv.Close()
	c := newTestClient(srv)

	_, err := c.Speech(context.Background(), cnCred(), "hello", "bad-voice")
	var ue *Error
	if err == nil || !asWorkbuddyError(err, &ue) {
		t.Fatalf("want classified *Error, got %#v", err)
	}
	if !strings.Contains(ue.Msg, "voice not configured") {
		t.Fatalf("msg=%q", ue.Msg)
	}
}

func asWorkbuddyError(err error, target **Error) bool {
	ue, ok := err.(*Error)
	if ok {
		*target = ue
	}
	return ok
}
