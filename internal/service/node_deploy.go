// node_deploy.go — 节点一键部署服务。
//
// 职责：
// - 通过 SSH 连接目标服务器
// - 自动检测/安装 Docker
// - 推送 node-agent 镜像
// - 启动容器
// - 创建节点记录
package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/secure"
	"suiyue/internal/platform/ssh"
	"suiyue/internal/repository"
)

// NodeDeployService 节点部署服务。
type NodeDeployService struct {
	nodeRepo      *repository.NodeRepository
	nodeHostRepo  *repository.NodeHostRepository
	nodeGroupRepo *repository.NodeGroupRepository
	relayRepo     *repository.RelayRepository
	nodeAccessSvc *NodeAccessService
}

const (
	nodeAgentContainerName     = "raypilot-node-agent"
	relayAgentContainerName    = "raypilot-relay-agent"
	deployContainerWaitTimeout = 45 * time.Second
	deployRealityWaitTimeout   = 45 * time.Second
	deployHeartbeatWaitTimeout = 45 * time.Second
	deployPortWaitTimeout      = 30 * time.Second
)

// NewNodeDeployService 创建节点部署服务。
func NewNodeDeployService(nodeRepo *repository.NodeRepository, nodeHostRepo ...*repository.NodeHostRepository) *NodeDeployService {
	var hostRepo *repository.NodeHostRepository
	if len(nodeHostRepo) > 0 {
		hostRepo = nodeHostRepo[0]
	}
	return &NodeDeployService{nodeRepo: nodeRepo, nodeHostRepo: hostRepo}
}

// NewNodeDeployServiceWithAutomation 创建带分组绑定、旧角色停用和用户同步能力的节点部署服务。
func NewNodeDeployServiceWithAutomation(
	nodeRepo *repository.NodeRepository,
	nodeHostRepo *repository.NodeHostRepository,
	nodeGroupRepo *repository.NodeGroupRepository,
	relayRepo *repository.RelayRepository,
	nodeAccessSvc *NodeAccessService,
) *NodeDeployService {
	return &NodeDeployService{
		nodeRepo:      nodeRepo,
		nodeHostRepo:  nodeHostRepo,
		nodeGroupRepo: nodeGroupRepo,
		relayRepo:     relayRepo,
		nodeAccessSvc: nodeAccessSvc,
	}
}

// DeployRequest 一键部署请求。
type DeployRequest struct {
	SSHHost             string   `json:"ssh_host" binding:"required"`
	SSHPort             int      `json:"ssh_port"`
	SSHUser             string   `json:"ssh_user" binding:"required"`
	SSHPassword         string   `json:"ssh_password" binding:"required"`
	CenterURL           string   `json:"center_url" binding:"required"`
	CenterURLs          []string `json:"center_urls"`
	NodeToken           string   `json:"node_token"`
	NodeName            string   `json:"node_name"`
	TrafficPool         string   `json:"traffic_pool"`
	OutboundType        string   `json:"outbound_type"`
	UDPEnabled          *bool    `json:"udp_enabled"`
	OutboundIP          string   `json:"outbound_ip"`
	OutboundProxyURL    string   `json:"outbound_proxy_url"`
	Transport           string   `json:"transport"`
	Transports          []string `json:"transports"`
	TCPPort             uint32   `json:"tcp_port"`
	XHTTPPort           uint32   `json:"xhttp_port"`
	XHTTPPath           string   `json:"xhttp_path"`
	XHTTPHost           string   `json:"xhttp_host"`
	XHTTPMode           string   `json:"xhttp_mode"`
	MultiIPEnabled      bool     `json:"multi_ip_enabled"`
	SelectedIPs         []string `json:"selected_ips"`
	NodeGroupIDs        []uint64 `json:"node_group_ids"`
	ReplaceExistingRole bool     `json:"replace_existing_role"`
}

func normalizeDeployOutboundRequest(req *DeployRequest) error {
	if normalizeDeployOutboundType(req.OutboundType) != model.NodeOutboundSocks5 {
		return nil
	}
	if len(normalizeDeployOutboundProxyURLs(req.OutboundType, req.OutboundProxyURL)) == 0 {
		return fmt.Errorf("至少需要一条 socks5 上游代理")
	}
	return nil
}

func normalizeDeployCenterRequest(req *DeployRequest) error {
	centerURLs := normalizeCenterURLList(req.CenterURL, req.CenterURLs)
	if len(centerURLs) == 0 {
		return fmt.Errorf("center_url must be a valid http or https URL")
	}
	req.CenterURL = centerURLs[0]
	if len(centerURLs) > 1 {
		req.CenterURLs = centerURLs[1:]
	} else {
		req.CenterURLs = nil
	}
	return nil
}

func normalizeCenterURLList(primary string, values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values)+1)
	add := func(raw string) {
		for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			item = normalizeCenterURL(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	add(primary)
	for _, value := range values {
		add(value)
	}
	for _, value := range append([]string(nil), result...) {
		for _, fallback := range knownCenterFallbackURLs(value) {
			add(fallback)
		}
	}
	return result
}

func normalizeCenterURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return ""
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func knownCenterFallbackURLs(raw string) []string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil
	}
	var hostnames []string
	switch strings.ToLower(parsed.Hostname()) {
	case "leiyunai.fun":
		hostnames = []string{"154.219.106.105", "154.219.106.53"}
	case "154.219.106.105":
		hostnames = []string{"leiyunai.fun", "154.219.106.53"}
	case "154.219.106.53":
		hostnames = []string{"leiyunai.fun", "154.219.106.105"}
	default:
		return nil
	}
	result := make([]string, 0, len(hostnames))
	for _, hostname := range hostnames {
		clone := *parsed
		clone.Host = replaceURLHostname(&clone, hostname)
		if normalized := normalizeCenterURL(clone.String()); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func replaceURLHostname(parsed *url.URL, hostname string) string {
	if port := parsed.Port(); port != "" {
		return net.JoinHostPort(hostname, port)
	}
	return hostname
}

func centerURLsEnvValue(primary string, values []string) string {
	urls := normalizeCenterURLList(primary, values)
	return strings.Join(urls, ",")
}

// DeployResult 部署结果。
type DeployResult struct {
	NodeID        uint64   `json:"node_id,omitempty"`
	NodeIDs       []uint64 `json:"node_ids,omitempty"`
	NodeHostID    uint64   `json:"node_host_id,omitempty"`
	NodeToken     string   `json:"node_token,omitempty"`
	NodeHostToken string   `json:"node_host_token,omitempty"`
	Success       bool     `json:"success"`
	Message       string   `json:"message"`
	Steps         []Step   `json:"steps"`
}

// RepairCenterRequest 通过 SSH 兜底修复 node-agent 中心地址请求。
type RepairCenterRequest struct {
	SSHHost            string   `json:"ssh_host" binding:"required"`
	SSHPort            int      `json:"ssh_port"`
	SSHUser            string   `json:"ssh_user" binding:"required"`
	SSHPassword        string   `json:"ssh_password" binding:"required"`
	CenterURL          string   `json:"center_url"`
	CenterURLs         []string `json:"center_urls"`
	NodeID             uint64   `json:"node_id"`
	NodeHostID         uint64   `json:"node_host_id"`
	RelayID            uint64   `json:"relay_id"`
	WaitTimeoutSeconds int      `json:"wait_timeout_seconds"`
}

// RepairCenterResult 兜底修复结果。
type RepairCenterResult struct {
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	CenterURLs []string `json:"center_urls"`
	Steps      []Step   `json:"steps"`
}

// Step 部署步骤。
type Step struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // success, failed, running
	Message string `json:"message"`
}

// ScanIPsRequest 扫描服务器出口 IP 请求。
type ScanIPsRequest struct {
	SSHHost     string `json:"ssh_host" binding:"required"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user" binding:"required"`
	SSHPassword string `json:"ssh_password" binding:"required"`
}

