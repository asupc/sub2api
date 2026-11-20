// web_tools.go Qoder 官方 Web 搜索 / 抓取（移植自 qoder2api-hub
// qoder_official_web.py + docs/official-web-tools.md，2026-11-16 真号验证）。
//
// 协议（官方 CLI 1.1.89 分发包静态分析 + 联网验证）：
//
//	CN  : POST {gateway}       /algo/api/v1/webSearch/oneSearch?Encode=1
//	                      /algo/api/v1/webFetch?Encode=1
//	INTL: POST https://center.qoder.sh 同路径（与 api1/api2/api3 推理服务分开）
//
// 外层 JSON {"payload":"<内层 JSON 字符串>","encodeVersion":"1"} 经 QoderEncode
// 编码后作为请求体；COSY 签名对象是**编码后的 body** 与去掉 /algo 前缀的路径。
// 只签未编码 JSON 会 500（2026-11-16 实测）。
//
// 安全边界：工具调用由模型驱动，输入 URL 必须在网关侧做公网校验；官方抓取由
// Qoder 服务器执行，网关无法钉扎远端 DNS，只能校验输入与回报的最终地址。
package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
)

const (
	webSearchPath = "/algo/api/v1/webSearch/oneSearch"
	webFetchPath  = "/algo/api/v1/webFetch"
	// webToolTimeout 官方客户端同口径的抓取超时（毫秒，参数值）。
	webToolTimeoutMS = 14000
	// webSearchMaxResults / webSnippetMaxChars / webFetchMaxChars 结果裁剪上限。
	webSearchMaxResults = 5
	webSnippetMaxChars  = 1000
	webFetchMaxChars    = 20000
	webFetchMaxLinks    = 10
)

// WebSearchResult 一条搜索结果（映射为本地工具契约 title/url/snippet）。
type WebSearchResult struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Snippet       string `json:"snippet,omitempty"`
	PublishedTime string `json:"published_time,omitempty"`
	Hostname      string `json:"hostname,omitempty"`
}

// WebFetchResult 一次页面抓取结果。
type WebFetchResult struct {
	URL       string   `json:"url"`
	FinalURL  string   `json:"final_url,omitempty"`
	Title     string   `json:"title,omitempty"`
	Text      string   `json:"text"`
	Links     []string `json:"links,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
}

// WebToolError 官方工具的确定性错误（不泄露上游 body / token）。
type WebToolError struct {
	Msg string
}

func (e *WebToolError) Error() string { return "qoder web tool: " + e.Msg }

// webToolOrigins 工具端点主机按区域分离（缓存）；INTL 固定 center.qoder.sh，
// QD_INTL_WEB_TOOLS_ORIGIN 可覆盖（测试/自建代理）。
func webToolOrigin(creds Credentials) (string, error) {
	if creds.NormalizedRealm() != "intl" {
		cfg := creds.RealmConfig()
		if cfg.VPCInstance != "" || strings.TrimSpace(creds.VPCEndpoint) != "" {
			// 企业 VPC 未验证：明确拒绝，不回退公网（源实现同口径）。
			return "", &WebToolError{Msg: "official web tools are not available for VPC accounts"}
		}
		origin := strings.TrimSuffix(strings.TrimSpace(cfg.Gateway), "/")
		if origin == "" {
			return "", &WebToolError{Msg: "no gateway origin configured for this realm"}
		}
		return origin, nil
	}
	if v := strings.TrimSpace(os.Getenv("QD_INTL_WEB_TOOLS_ORIGIN")); v != "" {
		return strings.TrimSuffix(v, "/"), nil
	}
	return "https://center.qoder.sh", nil
}

var (
	webToolSessionMu    sync.Mutex
	webToolSessionCache = NewCosySessionCache()
)

// webToolSession 取（或创建）账号的 COSY 会话。工具请求复用推理链的签名会话，
// access token 轮换后由缓存整体重建。
func webToolSession(creds Credentials) (*CosySession, error) {
	webToolSessionMu.Lock()
	defer webToolSessionMu.Unlock()
	return webToolSessionCache.Get(creds.Identity())
}

// WebSearch 调用官方 oneSearch。allowed/blocked 为应用级域名过滤（子域匹配），
// 结果在网关侧再过滤一次——请求参数无法被模型放宽（官方客户端同样二次检查）。
func (c *Client) WebSearch(ctx context.Context, creds Credentials, query string, limit int, allowed, blocked []string) ([]WebSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, &WebToolError{Msg: "search query is empty"}
	}
	if limit <= 0 || limit > webSearchMaxResults {
		limit = webSearchMaxResults
	}
	origin, err := webToolOrigin(creds)
	if err != nil {
		return nil, err
	}
	raw, err := c.webToolCall(ctx, creds, origin+webSearchPath+"?Encode=1", map[string]any{
		"query":     query,
		"timeRange": "NoLimit",
		"contents": map[string]any{
			"mainText":     false,
			"markdownText": false,
			"summary":      false,
		},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		RequestID string `json:"requestId"`
		PageItems []struct {
			Title         string `json:"title"`
			Link          string `json:"link"`
			Snippet       string `json:"snippet"`
			PublishedTime string `json:"publishedTime"`
			Hostname      string `json:"hostname"`
		} `json:"pageItems"`
		ErrorCode any    `json:"errorCode"`
		ErrorMsg  string `json:"errorMsg"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &WebToolError{Msg: "search response is not valid json"}
	}
	if msg := businessErrorCode(resp.ErrorCode); msg != "" {
		return nil, &WebToolError{Msg: "search failed: " + clipMessage(resp.ErrorMsg, msg)}
	}
	out := make([]WebSearchResult, 0, limit)
	seen := map[string]bool{}
	for _, item := range resp.PageItems {
		link := strings.TrimSpace(item.Link)
		if link == "" || seen[link] {
			continue
		}
		if !webURLAllowed(link, allowed, blocked) {
			continue
		}
		seen[link] = true
		out = append(out, WebSearchResult{
			Title:         strings.TrimSpace(item.Title),
			URL:           link,
			Snippet:       clipRunes(item.Snippet, webSnippetMaxChars),
			PublishedTime: strings.TrimSpace(item.PublishedTime),
			Hostname:      strings.TrimSpace(item.Hostname),
		})
		if len(out) >= limit {
			break
		}
	}
	if len(out) == 0 {
		return nil, &WebToolError{Msg: "search returned no usable results"}
	}
	return out, nil
}

