package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
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
	require.Equal(t, "2.0.0", suite.Version)
	require.Equal(t, "2.0.0", suite.ScoringVersion)
	require.Equal(t, 112, suite.ItemCount)
	items := GenerateModelBenchmarkItems(1, 2)
	require.Len(t, items, 112)
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

// 回归：constrained-permutations 题「前 fixed 个元素保持原位置」的正确答案是 (n-fixed)!，
// 旧实现误用 n!/fixed!（"保持相对顺序"的公式）导致所有模型被误判 0 分。
func TestModelBenchmarkConstrainedPermutationsAnswer(t *testing.T) {
	spec := modelBenchmarkFamilySpec{id: "constrained-permutations", category: "math", difficulty: 5, checker: "integer"}
	re := regexp.MustCompile(`计算 (\d+) 个不同元素的排列数，要求前 (\d+) 个`)
	for variant := 1; variant <= 2; variant++ {
		for offset := 0; offset < 100; offset++ {
			rng := newModelBenchmarkRNG(modelBenchmarkSeed(1, int64(1000+offset), spec.id, variant))
			prompt, expected := generateModelBenchmarkInstance(spec, rng, variant)
			match := re.FindStringSubmatch(prompt)
			require.NotNil(t, match, "unexpected prompt: %s", prompt)
			n, _ := strconv.Atoi(match[1])
			fixed, _ := strconv.Atoi(match[2])
			want := 1
			for i := 2; i <= n-fixed; i++ {
				want *= i
			}
			require.Equal(t, want, expected, "n=%d fixed=%d prompt=%s", n, fixed, prompt)
		}
	}
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

func TestModelBenchmarkRegistrySupportsIntegratedPlatformsAndOpenAIOAuth(t *testing.T) {
	cases := []struct {
		name    string
		account *Account
		want    bool
	}{
		{
			name: "workbuddy oauth with access token",
			account: &Account{ID: 1, Platform: PlatformWorkbuddy, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "wb-token", "model_mapping": map[string]any{"deepseek-v4.1-flash": "deepseek-v4.1-flash"},
			}},
			want: true,
		},
		{
			name:    "workbuddy oauth without token",
			account: &Account{ID: 2, Platform: PlatformWorkbuddy, Type: AccountTypeOAuth, Credentials: map[string]any{}},
			want:    false,
		},
		{
			name: "qoder oauth with access token",
			account: &Account{ID: 3, Platform: PlatformQoder, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "qoder-token", "model_mapping": map[string]any{"Qwen3.8-Flash": "Qwen3.8-Flash"},
			}},
			want: true,
		},
		{
			name: "trae oauth uses jwt credential",
			account: &Account{ID: 4, Platform: PlatformTrae, Type: AccountTypeOAuth, Credentials: map[string]any{
				"jwt": "trae-jwt", "model_mapping": map[string]any{"minimax-m3": "minimax-m3"},
			}},
			want: true,
		},
		{
			name: "openai oauth with access token",
			account: &Account{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"access_token": "oa-token", "model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"},
			}},
			want: true,
		},
		{
			name:    "openai oauth without access token",
			account: &Account{ID: 6, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{}},
			want:    false,
		},
		{
			name: "openai apikey stays supported",
			account: &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "sk-test", "model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"},
			}},
			want: true,
		},
		{
			name: "anthropic apikey remains unsupported",
			account: &Account{ID: 8, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "sk-ant", "model_mapping": map[string]any{"claude-sonnet-5": "claude-sonnet-5"},
			}},
			want: false,
		},
		{
			name: "image model target rejected",
			account: &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
				"api_key": "sk-test", "model_mapping": map[string]any{"gpt-image-2": "gpt-image-2"},
			}},
			want: false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			modelID := testCase.account.GetMappedModel(firstModelMappingKey(testCase.account))
			require.Equal(t, testCase.want, IsSupportedModelBenchmarkTarget(testCase.account, modelID))
		})
	}
}

// firstModelMappingKey 取账号映射里的首个对外模型名；无映射时回落到通用模型名，
// 以保证「图片模型」等用例仍能命中排除分支。
func firstModelMappingKey(account *Account) string {
	for key := range account.GetModelMapping() {
		return key
	}
	return "gpt-5.6-sol"
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
