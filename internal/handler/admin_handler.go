// admin_handler.go — 管理后台 HTTP 处理器。
//
// 职责：
// - 套餐管理（增删改查）
// - 节点分组管理（增删改查）
// - 节点管理（增删改查）
// - 用户管理查询
//
// 所有接口需要管理员权限，由 middleware.RequireAdmin() 中间件保证。
// 不写业务逻辑，只做参数绑定和响应输出。
package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/platform/secure"
	"suiyue/internal/repository"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// AdminPlanHandler 管理后台套餐处理器。
type AdminPlanHandler struct {
	planRepo      *repository.PlanRepository
	nodeGroupRepo *repository.NodeGroupRepository
	nodeAccessSvc nodeAccessSubscriptionSyncer
}

// AdminDashboardHandler 管理后台仪表盘处理器。
type AdminDashboardHandler struct {
	userRepo *repository.UserRepository
	nodeRepo *repository.NodeRepository
	planRepo *repository.PlanRepository
	subRepo  *repository.SubscriptionRepository
}

// NewAdminDashboardHandler 创建仪表盘处理器。
func NewAdminDashboardHandler(userRepo *repository.UserRepository, nodeRepo *repository.NodeRepository, planRepo *repository.PlanRepository, subRepo *repository.SubscriptionRepository) *AdminDashboardHandler {
	return &AdminDashboardHandler{
		userRepo: userRepo,
		nodeRepo: nodeRepo,
		planRepo: planRepo,
		subRepo:  subRepo,
	}
}

// Stats 处理 GET /api/admin/dashboard/stats — 获取后台统计数据。
func (h *AdminDashboardHandler) Stats(c *gin.Context) {
	ctx := c.Request.Context()

	userCount, err := h.userRepo.Count(ctx)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	nodeCount, err := h.nodeRepo.Count(ctx)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	planCount, err := h.planRepo.Count(ctx)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	activeSubCount, err := h.subRepo.CountActive(ctx)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{
		"user_count":       userCount,
		"node_count":       nodeCount,
		"plan_count":       planCount,
		"active_sub_count": activeSubCount,
	})
}

// NewAdminPlanHandler 创建管理后台套餐处理器。
func NewAdminPlanHandler(planRepo *repository.PlanRepository, nodeGroupRepo ...*repository.NodeGroupRepository) *AdminPlanHandler {
	h := &AdminPlanHandler{planRepo: planRepo}
	if len(nodeGroupRepo) > 0 {
		h.nodeGroupRepo = nodeGroupRepo[0]
	}
	return h
}

// NewAdminPlanHandlerWithSync 创建带订阅同步能力的套餐处理器。
func NewAdminPlanHandlerWithSync(planRepo *repository.PlanRepository, nodeGroupRepo *repository.NodeGroupRepository, nodeAccessSvc nodeAccessSubscriptionSyncer) *AdminPlanHandler {
	h := NewAdminPlanHandler(planRepo, nodeGroupRepo)
	h.nodeAccessSvc = nodeAccessSvc
	return h
}

// Create 处理 POST /api/admin/plans — 创建套餐。
func (h *AdminPlanHandler) Create(c *gin.Context) {
	var req model.CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.Currency == "" {
		req.Currency = "USDT"
	}

	plan, err := h.planRepo.Create(c.Request.Context(), &model.Plan{
		Name:         req.Name,
		Price:        req.Price,
		Currency:     req.Currency,
		TrafficLimit: req.TrafficLimit,
		DurationDays: req.DurationDays,
		SortWeight:   req.SortWeight,
		IsActive:     req.IsActive,
	})
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, plan)
}

// Update 处理 PUT /api/admin/plans/:id — 更新套餐。
func (h *AdminPlanHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req model.UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	plan, err := h.planRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	if req.Currency == "" {
		req.Currency = plan.Currency
	}

	// 更新字段
	plan.Name = req.Name
	plan.Price = req.Price
	plan.Currency = req.Currency
	plan.TrafficLimit = req.TrafficLimit
	plan.DurationDays = req.DurationDays
	plan.SortWeight = req.SortWeight
	plan.IsActive = req.IsActive
	if plan.IsDefault {
		plan.IsActive = true
	}

	if err := h.planRepo.Update(c.Request.Context(), plan); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, plan)
}

