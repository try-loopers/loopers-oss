package pricing

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestMergeRemote(t *testing.T) {
	store := &Store{
		config: Config{
			Providers: map[string]ProviderConfig{
				"openai": {
					DefaultMaxOutputTokens: 100,
					Models: map[string]ModelPrice{
						"gpt-4": {InputPer1M: 10.0, OutputPer1M: 30.0},
					},
				},
			},
		},
	}

	remoteConfig := map[string]ProviderConfig{
		"openai": {
			DefaultMaxOutputTokens: 200,
			Models: map[string]ModelPrice{
				"gpt-4":  {InputPer1M: 5.0, OutputPer1M: 15.0}, // Should not overwrite
				"gpt-4o": {InputPer1M: 2.5, OutputPer1M: 10.0}, // Should add
			},
		},
		"anthropic": {
			DefaultMaxOutputTokens: 50,
			Models: map[string]ModelPrice{
				"claude-3": {InputPer1M: 3.0, OutputPer1M: 15.0}, // Should add whole provider
			},
		},
	}

	store.MergeRemote(remoteConfig)

	// Check if openai gpt-4 remained unchanged
	if p, _, _ := store.GetModelPrice("openai", "gpt-4"); p != 10.0 {
		t.Errorf("expected gpt-4 to remain 10.0, got %v", p)
	}

	// Check if openai gpt-4o was added
	if p, _, _ := store.GetModelPrice("openai", "gpt-4o"); p != 2.5 {
		t.Errorf("expected gpt-4o to be added with 2.5, got %v", p)
	}

	// Check if anthropic was added
	if p, _, _ := store.GetModelPrice("anthropic", "claude-3"); p != 3.0 {
		t.Errorf("expected claude-3 to be added with 3.0, got %v", p)
	}
}

func TestFetchRemotePricing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"providers": {"test-prov": {"models": {"test-model": {"input_per_1m": 1.23}}}}}`))
	}))
	defer server.Close()

	config, err := FetchRemotePricing(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if config.Providers["test-prov"].Models["test-model"].InputPer1M != 1.23 {
		t.Errorf("expected 1.23, got %v", config.Providers["test-prov"].Models["test-model"].InputPer1M)
	}
}

func TestDefaultStore_2026Catalog(t *testing.T) {
	store := DefaultStore()
	if store == nil {
		t.Fatal("expected DefaultStore() to be non-nil")
	}

	// Test Anthropic Sonnet 4.6
	inPrice, outPrice, _ := store.GetModelPrice("anthropic", "claude-sonnet-4-6")
	if inPrice != 3.00 || outPrice != 15.00 {
		t.Errorf("expected Sonnet 4.6 at $3.00/$15.00, got $%.2f/$%.2f", inPrice, outPrice)
	}
	if ft := store.GetFastTier("anthropic", "claude-sonnet-4-6"); ft != "claude-haiku-4-5" {
		t.Errorf("expected Sonnet 4.6 fast tier claude-haiku-4-5, got %s", ft)
	}

	// Test OpenAI GPT-5.4
	inPrice, outPrice, _ = store.GetModelPrice("openai", "gpt-5.4")
	if inPrice != 30.00 || outPrice != 180.00 {
		t.Errorf("expected GPT-5.4 at $30.00/$180.00, got $%.2f/$%.2f", inPrice, outPrice)
	}

	// Test OpenRouter DeepSeek V4 Pro
	details, _ := store.GetModelPriceDetails("openrouter", "deepseek/deepseek-v4-pro")
	if details.InputPer1M != 0.22 || details.CacheReadPer1M != 0.018 {
		t.Errorf("expected DeepSeek V4 Pro rates, got %+v", details)
	}
}

func TestFetchRemotePricingWithUA_UserAgentHeader(t *testing.T) {
	var receivedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"providers": {}}`))
	}))
	defer server.Close()

	customUA := "loopers-oss/2.23.1 (cmd=audit; os=test; arch=test; harness=openclaw)"
	_, err := FetchRemotePricingWithUA(server.URL, customUA, 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedUA != customUA {
		t.Errorf("expected User-Agent %q, got %q", customUA, receivedUA)
	}
}

func TestFetchRemotePricingWithUA_OfflineMode(t *testing.T) {
	os.Setenv("LOOPERS_OFFLINE", "true")
	defer os.Unsetenv("LOOPERS_OFFLINE")

	_, err := FetchRemotePricingWithUA("https://example.com/pricing.json", "test", time.Second)
	if err == nil {
		t.Fatal("expected error in offline mode, got nil")
	}
}
