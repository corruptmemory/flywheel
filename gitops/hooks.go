package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookScript generates a pre-commit hook shell script.
// If allowedDirs is non-empty, only files under those directories may be committed (allowlist mode).
// If blockedDirs is non-empty, files under those directories are rejected (blocklist mode).
func hookScript(allowedDirs []string, blockedDirs []string) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString("# Auto-generated pre-commit hook — do not edit.\n\n")
	sb.WriteString("files=$(git diff --cached --name-only)\n")
	sb.WriteString("if [ -z \"$files\" ]; then\n  exit 0\nfi\n\n")

	if len(allowedDirs) > 0 {
		// Allowlist mode: every file must be under one of the allowed dirs.
		sb.WriteString("for f in $files; do\n")
		sb.WriteString("  allowed=0\n")
		for _, d := range allowedDirs {
			dir := strings.TrimSuffix(d, "/") + "/"
			sb.WriteString(fmt.Sprintf("  case \"$f\" in %s*) allowed=1 ;; esac\n", dir))
		}
		sb.WriteString("  if [ \"$allowed\" -eq 0 ]; then\n")
		sb.WriteString("    echo \"BLOCKED: $f is not in an allowed directory\"\n")
		sb.WriteString("    exit 1\n")
		sb.WriteString("  fi\n")
		sb.WriteString("done\n")
	}

	if len(blockedDirs) > 0 {
		// Blocklist mode: no file may be under a blocked dir.
		sb.WriteString("for f in $files; do\n")
		for _, d := range blockedDirs {
			dir := strings.TrimSuffix(d, "/") + "/"
			sb.WriteString(fmt.Sprintf("  case \"$f\" in %s*) echo \"BLOCKED: $f is in a restricted directory\"; exit 1 ;; esac\n", dir))
		}
		sb.WriteString("done\n")
	}

	sb.WriteString("exit 0\n")
	return sb.String()
}

// InstallAllHooks installs pre-commit hooks in every agent repo.
func InstallAllHooks(cfg WorkspaceConfig) error {
	root := cfg.RootDir

	// Collect all reviewer-specific dirs plus common restricted dirs for builder.
	var allReviewerDirs []string
	for _, r := range cfg.Reviewers {
		allReviewerDirs = append(allReviewerDirs, r.ToolsDir, r.FeedbackDir)
	}
	builderBlocked := append([]string{"build-specifications", "feedback", "product-owner-requests"}, allReviewerDirs...)

	// Builder: blocklist.
	if err := installHook(filepath.Join(root, "build"), nil, builderBlocked); err != nil {
		return fmt.Errorf("install builder hook: %w", err)
	}

	// Product owner: allowlist build-specifications only.
	if err := installHook(filepath.Join(root, "product-owner"), []string{"build-specifications"}, nil); err != nil {
		return fmt.Errorf("install product-owner hook: %w", err)
	}

	// Reviewers: allowlist their own dirs only.
	for _, r := range cfg.Reviewers {
		repoDir := filepath.Join(root, r.Name)
		if err := installHook(repoDir, []string{r.ToolsDir, r.FeedbackDir}, nil); err != nil {
			return fmt.Errorf("install %s hook: %w", r.Name, err)
		}
	}

	return nil
}

// installHook writes a pre-commit hook script to the repo's .git/hooks/ directory.
func installHook(repoDir string, allowedDirs, blockedDirs []string) error {
	hooksDir := filepath.Join(repoDir, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	script := hookScript(allowedDirs, blockedDirs)
	hookPath := filepath.Join(hooksDir, "pre-commit")
	return os.WriteFile(hookPath, []byte(script), 0o755)
}
