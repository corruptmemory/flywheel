# Jail Prototype Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a standalone test harness that runs one Claude Code session inside an UnshareJail with MCP coordination, to discover what Claude Code needs to function in a chroot.

**Architecture:** `cmd/jail-test/main.go` creates a jail, starts broker + MCP in-process, launches claude inside the jail via tmux, and waits for a `write_done` broker event. The jail's mount table is a best guess — expect iteration.

**Tech Stack:** Go 1.26.1, flywheel's jail/broker/mcp packages, tmux, go-flags

---

### Task 1: Add tmpfs mount support to jail package

The `Mount` struct currently handles bind mounts. We need tmpfs for the agent's `/tmp`. Add an `FSType` field — when set, mount uses `-t <fstype>` instead of `--bind`.

**Files:**
- Modify: `jail/mount.go`
- Modify: `jail/unshare.go` (script generation)

**Step 1: Add FSType field to Mount struct**

In `jail/mount.go`, add `FSType string` to the `Mount` struct:

```go
type Mount struct {
	Source    string
	Target    string
	Options   []string
	ReadOnly  bool
	FSType    string       // if set, use "mount -t FSType" instead of bind mount
	Condition func() bool
}
```

Add a helper:

```go
// TmpfsMount creates a tmpfs mount at the given target path.
func TmpfsMount(target string) Mount {
	return Mount{
		Source: "tmpfs",
		Target: target,
		FSType: "tmpfs",
	}
}
```

**Step 2: Update UnshareJail script generation**

In `jail/unshare.go`, in the `buildScript` method's mount loop (~line 87), check `FSType`:

```go
for _, m := range j.mounts {
	if m.Condition != nil && !m.Condition() {
		continue
	}
	fmt.Fprintf(&sb, "mkdir -p %s\n", shellQuote(m.Target))
	if m.FSType != "" {
		fmt.Fprintf(&sb, "mount -t %s %s %s\n", m.FSType, m.Source, shellQuote(m.Target))
	} else {
		fmt.Fprintf(&sb, "mount --bind %s %s\n", shellQuote(m.Source), shellQuote(m.Target))
		if m.ReadOnly {
			fmt.Fprintf(&sb, "mount -o remount,bind,ro %s\n", shellQuote(m.Target))
		}
	}
}
```

**Step 3: Verify**

```bash
go vet ./jail/... && go test ./jail/...
```

Expected: all existing tests pass, no regressions.

**Step 4: Commit**

```bash
git add jail/mount.go jail/unshare.go
git commit -m "Add FSType support to jail Mount for tmpfs"
```

---

### Task 2: Export jail script for tmux integration

The UnshareJail's `Command()` returns an `*exec.Cmd`, but tmux needs to execute the jail command as a string. Add a method to expose the generated script and unshare flags separately, so the harness can write the script to a file and send `unshare [flags] -- bash /path/to/script.sh` to tmux.

**Files:**
- Modify: `jail/unshare.go`

**Step 1: Add Script() and UnshareArgs() methods**

```go
// Script returns the bash script that sets up mounts, chroots, and execs the given command.
// Use with UnshareArgs() to run via tmux or other external process managers.
func (j *UnshareJail) Script(name string, args ...string) string {
	return j.buildScript(name, args...)
}

// UnshareArgs returns the unshare flags (without the trailing bash -c).
// Combine with Script() written to a file: unshare <flags> -- bash <script-file>
func (j *UnshareJail) UnshareArgs() []string {
	return []string{
		"--fork", "--pid", "--mount",
		"--map-auto", "--map-root-user",
		"--setuid", "0", "--setgid", "0",
	}
}
```

**Step 2: Verify**

```bash
go vet ./jail/... && go test ./jail/...
```

**Step 3: Commit**

```bash
git add jail/unshare.go
git commit -m "Export UnshareJail Script() and UnshareArgs() for tmux integration"
```

---

### Task 3: Create cmd/jail-test skeleton with CLI parsing

**Files:**
- Create: `cmd/jail-test/main.go`

**Step 1: Create the file with flag parsing and usage**

```go
package main

import (
	"fmt"
	"os"

	flags "github.com/jessevdk/go-flags"
)

type Options struct {
	Workspace   string `short:"w" long:"workspace" required:"true" description:"Directory to mount as the agent's workspace"`
	TmuxSession string `long:"tmux-session" description:"Use existing tmux session instead of creating one"`
	Role        string `long:"role" default:"builder" description:"MCP role identity"`
	Prompt      string `long:"prompt" description:"Prompt file to place in workspace (default: generates basic test prompt)"`
	Keep        bool   `long:"keep" description:"Don't tear down jail on exit (for debugging)"`
}

func main() {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(opts Options) error {
	fmt.Println("[jail-test] starting...")
	fmt.Printf("[jail-test] workspace: %s\n", opts.Workspace)
	fmt.Printf("[jail-test] role: %s\n", opts.Role)

	// TODO: implement in subsequent tasks
	return fmt.Errorf("not yet implemented")
}
```

