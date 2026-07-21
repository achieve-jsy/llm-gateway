//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"

	"github.com/example/llm-gateway/internal/configsync"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestMultiRoleApprovalE2E 验证同一负责人同时拥有部门与工作室角色时，
// 能合并处理两类 Key 待审和不同审批层级的预算扩容，同时不会越权处理其他部门成员的成员级扩容。
func TestMultiRoleApprovalE2E(t *testing.T) {
	fixture := newMultiRoleE2EFixture(t)

	memberAToken := loginDevUser(t, fixture.memberAUsername, fixture.password)
	memberBToken := loginDevUser(t, fixture.memberBUsername, fixture.password)
	managerToken := loginDevUser(t, fixture.managerUsername, fixture.password)
	deptBOwnerToken := loginDevUser(t, fixture.deptBOwnerUsername, fixture.password)

	// 两个部门成员分别发起 Key 创建申请。双角色负责人应在一个待审列表中看到两条。
	keyRequestA := createKeyRequest(t, memberAToken, fixture.runID+"-key-a")
	keyRequestB := createKeyRequest(t, memberBToken, fixture.runID+"-key-b")
	pendingKeys := doRequest(t, http.MethodGet, cpURL()+"/api/v1/key-requests/pending?page_size=100", authHeader(managerToken), nil)
	require.Equal(t, http.StatusOK, pendingKeys.Status, pendingKeys.Raw)
	pendingKeyIDs := itemIDs(t, pendingKeys.Body["items"])
	require.Contains(t, pendingKeyIDs, keyRequestA)
	require.Contains(t, pendingKeyIDs, keyRequestB)

	approveKeyRequest(t, managerToken, keyRequestA)
	approveKeyRequest(t, managerToken, keyRequestB)
	assertKeyRequestApproval(t, fixture, keyRequestA, fixture.memberAID)
	assertKeyRequestApproval(t, fixture, keyRequestB, fixture.memberBID)

	// 成员 A 的个人额度申请应由 manager 的 dept_admin 身份处理。
	memberAExpansion := createExpansionRequest(t, memberAToken, map[string]any{
		"scope_type":        "member",
		"scope_id":          fixture.memberAID.String(),
		"period_type":       "monthly",
		"dimension_type":    "total",
		"duration_type":     "permanent",
		"priority":          "normal",
		"target_amount_usd": "20",
		"reason":            "multi-role e2e member-a",
	})
	require.Equal(t, "dept", memberAExpansion.currentLevel)

	pendingExpansionA := doRequest(t, http.MethodGet, cpURL()+"/api/v1/expansion-requests/pending", authHeader(managerToken), nil)
	require.Equal(t, http.StatusOK, pendingExpansionA.Status, pendingExpansionA.Raw)
	require.Contains(t, itemIDs(t, pendingExpansionA.Body["items"]), memberAExpansion.id)

	revisionBeforeApprovals := readSnapshotRevision(t, fixture.rdb)
	approveExpansionRequest(t, managerToken, memberAExpansion)
	assertExpansionState(t, fixture, memberAExpansion.id, "approved", "synced", "dept", 20)
	assertExpansionStep(t, fixture, memberAExpansion.id, "dept", fixture.managerID)

	// 成员 B 与 manager 同属一个工作室，但 manager 并非该部门负责人。
	// 因此成员级申请既不应出现在 manager 的待审列表，也不能被直接审批。
	memberBExpansion := createExpansionRequest(t, memberBToken, map[string]any{
		"scope_type":        "member",
		"scope_id":          fixture.memberBID.String(),
		"period_type":       "monthly",
		"dimension_type":    "total",
		"duration_type":     "permanent",
		"priority":          "normal",
		"target_amount_usd": "20",
		"reason":            "multi-role e2e member-b",
	})
	pendingExpansionB := doRequest(t, http.MethodGet, cpURL()+"/api/v1/expansion-requests/pending", authHeader(managerToken), nil)
	require.Equal(t, http.StatusOK, pendingExpansionB.Status, pendingExpansionB.Raw)
	require.NotContains(t, itemIDs(t, pendingExpansionB.Body["items"]), memberBExpansion.id)

	forbiddenDecision := doRequest(t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/expansion-requests/%s/decision", cpURL(), memberBExpansion.id),
		authHeader(managerToken), map[string]any{
			"action":           "approve",
			"comment":          "must not approve another department member",
			"precheck_version": memberBExpansion.precheckVersion,
		})
	require.Equal(t, http.StatusForbidden, forbiddenDecision.Status, forbiddenDecision.Raw)
	require.Equal(t, "EXPANSION_FORBIDDEN", forbiddenDecision.Body["code"])
	assertExpansionState(t, fixture, memberBExpansion.id, "pending", "pending", "dept", 10)
	assertNoExpansionApprovalBy(t, fixture, memberBExpansion.id, fixture.managerID)

	// 部门 B 负责人申请提升部门预算，当前审批层级应为 studio，manager 的 studio_admin 身份可以处理。
	deptBExpansion := createExpansionRequest(t, deptBOwnerToken, map[string]any{
		"scope_type":        "dept_daily",
		"scope_id":          fixture.deptBSSOID,
		"period_type":       "monthly",
		"dimension_type":    "total",
		"duration_type":     "permanent",
		"priority":          "normal",
		"target_amount_usd": "200",
		"reason":            "multi-role e2e dept-b",
	})
	require.Equal(t, "studio", deptBExpansion.currentLevel)

	pendingDeptB := doRequest(t, http.MethodGet, cpURL()+"/api/v1/expansion-requests/pending", authHeader(managerToken), nil)
	require.Equal(t, http.StatusOK, pendingDeptB.Status, pendingDeptB.Raw)
	require.Contains(t, itemIDs(t, pendingDeptB.Body["items"]), deptBExpansion.id)

	approveExpansionRequest(t, managerToken, deptBExpansion)
	assertExpansionState(t, fixture, deptBExpansion.id, "approved", "synced", "studio", 200)
	assertExpansionStep(t, fixture, deptBExpansion.id, "studio", fixture.managerID)

	// 两次成功审批都应更新预算快照，且快照可由网关侧同一读取接口加载。
	finalSnapshot, err := configsync.LoadBudgetTierSnapshot(context.Background(), fixture.rdb)
	require.NoError(t, err)
	require.GreaterOrEqual(t, finalSnapshot.Revision, revisionBeforeApprovals+2)
}

