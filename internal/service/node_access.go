// Package service 提供业务逻辑层。
//
// NodeAccessService 管理 NodeAccessTask 的创建和执行。
// 当订阅状态发生变化（开通、续费、到期、超额）时，
// 为目标节点写入 NodeAccessTask，由 agent 主动拉取执行。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"suiyue/internal/config"
	"suiyue/internal/model"
	"suiyue/internal/platform/secure"
	"suiyue/internal/repository"
)

// NodeAccessService 节点访问同步服务。
type NodeAccessService struct {
	taskRepo *repository.NodeAccessTaskRepository
	nodeRepo *repository.NodeRepository
	planRepo *repository.PlanRepository
	subRepo  *repository.SubscriptionRepository
	userRepo *repository.UserRepository
	cfg      *config.Config
}

// NewNodeAccessService 创建节点访问同步服务。
func NewNodeAccessService(taskRepo *repository.NodeAccessTaskRepository, nodeRepo *repository.NodeRepository, planRepo *repository.PlanRepository, subRepo *repository.SubscriptionRepository, userRepo *repository.UserRepository, cfg *config.Config) *NodeAccessService {
	return &NodeAccessService{
		taskRepo: taskRepo,
		nodeRepo: nodeRepo,
		planRepo: planRepo,
		subRepo:  subRepo,
		userRepo: userRepo,
		cfg:      cfg,
	}
}

// TriggerOnSubscribe 订阅开通时触发节点同步。
func (s *NodeAccessService) TriggerOnSubscribe(ctx context.Context, userID uint64, subID uint64, planID uint64) error {
	return s.createTasksForSubscription(ctx, userID, subID, planID, "UPSERT_USER")
}

// TriggerOnRenew 订阅续费时触发节点同步。
func (s *NodeAccessService) TriggerOnRenew(ctx context.Context, userID uint64, subID uint64, planID uint64) error {
	return s.createTasksForSubscription(ctx, userID, subID, planID, "UPSERT_USER")
}

// TriggerOnExpire 订阅到期时触发节点禁用。
func (s *NodeAccessService) TriggerOnExpire(ctx context.Context, userID uint64, subID uint64, planID uint64) error {
	return s.createTasksForSubscription(ctx, userID, subID, planID, "DISABLE_USER")
}

// TriggerOnOverTraffic 订阅超额时触发节点禁用。
func (s *NodeAccessService) TriggerOnOverTraffic(ctx context.Context, userID uint64, subID uint64, planID uint64) error {
	return s.createTasksForSubscription(ctx, userID, subID, planID, "DISABLE_USER")
}

// TriggerForNode 为单个节点创建指定订阅的访问任务。
func (s *NodeAccessService) TriggerForNode(ctx context.Context, nodeID uint64, userID uint64, subID uint64, action string) error {
	return s.createTaskForNode(ctx, nodeID, userID, subID, action)
}

// TriggerForNodeGroups 为一个节点在指定分组影响到的活跃订阅创建任务。
func (s *NodeAccessService) TriggerForNodeGroups(ctx context.Context, nodeID uint64, groupIDs []uint64, action string) error {
	if len(groupIDs) == 0 {
		return nil
	}
	if action == "UPSERT_USER" {
		node, err := s.nodeRepo.FindByID(ctx, nodeID)
		if err != nil {
			return fmt.Errorf("find node %d: %w", nodeID, err)
		}
		if !node.IsEnabled {
			return nil
		}
	}

	subs, err := s.listActiveSubscriptionsForGroups(ctx, groupIDs)
	if err != nil {
		return err
	}
	var lastErr error
	for _, sub := range subs {
		if err := s.createTaskForNode(ctx, nodeID, sub.UserID, sub.ID, action); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// TriggerForNodeGroupNodes 为一个分组的部分节点同步该分组影响到的活跃订阅。
func (s *NodeAccessService) TriggerForNodeGroupNodes(ctx context.Context, groupID uint64, nodeIDs []uint64, action string) error {
	nodeIDs = uniqueUint64s(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}

	subs, err := s.listActiveSubscriptionsForGroups(ctx, []uint64{groupID})
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		return nil
	}

	var lastErr error
	for _, nodeID := range nodeIDs {
		if action == "UPSERT_USER" {
			node, err := s.nodeRepo.FindByID(ctx, nodeID)
			if err != nil {
				lastErr = err
				continue
			}
			if !node.IsEnabled {
				continue
			}
		}
		for _, sub := range subs {
			if err := s.createTaskForNode(ctx, nodeID, sub.UserID, sub.ID, action); err != nil {
				lastErr = err
			}
		}
	}
	return lastErr
}

// createTasksForSubscription 为订阅创建节点访问任务。
func (s *NodeAccessService) createTasksForSubscription(ctx context.Context, userID uint64, subID uint64, planID uint64, action string) error {
	// 查询套餐关联的节点组
	planNodeGroups, err := s.planRepo.FindNodeGroupIDs(ctx, planID)
	if err != nil {
		return fmt.Errorf("find plan node groups: %w", err)
	}

	// 查询用户获取 xray_user_key 和 uuid
	var xrayKey, uuid string
	if s.userRepo == nil {
		return fmt.Errorf("userRepo is nil")
	}
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("find user %d: %w", userID, err)
	}
	if user == nil {
		return fmt.Errorf("user %d not found", userID)
	}
	xrayKey = user.XrayUserKey
	uuid = user.UUID

	// 查询所有相关节点
	var allNodes []model.Node
	seenNodes := make(map[uint64]struct{})
	for _, ngID := range planNodeGroups {
		nodes, err := s.nodeRepo.FindByGroupID(ctx, ngID, true)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if _, ok := seenNodes[node.ID]; ok {
				continue
			}
			seenNodes[node.ID] = struct{}{}
			allNodes = append(allNodes, node)
		}
	}

	// 为每个节点创建任务（包含 payload）
	var lastErr error
	for _, node := range allNodes {
		idempotencyKey, err := generateIdempotencyKey(subID, node.ID, action)
		if err != nil {
			return fmt.Errorf("generate idempotency key: %w", err)
		}

		// 构建 payload
		payloadData := map[string]interface{}{
			"action":          action,
			"subscription_id": subID,
		}
		if xrayKey != "" {
			payloadData["xray_user_key"] = xrayKey
		}
		if uuid != "" {
			payloadData["uuid"] = uuid
		}
		payloadData["flow"] = "xtls-rprx-vision"

		payloadBytes, _ := json.Marshal(payloadData)
		payloadStr := string(payloadBytes)

		task := &model.NodeAccessTask{
			NodeID:         node.ID,
			SubscriptionID: &subID,
			Action:         action,
			Status:         "PENDING",
			ScheduledAt:    time.Now(),
			IdempotencyKey: idempotencyKey,
			Payload:        &payloadStr,
		}

		if err := s.taskRepo.Create(ctx, task); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func (s *NodeAccessService) createTaskForNode(ctx context.Context, nodeID uint64, userID uint64, subID uint64, action string) error {
	if s.userRepo == nil {
		return fmt.Errorf("userRepo is nil")
	}
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("find user %d: %w", userID, err)
	}
	node, err := s.nodeRepo.FindByID(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("find node %d: %w", nodeID, err)
	}

	idempotencyKey, err := generateIdempotencyKey(subID, node.ID, action)
	if err != nil {
		return fmt.Errorf("generate idempotency key: %w", err)
	}

	payloadData := map[string]interface{}{
		"action":          action,
		"subscription_id": subID,
		"xray_user_key":   user.XrayUserKey,
		"uuid":            user.UUID,
		"flow":            "xtls-rprx-vision",
	}
	payloadBytes, _ := json.Marshal(payloadData)
	payloadStr := string(payloadBytes)

	task := &model.NodeAccessTask{
		NodeID:         node.ID,
		SubscriptionID: &subID,
		Action:         action,
		Status:         "PENDING",
		ScheduledAt:    time.Now(),
		IdempotencyKey: idempotencyKey,
		Payload:        &payloadStr,
	}

	return s.taskRepo.Create(ctx, task)
}

