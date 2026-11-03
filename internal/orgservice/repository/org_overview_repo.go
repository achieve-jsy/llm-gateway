package repository

import (
	"context"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"gorm.io/gorm"
)

// ─── 工作室工作台概览（原型图1/图3 的头部 + 左树团队卡）─────────────────────
//
// 护栏：负责人（org_node_leaders，EHR 同步、只增不删、多值）与管理员（user_roles，可授可撤）
// 是两套数据，永远分开返回；前端文案也必须分开。

// OrgAccountBrief 管理员简报（够前端画头像行即可）
type OrgAccountBrief struct {
	UserID     string `json:"user_id"`
	Name       string `json:"name"`
	EmployeeID string `json:"employee_id"`
}

// OrgLeaderBrief 负责人简报（source 区分 EHR 同步 / 管理员手动加的）
type OrgLeaderBrief struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// TeamCard 左树里的一个团队卡
type TeamCard struct {
	DeptID      string `json:"dept_id"`   // departments.dept_id（EHR 业务键）
	DeptUUID    string `json:"dept_uuid"` // departments.id
	DeptName    string `json:"dept_name"`
	MemberCount int    `json:"member_count"`
	// 切片字段不是列（由批量查询填充），gorm:"-" 防止被当成关联表
	Admins  []OrgAccountBrief `gorm:"-" json:"admins"`
	Leaders []OrgLeaderBrief  `gorm:"-" json:"leaders"`
}

// StudioOverview 工作室工作台概览
type StudioOverview struct {
	StudioID         string            `json:"studio_id"`
	StudioName       string            `json:"studio_name"`
	MemberCount      int               `json:"member_count"`
	Admins           []OrgAccountBrief `gorm:"-" json:"admins"`
	Leaders          []OrgLeaderBrief  `gorm:"-" json:"leaders"`
	PendingDeptCount int               `json:"pending_dept_count"`
	Teams            []TeamCard        `gorm:"-" json:"teams"`
}

// OrgOverviewRepo 工作室/团队概览查询
type OrgOverviewRepo struct {
	db *gorm.DB
}

func NewOrgOverviewRepo(db *gorm.DB) *OrgOverviewRepo {
	return &OrgOverviewRepo{db: db}
}

// StudioOverview 组装工作室概览：人数、管理员、EHR 负责人、待归集团队数、下属团队卡。
// 查询数固定（不随团队数增长）：1 次工作室 + 1 次人数 + 1 次管理员 + 1 次负责人
// + 1 次待归集数 + 1 次团队 + 1 次团队人数 + 1 次团队管理员 + 1 次团队负责人。
func (r *OrgOverviewRepo) StudioOverview(ctx context.Context, studioID string) (*StudioOverview, error) {
	var studio model.Studio
	if err := r.db.WithContext(ctx).First(&studio, "id = ?", studioID).Error; err != nil {
		return nil, err
	}

	ov := &StudioOverview{
		StudioID:   studio.ID,
		StudioName: studio.Name,
		Admins:     []OrgAccountBrief{},
		Leaders:    []OrgLeaderBrief{},
		Teams:      []TeamCard{},
	}

	var err error
	if ov.MemberCount, err = r.countMembers(ctx, studio.ID); err != nil {
		return nil, err
	}
	if ov.Admins, err = r.admins(ctx, RoleKeyStudioAdmin, studio.ID); err != nil {
		return nil, err
	}
	if ov.Leaders, err = r.leaders(ctx, model.LeaderNodeStudio, studio.ID); err != nil {
		return nil, err
	}
	if err = r.db.WithContext(ctx).Raw(
		`SELECT COUNT(*) FROM departments WHERE status = ?`, string(model.DeptStatusPendingStudio),
	).Scan(&ov.PendingDeptCount).Error; err != nil {
		return nil, err
	}

	var teams []TeamCard
	if err = r.db.WithContext(ctx).Raw(`
		SELECT dept_id, `+textCol(r.db, "id")+` AS dept_uuid, dept_name
		FROM departments
		WHERE `+textCol(r.db, "studio_id")+` = ? AND status <> ?
		ORDER BY dept_name
	`, studio.ID, string(model.DeptStatusDeleted)).Scan(&teams).Error; err != nil {
		return nil, err
	}
	if len(teams) == 0 {
		return ov, nil
	}

	deptUUIDs := make([]string, 0, len(teams))
	deptIDs := make([]string, 0, len(teams))
	for _, t := range teams {
		deptUUIDs = append(deptUUIDs, t.DeptUUID)
		deptIDs = append(deptIDs, t.DeptID)
	}

	memberCounts, err := r.memberCountsByDept(ctx, deptUUIDs)
	if err != nil {
		return nil, err
	}
	adminsByDept, err := r.adminsByScope(ctx, RoleKeyDeptAdmin, deptIDs)
	if err != nil {
		return nil, err
	}
	leadersByDept, err := r.leadersByNodeIDs(ctx, model.LeaderNodeDept, deptUUIDs)
	if err != nil {
		return nil, err
	}
	for i := range teams {
		teams[i].MemberCount = memberCounts[teams[i].DeptUUID]
		teams[i].Admins = emptyIfNil(adminsByDept[teams[i].DeptID])
		teams[i].Leaders = emptyLeadersIfNil(leadersByDept[teams[i].DeptUUID])
	}
	ov.Teams = teams
	return ov, nil
}

