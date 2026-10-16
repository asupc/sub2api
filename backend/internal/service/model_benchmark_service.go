package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ModelBenchmarkAPIKeyStore 是测评所需的 API Key 窄依赖：保存设置时校验完整
// Key（含分组/用户），执行时取明文做回环认证。
type ModelBenchmarkAPIKeyStore interface {
	GetByID(ctx context.Context, id int64) (*APIKey, error)
	GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error)
}

// ModelBenchmarkService manages plans and executes model benchmark runs.
type ModelBenchmarkService struct {
	repo            ModelBenchmarkRepository
	apiKeys         ModelBenchmarkAPIKeyStore
	settings        *SettingService
	manifestService *ModelManifestService
	executor        *ModelBenchmarkExecutor
}

// NewModelBenchmarkService creates the benchmark application service.
func NewModelBenchmarkService(repo ModelBenchmarkRepository, apiKeys ModelBenchmarkAPIKeyStore, settings *SettingService, manifestService *ModelManifestService, executor *ModelBenchmarkExecutor) *ModelBenchmarkService {
	return &ModelBenchmarkService{repo: repo, apiKeys: apiKeys, settings: settings, manifestService: manifestService, executor: executor}
}

// ListSuites returns the only built-in suite in Phase 1.
func (s *ModelBenchmarkService) ListSuites() []ModelBenchmarkSuite {
	return []ModelBenchmarkSuite{GetModelBenchmarkSuite()}
}

// ListTargets returns the model-manifest view filtered to benchmark-supported models.
// 网关回环模式下不再逐账号适配：模型只要有可调度账号（网关可路由）且非
// 图像/视频生成模型即可测评。
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
		if !isModelBenchmarkSupportedModel(model.ID, model.ActiveAccounts > 0) {
			continue
		}
		out = append(out, model)
	}
	return out, nil
}

// GetBenchmarkSettings returns the current benchmark settings (loopback API key).
func (s *ModelBenchmarkService) GetBenchmarkSettings(ctx context.Context) (*ModelBenchmarkSettings, error) {
	if s.settings == nil {
		return nil, fmt.Errorf("setting service is unavailable")
	}
	return s.settings.GetModelBenchmarkSettings(ctx)
}

