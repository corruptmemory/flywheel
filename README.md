# Flywheel

Flywheel is an experiment in building software to build software: specs
in, no humans involved, software out. It is an exploration of the
software-factory idea (https://factory.strongdm.ai/), written in Go. The
repository holds the orchestration driver, the isolation layer (an MCP
broker, chroot jails, git enforcement) and the specs, prompts and
post-mortems from three complete build rounds.

## Status (October 2026)

- Three rounds complete: a Markdown spec set went in, a working Go web
  application came out, and each round's post-mortem fed the next
  (`feedback/`). The round-over-round table below has the numbers.
- The driver (`cmd/flywheel/`) runs Claude Code. The driver and the jail
  have nothing vendor-specific in them, so other agents could run through
  them; only Claude Code has so far.
- The isolation layer (`broker/`, `mcp/`, `jail/`, `gitops/`) is built and
  tested here, and `cmd/jail-duo/` demonstrates two jailed agents talking
  to each other only through the MCP broker. Wiring it into the round
  driver (Round 4 below) is where the project stopped.
- Why it stopped: the functionality explored here is becoming a commodity,
  with several techniques now reaching the same result; it was costly in
  time and tokens to build to this point; and it had taught what it was
  built to teach.

## Rules of Engagement

- No sharing of code from the source project, only and exclusively
  structured Markdown documents capturing instructions and
  specifications
- No "secret messages" that "unduly" bias the implementation to
  exactly replicate the source project. This said, you _can_ specify
  interfaces with textual descriptions for what is meant to be the
  logic and what the constraint and expectations are
- The "agents" that run the given instructions are not allowed to have
  access to or know of the existence of the source repositories that
  inspired the instructions they are following
- Don't assume that the agents that follow the instructions are
  `claude-code` agents. Your instructions and surrounding information
  should be robust enough for any frontier model to do the work:
  Claude, Gemini, or ChatGPT (codex)
- You can specify appropriate skills to use. In particular, your "go
  actor" skill will be quite useful in this context.

## Outputs

Each round produces a builder workspace and a UAT workspace:

| Round | Builder | UAT | Cycles | Builder | UAT | Status |
|-------|---------|-----|--------|---------|-----|--------|
| 1 | `~/projects/file-uploader` | `~/projects/file-uploader-uat` | 10-12 | B+ | C+ | Complete |
| 2 | `~/projects/file-uploader-r2` | `~/projects/file-uploader-r2-uat` | 5 | A- | A | Complete |
| 3 | `~/projects/file-uploader-r3` | `~/projects/file-uploader-r3-uat` | 2 | — | — | Complete (post-mortem in progress) |

### Round-over-Round Progress

| Metric | Round 1 | Round 2 |
|--------|---------|---------|
| UAT cycles to pass | 10-12 | 5 |
| Spec design | 8 topic-sized (~800+ lines) | 15 task-sized (~200-400 lines) |
| Sub-agent delegation | No | Yes (dependency graph) |
| UAT test automation | No | 111+ automated test points |
| UAT spec awareness | No | Mandatory spec citations |
| Go test coverage (lines) | Unknown | 5,358 (67% test-to-code ratio) |
| Total application code | Unknown | 17,569 lines |
| Build time (initial) | Unknown | 82 minutes |

See `feedback/round1.md`, `feedback/round1-analysis.md`, and
`feedback/round2-assessment.md` for detailed post-mortems.

Each builder workspace needs a `README.md`, `CLAUDE.md`, and
`build-specification/` directory. Each UAT workspace needs a
`README.md` with an adversarial checklist.

## Orchestration Driver

The `cmd/flywheel/` Go program runs a done-file-driven state machine that orchestrates the multi-phase pipeline:

```bash
go build -o flywheel ./cmd/flywheel/

# Round 3
./flywheel \
  --builder-dir ~/projects/file-uploader-r3 \
  --uat-dir ~/projects/file-uploader-r3-uat \
  --prompts-dir ~/projects/flywheel/prompts \
  --max-rounds 20 \
  --max-gate-retries 3 \
  --retries 2 \
  --skip-permissions --model opus
```

**Flags:**
- `--skip-permissions` — passes `--permission-mode bypassPermissions` to claude
- `--model` — model override (e.g., `sonnet` for smoke tests, `opus` for real builds)
- `--max-rounds` — max spec-level iterations (default: 20)
- `--max-gate-retries` — max times builder can fail same gate per spec, 0=unlimited (default: 3)
- `--timeout` — per-session wall-clock limit (default: 0 = unlimited)
- `--retries` — retries per phase if claude crashes without done file (default: 2)
- `--log-file` — JSONL timing log (default: `flywheel-runs.jsonl`)

**Pipeline (round 3):**

Per-spec quality gates: Build → Code Review → Security Review → Dev Test. All failures route back to Builder. After dev test PASS, advance to next spec. After all specs complete (UAT READY), run UAT. State is derived from done files on disk — always picks up where it left off.

**Resilience features:**
- Ignores SIGTERM/SIGUSR1/SIGUSR2/SIGHUP (child signals don't kill driver)
- Retries on crash (done-file is the authority, not exit code)
- Done-file watcher verifies git is clean and pushed before terminating session
- Per-phase JSONL logging with timing
- Filters `CLAUDECODE` env var to prevent nested-session detection

## Round 4: the isolation layer (built and demonstrated, not yet run as a round)

Round 4 is a major architectural evolution. See `feedback/round-4-ideas.md` for the full design and `docs/meeting-notes/2026-03-15-round4-*.md` for detailed discussion notes. The packages below exist in this repository with tests; `cmd/jail-duo/` runs two jailed agents against the broker; the round driver does not use them yet.

**Key changes:**

- **Mechanical isolation (built):** `broker/`, `mcp/`, `jail/` and `gitops/`, absorbed from the private `lightweight-containers` companion project (stress-tested to 76+ rounds). Each agent runs in a chroot jail with tailored filesystem projections: on unprivileged user namespaces, with the host toolchain bind-mounted read-only, no root needed. OS-level enforcement replaces prompt-based "don't touch X." Three-layer isolation: chroot (visibility) → git hooks (commit enforcement) → MCP (coordination enforcement).

- **MCP coordination layer:** Agents communicate with the pipeline exclusively through MCP tools backed by an in-process actor-based event broker. PO requests, done files, and check results are abstracted behind tool calls. The MCP enforces constraints mechanically (can't finish with outstanding PO requests, phase-appropriate done file format, identity isolation). Push-based events enable sub-second `wait_for_response` without polling.

- **Init/Prep convergence protocol:** Before any code is built, all agents simultaneously read specs, negotiate with PO, and build speculative tooling. Converges via pessimistic restart: any PO interaction triggers a full restart with accumulated amendments until everyone writes `UNDERSTAND`.

- **Product Owner agent:** Long-lived concurrent process with typed request protocol (CLARIFY, CONFLICT/AMBIGUITY, SUGGESTED IMPROVEMENT). Maintains specs via accumulating amendments. Communicates with agents exclusively through MCP-backed broker.

- **Tooling-driven reviews:** Reviewers build `run_checks.sh` during init/prep. The driver runs it mechanically before each review turn. Agents respond to tooling output rather than independently assessing the codebase.

- **Git workflow:** Multi-repo with per-role branches, `format-patch`/`am` transplant for clean linearized history, path filtering and squash support. Pre-commit hooks enforce per-role write restrictions (builder blocklist, PO/reviewer allowlists).

## Prior Art

Flywheel is part of the emerging "software factory" pattern. See `docs/software-factory-comparison.md` for a detailed comparison with StrongDM's Software Factory (https://factory.strongdm.ai/).

Key shared insight: individual AI agents are noisy but capable. The engineering challenge is designing systems that amplify signal over noise — adversarial quality gates, fresh contexts, mechanical boundaries. The system is reliable even when individual agents are not.

Inspired by Nate's Substack (Feb 2026) on the Opus 4.6 inflection point: https://natesnewsletter.substack.com/p/january-is-already-obsolete-my-honest

## Starting Point

We start by deep investigation of the source project: a Go web
application kept outside this repository, which the building agents
never see (see Rules of Engagement).

1. Load up your go-actor skill
2. Thoroughly investigate the source code for the source project, ask
   questions as needed.
3. There are some "example CSVs" for it, kept outside this repository,
   some of them good and some of them deliberately bad. The
   "building agents" are allowed to know about these files. Encourage
   your "building agent" to build tooling to generate synthetic CSVs,
   both valid and invalid for testing. This tooling can be used
   internally but should also be exposed as CLI tools so users can
   generate data on demand for testing (especially UAT!!!)
4. We are NOT looking for "bug-for-bug" compatibility; instead if you,
   in your analysis discover some latent bugs, surface them. We will
   discuss what _should_ be built instead.
5. Improve on the existing codebase:
     a. Add tests
     b. Use more interfaces to allow for mocking "connecting" things
        to enable sensible unit testing
	 c. Improve on some of the code and abstractions in the existing
        code base. You'll notice that the "setup wizard" code is a
        tangled mess, see if you can give hints to the "building
        agent" to do better than this.  Also, improve on the
        aesthetics.

## Constraints

Only use my "preferred" Go web app baseline tooling. Also note: this
project does not use a database, this is on purpose -- data is stored
on files.

## Evaluation

There will be 3 levels of evaluation of the output:

1. The "automated UAT" _must pass_. The automated UAT can leave
   document artifacts in the `uat-feedback` directory under
   `~/projects/file-uploader`.  The `README.md` in
   `~/projects/file-uploader` should tell the "building agent" to look
   for feedback there.
2. Once "automated UAT" is done, you, in this directory will be given
   a chance to review the work products of both the "building agents"
   and the UAT agent. If you find their work lacking, update any/all
   documents in one or both `~/projects/file-uploader` and
   `~/projects/file-uploader-uat`. We will then have the "build and
   UAT" agents re-run and update. Since you know the source project
   you're comparing against, it's your job to ensure that what you
   _think_ you communicated accurately reflected what you know about
   the source project. Clearly we're not looking for a `diff` with no
   outputs. Logically the work product of the "builder agents" needs
   to be sufficiently "the same" as to be arguably acceptable.
3. I'll review the final product and give feedback.  This may result
   in more iterations.
   
As you can see, the goal is to make me happy with the final
product. Only then are we done.
