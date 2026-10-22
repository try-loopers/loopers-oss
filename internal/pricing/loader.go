package pricing

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/internal/logging"
)

// ModelPrice holds the cost parameters for a specific model.
type ModelPrice struct {
	InputPer1M      float64 `mapstructure:"input_per_1m" json:"input_per_1m"`
	OutputPer1M     float64 `mapstructure:"output_per_1m" json:"output_per_1m"`
	CacheReadPer1M  float64 `mapstructure:"cache_read_per_1m" json:"cache_read_per_1m"`
	CacheWritePer1M float64 `mapstructure:"cache_write_per_1m" json:"cache_write_per_1m"`
	Fallback        string  `mapstructure:"fallback" json:"fallback"`
	FastTier        string  `mapstructure:"fast_tier" json:"fast_tier"`
}

// ProviderConfig holds the pricing config for a specific provider.
type ProviderConfig struct {
	DefaultMaxOutputTokens int                   `mapstructure:"default_max_output_tokens" json:"default_max_output_tokens"`
	Models                 map[string]ModelPrice `mapstructure:"models" json:"models"`
}

// Config represents the schema of pricing.yaml.
type ToolCostEntry struct {
	CostPerCall float64 `mapstructure:"cost_per_call" json:"cost_per_call"`
}

type ToolCostsConfig struct {
	Defaults struct {
		UnknownTool float64 `mapstructure:"unknown_tool" json:"unknown_tool"`
	} `mapstructure:"defaults" json:"defaults"`
	Tools map[string]ToolCostEntry `mapstructure:"tools" json:"tools"`
}

type Config struct {
	Providers map[string]ProviderConfig `mapstructure:"providers" json:"providers"`
	ToolCosts ToolCostsConfig           `mapstructure:"tool_costs" json:"tool_costs"`
}

// Store is a thread-safe in-memory store for LLM pricing.
type Store struct {
	mu     sync.RWMutex
	config Config
}

// LoadStore loads the pricing config from the specified YAML path.
func LoadStore(path string) (*Store, error) {
	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read pricing config: %w", err)
	}

	var config Config
	if err := v.Unmarshal(&config); err != nil {
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

	modelPrice, hasModel := provConf.Models[model]
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

	modelPrice, hasModel := provConf.Models[model]
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

	modelPrice, hasModel := provConf.Models[model]
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

	modelPrice, hasModel := provConf.Models[model]
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
