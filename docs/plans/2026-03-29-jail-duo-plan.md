# Jail Duo Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run two Claude Code agents (Builder + Product Owner) in separate UnshareJails communicating through a shared MCP broker, proving inter-agent communication through jail boundaries.

**Architecture:** Extends the jail-test pattern to two agents. One broker, two MCP servers (different roles/sockets), two jails, two tmux panes (PO left, Builder right). PO starts first, Builder after a delay. Both use curl to talk to their MCP socket. Driver waits for two `write_done` events.

**Tech Stack:** Go 1.26.1, go-flags, flywheel's broker/mcp/jail packages, tmux

---

### Task 1: Create cmd/jail-duo skeleton with CLI parsing

**Files:**
- Create: `cmd/jail-duo/main.go`

**Step 1: Create the file**

```go
package main

import (
	"fmt"
	"os"
	"time"

	flags "github.com/jessevdk/go-flags"
)

type Options struct {
	BuilderWorkspace string        `long:"builder-workspace" required:"true" description:"Builder agent workspace directory"`
	POWorkspace      string        `long:"po-workspace" required:"true" description:"Product Owner agent workspace directory"`
	TmuxSession      string        `long:"tmux-session" description:"Use existing tmux session instead of creating one"`
	Keep             bool          `long:"keep" description:"Don't tear down on exit (for debugging)"`
	Timeout          time.Duration `long:"timeout" default:"5m" description:"Max time to wait for both agents to finish"`
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
		fmt.Fprintf(os.Stderr, "[jail-duo] error: %v\n", err)
		os.Exit(1)
	}
}

func run(opts Options) error {
	fmt.Println("[jail-duo] starting...")
	return fmt.Errorf("not yet implemented")
}
```

**Step 2: Add `jail-duo` to `.gitignore`**

Append `/jail-duo` to `.gitignore`.

**Step 3: Verify**

```bash
go build ./cmd/jail-duo/
```

**Step 4: Commit**

```bash
git add cmd/jail-duo/main.go .gitignore
git commit -m "Add cmd/jail-duo skeleton with CLI flag parsing"
```

---

### Task 2: Create tmux helpers for split-pane layout

**Files:**
- Create: `cmd/jail-duo/tmux.go`

**Step 1: Create the file**

```go
package main

import (
	"fmt"
	"os/exec"
	"strings"
)

type duoTmux struct {
	name    string
	created bool
}

// newDuoTmux creates a tmux session with the PO command as the initial pane,
// then splits horizontally to add the Builder pane.
func newDuoTmux(sessionName, poCommand, builderCommand string, delay int) (*duoTmux, error) {
	name := sessionName
	if name == "" {
		name = "jail-duo"
	}
	// Kill stale session
	exec.Command("tmux", "kill-session", "-t", name).Run()

	// Create session with PO as the first pane
	cmd := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "200", "-y", "50",
		"bash", "-c", poCommand)
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}

	// Wait for PO to initialize before starting Builder
	if delay > 0 {
		fmt.Printf("[jail-duo] waiting %ds for PO to initialize...\n", delay)
		sleepCmd := fmt.Sprintf("sleep %d && %s", delay, builderCommand)
		splitCmd := exec.Command("tmux", "split-window", "-h", "-t", name,
			"bash", "-c", sleepCmd)
		if err := splitCmd.Run(); err != nil {
			return nil, fmt.Errorf("split tmux pane: %w", err)
		}
	}

	return &duoTmux{name: name, created: true}, nil
}

// anyPaneAlive returns true if at least one pane in the session is still running.
func (t *duoTmux) anyPaneAlive() bool {
	out, err := exec.Command("tmux", "list-panes", "-t", t.name, "-F", "#{pane_dead}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "0" {
			return true
		}
	}
	return false
}

// destroy kills the tmux session (only if we created it).
func (t *duoTmux) destroy() {
	if t.created {
		exec.Command("tmux", "kill-session", "-t", t.name).Run()
	}
}
```

**Step 2: Verify**

```bash
go build ./cmd/jail-duo/
```

**Step 3: Commit**

