package budget

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
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

// FinOpsRecord contains fine-grained breakdown of all optimization metrics.
type FinOpsRecord struct {
	// Ghost Token Metrics
	GhostTokensSaved  int64   `json:"ghost_tokens_saved"`
	GhostPayloadBytes int64   `json:"ghost_payload_bytes"`
	GhostStubsCreated int64   `json:"ghost_stubs_created"`

	// Schema Minification Metrics
	SchemaTokensSaved int64   `json:"schema_tokens_saved"`

	// Fast-Tier Model Routing Metrics
	FastTierTurns     int64   `json:"fast_tier_turns"`
	FastTierSavedUSD  float64 `json:"fast_tier_saved_usd"`

	// MCP Tool Memoization Metrics
	ToolMemoHits      int64   `json:"tool_memo_hits"`
	ToolMemoSavedUSD  float64 `json:"tool_memo_saved_usd"`

	// Overall Cost Ledger
	ActualSpendUSD    float64 `json:"actual_spend_usd"`
	CounterfactualUSD float64 `json:"counterfactual_usd"` // Baseline cost if unoptimized
	NetSavedUSD       float64 `json:"net_saved_usd"`       // Total dollars saved
	SavingsPercentage float64 `json:"savings_percentage"`  // (NetSavedUSD / CounterfactualUSD) * 100
}

// FinOpsTelemetryDelta represents an incremental delta emitted during request processing.
type FinOpsTelemetryDelta struct {
	GhostTokensSaved  int64
	GhostPayloadBytes int64
	GhostStubsCreated int64
	SchemaTokensSaved int64
	FastTierTurns     int64
	FastTierSavedUSD  float64
	ToolMemoHits      int64
	ToolMemoSavedUSD  float64
	ActualSpendUSD    float64
	CounterfactualUSD float64
	NetSavedUSD       float64
}

func parseFinOpsRecord(data map[string]string) FinOpsRecord {
	var rec FinOpsRecord
	if data == nil {
		return rec
	}
	rec.GhostTokensSaved, _ = strconv.ParseInt(data["ghost_tokens_saved"], 10, 64)
	rec.GhostPayloadBytes, _ = strconv.ParseInt(data["ghost_payload_bytes"], 10, 64)
	rec.GhostStubsCreated, _ = strconv.ParseInt(data["ghost_stubs_created"], 10, 64)
	rec.SchemaTokensSaved, _ = strconv.ParseInt(data["schema_tokens_saved"], 10, 64)
	rec.FastTierTurns, _ = strconv.ParseInt(data["fast_tier_turns"], 10, 64)
	rec.ToolMemoHits, _ = strconv.ParseInt(data["tool_memo_hits"], 10, 64)

	if v, err := strconv.ParseInt(data["fast_tier_saved_nano"], 10, 64); err == nil {
		rec.FastTierSavedUSD = FromNano(v)
	}
	if v, err := strconv.ParseInt(data["tool_memo_saved_nano"], 10, 64); err == nil {
		rec.ToolMemoSavedUSD = FromNano(v)
	}
	if v, err := strconv.ParseInt(data["actual_spend_nano"], 10, 64); err == nil {
		rec.ActualSpendUSD = FromNano(v)
	}
	if v, err := strconv.ParseInt(data["counterfactual_nano"], 10, 64); err == nil {
		rec.CounterfactualUSD = FromNano(v)
	}
	if v, err := strconv.ParseInt(data["net_saved_nano"], 10, 64); err == nil {
		rec.NetSavedUSD = FromNano(v)
	}

	if rec.CounterfactualUSD > 0 {
		rec.SavingsPercentage = (rec.NetSavedUSD / rec.CounterfactualUSD) * 100.0
		if rec.SavingsPercentage > 100.0 {
			rec.SavingsPercentage = 100.0
		}
	} else if rec.NetSavedUSD > 0 && rec.ActualSpendUSD == 0 {
		rec.SavingsPercentage = 100.0
	}

	return rec
}

