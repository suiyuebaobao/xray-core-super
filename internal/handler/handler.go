// Package handler 提供 HTTP 路由处理器。
//
// 职责：
// - 解析请求参数
// - 调用 service 层执行业务逻辑
// - 写入统一格式的响应
// - 不写业务逻辑，只做参数绑定和响应输出
package handler

import (
	"net/http"
	"strings"

	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

// AuthHandler 认证相关处理器。
type AuthHandler struct {
	authSvc         *service.AuthService
	operationLogSvc *service.OperationLogService
}

// NewAuthHandler 创建认证处理器。
func NewAuthHandler(authSvc *service.AuthService, operationLogSvc ...*service.OperationLogService) *AuthHandler {
	var logSvc *service.OperationLogService
	if len(operationLogSvc) > 0 {
		logSvc = operationLogSvc[0]
	}
	return &AuthHandler{authSvc: authSvc, operationLogSvc: logSvc}
}

// Register 处理 POST /api/auth/register。
func (h *AuthHandler) Register(c *gin.Context) {
	var req model.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	user, err := h.authSvc.Register(c.Request.Context(), &req)
	if err != nil {
		h.recordAuthOperation(c, "register", "failed", "用户注册失败", nil, map[string]interface{}{"username": req.Username})
		response.HandleError(c, err)
		return
	}

	h.recordAuthOperation(c, "register", "success", "用户注册成功", &user.ID, map[string]interface{}{"username": user.Username})
	response.Success(c, gin.H{"user": user})
}

// Login 处理 POST /api/auth/login。
func (h *AuthHandler) Login(c *gin.Context) {
	var req model.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	resp, refreshToken, err := h.authSvc.Login(c.Request.Context(), &req, c.ClientIP())
	if err != nil {
		h.recordAuthOperation(c, "login", "failed", "用户登录失败", nil, map[string]interface{}{"username": req.Username})
		response.HandleError(c, err)
		return
	}

	setRefreshTokenCookie(c, refreshToken, int(24*60*60*7))

	userID := resp.User.ID
	h.recordAuthOperation(c, "login", "success", "用户登录成功", &userID, map[string]interface{}{"username": resp.User.Username})
	response.Success(c, gin.H{
		"accessToken": resp.AccessToken,
		"user":        resp.User,
	})
}

// Refresh 处理 POST /api/auth/refresh。
// 从 Cookie 中读取 Refresh Token，生成新的 Access Token。
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil || refreshToken == "" {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	newAccess, newRefresh, err := h.authSvc.RefreshToken(c.Request.Context(), refreshToken)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	setRefreshTokenCookie(c, newRefresh, int(24*60*60*7))

	response.Success(c, gin.H{"accessToken": newAccess})
}

// Logout 处理 POST /api/auth/logout。
func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err == nil && refreshToken != "" {
		_ = h.authSvc.Logout(c.Request.Context(), refreshToken)
	}

	clearRefreshTokenCookie(c)

	h.recordAuthOperation(c, "logout", "success", "用户退出登录", nil, nil)
	response.Success(c, nil)
}

func setRefreshTokenCookie(c *gin.Context, value string, maxAge int) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("refresh_token", value, maxAge, "/", "", isSecureRequest(c), true)
}

func clearRefreshTokenCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("refresh_token", "", -1, "/", "", isSecureRequest(c), true)
}

func isSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	if proto := c.GetHeader("X-Forwarded-Proto"); proto != "" {
		return strings.EqualFold(proto, "https")
	}
	return true
}

func (h *AuthHandler) recordAuthOperation(c *gin.Context, action, result, summary string, targetID *uint64, extra interface{}) {
	if h == nil || h.operationLogSvc == nil {
		return
	}
	ctx := buildClientLogContext(c)
	targetType := "user"
	var targetTypePtr *string
	targetTypePtr = &targetType
	_ = h.operationLogSvc.Record(c.Request.Context(), ctx, "user", action, result, summary, targetTypePtr, targetID, extra)
}