// ScannedIP 表示服务器上的一个候选公网 IPv4。
type ScannedIP struct {
	IP         string `json:"ip"`
	Interface  string `json:"interface"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	OutboundIP string `json:"outbound_ip,omitempty"`
	IsPublic   bool   `json:"is_public"`
	IsUsable   bool   `json:"is_usable"`
	SkipReason string `json:"skip_reason,omitempty"`
}

// ScanIPsResult 扫描服务器出口 IP 结果。
type ScanIPsResult struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	IPs     []ScannedIP `json:"ips"`
	Steps   []Step      `json:"steps"`
}

type serverIP struct {
	IP        string
	Interface string
}

// MultiExitNodeConfig 是部署给 node-agent 的逻辑节点配置。
type MultiExitNodeConfig struct {
	NodeID            uint64 `json:"node_id"`
	IP                string `json:"ip"`
	Port              uint32 `json:"port"`
	Transport         string `json:"transport"`
	OutboundType      string `json:"outbound_type,omitempty"`
	OutboundIP        string `json:"outbound_ip,omitempty"`
	OutboundProxyURL  string `json:"outbound_proxy_url,omitempty"`
	XHTTPPath         string `json:"xhttp_path,omitempty"`
	XHTTPHost         string `json:"xhttp_host,omitempty"`
	XHTTPMode         string `json:"xhttp_mode,omitempty"`
	InboundTag        string `json:"inbound_tag"`
	OutboundTag       string `json:"outbound_tag"`
	XrayUserKeyPrefix string `json:"xray_user_key_prefix"`
}

type deployTransportOption struct {
	Transport string
	Port      uint32
	XHTTPPath string
	XHTTPHost string
	XHTTPMode string
	Flow      string
}

type deployListenEndpoint struct {
	IP   string
	Port uint32
}

func listenEndpointKey(ip string, port uint32) string {
	return fmt.Sprintf("%s:%d", strings.TrimSpace(ip), port)
}

func normalizeDeployOutboundProxyURLs(outboundType string, raw string) []string {
	if normalizeDeployOutboundType(outboundType) != model.NodeOutboundSocks5 {
		return []string{""}
	}
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(raw, "\n")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		result = append(result, value)
	}
	return result
}

func deployProxyNodeName(base, host string, proxyIndex int, proxyCount int, option deployTransportOption, optionCount int) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "raypilot-node-" + strings.TrimSpace(host)
	}
	name := base
	if proxyCount > 1 {
		name = fmt.Sprintf("%s-%d", name, proxyIndex+1)
	}
	if optionCount > 1 && option.Transport != "tcp" {
		name = fmt.Sprintf("%s-%s", name, strings.ToUpper(option.Transport))
	}
	return name
}

func normalizeDeployTransportOptions(req *DeployRequest) ([]deployTransportOption, error) {
	transports := normalizeTransportList(req.Transports, req.Transport)
	if len(transports) == 0 {
		transports = []string{"tcp"}
	}
	if len(transports) > 2 {
		return nil, fmt.Errorf("最多只能选择 2 种传输模式")
	}

	xhttpPath := normalizeXHTTPPath(req.XHTTPPath)
	xhttpHost := strings.TrimSpace(req.XHTTPHost)
	xhttpMode := normalizeXHTTPMode(req.XHTTPMode)
	tcpPort := req.TCPPort
	if tcpPort == 0 {
		tcpPort = 443
	}
	xhttpPort := req.XHTTPPort
	if xhttpPort == 0 {
		if len(transports) > 1 {
			xhttpPort = 8443
		} else {
			xhttpPort = 443
		}
	}

	options := make([]deployTransportOption, 0, len(transports))
	seenPorts := map[uint32]string{}
	for _, transport := range transports {
		option := deployTransportOption{Transport: transport}
		switch transport {
		case "xhttp":
			option.Port = xhttpPort
			option.XHTTPPath = xhttpPath
			option.XHTTPHost = xhttpHost
			option.XHTTPMode = xhttpMode
			option.Flow = ""
		default:
			option.Transport = "tcp"
			option.Port = tcpPort
			option.XHTTPMode = "auto"
			option.Flow = "xtls-rprx-vision"
		}
		if option.Port == 0 || option.Port > 65535 {
			return nil, fmt.Errorf("%s 端口无效: %d", strings.ToUpper(option.Transport), option.Port)
		}
		if existing, ok := seenPorts[option.Port]; ok {
			return nil, fmt.Errorf("%s 与 %s 不能使用同一个端口 %d", strings.ToUpper(existing), strings.ToUpper(option.Transport), option.Port)
		}
		seenPorts[option.Port] = option.Transport
		options = append(options, option)
	}

	primary := options[0]
	req.Transport = primary.Transport
	req.Transports = transports
	req.TCPPort = tcpPort
	req.XHTTPPort = xhttpPort
	req.XHTTPPath = xhttpPath
	req.XHTTPHost = xhttpHost
	req.XHTTPMode = xhttpMode
	return options, nil
}

func normalizeTransportList(transports []string, fallback string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, 2)
	appendTransport := func(raw string) {
		transport := normalizeTransport(raw)
		if _, ok := seen[transport]; ok {
			return
		}
		seen[transport] = struct{}{}
		result = append(result, transport)
	}
	for _, transport := range transports {
		appendTransport(transport)
	}
	if len(result) == 0 {
		appendTransport(fallback)
	}
	ordered := make([]string, 0, len(result))
	for _, transport := range []string{"tcp", "xhttp"} {
		if _, ok := seen[transport]; ok {
			ordered = append(ordered, transport)
		}
	}
	return ordered
}

func applyDeployTransportOption(node *model.Node, option deployTransportOption) {
	node.Transport = option.Transport
	node.Port = option.Port
	node.Flow = option.Flow
	node.XHTTPPath = ""
	node.XHTTPHost = ""
	node.XHTTPMode = "auto"
	if option.Transport == "xhttp" {
		node.XHTTPPath = option.XHTTPPath
		node.XHTTPHost = option.XHTTPHost
		node.XHTTPMode = option.XHTTPMode
	}
	if node.OutboundType == model.NodeOutboundSocks5 {
		node.Flow = ""
	}
}

func normalizeTransport(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "xhttp":
		return "xhttp"
	default:
		return "tcp"
	}
}

func normalizeXHTTPPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/raypilot"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

func normalizeXHTTPMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "packet-up", "stream-up", "stream-one":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "auto"
	}
}

// Deploy 一键部署节点。
func (s *NodeDeployService) Deploy(ctx context.Context, req *DeployRequest) (*DeployResult, error) {
	if err := normalizeDeployCenterRequest(req); err != nil {
		return &DeployResult{Steps: []Step{}}, err
	}
	if err := normalizeDeployOutboundRequest(req); err != nil {
		return &DeployResult{Steps: []Step{}}, err
	}
	options, err := normalizeDeployTransportOptions(req)
	if err != nil {
		return &DeployResult{Steps: []Step{}}, err
	}
	proxyURLs := normalizeDeployOutboundProxyURLs(req.OutboundType, req.OutboundProxyURL)
	if normalizeDeployOutboundType(req.OutboundType) == model.NodeOutboundSocks5 && len(proxyURLs) == 0 {
		return &DeployResult{Steps: []Step{}}, fmt.Errorf("socks5 outbound requires at least one upstream proxy url")
	}
	if req.MultiIPEnabled && len(proxyURLs) > 1 {
		return &DeployResult{Steps: []Step{}}, fmt.Errorf("multi_ip_enabled and multiple outbound proxies cannot be enabled together in one deployment")
	}
	if req.MultiIPEnabled || len(options) > 1 || len(proxyURLs) > 1 {
		return s.deployMultiLine(ctx, req, options)
	}

	result := &DeployResult{Steps: []Step{}}
	var sshClient *ssh.Client
	var createdNodeID uint64
	containerStarted := false

	addStep := func(name, status, msg string) {
		log.Printf("[deploy] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}
	fail := func(format string, args ...interface{}) (*DeployResult, error) {
		err := fmt.Errorf(format, args...)
		if containerStarted && sshClient != nil {
			s.collectContainerDiagnostics(sshClient, nodeAgentContainerName, addStep)
			if cleanupErr := s.removeDeployContainers(sshClient, nodeAgentContainerName, "suiyue-node-agent"); cleanupErr != nil {
				addStep("清理容器", "failed", cleanupErr.Error())
			} else {
				addStep("清理容器", "success", "已移除本次失败部署的 node-agent 容器")
			}
		}
		if createdNodeID > 0 {
			if delErr := s.nodeRepo.Delete(ctx, createdNodeID); delErr != nil {
				addStep("清理记录", "failed", delErr.Error())
			} else {
				addStep("清理记录", "success", fmt.Sprintf("已删除未完成部署的节点记录 %d", createdNodeID))
			}
		}
		return result, err
	}

	imagePath, imageSize, err := locateNodeAgentImage()
	if err != nil {
		addStep("镜像预检", "failed", err.Error())
		return result, err
	}
	addStep("镜像预检", "success", fmt.Sprintf("本地镜像包可用: %s (%s)", imagePath, formatBytes(imageSize)))

	// Step 1: 连接 SSH
	addStep("SSH 连接", "running", fmt.Sprintf("连接到 %s@%s:%d", req.SSHUser, req.SSHHost, req.SSHPort))
	sshClient = ssh.New(ssh.Config{
		Host:     req.SSHHost,
		Port:     req.SSHPort,
		User:     req.SSHUser,
		Password: req.SSHPassword,
	})
	if err := sshClient.Connect(); err != nil {
		addStep("SSH 连接", "failed", err.Error())
		return result, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	addStep("SSH 连接", "success", "连接成功")

	addStep("服务器预检", "running", "检查系统、磁盘和基础命令")
	if err := s.preflightRemoteServer(sshClient, imageSize); err != nil {
		addStep("服务器预检", "failed", err.Error())
		return result, fmt.Errorf("服务器预检失败: %w", err)
	}
	addStep("服务器预检", "success", "目标服务器满足 Docker 部署要求")

	// Step 2: 检测/安装 Docker
	addStep("检测 Docker", "running", "检查 Docker 是否已安装")
	dockerInstalled, _ := s.checkDocker(sshClient)
	if !dockerInstalled {
		addStep("安装 Docker", "running", "Docker 未安装，开始安装")
		if err := s.installDocker(sshClient); err != nil {
			addStep("安装 Docker", "failed", err.Error())
			return result, fmt.Errorf("Docker 安装失败: %w", err)
		}
		addStep("安装 Docker", "success", "Docker 安装成功")
	} else {
		addStep("检测 Docker", "success", "Docker 已安装")
	}

	// Step 3: 检查 node-agent 是否已运行
	addStep("检查容器", "running", "检查 node-agent 是否已运行")
	running, _ := s.checkContainerRunning(sshClient)
	if running {
		addStep("检查容器", "success", "node-agent 已在运行")
	} else {
		addStep("检查容器", "success", "无运行中的 node-agent")
	}

	// Step 4: 推送 Docker 镜像
	addStep("推送镜像", "running", "准备推送 node-agent 镜像")
	if err := s.pushImage(sshClient, imagePath); err != nil {
		addStep("推送镜像", "failed", err.Error())
		return result, fmt.Errorf("镜像推送失败: %w", err)
	}
	addStep("推送镜像", "success", "镜像推送成功")

	addStep("清理旧节点代理", "running", "清理旧 systemd node-agent、旧容器和冲突的宿主机 Xray")
	if err := s.cleanupLegacyNodeAgent(sshClient); err != nil {
		addStep("清理旧节点代理", "failed", err.Error())
		return result, err
	}
	addStep("清理旧节点代理", "success", "旧节点代理已清理")

	requestedEndpoints := []deployListenEndpoint{{IP: deployListenIP(false, req.SSHHost), Port: options[0].Port}}
	addStep("检查端口占用", "running", fmt.Sprintf("检查节点端点 %s 是否空闲", formatDeployListenEndpoints(requestedEndpoints)))
	if err := s.ensureEndpointsFree(sshClient, requestedEndpoints); err != nil {
		addStep("检查端口占用", "failed", err.Error())
		return result, err
	}
	addStep("检查端口占用", "success", "节点端口可用")

	nodeToken := strings.TrimSpace(req.NodeToken)
	if nodeToken == "" {
		token, err := secure.RandomHex(24)
		if err != nil {
			addStep("生成 Token", "failed", err.Error())
			return result, fmt.Errorf("生成节点 Token 失败: %w", err)
		}
		nodeToken = token
		result.NodeToken = token
		addStep("生成 Token", "success", "已自动生成节点鉴权 Token")
	}

	// Step 5: 先创建节点记录（获取 ID 用于容器环境变量）
	nodeName := req.NodeName
	if nodeName == "" {
		nodeName = "raypilot-node-" + req.SSHHost
	}
	hash := sha256.Sum256([]byte(nodeToken))
	node := &model.Node{
		Name:           nodeName,
		Protocol:       "vless",
		TrafficPool:    model.NormalizeTrafficPool(req.TrafficPool),
		OutboundType:   normalizeDeployOutboundType(req.OutboundType),
		UDPEnabled:     normalizeDeployUDPEnabled(req.OutboundType, req.UDPEnabled),
		Host:           req.SSHHost,
		ServerName:     "www.microsoft.com",
		LineMode:       "direct_and_relay",
		AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
		AgentTokenHash: hex.EncodeToString(hash[:]),
		IsEnabled:      true,
	}
	if ip := normalizeOptionalIPv4(req.OutboundIP); ip != "" {
		node.OutboundIP = ip
	} else if node.OutboundType == model.NodeOutboundDirect {
		node.OutboundIP = deployListenIP(false, req.SSHHost)
	}
	if len(proxyURLs) > 0 && strings.TrimSpace(proxyURLs[0]) != "" {
		trimmed := strings.TrimSpace(proxyURLs[0])
		node.OutboundProxyURL = &trimmed
	} else if trimmed := strings.TrimSpace(req.OutboundProxyURL); trimmed != "" {
		node.OutboundProxyURL = &trimmed
	}
	applyDeployTransportOption(node, options[0])
	node, err = s.nodeRepo.Create(ctx, node)
	if err != nil {
		addStep("创建记录", "failed", err.Error())
		return result, fmt.Errorf("创建节点记录失败: %w", err)
	}
	createdNodeID = node.ID
	addStep("创建记录", "success", fmt.Sprintf("节点已创建 (ID: %d)", node.ID))

	// Step 6: 启动容器（传入节点 ID）
	addStep("启动容器", "running", "启动 node-agent 容器")
	if err := s.startContainer(sshClient, req.CenterURL, req.CenterURLs, node, nodeToken); err != nil {
		addStep("启动容器", "failed", err.Error())
		return fail("容器启动失败: %w", err)
	}
	containerStarted = true
	addStep("启动容器", "success", "容器启动成功")

	// Step 7: 读取节点实际 Reality 参数并写回中心，保证订阅链接可直接使用。
	addStep("同步 Reality 参数", "running", "读取节点 Xray 配置")
	if err := s.syncRealityConfig(ctx, sshClient, node); err != nil {
		addStep("同步 Reality 参数", "failed", err.Error())
		return fail("Reality 参数同步失败: %w", err)
	}
	addStep("同步 Reality 参数", "success", "已写回 SNI、公钥和 Short ID")

	addStep("等待心跳", "running", "等待 node-agent 回连中心服务")
	if err := s.waitExitHeartbeat(ctx, node.ID, time.Now().Add(-5*time.Second), deployHeartbeatWaitTimeout); err != nil {
		addStep("等待心跳", "failed", err.Error())
		return fail("node-agent 心跳失败: %w", err)
	}
	addStep("等待心跳", "success", "node-agent 已回连中心服务")

	addStep("验证端口", "running", fmt.Sprintf("检查节点端口 %d 是否监听", node.Port))
	if err := s.waitPortsOwnedBy(sshClient, []uint32{node.Port}, "xray", deployPortWaitTimeout); err != nil {
		addStep("验证端口", "failed", err.Error())
		return fail("节点端口验证失败: %w", err)
	}
	addStep("验证端口", "success", "Xray 节点端口已监听")

	if err := s.finalizeExitDeploy(ctx, req, []uint64{node.ID}, addStep); err != nil {
		return fail("部署后自动配置失败: %w", err)
	}

	result.NodeID = node.ID
	result.Success = true
	result.Message = "部署成功"

	log.Printf("[deploy] node %s deployed successfully on %s, node_id=%d", nodeName, req.SSHHost, node.ID)
	return result, nil
}

// ScanIPs 扫描服务器可用公网 IPv4。只有管理员明确开启多 IP 模式时才应调用。
func (s *NodeDeployService) ScanIPs(ctx context.Context, req *ScanIPsRequest) (*ScanIPsResult, error) {
	result := &ScanIPsResult{Steps: []Step{}}
	addStep := func(name, status, msg string) {
		log.Printf("[deploy-scan] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}

	if req.SSHPort == 0 {
		req.SSHPort = 22
	}
	addStep("SSH 连接", "running", fmt.Sprintf("连接到 %s@%s:%d", req.SSHUser, req.SSHHost, req.SSHPort))
	sshClient := ssh.New(ssh.Config{Host: req.SSHHost, Port: req.SSHPort, User: req.SSHUser, Password: req.SSHPassword})
	if err := sshClient.Connect(); err != nil {
		addStep("SSH 连接", "failed", err.Error())
		return result, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	addStep("SSH 连接", "success", "连接成功")

	addStep("扫描 IPv4", "running", "读取服务器全局 IPv4 地址")
	ips, err := listServerIPv4s(sshClient)
	if err != nil {
		addStep("扫描 IPv4", "failed", err.Error())
		return result, err
	}
	addStep("扫描 IPv4", "success", fmt.Sprintf("发现 %d 个候选地址", len(ips)))

	result.IPs = make([]ScannedIP, 0, len(ips))
	for _, candidate := range ips {
		item := ScannedIP{
			IP:        candidate.IP,
			Interface: candidate.Interface,
			IsPublic:  isPublicIPv4(candidate.IP),
		}
		if !item.IsPublic {
			item.Status = "skipped"
			item.Message = "非公网 IPv4，已跳过"
			item.SkipReason = "private_or_reserved"
			result.IPs = append(result.IPs, item)
			continue
		}
		outbound, verifyErr := verifyOutboundIP(sshClient, candidate.IP)
		if verifyErr != nil {
			item.Status = "failed"
			item.Message = verifyErr.Error()
			result.IPs = append(result.IPs, item)
			continue
		}
		item.OutboundIP = outbound
		if outbound != candidate.IP {
			item.Status = "failed"
			item.Message = fmt.Sprintf("按该 IP 出站实际返回 %s", outbound)
			result.IPs = append(result.IPs, item)
			continue
		}
		item.Status = "success"
		item.Message = "出口验证成功"
		item.IsUsable = true
		result.IPs = append(result.IPs, item)
	}

	result.Success = true
	result.Message = "扫描完成"
	return result, nil
}

func (s *NodeDeployService) deployMultiLine(ctx context.Context, req *DeployRequest, options []deployTransportOption) (*DeployResult, error) {
	result := &DeployResult{Steps: []Step{}}
	if s.nodeHostRepo == nil {
		return result, fmt.Errorf("node host repository is not configured")
	}
	selectedIPs := []string{strings.TrimSpace(req.SSHHost)}
	proxyURLs := normalizeDeployOutboundProxyURLs(req.OutboundType, req.OutboundProxyURL)
	if len(proxyURLs) == 0 {
		proxyURLs = []string{""}
	}
	if req.MultiIPEnabled {
		var err error
		selectedIPs, err = normalizeSelectedIPs(req.SelectedIPs)
		if err != nil {
			return result, err
		}
		if len(selectedIPs) == 0 {
			return result, fmt.Errorf("multi_ip_enabled=true requires selected_ips")
		}
	}

	var sshClient *ssh.Client
	var createdHostID uint64
	var createdNodeIDs []uint64
	containerStarted := false
	addStep := func(name, status, msg string) {
		log.Printf("[deploy-multi] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}
	fail := func(format string, args ...interface{}) (*DeployResult, error) {
		err := fmt.Errorf(format, args...)
		if containerStarted && sshClient != nil {
			s.collectContainerDiagnostics(sshClient, nodeAgentContainerName, addStep)
			if cleanupErr := s.removeDeployContainers(sshClient, nodeAgentContainerName, "suiyue-node-agent"); cleanupErr != nil {
				addStep("清理容器", "failed", cleanupErr.Error())
			} else {
				addStep("清理容器", "success", "已移除本次失败部署的 multi_exit node-agent 容器")
			}
		}
		for _, id := range createdNodeIDs {
			if delErr := s.nodeRepo.Delete(ctx, id); delErr != nil {
				addStep("清理节点", "failed", fmt.Sprintf("节点 %d: %v", id, delErr))
			}
		}
		if createdHostID > 0 {
			if delErr := s.nodeHostRepo.Delete(ctx, createdHostID); delErr != nil {
				addStep("清理主机", "failed", delErr.Error())
			} else {
				addStep("清理主机", "success", fmt.Sprintf("已删除未完成部署的物理主机记录 %d", createdHostID))
			}
		}
		return result, err
	}

	imagePath, imageSize, err := locateNodeAgentImage()
	if err != nil {
		addStep("镜像预检", "failed", err.Error())
		return result, err
	}
	addStep("镜像预检", "success", fmt.Sprintf("本地镜像包可用: %s (%s)", imagePath, formatBytes(imageSize)))

	addStep("SSH 连接", "running", fmt.Sprintf("连接到 %s@%s:%d", req.SSHUser, req.SSHHost, req.SSHPort))
	sshClient = ssh.New(ssh.Config{Host: req.SSHHost, Port: req.SSHPort, User: req.SSHUser, Password: req.SSHPassword})
	if err := sshClient.Connect(); err != nil {
		addStep("SSH 连接", "failed", err.Error())
		return result, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	addStep("SSH 连接", "success", "连接成功")

	addStep("服务器预检", "running", "检查系统、磁盘和基础命令")
	if err := s.preflightRemoteServer(sshClient, imageSize); err != nil {
		addStep("服务器预检", "failed", err.Error())
		return result, fmt.Errorf("服务器预检失败: %w", err)
	}
	addStep("服务器预检", "success", "目标服务器满足 Docker 部署要求")

	if req.MultiIPEnabled {
		addStep("校验出口 IP", "running", "确认所选 IP 存在于服务器并可作为公网出口")
		if err := validateSelectedIPsOnServer(sshClient, selectedIPs); err != nil {
			addStep("校验出口 IP", "failed", err.Error())
			return result, err
		}
		addStep("校验出口 IP", "success", fmt.Sprintf("已确认 %d 个出口 IP", len(selectedIPs)))
	}

	addStep("检测 Docker", "running", "检查 Docker 是否已安装")
	dockerInstalled, _ := s.checkDocker(sshClient)
	if !dockerInstalled {
		addStep("安装 Docker", "running", "Docker 未安装，开始安装")
		if err := s.installDocker(sshClient); err != nil {
			addStep("安装 Docker", "failed", err.Error())
			return result, fmt.Errorf("Docker 安装失败: %w", err)
		}
		addStep("安装 Docker", "success", "Docker 安装成功")
	} else {
		addStep("检测 Docker", "success", "Docker 已安装")
	}

	addStep("推送镜像", "running", "准备推送 node-agent 镜像")
	if err := s.pushImage(sshClient, imagePath); err != nil {
		addStep("推送镜像", "failed", err.Error())
		return result, fmt.Errorf("镜像推送失败: %w", err)
	}
	addStep("推送镜像", "success", "镜像推送成功")

	addStep("清理旧节点代理", "running", "清理旧 systemd node-agent、旧容器和冲突的宿主机 Xray")
	if err := s.cleanupLegacyNodeAgent(sshClient); err != nil {
		addStep("清理旧节点代理", "failed", err.Error())
		return result, err
	}
	addStep("清理旧节点代理", "success", "旧节点代理已清理")

	requestedEndpoints := deployListenEndpointsForOptions(selectedIPs, req.MultiIPEnabled, options, len(proxyURLs))
	addStep("检查端口占用", "running", fmt.Sprintf("检查节点端点 %s 是否空闲", formatDeployListenEndpoints(requestedEndpoints)))
	if err := s.ensureEndpointsFree(sshClient, requestedEndpoints); err != nil {
		addStep("检查端口占用", "failed", err.Error())
		return result, err
	}
	addStep("检查端口占用", "success", "节点端口可用")

	hostToken := strings.TrimSpace(req.NodeToken)
	if hostToken == "" {
		token, err := secure.RandomHex(24)
		if err != nil {
			addStep("生成 Token", "failed", err.Error())
			return result, fmt.Errorf("生成物理主机 Token 失败: %w", err)
		}
		hostToken = token
		result.NodeHostToken = token
		addStep("生成 Token", "success", "已自动生成物理主机鉴权 Token")
	}
	hash := sha256.Sum256([]byte(hostToken))
	tokenHash := hex.EncodeToString(hash[:])

	hostName := req.NodeName
	if hostName == "" {
		hostName = "raypilot-host-" + req.SSHHost
	}
	nodeHost, err := s.nodeHostRepo.Create(ctx, &model.NodeHost{
		Name:           hostName,
		SSHHost:        req.SSHHost,
		SSHPort:        uint32(req.SSHPort),
		AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
		AgentTokenHash: tokenHash,
		IsEnabled:      true,
	})
	if err != nil {
		addStep("创建主机记录", "failed", err.Error())
		return result, fmt.Errorf("创建物理主机记录失败: %w", err)
	}
	createdHostID = nodeHost.ID
	addStep("创建主机记录", "success", fmt.Sprintf("物理主机已创建 (ID: %d)", nodeHost.ID))

	nodeConfigs := make([]MultiExitNodeConfig, 0, len(selectedIPs)*len(options))
	for i, ip := range selectedIPs {
		listenIP := deployListenIP(req.MultiIPEnabled, ip)
		seenEndpoints := map[string]struct{}{}
		for proxyIndex, proxyURL := range proxyURLs {
			for j, option := range options {
				optionCopy := option
				optionCopy.Port += uint32(proxyIndex)
				endpoint := listenEndpointKey(listenIP, optionCopy.Port)
				if _, ok := seenEndpoints[endpoint]; ok {
					return fail("创建节点记录失败: duplicate listen endpoint %s", endpoint)
				}
				seenEndpoints[endpoint] = struct{}{}
				nodeName := deployProxyNodeName(req.NodeName, ip, proxyIndex, len(proxyURLs), optionCopy, len(options))
				node := &model.Node{
					Name:           nodeName,
					Protocol:       "vless",
					TrafficPool:    model.NormalizeTrafficPool(req.TrafficPool),
					OutboundType:   normalizeDeployOutboundType(req.OutboundType),
					UDPEnabled:     normalizeDeployUDPEnabled(req.OutboundType, req.UDPEnabled),
					Host:           ip,
					ServerName:     "www.microsoft.com",
					LineMode:       "direct_and_relay",
					NodeHostID:     &nodeHost.ID,
					ListenIP:       listenIP,
					AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
					AgentTokenHash: tokenHash,
					IsEnabled:      true,
					SortWeight:     (i * len(proxyURLs) * len(options)) + (proxyIndex * len(options)) + j,
				}
				if node.OutboundType == model.NodeOutboundDirect {
					node.OutboundIP = listenIP
				} else if ip := normalizeOptionalIPv4(req.OutboundIP); ip != "" {
					node.OutboundIP = ip
				}
				if trimmed := strings.TrimSpace(proxyURL); trimmed != "" {
					node.OutboundProxyURL = &trimmed
				}
				applyDeployTransportOption(node, optionCopy)
				node, err = s.nodeRepo.Create(ctx, node)
				if err != nil {
					addStep("创建节点记录", "failed", err.Error())
					return fail("创建节点记录失败: %w", err)
				}
				createdNodeIDs = append(createdNodeIDs, node.ID)
				node.XrayInboundTag = fmt.Sprintf("node_%d_in", node.ID)
				node.XrayOutboundTag = fmt.Sprintf("node_%d_out", node.ID)
				if err := s.nodeRepo.Update(ctx, node); err != nil {
					addStep("更新节点标签", "failed", err.Error())
					return fail("更新节点标签失败: %w", err)
				}
				nodeConfigs = append(nodeConfigs, MultiExitNodeConfig{
					NodeID:            node.ID,
					IP:                listenIP,
					Port:              node.Port,
					Transport:         node.Transport,
					OutboundType:      node.OutboundType,
					OutboundIP:        node.OutboundIP,
					OutboundProxyURL:  strings.TrimSpace(derefString(node.OutboundProxyURL)),
					XHTTPPath:         node.XHTTPPath,
					XHTTPHost:         node.XHTTPHost,
					XHTTPMode:         node.XHTTPMode,
					InboundTag:        node.XrayInboundTag,
					OutboundTag:       node.XrayOutboundTag,
					XrayUserKeyPrefix: fmt.Sprintf("node_%d__", node.ID),
				})
				addStep("创建节点记录", "success", fmt.Sprintf("节点 %s:%d/%s 已创建 (ID: %d)", ip, node.Port, node.Transport, node.ID))
			}
		}
	}

	addStep("启动容器", "running", "启动 multi_exit node-agent 容器")
	if err := s.startMultiExitContainer(sshClient, req.CenterURL, req.CenterURLs, nodeHost.ID, hostToken, nodeConfigs); err != nil {
		addStep("启动容器", "failed", err.Error())
		return fail("容器启动失败: %w", err)
	}
	containerStarted = true
	addStep("启动容器", "success", "容器启动成功")

	addStep("同步 Reality 参数", "running", "读取 multi_exit Xray 配置")
	if err := s.syncRealityConfigForNodeIDs(ctx, sshClient, createdNodeIDs); err != nil {
		addStep("同步 Reality 参数", "failed", err.Error())
		return fail("Reality 参数同步失败: %w", err)
	}
	addStep("同步 Reality 参数", "success", "已写回所有逻辑节点的 SNI、公钥和 Short ID")

	addStep("等待心跳", "running", "等待 multi_exit node-agent 回连中心服务")
	if err := s.waitNodeHostHeartbeat(ctx, nodeHost.ID, time.Now().Add(-5*time.Second), deployHeartbeatWaitTimeout); err != nil {
		addStep("等待心跳", "failed", err.Error())
		return fail("multi_exit node-agent 心跳失败: %w", err)
	}
	addStep("等待心跳", "success", "multi_exit node-agent 已回连中心服务")

	endpoints := make([]deployListenEndpoint, 0, len(nodeConfigs))
	for _, cfg := range nodeConfigs {
		endpoints = append(endpoints, deployListenEndpoint{IP: cfg.IP, Port: cfg.Port})
	}
	addStep("验证端口", "running", "检查所有逻辑节点端口是否监听")
	if err := s.waitEndpointsOwnedBy(sshClient, endpoints, "xray", deployPortWaitTimeout); err != nil {
		addStep("验证端口", "failed", err.Error())
		return fail("节点端口验证失败: %w", err)
	}
	addStep("验证端口", "success", fmt.Sprintf("%d 个逻辑节点端点已由 Xray 监听", len(uniqueDeployListenEndpoints(endpoints))))

	if err := s.finalizeExitDeploy(ctx, req, createdNodeIDs, addStep); err != nil {
		return fail("部署后自动配置失败: %w", err)
	}

	result.NodeHostID = nodeHost.ID
	result.NodeIDs = createdNodeIDs
	result.Success = true
	if req.MultiIPEnabled {
		result.Message = "多 IP 节点部署成功"
	} else {
		result.Message = "多传输节点部署成功"
	}
	return result, nil
}

func normalizeDeployOutboundType(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), model.NodeOutboundSocks5) {
		return model.NodeOutboundSocks5
	}
	return model.NodeOutboundDirect
}

func normalizeDeployUDPEnabled(outboundType string, udpEnabled *bool) bool {
	if udpEnabled != nil {
		return *udpEnabled
	}
	return normalizeDeployOutboundType(outboundType) != model.NodeOutboundSocks5
}

func normalizeOptionalIPv4(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "auto") {
		return ""
	}
	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return ""
	}
	return ip.To4().String()
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func deployListenIP(multiIPEnabled bool, host string) string {
	if multiIPEnabled {
		return host
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil && ip.To4() != nil {
		return ip.To4().String()
	}
	return "0.0.0.0"
}

func deployLogicalNodeName(base string, ip string, ipIndex int, ipCount int, option deployTransportOption, optionCount int) string {
	transportLabel := strings.ToUpper(option.Transport)
	if strings.TrimSpace(base) == "" {
		name := "raypilot-node-" + ip
		if optionCount > 1 {
			return name + "-" + transportLabel
		}
		return name
	}
	base = strings.TrimSpace(base)
	if ipCount > 1 && optionCount > 1 {
		return fmt.Sprintf("%s-%d-%s", base, ipIndex+1, transportLabel)
	}
	if ipCount > 1 {
		return fmt.Sprintf("%s-%d", base, ipIndex+1)
	}
	if optionCount > 1 && option.Transport != "tcp" {
		return fmt.Sprintf("%s-%s", base, transportLabel)
	}
	return base
}

func (s *NodeDeployService) finalizeExitDeploy(ctx context.Context, req *DeployRequest, nodeIDs []uint64, addStep func(name, status, msg string)) error {
	nodeIDs = uniqueDeployUint64s(nodeIDs)
	if len(nodeIDs) == 0 {
		return nil
	}

	if req.ReplaceExistingRole {
		agentBaseURL := fmt.Sprintf("http://%s:8080", req.SSHHost)
		if s.relayRepo != nil {
			addStep("停用旧中转角色", "running", "停用同服务器旧中转记录，避免订阅下发错误入口")
			relays, err := s.relayRepo.DisableByAgentBaseURL(ctx, agentBaseURL)
			if err != nil {
				addStep("停用旧中转角色", "failed", err.Error())
				return err
			}
			addStep("停用旧中转角色", "success", fmt.Sprintf("已停用 %d 条旧中转记录", len(relays)))
		}
		if s.nodeRepo != nil && s.nodeAccessSvc != nil {
			addStep("停用旧出口角色", "running", "停用同服务器旧出口记录并下发禁用任务")
			oldNodes, groupIDsByNode, err := s.nodeRepo.DisableByAgentBaseURL(ctx, agentBaseURL, nodeIDs)
			if err != nil {
				addStep("停用旧出口角色", "failed", err.Error())
				return err
			}
			for _, oldNode := range oldNodes {
				if err := s.nodeAccessSvc.TriggerForNodeGroups(ctx, oldNode.ID, groupIDsByNode[oldNode.ID], "DISABLE_USER"); err != nil {
					addStep("停用旧出口角色", "failed", err.Error())
					return err
				}
			}
			addStep("停用旧出口角色", "success", fmt.Sprintf("已停用 %d 条旧出口记录", len(oldNodes)))
		}
	}

	if len(req.NodeGroupIDs) == 0 {
		return nil
	}
	if s.nodeGroupRepo == nil {
		return fmt.Errorf("node group repository is not configured")
	}

	groupIDs, err := normalizeDeployUint64IDs(req.NodeGroupIDs)
	if err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		addStep("绑定节点分组", "running", fmt.Sprintf("绑定节点到分组 %d", groupID))
		change, err := s.nodeGroupRepo.AddNodes(ctx, groupID, nodeIDs)
		if err != nil {
			addStep("绑定节点分组", "failed", err.Error())
			return err
		}
		if s.nodeAccessSvc != nil && len(change.AddedNodeIDs) > 0 {
			if err := s.nodeAccessSvc.TriggerForNodeGroupNodes(ctx, groupID, change.AddedNodeIDs, "UPSERT_USER"); err != nil {
				addStep("绑定节点分组", "failed", err.Error())
				return err
			}
		}
		addStep("绑定节点分组", "success", fmt.Sprintf("分组 %d 新增绑定 %d 条节点", groupID, len(change.AddedNodeIDs)))
	}
	return nil
}

func normalizeDeployUint64IDs(values []uint64) ([]uint64, error) {
	seen := map[uint64]struct{}{}
	result := make([]uint64, 0, len(values))
	for _, value := range values {
		if value == 0 {
			return nil, fmt.Errorf("id must be greater than 0")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func uniqueDeployUint64s(values []uint64) []uint64 {
	normalized, err := normalizeDeployUint64IDs(values)
	if err != nil {
		return []uint64{}
	}
	return normalized
}

func uniqueDeployPorts(values []uint32) []uint32 {
	seen := map[uint32]struct{}{}
	result := make([]uint32, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

// RepairCenterURLs 通过 SSH 修复已部署 node-agent 的中心地址。
func (s *NodeDeployService) RepairCenterURLs(ctx context.Context, req *RepairCenterRequest) (*RepairCenterResult, error) {
	result := &RepairCenterResult{Steps: []Step{}}
	addStep := func(name, status, msg string) {
		log.Printf("[repair-center] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}

	centerURLs := normalizeCenterURLList(req.CenterURL, req.CenterURLs)
	if len(centerURLs) == 0 {
		err := fmt.Errorf("至少需要一个中心地址")
		addStep("校验中心地址", "failed", err.Error())
		return result, err
	}
	result.CenterURLs = centerURLs
	centerURL := centerURLs[0]
	centerURLsValue := strings.Join(centerURLs, ",")
	addStep("校验中心地址", "success", fmt.Sprintf("共 %d 个中心入口", len(centerURLs)))

	sshClient := ssh.New(ssh.Config{
		Host:     req.SSHHost,
		Port:     req.SSHPort,
		User:     req.SSHUser,
		Password: req.SSHPassword,
	})
	addStep("SSH 连接", "running", fmt.Sprintf("连接到 %s@%s:%d", req.SSHUser, req.SSHHost, req.SSHPort))
	if err := sshClient.Connect(); err != nil {
		addStep("SSH 连接", "failed", err.Error())
		return result, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	addStep("SSH 连接", "success", "连接成功")

	addStep("修复 Docker agent", "running", "检查并重建 node-agent 容器中心地址")
	dockerOut, dockerErr := repairDockerAgentCenterURLs(sshClient, centerURL, centerURLsValue, repairDockerContainerCandidates(req))
	dockerRepaired := dockerErr == nil
	if dockerErr == nil {
		addStep("修复 Docker agent", "success", strings.TrimSpace(dockerOut))
	} else if isRepairAgentNotFound(dockerOut, dockerErr) {
		addStep("修复 Docker agent", "success", "未找到 Docker node-agent 容器，已跳过")
	} else {
		addStep("修复 Docker agent", "failed", dockerErr.Error())
	}

	addStep("修复 systemd agent", "running", "检查并更新 systemd node-agent 中心地址")
	systemdOut, systemdErr := repairSystemdAgentCenterURLs(sshClient, centerURL, centerURLsValue)
	systemdRepaired := systemdErr == nil
	if systemdErr == nil {
		addStep("修复 systemd agent", "success", strings.TrimSpace(systemdOut))
	} else if isRepairAgentNotFound(systemdOut, systemdErr) {
		addStep("修复 systemd agent", "success", "未找到 systemd node-agent 服务，已跳过")
	} else {
		addStep("修复 systemd agent", "failed", systemdErr.Error())
	}

	if !dockerRepaired && !systemdRepaired {
		err := fmt.Errorf("未找到可修复的 node-agent: docker=%v systemd=%v", dockerErr, systemdErr)
		addStep("修复结果", "failed", err.Error())
		return result, err
	}

	if err := s.waitRepairHeartbeat(ctx, req, addStep); err != nil {
		result.Message = "中心地址已写入，但等待心跳失败: " + err.Error()
		addStep("等待心跳", "failed", err.Error())
		return result, err
	}

	result.Success = true
	result.Message = "中心地址修复成功"
	addStep("修复结果", "success", result.Message)
	return result, nil
}

func repairDockerContainerCandidates(req *RepairCenterRequest) []string {
	if req != nil && req.RelayID > 0 {
		return []string{"raypilot-relay-agent", "suiyue-relay-agent", "raypilot-node-agent", "suiyue-node-agent"}
	}
	return []string{"raypilot-node-agent", "suiyue-node-agent", "raypilot-relay-agent", "suiyue-relay-agent"}
}

func isRepairAgentNotFound(output string, err error) bool {
	if err == nil {
		return false
	}
	text := output + " " + err.Error()
	return strings.Contains(text, "no docker node-agent container found") ||
		strings.Contains(text, "no systemd node-agent service found")
}

func repairDockerAgentCenterURLs(client *ssh.Client, centerURL string, centerURLs string, containerNames []string) (string, error) {
	if len(containerNames) == 0 {
		containerNames = repairDockerContainerCandidates(nil)
	}
	envContent := fmt.Sprintf("CENTER_SERVER_URL=%s\nCENTER_SERVER_URLS=%s\n", centerURL, centerURLs)
	envB64 := base64.StdEncoding.EncodeToString([]byte(envContent))
	cmd := fmt.Sprintf(`set -e
name=""
for n in %s; do
	if docker ps -a --format '{{.Names}}' | grep -Fx "$n" >/dev/null 2>&1; then
		name="$n"
		break
	fi
done
if [ -z "$name" ]; then
	echo "no docker node-agent container found"
	exit 21
fi
image="$(docker inspect "$name" --format '{{.Config.Image}}')"
network_mode="$(docker inspect "$name" --format '{{.HostConfig.NetworkMode}}')"
restart_name="$(docker inspect "$name" --format '{{.HostConfig.RestartPolicy.Name}}')"
was_running="0"
if docker ps --format '{{.Names}}' | grep -Fx "$name" >/dev/null 2>&1; then
	was_running="1"
fi
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT
env_file="$tmp_dir/env.list"
{
	printf '%%s' %s | base64 -d
	docker inspect "$name" --format '{{range .Config.Env}}{{println .}}{{end}}' | grep -Ev '^(CENTER_SERVER_URL|CENTER_SERVER_URLS)=' || true
} > "$env_file"
old_name="${name}-repair-old-$(date +%%s)"
docker rename "$name" "$old_name"
restore_old() {
	if [ "${repair_success:-0}" = "1" ]; then
		docker rm -f "$old_name" >/dev/null 2>&1 || true
		return
	fi
	docker rm -f "$name" >/dev/null 2>&1 || true
	if docker ps -a --format '{{.Names}}' | grep -Fx "$old_name" >/dev/null 2>&1; then
		docker rename "$old_name" "$name" >/dev/null 2>&1 || true
		if [ "$was_running" = "1" ]; then
			docker start "$name" >/dev/null 2>&1 || true
		fi
	fi
}
trap 'rm -rf "$tmp_dir"; restore_old' EXIT
if [ "$was_running" = "1" ]; then
	docker stop "$old_name" >/dev/null
fi
restart_arg=""
if [ -n "$restart_name" ] && [ "$restart_name" != "no" ]; then
	restart_arg="--restart $restart_name"
fi
network_arg=""
if [ "$network_mode" = "host" ]; then
	network_arg="--network host"
fi
if ! docker run -d --name "$name" $network_arg $restart_arg --env-file "$env_file" --volumes-from "$old_name" "$image" >/dev/null; then
	echo "failed to recreate $name; restored old container"
	exit 23
fi
sleep 3
status="$(docker ps --filter name="$name" --format '{{.Status}}')"
if [ -z "$status" ]; then
	echo "container $name not running"
	exit 22
fi
repair_success=1
docker rm -f "$old_name" >/dev/null
echo "recreated $name with CENTER_SERVER_URLS=%s"`, strings.Join(containerNames, " "), envB64, centerURLs)
	return client.Exec(cmd)
}

func repairSystemdAgentCenterURLs(client *ssh.Client, centerURL string, centerURLs string) (string, error) {
	cmd := fmt.Sprintf(`set -e
service=""
for s in node-agent.service raypilot-node-agent.service raypilot-relay-agent.service suiyue-node-agent.service suiyue-relay-agent.service; do
	if systemctl list-unit-files "$s" --no-legend 2>/dev/null | grep -q "$s"; then
		service="$s"
		break
	fi
done
if [ -z "$service" ]; then
	echo "no systemd node-agent service found"
	exit 31
fi
mkdir -p "/etc/systemd/system/$service.d"
cat > "/etc/systemd/system/$service.d/20-center-urls.conf" <<EOF
[Service]
Environment="CENTER_SERVER_URL=%s"
Environment="CENTER_SERVER_URLS=%s"
EOF
systemctl daemon-reload
systemctl restart "$service"
sleep 3
systemctl is-active --quiet "$service"
echo "restarted $service with CENTER_SERVER_URLS=%s"`, centerURL, centerURLs, centerURLs)
	return client.Exec(cmd)
}

func (s *NodeDeployService) waitRepairHeartbeat(ctx context.Context, req *RepairCenterRequest, addStep func(string, string, string)) error {
	if req.WaitTimeoutSeconds == 0 {
		addStep("等待心跳", "success", "已按请求跳过心跳确认")
		return nil
	}
	timeout := time.Duration(req.WaitTimeoutSeconds) * time.Second
	if timeout < 0 {
		timeout = 30 * time.Second
	}
	if req.NodeID == 0 && req.NodeHostID == 0 && req.RelayID == 0 {
		addStep("等待心跳", "success", "未指定节点记录 ID，跳过心跳确认")
		return nil
	}
	deadline := time.Now().Add(timeout)
	baseline := time.Now().Add(-2 * time.Second)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		ok, err := s.repairHeartbeatAfter(ctx, req, baseline)
		if err == nil && ok {
			addStep("等待心跳", "success", "新中心已收到 agent 心跳")
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting agent heartbeat")
}

func (s *NodeDeployService) repairHeartbeatAfter(ctx context.Context, req *RepairCenterRequest, after time.Time) (bool, error) {
	if req.NodeID > 0 {
		if s.nodeRepo == nil {
			return false, nil
		}
		node, err := s.nodeRepo.FindByID(ctx, req.NodeID)
		if err != nil {
			return false, err
		}
		return node.LastHeartbeatAt != nil && node.LastHeartbeatAt.After(after), nil
	}
	if req.NodeHostID > 0 {
		if s.nodeHostRepo == nil {
			return false, nil
		}
		host, err := s.nodeHostRepo.FindByID(ctx, req.NodeHostID)
		if err != nil {
			return false, err
		}
		return host.LastHeartbeatAt != nil && host.LastHeartbeatAt.After(after), nil
	}
	if req.RelayID > 0 {
		if s.relayRepo == nil {
			return false, nil
		}
		relay, err := s.relayRepo.FindByID(ctx, req.RelayID)
		if err != nil {
			return false, err
		}
		return relay.LastHeartbeatAt != nil && relay.LastHeartbeatAt.After(after), nil
	}
	return true, nil
}

func (s *NodeDeployService) checkDocker(client *ssh.Client) (bool, error) {
	out, err := client.Exec("docker --version && docker info >/dev/null 2>&1")
	return err == nil && strings.Contains(out, "Docker"), nil
}

func (s *NodeDeployService) installDocker(client *ssh.Client) error {
	cmd := `set -e
export DEBIAN_FRONTEND=noninteractive
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
	exit 0
fi
if command -v systemctl >/dev/null 2>&1; then
	systemctl unmask docker 2>/dev/null || true
	systemctl start docker 2>/dev/null || true
fi
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
	exit 0
fi
if command -v apt-get >/dev/null 2>&1; then
	apt-get update -y
	apt-get install -y ca-certificates curl gnupg lsb-release
fi
curl -fsSL --retry 5 --retry-delay 2 --connect-timeout 15 https://get.docker.com -o /tmp/get-docker.sh
sh /tmp/get-docker.sh
rm -f /tmp/get-docker.sh
if command -v systemctl >/dev/null 2>&1; then
	systemctl enable docker >/dev/null 2>&1 || true
	systemctl start docker >/dev/null 2>&1 || true
fi
deadline=$((SECONDS+45))
until docker info >/dev/null 2>&1; do
	if [ "$SECONDS" -ge "$deadline" ]; then
		docker --version || true
		systemctl status docker --no-pager 2>/dev/null || true
		exit 1
	fi
	sleep 2
done
docker ps >/dev/null`
	if out, err := client.Exec(cmd); err != nil {
		return fmt.Errorf("install docker: %w, output: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (s *NodeDeployService) checkContainerRunning(client *ssh.Client) (bool, error) {
	out, err := client.Exec("docker ps --format '{{.Names}}\t{{.Status}}' | grep -E '^(raypilot-node-agent|suiyue-node-agent|raypilot-relay-agent|suiyue-relay-agent)[[:space:]]'")
	return err == nil && strings.TrimSpace(out) != "", nil
}

func (s *NodeDeployService) pushImage(client *ssh.Client, imagePath ...string) error {
	path := strings.TrimSpace(firstString(imagePath))
	if path == "" {
		var err error
		path, _, err = locateNodeAgentImage()
		if err != nil {
			return err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read image file: %w", err)
	}

	remotePath := "/tmp/node-agent-image.tar.gz"
	err = client.Upload(remotePath, data)
	if err != nil {
		return fmt.Errorf("upload image: %w", err)
	}

	cmd := fmt.Sprintf("docker load < %s && rm -f %s && docker image inspect raypilot/node-agent:latest >/dev/null", remotePath, remotePath)
	if out, err := client.Exec(cmd); err != nil {
		return fmt.Errorf("load image on remote: %w, output: %s", err, strings.TrimSpace(out))
	}

	return nil
}

func firstString(values []string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func locateNodeAgentImage() (string, int64, error) {
	if path := strings.TrimSpace(os.Getenv("NODE_AGENT_IMAGE_PATH")); path != "" {
		info, err := os.Stat(path)
		if err != nil {
			return "", 0, fmt.Errorf("node-agent Docker image not found at NODE_AGENT_IMAGE_PATH=%s: %w; please run: make node-agent-image", path, err)
		}
		if info.IsDir() {
			return "", 0, fmt.Errorf("node-agent Docker image path is a directory: %s", path)
		}
		if info.Size() <= 0 {
			return "", 0, fmt.Errorf("node-agent Docker image is empty: %s", path)
		}
		return path, info.Size(), nil
	}
	checked := nodeAgentImageCandidates()
	for _, candidate := range checked {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			continue
		}
		if info.Size() <= 0 {
			return "", 0, fmt.Errorf("node-agent Docker image is empty: %s", candidate)
		}
		return candidate, info.Size(), nil
	}
	return "", 0, fmt.Errorf("node-agent Docker image not found, checked %s; please run: make node-agent-image", strings.Join(checked, ", "))
}

func nodeAgentImageCandidates() []string {
	candidates := []string{}
	if path := strings.TrimSpace(os.Getenv("NODE_AGENT_IMAGE_PATH")); path != "" {
		candidates = append(candidates, path)
	}
	candidates = append(candidates,
		"/root/raypilot-artifacts/node-agent-image.tar.gz",
		"/root/raypilot-artifacts/node-agent-image.tar",
		"/root/node-agent-image.tar.gz",
		"/root/node-agent-image.tar",
		"/app/deploy/artifacts/node-agent-image.tar.gz",
		"/app/deploy/artifacts/node-agent-image.tar",
	)
	return uniqueStrings(candidates)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	const unit = 1024
	value := float64(bytes)
	for _, suffix := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.1f TB", value/unit)
}

func (s *NodeDeployService) preflightRemoteServer(client *ssh.Client, imageSize int64) error {
	requiredKB := (imageSize / 1024) * 3
	if requiredKB < 512*1024 {
		requiredKB = 512 * 1024
	}
	cmd := fmt.Sprintf(`set -e
printf 'os='
(cat /etc/os-release 2>/dev/null | grep '^PRETTY_NAME=' | cut -d= -f2- | tr -d '"' || uname -a)
printf 'arch='
uname -m
command -v sh >/dev/null
command -v curl >/dev/null || command -v wget >/dev/null || command -v apt-get >/dev/null || command -v yum >/dev/null || command -v dnf >/dev/null
free_kb="$(df -Pk /tmp | awk 'NR==2 {print $4}')"
if [ -z "$free_kb" ] || [ "$free_kb" -lt %d ]; then
	echo "insufficient /tmp disk: ${free_kb:-0} KB available, need at least %d KB" >&2
	exit 1
fi
if command -v lsof >/dev/null 2>&1; then
	lsof -iTCP:8080 -sTCP:LISTEN -Pn 2>/dev/null || true
elif command -v ss >/dev/null 2>&1; then
	ss -lntp 2>/dev/null | grep ':8080 ' || true
fi`, requiredKB, requiredKB)
	if out, err := client.Exec(cmd); err != nil {
		return fmt.Errorf("%w, output: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (s *NodeDeployService) cleanupLegacyNodeAgent(client *ssh.Client) error {
	cmd := `set +e
	if command -v systemctl >/dev/null 2>&1; then
		systemctl stop node-agent 2>/dev/null || true
		systemctl disable node-agent 2>/dev/null || true
		systemctl reset-failed node-agent 2>/dev/null || true
		systemctl stop xray 2>/dev/null || true
		systemctl disable xray 2>/dev/null || true
		systemctl reset-failed xray 2>/dev/null || true
	fi
	if command -v docker >/dev/null 2>&1; then
		docker rm -f raypilot-node-agent suiyue-node-agent raypilot-relay-agent suiyue-relay-agent 2>/dev/null || true
	fi
	pkill -x node-agent 2>/dev/null || true
	pkill -x xray 2>/dev/null || true
	rm -f /etc/systemd/system/node-agent.service
	rm -f /etc/systemd/system/suiyue-node-agent.service
	rm -f /usr/local/bin/node-agent /usr/bin/node-agent /root/node-agent
	rm -f /tmp/node-agent-image.tar.gz /tmp/node-agent-image.tar
	rm -f /usr/local/etc/xray/config.json /usr/local/etc/xray/config.json.bak
	rm -f /etc/raypilot/haproxy/haproxy.cfg /etc/raypilot/haproxy/haproxy.cfg.bak
	if command -v systemctl >/dev/null 2>&1; then
		systemctl daemon-reload 2>/dev/null || true
	fi
	exit 0`
	_, err := client.Exec(cmd)
	if err != nil {
		return fmt.Errorf("cleanup legacy node agent: %w", err)
	}
	return nil
}

func (s *NodeDeployService) removeDeployContainers(client *ssh.Client, names ...string) error {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" {
			continue
		}
		quoted = append(quoted, shellQuote(name))
	}
	if len(quoted) == 0 {
		return nil
	}
	if out, err := client.Exec(fmt.Sprintf("docker rm -f %s 2>/dev/null || true", strings.Join(quoted, " "))); err != nil {
		return fmt.Errorf("remove deploy containers: %w, output: %s", err, strings.TrimSpace(out))
	}
	return nil
}

func (s *NodeDeployService) collectContainerDiagnostics(client *ssh.Client, containerName string, addStep func(name, status, msg string)) {
	if addStep == nil {
		return
	}
	containerName = strings.TrimSpace(containerName)
	if containerName == "" {
		return
	}
	cmd := fmt.Sprintf(`set +e
echo "== docker ps =="
docker ps -a --filter name=%s --format 'table {{.Names}}\t{{.Status}}\t{{.Image}}'
echo "== recent logs =="
docker logs --tail 160 %s 2>&1
echo "== listening ports =="
(ss -lntp 2>/dev/null || netstat -lntp 2>/dev/null || true) | head -80`, shellQuote(containerName), shellQuote(containerName))
	out, err := client.Exec(cmd)
	msg := strings.TrimSpace(out)
	if err != nil {
		msg = strings.TrimSpace(fmt.Sprintf("%s\n%v", msg, err))
	}
	if msg == "" {
		msg = "无可用容器诊断输出"
	}
	addStep("容器诊断", "success", truncateForDeployStep(msg, 6000))
}

func truncateForDeployStep(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "...(truncated)"
}

func (s *NodeDeployService) startContainer(client *ssh.Client, centerURL string, centerURLList []string, node *model.Node, nodeToken string) error {
	containerName := nodeAgentContainerName
	centerURLs := centerURLsEnvValue(centerURL, centerURLList)

	// 先停止并删除同名容器和旧品牌容器（如果存在）
	client.Exec(fmt.Sprintf("docker rm -f %s suiyue-node-agent 2>/dev/null", containerName))

	cmd := fmt.Sprintf(`docker run -d --name %s \
		--network host \
		--restart unless-stopped \
		-e CENTER_SERVER_URL=%s \
		-e CENTER_SERVER_URLS=%s \
		-e NODE_ID=%d \
		-e NODE_TOKEN=%s \
		-e NODE_PORT=%d \
		-e NODE_TRANSPORT=%s \
		-e OUTBOUND_TYPE=%s \
		-e OUTBOUND_IP=%s \
		-e OUTBOUND_PROXY_URL=%s \
		-e XHTTP_PATH=%s \
		-e XHTTP_HOST=%s \
		-e XHTTP_MODE=%s \
		-e XRAY_BINARY=/usr/local/bin/xray \
		-e XRAY_CONFIG_PATH=/usr/local/etc/xray/config.json \
		-e XRAY_API_SERVER=127.0.0.1:10085 \
		-v /usr/local/etc/xray:/usr/local/etc/xray:rw \
		raypilot/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), shellQuote(centerURLs), node.ID, shellQuote(nodeToken), node.Port, shellQuote(node.Transport), shellQuote(node.OutboundType), shellQuote(node.OutboundIP), shellQuote(derefString(node.OutboundProxyURL)), shellQuote(node.XHTTPPath), shellQuote(node.XHTTPHost), shellQuote(node.XHTTPMode))

	out, err := client.Exec(cmd)
	if err != nil {
		return fmt.Errorf("start container: %w, output: %s", err, out)
	}

	return s.waitContainerRunning(client, containerName, deployContainerWaitTimeout)
}

