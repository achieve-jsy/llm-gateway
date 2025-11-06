package service

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// TestMain 为整个 service 包测试初始化加密 KeyManager（DB-less 测试桩）。
// 新版 crypto 包不再读取 ENCRYPTION_KEY 环境变量，生产走 InitFromDB，
// 测试这里走 InitForTesting，使用固定 DEK 保证可重复。
func TestMain(m *testing.M) {
	crypto.InitForTesting()
	os.Exit(m.Run())
}

// MockProviderRepo 实现 ProviderRepoInterface（service 包中已定义）
type MockProviderRepo struct {
	createFn                    func(provider *model.Provider) error
	getByIDFn                   func(id uuid.UUID) (*model.Provider, error)
	listFn                      func(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error)
	updateFn                    func(provider *model.Provider) error
	updateStatusFn              func(id uuid.UUID, enabled bool) error
	deleteFn                    func(id uuid.UUID) error
	existsByNameFn              func(name string) (bool, error)
	existsByNameExcludingFn     func(name string, excludeID uuid.UUID) (bool, error)
	updateResponseTimeFn        func(id uuid.UUID, ms int) error
}

func (m *MockProviderRepo) Create(provider *model.Provider) error {
	if m.createFn != nil {
		return m.createFn(provider)
	}
	return nil
}

func (m *MockProviderRepo) GetByID(id uuid.UUID) (*model.Provider, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return nil, nil
}

func (m *MockProviderRepo) List(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
	if m.listFn != nil {
		return m.listFn(filter, offset, limit)
	}
	return nil, 0, nil
}

func (m *MockProviderRepo) Update(provider *model.Provider) error {
	if m.updateFn != nil {
		return m.updateFn(provider)
	}
	return nil
}

func (m *MockProviderRepo) UpdateStatus(id uuid.UUID, enabled bool) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(id, enabled)
	}
	return nil
}

func (m *MockProviderRepo) Delete(id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	return nil
}

func (m *MockProviderRepo) ExistsByName(name string) (bool, error) {
	if m.existsByNameFn != nil {
		return m.existsByNameFn(name)
	}
	return false, nil
}

func (m *MockProviderRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	if m.existsByNameExcludingFn != nil {
		return m.existsByNameExcludingFn(name, excludeID)
	}
	return false, nil
}

func (m *MockProviderRepo) UpdateResponseTime(id uuid.UUID, ms int) error {
	if m.updateResponseTimeFn != nil {
		return m.updateResponseTimeFn(id, ms)
	}
	return nil
}

// MockModelRepo 实现 ModelRepoInterface（service 包中已定义）
type MockModelRepo struct {
	countByProviderIDFn         func(providerID uuid.UUID) (int64, error)
	batchCountByProviderIDFn    func(providerIDs []uuid.UUID) (map[uuid.UUID]int64, error)
	getByIDsFn                  func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
	updateEnabledByProviderIDFn func(providerID uuid.UUID, enabled bool) error
	createFn                    func(m *model.Model) error
	batchCreateFn               func(ms []*model.Model) error
	getByIDFn                   func(id uuid.UUID) (*model.Model, error)
	listFn                      func(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error)
	updateFn                    func(m *model.Model) error
	deleteFn                    func(id uuid.UUID) error
	existsByNameFn              func(name string) (bool, error)
	existsByNameExcludingFn     func(name string, excludeID uuid.UUID) (bool, error)
	listAvailableFn             func() ([]model.Model, error)
	resetOverrideFn             func(modelID uuid.UUID, field string) error
	listByCatalogIDFn           func() ([]model.Model, error)
}

func (m *MockModelRepo) CountByProviderID(providerID uuid.UUID) (int64, error) {
	if m.countByProviderIDFn != nil {
		return m.countByProviderIDFn(providerID)
	}
	return 0, nil
}

func (m *MockModelRepo) UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error {
	if m.updateEnabledByProviderIDFn != nil {
		return m.updateEnabledByProviderIDFn(providerID, enabled)
	}
	return nil
}

func (m *MockModelRepo) Create(model *model.Model) error {
	if m.createFn != nil {
		return m.createFn(model)
	}
	return nil
}

