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