```bash
git add cmd/jail-duo/tmux.go
git commit -m "Add split-pane tmux helpers for jail-duo"
```

---

### Task 3: Extract shared jail setup into a helper

The jail setup logic (ephemeral config copy, mount table, chmod, trust injection) is identical for both agents except for workspace path, MCP socket path, and role. Extract it to avoid duplication.

**Files:**
- Create: `cmd/jail-duo/jailsetup.go`

**Step 1: Create the file**

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/corruptmemory/flywheel/jail"
)

type agentJail struct {
	jail       *jail.UnshareJail
	scriptPath string
	fullCmd    string
}

// setupAgentJail creates a complete jail environment for one agent.
// Returns the jail, the generated script path, and the full tmux command.
func setupAgentJail(
	tempBase string,
	agentName string,
	workspace string,
	mcpSocketDir string,
	promptContent string,
	currentUser *user.User,
) (*agentJail, error) {
	jailRoot := filepath.Join(tempBase, agentName, "root")
	configCopy := filepath.Join(tempBase, agentName, "claude-config")

	for _, d := range []string{jailRoot, configCopy} {
		if err := os.MkdirAll(d, 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	// Copy ~/.claude
	claudeDir := filepath.Join(currentUser.HomeDir, ".claude")
	claudeCopyDest := filepath.Join(configCopy, ".claude")
	cpCmd := exec.Command("cp", "-a", claudeDir, claudeCopyDest)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("copy .claude: %s: %w", string(out), err)
	}
	exec.Command("chmod", "-R", "a+rwX", claudeCopyDest).Run()

	// Copy ~/.claude.json and inject workspace trust
	claudeJSON := filepath.Join(currentUser.HomeDir, ".claude.json")
	claudeJSONCopy := filepath.Join(configCopy, ".claude.json")
	hasClaudeJSON := false
	if _, err := os.Stat(claudeJSON); err == nil {
		hasClaudeJSON = true
		cpCmd2 := exec.Command("cp", "-a", claudeJSON, claudeJSONCopy)
		if out, err := cpCmd2.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("copy .claude.json: %s: %w", string(out), err)
		}
		os.Chmod(claudeJSONCopy, 0666)
		injectWorkspaceTrust(claudeJSONCopy)
	}

	// Write prompt
	os.MkdirAll(workspace, 0755)
	os.Chmod(workspace, 0777)
	if err := os.WriteFile(filepath.Join(workspace, ".flywheel-prompt.md"), []byte(promptContent), 0644); err != nil {
		return nil, fmt.Errorf("write prompt: %w", err)
	}

	// Set up jail
	j := jail.NewUnshareJail(jailRoot)
	j.SetDropUser(currentUser.Username)

	homeInJail := filepath.Join(jailRoot, "home", currentUser.Username)
	workspaceInJail := filepath.Join(jailRoot, "workspace")
	mcpInJail := filepath.Join(jailRoot, "run", "mcp")
	tmpInJail := filepath.Join(jailRoot, "tmp")

	mounts := append(
		jail.HostMounts(jailRoot),
		jail.BindMount("/opt/claude-code", filepath.Join(jailRoot, "opt/claude-code"), true),
		jail.BindMount(claudeCopyDest, filepath.Join(homeInJail, ".claude"), false),
	)
	mounts = append(mounts,
		jail.BindMount(workspace, workspaceInJail, false),
		jail.TmpfsMount(tmpInJail),
		jail.BindMount(mcpSocketDir, mcpInJail, false),
	)

	if err := j.Setup(mounts); err != nil {
		return nil, fmt.Errorf("jail setup: %w", err)
	}

	// Create home dir and copy .claude.json
	os.MkdirAll(homeInJail, 0777)
	if hasClaudeJSON {
		exec.Command("cp", "-a", claudeJSONCopy, filepath.Join(homeInJail, ".claude.json")).Run()
	}

	// Generate jail script
	claudeInvocation := `/opt/claude-code/bin/claude --permission-mode bypassPermissions "Read .flywheel-prompt.md and follow ALL instructions exactly. Do not summarize or ask questions."`
	script := j.Script("/bin/bash", "-c", fmt.Sprintf("cd /workspace && exec %s", claudeInvocation))
	scriptPath := filepath.Join(tempBase, agentName, "jail-run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return nil, fmt.Errorf("write jail script: %w", err)
	}

	// Build tmux command
	unshareFlags := j.UnshareArgs()
	fullCmd := fmt.Sprintf("HOME=/home/%s TERM=xterm-256color DISABLE_AUTOUPDATER=1 unshare %s -- bash %s",
		currentUser.Username,
		strings.Join(unshareFlags, " "),
		scriptPath,
	)

	return &agentJail{jail: j, scriptPath: scriptPath, fullCmd: fullCmd}, nil
}

