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

// CatalogLookup ModelService 用来读 catalog 快照的最小接口（只读）。
//
// 这里没有直接依赖 *repository.ModelCatalogRepo 是为了：
//  1. ModelService 单测不必拉 sqlite，注入一个内存 stub 即可；
//  2. 未来 catalog 加 Redis 缓存装饰器时可以无侵入挂上来。
type CatalogLookup interface {
	GetByID(id uuid.UUID) (*model.ModelCatalog, error)
	GetByIDs(ids []uuid.UUID) (map[uuid.UUID]*model.ModelCatalog, error)
}

// ModelService 模型注册管理服务
type ModelService struct {
	modelRepo     ModelRepoInterface
	providerRepo  ProviderRepoInterface
	publisher     *event.Publisher
	snapshotter   *ProviderSnapshotRefresher
	catalogLookup CatalogLookup // 可选，未注入时 ResetOverride / CreateModelFromCatalog 走 fallback / 报错
}

// NewModelService 创建 ModelService 实例
func NewModelService(modelRepo ModelRepoInterface, providerRepo ProviderRepoInterface, publisher *event.Publisher) *ModelService {
	return &ModelService{
		modelRepo:    modelRepo,
		providerRepo: providerRepo,
		publisher:    publisher,
	}
}

// SetSnapshotRefresher injects the shared provider snapshot refresher.
func (s *ModelService) SetSnapshotRefresher(snapshotter *ProviderSnapshotRefresher) {
	s.snapshotter = snapshotter
}

// SetCatalogLookup 注入 catalog 只读依赖。生产入口由 main.go wiring；
// 测试可以传任意实现 GetByID 的对象。
func (s *ModelService) SetCatalogLookup(lookup CatalogLookup) {
	s.catalogLookup = lookup
}

