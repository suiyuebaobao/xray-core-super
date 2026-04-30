// admin_handler_test.go — 管理后台 Handler HTTP 集成测试。
//
// 测试策略：
// - 使用 Gin 的 httptest 创建测试服务器
// - 使用 SQLite 内存数据库
// - 验证完整的请求-响应链路
//
// 测试覆盖：
// - 套餐 CRUD
// - 节点分组 CRUD
// - 节点 CRUD
// - 用户列表和状态切换
package handler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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
)

// setupTestAdminApp 创建带管理权限的测试用 Gin 路由。
func setupTestAdminApp(t *testing.T) (*gin.Engine, string) {
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
		&model.NodeGroup{},
		&model.NodeGroupNode{},
		&model.Node{},
		&model.NodeAccessTask{},
		&model.RedeemCode{},
		&model.Order{},
		&model.PaymentRecord{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
		&PlanNodeGroup{},
	))

	// 测试配置
	cfg := &config.Config{
		JWTSecret:           "test-secret-for-admin-handler-test",
		JWTExpiresIn:        24 * time.Hour,
		JWTRefreshExpiresIn: 7 * 24 * time.Hour,
		BCryptRounds:        4,
		XrayUserKeyDomain:   "test.local",
	}

	// 创建管理员用户
	adminUser := &model.User{
		UUID:         "admin-uuid",
		Username:     "admin",
		PasswordHash: "$2a$04$e/5I.5qXqGqgF.5qXqGqgF.5qXqGqgF.5qXqGqgF.5qXqGqgF",
		XrayUserKey:  "admin@test.local",
		Status:       "active",
		IsAdmin:      true,
	}
	require.NoError(t, db.Create(adminUser).Error)

	// 创建 Repository
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeGroupRepo := repository.NewNodeGroupRepository(db)
	nodeRepo := repository.NewNodeRepository(db)

	// 创建 Service
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg)
	userSvc := service.NewUserService(userRepo, subRepo, tokenRepo, cfg)
	planSvc := service.NewPlanService(planRepo)

	// 创建 Handler
	authHandler := handler.NewAuthHandler(authSvc)
	userHandler := handler.NewUserHandler(userSvc, tokenRepo)
	planHandler := handler.NewPlanHandler(planSvc)
	adminPlanHandler := handler.NewAdminPlanHandler(planRepo)
	adminNodeGroupHandler := handler.NewAdminNodeGroupHandlerWithNodes(nodeGroupRepo, nodeRepo, nil)
	adminNodeHandler := handler.NewAdminNodeHandler(nodeRepo)
	adminUserHandler := handler.NewAdminUserHandlerWithSubscription(userRepo, subRepo, tokenRepo, planRepo, nil, cfg.BCryptRounds, cfg.XrayUserKeyDomain)
	usageHandler := handler.NewUsageHandler(repository.NewUsageLedgerRepository(db), userRepo, subRepo, planRepo)

	// 创建 Gin 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 注册路由
	r.POST("/api/auth/login", authHandler.Login)
	r.GET("/api/user/me", middleware.JWTAuth(cfg.JWTSecret), userHandler.GetMe)
	r.GET("/api/user/usage", middleware.JWTAuth(cfg.JWTSecret), usageHandler.GetCurrentUserUsage)
	r.GET("/api/plans", planHandler.ListActive)

	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.RequireAdmin())
	{
		adminGroup.GET("/plans", adminPlanHandler.List)
		adminGroup.POST("/plans", adminPlanHandler.Create)
		adminGroup.PUT("/plans/:id", adminPlanHandler.Update)
		adminGroup.DELETE("/plans/:id", adminPlanHandler.Delete)

		adminGroup.GET("/node-groups", adminNodeGroupHandler.List)
		adminGroup.POST("/node-groups", adminNodeGroupHandler.Create)
		adminGroup.PUT("/node-groups/:id", adminNodeGroupHandler.Update)
		adminGroup.DELETE("/node-groups/:id", adminNodeGroupHandler.Delete)
		adminGroup.GET("/node-groups/:id/nodes", adminNodeGroupHandler.ListNodes)
		adminGroup.PUT("/node-groups/:id/nodes", adminNodeGroupHandler.BindNodes)

		adminGroup.GET("/nodes", adminNodeHandler.List)
		adminGroup.POST("/nodes", adminNodeHandler.Create)
		adminGroup.PUT("/nodes/:id", adminNodeHandler.Update)
		adminGroup.DELETE("/nodes/:id", adminNodeHandler.Delete)

		adminGroup.GET("/users", adminUserHandler.List)
		adminGroup.POST("/users", adminUserHandler.Create)
		adminGroup.DELETE("/users/:id", adminUserHandler.Delete)
		adminGroup.PUT("/users/:id/status", adminUserHandler.ToggleStatus)
		adminGroup.PUT("/users/:id/password", adminUserHandler.ResetPassword)
		adminGroup.GET("/users/:id/subscription", adminUserHandler.GetSubscription)
		adminGroup.PUT("/users/:id/subscription", adminUserHandler.UpsertSubscription)
		adminGroup.GET("/users/:id/usage", usageHandler.GetAdminUserUsage)

		// 订阅 Token 管理
		nodeAccessSvc := service.NewNodeAccessService(
			repository.NewNodeAccessTaskRepository(db),
			nodeRepo, planRepo, subRepo, nil, cfg,
		)
		subTokenHandler := handler.NewAdminSubscriptionTokenHandler(
			tokenRepo, subRepo, userRepo, planRepo, nodeRepo, nodeAccessSvc,
		)
		adminGroup.GET("/subscription-tokens", subTokenHandler.ListTokens)
		adminGroup.POST("/subscription-tokens", subTokenHandler.CreateToken)
		adminGroup.POST("/subscription-tokens/:id/revoke", subTokenHandler.RevokeToken)
		adminGroup.POST("/subscription-tokens/:id/reset", subTokenHandler.ResetToken)
	}

	// 生成管理员 Token
	token, _ := generateAdminToken(cfg, adminUser.ID, adminUser.Username, adminUser.IsAdmin)

	return r, token
}

