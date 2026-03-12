# Flywheel

This is not a _true_ coding project, this is a "meta project," we're
going to write specs and documents that will be used by an independent
agent to do all the coding in a "lights-out" manner.

## Rules of Engagement

- No sharing of code from the source project, only and exclusively
  structured Markdown documents capturing instructions and
  specifications
- No "secret messages" that "unduly" bias the implementation to
  exactly replicate the source project. This said, you _can_ specify
  interfaces with textual descriptions for what is meant to be the
  logic and what the constraint and expectations are
- The "agents" that run the given instructions are not allowed to have
  access to or know of the existence of the source repositories that
  inspired the instructions they are following
- Don't assume that the agents that follow the instructions are
  `claude-code` agents. Your instructions and surrounding information
  should be robust enough for any frontier model to do the work:
  Claude, Gemini, or ChatGPT (codex)
- You can specify appropriate skills to use. In particular, your "go
  actor" skill will be quite useful in this context.

## Outputs

You will be putting your output into 2 locations

- `~/projects/file-uploader` -- this is the directory where the
  "lights-out" agent(s) will run. This needs a solid README.md and a
  `build-specification` directory where you will put all your
  specifications and plans. Initialize git in this directory and
  create a private remote on GitHub using `gh`.
- `~/projects/file-uploader-uat` -- this directory is where you will put
  documents related to _independent "user acceptance testing."_ We are
  going to try to automate UAT with agents as well. Initialize git in
  this directory and create a private remote on GitHub using `gh`

## Starting Point

We start by deep investigation of the "idpair-central" project located
at `~/idpair/idpair-central`.

1. Load up your go-actor skill
2. Thoroughly investigate the source code for `idpair-central`, ask
   questions as needed.
3. There are some "example CSVs" located at `~/idpair/example-csvs`,
   some of them are good, and some of them are deliberately bad. The
   "building agents" are allowed to know about these files. Encourage
   your "building agent" to build tooling to generate synthetic CSVs,
   both valid and invalid for testing. This tooling can be used
   internally but should also be exposed as CLI tools so users can
   generate data on demand for testing (especially UAT!!!)
4. We are NOT looking for "bug-for-bug" compatibility; instead if you,
   in your analysis discover some latent bugs, surface them. We will
   discuss what _should_ be built instead.
5. Improve on the existing codebase:
     a. Add tests
     b. Use more interfaces to allow for mocking "connecting" things
        to enable sensible unit testing
	 c. Improve on some of the code and abstractions in the existing
        code base. You'll notice that the "setup wizard" code is a
        tangled mess, see if you can give hints to the "building
        agent" to do better than this.  Also, improve on the
        aesthetics.

## Constraints

Only use my "preferred" Go web app baseline tooling. Also note: this
project does not use a database, this is on purpose -- data is stored
on files.

## Evaluation

There will be 3 levels of evaluation of the output:

1. The "automated UAT" _must pass_. The automated UAT can leave
   document artifacts in the `uat-feedback` directory under
   `~/projects/file-uploader`.  The `README.md` in
   `~/projects/file-uploader` should tell the "building agent" to look
   for feedback there.
2. Once "automated UAT" is done, you, in this directory will be given
   a chance to review the work products of both the "building agents"
   and the UAT agent. If you find their work lacking, update any/all
   documents in one or both `~/projects/file-uploader` and
   `~/projects/file-uploader-uat`. We will then have the "build and
   UAT" agents re-run and update. Since you know the source project
   you're comparing against, it's your job to ensure that what you
   _think_ you communicated accurately reflected what you know about
   the source project. Clearly we're not looking for a `diff` with no
   outputs. Logically the work product of the "builder agents" needs
   to be sufficiently "the same" as to be arguably acceptable.
3. I'll review the final product and give feedback.  This may result
   in more iterations.
   
As you can see, the goal is to make me happy with the final
product. Only then are we done.
