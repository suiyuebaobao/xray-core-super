// sub_handler_test.go — 订阅下载 Handler 测试补充。
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSubHandler_DefaultContentType 测试默认订阅 Content-Type。
func TestSubHandler_DefaultContentType(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/yaml")
	assert.Contains(t, w.Header().Get("Content-Disposition"), `filename="RayPilot"`)
	assert.Contains(t, w.Header().Get("Content-Disposition"), "filename*=UTF-8''RayPilot")
	assert.NotContains(t, w.Header().Get("Content-Disposition"), "RayPilot.yaml")
	assert.Contains(t, w.Header().Get("subscription-userinfo"), "upload=0; download=0; total=1073741824; expire=")
	assert.NotEmpty(t, w.Header().Get("profile-title"))
}

// TestSubHandler_Base64Disabled 测试旧 Base64 后缀已下线。
func TestSubHandler_Base64Disabled(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/base64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestSubHandler_PlainDisabled 测试旧纯文本 URI 后缀已下线。
func TestSubHandler_PlainDisabled(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/plain", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
