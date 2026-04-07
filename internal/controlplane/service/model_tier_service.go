package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrModelTierNotFound   = errors.New("model tier not found")
	ErrModelTierNameExists = errors.New("model tier name already exists")
	ErrModelTierReferenced = errors.New("model tier is referenced")
	ErrModelTierDisabled   = errors.New("model tier is disabled")
	ErrInvalidTierOrder    = errors.New("invalid model tier order")
	ErrInvalidModelTier    = errors.New("invalid model tier")
	ErrReasonRequired      = errors.New("reason is required")
)

var modelTierColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type ModelTierRepository interface {
	Create(context.Context, *model.ModelTier) error
	FindByID(context.Context, uuid.UUID) (*model.ModelTier, error)
	List(context.Context, bool) ([]model.ModelTier, error)
	Update(context.Context, *model.ModelTier) error
	Reorder(context.Context, []uuid.UUID) error
	ReferenceCounts(context.Context, uuid.UUID) (repository.ModelTierReferenceCounts, error)
	Delete(context.Context, uuid.UUID) error
}

type ModelTierService struct {
	repo              ModelTierRepository
	snapshotPublisher BudgetConfigPublisher
}

func (s *ModelTierService) WithSnapshotPublisher(p BudgetConfigPublisher) *ModelTierService {
	s.snapshotPublisher = p
	return s
}

func (s *ModelTierService) publishSnapshot(ctx context.Context, action string, id uuid.UUID) error {
	if s.snapshotPublisher != nil {
		if err := s.snapshotPublisher.MarkDirtyAndPublish(ctx); err != nil {
			slog.ErrorContext(ctx, "budget snapshot publish failed after model tier write",
				"action", action, "model_tier_id", id, "error", err)
			return fmt.Errorf("%w: %v", ErrBudgetConfigSyncPending, err)
		}
	}
	return nil
}

type CreateModelTierInput struct {
	Name        string
	Description string
	Color       string
	SortOrder   int
	Reason      string
}

type UpdateModelTierInput struct {
	Name        *string
	Description *string
	Color       *string
	SortOrder   *int
	Reason      string
}

func NewModelTierService(repo ModelTierRepository) *ModelTierService {
	return &ModelTierService{repo: repo}
}

func (s *ModelTierService) List(ctx context.Context, includeArchived bool) ([]model.ModelTier, error) {
	tiers, err := s.repo.List(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	for index := range tiers {
		counts, countErr := s.repo.ReferenceCounts(ctx, tiers[index].ID)
		if countErr != nil {
			return nil, countErr
		}
		tiers[index].GroupCount = counts.Groups
		tiers[index].BudgetCount = counts.Limits + counts.Overrides + counts.Schedules
		tiers[index].HistoryCount = counts.Alerts + counts.Expansions + counts.CallLogs
		tiers[index].ReferenceCount = counts.Total()
	}
	return tiers, nil
}

func (s *ModelTierService) Get(ctx context.Context, id uuid.UUID) (*model.ModelTier, error) {
	tier, err := s.repo.FindByID(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && tier == nil) {
		return nil, ErrModelTierNotFound
	}
	return tier, err
}

func (s *ModelTierService) Create(ctx context.Context, input CreateModelTierInput) (*model.ModelTier, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrReasonRequired
	}
	name, color, err := validateTierValues(input.Name, input.Color)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUniqueName(ctx, name, uuid.Nil); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tier := &model.ModelTier{
		ID: uuid.New(), Name: name, Description: strings.TrimSpace(input.Description),
		Color: color, SortOrder: input.SortOrder, Enabled: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, tier); err != nil {
		if errors.Is(err, repository.ErrUniqueConflict) {
			return nil, ErrModelTierNameExists
		}
		return nil, err
	}
	return tier, s.publishSnapshot(ctx, "create", tier.ID)
}

func (s *ModelTierService) Update(ctx context.Context, id uuid.UUID, input UpdateModelTierInput) (*model.ModelTier, error) {
	if strings.TrimSpace(input.Reason) == "" {
		return nil, ErrReasonRequired
	}
	tier, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 64 {
			return nil, ErrInvalidModelTier
		}
		if err := s.ensureUniqueName(ctx, name, id); err != nil {
			return nil, err
		}
		tier.Name = name
	}
	if input.Description != nil {
		tier.Description = strings.TrimSpace(*input.Description)
	}
	if input.Color != nil {
		if !modelTierColorPattern.MatchString(*input.Color) {
			return nil, ErrInvalidModelTier
		}
		tier.Color = *input.Color
	}
	if input.SortOrder != nil {
		tier.SortOrder = *input.SortOrder
	}
	tier.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, tier); err != nil {
		if errors.Is(err, repository.ErrUniqueConflict) {
			return nil, ErrModelTierNameExists
		}
		return nil, err
	}
	return tier, s.publishSnapshot(ctx, "update", tier.ID)
}

func (s *ModelTierService) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool, reason string) (*model.ModelTier, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrReasonRequired
	}
	tier, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if tier.ArchivedAt != nil && enabled {
		return nil, ErrInvalidModelTier
	}
	tier.Enabled = enabled
	if err := s.repo.Update(ctx, tier); err != nil {
		return nil, err
	}
	return tier, s.publishSnapshot(ctx, "set_enabled", tier.ID)
}

func (s *ModelTierService) Archive(ctx context.Context, id uuid.UUID, reason string) (*model.ModelTier, error) {
	if strings.TrimSpace(reason) == "" {
		return nil, ErrReasonRequired
	}
	tier, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tier.Enabled = false
	tier.ArchivedAt = &now
	if err := s.repo.Update(ctx, tier); err != nil {
		return nil, err
	}
	return tier, s.publishSnapshot(ctx, "archive", tier.ID)
}

func (s *ModelTierService) Delete(ctx context.Context, id uuid.UUID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	counts, err := s.repo.ReferenceCounts(ctx, id)
	if err != nil {
		return err
	}
	if counts.Total() > 0 {
		return ErrModelTierReferenced
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	return s.publishSnapshot(ctx, "delete", id)
}

func (s *ModelTierService) Reorder(ctx context.Context, ids []uuid.UUID, reason string) error {
	if strings.TrimSpace(reason) == "" {
		return ErrReasonRequired
	}
	tiers, err := s.repo.List(ctx, false)
	if err != nil {
		return err
	}
	if len(ids) != len(tiers) {
		return ErrInvalidTierOrder
	}
	available := make(map[uuid.UUID]struct{}, len(tiers))
	for _, tier := range tiers {
		available[tier.ID] = struct{}{}
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if _, ok := available[id]; !ok {
			return ErrInvalidTierOrder
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrInvalidTierOrder
		}
		seen[id] = struct{}{}
	}
	if err := s.repo.Reorder(ctx, ids); err != nil {
		return err
	}
	return s.publishSnapshot(ctx, "reorder", uuid.Nil)
}

func (s *ModelTierService) ensureUniqueName(ctx context.Context, name string, except uuid.UUID) error {
	tiers, err := s.repo.List(ctx, true)
	if err != nil {
		return err
	}
	for _, tier := range tiers {
		if tier.ID != except && tier.ArchivedAt == nil && strings.EqualFold(tier.Name, name) {
			return ErrModelTierNameExists
		}
	}
	return nil
}

func validateTierValues(name, color string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return "", "", ErrInvalidModelTier
	}
	if color == "" {
		color = "#64748B"
	}
	if !modelTierColorPattern.MatchString(color) {
		return "", "", ErrInvalidModelTier
	}
	return name, color, nil
}
