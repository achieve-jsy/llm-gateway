package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// CallLogRepository call_logs 表操作（observability-hub 侧）
type CallLogRepository struct {
	db *gorm.DB
}

// NewCallLogRepository 创建 CallLogRepository
func NewCallLogRepository(db *gorm.DB) *CallLogRepository {
	return &CallLogRepository{db: db}
}

// BatchInsert 批量幂等写入
func (r *CallLogRepository) BatchInsert(ctx context.Context, logs []obsmodel.CallLog) error {
	if len(logs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(logs, 200).
		Error
}