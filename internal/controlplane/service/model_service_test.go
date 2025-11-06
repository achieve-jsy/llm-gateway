package service

import (
	"errors"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModelService(t *testing.T) {
	svc := NewModelService(&MockModelRepo{}, &MockProviderRepo{}, nil)
	assert.NotNil(t, svc)
}

func TestModelService_CreateModel_Success(t *testing.T) {
	providerID := uuid.New()
	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) { return false, nil },
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			return nil
		},
	}
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: providerID, Enabled: true}, nil
	}}
	svc := NewModelService(modelRepo, providerRepo, nil)

	m, err := svc.CreateModel(CreateModelInput{Name: "n", ProviderID: providerID.String(), ModelType: "chat"})
	require.NoError(t, err)
	assert.NotNil(t, m)
	assert.Equal(t, "n", m.Name)
}

func TestModelService_CreateModel_NameExists(t *testing.T) {
	providerRepo := &MockProviderRepo{}
	modelRepo := &MockModelRepo{existsByNameFn: func(name string) (bool, error) { return true, nil }}
	svc := NewModelService(modelRepo, providerRepo, nil)

	_, err := svc.CreateModel(CreateModelInput{Name: "n", ProviderID: uuid.New().String(), ModelType: "chat"})
	require.ErrorIs(t, err, ErrModelNameExists)
}

func TestModelService_CreateModel_ExistsCheckError(t *testing.T) {
	providerRepo := &MockProviderRepo{}
	modelRepo := &MockModelRepo{existsByNameFn: func(name string) (bool, error) { return false, errors.New("db") }}
	svc := NewModelService(modelRepo, providerRepo, nil)

	_, err := svc.CreateModel(CreateModelInput{Name: "n", ProviderID: uuid.New().String(), ModelType: "chat"})
	require.Error(t, err)
}

func TestModelService_CreateModel_ProviderMissing(t *testing.T) {
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return nil, errors.New("not found")
	}}
	modelRepo := &MockModelRepo{existsByNameFn: func(name string) (bool, error) { return false, nil }}
	svc := NewModelService(modelRepo, providerRepo, nil)

	_, err := svc.CreateModel(CreateModelInput{Name: "n", ProviderID: uuid.New().String(), ModelType: "chat"})
	require.ErrorIs(t, err, ErrModelProviderInvalid)
}

func TestModelService_CreateModel_ProviderDisabled(t *testing.T) {
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: id, Enabled: false}, nil
	}}
	modelRepo := &MockModelRepo{existsByNameFn: func(name string) (bool, error) { return false, nil }}
	svc := NewModelService(modelRepo, providerRepo, nil)

	_, err := svc.CreateModel(CreateModelInput{Name: "n", ProviderID: uuid.New().String(), ModelType: "chat"})
	require.ErrorIs(t, err, ErrModelProviderInvalid)
}

func TestModelService_CreateModel_DBError(t *testing.T) {
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: id, Enabled: true}, nil
	}}
	modelRepo := &MockModelRepo{
		existsByNameFn: func(name string) (bool, error) { return false, nil },
		createFn:       func(m *model.Model) error { return errors.New("insert error") },
	}
	svc := NewModelService(modelRepo, providerRepo, nil)

	_, err := svc.CreateModel(CreateModelInput{Name: "n", ProviderID: uuid.New().String(), ModelType: "chat"})
	require.Error(t, err)
}

func TestModelService_ListModels(t *testing.T) {
	want := []model.Model{{ID: uuid.New(), Name: "x"}}
	pid := uuid.New()
	modelRepo := &MockModelRepo{listFn: func(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
		assert.NotNil(t, filter.ProviderID)
		return want, 1, nil
	}}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	got, total, err := svc.ListModels(repository.ModelListFilter{ProviderID: &pid}, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, want, got)
}

func TestModelService_GetModel(t *testing.T) {
	id := uuid.New()
	modelRepo := &MockModelRepo{getByIDFn: func(gotID uuid.UUID) (*model.Model, error) {
		assert.Equal(t, id, gotID)
		return &model.Model{ID: id, Name: "g"}, nil
	}}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	m, err := svc.GetModel(id)
	require.NoError(t, err)
	assert.Equal(t, "g", m.Name)
}