// Delete 处理 DELETE /api/admin/plans/:id — 删除套餐。
func (h *AdminPlanHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	result, err := h.planRepo.DeleteWithDefaultFallback(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, repository.ErrDefaultPlanCannotDelete) {
			response.HandleError(c, &response.AppError{
				Code:     40010,
				HTTPCode: http.StatusBadRequest,
				Message:  repository.ErrDefaultPlanCannotDelete.Error(),
			})
			return
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.HandleError(c, response.ErrNotFound)
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.syncPlanDeleteFallback(c.Request.Context(), result)

	response.Success(c, gin.H{
		"default_plan_id":          result.DefaultPlanID,
		"moved_subscription_count": len(result.MovedSubscriptions),
	})
}

func (h *AdminPlanHandler) syncPlanDeleteFallback(ctx context.Context, result *repository.PlanDeleteResult) {
	if h.nodeAccessSvc == nil || result == nil {
		return
	}
	for _, moved := range result.MovedSubscriptions {
		if err := h.nodeAccessSvc.TriggerOnExpire(ctx, moved.UserID, moved.SubscriptionID, moved.OldPlanID); err != nil {
			log.Printf("[admin] delete plan fallback expire sync failed: user=%d sub=%d old_plan=%d err=%v", moved.UserID, moved.SubscriptionID, moved.OldPlanID, err)
		}
		if err := h.nodeAccessSvc.TriggerOnRenew(ctx, moved.UserID, moved.SubscriptionID, moved.NewPlanID); err != nil {
			log.Printf("[admin] delete plan fallback renew sync failed: user=%d sub=%d new_plan=%d err=%v", moved.UserID, moved.SubscriptionID, moved.NewPlanID, err)
		}
	}
}

// List 处理 GET /api/admin/plans — 套餐列表（含已下架）。
func (h *AdminPlanHandler) List(c *gin.Context) {
	plans, err := h.planRepo.ListAll(c.Request.Context())
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	// 为每个套餐附加关联的节点分组 ID 列表
	type PlanWithNodeGroups struct {
		model.Plan
		NodeGroupIds   []uint64          `json:"node_group_ids"`
		NodeGroups     []model.NodeGroup `json:"node_groups"`
		NodeGroupCount int               `json:"node_group_count"`
	}
	result := make([]PlanWithNodeGroups, 0, len(plans))
	for _, plan := range plans {
		ids, err := h.planRepo.FindNodeGroupIDs(c.Request.Context(), plan.ID)
		if err != nil {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
		groups := []model.NodeGroup{}
		if h.nodeGroupRepo != nil {
			groups, err = h.nodeGroupRepo.FindByIDs(c.Request.Context(), ids)
			if err != nil {
				response.HandleError(c, response.ErrInternalServer)
				return
			}
		}
		result = append(result, PlanWithNodeGroups{
			Plan:           plan,
			NodeGroupIds:   ids,
			NodeGroups:     groups,
			NodeGroupCount: len(ids),
		})
	}

	response.Success(c, gin.H{"plans": result})
}

// AdminNodeGroupHandler 管理后台节点分组处理器。
type AdminNodeGroupHandler struct {
	nodeGroupRepo *repository.NodeGroupRepository
	nodeRepo      *repository.NodeRepository
	nodeAccessSvc nodeAccessNodeGroupSyncer
}

// NewAdminNodeGroupHandler 创建管理后台节点分组处理器。
func NewAdminNodeGroupHandler(nodeGroupRepo *repository.NodeGroupRepository) *AdminNodeGroupHandler {
	return &AdminNodeGroupHandler{nodeGroupRepo: nodeGroupRepo}
}

// NewAdminNodeGroupHandlerWithNodes 创建带节点绑定能力的节点分组处理器。
func NewAdminNodeGroupHandlerWithNodes(nodeGroupRepo *repository.NodeGroupRepository, nodeRepo *repository.NodeRepository, nodeAccessSvc nodeAccessNodeGroupSyncer) *AdminNodeGroupHandler {
	return &AdminNodeGroupHandler{nodeGroupRepo: nodeGroupRepo, nodeRepo: nodeRepo, nodeAccessSvc: nodeAccessSvc}
}

type nodeAccessNodeGroupSyncer interface {
	TriggerForNodeGroupNodes(ctx context.Context, groupID uint64, nodeIDs []uint64, action string) error
}

// Create 处理 POST /api/admin/node-groups — 创建节点分组。
func (h *AdminNodeGroupHandler) Create(c *gin.Context) {
	var req model.CreateNodeGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	group := &model.NodeGroup{
		Name: req.Name,
	}
	if req.Description != "" {
		group.Description = &req.Description
	}

	group, err := h.nodeGroupRepo.Create(c.Request.Context(), group)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, group)
}

// Update 处理 PUT /api/admin/node-groups/:id — 更新节点分组。
func (h *AdminNodeGroupHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req model.UpdateNodeGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	group, err := h.nodeGroupRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	group.Name = req.Name
	if req.Description != "" {
		group.Description = &req.Description
	} else {
		group.Description = nil
	}

	if err := h.nodeGroupRepo.Update(c.Request.Context(), group); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, group)
}

// Delete 处理 DELETE /api/admin/node-groups/:id — 删除节点分组。
func (h *AdminNodeGroupHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if err := h.nodeGroupRepo.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, repository.ErrNodeGroupInUse) {
			response.HandleError(c, &response.AppError{
				Code:     40007,
				HTTPCode: http.StatusBadRequest,
				Message:  "该分组下存在节点，无法删除",
			})
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// List 处理 GET /api/admin/node-groups — 节点分组列表。
func (h *AdminNodeGroupHandler) List(c *gin.Context) {
	groups, err := h.nodeGroupRepo.List(c.Request.Context())
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	type NodeGroupWithNodes struct {
		model.NodeGroup
		Nodes     []model.Node `json:"nodes"`
		NodeCount int          `json:"node_count"`
	}
	result := make([]NodeGroupWithNodes, 0, len(groups))
	for _, group := range groups {
		nodes := []model.Node{}
		if h.nodeRepo != nil {
			nodes, err = h.nodeRepo.FindByGroupID(c.Request.Context(), group.ID, false)
			if err != nil {
				response.HandleError(c, response.ErrInternalServer)
				return
			}
		}
		result = append(result, NodeGroupWithNodes{
			NodeGroup: group,
			Nodes:     nodes,
			NodeCount: len(nodes),
		})
	}

	response.Success(c, gin.H{"groups": result})
}

// ListNodes 处理 GET /api/admin/node-groups/:id/nodes — 查询节点分组内节点。
func (h *AdminNodeGroupHandler) ListNodes(c *gin.Context) {
	if h.nodeRepo == nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if _, err := h.nodeGroupRepo.FindByID(c.Request.Context(), id); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	nodes, err := h.nodeRepo.FindByGroupID(c.Request.Context(), id, false)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{"nodes": nodes})
}

// BindNodes 处理 PUT /api/admin/node-groups/:id/nodes — 绑定节点分组与节点。
func (h *AdminNodeGroupHandler) BindNodes(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	var req model.BindNodeGroupNodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if _, err := h.nodeGroupRepo.FindByID(c.Request.Context(), id); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	change, err := h.nodeGroupRepo.BindNodes(c.Request.Context(), id, req.NodeIDs)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidNodeID) {
			response.HandleError(c, response.ErrBadRequest)
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.syncNodeGroupBindingChange(c.Request.Context(), id, change)

	response.Success(c, gin.H{
		"node_ids":         change.NodeIDs,
		"added_node_ids":   change.AddedNodeIDs,
		"removed_node_ids": change.RemovedNodeIDs,
	})
}

func (h *AdminNodeGroupHandler) syncNodeGroupBindingChange(ctx context.Context, groupID uint64, change *repository.NodeGroupNodeBindingChange) {
	if h.nodeAccessSvc == nil || change == nil {
		return
	}
	if len(change.RemovedNodeIDs) > 0 {
		if err := h.nodeAccessSvc.TriggerForNodeGroupNodes(ctx, groupID, change.RemovedNodeIDs, "DISABLE_USER"); err != nil {
			log.Printf("[admin] sync node group %d removed nodes failed: %v", groupID, err)
		}
	}
	if len(change.AddedNodeIDs) > 0 {
		if err := h.nodeAccessSvc.TriggerForNodeGroupNodes(ctx, groupID, change.AddedNodeIDs, "UPSERT_USER"); err != nil {
			log.Printf("[admin] sync node group %d added nodes failed: %v", groupID, err)
		}
	}
}

