package service_test

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	crypto.InitForTesting()
	m.Run()
}

// fakeProviderRepo 模拟 ProviderRepoInterface 用于 ProviderService 单测
type fakeProviderRepo struct {
	createFunc              func(provider *model.Provider) error
	getByIDFunc             func(id uuid.UUID) (*model.Provider, error)
	listFunc                func(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error)
	updateFunc              func(provider *model.Provider) error
	updateStatusFunc        func(id uuid.UUID, enabled bool) error
	deleteFunc              func(id uuid.UUID) error
	existsByNameFunc        func(name string) (bool, error)
	existsByNameExclFunc    func(name string, excludeID uuid.UUID) (bool, error)
	updateResponseTimeFunc  func(id uuid.UUID, ms int) error
}

func (f *fakeProviderRepo) Create(provider *model.Provider) error {
	if f.createFunc != nil {
		return f.createFunc(provider)
	}
	return nil
}
func (f *fakeProviderRepo) GetByID(id uuid.UUID) (*model.Provider, error) {
	if f.getByIDFunc != nil {
		return f.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}
func (f *fakeProviderRepo) List(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error) {
	if f.listFunc != nil {
		return f.listFunc(filter, offset, limit)
	}
	return nil, 0, nil
}
func (f *fakeProviderRepo) Update(provider *model.Provider) error {
	if f.updateFunc != nil {
		return f.updateFunc(provider)
	}
	return nil
}
func (f *fakeProviderRepo) UpdateStatus(id uuid.UUID, enabled bool) error {
	if f.updateStatusFunc != nil {
		return f.updateStatusFunc(id, enabled)
	}
	return nil
}
func (f *fakeProviderRepo) Delete(id uuid.UUID) error {
	if f.deleteFunc != nil {
		return f.deleteFunc(id)
	}
	return nil
}
func (f *fakeProviderRepo) ExistsByName(name string) (bool, error) {
	if f.existsByNameFunc != nil {
		return f.existsByNameFunc(name)
	}
	return false, nil
}
func (f *fakeProviderRepo) ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error) {
	if f.existsByNameExclFunc != nil {
		return f.existsByNameExclFunc(name, excludeID)
	}
	return false, nil
}
func (f *fakeProviderRepo) UpdateResponseTime(id uuid.UUID, ms int) error {
	if f.updateResponseTimeFunc != nil {
		return f.updateResponseTimeFunc(id, ms)
	}
	return nil
}

// fakeModelRepo 模拟 ModelRepoInterface 用于 ProviderService 单测（仅需部分方法）
type fakeModelRepo struct {
	countByProviderIDFunc      func(providerID uuid.UUID) (int64, error)
	batchCountByProviderIDFunc func(providerIDs []uuid.UUID) (map[uuid.UUID]int64, error)
	updateEnabledByProvFunc    func(providerID uuid.UUID, enabled bool) error
}

