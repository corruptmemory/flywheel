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
	Resume          bool          `long:"resume" description:"Resume from where we left off (skip log truncation)"`
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

// LogEntry is a single JSONL line. Phase entries have Type "phase"; the final line has Type "summary".
type LogEntry struct {
	Type      string    `json:"type"`                // "phase" or "summary"
	Phase     string    `json:"phase,omitempty"`      // e.g. "build", "code-review"
	Scenario  string    `json:"scenario,omitempty"`   // e.g. "init", "fix-code-review" (build phases only)
	Prompt    string    `json:"prompt,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	DurationS float64   `json:"duration_seconds"`
	Verdict   string    `json:"verdict,omitempty"`
	Model     string    `json:"model,omitempty"`      // summary only
	Outcome   string    `json:"outcome,omitempty"`    // summary only
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
		// Always clear builder_done — a new one will be written.
		os.Remove(df.Builder)
		switch action.Scenario {
		case ScenarioNextSpec:
			// Advancing to next spec: clear all inner-loop done files.
			clearInnerLoopDoneFiles(df)
		case ScenarioFixCodeReview:
			os.Remove(df.CodeReview)
		case ScenarioFixSecurityReview:
			os.Remove(df.SecurityReview)
		case ScenarioFixDevTest:
			os.Remove(df.DevTest)
		case ScenarioFixUAT:
			os.Remove(df.UAT)
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

func appendLogEntry(logFile string, entry LogEntry) {
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write log to %s: %v\n", logFile, err)
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(entry); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not encode log entry: %v\n", err)
	}
}

// countUATRuns counts how many UAT phase entries exist in the log file (for --resume).
func countUATRuns(logFile string) int {
	data, err := os.ReadFile(logFile)
	if err != nil {
		return 0
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		var entry LogEntry
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Type == "phase" && entry.Phase == string(PhaseUAT) {
			count++
		}
	}
	return count
}

func logPhase(logFile string, action phaseAction, prompt string, start, end time.Time, verdict string) {
	appendLogEntry(logFile, LogEntry{
		Type:      "phase",
		Phase:     string(action.Phase),
		Scenario:  string(action.Scenario),
		Prompt:    prompt,
		Start:     start,
		End:       end,
		DurationS: end.Sub(start).Seconds(),
		Verdict:   verdict,
	})
}

func logSummary(logFile string, runStart time.Time, model, outcome string) {
	now := time.Now()
	appendLogEntry(logFile, LogEntry{
		Type:      "summary",
		Start:     runStart,
		End:       now,
		DurationS: now.Sub(runStart).Seconds(),
		Model:     model,
		Outcome:   outcome,
	})
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

	if !opts.Resume {
		os.Truncate(opts.LogFile, 0)
	}

	runStart := time.Now()
	gateFailCount := 0
	uatCount := countUATRuns(opts.LogFile)
	if uatCount > 0 {
		fmt.Printf("[flywheel] resuming with %d prior UAT run(s)\n", uatCount)
	}

	for {
		action := determineNextPhase(df, opts.BuilderDir, opts.UATDir)

		if action.Phase == "done" {
			logSummary(opts.LogFile, runStart, opts.Model, "pass")
			totalDur := time.Since(runStart).Round(time.Second)
			fmt.Printf("\nSUCCESS — UAT passed! Total time: %s\n", totalDur)
			return
		}

		// Count UAT invocations as rounds (full pipeline cycles)
		if action.Phase == PhaseUAT {
			uatCount++
			if uatCount > opts.MaxRounds {
				logSummary(opts.LogFile, runStart, opts.Model, "fail_max_rounds")
				totalDur := time.Since(runStart).Round(time.Second)
				die("max UAT rounds (%d) reached — aborting. Total time: %s", opts.MaxRounds, totalDur)
			}
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
			logSummary(opts.LogFile, runStart, opts.Model, "error")
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
			logSummary(opts.LogFile, runStart, opts.Model, "error")
			die("%s did not create done file after %d attempts — aborting", action.Phase, opts.Retries+1)
		}
		phaseEnd := time.Now()
		phaseDur := phaseEnd.Sub(phaseStart).Round(time.Second)

		// Read verdict
		verdict := readDoneVerdict(action.DoneFile)
		fmt.Printf("[flywheel] %s: %s (%s)\n", action.Phase, verdict, phaseDur)

		// Log the phase immediately (append-only, survives kills)
		logPhase(opts.LogFile, action, promptLabel, phaseStart, phaseEnd, verdict)

		// Track gate failures for --max-gate-retries
		if action.Phase != PhaseBuild && action.Phase != PhaseUAT {
			if strings.EqualFold(verdict, "FAIL") {
				gateFailCount++
				if opts.MaxGateRetries > 0 && gateFailCount >= opts.MaxGateRetries {
					logSummary(opts.LogFile, runStart, opts.Model, "fail_gate_retries")
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

	// Write prompt to a temp file so it doesn't appear in the process argv.
	// This prevents pgrep/pkill from matching the claude process when agents
	// search for strings that happen to appear in the prompt (e.g. "file-uploader --mock").
	promptFile := filepath.Join(dir, ".flywheel-prompt.md")
	if err := os.WriteFile(promptFile, []byte(prompt), 0600); err != nil {
		return fmt.Errorf("writing prompt file: %w", err)
	}
	defer os.Remove(promptFile)

	args := []string{}
	if opts.SkipPermissions {
		args = append(args, "--permission-mode", "bypassPermissions")
	}
	if opts.Model != "" {
		args = append(args, "--model", opts.Model)
	}
	args = append(args, "Read .flywheel-prompt.md and follow ALL instructions in it exactly. Do not summarize or ask questions — just execute.")

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
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := os.Stat(doneFile); err != nil {
				continue // done file doesn't exist yet
			}
			// Done file exists — terminate immediately. The builder commits
			// BEFORE writing progress/builder_done, so code is already pushed.
			// Waiting longer just gives the model time to ignore "STOP" and
			// start the next spec.
			fmt.Printf("\n[flywheel] done file detected: %s — terminating session\n", doneFile)
			cancel()
			return
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
