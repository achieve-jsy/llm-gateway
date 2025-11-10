package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/example/llm-gateway/internal/controlplane/event"
	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/example/llm-gateway/internal/controlplane/repository"
	"github.com/google/uuid"
)

var (
	ErrKeyNotFound = errors.New("key not found")
)

type KeyItem struct {
	ID        uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	MaskedKey string     `json:"masked_key"`
	KeyHint   string     `json:"key_hint"`
	Enabled   bool       `json:"enabled"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

type KeyDetail struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	MaskedKey string    `json:"masked_key"`
	KeyHint   string    `json:"key_hint"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type VerifyResultUser struct {
	UserID       uuid.UUID  `json:"user_id"`
	Name         string     `json:"name"`
	DepartmentID *uuid.UUID `json:"department_id"`
}

type VerifyResult struct {
	KeyID  uuid.UUID        `json:"key_id"`
	Status string           `json:"status"`
	User   VerifyResultUser `json:"user"`
}

type EventPublisher interface {
	Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID)
}

type publisherAdapter struct {
	p *event.Publisher
}

func (a *publisherAdapter) Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID) {
	if a.p == nil {
		return
	}
	ev := event.NewChangeEvent(eventType, resourceType, resourceID, 0)
	if err := a.p.Publish(context.Background(), ev); err != nil {
		slog.Error("failed to publish event", "type", eventType, "resource_id", resourceID, "error", err)
	}
}

func NewEventPublisherAdapter(p *event.Publisher) EventPublisher {
	if p == nil {
		return nil
	}
	return &publisherAdapter{p: p}
}

type KeyService struct {
	repo      *repository.KeyRepo
	publisher EventPublisher
}

func NewKeyService(repo *repository.KeyRepo, publisher EventPublisher) *KeyService {
	return &KeyService{repo: repo, publisher: publisher}
}

func (s *KeyService) CreateKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time) (*model.ApiKey, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate random key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)
	keyHint := keyStr[len(keyStr)-4:]

	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])

	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt key: %w", err)
	}

	k := &model.ApiKey{
		UserID:       userID,
		Name:         name,
		KeyHash:      keyHash,
		EncryptedKey: encryptedKey,
		KeyPrefix:    "sk-",
		KeyHint:      keyHint,
		Enabled:      true,
		ExpiresAt:    expiresAt,
	}

	if err := s.repo.Create(k); err != nil {
		return nil, "", fmt.Errorf("create key: %w", err)
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyCreated, "key", k.ID)
	}

	return k, keyStr, nil
}

func (s *KeyService) ListKeys(ctx context.Context, userID uuid.UUID, page, size int) ([]KeyItem, int64, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	offset := (page - 1) * size

	keys, total, err := s.repo.ListByUserID(userID, offset, size)
	if err != nil {
		return nil, 0, fmt.Errorf("list keys: %w", err)
	}

	items := make([]KeyItem, 0, len(keys))
	for _, k := range keys {
		items = append(items, KeyItem{
			ID:       k.ID,
			Name:     k.Name,
			MaskedKey: k.KeyPrefix + "****" + k.KeyHint,
			KeyHint:  k.KeyHint,
			Enabled:  k.Enabled,
			CreatedAt: k.CreatedAt,
			UpdatedAt: k.UpdatedAt,
			LastUsedAt: k.LastUsedAt,
			ExpiresAt: k.ExpiresAt,
		})
	}

	return items, total, nil
}

func (s *KeyService) GetKey(ctx context.Context, userID, keyID uuid.UUID) (*KeyDetail, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}

	return &KeyDetail{
		ID:        k.ID,
		Name:      k.Name,
		MaskedKey: k.KeyPrefix + "****" + k.KeyHint,
		KeyHint:   k.KeyHint,
		Enabled:   k.Enabled,
		CreatedAt: k.CreatedAt,
		UpdatedAt: k.UpdatedAt,
	}, nil
}

func (s *KeyService) UpdateKeyStatus(ctx context.Context, userID, keyID uuid.UUID, enabled bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}

	if err := s.repo.UpdateStatus(keyID, enabled); err != nil {
		return nil, fmt.Errorf("update key status: %w", err)
	}
	k.Enabled = enabled

	if s.publisher != nil {
		if enabled {
			s.publisher.Publish(event.KeyEnabled, "key", keyID)
		} else {
			s.publisher.Publish(event.KeyDisabled, "key", keyID)
		}
	}

	return k, nil
}

func (s *KeyService) DeleteKey(ctx context.Context, userID, keyID uuid.UUID) error {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return ErrKeyNotFound
	}
	if k.UserID != userID {
		return ErrKeyNotFound
	}

	if err := s.repo.Delete(keyID); err != nil {
		return fmt.Errorf("delete key: %w", err)
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyDeleted, "key", keyID)
	}

	return nil
}

func (s *KeyService) RevealKey(ctx context.Context, userID, keyID uuid.UUID) (string, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return "", ErrKeyNotFound
	}
	if k.UserID != userID {
		return "", ErrKeyNotFound
	}

	plaintext, err := crypto.Decrypt(k.EncryptedKey)
	if err != nil {
		return "", fmt.Errorf("decrypt key: %w", err)
	}
	return plaintext, nil
}

func (s *KeyService) ResetKey(ctx context.Context, userID, keyID uuid.UUID) (*model.ApiKey, string, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, "", ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, "", ErrKeyNotFound
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate random key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)
	keyHint := keyStr[len(keyStr)-4:]
	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])
	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return nil, "", fmt.Errorf("encrypt key: %w", err)
	}

	k.KeyHash = keyHash
	k.EncryptedKey = encryptedKey
	k.KeyHint = keyHint
	if err := s.repo.UpdateHashAndKey(k.ID, keyHash, encryptedKey, keyHint); err != nil {
		return nil, "", fmt.Errorf("update key: %w", err)
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyCreated, "key", keyID)
	}

	return k, keyStr, nil
}

func (s *KeyService) VerifyKey(ctx context.Context, keyRaw string) (*VerifyResult, error) {
	hash := sha256.Sum256([]byte(keyRaw))
	keyHash := hex.EncodeToString(hash[:])

	k, err := s.repo.FindByHash(keyHash)
	if err != nil {
		return nil, ErrKeyNotFound
	}

	status := "active"
	if !k.Enabled {
		status = "disabled"
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		status = "disabled"
	}

	s.repo.TouchLastUsed(keyHash) // best-effort update

	return &VerifyResult{
		KeyID:  k.ID,
		Status: status,
		User: VerifyResultUser{
			UserID: k.UserID,
		},
	}, nil
}
