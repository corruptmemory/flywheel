# Round 3 Multi-Phase Pipeline Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Rewrite the flywheel driver from a simple build→UAT loop to a 5-phase per-spec pipeline with quality gates, and create all prompt files, workspaces, and design system artifacts for round 3.

**Architecture:** Done-file-driven state machine. Driver reads done files to determine next phase, launches claude with concatenated prompts (scenario + base + suffix), and routes failures back to the builder. All review/test agents are adversarial and run in the builder directory.

**Tech Stack:** Go (go-flags, os/exec, encoding/json), Markdown prompts, bash (for smoke tests)

**Design doc:** `docs/plans/2026-03-13-round3-multi-phase-pipeline-design.md`

---

### Task 1: Driver Types and CLI Flags

**Files:**
- Modify: `cmd/flywheel/main.go:17-31` (Options struct)
- Modify: `cmd/flywheel/main.go:57-84` (RunLog/RoundLog structs)

**Step 1: Update Options struct**

Replace the Options struct. Remove `Start` and `Resume`. Add `MaxGateRetries`.

```go
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
```

**Step 2: Replace RunLog/RoundLog with new log types**

```go
// RunLog is the top-level entry written to the JSONL log file.
type RunLog struct {
	StartTime time.Time  `json:"start_time"`
	EndTime   time.Time  `json:"end_time,omitempty"`
	Duration  string     `json:"duration,omitempty"`
	DurationS float64    `json:"duration_seconds,omitempty"`
	Model     string     `json:"model,omitempty"`
	Outcome   string     `json:"outcome"` // "pass", "fail_max_rounds", "fail_gate_retries", "error"
	Specs     []SpecLog  `json:"specs"`
	UAT       *UATLog    `json:"uat,omitempty"`
}

// SpecLog records all phases for a single spec's quality gate cycle.
type SpecLog struct {
	Spec   string     `json:"spec"`
	Phases []PhaseLog `json:"phases"`
}

// UATLog records the UAT phase(s) after all specs pass.
type UATLog struct {
	Phases []PhaseLog `json:"phases"`
}

// PhaseLog records timing and verdict for a single phase execution.
type PhaseLog struct {
	Phase     string    `json:"phase"`
	Prompt    string    `json:"prompt,omitempty"`
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	DurationS float64   `json:"duration_seconds"`
	Verdict   string    `json:"verdict,omitempty"`
}
```

**Step 3: Build and verify**

Run: `go build -o flywheel ./cmd/flywheel/`
Expected: Compilation errors (main references old types) — that's fine, we'll fix in subsequent tasks.

**Step 4: Commit**

```bash
git add cmd/flywheel/main.go
git commit -m "Update types and CLI flags for round 3 multi-phase pipeline"
```

---

### Task 2: Phase Constants, Suffixes, and Prompt Loading

**Files:**
- Modify: `cmd/flywheel/main.go` (replace suffix constants, add phase types)

**Step 1: Define phase type and completion suffixes**

Replace the `builderSuffix` and `uatSuffix` constants with per-phase suffixes:

```go
// Phase represents a pipeline phase.
type Phase string

const (
	PhaseBuild          Phase = "build"
	PhaseCodeReview     Phase = "code-review"
	PhaseSecurityReview Phase = "security-review"
	PhaseDevTest        Phase = "dev-test"
	PhaseUAT            Phase = "uat"
)

// Builder scenarios determine which prompt snippet to prepend.
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

- If ALL checklist items pass, run: echo "PASS" > done
- If ANY checklist item fails, run: echo "FAIL" > done

The session will end automatically once the done file is detected.
`
```

**Step 2: Add prompt concatenation function**

```go
// loadBuilderPrompt loads scenario snippet + builder base + suffix.
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