func (s *NodeDeployService) startMultiExitContainer(client *ssh.Client, centerURL string, centerURLList []string, nodeHostID uint64, nodeHostToken string, nodes []MultiExitNodeConfig) error {
	containerName := nodeAgentContainerName
	centerURLs := centerURLsEnvValue(centerURL, centerURLList)
	configData, err := json.Marshal(nodes)
	if err != nil {
		return fmt.Errorf("marshal multi node config: %w", err)
	}

	client.Exec(fmt.Sprintf("docker rm -f %s suiyue-node-agent 2>/dev/null", containerName))

	cmd := fmt.Sprintf(`docker run -d --name %s \
		--network host \
		--restart unless-stopped \
		-e AGENT_ROLE=multi_exit \
		-e CENTER_SERVER_URL=%s \
		-e CENTER_SERVER_URLS=%s \
		-e NODE_HOST_ID=%d \
		-e NODE_HOST_TOKEN=%s \
		-e MULTI_NODE_CONFIG=%s \
		-e XRAY_BINARY=/usr/local/bin/xray \
		-e XRAY_CONFIG_PATH=/usr/local/etc/xray/config.json \
		-e XRAY_API_SERVER=127.0.0.1:10085 \
		-v /usr/local/etc/xray:/usr/local/etc/xray:rw \
		raypilot/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), shellQuote(centerURLs), nodeHostID, shellQuote(nodeHostToken), shellQuote(string(configData)))

	out, err := client.Exec(cmd)
	if err != nil {
		return fmt.Errorf("start multi_exit container: %w, output: %s", err, out)
	}

	return s.waitContainerRunning(client, containerName, deployContainerWaitTimeout)
}

func (s *NodeDeployService) waitContainerRunning(client *ssh.Client, containerName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastStatus string
	for time.Now().Before(deadline) {
		out, err := client.Exec(fmt.Sprintf("docker inspect -f '{{.State.Running}} {{.State.Status}} {{.State.ExitCode}}' %s 2>/dev/null", shellQuote(containerName)))
		lastStatus = strings.TrimSpace(out)
		if err == nil && strings.HasPrefix(lastStatus, "true ") {
			return nil
		}
		if err == nil && strings.HasPrefix(lastStatus, "false exited") {
			logs := s.containerLogs(client, containerName, 120)
			return fmt.Errorf("container exited during startup: %s\n%s", lastStatus, logs)
		}
		time.Sleep(2 * time.Second)
	}
	logs := s.containerLogs(client, containerName, 120)
	if lastStatus == "" {
		lastStatus = "not found"
	}
	return fmt.Errorf("container not running after %s: %s\n%s", timeout, lastStatus, logs)
}

func (s *NodeDeployService) containerLogs(client *ssh.Client, containerName string, lines int) string {
	if lines <= 0 {
		lines = 120
	}
	out, err := client.Exec(fmt.Sprintf("docker logs --tail %d %s 2>&1", lines, shellQuote(containerName)))
	if err != nil {
		return strings.TrimSpace(fmt.Sprintf("docker logs unavailable: %v\n%s", err, out))
	}
	return strings.TrimSpace(out)
}

type realityConfig struct {
	ServerName string
	PublicKey  string
	PrivateKey string
	ShortID    string
}

func (s *NodeDeployService) syncRealityConfig(ctx context.Context, client *ssh.Client, node *model.Node) error {
	raw, err := s.waitXrayConfig(client, deployRealityWaitTimeout)
	if err != nil {
		return fmt.Errorf("read xray config: %w", err)
	}

	reality, err := extractRealityConfig(raw)
	if err != nil {
		return err
	}
	if reality.PublicKey == "" && reality.PrivateKey != "" {
		reality.PublicKey, err = deriveRealityPublicKey(client, reality.PrivateKey)
		if err != nil {
			return err
		}
	}
	if reality.ServerName == "" {
		return fmt.Errorf("reality serverName is empty")
	}
	if reality.PublicKey == "" {
		return fmt.Errorf("reality publicKey is empty")
	}

	node.ServerName = reality.ServerName
	node.PublicKey = reality.PublicKey
	node.ShortID = reality.ShortID
	if node.Fingerprint == "" {
		node.Fingerprint = "chrome"
	}
	if node.Flow == "" {
		node.Flow = "xtls-rprx-vision"
	}
	if node.Transport == "xhttp" {
		node.Flow = ""
	}
	return s.nodeRepo.Update(ctx, node)
}

func (s *NodeDeployService) syncRealityConfigForNodeIDs(ctx context.Context, client *ssh.Client, nodeIDs []uint64) error {
	raw, err := s.waitXrayConfig(client, deployRealityWaitTimeout)
	if err != nil {
		return fmt.Errorf("read xray config: %w", err)
	}
	reality, err := extractRealityConfig(raw)
	if err != nil {
		return err
	}
	if reality.PublicKey == "" && reality.PrivateKey != "" {
		reality.PublicKey, err = deriveRealityPublicKey(client, reality.PrivateKey)
		if err != nil {
			return err
		}
	}
	if reality.ServerName == "" {
		return fmt.Errorf("reality serverName is empty")
	}
	if reality.PublicKey == "" {
		return fmt.Errorf("reality publicKey is empty")
	}
	for _, id := range nodeIDs {
		node, err := s.nodeRepo.FindByID(ctx, id)
		if err != nil {
			return err
		}
		node.ServerName = reality.ServerName
		node.PublicKey = reality.PublicKey
		node.ShortID = reality.ShortID
		if node.Fingerprint == "" {
			node.Fingerprint = "chrome"
		}
		if node.Flow == "" {
			node.Flow = "xtls-rprx-vision"
		}
		if node.Transport == "xhttp" {
			node.Flow = ""
		}
		if err := s.nodeRepo.Update(ctx, node); err != nil {
			return err
		}
	}
	return nil
}

func (s *NodeDeployService) waitXrayConfig(client *ssh.Client, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	var lastRaw string
	for time.Now().Before(deadline) {
		raw, err := client.Exec("test -s /usr/local/etc/xray/config.json && cat /usr/local/etc/xray/config.json")
		if err == nil {
			lastRaw = raw
			if _, extractErr := extractRealityConfig(raw); extractErr == nil {
				return raw, nil
			} else {
				lastErr = extractErr
			}
		} else {
			lastErr = err
		}
		time.Sleep(2 * time.Second)
	}
	logs := s.containerLogs(client, nodeAgentContainerName, 160)
	if lastErr != nil {
		return "", fmt.Errorf("xray config not ready after %s: %w\n%s", timeout, lastErr, logs)
	}
	return "", fmt.Errorf("xray config not ready after %s, last content length=%d\n%s", timeout, len(lastRaw), logs)
}

func (s *NodeDeployService) waitExitHeartbeat(ctx context.Context, nodeID uint64, startedAt time.Time, timeout time.Duration) error {
	if s.nodeRepo == nil || nodeID == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		node, err := s.nodeRepo.FindByID(ctx, nodeID)
		if err == nil && node.LastHeartbeatAt != nil && node.LastHeartbeatAt.After(startedAt) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting node %d heartbeat", nodeID)
}

func (s *NodeDeployService) waitNodeHostHeartbeat(ctx context.Context, nodeHostID uint64, startedAt time.Time, timeout time.Duration) error {
	if s.nodeHostRepo == nil || nodeHostID == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return err
		}
		host, err := s.nodeHostRepo.FindByID(ctx, nodeHostID)
		if err == nil && host.LastHeartbeatAt != nil && host.LastHeartbeatAt.After(startedAt) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting node_host %d heartbeat", nodeHostID)
}

func (s *NodeDeployService) waitNodePorts(client *ssh.Client, ports []uint32, timeout time.Duration) error {
	ports = uniqueDeployPorts(ports)
	if len(ports) == 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	var missing []uint32
	for time.Now().Before(deadline) {
		missing = missingListeningPorts(client, ports)
		if len(missing) == 0 {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	out, _ := client.Exec("ss -lntp 2>/dev/null || netstat -lntp 2>/dev/null || true")
	return fmt.Errorf("ports not listening: %v; current listeners: %s", missing, truncateForDeployStep(out, 3000))
}

func missingListeningPorts(client *ssh.Client, ports []uint32) []uint32 {
	missing := make([]uint32, 0, len(ports))
	for _, port := range ports {
		cmd := fmt.Sprintf(`if command -v ss >/dev/null 2>&1; then
	ss -lnt 2>/dev/null | awk '{print $4}' | grep -Eq '(^|:|\])%d$'
else
	netstat -lnt 2>/dev/null | awk '{print $4}' | grep -Eq '(^|:|\])%d$'
fi`, port, port)
		if _, err := client.Exec(cmd); err != nil {
			missing = append(missing, port)
		}
	}
	return missing
}

func deployPortsForOptions(options []deployTransportOption, proxyCount int) []uint32 {
	if proxyCount <= 0 {
		proxyCount = 1
	}
	ports := make([]uint32, 0, len(options)*proxyCount)
	for proxyIndex := 0; proxyIndex < proxyCount; proxyIndex++ {
		for _, option := range options {
			if option.Port == 0 {
				continue
			}
			ports = append(ports, option.Port+uint32(proxyIndex))
		}
	}
	return uniqueDeployPorts(ports)
}

func deployListenEndpointsForOptions(selectedIPs []string, multiIPEnabled bool, options []deployTransportOption, proxyCount int) []deployListenEndpoint {
	if proxyCount <= 0 {
		proxyCount = 1
	}
	endpoints := make([]deployListenEndpoint, 0, len(selectedIPs)*len(options)*proxyCount)
	for _, ip := range selectedIPs {
		listenIP := deployListenIP(multiIPEnabled, ip)
		for proxyIndex := 0; proxyIndex < proxyCount; proxyIndex++ {
			for _, option := range options {
				if option.Port == 0 {
					continue
				}
				endpoints = append(endpoints, deployListenEndpoint{IP: listenIP, Port: option.Port + uint32(proxyIndex)})
			}
		}
	}
	return uniqueDeployListenEndpoints(endpoints)
}

func deployListenEndpointsForPorts(ports []uint32) []deployListenEndpoint {
	endpoints := make([]deployListenEndpoint, 0, len(ports))
	for _, port := range ports {
		endpoints = append(endpoints, deployListenEndpoint{IP: "0.0.0.0", Port: port})
	}
	return uniqueDeployListenEndpoints(endpoints)
}

func uniqueDeployListenEndpoints(values []deployListenEndpoint) []deployListenEndpoint {
	seen := map[string]struct{}{}
	result := make([]deployListenEndpoint, 0, len(values))
	for _, value := range values {
		if value.Port == 0 {
			continue
		}
		ip := strings.TrimSpace(value.IP)
		if ip == "" {
			ip = "0.0.0.0"
		}
		endpoint := deployListenEndpoint{IP: ip, Port: value.Port}
		key := listenEndpointKey(endpoint.IP, endpoint.Port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, endpoint)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Port == result[j].Port {
			return result[i].IP < result[j].IP
		}
		return result[i].Port < result[j].Port
	})
	return result
}

func formatDeployListenEndpoints(endpoints []deployListenEndpoint) string {
	endpoints = uniqueDeployListenEndpoints(endpoints)
	parts := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		parts = append(parts, listenEndpointKey(endpoint.IP, endpoint.Port))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func (s *NodeDeployService) ensureEndpointsFree(client *ssh.Client, endpoints []deployListenEndpoint) error {
	endpoints = uniqueDeployListenEndpoints(endpoints)
	if len(endpoints) == 0 {
		return nil
	}
	busy := make([]string, 0)
	for _, endpoint := range endpoints {
		listeners, err := s.endpointListeners(client, endpoint)
		if err != nil {
			return err
		}
		if strings.TrimSpace(listeners) != "" {
			busy = append(busy, fmt.Sprintf("%s: %s", listenEndpointKey(endpoint.IP, endpoint.Port), truncateForDeployStep(listeners, 600)))
		}
	}
	if len(busy) > 0 {
		return fmt.Errorf("listen endpoints already in use: %s", strings.Join(busy, "; "))
	}
	return nil
}

func (s *NodeDeployService) waitPortsOwnedBy(client *ssh.Client, ports []uint32, processName string, timeout time.Duration) error {
	return s.waitEndpointsOwnedBy(client, deployListenEndpointsForPorts(ports), processName, timeout)
}

func (s *NodeDeployService) waitEndpointsOwnedBy(client *ssh.Client, endpoints []deployListenEndpoint, processName string, timeout time.Duration) error {
	endpoints = uniqueDeployListenEndpoints(endpoints)
	if len(endpoints) == 0 {
		return nil
	}
	processName = strings.ToLower(strings.TrimSpace(processName))
	deadline := time.Now().Add(timeout)
	var lastDetail string
	for time.Now().Before(deadline) {
		missing := make([]string, 0)
		wrongOwner := make([]string, 0)
		for _, endpoint := range endpoints {
			endpointKey := listenEndpointKey(endpoint.IP, endpoint.Port)
			listeners, err := s.endpointListeners(client, endpoint)
			if err != nil {
				lastDetail = err.Error()
				missing = append(missing, endpointKey)
				continue
			}
			trimmed := strings.TrimSpace(listeners)
			if trimmed == "" {
				missing = append(missing, endpointKey)
				continue
			}
			if processName != "" && !strings.Contains(strings.ToLower(trimmed), processName) {
				wrongOwner = append(wrongOwner, fmt.Sprintf("%s: %s", endpointKey, truncateForDeployStep(trimmed, 600)))
				continue
			}
		}
		if len(missing) == 0 && len(wrongOwner) == 0 {
			return nil
		}
		if len(missing) > 0 || len(wrongOwner) > 0 {
			lastDetail = fmt.Sprintf("missing=%v wrong_owner=%v", missing, wrongOwner)
		}
		time.Sleep(2 * time.Second)
	}
	out, _ := client.Exec("ss -lntp 2>/dev/null || netstat -lntp 2>/dev/null || true")
	return fmt.Errorf("listen endpoints not owned by %s after %s: %s; current listeners: %s", processName, timeout, lastDetail, truncateForDeployStep(out, 3000))
}

func (s *NodeDeployService) portListeners(client *ssh.Client, port uint32) (string, error) {
	return s.endpointListeners(client, deployListenEndpoint{IP: "0.0.0.0", Port: port})
}

func (s *NodeDeployService) endpointListeners(client *ssh.Client, endpoint deployListenEndpoint) (string, error) {
	ip := strings.TrimSpace(endpoint.IP)
	if ip == "" {
		ip = "0.0.0.0"
	}
	port := endpoint.Port
	if port == 0 || port > 65535 {
		return "", fmt.Errorf("invalid port: %d", port)
	}
	endpointPattern := deployEndpointAwkPattern(ip, port)
	cmd := fmt.Sprintf(`if command -v ss >/dev/null 2>&1; then
	ss -lntp 2>/dev/null | awk 'NR>1 && $4 ~ /%s/ {print}'
else
	netstat -lntp 2>/dev/null | awk 'NR>2 && $4 ~ /%s/ {print}'
fi`, endpointPattern, endpointPattern)
	out, err := client.Exec(cmd)
	if err != nil {
		return "", fmt.Errorf("check listen endpoint %s listeners: %w", listenEndpointKey(ip, port), err)
	}
	return strings.TrimSpace(out), nil
}

func deployEndpointAwkPattern(ip string, port uint32) string {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "0.0.0.0" || ip == "::" || ip == "*" {
		return fmt.Sprintf("(^|:|\\\\])%d$", port)
	}
	return regexp.QuoteMeta(ip) + fmt.Sprintf(":%d$", port)
}

func extractRealityConfig(raw string) (*realityConfig, error) {
	var cfg struct {
		Inbounds []struct {
			Protocol       string `json:"protocol"`
			StreamSettings struct {
				Security        string `json:"security"`
				RealitySettings struct {
					ServerNames []string `json:"serverNames"`
					PublicKey   string   `json:"publicKey"`
					PrivateKey  string   `json:"privateKey"`
					ShortIDs    []string `json:"shortIds"`
				} `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse xray config: %w", err)
	}

	for _, inbound := range cfg.Inbounds {
		if inbound.Protocol != "vless" || inbound.StreamSettings.Security != "reality" {
			continue
		}
		rc := &realityConfig{
			PublicKey:  strings.TrimSpace(inbound.StreamSettings.RealitySettings.PublicKey),
			PrivateKey: strings.TrimSpace(inbound.StreamSettings.RealitySettings.PrivateKey),
		}
		if len(inbound.StreamSettings.RealitySettings.ServerNames) > 0 {
			rc.ServerName = strings.TrimSpace(inbound.StreamSettings.RealitySettings.ServerNames[0])
		}
		if len(inbound.StreamSettings.RealitySettings.ShortIDs) > 0 {
			rc.ShortID = strings.TrimSpace(inbound.StreamSettings.RealitySettings.ShortIDs[0])
		}
		return rc, nil
	}

	return nil, fmt.Errorf("vless reality inbound not found")
}

