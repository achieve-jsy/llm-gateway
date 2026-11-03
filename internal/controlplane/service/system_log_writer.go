package service

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// defaultSystemLogBufferSize is the default channel buffer size for the async writer.
const defaultSystemLogBufferSize = 4096

// defaultSystemLogBatchSize is the max batch size per flush.
const defaultSystemLogBatchSize = 200

// defaultSystemLogFlushInterval is the ticker interval for periodic flush.
const defaultSystemLogFlushInterval = 1 * time.Second

// SystemLogWriter is an async batch writer for system_logs.
// It receives log entries via a buffered channel and flushes them to DB in batches.
type SystemLogWriter struct {
	db                *gorm.DB
	queue             chan *model.SystemLog
	batchSize         int
	flushInterval     time.Duration
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	droppedCount      atomic.Int64
	writeFailures     atomic.Int64
	writtenCount      atomic.Int64
	totalDelayNs      atomic.Int64
	queueDepthBuckets [16]int64 // atomic snapshot of queue depth for health
	lastError         atomic.Value
	running           atomic.Bool
}

// NewSystemLogWriter creates a new SystemLogWriter.
func NewSystemLogWriter(db *gorm.DB) *SystemLogWriter {
	w := NewSystemLogWriterWithConfig(db, defaultSystemLogBufferSize, defaultSystemLogBatchSize, defaultSystemLogFlushInterval)
	return w
}

// NewSystemLogWriterWithConfig creates a SystemLogWriter with custom configuration.
// Useful for tests to control flush timing.
func NewSystemLogWriterWithConfig(db *gorm.DB, bufferSize, batchSize int, flushInterval time.Duration) *SystemLogWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &SystemLogWriter{
		db:            db,
		queue:         make(chan *model.SystemLog, bufferSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		ctx:           ctx,
		cancel:        cancel,
	}
	w.lastError.Store("")
	return w
}

// Start begins the background flush goroutine.
func (w *SystemLogWriter) Start() {
	if w == nil || w.db == nil {
		return
	}
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	w.wg.Add(1)
	go w.run()
}

// Stop gracefully shuts down the writer, draining pending entries.
func (w *SystemLogWriter) Stop() {
	if w == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	w.running.Store(false)
}

// Write enqueues a system_log entry for async write.
// Non-blocking: drops the entry and increments droppedCount if the queue is full.
func (w *SystemLogWriter) Write(ctx context.Context, entry *model.SystemLog) {
	if w == nil || entry == nil {
		return
	}
	if w.ctx != nil {
		select {
		case <-w.ctx.Done():
			return
		default:
		}
	}
	select {
	case w.queue <- entry:
	default:
		w.droppedCount.Add(1)
	}
}

// QueueDepth returns the current number of pending entries in the channel.
func (w *SystemLogWriter) QueueDepth() int {
	if w == nil {
		return 0
	}
	return len(w.queue)
}

// QueueCapacity returns the channel buffer capacity.
func (w *SystemLogWriter) QueueCapacity() int {
	if w == nil {
		return 0
	}
	return cap(w.queue)
}

// DroppedCount returns the number of entries dropped due to full queue.
func (w *SystemLogWriter) DroppedCount() int64 {
	if w == nil {
		return 0
	}
	return w.droppedCount.Load()
}

// WriteFailures returns the number of entries that failed to write to DB.
func (w *SystemLogWriter) WriteFailures() int64 {
	if w == nil {
		return 0
	}
	return w.writeFailures.Load()
}

// WrittenCount returns the number of entries successfully written to DB.
func (w *SystemLogWriter) WrittenCount() int64 {
	if w == nil {
		return 0
	}
	return w.writtenCount.Load()
}

// AvgWriteDelay returns the average write delay in nanoseconds.
func (w *SystemLogWriter) AvgWriteDelay() time.Duration {
	if w == nil {
		return 0
	}
	written := w.writtenCount.Load()
	if written == 0 {
		return 0
	}
	return time.Duration(w.totalDelayNs.Load() / written)
}

// LastError returns the last write error message.
func (w *SystemLogWriter) LastError() string {
	if w == nil {
		return ""
	}
	v, _ := w.lastError.Load().(string)
	return v
}

// IsRunning returns true if the writer goroutine is active.
func (w *SystemLogWriter) IsRunning() bool {
	if w == nil {
		return false
	}
	return w.running.Load()
}

func (w *SystemLogWriter) run() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]*model.SystemLog, 0, w.batchSize)

	flush := func(baseCtx context.Context) {
		if len(batch) == 0 {
			return
		}
		start := time.Now()
		if err := w.flushBatch(baseCtx, batch); err != nil {
			w.writeFailures.Add(int64(len(batch)))
			w.lastError.Store(err.Error())
			slog.Warn("system_log_writer: flush failed",
				"batch_size", len(batch),
				"error", err,
			)
		} else {
			w.writtenCount.Add(int64(len(batch)))
			w.totalDelayNs.Add(time.Since(start).Nanoseconds())
			w.lastError.Store("")
		}
		batch = batch[:0]
	}

	drainAndFlush := func() {
		for {
			select {
			case item := <-w.queue:
				if item == nil {
					continue
				}
				batch = append(batch, item)
				if len(batch) >= w.batchSize {
					flush(context.Background())
				}
			default:
				flush(context.Background())
				return
			}
		}
	}

	for {
		select {
		case <-w.ctx.Done():
			drainAndFlush()
			return
		case item := <-w.queue:
			if item == nil {
				continue
			}
			batch = append(batch, item)
			if len(batch) >= w.batchSize {
				flush(w.ctx)
			}
		case <-ticker.C:
			flush(w.ctx)
		}
	}
}

func (w *SystemLogWriter) flushBatch(ctx context.Context, batch []*model.SystemLog) error {
	if len(batch) == 0 {
		return nil
	}
	// Use a timeout context for DB writes
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return w.db.WithContext(dbCtx).CreateInBatches(batch, 100).Error
}

// ── Global accessor for sink-health ──

var globalSystemLogWriter *SystemLogWriter
var globalWriterMu sync.RWMutex

// SetSystemLogWriter sets the global system log writer instance.
func SetSystemLogWriter(w *SystemLogWriter) {
	globalWriterMu.Lock()
	defer globalWriterMu.Unlock()
	globalSystemLogWriter = w
}

// GetSystemLogWriter returns the global system log writer instance.
func GetSystemLogWriter() *SystemLogWriter {
	globalWriterMu.RLock()
	defer globalWriterMu.RUnlock()
	return globalSystemLogWriter
}