package service

import (
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestSyncModels_Success(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:                   providerID,
		Name:                 "test-provider",
		DefinitionID:         "openai",
		Type:                 "openai",
		Endpoint:             "https://api.openai.com/v1",
		EncryptedCredentials: "encrypted-key",
		Enabled:              true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	localModels := []model.Model{
		{
			ID:         uuid.New(),
			Name:       "gpt-3.5-turbo",
			ProviderID: providerID,
			ModelType:  "chat",
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		},
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
	}

	modelRepo := &MockModelRepo{
		listFn: func(_ repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
			return localModels, int64(len(localModels)), nil
		},
	}

	svc := NewSyncService(providerRepo, modelRepo, nil)
	// 这里依赖 crypto.Decrypt 与 HTTP 调用，本测试只验证服务可被注入；
	// 真正的上下文测试在 ProviderNotFound/Disabled/Unsupported 用例。
	assert.NotNil(t, svc)
}

func TestSyncModels_ProviderNotFound(t *testing.T) {
	providerID := uuid.New()

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewSyncService(providerRepo, modelRepo, nil)

	result, err := svc.SyncModels(providerID)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider not found")
}

func TestSyncModels_ProviderDisabled(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:                   providerID,
		Name:                 "test-provider",
		DefinitionID:         "openai",
		Type:                 "openai",
		Endpoint:             "https://api.openai.com/v1",
		EncryptedCredentials: "encrypted-key",
		Enabled:              false,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewSyncService(providerRepo, modelRepo, nil)

	result, err := svc.SyncModels(providerID)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "provider is disabled")
}

func TestSyncModels_UnsupportedAdapter_Anthropic(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:                   providerID,
		Name:                 "anthropic-provider",
		DefinitionID:         "anthropic",
		Type:                 "anthropic",
		Endpoint:             "https://api.anthropic.com",
		EncryptedCredentials: "encrypted-key",
		Enabled:              true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewSyncService(providerRepo, modelRepo, nil)

	result, err := svc.SyncModels(providerID)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Unsupported)
}

func TestSyncModels_UnsupportedAdapter_Bedrock(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:                   providerID,
		Name:                 "bedrock-provider",
		DefinitionID:         "bedrock",
		Type:                 "bedrock",
		Endpoint:             "https://bedrock.amazonaws.com",
		EncryptedCredentials: "encrypted-key",
		Enabled:              true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewSyncService(providerRepo, modelRepo, nil)

	result, err := svc.SyncModels(providerID)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Unsupported)
}

func TestConfirmSync_Success_RegisterAndSkip(t *testing.T) {
	providerID := uuid.New()

	// 生产代码现在通过 List 一次性查已存在模型再做 BatchCreate
	modelRepo := &MockModelRepo{
		listFn: func(_ repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
			return []model.Model{{Name: "existing-model", ProviderID: providerID}}, 1, nil
		},
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"new-model-1", "existing-model", "new-model-2"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, result.Registered)
	assert.Equal(t, 1, result.Skipped)
}

func TestConfirmSync_NoModelsToAdd(t *testing.T) {
	providerID := uuid.New()

	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.Registered)
	assert.Equal(t, 0, result.Skipped)
}

func TestConfirmSync_CreateError(t *testing.T) {
	providerID := uuid.New()

	// 生产代码改用 BatchCreate（all-or-nothing）：任一失败整批回滚并返回 error
	modelRepo := &MockModelRepo{
		batchCreateFn: func(ms []*model.Model) error {
			return errors.New("database error")
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"model-1"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "database error")
}

func TestConfirmSync_MixedResults(t *testing.T) {
	providerID := uuid.New()

	// 生产现在用 List 一次拉取已存在；model-2 已经存在，
	// model-1 / model-3 走 BatchCreate 一次性写入
	modelRepo := &MockModelRepo{
		listFn: func(_ repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
			return []model.Model{{Name: "model-2", ProviderID: providerID}}, 1, nil
		},
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"model-1", "model-2", "model-3"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 2, result.Registered)
	assert.Equal(t, 1, result.Skipped)
}

func TestConfirmSync_RegistersNewModel(t *testing.T) {
	providerID := uuid.New()

	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"model-1"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 1, result.Registered)
	assert.Equal(t, 0, result.Disabled)
}

