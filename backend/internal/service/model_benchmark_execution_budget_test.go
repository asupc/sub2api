//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 回环网关模式下截断/过滤归类：不完整 finish_reason 必须保留证据并归类失败原因。
func TestModelBenchmarkExecutionClassifiesTruncation(t *testing.T) {
	for _, content := range []string{"", `{"answer":7}`} {
		for _, reason := range []string{"length", "max_tokens", "content_filter"} {
			t.Run(reason+content, func(t *testing.T) {
				chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": content}, "finish_reason": reason}}, "usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 8, "total_tokens": 13}})
				sse := "data: " + string(chunk) + "\n\ndata: [DONE]\n\n"
				server := modelBenchmarkLoopbackGateway(t, sse, nil)
				executor := modelBenchmarkLoopbackExecutor(t, modelBenchmarkLoopbackConfig(t, server))
				result := executor.Execute(context.Background(), &ModelBenchmarkPlan{ModelID: "fixture", MaxOutputTokens: 2048}, ModelBenchmarkItem{ID: "fixture", Category: "math", Difficulty: 4, Checker: "integer", Expected: 7})
				require.Equal(t, "execution_failed", result.Status)
				require.Equal(t, modelBenchmarkIncompleteReason(reason), result.FailureReason)
				require.Equal(t, content, result.ResponseText)
				require.Equal(t, int64(8), *result.OutputTokens)
			})
		}
	}
}

func TestModelBenchmarkStreamingStopEvidence(t *testing.T) {
	service := &AccountTestService{}
	for _, tc := range []struct {
		name, body, reason string
		responses          bool
	}{
		{"chat", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n", "length", false},
		{"responses", "data: {\"type\":\"response.incomplete\",\"response\":{\"incomplete_details\":{\"reason\":\"max_output_tokens\"},\"usage\":{\"input_tokens\":4,\"output_tokens\":8,\"total_tokens\":12}}}\n", "max_output_tokens", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/fixture", nil)
			c.Set(accountTestBenchmarkKey, true)
			if tc.responses {
				require.Error(t, service.processOpenAIStream(c, strings.NewReader(tc.body)))
			} else {
				require.NoError(t, service.processOpenAIChatCompletionsStream(c, strings.NewReader(tc.body)))
			}
			require.Contains(t, recorder.Body.String(), "benchmark_finish")
			require.Contains(t, recorder.Body.String(), tc.reason)
		})
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(accountTestBenchmarkLimitKey, 2048)
	require.Equal(t, 2048, modelBenchmarkOutputLimit(c))
}

func TestModelBenchmarkProviderBudgetIsDiagnosticOnly(t *testing.T) {
	run := &ModelBenchmarkRun{SuiteVersion: ModelBenchmarkSuiteVersion, ItemCount: len(modelBenchmarkFamilies), SampleCount: 1}
	items := []ModelBenchmarkRunItem{}
	for _, item := range GenerateModelBenchmarkComparisonItems("provider", 1) {
		raw, _ := json.Marshal(map[string]any{"answer": item.Expected})
		items = append(items, ModelBenchmarkScoreAnswer(item, string(raw)))
	}
	AggregateModelBenchmarkRun(run, items)
	require.Equal(t, 100.0, run.Quality.CompletionRate)
	require.False(t, run.Quality.RankingEligible)
	require.Equal(t, "provider_managed_budget", run.Quality.RankingReason)
}

func TestModelBenchmarkRunnerShutdownCancelsExecution(t *testing.T) {
	runner := NewModelBenchmarkRunnerService(nil, nil, nil)
	ctx := runner.lifetime
	runner.Stop()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("runner lifetime was not canceled")
	}
}
