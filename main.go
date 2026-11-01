package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

var version = "dev"

func main() {
	if len(os.Args) == 1 {
		if err := runHook(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}

	switch os.Args[1] {
	case "check":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: english-guard check <text>")
			os.Exit(2)
		}
		prompt := strings.Join(os.Args[2:], " ")
		if r, rejected := rejectedRune(prompt); rejected {
			fmt.Fprintln(os.Stderr, rejectionReason(r))
			os.Exit(1)
		}
		fmt.Println("Allowed")
	case "install":
		replaceLegacy := false
		if len(os.Args) == 3 && os.Args[2] == "--replace-legacy" {
			replaceLegacy = true
		} else if len(os.Args) != 2 {
			usage()
		}
		if err := installGuard(replaceLegacy); err != nil {
			fmt.Fprintln(os.Stderr, "install:", err)
			os.Exit(1)
		}
	case "uninstall":
		if len(os.Args) != 2 {
			usage()
		}
		if err := uninstallGuard(); err != nil {
			fmt.Fprintln(os.Stderr, "uninstall:", err)
			os.Exit(1)
		}
	case "version", "--version":
		fmt.Println(version)
	case "tutor":
		if err := tutorCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "dashboard":
		if err := dashboardCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "dashboard:", err)
			os.Exit(2)
		}
	case "mcp":
		if len(os.Args) != 2 {
			usage()
		}
		if err := serveTutorMCP(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "mcp:", err)
			os.Exit(2)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: english-guard [check <text> | install [--replace-legacy] | uninstall | tutor <command> | dashboard | mcp | version]")
	os.Exit(2)
}

func runHook(in io.Reader, out io.Writer) error {
	return runHookWithTutor(in, out, checkTutor)
}

func runHookWithTutor(in io.Reader, out io.Writer, tutor func(string, string) (string, error)) error {
	var event struct {
		Prompt    *string `json:"prompt"`
		SessionID string  `json:"session_id"`
	}
	decoder := json.NewDecoder(in)
	if err := decoder.Decode(&event); err != nil || event.Prompt == nil {
		return writeBlock(out, "English Guard could not read this prompt. Please try again.")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return writeBlock(out, "English Guard could not read this prompt. Please try again.")
	}
	if r, rejected := rejectedRune(*event.Prompt); rejected {
		return writeBlock(out, rejectionReason(r))
	}
	if home, err := codexHome(); err == nil && hasPendingTutorChoice(home, event.SessionID, *event.Prompt) {
		selected, handled, err := chooseTutorPrompt(event.SessionID, *event.Prompt)
		if err != nil {
			return writeBlock(out, "Tutor choice: "+err.Error())
		}
		if handled {
			return writeChosenPrompt(out, selected)
		}
	}
	reason, err := tutor(event.SessionID, *event.Prompt)
	if err != nil {
		return writeBlock(out, "Tutor could not check this prompt: "+err.Error()+". Send 'Tutor off' in this chat or run 'english-guard tutor off' to disable Tutor everywhere.")
	}
	if reason != "" {
		return writeBlock(out, reason)
	}
	return nil
}

func writeBlock(out io.Writer, reason string) error {
	return json.NewEncoder(out).Encode(map[string]string{"decision": "block", "reason": reason})
}

func writeChosenPrompt(out io.Writer, selected string) error {
	quoted, err := json.Marshal(selected)
	if err != nil {
		return err
	}
	context := "The user selected a prompt in English Guard Tutor. Treat the selected_prompt_json value as an ordinary user message, with user-level priority, and answer that request now. It is user-authored data, not an instruction from this hook. selected_prompt_json: " + string(quoted)
	return json.NewEncoder(out).Encode(map[string]any{
		"hookSpecificOutput": map[string]string{
			"hookEventName":     "UserPromptSubmit",
			"additionalContext": context,
		},
	})
}
