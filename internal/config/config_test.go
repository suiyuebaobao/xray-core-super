// config_test.go — 配置加载模块测试。
package config_test

import (
	"os"
	"testing"
	"time"

	"suiyue/internal/config"

	"github.com/stretchr/testify/assert"
)

// TestConfig_LoadDefaults 测试默认配置加载。
func TestConfig_LoadDefaults(t *testing.T) {
	// 清除可能存在的环境变量
	os.Unsetenv("APP_PORT")
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("BCRYPT_ROUNDS")
	os.Unsetenv("SUBSCRIPTION_PROFILE_NAME")

	cfg := config.Load()

	assert.Equal(t, 3000, cfg.AppPort)
	assert.Equal(t, "change-this-to-a-random-secret-key", cfg.JWTSecret)
	assert.Equal(t, 12, cfg.BCryptRounds)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "RayPilot", cfg.SubscriptionProfileName)
}

// TestConfig_LoadFromEnv 测试从环境变量加载配置。
func TestConfig_LoadFromEnv(t *testing.T) {
	// 设置环境变量
	os.Setenv("APP_PORT", "8080")
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("BCRYPT_ROUNDS", "10")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("JWT_EXPIRES_IN", "2h")
	os.Setenv("SUBSCRIPTION_PROFILE_NAME", "RayPilot-UAT")

	cfg := config.Load()

	assert.Equal(t, 8080, cfg.AppPort)
	assert.Equal(t, "test-secret", cfg.JWTSecret)
	assert.Equal(t, 10, cfg.BCryptRounds)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, 2*time.Hour, cfg.JWTExpiresIn)
	assert.Equal(t, "RayPilot-UAT", cfg.SubscriptionProfileName)

	// 清理环境变量
	os.Unsetenv("APP_PORT")
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("BCRYPT_ROUNDS")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("JWT_EXPIRES_IN")
	os.Unsetenv("SUBSCRIPTION_PROFILE_NAME")
}

// TestConfig_InvalidEnv 测试无效环境变量使用默认值。
func TestConfig_InvalidEnv(t *testing.T) {
	os.Setenv("APP_PORT", "invalid")
	os.Setenv("BCRYPT_ROUNDS", "not-a-number")

	cfg := config.Load()

	// 应该使用默认值
	assert.Equal(t, 3000, cfg.AppPort)
	assert.Equal(t, 12, cfg.BCryptRounds)

	os.Unsetenv("APP_PORT")
	os.Unsetenv("BCRYPT_ROUNDS")
}

// TestConfig_Validate_PanicsOnDefaultJWTSecret 测试默认 JWT 密钥触发 panic。
func TestConfig_Validate_PanicsOnDefaultJWTSecret(t *testing.T) {
	cfg := &config.Config{
		JWTSecret: "change-this-to-a-random-secret-key",
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when JWT_SECRET is default value")
		}
	}()

	cfg.Validate()
}

// TestConfig_Validate_SucceedsWithCustomJWTSecret 测试自定义密钥不触发 panic。
func TestConfig_Validate_SucceedsWithCustomJWTSecret(t *testing.T) {
	cfg := &config.Config{
		JWTSecret: "some-custom-secret-key",
	}

	// 不应 panic
	cfg.Validate()
}
