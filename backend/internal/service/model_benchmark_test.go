package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestModelBenchmarkSuiteGeneratesHardV1(t *testing.T) {
	suite := GetModelBenchmarkSuite()
	require.Equal(t, "hard-v1", suite.ID)
	require.Equal(t, "7.0.0", suite.Version)
	require.Equal(t, "7.0.0", suite.ScoringVersion)
	require.Equal(t, len(modelBenchmarkFamilies), suite.ItemCount)
	require.Equal(t, len(modelBenchmarkFamilies), suite.FamilyCount)
	items := GenerateModelBenchmarkItems(1, 2)
	require.Len(t, items, len(modelBenchmarkFamilies))
	seen := map[string]bool{}
	for _, item := range items {
		require.False(t, seen[item.ID], "duplicate item %s", item.ID)
		seen[item.ID] = true
		require.Condition(t, func() (success bool) { return item.Difficulty >= 3 && item.Difficulty <= 7 })
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

// 回环网关模式下目标矩阵是模型级的：任何平台的账号都构成可测评目标，
// 只排除无可调度账号的模型与图像/视频生成模型。
func TestModelBenchmarkListTargetsFollowsModelManifest(t *testing.T) {
	accounts := []Account{
		{ID: 1, Name: "openai-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{"model-a": "model-a"}}},
		{ID: 2, Name: "antigravity-oauth", Platform: PlatformAntigravity, Type: AccountTypeOAuth, Status: "active", Schedulable: true, Credentials: map[string]any{"access_token": "t", "model_mapping": map[string]any{"claude-opus-4-8": "claude-opus-4-8", "gemini-2.5-flash-image": "gemini-2.5-flash-image"}}},
		{ID: 3, Name: "expired", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: "error", Schedulable: false, Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{"model-c": "model-c"}}},
	}
	source := modelBenchmarkManifestAccountSource{accounts: accounts}
	service := NewModelBenchmarkService(nil, nil, nil, NewModelManifestService(source), nil)
	targets, err := service.ListTargets(context.Background())
	require.NoError(t, err)
	ids := make([]string, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
	}
	require.ElementsMatch(t, []string{"model-a", "claude-opus-4-8"}, ids)
}

func TestModelBenchmarkSupportedModelMatrix(t *testing.T) {
	cases := []struct {
		name        string
		modelID     string
		hasAccounts bool
		want        bool
	}{
		{name: "text model with accounts", modelID: "claude-opus-4-8", hasAccounts: true, want: true},
		{name: "text model without accounts", modelID: "claude-opus-4-8", hasAccounts: false, want: false},
		{name: "empty model id", modelID: "  ", hasAccounts: true, want: false},
		{name: "openai image model", modelID: "gpt-image-2", hasAccounts: true, want: false},
		{name: "gemini image model", modelID: "gemini-2.5-flash-image", hasAccounts: true, want: false},
		{name: "gemini image preview model", modelID: "gemini-3-pro-image-preview", hasAccounts: true, want: false},
		{name: "grok image model", modelID: "grok-imagine-image-quality", hasAccounts: true, want: false},
		{name: "grok video model", modelID: "grok-imagine-video-quality", hasAccounts: true, want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, isModelBenchmarkSupportedModel(testCase.modelID, testCase.hasAccounts))
		})
	}
}

type modelBenchmarkAPIKeyStoreStub struct {
	keys    map[int64]*APIKey
	missing bool
}

func (s modelBenchmarkAPIKeyStoreStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if key, ok := s.keys[id]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("api key %d not found", id)
}

func (s modelBenchmarkAPIKeyStoreStub) GetKeyAndOwnerID(_ context.Context, id int64) (string, int64, error) {
	key, ok := s.keys[id]
	if !ok {
		return "", 0, fmt.Errorf("api key %d not found", id)
	}
	return key.Key, key.UserID, nil
}

// modelBenchmarkSettingRepo 在只读 settingRepoStub 上支持 Set 回写（设置保存测试需要）。
type modelBenchmarkSettingRepo struct {
	settingRepoStub
}

func (s *modelBenchmarkSettingRepo) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

// modelBenchmarkBudgetRepoStub 记录 ApplyPlanBudgetDefaults 的回填调用（嵌入接口兜底其余方法）。
type modelBenchmarkBudgetRepoStub struct {
	ModelBenchmarkRepository
	calls           int
	sampleCount     int
	maxOutputTokens int
}

