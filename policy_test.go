package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPolicy(t *testing.T) {
	tests := []struct {
		name, prompt string
		blocked      bool
	}{
		{"plain English", "Fix the login bug", false},
		{"symbols and emoji", "Solve x² + y² ≤ 10 ∑ → ∞ 😊", false},
		{"Greek math", "Use α and Δ in this equation", false},
		{"styled math and letterlike", "Compare 𝑥 with ℝ", false},
		{"Cyrillic", "Исправь ошибку", true},
		{"mixed Cyrillic", "Fix привет in this file", true},
		{"accented Latin", "Please fix café", true},
		{"decomposed accent", "Please fix cafe\u0301", true},
		{"CJK", "修复错误", true},
		{"Arabic", "مرحبا", true},
		{"Greek extended", "ἀ", true},
		{"code is checked", "```go\n// привет\n```", true},
		{"path is checked", "Open src/файл.go", true},
		{"ASCII non-English is allowed", "Hola, como estas?", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, got := rejectedRune(test.prompt)
			if got != test.blocked {
				t.Fatalf("rejectedRune(%q) blocked=%v, want %v", test.prompt, got, test.blocked)
			}
		})
	}
}

func TestHookInputOutput(t *testing.T) {
	for _, test := range []struct {
		input   string
		blocked bool
	}{
		{`{"prompt":"Fix the bug","session_id":"example"}`, false},
		{`{"prompt":"Привет"}`, true},
		{`{"prompt":null}`, true},
		{`{"other":"field"}`, true},
		{`not json`, true},
		{`{"prompt":"Fix it"} {"prompt":"Привет"}`, true},
	} {
		var output bytes.Buffer
		if err := runHook(strings.NewReader(test.input), &output); err != nil {
			t.Fatal(err)
		}
		if !test.blocked && output.Len() != 0 {
			t.Fatalf("allowed prompt wrote %q", output.String())
		}
		if test.blocked {
			var decision map[string]string
			if err := json.Unmarshal(output.Bytes(), &decision); err != nil {
				t.Fatal(err)
			}
			if decision["decision"] != "block" || decision["reason"] == "" {
				t.Fatalf("bad block response: %v", decision)
			}
		}
	}
}
