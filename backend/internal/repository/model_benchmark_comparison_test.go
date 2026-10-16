//go:build unit

package repository

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// This fixture deliberately cannot use an application database: it requires an
// explicit DSN and checks the isolated database identity before writing schema.
func TestModelBenchmarkComparisonPostgres(t *testing.T) {
	dsn := os.Getenv("SUB2API_BENCHMARK_FIXTURE_DSN")
	if dsn == "" {
		t.Skip("isolated benchmark_fixture PostgreSQL DSN not provided")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var name string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT current_database()").Scan(&name))
	require.Equal(t, "benchmark_fixture", name, "writes require the isolated fixture database")
	_, err = db.ExecContext(ctx, "CREATE TABLE accounts(id BIGINT PRIMARY KEY);INSERT INTO accounts VALUES (1)")
	require.NoError(t, err)
	for _, file := range []string{"243_so_model_capability_benchmark.sql", "244_so_model_benchmark_manual_trigger.sql"} {
		raw, err := migrations.FS.ReadFile(file)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(raw))
		require.NoError(t, err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO model_benchmark_plans(name,suite_id,suite_version,scoring_version,model_id,account_id) VALUES('old','hard-v1','1.3.0','1.1.0','old-model',1);INSERT INTO model_benchmark_runs(plan_id,suite_id,suite_version,scoring_version,model_id,account_id) VALUES(1,'hard-v1','1.3.0','1.1.0','old-model',1)`)
	require.NoError(t, err)
	for _, file := range []string{"245_so_model_benchmark_comparison.sql", "246_so_model_benchmark_gateway_mode.sql"} {
		migration, err := migrations.FS.ReadFile(file)
		require.NoError(t, err)
		for i := 0; i < 2; i++ {
			_, err = db.ExecContext(ctx, string(migration))
			require.NoError(t, err, "new migration must be idempotent")
		}
	}
	var legacy string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT comparison_key FROM model_benchmark_runs WHERE id=1").Scan(&legacy))
	require.Equal(t, "legacy", legacy)
	repo := NewModelBenchmarkRepository(db)
	plan, err := repo.CreatePlan(ctx, &service.ModelBenchmarkPlan{Name: "fixture", SuiteID: service.ModelBenchmarkSuiteID, SuiteVersion: service.ModelBenchmarkSuiteVersion, ScoringVersion: service.ModelBenchmarkScoringVer, ModelID: "candidate", CronExpression: "0 4 * * 1", SampleCount: 3, MaxOutputTokens: 2048, TimeoutSeconds: 600, MaxResults: 50, ComparisonKey: "before-edit"})
	require.NoError(t, err)
	plan.ComparisonKey = "audit-batch"
	now := time.Now().UTC()
	plan.ManualTriggerAt = &now
	plan, err = repo.UpdatePlan(ctx, plan)
	require.NoError(t, err)
	require.Equal(t, "audit-batch", plan.ComparisonKey)
	got, err := repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "audit-batch", got.ComparisonKey)
	claimed, err := repo.ClaimDuePlans(ctx, now.Add(time.Second), 30*time.Minute, 100)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	got, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.True(t, got.ClaimExpiresAt.After(now.Add(service.ModelBenchmarkRunBudget(plan))), "lease must cover the complete 112-item, 3-sample form")
	var first *service.ModelBenchmarkRun
	for _, group := range []struct {
		model, key string
		tokens     int
		eligible   bool
	}{{"candidate", "audit-batch", 2048, true}, {"candidate", "audit-batch", 4096, true}, {"other-form", "other-batch", 2048, true}, {"low-coverage", "audit-batch", 2048, false}} {
		for i := 0; i < 3; i++ {
			run, err := repo.CreateRun(ctx, &service.ModelBenchmarkRun{PlanID: plan.ID, SuiteID: service.ModelBenchmarkSuiteID, SuiteVersion: service.ModelBenchmarkSuiteVersion, ScoringVersion: service.ModelBenchmarkScoringVer, ModelID: group.model, ComparisonKey: group.key, MaxOutputTokens: group.tokens, TimeoutSeconds: 90, SampleCount: 1, ItemCount: 112, StartedAt: now})
			require.NoError(t, err)
			score := float64(80 + i)
			output, latency := int64(600+i), int64(900+i)
			run.Status = "success"
			run.IntelligenceScore = &score
			run.OutputTokens = &output
			run.AvgLatencyMS = &latency
			run.AnsweredCount = 112
			run.FinishedAt = &now
			run.Quality = service.ModelBenchmarkQuality{CompletionRate: 100, FormatComplianceRate: 100, RankingEligible: group.eligible, CategoryCoverage: map[string]float64{"math": 100}}
			require.NoError(t, repo.UpdateRun(ctx, run))
			if first == nil {
				first = run
			}
		}
	}
	rankings, err := repo.ListRankings(ctx, service.ModelBenchmarkSuiteID, service.ModelBenchmarkSuiteVersion, service.ModelBenchmarkScoringVer, 50)
	require.NoError(t, err)
	// 无 ranking_eligible/最少次数/批次/预算门槛：三个跑过的模型全部上榜，分数取最新一次而非 AVG。
	require.Len(t, rankings, 3)
	byModel := map[string]*service.ModelBenchmarkRanking{}
	for _, ranking := range rankings {
		byModel[ranking.ModelID] = ranking
	}
	require.Equal(t, 82.0, *byModel["candidate"].IntelligenceScore)
	require.Equal(t, int64(602), *byModel["candidate"].OutputTokens)
	require.Equal(t, 6, byModel["candidate"].RunCount)
	require.Equal(t, 3, byModel["other-form"].RunCount)
	require.Equal(t, 3, byModel["low-coverage"].RunCount)
	// 同分平局按 run_count DESC、model_id ASC 保持确定顺序。
	require.Equal(t, "candidate", rankings[0].ModelID)
	require.Equal(t, "low-coverage", rankings[1].ModelID)
	require.Equal(t, "other-form", rankings[2].ModelID)
	suites, err := repo.ListRankingSuites(ctx)
	require.NoError(t, err)
	require.Len(t, suites, 1)
	require.Equal(t, service.ModelBenchmarkSuiteID, suites[0].SuiteID)
	require.Equal(t, service.ModelBenchmarkSuiteVersion, suites[0].SuiteVersion)
	require.Equal(t, 12, suites[0].RunCount)
	// 历史题库：旧版本 run 一旦有分数即可按其题库三元组单独排名。
	_, err = db.ExecContext(ctx, `UPDATE model_benchmark_runs SET status='success', intelligence_score=70 WHERE id=1`)
	require.NoError(t, err)
	legacyRankings, err := repo.ListRankings(ctx, "hard-v1", "1.3.0", "1.1.0", 50)
	require.NoError(t, err)
	require.Len(t, legacyRankings, 1)
	require.Equal(t, "old-model", legacyRankings[0].ModelID)
	suites, err = repo.ListRankingSuites(ctx)
	require.NoError(t, err)
	require.Len(t, suites, 2)
	history, err := repo.ListHistory(ctx, service.ModelBenchmarkHistoryFilters{SuiteID: service.ModelBenchmarkSuiteID, SuiteVersion: service.ModelBenchmarkSuiteVersion, ScoringVersion: service.ModelBenchmarkScoringVer, ComparisonKey: "audit-batch", MaxOutputTokens: 2048, TimeoutSeconds: 90, SampleCount: 1, ModelID: "candidate", Limit: 100})
	require.NoError(t, err)
	require.Len(t, history, 3)
	require.Equal(t, 2048, history[0].MaxOutputTokens)
	runs, err := repo.ListRuns(ctx, service.ModelBenchmarkRunFilters{ComparisonKey: "audit-batch", MaxOutputTokens: 2048, TimeoutSeconds: 90, SampleCount: 1, ModelID: "candidate", Limit: 100})
	require.NoError(t, err)
	require.Len(t, runs, 3)
	require.True(t, runs[0].Quality.RankingEligible)
	item := service.GenerateModelBenchmarkComparisonItems("audit-batch", 1)[0]
	result := service.ModelBenchmarkScoreAnswer(item, `{"answer":0}`)
	result.RunID = first.ID
	result.UsageFieldCoverage = map[string]bool{}
	result.UsageSource = "missing"
	result.PricingSource = "missing"
	stored, err := repo.CreateItem(ctx, &result)
	require.NoError(t, err)
	require.InDelta(t, result.PossiblePoints, stored.PossiblePoints, 1e-9)
	restored, err := repo.GetRun(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "audit-batch", restored.ComparisonKey)
	require.Equal(t, 112, restored.ItemCount)
	require.Equal(t, 2048, restored.MaxOutputTokens)
}
