package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
	"github.com/try-loopers/loopers-oss/internal/budget"
	"github.com/try-loopers/loopers-oss/internal/keyring"
	"github.com/try-loopers/loopers-oss/internal/logging"
	"github.com/try-loopers/loopers-oss/internal/pricing"
	"github.com/try-loopers/loopers-oss/internal/server"
)

var (
	keyName             string
	keyProvider         string
	keyAgentName        string
	keyOwner            string
	keyAllowedTools     string
	keyAllowedProviders string
	keyTags             string
	minuteLimit         string
	hourlyLimit         string
	dailyLimit          string
	weeklyLimit         string
	monthlyLimit        string
	execModelMap        string
	execModelOverride   string
	servePresets        []string
)

func getRedisClient() (*budget.Client, error) {
	addr := viper.GetString("redis.addr")
	if addr == "" {
		addr = "localhost:6379"
	}
	password := viper.GetString("redis.password")

	if password == "" && addr != "localhost:6379" && addr != "127.0.0.1:6379" && !strings.HasPrefix(addr, "localhost:") && !strings.HasPrefix(addr, "127.0.0.1:") {
		logging.Logger.Warn().Msg("SECURITY WARNING: Redis password is empty but address is not localhost. Ensure your Redis instance is protected by a firewall or VPC.")
	}

	db := viper.GetInt("redis.db")

	return budget.NewClient(addr, password, db)
}

func fetchActiveKeys(rdb *budget.Client) ([]huh.Option[string], error) {
	ctx := context.Background()
	iter := rdb.GetUnderlyingClient().Scan(ctx, 0, "loopers:key:*", 0).Iterator()
	var options []huh.Option[string]
	for iter.Next(ctx) {
		redisKey := iter.Val()
		h := redisKey[len("loopers:key:"):]
		var meta keyring.KeyMetadata
		if err := rdb.GetUnderlyingClient().HGetAll(ctx, redisKey).Scan(&meta); err == nil && meta.Active == "true" {
			displayHash := h
			if len(displayHash) > 12 {
				displayHash = displayHash[:12] + "..."
			}
			label := fmt.Sprintf("%s · %s", meta.Name, displayHash)
			options = append(options, huh.NewOption(label, h))
		}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	return options, nil
}

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the Loopers AI firewall runtime server",
	Long:  "Run the bare-metal Loopers AI firewall runtime reverse proxy, policy engine, and DLP gate.",
	Run: func(cmd *cobra.Command, args []string) {
		logLevel := viper.GetString("log.level")
		if logLevel == "" {
			logLevel = "info"
		}
		logging.InitLogger(logLevel)

		logging.Logger.Info().Msg("Starting Loopers AI Firewall runtime...")
		fmt.Println("\nIf Loopers saved your budget today, please star our repository: https://github.com/try-loopers/loopers-oss")

		pricingPath := viper.GetString("pricing_path")
		if pricingPath == "" {
			pricingPath = "./pricing.yaml"
		}
		pricingStore, err := pricing.LoadStore(pricingPath)
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to load pricing file")
		}

		redisClient, err := getRedisClient()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to connect to Redis")
		}

		if len(servePresets) > 0 {
			viper.Set("policy.enabled", true)
			viper.Set("policy.presets", servePresets)
		}

		budget.InitConfigCache()
		s := server.NewServer(redisClient, pricingStore)

		// Start background lease workers
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		redisClient.LeaseManager.StartLeaseWorkers(ctx)

		// Start remote pricing fetcher
		pricingURL := viper.GetString("pricing.remote_url")
		if pricingURL != "" {
			refreshHours := viper.GetInt("pricing.refresh_hours")
			if refreshHours <= 0 {
				refreshHours = 24
			}
			pricingStore.StartRemoteFetcher(ctx, pricingURL, refreshHours)
		}

		port := viper.GetString("server.port")
		if port == "" {
			port = "8080"
		}

		adminPort := viper.GetString("server.admin_port")
		if adminPort == "" {
			adminPort = "9090"
		}

		adminHost := viper.GetString("server.admin_host")
		if adminHost == "" {
			adminHost = "127.0.0.1"
		} else if adminHost == "0.0.0.0" {
			logging.Logger.Warn().Msg("SECURITY WARNING: Admin port bound to 0.0.0.0. Unauthenticated metrics are exposed.")
		}

		if viper.GetString("server.server_secret") == "" {
			logging.Logger.Warn().Msg("SECURITY WARNING: server.server_secret is empty. Key storage hash will fall back to unsalted SHA-256. Production deployments MUST set this.")
		}

		readHeaderTimeoutSec := viper.GetInt("server.read_header_timeout_seconds")
		if readHeaderTimeoutSec <= 0 {
			readHeaderTimeoutSec = 10
		}
		readTimeoutSec := viper.GetInt("server.read_timeout_seconds")
		if readTimeoutSec <= 0 {
			readTimeoutSec = 30
		}
		writeTimeoutSec := viper.GetInt("server.write_timeout_seconds")
		if writeTimeoutSec <= 0 {
			writeTimeoutSec = 120
		}
		idleTimeoutSec := viper.GetInt("server.idle_timeout_seconds")
		if idleTimeoutSec <= 0 {
			idleTimeoutSec = 120
		}

		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		srv := &http.Server{
			Addr:              ":" + port,
			Handler:           s.GetHandler(),
			ReadHeaderTimeout: time.Duration(readHeaderTimeoutSec) * time.Second,
			ReadTimeout:       time.Duration(readTimeoutSec) * time.Second,
			WriteTimeout:      time.Duration(writeTimeoutSec) * time.Second,
			IdleTimeout:       time.Duration(idleTimeoutSec) * time.Second,
			TLSConfig:         tlsConfig,
		}

		adminSrv := &http.Server{
			Addr:              adminHost + ":" + adminPort,
			Handler:           s.GetAdminRouter(),
			ReadHeaderTimeout: time.Duration(readHeaderTimeoutSec) * time.Second,
			ReadTimeout:       time.Duration(readTimeoutSec) * time.Second,
			WriteTimeout:      time.Duration(writeTimeoutSec) * time.Second,
			IdleTimeout:       time.Duration(idleTimeoutSec) * time.Second,
			TLSConfig:         tlsConfig,
		}

		certFile := viper.GetString("server.tls_cert_file")
		keyFile := viper.GetString("server.tls_key_file")
		insecureDev := viper.GetBool("server.insecure_dev")

		server.ListenAndServeWithGracefulShutdown(srv, adminSrv, redisClient, certFile, keyFile, insecureDev, s.GetOtelShutdown(), s.Shutdown)
	},
}

