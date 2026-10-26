package anthropic

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/try-loopers/loopers-oss/internal/provider"
)

func TestAnthropicProvider_CacheAwareInterface(t *testing.T) {
	p := NewAnthropicProvider()
	var cap provider.CacheAwareProvider = p
	assert.NotNil(t, cap)
}

func TestAnthropicProvider_ParseNonStreamUsage(t *testing.T) {
	p := NewAnthropicProvider()

	body := []byte(`{
		"id": "msg_01X9vhvwnz4Vvqa4j1j6FfB9",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello!"}],
		"model": "claude-3-5-sonnet-20241022",
		"stop_reason": "end_turn",
		"usage": {
			"input_tokens": 120,
			"output_tokens": 25,
			"cache_creation_input_tokens": 1024,
			"cache_read_input_tokens": 2048
		}
	}`)

	usage, err := p.ParseNonStreamUsage(body)
	assert.NoError(t, err)
	assert.Equal(t, 120, usage.InputTokens)
	assert.Equal(t, 25, usage.OutputTokens)
	assert.Equal(t, 1024, usage.CacheCreationTokens)
	assert.Equal(t, 2048, usage.CacheReadTokens)
}

func TestAnthropicProvider_ParseStreamChunkUsage(t *testing.T) {
	p := NewAnthropicProvider()

	// message_start event containing prompt tokens and cache creation/read
	startChunk := []byte(`event: message_start
data: {"type":"message_start","message":{"id":"msg_123","type":"message","role":"assistant","usage":{"input_tokens":50,"cache_creation_input_tokens":512,"cache_read_input_tokens":1024}}}`)

	usage, isDone, err := p.ParseStreamChunkUsage(startChunk)
	assert.NoError(t, err)
	assert.False(t, isDone)
	assert.Equal(t, 50, usage.InputTokens)
	assert.Equal(t, 512, usage.CacheCreationTokens)
	assert.Equal(t, 1024, usage.CacheReadTokens)
	assert.Equal(t, 0, usage.OutputTokens)

	// message_delta event containing output tokens
	deltaChunk := []byte(`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}`)

	deltaUsage, isDone, err := p.ParseStreamChunkUsage(deltaChunk)
	assert.NoError(t, err)
	assert.False(t, isDone)
	assert.Equal(t, 42, deltaUsage.OutputTokens)

	// message_stop event signaling completion
	stopChunk := []byte(`event: message_stop
data: {"type":"message_stop"}`)

	stopUsage, isDone, err := p.ParseStreamChunkUsage(stopChunk)
	assert.NoError(t, err)
	assert.True(t, isDone)
	assert.Equal(t, 0, stopUsage.OutputTokens)
}

func TestAnthropicProvider_InjectAuth_PromptCachingBeta(t *testing.T) {
	p := NewAnthropicProvider()

	// Case 1: Fresh request with no anthropic-beta header
	req1, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	p.InjectAuth(req1, "sk-ant-testkey")
	assert.Equal(t, "sk-ant-testkey", req1.Header.Get("x-api-key"))
	assert.Equal(t, "2023-06-01", req1.Header.Get("anthropic-version"))
	assert.Equal(t, "prompt-caching-2024-07-31", req1.Header.Get("anthropic-beta"))

	// Case 2: Existing anthropic-beta header from client without prompt-caching
	req2, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	req2.Header.Set("anthropic-beta", "pdfs-2024-09-25")
	p.InjectAuth(req2, "sk-ant-testkey")
	assert.Equal(t, "pdfs-2024-09-25,prompt-caching-2024-07-31", req2.Header.Get("anthropic-beta"))

	// Case 3: Existing anthropic-beta already contains prompt-caching
	req3, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	req3.Header.Set("anthropic-beta", "prompt-caching-2024-07-31,pdfs-2024-09-25")
	p.InjectAuth(req3, "sk-ant-testkey")
	assert.Equal(t, "prompt-caching-2024-07-31,pdfs-2024-09-25", req3.Header.Get("anthropic-beta"))
}
