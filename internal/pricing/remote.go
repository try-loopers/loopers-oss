package pricing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

// DefaultRemotePricingURL is the official Loopers static pricing catalog endpoint.
// Requests include a non-PII User-Agent header (cmd, os, arch, harness) unless --offline is set.
const DefaultRemotePricingURL = "https://tryloopers.com/pricing.json"

// FetchRemotePricing fetches the remote pricing JSON using the default 10s timeout.
func FetchRemotePricing(urlStr string) (*Config, error) {
	ua := fmt.Sprintf("loopers-oss/2.23.1 (cmd=serve; os=%s; arch=%s)", runtime.GOOS, runtime.GOARCH)
	return FetchRemotePricingWithUA(urlStr, ua, 10*time.Second)
}

// FetchRemotePricingWithUA fetches the remote pricing JSON with a custom User-Agent and timeout.
func FetchRemotePricingWithUA(urlStr, userAgent string, timeout time.Duration) (*Config, error) {
	if os.Getenv("LOOPERS_OFFLINE") == "true" || os.Getenv("DO_NOT_TRACK") == "1" {
		return nil, fmt.Errorf("offline mode enabled via environment")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		host := strings.Split(u.Host, ":")[0]
		if host != "localhost" && host != "127.0.0.1" {
			return nil, fmt.Errorf("remote pricing URL must be HTTPS or localhost")
		}
	}

	req, err := http.NewRequest(http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote pricing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote pricing returned status %d", resp.StatusCode)
	}

	const maxBodyBytes = 10 * 1024 * 1024 // 10 MB guard against unbounded reads
	var config Config
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&config); err != nil {
		return nil, fmt.Errorf("failed to decode remote pricing: %w", err)
	}

	return &config, nil
}

// DefaultStore returns a built-in fallback pricing store for standalone CLI commands (like `loopers audit`)
// when ./pricing.yaml is not present in the current working directory, synchronized with pricing.yaml.
func DefaultStore() *Store {
	cfg := Config{
		Providers: map[string]ProviderConfig{
			"anthropic": {
				DefaultMaxOutputTokens: 8192,
				Models: map[string]ModelPrice{
					"claude-opus-4-6":   {InputPer1M: 5.00, OutputPer1M: 25.00, CacheReadPer1M: 0.50, CacheWritePer1M: 6.25, Tier: "frontier", FastTier: "claude-sonnet-4-6"},
					"claude-sonnet-4-6": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75, Tier: "frontier", FastTier: "claude-haiku-4-5"},
					"claude-haiku-4-5":  {InputPer1M: 1.00, OutputPer1M: 5.00, CacheReadPer1M: 0.10, CacheWritePer1M: 1.25, Tier: "fast"},
					"claude-3-5-sonnet": {InputPer1M: 3.00, OutputPer1M: 15.00, CacheReadPer1M: 0.30, CacheWritePer1M: 3.75, Tier: "frontier", FastTier: "claude-3-5-haiku"},
					"claude-3-5-haiku":  {InputPer1M: 0.80, OutputPer1M: 4.00, CacheReadPer1M: 0.08, CacheWritePer1M: 1.00, Tier: "fast"},
					"_fallback":         {InputPer1M: 3.00, OutputPer1M: 15.00},
				},
			},
			"openai": {
				DefaultMaxOutputTokens: 16384,
				Models: map[string]ModelPrice{
					"gpt-5.4":     {InputPer1M: 30.00, OutputPer1M: 180.00, Tier: "frontier", FastTier: "gpt-4o-mini"},
					"gpt-4o":      {InputPer1M: 2.50, OutputPer1M: 10.00, CacheReadPer1M: 1.25, CacheWritePer1M: 2.50, Tier: "frontier", FastTier: "gpt-4o-mini"},
					"gpt-4o-mini": {InputPer1M: 0.15, OutputPer1M: 0.60, CacheReadPer1M: 0.075, CacheWritePer1M: 0.15, Tier: "fast"},
					"_fallback":   {InputPer1M: 2.50, OutputPer1M: 10.00},
				},
			},
			"openrouter": {
				DefaultMaxOutputTokens: 4096,
				Models: map[string]ModelPrice{
					"deepseek/deepseek-v4-pro":          {InputPer1M: 0.22, OutputPer1M: 0.44, CacheReadPer1M: 0.018, CacheWritePer1M: 0.22, Tier: "frontier", FastTier: "deepseek/deepseek-v4.1-flash-1017"},
					"deepseek/deepseek-v4.1-flash-1017": {InputPer1M: 0.01569, OutputPer1M: 0.6439, CacheReadPer1M: 0.007845, CacheWritePer1M: 0.01569, Tier: "fast"},
					"_fallback":                         {InputPer1M: 1.00, OutputPer1M: 3.00},
				},
			},
			"deepseek": {
				DefaultMaxOutputTokens: 8192,
				Models: map[string]ModelPrice{
					"deepseek-v4-pro": {InputPer1M: 1.32, OutputPer1M: 3.96, CacheReadPer1M: 0.022, CacheWritePer1M: 0.22, Tier: "frontier", FastTier: "deepseek-chat"},
					"deepseek-chat":   {InputPer1M: 0.14, OutputPer1M: 0.28, CacheReadPer1M: 0.014, CacheWritePer1M: 0.14, Tier: "fast"},
					"_fallback":       {InputPer1M: 1.32, OutputPer1M: 3.96},
				},
			},
		},
	}
	return &Store{config: cfg}
}
