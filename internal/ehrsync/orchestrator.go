package ehrsync

import (
	"context"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/ehrclient"
	"github.com/google/uuid"
)

// Orchestrator 编排整个 EHR 同步流程：多组织循环 + 多实体编排
type Orchestrator struct {
	client    *ehrclient.Client
	repo      *Repository
	cursor    *CursorManager
	recorder  *LogRecorder
	log       *slog.Logger
	batchSize int
}

// OrchestratorDeps 外部依赖
type OrchestratorDeps struct {
	Client    *ehrclient.Client
	Repo      *Repository
	Cursor    *CursorManager
	Recorder  *LogRecorder
	Log       *slog.Logger
	BatchSize int // 单批大小，默认 200
}

// NewOrchestrator 构造
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	if deps.BatchSize <= 0 {
		deps.BatchSize = 200
	}
	return &Orchestrator{
		client:    deps.Client,
		repo:      deps.Repo,
		cursor:    deps.Cursor,
		recorder:  deps.Recorder,
		log:       deps.Log,
		batchSize: deps.BatchSize,
	}
}

// RunIncrementalResult 增量同步结果汇总
type RunIncrementalResult struct {
	BatchID      string
	DurationMs   int64
	ByOrg        map[string]*OrgSyncResult
	CallCount    int
	ErrorCount   int
}

// OrgSyncResult 单个组织的同步结果
type OrgSyncResult struct {
	OrgID       string
	OrgName     string
	ByEntity    map[string]*EntitySyncResult
	ErrorMsg    string
	StartedAt   time.Time
	FinishedAt  time.Time
}

// EntitySyncResult 单个实体的同步结果
type EntitySyncResult struct {
	Entity           string
	Fetched          int64
	Created          int64
	Updated          int64
	Skipped          int64
	CountEHRRequests int
	ErrCount         int
	Duration         time.Duration
	Error            string
}

// RunIncremental 执行一次增量同步（跨所有组织）
func (o *Orchestrator) RunIncremental(ctx context.Context) (*RunIncrementalResult, error) {
	batchID := uuid.NewString()
	started := time.Now()

	result := &RunIncrementalResult{
		BatchID: batchID,
		ByOrg:   make(map[string]*OrgSyncResult),
	}

	// 1. 拉取全部组织
	orgs, err := o.client.ListOrganizations(ctx)
	if err != nil {
		o.log.Error("incremental sync failed to list organizations", "error", err)
		result.ErrorCount++
		return result, err
	}
	result.CallCount++

	execLog := &model.EhrSyncExecutionLog{
		ID:          uuid.New(),
		BatchID:     batchID,
		Entity:      "organizations",
		OrgID:       "",
		SyncType:    "incremental",
		Status:      "running",
		TriggerType: "cron",
		StartedAt:   started,
	}

	// 2. 按组织依次同步
	for i, org := range orgs.Items {
		orgStart := time.Now()
		orgResult := &OrgSyncResult{
			OrgID:     org.OrgID,
			OrgName:   org.OrgName,
			ByEntity:  make(map[string]*EntitySyncResult),
			StartedAt: orgStart,
		}

		// 先 upsert 组织自身
		if err := o.upsertOrg(ctx, org); err != nil {
			o.log.Error("upsert organization failed", "orgId", org.OrgID, "error", err)
			orgResult.ErrorMsg = err.Error()
			result.ErrorCount++
			continue
		}

		// 按依赖顺序同步其他实体
		chain := []struct {
			name string
			fn   func(context.Context, string) *EntitySyncResult
		}{
			{"company", func(ctx context.Context, orgID string) *EntitySyncResult { return o.syncCompanies(ctx, orgID) }},
			{"department", func(ctx context.Context, orgID string) *EntitySyncResult { return o.syncDepartments(ctx, orgID) }},
			{"employee", func(ctx context.Context, orgID string) *EntitySyncResult { return o.syncEmployees(ctx, orgID) }},
		}

		for _, step := range chain {
			r := step.fn(ctx, org.OrgID)
			orgResult.ByEntity[step.name] = r
		result.CallCount += r.CountEHRRequests
		result.ErrorCount += r.ErrCount
		}

		orgResult.FinishedAt = time.Now()
		result.ByOrg[org.OrgID] = orgResult

		o.log.Info("org sync done",
			"orgId", org.OrgID,
			"orgName", org.OrgName,
			"index", i+1,
			"total", len(orgs.Items),
			"durationMs", time.Since(orgStart).Milliseconds())
	}

	// 3. 写全局 snapshot
	if err := o.writeGlobalSnapshot(ctx, batchID, execLog, result, started, "incremental"); err != nil {
		o.log.Error("write global snapshot failed", "error", err)
	}

	finished := time.Now()
	execLog.Status = "completed"
	if result.ErrorCount > 0 {
		execLog.Status = "partial_success"
	}
	execLog.FinishedAt = &finished
	execLog.DurationMs = time.Since(started).Milliseconds()
	execLog.RecordsFetched = sumOrgFetched(result)

	if e := o.recorder.RecordExecution(ctx, execLog); e != nil {
		o.log.Error("record execution log failed", "error", e)
	}

	result.DurationMs = time.Since(started).Milliseconds()
	o.log.Info("incremental sync done",
		"batchId", batchID,
		"durationMs", result.DurationMs,
		"orgCount", len(orgs.Items),
		"callCount", result.CallCount,
		"errorCount", result.ErrorCount)
	return result, nil
}

