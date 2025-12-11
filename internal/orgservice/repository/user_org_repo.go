package repository

import (
	"time"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
)

// UserOrgRepo 用户组织归属数据访问（补充 auth 包中 UserRepository 未覆盖的 org 功能）
type UserOrgRepo struct {
	db *gorm.DB
}

func NewUserOrgRepo(db *gorm.DB) *UserOrgRepo {
	return &UserOrgRepo{db: db}
}

// userDeptRow 内部查询结构
type userDeptRow struct {
	Sub    string `gorm:"column:sub"`
	DeptID string `gorm:"column:dept_id_str"` // departments.dept_id（SSO dept_id 字符串）
}

// GetUserDeptIDByUserID 根据 users.id 获取该用户归属的 SSO dept_id
func (r *UserOrgRepo) GetUserDeptIDByUserID(userID string) (string, error) {
	var row userDeptRow
	err := r.db.Raw(`
		SELECT u.id::text AS sub, d.dept_id AS dept_id_str
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		WHERE u.id::text = ?
	`, userID).Scan(&row).Error
	if err != nil {
		return "", err
	}
	return row.DeptID, nil
}

// UpsertUserDept 幂等更新 users.dept_id（按 sub/passport 匹配）
func (r *UserOrgRepo) UpsertUserDept(userSub string, deptUUID string) error {
	// deptUUID 是 departments.id（UUID），不是 dept_id（varchar）
	return r.db.Exec(`
		UPDATE users SET dept_id = ?::uuid WHERE sub = ?
	`, deptUUID, userSub).Error
}

// InsertMigrationLog 写入迁移日志
func (r *UserOrgRepo) InsertMigrationLog(log *model.OrgMigrationLog) error {
	return r.db.Create(log).Error
}

// GetDeptByUUID 通过 departments.id（UUID 字符串）查询部门（含 studio_id）
// 使用原生 uuid 类型比较，大小写不敏感
func (r *UserOrgRepo) GetDeptByUUID(deptUUID string) (*model.OrgDepartment, error) {
	var dept model.OrgDepartment
	err := r.db.Where("id = ?::uuid", deptUUID).First(&dept).Error
	if err != nil {
		return nil, err
	}
	return &dept, nil
}

// GetDeptBySSO 通过 SSO dept_id（varchar 业务 ID）查询部门（含 studio_id）
func (r *UserOrgRepo) GetDeptBySSO(ssoID string) (*model.OrgDepartment, error) {
	var dept model.OrgDepartment
	err := r.db.Where("dept_id = ?", ssoID).First(&dept).Error
	if err != nil {
		return nil, err
	}
	return &dept, nil
}


// ListDeptIDsByStudioIDs 查询属于指定工作室集合的所有 dept_id（用于角色列表过滤）
func (r *UserOrgRepo) ListDeptIDsByStudioIDs(studioIDs []string) ([]string, error) {
	if len(studioIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		DeptID string `gorm:"column:dept_id"`
	}
	err := r.db.Raw(
		`SELECT dept_id FROM departments WHERE studio_id::text = ANY(?)`,
		studioIDs,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.DeptID)
	}
	return ids, nil
}

// GetStudioIDByDeptIDStr 通过 SSO dept_id 字符串查询关联的 studio_id
func (r *UserOrgRepo) GetStudioIDByDeptIDStr(deptID string) (string, error) {
	var d model.OrgDepartment
	err := r.db.Select("studio_id").Where("dept_id = ?", deptID).First(&d).Error
	if err != nil {
		return "", err
	}
	if d.StudioID == nil {
		return "", nil
	}
	return *d.StudioID, nil
}

// UserSearchResult 用户搜索结果
type UserSearchResult struct {
	ID       string `gorm:"column:id" json:"id"`
	Name     string `gorm:"column:name" json:"name"`
	Email    string `gorm:"column:email" json:"email"`
	DeptName string `gorm:"column:dept_name" json:"dept_name,omitempty"`
	DeptID   string `gorm:"column:dept_id_str" json:"dept_id,omitempty"`
}

// DeptMemberInfo 部门成员查询结果
type DeptMemberInfo struct {
	ID          string `gorm:"column:id"           json:"id"`
	Name        string `gorm:"column:name"         json:"name"`
	Email       string `gorm:"column:email"        json:"email"`
	JoinedAt    string `gorm:"column:joined_at"    json:"joined_at"`
	EmployeeID  string `gorm:"column:employee_id"  json:"employee_id,omitempty"`
	JobTitle    string `gorm:"column:job_title"    json:"job_title,omitempty"`
	JobFamily   string `gorm:"column:job_family"   json:"job_family,omitempty"`
	JobCategory string `gorm:"column:job_category" json:"job_category,omitempty"`
	DeptSource  string `gorm:"column:dept_source"  json:"dept_source"`
	LoginStatus string `gorm:"column:login_status" json:"login_status"`
	HasConflict bool   `gorm:"column:has_conflict" json:"has_conflict"`
}

