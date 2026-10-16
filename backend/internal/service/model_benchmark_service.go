package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ModelBenchmarkService manages plans and executes model benchmark runs.
type ModelBenchmarkService struct {
	repo            ModelBenchmarkRepository
	accounts        ModelBenchmarkAccountSource
	manifestService *ModelManifestService
	executor        *ModelBenchmarkExecutor
}

// NewModelBenchmarkService creates the benchmark application service.
func NewModelBenchmarkService(repo ModelBenchmarkRepository, accounts ModelBenchmarkAccountSource, manifestService *ModelManifestService, executor *ModelBenchmarkExecutor) *ModelBenchmarkService {
	return &ModelBenchmarkService{repo: repo, accounts: accounts, manifestService: manifestService, executor: executor}
}

// ListSuites returns the only built-in suite in Phase 1.
func (s *ModelBenchmarkService) ListSuites() []ModelBenchmarkSuite {
	return []ModelBenchmarkSuite{GetModelBenchmarkSuite()}
}

// ListTargets returns the model-manifest view filtered to benchmark-supported accounts.
func (s *ModelBenchmarkService) ListTargets(ctx context.Context) ([]ModelManifestItem, error) {
	if s.manifestService == nil {
		return nil, fmt.Errorf("model manifest service is unavailable")
	}
	manifest, err := s.manifestService.GetManifest(ctx, ModelManifestQuery{})
	if err != nil {
		return nil, err
	}
	out := make([]ModelManifestItem, 0, len(manifest.Models))
	for _, model := range manifest.Models {
		accounts := make([]ModelManifestAccountItem, 0, len(model.Accounts))
		for _, account := range model.Accounts {
			loaded, err := s.accounts.GetByID(ctx, account.ID)
			if err == nil && loaded.Status == "active" && loaded.IsSchedulable() && IsSupportedModelBenchmarkTarget(loaded, model.ID) {
				accounts = append(accounts, account)
			}
		}
		if len(accounts) == 0 {
			continue
		}
		item := model
		item.Accounts = accounts
		item.TotalAccounts = len(accounts)
		item.ActiveAccounts = len(accounts)
		item.TotalConcurrency = 0
		out = append(out, item)
	}
	return out, nil
}

// CreatePlan validates the Phase 1 target and persists a disabled-by-default plan.
func (s *ModelBenchmarkService) CreatePlan(ctx context.Context, input *ModelBenchmarkPlan) (*ModelBenchmarkPlan, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.ModelID = strings.TrimSpace(input.ModelID)
	input.CronExpression = strings.TrimSpace(input.CronExpression)
	input.SuiteID = ModelBenchmarkSuiteID
	input.SuiteVersion = ModelBenchmarkSuiteVersion
	input.ScoringVersion = ModelBenchmarkScoringVer
	if input.ComparisonKey == "" {
		input.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	if input.Name == "" || input.ModelID == "" || input.CronExpression == "" {
		return nil, fmt.Errorf("%w: name, model_id and cron_expression are required", ErrModelBenchmarkInvalidPlan)
	}
	if _, err := computeNextRun(input.CronExpression, time.Now()); err != nil {
		return nil, fmt.Errorf("%w: invalid cron expression: %w", ErrModelBenchmarkInvalidPlan, err)
	}
	account, err := s.accounts.GetByID(ctx, input.AccountID)
	if err != nil {
		return nil, fmt.Errorf("%w: account not found", ErrModelBenchmarkInvalidPlan)
	}
	if !IsSupportedModelBenchmarkTarget(account, input.ModelID) {
		return nil, ErrModelBenchmarkUnsupportedTarget
	}
	if input.SampleCount <= 0 {
		input.SampleCount = 1
	}
	if input.MaxOutputTokens <= 0 {
		input.MaxOutputTokens = 2048
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 90
	}
	if input.MaxResults <= 0 {
		input.MaxResults = 50
	}
	if err := validateModelBenchmarkPlanLimits(input); err != nil {
		return nil, err
	}
	if input.Enabled {
		next := time.Now().Add(time.Minute)
		input.NextRunAt = &next
	}
	return s.repo.CreatePlan(ctx, input)
}

// UpdatePlan validates and updates an idle plan.
func (s *ModelBenchmarkService) UpdatePlan(ctx context.Context, id int64, input *ModelBenchmarkPlan) (*ModelBenchmarkPlan, error) {
	existing, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		existing.Name = strings.TrimSpace(input.Name)
	}
	if input.ModelID != "" {
		existing.ModelID = strings.TrimSpace(input.ModelID)
	}
	if input.AccountID != 0 {
		existing.AccountID = input.AccountID
	}
	if input.CronExpression != "" {
		existing.CronExpression = strings.TrimSpace(input.CronExpression)
	}
	if input.Enabled {
		existing.Enabled = true
	} else if input.Enabled == false {
		existing.Enabled = false
	}
	if input.ComparisonKey != "" {
		existing.ComparisonKey = input.ComparisonKey
	}
	if input.SampleCount > 0 {
		existing.SampleCount = input.SampleCount
	}
	if input.MaxOutputTokens > 0 {
		existing.MaxOutputTokens = input.MaxOutputTokens
	}
	if input.TimeoutSeconds > 0 {
		existing.TimeoutSeconds = input.TimeoutSeconds
	}
	if input.MaxResults > 0 {
		existing.MaxResults = input.MaxResults
	}
	if _, err := computeNextRun(existing.CronExpression, time.Now()); err != nil {
		return nil, fmt.Errorf("%w: invalid cron expression: %w", ErrModelBenchmarkInvalidPlan, err)
	}
	if err := validateModelBenchmarkPlanLimits(existing); err != nil {
		return nil, err
	}
	account, err := s.accounts.GetByID(ctx, existing.AccountID)
	if err != nil {
		return nil, fmt.Errorf("%w: account not found", ErrModelBenchmarkInvalidPlan)
	}
	if !IsSupportedModelBenchmarkTarget(account, existing.ModelID) {
		return nil, ErrModelBenchmarkUnsupportedTarget
	}
	if existing.Enabled {
		next := time.Now().Add(time.Minute)
		existing.NextRunAt = &next
	}
	return s.repo.UpdatePlan(ctx, existing)
}

