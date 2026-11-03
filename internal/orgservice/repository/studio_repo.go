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

// List 只返回 visible=true 的工作室：EHR 里撤并的工作室软删（visible=false）后
// 不该再出现在组织树/下拉框里，但行要留着给历史用量 join。
func (r *StudioRepo) List(page, pageSize int) ([]model.Studio, int64, error) {
	var studios []model.Studio
	var total int64
	q := r.db.Model(&model.Studio{}).Where("visible = ?", true)
	q.Count(&total)
	offset := (page - 1) * pageSize
	err := q.Offset(offset).Limit(pageSize).Order("created_at DESC").Find(&studios).Error
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
