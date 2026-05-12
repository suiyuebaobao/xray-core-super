package handler

import (
	"encoding/json"
	"errors"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"
	"suiyue/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type SiteConfigHandler struct {
	settingRepo     *repository.SiteSettingRepository
	operationLogSvc *service.OperationLogService
}

func NewSiteConfigHandler(settingRepo *repository.SiteSettingRepository, operationLogSvc ...*service.OperationLogService) *SiteConfigHandler {
	var logSvc *service.OperationLogService
	if len(operationLogSvc) > 0 {
		logSvc = operationLogSvc[0]
	}
	return &SiteConfigHandler{settingRepo: settingRepo, operationLogSvc: logSvc}
}

func (h *SiteConfigHandler) GetSalesLanding(c *gin.Context) {
	cfg, err := h.loadSalesLanding(c)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, cfg)
}

func (h *SiteConfigHandler) UpdateSalesLanding(c *gin.Context) {
	var req model.SalesLandingConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	cfg := model.NormalizeSalesLandingConfig(req)
	data, err := json.Marshal(cfg)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if _, err := h.settingRepo.Upsert(c.Request.Context(), model.SiteSettingSalesLanding, string(data)); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.recordAdminOperation(c, "update_sales_landing", "success", "管理员更新销售首页", map[string]interface{}{
		"title": cfg.Title,
	})
	response.Success(c, cfg)
}

func (h *SiteConfigHandler) GetSubscriptionConfig(c *gin.Context) {
	cfg, err := h.loadSubscriptionConfig(c)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	response.Success(c, cfg)
}

func (h *SiteConfigHandler) UpdateSubscriptionConfig(c *gin.Context) {
	var req struct {
		ProfileName           string   `json:"profile_name"`
		CustomRules           []string `json:"custom_rules"`
		IncludeUserInfo       *bool    `json:"include_user_info"`
		ProfileUpdateInterval uint     `json:"profile_update_interval"`
		ProfileWebPageURL     string   `json:"profile_web_page_url"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	includeUserInfo := true
	if req.IncludeUserInfo != nil {
		includeUserInfo = *req.IncludeUserInfo
	}
	cfg := model.NormalizeSubscriptionConfig(model.SubscriptionConfig{
		ProfileName:           req.ProfileName,
		CustomRules:           req.CustomRules,
		IncludeUserInfo:       includeUserInfo,
		ProfileUpdateInterval: req.ProfileUpdateInterval,
		ProfileWebPageURL:     req.ProfileWebPageURL,
	})
	data, err := json.Marshal(cfg)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	if _, err := h.settingRepo.Upsert(c.Request.Context(), model.SiteSettingSubscriptionConfig, string(data)); err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	h.recordAdminOperation(c, "update_subscription_config", "success", "管理员更新订阅配置", map[string]interface{}{
		"profile_name":      cfg.ProfileName,
		"custom_rule_count": len(cfg.CustomRules),
		"include_user_info": cfg.IncludeUserInfo,
	})
	response.Success(c, cfg)
}

func (h *SiteConfigHandler) loadSalesLanding(c *gin.Context) (model.SalesLandingConfig, error) {
	if h == nil || h.settingRepo == nil {
		return model.DefaultSalesLandingConfig(), nil
	}
	setting, err := h.settingRepo.FindByKey(c.Request.Context(), model.SiteSettingSalesLanding)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.DefaultSalesLandingConfig(), nil
		}
		return model.SalesLandingConfig{}, err
	}
	return model.ParseSalesLandingConfig(setting.Value), nil
}

func (h *SiteConfigHandler) loadSubscriptionConfig(c *gin.Context) (model.SubscriptionConfig, error) {
	if h == nil || h.settingRepo == nil {
		return model.DefaultSubscriptionConfig(), nil
	}
	setting, err := h.settingRepo.FindByKey(c.Request.Context(), model.SiteSettingSubscriptionConfig)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.DefaultSubscriptionConfig(), nil
		}
		return model.SubscriptionConfig{}, err
	}
	return model.ParseSubscriptionConfig(setting.Value), nil
}

func (h *SiteConfigHandler) recordAdminOperation(c *gin.Context, action, result, summary string, extra interface{}) {
	if h == nil || h.operationLogSvc == nil {
		return
	}
	targetType := "site_setting"
	_ = h.operationLogSvc.Record(c.Request.Context(), buildClientLogContext(c), "admin", action, result, summary, &targetType, nil, extra)
}
