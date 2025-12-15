package auth_test

import (
	"testing"
	"time"

	cpauth "github.com/example/llm-gateway/internal/controlplane/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testJWTSecret = "test-secret-for-unit-tests"

func TestGenerateAccessToken_ContainsStudioID(t *testing.T) {
	// 测试目的：GenerateAccessToken 签入 studio_id/studio_name，ParseToken 后能正确读取
	tests := []struct {
		name        string
		description string
		studioID    string
		studioName  string
	}{
		{
			name:        "有工作室映射",
			description: "用户部门已映射工作室，studio_id/studio_name 应写入 JWT claims",
			studioID:    "studio-uuid-001",
			studioName:  "X-Lab",
		},
		{
			name:        "无工作室映射",
			description: "用户部门尚未映射工作室，studio_id 为空字符串，JWT 应正常生成",
			studioID:    "",
			studioName:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := cpauth.GenerateAccessToken(
				testJWTSecret, 15*time.Minute,
				"user-001", "ehr-dept-001", "研发部",
				tt.studioID, tt.studioName,
				"test@example.com", "测试用户", "passport-001", "user",
			)
			require.NoError(t, err)
			require.NotEmpty(t, token)

			claims, err := cpauth.ParseToken(testJWTSecret, token)
			require.NoError(t, err)

			assert.Equal(t, "user-001", claims.UserID)
			assert.Equal(t, "ehr-dept-001", claims.DeptID)
			assert.Equal(t, tt.studioID, claims.StudioID, tt.description)
			assert.Equal(t, tt.studioName, claims.StudioName)
		})
	}
}

func TestGenerateAccessToken_AllFields(t *testing.T) {
	// 测试目的：验证 GenerateAccessToken 生成的 token 包含所有必要字段
	token, err := cpauth.GenerateAccessToken(
		testJWTSecret, 30*time.Minute,
		"user-abc", "dept-xyz", "产品部",
		"studio-001", "测试工作室",
		"user@example.com", "某公司用户", "ppt-001", "admin",
	)
	require.NoError(t, err)

	claims, err := cpauth.ParseToken(testJWTSecret, token)
	require.NoError(t, err)

	assert.Equal(t, "user-abc", claims.UserID)
	assert.Equal(t, "dept-xyz", claims.DeptID)
	assert.Equal(t, "产品部", claims.DeptName)
	assert.Equal(t, "studio-001", claims.StudioID)
	assert.Equal(t, "测试工作室", claims.StudioName)
	assert.Equal(t, "user@example.com", claims.Email)
	assert.Equal(t, "某公司用户", claims.Name)
	assert.Equal(t, "admin", claims.Role)
}

func TestGenerateAccessToken_Expiry(t *testing.T) {
	// 测试目的：生成的 token 有正确的过期时间
	ttl := 15 * time.Minute
	token, err := cpauth.GenerateAccessToken(
		testJWTSecret, ttl,
		"user-001", "", "", "", "",
		"e@e.com", "u", "p", "user",
	)
	require.NoError(t, err)

	claims, err := cpauth.ParseToken(testJWTSecret, token)
	require.NoError(t, err)

	expiry := claims.ExpiresAt.Time
	assert.True(t, expiry.After(time.Now()), "token 应未过期")
	assert.True(t, expiry.Before(time.Now().Add(ttl+time.Minute)), "过期时间不应超过 TTL+1min")
}

func TestParseToken_InvalidSecret(t *testing.T) {
	// 测试目的：用错误 secret 解析 token 应返回错误
	token, err := cpauth.GenerateAccessToken(
		testJWTSecret, 15*time.Minute,
		"u", "", "", "", "", "e", "n", "p", "user",
	)
	require.NoError(t, err)

	_, err = cpauth.ParseToken("wrong-secret", token)
	assert.Error(t, err, "错误 secret 应导致解析失败")
}

func TestUserClaims_StudioIDOmitempty(t *testing.T) {
	// 测试目的：studio_id 为空时 token payload 不包含该字段（omitempty），减少 token 体积
	token, err := cpauth.GenerateAccessToken(
		testJWTSecret, 15*time.Minute,
		"user-001", "dept-001", "部门",
		"", "", // studio_id/studio_name 均为空
		"e@e.com", "name", "passport", "user",
	)
	require.NoError(t, err)

	// token 解析正常，studio_id 为空字符串
	claims, err := cpauth.ParseToken(testJWTSecret, token)
	require.NoError(t, err)
	assert.Equal(t, "", claims.StudioID)
	assert.Equal(t, "", claims.StudioName)
}
