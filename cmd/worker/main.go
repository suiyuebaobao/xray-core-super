// worker/main.go — 后台 Worker 服务入口。
//
// 功能：
// - 启动后运行所有后台定时任务
// - 订阅过期扫描
// - 订单过期扫描（v1 保留骨架）
// - 流量同步任务（由 node-agent 主动上报触发，服务端侧无需定时拉取）
//
// 启动方式：
//
//	go run ./cmd/worker
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"suiyue/internal/config"
	"suiyue/internal/platform/database"
	"suiyue/internal/repository"
	"suiyue/internal/scheduler"
	"suiyue/internal/service"

	"gorm.io/gorm"
)

func main() {
	cfg := config.Load()
	cfg.Validate()

	// 连接数据库
	db := database.New(cfg.DatabaseURL, cfg.LogLevel)
	log.Println("[worker] database connected")

	// 创建 Repository
	subRepo := repository.NewSubscriptionRepository(db)
	nodeAccessTaskRepo := repository.NewNodeAccessTaskRepository(db)
	relayConfigTaskRepo := repository.NewRelayConfigTaskRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	planRepo := repository.NewPlanRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	orderRepo := repository.NewOrderRepository(db)
	userRepo := repository.NewUserRepository(db)

	// 创建 NodeAccessService（用于创建节点访问任务）
	nodeAccessSvc := service.NewNodeAccessService(nodeAccessTaskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg)

	// 创建 OrderService（用于订单过期扫描）
	orderSvc := service.NewOrderService(orderRepo, planRepo)

	// 创建调度器
	s := scheduler.New()

	// 定时任务：扫描过期订阅（每分钟）
	s.AddJob("@every 60s", "scan_expired_subscriptions", func() {
		scanExpiredSubscriptions(db, subRepo, nodeAccessSvc)
	})

	// 定时任务：订单过期扫描（每分钟）
	s.AddJob("@every 60s", "scan_expired_orders", func() {
		ctx := context.Background()
		count, err := orderSvc.ExpirePendingOrders(ctx)
		if err != nil {
			log.Printf("[worker] scan_expired_orders error: %v", err)
			return
		}
		if count > 0 {
			log.Printf("[worker] scan_expired_orders expired %d orders", count)
		}
	})

	// 定时任务：清理过期 Refresh Token（每小时）
	s.AddJob("@every 1h", "cleanup_refresh_tokens", func() {
		ctx := context.Background()
		if err := refreshRepo.DeleteExpired(ctx); err != nil {
			log.Printf("[worker] cleanup_refresh_tokens error: %v", err)
			return
		}
		log.Println("[worker] cleanup_refresh_tokens completed")
	})

	// 定时任务：节点访问任务重试（每 5 分钟）
	s.AddJob("@every 5m", "retry_failed_node_tasks", func() {
		ctx := context.Background()
		count, err := nodeAccessTaskRepo.RetryFailedAndStaleTasks(ctx, cfg.TaskRetryLimit, cfg.TaskLockTTL)
		if err != nil {
			log.Printf("[worker] retry_failed_node_tasks error: %v", err)
			return
		}
		if count > 0 {
			log.Printf("[worker] retry_failed_node_tasks requeued %d tasks", count)
		}
	})

	// 定时任务：中转配置任务重试（每 5 分钟）
	s.AddJob("@every 5m", "retry_failed_relay_tasks", func() {
		ctx := context.Background()
		count, err := relayConfigTaskRepo.RetryFailedAndStaleTasks(ctx, cfg.TaskRetryLimit, cfg.TaskLockTTL)
		if err != nil {
			log.Printf("[worker] retry_failed_relay_tasks error: %v", err)
			return
		}
		if count > 0 {
			log.Printf("[worker] retry_failed_relay_tasks requeued %d tasks", count)
		}
	})

	s.Start()

	// 等待终止信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[worker] shutting down...")
	s.Stop()
}

// scanExpiredSubscriptions 扫描并处理过期订阅。
func scanExpiredSubscriptions(db *gorm.DB, subRepo *repository.SubscriptionRepository, nodeAccessSvc *service.NodeAccessService) {
	ctx := context.Background()

	// 查找过期但状态仍为 ACTIVE 的订阅
	var subs []struct {
		ID     uint64
		UserID uint64
		PlanID uint64
	}
	err := db.WithContext(ctx).
		Table("user_subscriptions").
		Select("id, user_id, plan_id").
		Where("status = ? AND expire_date < ?", "ACTIVE", time.Now()).
		Find(&subs).Error
	if err != nil {
		log.Printf("[worker] scan_expired_subscriptions query error: %v", err)
		return
	}

	if len(subs) == 0 {
		return
	}

	log.Printf("[worker] found %d expired subscriptions to process", len(subs))

	for _, sub := range subs {
		// 更新订阅状态为 EXPIRED
		if err := subRepo.UpdateStatus(ctx, sub.ID, "EXPIRED"); err != nil {
			log.Printf("[worker] failed to update status for sub %d: %v", sub.ID, err)
			continue
		}

		// 创建 DISABLE_USER 任务通知节点
		if err := nodeAccessSvc.TriggerOnExpire(ctx, sub.UserID, sub.ID, sub.PlanID); err != nil {
			log.Printf("[worker] failed to create disable tasks for sub %d: %v", sub.ID, err)
		}

		log.Printf("[worker] processed expired subscription %d for user %d", sub.ID, sub.UserID)
	}
}
