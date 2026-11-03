package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrQuickOnboardInvalidRequest = errors.New("invalid quick onboard request")
)

type QuickOnboardProviderInput struct {
	DefinitionID string `json:"definition_id" binding:"required,max=50"`
	Type         string `json:"type" binding:"omitempty,max=50"`
	Endpoint     string `json:"endpoint" binding:"required,max=500"`
	Weight       *int   `json:"weight" binding:"omitempty,min=0,max=9999"`
	Priority     *int   `json:"priority" binding:"omitempty,min=0,max=9999"`
	TestModel    string `json:"test_model" binding:"omitempty,max=200"`
	Remark       string `json:"remark" binding:"omitempty,max=500"`
	MaxRetries   *int   `json:"max_retries" binding:"omitempty,min=0,max=10"`
	Timeout      *int   `json:"timeout" binding:"omitempty,min=1,max=300"`
	Tag          string `json:"tag" binding:"omitempty,max=100"`
}

type QuickOnboardModelInput struct {
	Name            string   `json:"name" binding:"required,max=200"`
	ModelType       string   `json:"model_type" binding:"omitempty,oneof=chat embedding embedding_vl rerank rerank_vl video"`
	UpstreamModelID string   `json:"upstream_model_id" binding:"omitempty,max=200"`
	Description     string   `json:"description" binding:"omitempty,max=500"`
	ContextLength   *int     `json:"context_length" binding:"omitempty,min=0,max=10000000"`
	MaxOutputTokens *int     `json:"max_output_tokens" binding:"omitempty,min=0,max=10000000"`
	InputPrice      *float64 `json:"input_price" binding:"omitempty,min=0"`
	OutputPrice     *float64 `json:"output_price" binding:"omitempty,min=0"`
	PriceMode       string   `json:"price_mode" binding:"omitempty,oneof=catalog manual"`
	Weight          *int     `json:"weight" binding:"omitempty,min=0,max=9999"`
}

type QuickOnboardCredentialInput struct {
	Credentials string `json:"credentials" binding:"required"`
	Remark      string `json:"remark" binding:"omitempty,max=500"`
}

type QuickOnboardRequest struct {
	Provider     QuickOnboardProviderInput     `json:"provider" binding:"required"`
	ProviderSlug string                        `json:"provider_slug" binding:"omitempty,max=50"`
	Purpose      string                        `json:"purpose" binding:"omitempty,max=50"`
	NamePrefix   string                        `json:"name_prefix" binding:"omitempty,max=80"`
	Credentials  []QuickOnboardCredentialInput `json:"credentials" binding:"required,min=1,max=100,dive"`
	Models       []QuickOnboardModelInput      `json:"models" binding:"required,min=1,max=200,dive"`
}

type QuickOnboardProviderPlan struct {
	Index      int      `json:"index"`
	Name       string   `json:"name"`
	Credential string   `json:"credential"`
	Models     []string `json:"models"`
	ModelCount int      `json:"model_count"`
	Remark     string   `json:"remark,omitempty"`
	WillCreate bool     `json:"will_create"`
}

type QuickOnboardPreview struct {
	ProviderCount int                        `json:"provider_count"`
	ModelCount    int                        `json:"model_count"`
	MatrixCount   int                        `json:"matrix_count"`
	NamePrefix    string                     `json:"name_prefix"`
	Providers     []QuickOnboardProviderPlan `json:"providers"`
	Warnings      []string                   `json:"warnings"`
}

type QuickOnboardProviderResult struct {
	Index      int         `json:"index"`
	Name       string      `json:"name"`
	ProviderID uuid.UUID   `json:"provider_id,omitempty"`
	ModelIDs   []uuid.UUID `json:"model_ids,omitempty"`
	ModelCount int         `json:"model_count"`
	Status     string      `json:"status"`
	Error      string      `json:"error,omitempty"`
}

type QuickOnboardCreateResult struct {
	ProviderCount         int                          `json:"provider_count"`
	SucceededCount        int                          `json:"succeeded_count"`
	FailedCount           int                          `json:"failed_count"`
	ModelCountPerProvider int                          `json:"model_count_per_provider"`
	MatrixCount           int                          `json:"matrix_count"`
	Results               []QuickOnboardProviderResult `json:"results"`
}

type QuickOnboardService struct {
	db          *gorm.DB
	publisher   *event.Publisher
	snapshotter *ProviderSnapshotRefresher
}

