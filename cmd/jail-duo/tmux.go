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
// then splits horizontally to add the Builder pane after a delay.
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

	// Split horizontally to add Builder pane with delay
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
