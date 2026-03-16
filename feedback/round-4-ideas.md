# Round 4 Ideas

While contemplating these ideas, keep
~/projects/lightweight-containers in mind.  At some point we need to
come to a decision:

- Absorb the code from that project into this project and shape it
  into what we need
- Maintain it as an external library and apply updates there as needed

I'm not entirely sure which way to go, but perhaps after discussing
things a clear winner will emerge.

## The New State Machine

### The Roles

- Product Owner (PO):
  - Can read the source code of the evolving system
  - Read/Write access to the product specification
  - Read incoming requests from agents, writes replies
  - Represents the "needs and wants" of potential or actual
    customers. Is motivated to do what's best for those customers.
  - Maintains a firm attitude towards keeping the specification "as is", unless persuaded by:
    - Sound argument in support of a needed change (e.g.: small change
      to increase security profile, layout/placement/treatment change
      that is clearer to users, etc)
    - Convincing evidence that something cannot be "practically
      accomplished" in a way that complies with the
      specification. (e.g.: a clear example of mutually contradictory
      specifications, "magical thinking" kinds of ill formed,
      ambiguous, or unrealistic specifications: "and then the program
      will read the mind of the user and...", etc.)
  - Adds specification amendment files named NNNN-<spec name>-amendment-NNN.md
    - Specification amendments accumulate
    - Specification amendments cite the files and lines were the
      amendment applies
    - Specification amendments can amend other amendments
    - Amendments with a higher "suffix number" supersede amendments
      with a lower amendment number.
  - A single agent request that requests a change to the specification
    (providing evidence and argument) that is ACCEPTED by the PO can
    result in amendments to 1 or more specifications. It is the
    responsibility of the PO to consider the implications of a change
    across the entire specification set.
  - A PO MUST respond to every request. If the request is DENIED a
    full reason must be given. If the request is ACCEPTED a FULL,
    COMPLETE, AND SELF-CONTAINED reply must be constructed: remember,
    agent's cannot see updated specifications during their turn, so
    the replies must be self-contained and complete.
  - Example request types:
    - PLEASE CLARIFY: a request to clarify one or more
      specifications. The product owner can reply in any/all of: text,
      example code, pseudo code, etc. A clarification can (and should)
      be added as an amendment for all to see.
    - POTENTIAL SPEC CONFLICT/AMBIGUITY: an agent has come to the
      conclusion that there is no obvious way to interpret a set of
      specifications/requirements coherently. IF the PO agrees with
      the claim then the PO's job is to amend the specification in
      whatever way is necessary to resolve the conflict/ambiguity. The
      mental framing of the PO while doing so is "what is in the best
      interest of the customer?" while also keeping to the "spirit" of
      the product in question. It is absolutely forbidden to divert
      substantially from the initial product vision. You can't start
      out trying to build "customer address book" and wind up building
      Excel. The smallest practical change needed to resolve the
      conflict/ambiguity needs to be the one we go with.
      
      IF the PO disagrees with the request then almost certainly 2
      actions will be needed:
        1. A set of "clarification amendments" must be produced
        2. The response has to have a self-contained, fully elaborated
           clarification as to why the conflict/ambiguity doesn't
           exist. The constraint is that amendments to the spec are
           not seen "intra-round."
    - SUGGESTED IMPROVEMENT: an agent has identified an opportunity to
      improve the product w.r.t. the customer or customer
      experience. The PO can ACCEPT or REJECT all or part of the
      suggestion. If ACCEPTED then the PO MUST update all the relevant
      portions of the specification via the amendment process.
  - Cannot change code

- Builder (B):
  - The only one who can change "application" or "library" code
  - Firmly guided by the specification. Feedback from agents will be
    cross-checked against specifications. This said, it will actively
    asses specifications for:
    - Clear ambiguities/conflicts: when found will iterate with PO to
      find a solution
    - If B observes an opportunity to build a _better product_
      (relative to customer needs and UX) but is not captured in the
      specifications, it should suggest the improvement to the PO.
  - IF there is actionable feedback from agents and UAT, then B is
    obligated to respond to feedback from reviewer and UAT.  No
    further specifications will be worked on until all reviewer and
    UAT agents indicate `PASS`.
    1. At the beginning of B's "turn" make a comprehensive, durable,
       checklist of feedback item to be addressed in this turn.
    2. For each item in the checklist:
      - IF the feedback is not in conflict with the specification (as
        known to B at the time it's started) then comply with changes
        to the code base indicated/implied by the feedback. Check off
        the item.
      - IF the feedback would result in non-compliance with the
        specification: Consult the product owner with a POTENTIAL SPEC
        CONFLICT/AMBIGUITY request. Cite the specifications
        implicated, and B's description of why there is an
        issue. Provide the content of the "feedback" items (what agent
        provided the feedback, the details of the particular feedback
        item). 
          - If other work can be accomplished while it awaits a
            response, continue to other items.  Do not check off the
            feedback item.
          - If other work cannot be accomplished await a response.
    3. B's turn does not end until all feedback items have been
       checked off. For items that needed PO involvement, how the item
       gets addressed depends on the PO's response.
       - PO clarifies: 
         - Clarification enables progress: use the details of the
           clarification to proceed. Complete the item and check it
           off the list.
         - Clarification does not enable progress: Interact with the
           PO again as needed.
       - PO alters specification:
         - Altered specification enables progress: use the details of
           the reply to complete the item. Check it off the list.
         - Altered specification does not enable progress: Interact
           with the PO again as needed.
       - PO denies:
         - The B needs to build out a given feature the best it
           can. Perhaps the PO's response includes information useful
           towards that goal.

- Reviewers (R), which includes QA, Code Review, and Security Review
  (possibly more in the future):
  - Can read the source code of the evolving system
  - Read access to the product specification
  - Read/Write access to "private tooling" directory
  - Required tooling includes comprehensive automated tests
  - Each reviewer focuses on a particular specialty
  - Ambiguities/conflicts detected with the specification require
    interaction with the PO.
  - Adversarial to the builder when it comes to spec non-compliance
    and incomplete or missing work. Automatic FAIL.
  - Should be an advocate for the "customer's perspective."
    Specifically answering the question "If I were the customer would
    I accept the software I'm reviewing right now?" Further: "are
    there reasonable ways the software I'm reviewing right now could
    be improved to provide a better experience for the customer/user?"
    - Improvement ideas that deviate from the specification require
      interaction and approval from the PO.
  - Each reviewer should be motivated to take actions reviewing the
    code such that UAT succeeds on the first try.
    
- User Acceptance Test (UAT), _is_ the customer in this system
  - Final arbiter of whether the software product is acceptable


### The States

- Init/Prep (loop):
  - [driver] Prep the B, R(s), and UAT repositories as indicated in
    ~/projects/lightweight-containers
  - The B, R(s), and UAT thoroughly review the full specification
  - Issues (CONFLICTS/AMBIGUITIES/IMPROVEMENTS/CLARIFICATIONS) are
    worked out with the PO. This can take several rounds.
  - Work begins only after ALL participants 'UNDERSTAND' in their done
    file
  - EVERY TIME the PO alters/clarifies the specification in this stage
    ALL participants are allowed to review the changes.
  - Every participant produces a comprehensive checklist of items to
    accomplish/check at each specification point in the build.
  - Reviewers build their _best guess_ initial automated tooling for
    the ENTIRE set of specifications. Their tooling can be run using a
    `run_checks.sh` script in their "private tooling directory"
  - [driver] Each iteration performs "git shapshotting" as per the tricks used
    in ~/projects/lightweight-containers

Once all participants register UNDERSTAND we then move to the "Build loop"

- Build loop
  - Builder turn:
    - [driver] Prep the B repository as indicated in ~/projects/lightweight-containers
    - INITIAL BUILD: Builder is given it's initial prompt and told to work on spec 1.
    - "NEXT" BUILD: Builder is told to work on spec N.
    - RESPOND TO REVIEWERS: Any reviewer that indicates FAIL must be responded to.
      - Inputs to the BUILDER on review failures:
        - The reviewer's specific notes
        - The OUTPUT of the reviewer's automated tooling as reported by
          `run_checks.sh` for the reviewer in question (driven by the driver).
    - Builder writes a "done" file when finished with work
      - A list of ALL the specs for the project in the form:
        `NNN-<spec name>: <status>`
        - Where `<status>` can be one of:
          - PENDING for work not started
          - DONE for fully complete work
          - JUST COMPLETED<optionally with a list of "responding to" indicators>
    - [driver] Git capture and snapshotting is carried out as
        indicated in ~/projects/lightweight-containers
  - Reviewers Turn (all run concurrently):
    - [driver] Prep the Rs repositories as indicated in ~/projects/lightweight-containers
    - [driver] Runs each R's `run_checks.sh` and captures output
    - Each R is given an appropriate prompt which references the file created by the driver's run of `run_checks.sh`
    - Each R is responsible for reconciling the results of the `run_checks.sh` input
      - Failures, regardless of "spec stage" need to be investigated:
        - Actual failure? record and investigate other failures
        - Failure due to error in tooling? Fix only so far as the
          specification allows. Do NOT adapt to successfully match
          against "existing code" if that code violates specification
          expectations.
      - After _legitimate_ fixes to tooling, re-run `run_checks.sh` at
        this point any/all failures are automatic FAIL.
      - Investigate action checklist for the specification being
        examined. (This checklist was compiled during the init/prep
        loop.) Investigate all items on the checklist for the current
        spec (even if previously "checked" -- need to look for
        regressions in responses from B).
      - After ALL checklist items have been checked (confirmed to
        work), investigate options to automate checklist items such
        that simply running `run_checks.sh` provides an authoritative
        regression check.
      - After this, each R is encouraged to perform ad-hoc
        investigations into the code base consistent with their
        assigned role. Any ad-hoc research, whether or not it finds an
        issue, should result in more automated tooling to make
        `run_checks.sh` be able to repeat this work reliably in future
        runs.
      - Finally, each R should dedicate some time considering
        improvements that could be made to the application. Any
        insights should be discussed with PO, and sensible actions
        should be taken when the reply arrives.
      - Finally, each R writes a report of their findings into their
        assigned file in their assigned "feedback" directory, along
        with "PASS/FAIL" (with potential details) to their assigned
        "done file."
      - When all the "done files" have been produced, the driver will
        capture each R's repo as indicated in
        ~/projects/lightweight-containers
      - If any R's "done file" indicates FAIL it's the B's turn to
        respond to the feedback.
      - If all R's "done files" indicate "PASS", AND there are more
        specs to built, It's B's turn and it will be informed to work
        on the next spec.
      - If all R's "done files" indicate "PASS", AND there are NO more
        specs to built, then it's the UAT's turn.
  - UAT Turn:
    - [driver] Prep the UAT's repository as indicated in ~/projects/lightweight-containers
    - The UAT reconciles its checklist (it's initial checklist for
      action was built in the init/prep loop) with the current state
      of the specification. Adding/removing/altering checklist items
      as needed. Any concerns or ambiguity should result in a
      discussion with PO.
    - Build/update automation to verify all checklist items.
    - Run the automation
    - Investigate failures:
      - Real failure: record
      - Tooling failure: determine _if_ it's possible to fix the
        tooling while staying withing specification -- for instance,
        the automated tooling should not accept placeholder
        implementations.
    - Re-run tooling and record results, any failure is a hard FAIL result
    - For any checklist item that cannot be automated, run a manual
      investigation to determine if the item in question is complete
      and satisfactory.
    - Update the checklist to indicate what passed and what failed.
    - Record feedback into assigned "feedback" file and a
      "PASS"/"FAIL" (with optional details) into the assigned "done
      file"
