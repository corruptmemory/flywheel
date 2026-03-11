# File Uploader Specification Writing Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Write 8 layered build specifications + 2 READMEs that enable an independent agent to build a Go web application for gaming CSV anonymization and upload, from scratch, without access to the source project.

**Architecture:** Layered specs in `~/projects/file-uploader/build-specification/`, numbered 01-08, each covering one concern. A top-level README.md orients the builder agent. The UAT project at `~/projects/file-uploader-uat/` gets its own README. All specs are model-agnostic (Claude, Gemini, ChatGPT/Codex).

**Tech Stack:** Markdown specifications targeting: Go, chi router, templ templates, htmx, go-flags CLI, BurntSushi/toml config, Argon2 hashing, SSE, JWT auth, custom CSS design tokens.

**Design Doc:** `docs/plans/2026-03-11-file-uploader-spec-design.md`

**Source Reference (for spec authors only, never shared with builder):** `~/idpair/idpair-central/`

---

## Important Notes for the Spec Writer

- **Never include source code** from idpair-central. Describe interfaces, logic, and constraints textually.
- **Never reference** idpair-central, idPair, or any source project names in specs.
- **Do include** Go interface definitions — these are specifications, not copied code.
- **Do include** example data structures, TOML snippets, CLI usage examples.
- **Use the actor-pattern skill** when describing concurrency patterns.
- **Reference `~/idpair/example-csvs/`** — the builder IS allowed to know about these files.
- **Each spec is self-contained** enough to implement without reading all others, but references earlier specs for shared patterns.

---

### Task 1: Write 01-overview.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/01-overview.md`

**Step 1: Write the overview spec**

This is the "constitution" — every other spec references it. Cover:

1. **Project Purpose** — A local data bridge for regulated gaming. Operators upload CSV files containing player/gaming data. The app validates rows, anonymizes PII via Argon2 hashing, transforms columns to a canonical format, and delivers anonymized output to a remote endpoint. No PII ever leaves the machine.

2. **Tech Stack** — Enumerate every dependency with import path:
   - Router: `github.com/go-chi/chi/v5`
   - Templates: `github.com/a-h/templ` (edit .templ files only, never generated `_templ.go`)
   - Frontend: htmx (vendored JS, no npm/node)
   - CLI: `github.com/jessevdk/go-flags` with subcommands
   - Config: TOML via `github.com/BurntSushi/toml`
   - Hashing: `golang.org/x/crypto/argon2`
   - CSV: `encoding/csv` + `github.com/dimchansky/utfbom` for BOM handling
   - CSS: Custom design tokens (`tokens.css`) + utility classes. No CSS frameworks.
   - Build: `./build.sh` (never `go build` directly)

3. **Cross-Cutting Patterns** — These apply everywhere:
   - **Goroutine actor pattern** for all shared mutable state (describe the pattern: single goroutine owns state, typed command channel, `Stop()`/`Wait()` lifecycle, generic channel helpers)
   - **Interface-first design** — every external dependency has an interface with at least 2 implementations (real + mock)
   - **File-based storage** — no database, all persistence via files on disk
   - **Single static binary** — all assets embedded via `go:embed`
   - **CLI arguments over environment variables** — use go-flags with sensible defaults, TOML config for complex values, config file path is a CLI flag

4. **Core Invariant** — No PII leaves the machine. All player identifiers are hashed before output. Name normalization is one-way (lowercase, strip diacritics, normalize punctuation).

5. **Project Layout Convention:**
   ```
   cmd/                        -- main package (main.go, settables.go, configuration.go)
   internal/
     app/                      -- application state machine and business logic
     apiclient/                -- remote API client (behind interface)
     chanutil/                 -- generic channel helpers for actor pattern
     csv/                      -- CSV parsing, validation, type system
       columnmapping/          -- per-CSV-type column handlers
     hashers/                  -- hash algorithm implementations
     playerdb/                 -- player deduplication database
     server/                   -- HTTP handlers, routing, SSE
       pages/                  -- templ templates (.templ files)
     uploader/                 -- file upload to remote (behind interface)
     util/                     -- shared utilities
   static/
     vendor/                   -- vendored JS/CSS (htmx, etc.)
     css/                      -- custom CSS (tokens.css, app.css)
     js/                       -- custom JS (app.js, sse.js)
     img/                      -- favicon, icons
   ```

6. **Constraints:**
   - US-only country/state validation (50 states + DC)
   - No database — file-based persistence only
   - Specs are the sole source of truth — no peeking at any existing codebase

**Step 2: Review for source leakage**

