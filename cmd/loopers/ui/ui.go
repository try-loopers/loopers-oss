package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func PrintHeader(title string) {
	fmt.Println(styleHeaderBorder.Render(styleHeaderTitle.Render(title)))
}

func Success(msg string) {
	fmt.Printf("  %s  %s\n", styleSuccess.Render("✓"), msg)
}

func Error(msg string) {
	fmt.Printf("  %s  %s\n", styleError.Render("✗"), msg)
}

func Warn(msg string) {
	fmt.Printf("  %s  %s\n", styleWarn.Render("△"), msg)
}

func Info(msg string) {
	fmt.Printf("  %s  %s\n", styleInfo.Render("›"), styleInfo.Render(msg))
}

func PrintTable(headers []string, rows [][]string) {
	// Simple column width calculation
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = lipgloss.Width(h)
	}
	for _, row := range rows {
		for i, c := range row {
			if w := lipgloss.Width(c); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var out strings.Builder
	// Render headers
	for i, h := range headers {
		cell := lipgloss.NewStyle().Width(widths[i]).Render(h)
		out.WriteString(styleTableHdr.Render(cell))
		if i < len(headers)-1 {
			out.WriteString(styleInfo.Render(" │ "))
		}
	}
	out.WriteString("\n")

	// Render rows
	for _, row := range rows {
		for i, c := range row {
			cell := lipgloss.NewStyle().Width(widths[i]).Render(c)
			out.WriteString(cell)
			if i < len(row)-1 {
				out.WriteString(styleInfo.Render(" │ "))
			}
		}
		out.WriteString("\n")
	}

	fmt.Println(styleTableBorder.Render(out.String()))
}

func PrintBudgetBar(name string, spend, limit float64) {
	if limit == 0 {
		fmt.Printf("  %-8s %s\n", name, styleInfo.Render("not configured"))
		return
	}

	pct := (spend / limit) * 100
	barWidth := 20
	filled := int(math.Round((spend / limit) * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}

	empty := barWidth - filled

	barColor := colorGreen
	if pct >= 90 {
		barColor = colorRed
	} else if pct >= 70 {
		barColor = colorYellow
	}

	barStyle := lipgloss.NewStyle().Foreground(barColor)
	emptyStyle := lipgloss.NewStyle().Foreground(colorTextTertiary)

	bar := barStyle.Render(strings.Repeat("█", filled)) + emptyStyle.Render(strings.Repeat("░", empty))

	stats := fmt.Sprintf("$%.2f / $%.2f  (%.0f%%)", spend, limit, pct)

	msg := fmt.Sprintf("  %-8s %s  %s", name, bar, stats)
	if pct > 100 {
		msg += styleError.Render(" △ OVER LIMIT")
	} else if pct >= 90 {
		msg += styleWarn.Render(" △ approaching limit")
	}

	fmt.Println(msg)
}

func PrintKeyCard(name, provider, rawKey, hash string) {
	var sb strings.Builder

	sb.WriteString(styleSuccess.Render("✓  Key Created Successfully") + "\n\n")
	sb.WriteString(fmt.Sprintf("%s%s\n", styleLabel.Render("Name"), name))
	sb.WriteString(fmt.Sprintf("%s%s\n\n", styleLabel.Render("Provider"), provider))
	sb.WriteString(fmt.Sprintf("%s%s\n", styleLabel.Render("Raw Key"), lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true).Render(rawKey)))
	sb.WriteString(fmt.Sprintf("%s%s\n\n", styleLabel.Render("Hash"), hash))

	warnLines := "△  Copy the raw key now. It won't be\n   shown again."
	sb.WriteString(styleWarn.Render(warnLines))

	fmt.Println(styleCardBorder.Render(sb.String()))
}

func PrintSummary(pass bool, issues int) {
	if pass {
		fmt.Println(styleSummaryPass.Render("✓  All systems go"))
	} else {
		msg := fmt.Sprintf("✗  Fix %d issues above", issues)
		fmt.Println(styleSummaryFail.Render(msg))
	}
}

func PrintLogo() {
	// Clear the primary screen buffer robustly
	ClearScreen()

	// The logo mark is a 2×2 grid with a checkerboard pattern of solid and hollow squares
	// [Solid]  [Hollow]
	// [Solid]  [Solid]
	solidBlock := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true).Render("[x]")
	hollowBlock := lipgloss.NewStyle().Foreground(colorTextTertiary).Render("[ ]")

	gap := " "

	row1Logo := solidBlock + gap + hollowBlock
	row2Logo := solidBlock + gap + solidBlock

	title := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true).Render("LOOPERS")
	subtitle := lipgloss.NewStyle().Foreground(colorTextSecondary).Render("AI FIREWALL FOR THE AGENTIC ERA  ·  (Press Ctrl+C to go back)")

	row1 := "  " + row1Logo + "   " + title
	row2 := "  " + row2Logo + "   " + subtitle

	fmt.Println()
	fmt.Println(row1)
	fmt.Println(row2)
	fmt.Println()
}

