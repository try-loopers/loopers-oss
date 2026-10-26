package proxy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanonicalizeRequest_ToolSorting(t *testing.T) {
	raw := `{
		"model": "gpt-4o",
		"tools": [
			{"type": "function", "function": {"name": "z_tool", "description": "zebra"}},
			{"type": "function", "function": {"name": "a_tool", "description": "apple"}},
			{"type": "function", "function": {"name": "m_tool", "description": "mango"}}
		]
	}`

	canonical, err := CanonicalizeRequest("openai", "key-123", []byte(raw))
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(canonical, &req)
	require.NoError(t, err)

	tools := req["tools"].([]interface{})
	assert.Equal(t, "a_tool", tools[0].(map[string]interface{})["function"].(map[string]interface{})["name"])
	assert.Equal(t, "m_tool", tools[1].(map[string]interface{})["function"].(map[string]interface{})["name"])
	assert.Equal(t, "z_tool", tools[2].(map[string]interface{})["function"].(map[string]interface{})["name"])
}

func TestCanonicalizeRequest_VolatileMetadataRelocation(t *testing.T) {
	raw := `{
		"model": "claude-3-5-sonnet",
		"system": "You are a helpful coding assistant.\nTimestamp: 2026-10-19T14:30:00Z\nRequest_ID: req-abc-123\nFollow instructions carefully.",
		"messages": [
			{"role": "user", "content": "How do I reverse a slice in Go?"}
		]
	}`

	canonical, err := CanonicalizeRequest("anthropic", "key-123", []byte(raw))
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(canonical, &req)
	require.NoError(t, err)

	// In Anthropic, system is converted into a content block array with cache_control
	sysBlocks := req["system"].([]interface{})
	block0 := sysBlocks[0].(map[string]interface{})
	sysText := block0["text"].(string)

	assert.NotContains(t, sysText, "Timestamp: 2026-10-19T14:30:00Z")
	assert.NotContains(t, sysText, "Request_ID: req-abc-123")
	assert.Contains(t, sysText, "You are a helpful coding assistant.")
	assert.Contains(t, sysText, "Follow instructions carefully.")

	// Verify volatile fields moved to last user message
	msgs := req["messages"].([]interface{})
	lastMsg := msgs[len(msgs)-1].(map[string]interface{})
	userContent := lastMsg["content"].(string)
	assert.Contains(t, userContent, "[Context: Timestamp: 2026-10-19T14:30:00Z | Request_ID: req-abc-123]")

	// Verify Anthropic cache_control block exists
	assert.Equal(t, "ephemeral", block0["cache_control"].(map[string]interface{})["type"])
}

func TestCanonicalizeRequest_OpenAIStreamOptionsAndKey(t *testing.T) {
	raw := `{
		"model": "gpt-4o",
		"stream": true,
		"messages": [
			{"role": "user", "content": "ping"}
		]
	}`

	canonical, err := CanonicalizeRequest("openai", "test-hash-456", []byte(raw))
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(canonical, &req)
	require.NoError(t, err)

	assert.Equal(t, "test-hash-456", req["prompt_cache_key"])
	streamOpts, ok := req["stream_options"].(map[string]interface{})
	require.True(t, ok)
	assert.True(t, streamOpts["include_usage"].(bool))
}

func TestCanonicalizeRequest_IdempotentWhenAlreadyCached(t *testing.T) {
	raw := `{
		"model": "claude-3-5-sonnet",
		"system": [
			{"type": "text", "text": "Base prompt", "cache_control": {"type": "ephemeral"}}
		],
		"messages": [{"role": "user", "content": "hi"}]
	}`

	canonical, err := CanonicalizeRequest("anthropic", "key-123", []byte(raw))
	require.NoError(t, err)

	assert.Equal(t, strings.TrimSpace(raw), strings.TrimSpace(string(canonical)))
}

func TestCanonicalizeRequest_AnthropicDualBreakpoints(t *testing.T) {
	raw := `{
		"model": "claude-sonnet-4-6",
		"system": "You are an enterprise assistant.",
		"tools": [
			{"name": "tool_a", "description": "Tool A"},
			{"name": "tool_b", "description": "Tool B"}
		],
		"messages": [{"role": "user", "content": "hello"}]
	}`

	canonical, err := CanonicalizeRequest("anthropic", "key-123", []byte(raw))
	require.NoError(t, err)

	var req map[string]interface{}
	err = json.Unmarshal(canonical, &req)
	require.NoError(t, err)

	// Invariant 1: System block must have cache_control
	sysBlocks := req["system"].([]interface{})
	require.Len(t, sysBlocks, 1)
	assert.Equal(t, "ephemeral", sysBlocks[0].(map[string]interface{})["cache_control"].(map[string]interface{})["type"])

	// Invariant 2: Last tool in tools must also have cache_control (dual breakpoint)
	tools := req["tools"].([]interface{})
	require.Len(t, tools, 2)
	lastTool := tools[1].(map[string]interface{})
	require.NotNil(t, lastTool["cache_control"])
	assert.Equal(t, "ephemeral", lastTool["cache_control"].(map[string]interface{})["type"])

	// First tool should NOT have cache_control (only the boundary tool)
	firstTool := tools[0].(map[string]interface{})
	assert.Nil(t, firstTool["cache_control"])
}

func TestCanonicalizeRequest_TopLevelCacheControl_NotMutated(t *testing.T) {
	raw := `{
		"model": "claude-sonnet-4-6",
		"cache_control": {"type": "ephemeral"},
		"messages": [{"role": "user", "content": "hello"}]
	}`

	canonical, err := CanonicalizeRequest("anthropic", "key-123", []byte(raw))
	require.NoError(t, err)
	assert.Equal(t, strings.TrimSpace(raw), strings.TrimSpace(string(canonical)))
}

func TestCanonicalizeRequest_OpenRouterAndAzure(t *testing.T) {
	for _, prov := range []string{"openrouter", "azure"} {
		raw := `{
			"model": "gpt-5.4",
			"stream": true,
			"messages": [{"role": "user", "content": "ping"}]
		}`

		canonical, err := CanonicalizeRequest(prov, "test-hash", []byte(raw))
		require.NoError(t, err)

		var req map[string]interface{}
		err = json.Unmarshal(canonical, &req)
		require.NoError(t, err)

		streamOpts, ok := req["stream_options"].(map[string]interface{})
		require.True(t, ok)
		assert.True(t, streamOpts["include_usage"].(bool))
	}
}