// BatchCreate 模拟批量注册：如果未注入 batchCreateFn，则回退到逐个调用 createFn，
// 以兼容历史上对 createFn 的断言（如 capturedModels 累积）
func (m *MockModelRepo) BatchCreate(ms []*model.Model) error {
	if m.batchCreateFn != nil {
		return m.batchCreateFn(ms)
	}
	if m.createFn != nil {
		for _, item := range ms {
			if err := m.createFn(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *MockModelRepo) GetByID(id uuid.UUID) (*model.Model, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return nil, nil
}

func (m *MockModelRepo) List(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
	if m.listFn != nil {
		return m.listFn(filter, offset, limit)
	}
	return nil, 0, nil
}

func (m *MockModelRepo) Update(model *model.Model) error {
	if m.updateFn != nil {
		return m.updateFn(model)
	}
	return nil
}

func (m *MockModelRepo) Delete(id uuid.UUID) error {
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	return nil
}

func (m *MockModelRepo) ExistsByName(name string) (bool, error) {
	if m.existsByNameFn != nil {
		return m.existsByNameFn(name)
	}
	return false, nil
}

func (m *MockModelRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	if m.existsByNameExcludingFn != nil {
		return m.existsByNameExcludingFn(name, excludeID)
	}
	return false, nil
}

func (m *MockModelRepo) ListAvailable() ([]model.Model, error) {
	if m.listAvailableFn != nil {
		return m.listAvailableFn()
	}
	return nil, nil
}

// BatchDelete 批量删除 mock，默认返回 ids 长度
func (m *MockModelRepo) BatchDelete(ids []uuid.UUID) (int64, error) {
	return int64(len(ids)), nil
}

// BatchUpdateEnabled 批量启停 mock，默认返回 ids 长度
func (m *MockModelRepo) BatchUpdateEnabled(ids []uuid.UUID, enabled bool) (int64, error) {
	return int64(len(ids)), nil
}

func (m *MockModelRepo) DeleteModelTagRelations(modelID uuid.UUID) error {
	return nil
}

func (m *MockModelRepo) BatchCountByProviderID(providerIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	result := map[uuid.UUID]int64{}
	if m.countByProviderIDFn != nil {
		for _, id := range providerIDs {
			count, err := m.countByProviderIDFn(id)
			if err != nil {
				return nil, err
			}
			result[id] = count
		}
	}
	return result, nil
}

func (m *MockModelRepo) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	if m.getByIDsFn != nil {
		return m.getByIDsFn(ids)
	}
	result := map[uuid.UUID]*model.Model{}
	if m.getByIDFn != nil {
		for _, id := range ids {
			m2, err := m.getByIDFn(id)
			if err != nil {
				return nil, err
			}
			result[id] = m2
		}
	}
	return result, nil
}

func (m *MockModelRepo) GetHealthByNames(names []string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}

// NewTestProviderService 通过 interface 注入 mock，无需 unsafe 转换
func NewTestProviderService(providerRepo ProviderRepoInterface, modelRepo ModelRepoInterface) *ProviderService {
	return NewProviderService(providerRepo, modelRepo, nil)
}

// 编译期接口断言
var (
	_ ProviderRepoInterface = (*MockProviderRepo)(nil)
	_ ModelRepoInterface    = (*MockModelRepo)(nil)
)

func TestCreateProvider_Success(t *testing.T) {
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
		createFn: func(provider *model.Provider) error {
			provider.ID = uuid.New()
			provider.CreatedAt = time.Now()
			provider.UpdatedAt = time.Now()
			return nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := CreateProviderInput{
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-test-key",
	}

	provider, err := svc.CreateProvider(input)

	assert.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, "test-provider", provider.Name)
	assert.True(t, provider.Enabled)
	assert.NotEmpty(t, provider.ID)
}

func TestCreateProvider_NameExists(t *testing.T) {
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) {
			return true, nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := CreateProviderInput{
		Name:         "existing-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-test-key",
	}

	provider, err := svc.CreateProvider(input)

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.True(t, errors.Is(err, ErrProviderNameExists))
}

func TestCreateProvider_CheckNameError(t *testing.T) {
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, errors.New("database error")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := CreateProviderInput{
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-test-key",
	}

	provider, err := svc.CreateProvider(input)

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "database error")
}

func TestCreateProvider_CreateError(t *testing.T) {
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) {
			return false, nil
		},
		createFn: func(provider *model.Provider) error {
			return errors.New("insert error")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := CreateProviderInput{
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-test-key",
	}

	provider, err := svc.CreateProvider(input)

	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "insert error")
}

