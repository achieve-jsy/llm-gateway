package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

// MockModelTagRepo 标签仓储 mock，配合 ModelTagRepoInterface 注入。
type MockModelTagRepo struct {
	listTagsFn        func() ([]model.ModelTag, error)
	createTagFn       func(tag *model.ModelTag) error
	updateTagFn       func(tag *model.ModelTag) error
	deleteTagFn       func(id uuid.UUID) error
	getTagByIDFn      func(id uuid.UUID) (*model.ModelTag, error)
	existsTagByNameFn func(name string) (bool, error)
	setModelTagsFn    func(modelID uuid.UUID, tagIDs []uuid.UUID) error
	getModelTagsFn    func(modelID uuid.UUID) ([]model.ModelTag, error)
}

func (m *MockModelTagRepo) ListTags() ([]model.ModelTag, error) {
	if m.listTagsFn != nil {
		return m.listTagsFn()
	}
	return nil, nil
}
func (m *MockModelTagRepo) CreateTag(tag *model.ModelTag) error {
	if m.createTagFn != nil {
		return m.createTagFn(tag)
	}
	return nil
}
func (m *MockModelTagRepo) UpdateTag(tag *model.ModelTag) error {
	if m.updateTagFn != nil {
		return m.updateTagFn(tag)
	}
	return nil
}
func (m *MockModelTagRepo) DeleteTag(id uuid.UUID) error {
	if m.deleteTagFn != nil {
		return m.deleteTagFn(id)
	}
	return nil
}
func (m *MockModelTagRepo) GetTagByID(id uuid.UUID) (*model.ModelTag, error) {
	if m.getTagByIDFn != nil {
		return m.getTagByIDFn(id)
	}
	return nil, nil
}
func (m *MockModelTagRepo) ExistsTagByName(name string) (bool, error) {
	if m.existsTagByNameFn != nil {
		return m.existsTagByNameFn(name)
	}
	return false, nil
}
func (m *MockModelTagRepo) SetModelTags(modelID uuid.UUID, tagIDs []uuid.UUID) error {
	if m.setModelTagsFn != nil {
		return m.setModelTagsFn(modelID, tagIDs)
	}
	return nil
}
func (m *MockModelTagRepo) GetModelTags(modelID uuid.UUID) ([]model.ModelTag, error) {
	if m.getModelTagsFn != nil {
		return m.getModelTagsFn(modelID)
	}
	return nil, nil
}

// BatchGetModelTags 默认 stub：所有传入 modelID 都映射到空标签数组
func (m *MockModelTagRepo) BatchGetModelTags(modelIDs []uuid.UUID) (map[uuid.UUID][]model.ModelTag, error) {
	if len(modelIDs) == 0 {
		return nil, nil
	}
	out := make(map[uuid.UUID][]model.ModelTag, len(modelIDs))
	for _, id := range modelIDs {
		out[id] = []model.ModelTag{}
	}
	return out, nil
}

var _ ModelTagRepoInterface = (*MockModelTagRepo)(nil)

func newTagRouter(repo ModelTagRepoInterface) (*gin.Engine, *ModelTagHandler) {
	gin.SetMode(gin.TestMode)
	h := NewModelTagHandler(repo)
	r := gin.New()
	r.GET("/api/v1/model-tags", h.ListTags)
	r.POST("/api/v1/model-tags", h.CreateTag)
	r.PUT("/api/v1/model-tags/:id", h.UpdateTag)
	r.DELETE("/api/v1/model-tags/:id", h.DeleteTag)
	r.GET("/api/v1/models/:id/tags", h.GetModelTags)
	r.PUT("/api/v1/models/:id/tags", h.SetModelTags)
	r.PUT("/api/v1/models/batch-tags", h.BatchSetModelTags)
	return r, h
}