// AdminNodeHandler 管理后台节点处理器。
type AdminNodeHandler struct {
	nodeRepo      *repository.NodeRepository
	nodeHostRepo  *repository.NodeHostRepository
	subRepo       *repository.SubscriptionRepository
	nodeAccessSvc nodeAccessNodeSyncer
}

// NewAdminNodeHandler 创建管理后台节点处理器。
func NewAdminNodeHandler(nodeRepo *repository.NodeRepository) *AdminNodeHandler {
	return &AdminNodeHandler{nodeRepo: nodeRepo}
}

// NewAdminNodeHandlerWithSync 创建带节点访问同步能力的节点处理器。
func NewAdminNodeHandlerWithSync(nodeRepo *repository.NodeRepository, subRepo *repository.SubscriptionRepository, nodeAccessSvc nodeAccessNodeSyncer, nodeHostRepo ...*repository.NodeHostRepository) *AdminNodeHandler {
	var hostRepo *repository.NodeHostRepository
	if len(nodeHostRepo) > 0 {
		hostRepo = nodeHostRepo[0]
	}
	return &AdminNodeHandler{nodeRepo: nodeRepo, nodeHostRepo: hostRepo, subRepo: subRepo, nodeAccessSvc: nodeAccessSvc}
}

type nodeAccessNodeSyncer interface {
	TriggerForNodeGroups(ctx context.Context, nodeID uint64, groupIDs []uint64, action string) error
}

func normalizeNodeTransportFields(transport, xhttpPath, xhttpHost, xhttpMode, flow *string) {
	t := strings.ToLower(strings.TrimSpace(*transport))
	if t == "" {
		t = "tcp"
	}
	if t != "xhttp" {
		t = "tcp"
	}
	*transport = t

	*xhttpHost = strings.TrimSpace(*xhttpHost)
	mode := strings.ToLower(strings.TrimSpace(*xhttpMode))
	switch mode {
	case "packet-up", "stream-up", "stream-one":
	default:
		mode = "auto"
	}
	*xhttpMode = mode

	if t == "xhttp" {
		path := strings.TrimSpace(*xhttpPath)
		if path == "" {
			path = "/raypilot"
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		*xhttpPath = path
		*flow = ""
		return
	}

	*xhttpPath = ""
	*xhttpHost = ""
	*xhttpMode = "auto"
	if strings.TrimSpace(*flow) == "" {
		*flow = "xtls-rprx-vision"
	} else {
		*flow = strings.TrimSpace(*flow)
	}
}

type nodeTransportOption struct {
	Transport string
	Port      uint32
	XHTTPPath string
	XHTTPHost string
	XHTTPMode string
	Flow      string
}

func normalizeNodeTransportOptions(req *model.CreateNodeRequest) ([]nodeTransportOption, error) {
	transports := normalizeNodeTransportList(req.Transports, req.Transport)
	if len(transports) == 0 {
		transports = []string{"tcp"}
	}
	xhttpPath := req.XHTTPPath
	xhttpHost := req.XHTTPHost
	xhttpMode := req.XHTTPMode
	flow := req.Flow

	tcpPort := req.TCPPort
	if tcpPort == 0 {
		if req.Port != 0 && (len(transports) == 1 || transports[0] == "tcp") {
			tcpPort = req.Port
		} else {
			tcpPort = 443
		}
	}
	xhttpPort := req.XHTTPPort
	if xhttpPort == 0 {
		if req.Port != 0 && len(transports) == 1 && transports[0] == "xhttp" {
			xhttpPort = req.Port
		} else if len(transports) > 1 {
			xhttpPort = 8443
		} else {
			xhttpPort = 443
		}
	}

	options := make([]nodeTransportOption, 0, len(transports))
	seenPorts := map[uint32]string{}
	for _, transport := range transports {
		option := nodeTransportOption{Transport: transport}
		if transport == "xhttp" {
			option.Port = xhttpPort
			option.XHTTPPath = xhttpPath
			option.XHTTPHost = xhttpHost
			option.XHTTPMode = xhttpMode
			option.Flow = flow
		} else {
			option.Port = tcpPort
			option.Flow = flow
		}
		normalizeNodeTransportFields(&option.Transport, &option.XHTTPPath, &option.XHTTPHost, &option.XHTTPMode, &option.Flow)
		if option.Port == 0 || option.Port > 65535 {
			return nil, fmt.Errorf("%s 端口无效: %d", strings.ToUpper(option.Transport), option.Port)
		}
		if existing, ok := seenPorts[option.Port]; ok {
			return nil, fmt.Errorf("%s 与 %s 不能使用同一个端口 %d", strings.ToUpper(existing), strings.ToUpper(option.Transport), option.Port)
		}
		seenPorts[option.Port] = option.Transport
		options = append(options, option)
	}
	return options, nil
}

func normalizeNodeTransportList(transports []string, fallback string) []string {
	seen := map[string]struct{}{}
	appendTransport := func(raw string) {
		transport := strings.ToLower(strings.TrimSpace(raw))
		if transport != "xhttp" {
			transport = "tcp"
		}
		seen[transport] = struct{}{}
	}
	for _, transport := range transports {
		appendTransport(transport)
	}
	if len(seen) == 0 {
		appendTransport(fallback)
	}
	result := make([]string, 0, len(seen))
	for _, transport := range []string{"tcp", "xhttp"} {
		if _, ok := seen[transport]; ok {
			result = append(result, transport)
		}
	}
	return result
}

func applyNodeTransportOption(node *model.Node, option nodeTransportOption) {
	node.Transport = option.Transport
	node.Port = option.Port
	node.Flow = option.Flow
	node.XHTTPPath = option.XHTTPPath
	node.XHTTPHost = option.XHTTPHost
	node.XHTTPMode = option.XHTTPMode
}

func multiTransportNodeName(base string, option nodeTransportOption) string {
	if option.Transport == "tcp" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, strings.ToUpper(option.Transport))
}

func nodeListenIPForHost(host string) string {
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil && ip.To4() != nil {
		return ip.To4().String()
	}
	return "0.0.0.0"
}

