# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What This Project Is

Flywheel is a **meta-project** — it contains no application code. It produces structured Markdown specifications and plans that independent "lights-out" agents use to build and test a Go web application (a file uploader inspired by `idpair-central`).

**Key principle:** No code from the source project (`~/idpair/idpair-central`) is shared — only structured specifications. The building agents never see the source repo.

## Repository Structure

- `README.md` — Rules of engagement, constraints, and evaluation criteria
- Specifications and plans go into the **output directories** (not this repo):
  - `~/projects/file-uploader` — Builder agent workspace (needs `README.md` + `build-specification/` directory)
  - `~/projects/file-uploader-uat` — UAT agent workspace (independent acceptance testing)

## Workflow

1. Investigate `~/idpair/idpair-central` source project deeply
2. Write specifications as Markdown docs in `~/projects/file-uploader/build-specification/`
3. Builder agents execute specs lights-out in `~/projects/file-uploader`
4. UAT agents independently test in `~/projects/file-uploader-uat`
5. Review outputs, refine specs, iterate until acceptable

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

## Evaluation Loop

1. Automated UAT must pass (feedback goes to `~/projects/file-uploader/uat-feedback/`)
2. This project reviews builder + UAT outputs and refines specs
3. User gives final review — iterate until accepted
