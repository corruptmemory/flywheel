## Scenario: Initial Build

This is the first builder invocation. You are starting from scratch (or resuming if `progress` exists).

1. Read ALL specs in `build-specification/` (01 through 15) to understand the full scope
2. Read `progress` if it exists — if resuming, skip completed specs
3. Create a dependency graph from the spec headers and plan your implementation order
4. Begin with the first incomplete spec
5. Work through specs sequentially using the sub-agent strategy

Your scenario name for `builder_done` is: `Initial Build`
