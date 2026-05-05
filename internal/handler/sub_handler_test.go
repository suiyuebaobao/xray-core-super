// sub_handler_test.go — 订阅下载 Handler 测试补充。
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSubHandler_ClashContentType 测试 Clash 格式 Content-Type。
func TestSubHandler_ClashContentType(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/clash", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/yaml")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "RayPilot.yaml")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "filename*=UTF-8''RayPilot.yaml")
}

// TestSubHandler_Base64ContentType 测试 Base64 格式 Content-Type。
func TestSubHandler_Base64ContentType(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/base64", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
}

// TestSubHandler_PlainContentType 测试纯文本格式 Content-Type。
func TestSubHandler_PlainContentType(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/plain", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, w.Body.String(), "vless://")
}
