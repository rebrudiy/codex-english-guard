package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const tutorMCPName = "english-guard"

func codexHome() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Abs(home)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

func installedPath(home string) string {
	name := "english-guard"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, "english-guard", "versions", safeVersion(), name)
}

func safeVersion() string {
	for _, r := range version {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return "dev"
		}
	}
	if version == "" || version == "." || version == ".." {
		return "dev"
	}
	return version
}

func copyExecutable(destination string) error {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	source, err = filepath.EvalSymlinks(source)
	if err != nil {
		return err
	}
	if filepath.Clean(source) == filepath.Clean(destination) {
		return nil
	}
	if equalFiles(source, destination) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(destination), ".english-guard-*")
	if err != nil {
		return err
	}
	temporary := out.Name()
	defer os.Remove(temporary)
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err = out.Chmod(0755); err != nil {
		out.Close()
		return err
	}
	if err = out.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, destination)
}

func equalFiles(a, b string) bool {
	hash := func(path string) ([32]byte, error) {
		file, err := os.Open(path)
		if err != nil {
			return [32]byte{}, err
		}
		defer file.Close()
		state := sha256.New()
		if _, err := io.Copy(state, file); err != nil {
			return [32]byte{}, err
		}
		var sum [32]byte
		copy(sum[:], state.Sum(nil))
		return sum, nil
	}
	ha, ea := hash(a)
	hb, eb := hash(b)
	return ea == nil && eb == nil && ha == hb
}

func quoteCommand(path string) string {
	if runtime.GOOS == "windows" {
		return `"` + path + `"`
	}
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func loadHookConfig(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	root := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return nil, nil, fmt.Errorf("invalid %s: %w", path, err)
		}
		if root == nil {
			return nil, nil, fmt.Errorf("invalid %s: expected object", path)
		}
	}
	hooks := map[string]json.RawMessage{}
	if raw, exists := root["hooks"]; exists {
		if err := json.Unmarshal(raw, &hooks); err != nil || hooks == nil {
			return nil, nil, fmt.Errorf("invalid %s: hooks must be an object", path)
		}
	}
	return root, hooks, nil
}

func filterPromptGroups(raw json.RawMessage, home string, replaceLegacy bool) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var groups []json.RawMessage
	if err := json.Unmarshal(raw, &groups); err != nil || groups == nil {
		return nil, fmt.Errorf("UserPromptSubmit must be an array")
	}
	kept := make([]json.RawMessage, 0, len(groups))
	for _, groupRaw := range groups {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(groupRaw, &group); err != nil || group == nil {
			return nil, fmt.Errorf("UserPromptSubmit contains an invalid group")
		}
		var handlers []json.RawMessage
		if err := json.Unmarshal(group["hooks"], &handlers); err != nil || handlers == nil {
			return nil, fmt.Errorf("UserPromptSubmit group has invalid hooks")
		}
		remaining := make([]json.RawMessage, 0, len(handlers))
		for _, handlerRaw := range handlers {
			var handler map[string]json.RawMessage
			if err := json.Unmarshal(handlerRaw, &handler); err != nil || handler == nil {
				return nil, fmt.Errorf("UserPromptSubmit has an invalid handler")
			}
			var command string
			_ = json.Unmarshal(handler["command"], &command)
			var handlerType, server, tool string
			_ = json.Unmarshal(handler["type"], &handlerType)
			_ = json.Unmarshal(handler["server"], &server)
			_ = json.Unmarshal(handler["tool"], &tool)
			if handlerType == "mcp_tool" && server == tutorMCPName && tool == "check_prompt" {
				continue
			}
			if belongsToGuard(command, home) || replaceLegacy && isLegacyGuard(command, home) {
				continue
			}
			remaining = append(remaining, handlerRaw)
		}
		if len(remaining) > 0 {
			group["hooks"], _ = json.Marshal(remaining)
			updated, _ := json.Marshal(group)
			kept = append(kept, updated)
		}
	}
	return kept, nil
}

func configuredTutorMCP(home string) (bool, error) {
	codex, err := exec.LookPath("codex")
	if err != nil {
		return false, nil
	}
	command := exec.Command(codex, "mcp", "list", "--json")
	command.Env = append(os.Environ(), "CODEX_HOME="+home)
	output, err := command.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("list Codex MCP servers: %s: %w", strings.TrimSpace(string(output)), err)
	}
	var servers []struct {
		Name      string `json:"name"`
		Transport struct {
			Command string `json:"command"`
		} `json:"transport"`
	}
	if err := json.Unmarshal(output, &servers); err != nil {
		return false, fmt.Errorf("read Codex MCP server list: %w", err)
	}
	for _, server := range servers {
		if server.Name == tutorMCPName {
			prefix := filepath.Join(home, "english-guard", "versions") + string(filepath.Separator)
			if !strings.HasPrefix(filepath.Clean(server.Transport.Command), prefix) {
				return false, fmt.Errorf("Codex MCP server %q already belongs to another command", tutorMCPName)
			}
			return true, nil
		}
	}
	return false, nil
}

