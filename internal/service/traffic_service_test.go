// traffic_service_test.go — 流量采集服务测试。
//
// 测试范围：
// - 快照差值计算正确性
// - 节点重启计数器归零处理
// - 超限检测
package service_test

import (
	"context"
	"testing"
	"time"

	"suiyue/internal/config"
	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestCalculateDelta_Normal 测试正常增量计算。
func TestCalculateDelta_Normal(t *testing.T) {
	assert.Equal(t, uint64(100), service.CalculateDelta(200, 300))
	assert.Equal(t, uint64(0), service.CalculateDelta(500, 500))
	assert.Equal(t, uint64(1000), service.CalculateDelta(0, 1000))
}

// TestCalculateDelta_CounterReset 测试节点重启计数器归零。
func TestCalculateDelta_CounterReset(t *testing.T) {
	// new < old 说明计数器重置，应返回 0
	assert.Equal(t, uint64(0), service.CalculateDelta(1000, 100))
	assert.Equal(t, uint64(0), service.CalculateDelta(999999, 0))
}

func setupTrafficOverQuotaDB(t *testing.T) *gorm.DB {
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
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
	))
	// 手动创建 plan_node_groups 表
	db.Exec("CREATE TABLE IF NOT EXISTS plan_node_groups (id INTEGER PRIMARY KEY AUTOINCREMENT, plan_id INTEGER, node_group_id INTEGER, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)")

	// 创建默认用户
	db.Create(&model.User{
		ID: 1, UUID: "traffic-user-uuid", Username: "trafficuser",
		PasswordHash: "hashed", XrayUserKey: "trafficuser@test.local",
		Status: "active",
	})

	return db
}

// TestCheckAndHandleOverQuota_TrafficExceeded 测试流量超额触发 SUSPENDED。
func TestCheckAndHandleOverQuota_TrafficExceeded(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	nodeGroup := &model.NodeGroup{Name: "quota-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "quota-node", Protocol: "vless", Host: "node.test",
		Port: 443, ServerName: "node.test", AgentBaseURL: "http://node:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	plan := &model.Plan{Name: "quota-plan", Price: 10, DurationDays: 30, TrafficLimit: 1000, IsActive: true}
	db.Create(plan)
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	sub := &model.UserSubscription{
		UserID: 1, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 1000, UsedTraffic: 2000, Status: "ACTIVE",
	}
	db.Create(sub)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)

	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg),
	)

	err := trafficSvc.CheckAndHandleOverQuota(context.Background(), sub)
	assert.NoError(t, err)

	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, "SUSPENDED", updatedSub.Status)
}

// TestCheckAndHandleOverQuota_NoOverage 测试未超额时不触发操作。
func TestCheckAndHandleOverQuota_NoOverage(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 10000, UsedTraffic: 100, Status: "ACTIVE",
	}
	db.Create(sub)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)

	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg),
	)

	err := trafficSvc.CheckAndHandleOverQuota(context.Background(), sub)
	assert.NoError(t, err)

	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, "ACTIVE", updatedSub.Status)
}

// TestProcessTrafficReport_WithBaseline 测试 ProcessTrafficReport 创建账本记录。
func TestProcessTrafficReport_WithBaseline(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	// 创建必要的数据
	nodeGroup := &model.NodeGroup{Name: "report-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "report-node", Protocol: "vless", Host: "rn.test",
		Port: 443, ServerName: "rn.test", AgentBaseURL: "http://rn:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	plan := &model.Plan{Name: "report-plan", Price: 10, DurationDays: 30, TrafficLimit: 100000, IsActive: true}
	db.Create(plan)
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	user := &model.User{UUID: "tr1", Username: "trafficreport", PasswordHash: "h", XrayUserKey: "tr@x.local", Status: "active"}
	db.Create(user)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 100000, UsedTraffic: 0, Status: "ACTIVE",
	}
	db.Create(sub)

	// 先创建一个基线快照
	baseline := &model.TrafficSnapshot{
		NodeID: node.ID, XrayUserKey: "tr@x.local",
		UplinkTotal: 100, DownlinkTotal: 200, CapturedAt: time.Now(),
	}
	db.Create(baseline)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg))

	ctx := context.Background()

	// 上报新的流量数据（比基线大）
	items := []service.TrafficItem{
		{
			XrayUserKey:   "tr@x.local",
			UplinkTotal:   500,
			DownlinkTotal: 800,
		},
	}

	err := trafficSvc.ProcessTrafficReport(ctx, node.ID, items)
	assert.NoError(t, err)

	// 验证新快照被创建
	latest, err := snapshotRepo.FindLatest(ctx, node.ID, "tr@x.local")
	require.NoError(t, err)
	assert.Equal(t, uint64(500), latest.UplinkTotal)
	assert.Equal(t, uint64(800), latest.DownlinkTotal)

	// 验证账本记录（增量 = 新 - 基线）
	var ledgers []model.UsageLedger
	db.Where("node_id = ? AND delta_total > 0", node.ID).Find(&ledgers)
	assert.GreaterOrEqual(t, len(ledgers), 1)
	// uplink delta = 500 - 100 = 400, downlink delta = 800 - 200 = 600, total = 1000
	assert.Equal(t, uint64(400), ledgers[0].DeltaUpload)
	assert.Equal(t, uint64(600), ledgers[0].DeltaDownload)
}