func (f *fakeModelRepo) Create(_ *model.Model) error                        { return nil }
func (f *fakeModelRepo) GetByID(_ uuid.UUID) (*model.Model, error)          { return nil, nil }
func (f *fakeModelRepo) GetByIDs(_ []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}
func (f *fakeModelRepo) List(_ repository.ModelListFilter, _, _ int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (f *fakeModelRepo) Update(_ *model.Model) error { return nil }
func (f *fakeModelRepo) Delete(_ uuid.UUID) error    { return nil }
func (f *fakeModelRepo) AutoDisableModel(_ uuid.UUID) error { return nil }
func (f *fakeModelRepo) AutoEnableModel(_ uuid.UUID) error  { return nil }
func (f *fakeModelRepo) UpdateEnabled(_ uuid.UUID, _ *bool) error { return nil }
func (f *fakeModelRepo) UpdateStatus(_ uuid.UUID, _ bool, _ int) error { return nil }
func (f *fakeModelRepo) IncrementModelFailures(_ uuid.UUID) (int, error) { return 0, nil }
func (f *fakeModelRepo) ResetModelFailures(_ uuid.UUID) error { return nil }
func (f *fakeModelRepo) UpdateModelCheckResult(_ uuid.UUID, _ int, _ string) error { return nil }
func (f *fakeModelRepo) IncrementRecoverySuccesses(_ uuid.UUID) error { return nil }
func (f *fakeModelRepo) ResetRecoverySuccesses(_ uuid.UUID) error { return nil }
func (f *fakeModelRepo) ExistsByName(_ string, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeModelRepo) ExistsByNameExcluding(_ string, _ uuid.UUID, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeModelRepo) CountByProviderID(providerID uuid.UUID) (int64, error) {
	if f.countByProviderIDFunc != nil {
		return f.countByProviderIDFunc(providerID)
	}
	return 0, nil
}
func (f *fakeModelRepo) BatchCountByProviderID(providerIDs []uuid.UUID) (map[uuid.UUID]int64, error) {
	if f.batchCountByProviderIDFunc != nil {
		return f.batchCountByProviderIDFunc(providerIDs)
	}
	return map[uuid.UUID]int64{}, nil
}
func (f *fakeModelRepo) ListAvailable() ([]model.Model, error) { return nil, nil }
func (f *fakeModelRepo) UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error {
	if f.updateEnabledByProvFunc != nil {
		return f.updateEnabledByProvFunc(providerID, enabled)
	}
	return nil
}
func (f *fakeModelRepo) BatchDelete(_ []uuid.UUID) (int64, error)          { return 0, nil }
func (f *fakeModelRepo) BatchUpdateEnabled(_ []uuid.UUID, _ bool) (int64, error) { return 0, nil }
func (f *fakeModelRepo) DeleteModelTagRelations(_ uuid.UUID) error         { return nil }
func (f *fakeModelRepo) GetHealthByNames(_ []string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}

func TestCreateProvider_Scenarios(t *testing.T) {
	tests := []struct {
		name        string
		description string
		input       service.CreateProviderInput
		setupRepo   func(pr *fakeProviderRepo)
		wantErr     error
	}{
		{
			name:        "成功创建",
			description: "名称不重复时应正常创建 provider 并返回实体",
			input:       service.CreateProviderInput{Name: "test-provider", DefinitionID: "openai", Endpoint: "http://api.test.com", Credentials: `{"api_key":"sk-123"}`},
			setupRepo:   func(pr *fakeProviderRepo) {},
			wantErr:     nil,
		},
		{
			name:        "名称重复返回ErrProviderNameExists",
			description: "当渠道名已存在时，应返回 ErrProviderNameExists",
			input:       service.CreateProviderInput{Name: "dup-provider", DefinitionID: "openai", Endpoint: "http://dup.test.com", Credentials: `{"api_key":"sk-456"}`},
			setupRepo: func(pr *fakeProviderRepo) {
				pr.existsByNameFunc = func(name string) (bool, error) { return true, nil }
			},
			wantErr: service.ErrProviderNameExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &fakeProviderRepo{}
			tt.setupRepo(pr)
			mr := &fakeModelRepo{}
			svc := service.NewProviderService(pr, mr, nil)

			result, err := svc.CreateProvider(tt.input)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.input.Name, result.Name)
			}
		})
	}
}

func TestGetProvider_Scenarios(t *testing.T) {
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		id          uuid.UUID
		setupRepo   func(pr *fakeProviderRepo)
		wantErr     error
	}{
		{
			name:        "找到返回Provider",
			description: "按 ID 查到 provider 时应正常返回 ProviderResponse",
			id:          providerID,
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(id uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Name: "found-provider", Enabled: true}, nil
				}
			},
			wantErr: nil,
		},
		{
			name:        "未找到返回ErrProviderNotFound",
			description: "按 ID 未查到时应返回 ErrProviderNotFound",
			id:          uuid.New(),
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(id uuid.UUID) (*model.Provider, error) {
					return nil, errors.New("record not found")
				}
			},
			wantErr: service.ErrProviderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &fakeProviderRepo{}
			tt.setupRepo(pr)
			mr := &fakeModelRepo{}
			svc := service.NewProviderService(pr, mr, nil)

			resp, err := svc.GetProvider(tt.id)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

