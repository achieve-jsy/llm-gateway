package repository

import (
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ModelTagRepo 模型标签数据访问层
type ModelTagRepo struct {
	db *gorm.DB
}

// NewModelTagRepo 创建实例
func NewModelTagRepo(db *gorm.DB) *ModelTagRepo {
	return &ModelTagRepo{db: db}
}

// ========== 标签 CRUD ==========

// CreateTag 创建标签
func (r *ModelTagRepo) CreateTag(tag *model.ModelTag) error {
	return r.db.Create(tag).Error
}

// UpdateTag 更新标签
func (r *ModelTagRepo) UpdateTag(tag *model.ModelTag) error {
	return r.db.Save(tag).Error
}

// DeleteTag 删除标签（级联删除关联关系）
func (r *ModelTagRepo) DeleteTag(id uuid.UUID) error {
	// 先删关联
	r.db.Where("tag_id = ?", id).Delete(&model.ModelTagRelation{})
	return r.db.Delete(&model.ModelTag{}, "id = ?", id).Error
}

// GetTagByID 按 ID 查询
func (r *ModelTagRepo) GetTagByID(id uuid.UUID) (*model.ModelTag, error) {
	var tag model.ModelTag
	err := r.db.First(&tag, "id = ?", id).Error
	return &tag, err
}

// ListTags 查询所有标签（按 sort_order 降序）
func (r *ModelTagRepo) ListTags() ([]model.ModelTag, error) {
	var tags []model.ModelTag
	err := r.db.Order("sort_order DESC, name ASC").Find(&tags).Error
	return tags, err
}

// ExistsTagByName 检查标签名是否存在
func (r *ModelTagRepo) ExistsTagByName(name string) (bool, error) {
	var count int64
	err := r.db.Model(&model.ModelTag{}).Where("name = ?", name).Count(&count).Error
	return count > 0, err
}

// ========== 关联操作 ==========

// SetModelTags 设置模型的标签（全量替换）
func (r *ModelTagRepo) SetModelTags(modelID uuid.UUID, tagIDs []uuid.UUID) error {
	// 先删除该模型所有旧关联
	if err := r.db.Where("model_id = ?", modelID).Delete(&model.ModelTagRelation{}).Error; err != nil {
		return err
	}

	// 批量创建新关联
	if len(tagIDs) == 0 {
		return nil
	}

	relations := make([]model.ModelTagRelation, len(tagIDs))
	for i, tagID := range tagIDs {
		relations[i] = model.ModelTagRelation{
			ModelID: modelID,
			TagID:   tagID,
		}
	}
	return r.db.Create(&relations).Error
}

// GetModelTags 获取模型的所有标签
func (r *ModelTagRepo) GetModelTags(modelID uuid.UUID) ([]model.ModelTag, error) {
	var tags []model.ModelTag
	err := r.db.
		Joins("JOIN model_tag_relations ON model_tag_relations.tag_id = model_tags.id").
		Where("model_tag_relations.model_id = ?", modelID).
		Order("model_tags.sort_order DESC").
		Find(&tags).Error
	return tags, err
}

// GetModelIDsByTagID 获取某标签下的所有模型 ID
func (r *ModelTagRepo) GetModelIDsByTagID(tagID uuid.UUID) ([]uuid.UUID, error) {
	var relations []model.ModelTagRelation
	err := r.db.Where("tag_id = ?", tagID).Find(&relations).Error
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(relations))
	for i, rel := range relations {
		ids[i] = rel.ModelID
	}
	return ids, err
}

// BatchGetModelTags 批量获取多个模型的标签（减少 N+1 查询）
func (r *ModelTagRepo) BatchGetModelTags(modelIDs []uuid.UUID) (map[uuid.UUID][]model.ModelTag, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}

	type result struct {
		ModelID uuid.UUID
		model.ModelTag
	}

	var results []result
	err := r.db.
		Table("model_tags").
		Select("model_tag_relations.model_id, model_tags.*").
		Joins("JOIN model_tag_relations ON model_tag_relations.tag_id = model_tags.id").
		Where("model_tag_relations.model_id IN ?", modelIDs).
		Order("model_tags.sort_order DESC").
		Scan(&results).Error
	if err != nil {
		return nil, err
	}

	tagMap := make(map[uuid.UUID][]model.ModelTag)
	for _, r := range results {
		tagMap[r.ModelID] = append(tagMap[r.ModelID], r.ModelTag)
	}
	return tagMap, nil
}
