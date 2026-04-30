// node_agent_handler.go — Node Agent HTTP 处理器。
//
// 职责：
// - 处理 node-agent 心跳请求（POST /api/agent/heartbeat）
// - 返回待执行任务列表
// - 处理 node-agent 任务执行结果上报（POST /api/agent/task-result）
// - 处理 node-agent 流量数据上报（POST /api/agent/traffic）
package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

// AgentHandler node-agent 通信处理器。
type AgentHandler struct {
	nodeAccessSvc *service.NodeAccessService
	trafficSvc    *service.TrafficService
	nodeRepo      *repository.NodeRepository
	relaySvc      *service.RelayService
	relayTraffic  *service.RelayTrafficService
	relayRepo     *repository.RelayRepository
}

func validAgentToken(storedHash, token string) bool {
	if storedHash == "" || token == "" {
		return false
	}
	sum := sha256.Sum256([]byte(token))
	providedHash := hex.EncodeToString(sum[:])
	if len(storedHash) != len(providedHash) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(providedHash)) == 1
}

// NewAgentHandler 创建 node-agent 通信处理器。
func NewAgentHandler(nodeAccessSvc *service.NodeAccessService, trafficSvc *service.TrafficService, nodeRepo *repository.NodeRepository) *AgentHandler {
	return &AgentHandler{
		nodeAccessSvc: nodeAccessSvc,
		trafficSvc:    trafficSvc,
		nodeRepo:      nodeRepo,
	}
}

// NewAgentHandlerWithRelay 创建同时支持出口节点和中转节点的 agent 通信处理器。
func NewAgentHandlerWithRelay(nodeAccessSvc *service.NodeAccessService, trafficSvc *service.TrafficService, nodeRepo *repository.NodeRepository, relaySvc *service.RelayService, relayTraffic *service.RelayTrafficService, relayRepo *repository.RelayRepository) *AgentHandler {
	h := NewAgentHandler(nodeAccessSvc, trafficSvc, nodeRepo)
	h.relaySvc = relaySvc
	h.relayTraffic = relayTraffic
	h.relayRepo = relayRepo
	return h
}

