// generator_test.go — 订阅生成器测试。
//
// 测试范围：
// - Clash YAML 格式生成
// - 无效 Token 处理
// - 过期订阅处理
package subscription_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/subscription"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PlanNodeGroup 套餐-节点分组关联模型（测试用）。
type PlanNodeGroup struct {
	ID          uint64 `gorm:"primaryKey;column:id"`
	PlanID      uint64 `gorm:"column:plan_id"`
	NodeGroupID uint64 `gorm:"column:node_group_id"`
}

func (PlanNodeGroup) TableName() string { return "plan_node_groups" }

// setupSubTestDB 创建订阅测试用数据库。
func setupSubTestDB(t *testing.T) (*gorm.DB, *subscription.Generator) {
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
		&model.NodeGroup{},
		&model.Node{},
		&model.Relay{},
		&model.RelayBackend{},
		&model.SiteSetting{},
		&PlanNodeGroup{},
	))

	userRepo := repository.NewUserRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	relayBackendRepo := repository.NewRelayBackendRepository(db)

	gen := subscription.NewGenerator(subRepo, tokenRepo, planRepo, nodeRepo, userRepo, relayBackendRepo)
	gen.SetSettingRepository(repository.NewSiteSettingRepository(db))

	return db, gen
}

// TestGenerator_ClashYAML 测试 Clash YAML 格式生成。
func TestGenerator_ClashYAML(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "HK-01",
			Server:      "hk.example.com",
			Port:        443,
			UUID:        "test-uuid-1",
			ServerName:  "www.microsoft.com",
			PublicKey:   "pubkey1",
			ShortID:     "sid1",
			Fingerprint: "chrome",
			Flow:        "xtls-rprx-vision",
		},
		{
			Name:        "US-01",
			Server:      "us.example.com",
			Port:        443,
			UUID:        "test-uuid-2",
			ServerName:  "www.microsoft.com",
			PublicKey:   "pubkey2",
			ShortID:     "sid2",
			Fingerprint: "chrome",
			Flow:        "xtls-rprx-vision",
		},
	}

	yamlContent := gen.GenerateClashYAML(nodes)

	// 验证 YAML 包含节点名称
	assert.Contains(t, yamlContent, "HK-01")
	assert.Contains(t, yamlContent, "US-01")
	assert.Contains(t, yamlContent, "vless")
	assert.Contains(t, yamlContent, "hk.example.com")
	assert.Contains(t, yamlContent, "us.example.com")
	assert.Contains(t, yamlContent, "pubkey1")
	assert.Contains(t, yamlContent, "pubkey2")
}

// TestGenerator_GenerateByToken_Default 测试通过 token 生成默认订阅。
func TestGenerator_GenerateByToken_Default(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	// 创建用户、套餐、节点组、节点、订阅、token
	user := &model.User{UUID: "sub-user", Username: "subuser", PasswordHash: "h", XrayUserKey: "su@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "SubPlan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	db.Create(plan)

	nodeGroup := &model.NodeGroup{Name: "sub-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "sub-node", Protocol: "vless", Host: "sub.node",
		Port: 443, ServerName: "sub.node", AgentBaseURL: "http://node:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 10737418240, UsedTraffic: 0, Status: "ACTIVE",
	}
	db.Create(sub)

	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "sub-token-clash"}
	db.Create(token)

	// 关联套餐和节点组
	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	result, err := gen.GenerateByToken(ctx, "sub-token-clash")
	require.NoError(t, err)
	assert.Equal(t, "text/yaml; charset=utf-8", result.ContentType)
	assert.Equal(t, "RayPilot", result.Filename)
	assert.Contains(t, result.Content, "sub-node")
	assert.Equal(t, user.ID, result.User.ID)
	assert.Equal(t, sub.ID, result.Subscription.ID)

	gen.SetProfileName("RayPilot UAT.yaml")
	customResult, err := gen.GenerateByToken(ctx, "sub-token-clash")
	require.NoError(t, err)
	assert.Equal(t, "RayPilot UAT", customResult.Filename)

}

