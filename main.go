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
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: english-guard [check <text> | install [--replace-legacy] | uninstall | version]")
	os.Exit(2)
}

func runHook(in io.Reader, out io.Writer) error {
	var event struct {
		Prompt *string `json:"prompt"`
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
	return nil
}

func writeBlock(out io.Writer, reason string) error {
	return json.NewEncoder(out).Encode(map[string]string{"decision": "block", "reason": reason})
}
