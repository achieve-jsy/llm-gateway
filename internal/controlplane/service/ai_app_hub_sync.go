package service

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrScopeNotFound 指定的部门/工作室不在本次拉取结果里（或指向手工维护的工作室）。
var ErrScopeNotFound = errors.New("sync scope not found")

// ErrSyncInProgress 已有一次同步在执行；同一时刻只允许一个，避免两个请求交错写库。
var ErrSyncInProgress = errors.New("sync in progress")

// syncTimeout 单次同步上限。全量 99 条实测 1-3 秒，30 秒足够兜住网络抖动。
const syncTimeout = 30 * time.Second

const (
	// lastSyncDepartmentsKey 上次成功同步到的部门数，用于识别"上游只回了部分树"。
	lastSyncDepartmentsKey = "ai_app_hub.last_sync_departments"
	// lastSyncDeletedKey 上次对账隐藏的工作室数（管理页展示）。
	lastSyncDeletedKey = "ai_app_hub.last_sync_deleted_studios"

	// syncReconcileMinRatio 本次部门数 / 上次成功部门数 低于它 → 认为上游只给了部分树，
	// 本轮跳过隐藏对账：宁可漏隐藏，也不要因为上游抽风把整棵组织树藏掉。
	// ponytail: 0.8 是拍的阈值，真观测到合法的大规模撤并再调（或改成"连续两次一致才隐藏"）。
	syncReconcileMinRatio = 0.8
	// syncReconcileMinPrev 上次部门数少于此值时不设阈值：小树与测试库里比例噪声太大。
	syncReconcileMinPrev = 10
)

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
	// DeletedStudios 本次对账隐藏的工作室数（只有 scope=all 会变）。
	DeletedStudios int   `json:"deleted_studios"`
	DurationMs     int64 `json:"duration_ms"`
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
	mu       sync.Mutex
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
// 失败时业务表不写（本地保持上一次成功的数据），并把 failed 写进状态键；
// 中途失败可能留下半套数据，状态里同样记为 failed，避免管理页显示"成功"。
func (s *AIAppHubSyncService) Sync(ctx context.Context, scope SyncScope) (result HubSyncResult, err error) {
	if !s.mu.TryLock() {
		return HubSyncResult{}, ErrSyncInProgress
	}
	defer s.mu.Unlock()

	// 状态写入不受本次同步超时影响：超时恰恰是最需要记下 failed 的场景。
	ctx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	start := time.Now()
	defer func() {
		if err != nil {
			s.markStatus(scope, "failed", err.Error(), 0, 0)
		}
	}()

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
		if err = s.repo.UpsertDepartment(ctx, d); err != nil {
			return HubSyncResult{}, err
		}
	}

	var studioIDs map[string]uuid.UUID
	var studios, hidden int
	studioIDs, studios, hidden, err = s.syncStudios(ctx, items, targets)
	if err != nil {
		return HubSyncResult{}, err
	}

	for id := range targets {
		it, ok := byID[id]
		if !ok {
			continue
		}
		studioID, resolved := s.nearestStudioID(ctx, it, byID, studioIDs)
		if !resolved {
			// 解析不出归属（例如祖先事业部本地还没有行）时保持原值，
			// 不能把上一轮同步算出的 studio_id 抹成 NULL。
			continue
		}
		if err = s.repo.SetDepartmentStudio(ctx, id, studioID); err != nil {
			return HubSyncResult{}, err
		}
	}

	// 全量同步收尾对账：EHR 里已经不存在的工作室隐藏。空结果代表上游异常
	// （ehrclient 已把空 items 转成 ErrEmptyResult），绝不据此隐藏任何东西。
	var deleted int64
	switch {
	case scope.Kind != ScopeAll || len(items) == 0:
		// 非全量不参与对账；空结果代表上游异常（ehrclient 已转成 ErrEmptyResult）。
	case s.reconcileLooksPartial(len(items)):
		slog.Error("ai_app_hub_sync_reconcile_skipped",
			"reason", "department count dropped", "current", len(items))
	default:
		keep := make([]string, 0, len(items))
		for _, it := range items {
			keep = append(keep, it.ID)
		}
		deleted, err = s.repo.HideStudiosNotInDepartments(ctx, keep)
		if err != nil {
			return HubSyncResult{}, err
		}
	}

	s.markStatus(scope, "success", "", int(deleted), len(items))
	return HubSyncResult{
		Scope:          string(scope.Kind),
		Departments:    len(targets),
		Studios:        studios,
		Hidden:         hidden,
		DeletedStudios: int(deleted),
		DurationMs:     time.Since(start).Milliseconds(),
	}, nil
}