func TestListProviders_Scenarios(t *testing.T) {
	tests := []struct {
		name        string
		description string
		setupRepo   func(pr *fakeProviderRepo, mr *fakeModelRepo)
		wantTotal   int64
		wantLen     int
	}{
		{
			name:        "返回分页列表",
			description: "仓库有数据时应返回对应列表和总数",
			setupRepo: func(pr *fakeProviderRepo, mr *fakeModelRepo) {
				pr.listFunc = func(_ repository.ProviderListFilter, _, _ int) ([]model.Provider, int64, error) {
					return []model.Provider{{ID: uuid.New(), Name: "p1"}, {ID: uuid.New(), Name: "p2"}}, 2, nil
				}
				mr.batchCountByProviderIDFunc = func(_ []uuid.UUID) (map[uuid.UUID]int64, error) {
					return map[uuid.UUID]int64{}, nil
				}
			},
			wantTotal: 2,
			wantLen:   2,
		},
		{
			name:        "空列表返回空",
			description: "仓库无数据时应返回空列表和 total=0",
			setupRepo: func(pr *fakeProviderRepo, mr *fakeModelRepo) {
				pr.listFunc = func(_ repository.ProviderListFilter, _, _ int) ([]model.Provider, int64, error) {
					return []model.Provider{}, 0, nil
				}
				mr.batchCountByProviderIDFunc = func(_ []uuid.UUID) (map[uuid.UUID]int64, error) {
					return map[uuid.UUID]int64{}, nil
				}
			},
			wantTotal: 0,
			wantLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &fakeProviderRepo{}
			mr := &fakeModelRepo{}
			tt.setupRepo(pr, mr)
			svc := service.NewProviderService(pr, mr, nil)

			list, total, err := svc.ListProviders(repository.ProviderListFilter{}, 1, 20)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			assert.Len(t, list, tt.wantLen)
		})
	}
}

func TestUpdateProvider_Scenarios(t *testing.T) {
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		setupRepo   func(pr *fakeProviderRepo)
		input       service.UpdateProviderInput
		wantErr     error
	}{
		{
			name:        "更新名称不重复",
			description: "更新渠道名称（不与其他重复）应成功",
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Name: "old-name"}, nil
				}
				pr.existsByNameExclFunc = func(_ string, _ uuid.UUID) (bool, error) { return false, nil }
			},
			input:   service.UpdateProviderInput{Name: "new-name"},
			wantErr: nil,
		},
		{
			name:        "更新名称重复返回错误",
			description: "更新渠道名称与其他渠道重名时应返回 ErrProviderNameExists",
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Name: "old-name"}, nil
				}
				pr.existsByNameExclFunc = func(_ string, _ uuid.UUID) (bool, error) { return true, nil }
			},
			input:   service.UpdateProviderInput{Name: "dup-name"},
			wantErr: service.ErrProviderNameExists,
		},
		{
			name:        "provider不存在返回错误",
			description: "更新时渠道不存在应返回 ErrProviderNotFound",
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return nil, errors.New("not found")
				}
			},
			input:   service.UpdateProviderInput{Name: "any"},
			wantErr: service.ErrProviderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &fakeProviderRepo{}
			tt.setupRepo(pr)
			mr := &fakeModelRepo{}
			svc := service.NewProviderService(pr, mr, nil)

			resp, err := svc.UpdateProvider(providerID, tt.input)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}

func TestToggleProviderStatus_Scenarios(t *testing.T) {
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		enabled     bool
		setupRepo   func(pr *fakeProviderRepo)
		wantErr     error
	}{
		{
			name:        "启用→禁用",
			description: "将已启用的渠道设为禁用，应成功",
			enabled:     false,
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr: nil,
		},
		{
			name:        "禁用→启用",
			description: "将已禁用的渠道设为启用，应成功",
			enabled:     true,
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: false}, nil
				}
			},
			wantErr: nil,
		},
		{
			name:        "provider不存在",
			description: "渠道不存在时应返回 ErrProviderNotFound",
			enabled:     true,
			setupRepo: func(pr *fakeProviderRepo) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return nil, errors.New("not found")
				}
			},
			wantErr: service.ErrProviderNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr := &fakeProviderRepo{}
			tt.setupRepo(pr)
			mr := &fakeModelRepo{}
			svc := service.NewProviderService(pr, mr, nil)

			resp, err := svc.ToggleProviderStatus(providerID, tt.enabled)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, resp)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, resp)
			}
		})
	}
}
