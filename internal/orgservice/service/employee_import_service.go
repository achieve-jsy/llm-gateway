package service

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmployeeImportService 员工花名册导入服务
type EmployeeImportService struct {
	db          *gorm.DB
	studioRepo  *repository.StudioRepo
	deptRepo    *repository.DeptRepo
	mappingRepo *repository.MappingRepo
	orgSyncer   OrgSyncer
}

func NewEmployeeImportService(
	db *gorm.DB,
	studioRepo *repository.StudioRepo,
	deptRepo *repository.DeptRepo,
	mappingRepo *repository.MappingRepo,
) *EmployeeImportService {
	return &EmployeeImportService{
		db:          db,
		studioRepo:  studioRepo,
		deptRepo:    deptRepo,
		mappingRepo: mappingRepo,
	}
}

// SetOrgSyncer 注入 KeyOrgSyncer
func (s *EmployeeImportService) SetOrgSyncer(syncer OrgSyncer) {
	s.orgSyncer = syncer
}

// ParseFile 解析上传文件，返回原始行列表。支持 .csv 和 .xlsx
func (s *EmployeeImportService) ParseFile(r io.Reader, filename string) ([]model.EmployeeImportRow, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".xlsx", ".xls":
		return s.parseXLSX(r)
	default:
		return s.parseCSV(r)
	}
}

func (s *EmployeeImportService) parseCSV(r io.Reader) ([]model.EmployeeImportRow, error) {
	reader := csv.NewReader(r)
	reader.LazyQuotes = true
	reader.TrimLeadingSpace = true

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("文件内容为空（至少需要表头行和一条数据）")
	}
	return s.recordsToRows(records[1:], 2), nil // 跳过表头，从第 2 行算起
}

func (s *EmployeeImportService) parseXLSX(r io.Reader) ([]model.EmployeeImportRow, error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, fmt.Errorf("Excel 解析失败: %w", err)
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取 Sheet 失败: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("文件内容为空（至少需要表头行和一条数据）")
	}
	return s.recordsToRows(rows[1:], 2), nil
}

