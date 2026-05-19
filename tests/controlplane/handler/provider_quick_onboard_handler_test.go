package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/llm-gateway/internal/controlplane/handler"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const quickProviderPreviewPath = "/api/v1/providers/quick-onboard/preview"
const quickProviderCreatePath = "/api/v1/providers/quick-onboard"

func setupProviderQuickOnboardHandler(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	crypto.InitForTesting()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, db.Exec(`
		CREATE TABLE providers (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			name TEXT NOT NULL UNIQUE,
			definition_id TEXT DEFAULT '',
			type TEXT NOT NULL DEFAULT '',
			endpoint TEXT NOT NULL,
			encrypted_credentials TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			weight INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			status INTEGER NOT NULL DEFAULT 1,
			test_model TEXT DEFAULT '',
			response_time INTEGER DEFAULT 0,
			balance REAL DEFAULT 0,
			remark TEXT DEFAULT '',
			max_retries INTEGER NOT NULL DEFAULT 3,
			timeout INTEGER NOT NULL DEFAULT 30,
			tag TEXT DEFAULT '',
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			last_check_error TEXT DEFAULT '',
			created_at DATETIME,
			updated_at DATETIME
		);
		CREATE TABLE models (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			name TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			model_type TEXT NOT NULL DEFAULT 'chat',
			enabled INTEGER NOT NULL DEFAULT 1,
			status INTEGER NOT NULL DEFAULT 1,
			upstream_model_id TEXT DEFAULT '',
			base_model TEXT NOT NULL DEFAULT '',
			protocol_mode TEXT NOT NULL DEFAULT 'auto',
			param_profile TEXT NOT NULL DEFAULT 'auto',
			unsupported_param_policy TEXT NOT NULL DEFAULT 'preserve',
			supported_params_override TEXT NOT NULL DEFAULT '{}',
			transform_overrides TEXT NOT NULL DEFAULT '{}',
			description TEXT DEFAULT '',
			context_length INTEGER DEFAULT 0,
			max_output_tokens INTEGER DEFAULT 0,
			input_price REAL DEFAULT 0,
			output_price REAL DEFAULT 0,
			cache_read_price REAL,
			cache_write_price REAL,
			price_mode TEXT NOT NULL DEFAULT 'catalog',
			weight INTEGER NOT NULL DEFAULT 1,
			catalog_id TEXT,
			ctx_overridden INTEGER NOT NULL DEFAULT 0,
			max_output_overridden INTEGER NOT NULL DEFAULT 0,
			video_supported_resolutions TEXT NOT NULL DEFAULT '[]',
			video_supported_aspect_ratios TEXT NOT NULL DEFAULT '[]',
			video_min_duration_seconds INTEGER NOT NULL DEFAULT 0,
			video_max_duration_seconds INTEGER NOT NULL DEFAULT 0,
			video_supported_actions TEXT NOT NULL DEFAULT '[]',
			quota_rpm INTEGER,
			quota_max_concurrency INTEGER,
			param_style TEXT NOT NULL DEFAULT '',
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			consecutive_recovery_successes INTEGER NOT NULL DEFAULT 0,
			last_check_error TEXT DEFAULT '',
			response_time INTEGER DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			UNIQUE(name, provider_id)
		);
	`).Error)

	svc := service.NewQuickOnboardService(db, nil)
	h := handler.NewProviderQuickOnboardHandler(svc)
	r := gin.New()
	r.POST(quickProviderPreviewPath, h.Preview)
	r.POST(quickProviderCreatePath, h.Create)
	return r, db
}

func quickProviderOnboardPayload() string {
	return `{
		"provider": {
			"definition_id":"qwen",
			"type":"qwen",
			"endpoint":"https://dashscope.aliyuncs.com/compatible-mode/v1",
			"tag":"prod"
		},
		"provider_slug":"千问",
		"purpose":"坐席",
		"credentials":[
			{"credentials":"sk-live-credential-a","remark":"主账号"},
			{"credentials":"sk-live-credential-b","remark":"备用账号"}
		],
		"models":[
			{"name":"qwen-plus","model_type":"chat"},
			{"name":"qwen-max","model_type":"chat"}
		]
	}`
}

func quickProviderOnboardPayloadWithNamePrefix() string {
	return strings.Replace(quickProviderOnboardPayload(), `"purpose":"坐席",`, `"purpose":"坐席","name_prefix":"agent-prod",`, 1)
}

func quickProviderOnboardPayloadWithJSONCredentials() string {
	return strings.Replace(quickProviderOnboardPayload(), `"sk-live-credential-a"`, `"{\"api_key\":\"sk-json-credential-a\"}"`, 1)
}

func quickProviderOnboardPayloadWithCatalog(catalogID string) string {
	return strings.Replace(quickProviderOnboardPayload(), `{"name":"qwen-plus","model_type":"chat"}`, `{"name":"qwen-plus","model_type":"chat","catalog_id":"`+catalogID+`"}`, 1)
}

