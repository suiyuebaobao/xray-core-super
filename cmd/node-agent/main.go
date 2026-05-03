// cmd/node-agent/main.go — 真实 node-agent 服务。
//
// 功能：
// - 管理本机 xray-core 的用户配置（增删改）
// - 定时向中心服务心跳，拉取并执行任务
// - 采集流量数据并上报
//
// 启动方式：
//
//	CENTER_SERVER_URL="https://api.example.com" \
//	NODE_ID=1 \
//	NODE_TOKEN="agent-token" \
//	XRAY_CONFIG_PATH="/etc/xray/config.json" \
//	XRAY_RESTART_CMD="systemctl restart xray" \
//	REPORT_INTERVAL=60s \
//	go run ./cmd/node-agent
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// ---- helper functions ----

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getDurationEnv(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}

func getUint64Env(key string, defaultVal uint64) uint64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 64); err == nil {
			return n
		}
	}
	return defaultVal
}

func getIntEnv(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

// ---- request/response types ----

// Config node-agent 配置。
type Config struct {
	CenterServerURL   string        // 中心服务 API 地址
	AgentRole         string        // exit 或 relay
	NodeID            uint64        // 节点 ID
	NodeToken         string        // 节点鉴权 Token（明文传输，服务端只保存哈希）
	RelayID           uint64        // 中转节点 ID
	RelayToken        string        // 中转节点鉴权 Token
	XrayConfigPath    string        // xray-core 配置文件路径
	XrayRestartCmd    string        // 重启 xray-core 的命令
	XrayAPIServer     string        // xray-core API 地址，用于 statsquery
	XrayBinary        string        // xray 可执行文件路径
	HAProxyConfigPath string        // HAProxy 配置文件路径
	HAProxyPIDPath    string        // HAProxy PID 文件路径
	HAProxyStatsPath  string        // HAProxy stats socket 路径
	HeartbeatInterval time.Duration // 心跳间隔
	ReportInterval    time.Duration // 流量上报间隔
	TrafficQueuePath  string        // 流量上报失败队列文件
	TrafficQueueLimit int           // 流量上报失败队列最大长度
}

const defaultHAProxyStatsSocketPath = "/tmp/haproxy.sock"
const defaultXrayAPIServer = "127.0.0.1:10085"

// loadConfig 从环境变量加载配置。
func loadConfig() *Config {
	cfg := &Config{
		CenterServerURL:   getEnv("CENTER_SERVER_URL", ""),
		AgentRole:         getEnv("AGENT_ROLE", "exit"),
		XrayConfigPath:    getEnv("XRAY_CONFIG_PATH", "/etc/xray/config.json"),
		XrayRestartCmd:    getEnv("XRAY_RESTART_CMD", "systemctl restart xray"),
		XrayAPIServer:     getEnv("XRAY_API_SERVER", defaultXrayAPIServer),
		XrayBinary:        getEnv("XRAY_BINARY", "xray"),
		HAProxyConfigPath: getEnv("HAPROXY_CONFIG_PATH", "/etc/haproxy/haproxy.cfg"),
		HAProxyPIDPath:    getEnv("HAPROXY_PID_PATH", "/tmp/haproxy.pid"),
		HAProxyStatsPath:  getEnv("HAPROXY_STATS_SOCKET_PATH", defaultHAProxyStatsSocketPath),
		HeartbeatInterval: getDurationEnv("HEARTBEAT_INTERVAL", 10*time.Second),
		ReportInterval:    getDurationEnv("REPORT_INTERVAL", 60*time.Second),
		TrafficQueuePath:  getEnv("TRAFFIC_QUEUE_PATH", ""),
		TrafficQueueLimit: getIntEnv("TRAFFIC_QUEUE_LIMIT", 720),
	}

	if cfg.CenterServerURL == "" {
		log.Fatal("[agent] CENTER_SERVER_URL is required")
	}

	switch cfg.AgentRole {
	case "exit", "":
		cfg.AgentRole = "exit"
		cfg.NodeID = getUint64Env("NODE_ID", 0)
		if cfg.NodeID == 0 {
			log.Fatal("[agent] NODE_ID is required")
		}
		plainToken := getEnv("NODE_TOKEN", "")
		if plainToken == "" {
			log.Fatal("[agent] NODE_TOKEN is required")
		}
		cfg.NodeToken = plainToken
	case "relay":
		cfg.RelayID = getUint64Env("RELAY_ID", 0)
		if cfg.RelayID == 0 {
			log.Fatal("[agent] RELAY_ID is required")
		}
		plainToken := getEnv("RELAY_TOKEN", "")
		if plainToken == "" {
			log.Fatal("[agent] RELAY_TOKEN is required")
		}
		cfg.RelayToken = plainToken
	default:
		log.Fatalf("[agent] unsupported AGENT_ROLE: %s", cfg.AgentRole)
	}

	return cfg
}

// HeartbeatReq 心跳请求。
type HeartbeatReq struct {
	NodeID  uint64 `json:"node_id"`
	Version string `json:"version"`
	Token   string `json:"token"`
}

// HeartbeatResp 心跳响应。
type HeartbeatResp struct {
	Success bool          `json:"success"`
	Data    HeartbeatData `json:"data"`
}

type HeartbeatData struct {
	Tasks []AgentTask `json:"tasks"`
}

// AgentTask 从服务端返回的待执行任务。
type AgentTask struct {
	ID             int64  `json:"id"`
	Action         string `json:"action"`
	Payload        string `json:"payload"`
	IdempotencyKey string `json:"idempotency_key"`
	LockToken      string `json:"lock_token"`
}

// TaskResultReq 任务执行结果上报请求。
type TaskResultReq struct {
	NodeID    uint64 `json:"node_id"`
	Token     string `json:"token"`
	TaskID    uint64 `json:"task_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	LockToken string `json:"lock_token"`
}

// TrafficItem 流量计数器。
type TrafficItem struct {
	XrayUserKey   string `json:"xray_user_key"`
	UplinkTotal   uint64 `json:"uplink_total"`
	DownlinkTotal uint64 `json:"downlink_total"`
}

// TrafficReportReq 流量上报请求。
type TrafficReportReq struct {
	NodeID      uint64        `json:"node_id"`
	Token       string        `json:"token"`
	CollectedAt time.Time     `json:"collected_at"`
	Items       []TrafficItem `json:"items"`
}

// TrafficReportBatch 是本地排队的一次流量采集结果。
type TrafficReportBatch struct {
	CollectedAt time.Time     `json:"collected_at"`
	Items       []TrafficItem `json:"items"`
}

// RelayHeartbeatReq 中转节点心跳请求。
type RelayHeartbeatReq struct {
	RelayID uint64 `json:"relay_id"`
	Version string `json:"version"`
	Token   string `json:"token"`
}

// RelayHeartbeatResp 中转节点心跳响应。
type RelayHeartbeatResp struct {
	Success bool          `json:"success"`
	Data    HeartbeatData `json:"data"`
}

// RelayTaskResultReq 中转配置任务执行结果上报请求。
type RelayTaskResultReq struct {
	RelayID   uint64 `json:"relay_id"`
	Token     string `json:"token"`
	TaskID    uint64 `json:"task_id"`
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	LockToken string `json:"lock_token"`
}

// RelayTrafficItem 中转线路级流量计数器。
type RelayTrafficItem struct {
	RelayBackendID *uint64 `json:"relay_backend_id,omitempty"`
	ListenPort     uint32  `json:"listen_port"`
	BytesInTotal   uint64  `json:"bytes_in_total"`
	BytesOutTotal  uint64  `json:"bytes_out_total"`
}

// RelayTrafficReportReq 中转线路级流量上报请求。
type RelayTrafficReportReq struct {
	RelayID uint64             `json:"relay_id"`
	Token   string             `json:"token"`
	Items   []RelayTrafficItem `json:"items"`
}

// RelayReloadPayload 中转配置刷新任务 payload。
type RelayReloadPayload struct {
	Action        string                `json:"action"`
	ForwarderType string                `json:"forwarder_type"`
	Backends      []RelayBackendPayload `json:"backends"`
}

// RelayBackendPayload 中转后端绑定 payload。
type RelayBackendPayload struct {
	ID         uint64 `json:"id"`
	Name       string `json:"name"`
	ExitNodeID uint64 `json:"exit_node_id"`
	ListenPort uint32 `json:"listen_port"`
	TargetHost string `json:"target_host"`
	TargetPort uint32 `json:"target_port"`
	IsEnabled  bool   `json:"is_enabled"`
}

// XrayConfig xray-core 配置文件结构（部分）。
type XrayConfig struct {
	Inbounds []Inbound `json:"inbounds"`
}

// Inbound xray 入站配置。
type Inbound struct {
	Protocol       string          `json:"protocol"`
	Settings       json.RawMessage `json:"settings"`
	Port           int             `json:"port"`
	Listen         string          `json:"listen,omitempty"`
	StreamSettings json.RawMessage `json:"streamSettings,omitempty"`
}

// VLESSSettings VLESS 入站设置。
type VLESSSettings struct {
	Clients    []VLESSClient `json:"clients"`
	Decryption string        `json:"decryption"`
}

// VLESSClient VLESS 用户配置。
type VLESSClient struct {
	ID         string `json:"id"`
	Flow       string `json:"flow,omitempty"`
	Email      string `json:"email"`
	LimitIP    int    `json:"limitIp,omitempty"`
	TotalGB    uint64 `json:"totalGB,omitempty"`
	ExpiryTime int64  `json:"expiryTime,omitempty"`
}

// Agent node-agent 主控制器。
type Agent struct {
	cfg          *Config
	httpClient   *http.Client
	mu           sync.RWMutex
	traffic      map[string]*TrafficItem // xrayUserKey -> traffic
	queueMu      sync.Mutex
	trafficQueue []TrafficReportBatch
	reporting    int32
}

// NewAgent 创建 agent 实例。
func NewAgent(cfg *Config) *Agent {
	return &Agent{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		traffic:    make(map[string]*TrafficItem),
	}
}

// isContainerEnv 检测是否在容器环境中。
func isContainerEnv() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// ensureXrayInstalled 检查 xray-core 是否已安装，未安装则自动安装。
func (a *Agent) ensureXrayInstalled() error {
	// 1. 检查 xray 是否已安装
	if a.isXrayInstalled() {
		log.Printf("[agent] xray-core already installed at %s", a.cfg.XrayBinary)
		// 检查配置文件是否存在
		if _, err := os.Stat(a.cfg.XrayConfigPath); os.IsNotExist(err) {
			log.Printf("[agent] xray config not found at %s, generating default config", a.cfg.XrayConfigPath)
			if err := a.generateDefaultXrayConfig(); err != nil {
				return fmt.Errorf("generate xray config: %w", err)
			}
		}
		// 容器环境下不需要 systemd 服务
		if isContainerEnv() {
			log.Printf("[agent] running in container, skipping systemd setup")
			return nil
		}
		// 检查 xray systemd 服务是否存在
		if !a.isXrayServiceInstalled() {
			log.Printf("[agent] xray systemd service not found, installing")
			if err := a.installXrayService(); err != nil {
				return fmt.Errorf("install xray service: %w", err)
			}
		}
		return nil
	}

	// 2. 自动安装 xray-core
	log.Printf("[agent] xray-core not found, installing automatically")
	if err := a.installXrayCore(); err != nil {
		return fmt.Errorf("install xray-core: %w", err)
	}

	// 3. 生成默认配置
	if err := a.generateDefaultXrayConfig(); err != nil {
		return fmt.Errorf("generate xray config: %w", err)
	}

	// 4. 安装 systemd 服务
	if err := a.installXrayService(); err != nil {
		return fmt.Errorf("install xray service: %w", err)
	}

	log.Printf("[agent] xray-core installed successfully")
	return nil
}

// isXrayInstalled 检查 xray 是否已安装。
func (a *Agent) isXrayInstalled() bool {
	// 检查配置路径是否可执行
	cmd := exec.Command(a.cfg.XrayBinary, "version")
	return cmd.Run() == nil
}

// installXrayCore 自动安装 xray-core。
func (a *Agent) installXrayCore() error {
	log.Printf("[agent] downloading and installing xray-core...")

	// 下载官方安装脚本到临时文件
	scriptPath := "/tmp/xray-install-release.sh"
	resp, err := http.Get("https://github.com/XTLS/Xray-install/raw/main/install-release.sh")
	if err != nil {
		log.Printf("[agent] download install script failed, using manual install")
		return a.manualInstallXray()
	}
	defer resp.Body.Close()
	scriptData, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[agent] read install script failed, using manual install")
		return a.manualInstallXray()
	}
	os.WriteFile(scriptPath, scriptData, 0755)

	// 执行安装脚本
	cmd := exec.Command("bash", scriptPath, "@", "install")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("[agent] official install script failed, falling back to manual install")
		os.Remove(scriptPath)
		return a.manualInstallXray()
	}
	os.Remove(scriptPath)

	// 验证安装
	if _, err := os.Stat("/usr/local/bin/xray"); err == nil {
		a.cfg.XrayBinary = "/usr/local/bin/xray"
		a.cfg.XrayConfigPath = "/usr/local/etc/xray/config.json"
		a.cfg.XrayRestartCmd = "systemctl restart xray"
		log.Printf("[agent] xray-core installed via official script")
		return nil
	}

	return fmt.Errorf("xray installation failed")
}

// manualInstallXray 手动安装 xray-core（安装脚本失败时的备选方案）。
func (a *Agent) manualInstallXray() error {
	// 检测架构
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "64"
	case "arm64":
		arch = "arm64-v8a"
	case "arm":
		arch = "arm32-v7a"
	case "386":
		arch = "32"
	default:
		arch = "64"
	}

	// 获取最新版本
	resp, err := http.Get("https://api.github.com/repos/XTLS/Xray-core/releases/latest")
	if err != nil {
		return fmt.Errorf("get latest xray version: %w", err)
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("parse release info: %w", err)
	}

	// 找到对应架构的压缩包
	var assetURL string
	zipSuffix := fmt.Sprintf("linux-%s.zip", arch)
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, zipSuffix) {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		return fmt.Errorf("no xray binary found for arch %s", arch)
	}

	log.Printf("[agent] downloading xray-core %s from %s", release.TagName, assetURL)

	// 下载
	dlResp, err := http.Get(assetURL)
	if err != nil {
		return fmt.Errorf("download xray: %w", err)
	}
	defer dlResp.Body.Close()
	zipData, err := io.ReadAll(dlResp.Body)
	if err != nil {
		return fmt.Errorf("read zip data: %w", err)
	}

	// 直接解压到目标目录
	os.MkdirAll("/usr/local/bin", 0755)
	os.MkdirAll("/usr/local/etc/xray", 0755)

	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}

	for _, f := range zr.File {
		if f.Name == "xray" {
			rc, _ := f.Open()
			dst, _ := os.OpenFile("/usr/local/bin/xray", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			io.Copy(dst, rc)
			rc.Close()
			dst.Close()
			break
		}
	}

	// 安装
	os.MkdirAll("/usr/local/bin", 0755)
	os.MkdirAll("/usr/local/etc/xray", 0755)
	os.Chmod("/usr/local/bin/xray", 0755)

	a.cfg.XrayBinary = "/usr/local/bin/xray"
	a.cfg.XrayConfigPath = "/usr/local/etc/xray/config.json"
	a.cfg.XrayRestartCmd = "systemctl restart xray"

	log.Printf("[agent] xray-core %s installed manually", release.TagName)
	return nil
}

// generateDefaultXrayConfig 生成默认 xray-core 配置（含自动生成的 Reality 密钥）。
func (a *Agent) generateDefaultXrayConfig() error {
	// 生成 Reality 密钥对
	keyCmd := exec.Command(a.cfg.XrayBinary, "x25519")
	keyOut, err := keyCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generate x25519 keys: %w", err)
	}

	var privateKey, publicKey string
	for _, line := range strings.Split(string(keyOut), "\n") {
		if strings.HasPrefix(line, "PrivateKey: ") {
			privateKey = strings.TrimPrefix(line, "PrivateKey: ")
		}
		if strings.HasPrefix(line, "Password (PublicKey): ") {
			publicKey = strings.TrimPrefix(line, "Password (PublicKey): ")
		}
	}
	if privateKey == "" || publicKey == "" {
		return fmt.Errorf("failed to parse x25519 keys")
	}

	config := fmt.Sprintf(`{
  "log": {
    "loglevel": "warning"
  },
  "api": {
    "tag": "api",
    "services": ["StatsService"]
  },
  "stats": {},
  "policy": {
    "levels": {
      "0": {
        "statsUserUplink": true,
        "statsUserDownlink": true
      }
    },
    "system": {
      "statsInboundUplink": true,
      "statsInboundDownlink": true,
      "statsOutboundUplink": true,
      "statsOutboundDownlink": true
    }
  },
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [
      {
        "type": "field",
        "inboundTag": ["api"],
        "outboundTag": "api"
      },
      {
        "type": "field",
        "outboundTag": "blocked",
        "protocol": ["bittorrent"]
      }
    ]
  },
  "inbounds": [
    {
      "tag": "api",
      "listen": "127.0.0.1",
      "port": 10085,
      "protocol": "dokodemo-door",
      "settings": {
        "address": "127.0.0.1"
      }
    },
    {
      "protocol": "vless",
      "port": 443,
      "listen": "0.0.0.0",
      "settings": {
        "clients": [],
        "decryption": "none"
      },
      "streamSettings": {
        "network": "tcp",
        "security": "reality",
        "realitySettings": {
          "dest": "www.microsoft.com:443",
          "serverNames": ["www.microsoft.com"],
          "privateKey": "%s",
          "publicKey": "%s",
          "shortIds": [""]
        }
      },
      "sniffing": {
        "enabled": true,
        "destOverride": ["http", "tls"]
      }
    }
  ],
  "outbounds": [
    {
      "protocol": "freedom",
      "tag": "direct"
    },
    {
      "protocol": "blackhole",
      "tag": "blocked"
    }
  ]
}`, privateKey, publicKey)

	os.MkdirAll(filepath.Dir(a.cfg.XrayConfigPath), 0755)
	return os.WriteFile(a.cfg.XrayConfigPath, []byte(config), 0644)
}

func (a *Agent) ensureXrayStatsConfig() error {
	rawData, err := os.ReadFile(a.cfg.XrayConfigPath)
	if err != nil {
		return fmt.Errorf("read xray config: %w", err)
	}

	var rawCfg map[string]interface{}
	if err := json.Unmarshal(rawData, &rawCfg); err != nil {
		return fmt.Errorf("parse xray config: %w", err)
	}

	changed, err := ensureXrayStatsConfigMap(rawCfg, a.cfg.XrayAPIServer)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	newData, err := json.MarshalIndent(rawCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal xray config: %w", err)
	}

	backupPath := a.cfg.XrayConfigPath + ".bak"
	if err := os.WriteFile(backupPath, rawData, 0644); err != nil {
		log.Printf("[agent] backup xray config failed: %v", err)
	}
	if err := os.WriteFile(a.cfg.XrayConfigPath, newData, 0644); err != nil {
		return fmt.Errorf("write xray config: %w", err)
	}

	if err := a.restartXray(); err != nil {
		_ = os.WriteFile(a.cfg.XrayConfigPath, rawData, 0644)
		_ = a.restartXray()
		return fmt.Errorf("restart xray after stats config update: %w", err)
	}
	log.Printf("[agent] xray stats api enabled at %s", a.cfg.XrayAPIServer)
	return nil
}

func ensureXrayStatsConfigMap(rawCfg map[string]interface{}, apiServer string) (bool, error) {
	listenHost, listenPort, err := parseXrayAPIServer(apiServer)
	if err != nil {
		return false, err
	}

	changed := false
	changed = setMapValue(rawCfg, "api", map[string]interface{}{
		"tag":      "api",
		"services": []interface{}{"StatsService"},
	}) || changed
	if _, ok := rawCfg["stats"].(map[string]interface{}); !ok {
		rawCfg["stats"] = map[string]interface{}{}
		changed = true
	}

	policy, policyChanged := ensureObject(rawCfg, "policy")
	changed = policyChanged || changed
	levels, levelsChanged := ensureObject(policy, "levels")
	changed = levelsChanged || changed
	level0, levelChanged := ensureObject(levels, "0")
	changed = levelChanged || changed
	changed = setMapValue(level0, "statsUserUplink", true) || changed
	changed = setMapValue(level0, "statsUserDownlink", true) || changed
	system, systemChanged := ensureObject(policy, "system")
	changed = systemChanged || changed
	for _, key := range []string{"statsInboundUplink", "statsInboundDownlink", "statsOutboundUplink", "statsOutboundDownlink"} {
		changed = setMapValue(system, key, true) || changed
	}

	inbounds, ok := rawCfg["inbounds"].([]interface{})
	if !ok {
		return false, fmt.Errorf("xray config inbounds is missing or invalid")
	}
	apiInbound := map[string]interface{}{
		"tag":      "api",
		"listen":   listenHost,
		"port":     float64(listenPort),
		"protocol": "dokodemo-door",
		"settings": map[string]interface{}{
			"address": "127.0.0.1",
		},
	}
	apiInboundIdx := findTaggedObject(inbounds, "api")
	if apiInboundIdx < 0 {
		rawCfg["inbounds"] = append([]interface{}{apiInbound}, inbounds...)
		changed = true
	} else if inboundMap, ok := inbounds[apiInboundIdx].(map[string]interface{}); ok {
		for key, value := range apiInbound {
			changed = setMapValue(inboundMap, key, value) || changed
		}
		inbounds[apiInboundIdx] = inboundMap
		rawCfg["inbounds"] = inbounds
	} else {
		inbounds[apiInboundIdx] = apiInbound
		rawCfg["inbounds"] = inbounds
		changed = true
	}

	routing, routingChanged := ensureObject(rawCfg, "routing")
	changed = routingChanged || changed
	rules, _ := routing["rules"].([]interface{})
	if !hasXrayAPIRoutingRule(rules) {
		apiRule := map[string]interface{}{
			"type":        "field",
			"inboundTag":  []interface{}{"api"},
			"outboundTag": "api",
		}
		routing["rules"] = append([]interface{}{apiRule}, rules...)
		changed = true
	}

	outbounds, _ := rawCfg["outbounds"].([]interface{})
	if findTaggedObject(outbounds, "blocked") < 0 {
		rawCfg["outbounds"] = append(outbounds, map[string]interface{}{
			"tag":      "blocked",
			"protocol": "blackhole",
		})
		changed = true
	}

	return changed, nil
}

func parseXrayAPIServer(apiServer string) (string, int, error) {
	apiServer = strings.TrimSpace(apiServer)
	if apiServer == "" {
		apiServer = defaultXrayAPIServer
	}
	host, portRaw, err := net.SplitHostPort(apiServer)
	if err != nil {
		return "", 0, fmt.Errorf("invalid XRAY_API_SERVER %q: %w", apiServer, err)
	}
	if host == "" || host == "localhost" {
		host = "127.0.0.1"
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, fmt.Errorf("invalid XRAY_API_SERVER port %q", portRaw)
	}
	return host, port, nil
}

func ensureObject(parent map[string]interface{}, key string) (map[string]interface{}, bool) {
	if obj, ok := parent[key].(map[string]interface{}); ok {
		return obj, false
	}
	obj := map[string]interface{}{}
	parent[key] = obj
	return obj, true
}

func setMapValue(target map[string]interface{}, key string, value interface{}) bool {
	if reflect.DeepEqual(target[key], value) {
		return false
	}
	target[key] = value
	return true
}

func findTaggedObject(items []interface{}, tag string) int {
	for i, item := range items {
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if itemMap["tag"] == tag {
			return i
		}
	}
	return -1
}

func hasXrayAPIRoutingRule(rules []interface{}) bool {
	for _, rule := range rules {
		ruleMap, ok := rule.(map[string]interface{})
		if !ok || ruleMap["outboundTag"] != "api" {
			continue
		}
		switch tags := ruleMap["inboundTag"].(type) {
		case []interface{}:
			for _, tag := range tags {
				if tag == "api" {
					return true
				}
			}
		case []string:
			for _, tag := range tags {
				if tag == "api" {
					return true
				}
			}
		case string:
			if tags == "api" {
				return true
			}
		}
	}
	return false
}

type xrayStatValue int64

func (v *xrayStatValue) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		*v = 0
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var raw string
		if err := json.Unmarshal(data, &raw); err != nil {
			return err
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		*v = xrayStatValue(n)
		return nil
	}

	var n int64
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	*v = xrayStatValue(n)
	return nil
}

// isXrayServiceInstalled 检查 xray systemd 服务是否已安装。
func (a *Agent) isXrayServiceInstalled() bool {
	cmd := exec.Command("systemctl", "list-unit-files", "xray.service")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "xray.service")
}

// installXrayService 安装 xray systemd 服务。
func (a *Agent) installXrayService() error {
	serviceContent := `[Unit]
Description=Xray Core Service
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/xray run -config /usr/local/etc/xray/config.json
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`

	servicePath := "/etc/systemd/system/xray.service"
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}

	// 重载 systemd 并启用服务
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	if err := exec.Command("systemctl", "enable", "xray.service").Run(); err != nil {
		return fmt.Errorf("enable xray service: %w", err)
	}
	if err := exec.Command("systemctl", "start", "xray.service").Run(); err != nil {
		return fmt.Errorf("start xray service: %w", err)
	}

	log.Printf("[agent] xray.service installed and started")
	return nil
}

// Run 启动 agent 主循环。
func (a *Agent) Run() {
	log.Printf("[agent] starting node-agent v1.0.0")
	log.Printf("[agent] center server: %s", a.cfg.CenterServerURL)
	log.Printf("[agent] role: %s", a.cfg.AgentRole)
	if a.cfg.AgentRole == "relay" {
		a.runRelay()
		return
	}
	log.Printf("[agent] node ID: %d", a.cfg.NodeID)
	log.Printf("[agent] xray config: %s", a.cfg.XrayConfigPath)

	// 检查并自动安装 xray-core
	if err := a.ensureXrayInstalled(); err != nil {
		log.Fatalf("[agent] xray-core setup failed: %v", err)
	}
	if err := a.ensureXrayStatsConfig(); err != nil {
		log.Fatalf("[agent] xray stats setup failed: %v", err)
	}
	if err := a.ensureXrayProcessRunning(); err != nil {
		log.Fatalf("[agent] xray process setup failed: %v", err)
	}

	// 加载初始流量快照
	a.loadTrafficSnapshot()
	a.loadTrafficQueue()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动心跳任务
	go a.heartbeatLoop(ctx)

	// 启动流量采集
	go a.trafficCollectLoop(ctx)

	// 等待终止信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[agent] shutting down...")
	cancel()

	// 保存当前流量快照
	a.saveTrafficSnapshot()
	a.saveTrafficQueue()
}

// runRelay 启动中转节点模式。relay 模式只管理 HAProxy 转发配置，不读取或修改 Xray 用户。
func (a *Agent) runRelay() {
	log.Printf("[agent] relay ID: %d", a.cfg.RelayID)
	log.Printf("[agent] haproxy config: %s", a.cfg.HAProxyConfigPath)
	log.Printf("[agent] haproxy stats socket: %s", a.cfg.HAProxyStatsPath)

	if err := a.ensureHAProxyReady(); err != nil {
		log.Fatalf("[agent] haproxy setup failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go a.relayHeartbeatLoop(ctx)
	go a.relayTrafficCollectLoop(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[agent] relay mode shutting down...")
	cancel()
}

func (a *Agent) ensureHAProxyReady() error {
	if _, err := exec.LookPath("haproxy"); err != nil {
		return fmt.Errorf("haproxy not found in PATH: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(a.cfg.HAProxyConfigPath), 0755); err != nil {
		return fmt.Errorf("create haproxy config dir: %w", err)
	}
	if _, err := os.Stat(a.cfg.HAProxyConfigPath); os.IsNotExist(err) {
		if err := os.WriteFile(a.cfg.HAProxyConfigPath, []byte(renderHAProxyConfig(nil, a.cfg.HAProxyStatsPath)), 0644); err != nil {
			return fmt.Errorf("write default haproxy config: %w", err)
		}
		return nil
	}
	data, err := os.ReadFile(a.cfg.HAProxyConfigPath)
	if err != nil {
		return fmt.Errorf("read haproxy config: %w", err)
	}
	if !strings.Contains(string(data), "frontend relay_") {
		return nil
	}
	if err := a.validateHAProxyConfig(a.cfg.HAProxyConfigPath); err != nil {
		return err
	}
	return a.reloadHAProxy(a.cfg.HAProxyConfigPath)
}

// relayHeartbeatLoop 定时向中心服务上报中转心跳并拉取配置任务。
func (a *Agent) relayHeartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()

	a.doRelayHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.doRelayHeartbeat(ctx)
		}
	}
}

func (a *Agent) doRelayHeartbeat(ctx context.Context) {
	reqBody := RelayHeartbeatReq{
		RelayID: a.cfg.RelayID,
		Version: "1.0.0",
		Token:   a.cfg.RelayToken,
	}

	var resp RelayHeartbeatResp
	if err := a.postJSON(ctx, "/api/agent/relay/heartbeat", reqBody, &resp); err != nil {
		log.Printf("[agent] relay heartbeat error: %v", err)
		return
	}
	if !resp.Success {
		log.Printf("[agent] relay heartbeat failed")
		return
	}

	for _, task := range resp.Data.Tasks {
		a.executeRelayTask(ctx, task)
	}
}

func (a *Agent) executeRelayTask(ctx context.Context, task AgentTask) {
	log.Printf("[agent] executing relay task %d: action=%s", task.ID, task.Action)

	var errMsg string
	switch task.Action {
	case "RELOAD_CONFIG":
		errMsg = a.handleReloadConfig(task.Payload)
	default:
		errMsg = fmt.Sprintf("unknown relay action: %s", task.Action)
	}

	success := errMsg == ""
	resultReq := RelayTaskResultReq{
		RelayID:   a.cfg.RelayID,
		Token:     a.cfg.RelayToken,
		TaskID:    uint64(task.ID),
		Success:   success,
		Error:     errMsg,
		LockToken: task.LockToken,
	}

	var resultResp struct{}
	if err := a.postJSON(ctx, "/api/agent/relay/task-result", resultReq, &resultResp); err != nil {
		log.Printf("[agent] report relay task result error: %v", err)
	}

	if success {
		log.Printf("[agent] relay task %d completed successfully", task.ID)
	} else {
		log.Printf("[agent] relay task %d failed: %s", task.ID, errMsg)
	}
}

func (a *Agent) handleReloadConfig(payload string) string {
	var p RelayReloadPayload
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Sprintf("invalid payload: %v", err)
	}
	if p.ForwarderType == "" {
		p.ForwarderType = "haproxy"
	}
	if p.ForwarderType != "haproxy" {
		return fmt.Sprintf("unsupported forwarder_type: %s", p.ForwarderType)
	}

	config, enabledCount, err := buildHAProxyConfig(p.Backends, a.cfg.HAProxyStatsPath)
	if err != nil {
		return err.Error()
	}
	if err := a.applyHAProxyConfig(config, enabledCount == 0); err != nil {
		return err.Error()
	}
	return ""
}

func buildHAProxyConfig(backends []RelayBackendPayload, statsSocketPath ...string) (string, int, error) {
	enabled := make([]RelayBackendPayload, 0, len(backends))
	ports := make(map[uint32]struct{}, len(backends))
	for _, backend := range backends {
		if !backend.IsEnabled {
			continue
		}
		if backend.ListenPort == 0 || backend.ListenPort > 65535 {
			return "", 0, fmt.Errorf("invalid listen_port: %d", backend.ListenPort)
		}
		if _, exists := ports[backend.ListenPort]; exists {
			return "", 0, fmt.Errorf("duplicated listen_port: %d", backend.ListenPort)
		}
		ports[backend.ListenPort] = struct{}{}
		if backend.TargetHost == "" || strings.ContainsAny(backend.TargetHost, " \t\r\n") {
			return "", 0, fmt.Errorf("invalid target_host for listen_port %d", backend.ListenPort)
		}
		if backend.TargetPort == 0 || backend.TargetPort > 65535 {
			return "", 0, fmt.Errorf("invalid target_port: %d", backend.TargetPort)
		}
		enabled = append(enabled, backend)
	}
	return renderHAProxyConfig(enabled, statsSocketPath...), len(enabled), nil
}

func renderHAProxyConfig(backends []RelayBackendPayload, statsSocketPath ...string) string {
	statsPath := defaultHAProxyStatsSocketPath
	if len(statsSocketPath) > 0 && strings.TrimSpace(statsSocketPath[0]) != "" {
		statsPath = strings.TrimSpace(statsSocketPath[0])
	}

	var b strings.Builder
	b.WriteString("global\n")
	b.WriteString("  daemon\n")
	b.WriteString("  maxconn 20000\n")
	b.WriteString("  log stdout format raw local0\n\n")
	b.WriteString(fmt.Sprintf("  stats socket %s mode 600 level admin\n\n", statsPath))
	b.WriteString("defaults\n")
	b.WriteString("  mode tcp\n")
	b.WriteString("  timeout connect 5s\n")
	b.WriteString("  timeout client 5m\n")
	b.WriteString("  timeout server 5m\n\n")

	for _, backend := range backends {
		b.WriteString(fmt.Sprintf("frontend relay_%d_%d\n", backend.ID, backend.ListenPort))
		b.WriteString(fmt.Sprintf("  bind *:%d\n", backend.ListenPort))
		b.WriteString("  mode tcp\n")
		b.WriteString(fmt.Sprintf("  default_backend backend_%d\n\n", backend.ID))
		b.WriteString(fmt.Sprintf("backend backend_%d\n", backend.ID))
		b.WriteString("  mode tcp\n")
		b.WriteString(fmt.Sprintf("  server exit_%d %s:%d check\n\n", backend.ExitNodeID, backend.TargetHost, backend.TargetPort))
	}

	return b.String()
}

func (a *Agent) applyHAProxyConfig(config string, emptyConfig bool) error {
	configPath := a.cfg.HAProxyConfigPath
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("create haproxy config dir: %w", err)
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", configPath, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write temp haproxy config: %w", err)
	}
	defer os.Remove(tmpPath)

	if !emptyConfig {
		if err := a.validateHAProxyConfig(tmpPath); err != nil {
			return err
		}
	}

	oldData, oldErr := os.ReadFile(configPath)
	if err := os.Rename(tmpPath, configPath); err != nil {
		return fmt.Errorf("replace haproxy config: %w", err)
	}
	if emptyConfig {
		return a.stopHAProxy()
	}
	if err := a.reloadHAProxy(configPath); err != nil {
		if oldErr == nil {
			if restoreErr := os.WriteFile(configPath, oldData, 0644); restoreErr == nil {
				_ = a.reloadHAProxy(configPath)
			}
		}
		return err
	}
	return nil
}

func (a *Agent) stopHAProxy() error {
	pidData, err := os.ReadFile(a.cfg.HAProxyPIDPath)
	if err != nil {
		return nil
	}
	pids := strings.Fields(string(pidData))
	if len(pids) == 0 {
		_ = os.Remove(a.cfg.HAProxyPIDPath)
		return nil
	}
	args := append([]string{"-TERM"}, pids...)
	if output, err := exec.Command("kill", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("stop haproxy failed: %v, output: %s", err, string(output))
	}
	_ = os.Remove(a.cfg.HAProxyPIDPath)
	return nil
}

func (a *Agent) validateHAProxyConfig(configPath string) error {
	cmd := exec.Command("haproxy", "-c", "-f", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("haproxy config check failed: %v, output: %s", err, string(output))
	}
	return nil
}

func (a *Agent) reloadHAProxy(configPath string) error {
	args := []string{"-f", configPath, "-p", a.cfg.HAProxyPIDPath}
	if pidData, err := os.ReadFile(a.cfg.HAProxyPIDPath); err == nil {
		pids := strings.Fields(string(pidData))
		if len(pids) > 0 {
			args = append(args, "-sf")
			args = append(args, pids...)
		}
	}

	cmd := exec.Command("haproxy", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("haproxy reload failed: %v, output: %s", err, string(output))
	}
	return nil
}

// heartbeatLoop 定时心跳循环。
func (a *Agent) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval)
	defer ticker.Stop()

	// 立即执行一次
	a.doHeartbeat(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.doHeartbeat(ctx)
		}
	}
}

// doHeartbeat 执行一次心跳。
func (a *Agent) doHeartbeat(ctx context.Context) {
	reqBody := HeartbeatReq{
		NodeID:  a.cfg.NodeID,
		Version: "1.0.0",
		Token:   a.cfg.NodeToken,
	}

	var resp HeartbeatResp
	if err := a.postJSON(ctx, "/api/agent/heartbeat", reqBody, &resp); err != nil {
		log.Printf("[agent] heartbeat error: %v", err)
		return
	}

	if !resp.Success {
		log.Printf("[agent] heartbeat failed")
		return
	}

	// 执行拉取到的任务
	for _, task := range resp.Data.Tasks {
		a.executeTask(ctx, task)
	}
}

// executeTask 执行单个任务。
func (a *Agent) executeTask(ctx context.Context, task AgentTask) {
	log.Printf("[agent] executing task %d: action=%s", task.ID, task.Action)

	var errMsg string

	switch task.Action {
	case "UPSERT_USER":
		errMsg = a.handleUpsertUser(task.Payload)
	case "DISABLE_USER":
		errMsg = a.handleDisableUser(task.Payload)
	default:
		errMsg = fmt.Sprintf("unknown action: %s", task.Action)
	}

	success := errMsg == ""

	// 上报执行结果
	resultReq := TaskResultReq{
		NodeID:    a.cfg.NodeID,
		Token:     a.cfg.NodeToken,
		TaskID:    uint64(task.ID),
		Success:   success,
		Error:     errMsg,
		LockToken: task.LockToken,
	}

	var resultResp struct{}
	if err := a.postJSON(ctx, "/api/agent/task-result", resultReq, &resultResp); err != nil {
		log.Printf("[agent] report task result error: %v", err)
	}

	if success {
		log.Printf("[agent] task %d completed successfully", task.ID)
	} else {
		log.Printf("[agent] task %d failed: %s", task.ID, errMsg)
	}
}

// handleUpsertUser 处理 UPSERT_USER 任务。
// payload 格式：{"xray_user_key":"user@domain","uuid":"xxx-xxx-xxx"}
func (a *Agent) handleUpsertUser(payload string) string {
	var p struct {
		XrayUserKey string `json:"xray_user_key"`
		UUID        string `json:"uuid"`
		Flow        string `json:"flow"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Sprintf("invalid payload: %v", err)
	}

	if p.Flow == "" {
		p.Flow = "xtls-rprx-vision"
	}

	if err := a.addVLESSClient(p.UUID, p.XrayUserKey, p.Flow); err != nil {
		return fmt.Sprintf("add vless client failed: %v", err)
	}

	// 初始化流量计数器
	a.mu.Lock()
	a.traffic[p.XrayUserKey] = &TrafficItem{
		XrayUserKey: p.XrayUserKey,
	}
	a.mu.Unlock()

	return ""
}

