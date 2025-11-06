package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

var (
	ErrProviderNameExists = errors.New("provider name already exists")
	ErrProviderNotFound   = errors.New("provider not found")
	ErrProviderHasModels  = errors.New("cannot delete provider with associated models")
	ErrProviderDisabled   = errors.New("provider is disabled")
)

// ProviderRepoInterface ProviderService 所依赖的 ProviderRepo 子集
// 实现者：internal/controlplane/repository.ProviderRepo
type ProviderRepoInterface interface {
	Create(provider *model.Provider) error
	GetByID(id uuid.UUID) (*model.Provider, error)
	List(filter repository.ProviderListFilter, offset, limit int) ([]model.Provider, int64, error)
	Update(provider *model.Provider) error
	UpdateStatus(id uuid.UUID, enabled bool) error
	Delete(id uuid.UUID) error
	ExistsByName(name string) (bool, error)
	ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error)
	UpdateResponseTime(id uuid.UUID, ms int) error
}

// ModelRepoInterface ProviderService/ModelService 所依赖的 ModelRepo 子集
// 实现者：internal/controlplane/repository.ModelRepo
type ModelRepoInterface interface {
	Create(m *model.Model) error
	GetByID(id uuid.UUID) (*model.Model, error)
	List(filter repository.ModelListFilter, offset, limit int) ([]model.Model, int64, error)
	Update(m *model.Model) error
	Delete(id uuid.UUID) error
	ExistsByName(name string) (bool, error)
	ExistsByNameExcluding(name string, excludeID uuid.UUID) (bool, error)
	CountByProviderID(providerID uuid.UUID) (int64, error)
	ListAvailable() ([]model.Model, error)
	UpdateEnabledByProviderID(providerID uuid.UUID, enabled bool) error
	BatchDelete(ids []uuid.UUID) (int64, error)
	BatchUpdateEnabled(ids []uuid.UUID, enabled bool) (int64, error)
	DeleteModelTagRelations(modelID uuid.UUID) error
}

// ProviderService 渠道配置管理服务
type ProviderService struct {
	providerRepo ProviderRepoInterface
	modelRepo    ModelRepoInterface
	publisher    *event.Publisher
}

// NewProviderService 创建 ProviderService 实例
func NewProviderService(providerRepo ProviderRepoInterface, modelRepo ModelRepoInterface, publisher *event.Publisher) *ProviderService {
	return &ProviderService{
		providerRepo: providerRepo,
		modelRepo:    modelRepo,
		publisher:    publisher,
	}
}

// publishEvent 安全发布事件（publisher 为 nil 或发布失败不影响业务）
func (s *ProviderService) publishEvent(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	if s.publisher == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID)
	if err := s.publisher.Publish(context.Background(), ev); err != nil {
		slog.Error("failed to publish event", "type", eventType, "resource_id", resourceID, "error", err)
	}
}

// CreateProviderInput 创建渠道的输入参数
//
// 字段语义：
//   - 必填：Name、DefinitionID、Endpoint、Credentials
//   - 可选标量字段（Type/TestModel/Remark/Tag）：未传按零值（""）落库
//   - 可选数值字段（Weight/Priority/MaxRetries/Timeout）：指针类型用于区分
//     "未传" 与 "传零值"；未传时落 DB 列默认值（weight=1/priority=0/max_retries=3/timeout=30）
type CreateProviderInput struct {
	Name         string `json:"name" binding:"required,max=100"`
	DefinitionID string `json:"definition_id" binding:"required,max=50"`
	Type         string `json:"type" binding:"omitempty,max=50"`
	Endpoint     string `json:"endpoint" binding:"required,max=500"`
	Credentials  string `json:"credentials" binding:"required"`
	Weight       *int   `json:"weight" binding:"omitempty,min=0,max=9999"`
	Priority     *int   `json:"priority" binding:"omitempty,min=0,max=9999"`
	TestModel    string `json:"test_model" binding:"omitempty,max=200"`
	Remark       string `json:"remark" binding:"omitempty,max=500"`
	MaxRetries   *int   `json:"max_retries" binding:"omitempty,min=0,max=10"`
	Timeout      *int   `json:"timeout" binding:"omitempty,min=1,max=300"`
	Tag          string `json:"tag" binding:"omitempty,max=100"`
}

