package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

type fakeGroupSvc struct {
	createFn       func(req service.CreateGroupRequest) (*model.Group, error)
	listFn         func(search string, page, pageSize int) ([]model.Group, int64, error)
	getByIDFn      func(id uint) (*model.Group, error)
	updateFn       func(id uint, req service.UpdateGroupRequest) (*model.Group, error)
	deleteFn       func(id uint) error
	addModelsFn    func(groupID uint, modelIDs []uuid.UUID, priorities map[uuid.UUID]int, weights map[uuid.UUID]int) error
	listModelsFn   func(groupID uint) ([]model.GroupModel, error)
	removeModelFn  func(groupID uint, modelID uuid.UUID) error
	listEnabledFn  func() ([]model.Group, error)
	countModelsFn      func(groupID uint) int
	getDefaultsFn      func() service.FailoverDefaults
}

func (f *fakeGroupSvc) Create(req service.CreateGroupRequest) (*model.Group, error) {
	if f.createFn != nil {
		return f.createFn(req)
	}
	return nil, nil
}
func (f *fakeGroupSvc) List(search string, page, pageSize int) ([]model.Group, int64, error) {
	if f.listFn != nil {
		return f.listFn(search, page, pageSize)
	}
	return nil, 0, nil
}
func (f *fakeGroupSvc) GetByID(id uint) (*model.Group, error) {
	if f.getByIDFn != nil {
		return f.getByIDFn(id)
	}
	return nil, nil
}
func (f *fakeGroupSvc) Update(id uint, req service.UpdateGroupRequest) (*model.Group, error) {
	if f.updateFn != nil {
		return f.updateFn(id, req)
	}
	return nil, nil
}
func (f *fakeGroupSvc) Delete(id uint) error {
	if f.deleteFn != nil {
		return f.deleteFn(id)
	}
	return nil
}
func (f *fakeGroupSvc) AddModelsToGroup(groupID uint, modelIDs []uuid.UUID, priorities map[uuid.UUID]int, weights map[uuid.UUID]int) error {
	if f.addModelsFn != nil {
		return f.addModelsFn(groupID, modelIDs, priorities, weights)
	}
	return nil
}
func (f *fakeGroupSvc) ListGroupModels(groupID uint) ([]model.GroupModel, error) {
	if f.listModelsFn != nil {
		return f.listModelsFn(groupID)
	}
	return nil, nil
}
func (f *fakeGroupSvc) RemoveModelFromGroup(groupID uint, modelID uuid.UUID) error {
	if f.removeModelFn != nil {
		return f.removeModelFn(groupID, modelID)
	}
	return nil
}
func (f *fakeGroupSvc) ListEnabledGroupsWithModels() ([]model.Group, error) {
	if f.listEnabledFn != nil {
		return f.listEnabledFn()
	}
	return nil, nil
}

func (f *fakeGroupSvc) CountGroupModels(groupID uint) int {
	if f.countModelsFn != nil {
		return f.countModelsFn(groupID)
	}
	return 0
}

func (f *fakeGroupSvc) GetFailoverDefaults() service.FailoverDefaults {
	if f.getDefaultsFn != nil {
		return f.getDefaultsFn()
	}
	return service.FailoverDefaults{}
}

var _ GroupServiceInterface = (*fakeGroupSvc)(nil)

func newGroupRouter(svc GroupServiceInterface) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewGroupHandler(svc)
	r.GET("/api/v1/internal/groups", h.ListEnabledGroups)
	r.GET("/api/v1/groups/defaults", h.GetDefaults)
	r.POST("/api/v1/groups", h.Create)
	r.GET("/api/v1/groups", h.List)
	r.GET("/api/v1/groups/:id", h.GetByID)
	r.PUT("/api/v1/groups/:id", h.Update)
	r.DELETE("/api/v1/groups/:id", h.Delete)
	r.POST("/api/v1/groups/:id/models", h.AddModels)
	r.GET("/api/v1/groups/:id/models", h.ListModels)
	r.DELETE("/api/v1/groups/:id/models/:model_id", h.RemoveModel)
	return r
}

