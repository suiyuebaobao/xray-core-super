// order_service.go — 订单业务逻辑层。
//
// 职责：
// - 创建订单（校验套餐、生成订单号、写入数据库）
// - 查询用户订单列表
// - 订单过期扫描与状态更新
package service

import (
	"context"
	"fmt"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/platform/secure"
	"suiyue/internal/repository"
)

// OrderService 订单服务。
type OrderService struct {
	orderRepo      *repository.OrderRepository
	planRepo       *repository.PlanRepository
	expireDuration time.Duration
}

// NewOrderService 创建订单服务。
func NewOrderService(orderRepo *repository.OrderRepository, planRepo *repository.PlanRepository) *OrderService {
	return NewOrderServiceWithExpireDuration(orderRepo, planRepo, 30*time.Minute)
}

// NewOrderServiceWithExpireDuration 创建可配置过期时间的订单服务。
func NewOrderServiceWithExpireDuration(orderRepo *repository.OrderRepository, planRepo *repository.PlanRepository, expireDuration time.Duration) *OrderService {
	if expireDuration <= 0 {
		expireDuration = 30 * time.Minute
	}
	return &OrderService{
		orderRepo:      orderRepo,
		planRepo:       planRepo,
		expireDuration: expireDuration,
	}
}

// CreateOrder 创建订单。
//
// 流程：
// 1. 校验套餐是否存在且已上架
// 2. 生成唯一订单号
// 3. 写入订单（状态 PENDING，30 分钟过期）
func (s *OrderService) CreateOrder(ctx context.Context, userID uint64, planID uint64) (*model.Order, error) {
	// 1. 校验套餐
	plan, err := s.planRepo.FindByID(ctx, planID)
	if err != nil {
		return nil, &response.AppError{
			Code:     40001,
			HTTPCode: 400,
			Message:  "套餐不存在",
		}
	}
	if !plan.IsActive {
		return nil, &response.AppError{
			Code:     40002,
			HTTPCode: 400,
			Message:  "套餐已下架",
		}
	}

	// 2. 生成订单号
	orderNo, err := generateOrderNo()
	if err != nil {
		return nil, response.ErrInternalServer
	}

	// 3. 设置过期时间
	expiredAt := time.Now().Add(s.expireDuration)

	order := &model.Order{
		UserID:        userID,
		PlanID:        planID,
		OrderNo:       orderNo,
		Amount:        plan.Price,
		Currency:      plan.Currency,
		Status:        "PENDING",
		ExpectedChain: "TRC20",
		ExpiredAt:     &expiredAt,
	}

	if err := s.orderRepo.Create(ctx, order); err != nil {
		return nil, response.ErrInternalServer
	}

	return order, nil
}

// ListUserOrders 分页查询用户订单列表。
func (s *OrderService) ListUserOrders(ctx context.Context, userID uint64, page, size int) ([]model.Order, int64, error) {
	return s.orderRepo.ListByUserID(ctx, userID, page, size)
}

// ExpirePendingOrders 扫描并过期超时的 PENDING 订单。
// 返回处理的订单数量。
func (s *OrderService) ExpirePendingOrders(ctx context.Context) (int, error) {
	count, err := s.orderRepo.ExpireByTime(ctx)
	if err != nil {
		return 0, fmt.Errorf("expire pending orders: %w", err)
	}
	return int(count), nil
}

// generateOrderNo 生成唯一订单号，格式：ORD-YYYYMMDDHHMMSS-随机串。
func generateOrderNo() (string, error) {
	now := time.Now().Format("20060102150405")
	suffix, err := secure.RandomHex(4)
	if err != nil {
		return "", err
	}
	return "ORD-" + now + "-" + suffix, nil
}
