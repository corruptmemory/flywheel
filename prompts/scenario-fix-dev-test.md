## Scenario: Fix Dev Test Failures

The dev tester found failures. Fix them without breaking what already works.

1. Read ALL results in `dev-test-results/` directory
2. Fix every failure — watch for regressions, do not break working specs while fixing the current one
3. The dev tester is adversarial and cites specs — verify claims against specs before dismissing
4. For substantial fixes, use sub-agents to keep your context window fresh
5. After fixing, re-run ALL tests (`./build.sh -t`), not just the failing ones
6. Smoke-test affected features with curl and/or Playwright
7. Update `progress` if relevant
8. Commit and push

Your scenario name for `builder_done` is: `Fix Dev Test`
