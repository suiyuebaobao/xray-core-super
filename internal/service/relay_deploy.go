package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/secure"
	"suiyue/internal/platform/ssh"
	"suiyue/internal/repository"
)

// RelayDeployService 中转节点一键部署服务。
type RelayDeployService struct {
	relayRepo *repository.RelayRepository
}

// NewRelayDeployService 创建中转节点部署服务。
func NewRelayDeployService(relayRepo *repository.RelayRepository) *RelayDeployService {
	return &RelayDeployService{relayRepo: relayRepo}
}

// RelayDeployRequest 一键部署中转节点请求。
type RelayDeployRequest struct {
	SSHHost       string `json:"ssh_host" binding:"required"`
	SSHPort       int    `json:"ssh_port"`
	SSHUser       string `json:"ssh_user" binding:"required"`
	SSHPassword   string `json:"ssh_password" binding:"required"`
	CenterURL     string `json:"center_url" binding:"required"`
	RelayToken    string `json:"relay_token"`
	RelayName     string `json:"relay_name"`
	ForwarderType string `json:"forwarder_type"`
}

// RelayDeployResult 中转节点部署结果。
type RelayDeployResult struct {
	RelayID    uint64 `json:"relay_id"`
	RelayToken string `json:"relay_token,omitempty"`
	Success    bool   `json:"success"`
	Message    string `json:"message"`
	Steps      []Step `json:"steps"`
}

// Deploy 一键部署中转节点。
func (s *RelayDeployService) Deploy(ctx context.Context, req *RelayDeployRequest) (*RelayDeployResult, error) {
	result := &RelayDeployResult{Steps: []Step{}}
	var sshClient *ssh.Client
	var createdRelayID uint64
	containerName := "raypilot-relay-agent"

	addStep := func(name, status, msg string) {
		log.Printf("[relay-deploy] [%s] %s: %s", name, status, msg)
		result.Steps = append(result.Steps, Step{Name: name, Status: status, Message: msg})
	}
	cleanup := func() {
		if sshClient != nil {
			_, _ = sshClient.Exec(fmt.Sprintf("docker rm -f %s suiyue-relay-agent 2>/dev/null", shellQuote(containerName)))
		}
		if createdRelayID > 0 {
			if delErr := s.relayRepo.Delete(ctx, createdRelayID); delErr != nil {
				addStep("清理记录", "failed", delErr.Error())
			} else {
				addStep("清理记录", "success", fmt.Sprintf("已删除未完成部署的中转记录 %d", createdRelayID))
			}
		}
	}
	fail := func(format string, args ...interface{}) (*RelayDeployResult, error) {
		err := fmt.Errorf(format, args...)
		cleanup()
		return result, err
	}

	forwarderType := strings.TrimSpace(req.ForwarderType)
	if forwarderType == "" {
		forwarderType = "haproxy"
	}
	if forwarderType != "haproxy" {
		return result, fmt.Errorf("unsupported forwarder_type: %s", forwarderType)
	}

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

	nodeDeployHelper := &NodeDeployService{}
	addStep("检测 Docker", "running", "检查 Docker 是否已安装")
	dockerInstalled, _ := nodeDeployHelper.checkDocker(sshClient)
	if !dockerInstalled {
		addStep("安装 Docker", "running", "Docker 未安装，开始安装")
		if err := nodeDeployHelper.installDocker(sshClient); err != nil {
			addStep("安装 Docker", "failed", err.Error())
			return result, fmt.Errorf("Docker 安装失败: %w", err)
		}
		addStep("安装 Docker", "success", "Docker 安装成功")
	} else {
		addStep("检测 Docker", "success", "Docker 已安装")
	}

	addStep("推送镜像", "running", "准备推送 node-agent 镜像")
	if err := nodeDeployHelper.pushImage(sshClient); err != nil {
		addStep("推送镜像", "failed", err.Error())
		return result, fmt.Errorf("镜像推送失败: %w", err)
	}
	addStep("推送镜像", "success", "镜像推送成功")

	relayToken := strings.TrimSpace(req.RelayToken)
	if relayToken == "" {
		token, err := secure.RandomHex(24)
		if err != nil {
			addStep("生成 Token", "failed", err.Error())
			return result, fmt.Errorf("生成中转 Token 失败: %w", err)
		}
		relayToken = token
		result.RelayToken = token
		addStep("生成 Token", "success", "已自动生成中转鉴权 Token")
	}

	relayName := strings.TrimSpace(req.RelayName)
	if relayName == "" {
		relayName = "raypilot-relay-" + req.SSHHost
	}
	hash := sha256.Sum256([]byte(relayToken))
	relay := &model.Relay{
		Name:           relayName,
		Host:           req.SSHHost,
		ForwarderType:  forwarderType,
		AgentBaseURL:   fmt.Sprintf("http://%s:8080", req.SSHHost),
		AgentTokenHash: hex.EncodeToString(hash[:]),
		Status:         "offline",
		IsEnabled:      true,
	}
	relay, err := s.relayRepo.Create(ctx, relay)
	if err != nil {
		addStep("创建记录", "failed", err.Error())
		return result, fmt.Errorf("创建中转记录失败: %w", err)
	}
	createdRelayID = relay.ID
	addStep("创建记录", "success", fmt.Sprintf("中转节点已创建 (ID: %d)", relay.ID))

	addStep("启动容器", "running", "启动 node-agent relay 模式容器")
	if err := startRelayContainer(sshClient, req.CenterURL, relay.ID, relayToken); err != nil {
		addStep("启动容器", "failed", err.Error())
		return fail("容器启动失败: %w", err)
	}
	addStep("启动容器", "success", "容器启动成功")

	addStep("验证 HAProxy", "running", "检查容器内 HAProxy")
	if out, err := sshClient.Exec(fmt.Sprintf("docker exec %s haproxy -v", shellQuote(containerName))); err != nil {
		addStep("验证 HAProxy", "failed", strings.TrimSpace(out))
		return fail("HAProxy 验证失败: %w", err)
	}
	addStep("验证 HAProxy", "success", "HAProxy 可用")

	addStep("等待心跳", "running", "等待中转 agent 回连中心服务")
	if err := s.waitRelayHeartbeat(ctx, relay.ID, time.Now(), 30*time.Second); err != nil {
		addStep("等待心跳", "failed", err.Error())
		return fail("中转 agent 心跳失败: %w", err)
	}
	addStep("等待心跳", "success", "中转 agent 已回连")

	result.RelayID = relay.ID
	result.Success = true
	result.Message = "部署成功"
	log.Printf("[relay-deploy] relay %s deployed successfully on %s, relay_id=%d", relayName, req.SSHHost, relay.ID)
	return result, nil
}

