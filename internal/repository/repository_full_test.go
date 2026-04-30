// repository_full_test.go — Repository 层全面覆盖测试。
//
// 测试策略：使用 SQLite 内存数据库，覆盖所有零覆盖率的方法。
package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// helper: 创建全模型测试库。
func setupFullDB(t *testing.T) *gorm.DB {
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
		&model.NodeGroupNode{},
		&model.Node{},
		&model.UserSubscription{},
		&model.SubscriptionToken{},
		&model.RedeemCode{},
		&model.Order{},
		&model.NodeAccessTask{},
		&model.Relay{},
		&model.RelayBackend{},
		&model.RelayConfigTask{},
		&model.RelayTrafficSnapshot{},
		&model.TrafficSnapshot{},
		&model.UsageLedger{},
	))
	return db
}

// ---- UserRepository ----

func TestUserRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	user := &model.User{
		UUID:         "u1",
		Username:     "createuser",
		PasswordHash: "hash",
		XrayUserKey:  "create@test.local",
		Status:       "active",
	}
	err := repo.Create(context.Background(), user)
	assert.NoError(t, err)
	assert.Greater(t, user.ID, uint64(0))
}

func TestUserRepository_FindByID(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	user := &model.User{UUID: "u2", Username: "findbyid", PasswordHash: "h", XrayUserKey: "fbi@test.local", Status: "active"}
	db.Create(user)
	found, err := repo.FindByID(context.Background(), user.ID)
	require.NoError(t, err)
	assert.Equal(t, "findbyid", found.Username)
}

func TestUserRepository_FindByID_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	_, err := repo.FindByID(context.Background(), 99999)
	assert.Error(t, err)
}

func TestUserRepository_FindByUsername(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	db.Create(&model.User{UUID: "u3", Username: "findbyname", PasswordHash: "h", XrayUserKey: "fbn@test.local", Status: "active"})
	found, err := repo.FindByUsername(context.Background(), "findbyname")
	require.NoError(t, err)
	assert.Equal(t, "findbyname", found.Username)
}

func TestUserRepository_FindByUsername_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	_, err := repo.FindByUsername(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestUserRepository_FindByXrayUserKey(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	db.Create(&model.User{UUID: "u4", Username: "fbxrk", PasswordHash: "h", XrayUserKey: "fbxrk@test.local", Status: "active"})
	found, err := repo.FindByXrayUserKey(context.Background(), "fbxrk@test.local")
	require.NoError(t, err)
	assert.Equal(t, "fbxrk", found.Username)
}

func TestUserRepository_FindByXrayUserKey_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	_, err := repo.FindByXrayUserKey(context.Background(), "nonexistent@test.local")
	assert.Error(t, err)
}

func TestUserRepository_UpdateLoginInfo(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	user := &model.User{UUID: "u5", Username: "logininfo", PasswordHash: "h", XrayUserKey: "li@test.local", Status: "active"}
	db.Create(user)
	err := repo.UpdateLoginInfo(context.Background(), user.ID, "1.2.3.4")
	assert.NoError(t, err)
	var updated model.User
	db.First(&updated, user.ID)
	assert.Equal(t, "1.2.3.4", *updated.LastLoginIP)
	assert.NotNil(t, updated.LastLoginAt)
}

func TestUserRepository_List(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	db.Create(&model.User{UUID: "l1", Username: "user1", PasswordHash: "h", XrayUserKey: "u1@test.local", Status: "active"})
	db.Create(&model.User{UUID: "l2", Username: "user2", PasswordHash: "h", XrayUserKey: "u2@test.local", Status: "active"})
	users, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(users), 2)
}

func TestUserRepository_ListPaginated(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	for i := 0; i < 5; i++ {
		db.Create(&model.User{UUID: "lp-u" + string(rune('0'+i)), Username: "lpuser" + string(rune('0'+i)), PasswordHash: "h", XrayUserKey: "lp" + string(rune('0'+i)) + "@test.local", Status: "active"})
	}
	users, total, err := repo.ListPaginated(context.Background(), 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, users, 2)
}

