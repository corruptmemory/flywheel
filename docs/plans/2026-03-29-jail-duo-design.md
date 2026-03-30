# Jail Duo Design — Two Agents Communicating via MCP Broker

**Date:** 2026-03-29
**Status:** Approved

## Goal

Prove two Claude Code agents in separate UnshareJails can communicate through a shared MCP broker. Builder asks PO a clarification question, PO responds, both signal completion. Driver detects both `write_done` events.

## Architecture

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
│  /workspace/spec.md                   │              │
│  Claude: read spec,                   │              │
│    ask PO, build                      │              │
├───────────────────────────────────────┼──────────────┤
│  Jail B (product-owner)               │              │
│  /run/mcp/mcp.sock ◄─────────────────┘              │
│  /workspace/spec.md (same spec)                      │
│  Claude: wait for requests,                          │
│    respond, write_done                               │
└─────────────────────────────────────────────────────┘
```

- One broker, two MCP servers (different roles, different sockets), two jails
- Two tmux panes in one session (PO left, Builder right)
- PO starts first (needs to be listening before Builder sends requests)
- Both communicate exclusively via curl to their jail's MCP socket
- Driver subscribes to broker events, waits for both `write_done` signals

## Sequence

1. Create broker, two MCP servers (role=builder, role=product-owner), two jails
2. Start PO in left tmux pane
3. Wait ~5 seconds for PO to initialize
4. Start Builder in right tmux pane (split from PO pane)
5. Builder reads spec, sends PLEASE CLARIFY via MCP curl
6. Broker pushes `new_request` event to PO's subscription
7. PO polls `list_pending_requests`, reads request, sends response via MCP curl
8. Broker pushes `response_ready` event to Builder's subscription
9. Builder reads response, creates hello.go, calls `write_done`
10. PO calls `write_done`
11. Driver receives both `write_done` events — SUCCESS

## CLI

```
cmd/jail-duo [OPTIONS]

Required:
  --builder-workspace <dir>    Builder agent workspace
  --po-workspace <dir>         Product Owner agent workspace

Optional:
  --tmux-session <name>        Use existing tmux session
  --keep                       Don't tear down on exit
  --timeout <duration>         Max wait time (default: 5m)
```

## Tmux Layout

```
┌──────────────────────┬──────────────────────┐
│    Product Owner     │      Builder          │
│    (left pane)       │      (right pane)     │
└──────────────────────┴──────────────────────┘
```

Created via: `tmux new-session` with PO command, then `tmux split-window -h` with Builder command.

## Test Scenario

### Shared spec (`spec.md` in both workspaces)

```markdown
# Spec: Hello Service
Build a CLI tool that prints a greeting.
The greeting should include the user's name.
```

### Builder prompt

Reads spec, notices ambiguity (where does user's name come from?), sends PLEASE CLARIFY to PO via curl, waits for response, implements per PO's guidance, calls write_done.

### PO prompt

Polls for pending requests via curl, reads the request, responds with "name should be a CLI argument," calls write_done.

### MCP communication via curl

All MCP calls use the same pattern:
```bash
curl --unix-socket /run/mcp/mcp.sock -X POST http://localhost/ \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":N,"method":"tools/call","params":{"name":"TOOL","arguments":{...}}}'
```

Builder tools: `send_request`, `check_response`, `write_done`
PO tools: `list_pending_requests`, `read_request`, `send_response`, `write_done`

## Reuse from jail-test

- Mount table pattern (HostMounts, BaseMounts, /opt/claude-code, ephemeral config, tmpfs, MCP socket)
- Ephemeral `~/.claude` + `~/.claude.json` copy with workspace trust injection
- Permission chmod (777 for UID-mapped writable dirs)
- `newTmuxSessionWithCommand()` for direct PTY ownership
- Broker + MCP server setup
- `SetDropUser()` for privilege drop via runuser

## New code needed

- `cmd/jail-duo/main.go` — dual-agent harness
- `cmd/jail-duo/tmux.go` — extended tmux helpers (split pane)
- Prompt templates for Builder and PO
- Event loop waiting for TWO `write_done` events

## Success Criteria

Driver prints:
```
[jail-duo] broker event: write_done (role=product-owner)
[jail-duo] broker event: write_done (role=builder)
[jail-duo] SUCCESS — both agents completed
```

(Order of events may vary — PO might finish before or after Builder.)

## NOT in Scope

- stdio MCP bridge (curl hack is fine for prototype)
- Git operations inside jails
- More than two agents
- Round 4 pipeline logic
- X11 forwarding
