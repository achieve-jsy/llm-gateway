package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrGroupNotFound          = errors.New("group not found")
	ErrGroupNameExists        = errors.New("group name already exists")
	ErrGroupDeleted           = errors.New("group has been deleted")
	ErrGroupModelTypeMismatch = errors.New("group models must share the same model_type")
)

type FailoverDefaults struct {
	MaxSwitches      int
	SameModelRetries int
	CooldownSeconds  int
}

type GroupService struct {
	repo              *repository.GroupRepo
	modelRepo         *repository.ModelRepo
	failoverDefaults  *FailoverDefaults
	modelTierRepo     ModelTierRepository
	snapshotPublisher BudgetConfigPublisher
}

func (s *GroupService) WithBudgetTierSnapshotPublisher(p BudgetConfigPublisher) *GroupService {
	s.snapshotPublisher = p
	return s
}

func (s *GroupService) WithModelTierRepo(repo ModelTierRepository) *GroupService {
	s.modelTierRepo = repo
	return s
}

func (s *GroupService) SetBudgetTier(ctx context.Context, groupID uint, tierID *uuid.UUID, reason string) error {
	if err := validateReason(reason); err != nil {
		return err
	}
	if err := s.validateBudgetTier(ctx, tierID); err != nil {
		return err
	}
	if err := s.repo.SetBudgetTier(ctx, groupID, tierID); err != nil {
		if errors.Is(err, repository.ErrGroupNotFound) {
			return ErrGroupNotFound
		}
		return err
	}
	if s.snapshotPublisher != nil {
		if err := s.snapshotPublisher.MarkDirtyAndPublish(ctx); err != nil {
			slog.ErrorContext(ctx, "budget snapshot publish failed after group tier write",
				"group_id", groupID, "error", err)
			return fmt.Errorf("%w: %v", ErrBudgetConfigSyncPending, err)
		}
	}
	return nil
}

func (s *GroupService) SetBudgetTierBatch(ctx context.Context, groupIDs []uint, tierID *uuid.UUID, reason string) error {
	if err := validateReason(reason); err != nil {
		return err
	}
	if err := s.validateBudgetTier(ctx, tierID); err != nil {
		return err
	}
	if err := s.repo.SetBudgetTierBatch(ctx, groupIDs, tierID); err != nil {
		if errors.Is(err, repository.ErrGroupNotFound) {
			return ErrGroupNotFound
		}
		return err
	}
	if s.snapshotPublisher != nil {
		if err := s.snapshotPublisher.MarkDirtyAndPublish(ctx); err != nil {
			slog.ErrorContext(ctx, "budget snapshot publish failed after group tier batch write",
				"group_ids", groupIDs, "error", err)
			return fmt.Errorf("%w: %v", ErrBudgetConfigSyncPending, err)
		}
	}
	return nil
}

func (s *GroupService) validateBudgetTier(ctx context.Context, tierID *uuid.UUID) error {
	if tierID == nil {
		return nil
	}
	if s.modelTierRepo == nil {
		return ErrModelTierNotFound
	}
	tier, err := s.modelTierRepo.FindByID(ctx, *tierID)
	if err != nil || tier == nil || tier.ArchivedAt != nil {
		return ErrModelTierNotFound
	}
	if !tier.Enabled {
		return ErrModelTierDisabled
	}
	return nil
}

func NewGroupService(repo *repository.GroupRepo, modelRepo *repository.ModelRepo) *GroupService {
	return &GroupService{
		repo:      repo,
		modelRepo: modelRepo,
		failoverDefaults: &FailoverDefaults{
			MaxSwitches:      2,
			SameModelRetries: 1,
			CooldownSeconds:  60,
		},
	}
}

func (s *GroupService) SetFailoverDefaults(maxSwitches, sameModelRetries, cooldownSeconds int) {
	if maxSwitches > 0 {
		s.failoverDefaults.MaxSwitches = maxSwitches
	}
	if sameModelRetries >= 0 {
		s.failoverDefaults.SameModelRetries = sameModelRetries
	}
	if cooldownSeconds > 0 {
		s.failoverDefaults.CooldownSeconds = cooldownSeconds
	}
}

func (s *GroupService) GetFailoverDefaults() FailoverDefaults {
	return *s.failoverDefaults
}

type CreateGroupRequest struct {
	Name                    string `json:"name" binding:"required,max=64"`
	Description             string `json:"description"`
	ModelType               string `json:"model_type" binding:"omitempty,oneof=chat embedding embedding_vl rerank rerank_vl video"`
	Strategy                string `json:"strategy" binding:"required,oneof=round_robin priority weighted auto"`
	MaxSwitches             *int   `json:"max_switches"`
	SameModelRetries        *int   `json:"same_model_retries"`
	FailoverCooldownSeconds *int   `json:"failover_cooldown_seconds"`
	TimeoutSeconds          *int   `json:"timeout_seconds"`
	StickyEnabled           *bool  `json:"sticky_enabled"`
	StickyTTLSeconds        *int   `json:"sticky_ttl_seconds"`
}

