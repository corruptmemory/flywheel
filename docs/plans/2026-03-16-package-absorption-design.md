# Package Absorption Design — Phase 0

**Date:** 2026-03-16
**Status:** Approved

## Goal

Absorb 5 packages from `~/projects/lightweight-containers` into flywheel. Copy source, fix import paths, add build.sh, verify tests pass. No driver changes — packages are absorbed but not yet wired.

## Decision: Copy-In (Not Module Dependency)

- No second consumer exists; flywheel is the only target
- Packages will need adaptation as driver evolves
- lightweight-containers has a bare module path (not fully qualified)
- One repo, one build, no version coordination

## Packages Absorbed

| Source | Destination | Lines | Import Path Changes |
|--------|------------|-------|---------------------|
| `broker/` | `flywheel/broker/` | ~800 | `internal/chanutil` path |
| `mcp/` | `flywheel/mcp/` | ~755 | `broker` path |
| `jail/` | `flywheel/jail/` | ~715 | None (standalone) |
| `gitops/` | `flywheel/gitops/` | ~800 | None (standalone) |
| `internal/chanutil/` | `flywheel/internal/chanutil/` | ~48 | None (standalone) |

**Not absorbed:** `internal/driver/`, `cmd/lc/`, `stubs/`, `seqsock/`, `docs/`, `test-e2e.sh`, `build.sh`

## Package Layout

```
flywheel/
  broker/              ← actor-based event broker
  mcp/                 ← JSON-RPC MCP server
  jail/                ← chroot isolation (root + unshare)
  gitops/              ← workspace init, transplant, hooks
  internal/chanutil/   ← actor channel helpers
  cmd/flywheel/        ← existing driver (untouched)
  prompts/             ← existing
  design-system/       ← existing
  feedback/            ← existing
  docs/                ← existing
  build.sh             ← new
  .gitignore           ← new
  flywheel             ← built binary (gitignored)
```

## build.sh

Stripped-down version of the go-web-scaffold pattern. CLI tool, not a web app — no templ, no vendor refresh, no air.

**Flags:**
- `--build` / `-b` — `CGO_ENABLED=0 go build -trimpath -o flywheel ./cmd/flywheel/`
- `--test` / `-t` — `go test ./...`
- `--race` / `-r` — `go test -race ./...`
- `--vet` / `-V` — `go vet ./...`
- `--clean` / `-c` — `rm -f flywheel`
- `--all` / `-a` — vet + test + build

Binary lives in repo root (`./flywheel`), gitignored.

## Import Path Changes

All internal references change from `lightweight-containers/...` to `github.com/corruptmemory/flywheel/...`:

- `broker/broker.go` imports `internal/chanutil` → `github.com/corruptmemory/flywheel/internal/chanutil`
- `mcp/server.go` imports `broker` → `github.com/corruptmemory/flywheel/broker`
- Corresponding test files

`jail/` and `gitops/` have no internal cross-references — no changes needed.

## go.mod

Bump from `go 1.24` to `go 1.26.1` to match lightweight-containers and the builder workspace.

## Verification

1. `go vet ./...` — no warnings
2. `go test ./...` — all ~39 tests pass
3. `go build ./cmd/flywheel/` — compiles
4. `./build.sh --all` — full verification in one command

## Explicitly Out of Scope

- Driver changes (no new CLI flags, no jail/MCP wiring)
- Prompt rework
- Behavioral changes to absorbed packages
- Any Phase 1+ work
