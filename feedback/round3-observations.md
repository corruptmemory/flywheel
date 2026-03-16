# Round 3 Observations (In Progress)

**Status:** Post-mortem in progress. These are observations from live monitoring and log analysis. Not yet a complete assessment.

## Pipeline Stats

- **15 specs completed**, UAT passed in 2 rounds
- **6 driver launches** to reach PASS (bugs in driver, pgrep self-kill, gate retry exhaustion)
- **112 total phases** (40 builds, 27 code reviews, 25 security reviews, 18 dev tests, 2 UAT)
- **Total wall-clock time:** ~11 hours across all launches

## Driver Bugs Found and Fixed During Round 3

1. **Builder won't stop after one spec** — prompt said "STOP" but agent kept going. Fixed with immediate kill on done file detection.
2. **State machine routing after dev-test PASS** — `clearInnerLoopDoneFiles` in post-processing wiped state before `determineNextPhase` could read it. Fixed by moving cleanup to `prePhaseCleanup`.
3. **Git clean check too strict** — builder writes `progress` after final commit. Fixed by allowing state files through the clean check (later removed entirely with immediate kill).
4. **Done file not cleared on fix scenarios** — old `code_review_done` FAIL stayed on disk, causing infinite loop back to builder. Fixed by clearing triggering done file in `prePhaseCleanup`.
5. **`--max-rounds` counting phases, not spec cycles** — 20 rounds meant ~5 specs, not 20. Fixed to count UAT invocations.
6. **JSONL log truncated on every flush** — `O_TRUNC` in `flushRunLog` lost data. Fixed with append-only per-phase logging.
7. **`pgrep` self-kill** — entire prompt passed as CLI arg; security reviewer's `pgrep -f "file-uploader --mock"` matched the claude process and killed it. Fixed by writing prompt to file.
8. **Module name leakage** — `go.mod` and spec 01 referenced `file-uploader-r2` instead of `r3`.

## Agent Behavior Observations

### Builder

- **placeholderRunningApp persisted for 12 commits** across specs 11-15. Introduced at spec 11 (auth/server wiring), never replaced until UAT caught it. Same bug as round 2. All three review gates missed it because the placeholder satisfied all interfaces and compiled.
- **Builder is too deferential to reviewers.** When security reviewer said "use mutex," builder complied despite project rules requiring actor pattern. When security reviewer said "POST for logout," builder complied, then when UAT said "spec says GET," builder flipped it back. Builder should cross-check review findings against specs.
- **Builder modified dev tester's test harness** — changed `grep` patterns in `test-harness/spec-06-csv-type-definitions.sh` to match its error messages rather than fixing error messages to match the tests. Made the tests fit the code instead of the code fit the tests. This is the behavior StrongDM warned about: agents will game tests if they can reach them. No malice — but the builder optimized for passing tests rather than correctness.

### Code Reviewer

- **Most active gate** — found issues on almost every spec (gofmt, dead code, spec compliance).
- **"Lane crossing"** — discovered and ran the security reviewer's harness scripts. Not harmful but duplicates work and can produce cross-domain feedback.
- **Did not catch mutexes** for 10 commits (specs 07-14). The "no mutex" rule wasn't in its prompt.

### Security Reviewer

- **Built substantial test automation** — 1,752 lines across 10 probe scripts, ~61 curl probes, ~241 assertion checkpoints.
- **Inconsistent use of own tooling** — sometimes ran all harness scripts, sometimes ran individual ones, sometimes rationalized skipping them. Dev tester was more mechanically disciplined.
- **5-FAIL death spiral on spec 15** — hit `--max-gate-retries` cap. Combination of genuine findings + pgrep self-kill + fresh context not remembering previous approvals.
- **Prescribed wrong fix (mutex instead of actor)** on spec 07. Builder complied without pushback.
- **GET logout inconsistency** — flagged GET logout as a vulnerability, builder fixed to POST. Later UAT demanded GET per spec, builder reverted. Security reviewer didn't re-flag it on subsequent pass despite having a harness probe that checks for it.

### Dev Tester

- **Most disciplined agent** — consistently ran `for (scripts) { run }` pattern over all test harnesses.
- **Only failed once** (spec 11) — by the time code passes CR and SR, it's usually solid.
- **Caught the mutex violation** at spec 14 that code review and security review missed.

### UAT

- **Passed in 2 rounds** (vs 5 in round 2, 10-12 in round 1).
- **First round caught:** placeholderRunningApp (CRIT), GET/POST logout mismatch (HIGH), blank settings in mock mode (MED).
- **Built 106 automated tests + 12 manual browser tests.**
- **965-line test harness** — substantial improvement over round 2.

## Actionable Prompt Refinements

### Builder fix scenarios (all)
- Cross-check every review finding against the relevant spec before implementing
- If a finding contradicts the spec, do NOT change the code — add a comment citing the spec
- If a finding genuinely improves on the spec (real security concern), implement the fix AND note the spec deviation in `progress`

### Code review prompt
- Add explicit check: "Verify all concurrency uses the goroutine actor pattern — no sync.Mutex in production code"
- Add: "Do not run or evaluate security test harnesses — that's the security reviewer's job"

### Security review prompt
- Add: "Before writing your verdict, run ALL scripts in `security-harness/`. Do not skip any."
- Or: create `security-harness/run-all.sh` and mandate running it

### Spec 11 (auth)
- Change logout from GET to POST in the route table (the security reviewer was right)

## Agent Isolation (Round 4 Priority)

Three observed lane-crossing incidents:
1. **Code reviewer ran security harness** — discovered scripts in `security-harness/`, executed them, produced cross-domain feedback
2. **Builder modified dev tester's test harness** — changed test assertions to match its code instead of fixing code to match tests
3. **Security reviewer prescribed mutexes** — dictated an implementation pattern (mutex) that violated project rules (actor pattern)

**Pattern:** Agents are too clever to be given unrestricted access to each other's artifacts. No malice attributed — they're optimizing within their capabilities. But the result is: tests get gamed, roles blur, and accountability breaks down.

**Round 4 should introduce mechanical isolation:**
- Builder CANNOT write to `test-harness/`, `security-harness/`, review feedback directories
- Review agents CANNOT modify code or each other's harnesses
- Possible approaches: filesystem permissions, git worktrees, separate directories per agent, or driver-enforced file access rules
- The goal is not maximum restriction but minimum viable isolation — agents can READ across boundaries but not WRITE

**Design principle:** Trust agents to do their job within their lane. Don't trust them to stay in their lane voluntarily. The same squishiness that makes them capable makes them creative about shortcuts. Mechanical boundaries, not prompt discipline, are the answer.

## Open Questions

- Was the real RunningApp built during a session and then lost to a crash/kill/fresh context? User observed it being built but git history only shows placeholder.
- Should review agents have access to previous review findings (institutional memory)?
- Should the driver enforce harness execution before accepting a review verdict?
- Is the security reviewer's creative/adversarial posture compatible with mechanical discipline? May need a two-phase approach: run all harness scripts first, then do ad-hoc probing.
- What is the right isolation granularity? Per-directory ACLs? Git worktrees? Separate repos? Each has tradeoffs in complexity vs effectiveness.
