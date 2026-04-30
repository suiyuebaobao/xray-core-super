// Package middleware 提供 Gin 中间件。
//
// 包含：
// - JWT 鉴权中间件（Access Token 验证）
// - 管理员权限检查中间件
// - 请求日志中间件
// - CORS 中间件（仅开发环境）
package middleware

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"suiyue/internal/platform/auth"
	"suiyue/internal/platform/response"

	"github.com/gin-gonic/gin"
)

// contextKey 用于 gin.Context 存储的键。
type contextKey string

const (
	claimsKey contextKey = "claims"

	CSRFHeaderName  = "X-CSRF-Token"
	CSRFHeaderValue = "suiyue-web"
	RequestIDHeader = "X-Request-ID"
)

var requestIDCounter uint64

// JWTAuth 返回 JWT 鉴权中间件。
//
// 从请求 Header "Authorization: Bearer <token>" 中提取 Access Token，
// 解析后存入 gin.Context，供后续 handler 使用。
// 某些路径（如公开接口）可以通过 skipPaths 跳过验证。
func JWTAuth(secret string, skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]bool)
	for _, p := range skipPaths {
		skip[p] = true
	}

	return func(c *gin.Context) {
		// 跳过不需要鉴权的路径
		if skip[c.Request.URL.Path] {
			c.Next()
			return
		}

		// 提取 Authorization Header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.HandleError(c, response.ErrUnauthorized)
			c.Abort()
			return
		}

		// 提取 Bearer Token
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || parts[0] != "Bearer" {
			response.HandleError(c, response.ErrUnauthorized)
			c.Abort()
			return
		}

		token := parts[1]
		claims, err := auth.ParseClaims(token, secret)
		if err != nil {
			response.HandleError(c, response.ErrTokenInvalid)
			c.Abort()
			return
		}

		// 存储 Claims 到 Context
		c.Set(string(claimsKey), claims)
		c.Next()
	}
}

// RequireAdmin 返回管理员权限检查中间件。
//
// 必须在 JWTAuth 之后使用。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		val, exists := c.Get(string(claimsKey))
		if !exists {
			response.HandleError(c, response.ErrForbidden)
			c.Abort()
			return
		}

		claims, ok := val.(*auth.Claims)
		if !ok || !claims.IsAdmin {
			response.HandleError(c, response.ErrForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}

// GetClaims 从 gin.Context 获取当前用户的 Claims。
func GetClaims(c *gin.Context) (*auth.Claims, bool) {
	val, exists := c.Get(string(claimsKey))
	if !exists {
		return nil, false
	}
	claims, ok := val.(*auth.Claims)
	return claims, ok
}

// GetUserID 从 gin.Context 获取当前用户 ID。
func GetUserID(c *gin.Context) (uint64, bool) {
	claims, ok := GetClaims(c)
	if !ok {
		return 0, false
	}
	return claims.UserID, true
}

// RequestID 为每个请求注入请求 ID。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader(RequestIDHeader)
		if requestID == "" {
			requestID = generateRequestID()
		}
		c.Header(RequestIDHeader, requestID)
		c.Request.Header.Set(RequestIDHeader, requestID)
		c.Next()
	}
}

func generateRequestID() string {
	n := atomic.AddUint64(&requestIDCounter, 1)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatUint(n, 36)
}

// CSRF 为浏览器写操作校验自定义 Header。
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		if c.GetHeader(CSRFHeaderName) != CSRFHeaderValue {
			response.HandleError(c, response.ErrForbidden)
			c.Abort()
			return
		}

		c.Next()
	}
}

type rateBucket struct {
	count   int
	resetAt time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
	limit   int
	window  time.Duration
}

// RateLimit 返回固定窗口限流中间件，按客户端 IP 和路由模板计数。
func RateLimit(limit int, window time.Duration) gin.HandlerFunc {
	rl := &rateLimiter{
		buckets: make(map[string]rateBucket),
		limit:   limit,
		window:  window,
	}

	return func(c *gin.Context) {
		if !rl.allow(c.ClientIP(), c.FullPath()) {
			response.HandleError(c, response.ErrTooManyRequests)
			c.Abort()
			return
		}
		c.Next()
	}
}

func (r *rateLimiter) allow(ip, route string) bool {
	if r.limit <= 0 || r.window <= 0 {
		return true
	}

	now := time.Now()
	key := ip + "|" + route

	r.mu.Lock()
	defer r.mu.Unlock()

	for k, b := range r.buckets {
		if now.After(b.resetAt) {
			delete(r.buckets, k)
		}
	}

	b := r.buckets[key]
	if b.resetAt.IsZero() || now.After(b.resetAt) {
		r.buckets[key] = rateBucket{count: 1, resetAt: now.Add(r.window)}
		return true
	}

	if b.count >= r.limit {
		return false
	}
	b.count++
	r.buckets[key] = b
	return true
}

// Logger 请求日志中间件。
func Logger() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		requestID := param.Request.Header.Get(RequestIDHeader)
		return param.Request.URL.Path + " " + param.Method + " " +
			param.ClientIP + " " + requestID + " " + param.ErrorMessage + "\n"
	})
}

// CORS 开发环境跨域中间件。
//
// 生产环境由 Nginx 处理，此中间件仅在开发模式启用。
func CORS() gin.HandlerFunc {
	allowedOrigins := parseAllowedOrigins(os.Getenv("CORS_ALLOWED_ORIGINS"))
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowOrigin := allowedOrigin(origin, allowedOrigins)
		if allowOrigin != "" {
			c.Header("Access-Control-Allow-Origin", allowOrigin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, "+CSRFHeaderName)

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func parseAllowedOrigins(raw string) map[string]bool {
	origins := make(map[string]bool)
	if raw == "" {
		origins["*"] = true
		return origins
	}
	for _, origin := range strings.Split(raw, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = true
		}
	}
	if len(origins) == 0 {
		origins["*"] = true
	}
	return origins
}

func allowedOrigin(origin string, allowed map[string]bool) string {
	if allowed["*"] {
		return "*"
	}
	if origin != "" && allowed[origin] {
		return origin
	}
	return ""
}
