package repository

import (
	"fmt"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// ChannelMonitorRepo 渠道监控数据访问层
type ChannelMonitorRepo struct {
	db *gorm.DB
}

// NewChannelMonitorRepo 创建 ChannelMonitorRepo 实例
func NewChannelMonitorRepo(db *gorm.DB) *ChannelMonitorRepo {
	return &ChannelMonitorRepo{db: db}
}

// ── Monitor CRUD ──────────────────────────────────────────────────────────────

// Create 创建监控
func (r *ChannelMonitorRepo) Create(m *model.ChannelMonitor) error {
	return r.db.Create(m).Error
}

// GetByID 按 ID 查询监控（含软删除）
func (r *ChannelMonitorRepo) GetByID(id int64) (*model.ChannelMonitor, error) {
	var m model.ChannelMonitor
	err := r.db.Where("id = ?", id).First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// Update 更新监控
func (r *ChannelMonitorRepo) Update(m *model.ChannelMonitor) error {
	return r.db.Save(m).Error
}

// Delete 软删除监控
func (r *ChannelMonitorRepo) Delete(id int64) error {
	return r.db.Delete(&model.ChannelMonitor{}, id).Error
}

// List 列表查询（支持过滤 + 分页）
func (r *ChannelMonitorRepo) List(provider string, enabled *bool, search string, page, pageSize int) ([]*model.ChannelMonitor, int64, error) {
	query := r.db.Model(&model.ChannelMonitor{})
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}
	if search != "" {
		query = query.Where("name ILIKE ? OR primary_model ILIKE ? OR group_name ILIKE ?",
			"%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var items []*model.ChannelMonitor
	offset := (page - 1) * pageSize
	if err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&items).Error; err != nil {
		return nil, 0, err
	}

	return items, total, nil
}

// ListEnabled 返回所有 enabled=true 的监控（供 runner 启动时加载）
func (r *ChannelMonitorRepo) ListEnabled() ([]*model.ChannelMonitor, error) {
	var items []*model.ChannelMonitor
	err := r.db.Where("enabled = ?", true).Find(&items).Error
	return items, err
}

// MarkChecked 更新 last_checked_at
func (r *ChannelMonitorRepo) MarkChecked(id int64, checkedAt time.Time) error {
	return r.db.Model(&model.ChannelMonitor{}).Where("id = ?", id).
		Update("last_checked_at", checkedAt).Error
}

// ── History ───────────────────────────────────────────────────────────────────

// InsertHistoryBatch 批量插入历史记录
func (r *ChannelMonitorRepo) InsertHistoryBatch(rows []*model.ChannelMonitorHistory) error {
	if len(rows) == 0 {
		return nil
	}
	return r.db.Create(&rows).Error
}

// ListHistory 按 monitor_id + model 查询最近 N 条历史
func (r *ChannelMonitorRepo) ListHistory(monitorID int64, modelName string, limit int) ([]*model.ChannelMonitorHistory, error) {
	query := r.db.Where("monitor_id = ?", monitorID)
	if modelName != "" {
		query = query.Where("model = ?", modelName)
	}
	var items []*model.ChannelMonitorHistory
	err := query.Order("checked_at DESC").Limit(limit).Find(&items).Error
	return items, err
}

// DeleteHistoryBefore 删除指定时间之前的历史记录
func (r *ChannelMonitorRepo) DeleteHistoryBefore(before time.Time) (int64, error) {
	result := r.db.Where("checked_at < ?", before).Delete(&model.ChannelMonitorHistory{})
	return result.RowsAffected, result.Error
}

// ── Daily Rollups ─────────────────────────────────────────────────────────────

// UpsertDailyRollupsFor 对指定日期的明细聚合到 daily_rollups（幂等）
func (r *ChannelMonitorRepo) UpsertDailyRollupsFor(targetDate time.Time) (int64, error) {
	dateStr := targetDate.Format("2006-01-02")
	nextDate := targetDate.Add(24 * time.Hour).Format("2006-01-02")

	return 0, r.db.Exec(`
		INSERT INTO channel_monitor_daily_rollups
			(monitor_id, model, bucket_date, total_checks, ok_count,
			 operational_count, degraded_count, failed_count, error_count,
			 sum_latency_ms, count_latency, sum_ping_latency_ms, count_ping_latency, computed_at)
		SELECT
			monitor_id, model, ?::date,
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE status IN ('operational','degraded'))::int,
			COUNT(*) FILTER (WHERE status = 'operational')::int,
			COUNT(*) FILTER (WHERE status = 'degraded')::int,
			COUNT(*) FILTER (WHERE status = 'failed')::int,
			COUNT(*) FILTER (WHERE status = 'error')::int,
			COALESCE(SUM(latency_ms), 0)::bigint,
			COUNT(latency_ms)::int,
			COALESCE(SUM(ping_latency_ms), 0)::bigint,
			COUNT(ping_latency_ms)::int,
			NOW()
		FROM channel_monitor_histories
		WHERE checked_at >= ?::timestamptz AND checked_at < ?::timestamptz
		GROUP BY monitor_id, model
		ON CONFLICT (monitor_id, model, bucket_date) WHERE deleted_at IS NULL
		DO UPDATE SET
			total_checks = EXCLUDED.total_checks,
			ok_count = EXCLUDED.ok_count,
			operational_count = EXCLUDED.operational_count,
			degraded_count = EXCLUDED.degraded_count,
			failed_count = EXCLUDED.failed_count,
			error_count = EXCLUDED.error_count,
			sum_latency_ms = EXCLUDED.sum_latency_ms,
			count_latency = EXCLUDED.count_latency,
			sum_ping_latency_ms = EXCLUDED.sum_ping_latency_ms,
			count_ping_latency = EXCLUDED.count_ping_latency,
			computed_at = NOW()
	`, dateStr, dateStr, nextDate).Error
}

// DeleteRollupsBefore 软删指定日期之前的聚合行
func (r *ChannelMonitorRepo) DeleteRollupsBefore(beforeDate time.Time) (int64, error) {
	result := r.db.Where("bucket_date < ?", beforeDate.Format("2006-01-02")).
		Delete(&model.ChannelMonitorDailyRollup{})
	return result.RowsAffected, result.Error
}

// ── Aggregation Watermark ─────────────────────────────────────────────────────

// LoadAggregationWatermark 读 watermark
func (r *ChannelMonitorRepo) LoadAggregationWatermark() (*time.Time, error) {
	var wm model.ChannelMonitorAggregationWatermark
	err := r.db.First(&wm).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	t, err := time.Parse("2006-01-02", wm.LastAggregatedDate)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// UpdateAggregationWatermark 写 watermark
func (r *ChannelMonitorRepo) UpdateAggregationWatermark(date time.Time) error {
	return r.db.Exec(`
		INSERT INTO channel_monitor_aggregation_watermark (id, last_aggregated_date, updated_at)
		VALUES (1, ?, NOW())
		ON CONFLICT (id) DO UPDATE SET last_aggregated_date = EXCLUDED.last_aggregated_date, updated_at = NOW()
	`, date.Format("2006-01-02")).Error
}

// ── Availability & Aggregation ────────────────────────────────────────────────

// AvailabilityRow 可用率查询结果行
type AvailabilityRow struct {
	Model             string
	TotalChecks       int
	OperationalChecks int
	AvailabilityPct   float64
	AvgLatencyMs      *int
}

// ComputeAvailability 计算单个监控某窗口内的可用率
func (r *ChannelMonitorRepo) ComputeAvailability(monitorID int64, windowDays int) ([]AvailabilityRow, error) {
	cutoff := time.Now().AddDate(0, 0, -windowDays)
	var rows []AvailabilityRow
	err := r.db.Raw(`
		SELECT
			model,
			COUNT(*)::int AS total_checks,
			COUNT(*) FILTER (WHERE status IN ('operational','degraded'))::int AS operational_checks,
			ROUND(COUNT(*) FILTER (WHERE status IN ('operational','degraded'))::numeric / NULLIF(COUNT(*), 0) * 100, 2) AS availability_pct,
			AVG(latency_ms)::int AS avg_latency_ms
		FROM channel_monitor_histories
		WHERE monitor_id = ? AND checked_at >= ?
		GROUP BY model
	`, monitorID, cutoff).Scan(&rows).Error
	return rows, err
}

// ComputeAvailabilityFromRollups 从日聚合表计算可用率（用于 15d/30d 窗口）
func (r *ChannelMonitorRepo) ComputeAvailabilityFromRollups(monitorID int64, windowDays int) ([]AvailabilityRow, error) {
	cutoff := time.Now().AddDate(0, 0, -windowDays).Format("2006-01-02")
	var rows []AvailabilityRow
	err := r.db.Raw(`
		SELECT
			model,
			SUM(total_checks)::int AS total_checks,
			SUM(ok_count)::int AS operational_checks,
			ROUND(SUM(ok_count)::numeric / NULLIF(SUM(total_checks), 0) * 100, 2) AS availability_pct,
			CASE WHEN SUM(count_latency) > 0
				THEN (SUM(sum_latency_ms) / SUM(count_latency))::int
			END AS avg_latency_ms
		FROM channel_monitor_daily_rollups
		WHERE monitor_id = ? AND bucket_date >= ? AND deleted_at IS NULL
		GROUP BY model
	`, monitorID, cutoff).Scan(&rows).Error
	return rows, err
}

// LatestStatusRow 最近状态查询结果行
type LatestStatusRow struct {
	Model         string
	Status        string
	LatencyMs     *int
	PingLatencyMs *int
	CheckedAt     time.Time
}

// ListLatestPerModel 按模型取最近一次检测状态
func (r *ChannelMonitorRepo) ListLatestPerModel(monitorID int64) ([]LatestStatusRow, error) {
	var rows []LatestStatusRow
	err := r.db.Raw(`
		SELECT DISTINCT ON (model)
			model, status, latency_ms, ping_latency_ms, checked_at
		FROM channel_monitor_histories
		WHERE monitor_id = ?
		ORDER BY model, checked_at DESC
	`, monitorID).Scan(&rows).Error
	return rows, err
}

// ListLatestForMonitorIDs 批量取多个监控的最近模型状态
func (r *ChannelMonitorRepo) ListLatestForMonitorIDs(ids []int64) (map[int64][]LatestStatusRow, error) {
	if len(ids) == 0 {
		return map[int64][]LatestStatusRow{}, nil
	}
	var rows []struct {
		MonitorID     int64
		Model         string
		Status        string
		LatencyMs     *int
		PingLatencyMs *int
		CheckedAt     time.Time
	}
	err := r.db.Raw(`
		SELECT DISTINCT ON (monitor_id, model)
			monitor_id, model, status, latency_ms, ping_latency_ms, checked_at
		FROM channel_monitor_histories
		WHERE monitor_id IN ?
		ORDER BY monitor_id, model, checked_at DESC
	`, ids).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int64][]LatestStatusRow, len(ids))
	for i := range rows {
		row := rows[i]
		result[row.MonitorID] = append(result[row.MonitorID], LatestStatusRow{
			Model:         row.Model,
			Status:        row.Status,
			LatencyMs:     row.LatencyMs,
			PingLatencyMs: row.PingLatencyMs,
			CheckedAt:     row.CheckedAt,
		})
	}
	return result, nil
}

// ComputeAvailabilityForMonitors 批量计算多个监控的可用率
func (r *ChannelMonitorRepo) ComputeAvailabilityForMonitors(ids []int64, windowDays int) (map[int64][]AvailabilityRow, error) {
	cutoff := time.Now().AddDate(0, 0, -windowDays).Format("2006-01-02")
	var rows []struct {
		MonitorID         int64
		Model             string
		TotalChecks       int
		OperationalChecks int
		AvailabilityPct   float64
		AvgLatencyMs      *int
	}
	err := r.db.Raw(`
		SELECT
			monitor_id,
			model,
			SUM(total_checks)::int AS total_checks,
			SUM(ok_count)::int AS operational_checks,
			ROUND(SUM(ok_count)::numeric / NULLIF(SUM(total_checks), 0) * 100, 2) AS availability_pct,
			CASE WHEN SUM(count_latency) > 0
				THEN (SUM(sum_latency_ms) / SUM(count_latency))::int
			END AS avg_latency_ms
		FROM channel_monitor_daily_rollups
		WHERE monitor_id IN ? AND bucket_date >= ? AND deleted_at IS NULL
		GROUP BY monitor_id, model
	`, ids, cutoff).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int64][]AvailabilityRow, len(ids))
	for i := range rows {
		row := rows[i]
		result[row.MonitorID] = append(result[row.MonitorID], AvailabilityRow{
			Model:             row.Model,
			TotalChecks:       row.TotalChecks,
			OperationalChecks: row.OperationalChecks,
			AvailabilityPct:   row.AvailabilityPct,
			AvgLatencyMs:      row.AvgLatencyMs,
		})
	}
	return result, nil
}

