package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/example/llm-gateway/internal/controlplane/budgetdomain"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type memberResolverFunc func(deptID string) ([]string, error)

func (f memberResolverFunc) ListMemberIDsByDeptID(deptID string) ([]string, error) {
	return f(deptID)
}

func newMultiRoleExpansionFixture(t *testing.T) (*gorm.DB, *service.ExpansionService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE expansion_requests (
			id text primary key, scope_type text, scope_id text, requester_id text, period_type text,
			dimension_type text, dimension_key text, model_tier_id text, duration_type text, priority text,
			current_amount real, requested_amount real, current_amount_snapshot numeric, used_amount_snapshot numeric,
			precheck_version text, prechecked_at datetime, expires_at datetime, applied_at datetime,
			applied_target_type text, applied_target_id text, sync_status text, reason text, urgent_reason text,
			status text, current_level text, created_at datetime, updated_at datetime
		)`,
		`CREATE TABLE expansion_approval_steps (
			id text primary key, request_id text, level text, approver_id text,
			action text, comment text, created_at datetime
		)`,
		`CREATE TABLE budget_allocations (
			id text primary key, scope_type text, scope_id text, amount_usd real,
			daily_amount_usd real, weekly_amount_usd real, period text, enabled boolean,
			created_by text, updated_by text, created_at datetime, updated_at datetime,
			unique(scope_type, scope_id)
		)`,
		`CREATE TABLE budget_temporary_overrides (
			id text primary key, budget_allocation_id text, dimension_type text, dimension_key text,
			model_tier_id text, period_type text, target_limit_usd numeric, starts_at datetime,
			expires_at datetime, expansion_request_id text, status text, failure_reason text,
			created_at datetime, updated_at datetime
		)`,
		`CREATE TABLE budget_tier_limits (
			id text primary key, budget_allocation_id text, model_tier_id text, period_type text,
			limit_usd numeric, effective_at datetime, enabled boolean, created_at datetime, updated_at datetime,
			unique(budget_allocation_id, model_tier_id, period_type)
		)`,
		`CREATE TABLE studios (id text primary key, name text)`,
		`CREATE TABLE departments (id text primary key, dept_id text, dept_name text, studio_id text)`,
		`CREATE TABLE dept_studio_map (dept_id text, studio_id text)`,
		`CREATE TABLE users (id text primary key, dept_id text)`,
		`CREATE TABLE user_roles (
			id integer primary key autoincrement, user_id text, role text, scope_type text, scope_id text,
			granted_by text, actor_id text, created_at datetime
		)`,
		`CREATE TABLE budget_alert_records (
			id text primary key, scope_type text, scope_id text, period text
		)`,
		`CREATE TABLE model_groups (
			id text primary key, name text, budget_tier_id text, deleted_at datetime
		)`,
		`CREATE TABLE notification_outbox (
			id text primary key, dedupe_key text unique, user_id text, type text, level text, title text, content text,
			ref_type text, ref_id text, status text, attempts integer, next_attempt datetime, last_error text,
			locked_at datetime, lease_owner text, lease_expires_at integer, delivered_at datetime, created_at datetime, updated_at datetime
		)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}

	expansionRepo := repository.NewExpansionRepo(db)
	budgetRepo := repository.NewBudgetRepo(db)
	budgetSvc := service.NewBudgetService(budgetRepo, repository.NewBudgetAlertRepo(db))
	mr := miniredis.RunT(t)
	budgetSvc.SetRedis(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	budgetTierSvc := service.NewBudgetTierService(
		repository.NewBudgetTierRepo(db), budgetRepo, repository.NewModelTierRepo(db), time.Now,
	)
	expansionSvc := service.NewExpansionService(expansionRepo)
	expansionSvc.SetBudgetService(budgetSvc)
	expansionSvc.SetBudgetTierService(budgetTierSvc)
	expansionSvc.WithApprovalPool(service.NewDatabaseApprovalPoolResolver(db))
	return db, expansionSvc
}

