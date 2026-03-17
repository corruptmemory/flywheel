# Phase 0: Package Absorption Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Copy 5 packages from lightweight-containers into flywheel, fix import paths, add build.sh, verify all tests pass.

**Architecture:** Direct file copy with import path rewriting. No behavioral changes. No driver wiring.

**Tech Stack:** Go 1.26.1, go-flags

---

### Task 1: Add .gitignore and bump go.mod

**Files:**
- Create: `.gitignore`
- Modify: `go.mod`

**Step 1: Create .gitignore**

```
flywheel
tmp/
*.test
```

**Step 2: Bump go.mod to 1.26.1**

Change `go 1.24` to `go 1.26.1` in `go.mod`. Run:

```bash
go mod tidy
```

**Step 3: Verify**

Run: `go build ./cmd/flywheel/`
Expected: compiles without errors

**Step 4: Commit**

```bash
git add .gitignore go.mod go.sum
git commit -m "Add .gitignore and bump to Go 1.26.1"
```

---

### Task 2: Copy standalone packages (jail/, gitops/)

These have no internal cross-references — pure copy.

**Files:**
- Create: `jail/jail.go`, `jail/mount.go`, `jail/root.go`, `jail/unshare.go`, `jail/preflight.go`, `jail/jail_test.go`, `jail/unshare_test.go`
- Create: `gitops/workspace.go`, `gitops/transplant.go`, `gitops/sync.go`, `gitops/hooks.go`, `gitops/gitops_test.go`

**Step 1: Copy files**

```bash
cp -r ~/projects/lightweight-containers/jail/ ./jail/
cp -r ~/projects/lightweight-containers/gitops/ ./gitops/
```

**Step 2: Fix package declarations — verify they're already correct**

The packages should already declare `package jail` and `package gitops`. Verify:

```bash
head -1 jail/*.go gitops/*.go
```

Expected: `package jail` and `package gitops` respectively.

**Step 3: Verify compilation**

```bash
go vet ./jail/... ./gitops/...
```

Expected: no errors (these packages have no imports from lightweight-containers)

**Step 4: Run tests**

```bash
go test ./jail/... ./gitops/...
```

Expected: jail tests pass (root-requiring tests skip with `t.Skip`), gitops tests pass. Some jail tests may skip if user namespaces aren't available — that's expected.

**Step 5: Commit**

```bash
git add jail/ gitops/
git commit -m "Add jail and gitops packages from lightweight-containers"
```

---

### Task 3: Copy internal/chanutil/

**Files:**
- Create: `internal/chanutil/chanutil.go`

**Step 1: Copy**

```bash
mkdir -p internal/chanutil
cp ~/projects/lightweight-containers/internal/chanutil/chanutil.go internal/chanutil/
```

**Step 2: Verify**

```bash
go vet ./internal/chanutil/...
```

Expected: clean (chanutil has no external dependencies beyond stdlib)

**Step 3: Commit**

```bash
git add internal/chanutil/
git commit -m "Add internal/chanutil actor helpers from lightweight-containers"
```

---

### Task 4: Copy broker/ with import path fix

**Files:**
- Create: `broker/broker.go`, `broker/broker_test.go`

**Step 1: Copy**

```bash
cp -r ~/projects/lightweight-containers/broker/ ./broker/
```

**Step 2: Fix import path in broker.go**

In `broker/broker.go`, change:
```go
"lightweight-containers/internal/chanutil"
```
to:
```go
"github.com/corruptmemory/flywheel/internal/chanutil"
```

**Step 3: Verify compilation**

```bash
go vet ./broker/...
```

Expected: clean

**Step 4: Run tests**

```bash
go test ./broker/...
```

Expected: all 9 broker tests pass

**Step 5: Commit**

```bash
git add broker/
git commit -m "Add broker package from lightweight-containers

Actor-based event broker: PO request/response/amendment protocol,
push events, write_done enforcement, sequential IDs."
```

---

### Task 5: Copy mcp/ with import path fix