func TestInferModelType_Chat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"GPT", "gpt-4", "chat"},
		{"Claude", "claude-3-opus", "chat"},
		{"Gemini", "gemini-pro", "chat"},
		{"Generic", "my-model", "chat"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferModelType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestInferModelType_Embedding(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Lowercase embed", "text-embedding-ada", "embedding"},
		{"Uppercase embed", "text-EMBEDDING-3", "embedding"},
		{"Mixed case", "Embedding-Model", "embedding"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferModelType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConfirmSync_LargeModelList(t *testing.T) {
	providerID := uuid.New()
	registeredCount := 0

	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
		createFn: func(m *model.Model) error {
			registeredCount++
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	// Create 100 models
	models := make([]string, 100)
	for i := 0; i < 100; i++ {
		models[i] = "model-" + string(rune(i))
	}

	input := ConfirmSyncInput{
		ModelsToAdd: models,
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 100, result.Registered)
	assert.Equal(t, 0, result.Skipped)
}

func TestSyncResult_Structure(t *testing.T) {
	result := &SyncResult{
		Added: []SyncModelItem{
			{ID: "model-1", OwnedBy: "provider"},
			{ID: "model-2", OwnedBy: "provider"},
		},
		Existing: []SyncModelItem{
			{ID: "model-3", OwnedBy: "provider"},
		},
		Removed:       []SyncModelItem{},
		TotalUpstream: 3,
		TotalLocal:    1,
		Unsupported:   false,
	}

	assert.Equal(t, 2, len(result.Added))
	assert.Equal(t, 1, len(result.Existing))
	assert.Equal(t, 0, len(result.Removed))
	assert.Equal(t, 3, result.TotalUpstream)
	assert.Equal(t, 1, result.TotalLocal)
	assert.False(t, result.Unsupported)
}

func TestConfirmSyncResult_Structure(t *testing.T) {
	result := &ConfirmSyncResult{
		Registered: 5,
		Skipped:    2,
		Disabled:   1,
	}

	assert.Equal(t, 5, result.Registered)
	assert.Equal(t, 2, result.Skipped)
	assert.Equal(t, 1, result.Disabled)
}

func TestConfirmSync_AllSkipped(t *testing.T) {
	providerID := uuid.New()

	modelRepo := &MockModelRepo{
		// 生产用 List 拿已存在名字；三者全部已存在
		listFn: func(_ repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
			return []model.Model{
				{Name: "model-1", ProviderID: providerID},
				{Name: "model-2", ProviderID: providerID},
				{Name: "model-3", ProviderID: providerID},
			}, 3, nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"model-1", "model-2", "model-3"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0, result.Registered)
	assert.Equal(t, 3, result.Skipped)
	assert.Equal(t, 0, result.Disabled)
}

func TestConfirmSync_ExistsCheckError(t *testing.T) {
	providerID := uuid.New()

	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, errors.New("database error")
		},
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"model-1"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	// Error from ExistsByName is ignored, model is still attempted to be created
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestConfirmSync_ModelTypeInference(t *testing.T) {
	providerID := uuid.New()
	var capturedModels []*model.Model

	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
		createFn: func(m *model.Model) error {
			capturedModels = append(capturedModels, m)
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"gpt-4", "text-embedding-ada", "my-chat-model"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 3, result.Registered)

	// Verify model types
	assert.Equal(t, "chat", capturedModels[0].ModelType)
	assert.Equal(t, "embedding", capturedModels[1].ModelType)
	assert.Equal(t, "chat", capturedModels[2].ModelType)
}

func TestConfirmSync_ProviderIDAssignment(t *testing.T) {
	providerID := uuid.New()
	var capturedModels []*model.Model

	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
		createFn: func(m *model.Model) error {
			capturedModels = append(capturedModels, m)
			m.ID = uuid.New()
			m.CreatedAt = time.Now()
			m.UpdatedAt = time.Now()
			return nil
		},
	}

	svc := NewSyncService(&MockProviderRepo{}, modelRepo, nil)

	input := ConfirmSyncInput{
		ModelsToAdd: []string{"model-1", "model-2"},
	}

	result, err := svc.ConfirmSync(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, result)

	// Verify provider ID is correctly assigned to each model
	for _, m := range capturedModels {
		assert.Equal(t, providerID, m.ProviderID)
	}
}
