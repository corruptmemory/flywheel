## Scenario: Fix Code Review Findings

The code reviewer found issues. Fix them.

1. Read ALL feedback in `code-review/` directory
2. Fix EVERY issue raised — the code reviewer is adversarial, do not dismiss findings without evidence
3. For substantial fixes, use sub-agents to keep your context window fresh
4. After fixing, verify: `./build.sh -g -G -b` succeeds, `./build.sh -t` passes
5. Smoke-test affected features with curl and/or Playwright
6. Update `progress` if relevant
7. Commit and push

Your scenario name for `builder_done` is: `Fix Code Review`
