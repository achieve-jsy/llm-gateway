package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/orgservice/bootstrap"
	"github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/example/llm-gateway/internal/orgservice/repository"
	"gorm.io/gorm"
)

// OrgService 组织归集与实体管理服务
type OrgService struct {
	db          *gorm.DB
	studioRepo  *repository.StudioRepo
	deptRepo    *repository.DeptRepo
	mappingRepo *repository.MappingRepo
	userOrgRepo *repository.UserOrgRepo
	roleRepo    *repository.RoleRepo
	bootstrap   *bootstrap.Checker
}

// NewOrgService 创建 OrgService
func NewOrgService(
	db *gorm.DB,
	studioRepo *repository.StudioRepo,
	deptRepo *repository.DeptRepo,
	mappingRepo *repository.MappingRepo,
	userOrgRepo *repository.UserOrgRepo,
	roleRepo *repository.RoleRepo,
	bs *bootstrap.Checker,
) *OrgService {
	return &OrgService{
		db:          db,
		studioRepo:  studioRepo,
		deptRepo:    deptRepo,
		mappingRepo: mappingRepo,
		userOrgRepo: userOrgRepo,
		roleRepo:    roleRepo,
		bootstrap:   bs,
	}
}

// AggregateUser SSO 登录后归集员工到组织树
// 幂等：重复调用不产生重复记录
func (s *OrgService) AggregateUser(ctx context.Context, req model.AggregateRequest) (model.AggregateResult, error) {
	result := model.AggregateResult{}

	if req.DeptID == "" {
		slog.WarnContext(ctx, "[orgservice] AggregateUser: empty dept_id, skipping", "user_id", req.UserID)
		return result, nil
	}

	// 1. 幂等 upsert 部门（仅写 dept_id/name，不覆盖 studio_id/status）
	if err := s.deptRepo.UpsertDepartment(req.DeptID, req.DeptName); err != nil {
		return result, err
	}

	// 2. 查映射确定 studio_id 和 status
	// 优先按 SSO dept_id 查；若未命中则按 dept_name 兜底（import 以名称作为 dept_id 建立映射）
	mapping, err := s.mappingRepo.GetByDeptID(req.DeptID)
	if (err != nil || mapping == nil) && req.DeptName != "" {
		if fallback, fErr := s.mappingRepo.GetByDeptName(req.DeptName); fErr == nil && fallback != nil {
			mapping = fallback
			err = nil
			// 将此映射同步到当前 SSO dept_id，避免下次仍需兜底
			syncMap := &model.DeptStudioMap{
				DeptID:    req.DeptID,
				StudioID:  fallback.StudioID,
				Source:    "sso_sync",
				UpdatedBy: "system",
			}
			if syncErr := s.mappingRepo.Upsert(syncMap); syncErr != nil {
				slog.WarnContext(ctx, "[orgservice] failed to sync dept-studio mapping from name fallback",
					"dept_id", req.DeptID, "dept_name", req.DeptName, "error", syncErr)
			}
		}
	}
	if err == nil && mapping != nil {
		// 有映射：回填 studio_id + active
		if updateErr := s.deptRepo.UpdateStatus(req.DeptID, model.DeptStatusActive, mapping.StudioID); updateErr != nil {
			slog.WarnContext(ctx, "[orgservice] failed to update dept status", "dept_id", req.DeptID, "error", updateErr)
		}
		result.StudioID = mapping.StudioID
		result.IsPending = false
	} else {
		// 无映射：确保 status=pending_studio
		_ = s.deptRepo.UpdateStatus(req.DeptID, model.DeptStatusPendingStudio, "")
		result.IsPending = true
	}
	result.DeptID = req.DeptID

	// 3. 检测部门迁移（dept_id 变化）
	prevDeptID, _ := s.userOrgRepo.GetUserDeptIDByUserID(req.UserID)
	if prevDeptID != "" && prevDeptID != req.DeptID {
		log := &model.OrgMigrationLog{
			UserID:     req.UserID,
			FromDept:   prevDeptID,
			ToDept:     req.DeptID,
			MigratedAt: time.Now(),
		}
		if logErr := s.userOrgRepo.InsertMigrationLog(log); logErr != nil {
			slog.WarnContext(ctx, "[orgservice] failed to insert migration log", "error", logErr)
		}
	}

	// 4. Bootstrap 检查（是否需要授予初始 admin）
	if s.bootstrap != nil {
		s.bootstrap.CheckAndGrant(ctx, req.Email, req.UserID)
	}

	return result, nil
}

// GetOrgTree 返回完整组织树（供内部接口使用）
func (s *OrgService) GetOrgTree(ctx context.Context) (any, error) {
	studios, _, err := s.studioRepo.List(1, 1000)
	if err != nil {
		return nil, err
	}
	pendingDepts, _, _ := s.deptRepo.ListPending(1, 1000)

	type deptInfo struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Status      string `json:"status"`
		MemberCount int    `json:"member_count"`
	}
	type studioInfo struct {
		ID          string     `json:"id"`
		Name        string     `json:"name"`
		Departments []deptInfo `json:"departments"`
	}

	studioList := make([]studioInfo, 0, len(studios))
	for _, st := range studios {
		allDepts, _, _ := s.deptRepo.List("", 1, 1000)
		var depts []deptInfo
		for _, d := range allDepts {
			if d.StudioID != nil && *d.StudioID == st.ID {
				depts = append(depts, deptInfo{ID: d.DeptID, Name: d.DeptName, Status: string(d.Status)})
			}
		}
		studioList = append(studioList, studioInfo{ID: st.ID, Name: st.Name, Departments: depts})
	}

	unmapped := make([]deptInfo, 0, len(pendingDepts))
	for _, d := range pendingDepts {
		unmapped = append(unmapped, deptInfo{ID: d.DeptID, Name: d.DeptName, Status: string(d.Status)})
	}

	return map[string]any{
		"company": map[string]any{
			"studios":              studioList,
			"unmapped_departments": unmapped,
		},
	}, nil
}
