package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/try-loopers/loopers-oss/internal/provider"
	"github.com/try-loopers/loopers-oss/pkg/api"
)

type OpenAIProvider struct{}

// NewOpenAIProvider creates a new instance of OpenAIProvider.
func NewOpenAIProvider() *OpenAIProvider {
	return &OpenAIProvider{}
}

// Ensure OpenAIProvider implements provider.Provider.
var _ provider.Provider = (*OpenAIProvider)(nil)

func (o *OpenAIProvider) Name() string {
	return "openai"
}

func (o *OpenAIProvider) BaseURL() string {
	if override := os.Getenv("OPENAI_BASE_URL"); override != "" {
		return override
	}
	return "https://api.openai.com"
}

func (o *OpenAIProvider) InjectAuth(req *http.Request, providerKey string) {
	req.Header.Set("Authorization", "Bearer "+providerKey)
}

func (o *OpenAIProvider) RewritePath(originalPath string) string {
	return strings.TrimPrefix(originalPath, "/openai")
}

func (o *OpenAIProvider) ParseRequest(req *http.Request, body []byte) (model string, isStream bool, maxTokens int, mutatedBody []byte, err error) {
	var reqPayload struct {
		Model     string `json:"model"`
		MaxTokens *int   `json:"max_tokens"`
		Stream    bool   `json:"stream"`
	}
	if err := json.Unmarshal(body, &reqPayload); err != nil {
		return "", false, 0, nil, err
	}
	model = reqPayload.Model
	isStream = reqPayload.Stream
	if reqPayload.MaxTokens != nil {
		maxTokens = *reqPayload.MaxTokens
	}

	if isStream {
		var req map[string]interface{}
		if err := json.Unmarshal(body, &req); err == nil {
			var streamOpts map[string]interface{}
			if opts, ok := req["stream_options"]; ok {
				if optsMap, ok := opts.(map[string]interface{}); ok {
					streamOpts = optsMap
				}
			}
			if streamOpts == nil {
				streamOpts = make(map[string]interface{})
			}
			streamOpts["include_usage"] = true
			req["stream_options"] = streamOpts

			modified, err := json.Marshal(req)
			if err == nil {
				return model, isStream, maxTokens, modified, nil
			}
		}
	}

	return model, isStream, maxTokens, body, nil
}

func (o *OpenAIProvider) RewriteModel(req *http.Request, body []byte, fallbackModel string) ([]byte, error) {
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, err
	}
	reqMap["model"] = fallbackModel
	return json.Marshal(reqMap)
}

func (o *OpenAIProvider) CountInputTokens(ctx context.Context, model string, body []byte, providerKey string) (int, error) {
	return countOpenAIRequestTokens(model, body)
}

func (o *OpenAIProvider) ParseStreamChunk(chunk []byte) (inputTokens int, outputTokens int, isDone bool, err error) {
	return parseOpenAIStreamChunk(chunk)
}

func (o *OpenAIProvider) ParseNonStreamResponse(body []byte) (inputTokens int, outputTokens int, err error) {
	var respObj struct {
		Usage *api.OpenAIUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &respObj); err != nil {
		return 0, 0, err
	}
	if respObj.Usage == nil {
		return 0, 0, nil
	}
	return respObj.Usage.PromptTokens, respObj.Usage.CompletionTokens, nil
}

// ParseNonStreamUsage extracts detailed token usage including prompt_tokens_details.cached_tokens.
func (o *OpenAIProvider) ParseNonStreamUsage(body []byte) (api.TokenUsage, error) {
	var respObj struct {
		Usage *api.OpenAIUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &respObj); err != nil {
		return api.TokenUsage{}, err
	}
	if respObj.Usage == nil {
		return api.TokenUsage{}, nil
	}
	cached := 0
	if respObj.Usage.PromptTokensDetails != nil {
		cached = respObj.Usage.PromptTokensDetails.CachedTokens
	}
	uncached := respObj.Usage.PromptTokens - cached
	if uncached < 0 {
		uncached = respObj.Usage.PromptTokens
	}
	return api.TokenUsage{
		InputTokens:     uncached,
		OutputTokens:    respObj.Usage.CompletionTokens,
		CacheReadTokens: cached,
	}, nil
}

// ParseStreamChunkUsage processes an SSE stream chunk and extracts detailed token usage.
func (o *OpenAIProvider) ParseStreamChunkUsage(chunk []byte) (api.TokenUsage, bool, error) {
	return parseOpenAIStreamChunkUsage(chunk)
}

func (o *OpenAIProvider) FormatBudgetExceededSSE() []byte {
	return formatOpenAIBudgetExceededSSE()
}