// countMembers 工作室人数 = 下属部门的人数之和（按 users.dept_id 归属）。
func (r *OrgOverviewRepo) countMembers(ctx context.Context, studioID string) (int, error) {
	var n int
	err := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*) FROM users u
		JOIN departments d ON d.id = u.dept_id
		WHERE `+textCol(r.db, "d.studio_id")+` = ? AND u.deleted_at IS NULL
	`, studioID).Scan(&n).Error
	return n, err
}

// memberCountsByDept key = departments.id（uuid 文本）
func (r *OrgOverviewRepo) memberCountsByDept(ctx context.Context, deptUUIDs []string) (map[string]int, error) {
	result := make(map[string]int, len(deptUUIDs))
	var rows []struct {
		DeptUUID string `gorm:"column:dept_uuid"`
		Count    int    `gorm:"column:count"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT `+textCol(r.db, "u.dept_id")+` AS dept_uuid, COUNT(*) AS count
		FROM users u
		WHERE u.deleted_at IS NULL AND `+textCol(r.db, "u.dept_id")+` IN ?
		GROUP BY `+textCol(r.db, "u.dept_id")+`
	`, deptUUIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.DeptUUID] = row.Count
	}
	return result, nil
}

// admins 单个 scope 的管理员（scope_id 直接相等：studio=studios.id、dept=departments.dept_id）。
func (r *OrgOverviewRepo) admins(ctx context.Context, role, scopeID string) ([]OrgAccountBrief, error) {
	if scopeID == "" {
		return []OrgAccountBrief{}, nil
	}
	var rows []OrgAccountBrief
	err := r.db.WithContext(ctx).Raw(`
		SELECT `+textCol(r.db, "u.id")+` AS user_id, u.name, COALESCE(u.employee_id, '') AS employee_id
		FROM user_roles ur
		JOIN users u ON `+textCol(r.db, "u.id")+` = ur.user_id
		WHERE ur.role = ? AND ur.scope_id = ? AND u.deleted_at IS NULL
		ORDER BY u.name
	`, role, scopeID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return emptyIfNil(rows), nil
}

// adminsByScope 批量取多个 scope 的管理员，key = scope_id。
func (r *OrgOverviewRepo) adminsByScope(ctx context.Context, role string, scopeIDs []string) (map[string][]OrgAccountBrief, error) {
	result := make(map[string][]OrgAccountBrief, len(scopeIDs))
	if len(scopeIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		ScopeID    string `gorm:"column:scope_id"`
		UserID     string `gorm:"column:user_id"`
		Name       string `gorm:"column:name"`
		EmployeeID string `gorm:"column:employee_id"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT ur.scope_id AS scope_id, `+textCol(r.db, "u.id")+` AS user_id, u.name AS name,
		       COALESCE(u.employee_id, '') AS employee_id
		FROM user_roles ur
		JOIN users u ON `+textCol(r.db, "u.id")+` = ur.user_id
		WHERE ur.role = ? AND ur.scope_id IN ? AND u.deleted_at IS NULL
		ORDER BY u.name
	`, role, scopeIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ScopeID] = append(result[row.ScopeID], OrgAccountBrief{
			UserID: row.UserID, Name: row.Name, EmployeeID: row.EmployeeID,
		})
	}
	return result, nil
}

// leaders 单个节点的负责人（多值，来自 org_node_leaders）。
func (r *OrgOverviewRepo) leaders(ctx context.Context, nodeType, nodeID string) ([]OrgLeaderBrief, error) {
	var rows []OrgLeaderBrief
	err := r.db.WithContext(ctx).Raw(`
		SELECT name, source FROM org_node_leaders
		WHERE node_type = ? AND `+textCol(r.db, "node_id")+` = ?
		ORDER BY created_at
	`, nodeType, nodeID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return emptyLeadersIfNil(rows), nil
}

// leadersByNodeIDs 批量取节点负责人，key = node_id（uuid 文本）。
func (r *OrgOverviewRepo) leadersByNodeIDs(ctx context.Context, nodeType string, nodeIDs []string) (map[string][]OrgLeaderBrief, error) {
	result := make(map[string][]OrgLeaderBrief, len(nodeIDs))
	if len(nodeIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		NodeID string `gorm:"column:node_id"`
		Name   string `gorm:"column:name"`
		Source string `gorm:"column:source"`
	}
	err := r.db.WithContext(ctx).Raw(`
		SELECT `+textCol(r.db, "node_id")+` AS node_id, name, source FROM org_node_leaders
		WHERE node_type = ? AND `+textCol(r.db, "node_id")+` IN ?
		ORDER BY created_at
	`, nodeType, nodeIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.NodeID] = append(result[row.NodeID], OrgLeaderBrief{Name: row.Name, Source: row.Source})
	}
	return result, nil
}

func emptyIfNil(v []OrgAccountBrief) []OrgAccountBrief {
	if v == nil {
		return []OrgAccountBrief{}
	}
	return v
}

func emptyLeadersIfNil(v []OrgLeaderBrief) []OrgLeaderBrief {
	if v == nil {
		return []OrgLeaderBrief{}
	}
	return v
}
