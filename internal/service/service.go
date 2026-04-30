// Package service 提供业务逻辑层。
//
// 职责：
// - 用户注册、登录、Token 刷新、登出
// - 订阅查询
// - 套餐查询
//
// 不直接操作数据库，通过 repository 层访问数据。
// 所有写操作在事务中完成。
// 错误处理统一返回 platform/response 中定义的 AppError。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"suiyue/internal/config"
	"suiyue/internal/model"
	"suiyue/internal/platform/auth"
	"suiyue/internal/platform/response"
	"suiyue/internal/platform/secure"
	"suiyue/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// AuthService 认证服务。
type AuthService struct {
	userRepo    *repository.UserRepository
	refreshRepo *repository.RefreshTokenRepository
	cfg         *config.Config
}

// NewAuthService 创建认证服务。
func NewAuthService(userRepo *repository.UserRepository, refreshRepo *repository.RefreshTokenRepository, cfg *config.Config) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		refreshRepo: refreshRepo,
		cfg:         cfg,
	}
}

// Register 注册用户。
//
// 流程：
// 1. 校验用户名不重复
// 2. 密码 bcrypt 哈希
// 3. 生成 UUID 和 xray_user_key
// 4. 创建用户
func (s *AuthService) Register(ctx context.Context, req *model.CreateUserRequest) (*model.UserPublic, error) {
	// 1. 检查用户名是否已存在
	_, err := s.userRepo.FindByUsername(ctx, req.Username)
	if err == nil {
		return nil, response.ErrUserExists
	}

	// 2. 密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), s.cfg.BCryptRounds)
	if err != nil {
		return nil, response.ErrInternalServer
	}

	uuid, err := secure.RandomUUID()
	if err != nil {
		return nil, response.ErrInternalServer
	}

	// 3. 构建用户模型
	user := &model.User{
		UUID:         uuid,
		Username:     req.Username,
		PasswordHash: string(hash),
		XrayUserKey:  req.Username + "@" + s.cfg.XrayUserKeyDomain,
		Status:       "active",
	}
	if req.Email != "" {
		user.Email = &req.Email
	}

	// 4. 创建用户并预生成用户级订阅 Token
	if err := s.userRepo.CreateWithSubscriptionToken(ctx, user); err != nil {
		return nil, response.ErrInternalServer
	}

	return user.ToPublic(), nil
}

// Login 用户登录。
//
// 流程：
// 1. 根据用户名查找用户
// 2. 校验密码
// 3. 检查用户状态
// 4. 生成 Access Token + Refresh Token
// 5. 保存 Refresh Token 记录
// 6. 更新最后登录信息
func (s *AuthService) Login(ctx context.Context, req *model.LoginRequest, clientIP string) (*model.LoginResponse, string, error) {
	// 1. 查找用户
	user, err := s.userRepo.FindByUsername(ctx, req.Username)
	if err != nil {
		return nil, "", response.ErrLoginFailed
	}

	// 2. 校验密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return nil, "", response.ErrLoginFailed
	}

	// 3. 检查状态
	if user.Status != "active" {
		return nil, "", &response.AppError{Code: 40005, HTTPCode: http.StatusForbidden, Message: "账号已被禁用"}
	}

	// 4. 生成 Token
	now := time.Now()
	accessToken, err := auth.GenerateToken(user.ID, user.Username, user.IsAdmin, s.cfg.JWTSecret, now.Add(s.cfg.JWTExpiresIn))
	if err != nil {
		return nil, "", &response.AppError{
			Code:     response.ErrInternalServer.Code,
			HTTPCode: response.ErrInternalServer.HTTPCode,
			Message:  response.ErrInternalServer.Message,
			Detail:   "generate access token failed: " + err.Error(),
		}
	}

	refreshToken, err := auth.GenerateRefreshToken(user.ID, s.cfg.JWTSecret, now.Add(s.cfg.JWTRefreshExpiresIn))
	if err != nil {
		return nil, "", &response.AppError{
			Code:     response.ErrInternalServer.Code,
			HTTPCode: response.ErrInternalServer.HTTPCode,
			Message:  response.ErrInternalServer.Message,
			Detail:   "generate refresh token failed: " + err.Error(),
		}
	}

	// 5. 保存 Refresh Token 哈希（用 SHA-256，因为 JWT Token 超过 bcrypt 的 72 字节限制）
	refreshHash := sha256.Sum256([]byte(refreshToken))
	if err := s.refreshRepo.Create(ctx, &model.RefreshToken{
		UserID:    user.ID,
		TokenHash: hex.EncodeToString(refreshHash[:]),
		ExpiresAt: now.Add(s.cfg.JWTRefreshExpiresIn),
	}); err != nil {
		log.Printf("[auth] save refresh token failed: %v", err)
	}

	// 6. 更新登录信息
	if err := s.userRepo.UpdateLoginInfo(ctx, user.ID, clientIP); err != nil {
		log.Printf("[auth] update login info failed: %v", err)
	}

	return &model.LoginResponse{
		AccessToken: accessToken,
		User:        user.ToPublic(),
	}, refreshToken, nil
}

