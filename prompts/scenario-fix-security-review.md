## Scenario: Fix Security Review Findings

The security reviewer found issues. Security issues are non-negotiable — fix them all.

1. Read ALL findings in `security-review/` directory
2. Fix every finding — pay special attention to: PII leaks, file cleanup, token handling, injection vectors, session management
3. For substantial fixes, use sub-agents to keep your context window fresh
4. After fixing, verify: `./build.sh -g -G -b` succeeds, `./build.sh -t` passes
5. Smoke-test affected features with curl and/or Playwright
6. Update `progress` if relevant
7. Commit and push

Your scenario name for `builder_done` is: `Fix Security Review`
