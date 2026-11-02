package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexTutorDisablesCommandAndSearchTools(t *testing.T) {
	temporary := t.TempDir()
	stub := filepath.Join(temporary, "codex")
	if runtime.GOOS == "windows" {
		stub += ".exe"
	}
	source := filepath.Join(temporary, "stub.go")
	// Use a real executable so this tests the isolated CLI, output-schema,
	// and result-file flow on every supported operating system without an account.
	const program = `package main
import (
    "encoding/json"
    "os"
    "strings"
)
func main() {
    if len(os.Args) < 2 || os.Args[1] != "exec" { os.Exit(2) }
    args, _ := json.Marshal(os.Args[1:])
    if err := os.WriteFile(os.Getenv("ENGLISH_GUARD_TEST_ARGS"), args, 0600); err != nil { panic(err) }
    schema, output := "", ""
    for i := 2; i+1 < len(os.Args); i++ {
        if os.Args[i] == "--output-schema" { schema = os.Args[i+1] }
        if os.Args[i] == "-o" { output = os.Args[i+1] }
        if os.Args[i] == "-c" && strings.HasPrefix(os.Args[i+1], "model_instructions_file=") {
            var path string
            if err := json.Unmarshal([]byte(strings.TrimPrefix(os.Args[i+1], "model_instructions_file=")), &path); err != nil { panic(err) }
            instructions, err := os.ReadFile(path)
            if err != nil { panic(err) }
            if err := os.WriteFile(os.Getenv("ENGLISH_GUARD_TEST_INSTRUCTIONS"), instructions, 0600); err != nil { panic(err) }
        }
    }
    data, err := os.ReadFile(schema)
    if err != nil || !json.Valid(data) { os.Exit(3) }
    if err := os.WriteFile(output, []byte(os.Getenv("ENGLISH_GUARD_TEST_RESULT")), 0600); err != nil { panic(err) }
}
`
	if err := os.WriteFile(source, []byte(program), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("go", "build", "-o", stub, source).CombinedOutput(); err != nil {
		t.Fatalf("build stub CLI: %v\n%s", err, output)
	}
	argsPath := filepath.Join(temporary, "args.json")
	instructionsPath := filepath.Join(temporary, "instructions-captured.txt")
	home := filepath.Join(temporary, "user-codex-home")
	if err := os.MkdirAll(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("model_reasoning_effort = \"xhigh\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", home)
	t.Setenv("PATH", temporary+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ENGLISH_GUARD_TEST_ARGS", argsPath)
	t.Setenv("ENGLISH_GUARD_TEST_INSTRUCTIONS", instructionsPath)
	const validResult = `{"spelling_errors":[],"grammar_errors":[{"original":"I has","correction":"I have","explanation":"Use have with I."}],"word_choice":[],"corrected_prompt":"I have a bug.","meaning":"You are reporting a bug."}`
	t.Setenv("ENGLISH_GUARD_TEST_RESULT", validResult)
	result, err := codexTutor("I has a bug.")
	if err != nil {
		t.Fatal(err)
	}
	if result.CorrectedPrompt != "I have a bug." {
		t.Fatalf("GPT result changed: %q", result.CorrectedPrompt)
	}
	if len(result.GrammarErrors) != 1 || result.GrammarErrors[0].Explanation != "Use have with I." || result.Meaning != "You are reporting a bug." {
		t.Fatalf("GPT explanations were not preserved: %+v", result)
	}
	data, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	var args []string
	if err := json.Unmarshal(data, &args); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{
		{"--disable", "hooks"},
		{"--disable", "shell_tool"},
		{"--disable", "shell_snapshot"},
		{"--sandbox", "read-only"},
		{"-c", `approval_policy="never"`},
		{"-c", `web_search="disabled"`},
		{"-c", `model_reasoning_effort="low"`},
		{"-c", `project_doc_max_bytes=0`},
		{"-c", `analytics.enabled=false`},
	} {
		found := false
		for i := 0; i+1 < len(args); i++ {
			if args[i] == pair[0] && args[i+1] == pair[1] {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("grammar subprocess omitted %q %q", pair[0], pair[1])
		}
	}
	for _, flag := range []string{"--ignore-user-config", "--ignore-rules", "--ephemeral"} {
		if !strings.Contains(strings.Join(args, "\n"), flag) {
			t.Errorf("grammar subprocess omitted %s", flag)
		}
	}
	var workdir string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "-C" {
			workdir = args[i+1]
		}
	}
	if workdir == "" || workdir == home || !strings.HasPrefix(filepath.Base(workdir), "english-guard-tutor-") {
		t.Fatalf("grammar worker did not get a fresh isolated cwd: %q", workdir)
	}
	var promptData map[string]string
	if json.Unmarshal([]byte(args[len(args)-1]), &promptData) != nil || len(promptData) != 1 || promptData["prompt"] != "I has a bug." {
		t.Fatalf("worker did not receive only current prompt data: %q", args[len(args)-1])
	}
	instructions, err := os.ReadFile(instructionsPath)
	if err != nil {
		t.Fatal("grammar-only base instructions were not supplied:", err)
	}
	for _, phrase := range []string{"spelling and grammar", "word choice", "simple English", "Return the full corrected prompt", "meaning", "never invent mistakes", "Preserve names, code, commands, paths, and technical terms"} {
		if !strings.Contains(string(instructions), phrase) {
			t.Errorf("worker instruction omitted %q", phrase)
		}
	}
	if strings.Contains(string(instructions), "BYTE") || strings.Contains(tutorSchema, "edits") {
		t.Fatal("GPT was asked to calculate edit offsets instead of returning the full prompt")
	}
	for _, field := range []string{"spelling_errors", "grammar_errors", "word_choice", "corrected_prompt", "meaning"} {
		for _, variant := range []string{"missing", "null", "wrong type"} {
			t.Run(field+"/"+variant, func(t *testing.T) {
				var response map[string]any
				if err := json.Unmarshal([]byte(validResult), &response); err != nil {
					t.Fatal(err)
				}
				switch variant {
				case "missing":
					delete(response, field)
				case "null":
					response[field] = nil
				case "wrong type":
					response[field] = 42
				}
				data, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("ENGLISH_GUARD_TEST_RESULT", string(data))
				if _, err := codexTutor("I has a bug."); err == nil {
					t.Fatalf("invalid %s field accepted (%s)", field, variant)
				}
			})
		}
	}
}
