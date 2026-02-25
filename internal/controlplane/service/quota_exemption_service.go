package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/core/quota"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// QuotaExemptionService 管理紧急豁免（创建/撤销/查询）
type QuotaExemptionService struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewQuotaExemptionService(db *gorm.DB, rdb *redis.Client) *QuotaExemptionService {
	return &QuotaExemptionService{db: db, rdb: rdb}
}

// CreateExemption 创建紧急豁免
func (s *QuotaExemptionService) CreateExemption(ctx context.Context, req CreateExemptionRequest) (*quota.QuotaExemption, error) {
	if req.SubjectType != "user" && req.SubjectType != "service_key" {
		return nil, fmt.Errorf("invalid subject_type: must be 'user' or 'service_key'")
	}
	if req.SubjectID == "" {
		return nil, fmt.Errorf("subject_id is required")
	}
	if req.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("expires_at must be in the future")
	}
	if req.CreatedBy == "" {
		return nil, fmt.Errorf("created_by is required")
	}

	exemption := &quota.QuotaExemption{
		ID:          uuid.New(),
		SubjectType: req.SubjectType,
		SubjectID:   req.SubjectID,
		Reason:      req.Reason,
		ExpiresAt:   req.ExpiresAt,
		CreatedBy:   req.CreatedBy,
	}

	if err := s.db.WithContext(ctx).Create(exemption).Error; err != nil {
		return nil, fmt.Errorf("create exemption: %w", err)
	}

	slog.Info("quota exemption created",
		"exemption_id", exemption.ID,
		"subject_type", exemption.SubjectType,
		"subject_id", exemption.SubjectID,
		"expires_at", exemption.ExpiresAt,
		"created_by", exemption.CreatedBy,
	)

	// 刷新 snapshot 并写审计
	go s.refreshSnapshotAsync()

	return exemption, nil
}

// RevokeExemption 撤销豁免
func (s *QuotaExemptionService) RevokeExemption(ctx context.Context, exemptionID uuid.UUID, revokedBy string) error {
	var exemption quota.QuotaExemption
	if err := s.db.WithContext(ctx).Where("id = ?", exemptionID).First(&exemption).Error; err != nil {
		return fmt.Errorf("exemption not found: %w", err)
	}

	if err := s.db.WithContext(ctx).Delete(&exemption).Error; err != nil {
		return fmt.Errorf("delete exemption: %w", err)
	}

	slog.Info("quota exemption revoked",
		"exemption_id", exemptionID,
		"revoked_by", revokedBy,
	)

	go s.refreshSnapshotAsync()

	return nil
}

// ListActiveExemptions 列出所有当前生效的豁免
func (s *QuotaExemptionService) ListActiveExemptions(ctx context.Context) ([]quota.QuotaExemption, error) {
	var exemptions []quota.QuotaExemption
	if err := s.db.WithContext(ctx).Where("expires_at > ?", time.Now()).Find(&exemptions).Error; err != nil {
		return nil, fmt.Errorf("list exemptions: %w", err)
	}
	return exemptions, nil
}

// ListAllExemptions 列出所有豁免（含已过期）
func (s *QuotaExemptionService) ListAllExemptions(ctx context.Context, limit int) ([]quota.QuotaExemption, error) {
	var exemptions []quota.QuotaExemption
	q := s.db.WithContext(ctx).Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&exemptions).Error; err != nil {
		return nil, fmt.Errorf("list exemptions: %w", err)
	}
	return exemptions, nil
}

// IsExempt 检查指定主体是否当前被豁免
func (s *QuotaExemptionService) IsExempt(ctx context.Context, subjectType, subjectID string) bool {
	var count int64
	s.db.WithContext(ctx).Model(&quota.QuotaExemption{}).
		Where("subject_type = ? AND subject_id = ? AND expires_at > ?", subjectType, subjectID, time.Now()).
		Count(&count)
	return count > 0
}

// CleanupExpired 清理过期豁免（可由定时任务调用）
func (s *QuotaExemptionService) CleanupExpired(ctx context.Context) (int64, error) {
	result := s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&quota.QuotaExemption{})
	if result.Error != nil {
		return 0, fmt.Errorf("cleanup expired exemptions: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		slog.Info("cleaned up expired exemptions", "count", result.RowsAffected)
		go s.refreshSnapshotAsync()
	}
	return result.RowsAffected, nil
}