// UserHandler 用户相关处理器。
type UserHandler struct {
	userSvc         *service.UserService
	tokenRepo       *repository.SubscriptionTokenRepository
	operationLogSvc *service.OperationLogService
}

// NewUserHandler 创建用户处理器。
func NewUserHandler(userSvc *service.UserService, tokenRepo *repository.SubscriptionTokenRepository, operationLogSvc ...*service.OperationLogService) *UserHandler {
	var logSvc *service.OperationLogService
	if len(operationLogSvc) > 0 {
		logSvc = operationLogSvc[0]
	}
	return &UserHandler{userSvc: userSvc, tokenRepo: tokenRepo, operationLogSvc: logSvc}
}

// GetMe 处理 GET /api/user/me。
// 需要 JWT 鉴权，从 Context 获取当前用户 ID。
func (h *UserHandler) GetMe(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	data, err := h.userSvc.GetMeInfo(c.Request.Context(), userID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Success(c, data)
}

// GetSubscription 处理 GET /api/user/subscription。
// 返回当前有效订阅详情和订阅 Token。
func (h *UserHandler) GetSubscription(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	data, err := h.userSvc.GetMeInfo(c.Request.Context(), userID)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	subData, ok := data["subscription"]
	if !ok {
		response.Success(c, gin.H{"subscription": nil})
		return
	}

	// 查询用户级有效 Token。Token 不再跟随订阅变动而变化。
	subMap := subData.(map[string]interface{})
	tokenStrings := []string{}
	token, err := h.tokenRepo.FindActiveByUserID(c.Request.Context(), userID)
	if err == nil {
		tokenStrings = append(tokenStrings, token.Token)
	}

	subMap["tokens"] = tokenStrings

	response.Success(c, gin.H{"subscription": subMap})
}

// UpdateProfile 处理 PUT /api/user/profile。
func (h *UserHandler) UpdateProfile(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	var req struct {
		Email *string `json:"email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	user, err := h.userSvc.UpdateProfile(c.Request.Context(), userID, req.Email)
	if err != nil {
		h.recordUserOperation(c, "update_profile", "failed", "用户修改资料失败", &userID, nil)
		response.HandleError(c, err)
		return
	}

	h.recordUserOperation(c, "update_profile", "success", "用户修改资料成功", &userID, nil)
	response.Success(c, gin.H{"user": user})
}

// ChangePassword 处理 PUT /api/user/password。
func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if err := h.userSvc.ChangePassword(c.Request.Context(), userID, req.OldPassword, req.NewPassword); err != nil {
		h.recordUserOperation(c, "change_password", "failed", "用户修改密码失败", &userID, nil)
		response.HandleError(c, err)
		return
	}

	h.recordUserOperation(c, "change_password", "success", "用户修改密码成功", &userID, nil)
	response.Success(c, nil)
}

func (h *UserHandler) recordUserOperation(c *gin.Context, action, result, summary string, targetID *uint64, extra interface{}) {
	if h == nil || h.operationLogSvc == nil {
		return
	}
	ctx := buildClientLogContext(c)
	targetType := "user"
	_ = h.operationLogSvc.Record(c.Request.Context(), ctx, "user", action, result, summary, &targetType, targetID, extra)
}

// PlanHandler 套餐相关处理器。
type PlanHandler struct {
	planSvc *service.PlanService
}

// NewPlanHandler 创建套餐处理器。
func NewPlanHandler(planSvc *service.PlanService) *PlanHandler {
	return &PlanHandler{planSvc: planSvc}
}

// ListActive 处理 GET /api/plans。
// 公开接口，不需要鉴权。
func (h *PlanHandler) ListActive(c *gin.Context) {
	plans, err := h.planSvc.ListActive(c.Request.Context())
	if err != nil {
		response.HandleError(c, err)
		return
	}

	response.Success(c, gin.H{"plans": plans})
}
