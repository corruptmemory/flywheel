# Round 3 Design: Multi-Phase Pipeline

**Date:** 2026-03-13
**Status:** Approved
**Problem:** Builder cuts corners under context pressure; quality issues discovered too late (UAT round 4+ instead of immediately)
**Solution:** Per-spec quality gates — every spec passes code review, security review, and dev test before advancing

---

## Architecture Overview

```
Builder (1 spec)
    │
    ▼
Code Review ──FAIL──▶ Builder (respond to code review)
    │ PASS
    ▼
Security Review ──FAIL──▶ Builder (respond to security review)
    │ PASS
    ▼
Dev Test ──FAIL──▶ Builder (respond to dev test)
    │ PASS ──────────▶ Builder (next spec)
    │ UAT READY
    ▼
UAT ──FAIL──▶ Builder (respond to UAT)
    │ PASS
    ▼
DONE
```

All failures route back to the builder. Review/test agents are read-only judges, never fixers. After a builder fix, the inner loop restarts from code review (not from the phase that failed) — any code change could introduce new issues.

All review/test agents are **adversarial**. They look for problems, not confirm things work.

---

## 1. Driver State Machine

The driver is a dumb routing loop: determine phase → run phase → read done file → determine next phase.

### Phases

| Phase | Agent | Runs In | Done File |
|-------|-------|---------|-----------|
| BuildSpec | Builder | builder-dir | `builder_done` |
| CodeReview | Code review | builder-dir | `code_review_done` |
| SecurityReview | Security review | builder-dir | `security_review_done` |
| DevTest | Dev test | builder-dir | `dev_test_done` |
| UAT | UAT tester | uat-dir | `uat_done` |

### State Derivation from Done Files

The driver calls `determineNextPhase()` which checks done files in priority order:

```
1. If uat_done exists:
   - PASS → DONE, exit success
   - FAIL → BuildSpec with scenario "fix-uat"

2. If dev_test_done exists:
   - PASS → delete all inner-loop done files, BuildSpec with scenario "next-spec"
   - FAIL → delete builder_done, BuildSpec with scenario "fix-dev-test"
   - UAT READY → UAT

3. If security_review_done exists:
   - PASS → DevTest
   - FAIL → delete builder_done, BuildSpec with scenario "fix-security-review"

4. If code_review_done exists:
   - PASS → SecurityReview
   - FAIL → delete builder_done, BuildSpec with scenario "fix-code-review"

5. If builder_done exists → CodeReview

6. Nothing exists → BuildSpec with scenario "init" (first run)
```

### Cleanup Rules

Before launching each phase, the driver deletes **that phase's** done file:
- Before builder: delete `builder_done`
- Before code review: delete `code_review_done`
- Before security review: delete `security_review_done`
- Before dev test: delete `dev_test_done`
- Before UAT: delete `uat_done`

Before launching review/test agents, the driver also clears the corresponding **findings directory** (but NOT the persistent tooling directories):
- Before code review: clear `code-review/`
- Before security review: clear `security-review/`
- Before dev test: clear `dev-test-results/`
- Before UAT: clear `uat-feedback/`

When dev test says PASS (advance to next spec), delete all inner-loop done files (`builder_done`, `code_review_done`, `security_review_done`, `dev_test_done`) to reset the cycle.

### Safety Limits

| Flag | Default | Meaning |
|------|---------|---------|
| `--max-rounds` | 20 | Max spec-level iterations (outer loop) |
| `--max-gate-retries` | 3 | Max times builder can fail same gate on same spec (0=unlimited) |
| `--retries` | 2 | Retries per phase if claude crashes without done file |
| `--timeout` | 0 | Per-session wall-clock limit (0=unlimited) |

The gate retry counter increments each time a gate sends FAIL back to the builder. It resets when dev test says PASS (spec advances). If the counter exceeds `--max-gate-retries`, the driver halts with an error.

### CLI

```bash
./flywheel \
  --builder-dir ~/projects/file-uploader-r3 \
  --uat-dir ~/projects/file-uploader-r3-uat \
  --prompts-dir ~/projects/flywheel/prompts \
  --max-rounds 20 \
  --max-gate-retries 3 \
  --retries 2 \
  --skip-permissions \
  --model opus
```

Removed from round 2: `--start` and `--resume` (done-file-driven state machine makes these unnecessary — always picks up where it left off).

---

## 2. Done File Protocol

### `builder_done`

Written by builder. Read by code review, security review, and dev test to scope their work.

```
Agent: Builder
Scenario: init | next-spec | fix-code-review | fix-security-review | fix-dev-test | fix-uat
Spec status:
- 01-project-scaffold.md: DONE
- 02-chanutil-and-interfaces.md: DONE
- 03-configuration.md: JUST COMPLETED
- 04-hashing-and-normalization.md: PENDING
- 05-csv-framework.md: PENDING
- 06-csv-type-definitions.md: PENDING
- 07-worker-pool.md: PENDING
- 08-player-db.md: PENDING
- 09-csv-generator.md: PENDING
- 10-application-state-machine.md: PENDING
- 11-auth-and-server.md: PENDING
- 12-setup-wizard.md: PENDING
- 13-dashboard-and-sse.md: PENDING
- 14-archive-settings-playersdb.md: PENDING
- 15-build-and-deploy.md: PENDING
```

