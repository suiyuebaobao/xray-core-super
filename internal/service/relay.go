package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/secure"
	"suiyue/internal/repository"
)

// RelayService 管理中转节点后端绑定和配置任务。
type RelayService struct {
	relayRepo   *repository.RelayRepository
	backendRepo *repository.RelayBackendRepository
	taskRepo    *repository.RelayConfigTaskRepository
	nodeRepo    *repository.NodeRepository
	maxRetries  int
}

// NewRelayService 创建中转节点服务。
func NewRelayService(relayRepo *repository.RelayRepository, backendRepo *repository.RelayBackendRepository, taskRepo *repository.RelayConfigTaskRepository, nodeRepo *repository.NodeRepository, maxRetries int) *RelayService {
	if maxRetries <= 0 {
		maxRetries = 10
	}
	return &RelayService{
		relayRepo:   relayRepo,
		backendRepo: backendRepo,
		taskRepo:    taskRepo,
		nodeRepo:    nodeRepo,
		maxRetries:  maxRetries,
	}
}

// RelayBackendPayload 下发给 node-agent relay 模式的后端绑定。
type RelayBackendPayload struct {
	ID         uint64 `json:"id"`
	Name       string `json:"name"`
	ExitNodeID uint64 `json:"exit_node_id"`
	ListenPort uint32 `json:"listen_port"`
	TargetHost string `json:"target_host"`
	TargetPort uint32 `json:"target_port"`
	IsEnabled  bool   `json:"is_enabled"`
}

// RelayReloadPayload 下发给 node-agent relay 模式的完整 HAProxy 配置。
type RelayReloadPayload struct {
	Action        string                `json:"action"`
	ForwarderType string                `json:"forwarder_type"`
	Backends      []RelayBackendPayload `json:"backends"`
}

// SaveBackends 保存中转后端绑定并创建一次完整 reload 任务。
func (s *RelayService) SaveBackends(ctx context.Context, relayID uint64, reqs []model.RelayBackendRequest) ([]model.RelayBackend, error) {
	relay, err := s.relayRepo.FindByID(ctx, relayID)
	if err != nil {
		return nil, fmt.Errorf("find relay %d: %w", relayID, err)
	}
	if relay.ForwarderType == "" {
		relay.ForwarderType = "haproxy"
	}

	seenPorts := make(map[uint32]struct{}, len(reqs))
	backends := make([]model.RelayBackend, 0, len(reqs))
	for _, req := range reqs {
		if req.ListenPort == 0 || req.ListenPort > 65535 {
			return nil, fmt.Errorf("listen_port invalid: %d", req.ListenPort)
		}
		if _, exists := seenPorts[req.ListenPort]; exists {
			return nil, fmt.Errorf("listen_port %d duplicated", req.ListenPort)
		}
		seenPorts[req.ListenPort] = struct{}{}

		exitNode, err := s.nodeRepo.FindByID(ctx, req.ExitNodeID)
		if err != nil {
			return nil, fmt.Errorf("find exit node %d: %w", req.ExitNodeID, err)
		}

		targetHost := req.TargetHost
		if targetHost == "" {
			targetHost = exitNode.Host
		}
		targetPort := req.TargetPort
		if targetPort == 0 {
			targetPort = exitNode.Port
		}
		if targetHost == "" || targetPort == 0 || targetPort > 65535 {
			return nil, fmt.Errorf("target invalid for exit node %d", req.ExitNodeID)
		}

		name := req.Name
		if name == "" {
			name = fmt.Sprintf("%s -> %s", relay.Name, exitNode.Name)
		}

		backends = append(backends, model.RelayBackend{
			RelayID:    relayID,
			ExitNodeID: req.ExitNodeID,
			Name:       name,
			ListenPort: req.ListenPort,
			TargetHost: targetHost,
			TargetPort: targetPort,
			IsEnabled:  req.IsEnabled,
			SortWeight: req.SortWeight,
		})
	}

	saved, err := s.backendRepo.SaveForRelay(ctx, relayID, backends)
	if err != nil {
		return nil, err
	}
	if err := s.CreateReloadTask(ctx, relayID); err != nil {
		return saved, err
	}
	return saved, nil
}

