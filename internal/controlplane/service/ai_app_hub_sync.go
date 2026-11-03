package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrScopeNotFound 指定的部门/工作室不在本次拉取结果里（或指向手工维护的工作室）。
var ErrScopeNotFound = errors.New("sync scope not found")

// ScopeKind 同步范围。三种范围都只拉一次 EHR 全量（EHR 没有单节点接口），
// 差别只在写库范围。
type ScopeKind string

const (
	ScopeAll        ScopeKind = "all"
	ScopeDepartment ScopeKind = "department"
	ScopeStudio     ScopeKind = "studio"
)

type SyncScope struct {
	Kind ScopeKind
	ID   string
}

func (s SyncScope) String() string {
	if s.ID == "" {
		return string(s.Kind)
	}
	return string(s.Kind) + ":" + s.ID
}

// HubSyncResult 一次同步的统计，直接作为接口响应体。
type HubSyncResult struct {
	Scope       string `json:"scope"`
	Departments int    `json:"departments"`
	Studios     int    `json:"studios"`
	Hidden      int    `json:"hidden"`
	DurationMs  int64  `json:"duration_ms"`
}

// SyncRules 业务规则：哪些部门不展示、哪些要改名。写死在代码里，
// 由产品确认后修改，避免散落在数据库配置中难以追溯。
type SyncRules struct {
	HiddenDepartmentIDs map[string]bool
	NameOverrides       map[string]string
}

// DefaultSyncRules 公共关系事业部与元气工作室暂不展示；战略发展中心对外叫技术研究工作室。
func DefaultSyncRules() SyncRules {
	return SyncRules{
		HiddenDepartmentIDs: map[string]bool{
			"0B3CF753-9D6D-46E8-89C2-8E752D2A460F": true, // 公共关系事业部
			"08C6856B-D0D5-4FC4-830E-7D13441EA89F": true, // 元气工作室
		},
		NameOverrides: map[string]string{
			"53E83585-6CE3-48EF-8255-520BBAFCB156": "技术研究工作室", // 战略发展中心
		},
	}
}

// DepartmentFetcher 只依赖这一次调用，方便测试注入。
type DepartmentFetcher interface {
	FetchDepartments(ctx context.Context) ([]ehrclient.Department, error)
}

const (
	// rootParentID EHR 用全零 uuid 表示根节点。
	rootParentID = "00000000-0000-0000-0000-000000000000"
	// maxAncestorDepth 上溯上限，防止脏数据成环。
	maxAncestorDepth = 10
)

type AIAppHubSyncService struct {
	fetcher  DepartmentFetcher
	repo     *repository.AIAppHubRepo
	settings *repository.SettingsRepo
	rules    SyncRules
}

func NewAIAppHubSyncService(f DepartmentFetcher, repo *repository.AIAppHubRepo, settings *repository.SettingsRepo, rules SyncRules) *AIAppHubSyncService {
	return &AIAppHubSyncService{fetcher: f, repo: repo, settings: settings, rules: rules}
}

// Sync 拉取一次 EHR 组织树，按 scope 决定写库范围。
//
// 失败时业务表一行都不写（本地保持上一次成功的数据），只把失败写进状态键。
func (s *AIAppHubSyncService) Sync(ctx context.Context, scope SyncScope) (HubSyncResult, error) {
	start := time.Now()
	items, err := s.fetcher.FetchDepartments(ctx)
	if err != nil {
		return HubSyncResult{}, err
	}

	byID := make(map[string]ehrclient.Department, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}

	targets, err := s.resolveTargets(ctx, scope, byID)
	if err != nil {
		return HubSyncResult{}, err
	}

	for _, it := range items {
		if !targets[it.ID] {
			continue
		}
		d := &model.AIAppDepartment{
			DepartmentID:   it.ID,
			Name:           it.Name,
			ParentID:       normalizeParent(it.ParentID),
			DepartmentType: int32(it.Type),
			DepartmentPath: it.Path,
			LeaderName:     it.LeaderName,
			LeaderPassport: it.LeaderPassport,
			SyncedAt:       time.Now(),
		}
		if err := s.repo.UpsertDepartment(ctx, d); err != nil {
			return HubSyncResult{}, err
		}
	}

	studioIDs, studios, hidden, err := s.syncStudios(ctx, items, targets)
	if err != nil {
		return HubSyncResult{}, err
	}

	for id := range targets {
		it, ok := byID[id]
		if !ok {
			continue
		}
		studioID := nearestStudioID(it, byID, studioIDs)
		if err := s.repo.SetDepartmentStudio(ctx, id, studioID); err != nil {
			return HubSyncResult{}, err
		}
	}

	s.markStatus(ctx, scope, "success", "")
	return HubSyncResult{
		Scope:       string(scope.Kind),
		Departments: len(targets),
		Studios:     studios,
		Hidden:      hidden,
		DurationMs:  time.Since(start).Milliseconds(),
	}, nil
}

