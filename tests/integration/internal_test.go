package integration

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
)

func TestListAvailableModels_OnlyEnabled(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	internalSvc := service.NewInternalService(modelRepo, providerRepo)

	// 创建一个启用渠道和一个禁用渠道
	enabledProvider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name: "enabled-provider", DefinitionID: "dashscope", Endpoint: "https://enabled.com", Credentials: `{"api_key": "sk-enabled"}`,
	})
	disabledProvider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name: "disabled-provider", DefinitionID: "deepseek", Endpoint: "https://disabled.com", Credentials: `{"api_key": "sk-disabled"}`,
	})
	providerSvc.ToggleProviderStatus(disabledProvider.ID, false)

	// 注册模型
	modelRepo.Create(&model.Model{Name: "available-model", ProviderID: enabledProvider.ID, ModelType: "chat", Enabled: boolPtr(true)})
	modelRepo.Create(&model.Model{Name: "disabled-model", ProviderID: disabledProvider.ID, ModelType: "chat", Enabled: boolPtr(true)})
	modelRepo.Create(&model.Model{Name: "self-disabled-model", ProviderID: enabledProvider.ID, ModelType: "chat", Enabled: boolPtr(false)})

	// 查询可用模型
	available, err := internalSvc.ListAvailableModels()
	if err != nil {
		t.Fatalf("list available: %v", err)
	}

	// 只有 available-model 应该返回（渠道启用 + 模型启用）
	if len(available) != 1 {
		t.Errorf("expected 1 available model, got %d", len(available))
		for _, m := range available {
			t.Logf("  - %s (provider: %s, enabled: provider=%v)", m.Name, m.ProviderName, true)
		}
		return
	}

	if available[0].Name != "available-model" {
		t.Errorf("expected 'available-model', got %s", available[0].Name)
	}
	if available[0].ProviderEndpoint != "https://enabled.com" {
		t.Errorf("expected endpoint 'https://enabled.com', got %s", available[0].ProviderEndpoint)
	}
	if available[0].ProviderCredentials == "" {
		t.Error("expected decrypted credentials, got empty")
	}
}