type multiRoleE2EFixture struct {
	db  *gorm.DB
	rdb *redis.Client

	runID    string
	password string

	studioID     uuid.UUID
	deptAID      uuid.UUID
	deptBID      uuid.UUID
	deptASSOID   string
	deptBSSOID   string
	managerID    uuid.UUID
	memberAID    uuid.UUID
	memberBID    uuid.UUID
	deptBOwnerID uuid.UUID

	managerUsername    string
	memberAUsername    string
	memberBUsername    string
	deptBOwnerUsername string

	allocationIDs       []uuid.UUID
	createdGlobalBudget *uuid.UUID
}

func newMultiRoleE2EFixture(t *testing.T) *multiRoleE2EFixture {
	t.Helper()
	databaseURL := os.Getenv("E2E_DATABASE_URL")
	require.NotEmpty(t, databaseURL, "E2E_DATABASE_URL is required for the multi-role E2E test")

	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Ping())

	redisDB := 15
	if raw := os.Getenv("E2E_REDIS_DB"); raw != "" {
		redisDB, err = strconv.Atoi(raw)
		require.NoError(t, err)
	}
	redisAddr := os.Getenv("E2E_REDIS_ADDR")
	require.NotEmpty(t, redisAddr, "E2E_REDIS_ADDR is required for the multi-role E2E test")
	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("E2E_REDIS_PASSWORD"),
		DB:       redisDB,
	})
	require.NoError(t, rdb.Ping(context.Background()).Err())

	runID := uuid.NewString()
	fixture := &multiRoleE2EFixture{
		db:           db,
		rdb:          rdb,
		runID:        runID,
		password:     "e2e-approval-password",
		studioID:     uuid.New(),
		deptAID:      uuid.New(),
		deptBID:      uuid.New(),
		deptASSOID:   "e2e-dept-a-" + runID,
		deptBSSOID:   "e2e-dept-b-" + runID,
		managerID:    uuid.New(),
		memberAID:    uuid.New(),
		memberBID:    uuid.New(),
		deptBOwnerID: uuid.New(),

		managerUsername:    "e2e-manager-" + runID,
		memberAUsername:    "e2e-member-a-" + runID,
		memberBUsername:    "e2e-member-b-" + runID,
		deptBOwnerUsername: "e2e-dept-b-owner-" + runID,
	}
	t.Cleanup(func() {
		fixture.cleanup(t)
		_ = rdb.Close()
		_ = sqlDB.Close()
	})

	mustExec(t, db, `INSERT INTO studios (id, name, owner_id) VALUES (?, ?, ?)`, fixture.studioID, "E2E Studio "+runID, fixture.managerID.String())
	mustExec(t, db, `
		INSERT INTO departments (id, dept_id, dept_name, studio_id, status, owner_id)
		VALUES (?, ?, ?, ?, 'active', ?), (?, ?, ?, ?, 'active', ?)
	`,
		fixture.deptAID, fixture.deptASSOID, "E2E Dept A "+runID, fixture.studioID, fixture.managerID.String(),
		fixture.deptBID, fixture.deptBSSOID, "E2E Dept B "+runID, fixture.studioID, fixture.deptBOwnerID.String(),
	)
	mustExec(t, db, `
		INSERT INTO dept_studio_map (dept_id, studio_id, source, updated_by)
		VALUES (?, ?, 'manual', ?), (?, ?, 'manual', ?)
	`, fixture.deptASSOID, fixture.studioID, fixture.managerID.String(), fixture.deptBSSOID, fixture.studioID, fixture.managerID.String())

	fixture.seedUser(t, fixture.managerID, fixture.managerUsername, fixture.deptAID, "E2E Manager "+runID)
	fixture.seedUser(t, fixture.memberAID, fixture.memberAUsername, fixture.deptAID, "E2E Member A "+runID)
	fixture.seedUser(t, fixture.memberBID, fixture.memberBUsername, fixture.deptBID, "E2E Member B "+runID)
	fixture.seedUser(t, fixture.deptBOwnerID, fixture.deptBOwnerUsername, fixture.deptBID, "E2E Dept B Owner "+runID)

	mustExec(t, db, `
		INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by)
		VALUES (?, 'dept_admin', 'dept', ?, ?), (?, 'studio_admin', 'studio', ?, ?), (?, 'dept_admin', 'dept', ?, ?)
	`,
		fixture.managerID.String(), fixture.deptASSOID, fixture.managerID.String(),
		fixture.managerID.String(), fixture.studioID.String(), fixture.managerID.String(),
		fixture.deptBOwnerID.String(), fixture.deptBSSOID, fixture.managerID.String(),
	)

	fixture.seedBudget(t, "dept_daily", fixture.deptASSOID, 1000)
	fixture.seedBudget(t, "dept_daily", fixture.deptBSSOID, 100)
	fixture.seedBudget(t, "studio", fixture.studioID.String(), 5000)
	fixture.seedBudget(t, "member", fixture.memberAID.String(), 10)
	fixture.seedBudget(t, "member", fixture.memberBID.String(), 10)
	fixture.ensureCompanyBudget(t)

	return fixture
}