Read through the file and verify no references to idpair-central, idPair company, or source-specific implementation details leak through. The spec should describe WHAT to build, not replicate existing code.

**Step 3: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/01-overview.md
git commit -m "spec: add 01-overview — project goals, tech stack, cross-cutting patterns"
```

---

### Task 2: Write 02-architecture.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/02-architecture.md`

**Step 1: Write the architecture spec**

Cover:

1. **Application State Machine** — The app is a state machine with three states:
   - `SetupApp` — interactive setup wizard for initial configuration
   - `RunningApp` — main operational mode (CSV processing, web UI, API uploads)
   - `ErrorApp` — unrecoverable error display

   The `Application` struct owns a single `Stoppable` state. State transitions happen via builder functions passed through the actor command channel. Define the `Stoppable` interface.

2. **RecoverableError** — A special error type that carries a "next state builder." When the RunningApp fails to authenticate (e.g., expired service credentials), it returns a RecoverableError that transitions back to SetupApp with a reason message, rather than crashing. Define the interface and flow.

3. **Actor Pattern Template** — Provide the full template (reference actor-pattern skill). For each actor:
   - Define a command tag enum (iota constants)
   - Define a command struct with `tag`, domain fields, and `result chan any`
   - The unexported struct holds `commands chan`, `sync.WaitGroup`, and all owned state
   - A single `run()` goroutine processes commands via `for msg := range commands`
   - Public methods send commands and receive results via the channel
   - `Stop()` closes the channel, `Wait()` blocks on the WaitGroup

   Provide the generic channel helper functions:
   - `SendReceiveMessage[C, K](commands chan C, cmd C) (K, error)` — send command, get typed result
   - `SendReceiveError[C](commands chan C, cmd C) error` — send, expect nil or error
   - `SendReceiveErrorOrOK[C](commands chan C, cmd C) error` — send, close result chan on success

   These belong in an `internal/chanutil/` package.

4. **Interface-First Design** — Define the key interfaces that enable dependency injection:
   - `RemoteAPIClient` — health check, authentication, config retrieval, file upload (10 CSV types)
   - `AuthProvider` — login with username/password/MFA, session token acquisition, service account auth
   - `Uploader` — accepts a file path + CSV type + event sink, uploads asynchronously
   - `PlayerDB` — lookup by composite key, add entry, merge, save
   - `EventSink` — progress callbacks (Starting, Identified, Progress, Success, Failure)

   For each interface, briefly note what the "real" and "mock" implementations do.

5. **Dependency Flow** — How pieces wire together at startup:
   ```
   main() → parse CLI → load config → test endpoint → create Application
     → if NeedsSetup: SetupApp → (on completion) → RunningApp
     → else: RunningApp
   RunningApp owns: CSVProcessor, Uploader, PlayerDB, EventSubscriptions
   Server reads state from Application via actor commands
   ```

**Step 2: Verify actor pattern accuracy**

Load the actor-pattern skill and cross-check the template against it. Ensure the spec's description is consistent.

**Step 3: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/02-architecture.md
git commit -m "spec: add 02-architecture — state machine, actor pattern, interfaces"
```

---

### Task 3: Write 03-configuration.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/03-configuration.md`

**Step 1: Write the configuration spec**

Cover:

1. **TOML Config File** — Full schema with all fields, types, defaults, and validation rules:
   - `address` (string, default "0.0.0.0")
   - `port` (int, required, 1-65535)
   - `prefix` (string, optional URL prefix)
   - `signing-key` (string, required, JWT signing secret)
   - `service-credentials` (string, service account code)
   - `use-players-db` (bool)
   - `[org]` section: `org-playerID-pepper` (string, min 5 chars), `org-playerID-hash` (string, must be "argon2")
   - `[players-db]` section: `work-dir` (string, path)
   - `[data-processing]` section: `upload-dir`, `processing-dir`, `uploading-dir`, `archive-dir` (all string paths)
   - Include a complete example TOML file

2. **CLI Arguments** — go-flags struct with precedence rules:
   - CLI flag > TOML config > hardcoded default
   - Flags: `-v/--version`, `-c/--config-file`, `-a/--address`, `-p/--port`, `-P/--prefix`, `-s/--signing-key`
   - The config file path itself is a CLI flag (default: `./file-uploader.toml`)
   - Subcommands: `gen-csv` (synthetic CSV generator), `gen-config` (print default config)

