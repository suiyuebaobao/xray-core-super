// Package subscription 提供订阅生成能力。
//
// 职责：
// - 根据订阅 token 查询有效订阅
// - 查询套餐关联的节点组
// - 查询节点组下的启用节点
// - 生成三种格式的订阅内容（Clash YAML、Base64 聚合、纯文本 URI）
// - 更新 token 最后使用时间
//
// 订阅格式设计：
// - 抽象 NodeConfig 作为统一数据源
// - Clash YAML 作为一等公民，从 NodeConfig 直接生成
// - Base64 和纯文本 URI 从 Clash YAML 转换而来
package subscription

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"

	"gopkg.in/yaml.v3"
)

// Generator 订阅生成器。
type Generator struct {
	subRepo   *repository.SubscriptionRepository
	tokenRepo *repository.SubscriptionTokenRepository
	planRepo  *repository.PlanRepository
	nodeRepo  *repository.NodeRepository
	userRepo  *repository.UserRepository
	relayRepo *repository.RelayBackendRepository
}

// NewGenerator 创建订阅生成器。
func NewGenerator(subRepo *repository.SubscriptionRepository, tokenRepo *repository.SubscriptionTokenRepository, planRepo *repository.PlanRepository, nodeRepo *repository.NodeRepository, userRepo *repository.UserRepository, relayRepo ...*repository.RelayBackendRepository) *Generator {
	var rbRepo *repository.RelayBackendRepository
	if len(relayRepo) > 0 {
		rbRepo = relayRepo[0]
	}
	return &Generator{
		subRepo:   subRepo,
		tokenRepo: tokenRepo,
		planRepo:  planRepo,
		nodeRepo:  nodeRepo,
		userRepo:  userRepo,
		relayRepo: rbRepo,
	}
}

// GenerateResult 订阅生成结果。
type GenerateResult struct {
	Content      string                  // 订阅内容
	ContentType  string                  // MIME 类型
	Filename     string                  // 下载文件名
	ETag         string                  // 缓存标识
	User         *model.User             // 用户信息
	Subscription *model.UserSubscription // 订阅信息
}

