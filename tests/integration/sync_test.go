package integration

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
)

func TestSyncModels_NewModels(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, nil)

	// 创建一个百炼渠道（使用真实 Key）
	provider, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "sync-test-dashscope",
		DefinitionID: "dashscope",
		Endpoint:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Credentials:  `{"api_key": "sk-REDACTED"}`,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// 执行同步
	result, err := syncSvc.SyncModels(provider.ID)
	if err != nil {
		t.Fatalf("sync models: %v", err)
	}

	// 验证结果
	if result.Unsupported {
		t.Fatal("expected supported, got unsupported")
	}
	if result.TotalUpstream == 0 {
		t.Fatal("expected upstream models > 0")
	}
	if len(result.Added) == 0 {
		t.Fatal("expected added models > 0")
	}
	if len(result.Existing) != 0 {
		t.Errorf("expected 0 existing, got %d", len(result.Existing))
	}

	t.Logf("同步结果: 上游=%d, 新增=%d, 已存在=%d, 已下架=%d",
		result.TotalUpstream, len(result.Added), len(result.Existing), len(result.Removed))
}

func TestSyncModels_UnsupportedProvider(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, nil)

	// 创建 Anthropic 渠道（不支持 /models 接口）
	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "sync-test-anthropic",
		DefinitionID: "anthropic",
		Endpoint:     "https://api.anthropic.com/v1",
		Credentials:  `{"api_key": "sk-ant-test"}`,
	})

	result, err := syncSvc.SyncModels(provider.ID)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !result.Unsupported {
		t.Fatal("expected unsupported=true for anthropic")
	}
}

func TestSyncModels_DisabledProvider(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "sync-test-disabled",
		DefinitionID: "dashscope",
		Endpoint:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	// 禁用渠道
	providerSvc.ToggleProviderStatus(provider.ID, false)

	// 尝试同步
	_, err := syncSvc.SyncModels(provider.ID)
	if err == nil {
		t.Fatal("expected error for disabled provider")
	}
	if err.Error() != "provider is disabled" {
		t.Errorf("expected 'provider is disabled', got '%s'", err.Error())
	}
}

func TestConfirmSync_BatchRegister(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "confirm-test",
		DefinitionID: "dashscope",
		Endpoint:     "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	// 批量注册
	result, err := syncSvc.ConfirmSync(provider.ID, service.ConfirmSyncInput{
		ModelsToAdd: []string{"qwen-max", "qwen-plus", "qwen-turbo"},
	})
	if err != nil {
		t.Fatalf("confirm sync: %v", err)
	}

	if result.Registered != 3 {
		t.Errorf("expected 3 registered, got %d", result.Registered)
	}
	if result.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", result.Skipped)
	}

	// 验证模型已创建且默认禁用
	models, total, _ := modelRepo.List(&provider.ID, 0, 100)
	if total != 3 {
		t.Errorf("expected 3 models in DB, got %d", total)
	}
	for _, m := range models {
		if m.Enabled != nil && *m.Enabled {
			t.Errorf("expected model %s to be disabled", m.Name)
		}
	}
}

func TestConfirmSync_SkipDuplicates(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	syncSvc := service.NewSyncService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "dup-test",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	// 先注册一个模型
	enabled := false
	modelRepo.Create(&model.Model{
		Name:       "qwen-max",
		ProviderID: provider.ID,
		ModelType:  "chat",
		Enabled:    &enabled,
	})

	// 尝试同步同名模型
	result, err := syncSvc.ConfirmSync(provider.ID, service.ConfirmSyncInput{
		ModelsToAdd: []string{"qwen-max", "qwen-plus"},
	})
	if err != nil {
		t.Fatalf("confirm sync: %v", err)
	}

	if result.Registered != 1 {
		t.Errorf("expected 1 registered, got %d", result.Registered)
	}
	if result.Skipped != 1 {
		t.Errorf("expected 1 skipped (duplicate), got %d", result.Skipped)
	}
}
