package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cpHandler "github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// stubEnabledGroupService 只实现 ListEnabledGroups，其余方法通过嵌入接口占位
// （本用例不会调用它们）。
type stubEnabledGroupService struct {
	cpHandler.GroupServiceInterface
	groups []model.Group
}

func (s *stubEnabledGroupService) ListEnabledGroupsWithModels() ([]model.Group, error) {
	return s.groups, nil
}

// TestListEnabledGroupsAdvertisesCatalogVideoCapability 是 /api/v1/internal/groups
// 的 payload 级回归：catalog 声明 supports_video_input 且模型没有 override 时，
// 下发给网关的 supports_video_input 必须是 true。
//
// 这条曾经是 Critical：handler 用不预加载 Catalog 的 ModelRepo.GetByID 喂给
// EffectiveSupportsVideoInput，导致能力恒为 false（catalog 通路整条失效，
// 只有人工 override 生效），而 /internal/v1/groups/model-info 却是 true ——
// 同一模型在两条跨进程契约上给出相反答案。
func TestListEnabledGroupsAdvertisesCatalogVideoCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("INTERNAL_TOKEN", "test-internal-token")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	for _, stmt := range []string{
		"CREATE TABLE providers (id TEXT PRIMARY KEY, name TEXT, definition_id TEXT, type TEXT, endpoint TEXT, enabled INTEGER DEFAULT 1, status INTEGER DEFAULT 1, weight INTEGER DEFAULT 1, priority INTEGER DEFAULT 0)",
		"CREATE TABLE model_catalog (id TEXT PRIMARY KEY, source TEXT NOT NULL, source_model_id TEXT NOT NULL, display_name TEXT, provider TEXT, mode TEXT, supports_vision BOOLEAN DEFAULT false, supports_video_input BOOLEAN NOT NULL DEFAULT false)",
		"CREATE TABLE models (id TEXT PRIMARY KEY, name TEXT NOT NULL, provider_id TEXT, model_type TEXT DEFAULT 'chat', enabled INTEGER DEFAULT 1, status INTEGER DEFAULT 1, catalog_id TEXT, override_supports_video_input INTEGER, upstream_model_id TEXT DEFAULT '', created_at DATETIME, updated_at DATETIME)",
		"CREATE TABLE groups (id INTEGER PRIMARY KEY, name TEXT, strategy TEXT, enabled INTEGER DEFAULT 1)",
		"CREATE TABLE group_models (id INTEGER PRIMARY KEY, group_id INTEGER, model_id TEXT, priority INTEGER, weight INTEGER, complexity_tier TEXT)",
	} {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create table: %v", err)
		}
	}

	catalogID := uuid.New()
	if err := db.Exec("INSERT INTO model_catalog (id, source, source_model_id, provider, supports_video_input) VALUES (?, 'litellm', 'vertex_ai/gemini-2.0-flash', 'vertex_ai', 1)", catalogID.String()).Error; err != nil {
		t.Fatalf("insert catalog: %v", err)
	}
	providerID := uuid.New()
	if err := db.Exec("INSERT INTO providers (id, name, definition_id, type, endpoint, enabled, status) VALUES (?, 'vertex', 'vertex', 'vertex', 'https://example.com', 1, 1)", providerID.String()).Error; err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	modelID := uuid.New()
	if err := db.Exec("INSERT INTO models (id, name, provider_id, catalog_id, enabled, status) VALUES (?, 'gemini-video', ?, ?, 1, 1)", modelID.String(), providerID.String(), catalogID.String()).Error; err != nil {
		t.Fatalf("insert model: %v", err)
	}

	priority := 0
	svc := &stubEnabledGroupService{groups: []model.Group{{
		ID:       1,
		Name:     "video-group",
		Strategy: "priority",
		Enabled:  true,
		Models: []model.GroupModel{{
			GroupID:  1,
			ModelID:  modelID,
			Priority: &priority,
		}},
	}}}

	h := cpHandler.NewGroupHandler(svc, repository.NewModelRepo(db), repository.NewProviderRepo(db))
	router := gin.New()
	router.GET("/api/v1/internal/groups", h.ListEnabledGroups)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/internal/groups", nil)
	req.Header.Set("X-Internal-Token", "test-internal-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}

	var payload struct {
		Data []struct {
			Name   string `json:"name"`
			Models []struct {
				Name               string `json:"name"`
				SupportsVideoInput bool   `json:"supports_video_input"`
			} `json:"models"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, w.Body.String())
	}
	if len(payload.Data) != 1 || len(payload.Data[0].Models) != 1 {
		t.Fatalf("unexpected payload shape: %s", w.Body.String())
	}
	if !payload.Data[0].Models[0].SupportsVideoInput {
		t.Fatalf("catalog 声明支持视频时 payload 必须是 true；实际 body=%s", w.Body.String())
	}
}