func TestUserRepository_UpdateStatus(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUserRepository(db)
	user := &model.User{UUID: "u6", Username: "statususer", PasswordHash: "h", XrayUserKey: "su@test.local", Status: "active"}
	db.Create(user)
	err := repo.UpdateStatus(context.Background(), user.ID, "disabled")
	assert.NoError(t, err)
	var updated model.User
	db.First(&updated, user.ID)
	assert.Equal(t, "disabled", updated.Status)
}

// ---- RefreshTokenRepository ----

func TestRefreshTokenRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRefreshTokenRepository(db)
	token := &model.RefreshToken{
		UserID:    1,
		TokenHash: "creathash",
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	err := repo.Create(context.Background(), token)
	assert.NoError(t, err)
	assert.Greater(t, token.ID, uint64(0))
}

func TestRefreshTokenRepository_DeleteByHash(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRefreshTokenRepository(db)
	db.Create(&model.RefreshToken{UserID: 1, TokenHash: "delhash", ExpiresAt: time.Now().Add(24 * time.Hour)})
	err := repo.DeleteByHash(context.Background(), "delhash")
	assert.NoError(t, err)
	var count int64
	db.Model(&model.RefreshToken{}).Where("token_hash = ?", "delhash").Count(&count)
	assert.Equal(t, int64(0), count)
}

func TestRefreshTokenRepository_FindByHash(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRefreshTokenRepository(db)
	db.Create(&model.RefreshToken{UserID: 1, TokenHash: "findhash", ExpiresAt: time.Now().Add(24 * time.Hour)})
	found, err := repo.FindByHash(context.Background(), "findhash")
	require.NoError(t, err)
	assert.Equal(t, "findhash", found.TokenHash)
}

func TestRefreshTokenRepository_FindByHash_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRefreshTokenRepository(db)
	_, err := repo.FindByHash(context.Background(), "nope")
	assert.Error(t, err)
}

func TestRefreshTokenRepository_DeleteExpired_VerifyRows(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRefreshTokenRepository(db)
	// Create 2 expired tokens
	db.Create(&model.RefreshToken{UserID: 1, TokenHash: "exp1", ExpiresAt: time.Now().Add(-2 * time.Hour)})
	db.Create(&model.RefreshToken{UserID: 1, TokenHash: "exp2", ExpiresAt: time.Now().Add(-1 * time.Hour)})
	// Create 1 valid token
	db.Create(&model.RefreshToken{UserID: 1, TokenHash: "valid1", ExpiresAt: time.Now().Add(24 * time.Hour)})

	err := repo.DeleteExpired(context.Background())
	assert.NoError(t, err)

	var count int64
	db.Model(&model.RefreshToken{}).Count(&count)
	assert.Equal(t, int64(1), count)
}

// ---- SubscriptionRepository ----

func TestSubscriptionRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionRepository(db)
	sub := &model.UserSubscription{
		UserID:     1,
		PlanID:     1,
		StartDate:  time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30),
		Status:     "ACTIVE",
	}
	err := repo.Create(context.Background(), sub)
	assert.NoError(t, err)
	assert.Greater(t, sub.ID, uint64(0))
}

func TestSubscriptionRepository_FindActiveByUserID_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionRepository(db)
	_, err := repo.FindActiveByUserID(context.Background(), 99999)
	assert.Error(t, err)
}

// ---- SubscriptionTokenRepository ----

func TestSubscriptionTokenRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionTokenRepository(db)
	subscriptionID := uint64(1)
	st := &model.SubscriptionToken{
		UserID:         1,
		SubscriptionID: &subscriptionID,
		Token:          "create-token",
		IsRevoked:      false,
	}
	err := repo.Create(context.Background(), st)
	assert.NoError(t, err)
	assert.Greater(t, st.ID, uint64(0))
}

func TestSubscriptionTokenRepository_UpdateLastUsed(t *testing.T) {
	db := setupFullDB(t)
	sub := &model.UserSubscription{UserID: 1, PlanID: 1, StartDate: time.Now(), ExpireDate: time.Now().AddDate(0, 0, 30), Status: "ACTIVE"}
	db.Create(sub)
	st := &model.SubscriptionToken{UserID: 1, SubscriptionID: &sub.ID, Token: "update-used", IsRevoked: false}
	db.Create(st)
	repo := repository.NewSubscriptionTokenRepository(db)
	err := repo.UpdateLastUsed(context.Background(), st.ID)
	assert.NoError(t, err)
	var updated model.SubscriptionToken
	db.First(&updated, st.ID)
	assert.NotNil(t, updated.LastUsedAt)
}