type UpdateGroupRequest struct {
	Name                    *string `json:"name"`
	Description             *string `json:"description"`
	ModelType               *string `json:"model_type" binding:"omitempty,oneof=chat embedding embedding_vl rerank rerank_vl video"`
	Strategy                *string `json:"strategy" binding:"omitempty,oneof=round_robin priority weighted auto"`
	Enabled                 *bool   `json:"enabled"`
	MaxSwitches             *int    `json:"max_switches"`
	SameModelRetries        *int    `json:"same_model_retries"`
	FailoverCooldownSeconds *int    `json:"failover_cooldown_seconds"`
	TimeoutSeconds          *int    `json:"timeout_seconds"`

	IsCapabilityOverride   *bool `json:"is_capability_override"`
	OverrideSupportsVision *bool `json:"override_supports_vision"`
	OverrideSupportsFC     *bool `json:"override_supports_function_calling"`
	OverrideSupportsTC     *bool `json:"override_supports_tool_choice"`

	StickyEnabled    *bool `json:"sticky_enabled"`
	StickyTTLSeconds *int  `json:"sticky_ttl_seconds"`
}

func (s *GroupService) Create(req CreateGroupRequest) (*model.Group, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(req.Name) > 64 {
		return nil, fmt.Errorf("name exceeds 64 characters")
	}

	existing, err := s.repo.FindByName(req.Name)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("check name uniqueness: %w", err)
	}
	if existing != nil {
		return nil, ErrGroupNameExists
	}

	modelType := strings.TrimSpace(req.ModelType)
	if modelType == "" {
		modelType = "chat"
	}
	// Video 组固定单渠道路由，策略强制 round_robin（前端也会隐藏策略配置）
	strategy := req.Strategy
	if modelType == "video" {
		strategy = "round_robin"
	}

	group := &model.Group{
		Name:                    req.Name,
		Description:             req.Description,
		ModelType:               modelType,
		Strategy:                strategy,
		Enabled:                 true,
		MaxSwitches:             s.failoverDefaults.MaxSwitches,
		SameModelRetries:        s.failoverDefaults.SameModelRetries,
		FailoverCooldownSeconds: s.failoverDefaults.CooldownSeconds,
	}
	if req.MaxSwitches != nil {
		group.MaxSwitches = *req.MaxSwitches
	}
	if req.SameModelRetries != nil {
		group.SameModelRetries = *req.SameModelRetries
	}
	if req.FailoverCooldownSeconds != nil {
		group.FailoverCooldownSeconds = *req.FailoverCooldownSeconds
	}
	if req.TimeoutSeconds != nil {
		group.TimeoutSeconds = *req.TimeoutSeconds
	}
	if req.StickyEnabled != nil {
		group.StickyEnabled = req.StickyEnabled
	}
	if req.StickyTTLSeconds != nil {
		group.StickyTTLSeconds = req.StickyTTLSeconds
	}

	if err := s.repo.Create(group); err != nil {
		return nil, fmt.Errorf("create group: %w", err)
	}

	return group, nil
}

func (s *GroupService) Update(id uint, req UpdateGroupRequest) (*model.Group, error) {
	group, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("find group: %w", err)
	}

	if group.DeletedAt.Valid {
		return nil, ErrGroupDeleted
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("name is required")
		}
		if len(name) > 64 {
			return nil, fmt.Errorf("name exceeds 64 characters")
		}
		if name != group.Name {
			existing, err := s.repo.FindByName(name)
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("check name uniqueness: %w", err)
			}
			if existing != nil {
				return nil, ErrGroupNameExists
			}
			group.Name = name
		}
	}

	if req.Description != nil {
		group.Description = *req.Description
	}

	if req.ModelType != nil {
		mt := strings.TrimSpace(*req.ModelType)
		if mt == "" {
			mt = "chat"
		}
		group.ModelType = mt
		if mt == "video" {
			group.Strategy = "round_robin"
		}
	}

	if req.Strategy != nil && group.ModelType != "video" {
		group.Strategy = *req.Strategy
	}

	if req.Enabled != nil {
		group.Enabled = *req.Enabled
	}

	if req.MaxSwitches != nil {
		group.MaxSwitches = *req.MaxSwitches
	}
	if req.SameModelRetries != nil {
		group.SameModelRetries = *req.SameModelRetries
	}
	if req.FailoverCooldownSeconds != nil {
		group.FailoverCooldownSeconds = *req.FailoverCooldownSeconds
	}
	if req.TimeoutSeconds != nil {
		group.TimeoutSeconds = *req.TimeoutSeconds
	}

	if req.IsCapabilityOverride != nil {
		group.IsCapabilityOverride = *req.IsCapabilityOverride
	}
	if req.OverrideSupportsVision != nil {
		group.OverrideSupportsVision = *req.OverrideSupportsVision
	}
	if req.OverrideSupportsFC != nil {
		group.OverrideSupportsFC = *req.OverrideSupportsFC
	}
	if req.OverrideSupportsTC != nil {
		group.OverrideSupportsTC = *req.OverrideSupportsTC
	}

	if err := s.repo.Update(group); err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}

	return group, nil
}

