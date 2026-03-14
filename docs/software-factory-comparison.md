# Software Factory Comparison: Flywheel vs StrongDM

## Context

StrongDM published a production framework called "Software Factory" (https://factory.strongdm.ai/) built around fully autonomous agent-driven development. Nate's Substack article (Feb 2026) on the Opus 4.6 release discusses the broader "software factory" pattern emerging across the industry.

This document compares Flywheel's approach with StrongDM's, identifies shared principles, and captures ideas worth incorporating in future iterations.

## StrongDM's Approach

**Core philosophy:** "Code must not be written by humans. Code must not be reviewed by humans." They call this "non-interactive development" or "grown software."

**Key components:**

1. **Specs + Scenarios drive agent behavior.** Traditional tests proved insufficient because agents would "return true" to pass narrowly-written tests. StrongDM repurposed "scenario" language for end-to-end user stories — often stored outside the codebase (like "holdout" validation sets in ML) — flexibly validated by LLMs and less vulnerable to reward hacking.

2. **Satisfaction metrics over boolean tests.** Rather than PASS/FAIL, they measure: "of all observed trajectories through all scenarios, what fraction likely satisfy the user?" This probabilistic approach accounts for the non-determinism of agentic systems.

3. **Digital Twin Universe (DTU).** Behavioral clones of third-party services (Okta, Jira, Slack, Google Docs) replicating APIs, edge cases, and failure modes. Enables testing at volumes exceeding production limits without rate limits or API costs.

4. **"Deliberate naivete."** Removing habits and constraints from traditional development. Creating high-fidelity SaaS clones was previously economically infeasible; with AI it's "routine."

5. **Economics:** They suggest solid software factories require spending "at least $1,000 on tokens daily per human engineer."

**Timeline:** Team formed July 2025. Triggered by Claude 3.5 revision 2 (Oct 2024) demonstrating that long-horizon agentic coding "compounds correctness rather than error."

## Convergent Design Patterns

Both projects independently arrived at strikingly similar architectures:

| Pattern | StrongDM | Flywheel |
|---------|----------|----------|
| Specs drive agents | Specs + Scenarios | `build-specification/` (15 task-sized specs) |
| Automated validation | Harnesses validate scenarios | Adversarial review agents (code, security, dev-test) |
| Convergence without human review | Agents converge autonomously | Pipeline converges per-spec through quality gates |
| Anti-gaming measures | Scenarios stored outside codebase | Adversarial posture + separate UAT workspace |
| Agent specialization | Implied but vague | Explicit: builder, code reviewer, security reviewer, dev tester, UAT |
| Correctness accumulation | "Compounding correctness rather than error" | Fresh contexts + adversarial diversity = noise averaging toward spec alignment |

## Where StrongDM Goes Further

1. **No human review at all.** Flywheel still has a human evaluation step. StrongDM has closed the loop entirely — the logical endpoint if review agents become reliable enough.

2. **Probabilistic satisfaction vs boolean PASS/FAIL.** A confidence score across multiple trajectories is more nuanced than a single PASS/FAIL verdict. Worth considering for the UAT phase.

3. **Digital Twin Universe.** Sophisticated mock infrastructure for third-party services. Flywheel currently uses `--mock` mode with simpler mocks. DTU is the intermediate step between mock mode and live API testing.

4. **Holdout validation sets.** Scenarios stored outside the codebase prevent agents from gaming tests by reading the test expectations. Flywheel's UAT workspace is separate from the builder workspace — already half this pattern.

## Where Flywheel Goes Further

1. **Concrete orchestration mechanics.** StrongDM's architecture description is vague on coordination. Flywheel has a concrete, debuggable state machine (done-file-driven, deterministic phase routing).

2. **Per-spec quality gates.** Every individual spec passes code review, security review, and dev test before the next spec starts. This prevents bug accumulation across specs.

3. **Adversarial specialization.** Dedicated agents with distinct adversarial mandates (code quality vs security vs functional testing). The role separation prevents "squishy" agents from drifting into each other's domains.

4. **Resume/crash recovery.** The done-file state machine enables resume from any point without data loss. JSONL per-phase logging preserves timing data across kills.

5. **Token efficiency.** Fresh context per agent invocation, immediate session kill on done file, task-sized specs — all designed to minimize token waste. StrongDM's $1,000/day budget suggests less concern with efficiency.

## Ideas for Future Flywheel Iterations

### From StrongDM

- **Satisfaction metrics:** Replace binary PASS/FAIL in UAT with a probabilistic confidence score across multiple test trajectories.
- **Holdout scenarios:** Store some acceptance criteria only in the UAT workspace, invisible to the builder. Prevents gaming.
- **Digital Twin Universe:** Build behavioral clones of the real APIs (can run locally) for integration testing at scale before connecting to live services.

### From Broader Industry (Nate's Article)

- **Planning engine:** An agent layer above the flywheel that takes a product description and produces the task-sized specs with dependency graphs. This solves the greenfield spec problem.
- **Agent-to-human ratio optimization:** As the pipeline matures, identify which human touchpoints can be eliminated and which remain essential.
- **Multi-project reuse:** The driver is already generic (swap prompts and specs for any project). Validate this by running it on a second, different project.

## Key Insight

Both approaches converge on the same fundamental insight: individual AI agents are noisy but capable. The engineering challenge is not making agents smarter — it's designing systems that amplify signal over noise. StrongDM uses scenarios and satisfaction metrics. Flywheel uses adversarial quality gates and mechanical boundaries. Both work because the system is reliable even when individual agents are not.

The vagueness of StrongDM's coordination mechanics is either genuine IP worth protecting, or common-sense engineering wrapped in AI mystique. The done-file state machine is not glamorous, but it is debuggable, resumable, and works.

## References

- StrongDM Software Factory: https://factory.strongdm.ai/
- Nate's Substack (Feb 2026): https://natesnewsletter.substack.com/p/january-is-already-obsolete-my-honest
- Flywheel design doc: `docs/plans/2026-03-13-round3-multi-phase-pipeline-design.md`