func TestSubscriptionTokenRepository_FindBySubscriptionID(t *testing.T) {
	db := setupFullDB(t)
	sub := &model.UserSubscription{UserID: 1, PlanID: 1, StartDate: time.Now(), ExpireDate: time.Now().AddDate(0, 0, 30), Status: "ACTIVE"}
	db.Create(sub)
	repo := repository.NewSubscriptionTokenRepository(db)
	db.Create(&model.SubscriptionToken{UserID: 1, SubscriptionID: &sub.ID, Token: "tok1", IsRevoked: false})
	tokens, err := repo.FindBySubscriptionID(context.Background(), sub.ID)
	require.NoError(t, err)
	assert.Len(t, tokens, 1)
}

func TestSubscriptionTokenRepository_Revoke_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionTokenRepository(db)
	err := repo.Revoke(context.Background(), 99999)
	// GORM doesn't error on update with no rows, so no error expected
	assert.NoError(t, err)
}

func TestSubscriptionTokenRepository_ListPaginated_Empty(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionTokenRepository(db)
	tokens, total, err := repo.ListPaginated(context.Background(), 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, tokens)
}

func TestSubscriptionTokenRepository_FindByUserID_Empty(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionTokenRepository(db)
	tokens, err := repo.FindByUserID(context.Background(), 99999)
	require.NoError(t, err)
	assert.Empty(t, tokens)
}

func TestSubscriptionTokenRepository_FindByID_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewSubscriptionTokenRepository(db)
	_, err := repo.FindByID(context.Background(), 99999)
	assert.Error(t, err)
}

// ---- PlanRepository ----

func TestPlanRepository_FindByID(t *testing.T) {
	_, repo := setupPlanTestDB(t)
	plan := &model.Plan{Name: "Find Plan", Price: 10.00, TrafficLimit: 1024, DurationDays: 30, IsActive: true}
	created, err := repo.Create(context.Background(), plan)
	require.NoError(t, err)
	found, err := repo.FindByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, "Find Plan", found.Name)
}

func TestPlanRepository_FindByID_NotFound(t *testing.T) {
	_, repo := setupPlanTestDB(t)
	_, err := repo.FindByID(context.Background(), 99999)
	assert.Error(t, err)
}

func TestPlanRepository_ListActive(t *testing.T) {
	_, repo := setupPlanTestDB(t)
	repo.Create(context.Background(), &model.Plan{Name: "Active1", Price: 10.00, TrafficLimit: 1024, DurationDays: 30, IsActive: true, SortWeight: 1})
	repo.Create(context.Background(), &model.Plan{Name: "Active2", Price: 20.00, TrafficLimit: 2048, DurationDays: 30, IsActive: true, SortWeight: 2})
	// Create plan, then deactivate (GORM bool default:true overrides explicit false on create)
	inactivePlan, _ := repo.Create(context.Background(), &model.Plan{Name: "Inactive1", Price: 5.00, TrafficLimit: 512, DurationDays: 30, IsActive: true, SortWeight: 3})
	inactivePlan.IsActive = false
	repo.Update(context.Background(), inactivePlan)

	plans, err := repo.ListActive(context.Background())
	require.NoError(t, err)
	assert.Len(t, plans, 2)
	assert.Equal(t, "Active1", plans[0].Name)
}

func TestPlanRepository_Update(t *testing.T) {
	db, repo := setupPlanTestDB(t)
	plan := &model.Plan{Name: "Old Name", Price: 10.00, TrafficLimit: 1024, DurationDays: 30, IsActive: true}
	created, _ := repo.Create(context.Background(), plan)
	created.Name = "New Name"
	err := repo.Update(context.Background(), created)
	assert.NoError(t, err)
	var updated model.Plan
	db.First(&updated, created.ID)
	assert.Equal(t, "New Name", updated.Name)
}

