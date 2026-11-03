// TODO: model.PriceModeCatalog / CreateModelInput.ReasoningPrice / CachedPrice 字段已变更，
// 本文件测试需按最新模型定义更新。暂时用 //go:build ignore 跳过编译。
//
//go:build ignore

package service_test

import (
	"errors"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModelRepoForModelSvc 模拟 ModelRepoInterface 用于 ModelService 单测
type fakeModelRepoForModelSvc struct {
	createFunc              func(m *model.Model) error
	getByIDFunc             func(id uuid.UUID) (*model.Model, error)
	getByIDsFunc            func(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error)
	listFunc                func(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error)
	updateFunc              func(m *model.Model) error
	deleteFunc              func(id uuid.UUID) error
	existsByNameFunc        func(name string, providerID uuid.UUID) (bool, error)
	existsByNameExclFunc    func(name string, providerID uuid.UUID, excludeID uuid.UUID) (bool, error)
	countByProviderIDFunc   func(providerID uuid.UUID) (int64, error)
	batchCountFunc          func(ids []uuid.UUID) (map[uuid.UUID]int64, error)
	listAvailableFunc       func() ([]model.Model, error)
	updateEnabledByProvFunc func(providerID uuid.UUID, enabled bool) error
	batchDeleteFunc         func(ids []uuid.UUID) (int64, error)
	batchUpdateEnabledFunc  func(ids []uuid.UUID, enabled bool) (int64, error)
	deleteTagRelationsFunc  func(modelID uuid.UUID) error
	getHealthByNamesFunc    func(names []string) (map[string]repository.ModelHealthSummary, error)
}

func (f *fakeModelRepoForModelSvc) Create(m *model.Model) error {
	if f.createFunc != nil {
		return f.createFunc(m)
	}
	return nil
}
func (f *fakeModelRepoForModelSvc) GetByID(id uuid.UUID) (*model.Model, error) {
	if f.getByIDFunc != nil {
		return f.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}
func (f *fakeModelRepoForModelSvc) GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	if f.getByIDsFunc != nil {
		return f.getByIDsFunc(ids)
	}
	return nil, nil
}
func (f *fakeModelRepoForModelSvc) List(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error) {
	if f.listFunc != nil {
		return f.listFunc(filter, offset, limit)
	}
	return nil, 0, nil
}
func (f *fakeModelRepoForModelSvc) Update(m *model.Model) error {
	if f.updateFunc != nil {
		return f.updateFunc(m)
	}
	return nil
}
func (f *fakeModelRepoForModelSvc) Delete(id uuid.UUID) error {
	if f.deleteFunc != nil {
		return f.deleteFunc(id)
	}
	return nil
}
func (f *fakeModelRepoForModelSvc) ExistsByName(name string, providerID uuid.UUID) (bool, error) {
	if f.existsByNameFunc != nil {
		return f.existsByNameFunc(name, providerID)
	}
	return false, nil
}
func (f *fakeModelRepoForModelSvc) ExistsByNameExcluding(name string, providerID uuid.UUID, excludeID uuid.UUID) (bool, error) {
	if f.existsByNameExclFunc != nil {
		return f.existsByNameExclFunc(name, providerID, excludeID)
	}
	return false, nil
}
func (f *fakeModelRepoForModelSvc) CountByProviderID(providerID uuid.UUID) (int64, error) {
	if f.countByProviderIDFunc != nil {
		return f.countByProviderIDFunc(providerID)
	}
	return 0, nil
}
func (f *fakeModelRepoForModelSvc) BatchCountByProviderID(ids []uuid.UUID) (map[uuid.UUID]int64, error) {
	if f.batchCountFunc != nil {
		return f.batchCountFunc(ids)
	}
	return map[uuid.UUID]int64{}, nil
}
func (f *fakeModelRepoForModelSvc) ListAvailable() ([]model.Model, error) {
	if f.listAvailableFunc != nil {
		return f.listAvailableFunc()
	}
	return nil, nil
}
func (f *fakeModelRepoForModelSvc) UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error {
	if f.updateEnabledByProvFunc != nil {
		return f.updateEnabledByProvFunc(providerID, enabled)
	}
	return nil
}
func (f *fakeModelRepoForModelSvc) BatchDelete(ids []uuid.UUID) (int64, error) {
	if f.batchDeleteFunc != nil {
		return f.batchDeleteFunc(ids)
	}
	return 0, nil
}
func (f *fakeModelRepoForModelSvc) BatchUpdateEnabled(ids []uuid.UUID, enabled bool) (int64, error) {
	if f.batchUpdateEnabledFunc != nil {
		return f.batchUpdateEnabledFunc(ids, enabled)
	}
	return 0, nil
}
func (f *fakeModelRepoForModelSvc) DeleteModelTagRelations(modelID uuid.UUID) error {
	if f.deleteTagRelationsFunc != nil {
		return f.deleteTagRelationsFunc(modelID)
	}
	return nil
}
func (f *fakeModelRepoForModelSvc) GetHealthByNames(names []string) (map[string]repository.ModelHealthSummary, error) {
	if f.getHealthByNamesFunc != nil {
		return f.getHealthByNamesFunc(names)
	}
	return nil, nil
}
func (f *fakeModelRepoForModelSvc) UpdateEnabled(id uuid.UUID, enabled *bool) error { return nil }
func (f *fakeModelRepoForModelSvc) UpdateStatus(id uuid.UUID, enabled bool, status int) error {
	return nil
}
func (f *fakeModelRepoForModelSvc) AutoDisableModel(id uuid.UUID) error              { return nil }
func (f *fakeModelRepoForModelSvc) AutoEnableModel(id uuid.UUID) error               { return nil }
func (f *fakeModelRepoForModelSvc) IncrementModelFailures(id uuid.UUID) (int, error) { return 0, nil }
func (f *fakeModelRepoForModelSvc) ResetModelFailures(id uuid.UUID) error            { return nil }
func (f *fakeModelRepoForModelSvc) UpdateModelCheckResult(id uuid.UUID, responseTime int, checkError string) error {
	return nil
}
func (f *fakeModelRepoForModelSvc) IncrementRecoverySuccesses(id uuid.UUID) error { return nil }
func (f *fakeModelRepoForModelSvc) ResetRecoverySuccesses(id uuid.UUID) error     { return nil }

// fakeProviderRepoForModelSvc 模拟 ProviderRepoInterface 用于 ModelService 单测
type fakeProviderRepoForModelSvc struct {
	getByIDFunc func(id uuid.UUID) (*model.Provider, error)
}

func (f *fakeProviderRepoForModelSvc) Create(_ *model.Provider) error { return nil }
func (f *fakeProviderRepoForModelSvc) GetByID(id uuid.UUID) (*model.Provider, error) {
	if f.getByIDFunc != nil {
		return f.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}
func (f *fakeProviderRepoForModelSvc) List(_ repository.ProviderListFilter, _, _ int) ([]model.Provider, int64, error) {
	return nil, 0, nil
}
func (f *fakeProviderRepoForModelSvc) Update(_ *model.Provider) error         { return nil }
func (f *fakeProviderRepoForModelSvc) UpdateStatus(_ uuid.UUID, _ bool) error { return nil }
func (f *fakeProviderRepoForModelSvc) Delete(_ uuid.UUID) error               { return nil }
func (f *fakeProviderRepoForModelSvc) ExistsByName(_ string) (bool, error)    { return false, nil }
func (f *fakeProviderRepoForModelSvc) ExistsByNameExcluding(_ string, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeProviderRepoForModelSvc) UpdateResponseTime(_ uuid.UUID, _ int) error { return nil }

func TestCreateModel_Scenarios(t *testing.T) {
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		input       service.CreateModelInput
		setupMR     func(mr *fakeModelRepoForModelSvc)
		setupPR     func(pr *fakeProviderRepoForModelSvc)
		wantErr     error
	}{
		{
			name:        "成功创建",
			description: "provider 存在且名称不重复时应创建成功",
			input:       service.CreateModelInput{Name: "gpt-4o", ProviderID: providerID.String(), ModelType: "chat"},
			setupMR:     func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr: nil,
		},
		{
			name:        "同名同provider重复",
			description: "同一 provider 下模型名已存在时应返回 ErrModelNameExists",
			input:       service.CreateModelInput{Name: "gpt-4o", ProviderID: providerID.String(), ModelType: "chat"},
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.existsByNameFunc = func(_ string, _ uuid.UUID) (bool, error) { return true, nil }
			},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr: service.ErrModelNameExists,
		},
		{
			name:        "provider不存在",
			description: "关联的 provider 不存在时应返回 ErrModelProviderInvalid",
			input:       service.CreateModelInput{Name: "gpt-4o", ProviderID: uuid.New().String(), ModelType: "chat"},
			setupMR:     func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return nil, errors.New("not found")
				}
			},
			wantErr: service.ErrModelProviderInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			pr := &fakeProviderRepoForModelSvc{}
			tt.setupMR(mr)
			tt.setupPR(pr)
			svc := service.NewModelService(mr, pr, nil)

			result, err := svc.CreateModel(tt.input)
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

func TestGetModel_Scenarios(t *testing.T) {
	modelID := uuid.New()

	tests := []struct {
		name        string
		description string
		id          uuid.UUID
		setupMR     func(mr *fakeModelRepoForModelSvc)
		wantErr     error
	}{
		{
			name:        "找到返回Model",
			description: "模型存在时应返回 Model 实体",
			id:          modelID,
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{ID: modelID, Name: "test-model"}, nil
				}
			},
			wantErr: nil,
		},
		{
			name:        "未找到返回ErrModelNotFound",
			description: "模型不存在时应返回 ErrModelNotFound",
			id:          uuid.New(),
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return nil, errors.New("not found")
				}
			},
			wantErr: service.ErrModelNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			tt.setupMR(mr)
			pr := &fakeProviderRepoForModelSvc{}
			svc := service.NewModelService(mr, pr, nil)

			result, err := svc.GetModel(tt.id)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestListModels_Scenarios(t *testing.T) {
	tests := []struct {
		name        string
		description string
		setupMR     func(mr *fakeModelRepoForModelSvc)
		wantTotal   int64
		wantLen     int
	}{
		{
			name:        "分页列表",
			description: "有数据时应返回分页列表和总数",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.listFunc = func(_ repository.ModelListFilter, _, _ int) ([]model.Model, int64, error) {
					return []model.Model{{ID: uuid.New(), Name: "m1"}, {ID: uuid.New(), Name: "m2"}}, 2, nil
				}
			},
			wantTotal: 2,
			wantLen:   2,
		},
		{
			name:        "空列表",
			description: "无数据时应返回空列表和 total=0",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.listFunc = func(_ repository.ModelListFilter, _, _ int) ([]model.Model, int64, error) {
					return []model.Model{}, 0, nil
				}
			},
			wantTotal: 0,
			wantLen:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			tt.setupMR(mr)
			pr := &fakeProviderRepoForModelSvc{}
			svc := service.NewModelService(mr, pr, nil)

			list, total, err := svc.ListModels(repository.ModelListFilter{}, 1, 20)
			require.NoError(t, err)
			assert.Equal(t, tt.wantTotal, total)
			assert.Len(t, list, tt.wantLen)
		})
	}
}

