// traffic_service.go — 流量采集与计费服务。
//
// 职责：
// - 处理 node-agent 上报的流量数据
// - 快照差值法计算增量流量
// - 更新用户订阅使用量
// - 检测配额超限并触发节点禁用
//
// 流量统计必须按"快照差值"计算，避免重复计费。
package service

import (
	"context"
	"fmt"
	"log"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"

	"gorm.io/gorm"
)

// TrafficService 流量采集服务。
type TrafficService struct {
	snapshotRepo  *repository.TrafficSnapshotRepository
	ledgerRepo    *repository.UsageLedgerRepository
	subRepo       *repository.SubscriptionRepository
	nodeRepo      *repository.NodeRepository
	userRepo      *repository.UserRepository
	nodeAccessSvc *NodeAccessService
	db            *gorm.DB
}

type quotaTrigger struct {
	action string
	userID uint64
	subID  uint64
	planID uint64
}

// NewTrafficService 创建流量采集服务。
func NewTrafficService(db *gorm.DB, snapshotRepo *repository.TrafficSnapshotRepository, ledgerRepo *repository.UsageLedgerRepository, subRepo *repository.SubscriptionRepository, nodeRepo *repository.NodeRepository, userRepo *repository.UserRepository, nodeAccessSvc *NodeAccessService) *TrafficService {
	return &TrafficService{
		db:            db,
		snapshotRepo:  snapshotRepo,
		ledgerRepo:    ledgerRepo,
		subRepo:       subRepo,
		nodeRepo:      nodeRepo,
		userRepo:      userRepo,
		nodeAccessSvc: nodeAccessSvc,
	}
}

// TrafficItem 流量上报项。
type TrafficItem struct {
	XrayUserKey   string `json:"xray_user_key"`
	UplinkTotal   uint64 `json:"uplink_total"`
	DownlinkTotal uint64 `json:"downlink_total"`
}

// TrafficReportOptions 控制一次流量上报的元信息。
type TrafficReportOptions struct {
	CollectedAt time.Time
}

// ProcessTrafficReport 处理 node-agent 上报的流量数据。
// 返回处理失败的项数量及第一个错误。
func (s *TrafficService) ProcessTrafficReport(ctx context.Context, nodeID uint64, items []TrafficItem) error {
	return s.ProcessTrafficReportWithOptions(ctx, nodeID, items, TrafficReportOptions{})
}

// ProcessTrafficReportWithOptions 处理 node-agent 上报的流量数据。
func (s *TrafficService) ProcessTrafficReportWithOptions(ctx context.Context, nodeID uint64, items []TrafficItem, opts TrafficReportOptions) error {
	capturedAt := opts.CollectedAt
	if capturedAt.IsZero() {
		capturedAt = time.Now()
	}

	var firstErr error
	failed := 0
	for _, item := range items {
		if err := s.processSingleUser(ctx, nodeID, item, capturedAt); err != nil {
			failed++
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	if firstErr != nil {
		return fmt.Errorf("processed %d/%d items, first error: %w", len(items)-failed, len(items), firstErr)
	}
	return nil
}

// processSingleUser 处理单个用户的流量上报。
func (s *TrafficService) processSingleUser(ctx context.Context, nodeID uint64, item TrafficItem, capturedAt time.Time) error {
	// 1. 查找用户
	user, err := s.userRepo.FindByXrayUserKey(ctx, item.XrayUserKey)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // 未知用户，跳过
		}
		return fmt.Errorf("find user by key %s: %w", item.XrayUserKey, err)
	}

	// 2. 查找用户当前有效订阅
	sub, err := s.subRepo.FindActiveByUserID(ctx, user.ID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil // 无有效订阅，跳过
		}
		return fmt.Errorf("find active sub for user %d: %w", user.ID, err)
	}

	// 3. 读取上一次快照
	lastSnapshot, err := s.snapshotRepo.FindLatest(ctx, nodeID, item.XrayUserKey)
	if err != nil {
		// 无历史快照，创建基线
		if createErr := s.snapshotRepo.Create(ctx, &model.TrafficSnapshot{
			NodeID:        nodeID,
			XrayUserKey:   item.XrayUserKey,
			UplinkTotal:   item.UplinkTotal,
			DownlinkTotal: item.DownlinkTotal,
			CapturedAt:    capturedAt,
		}); createErr != nil {
			return fmt.Errorf("create baseline snapshot: %w", createErr)
		}
		return nil
	}
	if !capturedAt.After(lastSnapshot.CapturedAt) {
		return nil
	}

	// 4. 计算增量
	deltaUp := calculateDelta(lastSnapshot.UplinkTotal, item.UplinkTotal)
	deltaDown := calculateDelta(lastSnapshot.DownlinkTotal, item.DownlinkTotal)
	deltaTotal := deltaUp + deltaDown

	// 无论增量是否为 0，都要写入新快照（保持基线最新）
	if err := s.db.WithContext(ctx).Create(&model.TrafficSnapshot{
		NodeID:        nodeID,
		XrayUserKey:   item.XrayUserKey,
		UplinkTotal:   item.UplinkTotal,
		DownlinkTotal: item.DownlinkTotal,
		CapturedAt:    capturedAt,
	}).Error; err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}

	if deltaTotal == 0 {
		return nil
	}

	// 5. 事务：写入账本 + 原子累加订阅使用量 + 标记超限状态
	var trigger *quotaTrigger
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 写入账本
		if err := tx.Create(&model.UsageLedger{
			UserID:         user.ID,
			SubscriptionID: &sub.ID,
			NodeID:         nodeID,
			DeltaUpload:    deltaUp,
			DeltaDownload:  deltaDown,
			DeltaTotal:     deltaTotal,
			RecordedAt:     capturedAt,
		}).Error; err != nil {
			return fmt.Errorf("create ledger: %w", err)
		}

		// 原子累加订阅使用量，避免并发上报后写覆盖先写。
		if err := tx.Model(&model.UserSubscription{}).
			Where("id = ?", sub.ID).
			Update("used_traffic", gorm.Expr("used_traffic + ?", deltaTotal)).Error; err != nil {
			return fmt.Errorf("update used_traffic: %w", err)
		}

		var updatedSub model.UserSubscription
		if err := tx.First(&updatedSub, sub.ID).Error; err != nil {
			return fmt.Errorf("reload subscription: %w", err)
		}

		t, err := s.markOverQuotaTx(tx, &updatedSub)
		if err != nil {
			return err
		}
		trigger = t

		return nil
	}); err != nil {
		return err
	}

	if trigger != nil {
		if err := s.runQuotaTrigger(ctx, trigger); err != nil {
			log.Printf("[traffic] quota trigger for sub %d failed: %v", trigger.subID, err)
		}
	}

	return nil
}

