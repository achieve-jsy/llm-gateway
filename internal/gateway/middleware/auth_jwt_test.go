package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	"github.com/example/llm-gateway/internal/gateway/infra"
	"github.com/gin-gonic/gin"
	j "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// newTestJWTManager 生成一个带随机 RSA 密钥的 JWTManager，用于签发/验签测试 token。
func newTestJWTManager(t *testing.T) *acrypto.JWTManager {
	t.Helper()
	// 无 AUTH_JWT_PRIVATE_KEY_PEM 时生成 dev key
	m, err := acrypto.NewJWTManagerFromEnv()
	require.NoError(t, err)
	require.NotNil(t, m)
	return m
}

// signTestJWT 用 manager 私钥签发 RS256 token。
func signTestJWT(t *testing.T, m *acrypto.JWTManager, claims j.MapClaims) string {
	t.Helper()
	tok := j.NewWithClaims(m.SigningMethod(), claims)
	signed, err := tok.SignedString(m.PrivateKey())
	require.NoError(t, err)
	return signed
}

// runAuthMiddleware 用给定 token 跑一次 AuthMiddleware，返回响应码与 identity。
// authClient 传 nil（测试里不触发 sk- 路径）。
func runAuthMiddleware(token string, jwtManager *acrypto.JWTManager) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authClient := &infra.AuthClient{}
	r.GET("/test", AuthMiddleware(authClient, jwtManager), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r.ServeHTTP(w, req)
	return w
}

func TestAuthMiddleware_JWTSignatureVerified(t *testing.T) {
	m := newTestJWTManager(t)

	// 用真实密钥签发合法 token → 应放行
	valid := signTestJWT(t, m, j.MapClaims{
		"user_id": "u-valid",
		"dept_id": "d-1",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	w := runAuthMiddleware(valid, m)
	require.Equal(t, http.StatusOK, w.Code, "合法签名的 JWT 应通过")
}

func TestAuthMiddleware_JWTForgeRejected(t *testing.T) {
	m := newTestJWTManager(t)

	// 手工伪造：改 claims 后不重新签名（header.payload.签名三段，payload 替换）
	// 构造一个 header 与原 token 相同、但 payload 篡改、签名段随便填的字符串。
	parts := []string{
		"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9", // header: RS256
		"eyJ1c2VyX2lkIjoidS1mb3JnZWQiLCJkZXB0X2lkIjoiZC1oYWNrIiwiZXhwIjoyOTk5OTk5OTk5fQ", // payload: user_id=u-forged, dept_id=d-hack
		"Zm9yZ2Vkc2lnbmF0dXJl", // signature: "forgedsignature"
	}
	forged := strings.Join(parts, ".")
	w := runAuthMiddleware(forged, m)
	require.Equal(t, http.StatusUnauthorized, w.Code, "伪造签名的 JWT 应被拒")
}

func TestAuthMiddleware_JWTMissingManagerRejected(t *testing.T) {
	// jwtManager=nil → 任何 eyJ token 都应被拒（JWT 路径禁用）
	parts := []string{
		"eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9",
		"eyJ1c2VyX2lkIjoidS14In0",
		"c2ln",
	}
	eyJ := strings.Join(parts, ".")
	w := runAuthMiddleware(eyJ, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, "未配置 JWT manager 时 eyJ token 应被拒")
}

func TestAuthMiddleware_JWTSignatureFromOtherKeyRejected(t *testing.T) {
	// 用 A 密钥签、用 B 密钥验 → 应被拒
	ma := newTestJWTManager(t)
	mb := newTestJWTManager(t)
	validFromA := signTestJWT(t, ma, j.MapClaims{
		"user_id": "u-a",
		"exp":     time.Now().Add(time.Hour).Unix(),
	})
	w := runAuthMiddleware(validFromA, mb)
	require.Equal(t, http.StatusUnauthorized, w.Code, "用另一公钥验证应失败")
}

func TestAuthMiddleware_JWTExpiredRejected(t *testing.T) {
	m := newTestJWTManager(t)
	expired := signTestJWT(t, m, j.MapClaims{
		"user_id": "u-old",
		"exp":     time.Now().Add(-time.Hour).Unix(),
	})
	w := runAuthMiddleware(expired, m)
	require.Equal(t, http.StatusUnauthorized, w.Code, "过期 JWT 应被拒")
}