- `JUST COMPLETED` — the new work to focus review on
- `DONE` — previously completed and already reviewed
- `PENDING` — not yet built
- `Scenario` — tells downstream agents what the builder was doing

### `code_review_done`

```
PASS
```
or
```
FAIL
Summary: 3 issues found (1 major, 2 minor)
Details in: code-review/
```

### `security_review_done`

```
PASS
```
or
```
FAIL
Summary: 1 critical issue (session token not revoked on logout)
Details in: security-review/
```

### `dev_test_done`

```
PASS
```
(advance to next spec)

```
FAIL
Summary: 4 test failures, 2 regressions
Details in: dev-test-results/
```

```
UAT READY
Summary: All 15 specs complete, all regression tests passing
Details in: dev-test-results/
```

### `uat_done`

```
PASS
```
or
```
FAIL
```

The driver reads only the first line of each done file for routing. Summary lines are for human readability and agent context.

---

## 3. Progress File

Builder-owned persistent state. Never cleared by the driver. Read by all agents for context.

The builder maintains this file in the project root. After completing work on any spec, the builder appends/updates an entry.

```
# Progress

## Spec 01: Project scaffold (01-project-scaffold.md)
Status: DONE
Files: build.sh, go.mod, go.sum, main.go, internal/...
Notes: Basic scaffold, build.sh with all flags, go-flags CLI skeleton

## Spec 02: Channel utilities and interfaces (02-chanutil-and-interfaces.md)
Status: DONE
Files: internal/chanutil/chanutil.go, internal/chanutil/chanutil_test.go, internal/app/interfaces.go
Notes: Generic actor pattern helpers, core interfaces defined

## Spec 03: Configuration (03-configuration.md)
Status: DONE
Files: config.go, config_test.go, gen_config.go
Notes: TOML config with reflection-based gen-config subcommand
Testable: ./file-uploader gen-config prints default config

## Spec 04: Hashing and normalization (04-hashing-and-normalization.md)
Status: IN PROGRESS
```

Format: Markdown with per-spec sections. Each section has status, files modified, notes, and what's testable at that stage. Downstream agents use this to understand the codebase state without reading all the code.

The builder prompt specifies: *"Maintain a `progress` file in the project root. After completing each spec, update it with: spec name and file, status, files created/modified, notes on what was built, and what's testable at this stage. Downstream agents (code review, security review, dev test) read this file to scope their work."*

---

## 4. Prompt Architecture

### Builder: Base + Scenario Snippets (7 files)

The driver concatenates: `scenario-X.md` + `builder-base.md` + completion suffix.

**`builder-base.md`** — Shared instructions for every builder invocation:
- Sub-agent strategy (spawn sub-agents per spec, fresh context per spec)
- Progress file format and maintenance protocol
- `builder_done` file format and protocol
- Commit discipline (commit and push after each unit of work)
- Smoke testing (build, test, quick curl where applicable)
- "Don't ask questions, make reasonable choices"
- Design system reference ("follow `design-system/` for all UI work")

