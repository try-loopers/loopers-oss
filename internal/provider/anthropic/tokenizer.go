package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pkoukk/tiktoken-go"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/pkg/api"
)

// countAnthropicRequestTokens estimates the prompt tokens for Anthropic requests.
func countAnthropicRequestTokens(ctx context.Context, model string, body []byte, providerKey string) (int, error) {
	if providerKey != "" {
		tokens, err := callAnthropicCountAPI(ctx, body, providerKey)
		if err == nil {
			return tokens, nil
		}
		logging.Logger.Warn().Err(err).Msg("Anthropic count_tokens API failed, falling back to tiktoken estimation")
	}

	return countAnthropicTokensFallback(body)
}

func callAnthropicCountAPI(ctx context.Context, body []byte, providerKey string) (int, error) {
	apiCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	reqURL := "https://api.anthropic.com/v1/messages/count_tokens"
	req, err := http.NewRequestWithContext(apiCtx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}

	req.Header.Set("x-api-key", providerKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	req.Header.Set("content-type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("non-200 response from Anthropic API: %d", resp.StatusCode)
	}

	var countResp api.AnthropicCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&countResp); err != nil {
		return 0, err
	}

	return countResp.InputTokens, nil
}

func countAnthropicTokensFallback(body []byte) (int, error) {
	var req struct {
		Messages []api.Message `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return 0, err
	}

	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		return 0, err
	}

	tokens := 0
	for _, msg := range req.Messages {
		tokens += 4 // Average overhead per message
		switch c := msg.Content.(type) {
		case string:
			tokens += len(tke.Encode(c, nil, nil))
		case []interface{}:
			for _, part := range c {
				if partMap, ok := part.(map[string]interface{}); ok {
					if text, ok := partMap["text"].(string); ok {
						tokens += len(tke.Encode(text, nil, nil))
					}
				}
			}
		}
		tokens += len(tke.Encode(msg.Role, nil, nil))
	}

	return tokens, nil
}
