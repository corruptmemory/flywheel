package main

import (
	"bufio"
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
	Retries         int           `long:"retries" default:"2" description:"Retries per phase if claude crashes without done file"`
	Timeout         time.Duration `short:"t" long:"timeout" default:"0" description:"Max wall-clock time per agent run (0 = unlimited)"`
	SkipPermissions bool          `long:"skip-permissions" description:"Pass --permission-mode bypassPermissions to claude (required for lights-out)"`
	Model           string        `long:"model" description:"Model to use for claude sessions (e.g. sonnet, opus)"`
	LogFile         string        `short:"l" long:"log-file" default:"flywheel-runs.jsonl" description:"JSONL file for timing logs"`
	Start           string        `long:"start" choice:"build" choice:"fix" choice:"uat" default:"build" description:"Entry point: build (fresh), fix (builder-fix), uat (skip to UAT)"`
	Resume          bool          `long:"resume" description:"Read log file to determine start phase and round automatically"`
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
	StartTime time.Time  `json:"start_time"`
	EndTime   time.Time  `json:"end_time"`
	Duration  string     `json:"duration"`
	DurationS float64    `json:"duration_seconds"`
	Model     string     `json:"model,omitempty"`
	MaxRounds int        `json:"max_rounds"`
	Outcome   string     `json:"outcome"` // "pass", "fail_max_rounds", "error"
	Resumed   string     `json:"resumed,omitempty"` // "", "fix", "uat"
	Rounds    []RoundLog `json:"rounds"`
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
	LastPhase     string    `json:"last_phase"` // "builder" or "uat"
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

	// Determine starting round and phase
	startRound := 1
	startPhase := opts.Start // "build", "fix", or "uat"

	if opts.Resume {
		lastRun := readLastRunLog(opts.LogFile)
		if lastRun == nil {
			die("--resume: no previous run found in %s", opts.LogFile)
		}
		if len(lastRun.Rounds) == 0 {
			die("--resume: last run in %s has no rounds", opts.LogFile)
		}
		lastRound := lastRun.Rounds[len(lastRun.Rounds)-1]

		switch lastRound.LastPhase {
		case "builder":
			// Builder finished, UAT never ran
			startPhase = "uat"
			startRound = lastRound.Round
		case "uat":
			if strings.EqualFold(lastRound.Verdict, "FAIL") {
				// UAT failed — next step is builder-fix on next round
				startPhase = "fix"
				startRound = lastRound.Round + 1
			} else {
				// UAT passed — nothing to resume
				fmt.Println("[flywheel] Last run passed — nothing to resume.")
				return
			}
		default:
			die("--resume: last round has unknown phase %q", lastRound.LastPhase)
		}

		fmt.Printf("[flywheel] Resuming at %s, round %d (from %s)\n", startPhase, startRound, opts.LogFile)
	} else if startPhase != "build" {
		// --start fix or --start uat without --resume: stateless, start at round 1
		fmt.Printf("[flywheel] Starting at %s phase, round %d\n", startPhase, startRound)
	}

	runStart := time.Now()
	runLog := RunLog{
		StartTime: runStart,
		Model:     opts.Model,
		MaxRounds: opts.MaxRounds,
	}
	if startPhase != "build" {
		runLog.Resumed = startPhase
	}

	for round := startRound; round <= opts.MaxRounds; round++ {
		fmt.Printf("\n========== Round %d/%d ==========\n\n", round, opts.MaxRounds)
		roundLog := RoundLog{Round: round}

		// --- Builder phase ---
		skipBuilder := (round == startRound && startPhase == "uat")

		if !skipBuilder {
			promptFile := "builder-init.md"
			if round > 1 || startPhase == "fix" {
				promptFile = "builder-fix.md"
			}
			roundLog.BuilderPrompt = promptFile

			prompt, err := loadPrompt(opts.PromptsDir, promptFile, opts)
			if err != nil {
				die("reading prompt: %v", err)
			}

			builderDone := filepath.Join(opts.BuilderDir, "done")
			roundLog.BuilderStart = time.Now()

			if !runPhaseWithRetries("Builder", promptFile, opts.BuilderDir, prompt+builderSuffix, builderDone, opts) {
				roundLog.BuilderEnd = time.Now()
				roundLog.BuilderDur = roundLog.BuilderEnd.Sub(roundLog.BuilderStart).Round(time.Second).String()
				roundLog.BuilderDurS = roundLog.BuilderEnd.Sub(roundLog.BuilderStart).Seconds()
				roundLog.LastPhase = "builder"
				runLog.Rounds = append(runLog.Rounds, roundLog)
				finishRunLog(&runLog, runStart, "error", opts.LogFile)
				die("builder did not create 'done' file after %d attempts — aborting", opts.Retries+1)
			}

			roundLog.BuilderEnd = time.Now()
			roundLog.BuilderDur = roundLog.BuilderEnd.Sub(roundLog.BuilderStart).Round(time.Second).String()
			roundLog.BuilderDurS = roundLog.BuilderEnd.Sub(roundLog.BuilderStart).Seconds()
			roundLog.LastPhase = "builder"

			fmt.Printf("\n[flywheel] Builder phase: %s\n", roundLog.BuilderDur)
		} else {
			fmt.Println("--- Builder [skipped — resuming at UAT] ---")
			roundLog.BuilderPrompt = "skipped"
		}

		// --- UAT phase ---
		prompt, err := loadPrompt(opts.PromptsDir, "uat.md", opts)
		if err != nil {
			die("reading prompt: %v", err)
		}

		uatDone := filepath.Join(opts.UATDir, "done")
		roundLog.UATStart = time.Now()

		if !runPhaseWithRetries("UAT", "uat.md", opts.UATDir, prompt+uatSuffix, uatDone, opts) {
			roundLog.UATEnd = time.Now()
			roundLog.UATDur = roundLog.UATEnd.Sub(roundLog.UATStart).Round(time.Second).String()
			roundLog.UATDurS = roundLog.UATEnd.Sub(roundLog.UATStart).Seconds()
			roundLog.LastPhase = "uat"
			runLog.Rounds = append(runLog.Rounds, roundLog)
			finishRunLog(&runLog, runStart, "error", opts.LogFile)
			die("UAT did not create 'done' file after %d attempts — aborting", opts.Retries+1)
		}

		roundLog.UATEnd = time.Now()
		roundLog.UATDur = roundLog.UATEnd.Sub(roundLog.UATStart).Round(time.Second).String()
		roundLog.UATDurS = roundLog.UATEnd.Sub(roundLog.UATStart).Seconds()
		roundLog.LastPhase = "uat"

		fmt.Printf("\n[flywheel] UAT phase: %s\n", roundLog.UATDur)

		raw, err := os.ReadFile(uatDone)
		if err != nil {
			die("reading UAT done file: %v", err)
		}

		verdict := strings.TrimSpace(string(raw))
		roundLog.Verdict = verdict
		runLog.Rounds = append(runLog.Rounds, roundLog)

		roundDur := roundLog.UATEnd.Sub(roundLog.BuilderStart).Round(time.Second)
		if skipBuilder {
			roundDur = roundLog.UATEnd.Sub(roundLog.UATStart).Round(time.Second)
		}
		totalDur := time.Since(runStart).Round(time.Second)
		fmt.Printf("\n========== UAT verdict: %s (round: %s, total: %s) ==========\n\n", verdict, roundDur, totalDur)

		if strings.EqualFold(verdict, "PASS") {
			finishRunLog(&runLog, runStart, "pass", opts.LogFile)
			fmt.Printf("SUCCESS — UAT passed! Total time: %s\n", runLog.Duration)
			return
		}

		if round == opts.MaxRounds {
			finishRunLog(&runLog, runStart, "fail_max_rounds", opts.LogFile)
			die("max rounds (%d) reached without UAT pass — total time: %s", opts.MaxRounds, runLog.Duration)
		}

		fmt.Printf("UAT failed — starting round %d...\n", round+1)

		// After first iteration, reset startPhase so subsequent rounds run normally
		startPhase = ""
	}
}

