package handler

import (
	"context"
	"errors"
	"strconv"
	"time"

	"suiyue/internal/middleware"
	"suiyue/internal/model"
	"suiyue/internal/platform/response"
	"suiyue/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// UsageHandler 提供用户流量用量查询接口。
type UsageHandler struct {
	ledgerRepo *repository.UsageLedgerRepository
	userRepo   *repository.UserRepository
	subRepo    *repository.SubscriptionRepository
	planRepo   *repository.PlanRepository
}

// NewUsageHandler 创建用户流量用量处理器。
func NewUsageHandler(ledgerRepo *repository.UsageLedgerRepository, userRepo *repository.UserRepository, subRepo *repository.SubscriptionRepository, planRepo *repository.PlanRepository) *UsageHandler {
	return &UsageHandler{
		ledgerRepo: ledgerRepo,
		userRepo:   userRepo,
		subRepo:    subRepo,
		planRepo:   planRepo,
	}
}

// GetCurrentUserUsage 处理 GET /api/user/usage — 当前用户用量明细。
func (h *UsageHandler) GetCurrentUserUsage(c *gin.Context) {
	userID, ok := middleware.GetUserID(c)
	if !ok {
		response.HandleError(c, response.ErrUnauthorized)
		return
	}
	h.writeUsage(c, userID, false)
}

// GetAdminUserUsage 处理 GET /api/admin/users/:id/usage — 管理员查询指定用户用量明细。
func (h *UsageHandler) GetAdminUserUsage(c *gin.Context) {
	userID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.HandleError(c, response.ErrBadRequest)
		return
	}
	h.writeUsage(c, userID, true)
}

type usageBucket struct {
	Date     string `json:"date,omitempty"`
	Week     string `json:"week,omitempty"`
	Month    string `json:"month,omitempty"`
	StartAt  string `json:"start_at,omitempty"`
	EndAt    string `json:"end_at,omitempty"`
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
	Total    uint64 `json:"total"`
}

func (h *UsageHandler) writeUsage(c *gin.Context, userID uint64, includeUser bool) {
	if h.ledgerRepo == nil || h.userRepo == nil || h.subRepo == nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	ctx := c.Request.Context()
	user, err := h.userRepo.FindByID(ctx, userID)
	if err != nil {
		response.HandleError(c, response.ErrNotFound)
		return
	}

	days := clampQueryInt(c, "days", 30, 7, 365)
	weeks := clampQueryInt(c, "weeks", 8, 4, 52)
	months := clampQueryInt(c, "months", 12, 3, 36)
	recentLimit := clampQueryInt(c, "recent", 50, 1, 200)

	sub, hasActive, err := h.usageSubscription(ctx, userID)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	var subID *uint64
	if sub != nil {
		id := sub.ID
		subID = &id
	}

	now := time.Now()
	todayStart := startOfDay(now)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	weekStart := startOfWeek(now)
	monthStart := startOfMonth(now)
	dailyStart := todayStart.AddDate(0, 0, -(days - 1))
	weeklyStart := weekStart.AddDate(0, 0, -7*(weeks-1))
	monthlyStart := monthStart.AddDate(0, -(months - 1), 0)
	rangeStart := minTime(dailyStart, minTime(weeklyStart, monthlyStart))

	ledgers, err := h.ledgerRepo.ListByUser(ctx, userID, subID, rangeStart, tomorrowStart)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	daily := buildDailyBuckets(ledgers, dailyStart, days)
	weekly := buildWeeklyBuckets(ledgers, weeklyStart, weeks)
	monthly := buildMonthlyBuckets(ledgers, monthlyStart, months)

	summaryEnd := tomorrowStart
	subscriptionTotal, err := h.ledgerRepo.SumByUser(ctx, userID, subID, nil, &summaryEnd)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}
	allTimeTotal, err := h.ledgerRepo.SumByUser(ctx, userID, nil, nil, &summaryEnd)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	recent, err := h.ledgerRepo.RecentByUser(ctx, userID, subID, recentLimit)
	if err != nil {
		response.HandleError(c, response.ErrInternalServer)
		return
	}

	data := gin.H{
		"scope":                   "all_user",
		"has_active_subscription": hasActive,
		"summary": gin.H{
			"today":                 sumLedgersBetween(ledgers, todayStart, tomorrowStart),
			"current_week":          sumLedgersBetween(ledgers, weekStart, tomorrowStart),
			"current_month":         sumLedgersBetween(ledgers, monthStart, tomorrowStart),
			"subscription_to_today": subscriptionTotal,
			"all_time_to_today":     allTimeTotal,
		},
		"daily":   daily,
		"weekly":  weekly,
		"monthly": monthly,
		"recent":  recent,
		"range": gin.H{
			"days":       days,
			"weeks":      weeks,
			"months":     months,
			"start_date": dailyStart.Format("2006-01-02"),
			"end_date":   todayStart.Format("2006-01-02"),
		},
	}
	if includeUser {
		data["user"] = user.ToPublic()
	}
	if sub != nil {
		data["scope"] = "subscription"
		data["subscription"] = subscriptionSummary(sub)
		if h.planRepo != nil {
			if plan, err := h.planRepo.FindByID(ctx, sub.PlanID); err == nil {
				data["plan"] = planSummary(plan)
				data["plan_name"] = plan.Name
			}
		}
	}

	response.Success(c, data)
}

