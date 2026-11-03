package repository

import (
	"context"
	"errors"
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

// FindEmployeeByKeyword 花名册解析人：employee_id / group_e_number 精确优先，person_name 唯一匹配兜底。
// 重名或查不到都返回 gorm.ErrRecordNotFound —— 手动加负责人不允许猜人。
func (r *AIAppHubRepo) FindEmployeeByKeyword(ctx context.Context, kw string) (*model.EHREmployee, error) {
	if kw == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var emp model.EHREmployee
	err := r.db.WithContext(ctx).Where("employee_id = ? OR group_e_number = ?", kw, kw).First(&emp).Error
	if err == nil {
		return &emp, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var rows []model.EHREmployee
	if err := r.db.WithContext(ctx).Where("person_name = ?", kw).Limit(2).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, gorm.ErrRecordNotFound
	}
	return &rows[0], nil
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

// UserIdentity 本地账号与 EHR 的对应关系：工号 + passport（users.sub）。
type UserIdentity struct {
	EmployeeID string
	Passport   string
}

// ListUserIdentities 取本地全部账号的工号与 passport。
//
// 人员同步靠它反查档案：EHR 没授权"列全部人员"的路由，工号/passport 是仅有的两个可反查键。
// ResignedAccountStats 离职账号关停的统计。
type ResignedAccountStats struct {
	// UsersDisabled 被标记为 disabled 的账号数。
	UsersDisabled int
	// KeysDisabled 被关停的 Key 数。
	KeysDisabled int
	// SkippedRecent 最近还在用 Key、因而被放过的账号数（EHR 说离职但人还在跑流量）。
	SkippedRecent int
}

// resignedUsageWindow 误判保护窗口：最近这么久还在用 Key 的账号不封。
const resignedUsageWindow = 30 * 24 * time.Hour

// DisableResignedAccounts 关停离职员工的账号：软删除 users（deleted_at）+ Key 置 enabled=false。
//
// 三层一起做，各管一段路：
//   - users.deleted_at：管理页/组织统计里直接消失（GORM 查询自动过滤），行还在，审计和
//     历史调用记录仍指得到；SSO 登录在 upsertUser 里按 Unscoped 拦，不会删了又建。
//   - api_keys.enabled=false：网关 verify-key 只看这个字段（middleware/auth.go 里
//     status != active 直接 401），缓存 15 秒失效 —— 真正挡住流量的那一刀。
//   - login_status='disabled'：老数据/老页面还在读它，顺手写上保持一致。
//
// 误判保护：最近 30 天还在用 Key 的人不动 —— 先记日志交人工核对，别把在跑业务的人踢下线。
// 恢复（再入职/误判）走人工：把 enabled / login_status 改回去。
//
// ponytail: 30 天窗口是拍的，线上先观察一轮再定；要更细的规则（例如只封 0 流量的）再加。
func (r *AIAppHubRepo) DisableResignedAccounts(ctx context.Context, numbers []string) (ResignedAccountStats, error) {
	var stats ResignedAccountStats
	if len(numbers) == 0 {
		return stats, nil
	}
	cutoff := time.Now().Add(-resignedUsageWindow)

	// 最近还在用的账号：只统计、不封。
	if err := r.db.WithContext(ctx).Raw(
		"SELECT count(DISTINCT u.id) FROM users u JOIN api_keys k ON k.user_id = u.id "+
			"WHERE u.employee_id IN ? AND k.last_used_at > ?", numbers, cutoff).Scan(&stats.SkippedRecent).Error; err != nil {
		return stats, err
	}

	guard := "NOT EXISTS (SELECT 1 FROM api_keys k WHERE k.user_id = users.id AND k.last_used_at > ?)"

	keyRes := r.db.WithContext(ctx).Exec(
		"UPDATE api_keys SET enabled = false, updated_at = ? WHERE enabled AND user_id IN "+
			"(SELECT id FROM users WHERE employee_id IN ? AND "+guard+")",
		time.Now(), numbers, cutoff) // 注意：这里不按 deleted_at 过滤，已软删的账号再来一遍也要把 Key 关干净
	if keyRes.Error != nil {
		return stats, keyRes.Error
	}
	stats.KeysDisabled = int(keyRes.RowsAffected)

	now := time.Now()
	userRes := r.db.WithContext(ctx).Exec(
		"UPDATE users SET deleted_at = ?, login_status = 'disabled', updated_at = ? "+
			"WHERE employee_id IN ? AND deleted_at IS NULL AND "+guard,
		now, now, numbers, cutoff)
	if userRes.Error != nil {
		return stats, userRes.Error
	}
	stats.UsersDisabled = int(userRes.RowsAffected)
	return stats, nil
}

func (r *AIAppHubRepo) ListUserIdentities(ctx context.Context) ([]UserIdentity, error) {
	var rows []UserIdentity
	err := r.db.WithContext(ctx).Table("users").
		Select("coalesce(employee_id, '') AS employee_id, coalesce(sub, '') AS passport").
		Where("coalesce(employee_id, '') <> '' OR coalesce(sub, '') <> ''").
		Scan(&rows).Error
	return rows, err
}

// UpsertEmployees 按 (org_id, employee_id) 幂等写入人员档案，返回写入行数。
//
// 查不到档案的 id（离职/失效）不会出现在入参里，所以这里不做删除；
// 人员只是展示用数据，宁可保留最后一次已知状态，也不要因为一次拉取失败把人抹掉。
//
// ponytail: 线上还有 UNIQUE(passport) 单列索引。若上游某批里出现重复或空 passport，
// 整批会失败（服务层只告警）。日常数据不会触发；真遇到时改成逐行 upsert 并跳过冲突行。
func (r *AIAppHubRepo) UpsertEmployees(ctx context.Context, emps []model.EHREmployee) (int, error) {
	if len(emps) == 0 {
		return 0, nil
	}
	err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "org_id"}, {Name: "employee_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"person_id", "passport", "person_name", "group_e_number", "email",
				"office", "appoint_name", "appoint_code", "department_id", "department_name",
				"person_status", "person_type", "gender", "is_default", "location",
				"manager_employee_id", "manager_person_id", "manager_passport", "manager_name",
				"join_date", "on_board_date", "ehr_modify_date", "synced_at", "updated_at",
			}),
		}).
		CreateInBatches(emps, 100).Error
	return len(emps), err
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

// HideStudiosNotInDepartments 把"EHR 里已经不存在"的 EHR 来源工作室隐藏（visible=false）。
//
// 只动 source='ehr' 且有 department_id 的行：manual 覆盖行与手工工作室（department_id IS NULL）
// 永不参与对账；不做物理删除，ai_apps 挂在工作室行下。keep 为空表示"没有任何部门存活"，
// 调用方（同步服务）负责保证上游异常时不传空。
func (r *AIAppHubRepo) HideStudiosNotInDepartments(ctx context.Context, keep []string) (int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AIAppStudio{}).
		Where("source = ?", "ehr").
		Where("department_id IS NOT NULL").
		Where("department_id <> ''").
		Where("visible = ?", true)
	if len(keep) > 0 {
		q = q.Where("department_id NOT IN ?", keep)
	}
	res := q.Updates(map[string]any{"visible": false, "updated_at": time.Now()})
	return res.RowsAffected, res.Error
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
