package repository

import (
	"github.com/example/llm-gateway/internal/auth/model"
	"gorm.io/gorm"
)

type DeptRepository struct{ db *gorm.DB }

func NewDeptRepository(db *gorm.DB) *DeptRepository { return &DeptRepository{db: db} }

func (r *DeptRepository) FindByDeptID(deptID string) (*model.Department, error) {
	var dept model.Department
	if err := r.db.Where("dept_id = ?", deptID).First(&dept).Error; err != nil {
		return nil, err
	}
	return &dept, nil
}

func (r *DeptRepository) Create(dept *model.Department) error {
	return r.db.Create(dept).Error
}

func (r *DeptRepository) Update(dept *model.Department) error {
	return r.db.Save(dept).Error
}
