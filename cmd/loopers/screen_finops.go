package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
	"github.com/try-loopers/loopers-oss/internal/budget"
)

type finopsStatusMsg struct {
	status map[string]budget.FinOpsRecord
	err    error
}

type finopsTickMsg time.Time

type finopsModel struct {
	client      *budget.Client
	hash        string
	displayHash string
	name        string
	provider    string
	window      string
	status      map[string]budget.FinOpsRecord
	err         error
	lastUpdated time.Time
	tickCount   int
	width       int
	height      int
}

func newFinOpsModel(client *budget.Client, hash, name, provider string) finopsModel {
	displayHash := hash
	if len(displayHash) > 12 {
		displayHash = displayHash[:12] + "..."
	}
	return finopsModel{
		client:      client,
		hash:        hash,
		displayHash: displayHash,
		name:        name,
		provider:    provider,
		window:      "daily",
		lastUpdated: time.Now(),
	}
}

func (m finopsModel) Init() tea.Cmd {
	return tea.Batch(
		m.fetchStatusCmd(),
		m.tickCmd(),
	)
}

func (m finopsModel) tickCmd() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return finopsTickMsg(t)
	})
}

func (m finopsModel) fetchStatusCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		st, err := m.client.GetFinOpsStatus(ctx, m.hash)
		return finopsStatusMsg{status: st, err: err}
	}
}

func (m finopsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case finopsStatusMsg:
		if msg.err == nil {
			m.status = msg.status
			m.err = nil
			m.lastUpdated = time.Now()
		} else {
			m.err = msg.err
		}
		return m, nil

	case finopsTickMsg:
		m.tickCount++
		return m, tea.Batch(
			m.fetchStatusCmd(),
			m.tickCmd(),
		)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "1", "d", "D":
			m.window = "daily"
		case "2", "h", "H":
			m.window = "hourly"
		case "3", "w", "W":
			m.window = "weekly"
		case "4", "m", "M":
			m.window = "monthly"
		case "5", "t", "T":
			m.window = "total"
		case "tab":
			order := []string{"daily", "hourly", "weekly", "monthly", "total"}
			for i, w := range order {
				if w == m.window {
					m.window = order[(i+1)%len(order)]
					break
				}
			}
		case "r", "R":
			return m, m.fetchStatusCmd()
		}
	}
	return m, nil
}

func (m finopsModel) View() string {
	var sb strings.Builder

	// Top spacing
	sb.WriteString("\n")

	// Logo / Header banner
	solidBlock := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render("[x]")
	hollowBlock := lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).Render("[ ]")
	logo := solidBlock + " " + hollowBlock + "   " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render("LOOPERS") + "   " + lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render("AI FIREWALL FOR THE AGENTIC ERA")
	sb.WriteString("  " + logo + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#333333")).Render("  ────────────────────────────────────────────────────────────────────────────") + "\n\n")

	// Live status indicator
	pulseColor := lipgloss.Color("#00FF66")
	if m.tickCount%2 == 1 {
		pulseColor = lipgloss.Color("#00CC55")
	}
	liveDot := lipgloss.NewStyle().Foreground(pulseColor).Bold(true).Render("● LIVE")
	updateInfo := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(fmt.Sprintf("auto-updates every 1s  •  Last polled: %s", m.lastUpdated.Format("15:04:05")))
	sb.WriteString(fmt.Sprintf("  %s  %s\n\n", liveDot, updateInfo))

	// Navigation tabs for timeframe
	windows := []struct {
		key   string
		label string
	}{
		{"daily", "[1] Daily"},
		{"hourly", "[2] Hourly"},
		{"weekly", "[3] Weekly"},
		{"monthly", "[4] Monthly"},
		{"total", "[5] All-Time"},
	}
	var tabs []string
	for _, w := range windows {
		if w.key == m.window {
			tabs = append(tabs, lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#00FF66")).Bold(true).Render(" "+w.label+"* "))
		} else {
			tabs = append(tabs, lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(" "+w.label+" "))
		}
	}
	sb.WriteString("  " + strings.Join(tabs, "  ") + "\n\n")

	// Render Scorecard
	if m.status != nil {
		rec := m.status[m.window]
		title := fmt.Sprintf("FinOps Scorecard  ›  %s  (%s / %s)  [%s]", m.displayHash, m.name, m.provider, strings.ToUpper(m.window))
		cardStr := ui.RenderFinOpsScorecard(title, rec.ActualSpendUSD, rec.CounterfactualUSD, rec.NetSavedUSD, rec.SavingsPercentage, rec.GhostTokensSaved, rec.GhostPayloadBytes, rec.FastTierTurns, rec.FastTierSavedUSD, rec.ToolMemoHits, rec.ToolMemoSavedUSD, rec.SchemaTokensSaved)
		sb.WriteString(cardStr + "\n\n")
	} else if m.err != nil {
		sb.WriteString(fmt.Sprintf("  Error fetching FinOps status: %v\n\n", m.err))
	} else {
		sb.WriteString("  Connecting to FinOps engine...\n\n")
	}

	// Hotkeys helper footer
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render("  [1-5/Tab] Switch Timeframe   [r] Force Refresh   [q/Esc] Return to Menu") + "\n")

	return sb.String()
}

func screenFinOps() {
	redisClient, err := getRedisClient()
	if err != nil {
		ui.Error("Failed to connect to Redis. Ensure it is running.")
		pressEnterToContinue()
		return
	}
	defer redisClient.Close()

	options, err := fetchActiveKeys(redisClient)
	if err != nil {
		ui.Error(fmt.Sprintf("Failed to fetch keys: %v", err))
		pressEnterToContinue()
		return
	}
	if len(options) == 0 {
		ui.Error("No active agent keys found in registry.")
		pressEnterToContinue()
		return
	}

	var hash string
	if len(options) == 1 {
		hash = options[0].Value
	} else {
		err = huh.NewSelect[string]().
			Title("Select Agent Key to Inspect FinOps Savings").
			Options(options...).
			Value(&hash).
			WithTheme(ui.GetHuhTheme()).
			Run()

		if err != nil || hash == "" {
			return
		}
	}

	ctx := context.Background()
	rdb := redisClient.GetUnderlyingClient()

	keyConfig, _ := rdb.HGetAll(ctx, fmt.Sprintf("loopers:key:%s", hash)).Result()
	name := keyConfig["name"]
	provider := keyConfig["provider"]

	model := newFinOpsModel(redisClient, hash, name, provider)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running FinOps dashboard: %v\n", err)
	}
}
