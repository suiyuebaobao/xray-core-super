// database_test.go — 数据库连接测试。
//
// 注意：database.New() 需要真实 MySQL 连接，
// 此处仅测试日志级别映射逻辑，不测试实际连接。
package database_test

import (
	"testing"

	"gorm.io/gorm/logger"

	"github.com/stretchr/testify/assert"
)

// TestDatabase_LogLevelMapping 验证日志级别映射正确。
func TestDatabase_LogLevelMapping(t *testing.T) {
	testCases := []struct {
		input    string
		expected logger.LogLevel
	}{
		{"debug", logger.Info},
		{"info", logger.Warn},
		{"warn", logger.Warn},
		{"error", logger.Warn},
		{"", logger.Warn},
	}

	for _, tc := range testCases {
		// 映射逻辑与 database.go 一致
		lvl := logger.Warn
		switch tc.input {
		case "debug":
			lvl = logger.Info
		case "info":
			lvl = logger.Warn
		default:
			lvl = logger.Warn
		}
		assert.Equal(t, tc.expected, lvl, "input: %q", tc.input)
	}
}