func NewQuickOnboardService(db *gorm.DB, publisher *event.Publisher) *QuickOnboardService {
	return &QuickOnboardService{db: db, publisher: publisher}
}

func (s *QuickOnboardService) SetSnapshotRefresher(snapshotter *ProviderSnapshotRefresher) {
	s.snapshotter = snapshotter
}

func (s *QuickOnboardService) Preview(req QuickOnboardRequest) (*QuickOnboardPreview, error) {
	normalized, err := normalizeQuickOnboardRequest(req)
	if err != nil {
		return nil, err
	}
	return s.buildPreview(normalized)
}

func (s *QuickOnboardService) Create(ctx context.Context, req QuickOnboardRequest) (*QuickOnboardCreateResult, error) {
	normalized, err := normalizeQuickOnboardRequest(req)
	if err != nil {
		return nil, err
	}
	preview, err := s.buildPreview(normalized)
	if err != nil {
		return nil, err
	}

	result := &QuickOnboardCreateResult{
		ProviderCount:         len(preview.Providers),
		ModelCountPerProvider: len(normalized.Models),
		MatrixCount:           len(preview.Providers) * len(normalized.Models),
		Results:               make([]QuickOnboardProviderResult, 0, len(preview.Providers)),
	}

	for i, plan := range preview.Providers {
		item := normalized.Credentials[i]
		providerResult := QuickOnboardProviderResult{
			Index:      plan.Index,
			Name:       plan.Name,
			ModelCount: len(normalized.Models),
			Status:     "success",
		}
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			encrypted, err := crypto.Encrypt(item.Credentials)
			if err != nil {
				return fmt.Errorf("encrypt credentials: %w", err)
			}
			provider := buildQuickOnboardProvider(normalized, plan.Name, encrypted, item.Remark)
			if err := tx.Create(provider).Error; err != nil {
				return fmt.Errorf("create provider: %w", err)
			}
			providerResult.ProviderID = provider.ID

			models := make([]*model.Model, 0, len(normalized.Models))
			for _, modelInput := range normalized.Models {
				models = append(models, buildQuickOnboardModel(provider.ID, modelInput))
			}
			if err := tx.Omit("Provider", "Catalog").CreateInBatches(models, 100).Error; err != nil {
				return fmt.Errorf("create models: %w", err)
			}
			providerResult.ModelIDs = make([]uuid.UUID, len(models))
			for i := range models {
				providerResult.ModelIDs[i] = models[i].ID
			}
			return nil
		})
		if err != nil {
			providerResult.Status = "failed"
			providerResult.Error = err.Error()
			result.FailedCount++
		} else {
			result.SucceededCount++
		}
		result.Results = append(result.Results, providerResult)
	}

	if result.SucceededCount > 0 {
		s.publishChange(ctx)
	}

	return result, nil
}

func (s *QuickOnboardService) buildPreview(req QuickOnboardRequest) (*QuickOnboardPreview, error) {
	namePrefix := quickOnboardNamePrefix(req)
	modelNames := make([]string, len(req.Models))
	for i := range req.Models {
		modelNames[i] = req.Models[i].Name
	}

	reserved := make(map[string]bool)
	plans := make([]QuickOnboardProviderPlan, 0, len(req.Credentials))
	for i, cred := range req.Credentials {
		name, err := s.nextProviderName(namePrefix, i+1, reserved)
		if err != nil {
			return nil, err
		}
		reserved[name] = true
		plans = append(plans, QuickOnboardProviderPlan{
			Index:      i,
			Name:       name,
			Credential: maskQuickCredential(cred.Credentials),
			Models:     append([]string(nil), modelNames...),
			ModelCount: len(modelNames),
			Remark:     cred.Remark,
			WillCreate: true,
		})
	}

	return &QuickOnboardPreview{
		ProviderCount: len(plans),
		ModelCount:    len(modelNames),
		MatrixCount:   len(plans) * len(modelNames),
		NamePrefix:    namePrefix,
		Providers:     plans,
		Warnings:      quickOnboardWarnings(req),
	}, nil
}

