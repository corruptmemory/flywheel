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
