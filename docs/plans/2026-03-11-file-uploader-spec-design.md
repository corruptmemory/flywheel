# File Uploader Specification Design

**Date:** 2026-03-11
**Status:** Approved

## Context

We are writing structured Markdown specifications that an independent "lights-out" agent will use to build a Go web application from scratch. The agent has no access to the source project (`idpair-central`) — only these specs.

The application is a **local data bridge for regulated gaming**. Operators upload CSV files containing player/gaming data. The app validates, anonymizes (Argon2 hashing), and transforms the data, then uploads it to a remote API. No PII leaves the machine.

## Approach

**Layered specs (Option B):** Numbered documents, each covering one concern, read in order. Each is self-contained enough to implement independently. Cross-cutting patterns established in `01-overview.md` and referenced throughout.

## Spec Structure

Output goes to `~/projects/file-uploader/`:

```
README.md                          -- "Start here" for the builder agent
build-specification/
  01-overview.md                   -- Project goals, tech stack, cross-cutting patterns
  02-architecture.md               -- State machine, actor pattern, interfaces, DI
  03-configuration.md              -- TOML config, CLI args, setup wizard flow
  04-csv-pipeline.md               -- 10 CSV types, column mappings, validation, hashing
  05-web-ui.md                     -- Routes, pages, SSE, file upload, auth/JWT
  06-players-db.md                 -- Format, dedup logic, concurrent access
  07-testing.md                    -- Test strategy, synthetic CSV tooling, mocks
  08-build-and-deploy.md           -- build.sh, embed, ldflags, air config
uat-feedback/                      -- UAT agent drops feedback here
```

Each spec follows a consistent format:
- **Purpose** — what this spec covers and why
- **Requirements** — what must be built (numbered for traceability)
- **Interfaces** — Go interface definitions where applicable
- **Behavior** — how things work, with examples
- **Constraints** — what NOT to do
- **Acceptance Criteria** — how the UAT agent will verify this

## Tech Stack

- **Router:** chi (`github.com/go-chi/chi/v5`)
- **Templates:** templ (`github.com/a-h/templ`)
- **Frontend interactivity:** htmx (vendored JS, no npm/node)
- **CLI parsing:** go-flags with subcommands
- **Config:** TOML via BurntSushi/toml
- **CSS:** Custom design tokens + utility classes (no Bootstrap)
- **Concurrency:** Goroutine actor pattern (no mutexes)
- **Storage:** File-based only (no database)
- **Binary:** Single static binary with embedded assets

## Key Design Decisions

### Interfaces Everywhere
Every external dependency sits behind an interface with at least two implementations:
- `RemoteAPI` — real impl + mock (stores files locally)
- `AuthProvider` — real (login/password/MFA) + mock (accept anything)
- `Uploader` — real (HTTP PUT) + mock (copy to output dir)
- `PlayerDB` — real (file-backed) + DevNull

### Intentional Improvements Over Source
1. **MetaID hash** — must incorporate country+state (source has a TODO noting this bug)
2. **Players DB header** — store SHA-256(pepper) instead of raw pepper
3. **Setup wizard** — clean state machine with dispatch table, not nested switches
4. **Upload limit** — 50MB default (source has 1MB)
5. **MergeSettableValues** — fix copy-paste bug with UsePlayersDB tag
6. **Tests** — comprehensive test suite (source has nearly none)

### CSV Pipeline
- 10 CSV types: Bets, Players, Casino Players, Bonus, Casino, Casino Par Sheets, Complaints, Demographic, Deposits/Withdrawals, Responsible Gaming
- Auto-detection by header matching
- Columnar processor pattern (InColumnProcessor interface)
- Parallel row processing within a file, serial file processing
- Argon2 hashing (time=3, memory=32MB, parallelism=4, output=32 bytes)
- Name normalization: diacritics removal, punctuation handling, lowercase
- Country validation: US only, 50 states + DC

### Synthetic CSV Generator
- CLI subcommand: `file-uploader gen-csv --type players --rows 100`
- Also a Go library for use in tests
- Generates valid and invalid CSVs with configurable error injection
- Deterministic seeding for reproducibility

### Auth Flow
- JWT (HS256) with 5-minute expiry
- Login with username/password + optional MFA
- Session auto-extend on user activity
- Auth provider interface allows mock for testing

### Players DB
- Optional (configurable)
- TOML header + delimiter + CSV body
- Header stores: hash algorithm, SHA-256(pepper)
- Composite key: `orgPlayerID:country:state`
- Concurrent access via actor pattern
- DevNull implementation when disabled

## UAT Integration

UAT agent in `~/projects/file-uploader-uat/` tests:
1. Build succeeds, produces single binary
2. Setup wizard flow (all steps, validation, back-nav)
3. CSV processing for all 10 types
4. Invalid CSV error handling
5. Synthetic CSV tooling works
6. SSE real-time updates
7. Auth flow (login, expiry, extend, logout)
8. Settings persistence
9. Archive search/filter
10. Players DB dedup and download
11. Mock mode (no real API needed)

Feedback goes to `~/projects/file-uploader/uat-feedback/` as structured Markdown. Builder checks this directory and iterates.

## Specs Must Be Model-Agnostic

The specs must work for any frontier model agent (Claude, Gemini, ChatGPT/Codex). No Claude-specific affordances. No references to the source project.
