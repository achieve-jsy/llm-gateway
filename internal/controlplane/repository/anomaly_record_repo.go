package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AnomalyRecordRepo 方案 X 异常反馈闭环（真人工反馈）的 anomaly_records 数据访问。
// 封装 GORM；把「唯一键 upsert 不覆盖用户反馈态」等业务语义收拢到 repo，handler 只做编排/校验。
type AnomalyRecordRepo struct {
	db *gorm.DB
}

func NewAnomalyRecordRepo(db *gorm.DB) *AnomalyRecordRepo {
	return &AnomalyRecordRepo{db: db}
}

// FindByUpsertKey 按唯一键 (scope_level, scope_id, rule, window_start, member_id) 查找既有记录。
// 命中返回 (rec, nil)；未命中返回 (nil, gorm.ErrRecordNotFound)。
// 注意：window_start 为 timestamptz；非按窗规则（A3–A5）以零时间哨兵参与等值匹配。
func (r *AnomalyRecordRepo) FindByUpsertKey(scopeLevel, scopeID, rule, memberID string, ws time.Time) (*model.AnomalyRecord, error) {
	var rec model.AnomalyRecord
	err := r.db.Where("scope_level = ? AND scope_id = ? AND rule = ? AND member_id = ? AND window_start = ?",
		scopeLevel, scopeID, rule, memberID, ws).First(&rec).Error
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// Create 新建一条异常记录（默认「待确认」由 model 默认值或调用方设置；id 由 gen_random_uuid 回填）。
func (r *AnomalyRecordRepo) Create(rec *model.AnomalyRecord) error {
	return r.db.Create(rec).Error
}

// TouchUpdatedAt 命中已有记录时，仅刷 updated_at（不覆盖用户反馈态 status/feedback_*）。
func (r *AnomalyRecordRepo) TouchUpdatedAt(scopeLevel, scopeID, rule, memberID string, ws time.Time) error {
	return r.db.Model(&model.AnomalyRecord{}).
		Where("scope_level = ? AND scope_id = ? AND rule = ? AND member_id = ? AND window_start = ?",
			scopeLevel, scopeID, rule, memberID, ws).
		Update("updated_at", time.Now().UTC()).Error
}

// FindByScopeWindow 读回指定范围 + 时间窗的真实状态行（report 读回 / 覆盖 AnomalyHit.Status）。
// scopeLevel/scopeID 必填；from/to 为可选的 Asia/Shanghai 日窗闭开区间 [from 00:00, to+1 00:00)。
func (r *AnomalyRecordRepo) FindByScopeWindow(ctxScopeLevel, ctxScopeID string, from, to *time.Time) ([]model.AnomalyRecord, error) {
	q := r.db.Model(&model.AnomalyRecord{}).
		Select("rule, window_start, member_id, status").
		Where("scope_level = ? AND scope_id = ?", ctxScopeLevel, ctxScopeID)
	if from != nil {
		q = q.Where("window_start >= ?", *from)
	}
	if to != nil {
		q = q.Where("window_start < ?", *to)
	}
	rows := make([]model.AnomalyRecord, 0)
	err := q.Order("rule ASC, window_start ASC").Scan(&rows).Error
	return rows, err
}

// UpdateFeedback 人工反馈状态：写 status/feedback_at/feedback_by/reason + updated_at。
// 返回 RowsAffected（0 = 记录不存在）。
func (r *AnomalyRecordRepo) UpdateFeedback(id uuid.UUID, status, feedbackBy, reason string) (int64, error) {
	updates := map[string]interface{}{
		"status":      status,
		"feedback_at": time.Now().UTC(),
		"feedback_by": feedbackBy,
		"reason":      reason,
		"updated_at":  time.Now().UTC(),
	}
	res := r.db.Model(&model.AnomalyRecord{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