3. **Config Validation** — Describe the `ApplicationConfig` struct and its validation:
   - `NeedsSetup()` — returns true if any required field is empty
   - `ValidateEndpoint()` — URL parse + reachability check
   - `ValidateServiceCredentials()` — authenticate and fetch remote config
   - `ValidateSettableValues()` — all fields validated, returns structured errors with per-field tags
   - `Cleanup()` — trim whitespace, lowercase where appropriate
   - Immutable value-type pattern: `WithOrgPlayerIDPepper(v)` returns new config

4. **Config Writer** — A function `func(ApplicationConfig) error` passed into the app (not a method on config). On setup completion or settings change, writes merged config back to TOML file. This is a closure over the file path and logger.

5. **Setup Wizard** — Multi-step flow implemented as an actor:
   - **Step 1: Welcome** — Display reason (if re-entering due to error) and "Get Started" button
   - **Step 2: API Endpoint** — Enter endpoint URL and environment name. Validates reachability.
   - **Step 3: Service Credentials** — Enter one-time registration code (OTP). Exchanges code for persistent service credentials via API. Validates by fetching config.
   - **Step 4: Player ID Hasher** — Enter pepper string (min 5 chars). Hash algorithm is fixed to "argon2".
   - **Step 5: Use Players DB** — Yes/No selection.
   - **Completion** — Validates entire config, writes to file, transitions Application from SetupApp → RunningApp.

   Each step supports back-navigation (returns to previous step with preserved values). Each step validates before advancing. On validation failure, re-display current step with error message.

   The wizard state machine should be a clean dispatch table — map from `(currentStep, direction)` to handler function — not nested switch statements.

6. **Directory Initialization** — On config load, all configured directories are created if they don't exist. Paths support `~` expansion via go-homedir.

**Step 2: Verify against source config**

Cross-check field names and validation rules against `~/idpair/idpair-central/configuration.go` and `app/config.go`. Ensure completeness without copying code.

**Step 3: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/03-configuration.md
git commit -m "spec: add 03-configuration — TOML config, CLI args, setup wizard"
```

---

### Task 4: Write 04-csv-pipeline.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/04-csv-pipeline.md`

**Step 1: Write the CSV pipeline spec**

This is the longest and most detailed spec. Cover:

1. **Pipeline Overview:**
   ```
   Upload CSV → Queue → Identify Type → Validate & Transform Rows → Write Output → Upload to Remote → Archive
   ```
   Files are processed one at a time (serial queue). Within a file, rows are processed in parallel by a worker pool.

2. **InColumnProcessor Interface** — The core abstraction. Each processor:
   - Declares its input columns (what it reads from the CSV)
   - Declares its output columns (what it writes to the output)
   - Has a `Process(RowData) ([]CSVOutputString, error)` method
   - May flag itself as producing a MetaID or UniqueID

   `RowData` is `{RowIndex int, RowMap map[string]string}`.

   `CSVOutputString` interface with two implementations:
   - `Quoted(s)` — escapes CSV special characters (commas, quotes, newlines)
   - `Raw(s)` — no escaping (for hashed values, computed IDs)

3. **Processor Implementations** — Describe each with its validation rules:
   - **SimpleInColumnProcessor** — Single input → single output with transform function. Factory builders for common types:
     - `NonNilFlexBool` / `NillableFlexBool` — Three-state bool (true/false/null). Accepts: true/t/yes/y, false/f/no/n, empty.
     - `NonNillableInt` / `NillableInt` — Integer parsing with optional validator (e.g., non-negative)
     - `NonNilFloat64Full` / `NillableFloat64Full` — Float with full precision (%g format)
     - `NonEmptyStringWithMax` / `NillableStringWithMax` — String with max length, returns Quoted
     - `DateAndTimeNonZeroAndNotAfterNow` — Parses 8+ date formats, validates not in future, outputs RFC3339
     - `NonNillDate` — Date-only, parses multiple formats, outputs ISO 8601 (2006-01-02)
     - `NonNillMMDDYYYY` — Strict MMDDYYYY format
     - `NonNilBirthYear` — Parses date, extracts 4-digit year
     - `NonNillableHHMMSSTime` — Time-only in HHMMSS format, outputs 15:04:05

   - **ConstOutColumnProcessor** — Produces constant value for every row (e.g., operator ID)

   - **MetaIDColumnProcessor** — Generates anonymized organization player ID:
     - Inputs: OrganizationPlayerID, OrganizationCountry, OrganizationState
     - Validates country exists, validates state if country has subdivisions
     - **Hashes: `Argon2(pepper + ":" + playerID + ":" + country + ":" + state, pepper)`** (NOTE: this differs from source — source omits country/state, which is a bug)
     - Checks PlayerDB cache first; if miss, hashes and caches
     - Outputs: MetaID (Raw), Country, State

   - **MetaIDFixedLocationColumnProcessor** — Same as MetaID but country/state are hardcoded constants, not from input columns

   - **UniqueIDProcessor** — Generates unique player identifier:
     - Inputs: LastName, FirstName, Last4SSN, DOB
     - Validates all non-empty, Last4SSN exactly 4 digits, DOB valid date
     - Name normalization applied to first/last name
     - Uses first letter of firstName only
     - Hashes: `Argon2(idPairPepper + last4SSN + firstName[0] + lastName + dob, idPairPepper)`
     - Output: UniquePlayerID (Raw)
     - Variant: `UniqueIDDefaultNullableLast4SSN` — allows missing Last4SSN (substitutes "XXXX")

   - **OrgCountryProcessor** — Validates and normalizes country/state pair

