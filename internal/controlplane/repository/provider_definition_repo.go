package repository

import (
	"sort"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"gorm.io/gorm"
)

// ProviderDefinitionRepo 渠道定义数据访问层
type ProviderDefinitionRepo struct {
	db *gorm.DB
}

// NewProviderDefinitionRepo 创建实例
func NewProviderDefinitionRepo(db *gorm.DB) *ProviderDefinitionRepo {
	return &ProviderDefinitionRepo{db: db}
}

// List 查询所有启用的渠道定义
func (r *ProviderDefinitionRepo) List() ([]model.ProviderDefinition, error) {
	var defs []model.ProviderDefinition
	if err := r.db.Where("enabled = ?", true).Order("builtin DESC, display_name ASC").Find(&defs).Error; err != nil {
		return nil, err
	}
	if len(defs) > 0 {
		return defs, nil
	}
	return r.fallbackDefinitionsFromProviders()
}

type providerDefinitionFallbackRow struct {
	DefinitionID string
	Type         string
	Endpoint     string
}

func (r *ProviderDefinitionRepo) fallbackDefinitionsFromProviders() ([]model.ProviderDefinition, error) {
	var rows []providerDefinitionFallbackRow
	if err := r.db.Model(&model.Provider{}).
		Select("definition_id, type, MIN(endpoint) AS endpoint").
		Where("enabled = ?", true).
		Where("COALESCE(definition_id, '') <> '' OR COALESCE(type, '') <> ''").
		Group("definition_id, type").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	byID := make(map[string]model.ProviderDefinition)
	for _, row := range rows {
		id := strings.TrimSpace(row.DefinitionID)
		adapter := strings.TrimSpace(row.Type)
		if id == "" {
			id = adapter
		}
		if adapter == "" {
			adapter = id
		}
		if id == "" || adapter == "" {
			continue
		}
		def := fallbackProviderDefinition(id, adapter, row.Endpoint)
		if existing, ok := byID[def.ID]; ok && existing.BaseURL != "" {
			continue
		}
		byID[def.ID] = def
	}

	defs := make([]model.ProviderDefinition, 0, len(byID))
	for _, def := range byID {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].DisplayName < defs[j].DisplayName
	})
	return defs, nil
}

func fallbackProviderDefinition(id, adapter, endpoint string) model.ProviderDefinition {
	def := model.ProviderDefinition{
		ID:               id,
		DisplayName:      fallbackProviderDisplayName(id),
		Adapter:          adapter,
		BaseURL:          endpoint,
		CredentialSchema: fallbackCredentialSchema(adapter),
		VendorOptions:    fallbackVendorOptions(adapter),
		Builtin:          true,
		Enabled:          true,
	}
	return def
}

func fallbackProviderDisplayName(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "qwen", "dashscope", "bailian":
		return "千问"
	case "deepseek":
		return "DeepSeek"
	case "openai":
		return "OpenAI"
	case "openai-compatible", "openai_compatible":
		return "OpenAI 兼容"
	case "azure":
		return "Azure OpenAI"
	case "gemini":
		return "Google Gemini"
	default:
		return strings.ToUpper(id[:1]) + id[1:]
	}
}

func fallbackCredentialSchema(adapter string) model.CredentialSchema {
	schema := model.CredentialSchema{
		"api_key": {
			Type:        "string",
			Required:    true,
			Secret:      true,
			Label:       "API Key",
			Placeholder: "sk-...",
		},
	}
	if strings.EqualFold(adapter, "azure") {
		schema["api_key"] = model.CredentialField{Type: "string", Required: true, Secret: true, Label: "API Key", Placeholder: "从 Azure Portal 获取"}
	}
	return schema
}

func fallbackVendorOptions(adapter string) model.VendorOptions {
	switch strings.ToLower(strings.TrimSpace(adapter)) {
	case "azure":
		return model.VendorOptions{URLPattern: "azure", AuthHeader: "api-key", RequiresAPIVersion: true, APIVersion: "2025-04-01-preview", EmptyChoicesSkip: true}
	case "dashscope", "bailian":
		return model.VendorOptions{URLPattern: "standard", AuthHeader: "bearer", StreamUsageInLastChunk: true, EmptyChoicesSkip: true}
	default:
		return model.VendorOptions{URLPattern: "standard", AuthHeader: "bearer"}
	}
}

// GetByID 按 ID 查询渠道定义
func (r *ProviderDefinitionRepo) GetByID(id string) (*model.ProviderDefinition, error) {
	var def model.ProviderDefinition
	err := r.db.First(&def, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &def, nil
}

// Create 创建自定义渠道定义
func (r *ProviderDefinitionRepo) Create(def *model.ProviderDefinition) error {
	return r.db.Create(def).Error
}

// Update 更新渠道定义
func (r *ProviderDefinitionRepo) Update(def *model.ProviderDefinition) error {
	return r.db.Save(def).Error
}

// Delete 删除渠道定义（内置不可删）
func (r *ProviderDefinitionRepo) Delete(id string) error {
	return r.db.Where("id = ? AND builtin = ?", id, false).Delete(&model.ProviderDefinition{}).Error
}

// ExistsByID 检查 ID 是否已存在
func (r *ProviderDefinitionRepo) ExistsByID(id string) (bool, error) {
	var count int64
	err := r.db.Model(&model.ProviderDefinition{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}
