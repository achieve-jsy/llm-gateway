package crypto

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB 为每个测试创建一个独立的内存 SQLite 数据库。
//
// 之前使用 `file::memory:?cache=shared`，同一进程内多个 test 会共享同一个内存 DB，
// 造成 system_configs / encryption_keys 表数据互相污染（典型表现为 RotateKey
// 多轮换测试看到上一个测试残留的 active 行）。改用纯 `:memory:` DSN 后每次连接
// 都是独立的内存 DB，测试隔离。
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 创建表
	err = db.Exec(`
		CREATE TABLE IF NOT EXISTS system_configs (
			key VARCHAR(100) PRIMARY KEY,
			value TEXT NOT NULL,
			encrypted BOOLEAN NOT NULL DEFAULT false,
			description VARCHAR(500) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS encryption_keys (
			version INT PRIMARY KEY,
			encrypted_key TEXT NOT NULL,
			algorithm VARCHAR(50) NOT NULL DEFAULT 'AES-256-GCM',
			status VARCHAR(20) NOT NULL DEFAULT 'active',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			retired_at TIMESTAMP
		);
	`).Error
	require.NoError(t, err)

	return db
}

// resetDefaultManager 重置默认管理器（用于测试隔离）
func resetDefaultManager() {
	defaultManager = nil
}

// TestInitFromDB_FirstRun 首次运行应创建 KEK 和 DEK v1
func TestInitFromDB_FirstRun(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)

	err := InitFromDB(db)
	require.NoError(t, err)

	assert.NotNil(t, defaultManager)
	assert.NotNil(t, defaultManager.kek)
	assert.Equal(t, len(defaultManager.kek), 32)
	assert.Equal(t, defaultManager.activeVersion, 1)
	assert.True(t, HasVersion(1))

	// 验证数据库中有 KEK
	var kekConfig SystemConfig
	err = db.Where("key = ?", kekKey).First(&kekConfig).Error
	require.NoError(t, err)
	assert.NotEmpty(t, kekConfig.Value)

	// 验证数据库中有 DEK v1
	var dekKey EncryptionKey
	err = db.Where("version = ?", 1).First(&dekKey).Error
	require.NoError(t, err)
	assert.Equal(t, dekKey.Status, "active")
	assert.NotEmpty(t, dekKey.EncryptedKey)
}

// TestInitFromDB_Subsequent 后续运行应加载已有的 KEK 和 DEK
func TestInitFromDB_Subsequent(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)

	// 首次初始化
	err := InitFromDB(db)
	require.NoError(t, err)

	firstKEK := defaultManager.kek
	firstDEK := defaultManager.deks[1]

	// 重置管理器，再次初始化（模拟重启）
	resetDefaultManager()

	err = InitFromDB(db)
	require.NoError(t, err)

	// KEK 和 DEK 应该相同
	assert.Equal(t, firstKEK, defaultManager.kek)
	assert.Equal(t, firstDEK, defaultManager.deks[1])
	assert.Equal(t, defaultManager.activeVersion, 1)
}

// TestEncryptDecrypt_VersionedFormat 加密输出应包含版本前缀
func TestEncryptDecrypt_VersionedFormat(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	plaintext := "test-credential"
	ciphertext, err := Encrypt(plaintext)
	require.NoError(t, err)

	// 应该以 v1: 开头
	assert.True(t, strings.HasPrefix(ciphertext, "v1:"))

	// 应该能解密
	got, err := Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

// TestEncryptDecrypt_RoundTrip 加解密对称性
func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	cases := []string{
		"sk-test-1234567890",
		"",
		"中文凭证",
		"包含表情😀的字符串",
		strings.Repeat("A", 1024),
	}

	for _, plaintext := range cases {
		t.Run(plaintext, func(t *testing.T) {
			cipher, err := Encrypt(plaintext)
			require.NoError(t, err)
			require.NotEqual(t, plaintext, cipher)

			got, err := Decrypt(cipher)
			require.NoError(t, err)
			assert.Equal(t, plaintext, got)
		})
	}
}