// Create 处理 POST /api/admin/nodes — 创建节点。
func (h *AdminNodeHandler) Create(c *gin.Context) {
	var req model.CreateNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.Protocol == "" {
		req.Protocol = "vless"
	}
	options, err := normalizeNodeTransportOptions(&req)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.Fingerprint == "" {
		req.Fingerprint = "chrome"
	}
	if req.LineMode == "" {
		req.LineMode = "direct_and_relay"
	}

	// 计算 Agent Token 哈希（SHA-256，不存明文）
	agentTokenHash := ""
	if req.AgentToken != "" {
		h := sha256.Sum256([]byte(req.AgentToken))
		agentTokenHash = hex.EncodeToString(h[:])
	}

	if len(options) > 1 {
		if h.nodeHostRepo == nil {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
		nodeHost, err := h.nodeHostRepo.Create(c.Request.Context(), &model.NodeHost{
			Name:           req.Name,
			SSHHost:        req.Host,
			SSHPort:        22,
			AgentBaseURL:   req.AgentBaseURL,
			AgentTokenHash: agentTokenHash,
			IsEnabled:      true,
		})
		if err != nil {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
		listenIP := nodeListenIPForHost(req.Host)
		nodes := make([]*model.Node, 0, len(options))
		createdNodeIDs := make([]uint64, 0, len(options))
		cleanup := func() {
			for _, id := range createdNodeIDs {
				_ = h.nodeRepo.Delete(c.Request.Context(), id)
			}
			_ = h.nodeHostRepo.Delete(c.Request.Context(), nodeHost.ID)
		}
		for i, option := range options {
			node := &model.Node{
				Name:           multiTransportNodeName(req.Name, option),
				Protocol:       req.Protocol,
				Host:           req.Host,
				ServerName:     req.ServerName,
				PublicKey:      req.PublicKey,
				ShortID:        req.ShortID,
				Fingerprint:    req.Fingerprint,
				LineMode:       req.LineMode,
				NodeHostID:     &nodeHost.ID,
				ListenIP:       listenIP,
				OutboundIP:     listenIP,
				AgentBaseURL:   req.AgentBaseURL,
				AgentTokenHash: agentTokenHash,
				SortWeight:     req.SortWeight + i,
				IsEnabled:      req.IsEnabled,
			}
			applyNodeTransportOption(node, option)
			created, err := h.nodeRepo.Create(c.Request.Context(), node)
			if err != nil {
				cleanup()
				response.HandleError(c, response.ErrInternalServer)
				return
			}
			createdNodeIDs = append(createdNodeIDs, created.ID)
			created.XrayInboundTag = fmt.Sprintf("node_%d_in", created.ID)
			created.XrayOutboundTag = fmt.Sprintf("node_%d_out", created.ID)
			if err := h.nodeRepo.Update(c.Request.Context(), created); err != nil {
				cleanup()
				response.HandleError(c, response.ErrInternalServer)
				return
			}
			nodes = append(nodes, created)
		}
		response.Success(c, gin.H{"node_host": nodeHost, "nodes": nodes})
		return
	}

	node := &model.Node{
		Name:           req.Name,
		Protocol:       req.Protocol,
		Host:           req.Host,
		ServerName:     req.ServerName,
		PublicKey:      req.PublicKey,
		ShortID:        req.ShortID,
		Fingerprint:    req.Fingerprint,
		LineMode:       req.LineMode,
		AgentBaseURL:   req.AgentBaseURL,
		AgentTokenHash: agentTokenHash,
		SortWeight:     req.SortWeight,
		IsEnabled:      req.IsEnabled,
	}
	applyNodeTransportOption(node, options[0])
	node, err = h.nodeRepo.Create(c.Request.Context(), node)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, node)
}

// Update 处理 PUT /api/admin/nodes/:id — 更新节点。
func (h *AdminNodeHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req model.UpdateNodeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	node, err := h.nodeRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	oldEnabled := node.IsEnabled
	oldGroupIDs, err := h.nodeRepo.FindGroupIDsByNodeID(c.Request.Context(), node.ID)
	if err != nil {
		log.Printf("[admin] list node %d groups before update failed: %v", node.ID, err)
		oldGroupIDs = nil
	}

	node.Name = req.Name
	if req.Protocol != "" {
		node.Protocol = req.Protocol
	}
	if req.Transport == "" {
		req.Transport = node.Transport
	}
	normalizeNodeTransportFields(&req.Transport, &req.XHTTPPath, &req.XHTTPHost, &req.XHTTPMode, &req.Flow)
	if req.Transport != "" {
		node.Transport = req.Transport
	}
	node.Host = req.Host
	if req.Port != 0 {
		node.Port = req.Port
	}
	node.ServerName = req.ServerName
	node.PublicKey = req.PublicKey
	node.ShortID = req.ShortID
	if req.Fingerprint != "" {
		node.Fingerprint = req.Fingerprint
	}
	if req.Flow != "" {
		node.Flow = req.Flow
	}
	if req.Transport == "xhttp" {
		node.Flow = ""
	}
	if req.LineMode != "" {
		node.LineMode = req.LineMode
	}
	node.XHTTPPath = req.XHTTPPath
	node.XHTTPHost = req.XHTTPHost
	node.XHTTPMode = req.XHTTPMode
	node.AgentBaseURL = req.AgentBaseURL
	if req.AgentToken != "" {
		h := sha256.Sum256([]byte(req.AgentToken))
		node.AgentTokenHash = hex.EncodeToString(h[:])
	}
	node.SortWeight = req.SortWeight
	node.IsEnabled = req.IsEnabled

	if err := h.nodeRepo.Update(c.Request.Context(), node); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	newGroupIDs, err := h.nodeRepo.FindGroupIDsByNodeID(c.Request.Context(), node.ID)
	if err != nil {
		log.Printf("[admin] list node %d groups after update failed: %v", node.ID, err)
		newGroupIDs = oldGroupIDs
	}
	h.syncNodeChange(c.Request.Context(), node.ID, oldGroupIDs, newGroupIDs, oldEnabled, node.IsEnabled)

	response.Success(c, node)
}

// Delete 处理 DELETE /api/admin/nodes/:id — 删除节点。
func (h *AdminNodeHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	node, err := h.nodeRepo.FindByID(c.Request.Context(), id)
	if err == nil && node != nil {
		groupIDs, groupErr := h.nodeRepo.FindGroupIDsByNodeID(c.Request.Context(), node.ID)
		if groupErr != nil {
			log.Printf("[admin] list node %d groups before delete failed: %v", node.ID, groupErr)
		}
		h.syncNodeChange(c.Request.Context(), node.ID, groupIDs, nil, node.IsEnabled, false)
	}

	if err := h.nodeRepo.Delete(c.Request.Context(), id); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// List 处理 GET /api/admin/nodes — 节点列表。
func (h *AdminNodeHandler) List(c *gin.Context) {
	nodes, err := h.nodeRepo.List(c.Request.Context())
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{"nodes": nodes})
}

func (h *AdminNodeHandler) syncNodeChange(ctx context.Context, nodeID uint64, oldGroupIDs []uint64, newGroupIDs []uint64, oldEnabled bool, newEnabled bool) {
	if h.nodeAccessSvc == nil {
		return
	}

	syncGroups := func(groupIDs []uint64, action string) {
		if len(groupIDs) == 0 {
			return
		}
		if err := h.nodeAccessSvc.TriggerForNodeGroups(ctx, nodeID, groupIDs, action); err != nil {
			log.Printf("[admin] sync node %d action %s for groups %v failed: %v", nodeID, action, groupIDs, err)
		}
	}

	groupChanged := !sameUint64Slices(oldGroupIDs, newGroupIDs)
	if oldEnabled && (!newEnabled || groupChanged) {
		syncGroups(oldGroupIDs, "DISABLE_USER")
	}
	if newEnabled {
		syncGroups(newGroupIDs, "UPSERT_USER")
	}
}

func sameUint64Slices(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[uint64]int, len(a))
	for _, id := range a {
		seen[id]++
	}
	for _, id := range b {
		seen[id]--
		if seen[id] < 0 {
			return false
		}
	}
	return true
}

