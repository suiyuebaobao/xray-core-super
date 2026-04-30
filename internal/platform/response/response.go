// Package response 提供统一的 HTTP 响应格式。
//
// 所有 API 接口统一使用 JSON 响应，结构为：
//
//	{
//	  "success": true/false,
//	  "message": "描述信息",
//	  "code": 200,
//	  "data": { ... }  // 可选
//	}
//
// 错误处理通过 AppError 定义，包含业务错误码和 HTTP 状态码，
// 通过 HandleError 统一写入响应。
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Response 统一 API 响应结构。
type Response struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Code    int         `json:"code"`
	Data    interface{} `json:"data,omitempty"`
}

// Success 返回成功响应。
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Message: "success",
		Code:    0,
		Data:    data,
	})
}

// SuccessWithMessage 返回带自定义消息的成功响应。
func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Message: message,
		Code:    0,
		Data:    data,
	})
}

// AppError 应用级错误定义。
// Code 为业务错误码，HTTPCode 为对应的 HTTP 状态码。
type AppError struct {
	Code      int    `json:"code"`
	HTTPCode  int    `json:"-"`
	Message   string `json:"message"`
	Detail    string `json:"-"` // 内部日志用，不返回给客户端
}

// Error 实现 error 接口。
func (e *AppError) Error() string {
	if e.Detail != "" {
		return e.Detail
	}
	return e.Message
}

// 预定义的业务错误。
var (
	ErrBadRequest       = &AppError{Code: 40001, HTTPCode: http.StatusBadRequest, Message: "请求参数错误"}
	ErrUnauthorized     = &AppError{Code: 40101, HTTPCode: http.StatusUnauthorized, Message: "未授权访问"}
	ErrForbidden        = &AppError{Code: 40301, HTTPCode: http.StatusForbidden, Message: "权限不足"}
	ErrNotFound         = &AppError{Code: 40401, HTTPCode: http.StatusNotFound, Message: "资源不存在"}
	ErrTooManyRequests  = &AppError{Code: 42901, HTTPCode: http.StatusTooManyRequests, Message: "请求过于频繁"}
	ErrInternalServer   = &AppError{Code: 50001, HTTPCode: http.StatusInternalServerError, Message: "服务器内部错误"}
	ErrTokenExpired     = &AppError{Code: 40102, HTTPCode: http.StatusUnauthorized, Message: "Token 已过期"}
	ErrTokenInvalid     = &AppError{Code: 40103, HTTPCode: http.StatusUnauthorized, Message: "Token 无效"}
	ErrUserExists       = &AppError{Code: 40002, HTTPCode: http.StatusBadRequest, Message: "用户名已存在"}
	ErrLoginFailed      = &AppError{Code: 40003, HTTPCode: http.StatusBadRequest, Message: "用户名或密码错误"}
	ErrSubscriptionExpire = &AppError{Code: 40004, HTTPCode: http.StatusBadRequest, Message: "订阅已过期或不可用"}
	ErrOrderNotFound    = &AppError{Code: 40402, HTTPCode: http.StatusNotFound, Message: "订单不存在"}
)

// HandleError 将 AppError 写入 HTTP 响应。
// 如果错误不是 *AppError 类型，则当作内部错误处理。
func HandleError(c *gin.Context, err error) {
	var appErr *AppError
	if e, ok := err.(*AppError); ok {
		appErr = e
	} else {
		appErr = &AppError{
			Code:    ErrInternalServer.Code,
			HTTPCode: ErrInternalServer.HTTPCode,
			Message: ErrInternalServer.Message,
			Detail:  err.Error(),
		}
	}

	c.JSON(appErr.HTTPCode, Response{
		Success: false,
		Message: appErr.Message,
		Code:    appErr.Code,
	})
}

// ErrorWithDetail 返回带详细错误信息的错误响应（用于调试/部署类接口）。
func ErrorWithDetail(c *gin.Context, httpCode int, code int, message string, detail string) {
	c.JSON(httpCode, Response{
		Success: false,
		Message: message,
		Code:    code,
		Data:    gin.H{"detail": detail},
	})
}