**Step 2: Verify it compiles**

```bash
go build ./cmd/jail-test/
```

**Step 3: Add `jail-test` to .gitignore**

Append `jail-test` to `.gitignore`.

**Step 4: Commit**

```bash
git add cmd/jail-test/main.go .gitignore
git commit -m "Add cmd/jail-test skeleton with CLI flag parsing"
```

---

### Task 4: Implement tmux session management

**Files:**
- Create: `cmd/jail-test/tmux.go`

**Step 1: Write tmux helpers**

```go
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// tmuxSession manages a tmux session lifecycle.
type tmuxSession struct {
	name    string
	created bool // true if we created it (and should destroy on cleanup)
}

// newTmuxSession creates a new tmux session or wraps an existing one.
// If sessionName is empty, generates a name and creates a new session.
func newTmuxSession(sessionName string) (*tmuxSession, error) {
	if sessionName != "" {
		// Verify it exists
		if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err != nil {
			return nil, fmt.Errorf("tmux session %q not found", sessionName)
		}
		return &tmuxSession{name: sessionName, created: false}, nil
	}

	name := "jail-test"
	// Create detached session
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "200", "-y", "50")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}
	return &tmuxSession{name: name, created: true}, nil
}

// sendKeys sends a command string to the session's first pane.
func (t *tmuxSession) sendKeys(command string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", t.name, command, "Enter")
	return cmd.Run()
}

// waitForPaneDeath polls until the pane's process exits.
// Returns the exit status string or error.
func (t *tmuxSession) waitForPaneDeath() error {
	// tmux wait-for blocks until the channel is signaled.
	// The pane's remain-on-exit + hooks could signal it, but for the
	// prototype, poll list-panes and check if the command is still running.
	// This is a fallback — the broker event is the primary completion signal.
	return nil // TODO: implement if broker event isn't sufficient
}

// paneAlive checks if the pane's process is still running.
func (t *tmuxSession) paneAlive() bool {
	out, err := exec.Command("tmux", "list-panes", "-t", t.name, "-F", "#{pane_dead}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "0"
}

// destroy kills the tmux session (only if we created it).
func (t *tmuxSession) destroy() {
	if t.created {
		exec.Command("tmux", "kill-session", "-t", t.name).Run()
	}
}
```

**Step 2: Verify**

```bash
go build ./cmd/jail-test/
```

**Step 3: Commit**

```bash
git add cmd/jail-test/tmux.go
git commit -m "Add tmux session management for jail-test"
```

---

### Task 5: Implement the main flow

Wire everything together: broker, MCP server, jail setup, tmux, event loop.

**Files:**
- Modify: `cmd/jail-test/main.go`

**Step 1: Implement the `run` function**

Replace the TODO `run` function with the full implementation:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	flags "github.com/jessevdk/go-flags"

	"github.com/corruptmemory/flywheel/broker"
	"github.com/corruptmemory/flywheel/jail"
	"github.com/corruptmemory/flywheel/mcp"
)

type Options struct {
	Workspace   string `short:"w" long:"workspace" required:"true" description:"Directory to mount as the agent's workspace"`
	TmuxSession string `long:"tmux-session" description:"Use existing tmux session instead of creating one"`
	Role        string `long:"role" default:"builder" description:"MCP role identity"`
	Prompt      string `long:"prompt" description:"Prompt file to place in workspace (default: generates basic test prompt)"`
	Keep        bool   `long:"keep" description:"Don't tear down jail on exit (for debugging)"`
	Timeout     time.Duration `long:"timeout" default:"5m" description:"Max time to wait for agent to finish"`
}

const defaultPrompt = `# Test Prompt

You are inside a jail test. Complete these steps:

1. Run: ls -la /workspace/
2. Create the file /workspace/hello.txt containing "Hello from jail!"
3. Run: cat /workspace/hello.txt
4. Call the write_done MCP tool with status "PASS" and details "All four steps completed."

Do not ask questions. Just execute these steps.
`

func main() {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "[jail-test] error: %v\n", err)
		os.Exit(1)
	}
}

