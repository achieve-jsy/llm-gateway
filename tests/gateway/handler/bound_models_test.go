package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/core"
	"github.com/example/llm-gateway/internal/gateway/handler"
	"github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBoundModelsHandler_ServiceKeyUsableGroups 验证 Service Key 在 verify-key 下发
// access_all_groups=true + usable_groups 时，/v1/models 应返回部门可用模型组（无需 key_group_bindings）。
func TestBoundModelsHandler_ServiceKeyUsableGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := handler.NewBoundModelsHandler("", "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set("identity", &model.UserIdentity{
		KeyID:           "service-key-id",
		KeyType:         "service",
		AccessAllGroups: true,
		UsableGroups:    []string{"deepseek-v3", "qwen-turbo"},
	})

	h.Handle(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp core.ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "deepseek-v3", resp.Data[0].ID)
	assert.Equal(t, "qwen-turbo", resp.Data[1].ID)
}

// TestBoundModelsHandler_MissingIdentity 验证上下文中没有 identity 时返回 200 + 空列表。
func TestBoundModelsHandler_MissingIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := handler.NewBoundModelsHandler("", "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	// 不设置 identity

	h.Handle(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp core.ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Empty(t, resp.Data)
}

// TestBoundModelsHandler_InvalidIdentityType 验证上下文中 identity 类型错误时返回 200 + 空列表。
func TestBoundModelsHandler_InvalidIdentityType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := handler.NewBoundModelsHandler("", "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set("identity", "not-a-user-identity") // 错误的类型

	h.Handle(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp core.ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Empty(t, resp.Data)
}

// TestBoundModelsHandler_EmptyGroups 验证 Service Key 在 usable_groups 为空时返回空列表。
func TestBoundModelsHandler_EmptyGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := handler.NewBoundModelsHandler("", "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set("identity", &model.UserIdentity{
		KeyID:        "service-key-id",
		KeyType:      "service",
		UsableGroups: []string{}, // 空的 usable_groups
	})

	h.Handle(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp core.ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	assert.Empty(t, resp.Data)
}

// TestBoundModelsHandler_EmptyGroupName 验证 usable_groups 中包含空字符串的组名会被过滤掉。
func TestBoundModelsHandler_EmptyGroupName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := handler.NewBoundModelsHandler("", "")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	c.Set("identity", &model.UserIdentity{
		KeyID:           "service-key-id",
		KeyType:         "service",
		AccessAllGroups: true,
		UsableGroups:    []string{"deepseek-v3", "", "qwen-turbo"}, // 包含空字符串
	})

	h.Handle(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp core.ModelsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 2)
	assert.Equal(t, "deepseek-v3", resp.Data[0].ID)
	assert.Equal(t, "qwen-turbo", resp.Data[1].ID)
}
