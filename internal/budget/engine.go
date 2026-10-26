package budget

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/cache"
	"golang.org/x/sync/singleflight"
)

var configCache = cache.NewTTLCache[string, map[string]string](10 * time.Second)
var configGroup singleflight.Group

// InitConfigCache initializes the global config cache with the configured TTL.
func InitConfigCache() {
	ttlSec := viper.GetInt("budget.config_cache_ttl_seconds")
	if ttlSec <= 0 {
		ttlSec = 10
	}
	configCache = cache.NewTTLCache[string, map[string]string](time.Duration(ttlSec) * time.Second)
}

// BudgetExceededError is returned when a budget limit is reached.
type BudgetExceededError struct {
	WindowName   string
	CurrentSpend float64
	Limit        float64
	Status       string
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("budget exceeded for %s window: limit %f, current spend %f", e.WindowName, e.Limit, e.CurrentSpend)
}

// WindowInfo holds information about a specific time window for budgeting.
type WindowInfo struct {
	Name string
	Key  string
	TTL  int
}

func getWindowConfigs(keyHash string, now time.Time) []WindowInfo {
	// Daily
	nextDay := now.AddDate(0, 0, 1).Truncate(24 * time.Hour)
	dailyTTL := int(nextDay.Sub(now).Seconds())

	// Hourly
	nextHour := now.Add(time.Hour).Truncate(time.Hour)
	hourlyTTL := int(nextHour.Sub(now).Seconds())

	// Minute
	nextMinute := now.Add(time.Minute).Truncate(time.Minute)
	minuteTTL := int(nextMinute.Sub(now).Seconds())

	// Weekly (Monday of next week)
	daysToMonday := 8 - int(now.Weekday())
	if now.Weekday() == time.Sunday {
		daysToMonday = 1
	}
	nextWeek := now.AddDate(0, 0, daysToMonday).Truncate(24 * time.Hour)
	weeklyTTL := int(nextWeek.Sub(now).Seconds())
	year, week := now.ISOWeek()

	// Monthly
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	monthlyTTL := int(nextMonth.Sub(now).Seconds())

	configs := []WindowInfo{
		{
			Name: "minute",
			Key:  fmt.Sprintf("loopers:spend:{%s}:minute:%s", keyHash, now.Format("2006-01-02T15:04")),
			TTL:  minuteTTL,
		},
		{
			Name: "hourly",
			Key:  fmt.Sprintf("loopers:spend:{%s}:hourly:%s", keyHash, now.Format("2006-01-02T15")),
			TTL:  hourlyTTL,
		},
		{
			Name: "daily",
			Key:  fmt.Sprintf("loopers:spend:{%s}:daily:%s", keyHash, now.Format("2006-01-02")),
			TTL:  dailyTTL,
		},
		{
			Name: "weekly",
			Key:  fmt.Sprintf("loopers:spend:{%s}:weekly:%d-W%02d", keyHash, year, week),
			TTL:  weeklyTTL,
		},
		{
			Name: "monthly",
			Key:  fmt.Sprintf("loopers:spend:{%s}:monthly:%s", keyHash, now.Format("2006-01")),
			TTL:  monthlyTTL,
		},
	}

	for i := range configs {
		if configs[i].TTL <= 0 {
			configs[i].TTL = 1
		}
	}

	return configs
}

func (c *Client) getBudgetConfig(ctx context.Context, configKey string) (map[string]string, error) {
	if val, ok := configCache.Get(configKey); ok {
		return val, nil
	}

	v, err, _ := configGroup.Do(configKey, func() (interface{}, error) {
		limits, err := c.rdb.HGetAll(ctx, configKey).Result()
		if err != nil {
			return nil, err
		}

		configCache.Set(configKey, limits)
		return limits, nil
	})

	if err != nil {
		return nil, err
	}

	return v.(map[string]string), nil
}

