// redeem_handler_test.go — 兑换码处理器集成测试。
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
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupRedeemTest(t *testing.T) (*httptest.Server, *gorm.DB, *config.Config, func()) {
	t.Helper()
	db := openRedeemTestDB(t)

	// 自动建表
	db.AutoMigrate(&model.User{}, &model.UserSubscription{}, &model.SubscriptionToken{},
		&model.Plan{}, &model.NodeGroup{}, &model.Node{},
		&model.RedeemCode{}, &model.NodeAccessTask{})

	cfg := &config.Config{
		JWTSecret:           "test-jwt-secret",
		JWTExpiresIn:        24 * time.Hour,
		JWTRefreshExpiresIn: 7 * 24 * time.Hour,
		BCryptRounds:        4,
		TaskRetryLimit:      10,
	}

	cleanup := func() {}

	return httptest.NewServer(http.HandlerFunc(nil)), db, cfg, cleanup
}

func openRedeemTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:redeem_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sqlite db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return db
}

func generateTestJWTToken(cfg *config.Config, userID uint64, isAdmin bool) (string, error) {
	return auth.GenerateToken(userID, "testuser", isAdmin, cfg.JWTSecret, time.Now().Add(cfg.JWTExpiresIn))
}

func TestRedeem_Success_NewSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)

	db.AutoMigrate(&model.User{}, &model.UserSubscription{}, &model.SubscriptionToken{},
		&model.Plan{}, &model.NodeGroup{}, &model.Node{},
		&model.RedeemCode{}, &model.NodeAccessTask{})

	cfg := &config.Config{
		JWTSecret:      "test-jwt-secret",
		JWTExpiresIn:   24 * time.Hour,
		BCryptRounds:   4,
		TaskRetryLimit: 10,
	}

	// 创建用户
	user := &model.User{
		UUID:         "test-uuid-1",
		Username:     "testuser",
		PasswordHash: "hashed",
		XrayUserKey:  "test@suiyue.local",
		Status:       "active",
	}
	db.Create(user)

	// 创建套餐
	plan := &model.Plan{
		Name:         "测试套餐",
		Price:        10.00,
		Currency:     "USDT",
		TrafficLimit: 10737418240, // 10GB
		DurationDays: 30,
		IsActive:     true,
	}
	db.Create(plan)

	// 创建兑换码
	code := &model.RedeemCode{
		Code:         "REDEEMTEST001",
		PlanID:       plan.ID,
		DurationDays: 30,
		IsUsed:       false,
	}
	db.Create(code)

	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(
		repository.NewNodeAccessTaskRepository(db),
		repository.NewNodeRepository(db),
		planRepo,
		subRepo,
		nil,
		cfg,
	)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nodeAccessSvc)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	// 生成 JWT token
	token, err := generateTestJWTToken(cfg, user.ID, false)
	assert.NoError(t, err)

	body, _ := json.Marshal(gin.H{"code": "REDEEMTEST001"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.Equal(t, "success", resp["message"])

	// 验证 data 中有兑换成功消息
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "兑换成功", data["message"])

	// 验证订阅已创建
	var subCount int64
	db.Model(&model.UserSubscription{}).Where("user_id = ?", user.ID).Count(&subCount)
	assert.Equal(t, int64(1), subCount)

	// 验证兑换码已标记为已使用
	var updatedCode model.RedeemCode
	db.First(&updatedCode, code.ID)
	assert.True(t, updatedCode.IsUsed)
	assert.Equal(t, user.ID, *updatedCode.UsedByUserID)
}

