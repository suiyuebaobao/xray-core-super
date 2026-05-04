// cmd/api/main.go — 后端 API 服务入口。
//
// 功能：
// - 启动 Gin HTTP 服务，提供 REST API 和订阅下载接口
// - 连接 MySQL 数据库
// - 注册所有路由（认证、用户、套餐、节点、订阅、管理、兑换码、订单）
// - 加载中间件（鉴权、日志、CORS）
//
// 启动方式：
//
//	go run ./cmd/api
//
// 或设置环境变量后启动：
//
//	DATABASE_URL="..." JWT_SECRET="..." go run ./cmd/api
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"suiyue/internal/config"
	"suiyue/internal/handler"
	"suiyue/internal/middleware"
	"suiyue/internal/platform/database"
	"suiyue/internal/repository"
	"suiyue/internal/service"
	"suiyue/internal/subscription"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg := config.Load()
	cfg.Validate()

	// 连接数据库
	db := database.New(cfg.DatabaseURL, cfg.LogLevel)
	log.Println("[api] database connected")

	// 创建 Repository
	userRepo := repository.NewUserRepository(db)
	refreshRepo := repository.NewRefreshTokenRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeGroupRepo := repository.NewNodeGroupRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	nodeHostRepo := repository.NewNodeHostRepository(db)
	taskRepo := repository.NewNodeAccessTaskRepository(db)
	relayRepo := repository.NewRelayRepository(db)
	relayBackendRepo := repository.NewRelayBackendRepository(db)
	relayTaskRepo := repository.NewRelayConfigTaskRepository(db)
	relayTrafficRepo := repository.NewRelayTrafficSnapshotRepository(db)
	usageLedgerRepo := repository.NewUsageLedgerRepository(db)

	// 创建 Service
	authSvc := service.NewAuthService(userRepo, refreshRepo, cfg)
	userSvc := service.NewUserService(userRepo, subRepo, tokenRepo, cfg)
	planSvc := service.NewPlanService(planRepo)
	nodeAccessSvc := service.NewNodeAccessService(taskRepo, nodeRepo, planRepo, subRepo, userRepo, cfg)
	relaySvc := service.NewRelayService(relayRepo, relayBackendRepo, relayTaskRepo, nodeRepo, cfg.TaskRetryLimit)
	relayTrafficSvc := service.NewRelayTrafficService(relayTrafficRepo)
	orderRepo := repository.NewOrderRepository(db)
	orderSvc := service.NewOrderServiceWithExpireDuration(orderRepo, planRepo, cfg.OrderExpireDuration)

	// 创建订阅生成器
	subGen := subscription.NewGenerator(subRepo, tokenRepo, planRepo, nodeRepo, userRepo, relayBackendRepo)

	// 创建流量采集服务
	trafficSvc := service.NewTrafficService(
		db,
		repository.NewTrafficSnapshotRepository(db),
		usageLedgerRepo,
		subRepo,
		nodeRepo,
		userRepo,
		nodeAccessSvc,
	)

	// 创建 node-agent 通信处理器
	agentHandler := handler.NewAgentHandlerWithRelayAndNodeHosts(nodeAccessSvc, trafficSvc, nodeRepo, nodeHostRepo, relaySvc, relayTrafficSvc, relayRepo)

	// 创建订阅 Token Handler
	subTokenHandler := handler.NewAdminSubscriptionTokenHandler(
		tokenRepo,
		subRepo,
		userRepo,
		planRepo,
		nodeRepo,
		nodeAccessSvc,
	)

	// 创建 Handler
	authHandler := handler.NewAuthHandler(authSvc)
	userHandler := handler.NewUserHandler(userSvc, tokenRepo)
	planHandler := handler.NewPlanHandler(planSvc)
	subHandler := handler.NewSubHandler(subGen)
	orderHandler := handler.NewOrderHandler(orderSvc)
	usageHandler := handler.NewUsageHandler(usageLedgerRepo, userRepo, subRepo, planRepo)

	// 管理后台 Handler
	adminDashboardHandler := handler.NewAdminDashboardHandler(userRepo, nodeRepo, planRepo, subRepo)
	adminPlanHandler := handler.NewAdminPlanHandlerWithSync(planRepo, nodeGroupRepo, nodeAccessSvc)
	adminNodeGroupHandler := handler.NewAdminNodeGroupHandlerWithNodes(nodeGroupRepo, nodeRepo, nodeAccessSvc)
	adminNodeHandler := handler.NewAdminNodeHandlerWithSync(nodeRepo, subRepo, nodeAccessSvc)
	adminRelayHandler := handler.NewAdminRelayHandler(relayRepo, relayBackendRepo, relaySvc)
	adminUserHandler := handler.NewAdminUserHandlerWithSubscription(userRepo, subRepo, tokenRepo, planRepo, nodeAccessSvc, cfg.BCryptRounds, cfg.XrayUserKeyDomain)
	adminOrderHandler := handler.NewAdminOrderHandler(orderRepo)
	adminPlanNodeGroupHandler := handler.NewPlanNodeGroupHandlerWithSync(planRepo, subRepo, nodeAccessSvc)
	nodeDeploySvc := service.NewNodeDeployService(nodeRepo, nodeHostRepo)
	nodeDeployHandler := handler.NewNodeDeployHandler(nodeDeploySvc)
	relayDeploySvc := service.NewRelayDeployService(relayRepo)
	relayDeployHandler := handler.NewRelayDeployHandler(relayDeploySvc)

	// 设置 Gin 模式
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// 注册中间件
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Logger())
	r.Use(middleware.CORS()) // 开发环境跨域，生产由 Nginx 处理

	// 注册路由

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"version": "v1.0.0",
		})
	})

	// 认证路由（公开）
	authGroup := r.Group("/api/auth")
	authGroup.Use(middleware.CSRF())
	authLimiter := middleware.RateLimit(20, time.Minute)
	{
		authGroup.POST("/register", authLimiter, authHandler.Register)
		authGroup.POST("/login", authLimiter, authHandler.Login)
		authGroup.POST("/refresh", authHandler.Refresh)
		authGroup.POST("/logout", authHandler.Logout)
	}

	// 用户中心（需登录）
	userGroup := r.Group("/api/user")
	userGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.CSRF())
	{
		userGroup.GET("/me", userHandler.GetMe)
		userGroup.GET("/subscription", userHandler.GetSubscription)
		userGroup.GET("/usage", usageHandler.GetCurrentUserUsage)
		userGroup.GET("/orders", orderHandler.List)
		userGroup.PUT("/profile", userHandler.UpdateProfile)
		userGroup.PUT("/password", userHandler.ChangePassword)
	}

	// 订单创建（需登录）
	r.POST("/api/orders", middleware.JWTAuth(cfg.JWTSecret), middleware.CSRF(), orderHandler.Create)

	// 套餐（公开）
	r.GET("/api/plans", planHandler.ListActive)

	// 订阅下载（公开，通过 token 鉴权）
	r.GET("/sub/:token/:format", subHandler.Download)

	// 兑换码（用户侧，需登录）
	redeemGroup := r.Group("/api")
	redeemGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.CSRF())
	{
		redeemGroup.POST("/redeem", handler.NewRedeemHandler(repository.NewRedeemCodeRepository(db), subRepo, planRepo, tokenRepo, nodeAccessSvc).Redeem)
	}

	// node-agent 通信接口（使用独立 token 鉴权）
	agentGroup := r.Group("/api/agent")
	{
		agentGroup.POST("/heartbeat", agentHandler.Heartbeat)
		agentGroup.POST("/task-result", agentHandler.TaskResult)
		agentGroup.POST("/traffic", agentHandler.TrafficReport)
		agentGroup.POST("/multi/heartbeat", agentHandler.MultiHeartbeat)
		agentGroup.POST("/multi/task-result", agentHandler.MultiTaskResult)
		agentGroup.POST("/multi/traffic", agentHandler.MultiTrafficReport)
		agentGroup.POST("/relay/heartbeat", agentHandler.RelayHeartbeat)
		agentGroup.POST("/relay/task-result", agentHandler.RelayTaskResult)
		agentGroup.POST("/relay/traffic", agentHandler.RelayTrafficReport)
	}

	// 管理后台路由（需登录 + 管理员权限）
	adminGroup := r.Group("/api/admin")
	adminGroup.Use(middleware.JWTAuth(cfg.JWTSecret), middleware.RequireAdmin(), middleware.CSRF())
	{
		// 仪表盘
		adminGroup.GET("/dashboard/stats", adminDashboardHandler.Stats)

		// 套餐管理
		adminGroup.GET("/plans", adminPlanHandler.List)
		adminGroup.POST("/plans", adminPlanHandler.Create)
		adminGroup.PUT("/plans/:id", adminPlanHandler.Update)
		adminGroup.DELETE("/plans/:id", adminPlanHandler.Delete)

		// 节点分组管理
		adminGroup.GET("/node-groups", adminNodeGroupHandler.List)
		adminGroup.POST("/node-groups", adminNodeGroupHandler.Create)
		adminGroup.PUT("/node-groups/:id", adminNodeGroupHandler.Update)
		adminGroup.DELETE("/node-groups/:id", adminNodeGroupHandler.Delete)
		adminGroup.GET("/node-groups/:id/nodes", adminNodeGroupHandler.ListNodes)
		adminGroup.PUT("/node-groups/:id/nodes", adminNodeGroupHandler.BindNodes)

		// 节点管理
		adminGroup.GET("/nodes", adminNodeHandler.List)
		adminGroup.POST("/nodes", adminNodeHandler.Create)
		adminGroup.PUT("/nodes/:id", adminNodeHandler.Update)
		adminGroup.DELETE("/nodes/:id", adminNodeHandler.Delete)
		adminGroup.POST("/nodes/deploy/scan-ips", nodeDeployHandler.ScanIPs)
		adminGroup.POST("/nodes/deploy", nodeDeployHandler.Deploy)

		// 中转节点管理
		adminGroup.GET("/relays", adminRelayHandler.List)
		adminGroup.POST("/relays", adminRelayHandler.Create)
		adminGroup.PUT("/relays/:id", adminRelayHandler.Update)
		adminGroup.DELETE("/relays/:id", adminRelayHandler.Delete)
		adminGroup.POST("/relays/deploy", relayDeployHandler.Deploy)
		adminGroup.GET("/relays/:id/backends", adminRelayHandler.ListBackends)
		adminGroup.PUT("/relays/:id/backends", adminRelayHandler.SaveBackends)

		// 用户管理
		adminGroup.GET("/users", adminUserHandler.List)
		adminGroup.POST("/users", adminUserHandler.Create)
		adminGroup.DELETE("/users/:id", adminUserHandler.Delete)
		adminGroup.PUT("/users/:id/status", adminUserHandler.ToggleStatus)
		adminGroup.PUT("/users/:id/password", adminUserHandler.ResetPassword)
		adminGroup.GET("/users/:id/subscription", adminUserHandler.GetSubscription)
		adminGroup.PUT("/users/:id/subscription", adminUserHandler.UpsertSubscription)
		adminGroup.GET("/users/:id/usage", usageHandler.GetAdminUserUsage)

		// 套餐-节点组绑定
		adminGroup.POST("/plans/:id/node-groups", adminPlanNodeGroupHandler.BindNodeGroups)
		adminGroup.GET("/plans/:id/node-groups", adminPlanNodeGroupHandler.ListNodeGroups)

		// 订单管理
		adminGroup.GET("/orders", adminOrderHandler.List)

		// 兑换码管理
		adminGroup.GET("/redeem-codes", handler.NewAdminRedeemHandler(repository.NewRedeemCodeRepository(db)).List)
		adminGroup.POST("/redeem-codes", handler.NewAdminRedeemHandler(repository.NewRedeemCodeRepository(db)).Generate)

		// 订阅 Token 管理
		adminGroup.GET("/subscription-tokens", subTokenHandler.ListTokens)
		adminGroup.POST("/subscription-tokens", subTokenHandler.CreateToken)
		adminGroup.POST("/subscription-tokens/:id/revoke", subTokenHandler.RevokeToken)
		adminGroup.POST("/subscription-tokens/:id/reset", subTokenHandler.ResetToken)
	}

	// TODO: 注册更多路由（后续阶段）

	// 启动服务
	addr := fmt.Sprintf(":%d", cfg.AppPort)
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("[api] starting server on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[api] server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	log.Println("[api] shutting down...")
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("[api] server failed: %v", err)
	}
}
