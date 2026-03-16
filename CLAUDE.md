# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working in this repository.

## What This Project Is

Flywheel is a **meta-project** — it contains no application code. It produces structured Markdown specifications and plans that independent "lights-out" agents use to build and test a Go web application (a file uploader inspired by `idpair-central`).

**Key principle:** No code from the source project (`~/idpair/idpair-central`) is shared — only structured specifications. The building agents never see the source repo.

## Repository Structure

```
cmd/flywheel/main.go       — Orchestration driver (Go CLI)
prompts/                    — Agent prompt templates
  builder-base.md           — Shared builder instructions (all invocations)
  scenario-init.md          — First build (fresh start)
  scenario-next-spec.md     — After spec passes all gates
  scenario-fix-code-review.md    — After code review FAIL
  scenario-fix-security-review.md — After security review FAIL
  scenario-fix-dev-test.md  — After dev test FAIL
  scenario-fix-uat.md       — After UAT FAIL
  code-review.md            — Code review agent prompt
  security-review.md        — Security review agent prompt
  dev-test.md               — Dev test agent prompt
  uat.md                    — UAT agent prompt
design-system/              — Durable visual design artifact (copied into builder workspaces)
  tokens.css                — CSS custom properties (colors, spacing, fonts)
  patterns.md               — Component patterns (cards, badges, forms, etc.)
  layout.md                 — Layout rules (breakpoints, grid, page structure)
docs/meeting-notes/         — Design discussion transcripts
  screenshots/              — Visual reference from round 2
feedback/                   — Post-mortem analysis from previous rounds
  round1.md, round1-analysis.md, round2-assessment.md
  ideas-for-round-3.md      — Round 3 vision (5-phase pipeline)
docs/plans/                 — Design docs and implementation plans
```

### Output Directories (not in this repo)

| Round | Builder Workspace | UAT Workspace |
|-------|-------------------|---------------|
| 1 | `~/projects/file-uploader` | `~/projects/file-uploader-uat` |
| 2 | `~/projects/file-uploader-r2` | `~/projects/file-uploader-r2-uat` |
| 3 | `~/projects/file-uploader-r3` | `~/projects/file-uploader-r3-uat` |

## Flywheel Driver

The `cmd/flywheel/` Go program orchestrates the multi-phase pipeline:

```bash
go build -o flywheel ./cmd/flywheel/

# Round 3
./flywheel \
  --builder-dir ~/projects/file-uploader-r3 \
  --uat-dir ~/projects/file-uploader-r3-uat \
  --prompts-dir ~/projects/flywheel/prompts \
  --max-rounds 20 \
  --max-gate-retries 3 \
  --skip-permissions --model opus
```

**Key flags:**
- `--skip-permissions` — passes `--permission-mode bypassPermissions` to claude
- `--model` — model override (e.g., `sonnet` for smoke tests, `opus` for real builds)
- `--max-rounds` — max spec-level iterations (default: 20)
- `--max-gate-retries` — max times builder can fail same gate per spec, 0=unlimited (default: 3)
- `--timeout` — per-session wall-clock limit (default: 0 = unlimited)
- `--retries` — retries per phase if claude crashes without done file (default: 2)
- `--log-file` — JSONL timing log (default: `flywheel-runs.jsonl`)

**Pipeline (round 3 — done-file-driven state machine):**

```
Builder (1 spec) → Code Review → Security Review → Dev Test
   ↑ FAIL from any gate routes back to Builder
   Dev Test PASS → Builder (next spec)
   Dev Test UAT READY → UAT → PASS = DONE
```

The driver reads done files on disk to determine the next phase. No `--start`/`--resume` needed — always picks up where it left off. Prompt is assembled by concatenating scenario snippet + builder-base.md + completion suffix.

