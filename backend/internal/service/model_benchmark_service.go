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
	if err := validateModelBenchmarkPlanLimits(input); err != nil {
		return nil, err
	}
	if input.MaxOutputTokens <= 0 {
		input.MaxOutputTokens = 768
	}
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = 90
	}
	if input.MaxResults <= 0 {
		input.MaxResults = 50
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

// QueueRun sets an enabled plan due now; the runner claims it atomically.
func (s *ModelBenchmarkService) QueueRun(ctx context.Context, id int64) (*ModelBenchmarkPlan, error) {
	plan, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	if !plan.Enabled {
		return nil, fmt.Errorf("%w: plan is disabled", ErrModelBenchmarkInvalidPlan)
	}
	now := time.Now()
	plan.NextRunAt = &now
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
	return s.repo.ListHistory(ctx, filters)
}

// ListRankings returns models with at least three comparable runs.
func (s *ModelBenchmarkService) ListRankings(ctx context.Context, limit int) ([]*ModelBenchmarkRanking, error) {
	return s.repo.ListRankings(ctx, ModelBenchmarkSuiteID, ModelBenchmarkSuiteVersion, ModelBenchmarkScoringVer, limit)
}

func validateModelBenchmarkPlanLimits(plan *ModelBenchmarkPlan) error {
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
	run := &ModelBenchmarkRun{PlanID: plan.ID, SuiteID: plan.SuiteID, SuiteVersion: plan.SuiteVersion, ScoringVersion: plan.ScoringVersion, ModelID: plan.ModelID, UpstreamModelID: account.GetMappedModel(plan.ModelID), AccountID: plan.AccountID, StartedAt: time.Now(), UsageSource: "missing", UsageFieldCoverage: map[string]bool{}, CategoryScores: map[string]float64{}}
	run, err = s.repo.CreateRun(ctx, run)
	if err != nil {
		return err
	}
	generated := GenerateModelBenchmarkItems(plan.ID, run.ID)
	items := make([]ModelBenchmarkRunItem, 0, len(generated)*plan.SampleCount)
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(len(generated)*plan.SampleCount*plan.TimeoutSeconds+30)*time.Second)
	defer cancel()
	for sample := 1; sample <= plan.SampleCount; sample++ {
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
	now := time.Now()
	run.FinishedAt = &now
	AggregateModelBenchmarkRun(run, items)
	if execErr != nil {
		run.Status = "failed"
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
