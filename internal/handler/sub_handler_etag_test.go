// sub_handler_etag_test.go — 订阅下载 ETag 缓存测试。
package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSubHandler_ETag 测试 ETag 响应头。
func TestSubHandler_ETag(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/clash", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	etag := w.Header().Get("ETag")
	assert.NotEmpty(t, etag)
}

// TestSubHandler_IfNoneMatch 测试 If-None-Match 缓存命中。
func TestSubHandler_IfNoneMatch(t *testing.T) {
	r, _ := setupSubHandlerTest(t)

	// 先获取 ETag
	firstReq := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/clash", nil)
	firstW := httptest.NewRecorder()
	r.ServeHTTP(firstW, firstReq)
	etag := firstW.Header().Get("ETag")

	// 使用 If-None-Match 请求
	secondReq := httptest.NewRequest(http.MethodGet, "/sub/valid-token-123/clash", nil)
	secondReq.Header.Set("If-None-Match", etag)
	secondW := httptest.NewRecorder()
	r.ServeHTTP(secondW, secondReq)

	// 应该返回 304
	assert.Equal(t, http.StatusNotModified, secondW.Code)
}