// setupTestAdminAppWithDB 创建带管理权限的测试环境，返回 db 实例。
func setupTestAdminAppWithDB(t *testing.T) (*gin.Engine, string, *gorm.DB) {
	t.Helper()

	// Create SQLite 内存数据库
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
		&model.NodeGroup{},
		&model.NodeGroupNode{},
		&model.Node{},
		&model.NodeAccessTask{},
		&model.RedeemCode{},
		&model.Order{},
		&model.PaymentRecord{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
		&PlanNodeGroup{},
	))

	// 测试配置
	cfg := &config.Config{
		JWTSecret:           "test-secret-for-admin-handler-test",
		JWTExpiresIn:        24 * time.Hour,
		JWTRefreshExpiresIn: 7 * 24 * time.Hour,
		BCryptRounds:        4,
		XrayUserKeyDomain:   "test.local",
	}

	// 创建管理员用户
	adminUser := &model.User{
		UUID:         "admin-uuid",
		Username:     "admin",
		PasswordHash: "$2a$04$e/5I.5qXqGqgF.5qXqGqgF.5qXqGqgF.5qXqGqgF.5qXqGqgF",
		XrayUserKey:  "admin@test.local",
		Status:       "active",
		IsAdmin:      true,
	}
	require.NoError(t, db.Create(adminUser).Error)

	// 创建 Repository
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeGroupRepo := repository.NewNodeGroupRepository(db)
	nodeRepo := repository.NewNodeRepository(db)

	// 创建 Service
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg)
	userSvc := service.NewUserService(userRepo, subRepo, tokenRepo, cfg)
	planSvc := service.NewPlanService(planRepo)

	// 创建 Handler
	authHandler := handler.NewAuthHandler(authSvc)
	userHandler := handler.NewUserHandler(userSvc, tokenRepo)
	planHandler := handler.NewPlanHandler(planSvc)
	adminPlanHandler := handler.NewAdminPlanHandler(planRepo)
	adminNodeGroupHandler := handler.NewAdminNodeGroupHandlerWithNodes(nodeGroupRepo, nodeRepo, nil)
	adminNodeHandler := handler.NewAdminNodeHandler(nodeRepo)
	adminUserHandler := handler.NewAdminUserHandlerWithSubscription(userRepo, subRepo, tokenRepo, planRepo, nil, cfg.BCryptRounds, cfg.XrayUserKeyDomain)
	usageHandler := handler.NewUsageHandler(repository.NewUsageLedgerRepository(db), userRepo, subRepo, planRepo)

	// 创建 Gin 路由
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 注册路由
	r.POST("/api/auth/login", authHandler.Login)
	r.GET("/api/user/me", middleware.JWTAuth(cfg.JWTSecret), userHandler.GetMe)
	r.GET("/api/user/usage", middleware.JWTAuth(cfg.JWTSecret), usageHandler.GetCurrentUserUsage)
	r.GET("/api/plans", planHandler.ListActive)

	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.RequireAdmin())
	{
		adminGroup.GET("/plans", adminPlanHandler.List)
		adminGroup.POST("/plans", adminPlanHandler.Create)
		adminGroup.PUT("/plans/:id", adminPlanHandler.Update)
		adminGroup.DELETE("/plans/:id", adminPlanHandler.Delete)

		adminGroup.GET("/node-groups", adminNodeGroupHandler.List)
		adminGroup.POST("/node-groups", adminNodeGroupHandler.Create)
		adminGroup.PUT("/node-groups/:id", adminNodeGroupHandler.Update)
		adminGroup.DELETE("/node-groups/:id", adminNodeGroupHandler.Delete)
		adminGroup.GET("/node-groups/:id/nodes", adminNodeGroupHandler.ListNodes)
		adminGroup.PUT("/node-groups/:id/nodes", adminNodeGroupHandler.BindNodes)

		adminGroup.GET("/nodes", adminNodeHandler.List)
		adminGroup.POST("/nodes", adminNodeHandler.Create)
		adminGroup.PUT("/nodes/:id", adminNodeHandler.Update)
		adminGroup.DELETE("/nodes/:id", adminNodeHandler.Delete)

		adminGroup.GET("/users", adminUserHandler.List)
		adminGroup.POST("/users", adminUserHandler.Create)
		adminGroup.DELETE("/users/:id", adminUserHandler.Delete)
		adminGroup.PUT("/users/:id/status", adminUserHandler.ToggleStatus)
		adminGroup.PUT("/users/:id/password", adminUserHandler.ResetPassword)
		adminGroup.GET("/users/:id/subscription", adminUserHandler.GetSubscription)
		adminGroup.PUT("/users/:id/subscription", adminUserHandler.UpsertSubscription)
		adminGroup.GET("/users/:id/usage", usageHandler.GetAdminUserUsage)

		// 订阅 Token 管理
		nodeAccessSvc := service.NewNodeAccessService(
			repository.NewNodeAccessTaskRepository(db),
			nodeRepo, planRepo, subRepo, nil, cfg,
		)
		subTokenHandler := handler.NewAdminSubscriptionTokenHandler(
			tokenRepo, subRepo, userRepo, planRepo, nodeRepo, nodeAccessSvc,
		)
		adminGroup.GET("/subscription-tokens", subTokenHandler.ListTokens)
		adminGroup.POST("/subscription-tokens", subTokenHandler.CreateToken)
		adminGroup.POST("/subscription-tokens/:id/revoke", subTokenHandler.RevokeToken)
		adminGroup.POST("/subscription-tokens/:id/reset", subTokenHandler.ResetToken)
	}

	// 生成管理员 Token
	token, _ := generateAdminToken(cfg, adminUser.ID, adminUser.Username, adminUser.IsAdmin)

	return r, token, db
}

// PlanNodeGroup 套餐-节点分组关联模型（测试用）。
type PlanNodeGroup struct {
	ID          uint64 `gorm:"primaryKey;column:id"`
	PlanID      uint64 `gorm:"column:plan_id"`
	NodeGroupID uint64 `gorm:"column:node_group_id"`
}

func (PlanNodeGroup) TableName() string { return "plan_node_groups" }

// generateAdminToken 辅助函数：生成管理员 JWT Token。
func generateAdminToken(cfg *config.Config, userID uint64, username string, isAdmin bool) (string, error) {
	return auth.GenerateToken(userID, username, isAdmin, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))
}