// TestProcessTrafficReport_EmptyReport 测试空报告处理。
func TestProcessTrafficReport_EmptyReport(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	nodeGroup := &model.NodeGroup{Name: "empty-group"}
	db.Create(nodeGroup)
	node := &model.Node{
		Name: "empty-node", Protocol: "vless", Host: "en.test",
		Port: 443, ServerName: "en.test", AgentBaseURL: "http://en:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg))

	ctx := context.Background()

	// 空报告不应报错
	err := trafficSvc.ProcessTrafficReport(ctx, node.ID, []service.TrafficItem{})
	assert.NoError(t, err)
}

// TestCheckAndHandleOverQuota_ExpiredSubscription 测试过期订阅不触发 SUSPENDED。
func TestCheckAndHandleOverQuota_ExpiredSubscription(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	// 过期但流量未超的订阅
	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1, StartDate: time.Now().AddDate(0, 0, -60),
		ExpireDate:   time.Now().AddDate(0, 0, -30), // 已过期
		TrafficLimit: 1000, UsedTraffic: 500, Status: "EXPIRED",
	}
	db.Create(sub)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)

	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg),
	)

	err := trafficSvc.CheckAndHandleOverQuota(context.Background(), sub)
	assert.NoError(t, err)

	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, "EXPIRED", updatedSub.Status) // 不应变为 SUSPENDED
}

// TestCheckAndHandleOverQuota_ExpiredTriggersDisable 测试过期订阅触发 DISABLE_USER 任务。
func TestCheckAndHandleOverQuota_ExpiredTriggersDisable(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	// 创建必要的节点组、节点、套餐
	nodeGroup := &model.NodeGroup{Name: "expire-trigger-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "expire-trigger-node", Protocol: "vless", Host: "etn.test",
		Port: 443, ServerName: "etn.test", AgentBaseURL: "http://etn:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	plan := &model.Plan{Name: "expire-trigger-plan", Price: 10, DurationDays: 30, TrafficLimit: 100000, IsActive: true}
	db.Create(plan)
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	// 已过期但流量未超的订阅（ACTIVE 状态，过期时间在过去）
	sub := &model.UserSubscription{
		UserID: 1, PlanID: plan.ID, StartDate: time.Now().AddDate(0, 0, -60),
		ExpireDate:   time.Now().AddDate(0, 0, -30), // 已过期
		TrafficLimit: 100000, UsedTraffic: 500, Status: "ACTIVE",
	}
	db.Create(sub)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)

	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg),
	)

	err := trafficSvc.CheckAndHandleOverQuota(context.Background(), sub)
	assert.NoError(t, err)

	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, "EXPIRED", updatedSub.Status)

	// 验证创建了 DISABLE_USER 任务
	var taskCount int64
	db.Model(&model.NodeAccessTask{}).Where("action = ?", "DISABLE_USER").Count(&taskCount)
	assert.GreaterOrEqual(t, taskCount, int64(1))
}

// TestCheckAndHandleOverQuota_WithinLimits 测试未超额未过期时返回 nil。
func TestCheckAndHandleOverQuota_WithinLimits(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	sub := &model.UserSubscription{
		UserID: 1, PlanID: 1, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30), // 未过期
		TrafficLimit: 10000, UsedTraffic: 100, Status: "ACTIVE",
	}
	db.Create(sub)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)

	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg),
	)

	err := trafficSvc.CheckAndHandleOverQuota(context.Background(), sub)
	assert.NoError(t, err)

	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, "ACTIVE", updatedSub.Status) // 状态不变
}

// TestProcessTrafficReport_UnknownUserSkipped 测试未知用户跳过。
func TestProcessTrafficReport_UnknownUserSkipped(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	nodeGroup := &model.NodeGroup{Name: "unknown-user-group"}
	db.Create(nodeGroup)
	node := &model.Node{
		Name: "unknown-user-node", Protocol: "vless", Host: "uun.test",
		Port: 443, ServerName: "uun.test", AgentBaseURL: "http://uun:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg))

	ctx := context.Background()

	// 上报不存在的用户
	items := []service.TrafficItem{
		{XrayUserKey: "nonexistent@x.local", UplinkTotal: 100, DownlinkTotal: 200},
	}

	err := trafficSvc.ProcessTrafficReport(ctx, node.ID, items)
	assert.NoError(t, err) // 未知用户应被跳过，不报错
}

// TestProcessTrafficReport_NoActiveSubscription 测试无活跃订阅时跳过。
func TestProcessTrafficReport_NoActiveSubscription(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	nodeGroup := &model.NodeGroup{Name: "no-sub-group"}
	db.Create(nodeGroup)
	node := &model.Node{
		Name: "no-sub-node", Protocol: "vless", Host: "nsn.test",
		Port: 443, ServerName: "nsn.test", AgentBaseURL: "http://nsn:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	// 创建用户但无订阅
	user := &model.User{UUID: "nosub", Username: "nosubuser", PasswordHash: "h", XrayUserKey: "nosub@x.local", Status: "active"}
	db.Create(user)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg))

	ctx := context.Background()

	items := []service.TrafficItem{
		{XrayUserKey: "nosub@x.local", UplinkTotal: 100, DownlinkTotal: 200},
	}

	err := trafficSvc.ProcessTrafficReport(ctx, node.ID, items)
	assert.NoError(t, err) // 无活跃订阅应被跳过
}

