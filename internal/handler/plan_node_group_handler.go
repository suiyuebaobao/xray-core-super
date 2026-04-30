// plan_node_group_handler.go — 套餐-节点分组关联处理器。
//
// 职责：
// - 为套餐绑定节点分组
// - 查询套餐关联的节点分组
package handler

import (
	"context"
	"log"
	"strconv"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"

	"github.com/gin-gonic/gin"
)

// PlanNodeGroupHandler 套餐-节点分组关联处理器。
type PlanNodeGroupHandler struct {
	planRepo      *repository.PlanRepository
	subRepo       *repository.SubscriptionRepository
	nodeAccessSvc planNodeGroupSyncer
}

// NewPlanNodeGroupHandler 创建套餐-节点分组关联处理器。
func NewPlanNodeGroupHandler(planRepo *repository.PlanRepository) *PlanNodeGroupHandler {
	return &PlanNodeGroupHandler{planRepo: planRepo}
}

// NewPlanNodeGroupHandlerWithSync 创建带已有订阅重同步能力的处理器。
func NewPlanNodeGroupHandlerWithSync(planRepo *repository.PlanRepository, subRepo *repository.SubscriptionRepository, nodeAccessSvc planNodeGroupSyncer) *PlanNodeGroupHandler {
	return &PlanNodeGroupHandler{planRepo: planRepo, subRepo: subRepo, nodeAccessSvc: nodeAccessSvc}
}

type planNodeGroupSyncer interface {
	TriggerOnRenew(ctx context.Context, userID, subID, planID uint64) error
	TriggerOnExpire(ctx context.Context, userID, subID, planID uint64) error
}

// BindNodeGroups 处理 POST /api/admin/plans/:id/node-groups — 为套餐绑定节点分组。
func (h *PlanNodeGroupHandler) BindNodeGroups(c *gin.Context) {
	planID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req struct {
		NodeGroupIDs []uint64 `json:"node_group_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	activeSubs := h.listActiveSubscriptions(c.Request.Context(), planID)
	h.syncPlanSubscriptions(c.Request.Context(), activeSubs, "DISABLE_USER")

	if err := h.planRepo.BindNodeGroups(c.Request.Context(), planID, req.NodeGroupIDs); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.syncPlanSubscriptions(c.Request.Context(), activeSubs, "UPSERT_USER")

	response.Success(c, gin.H{"message": "绑定成功", "node_group_ids": req.NodeGroupIDs})
}

// ListNodeGroups 处理 GET /api/admin/plans/:id/node-groups — 查询套餐关联的节点分组。
func (h *PlanNodeGroupHandler) ListNodeGroups(c *gin.Context) {
	planID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	groupIDs, err := h.planRepo.FindNodeGroupIDs(c.Request.Context(), planID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{"node_group_ids": groupIDs})
}

func (h *PlanNodeGroupHandler) listActiveSubscriptions(ctx context.Context, planID uint64) []model.UserSubscription {
	if h.subRepo == nil || h.nodeAccessSvc == nil {
		return nil
	}
	subs, err := h.subRepo.ListActiveByPlanID(ctx, planID)
	if err != nil {
		log.Printf("[admin] list active subscriptions for plan %d failed: %v", planID, err)
		return nil
	}
	return subs
}

func (h *PlanNodeGroupHandler) syncPlanSubscriptions(ctx context.Context, subs []model.UserSubscription, action string) {
	if h.nodeAccessSvc == nil || len(subs) == 0 {
		return
	}
	for _, sub := range subs {
		var err error
		if action == "DISABLE_USER" {
			err = h.nodeAccessSvc.TriggerOnExpire(ctx, sub.UserID, sub.ID, sub.PlanID)
		} else {
			err = h.nodeAccessSvc.TriggerOnRenew(ctx, sub.UserID, sub.ID, sub.PlanID)
		}
		if err != nil {
			log.Printf("[admin] sync plan node groups action=%s sub=%d failed: %v", action, sub.ID, err)
		}
	}
}
