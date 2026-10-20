package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ModelBenchmarkAPIKeySource is the narrow API key dependency required by the executor.
type ModelBenchmarkAPIKeySource interface {
	GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error)
}

// ModelBenchmarkExecutor sends one item through the public gateway loopback.
// 测评不再绑定具体账号：每题以标准 OpenAI Chat Completions 请求打回环网关
// （127.0.0.1:server.port），认证、分组白名单、调度选号、平台协议转换、
// 流式 usage 标准化与计费全部走网关真实链路，天然覆盖全部平台。
type ModelBenchmarkExecutor struct {
	apiKeys  ModelBenchmarkAPIKeySource
	settings *SettingService
	billing  *BillingService
	cfg      *config.Config
	client   *http.Client
}

// NewModelBenchmarkExecutor creates the gateway-loopback executor.
func NewModelBenchmarkExecutor(apiKeys ModelBenchmarkAPIKeySource, settings *SettingService, billing *BillingService, cfg *config.Config) *ModelBenchmarkExecutor {
	return &ModelBenchmarkExecutor{apiKeys: apiKeys, settings: settings, billing: billing, cfg: cfg, client: &http.Client{}}
}

// isModelBenchmarkSupportedModel is the model-level target matrix.
// 回环网关模式下任何平台均可转发，仅需排除无法用文本 JSON 契约评分的
// 图像/视频生成模型，并要求模型在清单中存在账号（网关有可调度目标）。
func isModelBenchmarkSupportedModel(modelID string, hasAccounts bool) bool {
	if !hasAccounts || strings.TrimSpace(modelID) == "" {
		return false
	}
	if isOpenAIImageModel(modelID) || isImageGenerationModel(modelID) || isGrokImageGenerationModel(modelID) || isGrokVideoGenerationModel(modelID) {
		return false
	}
	return true
}

// Execute sends one benchmark item to the gateway loopback and returns scoring evidence.
func (e *ModelBenchmarkExecutor) Execute(ctx context.Context, plan *ModelBenchmarkPlan, item ModelBenchmarkItem) ModelBenchmarkRunItem {
	apiKey, err := e.resolveAPIKey(ctx)
	if err != nil {
		return modelBenchmarkExecutionFailure(item, err)
	}
	payload := map[string]any{
		"model": plan.ModelID,
		"messages": []map[string]any{
			{"role": "user", "content": item.Prompt},
		},
		"temperature": 0,
		"max_tokens":  plan.MaxOutputTokens,
		"stream":      true,
		// 网关两条转发路径自身会合成/透传 usage，显式声明可兼容更严格的上游。
		"stream_options": map[string]any{"include_usage": true},
	}
	if level := planReasoningEffort(plan); level != "" {
		payload["reasoning_effort"] = level
	}
	rawPayload, _ := json.Marshal(payload)
	start := time.Now()
	var responseBody []byte
	var status int
	// err was declared by resolveAPIKey; reuse it so retry results replace prior failures.
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			// 短暂退避，避免在网关/上游短时抖动中立刻重試。
			select {
			case <-ctx.Done():
				err = ctx.Err()
				break
			case <-time.After(200 * time.Millisecond):
			}
			if err != nil {
				break
			}
		}
		responseBody, status, err = e.postGateway(ctx, apiKey, rawPayload)
		if modelBenchmarkRetryableResult(status, err) {
			continue
		}
		break
	}
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return modelBenchmarkExecutionFailure(item, err)
	}
	if status < 200 || status >= 300 {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("gateway returned %d: %s", status, truncateModelBenchmarkText(responseBody, 512)))
	}
	captured, err := parseModelBenchmarkGatewayStream(bytes.NewReader(responseBody))
	if err != nil {
		return modelBenchmarkExecutionFailure(item, err)
	}
	servedModel := captured.ServedModel
	if servedModel == "" {
		servedModel = plan.ModelID
	}
	if reason := modelBenchmarkIncompleteReason(captured.FinishReason); reason != "" {
		result := modelBenchmarkExecutionFailure(item, fmt.Errorf("upstream response incomplete: %s", captured.FinishReason))
		result.FailureReason = reason
		result.ResponseText = captured.Text
		result.LatencyMS = &latency
		result.InputTokens, result.OutputTokens, result.ReasoningTokens, result.CacheReadTokens, result.CacheWriteTokens, result.TotalTokens, result.UsageFieldCoverage = parseModelBenchmarkUsage(captured.Usage)
		if result.InputTokens != nil && result.OutputTokens != nil && result.TotalTokens != nil {
			result.UsageSource = "upstream"
		}
		e.applyCost(&result, servedModel)
		return result
	}
	if captured.Text == "" {
		return modelBenchmarkExecutionFailure(item, fmt.Errorf("gateway returned empty content: %s", truncateModelBenchmarkText(responseBody, 256)))
	}
	result := ModelBenchmarkScoreAnswer(item, captured.Text)
	result.LatencyMS = &latency
	result.InputTokens, result.OutputTokens, result.ReasoningTokens, result.CacheReadTokens, result.CacheWriteTokens, result.TotalTokens, result.UsageFieldCoverage = parseModelBenchmarkUsage(captured.Usage)
	if result.InputTokens != nil && result.OutputTokens != nil && result.TotalTokens != nil {
		result.UsageSource = "upstream"
	}
	e.applyCost(&result, servedModel)
	return result
}