func startRelayContainer(client *ssh.Client, centerURL string, relayID uint64, relayToken string) error {
	containerName := "raypilot-relay-agent"
	_, _ = client.Exec(fmt.Sprintf("docker rm -f %s suiyue-relay-agent 2>/dev/null", shellQuote(containerName)))
	if _, err := client.Exec("mkdir -p /etc/raypilot/haproxy"); err != nil {
		return fmt.Errorf("prepare haproxy config dir: %w", err)
	}

	cmd := fmt.Sprintf(`docker run -d --name %s \
		--network host \
		--restart unless-stopped \
		-e AGENT_ROLE=relay \
		-e CENTER_SERVER_URL=%s \
		-e RELAY_ID=%d \
		-e RELAY_TOKEN=%s \
		-e HAPROXY_CONFIG_PATH=/etc/haproxy/haproxy.cfg \
		-e HAPROXY_PID_PATH=/tmp/haproxy.pid \
		-e HAPROXY_STATS_SOCKET_PATH=/tmp/haproxy.sock \
		-v /etc/raypilot/haproxy:/etc/haproxy:rw \
		raypilot/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), relayID, shellQuote(relayToken))

	out, err := client.Exec(cmd)
	if err != nil {
		return fmt.Errorf("start relay container: %w, output: %s", err, out)
	}

	time.Sleep(3 * time.Second)
	out, err = client.Exec(fmt.Sprintf("docker ps --filter name=%s --format '{{.Status}}'", shellQuote(containerName)))
	if err != nil {
		return fmt.Errorf("relay container not running after start: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return fmt.Errorf("relay container not running after start")
	}
	return nil
}

func (s *RelayDeployService) waitRelayHeartbeat(ctx context.Context, relayID uint64, startedAt time.Time, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		relay, err := s.relayRepo.FindByID(ctx, relayID)
		if err == nil && relay.LastHeartbeatAt != nil && relay.LastHeartbeatAt.After(startedAt.Add(-2*time.Second)) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting relay heartbeat")
}
