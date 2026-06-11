# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

**translai** — Go CLI that translates `.srt` subtitle files via LLM providers (Ollama / llama.cpp / OpenAI-compatible). Module path `github.com/gabrielfareau/translai`, binary `translai`.

- **v0 scope = CLI only (phases 0–7).** No web server, review editor, cloud providers, or Docker yet — those are phases 8+, deferred.
- Full spec: `.claude/PROMPT.md`. Autonomous build plan: `docs/PLAN.md`.

## Commands

```bash
make check              # gate: tidy + vet + lint + test + build (run before every commit)
make build             # static binary ./translai
make test              # go test ./...
make test-integration  # real Ollama tests (tag=integration), skips if endpoint absent
make lint              # golangci-lint
make run ARGS="translate -i testdata/en.srt --target fr"

go test -run TestName ./internal/core/   # single test
```

## Autonomous build workflow

Follow `docs/PLAN.md` phase by phase, in order. The main thread orchestrates:

1. Delegate the phase to the **`phase-builder`** subagent (`.claude/agents/phase-builder.md`) — it reads `docs/spec/<phase>.md` + dependent code, implements code+tests, runs `make check`, returns a diff. It does not commit.
2. Review the diff (optionally `/code-review`).
3. Close with the **`/milestone`** skill — re-runs the gate, then commits in **gitmoji** format `:emoji: type(scope): subject`. Milestone commits are pre-authorized for this autonomous build; **never `push`**.
4. Do not start phase N+1 until phase N is green and committed.

Per-domain specs live in `docs/spec/` (one per package); they are authoritative for types, signatures, and tests. `docs/PLAN.md` is the phase order + gate rules.

## Architecture

```
cmd/translai/main.go    # Cobra root + translate command (NO business logic)
internal/
  srt/                  # astisub wrapper: Parse/Save + Cue/Document types
  detect/               # lingua-go language detection
  translate/            # Translator interface + openai_compat provider + prompt
  core/                 # pipeline (parse→detect→chunk→translate→reassemble) + job
  config/               # YAML config + thread-safe store
testdata/               # en/fr/es.srt fixtures (round-trip + detection)
```

**Core constraint**: no translation logic in `cmd/` — commands only call `internal/core`, `internal/config`, etc.

## Key invariants (do not violate)

- **Never alter cue indices or timestamps.** Only text is translated and re-injected into the original structure.
- **Translator contract**: `len(output) == len(input)`. Mismatch → retry once → fallback cue-by-cue (log `warn`). Never emit a corrupted SRT.
- **Chunking**: sequential within a file (context-dependent); each batch carries 2-3 already-translated cues as context (not re-translated).
- **Encoding**: force UTF-8 output; detect/convert latin-1/Windows-1252 input at parse time.
- **Config**: never return API keys in plain text from config read paths.

## Stack

| Package | Use |
|---|---|
| `github.com/spf13/cobra` | CLI |
| `github.com/asticode/go-astisub` | SRT/VTT parse+serialize |
| `github.com/pemistahl/lingua-go` | Language detection (restrict languages to limit binary size) |
| `gopkg.in/yaml.v3` | Config |
| `log/slog` | Structured logging |

Single static binary, `CGO_ENABLED=0`. No web deps (chi/HTMX) until phase 8.

## Gotchas

- Small local models (7B) may ignore the `[N]` indexed prompt format → the cue-by-cue fallback is mandatory, not optional.
- `lingua-go` embeds all language models; use `FromLanguages(...)` to keep the binary small.
- Default `BatchSize=25` is conservative — too-large batches exceed small-model context windows.

## Post-v0 reference (phases 8+)

Web UI (HTMX + SSE, tested via httptest + goquery), `/review` alignment editor with write-behind persistence (debounce + ticker + SIGTERM flush), Anthropic/Gemini providers, Docker. Specs in `.claude/PROMPT.md`.
