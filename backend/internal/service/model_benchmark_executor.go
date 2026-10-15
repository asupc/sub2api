package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

// ModelBenchmarkAccountSource is the narrow account dependency required by the executor.
type ModelBenchmarkAccountSource interface {
	GetByID(ctx context.Context, id int64) (*Account, error)
}

// ModelBenchmarkExecutor sends one item to a supported OpenAI-compatible API-key upstream.
type ModelBenchmarkExecutor struct {
	accounts     ModelBenchmarkAccountSource
	httpUpstream HTTPUpstream
	tlsProfiles  *TLSFingerprintProfileService
	billing      *BillingService
	cfg          *config.Config
}

// NewModelBenchmarkExecutor creates the Phase 1 executor.
func NewModelBenchmarkExecutor(accounts ModelBenchmarkAccountSource, upstream HTTPUpstream, tlsProfiles *TLSFingerprintProfileService, billing *BillingService, cfg *config.Config) *ModelBenchmarkExecutor {
	return &ModelBenchmarkExecutor{accounts: accounts, httpUpstream: upstream, tlsProfiles: tlsProfiles, billing: billing, cfg: cfg}
}

// IsSupportedModelBenchmarkTarget enforces the Phase 1 adapter matrix.
func IsSupportedModelBenchmarkTarget(account *Account, modelID string) bool {
	if account == nil || account.Type != AccountTypeAPIKey || !account.IsOpenAICompatible() || IsSourceIntegratedPlatform(account.Platform) {
		return false
	}
	switch account.Platform {
	case PlatformOpenAI, PlatformKimi, PlatformZhipu, PlatformDeepseek, PlatformMiniMax, PlatformOpenCodeGo, PlatformCommandCode, PlatformCline:
	default:
		return false
	}
	if strings.TrimSpace(modelID) == "" || !account.IsModelSupported(modelID) {
		return false
	}
	mapped := account.GetMappedModel(modelID)
	if mapped == "" || isOpenAIImageModel(mapped) || isGrokImageGenerationModel(mapped) || isGrokVideoGenerationModel(mapped) {
		return false
	}
	if account.GetAPIProtocol() == APIProtocolAnthropic || account.IsAnthropicProtocol() {
		return false
	}
	return strings.TrimSpace(account.GetOpenAIProtocolAPIKey()) != ""
}

