// order_service_test.go — 订单服务测试。
//
// 测试范围：
// - 创建订单（正常流程）
// - 创建订单（套餐不存在）
// - 创建订单（套餐已下架）
// - 过期订单扫描
package service_test

import (
	"context"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupOrderServiceTest(t *testing.T) (*service.OrderService, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.Order{},
		&model.Plan{},
	))

	// 创建上架套餐
	activePlan := &model.Plan{
		Name:         "活跃套餐",
		Price:        12.0,
		Currency:     "USDT",
		TrafficLimit: 1024 * 1024 * 1024 * 200,
		DurationDays: 30,
		IsActive:     true,
	}
	require.NoError(t, db.Create(activePlan).Error)

	// 创建下架套餐（先创建，然后通过 UPDATE 设为 false 确保 SQLite 正确存储）
	inactivePlan := &model.Plan{
		Name:         "下架套餐",
		Price:        8.0,
		Currency:     "USDT",
		TrafficLimit: 1024 * 1024 * 1024 * 100,
		DurationDays: 30,
		IsActive:     true,
	}
	require.NoError(t, db.Create(inactivePlan).Error)
	// 手动设为 false 绕过 GORM 默认值行为
	db.Model(&model.Plan{}).Where("id = ?", inactivePlan.ID).Update("is_active", false)

	// 验证下架套餐的状态
	var verifyPlan model.Plan
	require.NoError(t, db.Where("name = ?", "下架套餐").First(&verifyPlan).Error)
	assert.False(t, verifyPlan.IsActive)

	orderRepo := repository.NewOrderRepository(db)
	planRepo := repository.NewPlanRepository(db)
	return service.NewOrderService(orderRepo, planRepo), db
}

func TestOrderService_CreateOrder_Success(t *testing.T) {
	orderSvc, _ := setupOrderServiceTest(t)

	order, err := orderSvc.CreateOrder(context.Background(), 1, 1)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), order.PlanID)
	assert.Equal(t, "PENDING", order.Status)
	assert.Equal(t, 12.0, order.Amount)
	assert.Equal(t, "USDT", order.Currency)
	assert.True(t, order.ExpiredAt.After(time.Now()))
}

func TestOrderService_CreateOrder_PlanNotFound(t *testing.T) {
	orderSvc, _ := setupOrderServiceTest(t)

	_, err := orderSvc.CreateOrder(context.Background(), 1, 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "套餐不存在")
}

func TestOrderService_CreateOrder_PlanInactive(t *testing.T) {
	orderSvc, db := setupOrderServiceTest(t)

	// 查找下架套餐 ID
	var inactivePlan model.Plan
	require.NoError(t, db.Where("is_active = ?", false).First(&inactivePlan).Error)

	_, err := orderSvc.CreateOrder(context.Background(), 1, inactivePlan.ID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "套餐已下架")
}

func TestOrderService_ListUserOrders(t *testing.T) {
	orderSvc, db := setupOrderServiceTest(t)

	// 创建订单
	expiredAt := time.Now().Add(30 * time.Minute)
	order := &model.Order{
		UserID:    1,
		PlanID:    1,
		OrderNo:   "ORD-LIST-001",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &expiredAt,
	}
	require.NoError(t, db.Create(order).Error)

	orders, total, err := orderSvc.ListUserOrders(context.Background(), 1, 1, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, orders, 1)
	assert.Equal(t, "ORD-LIST-001", orders[0].OrderNo)
}

func TestOrderService_ExpirePendingOrders(t *testing.T) {
	orderSvc, db := setupOrderServiceTest(t)

	// 创建一个已过期的 PENDING 订单
	expiredAt := time.Now().Add(-1 * time.Hour)
	order := &model.Order{
		UserID:    1,
		PlanID:    1,
		OrderNo:   "ORD-EXPIRE-001",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &expiredAt,
	}
	require.NoError(t, db.Create(order).Error)

	// 创建一个未到期的 PENDING 订单
	futureAt := time.Now().Add(1 * time.Hour)
	order2 := &model.Order{
		UserID:    2,
		PlanID:    1,
		OrderNo:   "ORD-EXPIRE-002",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &futureAt,
	}
	require.NoError(t, db.Create(order2).Error)

	// 运行过期扫描
	count, err := orderSvc.ExpirePendingOrders(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// 验证第一个订单已过期
	var checkOrder1 model.Order
	db.Where("order_no = ?", "ORD-EXPIRE-001").First(&checkOrder1)
	assert.Equal(t, "EXPIRED", checkOrder1.Status)

	// 验证第二个订单仍为 PENDING
	var checkOrder2 model.Order
	db.Where("order_no = ?", "ORD-EXPIRE-002").First(&checkOrder2)
	assert.Equal(t, "PENDING", checkOrder2.Status)
}