func (f *multiRoleE2EFixture) seedUser(t *testing.T, id uuid.UUID, username string, deptID uuid.UUID, name string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(f.password), bcrypt.MinCost)
	require.NoError(t, err)
	mustExec(t, f.db, `
		INSERT INTO users (id, sub, dept_id, name, email, source, role, dept_source, login_status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'dev', 'user', 'sso', 'active', NOW(), NOW())
	`, id, username+"-sub", deptID, name, username+"@e2e.local")
	mustExec(t, f.db, `INSERT INTO dev_users (id, username, password_hash, role) VALUES (?, ?, ?, 'user')`, id, username, string(hash))
}

func (f *multiRoleE2EFixture) seedBudget(t *testing.T, scopeType, scopeID string, amount float64) {
	t.Helper()
	id := uuid.New()
	mustExec(t, f.db, `
		INSERT INTO budget_allocations
			(id, scope_type, scope_id, amount_usd, daily_amount_usd, weekly_amount_usd, period, enabled, created_by, updated_by)
		VALUES (?, ?, ?, ?, -1, -1, 'monthly', true, ?, ?)
	`, id, scopeType, scopeID, amount, f.managerID, f.managerID)
	f.allocationIDs = append(f.allocationIDs, id)
}

func (f *multiRoleE2EFixture) ensureCompanyBudget(t *testing.T) {
	t.Helper()
	id := uuid.New()
	result := f.db.Exec(`
		INSERT INTO budget_allocations
			(id, scope_type, scope_id, amount_usd, daily_amount_usd, weekly_amount_usd, period, enabled, created_by, updated_by)
		VALUES (?, 'company', 'GLOBAL', 99999999.99, -1, -1, 'monthly', true, ?, ?)
		ON CONFLICT (scope_type, scope_id) DO NOTHING
	`, id, f.managerID, f.managerID)
	require.NoError(t, result.Error)
	if result.RowsAffected == 1 {
		f.createdGlobalBudget = &id
	}
}