**Files:**
- Create: `mcp/server.go`, `mcp/server_test.go`

**Step 1: Copy**

```bash
cp -r ~/projects/lightweight-containers/mcp/ ./mcp/
```

**Step 2: Fix import path in server.go**

In `mcp/server.go`, change:
```go
"lightweight-containers/broker"
```
to:
```go
"github.com/corruptmemory/flywheel/broker"
```

**Step 3: Fix import path in server_test.go**

In `mcp/server_test.go`, change:
```go
"lightweight-containers/broker"
```
to:
```go
"github.com/corruptmemory/flywheel/broker"
```

**Step 4: Verify compilation**

```bash
go vet ./mcp/...
```

Expected: clean

**Step 5: Run tests**

```bash
go test ./mcp/...
```

Expected: all 5 MCP tests pass

**Step 6: Commit**

```bash
git add mcp/
git commit -m "Add MCP server package from lightweight-containers

JSON-RPC MCP server with per-role tool surfaces, calls broker
in-process via broker.Broker interface."
```

---

### Task 6: Add build.sh

**Files:**
- Create: `build.sh`

**Step 1: Write build.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

BINARY="flywheel"

usage() {
    cat <<USAGE
Usage: $(basename "$0") [OPTIONS]

Options:
  -b, --build    Build the binary
  -t, --test     Run tests
  -r, --race     Run tests with race detector
  -V, --vet      Run go vet
  -c, --clean    Remove built binary
  -a, --all      Vet + test + build
  -h, --help     Show this help
USAGE
}

do_vet=false
do_test=false
do_race=false
do_build=false
do_clean=false

if [[ $# -eq 0 ]]; then
    usage
    exit 0
fi

while [[ $# -gt 0 ]]; do
    case "$1" in
        -b|--build) do_build=true ;;
        -t|--test)  do_test=true ;;
        -r|--race)  do_race=true ;;
        -V|--vet)   do_vet=true ;;
        -c|--clean) do_clean=true ;;
        -a|--all)   do_vet=true; do_test=true; do_build=true ;;
        -h|--help)  usage; exit 0 ;;
        *) echo "Unknown option: $1"; usage; exit 1 ;;
    esac
    shift
done

if $do_clean; then
    echo "==> Cleaning..."
    rm -f "$BINARY"
    echo "    Done."
fi

if $do_vet; then
    echo "==> Vetting..."
    go vet ./...
    echo "    Done."
fi

if $do_test; then
    echo "==> Testing..."
    go test ./...
    echo "    Done."
fi

if $do_race; then
    echo "==> Testing (race detector)..."
    go test -race ./...
    echo "    Done."
fi

if $do_build; then
    echo "==> Building..."
    CGO_ENABLED=0 go build -trimpath -o "$BINARY" ./cmd/flywheel/
    echo "    Done. Built ./$BINARY"
fi
```

**Step 2: Make executable**

```bash
chmod +x build.sh
```

**Step 3: Verify**

```bash
./build.sh --all
```

Expected: vet passes, all tests pass (~39 tests across all packages), binary built at `./flywheel`.

**Step 4: Commit**

```bash
git add build.sh
git commit -m "Add build.sh for flywheel

Flags: --build, --test, --race, --vet, --clean, --all.
Never invoke go build/test directly — use ./build.sh."
```

---

### Task 7: Full verification and final commit

**Step 1: Run full verification**

```bash
./build.sh --clean
./build.sh --all
```

Expected: clean build, all tests pass, binary exists at `./flywheel`.

**Step 2: Run race detector**

```bash
./build.sh --race
```

Expected: no race conditions detected.

**Step 3: Verify binary works**

```bash
./flywheel --help
```

Expected: prints usage with all flags.

**Step 4: Check nothing was missed**

```bash
git status
go mod tidy
git diff go.mod go.sum
```

If go.mod/go.sum changed, commit:

```bash
git add go.mod go.sum
git commit -m "Tidy go.mod after package absorption"
```

**Step 5: Push**

```bash
git push
```