// TestProcessTrafficReport_ZeroDelta 测试增量为零时不创建账本。
func TestProcessTrafficReport_ZeroDelta(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)
	cfg := &config.Config{JWTSecret: "test-secret", JWTExpiresIn: 24 * time.Hour, TaskRetryLimit: 10}

	nodeGroup := &model.NodeGroup{Name: "zero-delta-group"}
	db.Create(nodeGroup)
	node := &model.Node{
		Name: "zero-delta-node", Protocol: "vless", Host: "zdn.test",
		Port: 443, ServerName: "zdn.test", AgentBaseURL: "http://zdn:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	user := &model.User{UUID: "zerodelta", Username: "zerodeltauser", PasswordHash: "h", XrayUserKey: "zerodelta@x.local", Status: "active"}
	db.Create(user)

	// 创建基线快照
	baseline := &model.TrafficSnapshot{
		NodeID: node.ID, XrayUserKey: "zerodelta@x.local",
		UplinkTotal: 1000, DownlinkTotal: 2000, CapturedAt: time.Now(),
	}
	db.Create(baseline)

	subRepo := repository.NewSubscriptionRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	userRepo := repository.NewUserRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo,
		service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg))

	ctx := context.Background()

	// 上报与基线相同的值（零增量）
	items := []service.TrafficItem{
		{XrayUserKey: "zerodelta@x.local", UplinkTotal: 1000, DownlinkTotal: 2000},
	}

	err := trafficSvc.ProcessTrafficReport(ctx, node.ID, items)
	assert.NoError(t, err)

	// 验证新快照已创建（即使增量为零也要保持基线最新）
	latest, err := snapshotRepo.FindLatest(ctx, node.ID, "zerodelta@x.local")
	require.NoError(t, err)
	assert.Equal(t, uint64(1000), latest.UplinkTotal)

	// 零增量不应创建账本记录
	var ledgers []model.UsageLedger
	db.Where("node_id = ?", node.ID).Find(&ledgers)
	assert.Len(t, ledgers, 0)
}

// TestProcessTrafficReport_DuplicateSameSecondSnapshotNotDoubleBilled 测试 MySQL 秒级时间精度下重复上报不重复计费。
func TestProcessTrafficReport_DuplicateSameSecondSnapshotNotDoubleBilled(t *testing.T) {
	db := setupTrafficOverQuotaDB(t)

	node := &model.Node{
		Name: "same-second-node", Protocol: "vless", Host: "ssn.test",
		Port: 443, ServerName: "ssn.test", AgentBaseURL: "http://ssn:8080",
		AgentTokenHash: "hash", IsEnabled: true,
	}
	require.NoError(t, db.Create(node).Error)

	plan := &model.Plan{Name: "same-second-plan", Price: 10, DurationDays: 30, TrafficLimit: 100000, IsActive: true}
	require.NoError(t, db.Create(plan).Error)
	sub := &model.UserSubscription{
		UserID: 1, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 100000, UsedTraffic: 0, Status: "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	capturedAt := time.Now().Truncate(time.Second)
	require.NoError(t, db.Create(&model.TrafficSnapshot{
		NodeID: node.ID, XrayUserKey: "trafficuser@test.local",
		UplinkTotal: 1000, DownlinkTotal: 2000, CapturedAt: capturedAt,
	}).Error)
	require.NoError(t, db.Create(&model.TrafficSnapshot{
		NodeID: node.ID, XrayUserKey: "trafficuser@test.local",
		UplinkTotal: 1600, DownlinkTotal: 2600, CapturedAt: capturedAt,
	}).Error)

	trafficSvc := service.NewTrafficService(db,
		repository.NewTrafficSnapshotRepository(db),
		repository.NewUsageLedgerRepository(db),
		repository.NewSubscriptionRepository(db),
		repository.NewNodeRepository(db),
		repository.NewUserRepository(db),
		nil,
	)

	err := trafficSvc.ProcessTrafficReport(context.Background(), node.ID, []service.TrafficItem{
		{XrayUserKey: "trafficuser@test.local", UplinkTotal: 1600, DownlinkTotal: 2600},
	})
	require.NoError(t, err)

	var ledgerCount int64
	require.NoError(t, db.Model(&model.UsageLedger{}).Where("node_id = ?", node.ID).Count(&ledgerCount).Error)
	assert.Equal(t, int64(0), ledgerCount)

	var snapshotCount int64
	require.NoError(t, db.Model(&model.TrafficSnapshot{}).Where("node_id = ?", node.ID).Count(&snapshotCount).Error)
	assert.Equal(t, int64(3), snapshotCount)
}
