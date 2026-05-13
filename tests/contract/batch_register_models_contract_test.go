package contract_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type batchRegisterModelsContract struct {
	BatchRegisterModels struct {
		Routes map[string]struct {
			Handler            string              `json:"handler"`
			SuccessStatus      int                 `json:"success_status"`
			ErrorStatuses      map[string][]string `json:"error_statuses"`
			RequestBodyFields  []string            `json:"request_body_fields"`
			RequiredFields     []string            `json:"required_fields"`
			ResponseFields     []string            `json:"response_fields"`
		} `json:"routes"`
		ResultItemFields []string `json:"result_item_fields"`
		StatusEnum       []string `json:"status_enum"`
	} `json:"batch_register_models"`
}

func loadBatchRegisterModelsContract(t *testing.T) batchRegisterModelsContract {
	t.Helper()
	raw, err := os.ReadFile("contracts.json")
	require.NoError(t, err)
	var doc batchRegisterModelsContract
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}

func TestBatchRegisterModelsContractDefinesRoutes(t *testing.T) {
	doc := loadBatchRegisterModelsContract(t)
	routes := doc.BatchRegisterModels.Routes
	require.Contains(t, routes, "POST /models/batch")
	require.Contains(t, routes, "POST /models/batch-from-catalog")

	batch := routes["POST /models/batch"]
	require.Equal(t, 200, batch.SuccessStatus)
	require.Equal(t, "ModelHandler.BatchCreateModels", batch.Handler)
	require.ElementsMatch(t, []string{"name", "provider_ids", "model_type"}, batch.RequiredFields)
	require.ElementsMatch(t,
		[]string{"success_count", "skip_count", "fail_count", "results"},
		batch.ResponseFields,
	)
	require.Contains(t, batch.ErrorStatuses["400"], "bad_request")

	fromCat := routes["POST /models/batch-from-catalog"]
	require.Equal(t, 200, fromCat.SuccessStatus)
	require.Contains(t, fromCat.ErrorStatuses["404"], "catalog_not_found")
	require.ElementsMatch(t, []string{"catalog_id", "provider_ids", "name"}, fromCat.RequiredFields)

	require.ElementsMatch(t,
		[]string{"provider_id", "status", "model_id", "reason"},
		doc.BatchRegisterModels.ResultItemFields,
	)
	require.ElementsMatch(t,
		[]string{"created", "skipped", "failed"},
		doc.BatchRegisterModels.StatusEnum,
	)
}

// ---- HTTP 接口契约：真实 Gin 路由 + fake service，断言状态码与响应字段 ----

type contractBatchModelSvc struct {
	batchRes        *service.BatchCreateResult
	batchErr        error
	batchCatalogRes *service.BatchCreateResult
	batchCatalogErr error
}

func (f *contractBatchModelSvc) CreateModel(service.CreateModelInput) (*model.Model, error) {
	return nil, nil
}
func (f *contractBatchModelSvc) ListModels(repository.ModelListFilter, int, int) ([]model.Model, int64, error) {
	return nil, 0, nil
}
func (f *contractBatchModelSvc) UpdateModel(uuid.UUID, service.UpdateModelInput) (*model.Model, error) {
	return nil, nil
}
func (f *contractBatchModelSvc) DeleteModel(uuid.UUID) error { return nil }
func (f *contractBatchModelSvc) CheckModelName(string, uuid.UUID) (bool, error) {
	return true, nil
}
func (f *contractBatchModelSvc) BatchDeleteModels([]uuid.UUID) (int64, error) { return 0, nil }
func (f *contractBatchModelSvc) BatchUpdateModelStatus([]uuid.UUID, bool) (int64, error) {
	return 0, nil
}
func (f *contractBatchModelSvc) BatchUpdateModelStatusWithOperator([]uuid.UUID, bool, *uuid.UUID, string) (int64, error) {
	return 0, nil
}
func (f *contractBatchModelSvc) CreateModelFromCatalog(uuid.UUID, uuid.UUID, string) (*model.Model, error) {
	return nil, nil
}
func (f *contractBatchModelSvc) ResetOverride(uuid.UUID, string) error { return nil }
func (f *contractBatchModelSvc) GetModel(uuid.UUID) (*model.Model, error) {
	return nil, nil
}
func (f *contractBatchModelSvc) BindCatalog(uuid.UUID, uuid.UUID, bool) (*model.Model, error) {
	return nil, nil
}
func (f *contractBatchModelSvc) UnbindCatalog(uuid.UUID) (*model.Model, error) { return nil, nil }
func (f *contractBatchModelSvc) BatchBindCatalog([]service.BatchBindInput) (*service.BatchBindResult, error) {
	return nil, nil
}
func (f *contractBatchModelSvc) BatchCreateModels(service.BatchCreateModelsInput) (*service.BatchCreateResult, error) {
	return f.batchRes, f.batchErr
}
func (f *contractBatchModelSvc) BatchCreateModelsFromCatalog(service.BatchCreateFromCatalogInput) (*service.BatchCreateResult, error) {
	return f.batchCatalogRes, f.batchCatalogErr
}

func newBatchRegisterRouter(svc handler.ModelServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := handler.NewModelHandler(svc)
	r := gin.New()
	r.POST("/api/v1/models/batch", h.BatchCreateModels)
	r.POST("/api/v1/models/batch-from-catalog", h.BatchCreateModelsFromCatalog)
	return r
}

