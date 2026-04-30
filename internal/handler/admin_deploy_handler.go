// admin_deploy_handler.go — 节点一键部署处理器。
//
// 职责：处理管理后台的一键部署请求，通过 SSH 连接到目标服务器
// 自动安装 Docker、推送镜像、启动容器并创建节点记录。
package handler

import (
	"net/http"

	"suiyue/internal/platform/response"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

// NodeDeployHandler 节点部署处理器。
type NodeDeployHandler struct {
	deploySvc *service.NodeDeployService
}

// NewNodeDeployHandler 创建节点部署处理器。
func NewNodeDeployHandler(deploySvc *service.NodeDeployService) *NodeDeployHandler {
	return &NodeDeployHandler{deploySvc: deploySvc}
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

	result, err := h.deploySvc.Deploy(c.Request.Context(), &req)
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

// RelayDeployHandler 中转节点部署处理器。
type RelayDeployHandler struct {
	deploySvc *service.RelayDeployService
}

// NewRelayDeployHandler 创建中转节点部署处理器。
func NewRelayDeployHandler(deploySvc *service.RelayDeployService) *RelayDeployHandler {
	return &RelayDeployHandler{deploySvc: deploySvc}
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

	result, err := h.deploySvc.Deploy(c.Request.Context(), &req)
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
