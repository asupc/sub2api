//go:build unit

package workbuddy

// search_test.go 联网搜索客户端单测：请求形状 / 扁平响应解析 / 条数收敛 /
// 短 query 拒绝 / 错误分类。移植自 workbuddy2api-panel internal/upstream/search_test.go
// （commit 58caff1），断言口径逐条保留，仅适配本包 Credentials 与 Client 形态。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// searchTestServer 构造指向 httptest 假上游的客户端（复用 client_test 的 rewriteTransport）。
func searchTestServer(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newTestClient(srv)
}

// TestSearchRequestShape 断言搜索出站请求的形状与官方契约一致：
// 路径 /agenttool/v1/search、请求体三字段顺序、以及对齐官方 WebSearchTool 的请求头。
func TestSearchRequestShape(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotRawBody string
	var got http.Header
	c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		got = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		gotRawBody = string(raw)
		if err := json.Unmarshal(raw, &gotBody); err != nil {
			t.Errorf("body must be JSON: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"golang","type":"text2text","provider":"0","results":[],"total_results":0}`))
	})
	cred := Credentials{AccessToken: "at", UID: "u1"}
	// 前后空白必须被裁掉；maxResults=0 → 回落官方默认值 5。
	if _, err := c.Search(context.Background(), cred, "  golang  ", 0, ChatMeta{}); err != nil {
		t.Fatalf("search: %v", err)
	}

	if gotPath != "/agenttool/v1/search" {
		t.Errorf("path=%q want /agenttool/v1/search", gotPath)
	}
	if gotBody["query"] != "golang" {
		t.Errorf("query=%v want golang (trimmed)", gotBody["query"])
	}
	if gotBody["type"] != "text2text" {
		t.Errorf("type=%v want text2text (官方硬编码)", gotBody["type"])
	}
	if gotBody["max_results"] != float64(SearchDefaultMaxResults) {
		t.Errorf("max_results=%v want %d", gotBody["max_results"], SearchDefaultMaxResults)
	}
	// 字段顺序逐字节对齐官方 buildRequestBody（query → type → max_results）。
	// JSON 语义上顺序无关，但对齐后抓包比对才是同形。
	if want := `{"query":"golang","type":"text2text","max_results":5}`; gotRawBody != want {
		t.Errorf("body=%s\n     want=%s（字段顺序须与官方一致）", gotRawBody, want)
	}

	// 官方 WebSearchTool 的关键头（本包在 ChatHeaders 之上覆盖的四项 + 认证/身份）。
	if v := got.Get("Accept"); v != "application/json" {
		t.Errorf("Accept=%q want application/json（搜索非流式）", v)
	}
	if v := got.Get("Content-Type"); v != "application/json;charset=UTF-8" {
		t.Errorf("Content-Type=%q want application/json;charset=UTF-8", v)
	}
	if v := got.Get("X-Agent-Intent"); v != "craft" {
		t.Errorf("X-Agent-Intent=%q want craft", v)
	}
	if v := got.Get("X-Agent-Runtime"); v != "other" {
		t.Errorf("X-Agent-Runtime=%q want other（2.166.0 起官方必发）", v)
	}
	if v := got.Get("Authorization"); v != "Bearer at" {
		t.Errorf("Authorization=%q want Bearer at", v)
	}
	if v := got.Get("X-User-Id"); v != "u1" {
		t.Errorf("X-User-Id=%q want u1", v)
	}
	// 会话头族（复用 ChatHeaders）：X-Conversation-Message-ID 与 X-Request-ID 同值。
	if got.Get("X-Conversation-Request-ID") == "" {
		t.Error("X-Conversation-Request-ID must be set (轮级聚合主键)")
	}
	if got.Get("X-Request-ID") == "" || got.Get("X-Conversation-Message-ID") != got.Get("X-Request-ID") {
		t.Error("X-Conversation-Message-ID 必须与 X-Request-ID 同值")
	}
}

// TestSearchParsesFlatResponse 断言扁平响应（非 {code,msg,data} 信封）能完整解析，
// 包括官方会读的 site / favicon 可选字段。
func TestSearchParsesFlatResponse(t *testing.T) {
	c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"query":"OpenAI 最新模型",
			"type":"text2text",
			"provider":"0",
			"results":[
				{"title":"模型发布说明","url":"https://help.openai.com/x","snippet":"摘要","site":"help.openai.com","favicon":"https://help.openai.com/f.ico"},
				{"title":"无 favicon","url":"https://example.com/y","snippet":"s2"}
			],
			"total_results":2,
			"response_time_ms":625
		}`))
	})
	out, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "OpenAI 最新模型", 5, ChatMeta{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if out.Query != "OpenAI 最新模型" || out.Provider != "0" || out.TotalResults != 2 || out.ResponseTimeMs != 625 {
		t.Fatalf("outcome=%+v", out)
	}
	if len(out.Results) != 2 {
		t.Fatalf("results=%d want 2", len(out.Results))
	}
	first := out.Results[0]
	if first.Title != "模型发布说明" || first.URL != "https://help.openai.com/x" || first.Site != "help.openai.com" || first.Favicon == "" {
		t.Errorf("results[0]=%+v", first)
	}
	if out.Results[1].Favicon != "" {
		t.Errorf("results[1].Favicon=%q want empty", out.Results[1].Favicon)
	}
}

// TestSearchResultsNeverNull 断言空结果输出为空数组而非 null（JSON 消费方无需判空），
// 且 total_results 缺省时用实际条数兜底。
func TestSearchResultsNeverNull(t *testing.T) {
	c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query":"x","type":"text2text","provider":"0","total_results":0}`))
	})
	out, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "xy", 5, ChatMeta{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if out.Results == nil {
		t.Fatal("Results must be non-nil empty slice")
	}
	raw, _ := json.Marshal(out)
	if got := string(raw); got == "" || !strings.Contains(got, `"results":[]`) {
		t.Errorf("marshalled=%s want results:[]", got)
	}

	// total_results 缺省 + 有条目 → 用实际条数兜底。
	c2 := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query":"x","results":[{"title":"t","url":"https://e.com"}]}`))
	})
	out2, err := c2.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "xy", 5, ChatMeta{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if out2.TotalResults != 1 {
		t.Errorf("TotalResults=%d want 1（缺省兜底为实际条数）", out2.TotalResults)
	}
}

