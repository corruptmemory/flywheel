# Ideas for Round 3

I've got a good sense of what round 3 will look like.

The basic problem I see (and this problem was there in round 1 as
well) is that the builder is simply too incentivized to
cut-corners. This seems to be due to "agent context pressure." So,
let's remove some of that pressure. The basic flow will look like:

1. Build - builds the code AND still does _at least_ smoke tests. But
   unlike today where it goes from spec 1 to spec 15 it "hands off" to
   steps 2-4 after each spec is implemented. Instead of `done` it
   writes a file `builder_done`, and it's not just a touch file, it's
   a "progressive checklist". Something like:
```
Agent: Builder
Spec status:
- 01-project-scaffold.md: DONE
- 02-chanutil-and-interfaces.md: DONE
- 03-configuration.md: JUST COMPLETED
- 04-hashing-and-normalization.md
- 05-csv-framework.md
- 06-csv-type-definitions.md
- 07-worker-pool.md
- 08-player-db.md
- 09-csv-generator.md
- 10-application-state-machine.md
- 11-auth-and-server.md
- 12-setup-wizard.md
- 13-dashboard-and-sse.md
- 14-archive-settings-playersdb.md
- 15-build-and-deploy.md
```

  NOTE: the `builder_done` file is ephemeral and gets deleted every
  time the "builder" phase is started. A SEPARATE `progress` file will
  be needed by the builder agent that contains _at least_ the same
  information as what is put into the `builder_done` file. The
  `progress` file should contain any/all information needed for the
  builder to continue to the next spec to build.

  Prompt states:
   - Init
   - Respond to code review
   - Respond to security review
   - Respond to dev test
   - Respond to UAT
   - Build next spec
2. Code review -- does a thorough code review, this includes:
   - Bad/unclear code
   - Style
   - Comments/documentation needed
   - CLI argument conventions verified
   - Divergence from spec
   
   As you can guess, this agent looks at the `builder_done` file to
   determine what are the appropriate actions to take.
   
   The code review agent writes thorough issue results into the
   `code-review` directory under the builder project with all of its
   findings. Then writes a `code_review_done` file with a "PASS/FAIL"
   value. If PASS continue to stage 3 otherwise go back to stage 1 and
   prompt with "Respond to code review" prompt.
   
   Just like the UAT: this agent is encouraged to write automation
   tooling whenever possible to get it out of the business of
   "thinking work"
3. Security review -- does a thorough security review, this includes:
   - Ensuring no PII is leaked to output
   - Intermediate/uploaded/processed files are cleaned up (we don't
     keep CSV files, only JSON files with "metadata") -- we don't want
     this system to be a potential attack vector.
   - Execute replay attacks
   - Validate secure JWT handling
   - Look for pathways where "problem CSV" can poison or otherwise
     crash the server and its outputs
   - Divergence from spec where security concerns are explicitly
     indicated.

   As you can guess, this agent looks at the `builder_done` file to
   determine what are the appropriate actions to take.

   The security review agent writes thorough issue results into the
   `security-review` directory under the builder project with all of its
   findings. Then writes a `security_review_done` file with a "PASS/FAIL"
   value. If PASS continue to stage 4 otherwise go back to stage 1 and
   prompt with "Respond to security review" prompt.

   Just like the UAT: this agent is encouraged to write automation
   tooling whenever possible to get it out of the business of
   "thinking work"
4. Dev test -- to date the "builder" has been remarkably lax in doing
   testing by itself. Whatever the reason is we can't let UAT discover
   things like "whole subsystems are missing." This would never fly if
   humans were doing the work. So, we're dedicating an entire cycle
   only to "incremental testing".
   - Dev testing is just as "adversarial" as UAT testing -- no
     bullshit allowed. Examine and cite specs where needed.
   - Using the automated tooling it incrementally builds run all
     regression tests against all "claimed done" specs
   - Smoke test anything/everything it can
   - Once UI-capable elements are avilable to test, test all such
   features via curl AND Playwright. Add to automated tests any
   detected failure modes.
   
   As you can guess, this agent looks at the `builder_done` file to
   determine what are the appropriate actions to take.

   The dev test agent writes thorough issue results into the
   `dev-test-results` directory under the builder project with all of its
   findings. Then writes a `dev_test_done` file with a "PASS/FAIL/UAT READY"
   value.
   - PASS: Go to step 1 with the "Build next spec" prompt
   - FAIL: Go to step 1 with the "Respond to dev test" prompt
   - UAT READY: this means that all specs are complete and have passed all reviews and the "dev test" tests. Continue to step 5
   
   Just like the UAT: this agent is encouraged to write automation
   tooling whenever possible to get it out of the business of
   "thinking work"
5. UAT testing -- this is our exiting UAT procedure.

As you can guess, this machinery (which is now a real state machine)
will have to be incorporated into the "flywheel" driver logic

---

Other things to note:
- All "clauds" should be using "maximum" effort -- no slackers
- I REALLY like the new UI aesthetic, can we capture that and
  incorporate it into the spec so that future rounds have a consistent
  look?
