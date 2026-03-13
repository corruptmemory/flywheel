package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	flags "github.com/jessevdk/go-flags"
)

type Options struct {
	BuilderDir      string        `short:"b" long:"builder-dir" required:"true" description:"Builder agent workspace directory"`
	UATDir          string        `short:"u" long:"uat-dir" required:"true" description:"UAT agent workspace directory"`
	PromptsDir      string        `short:"p" long:"prompts-dir" default:"./prompts" description:"Directory containing prompt templates"`
	MaxRounds       int           `short:"m" long:"max-rounds" default:"20" description:"Maximum spec-level iterations"`
	MaxGateRetries  int           `long:"max-gate-retries" default:"3" description:"Max times builder can fail same gate per spec (0=unlimited)"`
	Retries         int           `long:"retries" default:"2" description:"Retries per phase if claude crashes without done file"`
	Timeout         time.Duration `short:"t" long:"timeout" default:"0" description:"Max wall-clock time per agent run (0 = unlimited)"`
	SkipPermissions bool          `long:"skip-permissions" description:"Pass --permission-mode bypassPermissions to claude (required for lights-out)"`
	Model           string        `long:"model" description:"Model to use for claude sessions (e.g. sonnet, opus)"`
	LogFile         string        `short:"l" long:"log-file" default:"flywheel-runs.jsonl" description:"JSONL file for timing logs"`
}

// Phase constants

type Phase string

const (
	PhaseBuild          Phase = "build"
	PhaseCodeReview     Phase = "code-review"
	PhaseSecurityReview Phase = "security-review"
	PhaseDevTest        Phase = "dev-test"
	PhaseUAT            Phase = "uat"
)

type Scenario string

const (
	ScenarioInit              Scenario = "init"
	ScenarioNextSpec          Scenario = "next-spec"
	ScenarioFixCodeReview     Scenario = "fix-code-review"
	ScenarioFixSecurityReview Scenario = "fix-security-review"
	ScenarioFixDevTest        Scenario = "fix-dev-test"
	ScenarioFixUAT            Scenario = "fix-uat"
)

const builderSuffix = `

## Completion Protocol

When you have completed ALL work described above:

1. Commit and push all changes
2. Write the builder_done file (see format in builder instructions above)

The session will end automatically once the builder_done file is detected.
`

const reviewSuffix = `

## Completion Protocol

When your review is complete:

1. Write all findings to the appropriate feedback directory
2. Write your done file with PASS or FAIL on the first line

The session will end automatically once the done file is detected.
`

const devTestSuffix = `

## Completion Protocol

When testing is complete:

1. Write all results to dev-test-results/
2. Write dev_test_done with one of: PASS, FAIL, or UAT READY on the first line

The session will end automatically once the done file is detected.
`

const uatSuffix = `

## Completion Protocol

When testing is complete:

- If ALL checklist items pass, run: echo "PASS" > uat_done
- If ANY checklist item fails, run: echo "FAIL" > uat_done

The session will end automatically once the done file is detected.
`

// RunLog is the top-level entry written to the JSONL log file after each flywheel execution.
type RunLog struct {
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time,omitzero"`
	Duration  string    `json:"duration,omitempty"`
	DurationS float64   `json:"duration_seconds,omitempty"`
	Model     string    `json:"model,omitempty"`
	Outcome   string    `json:"outcome"` // "pass", "fail_max_rounds", "fail_gate_retries", "error"
	Specs     []SpecLog `json:"specs"`
	UAT       *UATLog   `json:"uat,omitempty"`
}

type SpecLog struct {
	Spec   string     `json:"spec"`
	Phases []PhaseLog `json:"phases"`
}

type UATLog struct {
	Phases []PhaseLog `json:"phases"`
}

