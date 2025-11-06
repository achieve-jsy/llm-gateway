package repository

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ModelCatalogRepo model_catalog 表的数据访问层。
//
// 选型说明：
//   - Upsert 走 ON CONFLICT (source, source_model_id) DO UPDATE，避免先 SELECT 再分支；
//   - Search 默认 LIKE 大小写敏感（PG 用 ILIKE，但 sqlite 默认不支持 ILIKE）；
//     这里用 LOWER(...) LIKE LOWER(...) 兼容两端；
//   - 列表/搜索默认按 created_at DESC 倒序，符合"最新 sync 进来的优先曝光"的产品直觉。
type ModelCatalogRepo struct {
	db *gorm.DB
}

func NewModelCatalogRepo(db *gorm.DB) *ModelCatalogRepo {
	return &ModelCatalogRepo{db: db}
}

// CatalogSearchOpts 用于 Search 的入参集合。
type CatalogSearchOpts struct {
	Query    string // 模糊匹配 source_model_id 或 display_name
	Provider string // 精确匹配 provider
	Mode     string // 精确匹配 mode（chat/embedding/...）
	Source   string // 精确匹配 source（缺省 'litellm'）
	Limit    int    // 0 → 默认 20
}

// CatalogListFilter 用于 List 的入参集合。
type CatalogListFilter struct {
	Provider string
	Mode     string
	Source   string
}

// Upsert 按 (source, source_model_id) 唯一键插入或更新。
//
// gorm/clause OnConflict 在 sqlite + glebarez 驱动下也能工作；
// 这里没法依赖 PG 端 default gen_random_uuid()，因此调用方应该提前生成 ID。
func (r *ModelCatalogRepo) Upsert(entry *model.ModelCatalog) error {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.SyncedAt.IsZero() {
		entry.SyncedAt = time.Now()
	}
	if entry.Source == "" {
		entry.Source = "litellm"
	}

	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source"}, {Name: "source_model_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"display_name", "provider", "mode",
			"context_length", "max_output_tokens",
			"input_price_per_million", "output_price_per_million",
			"cache_read_price", "cache_write_price",
			"tier_threshold", "input_price_above_tier", "output_price_above_tier",
			"supports_function_calling", "supports_vision",
			"supports_prompt_caching", "supports_response_schema",
			"supports_tool_choice", "supports_parallel_function_calling",
			"deprecation_date", "raw_payload", "synced_at", "updated_at",
		}),
	}).Create(entry).Error
}

// BatchUpsert 批量 Upsert（一次事务）。
func (r *ModelCatalogRepo) BatchUpsert(entries []*model.ModelCatalog) error {
	if len(entries) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		repo := &ModelCatalogRepo{db: tx}
		for _, e := range entries {
			if err := repo.Upsert(e); err != nil {
				return fmt.Errorf("upsert %s/%s: %w", e.Source, e.SourceModelID, err)
			}
		}
		return nil
	})
}

// GetByID 按主键查询；找不到时返回 (nil, nil) 而非 ErrRecordNotFound，
// 上层 handler 据此返回 404。
func (r *ModelCatalogRepo) GetByID(id uuid.UUID) (*model.ModelCatalog, error) {
	var c model.ModelCatalog
	err := r.db.First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetBySourceModelID 按 (source, source_model_id) 查询。
func (r *ModelCatalogRepo) GetBySourceModelID(source, sourceModelID string) (*model.ModelCatalog, error) {
	var c model.ModelCatalog
	err := r.db.Where("source = ? AND source_model_id = ?", source, sourceModelID).First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// Search 模糊匹配。
//
// 大小写处理：用 LOWER(col) LIKE LOWER(?) 同时兼容 sqlite/PG，且对中英文都安全。
// query 包含 '%' '_' 时不做转义（视为低优先项）。
//
// 返回 (results, total, err)：
//   - results 是按 limit 分页后的数据；
//   - total 是匹配 where 条件的"全表总数"（不受 limit 影响），
//     供前端展示"目录已收录 N 条"这类元信息。前端误把 len(results) 当成 total
//     会导致 limit=1 时显示"目录里只有 1 条"。
func (r *ModelCatalogRepo) Search(opts CatalogSearchOpts) ([]model.ModelCatalog, int64, error) {
	q := r.db.Model(&model.ModelCatalog{})
	if opts.Source != "" {
		q = q.Where("source = ?", opts.Source)
	}
	if opts.Provider != "" {
		q = q.Where("provider = ?", opts.Provider)
	}
	if opts.Mode != "" {
		q = q.Where("mode = ?", opts.Mode)
	}
	if s := strings.TrimSpace(opts.Query); s != "" {
		pattern := "%" + strings.ToLower(s) + "%"
		q = q.Where(
			"LOWER(source_model_id) LIKE ? OR LOWER(COALESCE(display_name, '')) LIKE ?",
			pattern, pattern,
		)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	limit := opts.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	var results []model.ModelCatalog
	if err := q.Order("created_at DESC").Limit(limit).Find(&results).Error; err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// List 按筛选 + 分页查询全部。
func (r *ModelCatalogRepo) List(filter CatalogListFilter, offset, limit int) ([]model.ModelCatalog, int64, error) {
	q := r.db.Model(&model.ModelCatalog{})
	if filter.Source != "" {
		q = q.Where("source = ?", filter.Source)
	}
	if filter.Provider != "" {
		q = q.Where("provider = ?", filter.Provider)
	}
	if filter.Mode != "" {
		q = q.Where("mode = ?", filter.Mode)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	var results []model.ModelCatalog
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&results).Error; err != nil {
		return nil, 0, err
	}
	return results, total, nil
}

// DeleteStale 删除给定 source 下，syncedAt 早于 cutoff 的旧条目。
// 用于"远程 fetch 成功后清理本地多余存量"。
func (r *ModelCatalogRepo) DeleteStale(source string, cutoff time.Time) (int64, error) {
	res := r.db.Where("source = ? AND synced_at < ?", source, cutoff).Delete(&model.ModelCatalog{})
	return res.RowsAffected, res.Error
}

// ListAll 返回全量 catalog 条目，用于 auto-bind 任务在内存里做 hash join。
//
// catalog 量级在 10K 以内（LiteLLM 当前 ~3K），全量内存匹配比 N+1 query 简单稳。
func (r *ModelCatalogRepo) ListAll() ([]model.ModelCatalog, error) {
	var results []model.ModelCatalog
	err := r.db.Find(&results).Error
	return results, err
}

// Count 返回 catalog 表中的总条目数。
//
// 用于"目录同步状态"展示（前端页头的「目录已收录 N 条」）。
// 之所以单独拆出来，是为了避免前端拿元信息时还要走 Search() 路径——
// 那条路径会附带分页 LIMIT，total 字段语义容易出错。
func (r *ModelCatalogRepo) Count() (int64, error) {
	var n int64
	err := r.db.Model(&model.ModelCatalog{}).Count(&n).Error
	return n, err
}
