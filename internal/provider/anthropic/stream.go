package anthropic

import (
	"bytes"
	"encoding/json"

	"github.com/try-loopers/loopers-oss/pkg/api"
)

// parseAnthropicFrame parses event and data fields from an Anthropic SSE frame.
func parseAnthropicFrame(frame []byte) (eventType string, data []byte) {
	lines := bytes.Split(frame, []byte("\n"))
	for _, line := range lines {
		if bytes.HasPrefix(line, []byte("event: ")) {
			eventType = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("event: "))))
		}
		if bytes.HasPrefix(line, []byte("data: ")) {
			data = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data: ")))
		}
	}
	return
}

// parseAnthropicStreamChunk processes a single SSE chunk for Anthropic and extracts token usage if available.
func parseAnthropicStreamChunk(chunk []byte) (int, int, bool, error) {
	eventType, dataPayload := parseAnthropicFrame(chunk)

	if eventType == "message_start" {
		var msgStart api.AnthropicMessageStart
		if err := json.Unmarshal(dataPayload, &msgStart); err == nil {
			return msgStart.Message.Usage.InputTokens, 0, false, nil
		}
	} else if eventType == "message_delta" {
		var msgDelta api.AnthropicMessageDelta
		if err := json.Unmarshal(dataPayload, &msgDelta); err == nil {
			return 0, msgDelta.Usage.OutputTokens, false, nil
		}
	} else if eventType == "message_stop" {
		return 0, 0, true, nil
	}

	return 0, 0, false, nil
}

// parseAnthropicStreamChunkUsage extracts token usage including cache reads and writes.
func parseAnthropicStreamChunkUsage(chunk []byte) (api.TokenUsage, bool, error) {
	eventType, dataPayload := parseAnthropicFrame(chunk)

	if eventType == "message_start" {
		var msgStart api.AnthropicMessageStart
		if err := json.Unmarshal(dataPayload, &msgStart); err == nil {
			return api.TokenUsage{
				InputTokens:         msgStart.Message.Usage.InputTokens,
				CacheReadTokens:     msgStart.Message.Usage.CacheReadInputTokens,
				CacheCreationTokens: msgStart.Message.Usage.CacheCreationInputTokens,
			}, false, nil
		}
	} else if eventType == "message_delta" {
		var msgDelta api.AnthropicMessageDelta
		if err := json.Unmarshal(dataPayload, &msgDelta); err == nil {
			return api.TokenUsage{
				OutputTokens: msgDelta.Usage.OutputTokens,
			}, false, nil
		}
	} else if eventType == "message_stop" {
		return api.TokenUsage{}, true, nil
	}

	return api.TokenUsage{}, false, nil
}

// formatAnthropicBudgetExceededSSE returns the Anthropic SSE error frame for budget exceeded.
func formatAnthropicBudgetExceededSSE() []byte {
	return []byte("event: error\ndata: {\"error\":{\"type\":\"api_error\",\"message\":\"Budget exceeded mid-stream\"}}\n\n")
}
