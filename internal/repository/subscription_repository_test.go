// subscription_repository_test.go — 订阅相关 Repository 测试。
package repository_test

import (
	"context"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupSubscriptionTestDB 创建订阅测试用数据库。
func setupSubscriptionTestDB(t *testing.T) (*gorm.DB, *repository.SubscriptionRepository, *repository.SubscriptionTokenRepository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
		&model.Plan{},
	))

	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)

	return db, subRepo, tokenRepo
}

// TestSubscriptionRepository_FindActiveByUserID 测试查找有效订阅。
func TestSubscriptionRepository_FindActiveByUserID(t *testing.T) {
	db, subRepo, _ := setupSubscriptionTestDB(t)
	ctx := context.Background()

	// 创建用户
	user := &model.User{
		UUID:         "test-uuid",
		Username:     "testuser",
		PasswordHash: "hash",
		XrayUserKey:  "testuser@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(user).Error)

	// 创建有效订阅
	activeSub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       1,
		StartDate:    time.Now().Add(-24 * time.Hour),
		ExpireDate:   time.Now().Add(24 * time.Hour),
		TrafficLimit: 1024 * 1024 * 1024,
		UsedTraffic:  0,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(activeSub).Error)

	// 创建过期订阅
	expiredSub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       1,
		StartDate:    time.Now().Add(-48 * time.Hour),
		ExpireDate:   time.Now().Add(-24 * time.Hour),
		TrafficLimit: 1024 * 1024 * 1024,
		UsedTraffic:  0,
		Status:       "EXPIRED",
	}
	require.NoError(t, db.Create(expiredSub).Error)

	// 查找有效订阅
	found, err := subRepo.FindActiveByUserID(ctx, user.ID)
	require.NoError(t, err)
	assert.Equal(t, activeSub.ID, found.ID)
	assert.Equal(t, "ACTIVE", found.Status)
}

// TestSubscriptionTokenRepository_CreateAndFind 测试订阅 Token 创建和查找。
func TestSubscriptionTokenRepository_CreateAndFind(t *testing.T) {
	db, _, tokenRepo := setupSubscriptionTestDB(t)
	ctx := context.Background()

	// 创建用户
	user := &model.User{
		UUID:         "test-uuid",
		Username:     "testuser",
		PasswordHash: "hash",
		XrayUserKey:  "testuser@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(user).Error)

	// 创建订阅
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       1,
		StartDate:    time.Now().Add(-24 * time.Hour),
		ExpireDate:   time.Now().Add(24 * time.Hour),
		TrafficLimit: 1024 * 1024 * 1024,
		UsedTraffic:  0,
		Status:       "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	// 创建 Token
	token := &model.SubscriptionToken{
		UserID:         user.ID,
		SubscriptionID: &sub.ID,
		Token:          "test-token-123",
		IsRevoked:      false,
	}
	require.NoError(t, tokenRepo.Create(ctx, token))

	// 查找 Token
	found, err := tokenRepo.FindByToken(ctx, "test-token-123")
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", found.Token)
	assert.False(t, found.IsRevoked)

	// 查找不存在的 Token
	_, err = tokenRepo.FindByToken(ctx, "nonexistent")
	assert.Error(t, err)
}
