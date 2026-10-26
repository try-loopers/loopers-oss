package pricing

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/try-loopers/loopers-oss/internal/logging"
	"gopkg.in/yaml.v3"
)

// ModelPrice holds the cost parameters for a specific model.
type ModelPrice struct {
	InputPer1M      float64 `mapstructure:"input_per_1m" json:"input_per_1m" yaml:"input_per_1m"`
	OutputPer1M     float64 `mapstructure:"output_per_1m" json:"output_per_1m" yaml:"output_per_1m"`
	CacheReadPer1M  float64 `mapstructure:"cache_read_per_1m" json:"cache_read_per_1m" yaml:"cache_read_per_1m"`
	CacheWritePer1M float64 `mapstructure:"cache_write_per_1m" json:"cache_write_per_1m" yaml:"cache_write_per_1m"`
	Fallback        string  `mapstructure:"fallback" json:"fallback" yaml:"fallback"`
	FastTier        string  `mapstructure:"fast_tier" json:"fast_tier" yaml:"fast_tier"`
	Tier            string  `mapstructure:"tier" json:"tier" yaml:"tier"`
}

// ProviderConfig holds the pricing config for a specific provider.
type ProviderConfig struct {
	DefaultMaxOutputTokens int                   `mapstructure:"default_max_output_tokens" json:"default_max_output_tokens" yaml:"default_max_output_tokens"`
	Models                 map[string]ModelPrice `mapstructure:"models" json:"models" yaml:"models"`
}

// Config represents the schema of pricing.yaml.
type ToolCostEntry struct {
	CostPerCall float64 `mapstructure:"cost_per_call" json:"cost_per_call" yaml:"cost_per_call"`
}

type ToolCostsConfig struct {
	Defaults struct {
		UnknownTool float64 `mapstructure:"unknown_tool" json:"unknown_tool" yaml:"unknown_tool"`
	} `mapstructure:"defaults" json:"defaults" yaml:"defaults"`
	Tools map[string]ToolCostEntry `mapstructure:"tools" json:"tools" yaml:"tools"`
}

type Config struct {
	Providers map[string]ProviderConfig `mapstructure:"providers" json:"providers" yaml:"providers"`
	ToolCosts ToolCostsConfig           `mapstructure:"tool_costs" json:"tool_costs" yaml:"tool_costs"`
}

// Store is a thread-safe in-memory store for LLM pricing.
type Store struct {
	mu     sync.RWMutex
	config Config
}

// LoadStore loads the pricing config from the specified YAML path.
func LoadStore(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read pricing config: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pricing config: %w", err)
	}

	return &Store{config: config}, nil
}

// MergeRemote merges fetched pricing into the in-memory store without overwriting locally defined models.
func (s *Store) MergeRemote(remoteConfig map[string]ProviderConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for provName, remoteProv := range remoteConfig {
		localProv, hasProv := s.config.Providers[provName]
		if !hasProv {
			// Add whole provider with deep-copied Models map
			newProv := remoteProv
			if remoteProv.Models != nil {
				newProv.Models = make(map[string]ModelPrice, len(remoteProv.Models))
				for k, v := range remoteProv.Models {
					newProv.Models[k] = v
				}
			}
			if s.config.Providers == nil {
				s.config.Providers = make(map[string]ProviderConfig)
			}
			s.config.Providers[provName] = newProv
			continue
		}

		// Merge models into a cloned map to avoid modifying caller's map reference
		newModels := make(map[string]ModelPrice, len(localProv.Models)+len(remoteProv.Models))
		for k, v := range localProv.Models {
			newModels[k] = v
		}
		for modelName, remoteModel := range remoteProv.Models {
			if _, hasModel := newModels[modelName]; !hasModel {
				newModels[modelName] = remoteModel
			}
		}
		localProv.Models = newModels
		// Fix M1: inherit DefaultMaxOutputTokens from remote if not set locally
		if localProv.DefaultMaxOutputTokens == 0 && remoteProv.DefaultMaxOutputTokens > 0 {
			localProv.DefaultMaxOutputTokens = remoteProv.DefaultMaxOutputTokens
		}
		s.config.Providers[provName] = localProv

	}
}

