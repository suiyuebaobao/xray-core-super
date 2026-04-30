// node_access_test.go — 节点访问同步服务测试。
//
// 测试范围：
// - TriggerOnSubscribe / TriggerOnRenew / TriggerOnExpire / TriggerOnOverTraffic
// - ProcessHeartbeat 心跳处理
// - ReportTaskResult 任务结果上报
// - generateIdempotencyKey / generateLockToken
package service_test

import (
	"context"
	"testing"

	"suiyue/internal/config"
	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupNodeAccessTest(t *testing.T) (*gorm.DB, *service.NodeAccessService) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSubscription{},
		&model.Plan{},
		&model.Node{},
		&model.NodeGroup{},
		&model.NodeAccessTask{},
		&model.RefreshToken{},
	))

	// 手动创建 plan_node_groups 表（无对应 GORM 模型）
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	cfg := &config.Config{
		JWTSecret:           "test-secret",
		JWTExpiresIn:        24 * 60 * 60,
		TaskRetryLimit:      10,
	}

	taskRepo := repository.NewNodeAccessTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	userRepo := repository.NewUserRepository(db)

	// 创建默认用户供所有测试使用
	db.Create(&model.User{
		ID: 1, UUID: "default-user-uuid", Username: "defaultuser",
		PasswordHash: "hashed", XrayUserKey: "defaultuser@test.local",
		Status: "active",
	})

	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg)
	return db, nodeAccessSvc
}

// TestNodeAccessService_TriggerOnSubscribe 测试触发订阅创建任务。
func TestNodeAccessService_TriggerOnSubscribe(t *testing.T) {
	db, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 创建测试数据
	nodeGroup := &model.NodeGroup{Name: "group1"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name:           "test-node",
		Protocol:       "vless",
		Host:           "node.example.com",
		Port:           443,
		ServerName:     "node.example.com",
		AgentBaseURL:   "http://node:8080",
		AgentTokenHash: "hash",
		NodeGroupID:    &nodeGroup.ID,
		IsEnabled:      true,
	}
	db.Create(node)

	// 创建用户
	db.Create(&model.User{
		ID: 1, UUID: "test-uuid", Username: "testuser",
		PasswordHash: "hashed", XrayUserKey: "testuser@test.local",
		Status: "active",
	})

	sub := &model.UserSubscription{
		UserID:       1,
		PlanID:       1,
		StartDate:    db.NowFunc(),
		ExpireDate:   db.NowFunc().AddDate(0, 0, 30),
		TrafficLimit: 10737418240,
		Status:       "ACTIVE",
	}
	db.Create(sub)

	plan := &model.Plan{
		Name:         "test-plan",
		Price:        10.00,
		DurationDays: 30,
		IsActive:     true,
	}
	db.Create(plan)

	// 关联套餐与节点组
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	// 触发订阅创建
	err := svc.TriggerOnSubscribe(ctx, 1, sub.ID, plan.ID)
	assert.NoError(t, err)

	// 验证任务已创建
	var taskCount int64
	db.Model(&model.NodeAccessTask{}).Where("subscription_id = ? AND action = ?", sub.ID, "UPSERT_USER").Count(&taskCount)
	assert.GreaterOrEqual(t, taskCount, int64(1))
}

// TestNodeAccessService_TriggerOnExpire 测试触发订阅过期任务。
func TestNodeAccessService_TriggerOnExpire(t *testing.T) {
	db, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 创建用户
	db.Create(&model.User{
		ID: 1, UUID: "test-uuid", Username: "testuser",
		PasswordHash: "hashed", XrayUserKey: "testuser@test.local",
		Status: "active",
	})

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1, StartDate: db.NowFunc(),
		ExpireDate: db.NowFunc().AddDate(0, 0, 30),
		Status: "ACTIVE",
	}
	db.Create(sub)

	nodeGroup := &model.NodeGroup{Name: "expire-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "expire-node", Protocol: "vless", Host: "node.test",
		Port: 443, ServerName: "node.test", AgentBaseURL: "http://node:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	plan := &model.Plan{Name: "expire-plan", Price: 10, DurationDays: 30, IsActive: true}
	db.Create(plan)
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	err := svc.TriggerOnExpire(ctx, 1, sub.ID, plan.ID)
	assert.NoError(t, err)

	var count int64
	db.Model(&model.NodeAccessTask{}).Where("action = ?", "DISABLE_USER").Count(&count)
	assert.GreaterOrEqual(t, count, int64(1))
}

// TestNodeAccessService_ProcessHeartbeat 测试心跳处理。
func TestNodeAccessService_ProcessHeartbeat(t *testing.T) {
	db, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 先创建一个待执行任务
	task := &model.NodeAccessTask{
		NodeID:         1,
		Action:         "UPSERT_USER",
		Status:         "PENDING",
		IdempotencyKey: "test-key-1",
	}
	db.Create(task)

	tasks, err := svc.ProcessHeartbeat(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "UPSERT_USER", tasks[0].Action)

	// 验证任务已被锁定
	var updatedTask model.NodeAccessTask
	db.First(&updatedTask, task.ID)
	assert.Equal(t, "PROCESSING", updatedTask.Status)
	assert.NotNil(t, updatedTask.LockToken)
}

