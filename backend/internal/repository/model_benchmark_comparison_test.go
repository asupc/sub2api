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
	migration, err := migrations.FS.ReadFile("245_so_model_benchmark_comparison.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, "new migration must be idempotent")
	}
	var legacy string
	require.NoError(t, db.QueryRowContext(ctx, "SELECT comparison_key FROM model_benchmark_runs WHERE id=1").Scan(&legacy))
	require.Equal(t, "legacy", legacy)
	repo := NewModelBenchmarkRepository(db)
	plan, err := repo.CreatePlan(ctx, &service.ModelBenchmarkPlan{Name: "fixture", SuiteID: service.ModelBenchmarkSuiteID, SuiteVersion: service.ModelBenchmarkSuiteVersion, ScoringVersion: service.ModelBenchmarkScoringVer, ModelID: "candidate", AccountID: 1, CronExpression: "0 4 * * 1", SampleCount: 3, MaxOutputTokens: 2048, TimeoutSeconds: 600, MaxResults: 50, ComparisonKey: "before-edit"})
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
			run, err := repo.CreateRun(ctx, &service.ModelBenchmarkRun{PlanID: plan.ID, SuiteID: service.ModelBenchmarkSuiteID, SuiteVersion: service.ModelBenchmarkSuiteVersion, ScoringVersion: service.ModelBenchmarkScoringVer, ModelID: group.model, AccountID: 1, ComparisonKey: group.key, MaxOutputTokens: group.tokens, TimeoutSeconds: 90, SampleCount: 1, ItemCount: 112, StartedAt: now})
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
	rankings, err := repo.ListRankings(ctx, service.ModelBenchmarkSuiteID, service.ModelBenchmarkSuiteVersion, service.ModelBenchmarkScoringVer, "audit-batch", 50, 2048, 90, 1)
	require.NoError(t, err)
	require.Len(t, rankings, 1)
	require.Equal(t, "candidate", rankings[0].ModelID)
	require.Equal(t, 3, rankings[0].RunCount)
	require.Equal(t, 81.0, *rankings[0].IntelligenceScore)
	require.Equal(t, 1.0, *rankings[0].ScoreStddev)
	require.Equal(t, int64(601), *rankings[0].OutputTokens)
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
