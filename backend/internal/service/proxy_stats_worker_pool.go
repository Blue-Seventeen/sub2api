package service

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/alitto/pond/v2"
	"go.uber.org/zap"
)

const (
	defaultProxyStatsWorkerCount           = 32
	defaultProxyStatsQueueSize             = 4096
	defaultProxyStatsTaskTimeout           = 3 * time.Second
	proxyStatsWorkerDropLogInterval        = 5 * time.Second
	proxyStatsSubmitModeEnqueued    string = "enqueued"
	proxyStatsSubmitModeDropped     string = "dropped"
)

type ProxyStatsTask func(ctx context.Context)

type ProxyStatsWorkerPoolOptions struct {
	WorkerCount int
	QueueSize   int
	TaskTimeout time.Duration
}

type ProxyStatsWorkerPoolStats struct {
	MaxConcurrency   int
	RunningWorkers   int64
	WaitingTasks     uint64
	SubmittedTasks   uint64
	CompletedTasks   uint64
	SuccessfulTasks  uint64
	FailedTasks      uint64
	DroppedTasks     uint64
	DroppedQueueFull uint64
	DroppedStopped   uint64
}

// ProxyStatsWorkerPool is a best-effort async writer for proxy request stats.
// It never falls back to synchronous execution on the caller path.
type ProxyStatsWorkerPool struct {
	pool             pond.Pool
	taskTimeout      time.Duration
	droppedQueueFull atomic.Uint64
	droppedStopped   atomic.Uint64
	lastDropLogNanos atomic.Int64
}

func NewProxyStatsWorkerPool() *ProxyStatsWorkerPool {
	return NewProxyStatsWorkerPoolWithOptions(ProxyStatsWorkerPoolOptions{})
}

func NewProxyStatsWorkerPoolWithOptions(opts ProxyStatsWorkerPoolOptions) *ProxyStatsWorkerPool {
	opts = normalizeProxyStatsWorkerPoolOptions(opts)
	return &ProxyStatsWorkerPool{
		pool: pond.NewPool(
			opts.WorkerCount,
			pond.WithQueueSize(opts.QueueSize),
		),
		taskTimeout: opts.TaskTimeout,
	}
}

func (p *ProxyStatsWorkerPool) Submit(task ProxyStatsTask) string {
	if p == nil || task == nil {
		return proxyStatsSubmitModeDropped
	}
	if p.pool == nil || p.pool.Stopped() {
		p.droppedStopped.Add(1)
		p.logDrop("stopped")
		return proxyStatsSubmitModeDropped
	}
	_, ok := p.pool.TrySubmit(func() {
		p.execute(task)
	})
	if ok {
		return proxyStatsSubmitModeEnqueued
	}
	if p.pool.Stopped() {
		p.droppedStopped.Add(1)
		p.logDrop("stopped")
		return proxyStatsSubmitModeDropped
	}
	p.droppedQueueFull.Add(1)
	p.logDrop("full")
	return proxyStatsSubmitModeDropped
}

func (p *ProxyStatsWorkerPool) Stats() ProxyStatsWorkerPoolStats {
	if p == nil || p.pool == nil {
		return ProxyStatsWorkerPoolStats{}
	}
	return ProxyStatsWorkerPoolStats{
		MaxConcurrency:   p.pool.MaxConcurrency(),
		RunningWorkers:   p.pool.RunningWorkers(),
		WaitingTasks:     p.pool.WaitingTasks(),
		SubmittedTasks:   p.pool.SubmittedTasks(),
		CompletedTasks:   p.pool.CompletedTasks(),
		SuccessfulTasks:  p.pool.SuccessfulTasks(),
		FailedTasks:      p.pool.FailedTasks(),
		DroppedTasks:     p.pool.DroppedTasks(),
		DroppedQueueFull: p.droppedQueueFull.Load(),
		DroppedStopped:   p.droppedStopped.Load(),
	}
}

func (p *ProxyStatsWorkerPool) Stop() {
	if p == nil || p.pool == nil {
		return
	}
	p.pool.StopAndWait()
}

func (p *ProxyStatsWorkerPool) execute(task ProxyStatsTask) {
	ctx, cancel := context.WithTimeout(context.Background(), p.taskTimeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.L().With(
				zap.String("component", "service.proxy_stats_worker_pool"),
				zap.Any("panic", recovered),
			).Error("proxy_stats.task_panic")
		}
	}()
	task(ctx)
}

func (p *ProxyStatsWorkerPool) logDrop(reason string) {
	now := time.Now().UnixNano()
	last := p.lastDropLogNanos.Load()
	if now-last < int64(proxyStatsWorkerDropLogInterval) {
		return
	}
	if !p.lastDropLogNanos.CompareAndSwap(last, now) {
		return
	}
	stats := p.Stats()
	logger.L().With(
		zap.String("component", "service.proxy_stats_worker_pool"),
		zap.String("reason", reason),
		zap.Int64("running_workers", stats.RunningWorkers),
		zap.Uint64("waiting_tasks", stats.WaitingTasks),
		zap.Uint64("dropped_queue_full", stats.DroppedQueueFull),
		zap.Uint64("dropped_stopped", stats.DroppedStopped),
	).Warn("proxy_stats.task_dropped")
}

func normalizeProxyStatsWorkerPoolOptions(opts ProxyStatsWorkerPoolOptions) ProxyStatsWorkerPoolOptions {
	if opts.WorkerCount <= 0 {
		opts.WorkerCount = defaultProxyStatsWorkerCount
	}
	if opts.QueueSize <= 0 {
		opts.QueueSize = defaultProxyStatsQueueSize
	}
	if opts.TaskTimeout <= 0 {
		opts.TaskTimeout = defaultProxyStatsTaskTimeout
	}
	return opts
}
