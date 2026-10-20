package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type modelBenchmarkRepository struct{ db *sql.DB }

// NewModelBenchmarkRepository creates the model benchmark persistence adapter.
func NewModelBenchmarkRepository(db *sql.DB) service.ModelBenchmarkRepository {
	return &modelBenchmarkRepository{db: db}
}

func (r *modelBenchmarkRepository) CreatePlan(ctx context.Context, plan *service.ModelBenchmarkPlan) (*service.ModelBenchmarkPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO model_benchmark_plans
			(name, suite_id, suite_version, scoring_version, model_id, cron_expression, enabled, sample_count, max_output_tokens, timeout_seconds, max_results, next_run_at, comparison_key, reasoning_effort, reasoning_level)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id,name,suite_id,suite_version,scoring_version,model_id,cron_expression,enabled,sample_count,max_output_tokens,timeout_seconds,max_results,running_status,claim_expires_at,manual_trigger_at,last_run_at,next_run_at,created_at,updated_at,comparison_key,reasoning_effort,reasoning_level
	`, plan.Name, plan.SuiteID, plan.SuiteVersion, plan.ScoringVersion, plan.ModelID, plan.CronExpression, plan.Enabled, plan.SampleCount, plan.MaxOutputTokens, plan.TimeoutSeconds, plan.MaxResults, plan.NextRunAt, plan.ComparisonKey, plan.ReasoningEffort, plan.ReasoningLevel)
	return scanModelBenchmarkPlan(row)
}

func (r *modelBenchmarkRepository) GetPlan(ctx context.Context, id int64) (*service.ModelBenchmarkPlan, error) {
	row := r.db.QueryRowContext(ctx, modelBenchmarkPlanSelect+` WHERE id=$1`, id)
	return scanModelBenchmarkPlan(row)
}

func (r *modelBenchmarkRepository) ListPlans(ctx context.Context) ([]*service.ModelBenchmarkPlan, error) {
	rows, err := r.db.QueryContext(ctx, modelBenchmarkPlanSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*service.ModelBenchmarkPlan
	for rows.Next() {
		plan, err := scanModelBenchmarkPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, rows.Err()
}

func (r *modelBenchmarkRepository) UpdatePlan(ctx context.Context, plan *service.ModelBenchmarkPlan) (*service.ModelBenchmarkPlan, error) {
	row := r.db.QueryRowContext(ctx, `
		UPDATE model_benchmark_plans SET name=$2,suite_id=$3,suite_version=$4,scoring_version=$5,model_id=$6,cron_expression=$7,enabled=$8,sample_count=$9,max_output_tokens=$10,timeout_seconds=$11,max_results=$12,next_run_at=$13,manual_trigger_at=$14,comparison_key=$15,reasoning_effort=$16,reasoning_level=$17,updated_at=NOW()
		WHERE id=$1 AND running_status='idle'
		RETURNING id,name,suite_id,suite_version,scoring_version,model_id,cron_expression,enabled,sample_count,max_output_tokens,timeout_seconds,max_results,running_status,claim_expires_at,manual_trigger_at,last_run_at,next_run_at,created_at,updated_at,comparison_key,reasoning_effort,reasoning_level
	`, plan.ID, plan.Name, plan.SuiteID, plan.SuiteVersion, plan.ScoringVersion, plan.ModelID, plan.CronExpression, plan.Enabled, plan.SampleCount, plan.MaxOutputTokens, plan.TimeoutSeconds, plan.MaxResults, plan.NextRunAt, plan.ManualTriggerAt, plan.ComparisonKey, plan.ReasoningEffort, plan.ReasoningLevel)
	return scanModelBenchmarkPlan(row)
}

func (r *modelBenchmarkRepository) DeletePlan(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM model_benchmark_plans WHERE id=$1`, id)
	return err
}

// ApplyPlanBudgetDefaults 将测评设置中全局统一的采样次数与单题输出预算回填到所有计划。
// 不带 running_status 条件：执行中计划的内存对象与已拷贝到 run 的快照不受本更新影响。
func (r *modelBenchmarkRepository) ApplyPlanBudgetDefaults(ctx context.Context, sampleCount, maxOutputTokens int) error {
	_, err := r.db.ExecContext(ctx, `UPDATE model_benchmark_plans SET sample_count=$1,max_output_tokens=$2,updated_at=NOW()`, sampleCount, maxOutputTokens)
	return err
}