func (s *QuickOnboardService) nextProviderName(prefix string, ordinal int, reserved map[string]bool) (string, error) {
	for suffix := 0; suffix < 1000; suffix++ {
		candidate := fmt.Sprintf("%s-%03d", prefix, ordinal+suffix)
		if reserved[candidate] {
			continue
		}
		var count int64
		if err := s.db.Model(&model.Provider{}).Where("name = ?", candidate).Count(&count).Error; err != nil {
			return "", fmt.Errorf("check provider name: %w", err)
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%w: cannot allocate provider name for prefix %s", ErrQuickOnboardInvalidRequest, prefix)
}

func (s *QuickOnboardService) publishChange(ctx context.Context) {
	snapshotVersion := int64(0)
	if s.snapshotter != nil {
		version, err := s.snapshotter.RefreshAvailableSnapshot(ctx)
		if err != nil {
			slog.Error("failed to refresh provider snapshot after quick onboard", "error", err)
		} else {
			snapshotVersion = version
		}
	}
	if s.publisher == nil {
		return
	}
	ev := event.NewChangeEvent(event.ProviderCreated, "provider_quick_onboard", uuid.Nil, snapshotVersion)
	if err := s.publisher.Publish(ctx, ev); err != nil {
		slog.Error("failed to publish quick onboard event", "error", err)
	}
}

func normalizeQuickOnboardRequest(req QuickOnboardRequest) (QuickOnboardRequest, error) {
	req.Provider.DefinitionID = strings.TrimSpace(req.Provider.DefinitionID)
	req.Provider.Type = strings.TrimSpace(req.Provider.Type)
	req.Provider.Endpoint = strings.TrimSpace(req.Provider.Endpoint)
	req.ProviderSlug = strings.TrimSpace(req.ProviderSlug)
	req.Purpose = strings.TrimSpace(req.Purpose)
	req.NamePrefix = strings.TrimSpace(req.NamePrefix)
	req.Provider.Tag = strings.TrimSpace(req.Provider.Tag)
	req.Provider.Remark = strings.TrimSpace(req.Provider.Remark)
	req.Provider.TestModel = strings.TrimSpace(req.Provider.TestModel)

	if req.Provider.DefinitionID == "" || req.Provider.Endpoint == "" {
		return req, fmt.Errorf("%w: provider definition_id and endpoint are required", ErrQuickOnboardInvalidRequest)
	}
	if len(req.Credentials) == 0 {
		return req, fmt.Errorf("%w: at least one credential is required", ErrQuickOnboardInvalidRequest)
	}
	if len(req.Models) == 0 {
		return req, fmt.Errorf("%w: at least one model is required", ErrQuickOnboardInvalidRequest)
	}

	seenCredentials := make(map[string]bool, len(req.Credentials))
	credentials := make([]QuickOnboardCredentialInput, 0, len(req.Credentials))
	for _, item := range req.Credentials {
		item.Credentials = strings.TrimSpace(item.Credentials)
		item.Remark = strings.TrimSpace(item.Remark)
		if item.Credentials == "" {
			continue
		}
		if seenCredentials[item.Credentials] {
			continue
		}
		seenCredentials[item.Credentials] = true
		credentials = append(credentials, item)
	}
	if len(credentials) == 0 {
		return req, fmt.Errorf("%w: at least one non-empty credential is required", ErrQuickOnboardInvalidRequest)
	}
	req.Credentials = credentials

	seenModels := make(map[string]bool, len(req.Models))
	models := make([]QuickOnboardModelInput, 0, len(req.Models))
	for _, item := range req.Models {
		item.Name = strings.TrimSpace(item.Name)
		item.ModelType = strings.TrimSpace(item.ModelType)
		item.UpstreamModelID = strings.TrimSpace(item.UpstreamModelID)
		item.Description = strings.TrimSpace(item.Description)
		item.PriceMode = strings.TrimSpace(item.PriceMode)
		if item.Name == "" {
			continue
		}
		if seenModels[item.Name] {
			continue
		}
		seenModels[item.Name] = true
		if item.ModelType == "" {
			item.ModelType = inferQuickModelType(item.Name)
		}
		if item.UpstreamModelID == "" {
			item.UpstreamModelID = item.Name
		}
		if item.PriceMode == "" {
			item.PriceMode = "catalog"
		}
		if item.PriceMode == "manual" {
			inputPrice := 0.0
			if item.InputPrice != nil {
				inputPrice = *item.InputPrice
			}
			outputPrice := 0.0
			if item.OutputPrice != nil {
				outputPrice = *item.OutputPrice
			}
			if inputPrice <= 0 && outputPrice <= 0 {
				return req, fmt.Errorf("%w: manual price_mode requires input_price or output_price > 0", ErrQuickOnboardInvalidRequest)
			}
		}
		models = append(models, item)
	}
	if len(models) == 0 {
		return req, fmt.Errorf("%w: at least one non-empty model is required", ErrQuickOnboardInvalidRequest)
	}
	req.Models = models
	return req, nil
}

func buildQuickOnboardProvider(req QuickOnboardRequest, name, encryptedCredentials, credentialRemark string) *model.Provider {
	weight := 1
	if req.Provider.Weight != nil {
		weight = *req.Provider.Weight
	}
	priority := 0
	if req.Provider.Priority != nil {
		priority = *req.Provider.Priority
	}
	maxRetries := 3
	if req.Provider.MaxRetries != nil {
		maxRetries = *req.Provider.MaxRetries
	}
	timeout := 30
	if req.Provider.Timeout != nil {
		timeout = *req.Provider.Timeout
	}
	remark := req.Provider.Remark
	if credentialRemark != "" {
		if remark != "" {
			remark += " "
		}
		remark += credentialRemark
	}
	return &model.Provider{
		ID:                   uuid.New(),
		Name:                 name,
		DefinitionID:         req.Provider.DefinitionID,
		Type:                 req.Provider.Type,
		Endpoint:             req.Provider.Endpoint,
		EncryptedCredentials: encryptedCredentials,
		Enabled:              true,
		Weight:               weight,
		Priority:             priority,
		Status:               model.ProviderStatusEnabled,
		TestModel:            req.Provider.TestModel,
		Remark:               remark,
		MaxRetries:           maxRetries,
		Timeout:              timeout,
		Tag:                  req.Provider.Tag,
	}
}

func buildQuickOnboardModel(providerID uuid.UUID, input QuickOnboardModelInput) *model.Model {
	enabled := true
	weight := 1
	if input.Weight != nil {
		weight = *input.Weight
	}
	m := &model.Model{
		ID:              uuid.New(),
		Name:            input.Name,
		ProviderID:      providerID,
		ModelType:       input.ModelType,
		Enabled:         &enabled,
		Status:          model.ModelStatusEnabled,
		UpstreamModelID: input.UpstreamModelID,
		Description:     input.Description,
		PriceMode:       input.PriceMode,
		Weight:          weight,
	}
	if input.ContextLength != nil {
		m.ContextLength = *input.ContextLength
	}
	if input.MaxOutputTokens != nil {
		m.MaxOutputTokens = *input.MaxOutputTokens
	}
	if input.InputPrice != nil {
		m.InputPrice = *input.InputPrice
	}
	if input.OutputPrice != nil {
		m.OutputPrice = *input.OutputPrice
	}
	return m
}

func quickOnboardNamePrefix(req QuickOnboardRequest) string {
	if req.NamePrefix != "" {
		return slugifyQuickName(req.NamePrefix, "provider")
	}
	providerSlug := req.ProviderSlug
	if providerSlug == "" {
		providerSlug = req.Provider.DefinitionID
	}
	parts := []string{slugifyQuickName(providerSlug, "provider")}
	if purpose := slugifyQuickName(req.Purpose, ""); purpose != "" {
		parts = append(parts, purpose)
	}
	return strings.Join(parts, "-")
}

func slugifyQuickName(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	replacer := strings.NewReplacer("千问", "qwen", "坐席", "seat", "默认", "default", "备用", "backup", "研发", "dev", "视频", "video")
	value = replacer.Replace(value)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	value = strings.Trim(re.ReplaceAllString(value, "-"), "-")
	if value == "" {
		return fallback
	}
	return value
}

func inferQuickModelType(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "rerank"):
		return "rerank"
	case strings.Contains(lower, "embed") || strings.HasPrefix(lower, "text-embedding"):
		return "embedding"
	case strings.Contains(lower, "video"):
		return "video"
	case strings.Contains(lower, "vl") || strings.Contains(lower, "vision"):
		return "embedding_vl"
	default:
		return "chat"
	}
}

func maskQuickCredential(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return crypto.Mask(value)
}

func quickOnboardWarnings(req QuickOnboardRequest) []string {
	warnings := make([]string, 0)
	if len(req.Credentials) > 20 {
		warnings = append(warnings, "一次创建超过 20 个渠道，建议创建后批量测试")
	}
	if req.Provider.Type == "" {
		warnings = append(warnings, "provider.type 未填写，将依赖 definition_id 解析")
	}
	modelTypes := make(map[string]bool)
	for _, m := range req.Models {
		modelTypes[m.ModelType] = true
	}
	if len(modelTypes) > 1 {
		keys := make([]string, 0, len(modelTypes))
		for k := range modelTypes {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		warnings = append(warnings, "本次包含多种模型类型: "+strings.Join(keys, ", "))
	}
	return warnings
}