var keysCmd = &cobra.Command{
	Use:   "keys",
	Short: "Manage firewall agent and proxy keys",
}

var keysCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new firewall agent key with identity metadata",
	Run: func(cmd *cobra.Command, args []string) {
		if (keyName == "" || keyProvider == "") && ui.IsInteractive() {
			ui.PrintLogo()
			err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Key Name").
						Description("A descriptive name for this key (e.g. 'my-app')").
						Value(&keyName).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("name is required")
							}
							return nil
						}),
					huh.NewSelect[string]().
						Title("Provider").
						Options(
							huh.NewOption("OpenAI", "openai"),
							huh.NewOption("Anthropic", "anthropic"),
							huh.NewOption("Gemini", "gemini"),
							huh.NewOption("Bedrock", "bedrock"),
							huh.NewOption("Azure", "azure"),
							huh.NewOption("Mistral", "mistral"),
							huh.NewOption("Groq", "groq"),
							huh.NewOption("Cohere", "cohere"),
							huh.NewOption("DeepSeek", "deepseek"),
							huh.NewOption("Together", "together"),
							huh.NewOption("Ollama", "ollama"),
							huh.NewOption("Fireworks", "fireworks"),
							huh.NewOption("xAI", "xai"),
							huh.NewOption("vLLM", "vllm"),
							huh.NewOption("OpenRouter", "openrouter"),
						).
						Value(&keyProvider),
				).Title("Identity"),
				huh.NewGroup(
					huh.NewInput().Title("Agent Name (Optional)").Description("Name of the agent associated with this key").Value(&keyAgentName),
					huh.NewInput().Title("Owner (Optional)").Description("Owner/Team responsible for the key").Value(&keyOwner),
				).Title("Metadata"),
				huh.NewGroup(
					huh.NewInput().Title("Allowed Tools (Optional)").Description("Comma-separated list of allowed tools").Value(&keyAllowedTools),
					huh.NewInput().Title("Allowed Providers (Optional)").Description("Comma-separated list of allowed providers").Value(&keyAllowedProviders),
					huh.NewInput().Title("Tags (Optional)").Description("Comma-separated key=value tags for policy").Value(&keyTags),
				).Title("Access Control"),
			).WithTheme(ui.GetHuhTheme()).WithKeyMap(ui.GetHuhKeyMap()).Run()
			if err != nil {
				return
			}
		}

		if keyName == "" || keyProvider == "" {
			logging.Logger.Fatal().Msg("flags --name and --provider are required")
		}
		validProviders := map[string]bool{
			"openai": true, "anthropic": true, "gemini": true, "bedrock": true, "azure": true, "mistral": true,
			"groq": true, "cohere": true, "deepseek": true, "together": true,
			"ollama": true, "fireworks": true, "xai": true, "vllm": true, "openrouter": true,
		}

		type GenericProviderConfig struct {
			Name string `mapstructure:"name"`
		}
		var genericProviders []GenericProviderConfig
		if err := viper.UnmarshalKey("generic_providers", &genericProviders); err == nil {
			for _, gp := range genericProviders {
				if gp.Name != "" {
					validProviders[gp.Name] = true
				}
			}
		}

		if !validProviders[keyProvider] {
			var validList []string
			for k := range validProviders {
				validList = append(validList, k)
			}
			logging.Logger.Fatal().Msgf("provider must be one of: %v", strings.Join(validList, ", "))
		}

		rawKey, err := keyring.GenerateRawKey()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to generate key")
		}
		hash := keyring.HashKey(rawKey)

		redisClient, err := getRedisClient()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to connect to redis")
		}
		defer redisClient.Close()

		ctx := context.Background()
		rdb := redisClient.GetUnderlyingClient()

		key := fmt.Sprintf("loopers:key:%s", hash)
		fields := map[string]interface{}{
			"name":       keyName,
			"provider":   keyProvider,
			"created_at": time.Now().UTC().Format(time.RFC3339),
			"active":     "true",
		}
		if keyAgentName != "" {
			fields["agent_name"] = keyAgentName
		}
		if keyOwner != "" {
			fields["owner"] = keyOwner
		}
		if keyAllowedTools != "" {
			fields["allowed_tools"] = keyAllowedTools
		}
		if keyAllowedProviders != "" {
			fields["allowed_providers"] = keyAllowedProviders
		}
		if keyTags != "" {
			fields["tags"] = keyTags
		}

		err = rdb.HSet(ctx, key, fields).Err()

		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to store key in redis")
		}

		ui.PrintKeyCard(keyName, keyProvider, rawKey, hash)
		fmt.Println("\nIf Loopers saved your budget today, please star our repository: https://github.com/try-loopers/loopers-oss")
	},
}

var keysListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active firewall agent keys",
	Run: func(cmd *cobra.Command, args []string) {
		redisClient, err := getRedisClient()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to connect to redis")
		}
		defer redisClient.Close()

		ctx := context.Background()
		rdb := redisClient.GetUnderlyingClient()

		iter := rdb.Scan(ctx, 0, "loopers:key:*", 0).Iterator()

		headers := []string{"Hash", "Name", "Agent", "Owner", "Provider", "Created At", "Active"}
		var rows [][]string

		for iter.Next(ctx) {
			redisKey := iter.Val()
			hash := redisKey[len("loopers:key:"):]

			displayHash := hash
			if len(displayHash) > 12 {
				displayHash = displayHash[:12] + "..."
			}

			var meta keyring.KeyMetadata
			if err := rdb.HGetAll(ctx, redisKey).Scan(&meta); err == nil {
				t, _ := time.Parse(time.RFC3339, meta.CreatedAt)
				timeStr := t.Format("2006-01-02 15:04 UTC")
				if meta.CreatedAt == "" {
					timeStr = "Unknown"
				}

				activeStr := "✗"
				if meta.Active == "true" {
					activeStr = "✓"
				}

				rows = append(rows, []string{displayHash, meta.Name, meta.AgentName, meta.Owner, meta.Provider, timeStr, activeStr})
			}
		}

		if err := iter.Err(); err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed scanning keys")
		}

		fmt.Printf("  Keys (%d total)\n", len(rows))
		if len(rows) > 0 {
			ui.PrintTable(headers, rows)
		}
	},
}

var keysRevokeCmd = &cobra.Command{
	Use:   "revoke [hash]",
	Short: "Revoke an agent key",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var hash string
		if len(args) > 0 {
			hash = args[0]
		}

		redisClient, err := getRedisClient()
		if err != nil {
			if ui.IsInteractive() && len(args) == 0 {
				ui.Error("Failed to connect to Redis. Ensure it is running.")
				return
			}
			logging.Logger.Fatal().Err(err).Msg("failed to connect to redis")
		}
		defer redisClient.Close()

		if hash == "" && ui.IsInteractive() {
			ui.PrintLogo()
			options, err := fetchActiveKeys(redisClient)
			if err != nil {
				ui.Error(fmt.Sprintf("Failed to fetch keys: %v", err))
				return
			}
			if len(options) == 0 {
				ui.Error("No active keys found to revoke")
				return
			}

			err = huh.NewSelect[string]().
				Title("Select Key to Revoke").
				Options(options...).
				Value(&hash).
				WithTheme(ui.GetHuhTheme()).
				Run()

			if err != nil {
				return
			}
		}

		if hash == "" {
			logging.Logger.Fatal().Msg("hash argument is required")
		}

		ctx := context.Background()
		rdb := redisClient.GetUnderlyingClient()
		key := fmt.Sprintf("loopers:key:%s", hash)

		exists, err := rdb.Exists(ctx, key).Result()
		if err != nil || exists == 0 {
			logging.Logger.Fatal().Msg("Key hash not found")
		}

		err = rdb.HSet(ctx, key, "active", "false").Err()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to revoke key")
		}

		ui.Success("Key revoked successfully.")
	},
}

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "Manage atomic spending limits and budgets",
}