func (r *modelBenchmarkRepository) ClaimDuePlans(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*service.ModelBenchmarkPlan, error) {
	if limit <= 0 {
		limit = 1
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// cron due 仅对启用计划生效；手动排队（manual_trigger_at）对停用计划同样生效，
	// 且优先于周期任务（用户点“立即执行”期望尽快跑）。停用计划不参与 cron 分支，
	// 避免其遗留的 next_run_at 旧值把已停用计划反复拉起。
	rows, err := tx.QueryContext(ctx, modelBenchmarkPlanSelect+`
		WHERE ((enabled = TRUE AND next_run_at <= $1) OR manual_trigger_at IS NOT NULL)
		  AND (running_status='idle' OR claim_expires_at < $1)
		ORDER BY (manual_trigger_at IS NULL) ASC, COALESCE(manual_trigger_at, next_run_at) ASC
		LIMIT $2 FOR UPDATE SKIP LOCKED
	`, now, limit)
	if err != nil {
		return nil, err
	}
	var plans []*service.ModelBenchmarkPlan
	for rows.Next() {
		plan, err := scanModelBenchmarkPlan(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		plans = append(plans, plan)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, plan := range plans {
		// Each claim must cover the complete configured form and finalization.
		// 领取即消费一次性手动排队标记，避免同一请求被重复执行。
		if _, err := tx.ExecContext(ctx, `UPDATE model_benchmark_plans SET running_status='running',claim_expires_at=$2,manual_trigger_at=NULL,updated_at=NOW() WHERE id=$1`, plan.ID, now.Add(max(lease, service.ModelBenchmarkRunBudget(plan)+time.Minute))); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *modelBenchmarkRepository) CompletePlan(ctx context.Context, id int64, lastRunAt, nextRunAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE model_benchmark_plans SET running_status='idle',claim_expires_at=NULL,last_run_at=$2,next_run_at=$3,updated_at=NOW() WHERE id=$1`, id, lastRunAt, nextRunAt)
	return err
}

func (r *modelBenchmarkRepository) ReleaseExpiredClaims(ctx context.Context, now time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE model_benchmark_plans SET running_status='idle',claim_expires_at=NULL,updated_at=NOW() WHERE running_status='running' AND claim_expires_at < $1`, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *modelBenchmarkRepository) CreateRun(ctx context.Context, run *service.ModelBenchmarkRun) (*service.ModelBenchmarkRun, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO model_benchmark_runs (plan_id,suite_id,suite_version,scoring_version,model_id,upstream_model_id,status,started_at,comparison_key,max_output_tokens,timeout_seconds,reasoning_effort,reasoning_level,sample_count,item_count)
		VALUES ($1,$2,$3,$4,$5,$6,'running',$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id,plan_id,suite_id,suite_version,scoring_version,model_id,upstream_model_id,status,intelligence_score,efficiency_score,composite_score,category_scores,item_count,answered_count,correct_count,input_tokens,output_tokens,total_tokens,estimated_cost,avg_latency_ms,p95_latency_ms,usage_source,usage_field_coverage,error_message,started_at,finished_at,created_at,comparison_key,max_output_tokens,timeout_seconds,reasoning_effort,reasoning_level,sample_count,quality
	`, run.PlanID, run.SuiteID, run.SuiteVersion, run.ScoringVersion, run.ModelID, run.UpstreamModelID, run.StartedAt, run.ComparisonKey, run.MaxOutputTokens, run.TimeoutSeconds, run.ReasoningEffort, run.ReasoningLevel, run.SampleCount, run.ItemCount)
	return scanModelBenchmarkRun(row)
}

func (r *modelBenchmarkRepository) UpdateRun(ctx context.Context, run *service.ModelBenchmarkRun) error {
	scores, err := json.Marshal(run.CategoryScores)
	if err != nil {
		return err
	}
	coverage, err := json.Marshal(run.UsageFieldCoverage)
	if err != nil {
		return err
	}
	quality, err := json.Marshal(run.Quality)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE model_benchmark_runs SET status=$2,intelligence_score=$3,efficiency_score=$4,composite_score=$5,category_scores=$6,item_count=$7,answered_count=$8,correct_count=$9,input_tokens=$10,output_tokens=$11,total_tokens=$12,estimated_cost=$13,avg_latency_ms=$14,p95_latency_ms=$15,usage_source=$16,usage_field_coverage=$17,error_message=$18,finished_at=$19,quality=$20 WHERE id=$1
	`, run.ID, run.Status, run.IntelligenceScore, run.EfficiencyScore, run.CompositeScore, scores, run.ItemCount, run.AnsweredCount, run.CorrectCount, run.InputTokens, run.OutputTokens, run.TotalTokens, run.EstimatedCost, run.AvgLatencyMS, run.P95LatencyMS, run.UsageSource, coverage, run.ErrorMessage, run.FinishedAt, quality)
	return err
}

func (r *modelBenchmarkRepository) CreateItem(ctx context.Context, item *service.ModelBenchmarkRunItem) (*service.ModelBenchmarkRunItem, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO model_benchmark_run_items (run_id,item_id,family_id,category,difficulty,weight,prompt_hash,prompt_snapshot,response_text,status,score,earned_points,possible_points,checker_type,failure_reason,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_write_tokens,total_tokens,estimated_cost,usage_source,usage_field_coverage,pricing_source,latency_ms,first_token_ms,error_message)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)
		RETURNING id,run_id,item_id,family_id,category,difficulty,weight,prompt_hash,prompt_snapshot,response_text,status,score,earned_points,possible_points,checker_type,failure_reason,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_write_tokens,total_tokens,estimated_cost,usage_source,usage_field_coverage,pricing_source,latency_ms,first_token_ms,error_message,created_at
	`, item.RunID, item.ItemID, item.FamilyID, item.Category, item.Difficulty, item.Weight, item.PromptHash, item.PromptSnapshot, item.ResponseText, item.Status, item.Score, item.EarnedPoints, item.PossiblePoints, item.CheckerType, item.FailureReason, item.InputTokens, item.OutputTokens, item.ReasoningTokens, item.CacheReadTokens, item.CacheWriteTokens, item.TotalTokens, item.EstimatedCost, item.UsageSource, modelBenchmarkCoverageJSON(item.UsageFieldCoverage), item.PricingSource, item.LatencyMS, item.FirstTokenMS, item.ErrorMessage)
	return scanModelBenchmarkRunItem(row)
}

func (r *modelBenchmarkRepository) ListRuns(ctx context.Context, filters service.ModelBenchmarkRunFilters) ([]*service.ModelBenchmarkRun, error) {
	where := ` WHERE 1=1`
	args := []any{}
	if filters.ComparisonKey != "" {
		args = append(args, filters.ComparisonKey)
		where += fmt.Sprintf(` AND comparison_key=$%d`, len(args))
	}
	if filters.PlanID != nil {
		args = append(args, *filters.PlanID)
		where += fmt.Sprintf(` AND plan_id=$%d`, len(args))
	}
	for _, field := range []struct {
		name  string
		value int
	}{{"max_output_tokens", filters.MaxOutputTokens}, {"timeout_seconds", filters.TimeoutSeconds}, {"sample_count", filters.SampleCount}} {
		if field.value > 0 {
			args = append(args, field.value)
			where += fmt.Sprintf(" AND %s=$%d", field.name, len(args))
		}
	}
	if filters.ModelID != "" {
		args = append(args, filters.ModelID)
		where += fmt.Sprintf(` AND model_id=$%d`, len(args))
	}
	if filters.Status != "" {
		args = append(args, filters.Status)
		where += fmt.Sprintf(` AND status=$%d`, len(args))
	}
	if filters.Limit <= 0 {
		filters.Limit = 50
	}
	args = append(args, filters.Limit, filters.Offset)
	rows, err := r.db.QueryContext(ctx, modelBenchmarkRunSelect+where+fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*service.ModelBenchmarkRun
	for rows.Next() {
		run, err := scanModelBenchmarkRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (r *modelBenchmarkRepository) GetRun(ctx context.Context, id int64) (*service.ModelBenchmarkRun, error) {
	return scanModelBenchmarkRun(r.db.QueryRowContext(ctx, modelBenchmarkRunSelect+` WHERE id=$1`, id))
}

func (r *modelBenchmarkRepository) ListItems(ctx context.Context, runID int64) ([]*service.ModelBenchmarkRunItem, error) {
	rows, err := r.db.QueryContext(ctx, modelBenchmarkItemSelect+` WHERE run_id=$1 ORDER BY item_id`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*service.ModelBenchmarkRunItem
	for rows.Next() {
		item, err := scanModelBenchmarkRunItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *modelBenchmarkRepository) ListHistory(ctx context.Context, filters service.ModelBenchmarkHistoryFilters) ([]*service.ModelBenchmarkHistoryPoint, error) {
	args := []any{filters.SuiteID, filters.SuiteVersion, filters.ScoringVersion}
	where := ` WHERE suite_id=$1 AND suite_version=$2 AND scoring_version=$3`
	if filters.ComparisonKey != "" {
		args = append(args, filters.ComparisonKey)
		where += fmt.Sprintf(` AND comparison_key=$%d`, len(args))
	}
	for _, field := range []struct {
		name  string
		value int
	}{{"max_output_tokens", filters.MaxOutputTokens}, {"timeout_seconds", filters.TimeoutSeconds}, {"sample_count", filters.SampleCount}} {
		if field.value > 0 {
			args = append(args, field.value)
			where += fmt.Sprintf(" AND %s=$%d", field.name, len(args))
		}
	}
	if filters.ModelID != "" {
		args = append(args, filters.ModelID)
		where += fmt.Sprintf(` AND model_id=$%d`, len(args))
	}
	if !filters.From.IsZero() {
		args = append(args, filters.From)
		where += fmt.Sprintf(` AND created_at >= $%d`, len(args))
	}
	if !filters.To.IsZero() {
		args = append(args, filters.To)
		where += fmt.Sprintf(` AND created_at <= $%d`, len(args))
	}
	if filters.Limit <= 0 {
		filters.Limit = 500
	}
	args = append(args, filters.Limit)
	rows, err := r.db.QueryContext(ctx, `SELECT id,model_id,created_at,status,intelligence_score,efficiency_score,composite_score,output_tokens,total_tokens,avg_latency_ms,p95_latency_ms,estimated_cost,comparison_key,max_output_tokens,timeout_seconds,sample_count FROM model_benchmark_runs`+where+fmt.Sprintf(` ORDER BY created_at ASC LIMIT $%d`, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*service.ModelBenchmarkHistoryPoint
	for rows.Next() {
		p := &service.ModelBenchmarkHistoryPoint{}
		if err := rows.Scan(&p.RunID, &p.ModelID, &p.CreatedAt, &p.Status, &p.IntelligenceScore, &p.EfficiencyScore, &p.CompositeScore, &p.OutputTokens, &p.TotalTokens, &p.AvgLatencyMS, &p.P95LatencyMS, &p.EstimatedCost, &p.ComparisonKey, &p.MaxOutputTokens, &p.TimeoutSeconds, &p.SampleCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListRankings 返回指定题库下每模型最新一次成功/部分成功 run 的排名：
// 不设 ranking_eligible/最少次数/批次/预算门槛，分数与形态字段均取自该最新 run。
func (r *modelBenchmarkRepository) ListRankings(ctx context.Context, suiteID, suiteVersion, scoringVersion string, limit int) ([]*service.ModelBenchmarkRanking, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT model_id,suite_id,suite_version,scoring_version,run_count,
            intelligence_score,efficiency_score,composite_score,output_tokens,
            avg_latency_ms,estimated_cost,last_run_at,
            comparison_key,max_output_tokens,timeout_seconds,sample_count,
            reasoning_effort,reasoning_level,
            category_scores
        FROM (
            SELECT model_id,suite_id,suite_version,scoring_version,
                intelligence_score,efficiency_score,composite_score,output_tokens,
                avg_latency_ms,estimated_cost,created_at AS last_run_at,
                comparison_key,max_output_tokens,timeout_seconds,sample_count,
                reasoning_effort,reasoning_level,
                category_scores,
                COUNT(*) OVER (PARTITION BY model_id) AS run_count,
                ROW_NUMBER() OVER (PARTITION BY model_id ORDER BY created_at DESC, id DESC) AS rn
            FROM model_benchmark_runs
            WHERE suite_id=$1 AND suite_version=$2 AND scoring_version=$3
                AND status IN ('success','partial') AND intelligence_score IS NOT NULL
        ) ranked
        WHERE rn=1
        ORDER BY intelligence_score DESC NULLS LAST, run_count DESC, model_id ASC
        LIMIT $4
    `, suiteID, suiteVersion, scoringVersion, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*service.ModelBenchmarkRanking{}
	for rows.Next() {
		item := &service.ModelBenchmarkRanking{}
		var rawCat []byte
		if err := rows.Scan(&item.ModelID, &item.SuiteID, &item.SuiteVersion, &item.ScoringVersion, &item.RunCount, &item.IntelligenceScore, &item.EfficiencyScore, &item.CompositeScore, &item.OutputTokens, &item.AvgLatencyMS, &item.EstimatedCost, &item.LastRunAt, &item.ComparisonKey, &item.MaxOutputTokens, &item.TimeoutSeconds, &item.SampleCount, &item.ReasoningEffort, &item.ReasoningLevel, &rawCat); err != nil {
			return nil, err
		}
		if len(rawCat) > 0 {
			_ = json.Unmarshal(rawCat, &item.CategoryScores)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListRankingSuites 列出存在可排名执行的题库版本组合；入选谓词与 ListRankings 保持逐字一致。
func (r *modelBenchmarkRepository) ListRankingSuites(ctx context.Context) ([]*service.ModelBenchmarkRankingSuite, error) {
	rows, err := r.db.QueryContext(ctx, `
        SELECT suite_id,suite_version,scoring_version,COUNT(*),MAX(created_at)
        FROM model_benchmark_runs
        WHERE status IN ('success','partial') AND intelligence_score IS NOT NULL
        GROUP BY suite_id,suite_version,scoring_version
        ORDER BY MAX(created_at) DESC
    `)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*service.ModelBenchmarkRankingSuite{}
	for rows.Next() {
		item := &service.ModelBenchmarkRankingSuite{}
		if err := rows.Scan(&item.SuiteID, &item.SuiteVersion, &item.ScoringVersion, &item.RunCount, &item.LastRunAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *modelBenchmarkRepository) PruneRuns(ctx context.Context, planID int64, keep int) error {
	if keep <= 0 {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `DELETE FROM model_benchmark_runs WHERE plan_id=$1 AND id NOT IN (SELECT id FROM model_benchmark_runs WHERE plan_id=$1 ORDER BY created_at DESC LIMIT $2)`, planID, keep)
	return err
}

// LatestScores 返回每个模型最近一次成功/部分成功 run 的能力分快照。
// 刻意不做套件版本过滤：模型清单页展示的是「最近测试分数」，跨版本的历史分数
// 也应可见（前端按返回值标注版本差异）；亦不设最少 run 次数门槛（排名接口才需要）。
func (r *modelBenchmarkRepository) LatestScores(ctx context.Context) (map[string]service.ModelBenchmarkLatestScore, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT ON (model_id) model_id, intelligence_score, suite_id, suite_version, scoring_version, created_at
		FROM model_benchmark_runs
		WHERE status IN ('success','partial') AND intelligence_score IS NOT NULL
		ORDER BY model_id, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]service.ModelBenchmarkLatestScore{}
	for rows.Next() {
		var score service.ModelBenchmarkLatestScore
		if err := rows.Scan(&score.ModelID, &score.IntelligenceScore, &score.SuiteID, &score.SuiteVersion, &score.ScoringVersion, &score.ScoredAt); err != nil {
			return nil, err
		}
		out[score.ModelID] = score
	}
	return out, rows.Err()
}

func modelBenchmarkCoverageJSON(coverage map[string]bool) []byte {
	if coverage == nil {
		coverage = map[string]bool{}
	}
	raw, _ := json.Marshal(coverage)
	return raw
}

const modelBenchmarkPlanSelect = `SELECT id,name,suite_id,suite_version,scoring_version,model_id,cron_expression,enabled,sample_count,max_output_tokens,timeout_seconds,max_results,running_status,claim_expires_at,manual_trigger_at,last_run_at,next_run_at,created_at,updated_at,comparison_key,reasoning_effort,reasoning_level FROM model_benchmark_plans`
const modelBenchmarkRunSelect = `SELECT id,plan_id,suite_id,suite_version,scoring_version,model_id,upstream_model_id,status,intelligence_score,efficiency_score,composite_score,category_scores,item_count,answered_count,correct_count,input_tokens,output_tokens,total_tokens,estimated_cost,avg_latency_ms,p95_latency_ms,usage_source,usage_field_coverage,error_message,started_at,finished_at,created_at,comparison_key,max_output_tokens,timeout_seconds,reasoning_effort,reasoning_level,sample_count,quality FROM model_benchmark_runs`
const modelBenchmarkItemSelect = `SELECT id,run_id,item_id,family_id,category,difficulty,weight,prompt_hash,prompt_snapshot,response_text,status,score,earned_points,possible_points,checker_type,failure_reason,input_tokens,output_tokens,reasoning_tokens,cache_read_tokens,cache_write_tokens,total_tokens,estimated_cost,usage_source,usage_field_coverage,pricing_source,latency_ms,first_token_ms,error_message,created_at FROM model_benchmark_run_items`

type modelBenchmarkScanner interface{ Scan(dest ...any) error }

func scanModelBenchmarkPlan(row modelBenchmarkScanner) (*service.ModelBenchmarkPlan, error) {
	p := &service.ModelBenchmarkPlan{}
	err := row.Scan(&p.ID, &p.Name, &p.SuiteID, &p.SuiteVersion, &p.ScoringVersion, &p.ModelID, &p.CronExpression, &p.Enabled, &p.SampleCount, &p.MaxOutputTokens, &p.TimeoutSeconds, &p.MaxResults, &p.RunningStatus, &p.ClaimExpiresAt, &p.ManualTriggerAt, &p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt, &p.ComparisonKey, &p.ReasoningEffort, &p.ReasoningLevel)
	if err != nil {
		return nil, err
	}
	return p, nil
}
func scanModelBenchmarkRun(row modelBenchmarkScanner) (*service.ModelBenchmarkRun, error) {
	run := &service.ModelBenchmarkRun{CategoryScores: map[string]float64{}, UsageFieldCoverage: map[string]bool{}}
	var category, coverage, quality []byte
	err := row.Scan(&run.ID, &run.PlanID, &run.SuiteID, &run.SuiteVersion, &run.ScoringVersion, &run.ModelID, &run.UpstreamModelID, &run.Status, &run.IntelligenceScore, &run.EfficiencyScore, &run.CompositeScore, &category, &run.ItemCount, &run.AnsweredCount, &run.CorrectCount, &run.InputTokens, &run.OutputTokens, &run.TotalTokens, &run.EstimatedCost, &run.AvgLatencyMS, &run.P95LatencyMS, &run.UsageSource, &coverage, &run.ErrorMessage, &run.StartedAt, &run.FinishedAt, &run.CreatedAt, &run.ComparisonKey, &run.MaxOutputTokens, &run.TimeoutSeconds, &run.ReasoningEffort, &run.ReasoningLevel, &run.SampleCount, &quality)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(category, &run.CategoryScores)
	_ = json.Unmarshal(coverage, &run.UsageFieldCoverage)
	_ = json.Unmarshal(quality, &run.Quality)
	return run, nil
}
func scanModelBenchmarkRunItem(row modelBenchmarkScanner) (*service.ModelBenchmarkRunItem, error) {
	item := &service.ModelBenchmarkRunItem{UsageFieldCoverage: map[string]bool{}}
	var itemCoverage []byte
	err := row.Scan(&item.ID, &item.RunID, &item.ItemID, &item.FamilyID, &item.Category, &item.Difficulty, &item.Weight, &item.PromptHash, &item.PromptSnapshot, &item.ResponseText, &item.Status, &item.Score, &item.EarnedPoints, &item.PossiblePoints, &item.CheckerType, &item.FailureReason, &item.InputTokens, &item.OutputTokens, &item.ReasoningTokens, &item.CacheReadTokens, &item.CacheWriteTokens, &item.TotalTokens, &item.EstimatedCost, &item.UsageSource, &itemCoverage, &item.PricingSource, &item.LatencyMS, &item.FirstTokenMS, &item.ErrorMessage, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(itemCoverage, &item.UsageFieldCoverage)
	return item, nil
}
