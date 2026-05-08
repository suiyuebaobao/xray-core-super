package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
	relayRepo     *repository.RelayRepository
	relaySvc      *RelayService
	nodeRepo      *repository.NodeRepository
	nodeAccessSvc *NodeAccessService
}

// NewRelayDeployService 创建中转节点部署服务。
func NewRelayDeployService(relayRepo *repository.RelayRepository) *RelayDeployService {
	return &RelayDeployService{relayRepo: relayRepo}
}

// NewRelayDeployServiceWithAutomation 创建带后端绑定、旧出口停用和 reload 验证的一键中转部署服务。
func NewRelayDeployServiceWithAutomation(relayRepo *repository.RelayRepository, relaySvc *RelayService, nodeRepo *repository.NodeRepository, nodeAccessSvc *NodeAccessService) *RelayDeployService {
	return &RelayDeployService{relayRepo: relayRepo, relaySvc: relaySvc, nodeRepo: nodeRepo, nodeAccessSvc: nodeAccessSvc}
}

// RelayDeployRequest 一键部署中转节点请求。
type RelayDeployRequest struct {
	SSHHost             string   `json:"ssh_host" binding:"required"`
	SSHPort             int      `json:"ssh_port"`
	SSHUser             string   `json:"ssh_user" binding:"required"`
	SSHPassword         string   `json:"ssh_password" binding:"required"`
	CenterURL           string   `json:"center_url" binding:"required"`
	CenterURLs          []string `json:"center_urls"`
	RelayToken          string   `json:"relay_token"`
	RelayName           string   `json:"relay_name"`
	ForwarderType       string   `json:"forwarder_type"`
	ExitNodeID          uint64   `json:"exit_node_id"`
	ListenPort          uint32   `json:"listen_port"`
	TargetPort          uint32   `json:"target_port"`
	BackendName         string   `json:"backend_name"`
	ReplaceExistingRole bool     `json:"replace_existing_role"`
}

// RelayDeployResult 中转节点部署结果。
type RelayDeployResult struct {
	RelayID    uint64   `json:"relay_id"`
	BackendIDs []uint64 `json:"backend_ids,omitempty"`
	TargetHost string   `json:"target_host,omitempty"`
	TargetPort uint32   `json:"target_port,omitempty"`
	RelayToken string   `json:"relay_token,omitempty"`
	Success    bool     `json:"success"`
	Message    string   `json:"message"`
	Steps      []Step   `json:"steps"`
}

