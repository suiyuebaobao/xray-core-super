// order_handler_test.go — 订单 Handler 测试。
//
// 测试范围：
// - 用户订单列表（带鉴权）
// - 用户订单列表（无鉴权）
// - 管理后台订单列表
// - 创建订单（完整实现）
package handler_test

import (
	"bytes"
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupOrderTest 创建订单测试环境。
func setupOrderTest(t *testing.T) (*gin.Engine, *config.Config) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Order{},
		&model.Plan{},
	))

	cfg := &config.Config{
		JWTSecret:    "order-test-secret",
		JWTExpiresIn: 24 * time.Hour,
	}

	// 创建用户
	user := &model.User{
		UUID:         "order-user",
		Username:     "orderuser",
		PasswordHash: "hashed",
		XrayUserKey:  "orderuser@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(user).Error)

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

	// 创建订单
	expiredAt := time.Now().Add(30 * time.Minute)
	order := &model.Order{
		UserID:    user.ID,
		PlanID:    1,
		OrderNo:   "ORD-TEST-001",
		Amount:    10.0,
		Status:    "PENDING",
		ExpiredAt: &expiredAt,
	}
	require.NoError(t, db.Create(order).Error)

	orderRepo := repository.NewOrderRepository(db)
	planRepo := repository.NewPlanRepository(db)
	orderSvc := service.NewOrderService(orderRepo, planRepo)

	orderHandler := handler.NewOrderHandler(orderSvc)
	adminOrderHandler := handler.NewAdminOrderHandler(orderRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 用户侧路由
	userGroup := r.Group("/api/user")
	userGroup.Use(middleware.JWTAuth(cfg.JWTSecret))
	{
		userGroup.GET("/orders", orderHandler.List)
	}
	r.POST("/api/orders", middleware.JWTAuth(cfg.JWTSecret), orderHandler.Create)

	// 管理后台路由
	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.RequireAdmin())
	{
		adminGroup.GET("/orders", adminOrderHandler.List)
	}

	return r, cfg
}


// TestOrderHandler_List_NoAuth 测试用户订单列表无鉴权。
func TestOrderHandler_List_NoAuth(t *testing.T) {
	r, _ := setupOrderTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestOrderHandler_List_WithAuth 测试用户订单列表有鉴权。
func TestOrderHandler_List_WithAuth(t *testing.T) {
	r, cfg := setupOrderTest(t)

	token, _ := auth.GenerateToken(1, "orderuser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	assert.NotNil(t, data["orders"])
	assert.Equal(t, float64(1), data["total"])
}

// TestOrderHandler_List_WithPagination 测试用户订单列表分页。
func TestOrderHandler_List_WithPagination(t *testing.T) {
	r, cfg := setupOrderTest(t)

	token, _ := auth.GenerateToken(1, "orderuser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders?page=1&size=10", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["page"])
	assert.Equal(t, float64(10), data["size"])
}

// TestOrderHandler_Create_InvalidPlanID 测试创建订单使用不存在的套餐 ID。
func TestOrderHandler_Create_InvalidPlanID(t *testing.T) {
	r, cfg := setupOrderTest(t)

	token, _ := auth.GenerateToken(1, "orderuser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	body, _ := json.Marshal(map[string]interface{}{"plan_id": 99999})
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 不存在的套餐应返回错误
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminOrderHandler_List_WithPagination 测试管理后台订单列表分页。
func TestAdminOrderHandler_List_WithPagination(t *testing.T) {
	r, cfg := setupOrderTest(t)

	adminToken, _ := generateAdminToken(cfg, 1, "orderuser", true)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/orders?page=1&size=10", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["page"])
	assert.Equal(t, float64(10), data["size"])
}

// TestOrderHandler_Create 测试创建订单。
func TestOrderHandler_Create(t *testing.T) {
	r, cfg := setupOrderTest(t)

	token, _ := auth.GenerateToken(1, "orderuser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))
	body, _ := json.Marshal(map[string]interface{}{"plan_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
	assert.NotNil(t, resp["data"].(map[string]interface{})["order"])
}

// TestOrderHandler_Create_InvalidBody 测试创建订单无效请求体。
func TestOrderHandler_Create_InvalidBody(t *testing.T) {
	r, cfg := setupOrderTest(t)

	token, _ := auth.GenerateToken(1, "orderuser", false, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))

	// 缺少 plan_id
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// 有效 plan_id
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader([]byte(`{"plan_id":1}`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestOrderHandler_Create_NoAuth 测试创建订单无鉴权。
func TestOrderHandler_Create_NoAuth(t *testing.T) {
	r, _ := setupOrderTest(t)

	body, _ := json.Marshal(map[string]interface{}{"plan_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/api/orders", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAdminOrderHandler_List 测试管理后台订单列表。
func TestAdminOrderHandler_List(t *testing.T) {
	r, cfg := setupOrderTest(t)

	adminToken, _ := generateAdminToken(cfg, 1, "orderuser", true)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/orders", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAdminOrderHandler_List_NoAuth 测试管理后台订单列表无鉴权。
func TestAdminOrderHandler_List_NoAuth(t *testing.T) {
	r, _ := setupOrderTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/orders", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAdminOrderHandler_List_NonAdmin 测试非管理员访问管理后台订单列表。
func TestAdminOrderHandler_List_NonAdmin(t *testing.T) {
	r, cfg := setupOrderTest(t)

	userToken, _ := generateAdminToken(cfg, 1, "orderuser", false)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/orders", nil)
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}






// TestOrderHandler_List_NoAuth_Unauthenticated 测试用户订单列表无鉴权返回 401。
func TestOrderHandler_List_NoAuth_Unauthenticated(t *testing.T) {
	r, _ := setupOrderTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
