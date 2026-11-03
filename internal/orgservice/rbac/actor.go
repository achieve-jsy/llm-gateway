package rbac

import "gorm.io/gorm"

// Actor 当前请求者的身份 + 数据范围
// 由 Guard middleware 一次性计算并注入 gin.Context，全链路（Handler→Service→Repo）共用。
//
// DeptIDs 语义：
//   - dept_admin 直接管辖的部门标识列表
//   - studio_admin 管辖工作室内所有部门标识（由 Checker.buildActor 预计算填充）
//
// 这样 Policy/Repo 不需要再做额外 DB 查询来扩展范围。
type Actor struct {
	UserID          string
	IsGlobal        bool     // admin：无范围限制
	StudioIDs       []string // studio_admin 管辖的工作室 ID 列表
	DeptIDs         []string // 管辖范围内所有部门标识（含工作室下属部门）
	Roles           []string // 用户真实持有的角色集合（dept_admin / studio_admin / admin），与展开后的 DeptIDs 区分
	DeptAdminIDs    []string // dept_admin 直接管辖的部门 SSO ID（不含工作室展开），用于扩容 dept 级审批范围过滤
	ApprovalDeptIDs []string // Key 审批使用的部门 SSO ID 并集（dept_admin 直管 + studio_admin 下属）
	ScopeError      string   // 工作室范围展开失败标记；非 ScopeAny 操作必须拒绝，不能使用部分范围继续授权
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

// HasDirectDept 判断用户是否以 dept_admin 身份直接管辖该部门。
// 不能以工作室展开范围替代此判断：部门级策略由该部门负责人维护。
func (a Actor) HasDirectDept(id string) bool {
	if a.IsGlobal {
		return true
	}
	for _, d := range a.DeptAdminIDs {
		if d == id {
			return true
		}
	}
	return false
}

// CanManageDeptBudgetPool 判断用户是否有权写入部门预算池额度
// （scope_type = dept_daily / dept_service）。
//
// 权限遵循「上级配置下级」：部门预算池额度由工作室管理员或平台管理员下发，
// 部门管理员对自身部门额度只读，扩容须走扩容审批。
// 档位子额度（budget_tier_limits）与本规则共用同一判定，避免同一资源两条路径权限不一致。
func (a Actor) CanManageDeptBudgetPool(deptID string) bool {
	if a.IsGlobal {
		return true
	}
	// 仅持有 dept_admin（无任何工作室管理范围）时，不得修改本部门额度
	if len(a.StudioIDs) == 0 {
		return false
	}
	return a.HasDept(deptID)
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
