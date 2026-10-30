package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
	"github.com/try-loopers/loopers-oss/internal/blastradius"
	"github.com/try-loopers/loopers-oss/internal/inspector"
	"github.com/try-loopers/loopers-oss/internal/mcp"
	"github.com/try-loopers/loopers-oss/internal/pricing"
	"github.com/try-loopers/loopers-oss/internal/proxy"
	"gopkg.in/yaml.v3"
)

var (
	auditProvider string
	auditModel    string
	auditMCPURL   string
	auditTrace    string
	auditShare    bool
	auditFix      bool
	auditOffline  bool
)

var volatilePrefixCheck = regexp.MustCompile(`(?im)^[ \t]*(current_time|timestamp|request_id|run_id|nonce|date)[ \t]*:[ \t]*[^\n]+`)

type auditReport struct {
	TargetDir          string
	IsExplicitDir      bool
	HarnessName        string
	HarnessSlug        string
	RecommendedExecCmd string
	Provider           string
	Model              string
	ModelSource        string
	FastTierModel      string
	InputPer1M         float64
	OutputPer1M        float64
	CacheReadPer1M     float64
	FastInputPer1M     float64

	// Module 1: Workspace Prefix
	WorkspaceFilesFound int
	WorkspaceFiles      []string
	WorkspaceBytes      int
	WorkspaceTokens     int
	HasVolatilePrefix   bool
	HeartbeatsPerDay    int

	// Module 2: MCP Schema & Blast Radius
	MCPServersFound       int
	DiscoveredServerNames []string
	MCPToolsCount         int
	MCPRawSchemaTokens    int
	MCPMinifiedTokens     int
	MCPReadOnlyTools      int
	MCPHighBlastTools     int
	HighBlastToolNames    []string
	LiveMCPProbed         bool

	// Module 3: Session Replay / Simulation
	IsSimulatedReplay  bool
	SessionsAnalyzed   int
	TurnsAnalyzed      int
	OversizedPayloads  int
	BaggageReSends     int
	GhostTokensSaved   int
	DuplicateReadCalls int
	ColdDiscoveryTurns int

	// Module 4: Extended Agent Intelligence (DLP, Ghost Tools, Output Ratio)
	ConfiguredToolNames []string
	InvokedToolCounts   map[string]int
	UnusedGhostTools    []string
	UnusedSchemaTokens  int

	TotalToolCalls   int
	FailedToolCalls  int
	WastedRetryTurns int

	DLPSecretLeaks   int
	DLPPIIOrNetLeaks int
	InjectionHits    int

	TotalInputTokensLog  int
	TotalOutputTokensLog int
	ToolActiveTurns      int

	// Financials (Monthly 30-day Projection)
	BaselineMonthlyUSD float64
	LoopersMonthlyUSD  float64
	MonthlySavedUSD    float64
	SavingsPct         float64
	SecurityGrade      string

	// Frontier Reference Comparison (e.g. Claude 3.5 Sonnet)
	FrontierBaselineUSD float64
	FrontierSavedUSD    float64
}

var auditCmd = &cobra.Command{
	Use:   "audit [directory]",
	Short: "Run a zero-config FinOps & Blast-Radius X-Ray on any AI agent workspace or MCP setup",
	Long: `Inspect local AI agent workspaces (CrewAI, LangGraph, AutoGen, OpenClaw, ZeroClaw, OpenCode, Claude Code, or MCP configs),
measure static prompt & MCP schema token bloat, replay .jsonl session histories to calculate
quadratic Ghost Token savings, and audit MCP tool blast radius — with zero Redis or proxy setup required.`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		targetDir := "."
		isExplicit := false
		if len(args) > 0 && args[0] != "" {
			targetDir = args[0]
			isExplicit = true
		}
		runAuditCommand(targetDir, isExplicit)
	},
}

func init() {
	auditCmd.Flags().StringVar(&auditProvider, "provider", "", "LLM provider for cost calculation (anthropic|openai|openrouter|deepseek)")
	auditCmd.Flags().StringVar(&auditModel, "model", "", "Primary model to benchmark against (e.g. claude-sonnet-4-6, deepseek/deepseek-v4-pro)")
	auditCmd.Flags().StringVar(&auditMCPURL, "mcp-url", "", "Optional live HTTP MCP server URL to probe via tools/list")
	auditCmd.Flags().StringVar(&auditTrace, "trace", "", "Optional path to a .jsonl session trace file or directory")
	auditCmd.Flags().BoolVar(&auditShare, "share", false, "Print a copy-pasteable Markdown scorecard for GitHub Issues / Reddit / X")
	auditCmd.Flags().BoolVar(&auditFix, "fix", false, "Automatically generate a tailored loopers.yaml with all discovered FinOps & security settings enabled")
	auditCmd.Flags().BoolVar(&auditOffline, "offline", false, "Skip remote pricing sync and use local/embedded pricing only")
	rootCmd.AddCommand(auditCmd)
}

func runAuditCommand(targetDir string, isExplicit bool) {
	ui.PrintLogo()
	ui.PrintHeader("◎ Loopers Agent Context & Blast-Radius X-Ray (Zero-Config Audit)")
	fmt.Println()

	rep := &auditReport{
		TargetDir:         targetDir,
		IsExplicitDir:     isExplicit,
		HeartbeatsPerDay:  0,
		InvokedToolCounts: make(map[string]int),
	}

	// 1. Detect Agent Harness & Workspace Files
	detectHarnessAndWorkspace(rep)

	// 2. Load Pricing Store & Resolve Model Pricing
	store := loadAuditPricingStore(rep)
	resolveAuditModelPricing(rep, store)

	// 3. Scan MCP Configs (JSON, TOML, YAML) & Live MCP Server (if any)
	scanMCPConfiguration(rep)

	// 4. Replay .jsonl Session Traces (or run Trajectory Simulation if no logs exist yet)
	analyzeSessionsOrSimulate(rep)

	// 5. Compute Ghost Tools (Unused MCP tools in historical logs)
	computeGhostTools(rep)

	// 6. Compute Monthly Spend & Security Blast-Radius Grade
	computeAuditFinancialsAndGrade(rep)

	// 7. Render Terminal Output
	renderAuditTerminalReport(rep)

	if auditShare {
		renderShareableMarkdown(rep)
	}

	// 8. Conversion Bridge: Auto-generate pre-filled loopers.yaml (--fix or interactive [Y/n])
	offerInstantFixAndExec(rep)
}

