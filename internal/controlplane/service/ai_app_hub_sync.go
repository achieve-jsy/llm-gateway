package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/example/llm-gateway/internal/aiapphub/ehrclient"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
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
	// personStatusResigned EHR 里"离职"的 person_status 取值。
	// 实测 128 人取 3，其中 121 人从未用过 Key，4 人近 2 个月还有活动 → 3 = 离职。
	personStatusResigned = 3

	// lastSyncEmployeesKey 上次成功写入的人员档案数。
	lastSyncEmployeesKey = "ai_app_hub.last_sync_employees"
	// lastSyncDiffKey 上次投影到老表的差量（JSON），管理页直接展示。
	lastSyncDiffKey = "ai_app_hub.last_diff"
	// lastSyncResignedKey 上次关停的离职账号数。
	lastSyncResignedKey = "ai_app_hub.last_sync_resigned_accounts"
	// lastSyncUnmappedKey 归属不到工作室的部门数（聚合页不可见，非 0 要查数据）。
	lastSyncUnmappedKey = "ai_app_hub.last_sync_unmapped_departments"

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
	// Employees 本轮写入的人员档案数（只有实现 EmployeeFetcher 的 fetcher 才会统计）。
	Employees int `json:"employees"`
	// ResignedAccounts 本轮关停的离职账号数（Key 关停 + login_status=disabled）。
	ResignedAccounts int `json:"resigned_accounts_disabled"`
	// UnmappedDepartments 归属不到任何工作室的部门数（EHR 里没有父节点的孤儿部门）。
	// 这些部门在聚合页不可见，非 0 时看日志里的名字确认是数据问题还是配置问题。
	UnmappedDepartments int `json:"unmapped_departments"`
	// DeletedStudios 本次对账隐藏的工作室数（只有 scope=all 会变）。
	DeletedStudios int   `json:"deleted_studios"`
	DurationMs     int64 `json:"duration_ms"`

	// Diff 老表投影的差量：日常应为个位数，突然涨到几十说明上游组织大改或接口变了。
	Diff SyncDiff `json:"diff"`
}

// ProjectionDiff 一张老表的增改删计数。
type ProjectionDiff struct {
	Inserted int `json:"inserted"`
	Updated  int `json:"updated"`
	Deleted  int `json:"deleted"`
}

// SyncDiff 投影到组织老表的差量统计。
type SyncDiff struct {
	Studios     ProjectionDiff `json:"studios"`
	Departments ProjectionDiff `json:"departments"`
	Mappings    ProjectionDiff `json:"mappings"`
}

// departmentTypeBusinessUnit EHR 里"事业部"的 departmentType，对应老表 studios。
const departmentTypeBusinessUnit = 2

// HubSyncStatus 管理页同步卡片读的状态。来源是 system_configs 里的 ai_app_hub.* 键 + 进程内锁。
type HubSyncStatus struct {
	LastSyncAt          string   `json:"last_sync_at"`
	Status              string   `json:"status"` // success | failed | ""（从未同步）
	Scope               string   `json:"scope"`
	Error               string   `json:"error"`
	Departments         int      `json:"departments"`
	Employees           int      `json:"employees"`
	UnmappedDepartments int      `json:"unmapped_departments"`
	ResignedAccounts    int      `json:"resigned_accounts_disabled"`
	Diff                SyncDiff `json:"diff"`
	// Running 进程内是否已有同步在跑（含 cron）。页面刷新后靠它把按钮留在"同步中"。
	Running bool `json:"running"`
}

// Status 读上次同步结果。settings 缺失（未初始化）时只返回 Running，页面显示"尚未同步"。
func (s *AIAppHubSyncService) Status(_ context.Context) (HubSyncStatus, error) {
	st := HubSyncStatus{Running: s.Running()}
	if s.settings == nil {
		return st, nil
	}
	get := func(key string) string {
		v, err := s.settings.GetValue(key)
		if err != nil {
			return ""
		}
		return v
	}
	st.LastSyncAt = get("ai_app_hub.last_sync_at")
	st.Status = get("ai_app_hub.last_sync_status")
	st.Scope = get("ai_app_hub.last_sync_scope")
	st.Error = get("ai_app_hub.last_sync_error")
	st.Departments = atoiOrZero(get(lastSyncDepartmentsKey))
	st.Employees = atoiOrZero(get(lastSyncEmployeesKey))
	st.UnmappedDepartments = atoiOrZero(get(lastSyncUnmappedKey))
	st.ResignedAccounts = atoiOrZero(get(lastSyncResignedKey))
	if raw := get(lastSyncDiffKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), &st.Diff)
	}
	return st, nil
}

func atoiOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
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

// EmployeeFetcher 可选的第二段能力：按 employeeId 批量拉人员档案。
//
// 用可选接口而不是塞进 DepartmentFetcher，是因为人员是辅助数据——
// 实现不了（或上游没授权）时组织树照常同步，不该让主流程跟着失败。
type EmployeeFetcher interface {
	FetchEmployees(ctx context.Context, employeeIDs []string) ([]ehrclient.Employee, error)
}

// RosterFetcher 全量同步才用的第三段能力：按工号/passport 反查档案。
//
// 作用是把"系统里已有的每一个人"补齐成完整档案——部门节点上只有负责人和各类 BP，
// 普通员工一个都没有；而工号（users.employee_id）足够把这些人全部反查出来。
type RosterFetcher interface {
	FetchEmployeesByGroupENumber(ctx context.Context, numbers []string) ([]ehrclient.Employee, error)
	FetchEmployeesByPassport(ctx context.Context, passports []string) ([]ehrclient.Employee, error)
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
	// legacy 组织老表投影（studios / departments / dept_studio_map）。
	// nil 表示未接投影：老单元测试不关心老表，生产路由必须接。
	legacy *orgrepo.EHRProjectionRepo
}

func NewAIAppHubSyncService(f DepartmentFetcher, repo *repository.AIAppHubRepo, settings *repository.SettingsRepo, rules SyncRules) *AIAppHubSyncService {
	return &AIAppHubSyncService{fetcher: f, repo: repo, settings: settings, rules: rules}
}

// WithProjection 接上老表投影。组织页面/RBAC/Key 快照都读老表，生产路由必须接。
func (s *AIAppHubSyncService) WithProjection(repo *orgrepo.EHRProjectionRepo) *AIAppHubSyncService {
	s.legacy = repo
	return s
}