func listServerIPv4s(client *ssh.Client) ([]serverIP, error) {
	out, err := client.Exec("ip -o -4 addr show scope global | awk '{print $2, $4}'")
	if err != nil {
		return nil, fmt.Errorf("list server ipv4: %w", err)
	}
	seen := map[string]serverIP{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip, _, err := net.ParseCIDR(fields[1])
		if err != nil || ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		ipText := ip4.String()
		if _, ok := seen[ipText]; !ok {
			seen[ipText] = serverIP{IP: ipText, Interface: fields[0]}
		}
	}
	ips := make([]serverIP, 0, len(seen))
	for _, item := range seen {
		ips = append(ips, item)
	}
	sort.Slice(ips, func(i, j int) bool {
		return ips[i].IP < ips[j].IP
	})
	return ips, nil
}

func normalizeSelectedIPs(values []string) ([]string, error) {
	seen := map[string]struct{}{}
	ips := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		parsed := net.ParseIP(value)
		if parsed == nil || parsed.To4() == nil {
			return nil, fmt.Errorf("invalid selected ip: %s", value)
		}
		ip := parsed.To4().String()
		if !isPublicIPv4(ip) {
			return nil, fmt.Errorf("selected ip is not public ipv4: %s", ip)
		}
		if _, exists := seen[ip]; exists {
			continue
		}
		seen[ip] = struct{}{}
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return ips, nil
}

