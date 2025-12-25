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
	ID              uuid.UUID        `json:"id"`
	Name            string           `json:"name"`
	MaskedKey       string           `json:"masked_key"`
	KeyHint         string           `json:"key_hint"`
	Enabled         bool             `json:"enabled"`
	LogEnabled      bool             `json:"log_enabled"`
	AccessAllGroups bool             `json:"access_all_groups"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	LastUsedAt      *time.Time       `json:"last_used_at,omitempty"`
	ExpiresAt       *time.Time       `json:"expires_at,omitempty"`
	BoundGroups     []BoundGroupItem `json:"bound_groups,omitempty"`
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
	UserID       uuid.UUID `json:"user_id"`
	Name         string    `json:"name"`
	DepartmentID string    `json:"department_id,omitempty"` // SSO dept_id 原始字符串，不做 UUID 解析（避免大小写被标准化）
	DeptName     string    `json:"dept_name,omitempty"`
	StudioID     string    `json:"studio_id,omitempty"`
	StudioName   string    `json:"studio_name,omitempty"`
}

type VerifyResult struct {
	KeyID           uuid.UUID        `json:"key_id"`
	KeyName         string           `json:"key_name,omitempty"`
	Status          string           `json:"status"`
	AccessAllGroups bool             `json:"access_all_groups"`
	LogEnabled      bool             `json:"log_enabled"`
	User            VerifyResultUser `json:"user"`
}

type EventPublisher interface {
	Publish(eventType event.ChangeEventType, resourceType string, resourceID uuid.UUID)
	InvalidateKeyCache(keyHash string)
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

func (a *publisherAdapter) InvalidateKeyCache(keyHash string) {
	if a.p == nil {
		return
	}
	a.p.InvalidateKeyAuthCache(context.Background(), keyHash)
}

func NewEventPublisherAdapter(p *event.Publisher) EventPublisher {
	if p == nil {
		return nil
	}
	return &publisherAdapter{p: p}
}

type KeyService struct {
	repo        *repository.KeyRepo
	keyGroupSvc *KeyGroupService
	publisher   EventPublisher
}

func NewKeyService(repo *repository.KeyRepo, keyGroupSvc *KeyGroupService, publisher EventPublisher) *KeyService {
	return &KeyService{repo: repo, keyGroupSvc: keyGroupSvc, publisher: publisher}
}

func (s *KeyService) CreateKey(ctx context.Context, userID uuid.UUID, name string, expiresAt *time.Time, groupIDs []uint) (*model.ApiKey, string, []BoundGroupItem, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", nil, fmt.Errorf("generate random key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)
	keyHint := keyStr[len(keyStr)-4:]

	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])

	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return nil, "", nil, fmt.Errorf("encrypt key: %w", err)
	}

	// 冗余写入用户/部门名称，供 verify-key 直接返回，无需 JOIN
	info, infoErr := s.repo.GetUserDeptInfo(userID)
	if infoErr != nil {
		slog.Warn("create key: failed to fetch user/dept info", "user_id", userID, "error", infoErr)
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
		UserName:     info.UserName,
		DeptID:       info.DeptID,
		DeptName:     info.DeptName,
		StudioID:     info.StudioID,
		StudioName:   info.StudioName,
	}

	if err := s.repo.Create(k); err != nil {
		return nil, "", nil, fmt.Errorf("create key: %w", err)
	}

	var boundGroups []BoundGroupItem
	if len(groupIDs) > 0 && s.keyGroupSvc != nil {
		if err := s.keyGroupSvc.BindGroups(ctx, nil, k.ID, groupIDs); err != nil {
			slog.Warn("create key: bind groups failed", "key_id", k.ID, "error", err)
		}
	}
	if s.keyGroupSvc != nil {
		groups, err := s.keyGroupSvc.GetKeyGroups(ctx, k.ID)
		if err != nil {
			slog.Warn("create key: get key groups failed", "key_id", k.ID, "error", err)
		} else {
			boundGroups = groups
		}
	}

	if s.publisher != nil {
		s.publisher.Publish(event.KeyCreated, "api_key", k.ID)
	}

	return k, keyStr, boundGroups, nil
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
		var boundGroups []BoundGroupItem
		if s.keyGroupSvc != nil {
			groups, err := s.keyGroupSvc.GetKeyGroups(ctx, k.ID)
			if err != nil {
				slog.Warn("list keys: get key groups failed", "key_id", k.ID, "error", err)
			} else {
				boundGroups = groups
			}
		}
		items = append(items, KeyItem{
			ID:              k.ID,
			Name:            k.Name,
			MaskedKey:       k.KeyPrefix + "****" + k.KeyHint,
			KeyHint:         k.KeyHint,
			Enabled:         k.Enabled,
			LogEnabled:      k.LogEnabled,
			AccessAllGroups: k.AccessAllGroups,
			CreatedAt:       k.CreatedAt,
			UpdatedAt:       k.UpdatedAt,
			LastUsedAt:      k.LastUsedAt,
			ExpiresAt:       k.ExpiresAt,
			BoundGroups:     boundGroups,
		})
	}

	return items, total, nil
}

func (s *KeyService) GetKey(ctx context.Context, userID, keyID uuid.UUID) (*KeyDetail, []BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, nil, ErrKeyNotFound
	}

	var boundGroups []BoundGroupItem
	if s.keyGroupSvc != nil {
		groups, err := s.keyGroupSvc.GetKeyGroups(ctx, keyID)
		if err != nil {
			slog.Warn("get key: get key groups failed", "key_id", keyID, "error", err)
		} else {
			boundGroups = groups
		}
	}

	return &KeyDetail{
		ID:        k.ID,
		Name:      k.Name,
		MaskedKey: k.KeyPrefix + "****" + k.KeyHint,
		KeyHint:   k.KeyHint,
		Enabled:   k.Enabled,
		CreatedAt: k.CreatedAt,
		UpdatedAt: k.UpdatedAt,
	}, boundGroups, nil
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
		// FIX(v0.7): resource_type 统一为 "api_key"，与 KeyCache.HandleChangeEvent 期望一致；
		// 同时补调 InvalidateKeyCache（Redis Del + pub/sub），保证「即时生效」承诺不落空。
		s.publisher.InvalidateKeyCache(k.KeyHash)
		if enabled {
			s.publisher.Publish(event.KeyEnabled, "api_key", keyID)
		} else {
			s.publisher.Publish(event.KeyDisabled, "api_key", keyID)
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
		// FIX(v0.7): 统一 resource_type="api_key" + 主动失效缓存
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyDeleted, "api_key", keyID)
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
		// FIX(v0.7): 统一 resource_type="api_key" + 失效旧 hash 缓存（重置后旧 hash 作废）
		s.publisher.InvalidateKeyCache(k.KeyHash)
		s.publisher.Publish(event.KeyCreated, "api_key", keyID)
	}

	return k, keyStr, nil
}

func (s *KeyService) ListKeyGroups(ctx context.Context, userID, keyID uuid.UUID) ([]BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}
	if s.keyGroupSvc == nil {
		return nil, nil
	}
	return s.keyGroupSvc.GetKeyGroups(ctx, keyID)
}

func (s *KeyService) UpdateKeyGroups(ctx context.Context, userID, keyID uuid.UUID, add, remove []uint) ([]BoundGroupItem, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if k.UserID != userID {
		return nil, ErrKeyNotFound
	}
	if s.keyGroupSvc == nil {
		return nil, fmt.Errorf("key group service not available")
	}
	return s.keyGroupSvc.UpdateBindings(ctx, keyID, add, remove)
}

func (s *KeyService) ListAvailableGroups(ctx context.Context) ([]AvailableGroupItem, error) {
	if s.keyGroupSvc == nil {
		return nil, nil
	}
	return s.keyGroupSvc.ListAvailableGroups(ctx)
}

func (s *KeyService) UpdateAccessAllGroups(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if err := s.repo.UpdateAccessAllGroups(keyID, value); err != nil {
		return nil, fmt.Errorf("update access_all_groups: %w", err)
	}
	k.AccessAllGroups = value
	return k, nil
}

func (s *KeyService) UpdateLogEnabled(ctx context.Context, userID, keyID uuid.UUID, value bool) (*model.ApiKey, error) {
	k, err := s.repo.FindByID(keyID)
	if err != nil {
		return nil, ErrKeyNotFound
	}
	if err := s.repo.UpdateLogEnabled(keyID, value); err != nil {
		return nil, fmt.Errorf("update log_enabled: %w", err)
	}
	k.LogEnabled = value
	if s.publisher != nil {
		// 立即删除两层 Redis 缓存（auth service + gateway KeyCache），无需等 TTL
		s.publisher.InvalidateKeyCache(k.KeyHash)
		// 通知 gateway 订阅者清除本地内存缓存
		s.publisher.Publish(event.KeyLogChanged, "api_key", keyID)
	}
	return k, nil
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

	// 懒补全：如果 key 的 org 快照字段缺失，同步补齐后再返回，
	// 确保本次请求就能拿到正确的 studio_id，不再需要等第二次请求。
	// 跳过条件：UserName 已有值且 DeptID 为空，说明该用户没有部门，查了也没用。
	needBackfill := k.UserName == "" || k.DeptName == "" || k.DeptID == "" || k.StudioName == "" || k.StudioID == ""
	hasNoDept := k.UserName != "" && k.DeptID == "" // 用户存在但确实没有部门，不查
	if needBackfill && !hasNoDept {
		info, err := s.repo.GetUserDeptInfo(k.UserID)
		if err == nil && info.UserName != "" {
			k.UserName = info.UserName
			k.DeptID = info.DeptID
			k.DeptName = info.DeptName
			k.StudioID = info.StudioID
			k.StudioName = info.StudioName
			// 异步写回 DB + 发失效事件（不阻塞本次响应）
			go func() {
				if err := s.repo.BackfillUserDeptNames(k.ID, info.UserName, info.DeptID, info.DeptName, info.StudioID, info.StudioName); err == nil {
					if s.publisher != nil {
						s.publisher.InvalidateKeyCache(k.KeyHash)
						s.publisher.Publish(event.KeyUpdated, "api_key", k.ID)
					}
				}
			}()
		}
	}

	return &VerifyResult{
		KeyID:           k.ID,
		KeyName:         k.Name,
		Status:          status,
		AccessAllGroups: k.AccessAllGroups,
		LogEnabled:      k.LogEnabled,
		User: VerifyResultUser{
			UserID:       k.UserID,
			Name:         k.UserName,
			DepartmentID: k.DeptID, // 直接传 SSO dept_id 原始字符串，保留大小写
			DeptName:     k.DeptName,
			StudioID:     k.StudioID,
			StudioName:   k.StudioName,
		},
	}, nil
}

// parseDeptID 将字符串形式的 dept_id 转换为 *uuid.UUID，空字符串返回 nil。
func parseDeptID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}