func injectWorkspaceTrust(claudeJSONPath string) {
	data, err := os.ReadFile(claudeJSONPath)
	if err != nil {
		return
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}
	projects, ok := cfg["projects"].(map[string]any)
	if !ok {
		projects = make(map[string]any)
		cfg["projects"] = projects
	}
	projects["/workspace"] = map[string]any{
		"allowedTools":           []any{},
		"hasTrustDialogAccepted": true,
	}
	out, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	os.WriteFile(claudeJSONPath, out, 0666)
}
```

**Step 2: Verify**

```bash
go build ./cmd/jail-duo/
```

**Step 3: Commit**

```bash
git add cmd/jail-duo/jailsetup.go
git commit -m "Add shared jail setup helper for jail-duo agents"
```

---

### Task 4: Write the prompt templates

**Files:**
- Create: `cmd/jail-duo/prompts.go`

**Step 1: Create the file**

```go
package main

const specContent = `# Spec: Hello Service
Build a CLI tool that prints a greeting.
The greeting should include the user's name.
`

const builderPrompt = `# Builder Instructions

You are the Builder agent. Read spec.md, notice the ambiguity, ask the Product Owner for clarification, then implement.

1. Read spec.md in this workspace.

2. The spec doesn't say where the user's name comes from. Send a PLEASE CLARIFY request to the Product Owner:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_request","arguments":{"type":"PLEASE CLARIFY","content":"The spec says the greeting should include the users name, but does not specify where the name comes from. Should it be a CLI argument, environment variable, or read from stdin?"}}}'

3. Wait 15 seconds for the PO to respond, then check:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"check_response","arguments":{"request_id":"000001"}}}'

4. If the response is not ready, wait another 15 seconds and try step 3 again. Repeat up to 5 times.

5. Once you have the PO's response, implement the spec accordingly. Create a file called hello.go with the implementation.

6. Signal completion:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"write_done","arguments":{"status":"PASS","details":"Implemented hello.go per PO clarification."}}}'

7. Report what happened.

Do not ask questions. Execute these steps.
`

