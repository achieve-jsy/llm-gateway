package integration

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
)

func TestPagination_Models_FirstPage(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	// 创建渠道
	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "page-test-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-page-test"}`,
	})

	// 创建 15 个模型
	for i := 0; i < 15; i++ {
		modelSvc.CreateModel(service.CreateModelInput{
			Name:       "page-model-" + string(rune('a'+i)),
			ProviderID: provider.ID.String(),
			ModelType:  "chat",
		})
	}

	// 第 1 页，每页 5 条
	models, total, err := modelSvc.ListModels(repository.ModelListFilter{}, 1, 5)
	if err != nil {
		t.Fatalf("list models page 1: %v", err)
	}
	if total != 15 {
		t.Errorf("expected total=15, got %d", total)
	}
	if len(models) != 5 {
		t.Errorf("expected 5 models on page 1, got %d", len(models))
	}
}

func TestPagination_Models_SecondPage(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "page2-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	for i := 0; i < 15; i++ {
		modelSvc.CreateModel(service.CreateModelInput{
			Name:       "page2-model-" + string(rune('a'+i)),
			ProviderID: provider.ID.String(),
			ModelType:  "chat",
		})
	}

	// 第 2 页，每页 5 条
	models, total, err := modelSvc.ListModels(repository.ModelListFilter{}, 2, 5)
	if err != nil {
		t.Fatalf("list models page 2: %v", err)
	}
	if total != 15 {
		t.Errorf("expected total=15, got %d", total)
	}
	if len(models) != 5 {
		t.Errorf("expected 5 models on page 2, got %d", len(models))
	}
}

func TestPagination_Models_LastPage(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "page-last-provider",
		DefinitionID: "deepseek",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	// 创建 12 个模型
	for i := 0; i < 12; i++ {
		modelSvc.CreateModel(service.CreateModelInput{
			Name:       "last-model-" + string(rune('a'+i)),
			ProviderID: provider.ID.String(),
			ModelType:  "chat",
		})
	}

	// 第 3 页，每页 5 条（应该只有 2 条）
	models, total, err := modelSvc.ListModels(repository.ModelListFilter{}, 3, 5)
	if err != nil {
		t.Fatalf("list models last page: %v", err)
	}
	if total != 12 {
		t.Errorf("expected total=12, got %d", total)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models on last page, got %d", len(models))
	}
}

func TestPagination_Models_PageSizes(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)
	modelSvc := service.NewModelService(modelRepo, providerRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "sizes-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	for i := 0; i < 25; i++ {
		modelSvc.CreateModel(service.CreateModelInput{
			Name:       "sizes-model-" + string(rune('a'+i)),
			ProviderID: provider.ID.String(),
			ModelType:  "chat",
		})
	}

	// 测试不同 page_size
	testCases := []struct {
		pageSize int
		page     int
		expected int
	}{
		{10, 1, 10},
		{10, 3, 5},
		{20, 1, 20},
		{20, 2, 5},
		{50, 1, 25},
		{100, 1, 25},
	}

	for _, tc := range testCases {
		models, _, err := modelSvc.ListModels(repository.ModelListFilter{}, tc.page, tc.pageSize)
		if err != nil {
			t.Fatalf("pageSize=%d page=%d: %v", tc.pageSize, tc.page, err)
		}
		if len(models) != tc.expected {
			t.Errorf("pageSize=%d page=%d: expected %d models, got %d", tc.pageSize, tc.page, tc.expected, len(models))
		}
	}
}

func TestPagination_Providers(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建 8 个渠道
	for i := 0; i < 8; i++ {
		providerSvc.CreateProvider(service.CreateProviderInput{
			Name:         "prov-page-" + string(rune('a'+i)),
			DefinitionID: "dashscope",
			Endpoint:     "https://test.com",
			Credentials:  `{"api_key": "sk-test"}`,
		})
	}

	// 第 1 页，每页 3 条
	providers, total, err := providerSvc.ListProviders(repository.ProviderListFilter{}, 1, 3)
	if err != nil {
		t.Fatalf("list providers: %v", err)
	}
	if total != 8 {
		t.Errorf("expected total=8, got %d", total)
	}
	if len(providers) != 3 {
		t.Errorf("expected 3 providers on page 1, got %d", len(providers))
	}

	// 第 3 页，每页 3 条（应该只有 2 条）
	providers, _, err = providerSvc.ListProviders(repository.ProviderListFilter{}, 3, 3)
	if err != nil {
		t.Fatalf("list providers page 3: %v", err)
	}
	if len(providers) != 2 {
		t.Errorf("expected 2 providers on page 3, got %d", len(providers))
	}
}

func TestPagination_Models_FilterWithPagination(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建 2 个渠道
	p1, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name: "filter-page-a", DefinitionID: "dashscope", Endpoint: "https://a.com", Credentials: `{"api_key": "sk-a"}`,
	})
	p2, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name: "filter-page-b", DefinitionID: "deepseek", Endpoint: "https://b.com", Credentials: `{"api_key": "sk-b"}`,
	})

	// p1 下 10 个模型，p2 下 5 个
	enabled := true
	for i := 0; i < 10; i++ {
		modelRepo.Create(&model.Model{Name: "fp-a-" + string(rune('a'+i)), ProviderID: p1.ID, ModelType: "chat", Enabled: &enabled})
	}
	for i := 0; i < 5; i++ {
		modelRepo.Create(&model.Model{Name: "fp-b-" + string(rune('a'+i)), ProviderID: p2.ID, ModelType: "chat", Enabled: &enabled})
	}

	// 按 p1 筛选，每页 3 条，第 1 页
	models, total, err := modelRepo.List(repository.ModelListFilter{ProviderID: &p1.ID}, 0, 3)
	if err != nil {
		t.Fatalf("list with filter: %v", err)
	}
	if total != 10 {
		t.Errorf("expected total=10 for provider a, got %d", total)
	}
	if len(models) != 3 {
		t.Errorf("expected 3 models, got %d", len(models))
	}
}
