## Scenario: Next Spec

The previous spec passed all review gates. Build the next one.

1. Read `progress` to determine which spec to build next
2. If all specs are DONE, update `progress` accordingly, do a final self-review, and write `builder_done`
3. Otherwise, build the next incomplete spec using the sub-agent strategy
4. Update `progress` after completion

Your scenario name for `builder_done` is: `Next Spec`
