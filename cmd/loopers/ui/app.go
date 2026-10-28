package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type AppModel struct {
	width, height int
	cursor        int
	screen        string    // "menu" | "diagnostics" | "create-key" | ...
	form          *huh.Form // non-nil when a sub-form is active
	result        string    // last action output to display

	// Exported so main.go can read the user's selection
	Action string
}

var menuItems = []struct {
	label  string
	action string
}{
	{"Quick Start / Init", "screen_init"},
	{"Diagnostics", "screen_doctor"},
	{"Keys & Agent Identity", "screen_keys"},
	{"Budgets & Rate Limits", "screen_budget"},
	{"FinOps & Savings Scorecard", "screen_finops"},
	{"Context Acceleration & Ghost Tokens", "screen_context_acceleration"},
	{"Loop Detection", "screen_loop"},
	{"MCP Governance & Injection Wall", "screen_mcp"},
	{"Policy Engine & FSM Gating", "screen_policy"},
	{"Zero-Trust & A2A Security", "screen_zsp"},
	{"Observability", "screen_observability"},
	{"Execute Protected Agent", "screen_exec"},
	{"Start Firewall Server", "serve"},
	{"Quit", "quit"},
}

func NewApp() AppModel {
	return AppModel{
		screen: "menu",
	}
}

func (m AppModel) Init() tea.Cmd {
	return nil
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.screen == "menu" {
			switch msg.String() {
			case "ctrl+c", "q":
				return m, tea.Quit
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(menuItems)-1 {
					m.cursor++
				}
			case "enter":
				m.Action = menuItems[m.cursor].action
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

func (m AppModel) View() string {
	var sb strings.Builder

	// Top spacing
	sb.WriteString("\n")

	// Render Logo
	solidBlock := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true).Render("[x]")
	hollowBlock := lipgloss.NewStyle().Foreground(colorTextTertiary).Render("[ ]")
	gap := " "

	row1Logo := solidBlock + gap + hollowBlock
	row2Logo := solidBlock + gap + solidBlock

	title := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true).Render("LOOPERS")
	subtitle := lipgloss.NewStyle().Foreground(colorTextSecondary).Render("AI FIREWALL FOR THE AGENTIC ERA")

	sb.WriteString("  " + row1Logo + "   " + title + "\n")
	sb.WriteString("  " + row2Logo + "   " + subtitle + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorBorderStrong).Render("  ──────────────────────────────────────────────────────") + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorTextSecondary).Render("                                         ↑ ↓  navigate") + "\n")

	// Menu
	for i, item := range menuItems {
		cursor := "    "
		style := lipgloss.NewStyle().Foreground(colorTextTertiary)
		if m.cursor == i {
			cursor = "  > "
			style = lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true)
		}

		line := cursor + item.label
		// Pad to align the hints
		padLen := 44 - lipgloss.Width(line)
		if padLen < 0 {
			padLen = 0
		}
		line += strings.Repeat(" ", padLen)

		sb.WriteString(style.Render(line))

		hintStyle := lipgloss.NewStyle().Foreground(colorTextSecondary)
		if i == 0 {
			sb.WriteString(hintStyle.Render(" enter  select"))
		} else if i == 1 {
			sb.WriteString(hintStyle.Render(" q      quit"))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(colorBorderStrong).Render("  ──────────────────────────────────────────────────────") + "\n")
	sb.WriteString(lipgloss.NewStyle().Foreground(colorTextTertiary).Render("  LOOPERS  ·  AI Firewall  ·  MIT Licensed  ·  15 Providers  ·  ~1-2ms P99") + "\n")

	return sb.String()
}
