package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DeptPolicyRepo struct {
	db *gorm.DB
}

func NewDeptPolicyRepo(db *gorm.DB) *DeptPolicyRepo {
	return &DeptPolicyRepo{db: db}
}

// GetPolicy 查询某部门某策略的值；不存在返回空字符串与 gorm.ErrRecordNotFound。
func (r *DeptPolicyRepo) GetPolicy(deptID, policyKey string) (*model.DeptPolicy, error) {
	var p model.DeptPolicy
	err := r.db.Where("dept_id = ? AND policy_key = ?", deptID, policyKey).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ListByDept 查询某部门所有策略。
func (r *DeptPolicyRepo) ListByDept(deptID string) ([]model.DeptPolicy, error) {
	var items []model.DeptPolicy
	err := r.db.Where("dept_id = ?", deptID).Find(&items).Error
	return items, err
}

// Upsert 创建或更新一条部门策略（PostgreSQL ON CONFLICT）。
func (r *DeptPolicyRepo) Upsert(deptID, policyKey, policyValue string, updatedBy uuid.UUID) error {
	p := model.DeptPolicy{
		DeptID:      deptID,
		PolicyKey:   policyKey,
		PolicyValue: policyValue,
		UpdatedBy:   updatedBy,
		UpdatedAt:   time.Now(),
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dept_id"}, {Name: "policy_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"policy_value", "updated_by", "updated_at"}),
	}).Create(&p).Error
}

// Delete 删除一条部门策略。
func (r *DeptPolicyRepo) Delete(deptID, policyKey string) error {
	return r.db.Where("dept_id = ? AND policy_key = ?", deptID, policyKey).Delete(&model.DeptPolicy{}).Error
}