// ListMembersByDeptID 列出指定部门（按 SSO dept_id 字符串）的成员，支持分页
func (r *UserOrgRepo) ListMembersByDeptID(deptID string, page, pageSize int) ([]DeptMemberInfo, int64, error) {
	var total int64
	if err := r.db.Raw(`
		SELECT COUNT(*) FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE d.dept_id = ?
	`, deptID).Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	var members []DeptMemberInfo
	err := r.db.Raw(`
		SELECT u.id::text AS id, u.name, u.email,
		       TO_CHAR(u.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD') AS joined_at,
		       COALESCE(u.employee_id, '')  AS employee_id,
		       COALESCE(u.job_title, '')    AS job_title,
		       COALESCE(u.job_family, '')   AS job_family,
		       COALESCE(u.job_category, '') AS job_category,
		       COALESCE(u.dept_source, 'sso') AS dept_source,
		       COALESCE(u.login_status, 'active') AS login_status,
		       EXISTS(
		           SELECT 1 FROM user_dept_conflicts c
		           WHERE c.user_id = u.id
		           ORDER BY c.detected_at DESC LIMIT 1
		       ) AS has_conflict
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE d.dept_id = ?
		ORDER BY u.name
		LIMIT ? OFFSET ?
	`, deptID, pageSize, offset).Scan(&members).Error
	return members, total, err
}

// UpdateUserDepartment 手动修改成员部门（由 admin / studio_admin 调用）
// toDeptsID: departments.id（UUID），operatorID: 操作者 user_id
func (r *UserOrgRepo) UpdateUserDepartment(userID, toDeptUUID, operatorID string) error {
	now := time.Now()
	return r.db.Exec(`
		UPDATE users SET
		    dept_id         = ?::uuid,
		    dept_source     = 'manual',
		    dept_updated_by = ?,
		    dept_updated_at = ?
		WHERE id::text = ?
	`, toDeptUUID, operatorID, now, userID).Error
}

// FollowSSO 恢复跟随 SSO（将 dept_source 改回 sso）
func (r *UserOrgRepo) FollowSSO(userID string) error {
	return r.db.Exec(`
		UPDATE users SET dept_source = 'sso', dept_updated_by = NULL, dept_updated_at = NULL
		WHERE id::text = ?
	`, userID).Error
}

// GetDeptSourceForUser 获取指定用户的 dept_source 和当前 dept_id
type UserDeptSourceRow struct {
	DeptSource string `gorm:"column:dept_source"`
	DeptID     string `gorm:"column:dept_id"` // departments.id (UUID)
}

func (r *UserOrgRepo) GetDeptSourceForUser(userID string) (UserDeptSourceRow, error) {
	var row UserDeptSourceRow
	err := r.db.Raw(`
		SELECT COALESCE(dept_source,'sso') AS dept_source, COALESCE(dept_id::text,'') AS dept_id
		FROM users WHERE id::text = ?
	`, userID).Scan(&row).Error
	return row, err
}

// RecordDeptConflict 记录 SSO 与手动归属冲突
func (r *UserOrgRepo) RecordDeptConflict(userID, currentDeptID, ssoDeptID, ssoDeptName string) error {
	return r.db.Exec(`
		INSERT INTO user_dept_conflicts(user_id, current_dept_id, sso_dept_id, sso_dept_name)
		VALUES(?::uuid, NULLIF(?,'')::uuid, NULLIF(?,'')::uuid, ?)
	`, userID, currentDeptID, ssoDeptID, ssoDeptName).Error
}

// SSOMigrateDept 仅当 dept_source=sso 时，按 SSO claims 更新 dept_id（防手动覆盖）
// claimsDeptID: SSO 返回的 departments.id（UUID）
// 返回 (migrated bool, err)
func (r *UserOrgRepo) SSOMigrateDept(userID, claimsDeptUUID string) (bool, error) {
	res := r.db.Exec(`
		UPDATE users SET dept_id = ?::uuid
		WHERE id::text = ?
		  AND COALESCE(dept_source,'sso') = 'sso'
		  AND (dept_id IS NULL OR dept_id::text != ?)
	`, claimsDeptUUID, userID, claimsDeptUUID)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// SearchUsers 按姓名或邮箱关键词搜索用户（最多返回 20 条）
// studioID 不为空时，只返回该工作室下各部门的成员（用于指定部门负责人时约束选人范围）
func (r *UserOrgRepo) SearchUsers(keyword string, limit int, studioID string) ([]UserSearchResult, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	var results []UserSearchResult
	like := "%" + keyword + "%"

	if studioID != "" {
		// 只返回 studio 下各部门的成员：users.dept_id IN (departments whose studio_id = studioID)
		err := r.db.Raw(`
			SELECT u.id::text AS id, u.name, u.email,
			       COALESCE(d.dept_name, '') AS dept_name,
			       COALESCE(d.dept_id, '')  AS dept_id_str
			FROM users u
			LEFT JOIN departments d ON d.id = u.dept_id
			WHERE d.studio_id = ?::uuid
			  AND (u.name ILIKE ? OR u.email ILIKE ?)
			ORDER BY u.name
			LIMIT ?
		`, studioID, like, like, limit).Scan(&results).Error
		return results, err
	}

	err := r.db.Raw(`
		SELECT u.id::text AS id, u.name, u.email,
		       COALESCE(d.dept_name, '') AS dept_name,
		       COALESCE(d.dept_id, '')  AS dept_id_str
		FROM users u
		LEFT JOIN departments d ON d.id = u.dept_id
		WHERE u.name ILIKE ? OR u.email ILIKE ?
		ORDER BY u.name
		LIMIT ?
	`, like, like, limit).Scan(&results).Error
	return results, err
}
