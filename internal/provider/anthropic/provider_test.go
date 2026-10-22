package anthropic

import (
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
