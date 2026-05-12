// Package model 定义数据库模型（GORM）。
//
// 每个模型对应一张数据库表，通过 golang-migrate 创建实际表结构。
// GORM 模型仅用于代码层面的查询和写入，不替代 migration。
package model

import (
	"encoding/json"
	"math"
	"net/url"
	"strings"
	"time"
)

const (
	TrafficPoolNormal      = "normal"
	TrafficPoolResidential = "residential"
	NodeOutboundDirect     = "direct"
	NodeOutboundSocks5     = "socks5"
	NodeHealthHealthy      = "healthy"
	NodeHealthDegraded     = "degraded"
	NodeHealthDown         = "down"
	NodeHealthDisabled     = "disabled"
	NodeHealthUnchecked    = "unchecked"
)

func NormalizeTrafficPool(pool string) string {
	switch strings.ToLower(strings.TrimSpace(pool)) {
	case TrafficPoolResidential:
		return TrafficPoolResidential
	default:
		return TrafficPoolNormal
	}
}

func TrafficPoolDisplayName(pool string) string {
	if NormalizeTrafficPool(pool) == TrafficPoolResidential {
		return "家宽流量"
	}
	return "普通流量"
}

func PlanTrafficLimitByPool(plan *Plan, pool string) uint64 {
	if plan == nil {
		return 0
	}
	if NormalizeTrafficPool(pool) == TrafficPoolResidential {
		return plan.ResidentialTrafficLimit
	}
	return plan.TrafficLimit
}

func NormalizeTrafficMultiplier(multiplier float64) float64 {
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return 1
	}
	return multiplier
}

func PlanTrafficMultiplierByPool(plan *Plan, pool string) float64 {
	if plan == nil {
		return 1
	}
	if NormalizeTrafficPool(pool) == TrafficPoolResidential {
		return NormalizeTrafficMultiplier(plan.ResidentialTrafficMultiplier)
	}
	return NormalizeTrafficMultiplier(plan.NormalTrafficMultiplier)
}

func SubscriptionTrafficLimitByPool(sub *UserSubscription, pool string) uint64 {
	if sub == nil {
		return 0
	}
	if NormalizeTrafficPool(pool) == TrafficPoolResidential {
		return sub.ResidentialTrafficLimit
	}
	return sub.TrafficLimit
}

func SubscriptionUsedTrafficByPool(sub *UserSubscription, pool string) uint64 {
	if sub == nil {
		return 0
	}
	if NormalizeTrafficPool(pool) == TrafficPoolResidential {
		return sub.ResidentialUsedTraffic
	}
	return sub.UsedTraffic
}

func SubscriptionTrafficUnlimitedByPool(sub *UserSubscription, pool string) bool {
	if sub == nil {
		return false
	}
	return NormalizeTrafficPool(pool) == TrafficPoolNormal && sub.TrafficLimit == 0
}

func SubscriptionRemainingTrafficByPool(sub *UserSubscription, pool string) uint64 {
	if sub == nil {
		return 0
	}
	if SubscriptionTrafficUnlimitedByPool(sub, pool) {
		return 0
	}
	limit := SubscriptionTrafficLimitByPool(sub, pool)
	used := SubscriptionUsedTrafficByPool(sub, pool)
	if used >= limit {
		return 0
	}
	return limit - used
}

func SubscriptionTrafficAvailableByPool(sub *UserSubscription, pool string) bool {
	if sub == nil {
		return false
	}
	if SubscriptionTrafficUnlimitedByPool(sub, pool) {
		return true
	}
	limit := SubscriptionTrafficLimitByPool(sub, pool)
	used := SubscriptionUsedTrafficByPool(sub, pool)
	return limit > 0 && used < limit
}

