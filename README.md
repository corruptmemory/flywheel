# Flywheel

This is not a _true_ coding project, this is a "meta project," we're
going to write specs and documents that will be used by an independent
agent to do all the coding in a "lights-out" manner.

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
| 3 | `~/projects/file-uploader-r3` | `~/projects/file-uploader-r3-uat` | — | — | — | In progress |

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

## Prior Art

Flywheel is part of the emerging "software factory" pattern. See `docs/software-factory-comparison.md` for a detailed comparison with StrongDM's Software Factory (https://factory.strongdm.ai/).

Key shared insight: individual AI agents are noisy but capable. The engineering challenge is designing systems that amplify signal over noise — adversarial quality gates, fresh contexts, mechanical boundaries. The system is reliable even when individual agents are not.

Inspired by Nate's Substack (Feb 2026) on the Opus 4.6 inflection point: https://natesnewsletter.substack.com/p/january-is-already-obsolete-my-honest

## Starting Point

We start by deep investigation of the "idpair-central" project located
at `~/idpair/idpair-central`.

1. Load up your go-actor skill
2. Thoroughly investigate the source code for `idpair-central`, ask
   questions as needed.
3. There are some "example CSVs" located at `~/idpair/example-csvs`,
   some of them are good, and some of them are deliberately bad. The
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
