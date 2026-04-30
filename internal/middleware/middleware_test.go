// middleware_test.go — 中间件测试。
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"suiyue/internal/middleware"
	"suiyue/internal/platform/auth"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const testSecret = "test-secret-for-middleware"

// TestMiddleware_JWTAuth_Success 测试 JWT 鉴权成功。
func TestMiddleware_JWTAuth_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 生成有效 Token
	token, err := auth.GenerateToken(1, "testuser", false, testSecret, time.Now().Add(time.Hour))
	assert.NoError(t, err)

	// 创建测试路由
	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// 发送请求
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "ok", w.Body.String())
}

// TestMiddleware_JWTAuth_Missing 测试缺少 Token。
func TestMiddleware_JWTAuth_Missing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMiddleware_JWTAuth_Invalid 测试无效 Token。
func TestMiddleware_JWTAuth_Invalid(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMiddleware_JWTAuth_Expired 测试过期 Token。
func TestMiddleware_JWTAuth_Expired(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 生成过期 Token
	token, err := auth.GenerateToken(1, "testuser", false, testSecret, time.Now().Add(-time.Hour))
	assert.NoError(t, err)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMiddleware_RequireAdmin_Success 测试管理员权限验证成功。
func TestMiddleware_RequireAdmin_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 生成管理员 Token
	token, err := auth.GenerateToken(1, "admin", true, testSecret, time.Now().Add(time.Hour))
	assert.NoError(t, err)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.Use(middleware.RequireAdmin())
	r.GET("/admin", func(c *gin.Context) {
		c.String(http.StatusOK, "admin")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "admin", w.Body.String())
}

// TestMiddleware_RequireAdmin_Forbidden 测试非管理员访问被拒绝。
func TestMiddleware_RequireAdmin_Forbidden(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 生成普通用户 Token
	token, err := auth.GenerateToken(1, "user", false, testSecret, time.Now().Add(time.Hour))
	assert.NoError(t, err)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.Use(middleware.RequireAdmin())
	r.GET("/admin", func(c *gin.Context) {
		c.String(http.StatusOK, "admin")
	})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestMiddleware_JWTAuth_SkipPaths 测试跳过鉴权路径。
func TestMiddleware_JWTAuth_SkipPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret, "/health"))
	r.GET("/health", func(c *gin.Context) {
		c.String(http.StatusOK, "healthy")
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "healthy", w.Body.String())
}

// TestMiddleware_JWTAuth_BadFormat 测试 Authorization 格式错误。
func TestMiddleware_JWTAuth_BadFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "BadFormat token123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestMiddleware_CORS 测试 CORS 中间件。
func TestMiddleware_CORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CORS_ALLOWED_ORIGINS", "")

	r := gin.New()
	r.Use(middleware.CORS())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// 正常请求
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))

	// OPTIONS 预检
	optReq := httptest.NewRequest(http.MethodOptions, "/test", nil)
	optW := httptest.NewRecorder()
	r.ServeHTTP(optW, optReq)
	assert.Equal(t, http.StatusNoContent, optW.Code)
}

// TestMiddleware_CORS_AllowedOrigins 测试按环境变量限制 Origin。
func TestMiddleware_CORS_AllowedOrigins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://app.example.com")

	r := gin.New()
	r.Use(middleware.CORS())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	allowedReq := httptest.NewRequest(http.MethodGet, "/test", nil)
	allowedReq.Header.Set("Origin", "https://app.example.com")
	allowedW := httptest.NewRecorder()
	r.ServeHTTP(allowedW, allowedReq)
	assert.Equal(t, "https://app.example.com", allowedW.Header().Get("Access-Control-Allow-Origin"))

	blockedReq := httptest.NewRequest(http.MethodGet, "/test", nil)
	blockedReq.Header.Set("Origin", "https://evil.example.com")
	blockedW := httptest.NewRecorder()
	r.ServeHTTP(blockedW, blockedReq)
	assert.Empty(t, blockedW.Header().Get("Access-Control-Allow-Origin"))
}