func TestGetProvider_Success(t *testing.T) {
	providerID := uuid.New()
	testProvider := &model.Provider{
		ID:           providerID,
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return testProvider, nil
			}
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	resp, err := svc.GetProvider(providerID)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-provider", resp.Name)
	assert.True(t, resp.Enabled)
}

func TestGetProvider_NotFound(t *testing.T) {
	providerID := uuid.New()

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			return nil, errors.New("record not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	resp, err := svc.GetProvider(providerID)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, ErrProviderNotFound))
}

func TestListProviders_Success(t *testing.T) {
	providers := []model.Provider{
		{
			ID:           uuid.New(),
			Name:         "provider-1",
			DefinitionID: "openai",
			Type:         "openai",
			Endpoint:     "https://api.openai.com/v1",
			Enabled:      true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			ID:           uuid.New(),
			Name:         "provider-2",
			DefinitionID: "anthropic",
			Type:         "anthropic",
			Endpoint:     "https://api.anthropic.com",
			Enabled:      false,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			return providers, int64(len(providers)), nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	responses, total, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 20)

	assert.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Equal(t, 2, len(responses))
	assert.Equal(t, "provider-1", responses[0].Name)
	assert.Equal(t, "provider-2", responses[1].Name)
}

func TestListProviders_EmptyList(t *testing.T) {
	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			return []model.Provider{}, 0, nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	responses, total, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 20)

	assert.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Equal(t, 0, len(responses))
}

func TestListProviders_DatabaseError(t *testing.T) {
	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			return nil, 0, errors.New("database error")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	responses, total, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 20)

	assert.Error(t, err)
	assert.Nil(t, responses)
	assert.Equal(t, int64(0), total)
}

func TestListProviders_PaginationDefaults(t *testing.T) {
	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			assert.Equal(t, 0, offset)
			assert.Equal(t, 20, limit)
			return []model.Provider{}, 0, nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	_, _, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 20)

	assert.NoError(t, err)
}

func TestListProviders_InvalidPage(t *testing.T) {
	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			assert.Equal(t, 0, offset)
			return []model.Provider{}, 0, nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	_, _, err := svc.ListProviders(repository.ProviderListFilter{}, 0, 20)

	assert.NoError(t, err)
}

func TestListProviders_InvalidPageSize(t *testing.T) {
	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			assert.Equal(t, 20, limit)
			return []model.Provider{}, 0, nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	_, _, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 0)

	assert.NoError(t, err)
}

func TestUpdateProvider_Success(t *testing.T) {
	providerID := uuid.New()
	existingProvider := &model.Provider{
		ID:           providerID,
		Name:         "old-name",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return existingProvider, nil
			}
			return nil, errors.New("not found")
		},
		existsByNameExcludingFn: func(name string, excludeID uuid.UUID) (bool, error) {
			return false, nil
		},
		updateFn: func(provider *model.Provider) error {
			return nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := UpdateProviderInput{
		Name:     "new-name",
		Endpoint: "https://new.api.openai.com",
	}

	resp, err := svc.UpdateProvider(providerID, input)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "new-name", resp.Name)
	assert.Equal(t, "https://new.api.openai.com", resp.Endpoint)
}

func TestUpdateProvider_NotFound(t *testing.T) {
	providerID := uuid.New()

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := UpdateProviderInput{
		Name: "new-name",
	}

	resp, err := svc.UpdateProvider(providerID, input)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, ErrProviderNotFound))
}

func TestUpdateProvider_NameConflict(t *testing.T) {
	providerID := uuid.New()
	existingProvider := &model.Provider{
		ID:           providerID,
		Name:         "old-name",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return existingProvider, nil
			}
			return nil, errors.New("not found")
		},
		existsByNameExcludingFn: func(name string, excludeID uuid.UUID) (bool, error) {
			return true, nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	input := UpdateProviderInput{
		Name: "existing-name",
	}

	resp, err := svc.UpdateProvider(providerID, input)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, ErrProviderNameExists))
}

