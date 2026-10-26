package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
	"gopkg.in/yaml.v3"
)

func screenMCP() {
	ui.PrintLogo()
	ui.PrintHeader("MCP Governance Configuration")
	fmt.Println()

	var (
		enabled              = true
		maxRequestSize       = "1048576"
		allowToolsOverride   = false
		allowServersOverride = false
		cbEnabled            = true
		cbThreshold          = "5"
		cbWindowSeconds      = "60"
		sanitizerMaxDesc     = "1024"
		minifySchemas        = true
		memoizeReadOnly      = true
	)

	// Attempt to read current loopers.yaml to pre-fill
	data, err := os.ReadFile("loopers.yaml")
	if err == nil {
		var cfg map[string]interface{}
		if yaml.Unmarshal(data, &cfg) == nil {
			if mcpCfg, ok := cfg["mcp"].(map[string]interface{}); ok {
				if v, ok := mcpCfg["enabled"].(bool); ok {
					enabled = v
				}
				if v, ok := mcpCfg["max_request_size"].(int); ok {
					maxRequestSize = fmt.Sprintf("%d", v)
				}
				if v, ok := mcpCfg["allow_client_max_tools_override"].(bool); ok {
					allowToolsOverride = v
				}
				if v, ok := mcpCfg["allow_client_max_servers_override"].(bool); ok {
					allowServersOverride = v
				}

				if cbCfg, ok := mcpCfg["circuit_breaker"].(map[string]interface{}); ok {
					if v, ok := cbCfg["enabled"].(bool); ok {
						cbEnabled = v
					}
					if v, ok := cbCfg["threshold"].(int); ok {
						cbThreshold = fmt.Sprintf("%d", v)
					}
					if v, ok := cbCfg["window_seconds"].(int); ok {
						cbWindowSeconds = fmt.Sprintf("%d", v)
					}
				}

				if sCfg, ok := mcpCfg["sanitizer"].(map[string]interface{}); ok {
					if v, ok := sCfg["max_description_length"].(int); ok {
						sanitizerMaxDesc = fmt.Sprintf("%d", v)
					}
					if v, ok := sCfg["minify_schemas"].(bool); ok {
						minifySchemas = v
					}
					if v, ok := sCfg["memoize_read_only"].(bool); ok {
						memoizeReadOnly = v
					}
				}
			}

			if sCfg, ok := cfg["sanitizer"].(map[string]interface{}); ok {
				if v, ok := sCfg["minify_schemas"].(bool); ok {
					minifySchemas = v
				}
				if v, ok := sCfg["memoize_read_only"].(bool); ok {
					memoizeReadOnly = v
				}
			}
		}
	}

	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title("Enable MCP Governance? (Recommended: Yes)").Value(&enabled),
			huh.NewInput().Title("Max Request Size (bytes) (Recommended: 1MB)").Value(&maxRequestSize),
			huh.NewConfirm().Title("Allow Client Max Tools Override? (Recommended: No)").Value(&allowToolsOverride),
			huh.NewConfirm().Title("Allow Client Max Servers Override? (Recommended: No)").Value(&allowServersOverride),
		).Title("Global settings"),
		huh.NewGroup(
			huh.NewConfirm().Title("Enable Circuit Breaker? (Recommended: Yes)").Value(&cbEnabled),
			huh.NewInput().Title("Threshold (repeated identical calls) (Recommended: 5)").Value(&cbThreshold),
			huh.NewInput().Title("Window Seconds (Recommended: 60s)").Value(&cbWindowSeconds),
		).Title("Circuit Breaker"),
		huh.NewGroup(
			huh.NewInput().Title("Max Description Length (Recommended: 1024)").Value(&sanitizerMaxDesc),
			huh.NewConfirm().Title("Minify Tool Schemas? (Recommended: Yes)").Description("Strip schema redundancy to maximize KV cache hits").Value(&minifySchemas),
			huh.NewConfirm().Title("Memoize Read-Only Tool Responses? (Recommended: Yes)").Description("Cache idempotent MCP reads").Value(&memoizeReadOnly),
		).Title("Tool Sanitizer"),
	).WithTheme(ui.GetHuhTheme()).WithKeyMap(ui.GetHuhKeyMap()).Run()

	if err != nil {
		return
	}

	// Read existing yaml, map to map[string]interface{}, update mcp block, and write back
	data, err = os.ReadFile("loopers.yaml")
	var root map[string]interface{}
	if err == nil {
		yaml.Unmarshal(data, &root)
	} else {
		root = make(map[string]interface{})
	}

	mcpBlock := map[string]interface{}{
		"enabled":                           enabled,
		"max_request_size":                  parseInt(maxRequestSize),
		"allow_client_max_tools_override":   allowToolsOverride,
		"allow_client_max_servers_override": allowServersOverride,
		"circuit_breaker": map[string]interface{}{
			"enabled":        cbEnabled,
			"threshold":      parseInt(cbThreshold),
			"window_seconds": parseInt(cbWindowSeconds),
		},
		"sanitizer": map[string]interface{}{
			"max_description_length": parseInt(sanitizerMaxDesc),
			"minify_schemas":         minifySchemas,
			"memoize_read_only":      memoizeReadOnly,
		},
		"memoize_read_only": memoizeReadOnly,
	}

	// Also update root sanitizer block
	root["sanitizer"] = map[string]interface{}{
		"minify_schemas":    minifySchemas,
		"memoize_read_only": memoizeReadOnly,
	}

	// Preserve existing servers and tool_allowlist if they exist
	if existingMcp, ok := root["mcp"].(map[string]interface{}); ok {
		if servers, ok := existingMcp["servers"]; ok {
			mcpBlock["servers"] = servers
		}
		if sCfg, ok := existingMcp["sanitizer"].(map[string]interface{}); ok {
			if allowlist, ok := sCfg["tool_allowlist"]; ok {
				mcpBlock["sanitizer"].(map[string]interface{})["tool_allowlist"] = allowlist
			}
		}
	}
	root["mcp"] = mcpBlock

	out, err := yaml.Marshal(&root)
	if err != nil {
		ui.Error(fmt.Sprintf("Failed to encode YAML: %v", err))
	} else {
		err = os.WriteFile("loopers.yaml", out, 0600)
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to write loopers.yaml: %v", err))
		} else {
			ui.Success("Updated mcp configuration in loopers.yaml")
			ui.Warn("You must restart the firewall server for changes to take effect.")
		}
	}

	pressEnterToContinue()
}
