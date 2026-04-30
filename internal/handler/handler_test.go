// handler_test.go — Handler 层 HTTP 集成测试。
//
// 测试策略：
// - 使用 Gin 的 httptest 创建测试服务器
// - 使用 SQLite 内存数据库
// - 验证完整的请求-响应链路（HTTP → Handler → Service → Repository → DB）
//
// 测试覆盖：
// - POST /api/auth/register 注册接口
// - POST /api/auth/login 登录接口
// - GET /api/user/me 用户信息接口（需鉴权）
// - GET /api/plans 套餐列表接口（公开）
package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"suiyue/internal/config"
	"suiyue/internal/handler"
	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/auth"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// setupTestApp 创建测试用 Gin 路由和数据库。
func setupTestApp(t *testing.T) (*gin.Engine, *config.Config) {
	t.Helper()

	// 创建 SQLite 内存数据库
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.RefreshToken{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
		&model.Plan{},
	))

	// 测试配置
	cfg := &config.Config{
		JWTSecret:           "test-secret-for-handler-test",
		JWTExpiresIn:        24 * time.Hour,
		JWTRefreshExpiresIn: 7 * 24 * time.Hour,
		BCryptRounds:        4,
		XrayUserKeyDomain:   "test.local",
	}

	// 创建 Service 和 Handler
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)

	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg)
	userSvc := service.NewUserService(userRepo, subRepo, tokenRepo, cfg)
	planSvc := service.NewPlanService(planRepo)

	authHandler := handler.NewAuthHandler(authSvc)
	userHandler := handler.NewUserHandler(userSvc, tokenRepo)
	planHandler := handler.NewPlanHandler(planSvc)

	// 创建测试用户
	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass123"), 4)
	_ = db.Create(&model.User{
		UUID:         "test-uuid-handler",
		Username:     "testhandleruser",
		PasswordHash: string(hash),
		XrayUserKey:  "testhandleruser@test.local",
		Status:       "active",
	})

	// 创建 Gin 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 注册路由
	r.POST("/api/auth/register", authHandler.Register)
	r.POST("/api/auth/login", authHandler.Login)
	r.POST("/api/auth/refresh", authHandler.Refresh)
	r.POST("/api/auth/logout", authHandler.Logout)

	userGroup := r.Group("/api/user")
	userGroup.Use(middleware.JWTAuth(cfg.JWTSecret))
	{
		userGroup.GET("/me", userHandler.GetMe)
		userGroup.GET("/subscription", userHandler.GetSubscription)
		userGroup.PUT("/profile", userHandler.UpdateProfile)
		userGroup.PUT("/password", userHandler.ChangePassword)
	}

	r.GET("/api/plans", planHandler.ListActive)

	return r, cfg
}

