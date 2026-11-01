# English Guard v0.2.0 validation

Local validation on 2026-11-01 used macOS and Codex CLI 0.166.0. Tutor is experimental. Native desktop and IDE correction choices remain unverified.

## Automated and local checks

- Go unit tests, race tests, `go vet`, and `git diff --check` pass.
- Tests cover the Unicode policy, per-chat activation, complete GPT responses, unchanged prompts, invalid results, MCP selection and cancellation, and obsolete-hook migration.
- Installer tests preserve unrelated hooks, back up existing configuration, and remove this project's hooks, MCP registration, session state, timing data, and dashboard state on uninstall.
- Worker tests verify explicit Low effort despite an Xhigh user setting, fresh working directories, grammar-only instructions, and disabled hooks, file tools, and web search.
- Timing tests cover partial events, missing usage, privacy, native-choice wait, authenticated localhost access, background process detachment, and cleanup.
- Native Go tests passed on macOS, Linux, and Windows, each on amd64 and arm64, before [the release workflow](https://github.com/rebrudiy/codex-english-guard/actions/runs/41343267309) published v0.2.0. Local Windows and Linux cross-builds also passed.
- All six published archives were downloaded, SHA-256 checked against the published manifest, and inspected for their expected executable. The published macOS ARM binary passed isolated install/update/removal, per-chat activation, letters/math, backup, and unrelated-configuration preservation checks, including a Codex home path containing spaces.
- The local dashboard was rendered in Chrome against actual timing records and token counts. Timing records contain no prompt or correction text.

## Live Codex behavior

An isolated Codex CLI chat enabled Tutor for that chat, produced the complete correction `I have a bug.`, opened the native choice, and continued the same turn after **Use correction**. MCP protocol tests also cover **Keep original** and cancellation. A separate new chat had Tutor off. Sending `Tutor off` disabled only the enabled test chat.

The selected correction is JSON-encoded ordinary user text in hook context; it does not replace the visible message. A live test confirmed the main chat remained at Xhigh while the temporary grammar worker used Low. The worker received the current prompt without prior conversation history, project instructions, or user model settings. No account credentials were copied into the repository.

## Measured latency

These are individual observations on one machine, not latency guarantees. CLI events combine network, server queueing, and model generation into one request interval.

| Worker configuration | Startup | GPT request | Post-answer handling | Total |
| --- | ---: | ---: | ---: | ---: |
| Original short correction | 1.52 s | 7.44 s | 3.53 s | 12.49 s |
| Original already-correct prompt | 1.31 s | 6.46 s | 3.99 s | 11.76 s |
| Analytics disabled | 1.85 s | 8.43 s | 0.014 s | 10.30 s |
| Analytics disabled + grammar-only base instructions | 0.99 s | 5.00 s | 0.025 s | 6.03 s |

Disabling worker analytics removed the observed shutdown tail. Replacing the coding instruction template with the short grammar instruction reduced reported input from 12,039 to 7,888 tokens for the same prompt; output remained 21 tokens. These worker overrides do not change the main Codex configuration.

A separate six-request experiment compared two fixed prompts per profile. Both prompts matched their expected correction in every profile, including preservation of `npm test` and `src/main.go` in a technical question.

| Test profile | Agreement prompt total / GPT request | Technical prompt total / GPT request |
| --- | ---: | ---: |
| GPT-6.1 Sol, Low | 8.07 s / 6.21 s | 5.00 s / 4.11 s |
| GPT-6 Luna, Low | 5.18 s / 4.19 s | 3.94 s / 2.81 s |
| GPT-6 Luna, None | 3.89 s / 2.98 s | 4.16 s / 2.95 s |

The cases ran in forward then reverse profile order. Two prompts per profile are insufficient to establish grammar quality or reliable speed differences. No production model or effort was changed by this experiment. Reproduce it with `scripts/benchmark_tutor_latency.py`; it uses the existing Codex allowance and may require model access.

## Remaining limitations

- Desktop and IDE native-choice rendering needs direct verification; CLI results do not establish support in every client.
- Codex can skip an optional MCP Tutor hook if its server or tool is unavailable. The separate command hook still enforces the letter rule.
- GPT can miss errors, alter meaning, take several seconds, or time out after 30 seconds. Review the whole suggestion and use **Keep original** when needed.
- The original visible message remains in the transcript after selecting a correction.
- Codex must load and trust the installed hooks. The tool applies to local Codex prompt text, not cloud tasks, ChatGPT web/mobile, or text inside attachments.
- Exact token counts are available only when a Codex client reports them. Worker usage must not be attributed to the main chat or to development subagents.
- OpenAI's public plugin directory currently excludes packages with lifecycle hooks. Local MCP submissions additionally require approved-partner access. Distribution is through GitHub Releases; no public directory listing has been submitted.
