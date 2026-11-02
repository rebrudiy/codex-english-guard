# Changelog

## v0.3.0

- Show Tutor feedback in order: spelling errors, grammar errors with simple explanations, word choice, the full corrected version, and a short meaning summary.
- Show empty categories explicitly and keep teaching feedback out of the prompt sent to the main chat.
- Keep the independent worker at Low effort in a fresh temporary session for each check. The GPT model remains the Codex CLI default; no model is pinned.

## v0.2.0

- Add experimental GPT Tutor with a separate Low-effort grammar session and complete corrected prompts.
- Enable Tutor per chat using `Tutor on`; new chats start with Tutor off. Use `Tutor off` in chat to disable it there.
- Add native **Use correction / Keep original** choices through MCP, verified with Codex CLI 0.166.0. The original visible message remains in the transcript.
- Add a local timing dashboard opened with `Tutor dashboard`, including request stages, reported token usage, and correction-choice wait.
- Reduce worker instruction overhead and remove the measured analytics shutdown delay without changing the main chat's settings.
- Preserve the independent English-letter guard, unrelated hooks, and existing Codex login. Remove obsolete Tutor gates during upgrade.
- Add a bounded developer latency benchmark, current test evidence, and distribution documentation.

Tutor consumes the user's Codex allowance and can take several seconds. Native desktop and IDE choices remain unverified. An unavailable MCP hook can skip Tutor; the independent letter guard remains active. See [TEST_REPORT.md](TEST_REPORT.md) for evidence and limitations.

Distribute this release through GitHub. OpenAI's public directory currently excludes lifecycle-hook packages, so this release has no OpenAI marketplace listing.

## v0.1.0

- Initial English-letter guard with Unicode math exceptions, cross-platform installation, and six release targets.
