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
	now := time.Now().UTC()
	if req.ExpiresAt.UTC().Before(now) {
		slog.Warn("expires_at rejected as past",
			"expires_at_utc", req.ExpiresAt.UTC(),
			"now_utc", now,
			"expires_at_raw", req.ExpiresAt,
		)
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
	if err := s.db.WithContext(ctx).Where("expires_at > ?", time.Now().UTC()).Find(&exemptions).Error; err != nil {
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
		Where("subject_type = ? AND subject_id = ? AND expires_at > ?", subjectType, subjectID, time.Now().UTC()).
		Count(&count)
	return count > 0
}

// CleanupExpired 清理过期豁免（可由定时任务调用）
func (s *QuotaExemptionService) CleanupExpired(ctx context.Context) (int64, error) {
	result := s.db.WithContext(ctx).Where("expires_at <= ?", time.Now().UTC()).Delete(&quota.QuotaExemption{})
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

// ListExemptionFilter 豁免列表筛选条件
type ListExemptionFilter struct {
	Keyword     string // 按主体名称模糊搜索
	SubjectType string // 按类型筛选：user / service_key
	Status      string // 按状态筛选：active / expiring_soon / expired
}

// ListActiveExemptionsWithNames 列出豁免，并解析主体名称和创建人名称
// 支持分页：page 从 1 开始，pageSize 默认 20
// 支持搜索和筛选：keyword / subject_type / status
func (s *QuotaExemptionService) ListActiveExemptionsWithNames(ctx context.Context, page, pageSize int, filter ListExemptionFilter) ([]QuotaExemptionListItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	now := time.Now().UTC()

	// ── 关键词搜索：从 users / api_keys 表中模糊匹配名称，收集匹配的 subject ID ──
	var matchedUserIDs []string
	var matchedKeyIDs []string
	if filter.Keyword != "" {
		kw := "%" + filter.Keyword + "%"
		type idRow struct {
			ID string
		}
		// 搜索用户名
		var userRows []idRow
		_ = s.db.WithContext(ctx).Table("users").
			Select("CAST(id AS TEXT) AS id").
			Where("name ILIKE ?", kw).
			Find(&userRows).Error
		for _, r := range userRows {
			matchedUserIDs = append(matchedUserIDs, r.ID)
		}
		// 搜索 Service Key 名
		var keyRows []idRow
		_ = s.db.WithContext(ctx).Table("api_keys").
			Select("CAST(id AS TEXT) AS id").
			Where("name ILIKE ?", kw).
			Find(&keyRows).Error
		for _, r := range keyRows {
			matchedKeyIDs = append(matchedKeyIDs, r.ID)
		}
		// 关键词无匹配 → 直接返回空
		if len(matchedUserIDs) == 0 && len(matchedKeyIDs) == 0 {
			return []QuotaExemptionListItem{}, 0, nil
		}
	}

	// ── 构建基础查询 ──
	baseQuery := s.db.WithContext(ctx).Model(&quota.QuotaExemption{})

	// 状态筛选：未指定时默认只返回 active（与方法名语义一致）
	switch filter.Status {
	case "active", "":
		baseQuery = baseQuery.Where("expires_at > ?", now)
	case "expiring_soon":
		baseQuery = baseQuery.Where("expires_at > ? AND expires_at <= ?", now, now.Add(24*time.Hour))
	case "expired":
		baseQuery = baseQuery.Where("expires_at <= ?", now)
	case "all":
		// 显式传 "all" 时不过滤
	}

	// 类型筛选
	if filter.SubjectType == "user" || filter.SubjectType == "service_key" {
		baseQuery = baseQuery.Where("subject_type = ?", filter.SubjectType)
	}

	// 关键词筛选
	if filter.Keyword != "" {
		if len(matchedUserIDs) > 0 && len(matchedKeyIDs) > 0 {
			baseQuery = baseQuery.Where(
				"(subject_type = 'user' AND subject_id IN ?) OR (subject_type = 'service_key' AND subject_id IN ?)",
				matchedUserIDs, matchedKeyIDs,
			)
		} else if len(matchedUserIDs) > 0 {
			baseQuery = baseQuery.Where("subject_type = 'user' AND subject_id IN ?", matchedUserIDs)
		} else if len(matchedKeyIDs) > 0 {
			baseQuery = baseQuery.Where("subject_type = 'service_key' AND subject_id IN ?", matchedKeyIDs)
		}
	}

	// 先查总数
	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count exemptions: %w", err)
	}
	if total == 0 {
		return []QuotaExemptionListItem{}, 0, nil
	}

	// 分页查询
	offset := (page - 1) * pageSize
	var exemptions []quota.QuotaExemption
	if err := baseQuery.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&exemptions).Error; err != nil {
		return nil, 0, fmt.Errorf("list exemptions: %w", err)
	}

	// 收集用户 ID 和 key ID
	// SubjectID 总是 UUID 字符串
	// CreatedBy 可能是 UUID（新记录）或显示名称（旧记录），需要兼容
	userIDs := make(map[string]bool)
	keyIDs := make(map[string]bool)
	for _, e := range exemptions {
		if e.SubjectType == "user" {
			userIDs[e.SubjectID] = true
		} else if e.SubjectType == "service_key" {
			keyIDs[e.SubjectID] = true
		}
		// 尝试将 CreatedBy 解析为 UUID，若是则加入 userIDs 批量查询
		if _, err := uuid.Parse(e.CreatedBy); err == nil {
			userIDs[e.CreatedBy] = true
		}
	}

		// 批量查询用户名称（users.id 是 uuid，需要 CAST 为 text 与 string 比较）
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
			_ = s.db.WithContext(ctx).Table("users").
				Select("CAST(id AS TEXT) AS id, name").
				Where("CAST(id AS TEXT) IN ?", ids).
				Find(&rows).Error
			for _, r := range rows {
				userNames[r.ID] = r.Name
			}
		}

		// 批量查询 Service Key 名称（api_keys.id 是 uuid，需要 CAST 为 text 与 string 比较）
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
			_ = s.db.WithContext(ctx).Table("api_keys").
				Select("CAST(id AS TEXT) AS id, name").
				Where("CAST(id AS TEXT) IN ?", ids).
				Find(&rows).Error
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
		// CreatedBy 兼容：新记录存 UUID（查表解析），旧记录存显示名称（直接使用）
		if name, ok := userNames[e.CreatedBy]; ok {
			item.CreatedByName = name
		} else {
			item.CreatedByName = e.CreatedBy
		}
		items = append(items, item)
	}

	return items, total, nil
}

// CreateExemptionRequest 创建豁免请求
type CreateExemptionRequest struct {
	SubjectType string    `json:"subject_type"` // user | service_key
	SubjectID   string    `json:"subject_id"`
	Reason      string    `json:"reason"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedBy   string    `json:"-"` // 从 JWT context 注入
}