// handleDisableUser 处理 DISABLE_USER 任务。
// payload 格式：{"xray_user_key":"user@domain"}
func (a *Agent) handleDisableUser(payload string) string {
	var p struct {
		XrayUserKey string `json:"xray_user_key"`
	}
	if err := json.Unmarshal([]byte(payload), &p); err != nil {
		return fmt.Sprintf("invalid payload: %v", err)
	}

	if err := a.removeVLESSClient(p.XrayUserKey); err != nil {
		return fmt.Sprintf("remove vless client failed: %v", err)
	}

	return ""
}

// addVLESSClient 向 xray-core 配置文件添加 VLESS 用户。
func (a *Agent) addVLESSClient(uuid, email, flow string) error {
	configPath := a.cfg.XrayConfigPath

	// 读取原始配置（保留完整 JSON，避免丢失其他字段）
	rawData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	// 解析为通用 map，保留所有原始字段
	var rawCfg map[string]interface{}
	if err := json.Unmarshal(rawData, &rawCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// 找到 VLESS 入站
	inboundsRaw, ok := rawCfg["inbounds"].([]interface{})
	if !ok {
		return fmt.Errorf("no inbounds found")
	}

	vlessIdx := -1
	for i, inbound := range inboundsRaw {
		inbMap, ok := inbound.(map[string]interface{})
		if !ok {
			continue
		}
		if inbMap["protocol"] == "vless" {
			vlessIdx = i
			break
		}
	}

	if vlessIdx < 0 {
		return fmt.Errorf("no vless inbound found")
	}

	// 获取 VLESS 入站设置
	inbMap := inboundsRaw[vlessIdx].(map[string]interface{})
	settingsRaw, ok := inbMap["settings"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("parse vless settings: no settings")
	}

	clientsRaw, ok := settingsRaw["clients"].([]interface{})
	if !ok {
		return fmt.Errorf("parse vless settings: no clients")
	}

	// 查找并更新或添加用户
	found := false
	for i, client := range clientsRaw {
		cMap, ok := client.(map[string]interface{})
		if !ok {
			continue
		}
		if cMap["email"] == email {
			cMap["id"] = uuid
			cMap["flow"] = flow
			clientsRaw[i] = cMap
			found = true
			break
		}
	}

	if !found {
		clientsRaw = append(clientsRaw, map[string]interface{}{
			"id":    uuid,
			"flow":  flow,
			"email": email,
		})
	}

	settingsRaw["clients"] = clientsRaw
	inbMap["settings"] = settingsRaw
	inboundsRaw[vlessIdx] = inbMap
	rawCfg["inbounds"] = inboundsRaw

	// 写回完整配置（所有其他字段保持不变）
	newData, err := json.MarshalIndent(rawCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// 备份原配置
	backupPath := configPath + ".bak"
	if err := os.WriteFile(backupPath, rawData, 0644); err != nil {
		log.Printf("[agent] backup config failed: %v", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// 重启 xray-core
	return a.restartXray()
}

// removeVLESSClient 从 xray-core 配置文件中移除 VLESS 用户。
func (a *Agent) removeVLESSClient(email string) error {
	configPath := a.cfg.XrayConfigPath

	rawData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var rawCfg map[string]interface{}
	if err := json.Unmarshal(rawData, &rawCfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	inboundsRaw, ok := rawCfg["inbounds"].([]interface{})
	if !ok {
		return fmt.Errorf("no inbounds found")
	}

	vlessIdx := -1
	for i, inbound := range inboundsRaw {
		inbMap, ok := inbound.(map[string]interface{})
		if !ok {
			continue
		}
		if inbMap["protocol"] == "vless" {
			vlessIdx = i
			break
		}
	}

	if vlessIdx < 0 {
		return fmt.Errorf("no vless inbound found")
	}

	inbMap := inboundsRaw[vlessIdx].(map[string]interface{})
	settingsRaw, ok := inbMap["settings"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("parse vless settings: no settings")
	}
	clientsRaw, ok := settingsRaw["clients"].([]interface{})
	if !ok {
		return fmt.Errorf("parse vless settings: no clients")
	}

	// 移除用户
	newClients := make([]interface{}, 0, len(clientsRaw))
	found := false
	for _, client := range clientsRaw {
		cMap, ok := client.(map[string]interface{})
		if !ok {
			newClients = append(newClients, client)
			continue
		}
		if cMap["email"] == email {
			found = true
			continue
		}
		newClients = append(newClients, cMap)
	}

	if !found {
		log.Printf("[agent] user %s not found in config, skip", email)
		return nil
	}

	settingsRaw["clients"] = newClients
	inbMap["settings"] = settingsRaw
	inboundsRaw[vlessIdx] = inbMap
	rawCfg["inbounds"] = inboundsRaw

	newData, err := json.MarshalIndent(rawCfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	backupPath := configPath + ".bak"
	if err := os.WriteFile(backupPath, rawData, 0644); err != nil {
		log.Printf("[agent] backup config failed: %v", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// 重启 xray-core
	return a.restartXray()
}

// restartXray 重启 xray-core。
func (a *Agent) restartXray() error {
	// 容器环境下通过 Docker API 或 xray 进程管理重启
	if isContainerEnv() {
		log.Printf("[agent] container mode: restarting xray process")
		// 杀掉现有 xray 进程
		exec.Command("pkill", "-f", "xray run").Run()
		time.Sleep(1 * time.Second)
		// 启动新 xray 进程（后台）
		cmd := exec.Command(a.cfg.XrayBinary, "run", "-config", a.cfg.XrayConfigPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("start xray: %w", err)
		}
		log.Printf("[agent] xray process started (PID %d)", cmd.Process.Pid)
		return nil
	}

	if a.cfg.XrayRestartCmd == "" {
		log.Println("[agent] skip xray restart (no restart command configured)")
		return nil
	}

	parts := strings.Fields(a.cfg.XrayRestartCmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty restart command")
	}

	log.Printf("[agent] restarting xray: %s", a.cfg.XrayRestartCmd)
	cmd := exec.Command(parts[0], parts[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("restart failed: %v, output: %s", err, string(output))
	}

	log.Println("[agent] xray restarted successfully")
	return nil
}

func (a *Agent) ensureXrayProcessRunning() error {
	if !isContainerEnv() {
		return nil
	}
	cmd := exec.Command("sh", "-c", "pidof xray >/dev/null 2>&1 || pgrep -f 'xray run' >/dev/null 2>&1")
	if err := cmd.Run(); err == nil {
		return nil
	}
	return a.restartXray()
}

// trafficCollectLoop 流量采集循环。
func (a *Agent) trafficCollectLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.collectAndReportTraffic(ctx)
		}
	}
}

// collectAndReportTraffic 采集并上报流量。
func (a *Agent) collectAndReportTraffic(ctx context.Context) {
	// 从 xray-core 配置文件获取当前用户列表
	clients := a.getVLESSClients()
	if len(clients) == 0 {
		return
	}

	stats, err := a.queryXrayStats(ctx)
	if err != nil {
		log.Printf("[agent] query xray stats failed: %v", err)
		return
	}

	a.mu.Lock()
	items := make([]TrafficItem, 0, len(clients))
	for _, client := range clients {
		stat := stats[client.Email]
		if existing, ok := a.traffic[client.Email]; ok {
			existing.UplinkTotal = stat.UplinkTotal
			existing.DownlinkTotal = stat.DownlinkTotal
			itemCopy := *existing
			items = append(items, itemCopy)
		} else {
			// 新用户，初始化
			a.traffic[client.Email] = &TrafficItem{
				XrayUserKey:   client.Email,
				UplinkTotal:   stat.UplinkTotal,
				DownlinkTotal: stat.DownlinkTotal,
			}
			itemCopy := *a.traffic[client.Email]
			items = append(items, itemCopy)
		}
	}
	// 检查是否有已删除的用户需要保留快照
	for key, item := range a.traffic {
		found := false
		for _, c := range clients {
			if c.Email == key {
				found = true
				break
			}
		}
		if found {
			continue
		}
		// 用户已不在配置中，但保留其流量快照供下次比较
		items = append(items, *item)
	}
	a.mu.Unlock()

	if len(items) == 0 {
		return
	}

	a.enqueueTrafficReport(TrafficReportBatch{
		CollectedAt: time.Now(),
		Items:       items,
	})
	a.flushTrafficQueue(ctx)
}

func (a *Agent) trafficQueuePath() string {
	if a.cfg.TrafficQueuePath != "" {
		return a.cfg.TrafficQueuePath
	}
	return filepath.Join(filepath.Dir(a.cfg.XrayConfigPath), "traffic_queue.json")
}

func (a *Agent) enqueueTrafficReport(batch TrafficReportBatch) {
	if batch.CollectedAt.IsZero() {
		batch.CollectedAt = time.Now()
	}
	if len(batch.Items) == 0 {
		return
	}

	a.queueMu.Lock()
	defer a.queueMu.Unlock()

	a.trafficQueue = append(a.trafficQueue, batch)
	limit := a.cfg.TrafficQueueLimit
	if limit <= 0 {
		limit = 720
	}
	if len(a.trafficQueue) > limit {
		drop := len(a.trafficQueue) - limit
		a.trafficQueue = append([]TrafficReportBatch(nil), a.trafficQueue[drop:]...)
		log.Printf("[agent] traffic queue exceeded limit, dropped %d oldest batches", drop)
	}
	a.saveTrafficQueueLocked()
}

func (a *Agent) flushTrafficQueue(ctx context.Context) {
	if !atomic.CompareAndSwapInt32(&a.reporting, 0, 1) {
		return
	}
	defer atomic.StoreInt32(&a.reporting, 0)

	for {
		a.queueMu.Lock()
		if len(a.trafficQueue) == 0 {
			a.queueMu.Unlock()
			return
		}
		batch := a.trafficQueue[0]
		a.queueMu.Unlock()

		req := TrafficReportReq{
			NodeID:      a.cfg.NodeID,
			Token:       a.cfg.NodeToken,
			CollectedAt: batch.CollectedAt,
			Items:       batch.Items,
		}

		var resp struct{}
		if err := a.postJSON(ctx, "/api/agent/traffic", req, &resp); err != nil {
			log.Printf("[agent] traffic report error: %v", err)
			return
		}

		a.queueMu.Lock()
		if len(a.trafficQueue) > 0 && trafficReportBatchEqual(a.trafficQueue[0], batch) {
			a.trafficQueue = append([]TrafficReportBatch(nil), a.trafficQueue[1:]...)
			a.saveTrafficQueueLocked()
		}
		remaining := len(a.trafficQueue)
		a.queueMu.Unlock()

		log.Printf("[agent] traffic reported: %d users, queued batches remaining: %d", len(batch.Items), remaining)
	}
}

func trafficReportBatchEqual(a TrafficReportBatch, b TrafficReportBatch) bool {
	if !a.CollectedAt.Equal(b.CollectedAt) {
		return false
	}
	return reflect.DeepEqual(a.Items, b.Items)
}

// relayTrafficCollectLoop 采集中转端口线路级流量。
func (a *Agent) relayTrafficCollectLoop(ctx context.Context) {
	ticker := time.NewTicker(a.cfg.ReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.collectAndReportRelayTraffic(ctx)
		}
	}
}

func (a *Agent) collectAndReportRelayTraffic(ctx context.Context) {
	items, err := a.queryHAProxyStats(ctx)
	if err != nil {
		log.Printf("[agent] query haproxy stats failed: %v", err)
		return
	}
	if len(items) == 0 {
		return
	}

	req := RelayTrafficReportReq{
		RelayID: a.cfg.RelayID,
		Token:   a.cfg.RelayToken,
		Items:   items,
	}

	var resp struct{}
	if err := a.postJSON(ctx, "/api/agent/relay/traffic", req, &resp); err != nil {
		log.Printf("[agent] relay traffic report error: %v", err)
		return
	}

	log.Printf("[agent] relay traffic reported: %d backends", len(items))
}

func (a *Agent) queryHAProxyStats(ctx context.Context) ([]RelayTrafficItem, error) {
	if strings.TrimSpace(a.cfg.HAProxyStatsPath) == "" {
		return nil, fmt.Errorf("HAPROXY_STATS_SOCKET_PATH is empty")
	}

	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", a.cfg.HAProxyStatsPath)
	if err != nil {
		return nil, fmt.Errorf("dial stats socket: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, fmt.Errorf("set stats socket deadline: %w", err)
	}
	if _, err := io.WriteString(conn, "show stat\n"); err != nil {
		return nil, fmt.Errorf("write stats command: %w", err)
	}

	data, err := io.ReadAll(conn)
	if err != nil && len(data) == 0 {
		return nil, fmt.Errorf("read stats response: %w", err)
	}
	return parseHAProxyStats(data)
}

func parseHAProxyStats(data []byte) ([]RelayTrafficItem, error) {
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse haproxy stats csv: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	header := records[0]
	if len(header) == 0 {
		return nil, nil
	}
	header[0] = strings.TrimPrefix(header[0], "# ")
	header[0] = strings.TrimPrefix(header[0], "#")

	indexes := make(map[string]int, len(header))
	for i, name := range header {
		indexes[strings.TrimSpace(name)] = i
	}
	pxnameIdx, ok := indexes["pxname"]
	if !ok {
		return nil, fmt.Errorf("haproxy stats missing pxname column")
	}
	svnameIdx, ok := indexes["svname"]
	if !ok {
		return nil, fmt.Errorf("haproxy stats missing svname column")
	}
	binIdx, ok := indexes["bin"]
	if !ok {
		return nil, fmt.Errorf("haproxy stats missing bin column")
	}
	boutIdx, ok := indexes["bout"]
	if !ok {
		return nil, fmt.Errorf("haproxy stats missing bout column")
	}

	items := make([]RelayTrafficItem, 0)
	for _, record := range records[1:] {
		if len(record) <= pxnameIdx || len(record) <= svnameIdx || len(record) <= binIdx || len(record) <= boutIdx {
			continue
		}
		if record[svnameIdx] != "FRONTEND" {
			continue
		}
		backendID, listenPort, ok := parseRelayFrontendName(record[pxnameIdx])
		if !ok {
			continue
		}
		bytesIn, err := strconv.ParseUint(strings.TrimSpace(record[binIdx]), 10, 64)
		if err != nil {
			continue
		}
		bytesOut, err := strconv.ParseUint(strings.TrimSpace(record[boutIdx]), 10, 64)
		if err != nil {
			continue
		}

		idCopy := backendID
		items = append(items, RelayTrafficItem{
			RelayBackendID: &idCopy,
			ListenPort:     listenPort,
			BytesInTotal:   bytesIn,
			BytesOutTotal:  bytesOut,
		})
	}

	return items, nil
}

func parseRelayFrontendName(name string) (uint64, uint32, bool) {
	parts := strings.Split(name, "_")
	if len(parts) != 3 || parts[0] != "relay" {
		return 0, 0, false
	}
	backendID, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil || backendID == 0 {
		return 0, 0, false
	}
	port64, err := strconv.ParseUint(parts[2], 10, 32)
	if err != nil || port64 == 0 || port64 > 65535 {
		return 0, 0, false
	}
	return backendID, uint32(port64), true
}

func (a *Agent) queryXrayStats(ctx context.Context) (map[string]TrafficItem, error) {
	if a.cfg.XrayAPIServer == "" {
		return nil, fmt.Errorf("XRAY_API_SERVER is not configured")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, a.cfg.XrayBinary, "api", "statsquery",
		"--server", a.cfg.XrayAPIServer,
		"-pattern", "user>>>",
		"-reset=false",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("statsquery failed: %w, output: %s", err, strings.TrimSpace(string(output)))
	}

	var resp struct {
		Stat []struct {
			Name  string        `json:"name"`
			Value xrayStatValue `json:"value"`
		} `json:"stat"`
	}
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, fmt.Errorf("parse statsquery output: %w", err)
	}

	stats := make(map[string]TrafficItem)
	for _, stat := range resp.Stat {
		parts := strings.Split(stat.Name, ">>>")
		value := int64(stat.Value)
		if len(parts) < 4 || parts[0] != "user" || parts[2] != "traffic" || value < 0 {
			continue
		}
		email := parts[1]
		item := stats[email]
		item.XrayUserKey = email
		switch parts[3] {
		case "uplink":
			item.UplinkTotal = uint64(value)
		case "downlink":
			item.DownlinkTotal = uint64(value)
		}
		stats[email] = item
	}

	return stats, nil
}

// VLESSClientInfo 从 xray 配置读取的用户信息。
type VLESSClientInfo struct {
	Email         string
	UplinkTotal   uint64
	DownlinkTotal uint64
}

// getVLESSClients 从 xray 配置文件读取当前 VLESS 用户列表。
func (a *Agent) getVLESSClients() []VLESSClientInfo {
	data, err := os.ReadFile(a.cfg.XrayConfigPath)
	if err != nil {
		log.Printf("[agent] read xray config failed: %v", err)
		return nil
	}

	var xrayCfg XrayConfig
	if err := json.Unmarshal(data, &xrayCfg); err != nil {
		log.Printf("[agent] parse xray config failed: %v", err)
		return nil
	}

	var clients []VLESSClientInfo
	for _, inbound := range xrayCfg.Inbounds {
		if inbound.Protocol == "vless" {
			var settings VLESSSettings
			if err := json.Unmarshal(inbound.Settings, &settings); err != nil {
				continue
			}
			for _, c := range settings.Clients {
				if c.Email != "" {
					clients = append(clients, VLESSClientInfo{
						Email: c.Email,
					})
				}
			}
		}
	}

	return clients
}

// loadTrafficSnapshot 从文件加载流量快照。
func (a *Agent) loadTrafficSnapshot() {
	snapshotPath := filepath.Join(filepath.Dir(a.cfg.XrayConfigPath), "traffic_snapshot.json")

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[agent] load traffic snapshot error: %v", err)
		}
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	var items []TrafficItem
	if err := json.Unmarshal(data, &items); err != nil {
		log.Printf("[agent] parse traffic snapshot error: %v", err)
		return
	}

	for _, item := range items {
		a.traffic[item.XrayUserKey] = &TrafficItem{
			XrayUserKey:   item.XrayUserKey,
			UplinkTotal:   item.UplinkTotal,
			DownlinkTotal: item.DownlinkTotal,
		}
	}

	log.Printf("[agent] loaded traffic snapshot: %d users", len(a.traffic))
}

// saveTrafficSnapshot 保存流量快照到文件。
func (a *Agent) saveTrafficSnapshot() {
	a.mu.RLock()
	defer a.mu.RUnlock()

	items := make([]TrafficItem, 0, len(a.traffic))
	for _, item := range a.traffic {
		items = append(items, *item)
	}

	snapshotPath := filepath.Join(filepath.Dir(a.cfg.XrayConfigPath), "traffic_snapshot.json")

	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		log.Printf("[agent] marshal traffic snapshot error: %v", err)
		return
	}

	if err := os.WriteFile(snapshotPath, data, 0644); err != nil {
		log.Printf("[agent] save traffic snapshot error: %v", err)
	}
}

func (a *Agent) loadTrafficQueue() {
	queuePath := a.trafficQueuePath()
	data, err := os.ReadFile(queuePath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[agent] load traffic queue error: %v", err)
		}
		return
	}

	var batches []TrafficReportBatch
	if err := json.Unmarshal(data, &batches); err != nil {
		log.Printf("[agent] parse traffic queue error: %v", err)
		return
	}

	a.queueMu.Lock()
	a.trafficQueue = batches
	a.queueMu.Unlock()

	if len(batches) > 0 {
		log.Printf("[agent] loaded traffic queue: %d batches", len(batches))
	}
}

func (a *Agent) saveTrafficQueue() {
	a.queueMu.Lock()
	defer a.queueMu.Unlock()
	a.saveTrafficQueueLocked()
}

func (a *Agent) saveTrafficQueueLocked() {
	queuePath := a.trafficQueuePath()
	if len(a.trafficQueue) == 0 {
		if err := os.Remove(queuePath); err != nil && !os.IsNotExist(err) {
			log.Printf("[agent] remove traffic queue error: %v", err)
		}
		return
	}

	data, err := json.MarshalIndent(a.trafficQueue, "", "  ")
	if err != nil {
		log.Printf("[agent] marshal traffic queue error: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(queuePath), 0755); err != nil {
		log.Printf("[agent] create traffic queue dir error: %v", err)
		return
	}
	if err := os.WriteFile(queuePath, data, 0644); err != nil {
		log.Printf("[agent] save traffic queue error: %v", err)
	}
}

// postJSON 发送 POST JSON 请求。
func (a *Agent) postJSON(ctx context.Context, path string, reqBody interface{}, respBody interface{}) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := a.cfg.CenterServerURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	if respBody != nil {
		return json.NewDecoder(resp.Body).Decode(respBody)
	}

	return nil
}

func main() {
	cfg := loadConfig()
	agent := NewAgent(cfg)
	agent.Run()
}