func TestModelService_GetModel_NotFound(t *testing.T) {
	modelRepo := &MockModelRepo{getByIDFn: func(id uuid.UUID) (*model.Model, error) {
		return nil, errors.New("not found")
	}}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	_, err := svc.GetModel(uuid.New())
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestModelService_UpdateModel_Success(t *testing.T) {
	id := uuid.New()
	pid := uuid.New()
	existing := &model.Model{ID: id, Name: "old"}
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return existing, nil },
		existsByNameExcludingFn: func(string, uuid.UUID) (bool, error) { return false, nil },
		updateFn: func(*model.Model) error { return nil },
	}
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: id, Enabled: true}, nil
	}}
	svc := NewModelService(modelRepo, providerRepo, nil)

	enabled := false
	got, err := svc.UpdateModel(id, UpdateModelInput{Name: "new", ProviderID: pid.String(), ModelType: "embedding", Enabled: &enabled})
	require.NoError(t, err)
	assert.Equal(t, "new", got.Name)
	assert.Equal(t, "embedding", got.ModelType)
	assert.False(t, *got.Enabled)
}

func TestModelService_UpdateModel_NotFound(t *testing.T) {
	modelRepo := &MockModelRepo{getByIDFn: func(uuid.UUID) (*model.Model, error) { return nil, errors.New("nf") }}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	_, err := svc.UpdateModel(uuid.New(), UpdateModelInput{})
	require.ErrorIs(t, err, ErrModelNotFound)
}

func TestModelService_UpdateModel_NameConflict(t *testing.T) {
	id := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn:               func(uuid.UUID) (*model.Model, error) { return &model.Model{ID: id, Name: "old"}, nil },
		existsByNameExcludingFn: func(string, uuid.UUID) (bool, error) { return true, nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	_, err := svc.UpdateModel(id, UpdateModelInput{Name: "new"})
	require.ErrorIs(t, err, ErrModelNameExists)
}

func TestModelService_UpdateModel_ProviderInvalid(t *testing.T) {
	id := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return &model.Model{ID: id, Name: "n"}, nil },
	}
	providerRepo := &MockProviderRepo{getByIDFn: func(uuid.UUID) (*model.Provider, error) {
		return nil, errors.New("missing")
	}}
	svc := NewModelService(modelRepo, providerRepo, nil)
	_, err := svc.UpdateModel(id, UpdateModelInput{ProviderID: uuid.New().String()})
	require.ErrorIs(t, err, ErrModelProviderInvalid)
}

func TestModelService_UpdateModel_DBError(t *testing.T) {
	id := uuid.New()
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return &model.Model{ID: id, Name: "n"}, nil },
		updateFn:  func(*model.Model) error { return errors.New("boom") },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	_, err := svc.UpdateModel(id, UpdateModelInput{})
	require.Error(t, err)
}

func TestModelService_DeleteModel_Success(t *testing.T) {
	id := uuid.New()
	deleted := false
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return &model.Model{ID: id}, nil },
		deleteFn:  func(uuid.UUID) error { deleted = true; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	require.NoError(t, svc.DeleteModel(id))
	assert.True(t, deleted)
}

func TestModelService_DeleteModel_NotFound(t *testing.T) {
	modelRepo := &MockModelRepo{getByIDFn: func(uuid.UUID) (*model.Model, error) { return nil, errors.New("nf") }}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	require.ErrorIs(t, svc.DeleteModel(uuid.New()), ErrModelNotFound)
}

func TestModelService_DeleteModel_DBError(t *testing.T) {
	modelRepo := &MockModelRepo{
		getByIDFn: func(uuid.UUID) (*model.Model, error) { return &model.Model{}, nil },
		deleteFn:  func(uuid.UUID) error { return errors.New("boom") },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	require.Error(t, svc.DeleteModel(uuid.New()))
}

// publishEvent 路径只需保证 publisher 为 nil 时不 panic（已隐式覆盖）。
// 显式验证一次：
func TestModelService_publishEvent_NilPublisher(t *testing.T) {
	svc := NewModelService(&MockModelRepo{}, &MockProviderRepo{}, nil)
	svc.publishEvent("test", "x", uuid.New()) // 不应 panic
}

// 提升 InternalService 覆盖率
func TestInternalService_ListAvailableModels(t *testing.T) {
	pid := uuid.New()
	models := []model.Model{
		{
			ID:         uuid.New(),
			Name:       "gpt-4",
			ProviderID: pid,
			ModelType:  "chat",
			Provider: model.Provider{
				ID:                   pid,
				Name:                 "openai",
				Type:                 "openai",
				Endpoint:             "https://api.openai.com",
				EncryptedCredentials: "broken-cipher", // Decrypt 会失败，但服务忽略错误返回空 credentials
			},
		},
	}
	modelRepo := &MockModelRepo{listAvailableFn: func() ([]model.Model, error) { return models, nil }}
	svc := NewInternalService(modelRepo, &MockProviderRepo{})

	got, err := svc.ListAvailableModels()
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "gpt-4", got[0].Name)
}