// GenerateByToken 根据订阅 token 生成指定格式的订阅内容。
func (g *Generator) GenerateByToken(ctx context.Context, tokenString string, format string) (*GenerateResult, error) {
	// 1. 查询订阅 token
	token, err := g.tokenRepo.FindByToken(ctx, tokenString)
	if err != nil {
		return nil, response.ErrSubscriptionExpire
	}

	// 2. Token 是用户级凭证，下载时使用该用户当前有效订阅。
	sub, err := g.subRepo.FindActiveByUserID(ctx, token.UserID)
	if err != nil {
		return nil, response.ErrSubscriptionExpire
	}

	// 检查订阅状态
	if sub.Status != "ACTIVE" {
		return nil, response.ErrSubscriptionExpire
	}

	// 检查是否过期
	if time.Now().After(sub.ExpireDate) {
		return nil, response.ErrSubscriptionExpire
	}

	// 检查流量是否超限
	if sub.TrafficLimit > 0 && sub.UsedTraffic >= sub.TrafficLimit {
		return nil, response.ErrSubscriptionExpire
	}

	// 3. 查询用户
	user, err := g.userRepo.FindByID(ctx, token.UserID)
	if err != nil {
		return nil, response.ErrInternalServer
	}
	if user.Status != "active" {
		return nil, response.ErrSubscriptionExpire
	}

	// 4. 查询套餐
	plan, err := g.planRepo.FindByID(ctx, sub.PlanID)
	if err != nil {
		return nil, response.ErrInternalServer
	}

	// 5. 查询套餐关联的节点组
	planNodeGroups, err := g.planRepo.FindNodeGroupIDs(ctx, plan.ID)
	if err != nil {
		return nil, response.ErrInternalServer
	}

	// 6. 查询节点组下的启用节点
	var allNodes []model.Node
	seenNodes := make(map[uint64]struct{})
	for _, ngID := range planNodeGroups {
		nodes, err := g.nodeRepo.FindByGroupID(ctx, ngID, true)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			if _, ok := seenNodes[node.ID]; ok {
				continue
			}
			seenNodes[node.ID] = struct{}{}
			allNodes = append(allNodes, node)
		}
	}

	if len(allNodes) == 0 {
		return nil, response.ErrInternalServer
	}

	// 7. 构建 NodeConfig 列表。nodes 表表示出口节点；中转线路只替换 server/port，Reality 参数仍沿用出口节点。
	nodeConfigs := make([]NodeConfig, 0, len(allNodes))
	nodesByID := make(map[uint64]model.Node, len(allNodes))
	relayNodeIDs := make([]uint64, 0, len(allNodes))
	for _, node := range allNodes {
		nodesByID[node.ID] = node
		if allowsDirectLine(node.LineMode) {
			nodeConfigs = append(nodeConfigs, buildNodeConfigFromExitNode(node, user.UUID, node.Name, node.Host, node.Port))
		}
		if allowsRelayLine(node.LineMode) {
			relayNodeIDs = append(relayNodeIDs, node.ID)
		}
	}

	if g.relayRepo != nil && len(relayNodeIDs) > 0 {
		relayBackends, err := g.relayRepo.ListEnabledByExitNodeIDs(ctx, relayNodeIDs)
		if err != nil {
			return nil, response.ErrInternalServer
		}
		for _, backend := range relayBackends {
			exitNode, ok := nodesByID[backend.ExitNodeID]
			if !ok || backend.Relay == nil {
				continue
			}
			lineName := backend.Name
			if lineName == "" {
				lineName = fmt.Sprintf("%s -> %s", backend.Relay.Name, exitNode.Name)
			}
			nodeConfigs = append(nodeConfigs, buildNodeConfigFromExitNode(exitNode, user.UUID, lineName, backend.Relay.Host, backend.ListenPort))
		}
	}

	if len(nodeConfigs) == 0 {
		return nil, response.ErrInternalServer
	}

	// 8. 按格式生成
	var content string
	var contentType string
	var filename string

	switch format {
	case "clash":
		content = g.generateClashYAML(nodeConfigs)
		contentType = "text/yaml; charset=utf-8"
		filename = "config.yaml"
	case "base64":
		// Base64 格式：将 URI 列表做 Base64 编码，适用于通用客户端
		plainContent := g.generatePlainURI(nodeConfigs)
		content = base64.StdEncoding.EncodeToString([]byte(plainContent))
		contentType = "text/plain; charset=utf-8"
		filename = "config.txt"
	case "plain":
		content = g.generatePlainURI(nodeConfigs)
		contentType = "text/plain; charset=utf-8"
		filename = "config.txt"
	default:
		return nil, response.ErrBadRequest
	}

	// 9. 更新 token 最后使用时间
	_ = g.tokenRepo.UpdateLastUsed(ctx, token.ID)

	// 10. 生成 ETag
	etag := fmt.Sprintf("sub-%d-%d-%d", token.ID, len(nodeConfigs), sub.UpdatedAt.Unix())

	return &GenerateResult{
		Content:      content,
		ContentType:  contentType,
		Filename:     filename,
		ETag:         etag,
		User:         user,
		Subscription: sub,
	}, nil
}

// NodeConfig 节点配置抽象，作为统一数据源。
type NodeConfig struct {
	Name        string
	Server      string
	Port        uint32
	UUID        string
	ServerName  string
	PublicKey   string
	ShortID     string
	Fingerprint string
	Flow        string
}

func buildNodeConfigFromExitNode(node model.Node, uuid string, name string, server string, port uint32) NodeConfig {
	return NodeConfig{
		Name:        name,
		Server:      server,
		Port:        port,
		UUID:        uuid,
		ServerName:  node.ServerName,
		PublicKey:   node.PublicKey,
		ShortID:     node.ShortID,
		Fingerprint: node.Fingerprint,
		Flow:        node.Flow,
	}
}