// WebFetch 调用官方 webFetch。输入 URL 必须是公网 http/https；返回的最终地址
// 再次校验（远端已发生的抓取无法撤销，但结果不返回给模型）。
func (c *Client) WebFetch(ctx context.Context, creds Credentials, target string, allowed []string) (*WebFetchResult, error) {
	target = strings.TrimSpace(target)
	if err := validatePublicWebURL(target); err != nil {
		return nil, err
	}
	if len(allowed) > 0 && !webURLAllowed(target, allowed, nil) {
		return nil, &WebToolError{Msg: "url is outside the allowed domains"}
	}
	origin, err := webToolOrigin(creds)
	if err != nil {
		return nil, err
	}
	raw, err := c.webToolCall(ctx, creds, origin+webFetchPath+"?Encode=1", map[string]any{
		"url":     target,
		"maxAge":  0,
		"timeout": webToolTimeoutMS,
		"formats": []string{"markdown"},
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Results []struct {
			StatusCode int    `json:"statusCode"`
			Markdown   string `json:"markdown"`
			Text       string `json:"text"`
			HTML       string `json:"html"`
			RawHTML    string `json:"rawHtml"`
			URL        string `json:"url"`
			FinalURL   string `json:"finalUrl"`
			Metadata   struct {
				Title string `json:"title"`
			} `json:"metadata"`
			Links []any `json:"links"`
		} `json:"results"`
		ErrorCode any    `json:"errorCode"`
		ErrorMsg  string `json:"errorMsg"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &WebToolError{Msg: "fetch response is not valid json"}
	}
	if msg := businessErrorCode(resp.ErrorCode); msg != "" {
		return nil, &WebToolError{Msg: "fetch failed: " + clipMessage(resp.ErrorMsg, msg)}
	}
	if len(resp.Results) == 0 {
		return nil, &WebToolError{Msg: "fetch returned no results"}
	}
	first := resp.Results[0]
	if first.StatusCode >= 400 {
		return nil, &WebToolError{Msg: fmt.Sprintf("fetch failed: upstream status %d", first.StatusCode)}
	}
	body := first.Markdown
	if body == "" {
		body = first.Text
	}
	if body == "" {
		body = first.HTML
	}
	if body == "" {
		body = first.RawHTML
	}
	if strings.TrimSpace(body) == "" {
		return nil, &WebToolError{Msg: "fetch returned an empty body"}
	}
	finalURL := first.FinalURL
	if finalURL == "" {
		finalURL = first.URL
	}
	if finalURL != "" {
		if err := validatePublicWebURL(finalURL); err != nil {
			return nil, &WebToolError{Msg: "fetch redirected to a non-public address"}
		}
		if len(allowed) > 0 && !webURLAllowed(finalURL, allowed, nil) {
			return nil, &WebToolError{Msg: "fetch redirected outside the allowed domains"}
		}
	}
	out := &WebFetchResult{
		URL:      target,
		FinalURL: finalURL,
		Title:    strings.TrimSpace(first.Metadata.Title),
	}
	if runes := []rune(body); len(runes) > webFetchMaxChars {
		out.Text = string(runes[:webFetchMaxChars])
		out.Truncated = true
	} else {
		out.Text = body
	}
	for _, raw := range first.Links {
		if len(out.Links) >= webFetchMaxLinks {
			break
		}
		link := ""
		switch v := raw.(type) {
		case string:
			link = v
		case map[string]any:
			if s, _ := v["url"].(string); s != "" {
				link = s
			} else if s, _ := v["href"].(string); s != "" {
				link = s
			}
		}
		if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
			out.Links = append(out.Links, link)
		}
	}
	return out, nil
}

// webToolCall 构造编码 body + COSY 签名并 POST，返回原始 JSON。
func (c *Client) webToolCall(ctx context.Context, creds Credentials, rawURL string, inner map[string]any) (json.RawMessage, error) {
	sess, err := webToolSession(creds)
	if err != nil {
		return nil, &WebToolError{Msg: "failed to build signing session"}
	}
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		return nil, &WebToolError{Msg: "failed to encode tool arguments"}
	}
	outer, err := json.Marshal(map[string]any{
		"payload":       string(innerJSON),
		"encodeVersion": "1",
	})
	if err != nil {
		return nil, &WebToolError{Msg: "failed to encode tool request"}
	}
	body := []byte(QoderEncode(outer))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, &WebToolError{Msg: "failed to build tool request"}
	}
	req.Header = sess.Headers(string(body), rawURL, "", false, "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, &WebToolError{Msg: "tool upstream unreachable"}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, &WebToolError{Msg: "failed to read tool response"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 不回显原始 body / token / 异常内容，只给收敛后的错误。
		return nil, &WebToolError{Msg: fmt.Sprintf("tool upstream returned HTTP %d", resp.StatusCode)}
	}
	if !json.Valid(raw) {
		return nil, &WebToolError{Msg: "tool upstream returned a non-json body"}
	}
	return raw, nil
}

// businessErrorCode 判断业务错误码是否非成功（数字 0 / 空 / "0" 视为成功）。
func businessErrorCode(code any) string {
	switch v := code.(type) {
	case nil:
		return ""
	case float64:
		if v == 0 {
			return ""
		}
		return fmt.Sprintf("%.0f", v)
	case string:
		s := strings.TrimSpace(v)
		if s == "" || s == "0" {
			return ""
		}
		return s
	default:
		return "non-zero"
	}
}

// clipMessage 上游错误文案裁剪（不泄露原文以外的上下文）。
func clipMessage(msg, fallback string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fallback
	}
	return clipRunes(msg, 200)
}

// clipRunes 按 rune 截断（多字节安全）。
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// validatePublicWebURL 只允许 http/https、无凭据、主机解析为公网地址。
// 域名解析失败时拒绝（宁缺毋滥）；字面 IP 直接判公/私网。
func validatePublicWebURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return &WebToolError{Msg: "invalid url"}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return &WebToolError{Msg: "only http/https urls are allowed"}
	}
	if u.User != nil {
		return &WebToolError{Msg: "urls with credentials are not allowed"}
	}
	host := u.Hostname()
	if host == "" || strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return &WebToolError{Msg: "private or loopback addresses are not allowed"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicIP(ip) {
			return &WebToolError{Msg: "private or loopback addresses are not allowed"}
		}
		return nil
	}
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil || len(ips) == 0 {
		return &WebToolError{Msg: "host could not be resolved"}
	}
	for _, addr := range ips {
		if !isPublicIP(addr.IP) {
			return &WebToolError{Msg: "host resolves to a private address"}
		}
	}
	return nil
}

// isPublicIP 报告 IP 是否公网可达（排除环回、私网、链路本地、多播等）。
func isPublicIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return false
	}
	return true
}

// webURLAllowed 域名过滤：allowed 非空时必须命中其一；blocked 命中即拒。
// 匹配口径为精确主机或子域（docs.python.org 命中 docs.python.org 与 *.docs.python.org）。
func webURLAllowed(raw string, allowed, blocked []string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, b := range blocked {
		if hostMatchesDomain(host, b) {
			return false
		}
	}
	if len(allowed) == 0 {
		return true
	}
	for _, a := range allowed {
		if hostMatchesDomain(host, a) {
			return true
		}
	}
	return false
}

// hostMatchesDomain 精确域名或子域匹配。
func hostMatchesDomain(host, domain string) bool {
	d := strings.ToLower(strings.TrimSpace(domain))
	d = strings.TrimPrefix(d, ".")
	if d == "" {
		return false
	}
	return host == d || strings.HasSuffix(host, "."+d)
}
