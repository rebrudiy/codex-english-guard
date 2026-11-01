# Codex English Guard

A small local Codex hook that stops a prompt before the model sees it when the text contains letters outside the English alphabet. It allows numbers, punctuation, emoji, and math operators. For math use, it also allows Greek letters, mathematical alphanumeric letters, and letterlike symbols such as `α`, `𝑥`, and `ℝ`.

The check runs when you press **Send** in a local Codex chat. It does not intercept network traffic or change your text. It shows the first rejected character and asks you to rewrite the prompt. The executable makes no network requests.

## Rule

The guard checks the entire text of each prompt, including code blocks, quotes, and file paths. It normalizes Unicode to NFC, then allows:

- English letters `A-Z` and `a-z`
- all Unicode numbers, punctuation, symbols, emoji, and whitespace
- Greek and Coptic block letters (U+0370–U+03FF)
- Mathematical Alphanumeric Symbols (U+1D400–U+1D7FF)
- Letterlike Symbols (U+2100–U+214F)

It rejects other Unicode letters. An accent combined with an English letter is rejected, including decomposed accents that NFC cannot compose.

Examples:

| Prompt | Result |
| --- | --- |
| `Fix the login bug` | Allowed |
| `Solve x² + y² ≤ 10; use α and 𝑥` | Allowed |
| `Исправь ошибку` | Blocked |
| `Please fix café` | Blocked |
| `Open src/файл.go` | Blocked |

This is a writing habit tool, not a language detector or security boundary. Greek prose and Russian written with English letters can pass. It checks the prompt's text field only; it does not read text inside images, audio, or attachments.

## Install

Download the archive for your operating system and CPU from the [latest GitHub Release](https://github.com/rebrudiy/codex-english-guard/releases/latest). Verify its SHA-256 hash against `checksums.txt`, extract it, then run:

```sh
./english-guard install
```

On Windows, run `english-guard.exe install` in PowerShell. Installation copies the executable into your Codex home directory and adds one user-level `UserPromptSubmit` hook to `hooks.json`. Existing hooks are preserved, and an existing `hooks.json` is backed up before changes. The hook applies to new local Codex chats in every project. Review and trust it when Codex asks; in the CLI, use `/hooks` to inspect it.

For a shorter install, inspect and run [scripts/install.sh](scripts/install.sh) on macOS/Linux or [scripts/install.ps1](scripts/install.ps1) on Windows. These scripts download the latest release and verify its checksum before running `install`.

If you are replacing the earlier Swift Cyrillic guard installed at `~/.codex/english_gate`, run `install --replace-legacy`. This removes that exact hook entry while keeping its executable and the backed-up config available for rollback. Test the new hook in Codex before deleting the old executable.

`CODEX_HOME` overrides the default Codex directory (`~/.codex` on macOS/Linux, `%USERPROFILE%\.codex` on Windows).

## Use, update, remove

From an extracted release archive (or a local build):

```sh
./english-guard check 'Fix x² + y² using α'
./english-guard check 'Исправь ошибку'
./english-guard version
```

On Windows, use `./english-guard.exe` in PowerShell. The installer does not add the executable to your `PATH`; keep the downloaded copy for manual commands or use the installed path shown in `hooks.json`.

`check` exits with code 0 for allowed text, 1 for a rejected letter, and 2 for incorrect usage.

To update, download a newer release and run its `install` command again. Each version lives in a separate directory so an active chat can finish using the old executable, including on Windows. To disable the hook, run `uninstall` from any downloaded or installed copy. Uninstall removes only this project's hook entry and leaves binaries in place until you remove them after active chats finish.

The hook works with local Codex in the desktop app, CLI, and IDE extension. It does not cover cloud tasks or ChatGPT web/mobile chats. Codex must load and trust the hook for it to run.

## Build and test

Go 1.24 or later is needed only to build from source. Release binaries need no Go installation.

```sh
go test ./...
go build -o english-guard .
```

The project is MIT licensed. Contributions and test cases for Unicode edge cases are welcome.