func TestInternalService_ListAvailableModels_Error(t *testing.T) {
	modelRepo := &MockModelRepo{listAvailableFn: func() ([]model.Model, error) { return nil, errors.New("db") }}
	svc := NewInternalService(modelRepo, &MockProviderRepo{})
	_, err := svc.ListAvailableModels()
	require.Error(t, err)
}

func TestInternalService_CheckModelNameExists(t *testing.T) {
	id := uuid.New()
	modelRepo := &MockModelRepo{
		existsByNameFn:          func(name string) (bool, error) { return name == "exists", nil },
		existsByNameExcludingFn: func(name string, excludeID uuid.UUID) (bool, error) { return false, nil },
	}
	svc := NewInternalService(modelRepo, &MockProviderRepo{})

	ok, err := svc.CheckModelNameExists("exists", nil)
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = svc.CheckModelNameExists("missing", nil)
	require.NoError(t, err)
	assert.False(t, ok)

	ok, err = svc.CheckModelNameExists("anything", &id)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestInternalService_GetProviderWithDecryptedCredentials(t *testing.T) {
	pid := uuid.New()
	providerRepo := &MockProviderRepo{getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
		return &model.Provider{ID: pid, EncryptedCredentials: "broken"}, nil
	}}
	svc := NewInternalService(&MockModelRepo{}, providerRepo)

	prov, creds, err := svc.GetProviderWithDecryptedCredentials(pid)
	// 由于密文非法，Decrypt 出错；但 provider 仍返回
	require.Error(t, err)
	assert.Equal(t, "", creds)
	assert.NotNil(t, prov)
	assert.Equal(t, pid, prov.ID)
}

func TestInternalService_GetProviderWithDecryptedCredentials_NotFound(t *testing.T) {
	providerRepo := &MockProviderRepo{getByIDFn: func(uuid.UUID) (*model.Provider, error) {
		return nil, errors.New("not found")
	}}
	svc := NewInternalService(&MockModelRepo{}, providerRepo)

	_, _, err := svc.GetProviderWithDecryptedCredentials(uuid.New())
	require.Error(t, err)
}

// 帮助压低未使用变量警告
var _ = time.Now

// =============================================================================
// S0-2 修复回归：CreateModelInput / UpdateModelInput 必须接受参数字段
// =============================================================================

// modelTestSvc 构造一个 ModelService（带启用的 provider mock，便于 Create 走通）
func modelTestSvc(modelRepo *MockModelRepo, providerID uuid.UUID) *ModelService {
	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			return &model.Provider{ID: providerID, Name: "p", Enabled: true}, nil
		},
	}
	return NewModelService(modelRepo, providerRepo, nil)
}

func TestCreateModel_AdvancedFieldsAllPassed(t *testing.T) {
	pid := uuid.New()
	var captured *model.Model
	modelRepo := &MockModelRepo{
		existsByNameFn: func(string) (bool, error) { return false, nil },
		createFn: func(m *model.Model) error {
			m.ID = uuid.New()
			captured = m
			return nil
		},
	}
	svc := modelTestSvc(modelRepo, pid)

	cl := 128000
	mot := 8192
	ip := 0.0025
	op := 0.01
	w := 7
	_, err := svc.CreateModel(CreateModelInput{
		Name:            "gpt-4o",
		ProviderID:      pid.String(),
		ModelType:       "chat",
		UpstreamModelID: "openai/gpt-4o",
		Description:     "OpenAI Omni",
		ContextLength:   &cl,
		MaxOutputTokens: &mot,
		InputPrice:      &ip,
		OutputPrice:     &op,
		Weight:          &w,
	})
	assert.NoError(t, err)
	assert.NotNil(t, captured)
	assert.Equal(t, "openai/gpt-4o", captured.UpstreamModelID)
	assert.Equal(t, "OpenAI Omni", captured.Description)
	assert.Equal(t, 128000, captured.ContextLength)
	assert.Equal(t, 8192, captured.MaxOutputTokens)
	assert.InDelta(t, 0.0025, captured.InputPrice, 1e-9)
	assert.InDelta(t, 0.01, captured.OutputPrice, 1e-9)
	assert.Equal(t, 7, captured.Weight)
}

