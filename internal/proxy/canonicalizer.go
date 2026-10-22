package proxy

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

var volatileHeaderRegex = regexp.MustCompile(`(?i)(current_time|timestamp|request_id|run_id|nonce):\s*[^\n]+`)

// CanonicalizeRequest normalizes prompt structure, deterministically sorts tools and schemas,
// relocates volatile per-request metadata from system prompts to trailing messages,
// and injects provider-appropriate prompt cache directives.
func CanonicalizeRequest(providerName, keyHash string, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}

	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err != nil {
		return body, nil // Return unmodified if not JSON
	}

	mutated := false

	// 1. Deterministic Tool & Schema Sorting
	if toolsRaw, exists := reqMap["tools"]; exists {
		if toolsSlice, ok := toolsRaw.([]interface{}); ok && len(toolsSlice) > 1 {
			sort.SliceStable(toolsSlice, func(i, j int) bool {
				return getToolSortKey(toolsSlice[i]) < getToolSortKey(toolsSlice[j])
			})
			reqMap["tools"] = toolsSlice
			mutated = true
		}
	}

	// 2. Volatile Prefix Field Relocation
	var extractedVolatile []string
	if sysRaw, exists := reqMap["system"]; exists {
		if sysStr, ok := sysRaw.(string); ok {
			cleanSys, matched := extractVolatileLines(sysStr)
			if len(matched) > 0 {
				reqMap["system"] = cleanSys
				extractedVolatile = append(extractedVolatile, matched...)
				mutated = true
			}
		}
	}

	// Check messages[0] if system/developer
	if msgsRaw, exists := reqMap["messages"]; exists {
		if msgs, ok := msgsRaw.([]interface{}); ok && len(msgs) > 0 {
			if firstMsg, ok := msgs[0].(map[string]interface{}); ok {
				role, _ := firstMsg["role"].(string)
				if role == "system" || role == "developer" {
					if contentStr, ok := firstMsg["content"].(string); ok {
						cleanContent, matched := extractVolatileLines(contentStr)
						if len(matched) > 0 {
							firstMsg["content"] = cleanContent
							extractedVolatile = append(extractedVolatile, matched...)
							mutated = true
						}
					}
				}
			}

			// Append extracted volatile lines to the last message if any were found
			if len(extractedVolatile) > 0 {
				lastIdx := len(msgs) - 1
				if lastMsg, ok := msgs[lastIdx].(map[string]interface{}); ok {
					trailingContext := "\n[Context: " + strings.Join(extractedVolatile, " | ") + "]"
					if contentStr, ok := lastMsg["content"].(string); ok {
						lastMsg["content"] = contentStr + trailingContext
						mutated = true
					}
				}
			}
		}
	}

	// 3. Provider-Specific Cache Directives & Stream Usage Injection
	switch providerName {
	case "anthropic":
		if injectAnthropicCacheControl(reqMap) {
			mutated = true
		}
	case "openai":
		if keyHash != "" {
			if _, exists := reqMap["prompt_cache_key"]; !exists {
				reqMap["prompt_cache_key"] = keyHash
				mutated = true
			}
		}
		if isStream, ok := reqMap["stream"].(bool); ok && isStream {
			if streamOpts, exists := reqMap["stream_options"].(map[string]interface{}); exists {
				if incUsage, ok := streamOpts["include_usage"].(bool); !ok || !incUsage {
					streamOpts["include_usage"] = true
					reqMap["stream_options"] = streamOpts
					mutated = true
				}
			} else {
				reqMap["stream_options"] = map[string]interface{}{"include_usage": true}
				mutated = true
			}
		}
	}

	if !mutated {
		return body, nil
	}

	return json.Marshal(reqMap)
}

func getToolSortKey(toolItem interface{}) string {
	toolMap, ok := toolItem.(map[string]interface{})
	if !ok {
		return ""
	}
	if name, ok := toolMap["name"].(string); ok && name != "" {
		return name
	}
	if fnMap, ok := toolMap["function"].(map[string]interface{}); ok {
		if name, ok := fnMap["name"].(string); ok {
			return name
		}
	}
	return ""
}

func extractVolatileLines(input string) (string, []string) {
	lines := strings.Split(input, "\n")
	var kept []string
	var extracted []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if volatileHeaderRegex.MatchString(trimmed) {
			extracted = append(extracted, trimmed)
		} else {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n"), extracted
}

func injectAnthropicCacheControl(reqMap map[string]interface{}) bool {
	// If cache_control already exists in system or tools, do nothing
	if hasAnthropicCacheControl(reqMap) {
		return false
	}

	ephemeral := map[string]interface{}{"type": "ephemeral"}

	// 1. Try system message block
	if sysRaw, exists := reqMap["system"]; exists {
		if sysStr, ok := sysRaw.(string); ok && len(sysStr) > 0 {
			reqMap["system"] = []interface{}{
				map[string]interface{}{
					"type":          "text",
					"text":          sysStr,
					"cache_control": ephemeral,
				},
			}
			return true
		}
		if sysBlocks, ok := sysRaw.([]interface{}); ok && len(sysBlocks) > 0 {
			if lastBlock, ok := sysBlocks[len(sysBlocks)-1].(map[string]interface{}); ok {
				lastBlock["cache_control"] = ephemeral
				return true
			}
		}
	}

	// 2. Alternatively, place on the final tool in tools
	if toolsRaw, exists := reqMap["tools"]; exists {
		if toolsSlice, ok := toolsRaw.([]interface{}); ok && len(toolsSlice) > 0 {
			if lastTool, ok := toolsSlice[len(toolsSlice)-1].(map[string]interface{}); ok {
				lastTool["cache_control"] = ephemeral
				return true
			}
		}
	}

	return false
}

func hasAnthropicCacheControl(reqMap map[string]interface{}) bool {
	if sysBlocks, ok := reqMap["system"].([]interface{}); ok {
		for _, b := range sysBlocks {
			if bMap, ok := b.(map[string]interface{}); ok {
				if _, exists := bMap["cache_control"]; exists {
					return true
				}
			}
		}
	}
	if tools, ok := reqMap["tools"].([]interface{}); ok {
		for _, t := range tools {
			if tMap, ok := t.(map[string]interface{}); ok {
				if _, exists := tMap["cache_control"]; exists {
					return true
				}
			}
		}
	}
	return false
}
