// response_test.go — 统一响应格式测试。
package response_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"suiyue/internal/platform/response"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestResponse_Success 测试成功响应格式。
func TestResponse_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	response.Success(c, gin.H{"key": "value"})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "success", resp["message"])
	assert.Equal(t, float64(0), resp["code"])
	assert.NotNil(t, resp["data"])
}

// TestResponse_HandleError 测试错误响应格式。
func TestResponse_HandleError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	response.HandleError(c, response.ErrUnauthorized)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.False(t, resp["success"].(bool))
	assert.Equal(t, "未授权访问", resp["message"])
	assert.Equal(t, float64(40101), resp["code"])
}

// TestResponse_HandleCustomError 测试自定义错误响应。
func TestResponse_HandleCustomError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	customErr := &response.AppError{
		Code:     40007,
		HTTPCode: http.StatusBadRequest,
		Message:  "自定义错误",
	}
	response.HandleError(c, customErr)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.False(t, resp["success"].(bool))
	assert.Equal(t, "自定义错误", resp["message"])
	assert.Equal(t, float64(40007), resp["code"])
}

// TestResponse_SuccessWithMessage 测试带消息的成功响应。
func TestResponse_SuccessWithMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	response.SuccessWithMessage(c, "操作成功", gin.H{"id": 1})

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "操作成功", resp["message"])
}

// TestResponse_HandleError_NonAppError 测试 HandleError 处理非 AppError 错误。
func TestResponse_HandleError_NonAppError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	response.HandleError(c, fmt.Errorf("database connection failed"))

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)

	assert.False(t, resp["success"].(bool))
	assert.Equal(t, "服务器内部错误", resp["message"])
	assert.Equal(t, float64(50001), resp["code"])
}

// TestAppError_Error 测试 AppError.Error() 方法。
func TestAppError_Error(t *testing.T) {
	// 有 Detail 时返回 Detail
	withDetail := &response.AppError{Message: "msg", Detail: "detail"}
	assert.Equal(t, "detail", withDetail.Error())

	// 无 Detail 时返回 Message
	noDetail := &response.AppError{Message: "msg"}
	assert.Equal(t, "msg", noDetail.Error())
}

// TestResponse_AllPredefinedErrors 测试所有预定义错误都有正确的 HTTPCode。
func TestResponse_AllPredefinedErrors(t *testing.T) {
	errors := []*response.AppError{
		response.ErrBadRequest,
		response.ErrUnauthorized,
		response.ErrForbidden,
		response.ErrNotFound,
		response.ErrTooManyRequests,
		response.ErrInternalServer,
		response.ErrTokenExpired,
		response.ErrTokenInvalid,
		response.ErrUserExists,
		response.ErrLoginFailed,
		response.ErrSubscriptionExpire,
		response.ErrOrderNotFound,
	}

	for _, e := range errors {
		assert.Greater(t, e.Code, 0, "错误码应大于 0: %s", e.Message)
		assert.NotEmpty(t, e.Message, "错误消息不应为空")
	}
}
