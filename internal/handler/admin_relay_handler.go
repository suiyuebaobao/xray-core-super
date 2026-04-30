package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
)

// AdminRelayHandler 管理后台中转节点处理器。
type AdminRelayHandler struct {
	relayRepo   *repository.RelayRepository
	backendRepo *repository.RelayBackendRepository
	relaySvc    *service.RelayService
}

// NewAdminRelayHandler 创建管理后台中转节点处理器。
func NewAdminRelayHandler(relayRepo *repository.RelayRepository, backendRepo *repository.RelayBackendRepository, relaySvc *service.RelayService) *AdminRelayHandler {
	return &AdminRelayHandler{
		relayRepo:   relayRepo,
		backendRepo: backendRepo,
		relaySvc:    relaySvc,
	}
}

type relayListItem struct {
	model.Relay
	Backends     []model.RelayBackend `json:"backends"`
	BackendCount int                  `json:"backend_count"`
}

// List 处理 GET /api/admin/relays — 中转节点列表。
func (h *AdminRelayHandler) List(c *gin.Context) {
	relays, err := h.relayRepo.List(c.Request.Context())
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	items := make([]relayListItem, 0, len(relays))
	for _, relay := range relays {
		backends, err := h.backendRepo.ListByRelayID(c.Request.Context(), relay.ID)
		if err != nil {
			response.HandleError(c, response.ErrInternalServer)
			return
		}
		items = append(items, relayListItem{
			Relay:        relay,
			Backends:     backends,
			BackendCount: len(backends),
		})
	}

	response.Success(c, gin.H{"relays": items})
}

// Create 处理 POST /api/admin/relays — 创建中转节点。
func (h *AdminRelayHandler) Create(c *gin.Context) {
	var req model.CreateRelayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.ForwarderType == "" {
		req.ForwarderType = "haproxy"
	}

	tokenHash := sha256.Sum256([]byte(req.AgentToken))
	relay, err := h.relayRepo.Create(c.Request.Context(), &model.Relay{
		Name:           req.Name,
		Host:           req.Host,
		ForwarderType:  req.ForwarderType,
		AgentBaseURL:   req.AgentBaseURL,
		AgentTokenHash: hex.EncodeToString(tokenHash[:]),
		Status:         "offline",
		IsEnabled:      req.IsEnabled,
	})
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	response.Success(c, relay)
}

// Update 处理 PUT /api/admin/relays/:id — 更新中转节点。
func (h *AdminRelayHandler) Update(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req model.UpdateRelayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if req.ForwarderType == "" {
		req.ForwarderType = "haproxy"
	}

	relay, err := h.relayRepo.FindByID(c.Request.Context(), id)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	relay.Name = req.Name
	relay.Host = req.Host
	relay.ForwarderType = req.ForwarderType
	relay.AgentBaseURL = req.AgentBaseURL
	if req.AgentToken != "" {
		tokenHash := sha256.Sum256([]byte(req.AgentToken))
		relay.AgentTokenHash = hex.EncodeToString(tokenHash[:])
	}
	relay.IsEnabled = req.IsEnabled

	if err := h.relayRepo.Update(c.Request.Context(), relay); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, relay)
}

// Delete 处理 DELETE /api/admin/relays/:id — 删除中转节点。
func (h *AdminRelayHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if err := h.relayRepo.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, repository.ErrRelayHasEnabledBackends) {
			c.JSON(http.StatusBadRequest, response.Response{
				Success: false,
				Message: err.Error(),
				Code:    40001,
			})
			return
		}
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, nil)
}

// ListBackends 处理 GET /api/admin/relays/:id/backends — 查询中转后端绑定。
func (h *AdminRelayHandler) ListBackends(c *gin.Context) {
	relayID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	if _, err := h.relayRepo.FindByID(c.Request.Context(), relayID); err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}
	backends, err := h.backendRepo.ListByRelayID(c.Request.Context(), relayID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, gin.H{"backends": backends})
}

// SaveBackends 处理 PUT /api/admin/relays/:id/backends — 保存中转后端绑定。
func (h *AdminRelayHandler) SaveBackends(c *gin.Context) {
	relayID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	var req model.SaveRelayBackendsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	backends, err := h.relaySvc.SaveBackends(c.Request.Context(), relayID, req.Backends)
	if err != nil {
		c.JSON(http.StatusBadRequest, response.Response{
			Success: false,
			Message: err.Error(),
			Code:    40001,
		})
		return
	}
	response.Success(c, gin.H{"backends": backends})
}