func (s *modelBenchmarkBudgetRepoStub) ApplyPlanBudgetDefaults(_ context.Context, sampleCount, maxOutputTokens int) error {
	s.calls++
	s.sampleCount = sampleCount
	s.maxOutputTokens = maxOutputTokens
	return nil
}

func (s *modelBenchmarkBudgetRepoStub) CreatePlan(_ context.Context, plan *ModelBenchmarkPlan) (*ModelBenchmarkPlan, error) {
	return plan, nil
}

func TestModelBenchmarkSettingsValidation(t *testing.T) {
	groupID := int64(7)
	store := modelBenchmarkAPIKeyStoreStub{keys: map[int64]*APIKey{
		1: {ID: 1, Key: "sk-ok", UserID: 2, GroupID: &groupID, Status: StatusActive},
		2: {ID: 2, Key: "sk-disabled", UserID: 2, GroupID: &groupID, Status: "disabled"},
		3: {ID: 3, Key: "sk-nogroup", UserID: 2, GroupID: nil, Status: StatusActive},
	}}
	settings := NewSettingService(&modelBenchmarkSettingRepo{settingRepoStub: settingRepoStub{values: map[string]string{}}}, nil)
	repo := &modelBenchmarkBudgetRepoStub{}
	service := NewModelBenchmarkService(repo, store, settings, nil, nil)

	// 默认未配置；预算两项兜底为默认值
	got, err := service.GetBenchmarkSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(0), got.APIKeyID)
	require.Equal(t, 1, got.SampleCount)
	require.Equal(t, 8192, got.MaxOutputTokens)

	// 非法 Key 被拒绝
	_, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{APIKeyID: 99})
	require.Error(t, err)
	_, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{APIKeyID: 2})
	require.Error(t, err)
	_, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{APIKeyID: 3})
	require.Error(t, err)

	// 预算超范围被拒绝
	_, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{SampleCount: 5})
	require.Error(t, err)
	_, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{MaxOutputTokens: 100})
	require.Error(t, err)
	_, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{MaxOutputTokens: 40000})
	require.Error(t, err)

	// 合法 Key 保存并可回读；<=0 的预算项兜底为默认值，并回填到所有计划
	got, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{APIKeyID: 1})
	require.NoError(t, err)
	require.Equal(t, int64(1), got.APIKeyID)
	require.Equal(t, 1, got.SampleCount)
	require.Equal(t, 8192, got.MaxOutputTokens)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, 1, repo.sampleCount)
	require.Equal(t, 8192, repo.maxOutputTokens)
	got, err = service.GetBenchmarkSettings(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(1), got.APIKeyID)
	require.Equal(t, 1, got.SampleCount)
	require.Equal(t, 8192, got.MaxOutputTokens)

	// 自定义预算保存并回填
	got, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{APIKeyID: 1, SampleCount: 3, MaxOutputTokens: 16384})
	require.NoError(t, err)
	require.Equal(t, 3, got.SampleCount)
	require.Equal(t, 16384, got.MaxOutputTokens)
	require.Equal(t, 2, repo.calls)
	require.Equal(t, 3, repo.sampleCount)
	require.Equal(t, 16384, repo.maxOutputTokens)

	// KeyID=0 允许清除
	got, err = service.UpdateBenchmarkSettings(context.Background(), &ModelBenchmarkSettings{APIKeyID: 0})
	require.NoError(t, err)
	require.Equal(t, int64(0), got.APIKeyID)
}

