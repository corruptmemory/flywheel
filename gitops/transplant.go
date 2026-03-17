package gitops

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const markerFile = "lc-last-transplant"

// TransplantOpts controls how commits are transplanted between repos.
type TransplantOpts struct {
	SrcRepo    string
	DstRepo    string
	SinceRef   string
	PathFilter []string // if set, only include changes under these paths
	Squash     bool     // squash all commits into one
	Message    string   // squash commit message (required if Squash is true)
}

// Transplant applies new commits from srcRepo onto dstRepo using format-patch/am.
// sinceRef is the commit after which to generate patches. If empty, reads the
// lc-last-transplant marker from srcRepo.
func Transplant(srcRepo, dstRepo, sinceRef string) error {
	return TransplantWithOpts(TransplantOpts{
		SrcRepo:  srcRepo,
		DstRepo:  dstRepo,
		SinceRef: sinceRef,
	})
}

// TransplantWithOpts transplants commits with optional path filtering and squashing.
func TransplantWithOpts(opts TransplantOpts) error {
	sinceRef := opts.SinceRef
	if sinceRef == "" {
		marker, err := readMarker(opts.SrcRepo)
		if err != nil {
			return fmt.Errorf("read transplant marker: %w", err)
		}
		sinceRef = marker
	}

	// Check if there are new commits.
	logOut, err := gitOutputInDir(opts.SrcRepo, "log", "--oneline", sinceRef+"..HEAD")
	if err != nil {
		return fmt.Errorf("check for new commits: %w", err)
	}
	if logOut == "" {
		return nil
	}

	if opts.Squash {
		return transplantSquash(opts.SrcRepo, opts.DstRepo, sinceRef, opts.PathFilter, opts.Message)
	}
	if len(opts.PathFilter) > 0 {
		return transplantFiltered(opts.SrcRepo, opts.DstRepo, sinceRef, opts.PathFilter)
	}
	return transplantDirect(opts.SrcRepo, opts.DstRepo, sinceRef)
}

// transplantSquash generates a unified diff and applies it as a single commit.
func transplantSquash(srcRepo, dstRepo, sinceRef string, pathFilter []string, message string) error {
	// Build diff args.
	args := []string{"diff", sinceRef + "..HEAD"}
	if len(pathFilter) > 0 {
		args = append(args, "--")
		args = append(args, pathFilter...)
	}

	diffOut, err := gitOutputInDir(srcRepo, args...)
	if err != nil {
		return fmt.Errorf("git diff: %w", err)
	}
	if diffOut == "" {
		return nil
	}

	// Write diff to temp file.
	tmpFile, err := os.CreateTemp("", "lc-squash-*.patch")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(diffOut + "\n"); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()

	// Apply patch to dstRepo.
	if err := gitInDir(dstRepo, "apply", tmpFile.Name()); err != nil {
		return fmt.Errorf("git apply: %w", err)
	}

	// Stage and commit.
	if err := gitInDir(dstRepo, "add", "-A"); err != nil {
		return fmt.Errorf("git add: %w", err)
	}
	if err := gitInDir(dstRepo, "commit", "-m", message); err != nil {
		return fmt.Errorf("git commit: %w", err)
	}

	return SetTransplantMarker(srcRepo)
}

// transplantFiltered generates patches restricted to certain paths and applies via git am.
func transplantFiltered(srcRepo, dstRepo, sinceRef string, pathFilter []string) error {
	tmpDir, err := os.MkdirTemp("", "lc-patches-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	args := []string{"format-patch", "-o", tmpDir, sinceRef + "..HEAD", "--"}
	args = append(args, pathFilter...)

	_, err = gitOutputInDir(srcRepo, args...)
	if err != nil {
		return fmt.Errorf("format-patch: %w", err)
	}

	patches, err := listPatches(tmpDir)
	if err != nil {
		return err
	}

	for _, p := range patches {
		if err := gitInDir(dstRepo, "am", p); err != nil {
			_ = gitInDir(dstRepo, "am", "--abort")
			return fmt.Errorf("git am failed on %s: %w", filepath.Base(p), err)
		}
	}

	return SetTransplantMarker(srcRepo)
}

// transplantDirect does a straight format-patch/am with no filtering.
func transplantDirect(srcRepo, dstRepo, sinceRef string) error {
	tmpDir, err := os.MkdirTemp("", "lc-patches-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	_, err = gitOutputInDir(srcRepo, "format-patch", "-o", tmpDir, sinceRef+"..HEAD")
	if err != nil {
		return fmt.Errorf("format-patch: %w", err)
	}

	patches, err := listPatches(tmpDir)
	if err != nil {
		return err
	}

	for _, p := range patches {
		if err := gitInDir(dstRepo, "am", p); err != nil {
			_ = gitInDir(dstRepo, "am", "--abort")
			return fmt.Errorf("git am failed on %s: %w", filepath.Base(p), err)
		}
	}

	return SetTransplantMarker(srcRepo)
}

// listPatches returns sorted patch file paths from a directory.
func listPatches(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var patches []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".patch") {
			patches = append(patches, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(patches)
	return patches, nil
}

// SetTransplantMarker records current HEAD as the "last transplanted" point.
func SetTransplantMarker(repoDir string) error {
	head, err := gitOutputInDir(repoDir, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	markerPath := filepath.Join(repoDir, ".git", markerFile)
	return os.WriteFile(markerPath, []byte(head+"\n"), 0o644)
}

// readMarker reads the transplant marker from a repo's .git directory.
func readMarker(repoDir string) (string, error) {
	markerPath := filepath.Join(repoDir, ".git", markerFile)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
