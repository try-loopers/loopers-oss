package pricing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActualCostWithCache_Anthropic(t *testing.T) {
	// Anthropic Claude 3.5 Sonnet:
	// input = $3.00/1M, output = $15.00/1M
	// cache_read = $0.30/1M (0.10x), cache_write = $3.75/1M (1.25x)
	inputPrice := 3.00
	outputPrice := 15.00
	cacheReadPrice, cacheWritePrice := ResolveCachePrices("anthropic", inputPrice, 0, 0)

	assert.InDelta(t, 0.30, cacheReadPrice, 1e-6)
	assert.InDelta(t, 3.75, cacheWritePrice, 1e-6)

	// Turn: 1,000 uncached input, 20,000 cached read tokens, 500 output tokens
	actualUSD, savedUSD := ActualCostWithCache(1000, 500, 20000, 0, inputPrice, outputPrice, cacheReadPrice, cacheWritePrice)

	// Uncached input: 1000 * 3 / 1e6 = 0.003
	// Cached read: 20000 * 0.30 / 1e6 = 0.006
	// Output: 500 * 15 / 1e6 = 0.0075
	// Total actual = 0.003 + 0.006 + 0.0075 = 0.0165
	assert.InDelta(t, 0.0165, actualUSD, 1e-6)

	// Counterfactual: 21,000 total prompt tokens at full $3/1M = 0.063
	// Saved: 0.063 - (0.003 + 0.006) = 0.054
	assert.InDelta(t, 0.054, savedUSD, 1e-6)
}

func TestActualCostWithCache_OpenAI(t *testing.T) {
	// OpenAI GPT-4o:
	// input = $2.50/1M, output = $10.00/1M
	// cache_read = $1.25/1M (0.50x), cache_write = $2.50/1M (1.0x)
	inputPrice := 2.50
	outputPrice := 10.00
	cacheReadPrice, cacheWritePrice := ResolveCachePrices("openai", inputPrice, 0, 0)

	assert.InDelta(t, 1.25, cacheReadPrice, 1e-6)
	assert.InDelta(t, 2.50, cacheWritePrice, 1e-6)

	// 5,000 uncached, 15,000 cached read, 200 output
	actualUSD, savedUSD := ActualCostWithCache(5000, 200, 15000, 0, inputPrice, outputPrice, cacheReadPrice, cacheWritePrice)

	// Uncached: 5000 * 2.5 / 1e6 = 0.0125
	// Cached read: 15000 * 1.25 / 1e6 = 0.01875
	// Output: 200 * 10 / 1e6 = 0.002
	// Actual = 0.0125 + 0.01875 + 0.002 = 0.03325
	assert.InDelta(t, 0.03325, actualUSD, 1e-6)

	// Counterfactual: 20,000 prompt tokens at $2.50/1M = 0.050
	// Saved: 0.050 - (0.0125 + 0.01875) = 0.01875
	assert.InDelta(t, 0.01875, savedUSD, 1e-6)
}

func TestIsFrontierModel_2026Catalog(t *testing.T) {
	store, err := LoadStore("../../pricing.yaml")
	assert.NoError(t, err)

	// Frontier models MUST return true
	assert.True(t, store.IsFrontierModel("anthropic", "claude-sonnet-4-6"))
	assert.True(t, store.IsFrontierModel("anthropic", "claude-opus-4-6"))
	assert.True(t, store.IsFrontierModel("openai", "gpt-5.4"))
	assert.True(t, store.IsFrontierModel("openai", "gpt-5"))
	assert.True(t, store.IsFrontierModel("openai", "gpt-4o"))
	assert.True(t, store.IsFrontierModel("openai", "o3"))
	assert.True(t, store.IsFrontierModel("gemini", "gemini-3.5-pro"))
	assert.True(t, store.IsFrontierModel("gemini", "gemini-2.5-pro"))
	assert.True(t, store.IsFrontierModel("deepseek", "deepseek-v4-pro"))
	assert.True(t, store.IsFrontierModel("mistral", "mistral-large-latest"))

	// Fast/utility models MUST return false
	assert.False(t, store.IsFrontierModel("anthropic", "claude-haiku-4-5"))
	assert.False(t, store.IsFrontierModel("openai", "gpt-4o-mini"))
	assert.False(t, store.IsFrontierModel("openai", "gpt-4.1-nano"))
	assert.False(t, store.IsFrontierModel("openai", "o4-mini"))
	assert.False(t, store.IsFrontierModel("gemini", "gemini-3.5-flash"))
	assert.False(t, store.IsFrontierModel("gemini", "gemini-2.5-flash"))
	assert.False(t, store.IsFrontierModel("deepseek", "deepseek-v4-flash"))
	assert.False(t, store.IsFrontierModel("mistral", "mistral-small-latest"))

	// Unknown models with lightweight suffixes must return false and not inherit _fallback price
	assert.False(t, store.IsFrontierModel("openai", "unknown-nano-model"))
	assert.False(t, store.IsFrontierModel("anthropic", "custom-haiku-model"))
}

func TestFindModelPrice_DateSuffixAndAliases(t *testing.T) {
	store, err := LoadStore("../../pricing.yaml")
	assert.NoError(t, err)

	// 1. OpenAI dated snapshots must resolve to canonical model rates (not _fallback 30/180)
	in4o, out4o, _ := store.GetModelPrice("openai", "gpt-4o-2024-08-06")
	assert.Equal(t, 2.50, in4o)
	assert.Equal(t, 10.00, out4o)
	assert.Equal(t, "gpt-4o-mini", store.GetFastTier("openai", "gpt-4o-2024-08-06"))

	in4oMini, out4oMini, _ := store.GetModelPrice("openai", "gpt-4o-mini-2024-07-18")
	assert.Equal(t, 0.15, in4oMini)
	assert.Equal(t, 0.60, out4oMini)

	// chatgpt-4o-latest alias
	inChat, outChat, _ := store.GetModelPrice("openai", "chatgpt-4o-latest")
	assert.Equal(t, 2.50, inChat)
	assert.Equal(t, 10.00, outChat)

	// 2. Anthropic dated snapshots must resolve to canonical model rates (not _fallback 5/25)
	inSonnet, outSonnet, _ := store.GetModelPrice("anthropic", "claude-3-5-sonnet-20241022")
	assert.Equal(t, 3.00, inSonnet)
	assert.Equal(t, 15.00, outSonnet)
	assert.Equal(t, "claude-3-5-haiku", store.GetFastTier("anthropic", "claude-3-5-sonnet-20241022"))
	assert.True(t, store.IsFrontierModel("anthropic", "claude-3-5-sonnet-20241022"))

	inHaiku, outHaiku, _ := store.GetModelPrice("anthropic", "claude-3-5-haiku-20241022")
	assert.Equal(t, 0.80, inHaiku)
	assert.Equal(t, 4.00, outHaiku)
	assert.False(t, store.IsFrontierModel("anthropic", "claude-3-5-haiku-20241022"))

	// 3. Gemini dated snapshots / versions
	inGemini, outGemini, _ := store.GetModelPrice("gemini", "gemini-1.5-pro-002")
	assert.Equal(t, 1.25, inGemini)
	assert.Equal(t, 5.00, outGemini)
	assert.Equal(t, "gemini-1.5-flash", store.GetFastTier("gemini", "gemini-1.5-pro-002"))
}