func TestPlanRepository_Delete(t *testing.T) {
	db, repo := setupPlanTestDB(t)
	plan := &model.Plan{Name: "To Delete", Price: 10.00, TrafficLimit: 1024, DurationDays: 30, IsActive: true}
	created, _ := repo.Create(context.Background(), plan)
	err := repo.Delete(context.Background(), created.ID)
	assert.NoError(t, err)
	var deleted model.Plan
	require.NoError(t, db.First(&deleted, created.ID).Error)
	assert.True(t, deleted.IsDeleted)
	assert.False(t, deleted.IsActive)

	plans, err := repo.ListAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, plans, 1)
	assert.True(t, plans[0].IsDefault)
}

func TestPlanRepository_Delete_DefaultPlanBlocked(t *testing.T) {
	_, repo := setupPlanTestDB(t)
	base, err := repo.Create(context.Background(), &model.Plan{Name: "Base", Price: 0, DurationDays: 3650, IsActive: true, IsDefault: true})
	require.NoError(t, err)

	err = repo.Delete(context.Background(), base.ID)
	assert.ErrorIs(t, err, repository.ErrDefaultPlanCannotDelete)
}

func TestPlanRepository_Delete_MovesActiveSubscriptionsToDefault(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewPlanRepository(db)
	base, err := repo.Create(context.Background(), &model.Plan{Name: "Base", Price: 0, TrafficLimit: 100, DurationDays: 3650, IsActive: true, IsDefault: true})
	require.NoError(t, err)
	paid, err := repo.Create(context.Background(), &model.Plan{Name: "Paid", Price: 10, TrafficLimit: 1000, DurationDays: 30, IsActive: true})
	require.NoError(t, err)
	user := &model.User{UUID: "move-user", Username: "move-user", PasswordHash: "hash", XrayUserKey: "move@test.local", Status: "active"}
	require.NoError(t, db.Create(user).Error)
	activeUserID := user.ID
	sub := &model.UserSubscription{
		UserID:       user.ID,
		PlanID:       paid.ID,
		StartDate:    time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: paid.TrafficLimit,
		UsedTraffic:  500,
		Status:       "ACTIVE",
		ActiveUserID: &activeUserID,
	}
	require.NoError(t, db.Create(sub).Error)

	result, err := repo.DeleteWithDefaultFallback(context.Background(), paid.ID)
	require.NoError(t, err)
	require.Len(t, result.MovedSubscriptions, 1)
	assert.Equal(t, base.ID, result.DefaultPlanID)

	var updated model.UserSubscription
	require.NoError(t, db.First(&updated, sub.ID).Error)
	assert.Equal(t, base.ID, updated.PlanID)
	assert.Equal(t, base.TrafficLimit, updated.TrafficLimit)
	assert.Equal(t, uint64(0), updated.UsedTraffic)
	assert.Equal(t, "ACTIVE", updated.Status)
	assert.NotNil(t, updated.ActiveUserID)
	assert.Equal(t, user.ID, *updated.ActiveUserID)
}

func TestPlanRepository_ListAll(t *testing.T) {
	_, repo := setupPlanTestDB(t)
	repo.Create(context.Background(), &model.Plan{Name: "All1", Price: 10.00, TrafficLimit: 1024, DurationDays: 30, IsActive: true, SortWeight: 1})
	repo.Create(context.Background(), &model.Plan{Name: "All2", Price: 20.00, TrafficLimit: 2048, DurationDays: 30, IsActive: false, SortWeight: 2})
	plans, err := repo.ListAll(context.Background())
	require.NoError(t, err)
	assert.Len(t, plans, 2)
}

// ---- NodeGroupRepository ----

func TestNodeGroupRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	group := &model.NodeGroup{Name: "Test Group"}
	created, err := repo.Create(context.Background(), group)
	require.NoError(t, err)
	assert.Greater(t, created.ID, uint64(0))
	assert.Equal(t, "Test Group", created.Name)
}

func TestNodeGroupRepository_FindByID(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	db.Create(&model.NodeGroup{Name: "Find Group"})
	found, err := repo.FindByID(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, "Find Group", found.Name)
}

func TestNodeGroupRepository_FindByID_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	_, err := repo.FindByID(context.Background(), 99999)
	assert.Error(t, err)
}