// AdminUserHandler 管理后台用户处理器。
type AdminUserHandler struct {
	userRepo          *repository.UserRepository
	subRepo           *repository.SubscriptionRepository
	tokenRepo         *repository.SubscriptionTokenRepository
	planRepo          *repository.PlanRepository
	nodeAccessSvc     nodeAccessSubscriptionSyncer
	bcryptRounds      int
	xrayUserKeyDomain string
}

// NewAdminUserHandler 创建管理后台用户处理器。
func NewAdminUserHandler(userRepo *repository.UserRepository, bcryptRounds ...int) *AdminUserHandler {
	rounds := bcrypt.DefaultCost
	if len(bcryptRounds) > 0 && bcryptRounds[0] > 0 {
		rounds = bcryptRounds[0]
	}
	return &AdminUserHandler{userRepo: userRepo, bcryptRounds: rounds, xrayUserKeyDomain: "suiyue.local"}
}

// NewAdminUserHandlerWithSubscription 创建带订阅管理能力的用户处理器。
func NewAdminUserHandlerWithSubscription(
	userRepo *repository.UserRepository,
	subRepo *repository.SubscriptionRepository,
	tokenRepo *repository.SubscriptionTokenRepository,
	planRepo *repository.PlanRepository,
	nodeAccessSvc nodeAccessSubscriptionSyncer,
	bcryptRounds int,
	xrayUserKeyDomain ...string,
) *AdminUserHandler {
	h := NewAdminUserHandler(userRepo, bcryptRounds)
	h.subRepo = subRepo
	h.tokenRepo = tokenRepo
	h.planRepo = planRepo
	h.nodeAccessSvc = nodeAccessSvc
	if len(xrayUserKeyDomain) > 0 && xrayUserKeyDomain[0] != "" {
		h.xrayUserKeyDomain = xrayUserKeyDomain[0]
	}
	return h
}

type nodeAccessSubscriptionSyncer interface {
	TriggerOnSubscribe(ctx context.Context, userID, subID, planID uint64) error
	TriggerOnRenew(ctx context.Context, userID, subID, planID uint64) error
	TriggerOnExpire(ctx context.Context, userID, subID, planID uint64) error
}

