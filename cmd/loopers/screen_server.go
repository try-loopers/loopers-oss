package main

import (
	"fmt"
	"os"

	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
)

func screenInit() {
	ui.PrintLogo()
	ui.PrintHeader("Loopers Setup Wizard\nConfigure your AI Firewall")
	fmt.Println()

	var (
		redisURL            = viper.GetString("redis.addr")
		redisPass           = viper.GetString("redis.password")
		redisDB             = viper.GetString("redis.db")
		serverPort          = viper.GetString("server.port")
		adminHost           = viper.GetString("server.admin_host")
		adminPort           = viper.GetString("server.admin_port")
		serverSecret        = viper.GetString("server.server_secret")
		logLevel            = viper.GetString("log.level")
		maxPayload          = viper.GetString("server.max_payload_bytes")
		stripBudgetHeaders  = !viper.GetBool("server.keep_budget_headers") // keep_budget_headers does not exist, so default is false, !false = true
		insecureDev         = viper.GetBool("server.insecure_dev")
		tlsCert             = viper.GetString("server.tls_cert_file")
		tlsKey              = viper.GetString("server.tls_key_file")
		pricingPath         = viper.GetString("pricing_path")
		sessionMaxPerKey    = viper.GetString("session.max_per_key")
		allowClientOverride = viper.GetBool("session.allow_client_budget_override")
		enableGhostTokens   = true
		minifySchemas       = true
		memoizeReadOnly     = true
	)

	// Defaults if empty
	if redisURL == "" {
		redisURL = "localhost:6379"
	}
	if redisDB == "" {
		redisDB = "0"
	}
	if serverPort == "" {
		serverPort = "8080"
	}
	if adminHost == "" {
		adminHost = "127.0.0.1"
	}
	if adminPort == "" {
		adminPort = "9090"
	}
	if logLevel == "" {
		logLevel = "info"
	}
	if maxPayload == "" || maxPayload == "0" {
		maxPayload = "8388608"
	}
	if pricingPath == "" {
		pricingPath = "./pricing.yaml"
	}
	if sessionMaxPerKey == "" || sessionMaxPerKey == "0" {
		sessionMaxPerKey = "10"
	}

	err := huh.NewForm(
		// Group 1: Core Infrastructure
		huh.NewGroup(
			huh.NewInput().Title("Redis URL (Recommended: localhost:6379)").Value(&redisURL).Validate(func(s string) error {
				if !strings.Contains(s, ":") {
					return fmt.Errorf("must match host:port format")
				}
				return nil
			}),
			huh.NewInput().Title("Redis Password (Optional)").Value(&redisPass).EchoMode(huh.EchoModePassword),
			huh.NewInput().Title("Redis DB (Recommended: 0)").Value(&redisDB),
			huh.NewInput().Title("Server Port (Recommended: 8080)").Value(&serverPort),
			huh.NewInput().Title("Admin Host (Recommended: 127.0.0.1)").Value(&adminHost),
			huh.NewInput().Title("Admin Port (Recommended: 9090)").Value(&adminPort),
			huh.NewInput().Title("Server Secret (Optional, for secure hashing)").Value(&serverSecret).EchoMode(huh.EchoModePassword),
			huh.NewSelect[string]().Title("Log Level (Recommended: Info)").Options(
				huh.NewOption("Info", "info"),
				huh.NewOption("Debug", "debug"),
				huh.NewOption("Warn", "warn"),
				huh.NewOption("Error", "error"),
			).Value(&logLevel),
		).Title("Core Infrastructure"),

		// Group 2: Firewall Settings
		huh.NewGroup(
			huh.NewInput().Title("Max Payload Bytes (Recommended: 8MB)").Value(&maxPayload),
			huh.NewConfirm().Title("Strip Budget Headers? (Recommended: Yes)").Value(&stripBudgetHeaders),
			huh.NewConfirm().Title("Insecure Dev Mode? (Recommended: No)").Value(&insecureDev),
			huh.NewInput().Title("TLS Cert File (Optional)").Value(&tlsCert),
			huh.NewInput().Title("TLS Key File (Optional)").Value(&tlsKey),
			huh.NewInput().Title("Pricing Path (Recommended: ./pricing.yaml)").Value(&pricingPath),
		).Title("Firewall Settings"),

		// Group 3: Session Config
		huh.NewGroup(
			huh.NewInput().Title("Max Concurrent Sessions per Key (Recommended: 10)").Value(&sessionMaxPerKey),
			huh.NewConfirm().Title("Allow Client Budget Overrides? (Recommended: No)").Description("Should clients be able to override budgets via HTTP headers?").Value(&allowClientOverride),
		).Title("Session Config"),

		// Group 4: Context Acceleration & FinOps (Regime 2)
		huh.NewGroup(
			huh.NewConfirm().Title("Enable Ghost Tokens? (Recommended: Yes)").Description("Offload bulky tool results (>512B) to save 65%-75% tokens").Value(&enableGhostTokens),
			huh.NewConfirm().Title("Minify Tool Schemas? (Recommended: Yes)").Description("Strip schema redundancy to maximize KV cache hits").Value(&minifySchemas),
			huh.NewConfirm().Title("Memoize Read-Only Tool Responses? (Recommended: Yes)").Description("Cache idempotent MCP reads").Value(&memoizeReadOnly),
		).Title("Context Acceleration (Regime 2)"),
	).WithTheme(ui.GetHuhTheme()).WithKeyMap(ui.GetHuhKeyMap()).Run()

	if err != nil {
		return
	}

	// Generate loopers.yaml
	yamlLines := []string{
		"server:",
		fmt.Sprintf("  port: %s", serverPort),
		fmt.Sprintf("  max_payload_bytes: %s", maxPayload),
		fmt.Sprintf("  admin_host: \"%s\"", adminHost),
		fmt.Sprintf("  admin_port: %s", adminPort),
	}
	if serverSecret != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("  server_secret: \"%s\"", serverSecret))
	}
	if tlsCert != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("  tls_cert_file: \"%s\"", tlsCert))
	}
	if tlsKey != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("  tls_key_file: \"%s\"", tlsKey))
	}
	if insecureDev {
		yamlLines = append(yamlLines, "  insecure_dev: true")
	}
	if !stripBudgetHeaders {
		yamlLines = append(yamlLines, "  strip_budget_headers: false")
	}

	yamlLines = append(yamlLines, "redis:")
	yamlLines = append(yamlLines, fmt.Sprintf("  addr: \"%s\"", redisURL))
	yamlLines = append(yamlLines, fmt.Sprintf("  password: \"%s\"", redisPass))
	if redisDB != "0" && redisDB != "" {
		yamlLines = append(yamlLines, fmt.Sprintf("  db: %s", redisDB))
	}

	yamlLines = append(yamlLines, "log:")
	yamlLines = append(yamlLines, fmt.Sprintf("  level: \"%s\"", logLevel))
	yamlLines = append(yamlLines, fmt.Sprintf("pricing_path: \"%s\"", pricingPath))

	yamlLines = append(yamlLines, "")
	yamlLines = append(yamlLines, "session:")
	yamlLines = append(yamlLines, fmt.Sprintf("  max_per_key: %s", sessionMaxPerKey))
	if allowClientOverride {
		yamlLines = append(yamlLines, "  allow_client_budget_override: true")
	} else {
		yamlLines = append(yamlLines, "  allow_client_budget_override: false")
	}

	yamlLines = append(yamlLines, "")
	yamlLines = append(yamlLines, "ghost_tokens:")
	if enableGhostTokens {
		yamlLines = append(yamlLines, "  enabled: true")
		yamlLines = append(yamlLines, "  threshold_bytes: 512")
		yamlLines = append(yamlLines, "  ttl_seconds: 3600")
	} else {
		yamlLines = append(yamlLines, "  enabled: false")
	}

	yamlLines = append(yamlLines, "")
	yamlLines = append(yamlLines, "sanitizer:")
	yamlLines = append(yamlLines, "  enabled: true")
	if minifySchemas {
		yamlLines = append(yamlLines, "  minify_schemas: true")
	} else {
		yamlLines = append(yamlLines, "  minify_schemas: false")
	}
	if memoizeReadOnly {
		yamlLines = append(yamlLines, "  memoize_read_only: true")
	} else {
		yamlLines = append(yamlLines, "  memoize_read_only: false")
	}

	yamlContent := strings.Join(yamlLines, "\n") + "\n"

	err = os.WriteFile("loopers.yaml", []byte(yamlContent), 0600)
	if err != nil {
		ui.Error(fmt.Sprintf("Error writing loopers.yaml: %v", err))
	} else {
		ui.Success("loopers.yaml written")
	}

	composeContent := `version: '3.8'
services:
  redis:
    image: redis:8-alpine
    container_name: loopers-redis
    command: ["redis-server", "--requirepass", "${REDIS_PASSWORD:-demo-pass}"]
    environment:
      - REDIS_PASSWORD=${REDIS_PASSWORD:-demo-pass}
    healthcheck:
      test: ["CMD", "sh", "-c", "redis-cli -a $$REDIS_PASSWORD ping"]
      interval: 2s
      timeout: 2s
      retries: 5
    ports:
      - "6379:6379"
    networks:
      - loopers-net

  loopers:
    image: ghcr.io/try-loopers/loopers:latest
    container_name: loopers-firewall
    ports:
      - "8080:8080"
    environment:
      - REDIS_ADDR=redis:6379
      - REDIS_PASSWORD=${REDIS_PASSWORD:-demo-pass}
      - SERVER_PORT=8080
      - SERVER_ADMIN_HOST=0.0.0.0
      - PRICING_PATH=/app/pricing.yaml
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - loopers-net

networks:
  loopers-net:
    driver: bridge
`
	err = os.WriteFile("docker-compose.yml", []byte(composeContent), 0600)
	if err != nil {
		ui.Error(fmt.Sprintf("Error writing docker-compose.yml: %v", err))
	} else {
		ui.Success("docker-compose.yml written")
	}

	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("    1. docker-compose up -d")
	fmt.Println("    2. loopers keys create --name my-app --provider openai")
	fmt.Println("    3. loopers budget set <hash> --daily 10.00 --hourly 2.00")
	pressEnterToContinue()
}
