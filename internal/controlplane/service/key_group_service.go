package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrGroupNotBound = errors.New("group not bound to this key")
)

type BoundGroupItem struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

type AvailableGroupItem struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Strategy    string `json:"strategy"`
}

type KeyGroupService struct {
	repo      *repository.KeyGroupRepo
	groupRepo *repository.GroupRepo
	keyRepo   *repository.KeyRepo
	publisher EventPublisher
}

func NewKeyGroupService(repo *repository.KeyGroupRepo, groupRepo *repository.GroupRepo, keyRepo *repository.KeyRepo, publisher EventPublisher) *KeyGroupService {
	return &KeyGroupService{
		repo:      repo,
		groupRepo: groupRepo,
		keyRepo:   keyRepo,
		publisher: publisher,
	}
}

func (s *KeyGroupService) BindGroups(ctx context.Context, tx *gorm.DB, keyID uuid.UUID, groupIDs []uint) error {
	if len(groupIDs) == 0 {
		return nil
	}
	validIDs, err := s.validateGroupIDs(ctx, groupIDs)
	if err != nil {
		return err
	}
	return s.repo.BatchInsert(ctx, tx, keyID, validIDs)
}

func (s *KeyGroupService) GetKeyGroups(ctx context.Context, keyID uuid.UUID) ([]BoundGroupItem, error) {
	results, err := s.repo.FindByKeyID(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("get key groups: %w", err)
	}
	items := make([]BoundGroupItem, 0, len(results))
	for _, r := range results {
		items = append(items, BoundGroupItem{
			ID:      r.GroupID,
			Name:    r.GroupName,
			Enabled: r.Enabled,
		})
	}
	return items, nil
}

func (s *KeyGroupService) UpdateBindings(ctx context.Context, keyID uuid.UUID, add, remove []uint) ([]BoundGroupItem, error) {
	if len(add) == 0 && len(remove) == 0 {
		return nil, fmt.Errorf("add and remove cannot both be empty")
	}

	key, err := s.keyRepo.FindByID(keyID)
	if err != nil {
		return nil, fmt.Errorf("key not found: %w", err)
	}
	_ = key

	if len(add) > 0 {
		deduped := dedupGroupIDs(add, remove)
		if len(deduped) > 0 {
			if _, err := s.validateGroupIDs(ctx, deduped); err != nil {
				return nil, err
			}
		}
		// re-assign deduped to add (remove the overlapping)
		add = deduped
	}

	if len(remove) > 0 {
		for _, gid := range remove {
			exists, err := s.repo.ExistsByKeyAndGroup(ctx, keyID, gid)
			if err != nil {
				return nil, fmt.Errorf("check binding: %w", err)
			}
			if !exists {
				return nil, ErrGroupNotBound
			}
		}
	}

	err = s.repo.Transaction(func(db *gorm.DB) error {
		if len(remove) > 0 {
			if err := s.repo.BatchDelete(ctx, db, keyID, remove); err != nil {
				return err
			}
		}
		if len(add) > 0 {
			if err := s.repo.BatchInsert(ctx, db, keyID, add); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("update bindings: %w", err)
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyCreated, "key_binding", keyID)
	}

	return s.GetKeyGroups(ctx, keyID)
}

func (s *KeyGroupService) ListAvailableGroups(ctx context.Context) ([]AvailableGroupItem, error) {
	groups, err := s.groupRepo.ListEnabled()
	if err != nil {
		return nil, fmt.Errorf("list available groups: %w", err)
	}
	items := make([]AvailableGroupItem, 0, len(groups))
	for _, g := range groups {
		items = append(items, AvailableGroupItem{
			ID:          g.ID,
			Name:        g.Name,
			Description: g.Description,
			Strategy:    g.Strategy,
		})
	}
	return items, nil
}

func (s *KeyGroupService) validateGroupIDs(ctx context.Context, groupIDs []uint) ([]uint, error) {
	valid := make([]uint, 0, len(groupIDs))
	for _, gid := range groupIDs {
		g, err := s.groupRepo.FindByID(gid)
		if err != nil {
			slog.Warn("key group bind: group not found, skipping", "group_id", gid)
			continue
		}
		if !g.Enabled {
			slog.Warn("key group bind: group disabled, skipping", "group_id", gid)
			continue
		}
		valid = append(valid, gid)
	}
	return valid, nil
}

func dedupGroupIDs(add, remove []uint) []uint {
	removeSet := make(map[uint]bool, len(remove))
	for _, gid := range remove {
		removeSet[gid] = true
	}
	result := make([]uint, 0, len(add))
	for _, gid := range add {
		if !removeSet[gid] {
			result = append(result, gid)
		}
	}
	return result
}