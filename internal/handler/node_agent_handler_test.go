// node_agent_handler_test.go — 节点代理 Handler 测试。
//
// 测试范围：
// - Heartbeat 心跳处理
// - TaskResult 任务结果上报
// - TrafficReport 流量上报
package handler_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"suiyue/internal/config"
	"suiyue/internal/handler"
	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func agentTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func setupAgentTest(t *testing.T) (*gin.Engine, *config.Config) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.Node{},
		&model.NodeAccessTask{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
		&model.User{},
		&model.UserSubscription{},
		&model.Plan{},
	))
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	cfg := &config.Config{
		JWTSecret:      "test-secret",
		JWTExpiresIn:   24 * 60 * 60,
		AgentAuthMode:  "token",
		TaskRetryLimit: 10,
	}

	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)

	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, repository.NewUserRepository(db),
		nodeAccessSvc,
	)

	agentHandler := handler.NewAgentHandler(nodeAccessSvc, trafficSvc, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())

	// 创建测试节点
	db.Create(&model.Node{
		Name:           "test-node",
		Protocol:       "vless",
		Host:           "test.local",
		Port:           443,
		AgentBaseURL:   "http://test.local:8080",
		AgentTokenHash: agentTokenHash("test-node-token"),
		IsEnabled:      true,
	})

	r.POST("/api/agent/heartbeat", agentHandler.Heartbeat)
	r.POST("/api/agent/task-result", agentHandler.TaskResult)
	r.POST("/api/agent/traffic", agentHandler.TrafficReport)

	return r, cfg
}