// TestEncrypt_NotInitialized 未初始化时应报错
func TestEncrypt_NotInitialized(t *testing.T) {
	defer resetDefaultManager()

	_, err := Encrypt("test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// TestDecrypt_NotInitialized 未初始化时应报错
func TestDecrypt_NotInitialized(t *testing.T) {
	defer resetDefaultManager()

	_, err := Decrypt("v1:dGVzdA==")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not initialized")
}

// TestDecrypt_InvalidVersion 不存在的版本应报错
func TestDecrypt_InvalidVersion(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	// 尝试解密不存在的版本
	_, err = Decrypt("v99:dGVzdA==")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestRotateKey 密钥轮换应创建新版本
func TestRotateKey(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	// 用 v1 加密
	plaintext := "test-data"
	ciphertextV1, err := Encrypt(plaintext)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(ciphertextV1, "v1:"))

	// 轮换密钥
	err = RotateKey(db)
	require.NoError(t, err)

	assert.Equal(t, GetActiveVersion(), 2)
	assert.True(t, HasVersion(1))
	assert.True(t, HasVersion(2))

	// 新加密应该使用 v2
	ciphertextV2, err := Encrypt("new-data")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(ciphertextV2, "v2:"))

	// 旧密文应该仍然可以解密
	got, err := Decrypt(ciphertextV1)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

// TestRotateKey_MultipleRotations 多次轮换应创建多个版本
func TestRotateKey_MultipleRotations(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	// 执行多次轮换
	for i := 2; i <= 5; i++ {
		err = RotateKey(db)
		require.NoError(t, err)
		assert.Equal(t, GetActiveVersion(), i)
	}

	// 验证所有版本都存在
	for i := 1; i <= 5; i++ {
		assert.True(t, HasVersion(i))
	}

	// 验证数据库中的状态
	var keys []EncryptionKey
	err = db.Order("version").Find(&keys).Error
	require.NoError(t, err)
	assert.Len(t, keys, 5)

	// 只有最后一个应该是 active
	for _, key := range keys {
		if key.Version == 5 {
			assert.Equal(t, key.Status, "active")
		} else {
			assert.Equal(t, key.Status, "rotating")
		}
	}
}

// TestDecrypt_LegacyFormat_WithManager 即使有管理器，也应支持旧格式（通过环境变量）
func TestDecrypt_LegacyFormat_WithManager(t *testing.T) {
	defer resetDefaultManager()
	defer func() {
		old, hadOld := os.LookupEnv("ENCRYPTION_KEY")
		os.Setenv("ENCRYPTION_KEY", validKey)
		defer func() {
			if hadOld {
				os.Setenv("ENCRYPTION_KEY", old)
			} else {
				os.Unsetenv("ENCRYPTION_KEY")
			}
		}()
	}()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	// 设置环境变量
	os.Setenv("ENCRYPTION_KEY", validKey)

	// 用低级 AES 创建旧格式密文
	key := []byte(validKey)
	plaintext := []byte("legacy-data")
	ciphertext, err := aesEncrypt(key, plaintext)
	require.NoError(t, err)

	legacyEncoded := base64.StdEncoding.EncodeToString(ciphertext)

	// 应该能解密旧格式
	got, err := Decrypt(legacyEncoded)
	require.NoError(t, err)
	assert.Equal(t, string(plaintext), got)
}

// TestConcurrentEncryptDecrypt 并发加解密应该是安全的
func TestConcurrentEncryptDecrypt(t *testing.T) {
	defer resetDefaultManager()

	db := setupTestDB(t)
	err := InitFromDB(db)
	require.NoError(t, err)

	numGoroutines := 10
	numOperations := 50
	done := make(chan error, numGoroutines*numOperations)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				plaintext := "concurrent-test-" + string(rune(id)) + "-" + string(rune(j))
				ciphertext, err := Encrypt(plaintext)
				if err != nil {
					done <- err
					return
				}

				got, err := Decrypt(ciphertext)
				if err != nil {
					done <- err
					return
				}

				if got != plaintext {
					done <- assert.AnError
					return
				}

				done <- nil
			}
		}(i)
	}

	// 收集所有结果
	for i := 0; i < numGoroutines*numOperations; i++ {
		err := <-done
		require.NoError(t, err)
	}
}