4. **Name Normalization Algorithm** — Iterative normalization (repeat until stable):
   1. Replace punctuation with space (keep hyphens; convert m-dash/n-dash to hyphen)
   2. Remove diacritical marks (Unicode NFD decomposition, strip combining characters)
   3. Replace non-ASCII (>127) with space
   4. Collapse multiple spaces to single space
   5. Normalize hyphens (remove leading/trailing, collapse multiples)
   6. Lowercase, trim whitespace

   Example: "Zoë-André O'Brien" → "zoe-andre o brien"

5. **Argon2 Hashing Parameters:**
   - Algorithm: Argon2id (via `argon2.Key`)
   - Time cost: 3 iterations
   - Memory: 32 MB (32 * 1024)
   - Parallelism: 4 threads
   - Output: 32 bytes
   - Salt: pepper bytes
   - Input: pepper + ":" + value (for org player ID) or pepper + concatenated fields (for unique ID)
   - Output encoding: standard base64

6. **Date/Time Parsing** — Must accept all these input formats:
   - ISO 8601: `2006-01-02 15:04`, `2006-1-2 15:04`
   - Excel short: `1/2/06 15:04`, `01/02/06 15:04`
   - Excel long: `1/2/2006 15:04`, `01/02/2006 15:04`
   - RFC3339 (full timestamp with timezone)
   - Date only: `2006-01-02`, `01/02/2006`, `1/2/2006`, `01/02/06`, etc.
   - Time only: `15:04`, `15:04:05`, `3:04 PM`, `03:04 PM`, `150405`
   - MMDDYYYY: `01022006`

7. **Country/State Validation:**
   - Only "US" / "USA" recognized
   - 50 states + DC: AL, AK, AZ, AR, CA, CO, CT, DE, FL, GA, HI, ID, IL, IN, IA, KS, KY, LA, ME, MD, MA, MI, MN, MS, MO, MT, NE, NV, NH, NJ, NM, NY, NC, ND, OH, OK, OR, PA, RI, SC, SD, TN, TX, UT, VT, VA, WA, WV, WI, WY, DC
   - If country has subdivisions, state is required and must be valid
   - Case-insensitive input, uppercase output

8. **All 10 CSV Types** — For each, specify:
   - CSV type name and slug
   - All input column names (exact headers the auto-detector matches)
   - All output column names
   - Which processors handle each column
   - Which columns are required vs nullable
   - Validation rules per column

   The 10 types (reference `~/idpair/example-csvs/` for sample data):

   a. **Players** (slug: `players`) — UniqueID + MetaID + OrgID constant
   b. **Bonus** (slug: `bonus`) — MetaID + Date + 3 nullable amounts
   c. **Casino** (slug: `casino`) — MetaID + 45+ session metrics (slots, tables, points)
   d. **Complaints** (slug: `complaints`) — MetaID + Date + Method + Subject
   e. **Bets** (slug: `bets`) — MetaID + 28+ sports betting fields
   f. **Demographic** (slug: `demographic`) — MetaID + BirthYear + Gender + Country + AccountOpenDate
   g. **Deposits/Withdrawals** (slug: `deposits-withdrawals`) — MetaID + Type + Date + Amount + Currency + Success + Method
   h. **Casino Players** (slug: `casino-players`) — UniqueID + MetaID + demographics + loyalty
   i. **Casino Par Sheets** (slug: `casino-par-sheet`) — Machine specs, no player ID (standalone)
   j. **Responsible Gaming** (slug: `responsible-gaming`) — MetaID + Date + Type + Period + Unit + Purpose + Value

9. **CSV Type Auto-Detection:**
   - Read first line of CSV to get headers
   - Try each CSVMetadata handler's `MatchHeaders(headers)` — all declared input columns must be present
   - Exactly one handler must match. Zero matches = error. Multiple matches = error.

