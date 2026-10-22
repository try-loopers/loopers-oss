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
