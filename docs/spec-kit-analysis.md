# Spec-Kit Analysis

**Source:** https://github.com/github/spec-kit
**Reviewed:** 2026-03-26

## What It Is

GitHub's open-source toolkit for "Spec-Driven Development" (SDD). A Python CLI (`specify-cli`) that provides a structured workflow for LLM-agent-driven software development. 82.6k stars — significant traction.

## The 6-Phase Workflow

1. **Constitution** — project principles, quality standards, testing expectations (like a CLAUDE.md on steroids)
2. **Specify** — describe *what* and *why* (product scenarios, not implementation details)
3. **Plan** — technical architecture, stack decisions, design choices
4. **Tasks** — break the plan into granular implementation units
5. **Implement** — AI agents execute tasks guided by the accumulated spec artifacts
6. **Verify/Retrospect** — validate implementation against specs, detect drift

## Artifact Structure

```
constitution.md    — governing principles
specs.md           — feature descriptions (what/why)
plan.md            — technical approach (how)
tasks.md           — itemized work items
docs/              — additional context
```

## Key Features

- **Agent-agnostic:** Works with 25+ AI coding agents (Claude Code, Copilot, Cursor, Gemini, etc.)
- **Extension ecosystem:** 28+ community extensions for code review, drift detection, Jira sync
- **Traceability:** Every task explicitly references the spec section it implements
- **Presets:** Customize terminology and workflows without changing tooling

## Comparison with Flywheel

| Aspect | Spec-Kit | Flywheel |
|--------|----------|----------|
| **Focus** | Spec authoring + single-agent implementation | Multi-agent execution with quality gates |
| **Constitution** | Formalized as separate artifact | Spread across CLAUDE.md + builder-base.md |
| **Specs** | specs.md (monolithic per feature) | 15 task-sized specs with dependency declarations |
| **Quality gates** | Post-hoc verify/retrospect | Per-spec: code review → security → dev test |
| **Agent model** | Single agent implements | Specialized concurrent agents (builder, reviewers, PO, UAT) |
| **Isolation** | None — prompt discipline only | Mechanical: chroot jails, git hooks, MCP enforcement |
| **Orchestration** | CLI generates scaffolding + prompts | Done-file state machine with crash recovery, resume, retry |
| **Agent support** | 25+ agents | Claude Code specific |
| **Extensions** | 28+ community extensions | Closed system |
| **Traceability** | Structural (task → spec section) | Loose (progress file) |

### Where Spec-Kit Is Stronger

1. **Constitution concept** — more formalized than our CLAUDE.md. Explicitly captures quality standards, testing expectations, and UX guidelines as a distinct, referenced artifact.
2. **Agent-agnostic design** — works with 25+ agents. Flywheel is Claude Code-specific.
3. **Extension ecosystem** — community contributions for review, drift detection, integrations.
4. **Traceability** — structural references from tasks to spec sections.

### Where Flywheel Is Stronger

1. **Adversarial/cooperative review loop** — per-spec quality gates catch issues before the next spec starts, preventing bug accumulation. Spec-kit's verify is post-hoc only.
2. **Mechanical isolation** — OS-level enforcement (jails, git hooks, MCP) prevents agents from gaming tests or crossing lanes. Spec-kit relies on prompt discipline.
3. **Driver/state machine** — automatic crash recovery, resume, retry limits, phase routing. Spec-kit is a CLI without runtime orchestration.
4. **Multi-agent specialization** — concurrent agents with distinct mandates vs single-agent implementation.
5. **Empirically validated** — 3 rounds of actual execution with detailed post-mortems.

## How They Could Work Together

1. **Spec-kit upstream, flywheel downstream.** Spec-kit's constitution → specify → plan → tasks pipeline produces the `build-specification/` directory that flywheel's agents consume. This solves the "greenfield spec problem" — how to go from a product idea to task-sized specs without a reference implementation.
2. **Adopt the constitution pattern.** Formalize CLAUDE.md + builder-base.md as a spec-kit-style constitution that all agents reference.
3. **Steal the traceability model.** Make the progress file explicitly cite spec sections per task.
4. **Spec-kit's extension model for flywheel.** Community-contributed review checks, drift detection, etc. could inform the "speculative tooling" approach in round 4.

## Relevance to Evolving Agent Model

Spec-kit's "verify/retrospect" phase is essentially our UAT — but weaker because it's single-pass and post-hoc. The real insight from spec-kit is the **constitution as a shared reference point**. All agents (builder, reviewers, PO) should derive their behavior from the same constitution, not from individually crafted prompts. This aligns with the emerging thinking about cooperative agents with aligned incentives rather than purely adversarial postures.

## Emerging Direction: Cooperative Agents with Aligned Incentives

The current flywheel model uses "adversarial" review agents. Spec-kit's model is closer to "cooperative" — everyone works from the same spec toward the same goal. The next evolution may combine both:

**Objective functions the team optimizes together:**

1. **Minimize UAT passes** — ideally, the product passes on the first UAT review
2. **Maximize user effectiveness** — given a scenario, minimize steps needed to accomplish the goal. Requires having scenarios (spec-kit's approach). Constitution + specs cover this ground.
3. **Minimize bugs encountered by users** — QA testing targets zero user-facing bugs
4. **Minimize security vulnerabilities** — the deployed system should address all reasonable threats

**The shift:** Agents aren't "scoring points" adversarially (which leads to progressively more persnickety code review, for instance). They're a team optimizing objective functions that matter to end users. The builder, QA, security reviewer, code reviewer, and PO all share aligned incentives — they succeed or fail together based on UAT outcome and user satisfaction.

This requires fundamentally different prompt "attitudes" for each agent:
- Not "find problems" but "ensure the product meets the objective functions"
- Not "fail the builder" but "catch issues early so the team succeeds"
- Not "be adversarial" but "be constructively critical with the shared goal in mind"

The constitution becomes the shared agreement that aligns all agents' behavior toward the same objectives.