**`scenario-init.md`** — First invocation:
- Read all specs in `build-specification/` to understand full scope
- Begin with the first spec
- Read `progress` if it exists (shouldn't on a fresh start)

**`scenario-next-spec.md`** — After a spec passes all gates:
- Read `progress` to determine next spec
- If all specs are done, write `builder_done` with all DONE and proceed to final self-review

**`scenario-fix-code-review.md`** — After code review FAIL:
- Read ALL feedback in `code-review/`
- Fix every issue
- Reviewer is adversarial — don't dismiss without evidence

**`scenario-fix-security-review.md`** — After security review FAIL:
- Read ALL findings in `security-review/`
- Security issues are non-negotiable

**`scenario-fix-dev-test.md`** — After dev test FAIL:
- Read ALL results in `dev-test-results/`
- Pay attention to regressions — don't break working specs while fixing current one

**`scenario-fix-uat.md`** — After UAT FAIL:
- Read ALL feedback in `uat-feedback/`
- UAT cites specs — verify claims against specs before dismissing

### Review/Test Agents (3 files)

All run in builder-dir. All read `builder_done` to scope work and `progress` for context. All are adversarial.

**`code-review.md`:**
- Read `builder_done` to see what's JUST COMPLETED and what's DONE
- Read `progress` for context on the codebase
- Read the relevant spec(s) from `build-specification/`
- Review: bad/unclear code, style, comments/docs needed, CLI conventions, spec divergence, missing tests
- Build automation tooling in `code-review-tools/` where possible
- Write findings to `code-review/`
- Write `code_review_done` with PASS or FAIL

**`security-review.md`:**
- Same scoping via `builder_done` and `progress`
- Review: PII leaks, file cleanup (no raw CSVs retained), replay attacks, JWT handling, CSV poisoning, injection vectors, spec security requirements
- Build automation in `security-harness/` where possible
- Write findings to `security-review/`
- Write `security_review_done` with PASS or FAIL

**`dev-test.md`:**
- Same scoping via `builder_done` and `progress`
- Run all existing regression tests from `test-harness/`
- Build new tests for the JUST COMPLETED spec
- Smoke test everything available (curl, Playwright where UI exists)
- Adversarial — cite specs, no bullshit, no BLOCKED
- Build and maintain automated harness in `test-harness/`
- Write results to `dev-test-results/`
- Write `dev_test_done` with PASS, FAIL, or UAT READY
- UAT READY only when ALL specs are DONE and all regression tests pass

### UAT (1 file)

**`uat.md`:** — Updated from round 2. Runs in UAT dir. Full adversarial acceptance testing. Builds automated harness. Checks specs. No BLOCKED. Writes `uat_done`.

---

## 5. Design System

Durable visual artifact extracted from round 2. Lives in builder workspace at `design-system/`.

```
design-system/
  tokens.css            — CSS custom properties (colors, spacing, fonts, radii) — 52 lines from round 2
  patterns.md           — Component patterns (cards, badges, forms, tables, modals)
  layout.md             — Layout rules (responsive breakpoints, grid, page structure)
  screenshots/          — Visual reference from round 2 app
```

**Usage:**
- `tokens.css` is the starting point — copied into `static/css/tokens.css` during spec 01 (scaffold)
- `patterns.md` describes the component vocabulary that templ templates follow
- `screenshots/` are acceptance criteria, not pixel-match targets
- Referenced by UI specs (12-14) via "follow the design system in `design-system/`"

**Evolution:** Survives nuclear rebuilds and incremental amendments. Changes to visual style update the design system, not the specs.

---

## 6. Workspace Layout

### Builder (`~/projects/file-uploader-r3/`)

```
# Provided at start:
CLAUDE.md
README.md
build-specification/           — 15 task-sized specs
design-system/                 — tokens.css, patterns.md, layout.md, screenshots/

# Created during execution (persistent):
progress                       — Builder's state (which specs done, files, notes)
test-harness/                  — Dev test automation (grows over time)
security-harness/              — Security probes (grows over time)
code-review-tools/             — Code review automation (grows over time)

# Created during execution (ephemeral — cleared each cycle):
builder_done
code_review_done
security_review_done
dev_test_done
code-review/                   — Code review findings
security-review/               — Security review findings
dev-test-results/              — Dev test findings
uat-feedback/                  — UAT findings
```

### UAT (`~/projects/file-uploader-r3-uat/`)

```
# Provided at start:
README.md                      — Adversarial checklist

# Created during execution:
uat_done
test-harness.sh                — UAT automated tests (persists)
uat-playwright.js              — Browser tests (persists)
```

---

## 7. JSONL Logging

One line per complete flywheel run. Written at termination.

```json
{
  "start_time": "...",
  "end_time": "...",
  "duration": "...",
  "duration_seconds": 0.0,
  "model": "opus",
  "outcome": "pass | fail_max_rounds | error",
  "specs": [
    {
      "spec": "01-project-scaffold.md",
      "phases": [
        {
          "phase": "build",
          "prompt": "scenario-init.md",
          "start": "...",
          "end": "...",
          "duration_seconds": 0.0
        },
        {
          "phase": "code-review",
          "start": "...",
          "end": "...",
          "duration_seconds": 0.0,
          "verdict": "PASS"
        },
        {
          "phase": "security-review",
          "start": "...",
          "end": "...",
          "duration_seconds": 0.0,
          "verdict": "PASS"
        },
        {
          "phase": "dev-test",
          "start": "...",
          "end": "...",
          "duration_seconds": 0.0,
          "verdict": "PASS"
        }
      ]
    }
  ],
  "uat": {
    "phases": [
      {"phase": "uat", "verdict": "PASS", "...": "..."}
    ]
  }
}
```

Captures full history: which spec, which phases ran, which gates failed and retried, all with timing.

---

## 8. Implementation Scope

### Driver rewrite (`cmd/flywheel/main.go`)
- New state machine with `determineNextPhase()`
- Prompt concatenation (scenario + base + suffix)
- Done file cleanup per phase
- Findings directory cleanup per phase
- Gate retry counter with `--max-gate-retries`
- New JSONL log structure
- Remove `--start`/`--resume` flags
- Preserve: signal isolation, crash retries, done-file watcher, git verification, env filtering

### New prompt files (10 total)
- `builder-base.md` — extract common instructions from current `builder-init.md`
- 6 scenario snippets — small, focused context blocks
- `code-review.md` — new
- `security-review.md` — new
- `dev-test.md` — new
- `uat.md` — updated from round 2

### Design system extraction
- Screenshot round 2 app pages
- Extract `tokens.css` and `app.css` patterns
- Write `patterns.md` and `layout.md`

### Workspace setup
- Create `~/projects/file-uploader-r3` with CLAUDE.md, README.md, build-specification/, design-system/
- Create `~/projects/file-uploader-r3-uat` with README.md
- Create GitHub remotes

### Spec refinements (carried from round 2)
- Add explicit "wire real implementation in non-mock mode" to specs 10/11
- Reference design system in UI specs (12-14)
- Minor updates based on round 2 lessons
