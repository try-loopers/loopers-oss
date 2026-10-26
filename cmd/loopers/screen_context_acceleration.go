package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/huh"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
	"github.com/try-loopers/loopers-oss/internal/pricing"
	"gopkg.in/yaml.v3"
)

func screenContextAcceleration() {
	ui.PrintLogo()
	ui.PrintHeader("Context Acceleration & Ghost Tokens (Regime 2)\nSlash multi-turn token costs by 65%–75% with zero quality loss")
	fmt.Println()

	var (
		ghostEnabled     = true
		thresholdBytes   = "512"
		ttlSeconds       = "3600"
		encryptionSecret = ""

		minifySchemas   = true
		memoizeReadOnly = true

		pricingPath = "./pricing.yaml"
		fastTier    = ""
	)

	// Attempt to read current loopers.yaml to pre-fill
	data, err := os.ReadFile("loopers.yaml")
	if err == nil {
		var cfg map[string]interface{}
		if yaml.Unmarshal(data, &cfg) == nil {
			if pPath, ok := cfg["pricing_path"].(string); ok && pPath != "" {
				pricingPath = pPath
			}

			if gCfg, ok := cfg["ghost_tokens"].(map[string]interface{}); ok {
				if v, ok := gCfg["enabled"].(bool); ok {
					ghostEnabled = v
				}
				if v, ok := gCfg["threshold_bytes"].(int); ok {
					thresholdBytes = fmt.Sprintf("%d", v)
				}
				if v, ok := gCfg["ttl_seconds"].(int); ok {
					ttlSeconds = fmt.Sprintf("%d", v)
				}
				if v, ok := gCfg["encryption_secret"].(string); ok {
					encryptionSecret = v
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

	// Try reading pricing.yaml to inspect fast-tier models
	store, pErr := pricing.LoadStore(pricingPath)
	var fastTierInfo string
	if pErr == nil && store != nil {
		fastTierInfo = fmt.Sprintf("pricing.yaml loaded (%d models mapped, %d fast_tier targets)", store.ModelCount(), store.FastTierCount())
	} else {
		fastTierInfo = fmt.Sprintf("pricing.yaml not loaded: %v", pErr)
	}

	err = huh.NewForm(
		// Group 1: Ghost Token Side-Channel Offload
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Ghost Tokens? (Recommended: Yes)").
				Description("Offload bulky tool results (>512B) to encrypted Redis cache").
				Value(&ghostEnabled),
			huh.NewInput().
				Title("Payload Threshold Bytes (Recommended: 512)").
				Description("Tool responses larger than this will be replaced with ~80-token ghost stubs").
				Value(&thresholdBytes),
			huh.NewInput().
				Title("Cache TTL Seconds (Recommended: 3600)").
				Description("Retention period for offloaded tool payloads in Redis").
				Value(&ttlSeconds),
			huh.NewInput().
				Title("AES-256-GCM Encryption Secret (Optional)").
				Description("Leave blank to use environment variable LOOPERS_GHOST_SECRET").
				EchoMode(huh.EchoModePassword).
				Value(&encryptionSecret),
		).Title("Ghost Tokens (Side-Channel Offload)"),

		// Group 2: Tool Sanitization & KV-Cache Maximization
		huh.NewGroup(
			huh.NewConfirm().
				Title("Minify Tool Schemas? (Recommended: Yes)").
				Description("Strip verbose markdown and whitespace from MCP tool schemas to save prompt tokens").
				Value(&minifySchemas),
			huh.NewConfirm().
				Title("Memoize Read-Only Tool Responses? (Recommended: Yes)").
				Description("Cache idempotent read-only tool responses to avoid redundant upstream calls").
				Value(&memoizeReadOnly),
		).Title("Tool Sanitization & Cache Canonicalization"),

		// Group 3: Fast-Tier Model Routing
		huh.NewGroup(
			huh.NewInput().
				Title("Pricing File Path (Recommended: ./pricing.yaml)").
				Description(fastTierInfo).
				Value(&pricingPath),
			huh.NewInput().
				Title("Default Fast-Tier Target Model (Optional)").
				Description("e.g. gpt-4o-mini, claude-3-5-haiku-20241022, deepseek-v4.1-flash-1017").
				Value(&fastTier),
		).Title("Smart Fast-Tier Routing"),
	).WithTheme(ui.GetHuhTheme()).WithKeyMap(ui.GetHuhKeyMap()).Run()

	if err != nil {
		return
	}

	// Read existing yaml, map to map[string]interface{}, update blocks, and write back
	data, err = os.ReadFile("loopers.yaml")
	var root map[string]interface{}
	if err == nil {
		yaml.Unmarshal(data, &root)
	} else {
		root = make(map[string]interface{})
	}

	// Update ghost_tokens
	ghostBlock := map[string]interface{}{
		"enabled":         ghostEnabled,
		"threshold_bytes": parseInt(thresholdBytes),
		"ttl_seconds":     parseInt(ttlSeconds),
	}
	if encryptionSecret != "" {
		ghostBlock["encryption_secret"] = encryptionSecret
	} else {
		if existingG, ok := root["ghost_tokens"].(map[string]interface{}); ok {
			if secret, ok := existingG["encryption_secret"]; ok {
				ghostBlock["encryption_secret"] = secret
			}
		}
	}
	root["ghost_tokens"] = ghostBlock

	// Update sanitizer
	sanitizerBlock := map[string]interface{}{
		"minify_schemas":    minifySchemas,
		"memoize_read_only": memoizeReadOnly,
	}
	if existingS, ok := root["sanitizer"].(map[string]interface{}); ok {
		for k, v := range existingS {
			if k != "minify_schemas" && k != "memoize_read_only" {
				sanitizerBlock[k] = v
			}
		}
	}
	root["sanitizer"] = sanitizerBlock

	if pricingPath != "" {
		root["pricing_path"] = pricingPath
	}

	out, err := yaml.Marshal(&root)
	if err != nil {
		ui.Error(fmt.Sprintf("Failed to encode YAML: %v", err))
	} else {
		err = os.WriteFile("loopers.yaml", out, 0600)
		if err != nil {
			ui.Error(fmt.Sprintf("Failed to write loopers.yaml: %v", err))
		} else {
			ui.Success("Updated Context Acceleration & Ghost Tokens configuration in loopers.yaml")
			ui.Warn("You must restart the firewall server for changes to take effect.")
		}
	}

	pressEnterToContinue()
}
