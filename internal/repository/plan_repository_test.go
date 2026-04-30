// plan_repository_test.go — 套餐 Repository 测试。
package repository_test

import (
	"context"
	"testing"

	"suiyue/internal/model"
	"suiyue/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupPlanTestDB 创建测试用数据库。
func setupPlanTestDB(t *testing.T) (*gorm.DB, *repository.PlanRepository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	// 创建 PlanNodeGroup 模型
	type PlanNodeGroup struct {
		ID          uint64 `gorm:"primaryKey;column:id"`
		PlanID      uint64 `gorm:"column:plan_id"`
		NodeGroupID uint64 `gorm:"column:node_group_id"`
	}

	require.NoError(t, db.AutoMigrate(
		&model.Plan{},
		&model.NodeGroup{},
		&PlanNodeGroup{},
	))

	repo := repository.NewPlanRepository(db)
	return db, repo
}

// TestPlanRepository_BindNodeGroups 测试套餐绑定节点分组。
func TestPlanRepository_BindNodeGroups(t *testing.T) {
	db, repo := setupPlanTestDB(t)
	ctx := context.Background()

	// 创建套餐
	plan := &model.Plan{Name: "Test Plan", Price: 10.00, TrafficLimit: 1024, DurationDays: 30, IsActive: true}
	created, err := repo.Create(ctx, plan)
	require.NoError(t, err)

	// 创建节点分组
	group1 := &model.NodeGroup{Name: "Group 1"}
	group2 := &model.NodeGroup{Name: "Group 2"}
	require.NoError(t, db.Create(group1).Error)
	require.NoError(t, db.Create(group2).Error)

	// 绑定
	require.NoError(t, repo.BindNodeGroups(ctx, created.ID, []uint64{group1.ID, group2.ID}))

	// 查询
	ids, err := repo.FindNodeGroupIDs(ctx, created.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, group1.ID)
	assert.Contains(t, ids, group2.ID)

	// 重新绑定（覆盖）
	require.NoError(t, repo.BindNodeGroups(ctx, created.ID, []uint64{group1.ID}))
	ids, err = repo.FindNodeGroupIDs(ctx, created.ID)
	require.NoError(t, err)
	assert.Len(t, ids, 1)
	assert.Equal(t, group1.ID, ids[0])
}
