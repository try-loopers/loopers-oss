package main

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/try-loopers/loopers-oss/cmd/loopers/ui"
)

func runScreen(action string) {
	switch action {
	case "screen_init":
		screenInit()
	case "screen_doctor":
		screenDoctor()
	case "screen_keys":
		screenKeys()
	case "screen_budget":
		screenBudget()
	case "screen_context_acceleration":
		screenContextAcceleration()
	case "screen_loop":
		screenLoop()
	case "screen_mcp":
		screenMCP()
	case "screen_policy":
		screenPolicy()
	case "screen_zsp":
		screenZSP()
	case "screen_observability":
		screenObservability()
	case "screen_exec":
		screenExec()
	default:
		fmt.Printf("Screen %s not implemented yet.\n", action)
		pressEnterToContinue()
	}
}

func pressEnterToContinue() {
	fmt.Println()
	var unused string
	huh.NewSelect[string]().
		Options(huh.NewOption("[ Return to main menu ]", "back")).
		Value(&unused).
		WithTheme(ui.GetHuhTheme()).
		WithKeyMap(ui.GetHuhKeyMap()).
		Run()
}

func screenDoctor() {
	doctorCmd.Run(doctorCmd, []string{})
	pressEnterToContinue()
}

func screenKeys() {
	ui.PrintLogo()
	var action string
	err := huh.NewSelect[string]().
		Title("Keys").
		Options(
			huh.NewOption("Create Key", "create"),
			huh.NewOption("List Keys", "list"),
			huh.NewOption("Revoke Key", "revoke"),
			huh.NewOption("Back", "back"),
		).
		Value(&action).
		WithTheme(ui.GetHuhTheme()).
		Run()
	if err != nil || action == "back" {
		return
	}

	switch action {
	case "create":
		keysCreateCmd.Run(keysCreateCmd, []string{})
	case "list":
		keysListCmd.Run(keysListCmd, []string{})
	case "revoke":
		keysRevokeCmd.Run(keysRevokeCmd, []string{})
	}
	pressEnterToContinue()
}

func screenBudget() {
	ui.PrintLogo()
	var action string
	err := huh.NewSelect[string]().
		Title("Budgets").
		Options(
			huh.NewOption("Set Budget", "set"),
			huh.NewOption("Budget Status", "status"),
			huh.NewOption("Back", "back"),
		).
		Value(&action).
		WithTheme(ui.GetHuhTheme()).
		Run()
	if err != nil || action == "back" {
		return
	}

	switch action {
	case "set":
		budgetSetCmd.Run(budgetSetCmd, []string{})
	case "status":
		budgetStatusCmd.Run(budgetStatusCmd, []string{})
	}
	pressEnterToContinue()
}
