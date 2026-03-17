package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	flags "github.com/jessevdk/go-flags"

	"github.com/corruptmemory/flywheel/broker"
	"github.com/corruptmemory/flywheel/jail"
	"github.com/corruptmemory/flywheel/mcp"
)

type Options struct {
	Workspace   string        `short:"w" long:"workspace" required:"true" description:"Directory to mount as the agent's workspace"`
	TmuxSession string        `long:"tmux-session" description:"Use existing tmux session instead of creating one"`
	Role        string        `long:"role" default:"builder" description:"MCP role identity"`
	Prompt      string        `long:"prompt" description:"Prompt file to place in workspace (default: generates basic test prompt)"`
	Keep        bool          `long:"keep" description:"Don't tear down jail on exit (for debugging)"`
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
	claudeCopyDest := filepath.Join(configCopy, ".claude")
	fmt.Printf("[jail-test] copying %s to %s\n", claudeDir, claudeCopyDest)
	cpCmd := exec.Command("cp", "-a", claudeDir, claudeCopyDest)
	if out, err := cpCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy .claude: %s: %w", string(out), err)
	}

	// Also copy ~/.claude.json (Claude Code looks for it in $HOME, not inside ~/.claude/)
	claudeJSON := filepath.Join(currentUser.HomeDir, ".claude.json")
	claudeJSONCopy := filepath.Join(configCopy, ".claude.json")
	hasClaudeJSON := false
	if _, err := os.Stat(claudeJSON); err == nil {
		hasClaudeJSON = true
		fmt.Printf("[jail-test] copying %s to %s\n", claudeJSON, claudeJSONCopy)
		cpCmd2 := exec.Command("cp", "-a", claudeJSON, claudeJSONCopy)
		if out, err := cpCmd2.CombinedOutput(); err != nil {
			return fmt.Errorf("copy .claude.json: %s: %w", string(out), err)
		}
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
	if result := jail.CheckUnsharePreflight(); !result.OK() {
		return fmt.Errorf("unshare not available: %s", result.Error())
	}

	j := jail.NewUnshareJail(jailRoot)
	// Drop from namespace root to real user via runuser. Claude Code refuses
	// to run as root (UID 0). Username must exist in /etc/passwd (bind-mounted).
	j.SetDropUser(currentUser.Username)

	homeInJail := filepath.Join(jailRoot, "home", currentUser.Username)
	workspaceInJail := filepath.Join(jailRoot, "workspace")
	mcpInJail := filepath.Join(jailRoot, "run", "mcp")
	tmpInJail := filepath.Join(jailRoot, "tmp")

	mounts := append(
		jail.HostMounts(jailRoot),
		// Claude Code runtime (read-only)
		jail.BindMount("/opt/claude-code", filepath.Join(jailRoot, "opt/claude-code"), true),
		// Ephemeral ~/.claude/ directory (read-write)
		jail.BindMount(claudeCopyDest, filepath.Join(homeInJail, ".claude"), false),
	)
	mounts = append(mounts,
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

	// Create home directory and copy .claude.json directly (not bind-mounted —
	// bind-mounting a single file requires special handling that Setup's MkdirAll breaks)
	os.MkdirAll(homeInJail, 0755)
	if hasClaudeJSON {
		cpJSON := exec.Command("cp", "-a", claudeJSONCopy, filepath.Join(homeInJail, ".claude.json"))
		if out, err := cpJSON.CombinedOutput(); err != nil {
			return fmt.Errorf("copy .claude.json to jail: %s: %w", string(out), err)
		}
	}

	// --- Step 6: Generate the jail script ---
	// Use bash -c to cd to /workspace before running claude.
	// The chroot changes root but doesn't set CWD — claude defaults to
	// the host's CWD which doesn't exist inside the jail.
	claudeInvocation := `/opt/claude-code/bin/claude --permission-mode bypassPermissions "Read .flywheel-prompt.md and follow ALL instructions exactly. Do not summarize or ask questions."`
	script := j.Script("/bin/bash", "-c", fmt.Sprintf("cd /workspace && exec %s", claudeInvocation))
	scriptPath := filepath.Join(tempBase, "jail-run.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return fmt.Errorf("write jail script: %w", err)
	}
	fmt.Printf("[jail-test] jail script written to %s\n", scriptPath)

	// Build the tmux command: env vars + unshare [flags] -- bash <script>
	unshareFlags := j.UnshareArgs()
	tmuxCmd := fmt.Sprintf("HOME=/home/%s DISABLE_AUTOUPDATER=1 unshare %s -- bash %s",
		currentUser.Username,
		strings.Join(unshareFlags, " "),
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
