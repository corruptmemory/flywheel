# Round 4: MCP Communication Layer and Enforcement Design — 2026-03-15

## Context

Second session on round 4 direction. Followed up on the previous night's discussion about tooling-driven agents and mechanical isolation. This session focused on the concrete communication and enforcement architecture.

## Decisions Made

### 1. Absorb lightweight-containers packages, discard prototype driver

**Decision:** Copy `jail/`, `gitops/`, `seqsock/` into flywheel as packages. Do NOT absorb `internal/driver/` — it's a prototype that proved patterns but will be superseded by flywheel's more mature driver.

**Reasoning:**
- The library packages (`jail/`, `gitops/`, `seqsock/`) are clean, exported, well-tested, and general-purpose
- The prototype driver (`internal/driver/`) embodies a simpler orchestration model than round 4 needs
- Flywheel's existing driver already has battle-tested features the prototype lacks: gate retry tracking, JSONL logging, crash recovery, resume, scenario-based prompt assembly
- Round 4's state machine (init/prep loop + build loop + tooling-driven reviews) requires tight coupling between orchestration and isolation — this fights a clean library API boundary
- No second consumer exists to justify maintaining a separate library

lightweight-containers stays as a historical reference and stub-based test harness.

### 2. MCP server as the agent coordination layer

**Decision:** Each agent communicates with the pipeline exclusively through MCP tools. The MCP server abstracts file-based communication behind tool calls that Claude Code agents already know how to use.

**Why MCP:**
- Agents don't need to know file naming conventions, directory paths, or done file formats
- The MCP enforces "you only see your traffic" at the protocol level (complementing jail-level filesystem isolation)
- Works within Claude Code's existing tool-use model
- Under the hood it's still files on disk — the driver can observe everything without being in the protocol

**Per-role tool surface:**

All agents (Builder, Reviewers, UAT):
```
send_request(type, content)         → async: creates NNNNN-request-{role}.md
wait_for_response(request_id)       → sync: blocks until PO replies
check_response(request_id)          → async: non-blocking check
list_my_requests()                  → shows this role's request/response history
write_done(status, details)         → writes done file (REFUSES if outstanding PO requests)
```

Product Owner:
```
list_pending_requests()             → finds all unanswered requests
read_request(request_id)            → reads request content
send_response(request_id, content)  → writes response
create_amendment(spec, content)     → writes numbered amendment file
list_amendments()                   → shows all amendments
write_done(status, details)         → PO's own done file
```

The `NNNNN` sequence numbers come from the seqsock server — globally ordered, no collisions.

**Sync vs async:** Agents choose. `wait_for_response` blocks within the turn. `send_request` + later `check_response` is non-blocking. The prompt tells agents: "you cannot write your done file without examining responses to all outstanding PO requests." The MCP enforces this mechanically — `write_done` refuses if outstanding requests exist.

**Done files in the MCP:** Yes. If the MCP handles PO communication, it should also handle done files. This means:
- The MCP can enforce the "no outstanding requests" constraint mechanically
- Agents never need to know done file paths, names, or formats
- Phase-aware validation (init/prep accepts UNDERSTAND/DISCUSSION; build accepts PASS/FAIL/spec status)
- The driver's observation surface stays the same — it polls for files in `handoff/`

### 3. Init/Prep convergence protocol

**Decision:** Parallel agents with pessimistic restart on any discussion.

**Mechanics:**
1. PO is started and stays running
2. All other agents (Builder, Reviewers, UAT) start simultaneously
3. Each reads the full specification set
4. If an agent fully understands without PO interaction → writes `UNDERSTAND` in `init_understand_{role}`
5. If an agent had ANY PO interaction during the turn → writes `DISCUSSION` in `init_understand_{role}`
6. If ANY `init_understand_*` contains `DISCUSSION` → ALL agents restart with fresh, complete spec snapshot (including all accumulated amendments)
7. Converges when all agents write `UNDERSTAND` in the same round

**Properties:**
- Correctness: nobody proceeds with a stale spec view
- Convergence: amendments accumulate, so question surface shrinks each round
- Simplicity: driver logic is trivial — start all, wait for all, check for DISCUSSION, restart or proceed
- Needs a cap: `--max-init-rounds` to catch specs that are too ambiguous for automated resolution

**During init/prep, reviewers build speculative tooling:**
- Each reviewer produces a `run_checks.sh` covering their entire checklist
- This tooling is the reviewer's "best guess" automated verification for the full spec set
- In the build loop, the driver runs this tooling mechanically before each reviewer turn

### 4. PO request protocol types

From `feedback/round-4-ideas.md`:
- **PLEASE CLARIFY** — request to clarify specifications
- **POTENTIAL SPEC CONFLICT/AMBIGUITY** — agent believes specs are contradictory or ambiguous
- **SUGGESTED IMPROVEMENT** — agent identified an opportunity to improve the product

PO response obligations:
- MUST respond to every request
- DENIED responses require full reasoning
- ACCEPTED responses must be self-contained (agents can't see updated specs intra-turn)
- Accepted changes result in amendment files (numbered, accumulating, cross-referencing)

### 5. Enforcement boundary: coordination yes, judgment no

**Decision:** Mechanical enforcement for process/coordination. Trust trained intent for judgment quality.

**Enforce mechanically:**
- Identity (can't impersonate another role)
- Phase-appropriate done file format
- Can't finish with outstanding PO requests
- Correct request/response file naming
- Spec snapshot visibility (amendments invisible intra-turn)
- PO must respond to every request

**Trust to trained intent:**
- Quality of code, reviews, and automated tooling
- Honest triage of `run_checks.sh` results
- Adversarial posture
- Spec compliance judgment
- Meaningful (not gaming) automated checks

**Why:** Over-enforcing creates Goodhart's Law problems. If you require agents to "address every `run_checks.sh` failure" mechanically, they write all-passing checks or trivially acknowledge failures. You can't mechanically enforce "do the right thing." The bet is on the model's trained intent to make meaningful progress.

## Phased Implementation Plan

```
Phase 0 ──→ Phase 1 ──→ Phase 2 ──→ Phase 5
                    ╲        ╲
                     → Phase 3 → Phase 4
```

- **Phase 0: Foundation** — absorb jail/gitops/seqsock into flywheel, verify tests
- **Phase 1: Workspace and isolation** — adapt InitWorkspace, wire jails into driver, add CLI flags, tmux
- **Phase 2: State machine redesign** — init/prep loop, build loop with new routing, driver runs run_checks.sh
- **Phase 3: MCP server and PO** — typed request protocol, amendment management, done file enforcement, PO lifecycle
- **Phase 4: Prompt engineering** — rework all agent prompts for tooling-driven model
- **Phase 5: Smoke run** — replace stubs with real Claude Code sessions, discover new problems

Phases 2 and 3 can overlap. Phase 4 can start once 2 and 3 are sketched. Phase 5 needs everything.

## Open Items

- Exact structure of amendment files and numbering scheme
- How `run_checks.sh` tooling gets seeded during init/prep (what's the spec-to-tooling translation?)
- MCP server architecture: one per agent, or shared instance with role tokens?
- tmux + jails integration (currently separate code paths in lightweight-containers)
- Node.js/Claude Code runtime challenges inside chroot jails (network access, `~/.claude/` config)
- Whether round 4 targets the same file-uploader project or a new one
