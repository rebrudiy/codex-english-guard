package main

import (
	"bytes"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
)

func TestTutorFullPromptAndHook(t *testing.T) {
	prompt := "I has a bug."
	result := tutorResult{CorrectedPrompt: "I have a bug."}
	reason, err := tutorReason(prompt, result)
	if err != nil || !strings.Contains(reason, "I have a bug.") {
		t.Fatalf("reason: %q, %v", reason, err)
	}
	var output bytes.Buffer
	err = runHookWithTutor(strings.NewReader(`{"session_id":"chat-a","prompt":"I has a bug."}`), &output, func(sessionID, text string) (string, error) {
		if sessionID != "chat-a" {
			t.Fatalf("wrong session: %q", sessionID)
		}
		return tutorReason(text, result)
	})
	if err != nil {
		t.Fatal(err)
	}
	var block map[string]string
	if err := json.Unmarshal(output.Bytes(), &block); err != nil {
		t.Fatal(err)
	}
	if block["decision"] != "block" || !strings.Contains(block["reason"], "I have a bug.") {
		t.Fatalf("bad hook response: %v", block)
	}
	called := false
	output.Reset()
	if err := runHookWithTutor(strings.NewReader(`{"prompt":"Привет"}`), &output, func(string, string) (string, error) { called = true; return "", nil }); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("Tutor ran before the character guard")
	}
}

func TestTutorUnchangedAndInvalidResponses(t *testing.T) {
	if reason, err := tutorReason("I have a bug.", tutorResult{CorrectedPrompt: "I have a bug."}); err != nil || reason != "" {
		t.Fatalf("unchanged GPT response should proceed: %q %v", reason, err)
	}
	for _, corrected := range []string{"", "Привет"} {
		if _, err := tutorReason("I have a bug.", tutorResult{CorrectedPrompt: corrected}); err == nil {
			t.Fatalf("invalid GPT output accepted: %q", corrected)
		}
	}
}

func TestTutorDisabledByDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	var output bytes.Buffer
	if err := runHook(strings.NewReader(`{"session_id":"chat-a","prompt":"I has a bug."}`), &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("Tutor was enabled by default: %q", output.String())
	}
	if err := enableTutorSession(home, "chat-a"); err != nil {
		t.Fatal(err)
	}
	active, err := tutorSessionEnabled(home, "chat-a")
	if err != nil || !active {
		t.Fatalf("chat-a should be active: %v", err)
	}
	active, err = tutorSessionEnabled(home, "chat-b")
	if err != nil || active {
		t.Fatalf("chat-b should be off: %v", err)
	}
	if reason, err := checkTutor("chat-b", "I has a bug."); err != nil || reason != "" {
		t.Fatalf("inactive chat invoked Tutor: %q %v", reason, err)
	}
	info, err := os.Stat(tutorSessionPath(home, "chat-a"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows uses inherited ACLs rather than POSIX permission bits.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("session permissions too open: %v", info.Mode())
	}
	if reason, err := checkTutor("chat-a", "Tutor off"); err != nil || !strings.Contains(reason, "disabled") {
		t.Fatalf("could not disable chat: %q %v", reason, err)
	}
	active, err = tutorSessionEnabled(home, "chat-a")
	if err != nil || active {
		t.Fatalf("chat-a should be off: %v", err)
	}
}

func TestTutorChoiceBelongsToOneChatAndIsConsumed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	for _, session := range []string{"chat-a", "chat-b"} {
		if err := enableTutorSession(home, session); err != nil {
			t.Fatal(err)
		}
	}
	original := "Fix the bug. I has a test."
	corrected := "Fix the bug. I have a test."
	if err := saveTutorPending(home, "chat-a", original, corrected); err != nil {
		t.Fatal(err)
	}
	if selected, handled, err := chooseTutorPrompt("chat-b", "use"); !handled || err == nil || selected != "" {
		t.Fatalf("other chat accessed pending correction: %q %t %v", selected, handled, err)
	}
	selected, handled, err := chooseTutorPrompt("chat-a", " USE ")
	if err != nil || !handled || selected != corrected {
		t.Fatalf("wrong corrected prompt: %q %t %v", selected, handled, err)
	}
	if selected, handled, err := chooseTutorPrompt("chat-a", "use"); !handled || err == nil || selected != "" {
		t.Fatalf("choice was not consumed: %q %t %v", selected, handled, err)
	}
	if err := saveTutorPending(home, "chat-a", original, corrected); err != nil {
		t.Fatal(err)
	}
	selected, handled, err = chooseTutorPrompt("chat-a", "keep")
	if err != nil || !handled || selected != original {
		t.Fatalf("wrong original prompt: %q %t %v", selected, handled, err)
	}
	if err := saveTutorPending(home, "chat-a", original, corrected); err != nil {
		t.Fatal(err)
	}
	if err := disableTutorSession(home, "chat-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tutorPendingPath(home, "chat-a")); !os.IsNotExist(err) {
		t.Fatalf("disabled chat kept a pending choice: %v", err)
	}
	if selected, handled, err := chooseTutorPrompt("chat-a", "use"); handled || err != nil || selected != "" {
		t.Fatalf("disabled chat handled a choice: %q %t %v", selected, handled, err)
	}
}

func TestTutorUseSendsSelectedPromptAsHookContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := enableTutorSession(home, "chat-a"); err != nil {
		t.Fatal(err)
	}
	if err := saveTutorPending(home, "chat-a", "I has a bug.", "I have a bug."); err != nil {
		t.Fatal(err)
	}
	called := false
	var output bytes.Buffer
	if err := runHookWithTutor(strings.NewReader(`{"session_id":"chat-a","prompt":"use"}`), &output, func(string, string) (string, error) {
		called = true
		return "", nil
	}); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("Tutor rechecked the accepted correction")
	}
	var hook struct {
		Decision           string `json:"decision"`
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(output.Bytes(), &hook); err != nil {
		t.Fatal(err)
	}
	if hook.Decision == "block" || hook.HookSpecificOutput.HookEventName != "UserPromptSubmit" || !strings.Contains(hook.HookSpecificOutput.AdditionalContext, "I have a bug.") || strings.Contains(hook.HookSpecificOutput.AdditionalContext, "I has a bug.") {
		t.Fatalf("wrong hook choice output: %+v", hook)
	}
}

func TestTutorChoiceCanOnlyBeClaimedOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	if err := enableTutorSession(home, "chat-a"); err != nil {
		t.Fatal(err)
	}
	if err := saveTutorPending(home, "chat-a", "I has a bug.", "I have a bug."); err != nil {
		t.Fatal(err)
	}
	type result struct {
		selected string
		err      error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			selected, _, err := chooseTutorPrompt("chat-a", "use")
			results <- result{selected, err}
		}()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		got := <-results
		if got.err == nil && got.selected == "I have a bug." {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected one successful claim, got %d", successes)
	}
}