// loadAgentPrompt loads a review/test agent prompt + its suffix.
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
```

**Step 3: Commit**

```bash
git add cmd/flywheel/main.go
git commit -m "Add phase types, completion suffixes, and prompt concatenation"
```

---

### Task 3: State Derivation and Cleanup Functions

**Files:**
- Modify: `cmd/flywheel/main.go` (add determineNextPhase, cleanup helpers)

**Step 1: Add done file path helpers**

```go
// doneFiles returns paths for all done files.
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
```

**Step 2: Add readDoneVerdict helper**

```go
// readDoneVerdict reads the first line of a done file, returning "" if it doesn't exist.
func readDoneVerdict(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)[0]
	return strings.TrimSpace(line)
}
```

**Step 3: Add phaseAction return type and determineNextPhase**

```go
// phaseAction describes what the driver should do next.
type phaseAction struct {
	Phase    Phase
	Scenario Scenario // only meaningful for PhaseBuild
	DoneFile string   // which done file this phase writes
	WorkDir  string   // directory to run the agent in
}

// determineNextPhase reads done files and returns the next action.
func determineNextPhase(df doneFiles, builderDir, uatDir string) phaseAction {
	// Priority 1: UAT verdict
	if v := readDoneVerdict(df.UAT); v != "" {
		if strings.EqualFold(v, "PASS") {
			return phaseAction{Phase: "done"}
		}
		return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixUAT, DoneFile: df.Builder, WorkDir: builderDir}
	}

	// Priority 2: Dev test verdict
	if v := readDoneVerdict(df.DevTest); v != "" {
		switch {
		case strings.EqualFold(v, "UAT READY"):
			return phaseAction{Phase: PhaseUAT, DoneFile: df.UAT, WorkDir: uatDir}
		case strings.EqualFold(v, "PASS"):
			return phaseAction{Phase: PhaseBuild, Scenario: ScenarioNextSpec, DoneFile: df.Builder, WorkDir: builderDir}
		default: // FAIL
			return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixDevTest, DoneFile: df.Builder, WorkDir: builderDir}
		}
	}

	// Priority 3: Security review verdict
	if v := readDoneVerdict(df.SecurityReview); v != "" {
		if strings.EqualFold(v, "PASS") {
			return phaseAction{Phase: PhaseDevTest, DoneFile: df.DevTest, WorkDir: builderDir}
		}
		return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixSecurityReview, DoneFile: df.Builder, WorkDir: builderDir}
	}

	// Priority 4: Code review verdict
	if v := readDoneVerdict(df.CodeReview); v != "" {
		if strings.EqualFold(v, "PASS") {
			return phaseAction{Phase: PhaseSecurityReview, DoneFile: df.SecurityReview, WorkDir: builderDir}
		}
		return phaseAction{Phase: PhaseBuild, Scenario: ScenarioFixCodeReview, DoneFile: df.Builder, WorkDir: builderDir}
	}

	// Priority 5: Builder done → code review
	if v := readDoneVerdict(df.Builder); v != "" {
		return phaseAction{Phase: PhaseCodeReview, DoneFile: df.CodeReview, WorkDir: builderDir}
	}

	// Priority 6: Nothing exists → first run
	return phaseAction{Phase: PhaseBuild, Scenario: ScenarioInit, DoneFile: df.Builder, WorkDir: builderDir}
}
```

**Step 4: Add cleanup helpers**

```go
// clearDir removes all files in a directory (but not subdirectories).
// Creates the directory if it doesn't exist.
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

