package authz_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/gateway/authz"
	"github.com/example/llm-gateway/internal/gateway/binding"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newKeyGroupsServer 模拟控制面 /internal/v1/keys/:id/groups，只对 keyID 返回 groups。
func newKeyGroupsServer(t *testing.T, keyID string, groups []binding.BoundGroup) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/keys/"+keyID+"/groups" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(groups)
	}))
}

func TestResolveVisibleGroupNames_AccessAllWithinUsableGroups(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:           "k1",
		KeyType:         "service",
		AccessAllGroups: true,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo"},
	}

	names, err := authz.ResolveVisibleGroupNames(context.Background(), id, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"deepseek-v3", "qwen-turbo"}, names)
}

func TestResolveVisibleGroupNames_BoundSubsetWithinUsableGroups(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:           "k1",
		KeyType:         "service",
		AccessAllGroups: false,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo", "claude-opus"},
	}

	names, err := authz.ResolveVisibleGroupNames(context.Background(), id, nil)
	require.NoError(t, err)
	assert.Nil(t, names)
}

func TestAuthorizeGroupAccess_ServiceKeyInsideCeiling(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:        "svc-1",
		KeyType:      "service",
		UsableGroups: []string{"deepseek-v3"},
	}

	err := authz.AuthorizeGroupAccess(context.Background(), id, nil, "deepseek-v3")
	assert.NoError(t, err)
}

func TestAuthorizeGroupAccess_ServiceKeyOutsideCeiling(t *testing.T) {
	id := &gwmodel.UserIdentity{
		KeyID:        "svc-1",
		KeyType:      "service",
		UsableGroups: []string{"deepseek-v3"},
	}

	err := authz.AuthorizeGroupAccess(context.Background(), id, nil, "claude-opus")
	assert.ErrorIs(t, err, gwmodel.ErrModelGroupUnauthorized)
}

// TestAuthorizeGroupAccess_ServiceKeyBoundSubsetEnforced 回归测试：
// access_all_groups=false 的 Service Key 必须同时受「部门天花板 + 显式绑定子集」双层限制。
// 修复前该分支只看天花板，绑定子集外的组会被放行（越权调用无法拦截）。
func TestAuthorizeGroupAccess_ServiceKeyBoundSubsetEnforced(t *testing.T) {
	ts := newKeyGroupsServer(t, "svc-bound", []binding.BoundGroup{
		{ID: 1, Name: "deepseek-v3", Enabled: true},
	})
	defer ts.Close()
	client := binding.NewClient(ts.URL, "test-token")

	id := &gwmodel.UserIdentity{
		KeyID:           "svc-bound",
		KeyType:         "service",
		AccessAllGroups: false,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo"},
	}

	// 已绑定：放行
	require.NoError(t, authz.AuthorizeGroupAccess(context.Background(), id, client, "deepseek-v3"))
	// 天花板内但未绑定：403 拦截
	require.ErrorIs(t,
		authz.AuthorizeGroupAccess(context.Background(), id, client, "qwen-turbo"),
		gwmodel.ErrModelGroupUnauthorized)
	// 天花板外：403 拦截
	require.ErrorIs(t,
		authz.AuthorizeGroupAccess(context.Background(), id, client, "claude-opus"),
		gwmodel.ErrModelGroupUnauthorized)
}

// TestAuthorizeGroupAccess_ServiceKeyNoBindingsDenied 未绑定任何模型组且未开启
// access_all_groups 的 Service Key 应无任何可调用模型（UI 文案：不选则 Key 暂无可调用模型）。
func TestAuthorizeGroupAccess_ServiceKeyNoBindingsDenied(t *testing.T) {
	ts := newKeyGroupsServer(t, "svc-empty", nil)
	defer ts.Close()
	client := binding.NewClient(ts.URL, "test-token")

	id := &gwmodel.UserIdentity{
		KeyID:           "svc-empty",
		KeyType:         "service",
		AccessAllGroups: false,
		UsableGroups:    []string{"deepseek-v3"},
	}

	require.ErrorIs(t,
		authz.AuthorizeGroupAccess(context.Background(), id, client, "deepseek-v3"),
		gwmodel.ErrModelGroupUnauthorized)
}

// TestAuthorizeGroupAccess_ServiceKeyAccessAllSkipsBinding access_all_groups=true 时
// 只受天花板约束，不应查询绑定关系。
func TestAuthorizeGroupAccess_ServiceKeyAccessAllSkipsBinding(t *testing.T) {
	ts := newKeyGroupsServer(t, "svc-all", []binding.BoundGroup{
		{ID: 1, Name: "deepseek-v3", Enabled: true},
	})
	defer ts.Close()
	client := binding.NewClient(ts.URL, "test-token")

	id := &gwmodel.UserIdentity{
		KeyID:           "svc-all",
		KeyType:         "service",
		AccessAllGroups: true,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo"},
	}

	require.NoError(t, authz.AuthorizeGroupAccess(context.Background(), id, client, "qwen-turbo"))
	require.ErrorIs(t,
		authz.AuthorizeGroupAccess(context.Background(), id, client, "claude-opus"),
		gwmodel.ErrModelGroupUnauthorized)
}