func TestListTags_Success(t *testing.T) {
	tags := []model.ModelTag{
		{ID: uuid.New(), Name: "important", Color: "#FF0000", SortOrder: 1, CreatedAt: time.Now()},
		{ID: uuid.New(), Name: "experimental", Color: "#0000FF", SortOrder: 0, CreatedAt: time.Now()},
	}
	repo := &MockModelTagRepo{listTagsFn: func() ([]model.ModelTag, error) { return tags, nil }}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-tags", nil))

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].([]interface{})
	assert.Equal(t, 2, len(data))
}

func TestListTags_Error(t *testing.T) {
	repo := &MockModelTagRepo{listTagsFn: func() ([]model.ModelTag, error) {
		return nil, errors.New("db down")
	}}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/model-tags", nil))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateTag_Success(t *testing.T) {
	repo := &MockModelTagRepo{
		existsTagByNameFn: func(name string) (bool, error) { return false, nil },
		createTagFn: func(tag *model.ModelTag) error {
			tag.ID = uuid.New()
			tag.CreatedAt = time.Now()
			tag.UpdatedAt = time.Now()
			return nil
		},
	}
	router, _ := newTagRouter(repo)

	body := `{"name":"urgent","color":"#FF6600","description":"D","sort_order":2}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestCreateTag_Conflict(t *testing.T) {
	repo := &MockModelTagRepo{
		existsTagByNameFn: func(name string) (bool, error) { return true, nil },
	}
	router, _ := newTagRouter(repo)

	body := `{"name":"existing-tag","color":"#FF0000"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateTag_DBError(t *testing.T) {
	repo := &MockModelTagRepo{
		existsTagByNameFn: func(name string) (bool, error) { return false, nil },
		createTagFn:       func(*model.ModelTag) error { return errors.New("insert failed") },
	}
	router, _ := newTagRouter(repo)

	body := `{"name":"x","color":"#000000"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCreateTag_InvalidBody(t *testing.T) {
	repo := &MockModelTagRepo{}
	router, _ := newTagRouter(repo)

	tests := []struct{ name, body string }{
		{"Missing required name", `{"color":"#FF0000"}`},
		{"Missing required color", `{"name":"test"}`},
		{"Name exceeds max length", `{"name":"` + strings.Repeat("a", 60) + `","color":"#FF0000"}`},
		{"Invalid color too long", `{"name":"t","color":"` + strings.Repeat("x", 31) + `"}`},
		{"Invalid JSON", `{invalid json}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code, tt.name)
		})
	}
}