// List 处理 GET /api/admin/users — 用户列表。
func (h *AdminUserHandler) List(c *gin.Context) {
	page, size := parsePagination(c)
	keyword := c.DefaultQuery("keyword", "")

	ctx := c.Request.Context()

	var users []model.User
	var total int64
	var err error

	if keyword != "" {
		users, total, err = h.userRepo.SearchByUsername(ctx, keyword, page, size)
	} else {
		users, total, err = h.userRepo.ListPaginated(ctx, page, size)
	}
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	items := make([]gin.H, 0, len(users))
	plans := make(map[uint64]*model.Plan)
	for _, u := range users {
		item := adminUserListItem(&u)

		if h.subRepo != nil {
			subscription, err := h.userSubscriptionView(ctx, u.ID)
			if err != nil {
				response.HandleError(c, response.ErrInternalServer)
				return
			}
			item["has_active_subscription"] = subscription.hasActive
			if subscription.sub != nil {
				sub := subscription.sub
				item["subscription"] = subscriptionSummary(sub)
				item["subscription_status"] = sub.Status
				item["subscription_expire_date"] = sub.ExpireDate
				item["traffic_limit"] = sub.TrafficLimit
				item["used_traffic"] = sub.UsedTraffic
				item["traffic_unlimited"] = sub.TrafficLimit == 0
				item["remaining_traffic"] = remainingTraffic(sub)
				item["traffic_usage_percent"] = trafficUsagePercent(sub)
				if h.planRepo != nil {
					plan, ok := plans[sub.PlanID]
					if !ok {
						if p, err := h.planRepo.FindByID(ctx, sub.PlanID); err == nil {
							plan = p
						}
						plans[sub.PlanID] = plan
					}
					if plan != nil {
						item["plan"] = planSummary(plan)
						item["plan_name"] = plan.Name
					}
				}
			}
		}

		items = append(items, item)
	}

	response.Success(c, gin.H{
		"users": items,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

func adminUserListItem(u *model.User) gin.H {
	user := u.ToPublic()
	return gin.H{
		"id":            user.ID,
		"uuid":          user.UUID,
		"username":      user.Username,
		"email":         user.Email,
		"status":        user.Status,
		"is_admin":      user.IsAdmin,
		"last_login_at": user.LastLoginAt,
		"created_at":    user.CreatedAt,
	}
}

func (h *AdminUserHandler) userSubscriptionView(ctx context.Context, userID uint64) (adminTokenSubscriptionView, error) {
	sub, err := h.subRepo.FindActiveByUserID(ctx, userID)
	if err == nil {
		return adminTokenSubscriptionView{sub: sub, hasActive: true}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return adminTokenSubscriptionView{}, err
	}

	sub, err = h.subRepo.FindLatestByUserID(ctx, userID)
	if err == nil {
		return adminTokenSubscriptionView{sub: sub, hasActive: false}, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return adminTokenSubscriptionView{}, nil
	}
	return adminTokenSubscriptionView{}, err
}

func remainingTraffic(sub *model.UserSubscription) uint64 {
	if sub == nil || sub.TrafficLimit == 0 || sub.UsedTraffic >= sub.TrafficLimit {
		return 0
	}
	return sub.TrafficLimit - sub.UsedTraffic
}

func trafficUsagePercent(sub *model.UserSubscription) float64 {
	if sub == nil || sub.TrafficLimit == 0 {
		return 0
	}
	percent := float64(sub.UsedTraffic) / float64(sub.TrafficLimit) * 100
	if percent > 100 {
		return 100
	}
	return percent
}

// Create 处理 POST /api/admin/users — 管理员创建用户。
func (h *AdminUserHandler) Create(c *gin.Context) {
	var req model.AdminCreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.Status == "" {
		req.Status = "active"
	}

	if _, err := h.userRepo.FindByUsername(c.Request.Context(), req.Username); err == nil {
		response.HandleError(c, response.ErrUserExists)
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), h.bcryptRounds)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	uuid, err := secure.RandomUUID()
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	user := &model.User{
		UUID:         uuid,
		Username:     req.Username,
		PasswordHash: string(hash),
		XrayUserKey:  req.Username + "@" + h.xrayUserKeyDomain,
		Status:       req.Status,
		IsAdmin:      req.IsAdmin,
	}
	if req.Email != "" {
		user.Email = &req.Email
	}

	if err := h.userRepo.CreateWithSubscriptionToken(c.Request.Context(), user); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, user.ToPublic())
}

// Delete 处理 DELETE /api/admin/users/:id — 删除用户。
func (h *AdminUserHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if currentUserID, ok := middleware.GetUserID(c); ok && currentUserID == id {
		response.ErrorWithDetail(c, http.StatusBadRequest, 40005, "不能删除当前登录管理员", "")
		return
	}

	ctx := c.Request.Context()
	if _, err := h.userRepo.FindByID(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.HandleError(c, response.ErrNotFound)
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	var activeSub *model.UserSubscription
	if h.subRepo != nil {
		sub, err := h.subRepo.FindActiveByUserID(ctx, id)
		if err == nil {
			activeSub = sub
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
	}

	if activeSub != nil && h.nodeAccessSvc != nil {
		if err := h.nodeAccessSvc.TriggerOnExpire(ctx, id, activeSub.ID, activeSub.PlanID); err != nil {
			log.Printf("[admin] delete user node sync failed: user=%d sub=%d plan=%d err=%v", id, activeSub.ID, activeSub.PlanID, err)
		}
	}

	if err := h.userRepo.Delete(ctx, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.HandleError(c, response.ErrNotFound)
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// ToggleStatus 处理 PUT /api/admin/users/:id/status — 启用/禁用用户。
func (h *AdminUserHandler) ToggleStatus(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req struct {
		Status string `json:"status" binding:"required,oneof=active disabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if err := h.userRepo.UpdateStatus(c.Request.Context(), id, req.Status); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.syncUserStatusToNodes(c.Request.Context(), id, req.Status)

	response.Success(c, nil)
}

func (h *AdminUserHandler) syncUserStatusToNodes(ctx context.Context, userID uint64, status string) {
	if h.subRepo == nil || h.nodeAccessSvc == nil {
		return
	}
	sub, err := h.subRepo.FindActiveByUserID(ctx, userID)
	if err != nil || sub == nil {
		return
	}
	var syncErr error
	if status == "disabled" {
		syncErr = h.nodeAccessSvc.TriggerOnExpire(ctx, userID, sub.ID, sub.PlanID)
	} else {
		syncErr = h.nodeAccessSvc.TriggerOnRenew(ctx, userID, sub.ID, sub.PlanID)
	}
	if syncErr != nil {
		log.Printf("[admin] sync user status to nodes failed: user=%d sub=%d status=%s err=%v", userID, sub.ID, status, syncErr)
	}
}

// ResetPassword 处理 PUT /api/admin/users/:id/password — 管理员重置用户密码。
func (h *AdminUserHandler) ResetPassword(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req struct {
		NewPassword string `json:"new_password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), h.bcryptRounds)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	if err := h.userRepo.UpdatePassword(c.Request.Context(), id, string(hash)); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// GetSubscription 处理 GET /api/admin/users/:id/subscription — 查询用户最近订阅。
func (h *AdminUserHandler) GetSubscription(c *gin.Context) {
	if h.subRepo == nil || h.tokenRepo == nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if _, err := h.userRepo.FindByID(c.Request.Context(), userID); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	sub, err := h.subRepo.FindLatestByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			tokens, tokenErr := h.tokenRepo.FindByUserID(c.Request.Context(), userID)
			if tokenErr != nil {
				tokens = []model.SubscriptionToken{}
			}
			response.Success(c, gin.H{"subscription": nil, "tokens": tokens})
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	tokens, err := h.tokenRepo.FindByUserID(c.Request.Context(), userID)
	if err != nil {
		tokens = []model.SubscriptionToken{}
	}

	response.Success(c, gin.H{
		"subscription": sub,
		"tokens":       tokens,
	})
}

// UpsertSubscription 处理 PUT /api/admin/users/:id/subscription — 开通或调整用户订阅。
func (h *AdminUserHandler) UpsertSubscription(c *gin.Context) {
	if h.subRepo == nil || h.tokenRepo == nil || h.planRepo == nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req struct {
		PlanID        uint64  `json:"plan_id" binding:"required"`
		Status        string  `json:"status" binding:"omitempty,oneof=ACTIVE EXPIRED SUSPENDED PENDING"`
		ExpireDate    string  `json:"expire_date" binding:"required"`
		TrafficLimit  uint64  `json:"traffic_limit" binding:"min=0"`
		UsedTraffic   *uint64 `json:"used_traffic"`
		GenerateToken bool    `json:"generate_token"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.Status == "" {
		req.Status = "ACTIVE"
	}

	expireDate, err := parseAdminTime(req.ExpireDate)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if _, err := h.userRepo.FindByID(c.Request.Context(), userID); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}
	plan, err := h.planRepo.FindByID(c.Request.Context(), req.PlanID)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}
	if req.TrafficLimit == 0 {
		req.TrafficLimit = plan.TrafficLimit
	}

	ctx := c.Request.Context()
	oldSub, oldErr := h.subRepo.FindActiveByUserID(ctx, userID)
	if oldErr != nil && !errors.Is(oldErr, gorm.ErrRecordNotFound) {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	var savedSub model.UserSubscription
	created := oldSub == nil
	err = h.subRepo.WithTransaction(ctx, func(tx *gorm.DB) error {
		now := time.Now()
		usedTraffic := uint64(0)
		if req.UsedTraffic != nil {
			usedTraffic = *req.UsedTraffic
		}

		activeUserID := (*uint64)(nil)
		if req.Status == "ACTIVE" {
			activeUserID = &userID
		}

		if oldSub != nil {
			var sub model.UserSubscription
			if err := tx.First(&sub, oldSub.ID).Error; err != nil {
				return err
			}
			if req.UsedTraffic == nil {
				usedTraffic = sub.UsedTraffic
			}
			updates := map[string]interface{}{
				"plan_id":        req.PlanID,
				"expire_date":    expireDate,
				"traffic_limit":  req.TrafficLimit,
				"used_traffic":   usedTraffic,
				"status":         req.Status,
				"active_user_id": activeUserID,
			}
			if err := tx.Model(&model.UserSubscription{}).Where("id = ?", sub.ID).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.First(&savedSub, sub.ID).Error; err != nil {
				return err
			}
		} else {
			savedSub = model.UserSubscription{
				UserID:       userID,
				PlanID:       req.PlanID,
				StartDate:    now,
				ExpireDate:   expireDate,
				TrafficLimit: req.TrafficLimit,
				UsedTraffic:  usedTraffic,
				Status:       req.Status,
				ActiveUserID: activeUserID,
			}
			if err := tx.Create(&savedSub).Error; err != nil {
				return err
			}
		}

		if req.GenerateToken || req.Status == "ACTIVE" {
			if err := ensureValidSubscriptionTokenTx(tx, userID, savedSub.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.syncSubscriptionChange(ctx, userID, oldSub, &savedSub, created)

	tokens, err := h.tokenRepo.FindByUserID(ctx, userID)
	if err != nil {
		tokens = []model.SubscriptionToken{}
	}
	response.Success(c, gin.H{"subscription": savedSub, "tokens": tokens})
}

func (h *AdminUserHandler) syncSubscriptionChange(ctx context.Context, userID uint64, oldSub *model.UserSubscription, newSub *model.UserSubscription, created bool) {
	if h.nodeAccessSvc == nil || newSub == nil {
		return
	}
	if oldSub != nil && (newSub.Status != "ACTIVE" || oldSub.PlanID != newSub.PlanID) {
		if err := h.nodeAccessSvc.TriggerOnExpire(ctx, userID, oldSub.ID, oldSub.PlanID); err != nil {
			log.Printf("[admin] disable old subscription nodes failed: user=%d sub=%d err=%v", userID, oldSub.ID, err)
		}
	}
	if newSub.Status != "ACTIVE" {
		return
	}
	if created {
		if err := h.nodeAccessSvc.TriggerOnSubscribe(ctx, userID, newSub.ID, newSub.PlanID); err != nil {
			log.Printf("[admin] subscribe node sync failed: user=%d sub=%d err=%v", userID, newSub.ID, err)
		}
		return
	}
	if err := h.nodeAccessSvc.TriggerOnRenew(ctx, userID, newSub.ID, newSub.PlanID); err != nil {
		log.Printf("[admin] renew node sync failed: user=%d sub=%d err=%v", userID, newSub.ID, err)
	}
}

func parseAdminTime(value string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	var lastErr error
	for _, format := range formats {
		t, err := time.Parse(format, value)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, lastErr
}

func ensureValidSubscriptionTokenTx(tx *gorm.DB, userID uint64, subID uint64) error {
	subIDCopy := subID
	_, err := repository.EnsureSubscriptionTokenTx(tx, userID, &subIDCopy, nil)
	return err
}

// AdminSubscriptionTokenHandler 管理后台订阅 Token 处理器。
type AdminSubscriptionTokenHandler struct {
	tokenRepo  *repository.SubscriptionTokenRepository
	subRepo    *repository.SubscriptionRepository
	userRepo   *repository.UserRepository
	planRepo   *repository.PlanRepository
	nodeRepo   *repository.NodeRepository
	nodeAccSvc interface {
		TriggerOnExpire(ctx context.Context, userID, subID, planID uint64) error
	}
}

// NewAdminSubscriptionTokenHandler 创建管理后台订阅 Token 处理器。
func NewAdminSubscriptionTokenHandler(
	tokenRepo *repository.SubscriptionTokenRepository,
	subRepo *repository.SubscriptionRepository,
	userRepo *repository.UserRepository,
	planRepo *repository.PlanRepository,
	nodeRepo *repository.NodeRepository,
	nodeAccSvc interface {
		TriggerOnExpire(ctx context.Context, userID, subID, planID uint64) error
	},
) *AdminSubscriptionTokenHandler {
	return &AdminSubscriptionTokenHandler{
		tokenRepo:  tokenRepo,
		subRepo:    subRepo,
		userRepo:   userRepo,
		planRepo:   planRepo,
		nodeRepo:   nodeRepo,
		nodeAccSvc: nodeAccSvc,
	}
}

// CreateToken 处理 POST /api/admin/subscription-tokens — 为用户生成订阅 Token。
func (h *AdminSubscriptionTokenHandler) CreateToken(c *gin.Context) {
	var req struct {
		UserID    uint64 `json:"user_id" binding:"required"`
		ExpiresAt string `json:"expires_at"` // 可选，格式 2006-01-02T15:04:05Z
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	ctx := c.Request.Context()

	if _, err := h.userRepo.FindByID(ctx, req.UserID); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			expiresAt = &t
		} else {
			response.HandleError(c, response.ErrBadRequest)
			return
		}
	}

	subID, err := h.activeSubscriptionID(ctx, req.UserID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	st, err := h.tokenRepo.EnsureActiveByUserID(ctx, req.UserID, subID, expiresAt)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{"token": st.Token, "id": st.ID, "subscription_id": st.SubscriptionID})
}

// ListTokens 处理 GET /api/admin/subscription-tokens — Token 列表。
func (h *AdminSubscriptionTokenHandler) ListTokens(c *gin.Context) {
	page, size := parsePagination(c)

	ctx := c.Request.Context()
	tokens, total, err := h.tokenRepo.ListPaginated(ctx, page, size)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	items := make([]gin.H, 0, len(tokens))
	users := make(map[uint64]*model.UserPublic)
	subscriptions := make(map[uint64]adminTokenSubscriptionView)
	plans := make(map[uint64]*model.Plan)
	now := time.Now()
	for _, token := range tokens {
		user, ok := users[token.UserID]
		if !ok {
			if u, err := h.userRepo.FindByID(ctx, token.UserID); err == nil {
				user = u.ToPublic()
			}
			users[token.UserID] = user
		}
		subscription, ok := subscriptions[token.UserID]
		if !ok {
			subscription, err = h.tokenSubscriptionView(ctx, token.UserID)
			if err != nil {
				response.HandleError(c, response.ErrInternalServer)
				return
			}
			subscriptions[token.UserID] = subscription
		}

		var plan *model.Plan
		if subscription.sub != nil {
			plan, ok = plans[subscription.sub.PlanID]
			if !ok {
				if p, err := h.planRepo.FindByID(ctx, subscription.sub.PlanID); err == nil {
					plan = p
				}
				plans[subscription.sub.PlanID] = plan
			}
		}

		tokenStatus := "ACTIVE"
		isExpired := token.ExpiresAt != nil && !token.ExpiresAt.After(now)
		if token.IsRevoked {
			tokenStatus = "REVOKED"
		} else if isExpired {
			tokenStatus = "EXPIRED"
		}

		item := gin.H{
			"id":                      token.ID,
			"user_id":                 token.UserID,
			"subscription_id":         token.SubscriptionID,
			"token":                   token.Token,
			"is_revoked":              token.IsRevoked,
			"is_expired":              isExpired,
			"is_usable":               !token.IsRevoked && !isExpired,
			"token_status":            tokenStatus,
			"last_used_at":            token.LastUsedAt,
			"expires_at":              token.ExpiresAt,
			"created_at":              token.CreatedAt,
			"has_active_subscription": subscription.hasActive,
		}
		if user != nil {
			item["username"] = user.Username
			item["email"] = user.Email
			item["user"] = user
		}
		if subscription.sub != nil {
			item["subscription"] = subscriptionSummary(subscription.sub)
			item["subscription_status"] = subscription.sub.Status
			item["subscription_expire_date"] = subscription.sub.ExpireDate
		}
		if plan != nil {
			item["plan"] = planSummary(plan)
			item["plan_name"] = plan.Name
		}
		items = append(items, item)
	}

	response.Success(c, gin.H{
		"tokens": items,
		"total":  total,
		"page":   page,
		"size":   size,
	})
}

type adminTokenSubscriptionView struct {
	sub       *model.UserSubscription
	hasActive bool
}

func (h *AdminSubscriptionTokenHandler) tokenSubscriptionView(ctx context.Context, userID uint64) (adminTokenSubscriptionView, error) {
	sub, err := h.subRepo.FindActiveByUserID(ctx, userID)
	if err == nil {
		return adminTokenSubscriptionView{sub: sub, hasActive: true}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return adminTokenSubscriptionView{}, err
	}

	sub, err = h.subRepo.FindLatestByUserID(ctx, userID)
	if err == nil {
		return adminTokenSubscriptionView{sub: sub, hasActive: false}, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return adminTokenSubscriptionView{}, nil
	}
	return adminTokenSubscriptionView{}, err
}

func subscriptionSummary(sub *model.UserSubscription) gin.H {
	return gin.H{
		"id":            sub.ID,
		"user_id":       sub.UserID,
		"plan_id":       sub.PlanID,
		"start_date":    sub.StartDate,
		"expire_date":   sub.ExpireDate,
		"traffic_limit": sub.TrafficLimit,
		"used_traffic":  sub.UsedTraffic,
		"status":        sub.Status,
		"created_at":    sub.CreatedAt,
		"updated_at":    sub.UpdatedAt,
	}
}

func planSummary(plan *model.Plan) gin.H {
	return gin.H{
		"id":            plan.ID,
		"name":          plan.Name,
		"price":         plan.Price,
		"currency":      plan.Currency,
		"traffic_limit": plan.TrafficLimit,
		"duration_days": plan.DurationDays,
		"is_active":     plan.IsActive,
	}
}

// RevokeToken 处理 POST /api/admin/subscription-tokens/:id/revoke — 撤销 Token。
func (h *AdminSubscriptionTokenHandler) RevokeToken(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	ctx := c.Request.Context()
	st, err := h.tokenRepo.FindByID(ctx, id)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	if st.IsRevoked {
		response.HandleError(c, &response.AppError{
			Code:     40009,
			HTTPCode: 400,
			Message:  "Token 已撤销",
		})
		return
	}

	if err := h.tokenRepo.Revoke(ctx, id); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, nil)
}

// ResetToken 处理 POST /api/admin/subscription-tokens/:id/reset — 管理员重置用户订阅 Token。
func (h *AdminSubscriptionTokenHandler) ResetToken(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req struct {
		ExpiresAt string `json:"expires_at"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.HandleError(c, response.ErrBadRequest)
			return
		}
	}

	ctx := c.Request.Context()
	oldToken, err := h.tokenRepo.FindByID(ctx, id)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}
	if _, err := h.userRepo.FindByID(ctx, oldToken.UserID); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, req.ExpiresAt)
		if err != nil {
			response.HandleError(c, response.ErrBadRequest)
			return
		}
		expiresAt = &t
	}

	subID, err := h.activeSubscriptionID(ctx, oldToken.UserID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	newToken, err := h.tokenRepo.ResetByID(ctx, oldToken.ID, subID, expiresAt)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, gin.H{"token": newToken.Token, "id": newToken.ID, "subscription_id": newToken.SubscriptionID})
}

func (h *AdminSubscriptionTokenHandler) activeSubscriptionID(ctx context.Context, userID uint64) (*uint64, error) {
	sub, err := h.subRepo.FindActiveByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	id := sub.ID
	return &id, nil
}
