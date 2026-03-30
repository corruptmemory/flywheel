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