func (f *multiRoleE2EFixture) cleanup(t *testing.T) {
	t.Helper()
	userIDs := []uuid.UUID{f.managerID, f.memberAID, f.memberBID, f.deptBOwnerID}
	deptIDs := []string{f.deptASSOID, f.deptBSSOID}
	deptUUIDs := []uuid.UUID{f.deptAID, f.deptBID}

	var expansionIDs []uuid.UUID
	require.NoError(t, f.db.Table("expansion_requests").Where("requester_id IN ?", userIDs).Pluck("id", &expansionIDs).Error)
	var keyRequestIDs []uuid.UUID
	require.NoError(t, f.db.Table("key_requests").Where("requester_id IN ?", userIDs).Pluck("id", &keyRequestIDs).Error)

	refIDs := make([]string, 0, len(expansionIDs)+len(keyRequestIDs))
	for _, id := range expansionIDs {
		refIDs = append(refIDs, id.String())
	}
	for _, id := range keyRequestIDs {
		refIDs = append(refIDs, id.String())
	}
	if len(refIDs) > 0 {
		var dedupeKeys []string
		require.NoError(t, f.db.Table("notification_outbox").Where("ref_id IN ?", refIDs).Pluck("dedupe_key", &dedupeKeys).Error)
		if len(dedupeKeys) > 0 {
			require.NoError(t, f.db.Exec("DELETE FROM notification_deliveries WHERE dedupe_key IN ?", dedupeKeys).Error)
		}
		require.NoError(t, f.db.Exec("DELETE FROM notification_outbox WHERE ref_id IN ?", refIDs).Error)
		require.NoError(t, f.db.Exec("DELETE FROM notifications WHERE ref_id IN ?", refIDs).Error)
	}
	if len(expansionIDs) > 0 {
		require.NoError(t, f.db.Exec("DELETE FROM expansion_approval_steps WHERE request_id IN ?", expansionIDs).Error)
		require.NoError(t, f.db.Exec("DELETE FROM expansion_requests WHERE id IN ?", expansionIDs).Error)
	}

	var keyIDs []uuid.UUID
	require.NoError(t, f.db.Table("api_keys").Where("user_id IN ?", userIDs).Pluck("id", &keyIDs).Error)
	if len(keyIDs) > 0 {
		require.NoError(t, f.db.Exec("DELETE FROM key_group_bindings WHERE key_id IN ?", keyIDs).Error)
	}
	if len(keyRequestIDs) > 0 {
		require.NoError(t, f.db.Exec("DELETE FROM key_requests WHERE id IN ?", keyRequestIDs).Error)
	}
	if len(keyIDs) > 0 {
		require.NoError(t, f.db.Exec("DELETE FROM api_keys WHERE id IN ?", keyIDs).Error)
	}

	if len(f.allocationIDs) > 0 {
		require.NoError(t, f.db.Exec("DELETE FROM budget_limit_schedules WHERE budget_allocation_id IN ?", f.allocationIDs).Error)
		require.NoError(t, f.db.Exec("DELETE FROM budget_temporary_overrides WHERE budget_allocation_id IN ?", f.allocationIDs).Error)
		require.NoError(t, f.db.Exec("DELETE FROM budget_tier_limits WHERE budget_allocation_id IN ?", f.allocationIDs).Error)
		require.NoError(t, f.db.Exec("DELETE FROM budget_allocations WHERE id IN ?", f.allocationIDs).Error)
	}
	if f.createdGlobalBudget != nil {
		require.NoError(t, f.db.Exec("DELETE FROM budget_allocations WHERE id = ?", *f.createdGlobalBudget).Error)
	}

	require.NoError(t, f.db.Exec("DELETE FROM audit_logs WHERE user_id IN ?", userIDs).Error)
	require.NoError(t, f.db.Exec("DELETE FROM user_roles WHERE user_id IN ?", userIDs).Error)
	require.NoError(t, f.db.Exec("DELETE FROM dev_users WHERE id IN ?", userIDs).Error)
	require.NoError(t, f.db.Exec("DELETE FROM users WHERE id IN ?", userIDs).Error)
	require.NoError(t, f.db.Exec("DELETE FROM dept_studio_map WHERE dept_id IN ?", deptIDs).Error)
	require.NoError(t, f.db.Exec("DELETE FROM departments WHERE id IN ?", deptUUIDs).Error)
	require.NoError(t, f.db.Exec("DELETE FROM studios WHERE id = ?", f.studioID).Error)
}