func TestRedeem_AlreadyUsed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)

	db.AutoMigrate(&model.User{}, &model.UserSubscription{}, &model.SubscriptionToken{},
		&model.Plan{}, &model.RedeemCode{})

	cfg := &config.Config{
		JWTSecret:    "test-jwt-secret",
		JWTExpiresIn: 24 * time.Hour,
	}

	// 创建已使用的兑换码
	code := &model.RedeemCode{
		Code:         "USEDTEST001",
		PlanID:       1,
		DurationDays: 30,
		IsUsed:       true,
	}
	db.Create(code)

	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nil)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	user := &model.User{UUID: "u2", Username: "u2", PasswordHash: "x", XrayUserKey: "u2@x", Status: "active"}
	db.Create(user)
	token, _ := generateTestJWTToken(cfg, user.ID, false)

	body, _ := json.Marshal(gin.H{"code": "USEDTEST001"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRedeem_ExpiredCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.UserSubscription{}, &model.SubscriptionToken{},
		&model.Plan{}, &model.RedeemCode{})

	cfg := &config.Config{JWTSecret: "test-jwt-secret", JWTExpiresIn: 24 * time.Hour}
	user := &model.User{UUID: "expired-code-user", Username: "expiredcode", PasswordHash: "x", XrayUserKey: "expired@x", Status: "active"}
	db.Create(user)
	plan := &model.Plan{Name: "ExpiredCodePlan", Price: 10, DurationDays: 30, IsActive: true}
	db.Create(plan)
	expiredAt := time.Now().Add(-time.Hour)
	db.Create(&model.RedeemCode{Code: "EXPIREDCODE001", PlanID: plan.ID, DurationDays: 30, ExpiresAt: &expiredAt})

	redeemHandler := handler.NewRedeemHandler(
		repository.NewRedeemCodeRepository(db),
		repository.NewSubscriptionRepository(db),
		repository.NewPlanRepository(db),
		repository.NewSubscriptionTokenRepository(db),
		nil,
	)
	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	token, _ := generateTestJWTToken(cfg, user.ID, false)
	body, _ := json.Marshal(gin.H{"code": "EXPIREDCODE001"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var subCount int64
	db.Model(&model.UserSubscription{}).Where("user_id = ?", user.ID).Count(&subCount)
	assert.Equal(t, int64(0), subCount)
}

func TestRedeem_InvalidCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)

	db.AutoMigrate(&model.RedeemCode{})

	cfg := &config.Config{JWTSecret: "test-jwt-secret", JWTExpiresIn: 24 * time.Hour}

	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nil)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	user := &model.User{UUID: "u3", Username: "u3", PasswordHash: "x", XrayUserKey: "u3@x", Status: "active"}
	db.Create(user)
	token, _ := generateTestJWTToken(cfg, user.ID, false)

	body, _ := json.Marshal(gin.H{"code": "NONEXISTENT"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestRedeem_Renew_ExistingSubscription 测试已有订阅续费场景。
func TestRedeem_Renew_ExistingSubscription(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)

	db.AutoMigrate(&model.User{}, &model.UserSubscription{}, &model.SubscriptionToken{},
		&model.Plan{}, &model.NodeGroup{}, &model.Node{},
		&model.RedeemCode{}, &model.NodeAccessTask{})

	cfg := &config.Config{
		JWTSecret:      "test-jwt-secret",
		JWTExpiresIn:   24 * time.Hour,
		BCryptRounds:   4,
		TaskRetryLimit: 10,
	}

	user := &model.User{UUID: "renew-u", Username: "renewuser", PasswordHash: "h", XrayUserKey: "renew@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "RenewPlan", Price: 20.0, Currency: "USDT", TrafficLimit: 5368709120, DurationDays: 30, IsActive: true}
	db.Create(plan)

	// 已有活跃订阅
	existingSub := &model.UserSubscription{
		UserID: user.ID, PlanID: 1, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 15),
		TrafficLimit: 10737418240, UsedTraffic: 1000, Status: "ACTIVE",
	}
	db.Create(existingSub)

	code := &model.RedeemCode{Code: "RENEWTEST001", PlanID: plan.ID, DurationDays: 30, IsUsed: false}
	db.Create(code)

	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(
		repository.NewNodeAccessTaskRepository(db),
		repository.NewNodeRepository(db),
		planRepo, subRepo, nil, cfg,
	)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nodeAccessSvc)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	token, _ := generateTestJWTToken(cfg, user.ID, false)
	body, _ := json.Marshal(gin.H{"code": "RENEWTEST001"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// 验证原订阅被更新（未创建新订阅）
	var subCount int64
	db.Model(&model.UserSubscription{}).Where("user_id = ?", user.ID).Count(&subCount)
	assert.Equal(t, int64(1), subCount)

	// 验证流量叠加
	var updatedSub model.UserSubscription
	db.Where("user_id = ?", user.ID).First(&updatedSub)
	assert.Greater(t, updatedSub.TrafficLimit, uint64(10737418240))
}

// TestRedeem_MissingCodeField 测试缺少 code 字段。
func TestRedeem_MissingCodeField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.RedeemCode{})

	cfg := &config.Config{JWTSecret: "test-jwt-secret", JWTExpiresIn: 24 * time.Hour}
	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nil)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	user := &model.User{UUID: "no-code", Username: "noc", PasswordHash: "x", XrayUserKey: "nc@x", Status: "active"}
	db.Create(user)
	token, _ := generateTestJWTToken(cfg, user.ID, false)

	// 发送空 body
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminRedeemHandler_GenerateAndList 测试管理后台兑换码生成和列表。
func TestAdminRedeemHandler_GenerateAndList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.RedeemCode{}, &model.Plan{})

	cfg := &config.Config{JWTSecret: "admin-redeem-secret", JWTExpiresIn: 24 * time.Hour}
	redeemRepo := repository.NewRedeemCodeRepository(db)
	adminHandler := handler.NewAdminRedeemHandler(redeemRepo)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/admin/redeem-codes", adminHandler.Generate)
	r.GET("/api/admin/redeem-codes", adminHandler.List)

	adminUser := &model.User{UUID: "admin-r", Username: "adminr", PasswordHash: "x", XrayUserKey: "ar@x", Status: "active", IsAdmin: true}
	db.Create(adminUser)
	token, _ := generateTestJWTToken(cfg, adminUser.ID, true)

	// 生成兑换码
	genBody, _ := json.Marshal(gin.H{"plan_id": 1, "duration_days": 30, "count": 5})
	genReq := httptest.NewRequest("POST", "/api/admin/redeem-codes", bytes.NewReader(genBody))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	r.ServeHTTP(genW, genReq)

	assert.Equal(t, http.StatusOK, genW.Code)
	var genResp map[string]interface{}
	json.Unmarshal(genW.Body.Bytes(), &genResp)
	data := genResp["data"].(map[string]interface{})
	assert.Equal(t, float64(5), data["count"])
	codes := data["codes"].([]interface{})
	assert.Len(t, codes, 5)

	// 列表查询
	listReq := httptest.NewRequest("GET", "/api/admin/redeem-codes", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	assert.Equal(t, http.StatusOK, listW.Code)
	var listResp map[string]interface{}
	json.Unmarshal(listW.Body.Bytes(), &listResp)
	listData := listResp["data"].(map[string]interface{})
	assert.Equal(t, float64(5), listData["total"])
}