**Resilience:** Ignores SIGTERM/SIGUSR1/SIGUSR2/SIGHUP (child signals don't kill driver), retries on crash, verifies git clean+pushed before terminating, JSONL per-phase logging, filters `CLAUDECODE` env var.

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

## Round 2 Results

**Workspace:** `~/projects/file-uploader-r2` and `~/projects/file-uploader-r2-uat`
**Verdict:** UAT ACCEPTED after 5 cycles (vs 10-12 in round 1)
**Builder grade:** A- | **UAT grade:** A
**Full assessment:** `feedback/round2-assessment.md`

### What Changed from Round 1

- 15 task-sized specs (~200-400 lines) instead of 8 topic-sized specs (~800+ lines)
- Each spec has dependency declarations and inline tests
- Sub-agent delegation: builder constructs dependency graph, delegates per spec
- Automated UAT test harness: 111+ test points in bash scripts + Playwright
- UAT reads build specs and cites them in FAIL reports
- No BLOCKED verdicts — everything is PASS or FAIL
- Flywheel driver automates the builder→UAT loop with crash recovery

### What the Builder Produced

- 7,960 lines Go source, 5,358 lines tests (67% test-to-code ratio)
- All 15 specs implemented with one-commit-per-spec discipline
- Actor pattern throughout (zero mutexes in production code)
- All 10 CSV types, Argon2 hashing, player DB, SSE broadcaster, worker pool
- Zero TODO/FIXME/HACK comments
- Known source bug (hash collision) NOT carried forward

### Key Failure: RunningApp Wiring (Round 4)

The builder initially wired `NewMockRunningApp` for ALL code paths — not just mock mode. The real pipeline (CSV worker pool, SSE, player DB, archive) was implemented but never connected. This is the same context-window pressure pattern from round 1, but the sub-agent strategy made it a one-commit fix rather than a death spiral.

**Lesson:** Specs 10 and 11 need explicit "wire the real implementation in non-mock mode" mandates.

### UAT Test Harness

The UAT agent built genuine test automation in the UAT workspace:
- `test-harness.sh` — 65 automated HTTP/curl tests
- `test-harness-extra.sh` — 46 supplementary tests (concurrent uploads, hashing, filtering)
- `uat-playwright.js` — 530+ lines of browser tests, 25+ screenshots
- Fully idempotent, builder can re-run to verify fixes

## Round 3 Results

**Workspace:** `~/projects/file-uploader-r3` and `~/projects/file-uploader-r3-uat`
**Verdict:** UAT ACCEPTED after 2 rounds (vs 5 in round 2)
**Architecture:** Multi-phase pipeline with per-spec quality gates
**Full observations:** `feedback/round3-observations.md` (post-mortem in progress)
**Stats:** 112 total phases (40 builds, 27 code reviews, 25 security reviews, 18 dev tests, 2 UAT), ~11 hours, 6 driver launches (8 driver bugs found and fixed)

### Key Changes from Round 2

- **5-phase pipeline:** Build → Code Review → Security Review → Dev Test → (next spec or UAT)
- **Per-spec quality gates:** Every spec must pass all 3 review stages before advancing
- **Done-file-driven state machine:** Driver derives next phase from done files on disk
- **Adversarial review agents:** Code review, security review, and dev test are all adversarial judges
- **Prompt concatenation:** scenario snippet + builder-base.md + completion suffix
- **Gate retry tracking:** `--max-gate-retries` caps consecutive failures per spec
- **Design system:** Durable visual artifact extracted from round 2, copied into workspace
- **Spec refinements:** Specs 10/11 mandate real implementation wiring, specs 12-14 reference design system

### Design Doc

Full architecture: `docs/plans/2026-03-13-round3-multi-phase-pipeline-design.md`

## Lessons Learned (Cumulative)

### Spec Engineering

1. **Task-sized specs (200-400 lines) over topic-sized (800+).** Validated in round 2 — average 5.5 min per spec.
2. **Explicit dependency declarations** at the top of each spec. Enables sub-agent delegation.
3. **Separate "what to build" from reference data.** Column mappings, validation rules in structured format.
4. **Test specs inline with implementation tasks.** Sub-agent gets everything in one place.
5. **Interface-first design** with cross-references. Agent only loads what it needs.

### Builder Process

1. **"Taste the soup" — self-test before declaring done.** Improved from round 1 but still not fully internalized (missed RunningApp wiring).
2. **Sub-agent delegation with dependency graph** is the key innovation. Prevents context exhaustion.
3. **Commit per task** provides rollback points and makes UAT easier.
4. **Explicit "wire real implementation" mandates** needed in specs that define mock/real boundaries.

### UAT Process

1. **No BLOCKED verdicts, ever.** If a feature is missing, it's a FAIL.
2. **Read specs BEFORE testing.** UAT must know what was specified before judging what was built.
3. **Cite specs in FAIL reports.** Evidence-based, not opinion-based.
4. **Build automated test harness FIRST.** 111+ automated tests saved enormous context window.
5. **Add novel failures to harness as regression tests.** Metasploit model.
6. **Mock mode is not an excuse.** Per spec 11, mock replaces external deps only.

### Driver Operations

1. **Signal isolation** (SIGTERM, SIGUSR1, SIGUSR2, SIGHUP) — child signals must not kill the driver.
2. **No Setpgid** — claude needs TTY access for its UI.
3. **Done-file is the authority**, not exit codes. Retry if no done file.
4. **Verify git clean + pushed** before terminating a session.
5. **JSONL logging + resume** enables crash recovery across driver restarts.

## Prior Art and Industry Context

Flywheel is one instance of the emerging "software factory" pattern — fully autonomous agent-driven development pipelines. See `docs/software-factory-comparison.md` for a detailed comparison.

### StrongDM Software Factory (https://factory.strongdm.ai/)

StrongDM's published framework independently converged on many of the same patterns:
- Specs + scenarios drive agent behavior (same as our `build-specification/`)
- Agents converge without human review (we still have a human evaluation step)
- "Scenarios" instead of tests to prevent agents gaming narrow assertions
- "Satisfaction" metrics (probabilistic) instead of boolean PASS/FAIL
- Digital Twin Universe (DTU) — behavioral clones of third-party APIs for testing at scale

**Key differences:** StrongDM is vague on coordination mechanics. Flywheel has a concrete, debuggable state machine (done-file-driven, deterministic routing). Flywheel also has per-spec adversarial quality gates that prevent bug accumulation.

### Convergent Design Insight

Both approaches (and the broader industry trend) converge on the same insight: individual AI agents are noisy but capable. The engineering challenge is **system design that amplifies signal over noise** — not making individual agents smarter. StrongDM uses scenarios and satisfaction metrics. Flywheel uses adversarial quality gates, fresh contexts per invocation, and mechanical boundaries (done files, immediate kill switches). Both work because the system is reliable even when individual agents are not.

### Ideas for Future Iterations

- **Probabilistic satisfaction metrics** instead of binary PASS/FAIL for UAT
- **Holdout validation scenarios** stored only in UAT workspace (prevents gaming)
- **Digital Twin Universe** for third-party API testing at scale
- **Planning engine** — an agent layer that produces task-sized specs from product descriptions (solves the greenfield spec problem)
- **Multi-project validation** — run the generic driver on a second, different project

## Round 4 (Planned)

**Companion project:** `~/projects/lightweight-containers` — provides OS-level agent isolation via chroot jails with bind mounts. Working prototype with stub agents, stress-tested to 76+ rounds. Packages (`broker/`, `mcp/`, `jail/`, `gitops/`) will be absorbed into flywheel; prototype driver will be discarded (flywheel's driver is more mature). `seqsock/` is deprecated (broker has internal counter for MCP mode).

**Design discussions:**
- `docs/meeting-notes/2026-03-15-round4-direction.md` — initial direction
- `docs/meeting-notes/2026-03-15-round4-mcp-and-enforcement.md` — MCP communication layer and enforcement design
- `feedback/round-4-ideas.md` — full state machine and role descriptions

### Packages to Absorb from lightweight-containers

| Package | Lines | Purpose |
|---------|-------|---------|
| `broker/` | ~800 | Actor-based event broker: PO request/response/amendment protocol, push events, `write_done` enforcement, sequential IDs |
| `mcp/` | ~755 | JSON-RPC MCP server: per-role tool surfaces, calls broker in-process, Unix socket per agent |
| `jail/` | ~715 | Chroot isolation: `RootJail` (mount syscalls) + `UnshareJail` (user namespaces, no root), conditional mounts for merged-usr |
| `gitops/` | ~800 | Workspace init, per-role branches, `format-patch`/`am` transplant with path filtering/squash, pre-commit hook enforcement |
| `internal/chanutil/` | ~48 | Generic actor channel helpers (`SendReceive`, `SendReceiveError` with panic recovery) |

**Not absorbed:** `internal/driver/` (prototype, flywheel's driver is more mature), `cmd/lc/` (test CLI), `stubs/` (shell test harness), `seqsock/` (only for non-MCP mode).

**Key architectural evolution (2026-03-16):** The broker and MCP servers were consolidated from a 7-process architecture (driver + broker + 5 MCP servers communicating over sockets) into a single process. The broker runs as an in-process actor goroutine; MCP servers call it directly via the `broker.Broker` interface. This eliminates all IPC overhead.

### Mechanical Isolation

Replace prompt-based access control ("don't touch X") with OS-level enforcement. Each agent gets a chroot jail with tailored filesystem projections — the builder literally cannot see reviewer tooling, reviewers cannot write source code. Three-layer isolation: chroot (visibility) → git hooks (commit enforcement) → patch filtering (planned).

### Tooling-Driven Agent Model

The fundamental architectural shift: agents are **respondents**, not judges.

1. **Speculative tooling derived from specs** encodes what MUST be true at each pipeline stage
2. **Flywheel runs the tooling mechanically** and feeds output as agent input — no forgetting, no skipping
3. **Failures are true until proven otherwise** — the default is that a tooling failure is real
4. **One escape hatch:** agents may update detection criteria (HOW to prove something), never invariants (WHAT must be true)

This is the invariant/detection split: invariants are derived from specs and are non-negotiable; detection criteria are updatable when the specifics of proving an invariant don't align with reality.

### MCP Server as Coordination Layer

Each agent communicates with the pipeline exclusively through MCP tools — no direct file manipulation for coordination. The MCP server abstracts PO communication, done files, and check results behind tool calls.

**Per-role tools:**
- **All agents:** `send_request`, `wait_for_response` (sync), `check_response` (async), `list_my_requests`, `write_done`
- **PO:** `list_pending_requests`, `read_request`, `send_response`, `create_amendment`, `list_amendments`, `write_done`

**Mechanical enforcement via MCP:**
- `write_done` refuses if outstanding PO requests exist
- `write_done` validates status format per phase (UNDERSTAND/DISCUSSION during init; PASS/FAIL during review)
- Identity baked in — agent can't impersonate another role
- Spec amendments invisible intra-turn (MCP serves from turn-start snapshot)

**Enforcement boundary:** Mechanical enforcement for coordination (process happened, files exist, format correct, requests answered). Trust trained intent for judgment quality (review thoroughness, check meaningfulness, adversarial posture). Over-enforcing creates Goodhart's Law incentives for agents to game gates rather than do meaningful work.

### Init/Prep Convergence Protocol

Before any code is written, all agents negotiate understanding of the specs:

1. PO starts and stays running. All other agents start simultaneously.
2. Agents read specs, interact with PO as needed (CLARIFY, CONFLICT/AMBIGUITY, SUGGESTED IMPROVEMENT).
3. `UNDERSTAND` = no PO interaction needed. `DISCUSSION` = had PO interaction this turn.
4. Any `DISCUSSION` → restart ALL agents with fresh spec snapshot (including accumulated amendments).
5. Converges when all agents write `UNDERSTAND` in the same round.

During init/prep, reviewers build **speculative tooling** (`run_checks.sh`) covering the entire spec set.

### New Agent: Product Owner

A long-lived concurrent process contactable via MCP-based request/response protocol. Typed request system:
- **PLEASE CLARIFY** — request spec clarification
- **POTENTIAL SPEC CONFLICT/AMBIGUITY** — mutually contradictory or ambiguous specs
- **SUGGESTED IMPROVEMENT** — opportunity to improve the product for the customer

PO maintains specs via **accumulating amendments** (`NNNN-<spec>-amendment-NNN.md`). Amendments cite files and lines, can amend other amendments, and higher suffix numbers supersede lower ones. PO responses must be self-contained (agents can't see updated specs intra-turn).

### Prompt Rework

All agent prompts need significant rework to support the new tooling-driven model. This is a major upcoming workstream.

### Implementation Phases

```
Phase 0 (absorb packages) → Phase 1 (workspace/isolation) → Phase 2 (state machine) → Phase 5 (real agents)
                                                          ↘ Phase 3 (MCP/PO) → Phase 4 (prompts) ↗
```

## Evaluation Loop

1. Automated UAT must pass (feedback goes to builder workspace's `uat-feedback/`)
2. This project reviews builder + UAT outputs and refines specs
3. User gives final review — iterate until accepted
