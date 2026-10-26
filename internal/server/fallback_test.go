package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/policy"
	"github.com/try-loopers/loopers-oss/internal/pricing"
)

func TestFallbackRouting(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	viper.Set("testing.allow_private_urls", true)
	defer viper.Set("testing.allow_private_urls", false)

	redisClient, err := budget.NewClient(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer redisClient.Close()

	// Create a temp pricing yaml
	yamlContent := []byte(`
providers:
  mock:
    default_max_output_tokens: 100
    models:
      expensive-model:
        input_per_1m: 10.0
        output_per_1m: 30.0
        fallback: cheap-model
      cheap-model:
        input_per_1m: 0.1
        output_per_1m: 0.3
`)
	tmpFile, _ := os.CreateTemp("", "pricing*.yaml")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(yamlContent)
	tmpFile.Close()

	pricingStore, err := pricing.LoadStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load pricing store: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	s := NewServer(redisClient, pricingStore)
	defer s.Shutdown()
	s.RegisterProviderRoute(&mockProvider{baseURL: upstream.URL})

	ctx := context.Background()
	rdb := redisClient.GetUnderlyingClient()

	rawKey, _ := keyring.GenerateRawKey()
	keyHash := keyring.HashKey(rawKey)
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "fallback-test-key",
		"provider":   "mock",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	// Set a budget that blocks expensive-model but allows cheap-model
	// expensive-model est cost = 50 * 10/1M + 100 * 30/1M = 0.0005 + 0.003 = 0.0035
	// cheap-model est cost = 50 * 0.1/1M + 100 * 0.3/1M = 0.000005 + 0.00003 = 0.000035
	configKey := "loopers:budget:{" + keyHash + "}:config"
	rdb.HSet(ctx, configKey, "minute", "0.001") // Limits expensive, allows cheap
	defer rdb.Del(ctx, configKey)
	defer rdb.Del(ctx, "loopers:spend:"+keyHash+":minute:"+time.Now().UTC().Format("2006-01-02T15:04"))

	// Make request for expensive-model
	reqBody := []byte(`{"model": "expensive-model"}`)
	req, _ := http.NewRequest("POST", "/mock/v1/chat", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Loopers-Provider-Key", "dummy")

	w := newCloseNotifierRecorder()
	s.GetRouter().ServeHTTP(w, req)

	// The request should have been routed to the fallback, successfully reserved, and returned 200 OK
	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 OK due to fallback routing, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify the fallback header is set
	if w.Header().Get("X-Loopers-Fallback") != "cheap-model" {
		t.Errorf("Expected X-Loopers-Fallback header to be cheap-model, got %s. All headers: %v", w.Header().Get("X-Loopers-Fallback"), w.Header())
	}
}

func TestSmartFinOpsRouting_FastTier(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	viper.Set("testing.allow_private_urls", true)
	defer viper.Set("testing.allow_private_urls", false)

	redisClient, err := budget.NewClient(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer redisClient.Close()

	yamlContent := []byte(`
providers:
  mock:
    default_max_output_tokens: 100
    models:
      expensive-model:
        input_per_1m: 10.0
        output_per_1m: 30.0
        fast_tier: fast-model
      fast-model:
        input_per_1m: 0.1
        output_per_1m: 0.3
`)
	tmpFile, _ := os.CreateTemp("", "pricing*.yaml")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(yamlContent)
	tmpFile.Close()

	pricingStore, err := pricing.LoadStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load pricing store: %v", err)
	}

	var upstreamReceivedModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]interface{}
		_ = json.Unmarshal(body, &m)
		if modelVal, ok := m["model"].(string); ok {
			upstreamReceivedModel = modelVal
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	s := NewServer(redisClient, pricingStore)
	defer s.Shutdown()
	s.RegisterProviderRoute(&mockProvider{baseURL: upstream.URL})

	ctx := context.Background()
	rdb := redisClient.GetUnderlyingClient()

	rawKey, _ := keyring.GenerateRawKey()
	keyHash := keyring.HashKey(rawKey)
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "fast-tier-test-key",
		"provider":   "mock",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	// Turn 1 simple turn (no reasoning keywords, <600 bytes)
	reqBody := []byte(`{"model": "expensive-model", "messages": [{"role": "user", "content": "What is the weather?"}]}`)
	req, _ := http.NewRequest("POST", "/mock/v1/chat", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Loopers-Provider-Key", "dummy")

	w := newCloseNotifierRecorder()
	s.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify X-Loopers-FinOps-Route header is fast-tier
	if w.Header().Get("X-Loopers-FinOps-Route") != "fast-tier" {
		t.Errorf("Expected X-Loopers-FinOps-Route header to be fast-tier, got '%s'", w.Header().Get("X-Loopers-FinOps-Route"))
	}

	// Verify upstream actually received fast-model
	if upstreamReceivedModel != "fast-model" {
		t.Errorf("Expected upstream to receive fast-model, got '%s'", upstreamReceivedModel)
	}
}

func TestSmartFinOpsRouting_WarmCache(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	viper.Set("testing.allow_private_urls", true)
	defer viper.Set("testing.allow_private_urls", false)

	redisClient, err := budget.NewClient(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer redisClient.Close()

	yamlContent := []byte(`
providers:
  mock:
    default_max_output_tokens: 4096
    models:
      expensive-model:
        input_per_1m: 10.0
        output_per_1m: 30.0
        fast_tier: fast-model
      fast-model:
        input_per_1m: 0.1
        output_per_1m: 0.3
`)
	tmpFile, _ := os.CreateTemp("", "pricing*.yaml")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(yamlContent)
	tmpFile.Close()

	pricingStore, err := pricing.LoadStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load pricing store: %v", err)
	}

	var upstreamReceivedModel string
	var upstreamReceivedMaxTokens float64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var m map[string]interface{}
		_ = json.Unmarshal(body, &m)
		if modelVal, ok := m["model"].(string); ok {
			upstreamReceivedModel = modelVal
		}
		if mt, ok := m["max_tokens"].(float64); ok {
			upstreamReceivedMaxTokens = mt
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	s := NewServer(redisClient, pricingStore)
	defer s.Shutdown()
	s.RegisterProviderRoute(&mockProvider{baseURL: upstream.URL})

	ctx := context.Background()
	rdb := redisClient.GetUnderlyingClient()

	rawKey, _ := keyring.GenerateRawKey()
	keyHash := keyring.HashKey(rawKey)
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "warm-cache-test-key",
		"provider":   "mock",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	sessionID := "99999999-9999-9999-9999-999999999999"

	// Pre-populate session trace history with 2 prior turns so it's a warm cache
	_ = s.sessionManager.AppendSessionTrace(ctx, keyHash, sessionID, policy.SessionTrace{
		Timestamp: time.Now().Unix(),
		Type:      "llm_call",
		Provider:  "mock",
		Model:     "expensive-model",
	})
	_ = s.sessionManager.AppendSessionTrace(ctx, keyHash, sessionID, policy.SessionTrace{
		Timestamp: time.Now().Unix(),
		Type:      "llm_call",
		Provider:  "mock",
		Model:     "expensive-model",
	})

	// Warm turn with simple prompt
	reqBody := []byte(`{"model": "expensive-model", "messages": [{"role": "user", "content": "What is the weather?"}]}`)
	req, _ := http.NewRequest("POST", "/mock/v1/chat", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Loopers-Provider-Key", "dummy")
	req.Header.Set("X-Loopers-Session-ID", sessionID)

	w := newCloseNotifierRecorder()
	s.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Model must NOT be downgraded to fast-model (it stays pinned on expensive-model to protect warm KV-cache)
	if upstreamReceivedModel != "expensive-model" {
		t.Errorf("Expected model to stay pinned as expensive-model on warm cache, got '%s'", upstreamReceivedModel)
	}

	// Throttled header must be set because defaultMaxOut was 4096 (> 1024)
	if w.Header().Get("X-Loopers-FinOps-Throttled") != "true" {
		t.Errorf("Expected X-Loopers-FinOps-Throttled header to be true, got '%s'", w.Header().Get("X-Loopers-FinOps-Throttled"))
	}

	// AUD-04 Verification: Upstream request body must actually have max_tokens: 1024 injected
	if upstreamReceivedMaxTokens != 1024 {
		t.Errorf("Expected upstream max_tokens to be capped at 1024 in request body, got %f", upstreamReceivedMaxTokens)
	}
}

func TestStatelessRequests_NoLoopFalsePositive(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	viper.Set("testing.allow_private_urls", true)
	defer viper.Set("testing.allow_private_urls", false)

	redisClient, err := budget.NewClient(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer redisClient.Close()

	tmpFile, _ := os.CreateTemp("", "pricing*.yaml")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write([]byte(`
providers:
  mock:
    default_max_output_tokens: 100
    models:
      std-model:
        input_per_1m: 1.0
        output_per_1m: 2.0
`))
	tmpFile.Close()

	pricingStore, err := pricing.LoadStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load pricing store: %v", err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	s := NewServer(redisClient, pricingStore)
	defer s.Shutdown()
	s.RegisterProviderRoute(&mockProvider{baseURL: upstream.URL})

	ctx := context.Background()
	rdb := redisClient.GetUnderlyingClient()

	rawKey, _ := keyring.GenerateRawKey()
	keyHash := keyring.HashKey(rawKey)
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "stateless-test-key",
		"provider":   "mock",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	// Send 5 identical requests without X-Loopers-Session-ID
	for i := 1; i <= 5; i++ {
		reqBody := []byte(`{"model": "std-model", "messages": [{"role": "user", "content": "ping"}]}`)
		req, _ := http.NewRequest("POST", "/mock/v1/chat", bytes.NewBuffer(reqBody))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("X-Loopers-Provider-Key", "dummy")

		w := newCloseNotifierRecorder()
		s.GetRouter().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("Request %d failed with status %d: %s (should not trip loop detection or session ID format check)", i, w.Code, w.Body.String())
		}
	}
}

