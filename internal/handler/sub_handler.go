// sub_handler.go — 订阅下载 HTTP 处理器。
//
// 职责：
// - 处理 GET /sub/:token/clash
// - 处理 GET /sub/:token/base64
// - 处理 GET /sub/:token/plain
// - 设置正确的 Content-Type 和 Content-Disposition 响应头
// - 支持 ETag 缓存
package handler

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"suiyue/internal/platform/response"
	"suiyue/internal/service"
	"suiyue/internal/subscription"

	"github.com/gin-gonic/gin"
)

// SubHandler 订阅下载处理器。
type SubHandler struct {
	gen             *subscription.Generator
	operationLogSvc *service.OperationLogService
}

// NewSubHandler 创建订阅下载处理器。
func NewSubHandler(gen *subscription.Generator, operationLogSvc ...*service.OperationLogService) *SubHandler {
	var logSvc *service.OperationLogService
	if len(operationLogSvc) > 0 {
		logSvc = operationLogSvc[0]
	}
	return &SubHandler{gen: gen, operationLogSvc: logSvc}
}

// Download 处理 GET /sub/:token/:format。
// format 支持：clash、base64、plain。
func (h *SubHandler) Download(c *gin.Context) {
	token := c.Param("token")
	format := c.Param("format")

	// 校验格式
	if format != "clash" && format != "base64" && format != "plain" {
		response.HandleError(c, response.ErrBadRequest)
		return
	}

	result, err := h.gen.GenerateByToken(c.Request.Context(), token, format)
	if err != nil {
		response.HandleError(c, err)
		return
	}

	if h.operationLogSvc != nil && result.User != nil {
		logCtx := buildClientLogContext(c)
		targetType := "subscription_token"
		tokenSuffix := token
		if len(tokenSuffix) > 6 {
			tokenSuffix = tokenSuffix[len(tokenSuffix)-6:]
		}
		_ = h.operationLogSvc.Record(c.Request.Context(), logCtx, "user", "download_subscription", "success", "用户下载订阅", &targetType, nil, map[string]interface{}{
			"user_id":      result.User.ID,
			"format":       format,
			"token_suffix": tokenSuffix,
		})
	}

	// 设置 ETag
	c.Header("ETag", fmt.Sprintf(`"%s"`, result.ETag))

	// 检查 If-None-Match
	ifNoneMatch := c.GetHeader("If-None-Match")
	if ifNoneMatch == fmt.Sprintf(`"%s"`, result.ETag) {
		c.Status(http.StatusNotModified)
		return
	}

	// 设置响应头
	c.Header("Content-Type", result.ContentType)
	c.Header("Content-Disposition", subscriptionContentDisposition(result.Filename))

	// 直接写入响应体
	c.String(http.StatusOK, result.Content)
}

func subscriptionContentDisposition(filename string) string {
	fallback := asciiFilenameFallback(filename)
	return fmt.Sprintf("attachment; filename=\"%s\"; filename*=UTF-8''%s", fallback, url.PathEscape(filename))
}

func asciiFilenameFallback(filename string) string {
	var b strings.Builder
	for _, r := range filename {
		if r >= 0x20 && r <= 0x7e && r != '"' && r != '\\' && r != ';' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}
	fallback := strings.TrimSpace(b.String())
	if fallback == "" {
		return "RayPilot.yaml"
	}
	return fallback
}
