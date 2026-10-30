package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAudit_DetectHarnessAndWorkspace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "loopers-audit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create openclaw.json
	if err := os.WriteFile(filepath.Join(tmpDir, "openclaw.json"), []byte(`{"version": "1.0"}`), 0644); err != nil {
		t.Fatalf("failed to write openclaw.json: %v", err)
	}

	// Create SOUL.md with volatile timestamp
	soulContent := `# Agent Soul
timestamp: 2026-10-30T14:48:02Z
You are a reliable software engineering assistant.
`
	if err := os.WriteFile(filepath.Join(tmpDir, "SOUL.md"), []byte(soulContent), 0644); err != nil {
		t.Fatalf("failed to write SOUL.md: %v", err)
	}

	rep := &auditReport{TargetDir: tmpDir}
	detectHarnessAndWorkspace(rep)

	if rep.HarnessSlug != "openclaw" {
		t.Errorf("expected harness slug openclaw, got %s", rep.HarnessSlug)
	}
	if !rep.HasVolatilePrefix {
		t.Errorf("expected volatile prefix to be detected, got false")
	}
	if rep.WorkspaceFilesFound < 1 {
		t.Errorf("expected at least 1 workspace file, got %d", rep.WorkspaceFilesFound)
	}
	if rep.WorkspaceTokens <= 0 {
		t.Errorf("expected workspace tokens > 0, got %d", rep.WorkspaceTokens)
	}
}

func TestAudit_MCPToolsScoringAndMinification(t *testing.T) {
	rep := &auditReport{TargetDir: "."}
	tools := []string{"read_file", "git_status", "execute_command", "delete_record"}

	scoreToolsBlastRadius(rep, tools)

	if rep.MCPReadOnlyTools != 2 {
		t.Errorf("expected 2 read-only tools, got %d", rep.MCPReadOnlyTools)
	}
	if rep.MCPHighBlastTools != 2 {
		t.Errorf("expected 2 high-blast tools, got %d", rep.MCPHighBlastTools)
	}

	jsonBytes := buildSyntheticToolsListJSON(tools)
	if len(jsonBytes) == 0 {
		t.Fatal("expected non-empty synthetic tools json")
	}
}

func TestAudit_ParseSessionJSONL(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "loopers-trace-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	traceFile := filepath.Join(tmpDir, "session_test.jsonl")
	largeOutput := strings.Repeat("ROW_DATA_LOG_ENTRY ", 300) // ~5.7 KB

	lines := []string{
		`{"role": "user", "content": "Fetch system logs"}`,
		fmt.Sprintf(`{"role": "tool", "content": "%s"}`, largeOutput),
		`{"role": "user", "content": "Analyze those logs"}`,
		`{"role": "assistant", "content": "Analysis complete."}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(traceFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write trace file: %v", err)
	}

	rep := &auditReport{TargetDir: tmpDir}
	parseSessionJSONL(rep, traceFile)

	if rep.SessionsAnalyzed != 1 {
		t.Errorf("expected 1 session analyzed, got %d", rep.SessionsAnalyzed)
	}
	if rep.TurnsAnalyzed != 2 {
		t.Errorf("expected 2 turns analyzed, got %d", rep.TurnsAnalyzed)
	}
	if rep.OversizedPayloads != 1 {
		t.Errorf("expected 1 oversized payload, got %d", rep.OversizedPayloads)
	}
	if rep.GhostTokensSaved <= 0 {
		t.Errorf("expected GhostTokensSaved > 0, got %d", rep.GhostTokensSaved)
	}
}

func TestAudit_WriteTailoredLoopersYAML(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "loopers-yaml-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	targetYAML := filepath.Join(tmpDir, "loopers.yaml")
	rep := &auditReport{
		HarnessName: "OpenClaw Autonomous Agent Workspace",
	}

	if err := writeTailoredLoopersYAML(targetYAML, rep); err != nil {
		t.Fatalf("writeTailoredLoopersYAML failed: %v", err)
	}

	data, err := os.ReadFile(targetYAML)
	if err != nil {
		t.Fatalf("failed to read generated yaml: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "mcp:") {
		t.Errorf("expected mcp block in generated yaml")
	}
	if !strings.Contains(content, "ghost_tokens:") {
		t.Errorf("expected ghost_tokens in generated yaml")
	}
	if !strings.Contains(content, "loop_detection:") {
		t.Errorf("expected loop_detection in generated yaml")
	}
}

func TestAudit_StartEmbeddedFirewallIfNeeded(t *testing.T) {
	// Running with empty proxyKey and empty proxyURL should spin up miniredis + server.NewServer
	proxyURL, proxyKey, cleanup := startEmbeddedFirewallIfNeeded("", "", "openai")
	defer cleanup()

	if proxyURL == "" {
		t.Fatal("expected non-empty proxyURL")
	}
	if !strings.HasPrefix(proxyKey, "lp-") {
		t.Errorf("expected generated proxyKey to start with lp-, got %s", proxyKey)
	}

	// Verify health endpoint responds
	resp, err := http.Get(proxyURL + "/health")
	if err != nil {
		t.Fatalf("failed to GET /health on embedded firewall: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200 on /health, got %d", resp.StatusCode)
	}
}
