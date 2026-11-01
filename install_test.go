package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPreservesOtherHooksAndUninstalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	previousVersion := version
	version = "v0.1.0"
	t.Cleanup(func() { version = previousVersion })
	config := filepath.Join(home, "hooks.json")
	oldGate := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": quoteCommand(installedPath(home)) + " tutor-gate"}}}
	initial, err := json.Marshal(map[string]any{"description": "keep me", "hooks": map[string]any{
		"Stop":             []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo safe"}}}, oldGate},
		"PreToolUse":       []any{oldGate},
		"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": "echo other"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, initial, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := installGuard(false); err != nil {
			t.Fatal(err)
		}
	}
	root, hooks, err := loadHookConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	var description string
	_ = json.Unmarshal(root["description"], &description)
	if description != "keep me" || len(hooks["Stop"]) == 0 {
		t.Fatal("installer changed unrelated config")
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(hooks["UserPromptSubmit"], &groups); err != nil {
		t.Fatal(err)
	}
	mcpRegistered, err := configuredTutorMCP(home)
	if err != nil {
		t.Fatal(err)
	}
	expectedGroups := 2
	if mcpRegistered {
		expectedGroups++
	}
	if len(groups) != expectedGroups {
		t.Fatalf("expected %d hook groups, got %d", expectedGroups, len(groups))
	}
	if len(hooks["PreToolUse"]) != 0 || strings.Contains(string(hooks["Stop"]), "tutor-gate") {
		t.Fatal("installer left obsolete active-model review gates")
	}
	if _, err := os.Stat(installedPath(home)); err != nil {
		t.Fatal("installed binary missing:", err)
	}
	if err := enableTutorSession(home, "test-session"); err != nil {
		t.Fatal(err)
	}
	if startTutorTrace(10) == nil {
		t.Fatal("could not create test timing record")
	}
	if err := uninstallGuard(); err != nil {
		t.Fatal(err)
	}
	mcpRegistered, err = configuredTutorMCP(home)
	if err != nil || mcpRegistered {
		t.Fatalf("uninstall left Tutor MCP server: %t %v", mcpRegistered, err)
	}
	for _, path := range []string{tutorSettingsPath(home), tutorSessionsDir(home), tutorMetricsDir(home), dashboardStatePath(home)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("uninstall kept Tutor data %s: %v", path, err)
		}
	}
	_, hooks, err = loadHookConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hooks["UserPromptSubmit"]), "english-guard") || strings.Contains(string(hooks["Stop"]), "english-guard") || len(hooks["PreToolUse"]) != 0 || len(hooks["Stop"]) == 0 {
		t.Fatal("uninstall removed unrelated hook or left guard hook")
	}
	backups, _ := filepath.Glob(config + ".english-guard.*.bak")
	if len(backups) == 0 {
		t.Fatal("installer did not back up hooks.json")
	}
}

func TestLegacyMigrationAndInvalidConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	config := filepath.Join(home, "hooks.json")
	legacy, err := json.Marshal(map[string]any{"hooks": map[string]any{
		"UserPromptSubmit": []any{map[string]any{"hooks": []any{
			map[string]any{"type": "command", "command": filepath.Join(home, "english_gate")},
		}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	if err := installGuard(true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(config)
	if strings.Contains(string(data), "english_gate") {
		t.Fatal("legacy hook remains after migration")
	}
	if err := os.WriteFile(config, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := installGuard(false); err == nil {
		t.Fatal("invalid configuration should fail")
	}
	data, _ = os.ReadFile(config)
	if string(data) != "not json" {
		t.Fatal("invalid configuration was overwritten")
	}
}