func doQuickProviderRequest(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertQuickProviderNoSecretLeak(t *testing.T, raw string) {
	t.Helper()
	assert.NotContains(t, raw, "sk-live-credential-a", "响应不得回显第一个明文凭证")
	assert.NotContains(t, raw, "sk-live-credential-b", "响应不得回显第二个明文凭证")
}

func TestProviderQuickOnboardHandler_PreviewNoWriteAndMasksCredentials(t *testing.T) {
	// 中文说明：输入 2 个凭证和 2 个模型时，preview 只返回 N*M 预览矩阵，自动生成渠道名，且不写入 providers/models。
	r, db := setupProviderQuickOnboardHandler(t)
	require.NoError(t, db.Create(&model.Provider{
		ID:                   uuid.New(),
		Name:                 "qwen-seat-001",
		DefinitionID:         "qwen",
		Type:                 "qwen",
		Endpoint:             "https://existing.example.com",
		EncryptedCredentials: "encrypted",
		Enabled:              true,
		Status:               model.ProviderStatusEnabled,
	}).Error)

	w := doQuickProviderRequest(r, http.MethodPost, quickProviderPreviewPath, quickProviderOnboardPayload())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertQuickProviderNoSecretLeak(t, w.Body.String())

	var resp struct {
		Data struct {
			ProviderCount int    `json:"provider_count"`
			ModelCount    int    `json:"model_count"`
			MatrixCount   int    `json:"matrix_count"`
			NamePrefix    string `json:"name_prefix"`
			Providers     []struct {
				Index      int      `json:"index"`
				Name       string   `json:"name"`
				Credential string   `json:"credential"`
				Models     []string `json:"models"`
				ModelCount int      `json:"model_count"`
				WillCreate bool     `json:"will_create"`
			} `json:"providers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 2, resp.Data.ProviderCount)
	assert.Equal(t, 2, resp.Data.ModelCount)
	assert.Equal(t, 4, resp.Data.MatrixCount)
	assert.Equal(t, "qwen-seat", resp.Data.NamePrefix)
	require.Len(t, resp.Data.Providers, 2)
	assert.Equal(t, "qwen-seat-002", resp.Data.Providers[0].Name, "已有 qwen-seat-001 时应自动避让冲突")
	assert.Equal(t, "qwen-seat-003", resp.Data.Providers[1].Name)
	assert.Equal(t, []string{"qwen-plus", "qwen-max"}, resp.Data.Providers[0].Models)
	assert.Equal(t, 2, resp.Data.Providers[0].ModelCount)
	assert.True(t, resp.Data.Providers[0].WillCreate)
	assert.NotEmpty(t, resp.Data.Providers[0].Credential, "preview 只允许返回脱敏凭证")

	var providerCount int64
	var modelCount int64
	require.NoError(t, db.Model(&model.Provider{}).Count(&providerCount).Error)
	require.NoError(t, db.Model(&model.Model{}).Count(&modelCount).Error)
	assert.EqualValues(t, 1, providerCount, "preview 不应新增 provider")
	assert.EqualValues(t, 0, modelCount, "preview 不应新增 model")
}

func TestProviderQuickOnboardHandler_PreviewUsesCustomNamePrefix(t *testing.T) {
	// 中文说明：管理员可以自定义渠道名前缀，后端仍负责追加 001/002 并避让重名。
	r, _ := setupProviderQuickOnboardHandler(t)

	w := doQuickProviderRequest(r, http.MethodPost, quickProviderPreviewPath, quickProviderOnboardPayloadWithNamePrefix())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Data struct {
			NamePrefix string `json:"name_prefix"`
			Providers  []struct {
				Name string `json:"name"`
			} `json:"providers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, "agent-prod", resp.Data.NamePrefix)
	require.Len(t, resp.Data.Providers, 2)
	assert.Equal(t, "agent-prod-001", resp.Data.Providers[0].Name)
	assert.Equal(t, "agent-prod-002", resp.Data.Providers[1].Name)
}

func TestProviderQuickOnboardHandler_PreviewMasksJSONCredentialField(t *testing.T) {
	// 中文说明：前端传入 JSON 凭证时，preview 应脱敏真实 api_key，而不是把整个 JSON 脱敏成 {a****31}。
	r, _ := setupProviderQuickOnboardHandler(t)

	w := doQuickProviderRequest(r, http.MethodPost, quickProviderPreviewPath, quickProviderOnboardPayloadWithJSONCredentials())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Data struct {
			Providers []struct {
				Credential string `json:"credential"`
			} `json:"providers"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Data.Providers, 2)
	assert.Equal(t, "sk-****al-a", resp.Data.Providers[0].Credential)
	assert.NotContains(t, resp.Data.Providers[0].Credential, "{")
}

func TestProviderQuickOnboardHandler_CreateCreatesProviderModelMatrix(t *testing.T) {
	// 中文说明：create 以单个 credential 为事务目标；2 个凭证和 2 个模型应创建 2 个渠道、每个渠道 2 个模型，并返回逐目标结果。
	r, db := setupProviderQuickOnboardHandler(t)
	require.NoError(t, db.Create(&model.Provider{
		ID:                   uuid.New(),
		Name:                 "qwen-seat-001",
		DefinitionID:         "qwen",
		Type:                 "qwen",
		Endpoint:             "https://existing.example.com",
		EncryptedCredentials: "encrypted",
		Enabled:              true,
		Status:               model.ProviderStatusEnabled,
	}).Error)

	w := doQuickProviderRequest(r, http.MethodPost, quickProviderCreatePath, quickProviderOnboardPayload())
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assertQuickProviderNoSecretLeak(t, w.Body.String())

	var resp struct {
		Data struct {
			ProviderCount         int `json:"provider_count"`
			SucceededCount        int `json:"succeeded_count"`
			FailedCount           int `json:"failed_count"`
			ModelCountPerProvider int `json:"model_count_per_provider"`
			MatrixCount           int `json:"matrix_count"`
			Results               []struct {
				Index      int      `json:"index"`
				Name       string   `json:"name"`
				ProviderID string   `json:"provider_id"`
				ModelIDs   []string `json:"model_ids"`
				ModelCount int      `json:"model_count"`
				Status     string   `json:"status"`
				Error      string   `json:"error"`
			} `json:"results"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, 2, resp.Data.ProviderCount)
	assert.Equal(t, 2, resp.Data.SucceededCount)
	assert.Equal(t, 0, resp.Data.FailedCount)
	assert.Equal(t, 2, resp.Data.ModelCountPerProvider)
	assert.Equal(t, 4, resp.Data.MatrixCount)
	require.Len(t, resp.Data.Results, 2)
	assert.Equal(t, "qwen-seat-002", resp.Data.Results[0].Name)
	assert.Equal(t, "qwen-seat-003", resp.Data.Results[1].Name)

	for _, result := range resp.Data.Results {
		assert.Equal(t, "success", result.Status)
		assert.NotEmpty(t, result.ProviderID)
		assert.Len(t, result.ModelIDs, 2)
		assert.Equal(t, 2, result.ModelCount)
		assert.Empty(t, result.Error)
	}

	var providers []model.Provider
	require.NoError(t, db.Order("name ASC").Find(&providers).Error)
	require.Len(t, providers, 3)
	assert.Equal(t, []string{"qwen-seat-001", "qwen-seat-002", "qwen-seat-003"}, []string{providers[0].Name, providers[1].Name, providers[2].Name})

	for _, providerName := range []string{"qwen-seat-002", "qwen-seat-003"} {
		var provider model.Provider
		require.NoError(t, db.First(&provider, "name = ?", providerName).Error)
		var models []model.Model
		require.NoError(t, db.Where("provider_id = ?", provider.ID).Order("name ASC").Find(&models).Error)
		require.Len(t, models, 2)
		assert.Equal(t, []string{"qwen-max", "qwen-plus"}, []string{models[0].Name, models[1].Name})
		assert.Equal(t, "chat", models[0].ModelType)
		assert.Equal(t, "catalog", models[0].PriceMode)
	}
}

func TestProviderQuickOnboardHandler_CreateRespectsModelEnabledFalse(t *testing.T) {
	// 中文说明：继承源渠道禁用模型时，新渠道模型应保持 enabled=false / 手动禁用状态。
	r, db := setupProviderQuickOnboardHandler(t)
	body := `{
		"provider": {
			"definition_id":"qwen",
			"type":"qwen",
			"endpoint":"https://dashscope.aliyuncs.com/compatible-mode/v1"
		},
		"credentials":[{"credentials":"sk-live-credential-a"}],
		"models":[
			{"name":"qwen-plus","model_type":"chat","enabled":true},
			{"name":"qwen-disabled","model_type":"chat","enabled":false}
		]
	}`
	w := doQuickProviderRequest(r, http.MethodPost, quickProviderCreatePath, body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var enabledModel model.Model
	require.NoError(t, db.Where("name = ?", "qwen-plus").First(&enabledModel).Error)
	require.NotNil(t, enabledModel.Enabled)
	assert.True(t, *enabledModel.Enabled)
	assert.Equal(t, model.ModelStatusEnabled, enabledModel.Status)

	var disabledModel model.Model
	require.NoError(t, db.Where("name = ?", "qwen-disabled").First(&disabledModel).Error)
	require.NotNil(t, disabledModel.Enabled)
	assert.False(t, *disabledModel.Enabled)
	assert.Equal(t, model.ModelStatusManuallyDisabled, disabledModel.Status)
}

func TestProviderQuickOnboardHandler_CreatePersistsCatalogBinding(t *testing.T) {
	// 中文说明：快速接入可按模型手动关联 Catalog，创建出的每个渠道模型都写入 catalog_id。
	r, db := setupProviderQuickOnboardHandler(t)
	catalogID := uuid.New()

	w := doQuickProviderRequest(r, http.MethodPost, quickProviderCreatePath, quickProviderOnboardPayloadWithCatalog(catalogID.String()))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var models []model.Model
	require.NoError(t, db.Where("name = ?", "qwen-plus").Find(&models).Error)
	require.Len(t, models, 2)
	for _, m := range models {
		require.NotNil(t, m.CatalogID)
		assert.Equal(t, catalogID, *m.CatalogID)
	}
}
