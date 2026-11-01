package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

type tutorResult struct {
	CorrectedPrompt string `json:"corrected_prompt"`
	trace           *tutorTrace
}

type pendingTutorPrompt struct {
	Original  string `json:"original"`
	Corrected string `json:"corrected"`
}

func tutorSettingsPath(home string) string { return filepath.Join(home, "english-guard", "tutor.json") } // Legacy global setting, removed on upgrade.
func tutorSessionsDir(home string) string {
	return filepath.Join(home, "english-guard", "tutor-sessions")
}
func tutorSessionPath(home, sessionID string) string {
	hash := sha256.Sum256([]byte(sessionID))
	return filepath.Join(tutorSessionsDir(home), hex.EncodeToString(hash[:]))
}

func tutorPendingPath(home, sessionID string) string {
	return tutorSessionPath(home, sessionID) + ".pending"
}

func hasPendingTutorChoice(home, sessionID, prompt string) bool {
	choice := strings.ToLower(strings.TrimSpace(prompt))
	if sessionID == "" || (choice != "use" && choice != "keep") {
		return false
	}
	_, err := os.Stat(tutorPendingPath(home, sessionID))
	return err == nil
}

func saveTutorPending(home, sessionID, original, corrected string) error {
	if sessionID == "" {
		return errors.New("Codex did not supply a session ID")
	}
	if err := os.MkdirAll(tutorSessionsDir(home), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(pendingTutorPrompt{Original: original, Corrected: corrected})
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(tutorSessionsDir(home), ".pending-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), tutorPendingPath(home, sessionID))
}

func clearTutorPending(home, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	err := os.Remove(tutorPendingPath(home, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func chooseTutorPrompt(sessionID, choice string) (string, bool, error) {
	choice = strings.ToLower(strings.TrimSpace(choice))
	if choice != "use" && choice != "keep" {
		return "", false, nil
	}
	home, err := codexHome()
	if err != nil {
		return "", true, err
	}
	enabled, err := tutorSessionEnabled(home, sessionID)
	if err != nil || !enabled {
		return "", false, err
	}
	path := tutorPendingPath(home, sessionID)
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", true, err
	}
	claimed := path + ".claim-" + hex.EncodeToString(nonce[:])
	err = os.Rename(path, claimed)
	if errors.Is(err, os.ErrNotExist) {
		return "", true, errors.New("no Tutor correction is waiting; send a prompt first")
	}
	if err != nil {
		return "", true, err
	}
	defer os.Remove(claimed)
	info, err := os.Stat(claimed)
	if err != nil {
		return "", true, err
	}
	if time.Since(info.ModTime()) > 30*time.Minute {
		return "", true, errors.New("the Tutor correction expired; send the prompt again")
	}
	data, err := os.ReadFile(claimed)
	if err != nil {
		return "", true, err
	}
	var pending pendingTutorPrompt
	if err := json.Unmarshal(data, &pending); err != nil {
		return "", true, err
	}
	selected := pending.Corrected
	if choice == "keep" {
		selected = pending.Original
	}
	if r, rejected := rejectedRune(selected); rejected {
		return "", true, errors.New(rejectionReason(r))
	}
	return selected, true, nil
}

func tutorSessionEnabled(home, sessionID string) (bool, error) {
	if sessionID == "" {
		return false, nil
	}
	info, err := os.Stat(tutorSessionPath(home, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return time.Since(info.ModTime()) < 30*24*time.Hour, nil
}

func enableTutorSession(home, sessionID string) error {
	if sessionID == "" {
		return errors.New("Codex did not supply a session ID")
	}
	if err := os.MkdirAll(tutorSessionsDir(home), 0700); err != nil {
		return err
	}
	if err := clearTutorPending(home, sessionID); err != nil {
		return err
	}
	return os.WriteFile(tutorSessionPath(home, sessionID), nil, 0600)
}

func disableTutorSession(home, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if err := clearTutorPending(home, sessionID); err != nil {
		return err
	}
	err := os.Remove(tutorSessionPath(home, sessionID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func tutorReason(prompt string, result tutorResult) (string, error) {
	corrected := result.CorrectedPrompt
	if len(corrected) > 20000 || !utf8.ValidString(corrected) || (strings.TrimSpace(corrected) == "" && strings.TrimSpace(prompt) != "") {
		return "", errors.New("Tutor returned an empty or invalid prompt")
	}
	if r, rejected := rejectedRune(corrected); rejected {
		return "", errors.New(rejectionReason(r))
	}
	if corrected == prompt {
		return "", nil
	}
	return "Suggested prompt:\n" + corrected + "\nChoose whether Codex should use this correction or your original prompt.", nil
}

func checkTutor(sessionID, prompt string) (string, error) {
	home, err := codexHome()
	if err != nil {
		return "", err
	}
	switch strings.ToLower(strings.TrimSpace(prompt)) {
	case "tutor dashboard":
		url, err := launchTutorDashboard()
		if err != nil {
			return "", err
		}
		return "Tutor timing dashboard: " + url, nil
	case "tutor on":
		available, err := configuredTutorMCP(home)
		if err != nil {
			return "", err
		}
		if !available {
			return "", errors.New("Tutor's in-chat choice is unavailable; install English Guard with Codex CLI support")
		}
		if err := enableTutorSession(home, sessionID); err != nil {
			return "", err
		}
		return "Tutor enabled for this chat. Send 'Tutor off' to disable it here.", nil
	case "tutor off":
		if err := disableTutorSession(home, sessionID); err != nil {
			return "", err
		}
		return "Tutor disabled for this chat.", nil
	}
	// The MCP hook runs an isolated, low-effort grammar check. The command hook
	// stays independent so the letter rule still runs if MCP fails.
	return "", nil
}

func tutorCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: english-guard tutor on|off|status|check <text>")
	}
	home, err := codexHome()
	if err != nil {
		return err
	}
	switch args[0] {
	case "dashboard":
		return dashboardCommand(args[1:])
	case "on":
		if len(args) != 1 {
			return errors.New("usage: english-guard tutor on")
		}
		fmt.Println("To enable Tutor in one chat, send: Tutor on")
	case "off":
		if len(args) != 1 {
			return errors.New("usage: english-guard tutor off")
		}
		if err := os.RemoveAll(tutorSessionsDir(home)); err != nil {
			return err
		}
		if err := os.Remove(tutorSettingsPath(home)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Println("Tutor disabled in all chats. Letter checks remain active.")
	case "status":
		if len(args) != 1 {
			return errors.New("usage: english-guard tutor status")
		}
		fmt.Println("Tutor: off in new chats; enable inside one chat with 'Tutor on'")
		if err := requireCodexLogin(); err == nil {
			fmt.Println("Codex: logged in")
		} else {
			fmt.Println("Codex: unavailable or not logged in")
		}
	case "check":
		if len(args) < 2 {
			return errors.New("usage: english-guard tutor check <text>")
		}
		prompt := strings.Join(args[1:], " ")
		if r, rejected := rejectedRune(prompt); rejected {
			return errors.New(rejectionReason(r))
		}
		result, err := codexTutor(prompt)
		if err != nil {
			return err
		}
		reason, err := tutorReason(prompt, result)
		if err != nil {
			return err
		}
		if reason == "" {
			fmt.Println("No clear errors found.")
		} else {
			fmt.Println(reason)
		}
	default:
		return errors.New("usage: english-guard tutor on|off|status|check <text>")
	}
	return nil
}
