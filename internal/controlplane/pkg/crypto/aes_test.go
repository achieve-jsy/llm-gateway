package crypto

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validKey = "0123456789abcdef0123456789abcdef" // 32 字节

func withKey(t *testing.T, key string) func() {
	t.Helper()
	old, hadOld := os.LookupEnv("ENCRYPTION_KEY")
	require.NoError(t, os.Setenv("ENCRYPTION_KEY", key))
	return func() {
		if hadOld {
			_ = os.Setenv("ENCRYPTION_KEY", old)
		} else {
			_ = os.Unsetenv("ENCRYPTION_KEY")
		}
	}
}

// TestAesEncryptDecrypt_RoundTrip 低级 AES 加解密对称
func TestAesEncryptDecrypt_RoundTrip(t *testing.T) {
	key := []byte(validKey) // 32 字节

	cases := [][]byte{
		[]byte("sk-test-1234567890"),
		[]byte(""),
		[]byte("中文凭证"),
		[]byte("包含表情😀的字符串"),
		[]byte(strings.Repeat("A", 1024)),
	}

	for _, plaintext := range cases {
		t.Run(string(plaintext), func(t *testing.T) {
			cipher, err := aesEncrypt(key, plaintext)
			require.NoError(t, err)
			require.NotEqual(t, plaintext, cipher)

			got, err := aesDecrypt(key, cipher)
			require.NoError(t, err)
			// 空字节串语义对齐：crypto/cipher.GCM.Open 返回 nil 表示空切片，
			// 与 []byte("") 长度均为 0，使用长度+逐字节比对避免 nil != []byte{} 误判。
			assert.Equal(t, len(plaintext), len(got))
			assert.Equal(t, string(plaintext), string(got))
		})
	}
}

// TestAesDecrypt_InvalidBase64 非 base64 字符应触发解码错误
func TestAesDecrypt_InvalidBase64(t *testing.T) {
	key := []byte(validKey)
	// "aaaa" base64 解码后 3 字节 < 12 字节 nonceSize
	_, err := aesDecrypt(key, []byte("aaaa"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

// TestAesDecrypt_WrongKey 用另一把 key 解密会失败
func TestAesDecrypt_WrongKey(t *testing.T) {
	key := []byte(validKey)
	plaintext := []byte("payload")

	cipher, err := aesEncrypt(key, plaintext)
	require.NoError(t, err)

	// 用另一把 key 解密
	wrongKey := []byte("fedcba9876543210fedcba9876543210")
	_, err = aesDecrypt(wrongKey, cipher)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

// TestMask 凭证脱敏
func TestMask(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "****"},
		{"<=8 chars", "abcdefgh", "****"},
		{"9 chars", "123456789", "123****789"},
		{"12 chars", "123456789012", "123****9012"},
		{"13 chars", "sk-1234567890", "sk-****7890"},
		{"long", "very-long-credential-string-1234", "ver****1234"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Mask(tt.in))
		})
	}
}

// TestDecrypt_NoManager_FailsImmediately
//
// 安全语义升级后：未初始化 KeyManager 时，Decrypt 直接拒绝（不再回退环境变量），
// 避免生产部署时静默使用弱密钥。旧版"legacy fallback"现在仅在已初始化的管理器中生效，
// 由 manager_test.go::TestDecrypt_LegacyFormat_WithManager 覆盖。
func TestDecrypt_NoManager_FailsImmediately(t *testing.T) {
	defer withKey(t, validKey)()

	// 构造一个旧格式密文，但 defaultManager 为空
	key := []byte(validKey)
	ciphertext, err := aesEncrypt(key, []byte("legacy-payload"))
	require.NoError(t, err)

	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	_, err = Decrypt(encoded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key manager not initialized")
}
