package repository

import (
	"context"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// insertStudioSQL 用原生 INSERT 而不是 GORM Create：visible=false / sort_order=0
// 是"首次入库要写进去的值"，带 default 标签的字段走结构体插入会被当成零值省略，
// 落成数据库默认的 true。ON CONFLICT DO NOTHING 保证已存在的行一个字段都不动。
const insertStudioSQL = "INSERT INTO ai_app_studios (id, department_id, name, leader_name, leader_passport, leader_employee_id, source, visible, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (department_id) DO NOTHING"

// AIAppHubRepo AI 应用聚合页的持久化入口：工作室主数据 + EHR 部门镜像。
//
// 与 legacy 的 studios/dept_studio_map 无关，见 model.AIAppStudio 的说明。
type AIAppHubRepo struct {
	db *gorm.DB
}

func NewAIAppHubRepo(db *gorm.DB) *AIAppHubRepo {
	return &AIAppHubRepo{db: db}
}

// InsertStudioIfAbsent 按 department_id 插入工作室；已存在时什么都不改，返回 false。
func (r *AIAppHubRepo) InsertStudioIfAbsent(ctx context.Context, s *model.AIAppStudio) (bool, error) {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	var deptID any
	if s.DepartmentID != nil {
		deptID = *s.DepartmentID
	}
	now := time.Now()
	res := r.db.WithContext(ctx).Exec(insertStudioSQL,
		s.ID, deptID, s.Name, s.LeaderName, s.LeaderPassport, s.LeaderEmployeeID,
		s.Source, s.Visible, s.SortOrder, now, now)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// UpdateStudioEHRFields 只更新 EHR 可推导的字段；visible / sort_order 属运营配置，
// 同步永不触碰。
func (r *AIAppHubRepo) UpdateStudioEHRFields(ctx context.Context, id uuid.UUID, name, leaderName, leaderPassport, leaderEmployeeID string) error {
	return r.db.WithContext(ctx).Model(&model.AIAppStudio{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"name":               name,
			"leader_name":        leaderName,
			"leader_passport":    leaderPassport,
			"leader_employee_id": leaderEmployeeID,
		}).Error
}

func (r *AIAppHubRepo) GetStudioByID(ctx context.Context, id uuid.UUID) (*model.AIAppStudio, error) {
	var s model.AIAppStudio
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *AIAppHubRepo) GetStudioByDepartmentID(ctx context.Context, deptID string) (*model.AIAppStudio, error) {
	var s model.AIAppStudio
	if err := r.db.WithContext(ctx).Where("department_id = ?", deptID).First(&s).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *AIAppHubRepo) ListStudios(ctx context.Context, onlyVisible bool) ([]model.AIAppStudio, error) {
	q := r.db.WithContext(ctx).Model(&model.AIAppStudio{})
	if onlyVisible {
		q = q.Where("visible = ?", true)
	}
	var list []model.AIAppStudio
	if err := q.Order("sort_order, name").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *AIAppHubRepo) UpsertDepartment(ctx context.Context, d *model.AIAppDepartment) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "department_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name", "parent_id", "department_type", "department_path",
				"leader_name", "leader_passport", "synced_at",
			}),
		}).
		Create(d).Error
}

func (r *AIAppHubRepo) GetDepartment(ctx context.Context, deptID string) (*model.AIAppDepartment, error) {
	var d model.AIAppDepartment
	if err := r.db.WithContext(ctx).Where("department_id = ?", deptID).First(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *AIAppHubRepo) ListDepartments(ctx context.Context) ([]model.AIAppDepartment, error) {
	var list []model.AIAppDepartment
	if err := r.db.WithContext(ctx).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// SetDepartmentStudio 回填节点所属工作室；studioID 为 nil 表示没有事业部祖先。
func (r *AIAppHubRepo) SetDepartmentStudio(ctx context.Context, deptID string, studioID *uuid.UUID) error {
	return r.db.WithContext(ctx).Model(&model.AIAppDepartment{}).
		Where("department_id = ?", deptID).
		Update("studio_id", studioID).Error
}

// HasAnyRole 查 legacy 的 user_roles 表（migrations/043）。真实列名是 user_id，
// 不是 passport；该表没有纳入 Atlas 迁移，因此直接用表名查询，不建 GORM model。
//
// identities 同时接受 JWT 的 user_id（users.id）与 EHR passport：历史 token 可能只有后者。
func (r *AIAppHubRepo) HasAnyRole(ctx context.Context, identities []string, roles []string) (bool, error) {
	ids := make([]string, 0, len(identities))
	for _, id := range identities {
		if id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(roles) == 0 {
		return false, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Table("user_roles").
		Where("user_id IN ? AND role IN ?", ids, roles).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
