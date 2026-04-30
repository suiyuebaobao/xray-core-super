// traffic_integration_test.go — 流量采集集成测试。
//
// 测试完整的流量处理链路：
// 1. 上报流量数据
// 2. 快照差值计算
// 3. 账本记录写入
// 4. 订阅使用量更新
package service_test

import (
	"context"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PlanNodeGroup 套餐-节点分组关联模型（测试用）。
type PlanNodeGroup struct {
	ID          uint64 `gorm:"primaryKey;column:id"`
	PlanID      uint64 `gorm:"column:plan_id"`
	NodeGroupID uint64 `gorm:"column:node_group_id"`
}

func (PlanNodeGroup) TableName() string { return "plan_node_groups" }

// setupTrafficTestDB 创建流量测试用数据库。
func setupTrafficTestDB(t *testing.T) *gorm.DB {
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
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
		&model.NodeGroup{},
		&model.Node{},
		&PlanNodeGroup{},
	))

	return db
}

// TestTrafficService_ProcessTrafficReport_CreatesBaseline 测试首次上报创建基线。
func TestTrafficService_ProcessTrafficReport_CreatesBaseline(t *testing.T) {
	db := setupTrafficTestDB(t)

	userRepo := repository.NewUserRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	// 创建用户
	user := &model.User{
		UUID:        "test-uuid",
		Username:    "testuser",
		PasswordHash: "hash",
		XrayUserKey: "testuser@test.local",
		Status:      "active",
	}
	require.NoError(t, db.Create(user).Error)

	// 创建订阅
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       1,
		StartDate:    time.Now().Add(-24 * time.Hour),
		ExpireDate:   time.Now().Add(24 * time.Hour),
		TrafficLimit: 1024 * 1024 * 1024, // 1GB
		UsedTraffic:  0,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	// 创建节点仓库
	nodeRepo := repository.NewNodeRepository(db)

	// 创建流量采集服务
	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo, nil)

	// 首次上报流量
	items := []service.TrafficItem{
		{
			XrayUserKey:   "testuser@test.local",
			UplinkTotal:   1000,
			DownlinkTotal: 2000,
		},
	}

	err := trafficSvc.ProcessTrafficReport(context.Background(), 1, items)
	assert.NoError(t, err)

	// 验证快照已创建（基线）
	var snapshotCount int64
	db.Model(&model.TrafficSnapshot{}).Count(&snapshotCount)
	assert.Equal(t, int64(1), snapshotCount)

	// 验证账本未写入（基线不产生流量）
	var ledgerCount int64
	db.Model(&model.UsageLedger{}).Count(&ledgerCount)
	assert.Equal(t, int64(0), ledgerCount)
}

// TestTrafficService_ProcessTrafficReport_WithPreviousSnapshot 测试有历史快照时计算增量。
func TestTrafficService_ProcessTrafficReport_WithPreviousSnapshot(t *testing.T) {
	db := setupTrafficTestDB(t)

	userRepo := repository.NewUserRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	// 创建用户
	user := &model.User{
		UUID:        "test-uuid",
		Username:    "testuser",
		PasswordHash: "hash",
		XrayUserKey: "testuser@test.local",
		Status:      "active",
	}
	require.NoError(t, db.Create(user).Error)

	// 创建订阅
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       1,
		StartDate:    time.Now().Add(-24 * time.Hour),
		ExpireDate:   time.Now().Add(24 * time.Hour),
		TrafficLimit: 1024 * 1024 * 1024,
		UsedTraffic:  0,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	// 创建历史快照
	oldSnapshot := &model.TrafficSnapshot{
		NodeID:        1,
		XrayUserKey:   "testuser@test.local",
		UplinkTotal:   1000,
		DownlinkTotal: 2000,
		CapturedAt:    time.Now().Add(-5 * time.Minute),
	}
	require.NoError(t, db.Create(oldSnapshot).Error)

	// 创建节点仓库
	nodeRepo := repository.NewNodeRepository(db)

	// 创建流量采集服务
	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo, nil)

	// 上报新流量
	items := []service.TrafficItem{
		{
			XrayUserKey:   "testuser@test.local",
			UplinkTotal:   1500,  // 增量 500
			DownlinkTotal: 3000,  // 增量 1000
		},
	}

	err := trafficSvc.ProcessTrafficReport(context.Background(), 1, items)
	assert.NoError(t, err)

	// 验证新快照已创建
	var snapshotCount int64
	db.Model(&model.TrafficSnapshot{}).Count(&snapshotCount)
	assert.Equal(t, int64(2), snapshotCount)

	// 验证账本已写入
	var ledgers []model.UsageLedger
	db.Find(&ledgers)
	require.Len(t, ledgers, 1)
	assert.Equal(t, uint64(500), ledgers[0].DeltaUpload)
	assert.Equal(t, uint64(1000), ledgers[0].DeltaDownload)
	assert.Equal(t, uint64(1500), ledgers[0].DeltaTotal)

	// 验证订阅使用量已更新
	var updatedSub model.UserSubscription
	db.First(&updatedSub, sub.ID)
	assert.Equal(t, uint64(1500), updatedSub.UsedTraffic)
}

// TestTrafficService_ProcessTrafficReport_CounterReset 测试节点重启计数器归零。
func TestTrafficService_ProcessTrafficReport_CounterReset(t *testing.T) {
	db := setupTrafficTestDB(t)

	userRepo := repository.NewUserRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	snapshotRepo := repository.NewTrafficSnapshotRepository(db)
	ledgerRepo := repository.NewUsageLedgerRepository(db)

	// 创建用户
	user := &model.User{
		UUID:        "test-uuid",
		Username:    "testuser",
		PasswordHash: "hash",
		XrayUserKey: "testuser@test.local",
		Status:      "active",
	}
	require.NoError(t, db.Create(user).Error)

	// 创建订阅
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       1,
		StartDate:    time.Now().Add(-24 * time.Hour),
		ExpireDate:   time.Now().Add(24 * time.Hour),
		TrafficLimit: 1024 * 1024 * 1024,
		UsedTraffic:  0,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	// 创建历史快照（计数器较大）
	oldSnapshot := &model.TrafficSnapshot{
		NodeID:        1,
		XrayUserKey:   "testuser@test.local",
		UplinkTotal:   1000000,
		DownlinkTotal: 2000000,
		CapturedAt:    time.Now().Add(-5 * time.Minute),
	}
	require.NoError(t, db.Create(oldSnapshot).Error)

	// 创建节点仓库
	nodeRepo := repository.NewNodeRepository(db)

	// 创建流量采集服务
	trafficSvc := service.NewTrafficService(db, snapshotRepo, ledgerRepo, subRepo, nodeRepo, userRepo, nil)

	// 上报流量（计数器归零）
	items := []service.TrafficItem{
		{
			XrayUserKey:   "testuser@test.local",
			UplinkTotal:   1000,  // 比历史快照小
			DownlinkTotal: 2000,  // 比历史快照小
		},
	}

	err := trafficSvc.ProcessTrafficReport(context.Background(), 1, items)
	assert.NoError(t, err)

	// 验证新快照已创建
	var snapshotCount int64
	db.Model(&model.TrafficSnapshot{}).Count(&snapshotCount)
	assert.Equal(t, int64(2), snapshotCount)

	// 验证账本未写入（计数器归零，增量为 0）
	var ledgerCount int64
	db.Model(&model.UsageLedger{}).Count(&ledgerCount)
	assert.Equal(t, int64(0), ledgerCount)
}