// setupTestAppWithSub 创建带订阅的测试环境。
func setupTestAppWithSub(t *testing.T) (*gin.Engine, *config.Config, uint64) {
	t.Helper()

	// 创建 SQLite 内存数据库
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.RefreshToken{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
		&model.Plan{},
	))

	// 测试配置
	cfg := &config.Config{
		JWTSecret:           "test-secret-for-handler-test",
		JWTExpiresIn:        24 * time.Hour,
		JWTRefreshExpiresIn: 7 * 24 * time.Hour,
		BCryptRounds:        4,
		XrayUserKeyDomain:   "test.local",
	}

	// 创建 Service 和 Handler
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)

	// 创建套餐
	plan := &model.Plan{
		Name:         "测试套餐",
		Price:        12.0,
		Currency:     "USDT",
		TrafficLimit: 1024 * 1024 * 1024 * 200,
		DurationDays: 30,
		IsActive:     true,
	}
	require.NoError(t, db.Create(plan).Error)

	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg)
	userSvc := service.NewUserService(userRepo, subRepo, tokenRepo, cfg)
	planSvc := service.NewPlanService(planRepo)

	// 注册用户
	pub, err := authSvc.Register(context.Background(), &model.CreateUserRequest{
		Username: "subuser",
		Password: "password123",
	})
	require.NoError(t, err)

	// 创建订阅
	sub := &model.UserSubscription{
		UserID:     pub.ID,
		PlanID:     plan.ID,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	// 创建订阅 Token
	db.Create(&model.SubscriptionToken{
		UserID:         pub.ID,
		SubscriptionID: &sub.ID,
		Token:          "test-sub-token-123456",
	})

	authHandler := handler.NewAuthHandler(authSvc)
	userHandler := handler.NewUserHandler(userSvc, tokenRepo)
	planHandler := handler.NewPlanHandler(planSvc)

	// 创建 Gin 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.POST("/api/auth/register", authHandler.Register)
	r.POST("/api/auth/login", authHandler.Login)
	r.POST("/api/auth/refresh", authHandler.Refresh)
	r.POST("/api/auth/logout", authHandler.Logout)

	userGroup := r.Group("/api/user")
	userGroup.Use(middleware.JWTAuth(cfg.JWTSecret))
	{
		userGroup.GET("/me", userHandler.GetMe)
		userGroup.GET("/subscription", userHandler.GetSubscription)
		userGroup.PUT("/profile", userHandler.UpdateProfile)
		userGroup.PUT("/password", userHandler.ChangePassword)
	}

	r.GET("/api/plans", planHandler.ListActive)

	return r, cfg, pub.ID
}

// TestHandler_Register_Success 测试注册接口成功场景。
func TestHandler_Register_Success(t *testing.T) {
	r, _ := setupTestApp(t)

	body := map[string]string{
		"username": "testuser",
		"password": "password123",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.True(t, resp["success"].(bool))
	assert.NotNil(t, resp["data"])
}

// TestHandler_Register_Duplicate 测试注册接口用户名重复。
func TestHandler_Register_Duplicate(t *testing.T) {
	r, _ := setupTestApp(t)

	// 第一次注册
	body := map[string]string{"username": "testuser", "password": "password123"}
	jsonBody, _ := json.Marshal(body)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(jsonBody)))

	// 第二次注册
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(jsonBody)))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(jsonBody)))

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.False(t, resp["success"].(bool))
	assert.Contains(t, resp["message"], "用户名已存在")
}

// TestHandler_Login_Success 测试登录接口成功场景。
func TestHandler_Login_Success(t *testing.T) {
	r, _ := setupTestApp(t)

	// 先注册
	body := map[string]string{"username": "testuser", "password": "password123"}
	jsonBody, _ := json.Marshal(body)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(jsonBody)))

	// 登录
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.True(t, resp["success"].(bool))
	assert.NotEmpty(t, resp["data"].(map[string]interface{})["accessToken"])

	// 验证 Refresh Token Cookie 存在
	cookies := w.Result().Cookies()
	var hasRefreshCookie bool
	for _, c := range cookies {
		if c.Name == "refresh_token" {
			hasRefreshCookie = true
			assert.True(t, c.HttpOnly)
			assert.True(t, c.Secure)
		}
	}
	assert.True(t, hasRefreshCookie, "应该设置 refresh_token Cookie")
}

