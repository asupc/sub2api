//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type benchmarkBudgetUpstream struct{ body string }

func (u benchmarkBudgetUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(u.body)), Header: make(http.Header)}, nil
}
func (u benchmarkBudgetUpstream) DoWithTLS(req *http.Request, p string, a int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, p, a, n)
}

func TestModelBenchmarkExecutionClassifiesTruncation(t *testing.T) {
	for _, content := range []string{"", `{"answer":7}`} {
		for _, reason := range []string{"length", "max_tokens", "content_filter"} {
			t.Run(reason+content, func(t *testing.T) {
				raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}, "finish_reason": reason}}, "usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 8, "total_tokens": 13}})
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "fixture-only"}}
				executor := NewModelBenchmarkExecutor(modelBenchmarkAccountSource{account}, benchmarkBudgetUpstream{string(raw)}, nil, nil, nil, &config.Config{})
				result := executor.Execute(context.Background(), &ModelBenchmarkPlan{AccountID: 1, ModelID: "fixture", MaxOutputTokens: 2048}, ModelBenchmarkItem{ID: "fixture", Category: "math", Difficulty: 4, Checker: "integer", Expected: 7})
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
	run := &ModelBenchmarkRun{SuiteVersion: ModelBenchmarkSuiteVersion, ItemCount: 112, SampleCount: 1}
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
