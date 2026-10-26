package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/try-loopers/loopers-oss/internal/provider"
	"github.com/try-loopers/loopers-oss/pkg/api"
)

type AnthropicProvider struct{}

// NewAnthropicProvider creates a new instance of AnthropicProvider.
func NewAnthropicProvider() *AnthropicProvider {
	return &AnthropicProvider{}
}

// Ensure AnthropicProvider implements provider.Provider and provider.CacheAwareProvider.
var _ provider.Provider = (*AnthropicProvider)(nil)
var _ provider.CacheAwareProvider = (*AnthropicProvider)(nil)

// Ensure AnthropicProvider implements provider.Provider.
var _ provider.Provider = (*AnthropicProvider)(nil) // wait, typo in compilation warning: should check against (*AnthropicProvider)

func (a *AnthropicProvider) Name() string {
	return "anthropic"
}

func (a *AnthropicProvider) BaseURL() string {
	return "https://api.anthropic.com"
}

func (a *AnthropicProvider) InjectAuth(req *http.Request, providerKey string) {
	req.Header.Set("x-api-key", providerKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	// Ensure anthropic-beta includes prompt-caching so ephemeral cache_control breakpoints are honored
	beta := req.Header.Get("anthropic-beta")
	if beta == "" {
		req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	} else if !strings.Contains(beta, "prompt-caching") {
		req.Header.Set("anthropic-beta", beta+",prompt-caching-2024-07-31")
	}
}

func (a *AnthropicProvider) RewritePath(originalPath string) string {
	return strings.TrimPrefix(originalPath, "/anthropic")
}

func (a *AnthropicProvider) ParseRequest(req *http.Request, body []byte) (model string, isStream bool, maxTokens int, mutatedBody []byte, err error) {
	var reqPayload struct {
		Model           string `json:"model"`
		Stream          bool   `json:"stream"`
		MaxTokens       *int   `json:"max_tokens"`
		MaxTokensSecond *int   `json:"max_tokens_to_sample"`
	}
	if err := json.Unmarshal(body, &reqPayload); err != nil {
		return "", false, 0, nil, err
	}

	maxTokensVal := 0
	if reqPayload.MaxTokens != nil {
		maxTokensVal = *reqPayload.MaxTokens
	} else if reqPayload.MaxTokensSecond != nil {
		maxTokensVal = *reqPayload.MaxTokensSecond
	}

	return reqPayload.Model, reqPayload.Stream, maxTokensVal, body, nil
}

func (a *AnthropicProvider) RewriteModel(req *http.Request, body []byte, fallbackModel string) ([]byte, error) {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, err
	}
	reqMap["model"] = fallbackModel
	return json.Marshal(reqMap)
}

func (a *AnthropicProvider) CountInputTokens(ctx context.Context, model string, body []byte, providerKey string) (int, error) {
	return countAnthropicRequestTokens(ctx, model, body, providerKey)
}

func (a *AnthropicProvider) ParseStreamChunk(chunk []byte) (inputTokens int, outputTokens int, isDone bool, err error) {
	return parseAnthropicStreamChunk(chunk)
}

func (a *AnthropicProvider) ParseNonStreamResponse(body []byte) (inputTokens int, outputTokens int, err error) {
	var respObj struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &respObj); err != nil {
		return 0, 0, err
	}
	if respObj.Usage == nil {
		return 0, 0, nil
	}
	return respObj.Usage.InputTokens, respObj.Usage.OutputTokens, nil
}

// ParseNonStreamUsage extracts detailed token usage including cache reads and writes.
func (a *AnthropicProvider) ParseNonStreamUsage(body []byte) (api.TokenUsage, error) {
	var respObj struct {
		Usage *api.AnthropicUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &respObj); err != nil {
		return api.TokenUsage{}, err
	}
	if respObj.Usage == nil {
		return api.TokenUsage{}, nil
	}
	return api.TokenUsage{
		InputTokens:         respObj.Usage.InputTokens,
		OutputTokens:        respObj.Usage.OutputTokens,
		CacheReadTokens:     respObj.Usage.CacheReadInputTokens,
		CacheCreationTokens: respObj.Usage.CacheCreationInputTokens,
	}, nil
}

// ParseStreamChunkUsage processes an Anthropic SSE stream chunk and extracts detailed token usage.
func (a *AnthropicProvider) ParseStreamChunkUsage(chunk []byte) (api.TokenUsage, bool, error) {
	return parseAnthropicStreamChunkUsage(chunk)
}

func (a *AnthropicProvider) FormatBudgetExceededSSE() []byte {
	return formatAnthropicBudgetExceededSSE()
}
