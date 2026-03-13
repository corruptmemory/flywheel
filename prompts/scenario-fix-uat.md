## Scenario: Fix UAT Failures

The UAT tester found failures. UAT is the final gate before acceptance.

1. Read ALL feedback in `uat-feedback/` directory
2. UAT cites specs — verify claims against the actual specs before dismissing
3. UAT has an automated test harness — understand what the tests actually check
4. You may need to fix multiple unrelated issues across different specs
5. For substantial fixes, use sub-agents to keep your context window fresh
6. After fixing, re-run the full test suite (`./build.sh -t`)
7. Smoke-test affected features with curl and/or Playwright
8. Update `progress` if relevant
9. Commit and push

Your scenario name for `builder_done` is: `Fix UAT`