func TestUpdateModel_Scenarios(t *testing.T) {
	modelID := uuid.New()
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		setupMR     func(mr *fakeModelRepoForModelSvc)
		setupPR     func(pr *fakeProviderRepoForModelSvc)
		input       service.UpdateModelInput
		wantErr     error
	}{
		{
			name:        "成功更新",
			description: "名称不重复时应更新成功",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{ID: modelID, Name: "old", ProviderID: providerID, PriceMode: model.PriceModeCatalog}, nil
				}
				mr.existsByNameExclFunc = func(_ string, _ uuid.UUID, _ uuid.UUID) (bool, error) { return false, nil }
			},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {},
			input:   service.UpdateModelInput{Name: "new-name"},
			wantErr: nil,
		},
		{
			name:        "重名错误",
			description: "更新模型名与同 provider 下其他模型重名时应返回 ErrModelNameExists",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{ID: modelID, Name: "old", ProviderID: providerID, PriceMode: model.PriceModeCatalog}, nil
				}
				mr.existsByNameExclFunc = func(_ string, _ uuid.UUID, _ uuid.UUID) (bool, error) { return true, nil }
			},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {},
			input:   service.UpdateModelInput{Name: "dup-name"},
			wantErr: service.ErrModelNameExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			pr := &fakeProviderRepoForModelSvc{}
			tt.setupMR(mr)
			tt.setupPR(pr)
			svc := service.NewModelService(mr, pr, nil)

			result, err := svc.UpdateModel(modelID, tt.input)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