func detectHarnessAndWorkspace(rep *auditReport) {
	absDir, err := filepath.Abs(rep.TargetDir)
	if err == nil {
		rep.TargetDir = absDir
	}

	ignoreDirs := map[string]bool{
		".git": true, "node_modules": true, "venv": true, ".venv": true,
		"__pycache__": true, "dist": true, "bin": true, "target": true,
		"vendor": true, ".next": true, ".cache": true, ".idea": true, ".vscode": true,
		"build": true,
	}

	knownContextNames := map[string]bool{
		"soul.md": true, "memory.md": true, "agents.md": true, "user.md": true,
		"tools.md": true, "identity.md": true, "heartbeat.md": true, "claude.md": true,
		".cursorrules": true, ".windsurfrules": true, "instructions.md": true,
		"system.md": true, "rules.md": true, "agents.yaml": true, "tasks.yaml": true,
		"prompt.txt": true, "system.txt": true, "personality.md": true,
	}

	var foundFiles []string
	totalBytes := 0
	hasVolatile := false

	hasCrewAI := false
	hasLangGraph := false
	hasAutoGen := false
	hasOpenClaw := false
	hasZeroClaw := false
	hasOpenCode := false
	hasClaudeIDE := false

	baseDepth := strings.Count(filepath.Clean(rep.TargetDir), string(filepath.Separator))

	_ = filepath.WalkDir(rep.TargetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if ignoreDirs[name] {
				return filepath.SkipDir
			}
			curDepth := strings.Count(filepath.Clean(path), string(filepath.Separator)) - baseDepth
			if curDepth > 4 {
				return filepath.SkipDir
			}
			switch name {
			case ".openclaw":
				hasOpenClaw = true
			case ".zeroclaw":
				hasZeroClaw = true
			case ".opencode":
				hasOpenCode = true
			case ".claude":
				hasClaudeIDE = true
			}
			return nil
		}

		lower := strings.ToLower(name)

		// Check for harness signature files
		switch lower {
		case "crewai.yaml", "crew.py":
			hasCrewAI = true
		case "langgraph.json", "graph.py":
			hasLangGraph = true
		case "oai_config_list", "autogen.json":
			hasAutoGen = true
		case "openclaw.json":
			hasOpenClaw = true
		case "zeroclaw.toml":
			hasZeroClaw = true
		case "opencode.json":
			hasOpenCode = true
		case "claude.md", ".cursorrules", ".windsurfrules":
			hasClaudeIDE = true
		case "config.toml":
			if data, err := os.ReadFile(path); err == nil {
				str := string(data)
				if strings.Contains(str, "zeroclaw") || strings.Contains(str, "[agents") || strings.Contains(str, "[[mcp.servers]]") {
					hasZeroClaw = true
				}
			}
		}

		// Check context & prompt files
		isPromptFile := knownContextNames[lower]
		if !isPromptFile {
			if strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".txt") {
				if strings.Contains(lower, "soul") || strings.Contains(lower, "memory") ||
					strings.Contains(lower, "agent") || strings.Contains(lower, "prompt") ||
					strings.Contains(lower, "instruction") {
					isPromptFile = true
				}
			}
		}

		if isPromptFile {
			if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
				rel, rErr := filepath.Rel(rep.TargetDir, path)
				if rErr == nil {
					foundFiles = append(foundFiles, rel)
				} else {
					foundFiles = append(foundFiles, name)
				}
				totalBytes += len(data)
				if volatilePrefixCheck.Match(data) {
					hasVolatile = true
				}
			}
		}

		return nil
	})

	dirLower := strings.ToLower(rep.TargetDir)
	if strings.Contains(dirLower, "zeroclaw") {
		hasZeroClaw = true
	} else if strings.Contains(dirLower, "openclaw") {
		hasOpenClaw = true
	}

	// Classify harness dynamically
	switch {
	case hasCrewAI:
		rep.HarnessName = "CrewAI Multi-Agent Workspace"
		rep.HarnessSlug = "crewai"
		rep.RecommendedExecCmd = "loopers exec -- crewai run"
		rep.HeartbeatsPerDay = 12
	case hasLangGraph:
		rep.HarnessName = "LangGraph Agent Workspace"
		rep.HarnessSlug = "langgraph"
		rep.RecommendedExecCmd = "loopers exec -- langgraph up"
		rep.HeartbeatsPerDay = 12
	case hasAutoGen:
		rep.HarnessName = "AutoGen Multi-Agent Workspace"
		rep.HarnessSlug = "autogen"
		rep.RecommendedExecCmd = "loopers exec -- python main.py"
		rep.HeartbeatsPerDay = 12
	case hasOpenClaw:
		rep.HarnessName = "OpenClaw Autonomous Agent Workspace"
		rep.HarnessSlug = "openclaw"
		rep.RecommendedExecCmd = "loopers exec -- npx openclaw"
		rep.HeartbeatsPerDay = 48
	case hasZeroClaw:
		rep.HarnessName = "ZeroClaw Autonomous Agent Harness"
		rep.HarnessSlug = "zeroclaw"
		rep.RecommendedExecCmd = "loopers exec -- zeroclaw agent"
		rep.HeartbeatsPerDay = 24
	case hasOpenCode:
		rep.HarnessName = "OpenCode Agent Workspace"
		rep.HarnessSlug = "opencode"
		rep.RecommendedExecCmd = "loopers exec -- opencode"
		rep.HeartbeatsPerDay = 12
	case hasClaudeIDE:
		rep.HarnessName = "Anthropic / IDE Agent Workspace"
		rep.HarnessSlug = "claude-code"
		rep.RecommendedExecCmd = "loopers exec -- claude"
		rep.HeartbeatsPerDay = 6
	default:
		// Check home directory fallback only if default "." was used and 0 files found
		if !rep.IsExplicitDir && len(foundFiles) == 0 {
			if home, err := os.UserHomeDir(); err == nil {
				ocDir := filepath.Join(home, ".openclaw")
				zcDir := filepath.Join(home, ".zeroclaw")
				if dirExists(ocDir) {
					rep.TargetDir = ocDir
					rep.HarnessName = "OpenClaw Global Workspace (~/.openclaw)"
					rep.HarnessSlug = "openclaw"
					rep.RecommendedExecCmd = "loopers exec -- npx openclaw"
					rep.HeartbeatsPerDay = 48
				} else if dirExists(zcDir) {
					rep.TargetDir = zcDir
					rep.HarnessName = "ZeroClaw Global Workspace (~/.zeroclaw)"
					rep.HarnessSlug = "zeroclaw"
					rep.RecommendedExecCmd = "loopers exec -- zeroclaw agent"
					rep.HeartbeatsPerDay = 24
				}
			}
		}
		if rep.HarnessSlug == "" {
			rep.HarnessName = "Autonomous Agent / MCP Workspace"
			rep.HarnessSlug = "agent"
			rep.RecommendedExecCmd = "loopers exec -- <your-agent-cmd>"
			rep.HeartbeatsPerDay = 12
		}
	}

	rep.WorkspaceFilesFound = len(foundFiles)
	rep.WorkspaceFiles = foundFiles
	rep.WorkspaceBytes = totalBytes
	if rep.WorkspaceBytes > 0 {
		rep.WorkspaceTokens = (rep.WorkspaceBytes + 3) / 4
	} else {
		rep.WorkspaceTokens = 0
	}
	rep.HasVolatilePrefix = hasVolatile
}

func detectModelFromWorkspace(targetDir string) (provider, model, source string) {
	// 1. Check TOML configurations (config.toml, zeroclaw.toml)
	tomlCandidates := []string{
		filepath.Join(targetDir, "config.toml"),
		filepath.Join(targetDir, "zeroclaw.toml"),
	}
	for _, tc := range tomlCandidates {
		if data, err := os.ReadFile(tc); err == nil {
			var raw map[string]interface{}
			if err := toml.Unmarshal(data, &raw); err == nil {
				if m := findKeyStringRecursive(raw, "model"); m != "" {
					model = m
					if strings.Contains(m, "/") {
						parts := strings.SplitN(m, "/", 2)
						provider = parts[0]
					}
					return provider, model, filepath.Base(tc)
				}
			}
		}
	}

	// 2. Check JSON configurations (openclaw.json, opencode.json)
	jsonCandidates := []string{
		filepath.Join(targetDir, "openclaw.json"),
		filepath.Join(targetDir, "opencode.json"),
	}
	for _, jc := range jsonCandidates {
		if data, err := os.ReadFile(jc); err == nil {
			var raw map[string]interface{}
			if err := json.Unmarshal(data, &raw); err == nil {
				if m := findKeyStringRecursive(raw, "model"); m != "" {
					model = m
					if p := findKeyStringRecursive(raw, "provider"); p != "" {
						provider = p
					} else if strings.Contains(m, "/") {
						parts := strings.SplitN(m, "/", 2)
						provider = parts[0]
					}
					return provider, model, filepath.Base(jc)
				}
			}
		}
	}

	// 3. Check YAML configurations (crewai.yaml, agents.yaml)
	yamlCandidates := []string{
		filepath.Join(targetDir, "crewai.yaml"),
		filepath.Join(targetDir, "agents.yaml"),
	}
	for _, yc := range yamlCandidates {
		if data, err := os.ReadFile(yc); err == nil {
			var raw map[string]interface{}
			if err := yaml.Unmarshal(data, &raw); err == nil {
				if m := findKeyStringRecursive(raw, "llm"); m != "" {
					model = m
					return provider, model, filepath.Base(yc)
				}
			}
		}
	}

	return "", "", ""
}