func loginDevUser(t *testing.T, username, password string) string {
	t.Helper()
	response := doRequest(t, http.MethodPost, cpURL()+"/auth/dev/login", nil, map[string]string{
		"username": username,
		"password": password,
	})
	require.Equal(t, http.StatusOK, response.Status, response.Raw)
	token, ok := response.Body["accessToken"].(string)
	require.True(t, ok)
	require.NotEmpty(t, token)
	return token
}

func createKeyRequest(t *testing.T, token, keyName string) string {
	t.Helper()
	response := doRequest(t, http.MethodPost, cpURL()+"/api/v1/key-requests", authHeader(token), map[string]any{
		"key_name":       keyName,
		"purpose":        "multi-role e2e",
		"budget_monthly": 0,
	})
	require.Equal(t, http.StatusCreated, response.Status, response.Raw)
	return requiredString(t, nestedMap(t, response.Body["data"])["id"])
}

func approveKeyRequest(t *testing.T, token, requestID string) {
	t.Helper()
	response := doRequest(t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/key-requests/%s/decision", cpURL(), requestID),
		authHeader(token), map[string]string{"action": "approve", "comment": "multi-role e2e"})
	require.Equal(t, http.StatusOK, response.Status, response.Raw)
	require.Equal(t, true, response.Body["ok"])
}

type expansionAPIResponse struct {
	id              string
	currentLevel    string
	precheckVersion string
}

func createExpansionRequest(t *testing.T, token string, body map[string]any) expansionAPIResponse {
	t.Helper()
	response := doRequest(t, http.MethodPost, cpURL()+"/api/v1/expansion-requests", authHeader(token), body)
	require.Equal(t, http.StatusCreated, response.Status, response.Raw)
	return expansionAPIResponse{
		id:              requiredString(t, response.Body["id"]),
		currentLevel:    requiredString(t, response.Body["current_level"]),
		precheckVersion: requiredString(t, response.Body["precheck_version"]),
	}
}

func approveExpansionRequest(t *testing.T, token string, request expansionAPIResponse) {
	t.Helper()
	response := doRequest(t, http.MethodPost,
		fmt.Sprintf("%s/api/v1/expansion-requests/%s/decision", cpURL(), request.id),
		authHeader(token), map[string]string{
			"action":           "approve",
			"comment":          "multi-role e2e",
			"precheck_version": request.precheckVersion,
		})
	require.Equal(t, http.StatusOK, response.Status, response.Raw)
}

