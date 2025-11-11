package integration

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
)

func TestBatchSetModelTags(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	tagRepo := repository.NewModelTagRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建渠道
	provider, err := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "batch-tag-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-batch-test"}`,
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// 创建 3 个模型
	enabled := true
	m1 := &model.Model{Name: "batch-model-1", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	m2 := &model.Model{Name: "batch-model-2", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	m3 := &model.Model{Name: "batch-model-3", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	modelRepo.Create(m1)
	modelRepo.Create(m2)
	modelRepo.Create(m3)

	// 创建 2 个标签
	tag1 := &model.ModelTag{Name: "batch-tag-文本", Color: "#2BA8A2"}
	tag2 := &model.ModelTag{Name: "batch-tag-推理", Color: "#FFD23F"}
	tagRepo.CreateTag(tag1)
	tagRepo.CreateTag(tag2)

	// 批量设置标签
	modelIDs := []uuid.UUID{m1.ID, m2.ID, m3.ID}
	tagIDs := []uuid.UUID{tag1.ID, tag2.ID}

	for _, modelID := range modelIDs {
		err := tagRepo.SetModelTags(modelID, tagIDs)
		if err != nil {
			t.Fatalf("set model tags for %s: %v", modelID, err)
		}
	}

	// 验证每个模型都有 2 个标签
	for _, modelID := range modelIDs {
		tags, err := tagRepo.GetModelTags(modelID)
		if err != nil {
			t.Fatalf("get model tags: %v", err)
		}
		if len(tags) != 2 {
			t.Errorf("model %s: expected 2 tags, got %d", modelID, len(tags))
		}
	}
}

func TestBatchSetModelTags_PartialOverwrite(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	tagRepo := repository.NewModelTagRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "batch-overwrite-provider",
		DefinitionID: "deepseek",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	// 创建模型
	enabled := true
	m := &model.Model{Name: "batch-overwrite-model", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	modelRepo.Create(m)

	// 创建标签
	tagOld := &model.ModelTag{Name: "batch-old-tag", Color: "#111111"}
	tagNew := &model.ModelTag{Name: "batch-new-tag", Color: "#222222"}
	tagRepo.CreateTag(tagOld)
	tagRepo.CreateTag(tagNew)

	// 先给模型设置 old 标签
	tagRepo.SetModelTags(m.ID, []uuid.UUID{tagOld.ID})
	tags, _ := tagRepo.GetModelTags(m.ID)
	if len(tags) != 1 || tags[0].Name != "batch-old-tag" {
		t.Fatal("expected old tag")
	}

	// 批量设置新标签（全量替换）
	tagRepo.SetModelTags(m.ID, []uuid.UUID{tagNew.ID})
	tags, _ = tagRepo.GetModelTags(m.ID)
	if len(tags) != 1 || tags[0].Name != "batch-new-tag" {
		t.Fatalf("expected new tag after batch set, got %v", tags)
	}
}

func TestBatchSetModelTags_EmptyTags(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	tagRepo := repository.NewModelTagRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "batch-empty-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})

	enabled := true
	m := &model.Model{Name: "batch-empty-model", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	modelRepo.Create(m)

	tag := &model.ModelTag{Name: "batch-to-clear", Color: "#333333"}
	tagRepo.CreateTag(tag)

	// 先设置标签
	tagRepo.SetModelTags(m.ID, []uuid.UUID{tag.ID})
	tags, _ := tagRepo.GetModelTags(m.ID)
	if len(tags) != 1 {
		t.Fatal("expected 1 tag before clear")
	}

	// 设置空标签（清除所有标签）
	tagRepo.SetModelTags(m.ID, []uuid.UUID{})
	tags, _ = tagRepo.GetModelTags(m.ID)
	if len(tags) != 0 {
		t.Errorf("expected 0 tags after clearing, got %d", len(tags))
	}
}