func run(opts Options) error {
	// Resolve paths
	workspace, err := filepath.Abs(opts.Workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return fmt.Errorf("create workspace: %w", err)
	}

	// Create temp directory for jail root, config copy, etc.
	tempBase, err := os.MkdirTemp("", "jail-test-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	if !opts.Keep {
		defer os.RemoveAll(tempBase)
	} else {
		fmt.Printf("[jail-test] --keep: temp dir at %s\n", tempBase)
	}

	jailRoot := filepath.Join(tempBase, "root")
	configCopy := filepath.Join(tempBase, "claude-config")
	requestsDir := filepath.Join(tempBase, "requests")
	handoffDir := filepath.Join(tempBase, "handoff")
	mcpSocketDir := filepath.Join(tempBase, "mcp")

	for _, d := range []string{jailRoot, configCopy, requestsDir, handoffDir, mcpSocketDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// --- Step 1: Copy ~/.claude to ephemeral location ---
	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}
	claudeDir := filepath.Join(currentUser.HomeDir, ".claude")
	fmt.Printf("[jail-test] copying %s to %s\n", claudeDir, configCopy)
	cpCmd := exec.Command("cp", "-a", claudeDir, filepath.Join(configCopy, ".claude"))
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy .claude: %s: %w", string(out), err)
	}

	// --- Step 2: Write prompt file to workspace ---
	promptContent := defaultPrompt
	if opts.Prompt != "" {
		data, err := os.ReadFile(opts.Prompt)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}
		promptContent = string(data)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".flywheel-prompt.md"), []byte(promptContent), 0644); err != nil {
		return fmt.Errorf("write prompt: %w", err)
	}

	// --- Step 3: Start broker ---
	fmt.Println("[jail-test] starting broker...")
	brk, err := broker.New(broker.Config{
		RequestsDir: requestsDir,
		HandoffDir:  handoffDir,
		EventBuffer: 16,
	})
	if err != nil {
		return fmt.Errorf("create broker: %w", err)
	}
	defer brk.Stop()

	// Subscribe to events before starting agent
	events, err := brk.Subscribe("driver")
	if err != nil {
		return fmt.Errorf("subscribe to broker: %w", err)
	}

	// --- Step 4: Start MCP server ---
	socketPath := filepath.Join(mcpSocketDir, "mcp.sock")
	fmt.Printf("[jail-test] starting MCP server at %s (role: %s)\n", socketPath, opts.Role)
	mcpServer, err := mcp.NewServer(mcp.Config{
		Role:       opts.Role,
		SocketPath: socketPath,
		Broker:     brk,
	})
	if err != nil {
		return fmt.Errorf("create MCP server: %w", err)
	}
	defer mcpServer.Close()

	// --- Step 5: Set up jail ---
	fmt.Println("[jail-test] checking unshare preflight...")
	if result := jail.CheckUnsharePreflight(); result.Error() != nil {
		return fmt.Errorf("unshare not available: %s", result.Error())
	}

	j := jail.NewUnshareJail(jailRoot)

	homeInJail := filepath.Join(jailRoot, "home", currentUser.Username)
	workspaceInJail := filepath.Join(jailRoot, "workspace")
	mcpInJail := filepath.Join(jailRoot, "run", "mcp")
	tmpInJail := filepath.Join(jailRoot, "tmp")

	mounts := append(
		jail.HostMounts(jailRoot),
		// Claude Code runtime (read-only)
		jail.BindMount("/opt/claude-code", filepath.Join(jailRoot, "opt/claude-code"), true),
		// Ephemeral ~/.claude copy (read-write)
		jail.BindMount(filepath.Join(configCopy, ".claude"), filepath.Join(homeInJail, ".claude"), false),
		// Workspace (read-write)
		jail.BindMount(workspace, workspaceInJail, false),
		// tmpfs for scratch
		jail.TmpfsMount(tmpInJail),
		// MCP socket directory
		jail.BindMount(mcpSocketDir, mcpInJail, false),
	)

	fmt.Printf("[jail-test] setting up jail at %s (%d mounts)\n", jailRoot, len(mounts))
	if err := j.Setup(mounts); err != nil {
		return fmt.Errorf("jail setup: %w", err)
	}
	if !opts.Keep {
		defer j.Teardown()
	}

	// Create home directory in jail
	os.MkdirAll(homeInJail, 0755)

	// --- Step 6: Generate the jail script ---
	claudeArgs := []string{
		"--permission-mode", "bypassPermissions",
		"Read .flywheel-prompt.md and follow ALL instructions exactly. Do not summarize or ask questions.",
	}
	script := j.Script("/opt/claude-code/bin/claude", claudeArgs...)
	scriptPath := filepath.Join(tempBase, "jail-run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write jail script: %w", err)
	}
	fmt.Printf("[jail-test] jail script written to %s\n", scriptPath)

	// Build the tmux command: unshare [flags] -- bash <script>
	unshareFlags := j.UnshareArgs()
	tmuxCmd := fmt.Sprintf("cd /workspace && HOME=/home/%s unshare %s -- bash %s",
		currentUser.Username,
		joinArgs(unshareFlags),
		scriptPath,
	)

	// --- Step 7: Create tmux session and launch ---
	fmt.Println("[jail-test] creating tmux session...")
	tmux, err := newTmuxSession(opts.TmuxSession)
	if err != nil {
		return fmt.Errorf("tmux: %w", err)
	}
	if !opts.Keep {
		defer tmux.destroy()
	}

	fmt.Printf("[jail-test] launching claude in tmux session %q\n", tmux.name)
	fmt.Printf("[jail-test] attach with: tmux attach -t %s\n", tmux.name)
	if err := tmux.sendKeys(tmuxCmd); err != nil {
		return fmt.Errorf("tmux send-keys: %w", err)
	}

	// --- Step 8: Wait for broker event ---
	fmt.Printf("[jail-test] waiting for write_done event (timeout: %s)...\n", opts.Timeout)

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case evt := <-events:
			fmt.Printf("[jail-test] broker event: %s (role=%s)\n", evt.Event, evt.Role)
			if evt.Event == "write_done" {
				fmt.Printf("[jail-test] SUCCESS — agent wrote done: status=%s\n", evt.Summary)
				return nil
			}
		case <-ticker.C:
			if !tmux.paneAlive() {
				return fmt.Errorf("claude process died (tmux pane exited)")
			}
			fmt.Println("[jail-test] still waiting...")
		case <-ctx.Done():
			return fmt.Errorf("timeout after %s waiting for write_done", opts.Timeout)
		}
	}
}

