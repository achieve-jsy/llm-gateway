package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	controlrepo "github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/example/llm-gateway/internal/orgservice/rbac"
	orgrepo "github.com/example/llm-gateway/internal/orgservice/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type keyHandlerServiceStub struct {
	handler.KeyServiceInterface
	createCalled bool
}

func (s *keyHandlerServiceStub) CreateKey(
	context.Context,
	uuid.UUID,
	string,
	*time.Time,
	[]uint,
) (*model.ApiKey, string, []service.BoundGroupItem, error) {
	s.createCalled = true
	return &model.ApiKey{}, "plaintext", nil, nil
}

type keyPolicyStub struct {
	requiresApproval bool
	deptID           string
}

func (s *keyPolicyStub) RequiresApproval(_ context.Context, deptID string) (bool, error) {
	s.deptID = deptID
	return s.requiresApproval, nil
}

func TestKeyHandlerCreateKeyRequiresApprovalForOrdinaryDepartmentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE user_roles (id integer primary key autoincrement, user_id text, role text, scope_type text, scope_id text, granted_by text, actor_id text, created_at datetime)`,
		`CREATE TABLE users (id text primary key, role text)`,
		`CREATE TABLE dev_users (id text primary key, role text)`,
		`CREATE TABLE departments (id text primary key, dept_id text, studio_id text)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}

	userID := uuid.New()
	keySvc := &keyHandlerServiceStub{}
	policy := &keyPolicyStub{requiresApproval: true}
	keyHandler := handler.NewKeyHandler(
		keySvc,
		policy,
		rbac.NewChecker(orgrepo.NewRoleRepo(db), orgrepo.NewUserOrgRepo(db)),
	)
	router := gin.New()
	router.POST("/keys", func(c *gin.Context) {
		c.Set("user_id", userID.String())
		c.Set("dept_id", "dept-a")
		keyHandler.CreateKey(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/keys", strings.NewReader(`{"name":"blocked"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "dept-a", policy.deptID)
	require.False(t, keySvc.createCalled)

	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, "key_creation_requires_approval", response.Error.Code)
}

func TestKeyRequestHandlerGetDeptPolicyRejectsStudioOnlyAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, ddl := range []string{
		`CREATE TABLE user_roles (id integer primary key autoincrement, user_id text, role text, scope_type text, scope_id text, granted_by text, actor_id text, created_at datetime)`,
		`CREATE TABLE users (id text primary key, role text)`,
		`CREATE TABLE dev_users (id text primary key, role text)`,
		`CREATE TABLE departments (id text primary key, dept_id text, studio_id text)`,
		`CREATE TABLE dept_policies (id text primary key, dept_id text, policy_key text, policy_value text, description text, updated_by text, created_at datetime, updated_at datetime)`,
	} {
		require.NoError(t, db.Exec(ddl).Error)
	}

	managerID := uuid.New()
	require.NoError(t, db.Exec(`INSERT INTO departments (id, dept_id, studio_id) VALUES ('dept-row', 'dept-a', 'studio-a')`).Error)
	require.NoError(t, db.Exec(
		`INSERT INTO user_roles (user_id, role, scope_type, scope_id, granted_by) VALUES (?, 'studio_admin', 'studio', 'studio-a', 'admin')`,
		managerID.String(),
	).Error)

	keyRequestService := service.NewKeyRequestService(
		controlrepo.NewKeyRequestRepo(db),
		controlrepo.NewDeptPolicyRepo(db),
		nil,
	)
	keyRequestHandler := handler.NewKeyRequestHandler(
		keyRequestService,
		rbac.NewChecker(orgrepo.NewRoleRepo(db), orgrepo.NewUserOrgRepo(db)),
		nil,
	)
	router := gin.New()
	router.GET("/dept-policies/:dept_id/key-creation", func(c *gin.Context) {
		c.Set("user_id", managerID.String())
		keyRequestHandler.GetDeptPolicy(c)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dept-policies/dept-a/key-creation", nil))

	require.Equal(t, http.StatusForbidden, w.Code)
}