func TestToggleProviderStatus_Enable(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:           providerID,
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      false,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
		updateStatusFn: func(id uuid.UUID, enabled bool) error {
			provider.Enabled = enabled
			return nil
		},
	}
	modelRepo := &MockModelRepo{
		updateEnabledByProviderIDFn: func(providerID uuid.UUID, enabled bool) error {
			return nil
		},
	}

	svc := NewTestProviderService(providerRepo, modelRepo)

	resp, err := svc.ToggleProviderStatus(providerID, true)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.True(t, resp.Enabled)
}

func TestToggleProviderStatus_Disable(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:           providerID,
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
		updateStatusFn: func(id uuid.UUID, enabled bool) error {
			provider.Enabled = enabled
			return nil
		},
	}
	modelRepo := &MockModelRepo{
		updateEnabledByProviderIDFn: func(providerID uuid.UUID, enabled bool) error {
			return nil
		},
	}

	svc := NewTestProviderService(providerRepo, modelRepo)

	resp, err := svc.ToggleProviderStatus(providerID, false)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.False(t, resp.Enabled)
}

func TestToggleProviderStatus_NotFound(t *testing.T) {
	providerID := uuid.New()

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	resp, err := svc.ToggleProviderStatus(providerID, true)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, errors.Is(err, ErrProviderNotFound))
}

func TestDeleteProvider_Success(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:           providerID,
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
		deleteFn: func(id uuid.UUID) error {
			return nil
		},
	}
	modelRepo := &MockModelRepo{
		countByProviderIDFn: func(providerID uuid.UUID) (int64, error) {
			return 0, nil
		},
	}

	svc := NewTestProviderService(providerRepo, modelRepo)

	err := svc.DeleteProvider(providerID)

	assert.NoError(t, err)
}

func TestDeleteProvider_NotFound(t *testing.T) {
	providerID := uuid.New()

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			return nil, errors.New("not found")
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	err := svc.DeleteProvider(providerID)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderNotFound))
}

func TestDeleteProvider_HasModels(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:           providerID,
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
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
		countByProviderIDFn: func(providerID uuid.UUID) (int64, error) {
			return 5, nil
		},
	}

	svc := NewTestProviderService(providerRepo, modelRepo)

	err := svc.DeleteProvider(providerID)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrProviderHasModels))
}

func TestDeleteProvider_DeleteError(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:           providerID,
		Name:         "test-provider",
		DefinitionID: "openai",
		Type:         "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) {
			if id == providerID {
				return provider, nil
			}
			return nil, errors.New("not found")
		},
		deleteFn: func(id uuid.UUID) error {
			return errors.New("deletion failed")
		},
	}
	modelRepo := &MockModelRepo{
		countByProviderIDFn: func(providerID uuid.UUID) (int64, error) {
			return 0, nil
		},
	}

	svc := NewTestProviderService(providerRepo, modelRepo)

	err := svc.DeleteProvider(providerID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "deletion failed")
}