func (s *QuotaExemptionService) refreshSnapshotAsync() {
	if s.rdb == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	exemptions, err := s.ListActiveExemptions(ctx)
	if err != nil {
		slog.Warn("failed to list exemptions for snapshot", "error", err)
		return
	}

	// 读取当前 snapshot 并更新 exemptions
	raw, err := s.rdb.Get(ctx, QuotaSnapshotKey).Result()
	if err != nil {
		slog.Warn("failed to read snapshot for exemption update", "error", err)
		return
	}

	var snap QuotaSnapshotData
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		slog.Warn("failed to unmarshal snapshot", "error", err)
		return
	}

	// 转换 exemptions
	snap.Exemptions = make([]QuotaExemptionEntry, 0, len(exemptions))
	for _, e := range exemptions {
		snap.Exemptions = append(snap.Exemptions, QuotaExemptionEntry{
			SubjectType: e.SubjectType,
			SubjectID:   e.SubjectID,
			Reason:      e.Reason,
			ExpiresAt:   e.ExpiresAt,
		})
	}

	data, err := json.Marshal(snap)
	if err != nil {
		slog.Warn("failed to marshal snapshot", "error", err)
		return
	}

	if err := s.rdb.Set(ctx, QuotaSnapshotKey, data, 0).Err(); err != nil {
		slog.Warn("failed to write snapshot", "error", err)
		return
	}

	s.rdb.Publish(ctx, "config:change", "quota_snapshot_updated")
	slog.Info("quota snapshot updated with exemptions", "count", len(exemptions))
}

// QuotaExemptionListItem 豁免列表项（含解析后的名称）
type QuotaExemptionListItem struct {
	quota.QuotaExemption
	SubjectName   string `json:"subject_name"`    // 用户姓名 或 Service Key 名称
	CreatedByName string `json:"created_by_name"` // 创建人姓名
}

// ListActiveExemptionsWithNames 列出当前生效的豁免，并解析主体名称和创建人名称
func (s *QuotaExemptionService) ListActiveExemptionsWithNames(ctx context.Context) ([]QuotaExemptionListItem, error) {
	exemptions, err := s.ListActiveExemptions(ctx)
	if err != nil {
		return nil, err
	}
	if len(exemptions) == 0 {
		return []QuotaExemptionListItem{}, nil
	}

	// 收集用户 ID 和 key ID
	userIDs := make(map[string]bool)
	keyIDs := make(map[string]bool)
	for _, e := range exemptions {
		if e.SubjectType == "user" {
			userIDs[e.SubjectID] = true
		} else if e.SubjectType == "service_key" {
			keyIDs[e.SubjectID] = true
		}
		userIDs[e.CreatedBy] = true
	}

	// 批量查询用户名称
	userNames := make(map[string]string)
	if len(userIDs) > 0 {
		ids := make([]string, 0, len(userIDs))
		for id := range userIDs {
			ids = append(ids, id)
		}
		type userNameRow struct {
			ID   string
			Name string
		}
		var rows []userNameRow
		_ = s.db.WithContext(ctx).Table("users").Select("id, name").Where("id IN ?", ids).Find(&rows).Error
		for _, r := range rows {
			userNames[r.ID] = r.Name
		}
	}

	// 批量查询 Service Key 名称
	keyNames := make(map[string]string)
	if len(keyIDs) > 0 {
		ids := make([]string, 0, len(keyIDs))
		for id := range keyIDs {
			ids = append(ids, id)
		}
		type keyNameRow struct {
			ID   string
			Name string
		}
		var rows []keyNameRow
		_ = s.db.WithContext(ctx).Table("api_keys").Select("id, name").Where("id IN ?", ids).Find(&rows).Error
		for _, r := range rows {
			keyNames[r.ID] = r.Name
		}
	}

	// 组装结果
	items := make([]QuotaExemptionListItem, 0, len(exemptions))
	for _, e := range exemptions {
		item := QuotaExemptionListItem{QuotaExemption: e}
		if e.SubjectType == "user" {
			item.SubjectName = userNames[e.SubjectID]
		} else {
			item.SubjectName = keyNames[e.SubjectID]
		}
		item.CreatedByName = userNames[e.CreatedBy]
		items = append(items, item)
	}

	return items, nil
}

// CreateExemptionRequest 创建豁免请求
type CreateExemptionRequest struct {
	SubjectType string    `json:"subject_type"` // user | service_key
	SubjectID   string    `json:"subject_id"`
	Reason      string    `json:"reason"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedBy   string    `json:"-"` // 从 JWT context 注入
}