// clearInnerLoopDoneFiles removes all per-spec done files for a fresh cycle.
func clearInnerLoopDoneFiles(df doneFiles) {
	os.Remove(df.Builder)
	os.Remove(df.CodeReview)
	os.Remove(df.SecurityReview)
	os.Remove(df.DevTest)
}
```

**Step 5: Commit**

```bash
git add cmd/flywheel/main.go
git commit -m "Add state derivation from done files and cleanup helpers"
```

---

### Task 4: Main Loop Rewrite

**Files:**
- Modify: `cmd/flywheel/main.go:86-257` (replace entire main function)

**Step 1: Rewrite main()**

```go
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
		Outcome:   "error", // updated on clean exit
	}

	gateFailCount := 0  // consecutive gate failures on current spec
	iterationCount := 0 // total iterations (safety cap via --max-rounds)

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

		// Reset gate counter when dev test passes (spec advances)
		if action.Phase == PhaseDevTest && strings.EqualFold(verdict, "PASS") {
			gateFailCount = 0
			clearInnerLoopDoneFiles(df)
		}

		// Reset gate counter on UAT READY (moving to UAT)
		if action.Phase == PhaseDevTest && strings.EqualFold(verdict, "UAT READY") {
			gateFailCount = 0
		}
	}
}
```

**Step 2: Add prePhaseCleanup**

```go
// prePhaseCleanup clears feedback directories before launching review/test agents.
func prePhaseCleanup(action phaseAction, df doneFiles, opts Options) {
	switch action.Phase {
	case PhaseCodeReview:
		clearDir(filepath.Join(opts.BuilderDir, "code-review"))
	case PhaseSecurityReview:
		clearDir(filepath.Join(opts.BuilderDir, "security-review"))
	case PhaseDevTest:
		clearDir(filepath.Join(opts.BuilderDir, "dev-test-results"))
	case PhaseUAT:
		clearDir(filepath.Join(opts.BuilderDir, "uat-feedback"))
	case PhaseBuild:
		// When builder responds to a gate failure, delete builder_done
		// so the inner loop restarts from code review after the fix.
		// (builder_done deletion already happens via os.Remove(action.DoneFile) above)
	}
}
```

**Step 3: Add appendPhaseLog**

```go
// appendPhaseLog adds a phase entry to the appropriate place in the run log.
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

	// For spec phases, append to the current spec log (or create one).
	// The spec name comes from builder_done, but we may not have it yet.
	// Use "unknown" as placeholder; could be enriched later.
	if len(runLog.Specs) == 0 {
		runLog.Specs = append(runLog.Specs, SpecLog{Spec: "current"})
	}
	last := &runLog.Specs[len(runLog.Specs)-1]

	// If dev test passed, close this spec and prepare next
	if action.Phase == PhaseDevTest && strings.EqualFold(verdict, "PASS") {
		last.Phases = append(last.Phases, entry)
		runLog.Specs = append(runLog.Specs, SpecLog{Spec: "current"})
		return
	}

	last.Phases = append(last.Phases, entry)
}
```

**Step 4: Build and verify**

Run: `go build -o flywheel ./cmd/flywheel/`
Expected: Clean build.

**Step 5: Commit**

```bash
git add cmd/flywheel/main.go
git commit -m "Rewrite main loop as done-file-driven state machine"
```

---

### Task 5: Remove Dead Code, Build, and Smoke Test

**Files:**
- Modify: `cmd/flywheel/main.go` (remove old readLastRunLog, old finishRunLog, any unused code)

**Step 1: Clean up**

Remove `readLastRunLog` (no longer needed — state comes from done files, not log file).

Update `finishRunLog` to work with new RunLog type (it should already work — just verify).

Remove any remaining references to old `RoundLog` type.

**Step 2: Build and verify help output**

Run: `go build -o flywheel ./cmd/flywheel/ && ./flywheel --help`

Expected: Shows new flags (`--max-gate-retries`), does NOT show `--start` or `--resume`.

**Step 3: Smoke test with touch-based prompts**

Create a minimal smoke test:
```bash
mkdir -p /tmp/flywheel-smoke-r3/{builder,uat,prompts}

# Scenario init: just touch builder_done
cat > /tmp/flywheel-smoke-r3/prompts/scenario-init.md << 'EOF'
Touch builder_done with content showing spec 01 JUST COMPLETED.
EOF

cat > /tmp/flywheel-smoke-r3/prompts/builder-base.md << 'EOF'
You are a builder. Follow the scenario instructions above.
EOF

