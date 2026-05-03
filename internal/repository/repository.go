// Package repository 提供数据访问层。
//
// 封装对数据库的 CRUD 操作，对外只暴露模型和错误，
// 不泄露 GORM 内部细节。业务逻辑在 service 层。
package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"suiyue/internal/model"
	"suiyue/internal/platform/secure"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNodeGroupInUse = errors.New("该分组下存在节点，无法删除")
var ErrInvalidNodeID = errors.New("节点 ID 无效")
var ErrRelayHasEnabledBackends = errors.New("中转节点存在启用的后端绑定，无法删除")
var ErrDefaultPlanCannotDelete = errors.New("基础套餐不能删除，只能修改")

const (
	defaultPlanName         = "基础套餐"
	defaultPlanDurationDays = 3650
)

// NodeGroupNodeBindingChange 表示节点分组绑定变更结果。
type NodeGroupNodeBindingChange struct {
	NodeIDs        []uint64
	AddedNodeIDs   []uint64
	RemovedNodeIDs []uint64
}

// PlanSubscriptionMove 表示删除套餐时被迁移到基础套餐的活动订阅。
type PlanSubscriptionMove struct {
	UserID         uint64
	SubscriptionID uint64
	OldPlanID      uint64
	NewPlanID      uint64
}

// PlanDeleteResult 表示套餐删除的业务结果。
type PlanDeleteResult struct {
	PlanID             uint64
	DefaultPlanID      uint64
	MovedSubscriptions []PlanSubscriptionMove
}

// UserRepository 用户数据访问接口。
type UserRepository struct {
	db *gorm.DB
}

// NewUserRepository 创建用户 Repository。
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create 创建用户。
func (r *UserRepository) Create(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// CreateWithSubscriptionToken 创建用户，并同步生成用户级订阅 Token。
func (r *UserRepository) CreateWithSubscriptionToken(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}

		var subscriptionID *uint64
		if tx.Migrator().HasTable(&model.Plan{}) && tx.Migrator().HasTable(&model.UserSubscription{}) {
			plan, err := ensureDefaultPlanTx(tx)
			if err != nil {
				return err
			}
			sub, err := createDefaultSubscriptionForUserTx(tx, user.ID, plan, modelNow())
			if err != nil {
				return err
			}
			subscriptionID = &sub.ID
		}

		_, err := CreateSubscriptionTokenTx(tx, user.ID, subscriptionID, nil)
		return err
	})
}