func TestGenerator_GenerateByToken_UsesSubscriptionConfig(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "sub-config-user", Username: "subconfiguser", PasswordHash: "h", XrayUserKey: "sc@x", Status: "active"}
	require.NoError(t, db.Create(user).Error)
	plan := &model.Plan{Name: "SubConfigPlan", Price: 10, DurationDays: 30, TrafficLimit: 10_000, IsActive: true}
	require.NoError(t, db.Create(plan).Error)
	nodeGroup := &model.NodeGroup{Name: "sub-config-group"}
	require.NoError(t, db.Create(nodeGroup).Error)
	node := &model.Node{
		Name: "sub-config-node", Protocol: "vless", Host: "sub-config.node",
		Port: 443, ServerName: "sub-config.node", PublicKey: "pubkey", ShortID: "sid", Fingerprint: "chrome",
		AgentBaseURL: "http://node:8080", AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	require.NoError(t, db.Create(node).Error)
	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30), TrafficLimit: 10_000, UsedTraffic: 1234,
		ResidentialTrafficLimit: 20_000, ResidentialUsedTraffic: 5678, Status: "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)
	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "sub-config-token"}
	require.NoError(t, db.Create(token).Error)
	require.NoError(t, db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID).Error)

	settingRepo := repository.NewSiteSettingRepository(db)
	_, err := settingRepo.Upsert(ctx, model.SiteSettingSubscriptionConfig, `{
		"profile_name":"LeiYun VPN",
		"custom_rules":["DOMAIN-SUFFIX,openai.com,PROXY","GEOIP,CN,DIRECT"],
		"include_user_info":true,
		"profile_update_interval":12,
		"profile_web_page_url":"/subscription"
	}`)
	require.NoError(t, err)

	result, err := gen.GenerateByToken(ctx, "sub-config-token")
	require.NoError(t, err)
	assert.Equal(t, "LeiYun VPN", result.Filename)
	assert.Contains(t, result.Content, "DOMAIN-SUFFIX,openai.com,PROXY")
	assert.Contains(t, result.Content, "GEOIP,CN,DIRECT")
	assert.Contains(t, result.Content, "MATCH,PROXY")
	assert.Equal(t, "upload=0; download=6912; total=30000; expire="+strconv.FormatInt(sub.ExpireDate.Unix(), 10), result.Headers["subscription-userinfo"])
	assert.Equal(t, "12", result.Headers["profile-update-interval"])
	assert.Equal(t, "/subscription", result.Headers["profile-web-page-url"])
	assert.NotEmpty(t, result.Headers["profile-title"])
}

func TestGenerator_GenerateByToken_FiltersExhaustedTrafficPoolNodes(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "pool-user", Username: "pooluser", PasswordHash: "h", XrayUserKey: "pool@x", Status: "active"}
	require.NoError(t, db.Create(user).Error)

	plan := &model.Plan{
		Name:                    "PoolPlan",
		Price:                   10,
		DurationDays:            30,
		TrafficLimit:            1024,
		ResidentialTrafficLimit: 2048,
		IsActive:                true,
	}
	require.NoError(t, db.Create(plan).Error)

	nodeGroup := &model.NodeGroup{Name: "pool-group"}
	require.NoError(t, db.Create(nodeGroup).Error)
	require.NoError(t, db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID).Error)

	normalNode := &model.Node{
		Name:           "NormalNode",
		Protocol:       "vless",
		TrafficPool:    model.TrafficPoolNormal,
		Host:           "normal.example.com",
		Port:           443,
		ServerName:     "www.microsoft.com",
		PublicKey:      "pk-normal",
		ShortID:        "sid-normal",
		Fingerprint:    "chrome",
		Flow:           "xtls-rprx-vision",
		NodeGroupID:    &nodeGroup.ID,
		AgentBaseURL:   "http://normal:8080",
		AgentTokenHash: "hash",
		IsEnabled:      true,
	}
	residentialNode := &model.Node{
		Name:           "ResidentialNode",
		Protocol:       "vless",
		TrafficPool:    model.TrafficPoolResidential,
		Host:           "res.example.com",
		Port:           443,
		ServerName:     "www.microsoft.com",
		PublicKey:      "pk-res",
		ShortID:        "sid-res",
		Fingerprint:    "chrome",
		Flow:           "xtls-rprx-vision",
		NodeGroupID:    &nodeGroup.ID,
		AgentBaseURL:   "http://res:8080",
		AgentTokenHash: "hash",
		IsEnabled:      true,
	}
	require.NoError(t, db.Create(normalNode).Error)
	require.NoError(t, db.Create(residentialNode).Error)

	sub := &model.UserSubscription{
		UserID:                  user.ID,
		PlanID:                  plan.ID,
		StartDate:               time.Now(),
		ExpireDate:              time.Now().AddDate(0, 0, 30),
		TrafficLimit:            1024,
		UsedTraffic:             1024,
		ResidentialTrafficLimit: 2048,
		ResidentialUsedTraffic:  0,
		Status:                  "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)

	subID := sub.ID
	token := &model.SubscriptionToken{
		UserID:         user.ID,
		SubscriptionID: &subID,
		Token:          "pool-token",
	}
	require.NoError(t, db.Create(token).Error)

	result, err := gen.GenerateByToken(ctx, token.Token)
	require.NoError(t, err)
	assert.NotContains(t, result.Content, "normal.example.com")
	assert.Contains(t, result.Content, "res.example.com")
}