// TestRedeem_PlanNotFound 测试兑换码对应的套餐不存在。
func TestRedeem_PlanNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)

	db.AutoMigrate(&model.User{}, &model.UserSubscription{}, &model.SubscriptionToken{},
		&model.Plan{}, &model.RedeemCode{})

	cfg := &config.Config{JWTSecret: "test-jwt-secret", JWTExpiresIn: 24 * time.Hour}

	// 创建用户
	user := &model.User{UUID: "plan-notfound-u", Username: "pnf", PasswordHash: "x", XrayUserKey: "pnf@x", Status: "active"}
	db.Create(user)

	// 创建兑换码，指向不存在的套餐（PlanID=999）
	code := &model.RedeemCode{Code: "PLANMISSING01", PlanID: 999, DurationDays: 30, IsUsed: false}
	db.Create(code)

	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nil)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	token, _ := generateTestJWTToken(cfg, user.ID, false)
	body, _ := json.Marshal(gin.H{"code": "PLANMISSING01"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 套餐不存在时应返回 500（内部错误，因为 PlanRepo.FindByID 找不到记录）
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestRedeem_Generate_InvalidCount 测试管理后台生成兑换码时 count 无效。
func TestRedeem_Generate_InvalidCount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.RedeemCode{}, &model.Plan{})

	cfg := &config.Config{JWTSecret: "gen-invalid-secret", JWTExpiresIn: 24 * time.Hour}
	redeemRepo := repository.NewRedeemCodeRepository(db)
	adminHandler := handler.NewAdminRedeemHandler(redeemRepo)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/admin/redeem-codes", adminHandler.Generate)

	adminUser := &model.User{UUID: "admin-gen", Username: "ag", PasswordHash: "x", XrayUserKey: "ag@x", Status: "active", IsAdmin: true}
	db.Create(adminUser)
	token, _ := generateTestJWTToken(cfg, adminUser.ID, true)

	// count = 0（小于 min=1）
	genBody, _ := json.Marshal(gin.H{"plan_id": 1, "duration_days": 30, "count": 0})
	genReq := httptest.NewRequest("POST", "/api/admin/redeem-codes", bytes.NewReader(genBody))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	r.ServeHTTP(genW, genReq)

	assert.Equal(t, http.StatusBadRequest, genW.Code)
}

