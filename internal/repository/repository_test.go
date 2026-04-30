// repository_test.go — Repository 层集成测试。
//
// 测试策略：使用 SQLite 内存数据库，验证完整的数据访问逻辑。
package repository_test

import (
	"context"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.RefreshToken{},
		&model.Plan{},
		&model.NodeGroup{},
		&model.Node{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
		&model.RedeemCode{},
		&model.Order{},
		&model.NodeAccessTask{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
	))
	return db
}

func TestUserRepository_UpdatePassword(t *testing.T) {
	db := setupTestDB(t)
	userRepo := repository.NewUserRepository(db)

	user := &model.User{
		UUID:         "test-uuid",
		Username:     "testuser",
		PasswordHash: "oldhash",
		XrayUserKey:  "test@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(user).Error)

	err := userRepo.UpdatePassword(context.Background(), user.ID, "newhash")
	assert.NoError(t, err)

	var updated model.User
	db.First(&updated, user.ID)
	assert.Equal(t, "newhash", updated.PasswordHash)
}

func TestUserRepository_SearchByUsername(t *testing.T) {
	db := setupTestDB(t)
	userRepo := repository.NewUserRepository(db)

	db.Create(&model.User{UUID: "u1", Username: "alice", PasswordHash: "h", XrayUserKey: "a@test.local", Status: "active"})
	db.Create(&model.User{UUID: "u2", Username: "bob", PasswordHash: "h", XrayUserKey: "b@test.local", Status: "active"})
	db.Create(&model.User{UUID: "u3", Username: "charlie", PasswordHash: "h", XrayUserKey: "c@test.local", Status: "active"})

	users, total, err := userRepo.SearchByUsername(context.Background(), "ali", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, users, 1)
	assert.Equal(t, "alice", users[0].Username)
}

func TestUserRepository_SearchByUsername_EscapesLikeWildcards(t *testing.T) {
	db := setupTestDB(t)
	userRepo := repository.NewUserRepository(db)

	db.Create(&model.User{UUID: "u1", Username: "percent%user", PasswordHash: "h", XrayUserKey: "p@test.local", Status: "active"})
	db.Create(&model.User{UUID: "u2", Username: "plainuser", PasswordHash: "h", XrayUserKey: "plain@test.local", Status: "active"})

	users, total, err := userRepo.SearchByUsername(context.Background(), "%", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Len(t, users, 1)
	assert.Equal(t, "percent%user", users[0].Username)
}

func TestUserRepository_Update(t *testing.T) {
	db := setupTestDB(t)
	userRepo := repository.NewUserRepository(db)

	user := &model.User{
		UUID:         "test-uuid",
		Username:     "testuser",
		PasswordHash: "hash",
		XrayUserKey:  "test@test.local",
		Status:       "active",
	}
	require.NoError(t, db.Create(user).Error)

	email := "new@example.com"
	user.Email = &email
	err := userRepo.Update(context.Background(), user)
	assert.NoError(t, err)

	var updated model.User
	db.First(&updated, user.ID)
	assert.Equal(t, "new@example.com", *updated.Email)
}

func TestRefreshTokenRepository_DeleteExpired(t *testing.T) {
	db := setupTestDB(t)
	refreshRepo := repository.NewRefreshTokenRepository(db)

	// Create expired token
	db.Create(&model.RefreshToken{
		UserID:    1,
		TokenHash: "expiredhash",
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	// Create valid token
	db.Create(&model.RefreshToken{
		UserID:    1,
		TokenHash: "validhash",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	})

	err := refreshRepo.DeleteExpired(context.Background())
	assert.NoError(t, err)

	var count int64
	db.Model(&model.RefreshToken{}).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestSubscriptionTokenRepository_FindByID(t *testing.T) {
	db := setupTestDB(t)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	st := &model.SubscriptionToken{
		UserID:         1,
		SubscriptionID: &sub.ID,
		Token:          "findbyid-token",
	}
	db.Create(st)

	found, err := tokenRepo.FindByID(context.Background(), st.ID)
	require.NoError(t, err)
	assert.Equal(t, st.ID, found.ID)
	assert.Equal(t, "findbyid-token", found.Token)
}

func TestSubscriptionTokenRepository_Revoke(t *testing.T) {
	db := setupTestDB(t)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	st := &model.SubscriptionToken{
		UserID:         1,
		SubscriptionID: &sub.ID,
		Token:          "revoke-token",
		IsRevoked:      false,
	}
	db.Create(st)

	err := tokenRepo.Revoke(context.Background(), st.ID)
	assert.NoError(t, err)

	var updated model.SubscriptionToken
	db.First(&updated, st.ID)
	assert.True(t, updated.IsRevoked)
}

func TestSubscriptionTokenRepository_ListPaginated(t *testing.T) {
	db := setupTestDB(t)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	for i := 0; i < 5; i++ {
		db.Create(&model.SubscriptionToken{
			UserID:         uint64(i + 1),
			SubscriptionID: &sub.ID,
			Token:          "list-token-" + string(rune('a'+i)),
		})
	}

	tokens, total, err := tokenRepo.ListPaginated(context.Background(), 1, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, tokens, 3)
}

func TestSubscriptionTokenRepository_FindByUserID(t *testing.T) {
	db := setupTestDB(t)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	db.Create(&model.SubscriptionToken{
		UserID:         1,
		SubscriptionID: &sub.ID,
		Token:          "findbyuser-token",
	})

	tokens, err := tokenRepo.FindByUserID(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, tokens, 1)
	assert.Equal(t, uint64(1), tokens[0].UserID)
}

func TestOrderRepository_ExpireByTime(t *testing.T) {
	db := setupTestDB(t)
	orderRepo := repository.NewOrderRepository(db)

	// Create expired order
	expiredAt := time.Now().Add(-1 * time.Hour)
	db.Create(&model.Order{
		UserID:    1,
		PlanID:    1,
		OrderNo:   "ORD-EXPIRE-TEST",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &expiredAt,
	})

	// Create valid order
	futureAt := time.Now().Add(1 * time.Hour)
	db.Create(&model.Order{
		UserID:    2,
		PlanID:    1,
		OrderNo:   "ORD-VALID-TEST",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &futureAt,
	})

	count, err := orderRepo.ExpireByTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	var expiredOrder model.Order
	db.Where("order_no = ?", "ORD-EXPIRE-TEST").First(&expiredOrder)
	assert.Equal(t, "EXPIRED", expiredOrder.Status)

	var validOrder model.Order
	db.Where("order_no = ?", "ORD-VALID-TEST").First(&validOrder)
	assert.Equal(t, "PENDING", validOrder.Status)
}

func TestSubscriptionRepository_UpdateStatus(t *testing.T) {
	db := setupTestDB(t)
	subRepo := repository.NewSubscriptionRepository(db)

	activeUserID := uint64(1)
	sub := &model.UserSubscription{
		UserID:       1,
		PlanID:       1,
		StartDate:    time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		Status:       "ACTIVE",
		ActiveUserID: &activeUserID,
	}
	db.Create(sub)

	err := subRepo.UpdateStatus(context.Background(), sub.ID, "EXPIRED")
	assert.NoError(t, err)

	var updated model.UserSubscription
	db.First(&updated, sub.ID)
	assert.Equal(t, "EXPIRED", updated.Status)
	assert.Nil(t, updated.ActiveUserID)
}

func TestSubscriptionRepository_Update(t *testing.T) {
	db := setupTestDB(t)
	subRepo := repository.NewSubscriptionRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	sub.UsedTraffic = 1000
	err := subRepo.Update(context.Background(), sub)
	assert.NoError(t, err)

	var updated model.UserSubscription
	db.First(&updated, sub.ID)
	assert.Equal(t, uint64(1000), updated.UsedTraffic)
}

func TestRedeemCodeRepository_FindByCode(t *testing.T) {
	db := setupTestDB(t)
	redeemRepo := repository.NewRedeemCodeRepository(db)

	code := &model.RedeemCode{
		Code:         "TESTCODE123",
		PlanID:       1,
		DurationDays: 30,
	}
	db.Create(code)

	found, err := redeemRepo.FindByCode(context.Background(), "TESTCODE123")
	require.NoError(t, err)
	assert.Equal(t, code.ID, found.ID)
	assert.Equal(t, uint64(1), found.PlanID)
}

func TestOrderRepository_FindByOrderNo(t *testing.T) {
	db := setupTestDB(t)
	orderRepo := repository.NewOrderRepository(db)

	expiredAt := time.Now().Add(30 * time.Minute)
	order := &model.Order{
		UserID:    1,
		PlanID:    1,
		OrderNo:   "ORD-FIND-TEST",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &expiredAt,
	}
	db.Create(order)

	found, err := orderRepo.FindByOrderNo(context.Background(), "ORD-FIND-TEST")
	require.NoError(t, err)
	assert.Equal(t, order.ID, found.ID)
	assert.Equal(t, "PENDING", found.Status)
}

func TestSubscriptionRepository_FindByID(t *testing.T) {
	db := setupTestDB(t)
	subRepo := repository.NewSubscriptionRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	found, err := subRepo.FindByID(context.Background(), sub.ID)
	require.NoError(t, err)
	assert.Equal(t, sub.ID, found.ID)
	assert.Equal(t, "ACTIVE", found.Status)
}

func TestSubscriptionRepository_WithTransaction(t *testing.T) {
	db := setupTestDB(t)
	subRepo := repository.NewSubscriptionRepository(db)

	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	db.Create(sub)

	err := subRepo.WithTransaction(context.Background(), func(tx *gorm.DB) error {
		return tx.Model(&model.UserSubscription{}).Where("id = ?", sub.ID).Update("status", "EXPIRED").Error
	})
	assert.NoError(t, err)

	var updated model.UserSubscription
	db.First(&updated, sub.ID)
	assert.Equal(t, "EXPIRED", updated.Status)
}

func TestNodeAccessTaskRepository_FindByID(t *testing.T) {
	db := setupTestDB(t)
	taskRepo := repository.NewNodeAccessTaskRepository(db)

	task := &model.NodeAccessTask{
		NodeID:         1,
		Action:         "UPSERT_USER",
		Status:         "PENDING",
		ScheduledAt:    time.Now(),
		IdempotencyKey: "test-findbyid",
	}
	db.Create(task)

	found, err := taskRepo.FindByID(context.Background(), task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, found.ID)
	assert.Equal(t, "UPSERT_USER", found.Action)
}