10. **CSVMetadata Interface:**
    - `Type() CSVType`
    - `ColumnData() []InColumnProcessor`
    - `MatchHeaders(headers []string) bool`
    - `ProcessRow(rowdata RowData) (CSVOutputRow, error)`
    - `OutputHeaders() []string`

11. **Worker Pool Architecture:**
    - A `Processor` struct owns a work queue (buffered channel)
    - Single goroutine consumes from queue, processes files one at a time
    - Per file: fan-out to N worker goroutines for row processing
    - Input channel feeds RowData, workers call `ProcessRow`, output channel collects results
    - Writer goroutine reads output channel and writes to file
    - Cancel all workers on first row error
    - Save PlayerDB after each file completes
    - Progress reported via EventSink at throttled intervals (max once per second)

12. **Output Format:**
    - CSV with quoted headers
    - Each row is the output of ProcessRow, columns joined with commas
    - Row ordering within output file need not match input order (parallel processing)

13. **Error Handling:**
    - `RowErrors` — collects all errors for a single row, formats as "Row[N]: [err1, err2]"
    - `MultiError` — collects errors across rows, supports `TruncatedError(maxReported)` for display
    - First row error aborts entire file processing

14. **File Metadata:**
    - `FileMetadata` — original filename, upload timestamp, uploader username, local file path
    - `OutFileMetadata` — adds: hashers ref, start/finish time, output headers, CSV type, output path
    - `ProgressRecord` — `{TotalRows, CurrentRow}` with `Progress() float64`

**Step 2: Cross-check column mappings**

For each of the 10 CSV types, read the corresponding file in `~/idpair/idpair-central/csv/columnmapping/` and verify all input/output columns and validation rules are captured. This is the most error-prone part of the spec.

**Step 3: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/04-csv-pipeline.md
git commit -m "spec: add 04-csv-pipeline — 10 CSV types, processors, hashing, validation"
```

---

### Task 5: Write 05-web-ui.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/05-web-ui.md`

**Step 1: Write the web UI spec**

Cover:

1. **Route Table** — All routes with method, handler, auth requirement:
   - `GET /` — Dashboard (auth required)
   - `GET,POST /login` — Login page
   - `GET /logout` — Clear session, redirect to login
   - `POST /upload` — CSV file upload (auth required)
   - `GET /events` — SSE endpoint (auth required)
   - `GET /api/extend` — Extend JWT session
   - `GET /failure-details/{record-id}` — Failure details modal content
   - `GET,POST /settings` — Settings page (auth required)
   - `GET /health` — Health check (no auth)
   - `GET /players-db` — Players DB page (auth required)
   - `GET /download-players-db` — Download players DB file
   - `GET /archived` — Archive page (auth required)
   - `POST /search-archived` — Search/filter archived files
   - `GET /setup` — Setup wizard (no auth)
   - `POST /setup/{action}` — Setup wizard navigation
   - Static: `/js/*`, `/css/*`, `/img/*`, `/favicon.ico`

2. **Authentication Flow:**
   - `AuthProvider` interface with methods: `Login(username, password, mfaToken) (SessionToken, error)`, `ServiceAccountLogin(code) (ServiceCredentials, error)`, `ConsumeRegistrationCode(endpoint, code) (APIClient, error)`, `MFARequired() bool`
   - JWT: HS256, 5-minute expiry, claims: `{username, orgID, exp}`
   - Cookies: `session` (HttpOnly, SameSite=Strict) + `session-expires` (readable by JS)
   - Auto-extend: on user activity (mousedown, keydown, input, scroll), 60-second cooldown, only within 40-second window before expiry
   - Session check: JS polls every 1 second, shows modal 30 seconds before expiry
   - Modal has: Extend, Logout, Dismiss buttons

3. **Dashboard Page:**
   - File drop zone: drag-and-drop CSV upload with visual feedback
   - Upload validation: CSV files only, max 50MB
   - Four live-updating sections via SSE:
     - Queued files table (file, uploaded by, timestamp)
     - Processing file (current file, CSV type, progress bar with percentage)
     - Uploading files table (file, type, progress %)
     - Recently finished (last 5, with success/failure coloring)
   - Failure rows are clickable → shows modal with failure reason

4. **SSE Implementation:**
   - Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
   - Client subscribes via `RunningApp.Subscribe()`, gets event channel
   - 4 event names: `queued`, `processing`, `uploading`, `recently-finished`
   - Each event renders a templ component to HTML string, sends as SSE `data:`
   - Client uses htmx SSE extension: `hx-ext="sse"`, `sse-connect`, `sse-swap`
   - Reconnection with exponential backoff (1s to 128s max)
   - Connection error modal on disconnect

