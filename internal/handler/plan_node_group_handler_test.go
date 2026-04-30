// plan_node_group_handler_test.go — 套餐-节点分组绑定 Handler 测试。
package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"suiyue/internal/config"
	"suiyue/internal/handler"
	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/auth"
	"suiyue/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPlanNodeGroupApp 创建套餐-节点分组绑定的测试路由。
func setupPlanNodeGroupApp(t *testing.T) (*gin.Engine, string) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Plan{},
		&model.NodeGroup{},
	))
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	cfg := &config.Config{
		JWTSecret:   "test-secret-png",
		JWTExpiresIn: 24 * time.Hour,
	}

	adminUser := &model.User{
		UUID: "png-admin", Username: "pngadmin",
		PasswordHash: "h", XrayUserKey: "pngadmin@test.local",
		Status: "active", IsAdmin: true,
	}
	require.NoError(t, db.Create(adminUser).Error)

	planRepo := repository.NewPlanRepository(db)
	planHandler := handler.NewPlanNodeGroupHandler(planRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.RequireAdmin())
	{
		adminGroup.POST("/plans/:id/node-groups", planHandler.BindNodeGroups)
		adminGroup.GET("/plans/:id/node-groups", planHandler.ListNodeGroups)
	}

	token, _ := auth.GenerateToken(adminUser.ID, adminUser.Username, adminUser.IsAdmin, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))
	return r, token
}

// TestPlanNodeGroupHandler_BindNodeGroups_Success 测试绑定节点分组成功。
func TestPlanNodeGroupHandler_BindNodeGroups_Success(t *testing.T) {
	r, token := setupPlanNodeGroupApp(t)

	// 直接绑定到套餐 ID 1（已存在）
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/admin/plans/1/node-groups", strings.NewReader(`{"node_group_ids":[1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
}

// TestPlanNodeGroupHandler_BindNodeGroups_InvalidID 测试无效套餐 ID。
func TestPlanNodeGroupHandler_BindNodeGroups_InvalidID(t *testing.T) {
	r, token := setupPlanNodeGroupApp(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/admin/plans/abc/node-groups", strings.NewReader(`{"node_group_ids":[1]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPlanNodeGroupHandler_BindNodeGroups_MissingBody 测试缺少请求体。
func TestPlanNodeGroupHandler_BindNodeGroups_MissingBody(t *testing.T) {
	r, token := setupPlanNodeGroupApp(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/admin/plans/1/node-groups", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestPlanNodeGroupHandler_ListNodeGroups_Success 查询套餐关联的节点分组。
func TestPlanNodeGroupHandler_ListNodeGroups_Success(t *testing.T) {
	r, token := setupPlanNodeGroupApp(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/plans/1/node-groups", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp["success"].(bool))
}

// TestPlanNodeGroupHandler_ListNodeGroups_InvalidID 测试无效套餐 ID。
func TestPlanNodeGroupHandler_ListNodeGroups_InvalidID(t *testing.T) {
	r, token := setupPlanNodeGroupApp(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/admin/plans/abc/node-groups", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
