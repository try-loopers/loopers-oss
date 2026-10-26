package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
)

type WindowStatus struct {
	Limit        float64 `json:"limit"`
	CurrentSpend float64 `json:"current_spend"`
	SavedUSD     float64 `json:"saved_usd,omitempty"`
}

func main() {
	loopersURL := os.Getenv("LOOPERS_URL")
	loopersKey := os.Getenv("LOOPERS_KEY")
	keyHash := os.Getenv("KEY_HASH")
	minRemainingStr := os.Getenv("MIN_REMAINING")

	if loopersURL == "" || loopersKey == "" || keyHash == "" || minRemainingStr == "" {
		fmt.Println("Error: Missing required environment variables (LOOPERS_URL, LOOPERS_KEY, KEY_HASH, MIN_REMAINING)")
		os.Exit(1)
	}

	minRemaining, err := strconv.ParseFloat(minRemainingStr, 64)
	if err != nil {
		fmt.Printf("Error: Invalid MIN_REMAINING value '%s': %v\n", minRemainingStr, err)
		os.Exit(1)
	}

	url := fmt.Sprintf("%s/budget/status", loopersURL)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("Error creating HTTP request: %v\n", err)
		os.Exit(1)
	}

	req.Header.Set("Authorization", "Bearer "+loopersKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error performing HTTP request to Loopers: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Error: Loopers server returned status %d. Response: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var statusMap map[string]WindowStatus
	err = json.NewDecoder(resp.Body).Decode(&statusMap)
	if err != nil {
		fmt.Printf("Error decoding JSON response from Loopers: %v\n", err)
		os.Exit(1)
	}

	hasExceeded := false
	var minRemainingFound *float64
	var failingWindow string
	var failingLimit float64
	var failingSpend float64

	for windowName, status := range statusMap {
		if status.Limit > 0 {
			remaining := status.Limit - status.CurrentSpend
			if minRemainingFound == nil || remaining < *minRemainingFound {
				minRemainingFound = &remaining
			}

			if remaining < minRemaining {
				hasExceeded = true
				failingWindow = windowName
				failingLimit = status.Limit
				failingSpend = status.CurrentSpend
			}
		}
	}

	if hasExceeded {
		fmt.Printf("🚨 Budget Check FAILED!\n")
		fmt.Printf("Window '%s' has only $%.4f remaining (Limit: $%.2f, Spend: $%.4f) which is below the required minimum of $%.2f\n",
			failingWindow, failingLimit-failingSpend, failingLimit, failingSpend, minRemaining)
		os.Exit(1)
	}

	if minRemainingFound != nil {
		fmt.Printf("✅ Budget Check PASSED. Minimum remaining budget is $%.4f (required: $%.2f)\n", *minRemainingFound, minRemaining)
	} else {
		fmt.Printf("✅ Budget Check PASSED. No budget limits configured on this key (unlimited remaining).\n")
	}
}
