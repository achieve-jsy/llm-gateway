package ehrsync

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

// Repository 封装 EHR 同步相关的全部数据库操作
type Repository struct {
	db *gorm.DB
}

// NewRepository 创建 repository 实例
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// DB 暴露底层 db（测试 / 其他模块用）
func (r *Repository) DB() *gorm.DB { return r.db }

// ---------- 全量列表（用于对账） ----------

func (r *Repository) ListAllOrganizations(ctx context.Context) ([]model.EhrOrganization, error) {
	var items []model.EhrOrganization
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

func (r *Repository) ListAllCompanies(ctx context.Context) ([]model.EhrCompany, error) {
	var items []model.EhrCompany
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

func (r *Repository) ListAllDepartments(ctx context.Context) ([]model.EhrDepartment, error) {
	var items []model.EhrDepartment
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

func (r *Repository) ListAllEmployees(ctx context.Context) ([]model.EhrEmployee, error) {
	var items []model.EhrEmployee
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

func (r *Repository) ListAllPositions(ctx context.Context) ([]model.EhrPosition, error) {
	var items []model.EhrPosition
	err := r.db.WithContext(ctx).Find(&items).Error
	return items, err
}

// ---------- Upsert（带 override 检测） ----------

// UpsertResults 单次 upsert 的汇总
type UpsertResults struct {
	Fetched int
	Created int
	Updated int
	Skipped int // override 拦截
}

func (u *UpsertResults) Merge(other UpsertResults) {
	u.Fetched += other.Fetched
	u.Created += other.Created
	u.Updated += other.Updated
	u.Skipped += other.Skipped
}

// HasOverride 接口：用于识别支持 override 的实体
type HasOverride interface {
	GetOverride() *time.Time
}

// upsertEntity 通用 upsert：按 conflict 键插入或更新
func upsertEntity[T any](db *gorm.DB, items []T, conflictCols []string, updateAssignments func(dst, src *T) map[string]any, diff func(dst, src *T) (changed map[string]any, hasOverride bool)) (created, updated, skipped int, diffLogs []DiffEntry, err error) {
	if len(items) == 0 {
		return 0, 0, 0, nil, nil
	}

	for i := range items {
		src := &items[i]
		dst := new(T)
		// 查询现有记录（用主键或唯一键）
		where := buildWhereMap(src, conflictCols)
		query := db.Where(where).Model(dst)
		notFound := query.First(dst).Error != nil

		if notFound {
			if err := db.Create(src).Error; err != nil {
				if !isDuplicate(err) {
					return created, updated, skipped, diffLogs, err
				}
				// 并发插入：尝试更新
				if updates := updateAssignments(dst, src); len(updates) > 0 {
					if err := db.Model(dst).Where(where).Updates(updates).Error; err != nil {
						return created, updated, skipped, diffLogs, err
					}
					updated++
				} else {
					created++
				}
				continue
			}
			created++
			continue
		}

		// 已存在：diff + override
		if diffFn := diff; diffFn != nil {
			changes, overridden := diffFn(dst, src)
			if overridden {
				skipped++
				diffLogs = append(diffLogs, makeSkipEntry(dst, src, changes))
				continue
			}
			if len(changes) == 0 {
				continue
			}
			updates := updateAssignments(dst, src)
			if err := db.Model(dst).Where(buildWhereMap(src, conflictCols)).Updates(updates).Error; err != nil {
				return created, updated, skipped, diffLogs, err
			}
			updated++
			diffLogs = append(diffLogs, makeChangeEntry(dst, src, changes))
		}
	}
	return created, updated, skipped, diffLogs, nil
}

func buildWhereMap(src any, cols []string) map[string]any {
	v := reflect.ValueOf(src).Elem()
	t := v.Type()
	where := make(map[string]any, len(cols))
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("gorm")
		col := parseColumn(tag, f.Name)
		for _, c := range cols {
			if col == c {
				where[c] = v.Field(i).Interface()
			}
		}
	}
	return where
}

func parseColumn(tag, fallback string) string {
	for _, part := range strings.Split(tag, ";") {
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return schema.NamingStrategy{}.ColumnName("", fallback)
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "duplicate key") || strings.Contains(s, "unique")
}

// ---------- 各实体 Upsert 包装 ----------

func (r *Repository) UpsertOrganizations(items []model.EhrOrganization) (UpsertResults, error) {
	created, updated, _, _, err := upsertEntity(
		r.db, items,
		[]string{"org_id"},
		func(dst, src *model.EhrOrganization) map[string]any {
			return map[string]any{
				"org_name":        src.OrgName,
				"org_code":        src.OrgCode,
				"ehr_create_date": src.EhrCreateDate,
				"ehr_modify_date": src.EhrModifyDate,
				"synced_at":       time.Now(),
			}
		},
		func(dst, src *model.EhrOrganization) (map[string]any, bool) {
			changes := map[string]any{}
			if dst.OrgName != src.OrgName {
				changes["org_name"] = map[string]any{"old": dst.OrgName, "new": src.OrgName}
			}
			if dst.OrgCode != src.OrgCode {
				changes["org_code"] = map[string]any{"old": dst.OrgCode, "new": src.OrgCode}
			}
			if dst.EhrModifyDate != src.EhrModifyDate {
				changes["ehr_modify_date"] = map[string]any{"old": dst.EhrModifyDate, "new": src.EhrModifyDate}
			}
			return changes, false
		},
	)
	return UpsertResults{Fetched: len(items), Created: created, Updated: updated}, err
}

func (r *Repository) UpsertCompanies(items []model.EhrCompany) (UpsertResults, error) {
	created, updated, _, _, err := upsertEntity(
		r.db, items,
		[]string{"org_id", "company_id"},
		func(dst, src *model.EhrCompany) map[string]any {
			return map[string]any{
				"company_code":    src.CompanyCode,
				"company_name":    src.CompanyName,
				"parent_id":       src.ParentID,
				"ehr_modify_date": src.EhrModifyDate,
				"synced_at":       time.Now(),
			}
		},
		func(dst, src *model.EhrCompany) (map[string]any, bool) {
			changes := map[string]any{}
			if dst.CompanyCode != src.CompanyCode {
				changes["company_code"] = map[string]any{"old": dst.CompanyCode, "new": src.CompanyCode}
			}
			if dst.CompanyName != src.CompanyName {
				changes["company_name"] = map[string]any{"old": dst.CompanyName, "new": src.CompanyName}
			}
			if dst.ParentID != src.ParentID {
				changes["parent_id"] = map[string]any{"old": dst.ParentID, "new": src.ParentID}
			}
			if dst.EhrModifyDate != src.EhrModifyDate {
				changes["ehr_modify_date"] = map[string]any{"old": dst.EhrModifyDate, "new": src.EhrModifyDate}
			}
			return changes, false
		},
	)
	return UpsertResults{Fetched: len(items), Created: created, Updated: updated}, err
}

func (r *Repository) UpsertDepartments(items []model.EhrDepartment) (UpsertResults, []DiffEntry, error) {
	var allDiff []DiffEntry
	created, updated, skipped, diffLogs, err := upsertEntity(
		r.db, items,
		[]string{"org_id", "department_id"},
		deptUpdateMap,
		deptDiffFn,
	)
	allDiff = append(allDiff, diffLogs...)
	return UpsertResults{Fetched: len(items), Created: created, Updated: updated, Skipped: skipped}, allDiff, err
}

func deptUpdateMap(_, src *model.EhrDepartment) map[string]any {
	return map[string]any{
		"department_code":        src.DepartmentCode,
		"department_name":        src.DepartmentName,
		"department_type":        src.DepartmentType,
		"department_type_name":   src.DepartmentTypeName,
		"parent_id":              src.ParentID,
		"leader_name":            src.LeaderName,
		"leader_passport":        src.LeaderPassport,
		"leader_person_id":       src.LeaderPersonID,
		"leader_employee_id":     src.LeaderEmployeeID,
		"manager_name":           src.ManagerName,
		"manager_passport":       src.ManagerPassport,
		"manager_person_id":      src.ManagerPersonID,
		"manager_employee_id":    src.ManagerEmployeeID,
		"hr_bp_name":             src.HrBpName,
		"hr_bp_employee_id":      src.HrBpEmployeeID,
		"ehr_modify_date":        src.EhrModifyDate,
		"synced_at":              time.Now(),
	}
}

func deptDiffFn(dst, src *model.EhrDepartment) (map[string]any, bool) {
	// override 检测：本地有 override => 跳过
	if dst.ManualOverrideAt != nil {
		return map[string]any{
			"department_name":     map[string]any{"old": dst.DepartmentName, "new": src.DepartmentName},
			"parent_id":           map[string]any{"old": dst.ParentID, "new": src.ParentID},
			"leader_employee_id":  map[string]any{"old": dst.LeaderEmployeeID, "new": src.LeaderEmployeeID},
			"manager_employee_id": map[string]any{"old": dst.ManagerEmployeeID, "new": src.ManagerEmployeeID},
		}, true
	}
	changes := map[string]any{}
	pairs := []struct {
		key string
		a, b string
	}{
		{"department_code", dst.DepartmentCode, src.DepartmentCode},
		{"department_name", dst.DepartmentName, src.DepartmentName},
		{"parent_id", dst.ParentID, src.ParentID},
		{"leader_employee_id", dst.LeaderEmployeeID, src.LeaderEmployeeID},
		{"manager_employee_id", dst.ManagerEmployeeID, src.ManagerEmployeeID},
		{"ehr_modify_date", dst.EhrModifyDate, src.EhrModifyDate},
	}
	for _, p := range pairs {
		if p.a != p.b {
			changes[p.key] = map[string]any{"old": p.a, "new": p.b}
		}
	}
	return changes, false
}

func (r *Repository) UpsertEmployees(items []model.EhrEmployee) (UpsertResults, []DiffEntry, error) {
	var allDiff []DiffEntry
	created, updated, skipped, diffLogs, err := upsertEntity(
		r.db, items,
		[]string{"org_id", "employee_id"},
		empUpdateMap,
		empDiffFn,
	)
	allDiff = append(allDiff, diffLogs...)
	return UpsertResults{Fetched: len(items), Created: created, Updated: updated, Skipped: skipped}, allDiff, err
}

func empUpdateMap(_, src *model.EhrEmployee) map[string]any {
	return map[string]any{
		"person_id":            src.PersonID,
		"passport":             src.Passport,
		"appoint_name":         src.AppointName,
		"appoint_code":         src.AppointCode,
		"office":               src.Office,
		"email":                src.Email,
		"person_status":        src.PersonStatus,
		"person_type":          src.PersonType,
		"is_default":           src.IsDefault,
		"company_id":           src.CompanyID,
		"company_name":         src.CompanyName,
		"department_id":        src.DepartmentID,
		"department_name":      src.DepartmentName,
		"position_id":          src.PositionID,
		"position_name":        src.PositionName,
		"position_category_id": src.PositionCategoryID,
		"manager_employee_id":  src.ManagerEmployeeID,
		"manager_person_id":    src.ManagerPersonID,
		"manager_passport":     src.ManagerPassport,
		"manager_name":         src.ManagerName,
		"person_name":          src.PersonName,
		"group_e_number":       src.GroupENumber,
		"gender":               src.Gender,
		"join_date":            src.JoinDate,
		"on_board_date":        src.OnBoardDate,
		"location":             src.Location,
		"dismiss_date":         src.DismissDate,
		"ehr_modify_date":      src.EhrModifyDate,
		"synced_at":            time.Now(),
	}
}

func empDiffFn(dst, src *model.EhrEmployee) (map[string]any, bool) {
	if dst.ManualOverrideAt != nil {
		return map[string]any{
			"department_id":       map[string]any{"old": dst.DepartmentID, "new": src.DepartmentID},
			"position_id":         map[string]any{"old": dst.PositionID, "new": src.PositionID},
			"person_status":       map[string]any{"old": dst.PersonStatus, "new": src.PersonStatus},
			"manager_employee_id": map[string]any{"old": dst.ManagerEmployeeID, "new": src.ManagerEmployeeID},
		}, true
	}
	changes := map[string]any{}
	pairs := []struct {
		key string
		a, b string
	}{
		{"department_id", dst.DepartmentID, src.DepartmentID},
		{"position_id", dst.PositionID, src.PositionID},
		{"person_status", fmt.Sprint(dst.PersonStatus), fmt.Sprint(src.PersonStatus)},
		{"manager_employee_id", dst.ManagerEmployeeID, src.ManagerEmployeeID},
		{"manager_name", dst.ManagerName, src.ManagerName},
		{"company_id", dst.CompanyID, src.CompanyID},
		{"ehr_modify_date", dst.EhrModifyDate, src.EhrModifyDate},
	}
	for _, p := range pairs {
		if p.a != p.b {
			changes[p.key] = map[string]any{"old": p.a, "new": p.b}
		}
	}
	return changes, false
}

func (r *Repository) UpsertPositions(items []model.EhrPosition) (UpsertResults, error) {
	created, updated, _, _, err := upsertEntity(
		r.db, items,
		[]string{"org_id", "position_id"},
		func(dst, src *model.EhrPosition) map[string]any {
			return map[string]any{
				"name":      src.Name,
				"type":      src.Type,
				"name_path": src.NamePath,
				"parent_id": src.ParentID,
				"synced_at": time.Now(),
			}
		},
		func(dst, src *model.EhrPosition) (map[string]any, bool) {
			changes := map[string]any{}
			if dst.Name != src.Name {
				changes["name"] = map[string]any{"old": dst.Name, "new": src.Name}
			}
			if dst.ParentID != src.ParentID {
				changes["parent_id"] = map[string]any{"old": dst.ParentID, "new": src.ParentID}
			}
			return changes, false
		},
	)
	return UpsertResults{Fetched: len(items), Created: created, Updated: updated}, err
}

// ---------- 辅助（避免 GORM clause import 警告） ----------

var _ = clause.OnConflict{}
var _ = uuid.New