func validateSelectedIPsOnServer(client *ssh.Client, selected []string) error {
	serverIPs, err := listServerIPv4s(client)
	if err != nil {
		return err
	}
	serverSet := make(map[string]struct{}, len(serverIPs))
	for _, item := range serverIPs {
		serverSet[item.IP] = struct{}{}
	}
	for _, ip := range selected {
		if _, ok := serverSet[ip]; !ok {
			return fmt.Errorf("selected ip %s not found on server", ip)
		}
		outbound, err := verifyOutboundIP(client, ip)
		if err != nil {
			return fmt.Errorf("verify selected ip %s: %w", ip, err)
		}
		if outbound != ip {
			return fmt.Errorf("selected ip %s outbound verification returned %s", ip, outbound)
		}
	}
	return nil
}

func verifyOutboundIP(client *ssh.Client, ip string) (string, error) {
	cmd := fmt.Sprintf("curl -4 -sS --max-time 8 --interface %s https://api.ipify.org || curl -4 -sS --max-time 8 --interface %s https://ifconfig.me/ip", shellQuote(ip), shellQuote(ip))
	out, err := client.Exec(cmd)
	if err != nil {
		return "", fmt.Errorf("outbound probe failed: %w", err)
	}
	out = strings.TrimSpace(out)
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", fmt.Errorf("outbound probe returned empty response")
	}
	probed := fields[0]
	if net.ParseIP(probed) == nil {
		return "", fmt.Errorf("outbound probe returned non-ip response: %s", probed)
	}
	return probed, nil
}

func isPublicIPv4(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	if ip4[0] == 10 || ip4[0] == 127 || ip4[0] == 0 {
		return false
	}
	if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
		return false
	}
	if ip4[0] == 192 && ip4[1] == 168 {
		return false
	}
	if ip4[0] == 169 && ip4[1] == 254 {
		return false
	}
	if ip4[0] >= 224 {
		return false
	}
	return true
}

func deriveRealityPublicKey(client *ssh.Client, privateKey string) (string, error) {
	cmd := fmt.Sprintf(
		"docker exec raypilot-node-agent /usr/local/bin/xray x25519 -i %s 2>/dev/null || docker exec suiyue-node-agent /usr/local/bin/xray x25519 -i %s 2>/dev/null || xray x25519 -i %s",
		shellQuote(privateKey),
		shellQuote(privateKey),
		shellQuote(privateKey),
	)
	out, err := client.Exec(cmd)
	if err != nil {
		return "", fmt.Errorf("derive reality publicKey: %w", err)
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Password (PublicKey): ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Password (PublicKey): ")), nil
		}
		if strings.HasPrefix(line, "PublicKey: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "PublicKey: ")), nil
		}
	}
	return "", fmt.Errorf("derive reality publicKey: unexpected xray output")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