// TestMultiRoleActorSeesDeptAndStudioPendingV2 验证：同时持有 dept_admin + studio_admin 的用户，
// 其待审列表能同时看到“部门成员提出、当前到 dept 级”和“部门升级、当前到 studio 级”的扩容申请。
func TestMultiRoleActorSeesDeptAndStudioPendingV2(t *testing.T) {
	db, svc := newMultiRoleExpansionFixture(t)
	memberID := uuid.New().String()
	deptID := "dept-multi"
	studioID := uuid.New().String()

	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "多角色工作室").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('dept-row',?,?,?)`, deptID, "多角色部门", studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'dept-row')`, memberID).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_roles(user_id,role,scope_type,scope_id,granted_by) VALUES (?,'dept_admin','dept',?, 'tester')`, memberID, deptID).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_roles(user_id,role,scope_type,scope_id,granted_by) VALUES (?,'studio_admin','studio',?, 'tester')`, memberID, studioID).Error)

	for _, row := range []struct{ scopeType, scopeID string }{
		{model.BudgetScopeMember, memberID},
		{model.BudgetScopeDeptDaily, deptID},
		{model.BudgetScopeStudio, studioID},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, 1000, 1000, 1000, "monthly", true).Error)
	}
	svc.SetMemberResolver(memberResolverFunc(func(deptID string) ([]string, error) {
		if deptID == "dept-multi" {
			return []string{memberID}, nil
		}
		return nil, nil
	}))

	now := time.Now().UTC()
	memberReq := &model.ExpansionRequest{
		ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: memberID,
		RequesterID: uuid.MustParse(memberID), PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, Priority: model.ExpansionPriorityNormal,
		RequestedAmount: 50, Status: model.ExpansionStatusPending,
		CurrentLevel: model.ExpansionLevelDept, CreatedAt: now, UpdatedAt: now,
	}
	deptReq := &model.ExpansionRequest{
		ID: uuid.New(), ScopeType: model.BudgetScopeDeptDaily, ScopeID: deptID,
		RequesterID: uuid.MustParse(memberID), PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, Priority: model.ExpansionPriorityNormal,
		RequestedAmount: 200, Status: model.ExpansionStatusPending,
		CurrentLevel: model.ExpansionLevelStudio, CreatedAt: now.Add(time.Second), UpdatedAt: now,
	}
	for _, req := range []*model.ExpansionRequest{memberReq, deptReq} {
		require.NoError(t, db.Create(req).Error)
	}

	actor := service.ActorScope{
		UserID: memberID, Role: "studio_admin",
		Roles:   []string{"dept_admin", "studio_admin"},
		DeptIDs: []string{deptID}, StudioIDs: []string{studioID}, DeptAdminIDs: []string{deptID},
	}
	items, err := svc.ListPendingForActorScopedFiltered(context.Background(), actor, repository.ExpansionListFilter{})
	require.NoError(t, err)
	require.Len(t, items, 2, "多角色用户应同时看到 dept 级与 studio 级待审")

	levels := map[string]bool{}
	for _, it := range items {
		levels[it.CurrentLevel] = true
	}
	require.True(t, levels[model.ExpansionLevelDept], "应包含 dept 级（部门成员提出）")
	require.True(t, levels[model.ExpansionLevelStudio], "应包含 studio 级（升级到工作室）")
}

// TestMultiRoleActorStdioScopeDoesNotBleedUnrelatedMember 验证多角色用户在 dept 级别不会越权到其他部门成员的申请。
func TestMultiRoleActorStdioScopeDoesNotBleedUnrelatedMember(t *testing.T) {
	db, svc := newMultiRoleExpansionFixture(t)
	memberID := uuid.New().String()
	ownDeptID := "dept-own"
	studioID := uuid.New().String()
	otherDeptID := "dept-other"
	otherMember := uuid.New().String()

	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "工作室").Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('own-row',?,?,?)`, ownDeptID, "本部门", studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO departments(id,dept_id,dept_name,studio_id) VALUES ('other-row',?,?,?)`, otherDeptID, "其他部门", studioID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'own-row')`, memberID).Error)
	require.NoError(t, db.Exec(`INSERT INTO users(id,dept_id) VALUES (?,'other-row')`, otherMember).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_roles(user_id,role,scope_type,scope_id,granted_by) VALUES (?,'dept_admin','dept',?, 'tester')`, memberID, ownDeptID).Error)
	require.NoError(t, db.Exec(`INSERT INTO user_roles(user_id,role,scope_type,scope_id,granted_by) VALUES (?,'studio_admin','studio',?, 'tester')`, memberID, studioID).Error)

	for _, row := range []struct{ scopeType, scopeID string }{
		{model.BudgetScopeMember, otherMember},
		{model.BudgetScopeDeptDaily, otherDeptID},
		{model.BudgetScopeStudio, studioID},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, 1000, 1000, 1000, "monthly", true).Error)
	}
	svc.SetMemberResolver(memberResolverFunc(func(deptID string) ([]string, error) {
		if deptID == ownDeptID {
			return []string{memberID}, nil
		}
		if deptID == otherDeptID {
			return []string{otherMember}, nil
		}
		return nil, nil
	}))

	now := time.Now().UTC()
	otherReq := &model.ExpansionRequest{
		ID: uuid.New(), ScopeType: model.BudgetScopeMember, ScopeID: otherMember,
		RequesterID: uuid.MustParse(otherMember), PeriodType: string(budgetdomain.PeriodMonthly),
		DimensionType: budgetdomain.DimensionTotal, DimensionKey: "total",
		DurationType: budgetdomain.DurationPermanent, Priority: model.ExpansionPriorityNormal,
		RequestedAmount: 80, Status: model.ExpansionStatusPending,
		CurrentLevel: model.ExpansionLevelDept, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, db.Create(otherReq).Error)

	actor := service.ActorScope{
		UserID: memberID, Role: "studio_admin",
		Roles:   []string{"dept_admin", "studio_admin"},
		DeptIDs: []string{ownDeptID}, StudioIDs: []string{studioID}, DeptAdminIDs: []string{ownDeptID},
	}
	items, err := svc.ListPendingForActorScopedFiltered(context.Background(), actor, repository.ExpansionListFilter{})
	require.NoError(t, err)
	for _, it := range items {
		require.NotEqual(t, otherMember, it.ScopeID, "多角色用户不应看到其他部门成员申请")
	}
	_, err = svc.ListStepsScoped(context.Background(), actor, otherReq.ID.String())
	require.ErrorIs(t, err, service.ErrExpansionForbidden, "其他部门的 dept 级申请流水不应泄露")
}

