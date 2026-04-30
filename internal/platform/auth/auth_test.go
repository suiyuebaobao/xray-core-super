// auth_test.go — JWT 鉴权模块测试。
//
// 测试范围：
// - Access Token 生成和解析
// - Refresh Token 生成和解析
// - Token 过期处理
// - 无效 Token 处理
//
// 纯单元测试，不依赖数据库。
package auth_test

import (
	"testing"
	"time"

	"suiyue/internal/platform/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-for-auth-unit-test"

// TestAuth_GenerateAndParseAccessToken 测试 Access Token 生成和解析。
func TestAuth_GenerateAndParseAccessToken(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	token, err := auth.GenerateToken(1, "testuser", false, testSecret, expiresAt)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := auth.ParseClaims(token, testSecret)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), claims.UserID)
	assert.Equal(t, "testuser", claims.Username)
	assert.False(t, claims.IsAdmin)
}

// TestAuth_GenerateAndParseRefreshToken 测试 Refresh Token 生成和解析。
func TestAuth_GenerateAndParseRefreshToken(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(7 * 24 * time.Hour)

	token, err := auth.GenerateRefreshToken(1, testSecret, expiresAt)
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := auth.ParseRefreshClaims(token, testSecret)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), claims.UserID)
}

// TestAuth_ParseExpiredToken 测试解析过期 Token。
func TestAuth_ParseExpiredToken(t *testing.T) {
	// 生成已过期的 Token
	expiresAt := time.Now().Add(-1 * time.Hour)

	token, err := auth.GenerateToken(1, "testuser", false, testSecret, expiresAt)
	require.NoError(t, err)

	_, err = auth.ParseClaims(token, testSecret)
	assert.Error(t, err)
}

// TestAuth_ParseInvalidSecret 测试使用错误密钥解析 Token。
func TestAuth_ParseInvalidSecret(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	token, err := auth.GenerateToken(1, "testuser", false, testSecret, expiresAt)
	require.NoError(t, err)

	// 用错误的密钥解析
	_, err = auth.ParseClaims(token, "wrong-secret")
	assert.Error(t, err)
}

// TestAuth_AdminToken 测试管理员 Token 生成和解析。
func TestAuth_AdminToken(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	token, err := auth.GenerateToken(1, "admin", true, testSecret, expiresAt)
	require.NoError(t, err)

	claims, err := auth.ParseClaims(token, testSecret)
	require.NoError(t, err)
	assert.True(t, claims.IsAdmin)
}