// StartRemoteFetcher runs a background goroutine to fetch pricing periodically.
func (s *Store) StartRemoteFetcher(ctx context.Context, url string, refreshHours int) {
	if url == "" || refreshHours <= 0 {
		return
	}

	ticker := time.NewTicker(time.Duration(refreshHours) * time.Hour)
	go func() {
		defer ticker.Stop()
		// Initial fetch
		if remote, err := FetchRemotePricing(url); err == nil {
			s.MergeRemote(remote.Providers)
			logging.Logger.Info().Msgf("Loaded remote pricing from %s", url)
		} else {
			logging.Logger.Warn().Err(err).Msg("Failed to load remote pricing initially")
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if remote, err := FetchRemotePricing(url); err == nil {
					s.MergeRemote(remote.Providers)
				} else {
					logging.Logger.Warn().Err(err).Msg("Failed to reload remote pricing")
				}
			}
		}
	}()
}

var dateSuffixRegex = regexp.MustCompile(`-(20\d{2}[-_]?\d{2}[-_]?\d{2}|\d{4}|\d{3}|latest|preview)$`)

// findModelPrice looks up pricing for a model in provConf using exact match, case-insensitive match,
// date/version snapshot suffix stripping, and longest-prefix matching.
func (s *Store) findModelPrice(provConf ProviderConfig, model string) (ModelPrice, bool) {
	if provConf.Models == nil || model == "" {
		return ModelPrice{}, false
	}

	// 1. Exact match
	if mp, ok := provConf.Models[model]; ok {
		return mp, true
	}

	lowerModel := strings.ToLower(model)

	// 2. Case-insensitive exact match
	for k, v := range provConf.Models {
		if strings.EqualFold(k, model) {
			return v, true
		}
	}

	// 3. Normalized alias matching: strip date / version / tag suffixes
	// Examples:
	// "gpt-4o-2024-08-06" -> "gpt-4o"
	// "gpt-4o-mini-2024-07-18" -> "gpt-4o-mini"
	// "claude-3-5-sonnet-20241022" -> "claude-3-5-sonnet"
	// "claude-3-5-haiku-20241022" -> "claude-3-5-haiku"
	// "gemini-1.5-pro-002" -> "gemini-1.5-pro"
	// "chatgpt-4o-latest" -> "gpt-4o"
	stripped := lowerModel
	if strings.HasPrefix(stripped, "chatgpt-") {
		stripped = "gpt-" + strings.TrimPrefix(stripped, "chatgpt-")
	}
	stripped = dateSuffixRegex.ReplaceAllString(stripped, "")

	for k, v := range provConf.Models {
		if strings.EqualFold(k, stripped) {
			return v, true
		}
	}

	// 4. Longest-prefix match: if a configured model key is a prefix of the requested model name
	// e.g. configured "claude-3-5-sonnet", requested "claude-3-5-sonnet-20241022"
	var bestMatch ModelPrice
	var longestPrefix int
	for k, v := range provConf.Models {
		if k == "_fallback" {
			continue
		}
		lowerK := strings.ToLower(k)
		if strings.HasPrefix(lowerModel, lowerK) && len(lowerK) > longestPrefix {
			bestMatch = v
			longestPrefix = len(lowerK)
		}
	}
	if longestPrefix > 0 {
		return bestMatch, true
	}

	return ModelPrice{}, false
}

// GetModelPrice returns the pricing rates (input & output per 1M tokens) and default max output tokens for a model.
// Falls back to provider's "_fallback" if model not found.
func (s *Store) GetModelPrice(provider, model string) (inputPer1M, outputPer1M float64, defaultMaxOutputTokens int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provConf, hasProv := s.config.Providers[provider]
	if !hasProv {
		return 0, 0, 0
	}

	defaultMaxOutputTokens = provConf.DefaultMaxOutputTokens

	modelPrice, hasModel := s.findModelPrice(provConf, model)
	if !hasModel {
		fallback, hasFallback := provConf.Models["_fallback"]
		if hasFallback {
			return fallback.InputPer1M, fallback.OutputPer1M, defaultMaxOutputTokens
		}
		return 0, 0, defaultMaxOutputTokens
	}

	return modelPrice.InputPer1M, modelPrice.OutputPer1M, defaultMaxOutputTokens
}

