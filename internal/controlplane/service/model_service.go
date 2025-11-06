package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

var (
	ErrModelNameExists      = errors.New("model name already exists")
	ErrModelNotFound        = errors.New("model not found")
	ErrModelProviderInvalid = errors.New("associated provider not found or disabled")
)

// ModelService 模型注册管理服务
type ModelService struct {
	modelRepo    ModelRepoInterface
	providerRepo ProviderRepoInterface
	publisher    *event.Publisher
}

// NewModelService 创建 ModelService 实例
func NewModelService(modelRepo ModelRepoInterface, providerRepo ProviderRepoInterface, publisher *event.Publisher) *ModelService {
	return &ModelService{
		modelRepo:    modelRepo,
		providerRepo: providerRepo,
		publisher:    publisher,
	}
}

// publishEvent 安全发布事件（publisher 为 nil 或发布失败不影响业务）
func (s *ModelService) publishEvent(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	if s.publisher == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID)
	if err := s.publisher.Publish(context.Background(), ev); err != nil {
		slog.Error("failed to publish event", "type", eventType, "resource_id", resourceID, "error", err)
	}
}

// CreateModelInput 创建模型的输入参数
//
// 字段语义：
//   - 必填：Name、ProviderID、ModelType
//   - 可选标量字段（UpstreamModelID/Description）：未传按 ""
//   - 可选数值字段（ContextLength/MaxOutputTokens/InputPrice/OutputPrice/Weight）：
//     指针类型用于区分 "未传" 与 "传零值"；未传时落 DB 列默认值
//     （context_length=0/max_output_tokens=0/input_price=0/output_price=0/weight=1）
type CreateModelInput struct {
	Name            string   `json:"name" binding:"required,max=200"`
	ProviderID      string   `json:"provider_id" binding:"required,uuid"`
	ModelType       string   `json:"model_type" binding:"required,oneof=chat embedding"`
	UpstreamModelID string   `json:"upstream_model_id" binding:"omitempty,max=200"`
	Description     string   `json:"description" binding:"omitempty,max=500"`
	ContextLength   *int     `json:"context_length" binding:"omitempty,min=0,max=10000000"`
	MaxOutputTokens *int     `json:"max_output_tokens" binding:"omitempty,min=0,max=10000000"`
	InputPrice      *float64 `json:"input_price" binding:"omitempty,min=0"`
	OutputPrice     *float64 `json:"output_price" binding:"omitempty,min=0"`
	Weight          *int     `json:"weight" binding:"omitempty,min=0,max=9999"`
}

