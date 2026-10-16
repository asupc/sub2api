package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

func TestModelBenchmarkSuiteGeneratesHardV1(t *testing.T) {
	suite := GetModelBenchmarkSuite()
	require.Equal(t, "hard-v1", suite.ID)
	require.Equal(t, "1.1.0", suite.Version)
	require.Equal(t, 48, suite.ItemCount)
	items := GenerateModelBenchmarkItems(1, 2)
	require.Len(t, items, 48)
	seen := map[string]bool{}
	for _, item := range items {
		require.False(t, seen[item.ID], "duplicate item %s", item.ID)
		seen[item.ID] = true
		require.Condition(t, func() (success bool) { return item.Difficulty >= 3 && item.Difficulty <= 5 })
		require.NotEmpty(t, item.Prompt)
		require.Len(t, item.PromptHash, 64)
		require.Contains(t, item.Prompt, `"answer"`)
		require.Contains(t, item.Prompt, "只返回一个 JSON 对象")
		require.Condition(t, func() bool {
			for _, r := range item.Prompt {
				if unicode.Is(unicode.Han, r) {
					return true
				}
			}
			return false
		}, "prompt must be Chinese: %s", item.ID)
	}
	other := GenerateModelBenchmarkItems(1, 3)
	require.NotEqual(t, items[0].PromptHash, other[0].PromptHash)
	require.Equal(t, items[0].PromptHash, GenerateModelBenchmarkItems(1, 2)[0].PromptHash)
}

type modelBenchmarkManifestAccountSource struct {
	accounts []Account
}

func (s modelBenchmarkManifestAccountSource) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			return &s.accounts[i], nil
		}
	}
	return nil, fmt.Errorf("account %d not found", id)
}

func (s modelBenchmarkManifestAccountSource) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	return s.accounts, nil
}

func TestModelBenchmarkListTargetsFollowsModelManifest(t *testing.T) {
	accounts := []Account{
		{ID: 1, Name: "supported", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{"model-a": "model-a"}}},
		{ID: 2, Name: "unsupported", Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{"model-b": "model-b"}}},
	}
	source := modelBenchmarkManifestAccountSource{accounts: accounts}
	service := NewModelBenchmarkService(nil, source, NewModelManifestService(source), nil)
	targets, err := service.ListTargets(context.Background())
	require.NoError(t, err)
	require.Len(t, targets, 1)
	require.Equal(t, "model-a", targets[0].ID)
	require.Equal(t, []int64{1}, []int64{targets[0].Accounts[0].ID})
}

func TestModelBenchmarkScoreAnswerCheckers(t *testing.T) {
	cases := []ModelBenchmarkItem{
		{ID: "int", Category: "math", Difficulty: 4, Checker: "integer", Expected: int64(42), ExpectedOutputTokens: 100, ExpectedLatencyMS: 1000},
		{ID: "number", Category: "math", Difficulty: 3, Checker: "number_tolerance", Expected: 1.25, ExpectedOutputTokens: 100, ExpectedLatencyMS: 1000},
		{ID: "set", Category: "logic", Difficulty: 4, Checker: "set", Expected: []any{"B", "A"}, ExpectedOutputTokens: 100, ExpectedLatencyMS: 1000},
		{ID: "fields", Category: "code", Difficulty: 5, Checker: "fields", Expected: map[string]any{"defect": "data_race", "safe": false}, ExpectedOutputTokens: 100, ExpectedLatencyMS: 1000},
	}
	for _, item := range cases {
		raw, err := json.Marshal(map[string]any{"answer": item.Expected})
		require.NoError(t, err)
		payload := "```json\n" + string(raw) + "\n```"
		result := ModelBenchmarkScoreAnswer(item, payload)
		require.Equal(t, 1.0, result.Score, item.ID)
		require.Equal(t, "correct", result.Status, item.ID)
		require.Equal(t, item.ExpectedOutputTokens, result.ExpectedOutputTokens)
	}
	bad := ModelBenchmarkScoreAnswer(cases[0], "not-json")
	require.Equal(t, "format_error", bad.Status)
	require.Zero(t, bad.Score)
}

func TestModelBenchmarkAggregateExecutionFailureDoesNotBecomeZeroIntelligence(t *testing.T) {
	run := &ModelBenchmarkRun{UsageFieldCoverage: map[string]bool{}, CategoryScores: map[string]float64{}}
	items := []ModelBenchmarkRunItem{
		{Status: "correct", Score: 1, EarnedPoints: 2, PossiblePoints: 2, ExpectedOutputTokens: 10, ExpectedLatencyMS: 100, OutputTokens: modelBenchmarkPtrInt64(10), LatencyMS: modelBenchmarkPtrInt64(100)},
		{Status: "execution_failed", Score: 0, EarnedPoints: 0, PossiblePoints: 3, ExpectedOutputTokens: 10, ExpectedLatencyMS: 100, UsageSource: "missing"},
	}
	AggregateModelBenchmarkRun(run, items)
	require.NotNil(t, run.IntelligenceScore)
	require.Equal(t, 100.0, *run.IntelligenceScore)
	require.Equal(t, 1, run.AnsweredCount)
	require.Equal(t, "partial", run.Status)
	require.Nil(t, run.EfficiencyScore, "usage-incomplete run must not produce comparable efficiency")
	require.Nil(t, run.CompositeScore)
}