func TestNodeGroupRepository_Update(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	group := &model.NodeGroup{Name: "Old Name"}
	db.Create(group)
	group.Name = "New Name"
	err := repo.Update(context.Background(), group)
	assert.NoError(t, err)
	var updated model.NodeGroup
	db.First(&updated, group.ID)
	assert.Equal(t, "New Name", updated.Name)
}

func TestNodeGroupRepository_Delete(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	group := &model.NodeGroup{Name: "To Delete"}
	db.Create(group)
	err := repo.Delete(context.Background(), group.ID)
	assert.NoError(t, err)
}

func TestNodeGroupRepository_Delete_WithNodes(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	group := &model.NodeGroup{Name: "Has Nodes"}
	db.Create(group)
	nodeGroupID := group.ID
	db.Create(&model.Node{
		Name:         "Node In Group",
		Host:         "1.2.3.4",
		AgentBaseURL: "http://1.2.3.4",
		AgentToken:   "token",
		NodeGroupID:  &nodeGroupID,
	})
	err := repo.Delete(context.Background(), group.ID)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, repository.ErrNodeGroupInUse))
	assert.Contains(t, err.Error(), "该分组下存在节点")
}

func TestNodeGroupRepository_List(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeGroupRepository(db)
	db.Create(&model.NodeGroup{Name: "Group A"})
	db.Create(&model.NodeGroup{Name: "Group B"})
	groups, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(groups), 2)
}

// ---- NodeRepository ----

func TestNodeRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	node := &model.Node{
		Name:         "Test Node",
		Host:         "1.2.3.4",
		AgentBaseURL: "http://1.2.3.4",
		AgentToken:   "token",
	}
	created, err := repo.Create(context.Background(), node)
	require.NoError(t, err)
	assert.Greater(t, created.ID, uint64(0))
}

func TestNodeRepository_FindByID(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	node := &model.Node{Name: "Find Node", Host: "1.2.3.4", AgentBaseURL: "http://1.2.3.4", AgentToken: "t"}
	db.Create(node)
	found, err := repo.FindByID(context.Background(), node.ID)
	require.NoError(t, err)
	assert.Equal(t, "Find Node", found.Name)
}

func TestNodeRepository_FindByID_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	_, err := repo.FindByID(context.Background(), 99999)
	assert.Error(t, err)
}

func TestNodeRepository_Update(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	node := &model.Node{Name: "Old Node", Host: "1.2.3.4", AgentBaseURL: "http://1.2.3.4", AgentToken: "t"}
	db.Create(node)
	node.Name = "Updated Node"
	err := repo.Update(context.Background(), node)
	assert.NoError(t, err)
	var updated model.Node
	db.First(&updated, node.ID)
	assert.Equal(t, "Updated Node", updated.Name)
}

func TestNodeRepository_Delete(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	node := &model.Node{Name: "To Delete", Host: "1.2.3.4", AgentBaseURL: "http://1.2.3.4", AgentToken: "t"}
	db.Create(node)
	err := repo.Delete(context.Background(), node.ID)
	assert.NoError(t, err)
}

func TestNodeRepository_List(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	db.Create(&model.Node{Name: "Node A", Host: "1.1.1.1", AgentBaseURL: "http://1.1.1.1", AgentToken: "t"})
	db.Create(&model.Node{Name: "Node B", Host: "2.2.2.2", AgentBaseURL: "http://2.2.2.2", AgentToken: "t"})
	nodes, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(nodes), 2)
}

func TestNodeRepository_FindByGroupID(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	group := &model.NodeGroup{Name: "FBG Group"}
	db.Create(group)
	ngID := group.ID
	db.Create(&model.Node{Name: "Node1", Host: "1.1.1.1", AgentBaseURL: "http://1.1.1.1", AgentToken: "t", NodeGroupID: &ngID, IsEnabled: true, SortWeight: 1})
	node2 := &model.Node{Name: "Node2", Host: "2.2.2.2", AgentBaseURL: "http://2.2.2.2", AgentToken: "t", NodeGroupID: &ngID, IsEnabled: true, SortWeight: 2}
	db.Create(node2)
	node2.IsEnabled = false
	repo.Update(context.Background(), node2)
	nodes, err := repo.FindByGroupID(context.Background(), group.ID, false)
	require.NoError(t, err)
	assert.Len(t, nodes, 2)

	enabledNodes, err := repo.FindByGroupID(context.Background(), group.ID, true)
	require.NoError(t, err)
	assert.Len(t, enabledNodes, 1)
	assert.Equal(t, "Node1", enabledNodes[0].Name)
}

