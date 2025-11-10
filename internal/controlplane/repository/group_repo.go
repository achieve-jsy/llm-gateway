package repository

import (
	"errors"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GroupRepo struct {
	db *gorm.DB
}

func NewGroupRepo(db *gorm.DB) *GroupRepo {
	return &GroupRepo{db: db}
}

func (r *GroupRepo) Create(group *model.Group) error {
	return r.db.Omit("Models").Create(group).Error
}

func (r *GroupRepo) FindByID(id uint) (*model.Group, error) {
	var group model.Group
	err := r.db.Preload("Models").First(&group, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *GroupRepo) FindByName(name string) (*model.Group, error) {
	var group model.Group
	err := r.db.Where("name = ?", name).First(&group).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *GroupRepo) List(search string, page, pageSize int) ([]model.Group, int64, error) {
	var groups []model.Group
	var total int64

	query := r.db.Model(&model.Group{})
	if search != "" {
		query = query.Where("name ILIKE ?", "%"+search+"%")
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	err := query.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&groups).Error
	if err != nil {
		return nil, 0, err
	}

	return groups, total, nil
}

func (r *GroupRepo) Update(group *model.Group) error {
	return r.db.Omit("Models").Save(group).Error
}

func (r *GroupRepo) SoftDelete(id uint) error {
	return r.db.Delete(&model.Group{}, "id = ?", id).Error
}

func (r *GroupRepo) FindGroupWithModels(id uint) (*model.Group, error) {
	var group model.Group
	err := r.db.Preload("Models").First(&group, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

func (r *GroupRepo) ListEnabledGroupsWithModels() ([]model.Group, error) {
	var groups []model.Group
	err := r.db.Preload("Models").Where("enabled = ?", true).Order("created_at DESC").Find(&groups).Error
	return groups, err
}

func (r *GroupRepo) AddModelToGroup(groupID uint, gm *model.GroupModel) error {
	gm.GroupID = groupID
	var existing model.GroupModel
	err := r.db.Where("group_id = ? AND model_id = ?", groupID, gm.ModelID).First(&existing).Error
	if err == nil {
		updates := map[string]interface{}{}
		if gm.Priority != nil {
			updates["priority"] = *gm.Priority
		}
		if gm.Weight != nil {
			updates["weight"] = *gm.Weight
		}
		if len(updates) > 0 {
			return r.db.Model(&existing).Updates(updates).Error
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.Create(gm).Error
}

func (r *GroupRepo) RemoveModelFromGroup(groupID uint, modelID uuid.UUID) error {
	return r.db.Where("group_id = ? AND model_id = ?", groupID, modelID).Delete(&model.GroupModel{}).Error
}

func (r *GroupRepo) ListGroupModels(groupID uint) ([]model.GroupModel, error) {
	var gms []model.GroupModel
	err := r.db.Table("group_models").
		Select("group_models.*, models.name as model_name").
		Joins("JOIN models ON models.id = group_models.model_id").
		Where("group_models.group_id = ?", groupID).
		Order("group_models.id ASC").
		Scan(&gms).Error
	return gms, err
}

func (r *GroupRepo) FindGroupModel(groupID uint, modelID uuid.UUID) (*model.GroupModel, error) {
	var gm model.GroupModel
	err := r.db.Where("group_id = ? AND model_id = ?", groupID, modelID).First(&gm).Error
	if err != nil {
		return nil, err
	}
	return &gm, nil
}

func (r *GroupRepo) CountGroupModels(groupID uint) (int64, error) {
	var count int64
	err := r.db.Model(&model.GroupModel{}).Where("group_id = ?", groupID).Count(&count).Error
	return count, err
}
