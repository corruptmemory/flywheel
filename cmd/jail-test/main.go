package main

import (
	"fmt"
	"os"
	"time"

	flags "github.com/jessevdk/go-flags"
)

type Options struct {
	Workspace   string        `short:"w" long:"workspace" required:"true" description:"Directory to mount as the agent's workspace"`
	TmuxSession string        `long:"tmux-session" description:"Use existing tmux session instead of creating one"`
	Role        string        `long:"role" default:"builder" description:"MCP role identity"`
	Prompt      string        `long:"prompt" description:"Prompt file to place in workspace (default: generates basic test prompt)"`
	Keep        bool          `long:"keep" description:"Don't tear down jail on exit (for debugging)"`
	Timeout     time.Duration `long:"timeout" default:"5m" description:"Max time to wait for agent to finish"`
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
		fmt.Fprintf(os.Stderr, "[jail-test] error: %v\n", err)
		os.Exit(1)
	}
}

func run(opts Options) error {
	fmt.Println("[jail-test] starting...")
	fmt.Printf("[jail-test] workspace: %s\n", opts.Workspace)
	fmt.Printf("[jail-test] role: %s\n", opts.Role)
	return fmt.Errorf("not yet implemented")
}
