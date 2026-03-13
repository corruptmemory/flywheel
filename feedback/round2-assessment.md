# Round 2 Assessment

**Date:** 2026-03-13
**Builder workspace:** `~/projects/file-uploader-r2`
**UAT workspace:** `~/projects/file-uploader-r2-uat`
**Verdict:** UAT ACCEPTED after 5 rounds (4 FAIL + 1 PASS)

---

## Executive Summary

Round 2 is a substantial improvement over round 1. The task-sized spec strategy and sub-agent delegation pattern reduced UAT cycles from 10-12 to 5. The builder produced a fully functional 17,500-line application with a 67% test-to-code ratio — 40x more test coverage than the original source application. The UAT agent built a genuine automated test harness (111+ test points) and caught critical integration issues that round 1's UAT missed entirely.

---

## Metrics

| Metric | Round 1 | Round 2 | Change |
|--------|---------|---------|--------|
| UAT cycles to pass | 10-12 | 5 | -55% |
| Spec design | 8 topic-sized (~800+ lines) | 15 task-sized (~200-400 lines) | Right-sized |
| Builder grade | B+ | A- | Improved |
| UAT grade | C+ | A | Major improvement |
| Sub-agent strategy | None | Yes (dependency graph) | New |
| UAT automation | None | 111+ automated tests | New |
| Go source lines | Unknown | 7,960 | — |
| Go test lines | Unknown | 5,358 (67% ratio) | — |
| Total lines (all code) | Unknown | 17,569 | — |
| Build time (initial) | Unknown | 82 minutes (15 specs) | — |
| Total effective time | Unknown | ~2.5 hours | — |

## Timeline (from git timestamps and run logs)

### Builder Initial Build: 82 minutes

```
21:43  Specs loaded into workspace
21:47  01: Project scaffold and build system
21:48  02: Channel utilities and core interfaces
21:53  03: Configuration system
21:55  04: Hashing and name normalization
22:01  05: CSV framework
22:05  06: CSV type definitions
22:11  07: Worker pool and file processing
22:14  08: Player deduplication database
22:20  09: Synthetic CSV generator
22:23  10: Application state machine
22:30  11: Authentication, server, and HTTP wiring
22:46  12: Setup wizard
22:53  13: Dashboard, SSE, and client-side JavaScript
22:58  14: Archive, settings, players DB, and login pages
23:01  15: Build, embed, and deploy
23:05  Security: token revocation on logout, path traversal hardening
```

Average: ~5.5 minutes per spec. The builder constructed a dependency graph from the spec headers and delegated each spec to a sub-agent with a fresh context window.

### UAT Rounds

| Round | Phase | Duration | Outcome | Key Finding |
|-------|-------|----------|---------|-------------|
| 1 | UAT | ~28 min | FAIL | Crashed mid-run (exit 143), no verdict |
| 2 | UAT | ~11 min | FAIL | Setup wizard, settings, auth, CSS token issues |
| 3 | Builder fix | ~50 min | — | Fixed setup wizard, settings, auth, CSS tokens |
| 3 | UAT | — | FAIL | Real RunningApp not wired — all code paths used mock |
| 4 | Builder fix | ~90 min | — | Wired real RunningApp, fixed setup wizard, fixed data races |
| 4 | UAT | — | FAIL | MockAuthProvider.Login didn't accept test credentials |
| 5 | Builder fix | 3 min | — | Trivial fix: mock auth accepts any credentials |
| 5 | UAT | 16 min | **PASS** | 113/113 checklist items passed |

### Driver Issues (ate significant wall clock)

The flywheel driver had several issues that caused aborted runs and manual restarts:
- `CLAUDECODE` env var causing nested-session refusal
- `--dangerously-skip-permissions` showing trust dialog
- Exit code 143 (SIGTERM) crashing the driver
- Exit code 144 (SIGUSR1) crashing the driver
- `Setpgid` breaking claude's TTY access (setRawMode errno 5)

These are all fixed now. Without driver issues, the 5 UAT rounds would have completed in ~3-4 hours unattended.

---

## Builder Assessment: A-

### Strengths

1. **Perfect commit discipline.** One commit per spec, clean descriptive messages. 15 build commits + 1 security hardening + 4 UAT fix commits = 20 total.

2. **Sub-agent strategy worked brilliantly.** The builder analyzed spec dependencies, built a topological order, and delegated each spec to a fresh sub-agent. This prevented the context window exhaustion that plagued round 1.