// recordsToRows 将二维字符串数组按列约定映射到 EmployeeImportRow
// 列约定（0-indexed）: 工号 姓名 邮箱 事业部 部门 岗位族群 岗位类别 岗位
func (s *EmployeeImportService) recordsToRows(records [][]string, startLine int) []model.EmployeeImportRow {
	get := func(row []string, idx int) string {
		if idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	var rows []model.EmployeeImportRow
	for i, rec := range records {
		email := get(rec, 2)
		if email == "" {
			continue // 邮箱为空则跳过
		}
		rows = append(rows, model.EmployeeImportRow{
			RowIndex:    startLine + i,
			EmployeeID:  get(rec, 0),
			Name:        get(rec, 1),
			Email:       email,
			StudioName:  get(rec, 3),
			DeptName:    get(rec, 4),
			JobFamily:   get(rec, 5),
			JobCategory: get(rec, 6),
			JobTitle:    get(rec, 7),
		})
	}
	return rows
}

// Preview 预览（dry_run）：分析每行匹配状态，不写库
func (s *EmployeeImportService) Preview(
	ctx context.Context,
	rows []model.EmployeeImportRow,
) ([]model.EmployeeImportPreviewRow, error) {
	// 批量查已有工作室（按名称）
	studioNames := uniqueStrings(collect(rows, func(r model.EmployeeImportRow) string { return r.StudioName }))
	existingStudios, err := s.studiosByNames(studioNames)
	if err != nil {
		return nil, err
	}

	// 批量查已有部门（按 dept_name）
	deptNames := uniqueStrings(collect(rows, func(r model.EmployeeImportRow) string { return r.DeptName }))
	existingDepts, err := s.deptsByNames(deptNames)
	if err != nil {
		return nil, err
	}

	// 批量查已有用户（按邮箱）
	emails := collect(rows, func(r model.EmployeeImportRow) string { return r.Email })
	userMap, err := s.usersByEmails(emails)
	if err != nil {
		return nil, err
	}

	var previews []model.EmployeeImportPreviewRow
	for _, row := range rows {
		p := model.EmployeeImportPreviewRow{
			RowIndex:   row.RowIndex,
			EmployeeID: row.EmployeeID,
			Name:       row.Name,
			Email:      row.Email,
			StudioName: row.StudioName,
			DeptName:   row.DeptName,
			JobTitle:   row.JobTitle,
		}

		_, studioExists := existingStudios[strings.ToLower(row.StudioName)]
		_, deptExists := existingDepts[strings.ToLower(row.DeptName)]

		if u, ok := userMap[strings.ToLower(row.Email)]; ok {
			p.Status = model.ImportStatusLinked
			p.MatchedUserID = u.id
			p.MatchedUserName = u.name
		} else {
			p.Status = model.ImportStatusUnmatched
		}

		// 补充标注（不覆盖 unmatched，用 tag 字段记录）
		if !studioExists && row.StudioName != "" {
			p.NewStudio = true
		}
		if !deptExists && row.DeptName != "" {
			p.NewDept = true
		}

		previews = append(previews, p)
	}
	return previews, nil
}

// Import 正式导入，operatorID 用于 dept_studio_map.updated_by
// overrides: row_index -> user_id，用于手动指定未匹配行
func (s *EmployeeImportService) Import(
	ctx context.Context,
	rows []model.EmployeeImportRow,
	operatorID string,
	overrides map[int]string,
) (*model.EmployeeImportSummary, error) {
	summary := &model.EmployeeImportSummary{Total: len(rows)}
	var linkedUserIDs []string

	// ── 1. 收集所有工作室/部门名 ──────────────────────────────────────────────
	studioNames := uniqueStrings(collect(rows, func(r model.EmployeeImportRow) string { return r.StudioName }))
	deptNames := uniqueStrings(collect(rows, func(r model.EmployeeImportRow) string { return r.DeptName }))

	// ── 2. 批量 upsert 工作室 ─────────────────────────────────────────────────
	existingStudios, err := s.studiosByNames(studioNames)
	if err != nil {
		return nil, err
	}
	studioNameToID := make(map[string]string) // lower(name) -> id
	for k, v := range existingStudios {
		studioNameToID[k] = v.id
	}
	for _, name := range studioNames {
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := studioNameToID[key]; !ok {
			studio := model.Studio{Name: name}
			if err := s.studioRepo.Create(&studio); err != nil {
				return nil, fmt.Errorf("创建工作室 %q 失败: %w", name, err)
			}
			studioNameToID[key] = studio.ID
			summary.StudiosCreated++
		}
	}

	// ── 3. 批量 upsert 部门 ───────────────────────────────────────────────────
	existingDepts, err := s.deptsByNames(deptNames)
	if err != nil {
		return nil, err
	}
	// key: lower(dept_name) -> dept_id（varchar）
	deptNameToID := make(map[string]string)
	for k, v := range existingDepts {
		deptNameToID[k] = v.deptID
	}
	for _, name := range deptNames {
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := deptNameToID[key]; !ok {
			// dept_id 用 dept_name 本身（导入场景无 SSO dept_id）
			if err := s.deptRepo.UpsertDepartment(name, name); err != nil {
				return nil, fmt.Errorf("创建部门 %q 失败: %w", name, err)
			}
			deptNameToID[key] = name
			summary.DeptsCreated++
		}
	}

	// ── 4. 建立 dept → studio 映射 ───────────────────────────────────────────
	// 按 (deptName, studioName) 组合去重
	type deptStudioPair struct{ deptName, studioName string }
	pairSeen := map[deptStudioPair]bool{}
	for _, row := range rows {
		if row.DeptName == "" || row.StudioName == "" {
			continue
		}
		pair := deptStudioPair{row.DeptName, row.StudioName}
		if pairSeen[pair] {
			continue
		}
		pairSeen[pair] = true

		deptID := deptNameToID[strings.ToLower(row.DeptName)]
		studioID := studioNameToID[strings.ToLower(row.StudioName)]
		if deptID == "" || studioID == "" {
			continue
		}

		mapping := model.DeptStudioMap{
			DeptID:    deptID,
			StudioID:  studioID,
			Source:    "import",
			UpdatedBy: operatorID,
		}
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dept_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"studio_id", "source", "updated_by", "updated_at"}),
		}).Create(&mapping).Error; err != nil {
			return nil, fmt.Errorf("写 dept_studio_map 失败: %w", err)
		}
		// 同步 departments.studio_id + status
		if err := s.deptRepo.UpdateStatus(deptID, model.DeptStatusActive, studioID); err != nil {
			return nil, err
		}
		summary.DeptsMapped++
	}

	// ── 5. 用户匹配 & 归属 & HR 字段写入 ─────────────────────────────────────
	emails := collect(rows, func(r model.EmployeeImportRow) string { return r.Email })
	userMap, err := s.usersByEmails(emails)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		emailKey := strings.ToLower(row.Email)
		deptID := deptNameToID[strings.ToLower(row.DeptName)]

		var userID string
		if u, ok := userMap[emailKey]; ok {
			userID = u.id
		} else if oid, ok := overrides[row.RowIndex]; ok && oid != "" {
			userID = oid
		}

		if userID == "" {
			summary.UnmatchedEmails = append(summary.UnmatchedEmails, row.Email)
			continue
		}

		// 获取 departments.id（UUID）用于更新 users.dept_id
		var deptUUID string
		if deptID != "" {
			if err := s.db.Raw(`SELECT id::text FROM departments WHERE dept_id = ? LIMIT 1`, deptID).
				Scan(&deptUUID).Error; err != nil {
				return nil, err
			}
		}

		updates := map[string]any{
			"employee_id":  row.EmployeeID,
			"job_family":   row.JobFamily,
			"job_category": row.JobCategory,
			"job_title":    row.JobTitle,
		}
		if deptUUID != "" {
			updates["dept_id"] = deptUUID
		}

		if err := s.db.Table("users").Where("id::text = ?", userID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("更新用户 %s 失败: %w", row.Email, err)
		}
		summary.UsersLinked++
		linkedUserIDs = append(linkedUserIDs, userID)
	}

	// 通知 KeyOrgSyncer 同步受影响用户的 api_keys org 字段
	if s.orgSyncer != nil && len(linkedUserIDs) > 0 {
		go func() {
			for _, uid := range linkedUserIDs {
				s.orgSyncer.OnUserDeptChanged(context.Background(), uid)
			}
		}()
	}

	return summary, nil
}

