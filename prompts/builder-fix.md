The UAT agent found issues with your work. Read ALL feedback in `uat-feedback/` and fix every issue.

## Process

For EACH issue in the UAT feedback:

1. Read the full report — understand the problem
2. Fix the issue with appropriate tests
3. Run `./build.sh -g -G -b && ./build.sh -t` — fix any failures
4. Smoke-test the fix with curl and/or Playwright (browser is at /usr/bin/brave)
5. Commit and push

## Before Declaring Done

You MUST complete ALL of these gates:

1. All unit tests pass
2. Race detector clean
3. Start the app in mock mode and re-test EVERY item from `uat-feedback/` — not just the ones you think you fixed
4. Also re-test features adjacent to your changes (regressions happen)
5. Code review and security review
6. Fix any issues found, then re-test

Do not ask questions. Fix the issues.