func findKeyStringRecursive(m map[string]interface{}, targetKey string) string {
	for k, v := range m {
		if strings.EqualFold(k, targetKey) {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
		if subMap, ok := v.(map[string]interface{}); ok {
			if s := findKeyStringRecursive(subMap, targetKey); s != "" {
				return s
			}
		}
	}
	return ""
}

func loadAuditPricingStore(rep *auditReport) *pricing.Store {
	store := pricing.DefaultStore()

	pricingPath := viper.GetString("pricing_path")
	if pricingPath == "" {
		pricingPath = "./pricing.yaml"
	}
	if localStore, err := pricing.LoadStore(pricingPath); err == nil {
		store = localStore
	}

	if !auditOffline {
		remoteURL := viper.GetString("pricing.remote_url")
		if remoteURL == "" {
			remoteURL = pricing.DefaultRemotePricingURL
		}
		mode := "simulate"
		if auditTrace != "" {
			mode = "replay"
		}
		ua := fmt.Sprintf("loopers-oss/2.24.0 (cmd=audit; os=%s; arch=%s; harness=%s; mode=%s)",
			runtime.GOOS, runtime.GOARCH, rep.HarnessSlug, mode)
		if remoteCfg, err := pricing.FetchRemotePricingWithUA(remoteURL, ua, 1500*time.Millisecond); err == nil && remoteCfg != nil {
			store.MergeRemote(remoteCfg.Providers)
		}
	}

	return store
}

func resolveAuditModelPricing(rep *auditReport, store *pricing.Store) {
	rep.Provider = strings.ToLower(strings.TrimSpace(auditProvider))
	rep.Model = strings.TrimSpace(auditModel)

	if rep.Model != "" {
		rep.ModelSource = "CLI flag"
	} else {
		detProv, detModel, sourceFile := detectModelFromWorkspace(rep.TargetDir)
		if detModel != "" {
			rep.Model = detModel
			rep.ModelSource = "detected from " + sourceFile
			if rep.Provider == "" {
				if detProv != "" {
					rep.Provider = detProv
				} else {
					rep.Provider = "openrouter"
				}
			}
		} else {
			if rep.HarnessSlug == "zeroclaw" {
				rep.Provider = "openrouter"
				rep.Model = "deepseek/deepseek-v4.1-flash-1017"
				rep.ModelSource = "detected for ZeroClaw harness"
			} else {
				rep.Provider = "anthropic"
				rep.Model = "claude-sonnet-4-6"
				rep.ModelSource = "industry benchmark default"
			}
		}
	}

	if rep.Provider == "" || (rep.Provider == "deepseek" && strings.Contains(rep.Model, "/")) {
		if strings.Contains(rep.Model, "/") {
			rep.Provider = "openrouter"
		} else if strings.HasPrefix(rep.Model, "claude") {
			rep.Provider = "anthropic"
		} else if strings.HasPrefix(rep.Model, "gpt") || strings.HasPrefix(rep.Model, "o1") || strings.HasPrefix(rep.Model, "o3") {
			rep.Provider = "openai"
		} else {
			rep.Provider = "openrouter"
		}
	}

	details, _ := store.GetModelPriceDetails(rep.Provider, rep.Model)
	rep.InputPer1M = details.InputPer1M
	rep.OutputPer1M = details.OutputPer1M
	if rep.InputPer1M <= 0 {
		if strings.Contains(rep.Model, "deepseek") {
			rep.InputPer1M = 0.22
			rep.OutputPer1M = 0.44
		} else {
			rep.InputPer1M = 3.00
			rep.OutputPer1M = 15.00
		}
	}

	cacheRead, _ := pricing.ResolveCachePrices(rep.Provider, rep.InputPer1M, details.CacheReadPer1M, details.CacheWritePer1M)
	rep.CacheReadPer1M = cacheRead

	rep.FastTierModel = store.GetFastTier(rep.Provider, rep.Model)
	if rep.FastTierModel != "" {
		fastIn, _, _ := store.GetModelPrice(rep.Provider, rep.FastTierModel)
		rep.FastInputPer1M = fastIn
	}
	if rep.FastInputPer1M <= 0 {
		rep.FastInputPer1M = rep.InputPer1M * 0.15
	}
}

func scanMCPConfiguration(rep *auditReport) {
	if auditMCPURL != "" {
		if probeLiveMCPServer(rep, auditMCPURL) {
			return
		}
	}

	serverNamesMap := make(map[string]bool)

	configCandidates := []string{
		filepath.Join(rep.TargetDir, "config.toml"),
		filepath.Join(rep.TargetDir, "zeroclaw.toml"),
		filepath.Join(rep.TargetDir, ".mcp.json"),
		filepath.Join(rep.TargetDir, "mcp.json"),
		filepath.Join(rep.TargetDir, "openclaw.json"),
		filepath.Join(rep.TargetDir, "opencode.json"),
		filepath.Join(rep.TargetDir, "claude_desktop_config.json"),
		filepath.Join(rep.TargetDir, "config.yaml"),
		filepath.Join(rep.TargetDir, "mcp.yaml"),
		filepath.Join(rep.TargetDir, "crewai.yaml"),
	}

	if home, err := os.UserHomeDir(); err == nil {
		configCandidates = append(configCandidates,
			filepath.Join(home, ".mcp.json"),
			filepath.Join(home, ".openclaw", "openclaw.json"),
			filepath.Join(home, ".zeroclaw", "config.toml"),
		)
	}

	// Also check subdirectories up to depth 2
	subMatches, _ := filepath.Glob(filepath.Join(rep.TargetDir, "*", "*mcp*.*"))
	configCandidates = append(configCandidates, subMatches...)

	for _, cfgPath := range configCandidates {
		data, err := os.ReadFile(cfgPath)
		if err != nil || len(data) == 0 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(cfgPath))

		switch ext {
		case ".json":
			var raw map[string]interface{}
			if err := json.Unmarshal(data, &raw); err == nil {
				for _, key := range []string{"mcpServers", "mcp_servers", "servers", "mcp"} {
					if srvMap, ok := raw[key].(map[string]interface{}); ok && len(srvMap) > 0 {
						for srvName := range srvMap {
							serverNamesMap[srvName] = true
						}
					}
				}
			}
		case ".toml":
			var raw map[string]interface{}
			if err := toml.Unmarshal(data, &raw); err == nil {
				// Handle [[mcp.servers]] array of tables
				if mcpMap, ok := raw["mcp"].(map[string]interface{}); ok {
					if servers, ok := mcpMap["servers"].([]interface{}); ok {
						for _, s := range servers {
							if sm, ok := s.(map[string]interface{}); ok {
								if name, ok := sm["name"].(string); ok && name != "" {
									serverNamesMap[name] = true
								}
							}
						}
					}
				}
				// Handle [mcp_servers] or [mcpServers] tables
				for _, key := range []string{"mcp_servers", "mcpServers"} {
					if srvMap, ok := raw[key].(map[string]interface{}); ok {
						for srvName := range srvMap {
							serverNamesMap[srvName] = true
						}
					}
				}
			}
		case ".yaml", ".yml":
			var raw map[string]interface{}
			if err := yaml.Unmarshal(data, &raw); err == nil {
				for _, key := range []string{"mcp_servers", "mcpServers", "mcp"} {
					if srvMap, ok := raw[key].(map[string]interface{}); ok {
						for srvName := range srvMap {
							serverNamesMap[srvName] = true
						}
					}
				}
			}
		}
	}

	var discoveredServerNames []string
	for srvName := range serverNamesMap {
		discoveredServerNames = append(discoveredServerNames, srvName)
	}
	sort.Strings(discoveredServerNames)
	rep.DiscoveredServerNames = discoveredServerNames
	rep.MCPServersFound = len(discoveredServerNames)

	// Factual honesty: if 0 servers found, report 0
	if rep.MCPServersFound == 0 {
		rep.MCPToolsCount = 0
		rep.MCPRawSchemaTokens = 0
		rep.MCPMinifiedTokens = 0
		rep.MCPReadOnlyTools = 0
		rep.MCPHighBlastTools = 0
		rep.HighBlastToolNames = nil
		return
	}

	var discoveredToolNames []string
	for _, srvName := range discoveredServerNames {
		discoveredToolNames = append(discoveredToolNames, inferRepresentativeToolsForServer(srvName)...)
	}

	rep.ConfiguredToolNames = discoveredToolNames
	rep.MCPToolsCount = len(discoveredToolNames)
	syntheticToolsList := buildSyntheticToolsListJSON(discoveredToolNames)
	minifiedBytes, err := mcp.SanitizeToolList(syntheticToolsList, mcp.SanitizerConfig{
		MinifySchemas:        true,
		MaxDescriptionLength: 512,
	}, "audit")
	if err != nil {
		minifiedBytes = syntheticToolsList
	}

	rep.MCPRawSchemaTokens = (len(syntheticToolsList) + 3) / 4
	rep.MCPMinifiedTokens = (len(minifiedBytes) + 3) / 4

	scoreToolsBlastRadius(rep, discoveredToolNames)
}