var budgetSetCmd = &cobra.Command{
	Use:   "set [hash]",
	Short: "Set minute, hourly, daily, weekly, and monthly spending caps for an agent key",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var hash string
		if len(args) > 0 {
			hash = args[0]
		}

		redisClient, err := getRedisClient()
		if err != nil {
			if ui.IsInteractive() && len(args) == 0 {
				ui.Error("Failed to connect to Redis. Ensure it is running.")
				return
			}
			logging.Logger.Fatal().Err(err).Msg("failed to connect to redis")
		}
		defer redisClient.Close()

		if hash == "" && ui.IsInteractive() {
			ui.PrintLogo()
			options, err := fetchActiveKeys(redisClient)
			if err != nil {
				ui.Error(fmt.Sprintf("Failed to fetch keys: %v", err))
				return
			}
			if len(options) == 0 {
				ui.Error("No active keys found")
				return
			}

			err = huh.NewSelect[string]().
				Title("Select Key").
				Options(options...).
				Value(&hash).
				WithTheme(ui.GetHuhTheme()).
				Run()

			if err != nil {
				return
			}
		}

		if hash == "" {
			logging.Logger.Fatal().Msg("hash argument is required")
		}

		if (minuteLimit == "" && hourlyLimit == "" && dailyLimit == "" && weeklyLimit == "" && monthlyLimit == "") && ui.IsInteractive() {
			validateFloat := func(s string) error {
				if s == "" {
					return nil
				}
				v, err := strconv.ParseFloat(s, 64)
				if err != nil || v < 0 {
					return fmt.Errorf("must be a valid positive number")
				}
				return nil
			}

			err := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().Title("Minute Limit (USD)").Value(&minuteLimit).Validate(validateFloat),
					huh.NewInput().Title("Hourly Limit (USD)").Value(&hourlyLimit).Validate(validateFloat),
					huh.NewInput().Title("Daily Limit (USD)").Value(&dailyLimit).Validate(validateFloat),
					huh.NewInput().Title("Weekly Limit (USD)").Value(&weeklyLimit).Validate(validateFloat),
					huh.NewInput().Title("Monthly Limit (USD)").Value(&monthlyLimit).Validate(validateFloat),
				),
			).WithTheme(ui.GetHuhTheme()).WithKeyMap(ui.GetHuhKeyMap()).Run()

			if err != nil {
				return
			}
		}

		ctx := context.Background()
		rdb := redisClient.GetUnderlyingClient()

		keyExist, err := rdb.Exists(ctx, fmt.Sprintf("loopers:key:%s", hash)).Result()
		if err != nil || keyExist == 0 {
			logging.Logger.Fatal().Msg("key not found in registry")
		}

		configKey := fmt.Sprintf("loopers:budget:{%s}:config", hash)

		fields := make(map[string]interface{})
		if minuteLimit != "" {
			fields["minute"] = minuteLimit
		}
		if hourlyLimit != "" {
			fields["hourly"] = hourlyLimit
		}
		if dailyLimit != "" {
			fields["daily"] = dailyLimit
		}
		if weeklyLimit != "" {
			fields["weekly"] = weeklyLimit
		}
		if monthlyLimit != "" {
			fields["monthly"] = monthlyLimit
		}

		if len(fields) == 0 {
			logging.Logger.Fatal().Msg("either --minute, --hourly, --daily, --weekly, or --monthly must be provided")
		}

		err = rdb.HSet(ctx, configKey, fields).Err()
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to set budget")
		}

		ui.Success("Budget set successfully.")
		fmt.Println("\nIf Loopers saved your budget today, please star our repository: https://github.com/try-loopers/loopers-oss")
	},
}