// TestRedeem_Generate_MissingFields 测试管理后台生成兑换码缺少必填字段。
func TestRedeem_Generate_MissingFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.RedeemCode{})

	cfg := &config.Config{JWTSecret: "gen-missing-secret", JWTExpiresIn: 24 * time.Hour}
	redeemRepo := repository.NewRedeemCodeRepository(db)
	adminHandler := handler.NewAdminRedeemHandler(redeemRepo)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/admin/redeem-codes", adminHandler.Generate)

	adminUser := &model.User{UUID: "admin-gm", Username: "agm", PasswordHash: "x", XrayUserKey: "agm@x", Status: "active", IsAdmin: true}
	db.Create(adminUser)
	token, _ := generateTestJWTToken(cfg, adminUser.ID, true)

	// 缺少 plan_id 和 duration_days
	genBody, _ := json.Marshal(gin.H{"count": 5})
	genReq := httptest.NewRequest("POST", "/api/admin/redeem-codes", bytes.NewReader(genBody))
	genReq.Header.Set("Authorization", "Bearer "+token)
	genReq.Header.Set("Content-Type", "application/json")
	genW := httptest.NewRecorder()
	r.ServeHTTP(genW, genReq)

	assert.Equal(t, http.StatusBadRequest, genW.Code)
}

// TestRedeem_NoAuth 测试未认证访问兑换接口。
func TestRedeem_NoAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.RedeemCode{})

	cfg := &config.Config{JWTSecret: "no-auth-secret", JWTExpiresIn: 24 * time.Hour}
	redeemRepo := repository.NewRedeemCodeRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	redeemHandler := handler.NewRedeemHandler(redeemRepo, subRepo, planRepo, tokenRepo, nil)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.POST("/api/redeem", redeemHandler.Redeem)

	body, _ := json.Marshal(gin.H{"code": "ANYCODE"})
	req := httptest.NewRequest("POST", "/api/redeem", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// 不设置 Authorization header
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAdminRedeemHandler_List_WithPagination 测试管理后台兑换码列表分页。
func TestAdminRedeemHandler_List_WithPagination(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openRedeemTestDB(t)
	db.AutoMigrate(&model.User{}, &model.RedeemCode{}, &model.Plan{})

	cfg := &config.Config{JWTSecret: "list-pagination-secret", JWTExpiresIn: 24 * time.Hour}
	redeemRepo := repository.NewRedeemCodeRepository(db)
	adminHandler := handler.NewAdminRedeemHandler(redeemRepo)

	r := gin.New()
	r.Use(middleware.JWTAuth(cfg.JWTSecret))
	r.GET("/api/admin/redeem-codes", adminHandler.List)

	adminUser := &model.User{UUID: "admin-list", Username: "al", PasswordHash: "x", XrayUserKey: "al@x", Status: "active", IsAdmin: true}
	db.Create(adminUser)
	token, _ := generateTestJWTToken(cfg, adminUser.ID, true)

	// 先创建一些兑换码
	for i := 0; i < 3; i++ {
		db.Create(&model.RedeemCode{
			Code:         fmt.Sprintf("LISTCODE%03d", i),
			PlanID:       1,
			DurationDays: 30,
		})
	}

	// 带分页参数查询
	listReq := httptest.NewRequest("GET", "/api/admin/redeem-codes?page=1&size=2", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	assert.Equal(t, http.StatusOK, listW.Code)

	var listResp map[string]interface{}
	json.Unmarshal(listW.Body.Bytes(), &listResp)
	listData := listResp["data"].(map[string]interface{})
	assert.Equal(t, float64(3), listData["total"])
	assert.Equal(t, float64(1), listData["page"])
	assert.Equal(t, float64(2), listData["size"])
}