// ---------- 各实体同步步骤 ----------

func (o *Orchestrator) upsertOrg(ctx context.Context, org ehrclient.Organization) error {
	rec := toOrgModel(org)
	res, err := o.repo.UpsertOrganizations([]model.EhrOrganization{rec})
	if err != nil {
		return err
	}
	// 写 change log（创建 + 更新记录都记）
	if res.Created+res.Updated > 0 {
		changeType := "modified"
		if res.Created > 0 {
			changeType = "created"
		}
		logs := []model.EhrSyncChangeLog{{
			ID:         uuid.New(),
			BatchID:    uuid.NewString(),
			Entity:     "organization",
			EntityID:   org.OrgID,
			OrgID:      org.OrgID,
			ChangeType: changeType,
			OldValue:   "{}",
			NewValue:   MustJSON(org),
			Source:     "incremental",
			OccurredAt: time.Now(),
		}}
		if err := o.recorder.RecordChanges(ctx, logs); err != nil {
			o.log.Warn("record organization changes failed", "error", err)
		}
	}
	if org.LastModifyDate != "" {
		return o.cursor.UpdateIfGreater(ctx, "organization", org.OrgID, org.LastModifyDate)
	}
	return nil
}

func (o *Orchestrator) syncCompanies(ctx context.Context, orgID string) *EntitySyncResult {
	start := time.Now()
	r := &EntitySyncResult{Entity: "company"}

	repl, err := o.client.ListCompanies(ctx, orgID)
	if err != nil {
		r.Error = err.Error()
		r.ErrCount++
		r.CountEHRRequests++
		r.Duration = time.Since(start)
		return r
	}
	r.CountEHRRequests++
	if len(repl.Items) == 0 {
		r.Duration = time.Since(start)
		return r
	}

	// 转模型
	models := make([]model.EhrCompany, 0, len(repl.Items))
	for _, it := range repl.Items {
		models = append(models, toCompanyModel(orgID, it))
	}

	upRes, err := o.repo.UpsertCompanies(models)
	if err != nil {
		r.Error = err.Error()
		r.ErrCount++
		r.Duration = time.Since(start)
		return r
	}
	r.Fetched = int64(upRes.Fetched)
	r.Created = int64(upRes.Created)
	r.Updated = int64(upRes.Updated)

	// 写 change log（批量）
	if upRes.Created+upRes.Updated > 0 {
		changeType := "modified"
		if upRes.Created > 0 {
			changeType = "created"
		}
		logs := make([]model.EhrSyncChangeLog, 0, len(repl.Items))
		for _, it := range repl.Items {
			logs = append(logs, model.EhrSyncChangeLog{
				ID:         uuid.New(),
				BatchID:    uuid.NewString(),
				Entity:     "company",
				EntityID:   it.CompanyID,
				OrgID:      orgID,
				ChangeType: changeType,
				OldValue:   "{}",
				NewValue:   MustJSON(it),
				Source:     "incremental",
				OccurredAt: time.Now(),
			})
		}
		if err := o.recorder.RecordChanges(ctx, logs); err != nil {
			o.log.Warn("record company changes failed", "error", err)
		}
	}

	// 更新 cursor
	var maxMod string
	for _, it := range repl.Items {
		if it.LastModifyDate > maxMod {
			maxMod = it.LastModifyDate
		}
	}
	if maxMod != "" {
		if err := o.cursor.UpdateIfGreater(ctx, "company", orgID, maxMod); err != nil {
			o.log.Warn("update company cursor failed", "orgId", orgID, "error", err)
		}
	}

	r.Duration = time.Since(start)
	return r
}

