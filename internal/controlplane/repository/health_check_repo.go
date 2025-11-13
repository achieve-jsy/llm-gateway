package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// HealthCheckRepo 健康检测数据访问层
type HealthCheckRepo struct {
	db *gorm.DB
}

// NewHealthCheckRepo 创建 HealthCheckRepo 实例
func NewHealthCheckRepo(db *gorm.DB) *HealthCheckRepo {
	return &HealthCheckRepo{db: db}
}

// GetConfig 获取健康检测配置（单例）
func (r *HealthCheckRepo) GetConfig() (*model.HealthCheckConfig, error) {
	var config model.HealthCheckConfig
	err := r.db.First(&config).Error
	return &config, err
}

// UpdateConfig 更新配置
func (r *HealthCheckRepo) UpdateConfig(config *model.HealthCheckConfig) error {
	return r.db.Save(config).Error
}

// CreateLog 创建检测记录
func (r *HealthCheckRepo) CreateLog(log *model.HealthCheckLog) error {
	return r.db.Create(log).Error
}

// GetRecentLogs 获取最近的检测记录
func (r *HealthCheckRepo) GetRecentLogs(limit int) ([]model.HealthCheckLog, error) {
	var logs []model.HealthCheckLog
	err := r.db.Order("checked_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// GetTargetLogs 获取某目标的最近记录
func (r *HealthCheckRepo) GetTargetLogs(targetType string, targetID uuid.UUID, limit int) ([]model.HealthCheckLog, error) {
	var logs []model.HealthCheckLog
	err := r.db.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("checked_at DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// CountConsecutiveFailures 统计连续失败次数
func (r *HealthCheckRepo) CountConsecutiveFailures(targetType string, targetID uuid.UUID) (int, error) {
	var logs []model.HealthCheckLog
	err := r.db.Where("target_type = ? AND target_id = ?", targetType, targetID).
		Order("checked_at DESC").Limit(10).Find(&logs).Error
	if err != nil {
		return 0, err
	}
	count := 0
	for _, log := range logs {
		if log.Success {
			break
		}
		count++
	}
	return count, nil
}

// CleanOldLogs 清理超过指定天数的日志
func (r *HealthCheckRepo) CleanOldLogs(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	return r.db.Where("checked_at < ?", cutoff).Delete(&model.HealthCheckLog{}).Error
}

// GetLogsByTimeRange 获取指定时间范围内的检测记录，支持按目标名称搜索
func (r *HealthCheckRepo) GetLogsByTimeRange(since time.Time, targetType string, search string) ([]model.HealthCheckLog, error) {
	query := r.db.Where("checked_at >= ?", since)
	if targetType != "" {
		query = query.Where("target_type = ?", targetType)
	}
	if search != "" {
		query = query.Where("target_name ILIKE ?", "%"+search+"%")
	}
	var logs []model.HealthCheckLog
	err := query.Order("checked_at ASC").Find(&logs).Error
	return logs, err
}

// TargetProviderCount 按模型名称统计 provider/key 数量
type TargetProviderCount struct {
	TargetName   string `json:"target_name"`
	TargetType   string `json:"target_type"`
	ProviderCnt  int    `json:"provider_count"`
	KeyCnt       int    `json:"key_count"`
}

// GetTargetProviderCounts 批量获取模型名称对应的 provider/key 数量
func (r *HealthCheckRepo) GetTargetProviderCounts(targetNames []string) (map[string]TargetProviderCount, error) {
	if len(targetNames) == 0 {
		return map[string]TargetProviderCount{}, nil
	}
	type row struct {
		Name         string `gorm:"column:target_name"`
		ProviderCnt  int    `gorm:"column:provider_count"`
	}
	var rows []row
	err := r.db.Raw(`
		SELECT hcl.target_name, COUNT(DISTINCT m.provider_id) AS provider_count
		FROM health_check_logs hcl
		JOIN models m ON m.name = hcl.target_name
		WHERE hcl.target_type = 'model'
		  AND hcl.target_name IN ?
		GROUP BY hcl.target_name
	`, targetNames).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]TargetProviderCount, len(rows))
	for i := range rows {
		result[rows[i].Name] = TargetProviderCount{
			TargetName:  rows[i].Name,
			TargetType:  "model",
			ProviderCnt: rows[i].ProviderCnt,
			KeyCnt:      0,
		}
	}
	return result, nil
}

// TimelineBucketRow 时间线聚合桶查询结果行
type TimelineBucketRow struct {
	TargetName  string    `gorm:"column:target_name"`
	TargetType  string    `gorm:"column:target_type"`
	BucketIdx   int       `gorm:"column:bucket_idx"`
	TotalCount  int       `gorm:"column:total"`
	OkCount     int       `gorm:"column:ok_count"`
	AvgRtt      int       `gorm:"column:avg_rtt"`
	FirstErr    string    `gorm:"column:first_err"`
	BucketStart time.Time `gorm:"column:bucket_start"`
}

// GetTimelineBuckets SQL 层直接聚合 100 桶数据，同时返回 provider/key 计数
func (r *HealthCheckRepo) GetTimelineBuckets(since time.Time, search string, bucketCount int) ([]TimelineBucketRow, map[string]TargetProviderCount, error) {
	searchPattern := ""
	if search != "" {
		searchPattern = "%" + search + "%"
	}
	bucketMs := time.Since(since).Milliseconds() / int64(bucketCount)

	var rows []TimelineBucketRow
	err := r.db.Raw(`
		SELECT
			target_name,
			target_type,
			(EXTRACT(EPOCH FROM (checked_at - ?::timestamptz)) * 1000 / ?)::int AS bucket_idx,
			COUNT(*)::int AS total,
			COUNT(*) FILTER (WHERE success)::int AS ok_count,
			AVG(response_time_ms)::int AS avg_rtt,
			MIN(CASE WHEN NOT success THEN error_message END) AS first_err,
			MIN(checked_at) AS bucket_start
		FROM health_check_logs
		WHERE checked_at >= ?
		  AND target_type = 'model'
		  AND (? = '' OR target_name ILIKE ?)
		  AND target_name IN (SELECT DISTINCT name FROM models WHERE enabled = true)
		GROUP BY target_name, target_type, bucket_idx
		ORDER BY target_name, bucket_idx
	`, since, bucketMs, since, searchPattern, searchPattern).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	// 从 models 表直接获取 provider 计数（避免 JOIN health_check_logs 全表）
	nameSet := make(map[string]bool)
	for _, r := range rows {
		nameSet[r.TargetName] = true
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}

	type providerRow struct {
		Name string `gorm:"column:name"`
		Cnt  int    `gorm:"column:provider_count"`
	}
	var provRows []providerRow
	if len(names) > 0 {
		r.db.Raw(`
			SELECT name, COUNT(DISTINCT provider_id)::int AS provider_count
			FROM models
			WHERE name IN ? AND enabled = true
			GROUP BY name
		`, names).Scan(&provRows)
	}
	pcm := make(map[string]TargetProviderCount, len(provRows))
	for i := range provRows {
		pcm[provRows[i].Name] = TargetProviderCount{
			TargetName:  provRows[i].Name,
			TargetType:  "model",
			ProviderCnt: provRows[i].Cnt,
		}
	}

	return rows, pcm, nil
}

// GroupTimelineBucketRow 按模型组聚合的时间线桶查询结果行
type GroupTimelineBucketRow struct {
	GroupName   string    `gorm:"column:group_name"`
	BucketIdx   int       `gorm:"column:bucket_idx"`
	TotalCount  int       `gorm:"column:total"`
	OkCount     int       `gorm:"column:ok_count"`
	AvgRtt      int       `gorm:"column:avg_rtt"`
	FirstErr    string    `gorm:"column:first_err"`
	BucketStart time.Time `gorm:"column:bucket_start"`
	ModelCount  int       `gorm:"column:model_count"`
}

// GroupMeta 模型组元数据（成员模型数、策略）
type GroupMeta struct {
	GroupID    int    `gorm:"column:group_id"`
	GroupName  string `gorm:"column:group_name"`
	Strategy   string `gorm:"column:strategy"`
	ModelCount int    `gorm:"column:model_count"`
}

// GetGroupTimelineBuckets 以模型组为主驱动，查询各组的健康日志时间桶（target_type='group'）。
// 先枚举所有启用的组（含元数据），再 LEFT JOIN 健康日志，确保无日志的组也能出现在结果中。
func (r *HealthCheckRepo) GetGroupTimelineBuckets(since time.Time, search string, bucketCount int) ([]GroupTimelineBucketRow, map[string]GroupMeta, error) {
	searchPattern := ""
	if search != "" {
		searchPattern = "%" + search + "%"
	}
	bucketMs := time.Since(since).Milliseconds() / int64(bucketCount)

	// Step 1: 以组为主，查出所有符合条件的启用组元数据
	var metas []GroupMeta
	err := r.db.Raw(`
		SELECT mg.id AS group_id, mg.name AS group_name, mg.strategy,
		       COUNT(gm.model_id)::int AS model_count
		FROM model_groups mg
		LEFT JOIN group_models gm ON gm.group_id = mg.id
		WHERE mg.deleted_at IS NULL
		  AND mg.enabled = true
		  AND (? = '' OR mg.name ILIKE ?)
		GROUP BY mg.id, mg.name, mg.strategy
		ORDER BY mg.name
	`, searchPattern, searchPattern).Scan(&metas).Error
	if err != nil {
		return nil, nil, err
	}

	metaMap := make(map[string]GroupMeta, len(metas))
	for i := range metas {
		metaMap[metas[i].GroupName] = metas[i]
	}

	if len(metas) == 0 {
		return []GroupTimelineBucketRow{}, metaMap, nil
	}

	// 收集组名用于日志查询
	groupNames := make([]string, 0, len(metas))
	for i := range metas {
		groupNames = append(groupNames, metas[i].GroupName)
	}

	// Step 2: 查询这些组的健康日志时间桶（target_type='group'）
	var rows []GroupTimelineBucketRow
	err = r.db.Raw(`
		SELECT
			target_name AS group_name,
			(EXTRACT(EPOCH FROM (checked_at - ?::timestamptz)) * 1000 / ?)::int AS bucket_idx,
			COUNT(*)::int AS total,
			COUNT(*) FILTER (WHERE success)::int AS ok_count,
			AVG(response_time_ms)::int AS avg_rtt,
			MIN(CASE WHEN NOT success THEN error_message END) AS first_err,
			MIN(checked_at) AS bucket_start,
			0::int AS model_count
		FROM health_check_logs
		WHERE checked_at >= ?
		  AND target_type = 'group'
		  AND target_name IN ?
		GROUP BY target_name, bucket_idx
		ORDER BY target_name, bucket_idx
	`, since, bucketMs, since, groupNames).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}

	return rows, metaMap, nil
}