# ... (create minimal prompts for each phase that just write done files)
```

Verify the driver routes through: build → code review → security review → dev test → (PASS) → next build, etc.

**Step 4: Commit**

```bash
git add cmd/flywheel/main.go
git commit -m "Clean up dead code, verify round 3 driver builds and routes correctly"
```

---

### Task 6: Write Builder Prompts (base + 6 scenarios)

**Files:**
- Create: `prompts/builder-base.md`
- Create: `prompts/scenario-init.md`
- Create: `prompts/scenario-next-spec.md`
- Create: `prompts/scenario-fix-code-review.md`
- Create: `prompts/scenario-fix-security-review.md`
- Create: `prompts/scenario-fix-dev-test.md`
- Create: `prompts/scenario-fix-uat.md`
- Delete: `prompts/builder-init.md` (replaced)
- Delete: `prompts/builder-fix.md` (replaced)

**Step 1: Write `builder-base.md`**

Common instructions: sub-agent strategy, progress file format and protocol, builder_done format, commit discipline, smoke testing, design system reference, "don't ask questions."

**Step 2: Write all 6 scenario files**

Small, focused preambles. Each ~10-20 lines. Context-specific instructions only.

**Step 3: Remove old prompt files**

```bash
rm prompts/builder-init.md prompts/builder-fix.md
```

**Step 4: Commit**

```bash
git add prompts/
git commit -m "Add round 3 builder prompts: base + 6 scenario snippets"
```

---

### Task 7: Write Review/Test Agent Prompts

**Files:**
- Create: `prompts/code-review.md`
- Create: `prompts/security-review.md`
- Create: `prompts/dev-test.md`
- Modify: `prompts/uat.md` (update for round 3 workspace paths)

**Step 1: Write `code-review.md`**

Adversarial code review. Reads `builder_done` to scope. Reads `progress` for context. Reads relevant specs. Reviews: code quality, style, spec divergence, missing tests, CLI conventions. Builds automation in `code-review-tools/`. Writes findings to `code-review/`. Writes `code_review_done`.

**Step 2: Write `security-review.md`**

Adversarial security review. Same scoping. Reviews: PII leaks, file cleanup, replay attacks, JWT handling, CSV poisoning, injection. Builds automation in `security-harness/`. Writes findings to `security-review/`. Writes `security_review_done`.

**Step 3: Write `dev-test.md`**

Adversarial dev testing. Same scoping. Runs regression tests from `test-harness/`. Builds new tests for JUST COMPLETED spec. Smoke tests with curl/Playwright. Writes results to `dev-test-results/`. Maintains harness in `test-harness/`. Writes `dev_test_done` with PASS/FAIL/UAT READY.

**Step 4: Update `uat.md`**

Update `{{BUILDER_DIR}}` and `{{UAT_DIR}}` references. Add `uat_done` (not just `done`). Otherwise largely unchanged from round 2.

**Step 5: Commit**

```bash
git add prompts/
git commit -m "Add round 3 review/test agent prompts (code review, security, dev test)"
```

---

### Task 8: Extract Design System from Round 2

**Files:**
- Create: `design-system/tokens.css` (extracted from round 2)
- Create: `design-system/patterns.md`
- Create: `design-system/layout.md`
- Create: `design-system/screenshots/` (captured from round 2 app)

**Step 1: Copy tokens.css**

```bash
cp ~/projects/file-uploader-r2/static/css/tokens.css design-system/tokens.css
```

**Step 2: Write `patterns.md`**

Analyze `~/projects/file-uploader-r2/static/css/app.css` and the templ templates. Describe component patterns: cards, badges, forms, tables, modals, navigation, upload zone, progress bars, status indicators.

**Step 3: Write `layout.md`**

Describe: page structure (nav + main content), responsive breakpoints, grid behavior, max-width constraint, sidebar width.

**Step 4: Capture screenshots**

Launch the round 2 app and use Playwright to screenshot: login, dashboard (empty + with files), archive, settings, players DB, setup wizard.

```bash
cd ~/projects/file-uploader-r2
./file-uploader --mock -p 8080 &
# ... Playwright screenshots ...
kill %1
```

Save to `design-system/screenshots/`.

**Step 5: Commit**

```bash
git add design-system/
git commit -m "Extract design system from round 2 (tokens, patterns, layout, screenshots)"
```

---

### Task 9: Create Round 3 Builder Workspace

**Files:**
- Create: `~/projects/file-uploader-r3/CLAUDE.md`
- Create: `~/projects/file-uploader-r3/README.md`
- Copy: `~/projects/file-uploader-r2/build-specification/` → `~/projects/file-uploader-r3/build-specification/`
- Copy: design system into workspace
- Create: GitHub remote

**Step 1: Create directory and init git**

```bash
mkdir -p ~/projects/file-uploader-r3
cd ~/projects/file-uploader-r3
git init
```

**Step 2: Write CLAUDE.md**

Builder agent instructions: sub-agent strategy, progress file protocol, builder_done format, feedback directories, tech stack rules, design system reference. Updated from round 2 CLAUDE.md.

**Step 3: Write README.md**

Project overview, tech stack, constraints, build commands. Updated from round 2.

**Step 4: Copy and refine specs**

Copy all 15 specs from round 2. Refine:
- Specs 10/11: Add explicit "wire real implementation in non-mock mode" mandates
- Specs 12-14: Add "follow the design system in `design-system/`" references
- Minor updates based on round 2 lessons

**Step 5: Copy design system**

```bash
cp -r ~/projects/flywheel/design-system ~/projects/file-uploader-r3/design-system
```

**Step 6: Create GitHub remote and push**

```bash
gh repo create corruptmemory/file-uploader-r3 --public --source=. --push
```

**Step 7: Commit in flywheel repo**

No commit needed in flywheel — the workspace is a separate repo.

---

### Task 10: Create Round 3 UAT Workspace

**Files:**
- Create: `~/projects/file-uploader-r3-uat/README.md`
- Create: GitHub remote

**Step 1: Create directory and init git**

```bash
mkdir -p ~/projects/file-uploader-r3-uat
cd ~/projects/file-uploader-r3-uat
git init
```

**Step 2: Write README.md**

Updated from round 2 UAT README. Same adversarial checklist with:
- No BLOCKED verdicts
- Spec verification protocol
- Test automation strategy
- `uat_done` (not `done`) file protocol
- Updated workspace paths for r3

**Step 3: Create GitHub remote and push**

```bash
gh repo create corruptmemory/file-uploader-r3-uat --public --source=. --push
```

---

### Task 11: Update Flywheel CLAUDE.md and README.md

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`

