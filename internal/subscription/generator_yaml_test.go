// generator_yaml_test.go — 订阅生成器 YAML 输出测试。
package subscription_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/repository"
	"suiyue/internal/subscription"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestGenerator_ClashYAML_ValidYAML 测试生成的 Clash YAML 是有效的 YAML 格式。
func TestGenerator_ClashYAML_ValidYAML(t *testing.T) {
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

	yamlContent := gen.GenerateClashYAML(nodes)

	// 验证可以解析为 YAML
	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(yamlContent), &config)
	assert.NoError(t, err)

	// 验证关键字段存在
	assert.Contains(t, config, "mixed-port")
	assert.Contains(t, config, "proxies")
	assert.Contains(t, config, "proxy-groups")
	assert.Contains(t, config, "rules")
}

// TestGenerator_ClashYAML_ProxyStructure 测试代理节点结构正确。
func TestGenerator_ClashYAML_ProxyStructure(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "US-01",
			Server:      "us.example.com",
			Port:        8443,
			UUID:        "uuid-123",
			ServerName:  "example.com",
			PublicKey:   "test-pubkey",
			ShortID:     "test-sid",
			Fingerprint: "firefox",
			Flow:        "xtls-rprx-vision",
		},
	}

	yamlContent := gen.GenerateClashYAML(nodes)

	var config map[string]interface{}
	yaml.Unmarshal([]byte(yamlContent), &config)

	proxies := config["proxies"].([]interface{})
	assert.Len(t, proxies, 1)

	proxy := proxies[0].(map[string]interface{})
	assert.Equal(t, "US-01", proxy["name"])
	assert.Equal(t, "vless", proxy["type"])
	assert.Equal(t, "us.example.com", proxy["server"])
	// YAML 解析后端口可能是 int 或 float64，使用接口类型比较
	assert.Contains(t, []interface{}{uint64(8443), int(8443), float64(8443)}, proxy["port"])
	assert.Equal(t, "uuid-123", proxy["uuid"])
	assert.Equal(t, "tcp", proxy["network"])
	assert.True(t, proxy["udp"].(bool))
	assert.True(t, proxy["tls"].(bool))

	realityOpts := proxy["reality-opts"].(map[string]interface{})
	assert.Equal(t, "test-pubkey", realityOpts["public-key"])
	assert.Equal(t, "test-sid", realityOpts["short-id"])
}

func TestGenerator_ClashYAML_XHTTPProxyStructure(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "XHTTP-01",
			Server:      "xhttp.example.com",
			Port:        443,
			UUID:        "uuid-xhttp",
			ServerName:  "www.microsoft.com",
			PublicKey:   "xhttp-pubkey",
			ShortID:     "xhttp-sid",
			Fingerprint: "chrome",
			Transport:   "xhttp",
			XHTTPPath:   "raypilot-xhttp",
			XHTTPHost:   "cdn.example.com",
			XHTTPMode:   "stream-up",
			Flow:        "xtls-rprx-vision",
		},
	}

	yamlContent := gen.GenerateClashYAML(nodes)

	var config map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(yamlContent), &config))
	proxy := config["proxies"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "xhttp", proxy["network"])
	assert.NotContains(t, proxy, "flow")
	xhttpOpts := proxy["xhttp-opts"].(map[string]interface{})
	assert.Equal(t, "/raypilot-xhttp", xhttpOpts["path"])
	assert.Equal(t, "stream-up", xhttpOpts["mode"])
	assert.Equal(t, "cdn.example.com", xhttpOpts["host"])

}

func TestGenerator_ClashYAML_Socks5ProxyOmitsFlow(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:         "Home-01",
			Server:       "home.example.com",
			Port:         24465,
			UUID:         "uuid-home",
			ServerName:   "www.microsoft.com",
			PublicKey:    "home-pubkey",
			ShortID:      "home-sid",
			Fingerprint:  "chrome",
			Transport:    "tcp",
			OutboundType: "socks5",
			Flow:         "xtls-rprx-vision",
		},
	}

	yamlContent := gen.GenerateClashYAML(nodes)

	var config map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(yamlContent), &config))
	proxy := config["proxies"].([]interface{})[0].(map[string]interface{})
	assert.Equal(t, "tcp", proxy["network"])
	assert.False(t, proxy["udp"].(bool))
	assert.NotContains(t, proxy, "flow")

}

