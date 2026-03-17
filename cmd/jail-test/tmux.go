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
	// Kill any stale session with same name
	exec.Command("tmux", "kill-session", "-t", name).Run()
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