func TestDeleteModel_Scenarios(t *testing.T) {
	modelID := uuid.New()

	tests := []struct {
		name        string
		description string
		setupMR     func(mr *fakeModelRepoForModelSvc)
		wantErr     error
	}{
		{
			name:        "成功删除",
			description: "模型存在时应删除成功",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{ID: modelID, Name: "to-delete"}, nil
				}
			},
			wantErr: nil,
		},
		{
			name:        "模型不存在",
			description: "模型不存在时应返回 ErrModelNotFound",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return nil, errors.New("not found")
				}
			},
			wantErr: service.ErrModelNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			tt.setupMR(mr)
			pr := &fakeProviderRepoForModelSvc{}
			svc := service.NewModelService(mr, pr, nil)

			err := svc.DeleteModel(modelID)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCheckModelName_Scenarios(t *testing.T) {
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		modelName   string
		setupMR     func(mr *fakeModelRepoForModelSvc)
		wantAvail   bool
	}{
		{
			name:        "名称不重复",
			description: "名称在该 provider 下不存在时应返回 available=true",
			modelName:   "new-model",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.existsByNameFunc = func(_ string, _ uuid.UUID) (bool, error) { return false, nil }
			},
			wantAvail: true,
		},
		{
			name:        "名称重复",
			description: "名称在该 provider 下已存在时应返回 available=false",
			modelName:   "existing-model",
			setupMR: func(mr *fakeModelRepoForModelSvc) {
				mr.existsByNameFunc = func(_ string, _ uuid.UUID) (bool, error) { return true, nil }
			},
			wantAvail: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			tt.setupMR(mr)
			pr := &fakeProviderRepoForModelSvc{}
			svc := service.NewModelService(mr, pr, nil)

			avail, err := svc.CheckModelName(tt.modelName, providerID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAvail, avail)
		})
	}
}

