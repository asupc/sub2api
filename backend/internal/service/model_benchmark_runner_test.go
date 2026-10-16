//go:build unit

package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// modelBenchmarkRunnerRepoStub 是 runner 并行语义测试的最小仓储替身：只实现
// runScheduled 路径实际调用的方法，其余方法保持未实现（panic 即测试越界）。
// accounts/executor 为 nil，ExecutePlan 走 panic-recover 兜底后仍会 CompletePlan，
// 足以验证「领取全部 due plan → 全部执行」的核心语义。
type modelBenchmarkRunnerRepoStub struct {
	ModelBenchmarkRepository
	mu         sync.Mutex
	claimLimit int
	plans      []*ModelBenchmarkPlan
	completed  map[int64]int
}

func (r *modelBenchmarkRunnerRepoStub) ReleaseExpiredClaims(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (r *modelBenchmarkRunnerRepoStub) ClaimDuePlans(_ context.Context, _ time.Time, _ time.Duration, limit int) ([]*ModelBenchmarkPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimLimit = limit
	return r.plans, nil
}

func (r *modelBenchmarkRunnerRepoStub) CompletePlan(_ context.Context, id int64, _, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.completed[id]++
	return nil
}

func TestModelBenchmarkRunnerExecutesAllDuePlansInParallel(t *testing.T) {
	originalDelay := modelBenchmarkRunnerTickDelay
	modelBenchmarkRunnerTickDelay = time.Millisecond
	t.Cleanup(func() { modelBenchmarkRunnerTickDelay = originalDelay })

	repo := &modelBenchmarkRunnerRepoStub{plans: []*ModelBenchmarkPlan{
		{ID: 11, ModelID: "glm-5.3", CronExpression: "0 4 * * 1"},
		{ID: 12, ModelID: "claude-opus-5-5", CronExpression: "0 4 * * 1"},
		{ID: 13, ModelID: "claude-sonnet-5-5", CronExpression: "0 4 * * 1"},
	}, completed: map[int64]int{}}
	runner := NewModelBenchmarkRunnerService(repo, NewModelBenchmarkService(repo, nil, nil, nil, nil), nil)

	runner.runScheduled()

	require.GreaterOrEqual(t, repo.claimLimit, len(repo.plans), "领取上限必须覆盖全部 due plan，不得回落到串行时代的 1")
	for _, plan := range repo.plans {
		require.Equal(t, 1, repo.completed[plan.ID], "每个 due plan 都必须被执行且仅执行一次: plan=%d", plan.ID)
	}
}

func TestModelBenchmarkRunnerSkipsWhenNoDuePlans(t *testing.T) {
	originalDelay := modelBenchmarkRunnerTickDelay
	modelBenchmarkRunnerTickDelay = time.Millisecond
	t.Cleanup(func() { modelBenchmarkRunnerTickDelay = originalDelay })

	repo := &modelBenchmarkRunnerRepoStub{completed: map[int64]int{}}
	runner := NewModelBenchmarkRunnerService(repo, NewModelBenchmarkService(repo, nil, nil, nil, nil), nil)

	runner.runScheduled()

	require.Empty(t, repo.completed, "无 due plan 时不得产生任何执行")
}