// ListPlans returns all plans.
func (s *ModelBenchmarkService) ListPlans(ctx context.Context) ([]*ModelBenchmarkPlan, error) {
	return s.repo.ListPlans(ctx)
}

// GetPlan returns one plan.
func (s *ModelBenchmarkService) GetPlan(ctx context.Context, id int64) (*ModelBenchmarkPlan, error) {
	return s.repo.GetPlan(ctx, id)
}

// DeletePlan removes a plan and its evidence.
func (s *ModelBenchmarkService) DeletePlan(ctx context.Context, id int64) error {
	return s.repo.DeletePlan(ctx, id)
}

// QueueRun 手动排队一次执行。停用计划同样可排队（跑完仍是停用，不启动 cron）；
// runner 通过 manual_trigger_at 原子领取并清空标记。计划运行中时 UpdatePlan 的
// running_status='idle' 条件会命中 0 行，由 handler 映射为 409。
func (s *ModelBenchmarkService) QueueRun(ctx context.Context, id int64) (*ModelBenchmarkPlan, error) {
	plan, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	plan.ManualTriggerAt = &now
	if plan.Enabled {
		// 启用计划同时对齐 next_run_at，保持既有「启用即尽快执行」语义。
		plan.NextRunAt = &now
	}
	return s.repo.UpdatePlan(ctx, plan)
}

// ListRuns delegates to repository filtering.
func (s *ModelBenchmarkService) ListRuns(ctx context.Context, filters ModelBenchmarkRunFilters) ([]*ModelBenchmarkRun, error) {
	return s.repo.ListRuns(ctx, filters)
}

// GetRun returns run detail.
func (s *ModelBenchmarkService) GetRun(ctx context.Context, id int64) (*ModelBenchmarkRun, error) {
	return s.repo.GetRun(ctx, id)
}

// ListRunItems returns per-item evidence.
func (s *ModelBenchmarkService) ListRunItems(ctx context.Context, runID int64) ([]*ModelBenchmarkRunItem, error) {
	return s.repo.ListItems(ctx, runID)
}