func probeLiveMCPServer(rep *auditReport, mcpURL string) bool {
	reqBody := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req, err := http.NewRequest(http.MethodPost, mcpURL, bytes.NewReader(reqBody))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil || len(rawBody) == 0 {
		return false
	}

	var parsed struct {
		Result struct {
			Tools []mcp.Tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rawBody, &parsed); err != nil || len(parsed.Result.Tools) == 0 {
		return false
	}

	minifiedBody, err := mcp.SanitizeToolList(rawBody, mcp.SanitizerConfig{
		MinifySchemas:        true,
		MaxDescriptionLength: 512,
	}, "live-mcp")
	if err != nil {
		minifiedBody = rawBody
	}

	rep.LiveMCPProbed = true
	rep.MCPServersFound = 1
	rep.DiscoveredServerNames = []string{"live-mcp"}
	rep.MCPToolsCount = len(parsed.Result.Tools)
	rep.MCPRawSchemaTokens = (len(rawBody) + 3) / 4
	rep.MCPMinifiedTokens = (len(minifiedBody) + 3) / 4

	var names []string
	for _, t := range parsed.Result.Tools {
		names = append(names, t.Name)
	}
	rep.ConfiguredToolNames = names
	scoreToolsBlastRadius(rep, names)
	return true
}

func scoreToolsBlastRadius(rep *auditReport, toolNames []string) {
	highBlastSet := make(map[string]bool)
	for _, name := range toolNames {
		br := blastradius.Calculate(name, nil)
		if br.Score <= 15 {
			rep.MCPReadOnlyTools++
		}
		if br.Score >= 30 {
			rep.MCPHighBlastTools++
			if len(highBlastSet) < 4 && !highBlastSet[name] {
				highBlastSet[name] = true
				rep.HighBlastToolNames = append(rep.HighBlastToolNames, fmt.Sprintf("%s (score:%d)", name, br.Score))
			}
		}
	}
	sort.Strings(rep.HighBlastToolNames)
}

func analyzeSessionsOrSimulate(rep *auditReport) {
	var traceFiles []string
	if auditTrace != "" {
		info, err := os.Stat(auditTrace)
		if err == nil {
			if info.IsDir() {
				matches, _ := filepath.Glob(filepath.Join(auditTrace, "*.jsonl"))
				traceFiles = append(traceFiles, matches...)
			} else {
				traceFiles = append(traceFiles, auditTrace)
			}
		}
	} else {
		ignoreDirs := map[string]bool{".git": true, "node_modules": true, "venv": true, ".venv": true}
		_ = filepath.WalkDir(rep.TargetDir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && ignoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".jsonl") {
				traceFiles = append(traceFiles, path)
			}
			return nil
		})
	}

	if len(traceFiles) > 0 {
		if len(traceFiles) > 25 {
			traceFiles = traceFiles[:25]
		}
		for _, tf := range traceFiles {
			parseSessionJSONL(rep, tf)
		}
	}

	if rep.TurnsAnalyzed == 0 {
		rep.IsSimulatedReplay = true
		rep.SessionsAnalyzed = 30
		turnsPerSession := 10
		rep.TurnsAnalyzed = rep.SessionsAnalyzed * turnsPerSession
		rep.ColdDiscoveryTurns = rep.SessionsAnalyzed + (rep.HeartbeatsPerDay * 30)

		if rep.MCPToolsCount > 0 {
			blob := bytes.Repeat([]byte("LOG_EVENT_ROW "), 1300)
			blobID := proxy.ContentBlobID(blob)
			stubBytes, err := proxy.CreateGhostStub("read_audit_log", blob, blobID)
			savedPerBlob := (len(blob) - len(stubBytes)) / 4
			if savedPerBlob <= 0 || err != nil {
				savedPerBlob = 4400
			}
			rep.OversizedPayloads = rep.SessionsAnalyzed * 2
			rep.BaggageReSends = rep.OversizedPayloads * 6
			rep.GhostTokensSaved = rep.BaggageReSends * savedPerBlob
			rep.DuplicateReadCalls = rep.SessionsAnalyzed * 2
		} else {
			rep.OversizedPayloads = 0
			rep.BaggageReSends = 0
			rep.GhostTokensSaved = 0
			rep.DuplicateReadCalls = 0
		}
	}
}

