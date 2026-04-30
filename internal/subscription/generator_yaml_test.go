// generator_yaml_test.go — 订阅生成器 YAML 输出测试。
package subscription_test

import (
	"strings"
	"testing"

	"suiyue/internal/subscription"

	"github.com/stretchr/testify/assert"
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

// TestGenerator_PlainURI_Format 测试纯文本 URI 格式正确。
func TestGenerator_PlainURI_Format(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "HK-01",
			Server:      "hk.example.com",
			Port:        443,
			UUID:        "test-uuid",
			ServerName:  "example.com",
			PublicKey:   "pubkey",
			ShortID:     "sid",
			Fingerprint: "chrome",
			Flow:        "xtls-rprx-vision",
		},
	}

	uriContent := gen.GeneratePlainURI(nodes)

	// 验证 URI 格式
	assert.True(t, strings.HasPrefix(uriContent, "vless://"))
	assert.Contains(t, uriContent, "@hk.example.com:443")
	assert.Contains(t, uriContent, "encryption=none")
	assert.Contains(t, uriContent, "flow=xtls-rprx-vision")
	assert.Contains(t, uriContent, "security=reality")
	assert.Contains(t, uriContent, "sni=example.com")
	assert.Contains(t, uriContent, "pbk=pubkey")
	assert.Contains(t, uriContent, "sid=sid")
	assert.Contains(t, uriContent, "fp=chrome")
	assert.Contains(t, uriContent, "type=tcp")
	assert.Contains(t, uriContent, "#HK-01")
}