func TestNodeRepository_UpdateHeartbeat(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeRepository(db)
	node := &model.Node{Name: "HB Node", Host: "1.2.3.4", AgentBaseURL: "http://1.2.3.4", AgentToken: "t"}
	db.Create(node)
	err := repo.UpdateHeartbeat(context.Background(), node.ID)
	assert.NoError(t, err)
	var updated model.Node
	db.First(&updated, node.ID)
	assert.NotNil(t, updated.LastHeartbeatAt)
}

// ---- NodeAccessTaskRepository ----

func TestNodeAccessTaskRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	task := &model.NodeAccessTask{
		NodeID:         1,
		Action:         "ADD_USER",
		Status:         "PENDING",
		ScheduledAt:    time.Now(),
		IdempotencyKey: "idem-1",
	}
	err := repo.Create(context.Background(), task)
	assert.NoError(t, err)
	assert.Greater(t, task.ID, uint64(0))
}

func TestNodeAccessTaskRepository_FindPendingByNodeID(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "PENDING", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "fp-1"})
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "PENDING", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "fp-2"})
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "DONE", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "fp-3"})
	db.Create(&model.NodeAccessTask{NodeID: 2, Action: "ADD_USER", Status: "PENDING", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "fp-4"})
	tasks, err := repo.FindPendingByNodeID(context.Background(), 1, 3)
	require.NoError(t, err)
	assert.Len(t, tasks, 2)
}

func TestNodeAccessTaskRepository_UpdateLock(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	task := &model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "PENDING", ScheduledAt: time.Now(), IdempotencyKey: "ul-1"}
	db.Create(task)
	lockedAt := time.Now()
	err := repo.UpdateLock(context.Background(), task.ID, lockedAt, "lock-token-abc")
	assert.NoError(t, err)
	var updated model.NodeAccessTask
	db.First(&updated, task.ID)
	assert.Equal(t, "PROCESSING", updated.Status)
	assert.Equal(t, "lock-token-abc", *updated.LockToken)
}

func TestNodeAccessTaskRepository_MarkDone(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	task := &model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "PROCESSING", ScheduledAt: time.Now(), IdempotencyKey: "md-1"}
	db.Create(task)
	execAt := time.Now()
	err := repo.MarkDone(context.Background(), task.ID, execAt)
	assert.NoError(t, err)
	var updated model.NodeAccessTask
	db.First(&updated, task.ID)
	assert.Equal(t, "DONE", updated.Status)
	assert.Equal(t, execAt.Unix(), updated.ExecutedAt.Unix())
	assert.Nil(t, updated.LockToken)
}

func TestNodeAccessTaskRepository_MarkFailed(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	task := &model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "PROCESSING", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "mf-1"}
	db.Create(task)
	execAt := time.Now()
	err := repo.MarkFailed(context.Background(), task.ID, "connection timeout", execAt)
	assert.NoError(t, err)
	var updated model.NodeAccessTask
	db.First(&updated, task.ID)
	assert.Equal(t, "FAILED", updated.Status)
	assert.Equal(t, "connection timeout", *updated.LastError)
	assert.Equal(t, uint32(1), updated.RetryCount)
}

func TestNodeAccessTaskRepository_RetryFailedTasks(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "FAILED", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "rf-1"})
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "FAILED", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "rf-2"})
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "PENDING", RetryCount: 0, ScheduledAt: time.Now(), IdempotencyKey: "rf-3"})
	count, err := repo.RetryFailedTasks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	var pendingCount int64
	db.Model(&model.NodeAccessTask{}).Where("status = ?", "PENDING").Count(&pendingCount)
	assert.Equal(t, int64(3), pendingCount)
}