func TestGenerator_GenerateByToken_NoPlanNodeGroups_ReturnsNoNodesError(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "nogroup-user", Username: "nogroup", PasswordHash: "h", XrayUserKey: "nogroup@x", Status: "active"}
	require.NoError(t, db.Create(user).Error)

	plan := &model.Plan{Name: "NoGroupPlan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	require.NoError(t, db.Create(plan).Error)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30), TrafficLimit: 10737418240, Status: "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "nogroup-token"}).Error)

	_, err := gen.GenerateByToken(ctx, "nogroup-token")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "当前套餐没有可用节点")
}

func TestGenerator_GenerateByToken_RelayOnlyUsesRelayEndpoint(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "relay-user-uuid", Username: "relayuser", PasswordHash: "h", XrayUserKey: "relay@x", Status: "active"}
	require.NoError(t, db.Create(user).Error)

	plan := &model.Plan{Name: "RelayPlan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	require.NoError(t, db.Create(plan).Error)

	nodeGroup := &model.NodeGroup{Name: "relay-group"}
	require.NoError(t, db.Create(nodeGroup).Error)

	node := &model.Node{
		Name: "exit-hk", Protocol: "vless", Host: "exit.node",
		Port: 443, ServerName: "www.microsoft.com", PublicKey: "exit-pub", ShortID: "exit-sid",
		Fingerprint: "chrome", Flow: "xtls-rprx-vision", LineMode: "relay_only",
		AgentBaseURL: "http://node:8080", AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	require.NoError(t, db.Create(node).Error)

	relay := &model.Relay{
		Name: "relay-hk", Host: "relay.example.com", ForwarderType: "haproxy",
		AgentBaseURL: "http://relay:8080", AgentTokenHash: "hash", IsEnabled: true,
	}
	require.NoError(t, db.Create(relay).Error)
	require.NoError(t, db.Create(&model.RelayBackend{
		RelayID: relay.ID, ExitNodeID: node.ID, Name: "香港中转",
		ListenPort: 24443, TargetHost: "exit.node", TargetPort: 443,
		IsEnabled: true,
	}).Error)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30), TrafficLimit: 10737418240, Status: "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "relay-token"}).Error)
	require.NoError(t, db.Create(&PlanNodeGroup{PlanID: plan.ID, NodeGroupID: nodeGroup.ID}).Error)

	result, err := gen.GenerateByToken(ctx, "relay-token")
	require.NoError(t, err)
	assert.Contains(t, result.Content, "server: relay.example.com")
	assert.Contains(t, result.Content, "port: 24443")
	assert.Contains(t, result.Content, "public-key: exit-pub")
	assert.Contains(t, result.Content, "short-id: exit-sid")
	assert.NotContains(t, result.Content, "server: exit.node")
}

// TestGenerator_GenerateByToken_InvalidToken 测试无效 token。
func TestGenerator_GenerateByToken_InvalidToken(t *testing.T) {
	_, gen := setupSubTestDB(t)
	ctx := context.Background()

	_, err := gen.GenerateByToken(ctx, "nonexistent-token")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "订阅")
}