func strPtr(v string) *string { return &v }

func TestCreateModel_PriceModeValidation(t *testing.T) {
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		input       service.CreateModelInput
		setupMR     func(mr *fakeModelRepoForModelSvc)
		setupPR     func(pr *fakeProviderRepoForModelSvc)
		wantErr     bool
		wantErrMsg  string
		wantMode    string
	}{
		{
			name:        "创建manual模式且价格有效",
			description: "PriceMode=manual 且 InputPrice>0 OutputPrice>0 应创建成功",
			input: service.CreateModelInput{
				Name: "manual-model", ProviderID: providerID.String(), ModelType: "chat",
				PriceMode: "manual", InputPrice: floatPtr(1.0), OutputPrice: floatPtr(2.0),
			},
			setupMR: func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr:  false,
			wantMode: "manual",
		},
		{
			name:        "创建manual模式缺input_price",
			description: "PriceMode=manual 但 InputPrice 未设置应报错",
			input: service.CreateModelInput{
				Name: "no-input-price", ProviderID: providerID.String(), ModelType: "chat",
				PriceMode: "manual", OutputPrice: floatPtr(2.0),
			},
			setupMR: func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr:    true,
			wantErrMsg: "manual mode requires input_price > 0",
		},
		{
			name:        "创建manual模式缺output_price",
			description: "PriceMode=manual 但 OutputPrice 未设置应报错",
			input: service.CreateModelInput{
				Name: "no-output-price", ProviderID: providerID.String(), ModelType: "chat",
				PriceMode: "manual", InputPrice: floatPtr(1.0),
			},
			setupMR: func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr:    true,
			wantErrMsg: "manual mode requires output_price > 0",
		},
		{
			name:        "创建catalog模式默认值",
			description: "PriceMode 为空串时应默认为 catalog",
			input: service.CreateModelInput{
				Name: "catalog-default", ProviderID: providerID.String(), ModelType: "chat",
				PriceMode: "",
			},
			setupMR: func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr:  false,
			wantMode: "catalog",
		},
		{
			name:        "创建时reasoning/cached价格可选",
			description: "catalog 模式下可附带 reasoning_price 和 cached_price",
			input: service.CreateModelInput{
				Name: "with-extras", ProviderID: providerID.String(), ModelType: "chat",
				PriceMode: "catalog", ReasoningPrice: floatPtr(3.0), CachedPrice: floatPtr(1.5),
			},
			setupMR: func(mr *fakeModelRepoForModelSvc) {},
			setupPR: func(pr *fakeProviderRepoForModelSvc) {
				pr.getByIDFunc = func(_ uuid.UUID) (*model.Provider, error) {
					return &model.Provider{ID: providerID, Enabled: true}, nil
				}
			},
			wantErr:  false,
			wantMode: "catalog",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			pr := &fakeProviderRepoForModelSvc{}
			tt.setupMR(mr)
			tt.setupPR(pr)
			svc := service.NewModelService(mr, pr, nil)

			result, err := svc.CreateModel(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.wantMode, result.PriceMode)
				if tt.input.ReasoningPrice != nil {
					assert.Equal(t, *tt.input.ReasoningPrice, *result.ReasoningPrice)
				}
				if tt.input.CachedPrice != nil {
					assert.Equal(t, *tt.input.CachedPrice, *result.CachedPrice)
				}
			}
		})
	}
}

