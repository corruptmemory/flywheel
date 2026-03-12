# Feedback Round 1

Unfortunately the "builder agent" in `file-uploader` didn't create a commit after each
"round" with UAT, but in total it took about 10-12 "rounds" before UAT was accepted. Your
initial estimate is that it would take 2-3.

## Builder Improvements

* The builder should not EVER declare "done" without running the application and testing
  for itself what UAT is looking for. No competent human developer would ever hand off an
  application without running and testing it for themselves. This alone would have done
  wonders to reduce the UAT "loop" count. This includes "headless testing" via `curl` and
  running everything via `Playwright`. The builder has access to the acceptance criteria,
  it should use that and work hard to get UAT done in one try.
  
  This work should be done in a loop-like fashion:
       1. Work through the "task list"
	   2. Fire up the app BOTH headless with `curl` AND `Playwright`. Check the UAT
          criteria and verify things work. If not, formulate a plan to fix and goto step 1.
	   3. Only if the "builder" has confidence with the work product does it indicate UAT
          can happen. --> No chef let's their patrons discover if the soup is good or bad,
          the chef tastes the soup before the customers.
  
* The builder should _incrementally, and during development_ when ever possible, run the
  application and exercise the UI features. These "incremental" tests don't need to be
  comprehensive, but quick focused "smoke tests" of what was just worked on. If there are
  problems already, fix in a loop and then continue. Don't let problems accumulate until
  the end.

* The builder must always run code reviews and SECURITY REVIEWS at then end of every task
  list. All issues must be addressed before "done" can be declared. NOTE: the UAT tester
  discovered a session "replay" vulnerability and the "builder" tried to cover it up as
  "it's sessionless -- no biggie". There is absolutely no justification for this: ANY
  program with a login cannot allow session replay attacks of any kind. (MIND YOU, THE
  EXISTING APPLICATION, `idpair-central`, HAS THIS BUG!)

* As already indicated, the "task list" needs "context size mindfulness."

## UAT Improvements

* The UAT agent needs to be _mildly adversarial._ It should push back against _most_ lazy
  choices made by the "builder."

* The existing UAT agent was remarkably inconsistent and forgetful. Multiple times I had
  to prompt it to do certain tests. This indicates that the UAT inputs need more explicit
  direction.