// TestGenerator_GenerateByToken_InvalidFormat 测试无效格式。
func TestGenerator_GenerateByToken_InvalidFormat(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "fmt-user", Username: "fmtuser", PasswordHash: "h", XrayUserKey: "fmt@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "FmtPlan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	db.Create(plan)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 10737418240, UsedTraffic: 0, Status: "ACTIVE",
	}
	db.Create(sub)

	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "fmt-token"}
	db.Create(token)

	_, err := gen.GenerateByToken(ctx, "fmt-token")
	assert.Error(t, err)
}

// TestGenerator_GenerateByToken_ExpiredSub 测试过期订阅。
func TestGenerator_GenerateByToken_ExpiredSub(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "exp-user", Username: "expuser", PasswordHash: "h", XrayUserKey: "exp@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "ExpPlan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	db.Create(plan)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now().AddDate(0, 0, -60),
		ExpireDate:   time.Now().AddDate(0, 0, -30),
		TrafficLimit: 10737418240, UsedTraffic: 0, Status: "ACTIVE",
	}
	db.Create(sub)

	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "exp-token"}
	db.Create(token)

	_, err := gen.GenerateByToken(ctx, "exp-token")
	assert.Error(t, err)
}

// TestGenerator_GenerateByToken_OverTrafficSub 测试流量超额的订阅。
func TestGenerator_GenerateByToken_OverTrafficSub(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "over-user", Username: "overuser", PasswordHash: "h", XrayUserKey: "ov@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "OverPlan", Price: 10, DurationDays: 30, TrafficLimit: 1000, IsActive: true}
	db.Create(plan)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 1000, UsedTraffic: 2000, Status: "ACTIVE",
	}
	db.Create(sub)

	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "over-token"}
	db.Create(token)

	_, err := gen.GenerateByToken(ctx, "over-token")
	assert.Error(t, err)
}

func TestGenerator_GenerateByToken_UserTokenUsesCurrentSubscription(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "old-token-user", Username: "oldtoken", PasswordHash: "h", XrayUserKey: "oldtoken@test.local", Status: "active"}
	require.NoError(t, db.Create(user).Error)

	oldPlan := &model.Plan{Name: "OldPlan", Price: 10, DurationDays: 30, TrafficLimit: 10000, IsActive: true}
	newPlan := &model.Plan{Name: "NewPlan", Price: 20, DurationDays: 30, TrafficLimit: 10000, IsActive: true}
	require.NoError(t, db.Create(oldPlan).Error)
	require.NoError(t, db.Create(newPlan).Error)

	nodeGroup := &model.NodeGroup{Name: "old-token-group"}
	require.NoError(t, db.Create(nodeGroup).Error)
	node := &model.Node{
		Name: "old-token-node", Protocol: "vless", Host: "old-token.test",
		Port: 443, ServerName: "old-token.test", PublicKey: "pub", ShortID: "sid",
		Fingerprint: "chrome", Flow: "xtls-rprx-vision", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
		AgentBaseURL: "http://agent", AgentTokenHash: "hash",
	}
	require.NoError(t, db.Create(node).Error)
	require.NoError(t, db.Create(&PlanNodeGroup{PlanID: newPlan.ID, NodeGroupID: nodeGroup.ID}).Error)

	oldSub := &model.UserSubscription{
		UserID: user.ID, PlanID: oldPlan.ID, StartDate: time.Now().AddDate(0, 0, -60),
		ExpireDate: time.Now().AddDate(0, 0, -30), TrafficLimit: 10000, Status: "EXPIRED",
	}
	require.NoError(t, db.Create(oldSub).Error)
	newSub := &model.UserSubscription{
		UserID: user.ID, PlanID: newPlan.ID, StartDate: time.Now(),
		ExpireDate: time.Now().AddDate(0, 0, 30), TrafficLimit: 10000, Status: "ACTIVE",
	}
	require.NoError(t, db.Create(newSub).Error)
	require.NoError(t, db.Create(&model.SubscriptionToken{
		UserID: user.ID, SubscriptionID: &oldSub.ID, Token: "old-token-still-present",
	}).Error)

	result, err := gen.GenerateByToken(ctx, "old-token-still-present")
	require.NoError(t, err)
	assert.Equal(t, newSub.ID, result.Subscription.ID)
	assert.Contains(t, result.Content, "old-token-node")
}