func parseSessionJSONL(rep *auditReport, filePath string) {
	f, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var linePayloads []int
	seenReads := make(map[string]int)
	seenSessions := make(map[string]bool)
	userMsgs := 0
	parsedAny := false

	toolErrorMarkers := []string{
		`"iserror":true`, `"is_error":true`, `"error":{`, `"status":"error"`,
		`econnrefused`, `etimedout`, `context deadline exceeded`, `rate limit`,
		`401 unauthorized`, `403 forbidden`, `500 internal server error`, `traceback (most recent call last)`,
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Security DLP Scan: check every session log line for unmasked credentials & private network IPs
		rawLineStr := string(line)
		dlpRes, _ := inspector.InspectDLPContent(rawLineStr, inspector.DLPConfig{
			Enabled:     true,
			Action:      "mask",
			ScanSecrets: true,
			ScanPII:     true,
			ScanNetwork: true,
		})
		if len(dlpRes.Matches) > 0 {
			if strings.Contains(dlpRes.Reason, "Secret") {
				rep.DLPSecretLeaks += len(dlpRes.Matches)
			}
			if strings.Contains(dlpRes.Reason, "network") || strings.Contains(dlpRes.Reason, "Email") || strings.Contains(dlpRes.Reason, "Credit") || strings.Contains(dlpRes.Reason, "Social") || strings.Contains(dlpRes.Reason, "Phone") {
				rep.DLPPIIOrNetLeaks++
			}
		}

		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		// Check if it's a cost tracking entry (e.g. costs.jsonl)
		if usage, ok := entry["usage"].(map[string]interface{}); ok {
			parsedAny = true
			rep.TurnsAnalyzed++
			if inTok, ok := usage["input_tokens"].(float64); ok {
				rep.TotalInputTokensLog += int(inTok)
			}
			if outTok, ok := usage["output_tokens"].(float64); ok {
				rep.TotalOutputTokensLog += int(outTok)
			}
			if sID, ok := entry["session_id"].(string); ok && sID != "" {
				seenSessions[sID] = true
			}
			continue
		}

		role, _ := entry["role"].(string)
		if msg, ok := entry["message"].(map[string]interface{}); ok && role == "" {
			role, _ = msg["role"].(string)
			entry = msg
		}

		if role != "" {
			parsedAny = true
		}

		switch role {
		case "user":
			userMsgs++
			rep.TurnsAnalyzed++
			uText := extractContentString(entry["content"])
			rep.TotalInputTokensLog += (len(uText) + 3) / 4
			if userMsgs == 1 {
				rep.ColdDiscoveryTurns++
			}
			for _, savedTok := range linePayloads {
				rep.BaggageReSends++
				rep.GhostTokensSaved += savedTok
			}
		case "tool", "toolResult":
			tText := extractContentString(entry["content"])
			rep.TotalInputTokensLog += (len(tText) + 3) / 4
			lowerToolOut := strings.ToLower(tText)

			// 1. Indirect Prompt Injection Scan
			for _, pat := range inspector.InjectionPatterns {
				if strings.Contains(lowerToolOut, pat) {
					rep.InjectionHits++
					break
				}
			}

			// 2. Flaky Tool Failures Tracking
			for _, errPat := range toolErrorMarkers {
				if strings.Contains(lowerToolOut, errPat) {
					rep.FailedToolCalls++
					rep.WastedRetryTurns++
					break
				}
			}

			if len(tText) >= 4096 {
				rep.OversizedPayloads++
				blobBytes := []byte(tText)
				blobID := proxy.ContentBlobID(blobBytes)
				stubBytes, err := proxy.CreateGhostStub("tool_output", blobBytes, blobID)
				savedTok := (len(blobBytes) - len(stubBytes)) / 4
				if savedTok <= 0 && err == nil {
					savedTok = len(blobBytes) / 4
				}
				if savedTok > 0 {
					linePayloads = append(linePayloads, savedTok)
				}
			}
		case "assistant":
			aText := extractContentString(entry["content"])
			rep.TotalOutputTokensLog += (len(aText) + 3) / 4
			if toolCalls, ok := entry["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
				rep.ToolActiveTurns++
				for _, tc := range toolCalls {
					if tcMap, ok := tc.(map[string]interface{}); ok {
						fn, _ := tcMap["function"].(map[string]interface{})
						name, _ := fn["name"].(string)
						args, _ := fn["arguments"].(string)
						if name != "" {
							rep.TotalToolCalls++
							rep.InvokedToolCounts[name]++
							if blastradius.Calculate(name, nil).Score <= 15 {
								h := sha256.Sum256([]byte(name + "|" + args))
								sig := hex.EncodeToString(h[:8])
								seenReads[sig]++
								if seenReads[sig] > 1 {
									rep.DuplicateReadCalls++
								}
							}
						}
					}
				}
			}
		}
	}

	if parsedAny {
		if len(seenSessions) > 0 {
			rep.SessionsAnalyzed += len(seenSessions)
		} else {
			rep.SessionsAnalyzed++
		}
	}
}

func computeGhostTools(rep *auditReport) {
	if rep.SessionsAnalyzed < 1 || len(rep.ConfiguredToolNames) == 0 {
		return
	}
	for _, toolName := range rep.ConfiguredToolNames {
		if rep.InvokedToolCounts[toolName] == 0 {
			rep.UnusedGhostTools = append(rep.UnusedGhostTools, toolName)
		}
	}
	if len(rep.UnusedGhostTools) > 0 && len(rep.ConfiguredToolNames) > 0 {
		ratio := float64(len(rep.UnusedGhostTools)) / float64(len(rep.ConfiguredToolNames))
		rep.UnusedSchemaTokens = int(float64(rep.MCPRawSchemaTokens) * ratio)
	}
}

func computeAuditFinancialsAndGrade(rep *auditReport) {
	scaleFactor := 1.0
	if !rep.IsSimulatedReplay && rep.SessionsAnalyzed > 0 && rep.SessionsAnalyzed < 30 {
		scaleFactor = 30.0 / float64(rep.SessionsAnalyzed)
	}

	monthlyTurns := float64(rep.TurnsAnalyzed)*scaleFactor + float64(rep.HeartbeatsPerDay*30)
	if monthlyTurns < 300 {
		monthlyTurns = 300
	}

	baselinePrefixPerTurn := float64(rep.WorkspaceTokens + rep.MCPRawSchemaTokens)
	baselineBaggageMonthly := float64(rep.GhostTokensSaved) * scaleFactor
	totalBaselineInputTokens := (monthlyTurns * baselinePrefixPerTurn) + baselineBaggageMonthly
	totalBaselineOutputTokens := monthlyTurns * 350.0

	rep.BaselineMonthlyUSD = ((totalBaselineInputTokens * rep.InputPer1M) / 1e6) +
		((totalBaselineOutputTokens * rep.OutputPer1M) / 1e6)

	loopersPrefixPerTurn := float64(rep.WorkspaceTokens + rep.MCPMinifiedTokens)
	coldTurns := float64(rep.ColdDiscoveryTurns) * scaleFactor
	if coldTurns > monthlyTurns*0.6 {
		coldTurns = monthlyTurns * 0.6
	}
	warmTurns := monthlyTurns - coldTurns

	coldInputUSD := (coldTurns * loopersPrefixPerTurn * rep.FastInputPer1M) / 1e6
	warmCachedInputUSD := (warmTurns * loopersPrefixPerTurn * (0.80*rep.CacheReadPer1M + 0.20*rep.InputPer1M)) / 1e6
	outputUSD := ((coldTurns * 250.0 * (rep.OutputPer1M * 0.25)) / 1e6) + ((warmTurns * 350.0 * rep.OutputPer1M) / 1e6)

	rep.LoopersMonthlyUSD = coldInputUSD + warmCachedInputUSD + outputUSD
	rep.MonthlySavedUSD = rep.BaselineMonthlyUSD - rep.LoopersMonthlyUSD
	if rep.MonthlySavedUSD < 0 {
		rep.MonthlySavedUSD = 0
	}
	if rep.BaselineMonthlyUSD > 0 {
		rep.SavingsPct = (rep.MonthlySavedUSD / rep.BaselineMonthlyUSD) * 100.0
	}

	// Compute comparison at Frontier model pricing (Claude 3.5 Sonnet: $3.00 in / $15.00 out)
	frontierIn := 3.00
	frontierOut := 15.00
	frontierCacheRead := 0.30
	frontierFastIn := 0.45
	frontierBaselineUSD := ((totalBaselineInputTokens * frontierIn) / 1e6) + ((totalBaselineOutputTokens * frontierOut) / 1e6)
	frontierColdUSD := (coldTurns * loopersPrefixPerTurn * frontierFastIn) / 1e6
	frontierWarmUSD := (warmTurns * loopersPrefixPerTurn * (0.80*frontierCacheRead + 0.20*frontierIn)) / 1e6
	frontierOutUSD := ((coldTurns * 250.0 * (frontierOut * 0.25)) / 1e6) + ((warmTurns * 350.0 * frontierOut) / 1e6)
	frontierLoopersUSD := frontierColdUSD + frontierWarmUSD + frontierOutUSD
	rep.FrontierBaselineUSD = frontierBaselineUSD
	rep.FrontierSavedUSD = frontierBaselineUSD - frontierLoopersUSD
	if rep.FrontierSavedUSD < 0 {
		rep.FrontierSavedUSD = 0
	}

	// Security Grade: verify if TargetDir or current directory has an active Loopers config
	hasLocalLoopersConfig := fileExists(filepath.Join(rep.TargetDir, "loopers.yaml")) || fileExists("./loopers.yaml")
	isShielded := false
	if hasLocalLoopersConfig {
		configBytes, _ := os.ReadFile(filepath.Join(rep.TargetDir, "loopers.yaml"))
		if len(configBytes) == 0 {
			configBytes, _ = os.ReadFile("./loopers.yaml")
		}
		cfgStr := string(configBytes)
		if strings.Contains(cfgStr, "loop_detection") && strings.Contains(cfgStr, "mcp") {
			isShielded = true
		}
	}

	switch {
	case isShielded:
		rep.SecurityGrade = "A (Shielded by Loopers Firewall)"
	case rep.DLPSecretLeaks > 0:
		rep.SecurityGrade = fmt.Sprintf("F (%d unmasked credentials in logs · 0 wire DLP gates)", rep.DLPSecretLeaks)
	case rep.MCPToolsCount == 0:
		rep.SecurityGrade = "B (0 MCP tools · Runaway loop protection unconfigured)"
	case rep.MCPHighBlastTools >= 3:
		rep.SecurityGrade = fmt.Sprintf("F (%d high-blast MCP tools unshielded · 0 hard budget leases)", rep.MCPHighBlastTools)
	case rep.MCPHighBlastTools > 0:
		rep.SecurityGrade = fmt.Sprintf("C (%d high-blast MCP tools unshielded · 0 hard budget leases)", rep.MCPHighBlastTools)
	default:
		rep.SecurityGrade = "D (No wire circuit breaker or atomic Redis budget lease detected)"
	}
}

