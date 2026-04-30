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
	"os"
	"strings"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/secure"
	"suiyue/internal/platform/ssh"
	"suiyue/internal/repository"
)

// NodeDeployService 节点部署服务。
type NodeDeployService struct {
	nodeRepo *repository.NodeRepository
}

// NewNodeDeployService 创建节点部署服务。
func NewNodeDeployService(nodeRepo *repository.NodeRepository) *NodeDeployService {
	return &NodeDeployService{nodeRepo: nodeRepo}
}

// DeployRequest 一键部署请求。
type DeployRequest struct {
	SSHHost     string `json:"ssh_host" binding:"required"`
	SSHPort     int    `json:"ssh_port"`
	SSHUser     string `json:"ssh_user" binding:"required"`
	SSHPassword string `json:"ssh_password" binding:"required"`
	CenterURL   string `json:"center_url" binding:"required"`
	NodeToken   string `json:"node_token"`
	NodeName    string `json:"node_name"`
}

// DeployResult 部署结果。
type DeployResult struct {
	NodeID    uint64 `json:"node_id"`
	NodeToken string `json:"node_token,omitempty"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Steps     []Step `json:"steps"`
}

// Step 部署步骤。
type Step struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // success, failed, running
	Message string `json:"message"`
}

// Deploy 一键部署节点。
func (s *NodeDeployService) Deploy(ctx context.Context, req *DeployRequest) (*DeployResult, error) {
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
		nodeName = "suiyue-node-" + req.SSHHost
	}
	hash := sha256.Sum256([]byte(nodeToken))
	node := &model.Node{
		Name:           nodeName,
		Protocol:       "vless",
		Host:           req.SSHHost,
		Port:           443,
		ServerName:     "www.microsoft.com",
		LineMode:       "direct_and_relay",
		AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
		AgentTokenHash: hex.EncodeToString(hash[:]),
		IsEnabled:      true,
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
	if err := s.startContainer(sshClient, req.CenterURL, node.ID, nodeToken); err != nil {
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
	out, err := client.Exec("docker ps --filter name=suiyue-node-agent --format '{{.Status}}'")
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

func (s *NodeDeployService) startContainer(client *ssh.Client, centerURL string, nodeID uint64, nodeToken string) error {
	containerName := "suiyue-node-agent"

	// 先停止并删除同名容器（如果存在）
	client.Exec(fmt.Sprintf("docker rm -f %s 2>/dev/null", containerName))

	cmd := fmt.Sprintf(`docker run -d --name %s \
		--network host \
		--restart unless-stopped \
		-e CENTER_SERVER_URL=%s \
		-e NODE_ID=%d \
		-e NODE_TOKEN=%s \
		-e XRAY_BINARY=/usr/local/bin/xray \
		-e XRAY_CONFIG_PATH=/usr/local/etc/xray/config.json \
		-v /usr/local/etc/xray:/usr/local/etc/xray:rw \
		suiyue/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), nodeID, shellQuote(nodeToken))

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
	return s.nodeRepo.Update(ctx, node)
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

func deriveRealityPublicKey(client *ssh.Client, privateKey string) (string, error) {
	cmd := fmt.Sprintf(
		"docker exec suiyue-node-agent /usr/local/bin/xray x25519 -i %s 2>/dev/null || xray x25519 -i %s",
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
