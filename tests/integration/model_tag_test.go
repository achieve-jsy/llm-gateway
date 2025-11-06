package integration

import (
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
)

func TestCreateTag(t *testing.T) {
	db := setupTestDB(t)
	tagRepo := repository.NewModelTagRepo(db)

	tag := &model.ModelTag{
		Name:      "test-tag",
		Color:     "#2BA8A2",
		SortOrder: 10,
	}
	err := tagRepo.CreateTag(tag)
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if tag.ID == uuid.Nil {
		t.Fatal("expected non-nil tag ID")
	}
}

func TestListTags(t *testing.T) {
	db := setupTestDB(t)
	tagRepo := repository.NewModelTagRepo(db)

	tagRepo.CreateTag(&model.ModelTag{Name: "tag-a", Color: "#FF0000", SortOrder: 20})
	tagRepo.CreateTag(&model.ModelTag{Name: "tag-b", Color: "#00FF00", SortOrder: 10})

	tags, err := tagRepo.ListTags()
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}
	if len(tags) < 2 {
		t.Fatalf("expected at least 2 tags, got %d", len(tags))
	}
	// 验证排序（sort_order DESC）
	if tags[0].SortOrder < tags[1].SortOrder {
		t.Error("expected tags sorted by sort_order DESC")
	}
}

func TestSetModelTags(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	tagRepo := repository.NewModelTagRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	// 创建渠道和模型
	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "tag-test-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})
	enabled := true
	m := &model.Model{
		Name: "tag-test-model", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled,
	}
	modelRepo.Create(m)

	// 创建标签
	tag1 := &model.ModelTag{Name: "视觉-test", Color: "#5DADE2"}
	tag2 := &model.ModelTag{Name: "推理-test", Color: "#FFD23F"}
	tagRepo.CreateTag(tag1)
	tagRepo.CreateTag(tag2)

	// 设置模型标签
	err := tagRepo.SetModelTags(m.ID, []uuid.UUID{tag1.ID, tag2.ID})
	if err != nil {
		t.Fatalf("set model tags: %v", err)
	}

	// 查询模型标签
	tags, err := tagRepo.GetModelTags(m.ID)
	if err != nil {
		t.Fatalf("get model tags: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(tags))
	}
}

func TestSetModelTags_Replace(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	tagRepo := repository.NewModelTagRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "tag-replace-provider",
		DefinitionID: "deepseek",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})
	enabled := true
	m := &model.Model{Name: "tag-replace-model", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	modelRepo.Create(m)

	tag1 := &model.ModelTag{Name: "old-tag", Color: "#111111"}
	tag2 := &model.ModelTag{Name: "new-tag", Color: "#222222"}
	tagRepo.CreateTag(tag1)
	tagRepo.CreateTag(tag2)

	// 先设置 tag1
	tagRepo.SetModelTags(m.ID, []uuid.UUID{tag1.ID})
	tags, _ := tagRepo.GetModelTags(m.ID)
	if len(tags) != 1 || tags[0].Name != "old-tag" {
		t.Fatal("expected 1 tag 'old-tag'")
	}

	// 替换为 tag2（全量替换）
	tagRepo.SetModelTags(m.ID, []uuid.UUID{tag2.ID})
	tags, _ = tagRepo.GetModelTags(m.ID)
	if len(tags) != 1 || tags[0].Name != "new-tag" {
		t.Fatal("expected 1 tag 'new-tag' after replace")
	}
}

func TestDeleteTag_CascadeRelations(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "12345678901234567890123456789012")

	db := setupTestDB(t)
	providerRepo := repository.NewProviderRepo(db)
	modelRepo := repository.NewModelRepo(db)
	tagRepo := repository.NewModelTagRepo(db)
	providerSvc := service.NewProviderService(providerRepo, modelRepo, nil)

	provider, _ := providerSvc.CreateProvider(service.CreateProviderInput{
		Name:         "tag-delete-provider",
		DefinitionID: "dashscope",
		Endpoint:     "https://test.com",
		Credentials:  `{"api_key": "sk-test"}`,
	})
	enabled := true
	m := &model.Model{Name: "tag-delete-model", ProviderID: provider.ID, ModelType: "chat", Enabled: &enabled}
	modelRepo.Create(m)

	tag := &model.ModelTag{Name: "to-delete", Color: "#333333"}
	tagRepo.CreateTag(tag)
	tagRepo.SetModelTags(m.ID, []uuid.UUID{tag.ID})

	// 删除标签
	err := tagRepo.DeleteTag(tag.ID)
	if err != nil {
		t.Fatalf("delete tag: %v", err)
	}

	// 验证关联也被删除
	tags, _ := tagRepo.GetModelTags(m.ID)
	if len(tags) != 0 {
		t.Errorf("expected 0 tags after delete, got %d", len(tags))
	}
}