var budgetStatusCmd = &cobra.Command{
	Use:   "status [hash]",
	Short: "Get real-time budget status and spend consumption for an agent key",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var hash string
		if len(args) > 0 {
			hash = args[0]
		}

		redisClient, err := getRedisClient()
		if err != nil {
			if ui.IsInteractive() && len(args) == 0 {
				ui.Error("Failed to connect to Redis. Ensure it is running.")
				return
			}
			logging.Logger.Fatal().Err(err).Msg("failed to connect to redis")
		}
		defer redisClient.Close()

		if hash == "" && ui.IsInteractive() {
			ui.PrintLogo()
			options, err := fetchActiveKeys(redisClient)
			if err != nil {
				ui.Error(fmt.Sprintf("Failed to fetch keys: %v", err))
				return
			}
			if len(options) == 0 {
				ui.Error("No active keys found")
				return
			}

			err = huh.NewSelect[string]().
				Title("Select Key to View").
				Options(options...).
				Value(&hash).
				WithTheme(ui.GetHuhTheme()).
				Run()

			if err != nil {
				return
			}
		}

		if hash == "" {
			logging.Logger.Fatal().Msg("hash argument is required")
		}

		ctx := context.Background()
		rdb := redisClient.GetUnderlyingClient()

		keyExist, err := rdb.Exists(ctx, fmt.Sprintf("loopers:key:%s", hash)).Result()
		if err != nil || keyExist == 0 {
			logging.Logger.Fatal().Msg("key not found in registry")
		}

		status, err := redisClient.GetBudgetStatus(ctx, hash)
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to get budget status")
		}

		displayHash := hash
		if len(displayHash) > 12 {
			displayHash = displayHash[:12] + "..."
		}

		keyConfig, _ := rdb.HGetAll(ctx, fmt.Sprintf("loopers:key:%s", hash)).Result()
		name := keyConfig["name"]
		provider := keyConfig["provider"]

		fmt.Printf("  Budget Status  ›  %s  (%s / %s)\n\n", displayHash, name, provider)

		windowsOrdered := []string{"minute", "hourly", "daily", "weekly", "monthly"}
		for _, w := range windowsOrdered {
			s := status[w]
			ui.PrintBudgetBar(w, s.CurrentSpend, s.Limit)
			if s.SavedUSD > 0 {
				fmt.Printf("    ↳ Saved by Loopers (%s): $%.4f\n", w, s.SavedUSD)
			}
		}
	},
}

var (
	finopsWindow string
	finopsJSON   bool
)

var finopsCmd = &cobra.Command{
	Use:     "finops [hash]",
	Aliases: []string{"savings"},
	Short:   "Get real-time FinOps cost reduction and token acceleration scorecard",
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var hash string
		if len(args) > 0 {
			hash = args[0]
		}

		redisClient, err := getRedisClient()
		if err != nil {
			if ui.IsInteractive() && len(args) == 0 {
				ui.Error("Failed to connect to Redis. Ensure it is running.")
				return
			}
			logging.Logger.Fatal().Err(err).Msg("failed to connect to redis")
		}
		defer redisClient.Close()

		if hash == "" && ui.IsInteractive() {
			ui.PrintLogo()
			options, err := fetchActiveKeys(redisClient)
			if err != nil {
				ui.Error(fmt.Sprintf("Failed to fetch keys: %v", err))
				return
			}
			if len(options) == 0 {
				ui.Error("No active keys found")
				return
			}

			err = huh.NewSelect[string]().
				Title("Select Key to View FinOps Scorecard").
				Options(options...).
				Value(&hash).
				WithTheme(ui.GetHuhTheme()).
				Run()

			if err != nil {
				return
			}
		}

		if hash == "" {
			logging.Logger.Fatal().Msg("hash argument is required")
		}

		ctx := context.Background()
		rdb := redisClient.GetUnderlyingClient()

		keyExist, err := rdb.Exists(ctx, fmt.Sprintf("loopers:key:%s", hash)).Result()
		if err != nil || keyExist == 0 {
			logging.Logger.Fatal().Msg("key not found in registry")
		}

		status, err := redisClient.GetFinOpsStatus(ctx, hash)
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("failed to get finops status")
		}

		win := strings.ToLower(finopsWindow)
		if win == "" {
			win = "daily"
		}
		rec, ok := status[win]
		if !ok {
			rec = status["daily"]
			win = "daily"
		}

		if finopsJSON {
			out, _ := json.MarshalIndent(map[string]interface{}{
				"key_hash": hash,
				"window":   win,
				"finops":   rec,
			}, "", "  ")
			fmt.Println(string(out))
			return
		}

		displayHash := hash
		if len(displayHash) > 12 {
			displayHash = displayHash[:12] + "..."
		}
		keyConfig, _ := rdb.HGetAll(ctx, fmt.Sprintf("loopers:key:%s", hash)).Result()
		name := keyConfig["name"]
		provider := keyConfig["provider"]

		title := fmt.Sprintf("FinOps Scorecard  ›  %s  (%s / %s)  [%s]", displayHash, name, provider, strings.ToUpper(win))
		ui.PrintFinOpsScorecard(title, rec.ActualSpendUSD, rec.CounterfactualUSD, rec.NetSavedUSD, rec.SavingsPercentage, rec.GhostTokensSaved, rec.GhostPayloadBytes, rec.FastTierTurns, rec.FastTierSavedUSD, rec.ToolMemoHits, rec.ToolMemoSavedUSD, rec.SchemaTokensSaved)
	},
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Interactively initialize Loopers AI firewall configuration",
	Run: func(cmd *cobra.Command, args []string) {
		screenInit()
	},
}