func (s *NodeAccessService) listActiveSubscriptionsForGroups(ctx context.Context, groupIDs []uint64) ([]model.UserSubscription, error) {
	if s.subRepo == nil {
		return nil, fmt.Errorf("subRepo is nil")
	}
	groupIDs = uniqueUint64s(groupIDs)
	seenSubs := make(map[uint64]struct{})
	subs := make([]model.UserSubscription, 0)
	var lastErr error
	for _, groupID := range groupIDs {
		groupSubs, err := s.subRepo.ListActiveByNodeGroupID(ctx, groupID)
		if err != nil {
			lastErr = err
			continue
		}
		for _, sub := range groupSubs {
			if _, ok := seenSubs[sub.ID]; ok {
				continue
			}
			seenSubs[sub.ID] = struct{}{}
			subs = append(subs, sub)
		}
	}
	if len(subs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return subs, nil
}

func uniqueUint64s(values []uint64) []uint64 {
	seen := make(map[uint64]struct{}, len(values))
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// ProcessHeartbeat 处理 agent 心跳请求，返回待执行任务列表。
func (s *NodeAccessService) ProcessHeartbeat(ctx context.Context, nodeID uint64) ([]model.NodeAccessTask, error) {
	now := time.Now()
	lockToken, err := generateLockToken()
	if err != nil {
		return nil, fmt.Errorf("generate lock token: %w", err)
	}

	// 原子认领该节点的待执行任务，避免并发心跳重复下发同一任务。
	tasks, err := s.taskRepo.ClaimPendingByNodeID(ctx, nodeID, s.cfg.TaskRetryLimit, lockToken, now, 50)
	if err != nil {
		return nil, err
	}

	return tasks, nil
}

// ReportTaskResult 上报任务执行结果。
func (s *NodeAccessService) ReportTaskResult(ctx context.Context, taskID uint64, nodeID uint64, lockToken string, success bool, errMsg string) error {
	// 校验任务归属和锁
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if task.NodeID != nodeID {
		return fmt.Errorf("task %d does not belong to node %d", taskID, nodeID)
	}
	if task.LockToken == nil || *task.LockToken != lockToken {
		return fmt.Errorf("invalid lock token for task %d", taskID)
	}

	now := time.Now()
	if success {
		return s.taskRepo.MarkDone(ctx, taskID, now)
	}
	return s.taskRepo.MarkFailed(ctx, taskID, errMsg, now)
}

// generateIdempotencyKey 生成幂等键。
func generateIdempotencyKey(subID uint64, nodeID uint64, action string) (string, error) {
	raw := fmt.Sprintf("%d-%d-%s-%d", subID, nodeID, action, time.Now().UnixNano())
	suffix, err := secure.RandomHex(8)
	if err != nil {
		return "", err
	}
	return raw + "-" + suffix, nil
}

// generateLockToken 生成任务锁 Token。
func generateLockToken() (string, error) {
	return secure.RandomHex(16)
}
