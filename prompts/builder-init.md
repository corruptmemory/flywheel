Read all specs in `build-specification/` (01 through 15). Plan your approach, then build the full application.

## Sub-Agent Implementation Strategy

**CRITICAL:** Do NOT try to implement all 15 specs in your own context window. You will run out of context and start stubbing things out. Instead:

1. Read all specs and create a high-level implementation plan
2. For EACH spec, spawn a sub-agent (using the Agent tool) to implement it:
   - Pass the sub-agent the spec file path and its declared dependencies
   - The sub-agent reads the spec, implements it, writes tests, and verifies the build compiles
   - Each sub-agent gets a fresh context window — this is why the specs were designed to be self-contained
3. After each sub-agent completes, YOU verify:
   - `./build.sh -g -G -b` succeeds
   - `./build.sh -t` passes
   - Quick smoke-test with curl if applicable
4. Fix any integration issues between specs before moving to the next
5. Commit and push after each spec

The specs have explicit dependency declarations at the top (e.g., "Dependencies: 03-configuration.md (ApplicationConfig)"). The sub-agent should read those referenced specs to understand the interfaces it depends on.

## Process

For EACH spec in order:

1. Spawn a sub-agent to implement the spec with tests
2. After the sub-agent finishes: build, test, verify
3. Start the app (`./file-uploader --mock -p 8080`) and smoke-test with curl
4. For UI features: also verify with Playwright (browser is at /usr/bin/brave)
5. Fix any problems before moving to the next spec
6. Commit and push

## Before Declaring Done

You MUST complete ALL of these gates:

1. All unit tests pass: `./build.sh -t`
2. Race detector clean: tests run with `-race` (build.sh -t already includes it)
3. Start the app in mock mode and run through EVERY feature:
   - Login, dashboard, file upload, CSV processing, archive, settings, players DB, setup wizard
   - Use both curl (headless) and Playwright (browser)
4. Run a code review — dead code, unclear names, missing error handling
5. **Security review** — auth, session management, token handling. Any application with a login MUST prevent session replay attacks. Tokens must be revoked on logout and not reusable after.
6. Fix any issues found, then re-test

Do not ask questions. If something is ambiguous, make a reasonable choice and document it in a code comment.
