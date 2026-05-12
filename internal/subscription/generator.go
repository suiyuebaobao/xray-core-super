// Package subscription 提供订阅生成能力。
//
// 职责：
// - 根据订阅 token 查询有效订阅
// - 查询套餐关联的节点组
// - 查询节点组下的启用节点
// - 生成 Clash/mihomo YAML 订阅内容
// - 更新 token 最后使用时间
//
// 订阅格式设计：
// - 抽象 NodeConfig 作为统一数据源
// - Clash/mihomo YAML 作为唯一下发格式，从 NodeConfig 直接生成
package subscription

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
	"unicode"

	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"

	"gopkg.in/yaml.v3"
)

// Generator 订阅生成器。
type Generator struct {
	subRepo     *repository.SubscriptionRepository
	tokenRepo   *repository.SubscriptionTokenRepository
	planRepo    *repository.PlanRepository
	nodeRepo    *repository.NodeRepository
	userRepo    *repository.UserRepository
	relayRepo   *repository.RelayBackendRepository
	settingRepo *repository.SiteSettingRepository
	profileName string
}

// NewGenerator 创建订阅生成器。
func NewGenerator(subRepo *repository.SubscriptionRepository, tokenRepo *repository.SubscriptionTokenRepository, planRepo *repository.PlanRepository, nodeRepo *repository.NodeRepository, userRepo *repository.UserRepository, relayRepo ...*repository.RelayBackendRepository) *Generator {
	var rbRepo *repository.RelayBackendRepository
	if len(relayRepo) > 0 {
		rbRepo = relayRepo[0]
	}
	return &Generator{
		subRepo:     subRepo,
		tokenRepo:   tokenRepo,
		planRepo:    planRepo,
		nodeRepo:    nodeRepo,
		userRepo:    userRepo,
		relayRepo:   rbRepo,
		profileName: "RayPilot",
	}
}

// SetProfileName 设置订阅导入后客户端可读取的配置文件名基础名称。
func (g *Generator) SetProfileName(name string) {
	g.profileName = sanitizeProfileName(name)
}

// SetSettingRepository 设置站点配置仓库，用于动态读取后台订阅配置。
func (g *Generator) SetSettingRepository(settingRepo *repository.SiteSettingRepository) {
	g.settingRepo = settingRepo
}

// GenerateResult 订阅生成结果。
type GenerateResult struct {
	Content      string                  // 订阅内容
	ContentType  string                  // MIME 类型
	Filename     string                  // 下载文件名
	ETag         string                  // 缓存标识
	Headers      map[string]string       // 订阅客户端可识别的响应头
	User         *model.User             // 用户信息
	Subscription *model.UserSubscription // 订阅信息
}

// GenerateByToken 根据订阅 token 生成 Clash/mihomo YAML 订阅内容。
func (g *Generator) GenerateByToken(ctx context.Context, tokenString string) (*GenerateResult, error) {
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
		if sub.ResidentialTrafficLimit == 0 || sub.ResidentialUsedTraffic >= sub.ResidentialTrafficLimit {
			return nil, response.ErrSubscriptionExpire
		}
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
			if !model.SubscriptionTrafficAvailableByPool(sub, node.TrafficPool) {
				continue
			}
			seenNodes[node.ID] = struct{}{}
			allNodes = append(allNodes, node)
		}
	}

	if len(allNodes) == 0 {
		return nil, response.ErrSubscriptionNoNodes
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
		return nil, response.ErrSubscriptionNoNodes
	}

	// 8. 读取订阅输出配置。后台配置优先，环境变量作为默认回退。
	outputConfig := g.loadOutputConfig(ctx)

	// 9. 只下发 Clash/mihomo YAML。旧的 base64/plain 入口已下线。
	content := g.generateClashYAMLWithConfig(nodeConfigs, outputConfig)
	contentType := "text/yaml; charset=utf-8"
	filename := filenameForProfileName(outputConfig.ProfileName)

	// 10. 更新 token 最后使用时间
	_ = g.tokenRepo.UpdateLastUsed(ctx, token.ID)

	// 11. 生成 ETag
	etag := fmt.Sprintf("sub-%d-%d-%d", token.ID, len(nodeConfigs), sub.UpdatedAt.Unix())

	return &GenerateResult{
		Content:      content,
		ContentType:  contentType,
		Filename:     filename,
		ETag:         etag,
		Headers:      g.resultHeaders(outputConfig, sub),
		User:         user,
		Subscription: sub,
	}, nil
}

func filenameForProfileName(profileName string) string {
	baseName := sanitizeProfileName(profileName)
	return trimProfileExtension(baseName)
}

