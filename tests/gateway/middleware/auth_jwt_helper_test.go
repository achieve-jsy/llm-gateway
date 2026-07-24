package middleware_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"sync"
	"testing"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// 本文件修复的是一处与本分支（docs/trace-body-memory-design.md）无关的既有缺陷：
// main（commit 195b3495「fix(auth): verify JWT signature in AuthMiddleware」）把
// AuthMiddleware 改成「必须用 RSA 公钥校验 RS256 签名」并新增第二个参数后，
// 本目录既有的 auth 测试仍按旧写法（单参数 + HS256 + 任意 secret 签名）调用，
// 导致 tests/gateway/middleware 整包无法编译。补齐共享的测试签名器后，
// 该包恢复可编译，本目录下 trace 大请求体相关的验收测试才能被执行。

var (
	jwtTestOnce    sync.Once
	jwtTestKey     *rsa.PrivateKey
	jwtTestManager *acrypto.JWTManager
)

// testJWTSigner 懒加载测试用 RSA 私钥与对应的 JWTManager。
// JWTManager 没有「用给定私钥构造」的导出构造函数，只能经 AUTH_JWT_PRIVATE_KEY_PEM 注入。
func testJWTSigner() (*rsa.PrivateKey, *acrypto.JWTManager) {
	jwtTestOnce.Do(func() {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic("测试用 RSA 私钥生成失败: " + err.Error())
		}
		pemBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		if err := os.Setenv("AUTH_JWT_PRIVATE_KEY_PEM", string(pemBytes)); err != nil {
			panic("注入测试 JWT 私钥失败: " + err.Error())
		}
		mgr, err := acrypto.NewJWTManagerFromEnv()
		if err != nil {
			panic("测试用 JWTManager 构造失败: " + err.Error())
		}
		jwtTestKey, jwtTestManager = key, mgr
	})
	return jwtTestKey, jwtTestManager
}

// testJWTManager 返回测试用 JWTManager，供 AuthMiddleware 的第二个参数使用
// （传 nil 会禁用 JWT 路径，无法覆盖 JWT 用例）。
func testJWTManager() *acrypto.JWTManager {
	_, mgr := testJWTSigner()
	return mgr
}

// makeJWT 生成测试 JWT：AuthMiddleware 现在强制校验 RS256 签名，
// 因此必须用与 testJWTSigner 同一把 RSA 私钥签名（原实现用 HS256 + 任意 secret，
// 新实现下会因签名方式不符返回 401）。
func makeJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	key, _ := testJWTSigner()
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(key)
	require.NoError(t, err)
	return token
}