// CalculateDelta 公开方法：计算增量，处理节点重启导致计数器归零的情况。
func CalculateDelta(old, new uint64) uint64 {
	if new < old {
		return 0
	}
	return new - old
}

// calculateDelta 内部使用的增量计算方法。
func calculateDelta(old, new uint64) uint64 {
	return CalculateDelta(old, new)
}

func (s *TrafficService) markOverQuotaTx(tx *gorm.DB, sub *model.UserSubscription) (*quotaTrigger, error) {
	if sub.TrafficLimit > 0 && sub.UsedTraffic >= sub.TrafficLimit {
		if err := tx.Model(&model.UserSubscription{}).
			Where("id = ?", sub.ID).
			Updates(map[string]interface{}{
				"status":         "SUSPENDED",
				"active_user_id": nil,
			}).Error; err != nil {
			return nil, fmt.Errorf("update subscription %d status to SUSPENDED: %w", sub.ID, err)
		}
		return &quotaTrigger{action: "over_traffic", userID: sub.UserID, subID: sub.ID, planID: sub.PlanID}, nil
	}

	if time.Now().After(sub.ExpireDate) {
		if err := tx.Model(&model.UserSubscription{}).
			Where("id = ?", sub.ID).
			Updates(map[string]interface{}{
				"status":         "EXPIRED",
				"active_user_id": nil,
			}).Error; err != nil {
			return nil, fmt.Errorf("update subscription %d status to EXPIRED: %w", sub.ID, err)
		}
		return &quotaTrigger{action: "expire", userID: sub.UserID, subID: sub.ID, planID: sub.PlanID}, nil
	}

	return nil, nil
}

func (s *TrafficService) runQuotaTrigger(ctx context.Context, trigger *quotaTrigger) error {
	if s.nodeAccessSvc == nil {
		return nil
	}
	switch trigger.action {
	case "over_traffic":
		return s.nodeAccessSvc.TriggerOnOverTraffic(ctx, trigger.userID, trigger.subID, trigger.planID)
	case "expire":
		return s.nodeAccessSvc.TriggerOnExpire(ctx, trigger.userID, trigger.subID, trigger.planID)
	default:
		return nil
	}
}

// CheckAndHandleOverQuota 检查订阅是否超限。
func (s *TrafficService) CheckAndHandleOverQuota(ctx context.Context, sub *model.UserSubscription) error {
	if sub.TrafficLimit > 0 && sub.UsedTraffic >= sub.TrafficLimit {
		if err := s.subRepo.UpdateStatus(ctx, sub.ID, "SUSPENDED"); err != nil {
			log.Printf("[traffic] update subscription %d status to SUSPENDED failed: %v", sub.ID, err)
		}
		if err := s.nodeAccessSvc.TriggerOnOverTraffic(ctx, sub.UserID, sub.ID, sub.PlanID); err != nil {
			log.Printf("[traffic] trigger over quota tasks for sub %d failed: %v", sub.ID, err)
			return fmt.Errorf("trigger over quota: %w", err)
		}
		return nil
	}

	if time.Now().After(sub.ExpireDate) {
		if err := s.subRepo.UpdateStatus(ctx, sub.ID, "EXPIRED"); err != nil {
			log.Printf("[traffic] update subscription %d status to EXPIRED failed: %v", sub.ID, err)
		}
		if err := s.nodeAccessSvc.TriggerOnExpire(ctx, sub.UserID, sub.ID, sub.PlanID); err != nil {
			log.Printf("[traffic] trigger expire tasks for sub %d failed: %v", sub.ID, err)
			return fmt.Errorf("trigger expire: %w", err)
		}
		return nil
	}

	return nil
}