// FindByUsername 根据用户名查找用户。
func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// FindByID 根据 ID 查找用户。
func (r *UserRepository) FindByID(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Delete 删除用户及用户侧关联数据。
//
// 数据库迁移中已为生产库声明外键级联；这里仍显式清理一遍，
// 保证禁用外键的测试环境和未来迁移调整时行为一致。
func (r *UserRepository) Delete(ctx context.Context, userID uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		var subscriptionIDs []uint64
		if tx.Migrator().HasTable(&model.UserSubscription{}) {
			if err := tx.Model(&model.UserSubscription{}).
				Where("user_id = ?", userID).
				Pluck("id", &subscriptionIDs).Error; err != nil {
				return err
			}
		}

		if tx.Migrator().HasTable(&model.NodeAccessTask{}) && len(subscriptionIDs) > 0 {
			if err := tx.Model(&model.NodeAccessTask{}).
				Where("subscription_id IN ?", subscriptionIDs).
				Update("subscription_id", nil).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.RedeemCode{}) {
			if err := tx.Model(&model.RedeemCode{}).
				Where("used_by_user_id = ?", userID).
				Update("used_by_user_id", nil).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.PaymentRecord{}) {
			if err := tx.Where("user_id = ?", userID).Delete(&model.PaymentRecord{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.Order{}) {
			if err := tx.Where("user_id = ?", userID).Delete(&model.Order{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.UsageLedger{}) {
			if err := tx.Where("user_id = ?", userID).Delete(&model.UsageLedger{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.TrafficSnapshot{}) && user.XrayUserKey != "" {
			if err := tx.Where("xray_user_key = ?", user.XrayUserKey).Delete(&model.TrafficSnapshot{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.SubscriptionToken{}) {
			if err := tx.Where("user_id = ?", userID).Delete(&model.SubscriptionToken{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.RefreshToken{}) {
			if err := tx.Where("user_id = ?", userID).Delete(&model.RefreshToken{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.UserSubscription{}) {
			if err := tx.Where("user_id = ?", userID).Delete(&model.UserSubscription{}).Error; err != nil {
				return err
			}
		}

		return tx.Delete(&model.User{}, userID).Error
	})
}

// FindByXrayUserKey 根据 xray_user_key 查找用户。
func (r *UserRepository) FindByXrayUserKey(ctx context.Context, key string) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).Where("xray_user_key = ?", key).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// UpdateLoginInfo 更新最后登录时间和 IP。
func (r *UserRepository) UpdateLoginInfo(ctx context.Context, userID uint64, ip string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]interface{}{
			"last_login_at": modelNow(),
			"last_login_ip": ip,
		}).Error
}

// UpdatePassword 更新用户密码。
func (r *UserRepository) UpdatePassword(ctx context.Context, userID uint64, passwordHash string) error {
	return r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ?", userID).
		Update("password_hash", passwordHash).Error
}

// List 列出所有用户。
func (r *UserRepository) List(ctx context.Context) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).
		Order("id ASC").
		Find(&users).Error
	return users, err
}

// ListPaginated 分页查询用户列表。
func (r *UserRepository) ListPaginated(ctx context.Context, page, size int) ([]model.User, int64, error) {
	var users []model.User
	var total int64

	q := r.db.WithContext(ctx).Model(&model.User{})
	q.Count(&total)

	offset := (page - 1) * size
	err := q.Offset(offset).Limit(size).Order("id ASC").Find(&users).Error
	return users, total, err
}

// Count 统计用户数量。
func (r *UserRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&model.User{}).Count(&total).Error
	return total, err
}

// SearchByUsername 按用户名模糊搜索（分页）。
func (r *UserRepository) SearchByUsername(ctx context.Context, keyword string, page, size int) ([]model.User, int64, error) {
	var users []model.User
	var total int64

	q := r.db.WithContext(ctx).Model(&model.User{}).Where("username LIKE ? ESCAPE ?", "%"+escapeLike(keyword)+"%", `\`)
	q.Count(&total)

	offset := (page - 1) * size
	err := q.Offset(offset).Limit(size).Order("id ASC").Find(&users).Error
	return users, total, err
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(value)
}

// UpdateStatus 更新用户状态（启用/禁用）。
func (r *UserRepository) UpdateStatus(ctx context.Context, userID uint64, status string) error {
	return r.db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", userID).
		Update("status", status).Error
}

// Update 更新用户完整信息。
func (r *UserRepository) Update(ctx context.Context, user *model.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

// RefreshTokenRepository Refresh Token 数据访问。
type RefreshTokenRepository struct {
	db *gorm.DB
}

// NewRefreshTokenRepository 创建 Refresh Token Repository。
func NewRefreshTokenRepository(db *gorm.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

// Create 保存 Refresh Token 哈希。
func (r *RefreshTokenRepository) Create(ctx context.Context, token *model.RefreshToken) error {
	return r.db.WithContext(ctx).Create(token).Error
}

// DeleteByHash 根据 Token 哈希删除（登出时调用）。
func (r *RefreshTokenRepository) DeleteByHash(ctx context.Context, hash string) error {
	return r.db.WithContext(ctx).
		Where("token_hash = ?", hash).
		Delete(&model.RefreshToken{}).Error
}

// FindByHash 根据 Token 哈希查找记录（刷新时验证用）。
func (r *RefreshTokenRepository) FindByHash(ctx context.Context, hash string) (*model.RefreshToken, error) {
	var token model.RefreshToken
	err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&token).Error
	if err != nil {
		return nil, err
	}
	return &token, nil
}

// DeleteExpired 删除过期的 Refresh Token。
func (r *RefreshTokenRepository) DeleteExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ?", time.Now()).
		Delete(&model.RefreshToken{}).Error
}

// SubscriptionRepository 订阅数据访问。
type SubscriptionRepository struct {
	db *gorm.DB
}

// NewSubscriptionRepository 创建订阅 Repository。
func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

// FindActiveByUserID 查找用户当前有效订阅。
func (r *SubscriptionRepository) FindActiveByUserID(ctx context.Context, userID uint64) (*model.UserSubscription, error) {
	var sub model.UserSubscription
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND status = ? AND expire_date > ?", userID, "ACTIVE", time.Now()).
		Order("expire_date DESC").
		Limit(1).
		Find(&sub)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &sub, nil
}

// FindByID 根据 ID 查找订阅。
func (r *SubscriptionRepository) FindByID(ctx context.Context, id uint64) (*model.UserSubscription, error) {
	var sub model.UserSubscription
	err := r.db.WithContext(ctx).First(&sub, id).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// FindLatestByUserID 查找用户最近一条订阅记录。
func (r *SubscriptionRepository) FindLatestByUserID(ctx context.Context, userID uint64) (*model.UserSubscription, error) {
	var sub model.UserSubscription
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("created_at DESC, id DESC").
		First(&sub).Error
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// Create 创建订阅。
func (r *SubscriptionRepository) Create(ctx context.Context, sub *model.UserSubscription) error {
	return r.db.WithContext(ctx).Create(sub).Error
}

// WithTransaction 在事务中执行函数。
func (r *SubscriptionRepository) WithTransaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return r.db.WithContext(ctx).Transaction(fn)
}

// ListActiveByPlanID 查询套餐下所有仍有效的活跃订阅。
func (r *SubscriptionRepository) ListActiveByPlanID(ctx context.Context, planID uint64) ([]model.UserSubscription, error) {
	var subs []model.UserSubscription
	err := r.db.WithContext(ctx).
		Where("plan_id = ? AND status = ? AND expire_date > ?", planID, "ACTIVE", time.Now()).
		Find(&subs).Error
	return subs, err
}

// ListActiveByNodeGroupID 查询绑定了指定节点分组的活跃订阅。
func (r *SubscriptionRepository) ListActiveByNodeGroupID(ctx context.Context, nodeGroupID uint64) ([]model.UserSubscription, error) {
	var subs []model.UserSubscription
	err := r.db.WithContext(ctx).
		Table("user_subscriptions AS us").
		Select("us.*").
		Joins("JOIN plan_node_groups AS png ON png.plan_id = us.plan_id").
		Where("png.node_group_id = ? AND us.status = ? AND us.expire_date > ?", nodeGroupID, "ACTIVE", time.Now()).
		Find(&subs).Error
	return subs, err
}

// CountActive 统计仍有效的活跃订阅数量。
func (r *SubscriptionRepository) CountActive(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).
		Model(&model.UserSubscription{}).
		Where("status = ? AND expire_date > ?", "ACTIVE", time.Now()).
		Count(&total).Error
	return total, err
}

// SubscriptionTokenRepository 订阅 Token 数据访问。
type SubscriptionTokenRepository struct {
	db *gorm.DB
}

// NewSubscriptionTokenRepository 创建订阅 Token Repository。
func NewSubscriptionTokenRepository(db *gorm.DB) *SubscriptionTokenRepository {
	return &SubscriptionTokenRepository{db: db}
}

// FindByToken 根据 token 字符串查找。
func (r *SubscriptionTokenRepository) FindByToken(ctx context.Context, token string) (*model.SubscriptionToken, error) {
	var st model.SubscriptionToken
	err := r.db.WithContext(ctx).Where("token = ? AND is_revoked = ? AND (expires_at IS NULL OR expires_at > ?)", token, false, time.Now()).First(&st).Error
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// Create 创建订阅 Token。
func (r *SubscriptionTokenRepository) Create(ctx context.Context, st *model.SubscriptionToken) error {
	return r.db.WithContext(ctx).Create(st).Error
}

// CreateSubscriptionTokenTx 在指定事务中创建用户唯一订阅 Token。
func CreateSubscriptionTokenTx(tx *gorm.DB, userID uint64, subscriptionID *uint64, expiresAt *time.Time) (*model.SubscriptionToken, error) {
	for i := 0; i < 3; i++ {
		tokenStr, err := secure.RandomHex(16)
		if err != nil {
			return nil, err
		}
		st := &model.SubscriptionToken{
			UserID:         userID,
			SubscriptionID: subscriptionID,
			Token:          tokenStr,
			IsRevoked:      false,
			ExpiresAt:      expiresAt,
		}
		if err := tx.Create(st).Error; err == nil {
			return st, nil
		}
	}
	return nil, errors.New("create subscription token failed")
}

// EnsureSubscriptionTokenTx 确保用户有一条 Token 记录；已存在时不改变 token 值。
func EnsureSubscriptionTokenTx(tx *gorm.DB, userID uint64, subscriptionID *uint64, expiresAt *time.Time) (*model.SubscriptionToken, error) {
	var existing model.SubscriptionToken
	result := tx.Where("user_id = ?", userID).
		Order("is_revoked ASC, created_at DESC, id DESC").
		Limit(1).
		Find(&existing)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return CreateSubscriptionTokenTx(tx, userID, subscriptionID, expiresAt)
	}
	updates := map[string]interface{}{}
	if subscriptionID != nil && (existing.SubscriptionID == nil || *existing.SubscriptionID != *subscriptionID) {
		updates["subscription_id"] = *subscriptionID
	}
	if expiresAt != nil {
		updates["expires_at"] = *expiresAt
	}
	if len(updates) > 0 {
		if err := tx.Model(&model.SubscriptionToken{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
		if err := tx.First(&existing, existing.ID).Error; err != nil {
			return nil, err
		}
	}
	return &existing, nil
}

func resetSubscriptionTokenTx(tx *gorm.DB, id uint64, subscriptionID *uint64, expiresAt *time.Time) (*model.SubscriptionToken, error) {
	for i := 0; i < 3; i++ {
		tokenStr, err := secure.RandomHex(16)
		if err != nil {
			return nil, err
		}
		err = tx.Model(&model.SubscriptionToken{}).
			Where("id = ?", id).
			Updates(map[string]interface{}{
				"subscription_id": subscriptionID,
				"token":           tokenStr,
				"is_revoked":      false,
				"last_used_at":    nil,
				"expires_at":      expiresAt,
			}).Error
		if err == nil {
			var updated model.SubscriptionToken
			if err := tx.First(&updated, id).Error; err != nil {
				return nil, err
			}
			return &updated, nil
		}
	}
	return nil, errors.New("reset subscription token failed")
}

// UpdateLastUsed 更新最后使用时间。
func (r *SubscriptionTokenRepository) UpdateLastUsed(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Model(&model.SubscriptionToken{}).
		Where("id = ?", id).
		Update("last_used_at", modelNow()).Error
}

// FindBySubscriptionID 根据订阅 ID 查找所有关联的 Token。
func (r *SubscriptionTokenRepository) FindBySubscriptionID(ctx context.Context, subscriptionID uint64) ([]model.SubscriptionToken, error) {
	var tokens []model.SubscriptionToken
	err := r.db.WithContext(ctx).
		Where("subscription_id = ?", subscriptionID).
		Find(&tokens).Error
	return tokens, err
}

// FindByID 根据 ID 查找订阅 Token。
func (r *SubscriptionTokenRepository) FindByID(ctx context.Context, id uint64) (*model.SubscriptionToken, error) {
	var st model.SubscriptionToken
	err := r.db.WithContext(ctx).First(&st, id).Error
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// Revoke 撤销订阅 Token。
func (r *SubscriptionTokenRepository) Revoke(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).
		Model(&model.SubscriptionToken{}).
		Where("id = ?", id).
		Update("is_revoked", true).Error
}

// FindActiveByUserID 查找用户当前可用的订阅 Token。
func (r *SubscriptionTokenRepository) FindActiveByUserID(ctx context.Context, userID uint64) (*model.SubscriptionToken, error) {
	var st model.SubscriptionToken
	result := r.db.WithContext(ctx).
		Where("user_id = ? AND is_revoked = ? AND (expires_at IS NULL OR expires_at > ?)", userID, false, time.Now()).
		Order("created_at DESC, id DESC").
		Limit(1).
		Find(&st)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return &st, nil
}

// EnsureActiveByUserID 确保用户拥有唯一订阅 Token；已有记录时保持 token 不变。
func (r *SubscriptionTokenRepository) EnsureActiveByUserID(ctx context.Context, userID uint64, subscriptionID *uint64, expiresAt *time.Time) (*model.SubscriptionToken, error) {
	var created *model.SubscriptionToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		st, err := EnsureSubscriptionTokenTx(tx, userID, subscriptionID, expiresAt)
		if err != nil {
			return err
		}
		created = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ResetByID 重置指定 Token 记录的 token 值，不新增历史记录。
func (r *SubscriptionTokenRepository) ResetByID(ctx context.Context, id uint64, subscriptionID *uint64, expiresAt *time.Time) (*model.SubscriptionToken, error) {
	var updated *model.SubscriptionToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.SubscriptionToken
		result := tx.Where("id = ?", id).Limit(1).Find(&existing)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		st, err := resetSubscriptionTokenTx(tx, existing.ID, subscriptionID, expiresAt)
		if err != nil {
			return err
		}
		updated = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// ResetByUserID 重置用户唯一 Token 的 token 值；没有记录时创建一条。
func (r *SubscriptionTokenRepository) ResetByUserID(ctx context.Context, userID uint64, subscriptionID *uint64, expiresAt *time.Time) (*model.SubscriptionToken, error) {
	var created *model.SubscriptionToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing model.SubscriptionToken
		result := tx.Where("user_id = ?", userID).
			Order("is_revoked ASC, created_at DESC, id DESC").
			Limit(1).
			Find(&existing)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			st, err := CreateSubscriptionTokenTx(tx, userID, subscriptionID, expiresAt)
			if err != nil {
				return err
			}
			created = st
			return nil
		}
		st, err := resetSubscriptionTokenTx(tx, existing.ID, subscriptionID, expiresAt)
		if err != nil {
			return err
		}
		created = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// ListPaginated 分页查询所有订阅 Token（管理后台用）。
func (r *SubscriptionTokenRepository) ListPaginated(ctx context.Context, page, size int) ([]model.SubscriptionToken, int64, error) {
	var tokens []model.SubscriptionToken
	var total int64

	q := r.db.WithContext(ctx).Model(&model.SubscriptionToken{})
	q.Count(&total)

	offset := (page - 1) * size
	err := q.Offset(offset).Limit(size).Order("created_at DESC").Find(&tokens).Error
	return tokens, total, err
}

// FindByUserID 根据用户 ID 查找所有 Token。
func (r *SubscriptionTokenRepository) FindByUserID(ctx context.Context, userID uint64) ([]model.SubscriptionToken, error) {
	var tokens []model.SubscriptionToken
	err := r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_revoked ASC, created_at DESC, id DESC").
		Limit(1).
		Find(&tokens).Error
	return tokens, err
}

// PlanRepository 套餐数据访问。
type PlanRepository struct {
	db *gorm.DB
}

// NewPlanRepository 创建套餐 Repository。
func NewPlanRepository(db *gorm.DB) *PlanRepository {
	return &PlanRepository{db: db}
}

// FindByID 根据 ID 查找套餐。
func (r *PlanRepository) FindByID(ctx context.Context, id uint64) (*model.Plan, error) {
	var plan model.Plan
	err := r.db.WithContext(ctx).
		Where("id = ? AND is_deleted = ?", id, false).
		First(&plan).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// FindDefault 查询基础套餐。
func (r *PlanRepository) FindDefault(ctx context.Context) (*model.Plan, error) {
	var plan model.Plan
	err := r.db.WithContext(ctx).
		Where("is_default = ? AND is_deleted = ?", true, false).
		Order("id ASC").
		First(&plan).Error
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

// EnsureDefault 确保系统存在一个基础套餐。
func (r *PlanRepository) EnsureDefault(ctx context.Context) (*model.Plan, error) {
	var plan *model.Plan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		plan, err = ensureDefaultPlanTx(tx)
		return err
	})
	return plan, err
}

// ListActive 列出所有上架套餐。
func (r *PlanRepository) ListActive(ctx context.Context) ([]model.Plan, error) {
	var plans []model.Plan
	err := r.db.WithContext(ctx).
		Where("is_active = ? AND is_deleted = ?", true, false).
		Order("sort_weight ASC, id ASC").
		Find(&plans).Error
	return plans, err
}

// Create 创建套餐。
func (r *PlanRepository) Create(ctx context.Context, plan *model.Plan) (*model.Plan, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if plan.IsDefault {
			if err := tx.Model(&model.Plan{}).Where("is_default = ?", true).Update("is_default", false).Error; err != nil {
				return err
			}
			plan.IsActive = true
			plan.IsDeleted = false
		}
		return tx.Create(plan).Error
	})
	return plan, err
}

// Update 更新套餐。
func (r *PlanRepository) Update(ctx context.Context, plan *model.Plan) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if plan.IsDefault {
			plan.IsActive = true
			plan.IsDeleted = false
		}
		if err := tx.Save(plan).Error; err != nil {
			return err
		}
		if plan.IsDefault {
			return tx.Model(&model.Plan{}).
				Where("id <> ? AND is_default = ?", plan.ID, true).
				Update("is_default", false).Error
		}
		return nil
	})
}

// Delete 删除套餐。
func (r *PlanRepository) Delete(ctx context.Context, id uint64) error {
	_, err := r.DeleteWithDefaultFallback(ctx, id)
	return err
}

// DeleteWithDefaultFallback 删除普通套餐，并把活动订阅迁移到基础套餐。
func (r *PlanRepository) DeleteWithDefaultFallback(ctx context.Context, id uint64) (*PlanDeleteResult, error) {
	result := &PlanDeleteResult{PlanID: id}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var plan model.Plan
		if err := tx.Where("id = ? AND is_deleted = ?", id, false).First(&plan).Error; err != nil {
			return err
		}
		if plan.IsDefault {
			return ErrDefaultPlanCannotDelete
		}

		defaultPlan, err := ensureDefaultPlanTxExcept(tx, id)
		if err != nil {
			return err
		}
		result.DefaultPlanID = defaultPlan.ID

		now := modelNow()
		if tx.Migrator().HasTable(&model.UserSubscription{}) {
			var activeSubs []model.UserSubscription
			if err := tx.
				Where("plan_id = ? AND status = ? AND expire_date > ?", id, "ACTIVE", now).
				Order("id ASC").
				Find(&activeSubs).Error; err != nil {
				return err
			}
			for _, sub := range activeSubs {
				result.MovedSubscriptions = append(result.MovedSubscriptions, PlanSubscriptionMove{
					UserID:         sub.UserID,
					SubscriptionID: sub.ID,
					OldPlanID:      id,
					NewPlanID:      defaultPlan.ID,
				})
			}
			if len(activeSubs) > 0 {
				expireDate := defaultSubscriptionExpireDate(defaultPlan, now)
				if err := tx.Model(&model.UserSubscription{}).
					Where("plan_id = ? AND status = ? AND expire_date > ?", id, "ACTIVE", now).
					Updates(map[string]interface{}{
						"plan_id":        defaultPlan.ID,
						"start_date":     now,
						"expire_date":    expireDate,
						"traffic_limit":  defaultPlan.TrafficLimit,
						"used_traffic":   uint64(0),
						"status":         "ACTIVE",
						"active_user_id": gorm.Expr("user_id"),
						"updated_at":     now,
					}).Error; err != nil {
					return err
				}
				for _, sub := range activeSubs {
					if tx.Migrator().HasTable(&model.SubscriptionToken{}) {
						if err := tx.Model(&model.SubscriptionToken{}).
							Where("user_id = ?", sub.UserID).
							Update("subscription_id", sub.ID).Error; err != nil {
							return err
						}
					}
				}
			}
		}

		if tx.Migrator().HasTable("plan_node_groups") {
			if err := tx.Exec("DELETE FROM plan_node_groups WHERE plan_id = ?", id).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.Plan{}).
			Where("id = ?", id).
			Updates(map[string]interface{}{
				"is_active":  false,
				"is_deleted": true,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		return ensureDefaultSubscriptionsForAllUsersTx(tx, defaultPlan, now)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// EnsureDefaultSubscriptions 确保没有有效订阅的用户拥有基础套餐订阅。
func (r *PlanRepository) EnsureDefaultSubscriptions(ctx context.Context) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		plan, err := ensureDefaultPlanTx(tx)
		if err != nil {
			return err
		}
		return ensureDefaultSubscriptionsForAllUsersTx(tx, plan, modelNow())
	})
}

func ensureDefaultPlanTx(tx *gorm.DB) (*model.Plan, error) {
	return ensureDefaultPlanTxExcept(tx, 0)
}

func ensureDefaultPlanTxExcept(tx *gorm.DB, excludeID uint64) (*model.Plan, error) {
	var defaults []model.Plan
	defaultQuery := tx.Where("is_default = ? AND is_deleted = ?", true, false)
	if excludeID > 0 {
		defaultQuery = defaultQuery.Where("id <> ?", excludeID)
	}
	if err := defaultQuery.Order("id ASC").Find(&defaults).Error; err != nil {
		return nil, err
	}
	if len(defaults) > 0 {
		keep := defaults[0]
		if len(defaults) > 1 {
			ids := make([]uint64, 0, len(defaults)-1)
			for _, plan := range defaults[1:] {
				ids = append(ids, plan.ID)
			}
			if err := tx.Model(&model.Plan{}).Where("id IN ?", ids).Update("is_default", false).Error; err != nil {
				return nil, err
			}
		}
		if !keep.IsActive {
			if err := tx.Model(&model.Plan{}).Where("id = ?", keep.ID).Update("is_active", true).Error; err != nil {
				return nil, err
			}
			keep.IsActive = true
		}
		return &keep, nil
	}

	var existing model.Plan
	existingQuery := tx.Where("is_deleted = ?", false)
	if excludeID > 0 {
		existingQuery = existingQuery.Where("id <> ?", excludeID)
	}
	result := existingQuery.Order("sort_weight ASC, id ASC").Limit(1).Find(&existing)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected > 0 {
		if err := tx.Model(&model.Plan{}).
			Where("id = ?", existing.ID).
			Updates(map[string]interface{}{
				"is_default": true,
				"is_active":  true,
			}).Error; err != nil {
			return nil, err
		}
		existing.IsDefault = true
		existing.IsActive = true
		return &existing, nil
	}

	plan := &model.Plan{
		Name:         defaultPlanName,
		Price:        0,
		Currency:     "USDT",
		TrafficLimit: 0,
		DurationDays: defaultPlanDurationDays,
		SortWeight:   -1000,
		IsActive:     true,
		IsDefault:    true,
		IsDeleted:    false,
	}
	if err := tx.Create(plan).Error; err != nil {
		return nil, err
	}
	return plan, nil
}

func createDefaultSubscriptionForUserTx(tx *gorm.DB, userID uint64, plan *model.Plan, now time.Time) (*model.UserSubscription, error) {
	expireDate := defaultSubscriptionExpireDate(plan, now)
	activeUserID := userID
	sub := &model.UserSubscription{
		UserID:       userID,
		PlanID:       plan.ID,
		StartDate:    now,
		ExpireDate:   expireDate,
		TrafficLimit: plan.TrafficLimit,
		UsedTraffic:  0,
		Status:       "ACTIVE",
		ActiveUserID: &activeUserID,
	}
	if err := tx.Create(sub).Error; err != nil {
		return nil, err
	}
	return sub, nil
}

func ensureDefaultSubscriptionsForAllUsersTx(tx *gorm.DB, plan *model.Plan, now time.Time) error {
	if !tx.Migrator().HasTable(&model.User{}) || !tx.Migrator().HasTable(&model.UserSubscription{}) {
		return nil
	}
	if err := tx.Model(&model.UserSubscription{}).
		Where("status = ? AND expire_date <= ?", "ACTIVE", now).
		Updates(map[string]interface{}{
			"status":         "EXPIRED",
			"active_user_id": nil,
			"updated_at":     now,
		}).Error; err != nil {
		return err
	}

	var users []model.User
	if err := tx.Model(&model.User{}).
		Where("NOT EXISTS (SELECT 1 FROM user_subscriptions s WHERE s.user_id = users.id AND s.status = ? AND s.expire_date > ?)", "ACTIVE", now).
		Order("id ASC").
		Find(&users).Error; err != nil {
		return err
	}
	if len(users) == 0 {
		return nil
	}

	for _, user := range users {
		sub, err := createDefaultSubscriptionForUserTx(tx, user.ID, plan, now)
		if err != nil {
			return err
		}
		if tx.Migrator().HasTable(&model.SubscriptionToken{}) {
			if err := tx.Model(&model.SubscriptionToken{}).
				Where("user_id = ?", user.ID).
				Update("subscription_id", sub.ID).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func defaultSubscriptionExpireDate(plan *model.Plan, now time.Time) time.Time {
	days := int(plan.DurationDays)
	if days <= 0 {
		days = defaultPlanDurationDays
	}
	return now.AddDate(0, 0, days)
}

// ListAll 列出所有套餐（含已下架）。
func (r *PlanRepository) ListAll(ctx context.Context) ([]model.Plan, error) {
	var plans []model.Plan
	err := r.db.WithContext(ctx).
		Where("is_deleted = ?", false).
		Order("sort_weight ASC, id ASC").
		Find(&plans).Error
	return plans, err
}

// Count 统计套餐数量。
func (r *PlanRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&model.Plan{}).Where("is_deleted = ?", false).Count(&total).Error
	return total, err
}

// FindNodeGroupIDs 查询套餐关联的节点组 ID 列表。
func (r *PlanRepository) FindNodeGroupIDs(ctx context.Context, planID uint64) ([]uint64, error) {
	var ids []uint64
	err := r.db.WithContext(ctx).
		Table("plan_node_groups").
		Where("plan_id = ?", planID).
		Pluck("node_group_id", &ids).Error
	return ids, err
}

// BindNodeGroups 绑定套餐与节点分组。
func (r *PlanRepository) BindNodeGroups(ctx context.Context, planID uint64, nodeGroupIDs []uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除现有绑定
		if err := tx.Exec("DELETE FROM plan_node_groups WHERE plan_id = ?", planID).Error; err != nil {
			return err
		}

		// 插入新绑定
		for _, ngID := range nodeGroupIDs {
			if err := tx.Exec("INSERT INTO plan_node_groups (plan_id, node_group_id) VALUES (?, ?)", planID, ngID).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

// NodeGroupRepository 节点分组数据访问。
type NodeGroupRepository struct {
	db *gorm.DB
}

// NewNodeGroupRepository 创建节点分组 Repository。
func NewNodeGroupRepository(db *gorm.DB) *NodeGroupRepository {
	return &NodeGroupRepository{db: db}
}

// Create 创建节点分组。
func (r *NodeGroupRepository) Create(ctx context.Context, group *model.NodeGroup) (*model.NodeGroup, error) {
	err := r.db.WithContext(ctx).Create(group).Error
	return group, err
}

// FindByID 根据 ID 查找节点分组。
func (r *NodeGroupRepository) FindByID(ctx context.Context, id uint64) (*model.NodeGroup, error) {
	var group model.NodeGroup
	err := r.db.WithContext(ctx).First(&group, id).Error
	if err != nil {
		return nil, err
	}
	return &group, nil
}

// FindByIDs 根据 ID 列表查询节点分组。
func (r *NodeGroupRepository) FindByIDs(ctx context.Context, ids []uint64) ([]model.NodeGroup, error) {
	ids, err := normalizeUint64IDs(ids)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []model.NodeGroup{}, nil
	}

	var groups []model.NodeGroup
	err = r.db.WithContext(ctx).
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&groups).Error
	return groups, err
}

// Update 更新节点分组。
func (r *NodeGroupRepository) Update(ctx context.Context, group *model.NodeGroup) error {
	return r.db.WithContext(ctx).Save(group).Error
}

// Delete 删除节点分组。
func (r *NodeGroupRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if tx.Migrator().HasTable(&model.NodeGroupNode{}) {
			if err := tx.Model(&model.NodeGroupNode{}).Where("node_group_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
		}
		if count == 0 {
			if err := tx.Model(&model.Node{}).Where("node_group_id = ?", id).Count(&count).Error; err != nil {
				return err
			}
		}
		if count > 0 {
			return ErrNodeGroupInUse
		}
		if tx.Migrator().HasTable("plan_node_groups") {
			if err := tx.Exec("DELETE FROM plan_node_groups WHERE node_group_id = ?", id).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.Node{}).Where("node_group_id = ?", id).Update("node_group_id", nil).Error; err != nil {
			return err
		}
		return tx.Delete(&model.NodeGroup{}, id).Error
	})
}

// List 列出所有节点分组。
func (r *NodeGroupRepository) List(ctx context.Context) ([]model.NodeGroup, error) {
	var groups []model.NodeGroup
	err := r.db.WithContext(ctx).
		Order("id ASC").
		Find(&groups).Error
	return groups, err
}

// BindNodes 绑定节点分组与节点，返回新增和移除的节点 ID。
func (r *NodeGroupRepository) BindNodes(ctx context.Context, groupID uint64, nodeIDs []uint64) (*NodeGroupNodeBindingChange, error) {
	normalized, err := normalizeUint64IDs(nodeIDs)
	if err != nil {
		return nil, err
	}

	change := &NodeGroupNodeBindingChange{NodeIDs: normalized}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(normalized) > 0 {
			var count int64
			if err := tx.Model(&model.Node{}).Where("id IN ?", normalized).Count(&count).Error; err != nil {
				return err
			}
			if count != int64(len(normalized)) {
				return ErrInvalidNodeID
			}
		}

		var existing []uint64
		if err := tx.Table("node_group_nodes").
			Where("node_group_id = ?", groupID).
			Pluck("node_id", &existing).Error; err != nil {
			return err
		}
		existing, err = normalizeUint64IDs(existing)
		if err != nil {
			return err
		}

		existingSet := make(map[uint64]struct{}, len(existing))
		for _, id := range existing {
			existingSet[id] = struct{}{}
		}
		newSet := make(map[uint64]struct{}, len(normalized))
		for _, id := range normalized {
			newSet[id] = struct{}{}
		}

		for _, id := range normalized {
			if _, ok := existingSet[id]; !ok {
				change.AddedNodeIDs = append(change.AddedNodeIDs, id)
			}
		}
		for _, id := range existing {
			if _, ok := newSet[id]; !ok {
				change.RemovedNodeIDs = append(change.RemovedNodeIDs, id)
			}
		}

		deleteQuery := tx.Where("node_group_id = ?", groupID)
		if len(normalized) > 0 {
			deleteQuery = deleteQuery.Where("node_id NOT IN ?", normalized)
		}
		if err := deleteQuery.Delete(&model.NodeGroupNode{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Node{}).Where("node_group_id = ?", groupID).Update("node_group_id", nil).Error; err != nil {
			return err
		}

		if len(normalized) == 0 {
			return nil
		}
		links := make([]model.NodeGroupNode, 0, len(normalized))
		for _, nodeID := range normalized {
			links = append(links, model.NodeGroupNode{NodeID: nodeID, NodeGroupID: groupID})
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "node_id"}, {Name: "node_group_id"}},
			DoNothing: true,
		}).Create(&links).Error
	})
	if err != nil {
		return nil, err
	}

	return change, nil
}

func normalizeUint64IDs(ids []uint64) ([]uint64, error) {
	seen := make(map[uint64]struct{}, len(ids))
	normalized := make([]uint64, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			return nil, ErrInvalidNodeID
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
}

// NodeRepository 节点数据访问。
type NodeRepository struct {
	db *gorm.DB
}

// NewNodeRepository 创建节点 Repository。
func NewNodeRepository(db *gorm.DB) *NodeRepository {
	return &NodeRepository{db: db}
}

// Create 创建节点。
func (r *NodeRepository) Create(ctx context.Context, node *model.Node) (*model.Node, error) {
	err := r.db.WithContext(ctx).Create(node).Error
	return node, err
}

// FindByID 根据 ID 查找节点。
func (r *NodeRepository) FindByID(ctx context.Context, id uint64) (*model.Node, error) {
	var node model.Node
	err := r.db.WithContext(ctx).First(&node, id).Error
	if err != nil {
		return nil, err
	}
	return &node, nil
}

// Update 更新节点。
func (r *NodeRepository) Update(ctx context.Context, node *model.Node) error {
	return r.db.WithContext(ctx).Save(node).Error
}

// Delete 删除节点。
func (r *NodeRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if tx.Migrator().HasTable(&model.NodeGroupNode{}) {
			if err := tx.Where("node_id = ?", id).Delete(&model.NodeGroupNode{}).Error; err != nil {
				return err
			}
		}
		return tx.Delete(&model.Node{}, id).Error
	})
}

// List 列出所有节点。
func (r *NodeRepository) List(ctx context.Context) ([]model.Node, error) {
	var nodes []model.Node
	err := r.db.WithContext(ctx).
		Order("sort_weight ASC, id ASC").
		Find(&nodes).Error
	return nodes, err
}

// Count 统计节点数量。
func (r *NodeRepository) Count(ctx context.Context) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&model.Node{}).Count(&total).Error
	return total, err
}

// FindByGroupID 根据节点组 ID 查询节点。
func (r *NodeRepository) FindByGroupID(ctx context.Context, groupID uint64, enabledOnly bool) ([]model.Node, error) {
	var nodes []model.Node
	db := r.db.WithContext(ctx)
	q := db.Model(&model.Node{})
	if db.Migrator().HasTable(&model.NodeGroupNode{}) {
		q = q.Select("DISTINCT nodes.*").
			Joins("LEFT JOIN node_group_nodes AS ngn ON ngn.node_id = nodes.id").
			Where("(ngn.node_group_id = ? OR nodes.node_group_id = ?)", groupID, groupID)
	} else {
		q = q.Where("node_group_id = ?", groupID)
	}
	if enabledOnly {
		q = q.Where("nodes.is_enabled = ?", true)
	}
	err := q.Order("nodes.sort_weight ASC, nodes.id ASC").Find(&nodes).Error
	return nodes, err
}

// FindGroupIDsByNodeID 查询节点所属分组 ID 列表。
func (r *NodeRepository) FindGroupIDsByNodeID(ctx context.Context, nodeID uint64) ([]uint64, error) {
	db := r.db.WithContext(ctx)
	var ids []uint64
	if db.Migrator().HasTable(&model.NodeGroupNode{}) {
		if err := db.Table("node_group_nodes").
			Where("node_id = ?", nodeID).
			Order("node_group_id ASC").
			Pluck("node_group_id", &ids).Error; err != nil {
			return nil, err
		}
	}

	var node model.Node
	if err := db.Select("node_group_id").First(&node, nodeID).Error; err != nil {
		return nil, err
	}
	if node.NodeGroupID != nil {
		ids = append(ids, *node.NodeGroupID)
	}
	return normalizeUint64IDs(ids)
}

// UpdateHeartbeat 更新节点最后心跳时间。
func (r *NodeRepository) UpdateHeartbeat(ctx context.Context, nodeID uint64) error {
	return r.db.WithContext(ctx).
		Model(&model.Node{}).
		Where("id = ?", nodeID).
		Update("last_heartbeat_at", modelNow()).Error
}

// MarkTrafficReportSuccess 记录节点最近一次成功流量上报。
func (r *NodeRepository) MarkTrafficReportSuccess(ctx context.Context, nodeID uint64, reportedAt time.Time, successAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.Node{}).
		Where("id = ?", nodeID).
		Updates(map[string]interface{}{
			"last_traffic_report_at":  reportedAt,
			"last_traffic_success_at": gorm.Expr("CASE WHEN last_traffic_success_at IS NULL OR last_traffic_success_at < ? THEN ? ELSE last_traffic_success_at END", successAt, successAt),
			"last_traffic_error_at":   nil,
			"traffic_error_count":     0,
			"last_traffic_error":      nil,
		}).Error
}

// MarkTrafficReportFailure 记录节点最近一次流量上报失败。
func (r *NodeRepository) MarkTrafficReportFailure(ctx context.Context, nodeID uint64, errMsg string, reportedAt time.Time) error {
	if len(errMsg) > 1000 {
		errMsg = errMsg[:1000]
	}
	return r.db.WithContext(ctx).
		Model(&model.Node{}).
		Where("id = ?", nodeID).
		Updates(map[string]interface{}{
			"last_traffic_report_at": reportedAt,
			"last_traffic_error_at":  reportedAt,
			"traffic_error_count":    gorm.Expr("traffic_error_count + ?", 1),
			"last_traffic_error":     errMsg,
		}).Error
}

// RelayRepository 中转节点数据访问。
type RelayRepository struct {
	db *gorm.DB
}

// NewRelayRepository 创建中转节点 Repository。
func NewRelayRepository(db *gorm.DB) *RelayRepository {
	return &RelayRepository{db: db}
}

// Create 创建中转节点。
func (r *RelayRepository) Create(ctx context.Context, relay *model.Relay) (*model.Relay, error) {
	err := r.db.WithContext(ctx).Create(relay).Error
	return relay, err
}

// FindByID 根据 ID 查找中转节点。
func (r *RelayRepository) FindByID(ctx context.Context, id uint64) (*model.Relay, error) {
	var relay model.Relay
	err := r.db.WithContext(ctx).First(&relay, id).Error
	if err != nil {
		return nil, err
	}
	return &relay, nil
}

// Update 更新中转节点。
func (r *RelayRepository) Update(ctx context.Context, relay *model.Relay) error {
	return r.db.WithContext(ctx).Save(relay).Error
}

// Delete 删除中转节点。存在启用后端时拒绝删除，避免留下仍在订阅中可见的入口。
func (r *RelayRepository) Delete(ctx context.Context, id uint64) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var enabledBackends int64
		if err := tx.Model(&model.RelayBackend{}).
			Where("relay_id = ? AND is_enabled = ?", id, true).
			Count(&enabledBackends).Error; err != nil {
			return err
		}
		if enabledBackends > 0 {
			return ErrRelayHasEnabledBackends
		}
		return tx.Delete(&model.Relay{}, id).Error
	})
}

// List 列出所有中转节点。
func (r *RelayRepository) List(ctx context.Context) ([]model.Relay, error) {
	var relays []model.Relay
	err := r.db.WithContext(ctx).
		Order("id ASC").
		Find(&relays).Error
	return relays, err
}

// UpdateHeartbeat 更新中转节点心跳状态。
func (r *RelayRepository) UpdateHeartbeat(ctx context.Context, relayID uint64, version string) error {
	updates := map[string]interface{}{
		"last_heartbeat_at": modelNow(),
		"status":            "online",
	}
	if version != "" {
		updates["agent_version"] = version
	}
	return r.db.WithContext(ctx).
		Model(&model.Relay{}).
		Where("id = ?", relayID).
		Updates(updates).Error
}

// RelayBackendRepository 中转后端绑定数据访问。
type RelayBackendRepository struct {
	db *gorm.DB
}

// NewRelayBackendRepository 创建中转后端 Repository。
func NewRelayBackendRepository(db *gorm.DB) *RelayBackendRepository {
	return &RelayBackendRepository{db: db}
}

// ListByRelayID 查询中转节点后端绑定。
func (r *RelayBackendRepository) ListByRelayID(ctx context.Context, relayID uint64) ([]model.RelayBackend, error) {
	var backends []model.RelayBackend
	err := r.db.WithContext(ctx).
		Preload("ExitNode").
		Where("relay_id = ?", relayID).
		Order("sort_weight ASC, id ASC").
		Find(&backends).Error
	return backends, err
}

// ListEnabledByExitNodeIDs 查询指向指定出口节点的启用中转绑定。
func (r *RelayBackendRepository) ListEnabledByExitNodeIDs(ctx context.Context, exitNodeIDs []uint64) ([]model.RelayBackend, error) {
	exitNodeIDs, err := normalizeUint64IDs(exitNodeIDs)
	if err != nil {
		return nil, err
	}
	if len(exitNodeIDs) == 0 {
		return []model.RelayBackend{}, nil
	}
	var backends []model.RelayBackend
	err = r.db.WithContext(ctx).
		Preload("Relay").
		Joins("JOIN relays ON relays.id = relay_backends.relay_id").
		Where("relay_backends.exit_node_id IN ? AND relay_backends.is_enabled = ? AND relays.is_enabled = ?", exitNodeIDs, true, true).
		Order("relay_backends.sort_weight ASC, relay_backends.id ASC").
		Find(&backends).Error
	return backends, err
}

// SaveForRelay 全量保存一个中转节点的后端绑定列表。
func (r *RelayBackendRepository) SaveForRelay(ctx context.Context, relayID uint64, backends []model.RelayBackend) ([]model.RelayBackend, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("relay_id = ?", relayID).Delete(&model.RelayBackend{}).Error; err != nil {
			return err
		}
		if len(backends) == 0 {
			return nil
		}
		for i := range backends {
			backends[i].ID = 0
			backends[i].RelayID = relayID
		}
		return tx.Create(&backends).Error
	})
	if err != nil {
		return nil, err
	}
	return r.ListByRelayID(ctx, relayID)
}

// RelayConfigTaskRepository 中转配置任务数据访问。
type RelayConfigTaskRepository struct {
	db *gorm.DB
}

// NewRelayConfigTaskRepository 创建中转配置任务 Repository。
func NewRelayConfigTaskRepository(db *gorm.DB) *RelayConfigTaskRepository {
	return &RelayConfigTaskRepository{db: db}
}

// Create 创建中转配置任务。
func (r *RelayConfigTaskRepository) Create(ctx context.Context, task *model.RelayConfigTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// ClaimPendingByRelayID 原子认领中转节点待执行任务。
func (r *RelayConfigTaskRepository) ClaimPendingByRelayID(ctx context.Context, relayID uint64, maxRetries int, lockToken string, lockedAt time.Time, limit int) ([]model.RelayConfigTask, error) {
	if limit <= 0 {
		limit = 50
	}

	var tasks []model.RelayConfigTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("relay_id = ? AND status = ? AND retry_count < ? AND scheduled_at <= ?", relayID, "PENDING", maxRetries, lockedAt).
			Order("scheduled_at ASC").
			Limit(limit)
		if tx.Dialector.Name() == "mysql" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := q.Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}

		ids := make([]uint64, 0, len(tasks))
		for _, task := range tasks {
			ids = append(ids, task.ID)
		}
		if err := tx.Model(&model.RelayConfigTask{}).
			Where("id IN ? AND status = ?", ids, "PENDING").
			Updates(map[string]interface{}{
				"locked_at":  lockedAt,
				"lock_token": lockToken,
				"status":     "PROCESSING",
			}).Error; err != nil {
			return err
		}

		return tx.Where("id IN ? AND status = ? AND lock_token = ?", ids, "PROCESSING", lockToken).
			Order("scheduled_at ASC").
			Find(&tasks).Error
	})
	return tasks, err
}

// FindByID 根据 ID 查找中转配置任务。
func (r *RelayConfigTaskRepository) FindByID(ctx context.Context, id uint64) (*model.RelayConfigTask, error) {
	var task model.RelayConfigTask
	err := r.db.WithContext(ctx).First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// MarkDone 标记中转配置任务完成。
func (r *RelayConfigTaskRepository) MarkDone(ctx context.Context, taskID uint64, executedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.RelayConfigTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      "DONE",
			"executed_at": executedAt,
			"locked_at":   nil,
			"lock_token":  nil,
		}).Error
}

// MarkFailed 标记中转配置任务失败。
func (r *RelayConfigTaskRepository) MarkFailed(ctx context.Context, taskID uint64, errMsg string, executedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.RelayConfigTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      "FAILED",
			"last_error":  errMsg,
			"executed_at": executedAt,
			"retry_count": gorm.Expr("retry_count + 1"),
		}).Error
}

// RetryFailedAndStaleTasks 将失败任务和超时锁定的中转配置任务重新排队。
func (r *RelayConfigTaskRepository) RetryFailedAndStaleTasks(ctx context.Context, maxRetries int, lockTTL time.Duration) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&model.RelayConfigTask{}).
		Where("status = ? AND retry_count < ?", "FAILED", maxRetries).
		Updates(map[string]interface{}{
			"status":     "PENDING",
			"locked_at":  nil,
			"lock_token": nil,
		})
	if result.Error != nil {
		return 0, result.Error
	}

	total := result.RowsAffected
	if lockTTL > 0 {
		staleBefore := time.Now().Add(-lockTTL)
		stale := r.db.WithContext(ctx).
			Model(&model.RelayConfigTask{}).
			Where("status = ? AND retry_count < ? AND locked_at IS NOT NULL AND locked_at < ?", "PROCESSING", maxRetries, staleBefore).
			Updates(map[string]interface{}{
				"status":      "PENDING",
				"locked_at":   nil,
				"lock_token":  nil,
				"retry_count": gorm.Expr("retry_count + 1"),
			})
		if stale.Error != nil {
			return total, stale.Error
		}
		total += stale.RowsAffected
	}

	return total, nil
}

// RelayTrafficSnapshotRepository 中转线路级流量快照数据访问。
type RelayTrafficSnapshotRepository struct {
	db *gorm.DB
}

// NewRelayTrafficSnapshotRepository 创建中转流量快照 Repository。
func NewRelayTrafficSnapshotRepository(db *gorm.DB) *RelayTrafficSnapshotRepository {
	return &RelayTrafficSnapshotRepository{db: db}
}

// CreateBatch 批量创建中转流量快照。
func (r *RelayTrafficSnapshotRepository) CreateBatch(ctx context.Context, snapshots []model.RelayTrafficSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&snapshots).Error
}

// NodeAccessTaskRepository 节点访问任务数据访问。
type NodeAccessTaskRepository struct {
	db *gorm.DB
}

// NewNodeAccessTaskRepository 创建节点访问任务 Repository。
func NewNodeAccessTaskRepository(db *gorm.DB) *NodeAccessTaskRepository {
	return &NodeAccessTaskRepository{db: db}
}

// Create 创建任务。
func (r *NodeAccessTaskRepository) Create(ctx context.Context, task *model.NodeAccessTask) error {
	return r.db.WithContext(ctx).Create(task).Error
}

// FindPendingByNodeID 查询节点的待执行任务。
func (r *NodeAccessTaskRepository) FindPendingByNodeID(ctx context.Context, nodeID uint64, maxRetries int) ([]model.NodeAccessTask, error) {
	var tasks []model.NodeAccessTask
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND status = ? AND retry_count < ?", nodeID, "PENDING", maxRetries).
		Order("scheduled_at ASC").
		Limit(50).
		Find(&tasks).Error
	return tasks, err
}

// ClaimPendingByNodeID 原子认领节点待执行任务。
func (r *NodeAccessTaskRepository) ClaimPendingByNodeID(ctx context.Context, nodeID uint64, maxRetries int, lockToken string, lockedAt time.Time, limit int) ([]model.NodeAccessTask, error) {
	if limit <= 0 {
		limit = 50
	}

	var tasks []model.NodeAccessTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("node_id = ? AND status = ? AND retry_count < ? AND scheduled_at <= ?", nodeID, "PENDING", maxRetries, lockedAt).
			Order("scheduled_at ASC").
			Limit(limit)
		if tx.Dialector.Name() == "mysql" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := q.Find(&tasks).Error; err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}

		ids := make([]uint64, 0, len(tasks))
		for _, task := range tasks {
			ids = append(ids, task.ID)
		}

		if err := tx.Model(&model.NodeAccessTask{}).
			Where("id IN ? AND status = ?", ids, "PENDING").
			Updates(map[string]interface{}{
				"locked_at":  lockedAt,
				"lock_token": lockToken,
				"status":     "PROCESSING",
			}).Error; err != nil {
			return err
		}

		return tx.Where("id IN ? AND status = ? AND lock_token = ?", ids, "PROCESSING", lockToken).
			Order("scheduled_at ASC").
			Find(&tasks).Error
	})
	return tasks, err
}

// FindByID 根据 ID 查找任务。
func (r *NodeAccessTaskRepository) FindByID(ctx context.Context, id uint64) (*model.NodeAccessTask, error) {
	var task model.NodeAccessTask
	err := r.db.WithContext(ctx).First(&task, id).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// UpdateLock 更新任务锁。
func (r *NodeAccessTaskRepository) UpdateLock(ctx context.Context, taskID uint64, lockedAt time.Time, lockToken string) error {
	return r.db.WithContext(ctx).
		Model(&model.NodeAccessTask{}).
		Where("id = ? AND status = ?", taskID, "PENDING").
		Updates(map[string]interface{}{
			"locked_at":  lockedAt,
			"lock_token": lockToken,
			"status":     "PROCESSING",
		}).Error
}

// MarkDone 标记任务完成。
func (r *NodeAccessTaskRepository) MarkDone(ctx context.Context, taskID uint64, executedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.NodeAccessTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      "DONE",
			"executed_at": executedAt,
			"locked_at":   nil,
			"lock_token":  nil,
		}).Error
}

// MarkFailed 标记任务失败。
func (r *NodeAccessTaskRepository) MarkFailed(ctx context.Context, taskID uint64, errMsg string, executedAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.NodeAccessTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      "FAILED",
			"last_error":  errMsg,
			"executed_at": executedAt,
			"retry_count": gorm.Expr("retry_count + 1"),
		}).Error
}

// RetryFailedTasks 将失败且未超过重试次数的任务重新排队。
// 返回成功重新排队的任务数量。
func (r *NodeAccessTaskRepository) RetryFailedTasks(ctx context.Context) (int64, error) {
	return r.RetryFailedAndStaleTasks(ctx, 10, 0)
}

// RetryFailedAndStaleTasks 将失败任务和超时锁定任务重新排队。
func (r *NodeAccessTaskRepository) RetryFailedAndStaleTasks(ctx context.Context, maxRetries int, lockTTL time.Duration) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&model.NodeAccessTask{}).
		Where("status = ? AND retry_count < ?", "FAILED", maxRetries).
		Updates(map[string]interface{}{
			"status":     "PENDING",
			"locked_at":  nil,
			"lock_token": nil,
		})
	if result.Error != nil {
		return 0, result.Error
	}

	total := result.RowsAffected
	if lockTTL > 0 {
		staleBefore := time.Now().Add(-lockTTL)
		stale := r.db.WithContext(ctx).
			Model(&model.NodeAccessTask{}).
			Where("status = ? AND retry_count < ? AND locked_at IS NOT NULL AND locked_at < ?", "PROCESSING", maxRetries, staleBefore).
			Updates(map[string]interface{}{
				"status":      "PENDING",
				"locked_at":   nil,
				"lock_token":  nil,
				"retry_count": gorm.Expr("retry_count + 1"),
			})
		if stale.Error != nil {
			return total, stale.Error
		}
		total += stale.RowsAffected
	}

	return total, nil
}

// modelNow 返回当前时间，用于 GORM 更新。
func modelNow() time.Time {
	return time.Now()
}

// RedeemCodeRepository 兑换码数据访问。
type RedeemCodeRepository struct {
	db *gorm.DB
}

// NewRedeemCodeRepository 创建兑换码 Repository。
func NewRedeemCodeRepository(db *gorm.DB) *RedeemCodeRepository {
	return &RedeemCodeRepository{db: db}
}

// Create 创建兑换码。
func (r *RedeemCodeRepository) Create(ctx context.Context, code *model.RedeemCode) error {
	return r.db.WithContext(ctx).Create(code).Error
}

// FindByCode 根据兑换码字符串查找。
func (r *RedeemCodeRepository) FindByCode(ctx context.Context, code string) (*model.RedeemCode, error) {
	var redeemCode model.RedeemCode
	err := r.db.WithContext(ctx).Where("code = ?", code).First(&redeemCode).Error
	if err != nil {
		return nil, err
	}
	return &redeemCode, nil
}

// MarkUsed 标记兑换码已使用（防止并发重复使用）。
func (r *RedeemCodeRepository) MarkUsed(ctx context.Context, codeID uint64, userID uint64, usedAt time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&model.RedeemCode{}).
		Where("id = ? AND is_used = ?", codeID, false).
		Updates(map[string]interface{}{
			"is_used":         true,
			"used_by_user_id": userID,
			"used_at":         usedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("兑换码已被使用")
	}
	return nil
}

// List 分页查询兑换码列表。
func (r *RedeemCodeRepository) List(ctx context.Context, page, size int) ([]model.RedeemCode, int64, error) {
	var codes []model.RedeemCode
	var total int64

	q := r.db.WithContext(ctx).Model(&model.RedeemCode{})
	q.Count(&total)

	offset := (page - 1) * size
	err := q.Offset(offset).Limit(size).Order("created_at DESC").Find(&codes).Error
	return codes, total, err
}

// OrderRepository 订单数据访问。
type OrderRepository struct {
	db *gorm.DB
}

// NewOrderRepository 创建订单 Repository。
func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{db: db}
}

// Create 创建订单。
func (r *OrderRepository) Create(ctx context.Context, order *model.Order) error {
	return r.db.WithContext(ctx).Create(order).Error
}

// FindByOrderNo 根据订单号查找订单。
func (r *OrderRepository) FindByOrderNo(ctx context.Context, orderNo string) (*model.Order, error) {
	var order model.Order
	err := r.db.WithContext(ctx).Where("order_no = ?", orderNo).First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// ListByUserID 分页查询用户订单。
func (r *OrderRepository) ListByUserID(ctx context.Context, userID uint64, page, size int) ([]model.Order, int64, error) {
	var orders []model.Order
	var total int64

	q := r.db.WithContext(ctx).Model(&model.Order{}).Where("user_id = ?", userID)
	q.Count(&total)

	offset := (page - 1) * size
	err := q.Offset(offset).Limit(size).Order("created_at DESC").Find(&orders).Error
	return orders, total, err
}

// ListAll 分页查询所有订单（管理后台用）。
func (r *OrderRepository) ListAll(ctx context.Context, page, size int) ([]model.Order, int64, error) {
	var orders []model.Order
	var total int64

	q := r.db.WithContext(ctx).Model(&model.Order{})
	q.Count(&total)

	offset := (page - 1) * size
	err := q.Offset(offset).Limit(size).Order("created_at DESC").Find(&orders).Error
	return orders, total, err
}

// MarkPaid 标记订单已支付。
func (r *OrderRepository) MarkPaid(ctx context.Context, orderID uint64, paidAt time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.Order{}).
		Where("id = ?", orderID).
		Updates(map[string]interface{}{
			"status":  "PAID",
			"paid_at": paidAt,
		}).Error
}

// ExpireByTime 将所有过期且状态为 PENDING 的订单标记为 EXPIRED。
// 返回被更新的订单数量。
func (r *OrderRepository) ExpireByTime(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&model.Order{}).
		Where("status = ? AND expired_at < ?", "PENDING", time.Now()).
		Update("status", "EXPIRED")
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// PaymentRecordRepository 支付记录数据访问。
type PaymentRecordRepository struct {
	db *gorm.DB
}

// NewPaymentRecordRepository 创建支付记录 Repository。
func NewPaymentRecordRepository(db *gorm.DB) *PaymentRecordRepository {
	return &PaymentRecordRepository{db: db}
}

// Create 创建支付记录。
func (r *PaymentRecordRepository) Create(ctx context.Context, record *model.PaymentRecord) error {
	return r.db.WithContext(ctx).Create(record).Error
}

// FindByTxID 根据交易哈希查找支付记录。
func (r *PaymentRecordRepository) FindByTxID(ctx context.Context, txID string) (*model.PaymentRecord, error) {
	var record model.PaymentRecord
	err := r.db.WithContext(ctx).Where("tx_id = ?", txID).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// TrafficSnapshotRepository 流量快照数据访问。
type TrafficSnapshotRepository struct {
	db *gorm.DB
}

// NewTrafficSnapshotRepository 创建流量快照 Repository。
func NewTrafficSnapshotRepository(db *gorm.DB) *TrafficSnapshotRepository {
	return &TrafficSnapshotRepository{db: db}
}

// Create 创建流量快照。
func (r *TrafficSnapshotRepository) Create(ctx context.Context, snapshot *model.TrafficSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

// FindLatest 查找指定节点和用户的最新快照。
func (r *TrafficSnapshotRepository) FindLatest(ctx context.Context, nodeID uint64, xrayUserKey string) (*model.TrafficSnapshot, error) {
	var snapshot model.TrafficSnapshot
	err := r.db.WithContext(ctx).
		Where("node_id = ? AND xray_user_key = ?", nodeID, xrayUserKey).
		Order("captured_at DESC, id DESC").
		First(&snapshot).Error
	if err != nil {
		return nil, err
	}
	return &snapshot, nil
}

// UsageLedgerRepository 流量账本数据访问。
type UsageLedgerRepository struct {
	db *gorm.DB
}

// UsageLedgerTotal 表示流量账本汇总。
type UsageLedgerTotal struct {
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	Total    uint64 `json:"total"`
}

// UsageLedgerWithNode 表示带节点名称的流量账本记录。
type UsageLedgerWithNode struct {
	ID             uint64    `json:"id"`
	UserID         uint64    `json:"user_id"`
	SubscriptionID *uint64   `json:"subscription_id"`
	NodeID         uint64    `json:"node_id"`
	NodeName       *string   `json:"node_name,omitempty"`
	DeltaUpload    uint64    `json:"delta_upload"`
	DeltaDownload  uint64    `json:"delta_download"`
	DeltaTotal     uint64    `json:"delta_total"`
	RecordedAt     time.Time `json:"recorded_at"`
}

// NewUsageLedgerRepository 创建流量账本 Repository。
func NewUsageLedgerRepository(db *gorm.DB) *UsageLedgerRepository {
	return &UsageLedgerRepository{db: db}
}

// Create 创建账本记录。
func (r *UsageLedgerRepository) Create(ctx context.Context, ledger *model.UsageLedger) error {
	return r.db.WithContext(ctx).Create(ledger).Error
}

// ListByUser 查询指定用户在时间范围内的账本记录；subscriptionID 为空时查询该用户全部订阅。
func (r *UsageLedgerRepository) ListByUser(ctx context.Context, userID uint64, subscriptionID *uint64, startAt, endAt time.Time) ([]model.UsageLedger, error) {
	var ledgers []model.UsageLedger
	q := r.db.WithContext(ctx).
		Where("user_id = ? AND recorded_at >= ? AND recorded_at < ?", userID, startAt, endAt)
	if subscriptionID != nil {
		q = q.Where("subscription_id = ?", *subscriptionID)
	}
	err := q.Order("recorded_at ASC, id ASC").Find(&ledgers).Error
	return ledgers, err
}

// SumByUser 汇总指定用户在时间范围内的账本记录；startAt/endAt 为空时不限制对应边界。
func (r *UsageLedgerRepository) SumByUser(ctx context.Context, userID uint64, subscriptionID *uint64, startAt, endAt *time.Time) (UsageLedgerTotal, error) {
	var total UsageLedgerTotal
	q := r.db.WithContext(ctx).
		Model(&model.UsageLedger{}).
		Select("COALESCE(SUM(delta_upload), 0) AS upload, COALESCE(SUM(delta_download), 0) AS download, COALESCE(SUM(delta_total), 0) AS total").
		Where("user_id = ?", userID)
	if subscriptionID != nil {
		q = q.Where("subscription_id = ?", *subscriptionID)
	}
	if startAt != nil {
		q = q.Where("recorded_at >= ?", *startAt)
	}
	if endAt != nil {
		q = q.Where("recorded_at < ?", *endAt)
	}
	err := q.Scan(&total).Error
	return total, err
}

// RecentByUser 查询指定用户最近的账本记录，并附带节点名称。
func (r *UsageLedgerRepository) RecentByUser(ctx context.Context, userID uint64, subscriptionID *uint64, limit int) ([]UsageLedgerWithNode, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var records []UsageLedgerWithNode
	q := r.db.WithContext(ctx).
		Table("usage_ledgers AS ul").
		Select("ul.id, ul.user_id, ul.subscription_id, ul.node_id, nodes.name AS node_name, ul.delta_upload, ul.delta_download, ul.delta_total, ul.recorded_at").
		Joins("LEFT JOIN nodes ON nodes.id = ul.node_id").
		Where("ul.user_id = ?", userID)
	if subscriptionID != nil {
		q = q.Where("ul.subscription_id = ?", *subscriptionID)
	}
	err := q.Order("ul.recorded_at DESC, ul.id DESC").Limit(limit).Scan(&records).Error
	return records, err
}

// UpdateStatus 更新订阅状态。
func (r *SubscriptionRepository) UpdateStatus(ctx context.Context, subID uint64, status string) error {
	updates := map[string]interface{}{"status": status}
	if status != "ACTIVE" {
		updates["active_user_id"] = nil
	}
	return r.db.WithContext(ctx).
		Model(&model.UserSubscription{}).
		Where("id = ?", subID).
		Updates(updates).Error
}

// Update 更新订阅完整信息。
func (r *SubscriptionRepository) Update(ctx context.Context, sub *model.UserSubscription) error {
	return r.db.WithContext(ctx).Save(sub).Error
}
