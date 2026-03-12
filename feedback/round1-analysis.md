# Round 1 Analysis

## Results Summary

- **UAT cycles:** 10-12 (estimated 2-3)
- **Builder tests:** 254 passing, race-clean
- **UAT verdict:** Accepted (all areas pass)
- **Human verdict:** Partial success — architecture solid, process needs major improvement

## Builder Assessment (Grade: B+)

### What worked well

- **Actor pattern** — properly implemented throughout. Application, SetupApp, RunningApp, ConcurrentPlayerDB all use single-goroutine-owns-state with typed command channels. Generic `chanutil` package is clean.
- **All 10 CSV types** — implemented with composable InColumnProcessor types, matching the source's column-mapping architecture.
- **Player DB** — file-backed with TOML header + CSV body, pepper hash validation, atomic writes, concurrent access wrapper.
- **Argon2 hashing** — correct parameters (time=3, mem=32MB, parallelism=4, output=32 bytes). PII never leaves the machine.
- **Name normalization** — iterative until stable, handles diacritics, hyphens, punctuation.
- **Config system** — TOML with With*() builder methods, NeedsSetup(), MergeSettableValues(). Clean immutable value type.
- **254 tests, race-clean** — solid coverage across state machine, CSV processing, auth, player DB, config.
- **Build infrastructure** — comprehensive build.sh with ldflags, templ/stringer auto-install, Docker support.
- **Mock mode** — full mock implementations of AuthProvider, Uploader, APIClient.

### Gaps compared to source (idpair-central)

1. **Real API client** — source has full apiclient/ with token acquisition, service account flows, endpoint testing, config fetching, per-CSV-type upload functions. Builder may be mock-only.
2. **Real uploader** — source has uploader/ with HTTP file streaming. Builder's MockUploader simulates progress but real uploader status unclear.
3. **Upload size discrepancy** — source limits to 1MB, builder uses 32MB, spec said 50MB.
4. **AWS S3 operations** — source has aws/ package. Not present in builder (may be behind Uploader interface).
5. **MFA support** — source has MFA-aware login. Builder's mock returns MFARequired() → false. Real auth flow unclear.
6. **i18n/locales** — source embeds locales/. Builder doesn't have this.
7. **File watcher** — source may watch upload directory for externally dropped files. Builder status unclear.

### Critical process failure

The builder never tested its own work before handing off to UAT. No curl smoke tests, no Playwright verification. This is the #1 reason for the 10-12 cycle count.

## UAT Assessment (Grade: C+)

### What worked

- Covered all 6 major test areas (build, CLI, unit tests, CSV processing, web UI, setup wizard)
- Found real bugs that triggered 4 fix commits (auth loop, SSE alignment, wizard navigation, config overwrite)
- Used both Playwright and curl
- Structured reporting with pass/fail verdicts

### Gaps

1. **Only tested mock mode** — didn't flag "real API integration untested" as a risk
2. **Shallow PII verification** — checked "no raw identifiers" but didn't verify same-player-same-hash consistency or different-player-different-hash uniqueness
3. **No edge cases** — no large files (spec says 500K rows), no concurrent uploads, no malformed CSV beyond "unknown headers", no UTF-8/BOM edge cases
4. **No round-trip testing** — gen-csv --inject-errors produces bad rows but UAT didn't upload those and verify error reporting
5. **No session management depth** — didn't test 5-minute expiry, 40-second extension window, stale tokens
6. **No SSE resilience** — didn't test reconnection, multiple tabs, mid-processing interruption
7. **Inconsistent and forgetful** — required human prompting to complete certain tests (context window issue)
8. **Not adversarial** — accepted dismissive responses from builder (session replay vulnerability waved away as "sessionless")

## Security Finding

The builder dismissed a session replay vulnerability as "it's sessionless — no biggie." This is unacceptable for any application with a login. The source project (idpair-central) has this same bug. Round 2 specs must explicitly require:
- Token revocation on logout
- Token expiry enforcement
- No token reuse after logout
- Security review as a mandatory gate before declaring done

## Key Lessons for Round 2

### For specs

1. **Task-sized specs (~300 lines)** — the 1190-line CSV pipeline spec was too large for a single context window
2. **Explicit dependency declarations** at the top of each spec
3. **Reference data in structured files** (TOML/JSON), not prose
4. **Test cases inline** with implementation tasks
5. **Interface signatures as contracts** — subagent needs only the interface + its spec

### For builder process

1. **"Taste the soup" principle** — builder MUST test its own work before declaring done
2. **Incremental smoke testing** — build → run → curl/Playwright → fix after each task
3. **Full self-UAT** before inviting UAT agent
4. **Code review + security review** as mandatory final gates
5. **Commit after each task** — not one giant commit at the end

### For UAT process

1. **Explicit checklist format** — not prose descriptions, mandatory pass/fail per item
2. **Adversarial test cases** — session replay, large files, concurrent uploads, malformed input
3. **"Don't accept until ALL items pass"** instruction
4. **"Push back on dismissive responses"** instruction
5. **Better structure to combat forgetfulness** — context window is the enemy here too

### For kickoff prompts

Builder: must include "test your own work before declaring done" and "run code review + security review"
UAT: must include "be adversarial" and "don't accept lazy explanations"
Both: must include "don't ask questions — make reasonable choices"