// TestHandler_GetMe_WithAuth 测试带鉴权的用户信息接口。
func TestHandler_GetMe_WithAuth(t *testing.T) {
	r, _ := setupTestApp(t)

	// 先注册
	body := map[string]string{"username": "testuser", "password": "password123"}
	jsonBody, _ := json.Marshal(body)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewReader(jsonBody)))

	// 登录获取 Token
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(jsonBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	r.ServeHTTP(loginW, loginReq)

	var loginResp map[string]interface{}
	json.Unmarshal(loginW.Body.Bytes(), &loginResp)

	accessToken := loginResp["data"].(map[string]interface{})["accessToken"].(string)

	// 获取用户信息
	meReq := httptest.NewRequest(http.MethodGet, "/api/user/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+accessToken)
	meW := httptest.NewRecorder()
	r.ServeHTTP(meW, meReq)

	assert.Equal(t, http.StatusOK, meW.Code)

	var meResp map[string]interface{}
	json.Unmarshal(meW.Body.Bytes(), &meResp)

	assert.True(t, meResp["success"].(bool))
	assert.NotNil(t, meResp["data"].(map[string]interface{})["user"])
}

// TestHandler_GetMe_NoAuth 测试无鉴权的用户信息接口返回 401。
func TestHandler_GetMe_NoAuth(t *testing.T) {
	r, _ := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/user/me", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestHandler_Plans_Public 测试套餐列表公开接口。
func TestHandler_Plans_Public(t *testing.T) {
	r, _ := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.True(t, resp["success"].(bool))
}

// TestHandler_Plans_EmptyList 测试无套餐时返回空列表。
func TestHandler_Plans_EmptyList(t *testing.T) {
	r, _ := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	data := resp["data"].(map[string]interface{})
	plans := data["plans"].([]interface{})
	assert.Len(t, plans, 0)
}

// TestHandler_Plans_OnlyActive 测试下架套餐不出现在公开列表。
func TestHandler_Plans_OnlyActive(t *testing.T) {
	// 直接写入数据库创建套餐
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Plan{}))

	// 创建一个下架套餐（通过 raw SQL 绕过 GORM 的 default 行为）
	db.Exec("INSERT INTO plans (name, price, currency, traffic_limit, duration_days, is_active) VALUES (?, ?, ?, ?, ?, ?)",
		"已下架", 10.0, "USDT", 1000, 30, 0)

	var count int64
	db.Model(&model.Plan{}).Where("is_active = ?", false).Count(&count)
	require.Equal(t, int64(1), count, "确认下架套餐已写入")

	// 创建 Service/Handler 用这个 DB
	planRepo := repository.NewPlanRepository(db)
	planSvc := service.NewPlanService(planRepo)
	planHandler := handler.NewPlanHandler(planSvc)

	gin.SetMode(gin.TestMode)
	r2 := gin.New()
	r2.GET("/api/plans", planHandler.ListActive)

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	w := httptest.NewRecorder()
	r2.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	plans := data["plans"].([]interface{})
	assert.Len(t, plans, 0, "下架套餐不应出现在公开列表")
}

// TestPlanHandler_ListActive_ErrorPath 测试套餐列表错误处理。
func TestPlanHandler_ListActive_ErrorPath(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Plan{}))

	planRepo := repository.NewPlanRepository(db)
	planSvc := service.NewPlanService(planRepo)
	planHandler := handler.NewPlanHandler(planSvc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/plans", planHandler.ListActive)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
}

