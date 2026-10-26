package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/pricing"
)

type mockProvider struct {
	baseURL string
}

func (m *mockProvider) Name() string                                     { return "mock" }
func (m *mockProvider) BaseURL() string                                  { return m.baseURL }
func (m *mockProvider) InjectAuth(req *http.Request, providerKey string) {}
func (m *mockProvider) RewritePath(originalPath string) string           { return originalPath }
func (m *mockProvider) ParseRequest(req *http.Request, body []byte) (string, bool, int, []byte, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		maxTok := 0
		if mt, exists := payload["max_tokens"].(float64); exists {
			maxTok = int(mt)
		}
		if model, ok := payload["model"].(string); ok {
			return model, false, maxTok, body, nil
		}
	}
	return "mock-model", false, 0, body, nil
}
func (m *mockProvider) RewriteModel(req *http.Request, body []byte, fallbackModel string) ([]byte, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err == nil {
		payload["model"] = fallbackModel
		return json.Marshal(payload)
	}
	return body, nil
}
func (m *mockProvider) CountInputTokens(ctx context.Context, model string, body []byte, providerKey string) (int, error) {
	return 50, nil
}
func (m *mockProvider) ParseStreamChunk(chunk []byte) (int, int, bool, error) {
	return 0, 0, false, nil
}
func (m *mockProvider) ParseNonStreamResponse(body []byte) (int, int, error) {
	return 50, 100, nil
}
func (m *mockProvider) FormatBudgetExceededSSE() []byte {
	return nil
}

func TestShadowMode(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient, err := budget.NewClient(mr.Addr(), "", 0)
	if err != nil {
		t.Fatalf("Failed to create redis client: %v", err)
	}
	defer redisClient.Close()

	// Create a temp pricing yaml with non-zero prices so estimatedCost > 0 and budget check trips
	yamlContent := []byte(`
providers:
  mock:
    default_max_output_tokens: 100
    models:
      mock-model:
        input_per_1m: 10.0
        output_per_1m: 30.0
`)
	tmpFile, _ := os.CreateTemp("", "pricing*.yaml")
	defer os.Remove(tmpFile.Name())
	tmpFile.Write(yamlContent)
	tmpFile.Close()

	pricingStore, err := pricing.LoadStore(tmpFile.Name())
	if err != nil {
		t.Fatalf("Failed to load pricing store: %v", err)
	}

	// Start a dummy upstream server
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	s := NewServer(redisClient, pricingStore)
	defer s.Shutdown()

	// Register mock provider through the helper so BodyBuffer middleware is correctly applied
	s.RegisterProviderRoute(&mockProvider{baseURL: upstream.URL})

	ctx := context.Background()
	rdb := redisClient.GetUnderlyingClient()

	rawKey, _ := keyring.GenerateRawKey()
	keyHash := keyring.HashKey(rawKey)
	rdb.HSet(ctx, "loopers:key:"+keyHash, map[string]interface{}{
		"name":       "shadow-test-key",
		"provider":   "mock",
		"created_at": time.Now().UTC().Format(time.RFC3339),
		"active":     "true",
	})
	defer rdb.Del(ctx, "loopers:key:"+keyHash)

	// Set a tiny budget that will definitely fail
	configKey := "loopers:budget:{" + keyHash + "}:config"
	rdb.HSet(ctx, configKey, "minute", "0.000001")
	defer rdb.Del(ctx, configKey)
	defer rdb.Del(ctx, "loopers:spend:"+keyHash+":minute:"+time.Now().UTC().Format("2006-01-02T15:04"))

	// Enable Shadow Mode
	s.shadowMode = true

	// Initial metric count
	initialShadowBlocks := testutil.ToFloat64(shadowBlockedTotal.WithLabelValues("mock", "budget"))

	// Make request
	reqBody := []byte(`{"messages": []}`)
	req, _ := http.NewRequest("POST", "/mock/v1/chat", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Loopers-Provider-Key", "dummy")

	w := newCloseNotifierRecorder()
	s.GetRouter().ServeHTTP(w, req)

	// In shadow mode, it should proceed to upstream and return 200 OK
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK due to shadow mode, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify metric incremented
	finalShadowBlocks := testutil.ToFloat64(shadowBlockedTotal.WithLabelValues("mock", "budget"))
	if finalShadowBlocks != initialShadowBlocks+1 {
		t.Errorf("Expected shadowBlockedTotal metric to increment, got %v (was %v)", finalShadowBlocks, initialShadowBlocks)
	}

	// Disable Shadow Mode and verify it gets blocked
	s.shadowMode = false
	req, _ = http.NewRequest("POST", "/mock/v1/chat", bytes.NewBuffer(reqBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("X-Loopers-Provider-Key", "dummy")

	w = newCloseNotifierRecorder()
	s.GetRouter().ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429 Too Many Requests when shadow mode is false, got %d", w.Code)
	}
}