func TestUpdateTag_Success(t *testing.T) {
	tagID := uuid.New()
	existing := &model.ModelTag{ID: tagID, Name: "old", Color: "#FF0000"}
	updateCalled := false
	repo := &MockModelTagRepo{
		getTagByIDFn: func(id uuid.UUID) (*model.ModelTag, error) { return existing, nil },
		updateTagFn:  func(t *model.ModelTag) error { updateCalled = true; return nil },
	}
	router, _ := newTagRouter(repo)

	body := `{"name":"new","color":"#0000FF","description":"D","sort_order":3}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/model-tags/%s", tagID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, updateCalled)
	assert.Equal(t, "new", existing.Name)
	assert.Equal(t, "#0000FF", existing.Color)
	assert.Equal(t, 3, existing.SortOrder)
}

func TestUpdateTag_NotFound(t *testing.T) {
	tagID := uuid.New()
	repo := &MockModelTagRepo{
		getTagByIDFn: func(id uuid.UUID) (*model.ModelTag, error) { return nil, errors.New("not found") },
	}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/model-tags/%s", tagID), bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUpdateTag_InvalidID(t *testing.T) {
	repo := &MockModelTagRepo{}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/model-tags/bad-id", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateTag_UpdateError(t *testing.T) {
	tagID := uuid.New()
	repo := &MockModelTagRepo{
		getTagByIDFn: func(id uuid.UUID) (*model.ModelTag, error) { return &model.ModelTag{ID: tagID}, nil },
		updateTagFn:  func(t *model.ModelTag) error { return errors.New("update failed") },
	}
	router, _ := newTagRouter(repo)

	body := `{"name":"newname"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/model-tags/%s", tagID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateTag_InvalidBody(t *testing.T) {
	tagID := uuid.New()
	repo := &MockModelTagRepo{
		getTagByIDFn: func(id uuid.UUID) (*model.ModelTag, error) { return &model.ModelTag{ID: tagID}, nil },
	}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/model-tags/%s", tagID), bytes.NewBufferString(`{not-json`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteTag_Success(t *testing.T) {
	tagID := uuid.New()
	called := false
	repo := &MockModelTagRepo{deleteTagFn: func(id uuid.UUID) error { called = (id == tagID); return nil }}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/model-tags/%s", tagID), nil))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
}

func TestDeleteTag_InvalidID(t *testing.T) {
	router, _ := newTagRouter(&MockModelTagRepo{})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("DELETE", "/api/v1/model-tags/bad", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteTag_Error(t *testing.T) {
	tagID := uuid.New()
	repo := &MockModelTagRepo{deleteTagFn: func(id uuid.UUID) error { return errors.New("delete failed") }}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/model-tags/%s", tagID), nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSetModelTags_Success(t *testing.T) {
	modelID := uuid.New()
	tagA := uuid.New()
	tagB := uuid.New()
	setCalled := false
	repo := &MockModelTagRepo{
		setModelTagsFn: func(mid uuid.UUID, tids []uuid.UUID) error {
			setCalled = true
			assert.Equal(t, modelID, mid)
			assert.Equal(t, 2, len(tids))
			return nil
		},
		getModelTagsFn: func(mid uuid.UUID) ([]model.ModelTag, error) {
			return []model.ModelTag{{ID: tagA}, {ID: tagB}}, nil
		},
	}
	router, _ := newTagRouter(repo)

	body := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, tagA, tagB)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/models/%s/tags", modelID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, setCalled)
}

func TestSetModelTags_InvalidModelID(t *testing.T) {
	router, _ := newTagRouter(&MockModelTagRepo{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/models/bad-id/tags", bytes.NewBufferString(`{"tag_ids":[]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetModelTags_MissingTagIDs(t *testing.T) {
	router, _ := newTagRouter(&MockModelTagRepo{})
	modelID := uuid.New()
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/models/%s/tags", modelID), bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// S1-4: invalid UUID in tag_ids must return 400 with code=invalid_tag_id
func TestSetModelTags_InvalidTagID_Returns400(t *testing.T) {
	modelID := uuid.New()
	validTag := uuid.New()
	repo := &MockModelTagRepo{}
	router, _ := newTagRouter(repo)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"tag_ids":["%s","not-a-uuid"]}`, validTag)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/models/%s/tags", modelID),
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, CodeInvalidTagID, errObj["code"])
	assert.Equal(t, "invalid_request", errObj["type"])
}

// S1-4: all valid UUIDs must succeed (positive regression)
func TestSetModelTags_AllValidUUIDs_Returns200(t *testing.T) {
	modelID := uuid.New()
	tagA := uuid.New()
	tagB := uuid.New()
	setCalled := false
	repo := &MockModelTagRepo{
		setModelTagsFn: func(mid uuid.UUID, tids []uuid.UUID) error {
			setCalled = true
			assert.Equal(t, 2, len(tids))
			return nil
		},
		getModelTagsFn: func(mid uuid.UUID) ([]model.ModelTag, error) {
			return []model.ModelTag{{ID: tagA}, {ID: tagB}}, nil
		},
	}
	router, _ := newTagRouter(repo)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"tag_ids":["%s","%s"]}`, tagA, tagB)
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/models/%s/tags", modelID),
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, setCalled)
}

// S1-4 BatchSetModelTags: invalid tag_id → 400 with code=invalid_tag_id
func TestBatchSetModelTags_InvalidTagID(t *testing.T) {
	modelID := uuid.New()
	repo := &MockModelTagRepo{}
	router, _ := newTagRouter(repo)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"model_ids":["%s"],"tag_ids":["not-a-uuid"]}`, modelID)
	req := httptest.NewRequest("PUT", "/api/v1/models/batch-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, CodeInvalidTagID, errObj["code"])
}

// BatchSetModelTags success path
func TestBatchSetModelTags_Success(t *testing.T) {
	modelA := uuid.New()
	modelB := uuid.New()
	tagID := uuid.New()
	callCount := 0
	repo := &MockModelTagRepo{
		setModelTagsFn: func(mid uuid.UUID, tids []uuid.UUID) error {
			callCount++
			assert.Equal(t, 1, len(tids))
			return nil
		},
	}
	router, _ := newTagRouter(repo)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"model_ids":["%s","%s"],"tag_ids":["%s"]}`, modelA, modelB, tagID)
	req := httptest.NewRequest("PUT", "/api/v1/models/batch-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, callCount)
}

// S1-4 BatchSetModelTags: invalid model_id → 400 with code=invalid_model_id
func TestBatchSetModelTags_InvalidModelID(t *testing.T) {
	tagID := uuid.New()
	repo := &MockModelTagRepo{}
	router, _ := newTagRouter(repo)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"model_ids":["not-a-uuid"],"tag_ids":["%s"]}`, tagID)
	req := httptest.NewRequest("PUT", "/api/v1/models/batch-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errObj := resp["error"].(map[string]interface{})
	assert.Equal(t, CodeInvalidModelID, errObj["code"])
}

// S3-1: rgba color (length ~20) must be accepted → 201
func TestCreateTag_RgbaColor_Accepted(t *testing.T) {
	repo := &MockModelTagRepo{
		existsTagByNameFn: func(name string) (bool, error) { return false, nil },
		createTagFn: func(tag *model.ModelTag) error {
			tag.ID = uuid.New()
			return nil
		},
	}
	router, _ := newTagRouter(repo)
	body := `{"name":"rgba-tag","color":"rgba(255, 128, 0, 0.5)"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

// S3-1: short hex color (#5DADE2) still accepted → 201
func TestCreateTag_HexColor_Accepted(t *testing.T) {
	repo := &MockModelTagRepo{
		existsTagByNameFn: func(name string) (bool, error) { return false, nil },
		createTagFn:       func(tag *model.ModelTag) error { return nil },
	}
	router, _ := newTagRouter(repo)
	body := `{"name":"hex-tag","color":"#5DADE2"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
}

// S3-1: color exceeding max length → 400
func TestCreateTag_ColorTooLong_Rejected(t *testing.T) {
	repo := &MockModelTagRepo{}
	router, _ := newTagRouter(repo)
	longColor := strings.Repeat("x", 31)
	body := fmt.Sprintf(`{"name":"long-color","color":"%s"}`, longColor)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/model-tags", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSetModelTags_SetError(t *testing.T) {
	modelID := uuid.New()
	repo := &MockModelTagRepo{
		setModelTagsFn: func(mid uuid.UUID, tids []uuid.UUID) error { return errors.New("set failed") },
	}
	router, _ := newTagRouter(repo)
	body := fmt.Sprintf(`{"tag_ids":["%s"]}`, uuid.New())
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", fmt.Sprintf("/api/v1/models/%s/tags", modelID), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetModelTags_Success(t *testing.T) {
	modelID := uuid.New()
	tags := []model.ModelTag{{ID: uuid.New(), Name: "tag1"}}
	repo := &MockModelTagRepo{getModelTagsFn: func(uuid.UUID) ([]model.ModelTag, error) { return tags, nil }}
	router, _ := newTagRouter(repo)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/v1/models/%s/tags", modelID), nil))

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetModelTags_InvalidID(t *testing.T) {
	router, _ := newTagRouter(&MockModelTagRepo{})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/models/bad-id/tags", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetModelTags_Error(t *testing.T) {
	modelID := uuid.New()
	repo := &MockModelTagRepo{
		getModelTagsFn: func(uuid.UUID) ([]model.ModelTag, error) { return nil, errors.New("query failed") },
	}
	router, _ := newTagRouter(repo)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", fmt.Sprintf("/api/v1/models/%s/tags", modelID), nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
