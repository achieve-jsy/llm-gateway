package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
)

// channelMonitorRunner manages per-monitor goroutines with a ticker.
type channelMonitorRunner struct {
	service *ChannelMonitorService
	mu      sync.Mutex
	entries map[int64]*runnerEntry
	pool    chan struct{}
}

type runnerEntry struct {
	view   *ChannelMonitorView
	cancel context.CancelFunc
}

// NewChannelMonitorRunner creates a new runner instance.
func NewChannelMonitorRunner(svc *ChannelMonitorService) *channelMonitorRunner {
	return &channelMonitorRunner{
		service: svc,
		entries: make(map[int64]*runnerEntry),
		pool:    make(chan struct{}, monitorWorkerConcurrency),
	}
}

// Schedule starts a goroutine for the given monitor. If one already exists, it is restarted.
func (r *channelMonitorRunner) Schedule(view *ChannelMonitorView) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.entries[view.ID]; ok {
		existing.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.entries[view.ID] = &runnerEntry{view: view, cancel: cancel}
	go r.loop(ctx, view)
	slog.Info("channel_monitor: scheduled", "id", view.ID, "name", view.Name, "interval", view.IntervalSeconds)
}

// Unschedule stops the goroutine for the given monitor.
func (r *channelMonitorRunner) Unschedule(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, ok := r.entries[id]; ok {
		entry.cancel()
		delete(r.entries, id)
		slog.Info("channel_monitor: unscheduled", "id", id)
	}
}

// StartAll loads all enabled monitors and starts goroutines for them.
func (r *channelMonitorRunner) StartAll(ctx context.Context) {
	monitors, err := r.service.repo.ListEnabled()
	if err != nil {
		slog.Error("channel_monitor: failed to load enabled monitors", "error", err)
		return
	}

	for _, m := range monitors {
		apiKey, err := crypto.Decrypt(m.APIKeyEncrypted)
		if err != nil {
			slog.Warn("channel_monitor: skip monitor with corrupt api key", "id", m.ID, "name", m.Name)
			continue
		}
		view := modelToView(m, apiKey)
		r.Schedule(view)
	}

	slog.Info("channel_monitor: runner started", "monitors", len(monitors))
}

// StopAll stops all goroutines.
func (r *channelMonitorRunner) StopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for id, entry := range r.entries {
		entry.cancel()
		delete(r.entries, id)
	}
	slog.Info("channel_monitor: runner stopped")
}

func (r *channelMonitorRunner) loop(ctx context.Context, view *ChannelMonitorView) {
	interval := time.Duration(view.IntervalSeconds) * time.Second
	if interval < time.Duration(monitorMinIntervalSeconds)*time.Second {
		interval = time.Duration(monitorMinIntervalSeconds) * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run first check after a short buffer
	time.AfterFunc(monitorRunOneBuffer, func() {
		r.runOne(ctx, view)
	})

	for {
		select {
		case <-ctx.Done():
			slog.Info("channel_monitor: loop stopped", "id", view.ID, "name", view.Name)
			return
		case <-ticker.C:
			r.runOne(ctx, view)
		}
	}
}

func (r *channelMonitorRunner) runOne(ctx context.Context, view *ChannelMonitorView) {
	select {
	case r.pool <- struct{}{}:
		defer func() { <-r.pool }()
	case <-ctx.Done():
		return
	}

	checkCtx, cancel := context.WithTimeout(ctx, monitorRequestTimeout+monitorRunOneBuffer)
	defer cancel()

	_, err := r.service.RunCheck(checkCtx, view.ID)
	if err != nil {
		slog.Error("channel_monitor: check failed", "id", view.ID, "name", view.Name, "error", err)
	}
}

// Ensure channelMonitorRunner implements MonitorScheduler
var _ MonitorScheduler = (*channelMonitorRunner)(nil)