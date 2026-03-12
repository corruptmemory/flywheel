# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What This Project Is

Flywheel is a **meta-project** — it contains no application code. It produces structured Markdown specifications and plans that independent "lights-out" agents use to build and test a Go web application (a file uploader inspired by `idpair-central`).

**Key principle:** No code from the source project (`~/idpair/idpair-central`) is shared — only structured specifications. The building agents never see the source repo.

## Repository Structure

```
cmd/flywheel/main.go    — Orchestration driver (Go CLI)
prompts/                 — Agent prompt templates
  builder-init.md        — Initial builder prompt (round 1)
  builder-fix.md         — Fix prompt (rounds 2+, after UAT failure)
  uat.md                 — UAT agent prompt (uses {{BUILDER_DIR}}, {{UAT_DIR}} vars)
feedback/                — Post-mortem analysis from previous rounds
  round1.md              — User feedback from round 1
  round1-analysis.md     — Detailed analysis
```

### Output Directories (not in this repo)

| Round | Builder Workspace | UAT Workspace |
|-------|-------------------|---------------|
| 1 | `~/projects/file-uploader` | `~/projects/file-uploader-uat` |
| 2 | `~/projects/file-uploader-r2` | `~/projects/file-uploader-r2-uat` |

## Flywheel Driver

The `cmd/flywheel/` Go program orchestrates builder→UAT round trips:

```bash
# Build the driver
go build -o flywheel ./cmd/flywheel/

# Run (round 2 example)
./flywheel \
  --builder-dir ~/projects/file-uploader-r2 \
  --uat-dir ~/projects/file-uploader-r2-uat \
  --prompts-dir ~/projects/flywheel/prompts \
  --max-rounds 5 \
  --timeout 3h \
  --skip-permissions \
  --model opus
```

**Flags:**
- `--skip-permissions` — passes `--dangerously-skip-permissions` to claude (required for lights-out)
- `--model` — model override (e.g., `sonnet` for smoke tests, `opus` for real builds)
- `--max-rounds` — safety cap on builder→UAT cycles (default: 5)
- `--timeout` — wall-clock limit per agent session (default: 3h)

**Loop logic:**
1. Round 1: load `builder-init.md` → builder builds from scratch
2. UAT: load `uat.md` → adversarial testing, writes PASS or FAIL to `done`
3. If FAIL: load `builder-fix.md` → builder reads `uat-feedback/`, fixes issues
4. Repeat until PASS or max rounds

The driver clears `CLAUDECODE` env var from child processes to avoid nested-session detection.

## Workflow

1. Investigate `~/idpair/idpair-central` source project deeply
2. Write specifications as Markdown docs in the builder workspace's `build-specification/`
3. Run the flywheel driver — builder and UAT agents execute lights-out
4. Review outputs, refine specs, iterate until acceptable

## Target Stack (for specs you write)

The built application must use this Go web stack (per user preferences):
- **Router:** chi (`github.com/go-chi/chi/v5`)
- **Templates:** templ (`github.com/a-h/templ`)
- **Interactivity:** htmx (vendored, no npm/node)
- **CLI:** go-flags with subcommands
- **Config:** TOML via BurntSushi/toml
- **State management:** Goroutine actor pattern (no mutexes) — use the `actor-pattern` skill
- **No database** — file-based storage only
- **Single static binary** with embedded assets

## Key Constraints

- Specs must be model-agnostic (Claude, Gemini, ChatGPT/Codex should all be able to follow them)
- No "secret messages" biasing toward exact source replication — describe interfaces and logic textually
- Surface bugs found in the source rather than replicating them
- Example CSVs at `~/idpair/example-csvs` (mix of valid and invalid) — builder agents may reference these
- Specs should instruct builders to create synthetic CSV generation tooling (both library and CLI)

## Engineering Specs for Lights-Out Execution

The fundamental constraint for lights-out agent work is the **context window**. Every task an agent executes has a context budget — the spec it loads, existing code it reads, code it writes, and compiler/test output all compete for the same window. Design specs to minimize what the agent needs to load.

### Principles

- **Task-sized specs over topic-sized specs.** A 1200-line spec covering 10 CSV types, 6 processors, hashing, and a worker pool is really 4-5 implementation tasks crammed into one doc. If each task's spec fits in ~300 lines, the implementer has room for code + test output + error messages.

