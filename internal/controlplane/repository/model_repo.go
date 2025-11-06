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
	return r.db.Select("Name", "ProviderID", "ModelType", "Enabled").Create(m).Error
}

// BatchCreate 批量注册模型（一次 INSERT）
func (r *ModelRepo) BatchCreate(models []*model.Model) error {
	if len(models) == 0 {
		return nil
	}
	return r.db.Select("Name", "ProviderID", "ModelType", "Enabled").CreateInBatches(models, 100).Error
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