// checkAndReserveRedis checks the configured budgets in Redis and reserves the estimated cost.
// It checks all windows atomically in a single Redis roundtrip.
func (c *Client) checkAndReserveRedis(ctx context.Context, keyHash string, estCost float64) error {
	now := time.Now().UTC()
	configKey := fmt.Sprintf("loopers:budget:{%s}:config", keyHash)
	windows := getWindowConfigs(keyHash, now)

	limits, err := c.getBudgetConfig(ctx, configKey)
	if err != nil {
		return fmt.Errorf("failed to get budget config: %w", err)
	}

	var keys []string
	args := []interface{}{strconv.FormatInt(ToNano(estCost), 10)}

	for _, w := range windows {
		keys = append(keys, w.Key)
	}
	for _, w := range windows {
		args = append(args, w.Name)
	}
	for _, w := range windows {
		limitStr := "0"
		if val, exists := limits[w.Name]; exists && val != "" {
			if limitFloat, err := strconv.ParseFloat(val, 64); err == nil {
				limitStr = strconv.FormatInt(ToNano(limitFloat), 10)
			}
		}
		args = append(args, limitStr)
	}
	for _, w := range windows {
		args = append(args, strconv.Itoa(w.TTL))
	}

	res, err := checkAllScript.Run(ctx, c.rdb, keys, args...).Result()
	if err != nil {
		return fmt.Errorf("redis check script failed: %w", err)
	}

	resSlice, ok := res.([]interface{})
	if !ok || len(resSlice) == 0 {
		return fmt.Errorf("unexpected script response type")
	}

	status := resSlice[0].(int64)
	if status == 0 {
		// Budget exceeded
		currentNano, _ := strconv.ParseInt(resSlice[1].(string), 10, 64)
		limitNano, _ := strconv.ParseInt(resSlice[2].(string), 10, 64)
		currentSpend := FromNano(currentNano)
		limit := FromNano(limitNano)
		windowName := resSlice[3].(string)

		return &BudgetExceededError{
			WindowName:   windowName,
			CurrentSpend: currentSpend,
			Limit:        limit,
			Status:       "budget_exceeded",
		}
	}

	return nil
}

// reconcileRedis refunds unused budget back to Redis.
func (c *Client) reconcileRedis(ctx context.Context, keyHash string, reservedCost, actualCost float64) error {
	now := time.Now().UTC()
	windows := getWindowConfigs(keyHash, now)

	var keys []string
	for _, w := range windows {
		keys = append(keys, w.Key)
	}

	args := []interface{}{
		strconv.FormatInt(ToNano(reservedCost), 10),
		strconv.FormatInt(ToNano(actualCost), 10),
	}
	for _, w := range windows {
		args = append(args, strconv.Itoa(w.TTL))
	}

	_, err := reconcileAllScript.Run(ctx, c.rdb, keys, args...).Result()
	if err != nil {
		return fmt.Errorf("failed to reconcile spend: %w", err)
	}

	return nil
}

// WindowStatus represents the status of a specific budget window.
type WindowStatus struct {
	Limit        float64 `json:"limit"` // 0 if not configured
	CurrentSpend float64 `json:"current_spend"`
	SavedUSD     float64 `json:"saved_usd,omitempty"`
}

