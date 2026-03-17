# Jail Prototype Results — 2026-03-16

## Summary

Built and validated a prototype that runs Claude Code inside an UnshareJail (user namespace chroot) with MCP broker coordination. 14 iterations over one session to get from first attempt to full end-to-end success. The prototype proves that OS-level agent isolation for round 4 is feasible.

**Final state:** Claude Code starts inside a chroot jail, renders its TUI, executes tools (Read, Write, Bash), makes API calls to Anthropic, and signals completion to the host driver via MCP broker over a Unix socket.

## What Was Built

### Files

- `jail/mount.go` — Added `FSType` field to `Mount` struct and `TmpfsMount()` helper for tmpfs mounts
- `jail/unshare.go` — Added `Script()`, `UnshareArgs()` for tmux integration; `SetDropUser(username)` for privilege drop via `runuser`; `buildScript()` updated for FSType mounts, /dev/pts, PTY chmod, and runuser exec
- `broker/broker.go` — Added `write_done` event broadcast to all subscribers (was missing — broker wrote done file but never pushed notification)
- `cmd/jail-test/main.go` — Full test harness: broker + MCP server in-process, UnshareJail with mount table, tmux session management, broker event wait loop, workspace trust injection, X11 socket mount
- `cmd/jail-test/tmux.go` — `newTmuxSession()` and `newTmuxSessionWithCommand()` (direct pane command, critical for TUI), `sendKeys()`, `paneAlive()`, `destroy()`

### CLI

```bash
go build -o jail-test ./cmd/jail-test/
./jail-test --workspace /tmp/jail-workspace --keep
# In another terminal: tmux attach -t jail-test
```

Flags: `--workspace` (required), `--tmux-session`, `--role`, `--prompt`, `--keep`, `--timeout`

## The 14 Iterations

### Iteration 1: Missing ~/.claude.json

**Problem:** Claude Code looks for `~/.claude.json` in `$HOME`, separate from the `~/.claude/` directory. Our ephemeral copy only included `~/.claude/`.

**Fix:** Copy both `~/.claude/` and `~/.claude.json` into the jail.

### Iteration 2: Root rejection

**Problem:** `unshare --map-root-user` makes the process UID 0 inside the namespace. Claude Code refuses `--permission-mode bypassPermissions` (and `--dangerously-skip-permissions`) when running as root.

**Fix:** Added `SetDropPrivileges(uid, gid)` to UnshareJail using `setpriv`. (Later replaced — see iteration 3.)

### Iteration 3: File bind mount fails

**Problem:** Jail `Setup()` calls `MkdirAll` on all mount targets, creating a directory at the `.claude.json` path. Bind-mounting a file onto a directory fails with "wrong fs type, bad superblock."

**Fix:** Don't bind-mount `.claude.json`. Copy it directly into the jail's home directory before `Setup()` runs. The jail package assumes all mount targets are directories.

### Iteration 4: UID mapping breaks setpriv

**Problem:** `setpriv --reuid=1000` inside the namespace doesn't do what we expect. The `--map-auto` flag creates: namespace UID 0 → host UID 1000, namespace UIDs 1-65535 → host UIDs 100000-165535 (from `/etc/subuid` mapping `jim:100000:65536`). So namespace UID 1000 maps to host UID 100999, not 1000. Claude ran as the wrong UID and couldn't read its config.

**Also confirmed:** `--permission-mode bypassPermissions` triggers the same root check as `--dangerously-skip-permissions`. Both refuse UID 0.

### Iteration 5: Nested user namespaces not permitted

**Problem:** Tried `unshare --map-user=1000 --map-group=1000` after chroot to create a child namespace. Failed with "Operation not permitted" — can't nest user namespaces without `CAP_SYS_ADMIN`.

**Fix:** `runuser -u jim` — doesn't need PAM, works inside user namespaces, changes effective UID/GID. Claude Code's root check passes because `whoami` returns the real username.

### Iteration 5b: CWD problem