type PhaseLog struct {
	Phase     string    `json:"phase"`
	Prompt    string    `json:"prompt,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	DurationS float64   `json:"duration_seconds"`
	Verdict   string    `json:"verdict,omitempty"`
}

// Done file types and state derivation

type doneFiles struct {
	Builder        string
	CodeReview     string
	SecurityReview string
	DevTest        string
	UAT            string
}

func newDoneFiles(builderDir, uatDir string) doneFiles {
	return doneFiles{
		Builder:        filepath.Join(builderDir, "builder_done"),
		CodeReview:     filepath.Join(builderDir, "code_review_done"),
		SecurityReview: filepath.Join(builderDir, "security_review_done"),
		DevTest:        filepath.Join(builderDir, "dev_test_done"),
		UAT:            filepath.Join(uatDir, "uat_done"),
	}
}

func readDoneVerdict(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)[0]
	return strings.TrimSpace(line)
}

type phaseAction struct {
	Phase    Phase
	Scenario Scenario
	DoneFile string
	WorkDir  string
}

func determineNextPhase(df doneFiles, builderDir, uatDir string) phaseAction {
	if v := readDoneVerdict(df.UAT); v != "" {
		if strings.EqualFold(v, "PASS") {
			return phaseAction{Phase: "done"}
		}
		return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixUAT, DoneFile: df.Builder, WorkDir: builderDir}
	}

	if v := readDoneVerdict(df.DevTest); v != "" {
		switch {
		case strings.EqualFold(v, "UAT READY"):
			return phaseAction{Phase: PhaseUAT, DoneFile: df.UAT, WorkDir: uatDir}
		case strings.EqualFold(v, "PASS"):
			return phaseAction{Phase: PhaseBuild, Scenario: ScenarioNextSpec, DoneFile: df.Builder, WorkDir: builderDir}
		default:
			return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixDevTest, DoneFile: df.Builder, WorkDir: builderDir}
		}
	}

	if v := readDoneVerdict(df.SecurityReview); v != "" {
		if strings.EqualFold(v, "PASS") {
			return phaseAction{Phase: PhaseDevTest, DoneFile: df.DevTest, WorkDir: builderDir}
		}
		return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixSecurityReview, DoneFile: df.Builder, WorkDir: builderDir}
	}

	if v := readDoneVerdict(df.CodeReview); v != "" {
		if strings.EqualFold(v, "PASS") {
			return phaseAction{Phase: PhaseSecurityReview, DoneFile: df.SecurityReview, WorkDir: builderDir}
		}
		return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixCodeReview, DoneFile: df.Builder, WorkDir: builderDir}
	}

	if v := readDoneVerdict(df.Builder); v != "" {
		return phaseAction{Phase: PhaseCodeReview, DoneFile: df.CodeReview, WorkDir: builderDir}
	}

	return phaseAction{Phase: PhaseBuild, Scenario: ScenarioInit, DoneFile: df.Builder, WorkDir: builderDir}
}

func clearDir(dir string) {
	os.MkdirAll(dir, 0755)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func clearInnerLoopDoneFiles(df doneFiles) {
	os.Remove(df.Builder)
	os.Remove(df.CodeReview)
	os.Remove(df.SecurityReview)
	os.Remove(df.DevTest)
}

func prePhaseCleanup(action phaseAction, df doneFiles, opts Options) {
	switch action.Phase {
	case PhaseBuild:
		// When advancing to next spec, clear all inner-loop done files
		// so the new spec starts with a clean slate.
		if action.Scenario == ScenarioNextSpec {
			clearInnerLoopDoneFiles(df)
		}
	case PhaseCodeReview:
		clearDir(filepath.Join(opts.BuilderDir, "code-review"))
	case PhaseSecurityReview:
		clearDir(filepath.Join(opts.BuilderDir, "security-review"))
	case PhaseDevTest:
		clearDir(filepath.Join(opts.BuilderDir, "dev-test-results"))
	case PhaseUAT:
		clearDir(filepath.Join(opts.BuilderDir, "uat-feedback"))
	}
}

// Prompt loading

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

func loadBuilderPrompt(promptsDir string, scenario Scenario, opts Options) (string, error) {
	scenarioFile := fmt.Sprintf("scenario-%s.md", scenario)
	scenarioText, err := loadPrompt(promptsDir, scenarioFile, opts)
	if err != nil {
		return "", err
	}
	baseText, err := loadPrompt(promptsDir, "builder-base.md", opts)
	if err != nil {
		return "", err
	}
	return scenarioText + "\n\n" + baseText + builderSuffix, nil
}

func loadAgentPrompt(promptsDir string, phase Phase, opts Options) (string, error) {
	promptFile := fmt.Sprintf("%s.md", phase)
	text, err := loadPrompt(promptsDir, promptFile, opts)
	if err != nil {
		return "", err
	}
	switch phase {
	case PhaseDevTest:
		return text + devTestSuffix, nil
	case PhaseUAT:
		return text + uatSuffix, nil
	default:
		return text + reviewSuffix, nil
	}
}

// Logging

func appendPhaseLog(runLog *RunLog, action phaseAction, prompt string, start, end time.Time, verdict string) {
	entry := PhaseLog{
		Phase:     string(action.Phase),
		Prompt:    prompt,
		Start:     start,
		End:       end,
		DurationS: end.Sub(start).Seconds(),
		Verdict:   verdict,
	}

	if action.Phase == PhaseUAT {
		if runLog.UAT == nil {
			runLog.UAT = &UATLog{}
		}
		runLog.UAT.Phases = append(runLog.UAT.Phases, entry)
		return
	}

	if len(runLog.Specs) == 0 {
		runLog.Specs = append(runLog.Specs, SpecLog{Spec: "current"})
	}
	last := &runLog.Specs[len(runLog.Specs)-1]

	if action.Phase == PhaseDevTest && strings.EqualFold(verdict, "PASS") {
		last.Phases = append(last.Phases, entry)
		runLog.Specs = append(runLog.Specs, SpecLog{Spec: "current"})
		return
	}

	last.Phases = append(last.Phases, entry)
}

func finishRunLog(runLog *RunLog, runStart time.Time, outcome, logFile string) {
	runLog.Outcome = outcome
	runLog.EndTime = time.Now()
	runLog.Duration = runLog.EndTime.Sub(runStart).Round(time.Second).String()
	runLog.DurationS = runLog.EndTime.Sub(runStart).Seconds()
	writeRunLog(logFile, *runLog)
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

// Main loop

func main() {
	signal.Ignore(syscall.SIGTERM, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGHUP)

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

	df := newDoneFiles(opts.BuilderDir, opts.UATDir)

	runStart := time.Now()
	runLog := RunLog{
		StartTime: runStart,
		Model:     opts.Model,
		Outcome:   "error",
	}

	gateFailCount := 0
	iterationCount := 0

	for {
		action := determineNextPhase(df, opts.BuilderDir, opts.UATDir)

		if action.Phase == "done" {
			finishRunLog(&runLog, runStart, "pass", opts.LogFile)
			fmt.Printf("\nSUCCESS — UAT passed! Total time: %s\n", runLog.Duration)
			return
		}

		iterationCount++
		if iterationCount > opts.MaxRounds {
			finishRunLog(&runLog, runStart, "fail_max_rounds", opts.LogFile)
			die("max iterations (%d) reached — aborting. Total time: %s", opts.MaxRounds, runLog.Duration)
		}

		// Pre-phase cleanup
		os.Remove(action.DoneFile)
		prePhaseCleanup(action, df, opts)

		// Build the prompt
		var prompt string
		var promptLabel string
		var err error

		switch action.Phase {
		case PhaseBuild:
			prompt, err = loadBuilderPrompt(opts.PromptsDir, action.Scenario, opts)
			promptLabel = fmt.Sprintf("scenario-%s.md + builder-base.md", action.Scenario)
		case PhaseUAT:
			prompt, err = loadAgentPrompt(opts.PromptsDir, PhaseUAT, opts)
			promptLabel = "uat.md"
		default:
			prompt, err = loadAgentPrompt(opts.PromptsDir, action.Phase, opts)
			promptLabel = fmt.Sprintf("%s.md", action.Phase)
		}
		if err != nil {
			finishRunLog(&runLog, runStart, "error", opts.LogFile)
			die("loading prompt: %v", err)
		}

		// Print phase header
		totalDur := time.Since(runStart).Round(time.Second)
		if action.Phase == PhaseBuild {
			fmt.Printf("\n--- Builder [%s] (elapsed: %s) ---\n", action.Scenario, totalDur)
		} else {
			fmt.Printf("\n--- %s (elapsed: %s) ---\n", action.Phase, totalDur)
		}

		// Run the phase
		phaseStart := time.Now()
		if !runPhaseWithRetries(string(action.Phase), promptLabel, action.WorkDir, prompt, action.DoneFile, opts) {
			finishRunLog(&runLog, runStart, "error", opts.LogFile)
			die("%s did not create done file after %d attempts — aborting", action.Phase, opts.Retries+1)
		}
		phaseEnd := time.Now()
		phaseDur := phaseEnd.Sub(phaseStart).Round(time.Second)

		// Read verdict
		verdict := readDoneVerdict(action.DoneFile)
		fmt.Printf("[flywheel] %s: %s (%s)\n", action.Phase, verdict, phaseDur)

		// Log the phase
		appendPhaseLog(&runLog, action, promptLabel, phaseStart, phaseEnd, verdict)

		// Track gate failures for --max-gate-retries
		if action.Phase != PhaseBuild && action.Phase != PhaseUAT {
			if strings.EqualFold(verdict, "FAIL") {
				gateFailCount++
				if opts.MaxGateRetries > 0 && gateFailCount >= opts.MaxGateRetries {
					finishRunLog(&runLog, runStart, "fail_gate_retries", opts.LogFile)
					die("builder failed gate %d times on same spec — aborting", gateFailCount)
				}
			}
		}

		// Reset gate counter when dev test passes or UAT READY
		// Note: done files are NOT cleared here — determineNextPhase needs to see
		// dev_test_done PASS to route to ScenarioNextSpec. Clearing happens in
		// prePhaseCleanup when the next builder invocation starts.
		if action.Phase == PhaseDevTest && (strings.EqualFold(verdict, "PASS") || strings.EqualFold(verdict, "UAT READY")) {
			gateFailCount = 0
		}
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
	// Note: we do NOT use Setpgid here — claude needs TTY access for its UI,
	// which requires being in the terminal's foreground process group.
	// Signal isolation is handled by signal.Ignore in main() instead.
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

// stateFiles are ephemeral files managed by the driver, not code the builder should commit.
// The builder writes progress/done files after its final commit, so these may be dirty.
var stateFiles = map[string]bool{
	"progress":             true,
	"builder_done":         true,
	"code_review_done":     true,
	"security_review_done": true,
	"dev_test_done":        true,
	"uat_done":             true,
}

// gitWorktreeClean returns true if there are no unstaged or uncommitted changes to tracked files,
// ignoring ephemeral state files (progress, *_done) that the driver manages.
func gitWorktreeClean(dir string) bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		// git status --porcelain format: XY filename
		file := strings.TrimSpace(line[2:])
		file = filepath.Base(file) // strip any path prefix
		if !stateFiles[file] {
			return false // non-state file is dirty
		}
	}
	return true
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