// Running 是否有一次同步正在执行（含 cron 触发的）。管理页刷新后靠它把按钮留在"同步中"。
func (s *AIAppHubSyncService) Running() bool {
	if s.mu.TryLock() {
		s.mu.Unlock()
		return false
	}
	return true
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
			s.markStatus(scope, "failed", err.Error(), 0, 0, 0, 0, 0, SyncDiff{})
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

	// 人员档案：EHR 没授权"列全部人员"的路由，只能靠已知标识批量反查。
	// 失败只告警：组织树已经写完了，不值得为辅助数据整轮回滚。
	employees, resignedAccounts := s.syncEmployees(ctx, scope, byID, targets)

	var studioIDs map[string]uuid.UUID
	var studios, hidden int
	studioIDs, studios, hidden, err = s.syncStudios(ctx, items, targets)
	if err != nil {
		return HubSyncResult{}, err
	}

	var unmapped []string
	for id := range targets {
		it, ok := byID[id]
		if !ok {
			continue
		}
		studioID, resolved := s.nearestStudioID(ctx, it, byID, studioIDs)
		if !resolved {
			// 解析不出归属（例如祖先事业部本地还没有行）时保持原值，
			// 不能把上一轮同步算出的 studio_id 抹成 NULL。
			unmapped = append(unmapped, it.Name)
			continue
		}
		if err = s.repo.SetDepartmentStudio(ctx, id, studioID); err != nil {
			return HubSyncResult{}, err
		}
	}

	// 老表投影：组织页面 / RBAC / Key 快照读的都是老表，这一步让它们跟上 EHR。
	// 对账（软删）与窗口内的"上游只回了部分树"护栏共用同一判断：宁可漏删，不要误删。
	reconcileScope := scope.Kind == ScopeAll && len(items) > 0 && !s.reconcileLooksPartial(len(items))
	var diff SyncDiff
	diff, err = s.projectOrg(ctx, items, byID, studioIDs, targets, reconcileScope)
	if err != nil {
		return HubSyncResult{}, err
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

	if len(unmapped) > 0 {
		// 这些部门在聚合页上看不到，属于"数据有问题但不该让同步失败"的情况：
		// 打日志 + 计数，别让它静默消失。
		slog.Warn("ai_app_hub_sync_departments_without_studio", "count", len(unmapped), "names", unmapped)
	}
	s.markStatus(scope, "success", "", int(deleted), len(items), employees, len(unmapped), resignedAccounts, diff)
	return HubSyncResult{
		Scope:               string(scope.Kind),
		Departments:         len(targets),
		Studios:             studios,
		Hidden:              hidden,
		Employees:           employees,
		ResignedAccounts:    resignedAccounts,
		UnmappedDepartments: len(unmapped),
		DeletedStudios:      int(deleted),
		DurationMs:          time.Since(start).Milliseconds(),
		Diff:                diff,
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
// projectOrg 把本轮 EHR 数据投影到组织老表（studios / departments / dept_studio_map）。
//
// 只 upsert + 软删，永不 delete + insert：studios.id / departments.id 一动，
// api_keys / user_roles / call_logs / usage_daily 里的组织快照就跟页面上的组织对不上了。
// 返回差量，供管理页判断"这轮到底改了什么"。
func (s *AIAppHubSyncService) projectOrg(ctx context.Context, items []ehrclient.Department,
	byID map[string]ehrclient.Department, studioIDs map[string]uuid.UUID,
	targets map[string]bool, reconcile bool) (SyncDiff, error) {
	var diff SyncDiff
	if s.legacy == nil {
		return diff, nil
	}
	count := func(outcome string, bucket *ProjectionDiff) {
		switch outcome {
		case orgrepo.ProjectionInserted:
			bucket.Inserted++
		case orgrepo.ProjectionUpdated:
			bucket.Updated++
		}
	}

	// 事业部先写：部门要靠 studios.id 建外键，一轮里把事业部排在前面就不用等下一轮。
	// legacyStudioIDs：EHR 事业部 id → 老表 studios.id（不是 ai_app_studios.id，写错就 FK 违规）。
	legacyStudioIDs := make(map[string]uuid.UUID, 16)
	for _, it := range items {
		if it.Type != departmentTypeBusinessUnit || !targets[it.ID] {
			continue
		}
		name := it.Name
		if override, ok := s.rules.NameOverrides[it.ID]; ok {
			name = override
		}
		outcome, err := s.legacy.UpsertStudio(ctx, orgrepo.EHRStudioInput{
			EHRDepartmentID:  it.ID,
			Name:             name,
			LeaderName:       it.LeaderName,
			LeaderPassport:   it.LeaderPassport,
			LeaderEmployeeID: it.LeaderEmployeeID,
			Visible:          !s.rules.HiddenDepartmentIDs[it.ID],
		})
		if err != nil {
			return diff, err
		}
		count(outcome, &diff.Studios)
		legacyID, idErr := s.legacy.StudioIDByEHRDept(ctx, it.ID)
		if idErr != nil {
			continue
		}
		legacyStudioIDs[it.ID] = legacyID
		if leaderIsEmpty(it) {
			continue
		}
		if _, lErr := s.legacy.UpsertLeader(ctx, orgrepo.OrgLeaderInput{
			NodeType: orgmodel.LeaderNodeStudio, NodeID: legacyID,
			Name: it.LeaderName, Passport: it.LeaderPassport, EmployeeID: it.LeaderEmployeeID,
			Source: orgmodel.LeaderSourceEHR,
		}); lErr != nil {
			return diff, lErr
		}
	}

	legacyStudioOf := func(ehrStudioDeptID string) (uuid.UUID, bool) {
		if id, ok := legacyStudioIDs[ehrStudioDeptID]; ok {
			return id, true
		}
		id, err := s.legacy.StudioIDByEHRDept(ctx, ehrStudioDeptID)
		if err != nil {
			return uuid.Nil, false
		}
		legacyStudioIDs[ehrStudioDeptID] = id
		return id, true
	}

	for _, it := range items {
		if it.Type == departmentTypeBusinessUnit || !targets[it.ID] {
			continue
		}
		// 老表的归属以"事业部"为准，且必须换成老表自己的 studios.id。
		var studioID *uuid.UUID
		if ehrStudioDeptID, resolved := s.nearestStudioEHRDeptID(it, byID); resolved {
			if legacyID, ok := legacyStudioOf(ehrStudioDeptID); ok {
				studioID = &legacyID
			}
		}
		status := string(orgmodel.DeptStatusPendingStudio)
		if studioID != nil {
			status = string(orgmodel.DeptStatusActive)
		}
		outcome, deptRowID, err := s.legacy.UpsertDepartment(ctx, orgrepo.EHRDepartmentInput{
			DeptID: it.ID, DeptName: it.Name, StudioID: studioID, Status: status,
		})
		if err != nil {
			return diff, err
		}
		count(outcome, &diff.Departments)
		if !leaderIsEmpty(it) {
			if _, lErr := s.legacy.UpsertLeader(ctx, orgrepo.OrgLeaderInput{
				NodeType: orgmodel.LeaderNodeDept, NodeID: deptRowID,
				Name: it.LeaderName, Passport: it.LeaderPassport, EmployeeID: it.LeaderEmployeeID,
				Source: orgmodel.LeaderSourceEHR,
			}); lErr != nil {
				return diff, lErr
			}
		}
		if studioID != nil {
			mappingOutcome, mErr := s.legacy.UpsertMapping(ctx, it.ID, *studioID, "ehr-sync")
			if mErr != nil {
				return diff, mErr
			}
			count(mappingOutcome, &diff.Mappings)
		}
	}

	if !reconcile {
		return diff, nil
	}
	keepStudios := make([]string, 0, len(items))
	keepDepts := make([]string, 0, len(items))
	for _, it := range items {
		keepDepts = append(keepDepts, it.ID)
		if it.Type == departmentTypeBusinessUnit {
			keepStudios = append(keepStudios, it.ID)
		}
	}
	hidden, err := s.legacy.HideStudiosNotIn(ctx, keepStudios)
	if err != nil {
		return diff, err
	}
	diff.Studios.Deleted = hidden
	archived, err := s.legacy.ArchiveDepartmentsNotIn(ctx, keepDepts)
	if err != nil {
		return diff, err
	}
	diff.Departments.Deleted = archived
	return diff, nil
}

// leaderIsEmpty EHR 节点三个负责人字段全空就不投影，避免插出名/证/工号全空的垃圾行。
func leaderIsEmpty(d ehrclient.Department) bool {
	return d.LeaderName == "" && d.LeaderPassport == "" && d.LeaderEmployeeID == ""
}

// nearestStudioEHRDeptID 沿父链找到所属事业部的 **EHR 部门 ID**（type=2 的那个节点）。
// 与 nearestStudioID 的区别：后者返回本地 ai_app_studios.id，老表要的是 studios.id。
func (s *AIAppHubSyncService) nearestStudioEHRDeptID(node ehrclient.Department, byID map[string]ehrclient.Department) (string, bool) {
	current := node
	for depth := 0; depth < maxAncestorDepth; depth++ {
		if current.Type == departmentTypeBusinessUnit {
			return current.ID, true
		}
		parentID := normalizeParent(current.ParentID)
		if parentID == "" {
			return "", false
		}
		parent, ok := byID[parentID]
		if !ok {
			return "", false
		}
		current = parent
	}
	return "", false
}

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
			// 链顶也不是事业部：EHR 里有这类孤儿部门（例如 杭州舰宇/杭州边翼（新）
			// 的 parentId 是空的），它们挂在任何工作室下都无从推断 → 算"解析失败"，
			// 由调用方保留原值、计数并打日志，别让它静默从聚合页消失。
			return nil, false
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
func (s *AIAppHubSyncService) markStatus(scope SyncScope, status, errMsg string, deleted, departments, employees, unmapped, resigned int, diff SyncDiff) {
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
		values[lastSyncEmployeesKey] = strconv.Itoa(employees)
		values[lastSyncUnmappedKey] = strconv.Itoa(unmapped)
		values[lastSyncResignedKey] = strconv.Itoa(resigned)
		if raw, mErr := json.Marshal(diff); mErr == nil {
			values[lastSyncDiffKey] = string(raw)
		}
	} else {
		values["ai_app_hub.last_sync_error"] = truncateRunes(errMsg, 500)
	}
	if err := s.settings.SetMultiple(values); err != nil {
		slog.Error("ai_app_hub_sync_status_write_failed", "error", err)
	}
}

// truncateRunes 按字符截断：按字节切中文会在 Postgres 产生非法 UTF-8 而整条写入失败。
// syncEmployees 拉两段人员档案并落库，返回写入行数。
//
// 第一段：部门节点上 9 类岗位的 employeeId（负责人/主管/各 BP），能拿到没有本地账号的人。
// 第二段（仅全量同步）：拿本地账号的工号/passport 反查，覆盖部门节点根本点不到的普通员工。
//
// 每段各自容错：一段失败不影响另一段，也不影响组织树。
func (s *AIAppHubSyncService) syncEmployees(ctx context.Context, scope SyncScope, byID map[string]ehrclient.Department, targets map[string]bool) (int, int) {
	ef, ok := s.fetcher.(EmployeeFetcher)
	if !ok {
		return 0, 0
	}

	ids := make([]string, 0, len(targets))
	for id := range targets {
		if it, ok := byID[id]; ok {
			ids = append(ids, it.EmployeeIDs()...)
		}
	}
	emps, err := ef.FetchEmployees(ctx, ids)
	if err != nil {
		slog.Warn("ai_app_hub_employees_fetch_failed", "error", err, "candidate_ids", len(ids))
		emps = nil
	}

	if scope.Kind == ScopeAll {
		if rf, ok := s.fetcher.(RosterFetcher); ok {
			emps = append(emps, s.fetchRoster(ctx, rf)...)
		}
	}

	emps = dedupeEmployees(emps)
	n, err := s.repo.UpsertEmployees(ctx, toEmployeeModels(emps))
	if err != nil {
		slog.Warn("ai_app_hub_employees_upsert_failed", "error", err)
		return 0, 0
	}
	return n, s.disableResigned(ctx, emps)
}

// disableResigned 把 EHR 标记为离职的人在本地的账号关停。
// 失败只告警不中断：同步的价值在数据，封号是附加动作，不该让它整轮失败。
func (s *AIAppHubSyncService) disableResigned(ctx context.Context, emps []ehrclient.Employee) int {
	numbers := make([]string, 0, len(emps))
	for _, e := range emps {
		if e.PersonStatus == personStatusResigned && e.GroupENumber != "" {
			numbers = append(numbers, e.GroupENumber)
		}
	}
	if len(numbers) == 0 {
		return 0
	}
	stats, err := s.repo.DisableResignedAccounts(ctx, numbers)
	if err != nil {
		slog.Warn("ai_app_hub_disable_resigned_failed", "error", err, "candidates", len(numbers))
		return 0
	}
	slog.Info("ai_app_hub_disabled_resigned_accounts",
		"users", stats.UsersDisabled, "keys", stats.KeysDisabled,
		"skipped_recent_usage", stats.SkippedRecent, "resigned_people", len(numbers))
	return stats.UsersDisabled
}

// fetchRoster 按本地账号反查档案：有工号就用工号，没有才用 passport，避免同一批人查两遍。
func (s *AIAppHubSyncService) fetchRoster(ctx context.Context, rf RosterFetcher) []ehrclient.Employee {
	identities, err := s.repo.ListUserIdentities(ctx)
	if err != nil {
		slog.Warn("ai_app_hub_employees_identities_failed", "error", err)
		return nil
	}
	numbers := make([]string, 0, len(identities))
	passports := make([]string, 0, 16)
	for _, id := range identities {
		switch {
		case id.EmployeeID != "":
			numbers = append(numbers, id.EmployeeID)
		case id.Passport != "":
			passports = append(passports, id.Passport)
		}
	}

	// ponytail: 这批请求和部门同步共用 30s 超时。实测 600 个工号 = 60 次请求约 8 秒，
	// 组织树 1 秒，够用；真到几千人（几百次请求）时要给人员单独一个更长超时。
	out := make([]ehrclient.Employee, 0, len(identities))
	if len(numbers) > 0 {
		got, err := rf.FetchEmployeesByGroupENumber(ctx, numbers)
		if err != nil {
			slog.Warn("ai_app_hub_employees_roster_failed", "key", "groupENumbers", "count", len(numbers), "error", err)
		}
		out = append(out, got...)
	}
	if len(passports) > 0 {
		got, err := rf.FetchEmployeesByPassport(ctx, passports)
		if err != nil {
			slog.Warn("ai_app_hub_employees_roster_failed", "key", "passports", "count", len(passports), "error", err)
		}
		out = append(out, got...)
	}
	return out
}

// dedupeEmployees 同一个人只留一条，保留 isDefault 那条（一人多岗 / 换过 employeeId）。
//
// 按 passport 去重而不是 employeeId：线上 ehr_employees 有 UNIQUE(passport)，
// 实测 568 个档案里有 17 个 passport 对应多个 employeeId（离职再入职、历史重复档案），
// 不去重整批 upsert 会因唯一约束失败，一行都写不进去。
//
// ponytail: passport 为空的人只能留一条（唯一索引不允许第二条），
// 线上实测 0 例；真出现多人无 passport 时，该改索引而不是在这里凑。
func dedupeEmployees(emps []ehrclient.Employee) []ehrclient.Employee {
	if len(emps) < 2 {
		return emps
	}
	idx := make(map[string]int, len(emps))
	out := make([]ehrclient.Employee, 0, len(emps))
	for _, e := range emps {
		key := e.Passport
		if key == "" {
			key = "no-passport"
		}
		if i, ok := idx[key]; ok {
			if e.IsDefault && !out[i].IsDefault {
				out[i] = e
			}
			continue
		}
		idx[key] = len(out)
		out = append(out, e)
	}
	return out
}

// toEmployeeModels 把上游档案映射成落库行。时间戳按毫秒原样转字符串，
// 与线上 ehr_employees 的 varchar 列一致。
func toEmployeeModels(emps []ehrclient.Employee) []model.EHREmployee {
	now := time.Now()
	out := make([]model.EHREmployee, 0, len(emps))
	for _, e := range emps {
		out = append(out, model.EHREmployee{
			OrgID:             e.OrgID,
			EmployeeID:        e.EmployeeID,
			PersonID:          e.PersonID,
			Passport:          e.Passport,
			PersonName:        e.PersonName,
			GroupENumber:      e.GroupENumber,
			Email:             e.Email,
			Office:            e.Office,
			AppointName:       e.AppointName,
			AppointCode:       e.AppointCode,
			DepartmentID:      e.DepartmentID,
			DepartmentName:    e.DepartmentName,
			PersonStatus:      e.PersonStatus,
			PersonType:        e.PersonType,
			Gender:            e.Gender,
			IsDefault:         e.IsDefault,
			Location:          e.Location,
			ManagerEmployeeID: e.ManagerEmployeeID,
			ManagerPersonID:   e.ManagerPersonID,
			ManagerPassport:   e.ManagerPassport,
			ManagerName:       e.ManagerName,
			JoinDate:          epochMillis(e.JoinDate),
			OnBoardDate:       epochMillis(e.OnBoardDate),
			EHRModifyDate:     epochMillis(e.LastModifyDate),
			SyncedAt:          now,
			CreatedAt:         now,
			UpdatedAt:         now,
		})
	}
	return out
}

// epochMillis 毫秒时间戳转字符串；0 视为"上游没给"，存空串而不是 "0"。
func epochMillis(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return strconv.FormatInt(ms, 10)
}

func truncateRunes(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