**Step 1: Update CLAUDE.md**

Add round 3 section documenting: multi-phase pipeline, new prompt architecture, done-file protocol, design system approach. Update driver documentation with new flags and state machine description.

**Step 2: Update README.md**

Add round 3 row to the outputs table (status: In progress). Update driver CLI example with new flags. Add brief description of the multi-phase pipeline.

**Step 3: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "Update docs for round 3 multi-phase pipeline"
```

---

### Task 12: End-to-End Smoke Test

**Files:**
- No new files — verification only

**Step 1: Build driver**

```bash
go build -o flywheel ./cmd/flywheel/
```

**Step 2: Create minimal smoke test prompts**

Create trivial prompt files that just write the appropriate done files (similar to the round 2 smoke test). Verify the driver correctly routes through the full state machine:

1. init → builder_done → code review → code_review_done PASS → security review → security_review_done PASS → dev test → dev_test_done PASS → next spec → ... → dev_test_done UAT READY → UAT → uat_done PASS → DONE

2. Test FAIL routing: code_review_done FAIL → builder (fix-code-review) → builder_done → code review again

3. Test gate retry limit: set `--max-gate-retries 1`, have code review always FAIL → driver halts

**Step 3: Verify logging**

Check that `flywheel-runs.jsonl` contains proper per-spec phase history.

**Step 4: Commit any fixes**

```bash
git add cmd/flywheel/main.go
git commit -m "Fix issues found during end-to-end smoke test"
```

**Step 5: Final push**

```bash
git push
```
