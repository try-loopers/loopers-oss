package ui

import (
	"os"

	"golang.org/x/term"
)

// IsInteractive returns true if standard input is attached to a terminal.
// This is used to determine if interactive TUI elements (like huh forms) should be launched.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// TerminalWidth returns the width of the terminal, or 90 if not attached or undetermined.
func TerminalWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 40 {
		return w
	}
	if w, _, err := term.GetSize(int(os.Stdin.Fd())); err == nil && w > 40 {
		return w
	}
	return 90
}