3. **Actor pattern implemented correctly.** All shared mutable state in single-owner goroutines communicating via typed command channels. Zero mutexes in production code. The `chanutil` package provides generic helpers (`SendReceiveMessage`, `SendReceiveError`).

4. **Comprehensive test suite.** 5,358 lines of tests — 67% test-to-code ratio vs 1% in the original. Table-driven tests with real assertions, no mocking frameworks.

5. **Proactive security hardening.** Before UAT even ran, the builder added token revocation on logout and path traversal protection. This is "taste the soup" working.

6. **All 10 CSV types implemented** with full column mappings, validation rules, and error injection support.

7. **Zero technical debt markers.** No TODO, FIXME, or HACK comments anywhere.

### Weaknesses

1. **Mock RunningApp wiring issue (caught in round 4).** The builder initially used `NewMockRunningApp` for ALL code paths — mock, setup-complete, and fully-configured. The real `RunningApp` that wires up the CSV worker pool, SSE broadcaster, player DB, and archive was never connected. This is the same context-window pressure pattern from round 1, but the sub-agent strategy made it recoverable (one fix commit instead of a death spiral).

2. **Thinner implementation.** 7,960 lines vs 12,840 in the source (~62%). Some is legitimate simplification (no real API client, no i18n, no Bootstrap), but some areas may be thinner than spec'd.

3. **Self-testing incomplete.** The builder did some self-testing (security review gate) but didn't catch the RunningApp wiring issue itself. The "taste the soup" principle improved from round 1 but isn't fully internalized yet.

---

## UAT Assessment: A

### Strengths

1. **Real automated test harness.** Two bash scripts with 111+ test points, fully idempotent, with evidence capture. This is not a token effort — it has JWT decoding, cookie parsing, determinism checks, concurrent upload testing, and size limit enforcement.

2. **Playwright browser testing.** 530+ lines of browser tests with 25+ screenshots. Covers login, dashboard, upload zone, responsive layout, modals, and settings.

3. **Spec-aware.** Feedback reports cite build specifications: "Per spec 07, the worker pool should process files FIFO." This is a qualitative leap from round 1's opinion-based testing.

4. **Caught the critical issue.** The real RunningApp wiring problem — where every code path used the mock — is exactly what round 1's UAT missed. Round 2's UAT caught it by cross-referencing the spec against the source code.

5. **Structured feedback.** 15 Markdown files with severity, evidence, steps to reproduce, and spec references. The builder could understand and fix issues without ambiguity.

6. **Harness is reusable.** The builder could run the test harness to verify fixes, and it can be re-run in future rounds for regression testing.

### Weaknesses

1. **Took 3 rounds to catch RunningApp issue.** Rounds 1-3 focused on surface issues (setup wizard, CSS, auth). The fundamental "builder never built the real pipeline" wasn't caught until round 4. With spec-first verification (which we added mid-experiment), this should have been caught immediately.

2. **No novel adversarial discoveries.** The UAT tested everything on the checklist but didn't find anything surprising beyond it. The "Metasploit-style regression catalog" concept wasn't really exercised.

3. **Mock auth credential test was a late catch (round 5).** The harness should have tested login failure from the start, which would have revealed mock auth behavior earlier.

---

## Comparison: file-uploader-r2 vs idpair-central (Source)

### Architecture (Faithful Reproduction)

| Aspect | idpair-central | file-uploader-r2 | Match? |
|--------|---------------|-------------------|--------|
| Router | chi v5 | chi v5 | ✅ |
| Templates | templ (12 files) | templ (8 files) | ✅ (fewer pages) |
| Frontend | htmx + Bootstrap 5 | htmx + custom CSS tokens | ✅ (improved) |
| State management | Actor pattern (goroutine + channels) | Actor pattern (goroutine + channels) | ✅ |
| Config | TOML via BurntSushi | TOML via BurntSushi | ✅ |
| CLI | go-flags with subcommands | go-flags with subcommands | ✅ |
| Static assets | go:embed | go:embed | ✅ |
| Storage | File-based (no DB) | File-based (no DB) | ✅ |
| Hashing | Argon2 (t=3, m=32MB, p=4) | Argon2 (t=3, m=32MB, p=4) | ✅ |
| Player DB | In-memory + TOML/CSV persistence | In-memory + TOML/CSV persistence | ✅ |
| CSV types | 10 | 10 | ✅ |
| SSE | htmx SSE extension + event stream | htmx SSE extension + event stream | ✅ |
| Setup wizard | Multi-step state machine | Multi-step state machine + map dispatch | ✅ (improved) |
| Auth | JWT (5min expiry) + API-backed | JWT (5min expiry) + mock/file-backed | ✅ (simplified) |
| Mock mode | None | External deps only (API, uploader) | ✅ (added) |

