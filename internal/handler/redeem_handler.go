// redeem_handler.go — 兑换码 HTTP 处理器。
//
// 职责：
// - 用户提交兑换码开通订阅（POST /api/redeem）
// - 后台生成兑换码（POST /api/admin/redeem-codes）
// - 后台查询兑换码列表（GET /api/admin/redeem-codes）
package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/platform/secure"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const redeemCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// RedeemHandler 兑换码用户侧处理器。
type RedeemHandler struct {
	redeemRepo    *repository.RedeemCodeRepository
	subRepo       *repository.SubscriptionRepository
	planRepo      *repository.PlanRepository
	tokenRepo     *repository.SubscriptionTokenRepository
	nodeAccessSvc *service.NodeAccessService
}

// NewRedeemHandler 创建兑换码处理器。
func NewRedeemHandler(redeemRepo *repository.RedeemCodeRepository, subRepo *repository.SubscriptionRepository, planRepo *repository.PlanRepository, tokenRepo *repository.SubscriptionTokenRepository, nodeAccessSvc *service.NodeAccessService) *RedeemHandler {
	return &RedeemHandler{
		redeemRepo:    redeemRepo,
		subRepo:       subRepo,
		planRepo:      planRepo,
		tokenRepo:     tokenRepo,
		nodeAccessSvc: nodeAccessSvc,
	}
}

// Redeem 处理 POST /api/redeem — 用户提交兑换码开通订阅。
func (h *RedeemHandler) Redeem(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	code, err := h.redeemRepo.FindByCode(c.Request.Context(), req.Code)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	if code.IsUsed {
		response.HandleError(c, &response.AppError{Code: 40006, HTTPCode: http.StatusBadRequest, Message: "兑换码已使用"})
		return
	}

	now := time.Now()
	if code.ExpiresAt != nil && !code.ExpiresAt.After(now) {
		response.HandleError(c, &response.AppError{Code: 40011, HTTPCode: http.StatusBadRequest, Message: "兑换码已过期"})
		return
	}

	// 获取当前用户 ID
	uid, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	// 查询套餐详情
	plan, err := h.planRepo.FindByID(c.Request.Context(), code.PlanID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	ctx := c.Request.Context()

	// 使用数据库事务保证原子性：先标记兑换码已使用，再更新订阅
	err = h.subRepo.WithTransaction(ctx, func(tx *gorm.DB) error {
		// 1. 先标记兑换码（带 is_used=false 条件，防止并发重复使用）
		result := tx.Model(&model.RedeemCode{}).
			Where("id = ? AND is_used = ? AND (expires_at IS NULL OR expires_at > ?)", code.ID, false, now).
			Updates(map[string]interface{}{
				"is_used":         true,
				"used_by_user_id": uid,
				"used_at":         now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("兑换码已使用或已过期")
		}

		// 2. 检查用户是否已有有效订阅
		var activeSub *model.UserSubscription
		if err := tx.Where("user_id = ? AND status = ? AND expire_date > ?", uid, "ACTIVE", now).
			Order("expire_date DESC").First(&model.UserSubscription{}).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
		} else {
			activeSub = &model.UserSubscription{}
			tx.Where("user_id = ? AND status = ? AND expire_date > ?", uid, "ACTIVE", now).
				Order("expire_date DESC").First(activeSub)
		}

		if activeSub != nil && activeSub.ID > 0 {
			// 已有有效订阅，在原到期时间基础上延长并叠加流量
			baseDate := activeSub.ExpireDate
			if baseDate.Before(now) {
				baseDate = now
			}
			newExpireDate := baseDate.AddDate(0, 0, int(code.DurationDays))
			if err := tx.Model(&model.UserSubscription{}).Where("id = ?", activeSub.ID).
				Updates(map[string]interface{}{
					"expire_date":   newExpireDate,
					"traffic_limit": gorm.Expr("traffic_limit + ?", plan.TrafficLimit),
					"plan_id":       code.PlanID,
				}).Error; err != nil {
				return err
			}
			if err := ensureValidSubscriptionTokenTx(tx, uid, activeSub.ID); err != nil {
				return err
			}
		} else {
			// 创建新订阅
			newSub := &model.UserSubscription{
				UserID:       uid,
				PlanID:       code.PlanID,
				StartDate:    now,
				ExpireDate:   now.AddDate(0, 0, int(code.DurationDays)),
				TrafficLimit: plan.TrafficLimit,
				UsedTraffic:  0,
				Status:       "ACTIVE",
				ActiveUserID: &uid,
			}
			if err := tx.Create(newSub).Error; err != nil {
				return err
			}

			// 生成订阅 Token（使用 tx 写入）
			if err := ensureValidSubscriptionTokenTx(tx, uid, newSub.ID); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		if err.Error() == "兑换码已使用或已过期" {
			response.HandleError(c, &response.AppError{Code: 40006, HTTPCode: http.StatusBadRequest, Message: "兑换码已使用或已过期"})
		} else {
			response.HandleError(c, response.ErrInternalServer)
		}
		return
	}

	// 通知节点更新（事务外异步执行）
	go func() {
		if h.nodeAccessSvc == nil {
			return
		}
		activeSub, sErr := h.subRepo.FindActiveByUserID(context.Background(), uid)
		if sErr == nil && activeSub != nil {
			if err := h.nodeAccessSvc.TriggerOnRenew(context.Background(), uid, activeSub.ID, code.PlanID); err != nil {
				log.Printf("[redeem] trigger node access renew failed: %v", err)
			}
		} else {
			// 可能是新订阅
			activeSub2, sErr2 := h.subRepo.FindActiveByUserID(context.Background(), uid)
			if sErr2 == nil && activeSub2 != nil {
				if err := h.nodeAccessSvc.TriggerOnSubscribe(context.Background(), uid, activeSub2.ID, code.PlanID); err != nil {
					log.Printf("[redeem] trigger node access subscribe failed: %v", err)
				}
			}
		}
	}()

	response.Success(c, gin.H{"message": "兑换成功"})
}

// AdminRedeemHandler 管理后台兑换码处理器。
type AdminRedeemHandler struct {
	redeemRepo *repository.RedeemCodeRepository
}

// NewAdminRedeemHandler 创建管理后台兑换码处理器。
func NewAdminRedeemHandler(redeemRepo *repository.RedeemCodeRepository) *AdminRedeemHandler {
	return &AdminRedeemHandler{redeemRepo: redeemRepo}
}

// Generate 处理 POST /api/admin/redeem-codes — 批量生成兑换码。
func (h *AdminRedeemHandler) Generate(c *gin.Context) {
	var req struct {
		PlanID       uint64 `json:"plan_id" binding:"required"`
		DurationDays uint32 `json:"duration_days" binding:"required"`
		Count        int    `json:"count" binding:"required,min=1,max=1000"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	codes := make([]string, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		code, err := secure.RandomString(16, redeemCodeCharset)
		if err != nil {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
		if err := h.redeemRepo.Create(c.Request.Context(), &model.RedeemCode{
			Code:         code,
			PlanID:       req.PlanID,
			DurationDays: req.DurationDays,
		}); err != nil {
			// 冲突或 DB 错误，跳过
			continue
		}
		codes = append(codes, code)
	}

	response.Success(c, gin.H{"codes": codes, "count": len(codes)})
}

// List 处理 GET /api/admin/redeem-codes — 兑换码列表。
func (h *AdminRedeemHandler) List(c *gin.Context) {
	page, size := parsePagination(c)

	codes, total, err := h.redeemRepo.List(c.Request.Context(), page, size)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{
		"codes": codes,
		"total": total,
		"page":  page,
		"size":  size,
	})
}
