# Phase 0 Absorption & Cross-Platform Research — 2026-03-16

## Session Summary

Two workstreams in this session: executing Phase 0 (package absorption from lightweight-containers) and researching cross-platform isolation feasibility.

## Phase 0: Package Absorption (Complete)

Absorbed 5 packages from `~/projects/lightweight-containers` into flywheel via copy-in:

| Package | Lines | Tests | Import Fixes |
|---------|-------|-------|-------------|
| `jail/` | 715 | 12 | None (standalone) |
| `gitops/` | 804 | 7 | None (standalone) |
| `internal/chanutil/` | 48 | — | None (standalone) |
| `broker/` | 797 | 9 | 1 (`chanutil` path) |
| `mcp/` | 755 | 5 | 2 (`broker` path in server.go + test) |
| **Total** | **3,119** | **33** | **3** |

Additional changes:
- Bumped `go.mod` to Go 1.26.1
- Added `build.sh` with `--build`, `--test`, `--race`, `--vet`, `--clean`, `--all`
- Added `.gitignore` for binary
- Cleaned up lint: `interface{}` → `any`, removed unnecessary type arguments, modernized range loop
- All tests pass, race detector clean

## Cross-Platform Isolation Research

**Question:** Can flywheel's jail-based agent isolation work on macOS and Windows?

**Answer:** Yes, with platform-specific `Jail` interface implementations.

### Recommended Architecture

```
Platform    Mechanism                         Isolation Quality
--------    ---------                         -----------------
Linux       chroot + user namespaces          Full (existing RootJail/UnshareJail)
macOS       Seatbelt + symlink projection     Medium (access control, no path remapping)
Windows     WSL2 + Linux jails                Full (delegates to existing Linux impl)
```

### macOS: Seatbelt + Symlink Projection

- `sandbox-exec` provides kernel-level access control via SBPL profiles (deny/allow reads and writes to specific paths)
- Deprecated by Apple since 2016 but still functional and used by Chrome, Cursor, and Claude Code
- No bind mount equivalent — cannot remap paths. Workaround: Bazel-style symlink-projected workspace layout
- A `SeatbeltJail` implementation would generate SBPL profiles dynamically (like Cursor does)
- Combined with symlinks, provides tailored per-agent filesystem views with kernel enforcement
- Weaker than Linux (determined agent could discover real paths via symlinks) but acceptable as defense-in-depth alongside MCP coordination

### macOS: Apple Containerization Framework

- https://github.com/apple/container — open-source Swift framework, announced WWDC 2025
- Runs Linux inside micro-VMs on Apple Silicon (macOS 26+ only, pre-1.0)
- Overkill for our use case — we want process isolation, not a VM
- Potential future fallback if Apple eventually removes Seatbelt
- Not a replacement for native macOS sandboxing

### Windows: WSL2 Delegation

- Industry consensus: Cursor and OpenAI Codex both run Linux sandboxes inside WSL2 on Windows
- Our existing `jail/` package works unmodified inside WSL2 (real Linux kernel)
- Multiple concurrent jails supported (WSL2 is one shared VM)
- Key constraint: workspaces must live in WSL2 filesystem (`/home/...`), not on Windows side (`/mnt/c/`), due to ~10x cross-filesystem performance penalty
- Native Windows options (Windows Sandbox: single-instance only; Windows Containers: multi-GB images; AppContainer: too restrictive) are all poor fits

### Windows Sandbox (Not Recommended)

- Single-instance limitation is a dealbreaker for concurrent agent isolation
- Full VM overhead (~2GB+ per instance)
- Disposable only (no persistent state)
- Requires Hyper-V (not on Windows Home)

### Cross-Platform Go Libraries

None exist for our use case. The closest (`criyle/go-sandbox`, `criyle/go-judge`) are Linux-only or experimental on other platforms.

### Design Decision

The `Jail` interface (`Setup`, `Command`, `Teardown`, `Root`) is already the right abstraction. Platform-specific implementations (`RootJail`, `UnshareJail`, future `SeatbeltJail`, `WSL2Jail`) hide OS differences. The `Mount` struct translates to bind mounts on Linux, symlinks + SBPL rules on macOS, and bind mounts inside WSL2 on Windows.

**This is NOT a current priority.** Research captured for future reference when portability becomes relevant.

### Risk: Seatbelt Deprecation

Apple deprecated `sandbox-exec` 10 years ago but hasn't removed it. Chrome, Cursor, and Claude Code all depend on it. Low-probability near-term risk but worth monitoring. Fallback: Apple's Containerization framework or direct use of Virtualization.framework for lightweight Linux VMs.
