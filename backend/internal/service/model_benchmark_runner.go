package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

// modelBenchmarkRunnerTickDelay 是每个 tick 领取前的固定延迟，让手动触发请求先落库；测试中可缩短。
var modelBenchmarkRunnerTickDelay = 10 * time.Second

// ModelBenchmarkRunnerService periodically claims and executes due benchmark plans.
type ModelBenchmarkRunnerService struct {
	repo      ModelBenchmarkRepository
	service   *ModelBenchmarkService
	cfg       *config.Config
	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once
	runningMu sync.Mutex
	lifetime  context.Context
	cancel    context.CancelFunc
}

// NewModelBenchmarkRunnerService creates the benchmark runner.
func NewModelBenchmarkRunnerService(repo ModelBenchmarkRepository, benchmarkService *ModelBenchmarkService, cfg *config.Config) *ModelBenchmarkRunnerService {
	ctx, cancel := context.WithCancel(context.Background())
	return &ModelBenchmarkRunnerService{repo: repo, service: benchmarkService, cfg: cfg, lifetime: ctx, cancel: cancel}
}

// Start launches a one-minute ticker. All due plans are claimed and executed in parallel each tick.
func (s *ModelBenchmarkRunnerService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		location := time.Local
		if s.cfg != nil {
			if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
				location = parsed
			}
		}
		c := cron.New(cron.WithParser(scheduledTestCronParser), cron.WithLocation(location))
		if _, err := c.AddFunc("* * * * *", func() { s.runScheduled() }); err != nil {
			logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] not started: %v", err)
			return
		}
		s.cron = c
		s.cron.Start()
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] started (tick=every minute, due plans run in parallel)")
	})
}

// Stop waits for in-flight runs up to 30 seconds.
func (s *ModelBenchmarkRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.cron == nil {
			return
		}
		ctx := s.cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Second):
			logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] stop timed out")
		}
	})
}

func (s *ModelBenchmarkRunnerService) executeClaimedPlan(ctx context.Context, plan *ModelBenchmarkPlan) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] plan=%d panicked: %v", plan.ID, recovered)
			now := time.Now()
			if next, err := computeNextRun(plan.CronExpression, now); err == nil {
				_ = s.repo.CompletePlan(context.WithoutCancel(ctx), plan.ID, now, next)
			}
		}
	}()
	if err := s.service.ExecutePlan(ctx, plan); err != nil {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] plan=%d failed: %v", plan.ID, err)
		now := time.Now()
		if next, cronErr := computeNextRun(plan.CronExpression, now); cronErr == nil {
			_ = s.repo.CompletePlan(context.WithoutCancel(ctx), plan.ID, now, next)
		}
	}
}

func (s *ModelBenchmarkRunnerService) runScheduled() {
	if !s.runningMu.TryLock() {
		return
	}
	locked := true
	defer func() {
		if locked {
			s.runningMu.Unlock()
		}
	}()

	time.Sleep(modelBenchmarkRunnerTickDelay)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	now := time.Now()
	if recovered, err := s.repo.ReleaseExpiredClaims(ctx, now); err != nil {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] release expired claims failed: %v", err)
	} else if recovered > 0 {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] recovered %d expired claims", recovered)
	}
	// 领取本次 tick 到期的全部 plan 并行执行；limit 仅作防御性上限，不构成日常约束。
	plans, err := s.repo.ClaimDuePlans(ctx, now, 30*time.Minute, 100)
	if err != nil {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] claim failed: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}
	s.runningMu.Unlock()
	locked = false
	var wg sync.WaitGroup
	for _, plan := range plans {
		wg.Add(1)
		go func(plan *ModelBenchmarkPlan) {
			defer wg.Done()
			// 每个 plan 独立占用完整执行预算，不因并行共享而缩水。
			parent := s.lifetime
			if parent == nil {
				parent = context.Background()
			}
			runCtx, runCancel := context.WithTimeout(parent, ModelBenchmarkRunBudget(plan))
			defer runCancel()
			s.executeClaimedPlan(runCtx, plan)
		}(plan)
	}
	wg.Wait()
}