// User 用户模型。
type User struct {
	ID           uint64     `gorm:"primaryKey;column:id" json:"id"`
	UUID         string     `gorm:"column:uuid;type:varchar(36);uniqueIndex" json:"uuid"`
	Username     string     `gorm:"column:username;type:varchar(64);uniqueIndex" json:"username"`
	PasswordHash string     `gorm:"column:password_hash;type:varchar(255)" json:"-"`
	Email        *string    `gorm:"column:email;type:varchar(255)" json:"email,omitempty"`
	XrayUserKey  string     `gorm:"column:xray_user_key;type:varchar(255);uniqueIndex" json:"-"`
	Status       string     `gorm:"column:status;type:varchar(16);default:active" json:"status"`
	IsAdmin      bool       `gorm:"column:is_admin" json:"is_admin"`
	LastLoginAt  *time.Time `gorm:"column:last_login_at" json:"last_login_at,omitempty"`
	LastLoginIP  *string    `gorm:"column:last_login_ip;type:varchar(45)" json:"-"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (User) TableName() string {
	return "users"
}

// CreateUserRequest 创建用户请求（注册用）。
type CreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Email    string `json:"email" binding:"omitempty,email"`
	Password string `json:"password" binding:"required,min=6"`
}

// AdminCreateUserRequest 管理员创建用户请求。
type AdminCreateUserRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Email    string `json:"email" binding:"omitempty,email"`
	Password string `json:"password" binding:"required,min=6"`
	Status   string `json:"status" binding:"omitempty,oneof=active disabled"`
	IsAdmin  bool   `json:"is_admin"`
}

// LoginRequest 登录请求。
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse 登录响应。
type LoginResponse struct {
	AccessToken string      `json:"accessToken"`
	User        *UserPublic `json:"user"`
}

// UserPublic 对外暴露的用户信息（脱敏）。
type UserPublic struct {
	ID          uint64     `json:"id"`
	UUID        string     `json:"uuid"`
	Username    string     `json:"username"`
	Email       *string    `json:"email,omitempty"`
	Status      string     `json:"status"`
	IsAdmin     bool       `json:"is_admin"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ToPublic 转为对外暴露的用户信息。
func (u *User) ToPublic() *UserPublic {
	return &UserPublic{
		ID:          u.ID,
		UUID:        u.UUID,
		Username:    u.Username,
		Email:       u.Email,
		Status:      u.Status,
		IsAdmin:     u.IsAdmin,
		LastLoginAt: u.LastLoginAt,
		CreatedAt:   u.CreatedAt,
	}
}

// UserSubscription 用户订阅模型。
type UserSubscription struct {
	ID                      uint64    `gorm:"primaryKey;column:id" json:"id"`
	UserID                  uint64    `gorm:"column:user_id;index" json:"user_id"`
	PlanID                  uint64    `gorm:"column:plan_id" json:"plan_id"`
	StartDate               time.Time `gorm:"column:start_date" json:"start_date"`
	ExpireDate              time.Time `gorm:"column:expire_date;index" json:"expire_date"`
	TrafficLimit            uint64    `gorm:"column:traffic_limit" json:"traffic_limit"`
	UsedTraffic             uint64    `gorm:"column:used_traffic" json:"used_traffic"`
	ResidentialTrafficLimit uint64    `gorm:"column:residential_traffic_limit" json:"residential_traffic_limit"`
	ResidentialUsedTraffic  uint64    `gorm:"column:residential_used_traffic" json:"residential_used_traffic"`
	SpeedLimitBps           uint64    `gorm:"column:speed_limit_bps" json:"speed_limit_bps"`
	Status                  string    `gorm:"column:status;type:varchar(16);index" json:"status"`
	ActiveUserID            *uint64   `gorm:"column:active_user_id;uniqueIndex" json:"-"`
	CreatedAt               time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt               time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (UserSubscription) TableName() string {
	return "user_subscriptions"
}

// SubscriptionToken 订阅 Token 模型。
type SubscriptionToken struct {
	ID             uint64     `gorm:"primaryKey;column:id" json:"id"`
	UserID         uint64     `gorm:"column:user_id;uniqueIndex:uk_subscription_tokens_user_id" json:"user_id"`
	SubscriptionID *uint64    `gorm:"column:subscription_id;index" json:"subscription_id,omitempty"`
	Token          string     `gorm:"column:token;type:varchar(128);uniqueIndex" json:"token"`
	IsRevoked      bool       `gorm:"column:is_revoked" json:"is_revoked"`
	LastUsedAt     *time.Time `gorm:"column:last_used_at" json:"last_used_at,omitempty"`
	ExpiresAt      *time.Time `gorm:"column:expires_at" json:"expires_at,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 指定表名。
func (SubscriptionToken) TableName() string {
	return "subscription_tokens"
}

// RefreshToken 存储在服务端的 Refresh Token 记录（可选，用于黑名单/吊销）。
type RefreshToken struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	UserID    uint64    `gorm:"column:user_id;index"`
	TokenHash string    `gorm:"column:token_hash;type:varchar(255);uniqueIndex"`
	ExpiresAt time.Time `gorm:"column:expires_at"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName 指定表名。
func (RefreshToken) TableName() string {
	return "refresh_tokens"
}

// Plan 套餐模型。
type Plan struct {
	ID                           uint64    `gorm:"primaryKey;column:id" json:"id"`
	Name                         string    `gorm:"column:name;type:varchar(128)" json:"name"`
	Price                        float64   `gorm:"column:price;type:decimal(10,2)" json:"price"`
	Currency                     string    `gorm:"column:currency;type:varchar(8);default:USDT" json:"currency"`
	TrafficLimit                 uint64    `gorm:"column:traffic_limit" json:"traffic_limit"`
	ResidentialTrafficLimit      uint64    `gorm:"column:residential_traffic_limit" json:"residential_traffic_limit"`
	NormalTrafficMultiplier      float64   `gorm:"column:normal_traffic_multiplier;type:decimal(8,3);default:1" json:"normal_traffic_multiplier"`
	ResidentialTrafficMultiplier float64   `gorm:"column:residential_traffic_multiplier;type:decimal(8,3);default:1" json:"residential_traffic_multiplier"`
	DurationDays                 uint32    `gorm:"column:duration_days" json:"duration_days"`
	SortWeight                   int       `gorm:"column:sort_weight;default:0" json:"sort_weight"`
	IsActive                     bool      `gorm:"column:is_active;default:true;index" json:"is_active"`
	IsDefault                    bool      `gorm:"column:is_default;default:false;index" json:"is_default"`
	IsDeleted                    bool      `gorm:"column:is_deleted;default:false;index" json:"is_deleted"`
	CreatedAt                    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt                    time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (Plan) TableName() string {
	return "plans"
}

// CreatePlanRequest 创建套餐请求。
type CreatePlanRequest struct {
	Name                         string  `json:"name" binding:"required"`
	Price                        float64 `json:"price" binding:"required,min=0"`
	Currency                     string  `json:"currency" default:"USDT"`
	TrafficLimit                 uint64  `json:"traffic_limit" binding:"min=0"`
	ResidentialTrafficLimit      uint64  `json:"residential_traffic_limit" binding:"min=0"`
	NormalTrafficMultiplier      float64 `json:"normal_traffic_multiplier" binding:"omitempty,min=0.001"`
	ResidentialTrafficMultiplier float64 `json:"residential_traffic_multiplier" binding:"omitempty,min=0.001"`
	DurationDays                 uint32  `json:"duration_days" binding:"min=0"`
	SortWeight                   int     `json:"sort_weight"`
	IsActive                     bool    `json:"is_active"`
}

// UpdatePlanRequest 更新套餐请求。
type UpdatePlanRequest struct {
	Name                         string  `json:"name" binding:"required"`
	Price                        float64 `json:"price" binding:"required,min=0"`
	Currency                     string  `json:"currency"`
	TrafficLimit                 uint64  `json:"traffic_limit" binding:"min=0"`
	ResidentialTrafficLimit      uint64  `json:"residential_traffic_limit" binding:"min=0"`
	NormalTrafficMultiplier      float64 `json:"normal_traffic_multiplier" binding:"omitempty,min=0.001"`
	ResidentialTrafficMultiplier float64 `json:"residential_traffic_multiplier" binding:"omitempty,min=0.001"`
	DurationDays                 uint32  `json:"duration_days" binding:"min=0"`
	SortWeight                   int     `json:"sort_weight"`
	IsActive                     bool    `json:"is_active"`
}

// NodeGroup 节点分组模型。
type NodeGroup struct {
	ID          uint64    `gorm:"primaryKey;column:id" json:"id"`
	Name        string    `gorm:"column:name;type:varchar(128)" json:"name"`
	Description *string   `gorm:"column:description;type:text" json:"description,omitempty"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (NodeGroup) TableName() string {
	return "node_groups"
}

// NodeGroupNode 节点与节点分组多对多关联模型。
type NodeGroupNode struct {
	ID          uint64    `gorm:"primaryKey;column:id" json:"id"`
	NodeID      uint64    `gorm:"column:node_id;index;uniqueIndex:uk_node_group_node" json:"node_id"`
	NodeGroupID uint64    `gorm:"column:node_group_id;index;uniqueIndex:uk_node_group_node" json:"node_group_id"`
	CreatedAt   time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 指定表名。
func (NodeGroupNode) TableName() string {
	return "node_group_nodes"
}

// CreateNodeGroupRequest 创建节点分组请求。
type CreateNodeGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// UpdateNodeGroupRequest 更新节点分组请求。
type UpdateNodeGroupRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// BindNodeGroupNodesRequest 绑定节点分组与节点请求。
type BindNodeGroupNodesRequest struct {
	NodeIDs []uint64 `json:"node_ids" binding:"required"`
}

// NodeHost 物理节点服务器模型。
//
// 一台物理服务器只运行一个 node-agent；多出口 IP 场景下，
// 同一个 NodeHost 可以关联多个逻辑出口节点 Node。
type NodeHost struct {
	ID              uint64     `gorm:"primaryKey;column:id" json:"id"`
	Name            string     `gorm:"column:name;type:varchar(128)" json:"name"`
	SSHHost         string     `gorm:"column:ssh_host;type:varchar(255)" json:"ssh_host"`
	SSHPort         uint32     `gorm:"column:ssh_port;default:22" json:"ssh_port"`
	AgentBaseURL    string     `gorm:"column:agent_base_url;type:varchar(255)" json:"agent_base_url"`
	AgentToken      string     `gorm:"-" json:"-"`
	AgentTokenHash  string     `gorm:"column:agent_token_hash;type:varchar(255)" json:"-"`
	AgentVersion    *string    `gorm:"column:agent_version;type:varchar(32)" json:"agent_version,omitempty"`
	LastHeartbeatAt *time.Time `gorm:"column:last_heartbeat_at" json:"last_heartbeat_at,omitempty"`
	IsEnabled       bool       `gorm:"column:is_enabled;default:true;index" json:"is_enabled"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (NodeHost) TableName() string {
	return "node_hosts"
}

// Node 节点模型。
type Node struct {
	ID                   uint64     `gorm:"primaryKey;column:id" json:"id"`
	Name                 string     `gorm:"column:name;type:varchar(128)" json:"name"`
	Protocol             string     `gorm:"column:protocol;type:varchar(32);default:vless" json:"protocol"`
	Transport            string     `gorm:"column:transport;type:varchar(32);default:tcp" json:"transport"`
	TrafficPool          string     `gorm:"column:traffic_pool;type:varchar(32);default:normal" json:"traffic_pool"`
	OutboundType         string     `gorm:"column:outbound_type;type:varchar(32);default:direct" json:"outbound_type"`
	Host                 string     `gorm:"column:host;type:varchar(255)" json:"host"`
	Port                 uint32     `gorm:"column:port;default:443" json:"port"`
	ServerName           string     `gorm:"column:server_name;type:varchar(255)" json:"server_name"`
	PublicKey            string     `gorm:"column:public_key;type:varchar(255)" json:"public_key"`
	ShortID              string     `gorm:"column:short_id;type:varchar(32)" json:"short_id"`
	Fingerprint          string     `gorm:"column:fingerprint;type:varchar(32);default:chrome" json:"fingerprint"`
	Flow                 string     `gorm:"column:flow;type:varchar(32);default:xtls-rprx-vision" json:"flow"`
	UDPEnabled           bool       `gorm:"column:udp_enabled" json:"udp_enabled"`
	LineMode             string     `gorm:"column:line_mode;type:varchar(32);default:direct_and_relay" json:"line_mode"`
	XHTTPPath            string     `gorm:"column:xhttp_path;type:varchar(255)" json:"xhttp_path"`
	XHTTPHost            string     `gorm:"column:xhttp_host;type:varchar(255)" json:"xhttp_host"`
	XHTTPMode            string     `gorm:"column:xhttp_mode;type:varchar(32);default:auto" json:"xhttp_mode"`
	NodeHostID           *uint64    `gorm:"column:node_host_id;index" json:"node_host_id,omitempty"`
	ListenIP             string     `gorm:"column:listen_ip;type:varchar(45)" json:"listen_ip"`
	OutboundIP           string     `gorm:"column:outbound_ip;type:varchar(45)" json:"outbound_ip"`
	OutboundProxyURL     *string    `gorm:"column:outbound_proxy_url;type:text" json:"outbound_proxy_url,omitempty"`
	XrayInboundTag       string     `gorm:"column:xray_inbound_tag;type:varchar(64)" json:"xray_inbound_tag"`
	XrayOutboundTag      string     `gorm:"column:xray_outbound_tag;type:varchar(64)" json:"xray_outbound_tag"`
	NodeGroupID          *uint64    `gorm:"column:node_group_id;index" json:"node_group_id"`
	AgentBaseURL         string     `gorm:"column:agent_base_url;type:varchar(255)" json:"agent_base_url"`
	AgentToken           string     `gorm:"-" json:"-"`
	AgentTokenHash       string     `gorm:"column:agent_token_hash;type:varchar(255)" json:"-"`
	AgentVersion         *string    `gorm:"column:agent_version;type:varchar(32)" json:"agent_version,omitempty"`
	LastHeartbeatAt      *time.Time `gorm:"column:last_heartbeat_at" json:"last_heartbeat_at,omitempty"`
	LastTrafficReportAt  *time.Time `gorm:"column:last_traffic_report_at" json:"last_traffic_report_at,omitempty"`
	LastTrafficSuccessAt *time.Time `gorm:"column:last_traffic_success_at" json:"last_traffic_success_at,omitempty"`
	LastTrafficErrorAt   *time.Time `gorm:"column:last_traffic_error_at" json:"last_traffic_error_at,omitempty"`
	TrafficErrorCount    uint32     `gorm:"column:traffic_error_count" json:"traffic_error_count"`
	LastTrafficError     *string    `gorm:"column:last_traffic_error;type:text" json:"last_traffic_error,omitempty"`
	IsEnabled            bool       `gorm:"column:is_enabled;default:true;index" json:"is_enabled"`
	SortWeight           int        `gorm:"column:sort_weight;default:0" json:"sort_weight"`
	CreatedAt            time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (Node) TableName() string {
	return "nodes"
}

// NodeRuntimeMetric 节点运行指标。
type NodeRuntimeMetric struct {
	ID                 uint64    `gorm:"primaryKey;column:id" json:"id"`
	NodeID             *uint64   `gorm:"column:node_id;index" json:"node_id,omitempty"`
	NodeHostID         *uint64   `gorm:"column:node_host_id;index" json:"node_host_id,omitempty"`
	CPUUsagePercent    float64   `gorm:"column:cpu_usage_percent;type:decimal(5,2)" json:"cpu_usage_percent"`
	MemoryUsagePercent float64   `gorm:"column:memory_usage_percent;type:decimal(5,2)" json:"memory_usage_percent"`
	DiskUsagePercent   float64   `gorm:"column:disk_usage_percent;type:decimal(5,2)" json:"disk_usage_percent"`
	Load1              float64   `gorm:"column:load1;type:decimal(8,2)" json:"load1"`
	Load5              float64   `gorm:"column:load5;type:decimal(8,2)" json:"load5"`
	Load15             float64   `gorm:"column:load15;type:decimal(8,2)" json:"load15"`
	TCPConnections     uint32    `gorm:"column:tcp_connections" json:"tcp_connections"`
	XrayRunning        bool      `gorm:"column:xray_running" json:"xray_running"`
	XrayUptimeSeconds  *uint64   `gorm:"column:xray_uptime_seconds" json:"xray_uptime_seconds,omitempty"`
	ObservedAt         time.Time `gorm:"column:observed_at;index" json:"observed_at"`
	CreatedAt          time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (NodeRuntimeMetric) TableName() string {
	return "node_runtime_metrics"
}

// NodeHealthCheck 节点健康检查结果。
type NodeHealthCheck struct {
	ID            uint64    `gorm:"primaryKey;column:id" json:"id"`
	NodeID        uint64    `gorm:"column:node_id;index" json:"node_id"`
	Status        string    `gorm:"column:status;type:varchar(16);index" json:"status"`
	HealthScore   int       `gorm:"column:health_score" json:"health_score"`
	ReasonCode    string    `gorm:"column:reason_code;type:varchar(64)" json:"reason_code"`
	ReasonMessage string    `gorm:"column:reason_message;type:varchar(255)" json:"reason_message"`
	TCPLatencyMS  *int      `gorm:"column:tcp_latency_ms" json:"tcp_latency_ms,omitempty"`
	TCPReachable  bool      `gorm:"column:tcp_reachable" json:"tcp_reachable"`
	HeartbeatOK   bool      `gorm:"column:heartbeat_ok" json:"heartbeat_ok"`
	TrafficOK     bool      `gorm:"column:traffic_ok" json:"traffic_ok"`
	LoadOK        bool      `gorm:"column:load_ok" json:"load_ok"`
	CheckedAt     time.Time `gorm:"column:checked_at;index" json:"checked_at"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (NodeHealthCheck) TableName() string {
	return "node_health_checks"
}

// CreateNodeRequest 创建节点请求。
type CreateNodeRequest struct {
	Name             string   `json:"name" binding:"required"`
	Protocol         string   `json:"protocol" default:"vless"`
	Transport        string   `json:"transport" binding:"omitempty,oneof=tcp xhttp"`
	Transports       []string `json:"transports" binding:"omitempty,dive,oneof=tcp xhttp"`
	TrafficPool      string   `json:"traffic_pool" binding:"omitempty,oneof=normal residential"`
	OutboundType     string   `json:"outbound_type" binding:"omitempty,oneof=direct socks5"`
	UDPEnabled       *bool    `json:"udp_enabled"`
	Host             string   `json:"host" binding:"required"`
	Port             uint32   `json:"port" default:"443"`
	TCPPort          uint32   `json:"tcp_port"`
	XHTTPPort        uint32   `json:"xhttp_port"`
	ServerName       string   `json:"server_name"`
	PublicKey        string   `json:"public_key"`
	ShortID          string   `json:"short_id"`
	Fingerprint      string   `json:"fingerprint" default:"chrome"`
	Flow             string   `json:"flow" default:"xtls-rprx-vision"`
	LineMode         string   `json:"line_mode" binding:"omitempty,oneof=direct_only relay_only direct_and_relay"`
	NodeHostID       *uint64  `json:"node_host_id"`
	XHTTPPath        string   `json:"xhttp_path"`
	XHTTPHost        string   `json:"xhttp_host"`
	XHTTPMode        string   `json:"xhttp_mode" binding:"omitempty,oneof=auto packet-up stream-up stream-one"`
	OutboundIP       string   `json:"outbound_ip"`
	OutboundProxyURL string   `json:"outbound_proxy_url"`
	AgentBaseURL     string   `json:"agent_base_url"`
	AgentToken       string   `json:"agent_token"`
	SortWeight       int      `json:"sort_weight"`
	IsEnabled        bool     `json:"is_enabled"`
}

// UpdateNodeRequest 更新节点请求。
type UpdateNodeRequest struct {
	Name             string `json:"name" binding:"required"`
	Protocol         string `json:"protocol"`
	Transport        string `json:"transport" binding:"omitempty,oneof=tcp xhttp"`
	TrafficPool      string `json:"traffic_pool" binding:"omitempty,oneof=normal residential"`
	OutboundType     string `json:"outbound_type" binding:"omitempty,oneof=direct socks5"`
	UDPEnabled       *bool  `json:"udp_enabled"`
	Host             string `json:"host" binding:"required"`
	Port             uint32 `json:"port"`
	ServerName       string `json:"server_name"`
	PublicKey        string `json:"public_key"`
	ShortID          string `json:"short_id"`
	Fingerprint      string `json:"fingerprint"`
	Flow             string `json:"flow"`
	LineMode         string `json:"line_mode" binding:"omitempty,oneof=direct_only relay_only direct_and_relay"`
	XHTTPPath        string `json:"xhttp_path"`
	XHTTPHost        string `json:"xhttp_host"`
	XHTTPMode        string `json:"xhttp_mode" binding:"omitempty,oneof=auto packet-up stream-up stream-one"`
	OutboundIP       string `json:"outbound_ip"`
	OutboundProxyURL string `json:"outbound_proxy_url"`
	AgentBaseURL     string `json:"agent_base_url" binding:"required"`
	AgentToken       string `json:"agent_token"`
	SortWeight       int    `json:"sort_weight"`
	IsEnabled        bool   `json:"is_enabled"`
}

// Relay 中转节点模型。
type Relay struct {
	ID              uint64     `gorm:"primaryKey;column:id" json:"id"`
	Name            string     `gorm:"column:name;type:varchar(128)" json:"name"`
	Host            string     `gorm:"column:host;type:varchar(255)" json:"host"`
	ForwarderType   string     `gorm:"column:forwarder_type;type:varchar(32);default:haproxy" json:"forwarder_type"`
	AgentBaseURL    string     `gorm:"column:agent_base_url;type:varchar(255)" json:"agent_base_url"`
	AgentToken      string     `gorm:"-" json:"-"`
	AgentTokenHash  string     `gorm:"column:agent_token_hash;type:varchar(255)" json:"-"`
	AgentVersion    *string    `gorm:"column:agent_version;type:varchar(32)" json:"agent_version,omitempty"`
	Status          string     `gorm:"column:status;type:varchar(16);default:offline" json:"status"`
	LastHeartbeatAt *time.Time `gorm:"column:last_heartbeat_at" json:"last_heartbeat_at,omitempty"`
	IsEnabled       bool       `gorm:"column:is_enabled;default:true;index" json:"is_enabled"`
	CreatedAt       time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (Relay) TableName() string {
	return "relays"
}

// RelayBackend 中转后端绑定模型。
type RelayBackend struct {
	ID         uint64    `gorm:"primaryKey;column:id" json:"id"`
	RelayID    uint64    `gorm:"column:relay_id;index;uniqueIndex:uk_relay_listen_port" json:"relay_id"`
	ExitNodeID uint64    `gorm:"column:exit_node_id;index" json:"exit_node_id"`
	Name       string    `gorm:"column:name;type:varchar(128)" json:"name"`
	ListenPort uint32    `gorm:"column:listen_port;uniqueIndex:uk_relay_listen_port" json:"listen_port"`
	TargetHost string    `gorm:"column:target_host;type:varchar(255)" json:"target_host"`
	TargetPort uint32    `gorm:"column:target_port" json:"target_port"`
	IsEnabled  bool      `gorm:"column:is_enabled;default:true;index" json:"is_enabled"`
	SortWeight int       `gorm:"column:sort_weight;default:0" json:"sort_weight"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
	Relay      *Relay    `gorm:"foreignKey:RelayID" json:"relay,omitempty"`
	ExitNode   *Node     `gorm:"foreignKey:ExitNodeID" json:"exit_node,omitempty"`
}

// TableName 指定表名。
func (RelayBackend) TableName() string {
	return "relay_backends"
}

// RelayConfigTask 中转配置同步任务模型。
type RelayConfigTask struct {
	ID             uint64     `gorm:"primaryKey;column:id" json:"id"`
	RelayID        uint64     `gorm:"column:relay_id;index" json:"relay_id"`
	Action         string     `gorm:"column:action;type:varchar(32)" json:"action"`
	Payload        *string    `gorm:"column:payload;type:json" json:"payload,omitempty"`
	Status         string     `gorm:"column:status;type:varchar(16);index" json:"status"`
	RetryCount     uint32     `gorm:"column:retry_count;default:0" json:"retry_count"`
	LastError      *string    `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	ScheduledAt    time.Time  `gorm:"column:scheduled_at;index" json:"scheduled_at"`
	LockedAt       *time.Time `gorm:"column:locked_at;index" json:"locked_at,omitempty"`
	LockToken      *string    `gorm:"column:lock_token;type:varchar(64)" json:"lock_token,omitempty"`
	ExecutedAt     *time.Time `gorm:"column:executed_at" json:"executed_at,omitempty"`
	IdempotencyKey string     `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex" json:"idempotency_key"`
}

// TableName 指定表名。
func (RelayConfigTask) TableName() string {
	return "relay_config_tasks"
}

// RelayTrafficSnapshot 中转线路级流量快照。
type RelayTrafficSnapshot struct {
	ID             uint64    `gorm:"primaryKey;column:id" json:"id"`
	RelayID        uint64    `gorm:"column:relay_id;index" json:"relay_id"`
	RelayBackendID *uint64   `gorm:"column:relay_backend_id;index" json:"relay_backend_id,omitempty"`
	ListenPort     uint32    `gorm:"column:listen_port" json:"listen_port"`
	BytesInTotal   uint64    `gorm:"column:bytes_in_total" json:"bytes_in_total"`
	BytesOutTotal  uint64    `gorm:"column:bytes_out_total" json:"bytes_out_total"`
	CapturedAt     time.Time `gorm:"column:captured_at;index" json:"captured_at"`
}

// TableName 指定表名。
func (RelayTrafficSnapshot) TableName() string {
	return "relay_traffic_snapshots"
}

// CreateRelayRequest 创建中转节点请求。
type CreateRelayRequest struct {
	Name          string `json:"name" binding:"required"`
	Host          string `json:"host" binding:"required"`
	ForwarderType string `json:"forwarder_type" binding:"omitempty,oneof=haproxy"`
	AgentBaseURL  string `json:"agent_base_url" binding:"required"`
	AgentToken    string `json:"agent_token" binding:"required"`
	IsEnabled     bool   `json:"is_enabled"`
}

// UpdateRelayRequest 更新中转节点请求。
type UpdateRelayRequest struct {
	Name          string `json:"name" binding:"required"`
	Host          string `json:"host" binding:"required"`
	ForwarderType string `json:"forwarder_type" binding:"omitempty,oneof=haproxy"`
	AgentBaseURL  string `json:"agent_base_url" binding:"required"`
	AgentToken    string `json:"agent_token"`
	IsEnabled     bool   `json:"is_enabled"`
}

// RelayBackendRequest 保存中转后端绑定请求。
type RelayBackendRequest struct {
	ID         uint64 `json:"id"`
	ExitNodeID uint64 `json:"exit_node_id" binding:"required"`
	Name       string `json:"name"`
	ListenPort uint32 `json:"listen_port" binding:"required,min=1,max=65535"`
	TargetHost string `json:"target_host"`
	TargetPort uint32 `json:"target_port" binding:"omitempty,min=1,max=65535"`
	IsEnabled  bool   `json:"is_enabled"`
	SortWeight int    `json:"sort_weight"`
}

// SaveRelayBackendsRequest 保存中转后端绑定列表请求。
type SaveRelayBackendsRequest struct {
	Backends []RelayBackendRequest `json:"backends"`
}

// NodeAccessTask 节点访问同步任务模型。
type NodeAccessTask struct {
	ID             uint64     `gorm:"primaryKey;column:id" json:"id"`
	NodeID         uint64     `gorm:"column:node_id;index" json:"node_id"`
	SubscriptionID *uint64    `gorm:"column:subscription_id;index" json:"subscription_id"`
	Action         string     `gorm:"column:action;type:varchar(32)" json:"action"`
	Payload        *string    `gorm:"column:payload;type:json" json:"payload,omitempty"`
	Status         string     `gorm:"column:status;type:varchar(16);index" json:"status"`
	RetryCount     uint32     `gorm:"column:retry_count;default:0" json:"retry_count"`
	LastError      *string    `gorm:"column:last_error;type:text" json:"last_error,omitempty"`
	ScheduledAt    time.Time  `gorm:"column:scheduled_at;index" json:"scheduled_at"`
	LockedAt       *time.Time `gorm:"column:locked_at;index" json:"locked_at,omitempty"`
	LockToken      *string    `gorm:"column:lock_token;type:varchar(64)" json:"lock_token,omitempty"`
	ExecutedAt     *time.Time `gorm:"column:executed_at" json:"executed_at,omitempty"`
	IdempotencyKey string     `gorm:"column:idempotency_key;type:varchar(128);uniqueIndex" json:"idempotency_key"`
}

// TableName 指定表名。
func (NodeAccessTask) TableName() string {
	return "node_access_tasks"
}

// RedeemCode 兑换码模型。
type RedeemCode struct {
	ID           uint64     `gorm:"primaryKey;column:id" json:"id"`
	Code         string     `gorm:"column:code;type:varchar(64);uniqueIndex" json:"code"`
	PlanID       uint64     `gorm:"column:plan_id" json:"plan_id"`
	DurationDays uint32     `gorm:"column:duration_days" json:"duration_days"`
	IsUsed       bool       `gorm:"column:is_used;default:false;index" json:"is_used"`
	UsedByUserID *uint64    `gorm:"column:used_by_user_id" json:"used_by_user_id,omitempty"`
	UsedAt       *time.Time `gorm:"column:used_at" json:"used_at,omitempty"`
	ExpiresAt    *time.Time `gorm:"column:expires_at" json:"expires_at,omitempty"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 指定表名。
func (RedeemCode) TableName() string {
	return "redeem_codes"
}

// Order 订单模型。
type Order struct {
	ID            uint64     `gorm:"primaryKey;column:id" json:"id"`
	OrderNo       string     `gorm:"column:order_no;type:varchar(64);uniqueIndex" json:"order_no"`
	UserID        uint64     `gorm:"column:user_id;index" json:"user_id"`
	PlanID        uint64     `gorm:"column:plan_id" json:"plan_id"`
	Amount        float64    `gorm:"column:amount;type:decimal(10,2)" json:"amount"`
	Currency      string     `gorm:"column:currency;type:varchar(8);default:USDT" json:"currency"`
	PayAddress    string     `gorm:"column:pay_address;type:varchar(255)" json:"pay_address"`
	ExpectedChain string     `gorm:"column:expected_chain;type:varchar(32);default:TRC20" json:"expected_chain"`
	Status        string     `gorm:"column:status;type:varchar(16);default:PENDING;index" json:"status"`
	PaidAt        *time.Time `gorm:"column:paid_at" json:"paid_at,omitempty"`
	ExpiredAt     *time.Time `gorm:"column:expired_at" json:"expired_at,omitempty"`
	CreatedAt     time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (Order) TableName() string {
	return "orders"
}

// PaymentRecord 支付记录模型（v1 保留骨架）。
type PaymentRecord struct {
	ID          uint64     `gorm:"primaryKey;column:id" json:"id"`
	OrderID     uint64     `gorm:"column:order_id;index" json:"order_id"`
	UserID      uint64     `gorm:"column:user_id;index" json:"user_id"`
	TxID        string     `gorm:"column:tx_id;type:varchar(255);uniqueIndex" json:"tx_id"`
	Chain       string     `gorm:"column:chain;type:varchar(32);default:TRC20" json:"chain"`
	Amount      float64    `gorm:"column:amount;type:decimal(18,6)" json:"amount"`
	FromAddress string     `gorm:"column:from_address;type:varchar(255)" json:"from_address"`
	ToAddress   string     `gorm:"column:to_address;type:varchar(255)" json:"to_address"`
	RawPayload  *string    `gorm:"column:raw_payload;type:json" json:"raw_payload,omitempty"`
	Status      string     `gorm:"column:status;type:varchar(16);default:PENDING" json:"status"`
	ConfirmedAt *time.Time `gorm:"column:confirmed_at" json:"confirmed_at,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 指定表名。
func (PaymentRecord) TableName() string {
	return "payment_records"
}

// SiteSetting 站点级配置。
type SiteSetting struct {
	ID        uint64    `gorm:"primaryKey;column:id" json:"id"`
	Key       string    `gorm:"column:setting_key;type:varchar(128);uniqueIndex" json:"key"`
	Value     string    `gorm:"column:setting_value;type:json" json:"value"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

func (SiteSetting) TableName() string {
	return "site_settings"
}

const (
	SiteSettingSalesLanding       = "sales_landing"
	SiteSettingSubscriptionConfig = "subscription_config"
)

// SubscriptionConfig 订阅输出配置。
type SubscriptionConfig struct {
	ProfileName           string   `json:"profile_name"`
	CustomRules           []string `json:"custom_rules"`
	IncludeUserInfo       bool     `json:"include_user_info"`
	ProfileUpdateInterval uint     `json:"profile_update_interval"`
	ProfileWebPageURL     string   `json:"profile_web_page_url"`
}

func DefaultSubscriptionConfig() SubscriptionConfig {
	return SubscriptionConfig{
		ProfileName:           "RayPilot",
		CustomRules:           []string{"GEOIP,CN,DIRECT", "MATCH,PROXY"},
		IncludeUserInfo:       true,
		ProfileUpdateInterval: 24,
	}
}

func NormalizeSubscriptionConfig(cfg SubscriptionConfig) SubscriptionConfig {
	def := DefaultSubscriptionConfig()
	cfg.ProfileName = sanitizeSubscriptionProfileName(cfg.ProfileName)
	if cfg.ProfileName == "" {
		cfg.ProfileName = def.ProfileName
	}
	cfg.CustomRules = normalizeSubscriptionRules(cfg.CustomRules)
	if len(cfg.CustomRules) == 0 {
		cfg.CustomRules = def.CustomRules
	}
	if cfg.ProfileUpdateInterval > 168 {
		cfg.ProfileUpdateInterval = 168
	}
	cfg.ProfileWebPageURL = sanitizeSalesLandingHref(cfg.ProfileWebPageURL)
	return cfg
}

func ParseSubscriptionConfig(raw string) SubscriptionConfig {
	def := DefaultSubscriptionConfig()
	if strings.TrimSpace(raw) == "" {
		return def
	}
	var decoded struct {
		ProfileName           string   `json:"profile_name"`
		CustomRules           []string `json:"custom_rules"`
		IncludeUserInfo       *bool    `json:"include_user_info"`
		ProfileUpdateInterval uint     `json:"profile_update_interval"`
		ProfileWebPageURL     string   `json:"profile_web_page_url"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return def
	}
	cfg := SubscriptionConfig{
		ProfileName:           decoded.ProfileName,
		CustomRules:           decoded.CustomRules,
		IncludeUserInfo:       def.IncludeUserInfo,
		ProfileUpdateInterval: decoded.ProfileUpdateInterval,
		ProfileWebPageURL:     decoded.ProfileWebPageURL,
	}
	if decoded.IncludeUserInfo != nil {
		cfg.IncludeUserInfo = *decoded.IncludeUserInfo
	}
	return NormalizeSubscriptionConfig(cfg)
}

func sanitizeSubscriptionProfileName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
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
	runes := []rune(name)
	if len(runes) > 64 {
		name = string(runes[:64])
	}
	return name
}

func normalizeSubscriptionRules(rules []string) []string {
	normalized := make([]string, 0, len(rules)+1)
	seen := make(map[string]struct{})
	hasCatchAll := false
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)
		if rule == "" || strings.HasPrefix(rule, "#") || strings.HasPrefix(rule, "//") {
			continue
		}
		runes := []rune(rule)
		if len(runes) > 300 {
			rule = string(runes[:300])
		}
		upper := strings.ToUpper(rule)
		if _, ok := seen[upper]; ok {
			continue
		}
		seen[upper] = struct{}{}
		if strings.HasPrefix(upper, "MATCH,") || strings.HasPrefix(upper, "FINAL,") {
			hasCatchAll = true
		}
		normalized = append(normalized, rule)
		if len(normalized) >= 300 {
			break
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	if !hasCatchAll {
		normalized = append(normalized, "MATCH,PROXY")
	}
	return normalized
}

// SalesLandingConfig 销售首页配置。
type SalesLandingConfig struct {
	Brand         string             `json:"brand"`
	NavLinks      []SalesLandingLink `json:"nav_links"`
	Badges        []string           `json:"badges"`
	Title         string             `json:"title"`
	Subtitle      string             `json:"subtitle"`
	PrimaryCTA    SalesLandingLink   `json:"primary_cta"`
	SecondaryCTA  SalesLandingLink   `json:"secondary_cta"`
	TrustTags     []string           `json:"trust_tags"`
	HeroNodes     []SalesLandingNode `json:"hero_nodes"`
	SellingPoints []SalesLandingItem `json:"selling_points"`
	Plans         []SalesLandingPlan `json:"plans"`
	UseCases      []SalesLandingItem `json:"use_cases"`
	FAQs          []SalesLandingFAQ  `json:"faqs"`
	FinalCTA      SalesLandingCTA    `json:"final_cta"`
	FooterText    string             `json:"footer_text"`
}

type SalesLandingLink struct {
	Label string `json:"label"`
	To    string `json:"to"`
}

type SalesLandingNode struct {
	Flag    string `json:"flag"`
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Latency string `json:"latency"`
}

type SalesLandingItem struct {
	No    string `json:"no"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

type SalesLandingPlan struct {
	Tag      string   `json:"tag"`
	Name     string   `json:"name"`
	Price    string   `json:"price"`
	Unit     string   `json:"unit"`
	Action   string   `json:"action"`
	Featured bool     `json:"featured"`
	Features []string `json:"features"`
}

type SalesLandingFAQ struct {
	Q string `json:"q"`
	A string `json:"a"`
}

type SalesLandingCTA struct {
	Title       string             `json:"title"`
	Text        string             `json:"text"`
	ButtonLabel string             `json:"button_label"`
	ButtonTo    string             `json:"button_to"`
	FooterLinks []SalesLandingLink `json:"footer_links"`
}

func DefaultSalesLandingConfig() SalesLandingConfig {
	return SalesLandingConfig{
		Brand: "RayPilot",
		NavLinks: []SalesLandingLink{
			{Label: "套餐", To: "#plans"},
			{Label: "节点", To: "#nodes"},
			{Label: "说明", To: "#faq"},
			{Label: "登录", To: "/login"},
		},
		Badges:       []string{"高速节点", "稳定订阅", "按量流量"},
		Title:        "高速 VPN 节点",
		Subtitle:     "面向 AI、游戏、跨境办公和日常网络访问，提供多地区出口、专属订阅链接和清晰的流量管理。",
		PrimaryCTA:   SalesLandingLink{Label: "立即开通", To: "/register"},
		SecondaryCTA: SalesLandingLink{Label: "已有账号登录", To: "/login"},
		TrustTags:    []string{"VLESS Reality", "XHTTP 可选", "Clash / Mihomo 订阅"},
		HeroNodes: []SalesLandingNode{
			{Flag: "HK", Name: "香港入口", Desc: "低延迟中转", Latency: "35ms"},
			{Flag: "US", Name: "美国出口", Desc: "AI / 海外服务", Latency: "128ms"},
			{Flag: "SG", Name: "新加坡备用", Desc: "亚洲优化", Latency: "68ms"},
		},
		SellingPoints: []SalesLandingItem{
			{No: "01", Title: "多地区高速节点", Text: "按地区和线路能力下发订阅，支持直连与中转线路，减少单点不稳定带来的影响。"},
			{No: "02", Title: "流量清晰可查", Text: "用户中心展示套餐、剩余流量和订阅链接，用多少、剩多少一目了然。"},
			{No: "03", Title: "客户端导入简单", Text: "支持 Clash / Mihomo 等常见客户端订阅格式，复制订阅链接即可导入使用。"},
		},
		Plans: []SalesLandingPlan{
			{Tag: "STARTER", Name: "轻量流量", Price: "按套餐", Unit: "灵活开通", Action: "开始使用", Features: []string{"适合临时访问和轻量使用", "标准节点订阅", "用户中心自助查看"}},
			{Tag: "POPULAR", Name: "高速节点", Price: "推荐", Unit: "日常主力", Action: "选择推荐", Featured: true, Features: []string{"适合 AI、办公和影音访问", "多线路订阅", "支持流量池计费"}},
			{Tag: "PRO", Name: "大流量套餐", Price: "长期", Unit: "高频使用", Action: "开通套餐", Features: []string{"适合多设备和长期使用", "更多流量额度", "可配合兑换码续费"}},
		},
		UseCases: []SalesLandingItem{
			{Title: "AI 工具访问", Text: "为海外 AI 服务准备稳定出口线路。"},
			{Title: "游戏加速", Text: "选择低延迟地区节点，减少跨境链路波动。"},
			{Title: "跨境办公", Text: "让资料查询、远程协作和海外服务访问更顺畅。"},
			{Title: "多设备订阅", Text: "同一账号管理套餐和订阅链接，使用更方便。"},
		},
		FAQs: []SalesLandingFAQ{
			{Q: "购买后怎么使用？", A: "注册并开通套餐后，在用户中心复制订阅链接，导入 Clash Verge Rev、Mihomo 等客户端即可使用。"},
			{Q: "流量怎么计算？", A: "系统会按套餐规则统计已用流量和剩余流量，不同套餐可能有不同的计费倍率。"},
			{Q: "支持哪些节点模式？", A: "当前系统支持 VLESS Reality、TCP、XHTTP 和中转线路，具体以下发订阅为准。"},
		},
		FinalCTA: SalesLandingCTA{
			Title:       "现在开通 RayPilot 节点服务",
			Text:        "注册账号后进入用户中心，选择套餐或兑换码开通订阅。",
			ButtonLabel: "创建账号",
			ButtonTo:    "/register",
			FooterLinks: []SalesLandingLink{
				{Label: "用户登录", To: "/login"},
				{Label: "平台能力", To: "/platform"},
			},
		},
		FooterText: "RayPilot VPN 节点与流量服务",
	}
}

func NormalizeSalesLandingConfig(cfg SalesLandingConfig) SalesLandingConfig {
	def := DefaultSalesLandingConfig()
	cfg.Brand = strings.TrimSpace(cfg.Brand)
	cfg.Title = strings.TrimSpace(cfg.Title)
	cfg.Subtitle = strings.TrimSpace(cfg.Subtitle)
	cfg.FooterText = strings.TrimSpace(cfg.FooterText)
	if cfg.Brand == "" {
		cfg.Brand = def.Brand
	}
	cfg.NavLinks = normalizeSalesLandingLinks(cfg.NavLinks, def.NavLinks)
	cfg.Badges = normalizeSalesLandingTextList(cfg.Badges, def.Badges)
	if cfg.Title == "" {
		cfg.Title = def.Title
	}
	if cfg.Subtitle == "" {
		cfg.Subtitle = def.Subtitle
	}
	cfg.PrimaryCTA = normalizeSalesLandingLink(cfg.PrimaryCTA, def.PrimaryCTA)
	cfg.SecondaryCTA = normalizeSalesLandingLink(cfg.SecondaryCTA, def.SecondaryCTA)
	cfg.TrustTags = normalizeSalesLandingTextList(cfg.TrustTags, def.TrustTags)
	cfg.HeroNodes = normalizeSalesLandingNodes(cfg.HeroNodes, def.HeroNodes)
	cfg.SellingPoints = normalizeSalesLandingItems(cfg.SellingPoints, def.SellingPoints)
	cfg.Plans = normalizeSalesLandingPlans(cfg.Plans, def.Plans)
	cfg.UseCases = normalizeSalesLandingItems(cfg.UseCases, def.UseCases)
	cfg.FAQs = normalizeSalesLandingFAQs(cfg.FAQs, def.FAQs)
	cfg.FinalCTA = normalizeSalesLandingCTA(cfg.FinalCTA, def.FinalCTA)
	if cfg.FooterText == "" {
		cfg.FooterText = def.FooterText
	}
	return cfg
}

func normalizeSalesLandingLink(link, fallback SalesLandingLink) SalesLandingLink {
	link.Label = strings.TrimSpace(link.Label)
	link.To = sanitizeSalesLandingHref(link.To)
	if link.Label == "" {
		link.Label = fallback.Label
	}
	if link.To == "" {
		link.To = sanitizeSalesLandingHref(fallback.To)
	}
	return link
}

func normalizeSalesLandingLinks(links, fallback []SalesLandingLink) []SalesLandingLink {
	normalized := make([]SalesLandingLink, 0, len(links))
	for _, link := range links {
		link.Label = strings.TrimSpace(link.Label)
		link.To = sanitizeSalesLandingHref(link.To)
		if link.Label == "" && link.To == "" {
			continue
		}
		if link.Label == "" {
			link.Label = link.To
		}
		if link.To == "" {
			link.To = "#"
		}
		normalized = append(normalized, link)
	}
	if len(normalized) == 0 {
		return fallback
	}
	return normalized
}

func normalizeSalesLandingTextList(values, fallback []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) == 0 {
		return fallback
	}
	return normalized
}

func normalizeSalesLandingNodes(nodes, fallback []SalesLandingNode) []SalesLandingNode {
	normalized := make([]SalesLandingNode, 0, len(nodes))
	for _, node := range nodes {
		node.Flag = strings.TrimSpace(node.Flag)
		node.Name = strings.TrimSpace(node.Name)
		node.Desc = strings.TrimSpace(node.Desc)
		node.Latency = strings.TrimSpace(node.Latency)
		if node.Flag == "" && node.Name == "" && node.Desc == "" && node.Latency == "" {
			continue
		}
		normalized = append(normalized, node)
	}
	if len(normalized) == 0 {
		return fallback
	}
	return normalized
}

func normalizeSalesLandingItems(items, fallback []SalesLandingItem) []SalesLandingItem {
	normalized := make([]SalesLandingItem, 0, len(items))
	for _, item := range items {
		item.No = strings.TrimSpace(item.No)
		item.Title = strings.TrimSpace(item.Title)
		item.Text = strings.TrimSpace(item.Text)
		if item.No == "" && item.Title == "" && item.Text == "" {
			continue
		}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		return fallback
	}
	return normalized
}

func normalizeSalesLandingPlans(plans, fallback []SalesLandingPlan) []SalesLandingPlan {
	normalized := make([]SalesLandingPlan, 0, len(plans))
	for _, plan := range plans {
		plan.Tag = strings.TrimSpace(plan.Tag)
		plan.Name = strings.TrimSpace(plan.Name)
		plan.Price = strings.TrimSpace(plan.Price)
		plan.Unit = strings.TrimSpace(plan.Unit)
		plan.Action = strings.TrimSpace(plan.Action)
		plan.Features = normalizeSalesLandingTextList(plan.Features, nil)
		if plan.Features == nil {
			plan.Features = []string{}
		}
		if plan.Tag == "" && plan.Name == "" && plan.Price == "" && plan.Unit == "" && plan.Action == "" && len(plan.Features) == 0 {
			continue
		}
		if plan.Action == "" {
			plan.Action = "开始使用"
		}
		normalized = append(normalized, plan)
	}
	if len(normalized) == 0 {
		return fallback
	}
	return normalized
}

func normalizeSalesLandingFAQs(faqs, fallback []SalesLandingFAQ) []SalesLandingFAQ {
	normalized := make([]SalesLandingFAQ, 0, len(faqs))
	for _, faq := range faqs {
		faq.Q = strings.TrimSpace(faq.Q)
		faq.A = strings.TrimSpace(faq.A)
		if faq.Q == "" && faq.A == "" {
			continue
		}
		normalized = append(normalized, faq)
	}
	if len(normalized) == 0 {
		return fallback
	}
	return normalized
}

func normalizeSalesLandingCTA(cta, fallback SalesLandingCTA) SalesLandingCTA {
	cta.Title = strings.TrimSpace(cta.Title)
	cta.Text = strings.TrimSpace(cta.Text)
	cta.ButtonLabel = strings.TrimSpace(cta.ButtonLabel)
	cta.ButtonTo = sanitizeSalesLandingHref(cta.ButtonTo)
	if cta.Title == "" {
		cta.Title = fallback.Title
	}
	if cta.Text == "" {
		cta.Text = fallback.Text
	}
	if cta.ButtonLabel == "" {
		cta.ButtonLabel = fallback.ButtonLabel
	}
	if cta.ButtonTo == "" {
		cta.ButtonTo = sanitizeSalesLandingHref(fallback.ButtonTo)
	}
	cta.FooterLinks = normalizeSalesLandingLinks(cta.FooterLinks, fallback.FooterLinks)
	return cta
}

func sanitizeSalesLandingHref(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "#") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host != "" {
			return value
		}
	}
	return ""
}

func ParseSalesLandingConfig(raw string) SalesLandingConfig {
	if strings.TrimSpace(raw) == "" {
		return DefaultSalesLandingConfig()
	}
	var cfg SalesLandingConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return DefaultSalesLandingConfig()
	}
	return NormalizeSalesLandingConfig(cfg)
}

// OperationLog 操作日志模型。
type OperationLog struct {
	ID            uint64    `gorm:"primaryKey;column:id" json:"id"`
	ActorType     string    `gorm:"column:actor_type;type:varchar(16)" json:"actor_type"`
	ActorUserID   *uint64   `gorm:"column:actor_user_id;index" json:"actor_user_id,omitempty"`
	ActorUsername *string   `gorm:"column:actor_username;type:varchar(64)" json:"actor_username,omitempty"`
	ClientIP      *string   `gorm:"column:client_ip;type:varchar(45)" json:"client_ip,omitempty"`
	ForwardedFor  *string   `gorm:"column:forwarded_for;type:text" json:"forwarded_for,omitempty"`
	RealIP        *string   `gorm:"column:real_ip;type:varchar(45)" json:"real_ip,omitempty"`
	UserAgent     *string   `gorm:"column:user_agent;type:text" json:"user_agent,omitempty"`
	Action        string    `gorm:"column:action;type:varchar(64);index" json:"action"`
	TargetType    *string   `gorm:"column:target_type;type:varchar(32)" json:"target_type,omitempty"`
	TargetID      *uint64   `gorm:"column:target_id;index" json:"target_id,omitempty"`
	Result        string    `gorm:"column:result;type:varchar(16);default:success;index" json:"result"`
	Summary       string    `gorm:"column:summary;type:varchar(255)" json:"summary"`
	ExtraJSON     *string   `gorm:"column:extra_json;type:json" json:"extra_json,omitempty"`
	CreatedAt     time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (OperationLog) TableName() string {
	return "operation_logs"
}

// DeploymentLog 部署日志模型。
type DeploymentLog struct {
	ID               uint64              `gorm:"primaryKey;column:id" json:"id"`
	OperatorUserID   *uint64             `gorm:"column:operator_user_id;index" json:"operator_user_id,omitempty"`
	OperatorUsername *string             `gorm:"column:operator_username;type:varchar(64)" json:"operator_username,omitempty"`
	OperatorIP       *string             `gorm:"column:operator_ip;type:varchar(45)" json:"operator_ip,omitempty"`
	DeployType       string              `gorm:"column:deploy_type;type:varchar(32);index" json:"deploy_type"`
	TargetServerIP   string              `gorm:"column:target_server_ip;type:varchar(255);index" json:"target_server_ip"`
	TargetRole       string              `gorm:"column:target_role;type:varchar(16)" json:"target_role"`
	RequestSummary   *string             `gorm:"column:request_summary;type:json" json:"request_summary,omitempty"`
	Result           string              `gorm:"column:result;type:varchar(16);default:success;index" json:"result"`
	DurationMS       *uint64             `gorm:"column:duration_ms" json:"duration_ms,omitempty"`
	NodeID           *uint64             `gorm:"column:node_id" json:"node_id,omitempty"`
	NodeIDs          *string             `gorm:"column:node_ids;type:json" json:"node_ids,omitempty"`
	NodeHostID       *uint64             `gorm:"column:node_host_id" json:"node_host_id,omitempty"`
	RelayID          *uint64             `gorm:"column:relay_id" json:"relay_id,omitempty"`
	BackendIDs       *string             `gorm:"column:backend_ids;type:json" json:"backend_ids,omitempty"`
	ErrorDetail      *string             `gorm:"column:error_detail;type:text" json:"error_detail,omitempty"`
	CreatedAt        time.Time           `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	Steps            []DeploymentLogStep `gorm:"foreignKey:DeploymentLogID" json:"steps,omitempty"`
}

func (DeploymentLog) TableName() string {
	return "deployment_logs"
}

// DeploymentLogStep 部署步骤日志模型。
type DeploymentLogStep struct {
	ID              uint64    `gorm:"primaryKey;column:id" json:"id"`
	DeploymentLogID uint64    `gorm:"column:deployment_log_id;index" json:"deployment_log_id"`
	StepOrder       int       `gorm:"column:step_order" json:"step_order"`
	Name            string    `gorm:"column:name;type:varchar(128)" json:"name"`
	Status          string    `gorm:"column:status;type:varchar(16)" json:"status"`
	Message         string    `gorm:"column:message;type:text" json:"message"`
	CreatedAt       time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

func (DeploymentLogStep) TableName() string {
	return "deployment_log_steps"
}

// CreateOrderRequest 创建订单请求。
type CreateOrderRequest struct {
	PlanID uint64 `json:"plan_id" binding:"required"`
}

// TrafficSnapshot 流量快照模型。
type TrafficSnapshot struct {
	ID            uint64    `gorm:"primaryKey;column:id" json:"id"`
	NodeID        uint64    `gorm:"column:node_id;index" json:"node_id"`
	XrayUserKey   string    `gorm:"column:xray_user_key;type:varchar(255);index" json:"xray_user_key"`
	UplinkTotal   uint64    `gorm:"column:uplink_total" json:"uplink_total"`
	DownlinkTotal uint64    `gorm:"column:downlink_total" json:"downlink_total"`
	CapturedAt    time.Time `gorm:"column:captured_at;index" json:"captured_at"`
}

// TableName 指定表名。
func (TrafficSnapshot) TableName() string {
	return "traffic_snapshots"
}

// UsageLedger 流量账本模型。
type UsageLedger struct {
	ID                uint64    `gorm:"primaryKey;column:id" json:"id"`
	UserID            uint64    `gorm:"column:user_id;index" json:"user_id"`
	SubscriptionID    *uint64   `gorm:"column:subscription_id;index" json:"subscription_id"`
	NodeID            uint64    `gorm:"column:node_id" json:"node_id"`
	TrafficPool       string    `gorm:"column:traffic_pool;type:varchar(32);default:normal" json:"traffic_pool"`
	BillingMultiplier float64   `gorm:"column:billing_multiplier;type:decimal(8,3);default:1" json:"billing_multiplier"`
	DeltaUpload       uint64    `gorm:"column:delta_upload" json:"delta_upload"`
	BilledUpload      uint64    `gorm:"column:billed_upload" json:"billed_upload"`
	DeltaDownload     uint64    `gorm:"column:delta_download" json:"delta_download"`
	BilledDownload    uint64    `gorm:"column:billed_download" json:"billed_download"`
	DeltaTotal        uint64    `gorm:"column:delta_total" json:"delta_total"`
	BilledTotal       uint64    `gorm:"column:billed_total" json:"billed_total"`
	RecordedAt        time.Time `gorm:"column:recorded_at;index" json:"recorded_at"`
}

// TableName 指定表名。
func (UsageLedger) TableName() string {
	return "usage_ledgers"
}

// AgentTask 下发给 node-agent 的任务。
type AgentTask struct {
	ID             int64  `json:"id"`
	Action         string `json:"action"`
	Payload        string `json:"payload"`
	IdempotencyKey string `json:"idempotency_key"`
	LockToken      string `json:"lock_token"`
}