func TestNodeAccessTaskRepository_RetryFailedTasks_ExceedMaxRetry(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewNodeAccessTaskRepository(db)
	db.Create(&model.NodeAccessTask{NodeID: 1, Action: "ADD_USER", Status: "FAILED", RetryCount: 10, ScheduledAt: time.Now(), IdempotencyKey: "rf-max-1"})
	count, err := repo.RetryFailedTasks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestRelayConfigTaskRepository_RetryFailedAndStaleTasks(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRelayConfigTaskRepository(db)
	relay := &model.Relay{Name: "relay-test", Host: "relay.test", ForwarderType: "haproxy", AgentBaseURL: "http://relay:8080", AgentTokenHash: "hash", IsEnabled: true}
	require.NoError(t, db.Create(relay).Error)

	oldLockTime := time.Now().Add(-10 * time.Minute)
	lockToken := "stale-lock"
	db.Create(&model.RelayConfigTask{RelayID: relay.ID, Action: "RELOAD_CONFIG", Status: "FAILED", RetryCount: 1, ScheduledAt: time.Now(), IdempotencyKey: "relay-rf-1"})
	db.Create(&model.RelayConfigTask{RelayID: relay.ID, Action: "RELOAD_CONFIG", Status: "FAILED", RetryCount: 10, ScheduledAt: time.Now(), IdempotencyKey: "relay-rf-max"})
	db.Create(&model.RelayConfigTask{RelayID: relay.ID, Action: "RELOAD_CONFIG", Status: "PROCESSING", RetryCount: 1, ScheduledAt: time.Now(), LockedAt: &oldLockTime, LockToken: &lockToken, IdempotencyKey: "relay-stale-1"})

	count, err := repo.RetryFailedAndStaleTasks(context.Background(), 10, time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	var pendingCount int64
	require.NoError(t, db.Model(&model.RelayConfigTask{}).Where("status = ?", "PENDING").Count(&pendingCount).Error)
	assert.Equal(t, int64(2), pendingCount)

	var stale model.RelayConfigTask
	require.NoError(t, db.Where("idempotency_key = ?", "relay-stale-1").First(&stale).Error)
	assert.Nil(t, stale.LockedAt)
	assert.Nil(t, stale.LockToken)
	assert.Equal(t, uint32(2), stale.RetryCount)
}

// ---- RedeemCodeRepository ----

func TestRedeemCodeRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRedeemCodeRepository(db)
	code := &model.RedeemCode{Code: "CREATECODE", PlanID: 1, DurationDays: 30}
	err := repo.Create(context.Background(), code)
	assert.NoError(t, err)
	assert.Greater(t, code.ID, uint64(0))
}

func TestRedeemCodeRepository_MarkUsed(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRedeemCodeRepository(db)
	code := &model.RedeemCode{Code: "USEME", PlanID: 1, DurationDays: 30}
	db.Create(code)
	usedAt := time.Now()
	err := repo.MarkUsed(context.Background(), code.ID, 42, usedAt)
	assert.NoError(t, err)
	var updated model.RedeemCode
	db.First(&updated, code.ID)
	assert.True(t, updated.IsUsed)
	assert.Equal(t, uint64(42), *updated.UsedByUserID)
}

func TestRedeemCodeRepository_List(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRedeemCodeRepository(db)
	for i := 0; i < 5; i++ {
		db.Create(&model.RedeemCode{Code: "LIST-" + string(rune('A'+i)), PlanID: 1, DurationDays: 30})
	}
	codes, total, err := repo.List(context.Background(), 1, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, codes, 3)
}

func TestRedeemCodeRepository_FindByCode_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewRedeemCodeRepository(db)
	_, err := repo.FindByCode(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}

// ---- OrderRepository ----

func TestOrderRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewOrderRepository(db)
	expiredAt := time.Now().Add(30 * time.Minute)
	order := &model.Order{
		UserID:    1,
		PlanID:    1,
		OrderNo:   "ORD-CREATE-001",
		Amount:    12.0,
		Status:    "PENDING",
		ExpiredAt: &expiredAt,
	}
	err := repo.Create(context.Background(), order)
	assert.NoError(t, err)
	assert.Greater(t, order.ID, uint64(0))
}

func TestOrderRepository_FindByOrderNo_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewOrderRepository(db)
	_, err := repo.FindByOrderNo(context.Background(), "NO-SUCH-ORDER")
	assert.Error(t, err)
}