// TestMiddleware_RequestID 测试请求 ID 透传与生成。
func TestMiddleware_RequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetHeader(middleware.RequestIDHeader))
	})

	existingReq := httptest.NewRequest(http.MethodGet, "/test", nil)
	existingReq.Header.Set(middleware.RequestIDHeader, "req-123")
	existingW := httptest.NewRecorder()
	r.ServeHTTP(existingW, existingReq)
	assert.Equal(t, "req-123", existingW.Header().Get(middleware.RequestIDHeader))
	assert.Equal(t, "req-123", existingW.Body.String())

	generatedReq := httptest.NewRequest(http.MethodGet, "/test", nil)
	generatedW := httptest.NewRecorder()
	r.ServeHTTP(generatedW, generatedReq)
	assert.NotEmpty(t, generatedW.Header().Get(middleware.RequestIDHeader))
}

// TestMiddleware_CSRF 测试写请求必须携带自定义 CSRF Header。
func TestMiddleware_CSRF(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.CSRF())
	r.GET("/safe", func(c *gin.Context) {
		c.String(http.StatusOK, "safe")
	})
	r.POST("/write", func(c *gin.Context) {
		c.String(http.StatusOK, "write")
	})

	getReq := httptest.NewRequest(http.MethodGet, "/safe", nil)
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)
	assert.Equal(t, http.StatusOK, getW.Code)

	missingReq := httptest.NewRequest(http.MethodPost, "/write", nil)
	missingW := httptest.NewRecorder()
	r.ServeHTTP(missingW, missingReq)
	assert.Equal(t, http.StatusForbidden, missingW.Code)

	okReq := httptest.NewRequest(http.MethodPost, "/write", nil)
	okReq.Header.Set(middleware.CSRFHeaderName, middleware.CSRFHeaderValue)
	okW := httptest.NewRecorder()
	r.ServeHTTP(okW, okReq)
	assert.Equal(t, http.StatusOK, okW.Code)
}

// TestMiddleware_RateLimit 测试固定窗口限流。
func TestMiddleware_RateLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.RateLimit(2, time.Minute))
	r.POST("/limited", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/limited", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/limited", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

// TestMiddleware_GetClaims 测试获取 Claims。
func TestMiddleware_GetClaims(t *testing.T) {
	gin.SetMode(gin.TestMode)

	token, err := auth.GenerateToken(42, "claimsuser", true, testSecret, time.Now().Add(time.Hour))
	assert.NoError(t, err)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/claims", func(c *gin.Context) {
		claims, ok := middleware.GetClaims(c)
		assert.True(t, ok)
		assert.NotNil(t, claims)
		assert.Equal(t, uint64(42), claims.UserID)
		assert.Equal(t, "claimsuser", claims.Username)
		assert.True(t, claims.IsAdmin)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/claims", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_GetClaims_NoAuth 测试无鉴权时 GetClaims 返回 false。
func TestMiddleware_GetClaims_NoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/noclaim", func(c *gin.Context) {
		claims, ok := middleware.GetClaims(c)
		assert.False(t, ok)
		assert.Nil(t, claims)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/noclaim", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_GetUserID 测试获取用户 ID。
func TestMiddleware_GetUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	token, err := auth.GenerateToken(99, "iduser", false, testSecret, time.Now().Add(time.Hour))
	assert.NoError(t, err)

	r := gin.New()
	r.Use(middleware.JWTAuth(testSecret))
	r.GET("/userid", func(c *gin.Context) {
		userID, ok := middleware.GetUserID(c)
		assert.True(t, ok)
		assert.Equal(t, uint64(99), userID)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/userid", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestMiddleware_Logger 测试日志中间件。
func TestMiddleware_Logger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(middleware.Logger())
	r.GET("/log", func(c *gin.Context) {
		c.String(http.StatusOK, "logged")
	})

	req := httptest.NewRequest(http.MethodGet, "/log", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "logged", w.Body.String())
}