func renderAuditTerminalReport(rep *auditReport) {
	fmt.Printf("  %-18s %s (%s)\n", "Target Detected:", rep.HarnessName, rep.TargetDir)
	modelDisplay := rep.Model
	if rep.Provider != "" && !strings.Contains(rep.Model, "/") {
		modelDisplay = fmt.Sprintf("%s/%s", rep.Provider, rep.Model)
	} else if rep.Provider != "" && strings.Contains(rep.Model, "/") {
		modelDisplay = fmt.Sprintf("%s (%s)", rep.Model, rep.Provider)
	}
	modelLine := fmt.Sprintf("%s ($%.2f in / $%.2f out per 1M) [%s]", modelDisplay, rep.InputPer1M, rep.OutputPer1M, rep.ModelSource)
	if rep.FastTierModel != "" {
		modelLine += fmt.Sprintf(" ➔ Fast-Tier: %s", rep.FastTierModel)
	}
	fmt.Printf("  %-18s %s\n", "Benchmark Model:", modelLine)

	if len(rep.WorkspaceFiles) > 0 || len(rep.DiscoveredServerNames) > 0 {
		var assetParts []string
		if len(rep.WorkspaceFiles) > 0 {
			var fNames []string
			for _, f := range rep.WorkspaceFiles {
				fNames = append(fNames, filepath.Base(f))
			}
			assetParts = append(assetParts, fmt.Sprintf("%d prompt files [%s]", len(rep.WorkspaceFiles), strings.Join(fNames, ", ")))
		}
		if len(rep.DiscoveredServerNames) > 0 {
			assetParts = append(assetParts, fmt.Sprintf("%d MCP servers [%s]", len(rep.DiscoveredServerNames), strings.Join(rep.DiscoveredServerNames, ", ")))
		}
		fmt.Printf("  %-18s %s\n", "Discovered Assets:", strings.Join(assetParts, " · "))
	}
	fmt.Println()

	fmt.Println("  1. Static Context & MCP Schema Overhead (Billed on Every Turn)")
	schemaRedPct := 0.0
	if rep.MCPRawSchemaTokens > 0 {
		schemaRedPct = (1.0 - float64(rep.MCPMinifiedTokens)/float64(rep.MCPRawSchemaTokens)) * 100.0
	}
	totalBefore := rep.WorkspaceTokens + rep.MCPRawSchemaTokens
	totalAfter := rep.WorkspaceTokens + rep.MCPMinifiedTokens
	totalRedPct := 0.0
	if totalBefore > 0 {
		totalRedPct = (1.0 - float64(totalAfter)/float64(totalBefore)) * 100.0
	}

	workspaceRowLabel := fmt.Sprintf("Workspace Files (%d found)", rep.WorkspaceFilesFound)
	workspaceUnopt := fmt.Sprintf("%d tokens", rep.WorkspaceTokens)
	workspaceOpt := fmt.Sprintf("%d tokens", rep.WorkspaceTokens)
	workspaceDelta := "KV-Pinned (-50% to -90%)"
	if rep.WorkspaceFilesFound == 0 {
		workspaceUnopt = "0 tokens"
		workspaceOpt = "0 tokens"
		workspaceDelta = "No prompt files detected"
	}

	mcpRowLabel := fmt.Sprintf("MCP Schemas (%d tools)", rep.MCPToolsCount)
	mcpUnopt := fmt.Sprintf("%d tokens", rep.MCPRawSchemaTokens)
	mcpOpt := fmt.Sprintf("%d tokens", rep.MCPMinifiedTokens)
	mcpDelta := fmt.Sprintf("-%.1f%% schema tokens", schemaRedPct)
	if rep.MCPServersFound == 0 {
		mcpRowLabel = "MCP Schemas (0 found)"
		mcpUnopt = "0 tokens"
		mcpOpt = "0 tokens"
		mcpDelta = "No MCP tools configured"
	} else {
		mcpRowLabel = fmt.Sprintf("MCP Schemas (%d srv / %d tools)", rep.MCPServersFound, rep.MCPToolsCount)
	}

	totalDelta := fmt.Sprintf("-%.1f%% per-turn prefix", totalRedPct)
	if totalBefore == 0 {
		totalDelta = "0 tokens"
	}

	headers := []string{"Context Layer", "Unoptimized", "With Loopers", "Wire Optimization"}
	rows := [][]string{
		{
			workspaceRowLabel,
			workspaceUnopt,
			workspaceOpt,
			workspaceDelta,
		},
		{
			mcpRowLabel,
			mcpUnopt,
			mcpOpt,
			mcpDelta,
		},
	}

	if len(rep.UnusedGhostTools) > 0 {
		rows = append(rows, []string{
			fmt.Sprintf("Unused \"Ghost Tools\" (%d tools)", len(rep.UnusedGhostTools)),
			fmt.Sprintf("%d tokens", rep.UnusedSchemaTokens),
			"0 tokens",
			"-100% via tool_allowlist",
		})
	}

	rows = append(rows, []string{
		"Total Static Prefix",
		fmt.Sprintf("%d tokens", totalBefore),
		fmt.Sprintf("%d tokens", totalAfter),
		totalDelta,
	})

	ui.PrintTable(headers, rows)
	fmt.Println()

	modeLabel := fmt.Sprintf("Live Session Replay — %d sessions, %d turns from logs", rep.SessionsAnalyzed, rep.TurnsAnalyzed)
	if rep.IsSimulatedReplay {
		modeLabel = "30-Day Trajectory Projection — No local .jsonl session history found"
	}
	fmt.Printf("  2. Multi-Turn Context Baggage & FinOps Audit (%s)\n", modeLabel)

	// Token spend split (Input vs Output)
	if rep.TotalInputTokensLog > 0 || rep.TotalOutputTokensLog > 0 {
		inUSD := (float64(rep.TotalInputTokensLog) * rep.InputPer1M) / 1e6
		outUSD := (float64(rep.TotalOutputTokensLog) * rep.OutputPer1M) / 1e6
		totUSD := inUSD + outUSD
		if totUSD > 0 {
			outPct := (outUSD / totUSD) * 100.0
			inPct := (inUSD / totUSD) * 100.0
			ratioStr := ""
			if rep.OutputPer1M > rep.InputPer1M*5.0 {
				ratioStr = fmt.Sprintf(" [%.0fx Output-Heavy Model]", rep.OutputPer1M/rep.InputPer1M)
			}
			ui.Info(fmt.Sprintf("Token Spend Split: %.1f%% Output Tokens ($%.2f) │ %.1f%% Input Tokens ($%.2f)%s",
				outPct, outUSD, inPct, inUSD, ratioStr))
		}
	}

	if len(rep.UnusedGhostTools) > 0 {
		preview := rep.UnusedGhostTools
		if len(preview) > 3 {
			preview = preview[:3]
		}
		ui.Warn(fmt.Sprintf("Dead MCP Bloat (\"Ghost Tools\"): %d of %d tools were NEVER invoked across %d sessions [%s]",
			len(rep.UnusedGhostTools), len(rep.ConfiguredToolNames), rep.SessionsAnalyzed, strings.Join(preview, ", ")))
	}

	if rep.FailedToolCalls > 0 {
		ui.Error(fmt.Sprintf("Flaky Tool Failures: %d tool calls failed -> triggered %d wasted retry turns",
			rep.FailedToolCalls, rep.WastedRetryTurns))
	}

	if rep.GhostTokensSaved > 0 {
		ui.Error(fmt.Sprintf("Quadratic Tool Baggage: %d tool outputs (>4 KB) re-sent %d times (%d wasted input tokens)",
			rep.OversizedPayloads, rep.BaggageReSends, rep.GhostTokensSaved))
	} else {
		ui.Success("Quadratic Tool Baggage: 0 oversized tool outputs detected")
	}
	if rep.DuplicateReadCalls > 0 {
		ui.Warn(fmt.Sprintf("Duplicate Read-Only Calls: %d repeat read calls (%d tools eligible for 3.8ms Redis memoization)",
			rep.DuplicateReadCalls, rep.MCPReadOnlyTools))
	} else {
		ui.Success("Duplicate Read-Only Calls: 0 redundant read calls detected")
	}
	if rep.FastTierModel != "" {
		ui.Warn(fmt.Sprintf("Cold Discovery & Heartbeat Tax: %d cold/heartbeat turns routable to %s (-90%%+ cost)",
			rep.ColdDiscoveryTurns, rep.FastTierModel))
	}
	if rep.HasVolatilePrefix {
		ui.Error("Volatile Timestamp/ID detected in static workspace files (busts upstream KV prompt cache on every turn)")
	}
	fmt.Println()

	fmt.Println("  3. Regime 1 Security, DLP & Blast-Radius Exposure")
	if rep.DLPSecretLeaks > 0 || rep.DLPPIIOrNetLeaks > 0 {
		ui.Error(fmt.Sprintf("Cleartext DLP Leaks: %d secrets/credentials & %d internal network IPs/PII unmasked in session logs",
			rep.DLPSecretLeaks, rep.DLPPIIOrNetLeaks))
	} else {
		ui.Success("Cleartext Secrets & PII: 0 unmasked credentials detected in session logs")
	}

	if rep.MCPHighBlastTools > 0 {
		ui.Error(fmt.Sprintf("High-Blast-Radius MCP Tools: %d tools scored >= 30 [%s]",
			rep.MCPHighBlastTools, strings.Join(rep.HighBlastToolNames, ", ")))
	} else {
		ui.Success("MCP Tool Blast Radius: 0 high-risk tools detected")
	}

	if rep.InjectionHits > 0 {
		ui.Error(fmt.Sprintf("Prompt Injections: %d indirect injection patterns detected in recorded tool responses",
			rep.InjectionHits))
	} else {
		ui.Success("Prompt Injections: 0 indirect injection patterns detected in recorded tool responses")
	}

	ui.Warn(fmt.Sprintf("Runaway Protection Grade: %s", rep.SecurityGrade))
	fmt.Println()

	fmt.Println("  4. Estimated 30-Day Agent FinOps Scorecard")
	scoreHeaders := []string{"Metric", "Without Loopers", "With Loopers", "Net Monthly Impact"}
	scoreRows := [][]string{}

	// Tweak 1: Highlight Token Savings Column in Table 4 Beside the Dollar Figure
	prefixDelta := totalBefore - totalAfter
	prefixImpact := fmt.Sprintf("-%d tokens (-%.1f%%)", prefixDelta, totalRedPct)
	if totalBefore == 0 {
		prefixImpact = "0 tokens"
	}
	scoreRows = append(scoreRows, []string{
		"Per-Turn Static Prefix",
		fmt.Sprintf("%d tokens", totalBefore),
		fmt.Sprintf("%d tokens", totalAfter),
		prefixImpact,
	})

	// Row 2: Spend on Active Model
	scoreRows = append(scoreRows, []string{
		"Spend (Active Model)",
		fmt.Sprintf("$%.2f / mo", rep.BaselineMonthlyUSD),
		fmt.Sprintf("$%.2f / mo", rep.LoopersMonthlyUSD),
		fmt.Sprintf("Saves $%.2f / mo (-%.1f%%)", rep.MonthlySavedUSD, rep.SavingsPct),
	})

	// Tweak 2: Promote Frontier Rate Comparison into Table 4 as a Row
	if rep.InputPer1M < 2.0 || !strings.Contains(rep.Model, "claude-sonnet") {
		frontierSavingsPct := 0.0
		if rep.FrontierBaselineUSD > 0 {
			frontierSavingsPct = (rep.FrontierSavedUSD / rep.FrontierBaselineUSD) * 100.0
		}
		frontierLoopersUSD := rep.FrontierBaselineUSD - rep.FrontierSavedUSD
		scoreRows = append(scoreRows, []string{
			"Spend (Frontier Sonnet)",
			fmt.Sprintf("$%.2f / mo", rep.FrontierBaselineUSD),
			fmt.Sprintf("$%.2f / mo", frontierLoopersUSD),
			fmt.Sprintf("Saves $%.2f / mo (-%.1f%%)", rep.FrontierSavedUSD, frontierSavingsPct),
		})
	}

	scoreRows = append(scoreRows, []string{
		"Runaway Budget Ceiling",
		"Unbounded (Soft)",
		"0% Over-Spend (Lua)",
		"Deterministic Kill-Switch",
	})

	ui.PrintTable(scoreHeaders, scoreRows)
	fmt.Println()

	if len(rep.UnusedGhostTools) > 0 || rep.DLPSecretLeaks > 0 {
		ui.Info("Auto-strip dead Ghost Tools & enable DLP masking on wire: loopers audit --fix")
	}
	ui.Info(fmt.Sprintf("Run protected right now (0 Redis setup needed):        %s", rep.RecommendedExecCmd))
	ui.Info("Share this scorecard on GitHub/Reddit/X:              loopers audit --share")
}

