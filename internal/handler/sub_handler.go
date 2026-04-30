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

	"suiyue/internal/platform/response"
	"suiyue/internal/subscription"

	"github.com/gin-gonic/gin"
)

// SubHandler 订阅下载处理器。
type SubHandler struct {
	gen *subscription.Generator
}

// NewSubHandler 创建订阅下载处理器。
func NewSubHandler(gen *subscription.Generator) *SubHandler {
	return &SubHandler{gen: gen}
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
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", result.Filename))

	// 直接写入响应体
	c.String(http.StatusOK, result.Content)
}
