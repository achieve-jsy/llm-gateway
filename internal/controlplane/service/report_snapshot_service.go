package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"

	cpmodel "github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/observability/report/v2"
)

// 本文件实现 docs/token-usage-analysis-report-implementation.md §10.3 的 report_snapshot 存档读写，
// 及 §11 严重异常待核查反馈回写（POST /usage/report/anomaly/:user_id/feedback）。
//
//   - report_snapshot 唯一索引 (level, scope_id, period_start)：周期性 job 写，页面/MD/HTML 都读它保证一致。
//   - API 命中缓存直接返回；未命中由调用方装配后 Upsert 落库。
//   - 3.6 反馈只允许 pending_review -> confirmed_ok|need_check|false_positive，由负责人处置。

// ReportSnapshotQuery 标识一份报告的缓存键（level + scope_id + period_start + period_type）。
type ReportSnapshotQuery struct {
	Level       string
	ScopeID     string
	PeriodType  string
	PeriodStart time.Time
}

// ReportSnapshotService 提供 report_snapshot 读/写与 anomaly_review 反馈回写。
type ReportSnapshotService struct {
	db *gorm.DB
}

// NewReportSnapshotService 创建 report_snapshot 存档服务。
func NewReportSnapshotService(db *gorm.DB) *ReportSnapshotService {
	return &ReportSnapshotService{db: db}
}

// Get 命中缓存返回 *report.Report（nil 表示未命中）。
func (s *ReportSnapshotService) Get(ctx context.Context, q ReportSnapshotQuery) (*report.Report, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var snap cpmodel.ReportSnapshot
	err := s.db.WithContext(ctx).
		Where("level = ? AND scope_id = ? AND period_start = ?", q.Level, q.ScopeID, q.PeriodStart).
		First(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("report snapshot get: %w", err)
	}
	var r report.Report
	if err := json.Unmarshal(snap.ReportJSON, &r); err != nil {
		return nil, fmt.Errorf("report snapshot unmarshal: %w", err)
	}
	return &r, nil
}

// Upsert 写入/更新一份 report_snapshot（按 (level, scope_id, period_start) 幂等 upsert）。
func (s *ReportSnapshotService) Upsert(ctx context.Context, q ReportSnapshotQuery, periodEnd time.Time, r *report.Report) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("report snapshot: db not configured")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("report snapshot marshal: %w", err)
	}

	var snap cpmodel.ReportSnapshot
	err = s.db.WithContext(ctx).
		Where("level = ? AND scope_id = ? AND period_start = ?", q.Level, q.ScopeID, q.PeriodStart).
		First(&snap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 不存在 → 新建。
		rec := cpmodel.ReportSnapshot{
			Level:         q.Level,
			ScopeID:       q.ScopeID,
			PeriodType:    q.PeriodType,
			PeriodStart:   q.PeriodStart,
			PeriodEnd:     periodEnd,
			ReportJSON:    datatypes.JSON(raw),
			SchemaVersion: report.SchemaVersion,
			GeneratedAt:   time.Now(),
		}
		return s.db.WithContext(ctx).Create(&rec).Error
	}
	if err != nil {
		return fmt.Errorf("report snapshot upsert: %w", err)
	}

	// 存在 → 刷新内容与时间。
	updates := map[string]any{
		"period_type":    q.PeriodType,
		"period_end":     periodEnd,
		"report_json":    datatypes.JSON(raw),
		"schema_version": report.SchemaVersion,
		"generated_at":   time.Now(),
	}
	return s.db.WithContext(ctx).Model(&snap).Updates(updates).Error
}

// Members 从缓存报告读取四象限成员点集（仅 department|studio）。未命中返回 nil,nil。
func (s *ReportSnapshotService) Members(ctx context.Context, q ReportSnapshotQuery) (*report.MemberEvaluation, error) {
	r, err := s.Get(ctx, q)
	if err != nil {
		return nil, err
	}
	if r == nil || r.MemberEvaluation == nil {
		return nil, nil
	}
	return r.MemberEvaluation, nil
}

// AnomalyFeedback 是 3.6 待核查反馈回写的请求体。
type AnomalyFeedback struct {
	// Status 目标状态：confirmed_ok | need_check | false_positive（pending_review -> 上述之一）。
	Status string
	// PeriodStart 定位待核查记录所在周期（YYYY-MM-DD）。非空时按 user+period 定位。
	PeriodStart string
}

// WriteAnomalyFeedback 把某 user 的 3.6 待核查记录置为负责人终裁状态。
// 只允许 pending_review -> confirmed_ok|need_check|false_positive；已终裁的记录不可重复改。
func (s *ReportSnapshotService) WriteAnomalyFeedback(ctx context.Context, userID, reviewer string, fb AnomalyFeedback) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("report snapshot: db not configured")
	}
	switch fb.Status {
	case cpmodel.AnomalyReviewStatusConfirmedOK,
		cpmodel.AnomalyReviewStatusNeedCheck,
		cpmodel.AnomalyReviewStatusFalsePositive:
	default:
		return fmt.Errorf("report snapshot: invalid status %q (want confirmed_ok|need_check|false_positive)", fb.Status)
	}

	now := time.Now()
	q := s.db.WithContext(ctx).
		Model(&cpmodel.AnomalyReview{}).
		Where("user_id = ? AND status = ?", userID, cpmodel.AnomalyReviewStatusPendingReview)
	if fb.PeriodStart != "" {
		q = q.Where("period_start = ?", parsePeriodDate(fb.PeriodStart))
	}
	// 若该用户有多个待核查周期记录，则全部终裁（leader 对某人对某状态的整体处置）。
	res := q.Updates(map[string]any{
		"status":      fb.Status,
		"reviewer":    reviewer,
		"reviewed_at": now,
	})
	if res.Error != nil {
		return fmt.Errorf("report snapshot anomaly feedback: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// GetAnomalyReviews 返回某 user 在 [from,to) 的待核查记录（供前端回显现状）。
func (s *ReportSnapshotService) GetAnomalyReviews(ctx context.Context, userID string) ([]cpmodel.AnomalyReview, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var rows []cpmodel.AnomalyReview
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).Order("period_start DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("report snapshot anomaly reviews: %w", err)
	}
	return rows, nil
}

// parsePeriodDate 解析 YYYY-MM-DD，失败返回零值（查询语义为不匹配任何记录）。
func parsePeriodDate(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}