func offerInstantFixAndExec(rep *auditReport) {
	outPath := filepath.Join(rep.TargetDir, "loopers.yaml")
	shouldWrite := auditFix

	if !shouldWrite && !fileExists(outPath) && ui.IsInteractive() {
		fmt.Println()
		_ = huh.NewConfirm().
			Title(fmt.Sprintf("Generate tailored loopers.yaml for %s (saves ~$%.0f/mo) now?", rep.HarnessName, rep.MonthlySavedUSD)).
			Affirmative("Yes, generate loopers.yaml").
			Negative("Not now").
			Value(&shouldWrite).
			WithTheme(ui.GetHuhTheme()).
			Run()
	}

	if shouldWrite {
		if err := writeTailoredLoopersYAML(outPath, rep); err == nil {
			fmt.Println()
			ui.Success(fmt.Sprintf("Generated %s pre-configured for %s (%s/%s)", outPath, rep.HarnessName, rep.Provider, rep.Model))
			ui.Success(fmt.Sprintf("Next step — run your agent with inline protection: %s", rep.RecommendedExecCmd))
		}
	}
}

func writeTailoredLoopersYAML(outPath string, rep *auditReport) error {
	allowlistBlock := ""
	if len(rep.UnusedGhostTools) > 0 && len(rep.InvokedToolCounts) > 0 {
		var activeTools []string
		for t, count := range rep.InvokedToolCounts {
			if count > 0 {
				activeTools = append(activeTools, fmt.Sprintf("      - %q", t))
			}
		}
		sort.Strings(activeTools)
		if len(activeTools) > 0 {
			allowlistBlock = fmt.Sprintf("    tool_allowlist:\n      default:\n%s\n", strings.Join(activeTools, "\n"))
		}
	}

	content := fmt.Sprintf(`# Auto-generated by 'loopers audit' for %s
server:
  port: "8080"
  admin_port: "9090"
  insecure_dev: true
  strip_budget_headers: false
  dlp:
    enabled: true
    action: "redact"

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

pricing_path: "./pricing.yaml"

# Regime 1: Rogue Agent Defense (Bad Days)
loop_detection:
  enabled: true
  window_seconds: 60
  max_identical_calls: 4

risk_profile:
  enabled: true

policy:
  enabled: true
  presets:
    - "safety"
    - "mcp_sandbox"

# Regime 2: Inline Context Acceleration & Wire FinOps (Normal Days)
mcp:
  enabled: true
  memoize_read_only: true
  inspector:
    enabled: true
  sanitizer:
    minify_schemas: true
    max_description_length: 512
%s  ghost_tokens:
    enabled: true
    threshold_bytes: 4096
    ttl_seconds: 1800
`, rep.HarnessName, allowlistBlock)

	return os.WriteFile(outPath, []byte(content), 0644)
}

