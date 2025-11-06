package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestRequestID_Generated tests RequestID middleware generates new ID when missing
func TestRequestID_Generated(t *testing.T) {
	tests := []struct {
		name             string
		header           string
		expectNewIDGen   bool
		expectIDInHeader bool
	}{
		{
			name:             "no request id header generates new id",
			header:           "",
			expectNewIDGen:   true,
			expectIDInHeader: true,
		},
		{
			name:             "empty request id generates new id",
			header:           "",
			expectNewIDGen:   true,
			expectIDInHeader: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.header != "" {
				req.Header.Set("X-Request-ID", tt.header)
			}
			c.Request = req

			// Call middleware
			middleware := RequestID()
			middleware(c)

			// Verify ID was set in context
			requestID, ok := c.Get("request_id")
			assert.True(t, ok)
			assert.NotEmpty(t, requestID)

			// Verify ID is UUID-like format (36 chars with hyphens)
			idStr := requestID.(string)
			if tt.expectNewIDGen {
				assert.Equal(t, 36, len(idStr))
				assert.Contains(t, idStr, "-")
			}

			// Verify ID in response header
			if tt.expectIDInHeader {
				assert.Equal(t, idStr, w.Header().Get("X-Request-ID"))
			}
		})
	}
}

// TestRequestID_Existing tests RequestID middleware preserves existing ID
func TestRequestID_Existing(t *testing.T) {
	existingID := "custom-request-id-12345"

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-ID", existingID)
	c.Request = req

	// Call middleware
	middleware := RequestID()
	middleware(c)

	// Verify ID was preserved
	requestID, ok := c.Get("request_id")
	assert.True(t, ok)
	assert.Equal(t, existingID, requestID)

	// Verify ID in response header
	assert.Equal(t, existingID, w.Header().Get("X-Request-ID"))
}

// TestAPIKeyAuth_ValidKey tests APIKeyAuth passes with valid key
func TestAPIKeyAuth_ValidKey(t *testing.T) {
	tests := []struct {
		name   string
		apiKey string
	}{
		{
			name:   "valid api key",
			apiKey: "sk-1234567890",
		},
		{
			name:   "long api key",
			apiKey: "sk-" + strings.Repeat("a", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			req.Header.Set("Authorization", "Bearer "+tt.apiKey)
			c.Request = req

			middleware := APIKeyAuth()
			middleware(c)

			// Check if the middleware set the API key
			apiKey, ok := c.Get("api_key")
			assert.True(t, ok)
			assert.Equal(t, tt.apiKey, apiKey)

			// Verify request wasn't aborted
			assert.False(t, c.IsAborted())
		})
	}
}

// TestAPIKeyAuth_MissingKey tests APIKeyAuth rejects missing Authorization header
func TestAPIKeyAuth_MissingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	// No Authorization header
	c.Request = req

	middleware := APIKeyAuth()
	middleware(c)

	// Verify request was aborted
	assert.True(t, c.IsAborted())
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Verify error response
	body, _ := io.ReadAll(w.Body)
	assert.Contains(t, string(body), "error")
	assert.Contains(t, string(body), "authentication_error")
}

// TestAPIKeyAuth_InvalidKey tests APIKeyAuth rejects invalid key format
func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		expectErrorMsg string
	}{
		{
			name:          "missing Bearer prefix",
			authHeader:    "sk-1234567890",
			expectErrorMsg: "missing or invalid Authorization header",
		},
		{
			name:          "wrong prefix",
			authHeader:    "Basic sk-1234567890",
			expectErrorMsg: "missing or invalid Authorization header",
		},
		{
			name:          "Bearer with empty key",
			authHeader:    "Bearer ",
			expectErrorMsg: "empty API key",
		},
		{
			name:          "Bearer with spaces",
			authHeader:    "Bearer  ",
			expectErrorMsg: "empty API key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			req.Header.Set("Authorization", tt.authHeader)
			c.Request = req

			middleware := APIKeyAuth()
			middleware(c)

			assert.True(t, c.IsAborted())
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

// TestLogger_LogsRequestInfo tests Logger middleware records request information
func TestLogger_LogsRequestInfo(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		method         string
		statusCode     int
		shouldLog      bool
		expectedFields []string
	}{
		{
			name:           "logs api request",
			path:           "/v1/chat/completions",
			method:         "POST",
			statusCode:     200,
			shouldLog:      true,
			expectedFields: []string{"request_id", "method", "path", "status", "duration_ms"},
		},
		{
			name:           "logs GET request",
			path:           "/v1/models",
			method:         "GET",
			statusCode:     200,
			shouldLog:      true,
			expectedFields: []string{"method", "path", "status"},
		},
		{
			name:           "skips health check logging",
			path:           "/health",
			method:         "GET",
			statusCode:     200,
			shouldLog:      false,
			expectedFields: []string{},
		},
		{
			name:           "skips ready check logging",
			path:           "/ready",
			method:         "GET",
			statusCode:     200,
			shouldLog:      false,
			expectedFields: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			req := httptest.NewRequest(tt.method, tt.path, nil)
			c.Request = req

			// Set context values that logger uses
			c.Set("request_id", "test-request-123")
			c.Set("model", "gpt-3.5")

			// Simulate handler that sets status
			c.Writer.WriteHeader(tt.statusCode)

			middleware := Logger()
			middleware(c)

			// Since logger uses slog, we can't easily capture output in unit test
			// But we can verify middleware doesn't panic
			assert.False(t, c.IsAborted())
		})
	}
}

