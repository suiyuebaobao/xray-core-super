package handler

import (
	"strconv"

	"suiyue/internal/platform/response"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

type AdminLogHandler struct {
	runtimeSvc    *service.RuntimeLogService
	deploymentSvc *service.DeploymentLogService
	operationSvc  *service.OperationLogService
}

func NewAdminLogHandler(runtimeSvc *service.RuntimeLogService, deploymentSvc *service.DeploymentLogService, operationSvc *service.OperationLogService) *AdminLogHandler {
	return &AdminLogHandler{
		runtimeSvc:    runtimeSvc,
		deploymentSvc: deploymentSvc,
		operationSvc:  operationSvc,
	}
}

func (h *AdminLogHandler) Runtime(c *gin.Context) {
	source := c.DefaultQuery("source", "api")
	lineCount := parsePositiveInt(c.DefaultQuery("lines", "200"), 200)
	keyword := c.Query("keyword")
	date := c.Query("date")
	hour := c.Query("hour")
	lines, err := h.runtimeSvc.Read(c.Request.Context(), source, lineCount, keyword, date, hour)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, gin.H{
		"source": source,
		"date":   date,
		"hour":   hour,
		"lines":  lines,
		"count":  len(lines),
	})
}

func (h *AdminLogHandler) Deployments(c *gin.Context) {
	page, size := parsePagination(c)
	deployType := c.Query("deploy_type")
	result := c.Query("result")
	keyword := c.Query("keyword")
	items, total, err := h.deploymentSvc.List(c.Request.Context(), page, size, deployType, result, keyword)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, gin.H{
		"logs":  items,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

func (h *AdminLogHandler) Operations(c *gin.Context) {
	page, size := parsePagination(c)
	actorType := c.Query("actor_type")
	action := c.Query("action")
	keyword := c.Query("keyword")
	items, total, err := h.operationSvc.List(c.Request.Context(), page, size, actorType, action, keyword)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, gin.H{
		"logs":  items,
		"total": total,
		"page":  page,
		"size":  size,
	})
}

func parsePositiveInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