### Improvements Over Source

1. **Test coverage: 67% vs 1%.** The rebuild has 5,358 lines of tests; the source has 132. This alone justifies the experiment.

2. **Known bugs not carried forward.** The source has a hash collision bug where `OrganizationPlayerIDHasher` doesn't include country/state in the hash computation. The rebuild doesn't replicate this — it uses a proper composite key.

3. **Mock mode for testing.** The source has no mock mode at all; you need a live idPair API to run it. The rebuild has a clean boundary between external dependencies (mocked) and internal pipeline (real).

4. **No CSS framework dependency.** The source uses Bootstrap 5 (large vendored JS/CSS). The rebuild uses custom design tokens — smaller footprint, no framework lock-in.

5. **Setup wizard uses map dispatch.** The source's setup wizard is a tangled mess of nested switches. The rebuild uses a clean map-based command dispatch pattern per the spec.

6. **Security hardening.** Token revocation on logout, path traversal protection, and proper session management were proactively added. The source has weaker session handling.

### What's Thinner

1. **No real API client.** The source integrates with the idPair API for service accounts, user login, MFA, and file upload. The rebuild is file-based only. This is by design — the specs intentionally omit the external service integration.

2. **No i18n/locale support.** The source has a locales directory. The rebuild doesn't.

3. **Fewer templ templates (8 vs 12).** The rebuild consolidated some pages.

4. **Simpler uploader.** Mock-only vs real HTTP upload to external service.

---

## Flywheel Driver Lessons

### What Broke and What We Fixed

| Issue | Root Cause | Fix |
|-------|-----------|-----|
| Nested session refusal | `CLAUDECODE` env var inherited by child | `filterEnv()` strips it |
| Trust dialog blocking | `--dangerously-skip-permissions` prompts | `--permission-mode bypassPermissions` |
| Exit 143 kills driver | SIGTERM propagates to parent | `signal.Ignore(SIGTERM, ...)` |
| Exit 144 kills driver | SIGUSR1 propagates to parent | `signal.Ignore(SIGUSR1, ...)` |
| TTY access broken | `Setpgid` puts child in separate group | Removed `Setpgid`, rely on signal.Ignore |
| Done file not created | `/exit` is CLI built-in, not agent tool | `watchForDone()` polls + kills via context |
| No crash recovery | Driver exits on any error | `runPhaseWithRetries()` retries N times |

### Current Driver Capabilities

- JSONL run logging with per-phase timing
- `--start` (stateless entry point) and `--resume` (log-driven automatic resume)
- `--retries` (default 2) for crash resilience
- Done-file watcher with git clean + push verification before termination
- Signal isolation (SIGTERM, SIGUSR1, SIGUSR2, SIGHUP ignored)
- Unlimited timeout support (`--timeout 0`)

---

## Key Lessons for Round 3

1. **Start with hardened UAT from day one.** The mid-experiment improvements (no BLOCKED verdicts, spec verification protocol, automated test harness strategy) landed too late to affect rounds 1-4. In round 3, these should be in place from the start.

2. **Specs need explicit "wire real implementation" mandates.** The builder's tendency to wire mock implementations for all code paths (not just mock mode) needs to be countered. Specs 10 and 11 should explicitly state: "In non-mock mode, RunningApp MUST use the real CSV worker pool, SSE broadcaster, player DB, and archive."

3. **UAT should check spec compliance in round 1, not round 4.** The spec verification protocol should mandate reading specs 10 and 11 before any testing. If the UAT had checked "does a real RunningApp exist?" first, the 3-round delay would have been avoided.

4. **The test harness is a multiplier.** 111+ automated test points in bash scripts saved enormous UAT context window. Round 3 should start the UAT with explicit instructions to build tooling first.

5. **The sub-agent dependency graph is the key innovation.** The builder analyzed spec dependency declarations, built a topological order, and delegated each spec to a fresh sub-agent. This is what made 15 specs implementable in 82 minutes. Preserve this pattern.

6. **Driver resilience matters for lights-out.** The signal isolation, retry logic, and crash recovery features were all discovered by necessity during this round. They're stable now and should need no changes for round 3.