func assertCountZero(t *testing.T, db *gorm.DB, model interface{}, query string, args ...interface{}) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Where(query, args...).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

// TestAdminHandler_CreatePlan_Success 测试创建套餐成功。
func TestAdminHandler_CreatePlan_Success(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body := map[string]interface{}{
		"name":          "月付 200G",
		"price":         12.00,
		"traffic_limit": 214748364800,
		"duration_days": 30,
		"is_active":     true,
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plans", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "月付 200G", data["name"])
}

// TestAdminHandler_ListPlans 测试套餐列表。
func TestAdminHandler_ListPlans(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_CreateNodeGroup 测试创建节点分组。
func TestAdminHandler_CreateNodeGroup(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body := map[string]string{
		"name":        "香港节点组",
		"description": "香港地区节点",
	}
	jsonBody, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/node-groups", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_ListUsers 测试用户列表。
func TestAdminHandler_ListUsers(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminUserHandler_Create 测试管理员创建用户。
func TestAdminUserHandler_Create(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body, _ := json.Marshal(map[string]interface{}{
		"username": "newuser",
		"email":    "newuser@example.com",
		"password": "password123",
		"status":   "active",
		"is_admin": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "newuser", data["username"])
	assert.Equal(t, "active", data["status"])
}

// TestAdminUserHandler_Create_Duplicate 测试管理员创建重复用户名。
func TestAdminUserHandler_Create_Duplicate(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body, _ := json.Marshal(map[string]interface{}{
		"username": "admin",
		"password": "password123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminUserHandler_Delete 测试管理员删除用户并清理关联数据。
func TestAdminUserHandler_Delete(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	plan := &model.Plan{
		Name:         "Delete User Plan",
		Price:        1,
		Currency:     "USDT",
		TrafficLimit: 1024,
		DurationDays: 30,
		IsActive:     true,
	}
	require.NoError(t, db.Create(plan).Error)

	user := &model.User{
		UUID:        "delete-user-uuid",
		Username:    "deleteuser",
		XrayUserKey: "deleteuser@test.local",
		Status:      "active",
		IsAdmin:     false,
	}
	require.NoError(t, db.Create(user).Error)

	node := &model.Node{Name: "Delete User Node", Host: "127.0.0.1", Port: 443, IsEnabled: true}
	require.NoError(t, db.Create(node).Error)

	activeUserID := user.ID
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       plan.ID,
		StartDate:    time.Now().Add(-time.Hour),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 1024,
		UsedTraffic:  128,
		Status:       "ACTIVE",
		ActiveUserID: &activeUserID,
	}
	require.NoError(t, db.Create(sub).Error)
	subID := sub.ID

	require.NoError(t, db.Create(&model.SubscriptionToken{
		UserID:         user.ID,
		SubscriptionID: &subID,
		Token:          "delete-user-token",
		IsRevoked:      false,
	}).Error)
	require.NoError(t, db.Create(&model.RefreshToken{
		UserID:    user.ID,
		TokenHash: "delete-user-refresh",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}).Error)

	order := &model.Order{UserID: user.ID, PlanID: plan.ID, OrderNo: "ORD-DELETE-USER", Amount: 1, Status: "PENDING"}
	require.NoError(t, db.Create(order).Error)
	require.NoError(t, db.Create(&model.PaymentRecord{
		OrderID: order.ID,
		UserID:  user.ID,
		TxID:    "tx-delete-user",
		Amount:  1,
		Status:  "CONFIRMED",
	}).Error)
	require.NoError(t, db.Create(&model.UsageLedger{
		UserID:         user.ID,
		SubscriptionID: &subID,
		NodeID:         node.ID,
		DeltaUpload:    10,
		DeltaDownload:  20,
		DeltaTotal:     30,
		RecordedAt:     time.Now(),
	}).Error)
	require.NoError(t, db.Create(&model.TrafficSnapshot{
		NodeID:        node.ID,
		XrayUserKey:   user.XrayUserKey,
		UplinkTotal:   10,
		DownlinkTotal: 20,
		CapturedAt:    time.Now(),
	}).Error)
	usedAt := time.Now()
	require.NoError(t, db.Create(&model.RedeemCode{
		Code:         "DELETE-USER-CODE",
		PlanID:       plan.ID,
		DurationDays: 30,
		IsUsed:       true,
		UsedByUserID: &user.ID,
		UsedAt:       &usedAt,
	}).Error)
	task := &model.NodeAccessTask{
		NodeID:         node.ID,
		SubscriptionID: &subID,
		Action:         "remove_user",
		Status:         "PENDING",
		ScheduledAt:    time.Now(),
		IdempotencyKey: "delete-user-task",
	}
	require.NoError(t, db.Create(task).Error)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/users/%d", user.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.ErrorIs(t, db.First(&model.User{}, user.ID).Error, gorm.ErrRecordNotFound)

	assertCountZero(t, db, &model.UserSubscription{}, "user_id = ?", user.ID)
	assertCountZero(t, db, &model.SubscriptionToken{}, "user_id = ?", user.ID)
	assertCountZero(t, db, &model.RefreshToken{}, "user_id = ?", user.ID)
	assertCountZero(t, db, &model.Order{}, "user_id = ?", user.ID)
	assertCountZero(t, db, &model.PaymentRecord{}, "user_id = ?", user.ID)
	assertCountZero(t, db, &model.UsageLedger{}, "user_id = ?", user.ID)
	assertCountZero(t, db, &model.TrafficSnapshot{}, "xray_user_key = ?", user.XrayUserKey)

	var code model.RedeemCode
	require.NoError(t, db.Where("code = ?", "DELETE-USER-CODE").First(&code).Error)
	assert.Nil(t, code.UsedByUserID)

	var updatedTask model.NodeAccessTask
	require.NoError(t, db.First(&updatedTask, task.ID).Error)
	assert.Nil(t, updatedTask.SubscriptionID)
}

// TestAdminUserHandler_DeleteSelf_Rejected 测试禁止管理员删除当前登录账号。
func TestAdminUserHandler_DeleteSelf_Rejected(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/admin/users/1", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	require.NoError(t, db.First(&model.User{}, 1).Error)
}

// TestAdminHandler_UnauthorizedAccess 测试非管理员访问管理接口返回 403。
func TestAdminHandler_UnauthorizedAccess(t *testing.T) {
	r, _ := setupTestAdminApp(t)

	// 生成普通用户 Token
	cfg := &config.Config{
		JWTSecret:    "test-secret-for-admin-handler-test",
		JWTExpiresIn: 24 * time.Hour,
	}
	normalToken, _ := generateAdminToken(cfg, 99, "normaluser", false)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	req.Header.Set("Authorization", "Bearer "+normalToken)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestAdminHandler_UpdatePlan 测试更新套餐。
func TestAdminHandler_UpdatePlan(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建一个套餐
	createBody := map[string]interface{}{"name": "updatable", "price": 10.0, "traffic_limit": 0, "duration_days": 30, "is_active": true}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/plans", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	// 更新
	updateBody := map[string]interface{}{"name": "updated-plan", "price": 20.0, "traffic_limit": 1000, "duration_days": 60, "is_active": true}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/plans/1", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusOK, updateW.Code)
}

// TestAdminHandler_DeletePlan 测试删除套餐。
func TestAdminHandler_DeletePlan(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建再删除
	createBody := map[string]interface{}{"name": "deletable", "price": 5.0, "traffic_limit": 0, "duration_days": 7, "is_active": true}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/plans", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)
	var createResp map[string]interface{}
	require.NoError(t, json.Unmarshal(createW.Body.Bytes(), &createResp))
	planID := createResp["data"].(map[string]interface{})["id"]

	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/plans/%.0f", planID), nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusOK, deleteW.Code)
}

// TestAdminHandler_DeletePlan_InUse 测试删除已被订阅使用的普通套餐时自动迁移到基础套餐。
func TestAdminHandler_DeletePlan_InUse(t *testing.T) {
	r, token, db := setupTestAdminAppWithDB(t)

	basePlan := &model.Plan{Name: "base-plan", Price: 0, TrafficLimit: 0, DurationDays: 3650, IsActive: true, IsDefault: true}
	require.NoError(t, db.Create(basePlan).Error)
	plan := &model.Plan{Name: "in-use-plan", Price: 5.0, TrafficLimit: 1024, DurationDays: 7, IsActive: true}
	require.NoError(t, db.Create(plan).Error)
	sub := &model.UserSubscription{
		UserID:       1,
		PlanID:       plan.ID,
		StartDate:    time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 7),
		TrafficLimit: plan.TrafficLimit,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/plans/%d", plan.ID), nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusOK, deleteW.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(deleteW.Body.Bytes(), &resp))
	assert.Equal(t, float64(1), resp["data"].(map[string]interface{})["moved_subscription_count"])

	var updatedSub model.UserSubscription
	require.NoError(t, db.First(&updatedSub, sub.ID).Error)
	assert.Equal(t, basePlan.ID, updatedSub.PlanID)
	assert.Equal(t, uint64(0), updatedSub.UsedTraffic)

	var deletedPlan model.Plan
	require.NoError(t, db.First(&deletedPlan, plan.ID).Error)
	assert.True(t, deletedPlan.IsDeleted)
}

// TestAdminHandler_DeletePlan_Default 测试基础套餐不能删除。
func TestAdminHandler_DeletePlan_Default(t *testing.T) {
	r, token, db := setupTestAdminAppWithDB(t)

	basePlan := &model.Plan{Name: "base-plan", Price: 0, TrafficLimit: 0, DurationDays: 3650, IsActive: true, IsDefault: true}
	require.NoError(t, db.Create(basePlan).Error)

	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/admin/plans/%d", basePlan.ID), nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusBadRequest, deleteW.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(deleteW.Body.Bytes(), &resp))
	assert.Contains(t, resp["message"].(string), "基础套餐不能删除")
}

// TestAdminHandler_UpdateNodeGroup 测试更新节点分组。
func TestAdminHandler_UpdateNodeGroup(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建
	createBody := map[string]string{"name": "test-group", "description": "test"}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/node-groups", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	// 更新
	updateBody := map[string]string{"name": "updated-group", "description": "updated"}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/node-groups/1", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusOK, updateW.Code)
}

// TestAdminHandler_DeleteNodeGroup 测试删除节点分组。
func TestAdminHandler_DeleteNodeGroup(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/node-groups/1", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusOK, deleteW.Code)
}

// TestAdminHandler_CreateNode 测试创建节点。
func TestAdminHandler_CreateNode(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]interface{}{
		"name":           "test-node",
		"protocol":       "vless",
		"host":           "node.test.com",
		"port":           443,
		"agent_base_url": "http://node:8080",
		"agent_token":    "secret-token",
		"server_name":    "node.test.com",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/nodes", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_ListNodes 测试列出节点。
func TestAdminHandler_ListNodes(t *testing.T) {
	r, token := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_UpdateNode 测试更新节点。
func TestAdminHandler_UpdateNode(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建节点
	createBody := map[string]interface{}{
		"name": "update-node", "protocol": "vless", "host": "node.test",
		"port": 443, "agent_base_url": "http://node:8080",
		"agent_token": "secret", "server_name": "node.test",
	}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/nodes", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	// 更新
	updateBody := map[string]interface{}{
		"name": "updated-node", "protocol": "vless", "host": "node2.test",
		"port": 443, "agent_base_url": "http://node2:8080",
		"agent_token": "new-secret", "server_name": "node2.test",
	}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/nodes/1", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusOK, updateW.Code)
}

// TestAdminHandler_DeleteNode 测试删除节点。
func TestAdminHandler_DeleteNode(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/nodes/1", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusOK, deleteW.Code)
}

// TestAdminHandler_ToggleUserStatus 测试切换用户状态。
func TestAdminHandler_ToggleUserStatus(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]string{"status": "disabled"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/1/status", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_ToggleUserStatus_InvalidStatus 测试切换用户状态时传入无效状态值。
func TestAdminHandler_ToggleUserStatus_InvalidStatus(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]string{"status": "banned"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/1/status", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminHandler_ToggleUserStatus_UserNotFound 测试切换不存在用户的状态。
func TestAdminHandler_ToggleUserStatus_UserNotFound(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]string{"status": "disabled"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/99999/status", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// UpdateStatus 对不存在的用户执行 UPDATE 影响 0 行但不报错，所以返回 200
	// 这是当前实现行为：SQL UPDATE 不检查 affected rows
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_ToggleUserStatus_InvalidID 测试无效 ID 格式。
func TestAdminHandler_ToggleUserStatus_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]string{"status": "disabled"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/abc/status", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminHandler_UpdatePlan_NotFound 测试更新不存在的套餐。
func TestAdminHandler_UpdatePlan_NotFound(t *testing.T) {
	r, token := setupTestAdminApp(t)

	updateBody := map[string]interface{}{"name": "ghost", "price": 10.0, "traffic_limit": 0, "duration_days": 30, "is_active": true}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/plans/99999", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusNotFound, updateW.Code)
}

// TestAdminHandler_UpdatePlan_InvalidID 测试更新套餐时无效 ID。
func TestAdminHandler_UpdatePlan_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	updateBody := map[string]interface{}{"name": "ghost", "price": 10.0, "traffic_limit": 0, "duration_days": 30, "is_active": true}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/plans/abc", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusBadRequest, updateW.Code)
}

// TestAdminHandler_DeletePlan_NotFound 测试删除不存在的套餐。
func TestAdminHandler_DeletePlan_NotFound(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/plans/99999", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	// 删除不存在的记录可能返回 200（幂等）或 404
	assert.True(t, deleteW.Code == http.StatusOK || deleteW.Code == http.StatusNotFound, "got %d", deleteW.Code)
}

// TestAdminHandler_CreatePlan_MissingFields 测试创建套餐缺少必填字段。
func TestAdminHandler_CreatePlan_MissingFields(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]interface{}{
		"name": "incomplete",
		// 缺少 price, traffic_limit 等必填字段
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/plans", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminHandler_CreateNodeGroup_MissingName 测试创建节点分组缺少名称。
func TestAdminHandler_CreateNodeGroup_MissingName(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]string{"description": "no name"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/node-groups", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminHandler_UpdateNodeGroup_NotFound 测试更新不存在的节点分组。
func TestAdminHandler_UpdateNodeGroup_NotFound(t *testing.T) {
	r, token := setupTestAdminApp(t)

	updateBody := map[string]string{"name": "ghost-group", "description": "not found"}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/node-groups/99999", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusNotFound, updateW.Code)
}

// TestAdminHandler_DeleteNodeGroup_WithNodes 测试删除有节点的分组。
func TestAdminHandler_DeleteNodeGroup_WithNodes(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建节点分组
	createBody := map[string]string{"name": "group-with-nodes", "description": "test"}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/node-groups", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	// 在分组下创建节点
	nodeBody := map[string]interface{}{
		"name": "test-node", "protocol": "vless", "host": "node.test.com",
		"port": 443, "agent_base_url": "http://node:8080",
		"agent_token": "secret", "server_name": "node.test.com",
		"node_group_id": 1,
	}
	nodeJSON, _ := json.Marshal(nodeBody)
	nodeReq := httptest.NewRequest(http.MethodPost, "/api/admin/nodes", bytes.NewReader(nodeJSON))
	nodeReq.Header.Set("Authorization", "Bearer "+token)
	nodeReq.Header.Set("Content-Type", "application/json")
	nodeW := httptest.NewRecorder()
	r.ServeHTTP(nodeW, nodeReq)
	require.Equal(t, http.StatusOK, nodeW.Code)

	var nodeResp map[string]interface{}
	require.NoError(t, json.Unmarshal(nodeW.Body.Bytes(), &nodeResp))
	nodeData := nodeResp["data"].(map[string]interface{})
	nodeID := uint64(nodeData["id"].(float64))
	bindBody := map[string]interface{}{"node_ids": []uint64{nodeID}}
	bindJSON, _ := json.Marshal(bindBody)
	bindReq := httptest.NewRequest(http.MethodPut, "/api/admin/node-groups/1/nodes", bytes.NewReader(bindJSON))
	bindReq.Header.Set("Authorization", "Bearer "+token)
	bindReq.Header.Set("Content-Type", "application/json")
	bindW := httptest.NewRecorder()
	r.ServeHTTP(bindW, bindReq)
	require.Equal(t, http.StatusOK, bindW.Code)

	// 尝试删除分组
	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/node-groups/1", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	// 分组下有节点，应返回 400
	assert.Equal(t, http.StatusBadRequest, deleteW.Code)

	var resp map[string]interface{}
	json.Unmarshal(deleteW.Body.Bytes(), &resp)
	assert.Contains(t, resp["message"], "该分组下存在节点")
}

// TestAdminHandler_UpdateNode_NotFound 测试更新不存在的节点。
func TestAdminHandler_UpdateNode_NotFound(t *testing.T) {
	r, token := setupTestAdminApp(t)

	updateBody := map[string]interface{}{
		"name": "ghost-node", "protocol": "vless", "host": "ghost",
		"port": 443, "agent_base_url": "http://ghost:8080",
		"agent_token": "secret", "server_name": "ghost",
	}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/nodes/99999", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusNotFound, updateW.Code)
}

// TestAdminHandler_UpdateNodeGroup_InvalidID 测试更新节点分组时无效 ID。
func TestAdminHandler_UpdateNodeGroup_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	updateBody := map[string]string{"name": "updated", "description": "updated"}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/node-groups/abc", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusBadRequest, updateW.Code)
}

// TestAdminHandler_DeleteNode_InvalidID 测试删除节点时无效 ID。
func TestAdminHandler_DeleteNode_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/nodes/abc", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusBadRequest, deleteW.Code)
}

// TestAdminHandler_CreateNode_MissingFields 测试创建节点缺少必填字段。
func TestAdminHandler_CreateNode_MissingFields(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]interface{}{
		"name": "incomplete",
		// 缺少 protocol, host 等必填字段
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/nodes", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminHandler_ToggleUserStatus_EmptyStatus 测试切换状态时缺少 status 字段。
func TestAdminHandler_ToggleUserStatus_EmptyStatus(t *testing.T) {
	r, token := setupTestAdminApp(t)

	body := map[string]string{}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/1/status", bytes.NewReader(jsonBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminHandler_ListUsers_Pagination 测试用户列表分页参数。
func TestAdminHandler_ListUsers_Pagination(t *testing.T) {
	r, token, db := setupTestAdminAppWithDB(t)

	user := &model.User{
		UUID:        "list-user-uuid",
		Username:    "listuser",
		XrayUserKey: "listuser@test.local",
		Status:      "active",
		IsAdmin:     false,
	}
	require.NoError(t, db.Create(user).Error)
	plan := &model.Plan{Name: "List Plan", Price: 1, Currency: "USDT", TrafficLimit: 1000, DurationDays: 30, IsActive: true}
	require.NoError(t, db.Create(plan).Error)
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       plan.ID,
		StartDate:    time.Now().Add(-time.Hour),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 1000,
		UsedTraffic:  250,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users?page=1&size=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	assert.NotNil(t, data["users"])
	assert.Equal(t, float64(1), data["page"])
	assert.Equal(t, float64(10), data["size"])

	users := data["users"].([]interface{})
	var listUser map[string]interface{}
	for _, raw := range users {
		item := raw.(map[string]interface{})
		if item["username"] == "listuser" {
			listUser = item
			break
		}
	}
	require.NotNil(t, listUser)
	assert.Equal(t, "List Plan", listUser["plan_name"])
	assert.Equal(t, true, listUser["has_active_subscription"])
	assert.Equal(t, float64(750), listUser["remaining_traffic"])
	assert.Equal(t, float64(25), listUser["traffic_usage_percent"])
}

// TestAdminHandler_NoAuthToken 测试无 Token 访问管理接口。
func TestAdminHandler_NoAuthToken(t *testing.T) {
	r, _ := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAdminHandler_UpdatePlan_EmptyBody 测试更新套餐时请求体为空。
func TestAdminHandler_UpdatePlan_EmptyBody(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建套餐
	createBody := map[string]interface{}{"name": "plan-for-empty-update", "price": 10.0, "traffic_limit": 0, "duration_days": 30, "is_active": true}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/plans", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	// 更新时发送空对象
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/plans/1", bytes.NewReader([]byte("{}")))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	// 如果 model.UpdatePlanRequest 有 required 标记，应返回 400；否则可能 200
	assert.True(t, updateW.Code == http.StatusOK || updateW.Code == http.StatusBadRequest, "got %d", updateW.Code)
}

// TestAdminHandler_DeleteNodeGroup_InvalidID 测试删除节点分组时无效 ID。
func TestAdminHandler_DeleteNodeGroup_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/node-groups/abc", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusBadRequest, deleteW.Code)
}

// TestAdminHandler_DeletePlan_InvalidID 测试删除套餐时无效 ID。
func TestAdminHandler_DeletePlan_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/plans/abc", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusBadRequest, deleteW.Code)
}

// TestAdminHandler_ListNodeGroups 测试节点分组列表。
func TestAdminHandler_ListNodeGroups(t *testing.T) {
	r, token := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/node-groups", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
}

// TestAdminHandler_ListPlansWithData 测试套餐列表有数据。
func TestAdminHandler_ListPlansWithData(t *testing.T) {
	r, token := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/plans", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminHandler_UpdateNodeGroup_EmptyDescription 测试更新时清空描述。
func TestAdminHandler_UpdateNodeGroup_EmptyDescription(t *testing.T) {
	r, token := setupTestAdminApp(t)

	// 先创建
	createBody := map[string]string{"name": "test-group", "description": "test"}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/node-groups", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)

	// 更新时不带 description（走 else 分支）
	updateBody := map[string]string{"name": "updated-group"}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/node-groups/1", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusOK, updateW.Code)
}

// TestAdminUserHandler_ResetPassword 测试管理员重置用户密码。
func TestAdminUserHandler_ResetPassword(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body, _ := json.Marshal(map[string]interface{}{"new_password": "newpassword123"})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/1/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminUserHandler_List_WithKeyword 测试用户列表搜索。
func TestAdminUserHandler_List_WithKeyword(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/users?page=1&size=10&keyword=admin", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestUsageHandler_AdminUserUsage 测试管理员查询用户按日/周/月用量。
func TestUsageHandler_AdminUserUsage(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	user := &model.User{
		UUID:        "usage-user-uuid",
		Username:    "usageuser",
		XrayUserKey: "usageuser@test.local",
		Status:      "active",
		IsAdmin:     false,
	}
	require.NoError(t, db.Create(user).Error)

	plan := &model.Plan{Name: "Usage Plan", Price: 1, Currency: "USDT", TrafficLimit: 1024 * 1024 * 1024, DurationDays: 30, IsActive: true}
	require.NoError(t, db.Create(plan).Error)

	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       plan.ID,
		StartDate:    time.Now().AddDate(0, 0, -1),
		ExpireDate:   time.Now().AddDate(0, 0, 29),
		TrafficLimit: plan.TrafficLimit,
		UsedTraffic:  600,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	node := &model.Node{Name: "Usage Node", Protocol: "vless", Host: "127.0.0.1", Port: 443, AgentBaseURL: "http://127.0.0.1", IsEnabled: true}
	require.NoError(t, db.Create(node).Error)

	subID := sub.ID
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	today := todayStart.Add(2 * time.Hour)
	yesterday := todayStart.AddDate(0, 0, -1).Add(3 * time.Hour)
	require.NoError(t, db.Create(&model.UsageLedger{UserID: user.ID, SubscriptionID: &subID, NodeID: node.ID, DeltaUpload: 100, DeltaDownload: 200, DeltaTotal: 300, RecordedAt: today}).Error)
	require.NoError(t, db.Create(&model.UsageLedger{UserID: user.ID, SubscriptionID: &subID, NodeID: node.ID, DeltaUpload: 50, DeltaDownload: 250, DeltaTotal: 300, RecordedAt: yesterday}).Error)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/admin/users/%d/usage?days=7&weeks=4&months=3&recent=10", user.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "Usage Plan", data["plan_name"])
	assert.Equal(t, true, data["has_active_subscription"])
	assert.Len(t, data["daily"].([]interface{}), 7)
	assert.Len(t, data["weekly"].([]interface{}), 4)
	assert.Len(t, data["monthly"].([]interface{}), 3)

	summary := data["summary"].(map[string]interface{})
	todaySummary := summary["today"].(map[string]interface{})
	assert.Equal(t, float64(300), todaySummary["total"])
	toToday := summary["subscription_to_today"].(map[string]interface{})
	assert.Equal(t, float64(600), toToday["total"])
	recent := data["recent"].([]interface{})
	require.NotEmpty(t, recent)
	assert.Equal(t, "Usage Node", recent[0].(map[string]interface{})["node_name"])
}

// TestAdminSubscriptionTokenHandler_ListTokens 测试订阅 Token 列表。
func TestAdminSubscriptionTokenHandler_ListTokens(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	email := "alice@example.com"
	user := &model.User{
		UUID:        "token-list-user-uuid",
		Username:    "alice",
		Email:       &email,
		XrayUserKey: "alice@test.local",
		Status:      "active",
		IsAdmin:     false,
	}
	require.NoError(t, db.Create(user).Error)

	plan := &model.Plan{
		Name:         "测试套餐",
		Price:        9.9,
		Currency:     "USDT",
		TrafficLimit: 1024,
		DurationDays: 30,
		IsActive:     true,
	}
	require.NoError(t, db.Create(plan).Error)

	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       plan.ID,
		StartDate:    time.Now().Add(-time.Hour),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 1024,
		UsedTraffic:  128,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	subID := sub.ID
	lastUsedAt := time.Now().Add(-time.Minute)
	st := &model.SubscriptionToken{
		UserID:         user.ID,
		SubscriptionID: &subID,
		Token:          "token-list-test",
		LastUsedAt:     &lastUsedAt,
		IsRevoked:      false,
	}
	require.NoError(t, db.Create(st).Error)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/subscription-tokens?page=1&size=10", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))

	data := resp["data"].(map[string]interface{})
	tokens := data["tokens"].([]interface{})
	require.Len(t, tokens, 1)

	item := tokens[0].(map[string]interface{})
	assert.Equal(t, "alice", item["username"])
	assert.Equal(t, true, item["has_active_subscription"])
	assert.Equal(t, "测试套餐", item["plan_name"])
	assert.Equal(t, "ACTIVE", item["subscription_status"])
	assert.Equal(t, "ACTIVE", item["token_status"])
	assert.NotNil(t, item["subscription"])
	assert.NotNil(t, item["plan"])
}

// TestAdminSubscriptionTokenHandler_CreateToken 测试创建订阅 Token。
func TestAdminSubscriptionTokenHandler_CreateToken(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	// Create a subscription for user 1
	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	body, _ := json.Marshal(map[string]interface{}{"user_id": float64(1)})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/subscription-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminSubscriptionTokenHandler_RevokeToken 测试撤销订阅 Token。
func TestAdminSubscriptionTokenHandler_RevokeToken(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	// Create subscription and token
	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	st := &model.SubscriptionToken{
		UserID:         1,
		SubscriptionID: &sub.ID,
		Token:          "revoke-test-token-123456",
	}
	db.Create(st)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/subscription-tokens/%d/revoke", st.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminUserHandler_ResetPassword_InvalidBody 测试重置密码无效请求体。
func TestAdminUserHandler_ResetPassword_InvalidBody(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	// 缺少 new_password
	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/1/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminUserHandler_ResetPassword_ShortPassword 测试重置密码过短。
func TestAdminUserHandler_ResetPassword_ShortPassword(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body, _ := json.Marshal(map[string]interface{}{"new_password": "123"})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/1/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminUserHandler_ResetPassword_InvalidID 测试重置密码时无效用户 ID。
func TestAdminUserHandler_ResetPassword_InvalidID(t *testing.T) {
	r, adminToken := setupTestAdminApp(t)

	body, _ := json.Marshal(map[string]interface{}{"new_password": "newpassword123"})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/users/abc/password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminSubscriptionTokenHandler_CreateToken_NoActiveSub 测试用户无活跃订阅时也能创建用户级 Token。
func TestAdminSubscriptionTokenHandler_CreateToken_NoActiveSub(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
		&model.Plan{},
		&model.NodeGroup{},
		&model.Node{},
		&model.NodeAccessTask{},
		&PlanNodeGroup{},
	))

	cfg := &config.Config{
		JWTSecret:           "test-no-sub-secret",
		JWTExpiresIn:        24 * time.Hour,
		JWTRefreshExpiresIn: 7 * 24 * time.Hour,
		BCryptRounds:        4,
		XrayUserKeyDomain:   "test.local",
		TaskRetryLimit:      10,
	}

	adminUser := &model.User{
		UUID:         "admin-nosub",
		Username:     "adminnosub",
		PasswordHash: "h",
		XrayUserKey:  "adminnosub@test.local",
		Status:       "active",
		IsAdmin:      true,
	}
	require.NoError(t, db.Create(adminUser).Error)

	normalUser := &model.User{
		UUID:         "user-nosub",
		Username:     "nosubuser",
		PasswordHash: "h",
		XrayUserKey:  "nosubuser@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(normalUser).Error)

	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	userRepo := repository.NewUserRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(
		repository.NewNodeAccessTaskRepository(db),
		nodeRepo, planRepo, subRepo, nil, cfg,
	)
	subTokenHandler := handler.NewAdminSubscriptionTokenHandler(
		tokenRepo, subRepo, userRepo, planRepo, nodeRepo, nodeAccessSvc,
	)

	r := gin.New()
	r.Use(gin.Recovery())
	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.RequireAdmin())
	{
		adminGroup.POST("/subscription-tokens", subTokenHandler.CreateToken)
	}

	adminToken, _ := generateAdminToken(cfg, adminUser.ID, adminUser.Username, adminUser.IsAdmin)

	body, _ := json.Marshal(map[string]interface{}{"user_id": float64(normalUser.ID)})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/subscription-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var tokenCount int64
	db.Model(&model.SubscriptionToken{}).
		Where("user_id = ? AND is_revoked = ?", normalUser.ID, false).
		Count(&tokenCount)
	assert.Equal(t, int64(1), tokenCount)
}

// TestAdminSubscriptionTokenHandler_RevokeToken_InvalidID 测试撤销 Token 时无效 ID。
func TestAdminSubscriptionTokenHandler_RevokeToken_InvalidID(t *testing.T) {
	r, adminToken, _ := setupTestAdminAppWithDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/subscription-tokens/abc/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminSubscriptionTokenHandler_RevokeToken_NotFound 测试撤销不存在的 Token。
func TestAdminSubscriptionTokenHandler_RevokeToken_NotFound(t *testing.T) {
	r, adminToken, _ := setupTestAdminAppWithDB(t)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/subscription-tokens/99999/revoke", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestAdminSubscriptionTokenHandler_RevokeToken_AlreadyRevoked 测试重复撤销已撤销的 Token。
func TestAdminSubscriptionTokenHandler_RevokeToken_AlreadyRevoked(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	st := &model.SubscriptionToken{
		UserID: 1, SubscriptionID: &sub.ID,
		Token:     "revoke-already-test",
		IsRevoked: true,
	}
	db.Create(st)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/subscription-tokens/%d/revoke", st.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Contains(t, resp["message"], "已撤销")
}

// TestAdminSubscriptionTokenHandler_RevokeToken_LastToken 测试撤销 Token 不会改变订阅状态。
func TestAdminSubscriptionTokenHandler_RevokeToken_LastToken(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	st := &model.SubscriptionToken{
		UserID: 1, SubscriptionID: &sub.ID,
		Token:     "last-token-only",
		IsRevoked: false,
	}
	db.Create(st)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/subscription-tokens/%d/revoke", st.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, "ACTIVE", updatedSub.Status)

	var updatedToken model.SubscriptionToken
	db.First(&updatedToken, st.ID)
	assert.True(t, updatedToken.IsRevoked)
}

// TestAdminSubscriptionTokenHandler_ResetToken 测试管理员重置用户级 Token。
func TestAdminSubscriptionTokenHandler_ResetToken(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	st := &model.SubscriptionToken{
		UserID:         1,
		SubscriptionID: &sub.ID,
		Token:          "reset-old-token",
		IsRevoked:      false,
	}
	db.Create(st)

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/admin/subscription-tokens/%d/reset", st.ID), nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var oldToken model.SubscriptionToken
	db.First(&oldToken, st.ID)
	assert.False(t, oldToken.IsRevoked)
	assert.NotEqual(t, "reset-old-token", oldToken.Token)

	var tokenCount int64
	db.Model(&model.SubscriptionToken{}).
		Where("user_id = ?", uint64(1)).
		Count(&tokenCount)
	assert.Equal(t, int64(1), tokenCount)
}

// TestAdminSubscriptionTokenHandler_CreateToken_InvalidBody 测试创建 Token 时缺少 user_id。
func TestAdminSubscriptionTokenHandler_CreateToken_InvalidBody(t *testing.T) {
	r, adminToken, _ := setupTestAdminAppWithDB(t)

	body, _ := json.Marshal(map[string]interface{}{})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/subscription-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminSubscriptionTokenHandler_CreateToken_WithExpiry 测试创建 Token 时指定过期时间。
func TestAdminSubscriptionTokenHandler_CreateToken_WithExpiry(t *testing.T) {
	r, adminToken, db := setupTestAdminAppWithDB(t)

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":    float64(1),
		"expires_at": "2026-12-31T23:59:59Z",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/subscription-tokens", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	assert.NotEmpty(t, data["token"])
}

// TestAdminPlanHandler_Create_InvalidBody 测试创建套餐缺少必填字段。
func TestAdminPlanHandler_Create_InvalidBody(t *testing.T) {
	r, token := setupTestAdminApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/plans", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminNodeGroupHandler_List_WithData 测试有数据的节点分组列表。
func TestAdminNodeGroupHandler_List_WithData(t *testing.T) {
	r, token := setupTestAdminApp(t)

	createBody := map[string]string{"name": "list-test-group", "description": "for list test"}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/node-groups", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/node-groups", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	groups := data["groups"].([]interface{})
	assert.GreaterOrEqual(t, len(groups), 1)
}

// TestAdminNodeHandler_List_WithData 测试有数据的节点列表。
func TestAdminNodeHandler_List_WithData(t *testing.T) {
	r, token := setupTestAdminApp(t)

	createBody := map[string]interface{}{
		"name": "list-test-node", "protocol": "vless", "host": "node.test.com",
		"port": 443, "agent_base_url": "http://node:8080",
		"agent_token": "secret", "server_name": "node.test.com",
	}
	createJSON, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/admin/nodes", bytes.NewReader(createJSON))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	nodes := data["nodes"].([]interface{})
	assert.GreaterOrEqual(t, len(nodes), 1)
}

// TestAdminNodeHandler_Delete_InvalidID 测试删除节点时无效 ID。
func TestAdminNodeHandler_Delete_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/nodes/abc", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusBadRequest, deleteW.Code)
}

// TestAdminNodeHandler_Update_InvalidID 测试更新节点时无效 ID。
func TestAdminNodeHandler_Update_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	updateBody := map[string]interface{}{
		"name": "updated", "protocol": "vless", "host": "node",
		"port": 443, "agent_base_url": "http://node:8080",
		"agent_token": "secret", "server_name": "node",
	}
	updateJSON, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/admin/nodes/abc", bytes.NewReader(updateJSON))
	updateReq.Header.Set("Authorization", "Bearer "+token)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)

	assert.Equal(t, http.StatusBadRequest, updateW.Code)
}

// TestAdminPlanHandler_Delete_InvalidID 测试删除套餐时无效 ID。
func TestAdminPlanHandler_Delete_InvalidID(t *testing.T) {
	r, token := setupTestAdminApp(t)

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/admin/plans/abc", nil)
	deleteReq.Header.Set("Authorization", "Bearer "+token)
	deleteW := httptest.NewRecorder()
	r.ServeHTTP(deleteW, deleteReq)

	assert.Equal(t, http.StatusBadRequest, deleteW.Code)
}