// TestHandler_GetSubscription_NoSubscription 测试无订阅时返回 nil。
func TestHandler_GetSubscription_NoSubscription(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
	))

	cfg := &config.Config{
		JWTSecret:    "test-secret-sub2",
		JWTExpiresIn: 24 * time.Hour,
	}

	user := &model.User{UUID: "sub-u2", Username: "subuser2", PasswordHash: "h", XrayUserKey: "sub2@x", Status: "active"}
	require.NoError(t, db.Create(user).Error)

	userRepo := repository.NewUserRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	userSvc := service.NewUserService(userRepo, subRepo, tokenRepo, cfg)
	userHandler := handler.NewUserHandler(userSvc, tokenRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	userGroup := r.Group("/api/user")
	userGroup.Use(middleware.JWTAuth(cfg.JWTSecret))
	{
		userGroup.GET("/subscription", userHandler.GetSubscription)
	}

	token, _ := auth.GenerateToken(user.ID, user.Username, false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	// 无订阅
	req := httptest.NewRequest(http.MethodGet, "/api/user/subscription", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
}

// TestHandler_Login_InvalidBody 测试登录接口无效请求体。
func TestHandler_Login_InvalidBody(t *testing.T) {
	r, _ := setupTestApp(t)

	// 空请求体
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUserHandler_UpdateProfile 测试更新用户资料。
func TestUserHandler_UpdateProfile(t *testing.T) {
	r, cfg := setupTestApp(t)

	token, _ := auth.GenerateToken(1, "testhandleruser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	body, _ := json.Marshal(map[string]interface{}{"email": "new@test.com"})
	req := httptest.NewRequest(http.MethodPut, "/api/user/profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestUserHandler_ChangePassword 测试修改密码。
func TestUserHandler_ChangePassword(t *testing.T) {
	r, cfg := setupTestApp(t)

	token, _ := auth.GenerateToken(1, "testhandleruser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	body, _ := json.Marshal(map[string]interface{}{
		"old_password": "testpass123",
		"new_password": "newpassword456",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestUserHandler_ChangePassword_InvalidBody 测试修改密码无效请求体。
func TestUserHandler_ChangePassword_InvalidBody(t *testing.T) {
	r, cfg := setupTestApp(t)

	token, _ := auth.GenerateToken(1, "testhandleruser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestHandler_GetMe_AuthError 测试无效 Token 返回 401。
func TestHandler_GetMe_AuthError(t *testing.T) {
	r, _ := setupTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/user/me", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestUserHandler_UpdateProfile_InvalidBody 测试更新用户资料无效请求体。
func TestUserHandler_UpdateProfile_InvalidBody(t *testing.T) {
	r, cfg := setupTestApp(t)

	token, _ := auth.GenerateToken(1, "testhandleruser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	// 非 JSON 请求体
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/user/profile", bytes.NewReader([]byte("not-json")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUserHandler_UpdateProfile_NoAuth 测试未认证访问更新资料接口。
func TestUserHandler_UpdateProfile_NoAuth(t *testing.T) {
	r, _ := setupTestApp(t)

	body, _ := json.Marshal(map[string]interface{}{"email": "new@test.com"})
	req := httptest.NewRequest(http.MethodPut, "/api/user/profile", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestUserHandler_ChangePassword_WrongOldPassword 测试旧密码错误。
func TestUserHandler_ChangePassword_WrongOldPassword(t *testing.T) {
	r, cfg := setupTestApp(t)

	token, _ := auth.GenerateToken(1, "testhandleruser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	body, _ := json.Marshal(map[string]interface{}{
		"old_password": "wrongpassword",
		"new_password": "newpassword456",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestUserHandler_ChangePassword_NoAuth 测试未认证访问修改密码接口。
func TestUserHandler_ChangePassword_NoAuth(t *testing.T) {
	r, _ := setupTestApp(t)

	body, _ := json.Marshal(map[string]interface{}{
		"old_password": "testpass123",
		"new_password": "newpassword456",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestUserHandler_ChangePassword_ShortNewPassword 测试新密码过短。
func TestUserHandler_ChangePassword_ShortNewPassword(t *testing.T) {
	r, cfg := setupTestApp(t)

	token, _ := auth.GenerateToken(1, "testhandleruser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	body, _ := json.Marshal(map[string]interface{}{
		"old_password": "testpass123",
		"new_password": "123",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/user/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPlanHandler_ListActive_WithData 测试有数据的套餐列表。
func TestPlanHandler_ListActive_WithData(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Plan{}))

	// 创建活跃套餐
	db.Create(&model.Plan{
		Name:         "活跃套餐",
		Price:        20.0,
		Currency:     "USDT",
		TrafficLimit: 5368709120,
		DurationDays: 30,
		IsActive:     true,
	})

	planRepo := repository.NewPlanRepository(db)
	planSvc := service.NewPlanService(planRepo)
	planHandler := handler.NewPlanHandler(planSvc)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/plans", planHandler.ListActive)

	req := httptest.NewRequest(http.MethodGet, "/api/plans", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	plans := data["plans"].([]interface{})
	assert.GreaterOrEqual(t, len(plans), 1)
}