// Deploy 一键部署中转节点。
func (s *RelayDeployService) Deploy(ctx context.Context, req *RelayDeployRequest) (*RelayDeployResult, error) {
	result := &RelayDeployResult{Steps: []Step{}}
	var sshClient *ssh.Client
	var createdRelayID uint64
	containerName := "raypilot-relay-agent"
	centerURLs := normalizeCenterURLList(req.CenterURL, req.CenterURLs)
	if len(centerURLs) == 0 {
		return result, fmt.Errorf("center_url must be a valid http or https URL")
	}
	req.CenterURL = centerURLs[0]
	if len(centerURLs) > 1 {
		req.CenterURLs = centerURLs[1:]
	} else {
		req.CenterURLs = nil
	}

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
	if err := startRelayContainer(sshClient, req.CenterURL, req.CenterURLs, relay.ID, relayToken); err != nil {
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

	backendIDs, err := s.finalizeRelayDeploy(ctx, req, relay.ID, addStep)
	if err != nil {
		addStep("部署后自动配置", "failed", err.Error())
		return fail("部署后自动配置失败: %w", err)
	}

	result.RelayID = relay.ID
	result.BackendIDs = backendIDs
	if req.ExitNodeID > 0 && s.nodeRepo != nil {
		if exitNode, findErr := s.nodeRepo.FindByID(ctx, req.ExitNodeID); findErr == nil {
			result.TargetHost = exitNode.Host
			if req.TargetPort > 0 {
				result.TargetPort = req.TargetPort
			} else {
				result.TargetPort = exitNode.Port
			}
		}
	}
	result.Success = true
	result.Message = "部署成功"
	log.Printf("[relay-deploy] relay %s deployed successfully on %s, relay_id=%d", relayName, req.SSHHost, relay.ID)
	return result, nil
}

func startRelayContainer(client *ssh.Client, centerURL string, centerURLList []string, relayID uint64, relayToken string) error {
	containerName := "raypilot-relay-agent"
	centerURLs := centerURLsEnvValue(centerURL, centerURLList)
	_, _ = client.Exec(fmt.Sprintf("docker rm -f %s suiyue-relay-agent 2>/dev/null", shellQuote(containerName)))
	if _, err := client.Exec("mkdir -p /etc/raypilot/haproxy"); err != nil {
		return fmt.Errorf("prepare haproxy config dir: %w", err)
	}

	cmd := fmt.Sprintf(`docker run -d --name %s \
		--network host \
		--restart unless-stopped \
		-e AGENT_ROLE=relay \
		-e CENTER_SERVER_URL=%s \
		-e CENTER_SERVER_URLS=%s \
		-e RELAY_ID=%d \
		-e RELAY_TOKEN=%s \
		-e HAPROXY_CONFIG_PATH=/etc/haproxy/haproxy.cfg \
		-e HAPROXY_PID_PATH=/tmp/haproxy.pid \
		-e HAPROXY_STATS_SOCKET_PATH=/tmp/haproxy.sock \
		-v /etc/raypilot/haproxy:/etc/haproxy:rw \
		raypilot/node-agent:latest`, shellQuote(containerName), shellQuote(centerURL), shellQuote(centerURLs), relayID, shellQuote(relayToken))

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

func (s *RelayDeployService) finalizeRelayDeploy(ctx context.Context, req *RelayDeployRequest, relayID uint64, addStep func(name, status, msg string)) ([]uint64, error) {
	if req.ReplaceExistingRole && s.nodeRepo != nil && s.nodeAccessSvc != nil {
		agentBaseURL := fmt.Sprintf("http://%s:8080", req.SSHHost)
		addStep("停用旧出口角色", "running", "停用同服务器旧出口节点并下发禁用任务")
		oldNodes, groupIDsByNode, err := s.nodeRepo.DisableByAgentBaseURL(ctx, agentBaseURL, nil)
		if err != nil {
			addStep("停用旧出口角色", "failed", err.Error())
			return nil, err
		}
		for _, oldNode := range oldNodes {
			if err := s.nodeAccessSvc.TriggerForNodeGroups(ctx, oldNode.ID, groupIDsByNode[oldNode.ID], "DISABLE_USER"); err != nil {
				addStep("停用旧出口角色", "failed", err.Error())
				return nil, err
			}
		}
		addStep("停用旧出口角色", "success", fmt.Sprintf("已停用 %d 条旧出口记录", len(oldNodes)))
	}

	if req.ExitNodeID == 0 {
		return nil, nil
	}
	if s.relaySvc == nil {
		return nil, fmt.Errorf("relay service is not configured")
	}
	listenPort := req.ListenPort
	if listenPort == 0 {
		listenPort = 24443
	}
	targetPort := req.TargetPort
	if targetPort == 0 && s.nodeRepo != nil {
		exitNode, err := s.nodeRepo.FindByID(ctx, req.ExitNodeID)
		if err != nil {
			return nil, fmt.Errorf("find exit node %d: %w", req.ExitNodeID, err)
		}
		targetPort = exitNode.Port
	}
	if targetPort == 0 {
		targetPort = 443
	}

	addStep("绑定中转后端", "running", fmt.Sprintf("绑定 %d -> 出口节点 %d:%d", listenPort, req.ExitNodeID, targetPort))
	backends, err := s.relaySvc.SaveBackends(ctx, relayID, []model.RelayBackendRequest{{
		ExitNodeID: req.ExitNodeID,
		Name:       strings.TrimSpace(req.BackendName),
		ListenPort: listenPort,
		TargetPort: targetPort,
		IsEnabled:  true,
	}})
	if err != nil {
		addStep("绑定中转后端", "failed", err.Error())
		return nil, err
	}
	backendIDs := make([]uint64, 0, len(backends))
	for _, backend := range backends {
		backendIDs = append(backendIDs, backend.ID)
	}
	addStep("绑定中转后端", "success", fmt.Sprintf("已保存 %d 条后端绑定", len(backends)))

	addStep("等待转发配置", "running", "等待 relay agent 应用 HAProxy 配置")
	if err := s.waitRelayReloadDone(ctx, relayID, time.Now().Add(-5*time.Second), 45*time.Second); err != nil {
		addStep("等待转发配置", "failed", err.Error())
		return backendIDs, err
	}
	addStep("等待转发配置", "success", "HAProxy 配置已应用")
	return backendIDs, nil
}

func (s *RelayDeployService) waitRelayReloadDone(ctx context.Context, relayID uint64, startedAt time.Time, timeout time.Duration) error {
	if s.relaySvc == nil || s.relaySvc.taskRepo == nil {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		done, lastErr, err := s.relaySvc.taskRepo.LatestStatusByRelayAndAction(ctx, relayID, "RELOAD_CONFIG", startedAt)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		if lastErr != "" {
			return errors.New(lastErr)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting relay reload")
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
