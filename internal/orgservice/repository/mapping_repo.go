package repository

import (
	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MappingRepo 部门↔工作室映射数据访问
type MappingRepo struct {
	db *gorm.DB
}

func NewMappingRepo(db *gorm.DB) *MappingRepo {
	return &MappingRepo{db: db}
}

// GetByDeptID 查询单个部门的映射
func (r *MappingRepo) GetByDeptID(deptID string) (*model.DeptStudioMap, error) {
	var m model.DeptStudioMap
	err := r.db.First(&m, "dept_id = ?", deptID).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetByDeptName 按部门名称兜底查映射。
// 用于 SSO 归集时：import 以 dept_name 作为 dept_id 建立了映射，
// 但 SSO 带来的是真实 HR dept_id，两者 key 不同，需按名称做回退匹配。
func (r *MappingRepo) GetByDeptName(deptName string) (*model.DeptStudioMap, error) {
	var m model.DeptStudioMap
	err := r.db.Raw(`
		SELECT dsm.*
		FROM dept_studio_map dsm
		JOIN departments d ON d.dept_id = dsm.dept_id
		WHERE LOWER(d.dept_name) = LOWER(?)
		LIMIT 1
	`, deptName).Scan(&m).Error
	if err != nil {
		return nil, err
	}
	if m.DeptID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	return &m, nil
}

// List 查询所有映射（分页）
func (r *MappingRepo) List(page, pageSize int) ([]model.DeptStudioMap, int64, error) {
	var mappings []model.DeptStudioMap
	var total int64
	r.db.Model(&model.DeptStudioMap{}).Count(&total)
	offset := (page - 1) * pageSize
	err := r.db.Offset(offset).Limit(pageSize).Find(&mappings).Error
	return mappings, total, err
}

// UpsertBatch 在事务中批量 upsert 映射（含回填 pending dept status）
func (r *MappingRepo) UpsertBatch(tx *gorm.DB, rows []model.DeptStudioMap) error {
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dept_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"studio_id", "source", "updated_by", "updated_at"}),
	}).CreateInBatches(rows, 100).Error
}

// GetExistingMappings 批量查询已有映射（用于 diff 预览）
func (r *MappingRepo) GetExistingMappings(deptIDs []string) (map[string]string, error) {
	var mappings []model.DeptStudioMap
	if err := r.db.Where("dept_id IN ?", deptIDs).Find(&mappings).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(mappings))
	for _, m := range mappings {
		result[m.DeptID] = m.StudioID
	}
	return result, nil
}

// Upsert 单行 upsert（手工维护）
func (r *MappingRepo) Upsert(m *model.DeptStudioMap) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dept_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"studio_id", "source", "updated_by", "updated_at"}),
	}).Create(m).Error
}

// DeleteByDeptID 删除部门映射（移出工作室时调用）
func (r *MappingRepo) DeleteByDeptID(deptID string) error {
	return r.db.Where("dept_id = ?", deptID).Delete(&model.DeptStudioMap{}).Error
}
