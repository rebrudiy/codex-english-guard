# English Guard progress checklist

## Timing dashboard

- [x] Record actual CLI startup/request/result events and reported token usage.
- [x] Separate time waiting for a native correction choice from the grammar request.
- [x] Build a live localhost dashboard with selectable recent checks.
- [x] Open it from `Tutor dashboard` inside a Codex chat.
- [x] Store timing metadata without prompt/correction text and remove it on uninstall.
- [x] Fix background launch survival and verify real browser rendering.
- [x] Remove the measured analytics shutdown tail in the temporary worker.
- [x] Reduce coding-instruction token overhead with a grammar-only base instruction.
- [x] Install and verify the final dashboard build, including real browser rendering with measured records and token counts.

## Independent Tutor change

Tutor uses a separate temporary Codex grammar session fixed at Low effort. Each check receives only the current prompt and grammar instructions. The main coding chat keeps its own model, reasoning effort, and history.

- [x] Remove the active-chat effort dependency and obsolete tool/completion gates.
- [x] Explicitly set Tutor effort to Low, ignoring user model settings and project instructions.
- [x] Keep Tutor activation local to the chat where it was enabled; new chats start off.
- [x] Preserve native Use correction / Keep original choices and cancellation.
- [x] Keep the independent English letter guard.
- [x] Use GPT's complete corrected prompt directly; remove local grammar filters and byte-offset edit processing.
- [x] Disable file tools, web search, and recursive hooks in the grammar worker.
- [x] Pass tests, race tests, vet, and Windows/Linux cross-builds.
- [x] Verify live main-chat Xhigh versus worker Low, empty worker stdin, and absence of earlier main-chat history from the worker prompt.
- [x] Install and verify the updated local binary, independent MCP prompt hook, removal of old gates, and unchanged main Xhigh setting.

## Remaining validation and product limits

- [ ] Verify native choice rendering in the desktop app and IDE extension.
- [x] Measure startup, request, and shutdown stages and compare three model/effort profiles on two fixed prompts each.
- [ ] Assess latency and grammar quality across a larger realistic prompt set; immediate GPT correction is not guaranteed.
- [ ] Decide whether hook context is acceptable for the selected correction. The original message remains visible.
- [ ] Address optional MCP runtime failures: Codex may skip Tutor if the server/tool is unavailable. The command letter guard remains active.
- [ ] Assess grammar quality and meaning preservation with more real examples. Full correction review and Keep original remain necessary.
- [ ] Surface exact agent token usage only where the Codex client exposes it; the collaboration tools here provide status and results without those counts.

## Release

- [x] Prepare v0.2.0 release notes, installation documentation, and explicit experimental Tutor limitations.
- [x] Confirm GitHub publishing access and the existing six-platform release workflow.
- [x] Publish [v0.2.0 on GitHub](https://github.com/rebrudiy/codex-english-guard/releases/tag/v0.2.0) after native CI passes for all six targets.
- [x] Download all six published archives and verify their checksums and contents; verify repeat installation, per-chat activation, and removal using the published macOS ARM binary.
- [ ] Verify end-user release installation and removal on Windows and Linux beyond native automated tests.

## OpenAI public directory

- [x] Check the current official submission and desktop-only requirements.
- [ ] Publish the automatic guard in the public directory: currently unsupported. OpenAI excludes lifecycle-hook packages; local MCP submissions also require approved-partner access. See the distribution section in [README.md](README.md).

See [TEST_REPORT.md](TEST_REPORT.md) for validation evidence and the current independent Low-effort design. The GitHub release page and Actions run are the source of truth for publication and native CI status.
