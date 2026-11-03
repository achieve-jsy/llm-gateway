package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ─── 账号页：一人一行（不是一条授权记录一行）──────────────────────────────────
//
// 语义（spec 2026-11-03-org-workspace-redesign）：
//   - 主角色取最高权限 admin > studio_admin > dept_admin > user（user = 无任何管理授权 = 员工）
//   - roles[] 保留全部授权明细，供前端折叠显示
//   - 角色筛选与统计都按"人"算，不是按授权条数算
const (
	RoleKeyAdmin       = "admin"
	RoleKeyStudioAdmin = "studio_admin"
	RoleKeyDeptAdmin   = "dept_admin"
	RoleKeyUser        = "user"
)

// AccountRoleItem 一条授权明细（scope_name 用于前端展示"管辖范围"）
type AccountRoleItem struct {
	Role      string `json:"role"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	ScopeName string `json:"scope_name"`
}

// AccountRow 账号页一行：人员属性 + 主角色 + 全部授权明细。
type AccountRow struct {
	UserID     string `json:"user_id"`
	Name       string `json:"name"`
	EmployeeID string `json:"employee_id"`
	JobTitle   string `json:"job_title"`
	Email      string `json:"email"`
	CreatedAt  string `json:"created_at"`
	StudioID   string `json:"studio_id"`
	StudioName string `json:"studio_name"`
	DeptID     string `json:"dept_id"` // departments.dept_id（EHR 业务键）
	DeptName   string `json:"dept_name"`
	// PrimaryRole/Roles 不是列：由 rolesByUserIDs 批量填充（gorm:"-" 防止被当成关联表）
	PrimaryRole string            `gorm:"-" json:"primary_role"`
	Roles       []AccountRoleItem `gorm:"-" json:"roles"`
}

// AccountRoleCounts 四种角色的人数（同 keyword/组织过滤下，按人分区统计）。
type AccountRoleCounts struct {
	Admin       int64 `json:"admin"`
	StudioAdmin int64 `json:"studio_admin"`
	DeptAdmin   int64 `json:"dept_admin"`
	User        int64 `json:"user"`
}

// AccountListFilter 账号页查询条件。
// Restricted 为 true 时把结果限制在 ScopeStudioIDs/ScopeDeptIDs 内（非 admin 调用方），
// 两个 scope 都为空 = 无管辖范围 = 不返回任何数据（与 policy.RoleListScope 语义一致）。
type AccountListFilter struct {
	Keyword  string
	Role     string
	StudioID string
	DeptID   string
	Page     int
	PageSize int
	// CreatedFrom/CreatedTo 创建时间区间（users.created_at）。CreatedTo 为"含当日"的左闭右开右界，
	// 由 handler 把 YYYY-MM-DD 解析成次日零点。
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	Restricted     bool
	ScopeStudioIDs []string
	ScopeDeptIDs   []string
}

// AccountRepo 账号聚合查询
type AccountRepo struct {
	db *gorm.DB
}

func NewAccountRepo(db *gorm.DB) *AccountRepo {
	return &AccountRepo{db: db}
}

// ListAccounts 一人一行分页查询 + 角色统计。
// 主查询/统计各一次，roles 明细再按 user_id IN (...) 批量取一次 —— 不按行回查。
// ponytail: 关键词/组织过滤靠 EXISTS + 列 cast，users 上没有可用索引（全表扫）；
// 当前人员规模（几千）一次几十毫秒可接受，上万后再给 users(employee_id,name) 补索引。
func (r *AccountRepo) ListAccounts(ctx context.Context, f AccountListFilter) ([]AccountRow, int64, AccountRoleCounts, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 || f.PageSize > 100 {
		f.PageSize = 20
	}

	// base 只做 keyword/组织/时间过滤；角色筛选单独叠加（见下方 total），
	// 这样 role_counts 与 /org/all-roles 的语义一致：统计不受角色筛选影响。
	base := func() *gorm.DB {
		q := r.db.WithContext(ctx).Table("users u").
			Joins("LEFT JOIN departments d ON d.id = u.dept_id").
			Joins("LEFT JOIN studios s ON s.id = d.studio_id").
			Where("u.deleted_at IS NULL")

		if kw := strings.TrimSpace(f.Keyword); kw != "" {
			pattern := "%" + strings.ToLower(kw) + "%"
			if isPostgres(r.db) {
				q = q.Where(`(COALESCE(u.name,'') ILIKE ? OR COALESCE(u.employee_id,'') ILIKE ?
					OR COALESCE(u.job_title,'') ILIKE ? OR COALESCE(u.email,'') ILIKE ?)`,
					pattern, pattern, pattern, pattern)
			} else {
				q = q.Where(`(LOWER(COALESCE(u.name,'')) LIKE ? OR LOWER(COALESCE(u.employee_id,'')) LIKE ?
					OR LOWER(COALESCE(u.job_title,'')) LIKE ? OR LOWER(COALESCE(u.email,'')) LIKE ?)`,
					pattern, pattern, pattern, pattern)
			}
		}
		// 组织过滤：用户归属的团队/工作室
		if id := strings.TrimSpace(f.StudioID); id != "" {
			q = q.Where(textCol(r.db, "d.studio_id")+" = ?", id)
		}
		if id := strings.TrimSpace(f.DeptID); id != "" {
			// 兼容前端传 SSO dept_id 或 departments.id（uuid）
			q = q.Where("(d.dept_id = ? OR "+textCol(r.db, "d.id")+" = ?)", id, id)
		}
		if f.Restricted {
			switch {
			case len(f.ScopeStudioIDs) > 0 && len(f.ScopeDeptIDs) > 0:
				q = q.Where("("+textCol(r.db, "d.studio_id")+" IN ? OR d.dept_id IN ?)", f.ScopeStudioIDs, f.ScopeDeptIDs)
			case len(f.ScopeStudioIDs) > 0:
				q = q.Where(textCol(r.db, "d.studio_id")+" IN ?", f.ScopeStudioIDs)
			case len(f.ScopeDeptIDs) > 0:
				q = q.Where("d.dept_id IN ?", f.ScopeDeptIDs)
			default:
				q = q.Where("1 = 0")
			}
		}
		if f.CreatedFrom != nil {
			q = q.Where("u.created_at >= ?", *f.CreatedFrom)
		}
		if f.CreatedTo != nil {
			q = q.Where("u.created_at < ?", *f.CreatedTo)
		}
		return q
	}
	// filtered = base + 角色筛选（分页与 total 用它）
	filtered := func() *gorm.DB {
		q := base()
		if cond := r.roleCond(f.Role); cond != "" {
			q = q.Where(cond)
		}
		return q
	}

	counts, err := r.countRoles(base())
	if err != nil {
		return nil, 0, AccountRoleCounts{}, err
	}

	var total int64
	if err = filtered().Count(&total).Error; err != nil {
		return nil, 0, AccountRoleCounts{}, err
	}
	if total == 0 {
		return []AccountRow{}, 0, counts, nil
	}

	var rows []AccountRow
	err = filtered().
		Select(`` + textCol(r.db, "u.id") + ` AS user_id, u.name,
			COALESCE(u.employee_id, '') AS employee_id,
			COALESCE(u.job_title, '') AS job_title,
			COALESCE(u.email, '') AS email,
			` + timeCol(r.db, "u.created_at") + ` AS created_at,
			COALESCE(` + textCol(r.db, "s.id") + `, '') AS studio_id,
			COALESCE(s.name, '') AS studio_name,
			COALESCE(d.dept_id, '') AS dept_id,
			COALESCE(d.dept_name, '') AS dept_name`).
		Order("u.name ASC, u.id ASC").
		Offset((f.Page - 1) * f.PageSize).
		Limit(f.PageSize).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, counts, err
	}

	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	roleMap, err := r.rolesByUserIDs(ctx, ids)
	if err != nil {
		return nil, 0, counts, err
	}
	for i := range rows {
		items := roleMap[rows[i].UserID]
		if items == nil {
			items = []AccountRoleItem{}
		}
		rows[i].Roles = items
		rows[i].PrimaryRole = primaryRoleOf(items)
	}
	return rows, total, counts, nil
}

// countRoles 一次扫描统计四种角色的人数。四个分支互斥（按最高角色归口），
// 相加等于 total —— 前端统计卡不会出现"四个人数加起来超过总数"。
func (r *AccountRepo) countRoles(q *gorm.DB) (AccountRoleCounts, error) {
	uid := textCol(r.db, "u.id")
	admin := existsRole(uid, RoleKeyAdmin)
	studioAdmin := existsRole(uid, RoleKeyStudioAdmin)
	deptAdmin := existsRole(uid, RoleKeyDeptAdmin)
	anyRole := "EXISTS (SELECT 1 FROM user_roles x WHERE x.user_id = " + uid + " AND x.role <> 'user')"

	var row AccountRoleCounts
	// 空结果集时 SUM 返回 NULL，必须 COALESCE，否则 COUNT 为 0 的筛选会扫描失败。
	err := q.Select(fmt.Sprintf(`
		CAST(COALESCE(SUM(CASE WHEN %s THEN 1 ELSE 0 END), 0) AS BIGINT) AS admin,
		CAST(COALESCE(SUM(CASE WHEN NOT (%s) AND %s THEN 1 ELSE 0 END), 0) AS BIGINT) AS studio_admin,
		CAST(COALESCE(SUM(CASE WHEN NOT (%s) AND NOT (%s) AND %s THEN 1 ELSE 0 END), 0) AS BIGINT) AS dept_admin,
		CAST(COALESCE(SUM(CASE WHEN NOT (%s) THEN 1 ELSE 0 END), 0) AS BIGINT) AS "user"`,
		admin, admin, studioAdmin, admin, studioAdmin, deptAdmin, anyRole,
	)).Scan(&row).Error
	return row, err
}

// rolesByUserIDs 批量取多人的授权明细，scope_name 在 SQL 里 join 出来
// （admin→全公司、studio→studios.name、dept→departments.dept_name）。
func (r *AccountRepo) rolesByUserIDs(ctx context.Context, userIDs []string) (map[string][]AccountRoleItem, error) {
	result := make(map[string][]AccountRoleItem, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		UserID    string `gorm:"column:user_id"`
		Role      string `gorm:"column:role"`
		ScopeType string `gorm:"column:scope_type"`
		ScopeID   string `gorm:"column:scope_id"`
		ScopeName string `gorm:"column:scope_name"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT ur.user_id, ur.role, ur.scope_type, ur.scope_id,
		       CASE ur.role
		         WHEN 'admin' THEN '全公司'
		         WHEN 'studio_admin' THEN COALESCE(st.name, '')
		         WHEN 'dept_admin' THEN COALESCE(dp.dept_name, '')
		         ELSE ''
		       END AS scope_name
		FROM user_roles ur
		LEFT JOIN studios st ON ur.role = 'studio_admin' AND `+textCol(r.db, "st.id")+` = ur.scope_id
		LEFT JOIN departments dp ON ur.role = 'dept_admin'
		     AND (dp.dept_id = ur.scope_id OR `+textCol(r.db, "dp.id")+` = ur.scope_id)
		WHERE ur.user_id IN ? AND ur.role <> 'user'
		ORDER BY ur.user_id, ur.created_at, ur.id
	`, userIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.UserID] = append(result[row.UserID], AccountRoleItem{
			Role: row.Role, ScopeType: row.ScopeType, ScopeID: row.ScopeID, ScopeName: row.ScopeName,
		})
	}
	return result, nil
}

// roleCond 角色筛选条件（空 = 不过滤）。
func (r *AccountRepo) roleCond(role string) string {
	uid := textCol(r.db, "u.id")
	admin := existsRole(uid, RoleKeyAdmin)
	switch strings.TrimSpace(role) {
	case "":
		return ""
	case RoleKeyAdmin:
		return admin
	case RoleKeyStudioAdmin:
		return "NOT (" + admin + ") AND " + existsRole(uid, RoleKeyStudioAdmin)
	case RoleKeyDeptAdmin:
		return "NOT (" + admin + ") AND NOT (" + existsRole(uid, RoleKeyStudioAdmin) + ") AND " + existsRole(uid, RoleKeyDeptAdmin)
	case RoleKeyUser:
		// 员工 = 没有任何管理授权（user_roles 里 role='user' 的历史行不算授权）
		return "NOT EXISTS (SELECT 1 FROM user_roles x WHERE x.user_id = " + uid + " AND x.role <> 'user')"
	}
	return ""
}

// primaryRoleOf 按 admin > studio_admin > dept_admin > user 取主角色。
func primaryRoleOf(items []AccountRoleItem) string {
	rank := map[string]int{RoleKeyAdmin: 4, RoleKeyStudioAdmin: 3, RoleKeyDeptAdmin: 2}
	best, bestRank := RoleKeyUser, 0
	for _, item := range items {
		if v := rank[item.Role]; v > bestRank {
			best, bestRank = item.Role, v
		}
	}
	return best
}

func existsRole(uidCol, role string) string {
	return "EXISTS (SELECT 1 FROM user_roles x WHERE x.user_id = " + uidCol + " AND x.role = '" + role + "')"
}

// ─── 方言辅助 ────────────────────────────────────────────────────────────────
// 生产是 Postgres、单测是 sqlite：Raw SQL 里的 uuid→text 与时间→text 只在
// 这两个辅助函数里分叉，其余 SQL 两份方言共用。

func isPostgres(db *gorm.DB) bool {
	return db != nil && db.Dialector != nil && db.Dialector.Name() == "postgres"
}

func textCol(db *gorm.DB, col string) string {
	if isPostgres(db) {
		return col + "::text"
	}
	return "CAST(" + col + " AS TEXT)"
}

func timeCol(db *gorm.DB, col string) string {
	if isPostgres(db) {
		return col + "::text"
	}
	return "datetime(" + col + ")"
}

// dateCol 取 YYYY-MM-DD 文本（Postgres 统一按 UTC 取日，与历史行为一致）
func dateCol(db *gorm.DB, col string) string {
	if isPostgres(db) {
		return "TO_CHAR(" + col + " AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	}
	return "date(" + col + ")"
}
