package handler

import (
	"strings"

	"suiyue/internal/middleware"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

func buildClientLogContext(c *gin.Context) service.ClientLogContext {
	var userID *uint64
	var username *string
	if claims, ok := middleware.GetClaims(c); ok {
		userID = &claims.UserID
		name := claims.Username
		username = &name
	}

	clientIP := strings.TrimSpace(c.ClientIP())
	forwardedFor := strings.TrimSpace(c.GetHeader("X-Forwarded-For"))
	realIP := strings.TrimSpace(c.GetHeader("X-Real-IP"))
	userAgent := strings.TrimSpace(c.Request.UserAgent())

	var clientIPPtr, forwardedForPtr, realIPPtr, userAgentPtr *string
	if clientIP != "" {
		clientIPPtr = &clientIP
	}
	if forwardedFor != "" {
		forwardedForPtr = &forwardedFor
	}
	if realIP != "" {
		realIPPtr = &realIP
	}
	if userAgent != "" {
		userAgentPtr = &userAgent
	}

	return service.ClientLogContext{
		UserID:       userID,
		Username:     username,
		ClientIP:     clientIPPtr,
		ForwardedFor: forwardedForPtr,
		RealIP:       realIPPtr,
		UserAgent:    userAgentPtr,
	}
}
