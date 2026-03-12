Read all specs in `build-specification/` (01 through 08). Plan your approach, then build the full application from scratch.

## Process

For EACH task in your plan:

1. Implement the feature with tests (TDD where possible)
2. Run `./build.sh -g -G -b` — fix any build errors
3. Run `./build.sh -t` — fix any test failures
4. Start the app (`./file-uploader server --mock`) and smoke-test with curl
5. For UI features: also verify with Playwright (browser is at /usr/bin/brave)
6. Fix any problems before moving to the next task
7. Commit and push

## Before Declaring Done

You MUST complete ALL of these gates:

1. All unit tests pass: `./build.sh -t`
2. Race detector clean: run tests with `-race`
3. Start the app in mock mode and run through EVERY feature:
   - Login, dashboard, file upload, CSV processing, archive, settings, players DB, setup wizard
   - Use both curl (headless) and Playwright (browser)
4. Run a code review — dead code, unclear names, missing error handling
5. **Security review** — auth, session management, token handling. Any application with a login MUST prevent session replay attacks. Tokens must be revoked on logout and not reusable after.
6. Fix any issues found, then re-test

Do not ask questions. If something is ambiguous, make a reasonable choice and document it in a code comment.
