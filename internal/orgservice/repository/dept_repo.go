package repository

import (
	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DeptRepo 部门数据访问（针对 org 扩展字段）
type DeptRepo struct {
	db *gorm.DB
}

func NewDeptRepo(db *gorm.DB) *DeptRepo {
	return &DeptRepo{db: db}
}

// UpsertDepartment 幂等 upsert 部门（仅更新 name，不覆盖 studio_id/status）
// 必须 Omit StudioID/OwnerID——它们在 PG 侧是 uuid/varchar，空字符串会导致
// "invalid input syntax for type uuid" 错误，应写 NULL。
func (r *DeptRepo) UpsertDepartment(deptID, deptName string) error {
	dept := model.OrgDepartment{
		DeptID:   deptID,
		DeptName: deptName,
		Status:   model.DeptStatusPendingStudio,
	}
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dept_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"dept_name"}),
	}).Create(&dept).Error
}

// GetByDeptID 按 SSO dept_id 查询
func (r *DeptRepo) GetByDeptID(deptID string) (*model.OrgDepartment, error) {
	var dept model.OrgDepartment
	err := r.db.Where("dept_id = ?", deptID).First(&dept).Error
	if err != nil {
		return nil, err
	}
	return &dept, nil
}

// UpdateStatus 更新部门的 studio_id 和 status
// studioID 为空字符串时写入 NULL（移出工作室场景）
func (r *DeptRepo) UpdateStatus(deptID string, status model.DeptStatus, studioID string) error {
	var studioVal any
	if studioID == "" {
		studioVal = nil // 写 NULL
	} else {
		studioVal = studioID
	}
	return r.db.Model(&model.OrgDepartment{}).Where("dept_id = ?", deptID).
		UpdateColumns(map[string]any{
			"status":    status,
			"studio_id": studioVal,
		}).Error
}

// ListPending 查询所有 pending_studio 部门
func (r *DeptRepo) ListPending(page, pageSize int) ([]model.OrgDepartment, int64, error) {
	var depts []model.OrgDepartment
	var total int64
	q := r.db.Model(&model.OrgDepartment{}).Where("status = ?", model.DeptStatusPendingStudio)
	q.Count(&total)
	offset := (page - 1) * pageSize
	err := q.Offset(offset).Limit(pageSize).Find(&depts).Error
	return depts, total, err
}

// List 查询部门（可选按 status 过滤）。
// 默认排除软删（status='deleted'）：这些部门只在历史用量/日志里还该出现。
func (r *DeptRepo) List(status string, page, pageSize int) ([]model.OrgDepartment, int64, error) {
	var depts []model.OrgDepartment
	var total int64
	q := r.db.Model(&model.OrgDepartment{})
	if status != "" {
		q = q.Where("status = ?", status)
	} else {
		q = q.Where("status <> ?", model.DeptStatusDeleted)
	}
	q.Count(&total)
	offset := (page - 1) * pageSize
	err := q.Offset(offset).Limit(pageSize).Find(&depts).Error
	return depts, total, err
}

// BackfillActiveByDeptIDs 批量回填已映射部门的 status=active（事务内调用）
func (r *DeptRepo) BackfillActiveByDeptIDs(tx *gorm.DB, deptIDs []string, studioID string) error {
	return tx.Model(&model.OrgDepartment{}).
		Where("dept_id IN ? AND status = ?", deptIDs, model.DeptStatusPendingStudio).
		Updates(map[string]any{
			"status":    model.DeptStatusActive,
			"studio_id": studioID,
		}).Error
}
