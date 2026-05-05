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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
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
	nodeRepo     *repository.NodeRepository
	nodeHostRepo *repository.NodeHostRepository
}

// NewNodeDeployService 创建节点部署服务。
func NewNodeDeployService(nodeRepo *repository.NodeRepository, nodeHostRepo ...*repository.NodeHostRepository) *NodeDeployService {
	var hostRepo *repository.NodeHostRepository
	if len(nodeHostRepo) > 0 {
		hostRepo = nodeHostRepo[0]
	}
	return &NodeDeployService{nodeRepo: nodeRepo, nodeHostRepo: hostRepo}
}

// DeployRequest 一键部署请求。
type DeployRequest struct {
	SSHHost        string   `json:"ssh_host" binding:"required"`
	SSHPort        int      `json:"ssh_port"`
	SSHUser        string   `json:"ssh_user" binding:"required"`
	SSHPassword    string   `json:"ssh_password" binding:"required"`
	CenterURL      string   `json:"center_url" binding:"required"`
	NodeToken      string   `json:"node_token"`
	NodeName       string   `json:"node_name"`
	Transport      string   `json:"transport"`
	XHTTPPath      string   `json:"xhttp_path"`
	XHTTPHost      string   `json:"xhttp_host"`
	XHTTPMode      string   `json:"xhttp_mode"`
	MultiIPEnabled bool     `json:"multi_ip_enabled"`
	SelectedIPs    []string `json:"selected_ips"`
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
	XHTTPPath         string `json:"xhttp_path,omitempty"`
	XHTTPHost         string `json:"xhttp_host,omitempty"`
	XHTTPMode         string `json:"xhttp_mode,omitempty"`
	InboundTag        string `json:"inbound_tag"`
	OutboundTag       string `json:"outbound_tag"`
	XrayUserKeyPrefix string `json:"xray_user_key_prefix"`
}