// GetModelPriceDetails returns the full pricing rates (including cache read/write) and default max output tokens for a model.
func (s *Store) GetModelPriceDetails(provider, model string) (price ModelPrice, defaultMaxOutputTokens int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provConf, hasProv := s.config.Providers[provider]
	if !hasProv {
		return ModelPrice{}, 0
	}

	defaultMaxOutputTokens = provConf.DefaultMaxOutputTokens

	modelPrice, hasModel := s.findModelPrice(provConf, model)
	if !hasModel {
		fallback, hasFallback := provConf.Models["_fallback"]
		if hasFallback {
			return fallback, defaultMaxOutputTokens
		}
		return ModelPrice{}, defaultMaxOutputTokens
	}

	return modelPrice, defaultMaxOutputTokens
}

// GetFallback returns the configured fallback model for the given provider and model, or empty string if none configured.
func (s *Store) GetFallback(provider, model string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provConf, hasProv := s.config.Providers[provider]
	if !hasProv {
		return ""
	}

	modelPrice, hasModel := s.findModelPrice(provConf, model)
	if !hasModel {
		return ""
	}

	return modelPrice.Fallback
}

// GetFastTier returns the configured fast tier model for the given provider and model, or empty string if none configured.
func (s *Store) GetFastTier(provider, model string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	provConf, hasProv := s.config.Providers[provider]
	if !hasProv {
		return ""
	}

	modelPrice, hasModel := s.findModelPrice(provConf, model)
	if !hasModel {
		return ""
	}

	return modelPrice.FastTier
}

// GetToolCost returns the configured cost for the given tool, or the default unknown tool cost if not mapped.
func (s *Store) GetToolCost(toolName string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	costEntry, hasCost := s.config.ToolCosts.Tools[toolName]
	if !hasCost {
		return s.config.ToolCosts.Defaults.UnknownTool
	}
	return costEntry.CostPerCall
}

// IsFrontierModel determines whether a model belongs to the frontier reasoning tier
// using explicit tier metadata, fast-tier mappings, and non-fallback input pricing.
func (s *Store) IsFrontierModel(provider, model string) bool {
	m := strings.ToLower(model)
	// 1. Explicit lightweight qualifier exclusion always wins unless overridden by Tier == "frontier"
	isLightweightName := strings.Contains(m, "mini") || strings.Contains(m, "nano") ||
		strings.Contains(m, "haiku") || strings.Contains(m, "flash") ||
		strings.Contains(m, "small")

	s.mu.RLock()
	provConf, hasProv := s.config.Providers[provider]
	var modelPrice ModelPrice
	var hasModel bool
	if hasProv {
		modelPrice, hasModel = s.findModelPrice(provConf, model)
	}
	s.mu.RUnlock()

	if hasModel {
		switch strings.ToLower(modelPrice.Tier) {
		case "frontier":
			return true
		case "fast", "lightweight":
			return false
		}
		if modelPrice.FastTier != "" {
			return true
		}
		if !isLightweightName && modelPrice.InputPer1M >= 1.25 {
			return true
		}
	}

	if isLightweightName {
		return false
	}

	// Fallback heuristic for unmapped model aliases (e.g. "claude-3-5-sonnet-20241022")
	return strings.Contains(m, "gpt-5") || strings.Contains(m, "gpt-4") ||
		strings.Contains(m, "claude-") || strings.Contains(m, "o1") ||
		strings.Contains(m, "o3") || strings.Contains(m, "pro") ||
		strings.Contains(m, "large") || strings.Contains(m, "reasoner")
}

// GetProviders returns a copy of configured providers and their models.
func (s *Store) GetProviders() map[string]ProviderConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	copied := make(map[string]ProviderConfig, len(s.config.Providers))
	for k, v := range s.config.Providers {
		copied[k] = v
	}
	return copied
}

// ModelCount returns the total number of configured models across all providers.
func (s *Store) ModelCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, p := range s.config.Providers {
		count += len(p.Models)
	}
	return count
}

// FastTierCount returns the number of models with fast_tier targets configured.
func (s *Store) FastTierCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, p := range s.config.Providers {
		for _, m := range p.Models {
			if m.FastTier != "" {
				count++
			}
		}
	}
	return count
}

