package pricing

// EstimateCost computes the maximum possible cost based on input tokens and max output tokens.
func EstimateCost(inputTokens, maxOutputTokens int, inputPricePer1M, outputPricePer1M float64) float64 {
	inputCost := (float64(inputTokens) * inputPricePer1M) / 1000000.0
	outputCost := (float64(maxOutputTokens) * outputPricePer1M) / 1000000.0
	return inputCost + outputCost
}

// ActualCost computes the actual cost based on input tokens and actual output tokens.
func ActualCost(inputTokens, outputTokens int, inputPricePer1M, outputPricePer1M float64) float64 {
	inputCost := (float64(inputTokens) * inputPricePer1M) / 1000000.0
	outputCost := (float64(outputTokens) * outputPricePer1M) / 1000000.0
	return inputCost + outputCost
}

// ResolveCachePrices returns the effective cache read and cache write prices per 1M tokens.
// If not explicitly configured, it applies industry standard provider multipliers:
// Anthropic: cache read = 0.1x, cache write = 1.25x.
// OpenAI/Others: cache read = 0.5x, cache write = 1.0x.
func ResolveCachePrices(provider string, inputPer1M, cacheReadPer1M, cacheWritePer1M float64) (resolvedRead, resolvedWrite float64) {
	resolvedRead = cacheReadPer1M
	resolvedWrite = cacheWritePer1M

	if resolvedRead <= 0 {
		if provider == "anthropic" {
			resolvedRead = inputPer1M * 0.10
		} else {
			resolvedRead = inputPer1M * 0.50
		}
	}

	if resolvedWrite <= 0 {
		if provider == "anthropic" {
			resolvedWrite = inputPer1M * 1.25
		} else {
			resolvedWrite = inputPer1M * 1.00
		}
	}

	return resolvedRead, resolvedWrite
}

// ActualCostWithCache computes exact provider spend accounting for KV-cache reads and writes,
// and returns both actual spend in USD and counterfactual savings compared to an un-cached run.
func ActualCostWithCache(inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int, inputPer1M, outputPer1M, cacheReadPer1M, cacheWritePer1M float64) (actualUSD float64, savedUSD float64) {
	uncachedInputCost := (float64(inputTokens) * inputPer1M) / 1e6
	cacheReadCost := (float64(cacheReadTokens) * cacheReadPer1M) / 1e6
	cacheWriteCost := (float64(cacheCreationTokens) * cacheWritePer1M) / 1e6
	outputCost := (float64(outputTokens) * outputPer1M) / 1e6

	actualUSD = uncachedInputCost + cacheReadCost + cacheWriteCost + outputCost

	// Counterfactual: what the user would have paid for all input tokens without prompt caching
	totalPromptTokens := inputTokens + cacheReadTokens + cacheCreationTokens
	counterfactualInputUSD := (float64(totalPromptTokens) * inputPer1M) / 1e6
	savedUSD = counterfactualInputUSD - (uncachedInputCost + cacheReadCost + cacheWriteCost)
	if savedUSD < 0 {
		savedUSD = 0
	}

	return actualUSD, savedUSD
}