func RenderFinOpsScorecard(title string, actualSpend, counterfactualSpend, netSaved, savingsPct float64, ghostTokens, ghostBytes int64, fastTierTurns int64, fastTierSaved float64, toolMemoHits int64, toolMemoSaved float64, schemaTokens int64) string {
	var sb strings.Builder

	headerStyle := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true)
	labelStyle := lipgloss.NewStyle().Foreground(colorTextSecondary).Width(24)
	valStyle := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true)
	greenValStyle := lipgloss.NewStyle().Foreground(colorGreen).Bold(true)

	sb.WriteString(headerStyle.Render("  "+title) + "\n\n")

	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("Total Spend:"), valStyle.Render(fmt.Sprintf("$%.4f", actualSpend))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("Counterfactual Spend:"), valStyle.Render(fmt.Sprintf("$%.4f", counterfactualSpend))))
	sb.WriteString(fmt.Sprintf("  %s%s\n\n", labelStyle.Render("Net Savings:"), greenValStyle.Render(fmt.Sprintf("$%.4f (%.1f%% saved)", netSaved, savingsPct))))

	sb.WriteString(headerStyle.Render("  FinOps Acceleration Breakdown:") + "\n")
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Ghost Tokens:"), valStyle.Render(fmt.Sprintf("%d tokens offloaded (%s payload cached)", ghostTokens, FormatBytes(ghostBytes)))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Fast-Tier Routing:"), valStyle.Render(fmt.Sprintf("%d turns routed ($%.4f saved)", fastTierTurns, fastTierSaved))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Tool Memoization:"), valStyle.Render(fmt.Sprintf("%d idempotent calls ($%.4f saved)", toolMemoHits, toolMemoSaved))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Schema Minifier:"), valStyle.Render(fmt.Sprintf("%d prompt discovery tokens stripped", schemaTokens))))

	return styleCardBorder.Render(sb.String())
}

func PrintFinOpsScorecard(title string, actualSpend, counterfactualSpend, netSaved, savingsPct float64, ghostTokens, ghostBytes int64, fastTierTurns int64, fastTierSaved float64, toolMemoHits int64, toolMemoSaved float64, schemaTokens int64) {
	fmt.Println(RenderFinOpsScorecard(title, actualSpend, counterfactualSpend, netSaved, savingsPct, ghostTokens, ghostBytes, fastTierTurns, fastTierSaved, toolMemoHits, toolMemoSaved, schemaTokens))
}

func PrintFinOpsExitSummary(requests int, actualSpend, counterfactualSpend, netSaved, savingsPct float64, ghostTokens int64, fastTierTurns int64, toolMemoHits int64) {
	var sb strings.Builder

	title := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true).Render("LOOPERS FINOPS SESSION SUMMARY")
	sb.WriteString("  " + title + "\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(colorTextSecondary).Width(22)
	valStyle := lipgloss.NewStyle().Foreground(colorTextPrimary).Bold(true)
	greenValStyle := lipgloss.NewStyle().Foreground(colorGreen).Bold(true)

	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("Requests Intercepted:"), valStyle.Render(fmt.Sprintf("%d turns", requests))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("Actual Spend:"), valStyle.Render(fmt.Sprintf("$%.4f", actualSpend))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("Counterfactual Spend:"), valStyle.Render(fmt.Sprintf("$%.4f", counterfactualSpend))))
	sb.WriteString(fmt.Sprintf("  %s%s\n\n", labelStyle.Render("Net Savings:"), greenValStyle.Render(fmt.Sprintf("$%.4f (%.1f%% saved)", netSaved, savingsPct))))

	sb.WriteString(lipgloss.NewStyle().Foreground(colorTextSecondary).Render("  Optimizations Active:") + "\n")
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Ghost Tokens:"), valStyle.Render(fmt.Sprintf("%d context tokens offloaded", ghostTokens))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Fast-Tier Routing:"), valStyle.Render(fmt.Sprintf("%d discovery turns accelerated", fastTierTurns))))
	sb.WriteString(fmt.Sprintf("  %s%s\n", labelStyle.Render("  • Tool Memoization:"), valStyle.Render(fmt.Sprintf("%d idempotent reads served (<4ms)", toolMemoHits))))

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBorderStrong).
		Padding(0, 1)

	fmt.Println()
	fmt.Println(border.Render(sb.String()))
}

func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