// 测评设置统一管理采样次数与单题输出预算：创建计划时以设置值为准，忽略请求传入值。
func TestModelBenchmarkCreatePlanAppliesBudgetDefaults(t *testing.T) {
	accounts := []Account{{ID: 1, Name: "openai-key", Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: "active", Schedulable: true, Credentials: map[string]any{"api_key": "sk-test", "model_mapping": map[string]any{"model-a": "model-a"}}}}
	settings := NewSettingService(&modelBenchmarkSettingRepo{settingRepoStub: settingRepoStub{values: map[string]string{}}}, nil)
	repo := &modelBenchmarkBudgetRepoStub{}
	service := NewModelBenchmarkService(repo, modelBenchmarkAPIKeyStoreStub{}, settings, NewModelManifestService(modelBenchmarkManifestAccountSource{accounts: accounts}), nil)
	ctx := context.Background()
	_, err := service.UpdateBenchmarkSettings(ctx, &ModelBenchmarkSettings{SampleCount: 2, MaxOutputTokens: 16384})
	require.NoError(t, err)

	plan, err := service.CreatePlan(ctx, &ModelBenchmarkPlan{Name: "p", ModelID: "model-a", CronExpression: "0 4 * * 1", SampleCount: 99, MaxOutputTokens: 99999})
	require.NoError(t, err)
	require.Equal(t, 2, plan.SampleCount)
	require.Equal(t, 16384, plan.MaxOutputTokens)

	// 请求不携带这两项时同样以设置值为准
	plan, err = service.CreatePlan(ctx, &ModelBenchmarkPlan{Name: "p2", ModelID: "model-a", CronExpression: "0 4 * * 1"})
	require.NoError(t, err)
	require.Equal(t, 2, plan.SampleCount)
	require.Equal(t, 16384, plan.MaxOutputTokens)
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

func TestModelBenchmarkAggregateExecutionFailureCountsAsMissed(t *testing.T) {
	run := &ModelBenchmarkRun{UsageFieldCoverage: map[string]bool{}, CategoryScores: map[string]float64{}}
	items := []ModelBenchmarkRunItem{
		{Status: "correct", Score: 1, EarnedPoints: 2, PossiblePoints: 2, ExpectedOutputTokens: 10, ExpectedLatencyMS: 100, OutputTokens: modelBenchmarkPtrInt64(10), LatencyMS: modelBenchmarkPtrInt64(100)},
		{Status: "execution_failed", Score: 0, EarnedPoints: 0, PossiblePoints: 3, ExpectedOutputTokens: 10, ExpectedLatencyMS: 100, UsageSource: "missing"},
	}
	AggregateModelBenchmarkRun(run, items)
	require.NotNil(t, run.IntelligenceScore)
	// 最终计分原则：执行失败即未作对，必须以 0 分进入分母（2/5 = 40）。
	require.Equal(t, 40.0, *run.IntelligenceScore)
	require.Equal(t, 1, run.AnsweredCount)
	require.Equal(t, "partial", run.Status)
	require.Nil(t, run.EfficiencyScore, "usage-incomplete run must not produce comparable efficiency")
	require.Nil(t, run.CompositeScore)
}

// modelBenchmarkLoopbackGateway 用 httptest 模拟回环网关：校验收到的载荷并把
// 预置 SSE 流回给 executor。
func modelBenchmarkLoopbackGateway(t *testing.T, sse string, inspect func(body []byte)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer sk-loop", r.Header.Get("Authorization"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		if inspect != nil {
			inspect(body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(sse))
	}))
	t.Cleanup(server.Close)
	return server
}

func modelBenchmarkLoopbackConfig(t *testing.T, server *httptest.Server) *config.Config {
	t.Helper()
	port, err := strconv.Atoi(strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	require.NoError(t, err)
	return &config.Config{Server: config.ServerConfig{Port: port}}
}

func modelBenchmarkLoopbackExecutor(t *testing.T, cfg *config.Config) *ModelBenchmarkExecutor {
	t.Helper()
	groupID := int64(1)
	store := modelBenchmarkAPIKeyStoreStub{keys: map[int64]*APIKey{
		11: {ID: 11, Key: "sk-loop", UserID: 2, GroupID: &groupID, Status: StatusActive},
	}}
	settings := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyModelBenchmarkSettings: `{"api_key_id":11}`,
	}}, nil)
	return NewModelBenchmarkExecutor(store, settings, nil, cfg)
}