// RefreshToken 刷新 Access Token（带轮转机制）。
//
// 流程：
// 1. 解析并验证旧 Refresh Token
// 2. 查找用户并检查状态
// 3. 验证服务端存在该 Refresh Token 记录（防重放）
// 4. 使旧 Refresh Token 失效（轮转）
// 5. 生成新的 Access Token + Refresh Token
// 6. 保存新 Refresh Token 哈希
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (newAccess, newRefresh string, err error) {
	// 1. 解析旧 Token
	claims, parseErr := auth.ParseRefreshClaims(refreshToken, s.cfg.JWTSecret)
	if parseErr != nil {
		return "", "", response.ErrTokenInvalid
	}

	// 2. 查找用户
	user, findErr := s.userRepo.FindByID(ctx, uint64(claims.UserID))
	if findErr != nil || user.Status != "active" {
		return "", "", response.ErrUnauthorized
	}

	// 3. 验证旧 Token 在服务端存在（防重放攻击）
	oldHash := sha256.Sum256([]byte(refreshToken))
	hashStr := hex.EncodeToString(oldHash[:])
	_, err = s.refreshRepo.FindByHash(ctx, hashStr)
	if err != nil {
		return "", "", response.ErrUnauthorized
	}

	// 4. 使旧 Token 失效（轮转）
	if err := s.refreshRepo.DeleteByHash(ctx, hashStr); err != nil {
		log.Printf("[auth] delete old refresh token failed: %v", err)
	}

	// 5. 生成新 Token
	now := time.Now()
	newAccess, err = auth.GenerateToken(user.ID, user.Username, user.IsAdmin, s.cfg.JWTSecret, now.Add(s.cfg.JWTExpiresIn))
	if err != nil {
		return "", "", response.ErrInternalServer
	}

	newRefresh, err = auth.GenerateRefreshToken(user.ID, s.cfg.JWTSecret, now.Add(s.cfg.JWTRefreshExpiresIn))
	if err != nil {
		return "", "", response.ErrInternalServer
	}

	// 6. 保存新 Refresh Token 哈希
	newRefreshHash := sha256.Sum256([]byte(newRefresh))
	if err := s.refreshRepo.Create(ctx, &model.RefreshToken{
		UserID:    user.ID,
		TokenHash: hex.EncodeToString(newRefreshHash[:]),
		ExpiresAt: now.Add(s.cfg.JWTRefreshExpiresIn),
	}); err != nil {
		log.Printf("[auth] save new refresh token failed: %v", err)
	}

	return newAccess, newRefresh, nil
}

// Logout 登出，从服务端清除 Refresh Token。
func (s *AuthService) Logout(ctx context.Context, refreshToken string) error {
	// 解析 Token 验证有效性
	_, err := auth.ParseRefreshClaims(refreshToken, s.cfg.JWTSecret)
	if err != nil {
		return nil // Token 无效，无需处理
	}

	// 计算哈希并删除
	refreshHash := sha256.Sum256([]byte(refreshToken))
	return s.refreshRepo.DeleteByHash(ctx, hex.EncodeToString(refreshHash[:]))
}

// UserService 用户服务。
type UserService struct {
	userRepo  *repository.UserRepository
	subRepo   *repository.SubscriptionRepository
	tokenRepo *repository.SubscriptionTokenRepository
	cfg       *config.Config
}

// NewUserService 创建用户服务。
func NewUserService(userRepo *repository.UserRepository, subRepo *repository.SubscriptionRepository, tokenRepo *repository.SubscriptionTokenRepository, cfg *config.Config) *UserService {
	return &UserService{
		userRepo:  userRepo,
		subRepo:   subRepo,
		tokenRepo: tokenRepo,
		cfg:       cfg,
	}
}

// GetMeInfo 获取当前用户信息（含订阅状态）。
func (s *UserService) GetMeInfo(ctx context.Context, userID uint64) (map[string]interface{}, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, response.ErrInternalServer
	}

	result := map[string]interface{}{
		"user": user.ToPublic(),
	}

	// 查询当前有效订阅
	sub, subErr := s.subRepo.FindActiveByUserID(ctx, userID)
	if subErr == nil && sub != nil {
		tokenData := map[string]interface{}{
			"subscription_id": sub.ID,
			"plan_id":         sub.PlanID,
			"status":          sub.Status,
			"expire_date":     sub.ExpireDate,
			"traffic_limit":   sub.TrafficLimit,
			"used_traffic":    sub.UsedTraffic,
			"tokens":          []string{},
		}
		result["subscription"] = tokenData
	}

	return result, nil
}

// PlanService 套餐服务。
type PlanService struct {
	planRepo *repository.PlanRepository
}

// NewPlanService 创建套餐服务。
func NewPlanService(planRepo *repository.PlanRepository) *PlanService {
	return &PlanService{planRepo: planRepo}
}

// ListActive 列出所有上架套餐。
func (s *PlanService) ListActive(ctx context.Context) ([]model.Plan, error) {
	return s.planRepo.ListActive(ctx)
}

// UpdateProfile 更新用户资料（邮箱、密码）。
func (s *UserService) UpdateProfile(ctx context.Context, userID uint64, email *string) (*model.UserPublic, error) {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return nil, response.ErrInternalServer
	}

	if email != nil {
		user.Email = email
	}

	// 通过 Save 更新
	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, response.ErrInternalServer
	}

	return user.ToPublic(), nil
}

// ChangePassword 修改密码。
func (s *UserService) ChangePassword(ctx context.Context, userID uint64, oldPassword, newPassword string) error {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return response.ErrInternalServer
	}

	// 校验旧密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return &response.AppError{Code: 40010, HTTPCode: 400, Message: "原密码不正确"}
	}

	// 生成新密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), s.cfg.BCryptRounds)
	if err != nil {
		return response.ErrInternalServer
	}

	return s.userRepo.UpdatePassword(ctx, userID, string(hash))
}