**Problem:** `chroot` changes the filesystem root but doesn't set the working directory. Claude Code tried to `lstat` the host's CWD (`/home/jim/projects/flywheel`) which doesn't exist inside the jail.

**Fix:** Wrap the claude command in `bash -c "cd /workspace && exec claude..."`.

### Iteration 6: Missing /dev/pts

**Problem:** Claude Code starts (confirmed: 13 threads, network connections, correct process tree) but produces no visible output in the tmux pane. The jail mounted individual device nodes (`/dev/null`, `/dev/tty`, etc.) but NOT the devpts filesystem. Without `/dev/pts/1` (the tmux pane's PTY), claude couldn't do I/O.

**Initial misdiagnosis:** We thought claude had zero file descriptors. Actually, `/proc/<pid>/fd/` was unreadable from the host because the process ran as host UID 100999 and we were UID 1000. Claude DID have fds.

**Fix:** Added `mount --rbind /dev/pts` to `writeUnshareMounts()`.

### Iteration 7: TUI lockup via send-keys

**Problem:** Claude Code's TUI took over the terminal (alternate screen buffer, input capture) but wouldn't render. `tmux attach` showed a blinking cursor but no TUI content. Eventually locked up completely — couldn't even Ctrl-C. Had to `tmux kill-server` from outside.

**Root cause:** The tmux pane was running `fish` shell, and `send-keys` typed the unshare command into it. The process chain was: `fish → unshare → bash (jail script) → chroot → runuser → bash -c → claude` — 6 layers of process wrapping. Claude didn't have direct PTY ownership.

**Fix:** `newTmuxSessionWithCommand()` — creates the tmux session with the unshare command AS the pane process. No intermediate shell. Claude gets direct PTY ownership.

### Iteration 8: Workspace trust dialog

**Problem:** Claude Code prompts "Is this a project you trust?" on first access to a workspace. This dialog blocked automation — the previous iterations' "no visible output" was partly this dialog rendering in the alternate screen buffer where we couldn't see it.

**Fix:** `injectWorkspaceTrust()` — reads the ephemeral `.claude.json`, adds `/workspace` to the `projects` map with `hasTrustDialogAccepted: true`, writes it back. No dialog on subsequent runs.

### Iteration 9: --print mode validation

**Detour:** Switched to `--print` mode (non-interactive) to validate the jail mounts independently of TUI issues. Confirmed "Hello from jail!" output — the jail works. But `--print` mode can't use tools, so MCP testing needed interactive mode.

### Iteration 10: Interactive mode SUCCESS

**Breakthrough:** With `newTmuxSessionWithCommand()` and workspace trust pre-configured, Claude Code's full TUI rendered correctly inside the jail. Completed a 4-step test prompt (ls, create file, cat file, report) autonomously in 60 seconds. Read, Write, and Bash tools all worked.

### Iteration 11: MCP socket permission denied

**Problem:** `curl --unix-socket /run/mcp/mcp.sock` failed with "Could not connect to server." The socket file was owned by host UID 1000; the jailed user (host UID 100999) couldn't access it.

**Fix:** `chmod 777` on MCP socket directory AND socket file after creation.

### Iteration 12: MCP server never started

**Problem:** curl connected to the socket but hung — sent 153 bytes, got 0 bytes back. The socket was listening at the OS level (the listener was created in `NewServer`), but `Serve()` was never called — no goroutine was reading requests.

**Fix:** Added `go mcpServer.Serve()` after `NewServer()`.

### Iteration 13: MCP round-trip works, driver not notified

**Problem:** curl returned `{"ok":true}` from the broker — the `write_done` call succeeded! But the driver's event loop kept waiting. The broker's `handleWriteDone` wrote the done file to disk but never pushed a `BrokerEvent` to subscribers. The `push()` calls only existed for `new_request` and `response_ready` events.

**Fix:** Added `write_done` event broadcast to ALL subscribers in `broker/broker.go`.

### Iteration 14: FULL SUCCESS

Everything works. The driver printed:
```
[jail-test] broker event: write_done (role=builder)
[jail-test] SUCCESS — agent wrote done: status=PASS
```

## Proven Mount Table

```
Source                              Target in jail           Mode        Notes
──────────────────────────────────────────────────────────────────────────────────
HostMounts (/usr, /etc, /bin, etc.) standard paths           read-only   Includes merged-usr symlink handling
BaseMounts (/proc, /sys, /dev)      standard paths           varies      /dev/pts added for PTY access
/opt/claude-code                    /opt/claude-code         read-only   Claude Code ELF binary
ephemeral ~/.claude/ copy           /home/<user>/.claude     read-write  chmod -R a+rwX for UID mapping
<workspace>                         /workspace               read-write  chmod 777 for UID mapping
tmpfs                               /tmp                     read-write  RAM-backed, isolated per jail
MCP socket dir                      /run/mcp                 read-write  chmod 777 dir + socket
/tmp/.X11-unix                      /tmp/.X11-unix           read-write  X11 socket (after tmpfs)

NOT bind-mounted (copied into jail root before Setup):
~/.claude.json                      /home/<user>/.claude.json  file copy   chmod 666, trust injected
.Xauthority                         /home/<user>/.Xauthority   file copy   chmod 666 (X11 auth, not yet working)
```

### Environment Variables

```
HOME=/home/<user>
TERM=xterm-256color
DISPLAY=:0                          (for X11, not yet working)
DISABLE_AUTOUPDATER=1               (prevents claude self-update)
```

## Key Technical Discoveries

### 1. User Namespace UID Mapping

`/etc/subuid` maps `jim:100000:65536`. With `unshare --map-auto --map-root-user`:
- Namespace UID 0 → Host UID 1000 (the real user)
- Namespace UIDs 1-65535 → Host UIDs 100000-165535

This means ANY file/socket/directory that the jailed process needs to write to must be world-writable (`chmod 777`) or chowned to the mapped UID. This affects: workspace, home dir, config copies, MCP socket, X11 auth.

### 2. Privilege Drop: runuser

Five approaches tried for the "need root for mounts but claude refuses root" problem:

| Approach | Result |
|----------|--------|
| `setpriv --reuid=1000` | Wrong UID mapping (namespace 1000 = host 100999) |
| Stay as root, use `bypassPermissions` | Same root check as `dangerously-skip-permissions` |
| Nested `unshare --map-user=1000` | "Operation not permitted" (can't nest namespaces) |
| `--setuid 1000` on outer unshare | Loses ability to mount/chroot |
| **`runuser -u <username>`** | **WORKS** — changes effective user, no namespace tricks |

### 3. tmux PTY Ownership

`tmux send-keys` types into an existing shell (fish), creating 6 layers of process wrapping. Claude Code's TUI needs direct PTY ownership. `tmux new-session -d -s name "command"` makes the command the pane process — no intermediate shell, direct PTY.

### 4. Workspace Trust

Claude Code stores trust state in `~/.claude.json` under `projects.<path>.hasTrustDialogAccepted`. Pre-injecting this for `/workspace` bypasses the interactive trust dialog.

### 5. Broker Event Gap

The broker's `handleWriteDone` wrote to disk but never pushed events. The `Subscribe()` mechanism only covered `new_request` and `response_ready`. Any new event types need explicit `push()` calls in the broker's actor goroutine.

### 6. MCP via curl (Temporary)

Claude Code's `--mcp-config` expects stdio-based MCP servers. Our MCP server is HTTP over Unix socket. For the prototype, claude uses curl to call the broker directly. Production will need either:
- A stdio-to-socket bridge binary
- Converting the MCP server to stdio transport
- Using `--mcp-config` with a wrapper command

## What's NOT Working

### X11 Forwarding

xeyes fails with "Authorization required, but no authorization protocol specified." The X11 socket is mounted and `.Xauthority` is copied, but:
- `XAUTHORITY` env var not set (easy fix)
- May also need `xhost +local:` on the host to allow connections from the mapped UID
- Low priority — Playwright runs headless by default

### Cleanup Permissions

`rm -rf /tmp/jail-test-*` fails on files created by the jailed user (host UID 100999). Need either `sudo rm` or namespace-aware cleanup (run `rm` inside the namespace before teardown).

### curl Exit Code 52

curl reports "Empty reply from server" (exit 52) even though the broker processes the request. Likely a timing issue — the HTTP handler sends the response but the connection closes before curl reads it. Functionally works (broker gets the message).

## Architecture Validated

```
┌─────────────────────────────────────────────┐
│                  Host (driver)               │
│                                              │
│  ┌──────────┐    ┌──────────┐               │
│  │  Broker   │◄──│   MCP    │               │
│  │  (actor)  │   │  Server  │               │
│  └────┬─────┘    └────┬─────┘               │
│       │ events        │ Unix socket          │
│       ▼               │                      │
│  Driver event loop    │                      │
│                       │                      │
├───────────────────────┼──────────────────────┤
│  UnshareJail          │ /run/mcp/mcp.sock    │
│  (user namespace      │ (bind-mounted)       │
│   + chroot)           │                      │
│                       │                      │
│  ┌────────────────────▼────────────────────┐ │
│  │  Claude Code (interactive TUI)          │ │
│  │  - reads .flywheel-prompt.md            │ │
│  │  - uses Read/Write/Bash tools           │ │
│  │  - curls MCP socket for write_done      │ │
│  │  - /workspace is the working directory  │ │
│  └─────────────────────────────────────────┘ │
└─────────────────────────────────────────────┘
```

## Next Steps

1. **stdio MCP bridge** — so claude can use MCP tools natively instead of curl
2. **X11/XAUTHORITY** — set env var, possibly `xhost +local:` for Playwright support
3. **Cleanup in namespace** — run `rm -rf` inside the namespace before teardown
4. **Multiple concurrent jails** — the real round 4 use case (builder + reviewers + PO)
5. **Wire into flywheel driver** — replace the current direct `claude` invocation with jail-based invocation
6. **Git operations inside jail** — agents need to commit and push
7. **Duplicate mount cleanup** — `writeUnshareMounts` and `HostMounts` both mount /usr and /etc

## Commits (chronological)

```
b1cc18a Add FSType support to jail Mount for tmpfs
eba51d0 Export UnshareJail Script() and UnshareArgs() for tmux integration
05fc067 Add cmd/jail-test skeleton with CLI flag parsing
deca0c8 Add tmux session management for jail-test
56141cb Implement jail-test main flow: broker, MCP, jail, tmux, event loop
9e052e8 Fix jail for Claude Code: drop privileges, copy .claude.json
2edca1e Fix .claude.json: copy into jail instead of bind-mounting
4efa6f9 Remove setpriv drop: namespace UID mapping prevents it
88b38a7 Use nested user namespace to drop privileges instead of setpriv
450e6f1 Use runuser for privilege drop, fix CWD inside chroot
b712b33 Mount /dev/pts inside jail for PTY access
2c89ddf chmod PTY devices before runuser privilege drop
aac6244 Add TERM=xterm-256color to jail environment
849891c Use --print mode for jail test (non-interactive)
9148fb4 Simplify jail test for --print mode validation
1d1ecc6 Pass prompt directly to --print mode, tee output to file
8262de2 Fix permissions for user namespace UID mapping
2c88874 Try interactive mode with direct tmux pane command
22c237c Add MCP curl test prompt and workspace trust pre-configuration
5a1957c Add X11 socket mount and xeyes test step
2ffd1f5 Fix MCP socket and X11 permissions for jailed user
f4ceb25 Start MCP server goroutine — was never calling Serve()
b6005a6 Broadcast write_done event to all broker subscribers
```