// Execute sends one benchmark item and returns scoring evidence.
func (e *ModelBenchmarkExecutor) Execute(ctx context.Context, plan *ModelBenchmarkPlan, item ModelBenchmarkItem) ModelBenchmarkRunItem {
	account, err := e.accounts.GetByID(ctx, plan.AccountID)
	if err != nil || !IsSupportedModelBenchmarkTarget(account, plan.ModelID) {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("unsupported benchmark target"))
	}
	upstreamModel := account.GetMappedModel(plan.ModelID)
	payload := map[string]any{
		"model":       upstreamModel,
		"messages":    []map[string]any{{"role": "user", "content": item.Prompt}},
		"temperature": 0,
		"stream":      false,
		"max_tokens":  plan.MaxOutputTokens,
	}
	rawPayload, _ := json.Marshal(payload)
	baseURL := strings.TrimSpace(account.GetOpenAIFormatBaseURL())
	if baseURL == "" {
		baseURL = account.GetOpenAIBaseURL()
	}
	baseURL, err = e.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("invalid base URL: %w", err))
	}
	endpoint := buildOpenAIChatCompletionsURL(baseURL)
	start := time.Now()
	responseBody, status, err := e.do(ctx, account, endpoint, rawPayload)
	if err != nil {
		return modelBenchmarkExecutionFailure(item, err)
	}
	if status < 200 || status >= 300 {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("upstream returned %d: %s", status, truncateModelBenchmarkText(responseBody, 512)))
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content any `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]any `json:"usage"`
	}
	if err := json.Unmarshal(responseBody, &parsed); err != nil {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("invalid upstream JSON: %w", err))
	}
	if len(parsed.Choices) == 0 {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("upstream returned no choices"))
	}
	content := normalizeModelBenchmarkChatContent(parsed.Choices[0].Message.Content)
	if content == "" {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("upstream returned empty content"))
	}

	result := ModelBenchmarkScoreAnswer(item, content)
	latency := time.Since(start).Milliseconds()
	result.LatencyMS = &latency
	result.InputTokens, result.OutputTokens, result.ReasoningTokens, result.CacheReadTokens, result.CacheWriteTokens, result.TotalTokens, result.UsageFieldCoverage = parseModelBenchmarkUsage(parsed.Usage)
	if result.InputTokens != nil && result.OutputTokens != nil && result.TotalTokens != nil {
		result.UsageSource = "upstream"
	}
	e.applyCost(&result, upstreamModel)
	return result
}

func (e *ModelBenchmarkExecutor) validateUpstreamBaseURL(raw string) (string, error) {
	if e.cfg == nil {
		return "", fmt.Errorf("config is not available")
	}
	if !e.cfg.Security.URLAllowlist.Enabled {
		return urlvalidator.ValidateURLFormat(raw, e.cfg.Security.URLAllowlist.AllowInsecureHTTP)
	}
	return urlvalidator.ValidateHTTPSURL(raw, urlvalidator.ValidationOptions{
		AllowedHosts:     e.cfg.Security.URLAllowlist.UpstreamHosts,
		RequireAllowlist: true,
		AllowPrivate:     e.cfg.Security.URLAllowlist.AllowPrivateHosts,
	})
}

func (e *ModelBenchmarkExecutor) do(ctx context.Context, account *Account, endpoint string, payload []byte) ([]byte, int, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+account.GetOpenAIProtocolAPIKey())
		account.ApplyHeaderOverrides(req.Header)
		proxyURL := ""
		if account.ProxyID != nil && account.Proxy != nil {
			proxyURL = account.Proxy.URL()
		}
		var profile *tlsfingerprint.Profile
		if e.tlsProfiles != nil {
			profile = e.tlsProfiles.ResolveTLSProfile(account)
		}
		resp, err := e.httpUpstream.DoWithTLS(req, proxyURL, account.ID, account.Concurrency, profile)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 && attempt == 0 {
			lastErr = fmt.Errorf("upstream returned %d", resp.StatusCode)
			continue
		}
		return body, resp.StatusCode, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("upstream request failed")
	}
	return nil, 0, lastErr
}

func (e *ModelBenchmarkExecutor) applyCost(item *ModelBenchmarkRunItem, model string) {
	if e.billing == nil || item.InputTokens == nil || item.OutputTokens == nil {
		return
	}
	tokens := UsageTokens{InputTokens: int(*item.InputTokens), OutputTokens: int(*item.OutputTokens)}
	if item.CacheReadTokens != nil {
		tokens.CacheReadTokens = int(*item.CacheReadTokens)
	}
	if item.CacheWriteTokens != nil {
		tokens.CacheCreationTokens = int(*item.CacheWriteTokens)
	}
	breakdown, err := e.billing.CalculateCost(model, tokens, 1)
	if err != nil || breakdown == nil {
		item.PricingSource = "missing"
		return
	}
	cost := breakdown.TotalCost
	item.EstimatedCost = &cost
	item.PricingSource = "pricing"
}

func modelBenchmarkExecutionFailure(item ModelBenchmarkItem, err error) ModelBenchmarkRunItem {
	weight := modelBenchmarkItemWeight(item)
	return ModelBenchmarkRunItem{
		ItemID: item.ID, FamilyID: item.FamilyID, Category: item.Category, Difficulty: item.Difficulty, Weight: weight,
		PromptHash: item.PromptHash, PromptSnapshot: item.Prompt, PossiblePoints: weight, CheckerType: item.Checker,
		Status: "execution_failed", ErrorMessage: err.Error(), PricingSource: "missing", UsageSource: "missing",
		UsageFieldCoverage: map[string]bool{}, ExpectedOutputTokens: item.ExpectedOutputTokens, ExpectedLatencyMS: item.ExpectedLatencyMS,
	}
}

func normalizeModelBenchmarkChatContent(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, part := range typed {
			if object, ok := part.(map[string]any); ok {
				if text, ok := object["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, ""))
	default:
		return ""
	}
}

func parseModelBenchmarkUsage(usage map[string]any) (input, output, reasoning, cacheRead, cacheWrite, total *int64, coverage map[string]bool) {
	coverage = map[string]bool{"input": false, "output": false, "total": false, "reasoning": false, "cache_read": false, "cache_write": false}
	getInt := func(keys ...string) (*int64, bool) {
		for _, key := range keys {
			if value, ok := usage[key]; ok {
				if converted, ok := modelBenchmarkUsageInt(value); ok {
					return converted, true
				}
			}
			if object, ok := usage[key].(map[string]any); ok {
				if nested, ok := object["reasoning_tokens"]; ok {
					if converted, ok := modelBenchmarkUsageInt(nested); ok {
						return converted, true
					}
				}
			}
		}
		return nil, false
	}
	input, coverage["input"] = getInt("prompt_tokens", "input_tokens")
	output, coverage["output"] = getInt("completion_tokens", "output_tokens")
	reasoning, coverage["reasoning"] = getInt("completion_tokens_details", "output_tokens_details")
	if nested, ok := usage["prompt_tokens_details"].(map[string]any); ok {
		cacheRead, coverage["cache_read"] = modelBenchmarkNestedUsageInt(nested, "cached_tokens")
	} else {
		cacheRead, coverage["cache_read"] = getInt("cache_read_input_tokens")
	}
	cacheWrite, coverage["cache_write"] = getInt("cache_creation_input_tokens")
	total, coverage["total"] = getInt("total_tokens")
	if total == nil && input != nil && output != nil {
		sum := *input + *output
		total = &sum
		coverage["total"] = true
	}
	return
}

func modelBenchmarkUsageInt(value any) (*int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed >= 0 && typed == float64(int64(typed)) {
			converted := int64(typed)
			return &converted, true
		}
	case int64:
		if typed >= 0 {
			return &typed, true
		}
	}
	return nil, false
}

func modelBenchmarkNestedUsageInt(object map[string]any, keys ...string) (*int64, bool) {
	for _, key := range keys {
		if converted, ok := modelBenchmarkUsageInt(object[key]); ok {
			return converted, true
		}
	}
	return nil, false
}

func truncateModelBenchmarkText(value []byte, limit int) string {
	if len(value) <= limit {
		return string(value)
	}
	return string(value[:limit]) + "..."
}
