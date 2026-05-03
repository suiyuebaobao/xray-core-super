// Package model 定义数据库模型（GORM）。
//
// 每个模型对应一张数据库表，通过 golang-migrate 创建实际表结构。
// GORM 模型仅用于代码层面的查询和写入，不替代 migration。
package model

import (
	"time"
)

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
	ID           uint64    `gorm:"primaryKey;column:id" json:"id"`
	UserID       uint64    `gorm:"column:user_id;index" json:"user_id"`
	PlanID       uint64    `gorm:"column:plan_id" json:"plan_id"`
	StartDate    time.Time `gorm:"column:start_date" json:"start_date"`
	ExpireDate   time.Time `gorm:"column:expire_date;index" json:"expire_date"`
	TrafficLimit uint64    `gorm:"column:traffic_limit" json:"traffic_limit"`
	UsedTraffic  uint64    `gorm:"column:used_traffic" json:"used_traffic"`
	Status       string    `gorm:"column:status;type:varchar(16);index" json:"status"`
	ActiveUserID *uint64   `gorm:"column:active_user_id;uniqueIndex" json:"-"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
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
	ID           uint64    `gorm:"primaryKey;column:id" json:"id"`
	Name         string    `gorm:"column:name;type:varchar(128)" json:"name"`
	Price        float64   `gorm:"column:price;type:decimal(10,2)" json:"price"`
	Currency     string    `gorm:"column:currency;type:varchar(8);default:USDT" json:"currency"`
	TrafficLimit uint64    `gorm:"column:traffic_limit" json:"traffic_limit"`
	DurationDays uint32    `gorm:"column:duration_days" json:"duration_days"`
	SortWeight   int       `gorm:"column:sort_weight;default:0" json:"sort_weight"`
	IsActive     bool      `gorm:"column:is_active;default:true;index" json:"is_active"`
	IsDefault    bool      `gorm:"column:is_default;default:false;index" json:"is_default"`
	IsDeleted    bool      `gorm:"column:is_deleted;default:false;index" json:"is_deleted"`
	CreatedAt    time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名。
func (Plan) TableName() string {
	return "plans"
}

// CreatePlanRequest 创建套餐请求。
type CreatePlanRequest struct {
	Name         string  `json:"name" binding:"required"`
	Price        float64 `json:"price" binding:"required,min=0"`
	Currency     string  `json:"currency" default:"USDT"`
	TrafficLimit uint64  `json:"traffic_limit" binding:"min=0"`
	DurationDays uint32  `json:"duration_days" binding:"min=0"`
	SortWeight   int     `json:"sort_weight"`
	IsActive     bool    `json:"is_active"`
}

// UpdatePlanRequest 更新套餐请求。
type UpdatePlanRequest struct {
	Name         string  `json:"name" binding:"required"`
	Price        float64 `json:"price" binding:"required,min=0"`
	Currency     string  `json:"currency"`
	TrafficLimit uint64  `json:"traffic_limit" binding:"min=0"`
	DurationDays uint32  `json:"duration_days" binding:"min=0"`
	SortWeight   int     `json:"sort_weight"`
	IsActive     bool    `json:"is_active"`
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

// Node 节点模型。
type Node struct {
	ID                   uint64     `gorm:"primaryKey;column:id" json:"id"`
	Name                 string     `gorm:"column:name;type:varchar(128)" json:"name"`
	Protocol             string     `gorm:"column:protocol;type:varchar(32);default:vless" json:"protocol"`
	Host                 string     `gorm:"column:host;type:varchar(255)" json:"host"`
	Port                 uint32     `gorm:"column:port;default:443" json:"port"`
	ServerName           string     `gorm:"column:server_name;type:varchar(255)" json:"server_name"`
	PublicKey            string     `gorm:"column:public_key;type:varchar(255)" json:"public_key"`
	ShortID              string     `gorm:"column:short_id;type:varchar(32)" json:"short_id"`
	Fingerprint          string     `gorm:"column:fingerprint;type:varchar(32);default:chrome" json:"fingerprint"`
	Flow                 string     `gorm:"column:flow;type:varchar(32);default:xtls-rprx-vision" json:"flow"`
	LineMode             string     `gorm:"column:line_mode;type:varchar(32);default:direct_and_relay" json:"line_mode"`
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

// CreateNodeRequest 创建节点请求。
type CreateNodeRequest struct {
	Name         string `json:"name" binding:"required"`
	Protocol     string `json:"protocol" default:"vless"`
	Host         string `json:"host" binding:"required"`
	Port         uint32 `json:"port" default:"443"`
	ServerName   string `json:"server_name"`
	PublicKey    string `json:"public_key"`
	ShortID      string `json:"short_id"`
	Fingerprint  string `json:"fingerprint" default:"chrome"`
	Flow         string `json:"flow" default:"xtls-rprx-vision"`
	LineMode     string `json:"line_mode" binding:"omitempty,oneof=direct_only relay_only direct_and_relay"`
	AgentBaseURL string `json:"agent_base_url" binding:"required"`
	AgentToken   string `json:"agent_token" binding:"required"`
	SortWeight   int    `json:"sort_weight"`
	IsEnabled    bool   `json:"is_enabled"`
}

// UpdateNodeRequest 更新节点请求。
type UpdateNodeRequest struct {
	Name         string `json:"name" binding:"required"`
	Protocol     string `json:"protocol"`
	Host         string `json:"host" binding:"required"`
	Port         uint32 `json:"port"`
	ServerName   string `json:"server_name"`
	PublicKey    string `json:"public_key"`
	ShortID      string `json:"short_id"`
	Fingerprint  string `json:"fingerprint"`
	Flow         string `json:"flow"`
	LineMode     string `json:"line_mode" binding:"omitempty,oneof=direct_only relay_only direct_and_relay"`
	AgentBaseURL string `json:"agent_base_url" binding:"required"`
	AgentToken   string `json:"agent_token"`
	SortWeight   int    `json:"sort_weight"`
	IsEnabled    bool   `json:"is_enabled"`
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
	ID             uint64    `gorm:"primaryKey;column:id" json:"id"`
	UserID         uint64    `gorm:"column:user_id;index" json:"user_id"`
	SubscriptionID *uint64   `gorm:"column:subscription_id;index" json:"subscription_id"`
	NodeID         uint64    `gorm:"column:node_id" json:"node_id"`
	DeltaUpload    uint64    `gorm:"column:delta_upload" json:"delta_upload"`
	DeltaDownload  uint64    `gorm:"column:delta_download" json:"delta_download"`
	DeltaTotal     uint64    `gorm:"column:delta_total" json:"delta_total"`
	RecordedAt     time.Time `gorm:"column:recorded_at;index" json:"recorded_at"`
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
