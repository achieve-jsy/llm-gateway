//go:build ignore

package handler_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeModelRepoForPricingHandler 模拟 ModelRepoInterface 用于 PricingHandler.GetAllPricing 单测。
//
// 仅 ListAvailable 和 GetByID 有实际行为，其余方法返回零值。
type fakeModelRepoForPricingHandler struct {
	listAvailableFunc func() ([]model.Model, error)
	getByIDFunc       func(id uuid.UUID) (*model.Model, error)
}

func (f *fakeModelRepoForPricingHandler) Create(_ *model.Model) error { return nil }

func (f *fakeModelRepoForPricingHandler) GetByID(id uuid.UUID) (*model.Model, error) {
	if f.getByIDFunc != nil {
		return f.getByIDFunc(id)
	}
	return nil, errors.New("not found")
}

func (f *fakeModelRepoForPricingHandler) GetByIDs(_ []uuid.UUID) (map[uuid.UUID]*model.Model, error) {
	return nil, nil
}

func (f *fakeModelRepoForPricingHandler) List(_ repository.ModelListFilter, _, _ int) ([]model.Model, int64, error) {
	return nil, 0, nil
}

func (f *fakeModelRepoForPricingHandler) Update(_ *model.Model) error { return nil }

func (f *fakeModelRepoForPricingHandler) Delete(_ uuid.UUID) error { return nil }

func (f *fakeModelRepoForPricingHandler) ExistsByName(_ string, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeModelRepoForPricingHandler) ExistsByNameExcluding(_ string, _ uuid.UUID, _ uuid.UUID) (bool, error) {
	return false, nil
}

func (f *fakeModelRepoForPricingHandler) CountByProviderID(_ uuid.UUID) (int64, error) { return 0, nil }

func (f *fakeModelRepoForPricingHandler) BatchCountByProviderID(_ []uuid.UUID) (map[uuid.UUID]int64, error) {
	return nil, nil
}

func (f *fakeModelRepoForPricingHandler) ListAvailable() ([]model.Model, error) {
	if f.listAvailableFunc != nil {
		return f.listAvailableFunc()
	}
	return nil, nil
}

func (f *fakeModelRepoForPricingHandler) UpdateEnabledByProviderID(_ uuid.UUID, _ bool) error {
	return nil
}

func (f *fakeModelRepoForPricingHandler) BatchDelete(_ []uuid.UUID) (int64, error) { return 0, nil }

func (f *fakeModelRepoForPricingHandler) BatchUpdateEnabled(_ []uuid.UUID, _ bool) (int64, error) {
	return 0, nil
}

func (f *fakeModelRepoForPricingHandler) DeleteModelTagRelations(_ uuid.UUID) error { return nil }

func (f *fakeModelRepoForPricingHandler) GetHealthByNames(_ []string) (map[string]repository.ModelHealthSummary, error) {
	return nil, nil
}

func floatPtr(v float64) *float64 { return &v }

// setupPricingHandlerTest 创建带 PricingHandler.GetAllPricing 路由的 Gin 引擎。
func setupPricingHandlerTest(fakeRepo *fakeModelRepoForPricingHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	pricingSvc := service.NewPricingService(fakeRepo)
	h := handler.NewPricingHandler(pricingSvc, fakeRepo)
	r := gin.New()
	r.GET("/internal/v1/pricing", h.GetAllPricing)
	return r
}