var execCmd = &cobra.Command{
	Use:   "exec -- <command...>",
	Short: "Execute an agent command with Loopers firewall environment variables and transparent proxying injected",
	Long:  "Wrap and monitor autonomous AI agents (Aider, OpenHands, Claude, Pi, Codex, OpenCode, DeepSeek Harness) with transparent firewall proxying, budget caps, and DLP enforcement.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		proxyKey := os.Getenv("LOOPERS_PROXY_KEY")
		if proxyKey == "" {
			logging.Logger.Fatal().Msg("LOOPERS_PROXY_KEY environment variable is required. Unix: export LOOPERS_PROXY_KEY=lp-xxx | Windows (PS): $env:LOOPERS_PROXY_KEY=\"lp-xxx\"")
		}

		proxyURL := os.Getenv("LOOPERS_PROXY_URL")
		if proxyURL == "" {
			proxyURL = "http://localhost:8080"
		}
		proxyURL = strings.TrimSuffix(proxyURL, "/")

		provider := os.Getenv("LOOPERS_PROVIDER")
		if provider == "" {
			// Auto-detect provider from the CLI executable name.
			// Note: antigravity is intentionally excluded as it does not support BYOK.
			executable := strings.ToLower(filepath.Base(args[0]))
			executable = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(executable, ".exe"), ".cmd"), ".bat")
			switch {
			case executable == "claude":
				provider = "anthropic"
			case executable == "gemini":
				provider = "gemini"
			case strings.Contains(executable, "openrouter"):
				provider = "openrouter"
			case executable == "dsh" || executable == "deepseek-harness" || executable == "deepseek":
				provider = "deepseek"
			case executable == "aider":
				provider = "openai"
			case executable == "openhands":
				provider = "openai"
			case executable == "pi":
				provider = "openai"
			default:
				// opencode, codex, cursor, and most OpenAI-compatible CLIs default to openai.
				provider = "openai"
			}
			logging.Logger.Info().Msgf("LOOPERS_PROVIDER not set. Auto-detected provider '%s' from executable '%s'. Set LOOPERS_PROVIDER to override.", provider, executable)
		}

		targetURLStr := fmt.Sprintf("%s/%s", proxyURL, provider)
		targetURL, err := url.Parse(targetURLStr)
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Invalid proxy URL")
		}

		// Start a local transparent reverse proxy
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			logging.Logger.Fatal().Err(err).Msg("Failed to start local proxy listener")
		}
		localProxyPort := listener.Addr().(*net.TCPAddr).Port
		localProxyURL := fmt.Sprintf("http://127.0.0.1:%d/v1", localProxyPort)

		sessionID := os.Getenv("LOOPERS_SESSION_ID")
		if sessionID == "" {
			sessionID = fmt.Sprintf("sess_%d_%s", time.Now().Unix(), uuid.New().String()[:8])
		}

		type execTracker struct {
			mu            sync.Mutex
			requests      int
			actualSpend   float64
			savedUSD      float64
			ghostCount    int
			fastTierTurns int
			memoHits      int
		}
		tracker := &execTracker{}

		rp := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				req.URL.Scheme = targetURL.Scheme
				req.URL.Host = targetURL.Host

				// Ensure path is properly joined without stripping /v1,
				// as providers like OpenRouter need /v1 in their upstream path.
				req.URL.Path = strings.TrimSuffix(targetURL.Path, "/") + req.URL.Path
				req.Host = targetURL.Host

				authHeader := req.Header.Get("Authorization")
				if authHeader != "" {
					// Move agent's API key to the provider key header
					req.Header.Set("X-Loopers-Provider-Key", strings.TrimPrefix(authHeader, "Bearer "))
				}
				req.Header.Set("Authorization", "Bearer "+proxyKey)

				if execModelMap != "" {
					req.Header.Set("X-Loopers-Model-Map", execModelMap)
				}
				if execModelOverride != "" {
					req.Header.Set("X-Loopers-Model-Override", execModelOverride)
				}
				req.Header.Set("X-Loopers-Session-ID", sessionID)
				if os.Getenv("LOOPERS_SKIP_HYDRATION") == "true" {
					req.Header.Set("X-Loopers-Skip-Hydration", "true")
				}
				if os.Getenv("LOOPERS_FORCE_FRONTIER") == "true" {
					req.Header.Set("X-Loopers-Force-Frontier", "true")
				}
			},
			ModifyResponse: func(resp *http.Response) error {
				tracker.mu.Lock()
				defer tracker.mu.Unlock()

				tracker.requests++

				if costHeader := resp.Header.Get("X-Loopers-Request-Cost"); costHeader != "" {
					if cost, err := strconv.ParseFloat(costHeader, 64); err == nil {
						tracker.actualSpend += cost
					}
				}
				if savedHeader := resp.Header.Get("X-Loopers-Saved-USD"); savedHeader != "" {
					if saved, err := strconv.ParseFloat(savedHeader, 64); err == nil {
						tracker.savedUSD += saved
					}
				}
				if resp.Header.Get("X-Loopers-Ghost-Token") != "" {
					tracker.ghostCount++
				}
				if strings.EqualFold(resp.Header.Get("X-Loopers-FinOps-Route"), "fast-tier") {
					tracker.fastTierTurns++
				}
				if strings.EqualFold(resp.Header.Get("X-Loopers-MCP-Memoized"), "true") {
					tracker.memoHits++
				}
				return nil
			},
		}

		go func() {
			if err := http.Serve(listener, rp); err != nil && err != http.ErrServerClosed {
				logging.Logger.Error().Err(err).Msg("Local proxy error")
			}
		}()

		var envAPIKey string
		switch provider {
		case "anthropic":
			envAPIKey = "ANTHROPIC_API_KEY"
		case "gemini":
			envAPIKey = "GEMINI_API_KEY"
		case "openrouter":
			envAPIKey = "OPENROUTER_API_KEY"
		case "deepseek":
			envAPIKey = "DEEPSEEK_API_KEY"
		default:
			envAPIKey = "OPENAI_API_KEY"
		}

		// Prepare command
		c := exec.Command(args[0], args[1:]...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr

		// Copy existing env and append
		c.Env = os.Environ()

		// Set base URLs to point to our local transparent proxy
		c.Env = append(c.Env, fmt.Sprintf("OPENAI_BASE_URL=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("OPENAI_API_BASE=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("ANTHROPIC_BASE_URL=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("GEMINI_BASE_URL=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("DEEPSEEK_BASE_URL=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("DEEPSEEK_API_BASE=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("OPENROUTER_BASE_URL=%s", localProxyURL))
		c.Env = append(c.Env, fmt.Sprintf("LOOPERS_SESSION_ID=%s", sessionID))

		// Inject synthetic API keys if real ones are absent.
		syntheticKey := "loopers-managed"
		injectIfMissing := func(envVar string) {
			for _, e := range c.Env {
				if strings.HasPrefix(e, envVar+"=") {
					return // real key present; do not overwrite
				}
			}
			c.Env = append(c.Env, fmt.Sprintf("%s=%s", envVar, syntheticKey))
		}
		injectIfMissing("OPENAI_API_KEY")
		injectIfMissing("ANTHROPIC_API_KEY")
		injectIfMissing("GEMINI_API_KEY")
		injectIfMissing("DEEPSEEK_API_KEY")

		// Inject OpenHands-specific LLM override variables.
		if strings.ToLower(args[0]) == "openhands" {
			c.Env = append(c.Env, fmt.Sprintf("LLM_BASE_URL=%s", localProxyURL))
			realKey := os.Getenv(envAPIKey)
			if realKey != "" {
				c.Env = append(c.Env, fmt.Sprintf("LLM_API_KEY=%s", realKey))
			} else {
				injectIfMissing("LLM_API_KEY")
			}
		}

		// Pi does not support env-var overrides; inject provider into models.json.
		if strings.ToLower(args[0]) == "pi" {
			cleanup, err := injectPiProvider(localProxyURL)
			if err != nil {
				logging.Logger.Warn().Err(err).Msg("Failed to inject Loopers provider into Pi models.json; Pi will not route through proxy")
			} else {
				defer cleanup()
			}
		}

		managedHarnesses := map[string]bool{
			"aider": true, "openhands": true, "pi": true,
			"dsh": true, "deepseek-harness": true, "deepseek": true,
		}
		execName := strings.ToLower(filepath.Base(args[0]))
		execName = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(execName, ".exe"), ".cmd"), ".bat")
		if !managedHarnesses[execName] {
			foundRealKey := false
			for _, e := range c.Env {
				if strings.HasPrefix(e, envAPIKey+"=") && !strings.HasPrefix(e, envAPIKey+"="+syntheticKey) {
					foundRealKey = true
					break
				}
			}
			if !foundRealKey {
				logging.Logger.Warn().Msgf("Warning: %s is not set in your environment. The underlying agent command might fail if it requires an upstream key.", envAPIKey)
			}
		}

		runErr := c.Run()

		if tracker.requests > 0 {
			actual := tracker.actualSpend
			saved := tracker.savedUSD
			counterfactual := actual + saved
			ghostToks := int64(tracker.ghostCount * 3600)
			fastTurns := int64(tracker.fastTierTurns)
			memoCount := int64(tracker.memoHits)

			keyHash := keyring.HashKey(proxyKey)
			if rClient, rErr := getRedisClient(); rErr == nil && rClient != nil {
				defer rClient.Close()
				if sessRec, sErr := rClient.GetSessionFinOps(context.Background(), keyHash, sessionID); sErr == nil && sessRec != nil {
					if sessRec.ActualSpendUSD > 0 || sessRec.NetSavedUSD > 0 || sessRec.GhostTokensSaved > 0 {
						actual = sessRec.ActualSpendUSD
						saved = sessRec.NetSavedUSD
						counterfactual = sessRec.CounterfactualUSD
						ghostToks = sessRec.GhostTokensSaved
						fastTurns = sessRec.FastTierTurns
						memoCount = sessRec.ToolMemoHits
					}
				}
			}

			pct := 0.0
			if counterfactual > 0 {
				pct = (saved / counterfactual) * 100.0
			}
			ui.PrintFinOpsExitSummary(tracker.requests, actual, counterfactual, saved, pct, ghostToks, fastTurns, memoCount)
		}

		if runErr != nil {
			if exitError, ok := runErr.(*exec.ExitError); ok {
				os.Exit(exitError.ExitCode())
			}
			logging.Logger.Fatal().Err(runErr).Msg("failed to execute command")
		}
	},
}

