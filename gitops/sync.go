package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Sync updates agent repo's working tree from target.
// preserveDirs are agent-specific directories that should not be overwritten.
func Sync(targetDir, agentDir string, preserveDirs []string) error {
	// 1. Get agent's current branch (for the commit message).
	branch, err := gitOutputInDir(agentDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}

	preserveSet := make(map[string]bool)
	preserveSet[".git"] = true
	for _, d := range preserveDirs {
		preserveSet[strings.TrimSuffix(d, "/")] = true
	}

	// 2. Remove all non-.git, non-preserved files from agent.
	agentEntries, err := os.ReadDir(agentDir)
	if err != nil {
		return err
	}
	for _, e := range agentEntries {
		if preserveSet[e.Name()] {
			continue
		}
		if err := os.RemoveAll(filepath.Join(agentDir, e.Name())); err != nil {
			return err
		}
	}

	// 3. Copy all non-.git, non-preserved files from target to agent.
	targetEntries, err := os.ReadDir(targetDir)
	if err != nil {
		return err
	}
	for _, e := range targetEntries {
		if preserveSet[e.Name()] {
			continue
		}
		src := filepath.Join(targetDir, e.Name())
		dst := filepath.Join(agentDir, e.Name())
		if err := copyEntry(src, dst); err != nil {
			return fmt.Errorf("copy %s: %w", e.Name(), err)
		}
	}

	// 4. git add -A, commit if changes exist.
	if err := gitInDir(agentDir, "add", "-A"); err != nil {
		return err
	}

	// Check if there are staged changes.
	status, err := gitOutputInDir(agentDir, "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		msg := fmt.Sprintf("Sync from target to %s", branch)
		if err := gitInDir(agentDir, "commit", "-m", msg); err != nil {
			return fmt.Errorf("commit sync: %w", err)
		}
	}

	// 5. Set transplant marker.
	return SetTransplantMarker(agentDir)
}

// copyEntry copies a file or directory using cp -a.
func copyEntry(src, dst string) error {
	return copyRepo(src, dst) // reuse cp -a helper
}