const poPrompt = `# Product Owner Instructions

You are the Product Owner. Your job is to wait for and respond to requests from other agents.

1. Poll for pending requests:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_pending_requests","arguments":{}}}'

2. If no requests yet (the result content text shows an empty list), wait 10 seconds and repeat step 1. Keep polling until you get a request. Try up to 12 times (2 minutes total).

3. When you see a request, read it:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"read_request","arguments":{"request_id":"000001"}}}'

4. Send your response — the user's name should come from a CLI argument:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"send_response","arguments":{"request_id":"000001","content":"The users name should be provided as a CLI argument. Usage: ./hello <name>. If no argument is given, print a usage message to stderr and exit with code 1."}}}'

5. Signal completion:
   curl --unix-socket /run/mcp/mcp.sock -s -X POST http://localhost/ -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"write_done","arguments":{"status":"PASS","details":"Responded to all requests."}}}'

6. Report what happened.

Do not ask questions. Execute these steps.
`
```

**Step 2: Verify**

```bash
go build ./cmd/jail-duo/
```

**Step 3: Commit**

```bash
git add cmd/jail-duo/prompts.go
git commit -m "Add Builder and PO prompt templates for jail-duo"
```

---

### Task 5: Implement the main flow

Wire everything together: broker, two MCP servers, two jails, tmux split panes, dual event wait loop.

**Files:**
- Modify: `cmd/jail-duo/main.go`

**Step 1: Replace the stub `run()` function with the full implementation**

```go
func run(opts Options) error {
	builderWorkspace, err := filepath.Abs(opts.BuilderWorkspace)
	if err != nil {
		return fmt.Errorf("resolve builder workspace: %w", err)
	}
	poWorkspace, err := filepath.Abs(opts.POWorkspace)
	if err != nil {
		return fmt.Errorf("resolve po workspace: %w", err)
	}

	// Create temp base
	tempBase, err := os.MkdirTemp("", "jail-duo-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	if !opts.Keep {
		defer os.RemoveAll(tempBase)
	} else {
		fmt.Printf("[jail-duo] --keep: temp dir at %s\n", tempBase)
	}

	// Shared directories
	requestsDir := filepath.Join(tempBase, "requests")
	handoffDir := filepath.Join(tempBase, "handoff")
	builderMCPDir := filepath.Join(tempBase, "mcp-builder")
	poMCPDir := filepath.Join(tempBase, "mcp-po")

	for _, d := range []string{requestsDir, handoffDir, builderMCPDir, poMCPDir} {
		if err := os.MkdirAll(d, 0777); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	currentUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	// --- Start broker ---
	fmt.Println("[jail-duo] starting broker...")
	brk, err := broker.New(broker.Config{
		RequestsDir: requestsDir,
		HandoffDir:  handoffDir,
		EventBuffer: 16,
	})
	if err != nil {
		return fmt.Errorf("create broker: %w", err)
	}
	defer brk.Stop()

	events, err := brk.Subscribe("driver")
	if err != nil {
		return fmt.Errorf("subscribe to broker: %w", err)
	}

	// --- Start two MCP servers ---
	builderSocket := filepath.Join(builderMCPDir, "mcp.sock")
	fmt.Printf("[jail-duo] starting builder MCP server at %s\n", builderSocket)
	builderMCP, err := mcp.NewServer(mcp.Config{
		Role:       "builder",
		SocketPath: builderSocket,
		Broker:     brk,
	})
	if err != nil {
		return fmt.Errorf("create builder MCP: %w", err)
	}
	os.Chmod(builderSocket, 0777)
	defer builderMCP.Close()
	go builderMCP.Serve()

	poSocket := filepath.Join(poMCPDir, "mcp.sock")
	fmt.Printf("[jail-duo] starting PO MCP server at %s\n", poSocket)
	poMCP, err := mcp.NewServer(mcp.Config{
		Role:       "product-owner",
		SocketPath: poSocket,
		Broker:     brk,
	})
	if err != nil {
		return fmt.Errorf("create PO MCP: %w", err)
	}
	os.Chmod(poSocket, 0777)
	defer poMCP.Close()
	go poMCP.Serve()

	// --- Check preflight ---
	fmt.Println("[jail-duo] checking unshare preflight...")
	if result := jail.CheckUnsharePreflight(); !result.OK() {
		return fmt.Errorf("unshare not available: %s", result.Error())
	}

	// --- Write spec.md to both workspaces ---
	for _, ws := range []string{builderWorkspace, poWorkspace} {
		os.MkdirAll(ws, 0755)
		os.WriteFile(filepath.Join(ws, "spec.md"), []byte(specContent), 0644)
	}

	// --- Set up jails ---
	fmt.Println("[jail-duo] setting up PO jail...")
	poJail, err := setupAgentJail(tempBase, "po", poWorkspace, poMCPDir, poPrompt, currentUser)
	if err != nil {
		return fmt.Errorf("PO jail: %w", err)
	}
	if !opts.Keep {
		defer poJail.jail.Teardown()
	}

	fmt.Println("[jail-duo] setting up Builder jail...")
	builderJail, err := setupAgentJail(tempBase, "builder", builderWorkspace, builderMCPDir, builderPrompt, currentUser)
	if err != nil {
		return fmt.Errorf("Builder jail: %w", err)
	}
	if !opts.Keep {
		defer builderJail.jail.Teardown()
	}

	// --- Launch both in tmux (PO first, Builder after delay) ---
	fmt.Println("[jail-duo] creating tmux session (PO left, Builder right)...")
	tmux, err := newDuoTmux(opts.TmuxSession, poJail.fullCmd, builderJail.fullCmd, 5)
	if err != nil {
		return fmt.Errorf("tmux: %w", err)
	}
	if !opts.Keep {
		defer tmux.destroy()
	}

	fmt.Printf("[jail-duo] agents launched in tmux session %q\n", tmux.name)
	fmt.Printf("[jail-duo] attach with: tmux attach -t %s\n", tmux.name)

	// --- Wait for BOTH write_done events ---
	fmt.Printf("[jail-duo] waiting for both write_done events (timeout: %s)...\n", opts.Timeout)

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	doneRoles := make(map[string]bool)

	for {
		select {
		case evt := <-events:
			fmt.Printf("[jail-duo] broker event: %s (role=%s)\n", evt.Event, evt.Role)
			if evt.Event == "write_done" {
				doneRoles[evt.Role] = true
				fmt.Printf("[jail-duo] %s done: status=%s (%d/2 complete)\n", evt.Role, evt.Summary, len(doneRoles))
				if doneRoles["builder"] && doneRoles["product-owner"] {
					fmt.Println("[jail-duo] SUCCESS — both agents completed")
					return nil
				}
			}
		case <-ticker.C:
			if !tmux.anyPaneAlive() {
				fmt.Println("[jail-duo] all tmux panes exited")
				if len(doneRoles) > 0 {
					fmt.Printf("[jail-duo] partial: %d/2 agents completed\n", len(doneRoles))
				}
				return fmt.Errorf("agents exited before both wrote done (%d/2)", len(doneRoles))
			}
			fmt.Printf("[jail-duo] still waiting... (%d/2 done)\n", len(doneRoles))
		case <-ctx.Done():
			fmt.Printf("[jail-duo] timeout — %d/2 agents completed\n", len(doneRoles))
			fmt.Println("[jail-duo] check tmux: tmux attach -t " + tmux.name)
			return fmt.Errorf("timeout after %s", opts.Timeout)
		}
	}
}
```

Add these imports to the top of main.go:

```go
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
```

**Step 2: Verify**

```bash
go build ./cmd/jail-duo/
```

**Step 3: Commit**

```bash
git add cmd/jail-duo/main.go
git commit -m "Implement jail-duo main flow: broker, 2x MCP, 2x jails, tmux split, dual event loop"
```

---

### Task 6: First test run

**Step 1: Build and run**

```bash
cd ~/projects/flywheel
go build -o jail-duo ./cmd/jail-duo/
rm -rf /tmp/jail-duo-* /tmp/duo-builder /tmp/duo-po
mkdir /tmp/duo-builder /tmp/duo-po
./jail-duo --builder-workspace /tmp/duo-builder --po-workspace /tmp/duo-po --keep 2>&1 | tee /tmp/jail-duo.log
```

**Step 2: Watch in tmux**

```bash
tmux attach -t jail-duo
```

Should see two panes: PO on left polling for requests, Builder on right reading spec and sending a PLEASE CLARIFY.

**Step 3: Watch the driver log**

Expected output:
```
[jail-duo] broker event: new_request (role=builder)
[jail-duo] broker event: response_ready (role=product-owner)
[jail-duo] broker event: write_done (role=product-owner)
[jail-duo] product-owner done: status=PASS (1/2 complete)
[jail-duo] broker event: write_done (role=builder)
[jail-duo] builder done: status=PASS (2/2 complete)
[jail-duo] SUCCESS — both agents completed
```

**Step 4: Check artifacts**

```bash
cat /tmp/duo-builder/hello.go      # Builder's implementation
cat /tmp/duo-builder/spec.md       # Original spec
ls /tmp/jail-duo-*/handoff/        # Done files: builder_done, po_done
ls /tmp/jail-duo-*/requests/       # Request/response files
```

**Step 5: Record findings and iterate**

If something fails, record what broke, fix it, re-run. Same empirical approach as jail-test (14 iterations).

**Step 6: Commit working state**

```bash
git add -A
git commit -m "Jail duo working: two Claude Code agents communicating via MCP broker"
git push
```
