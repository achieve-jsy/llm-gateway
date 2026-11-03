package repository

import (
	"context"
	"strconv"

	cprepo "github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
)

// 全局默认配额（system_configs 里缺行时的兜底，与 settings_service.ensureDefaults 同值）
const (
	defaultUserRPM               = 60
	defaultUserMaxConcurrency    = 10
	settingKeyDefaultUserRPM     = "quota_default_user_rpm"
	settingKeyDefaultUserMaxConc = "quota_default_user_max_concurrency"
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
// 同时返回 dept_id（SSO EhrDeptID）和部门主键文本，以兼容 call_logs 两种格式
func (r *UserOrgRepo) ListSSODeptIDsByStudioIDs(studioIDs []string) ([]string, error) {
	if len(studioIDs) == 0 {
		return nil, nil
	}
	var ids []string
	err := r.db.Raw(`SELECT dept_id FROM departments WHERE CAST(studio_id AS TEXT) IN ?`, studioIDs).Scan(&ids).Error
	return ids, err
}

func (r *UserOrgRepo) ListDeptIDsByStudioIDs(studioIDs []string) ([]string, error) {
	if len(studioIDs) == 0 {
		return nil, nil
	}
	var rows []struct {
		DeptID string `gorm:"column:dept_id"`
		UUIDID string `gorm:"column:uuid_id"`
	}
	err := r.db.Raw(
		`SELECT dept_id, CAST(id AS TEXT) AS uuid_id FROM departments WHERE CAST(studio_id AS TEXT) IN ?`,
		studioIDs,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		if row.DeptID != "" {
			ids = append(ids, row.DeptID)
		}
		if row.UUIDID != "" {
			ids = append(ids, row.UUIDID)
		}
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

	// IsDeptAdmin 是否该团队的 dept_admin（管理员，不是负责人）
	IsDeptAdmin bool `gorm:"column:is_dept_admin" json:"is_dept_admin"`
	// Config 成员配置摘要（配额/预算/模型授权），由 attachMemberConfigs 批量填充
	Config *MemberConfigSummary `gorm:"-" json:"config"`

	// 配额覆盖原始值：NULL = 继承全局默认。只用于拼 Config，不直接输出。
	QuotaRPM            *int `gorm:"column:quota_rpm" json:"-"`
	QuotaMaxConcurrency *int `gorm:"column:quota_max_concurrency" json:"-"`
}

// ─── 成员配置摘要（原型图3 行展开的三张卡）───────────────────────────────────

// MemberQuotaConfig 配额：source=inherit（继承全局默认 default_user）| override（成员单独设置）
type MemberQuotaConfig struct {
	RPM            int    `json:"rpm"`
	MaxConcurrency int    `json:"max_concurrency"`
	Source         string `json:"source"`
}

// MemberBudgetConfig 预算：source=member（成员单独设额）| team（继承团队池 dept_daily）| none
type MemberBudgetConfig struct {
	Monthly float64 `json:"monthly"`
	Daily   float64 `json:"daily"`
	Weekly  float64 `json:"weekly"`
	Source  string  `json:"source"`
}

// MemberEntitlementConfig 模型授权：只有 studio/dept 两级，无成员级 → 成员行显示继承后的可用组数
type MemberEntitlementConfig struct {
	GroupCount int    `json:"group_count"`
	Source     string `json:"source"`
}

// MemberConfigSummary 成员配置三块摘要
type MemberConfigSummary struct {
	Quota       MemberQuotaConfig       `json:"quota"`
	Budget      MemberBudgetConfig      `json:"budget"`
	Entitlement MemberEntitlementConfig `json:"entitlement"`
}

// ListMembersByDeptID 列出指定部门（按 SSO dept_id 字符串）的成员，支持分页。
// 每个成员额外带 is_dept_admin 与三块配置摘要（配额/预算/模型授权）：
// 摘要是 4 次批量查询（IN）拼出来的，一页 20 行不会退化成 N+1。
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
	// 方言差异（uuid→text / 日期截断）走 textCol/dateCol，单测才能跑 sqlite。
	err := r.db.Raw(`
		SELECT `+textCol(r.db, "u.id")+` AS id, u.name, u.email,
		       `+dateCol(r.db, "u.created_at")+` AS joined_at,
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
		       ) AS has_conflict,
		       u.quota_rpm,
		       u.quota_max_concurrency
		FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE d.dept_id = ?
		ORDER BY u.name
		LIMIT ? OFFSET ?
	`, deptID, pageSize, offset).Scan(&members).Error
	if err != nil || len(members) == 0 {
		return members, total, err
	}
	if err := r.attachMemberConfigs(deptID, members); err != nil {
		return nil, 0, err
	}
	return members, total, nil
}

// attachMemberConfigs 给一页成员补 is_dept_admin + config（配额/预算/模型授权）。
// 语义（spec 2026-11-03-org-workspace-redesign 护栏）：
//   - 配额：users.quota_rpm/quota_max_concurrency 非 NULL = override，NULL = 继承全局默认 default_user
//   - 预算：budget_allocations 的 member（scope_id=user uuid）优先，否则团队池 dept_daily（scope_id=SSO dept_id）
//   - 模型授权：group_entitlements 只有 studio/dept 两级，取该团队工作室 + 团队自身的 enabled 组去重数
func (r *UserOrgRepo) attachMemberConfigs(deptID string, members []DeptMemberInfo) error {
	ctx := context.Background()
	userIDs := make([]string, 0, len(members))
	for i := range members {
		userIDs = append(userIDs, members[i].ID)
	}

	// 1) 全局默认配额（复用 settings 读取：system_configs 一行一个 key）
	defaultRPM, defaultConc := defaultUserRPM, defaultUserMaxConcurrency
	if vals, err := cprepo.NewSettingsRepo(r.db).GetMultiple([]string{settingKeyDefaultUserRPM, settingKeyDefaultUserMaxConc}); err == nil {
		if v := parseIntSetting(vals[settingKeyDefaultUserRPM]); v != nil {
			defaultRPM = *v
		}
		if v := parseIntSetting(vals[settingKeyDefaultUserMaxConc]); v != nil {
			defaultConc = *v
		}
	}

	// 2) 预算：成员单设优先，其次团队池
	type budgetRow struct {
		ScopeType string  `gorm:"column:scope_type"`
		ScopeID   string  `gorm:"column:scope_id"`
		Monthly   float64 `gorm:"column:amount_usd"`
		Daily     float64 `gorm:"column:daily_amount_usd"`
		Weekly    float64 `gorm:"column:weekly_amount_usd"`
	}
	var budgets []budgetRow
	if err := r.db.WithContext(ctx).Raw(`
		SELECT scope_type, scope_id, amount_usd, daily_amount_usd, weekly_amount_usd
		FROM budget_allocations
		WHERE enabled = true
		  AND ((scope_type = 'member' AND scope_id IN ?) OR (scope_type = 'dept_daily' AND scope_id = ?))
	`, userIDs, deptID).Scan(&budgets).Error; err != nil {
		return err
	}
	memberBudget := make(map[string]budgetRow, len(budgets))
	var teamBudget *budgetRow
	for i := range budgets {
		if budgets[i].ScopeType == "member" {
			memberBudget[budgets[i].ScopeID] = budgets[i]
			continue
		}
		teamBudget = &budgets[i]
	}

	// 3) 是否该团队的 dept_admin
	var adminUserIDs []string
	if err := r.db.WithContext(ctx).Raw(
		`SELECT user_id FROM user_roles WHERE role = 'dept_admin' AND scope_id = ? AND user_id IN ?`,
		deptID, userIDs,
	).Scan(&adminUserIDs).Error; err != nil {
		return err
	}
	isDeptAdmin := make(map[string]bool, len(adminUserIDs))
	for _, id := range adminUserIDs {
		isDeptAdmin[id] = true
	}

	// 4) 模型授权：该团队所属工作室 + 团队自身的 enabled 授权组去重数
	var studioID string
	if err := r.db.WithContext(ctx).Raw(
		`SELECT COALESCE(`+textCol(r.db, "studio_id")+`, '') FROM departments WHERE dept_id = ?`, deptID,
	).Scan(&studioID).Error; err != nil {
		return err
	}
	groupCount := 0
	if err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(DISTINCT group_id) FROM group_entitlements
		WHERE enabled = true AND ((scope_type = 'studio' AND scope_id = ?) OR (scope_type = 'dept' AND scope_id = ?))
	`, studioID, deptID).Scan(&groupCount).Error; err != nil {
		return err
	}

	for i := range members {
		m := &members[i]
		m.IsDeptAdmin = isDeptAdmin[m.ID]

		quota := MemberQuotaConfig{RPM: defaultRPM, MaxConcurrency: defaultConc, Source: "inherit"}
		if m.QuotaRPM != nil || m.QuotaMaxConcurrency != nil {
			quota.Source = "override"
			if m.QuotaRPM != nil {
				quota.RPM = *m.QuotaRPM
			}
			if m.QuotaMaxConcurrency != nil {
				quota.MaxConcurrency = *m.QuotaMaxConcurrency
			}
		}

		budget := MemberBudgetConfig{Source: "none"}
		if b, ok := memberBudget[m.ID]; ok {
			budget = MemberBudgetConfig{Monthly: b.Monthly, Daily: b.Daily, Weekly: b.Weekly, Source: "member"}
		} else if teamBudget != nil {
			budget = MemberBudgetConfig{
				Monthly: teamBudget.Monthly, Daily: teamBudget.Daily, Weekly: teamBudget.Weekly, Source: "team",
			}
		}

		entitlement := MemberEntitlementConfig{GroupCount: groupCount, Source: "none"}
		if groupCount > 0 {
			entitlement.Source = "studio_dept"
		}

		m.Config = &MemberConfigSummary{Quota: quota, Budget: budget, Entitlement: entitlement}
	}
	return nil
}

// parseIntSetting 解析 system_configs 里的整数值；空/非法返回 nil（调用方用兜底默认）。
func parseIntSetting(raw string) *int {
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil
	}
	return &v
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
