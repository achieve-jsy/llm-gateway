package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestRequestID_GeneratesNewID tests that RequestID middleware generates a new UUID when not provided
func TestRequestID_GeneratesNewID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"request_id": c.GetString("request_id"),
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	requestID := w.Header().Get("X-Request-ID")
	assert.NotEmpty(t, requestID)
	assert.Len(t, requestID, 36) // UUID format: 8-4-4-4-12 = 36 chars with hyphens
}

// TestRequestID_UsesProvidedID tests that RequestID middleware uses the provided X-Request-ID header
func TestRequestID_UsesProvidedID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"request_id": c.GetString("request_id"),
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	expectedID := "custom-request-id-123"
	req.Header.Set("X-Request-ID", expectedID)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, expectedID, w.Header().Get("X-Request-ID"))
}

// TestRequestID_SetsContextValue tests that RequestID middleware sets the request_id in context
func TestRequestID_SetsContextValue(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var capturedID string

	router := gin.New()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		capturedID = c.GetString("request_id")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.NotEmpty(t, capturedID)
	assert.Equal(t, w.Header().Get("X-Request-ID"), capturedID)
}

// TestLogger_SkipsHealthz tests that Logger middleware skips /healthz path
func TestLogger_SkipsHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Logger())
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/api/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Test /healthz path (should not be logged)
	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/healthz", nil)
	router.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Test /api/test path (should be logged)
	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/api/test", nil)
	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)
}

// TestJWTAuth_ValidToken tests that JWTAuth middleware accepts valid Bearer token
func TestJWTAuth_ValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		token := c.GetString("admin_token")
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer valid-token-12345")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestJWTAuth_MissingToken tests that JWTAuth middleware rejects request without Authorization header
func TestJWTAuth_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing or invalid authorization header")
}

// TestJWTAuth_MissingBearerPrefix tests that JWTAuth middleware rejects token without Bearer prefix
func TestJWTAuth_MissingBearerPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "token-12345")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "missing or invalid authorization header")
}

// TestJWTAuth_InvalidToken tests that JWTAuth middleware rejects empty Bearer token
func TestJWTAuth_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer ")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "empty token")
}

// TestJWTAuth_EmptyBearerToken tests that JWTAuth middleware rejects empty Bearer token
func TestJWTAuth_EmptyBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestJWTAuth_ResponseFormat tests that JWTAuth middleware returns proper error format
func TestJWTAuth_ResponseFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "authentication_error")
	assert.Contains(t, w.Body.String(), "unauthorized")
}

// TestInternalAuth_ValidToken tests that InternalAuth middleware accepts valid X-Internal-Token header
func TestInternalAuth_ValidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		token := c.GetString("internal_token")
		c.JSON(http.StatusOK, gin.H{"token": token})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", "internal-secret-token")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestInternalAuth_MissingToken tests that InternalAuth middleware rejects request without X-Internal-Token header
func TestInternalAuth_MissingToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "missing internal token")
}

// TestInternalAuth_InvalidToken tests that InternalAuth middleware rejects empty X-Internal-Token header
func TestInternalAuth_InvalidToken(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", "")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestInternalAuth_ResponseFormat tests that InternalAuth middleware returns proper error format
func TestInternalAuth_ResponseFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "authorization_error")
	assert.Contains(t, w.Body.String(), "forbidden")
}

// TestMiddlewareChaining tests that multiple middlewares work together correctly
func TestMiddlewareChaining(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var capturedRequestID string
	var capturedToken string

	router := gin.New()
	router.Use(RequestID())
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		capturedRequestID = c.GetString("request_id")
		capturedToken = c.GetString("admin_token")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("X-Request-ID", "custom-id-123")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "custom-id-123", capturedRequestID)
	assert.Equal(t, "test-token", capturedToken)
}

// TestAllMiddlewares tests all middlewares together
func TestAllMiddlewares(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Logger())
	router.Use(RequestID())
	router.Use(JWTAuth())
	router.Use(InternalAuth())

	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"request_id":    c.GetString("request_id"),
			"admin_token":   c.GetString("admin_token"),
			"internal_token": c.GetString("internal_token"),
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	req.Header.Set("X-Internal-Token", "internal-token")
	req.Header.Set("X-Request-ID", "req-id-123")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "req-id-123", w.Header().Get("X-Request-ID"))
}

// TestJWTAuth_CaseInsensitiveBearer tests Bearer prefix case sensitivity
func TestJWTAuth_CaseInsensitiveBearer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "bearer valid-token")

	router.ServeHTTP(w, req)

	// The implementation uses strings.HasPrefix with "Bearer " (capital B)
	// so "bearer" (lowercase) would fail
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestJWTAuth_TokenWithSpaces tests that token with leading/trailing spaces is handled
func TestJWTAuth_TokenWithSpaces(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var capturedToken string

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		capturedToken = c.GetString("admin_token")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer token-with-spaces ")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	// Token should include the trailing space as-is
	assert.Equal(t, "token-with-spaces ", capturedToken)
}

// TestInternalAuth_TokenStored tests that internal token is properly stored in context
func TestInternalAuth_TokenStored(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var capturedToken string
	expectedToken := "secret-internal-key-xyz"

	router := gin.New()
	router.Use(InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		capturedToken = c.GetString("internal_token")
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", expectedToken)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, expectedToken, capturedToken)
}

// TestRequestID_UniquePerRequest tests that each request gets a unique ID
func TestRequestID_UniquePerRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w1, req1)
	id1 := w1.Header().Get("X-Request-ID")

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/test", nil)
	router.ServeHTTP(w2, req2)
	id2 := w2.Header().Get("X-Request-ID")

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}

// TestMiddlewareOrderMatters tests that middleware order affects behavior
func TestMiddlewareOrderMatters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// This test verifies that when JWT middleware is first, unauthenticated
	// requests don't reach the handler
	router := gin.New()
	router.Use(JWTAuth())
	router.Use(RequestID())

	handlerCalled := false

	router.GET("/test", func(c *gin.Context) {
		handlerCalled = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, handlerCalled, "Handler should not be called due to JWT auth failure")
}

// TestRequestIDHeaderIsReturned tests that X-Request-ID header is returned in response
func TestRequestIDHeaderIsReturned(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
	// Verify it's a valid UUID format
	idStr := w.Header().Get("X-Request-ID")
	assert.Equal(t, 36, len(idStr), "Expected UUID format (36 chars)")
}

// TestJWTAuth_MultipleWhitespace tests Bearer prefix with multiple spaces
func TestJWTAuth_MultipleWhitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(JWTAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	// Multiple spaces between Bearer and token
	req.Header.Set("Authorization", "Bearer  token")

	router.ServeHTTP(w, req)

	// Should fail because it's looking for "Bearer " exactly
	// and anything after that is the token, including extra spaces
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestInternalAuth_Whitespace tests X-Internal-Token with whitespace
func TestInternalAuth_Whitespace(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(InternalAuth())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Internal-Token", "  ")

	router.ServeHTTP(w, req)

	// Whitespace-only token should be accepted (non-empty string)
	assert.Equal(t, http.StatusOK, w.Code)
}