// ─── 内部查询辅助 ──────────────────────────────────────────────────────────────

type studioInfo struct{ id, name string }
type deptInfo struct{ deptID, name string }
type userInfo struct{ id, name string }

func (s *EmployeeImportService) studiosByNames(names []string) (map[string]studioInfo, error) {
	if len(names) == 0 {
		return map[string]studioInfo{}, nil
	}
	var rows []struct {
		ID   string `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := s.db.Table("studios").Where("LOWER(name) IN ?", toLower(names)).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]studioInfo, len(rows))
	for _, r := range rows {
		m[strings.ToLower(r.Name)] = studioInfo{id: r.ID, name: r.Name}
	}
	return m, nil
}

func (s *EmployeeImportService) deptsByNames(names []string) (map[string]deptInfo, error) {
	if len(names) == 0 {
		return map[string]deptInfo{}, nil
	}
	var rows []struct {
		DeptID   string `gorm:"column:dept_id"`
		DeptName string `gorm:"column:dept_name"`
	}
	if err := s.db.Table("departments").Where("LOWER(dept_name) IN ?", toLower(names)).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]deptInfo, len(rows))
	for _, r := range rows {
		m[strings.ToLower(r.DeptName)] = deptInfo{deptID: r.DeptID, name: r.DeptName}
	}
	return m, nil
}

func (s *EmployeeImportService) usersByEmails(emails []string) (map[string]userInfo, error) {
	if len(emails) == 0 {
		return map[string]userInfo{}, nil
	}
	var rows []struct {
		ID    string `gorm:"column:id"`
		Name  string `gorm:"column:name"`
		Email string `gorm:"column:email"`
	}
	if err := s.db.Table("users").Select("id::text AS id, name, email").
		Where("LOWER(email) IN ?", toLower(emails)).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]userInfo, len(rows))
	for _, r := range rows {
		m[strings.ToLower(r.Email)] = userInfo{id: r.ID, name: r.Name}
	}
	return m, nil
}

// ─── 工具函数 ─────────────────────────────────────────────────────────────────

func collect[T any](slice []T, fn func(T) string) []string {
	out := make([]string, 0, len(slice))
	for _, v := range slice {
		out = append(out, fn(v))
	}
	return out
}

func uniqueStrings(ss []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range ss {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func toLower(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(s)
	}
	return out
}
