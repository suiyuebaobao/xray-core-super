// generator_edge_test.go — 订阅生成器边界条件测试。
//
// 测试范围：
// - 空节点列表生成
// - 特殊字符处理
// - 大量节点生成
package subscription_test

import (
	"strings"
	"testing"

	"suiyue/internal/subscription"

	"github.com/stretchr/testify/assert"
)

// TestGenerator_EmptyNodes 测试空节点列表生成。
func TestGenerator_EmptyNodes(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{}

	yamlContent := gen.GenerateClashYAML(nodes)
	assert.Contains(t, yamlContent, "proxies: []")

	uriContent := gen.GeneratePlainURI(nodes)
	assert.Equal(t, "", uriContent)

	base64Content := gen.GenerateBase64(nodes)
	assert.NotEmpty(t, base64Content)
}

// TestGenerator_SpecialCharacters 测试特殊字符处理。
func TestGenerator_SpecialCharacters(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := []subscription.NodeConfig{
		{
			Name:        "HK-节点-01 [测试]",
			Server:      "hk.example.com",
			Port:        443,
			UUID:        "test-uuid",
			ServerName:  "www.microsoft.com",
			PublicKey:   "pubkey+/=",
			ShortID:     "sid",
			Fingerprint: "chrome",
			Flow:        "xtls-rprx-vision",
		},
	}

	yamlContent := gen.GenerateClashYAML(nodes)
	assert.Contains(t, yamlContent, "HK-节点-01 [测试]")

	uriContent := gen.GeneratePlainURI(nodes)
	assert.True(t, strings.HasPrefix(uriContent, "vless://"))
	assert.Contains(t, uriContent, "#HK-%E8%8A%82%E7%82%B9-01%20%5B%E6%B5%8B%E8%AF%95%5D")
}

// TestGenerator_MultipleNodes 测试大量节点生成。
func TestGenerator_MultipleNodes(t *testing.T) {
	_, gen := setupSubTestDB(t)

	nodes := make([]subscription.NodeConfig, 50)
	for i := 0; i < 50; i++ {
		nodes[i] = subscription.NodeConfig{
			Name:        "Node-" + string(rune('A'+i%26)),
			Server:      "node" + string(rune('0'+i)) + ".example.com",
			Port:        443,
			UUID:        "uuid-" + string(rune('0'+i)),
			ServerName:  "example.com",
			PublicKey:   "pubkey",
			ShortID:     "sid",
			Fingerprint: "chrome",
			Flow:        "xtls-rprx-vision",
		}
	}

	yamlContent := gen.GenerateClashYAML(nodes)
	assert.Contains(t, yamlContent, "Node-")

	uriContent := gen.GeneratePlainURI(nodes)
	lines := strings.Split(strings.TrimSpace(uriContent), "\n")
	assert.Len(t, lines, 50)
}