5. **Archive Page:**
   - Search filters: status dropdown (All/Success/Failure), type dropdown (all 10 CSV types), free-text search
   - HTMX POST to `/search-archived` with debounce on text input
   - Results table: file, type, status, uploaded by, processed at, uploaded at, failure phase
   - Success rows: green background. Failure rows: red background, clickable for details.

6. **Settings Page:**
   - Sections: Endpoint config (name + URL), Service credentials (registration code), Player ID hasher (pepper)
   - Validation errors displayed inline per field
   - Save writes config and may trigger app restart
   - Uses `ApplicationConfig.ValidateSettableValues()` from 03-configuration spec

7. **Players DB Page:**
   - Lists available player databases
   - Shows active DB with "ACTIVE" badge
   - Download link for each DB

8. **Login Page:**
   - Username/password fields
   - Conditional MFA field (shown when `AuthProvider.MFARequired()` returns true)
   - Error message display
   - HTMX POST to `/login`

9. **Custom CSS Approach:**
   - `tokens.css`: CSS custom properties for colors, spacing, typography, border-radius, shadows
   - Responsive breakpoints: mobile (<768px), tablet (768-1024px), desktop (>1024px)
   - Utility classes for flex, grid, spacing, text alignment
   - Component styles: drop-zone, progress-bar, modal, table (sticky headers, scrollable), status badges
   - Color coding: success (#d4edda), failure (#f8d7da)
   - Animations: fade-out for success messages (5s)

10. **Custom JavaScript:**
    - `app.js`: session management, file upload, timestamp formatting, cookie helpers
    - `sse.js`: htmx SSE extension with exponential backoff reconnection
    - No build step — plain JS, vendored alongside htmx

11. **State-Aware Routing:**
    - Handlers check application state before rendering:
      - If `ErrorApp` → show error page
      - If `SetupApp` → redirect to `/setup`
      - If `RunningApp` → render requested page
    - Helper wrappers: `withRunningState()`, `withSetupApp()`, `withStateAndSession()`

**Step 2: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/05-web-ui.md
git commit -m "spec: add 05-web-ui — routes, auth, SSE, dashboard, CSS approach"
```

---

### Task 6: Write 06-players-db.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/06-players-db.md`

**Step 1: Write the players DB spec**

Cover:

1. **Purpose** — Optional cache of `orgPlayerID → MetaID` mappings. Avoids re-hashing the same player ID across multiple CSV uploads. When disabled, a DevNull implementation silently discards all operations.

2. **Data Model:**
   - `Entry`: MetaID, OrganizationPlayerID, Country, State
   - Composite lookup key: `"orgPlayerID:country:state"` (all lowercased)
   - In-memory index: `map[string]int` mapping lookup key → entry slice index

3. **File Format — TOML Header + CSV Body:**
   ```
   organization-player-hash = "argon2"
   organization-player-id-pepper-hash = "sha256:abc123..."

   --- END OF HEADER ---
   meta_id,organization_player_id,country,state
   base64hash1,player001,us,ma
   base64hash2,player002,us,ny
   ```
   - Header: TOML with hash algorithm name and SHA-256 of pepper (not raw pepper)
   - Delimiter: `--- END OF HEADER ---\n`
   - Body: Standard CSV with headers
   - On load: compute SHA-256 of configured pepper, compare to header value. Mismatch = error.
   - If file doesn't exist: create empty DB with correct header.

4. **PlayerDB Interface:**
   ```go
   type PlayerDB interface {
       GetPlayerByOrgPlayerID(orgPlayerID, country, state string) (Entry, bool)
       AddEntry(metaID, orgPlayerID, country, state string)
       Merge(other PlayerDB)
       Header() DBHeader
   }
   ```

5. **ConcurrentPlayerDB** — Actor-pattern wrapper:
   - Command types: `lookup` (returns Entry + bool), `add` (fire-and-forget)
   - Buffered command channel (capacity 1000)
   - Single goroutine owns the underlying PlayerDB
   - `Close()` stops goroutine and waits

6. **Deduplication Logic:**
   - On `AddEntry`: if key exists and existing MetaID is empty but new MetaID is non-empty, update it. Otherwise skip (first write wins).

7. **SaveDB** — Called after each CSV file completes processing. Writes entire DB to file using atomic write (temp file + rename).

8. **Directory Naming** — `playersdb-{algorithm}-{base64url(pepper)}` under the configured work directory. File is always `players.db` within that directory.

9. **DevNull Implementation** — `DevNullDB` singleton: `GetPlayerByOrgPlayerID` always returns `(Entry{}, false)`, all other methods are no-ops.

10. **Snapshot/Download** — For the web UI download feature: copy current `players.db` to a timestamped snapshot file, serve that file. Format: `players-YYYYMMdd_HHmmss.nnnnnnnnn.db`.

**Step 2: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/06-players-db.md
git commit -m "spec: add 06-players-db — format, dedup, concurrent access, DevNull"
```

---

### Task 7: Write 07-testing.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/07-testing.md`

**Step 1: Write the testing spec**

Cover:

1. **Mock Implementations:**
   - **MockRemoteAPIClient** — In-memory. `UploadFile` writes to a configurable output directory. `GetConfig` returns static config. `TestEndpoint` always succeeds. `Login` accepts any credentials and returns a fixed token.
   - **MockAuthProvider** — Accepts any username/password, returns valid session. `MFARequired()` returns false. `ConsumeRegistrationCode` returns hardcoded service credentials.
   - **MockUploader** — Copies file to output directory, fires EventSink callbacks (Starting → Progress → Success). Configurable: can simulate failures.
   - All mocks should be selectable via a CLI flag or config option (e.g., `--mock` flag or a `[testing]` section in TOML).

2. **Synthetic CSV Generator** — Both library and CLI subcommand:
   - CLI: `file-uploader gen-csv --type <slug> --rows <N> [--output <path>] [--inject-errors] [--error-rate <0.0-1.0>] [--seed <int>]`
   - Supports all 10 CSV types
   - Generates realistic fake data:
     - Names: mix of common names with diacritics, hyphens, apostrophes
     - Dates: within last 5 years, various formats
     - Amounts: realistic ranges for gaming (bets, deposits, etc.)
     - Countries/states: random US states
     - Player IDs: sequential with prefix
   - Error injection modes:
     - Missing required columns (drop random headers)
     - Empty required fields
     - Invalid types (string where number expected)
     - Future dates where past required
     - Invalid country/state combinations
   - Deterministic: same seed produces same output (for reproducible tests)
   - Library API: `func GenerateCSV(csvType CSVType, rows int, opts ...Option) ([]byte, error)`

3. **Unit Tests** — Table-driven, using `t.TempDir()` for isolation:
   - Column processors: each type with valid input, edge cases, and expected errors
   - Name normalization: diacritics, hyphens, punctuation, whitespace, Unicode
   - Date parsing: all supported formats, invalid dates, future dates
   - FlexBool: all valid inputs (true/t/yes/y, false/f/no/n, empty)
   - Config validation: missing fields, invalid values, NeedsSetup logic
   - PlayerDB: add, lookup, dedup, merge, file round-trip, pepper mismatch detection
   - Argon2 hashing: deterministic output for known inputs

4. **Integration Tests:**
   - CSV pipeline end-to-end: upload file → process → verify output columns are correct and PII is hashed
   - Setup wizard: step through all states, verify config is written
   - File upload → queue → process → archive: verify metadata files are created at each stage
   - SSE: subscribe, upload file, verify events received in order
   - Settings: change config via HTTP, verify persistence

5. **Test Data:**
   - Reference the example CSVs at `~/idpair/example-csvs/` — these can be used for integration testing
   - The synthetic CSV generator should also be used to create test fixtures programmatically

6. **Running Tests:**
   ```bash
   ./build.sh -g -G -b   # Build first
   go test ./...          # All tests
   go test ./internal/csv/... -run TestNameProcessor -v  # Single test
   ```

**Step 2: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/07-testing.md
git commit -m "spec: add 07-testing — mocks, synthetic CSV generator, test strategy"
```

---

### Task 8: Write 08-build-and-deploy.md

**Files:**
- Create: `~/projects/file-uploader/build-specification/08-build-and-deploy.md`

**Step 1: Write the build spec**

Cover:

1. **build.sh** — Central build script. Flags:
   - `-b` — Build the binary (`go build` with ldflags)
   - `-g` — Run `go generate` (for stringer/enum changes)
   - `-G` — Run `templ generate` (for .templ file changes)
   - `-C` — Clean build artifacts
   - `-g -G -b` — Safe default for any change
   - Auto-installs `stringer` and `templ` if missing

2. **Version Metadata via ldflags:**
   - `GitSHA` — Current commit hash
   - `GitVersion` — Git tag or describe output
   - `GitFullVersionDescription` — `git describe --long`
   - `GitDescribeVersion` — `git describe --tags`
   - `GitLastCommitDate` — Date of last commit
   - `DirtyBuild` — "true" if working tree is dirty
   - `SnapshotBuild` — "true" if no tag
   - These live in a `settables.go` file with package-level `var` declarations
   - `PrintVersion()` function outputs all values
   - `-v/--version` flag triggers PrintVersion and exits

3. **Static Asset Embedding:**
   - `go:embed` directives for CSS, JS, images in the server package
   - All static files served via chi router: `/js/*`, `/css/*`, `/img/*`
   - Favicon served at `/favicon.ico`

4. **Development with Air:**
   - Air config file (`.air.toml`) for hot reload
   - Watches: `.go`, `.templ`, `.css`, `.js` files
   - Build command: `./build.sh -G -b`
   - Exclude: `tmp/`, `vendor/`, `node_modules/`

5. **Docker Build:**
   - Multi-stage Dockerfile: builder stage (Go build) + runtime stage (scratch or distroless)
   - Build args for API endpoint, signing key
   - Single binary + no external dependencies = minimal image

6. **Systemd Service:**
   - Template unit file for production deployment
   - Working directory, config file path, restart policy
   - Install script that creates user, sets up directories, installs service

**Step 2: Commit**

```bash
cd ~/projects/file-uploader
git add build-specification/08-build-and-deploy.md
git commit -m "spec: add 08-build-and-deploy — build.sh, ldflags, embed, Docker, systemd"
```

---

### Task 9: Write file-uploader README.md

**Files:**
- Create: `~/projects/file-uploader/README.md`

**Step 1: Write the builder agent README**

This is the first thing the builder agent reads. It must:

1. **Orient the agent** — "You are building a Go web application from specifications. Read `build-specification/` files in order (01 through 08)."

2. **State the tech stack** — Brief list with import paths

3. **Establish the build command** — "Always use `./build.sh`. Never invoke `go build`, `go test`, `templ generate` directly."

4. **Point to UAT feedback** — "Check `uat-feedback/` for automated test results. Address any issues found there before considering your work complete."

5. **State the actor-pattern requirement** — "For all shared mutable state accessed by HTTP handlers, use the goroutine actor pattern. Load the `actor-pattern` skill for guidance."

6. **List the example CSVs** — "Reference files at `~/idpair/example-csvs/` for sample data across all supported CSV types."

7. **State key constraints:**
   - No database — file-based storage only
   - Single static binary with embedded assets
   - Interface-first design — every external dependency behind an interface
   - No CSS frameworks — custom design tokens + utility classes

**Step 2: Commit**

```bash
cd ~/projects/file-uploader
git add README.md
git commit -m "Add builder agent README"
```

---

### Task 10: Write file-uploader-uat README.md

**Files:**
- Create: `~/projects/file-uploader-uat/README.md`

**Step 1: Write the UAT agent README**

This orients the UAT agent:

1. **Purpose** — "You are independently testing a Go web application built by another agent. You have no access to the build specifications — you test the running application."

2. **What to test** — High-level categories:
   - Build: `./build.sh -g -G -b` produces a single binary
   - Setup wizard: fresh start, all steps, validation, back-nav, config persistence
   - CSV upload and processing: all 10 types, invalid files, progress tracking
   - Web UI: login, dashboard, archive, settings, players DB
   - Synthetic CSV generator: `gen-csv` subcommand works
   - Mock mode: app runs fully without real API
   - SSE: real-time updates during processing

3. **How to report** — "Write test results as Markdown files in `~/projects/file-uploader/uat-feedback/`. Each file should describe: what was tested, expected behavior, actual behavior, pass/fail."

4. **Test approach** — Build the app, run it with `--mock` flag, exercise via HTTP requests or browser automation.

5. **Example CSV files** — "Use `~/idpair/example-csvs/` for test data, and the `gen-csv` subcommand for synthetic data."

**Step 2: Commit**

```bash
cd ~/projects/file-uploader-uat
git add README.md
git commit -m "Add UAT agent README"
```

---

### Task 11: Push all repos and final review

**Step 1: Push file-uploader**
```bash
cd ~/projects/file-uploader
git push
```

**Step 2: Push file-uploader-uat**
```bash
cd ~/projects/file-uploader-uat
git push
```

**Step 3: Push flywheel**
```bash
cd ~/projects/flywheel
git push
```

**Step 4: Final review**

Read through all specs one more time and verify:
- No source code or source project references leaked
- All 10 CSV types are fully specified
- All interfaces are defined
- Acceptance criteria are testable
- Cross-references between specs are consistent
