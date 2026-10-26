package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
)

func screenExec() {
	ui.PrintLogo()
	ui.PrintHeader("Transparent Execution Wrapper")
	fmt.Println()

	var (
		proxyKey      = os.Getenv("LOOPERS_PROXY_KEY")
		proxyURL      = os.Getenv("LOOPERS_PROXY_URL")
		provider      = os.Getenv("LOOPERS_PROVIDER")
		modelMap      = ""
		modelOverride = ""
		skipHydration = false
		forceFrontier = false
		commandToRun  = ""
	)

	if proxyURL == "" {
		proxyURL = "http://localhost:8080"
	}
	if provider == "" {
		provider = "auto-detect"
	}

	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Proxy Key").Value(&proxyKey).EchoMode(huh.EchoModePassword),
			huh.NewInput().Title("Proxy URL (Recommended: http://localhost:8080)").Value(&proxyURL),
		).Title("Proxy Configuration"),

		huh.NewGroup(
			huh.NewSelect[string]().Title("Provider (Recommended: Auto-Detect)").Options(
				huh.NewOption("Auto-Detect", "auto-detect"),
				huh.NewOption("OpenAI", "openai"),
				huh.NewOption("Anthropic", "anthropic"),
				huh.NewOption("Google", "google"),
				huh.NewOption("DeepSeek", "deepseek"),
				huh.NewOption("Ollama", "ollama"),
				huh.NewOption("OpenRouter", "openrouter"),
			).Value(&provider),
			huh.NewInput().Title("Model Map (Optional)").Description("e.g. gpt-4o=google/gemini-2.5-pro").Value(&modelMap),
			huh.NewInput().Title("Model Override (Optional)").Description("Force specific model").Value(&modelOverride),
			huh.NewConfirm().Title("Skip Tail Hydration? (Optional)").Description("Keep Ghost stubs unhydrated on intermediate turns").Value(&skipHydration),
			huh.NewConfirm().Title("Force Frontier Model? (Optional)").Description("Bypass smart fast-tier routing").Value(&forceFrontier),
			huh.NewInput().Title("Command to Run").Description("e.g. aider, openhands, pi, dsh, or claude").Value(&commandToRun).Validate(func(s string) error {
				if strings.TrimSpace(s) == "" {
					return fmt.Errorf("command is required")
				}
				return nil
			}),
		).Title("Execution Overrides"),
	).WithTheme(ui.GetHuhTheme()).WithKeyMap(ui.GetHuhKeyMap()).Run()

	if err != nil {
		return
	}

	// Set environments so execCmd picks them up
	os.Setenv("LOOPERS_PROXY_KEY", proxyKey)
	os.Setenv("LOOPERS_PROXY_URL", proxyURL)
	if provider != "auto-detect" {
		os.Setenv("LOOPERS_PROVIDER", provider)
	} else {
		os.Unsetenv("LOOPERS_PROVIDER")
	}

	if skipHydration {
		os.Setenv("LOOPERS_SKIP_HYDRATION", "true")
	} else {
		os.Unsetenv("LOOPERS_SKIP_HYDRATION")
	}

	if forceFrontier {
		os.Setenv("LOOPERS_FORCE_FRONTIER", "true")
	} else {
		os.Unsetenv("LOOPERS_FORCE_FRONTIER")
	}

	// Set globals for execCmd
	execModelMap = modelMap
	execModelOverride = modelOverride

	// Parse command string into slice simply
	// A robust parser would handle quotes, but fields is good enough for basic tests
	args := strings.Fields(commandToRun)
	if len(args) == 0 {
		return
	}

	fmt.Printf("\nExecuting: %s\n", commandToRun)
	fmt.Println(strings.Repeat("-", 40))

	// Execute it!
	execCmd.Run(execCmd, args)

	fmt.Println(strings.Repeat("-", 40))
	pressEnterToContinue()
}