// TestGetAllPricing 覆盖 PricingHandler.GetAllPricing 的成功与错误路径。
func TestGetAllPricing(t *testing.T) {
	t.Run("正常返回全量价表", func(t *testing.T) {
		// 测试目的：ListAvailable 返回 2 个 catalog 模式的模型，应返回完整的价表 JSON
		// — model-a：无 reasoning/cached 价格，期望 FallbackReasoning=true
		// — model-b：有全套价格，期望精确匹配
		modelID1 := uuid.New()
		modelID2 := uuid.New()
		fakeRepo := &fakeModelRepoForPricingHandler{
			listAvailableFunc: func() ([]model.Model, error) {
				return []model.Model{
					{
						ID:        modelID1,
						Name:      "model-a",
						PriceMode: model.PriceModeCatalog,
						Catalog: &model.ModelCatalog{
							InputPricePerMillion:  0.15,
							OutputPricePerMillion: 0.6,
							ReasoningPrice:        nil,
							CachedPrice:           nil,
						},
					},
					{
						ID:        modelID2,
						Name:      "model-b",
						PriceMode: model.PriceModeCatalog,
						Catalog: &model.ModelCatalog{
							InputPricePerMillion:  1.0,
							OutputPricePerMillion: 2.0,
							ReasoningPrice:        floatPtr(3.0),
							CachedPrice:           floatPtr(0.5),
						},
					},
				}, nil
			},
			getByIDFunc: func(id uuid.UUID) (*model.Model, error) {
				switch id {
				case modelID1:
					return &model.Model{
						ID:        modelID1,
						Name:      "model-a",
						PriceMode: model.PriceModeCatalog,
						Catalog: &model.ModelCatalog{
							InputPricePerMillion:  0.15,
							OutputPricePerMillion: 0.6,
							ReasoningPrice:        nil,
							CachedPrice:           nil,
						},
					}, nil
				case modelID2:
					return &model.Model{
						ID:        modelID2,
						Name:      "model-b",
						PriceMode: model.PriceModeCatalog,
						Catalog: &model.ModelCatalog{
							InputPricePerMillion:  1.0,
							OutputPricePerMillion: 2.0,
							ReasoningPrice:        floatPtr(3.0),
							CachedPrice:           floatPtr(0.5),
						},
					}, nil
				default:
					return nil, errors.New("not found")
				}
			},
		}
		r := setupPricingHandlerTest(fakeRepo)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/internal/v1/pricing", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Models map[string]*model.ModelPricing `json:"models"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Contains(t, resp.Models, "model-a")
		require.Contains(t, resp.Models, "model-b")
		assert.Equal(t, 2, len(resp.Models))

		// model-a：无 reasoning/cached 价格，应触发 fallback
		ma := resp.Models["model-a"]
		assert.Equal(t, 0.15, ma.PromptPrice)
		assert.Equal(t, 0.6, ma.CompletionPrice)
		assert.Nil(t, ma.ReasoningPrice)
		assert.Nil(t, ma.CachedPrice)
		assert.False(t, ma.PricingMissing)
		assert.True(t, ma.FallbackReasoning)

		// model-b：全套价格
		mb := resp.Models["model-b"]
		assert.Equal(t, 1.0, mb.PromptPrice)
		assert.Equal(t, 2.0, mb.CompletionPrice)
		require.NotNil(t, mb.ReasoningPrice)
		assert.Equal(t, 3.0, *mb.ReasoningPrice)
		require.NotNil(t, mb.CachedPrice)
		assert.Equal(t, 0.5, *mb.CachedPrice)
		assert.False(t, mb.PricingMissing)
		assert.False(t, mb.FallbackReasoning)
	})

	t.Run("空模型列表", func(t *testing.T) {
		// 测试目的：ListAvailable 返回空切片而非 nil，应返回 200 和空的 models 映射
		fakeRepo := &fakeModelRepoForPricingHandler{
			listAvailableFunc: func() ([]model.Model, error) {
				return []model.Model{}, nil
			},
		}
		r := setupPricingHandlerTest(fakeRepo)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/internal/v1/pricing", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Models map[string]*model.ModelPricing `json:"models"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Empty(t, resp.Models)
	})

	t.Run("ListAvailable返回错误", func(t *testing.T) {
		// 测试目的：ListAvailable 返回错误时，handler 应返回 500 和 internal_error 错误响应
		fakeRepo := &fakeModelRepoForPricingHandler{
			listAvailableFunc: func() ([]model.Model, error) {
				return nil, errors.New("db connection failed")
			},
		}
		r := setupPricingHandlerTest(fakeRepo)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/internal/v1/pricing", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Code)

		var errResp struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "internal_error", errResp.Error.Type)
		assert.Contains(t, errResp.Error.Message, "db connection failed")
	})

	t.Run("ResolveModelPricing返回错误", func(t *testing.T) {
		// 测试目的：PricingService.ResolveModelPricing 内部 GetByID 返回错误时，
		// handler 应返回 500，错误中包含模型名和原始错误信息
		modelID := uuid.New()
		fakeRepo := &fakeModelRepoForPricingHandler{
			listAvailableFunc: func() ([]model.Model, error) {
				return []model.Model{
					{
						ID:        modelID,
						Name:      "model-error",
						PriceMode: model.PriceModeCatalog,
					},
				}, nil
			},
			getByIDFunc: func(_ uuid.UUID) (*model.Model, error) {
				return nil, errors.New("model not found in db")
			},
		}
		r := setupPricingHandlerTest(fakeRepo)

		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/internal/v1/pricing", nil)
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusInternalServerError, w.Code)

		var errResp struct {
			Error struct {
				Type    string `json:"type"`
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "internal_error", errResp.Error.Type)
		assert.Contains(t, errResp.Error.Message, "model-error")
		assert.Contains(t, errResp.Error.Message, "model not found in db")
	})
}
