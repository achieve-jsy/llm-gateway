package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProviderRepo 渠道数据访问层
type ProviderRepo struct {
	db *gorm.DB
}

// NewProviderRepo 创建 ProviderRepo 实例
func NewProviderRepo(db *gorm.DB) *ProviderRepo {
	return &ProviderRepo{db: db}
}

// Create 创建渠道
func (r *ProviderRepo) Create(provider *model.Provider) error {
	return r.db.Create(provider).Error
}

// GetByID 按 ID 查询渠道
func (r *ProviderRepo) GetByID(id uuid.UUID) (*model.Provider, error) {
	var provider model.Provider
	err := r.db.First(&provider, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &provider, nil
}

// ProviderListFilter 渠道列表筛选条件
type ProviderListFilter struct {
	Name         string
	DefinitionID string
	Tag          string
}

// List 查询渠道列表（分页+筛选）
func (r *ProviderRepo) List(filter ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
	var providers []model.Provider
	var total int64

	query := r.db.Model(&model.Provider{})
	if filter.Name != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Name+"%")
	}
	if filter.DefinitionID != "" {
		query = query.Where("definition_id = ?", filter.DefinitionID)
	}
	if filter.Tag != "" {
		// 支持多标签逗号分隔存储：只要 tag 字段包含该标签即命中
		// 为精确匹配每个子标签，使用三种模式：
		//   完全相等（整个字段就是该标签）
		//   以该标签开头（tag="fast,chat"，筛 "fast" → "fast,%"）
		//   以该标签结尾（tag="chat,fast"，筛 "fast" → "%,fast"）
		//   在中间（tag="a,fast,b"，筛 "fast" → "%,fast,%"）
		t := filter.Tag
		query = query.Where(
			"tag = ? OR tag LIKE ? OR tag LIKE ? OR tag LIKE ?",
			t,
			t+",%",
			"%,"+t,
			"%,"+t+",%",
		)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&providers).Error
	if err != nil {
		return nil, 0, err
	}

	return providers, total, nil
}

// Update 更新渠道信息
func (r *ProviderRepo) Update(provider *model.Provider) error {
	return r.db.Save(provider).Error
}

// UpdateStatus 更新渠道启用状态（同时更新 enabled 和 status 字段）
func (r *ProviderRepo) UpdateStatus(id uuid.UUID, enabled bool) error {
	status := 2 // MANUALLY_DISABLED
	if enabled {
		status = 1 // ENABLED
	}
	return r.db.Model(&model.Provider{}).Where("id = ?", id).Updates(map[string]interface{}{
		"enabled": enabled,
		"status":  status,
	}).Error
}

// Delete 删除渠道
func (r *ProviderRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&model.Provider{}, "id = ?", id).Error
}

// ExistsByName 检查渠道名称是否已存在
func (r *ProviderRepo) ExistsByName(name string) (bool, error) {
	var count int64
	err := r.db.Model(&model.Provider{}).Where("name = ?", name).Count(&count).Error
	return count > 0, err
}

// ExistsByNameExcluding 检查渠道名称是否已存在（排除指定 ID）
func (r *ProviderRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&model.Provider{}).Where("name = ? AND id != ?", name, excludeID).Count(&count).Error
	return count > 0, err
}

// UpdateResponseTime 更新渠道最近测试响应时间
func (r *ProviderRepo) UpdateResponseTime(id uuid.UUID, ms int) error {
	return r.db.Model(&model.Provider{}).Where("id = ?", id).Updates(map[string]interface{}{
		"response_time":    ms,
		"last_check_error": "",
	}).Error
}

// UpdateCheckError 更新最近检测错误信息
func (r *ProviderRepo) UpdateCheckError(id uuid.UUID, errMsg string) error {
	return r.db.Model(&model.Provider{}).Where("id = ?", id).Update("last_check_error", errMsg).Error
}

// IncrementFailures 递增失败计数并返回新值。
// 生产 PG 使用 RETURNING 单条 SQL；SQLite 不支持 RETURNING 时回退到 UPDATE+SELECT。
func (r *ProviderRepo) IncrementFailures(id uuid.UUID) (int, error) {
	result := r.db.Model(&model.Provider{}).Where("id = ?", id).
		UpdateColumn("consecutive_failures", gorm.Expr("consecutive_failures + 1"))
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected > 0 {
		var provider model.Provider
		if err := r.db.Select("consecutive_failures").First(&provider, "id = ?", id).Error; err != nil {
			return 0, err
		}
		return provider.ConsecutiveFailures, nil
	}
	return 0, nil
}

// ResetFailures 重置失败计数为 0
func (r *ProviderRepo) ResetFailures(id uuid.UUID) error {
	return r.db.Model(&model.Provider{}).Where("id = ?", id).Update("consecutive_failures", 0).Error
}

// AutoDisable 设置 status=3（自动禁用）并关闭 enabled
func (r *ProviderRepo) AutoDisable(id uuid.UUID) error {
	return r.db.Model(&model.Provider{}).Where("id = ?", id).Updates(map[string]interface{}{
		"enabled": false,
		"status":  3,
	}).Error
}