// resolveTargets 计算本次要写哪些部门节点。
//
// all 写全部；department 只写那一个节点；studio 写工作室节点加它整棵子树。
func (s *AIAppHubSyncService) resolveTargets(ctx context.Context, scope SyncScope, nodes map[string]ehrclient.Department) (map[string]bool, error) {
	switch scope.Kind {
	case ScopeAll, "":
		targets := make(map[string]bool, len(nodes))
		for id := range nodes {
			targets[id] = true
		}
		return targets, nil

	case ScopeDepartment:
		if _, ok := nodes[scope.ID]; !ok {
			return nil, ErrScopeNotFound
		}
		return map[string]bool{scope.ID: true}, nil

	case ScopeStudio:
		studioID, err := uuid.Parse(scope.ID)
		if err != nil {
			return nil, ErrScopeNotFound
		}
		studio, err := s.repo.GetStudioByID(ctx, studioID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrScopeNotFound
			}
			return nil, err
		}
		// 手工维护的工作室没有 EHR 部门 ID，无从拉取子树。
		if studio.DepartmentID == nil || *studio.DepartmentID == "" {
			return nil, ErrScopeNotFound
		}
		root := *studio.DepartmentID
		if _, ok := nodes[root]; !ok {
			return nil, ErrScopeNotFound
		}
		return subtreeOf(root, nodes), nil

	default:
		return nil, ErrScopeNotFound
	}
}

// subtreeOf 返回 root 及其全部后代的 ID 集合。
func subtreeOf(root string, nodes map[string]ehrclient.Department) map[string]bool {
	targets := map[string]bool{root: true}
	queue := []string{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for id, node := range nodes {
			if normalizeParent(node.ParentID) != parent || targets[id] {
				continue
			}
			targets[id] = true
			queue = append(queue, id)
		}
	}
	return targets
}

// syncStudios 为 targets 内的 type=2 节点建/更新工作室行，返回 departmentID -> studioID 映射。
func (s *AIAppHubSyncService) syncStudios(ctx context.Context, items []ehrclient.Department, targets map[string]bool) (map[string]uuid.UUID, int, int, error) {
	ids := make(map[string]uuid.UUID)
	studios, hidden := 0, 0
	for _, it := range items {
		if it.Type != 2 || !targets[it.ID] {
			continue
		}
		studios++
		name := it.Name
		if override, ok := s.rules.NameOverrides[it.ID]; ok {
			name = override
		}
		visible := !s.rules.HiddenDepartmentIDs[it.ID]
		if !visible {
			hidden++
		}

		existing, err := s.repo.GetStudioByDepartmentID(ctx, it.ID)
		switch {
		case err == nil:
			// manual 行是运营手工维护的，同步整行跳过。
			if existing.Source == "manual" {
				ids[it.ID] = existing.ID
				continue
			}
			if err := s.repo.UpdateStudioEHRFields(ctx, existing.ID, name, it.LeaderName, it.LeaderPassport, it.LeaderEmployeeID); err != nil {
				return nil, 0, 0, err
			}
			ids[it.ID] = existing.ID
		case errors.Is(err, gorm.ErrRecordNotFound):
			deptID := it.ID
			st := &model.AIAppStudio{
				DepartmentID:     &deptID,
				Name:             name,
				LeaderName:       it.LeaderName,
				LeaderPassport:   it.LeaderPassport,
				LeaderEmployeeID: it.LeaderEmployeeID,
				Source:           "ehr",
				Visible:          visible,
			}
			if _, err := s.repo.InsertStudioIfAbsent(ctx, st); err != nil {
				return nil, 0, 0, err
			}
			created, err := s.repo.GetStudioByDepartmentID(ctx, it.ID)
			if err != nil {
				return nil, 0, 0, err
			}
			ids[it.ID] = created.ID
		default:
			return nil, 0, 0, err
		}
	}
	return ids, studios, hidden, nil
}

// nearestStudioID 沿 parent 上溯到最近的事业部（type=2）；没有则返回 nil。
func nearestStudioID(node ehrclient.Department, byID map[string]ehrclient.Department, studioIDs map[string]uuid.UUID) *uuid.UUID {
	current := node
	for depth := 0; depth < maxAncestorDepth; depth++ {
		if id, ok := studioIDs[current.ID]; ok {
			studioID := id
			return &studioID
		}
		parentID := normalizeParent(current.ParentID)
		if parentID == "" {
			return nil
		}
		parent, ok := byID[parentID]
		if !ok {
			return nil
		}
		current = parent
	}
	slog.Warn("ai_app_hub_sync_ancestor_depth_exceeded", "department_id", node.ID)
	return nil
}

func normalizeParent(parentID string) string {
	if parentID == rootParentID {
		return ""
	}
	return parentID
}

// markStatus 记录最近一次同步结果，管理页据此展示"上次同步时间/结果"。
func (s *AIAppHubSyncService) markStatus(ctx context.Context, scope SyncScope, status, errMsg string) {
	if s.settings == nil {
		return
	}
	values := map[string]string{
		"ai_app_hub.last_sync_at":     time.Now().Format(time.RFC3339),
		"ai_app_hub.last_sync_status": status,
		"ai_app_hub.last_sync_scope":  scope.String(),
	}
	if errMsg != "" {
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
		values["ai_app_hub.last_sync_error"] = errMsg
	}
	if err := s.settings.SetMultiple(values); err != nil {
		slog.Error("ai_app_hub_sync_status_write_failed", "error", err)
	}
}