func registerTutorMCP(home, path string) (bool, error) {
	if _, err := exec.LookPath("codex"); err != nil {
		return false, nil // The standalone letter guard does not require the Codex CLI.
	}
	_, err := configuredTutorMCP(home)
	if err != nil {
		return false, err
	}
	codex, _ := exec.LookPath("codex")
	command := exec.Command(codex, "mcp", "add", tutorMCPName, "--env", "CODEX_HOME="+home, "--", path, "mcp")
	command.Env = append(os.Environ(), "CODEX_HOME="+home)
	output, err := command.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("register Tutor MCP server: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return true, nil
}

func removeTutorMCP(home string) error {
	owned, err := configuredTutorMCP(home)
	if err != nil || !owned {
		return err
	}
	codex, _ := exec.LookPath("codex")
	command := exec.Command(codex, "mcp", "remove", tutorMCPName)
	command.Env = append(os.Environ(), "CODEX_HOME="+home)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove Tutor MCP server: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func belongsToGuard(command, home string) bool {
	prefix := filepath.Join(home, "english-guard", "versions") + string(filepath.Separator)
	return strings.Contains(command, prefix)
}

func isLegacyGuard(command, home string) bool {
	return strings.Trim(command, `"'`) == filepath.Join(home, "english_gate")
}

func saveHookConfig(path string, root, hooks map[string]json.RawMessage) error {
	root["hooks"], _ = json.MarshalIndent(hooks, "  ", "  ")
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if original, err := os.ReadFile(path); err == nil {
		if string(original) == string(data) {
			return nil
		}
		backup := fmt.Sprintf("%s.english-guard.%d.bak", path, time.Now().UnixNano())
		if err := os.WriteFile(backup, original, 0600); err != nil {
			return fmt.Errorf("cannot back up hooks: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out, err := os.CreateTemp(filepath.Dir(path), ".hooks-*")
	if err != nil {
		return err
	}
	temporary := out.Name()
	defer os.Remove(temporary)
	if _, err := out.Write(data); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(0600); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func installGuard(replaceLegacy bool) error {
	home, err := codexHome()
	if err != nil {
		return err
	}
	config := filepath.Join(home, "hooks.json")
	root, hooks, err := loadHookConfig(config)
	if err != nil {
		return err
	}
	groups, err := filterPromptGroups(hooks["UserPromptSubmit"], home, replaceLegacy)
	if err != nil {
		return err
	}
	if err := stopTutorDashboard(home); err != nil {
		return fmt.Errorf("stop previous timing dashboard: %w", err)
	}
	path := installedPath(home)
	if err := copyExecutable(path); err != nil {
		return fmt.Errorf("copy executable: %w", err)
	}
	mcpAvailable, err := registerTutorMCP(home, path)
	if err != nil {
		return err
	}
	handler := map[string]any{
		"type": "command", "command": quoteCommand(path),
		"timeout": 35, "statusMessage": "Checking prompt",
	}
	if runtime.GOOS == "windows" {
		handler["commandWindows"] = quoteCommand(path)
	}
	group, _ := json.Marshal(map[string]any{"hooks": []any{handler}})
	groups = append(groups, group)
	if mcpAvailable {
		mcpHandler := map[string]any{
			"type": "mcp_tool", "server": tutorMCPName, "tool": "check_prompt",
			"input":   map[string]string{"session_id": "${session_id}", "prompt": "${prompt}"},
			"timeout": 660, "statusMessage": "Tutor: checking English at Low effort",
		}
		mcpGroup, _ := json.Marshal(map[string]any{"hooks": []any{mcpHandler}})
		groups = append(groups, mcpGroup)
	}
	hooks["UserPromptSubmit"], _ = json.Marshal(groups)
	// Remove review gates from the previous active-model design. Grammar now
	// runs in an independent Tutor chat before the main model receives the turn.
	for _, event := range []string{"PreToolUse", "Stop"} {
		gateGroups, err := filterPromptGroups(hooks[event], home, false)
		if err != nil {
			return fmt.Errorf("read %s hooks: %w", event, err)
		}
		if len(gateGroups) == 0 {
			delete(hooks, event)
		} else {
			hooks[event], _ = json.Marshal(gateGroups)
		}
	}
	if err := saveHookConfig(config, root, hooks); err != nil {
		return err
	}
	fmt.Println("Installed English Guard for new local Codex chats:", config)
	fmt.Println("Installed command:", path)
	if mcpAvailable {
		fmt.Println("Registered in-chat Tutor choices through Codex MCP.")
	} else {
		fmt.Println("Codex CLI was not found; Tutor choices are unavailable until it is installed and English Guard is reinstalled.")
	}
	fmt.Println("Review and trust the hook when Codex asks.")
	return nil
}

func uninstallGuard() error {
	home, err := codexHome()
	if err != nil {
		return err
	}
	config := filepath.Join(home, "hooks.json")
	root, hooks, err := loadHookConfig(config)
	if err != nil {
		return err
	}
	for _, event := range []string{"UserPromptSubmit", "PreToolUse", "Stop"} {
		groups, err := filterPromptGroups(hooks[event], home, false)
		if err != nil {
			return fmt.Errorf("read %s hooks: %w", event, err)
		}
		if len(groups) == 0 {
			delete(hooks, event)
		} else {
			hooks[event], _ = json.Marshal(groups)
		}
	}
	if err := saveHookConfig(config, root, hooks); err != nil {
		return err
	}
	if err := removeTutorMCP(home); err != nil {
		return err
	}
	if err := stopTutorDashboard(home); err != nil {
		return fmt.Errorf("stop Tutor dashboard: %w", err)
	}
	fmt.Println("Removed English Guard hook from:", config)
	fmt.Println("Installed binaries are retained so active chats can finish; remove the english-guard directory later if desired.")
	for _, path := range []string{tutorSettingsPath(home)} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove Tutor data: %w", err)
		}
	}
	if err := os.RemoveAll(tutorSessionsDir(home)); err != nil {
		return fmt.Errorf("remove Tutor sessions: %w", err)
	}
	if err := os.RemoveAll(tutorMetricsDir(home)); err != nil {
		return fmt.Errorf("remove Tutor timings: %w", err)
	}
	return nil
}