func postJSON(router http.Handler, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

type batchCreateAPIResponse struct {
	Data struct {
		SuccessCount int `json:"success_count"`
		SkipCount    int `json:"skip_count"`
		FailCount    int `json:"fail_count"`
		Results      []struct {
			ProviderID string  `json:"provider_id"`
			Status     string  `json:"status"`
			ModelID    *string `json:"model_id"`
			Reason     string  `json:"reason"`
		} `json:"results"`
	} `json:"data"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

func TestBatchRegisterModelsAPI_BatchSuccessShape(t *testing.T) {
	doc := loadBatchRegisterModelsContract(t)
	contract := doc.BatchRegisterModels.Routes["POST /models/batch"]

	modelID := uuid.New().String()
	pOK, pSkip, pFail := uuid.New().String(), uuid.New().String(), uuid.New().String()
	svc := &contractBatchModelSvc{
		batchRes: &service.BatchCreateResult{
			SuccessCount: 1,
			SkipCount:    1,
			FailCount:    1,
			Results: []service.BatchCreateResultItem{
				{ProviderID: pOK, Status: "created", ModelID: &modelID},
				{ProviderID: pSkip, Status: "skipped", Reason: "model already exists"},
				{ProviderID: pFail, Status: "failed", Reason: "associated provider not found or disabled"},
			},
		},
	}
	router := newBatchRegisterRouter(svc)

	body := `{"name":"gpt-4o","provider_ids":["` + pOK + `","` + pSkip + `","` + pFail + `"],"model_type":"chat"}`
	w := postJSON(router, "/api/v1/models/batch", body)

	require.Equal(t, contract.SuccessStatus, w.Code)

	var resp batchCreateAPIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Data.SuccessCount)
	require.Equal(t, 1, resp.Data.SkipCount)
	require.Equal(t, 1, resp.Data.FailCount)
	require.Len(t, resp.Data.Results, 3)

	// 契约：response_fields 均存在
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	data, ok := raw["data"].(map[string]any)
	require.True(t, ok)
	for _, field := range contract.ResponseFields {
		_, exists := data[field]
		require.True(t, exists, "missing response field %s", field)
	}

	// 契约：status_enum
	statusSet := map[string]struct{}{}
	for _, s := range doc.BatchRegisterModels.StatusEnum {
		statusSet[s] = struct{}{}
	}
	for _, item := range resp.Data.Results {
		_, ok := statusSet[item.Status]
		require.True(t, ok, "unexpected status %s", item.Status)
		require.NotEmpty(t, item.ProviderID)
	}
	require.Equal(t, "created", resp.Data.Results[0].Status)
	require.NotNil(t, resp.Data.Results[0].ModelID)
	require.Equal(t, "skipped", resp.Data.Results[1].Status)
	require.Equal(t, "model already exists", resp.Data.Results[1].Reason)
	require.Equal(t, "failed", resp.Data.Results[2].Status)
}

func TestBatchRegisterModelsAPI_BatchBadRequest(t *testing.T) {
	doc := loadBatchRegisterModelsContract(t)
	contract := doc.BatchRegisterModels.Routes["POST /models/batch"]

	router := newBatchRegisterRouter(&contractBatchModelSvc{})
	w := postJSON(router, "/api/v1/models/batch", `{"name":"gpt-4o","model_type":"chat"}`)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var resp batchCreateAPIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, "invalid_request", resp.Error.Type)
	require.Contains(t, contract.ErrorStatuses["400"], resp.Error.Code)
}

func TestBatchRegisterModelsAPI_FromCatalogNotFound(t *testing.T) {
	doc := loadBatchRegisterModelsContract(t)
	contract := doc.BatchRegisterModels.Routes["POST /models/batch-from-catalog"]
	catID := uuid.New()

	svc := &contractBatchModelSvc{
		batchCatalogErr: catalogMissingAPIErr{id: catID},
	}
	router := newBatchRegisterRouter(svc)

	body := `{"catalog_id":"` + catID.String() + `","provider_ids":["` + uuid.New().String() + `"],"name":"gpt-4o"}`
	w := postJSON(router, "/api/v1/models/batch-from-catalog", body)

	require.Equal(t, http.StatusNotFound, w.Code)
	var resp batchCreateAPIResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Error)
	require.Equal(t, "not_found", resp.Error.Type)
	require.Contains(t, contract.ErrorStatuses["404"], resp.Error.Code)
}

func TestBatchRegisterModelsAPI_FromCatalogSuccessShape(t *testing.T) {
	doc := loadBatchRegisterModelsContract(t)
	contract := doc.BatchRegisterModels.Routes["POST /models/batch-from-catalog"]

	modelID := uuid.New().String()
	p1 := uuid.New().String()
	svc := &contractBatchModelSvc{
		batchCatalogRes: &service.BatchCreateResult{
			SuccessCount: 1,
			Results: []service.BatchCreateResultItem{
				{ProviderID: p1, Status: "created", ModelID: &modelID},
			},
		},
	}
	router := newBatchRegisterRouter(svc)

	body := `{"catalog_id":"` + uuid.New().String() + `","provider_ids":["` + p1 + `"],"name":"gpt-4o"}`
	w := postJSON(router, "/api/v1/models/batch-from-catalog", body)

	require.Equal(t, contract.SuccessStatus, w.Code)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	data, ok := raw["data"].(map[string]any)
	require.True(t, ok)
	for _, field := range contract.ResponseFields {
		_, exists := data[field]
		require.True(t, exists, "missing response field %s", field)
	}
}

type catalogMissingAPIErr struct{ id uuid.UUID }

func (e catalogMissingAPIErr) Error() string {
	return "catalog " + e.id.String() + " not found"
}
