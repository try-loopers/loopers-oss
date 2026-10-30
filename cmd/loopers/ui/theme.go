package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

var (
	// Brand Colors (tryloopers.com aesthetic)
	colorTextPrimary   = lipgloss.Color("#ffffff")
	colorTextSecondary = lipgloss.Color("#a0a0a0")
	colorTextTertiary  = lipgloss.Color("#666666")
	colorBorderStrong  = lipgloss.Color("#333333")

	// Semantic Colors
	colorGreen  = lipgloss.Color("42")
	colorRed    = lipgloss.Color("196")
	colorYellow = lipgloss.Color("220")

	// Base Styles
	styleHeaderBorder = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderStrong).
				Padding(0, 1)

	styleHeaderTitle = lipgloss.NewStyle().
				Foreground(colorTextPrimary).
				Bold(true)

	styleSuccess = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	styleError   = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	styleWarn    = lipgloss.NewStyle().Foreground(colorYellow)
	styleInfo    = lipgloss.NewStyle().Foreground(colorTextTertiary)
	styleLabel   = lipgloss.NewStyle().Foreground(colorTextSecondary).Width(12)

	styleTableBorder = lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(colorBorderStrong)

	styleTableHdr = lipgloss.NewStyle().
			Foreground(colorTextPrimary).
			Bold(true)

	styleCardBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderStrong).
			Padding(1, 2)

	styleSummaryPass = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorGreen).
				Padding(0, 1)

	styleSummaryFail = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorRed).
				Padding(0, 1)
)

func GetHuhTheme() *huh.Theme {
	t := huh.ThemeBase()

	// Apply our monochrome palette
	t.Focused.Base = t.Focused.Base.BorderForeground(colorBorderStrong).MarginLeft(2)
	t.Blurred.Base = t.Blurred.Base.MarginLeft(2)

	t.Focused.Title = lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true)
	t.Focused.TextInput.Cursor = lipgloss.NewStyle().Foreground(colorTextPrimary)
	t.Focused.TextInput.Prompt = lipgloss.NewStyle().Foreground(colorTextPrimary)
	t.Focused.TextInput.Text = lipgloss.NewStyle().Foreground(colorTextPrimary)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colorTextPrimary)
	t.Focused.Option = lipgloss.NewStyle().Foreground(colorTextPrimary)
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colorTextPrimary)
	t.Focused.UnselectedOption = lipgloss.NewStyle().Foreground(colorTextTertiary)

	t.Blurred.Title = lipgloss.NewStyle().Foreground(colorTextTertiary)
	t.Blurred.TextInput.Prompt = lipgloss.NewStyle().Foreground(colorTextTertiary)
	t.Blurred.TextInput.Text = lipgloss.NewStyle().Foreground(colorTextTertiary)

	t.Help.Ellipsis = lipgloss.NewStyle().Foreground(colorTextSecondary)
	t.Help.ShortKey = lipgloss.NewStyle().Foreground(colorTextSecondary)
	t.Help.ShortDesc = lipgloss.NewStyle().Foreground(colorTextTertiary)
	t.Help.ShortSeparator = lipgloss.NewStyle().Foreground(colorBorderStrong)

	return t
}

func GetHuhKeyMap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit.SetKeys("ctrl+c", "esc")
	return km
}
