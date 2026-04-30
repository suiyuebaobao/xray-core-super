// generator_test.go — 订阅生成器测试。
//
// 测试范围：
// - Clash YAML 格式生成
// - Base64 格式生成
// - 纯文本 URI 格式生成
// - 无效 Token 处理
// - 过期订阅处理
package subscription_test

import (
	"context"
	"encoding/base64"
	"strings"
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
		&PlanNodeGroup{},
	))

	userRepo := repository.NewUserRepository(db)
	subRepo := repository.NewSubscriptionRepository(db)
	tokenRepo := repository.NewSubscriptionTokenRepository(db)
	planRepo := repository.NewPlanRepository(db)
	nodeRepo := repository.NewNodeRepository(db)
	relayBackendRepo := repository.NewRelayBackendRepository(db)

	gen := subscription.NewGenerator(subRepo, tokenRepo, planRepo, nodeRepo, userRepo, relayBackendRepo)

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

// TestGenerator_Base64 测试 Base64 格式生成。
func TestGenerator_Base64(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "HK-01",
			Server:      "hk.example.com",
			Port:        443,
			UUID:        "test-uuid",
			ServerName:  "www.microsoft.com",
			PublicKey:   "pubkey",
			ShortID:     "sid",
			Fingerprint: "chrome",
			Flow:        "xtls-rprx-vision",
		},
	}

	base64Content := gen.GenerateBase64(nodes)

	// 验证可以解码
	decoded, err := base64.StdEncoding.DecodeString(base64Content)
	require.NoError(t, err)

	// 解码后应包含 Clash YAML 内容
	decodedStr := string(decoded)
	assert.Contains(t, decodedStr, "HK-01")
	assert.Contains(t, decodedStr, "vless")
}

// TestGenerator_PlainURI 测试纯文本 URI 格式生成。
func TestGenerator_PlainURI(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "HK-01",
			Server:      "hk.example.com",
			Port:        443,
			UUID:        "test-uuid",
			ServerName:  "www.microsoft.com",
			PublicKey:   "pubkey",
			ShortID:     "sid",
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

	uriContent := gen.GeneratePlainURI(nodes)

	// 验证包含两个 URI（每行一个）
	lines := strings.Split(strings.TrimSpace(uriContent), "\n")
	assert.Len(t, lines, 2)

	// 验证 URI 格式
	assert.True(t, strings.HasPrefix(lines[0], "vless://"))
	assert.Contains(t, lines[0], "hk.example.com")
	assert.Contains(t, lines[0], "test-uuid")
	assert.True(t, strings.HasPrefix(lines[1], "vless://"))
	assert.Contains(t, lines[1], "us.example.com")
}

// TestGenerator_GenerateByToken_Clash 测试通过 token 生成 Clash 订阅。
func TestGenerator_GenerateByToken_Clash(t *testing.T) {
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

	result, err := gen.GenerateByToken(ctx, "sub-token-clash", "clash")
	require.NoError(t, err)
	assert.Equal(t, "text/yaml; charset=utf-8", result.ContentType)
	assert.Equal(t, "config.yaml", result.Filename)
	assert.Contains(t, result.Content, "sub-node")
	assert.Equal(t, user.ID, result.User.ID)
	assert.Equal(t, sub.ID, result.Subscription.ID)
}

// TestGenerator_GenerateByToken_Base64 测试通过 token 生成 Base64 订阅。
func TestGenerator_GenerateByToken_Base64(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "b64-user", Username: "b64user", PasswordHash: "h", XrayUserKey: "b64@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "B64Plan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	db.Create(plan)

	nodeGroup := &model.NodeGroup{Name: "b64-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "b64-node", Protocol: "vless", Host: "b64.node",
		Port: 443, ServerName: "b64.node", AgentBaseURL: "http://node:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 10737418240, UsedTraffic: 0, Status: "ACTIVE",
	}
	db.Create(sub)

	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "b64-token"}
	db.Create(token)

	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	result, err := gen.GenerateByToken(ctx, "b64-token", "base64")
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", result.ContentType)
	assert.Equal(t, "config.txt", result.Filename)
	// 验证 Base64 可解码
	decoded, decodeErr := base64.StdEncoding.DecodeString(result.Content)
	require.NoError(t, decodeErr)
	assert.Contains(t, string(decoded), "b64-node")
}

// TestGenerator_GenerateByToken_Plain 测试通过 token 生成纯文本 URI 订阅。
func TestGenerator_GenerateByToken_Plain(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "plain-uuid", Username: "plainuser", PasswordHash: "h", XrayUserKey: "pl@x", Status: "active"}
	db.Create(user)

	plan := &model.Plan{Name: "PlainPlan", Price: 10, DurationDays: 30, TrafficLimit: 10737418240, IsActive: true}
	db.Create(plan)

	nodeGroup := &model.NodeGroup{Name: "plain-group"}
	db.Create(nodeGroup)

	node := &model.Node{
		Name: "plain-node", Protocol: "vless", Host: "plain.node",
		Port: 443, ServerName: "plain.node", AgentBaseURL: "http://node:8080",
		AgentTokenHash: "hash", NodeGroupID: &nodeGroup.ID, IsEnabled: true,
	}
	db.Create(node)

	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(),
		ExpireDate:   time.Now().AddDate(0, 0, 30),
		TrafficLimit: 10737418240, UsedTraffic: 0, Status: "ACTIVE",
	}
	db.Create(sub)

	token := &model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "plain-token"}
	db.Create(token)

	db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID)

	result, err := gen.GenerateByToken(ctx, "plain-token", "plain")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(result.Content, "vless://"))
	assert.Contains(t, result.Content, "plain-node")
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

	result, err := gen.GenerateByToken(ctx, "relay-token", "plain")
	require.NoError(t, err)
	assert.Contains(t, result.Content, "relay.example.com:24443")
	assert.Contains(t, result.Content, "pbk=exit-pub")
	assert.Contains(t, result.Content, "sid=exit-sid")
	assert.NotContains(t, result.Content, "exit.node:443")
}

// TestGenerator_GenerateByToken_InvalidToken 测试无效 token。
func TestGenerator_GenerateByToken_InvalidToken(t *testing.T) {
	_, gen := setupSubTestDB(t)
	ctx := context.Background()

	_, err := gen.GenerateByToken(ctx, "nonexistent-token", "clash")
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

	_, err := gen.GenerateByToken(ctx, "fmt-token", "xml")
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

	_, err := gen.GenerateByToken(ctx, "exp-token", "clash")
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

	_, err := gen.GenerateByToken(ctx, "over-token", "clash")
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

	result, err := gen.GenerateByToken(ctx, "old-token-still-present", "plain")
	require.NoError(t, err)
	assert.Equal(t, newSub.ID, result.Subscription.ID)
	assert.Contains(t, result.Content, "old-token-node")
}
