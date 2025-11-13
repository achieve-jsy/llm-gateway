package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/example/llm-gateway/internal/controlplane/model"
	"github.com/example/llm-gateway/internal/controlplane/pkg/crypto"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// SystemUserID 系统内置用户 UUID（固定值，不对应真实用户）
	SystemUserID = "00000000-0000-0000-0000-000000000001"
	// SystemProbeKeyName 健康探针专用 key 名称（唯一标识）
	SystemProbeKeyName = "__system_health_probe__"
)

// SystemKeyService 管理系统内置 API Key
type SystemKeyService struct {
	db *gorm.DB
}

// NewSystemKeyService 创建 SystemKeyService 实例
func NewSystemKeyService(db *gorm.DB) *SystemKeyService {
	return &SystemKeyService{db: db}
}

// EnsureProbeKey 确保系统健康探针 key 存在并启用，返回明文 key。
// 若 key 已存在则解密后返回；若不存在则新建。
// 幂等：多次调用安全。
func (s *SystemKeyService) EnsureProbeKey(ctx context.Context) (string, error) {
	systemUID, _ := uuid.Parse(SystemUserID)

	// 查找已有的探针 key
	var existing model.ApiKey
	err := s.db.WithContext(ctx).
		Where("user_id = ? AND name = ?", systemUID, SystemProbeKeyName).
		First(&existing).Error

	if err == nil {
		// 已存在：解密返回
		if !existing.Enabled {
			// 若被禁用则重新启用
			s.db.WithContext(ctx).Model(&existing).Update("enabled", true)
		}
		raw, err := crypto.Decrypt(existing.EncryptedKey)
		if err != nil {
			return "", fmt.Errorf("decrypt probe key: %w", err)
		}
		return raw, nil
	}

	if err != gorm.ErrRecordNotFound {
		return "", fmt.Errorf("query probe key: %w", err)
	}

	// 不存在：新建
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return "", fmt.Errorf("generate probe key: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(rawBytes)

	hash := sha256.Sum256([]byte(keyStr))
	keyHash := hex.EncodeToString(hash[:])

	encryptedKey, err := crypto.Encrypt(keyStr)
	if err != nil {
		return "", fmt.Errorf("encrypt probe key: %w", err)
	}

	k := &model.ApiKey{
		UserID:          systemUID,
		Name:            SystemProbeKeyName,
		KeyHash:         keyHash,
		EncryptedKey:    encryptedKey,
		KeyPrefix:       "sk-",
		KeyHint:         keyStr[len(keyStr)-4:],
		Enabled:         true,
		AccessAllGroups: true,
	}

	if err := s.db.WithContext(ctx).Create(k).Error; err != nil {
		return "", fmt.Errorf("create probe key: %w", err)
	}

	slog.Info("system health probe key created", "key_id", k.ID)
	return keyStr, nil
}
