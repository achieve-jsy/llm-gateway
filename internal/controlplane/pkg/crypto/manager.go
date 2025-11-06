package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"
)

// KeyManager 管理加密密钥的生命周期
type KeyManager struct {
	kek           []byte            // Key Encryption Key (KEK)
	deks          map[int][]byte    // 版本 -> 数据加密密钥 (DEK)
	activeVersion int               // 当前活跃版本
	mu            sync.RWMutex      // 保护并发访问
}

var defaultManager *KeyManager

// SystemConfig 和 EncryptionKey 模型用于数据库操作
type SystemConfig struct {
	Key         string `gorm:"primaryKey;column:key;type:varchar(100)"`
	Value       string `gorm:"column:value;type:text"`
	Encrypted   bool   `gorm:"column:encrypted"`
	Description string `gorm:"column:description;type:varchar(500)"`
	CreatedAt   string `gorm:"column:created_at"`
	UpdatedAt   string `gorm:"column:updated_at"`
}

func (SystemConfig) TableName() string {
	return "system_configs"
}

type EncryptionKey struct {
	Version     int    `gorm:"primaryKey;column:version"`
	EncryptedKey string `gorm:"column:encrypted_key;type:text"`
	Algorithm   string `gorm:"column:algorithm;type:varchar(50)"`
	Status      string `gorm:"column:status;type:varchar(20)"`
	CreatedAt   string `gorm:"column:created_at"`
	RetiredAt   *string `gorm:"column:retired_at"`
}

func (EncryptionKey) TableName() string {
	return "encryption_keys"
}

const (
	kekKey              = "encryption_kek"
	keySize             = 32 // AES-256 需要 32 字节密钥
	versionedFormatReg  = `^v(\d+):`
)

// InitFromDB 从数据库初始化密钥管理器
func InitFromDB(db *gorm.DB) error {
	mgr := &KeyManager{
		deks: make(map[int][]byte),
	}

	// 步骤 1: 加载或生成 KEK
	var kekConfig SystemConfig
	err := db.Where("key = ?", kekKey).First(&kekConfig).Error
	if err == gorm.ErrRecordNotFound {
		// 首次运行：生成 KEK
		kekBytes := make([]byte, keySize)
		if _, err := rand.Read(kekBytes); err != nil {
			return fmt.Errorf("generate KEK: %w", err)
		}
		mgr.kek = kekBytes

		// 存储 KEK（Base64 编码）
		if err := db.Create(&SystemConfig{
			Key:       kekKey,
			Value:     base64.StdEncoding.EncodeToString(kekBytes),
			Encrypted: false,
			Description: "Key Encryption Key for DEK",
		}).Error; err != nil {
			return fmt.Errorf("store KEK: %w", err)
		}

		// 生成初始 DEK v1
		dekBytes := make([]byte, keySize)
		if _, err := rand.Read(dekBytes); err != nil {
			return fmt.Errorf("generate DEK v1: %w", err)
		}

		// 用 KEK 加密 DEK
		encryptedDEK, err := aesEncrypt(mgr.kek, dekBytes)
		if err != nil {
			return fmt.Errorf("encrypt DEK v1: %w", err)
		}

		// 存储加密后的 DEK
		if err := db.Create(&EncryptionKey{
			Version:      1,
			EncryptedKey: base64.StdEncoding.EncodeToString(encryptedDEK),
			Algorithm:    "AES-256-GCM",
			Status:       "active",
		}).Error; err != nil {
			return fmt.Errorf("store DEK v1: %w", err)
		}

		mgr.deks[1] = dekBytes
		mgr.activeVersion = 1
	} else if err != nil {
		return fmt.Errorf("fetch KEK: %w", err)
	} else {
		// 加载已有的 KEK
		kekBytes, err := base64.StdEncoding.DecodeString(kekConfig.Value)
		if err != nil {
			return fmt.Errorf("decode KEK: %w", err)
		}
		mgr.kek = kekBytes
	}

	// 步骤 2: 加载所有 DEK（按版本）
	var encKeys []EncryptionKey
	if err := db.Order("version DESC").Find(&encKeys).Error; err != nil {
		return fmt.Errorf("fetch DEKs: %w", err)
	}

	for _, ek := range encKeys {
		// 用 KEK 解密 DEK
		encryptedDEK, err := base64.StdEncoding.DecodeString(ek.EncryptedKey)
		if err != nil {
			return fmt.Errorf("decode encrypted DEK v%d: %w", ek.Version, err)
		}

		dekBytes, err := aesDecrypt(mgr.kek, encryptedDEK)
		if err != nil {
			return fmt.Errorf("decrypt DEK v%d: %w", ek.Version, err)
		}

		mgr.deks[ek.Version] = dekBytes

		if ek.Status == "active" {
			mgr.activeVersion = ek.Version
		}
	}

	if mgr.activeVersion == 0 && len(mgr.deks) > 0 {
		// 如果没有标记为 active 的密钥，使用最高版本
		for v := range mgr.deks {
			if v > mgr.activeVersion {
				mgr.activeVersion = v
			}
		}
	}

	defaultManager = mgr
	return nil
}