func finishRunLog(runLog *RunLog, runStart time.Time, outcome, logFile string) {
	runLog.Outcome = outcome
	runLog.EndTime = time.Now()
	runLog.Duration = runLog.EndTime.Sub(runStart).Round(time.Second).String()
	runLog.DurationS = runLog.EndTime.Sub(runStart).Seconds()
	writeRunLog(logFile, *runLog)
}

func readLastRunLog(path string) *RunLog {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var last string
	scanner := bufio.NewScanner(f)
	// Allow up to 1MB lines (run logs can be large with many rounds)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			last = line
		}
	}
	if last == "" {
		return nil
	}

	var entry RunLog
	if err := json.Unmarshal([]byte(last), &entry); err != nil {
		return nil
	}
	return &entry
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

// runPhaseWithRetries runs a claude session, retrying if it crashes without producing a done file.
// Returns true if the done file was created, false if all attempts exhausted.
func runPhaseWithRetries(phase, promptFile, dir, prompt, doneFile string, opts Options) bool {
	for attempt := 0; attempt <= opts.Retries; attempt++ {
		os.Remove(doneFile)

		if attempt > 0 {
			fmt.Printf("\n[flywheel] %s crashed without done file — retry %d/%d\n", phase, attempt, opts.Retries)
		}

		fmt.Printf("--- %s [%s] ---\n", phase, promptFile)
		if err := runClaude(dir, prompt, doneFile, opts); err != nil {
			fmt.Fprintf(os.Stderr, "%s exited: %v\n", phase, err)
		}

		if _, err := os.Stat(doneFile); err == nil {
			return true
		}
	}
	return false
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
