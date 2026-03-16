# lightweight-containers Package Review — 2026-03-16

## Context

Review of `~/projects/lightweight-containers` to understand current state before absorbing packages into flywheel for round 4. Significant work was done on the laptop session (2026-03-15 through 2026-03-16) that evolved the architecture substantially.

## Key Finding: In-Process Consolidation

The biggest architectural change: what was originally a 7-process system (driver + broker process + 5 MCP server processes communicating over Unix sockets) was consolidated into a single process. The broker runs as an in-process actor goroutine; MCP servers call `broker.Broker` interface directly. This happened in commits `9feb588` through `ffc1c59` on 2026-03-16.

**What was removed:**
- `broker/listener.go` — socket-based server (no longer needed)
- `mcp/store.go` — socket-based broker client adapter (no longer needed)
- `lc broker` and `lc mcp` subcommands — broker and MCP no longer run as separate processes

**What replaced it:**
- `mcp.Server` accepts a `broker.Broker` interface directly in constructor
- All 5 MCP servers (one per role) run as goroutines in the driver process
- Push events flow through Go channels, not sockets

## Package Inventory

### broker/ (797 lines, 9 tests)

Actor-based event broker implementing the PO request/response/amendment protocol.

**Public interface (11 methods):**
- Request lifecycle: `CreateRequest`, `CheckResponse`, `ListMyRequests`, `ListPendingRequests`, `ReadRequest`, `SendResponse`
- Amendments: `CreateAmendment`, `ListAmendments`
- Completion: `WriteDone` (enforces no outstanding requests per role)
- Events: `Subscribe` (push-based, non-blocking sends)

**State model:**
- Sequential request IDs (`%06d` format) via internal counter
- Amendment counter (`%03d` format)
- `outstanding` map tracks unanswered requests per role
- Subscriber channels with drop-on-slow-consumer semantics

**Disk persistence:** Request files (`NNNNNN-request-{role}.md`), response files, amendment files (`NNN-{sanitized-spec}.md`), done files (`{role}_done`).

### mcp/ (755 lines, 5 tests)

JSON-RPC MCP server implementing protocol v2024-11-05.

**Two role-based toolsets:**
- Pipeline agents (builder, code-reviewer, security-reviewer, qa): `send_request`, `check_response`, `wait_for_response`, `list_my_requests`, `write_done`
- Product Owner: `list_pending_requests`, `read_request`, `send_response`, `create_amendment`, `list_amendments`, `write_done`

**Wait mechanism:** `wait_for_response` checks broker immediately, subscribes to push events if not answered, blocks on channel until `response_ready` event. Sub-second latency.

### jail/ (715 lines, 12 tests)

Two chroot implementations behind a common `Jail` interface.

**RootJail:** Direct `mount(1)` syscalls. Teardown with `umount -R` and 3-attempt retry in reverse order. Requires root.

**UnshareJail:** User namespaces via `unshare(1)`. Generates bash mount script. Automatic cleanup via namespace exit. No root required.

**Mount system:** Declarative `Mount` structs with conditional logic. Handles merged-usr (Arch/Fedora: `/lib` → `usr/lib`) and traditional layouts. `BaseMounts` (7 pseudo-fs), `HostMounts` (read-only /usr, /etc + symlink-aware bin/lib), `conditionalDirMount`.

**Preflight:** `CheckUnsharePreflight()` verifies `/proc/sys/kernel/unprivileged_userns_clone` support.

### gitops/ (804 lines, 7 tests)

Multi-repo workspace management with per-role isolation.

**Workspace layout:** `target/` (source of truth), `build/`, `product-owner/`, `code-reviewer/`, `security-reviewer/`, `qa/` repos. Plus `jails/`, `handoff/`, `product-owner-requests/` directories.

**Per-role branches:** Each repo checks out a role-specific branch. Commits are transplanted between repos via `format-patch`/`am`.

**Transplant features:** Path filtering (only include changes under specified paths), squash (combine multiple commits), transplant marker tracking (`lc-last-transplant`).

**Pre-commit hooks:**
- Builder: blocklist — cannot touch specs, feedback, po-requests, reviewer dirs
- PO: allowlist — can only touch `build-specifications/`
- Reviewers: allowlist — own tools dir + own feedback dir only

**Sync:** Updates agent branch from target's HEAD, preserving agent-specific directories.

### internal/chanutil/ (48 lines)

Generic actor channel helpers. `WithResult[T]` interface, `SendReceive[C, K]()`, `SendReceiveError[C]()`. Panic recovery returns `io.EOF` if actor stopped.

### seqsock/ (214 lines, 4 tests)

Minimal Unix socket server for monotonic sequence numbers. JSON protocol. `atomic.Int64` counter. **Deprecated for MCP mode** — broker has its own internal counter.

## Integration Plan for Flywheel

**Drop-in packages:**
```go
import (
    "github.com/corruptmemory/flywheel/broker"
    "github.com/corruptmemory/flywheel/mcp"
    "github.com/corruptmemory/flywheel/jail"
    "github.com/corruptmemory/flywheel/gitops"
)
```

**Integration points:**
1. `broker.NewBroker(dir)` — create in-process actor, owns PO protocol
2. `mcp.NewServer(role, broker, socketPath)` — per-role MCP server as goroutine
3. `jail.NewRootJail(root, mounts)` or `jail.NewUnshareJail(root, mounts)` — per-agent filesystem projection
4. `gitops.InitWorkspace(config)` — create multi-repo workspace with hooks
5. `gitops.Transplant(src, dst, opts)` — move commits between repos with filtering

**Not consumed:**
- `internal/driver/` — flywheel's existing driver is more mature (gate retry tracking, JSONL logging, crash recovery, resume, scenario-based prompt assembly)
- `cmd/lc/`, `stubs/` — test harness only
- `seqsock/` — only for non-MCP mode

## Stress Test Results

- 76+ rounds completed without failures
- Both root jail and unshare jail modes tested
- MCP mode with full request/response/amendment protocol exercised
- tmux multi-pane visibility for concurrent agent monitoring
- End-to-end test script (`test-e2e.sh`) covers both non-MCP and MCP modes

## Open Items for Absorption

- Module path: packages currently under `github.com/corruptmemory/lightweight-containers` — need to update import paths
- `internal/chanutil/` duplicates flywheel's existing actor helpers — reconcile or pick one
- Node.js/Claude Code runtime inside chroot jails — network access, `~/.claude/` config, plugin paths
- tmux integration with flywheel's driver (currently separate code paths)