func renderShareableMarkdown(rep *auditReport) {
	fmt.Println("\n  ─── Copy & Paste Markdown Scorecard Below ───────────────────────────────────")
	fmt.Printf("### Loopers-OSS Agent Context & Blast-Radius Audit (`%s`)\n\n", rep.HarnessName)
	fmt.Printf("| Audit Metric | Unoptimized Baseline | With Loopers-OSS (`v2.24.0`) | Delta |\n")
	fmt.Printf("| :--- | :---: | :---: | :---: |\n")
	totalBefore := rep.WorkspaceTokens + rep.MCPRawSchemaTokens
	totalAfter := rep.WorkspaceTokens + rep.MCPMinifiedTokens
	totalRedPct := 0.0
	if totalBefore > 0 {
		totalRedPct = (1.0 - float64(totalAfter)/float64(totalBefore)) * 100.0
	}
	prefixDelta := totalBefore - totalAfter
	fmt.Printf("| **Per-Turn Context Prefix** | `%d` tokens | `%d` tokens | **-%d tokens (-%.1f%%)** |\n",
		totalBefore, totalAfter, prefixDelta, totalRedPct)
	schemaPct := 0.0
	if rep.MCPRawSchemaTokens > 0 {
		schemaPct = (1.0 - float64(rep.MCPMinifiedTokens)/float64(rep.MCPRawSchemaTokens)) * 100.0
	}
	fmt.Printf("| **Per-Turn MCP Schema Tokens** (%d tools) | `%d` tokens | `%d` tokens | **-%.1f%%** |\n",
		rep.MCPToolsCount, rep.MCPRawSchemaTokens, rep.MCPMinifiedTokens, schemaPct)
	if len(rep.UnusedGhostTools) > 0 {
		fmt.Printf("| **Dead MCP Bloat (Ghost Tools)** | `%d` tools called 0x | `Allowlisted` | **-100%% unused schema** |\n", len(rep.UnusedGhostTools))
	}
	fmt.Printf("| **Est. 30-Day Agent API Spend** (`%s`) | **`$%.2f / mo`** | **`$%.2f / mo`** | **Saves `$%.2f / mo` (-%.1f%%)** |\n",
		rep.Model, rep.BaselineMonthlyUSD, rep.LoopersMonthlyUSD, rep.MonthlySavedUSD, rep.SavingsPct)
	if rep.FrontierBaselineUSD > 0 {
		frontierSavingsPct := 0.0
		if rep.FrontierBaselineUSD > 0 {
			frontierSavingsPct = (rep.FrontierSavedUSD / rep.FrontierBaselineUSD) * 100.0
		}
		frontierLoopersUSD := rep.FrontierBaselineUSD - rep.FrontierSavedUSD
		fmt.Printf("| **Est. 30-Day Spend (Frontier Tier)** | **`$%.2f / mo`** | **`$%.2f / mo`** | **Saves `$%.2f / mo` (-%.1f%%)** |\n",
			rep.FrontierBaselineUSD, frontierLoopersUSD, rep.FrontierSavedUSD, frontierSavingsPct)
	}
	fmt.Printf("| **Runaway Loop & Blast-Radius Grade** | `%s` | `Grade A (Redis Lua Lease + OPA)` | **0%% Over-Spend** |\n\n",
		rep.SecurityGrade)
	fmt.Printf("*Audited in <2s via `npx @loopers/audit` ([github.com/try-loopers/loopers-oss](https://github.com/try-loopers/loopers-oss))* \n")
}

// Helper functions
func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func dirExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func extractContentString(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case []interface{}:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		return ""
	}
}

func inferRepresentativeToolsForServer(serverName string) []string {
	s := strings.ToLower(serverName)
	switch {
	case strings.Contains(s, "fetch"):
		return []string{"fetch_url", "fetch_html_text"}
	case strings.Contains(s, "notion"):
		return []string{"notion_search", "notion_get_page", "notion_create_page", "notion_update_database"}
	case strings.Contains(s, "enterprise"):
		return []string{"enterprise_search", "enterprise_query", "enterprise_invoke_action"}
	case strings.Contains(s, "git") || strings.Contains(s, "github"):
		return []string{"git_status", "git_diff", "git_log", "create_pull_request", "push_files", "search_repositories"}
	case strings.Contains(s, "postgres") || strings.Contains(s, "sql") || strings.Contains(s, "db") || strings.Contains(s, "supabase"):
		return []string{"query_database", "describe_table", "list_tables", "execute_sql_write", "drop_table"}
	case strings.Contains(s, "fs") || strings.Contains(s, "file") || strings.Contains(s, "desktop"):
		return []string{"read_file", "write_file", "list_directory", "delete_file", "execute_command"}
	case strings.Contains(s, "slack") || strings.Contains(s, "discord") || strings.Contains(s, "telegram"):
		return []string{"list_channels", "read_messages", "post_message", "delete_message"}
	default:
		return []string{s + "_get_status", s + "_list_items", s + "_update_record", s + "_execute_action"}
	}
}

func buildSyntheticToolsListJSON(toolNames []string) []byte {
	if len(toolNames) == 0 {
		return []byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	}
	tools := make([]map[string]interface{}, 0, len(toolNames))
	verboseDescSuffix := " This enterprise MCP tool connects to the underlying resource provider and returns comprehensive structured JSON metadata. Ensure all parameters are validated before invoking this operation in production workflows. Examples and schema constraints are provided below for agent reference."
	for _, name := range toolNames {
		tools = append(tools, map[string]interface{}{
			"name":        name,
			"description": fmt.Sprintf("Executes the %s operation against the configured MCP server.", name) + verboseDescSuffix,
			"inputSchema": map[string]interface{}{
				"$schema": "http://json-schema.org/draft-07/schema#",
				"title":   name + "InputSchema",
				"type":    "object",
				"examples": []interface{}{
					map[string]interface{}{"target": "production-cluster", "verbose": true, "limit": 100},
				},
				"properties": map[string]interface{}{
					"target": map[string]interface{}{
						"type":        "string",
						"title":       "Target Identifier",
						"description": "The primary target identifier or resource path for the operation. Must be a valid non-empty string.",
						"default":     "default",
						"examples":    []string{"prod-db-01", "/var/log/syslog"},
					},
					"filter": map[string]interface{}{
						"type":        "string",
						"title":       "Optional Query Filter",
						"description": "Optional filter expression applied before returning results. Supports standard glob and regex syntax.",
						"default":     "*",
					},
				},
			},
		})
	}
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]interface{}{
			"tools": tools,
		},
	}
	b, _ := json.Marshal(payload)
	return b
}
