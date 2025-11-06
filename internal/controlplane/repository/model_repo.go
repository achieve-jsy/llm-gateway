package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ModelRepo 模型数据访问层
type ModelRepo struct {
	db *gorm.DB
}

// NewModelRepo 创建 ModelRepo 实例
func NewModelRepo(db *gorm.DB) *ModelRepo {
	return &ModelRepo{db: db}
}

// Create 注册模型
func (r *ModelRepo) Create(m *model.Model) error {
	// 全字段写入：让 GORM 根据 struct tag 处理所有字段（含 P0 加的 UpstreamModelID/Description/
	// ContextLength/MaxOutputTokens/InputPrice/OutputPrice/Weight 与 CAT-1 加的
	// CatalogID/CtxOverridden/MaxOutputOverridden）。
	// 注意：Provider/Catalog 关联字段需在 Omit 中显式排除，避免 GORM 同时更新关联记录。
	return r.db.Omit("Provider", "Catalog").Create(m).Error
}

// BatchCreate 批量注册模型（一次 INSERT）
func (r *ModelRepo) BatchCreate(models []*model.Model) error {
	if len(models) == 0 {
		return nil
	}
	return r.db.Omit("Provider", "Catalog").CreateInBatches(models, 100).Error
}

// GetByID 按 ID 查询模型
func (r *ModelRepo) GetByID(id uuid.UUID) (*model.Model, error) {
	var m model.Model
	err := r.db.Preload("Provider").First(&m, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// ModelListFilter 模型列表筛选条件
type ModelListFilter struct {
	ProviderID *uuid.UUID
	Name       string
	ModelType  string
}

// List 查询模型列表（支持筛选和分页）
func (r *ModelRepo) List(filter ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
	var models []model.Model
	var total int64

	query := r.db.Model(&model.Model{})
	if filter.ProviderID != nil {
		query = query.Where("provider_id = ?", *filter.ProviderID)
	}
	if filter.Name != "" {
		query = query.Where("name ILIKE ?", "%"+filter.Name+"%")
	}
	if filter.ModelType != "" {
		query = query.Where("model_type = ?", filter.ModelType)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Preload("Provider").Order("created_at DESC").Offset(offset).Limit(limit).Find(&models).Error
	if err != nil {
		return nil, 0, err
	}

	return models, total, nil
}

// Update 更新模型信息
func (r *ModelRepo) Update(m *model.Model) error {
	return r.db.Save(m).Error
}

// Delete 删除模型
func (r *ModelRepo) Delete(id uuid.UUID) error {
	return r.db.Delete(&model.Model{}, "id = ?", id).Error
}

// ExistsByName 检查模型名称是否已存在
func (r *ModelRepo) ExistsByName(name string) (bool, error) {
	var count int64
	err := r.db.Model(&model.Model{}).Where("name = ?", name).Count(&count).Error
	return count > 0, err
}

// ExistsByNameExcluding 检查模型名称是否已存在（排除指定 ID）
func (r *ModelRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.Model(&model.Model{}).Where("name = ? AND id != ?", name, excludeID).Count(&count).Error
	return count > 0, err
}

// CountByProviderID 统计渠道下的模型数量
func (r *ModelRepo) CountByProviderID(providerID uuid.UUID) (int64, error) {
	var count int64
	err := r.db.Model(&model.Model{}).Where("provider_id = ?", providerID).Count(&count).Error
	return count, err
}

// ListAvailable 查询所有启用的模型及其渠道信息（供 gateway-api 内部接口消费）
func (r *ModelRepo) ListAvailable() ([]model.Model, error) {
	var models []model.Model
	err := r.db.Preload("Provider").
		Joins("JOIN providers ON providers.id = models.provider_id").
		Where("models.enabled = ? AND providers.enabled = ?", true, true).
		Find(&models).Error
	return models, err
}

// ListByCatalogID 列出所有 catalog_id 非空的模型（供 catalog 自动同步任务用）。
func (r *ModelRepo) ListByCatalogID() ([]model.Model, error) {
	var models []model.Model
	err := r.db.Where("catalog_id IS NOT NULL").Find(&models).Error
	return models, err
}

// ListUnboundFromCatalog 列出所有 catalog_id 为空的模型（供 catalog 自动绑定任务用）。
func (r *ModelRepo) ListUnboundFromCatalog() ([]model.Model, error) {
	var models []model.Model
	err := r.db.Where("catalog_id IS NULL").Find(&models).Error
	return models, err
}

// SetCatalogID 单独把某个 model 的 catalog_id 设为给定值（auto-bind 用）。
//
// 显式只更新 catalog_id 一列，避免误触 GORM 的全字段 Save 把其他字段覆盖回零值。
func (r *ModelRepo) SetCatalogID(id, catalogID uuid.UUID) error {
	return r.db.Model(&model.Model{}).Where("id = ?", id).Update("catalog_id", catalogID).Error
}

// UpdateEnabledByProviderID 批量更新渠道下所有模型的启用状态
func (r *ModelRepo) UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error {
	return r.db.Model(&model.Model{}).Where("provider_id = ?", providerID).Update("enabled", enabled).Error
}

// BatchDelete 批量删除模型
func (r *ModelRepo) BatchDelete(ids []uuid.UUID) (int64, error) {
	result := r.db.Where("id IN ?", ids).Delete(&model.Model{})
	return result.RowsAffected, result.Error
}

// BatchUpdateEnabled 批量更新模型启用状态
func (r *ModelRepo) BatchUpdateEnabled(ids []uuid.UUID, enabled bool) (int64, error) {
	result := r.db.Model(&model.Model{}).Where("id IN ?", ids).Update("enabled", enabled)
	return result.RowsAffected, result.Error
}

// DeleteModelTagRelations 删除模型的所有标签关联
func (r *ModelRepo) DeleteModelTagRelations(modelID uuid.UUID) error {
	return r.db.Where("model_id = ?", modelID).Delete(&model.ModelTagRelation{}).Error
}

// UpdateEnabled 更新单个模型启用状态
func (r *ModelRepo) UpdateEnabled(id uuid.UUID, enabled *bool) error {
	return r.db.Model(&model.Model{}).Where("id = ?", id).Update("enabled", enabled).Error
}

// IncrementModelFailures 递增模型失败计数并返回新值
func (r *ModelRepo) IncrementModelFailures(id uuid.UUID) (int, error) {
	result := r.db.Model(&model.Model{}).Where("id = ?", id).
		UpdateColumn("consecutive_failures", gorm.Expr("consecutive_failures + 1"))
	if result.Error != nil {
		return 0, result.Error
	}
	var m model.Model
	if err := r.db.Select("consecutive_failures").First(&m, "id = ?", id).Error; err != nil {
		return 0, err
	}
	return m.ConsecutiveFailures, nil
}

// ResetModelFailures 重置模型失败计数为 0
func (r *ModelRepo) ResetModelFailures(id uuid.UUID) error {
	return r.db.Model(&model.Model{}).Where("id = ?", id).Updates(map[string]interface{}{
		"consecutive_failures": 0,
		"last_check_error":     "",
	}).Error
}

// UpdateModelCheckResult 更新模型检测结果（响应时间 + 错误信息）
func (r *ModelRepo) UpdateModelCheckResult(id uuid.UUID, responseTime int, checkError string) error {
	return r.db.Model(&model.Model{}).Where("id = ?", id).Updates(map[string]interface{}{
		"response_time":    responseTime,
		"last_check_error": checkError,
	}).Error
}

// AutoDisableModel 自动禁用模型（连续失败超阈值时调用）
func (r *ModelRepo) AutoDisableModel(id uuid.UUID) error {
	enabled := false
	return r.db.Model(&model.Model{}).Where("id = ?", id).Update("enabled", &enabled).Error
}