// Heartbeat 处理 POST /api/agent/heartbeat — 节点心跳上报。
// 返回待执行任务列表。
func (h *AgentHandler) Heartbeat(c *gin.Context) {
	var req struct {
		NodeID  uint64 `json:"node_id" binding:"required"`
		Version string `json:"version"`
		Token   string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	// 验证节点凭证
	node, err := h.nodeRepo.FindByID(c.Request.Context(), req.NodeID)
	if err != nil || !validAgentToken(node.AgentTokenHash, req.Token) {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	// 更新节点最后心跳时间
	_ = h.nodeRepo.UpdateHeartbeat(c.Request.Context(), req.NodeID)

	// 获取待执行任务
	tasks, err := h.nodeAccessSvc.ProcessHeartbeat(c.Request.Context(), req.NodeID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	// 转换为 agent 任务格式
	agentTasks := make([]model.AgentTask, 0, len(tasks))
	for _, t := range tasks {
		payloadStr := ""
		if t.Payload != nil {
			payloadStr = *t.Payload
		}
		lockToken := ""
		if t.LockToken != nil {
			lockToken = *t.LockToken
		}
		agentTasks = append(agentTasks, model.AgentTask{
			ID:             int64(t.ID),
			Action:         t.Action,
			Payload:        payloadStr,
			IdempotencyKey: t.IdempotencyKey,
			LockToken:      lockToken,
		})
	}

	response.Success(c, gin.H{"tasks": agentTasks})
}

// TaskResult 处理 POST /api/agent/task-result — 任务执行结果上报。
func (h *AgentHandler) TaskResult(c *gin.Context) {
	var req struct {
		NodeID    uint64 `json:"node_id" binding:"required"`
		Token     string `json:"token" binding:"required"`
		TaskID    uint64 `json:"task_id" binding:"required"`
		Success   bool   `json:"success"`
		Error     string `json:"error"`
		LockToken string `json:"lock_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	// 验证节点凭证
	node, err := h.nodeRepo.FindByID(c.Request.Context(), req.NodeID)
	if err != nil || !validAgentToken(node.AgentTokenHash, req.Token) {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	if err := h.nodeAccessSvc.ReportTaskResult(c.Request.Context(), req.TaskID, req.NodeID, req.LockToken, req.Success, req.Error); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// TrafficReport 处理 POST /api/agent/traffic — 流量数据上报。
func (h *AgentHandler) TrafficReport(c *gin.Context) {
	var req struct {
		NodeID uint64                `json:"node_id" binding:"required"`
		Token  string                `json:"token" binding:"required"`
		Items  []service.TrafficItem `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	// 验证节点凭证
	node, err := h.nodeRepo.FindByID(c.Request.Context(), req.NodeID)
	if err != nil || !validAgentToken(node.AgentTokenHash, req.Token) {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	if err := h.trafficSvc.ProcessTrafficReport(c.Request.Context(), req.NodeID, req.Items); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// RelayHeartbeat 处理 POST /api/agent/relay/heartbeat — 中转节点心跳上报。
func (h *AgentHandler) RelayHeartbeat(c *gin.Context) {
	if h.relayRepo == nil || h.relaySvc == nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	var req struct {
		RelayID uint64 `json:"relay_id" binding:"required"`
		Version string `json:"version"`
		Token   string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	relay, err := h.relayRepo.FindByID(c.Request.Context(), req.RelayID)
	if err != nil || !validAgentToken(relay.AgentTokenHash, req.Token) {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	_ = h.relayRepo.UpdateHeartbeat(c.Request.Context(), req.RelayID, req.Version)
	tasks, err := h.relaySvc.ProcessHeartbeat(c.Request.Context(), req.RelayID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	agentTasks := make([]model.AgentTask, 0, len(tasks))
	for _, t := range tasks {
		payloadStr := ""
		if t.Payload != nil {
			payloadStr = *t.Payload
		}
		lockToken := ""
		if t.LockToken != nil {
			lockToken = *t.LockToken
		}
		agentTasks = append(agentTasks, model.AgentTask{
			ID:             int64(t.ID),
			Action:         t.Action,
			Payload:        payloadStr,
			IdempotencyKey: t.IdempotencyKey,
			LockToken:      lockToken,
		})
	}

	response.Success(c, gin.H{"tasks": agentTasks})
}

// RelayTaskResult 处理 POST /api/agent/relay/task-result — 中转配置任务执行结果。
func (h *AgentHandler) RelayTaskResult(c *gin.Context) {
	if h.relayRepo == nil || h.relaySvc == nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	var req struct {
		RelayID   uint64 `json:"relay_id" binding:"required"`
		Token     string `json:"token" binding:"required"`
		TaskID    uint64 `json:"task_id" binding:"required"`
		Success   bool   `json:"success"`
		Error     string `json:"error"`
		LockToken string `json:"lock_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	relay, err := h.relayRepo.FindByID(c.Request.Context(), req.RelayID)
	if err != nil || !validAgentToken(relay.AgentTokenHash, req.Token) {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}

	if err := h.relaySvc.ReportTaskResult(c.Request.Context(), req.TaskID, req.RelayID, req.LockToken, req.Success, req.Error); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// RelayTrafficReport 处理 POST /api/agent/relay/traffic — 中转线路级指标上报。
func (h *AgentHandler) RelayTrafficReport(c *gin.Context) {
	if h.relayRepo == nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	var req struct {
		RelayID uint64                     `json:"relay_id" binding:"required"`
		Token   string                     `json:"token" binding:"required"`
		Items   []service.RelayTrafficItem `json:"items" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	relay, err := h.relayRepo.FindByID(c.Request.Context(), req.RelayID)
	if err != nil || !validAgentToken(relay.AgentTokenHash, req.Token) {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}
	if h.relayTraffic != nil {
		if err := h.relayTraffic.ProcessTrafficReport(c.Request.Context(), req.RelayID, req.Items); err != nil {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
	}

	response.Success(c, nil)
}
