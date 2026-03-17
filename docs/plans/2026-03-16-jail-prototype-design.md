# Jail Prototype Design — Claude Code in a Chroot

**Date:** 2026-03-16
**Status:** Approved

## Goal

Empirically discover what Claude Code needs to function inside a chroot jail. Build a standalone test harness that runs one Claude Code session inside an UnshareJail with MCP coordination and tmux terminal management.

## Architecture

A standalone `cmd/jail-test/main.go` that:

1. Creates an UnshareJail with a mount table tailored for Claude Code
2. Starts a broker actor in-process
3. Starts one MCP server (configurable role) on a Unix socket, backed by the broker
4. Creates a tmux session (or uses an existing one via `--tmux-session`)
5. Sends the claude command to the tmux pane, pointing at `.flywheel-prompt.md`
6. Subscribes to broker events and waits for `write_done`
7. Logs everything — what happened, what the agent wrote, whether MCP worked
8. Tears down — kill tmux session (if we created it), teardown jail

## CLI Flags

```
cmd/jail-test [OPTIONS]

Required:
  --workspace <dir>     Directory to mount as the agent's workspace

Optional:
  --tmux-session <name> Use existing tmux session instead of creating one
  --role <name>         MCP role identity (default: "builder")
  --prompt <file>       Prompt file to place in workspace (default: basic test prompt)
  --keep                Don't tear down jail on exit (for debugging)
```

## Mount Table (Best Guess — Expect Iteration)

```
Mount                              Target in jail       Mode        Why
──────────────────────────────────────────────────────────────────────────
HostMounts (jail package)          /usr, /etc, /bin...  read-only   System userland
BaseMounts (jail package)          /proc, /sys, /dev    varies      Pseudo-filesystems
/opt/claude-code                   /opt/claude-code     read-only   Claude Code runtime
ephemeral ~/.claude copy           /home/<user>/.claude read-write  Config, sessions, plugins
tmpfs                              /tmp                 read-write  Agent scratch (RAM-backed)
MCP socket dir                     /run/mcp             read-write  Broker communication
<workspace dir>                    /workspace           read-write  Agent's working directory
```

### Mount ordering

1. All system and application mounts first
2. `tmpfs` on `/tmp` (creates fresh writable layer)
3. MCP socket bind-mount into `/run/mcp` (after any parent tmpfs)

### Ephemeral ~/.claude

Each agent gets a disposable copy of `~/.claude`:

1. `cp -r ~/.claude <tempdir>/claude-config/`
2. Bind-mount the copy into the jail at `/home/<user>/.claude`
3. Agent can modify freely — no host contamination
4. Deleted on teardown

This prevents cross-agent config leakage and protects the host config.

### Known unknowns in the mount table

- **Node.js location** — probably under `/usr` (covered by HostMounts), verify empirically
- **`$HOME` resolution** — Claude Code expects HOME to resolve; set `HOME=/home/<user>` in env
- **Git + SSH** — git under `/usr/bin`, SSH keys under `~/.ssh`. May need `~/.ssh` read-only mount
- **Dangling symlinks** — ephemeral `~/.claude` copy may have symlinks to absolute paths not in jail
- **DNS** — `/etc/resolv.conf` via HostMounts should work

## Test Prompt

`.flywheel-prompt.md` placed in workspace, exercises four capabilities:

1. **Read a file** — proves workspace mount works
2. **Write a file** — proves workspace is writable
3. **Run a shell command** — proves system mounts work (/bin, /usr)
4. **Call `write_done` via MCP** — proves MCP socket works through jail boundary

## Event Flow

```
jail-test starts
  │
  ├── Create temp dirs (jail root, claude config copy)
  ├── Start broker actor (in-process)
  ├── Start MCP server (Unix socket in temp dir)
  ├── Subscribe to broker events
  ├── Set up UnshareJail with mount table
  ├── Copy ~/.claude to temp dir
  ├── Write .flywheel-prompt.md to workspace (unless --prompt provided)
  ├── Create tmux session (or use --tmux-session)
  │
  ├── Send to tmux pane:
  │     claude --permission-mode bypassPermissions \
  │       "Read .flywheel-prompt.md and follow ALL instructions exactly."
  │
  ├── Wait on broker event channel:
  │     ├── write_done received → log verdict, exit success
  │     ├── timeout (5 min) → log failure, exit error
  │     └── tmux pane dies → log exit code, exit error
  │
  └── Teardown:
        ├── Kill tmux session (if we created it)
        ├── Teardown jail (unmounts)
        └── Delete temp dirs (unless --keep)
```

### Done file detection

No filesystem polling. The broker pushes a `write_done` event to the driver's subscription channel the instant the agent calls the MCP `write_done` tool. Zero latency.

### Failure detection

Monitor tmux pane status. If the pane exits (claude crashes), the harness detects it via `tmux list-panes` and logs the failure without waiting for the 5-minute timeout.

## Success Criteria

Claude Code starts, reads the prompt, writes a file, and calls `write_done` via MCP. If all four succeed, the jail works.

## Explicitly NOT in Scope

- Multiple concurrent agents
- Full driver state machine
- Git operations inside the jail
- Review agents, PO, or pipeline logic
- Production error handling
- RootJail testing (UnshareJail only for prototype)

## Expected Outcome

The mount table WILL need iteration. The prototype is designed to fail fast and tell us exactly what's missing. Each failure adds a mount, an env var, or a symlink fix. After 3-5 iterations, we'll have the definitive mount table for Claude Code in a chroot.
