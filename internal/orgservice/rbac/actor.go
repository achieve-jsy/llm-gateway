package rbac

import "gorm.io/gorm"

// Actor 当前请求者的身份 + 数据范围
// 由 Guard middleware 一次性计算并注入 gin.Context，全链路（Handler→Service→Repo）共用。
//
// DeptIDs 语义：
//   - dept_admin 直接管辖的部门 UUID 列表
//   - studio_admin 管辖工作室内所有部门 UUID（由 Checker.buildActor 预计算填充）
//
// 这样 Policy/Repo 不需要再做额外 DB 查询来扩展范围。
type Actor struct {
	UserID    string
	IsGlobal  bool     // admin：无范围限制
	StudioIDs []string // studio_admin 管辖的工作室 UUID 列表
	DeptIDs   []string // 管辖范围内所有部门 UUID（含工作室下属部门）
}

// HasStudio 判断工作室 ID 是否在管辖范围（admin 始终返回 true）
func (a Actor) HasStudio(id string) bool {
	if a.IsGlobal {
		return true
	}
	for _, s := range a.StudioIDs {
		if s == id {
			return true
		}
	}
	return false
}

// HasDept 判断部门 ID 是否在管辖范围（admin 或管辖工作室/部门内均返回 true）
func (a Actor) HasDept(id string) bool {
	if a.IsGlobal {
		return true
	}
	for _, d := range a.DeptIDs {
		if d == id {
			return true
		}
	}
	return false
}

// IsManager 是否持有任意管理角色
func (a Actor) IsManager() bool {
	return a.IsGlobal || len(a.StudioIDs) > 0 || len(a.DeptIDs) > 0
}

// StudioScope 返回 GORM Scope 函数，对查询按工作室维度过滤
// col: 目标表中工作室 ID 列名，如 "studio_id"、"d.studio_id"
func (a Actor) StudioScope(col string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if a.IsGlobal {
			return db
		}
		if len(a.StudioIDs) == 0 {
			return db.Where("1 = 0")
		}
		return db.Where(col+" IN ?", a.StudioIDs)
	}
}

// DeptScope 返回 GORM Scope 函数，对查询按部门维度过滤
// col: 目标表中部门 ID 列名
func (a Actor) DeptScope(col string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if a.IsGlobal {
			return db
		}
		if len(a.DeptIDs) == 0 {
			return db.Where("1 = 0")
		}
		return db.Where(col+" IN ?", a.DeptIDs)
	}
}
