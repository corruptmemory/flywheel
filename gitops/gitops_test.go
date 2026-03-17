package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// helper: configure git user in a repo for non-interactive commits.
func configGit(t *testing.T, dir string) {
	t.Helper()
	if err := gitInDir(dir, "config", "user.email", "test@test.local"); err != nil {
		t.Fatalf("git config email in %s: %v", dir, err)
	}
	if err := gitInDir(dir, "config", "user.name", "test"); err != nil {
		t.Fatalf("git config name in %s: %v", dir, err)
	}
}

func TestInitWorkspace(t *testing.T) {
	root := t.TempDir()
	cfg := WorkspaceConfig{
		RootDir:   root,
		Reviewers: DefaultReviewers(),
		Specs:     []string{"spec1.md", "spec2.md"},
	}

	if err := InitWorkspace(cfg); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}

	// Verify repos exist.
	for _, name := range []string{"target", "build", "product-owner", "code-reviewer", "security-reviewer", "qa"} {
		repoDir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
			t.Errorf("repo %s: .git not found: %v", name, err)
		}
	}

	// Verify branches.
	branchTests := []struct {
		repo   string
		branch string
	}{
		{"build", "builder"},
		{"product-owner", "product-owner"},
		{"code-reviewer", "code-reviewer"},
		{"security-reviewer", "security-reviewer"},
		{"qa", "qa"},
	}
	for _, tt := range branchTests {
		repoDir := filepath.Join(root, tt.repo)
		branch, err := gitOutputInDir(repoDir, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			t.Errorf("repo %s: get branch: %v", tt.repo, err)
			continue
		}
		if branch != tt.branch {
			t.Errorf("repo %s: expected branch %q, got %q", tt.repo, tt.branch, branch)
		}
	}

	// Verify reviewer dirs exist in their repos.
	for _, r := range DefaultReviewers() {
		repoDir := filepath.Join(root, r.Name)
		for _, d := range []string{r.ToolsDir, r.FeedbackDir} {
			if _, err := os.Stat(filepath.Join(repoDir, d, ".gitkeep")); err != nil {
				t.Errorf("repo %s: dir %s/.gitkeep not found: %v", r.Name, d, err)
			}
		}
	}

	// Verify supporting dirs.
	for _, d := range []string{"jails", "handoff", "product-owner-requests"} {
		if _, err := os.Stat(filepath.Join(root, d)); err != nil {
			t.Errorf("supporting dir %s not found: %v", d, err)
		}
	}

	// Verify specs in target.
	for _, s := range []string{"spec1.md", "spec2.md"} {
		if _, err := os.Stat(filepath.Join(root, "target", "build-specifications", s)); err != nil {
			t.Errorf("spec %s not found in target: %v", s, err)
		}
	}

	// Verify reviewer dirs in target.
	for _, r := range DefaultReviewers() {
		for _, d := range []string{r.ToolsDir, r.FeedbackDir} {
			if _, err := os.Stat(filepath.Join(root, "target", d, ".gitkeep")); err != nil {
				t.Errorf("target: dir %s/.gitkeep not found: %v", d, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, "target", "product-owner-requests", ".gitkeep")); err != nil {
		t.Error("target: product-owner-requests/.gitkeep not found")
	}
}

func TestHooksBlockDisallowedCommits(t *testing.T) {
	root := t.TempDir()
	cfg := WorkspaceConfig{
		RootDir:   root,
		Reviewers: DefaultReviewers(),
	}
	if err := InitWorkspace(cfg); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}

	// Builder should NOT be able to commit to build-specifications/.
	buildDir := filepath.Join(root, "build")
	specFile := filepath.Join(buildDir, "build-specifications", "evil.md")
	os.MkdirAll(filepath.Dir(specFile), 0o755)
	os.WriteFile(specFile, []byte("hacked\n"), 0o644)
	gitInDir(buildDir, "add", specFile)
	if err := gitInDir(buildDir, "commit", "-m", "should fail"); err == nil {
		t.Error("builder: commit to build-specifications/ should have been blocked")
	}

	// Reviewer should NOT be able to commit to README.md.
	crDir := filepath.Join(root, "code-reviewer")
	readmeFile := filepath.Join(crDir, "README.md")
	os.WriteFile(readmeFile, []byte("hacked\n"), 0o644)
	gitInDir(crDir, "add", readmeFile)
	if err := gitInDir(crDir, "commit", "-m", "should fail"); err == nil {
		t.Error("code-reviewer: commit to README.md should have been blocked")
	}
}

