package repository

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/example/llm-gateway/internal/gateway/observability/model"
)

// CallLogRepository call_logs 表持久化操作
type CallLogRepository struct {
	db *gorm.DB
	mu sync.Mutex
}

// NewCallLogRepository 创建 CallLogRepository
func NewCallLogRepository(db *gorm.DB) *CallLogRepository {
	return &CallLogRepository{db: db}
}

// BatchInsert 批量写入 call_logs
// 使用 ON CONFLICT (request_id) DO NOTHING 保证幂等
func (r *CallLogRepository) BatchInsert(ctx context.Context, logs []model.CallLog) error {
	if len(logs) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(logs, 200).
		Error
}

// FlushBatch 带批量大小参数的写入
func (r *CallLogRepository) FlushBatch(ctx context.Context, logs []model.CallLog, batchSize int) error {
	if len(logs) == 0 {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(logs, batchSize).
		Error
}

// CallLogBatchWriter 批量写入器配置
type CallLogBatchWriter struct {
	repo     *CallLogRepository
	batch    []model.CallLog
	maxSize  int
	interval time.Duration
	lastSent time.Time
	mu       sync.Mutex
}

// NewCallLogBatchWriter 创建批量写入器
func NewCallLogBatchWriter(repo *CallLogRepository, maxSize int, interval time.Duration) *CallLogBatchWriter {
	return &CallLogBatchWriter{
		repo:     repo,
		batch:    make([]model.CallLog, 0, maxSize),
		maxSize:  maxSize,
		interval: interval,
		lastSent: time.Now(),
	}
}

// Add 添加一条记录到批次，返回是否需要立即刷新
func (w *CallLogBatchWriter) Add(log model.CallLog) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.batch = append(w.batch, log)
	return len(w.batch) >= w.maxSize
}

// ShouldFlush 检查是否到达时间阈值需要刷新
func (w *CallLogBatchWriter) ShouldFlush() bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	return len(w.batch) > 0 && time.Since(w.lastSent) >= w.interval
}

// Flush 执行批量写入
func (w *CallLogBatchWriter) Flush(ctx context.Context) error {
	w.mu.Lock()
	batch := w.batch
	w.batch = make([]model.CallLog, 0, w.maxSize)
	w.lastSent = time.Now()
	w.mu.Unlock()

	if len(batch) == 0 {
		return nil
	}
	return w.repo.BatchInsert(ctx, batch)
}

// Size 返回当前批次大小
func (w *CallLogBatchWriter) Size() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.batch)
}