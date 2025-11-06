package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// ProviderDefinitionRepo 渠道定义数据访问层
type ProviderDefinitionRepo struct {
	db *gorm.DB
}

// NewProviderDefinitionRepo 创建实例
func NewProviderDefinitionRepo(db *gorm.DB) *ProviderDefinitionRepo {
	return &ProviderDefinitionRepo{db: db}
}

// List 查询所有启用的渠道定义
func (r *ProviderDefinitionRepo) List() ([]model.ProviderDefinition, error) {
	var defs []model.ProviderDefinition
	err := r.db.Where("enabled = ?", true).Order("builtin DESC, display_name ASC").Find(&defs).Error
	return defs, err
}

// GetByID 按 ID 查询渠道定义
func (r *ProviderDefinitionRepo) GetByID(id string) (*model.ProviderDefinition, error) {
	var def model.ProviderDefinition
	err := r.db.First(&def, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &def, nil
}

// Create 创建自定义渠道定义
func (r *ProviderDefinitionRepo) Create(def *model.ProviderDefinition) error {
	return r.db.Create(def).Error
}

// Update 更新渠道定义
func (r *ProviderDefinitionRepo) Update(def *model.ProviderDefinition) error {
	return r.db.Save(def).Error
}

// Delete 删除渠道定义（内置不可删）
func (r *ProviderDefinitionRepo) Delete(id string) error {
	return r.db.Where("id = ? AND builtin = ?", id, false).Delete(&model.ProviderDefinition{}).Error
}

// ExistsByID 检查 ID 是否已存在
func (r *ProviderDefinitionRepo) ExistsByID(id string) (bool, error) {
	var count int64
	err := r.db.Model(&model.ProviderDefinition{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}