// CreateProvider 创建渠道（T020）
func (s *ProviderService) CreateProvider(input CreateProviderInput) (*model.Provider, error) {
	exists, err := s.providerRepo.ExistsByName(input.Name)
	if err != nil {
		return nil, fmt.Errorf("check name uniqueness: %w", err)
	}
	if exists {
		return nil, ErrProviderNameExists
	}

	encrypted, err := crypto.Encrypt(input.Credentials)
	if err != nil {
		return nil, fmt.Errorf("encrypt credentials: %w", err)
	}

	provider := &model.Provider{
		Name:                 input.Name,
		DefinitionID:         input.DefinitionID,
		Type:                 input.Type,
		Endpoint:             input.Endpoint,
		EncryptedCredentials: encrypted,
		Enabled:              true,
		TestModel:            input.TestModel,
		Remark:               input.Remark,
		Tag:                  input.Tag,
	}
	if input.Weight != nil {
		provider.Weight = *input.Weight
	} else {
		provider.Weight = 1
	}
	if input.Priority != nil {
		provider.Priority = *input.Priority
	}
	if input.MaxRetries != nil {
		provider.MaxRetries = *input.MaxRetries
	} else {
		provider.MaxRetries = 3
	}
	if input.Timeout != nil {
		provider.Timeout = *input.Timeout
	} else {
		provider.Timeout = 30
	}

	if err := s.providerRepo.Create(provider); err != nil {
		return nil, fmt.Errorf("create provider: %w", err)
	}

	s.publishEvent(event.ProviderCreated, "provider", provider.ID)

	return provider, nil
}

// ProviderResponse 渠道响应（凭证脱敏）
type ProviderResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	DefinitionID      string    `json:"definition_id"`
	Type              string    `json:"type"`
	Endpoint          string    `json:"endpoint"`
	MaskedCredentials string    `json:"credentials"`
	Enabled           bool      `json:"enabled"`
	Weight            int       `json:"weight"`
	Priority          int       `json:"priority"`
	Status            int       `json:"status"`
	TestModel         string    `json:"test_model"`
	ResponseTime      int       `json:"response_time"`
	Balance           float64   `json:"balance"`
	Remark            string    `json:"remark"`
	MaxRetries           int       `json:"max_retries"`
	Timeout              int       `json:"timeout"`
	Tag                  string    `json:"tag"`
	ConsecutiveFailures  int       `json:"consecutive_failures"`
	LastCheckError       string    `json:"last_check_error"`
	CreatedAt            string    `json:"created_at"`
	UpdatedAt            string    `json:"updated_at"`
}