func (o *Orchestrator) syncDepartments(ctx context.Context, orgID string) *EntitySyncResult {
	start := time.Now()
	r := &EntitySyncResult{Entity: "department"}

	repl, err := o.client.ListDepartments(ctx, orgID, nil)
	if err != nil {
		r.Error = err.Error()
		r.ErrCount++
		r.CountEHRRequests++
		r.Duration = time.Since(start)
		return r
	}
	r.CountEHRRequests++
	if len(repl.Items) == 0 {
		r.Duration = time.Since(start)
		return r
	}

	// 分批 + cursor 过滤
	batchResult := new(UpsertResults)
	var allSkips []model.EhrSyncSkipLog
	var logs []model.EhrSyncChangeLog
	batchID := uuid.NewString()

	for _, chunk := range chunkSlice(repl.Items, o.batchSize) {
		models := make([]model.EhrDepartment, 0, len(chunk))
		for _, it := range chunk {
			models = append(models, toDepartmentModel(orgID, it))
		}
		res, skipEntries, err := o.repo.UpsertDepartments(models)
		if err != nil {
			r.Error = err.Error()
			r.ErrCount++
			r.Duration = time.Since(start)
			return r
		}
		batchResult.Merge(res)
		allSkips = append(allSkips, SkipsFromOverride(batchID, "department", orgID, skipEntries)...)

		// 写变更 change log
		if res.Created+res.Updated > 0 {
			changeType := "modified"
			if res.Created > 0 {
				changeType = "created"
			}
			for _, it := range chunk {
				logs = append(logs, model.EhrSyncChangeLog{
					ID:         uuid.New(),
					BatchID:    batchID,
					Entity:     "department",
					EntityID:   it.DepartmentID,
					OrgID:      orgID,
					ChangeType: changeType,
					OldValue:   "{}",
					NewValue:   MustJSON(it),
					Source:     "incremental",
					OccurredAt: time.Now(),
				})
			}
		}
	}

	if err := o.recorder.RecordChanges(ctx, logs); err != nil {
		o.log.Warn("record department changes failed", "error", err)
	}

	// 写 skip logs
	if err := o.recorder.RecordSkips(ctx, allSkips); err != nil {
		o.log.Warn("record department skips failed", "error", err)
	}
	r.Skipped = int64(batchResult.Skipped)
	r.Fetched = int64(batchResult.Fetched)
	r.Created = int64(batchResult.Created)
	r.Updated = int64(batchResult.Updated)

	// 更新 cursor
	var maxMod string
	for _, it := range repl.Items {
		if it.LastModifyDate > maxMod {
			maxMod = it.LastModifyDate
		}
	}
	if maxMod != "" {
		if err := o.cursor.UpdateIfGreater(ctx, "department", orgID, maxMod); err != nil {
			o.log.Warn("update department cursor failed", "orgId", orgID, "error", err)
		}
	}

	r.Duration = time.Since(start)
	return r
}

