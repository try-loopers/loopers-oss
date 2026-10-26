package budget

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBudgetRaceCondition(t *testing.T) {
	redisAddr := os.Getenv("REDIS_TEST_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	client, err := NewClient(redisAddr, "", 0)
	if err != nil {
		t.Skipf("Skipping TestBudgetRaceCondition: Redis not running on %s. Run local Redis or set REDIS_TEST_ADDR to run this test.", redisAddr)
		return
	}
	defer client.Close()

	ctx := context.Background()
	rdb := client.GetUnderlyingClient()

	keyHash := "test-race-key-" + uuid.New().String()

	// Setup registry key
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "test-race",
		"provider":   "openai",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	// Setup budget config: $5.00 limit for daily window
	configKey := "loopers:budget:{" + keyHash + "}:config"
	rdb.HSet(ctx, configKey, "daily", "5.00")
	defer rdb.Del(ctx, configKey)

	// Clean any pre-existing spend counter
	dailySpendKey := "loopers:spend:" + keyHash + ":daily:" + time.Now().UTC().Format("2006-01-02")
	rdb.Del(ctx, dailySpendKey)
	defer rdb.Del(ctx, dailySpendKey)

	var allowed int64
	var blocked int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Reserve $0.10. With $5.00 limit, exactly 50 requests should pass and 50 should fail.
			err := client.LeaseManager.Acquire(ctx, keyHash, 0.10, 1.0)
			if err == nil {
				atomic.AddInt64(&allowed, 1)
			} else {
				atomic.AddInt64(&blocked, 1)
			}
		}()
	}
	wg.Wait()

	if allowed != 50 {
		t.Errorf("Expected exactly 50 allowed requests, got %d", allowed)
	}
	if blocked != 50 {
		t.Errorf("Expected exactly 50 blocked requests, got %d", blocked)
	}
}