// TestSearchClampsMaxResults 断言条数在入口收敛：0 → 官方默认 5；超上限 → 10；
// 合法值原样。服务端实际上限约 10（实测 15/30/50 均只回 9–10 条）。
func TestSearchClampsMaxResults(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, SearchDefaultMaxResults},
		{-3, SearchDefaultMaxResults},
		{1, 1},
		{5, 5},
		{10, 10},
		{11, SearchMaxResultsCap},
		{50, SearchMaxResultsCap},
	}
	for _, tc := range cases {
		var got int
		c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			if v, ok := body["max_results"].(float64); ok {
				got = int(v)
			}
			_, _ = w.Write([]byte(`{"query":"x","results":[],"total_results":0}`))
		})
		if _, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "xy", tc.in, ChatMeta{}); err != nil {
			t.Fatalf("in=%d: %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("max_results in=%d got=%d want=%d", tc.in, got, tc.want)
		}
	}
}

// TestSearchRejectsShortQuery 断言短 query 在本地就被拒（不浪费一次上游调用），
// 与官方 zod schema 的 query.min(2) 同口径。
func TestSearchRejectsShortQuery(t *testing.T) {
	called := false
	c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{}`))
	})
	for _, q := range []string{"", " ", "a", "  a  "} {
		_, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, q, 5, ChatMeta{})
		var ue *Error
		if !errors.As(err, &ue) {
			t.Fatalf("query=%q err=%v want *Error", q, err)
		}
		if ue.Kind != KindClient {
			t.Errorf("query=%q kind=%v want KindClient", q, ue.Kind)
		}
	}
	if called {
		t.Error("短 query 不应触发上游调用")
	}
}

// TestSearchBusinessErrorIsTyped 断言 2xx + code!=0 也按 *Error 返回（上游业务错误约定），
// 且 Kind 落到 KindClient 兜底——不能让业务错误被当成成功空结果。
func TestSearchBusinessErrorIsTyped(t *testing.T) {
	c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":15003,"msg":"unsupported search type: text2search"}`))
	})
	_, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "xy", 5, ChatMeta{})
	var ue *Error
	if !errors.As(err, &ue) {
		t.Fatalf("err=%v want *Error", err)
	}
	if ue.Kind != KindClient {
		t.Errorf("kind=%v want KindClient", ue.Kind)
	}
	if !strings.Contains(ue.Msg, "15003") {
		t.Errorf("msg=%q want 含 code 15003", ue.Msg)
	}
}

// TestSearchHTTPErrorClassified 断言 HTTP 4xx/5xx 走 classifyString（与 chat 同口径），
// 供 service 决定罚不罚号；Retry-After 头解析进 Error.RetryAfter。
func TestSearchHTTPErrorClassified(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   ErrorKind
	}{
		{http.StatusUnauthorized, `<html>401 Authorization Required</html>`, KindClient},
		{http.StatusTooManyRequests, `{"code":429,"msg":"rate limit exceeded, will reset in 60s"}`, KindSoftRate},
		{http.StatusInternalServerError, `{"code":500,"msg":"internal error"}`, KindServer},
	}
	for _, tc := range cases {
		c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		_, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "xy", 5, ChatMeta{})
		var ue *Error
		if !errors.As(err, &ue) {
			t.Fatalf("status=%d err=%v want *Error", tc.status, err)
		}
		if ue.Status != tc.status {
			t.Errorf("status=%d got=%d", tc.status, ue.Status)
		}
		if ue.Kind != tc.want {
			t.Errorf("status=%d kind=%v want %v", tc.status, ue.Kind, tc.want)
		}
	}
}

// TestSearchRetryAfterParsed 断言限流响应的 Retry-After 头被解析进 Error.RetryAfter
// （与 ChatStreamContext 同口径，供 service 计算冷却时长）。
func TestSearchRetryAfterParsed(t *testing.T) {
	c := searchTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":429,"msg":"slow down"}`))
	})
	_, err := c.Search(context.Background(), Credentials{AccessToken: "at", UID: "u"}, "xy", 5, ChatMeta{})
	var ue *Error
	if !errors.As(err, &ue) {
		t.Fatalf("err=%v want *Error", err)
	}
	if ue.RetryAfter != 90*1e9 && ue.RetryAfter.Seconds() != 90 {
		t.Errorf("RetryAfter=%v want 90s", ue.RetryAfter)
	}
}