// CreateReloadTask 创建完整中转配置刷新任务。
func (s *RelayService) CreateReloadTask(ctx context.Context, relayID uint64) error {
	relay, err := s.relayRepo.FindByID(ctx, relayID)
	if err != nil {
		return err
	}
	forwarderType := relay.ForwarderType
	if forwarderType == "" {
		forwarderType = "haproxy"
	}

	backends, err := s.backendRepo.ListByRelayID(ctx, relayID)
	if err != nil {
		return err
	}
	payloadBackends := make([]RelayBackendPayload, 0, len(backends))
	for _, backend := range backends {
		if !backend.IsEnabled {
			continue
		}
		payloadBackends = append(payloadBackends, RelayBackendPayload{
			ID:         backend.ID,
			Name:       backend.Name,
			ExitNodeID: backend.ExitNodeID,
			ListenPort: backend.ListenPort,
			TargetHost: backend.TargetHost,
			TargetPort: backend.TargetPort,
			IsEnabled:  backend.IsEnabled,
		})
	}

	payload := RelayReloadPayload{
		Action:        "RELOAD_CONFIG",
		ForwarderType: forwarderType,
		Backends:      payloadBackends,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	payloadStr := string(payloadBytes)
	idempotencyKey, err := generateRelayTaskKey(relayID, "RELOAD_CONFIG")
	if err != nil {
		return err
	}
	return s.taskRepo.Create(ctx, &model.RelayConfigTask{
		RelayID:        relayID,
		Action:         "RELOAD_CONFIG",
		Payload:        &payloadStr,
		Status:         "PENDING",
		ScheduledAt:    time.Now(),
		IdempotencyKey: idempotencyKey,
	})
}

// ProcessHeartbeat 处理 relay agent 心跳并返回待执行任务。
func (s *RelayService) ProcessHeartbeat(ctx context.Context, relayID uint64) ([]model.RelayConfigTask, error) {
	lockToken, err := generateRelayLockToken()
	if err != nil {
		return nil, fmt.Errorf("generate lock token: %w", err)
	}
	return s.taskRepo.ClaimPendingByRelayID(ctx, relayID, s.maxRetries, lockToken, time.Now(), 50)
}

// ReportTaskResult 上报 relay agent 配置任务执行结果。
func (s *RelayService) ReportTaskResult(ctx context.Context, taskID uint64, relayID uint64, lockToken string, success bool, errMsg string) error {
	task, err := s.taskRepo.FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("task not found: %w", err)
	}
	if task.RelayID != relayID {
		return fmt.Errorf("task %d does not belong to relay %d", taskID, relayID)
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

// RelayTrafficItem 中转线路级流量计数器。
type RelayTrafficItem struct {
	RelayBackendID *uint64 `json:"relay_backend_id"`
	ListenPort     uint32  `json:"listen_port"`
	BytesInTotal   uint64  `json:"bytes_in_total"`
	BytesOutTotal  uint64  `json:"bytes_out_total"`
}

// RelayTrafficService 处理中转线路级流量上报。
type RelayTrafficService struct {
	snapshotRepo *repository.RelayTrafficSnapshotRepository
}

// NewRelayTrafficService 创建中转流量服务。
func NewRelayTrafficService(snapshotRepo *repository.RelayTrafficSnapshotRepository) *RelayTrafficService {
	return &RelayTrafficService{snapshotRepo: snapshotRepo}
}

// ProcessTrafficReport 保存中转线路级快照。该数据仅用于运维分析，不参与用户套餐扣量。
func (s *RelayTrafficService) ProcessTrafficReport(ctx context.Context, relayID uint64, items []RelayTrafficItem) error {
	if s == nil || s.snapshotRepo == nil {
		return nil
	}
	now := time.Now()
	snapshots := make([]model.RelayTrafficSnapshot, 0, len(items))
	for _, item := range items {
		if item.ListenPort == 0 {
			continue
		}
		snapshots = append(snapshots, model.RelayTrafficSnapshot{
			RelayID:        relayID,
			RelayBackendID: item.RelayBackendID,
			ListenPort:     item.ListenPort,
			BytesInTotal:   item.BytesInTotal,
			BytesOutTotal:  item.BytesOutTotal,
			CapturedAt:     now,
		})
	}
	return s.snapshotRepo.CreateBatch(ctx, snapshots)
}

func generateRelayTaskKey(relayID uint64, action string) (string, error) {
	suffix, err := secure.RandomHex(8)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("relay-%d-%s-%d-%s", relayID, action, time.Now().UnixNano(), suffix), nil
}

func generateRelayLockToken() (string, error) {
	return secure.RandomHex(16)
}