- **Explicit dependency declarations at the top of each spec.** Something like: "This task requires: `Hashers` interface (02-architecture.md, lines 145-150), `Entry` struct (06-players-db.md, lines 19-26)." The orchestrator can inject only those snippets instead of the full referenced spec.

- **Separate "what to build" from "reference data."** Column mappings, validation rules, and enum tables are reference data. They could live in structured files (TOML/JSON) that the code reads, rather than prose the agent has to parse and translate into code.

- **Test specs inline with implementation tasks.** Rather than one monolithic testing spec, put each task's test cases alongside its implementation spec. The subagent gets everything in one place.

- **Interface signatures are the API contracts.** Interface-first design pays double for lights-out: the implementing subagent only needs the interface + its spec, not the caller's full context. Cross-references between specs (`→ see 04-csv-pipeline.md`) let the agent fetch only what it needs.

- **"Don't ask questions" + "make reasonable choices" is what makes it lights-out.** Without that, brainstorming skills will try to ask clarifying questions and stall. Include this in the kickoff prompt.

### Anti-Patterns

- Specs that require reading 3+ other specs to understand a single task
- Prose descriptions of data that should be structured (column mappings, enum values)
- Monolithic specs that mix foundational types with high-level orchestration
- Assuming the agent will "remember" context from earlier in the same spec — if a spec is long, the agent may lose early details

## Builder Process Requirements (from Round 1)

The builder agent MUST follow this workflow. These are non-negotiable.

### "Taste the Soup" Principle

No chef lets their patrons discover if the soup is good or bad — the chef tastes the soup first. The builder must:

1. Work through the task list
2. After each task: fire up the app, smoke-test with `curl` AND Playwright
3. If anything fails: formulate a fix plan, loop back to step 1
4. Only declare "ready for UAT" after a full self-UAT pass against the acceptance criteria

### Incremental Testing

During development, after completing each feature:
- Run the application
- Exercise the feature via curl (headless) and/or Playwright (browser)
- Quick focused smoke tests — not comprehensive, just "does what I just built work?"
- Fix problems immediately, don't let them accumulate

### Mandatory Gates Before "Done"

1. All unit tests pass (`./build.sh -t`)
2. Race detector clean (`./build.sh -t` with `-race`)
3. Full self-UAT pass (builder runs through UAT acceptance criteria)
4. Code review (architecture, patterns, dead code)
5. **Security review** — especially auth, session management, token handling. Any application with a login MUST prevent session replay attacks.

### Commit Discipline

Commit after each completed task, not one giant commit at the end. This provides rollback points and makes the UAT agent's job easier.

## UAT Process Requirements (from Round 1)

### Adversarial Posture

The UAT agent must be mildly adversarial. It should:
- Actively try to break things, not just confirm happy paths work
- Push back against lazy explanations ("it's sessionless" is not an excuse for session replay)
- Test edge cases: large files, concurrent uploads, malformed input, session expiry
- Flag untested areas as risks (e.g., "real API integration not tested in mock mode")

### Explicit Checklists

UAT instructions must use checklist format with mandatory pass/fail per item, not prose descriptions. This combats forgetfulness (context window is the enemy for UAT too).

### Round-Trip Testing

Generate test data with `gen-csv`, upload it, verify processing output. Especially:
- `--inject-errors` → upload → verify error reporting per row
- Same player across multiple uploads → verify consistent hashing
- Different players → verify different hashes

## Round 1 Results

See `feedback/round1.md` (user feedback) and `feedback/round1-analysis.md` (detailed analysis).

- **UAT cycles:** 10-12 (estimated 2-3)
- **Root cause:** Builder never tested its own work before handing off
- **Builder grade:** B+ (architecture solid, process failed)
- **UAT grade:** C+ (found real bugs but too shallow, not adversarial enough)

## Round 2 (Current)

Running in `~/projects/file-uploader-r2` and `~/projects/file-uploader-r2-uat`.

Key changes from round 1:
- 15 task-sized specs (~200-400 lines) instead of 8 topic-sized specs (~800+ lines)
- Each spec has dependency declarations and inline tests
- Added `chi/render` to the tech stack
- Separated reference data (CSV column mappings) from implementation logic
- Flywheel driver automates the builder→UAT loop

## Evaluation Loop

1. Automated UAT must pass (feedback goes to builder workspace's `uat-feedback/`)
2. This project reviews builder + UAT outputs and refines specs
3. User gives final review — iterate until accepted