func init() {
	// Root flags
	serveCmd.Flags().StringSliceVar(&servePresets, "presets", nil, "Comma-separated list of policy presets to enable (safety|safety_drift|pci|mcp_sandbox|zero_trust|owasp_llm_top10|nist_ai_rmf|eu_ai_act)")
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(initCmd)

	// Exec command
	execCmd.Flags().StringVar(&execModelMap, "model-map", "", "Comma-separated list of model aliases (e.g. gpt-4o=google/gemini-2.5-pro,claude=anthropic/claude-3)")
	execCmd.Flags().StringVar(&execModelOverride, "model-override", "", "Force a specific model to be used for all requests")
	rootCmd.AddCommand(execCmd)

	// Keys commands
	keysCreateCmd.Flags().StringVar(&keyName, "name", "", "Name of the proxy key (required)")
	keysCreateCmd.Flags().StringVar(&keyProvider, "provider", "", "Provider for the key (openai|anthropic|gemini|bedrock|azure|mistral|groq|cohere|deepseek|together|ollama|fireworks|xai|vllm|openrouter) (required)")
	keysCreateCmd.Flags().StringVar(&keyAgentName, "agent-name", "", "Name of the agent associated with this key (optional)")
	keysCreateCmd.Flags().StringVar(&keyOwner, "owner", "", "Owner of the key (optional)")
	keysCreateCmd.Flags().StringVar(&keyAllowedTools, "allowed-tools", "", "Comma-separated list of allowed tools (optional)")
	keysCreateCmd.Flags().StringVar(&keyAllowedProviders, "allowed-providers", "", "Comma-separated list of allowed providers (optional)")
	keysCreateCmd.Flags().StringVar(&keyTags, "tags", "", "Comma-separated key=value tags for policy evaluation (optional)")
	keysCmd.AddCommand(keysCreateCmd)
	keysCmd.AddCommand(keysListCmd)
	keysCmd.AddCommand(keysRevokeCmd)
	rootCmd.AddCommand(keysCmd)

	// Budget commands
	budgetSetCmd.Flags().StringVar(&minuteLimit, "minute", "", "Minute budget in USD")
	budgetSetCmd.Flags().StringVar(&hourlyLimit, "hourly", "", "Hourly budget in USD")
	budgetSetCmd.Flags().StringVar(&dailyLimit, "daily", "", "Daily budget in USD")
	budgetSetCmd.Flags().StringVar(&weeklyLimit, "weekly", "", "Weekly budget in USD")
	budgetSetCmd.Flags().StringVar(&monthlyLimit, "monthly", "", "Monthly budget in USD")
	budgetCmd.AddCommand(budgetSetCmd)
	budgetCmd.AddCommand(budgetStatusCmd)
	rootCmd.AddCommand(budgetCmd)

	// FinOps command
	finopsCmd.Flags().StringVar(&finopsWindow, "window", "daily", "Time window to inspect (minute|hourly|daily|weekly|monthly|total)")
	finopsCmd.Flags().BoolVar(&finopsJSON, "json", false, "Output FinOps scorecard in JSON format")
	rootCmd.AddCommand(finopsCmd)
}