func (s *GroupService) Delete(id uint) error {
	group, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupNotFound
		}
		return fmt.Errorf("find group: %w", err)
	}

	if group.DeletedAt.Valid {
		return ErrGroupNotFound
	}

	return s.repo.SoftDelete(id)
}

func (s *GroupService) GetByID(id uint) (*model.Group, error) {
	group, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("find group: %w", err)
	}
	return group, nil
}

func (s *GroupService) List(search string, page, pageSize int) ([]model.Group, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return s.repo.List(search, page, pageSize)
}

func (s *GroupService) AddModelsToGroup(groupID uint, modelIDs []uuid.UUID, priorities map[uuid.UUID]int, weights map[uuid.UUID]int, tiers map[uuid.UUID]string) error {
	if _, err := s.repo.FindByID(groupID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupNotFound
		}
		return fmt.Errorf("find group: %w", err)
	}

	if err := s.validateGroupModelTypes(groupID, modelIDs); err != nil {
		return err
	}

	for _, modelID := range modelIDs {
		gm := &model.GroupModel{
			ModelID: modelID,
		}

		if p, ok := priorities[modelID]; ok {
			v := p
			gm.Priority = &v
		}
		if w, ok := weights[modelID]; ok {
			v := w
			gm.Weight = &v
		}
		if t, ok := tiers[modelID]; ok {
			gm.ComplexityTier = t
		}

		if err := s.repo.AddModelToGroup(groupID, gm); err != nil {
			return fmt.Errorf("add model %s to group %d: %w", modelID, groupID, err)
		}
	}

	return nil
}

func (s *GroupService) validateGroupModelTypes(groupID uint, newModelIDs []uuid.UUID) error {
	if s.modelRepo == nil {
		return fmt.Errorf("model repo is required")
	}

	existingModels, err := s.repo.ListGroupModels(groupID)
	if err != nil {
		return fmt.Errorf("list group models: %w", err)
	}

	allIDs := make([]uuid.UUID, 0, len(existingModels)+len(newModelIDs))
	seen := make(map[uuid.UUID]struct{}, len(existingModels)+len(newModelIDs))
	for _, gm := range existingModels {
		if _, ok := seen[gm.ModelID]; ok {
			continue
		}
		seen[gm.ModelID] = struct{}{}
		allIDs = append(allIDs, gm.ModelID)
	}
	for _, modelID := range newModelIDs {
		if _, ok := seen[modelID]; ok {
			continue
		}
		seen[modelID] = struct{}{}
		allIDs = append(allIDs, modelID)
	}

	modelsByID, err := s.modelRepo.GetByIDs(allIDs)
	if err != nil {
		return fmt.Errorf("get group models: %w", err)
	}

	modelTypes := make([]string, 0, len(allIDs))
	for _, modelID := range allIDs {
		m, ok := modelsByID[modelID]
		if !ok {
			return fmt.Errorf("model %s not found", modelID)
		}
		modelTypes = append(modelTypes, m.ModelType)
	}

	return assertSameModelType(modelTypes)
}

func assertSameModelType(modelTypes []string) error {
	if len(modelTypes) == 0 {
		return nil
	}

	expected := strings.TrimSpace(modelTypes[0])
	if expected == "" {
		return ErrGroupModelTypeMismatch
	}
	for _, modelType := range modelTypes[1:] {
		if strings.TrimSpace(modelType) != expected {
			return ErrGroupModelTypeMismatch
		}
	}
	return nil
}

func (s *GroupService) RemoveModelFromGroup(groupID uint, modelID uuid.UUID) error {
	if _, err := s.repo.FindByID(groupID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrGroupNotFound
		}
		return fmt.Errorf("find group: %w", err)
	}

	_, err := s.repo.FindGroupModel(groupID, modelID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("model %s not found in group %d", modelID, groupID)
		}
		return fmt.Errorf("find group model: %w", err)
	}

	return s.repo.RemoveModelFromGroup(groupID, modelID)
}

func (s *GroupService) ListGroupModels(groupID uint) ([]model.GroupModel, error) {
	if _, err := s.repo.FindByID(groupID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("find group: %w", err)
	}

	return s.repo.ListGroupModels(groupID)
}

func (s *GroupService) ListEnabledGroupsWithModels() ([]model.Group, error) {
	return s.repo.ListEnabledGroupsWithModels()
}

func (s *GroupService) CountGroupModels(groupID uint) int {
	count, err := s.repo.CountGroupModels(groupID)
	if err != nil {
		return 0
	}
	return int(count)
}

func (s *GroupService) ResetCapability(id uint) (*model.Group, error) {
	group, err := s.repo.FindByID(id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("find group: %w", err)
	}
	if group.DeletedAt.Valid {
		return nil, ErrGroupDeleted
	}

	group.IsCapabilityOverride = false
	group.OverrideSupportsVision = false
	group.OverrideSupportsFC = false
	group.OverrideSupportsTC = false

	if err := s.repo.Update(group); err != nil {
		return nil, fmt.Errorf("update group: %w", err)
	}
	return group, nil
}