func TestHooksAllowPermittedCommits(t *testing.T) {
	root := t.TempDir()
	cfg := WorkspaceConfig{
		RootDir:   root,
		Reviewers: DefaultReviewers(),
	}
	if err := InitWorkspace(cfg); err != nil {
		t.Fatalf("InitWorkspace: %v", err)
	}

	// Reviewer can commit to their feedback dir.
	crDir := filepath.Join(root, "code-reviewer")
	fbFile := filepath.Join(crDir, "code-review-feedback", "review.md")
	if err := os.WriteFile(fbFile, []byte("looks good\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInDir(crDir, "add", fbFile); err != nil {
		t.Fatal(err)
	}
	if err := gitInDir(crDir, "commit", "-m", "add review feedback"); err != nil {
		t.Errorf("code-reviewer: commit to feedback dir should be allowed: %v", err)
	}

	// Builder can commit source files.
	buildDir := filepath.Join(root, "build")
	srcFile := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(srcFile, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := gitInDir(buildDir, "add", srcFile); err != nil {
		t.Fatal(err)
	}
	if err := gitInDir(buildDir, "commit", "-m", "add source file"); err != nil {
		t.Errorf("builder: commit source file should be allowed: %v", err)
	}
}

func TestTransplant(t *testing.T) {
	root := t.TempDir()

	// Create two repos: src and dst.
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")

	// Init src.
	os.MkdirAll(srcDir, 0o755)
	gitInDir(srcDir, "init")
	configGit(t, srcDir)
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("# src\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "initial")

	// Set marker at current HEAD (the "already transplanted" point).
	if err := SetTransplantMarker(srcDir); err != nil {
		t.Fatalf("SetTransplantMarker: %v", err)
	}

	// Init dst as a copy.
	copyRepo(srcDir, dstDir)
	configGit(t, dstDir)

	// Builder creates a new file and commits in src.
	os.WriteFile(filepath.Join(srcDir, "newfile.go"), []byte("package main\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "add newfile.go")

	// Transplant.
	if err := Transplant(srcDir, dstDir, ""); err != nil {
		t.Fatalf("Transplant: %v", err)
	}

	// Verify file exists in dst.
	if _, err := os.Stat(filepath.Join(dstDir, "newfile.go")); err != nil {
		t.Error("newfile.go should exist in dst after transplant")
	}

	// Verify marker was updated.
	srcHead, _ := gitOutputInDir(srcDir, "rev-parse", "HEAD")
	marker, err := readMarker(srcDir)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if marker != srcHead {
		t.Errorf("marker should be %s, got %s", srcHead, marker)
	}
}

func TestTransplantWithPathFilter(t *testing.T) {
	root := t.TempDir()

	// Create two repos: src and dst.
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")

	// Init src.
	os.MkdirAll(srcDir, 0o755)
	gitInDir(srcDir, "init")
	configGit(t, srcDir)
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("# src\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "initial")

	// Set marker at current HEAD.
	if err := SetTransplantMarker(srcDir); err != nil {
		t.Fatalf("SetTransplantMarker: %v", err)
	}

	// Init dst as a copy.
	copyRepo(srcDir, dstDir)
	configGit(t, dstDir)

	// In src: commit changes to both allowed/ and blocked/.
	os.MkdirAll(filepath.Join(srcDir, "allowed"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "allowed", "file.go"), []byte("package allowed\n"), 0o644)
	os.MkdirAll(filepath.Join(srcDir, "blocked"), 0o755)
	os.WriteFile(filepath.Join(srcDir, "blocked", "secret.txt"), []byte("secret\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "add allowed and blocked files")

	// Transplant with PathFilter.
	err := TransplantWithOpts(TransplantOpts{
		SrcRepo:    srcDir,
		DstRepo:    dstDir,
		PathFilter: []string{"allowed"},
	})
	if err != nil {
		t.Fatalf("TransplantWithOpts: %v", err)
	}

	// Verify allowed/file.go exists in dst.
	if _, err := os.Stat(filepath.Join(dstDir, "allowed", "file.go")); err != nil {
		t.Error("allowed/file.go should exist in dst after transplant")
	}

	// Verify blocked/secret.txt does NOT exist in dst.
	if _, err := os.Stat(filepath.Join(dstDir, "blocked", "secret.txt")); !os.IsNotExist(err) {
		t.Error("blocked/secret.txt should NOT exist in dst after filtered transplant")
	}
}

func TestTransplantSquash(t *testing.T) {
	root := t.TempDir()

	// Create two repos: src and dst.
	srcDir := filepath.Join(root, "src")
	dstDir := filepath.Join(root, "dst")

	// Init src.
	os.MkdirAll(srcDir, 0o755)
	gitInDir(srcDir, "init")
	configGit(t, srcDir)
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("# src\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "initial")

	// Set marker at current HEAD.
	if err := SetTransplantMarker(srcDir); err != nil {
		t.Fatalf("SetTransplantMarker: %v", err)
	}

	// Init dst as a copy.
	copyRepo(srcDir, dstDir)
	configGit(t, dstDir)

	// Count dst commits before transplant.
	dstLogBefore, _ := gitOutputInDir(dstDir, "rev-list", "--count", "HEAD")

	// In src: make 3 separate commits.
	os.WriteFile(filepath.Join(srcDir, "file1.go"), []byte("package one\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "add file1")

	os.WriteFile(filepath.Join(srcDir, "file2.go"), []byte("package two\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "add file2")

	os.WriteFile(filepath.Join(srcDir, "file3.go"), []byte("package three\n"), 0o644)
	gitInDir(srcDir, "add", "-A")
	gitInDir(srcDir, "commit", "-m", "add file3")

	// Transplant with Squash.
	err := TransplantWithOpts(TransplantOpts{
		SrcRepo: srcDir,
		DstRepo: dstDir,
		Squash:  true,
		Message: "Squashed",
	})
	if err != nil {
		t.Fatalf("TransplantWithOpts: %v", err)
	}

	// Verify all 3 files exist in dst.
	for _, f := range []string{"file1.go", "file2.go", "file3.go"} {
		if _, err := os.Stat(filepath.Join(dstDir, f)); err != nil {
			t.Errorf("%s should exist in dst after squash transplant", f)
		}
	}

	// Verify dst has exactly 1 new commit (not 3).
	dstLogAfter, _ := gitOutputInDir(dstDir, "rev-list", "--count", "HEAD")
	// Parse counts.
	var before, after int
	fmt.Sscanf(dstLogBefore, "%d", &before)
	fmt.Sscanf(dstLogAfter, "%d", &after)
	if after-before != 1 {
		t.Errorf("expected 1 new commit in dst, got %d (before=%d, after=%d)", after-before, before, after)
	}

	// Verify commit message.
	msg, _ := gitOutputInDir(dstDir, "log", "-1", "--format=%s")
	if msg != "Squashed" {
		t.Errorf("expected commit message %q, got %q", "Squashed", msg)
	}
}

func TestSync(t *testing.T) {
	root := t.TempDir()

	// Create target repo.
	targetDir := filepath.Join(root, "target")
	os.MkdirAll(targetDir, 0o755)
	gitInDir(targetDir, "init")
	configGit(t, targetDir)
	os.WriteFile(filepath.Join(targetDir, "README.md"), []byte("# target\n"), 0o644)
	gitInDir(targetDir, "add", "-A")
	gitInDir(targetDir, "commit", "-m", "initial")

	// Create agent repo as copy, with a preserved dir.
	agentDir := filepath.Join(root, "agent")
	copyRepo(targetDir, agentDir)
	configGit(t, agentDir)
	gitInDir(agentDir, "checkout", "-b", "agent-branch")
	preserveDir := filepath.Join(agentDir, "my-tools")
	os.MkdirAll(preserveDir, 0o755)
	os.WriteFile(filepath.Join(preserveDir, "tool.sh"), []byte("#!/bin/sh\n"), 0o644)
	gitInDir(agentDir, "add", "-A")
	gitInDir(agentDir, "commit", "-m", "add tools")

	// Add new file to target.
	os.WriteFile(filepath.Join(targetDir, "newfile.txt"), []byte("hello\n"), 0o644)
	gitInDir(targetDir, "add", "-A")
	gitInDir(targetDir, "commit", "-m", "add newfile")

	// Sync.
	if err := Sync(targetDir, agentDir, []string{"my-tools"}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	// Verify new file appears in agent.
	if _, err := os.Stat(filepath.Join(agentDir, "newfile.txt")); err != nil {
		t.Error("newfile.txt should exist in agent after sync")
	}

	// Verify preserved dir still exists.
	if _, err := os.Stat(filepath.Join(agentDir, "my-tools", "tool.sh")); err != nil {
		t.Error("preserved dir my-tools/tool.sh should still exist")
	}

	// Verify branch is still correct.
	branch, _ := gitOutputInDir(agentDir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "agent-branch" {
		t.Errorf("expected branch agent-branch, got %s", branch)
	}
}
