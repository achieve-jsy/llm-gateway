package service

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"gorm.io/gorm"
)

var (
	ErrImportInvalidStudio = errors.New("CSV 含非法 studio_id")
	ErrImportTooManyRows   = errors.New("超过 500 条导入上限")
)

// InvalidRow 非法行信息
type InvalidRow struct {
	Line     int    `json:"line"`
	DeptID   string `json:"dept_id"`
	StudioID string `json:"studio_id"`
	Reason   string `json:"reason"`
}

// ImportValidationError 导入校验错误
type ImportValidationError struct {
	InvalidRows []InvalidRow `json:"invalid_rows"`
}

func (e *ImportValidationError) Error() string {
	return fmt.Sprintf("导入校验失败：%d 行无效", len(e.InvalidRows))
}

// MappingService 映射维护业务逻辑
type MappingService struct {
	db          *gorm.DB
	mappingRepo *repository.MappingRepo
	deptRepo    *repository.DeptRepo
	studioRepo  *repository.StudioRepo
}

func NewMappingService(
	db *gorm.DB,
	mappingRepo *repository.MappingRepo,
	deptRepo *repository.DeptRepo,
	studioRepo *repository.StudioRepo,
) *MappingService {
	return &MappingService{
		db:          db,
		mappingRepo: mappingRepo,
		deptRepo:    deptRepo,
		studioRepo:  studioRepo,
	}
}

// ParseCSV 解析 CSV 内容，返回 ImportRow 列表（最多 500 条）
func (s *MappingService) ParseCSV(r io.Reader) ([]model.ImportRow, error) {
	cr := csv.NewReader(r)
	// 跳过 header
	if _, err := cr.Read(); err != nil {
		return nil, fmt.Errorf("读取 CSV header 失败: %w", err)
	}
	var rows []model.ImportRow
	lineNum := 1
	for {
		record, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV 解析错误 (行 %d): %w", lineNum+1, err)
		}
		if len(record) < 2 {
			return nil, fmt.Errorf("CSV 格式错误 (行 %d): 期望 dept_id,studio_id", lineNum+1)
		}
		rows = append(rows, model.ImportRow{
			DeptID:   strings.TrimSpace(record[0]),
			StudioID: strings.TrimSpace(record[1]),
		})
		lineNum++
		if len(rows) > 500 {
			return nil, ErrImportTooManyRows
		}
	}
	return rows, nil
}

// DryRun 预览导入 diff，不写入数据库
func (s *MappingService) DryRun(ctx context.Context, rows []model.ImportRow, updatedBy string) (*model.ImportDiff, error) {
	// 校验 studio_id
	if err := s.validateStudioIDs(ctx, rows); err != nil {
		return nil, err
	}
	// 查已有映射
	deptIDs := make([]string, len(rows))
	for i, r := range rows {
		deptIDs[i] = r.DeptID
	}
	existing, err := s.mappingRepo.GetExistingMappings(deptIDs)
	if err != nil {
		return nil, err
	}

	diff := &model.ImportDiff{}
	for _, row := range rows {
		if old, exists := existing[row.DeptID]; exists {
			if old != row.StudioID {
				diff.Updated = append(diff.Updated, struct {
					DeptID      string `json:"dept_id"`
					OldStudioID string `json:"old_studio_id"`
					NewStudioID string `json:"new_studio_id"`
				}{DeptID: row.DeptID, OldStudioID: old, NewStudioID: row.StudioID})
			} else {
				diff.Unchanged++
			}
		} else {
			diff.Added = append(diff.Added, row)
		}
	}
	return diff, nil
}

// Import 执行导入（单事务 upsert + 回填 pending_studio 部门）
func (s *MappingService) Import(ctx context.Context, rows []model.ImportRow, updatedBy string) (*model.ImportResult, error) {
	// 1. 校验 studio_id 存在性
	if err := s.validateStudioIDs(ctx, rows); err != nil {
		return nil, err
	}

	result := &model.ImportResult{}

	// 2. 单事务 upsert + 回填
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		mapRows := make([]model.DeptStudioMap, 0, len(rows))
		for _, r := range rows {
			mapRows = append(mapRows, model.DeptStudioMap{
				DeptID:    r.DeptID,
				StudioID:  r.StudioID,
				Source:    "import",
				UpdatedBy: updatedBy,
				UpdatedAt: now,
			})
		}
		// upsert 映射
		if err := s.mappingRepo.UpsertBatch(tx, mapRows); err != nil {
			return fmt.Errorf("upsert 映射失败: %w", err)
		}
		// 按 studio 分组回填 dept status
		studioDepts := make(map[string][]string)
		for _, r := range rows {
			studioDepts[r.StudioID] = append(studioDepts[r.StudioID], r.DeptID)
		}
		backfilled := 0
		for studioID, deptIDs := range studioDepts {
			before := tx.Model(&model.OrgDepartment{}).Where("dept_id IN ? AND status = ?", deptIDs, model.DeptStatusPendingStudio)
			var cnt int64
			before.Count(&cnt)
			if err := s.deptRepo.BackfillActiveByDeptIDs(tx, deptIDs, studioID); err != nil {
				return fmt.Errorf("回填 dept status 失败: %w", err)
			}
			backfilled += int(cnt)
		}
		result.Updated = len(rows)
		result.Backfilled = backfilled
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// SetDeptStudio 手工设置单个部门的工作室映射。
// studioID 为空字符串时表示"移出工作室"：删除映射记录，部门置为 pending_studio。
func (s *MappingService) SetDeptStudio(ctx context.Context, deptID, studioID, updatedBy string) error {
	if studioID == "" {
		// 移出工作室：删除映射 + 部门置 pending_studio
		if err := s.mappingRepo.DeleteByDeptID(deptID); err != nil {
			return err
		}
		return s.deptRepo.UpdateStatus(deptID, model.DeptStatusPendingStudio, "")
	}
	// 校验 studio 存在
	if _, err := s.studioRepo.GetByID(studioID); err != nil {
		return fmt.Errorf("%w: studio_id=%s", ErrStudioNotFound, studioID)
	}
	m := &model.DeptStudioMap{
		DeptID:    deptID,
		StudioID:  studioID,
		Source:    "manual",
		UpdatedBy: updatedBy,
		UpdatedAt: time.Now(),
	}
	if err := s.mappingRepo.Upsert(m); err != nil {
		return err
	}
	return s.deptRepo.UpdateStatus(deptID, model.DeptStatusActive, studioID)
}

// List 查询映射列表
func (s *MappingService) List(ctx context.Context, page, pageSize int) ([]model.DeptStudioMap, int64, error) {
	return s.mappingRepo.List(page, pageSize)
}

// validateStudioIDs 校验所有 studio_id 存在
func (s *MappingService) validateStudioIDs(ctx context.Context, rows []model.ImportRow) error {
	studioIDs := make([]string, 0, len(rows))
	seen := map[string]bool{}
	for _, r := range rows {
		if !seen[r.StudioID] {
			studioIDs = append(studioIDs, r.StudioID)
			seen[r.StudioID] = true
		}
	}
	existMap, err := s.studioRepo.ExistsByIDs(studioIDs)
	if err != nil {
		return err
	}
	var invalid []InvalidRow
	for i, r := range rows {
		if !existMap[r.StudioID] {
			invalid = append(invalid, InvalidRow{
				Line:     i + 2, // header 占第 1 行
				DeptID:   r.DeptID,
				StudioID: r.StudioID,
				Reason:   "studio_id 不存在",
			})
		}
	}
	if len(invalid) > 0 {
		return &ImportValidationError{InvalidRows: invalid}
	}
	return nil
}