func TestProviderService_MaskedCredentials(t *testing.T) {
	providerID := uuid.New()
	provider := &model.Provider{
		ID:                   providerID,
		Name:                 "test-provider",
		DefinitionID:         "openai",
		Type:                 "openai",
		Endpoint:             "https://api.openai.com/v1",
		EncryptedCredentials: "encrypted-value",
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

	svc := NewTestProviderService(providerRepo, modelRepo)

	resp, err := svc.GetProvider(providerID)

	assert.NoError(t, err)
	assert.NotNil(t, resp)
	// Credentials should be masked, not plaintext
	assert.NotEqual(t, "encrypted-value", resp.MaskedCredentials)
	assert.NotContains(t, resp.MaskedCredentials, "sk-")
}

func TestProviderService_MultipleProviders(t *testing.T) {
	providers := []model.Provider{
		{
			ID:           uuid.New(),
			Name:         "provider-1",
			DefinitionID: "openai",
			Type:         "openai",
			Endpoint:     "https://api.openai.com/v1",
			Enabled:      true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			ID:           uuid.New(),
			Name:         "provider-2",
			DefinitionID: "anthropic",
			Type:         "anthropic",
			Endpoint:     "https://api.anthropic.com",
			Enabled:      true,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
		{
			ID:           uuid.New(),
			Name:         "provider-3",
			DefinitionID: "bedrock",
			Type:         "bedrock",
			Endpoint:     "https://bedrock.amazonaws.com",
			Enabled:      false,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		},
	}

	providerRepo := &MockProviderRepo{
		listFn: func(_ repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
			start := offset
			end := offset + limit
			if start > len(providers) {
				start = len(providers)
			}
			if end > len(providers) {
				end = len(providers)
			}
			return providers[start:end], int64(len(providers)), nil
		},
	}
	modelRepo := &MockModelRepo{}

	svc := NewTestProviderService(providerRepo, modelRepo)

	responses, total, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 2)

	assert.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Equal(t, 2, len(responses))
}

// =============================================================================
// S0-1 修复回归：CreateProviderInput / UpdateProviderInput 必须接受高级字段
//
// 这些用例首次跑通时（在生产代码补字段之前）会因字段被 Gin 丢弃而失败，
// 修复后转绿。它们的存在保证未来不会再次回退。
// =============================================================================

// TestCreateProvider_AdvancedFieldsAllPassed 覆盖 7 个补齐字段全部透传
func TestCreateProvider_AdvancedFieldsAllPassed(t *testing.T) {
	var captured *model.Provider
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) { return false, nil },
		createFn: func(p *model.Provider) error {
			p.ID = uuid.New()
			p.CreatedAt = time.Now()
			p.UpdatedAt = time.Now()
			captured = p
			return nil
		},
	}
	svc := NewTestProviderService(providerRepo, &MockModelRepo{})

	weight := 88
	priority := 7
	maxRetries := 5
	timeout := 120
	input := CreateProviderInput{
		Name:         "advanced-provider",
		DefinitionID: "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-x",
		Weight:       &weight,
		Priority:     &priority,
		TestModel:    "gpt-4o-mini",
		Remark:       "prod channel",
		MaxRetries:   &maxRetries,
		Timeout:      &timeout,
		Tag:          "production",
	}
	_, err := svc.CreateProvider(input)
	assert.NoError(t, err)
	assert.NotNil(t, captured)
	assert.Equal(t, 88, captured.Weight)
	assert.Equal(t, 7, captured.Priority)
	assert.Equal(t, "gpt-4o-mini", captured.TestModel)
	assert.Equal(t, "prod channel", captured.Remark)
	assert.Equal(t, 5, captured.MaxRetries)
	assert.Equal(t, 120, captured.Timeout)
	assert.Equal(t, "production", captured.Tag)
}

// TestCreateProvider_AdvancedFieldsDefaultsApplied 验证未传可选数值字段时的默认值
// （保证默认 weight=1, max_retries=3, timeout=30 与 DB 列默认一致，不会因为
// Go 零值落库导致语义错位）
func TestCreateProvider_AdvancedFieldsDefaultsApplied(t *testing.T) {
	var captured *model.Provider
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) { return false, nil },
		createFn: func(p *model.Provider) error {
			p.ID = uuid.New()
			captured = p
			return nil
		},
	}
	svc := NewTestProviderService(providerRepo, &MockModelRepo{})

	_, err := svc.CreateProvider(CreateProviderInput{
		Name:         "minimal-provider",
		DefinitionID: "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-x",
	})
	assert.NoError(t, err)
	assert.Equal(t, 1, captured.Weight)
	assert.Equal(t, 0, captured.Priority)
	assert.Equal(t, "", captured.TestModel)
	assert.Equal(t, "", captured.Remark)
	assert.Equal(t, 3, captured.MaxRetries)
	assert.Equal(t, 30, captured.Timeout)
	assert.Equal(t, "", captured.Tag)
}

// TestCreateProvider_ZeroValuesArePreservedViaPointer 显式传零值与未传需要可区分
func TestCreateProvider_ZeroValuesArePreservedViaPointer(t *testing.T) {
	var captured *model.Provider
	providerRepo := &MockProviderRepo{
		existsByNameFn: func(name string) (bool, error) { return false, nil },
		createFn: func(p *model.Provider) error {
			p.ID = uuid.New()
			captured = p
			return nil
		},
	}
	svc := NewTestProviderService(providerRepo, &MockModelRepo{})

	zero := 0
	_, err := svc.CreateProvider(CreateProviderInput{
		Name:         "zero-provider",
		DefinitionID: "openai",
		Endpoint:     "https://api.openai.com/v1",
		Credentials:  "sk-x",
		Weight:       &zero,
		Priority:     &zero,
		MaxRetries:   &zero,
		Timeout:      nil,
	})
	assert.NoError(t, err)
	assert.Equal(t, 0, captured.Weight, "显式传 0 应该保留 0 而非默认 1")
	assert.Equal(t, 0, captured.Priority)
	assert.Equal(t, 0, captured.MaxRetries)
	assert.Equal(t, 30, captured.Timeout, "Timeout=nil 走默认 30")
}

