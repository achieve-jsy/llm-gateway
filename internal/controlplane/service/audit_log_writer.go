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

const (
	defaultAuditLogBufferSize    = 4096
	defaultAuditLogBatchSize     = 200
	defaultAuditLogFlushInterval = 1 * time.Second
	auditLogDropLogIntervalSec   = 10 // 队列满丢弃时，warn 日志的最小间隔（秒），避免刷屏
)

// AuditLogWriter 是 audit_logs 的异步批量写器。
// 通过有界 channel 接收审计日志，后台 goroutine 批量落库，写失败不阻断业务。
// 队列满时丢弃并计数（审计是 best-effort，宁可丢也不阻塞请求路径）。
type AuditLogWriter struct {
	db             *gorm.DB
	queue          chan *model.AuditLog
	batchSize      int
	flushInterval  time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	droppedCount   atomic.Int64
	writeFailures  atomic.Int64
	writtenCount   atomic.Int64
	lastError      atomic.Value
	running        atomic.Bool
	lastDropLogSec atomic.Int64
}

// NewAuditLogWriter 创建审计日志异步写器（使用默认配置）。
func NewAuditLogWriter(db *gorm.DB) *AuditLogWriter {
	return NewAuditLogWriterWithConfig(db, defaultAuditLogBufferSize, defaultAuditLogBatchSize, defaultAuditLogFlushInterval)
}

// NewAuditLogWriterWithConfig 创建带自定义配置的写器（测试用）。
func NewAuditLogWriterWithConfig(db *gorm.DB, bufferSize, batchSize int, flushInterval time.Duration) *AuditLogWriter {
	ctx, cancel := context.WithCancel(context.Background())
	w := &AuditLogWriter{
		db:            db,
		queue:         make(chan *model.AuditLog, bufferSize),
		batchSize:     batchSize,
		flushInterval: flushInterval,
		ctx:           ctx,
		cancel:        cancel,
	}
	w.lastError.Store("")
	return w
}

// Start 启动后台 flush goroutine。
func (w *AuditLogWriter) Start() {
	if w == nil || w.db == nil {
		return
	}
	if !w.running.CompareAndSwap(false, true) {
		return
	}
	w.wg.Add(1)
	go w.run()
}

// Stop 优雅停机，排空待写队列。
func (w *AuditLogWriter) Stop() {
	if w == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
	w.running.Store(false)
}

// Write 入队一条审计日志（非阻塞，队列满则丢弃并计数）。
func (w *AuditLogWriter) Write(entry *model.AuditLog) {
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
		recordAuditLogDropped(context.Background())
		w.logDropThrottled()
	}
}

// logDropThrottled 队列满丢弃时打 warn 日志：每 10 秒最多一条，避免高负载刷屏。
// 丢弃计数与 metric 每次都记录，这里只节流日志输出。
func (w *AuditLogWriter) logDropThrottled() {
	now := time.Now().Unix()
	if last := w.lastDropLogSec.Load(); now-last >= auditLogDropLogIntervalSec {
		if w.lastDropLogSec.CompareAndSwap(last, now) {
			slog.Warn("audit_log_writer: queue full, audit log dropped", "dropped_total", w.droppedCount.Load())
		}
	}
}

// DroppedCount 返回因队列满而丢弃的条数。
func (w *AuditLogWriter) DroppedCount() int64 {
	if w == nil {
		return 0
	}
	return w.droppedCount.Load()
}

// WriteFailures 返回落库失败的条数。
func (w *AuditLogWriter) WriteFailures() int64 {
	if w == nil {
		return 0
	}
	return w.writeFailures.Load()
}

// WrittenCount 返回成功落库条数。
func (w *AuditLogWriter) WrittenCount() int64 {
	if w == nil {
		return 0
	}
	return w.writtenCount.Load()
}

func (w *AuditLogWriter) run() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]*model.AuditLog, 0, w.batchSize)

	flush := func(ctx context.Context) {
		if len(batch) == 0 {
			return
		}
		n := len(batch)
		if err := w.flushBatch(ctx, batch); err != nil {
			w.writeFailures.Add(int64(n))
			w.lastError.Store(err.Error())
			recordAuditLogWriteFailed(ctx, n)
			slog.Warn("audit_log_writer: flush failed", "batch_size", n, "error", err)
		} else {
			w.writtenCount.Add(int64(n))
			w.lastError.Store("")
			recordAuditLogWritten(ctx, n)
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

func (w *AuditLogWriter) flushBatch(ctx context.Context, batch []*model.AuditLog) error {
	if len(batch) == 0 {
		return nil
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return w.db.WithContext(dbCtx).CreateInBatches(batch, 100).Error
}
