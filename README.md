# Codex English Guard

A small local Codex hook that stops a prompt before the model sees it when the text contains letters outside the English alphabet. It allows numbers, punctuation, emoji, and math operators. For math use, it also allows Greek letters, mathematical alphanumeric letters, and letterlike symbols such as `α`, `𝑥`, and `ℝ`.

Optional, experimental **Tutor mode** checks English spelling and grammar in its own temporary Codex session, fixed at **Low** reasoning effort. It receives the submitted prompt without the coding chat's history. Your main chat keeps its model and effort setting. Tutor is off by default and uses the customer's existing Codex allowance. The character guard continues to work without Tutor.

The check runs when you press **Send** in a local Codex chat. It does not intercept network traffic or change your text. It shows the first rejected character and asks you to rewrite the prompt. With Tutor off, the executable makes no network requests.

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

## Distribution and OpenAI directory status

This project is distributed through [GitHub Releases](https://github.com/rebrudiy/codex-english-guard/releases). It is not listed in OpenAI's public plugin directory. OpenAI's [submission rules](https://developers.openai.com/plugins/deploy/submission) currently exclude plugin packages containing lifecycle hooks and direct hook-dependent projects to manual installation. Both the letter guard and automatic Tutor depend on `UserPromptSubmit` hooks. Removing them would remove automatic prompt checking.

OpenAI also restricts [local MCP submissions](https://developers.openai.com/plugins/deploy/desktop-only) to approved partners. A GitHub release does not register a plugin with OpenAI or bypass directory review.

The source and binaries are MIT licensed. The local character check needs no paid service. Optional GPT Tutor requests use your existing Codex account and its access and usage limits; the project provides no free inference allowance.

## Tutor mode

Tutor requires a current local Codex client with MCP tools and hooks, plus Codex CLI authentication. It reuses the existing CLI login; no separate Tutor account or API key is needed. Codex CLI 0.166.0 was used to verify the native choice mechanism.

Tutor starts off in every new chat. To enable it for one chat, send this exact message in that chat:

```text
Tutor on
```

Send `Tutor off` in that chat to disable it there. These two messages are case insensitive. Codex shows them as “Blocked by hook” because the command hook consumes the command and reports the result through its block message. Other chats stay off. Tutor activation is stored by Codex session ID and expires after 30 days. Installation also registers an `english-guard` MCP server and lifecycle hooks. If the CLI was missing during installation, install it and reinstall English Guard. Restart Codex after an update, then start a new chat so it loads the new hooks and MCP server.

From an extracted release archive or the installed executable, you can also run:

```sh
./english-guard tutor status
./english-guard tutor check 'I has a bug'
./english-guard tutor off
```

The CLI `tutor off` command disables Tutor in every chat and removes the old global setting. The CLI `tutor on` command only prints the in-chat activation instruction. Uninstall removes Tutor session data. Installing a newer version retains active chat sessions.

On each Send, the independent command hook enforces the letter rule. A separate MCP prompt hook checks whether Tutor is enabled for that chat. If enabled, it launches a clean, ephemeral GPT session fixed at Low effort with only a short correction instruction and the current prompt. User model settings and coding history are not loaded into that session. File tools and web search are disabled. GPT returns the complete corrected prompt directly; Tutor has no local grammar rules, edit-offset calculations, or technical-text filters. It opens a native **Use correction / Keep original** choice with that complete suggestion. Choosing either continues the main chat without another grammar check. If GPT returns the original prompt unchanged, it proceeds without a choice.

For example, in the same Codex chat:

```text
You: I has a bug.
Tutor choice: Suggested prompt: I have a bug.
You select: Use correction
Codex: [responds using the selected correction]
```

When you choose **Use correction**, the selected prompt reaches the main model as JSON-encoded user text in hook context; the original message remains visible in the chat. A native choice is supported through MCP elicitation in the tested Codex CLI; rendering in every desktop and IDE version has not been verified. Canceling a running choice blocks the prompt. If the MCP server or tool cannot run, Codex can skip that optional Tutor check; the independent letter guard remains active. Tutor supports prompts up to 20,000 bytes. The choice expires after ten minutes.

Tutor's effort is always Low, independently of whether the main chat uses Low, High, Xhigh, or another supported effort. It makes its own model request, so correction is not instantaneous and depends on service latency. Tutor does not change your main chat or global model settings.

Tutor can miss errors or suggest an imperfect correction. It cannot guarantee that a suggested edit preserves meaning. Review the complete suggested text before choosing. It makes no paid API-key request from the project owner.

The manual `tutor check` command uses the same isolated Low-effort grammar worker for diagnostics.

## Timing dashboard

Send `Tutor dashboard` in a Codex chat to open the local browser dashboard. This does not enable Tutor in that chat. You can also run `english-guard dashboard` from a terminal.

The dashboard updates every second and shows the last 100 checks: local preparation, process launch, Codex startup, the GPT request, result handling, and time waiting for a native choice. It also shows token counts when Codex reports them. Cached input is included in input tokens; it is not added again. Network, server queueing, and generation remain one combined stage because CLI events cannot measure them separately.

Timing records contain no prompt or correction text. They are stored under `CODEX_HOME/english-guard/metrics`, served only on localhost, and removed by `uninstall`. The dashboard stops after 30 minutes without authenticated activity. Opening it makes no GPT request.

The temporary grammar worker disables analytics upload and replaces the coding instruction template with a short grammar instruction. Measurements found that this removed a multi-second shutdown wait and reduced input-token overhead. These settings apply only to Tutor; the main Codex configuration is unchanged.

## Build and test

Go 1.24 or later is needed only to build from source. Release binaries need no Go installation.

```sh
go test ./...
go build -o english-guard .
```

To compare the current worker with smaller-model experiments without changing your settings:

```sh
python3 scripts/benchmark_tutor_latency.py --output-dir ./benchmark-results --dashboard
```

This optional developer benchmark makes up to six real GPT requests using your Codex allowance. It needs Python 3, a logged-in Codex CLI, and access to the tested models. Results are observations from two fixed prompts per profile, not a grammar-quality evaluation. It does not change Tutor's production model or effort. See [TEST_REPORT.md](TEST_REPORT.md) for measured results and client limitations, and [CHECKLIST.md](CHECKLIST.md) for remaining work.

The project is MIT licensed. Contributions and test cases for Unicode edge cases are welcome.
