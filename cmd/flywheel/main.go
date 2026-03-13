package main

import (
	"context"
	"encoding/json"
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
	Timeout         time.Duration `short:"t" long:"timeout" default:"0" description:"Max wall-clock time per agent run (0 = unlimited)"`
	SkipPermissions bool          `long:"skip-permissions" description:"Pass --permission-mode bypassPermissions to claude (required for lights-out)"`
	Model           string        `long:"model" description:"Model to use for claude sessions (e.g. sonnet, opus)"`
	LogFile         string        `short:"l" long:"log-file" default:"flywheel-runs.jsonl" description:"JSONL file for timing logs"`
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

// RunLog is the top-level entry written to the JSONL log file after each flywheel execution.
type RunLog struct {
	StartTime  time.Time   `json:"start_time"`
	EndTime    time.Time   `json:"end_time"`
	Duration   string      `json:"duration"`
	DurationS  float64     `json:"duration_seconds"`
	Model      string      `json:"model,omitempty"`
	MaxRounds  int         `json:"max_rounds"`
	Outcome    string      `json:"outcome"` // "pass", "fail_max_rounds", "error"
	Rounds     []RoundLog  `json:"rounds"`
}

// RoundLog records timing for a single builder→UAT round.
type RoundLog struct {
	Round         int       `json:"round"`
	BuilderStart  time.Time `json:"builder_start"`
	BuilderEnd    time.Time `json:"builder_end"`
	BuilderDur    string    `json:"builder_duration"`
	BuilderDurS   float64   `json:"builder_duration_seconds"`
	BuilderPrompt string    `json:"builder_prompt"`
	UATStart      time.Time `json:"uat_start"`
	UATEnd        time.Time `json:"uat_end"`
	UATDur        string    `json:"uat_duration"`
	UATDurS       float64   `json:"uat_duration_seconds"`
	Verdict       string    `json:"verdict"`
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

	opts.BuilderDir = mustAbs(opts.BuilderDir)
	opts.UATDir = mustAbs(opts.UATDir)
	opts.PromptsDir = mustAbs(opts.PromptsDir)

	runStart := time.Now()
	runLog := RunLog{
		StartTime: runStart,
		Model:     opts.Model,
		MaxRounds: opts.MaxRounds,
	}

	for round := 1; round <= opts.MaxRounds; round++ {
		fmt.Printf("\n========== Round %d/%d ==========\n\n", round, opts.MaxRounds)
		roundLog := RoundLog{Round: round}

		// --- Builder phase ---
		promptFile := "builder-init.md"
		if round > 1 {
			promptFile = "builder-fix.md"
		}
		roundLog.BuilderPrompt = promptFile

		prompt, err := loadPrompt(opts.PromptsDir, promptFile, opts)
		if err != nil {
			die("reading prompt: %v", err)
		}

		builderDone := filepath.Join(opts.BuilderDir, "done")
		os.Remove(builderDone)

		fmt.Printf("--- Builder [%s] ---\n", promptFile)
		roundLog.BuilderStart = time.Now()
		if err := runClaude(opts.BuilderDir, prompt+builderSuffix, builderDone, opts); err != nil {
			fmt.Fprintf(os.Stderr, "builder exited: %v\n", err)
		}
		roundLog.BuilderEnd = time.Now()
		roundLog.BuilderDur = roundLog.BuilderEnd.Sub(roundLog.BuilderStart).Round(time.Second).String()
		roundLog.BuilderDurS = roundLog.BuilderEnd.Sub(roundLog.BuilderStart).Seconds()

		fmt.Printf("\n[flywheel] Builder phase: %s\n", roundLog.BuilderDur)

		if _, err := os.Stat(builderDone); os.IsNotExist(err) {
			runLog.Rounds = append(runLog.Rounds, roundLog)
			runLog.Outcome = "error"
			runLog.EndTime = time.Now()
			runLog.Duration = runLog.EndTime.Sub(runStart).Round(time.Second).String()
			runLog.DurationS = runLog.EndTime.Sub(runStart).Seconds()
			writeRunLog(opts.LogFile, runLog)
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
		roundLog.UATStart = time.Now()
		if err := runClaude(opts.UATDir, prompt+uatSuffix, uatDone, opts); err != nil {
			fmt.Fprintf(os.Stderr, "UAT exited: %v\n", err)
		}
		roundLog.UATEnd = time.Now()
		roundLog.UATDur = roundLog.UATEnd.Sub(roundLog.UATStart).Round(time.Second).String()
		roundLog.UATDurS = roundLog.UATEnd.Sub(roundLog.UATStart).Seconds()

		fmt.Printf("\n[flywheel] UAT phase: %s\n", roundLog.UATDur)

		if _, err := os.Stat(uatDone); os.IsNotExist(err) {
			runLog.Rounds = append(runLog.Rounds, roundLog)
			runLog.Outcome = "error"
			runLog.EndTime = time.Now()
			runLog.Duration = runLog.EndTime.Sub(runStart).Round(time.Second).String()
			runLog.DurationS = runLog.EndTime.Sub(runStart).Seconds()
			writeRunLog(opts.LogFile, runLog)
			die("UAT did not create 'done' file — aborting")
		}

		raw, err := os.ReadFile(uatDone)
		if err != nil {
			die("reading UAT done file: %v", err)
		}

		verdict := strings.TrimSpace(string(raw))
		roundLog.Verdict = verdict
		runLog.Rounds = append(runLog.Rounds, roundLog)

		roundDur := roundLog.UATEnd.Sub(roundLog.BuilderStart).Round(time.Second)
		totalDur := time.Since(runStart).Round(time.Second)
		fmt.Printf("\n========== UAT verdict: %s (round: %s, total: %s) ==========\n\n", verdict, roundDur, totalDur)

		if strings.EqualFold(verdict, "PASS") {
			runLog.Outcome = "pass"
			runLog.EndTime = time.Now()
			runLog.Duration = runLog.EndTime.Sub(runStart).Round(time.Second).String()
			runLog.DurationS = runLog.EndTime.Sub(runStart).Seconds()
			writeRunLog(opts.LogFile, runLog)
			fmt.Printf("SUCCESS — UAT passed! Total time: %s\n", runLog.Duration)
			return
		}

		if round == opts.MaxRounds {
			runLog.Outcome = "fail_max_rounds"
			runLog.EndTime = time.Now()
			runLog.Duration = runLog.EndTime.Sub(runStart).Round(time.Second).String()
			runLog.DurationS = runLog.EndTime.Sub(runStart).Seconds()
			writeRunLog(opts.LogFile, runLog)
			die("max rounds (%d) reached without UAT pass — total time: %s", opts.MaxRounds, runLog.Duration)
		}

		fmt.Printf("UAT failed — starting round %d...\n", round+1)
	}
}

func writeRunLog(path string, entry RunLog) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write log to %s: %v\n", path, err)
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "")
	if err := enc.Encode(entry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not encode log entry: %v\n", path)
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
	var ctx context.Context
	var cancel context.CancelFunc
	if opts.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), opts.Timeout)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
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