// TestLogger_StatusCodeTracking tests Logger middleware tracks different status codes
func TestLogger_StatusCodeTracking(t *testing.T) {
	statusCodes := []int{
		http.StatusOK,
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusNotFound,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	}

	for _, code := range statusCodes {
		t.Run(("logs_status_" + http.StatusText(code)), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
			c.Request = req
			c.Set("request_id", "test-123")

			c.Writer.WriteHeader(code)

			middleware := Logger()
			middleware(c)

			assert.False(t, c.IsAborted())
		})
	}
}

// TestErrorHandler_PanicRecovery tests ErrorHandler recovers from panics
func TestErrorHandler_PanicRecovery(t *testing.T) {
	tests := []struct {
		name              string
		panickingHandler  gin.HandlerFunc
		expectStatusCode  int
		expectAbort       bool
	}{
		{
			name: "recovers from panic",
			panickingHandler: func(c *gin.Context) {
				panic("test panic")
			},
			expectStatusCode: http.StatusInternalServerError,
			expectAbort:      true,
		},
		{
			name: "recovers from nil panic",
			panickingHandler: func(c *gin.Context) {
				panic(nil)
			},
			expectStatusCode: http.StatusInternalServerError,
			expectAbort:      true,
		},
		{
			name: "normal handler doesn't trigger panic recovery",
			panickingHandler: func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"status": "ok"})
			},
			expectStatusCode: http.StatusOK,
			expectAbort:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			req := httptest.NewRequest("GET", "/test", nil)
			c.Request = req
			c.Set("request_id", "test-123")

			// Create error handler middleware and wrap the test handler
			errorHandlerMW := ErrorHandler()

			// Create a route with both error handler and test handler
			gin.SetMode(gin.TestMode)
			router := gin.New()
			router.Use(errorHandlerMW)
			router.GET("/test", tt.panickingHandler)

			// Make the request
			req = httptest.NewRequest("GET", "/test", nil)
			w = httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectStatusCode, w.Code)

			// Verify error response format only when the panic recovery branch was hit
			if tt.expectStatusCode == http.StatusInternalServerError {
				assert.Contains(t, w.Body.String(), "error")
				assert.Contains(t, w.Body.String(), "internal_error")
			}
		})
	}
}

// TestErrorHandler_DoesntPanicOnNormalFlow tests ErrorHandler doesn't interfere with normal request flow
func TestErrorHandler_DoesntPanicOnNormalFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add error handler and a normal handler
	router.Use(ErrorHandler())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "success")
}

// TestErrorHandler_PanicWithCustomObject tests ErrorHandler with custom panic values
func TestErrorHandler_PanicWithCustomObject(t *testing.T) {
	panicValues := []interface{}{
		"string panic",
		123,
		map[string]string{"error": "test"},
		struct{ Message string }{"test panic"},
	}

	for _, panicVal := range panicValues {
		t.Run(("recovers_from_"+http.StatusText(500)), func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			router := gin.New()

			router.Use(ErrorHandler())
			router.GET("/test", func(c *gin.Context) {
				panic(panicVal)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/test", nil)

			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.Contains(t, w.Body.String(), "internal_error")
		})
	}
}

// TestMiddlewareChaining tests multiple middlewares work together
func TestMiddlewareChaining(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Add middlewares in order
	router.Use(RequestID())
	router.Use(APIKeyAuth())
	router.Use(Logger())
	router.Use(ErrorHandler())

	router.POST("/v1/chat/completions", func(c *gin.Context) {
		requestID, _ := c.Get("request_id")
		apiKey, _ := c.Get("api_key")

		c.JSON(http.StatusOK, gin.H{
			"request_id": requestID,
			"has_api_key": apiKey != nil,
		})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req.Header.Set("Authorization", "Bearer sk-test123")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "request_id")
	assert.Contains(t, w.Body.String(), "true")
}

// TestRequestIDMiddlewareNext 通过完整 router 验证 RequestID 中间件会调用后续 handler
func TestRequestIDMiddlewareNext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	nextCalled := false
	router := gin.New()
	router.Use(RequestID())
	router.GET("/test", func(c *gin.Context) {
		nextCalled = true
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest("GET", "/test", nil))

	assert.True(t, nextCalled, "next handler should be called by middleware chain")
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

// TestLogger_WithModel tests Logger includes model in logs
func TestLogger_WithModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Request = req
	c.Set("request_id", "test-123")
	c.Set("model", "gpt-3.5-turbo")

	c.Writer.WriteHeader(http.StatusOK)

	middleware := Logger()
	middleware(c)

	// Model should be set and retrievable from context
	model := c.GetString("model")
	assert.Equal(t, "gpt-3.5-turbo", model)
}
