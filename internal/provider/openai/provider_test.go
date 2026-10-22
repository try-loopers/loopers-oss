package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/try-loopers/loopers-oss/internal/provider"
)

func TestOpenAIProvider_CacheAwareInterface(t *testing.T) {
	p := NewOpenAIProvider()
	var cap provider.CacheAwareProvider = p
	assert.NotNil(t, cap)
}

func TestOpenAIProvider_ParseNonStreamUsage(t *testing.T) {
	p := NewOpenAIProvider()

	body := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"usage": {
			"prompt_tokens": 2048,
			"completion_tokens": 150,
			"total_tokens": 2198,
			"prompt_tokens_details": {
				"cached_tokens": 1024
			}
		}
	}`)

	usage, err := p.ParseNonStreamUsage(body)
	assert.NoError(t, err)
	assert.Equal(t, 1024, usage.InputTokens) // 2048 - 1024 cached = 1024 uncached
	assert.Equal(t, 150, usage.OutputTokens)
	assert.Equal(t, 1024, usage.CacheReadTokens)
	assert.Equal(t, 0, usage.CacheCreationTokens)
}

func TestOpenAIProvider_ParseStreamChunkUsage(t *testing.T) {
	p := NewOpenAIProvider()

	// Stream chunk with usage details
	chunk := []byte(`data: {"id":"chatcmpl-123","choices":[],"usage":{"prompt_tokens":4096,"completion_tokens":256,"prompt_tokens_details":{"cached_tokens":3000}}}`)

	usage, isDone, err := p.ParseStreamChunkUsage(chunk)
	assert.NoError(t, err)
	assert.False(t, isDone)
	assert.Equal(t, 1096, usage.InputTokens) // 4096 - 3000 = 1096
	assert.Equal(t, 256, usage.OutputTokens)
	assert.Equal(t, 3000, usage.CacheReadTokens)

	// Stream done chunk
	doneChunk := []byte(`data: [DONE]`)
	doneUsage, isDone, err := p.ParseStreamChunkUsage(doneChunk)
	assert.NoError(t, err)
	assert.True(t, isDone)
	assert.Equal(t, 0, doneUsage.InputTokens)
}