func (g *Generator) loadOutputConfig(ctx context.Context) model.SubscriptionConfig {
	cfg := model.DefaultSubscriptionConfig()
	cfg.ProfileName = sanitizeProfileName(g.profileName)
	cfg = model.NormalizeSubscriptionConfig(cfg)
	if g.settingRepo == nil {
		return cfg
	}
	setting, err := g.settingRepo.FindByKey(ctx, model.SiteSettingSubscriptionConfig)
	if err != nil || setting == nil {
		return cfg
	}
	loaded := model.ParseSubscriptionConfig(setting.Value)
	if strings.TrimSpace(loaded.ProfileName) == "" {
		loaded.ProfileName = cfg.ProfileName
	}
	return model.NormalizeSubscriptionConfig(loaded)
}

func (g *Generator) resultHeaders(cfg model.SubscriptionConfig, sub *model.UserSubscription) map[string]string {
	headers := map[string]string{
		"profile-title": `base64:` + base64.StdEncoding.EncodeToString([]byte(cfg.ProfileName)),
	}
	if cfg.ProfileUpdateInterval > 0 {
		headers["profile-update-interval"] = fmt.Sprintf("%d", cfg.ProfileUpdateInterval)
	}
	if cfg.ProfileWebPageURL != "" {
		headers["profile-web-page-url"] = cfg.ProfileWebPageURL
	}
	if cfg.IncludeUserInfo && sub != nil {
		headers["subscription-userinfo"] = subscriptionUserInfoHeader(sub)
	}
	return headers
}

func subscriptionUserInfoHeader(sub *model.UserSubscription) string {
	used := sub.UsedTraffic + sub.ResidentialUsedTraffic
	totalParts := make([]uint64, 0, 2)
	if sub.TrafficLimit > 0 {
		totalParts = append(totalParts, sub.TrafficLimit)
	}
	if sub.ResidentialTrafficLimit > 0 {
		totalParts = append(totalParts, sub.ResidentialTrafficLimit)
	}
	var total uint64
	for _, value := range totalParts {
		total += value
	}
	return fmt.Sprintf("upload=0; download=%d; total=%d; expire=%d", used, total, sub.ExpireDate.Unix())
}

func sanitizeProfileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "RayPilot"
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		default:
			return r
		}
	}, name)
	name = strings.TrimSpace(strings.Trim(name, ".-"))
	if name == "" {
		return "RayPilot"
	}
	return name
}

func trimProfileExtension(name string) string {
	lower := strings.ToLower(name)
	for _, ext := range []string{".yaml", ".yml", ".txt"} {
		if strings.HasSuffix(lower, ext) {
			return name[:len(name)-len(ext)]
		}
	}
	return name
}

// NodeConfig 节点配置抽象，作为统一数据源。
type NodeConfig struct {
	NodeID       uint64
	Name         string
	RegionCode   string
	RegionName   string
	RegionFlag   string
	Server       string
	Port         uint32
	UUID         string
	ServerName   string
	PublicKey    string
	ShortID      string
	Fingerprint  string
	Flow         string
	UDPEnabled   *bool
	Transport    string
	OutboundType string
	XHTTPPath    string
	XHTTPHost    string
	XHTTPMode    string
}

func buildNodeConfigFromExitNode(node model.Node, uuid string, name string, server string, port uint32) NodeConfig {
	return NodeConfig{
		NodeID:       node.ID,
		Name:         name,
		RegionCode:   node.RegionCode,
		RegionName:   node.RegionName,
		RegionFlag:   node.RegionFlag,
		Server:       server,
		Port:         port,
		UUID:         uuid,
		ServerName:   node.ServerName,
		PublicKey:    node.PublicKey,
		ShortID:      node.ShortID,
		Fingerprint:  node.Fingerprint,
		Flow:         node.Flow,
		UDPEnabled:   boolPtr(node.UDPEnabled),
		Transport:    node.Transport,
		OutboundType: node.OutboundType,
		XHTTPPath:    node.XHTTPPath,
		XHTTPHost:    node.XHTTPHost,
		XHTTPMode:    node.XHTTPMode,
	}
}

func normalizeSubscriptionTransport(transport string) string {
	if strings.EqualFold(strings.TrimSpace(transport), "xhttp") {
		return "xhttp"
	}
	return "tcp"
}

func normalizeSubscriptionXHTTPPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/raypilot"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func normalizeSubscriptionXHTTPMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "packet-up", "stream-up", "stream-one":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}

func subscriptionFlowForNode(nc NodeConfig) string {
	if normalizeSubscriptionTransport(nc.Transport) == "xhttp" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(nc.OutboundType), model.NodeOutboundSocks5) {
		return ""
	}
	flow := strings.TrimSpace(nc.Flow)
	if flow == "" {
		return "xtls-rprx-vision"
	}
	return flow
}