func TestCreateModel_DefaultWeightIsOne(t *testing.T) {
	pid := uuid.New()
	var captured *model.Model
	modelRepo := &MockModelRepo{
		existsByNameFn: func(string) (bool, error) { return false, nil },
		createFn:       func(m *model.Model) error { captured = m; return nil },
	}
	svc := modelTestSvc(modelRepo, pid)
	_, err := svc.CreateModel(CreateModelInput{
		Name: "m", ProviderID: pid.String(), ModelType: "chat",
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, captured.Weight, "Weight=nil 应落 DB 列默认 1")
	assert.Equal(t, 0, captured.ContextLength)
	assert.Equal(t, 0.0, captured.InputPrice)
}

func TestUpdateModel_AdvancedFieldsAllPassed(t *testing.T) {
	mid := uuid.New()
	enabled := true
	original := &model.Model{ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat", Enabled: &enabled}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(id uuid.UUID) (*model.Model, error) { return original, nil },
		updateFn:  func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	um := "openai/gpt-4o-2"
	desc := "v2"
	cl := 200000
	mot := 16384
	ip := 0.005
	op := 0.02
	w := 9
	_, err := svc.UpdateModel(mid, UpdateModelInput{
		UpstreamModelID: &um,
		Description:     &desc,
		ContextLength:   &cl,
		MaxOutputTokens: &mot,
		InputPrice:      &ip,
		OutputPrice:     &op,
		Weight:          &w,
	})
	assert.NoError(t, err)
	assert.Equal(t, "openai/gpt-4o-2", captured.UpstreamModelID)
	assert.Equal(t, "v2", captured.Description)
	assert.Equal(t, 200000, captured.ContextLength)
	assert.Equal(t, 16384, captured.MaxOutputTokens)
	assert.InDelta(t, 0.005, captured.InputPrice, 1e-9)
	assert.InDelta(t, 0.02, captured.OutputPrice, 1e-9)
	assert.Equal(t, 9, captured.Weight)
}

func TestUpdateModel_UntouchedFieldsArePreserved(t *testing.T) {
	mid := uuid.New()
	enabled := true
	original := &model.Model{
		ID: mid, Name: "m", ProviderID: uuid.New(), ModelType: "chat", Enabled: &enabled,
		UpstreamModelID: "u1", Description: "d1", ContextLength: 1, MaxOutputTokens: 2,
		InputPrice: 0.1, OutputPrice: 0.2, Weight: 3,
	}
	var captured *model.Model
	modelRepo := &MockModelRepo{
		getByIDFn: func(id uuid.UUID) (*model.Model, error) { return original, nil },
		updateFn:  func(m *model.Model) error { captured = m; return nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)

	// 只把 Name 改了，其他字段全部 nil
	_, err := svc.UpdateModel(mid, UpdateModelInput{Name: "m-renamed"})
	assert.NoError(t, err)
	assert.Equal(t, "m-renamed", captured.Name)
	assert.Equal(t, "u1", captured.UpstreamModelID)
	assert.Equal(t, "d1", captured.Description)
	assert.Equal(t, 1, captured.ContextLength)
	assert.Equal(t, 2, captured.MaxOutputTokens)
	assert.InDelta(t, 0.1, captured.InputPrice, 1e-9)
	assert.InDelta(t, 0.2, captured.OutputPrice, 1e-9)
	assert.Equal(t, 3, captured.Weight)
}

// =============================================================================
// S0-3 修复回归：CheckModelName 独立服务方法
// =============================================================================

func TestCheckModelName_Available(t *testing.T) {
	modelRepo := &MockModelRepo{
		existsByNameFn: func(string) (bool, error) { return false, nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	ok, err := svc.CheckModelName("never-used")
	assert.NoError(t, err)
	assert.True(t, ok, "未存在的名字应返回 available=true")
}

func TestCheckModelName_NotAvailable(t *testing.T) {
	modelRepo := &MockModelRepo{
		existsByNameFn: func(string) (bool, error) { return true, nil },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	ok, err := svc.CheckModelName("gpt-4o")
	assert.NoError(t, err)
	assert.False(t, ok, "已存在的名字应返回 available=false")
}

func TestCheckModelName_RepoError(t *testing.T) {
	modelRepo := &MockModelRepo{
		existsByNameFn: func(string) (bool, error) { return false, errors.New("db down") },
	}
	svc := NewModelService(modelRepo, &MockProviderRepo{}, nil)
	ok, err := svc.CheckModelName("anything")
	assert.Error(t, err)
	assert.False(t, ok)
	assert.Contains(t, err.Error(), "db down")
}