// CreateModel 注册模型（T030）
func (s *ModelService) CreateModel(input CreateModelInput) (*model.Model, error) {
	exists, err := s.modelRepo.ExistsByName(input.Name)
	if err != nil {
		return nil, fmt.Errorf("check name uniqueness: %w", err)
	}
	if exists {
		return nil, ErrModelNameExists
	}

	providerID, _ := uuid.Parse(input.ProviderID)
	provider, err := s.providerRepo.GetByID(providerID)
	if err != nil || provider == nil {
		return nil, ErrModelProviderInvalid
	}
	if !provider.Enabled {
		return nil, ErrModelProviderInvalid
	}

	enabled := true
	m := &model.Model{
		Name:            input.Name,
		ProviderID:      providerID,
		ModelType:       input.ModelType,
		Enabled:         &enabled,
		UpstreamModelID: input.UpstreamModelID,
		Description:     input.Description,
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
	if input.Weight != nil {
		m.Weight = *input.Weight
	} else {
		m.Weight = 1
	}

	if err := s.modelRepo.Create(m); err != nil {
		return nil, fmt.Errorf("create model: %w", err)
	}

	s.publishEvent(event.ModelCreated, "model", m.ID)

	return m, nil
}

// CheckModelName 检查模型名称是否可用（S0-3 修复）
//
// 单独路由 GET /api/v1/models/check-name?name=X 服务，避免 ListModels 端点
// 通过 check_name query 隐式重载导致 FE 永远拿不到 available 字段。
//
// 返回 (available, error)：true 表示名称可用。
func (s *ModelService) CheckModelName(name string) (bool, error) {
	exists, err := s.modelRepo.ExistsByName(name)
	if err != nil {
		return false, fmt.Errorf("check name uniqueness: %w", err)
	}
	return !exists, nil
}

// ListModels 查询模型列表（T031）
func (s *ModelService) ListModels(filter repository.ModelListFilter, page, pageSize int) ([]model.Model, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	return s.modelRepo.List(filter, offset, pageSize)
}

// GetModel 查询单个模型
func (s *ModelService) GetModel(id uuid.UUID) (*model.Model, error) {
	m, err := s.modelRepo.GetByID(id)
	if err != nil {
		return nil, ErrModelNotFound
	}
	return m, nil
}

// UpdateModelInput 更新模型的输入参数
//
// 字段同 CreateModelInput；标量字段使用 *string 以便区分 "未传" 与 "传空串清空"。
type UpdateModelInput struct {
	Name            string   `json:"name" binding:"omitempty,max=200"`
	ProviderID      string   `json:"provider_id" binding:"omitempty,uuid"`
	ModelType       string   `json:"model_type" binding:"omitempty,oneof=chat embedding"`
	Enabled         *bool    `json:"enabled"`
	UpstreamModelID *string  `json:"upstream_model_id" binding:"omitempty,max=200"`
	Description     *string  `json:"description" binding:"omitempty,max=500"`
	ContextLength   *int     `json:"context_length" binding:"omitempty,min=0,max=10000000"`
	MaxOutputTokens *int     `json:"max_output_tokens" binding:"omitempty,min=0,max=10000000"`
	InputPrice      *float64 `json:"input_price" binding:"omitempty,min=0"`
	OutputPrice     *float64 `json:"output_price" binding:"omitempty,min=0"`
	Weight          *int     `json:"weight" binding:"omitempty,min=0,max=9999"`
}

// UpdateModel 更新模型（T032）
func (s *ModelService) UpdateModel(id uuid.UUID, input UpdateModelInput) (*model.Model, error) {
	m, err := s.modelRepo.GetByID(id)
	if err != nil {
		return nil, ErrModelNotFound
	}

	if input.Name != "" && input.Name != m.Name {
		exists, err := s.modelRepo.ExistsByNameExcluding(input.Name, id)
		if err != nil {
			return nil, fmt.Errorf("check name uniqueness: %w", err)
		}
		if exists {
			return nil, ErrModelNameExists
		}
		m.Name = input.Name
	}

	if input.ProviderID != "" {
		providerID, _ := uuid.Parse(input.ProviderID)
		provider, err := s.providerRepo.GetByID(providerID)
		if err != nil || provider == nil {
			return nil, ErrModelProviderInvalid
		}
		m.ProviderID = providerID
	}

	if input.ModelType != "" {
		m.ModelType = input.ModelType
	}

	if input.Enabled != nil {
		m.Enabled = input.Enabled
	}
	if input.UpstreamModelID != nil {
		m.UpstreamModelID = *input.UpstreamModelID
	}
	if input.Description != nil {
		m.Description = *input.Description
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
	if input.Weight != nil {
		m.Weight = *input.Weight
	}

	if err := s.modelRepo.Update(m); err != nil {
		return nil, fmt.Errorf("update model: %w", err)
	}

	s.publishEvent(event.ModelUpdated, "model", m.ID)

	return m, nil
}

// DeleteModel 删除模型（T033）
func (s *ModelService) DeleteModel(id uuid.UUID) error {
	_, err := s.modelRepo.GetByID(id)
	if err != nil {
		return ErrModelNotFound
	}

	// 清理模型标签关联
	if err := s.modelRepo.DeleteModelTagRelations(id); err != nil {
		return fmt.Errorf("delete model tag relations: %w", err)
	}

	if err := s.modelRepo.Delete(id); err != nil {
		return fmt.Errorf("delete model: %w", err)
	}

	s.publishEvent(event.ModelDeleted, "model", id)

	return nil
}

// BatchDeleteModels 批量删除模型
func (s *ModelService) BatchDeleteModels(ids []uuid.UUID) (int64, error) {
	affected, err := s.modelRepo.BatchDelete(ids)
	if err != nil {
		return 0, fmt.Errorf("batch delete models: %w", err)
	}
	for _, id := range ids {
		s.publishEvent(event.ModelDeleted, "model", id)
	}
	return affected, nil
}

// BatchUpdateModelStatus 批量更新模型启用状态
func (s *ModelService) BatchUpdateModelStatus(ids []uuid.UUID, enabled bool) (int64, error) {
	affected, err := s.modelRepo.BatchUpdateEnabled(ids, enabled)
	if err != nil {
		return 0, fmt.Errorf("batch update model status: %w", err)
	}
	for _, id := range ids {
		s.publishEvent(event.ModelUpdated, "model", id)
	}
	return affected, nil
}