func assertKeyRequestApproval(t *testing.T, fixture *multiRoleE2EFixture, requestID string, requesterID uuid.UUID) {
	t.Helper()
	var row struct {
		Status           string
		ApproverID       string
		CreatedKeyID     string
		CreatedKeyUserID string
		AccessAllGroups  bool
	}
	require.NoError(t, fixture.db.Raw(`
		SELECT kr.status, COALESCE(kr.approver_id::text, '') AS approver_id,
		       COALESCE(kr.created_key_id::text, '') AS created_key_id,
		       COALESCE(ak.user_id::text, '') AS created_key_user_id,
		       COALESCE(ak.access_all_groups, false) AS access_all_groups
		FROM key_requests kr
		LEFT JOIN api_keys ak ON ak.id = kr.created_key_id
		WHERE kr.id = ?
	`, requestID).Scan(&row).Error)
	require.Equal(t, "approved", row.Status)
	require.Equal(t, fixture.managerID.String(), row.ApproverID)
	require.NotEmpty(t, row.CreatedKeyID)
	require.Equal(t, requesterID.String(), row.CreatedKeyUserID)
	require.True(t, row.AccessAllGroups)
}

func assertExpansionState(t *testing.T, fixture *multiRoleE2EFixture, requestID, wantStatus, wantSyncStatus, wantLevel string, wantAmount float64) {
	t.Helper()
	var row struct {
		Status       string
		SyncStatus   string
		CurrentLevel string
		AmountUSD    float64
	}
	require.NoError(t, fixture.db.Raw(`
		SELECT er.status, er.sync_status, er.current_level, ba.amount_usd::float8 AS amount_usd
		FROM expansion_requests er
		JOIN budget_allocations ba ON ba.scope_type = er.scope_type AND ba.scope_id = er.scope_id
		WHERE er.id = ?
	`, requestID).Scan(&row).Error)
	require.Equal(t, wantStatus, row.Status)
	require.Equal(t, wantSyncStatus, row.SyncStatus)
	require.Equal(t, wantLevel, row.CurrentLevel)
	require.InDelta(t, wantAmount, row.AmountUSD, 0.0001)
}

func assertExpansionStep(t *testing.T, fixture *multiRoleE2EFixture, requestID, wantLevel string, approverID uuid.UUID) {
	t.Helper()
	var row struct {
		Level      string
		Action     string
		ApproverID string
	}
	require.NoError(t, fixture.db.Raw(`
		SELECT level, action, COALESCE(approver_id::text, '') AS approver_id
		FROM expansion_approval_steps
		WHERE request_id = ?
		ORDER BY created_at ASC
		LIMIT 1
	`, requestID).Scan(&row).Error)
	require.Equal(t, wantLevel, row.Level)
	require.Equal(t, "approve", row.Action)
	require.Equal(t, approverID.String(), row.ApproverID)
}

func assertNoExpansionApprovalBy(t *testing.T, fixture *multiRoleE2EFixture, requestID string, approverID uuid.UUID) {
	t.Helper()
	var count int64
	require.NoError(t, fixture.db.Table("expansion_approval_steps").
		Where("request_id = ? AND approver_id = ?", requestID, approverID).Count(&count).Error)
	require.Zero(t, count)
}

func nestedMap(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	require.True(t, ok)
	return result
}

func itemIDs(t *testing.T, value any) map[string]struct{} {
	t.Helper()
	items, ok := value.([]any)
	require.True(t, ok)
	result := make(map[string]struct{}, len(items))
	for _, item := range items {
		entry := nestedMap(t, item)
		result[requiredString(t, entry["id"])] = struct{}{}
	}
	return result
}

func requiredString(t *testing.T, value any) string {
	t.Helper()
	result, ok := value.(string)
	require.True(t, ok)
	require.NotEmpty(t, result)
	return result
}

func readSnapshotRevision(t *testing.T, rdb *redis.Client) int64 {
	t.Helper()
	revision, err := rdb.Get(context.Background(), configsync.BudgetTierSnapshotRevisionKey).Int64()
	if err == redis.Nil {
		return 0
	}
	require.NoError(t, err)
	return revision
}

func mustExec(t *testing.T, db *gorm.DB, query string, args ...any) {
	t.Helper()
	require.NoError(t, db.Exec(query, args...).Error)
}