func subscriptionUDPForNode(nc NodeConfig) bool {
	if nc.UDPEnabled != nil {
		return *nc.UDPEnabled
	}
	return !strings.EqualFold(strings.TrimSpace(nc.OutboundType), model.NodeOutboundSocks5)
}

func boolPtr(value bool) *bool {
	return &value
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
	return g.generateClashYAMLWithRules(nodes, model.DefaultSubscriptionConfig().CustomRules)
}

// generateClashYAML 生成 Clash/mihomo 格式的 YAML 配置。
func (g *Generator) generateClashYAML(nodes []NodeConfig) string {
	return g.generateClashYAMLWithRules(nodes, model.DefaultSubscriptionConfig().CustomRules)
}

func (g *Generator) generateClashYAMLWithRules(nodes []NodeConfig, rules []string) string {
	cfg := model.NormalizeSubscriptionConfig(model.SubscriptionConfig{
		ProfileName:        g.profileName,
		CustomRules:        rules,
		IncludeUserInfo:    true,
		IncludeRegionIcon:  model.DefaultSubscriptionConfig().IncludeRegionIcon,
		NodeNameTemplate:   model.DefaultSubscriptionConfig().NodeNameTemplate,
		HealthCheckURL:     model.DefaultSubscriptionConfig().HealthCheckURL,
		URLTestInterval:    model.DefaultSubscriptionConfig().URLTestInterval,
		EnableURLTestGroup: false,
	})
	return g.generateClashYAMLWithConfig(nodes, cfg)
}

func (g *Generator) generateClashYAMLWithConfig(nodes []NodeConfig, cfg model.SubscriptionConfig) string {
	cfg = model.NormalizeSubscriptionConfig(cfg)
	proxies := make([]map[string]interface{}, 0, len(nodes))
	displayItems := make([]subscriptionDisplayNode, 0, len(nodes))
	seenDisplayNames := make(map[string]int, len(nodes))
	for index, nc := range nodes {
		displayName := formatSubscriptionNodeName(nc, cfg, index+1)
		if count := seenDisplayNames[displayName]; count > 0 {
			displayName = fmt.Sprintf("%s %02d", displayName, count+1)
		}
		seenDisplayNames[displayName]++
		displayItems = append(displayItems, subscriptionDisplayNode{NodeID: nc.NodeID, Name: displayName})

		transport := normalizeSubscriptionTransport(nc.Transport)
		proxy := map[string]interface{}{
			"name":               displayName,
			"type":               "vless",
			"server":             nc.Server,
			"port":               nc.Port,
			"uuid":               nc.UUID,
			"network":            transport,
			"udp":                subscriptionUDPForNode(nc),
			"tls":                true,
			"servername":         nc.ServerName,
			"client-fingerprint": nc.Fingerprint,
			"reality-opts": map[string]string{
				"public-key": nc.PublicKey,
				"short-id":   nc.ShortID,
			},
		}
		if flow := subscriptionFlowForNode(nc); flow != "" {
			proxy["flow"] = flow
		}
		if transport == "xhttp" {
			xhttpOpts := map[string]string{
				"path": normalizeSubscriptionXHTTPPath(nc.XHTTPPath),
				"mode": normalizeSubscriptionXHTTPMode(nc.XHTTPMode),
			}
			if host := strings.TrimSpace(nc.XHTTPHost); host != "" {
				xhttpOpts["host"] = host
			}
			proxy["xhttp-opts"] = xhttpOpts
		}
		proxies = append(proxies, proxy)
	}

	proxyGroups := g.generateProxyGroups(displayItems, cfg)
	if cfg.EnableURLTestGroup && !generatedProxyGroupNameExists(proxyGroups, subscriptionAutoGroupName()) {
		proxyGroups = append(proxyGroups, map[string]interface{}{
			"name":     subscriptionAutoGroupName(),
			"type":     "url-test",
			"proxies":  displayNodeNames(displayItems),
			"url":      cfg.HealthCheckURL,
			"interval": cfg.URLTestInterval,
		})
	}

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
		"proxies":      proxies,
		"proxy-groups": proxyGroups,
		"rules":        cfg.CustomRules,
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return "# Failed to generate config"
	}

	return string(data)
}

