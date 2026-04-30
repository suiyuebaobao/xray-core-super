// Package config 提供应用配置加载能力。
//
// 配置仅通过环境变量加载，不引入配置中心或配置文件。
// 所有配置项都有合理的默认值，生产环境通过 .env 或容器环境变量覆盖。
package config

import (
	"os"
	"strconv"
	"time"
)

// Config 应用全局配置。
type Config struct {
	// 数据库
	DatabaseURL string

	// 服务
	AppPort    int
	AppBaseURL string

	// JWT
	JWTSecret           string
	JWTExpiresIn        time.Duration
	JWTRefreshExpiresIn time.Duration

	// 安全
	BCryptRounds int

	// 日志
	LogLevel string

	// 节点 Agent
	AgentAuthMode          string
	AgentHeartbeatInterval time.Duration
	XrayUserKeyDomain      string

	// 兑换码
	RedeemCodeLength int

	// 订阅
	SubscriptionTokenTTLDays int

	// 任务
	TaskRetryLimit int
	TaskLockTTL    time.Duration

	// 支付（v1 保留骨架）
	TRC20Address            string
	PaymentCallbackSecret   string
	PaymentMinConfirmations int
	OrderExpireDuration     time.Duration
}

// Load 从环境变量加载配置。
func Load() *Config {
	return &Config{
		DatabaseURL: getEnv("DATABASE_URL", "root:root@tcp(127.0.0.1:3306)/suiyue?charset=utf8mb4&parseTime=True&loc=Local"),

		AppPort:    getEnvInt("APP_PORT", 3000),
		AppBaseURL: getEnv("APP_BASE_URL", "http://localhost:3000"),

		JWTSecret:           getEnv("JWT_SECRET", "change-this-to-a-random-secret-key"),
		JWTExpiresIn:        getEnvDuration("JWT_EXPIRES_IN", 24*time.Hour),
		JWTRefreshExpiresIn: getEnvDuration("JWT_REFRESH_EXPIRES_IN", 7*24*time.Hour),

		BCryptRounds: getEnvInt("BCRYPT_ROUNDS", 12),

		LogLevel: getEnv("LOG_LEVEL", "info"),

		AgentAuthMode:          getEnv("AGENT_AUTH_MODE", "token"),
		AgentHeartbeatInterval: getEnvDuration("AGENT_HEARTBEAT_INTERVAL", 30*time.Second),
		XrayUserKeyDomain:      getEnv("XRAY_USER_KEY_DOMAIN", "suiyue.local"),

		RedeemCodeLength: getEnvInt("REDEEM_CODE_LENGTH", 16),

		SubscriptionTokenTTLDays: getEnvInt("SUBSCRIPTION_TOKEN_TTL_DAYS", 365),

		TaskRetryLimit: getEnvInt("TASK_RETRY_LIMIT", 10),
		TaskLockTTL:    getEnvDuration("TASK_LOCK_TTL", 120*time.Second),

		TRC20Address:            getEnv("TRC20_ADDRESS", ""),
		PaymentCallbackSecret:   getEnv("PAYMENT_CALLBACK_SECRET", ""),
		PaymentMinConfirmations: getEnvInt("PAYMENT_MIN_CONFIRMATIONS", 20),
		OrderExpireDuration:     getEnvDuration("ORDER_EXPIRE_DURATION", 30*time.Minute),
	}
}

const defaultJWTSecret = "change-this-to-a-random-secret-key"

// Validate 校验关键配置，不合法时直接 panic。
func (c *Config) Validate() {
	if c.JWTSecret == defaultJWTSecret {
		panic("JWT_SECRET must be set to a strong random value before running in production")
	}
}

// getEnv 获取环境变量，不存在时返回默认值。
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// getEnvInt 获取环境变量（整数），不存在时返回默认值。
func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

// getEnvDuration 获取环境变量（时间间隔），不存在时返回默认值。
// 支持 "30s"、"5m"、"1h" 等格式。
func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(val)
	if err != nil {
		return defaultVal
	}
	return d
}
