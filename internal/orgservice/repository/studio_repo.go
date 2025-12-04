package repository

import (
	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
)

// StudioRepo 工作室数据访问
type StudioRepo struct {
	db *gorm.DB
}

func NewStudioRepo(db *gorm.DB) *StudioRepo {
	return &StudioRepo{db: db}
}

func (r *StudioRepo) Create(studio *model.Studio) error {
	return r.db.Create(studio).Error
}

func (r *StudioRepo) List(page, pageSize int) ([]model.Studio, int64, error) {
	var studios []model.Studio
	var total int64
	r.db.Model(&model.Studio{}).Count(&total)
	offset := (page - 1) * pageSize
	err := r.db.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&studios).Error
	return studios, total, err
}

func (r *StudioRepo) GetByID(id string) (*model.Studio, error) {
	var studio model.Studio
	err := r.db.First(&studio, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &studio, nil
}

func (r *StudioRepo) ExistsByIDs(ids []string) (map[string]bool, error) {
	var studios []model.Studio
	if err := r.db.Select("id").Where("id IN ?", ids).Find(&studios).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(ids))
	for _, s := range studios {
		result[s.ID] = true
	}
	return result, nil
}

func (r *StudioRepo) Delete(id string) error {
	return r.db.Delete(&model.Studio{}, "id = ?", id).Error
}

func (r *StudioRepo) HasDepartments(studioID string) (bool, error) {
	var count int64
	err := r.db.Model(&model.OrgDepartment{}).Where("studio_id = ?", studioID).Count(&count).Error
	return count > 0, err
}
