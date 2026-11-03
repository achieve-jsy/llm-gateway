package router_test

import (
	"net/http"
	"testing"

	orgmodel "github.com/example/llm-gateway/internal/orgservice/model"
	"github.com/stretchr/testify/require"
)

// 组织工作台的契约路径必须在**真实 router.Setup 装配**后命中：
//
//	无 token           → 401（JWTAuth 生效，说明路由存在）
//	有 token 但无角色  → 403（guard(PermRoleList/PermMemberView) 生效）
//	404 就说明路径拼错了 —— 这是本用例唯一的目的（handler 行为由 tests/controlplane/handler 覆盖）。
func TestOrgWorkspaceRoutesRegistered(t *testing.T) {
	db := assemblyDB(t)
	engine := assemblyEngine(t, db)
	assemblySeedRole(t, db, assemblyPlainUserID, orgmodel.RoleUser)

	for _, path := range []string{
		"/api/v1/org/accounts",
		"/api/v1/org/studios/00000000-0000-0000-0000-000000000000/overview",
	} {
		code, body := assemblyGet(engine, path, "", nil)
		require.Equal(t, http.StatusUnauthorized, code, "%s 无 token 应 401，实际 %d: %s", path, code, body)

		code, body = assemblyGet(engine, path, assemblyJWT(t, assemblyPlainUserID), nil)
		require.Equal(t, http.StatusForbidden, code, "%s 无管理角色应 403，实际 %d: %s", path, code, body)
	}

	// 负向对照：未注册的路径必须是 404，证明上面两条不是"全都 401"。
	code, _ := assemblyGet(engine, "/api/v1/org/not-registered", "", nil)
	require.Equal(t, http.StatusNotFound, code)
}
