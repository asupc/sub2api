package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ModelBenchmarkPlan is a persistent scheduled benchmark target.
type ModelBenchmarkPlan struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	SuiteID         string     `json:"suite_id"`
	SuiteVersion    string     `json:"suite_version"`
	ScoringVersion  string     `json:"scoring_version"`
	ModelID         string     `json:"model_id"`
	AccountID       int64      `json:"account_id"`
	CronExpression  string     `json:"cron_expression"`
	Enabled         bool       `json:"enabled"`
	SampleCount     int        `json:"sample_count"`
	MaxOutputTokens int        `json:"max_output_tokens"`
	TimeoutSeconds  int        `json:"timeout_seconds"`
	MaxResults      int        `json:"max_results"`
	RunningStatus   string     `json:"running_status"`
	ClaimExpiresAt  *time.Time `json:"claim_expires_at"`
	LastRunAt       *time.Time `json:"last_run_at"`
	NextRunAt       *time.Time `json:"next_run_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// ModelBenchmarkRun aggregates one execution of a plan.
type ModelBenchmarkRun struct {
	ID                 int64              `json:"id"`
	PlanID             int64              `json:"plan_id"`
	SuiteID            string             `json:"suite_id"`
	SuiteVersion       string             `json:"suite_version"`
	ScoringVersion     string             `json:"scoring_version"`
	ModelID            string             `json:"model_id"`
	UpstreamModelID    string             `json:"upstream_model_id"`
	AccountID          int64              `json:"account_id"`
	Status             string             `json:"status"`
	IntelligenceScore  *float64           `json:"intelligence_score"`
	EfficiencyScore    *float64           `json:"efficiency_score"`
	CompositeScore     *float64           `json:"composite_score"`
	CategoryScores     map[string]float64 `json:"category_scores"`
	ItemCount          int                `json:"item_count"`
	AnsweredCount      int                `json:"answered_count"`
	CorrectCount       int                `json:"correct_count"`
	InputTokens        *int64             `json:"input_tokens"`
	OutputTokens       *int64             `json:"output_tokens"`
	TotalTokens        *int64             `json:"total_tokens"`
	EstimatedCost      *float64           `json:"estimated_cost"`
	AvgLatencyMS       *int64             `json:"avg_latency_ms"`
	P95LatencyMS       *int64             `json:"p95_latency_ms"`
	UsageSource        string             `json:"usage_source"`
	UsageFieldCoverage map[string]bool    `json:"usage_field_coverage"`
	ErrorMessage       string             `json:"error_message"`
	StartedAt          time.Time          `json:"started_at"`
	FinishedAt         *time.Time         `json:"finished_at"`
	CreatedAt          time.Time          `json:"created_at"`
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
	RunID             int64     `json:"run_id"`
	ModelID           string    `json:"model_id"`
	AccountID         int64     `json:"account_id"`
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

// ModelBenchmarkRanking is the latest stable ranking row.
type ModelBenchmarkRanking struct {
	ModelID           string    `json:"model_id"`
	SuiteID           string    `json:"suite_id"`
	SuiteVersion      string    `json:"suite_version"`
	ScoringVersion    string    `json:"scoring_version"`
	RunCount          int       `json:"run_count"`
	IntelligenceScore *float64  `json:"intelligence_score"`
	EfficiencyScore   *float64  `json:"efficiency_score"`
	CompositeScore    *float64  `json:"composite_score"`
	OutputTokens      *int64    `json:"output_tokens"`
	AvgLatencyMS      *int64    `json:"avg_latency_ms"`
	EstimatedCost     *float64  `json:"estimated_cost"`
	LastRunAt         time.Time `json:"last_run_at"`
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
	PruneRuns(ctx context.Context, planID int64, keep int) error
}

// ModelBenchmarkRunFilters limits run queries for admin pages.
type ModelBenchmarkRunFilters struct {
	PlanID  *int64
	ModelID string
	Status  string
	Limit   int
	Offset  int
}

// ModelBenchmarkHistoryFilters is version-isolated time-series input.
type ModelBenchmarkHistoryFilters struct {
	SuiteID        string
	SuiteVersion   string
	ScoringVersion string
	ModelID        string
	AccountID      *int64
	From           time.Time
	To             time.Time
	Limit          int
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
	answer, err := extractModelBenchmarkAnswer(response)
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

func modelBenchmarkItemWeight(item ModelBenchmarkItem) float64 {
	difficulty := 1.0
	switch item.Difficulty {
	case 4:
		difficulty = 1.5
	case 5:
		difficulty = 2.2
	}
	return modelBenchmarkCategoryWeights[item.Category] * difficulty
}

func extractModelBenchmarkAnswer(response string) (any, error) {
	trimmed := strings.TrimSpace(response)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found")
	}
	var envelope struct {
		Answer json.RawMessage `json:"answer"`
	}
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &envelope); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if len(envelope.Answer) == 0 {
		return nil, fmt.Errorf("answer field is missing")
	}
	var answer any
	if err := json.Unmarshal(envelope.Answer, &answer); err != nil {
		return nil, fmt.Errorf("invalid answer field: %w", err)
	}
	return answer, nil
}

func scoreModelBenchmarkAnswer(item ModelBenchmarkItem, answer any) (float64, string) {
	switch item.Checker {
	case "integer":
		got, ok := modelBenchmarkJSONInt(answer)
		want, _ := modelBenchmarkJSONInt(item.Expected)
		if ok && got == want {
			return 1, ""
		}
		return 0, fmt.Sprintf("want integer %v, got %v", item.Expected, answer)
	case "number_tolerance":
		got, ok := answer.(float64)
		want, _ := item.Expected.(float64)
		if ok && math.Abs(got-want) <= 1e-6 {
			return 1, ""
		}
		return 0, fmt.Sprintf("want number %.6f, got %v", want, answer)
	case "set":
		return modelBenchmarkCompareSets(answer, item.Expected)
	case "sequence":
		return modelBenchmarkCompareSequence(answer, item.Expected)
	case "fields":
		return modelBenchmarkCompareFields(answer, item.Expected)
	case "schema_only", "exact":
		return modelBenchmarkCompareValues(answer, item.Expected)
	default:
		return 0, "unsupported checker"
	}
}

func modelBenchmarkJSONInt(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		if typed == math.Trunc(typed) && math.Abs(typed) < 1e18 {
			return int64(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	}
	return 0, false
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
	earned, total := 0.0, 0.0
	for key, target := range want {
		total++
		score, _ := modelBenchmarkCompareValues(got[key], target)
		earned += score
	}
	if total == 0 {
		return 0, "no expected fields"
	}
	return earned / total, ""
}

func modelBenchmarkCompareValues(answer, expected any) (float64, string) {
	switch want := expected.(type) {
	case map[string]any:
		return modelBenchmarkCompareFields(answer, want)
	case []any:
		return modelBenchmarkCompareSequence(answer, want)
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
		got, ok := answer.(float64)
		if ok && math.Abs(got-typed) <= 1e-6 {
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
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		raw, _ := json.Marshal(value)
		return string(raw)
	}
}

// AggregateModelBenchmarkRun calculates scores while keeping unavailable execution evidence out of correctness.
func AggregateModelBenchmarkRun(run *ModelBenchmarkRun, items []ModelBenchmarkRunItem) {
	totalWeight, earnedWeight, answered := 0.0, 0.0, 0
	categoryTotals, categoryEarned := map[string]float64{}, map[string]float64{}
	type observation struct {
		item    ModelBenchmarkRunItem
		latency int64
	}
	observations := make([]observation, 0, len(items))
	usageComplete := false
	for _, item := range items {
		if item.Status == "execution_failed" {
			continue
		}
		totalWeight += item.PossiblePoints
		categoryTotals[item.Category] += item.PossiblePoints
		if item.UsageSource == "upstream" {
			usageComplete = true
		} else {
			usageComplete = false
		}
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
	if answered*5 < len(items)*4 {
		usageComplete = false
	}
	run.ItemCount = len(items)
	run.AnsweredCount = answered
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
				latencySum += math.Min(float64(observed.item.ExpectedLatencyMS)/float64(observed.latency), 1)
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
