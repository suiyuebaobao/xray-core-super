// admin_deploy_handler.go — 节点一键部署处理器。
//
// 职责：处理管理后台的一键部署请求，通过 SSH 连接到目标服务器
// 自动安装 Docker、推送镜像、启动容器并创建节点记录。
package handler

import (
	"net/http"
	"time"

	"suiyue/internal/platform/response"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

// NodeDeployHandler 节点部署处理器。
type NodeDeployHandler struct {
	deploySvc        *service.NodeDeployService
	deploymentLogSvc *service.DeploymentLogService
}

// NewNodeDeployHandler 创建节点部署处理器。
func NewNodeDeployHandler(deploySvc *service.NodeDeployService, deploymentLogSvc ...*service.DeploymentLogService) *NodeDeployHandler {
	var logSvc *service.DeploymentLogService
	if len(deploymentLogSvc) > 0 {
		logSvc = deploymentLogSvc[0]
	}
	return &NodeDeployHandler{deploySvc: deploySvc, deploymentLogSvc: logSvc}
}

// Deploy 处理 POST /api/admin/nodes/deploy — 一键部署节点。
func (h *NodeDeployHandler) Deploy(c *gin.Context) {
	var req service.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if req.SSHPort == 0 {
		req.SSHPort = 22
	}

	startedAt := time.Now()
	result, err := h.deploySvc.Deploy(c.Request.Context(), &req)
	h.recordNodeDeploymentLog(c, &req, result, err, time.Since(startedAt))
	if err != nil {
		c.JSON(http.StatusInternalServerError, response.Response{
			Success: false,
			Message: "部署失败: " + err.Error(),
			Code:    50001,
			Data:    result,
		})
		return
	}

	response.Success(c, result)
}

// ScanIPs 处理 POST /api/admin/nodes/deploy/scan-ips — 扫描服务器出口 IP。
func (h *NodeDeployHandler) ScanIPs(c *gin.Context) {
	var req service.ScanIPsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if req.SSHPort == 0 {
		req.SSHPort = 22
	}

	result, err := h.deploySvc.ScanIPs(c.Request.Context(), &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, response.Response{
			Success: false,
			Message: "扫描失败: " + err.Error(),
			Code:    50002,
			Data:    result,
		})
		return
	}

	response.Success(c, result)
}

// RelayDeployHandler 中转节点部署处理器。
type RelayDeployHandler struct {
	deploySvc        *service.RelayDeployService
	deploymentLogSvc *service.DeploymentLogService
}

// NewRelayDeployHandler 创建中转节点部署处理器。
func NewRelayDeployHandler(deploySvc *service.RelayDeployService, deploymentLogSvc ...*service.DeploymentLogService) *RelayDeployHandler {
	var logSvc *service.DeploymentLogService
	if len(deploymentLogSvc) > 0 {
		logSvc = deploymentLogSvc[0]
	}
	return &RelayDeployHandler{deploySvc: deploySvc, deploymentLogSvc: logSvc}
}

// Deploy 处理 POST /api/admin/relays/deploy — 一键部署中转节点。
func (h *RelayDeployHandler) Deploy(c *gin.Context) {
	var req service.RelayDeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	if req.ForwarderType == "" {
		req.ForwarderType = "haproxy"
	}

	startedAt := time.Now()
	result, err := h.deploySvc.Deploy(c.Request.Context(), &req)
	h.recordRelayDeploymentLog(c, &req, result, err, time.Since(startedAt))
	if err != nil {
		c.JSON(http.StatusInternalServerError, response.Response{
			Success: false,
			Message: "部署失败: " + err.Error(),
			Code:    50001,
			Data:    result,
		})
		return
	}

	response.Success(c, result)
}