// TestStudioEscalationCreatesPlatformNotificationForDualRoleAdmin 验证同一用户既是
// 工作室负责人又是平台管理员时，工作室申请升级到平台仍会创建新的平台待办。
// 待办去重必须包含审批层级，不能让旧的工作室待办吞掉平台待办。
func TestStudioEscalationCreatesPlatformNotificationForDualRoleAdmin(t *testing.T) {
	db, svc := newMultiRoleExpansionFixture(t)
	managerID := uuid.New()
	requesterID := uuid.New()
	studioID := uuid.NewString()
	requestID := uuid.New()

	require.NoError(t, db.Exec(`INSERT INTO studios(id,name) VALUES (?,?)`, studioID, "升级工作室").Error)
	require.NoError(t, db.Exec(`
		INSERT INTO user_roles(user_id,role,scope_type,scope_id,granted_by) VALUES
		(?, 'studio_admin', 'studio', ?, 'tester'),
		(?, 'admin', 'company', '', 'tester')
	`, managerID.String(), studioID, managerID.String()).Error)
	for _, row := range []struct{ scopeType, scopeID string }{
		{model.BudgetScopeStudio, studioID},
		{model.BudgetScopeCompany, model.BudgetScopeGlobal},
	} {
		require.NoError(t, db.Exec(`INSERT INTO budget_allocations
			(id,scope_type,scope_id,amount_usd,daily_amount_usd,weekly_amount_usd,period,enabled)
			VALUES (?,?,?,?,?,?,?,?)`, uuid.New(), row.scopeType, row.scopeID, 100, 100, 100, "monthly", true).Error)
	}

	precheck, err := svc.Precheck(context.Background(), service.ActorScope{
		UserID: managerID.String(), Role: "admin", IsGlobal: true,
	}, service.PrecheckInput{
		ScopeType: model.BudgetScopeStudio, ScopeID: studioID, PeriodType: "monthly",
		DimensionType: "total", DurationType: "permanent", TargetAmountUSD: "120.000000",
	})
	require.NoError(t, err)
	expiresAt := time.Now().UTC().Add(time.Hour)
	require.NoError(t, db.Create(&model.ExpansionRequest{
		ID: requestID, ScopeType: model.BudgetScopeStudio, ScopeID: studioID,
		RequesterID: requesterID, PeriodType: "monthly", DimensionType: budgetdomain.DimensionTotal,
		DimensionKey: "total", DurationType: budgetdomain.DurationPermanent,
		RequestedAmount: 120, PrecheckVersion: precheck.PrecheckVersion,
		PrecheckedAt: &precheck.DataAsOf, ExpiresAt: &expiresAt, Reason: "需要平台预算",
		Status: model.ExpansionStatusPending, CurrentLevel: model.ExpansionLevelStudio,
	}).Error)

	// 模拟该用户已收到旧实现创建的工作室待办。若去重键未包含审批层级，平台待办会被冲掉。
	require.NoError(t, db.Create(&model.NotificationOutbox{
		ID:        uuid.New(),
		DedupeKey: "expansion:" + requestID.String() + ":" + model.NotificationTypeExpansionSubmitted + ":" + managerID.String(),
		UserID:    managerID, Type: model.NotificationTypeExpansionSubmitted, Level: model.NotificationLevelInfo,
		Title: "新的扩容审批申请", Content: "工作室待办", RefType: "expansion_request", RefID: requestID.String(),
		Status: "pending", NextAttempt: time.Now().UTC(),
	}).Error)

	_, err = svc.DecideWorkflow(context.Background(), requestID.String(), managerID, service.DecisionInput{
		Action: model.ExpansionActionEscalate, Comment: "需要平台审批", PrecheckVersion: precheck.PrecheckVersion,
	})
	require.NoError(t, err)

	var request model.ExpansionRequest
	require.NoError(t, db.First(&request, "id = ?", requestID).Error)
	require.Equal(t, model.ExpansionLevelPlatform, request.CurrentLevel)

	var count int64
	require.NoError(t, db.Model(&model.NotificationOutbox{}).
		Where("user_id = ? AND title = ?", managerID, "新的平台级扩容审批申请").
		Count(&count).Error)
	require.Equal(t, int64(1), count)
}