func TestBudgetWindowExpansion(t *testing.T) {
	redisAddr := os.Getenv("REDIS_TEST_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	client, err := NewClient(redisAddr, "", 0)
	if err != nil {
		t.Skipf("Skipping TestBudgetWindowExpansion: Redis not running on %s", redisAddr)
		return
	}
	defer client.Close()

	ctx := context.Background()
	rdb := client.GetUnderlyingClient()
	keyHash := "test-window-key-" + uuid.New().String()

	// Setup registry key
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "test-window",
		"provider":   "openai",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	// Set minute limit of 0.20 and weekly limit of 0.50
	configKey := "loopers:budget:{" + keyHash + "}:config"
	rdb.HSet(ctx, configKey, map[string]interface{}{
		"minute": "0.20",
		"weekly": "0.50",
	})
	defer rdb.Del(ctx, configKey)

	// Clean up keys
	now := time.Now().UTC()
	minuteKey := "loopers:spend:" + keyHash + ":minute:" + now.Format("2006-01-02T15:04")
	year, week := now.ISOWeek()
	weeklyKey := fmt.Sprintf("loopers:spend:%s:weekly:%d-W%02d", keyHash, year, week)
	rdb.Del(ctx, minuteKey, weeklyKey)
	defer rdb.Del(ctx, minuteKey, weeklyKey)

	// First request: reserve 0.15 -> should succeed
	err = client.LeaseManager.Acquire(ctx, keyHash, 0.15, 1.0)
	if err != nil {
		t.Errorf("expected first request to succeed, got: %v", err)
	}

	// Second request: reserve 0.10 -> should fail minute limit (0.15 + 0.10 = 0.25 > 0.20)
	err = client.LeaseManager.Acquire(ctx, keyHash, 0.10, 1.0)
	if err == nil {
		t.Errorf("expected second request to fail minute limit")
	} else if err != ErrBudgetExceeded {
		t.Errorf("expected ErrBudgetExceeded, got: %v", err)
	}

	// Reconcile first request actual cost: 0.15 -> 0.05
	client.LeaseManager.ReconcileSpend(context.Background(), keyHash, 0.15, 0.05)

	// Wait for async batched reconcile to flush (100ms batch interval)
	time.Sleep(150 * time.Millisecond)

	// 3. Third request ($0.06): Should now succeed because $0.05 was refunded
	err = client.LeaseManager.Acquire(ctx, keyHash, 0.10, 1.0)
	if err != nil {
		t.Fatalf("expected third request to succeed after reconciliation, got: %v", err)
	}
}

func TestSessionEnforcement(t *testing.T) {
	redisAddr := os.Getenv("REDIS_TEST_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	client, err := NewClient(redisAddr, "", 0)
	if err != nil {
		t.Skipf("Skipping TestSessionEnforcement: Redis not running on %s", redisAddr)
		return
	}
	defer client.Close()

	ctx := context.Background()
	rdb := client.GetUnderlyingClient()
	sessionID := "test-sess-" + uuid.New().String()

	spendKey := fmt.Sprintf("loopers:session:{%s}:%s:spend", "hash1", sessionID)
	budgetKey := fmt.Sprintf("loopers:session:{%s}:%s:budget", "hash1", sessionID)
	stepsKey := fmt.Sprintf("loopers:session:{%s}:%s:steps", "hash1", sessionID)
	maxStepsKey := fmt.Sprintf("loopers:session:{%s}:%s:max_steps", "hash1", sessionID)

	defer rdb.Del(ctx, spendKey, budgetKey, stepsKey, maxStepsKey)

	// First session reservation: cost=0.50, budget=1.00, max_steps=2
	allowed, spend, steps, status, err := client.CheckAndReserveSession(ctx, "hash1", sessionID, 0.50, 1.00, 2, 3600)
	if err != nil {
		t.Fatalf("CheckAndReserveSession failed: %v", err)
	}
	if !allowed || spend != 0.50 || steps != 1 || status != "ok" {
		t.Errorf("expected allowed=true, spend=0.50, steps=1, status=ok; got allowed=%v, spend=%f, steps=%f, status=%s", allowed, spend, steps, status)
	}

	// Second reservation: cost=0.60 -> should fail session budget limit (0.50 + 0.60 = 1.10 > 1.00)
	allowed, val1, val2, status, err := client.CheckAndReserveSession(ctx, "hash1", sessionID, 0.60, 0, 0, 3600)
	if err != nil {
		t.Fatalf("CheckAndReserveSession failed: %v", err)
	}
	if allowed || status != "session_budget_exceeded" || val1 != 0.50 || val2 != 1.00 {
		t.Errorf("expected blocked with session_budget_exceeded, current=0.50, limit=1.00; got allowed=%v, status=%s, val1=%f, val2=%f", allowed, status, val1, val2)
	}

	// Third reservation: cost=0.10 -> should pass, step count becomes 2
	allowed, spend, steps, status, err = client.CheckAndReserveSession(ctx, "hash1", sessionID, 0.10, 0, 0, 3600)
	if err != nil {
		t.Fatalf("CheckAndReserveSession failed: %v", err)
	}
	if !allowed || spend != 0.60 || steps != 2 {
		t.Errorf("expected allowed, spend=0.60, steps=2; got allowed=%v, spend=%f, steps=%f", allowed, spend, steps)
	}

	// Fourth reservation: cost=0.10 -> should fail session max steps limit (current steps=2, max_steps=2)
	allowed, val1, val2, status, err = client.CheckAndReserveSession(ctx, "hash1", sessionID, 0.10, 0, 0, 3600)
	if err != nil {
		t.Fatalf("CheckAndReserveSession failed: %v", err)
	}
	if allowed || status != "session_steps_exceeded" || val1 != 2 || val2 != 2 {
		t.Errorf("expected blocked with session_steps_exceeded; got allowed=%v, status=%s, val1=%f, val2=%f", allowed, status, val1, val2)
	}
}

func TestRecordSavingsAndGetBudgetStatus(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer client.Close()
	defer mr.Close()

	ctx := context.Background()
	keyHash := "test-savings-key"

	// Setup budget config: $10.00 daily limit
	configKey := "loopers:budget:{" + keyHash + "}:config"
	client.GetUnderlyingClient().HSet(ctx, configKey, "daily", "10.00")

	// Initially spend and saved should be 0
	status, err := client.GetBudgetStatus(ctx, keyHash)
	if err != nil {
		t.Fatalf("GetBudgetStatus failed: %v", err)
	}
	if status["daily"].Limit != 10.00 {
		t.Errorf("expected daily limit 10.00, got %f", status["daily"].Limit)
	}
	if status["daily"].SavedUSD != 0.0 {
		t.Errorf("expected initial saved 0.0, got %f", status["daily"].SavedUSD)
	}

	// Record $2.50 of savings
	err = client.RecordSavings(ctx, keyHash, 2.50)
	if err != nil {
		t.Fatalf("RecordSavings failed: %v", err)
	}

	// Verify savings reflected in GetBudgetStatus
	status, err = client.GetBudgetStatus(ctx, keyHash)
	if err != nil {
		t.Fatalf("GetBudgetStatus failed: %v", err)
	}
	if status["daily"].SavedUSD != 2.50 {
		t.Errorf("expected daily saved 2.50, got %f", status["daily"].SavedUSD)
	}
}

func TestBudgetCheckAction_JSONDecoding(t *testing.T) {
	// Represents the exact struct defined in .github/actions/budget-check/check.go
	type actionWindowStatus struct {
		Limit        float64 `json:"limit"`
		CurrentSpend float64 `json:"current_spend"`
		SavedUSD     float64 `json:"saved_usd,omitempty"`
	}

	engineStatus := map[string]WindowStatus{
		"daily": {
			Limit:        100.0,
			CurrentSpend: 42.50,
			SavedUSD:     15.75,
		},
	}

	rawJSON, err := json.Marshal(engineStatus)
	if err != nil {
		t.Fatalf("failed to marshal engine status: %v", err)
	}

	var actionStatus map[string]actionWindowStatus
	if err := json.Unmarshal(rawJSON, &actionStatus); err != nil {
		t.Fatalf("failed to unmarshal into action struct: %v", err)
	}

	daily, ok := actionStatus["daily"]
	if !ok {
		t.Fatalf("expected daily window status in unmarshaled result")
	}

	if daily.Limit != 100.0 {
		t.Errorf("expected limit 100.0, got %f", daily.Limit)
	}
	if daily.CurrentSpend != 42.50 {
		t.Errorf("expected current_spend 42.50, got %f", daily.CurrentSpend)
	}
	if daily.SavedUSD != 15.75 {
		t.Errorf("expected saved_usd 15.75, got %f", daily.SavedUSD)
	}
}
