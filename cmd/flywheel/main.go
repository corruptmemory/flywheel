package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	flags "github.com/jessevdk/go-flags"
)

type Options struct {
	BuilderDir      string        `short:"b" long:"builder-dir" required:"true" description:"Builder agent workspace directory"`
	UATDir          string        `short:"u" long:"uat-dir" required:"true" description:"UAT agent workspace directory"`
	PromptsDir      string        `short:"p" long:"prompts-dir" default:"./prompts" description:"Directory containing prompt templates"`
	MaxRounds       int           `short:"m" long:"max-rounds" default:"5" description:"Maximum builder→UAT round trips"`
	Timeout         time.Duration `short:"t" long:"timeout" default:"3h" description:"Max wall-clock time per agent run"`
	SkipPermissions bool          `long:"skip-permissions" description:"Pass --permission-mode bypassPermissions to claude (required for lights-out)"`
	Model           string        `long:"model" description:"Model to use for claude sessions (e.g. sonnet, opus)"`
}

const builderSuffix = `

## Completion Protocol

When you have completed ALL work above — including self-UAT, code review, and security review:

1. Commit and push all changes
2. Run: touch done

The session will end automatically once the done file is detected.
`

const uatSuffix = `

## Completion Protocol

When testing is complete:

- If ALL checklist items pass, run: echo "PASS" > done
- If ANY checklist item fails, run: echo "FAIL" > done

The session will end automatically once the done file is detected.
`

func main() {
	var opts Options
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		if flagsErr, ok := err.(*flags.Error); ok && flagsErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	opts.BuilderDir = mustAbs(opts.BuilderDir)
	opts.UATDir = mustAbs(opts.UATDir)
	opts.PromptsDir = mustAbs(opts.PromptsDir)

	for round := 1; round <= opts.MaxRounds; round++ {
		fmt.Printf("\n========== Round %d/%d ==========\n\n", round, opts.MaxRounds)

		// --- Builder phase ---
		promptFile := "builder-init.md"
		if round > 1 {
			promptFile = "builder-fix.md"
		}

		prompt, err := loadPrompt(opts.PromptsDir, promptFile, opts)
		if err != nil {
			die("reading prompt: %v", err)
		}

		builderDone := filepath.Join(opts.BuilderDir, "done")
		os.Remove(builderDone)

		fmt.Printf("--- Builder [%s] ---\n", promptFile)
		if err := runClaude(opts.BuilderDir, prompt+builderSuffix, builderDone, opts); err != nil {
			fmt.Fprintf(os.Stderr, "builder exited: %v\n", err)
		}

		if _, err := os.Stat(builderDone); os.IsNotExist(err) {
			die("builder did not create 'done' file — aborting")
		}

		// --- UAT phase ---
		prompt, err = loadPrompt(opts.PromptsDir, "uat.md", opts)
		if err != nil {
			die("reading prompt: %v", err)
		}

		uatDone := filepath.Join(opts.UATDir, "done")
		os.Remove(uatDone)

		fmt.Println("--- UAT ---")
		if err := runClaude(opts.UATDir, prompt+uatSuffix, uatDone, opts); err != nil {
			fmt.Fprintf(os.Stderr, "UAT exited: %v\n", err)
		}

		if _, err := os.Stat(uatDone); os.IsNotExist(err) {
			die("UAT did not create 'done' file — aborting")
		}

		raw, err := os.ReadFile(uatDone)
		if err != nil {
			die("reading UAT done file: %v", err)
		}

		verdict := strings.TrimSpace(string(raw))
		fmt.Printf("\n========== UAT verdict: %s ==========\n\n", verdict)

		if strings.EqualFold(verdict, "PASS") {
			fmt.Println("SUCCESS — UAT passed!")
			return
		}

		if round == opts.MaxRounds {
			die("max rounds (%d) reached without UAT pass", opts.MaxRounds)
		}

		fmt.Printf("UAT failed — starting round %d...\n", round+1)
	}
}

func loadPrompt(dir, filename string, opts Options) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, filename))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filename, err)
	}
	s := string(data)
	s = strings.ReplaceAll(s, "{{BUILDER_DIR}}", opts.BuilderDir)
	s = strings.ReplaceAll(s, "{{UAT_DIR}}", opts.UATDir)
	return s, nil
}

func runClaude(dir, prompt, doneFile string, opts Options) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	// Watch for the done file and cancel context when it appears
	go watchForDone(ctx, cancel, doneFile)

	args := []string{}
	if opts.SkipPermissions {
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = dir
	// Clear CLAUDECODE env var so child claude sessions don't think they're nested
	cmd.Env = filterEnv(os.Environ(), "CLAUDECODE")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func watchForDone(ctx context.Context, cancel context.CancelFunc, path string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				fmt.Printf("\n[flywheel] done file detected: %s\n", path)
				cancel()
				return
			}
		}
	}
}

func filterEnv(env []string, excludePrefix string) []string {
	var filtered []string
	for _, e := range env {
		if !strings.HasPrefix(e, excludePrefix+"=") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func mustAbs(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		die("resolving path %q: %v", path, err)
	}
	return abs
}

func die(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