func formatSubscriptionNodeName(nc NodeConfig, cfg model.SubscriptionConfig, index int) string {
	rawName := strings.TrimSpace(nc.Name)
	if rawName == "" {
		rawName = fmt.Sprintf("Node %02d", index)
	}
	flag := ""
	if cfg.IncludeRegionIcon {
		flag = strings.TrimSpace(nc.RegionFlag)
	}
	template := strings.TrimSpace(cfg.NodeNameTemplate)
	if template == "" {
		template = "{{flag}} {{name}}"
	}
	values := map[string]string{
		"{{flag}}":      flag,
		"{{name}}":      rawName,
		"{{region}}":    strings.TrimSpace(nc.RegionName),
		"{{code}}":      strings.TrimSpace(nc.RegionCode),
		"{{index}}":     fmt.Sprintf("%02d", index),
		"{{transport}}": strings.ToUpper(normalizeSubscriptionTransport(nc.Transport)),
		"{{pool}}":      trafficPoolNameForOutbound(nc.OutboundType),
	}
	name := template
	for placeholder, value := range values {
		name = strings.ReplaceAll(name, placeholder, value)
	}
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		name = rawName
	}
	return name
}

func trafficPoolNameForOutbound(outboundType string) string {
	if strings.EqualFold(strings.TrimSpace(outboundType), model.NodeOutboundSocks5) {
		return "家宽"
	}
	return "普通"
}

type subscriptionDisplayNode struct {
	NodeID uint64
	Name   string
}

func displayNodeNames(nodes []subscriptionDisplayNode) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.Name)
	}
	return names
}

func (g *Generator) generateProxyGroups(nodes []subscriptionDisplayNode, cfg model.SubscriptionConfig) []map[string]interface{} {
	groups := make([]map[string]interface{}, 0, len(cfg.ProxyGroups)+1)
	allNames := displayNodeNames(nodes)
	autoGroupName := subscriptionAutoGroupName()
	canReferenceAutoGroup := cfg.EnableURLTestGroup && (!subscriptionConfigHasGroupNamed(cfg.ProxyGroups, autoGroupName) || subscriptionConfigHasURLTestGroupNamed(cfg.ProxyGroups, autoGroupName))
	for index, group := range cfg.ProxyGroups {
		names := namesForSubscriptionGroup(nodes, group)
		if len(names) == 0 {
			continue
		}
		proxies := make([]string, 0, len(names)+2)
		if group.Type == "select" && canReferenceAutoGroup && !subscriptionGroupNameEquals(group.Name, autoGroupName) {
			if index == 0 || group.IncludeAuto {
				proxies = append(proxies, autoGroupName)
			}
		}
		proxies = append(proxies, names...)
		if group.IncludeDirect {
			proxies = append(proxies, "DIRECT")
		}
		outputGroup := map[string]interface{}{
			"name":    group.Name,
			"type":    group.Type,
			"proxies": proxies,
		}
		if group.Type == "url-test" {
			outputGroup["url"] = cfg.HealthCheckURL
			outputGroup["interval"] = cfg.URLTestInterval
		}
		groups = append(groups, outputGroup)
	}
	if len(groups) == 0 {
		proxies := make([]string, 0, len(allNames)+2)
		if canReferenceAutoGroup {
			proxies = append(proxies, autoGroupName)
		}
		proxies = append(proxies, allNames...)
		proxies = append(proxies, "DIRECT")
		groups = append(groups, map[string]interface{}{
			"name":    "PROXY",
			"type":    "select",
			"proxies": proxies,
		})
	}
	return groups
}

func namesForSubscriptionGroup(nodes []subscriptionDisplayNode, group model.SubscriptionProxyGroupConfig) []string {
	if group.IncludeAll {
		return displayNodeNames(nodes)
	}
	if len(group.NodeIDs) == 0 {
		return nil
	}
	wanted := make(map[uint64]struct{}, len(group.NodeIDs))
	for _, id := range group.NodeIDs {
		if id != 0 {
			wanted[id] = struct{}{}
		}
	}
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if _, ok := wanted[node.NodeID]; ok {
			names = append(names, node.Name)
		}
	}
	return names
}

func subscriptionAutoGroupName() string {
	return "自动选择"
}

func subscriptionConfigHasGroupNamed(groups []model.SubscriptionProxyGroupConfig, name string) bool {
	normalizedName := strings.TrimSpace(name)
	for _, group := range groups {
		if subscriptionGroupNameEquals(group.Name, normalizedName) {
			return true
		}
	}
	return false
}

func subscriptionConfigHasURLTestGroupNamed(groups []model.SubscriptionProxyGroupConfig, name string) bool {
	for _, group := range groups {
		if subscriptionGroupNameEquals(group.Name, name) && group.Type == "url-test" {
			return true
		}
	}
	return false
}

func subscriptionGroupNameEquals(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func generatedProxyGroupNameExists(groups []map[string]interface{}, name string) bool {
	normalizedName := strings.TrimSpace(name)
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(group["name"])), normalizedName) {
			return true
		}
	}
	return false
}