func normalizeDeployTransport(req *DeployRequest) {
	req.Transport = normalizeTransport(req.Transport)
	req.XHTTPHost = strings.TrimSpace(req.XHTTPHost)
	req.XHTTPMode = normalizeXHTTPMode(req.XHTTPMode)
	if req.Transport == "xhttp" {
		req.XHTTPPath = normalizeXHTTPPath(req.XHTTPPath)
		return
	}
	req.XHTTPPath = ""
	req.XHTTPHost = ""
	req.XHTTPMode = "auto"
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
	normalizeDeployTransport(req)
	if req.MultiIPEnabled {
		return s.deployMultiIP(ctx, req)
	}

	result := &DeployResult{Steps: []Step{}}
	var sshClient *ssh.Client
	var createdNodeID uint64

	addStep := func(name, status, msg string) {
		log.Printf("[deploy] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}
	fail := func(format string, args ...interface{}) (*DeployResult, error) {
		err := fmt.Errorf(format, args...)
		if createdNodeID > 0 {
			if delErr := s.nodeRepo.Delete(ctx, createdNodeID); delErr != nil {
				addStep("清理记录", "failed", delErr.Error())
			} else {
				addStep("清理记录", "success", fmt.Sprintf("已删除未完成部署的节点记录 %d", createdNodeID))
			}
		}
		return result, err
	}

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
	if err := s.pushImage(sshClient); err != nil {
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
		Transport:      req.Transport,
		Host:           req.SSHHost,
		Port:           443,
		ServerName:     "www.microsoft.com",
		Flow:           "xtls-rprx-vision",
		LineMode:       "direct_and_relay",
		XHTTPPath:      req.XHTTPPath,
		XHTTPHost:      req.XHTTPHost,
		XHTTPMode:      req.XHTTPMode,
		AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
		AgentTokenHash: hex.EncodeToString(hash[:]),
		IsEnabled:      true,
	}
	if node.Transport == "xhttp" {
		node.Flow = ""
	}
	node, err := s.nodeRepo.Create(ctx, node)
	if err != nil {
		addStep("创建记录", "failed", err.Error())
		return result, fmt.Errorf("创建节点记录失败: %w", err)
	}
	createdNodeID = node.ID
	addStep("创建记录", "success", fmt.Sprintf("节点已创建 (ID: %d)", node.ID))

	// Step 6: 启动容器（传入节点 ID）
	addStep("启动容器", "running", "启动 node-agent 容器")
	if err := s.startContainer(sshClient, req.CenterURL, node, nodeToken); err != nil {
		addStep("启动容器", "failed", err.Error())
		return fail("容器启动失败: %w", err)
	}
	addStep("启动容器", "success", "容器启动成功")

	// Step 7: 读取节点实际 Reality 参数并写回中心，保证订阅链接可直接使用。
	addStep("同步 Reality 参数", "running", "读取节点 Xray 配置")
	if err := s.syncRealityConfig(ctx, sshClient, node); err != nil {
		addStep("同步 Reality 参数", "failed", err.Error())
		return fail("Reality 参数同步失败: %w", err)
	}
	addStep("同步 Reality 参数", "success", "已写回 SNI、公钥和 Short ID")

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

func (s *NodeDeployService) deployMultiIP(ctx context.Context, req *DeployRequest) (*DeployResult, error) {
	result := &DeployResult{Steps: []Step{}}
	if s.nodeHostRepo == nil {
		return result, fmt.Errorf("node host repository is not configured")
	}
	selectedIPs, err := normalizeSelectedIPs(req.SelectedIPs)
	if err != nil {
		return result, err
	}
	if len(selectedIPs) == 0 {
		return result, fmt.Errorf("multi_ip_enabled=true requires selected_ips")
	}

	var sshClient *ssh.Client
	var createdHostID uint64
	var createdNodeIDs []uint64
	addStep := func(name, status, msg string) {
		log.Printf("[deploy-multi] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}
	fail := func(format string, args ...interface{}) (*DeployResult, error) {
		err := fmt.Errorf(format, args...)
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

	addStep("SSH 连接", "running", fmt.Sprintf("连接到 %s@%s:%d", req.SSHUser, req.SSHHost, req.SSHPort))
	sshClient = ssh.New(ssh.Config{Host: req.SSHHost, Port: req.SSHPort, User: req.SSHUser, Password: req.SSHPassword})
	if err := sshClient.Connect(); err != nil {
		addStep("SSH 连接", "failed", err.Error())
		return result, fmt.Errorf("SSH 连接失败: %w", err)
	}
	defer sshClient.Close()
	addStep("SSH 连接", "success", "连接成功")

	addStep("校验出口 IP", "running", "确认所选 IP 存在于服务器并可作为公网出口")
	if err := validateSelectedIPsOnServer(sshClient, selectedIPs); err != nil {
		addStep("校验出口 IP", "failed", err.Error())
		return result, err
	}
	addStep("校验出口 IP", "success", fmt.Sprintf("已确认 %d 个出口 IP", len(selectedIPs)))

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
	if err := s.pushImage(sshClient); err != nil {
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

	nodeConfigs := make([]MultiExitNodeConfig, 0, len(selectedIPs))
	for i, ip := range selectedIPs {
		nodeName := fmt.Sprintf("raypilot-node-%s", ip)
		if strings.TrimSpace(req.NodeName) != "" {
			nodeName = fmt.Sprintf("%s-%d", req.NodeName, i+1)
		}
		node := &model.Node{
			Name:           nodeName,
			Protocol:       "vless",
			Transport:      req.Transport,
			Host:           ip,
			Port:           443,
			ServerName:     "www.microsoft.com",
			Flow:           "xtls-rprx-vision",
			LineMode:       "direct_and_relay",
			XHTTPPath:      req.XHTTPPath,
			XHTTPHost:      req.XHTTPHost,
			XHTTPMode:      req.XHTTPMode,
			NodeHostID:     &nodeHost.ID,
			ListenIP:       ip,
			OutboundIP:     ip,
			AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
			AgentTokenHash: tokenHash,
			IsEnabled:      true,
			SortWeight:     i,
		}
		if node.Transport == "xhttp" {
			node.Flow = ""
		}
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
			IP:                ip,
			Port:              node.Port,
			Transport:         node.Transport,
			XHTTPPath:         node.XHTTPPath,
			XHTTPHost:         node.XHTTPHost,
			XHTTPMode:         node.XHTTPMode,
			InboundTag:        node.XrayInboundTag,
			OutboundTag:       node.XrayOutboundTag,
			XrayUserKeyPrefix: fmt.Sprintf("node_%d__", node.ID),
		})
		addStep("创建节点记录", "success", fmt.Sprintf("节点 %s 已创建 (ID: %d)", ip, node.ID))
	}

	addStep("启动容器", "running", "启动 multi_exit node-agent 容器")
	if err := s.startMultiExitContainer(sshClient, req.CenterURL, nodeHost.ID, hostToken, nodeConfigs); err != nil {
		addStep("启动容器", "failed", err.Error())
		return fail("容器启动失败: %w", err)
	}
	addStep("启动容器", "success", "容器启动成功")

	addStep("同步 Reality 参数", "running", "读取 multi_exit Xray 配置")
	if err := s.syncRealityConfigForNodeIDs(ctx, sshClient, createdNodeIDs); err != nil {
		addStep("同步 Reality 参数", "failed", err.Error())
		return fail("Reality 参数同步失败: %w", err)
	}
	addStep("同步 Reality 参数", "success", "已写回所有逻辑节点的 SNI、公钥和 Short ID")

	result.NodeHostID = nodeHost.ID
	result.NodeIDs = createdNodeIDs
	result.Success = true
	result.Message = "多 IP 节点部署成功"
	return result, nil
}

func (s *NodeDeployService) checkDocker(client *ssh.Client) (bool, error) {
	out, err := client.Exec("docker --version")
	return err == nil && strings.Contains(out, "Docker"), nil
}

func (s *NodeDeployService) installDocker(client *ssh.Client) error {
	_, err := client.Exec("curl -fsSL https://get.docker.com | bash")
	if err != nil {
		return fmt.Errorf("install docker: %w", err)
	}
	// 等待 Docker 启动
	time.Sleep(3 * time.Second)
	_, err = client.Exec("docker ps")
	if err != nil {
		return fmt.Errorf("docker not running after install: %w", err)
	}
	return nil
}

func (s *NodeDeployService) checkContainerRunning(client *ssh.Client) (bool, error) {
	out, err := client.Exec("docker ps --format '{{.Names}}\t{{.Status}}' | grep -E '^(raypilot-node-agent|suiyue-node-agent)[[:space:]]'")
	return err == nil && strings.TrimSpace(out) != "", nil
}

func (s *NodeDeployService) pushImage(client *ssh.Client) error {
	// 读取本地镜像文件
	imagePath := "/root/node-agent-image.tar.gz"
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		// 尝试非压缩版本
		imagePath = "/root/node-agent-image.tar"
	}
	if _, err := os.Stat(imagePath); os.IsNotExist(err) {
		// 如果没有预置镜像，尝试从 Docker 导出
		return fmt.Errorf("node-agent Docker image not found at %s, please run: make node-agent-image", imagePath)
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		return fmt.Errorf("read image file: %w", err)
	}

	remotePath := "/tmp/node-agent-image.tar.gz"
	err = client.Upload(remotePath, data)
	if err != nil {
		return fmt.Errorf("upload image: %w", err)
	}

	// 在远程加载镜像
	_, err = client.Exec(fmt.Sprintf("docker load < %s && rm -f %s", remotePath, remotePath))
	if err != nil {
		return fmt.Errorf("load image on remote: %w", err)
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

func (s *NodeDeployService) startContainer(client *ssh.Client, centerURL string, node *model.Node, nodeToken string) error {
	containerName := "raypilot-node-agent"

	// 先停止并删除同名容器和旧品牌容器（如果存在）
	client.Exec(fmt.Sprintf("docker rm -f %s suiyue-node-agent 2>/dev/null", containerName))

	cmd := fmt.Sprintf(`docker run -d --name %s \
		--network host \
		--restart unless-stopped \
		-e CENTER_SERVER_URL=%s \
		-e NODE_ID=%d \
		-e NODE_TOKEN=%s \
		-e NODE_TRANSPORT=%s \
		-e XHTTP_PATH=%s \
		-e XHTTP_HOST=%s \
		-e XHTTP_MODE=%s \
		-e XRAY_BINARY=/usr/local/bin/xray \
		-e XRAY_CONFIG_PATH=/usr/local/etc/xray/config.json \
		-e XRAY_API_SERVER=127.0.0.1:10085 \
		-v /usr/local/etc/xray:/usr/local/etc/xray:rw \
		raypilot/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), node.ID, shellQuote(nodeToken), shellQuote(node.Transport), shellQuote(node.XHTTPPath), shellQuote(node.XHTTPHost), shellQuote(node.XHTTPMode))

	out, err := client.Exec(cmd)
	if err != nil {
		return fmt.Errorf("start container: %w, output: %s", err, out)
	}

	// 等待容器启动
	time.Sleep(3 * time.Second)
	out, err = client.Exec(fmt.Sprintf("docker ps --filter name=%s --format '{{.Status}}'", shellQuote(containerName)))
	if err != nil {
		return fmt.Errorf("container not running after start: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("container not running after start")
	}

	return nil
}

func (s *NodeDeployService) startMultiExitContainer(client *ssh.Client, centerURL string, nodeHostID uint64, nodeHostToken string, nodes []MultiExitNodeConfig) error {
	containerName := "raypilot-node-agent"
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
		-e NODE_HOST_ID=%d \
		-e NODE_HOST_TOKEN=%s \
		-e MULTI_NODE_CONFIG=%s \
		-e XRAY_BINARY=/usr/local/bin/xray \
		-e XRAY_CONFIG_PATH=/usr/local/etc/xray/config.json \
		-e XRAY_API_SERVER=127.0.0.1:10085 \
		-v /usr/local/etc/xray:/usr/local/etc/xray:rw \
		raypilot/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), nodeHostID, shellQuote(nodeHostToken), shellQuote(string(configData)))

	out, err := client.Exec(cmd)
	if err != nil {
		return fmt.Errorf("start multi_exit container: %w, output: %s", err, out)
	}

	time.Sleep(3 * time.Second)
	out, err = client.Exec(fmt.Sprintf("docker ps --filter name=%s --format '{{.Status}}'", shellQuote(containerName)))
	if err != nil {
		return fmt.Errorf("container not running after start: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("container not running after start")
	}
	return nil
}

type realityConfig struct {
	ServerName string
	PublicKey  string
	PrivateKey string
	ShortID    string
}

func (s *NodeDeployService) syncRealityConfig(ctx context.Context, client *ssh.Client, node *model.Node) error {
	raw, err := client.Exec("cat /usr/local/etc/xray/config.json")
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
	raw, err := client.Exec("cat /usr/local/etc/xray/config.json")
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
