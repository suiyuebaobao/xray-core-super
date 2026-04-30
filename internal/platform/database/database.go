// Package database 提供数据库连接初始化能力。
//
// 使用 GORM 连接 MySQL，设置通用配置：
// - UTF-8 字符集
// - 禁用自动表迁移（使用 golang-migrate 管理）
// - 设置连接池参数
package database

import (
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// New 创建 GORM 数据库连接。
//
// dsn 为 MySQL 连接串，例如：
// "user:pass@tcp(host:port)/dbname?charset=utf8mb4&parseTime=True&loc=Local"
func New(dsn string, logLevel string) *gorm.DB {
	// 根据日志级别设置 GORM 日志级别
	lvl := logger.Warn
	switch logLevel {
	case "debug":
		lvl = logger.Info
	case "info":
		lvl = logger.Warn
	default:
		lvl = logger.Warn
	}

	var db *gorm.DB
	var sqlDB interface {
		Ping() error
		Close() error
		SetMaxIdleConns(int)
		SetMaxOpenConns(int)
		SetConnMaxLifetime(time.Duration)
	}
	var err error

	for attempt := 1; attempt <= 10; attempt++ {
		db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
			Logger:                                   logger.Default.LogMode(lvl),
			DisableForeignKeyConstraintWhenMigrating: false,
		})
		if err == nil {
			sqlDB, err = db.DB()
		}
		if err == nil {
			err = sqlDB.Ping()
		}
		if err == nil {
			break
		}
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		if attempt == 10 {
			log.Fatalf("failed to connect database after retries: %v", err)
		}
		log.Printf("database not ready, retrying (%d/10): %v", attempt, err)
		time.Sleep(time.Duration(attempt) * time.Second)
	}

	// 设置连接池
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db
}