func TestGenerator_ClashYAML_UDPEnabledCanOverrideSocks5Default(t *testing.T) {
	_, gen := setupSubTestDB(t)
	enabled := true

	nodes := []subscription.NodeConfig{
		{
			Name:         "Home-UDP",
			Server:       "home-udp.example.com",
			Port:         24465,
			UUID:         "uuid-home-udp",
			ServerName:   "www.microsoft.com",
			PublicKey:    "home-pubkey",
			ShortID:      "home-sid",
			Fingerprint:  "chrome",
			Transport:    "tcp",
			OutboundType: "socks5",
			UDPEnabled:   &enabled,
		},
	}

	yamlContent := gen.GenerateClashYAML(nodes)

	var config map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(yamlContent), &config))
	proxy := config["proxies"].([]interface{})[0].(map[string]interface{})
	assert.True(t, proxy["udp"].(bool))
}

// TestGenerator_ClashYAML_MultipleNodes 测试多节点生成。
func TestGenerator_ClashYAML_MultipleNodes(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{Name: "HK-01", Server: "hk.example.com", Port: 443, UUID: "uuid1", ServerName: "example.com", PublicKey: "key1", ShortID: "s1", Fingerprint: "chrome", Flow: "xtls-rprx-vision"},
		{Name: "US-01", Server: "us.example.com", Port: 443, UUID: "uuid2", ServerName: "example.com", PublicKey: "key2", ShortID: "s2", Fingerprint: "chrome", Flow: "xtls-rprx-vision"},
		{Name: "JP-01", Server: "jp.example.com", Port: 443, UUID: "uuid3", ServerName: "example.com", PublicKey: "key3", ShortID: "s3", Fingerprint: "chrome", Flow: "xtls-rprx-vision"},
	}

	yamlContent := gen.GenerateClashYAML(nodes)

	var config map[string]interface{}
	yaml.Unmarshal([]byte(yamlContent), &config)

	proxies := config["proxies"].([]interface{})
	assert.Len(t, proxies, 3)

	// 验证代理组包含所有节点
	groups := config["proxy-groups"].([]interface{})
	proxyGroup := groups[0].(map[string]interface{})
	proxyNames := proxyGroup["proxies"].([]interface{})
	assert.Len(t, proxyNames, 4) // 3 个节点 + DIRECT
	assert.Contains(t, proxyNames, "HK-01")
	assert.Contains(t, proxyNames, "US-01")
	assert.Contains(t, proxyNames, "JP-01")
	assert.Contains(t, proxyNames, "DIRECT")
}

