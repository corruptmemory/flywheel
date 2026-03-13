## Scenario: Next Spec

The previous spec passed all review gates (code review, security review, dev test). Now build the next one.

1. Read `progress` to determine which spec to build next
2. If ALL 15 specs are DONE, do a final self-review, then write `builder_done` with all specs as DONE
3. Otherwise, **build the ONE next incomplete spec only** — do NOT continue beyond it
4. After completing that one spec: update `progress`, write `builder_done`, and STOP

Your scenario name for `builder_done` is: `next-spec`