// resolveTargets 计算本次要写哪些部门节点。
//
// all 写全部；department 只写那一个节点；studio 写工作室节点加它整棵子树。
// 空 Kind 视为非法——零值指向"全量"会让忘了传范围的调用方静默执行最重的操作。
func (s *AIAppHubSyncService) resolveTargets(ctx context.Context, scope SyncScope, nodes map[string]ehrclient.Department) (map[string]bool, error) {
	switch scope.Kind {
	case ScopeAll:
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

// nearestStudioID 沿 parent 上溯到最近的事业部（type=2）。
//
// 返回 (studioID, resolved)：resolved=false 表示"这次算不出来"（祖先事业部本地还没有行、
// 或层级超限），调用方应当保持原值而不是写 NULL；只有真正没有事业部祖先时才返回 (nil, true)。
func (s *AIAppHubSyncService) nearestStudioID(ctx context.Context, node ehrclient.Department, byID map[string]ehrclient.Department, studioIDs map[string]uuid.UUID) (*uuid.UUID, bool) {
	current := node
	for depth := 0; depth < maxAncestorDepth; depth++ {
		if id, ok := studioIDs[current.ID]; ok {
			studioID := id
			return &studioID, true
		}
		if current.Type == 2 {
			// 本次没有同步到的 type=2 祖先（例如单部门同步的上级事业部）：查本地已有行。
			existing, err := s.repo.GetStudioByDepartmentID(ctx, current.ID)
			if err != nil {
				return nil, false
			}
			studioID := existing.ID
			return &studioID, true
		}
		parentID := normalizeParent(current.ParentID)
		if parentID == "" {
			return nil, true
		}
		parent, ok := byID[parentID]
		if !ok {
			return nil, false
		}
		current = parent
	}
	slog.Warn("ai_app_hub_sync_ancestor_depth_exceeded", "department_id", node.ID)
	return nil, false
}

func normalizeParent(parentID string) string {
	if parentID == rootParentID {
		return ""
	}
	return parentID
}

// Authorize 判定当前用户能否触发某个范围的同步。
//
// userID 是 JWT 的 user_id（users.id，用于查 user_roles）；
// passport 是 EHR 账号（ai_app_studios.leader_passport 用的就是它）。两者不能混用。
func (s *AIAppHubSyncService) Authorize(ctx context.Context, userID, passport string, scope SyncScope) (bool, error) {
	if userID == "" && passport == "" {
		return false, nil
	}
	admin, err := s.repo.HasAnyRole(ctx, []string{userID, passport}, []string{"admin"})
	if err != nil {
		return false, err
	}
	if admin {
		return true, nil
	}

	switch scope.Kind {
	case ScopeAll:
		return false, nil
	case ScopeStudio:
		id, err := uuid.Parse(scope.ID)
		if err != nil {
			return false, nil
		}
		studio, err := s.repo.GetStudioByID(ctx, id)
		if err != nil {
			return false, nil
		}
		return isLeader(studio.LeaderPassport, passport), nil
	case ScopeDepartment:
		department, err := s.repo.GetDepartment(ctx, scope.ID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 还没同步过的部门：对调用方是"找不到这个范围"，不是"没权限"。
				return false, ErrScopeNotFound
			}
			return false, err
		}
		if department.StudioID == nil {
			return false, nil
		}
		studio, err := s.repo.GetStudioByID(ctx, *department.StudioID)
		if err != nil {
			return false, nil
		}
		return isLeader(studio.LeaderPassport, passport), nil
	default:
		return false, nil
	}
}

func isLeader(leaderPassport, passport string) bool {
	return leaderPassport != "" && passport != "" && leaderPassport == passport
}

// lastSyncedDepartments 上次成功同步的部门数；没记录或值坏了返回 (0, false)。
func (s *AIAppHubSyncService) lastSyncedDepartments() (int, bool) {
	if s.settings == nil {
		return 0, false
	}
	raw, err := s.settings.GetValue(lastSyncDepartmentsKey)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// reconcileLooksPartial 本次部门数比上次成功少太多，判定上游只返回了部分树。
func (s *AIAppHubSyncService) reconcileLooksPartial(current int) bool {
	prev, ok := s.lastSyncedDepartments()
	if !ok || prev < syncReconcileMinPrev {
		return false
	}
	return float64(current) < float64(prev)*syncReconcileMinRatio
}

// markStatus 记录最近一次同步结果，管理页据此展示"上次同步时间/结果"。
func (s *AIAppHubSyncService) markStatus(scope SyncScope, status, errMsg string, deleted, departments int) {
	if s.settings == nil {
		return
	}
	values := map[string]string{
		"ai_app_hub.last_sync_at":     time.Now().Format(time.RFC3339),
		"ai_app_hub.last_sync_status": status,
		"ai_app_hub.last_sync_scope":  scope.String(),
	}
	// 成功时显式清空：否则管理页会出现"成功但仍显示上次的错误"。
	if errMsg == "" {
		values["ai_app_hub.last_sync_error"] = ""
		values[lastSyncDeletedKey] = strconv.Itoa(deleted)
		values[lastSyncDepartmentsKey] = strconv.Itoa(departments)
	} else {
		values["ai_app_hub.last_sync_error"] = truncateRunes(errMsg, 500)
	}
	if err := s.settings.SetMultiple(values); err != nil {
		slog.Error("ai_app_hub_sync_status_write_failed", "error", err)
	}
}

// truncateRunes 按字符截断：按字节切中文会在 Postgres 产生非法 UTF-8 而整条写入失败。
func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
