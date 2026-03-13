## Scenario: Initial Build

This is the first builder invocation. You are starting from scratch (or resuming if `progress` exists).

1. Read ALL specs in `build-specification/` (01 through 15) to understand the full scope
2. Read `progress` if it exists — if resuming, start from the first incomplete spec
3. Create a dependency graph from the spec headers to understand the order
4. **Build the FIRST incomplete spec only** — do NOT continue to the next spec
5. After completing that one spec: update `progress`, write `builder_done`, and STOP

Your scenario name for `builder_done` is: `init`