// resolveAPIKey loads the loopback credential from benchmark settings on every
// item so mid-run setting changes apply without restarts (two point queries).
func (e *ModelBenchmarkExecutor) resolveAPIKey(ctx context.Context) (string, error) {
	if e.settings == nil {
		return "", fmt.Errorf("model benchmark settings service is unavailable")
	}
	settings, err := e.settings.GetModelBenchmarkSettings(ctx)
	if err != nil {
		return "", err
	}
	if settings.APIKeyID == 0 {
		return "", fmt.Errorf("model benchmark API key is not configured")
	}
	if e.apiKeys == nil {
		return "", fmt.Errorf("model benchmark API key source is unavailable")
	}
	key, _, err := e.apiKeys.GetKeyAndOwnerID(ctx, settings.APIKeyID)
	if err != nil {
		return "", fmt.Errorf("resolve benchmark API key: %w", err)
	}
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("model benchmark API key is empty")
	}
	return key, nil
}

// gatewayChatCompletionsURL derives the loopback endpoint from the server config.
// 127.0.0.1 直连自身进程，绕过 nginx 反代与外部网络。
func (e *ModelBenchmarkExecutor) gatewayChatCompletionsURL() (string, error) {
	if e.cfg == nil {
		return "", fmt.Errorf("config is not available")
	}
	return fmt.Sprintf("http://127.0.0.1:%d/v1/chat/completions", e.cfg.Server.Port), nil
}

// modelBenchmarkRetryableResult reports whether a failed gateway response should be retried once.
// Only transport and transient gateway failures retry; business/account/format errors do not.
func modelBenchmarkRetryableResult(status int, err error) bool {
	if err != nil {
		return true
	}
	if status == http.StatusTooManyRequests || status == http.StatusRequestTimeout {
		return true
	}
	return status >= http.StatusInternalServerError
}

// postGateway performs one loopback request and returns the drained body.
func (e *ModelBenchmarkExecutor) postGateway(ctx context.Context, apiKey string, payload []byte) ([]byte, int, error) {
	endpoint, err := e.gatewayChatCompletionsURL()
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// modelBenchmarkGatewayStream is the aggregated view of one gateway SSE response.
type modelBenchmarkGatewayStream struct {
	Text         string
	FinishReason string
	Usage        map[string]any
	ServedModel  string
}

// parseModelBenchmarkGatewayStream aggregates an OpenAI-compatible SSE stream:
// delta text, final finish_reason, terminal usage chunk and the served model.
func parseModelBenchmarkGatewayStream(body io.Reader) (modelBenchmarkGatewayStream, error) {
	var out modelBenchmarkGatewayStream
	var texts []string
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return out, fmt.Errorf("gateway stream read error: %w", err)
		}
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonStr := strings.TrimPrefix(line, "data: ")
		if jsonStr == "[DONE]" {
			break
		}
		var data struct {
			Model   string `json:"model"`
			Choices []struct {
				FinishReason *string `json:"finish_reason"`
				Delta        struct {
					Content string `json:"content"`
				} `json:"delta"`
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(jsonStr), &data) != nil {
			continue
		}
		if data.Error != nil {
			message := data.Error.Message
			if message == "" {
				message = "gateway stream returned an error"
			}
			return out, fmt.Errorf("%s", message)
		}
		if data.Model != "" && out.ServedModel == "" {
			out.ServedModel = data.Model
		}
		if data.Usage != nil {
			out.Usage = data.Usage
		}
		for _, choice := range data.Choices {
			if choice.Delta.Content != "" {
				texts = append(texts, choice.Delta.Content)
			}
			if choice.Message.Content != "" {
				texts = append(texts, choice.Message.Content)
			}
			if choice.FinishReason != nil && *choice.FinishReason != "" {
				out.FinishReason = *choice.FinishReason
			}
		}
	}
	out.Text = strings.Join(texts, "")
	return out, nil
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

func planReasoningEffort(plan *ModelBenchmarkPlan) string {
	if plan == nil {
		return ""
	}
	if level := strings.TrimSpace(plan.ReasoningEffort); level != "" {
		return level
	}
	return strings.TrimSpace(plan.ReasoningLevel)
}

func modelBenchmarkIncompleteReason(reason string) string {
	switch reason {
	case "length", "max_tokens", "max_output_tokens":
		return "budget_exhausted"
	case "content_filter":
		return "content_filtered"
	case "incomplete":
		return "incomplete_response"
	}
	return ""
}