// toResponse 将 Provider 转为脱敏响应
func (s *ProviderService) toResponse(p *model.Provider) ProviderResponse {
	// 解密凭证用于脱敏展示
	decrypted, err := crypto.Decrypt(p.EncryptedCredentials)
	masked := "****"
	if err == nil {
		masked = maskCredentialJSON(decrypted)
	}

	return ProviderResponse{
		ID:                p.ID,
		Name:              p.Name,
		DefinitionID:      p.DefinitionID,
		Type:              p.Type,
		Endpoint:          p.Endpoint,
		MaskedCredentials: masked,
		Enabled:           p.Enabled,
		Weight:            p.Weight,
		Priority:          p.Priority,
		Status:            p.Status,
		TestModel:         p.TestModel,
		ResponseTime:      p.ResponseTime,
		Balance:           p.Balance,
		Remark:            p.Remark,
		MaxRetries:        p.MaxRetries,
		Timeout:           p.Timeout,
		Tag:                  p.Tag,
		ConsecutiveFailures:  p.ConsecutiveFailures,
		LastCheckError:       p.LastCheckError,
		CreatedAt:         p.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:         p.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
}

// GetProvider 查询单个渠道（T021）
func (s *ProviderService) GetProvider(id uuid.UUID) (*ProviderResponse, error) {
	provider, err := s.providerRepo.GetByID(id)
	if err != nil {
		return nil, ErrProviderNotFound
	}
	resp := s.toResponse(provider)
	return &resp, nil
}

// ListProviders 查询渠道列表（T021）
func (s *ProviderService) ListProviders(filter repository.ProviderListFilter, page, pageSize int) ([]ProviderResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	providers, total, err := s.providerRepo.List(filter, offset, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list providers: %w", err)
	}

	responses := make([]ProviderResponse, len(providers))
	for i := range providers {
		responses[i] = s.toResponse(&providers[i])
	}

	return responses, total, nil
}

// UpdateProviderInput 更新渠道的输入参数
//
// 所有字段都用指针 / "" 表示 "未传"，仅在显式传入时才会覆盖。
// 数值字段指针化是为了区分 "用户没动" 和 "用户主动改为 0"。
// Credentials 使用指针以区分"未传"和"主动更新"，如果传入则必须非空
type UpdateProviderInput struct {
	Name         string  `json:"name" binding:"omitempty,max=100"`
	DefinitionID string  `json:"definition_id" binding:"omitempty,max=50"`
	Type         string  `json:"type" binding:"omitempty,max=50"`
	Endpoint     string  `json:"endpoint" binding:"omitempty,max=500"`
	Credentials  *string `json:"credentials" binding:"omitempty"`
	Weight       *int    `json:"weight" binding:"omitempty,min=0,max=9999"`
	Priority     *int    `json:"priority" binding:"omitempty,min=0,max=9999"`
	TestModel    *string `json:"test_model" binding:"omitempty,max=200"`
	Remark       *string `json:"remark" binding:"omitempty,max=500"`
	MaxRetries   *int    `json:"max_retries" binding:"omitempty,min=0,max=10"`
	Timeout      *int    `json:"timeout" binding:"omitempty,min=1,max=300"`
	Tag          *string `json:"tag" binding:"omitempty,max=100"`
}

// UpdateProvider 更新渠道（T022）
func (s *ProviderService) UpdateProvider(id uuid.UUID, input UpdateProviderInput) (*ProviderResponse, error) {
	provider, err := s.providerRepo.GetByID(id)
	if err != nil {
		return nil, ErrProviderNotFound
	}

	if input.Name != "" && input.Name != provider.Name {
		exists, err := s.providerRepo.ExistsByNameExcluding(input.Name, id)
		if err != nil {
			return nil, fmt.Errorf("check name uniqueness: %w", err)
		}
		if exists {
			return nil, ErrProviderNameExists
		}
		provider.Name = input.Name
	}

	if input.Type != "" {
		provider.Type = input.Type
	}
	if input.DefinitionID != "" {
		provider.DefinitionID = input.DefinitionID
	}
	if input.Endpoint != "" {
		provider.Endpoint = input.Endpoint
	}
	if input.Credentials != nil {
		encrypted, err := crypto.Encrypt(*input.Credentials)
		if err != nil {
			return nil, fmt.Errorf("encrypt credentials: %w", err)
		}
		provider.EncryptedCredentials = encrypted
	}
	if input.Weight != nil {
		provider.Weight = *input.Weight
	}
	if input.Priority != nil {
		provider.Priority = *input.Priority
	}
	if input.TestModel != nil {
		provider.TestModel = *input.TestModel
	}
	if input.Remark != nil {
		provider.Remark = *input.Remark
	}
	if input.MaxRetries != nil {
		provider.MaxRetries = *input.MaxRetries
	}
	if input.Timeout != nil {
		provider.Timeout = *input.Timeout
	}
	if input.Tag != nil {
		provider.Tag = *input.Tag
	}

	if err := s.providerRepo.Update(provider); err != nil {
		return nil, fmt.Errorf("update provider: %w", err)
	}

	s.publishEvent(event.ProviderUpdated, "provider", provider.ID)

	resp := s.toResponse(provider)
	return &resp, nil
}

// ToggleProviderStatus 启用/禁用渠道（T023）
func (s *ProviderService) ToggleProviderStatus(id uuid.UUID, enabled bool) (*ProviderResponse, error) {
	provider, err := s.providerRepo.GetByID(id)
	if err != nil {
		return nil, ErrProviderNotFound
	}

	// 更新渠道状态
	if err := s.providerRepo.UpdateStatus(id, enabled); err != nil {
		return nil, fmt.Errorf("update provider status: %w", err)
	}
	provider.Enabled = enabled

	// 级联更新下属模型启用状态
	if err := s.modelRepo.UpdateEnabledByProviderID(id, enabled); err != nil {
		return nil, fmt.Errorf("cascade update models: %w", err)
	}

	if enabled {
		s.publishEvent(event.ProviderEnabled, "provider", id)
	} else {
		s.publishEvent(event.ProviderDisabled, "provider", id)
	}

	resp := s.toResponse(provider)
	return &resp, nil
}

// DeleteProvider 删除渠道（T024）
func (s *ProviderService) DeleteProvider(id uuid.UUID) error {
	// 检查渠道是否存在
	_, err := s.providerRepo.GetByID(id)
	if err != nil {
		return ErrProviderNotFound
	}

	// 检查是否有关联模型
	count, err := s.modelRepo.CountByProviderID(id)
	if err != nil {
		return fmt.Errorf("count models: %w", err)
	}
	if count > 0 {
		return ErrProviderHasModels
	}

	if err := s.providerRepo.Delete(id); err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}

	s.publishEvent(event.ProviderDeleted, "provider", id)

	return nil
}

// maskCredentialJSON 对 JSON 格式凭证中的每个值单独脱敏
// 输入: {"api_key": "sk-REDACTED"}
// 输出: {"api_key": "sk-703ff****a13f"}
func maskCredentialJSON(decrypted string) string {
	var creds map[string]string
	if err := json.Unmarshal([]byte(decrypted), &creds); err != nil {
		// 非 JSON 格式，直接对整体脱敏
		return crypto.Mask(decrypted)
	}

	masked := make(map[string]string)
	for k, v := range creds {
		masked[k] = crypto.Mask(v)
	}

	result, _ := json.Marshal(masked)
	return string(result)
}
