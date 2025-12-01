package repository

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	obsmodel "github.com/example/llm-gateway/internal/observability/model"
)

// UsageDailyRepository usage_daily 表操作
type UsageDailyRepository struct {
	db *gorm.DB
}

// NewUsageDailyRepository 创建 UsageDailyRepository
func NewUsageDailyRepository(db *gorm.DB) *UsageDailyRepository {
	return &UsageDailyRepository{db: db}
}

// UpsertRange 整日重算 UPSERT（幂等）
func (r *UsageDailyRepository) UpsertRange(ctx context.Context, dayStart, dayEnd time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		INSERT INTO usage_daily (
			stat_date, key_id, user_id, dept_id, studio_id, model,
			req_count, success_count, prompt_tokens, cached_tokens,
			cache_write_tokens, completion_tokens, reasoning_tokens, est_cost_usd)
		SELECT
			date(ts)                                    AS stat_date,
			key_id,
			COALESCE(MAX(user_id), '')                  AS user_id,
			COALESCE(dept_id, '')                       AS dept_id,
			COALESCE(studio_id, '')                     AS studio_id,
			model,
			COUNT(*)                                    AS req_count,
			COUNT(*) FILTER (WHERE status = 200)        AS success_count,
			SUM(prompt_tokens)                          AS prompt_tokens,
			SUM(cached_tokens)                          AS cached_tokens,
			SUM(cache_write_tokens)                     AS cache_write_tokens,
			SUM(completion_tokens)                      AS completion_tokens,
			SUM(reasoning_tokens)                       AS reasoning_tokens,
			SUM(est_cost_usd)                           AS est_cost_usd
		FROM call_logs
		WHERE ts >= ? AND ts < ?
		GROUP BY date(ts), key_id, COALESCE(dept_id,''), COALESCE(studio_id,''), model
		ON CONFLICT (stat_date, key_id, dept_id, studio_id, model) DO UPDATE SET
			req_count          = EXCLUDED.req_count,
			success_count      = EXCLUDED.success_count,
			prompt_tokens      = EXCLUDED.prompt_tokens,
			cached_tokens      = EXCLUDED.cached_tokens,
			cache_write_tokens = EXCLUDED.cache_write_tokens,
			completion_tokens  = EXCLUDED.completion_tokens,
			reasoning_tokens   = EXCLUDED.reasoning_tokens,
			est_cost_usd       = EXCLUDED.est_cost_usd
	`, dayStart, dayEnd)
	if result.Error != nil {
		return 0, fmt.Errorf("upsert usage_daily: %w", result.Error)
	}
	return result.RowsAffected, nil
}

// QueryRange 查询 usage_daily 指定日期范围数据
func (r *UsageDailyRepository) QueryRange(ctx context.Context,
	from, to time.Time,
	groupBy string,
	scopeFilter string,
	keyID, deptID, model string,
) ([]obsmodel.UsageDaily, error) {
	query := r.db.WithContext(ctx).
		Where("stat_date BETWEEN ? AND ?", from.Format("2006-01-02"), to.Format("2006-01-02"))
	if scopeFilter != "" {
		query = query.Where(scopeFilter)
	}
	if keyID != "" {
		query = query.Where("key_id = ?", keyID)
	}
	if deptID != "" {
		query = query.Where("dept_id = ?", deptID)
	}
	if model != "" {
		query = query.Where("model = ?", model)
	}

	var rows []obsmodel.UsageDaily
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query usage_daily: %w", err)
	}
	return rows, nil
}