// ListRecentHistoryForMonitors 批量取多个监控主模型最近 N 条历史（不含 message）
func (r *ChannelMonitorRepo) ListRecentHistoryForMonitors(ids []int64, primaryModels map[int64]string, perMonitorLimit int) (map[int64][]*model.ChannelMonitorHistory, error) {
	if len(ids) == 0 {
		return map[int64][]*model.ChannelMonitorHistory{}, nil
	}
	// Build UNION ALL for each monitor
	unionParts := make([]string, 0, len(ids))
	args := make([]interface{}, 0, len(ids)*3)
	for _, id := range ids {
		m := primaryModels[id]
		if m == "" {
			continue
		}
		unionParts = append(unionParts, fmt.Sprintf(
			`(SELECT id, monitor_id, model, status, latency_ms, ping_latency_ms, checked_at
			  FROM channel_monitor_histories
			  WHERE monitor_id = ? AND model = ?
			  ORDER BY checked_at DESC LIMIT ?)`,
		))
		args = append(args, id, m, perMonitorLimit)
	}
	if len(unionParts) == 0 {
		return map[int64][]*model.ChannelMonitorHistory{}, nil
	}
	rows := make([]*model.ChannelMonitorHistory, 0)
	err := r.db.Raw(string(fmt.Sprintf(
		"SELECT id, monitor_id, model, status, latency_ms, ping_latency_ms, checked_at FROM (%s) AS sub ORDER BY monitor_id, checked_at DESC",
		joinStrings(unionParts, " UNION ALL "),
	)), args...).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int64][]*model.ChannelMonitorHistory)
	for _, row := range rows {
		result[row.MonitorID] = append(result[row.MonitorID], row)
	}
	return result, nil
}