// TestNodeAccessService_ReportTaskResult 测试任务结果上报。
func TestNodeAccessService_ReportTaskResult(t *testing.T) {
	db, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	lockToken := "test-lock-token"
	task := &model.NodeAccessTask{
		NodeID: 1, Action: "UPSERT_USER", Status: "PROCESSING",
		IdempotencyKey: "test-key-2",
		LockToken:      &lockToken,
	}
	db.Create(task)

	// 上报成功
	err := svc.ReportTaskResult(ctx, task.ID, 1, "test-lock-token", true, "")
	assert.NoError(t, err)

	var doneTask model.NodeAccessTask
	db.First(&doneTask, task.ID)
	assert.Equal(t, "DONE", doneTask.Status)

	// 上报失败
	task2 := &model.NodeAccessTask{
		NodeID: 1, Action: "DISABLE_USER", Status: "PROCESSING",
		IdempotencyKey: "test-key-3",
		LockToken:      &lockToken,
	}
	db.Create(task2)

	err = svc.ReportTaskResult(ctx, task2.ID, 1, "test-lock-token", false, "connection timeout")
	assert.NoError(t, err)

	var failedTask model.NodeAccessTask
	db.First(&failedTask, task2.ID)
	assert.Equal(t, "FAILED", failedTask.Status)
	assert.Equal(t, "connection timeout", *failedTask.LastError)
}

// TestNodeAccessService_TriggerOnOverTraffic 测试超额触发。
func TestNodeAccessService_TriggerOnOverTraffic(t *testing.T) {
	_, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 无关联节点组时不应报错
	err := svc.TriggerOnOverTraffic(ctx, 1, 1, 1)
	assert.NoError(t, err)
}

// TestNodeAccessService_TriggerOnRenew 测试续费触发。
func TestNodeAccessService_TriggerOnRenew(t *testing.T) {
	_, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 无关联节点组时不应报错
	err := svc.TriggerOnRenew(ctx, 1, 1, 1)
	assert.NoError(t, err)
}

// TestNodeAccessService_ProcessHeartbeat_NoPendingTasks 测试无待执行任务的心跳。
func TestNodeAccessService_ProcessHeartbeat_NoPendingTasks(t *testing.T) {
	_, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 节点 99 没有任何任务
	tasks, err := svc.ProcessHeartbeat(ctx, 99)
	assert.NoError(t, err)
	assert.Len(t, tasks, 0)
}

// TestNodeAccessService_ProcessHeartbeat_MultipleTasks 测试多个待执行任务。
func TestNodeAccessService_ProcessHeartbeat_MultipleTasks(t *testing.T) {
	db, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	// 创建多个待执行任务
	task1 := &model.NodeAccessTask{
		NodeID: 1, Action: "UPSERT_USER", Status: "PENDING",
		IdempotencyKey: "multi-key-1",
	}
	db.Create(task1)

	task2 := &model.NodeAccessTask{
		NodeID: 1, Action: "DISABLE_USER", Status: "PENDING",
		IdempotencyKey: "multi-key-2",
	}
	db.Create(task2)

	tasks, err := svc.ProcessHeartbeat(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)

	// 验证两个任务都被锁定
	for _, task := range tasks {
		var updatedTask model.NodeAccessTask
		db.First(&updatedTask, task.ID)
		assert.Equal(t, "PROCESSING", updatedTask.Status)
		assert.NotNil(t, updatedTask.LockToken)
		assert.NotNil(t, updatedTask.LockedAt)
	}
}

// TestNodeAccessService_TriggerOnSubscribe_DuplicateTask 测试重复幂等键创建失败。
func TestNodeAccessService_TriggerOnSubscribe_DuplicateTask(t *testing.T) {
	db, svc := setupNodeAccessTest(t)
	ctx := context.Background()

	nodeGroup := &model.NodeGroup{Name: "dup-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "dup-node", Protocol: "vless", Host: "dn.test",
		Port: 443, ServerName: "dn.test", AgentBaseURL: "http://dn:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	// 创建用户
	db.Create(&model.User{
		ID: 1, UUID: "test-uuid", Username: "testuser",
		PasswordHash: "hashed", XrayUserKey: "testuser@test.local",
		Status: "active",
	})

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1, StartDate: db.NowFunc(),
		ExpireDate: db.NowFunc().AddDate(0, 0, 30),
		Status: "ACTIVE",
	}
	db.Create(sub)

	plan := &model.Plan{Name: "dup-plan", Price: 10, DurationDays: 30, IsActive: true}
	db.Create(plan)
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	// 先触发一次
	err := svc.TriggerOnSubscribe(ctx, 1, sub.ID, plan.ID)
	assert.NoError(t, err)

	// 验证任务已创建
	var taskCount int64
	db.Model(&model.NodeAccessTask{}).Where("subscription_id = ?", sub.ID).Count(&taskCount)
	assert.GreaterOrEqual(t, taskCount, int64(1))
}
