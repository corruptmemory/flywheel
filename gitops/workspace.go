package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ReviewerConfig describes a reviewer agent's branch and directory layout.
type ReviewerConfig struct {
	Name        string // e.g., "code-reviewer"
	Branch      string // e.g., "code-reviewer"
	ToolsDir    string // e.g., "code-review-tools"
	FeedbackDir string // e.g., "code-review-feedback"
}

// WorkspaceConfig holds the parameters for InitWorkspace.
type WorkspaceConfig struct {
	RootDir   string
	Reviewers []ReviewerConfig
	Specs     []string // placeholder spec filenames
}

// DefaultReviewers returns the standard set of reviewer configurations.
func DefaultReviewers() []ReviewerConfig {
	return []ReviewerConfig{
		{
			Name:        "code-reviewer",
			Branch:      "code-reviewer",
			ToolsDir:    "code-review-tools",
			FeedbackDir: "code-review-feedback",
		},
		{
			Name:        "security-reviewer",
			Branch:      "security-reviewer",
			ToolsDir:    "security-tools",
			FeedbackDir: "security-feedback",
		},
		{
			Name:        "qa",
			Branch:      "qa",
			ToolsDir:    "qa-tools",
			FeedbackDir: "qa-feedback",
		},
	}
}

// InitWorkspace creates the full multi-repo workspace layout.
func InitWorkspace(cfg WorkspaceConfig) error {
	root := cfg.RootDir

	// 1. Create and initialize target repo.
	targetDir := filepath.Join(root, "target")
	if err := initTargetRepo(targetDir, cfg); err != nil {
		return fmt.Errorf("init target repo: %w", err)
	}

	// 2. Copy target to build, product-owner, and each reviewer.
	agentNames := []string{"build", "product-owner"}
	for _, r := range cfg.Reviewers {
		agentNames = append(agentNames, r.Name)
	}

	for _, name := range agentNames {
		dst := filepath.Join(root, name)
		if err := copyRepo(targetDir, dst); err != nil {
			return fmt.Errorf("copy repo to %s: %w", name, err)
		}
		// Set git config in copy.
		if err := configureGitUser(dst); err != nil {
			return fmt.Errorf("configure git user in %s: %w", name, err)
		}
	}

	// 3. Checkout branches.
	if err := gitInDir(filepath.Join(root, "build"), "checkout", "-b", "builder"); err != nil {
		return fmt.Errorf("checkout builder branch: %w", err)
	}
	if err := gitInDir(filepath.Join(root, "product-owner"), "checkout", "-b", "product-owner"); err != nil {
		return fmt.Errorf("checkout product-owner branch: %w", err)
	}
	for _, r := range cfg.Reviewers {
		repoDir := filepath.Join(root, r.Name)
		if err := gitInDir(repoDir, "checkout", "-b", r.Branch); err != nil {
			return fmt.Errorf("checkout branch %s: %w", r.Branch, err)
		}
	}

	// 4. In each reviewer: create tools + feedback dirs, commit.
	for _, r := range cfg.Reviewers {
		repoDir := filepath.Join(root, r.Name)
		for _, d := range []string{r.ToolsDir, r.FeedbackDir} {
			dir := filepath.Join(repoDir, d)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644); err != nil {
				return err
			}
		}
		if err := gitInDir(repoDir, "add", "-A"); err != nil {
			return err
		}
		if err := gitInDir(repoDir, "commit", "-m", fmt.Sprintf("Add %s directories", r.Name)); err != nil {
			return err
		}
	}

	// 5. In target: add reviewer dirs + product-owner-requests/.
	for _, r := range cfg.Reviewers {
		for _, d := range []string{r.ToolsDir, r.FeedbackDir} {
			dir := filepath.Join(targetDir, d)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), nil, 0o644); err != nil {
				return err
			}
		}
	}
	poReqDir := filepath.Join(targetDir, "product-owner-requests")
	if err := os.MkdirAll(poReqDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(poReqDir, ".gitkeep"), nil, 0o644); err != nil {
		return err
	}
	if err := gitInDir(targetDir, "add", "-A"); err != nil {
		return err
	}
	if err := gitInDir(targetDir, "commit", "-m", "Add reviewer and product-owner directories"); err != nil {
		return err
	}

	// 6. Install all hooks.
	if err := InstallAllHooks(cfg); err != nil {
		return fmt.Errorf("install hooks: %w", err)
	}

	// 7. Create supporting dirs.
	for _, d := range []string{"jails", "handoff", "product-owner-requests"} {
		dir := filepath.Join(root, d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	return nil
}

// initTargetRepo creates and initializes the target git repository.
func initTargetRepo(dir string, cfg WorkspaceConfig) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := gitInDir(dir, "init"); err != nil {
		return err
	}
	if err := configureGitUser(dir); err != nil {
		return err
	}

	// Create README.md.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Project\n"), 0o644); err != nil {
		return err
	}

	// Create build-specifications/.
	specsDir := filepath.Join(dir, "build-specifications")
	if err := os.MkdirAll(specsDir, 0o755); err != nil {
		return err
	}
	if len(cfg.Specs) > 0 {
		for _, s := range cfg.Specs {
			if err := os.WriteFile(filepath.Join(specsDir, s), []byte("# "+s+"\n"), 0o644); err != nil {
				return err
			}
		}
	} else {
		if err := os.WriteFile(filepath.Join(specsDir, ".gitkeep"), nil, 0o644); err != nil {
			return err
		}
	}

	// Create feedback/.
	feedbackDir := filepath.Join(dir, "feedback")
	if err := os.MkdirAll(feedbackDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(feedbackDir, ".gitkeep"), nil, 0o644); err != nil {
		return err
	}

	// Initial commit.
	if err := gitInDir(dir, "add", "-A"); err != nil {
		return err
	}
	if err := gitInDir(dir, "commit", "-m", "Initial commit"); err != nil {
		return err
	}

	return nil
}

// configureGitUser sets user.email and user.name for non-interactive commits.
func configureGitUser(dir string) error {
	if err := gitInDir(dir, "config", "user.email", "lc@localhost"); err != nil {
		return err
	}
	return gitInDir(dir, "config", "user.name", "lc")
}

// gitInDir runs a git command in the specified directory.
func gitInDir(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// gitOutputInDir runs a git command in the specified directory and captures stdout.
func gitOutputInDir(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w\nstderr: %s", strings.Join(args, " "), err, ee.Stderr)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// copyRepo copies a repository using cp -a.
func copyRepo(src, dst string) error {
	cmd := exec.Command("cp", "-a", src, dst)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