func TestOrderRepository_ListByUserID(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewOrderRepository(db)
	for i := 0; i < 5; i++ {
		exp := time.Now().Add(30 * time.Minute)
		db.Create(&model.Order{UserID: 1, PlanID: 1, OrderNo: "ORD-LBU-" + string(rune('0'+i)), Amount: 12.0, Status: "PENDING", ExpiredAt: &exp})
	}
	exp := time.Now().Add(30 * time.Minute)
	db.Create(&model.Order{UserID: 2, PlanID: 1, OrderNo: "ORD-LBU-OTHER", Amount: 12.0, Status: "PENDING", ExpiredAt: &exp})
	orders, total, err := repo.ListByUserID(context.Background(), 1, 1, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, orders, 3)
}

func TestOrderRepository_ListAll(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewOrderRepository(db)
	for i := 0; i < 4; i++ {
		exp := time.Now().Add(30 * time.Minute)
		db.Create(&model.Order{UserID: 1, PlanID: 1, OrderNo: "ORD-LA-" + string(rune('0'+i)), Amount: 12.0, Status: "PENDING", ExpiredAt: &exp})
	}
	orders, total, err := repo.ListAll(context.Background(), 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	assert.Len(t, orders, 2)
}

func TestOrderRepository_MarkPaid(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewOrderRepository(db)
	exp := time.Now().Add(30 * time.Minute)
	order := &model.Order{UserID: 1, PlanID: 1, OrderNo: "ORD-PAID-001", Amount: 12.0, Status: "PENDING", ExpiredAt: &exp}
	db.Create(order)
	paidAt := time.Now()
	err := repo.MarkPaid(context.Background(), order.ID, paidAt)
	assert.NoError(t, err)
	var updated model.Order
	db.First(&updated, order.ID)
	assert.Equal(t, "PAID", updated.Status)
	assert.Equal(t, paidAt.Unix(), updated.PaidAt.Unix())
}

func TestOrderRepository_ExpireByTime_NoExpired(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewOrderRepository(db)
	futureAt := time.Now().Add(1 * time.Hour)
	db.Create(&model.Order{UserID: 1, PlanID: 1, OrderNo: "ORD-NOEXP", Amount: 12.0, Status: "PENDING", ExpiredAt: &futureAt})
	count, err := repo.ExpireByTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

// ---- TrafficSnapshotRepository ----

func TestTrafficSnapshotRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewTrafficSnapshotRepository(db)
	snap := &model.TrafficSnapshot{
		NodeID:        1,
		XrayUserKey:   "snap@test.local",
		UplinkTotal:   100,
		DownlinkTotal: 200,
		CapturedAt:    time.Now(),
	}
	err := repo.Create(context.Background(), snap)
	assert.NoError(t, err)
	assert.Greater(t, snap.ID, uint64(0))
}

func TestTrafficSnapshotRepository_FindLatest(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewTrafficSnapshotRepository(db)
	db.Create(&model.TrafficSnapshot{NodeID: 1, XrayUserKey: "fl@test.local", UplinkTotal: 100, DownlinkTotal: 200, CapturedAt: time.Now().Add(-time.Hour)})
	db.Create(&model.TrafficSnapshot{NodeID: 1, XrayUserKey: "fl@test.local", UplinkTotal: 500, DownlinkTotal: 600, CapturedAt: time.Now()})
	latest, err := repo.FindLatest(context.Background(), 1, "fl@test.local")
	require.NoError(t, err)
	assert.Equal(t, uint64(500), latest.UplinkTotal)
}

func TestTrafficSnapshotRepository_FindLatest_NotFound(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewTrafficSnapshotRepository(db)
	_, err := repo.FindLatest(context.Background(), 99999, "nope@test.local")
	assert.Error(t, err)
}

// ---- UsageLedgerRepository ----

func TestUsageLedgerRepository_Create(t *testing.T) {
	db := setupFullDB(t)
	repo := repository.NewUsageLedgerRepository(db)
	ledger := &model.UsageLedger{
		UserID:        1,
		NodeID:        1,
		DeltaUpload:   100,
		DeltaDownload: 200,
		DeltaTotal:    300,
		RecordedAt:    time.Now(),
	}
	err := repo.Create(context.Background(), ledger)
	assert.NoError(t, err)
	assert.Greater(t, ledger.ID, uint64(0))
}
