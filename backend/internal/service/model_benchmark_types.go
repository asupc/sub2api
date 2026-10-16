package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"sort"
	"strings"
	"time"
)

// ModelBenchmarkPlan is a persistent scheduled benchmark target.
type ModelBenchmarkPlan struct {
	ComparisonKey   string     `json:"comparison_key"`
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	SuiteID         string     `json:"suite_id"`
	SuiteVersion    string     `json:"suite_version"`
	ScoringVersion  string     `json:"scoring_version"`
	ModelID         string     `json:"model_id"`
	CronExpression  string     `json:"cron_expression"`
	Enabled         bool       `json:"enabled"`
	SampleCount     int        `json:"sample_count"`
	MaxOutputTokens int        `json:"max_output_tokens"`
	TimeoutSeconds  int        `json:"timeout_seconds"`
	MaxResults      int        `json:"max_results"`
	RunningStatus   string     `json:"running_status"`
	ClaimExpiresAt  *time.Time `json:"claim_expires_at"`
	// ManualTriggerAt 非空表示存在一次性手动排队请求（停用计划也可排队）；runner
	// 领取时清空。enabled 仍只表示是否按 cron 周期调度。
	ManualTriggerAt *time.Time `json:"manual_trigger_at"`
	LastRunAt       *time.Time `json:"last_run_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ModelBenchmarkRun aggregates one execution of a plan.
type ModelBenchmarkRun struct {
	ComparisonKey      string                `json:"comparison_key"`
	MaxOutputTokens    int                   `json:"max_output_tokens"`
	TimeoutSeconds     int                   `json:"timeout_seconds"`
	SampleCount        int                   `json:"sample_count"`
	Quality            ModelBenchmarkQuality `json:"quality"`
	ID                 int64                 `json:"id"`
	PlanID             int64                 `json:"plan_id"`
	SuiteID            string                `json:"suite_id"`
	SuiteVersion       string                `json:"suite_version"`
	ScoringVersion     string                `json:"scoring_version"`
	ModelID            string                `json:"model_id"`
	UpstreamModelID    string                `json:"upstream_model_id"`
	Status             string                `json:"status"`
	IntelligenceScore  *float64              `json:"intelligence_score"`
	EfficiencyScore    *float64              `json:"efficiency_score"`
	CompositeScore     *float64              `json:"composite_score"`
	CategoryScores     map[string]float64    `json:"category_scores"`
	ItemCount          int                   `json:"item_count"`
	AnsweredCount      int                   `json:"answered_count"`
	CorrectCount       int                   `json:"correct_count"`
	InputTokens        *int64                `json:"input_tokens"`
	OutputTokens       *int64                `json:"output_tokens"`
	TotalTokens        *int64                `json:"total_tokens"`
	EstimatedCost      *float64              `json:"estimated_cost"`
	AvgLatencyMS       *int64                `json:"avg_latency_ms"`
	P95LatencyMS       *int64                `json:"p95_latency_ms"`
	UsageSource        string                `json:"usage_source"`
	UsageFieldCoverage map[string]bool       `json:"usage_field_coverage"`
	ErrorMessage       string                `json:"error_message"`
	StartedAt          time.Time             `json:"started_at"`
	FinishedAt         *time.Time            `json:"finished_at"`
	CreatedAt          time.Time             `json:"created_at"`
}

// ModelBenchmarkRunItem is per-item response and scoring evidence.
type ModelBenchmarkRunItem struct {
	ID                   int64           `json:"id"`
	RunID                int64           `json:"run_id"`
	ItemID               string          `json:"item_id"`
	FamilyID             string          `json:"family_id"`
	Category             string          `json:"category"`
	Difficulty           int             `json:"difficulty"`
	Weight               float64         `json:"weight"`
	PromptHash           string          `json:"prompt_hash"`
	PromptSnapshot       string          `json:"prompt_snapshot"`
	ResponseText         string          `json:"response_text"`
	Status               string          `json:"status"`
	Score                float64         `json:"score"`
	EarnedPoints         float64         `json:"earned_points"`
	PossiblePoints       float64         `json:"possible_points"`
	CheckerType          string          `json:"checker_type"`
	FailureReason        string          `json:"failure_reason"`
	InputTokens          *int64          `json:"input_tokens"`
	OutputTokens         *int64          `json:"output_tokens"`
	ReasoningTokens      *int64          `json:"reasoning_tokens"`
	CacheReadTokens      *int64          `json:"cache_read_tokens"`
	CacheWriteTokens     *int64          `json:"cache_write_tokens"`
	TotalTokens          *int64          `json:"total_tokens"`
	UsageSource          string          `json:"usage_source"`
	UsageFieldCoverage   map[string]bool `json:"usage_field_coverage"`
	EstimatedCost        *float64        `json:"estimated_cost"`
	PricingSource        string          `json:"pricing_source"`
	LatencyMS            *int64          `json:"latency_ms"`
	ExpectedOutputTokens int             `json:"-"`
	ExpectedLatencyMS    int             `json:"-"`
	FirstTokenMS         *int64          `json:"first_token_ms"`
	ErrorMessage         string          `json:"error_message"`
	CreatedAt            time.Time       `json:"created_at"`
}

// ModelBenchmarkHistoryPoint is the compact time-series DTO.
type ModelBenchmarkHistoryPoint struct {
	ComparisonKey     string    `json:"comparison_key"`
	MaxOutputTokens   int       `json:"max_output_tokens"`
	TimeoutSeconds    int       `json:"timeout_seconds"`
	SampleCount       int       `json:"sample_count"`
	RunID             int64     `json:"run_id"`
	ModelID           string    `json:"model_id"`
	CreatedAt         time.Time `json:"created_at"`
	Status            string    `json:"status"`
	IntelligenceScore *float64  `json:"intelligence_score"`
	EfficiencyScore   *float64  `json:"efficiency_score"`
	CompositeScore    *float64  `json:"composite_score"`
	OutputTokens      *int64    `json:"output_tokens"`
	TotalTokens       *int64    `json:"total_tokens"`
	AvgLatencyMS      *int64    `json:"avg_latency_ms"`
	P95LatencyMS      *int64    `json:"p95_latency_ms"`
	EstimatedCost     *float64  `json:"estimated_cost"`
}

// ModelBenchmarkRanking 是某题库下每模型最新一次成功/部分成功 run 的排名行；
// ComparisonKey 与预算三元组是取自该次 run 的形态快照，仅作展示标注。
type ModelBenchmarkRanking struct {
	ComparisonKey     string             `json:"comparison_key"`
	MaxOutputTokens   int                `json:"max_output_tokens"`
	TimeoutSeconds    int                `json:"timeout_seconds"`
	SampleCount       int                `json:"sample_count"`
	ModelID           string             `json:"model_id"`
	SuiteID           string             `json:"suite_id"`
	SuiteVersion      string             `json:"suite_version"`
	ScoringVersion    string             `json:"scoring_version"`
	RunCount          int                `json:"run_count"`
	IntelligenceScore *float64           `json:"intelligence_score"`
	EfficiencyScore   *float64           `json:"efficiency_score"`
	CompositeScore    *float64           `json:"composite_score"`
	CategoryScores    map[string]float64 `json:"category_scores"`
	OutputTokens      *int64             `json:"output_tokens"`
	AvgLatencyMS      *int64             `json:"avg_latency_ms"`
	EstimatedCost     *float64           `json:"estimated_cost"`
	LastRunAt         time.Time          `json:"last_run_at"`
}

// ModelBenchmarkModelOverview 聚合返回单模型全景数据，支持抽屉下钻
type ModelBenchmarkModelOverview struct {
	ModelID    string                        `json:"model_id"`
	Ranking    *ModelBenchmarkRanking        `json:"ranking,omitempty"`
	LatestRun  *ModelBenchmarkRun            `json:"latest_run,omitempty"`
	Plans      []*ModelBenchmarkPlan         `json:"plans"`
	RecentRuns []*ModelBenchmarkRun          `json:"recent_runs"`
	History    []*ModelBenchmarkHistoryPoint `json:"history"`
}

// ModelBenchmarkRankingSuite 列出 runs 中存在可排名执行的题库版本组合，供前端切换历史题库。
type ModelBenchmarkRankingSuite struct {
	SuiteID        string    `json:"suite_id"`
	SuiteVersion   string    `json:"suite_version"`
	ScoringVersion string    `json:"scoring_version"`
	RunCount       int       `json:"run_count"`
	LastRunAt      time.Time `json:"last_run_at"`
}

// ModelBenchmarkLatestScore 是某模型最近一次成功/部分成功 run 的分数快照，
// 供模型清单页展示「最近测试分数」。不做套件版本隔离（前端按返回版本标注差异）。
type ModelBenchmarkLatestScore struct {
	ModelID           string    `json:"model_id"`
	IntelligenceScore float64   `json:"intelligence_score"`
	SuiteID           string    `json:"suite_id"`
	SuiteVersion      string    `json:"suite_version"`
	ScoringVersion    string    `json:"scoring_version"`
	ScoredAt          time.Time `json:"scored_at"`
}

// ModelBenchmarkRepository is the persistence port for benchmark plans and evidence.
type ModelBenchmarkRepository interface {
	CreatePlan(ctx context.Context, plan *ModelBenchmarkPlan) (*ModelBenchmarkPlan, error)
	GetPlan(ctx context.Context, id int64) (*ModelBenchmarkPlan, error)
	ListPlans(ctx context.Context) ([]*ModelBenchmarkPlan, error)
	UpdatePlan(ctx context.Context, plan *ModelBenchmarkPlan) (*ModelBenchmarkPlan, error)
	DeletePlan(ctx context.Context, id int64) error
	ClaimDuePlans(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*ModelBenchmarkPlan, error)
	CompletePlan(ctx context.Context, id int64, lastRunAt, nextRunAt time.Time) error
	ReleaseExpiredClaims(ctx context.Context, now time.Time) (int64, error)
	CreateRun(ctx context.Context, run *ModelBenchmarkRun) (*ModelBenchmarkRun, error)
	UpdateRun(ctx context.Context, run *ModelBenchmarkRun) error
	CreateItem(ctx context.Context, item *ModelBenchmarkRunItem) (*ModelBenchmarkRunItem, error)
	ListRuns(ctx context.Context, filters ModelBenchmarkRunFilters) ([]*ModelBenchmarkRun, error)
	GetRun(ctx context.Context, id int64) (*ModelBenchmarkRun, error)
	ListItems(ctx context.Context, runID int64) ([]*ModelBenchmarkRunItem, error)
	ListHistory(ctx context.Context, filters ModelBenchmarkHistoryFilters) ([]*ModelBenchmarkHistoryPoint, error)
	ListRankings(ctx context.Context, suiteID, suiteVersion, scoringVersion string, limit int) ([]*ModelBenchmarkRanking, error)
	ListRankingSuites(ctx context.Context) ([]*ModelBenchmarkRankingSuite, error)
	LatestScores(ctx context.Context) (map[string]ModelBenchmarkLatestScore, error)
	PruneRuns(ctx context.Context, planID int64, keep int) error
}

// ModelBenchmarkRunFilters limits run queries for admin pages.
type ModelBenchmarkRunFilters struct {
	MaxOutputTokens int
	TimeoutSeconds  int
	SampleCount     int
	ComparisonKey   string
	PlanID          *int64
	ModelID         string
	Status          string
	Limit           int
	Offset          int
}

// ModelBenchmarkHistoryFilters is version-isolated time-series input.
type ModelBenchmarkHistoryFilters struct {
	MaxOutputTokens int
	TimeoutSeconds  int
	SampleCount     int
	ComparisonKey   string
	SuiteID         string
	SuiteVersion    string
	ScoringVersion  string
	ModelID         string
	From            time.Time
	To              time.Time
	Limit           int
}

// ModelBenchmarkRankingFilters 只按题库三元组过滤排名；空字段由 service 兜底为编译期常量。
type ModelBenchmarkRankingFilters struct {
	SuiteID        string
	SuiteVersion   string
	ScoringVersion string
}

var (
	ErrModelBenchmarkUnsupportedTarget = fmt.Errorf("model benchmark unsupported target")
	ErrModelBenchmarkInvalidPlan       = fmt.Errorf("model benchmark invalid plan")
)

// ModelBenchmarkScoreAnswer parses and scores one model response.
func ModelBenchmarkScoreAnswer(item ModelBenchmarkItem, response string) ModelBenchmarkRunItem {
	result := ModelBenchmarkRunItem{
		ItemID: item.ID, FamilyID: item.FamilyID, Category: item.Category,
		Difficulty: item.Difficulty, Weight: modelBenchmarkItemWeight(item),
		PromptHash: item.PromptHash, PromptSnapshot: item.Prompt, ResponseText: response,
		PossiblePoints: modelBenchmarkItemWeight(item), CheckerType: item.Checker,
		ExpectedOutputTokens: item.ExpectedOutputTokens, ExpectedLatencyMS: item.ExpectedLatencyMS,
	}
	var answer any
	var err error
	if item.Checker == "schema_only" {
		answer, err = extractModelBenchmarkStrictAnswer(response)
	} else {
		answer, err = extractModelBenchmarkAnswer(response)
	}
	if err != nil {
		result.Status = "format_error"
		result.FailureReason = err.Error()
		return result
	}
	score, reason := scoreModelBenchmarkAnswer(item, answer)
	result.Score = score
	result.EarnedPoints = score * result.PossiblePoints
	if score >= 1 {
		result.Status = "correct"
	} else if score > 0 {
		result.Status = "partial"
	} else {
		result.Status = "incorrect"
	}
	if reason != "" {
		result.FailureReason = reason
	}
	return result
}

func modelBenchmarkDifficultyWeight(difficulty int) float64 {
	switch difficulty {
	case 4:
		return 1.5
	case 5:
		return 2.2
	}
	return 1
}
func modelBenchmarkItemWeight(item ModelBenchmarkItem) float64 {
	total := 0.0
	for _, family := range modelBenchmarkFamilies {
		if family.category == item.Category {
			total += 1 * modelBenchmarkDifficultyWeight(family.difficulty)
		}
	}
	if total == 0 {
		return 0
	}
	return modelBenchmarkCategoryWeights[item.Category] * modelBenchmarkDifficultyWeight(item.Difficulty) / total
}

func extractModelBenchmarkAnswer(response string) (any, error) {
	// Decode each candidate object independently. Pick the one ending last;
	// an enclosing object wins over its nested objects when their end is tied.
	var envelope map[string]json.RawMessage
	lastEnd := -1
	for start := 0; start < len(response); start++ {
		if response[start] != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(response[start:]))
		var candidate map[string]json.RawMessage
		if err := decoder.Decode(&candidate); err != nil {
			continue
		}
		if end := start + int(decoder.InputOffset()); end > lastEnd {
			envelope, lastEnd = candidate, end
		}
	}
	if lastEnd < 0 {
		return nil, fmt.Errorf("no valid JSON object found")
	}
	raw, ok := envelope["answer"]
	if !ok {
		return nil, fmt.Errorf("answer field is missing")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var answer any
	if err := decoder.Decode(&answer); err != nil {
		return nil, fmt.Errorf("invalid answer field: %w", err)
	}
	return answer, nil
}

func extractModelBenchmarkStrictAnswer(response string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(response)))
	decoder.UseNumber()
	value, err := readModelBenchmarkStrictJSONValue(decoder)
	if err != nil {
		return nil, fmt.Errorf("invalid strict JSON: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("strict response must contain exactly one JSON object")
	}
	envelope, ok := value.(map[string]any)
	if !ok || len(envelope) != 1 {
		return nil, fmt.Errorf("strict response must contain only the answer field")
	}
	answer, ok := envelope["answer"]
	if !ok {
		return nil, fmt.Errorf("answer field is missing")
	}
	return answer, nil
}

// Token decoding retains numeric precision and rejects duplicate object keys,
// which ordinary json.Unmarshal would silently overwrite.
func readModelBenchmarkStrictJSONValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("object key must be a string")
			}
			if _, exists := object[name]; exists {
				return nil, fmt.Errorf("duplicate key %q", name)
			}
			value, err := readModelBenchmarkStrictJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case json.Delim('['):
		array := []any{}
		for decoder.More() {
			value, err := readModelBenchmarkStrictJSONValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return array, nil
	default:
		return token, nil
	}
}

func scoreModelBenchmarkAnswer(item ModelBenchmarkItem, answer any) (float64, string) {
	switch item.Checker {
	case "witness":
		return scoreModelBenchmarkWitness(item, answer)
	case "integer":
		got, ok := modelBenchmarkJSONInt(answer)
		want, valid := modelBenchmarkJSONInt(item.Expected)
		if ok && valid && got == want {
			return 1, ""
		}
		return 0, fmt.Sprintf("want integer %v, got %v", item.Expected, answer)
	case "number_tolerance":
		got, ok := modelBenchmarkJSONNumber(answer)
		want, valid := modelBenchmarkJSONNumber(item.Expected)
		if ok && valid && math.Abs(got-want) <= 1e-6 {
			return 1, ""
		}
		return 0, fmt.Sprintf("want number %.6f, got %v", want, answer)
	case "set":
		return modelBenchmarkCompareSets(answer, item.Expected)
	case "sequence":
		return modelBenchmarkCompareSequence(answer, item.Expected)
	case "fields":
		return modelBenchmarkCompareFields(answer, item.Expected)
	case "schema_only":
		return modelBenchmarkCompareSchema(answer, item.Expected)
	case "exact":
		return modelBenchmarkCompareValues(answer, item.Expected)
	default:
		return 0, "unsupported checker"
	}
}

func modelBenchmarkJSONInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) && typed >= -0x1p63 && typed < 0x1p63 {
			return int64(typed), true
		}
	case json.Number:
		// Accept equivalent JSON numbers such as 42.0 and 4.2e1 without
		// rounding large integers through float64.
		if parsed, err := typed.Int64(); err == nil {
			return parsed, true
		}
		if parsed, ok := new(big.Rat).SetString(string(typed)); ok && parsed.IsInt() && parsed.Num().IsInt64() {
			return parsed.Num().Int64(), true
		}
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	}
	return 0, false
}

func modelBenchmarkJSONNumber(value any) (float64, bool) {
	var number float64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case float64:
		number = typed
	case int:
		number = float64(typed)
	case int64:
		number = float64(typed)
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func modelBenchmarkNormalizeList(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []string:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = value
		}
		return out, true
	case []int:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = value
		}
		return out, true
	default:
		return nil, false
	}
}

func modelBenchmarkCompareSequence(answer, expected any) (float64, string) {
	got, ok1 := modelBenchmarkNormalizeList(answer)
	want, ok2 := modelBenchmarkNormalizeList(expected)
	if !ok1 || !ok2 {
		return 0, "answer must be an array"
	}
	if len(got) != len(want) {
		return 0, "sequence length mismatch"
	}
	for i := range got {
		score, _ := modelBenchmarkCompareValues(got[i], want[i])
		if score < 1 {
			return 0, fmt.Sprintf("sequence differs at index %d", i)
		}
	}
	return 1, ""
}

func modelBenchmarkCompareSets(answer, expected any) (float64, string) {
	got, ok1 := modelBenchmarkNormalizeList(answer)
	want, ok2 := modelBenchmarkNormalizeList(expected)
	if !ok1 || !ok2 {
		return 0, "answer must be an array"
	}
	if len(got) != len(want) {
		return 0, "set length mismatch"
	}
	matched := make([]bool, len(want))
	for _, candidate := range got {
		found := false
		for i, target := range want {
			if !matched[i] {
				if score, _ := modelBenchmarkCompareValues(candidate, target); score >= 1 {
					matched[i] = true
					found = true
					break
				}
			}
		}
		if !found {
			return 0, fmt.Sprintf("unexpected set value %v", candidate)
		}
	}
	return 1, ""
}

func modelBenchmarkCompareFields(answer, expected any) (float64, string) {
	got, ok1 := answer.(map[string]any)
	want, ok2 := expected.(map[string]any)
	if !ok1 || !ok2 {
		return 0, "answer must be an object"
	}
	if len(want) == 0 {
		return 0, "no expected fields"
	}
	earned := 0.0
	failures := []string{}
	for key, target := range want {
		score, reason := modelBenchmarkCompareValues(got[key], target)
		earned += score
		if score < 1 {
			failures = append(failures, fmt.Sprintf("field %s: %s", key, reason))
		}
	}
	sort.Strings(failures)
	return earned / float64(len(want)), strings.Join(failures, "; ")
}

// Schema and exact object checks are all-or-nothing; only the fields checker
// explicitly awards partial credit. Nested arrays compare values, not JSON text.
func modelBenchmarkCompareSchema(answer, expected any) (float64, string) {
	if want, ok := expected.(map[string]any); ok {
		got, ok := answer.(map[string]any)
		if !ok || len(got) != len(want) {
			return 0, "object fields mismatch"
		}
		keys := make([]string, 0, len(want))
		for key := range want {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, exists := got[key]
			if !exists {
				return 0, fmt.Sprintf("missing field %s", key)
			}
			if score, reason := modelBenchmarkCompareSchema(value, want[key]); score < 1 {
				return 0, fmt.Sprintf("field %s: %s", key, reason)
			}
		}
		return 1, ""
	}
	if want, ok := modelBenchmarkNormalizeList(expected); ok {
		got, ok := modelBenchmarkNormalizeList(answer)
		if !ok || len(got) != len(want) {
			return 0, "array length mismatch"
		}
		for i := range want {
			if score, reason := modelBenchmarkCompareSchema(got[i], want[i]); score < 1 {
				return 0, fmt.Sprintf("array index %d: %s", i, reason)
			}
		}
		return 1, ""
	}
	if want, ok := expected.(string); ok {
		if got, ok := answer.(string); ok && got == want {
			return 1, ""
		}
		return 0, fmt.Sprintf("want string %q, got %v", want, answer)
	}
	return modelBenchmarkCompareValues(answer, expected)
}

func modelBenchmarkCompareValues(answer, expected any) (float64, string) {
	switch expected.(type) {
	case map[string]any:
		return modelBenchmarkCompareSchema(answer, expected)
	case []any, []int, []string:
		return modelBenchmarkCompareSequence(answer, expected)
	}
	switch typed := expected.(type) {
	case int64:
		got, ok := modelBenchmarkJSONInt(answer)
		if ok && got == typed {
			return 1, ""
		}
	case int:
		got, ok := modelBenchmarkJSONInt(answer)
		if ok && got == int64(typed) {
			return 1, ""
		}
	case bool:
		got, ok := answer.(bool)
		if ok && got == typed {
			return 1, ""
		}
	case float64:
		got, ok := modelBenchmarkJSONNumber(answer)
		if ok && math.Abs(got-typed) <= 1e-6 {
			return 1, ""
		}
	case string:
		got, ok := answer.(string)
		if ok && strings.TrimSpace(got) == strings.TrimSpace(typed) {
			return 1, ""
		}
	default:
		if modelBenchmarkDisplay(answer) == modelBenchmarkDisplay(expected) {
			return 1, ""
		}
	}
	return 0, fmt.Sprintf("want %v, got %v", expected, answer)
}

func modelBenchmarkDisplay(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

// AggregateModelBenchmarkRun calculates scores while keeping unavailable execution evidence out of correctness.
func AggregateModelBenchmarkRun(run *ModelBenchmarkRun, items []ModelBenchmarkRunItem) {
	planned := run.ItemCount
	if planned < len(items) {
		planned = len(items)
	}
	run.Quality = ModelBenchmarkQuality{CategoryCoverage: map[string]float64{}}
	run.IntelligenceScore, run.EfficiencyScore, run.CompositeScore = nil, nil, nil
	run.CorrectCount = 0
	expectedCategory, completedCategory := map[string]int{}, map[string]int{}
	samples := max(run.SampleCount, 1)
	for _, family := range modelBenchmarkFamilies {
		expectedCategory[family.category] += 1 * samples
	}

	totalWeight, earnedWeight, answered := 0.0, 0.0, 0
	categoryTotals, categoryEarned := map[string]float64{}, map[string]float64{}
	type observation struct {
		item    ModelBenchmarkRunItem
		latency int64
	}
	observations := make([]observation, 0, len(items))
	usageComplete := len(items) > 0
	for _, item := range items {
		if item.Status == "format_error" {
			run.Quality.FormatErrorCount++
		}
		if item.FailureReason == "budget_exhausted" {
			run.Quality.BudgetExhaustedCount++
		}
		if item.Status != "execution_failed" && item.Status != "format_error" {
			completedCategory[item.Category]++
		}
		if item.Status == "execution_failed" {
			continue
		}
		totalWeight += item.PossiblePoints
		categoryTotals[item.Category] += item.PossiblePoints
		usageComplete = usageComplete && item.UsageSource == "upstream"
		answered++
		earnedWeight += item.EarnedPoints
		categoryEarned[item.Category] += item.EarnedPoints
		if item.Score >= 1 {
			run.CorrectCount++
		}
		if item.LatencyMS != nil {
			observations = append(observations, observation{item: item, latency: *item.LatencyMS})
		}
	}
	if answered*5 < planned*4 {
		usageComplete = false
	}
	run.ItemCount = planned
	run.AnsweredCount = answered
	valid := answered - run.Quality.FormatErrorCount
	if planned > 0 {
		run.Quality.CompletionRate = 100 * float64(valid) / float64(planned)
		run.Quality.FormatComplianceRate = 100 * float64(valid) / float64(max(answered, 1))
	}
	run.Quality.RankingEligible = planned == len(modelBenchmarkFamilies)*1*samples && run.Quality.CompletionRate >= 90
	for category, expected := range expectedCategory {
		coverage := 100 * float64(completedCategory[category]) / float64(expected)
		run.Quality.CategoryCoverage[category] = coverage
		if coverage < 80 {
			run.Quality.RankingEligible = false
		}
	}

	if !run.Quality.RankingEligible {
		run.Quality.RankingReason = "insufficient_coverage"
	}
	if run.SuiteVersion == ModelBenchmarkSuiteVersion && run.MaxOutputTokens <= 0 {
		run.Quality.RankingEligible = false
		run.Quality.RankingReason = "provider_managed_budget"
	}
	if totalWeight > 0 && answered > 0 {
		score := math.Round(earnedWeight/totalWeight*1000) / 10
		run.IntelligenceScore = &score
		run.CategoryScores = map[string]float64{}
		for category, total := range categoryTotals {
			if total > 0 {
				run.CategoryScores[category] = math.Round(categoryEarned[category]/total*1000) / 10
			}
		}
	}
	if len(observations) > 0 {
		sortedLatencies := make([]int64, 0, len(observations))
		for _, observed := range observations {
			sortedLatencies = append(sortedLatencies, observed.latency)
		}
		sort.Slice(sortedLatencies, func(i, j int) bool { return sortedLatencies[i] < sortedLatencies[j] })
		var totalLatency int64
		for _, latency := range sortedLatencies {
			totalLatency += latency
		}
		avg := totalLatency / int64(len(sortedLatencies))
		run.AvgLatencyMS = &avg
		p95 := sortedLatencies[(len(sortedLatencies)*95-1)/100]
		run.P95LatencyMS = &p95

		tokenSum, latencySum := 0.0, 0.0
		for _, observed := range observations {
			if observed.item.OutputTokens != nil && *observed.item.OutputTokens > 0 && observed.item.ExpectedOutputTokens > 0 {
				tokenSum += math.Min(float64(observed.item.ExpectedOutputTokens)/float64(*observed.item.OutputTokens), 1)
			}
			if observed.item.ExpectedLatencyMS > 0 {
				latencySum += math.Min(float64(observed.item.ExpectedLatencyMS)/math.Max(float64(observed.latency), 1), 1)
			}
		}
		if usageComplete {
			tokenEfficiency := tokenSum / float64(len(observations))
			latencyEfficiency := latencySum / float64(len(observations))
			efficiency := tokenEfficiency*70 + latencyEfficiency*30
			run.EfficiencyScore = &efficiency
			if run.IntelligenceScore != nil {
				composite := *run.IntelligenceScore*.85 + tokenEfficiency*10 + latencyEfficiency*5
				run.CompositeScore = &composite
			}
		}
	}
	if run.AnsweredCount == 0 {
		run.Status = "failed"
	} else if run.AnsweredCount < run.ItemCount {
		run.Status = "partial"
	} else {
		run.Status = "success"
	}
}

// ModelBenchmarkQuality keeps measurement coverage separate from correctness.
type ModelBenchmarkQuality struct {
	RankingReason        string             `json:"ranking_reason"`
	CompletionRate       float64            `json:"completion_rate"`
	FormatComplianceRate float64            `json:"format_compliance_rate"`
	FormatErrorCount     int                `json:"format_error_count"`
	BudgetExhaustedCount int                `json:"budget_exhausted_count"`
	RankingEligible      bool               `json:"ranking_eligible"`
	CategoryCoverage     map[string]float64 `json:"category_coverage"`
}
