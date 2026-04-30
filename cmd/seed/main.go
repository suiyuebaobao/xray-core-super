// cmd/seed 是数据库种子工具入口。
//
// 功能：
// - 插入初始数据（管理员账号、默认套餐、节点分组等）
// - 仅用于开发和测试环境
//
// 使用方式：
//
//	go run ./cmd/seed
package main

import (
	"fmt"
	"log"

	"suiyue/internal/config"
	"suiyue/internal/platform/database"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func main() {
	cfg := config.Load()
	db := database.New(cfg.DatabaseURL, cfg.LogLevel)
	log.Println("[seed] database connected")

	// 创建管理员账号（如果不存在）
	createAdmin(db, cfg)

	log.Println("[seed] done")
}

// createAdmin 创建初始管理员账号。
func createAdmin(db *gorm.DB, cfg *config.Config) {
	// 检查是否已存在管理员
	var count int64
	db.Raw("SELECT COUNT(*) FROM users WHERE is_admin = 1").Scan(&count)
	if count > 0 {
		log.Println("[seed] admin user already exists, skip")
		return
	}

	// 密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123456"), cfg.BCryptRounds)
	if err != nil {
		log.Fatalf("[seed] failed to hash password: %v", err)
	}

	// 插入管理员
	result := db.Exec(`
		INSERT INTO users (uuid, username, password_hash, email, xray_user_key, status, is_admin, created_at, updated_at)
		VALUES (UUID(), 'admin', ?, 'admin@suiyue.local', 'admin@suiyue.local', 'active', 1, NOW(), NOW())
	`, string(hash))

	if result.Error != nil {
		log.Printf("[seed] failed to create admin: %v", result.Error)
		return
	}

	fmt.Println("[seed] admin user created")
	fmt.Println("[seed]   username: admin")
	fmt.Println("[seed]   password: admin123456")
	fmt.Println("[seed]   请及时修改默认密码！")
}
