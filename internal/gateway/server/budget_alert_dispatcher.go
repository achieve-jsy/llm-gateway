package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type BudgetAlertDispatcherConfig struct {
	QueueSize      int
	Workers        int
	RequestTimeout time.Duration
}

type BudgetAlertDispatcher struct {
	client    *BudgetAlertClient
	cfg       BudgetAlertDispatcherConfig
	queue     chan BudgetAlertRequest
	queueFull metric.Int64Counter
	mu        sync.Mutex
	closed    bool
	workers   sync.WaitGroup
	done      chan struct{}
}

func NewBudgetAlertDispatcher(
	ctx context.Context,
	client *BudgetAlertClient,
	cfg BudgetAlertDispatcherConfig,
) *BudgetAlertDispatcher {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = 3 * time.Second
	}
	queueFull, _ := otel.Meter("llm-gateway/budget").Int64Counter("gateway_budget_alert_queue_full_total")
	dispatcher := &BudgetAlertDispatcher{
		client: client, cfg: cfg, queue: make(chan BudgetAlertRequest, cfg.QueueSize),
		queueFull: queueFull, done: make(chan struct{}),
	}
	dispatcher.workers.Add(cfg.Workers)
	for range cfg.Workers {
		go dispatcher.worker()
	}
	go func() {
		<-ctx.Done()
		dispatcher.close()
		dispatcher.workers.Wait()
		close(dispatcher.done)
	}()
	return dispatcher
}

func (d *BudgetAlertDispatcher) Enqueue(request BudgetAlertRequest) bool {
	if d == nil || request.ScopeType == "" || request.ScopeID == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	select {
	case d.queue <- request:
		return true
	default:
		if d.queueFull != nil {
			d.queueFull.Add(context.Background(), 1)
		}
		slog.Error("budget alert queue full",
			"scope_type", request.ScopeType,
			"scope_id", request.ScopeID,
			"queue_capacity", cap(d.queue),
		)
		return false
	}
}

func (d *BudgetAlertDispatcher) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	d.closed = true
	close(d.queue)
}

func (d *BudgetAlertDispatcher) worker() {
	defer d.workers.Done()
	for request := range d.queue {
		ctx, cancel := context.WithTimeout(context.Background(), d.cfg.RequestTimeout)
		err := d.client.Send(ctx, request)
		cancel()
		if err != nil {
			slog.Warn("budget alert delivery failed",
				"scope_type", request.ScopeType,
				"scope_id", request.ScopeID,
				"dimension_type", request.DimensionType,
				"model_tier_id", request.ModelTierID,
				"error", err,
			)
		}
	}
}

func (d *BudgetAlertDispatcher) Wait(ctx context.Context) error {
	if d == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-d.done:
		return nil
	}
}