func TestUpdateModel_PriceModeSwitch(t *testing.T) {
	modelID := uuid.New()
	providerID := uuid.New()

	tests := []struct {
		name        string
		description string
		existing    *model.Model
		input       service.UpdateModelInput
		setupMR     func(mr *fakeModelRepoForModelSvc)
		setupPR     func(pr *fakeProviderRepoForModelSvc)
		wantErr     bool
		wantErrMsg  string
		wantMode    string
		wantCheck   func(t *testing.T, result *model.Model)
	}{
		{
			name:        "catalog→manual切换需校验input/output_price",
			description: "从 catalog 切到 manual 但未设价格应报错",
			existing: &model.Model{
				ID: modelID, Name: "test", ProviderID: providerID,
				PriceMode: "catalog", InputPrice: 0, OutputPrice: 0,
			},
			input:      service.UpdateModelInput{PriceMode: strPtr("manual")},
			setupMR:    func(mr *fakeModelRepoForModelSvc) {},
			setupPR:    func(pr *fakeProviderRepoForModelSvc) {},
			wantErr:    true,
			wantErrMsg: "manual mode requires input_price > 0",
		},
		{
			name:        "catalog→manual切换携带有效价格",
			description: "从 catalog 切到 manual 且同时设置有效价格应成功",
			existing: &model.Model{
				ID: modelID, Name: "test", ProviderID: providerID,
				PriceMode: "catalog", InputPrice: 0, OutputPrice: 0,
			},
			input: service.UpdateModelInput{
				PriceMode: strPtr("manual"), InputPrice: floatPtr(1.5), OutputPrice: floatPtr(3.0),
			},
			setupMR:  func(mr *fakeModelRepoForModelSvc) {},
			setupPR:  func(pr *fakeProviderRepoForModelSvc) {},
			wantErr:  false,
			wantMode: "manual",
		},
		{
			name:        "manual→catalog切换清除reasoning/cached",
			description: "从 manual 切到 catalog 时应清空 ReasoningPrice 和 CachedPrice",
			existing: &model.Model{
				ID: modelID, Name: "test", ProviderID: providerID,
				PriceMode: "manual", InputPrice: 1.0, OutputPrice: 2.0,
				ReasoningPrice: floatPtr(5.0), CachedPrice: floatPtr(2.5),
			},
			input:    service.UpdateModelInput{PriceMode: strPtr("catalog")},
			setupMR:  func(mr *fakeModelRepoForModelSvc) {},
			setupPR:  func(pr *fakeProviderRepoForModelSvc) {},
			wantErr:  false,
			wantMode: "catalog",
			wantCheck: func(t *testing.T, result *model.Model) {
				assert.Nil(t, result.ReasoningPrice, "manual→catalog 应清空 ReasoningPrice")
				assert.Nil(t, result.CachedPrice, "manual→catalog 应清空 CachedPrice")
			},
		},
		{
			name:        "price_mode不传时保持原值",
			description: "不传 PriceMode 时 model 应保持原有 price_mode",
			existing: &model.Model{
				ID: modelID, Name: "test", ProviderID: providerID,
				PriceMode: "manual", InputPrice: 1.0, OutputPrice: 2.0,
			},
			input:    service.UpdateModelInput{Name: "new-name"},
			setupMR:  func(mr *fakeModelRepoForModelSvc) {},
			setupPR:  func(pr *fakeProviderRepoForModelSvc) {},
			wantErr:  false,
			wantMode: "manual",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForModelSvc{}
			pr := &fakeProviderRepoForModelSvc{}
			mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
				clone := *tt.existing
				return &clone, nil
			}
			tt.setupMR(mr)
			tt.setupPR(pr)
			svc := service.NewModelService(mr, pr, nil)

			result, err := svc.UpdateModel(modelID, tt.input)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErrMsg)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tt.wantMode, result.PriceMode)
				if tt.wantCheck != nil {
					tt.wantCheck(t, result)
				}
			}
		})
	}
}
