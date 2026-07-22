package service

import (
	"context"
	"strings"

	"gorm.io/gorm"
)

// PositionAttributionService 提供「岗位归因」查询：LEFT JOIN users 取
// job_category/job_family/job_title，收敛为单一口径的 Position 字段，
// 供 dashboard 四象限「同岗」分组与判定引擎「输出失控(3.4)/杀鸡用牛刀(3.5)」
// 的同岗位分位基线共用，避免在多处各自 COALESCE 出现口径分叉。
//
// 对齐 docs/token-usage-analysis-report-implementation.md §6.4：
//
//	LEFT JOIN users u ON u.id::text = cl.user_id
//	-- 取 COALESCE(u.job_category, u.job_family, u.job_title, '') AS position
//
// 只读查询，不改 users / call_logs 结构。
type PositionAttributionService struct {
	db *gorm.DB
}

// NewPositionAttributionService 创建 PositionAttributionService。
func NewPositionAttributionService(db *gorm.DB) *PositionAttributionService {
	return &PositionAttributionService{db: db}
}

// UserPosition 是单个用户的岗位归因结果。
// Position 是统一口径：COALESCE(job_category, job_family, job_title, ”)，
// 缺任何花名册数据时为空字符串（未知岗位），调用方应将其视为独立分组，
// 不应报错、也不应回退到全局基线掩盖「岗位数据缺失」这一事实。
type UserPosition struct {
	UserID      string
	JobCategory string
	JobFamily   string
	JobTitle    string
	Position    string
}

// userPositionRow 是查询期投影行（供 GORM Raw().Scan() 使用）。
type userPositionRow struct {
	UserID      string `gorm:"column:user_id"`
	JobCategory string `gorm:"column:job_category"`
	JobFamily   string `gorm:"column:job_family"`
	JobTitle    string `gorm:"column:job_title"`
}

// GetUserPositions 批量查询岗位归因（单次 SELECT ... WHERE id IN (?)，消除 N+1）。
//
// userIDs 为空、去重后为空、或 service 未初始化（db=nil）时返回空 map、nil error，
// 不触发全表查询。未在 users 表命中的 userID（用户不存在/未导入花名册）不会出现
// 在返回 map 中；调用方应对缺失键回退为 UserPosition{Position:""}（未知岗位），
// 不应视为错误。
func (s *PositionAttributionService) GetUserPositions(ctx context.Context, userIDs []string) (map[string]UserPosition, error) {
	result := make(map[string]UserPosition, len(userIDs))
	if s == nil || s.db == nil {
		return result, nil
	}

	ids := dedupNonEmptyIDs(userIDs)
	if len(ids) == 0 {
		return result, nil
	}

	var rows []userPositionRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT CAST(id AS TEXT) AS user_id,
		       COALESCE(job_category, '') AS job_category,
		       COALESCE(job_family, '')   AS job_family,
		       COALESCE(job_title, '')    AS job_title
		FROM users
		WHERE CAST(id AS TEXT) IN ?
	`, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, r := range rows {
		result[r.UserID] = UserPosition{
			UserID:      r.UserID,
			JobCategory: r.JobCategory,
			JobFamily:   r.JobFamily,
			JobTitle:    r.JobTitle,
			Position:    resolvePosition(r.JobCategory, r.JobFamily, r.JobTitle),
		}
	}
	return result, nil
}

// GetUserPosition 查询单个用户的岗位归因；未命中返回 UserPosition{Position:""}
// （未知岗位），不报错——保持与 GetUserPositions 缺失键语义一致。
func (s *PositionAttributionService) GetUserPosition(ctx context.Context, userID string) (UserPosition, error) {
	trimmed := strings.TrimSpace(userID)
	positions, err := s.GetUserPositions(ctx, []string{trimmed})
	if err != nil {
		return UserPosition{}, err
	}
	if p, ok := positions[trimmed]; ok {
		return p, nil
	}
	return UserPosition{UserID: trimmed}, nil
}

// resolvePosition 统一岗位口径：COALESCE(job_category, job_family, job_title, ”)。
func resolvePosition(jobCategory, jobFamily, jobTitle string) string {
	if jobCategory != "" {
		return jobCategory
	}
	if jobFamily != "" {
		return jobFamily
	}
	return jobTitle
}

// dedupNonEmptyIDs 去重并剔除空白 ID，保持首次出现的相对顺序（顺序对本服务
// 无业务含义，仅为了让测试断言稳定）。
func dedupNonEmptyIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