func (o *Orchestrator) syncEmployees(ctx context.Context, orgID string) *EntitySyncResult {
	start := time.Now()
	r := &EntitySyncResult{Entity: "employee"}

	// 先拉 departments 获取所有 deptId，再按 deptId 拉员工
	depts, err := o.client.ListDepartments(ctx, orgID, nil)
	if err != nil {
		r.Error = err.Error()
		r.ErrCount++
		r.CountEHRRequests++
		r.Duration = time.Since(start)
		return r
	}
	r.CountEHRRequests++
	deptIDs := make([]string, 0, len(depts.Items))
	for _, d := range depts.Items {
		deptIDs = append(deptIDs, d.DepartmentID)
	}

	// 增量 cursor 过滤：取 cursor 之后有变化的 departmentId 子集
	cursor, _ := o.cursor.Get(ctx, "employee", orgID)

	var allSkips []model.EhrSyncSkipLog
	var changeLogs []model.EhrSyncChangeLog
	batchID := uuid.NewString()

	for _, chunk := range chunkSlice(deptIDs, 20) {
		repl, err := o.client.ListEmployees(ctx, orgID, chunk)
		if err != nil {
			r.Error = err.Error()
			r.ErrCount++
			r.CountEHRRequests++
			break
		}
		r.CountEHRRequests++
		if len(repl.Items) == 0 {
			continue
		}

		// 应用层过滤 cursor
		filtered := make([]ehrclient.Employee, 0, len(repl.Items))
		for _, it := range repl.Items {
			if cursor == "" || it.LastModifyDate > cursor {
				filtered = append(filtered, it)
			}
		}
		if len(filtered) == 0 {
			continue
		}

		models := make([]model.EhrEmployee, 0, len(filtered))
		for _, it := range filtered {
			models = append(models, toEmployeeModel(orgID, it))
		}
		if len(models) == 0 {
			continue
		}
		res, skipEntries, err := o.repo.UpsertEmployees(models)
		if err != nil {
			r.Error = err.Error()
			r.ErrCount++
			r.Duration = time.Since(start)
			return r
		}
		r.Fetched += int64(res.Fetched)
		r.Created += int64(res.Created)
		r.Updated += int64(res.Updated)
		r.Skipped += int64(res.Skipped)
		allSkips = append(allSkips, SkipsFromOverride(batchID, "employee", orgID, skipEntries)...)

		// 写变更 change log
		if res.Created+res.Updated > 0 {
			changeType := "modified"
			if res.Created > 0 {
				changeType = "created"
			}
			for _, it := range filtered {
				changeLogs = append(changeLogs, model.EhrSyncChangeLog{
					ID:         uuid.New(),
					BatchID:    batchID,
					Entity:     "employee",
					EntityID:   it.EmployeeID,
					OrgID:      orgID,
					ChangeType: changeType,
					OldValue:   "{}",
					NewValue:   MustJSON(it),
					Source:     "incremental",
					OccurredAt: time.Now(),
				})
			}
		}
	}

	if err := o.recorder.RecordChanges(ctx, changeLogs); err != nil {
		o.log.Warn("record employee changes failed", "error", err)
	}
	if err := o.recorder.RecordSkips(ctx, allSkips); err != nil {
		o.log.Warn("record employee skips failed", "error", err)
	}

	// 更新 cursor
	var maxMod string
	for _, it := range depts.Items {
		if it.LastModifyDate > maxMod {
			maxMod = it.LastModifyDate
		}
	}
	if maxMod != "" {
		_ = o.cursor.UpdateIfGreater(ctx, "employee", orgID, maxMod)
	}

	r.Duration = time.Since(start)
	return r
}

// ---------- 模型转换 ----------

func toOrgModel(src ehrclient.Organization) model.EhrOrganization {
	return model.EhrOrganization{
		OrgID:         src.OrgID,
		OrgName:       src.OrgName,
		OrgCode:       src.OrgCode,
		EhrCreateDate: src.CreateDate,
		EhrModifyDate: src.LastModifyDate,
		SyncedAt:      time.Now(),
	}
}

func toCompanyModel(orgID string, src ehrclient.Company) model.EhrCompany {
	return model.EhrCompany{
		OrgID:         orgID,
		CompanyID:     src.CompanyID,
		CompanyCode:   src.CompanyCode,
		CompanyName:   src.CompanyName,
		ParentID:      src.ParentID,
		EhrModifyDate: src.LastModifyDate,
		SyncedAt:      time.Now(),
	}
}