// TestUpdateProvider_AdvancedFieldsAllPassed 7 字段全部能写穿
func TestUpdateProvider_AdvancedFieldsAllPassed(t *testing.T) {
	providerID := uuid.New()
	original := &model.Provider{
		ID:           providerID,
		Name:         "p",
		DefinitionID: "openai",
		Endpoint:     "https://api.openai.com/v1",
		Enabled:      true,
		Weight:       1, Priority: 0, MaxRetries: 3, Timeout: 30,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	var captured *model.Provider
	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) { return original, nil },
		updateFn:  func(p *model.Provider) error { captured = p; return nil },
	}
	svc := NewTestProviderService(providerRepo, &MockModelRepo{})

	weight := 50
	priority := 9
	tm := "gpt-4o"
	rk := "vip"
	mr := 7
	to := 60
	tag := "tier-1"
	_, err := svc.UpdateProvider(providerID, UpdateProviderInput{
		Weight: &weight, Priority: &priority,
		TestModel: &tm, Remark: &rk,
		MaxRetries: &mr, Timeout: &to, Tag: &tag,
	})
	assert.NoError(t, err)
	assert.Equal(t, 50, captured.Weight)
	assert.Equal(t, 9, captured.Priority)
	assert.Equal(t, "gpt-4o", captured.TestModel)
	assert.Equal(t, "vip", captured.Remark)
	assert.Equal(t, 7, captured.MaxRetries)
	assert.Equal(t, 60, captured.Timeout)
	assert.Equal(t, "tier-1", captured.Tag)
}

// TestUpdateProvider_UntouchedFieldsArePreserved 未传字段不应清零
func TestUpdateProvider_UntouchedFieldsArePreserved(t *testing.T) {
	providerID := uuid.New()
	original := &model.Provider{
		ID: providerID, Name: "p", DefinitionID: "openai",
		Endpoint: "https://x", Enabled: true,
		Weight: 100, Priority: 9, TestModel: "gpt-4o",
		Remark: "vip", MaxRetries: 7, Timeout: 60, Tag: "tier-1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	var captured *model.Provider
	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) { return original, nil },
		updateFn:  func(p *model.Provider) error { captured = p; return nil },
	}
	svc := NewTestProviderService(providerRepo, &MockModelRepo{})

	// 只更新 Name，其他字段全部 nil
	_, err := svc.UpdateProvider(providerID, UpdateProviderInput{Name: "p-renamed"})
	assert.NoError(t, err)
	assert.Equal(t, "p-renamed", captured.Name)
	assert.Equal(t, 100, captured.Weight, "Weight 未传应保持原值 100")
	assert.Equal(t, 9, captured.Priority)
	assert.Equal(t, "gpt-4o", captured.TestModel)
	assert.Equal(t, "vip", captured.Remark)
	assert.Equal(t, 7, captured.MaxRetries)
	assert.Equal(t, 60, captured.Timeout)
	assert.Equal(t, "tier-1", captured.Tag)
}

// TestUpdateProvider_EmptyStringClearsField 通过 *string 主动传空串可清空
func TestUpdateProvider_EmptyStringClearsField(t *testing.T) {
	providerID := uuid.New()
	original := &model.Provider{
		ID: providerID, Name: "p", DefinitionID: "openai",
		Endpoint: "https://x", Enabled: true,
		TestModel: "gpt-4o", Remark: "vip", Tag: "tier-1",
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	var captured *model.Provider
	providerRepo := &MockProviderRepo{
		getByIDFn: func(id uuid.UUID) (*model.Provider, error) { return original, nil },
		updateFn:  func(p *model.Provider) error { captured = p; return nil },
	}
	svc := NewTestProviderService(providerRepo, &MockModelRepo{})

	empty := ""
	_, err := svc.UpdateProvider(providerID, UpdateProviderInput{
		TestModel: &empty, Remark: &empty, Tag: &empty,
	})
	assert.NoError(t, err)
	assert.Equal(t, "", captured.TestModel)
	assert.Equal(t, "", captured.Remark)
	assert.Equal(t, "", captured.Tag)
}
