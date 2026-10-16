//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// modelBenchmarkPlanRepoStub 是 QueueRun 语义测试用的最小仓储替身：只实现
// 服务在该路径上实际调用的方法，其余方法保持未实现（panic 即测试越界）。
type modelBenchmarkPlanRepoStub struct {
	ModelBenchmarkRepository
	plan      *ModelBenchmarkPlan
	persisted *ModelBenchmarkPlan
}

func (r *modelBenchmarkPlanRepoStub) GetPlan(_ context.Context, _ int64) (*ModelBenchmarkPlan, error) {
	return r.plan, nil
}

func (r *modelBenchmarkPlanRepoStub) UpdatePlan(_ context.Context, plan *ModelBenchmarkPlan) (*ModelBenchmarkPlan, error) {
	r.persisted = plan
	return plan, nil
}

func TestModelBenchmarkQueueRunAllowsDisabledPlanWithoutEnablingIt(t *testing.T) {
	plan := &ModelBenchmarkPlan{
		ID: 7, Name: "manual-once", ModelID: "glm-5.3",
		CronExpression: "0 4 * * 1", Enabled: false, RunningStatus: "idle",
	}
	repo := &modelBenchmarkPlanRepoStub{plan: plan}
	service := NewModelBenchmarkService(repo, nil, nil, nil, nil)

	updated, err := service.QueueRun(context.Background(), plan.ID)
	require.NoError(t, err, "停用计划必须允许手动执行一次")
	require.NotNil(t, updated.ManualTriggerAt, "必须写入一次性手动触发标记")
	require.False(t, updated.Enabled, "手动执行不得顺带启用计划")
	require.Nil(t, updated.NextRunAt, "停用计划不应写入 cron 的 next_run_at")
	require.Same(t, repo.persisted, updated, "必须经仓储持久化")
}

func TestModelBenchmarkQueueRunKeepsEnabledPlanSemantics(t *testing.T) {
	plan := &ModelBenchmarkPlan{
		ID: 8, Name: "weekly", ModelID: "glm-5.3",
		CronExpression: "0 4 * * 1", Enabled: true, RunningStatus: "idle",
	}
	repo := &modelBenchmarkPlanRepoStub{plan: plan}
	service := NewModelBenchmarkService(repo, nil, nil, nil, nil)

	before := time.Now()
	updated, err := service.QueueRun(context.Background(), plan.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.ManualTriggerAt)
	// 启用计划保持既有「启用即尽快执行」语义：next_run_at 对齐到当前时间附近
	require.NotNil(t, updated.NextRunAt)
	require.False(t, updated.NextRunAt.Before(before.Add(-time.Second)))
	require.True(t, updated.Enabled)
}
