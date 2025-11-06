package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// setupProviderRouter 构建渠道管理测试路由
func setupProviderRouter(db *gorm.DB) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	providerHandler := handler.NewProviderHandler(providerSvc)

	api := r.Group("/api/v1")
	api.POST("/providers", providerHandler.CreateProvider)
	api.GET("/providers", providerHandler.ListProviders)
	api.GET("/providers/:id", providerHandler.GetProvider)
	api.PUT("/providers/:id", providerHandler.UpdateProvider)
	api.PATCH("/providers/:id/status", providerHandler.ToggleProviderStatus)
	api.DELETE("/providers/:id", providerHandler.DeleteProvider)

	return r
}

func TestCreateProvider_Success(t *testing.T) {
	// 设置加密密钥
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	router := setupProviderRouter(db)

	body := map[string]interface{}{
		"name":        "test-dashscope",
		"definition_id": "dashscope",
		"endpoint":    "https://dashscope.aliyuncs.com",
		"credentials": "sk-test-key-12345",
	}
	jsonBody, _ := json.Marshal(body)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/providers", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	if data["name"] != "test-dashscope" {
		t.Errorf("expected name 'test-dashscope', got %v", data["name"])
	}
}

func TestCreateProvider_DuplicateName(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	router := setupProviderRouter(db)

	body := map[string]interface{}{
		"name":        "duplicate-provider",
		"definition_id": "deepseek",
		"endpoint":    "https://api.deepseek.com",
		"credentials": "sk-dup-key",
	}
	jsonBody, _ := json.Marshal(body)

	// 第一次创建
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/providers", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first create failed: %d", w.Code)
	}

	// 第二次创建同名
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/v1/providers", bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestToggleProviderStatus_Cascade(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)

	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建渠道
	provider, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:        "cascade-test",
		DefinitionID: "dashscope",
		Endpoint:    "https://test.com",
		Credentials: `{"api_key": "sk-test"}`,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// 在渠道下注册模型
	m := &model.Model{
		Name:       "qwen-max",
		ProviderID: provider.ID,
		ModelType:  "chat",
		Enabled:    boolPtr(true),
	}
	if err := modelRepo.Create(m); err != nil {
		t.Fatalf("create model: %v", err)
	}

	// 禁用渠道
	_, err = providerSvc.ToggleProviderStatus(provider.ID, false)
	if err != nil {
		t.Fatalf("toggle status: %v", err)
	}

	// 验证模型也被禁用
	updatedModel, _ := modelRepo.GetByID(m.ID)
	if updatedModel.Enabled != nil && *updatedModel.Enabled {
		t.Error("expected model to be disabled after provider disabled")
	}
}

func TestDeleteProvider_BlockedByModels(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)

	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建渠道
	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:        "blocked-delete",
		DefinitionID: "deepseek",
		Endpoint:    "https://api.deepseek.com",
		Credentials: `{"api_key": "sk-block"}`,
	})

	// 注册模型
	m := &model.Model{
		Name:       "deepseek-chat",
		ProviderID: provider.ID,
		ModelType:  "chat",
		Enabled:    boolPtr(true),
	}
	modelRepo.Create(m)

	// 尝试删除渠道
	err := providerSvc.DeleteProvider(provider.ID)
	if err == nil {
		t.Error("expected error when deleting provider with models")
	}
	if err != service.ErrProviderHasModels {
		t.Errorf("expected ErrProviderHasModels, got %v", err)
	}
}
