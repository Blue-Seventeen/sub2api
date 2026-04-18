package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/robfig/cron/v3"
)

var accountAutoOpsRunnerParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

type AccountAutoOpsRunnerService struct {
	autoOpsService *AccountAutoOpsService
	cfg            *config.Config

	cron     *cron.Cron
	started  bool
	stopOnce bool
}

func NewAccountAutoOpsRunnerService(autoOpsService *AccountAutoOpsService, cfg *config.Config) *AccountAutoOpsRunnerService {
	return &AccountAutoOpsRunnerService{
		autoOpsService: autoOpsService,
		cfg:            cfg,
	}
}

func (s *AccountAutoOpsRunnerService) Start() {
	if s == nil || s.autoOpsService == nil || s.started {
		return
	}
	loc := time.Local
	if s.cfg != nil && s.cfg.Timezone != "" {
		if parsed, err := time.LoadLocation(s.cfg.Timezone); err == nil && parsed != nil {
			loc = parsed
		}
	}
	c := cron.New(cron.WithParser(accountAutoOpsRunnerParser), cron.WithLocation(loc))
	_, err := c.AddFunc("* * * * *", func() { s.tick() })
	if err != nil {
		logger.LegacyPrintf("service.account_auto_ops_runner", "[AccountAutoOpsRunner] not started: %v", err)
		return
	}
	s.cron = c
	s.cron.Start()
	s.started = true
	logger.LegacyPrintf("service.account_auto_ops_runner", "[AccountAutoOpsRunner] started (tick=every minute)")
}

func (s *AccountAutoOpsRunnerService) Stop() {
	if s == nil || !s.started || s.stopOnce {
		return
	}
	s.stopOnce = true
	if s.cron != nil {
		ctx := s.cron.Stop()
		select {
		case <-ctx.Done():
		case <-time.After(3 * time.Second):
			logger.LegacyPrintf("service.account_auto_ops_runner", "[AccountAutoOpsRunner] cron stop timed out")
		}
	}
}

func (s *AccountAutoOpsRunnerService) tick() {
	if s == nil || s.autoOpsService == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cfg, err := s.autoOpsService.GetConfig(ctx)
	if err != nil {
		logger.LegacyPrintf("service.account_auto_ops_runner", "[AccountAutoOpsRunner] load config failed: %v", err)
		return
	}
	if cfg == nil || !cfg.Enabled || !cfg.Configured {
		return
	}

	latestRunAt, err := s.autoOpsService.GetLatestAutomaticRunAt(ctx)
	if err != nil {
		logger.LegacyPrintf("service.account_auto_ops_runner", "[AccountAutoOpsRunner] load latest run failed: %v", err)
		return
	}
	if latestRunAt != nil && time.Since(*latestRunAt) < time.Duration(cfg.IntervalMinutes)*time.Minute {
		return
	}

	if _, err := s.autoOpsService.RunAutomatic(ctx); err != nil {
		logger.LegacyPrintf("service.account_auto_ops_runner", "[AccountAutoOpsRunner] run failed: %v", err)
	}
}
