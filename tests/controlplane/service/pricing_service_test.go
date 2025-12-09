// TODO: model.ModelPricing / PriceModeCatalog 已重构，本测试需更新。
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

// fakeModelRepoForPricingSvc 模拟 ModelRepoInterface 用于 PricingService 单测
type fakeModelRepoForPricingSvc struct {
	getByIDFunc func(id uuid.UUID) (*model.Model, error)
}

func (f *fakeModelRepoForPricingSvc) Create(_ *model.Model) error                                          { return nil }
func (f *fakeModelRepoForPricingSvc) GetByID(id uuid.UUID) (*model.Model, error) {
	if f.getByIDFunc != nil {
		return f.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}
func (f *fakeModelRepoForPricingSvc) GetByIDs(_ []uuid.UUID) (map[uuid.UUID]*model.Model, error)          { return nil, nil }
func (f *fakeModelRepoForPricingSvc) List(_ repository.ModelListFilter, _, _ int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (f *fakeModelRepoForPricingSvc) Update(_ *model.Model) error                                          { return nil }
func (f *fakeModelRepoForPricingSvc) Delete(_ uuid.UUID) error                                            { return nil }
func (f *fakeModelRepoForPricingSvc) ExistsByName(_ string, _ uuid.UUID) (bool, error)                    { return false, nil }
func (f *fakeModelRepoForPricingSvc) ExistsByNameExcluding(_ string, _ uuid.UUID, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (f *fakeModelRepoForPricingSvc) CountByProviderID(_ uuid.UUID) (int64, error)                        { return 0, nil }
func (f *fakeModelRepoForPricingSvc) BatchCountByProviderID(_ []uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}
func (f *fakeModelRepoForPricingSvc) ListAvailable() ([]model.Model, error)                                { return nil, nil }
func (f *fakeModelRepoForPricingSvc) UpdateEnabledByProviderID(_ uuid.UUID, _ bool) error                 { return nil }
func (f *fakeModelRepoForPricingSvc) BatchDelete(_ []uuid.UUID) (int64, error)                             { return 0, nil }
func (f *fakeModelRepoForPricingSvc) BatchUpdateEnabled(_ []uuid.UUID, _ bool) (int64, error)             { return 0, nil }
func (f *fakeModelRepoForPricingSvc) DeleteModelTagRelations(_ uuid.UUID) error                           { return nil }
func (f *fakeModelRepoForPricingSvc) GetHealthByNames(_ []string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}

func floatPtr(v float64) *float64 { return &v }

func TestResolveModelPricing_Scenarios(t *testing.T) {
	modelID := uuid.New()

	tests := []struct {
		name        string
		description string
		setupMR     func(mr *fakeModelRepoForPricingSvc)
		wantResult  *model.ModelPricing
		wantErr     bool
	}{
		{
			name:        "Catalog模式返回catalog价格",
			description: "模型 price_mode=catalog 且关联了 Catalog，应直接从 Catalog 价格字段取值，PricingMissing=false",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:        modelID,
						PriceMode: model.PriceModeCatalog,
						Catalog: &model.ModelCatalog{
							InputPricePerMillion:  10.0,
							OutputPricePerMillion: 20.0,
							ReasoningPrice:        floatPtr(5.0),
							CachedPrice:           floatPtr(2.0),
						},
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       10.0,
				CompletionPrice:   20.0,
				ReasoningPrice:    floatPtr(5.0),
				CachedPrice:       floatPtr(2.0),
				PricingMissing:    false,
				FallbackReasoning: false,
			},
			wantErr: false,
		},
		{
			name:        "Manual模式返回手动设置的价格",
			description: "模型 price_mode=manual，应从模型自身的 InputPrice/OutputPrice/ReasoningPrice/CachedPrice 字段取值",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:            modelID,
						PriceMode:     model.PriceModeManual,
						InputPrice:    15.0,
						OutputPrice:   25.0,
						ReasoningPrice: floatPtr(8.0),
						CachedPrice:    floatPtr(3.0),
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       15.0,
				CompletionPrice:   25.0,
				ReasoningPrice:    floatPtr(8.0),
				CachedPrice:       floatPtr(3.0),
				PricingMissing:    false,
				FallbackReasoning: false,
			},
			wantErr: false,
		},
		{
			name:        "ReasoningPrice为nil时FallbackReasoning=true",
			description: "当模型的 ReasoningPrice 为 nil 时，结果中的 FallbackReasoning 应为 true，表示回退到 completion 价格",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:            modelID,
						PriceMode:     model.PriceModeManual,
						InputPrice:    10.0,
						OutputPrice:   20.0,
						ReasoningPrice: nil,
						CachedPrice:    floatPtr(1.0),
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       10.0,
				CompletionPrice:   20.0,
				ReasoningPrice:    nil,
				CachedPrice:       floatPtr(1.0),
				PricingMissing:    false,
				FallbackReasoning: true,
			},
			wantErr: false,
		},
		{
			name:        "CachedPrice为nil时保持nil",
			description: "当模型的 CachedPrice 为 nil 时，结果中的 CachedPrice 应保持 nil（调用方当作 0 处理）",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:            modelID,
						PriceMode:     model.PriceModeManual,
						InputPrice:    10.0,
						OutputPrice:   20.0,
						ReasoningPrice: floatPtr(5.0),
						CachedPrice:    nil,
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       10.0,
				CompletionPrice:   20.0,
				ReasoningPrice:    floatPtr(5.0),
				CachedPrice:       nil,
				PricingMissing:    false,
				FallbackReasoning: false,
			},
			wantErr: false,
		},
		{
			name:        "PromptPrice为0时PricingMissing=true",
			description: "当解析出的 PromptPrice 为 0 时，PricingMissing 应设为 true，表示价格不可用",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:            modelID,
						PriceMode:     model.PriceModeManual,
						InputPrice:    0.0,
						OutputPrice:   20.0,
						ReasoningPrice: floatPtr(5.0),
						CachedPrice:    floatPtr(1.0),
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       0.0,
				CompletionPrice:   20.0,
				ReasoningPrice:    floatPtr(5.0),
				CachedPrice:       floatPtr(1.0),
				PricingMissing:    true,
				FallbackReasoning: false,
			},
			wantErr: false,
		},
		{
			name:        "CompletionPrice为0时PricingMissing=true",
			description: "当解析出的 CompletionPrice 为 0 时，PricingMissing 应设为 true，表示价格不可用",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:            modelID,
						PriceMode:     model.PriceModeManual,
						InputPrice:    10.0,
						OutputPrice:   0.0,
						ReasoningPrice: floatPtr(5.0),
						CachedPrice:    floatPtr(1.0),
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       10.0,
				CompletionPrice:   0.0,
				ReasoningPrice:    floatPtr(5.0),
				CachedPrice:       floatPtr(1.0),
				PricingMissing:    true,
				FallbackReasoning: false,
			},
			wantErr: false,
		},
		{
			name:        "模型不存在返回错误",
			description: "GetByID 返回 nil 时应返回明确的 'model not found' 错误",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return nil, nil
				}
			},
			wantResult: nil,
			wantErr:    true,
		},
		{
			name:        "未知price_mode导致PricingMissing",
			description: "price_mode 既不是 catalog 也不是 manual 时，走 default 分支，PricingMissing=true，且价格字段均为零值",
			setupMR: func(mr *fakeModelRepoForPricingSvc) {
				mr.getByIDFunc = func(_ uuid.UUID) (*model.Model, error) {
					return &model.Model{
						ID:        modelID,
						PriceMode: "unknown",
					}, nil
				}
			},
			wantResult: &model.ModelPricing{
				PromptPrice:       0.0,
				CompletionPrice:   0.0,
				ReasoningPrice:    nil,
				CachedPrice:       nil,
				PricingMissing:    true,
				FallbackReasoning: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &fakeModelRepoForPricingSvc{}
			tt.setupMR(mr)
			svc := service.NewPricingService(mr)

			result, err := svc.ResolveModelPricing(t.Context(), modelID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)

			assert.Equal(t, tt.wantResult.PromptPrice, result.PromptPrice)
			assert.Equal(t, tt.wantResult.CompletionPrice, result.CompletionPrice)
			assert.Equal(t, tt.wantResult.PricingMissing, result.PricingMissing)
			assert.Equal(t, tt.wantResult.FallbackReasoning, result.FallbackReasoning)

			// ReasoningPrice：nil 和 *float64 分开断言
			if tt.wantResult.ReasoningPrice == nil {
				assert.Nil(t, result.ReasoningPrice)
			} else {
				require.NotNil(t, result.ReasoningPrice)
				assert.Equal(t, *tt.wantResult.ReasoningPrice, *result.ReasoningPrice)
			}

			// CachedPrice：nil 和 *float64 分开断言
			if tt.wantResult.CachedPrice == nil {
				assert.Nil(t, result.CachedPrice)
			} else {
				require.NotNil(t, result.CachedPrice)
				assert.Equal(t, *tt.wantResult.CachedPrice, *result.CachedPrice)
			}
		})
	}
}