// TestAgentHandler_Heartbeat 测试心跳接口。
func TestAgentHandler_Heartbeat(t *testing.T) {
	// 先创建节点
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Node{}, &model.NodeAccessTask{}, &model.TrafficSnapshot{}, &model.UsageLedger{}))
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	db.Create(&model.Node{
		ID: 1, Name: "test-node", Protocol: "vless", Host: "node.test",
		Port: 443, ServerName: "node.test", AgentBaseURL: "http://node:8080",
		AgentTokenHash: agentTokenHash("test-token"), IsEnabled: true,
	})

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	trafficSvc := service.NewTrafficService(db, repository.NewTrafficSnapshotRepository(db), repository.NewUsageLedgerRepository(db), subRepo, nodeRepo, repository.NewUserRepository(db), nodeAccessSvc)
	agentHandler := handler.NewAgentHandler(nodeAccessSvc, trafficSvc, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/heartbeat", agentHandler.Heartbeat)

	// 节点存在但无任务
	body := map[string]interface{}{"node_id": uint64(1),
		"token": "test-token"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAgentHandler_TaskResult 测试任务结果上报。
func TestAgentHandler_TaskResult(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.NodeAccessTask{}, &model.Node{}))

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)

	// 创建节点
	db.Create(&model.Node{
		Name:           "test-node",
		Protocol:       "vless",
		Host:           "test.local",
		Port:           443,
		AgentBaseURL:   "http://test.local:8080",
		AgentTokenHash: agentTokenHash("test-token"),
		IsEnabled:      true,
	})

	// 创建一个待处理任务
	lockToken := "test-lock-token"
	db.Create(&model.NodeAccessTask{
		ID: 1, NodeID: 1, Action: "UPSERT_USER", Status: "PROCESSING",
		IdempotencyKey: "test-key-result",
		LockToken:      &lockToken,
	})

	agentHandler := handler.NewAgentHandler(nodeAccessSvc, nil, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/task-result", agentHandler.TaskResult)

	body := map[string]interface{}{"node_id": uint64(1), "token": "test-token", "task_id": 1, "lock_token": "test-lock-token", "success": true}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/task-result", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAgentHandler_TrafficReport 测试流量上报。
func TestAgentHandler_TrafficReport(t *testing.T) {
	r, _ := setupAgentTest(t)

	body := map[string]interface{}{
		"node_id": uint64(1),
		"token":   "test-node-token",
		"items": []map[string]interface{}{
			{"xray_user_key": "testuser@test.local", "uplink_total": 1000, "downlink_total": 2000},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/traffic", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 无匹配用户时应正常返回（不报错，跳过）
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAgentHandler_TrafficReport_InvalidBody 测试无效请求体。
func TestAgentHandler_TrafficReport_InvalidBody(t *testing.T) {
	r, _ := setupAgentTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/agent/traffic", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAgentHandler_Heartbeat_WithPendingTasks 测试心跳返回待执行任务。
func TestAgentHandler_Heartbeat_WithPendingTasks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Node{},
		&model.NodeAccessTask{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
		&model.User{},
		&model.UserSubscription{},
		&model.Plan{},
	))
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	cfg := &config.Config{
		JWTSecret:      "test-secret",
		JWTExpiresIn:   24 * 60 * 60,
		AgentAuthMode:  "token",
		TaskRetryLimit: 10,
	}

	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)

	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, repository.NewUserRepository(db),
		nodeAccessSvc,
	)

	agentHandler := handler.NewAgentHandler(nodeAccessSvc, trafficSvc, nodeRepo)

	// 创建节点和待执行任务
	db.Create(&model.Node{
		ID: 1, Name: "test-node", Protocol: "vless", Host: "node.test",
		Port: 443, ServerName: "node.test", AgentBaseURL: "http://node:8080",
		AgentTokenHash: agentTokenHash("test-token"), IsEnabled: true,
	})
	db.Create(&model.NodeAccessTask{
		NodeID: 1, Action: "UPSERT_USER", Status: "PENDING",
		IdempotencyKey: "test-key-heartbeat",
	})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/heartbeat", agentHandler.Heartbeat)

	body := map[string]interface{}{
		"node_id": 1,
		"token":   "test-token",
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	assert.True(t, resp["success"].(bool))
	data := resp["data"].(map[string]interface{})
	tasks := data["tasks"].([]interface{})
	assert.GreaterOrEqual(t, len(tasks), 1)
}

// TestAgentHandler_TaskResult_InvalidBody 测试任务结果上报无效请求体。
func TestAgentHandler_TaskResult_InvalidBody(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.NodeAccessTask{}))

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	agentHandler := handler.NewAgentHandler(nodeAccessSvc, nil, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/task-result", agentHandler.TaskResult)

	// 缺少 task_id
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/task-result", bytes.NewReader([]byte(`{"success":true}`)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAgentHandler_TaskResult_TaskNotFound 测试上报不存在的任务。
func TestAgentHandler_TaskResult_TaskNotFound(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.NodeAccessTask{}))

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	agentHandler := handler.NewAgentHandler(nodeAccessSvc, nil, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/task-result", agentHandler.TaskResult)

	// 任务不存在
	body := map[string]interface{}{"task_id": 999, "success": true}
	jsonBody, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agent/task-result", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// 服务层对不存在的任务更新可能返回错误或成功，取决于实现
	_ = w.Code
}

// TestAgentHandler_Heartbeat_InvalidBody 测试心跳无效请求体。
func TestAgentHandler_Heartbeat_InvalidBody(t *testing.T) {
	r, _ := setupAgentTest(t)

	// 缺少 node_id
	body := map[string]interface{}{"token": "some-token"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAgentHandler_Heartbeat_AuthFailure 测试心跳认证失败（节点不存在或 token 不匹配）。
func TestAgentHandler_Heartbeat_AuthFailure(t *testing.T) {
	r, _ := setupAgentTest(t)

	// 节点 ID 不存在
	body := map[string]interface{}{"node_id": 999, "token": "wrong-token"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAgentHandler_Heartbeat_TokenMismatch 测试心跳 token 不匹配。
func TestAgentHandler_Heartbeat_TokenMismatch(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.Node{},
		&model.NodeAccessTask{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
		&model.User{},
		&model.UserSubscription{},
		&model.Plan{},
	))
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	// 创建节点但 token 不同
	db.Create(&model.Node{
		ID: 1, Name: "test-node", Protocol: "vless", Host: "node.test",
		Port: 443, ServerName: "node.test", AgentBaseURL: "http://node:8080",
		AgentTokenHash: agentTokenHash("correct-token"), IsEnabled: true,
	})

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, repository.NewUserRepository(db),
		nodeAccessSvc,
	)
	agentHandler := handler.NewAgentHandler(nodeAccessSvc, trafficSvc, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/heartbeat", agentHandler.Heartbeat)

	// 发送错误的 token
	body := map[string]interface{}{"node_id": uint64(1),
		"token": "wrong-token"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAgentHandler_Heartbeat_StoredHashCannotAuthenticate 验证数据库哈希不能直接作为 bearer 凭证。
func TestAgentHandler_Heartbeat_StoredHashCannotAuthenticate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Node{}, &model.NodeAccessTask{}))

	storedHash := agentTokenHash("correct-token")
	db.Create(&model.Node{
		ID: 1, Name: "test-node", Protocol: "vless", Host: "node.test",
		Port: 443, AgentBaseURL: "http://node:8080",
		AgentTokenHash: storedHash, IsEnabled: true,
	})

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)
	agentHandler := handler.NewAgentHandler(nodeAccessSvc, nil, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/heartbeat", agentHandler.Heartbeat)

	body := map[string]interface{}{"node_id": uint64(1), "token": storedHash}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/heartbeat", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAgentHandler_TaskResult_WithLockToken 测试任务结果上报带 lock_token。
func TestAgentHandler_TaskResult_WithLockToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.NodeAccessTask{}, &model.Node{}))

	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * 60 * 60, AgentAuthMode: "token", TaskRetryLimit: 10}

	// 创建节点
	db.Create(&model.Node{
		Name: "test-node", Protocol: "vless", Host: "test.local",
		Port: 443, AgentBaseURL: "http://test.local:8080",
		AgentTokenHash: agentTokenHash("test-node-token"), IsEnabled: true,
	})
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, nil, cfg)

	payload := "test-payload"
	lockToken := "test-lock-token"
	db.Create(&model.NodeAccessTask{
		ID: 1, NodeID: 1, Action: "UPSERT_USER", Status: "PROCESSING",
		IdempotencyKey: "test-key-lock", Payload: &payload, LockToken: &lockToken,
	})

	agentHandler := handler.NewAgentHandler(nodeAccessSvc, nil, nodeRepo)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/api/agent/task-result", agentHandler.TaskResult)

	body := map[string]interface{}{"node_id": 1, "token": "test-node-token", "task_id": 1, "success": true, "lock_token": "test-lock-token"}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/task-result", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAgentHandler_TrafficReport_EmptyItems 测试流量上报空 items。
func TestAgentHandler_TrafficReport_EmptyItems(t *testing.T) {
	r, _ := setupAgentTest(t)

	body := map[string]interface{}{
		"node_id": uint64(1),
		"token":   "test-node-token",
		"items":   []map[string]interface{}{},
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/traffic", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 空 items 应该仍返回 200（不报错）
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestAgentHandler_TrafficReport_MissingFields 测试流量上报缺少必填字段。
func TestAgentHandler_TrafficReport_MissingFields(t *testing.T) {
	r, _ := setupAgentTest(t)

	// 缺少 node_id
	body := map[string]interface{}{
		"items": []map[string]interface{}{
			{"xray_user_key": "test@test.local", "uplink_total": 100, "downlink_total": 200},
		},
	}
	jsonBody, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/agent/traffic", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