// joinArgs joins string slices with spaces for shell command construction.
func joinArgs(args []string) string {
	result := ""
	for i, a := range args {
		if i > 0 {
			result += " "
		}
		result += a
	}
	return result
}
```

Note: this requires adding `"os/exec"` and `"context"` to imports. The `exec` import is needed in `main.go` for the `cp` command.

**Step 2: Verify it compiles**

```bash
go build ./cmd/jail-test/
```

**Step 3: Commit**

```bash
git add cmd/jail-test/main.go
git commit -m "Implement jail-test main flow: broker, MCP, jail, tmux, event loop"
```

---

### Task 6: First test run

This is the empirical discovery phase. We expect failures.

**Step 1: Create a test workspace**

```bash
mkdir -p /tmp/jail-workspace
```

**Step 2: Run jail-test**

```bash
./jail-test --workspace /tmp/jail-workspace --keep 2>&1 | tee /tmp/jail-test.log
```

Use `--keep` so we can inspect the jail root if something fails.

**Step 3: In another terminal, attach to tmux**

```bash
tmux attach -t jail-test
```

Watch what happens. Claude will either:
- **Start successfully** — the mount table is good enough
- **Fail with missing library/file** — tells us what to add to the mount table
- **Fail with permission error** — tells us what needs read-write
- **Fail with network error** — tells us about DNS/API connectivity

**Step 4: Record findings**

Whatever happens, record in the commit message:
- What worked
- What failed
- What mount table changes are needed

```bash
git add -A
git commit -m "First jail-test run: [describe results]"
```

**Step 5: Iterate**

Each failure adds a mount, an env var, or a fix. Re-run until claude starts and completes the prompt. Expect 3-5 iterations.

---

### Task 7: Verify MCP round-trip

Once claude starts inside the jail, verify the MCP socket works by checking the broker received the `write_done` call.

**Step 1: Check the harness output**

The harness should print:
```
[jail-test] broker event: write_done (role=builder)
[jail-test] SUCCESS — agent wrote done: status=PASS
```

**Step 2: Check the workspace**

```bash
cat /tmp/jail-workspace/hello.txt
```

Expected: `Hello from jail!`

**Step 3: Check the handoff directory**

```bash
cat <tempdir>/handoff/builder_done
```

Expected: `PASS` on first line.

**Step 4: Commit the working state**

```bash
git add -A
git commit -m "Jail prototype working: Claude Code runs inside UnshareJail with MCP"
```

**Step 5: Push**

```bash
git push
```