// ── Request Templates ─────────────────────────────────────────────────────────

// CreateTemplate 创建模板
func (r *ChannelMonitorRepo) CreateTemplate(t *model.ChannelMonitorRequestTemplate) error {
	return r.db.Create(t).Error
}

// UpdateTemplate 更新模板
func (r *ChannelMonitorRepo) UpdateTemplate(t *model.ChannelMonitorRequestTemplate) error {
	return r.db.Save(t).Error
}

// DeleteTemplate 软删除模板
func (r *ChannelMonitorRepo) DeleteTemplate(id int64) error {
	return r.db.Delete(&model.ChannelMonitorRequestTemplate{}, id).Error
}

// GetTemplateByID 按 ID 查询模板
func (r *ChannelMonitorRepo) GetTemplateByID(id int64) (*model.ChannelMonitorRequestTemplate, error) {
	var t model.ChannelMonitorRequestTemplate
	err := r.db.Where("id = ?", id).First(&t).Error
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTemplates 列表查询模板
func (r *ChannelMonitorRepo) ListTemplates(provider string, page, pageSize int) ([]*model.ChannelMonitorRequestTemplate, int64, error) {
	query := r.db.Model(&model.ChannelMonitorRequestTemplate{})
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []*model.ChannelMonitorRequestTemplate
	offset := (page - 1) * pageSize
	err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// ApplyTemplateToMonitors 将模板应用到匹配的监控
func (r *ChannelMonitorRepo) ApplyTemplateToMonitors(templateID int64, provider, apiMode string) (int64, error) {
	query := r.db.Model(&model.ChannelMonitor{}).Where("template_id = ?", templateID)
	if provider != "" {
		query = query.Where("provider = ?", provider)
	}
	if apiMode != "" {
		query = query.Where("api_mode = ?", apiMode)
	}
	result := query.Update("updated_at", time.Now())
	return result.RowsAffected, result.Error
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for i := 1; i < len(ss); i++ {
		result += sep + ss[i]
	}
	return result
}