func allowsDirectLine(lineMode string) bool {
	switch lineMode {
	case "relay_only":
		return false
	default:
		return true
	}
}

func allowsRelayLine(lineMode string) bool {
	switch lineMode {
	case "direct_only":
		return false
	default:
		return true
	}
}

// GenerateClashYAML 公开方法：生成 Clash/mihomo 格式的 YAML 配置。
func (g *Generator) GenerateClashYAML(nodes []NodeConfig) string {
	return g.generateClashYAML(nodes)
}

// GenerateBase64 公开方法：生成 Base64 编码的聚合订阅。
func (g *Generator) GenerateBase64(nodes []NodeConfig) string {
	clashContent := g.generateClashYAML(nodes)
	return base64.StdEncoding.EncodeToString([]byte(clashContent))
}

// GeneratePlainURI 公开方法：生成纯文本 URI 分享链接。
func (g *Generator) GeneratePlainURI(nodes []NodeConfig) string {
	return g.generatePlainURI(nodes)
}

// generateClashYAML 生成 Clash/mihomo 格式的 YAML 配置。
func (g *Generator) generateClashYAML(nodes []NodeConfig) string {
	proxies := make([]map[string]interface{}, 0, len(nodes))
	for i, nc := range nodes {
		proxy := map[string]interface{}{
			"name":               nc.Name,
			"type":               "vless",
			"server":             nc.Server,
			"port":               nc.Port,
			"uuid":               nc.UUID,
			"network":            "tcp",
			"udp":                true,
			"tls":                true,
			"servername":         nc.ServerName,
			"flow":               nc.Flow,
			"client-fingerprint": nc.Fingerprint,
			"reality-opts": map[string]string{
				"public-key": nc.PublicKey,
				"short-id":   nc.ShortID,
			},
		}
		_ = i
		proxies = append(proxies, proxy)
	}

	proxyNames := make([]string, 0, len(nodes))
	for _, nc := range nodes {
		proxyNames = append(proxyNames, nc.Name)
	}
	proxyNames = append(proxyNames, "DIRECT")

	config := map[string]interface{}{
		"mixed-port": 7890,
		"allow-lan":  false,
		"mode":       "Rule",
		"log-level":  "info",
		"dns": map[string]interface{}{
			"enable":        true,
			"enhanced-mode": "fake-ip",
			"nameserver": []string{
				"223.5.5.5",
				"119.29.29.29",
			},
		},
		"proxies": proxies,
		"proxy-groups": []map[string]interface{}{
			{
				"name":    "PROXY",
				"type":    "select",
				"proxies": proxyNames,
			},
		},
		"rules": []string{
			"GEOIP,CN,DIRECT",
			"MATCH,PROXY",
		},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return "# Failed to generate config"
	}

	return string(data)
}

// generatePlainURI 生成纯文本 URI 分享链接（VLESS 格式）。
func (g *Generator) generatePlainURI(nodes []NodeConfig) string {
	var lines []string
	for _, nc := range nodes {
		// VLESS URI 格式：vless://uuid@server:port?encryption=none&flow=xtls-rprx-vision&security=reality&sni=SERVER_NAME&pbk=PUBLIC_KEY&sid=SHORT_ID&fp=FINGERPRINT&type=tcp#NAME
		uri := fmt.Sprintf("vless://%s@%s:%d?encryption=none&flow=%s&security=reality&sni=%s&pbk=%s&sid=%s&fp=%s&type=tcp#%s",
			nc.UUID,
			nc.Server,
			nc.Port,
			nc.Flow,
			nc.ServerName,
			nc.PublicKey,
			nc.ShortID,
			nc.Fingerprint,
			nc.Name,
		)
		lines = append(lines, uri)
	}
	return strings.Join(lines, "\n")
}
