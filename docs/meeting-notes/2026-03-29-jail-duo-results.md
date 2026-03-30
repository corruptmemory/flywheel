# Jail Duo Results — 2026-03-29

## Summary

Built and validated a two-agent prototype: Builder + Product Owner running in separate UnshareJails, communicating through a shared MCP broker. **Worked on the first try** — no iterations needed. All the hard problems were solved in the jail-test prototype (14 iterations); jail-duo composed those solutions and they worked immediately.

## What Happened

### The Scenario

A deliberately ambiguous spec: "Build a CLI tool that prints a greeting. The greeting should include the user's name." — doesn't say where the name comes from.

### The Agents

- **Product Owner** (left tmux pane): started first, polled for requests, waited
- **Builder** (right tmux pane): started 5 seconds later, read spec, noticed ambiguity

### The Communication

1. Builder sent PLEASE CLARIFY via MCP curl: "Should the name be a CLI argument, environment variable, or stdin?"
2. Broker received request, pushed `new_request` event to PO's subscription
3. PO polled `list_pending_requests`, found the request on second attempt
4. PO read the request, sent response: "CLI argument. `./hello <name>`. Usage message on missing arg, exit code 1."
5. Broker pushed `response_ready` event to Builder's subscription
6. Builder checked response, received PO's guidance
7. Builder implemented `hello.go` — a correct Go program with `os.Args`, error handling, usage message
8. Both agents called `write_done` with PASS
9. Driver received both events: SUCCESS

### Bonus

The Builder's Insight box noted: "Per the user's CLAUDE.md preferences, CLI arguments are preferred over environment variables." It found Jim's global CLAUDE.md inside the jail's ephemeral `~/.claude` copy and applied the preference. Impressively contextual.

### Timing

- PO: 38 seconds of work
- Builder: 52 seconds of work
- Total wall-clock: ~60 seconds (overlapping, concurrent)

## Architecture Validated

```
┌─────────────────────────────────────────────────────┐
│                    Host (driver)                     │
│                                                      │
│  ┌──────────┐    ┌──────────┐    ┌──────────┐       │
│  │  Broker   │◄──│ MCP Srv  │    │ MCP Srv  │       │
│  │  (actor)  │◄──│ (builder)│    │  (po)    │       │
│  └────┬─────┘    └────┬─────┘    └────┬─────┘       │
│       │ events        │ sock          │ sock         │
│       ▼               │               │              │
│  Driver event loop    │               │              │
│                       │               │              │
├───────────────────────┼───────────────┼──────────────┤
│  Jail A (builder)     │               │              │
│  /run/mcp/mcp.sock ◄─┘               │              │
│  /workspace/ (hello.go written here)  │              │
├───────────────────────────────────────┼──────────────┤
│  Jail B (product-owner)               │              │
│  /run/mcp/mcp.sock ◄─────────────────┘              │
│  /workspace/ (spec.md, read-only context)            │
└─────────────────────────────────────────────────────┘
```

## Files Created

- `cmd/jail-duo/main.go` — driver: broker, 2 MCP servers, 2 jails, tmux split, dual event loop
- `cmd/jail-duo/tmux.go` — `newDuoTmux()` creates session with PO pane, splits for Builder with delay
- `cmd/jail-duo/jailsetup.go` — `setupAgentJail()` extracts reusable jail setup (config copy, mounts, trust, script gen)
- `cmd/jail-duo/prompts.go` — `specContent`, `builderPrompt`, `poPrompt` constants

## Key Design Decisions

- **PO starts first** — needs to be listening before Builder sends requests. 5-second head start via `sleep` in the tmux split command.
- **Separate MCP sockets** — each jail gets its own socket dir bind-mounted to `/run/mcp/mcp.sock`. Different sockets, same broker.
- **Shared broker** — single in-process actor. Both MCP servers call it directly. Push events flow to both agents and the driver.
- **Direct tmux pane command** — `newTmuxSessionWithCommand` for PO pane, `split-window` for Builder pane. Claude gets direct PTY ownership.
- **curl hack for MCP** — both agents use curl to call their MCP socket. Works but prompts are verbose. stdio bridge would make this cleaner.

## What the Builder Produced

```go
package main

import (
    "fmt"
    "os"
)

func main() {
    if len(os.Args) < 2 {
        fmt.Fprintln(os.Stderr, "Usage: ./hello <name>")
        os.Exit(1)
    }
    fmt.Printf("Hello, %s!\n", os.Args[1])
}
```

Correct, follows PO's guidance exactly (CLI argument, usage message to stderr, exit code 1).

## Comparison: jail-test vs jail-duo

| Metric | jail-test (single agent) | jail-duo (two agents) |
|--------|--------------------------|----------------------|
| Iterations to working | 14 | 1 |
| Time to first success | ~4 hours of debugging | ~60 seconds |
| New code written | ~340 lines (main.go + tmux.go) | ~450 lines (4 files) |
| Hard problems solved | UID mapping, privilege drop, PTY, trust, MCP socket, broker events | None — all reused from jail-test |

The lesson: solve the hard problems empirically on the simplest case, then composition is (almost) free.

## Next Steps

The "full peanut gallery" — add review agents (QA, code review, security review) alongside Builder and PO. This is the round 4 pipeline:

1. **Init/Prep phase**: All agents read specs, negotiate with PO, build speculative tooling
2. **Build loop**: Builder implements one spec → all reviewers run concurrently → feedback loop
3. **UAT**: Final acceptance testing

This requires:
- 5+ concurrent jails (PO, Builder, QA, Code Review, Security Review)
- tmux with 5+ panes or a different visibility approach
- The full state machine from `feedback/round-4-ideas.md`
- Proper stdio MCP bridge (curl hack won't scale to 5 agents with complex prompts)

## Driver Log

```
[jail-duo] --keep: temp dir at /tmp/jail-duo-399654961
[jail-duo] starting broker...
[jail-duo] starting builder MCP server at /tmp/jail-duo-399654961/mcp-builder/mcp.sock
[jail-duo] starting PO MCP server at /tmp/jail-duo-399654961/mcp-po/mcp.sock
[jail-duo] checking unshare preflight...
[jail-duo] setting up PO jail...
[jail-duo] setting up Builder jail...
[jail-duo] creating tmux session (PO left, Builder right)...
[jail-duo] waiting 5s for PO to initialize...
[jail-duo] agents launched in tmux session "jail-duo"
[jail-duo] attach with: tmux attach -t jail-duo
[jail-duo] waiting for both write_done events (timeout: 5m0s)...
[jail-duo] still waiting... (0/2 done)
[jail-duo] still waiting... (0/2 done)
[jail-duo] still waiting... (0/2 done)
[jail-duo] broker event: write_done (role=product-owner)
[jail-duo] product-owner done: status=PASS (1/2 complete)
[jail-duo] still waiting... (1/2 done)
[jail-duo] broker event: write_done (role=builder)
[jail-duo] builder done: status=PASS (2/2 complete)
[jail-duo] SUCCESS — both agents completed
```