func toDepartmentModel(orgID string, src ehrclient.Department) model.EhrDepartment {
	return model.EhrDepartment{
		OrgID:              orgID,
		DepartmentID:       src.DepartmentID,
		DepartmentCode:     src.DepartmentCode,
		DepartmentName:     src.DepartmentName,
		DepartmentType:     src.DepartmentType,
		DepartmentTypeName: src.DepartmentTypeName,
		ParentID:           src.ParentID,
		LeaderName:         src.LeaderName,
		LeaderPassport:     src.LeaderPassport,
		LeaderPersonID:     src.LeaderPersonID,
		LeaderEmployeeID:   src.LeaderEmployeeID,
		ManagerName:        src.ManagerName,
		ManagerPassport:    src.ManagerPassport,
		ManagerPersonID:    src.ManagerPersonID,
		ManagerEmployeeID:  src.ManagerEmployeeID,
		HrBpName:           src.HrBpName,
		HrBpEmployeeID:     src.HrBpEmployeeID,
		EhrModifyDate:      src.LastModifyDate,
		SyncedAt:           time.Now(),
	}
}

func toEmployeeModel(orgID string, src ehrclient.Employee) model.EhrEmployee {
	return model.EhrEmployee{
		OrgID:              orgID,
		EmployeeID:         src.EmployeeID,
		PersonID:           src.PersonID,
		Passport:           src.Passport,
		AppointName:        src.AppointName,
		AppointCode:        src.AppointCode,
		Office:             src.Office,
		Email:              src.Email,
		PersonStatus:       src.PersonStatus,
		PersonType:         src.PersonType,
		IsDefault:          src.IsDefault,
		CompanyID:          src.CompanyID,
		CompanyName:        src.CompanyName,
		DepartmentID:       src.DepartmentID,
		DepartmentName:     src.DepartmentName,
		PositionID:         src.PositionID,
		PositionName:       src.PositionName,
		PositionCategoryID: src.PositionCategoryID,
		ManagerEmployeeID:  src.ManagerEmployeeID,
		ManagerPersonID:    src.ManagerPersonID,
		ManagerPassport:    src.ManagerPassport,
		ManagerName:        src.ManagerName,
		PersonName:         src.PersonName,
		GroupENumber:       src.GroupENumber,
		Gender:             src.Gender,
		JoinDate:           src.JoinDate,
		OnBoardDate:        src.OnBoardDate,
		Location:           src.Location,
		DismissDate:        src.DismissDate,
		EhrModifyDate:      src.LastModifyDate,
		SyncedAt:           time.Now(),
	}
}

// ---------- 辅助 ----------

func (o *Orchestrator) writeGlobalSnapshot(
	ctx context.Context, batchID string, execLog *model.EhrSyncExecutionLog,
	result *RunIncrementalResult, started time.Time, syncType string,
) error {
	now := time.Now()
	snap := &model.EhrSyncStatSnapshot{
		ID:                 uuid.New(),
		OrgID:              "aggregate",
		SyncedAt:           now,
		TotalOrganizations: int64(len(result.ByOrg)),
		LastIncrementalMs:  time.Since(started).Milliseconds(),
		EHRCallCount:       int64(result.CallCount),
		EHRErrorCount:      int64(result.ErrorCount),
	}
	if syncType == "full" {
		snap.LastFullReconcileMs = snap.LastIncrementalMs
		snap.LastIncrementalMs = 0
	}
	return o.recorder.RecordStatSnapshot(ctx, snap)
}

func sumOrgFetched(r *RunIncrementalResult) int64 {
	var total int64
	for _, org := range r.ByOrg {
		for _, e := range org.ByEntity {
			total += e.Fetched
		}
	}
	return total
}

func chunkSlice[T any](s []T, chunkSize int) [][]T {
	if chunkSize <= 0 || len(s) == 0 {
		return nil
	}
	chunks := make([][]T, 0, (len(s)+chunkSize-1)/chunkSize)
	for start := 0; start < len(s); start += chunkSize {
		end := start + chunkSize
		if end > len(s) {
			end = len(s)
		}
		chunks = append(chunks, s[start:end])
	}
	return chunks
}

func (r *EntitySyncResult) CountEHRRequestsAdd(n int) { r.CountEHRRequests += n }
