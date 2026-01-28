package ehrsync

import (
	"context"

	"github.com/example/llm-gateway/internal/controlplane/model"
)

// LogRecorder 批量写 6 张观测表
type LogRecorder struct {
	repo *Repository
}

// NewLogRecorder 用 Repository 实例化
func NewLogRecorder(repo *Repository) *LogRecorder {
	return &LogRecorder{repo: repo}
}

// RecordExecution 写入/更新 execution_log
func (lr *LogRecorder) RecordExecution(ctx context.Context, log *model.EhrSyncExecutionLog) error {
	return lr.repo.db.WithContext(ctx).Save(log).Error
}

// RecordChanges 批量写入 change_log
func (lr *LogRecorder) RecordChanges(ctx context.Context, items []model.EhrSyncChangeLog) error {
	if len(items) == 0 {
		return nil
	}
	return lr.repo.db.WithContext(ctx).CreateInBatches(items, 200).Error
}

// RecordErrors 批量写入 error_log
func (lr *LogRecorder) RecordErrors(ctx context.Context, items []model.EhrSyncErrorLog) error {
	if len(items) == 0 {
		return nil
	}
	return lr.repo.db.WithContext(ctx).CreateInBatches(items, 200).Error
}

// RecordSkips 批量写入 skip_log
func (lr *LogRecorder) RecordSkips(ctx context.Context, items []model.EhrSyncSkipLog) error {
	if len(items) == 0 {
		return nil
	}
	return lr.repo.db.WithContext(ctx).CreateInBatches(items, 200).Error
}

// RecordDiffReport 写入 diff_report（全量对账完成后）
func (lr *LogRecorder) RecordDiffReport(ctx context.Context, report *model.EhrSyncDiffReport) error {
	return lr.repo.db.WithContext(ctx).Create(report).Error
}

// RecordStatSnapshot 写入 stat_snapshot
func (lr *LogRecorder) RecordStatSnapshot(ctx context.Context, snap *model.EhrSyncStatSnapshot) error {
	return lr.repo.db.WithContext(ctx).Create(snap).Error
}