func TestModelBenchmarkExecutorLoopbackCapturesUsage(t *testing.T) {
	sse := "data: {\"model\":\"gpt-test\",\"choices\":[{\"delta\":{\"content\":\"{\\\"answer\\\":\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"7}\"}}]}\n\n" +
		"data: {\"choices\":[{\"finish_reason\":\"stop\",\"delta\":{}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7,\"total_tokens\":18,\"completion_tokens_details\":{\"reasoning_tokens\":3}}}\n\n" +
		"data: [DONE]\n\n"
	var received map[string]any
	server := modelBenchmarkLoopbackGateway(t, sse, func(body []byte) {
		require.NoError(t, json.Unmarshal(body, &received))
	})
	executor := modelBenchmarkLoopbackExecutor(t, modelBenchmarkLoopbackConfig(t, server))
	item := ModelBenchmarkItem{ID: "mod", Category: "math", Difficulty: 4, Checker: "integer", Prompt: "return 7", Expected: int64(7), PromptHash: "hash", ExpectedOutputTokens: 20, ExpectedLatencyMS: 1000}
	plan := &ModelBenchmarkPlan{ModelID: "gpt-test", MaxOutputTokens: 128}
	result := executor.Execute(context.Background(), plan, item)
	require.Equal(t, "correct", result.Status)
	require.Equal(t, int64(11), *result.InputTokens)
	require.Equal(t, int64(7), *result.OutputTokens)
	require.Equal(t, int64(18), *result.TotalTokens)
	require.Equal(t, int64(3), *result.ReasoningTokens)
	require.Equal(t, "upstream", result.UsageSource)
	require.True(t, result.UsageFieldCoverage["reasoning"])
	require.NotNil(t, result.LatencyMS)
	// 回环载荷语义：真实题目、temperature=0、显式输出预算与 usage 声明
	require.Equal(t, "gpt-test", received["model"])
	require.Equal(t, float64(0), received["temperature"])
	require.Equal(t, float64(128), received["max_tokens"])
	require.Equal(t, true, received["stream"])
	require.Equal(t, map[string]any{"include_usage": true}, received["stream_options"])
	messages := received["messages"].([]any)
	require.Len(t, messages, 1)
	require.Equal(t, "return 7", messages[0].(map[string]any)["content"])
}

func TestModelBenchmarkExecutorRequiresConfiguredAPIKey(t *testing.T) {
	server := modelBenchmarkLoopbackGateway(t, "data: [DONE]\n\n", nil)
	cfg := modelBenchmarkLoopbackConfig(t, server)
	store := modelBenchmarkAPIKeyStoreStub{keys: map[int64]*APIKey{}}
	executor := NewModelBenchmarkExecutor(store, NewSettingService(&settingRepoStub{values: map[string]string{}}, nil), nil, cfg)
	item := ModelBenchmarkItem{ID: "mod", Category: "math", Difficulty: 4, Checker: "integer", Prompt: "return 7", Expected: int64(7), PromptHash: "hash", ExpectedOutputTokens: 20, ExpectedLatencyMS: 1000}
	result := executor.Execute(context.Background(), &ModelBenchmarkPlan{ModelID: "gpt-test", MaxOutputTokens: 128}, item)
	require.Equal(t, "execution_failed", result.Status)
	require.Contains(t, result.ErrorMessage, "not configured")
}

func TestModelBenchmarkExecutorReportsGatewayError(t *testing.T) {
	executor := NewModelBenchmarkExecutor(nil, nil, nil, &config.Config{Server: config.ServerConfig{Port: 1}})
	item := ModelBenchmarkItem{ID: "mod", Category: "math", Difficulty: 4, Checker: "integer", Prompt: "return 7", Expected: int64(7), PromptHash: "hash", ExpectedOutputTokens: 20, ExpectedLatencyMS: 1000}
	result := executor.Execute(context.Background(), &ModelBenchmarkPlan{ModelID: "gpt-test", MaxOutputTokens: 128}, item)
	require.Equal(t, "execution_failed", result.Status)
	require.NotEmpty(t, result.ErrorMessage)
}

func TestModelBenchmarkExecutorMarksBudgetExhausted(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"answer\\\":1}\"},\"finish_reason\":\"length\"}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":128,\"total_tokens\":133}}\n\n" +
		"data: [DONE]\n\n"
	server := modelBenchmarkLoopbackGateway(t, sse, nil)
	executor := modelBenchmarkLoopbackExecutor(t, modelBenchmarkLoopbackConfig(t, server))
	item := ModelBenchmarkItem{ID: "mod", Category: "math", Difficulty: 4, Checker: "integer", Prompt: "return 7", Expected: int64(7), PromptHash: "hash", ExpectedOutputTokens: 20, ExpectedLatencyMS: 1000}
	result := executor.Execute(context.Background(), &ModelBenchmarkPlan{ModelID: "gpt-test", MaxOutputTokens: 128}, item)
	require.Equal(t, "execution_failed", result.Status)
	require.Equal(t, "budget_exhausted", result.FailureReason)
	require.Equal(t, "{\"answer\":1}", result.ResponseText)
	require.Equal(t, int64(128), *result.OutputTokens)
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
