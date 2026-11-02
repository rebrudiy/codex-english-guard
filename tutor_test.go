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
	result := tutorResult{
		GrammarErrors:   []tutorIssue{{Original: "I has", Correction: "I have", Explanation: "Use have with I."}},
		CorrectedPrompt: "I have a bug.",
		Meaning:         "You are reporting a bug.",
	}
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

func TestTutorReportExplainsErrorsInOrder(t *testing.T) {
	result := tutorResult{
		SpellingErrors:  []tutorIssue{{Original: "surnanes", Correction: "surnames", Explanation: "Surnames is the correct spelling."}},
		GrammarErrors:   []tutorIssue{{Original: "Can you lists", Correction: "Can you list", Explanation: "Use the base form list after can."}},
		WordChoice:      []tutorIssue{{Original: "live in the image", Correction: "appear in the image", Explanation: "Appear describes being shown in an image."}},
		CorrectedPrompt: "Can you list the surnames of people who appear in the image?",
		Meaning:         "You want the surnames of the people shown in the image.",
	}
	reason, err := tutorReason("Can you lists the surnanes of people who live in the image?", result)
	if err != nil {
		t.Fatal(err)
	}
	previous := -1
	for _, text := range []string{
		"1. Spelling errors", `"surnanes" → "surnames"`, "Surnames is the correct spelling.",
		"2. Grammar errors", "Use the base form list after can.",
		"3. Word choice", "Appear describes being shown in an image.",
		"4. Corrected version", result.CorrectedPrompt, "Meaning", result.Meaning,
	} {
		index := strings.Index(reason, text)
		if index <= previous {
			t.Fatalf("missing or misplaced %q in report:\n%s", text, reason)
		}
		previous = index
	}
}

func TestTutorReportEmptyCategoriesAndDeletion(t *testing.T) {
	result := tutorResult{
		GrammarErrors:   []tutorIssue{{Original: "also", Correction: "", Explanation: "Also repeats the meaning of too here."}},
		CorrectedPrompt: "Check this too.",
		Meaning:         "You want this checked as well.",
	}
	reason, err := tutorReason("Check this also too.", result)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"1. Spelling errors\nNone found.", "3. Word choice\nNone found.", `"also" → (remove)`} {
		if !strings.Contains(reason, text) {
			t.Fatalf("missing %q in report:\n%s", text, reason)
		}
	}
}

func TestTutorRejectsIncompleteExplanations(t *testing.T) {
	for _, result := range []tutorResult{
		{CorrectedPrompt: "I have a bug."},
		{CorrectedPrompt: "I have a bug.", Meaning: "You are reporting a bug."},
		{CorrectedPrompt: "I have a bug.", Meaning: "You are reporting a bug.", GrammarErrors: []tutorIssue{{Original: "I has", Correction: "I have"}}},
		{CorrectedPrompt: "I have a bug.", Meaning: "You are reporting a bug.", GrammarErrors: []tutorIssue{{Correction: "I have", Explanation: "Use have with I."}}},
	} {
		if _, err := tutorReason("I has a bug.", result); err == nil {
			t.Fatalf("incomplete explanation accepted: %+v", result)
		}
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