func (h *NodeDeployHandler) recordNodeDeploymentLog(c *gin.Context, req *service.DeployRequest, result *service.DeployResult, err error, duration time.Duration) {
	if h == nil || h.deploymentLogSvc == nil {
		return
	}
	ctx := buildClientLogContext(c)
	var resultStatus string
	var detail *string
	if err != nil {
		resultStatus = "failed"
		msg := err.Error()
		detail = &msg
	} else {
		resultStatus = "success"
	}
	var nodeID *uint64
	var nodeHostID *uint64
	var nodeIDs []uint64
	var steps []service.Step
	if result != nil {
		if result.NodeID != 0 {
			nodeID = &result.NodeID
		}
		if result.NodeHostID != 0 {
			nodeHostID = &result.NodeHostID
		}
		nodeIDs = result.NodeIDs
		steps = result.Steps
	}
	_ = h.deploymentLogSvc.Record(c.Request.Context(), ctx, "exit_deploy", req.SSHHost, "exit", resultStatus, sanitizeNodeDeployRequest(req), duration, nodeID, nodeIDs, nodeHostID, nil, nil, steps, detail)
}

func (h *RelayDeployHandler) recordRelayDeploymentLog(c *gin.Context, req *service.RelayDeployRequest, result *service.RelayDeployResult, err error, duration time.Duration) {
	if h == nil || h.deploymentLogSvc == nil {
		return
	}
	ctx := buildClientLogContext(c)
	var resultStatus string
	var detail *string
	if err != nil {
		resultStatus = "failed"
		msg := err.Error()
		detail = &msg
	} else {
		resultStatus = "success"
	}
	var relayID *uint64
	var backendIDs []uint64
	var steps []service.Step
	var targetHost string
	var targetPort uint32
	if result != nil {
		if result.RelayID != 0 {
			relayID = &result.RelayID
		}
		backendIDs = result.BackendIDs
		steps = result.Steps
		targetHost = result.TargetHost
		targetPort = result.TargetPort
	}
	_ = h.deploymentLogSvc.Record(c.Request.Context(), ctx, "relay_deploy", req.SSHHost, "relay", resultStatus, sanitizeRelayDeployRequest(req, targetHost, targetPort), duration, nil, nil, nil, relayID, backendIDs, steps, detail)
}

func sanitizeNodeDeployRequest(req *service.DeployRequest) map[string]interface{} {
	if req == nil {
		return nil
	}
	return map[string]interface{}{
		"ssh_host":              req.SSHHost,
		"ssh_port":              req.SSHPort,
		"ssh_user":              req.SSHUser,
		"center_url":            req.CenterURL,
		"target_server_ip":      req.SSHHost,
		"node_name":             req.NodeName,
		"transport":             req.Transport,
		"transports":            req.Transports,
		"tcp_port":              req.TCPPort,
		"xhttp_port":            req.XHTTPPort,
		"xhttp_path":            req.XHTTPPath,
		"xhttp_host":            req.XHTTPHost,
		"xhttp_mode":            req.XHTTPMode,
		"multi_ip_enabled":      req.MultiIPEnabled,
		"selected_ips":          req.SelectedIPs,
		"node_group_ids":        req.NodeGroupIDs,
		"replace_existing_role": req.ReplaceExistingRole,
		"node_token_provided":   req.NodeToken != "",
	}
}

func sanitizeRelayDeployRequest(req *service.RelayDeployRequest, targetHost string, targetPort uint32) map[string]interface{} {
	if req == nil {
		return nil
	}
	if targetPort == 0 {
		targetPort = req.TargetPort
	}
	return map[string]interface{}{
		"ssh_host":              req.SSHHost,
		"ssh_port":              req.SSHPort,
		"ssh_user":              req.SSHUser,
		"center_url":            req.CenterURL,
		"target_server_ip":      req.SSHHost,
		"relay_entry_ip":        req.SSHHost,
		"relay_name":            req.RelayName,
		"forwarder_type":        req.ForwarderType,
		"exit_node_id":          req.ExitNodeID,
		"listen_port":           req.ListenPort,
		"target_port":           targetPort,
		"target_host":           targetHost,
		"backend_name":          req.BackendName,
		"replace_existing_role": req.ReplaceExistingRole,
		"relay_token_provided":  req.RelayToken != "",
	}
}