type modelBenchmarkAccountSource struct{ account *Account }

func (s modelBenchmarkAccountSource) GetByID(context.Context, int64) (*Account, error) {
	return s.account, nil
}

type modelBenchmarkHTTPUpstream struct{ request *http.Request }

func (u *modelBenchmarkHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.DoWithTLS(req, "", 0, 0, nil)
}

func (u *modelBenchmarkHTTPUpstream) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.request = req
	body := `{"choices":[{"message":{"content":"{\"answer\":7}"}}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18,"completion_tokens_details":{"reasoning_tokens":3}}}`
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func TestModelBenchmarkExecutorCapturesUsage(t *testing.T) {
	account := &Account{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: map[string]any{"api_key": "sk-test"}}
	upstream := &modelBenchmarkHTTPUpstream{}
	executor := NewModelBenchmarkExecutor(modelBenchmarkAccountSource{account}, upstream, nil, nil, nil, &config.Config{})
	item := ModelBenchmarkItem{ID: "mod", Category: "math", Difficulty: 4, Checker: "integer", Prompt: "return 7", Expected: int64(7), PromptHash: "hash", ExpectedOutputTokens: 20, ExpectedLatencyMS: 1000}
	plan := &ModelBenchmarkPlan{AccountID: 5, ModelID: "gpt-test", MaxOutputTokens: 128}
	result := executor.Execute(context.Background(), plan, item)
	require.Equal(t, "correct", result.Status)
	require.Equal(t, int64(11), *result.InputTokens)
	require.Equal(t, int64(7), *result.OutputTokens)
	require.Equal(t, int64(18), *result.TotalTokens)
	require.Equal(t, int64(3), *result.ReasoningTokens)
	require.Equal(t, "upstream", result.UsageSource)
	require.True(t, result.UsageFieldCoverage["reasoning"])
	require.Equal(t, "https://api.openai.com/v1/chat/completions", upstream.request.URL.String())
	require.Equal(t, "Bearer sk-test", upstream.request.Header.Get("Authorization"))
}

func TestGeneratedBenchmarkExpectedAnswersPassOwnCheckers(t *testing.T) {
	for _, item := range GenerateModelBenchmarkItems(9, 10) {
		raw, err := json.Marshal(map[string]any{"answer": item.Expected})
		require.NoError(t, err, item.ID)
		result := ModelBenchmarkScoreAnswer(item, string(raw))
		require.Equal(t, 1.0, result.Score, item.ID+": "+result.FailureReason)
		require.Equal(t, "correct", result.Status, item.ID)
	}
}

func TestGeneratedBenchmarkFamiliesVaryAcrossRuns(t *testing.T) {
	hashesByItem := map[string]map[string]struct{}{}
	for runID := int64(2); runID <= 20; runID++ {
		for _, item := range GenerateModelBenchmarkItems(1, runID) {
			if hashesByItem[item.ID] == nil {
				hashesByItem[item.ID] = map[string]struct{}{}
			}
			hashesByItem[item.ID][item.PromptHash] = struct{}{}
		}
	}
	for itemID, hashes := range hashesByItem {
		require.Greater(t, len(hashes), 1, "item %s should vary across runs", itemID)
	}
}

func modelBenchmarkPtrInt64(value int64) *int64 { return &value }

func TestModelBenchmarkAggregateCompositeWeights(t *testing.T) {
	run := &ModelBenchmarkRun{UsageFieldCoverage: map[string]bool{}, CategoryScores: map[string]float64{}}
	items := []ModelBenchmarkRunItem{
		{Status: "correct", Score: 1, EarnedPoints: 2, PossiblePoints: 2, ExpectedOutputTokens: 10, ExpectedLatencyMS: 100, OutputTokens: modelBenchmarkPtrInt64(10), LatencyMS: modelBenchmarkPtrInt64(100), UsageSource: "upstream"},
		{Status: "incorrect", Score: 0, EarnedPoints: 0, PossiblePoints: 2, ExpectedOutputTokens: 10, ExpectedLatencyMS: 100, OutputTokens: modelBenchmarkPtrInt64(10), LatencyMS: modelBenchmarkPtrInt64(100), UsageSource: "upstream"},
	}
	AggregateModelBenchmarkRun(run, items)
	require.NotNil(t, run.IntelligenceScore)
	require.Equal(t, 50.0, *run.IntelligenceScore)
	require.NotNil(t, run.EfficiencyScore)
	require.Equal(t, 100.0, *run.EfficiencyScore)
	require.NotNil(t, run.CompositeScore)
	require.Equal(t, 57.5, *run.CompositeScore)
}
