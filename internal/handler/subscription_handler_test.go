// subscription_handler_test.go — 订阅下载 Handler 测试。
//
// 测试范围：
// - 有效 token 下载订阅
// - 无效 token 返回错误
// - 不同格式返回正确 Content-Type
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"suiyue/internal/handler"
	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/subscription"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSubHandlerTest 创建订阅下载测试环境。
func setupSubHandlerTest(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()

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
		&PlanNodeGroup{},
	))

	// 创建测试用户
	user := &model.User{
		UUID:         "test-uuid",
		Username:     "testuser",
		PasswordHash: "hash",
		XrayUserKey:  "testuser@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(user).Error)

	// 创建测试套餐
	plan := &model.Plan{
		Name:         "Test Plan",
		Price:        10.00,
		TrafficLimit: 1024 * 1024 * 1024,
		DurationDays: 30,
		IsActive:     true,
	}
	require.NoError(t, db.Create(plan).Error)

	// 创建测试订阅
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       plan.ID,
		StartDate:    time.Now().Add(-24 * time.Hour),
		ExpireDate:   time.Now().Add(24 * time.Hour),
		TrafficLimit: plan.TrafficLimit,
		UsedTraffic:  0,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	// 创建订阅 Token
	token := &model.SubscriptionToken{
		UserID:         user.ID,
		SubscriptionID: &sub.ID,
		Token:          "valid-token-123",
		IsRevoked:      false,
	}
	require.NoError(t, db.Create(token).Error)

	// 创建节点分组
	ng := &model.NodeGroup{Name: "Test Group"}
	require.NoError(t, db.Create(ng).Error)

	// 创建节点
	node := &model.Node{
		Name:         "Test Node",
		Host:         "test.example.com",
		Port:         443,
		ServerName:   "example.com",
		PublicKey:    "pubkey",
		ShortID:      "sid",
		AgentBaseURL: "http://localhost:8080",
		AgentToken:   "token",
		IsEnabled:    true,
		NodeGroupID:  &ng.ID,
	}
	require.NoError(t, db.Create(node).Error)

	// 创建套餐-节点分组关联
	png := PlanNodeGroup{
		PlanID:      plan.ID,
		NodeGroupID: ng.ID,
	}
	require.NoError(t, db.Create(&png).Error)

	// 创建生成器
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	userRepo := repository.NewUserRepository(db)

	gen := subscription.NewGenerator(subRepo, tokenRepo, planRepo, nodeRepo, userRepo)
	subHandler := handler.NewSubHandler(gen)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/sub/:token/:format", subHandler.Download)

	return r, db
}

// TestSubHandler_DownloadClash 测试 Clash 格式下载。
func TestSubHandler_DownloadClash(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/clash", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/yaml")
	assert.Contains(t, w.Body.String(), "proxies")
}

// TestSubHandler_DownloadBase64 测试 Base64 格式下载。
func TestSubHandler_DownloadBase64(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/base64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

// TestSubHandler_DownloadPlain 测试纯文本格式下载。
func TestSubHandler_DownloadPlain(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/plain", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, w.Body.String(), "vless://")
}

// TestSubHandler_InvalidToken 测试无效 Token。
func TestSubHandler_InvalidToken(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/invalid-token/clash", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSubHandler_InvalidFormat 测试无效格式。
func TestSubHandler_InvalidFormat(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/invalid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