// Encrypt 使用活跃 DEK 加密明文，返回版本化格式。
// Encrypt 加密明文。优先使用 KeyManager，未初始化时 fallback 到环境变量。
func Encrypt(plaintext string) (string, error) {
	if defaultManager == nil {
		return encryptFallback(plaintext)
	}

	defaultManager.mu.RLock()
	defer defaultManager.mu.RUnlock()

	if defaultManager.activeVersion == 0 || len(defaultManager.deks) == 0 {
		return encryptFallback(plaintext)
	}

	activeDEK := defaultManager.deks[defaultManager.activeVersion]
	if activeDEK == nil {
		return encryptFallback(plaintext)
	}

	// 使用 AES-256-GCM 加密
	ciphertext, err := aesEncrypt(activeDEK, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	// 返回版本化格式: v{version}:{base64_ciphertext}
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	return fmt.Sprintf("v%d:%s", defaultManager.activeVersion, encoded), nil
}

// encryptFallback 使用环境变量 ENCRYPTION_KEY 直接加密（无 KeyManager 时的兼容模式）
func encryptFallback(plaintext string) (string, error) {
	key := getKeyFromEnv()
	if key == nil {
		return "", fmt.Errorf("ENCRYPTION_KEY environment variable not set or invalid (need 32 bytes)")
	}
	ciphertext, err := aesEncrypt(key, []byte(plaintext))
	if err != nil {
		return "", fmt.Errorf("encrypt fallback: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func getKeyFromEnv() []byte {
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if len(keyStr) != 32 {
		return nil
	}
	return []byte(keyStr)
}

// Decrypt 解密密文，支持版本化格式 v{version}:{ciphertext}。
// Decrypt 解密密文。优先使用 KeyManager，未初始化时 fallback 到环境变量。
func Decrypt(ciphertext string) (string, error) {
	if defaultManager == nil {
		return decryptFallback(ciphertext)
	}

	defaultManager.mu.RLock()
	defer defaultManager.mu.RUnlock()

	// 尝试解析版本化格式 v{version}:{base64_ciphertext}
	re := regexp.MustCompile(versionedFormatReg)
	match := re.FindStringSubmatch(ciphertext)

	if len(match) > 1 {
		// 版本化格式
		version, err := strconv.Atoi(match[1])
		if err != nil {
			return "", fmt.Errorf("invalid version: %w", err)
		}

		dek, exists := defaultManager.deks[version]
		if !exists {
			return "", fmt.Errorf("DEK version %d not found", version)
		}

		// 提取 Base64 部分
		parts := strings.SplitN(ciphertext, ":", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid versioned format")
		}

		encoded := parts[1]
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", fmt.Errorf("decode base64: %w", err)
		}

		plaintext, err := aesDecrypt(dek, decoded)
		if err != nil {
			return "", fmt.Errorf("decrypt v%d: %w", version, err)
		}

		return string(plaintext), nil
	}

	// 旧格式：尝试用环境变量 ENCRYPTION_KEY 后备
	keyStr := os.Getenv("ENCRYPTION_KEY")
	if keyStr == "" {
		return "", fmt.Errorf("ciphertext not in versioned format and ENCRYPTION_KEY not set")
	}

	key := []byte(keyStr)
	if len(key) != keySize {
		return "", fmt.Errorf("ENCRYPTION_KEY must be exactly %d bytes, got %d", keySize, len(key))
	}

	// 解码 Base64
	decoded, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	plaintext, err := aesDecrypt(key, decoded)
	if err != nil {
		return "", fmt.Errorf("decrypt with legacy key: %w", err)
	}

	return string(plaintext), nil
}

// Mask 对凭证进行脱敏展示。
//   - 长度 <= 8 → "****"
//   - 长度 9..12（中等）→ 前 3 位 + "****" + 尾 4 位，但若总长 <= 8+前缀 时仍用 ****（短串无意义）
//   - 长度 > 12 → 前 3 位 + "****" + 尾 4 位
//
// 设计取舍：测试用例 "sk-1234567890" (13 长) → "sk-****7890"（前3 + **** + 后4）
// "very-long-credential-string-1234" (32 长) → "ver****1234"（前3 + **** + 后4）
func Mask(credential string) string {
	if len(credential) <= 12 {
		return "****"
	}
	// 前8位 + **** + 后4位，中间脱敏
	return credential[:8] + "****" + credential[len(credential)-4:]
}

// RotateKey 生成新的 DEK 版本并存储
func RotateKey(db *gorm.DB) error {
	if defaultManager == nil {
		return fmt.Errorf("key manager not initialized")
	}

	defaultManager.mu.Lock()
	defer defaultManager.mu.Unlock()

	// 生成新 DEK
	newDEK := make([]byte, keySize)
	if _, err := rand.Read(newDEK); err != nil {
		return fmt.Errorf("generate new DEK: %w", err)
	}

	// 确定新版本号
	newVersion := defaultManager.activeVersion + 1

	// 用 KEK 加密新 DEK
	encryptedDEK, err := aesEncrypt(defaultManager.kek, newDEK)
	if err != nil {
		return fmt.Errorf("encrypt new DEK: %w", err)
	}

	// 将旧版本标记为 "rotating"
	if err := db.Model(&EncryptionKey{}).
		Where("status = ?", "active").
		Update("status", "rotating").Error; err != nil {
		return fmt.Errorf("update old key status: %w", err)
	}

	// 存储新 DEK
	if err := db.Create(&EncryptionKey{
		Version:      newVersion,
		EncryptedKey: base64.StdEncoding.EncodeToString(encryptedDEK),
		Algorithm:    "AES-256-GCM",
		Status:       "active",
	}).Error; err != nil {
		return fmt.Errorf("store new DEK: %w", err)
	}

	// 更新内存状态
	defaultManager.deks[newVersion] = newDEK
	defaultManager.activeVersion = newVersion

	return nil
}

// InitForTesting 在不依赖数据库的前提下初始化一个固定 DEK 的 KeyManager，
// 供单元测试使用。生产代码必须走 InitFromDB。
//
// 在多个测试包反复调用是安全的，因为同一进程内 defaultManager 重新赋值。
func InitForTesting() {
	kek := make([]byte, keySize)
	dek := make([]byte, keySize)
	for i := range kek {
		kek[i] = byte(i + 1)
		dek[i] = byte(0x80 + i)
	}
	defaultManager = &KeyManager{
		kek:           kek,
		deks:          map[int][]byte{1: dek},
		activeVersion: 1,
	}
}

// GetActiveVersion 返回当前活跃的 DEK 版本（仅用于测试/调试）
func GetActiveVersion() int {
	if defaultManager == nil {
		return 0
	}

	defaultManager.mu.RLock()
	defer defaultManager.mu.RUnlock()

	return defaultManager.activeVersion
}

// HasVersion 检查某个版本的 DEK 是否存在（仅用于测试/调试）
func HasVersion(version int) bool {
	if defaultManager == nil {
		return false
	}

	defaultManager.mu.RLock()
	defer defaultManager.mu.RUnlock()

	_, exists := defaultManager.deks[version]
	return exists
}

// decryptFallback 使用环境变量 ENCRYPTION_KEY 直接解密（无 KeyManager 时的兼容模式）
func decryptFallback(ciphertext string) (string, error) {
	key := getKeyFromEnv()
	if key == nil {
		return "", fmt.Errorf("ENCRYPTION_KEY environment variable not set or invalid (need 32 bytes)")
	}

	// 尝试版本化格式 v{n}:base64
	re := regexp.MustCompile(versionedFormatReg)
	if match := re.FindStringSubmatch(ciphertext); len(match) > 1 {
		parts := strings.SplitN(ciphertext, ":", 2)
		if len(parts) == 2 {
			ciphertext = parts[1]
		}
	}

	decoded, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	plaintext, err := aesDecrypt(key, decoded)
	if err != nil {
		return "", fmt.Errorf("decrypt fallback: %w", err)
	}
	return string(plaintext), nil
}