// publishEvent 安全发布事件（publisher 为 nil 或发布失败不影响业务）
func (s *ModelService) publishEvent(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	snapshotVersion := int64(0)
	if s.snapshotter != nil {
		version, err := s.snapshotter.RefreshAvailableSnapshot(context.Background())
		if err != nil {
			slog.Error("failed to refresh provider snapshot", "type", eventType, "resource_id", resourceID, "error", err)
		} else {
			snapshotVersion = version
		}
	}
	if s.publisher == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID, snapshotVersion)
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
	providerID, _ := uuid.Parse(input.ProviderID)
	provider, err := s.providerRepo.GetByID(providerID)
	if err != nil || provider == nil {
		return nil, ErrModelProviderInvalid
	}
	if !provider.Enabled {
		return nil, ErrModelProviderInvalid
	}

	exists, err := s.modelRepo.ExistsByName(input.Name, providerID)
	if err != nil {
		return nil, fmt.Errorf("check name uniqueness: %w", err)
	}
	if exists {
		return nil, ErrModelNameExists
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

// CheckModelName 检查模型名称在指定渠道下是否可用（S0-3 修复）
//
// 单独路由 GET /api/v1/models/check-name?name=X&provider_id=Y 服务，避免 ListModels 端点
// 通过 check_name query 隐式重载导致 FE 永远拿不到 available 字段。
//
// 返回 (available, error)：true 表示名称可用。
func (s *ModelService) CheckModelName(name string, providerID uuid.UUID) (bool, error) {
	exists, err := s.modelRepo.ExistsByName(name, providerID)
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
		exists, err := s.modelRepo.ExistsByNameExcluding(input.Name, m.ProviderID, id)
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
		// 管理员显式切换启停后，清空失败状态，避免被健康检测按旧失败计数立即抢回。
		m.ConsecutiveFailures = 0
		m.LastCheckError = ""
	}
	if input.UpstreamModelID != nil {
		m.UpstreamModelID = *input.UpstreamModelID
	}
	if input.Description != nil {
		m.Description = *input.Description
	}
	if input.ContextLength != nil {
		// D10：admin 改 context_length 且模型挂着 catalog → 自动锁定（防止后续 auto sync 被覆盖）
		if m.CatalogID != nil && *input.ContextLength != m.ContextLength {
			m.CtxOverridden = true
		}
		m.ContextLength = *input.ContextLength
	}
	if input.MaxOutputTokens != nil {
		if m.CatalogID != nil && *input.MaxOutputTokens != m.MaxOutputTokens {
			m.MaxOutputOverridden = true
		}
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

// CreateModelFromCatalog 从 catalog 复制一份快照建出 model。
//
// 字段拷贝策略（D10）：
//   - context_length / max_output_tokens / input_price / output_price 全部从 catalog 复制；
//   - upstream_model_id 复制 catalog.SourceModelID（admin 之后可手工改）；
//   - 不设 ctx_overridden / max_output_overridden（默认 false），让自动同步任务正常工作；
//   - description = catalog.DisplayName（占位，admin 可改）。
func (s *ModelService) CreateModelFromCatalog(catalogID, providerID uuid.UUID, name string) (*model.Model, error) {
	if s.catalogLookup == nil {
		return nil, fmt.Errorf("catalog lookup not configured")
	}
	cat, err := s.catalogLookup.GetByID(catalogID)
	if err != nil {
		return nil, fmt.Errorf("lookup catalog: %w", err)
	}
	if cat == nil {
		return nil, fmt.Errorf("catalog %s not found", catalogID)
	}
	provider, err := s.providerRepo.GetByID(providerID)
	if err != nil || provider == nil {
		return nil, ErrModelProviderInvalid
	}
	if !provider.Enabled {
		return nil, ErrModelProviderInvalid
	}
	exists, err := s.modelRepo.ExistsByName(name, providerID)
	if err != nil {
		return nil, fmt.Errorf("check name uniqueness: %w", err)
	}
	if exists {
		return nil, ErrModelNameExists
	}

	enabled := true
	catID := cat.ID
	m := &model.Model{
		Name:            name,
		ProviderID:      providerID,
		ModelType:       cat.Mode,
		Enabled:         &enabled,
		UpstreamModelID: cat.SourceModelID,
		Description:     cat.DisplayName,
		ContextLength:   int(cat.ContextLength),
		MaxOutputTokens: int(cat.MaxOutputTokens),
		InputPrice:      cat.InputPricePerMillion,
		OutputPrice:     cat.OutputPricePerMillion,
		Weight:          1,
		CatalogID:       &catID,
	}
	if m.ModelType == "" {
		m.ModelType = "chat"
	}
	if err := s.modelRepo.Create(m); err != nil {
		return nil, fmt.Errorf("create model: %w", err)
	}
	s.publishEvent(event.ModelCreated, "model", m.ID)
	return m, nil
}

// ResetOverride 解除某字段的 override flag，并把字段值重置为 catalog 当前值。
//
// 支持的 field：
//   - "context_length"
//   - "max_output_tokens"
//
// 其它字段名一律 400。
func (s *ModelService) ResetOverride(modelID uuid.UUID, field string) error {
	m, err := s.modelRepo.GetByID(modelID)
	if err != nil || m == nil {
		return ErrModelNotFound
	}
	if m.CatalogID == nil {
		return fmt.Errorf("model %s has no catalog link", modelID)
	}
	if s.catalogLookup == nil {
		return fmt.Errorf("catalog lookup not configured")
	}
	cat, err := s.catalogLookup.GetByID(*m.CatalogID)
	if err != nil || cat == nil {
		return fmt.Errorf("catalog for model %s not found", modelID)
	}
	switch field {
	case "context_length":
		m.CtxOverridden = false
		m.ContextLength = int(cat.ContextLength)
	case "max_output_tokens":
		m.MaxOutputOverridden = false
		m.MaxOutputTokens = int(cat.MaxOutputTokens)
	default:
		return fmt.Errorf("unsupported override field: %s", field)
	}
	if err := s.modelRepo.Update(m); err != nil {
		return fmt.Errorf("update model: %w", err)
	}
	s.publishEvent(event.ModelUpdated, "model", m.ID)
	return nil
}

// ===================== Catalog 绑定/解绑 =====================
//
// 设计要点（D1=C / D2=A）：
//   - BindCatalog 是绑定/换绑统一入口；reset_fields=true 时把价格/容量/上游 ID 全部回写为新 catalog 当前值。
//   - UnbindCatalog 把 catalog_id 设为 NULL，并清掉 ctx_overridden / max_output_overridden（脱钩后这两个标志位无意义）。
//   - 价格快照保留：unbind 后本地价不动；admin 想清零得显式 PUT。
//   - BatchBind 用事务，要么全成功要么全回滚，避免部分写入难调试。

// ErrCatalogAlreadyBoundToSame 幂等保护：当 model 已经绑定在同一个 catalog 上时再 bind 报错。
var ErrCatalogAlreadyBoundToSame = errors.New("model already bound to the same catalog")

// ErrCatalogNotBound model.catalog_id == NULL 时尝试 unbind 报错。
var ErrCatalogNotBound = errors.New("model has no catalog link")

// BindCatalog 把指定 model 关联到 catalog 条目；catalog_id 已绑定时变成"换绑"。
//
// resetFields=true 时同步把 InputPrice / OutputPrice / ContextLength / MaxOutputTokens / UpstreamModelID
// 重置为新 catalog 的当前值，并把两个 *_overridden 标志位重置为 false。
// resetFields=false 时仅切换 catalog_id，本地价格/容量保持原状（之后会出现在 diff 列表里）。
func (s *ModelService) BindCatalog(modelID, catalogID uuid.UUID, resetFields bool) (*model.Model, error) {
	m, err := s.modelRepo.GetByID(modelID)
	if err != nil || m == nil {
		return nil, ErrModelNotFound
	}
	if m.CatalogID != nil && *m.CatalogID == catalogID && !resetFields {
		// 同一个 catalog + 不需要 reset 等于无操作，避免 admin 反复点
		return nil, ErrCatalogAlreadyBoundToSame
	}
	if s.catalogLookup == nil {
		return nil, fmt.Errorf("catalog lookup not configured")
	}
	cat, err := s.catalogLookup.GetByID(catalogID)
	if err != nil {
		return nil, fmt.Errorf("lookup catalog: %w", err)
	}
	if cat == nil {
		return nil, fmt.Errorf("catalog %s not found", catalogID)
	}

	// 必填：写入新 catalog_id；标志位无论 reset 与否都重置为 false（语义：admin 显式选了新关联）
	cid := cat.ID
	m.CatalogID = &cid
	m.CtxOverridden = false
	m.MaxOutputOverridden = false

	if resetFields {
		m.InputPrice = cat.InputPricePerMillion
		m.OutputPrice = cat.OutputPricePerMillion
		m.ContextLength = int(cat.ContextLength)
		m.MaxOutputTokens = int(cat.MaxOutputTokens)
		m.UpstreamModelID = cat.SourceModelID
	}

	if err := s.modelRepo.Update(m); err != nil {
		return nil, fmt.Errorf("update model: %w", err)
	}
	s.publishEvent(event.ModelUpdated, "model", m.ID)
	return m, nil
}

// UnbindCatalog 解除 model 与 catalog 的关联：catalog_id → NULL，标志位清零；价格/容量保留 snapshot。
func (s *ModelService) UnbindCatalog(modelID uuid.UUID) (*model.Model, error) {
	m, err := s.modelRepo.GetByID(modelID)
	if err != nil || m == nil {
		return nil, ErrModelNotFound
	}
	if m.CatalogID == nil {
		return nil, ErrCatalogNotBound
	}
	m.CatalogID = nil
	m.CtxOverridden = false
	m.MaxOutputOverridden = false
	if err := s.modelRepo.Update(m); err != nil {
		return nil, fmt.Errorf("update model: %w", err)
	}
	s.publishEvent(event.ModelUpdated, "model", m.ID)
	return m, nil
}

// BatchBindInput 批量绑定的单条输入。
type BatchBindInput struct {
	ModelID     uuid.UUID
	CatalogID   uuid.UUID
	ResetFields bool
}

// BatchBindResult BatchBindCatalog 的汇总。
type BatchBindResult struct {
	Applied   int                 `json:"applied"`
	FailedIDs map[string]string   `json:"failed_ids"` // model_id -> 错误原因（admin 可看）
}

// BatchBindCatalog 批量绑定。
//
// 容错策略：单条失败不阻塞剩余条；最终返回 applied 数 + 每条失败的 model_id → reason 映射。
// 内部对每条独立执行 BindCatalog，复用其校验/事件逻辑；不放在单个事务里，避免一条 catalog 不存在
// 把整批回滚（admin 视角"我有 38 条本来能成功的为什么也没绑上？"更不可接受）。
func (s *ModelService) BatchBindCatalog(items []BatchBindInput) (*BatchBindResult, error) {
	if len(items) == 0 {
		return nil, errors.New("empty bindings")
	}

	modelIDs := make([]uuid.UUID, len(items))
	catalogIDs := make([]uuid.UUID, len(items))
	for i, it := range items {
		modelIDs[i] = it.ModelID
		catalogIDs[i] = it.CatalogID
	}

	modelMap, err := s.modelRepo.GetByIDs(modelIDs)
	if err != nil {
		return nil, fmt.Errorf("batch fetch models: %w", err)
	}

	catalogMap, err := s.catalogLookup.GetByIDs(catalogIDs)
	if err != nil {
		return nil, fmt.Errorf("batch fetch catalogs: %w", err)
	}

	res := &BatchBindResult{FailedIDs: map[string]string{}}
	for _, it := range items {
		m := modelMap[it.ModelID]
		if m == nil {
			res.FailedIDs[it.ModelID.String()] = ErrModelNotFound.Error()
			continue
		}
		if m.CatalogID != nil && *m.CatalogID == it.CatalogID && !it.ResetFields {
			res.FailedIDs[it.ModelID.String()] = ErrCatalogAlreadyBoundToSame.Error()
			continue
		}
		cat := catalogMap[it.CatalogID]
		if cat == nil {
			res.FailedIDs[it.ModelID.String()] = fmt.Sprintf("catalog %s not found", it.CatalogID)
			continue
		}

		cid := cat.ID
		m.CatalogID = &cid
		m.CtxOverridden = false
		m.MaxOutputOverridden = false

		if it.ResetFields {
			m.InputPrice = cat.InputPricePerMillion
			m.OutputPrice = cat.OutputPricePerMillion
			m.ContextLength = int(cat.ContextLength)
			m.MaxOutputTokens = int(cat.MaxOutputTokens)
			m.UpstreamModelID = cat.SourceModelID
		}

		if err := s.modelRepo.Update(m); err != nil {
			res.FailedIDs[it.ModelID.String()] = fmt.Errorf("update model: %w", err).Error()
			continue
		}
		s.publishEvent(event.ModelUpdated, "model", it.ModelID)
		res.Applied++
	}
	return res, nil
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
