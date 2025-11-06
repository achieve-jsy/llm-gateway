package integration

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
)

func TestCreateModel_Success(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	// 先创建渠道
	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:        "model-test-provider",
		DefinitionID: "dashscope",
		Endpoint:    "https://dashscope.aliyuncs.com",
		Credentials: `{"api_key": "sk-model-test"}`,
	})

	// 注册模型
	m, err := modelSvc.CreateModel(service.CreateModelInput{
		Name:       "qwen-turbo",
		ProviderID: provider.ID.String(),
		ModelType:  "chat",
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	if m.Name != "qwen-turbo" {
		t.Errorf("expected name 'qwen-turbo', got %s", m.Name)
	}
	if m.ProviderID != provider.ID {
		t.Errorf("expected provider_id %s, got %s", provider.ID, m.ProviderID)
	}
}

func TestCreateModel_DuplicateName(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:        "dup-model-provider",
		DefinitionID: "deepseek",
		Endpoint:    "https://api.deepseek.com",
		Credentials: `{"api_key": "sk-dup"}`,
	})

	// 第一次注册
	_, err := modelSvc.CreateModel(service.CreateModelInput{
		Name:       "deepseek-chat",
		ProviderID: provider.ID.String(),
		ModelType:  "chat",
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// 第二次同名注册
	_, err = modelSvc.CreateModel(service.CreateModelInput{
		Name:       "deepseek-chat",
		ProviderID: provider.ID.String(),
		ModelType:  "chat",
	})
	if err != service.ErrModelNameExists {
		t.Errorf("expected ErrModelNameExists, got %v", err)
	}
}

func TestCreateModel_DisabledProvider(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	// 创建并禁用渠道
	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:        "disabled-provider",
		DefinitionID: "bedrock",
		Endpoint:    "https://bedrock.aws.com",
		Credentials: `{"api_key": "sk-disabled"}`,
	})
	providerSvc.ToggleProviderStatus(provider.ID, false)

	// 尝试在禁用渠道下注册模型
	_, err := modelSvc.CreateModel(service.CreateModelInput{
		Name:       "claude-sonnet",
		ProviderID: provider.ID.String(),
		ModelType:  "chat",
	})
	if err != service.ErrModelProviderInvalid {
		t.Errorf("expected ErrModelProviderInvalid, got %v", err)
	}
}

func TestListModels_FilterByProvider(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建两个渠道
	p1, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name: "provider-a", DefinitionID: "dashscope", Endpoint: "https://a.com", Credentials: `{"api_key": "sk-a"}`,
	})
	p2, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name: "provider-b", DefinitionID: "deepseek", Endpoint: "https://b.com", Credentials: `{"api_key": "sk-b"}`,
	})

	// 各注册模型
	modelRepo.Create(&model.Model{Name: "model-a1", ProviderID: p1.ID, ModelType: "chat", Enabled: boolPtr(true)})
	modelRepo.Create(&model.Model{Name: "model-a2", ProviderID: p1.ID, ModelType: "chat", Enabled: boolPtr(true)})
	modelRepo.Create(&model.Model{Name: "model-b1", ProviderID: p2.ID, ModelType: "chat", Enabled: boolPtr(true)})

	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	// 按渠道 A 筛选
	models, total, err := modelSvc.ListModels(&p1.ID, 1, 10)
	if err != nil {
		t.Fatalf("list models: %v", err)
	}
	if total != 2 {
		t.Errorf("expected 2 models for provider-a, got %d", total)
	}
	for _, m := range models {
		if m.ProviderID != p1.ID {
			t.Errorf("expected provider_id %s, got %s", p1.ID, m.ProviderID)
		}
	}
}
