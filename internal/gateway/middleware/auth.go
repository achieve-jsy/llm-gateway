package middleware

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	acrypto "github.com/example/llm-gateway/internal/auth/crypto"
	"github.com/example/llm-gateway/internal/gateway/infra"
	gwmodel "github.com/example/llm-gateway/internal/gateway/model"
	"github.com/gin-gonic/gin"
	j "github.com/golang-jwt/jwt/v5"
)

// AuthMiddleware provides unified authentication (API key via auth service + JWT).
// 同时支持两种 Header 格式：
//   - OpenAI 兼容格式：Authorization: Bearer sk-xxx
//   - Anthropic 原生格式：x-api-key: sk-xxx（Claude Code 等客户端使用）
//
// jwtManager 为可选参数（nil 时 JWT 路径禁用）：持有签发 RSA 公钥，用于
// 校验 employee 自助服务（/v1/me/models 等）的 JWT 签名，杜绝 ParseUnverified
// 免验签导致的任意身份伪造。
func AuthMiddleware(authClient *infra.AuthClient, jwtManager *acrypto.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var token string
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			// OpenAI 兼容格式：Authorization: Bearer sk-xxx
			token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		} else {
			// Anthropic 原生格式：x-api-key: sk-xxx（Claude Code 使用）
			token = strings.TrimSpace(c.GetHeader("x-api-key"))
		}
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
			return
		}

		// API Key path
		if strings.HasPrefix(token, "sk-") {
			// 注意：此处不再做无意义的 JWT ParseUnverified（原第 32 行已删除）
			authStart := time.Now()
			resp, err := authClient.VerifyKey(c.Request.Context(), token)
			if err != nil {
				// 这个分支此前不打任何日志：发布/蓝绿切流期间 verify 失败时，服务端没有任何证据
				// 能区分「控制面不可达 / 内部 token 失效(403) / 5xx / 3s 超时」，而用户侧一律显示
				// 「API Key 无效」。补上错误与耗时后，才能把故障时刻与切流时间对齐。
				slog.ErrorContext(c.Request.Context(), "auth: verify key failed",
					"error", err,
					"key_suffix", maskKeySuffix(token),
					"elapsed_ms", time.Since(authStart).Milliseconds(),
					"client_ip", c.ClientIP(),
					"path", c.Request.URL.Path,
				)
				c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
					"error": gin.H{"type": "service_unavailable", "message": "auth service unavailable", "code": "auth_service_unavailable"},
				})
				return
			}
			if resp.Data.Status != "active" {
				// 同 503 分支：401 也必须留痕，否则「某批 key 突然全部 invalid」无法定位到
				// 具体是 key 被禁用/过期，还是控制面读到了不一致的数据。
				slog.WarnContext(c.Request.Context(), "auth: key not active",
					"status", resp.Data.Status,
					"key_id", resp.Data.KeyID,
					"key_suffix", maskKeySuffix(token),
					"elapsed_ms", time.Since(authStart).Milliseconds(),
					"client_ip", c.ClientIP(),
				)
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": gin.H{"type": "authentication_error", "message": "invalid or disabled API key", "code": "invalid_api_key"},
				})
				return
			}

			// v0.8: IP 白名单校验（仅 sk- 路径生效）
			// 白名单为空（旧 Key / 未配置）时视为不限制，保持向后兼容。
			if !IsIPAllowed(ExtractClientIP(c.GetHeader("X-Forwarded-For"), c.GetHeader("X-Real-IP"), c.Request.RemoteAddr), resp.Data.IPAllowlist) {
				slog.WarnContext(c.Request.Context(), "auth: ip allowlist rejected",
					"key_name", resp.Data.KeyName,
					"client_ip", ExtractClientIP(c.GetHeader("X-Forwarded-For"), c.GetHeader("X-Real-IP"), c.Request.RemoteAddr))
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"error": gin.H{"type": "authentication_error", "message": "client IP not in allowlist", "code": "ip_not_allowed"},
				})
				return
			}

			// log_enabled: nil（旧缓存无此字段）时默认 true，保持原有行为
			logEnabled := resp.Data.LogEnabled == nil || *resp.Data.LogEnabled
			slog.DebugContext(c.Request.Context(), "auth: key verified",
				"key_name", resp.Data.KeyName,
				"log_enabled", logEnabled,
			)

			identity := &gwmodel.UserIdentity{
				UserID:          resp.Data.User.UserID,
				KeyID:           resp.Data.KeyID,
				DepartmentID:    resp.Data.User.DepartmentID,
				StudioID:        resp.Data.User.StudioID,
				AccessAllGroups: resp.Data.AccessAllGroups,
				BypassOrgCheck:  resp.Data.BypassOrgCheck,
				LogEnabled:      logEnabled,
				// v0.7: 从 verify-key 快照直接下发，无需网关再查库
				UsableGroups: resp.Data.UsableGroups,
				KeyType:      resp.Data.KeyType,
				BudgetChain:  resp.Data.BudgetChain,
			}
			c.Set("identity", identity)
			c.Set("key_id", identity.KeyID)
			c.Set("key_name", resp.Data.KeyName)
			keyType := resp.Data.KeyType
			if keyType == "" {
				keyType = "personal"
			}
			c.Set("key_type", keyType)
			c.Set("log_enabled", logEnabled)
			c.Set("user_id", identity.UserID)
			c.Set("user_name", resp.Data.User.Name)
			c.Set("dept_id", identity.DepartmentID)
			c.Set("dept_name", resp.Data.User.DeptName)
			c.Set("studio_id", identity.StudioID)
			c.Set("studio_name", resp.Data.User.StudioName)

			// 阶段计时：auth 完成时间（相对于请求抵达）
			if startTime, ok := c.Get("start_time"); ok {
				if t, ok := startTime.(time.Time); ok {
					ms := int16(time.Since(t).Milliseconds())
					c.Set("t_auth_ms", ms)
				}
			} else {
				ms := int16(time.Since(authStart).Milliseconds())
				c.Set("t_auth_ms", ms)
			}
			c.Next()
			return
		}

		// JWT path for employee self-service (e.g. /v1/me/models)
		// 必须校验签名：jwtManager 未配置时 JWT 路径禁用（拒绝一切 eyJ token），
		// 避免 ParseUnverified 免验签带来的任意身份伪造。
		if strings.HasPrefix(token, "eyJ") {
			if jwtManager == nil || jwtManager.PublicKey() == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": gin.H{"type": "authentication_error", "message": "invalid JWT", "code": "invalid_token"},
				})
				return
			}
			parser := j.NewParser(j.WithValidMethods([]string{"RS256"}), j.WithLeeway(5*time.Second))
			parsed, err := parser.ParseWithClaims(token, j.MapClaims{}, func(t *j.Token) (interface{}, error) {
				if _, ok := t.Method.(*j.SigningMethodRSA); !ok {
					return nil, j.ErrSignatureInvalid
				}
				return jwtManager.PublicKey(), nil
			})
			if err != nil || parsed == nil || !parsed.Valid {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": gin.H{"type": "authentication_error", "message": "invalid JWT", "code": "invalid_token"},
				})
				return
			}
			claims, _ := parsed.Claims.(j.MapClaims)
			uid, _ := claims["user_id"].(string)
			did, _ := claims["dept_id"].(string)
			sid, _ := claims["studio_id"].(string)
			uname, _ := claims["name"].(string)
			dname, _ := claims["dept_name"].(string)
			sname, _ := claims["studio_name"].(string)
			identity := &gwmodel.UserIdentity{UserID: uid, DepartmentID: did, StudioID: sid}
			c.Set("identity", identity)
			c.Set("user_id", uid)
			c.Set("user_name", uname)
			c.Set("dept_id", did)
			c.Set("dept_name", dname)
			c.Set("studio_id", sid)
			c.Set("studio_name", sname)
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gwmodel.ErrUnauthorized})
	}
}

// maskKeySuffix 只保留 key 末 4 位，避免日志落全量密钥。
func maskKeySuffix(token string) string {
	if len(token) <= 8 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}
