package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

// ModelBenchmarkRunnerService periodically claims and executes due benchmark plans.
type ModelBenchmarkRunnerService struct {
	repo      ModelBenchmarkRepository
	service   *ModelBenchmarkService
	cfg       *config.Config
	cron      *cron.Cron
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewModelBenchmarkRunnerService creates the benchmark runner.
func NewModelBenchmarkRunnerService(repo ModelBenchmarkRepository, benchmarkService *ModelBenchmarkService, cfg *config.Config) *ModelBenchmarkRunnerService {
	return &ModelBenchmarkRunnerService{repo: repo, service: benchmarkService, cfg: cfg}
}

// Start launches a one-minute ticker. Benchmark work is serial and globally capped at one plan.
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
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] started (tick=every minute, concurrency=1)")
	})
}

// Stop waits for the current serial run up to 30 seconds.
func (s *ModelBenchmarkRunnerService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
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
	time.Sleep(10 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	now := time.Now()
	if recovered, err := s.repo.ReleaseExpiredClaims(ctx, now); err != nil {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] release expired claims failed: %v", err)
	} else if recovered > 0 {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] recovered %d expired claims", recovered)
	}
	plans, err := s.repo.ClaimDuePlans(ctx, now, 30*time.Minute, 1)
	if err != nil {
		logger.LegacyPrintf("service.model_benchmark_runner", "[ModelBenchmarkRunner] claim failed: %v", err)
		return
	}
	if len(plans) == 0 {
		return
	}
	plan := plans[0]
	s.executeClaimedPlan(ctx, plan)
}