// UpdateBenchmarkSettings validates and persists the loopback API key choice.
// 校验：Key 存在、启用、未过期且已绑定分组（未分组 Key 会被网关的
// RequireGroupAssignment 拦截）；KeyID=0 表示清除配置。
func (s *ModelBenchmarkService) UpdateBenchmarkSettings(ctx context.Context, input *ModelBenchmarkSettings) (*ModelBenchmarkSettings, error) {
	if s.settings == nil || s.apiKeys == nil {
		return nil, fmt.Errorf("benchmark settings dependencies are unavailable")
	}
	if input == nil {
		return nil, fmt.Errorf("%w: settings payload is required", ErrModelBenchmarkInvalidPlan)
	}
	if input.APIKeyID != 0 {
		key, err := s.apiKeys.GetByID(ctx, input.APIKeyID)
		if err != nil {
			return nil, fmt.Errorf("%w: API key not found", ErrModelBenchmarkInvalidPlan)
		}
		if !key.IsActive() {
			return nil, fmt.Errorf("%w: API key is not active", ErrModelBenchmarkInvalidPlan)
		}
		if key.IsExpired() {
			return nil, fmt.Errorf("%w: API key is expired", ErrModelBenchmarkInvalidPlan)
		}
		if key.GroupID == nil {
			return nil, fmt.Errorf("%w: API key has no group assignment", ErrModelBenchmarkInvalidPlan)
		}
	}
	if err := s.settings.SetModelBenchmarkSettings(ctx, input); err != nil {
		return nil, err
	}
	return s.settings.GetModelBenchmarkSettings(ctx)
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
	if err := s.validateBenchmarkModel(ctx, input.ModelID); err != nil {
		return nil, err
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
	if err := s.validateBenchmarkModel(ctx, existing.ModelID); err != nil {
		return nil, err
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
// 当明确指定了 ModelID 且前端未传 ComparisonKey 时，不强制施加默认 comparison_key 与预算过滤，
// 以便查看该模型的全量历史演进趋势。
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
	// 仅在未指定特定模型且未传 comparison_key 时才回退到默认基准条件
	if filters.ModelID == "" && filters.ComparisonKey == "" && filters.SuiteVersion == ModelBenchmarkSuiteVersion {
		filters.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	if filters.ModelID == "" && filters.SuiteVersion == ModelBenchmarkSuiteVersion {
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

// GetModelOverview 聚合返回单模型全景数据：排名与能力分、最新Run、关联计划、最近Runs与历史趋势
func (s *ModelBenchmarkService) GetModelOverview(ctx context.Context, modelID string) (*ModelBenchmarkModelOverview, error) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return nil, errors.New("model_id is required")
	}

	overview := &ModelBenchmarkModelOverview{
		ModelID:    modelID,
		Plans:      []*ModelBenchmarkPlan{},
		RecentRuns: []*ModelBenchmarkRun{},
		History:    []*ModelBenchmarkHistoryPoint{},
	}

	// 1. 查询该模型在最新题库下的排名
	rankings, err := s.ListRankings(ctx, ModelBenchmarkRankingFilters{
		SuiteID:        ModelBenchmarkSuiteID,
		SuiteVersion:   ModelBenchmarkSuiteVersion,
		ScoringVersion: ModelBenchmarkScoringVer,
	}, 100)
	if err == nil {
		for _, r := range rankings {
			if r.ModelID == modelID {
				overview.Ranking = r
				break
			}
		}
	}

	// 2. 查询该模型最近一次成功的 Run (以及最近 10 次 Runs)
	recentRuns, err := s.repo.ListRuns(ctx, ModelBenchmarkRunFilters{
		ModelID: modelID,
		Limit:   10,
	})
	if err == nil {
		overview.RecentRuns = recentRuns
		for _, r := range recentRuns {
			if r.Status == "success" || r.Status == "partial" {
				overview.LatestRun = r
				break
			}
		}
		if overview.LatestRun == nil && len(recentRuns) > 0 {
			overview.LatestRun = recentRuns[0]
		}
	}

	// 3. 查询关联的计划
	allPlans, err := s.repo.ListPlans(ctx)
	if err == nil {
		for _, p := range allPlans {
			if p.ModelID == modelID {
				overview.Plans = append(overview.Plans, p)
			}
		}
	}

	// 4. 查询该模型历史趋势点 (按时间顺序，无预算死锁限制)
	history, err := s.ListHistory(ctx, ModelBenchmarkHistoryFilters{
		ModelID:        modelID,
		SuiteID:        ModelBenchmarkSuiteID,
		SuiteVersion:   ModelBenchmarkSuiteVersion,
		ScoringVersion: ModelBenchmarkScoringVer,
		Limit:          100,
	})
	if err == nil {
		overview.History = history
	}

	return overview, nil
}

// ListRankings 返回指定题库（空则编译期最新题库）下、每模型最新一次成功/部分成功 run 的排名。
func (s *ModelBenchmarkService) ListRankings(ctx context.Context, filters ModelBenchmarkRankingFilters, limit int) ([]*ModelBenchmarkRanking, error) {
	if filters.SuiteID == "" {
		filters.SuiteID = ModelBenchmarkSuiteID
	}
	if filters.SuiteVersion == "" {
		filters.SuiteVersion = ModelBenchmarkSuiteVersion
	}
	if filters.ScoringVersion == "" {
		filters.ScoringVersion = ModelBenchmarkScoringVer
	}
	return s.repo.ListRankings(ctx, filters.SuiteID, filters.SuiteVersion, filters.ScoringVersion, limit)
}

// ListRankingSuites 列出存在可排名执行的历史题库版本。
func (s *ModelBenchmarkService) ListRankingSuites(ctx context.Context) ([]*ModelBenchmarkRankingSuite, error) {
	return s.repo.ListRankingSuites(ctx)
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

// validateBenchmarkModel 校验模型当前可测评（模型清单中存在且有可调度账号）。
func (s *ModelBenchmarkService) validateBenchmarkModel(ctx context.Context, modelID string) error {
	targets, err := s.ListTargets(ctx)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.ID == modelID {
			return nil
		}
	}
	return ErrModelBenchmarkUnsupportedTarget
}

// ExecutePlan runs all generated items serially and persists aggregate evidence.
func (s *ModelBenchmarkService) ExecutePlan(ctx context.Context, plan *ModelBenchmarkPlan) error {
	if s.executor == nil {
		return fmt.Errorf("model benchmark executor is unavailable")
	}
	// Generation and scoring use the built-in implementation, including for older plans.
	// 网关回环模式：不绑定账号，由网关调度选号；upstream_model_id 以网关实际
	// 服务模型为准（请求模型名兜底），在 executor 内按题捕获计费。
	run := &ModelBenchmarkRun{ComparisonKey: plan.ComparisonKey, MaxOutputTokens: plan.MaxOutputTokens, TimeoutSeconds: plan.TimeoutSeconds, SampleCount: plan.SampleCount, ItemCount: len(modelBenchmarkFamilies) * 2 * max(plan.SampleCount, 1), PlanID: plan.ID, SuiteID: ModelBenchmarkSuiteID, SuiteVersion: ModelBenchmarkSuiteVersion, ScoringVersion: ModelBenchmarkScoringVer, ModelID: plan.ModelID, UpstreamModelID: plan.ModelID, StartedAt: time.Now(), UsageSource: "missing", UsageFieldCoverage: map[string]bool{}, CategoryScores: map[string]float64{}}
	if run.ComparisonKey == "" {
		run.ComparisonKey = ModelBenchmarkDefaultComparisonKey
	}
	run, err := s.repo.CreateRun(ctx, run)
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