func (h *UsageHandler) usageSubscription(ctx context.Context, userID uint64) (*model.UserSubscription, bool, error) {
	sub, err := h.subRepo.FindActiveByUserID(ctx, userID)
	if err == nil {
		return sub, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	sub, err = h.subRepo.FindLatestByUserID(ctx, userID)
	if err == nil {
		return sub, false, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func clampQueryInt(c *gin.Context, key string, fallback, min, max int) int {
	value := fallback
	if raw := c.Query(key); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func startOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func startOfWeek(t time.Time) time.Time {
	day := startOfDay(t)
	weekday := int(day.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return day.AddDate(0, 0, -(weekday - 1))
}

func startOfMonth(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func buildDailyBuckets(ledgers []model.UsageLedger, start time.Time, days int) []usageBucket {
	buckets := make([]usageBucket, 0, days)
	lookup := make(map[string]int, days)
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		key := day.Format("2006-01-02")
		lookup[key] = i
		buckets = append(buckets, usageBucket{Date: key, StartAt: day.Format(time.RFC3339), EndAt: day.AddDate(0, 0, 1).Format(time.RFC3339)})
	}
	for _, ledger := range ledgers {
		key := ledger.RecordedAt.Format("2006-01-02")
		if idx, ok := lookup[key]; ok {
			addLedgerToBucket(&buckets[idx], ledger)
		}
	}
	return buckets
}

func buildWeeklyBuckets(ledgers []model.UsageLedger, start time.Time, weeks int) []usageBucket {
	buckets := make([]usageBucket, 0, weeks)
	lookup := make(map[string]int, weeks)
	for i := 0; i < weeks; i++ {
		weekStart := start.AddDate(0, 0, i*7)
		weekEnd := weekStart.AddDate(0, 0, 7)
		key := weekStart.Format("2006-01-02")
		lookup[key] = i
		buckets = append(buckets, usageBucket{
			Week:    key,
			StartAt: weekStart.Format("2006-01-02"),
			EndAt:   weekEnd.AddDate(0, 0, -1).Format("2006-01-02"),
		})
	}
	for _, ledger := range ledgers {
		key := startOfWeek(ledger.RecordedAt).Format("2006-01-02")
		if idx, ok := lookup[key]; ok {
			addLedgerToBucket(&buckets[idx], ledger)
		}
	}
	return buckets
}

func buildMonthlyBuckets(ledgers []model.UsageLedger, start time.Time, months int) []usageBucket {
	buckets := make([]usageBucket, 0, months)
	lookup := make(map[string]int, months)
	for i := 0; i < months; i++ {
		monthStart := start.AddDate(0, i, 0)
		key := monthStart.Format("2006-01")
		lookup[key] = i
		buckets = append(buckets, usageBucket{
			Month:   key,
			StartAt: monthStart.Format("2006-01-02"),
			EndAt:   monthStart.AddDate(0, 1, -1).Format("2006-01-02"),
		})
	}
	for _, ledger := range ledgers {
		key := startOfMonth(ledger.RecordedAt).Format("2006-01")
		if idx, ok := lookup[key]; ok {
			addLedgerToBucket(&buckets[idx], ledger)
		}
	}
	return buckets
}

func sumLedgersBetween(ledgers []model.UsageLedger, start, end time.Time) repository.UsageLedgerTotal {
	var total repository.UsageLedgerTotal
	for _, ledger := range ledgers {
		if ledger.RecordedAt.Before(start) || !ledger.RecordedAt.Before(end) {
			continue
		}
		total.Upload += ledger.DeltaUpload
		total.Download += ledger.DeltaDownload
		total.Total += ledger.DeltaTotal
	}
	return total
}

func addLedgerToBucket(bucket *usageBucket, ledger model.UsageLedger) {
	bucket.Upload += ledger.DeltaUpload
	bucket.Download += ledger.DeltaDownload
	bucket.Total += ledger.DeltaTotal
}
