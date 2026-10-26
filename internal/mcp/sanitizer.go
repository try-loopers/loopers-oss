package mcp

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/try-loopers/loopers-oss/internal/inspector"
)

// ListToolsResult represents the payload in the JSON-RPC result for tools/list.
type ListToolsResult struct {
	Tools []Tool `json:"tools"`
}

// Tool represents a single tool returned by an MCP server.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// sanitizeString delegates to the unified inspector.SanitizeString function.
func sanitizeString(input string) string {
	return inspector.SanitizeString(input)
}

// SanitizeToolList takes a JSON-RPC response body for tools/list and applies
// sanitization rules, returning the new JSON body.
func SanitizeToolList(body []byte, cfg SanitizerConfig, serverName string) ([]byte, error) {
	var resp map[string]interface{}
	if err := json.Unmarshal(body, &resp); err != nil {
		// Not JSON, can't sanitize, return original
		return body, nil
	}

	resultMap, ok := resp["result"].(map[string]interface{})
	if !ok {
		return body, nil
	}

	toolsRaw, ok := resultMap["tools"].([]interface{})
	if !ok {
		return body, nil
	}

	var allowedTools map[string]bool
	if cfg.ToolAllowlist != nil {
		if list, exists := cfg.ToolAllowlist[serverName]; exists {
			allowedTools = make(map[string]bool)
			for _, t := range list {
				allowedTools[t] = true
			}
		}
	}

	var sanitizedTools []interface{}
	for _, tr := range toolsRaw {
		toolMap, ok := tr.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := toolMap["name"].(string)

		// 1. Tool allowlist
		if allowedTools != nil && !allowedTools[name] {
			continue // skip this tool
		}

		// 2. Strip injection patterns
		if desc, ok := toolMap["description"].(string); ok {
			cleanDesc := sanitizeString(desc)
			hasInjection := false
			for _, pat := range inspector.InjectionPatterns {
				if strings.Contains(cleanDesc, pat) {
					hasInjection = true
					break
				}
			}

			if hasInjection {
				// Strip malicious description entirely
				toolMap["description"] = "[Description removed due to security policy]"
			} else {
				// First-Sentence Compression when MinifySchemas is enabled
				if cfg.MinifySchemas {
					if idx := findFirstSentenceBoundary(desc); idx > 0 && idx < len(desc)-1 {
						desc = strings.TrimSpace(desc[:idx+1])
					}
				}
				// 3. Truncate description if clean
				if cfg.MaxDescriptionLength > 0 && len(desc) > cfg.MaxDescriptionLength {
					desc = desc[:cfg.MaxDescriptionLength] + "..."
				}
				toolMap["description"] = desc
			}
		}

		// Property Metadata Stripping when MinifySchemas is enabled
		if cfg.MinifySchemas {
			if inputSchema, ok := toolMap["inputSchema"].(map[string]interface{}); ok {
				cleanSchemaProperties(inputSchema)
			}
		}

		sanitizedTools = append(sanitizedTools, toolMap)
	}

	// 4. Deterministic Tool Sorting
	if len(sanitizedTools) > 1 {
		sort.SliceStable(sanitizedTools, func(i, j int) bool {
			m1, _ := sanitizedTools[i].(map[string]interface{})
			m2, _ := sanitizedTools[j].(map[string]interface{})
			n1, _ := m1["name"].(string)
			n2, _ := m2["name"].(string)
			return n1 < n2
		})
	}

	resultMap["tools"] = sanitizedTools
	resp["result"] = resultMap

	return json.Marshal(resp)
}

func findFirstSentenceBoundary(s string) int {
	for i := 0; i < len(s)-1; i++ {
		c := s[i]
		if (c == '.' || c == '!' || c == '?') && (s[i+1] == ' ' || s[i+1] == '\n' || s[i+1] == '\r') {
			return i
		}
		if c == '\n' {
			return i
		}
	}
	return -1
}

func cleanSchemaProperties(schema map[string]interface{}) {
	delete(schema, "$schema")
	delete(schema, "title")
	delete(schema, "examples")

	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for _, v := range props {
			if propMap, ok := v.(map[string]interface{}); ok {
				delete(propMap, "title")
				delete(propMap, "examples")
				delete(propMap, "$schema")
				delete(propMap, "default")
				if pDesc, ok := propMap["description"].(string); ok {
					if idx := findFirstSentenceBoundary(pDesc); idx > 0 && idx < len(pDesc)-1 {
						propMap["description"] = strings.TrimSpace(pDesc[:idx+1])
					}
				}
				if _, hasNested := propMap["properties"]; hasNested {
					cleanSchemaProperties(propMap)
				}
				if items, ok := propMap["items"].(map[string]interface{}); ok {
					cleanSchemaProperties(items)
				}
			}
		}
	}
}