// RecordSavings increments counterfactual savings across all active windows in Redis.
func (c *Client) RecordSavings(ctx context.Context, keyHash string, savedUSD float64) error {
	if savedUSD <= 0 {
		return nil
	}
	now := time.Now().UTC()
	windows := getWindowConfigs(keyHash, now)
	pipe := c.rdb.Pipeline()
	savedNano := ToNano(savedUSD)
	for _, w := range windows {
		savedKey := strings.Replace(w.Key, "loopers:spend:", "loopers:saved:", 1)
		pipe.IncrBy(ctx, savedKey, savedNano)
		pipe.Expire(ctx, savedKey, time.Duration(w.TTL)*time.Second)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// GetBudgetStatus returns the limit, current spend, and counterfactual savings for all 5 windows.
func (c *Client) GetBudgetStatus(ctx context.Context, keyHash string) (map[string]WindowStatus, error) {
	now := time.Now().UTC()
	configKey := fmt.Sprintf("loopers:budget:{%s}:config", keyHash)
	windows := getWindowConfigs(keyHash, now)

	limits, err := c.rdb.HGetAll(ctx, configKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to read budget config: %w", err)
	}

	status := make(map[string]WindowStatus)
	for _, w := range windows {
		var limit float64
		if limitStr, exists := limits[w.Name]; exists && limitStr != "" {
			if limitFloat, err := strconv.ParseFloat(limitStr, 64); err == nil {
				limit = limitFloat
			}
		}

		spendStr, _ := c.rdb.Get(ctx, w.Key).Result()
		var spend float64
		if spendStr != "" {
			if spendNano, err := strconv.ParseInt(spendStr, 10, 64); err == nil {
				spend = FromNano(spendNano)
			}
		}

		savedKey := strings.Replace(w.Key, "loopers:spend:", "loopers:saved:", 1)
		savedStr, _ := c.rdb.Get(ctx, savedKey).Result()
		var saved float64
		if savedStr != "" {
			if savedNano, err := strconv.ParseInt(savedStr, 10, 64); err == nil {
				saved = FromNano(savedNano)
			}
		}

		status[w.Name] = WindowStatus{
			Limit:        limit,
			CurrentSpend: spend,
			SavedUSD:     saved,
		}
	}

	return status, nil
}

// CheckAndReserveSession checks session budgets and max steps and reserves estimated spend and step increments.
// Returns (allowed, val1, val2, status, error)
func (c *Client) CheckAndReserveSession(ctx context.Context, keyHash, sessionID string, estCost float64, inputBudget float64, inputMaxSteps int, ttlSeconds int) (bool, float64, float64, string, error) {
	spendKey := fmt.Sprintf("loopers:session:{%s}:%s:spend", keyHash, sessionID)
	budgetKey := fmt.Sprintf("loopers:session:{%s}:%s:budget", keyHash, sessionID)
	stepsKey := fmt.Sprintf("loopers:session:{%s}:%s:steps", keyHash, sessionID)
	maxStepsKey := fmt.Sprintf("loopers:session:{%s}:%s:max_steps", keyHash, sessionID)

	budgetArg := ""
	if inputBudget > 0 {
		budgetArg = strconv.FormatInt(ToNano(inputBudget), 10)
	}
	maxStepsArg := ""
	if inputMaxSteps > 0 {
		maxStepsArg = strconv.Itoa(inputMaxSteps)
	}

	res, err := sessionCheckScript.Run(ctx, c.rdb,
		[]string{spendKey, budgetKey, stepsKey, maxStepsKey},
		strconv.FormatInt(ToNano(estCost), 10),
		budgetArg,
		maxStepsArg,
		ttlSeconds,
	).Result()

	if err != nil {
		return false, 0, 0, "", fmt.Errorf("redis error during session check: %w", err)
	}

	slice, ok := res.([]interface{})
	if !ok || len(slice) < 4 {
		return false, 0, 0, "", fmt.Errorf("unexpected response from session check script: %v", res)
	}

	allowed, _ := slice[0].(int64)
	val1Str, _ := slice[1].(string)
	val2Str, _ := slice[2].(string)
	status, _ := slice[3].(string)

	val1Raw, _ := strconv.ParseInt(val1Str, 10, 64)
	val2Raw, _ := strconv.ParseInt(val2Str, 10, 64)

	var val1, val2 float64
	if status == "session_steps_exceeded" {
		val1 = float64(val1Raw)
		val2 = float64(val2Raw)
	} else if status == "session_budget_exceeded" {
		val1 = FromNano(val1Raw)
		val2 = FromNano(val2Raw)
	} else { // status == "ok"
		val1 = FromNano(val1Raw)
		val2 = float64(val2Raw)
	}

	return allowed == 1, val1, val2, status, nil
}
