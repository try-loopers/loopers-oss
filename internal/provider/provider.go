package provider

import (
	"context"
	"net/http"

	"github.com/try-loopers/loopers-oss/pkg/api"
)

// Provider defines the contract every AI provider adapter must implement.
type Provider interface {
	// Name returns the provider identifier (e.g., "openai", "gemini").
	Name() string

	// BaseURL returns the upstream API base URL.
	BaseURL() string

	// InjectAuth sets the provider-specific auth headers and parameters on the outgoing request.
	// providerKey is the raw API key from X-Loopers-Provider-Key.
	InjectAuth(req *http.Request, providerKey string)

	// RewritePath transforms the Loopers route path to the upstream path.
	// e.g., "/anthropic/v1/messages" -> "/v1/messages"
	RewritePath(originalPath string) string

	// ParseRequest extracts model name, streaming flag, and max_tokens from the request/body.
	ParseRequest(req *http.Request, body []byte) (model string, isStream bool, maxTokens int, mutatedBody []byte, err error)

	// RewriteModel rewrites the request to use a fallback model.
	// It modifies req in-place if needed (e.g. Gemini/Azure URL) and returns the modified JSON body.
	RewriteModel(req *http.Request, body []byte, fallbackModel string) ([]byte, error)

	// CountInputTokens returns the estimated input token count for the request.
	// ctx and providerKey are passed for providers that require API-based counting (Gemini, Anthropic).
	CountInputTokens(ctx context.Context, model string, body []byte, providerKey string) (int, error)

	// ParseStreamChunk processes a single SSE/EventStream chunk and returns token usage if available.
	// Returns (inputTokens, outputTokens, isDone, err).
	ParseStreamChunk(chunk []byte) (inputTokens int, outputTokens int, isDone bool, err error)

	// ParseNonStreamResponse extracts token usage from a non-streaming response body.
	ParseNonStreamResponse(body []byte) (inputTokens int, outputTokens int, err error)

	// FormatBudgetExceededSSE returns the provider-specific SSE error frame for mid-stream budget cut.
	FormatBudgetExceededSSE() []byte
}

// CacheAwareProvider is an optional interface implemented by providers that report
// KV-cache reads and writes (e.g. Anthropic, OpenAI).
type CacheAwareProvider interface {
	Provider
	ParseNonStreamUsage(body []byte) (usage api.TokenUsage, err error)
	ParseStreamChunkUsage(chunk []byte) (usage api.TokenUsage, isDone bool, err error)
}
