package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// PromotionSettlementRunnerService periodically processes activation checks,
// daily commission aggregation and automatic settlements.
type PromotionSettlementRunnerService struct {
	promotionService *PromotionService
	cfg              *config.Config

	ticker   *time.Ticker
	started  bool
	stopOnce sync.Once
	done     chan struct{}
}

func NewPromotionSettlementRunnerService(promotionService *PromotionService, cfg *config.Config) *PromotionSettlementRunnerService {
	return &PromotionSettlementRunnerService{
		promotionService: promotionService,
		cfg:              cfg,
		done:             make(chan struct{}),
	}
}

func (s *PromotionSettlementRunnerService) Start() {
	if s == nil || s.promotionService == nil || s.started {
		return
	}
	s.started = true
	s.ticker = time.NewTicker(time.Minute)
	go s.loop()
	logger.LegacyPrintf("service.promotion_runner", "[PromotionRunner] started (tick=every minute)")
}

func (s *PromotionSettlementRunnerService) Stop() {
	if s == nil || !s.started {
		return
	}
	s.stopOnce.Do(func() {
		if s.ticker != nil {
			s.ticker.Stop()
		}
		close(s.done)
	})
}

func (s *PromotionSettlementRunnerService) loop() {
	s.tick()
	for {
		select {
		case <-s.done:
			return
		case <-s.ticker.C:
			s.tick()
		}
	}
}

func (s *PromotionSettlementRunnerService) tick() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.promotionService.ProcessSettlementTick(ctx, time.Now()); err != nil {
		logger.LegacyPrintf("service.promotion_runner", "[PromotionRunner] tick failed: %v", err)
	}
}