func groupPtr(s string) *string { return &s }

// ======================== Create ========================

func TestCreateGroup_Success(t *testing.T) {
	created := &model.Group{
		ID: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Name: "test-group", Description: "desc", Strategy: "round_robin", Enabled: true,
	}
	svc := &fakeGroupSvc{
		createFn: func(req service.CreateGroupRequest) (*model.Group, error) {
			assert.Equal(t, "test-group", req.Name)
			assert.Equal(t, "round_robin", req.Strategy)
			return created, nil
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"name": "test-group", "strategy": "round_robin"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]json.RawMessage
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	dataBytes, ok := resp["data"]
	assert.True(t, ok)
	assert.Contains(t, string(dataBytes), "test-group")
	assert.Contains(t, string(dataBytes), "round_robin")
}

func TestCreateGroup_NameRequired(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"strategy": "round_robin"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

func TestCreateGroup_NameConflict(t *testing.T) {
	svc := &fakeGroupSvc{
		createFn: func(req service.CreateGroupRequest) (*model.Group, error) {
			return nil, service.ErrGroupNameExists
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"name": "dup", "strategy": "round_robin"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeGroupNameConflict, resp["error"].(map[string]interface{})["code"])
}

func TestCreateGroup_InvalidStrategy(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"name": "test", "strategy": "invalid"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

func TestCreateGroup_NameTooLong(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	longName := ""
	for i := 0; i < 65; i++ {
		longName += "a"
	}
	body, _ := json.Marshal(map[string]string{"name": longName, "strategy": "round_robin"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

// ======================== List ========================

func TestListGroups_Success(t *testing.T) {
	groups := []model.Group{
		{ID: 1, Name: "g1", Strategy: "round_robin"},
		{ID: 2, Name: "g2", Strategy: "priority"},
		{ID: 3, Name: "g3", Strategy: "weighted"},
	}
	svc := &fakeGroupSvc{
		listFn: func(search string, page, pageSize int) ([]model.Group, int64, error) {
			return groups, 3, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"total":3`)
	assert.Contains(t, w.Body.String(), "g1")
	assert.Contains(t, w.Body.String(), "g2")
	assert.Contains(t, w.Body.String(), "g3")
}

func TestListGroups_Empty(t *testing.T) {
	svc := &fakeGroupSvc{
		listFn: func(search string, page, pageSize int) ([]model.Group, int64, error) {
			return []model.Group{}, 0, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"total":0`)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}

func TestListGroups_Search(t *testing.T) {
	var capturedSearch string
	svc := &fakeGroupSvc{
		listFn: func(search string, page, pageSize int) ([]model.Group, int64, error) {
			capturedSearch = search
			return []model.Group{{ID: 1, Name: "search-result", Strategy: "round_robin"}}, 1, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups?search=foo", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "foo", capturedSearch)
}

func TestListGroups_Pagination(t *testing.T) {
	var capturedPage, capturedPageSize int
	svc := &fakeGroupSvc{
		listFn: func(search string, page, pageSize int) ([]model.Group, int64, error) {
			capturedPage = page
			capturedPageSize = pageSize
			return []model.Group{{ID: 1, Name: "p", Strategy: "round_robin"}}, 10, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups?page=2&page_size=5", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, capturedPage)
	assert.Equal(t, 5, capturedPageSize)
}

func TestListGroups_Error(t *testing.T) {
	svc := &fakeGroupSvc{
		listFn: func(search string, page, pageSize int) ([]model.Group, int64, error) {
			return nil, 0, errors.New("db error")
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ======================== GetByID ========================

func TestGetGroupByID_Success(t *testing.T) {
	group := &model.Group{
		ID: 42, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Name: "my-group", Description: "desc", Strategy: "priority", Enabled: true,
	}
	svc := &fakeGroupSvc{
		getByIDFn: func(id uint) (*model.Group, error) {
			assert.Equal(t, uint(42), id)
			return group, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/42", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "my-group")
}

func TestGetGroupByID_NotFound(t *testing.T) {
	svc := &fakeGroupSvc{
		getByIDFn: func(id uint) (*model.Group, error) {
			return nil, service.ErrGroupNotFound
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/99", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	var resp map[string]interface{}
	assert.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, CodeGroupNotFound, resp["error"].(map[string]interface{})["code"])
}

func TestGetGroupByID_InvalidID(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/not-a-number", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

// ======================== Update ========================

func TestUpdateGroup_Success(t *testing.T) {
	desc := "updated"
	reqBody := service.UpdateGroupRequest{Description: &desc}
	updated := &model.Group{
		ID: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		Name: "original", Description: "updated", Strategy: "round_robin", Enabled: true,
	}
	svc := &fakeGroupSvc{
		updateFn: func(id uint, req service.UpdateGroupRequest) (*model.Group, error) {
			assert.Equal(t, uint(1), id)
			assert.Equal(t, "updated", *req.Description)
			return updated, nil
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(reqBody)
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/groups/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "updated")
}

func TestUpdateGroup_NotFound(t *testing.T) {
	svc := &fakeGroupSvc{
		updateFn: func(id uint, req service.UpdateGroupRequest) (*model.Group, error) {
			return nil, service.ErrGroupNotFound
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"description": "x"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/groups/99", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeGroupNotFound)
}

func TestUpdateGroup_NameConflict(t *testing.T) {
	svc := &fakeGroupSvc{
		updateFn: func(id uint, req service.UpdateGroupRequest) (*model.Group, error) {
			return nil, service.ErrGroupNameExists
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"name": "duplicate"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/groups/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), CodeGroupNameConflict)
}

func TestUpdateGroup_Deleted(t *testing.T) {
	svc := &fakeGroupSvc{
		updateFn: func(id uint, req service.UpdateGroupRequest) (*model.Group, error) {
			return nil, service.ErrGroupDeleted
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]string{"description": "x"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/api/v1/groups/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "group has been deleted")
}

// ======================== Delete ========================

func TestDeleteGroup_Success(t *testing.T) {
	called := false
	svc := &fakeGroupSvc{
		deleteFn: func(id uint) error {
			called = true
			assert.Equal(t, uint(1), id)
			return nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, called)
}

func TestDeleteGroup_NotFound(t *testing.T) {
	svc := &fakeGroupSvc{
		deleteFn: func(id uint) error {
			return service.ErrGroupNotFound
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/99", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeGroupNotFound)
}

func TestDeleteGroup_InvalidID(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/invalid", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

// ======================== AddModels ========================

func TestAddModels_Success(t *testing.T) {
	var capturedID uint
	var capturedModelIDs []uuid.UUID
	modelID1 := uuid.New()
	modelID2 := uuid.New()
	svc := &fakeGroupSvc{
		addModelsFn: func(groupID uint, modelIDs []uuid.UUID, priorities map[uuid.UUID]int, weights map[uuid.UUID]int) error {
			capturedID = groupID
			capturedModelIDs = modelIDs
			return nil
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []uuid.UUID{modelID1, modelID2},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups/1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, uint(1), capturedID)
	assert.Len(t, capturedModelIDs, 2)
}

func TestAddModels_GroupNotFound(t *testing.T) {
	svc := &fakeGroupSvc{
		addModelsFn: func(groupID uint, modelIDs []uuid.UUID, priorities map[uuid.UUID]int, weights map[uuid.UUID]int) error {
			return service.ErrGroupNotFound
		},
	}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []uuid.UUID{uuid.New()},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups/99/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeGroupNotFound)
}

func TestAddModels_EmptyList(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []uuid.UUID{},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups/1/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

func TestAddModels_InvalidGroupID(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	body, _ := json.Marshal(map[string]interface{}{
		"model_ids": []uuid.UUID{uuid.New()},
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/groups/invalid/models", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

// ======================== ListModels ========================

func TestListModels_Success(t *testing.T) {
	modelID := uuid.New()
	models := []model.GroupModel{
		{ID: 1, GroupID: 1, ModelID: modelID},
		{ID: 2, GroupID: 1, ModelID: uuid.New()},
	}
	svc := &fakeGroupSvc{
		listModelsFn: func(groupID uint) ([]model.GroupModel, error) {
			assert.Equal(t, uint(1), groupID)
			return models, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/1/models", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), modelID.String())
}

func TestListModels_Empty(t *testing.T) {
	svc := &fakeGroupSvc{
		listModelsFn: func(groupID uint) ([]model.GroupModel, error) {
			return []model.GroupModel{}, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/1/models", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}

func TestListModels_GroupNotFound(t *testing.T) {
	svc := &fakeGroupSvc{
		listModelsFn: func(groupID uint) ([]model.GroupModel, error) {
			return nil, service.ErrGroupNotFound
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/99/models", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeGroupNotFound)
}

// ======================== RemoveModel ========================

func TestRemoveModel_Success(t *testing.T) {
	modelID := uuid.New()
	var capturedGroupID uint
	var capturedModelID uuid.UUID
	svc := &fakeGroupSvc{
		removeModelFn: func(groupID uint, mID uuid.UUID) error {
			capturedGroupID = groupID
			capturedModelID = mID
			return nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/1/models/"+modelID.String(), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, uint(1), capturedGroupID)
	assert.Equal(t, modelID, capturedModelID)
}

func TestRemoveModel_GroupNotFound(t *testing.T) {
	svc := &fakeGroupSvc{
		removeModelFn: func(groupID uint, modelID uuid.UUID) error {
			return service.ErrGroupNotFound
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/99/models/"+uuid.New().String(), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), CodeGroupNotFound)
}

func TestRemoveModel_InvalidIDs(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/v1/groups/1/models/not-a-uuid", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), CodeBadRequest)
}

// ======================== Internal ListEnabledGroups ========================

func TestInternalListEnabledGroups_NoToken(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/internal/groups", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestInternalListEnabledGroups_WrongToken(t *testing.T) {
	svc := &fakeGroupSvc{}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/internal/groups", nil)
	req.Header.Set("X-Auth-Token", "wrong-token")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestInternalListEnabledGroups_Success(t *testing.T) {
	enabledGroups := []model.Group{
		{
			ID: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			Name: "enabled-group", Strategy: "round_robin", Enabled: true,
			Models: []model.GroupModel{
				{ID: 1, GroupID: 1, ModelID: uuid.New()},
			},
		},
	}
	svc := &fakeGroupSvc{
		listEnabledFn: func() ([]model.Group, error) {
			return enabledGroups, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/internal/groups", nil)
	req.Header.Set("X-Auth-Token", "internal-shared-secret")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "enabled-group")
}

func TestInternalListEnabledGroups_Empty(t *testing.T) {
	svc := &fakeGroupSvc{
		listEnabledFn: func() ([]model.Group, error) {
			return []model.Group{}, nil
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/internal/groups", nil)
	req.Header.Set("X-Auth-Token", "internal-shared-secret")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"data":[]`)
}

func TestGetDefaults_Success(t *testing.T) {
	svc := &fakeGroupSvc{
		getDefaultsFn: func() service.FailoverDefaults {
			return service.FailoverDefaults{
				MaxSwitches:      3,
				SameModelRetries: 2,
				CooldownSeconds:  120,
			}
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/defaults", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"max_switches":3`)
	assert.Contains(t, w.Body.String(), `"same_model_retries":2`)
	assert.Contains(t, w.Body.String(), `"failover_cooldown_seconds":120`)
}

func TestGetDefaults_DefaultValues(t *testing.T) {
	svc := &fakeGroupSvc{
		getDefaultsFn: func() service.FailoverDefaults {
			return service.FailoverDefaults{
				MaxSwitches:      2,
				SameModelRetries: 1,
				CooldownSeconds:  60,
			}
		},
	}
	r := newGroupRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/groups/defaults", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"max_switches":2`)
	assert.Contains(t, w.Body.String(), `"same_model_retries":1`)
	assert.Contains(t, w.Body.String(), `"failover_cooldown_seconds":60`)
}