// ListHistory returns a version-isolated curve.
func (s *ModelBenchmarkService) ListHistory(ctx context.Context, filters ModelBenchmarkHistoryFilters) ([]*ModelBenchmarkHistoryPoint, error) {
	if filters.SuiteID == "" {
		filters.SuiteID = ModelBenchmarkSuiteID
	}
	if filters.SuiteVersion == "" {
		filters.SuiteVersion = ModelBenchmarkSuiteVersion
	}
	if filters.ScoringVersion == "" {
		filters.ScoringVersion = ModelBenchmarkScoringVer
	}
	if filters.ComparisonKey == "" && filters.SuiteVersion == ModelBenchmarkSuiteVersion {
		filters.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	if filters.SuiteVersion == ModelBenchmarkSuiteVersion {
		if filters.MaxOutputTokens <= 0 {
			filters.MaxOutputTokens = 2048
		}
		if filters.TimeoutSeconds <= 0 {
			filters.TimeoutSeconds = 90
		}
		if filters.SampleCount <= 0 {
			filters.SampleCount = 1
		}
	}
	return s.repo.ListHistory(ctx, filters)
}

// ListRankings returns models with at least three comparable runs.
func (s *ModelBenchmarkService) ListRankings(ctx context.Context, limit int, comparisonKeys ...string) ([]*ModelBenchmarkRanking, error) {
	key := ModelBenchmarkDefaultComparisonKey
	if len(comparisonKeys) > 0 && comparisonKeys[0] != "" {
		key = comparisonKeys[0]
	}
	return s.ListComparisonRankings(ctx, limit, ModelBenchmarkHistoryFilters{ComparisonKey: key})
}
func (s *ModelBenchmarkService) ListComparisonRankings(ctx context.Context, limit int, filters ModelBenchmarkHistoryFilters) ([]*ModelBenchmarkRanking, error) {
	if filters.ComparisonKey == "" {
		filters.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	if filters.MaxOutputTokens <= 0 {
		filters.MaxOutputTokens = 2048
	}
	if filters.TimeoutSeconds <= 0 {
		filters.TimeoutSeconds = 90
	}
	if filters.SampleCount <= 0 {
		filters.SampleCount = 1
	}
	return s.repo.ListRankings(ctx, ModelBenchmarkSuiteID, ModelBenchmarkSuiteVersion, ModelBenchmarkScoringVer, filters.ComparisonKey, limit, filters.MaxOutputTokens, filters.TimeoutSeconds, filters.SampleCount)
}

func validateModelBenchmarkPlanLimits(plan *ModelBenchmarkPlan) error {
	if plan.ComparisonKey == "" {
		plan.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	if len(plan.ComparisonKey) > 64 {
		return fmt.Errorf("%w: comparison_key exceeds 64 characters", ErrModelBenchmarkInvalidPlan)
	}
	for _, c := range plan.ComparisonKey {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return fmt.Errorf("%w: comparison_key only accepts letters, numbers, dot, underscore and hyphen", ErrModelBenchmarkInvalidPlan)
		}
	}
	if plan.SampleCount < 1 || plan.SampleCount > 3 ||
		plan.MaxOutputTokens < 128 || plan.MaxOutputTokens > 4096 ||
		plan.TimeoutSeconds < 10 || plan.TimeoutSeconds > 600 ||
		plan.MaxResults < 1 || plan.MaxResults > 500 {
		return fmt.Errorf("%w: sample_count, max_output_tokens, timeout_seconds or max_results is out of range", ErrModelBenchmarkInvalidPlan)
	}
	return nil
}

// ExecutePlan runs all generated items serially and persists aggregate evidence.
func (s *ModelBenchmarkService) ExecutePlan(ctx context.Context, plan *ModelBenchmarkPlan) error {
	if s.executor == nil {
		return fmt.Errorf("model benchmark executor is unavailable")
	}
	account, err := s.accounts.GetByID(ctx, plan.AccountID)
	if err != nil {
		return err
	}
	// Generation and scoring use the built-in implementation, including for older plans.
	run := &ModelBenchmarkRun{ComparisonKey: plan.ComparisonKey, MaxOutputTokens: plan.MaxOutputTokens, TimeoutSeconds: plan.TimeoutSeconds, SampleCount: plan.SampleCount, ItemCount: len(modelBenchmarkFamilies) * 2 * max(plan.SampleCount, 1), PlanID: plan.ID, SuiteID: ModelBenchmarkSuiteID, SuiteVersion: ModelBenchmarkSuiteVersion, ScoringVersion: ModelBenchmarkScoringVer, ModelID: plan.ModelID, UpstreamModelID: account.GetMappedModel(plan.ModelID), AccountID: plan.AccountID, StartedAt: time.Now(), UsageSource: "missing", UsageFieldCoverage: map[string]bool{}, CategoryScores: map[string]float64{}}
	if account.Platform == PlatformOpenAI && account.IsOpenAIOAuthLike() {
		run.MaxOutputTokens = 0
	}
	if run.ComparisonKey == "" {
		run.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	run, err = s.repo.CreateRun(ctx, run)
	if err != nil {
		return err
	}
	items := make([]ModelBenchmarkRunItem, 0, run.ItemCount)
	runCtx, cancel := context.WithTimeout(ctx, ModelBenchmarkRunBudget(plan))
	defer cancel()
	for sample := 1; sample <= max(plan.SampleCount, 1); sample++ {
		generated := GenerateModelBenchmarkComparisonItems(run.ComparisonKey, sample)
		for _, generatedItem := range generated {
			select {
			case <-runCtx.Done():
				return s.finishRun(ctx, plan, run, items, runCtx.Err())
			default:
			}
			itemCtx, itemCancel := context.WithTimeout(runCtx, time.Duration(plan.TimeoutSeconds)*time.Second)
			result := s.executor.Execute(itemCtx, plan, generatedItem)
			itemCancel()
			if sample > 1 {
				result.ItemID = fmt.Sprintf("%s#%d", result.ItemID, sample)
			}
			result.RunID = run.ID
			if result.UsageSource == "" {
				result.UsageSource = "missing"
			}
			if result.UsageFieldCoverage == nil {
				result.UsageFieldCoverage = map[string]bool{}
			}
			persisted, err := s.repo.CreateItem(ctx, &result)
			if err != nil {
				return s.finishRun(ctx, plan, run, items, err)
			}
			items = append(items, *persisted)
		}
	}
	return s.finishRun(ctx, plan, run, items, nil)
}

func (s *ModelBenchmarkService) finishRun(ctx context.Context, plan *ModelBenchmarkPlan, run *ModelBenchmarkRun, items []ModelBenchmarkRunItem, execErr error) error {
	ctx = context.WithoutCancel(ctx)
	finishCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ctx = finishCtx
	now := time.Now()
	run.FinishedAt = &now
	AggregateModelBenchmarkRun(run, items)
	if execErr != nil {
		run.Status = "partial"
		if run.AnsweredCount == 0 {
			run.Status = "failed"
		}
		run.Quality.RankingEligible = false
		run.ErrorMessage = execErr.Error()
	}
	aggregateModelBenchmarkUsage(run, items)
	if err := s.repo.UpdateRun(ctx, run); err != nil {
		return errors.Join(execErr, err)
	}
	next, err := computeNextRun(plan.CronExpression, time.Now())
	if err != nil {
		return errors.Join(execErr, err)
	}
	if err := s.repo.CompletePlan(ctx, plan.ID, *run.FinishedAt, next); err != nil {
		return errors.Join(execErr, err)
	}
	if err := s.repo.PruneRuns(ctx, plan.ID, plan.MaxResults); err != nil {
		return errors.Join(execErr, err)
	}
	return execErr
}

func aggregateModelBenchmarkUsage(run *ModelBenchmarkRun, items []ModelBenchmarkRunItem) {
	allUpstream := false
	answered := 0
	for _, item := range items {
		if item.Status == "execution_failed" {
			continue
		}
		answered++
		allUpstream = item.UsageSource == "upstream"
		if !allUpstream {
			break
		}
	}
	if answered*5 < len(items)*4 {
		allUpstream = false
	}
	var input, output, total, cost float64
	usageCount := 0
	for _, item := range items {
		if item.InputTokens != nil {
			input += float64(*item.InputTokens)
		}
		if item.OutputTokens != nil {
			output += float64(*item.OutputTokens)
		}
		if item.TotalTokens != nil {
			total += float64(*item.TotalTokens)
		}
		if item.EstimatedCost != nil {
			cost += *item.EstimatedCost
		}
		if item.InputTokens != nil || item.OutputTokens != nil || item.TotalTokens != nil {
			usageCount++
		}
	}
	if usageCount > 0 {
		in := int64(input)
		out := int64(output)
		tot := int64(total)
		run.InputTokens = &in
		run.OutputTokens = &out
		run.TotalTokens = &tot
		run.EstimatedCost = &cost
	}
	if allUpstream {
		run.UsageSource = "upstream"
	} else {
		run.UsageSource = "missing"
	}
}
