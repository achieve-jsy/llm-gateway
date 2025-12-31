package model

// DeptStudioRow 是一个 (部门, 所属工作室) 组合记录，供预算初始化使用。
// 来源：departments + dept_studio_map + studios 联合查询。
// 注：DeptID 永不为空（已过滤 null），StudioID 可能为空（未映射工作室）。
type DeptStudioRow struct {
	DeptID     string
	StudioID   string
	StudioName string
}
