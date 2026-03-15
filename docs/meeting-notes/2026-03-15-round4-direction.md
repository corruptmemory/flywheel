# Round 4 Direction Discussion — 2026-03-15

## Context

Late-night discussion after reviewing both the flywheel project and the lightweight-containers companion project (`~/projects/lightweight-containers`). No code written — planning and direction-setting only.

## Key Decisions

### 1. lightweight-containers integration

The `lightweight-containers` project will be integrated into flywheel for round 4. The exact relationship is TBD — could be consumed as a library/CLI, or adapted and merged directly. Both projects are Go, share conventions, and were designed to fit together.

lightweight-containers provides:
- OS-level agent isolation via chroot jails with bind mounts (two implementations: root-based and unprivileged user namespaces)
- Per-agent filesystem projections (builder can't see reviewer tooling, reviewers can't write source code)
- Multi-repo git workflow with `format-patch`/`am` transplant for clean linearized history
- Product Owner as a new long-lived concurrent agent with file-based request/response protocol
- Three-layer isolation: chroot (visibility) → git hooks (commit enforcement) → patch filtering (planned)
- tmux multi-pane layout for observability

Current state: working prototype with stub agents, stress-tested to 76+ rounds. Next step is replacing stubs with real Claude Code sessions.

### 2. Tooling-driven agent model (major architectural shift)

The fundamental change for round 4: agents are no longer judges, they are respondents.

**Current model (rounds 1-3):** Agents assess the codebase and render verdicts. This is a judgment call, and judgment calls are where context pressure causes agents to cut corners.

**New model (round 4):** Each agent is driven by their own automation tooling:

1. **Speculative tooling derived from specs.** Before any agent runs, automated checks are generated that encode what MUST be true at each pipeline stage. These checks are derived from the build specifications.

2. **Flywheel runs the tooling mechanically.** The driver executes each agent's tooling and feeds the output as input to the agent's round. No agent can "forget" to run their tests. No agent can skip inconvenient results.

3. **Agent's job is to respond to tooling output.** Not to independently assess the codebase. The tooling output IS their input.

4. **Failures are true until proven otherwise.** The default assumption is that a tooling failure is a real problem.

5. **One escape hatch: detection criteria mismatch.** If a check fails, the agent may update HOW it detects something (matching criteria only). It may NOT change WHAT it's checking for.

**Example:** The QA's script says "by spec 08 there should be a `RunningApp` implementation wired into the `Application`." If there isn't one — FAIL, no guessing, no judgment call. The only thing that might change is the particulars of how to _prove_ the RunningApp is wired in and available (e.g., grep pattern, function signature check), but _that_ it must exist and must be wired in is non-negotiable.

**Invariant/detection split:**
- **Invariants** (what must be true) — derived from specs, never changeable by agents
- **Detection criteria** (how to prove it) — updatable by agents when reality doesn't match expectations

This is analogous to property-based testing: the property is sacred, the test oracle is negotiable.

### 3. Prompt engineering is a major workstream

Each agent's prompts need significant rework for round 4. This is a known upcoming discussion topic, not yet started.

## What's NOT decided yet

- Exact integration approach (library, CLI, merge)
- Product Owner's role and visibility scope
- How speculative tooling is generated from specs
- Prompt content for each agent
- Whether round 4 targets the same file-uploader project or a new one

## Next Steps

- Discuss integration approach in detail
- Design the speculative tooling generation process
- Rework prompts for each agent role under the new model