func watchForDone(ctx context.Context, cancel context.CancelFunc, doneFile string) {
	dir := filepath.Dir(doneFile)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	doneDetected := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !doneDetected {
				if _, err := os.Stat(doneFile); err == nil {
					fmt.Printf("\n[flywheel] done file detected: %s\n", doneFile)
					doneDetected = true
				}
				continue
			}
			// Done file exists — wait for git to be clean and pushed
			if !gitWorktreeClean(dir) {
				fmt.Println("[flywheel] waiting for clean git worktree...")
				continue
			}
			if !gitRemoteUpToDate(dir) {
				fmt.Println("[flywheel] waiting for remote to be up to date...")
				continue
			}
			fmt.Println("[flywheel] git clean and pushed — terminating session")
			cancel()
			return
		}
	}
}

// gitWorktreeClean returns true if there are no unstaged or uncommitted changes to tracked files.
func gitWorktreeClean(dir string) bool {
	// Check for unstaged changes
	diff := exec.Command("git", "diff", "--quiet")
	diff.Dir = dir
	if err := diff.Run(); err != nil {
		return false
	}
	// Check for staged but uncommitted changes
	diffCached := exec.Command("git", "diff", "--cached", "--quiet")
	diffCached.Dir = dir
	return diffCached.Run() == nil
}

// gitRemoteUpToDate returns true if the local branch has no unpushed commits.
func gitRemoteUpToDate(dir string) bool {
	// Fetch latest remote state quietly
	fetch := exec.Command("git", "fetch", "--quiet")
	fetch.Dir = dir
	fetch.Run() // best-effort, don't fail if offline

	cmd := exec.Command("git", "rev-list", "@{u}..HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// No upstream configured or other error — treat as not up to date
		return false
	}
	return strings.TrimSpace(string(out)) == ""
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
