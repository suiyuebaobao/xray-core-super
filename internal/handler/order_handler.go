// order_handler.go — 订单 HTTP 处理器。
//
// 职责：
// - 用户创建订单（POST /api/orders）
// - 用户查询订单列表（GET /api/user/orders）
// - 管理后台查询订单（GET /api/admin/orders）
package handler

import (
	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

// OrderHandler 订单用户侧处理器。
type OrderHandler struct {
	orderSvc        *service.OrderService
	operationLogSvc *service.OperationLogService
}

// NewOrderHandler 创建订单处理器。
func NewOrderHandler(orderSvc *service.OrderService, operationLogSvc ...*service.OperationLogService) *OrderHandler {
	var logSvc *service.OperationLogService
	if len(operationLogSvc) > 0 {
		logSvc = operationLogSvc[0]
	}
	return &OrderHandler{orderSvc: orderSvc, operationLogSvc: logSvc}
}

// Create 处理 POST /api/orders — 创建订单。
func (h *OrderHandler) Create(c *gin.Context) {
	var req model.CreateOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	order, err := h.orderSvc.CreateOrder(c.Request.Context(), userID, req.PlanID)
	if err != nil {
		h.recordOrderOperation(c, "create_order", "failed", "用户创建订单失败", nil, map[string]interface{}{"plan_id": req.PlanID})
		response.HandleError(c, err)
		return
	}

	h.recordOrderOperation(c, "create_order", "success", "用户创建订单成功", &order.ID, map[string]interface{}{"plan_id": req.PlanID})
	response.Success(c, gin.H{"order": order})
}

// List 处理 GET /api/user/orders — 用户订单列表。
func (h *OrderHandler) List(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	page, size := parsePagination(c)

	orders, total, err := h.orderSvc.ListUserOrders(c.Request.Context(), userID, page, size)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{
		"orders": orders,
		"total":  total,
		"page":   page,
		"size":   size,
	})
}

func (h *OrderHandler) recordOrderOperation(c *gin.Context, action, result, summary string, targetID *uint64, extra interface{}) {
	if h == nil || h.operationLogSvc == nil {
		return
	}
	ctx := buildClientLogContext(c)
	targetType := "order"
	_ = h.operationLogSvc.Record(c.Request.Context(), ctx, "user", action, result, summary, &targetType, targetID, extra)
}

// AdminOrderHandler 管理后台订单处理器。
type AdminOrderHandler struct {
	orderRepo *repository.OrderRepository
}

// NewAdminOrderHandler 创建管理后台订单处理器。
func NewAdminOrderHandler(orderRepo *repository.OrderRepository) *AdminOrderHandler {
	return &AdminOrderHandler{orderRepo: orderRepo}
}

// List 处理 GET /api/admin/orders — 管理后台订单列表。
func (h *AdminOrderHandler) List(c *gin.Context) {
	page, size := parsePagination(c)

	orders, total, err := h.orderRepo.ListAll(c.Request.Context(), page, size)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{
		"orders": orders,
		"total":  total,
		"page":   page,
		"size":   size,
	})
}