// RecordFinOpsTelemetry atomically records fine-grained FinOps savings across sliding windows,
// all-time ledger, and an optional session key in Redis.
func (c *Client) RecordFinOpsTelemetry(ctx context.Context, keyHash, sessionID string, delta FinOpsTelemetryDelta) error {
	now := time.Now().UTC()
	windows := getWindowConfigs(keyHash, now)
	pipe := c.rdb.Pipeline()

	applyDeltaToHash := func(key string, ttl time.Duration) {
		if delta.GhostTokensSaved > 0 {
			pipe.HIncrBy(ctx, key, "ghost_tokens_saved", delta.GhostTokensSaved)
		}
		if delta.GhostPayloadBytes > 0 {
			pipe.HIncrBy(ctx, key, "ghost_payload_bytes", delta.GhostPayloadBytes)
		}
		if delta.GhostStubsCreated > 0 {
			pipe.HIncrBy(ctx, key, "ghost_stubs_created", delta.GhostStubsCreated)
		}
		if delta.SchemaTokensSaved > 0 {
			pipe.HIncrBy(ctx, key, "schema_tokens_saved", delta.SchemaTokensSaved)
		}
		if delta.FastTierTurns > 0 {
			pipe.HIncrBy(ctx, key, "fast_tier_turns", delta.FastTierTurns)
		}
		if delta.FastTierSavedUSD > 0 {
			pipe.HIncrBy(ctx, key, "fast_tier_saved_nano", ToNano(delta.FastTierSavedUSD))
		}
		if delta.ToolMemoHits > 0 {
			pipe.HIncrBy(ctx, key, "tool_memo_hits", delta.ToolMemoHits)
		}
		if delta.ToolMemoSavedUSD > 0 {
			pipe.HIncrBy(ctx, key, "tool_memo_saved_nano", ToNano(delta.ToolMemoSavedUSD))
		}
		if delta.ActualSpendUSD > 0 {
			pipe.HIncrBy(ctx, key, "actual_spend_nano", ToNano(delta.ActualSpendUSD))
		}
		if delta.CounterfactualUSD > 0 {
			pipe.HIncrBy(ctx, key, "counterfactual_nano", ToNano(delta.CounterfactualUSD))
		}
		if delta.NetSavedUSD > 0 {
			pipe.HIncrBy(ctx, key, "net_saved_nano", ToNano(delta.NetSavedUSD))
		}
		if ttl > 0 {
			pipe.Expire(ctx, key, ttl)
		}
	}

	// 1. Sliding Windows
	savedNano := ToNano(delta.NetSavedUSD)
	for _, w := range windows {
		finopsKey := strings.Replace(w.Key, "loopers:spend:", "loopers:finops:", 1)
		applyDeltaToHash(finopsKey, time.Duration(w.TTL)*time.Second)

		// Backward-compatibility: also update legacy loopers:saved key
		if savedNano > 0 {
			savedKey := strings.Replace(w.Key, "loopers:spend:", "loopers:saved:", 1)
			pipe.IncrBy(ctx, savedKey, savedNano)
			pipe.Expire(ctx, savedKey, time.Duration(w.TTL)*time.Second)
		}
	}

	// 2. All-Time Key
	applyDeltaToHash(fmt.Sprintf("loopers:finops:{%s}:total", keyHash), 0)

	// 3. Session Key (24h retention)
	if sessionID != "" {
		sessionKey := fmt.Sprintf("loopers:session:{%s}:%s:finops", keyHash, sessionID)
		applyDeltaToHash(sessionKey, 24*time.Hour)
	}

	_, err := pipe.Exec(ctx)
	return err
}

// GetFinOpsStatus returns the fine-grained FinOps breakdown across all 5 sliding windows and all-time total.
func (c *Client) GetFinOpsStatus(ctx context.Context, keyHash string) (map[string]FinOpsRecord, error) {
	now := time.Now().UTC()
	windows := getWindowConfigs(keyHash, now)
	pipe := c.rdb.Pipeline()

	windowCmds := make(map[string]*redis.MapStringStringCmd)
	for _, w := range windows {
		finopsKey := strings.Replace(w.Key, "loopers:spend:", "loopers:finops:", 1)
		windowCmds[w.Name] = pipe.HGetAll(ctx, finopsKey)
	}
	totalCmd := pipe.HGetAll(ctx, fmt.Sprintf("loopers:finops:{%s}:total", keyHash))

	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to fetch finops status: %w", err)
	}

	status := make(map[string]FinOpsRecord)
	for name, cmd := range windowCmds {
		res, _ := cmd.Result()
		status[name] = parseFinOpsRecord(res)
	}

	totRes, _ := totalCmd.Result()
	status["total"] = parseFinOpsRecord(totRes)

	return status, nil
}

// GetSessionFinOps returns the FinOps scorecard for a specific active or historical session.
func (c *Client) GetSessionFinOps(ctx context.Context, keyHash, sessionID string) (*FinOpsRecord, error) {
	if sessionID == "" {
		return nil, nil
	}
	sessionKey := fmt.Sprintf("loopers:session:{%s}:%s:finops", keyHash, sessionID)
	data, err := c.rdb.HGetAll(ctx, sessionKey).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to fetch session finops: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	rec := parseFinOpsRecord(data)
	return &rec, nil
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
