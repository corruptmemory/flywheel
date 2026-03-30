# Peanut Gallery Roadmap

**Date:** 2026-03-29
**Status:** Planning

Incremental path from two-agent prototype to full round 4 pipeline. Each stage adds one layer of complexity on proven foundations.

## Tmux Approach

One tmux session, one window per agent. Switch between inmates with `Ctrl-B n/p` or window number. No split panes, no concurrent visibility needed. Simplifies the driver's tmux management to `new-window` per agent.

```
[PO] [Builder] [QA] [CR] [SEC] [UAT]
 └── tmux windows, one per jailed agent
```

## Peanut Gallery 1: All Players, One Turn Each

**Goal:** Prove 6 agents can run concurrently in separate jails with MCP coordination.

**Sequence:**
1. PO starts (long-lived)
2. Builder starts, asks a question, builds something simple
3. QA, CR, SEC start concurrently — each looks at code, asks PO a question, approves on first run
4. UAT starts, looks at output, accepts on first try

**What it proves:** 6 jails, 6 MCP servers, one broker, tmux with 6 windows, all agents communicate and write_done.

**Curl hack:** Still viable for one turn each.

## Peanut Gallery 2: Add Prep Phase

**Goal:** Test the init/prep convergence protocol.

**Sequence:**
1. PO starts
2. ALL reviewers (B, QA, CR, SEC) start simultaneously for prep phase
3. Each reads specs, asks PO questions if needed
4. Each writes UNDERSTAND or DISCUSSION to their init status
5. Driver checks: any DISCUSSION? → restart all with accumulated amendments
6. Converges when all write UNDERSTAND in same round
7. Then do one-turn build loop like PG1

**What it proves:** Multi-round convergence protocol, spec amendment accumulation, fresh-start-on-discussion.

## Peanut Gallery 3: Multiple Review Turns

**Goal:** Test the full feedback loop with speculative tooling.

**Sequence:**
1. Prep phase (from PG2)
2. Builder implements (small project)
3. Reviewers run — each has `run_checks.sh` built during prep
4. Driver runs `run_checks.sh` mechanically before each reviewer turn
5. Reviewers may FAIL → Builder fixes → reviewers re-run
6. Multiple rounds until all PASS
7. UAT

**What it proves:** Speculative tooling, mechanical check execution, feedback loops, the invariant/detection split.

**Prerequisites:**
- **stdio MCP bridge** — curl hack won't scale to 5+ agents with complex multi-turn prompts. Build between PG2 and PG3.
- **run_checks.sh pattern** — reviewers build tooling during prep, driver runs it before each review turn

## Peanut Gallery 4: Full Prompt Rework

**Goal:** Production dress rehearsal with refined prompts and cooperative agent model.

**Sequence:** Same mechanics as PG3 but with:
- Cooperative prompts (objective functions, not adversarial postures)
- Full spec set (the file-uploader specs or a new project)
- Constitution-style shared reference (spec-kit influence)

**What it proves:** The prompt design works at scale. Agents optimize together for:
1. Minimize UAT passes (first-pass acceptance)
2. Maximize user effectiveness (minimize steps per scenario)
3. Minimize bugs (QA target: zero)
4. Minimize security vulnerabilities (target: zero)

## Dependency Chain

```
PG1 (6 agents, one turn)
  │
  ├── PG2 needs: prep phase convergence loop in driver
  │
  └── PG2 (prep phase + one turn)
        │
        ├── PG3 needs: stdio MCP bridge, run_checks.sh pattern
        │
        └── PG3 (multiple review turns)
              │
              ├── PG4 needs: full prompt rework, cooperative model
              │
              └── PG4 (production dress rehearsal)
```

## Key Technical Milestones

| Between | What to Build |
|---------|--------------|
| PG1 → PG2 | Init/prep convergence loop in driver, UNDERSTAND/DISCUSSION protocol |
| PG2 → PG3 | stdio MCP bridge (small Go binary: stdin/stdout ↔ Unix socket HTTP), run_checks.sh driver integration |
| PG3 → PG4 | Full prompt rework, constitution document, cooperative objective functions |