func TestGenerator_GenerateByToken_UsesRegionAndManualProxyGroups(t *testing.T) {
	db, gen := setupSubTestDB(t)
	ctx := context.Background()

	user := &model.User{UUID: "group-user", Username: "groupuser", PasswordHash: "h", XrayUserKey: "group@x", Status: "active"}
	require.NoError(t, db.Create(user).Error)
	plan := &model.Plan{Name: "GroupPlan", Price: 10, DurationDays: 30, TrafficLimit: 10_000, IsActive: true}
	require.NoError(t, db.Create(plan).Error)
	nodeGroup := &model.NodeGroup{Name: "group-nodes"}
	require.NoError(t, db.Create(nodeGroup).Error)
	require.NoError(t, db.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", plan.ID, nodeGroup.ID).Error)

	hkNode := &model.Node{
		Name: "香港 01", RegionCode: "HK", RegionName: "香港", RegionFlag: "🇭🇰",
		Protocol: "vless", Host: "hk.example.com", Port: 443, ServerName: "www.microsoft.com", PublicKey: "hk-pk", ShortID: "hk-sid", Fingerprint: "chrome",
		NodeGroupID: &nodeGroup.ID, AgentBaseURL: "http://hk:8080", AgentTokenHash: "hash", IsEnabled: true,
	}
	usNode := &model.Node{
		Name: "美国 01", RegionCode: "US", RegionName: "美国", RegionFlag: "🇺🇸",
		Protocol: "vless", Host: "us.example.com", Port: 443, ServerName: "www.microsoft.com", PublicKey: "us-pk", ShortID: "us-sid", Fingerprint: "chrome",
		NodeGroupID: &nodeGroup.ID, AgentBaseURL: "http://us:8080", AgentTokenHash: "hash", IsEnabled: true,
	}
	require.NoError(t, db.Create(hkNode).Error)
	require.NoError(t, db.Create(usNode).Error)
	sub := &model.UserSubscription{
		UserID: user.ID, PlanID: plan.ID, StartDate: time.Now(), ExpireDate: time.Now().AddDate(0, 0, 30),
		TrafficLimit: 10_000, UsedTraffic: 0, Status: "ACTIVE",
	}
	require.NoError(t, db.Create(sub).Error)
	require.NoError(t, db.Create(&model.SubscriptionToken{UserID: user.ID, SubscriptionID: &sub.ID, Token: "manual-group-token"}).Error)
	settingRepo := repository.NewSiteSettingRepository(db)
	_, err := settingRepo.Upsert(ctx, model.SiteSettingSubscriptionConfig, fmt.Sprintf(`{
		"profile_name":"LeiYun",
		"custom_rules":["MATCH,PROXY"],
		"include_region_icon":true,
		"enable_url_test_group":true,
		"node_name_template":"{{flag}} {{region}} {{name}}",
		"health_check_url":"http://cp.cloudflare.com/generate_204",
		"url_test_interval":300,
		"proxy_groups":[
			{"name":"PROXY","type":"select","include_all":true,"include_auto":true,"include_direct":true},
			{"name":"空分组","type":"select","node_ids":[],"include_all":false,"include_auto":false,"include_direct":false},
			{"name":"美国节点","type":"select","node_ids":[%d],"include_all":false,"include_auto":false,"include_direct":false}
		]
	}`, usNode.ID))
	require.NoError(t, err)

	result, err := gen.GenerateByToken(ctx, "manual-group-token")
	require.NoError(t, err)
	var config map[string]interface{}
	require.NoError(t, yaml.Unmarshal([]byte(result.Content), &config))

	proxies := config["proxies"].([]interface{})
	require.Len(t, proxies, 2)
	assert.Equal(t, "🇭🇰 香港 香港 01", proxies[0].(map[string]interface{})["name"])
	assert.Equal(t, "🇺🇸 美国 美国 01", proxies[1].(map[string]interface{})["name"])

	groups := config["proxy-groups"].([]interface{})
	require.Len(t, groups, 3)
	proxyGroup := groups[0].(map[string]interface{})
	assert.Equal(t, "PROXY", proxyGroup["name"])
	assert.Contains(t, proxyGroup["proxies"].([]interface{}), "自动选择")
	assert.Contains(t, proxyGroup["proxies"].([]interface{}), "🇭🇰 香港 香港 01")
	assert.Contains(t, proxyGroup["proxies"].([]interface{}), "🇺🇸 美国 美国 01")

	usGroup := groups[1].(map[string]interface{})
	assert.Equal(t, "美国节点", usGroup["name"])
	assert.Equal(t, []interface{}{"🇺🇸 美国 美国 01"}, usGroup["proxies"].([]interface{}))

	autoGroup := groups[2].(map[string]interface{})
	assert.Equal(t, "自动选择", autoGroup["name"])
	assert.Equal(t, "url-test", autoGroup["type"])
	assert.NotContains(t, fmt.Sprint(groups), "空分组")
	assert.NotContains(t, fmt.Sprint(groups), "fallback")
}
