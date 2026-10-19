::  Native CI: the %urgit <-> %urgit-ci contract, the ship-to-runner
::  channel, and the relayed `act --json` envelope.
::
::    %urgit-ci persists state-0 and nothing else in this phase; it is a
::    greenfield agent and follows the state-0 rule literally.  P2 grows
::    state-0 in place (D8): a log handle on the attempt, the trust class
::    and pull number on the candidate, the untrusted-revision policy per
::    repository, the credential store, the CI signing key and the ship's
::    signing pair it is certified with.  P3 grows it again, in place: the
::    daemon's revocation, refusal, labels and repository binding, the
::    job's runs-on set and timeout, and a re-offered attempt status.
::    P4 grows it once more, in place (riders 02-04): the repository
::    incarnation and policy generation, the promoted baseline and its
::    lock, role bindings, environments, one-use approvals and overrides,
::    network policies, read capabilities, shadows and comparisons, the
::    audit, the sandbox requirement; the candidate's mode and bindings;
::    the attempt's manifest digest, sandbox, network and outcome; the
::    daemon's profiles and resolver flag.
::
::    that in-place rule ends here (state-migration ruling 01: Q12 A).
::    the state is versioned from now on.  state-1, tagged %1, was the
::    Q11 schema's fields exactly; it is frozen byte for byte as
::    sur/ci-state-1.  state-2, tagged %2, is state-1 with each attempt's
::    pinned addresses (CI-P4-NET-1, pinned addresses shown per run); it
::    is frozen byte for byte as sur/ci-state-2.  state-3, tagged %3
::    (Delivery 1A, manual runs), is state-2 with each candidate's
::    trigger — the event it runs for, a push or a manual dispatch with
::    its admitted inputs — and each lock's manual-run inventory; it is
::    frozen byte for byte as sur/ci-state-3.  state-4, tagged %4
::    (Delivery 1B), gives a planned job its compiled condition and its
::    declared outputs, and an attempt its step outputs by step, the
::    decision that ran or skipped it, the needs context it was given and
::    the declared outputs resolved when it closed; it is frozen byte for
::    byte as sur/ci-state-4.  state-5, tagged %5 (Delivery 1C), adds the
::    automatic events: a run mode %event, a candidate's event trigger and
::    its workflow selection, each lock's trigger catalog, the recorded
::    forge events and their ids, and the armed schedules.
::    every earlier shape shared the tag %0 and is frozen byte for byte in
::    a sur file of its own — ci-state-base, ci-state-review06 and
::    ci-state-q11 — and every shape is converted explicitly by
::    lib/ci-migrate on load, never cast (contract §8c).
::
/-  git
|%
+$  candidate-id  @uv
+$  daemon-id  @uv
+$  assignment-id  @uv
+$  attempt-id  @uv
::
::  $? bunts to its last entry: a defaulted trust class is untrusted, a
::  defaulted candidate status is unknown, and a defaulted attempt status
::  is an infrastructure error.  none of them can read as success.
::
+$  trust  ?(%trusted %untrusted)
+$  result  ?(%success %failure %skipped %cancelled)
+$  candidate-status  ?(%passed %failed %pending %skipped %unknown)
::  %reoffered is an attempt its daemon gave up, went silent on, or was
::  revoked from (CI-DELIVERY-1.1): closed without a verdict and offered
::  again as a fresh attempt, so it stands for nothing in a job's standing.
::
::  %cancelled is an attempt the ship closed under its runner (a policy
::  generation change, a revoked approval, an operator's cancel): it
::  stands for nothing.  the bunt stays the infrastructure error.
::
+$  attempt-status  ?(%passed %failed %skipped %running %reoffered %cancelled %infrastructure-error)
::
::  P4 (D5-D7, riders 02-04).  every $? bunts to its safe entry: a
::  defaulted mode is a shadow (never lands), a defaulted sandbox need is
::  the VM, a defaulted automation is manual, a defaulted outcome is
::  unknown, a defaulted dependency kind is unknown.
::
::  a manual run (%manual, Delivery 1A) is another mode that never lands:
::  it runs the promoted baseline's workflow on a ref's tip because an
::  operator dispatched it, and is no evidence for any landing.  an event
::  run (%event, Delivery 1C) is the run of an automatic push, pull
::  request or schedule: it never lands, receives no credentials and is
::  no evidence for any landing either
::
+$  mode  ?(%required %trial %manual %event %shadow)
+$  sandbox-need  ?(%container %vm)
+$  automation  ?(%automatic %manual)
+$  outcome  ?(%known %unknown)
+$  role  ?(%ci-policy %environment-approver %override)
+$  dep-kind  ?(%js %composite %container %local %download %reusable %unknown)
::
::  a delegated role binding (rider 02): a subset of the owner's authority
::  for other ships, scoped to an environment (approver) or a branch
::  (override); the owner holds every role implicitly and is never listed
::
+$  binding  [=role scope=(unit @t) ships=(set @p)]
::
::  an environment record (D6): user-extensible data, never an enum.  a
::  privileged job names one; its credentials are the only ones an
::  approval may release; automation admits without a click
::
+$  environment
  $:  name=@t
      description=@t
      =automation
      credentials=(set @t)
      created=@da
      actor=@p
  ==
::
::  one resolved dependency (D4): the original `uses:` text and where it
::  came from, its immutable identity (commit + tree for Git, digest for
::  OCI, sha256 for a download), the urgit mirror holding it, its license
::  notice, and the source location that named it.  a refusal names why
::  the node could not be resolved; a lock with a refusal is never
::  promoted.
::
+$  dep-node
  $:  uses=@t
      kind=dep-kind
      origin=@t
      ref=@t
      commit=@t
      tree=@t
      subpath=@t
      mirror=@t
      mirror-commit=@t
      digest=@t
      license=@t
      workflow=@t
      job=@t
      step=@ud
      sha256=@t
      size=@ud
      path=@t
      refusal=(unit @t)
  ==
::
::  the lock (D4): every node of one revision's dependency walk, keyed by
::  the digest of its canonical bytes; resolved by a daemon at an explicit
::  import and never refreshed by execution
::
+$  lock
  $:  digest=@ux
      repo=@t
      revision=oid:git
      nodes=(list dep-node)
      resolved=@da
      resolver=daemon-id
      bytes=@ud
      notices=(list @t)
      ::  state-3: the workflows of the revision that declare
      ::  workflow_dispatch, as the resolver's pinned compiler read them;
      ::  ~ for a lock resolved before manual runs were read, which is
      ::  not an empty inventory (contract §5; Delivery 1A)
      inventory=(unit (list dispatch-workflow))
      ::  state-5: every workflow's compiled `on:` (Delivery 1C); ~ for a
      ::  lock resolved before triggers were read, which is unknown: never
      ::  an empty catalog, never an authorization to run anything
      triggers=(unit trigger-catalog)
  ==
::
::  the promoted baseline of one branch (D5; rider 02): the approved
::  harness revision, which paths of the tree are harness (taken from
::  the baseline, never the candidate, in a required run), the lock it
::  runs with, who promoted it, and the generation the promotion opened.
::  it persists until replaced or revoked.
::
+$  baseline
  $:  revision=oid:git
      harness-paths=(list @t)
      lock=(unit @ux)
      promoted=@da
      actor=@p
      reason=@t
      generation=@ud
      policy-repo=(unit @t)
  ==
::
::  a one-use environment approval (rider 02): bound to everything the
::  privileged attempt is, fifteen minutes to be consumed, consumed by
::  exactly one attempt at admission
::
+$  approval
  $:  id=@uv
      repo=@t
      incarnation=@uv
      candidate=candidate-id
      oid=oid:git
      workflow=@t
      job=@t
      environment=@t
      credentials=(set @t)
      baseline=oid:git
      lock=(unit @ux)
      generation=@ud
      approver=@p
      at=@da
      expires=@da
      consumed=(unit attempt-id)
      invalidated=(unit @t)
  ==
::
::  a recorded override (R4.3-A; rider 02): the exact candidate object,
::  the tip it may replace, who, why, and what evidence was missing;
::  fifteen minutes to be used, consumed at the ref advance
::
+$  override
  $:  id=@uv
      repo=@t
      ref=@t
      incarnation=@uv
      candidate=candidate-id
      oid=oid:git
      expected=oid:git
      actor=@p
      reason=@t
      missing=@t
      generation=@ud
      at=@da
      expires=@da
      consumed=(unit @da)
      invalidated=(unit @t)
  ==
::
::  a standing network policy (rider 03): a job of a workflow may use a
::  named runner profile toward exactly these destinations; everything
::  else is locked
::
+$  network-policy
  $:  workflow=@t
      job=@t
      profile=@t
      destinations=(list @t)
      environment=(unit @t)
      actor=@p
      at=@da
  ==
::
+$  audit-entry  [at=@da actor=@p kind=@t repo=@t detail=@t]
::
::  a shadow run (D7): a candidate that lands nothing and deploys nothing,
::  staged for one external event so the two systems can be compared on
::  the same source
::
+$  shadow
  $:  id=@uv
      candidate=candidate-id
      repo=@t
      ref=@t
      event=@t
      external=@t
      created=@da
  ==
::
+$  comparison
  $:  id=@uv
      shadow=@uv
      at=@da
      agreement=?
      reason=@t
      external-verdict=@t
      native-verdict=@t
  ==
::
::  an attempt-bound read capability (A06): the repositories one attempt's
::  daemon may clone through /git with it, until the attempt's deadline
::
+$  read-capability  [attempt=attempt-id repos=(set @t) expires=@da]
::
::  what a repository does with a revision from an untrusted actor (D3):
::  wait for a writer's approval, or run it at once as a restricted check
::  that receives no credentials and can never land.  the bunt is
::  approval: a defaulted policy runs nothing.
::
+$  untrusted-policy  ?(%restricted %approval)
::
::  an object in the store, by handle only (D1): the key, its size and
::  its sha-256.  the bytes never enter Gall state.
::
+$  object-ref  [key=@t size=@ud sha256=@t]
::
::  a stored third-party credential (D4).  the value sits in Gall state
::  because the spec says so; it never leaves through a scry, a JSON body,
::  a log line or an event.  scope %job releases it to every trusted job;
::  scope %env only to a trusted job whose `environment:` is in .envs.
::
+$  credential  [value=@t scope=?(%job %env) envs=(set @t) created=@da]
::
::  a released credential on the wire (D4/D5): the name, the value, the
::  expiry the attempt's deadline sets, a nonce, and the CI key's
::  signature over the jam of [recipient attempt 'grant:<name>' expiry
::  nonce].  never stored; built for one assignment answer.
::
+$  grant  [name=@t expiry=@da nonce=@uv sig=@ux]
::
::  the CI signing key (D5): an ed25519 pair from eny, and the ship's
::  certificate over the public key, signed with the ship's own networking
::  signing key.  .sek never leaves the ship.
::
+$  signing  [pub=@ux sek=@ux cert=@ux created=@da]
::
::  the ship's own signing pair, derived from the ring Jael hands over on
::  %private-keys (the way ames derives [sgn.pub sgn.sek]); only the
::  signing half is kept, and it is never read out.
::
+$  ship-keys  [=life pub=@ux sek=@ux]
::
::  what an assignment asks a daemon to do: run `act -l` over the candidate
::  checkout and post the plan, or run one job of a stored plan.  a
::  defaulted kind is a plan: it can never land anything.
::
+$  kind  ?(%job %resolve %plan)
::
::  how the pusher was admitted: the ship's own session, or the owner's
::  delegation through a write token (CI-TRUST-P1)
::
+$  via  ?(%session %token)
::
::  a job expression, compiled OUTSIDE the ship by the pinned compiler
::  (CI-EXPR-2, R2.1-A/R2.2-A; runner/internal/plan expr.go) and checked
::  here node by node (lib/ci-expr).  the ship evaluates the tree and never
::  parses expression text.  %ctx names a context in lower case; %prop a
::  property as written (property names are read without case); %star is
::  the object filter `.*`; a number is a double.
::
+$  expr
  $~  [%null ~]
  $%  [%null ~]
      [%bool p=?]
      [%num p=@rd]
      [%str p=@t]
      [%ctx p=@t]
      [%prop of=expr name=@t]
      [%index of=expr at=expr]
      [%star of=expr]
      [%not p=expr]
      [%cmp op=?(%eq %ne %lt %le %gt %ge) l=expr r=expr]
      [%and l=expr r=expr]
      [%or l=expr r=expr]
      [%call fn=@t args=(list expr)]
  ==
::
::  a template: text with ${{ }} expressions interpolated as text
::
+$  part  $%([%lit p=@t] [%expr p=expr])
+$  template  [text=@t line=@ud col=@ud parts=(list part)]
::
::  a job's condition.  %if is a compiled `if` with the text and place the
::  workflow wrote it at, for diagnosis only.  %output-eq and %refused are
::  the two state-3 shapes (CI-EXPR-1) a plan saved before state-4 may
::  hold, kept with the meaning they were admitted under: %output-eq reads
::  the named output a need recorded, after every need passed; a %refused
::  one never runs its job
::
+$  condition
  $%  [%if text=@t line=@ud col=@ud =expr]
      [%output-eq job=@t output=@t literal=@t]
      [%refused text=@t reason=@t]
  ==
::
::  a declared job output (`jobs.<id>.outputs.<name>`), compiled
::
+$  declared-output  [name=@t =template]
::
::  a step output an attempt recorded, by the step act names it under:
::  [id] for a job's step, [id sub] for a composite action's own step,
::  ~ for an output recorded before state-4, which named no step.
::  .redacted says the ship's credential fence changed the value
::
+$  step-output  [step=(list @t) name=@t value=@t redacted=?]
::
::  a declared output resolved when its attempt closed: the value, or why
::  it could not be resolved
::
+$  job-output  [name=@t value=(each @t @t) redacted=?]
::
::  one need of a job as the job's decision and its needs context saw it:
::  the accepted attempt, its status, the result expressions read
::  (success, failure, skipped; an infrastructure error reads as failure)
::  and its resolved declared outputs
::
+$  need-entry
  $:  id=@t
      attempt=attempt-id
      status=attempt-status
      result=@t
      outputs=(list [name=@t value=@t])
  ==
::
::  why a job ran, was skipped, or was refused at its condition; .value is
::  the condition's value as shown, ~ when no condition was evaluated
::
+$  decision  [verdict=?(%run %skip %error) reason=@t value=(unit @t)]
::
::  manual runs (Delivery 1A).  a workflow that declares
::  `on.workflow_dispatch` is listed with its declared inputs as the
::  pinned compiler read them at the baseline revision; the ship checks
::  each definition and admits a run's values against them, and never
::  parses YAML.  a default is the text the workflow wrote: ~ is no
::  default, which is not an empty one.
::
+$  input-kind  ?(%boolean %choice %number %environment %string)
::
+$  dispatch-input
  $:  id=@t
      kind=input-kind
      description=@t
      required=?
      default=(unit @t)
      options=(list @t)
  ==
::
::  one workflow of a revision's manual-run inventory; .refusal is why it
::  cannot be run manually, with its inputs dropped
::
+$  dispatch-workflow
  $:  workflow=@t
      name=@t
      inputs=(list dispatch-input)
      refusal=(unit @t)
  ==
::
::  an admitted value, typed as its input declares: a number keeps its
::  plain decimal text; a choice and an environment are strings
::
+$  input-value
  $%  [%boolean p=?]
      [%number p=@t]
      [%string p=@t]
  ==
::
::  how an input of a run got its value.  $? bunts to its last entry: a
::  defaulted source claims nothing was supplied.
::
+$  input-source  ?(%supplied %default %absent)
::
+$  admitted-input
  $:  id=@t
      kind=input-kind
      source=input-source
      value=(unit input-value)
  ==
::
::  the event a run is for, immutable once the run exists (CI-SIGN-1):
::  a push (the candidate's ref, head and actor say which), or a manual
::  dispatch of one workflow file with the inputs admitted for it, the
::  event payload act reads, and the operator's idempotency key.  every
::  attempt's signed manifest binds the event and the payload's digest.
::
+$  trigger
  $%  [%push ~]
      $:  %dispatch
          workflow=@t
          name=@t
          inputs=(list admitted-input)
          payload=@t
          request=@t
      ==
      ::  state-5 (Delivery 1C): an automatic event's run — the event as
      ::  the forge persisted it and the ship admitted it, fixed here
      [%event =run-event]
  ==
::
::  Delivery 1C: automatic events, their filters and schedules.  The
::  pinned compiler (runner/internal/plan TriggersOf) compiles every
::  workflow's `on:` at the explicit import; the ship checks the structure
::  (lib/ci-trigger) and evaluates it, and never reads YAML, glob or cron
::  text, which travels for display only.
::
::  a filter pattern's compiled token (plan glob.go): %lit a run of bytes,
::  %class one byte of a set, %star bytes but /, %globstar any bytes, %dirs
::  zero or more whole directories (`**/`), %opt zero or one and %many zero
::  or more of one byte or class
::
+$  glob-atom  $%([%lit p=@t] [%class p=@t])
+$  glob-token
  $~  [%globstar ~]
  $%  [%lit p=@t]
      [%class p=@t]
      [%star ~]
      [%globstar ~]
      [%dirs ~]
      [%opt p=glob-atom]
      [%many p=glob-atom]
  ==
+$  pattern  [text=@t negated=? tokens=(list glob-token)]
::
::  an ordered include list (its ! patterns exclude again), or an -ignore
::  list every pattern of which excludes
::
+$  filter  [ignore=? patterns=(list pattern)]
::
::  one cron line's fields as sorted values; the day rule takes both day
::  fields when either begins with * (day-star, weekday-star), else either
::
+$  cron-spec
  $:  minutes=(list @ud)
      hours=(list @ud)
      days=(list @ud)
      months=(list @ud)
      weekdays=(list @ud)
      day-star=?
      weekday-star=?
  ==
::
::  one `on.schedule` entry: the text as written, the IANA zone it names
::  (~ is the UTC default), and its fields, or why it is refused
::
+$  schedule-def
  $:  cron=@t
      timezone=(unit @t)
      refusal=(unit @t)
      spec=(unit cron-spec)
  ==
::
::  one event of a workflow's `on:`; .refusal is why it never selects
::
+$  trigger-event
  $:  event=@t
      refusal=(unit @t)
      branches=(unit filter)
      tags=(unit filter)
      paths=(unit filter)
      types=(list @t)
      types-declared=?
      schedules=(list schedule-def)
      line=@ud
      col=@ud
  ==
+$  trigger-workflow
  $:  workflow=@t
      name=@t
      events=(list trigger-event)
      refusal=(unit @t)
  ==
::
::  a timezone's UTC offsets (seconds east, signed) over the compiler's
::  fixed horizon: the offset at .start, then each transition
::
+$  zone  [start=@da end=@da offset=@sd transitions=(list [at=@da offset=@sd])]
+$  trigger-catalog  [workflows=(list trigger-workflow) zones=(map @t zone)]
::
::  what an event selected: per workflow, selected, excluded by its
::  filters, or in error (a refused trigger), with the reason; .basis says
::  what was compared (the ref, the diff) and .note anything the whole
::  selection must say (an unknown catalog, evidence not evaluated)
::
+$  selection-verdict  ?(%selected %excluded %error)
+$  workflow-selection  [workflow=@t name=@t verdict=selection-verdict reason=@t]
+$  event-selection
  $:  basis=@t
      outcomes=(list workflow-selection)
      note=(unit @t)
  ==
::
::  a native pull request's facts as the forge persisted them: the
::  activity, the head and the ref it came from, the target branch and the
::  tip the run integrates onto (the pre-merge tip for a merged close)
::
+$  pull-facts
  $:  number=@ud
      title=@t
      action=@t
      head=oid:git
      head-ref=@t
      base-ref=@t
      base=oid:git
      merged=?
      source-ship=@p
      source-repository=@t
  ==
::
::  a due schedule occurrence: the definition it belongs to, its UTC due
::  instant and when it was admitted, and the promotion that authorized it
::
+$  occurrence-facts
  $:  workflow=@t
      index=@ud
      cron=@t
      timezone=(unit @t)
      due=@da
      admitted=@da
      definition=@uv
      promoter=@p
      promoted=@da
  ==
::
::  an automatic event's run, immutable: the forge event or occurrence it
::  was admitted for, the ref, the before and after tips (a push), the pull
::  request or the occurrence, who or what caused it and how it arrived,
::  when, and the payload act reads (rendered once at admission)
::
+$  run-event
  $:  id=@uv
      kind=?(%push %pull-request %schedule)
      ref=@t
      before=(unit oid:git)
      after=(unit oid:git)
      pull=(unit pull-facts)
      occurrence=(unit occurrence-facts)
      actor=@p
      route=@t
      at=@da
      payload=@t
  ==
::
::  a source event the forge persisted, as %urgit-ci recorded it, whether
::  it ran anything or not: the facts, what the selection decided and the
::  runs it created
::
+$  forge-event
  $:  id=@uv
      repo=@t
      kind=?(%push %pull-request %default-branch)
      ref=@t
      before=(unit oid:git)
      after=(unit oid:git)
      pull=(unit pull-facts)
      actor=@p
      route=@t
      at=@da
      received=@da
      outcome=@t
      selected=(unit event-selection)
      runs=(list candidate-id)
  ==
::
::  an armed schedule of a repository's default branch baseline: its
::  definition and identity, what authorized it, its state, the due
::  instant its wake is armed for, and what became of earlier ones
::
+$  schedule-key  [repo=@t workflow=@t index=@ud]
+$  missed-interval  [first=@da last=@da count=@ud]
+$  schedule-entry
  $:  name=@t
      def=schedule-def
      zone=(unit zone)
      definition=@uv
      ref=@t
      baseline=oid:git
      lock=@ux
      incarnation=@uv
      promoter=@p
      promoted=@da
      registered=@da
      state=?(%active %refused %retired)
      reason=(unit @t)
      armed=(unit @da)
      last-due=(unit @da)
      last-admitted=(unit [due=@da at=@da run=candidate-id late=?])
      admitted=(list @uv)
      missed=(list missed-interval)
      stale-wakes=@ud
  ==
::
::  what %urgit reports of one persisted write (%forge-events): a ref
::  move, or a pull request transition, each with the identity the forge
::  gave it in the event that persisted it
::
+$  ref-update  [id=@uv ref=@t old=(unit oid:git) new=(unit oid:git)]
+$  pull-update
  $:  id=@uv
      number=@ud
      action=@t
      head=oid:git
      base=oid:git
      source-ref=@t
      target-ref=@t
      source-ship=@p
      source-repository=@t
      title=@t
      merged=?
      target-before=(unit oid:git)
  ==
::
::  one planned job.  jobs are keyed by [workflow id], never by id alone:
::  `needs` never crosses a workflow file.  .name is the workflow's real
::  name as `act -l` printed it from the unprojected candidate; the name
::  act runs a job under is the attempt's projection-name (CI-PROJECT-1.1).
::  .runs-on is the job's `runs-on` as a set (a string is a one-element
::  set); a daemon takes the job only when its labels and the implicit set
::  cover it (CI-P3-SCHED-A).  .timeout is the job's `timeout-minutes`,
::  which bounds the attempt's deadline (CI-DELIVERY-1.1) when declared.
::
::  .outputs are the declared outputs (state-4), ~ for a plan saved
::  before they were read, which is not a job that declares none.
::
+$  job
  $:  id=@t
      workflow=@t
      name=@t
      stage=@ud
      needs=(list @t)
      cond=(unit condition)
      environment=(unit @t)
      runs-on=(set @t)
      timeout=(unit @ud)
      outputs=(unit (list declared-output))
  ==
::
::  a staged head for a CI-protected ref.  .candidate is the exact
::  integration object once %urgit has materialized it: the head itself
::  for a fast-forward, else the merge of the head onto the base tip.
::  .trust is decided at staging from the actor (D3): a writer's revision
::  is trusted, anyone else's is untrusted and waits for approval unless
::  the repository's policy runs it restricted.  .pull is the pull request
::  the web merge staged it for (D3a); landing flips that pull to merged.
::
+$  candidate
  $:  id=candidate-id
      repo=@t
      ref=@t
      head=oid:git
      base=oid:git
      candidate=(unit oid:git)
      conflict=?
      status=candidate-status
      attempts=(list attempt-id)
      plan=(unit (list job))
      plan-oid=(unit oid:git)
      verdict-reason=(unit @t)
      actor=@p
      =via
      =trust
      pull=(unit @ud)
      created=@da
      updated=@da
      ::  P4: what the candidate is bound to (D5).  .mode says whether its
      ::  evidence can be required; .generation, .baseline, .lock and
      ::  .incarnation are the bindings its evidence carries; a candidate
      ::  whose bindings are no longer current is reset, never landed.
      ::  .sandbox is the least sandbox its attempts may run in.  .trial-of
      ::  names the required candidate a trial twin was staged beside.
      =mode
      generation=@ud
      baseline=(unit oid:git)
      lock=(unit @ux)
      incarnation=@uv
      sandbox=sandbox-need
      trial-of=(unit candidate-id)
      harness-differs=?
      ::  state-3: the event the run is for (Delivery 1A)
      =trigger
      ::  state-5: which workflows the event selected and why (Delivery
      ::  1C), from the governing catalog; ~ before it was evaluated, and
      ::  for every run before state-5
      selected=(unit event-selection)
  ==
::
::  a runner daemon.  a record is created when the operator mints an
::  enrollment token and activated when a daemon enrolls with it; the raw
::  token and the raw bearer are never stored, only their hashes.  P3:
::  .revoked is set and .bearer-hash cleared when the operator revokes it
::  (its next poll answers 401); .refused carries the reason it abandoned
::  an assignment over its pinned key, and holds until it re-enrolls;
::  .labels is what the daemon declares (its TOML, sent on every poll);
::  .repos is the operator's binding, set on the ship and never by the
::  daemon: ~ is the pool, [~ set] restricts it to those repositories.
::
+$  daemon
  $:  id=daemon-id
      token-hash=@
      bearer-hash=(unit @)
      minted=@da
      enrolled=(unit @da)
      last-seen=(unit @da)
      capacity=@ud
      sandbox=@t
      running=(set attempt-id)
      revoked=(unit @da)
      refused=(unit @t)
      labels=(set @t)
      repos=(unit (set @t))
      profiles=(set @t)
      resolver=?
  ==
::
::  the operator's release of a legacy retention from the Runners panel
::  (legacy-recovery UI ruling 01; runner/launcher/INTEGRATION.md §11.12;
::  contract §8b).  a daemon's latest retention report: when it came, its
::  body as posted (served to the panel as it is), and the entries a
::  command is bound to.  the report authorizes nothing.
::
+$  recovery-entry
  $:  selection=@t
      revision=@ud
      attempt=@t
      label=@t
      kind=@t
      eligible=?
      evidence=@t
  ==
+$  recovery-report  [at=@da body=@t entries=(list recovery-entry)]
::
::  a recovery command's lifecycle: %queued until a poll that carries them
::  out hands it over, %delivered until its daemon answers, then the
::  answer.  %completed — the runner's durable release — stands against
::  any other; %expired is a command never handed over in its fifteen
::  minutes.  the bunt is %refused, which claims nothing and is never
::  handed over.
::
+$  recovery-status  ?(%queued %delivered %uncertain %completed %expired %refused)
::
+$  recovery-command
  $:  id=@uv
      daemon=daemon-id
      operation=@t
      selection=@t
      revision=@ud
      evidence=@t
      label=@t
      actor=@p
      requested=@da
      expires=@da
      nonce=@uv
      delivered=(unit @da)
      deliveries=@ud
      status=recovery-status
      detail=@t
      finished=(unit @da)
  ==
::
+$  assignment
  $:  id=assignment-id
      candidate=candidate-id
      daemon=daemon-id
      attempt=attempt-id
      =trust
      =kind
      workflow=(unit @t)
      job=(unit @t)
      deadline=@dr
      assigned=@da
      delivered=(unit @da)
  ==
::
::  a plan the ship could not validate closes its attempt %failed with the
::  reason; the candidate fails with the same reason (R1-A: diagnosed,
::  never dropped)
::
+$  attempt-result
  $%  [%job-result =result]
      [%plan-invalid message=@t]
      [%infrastructure-error message=@t]
  ==
::
::  one execution of a candidate on one daemon.  the event stream is not
::  stored; only the count, the recorded step outputs, the relayed
::  jobResult and the claimed result enter state.  the full log is an object in the
::  store the daemon uploaded before it posted the result (D1); .log is
::  its handle, ~ until a result names one.
::
+$  attempt
  $:  id=attempt-id
      candidate=candidate-id
      assignment=assignment-id
      daemon=daemon-id
      =trust
      =kind
      workflow=(unit @t)
      job=(unit @t)
      status=attempt-status
      events=@ud
      step-outputs=(list step-output)
      job-result=(unit result)
      result=(unit attempt-result)
      reason=(unit @t)
      projection-name=(unit @t)
      log=(unit object-ref)
      started=@da
      finished=(unit @da)
      ::  P4: the bindings the signed manifest carried (D5), the sandbox
      ::  it required, the network profile it ran under, the approval it
      ::  consumed (a privileged attempt), and whether its outcome is known
      generation=@ud
      =mode
      manifest=@uv
      sandbox=sandbox-need
      network=@t
      approval=(unit @uv)
      =outcome
      ::  state-2: what the launcher pinned each granted DNS name to for
      ::  this run, as the daemon reported it when the VM started; ~ for
      ::  a run granted no name, and for every run before state-2
      pinned=(list pin)
      ::  state-4 (Delivery 1B): why the job ran or did not (~ before
      ::  state-4, and for a plan or a resolve); the needs context it was
      ::  given, each need's accepted attempt; and its declared outputs as
      ::  resolved when it closed (~ while it runs, when it had no result,
      ::  and before state-4)
      decision=(unit decision)
      needs=(list need-entry)
      job-outputs=(unit (list job-output))
  ==
::
::  one granted DNS name and the IPv4 addresses the launcher pinned it to
::  for one run (CI-P4-NET-1): they change from run to run
::
+$  pin  [name=@t addrs=(list @t)]
::
+$  state-5
  $:  %5
      candidates=(map candidate-id candidate)
      daemons=(map daemon-id daemon)
      assignments=(map assignment-id assignment)
      attempts=(map attempt-id attempt)
      ci-protected=(set [repo=@t ref=@t])
      policies=(map @t untrusted-policy)
      credentials=(map [repo=@t name=@t] credential)
      signing=(unit signing)
      ship-keys=(unit ship-keys)
      ::  P4
      incarnations=(map @t @uv)
      generations=(map @t @ud)
      baselines=(map [repo=@t ref=@t] baseline)
      locks=(map @ux lock)
      roles=(map @t (list binding))
      environments=(map [repo=@t name=@t] environment)
      approvals=(map @uv approval)
      overrides=(map @uv override)
      network-policies=(map @t (list network-policy))
      read-capabilities=(map @ read-capability)
      shadows=(map @uv shadow)
      comparisons=(map @uv comparison)
      audit=(list audit-entry)
      sandbox-requirements=(map @t sandbox-need)
      ::  the write tokens a resolve attempt's mirrors hold until it closes
      mirror-tokens=(map attempt-id (set @t))
      ::  the operator's explicit import mappings of a resolve (rider 03),
      ::  by the synthetic resolve candidate: [from to] prefixes
      resolve-mappings=(map candidate-id (list [from=@t to=@t]))
      ::  a repository's harness paths (D5): what a promotion records on
      ::  the baseline it creates; ~ means the default, .github/
      harness-paths=(map @t (list @t))
      ::  the legacy recovery (legacy-recovery UI ruling 01): each
      ::  runner's latest retention report, and the operator's recovery
      ::  commands by id, never pruned
      recovery-reports=(map daemon-id recovery-report)
      recovery-commands=(map @uv recovery-command)
      ::  state-5 (Delivery 1C): the forge events recorded per repository
      ::  (newest kept), the ids of every event admitted (the replay
      ::  guard, newest kept), and the armed schedules
      forge-events=(map @uv forge-event)
      event-ids=(map @uv @da)
      schedules=(map schedule-key schedule-entry)
  ==
::
+$  state-6
  $:  %6
      candidates=(map candidate-id candidate)
      daemons=(map daemon-id daemon)
      assignments=(map assignment-id assignment)
      attempts=(map attempt-id attempt)
      ci-protected=(set [repo=@t ref=@t])
      policies=(map @t untrusted-policy)
      credentials=(map [repo=@t name=@t] credential)
      signing=(unit signing)
      ship-keys=(unit ship-keys)
      ::  P4
      incarnations=(map @t @uv)
      generations=(map @t @ud)
      baselines=(map [repo=@t ref=@t] baseline)
      locks=(map @ux lock)
      roles=(map @t (list binding))
      environments=(map [repo=@t name=@t] environment)
      approvals=(map @uv approval)
      overrides=(map @uv override)
      network-policies=(map @t (list network-policy))
      read-capabilities=(map @ read-capability)
      shadows=(map @uv shadow)
      comparisons=(map @uv comparison)
      audit=(list audit-entry)
      sandbox-requirements=(map @t sandbox-need)
      ::  the write tokens a resolve attempt's mirrors hold until it closes
      mirror-tokens=(map attempt-id (set @t))
      ::  the operator's explicit import mappings of a resolve (rider 03),
      ::  by the synthetic resolve candidate: [from to] prefixes
      resolve-mappings=(map candidate-id (list [from=@t to=@t]))
      ::  a repository's harness paths (D5): what a promotion records on
      ::  the baseline it creates; ~ means the default, .github/
      harness-paths=(map @t (list @t))
      ::  the legacy recovery (legacy-recovery UI ruling 01): each
      ::  runner's latest retention report, and the operator's recovery
      ::  commands by id, never pruned
      recovery-reports=(map daemon-id recovery-report)
      recovery-commands=(map @uv recovery-command)
      ::  state-5 (Delivery 1C): the forge events recorded per repository
      ::  (newest kept), the ids of every event admitted (the replay
      ::  guard, newest kept), and the armed schedules
      forge-events=(map @uv forge-event)
      event-ids=(map @uv @da)
      schedules=(map schedule-key schedule-entry)
      ::  Tagged-revision runs and their exact resolver attempts. A null
      ::  attempt waits for a resolver. Completed runs keep this provenance.
      tag-runs=(map candidate-id (unit attempt-id))
  ==

+$  state-7
  $:  %7
      candidates=(map candidate-id candidate)
      daemons=(map daemon-id daemon)
      assignments=(map assignment-id assignment)
      attempts=(map attempt-id attempt)
      ci-protected=(set [repo=@t ref=@t])
      policies=(map @t untrusted-policy)
      credentials=(map [repo=@t name=@t] credential)
      signing=(unit signing)
      ship-keys=(unit ship-keys)
      ::  P4
      incarnations=(map @t @uv)
      generations=(map @t @ud)
      baselines=(map [repo=@t ref=@t] baseline)
      locks=(map @ux lock)
      roles=(map @t (list binding))
      environments=(map [repo=@t name=@t] environment)
      approvals=(map @uv approval)
      overrides=(map @uv override)
      network-policies=(map @t (list network-policy))
      read-capabilities=(map @ read-capability)
      shadows=(map @uv shadow)
      comparisons=(map @uv comparison)
      audit=(list audit-entry)
      sandbox-requirements=(map @t sandbox-need)
      ::  the write tokens a resolve attempt's mirrors hold until it closes
      mirror-tokens=(map attempt-id (set @t))
      ::  the operator's explicit import mappings of a resolve (rider 03),
      ::  by the synthetic resolve candidate: [from to] prefixes
      resolve-mappings=(map candidate-id (list [from=@t to=@t]))
      ::  a repository's harness paths (D5): what a promotion records on
      ::  the baseline it creates; ~ means the default, .github/
      harness-paths=(map @t (list @t))
      ::  the legacy recovery (legacy-recovery UI ruling 01): each
      ::  runner's latest retention report, and the operator's recovery
      ::  commands by id, never pruned
      recovery-reports=(map daemon-id recovery-report)
      recovery-commands=(map @uv recovery-command)
      ::  state-5 (Delivery 1C): the forge events recorded per repository
      ::  (newest kept), the ids of every event admitted (the replay
      ::  guard, newest kept), and the armed schedules
      forge-events=(map @uv forge-event)
      event-ids=(map @uv @da)
      schedules=(map schedule-key schedule-entry)
      ::  Tagged-revision runs and their exact resolver attempts. A null
      ::  attempt waits for a resolver. Completed runs keep this provenance.
      tag-runs=(map candidate-id (unit attempt-id))
      ::  Empty defaults grant no new automatic acquisition authority.
      automatic-inputs=(map @t (list @t))
      input-snapshots=(map candidate-id (list @t))
      input-evidence=(map @ux @t)
      resolver-inputs=(map daemon-id (list @t))
  ==

::
::  the event envelope: one `act --json` line after validation
::
+$  command
  $%  [%set-output name=@t value=@t]
      [%summary body=@t]
      [%group name=@t]
      [%endgroup ~]
      [%other @t]
  ==
::
+$  event
  $:  job=@t
      job-id=@t
      stage=(unit @t)
      step=(unit @t)
      step-id=(list @t)
      step-result=(unit result)
      job-result=(unit result)
      command=(unit command)
      raw=?
      msg=@t
      at=@da
  ==
::
::  pokes on the %ci-action mark.  %urgit sends %stage-candidate,
::  %candidate-ready, %candidate-conflict, %landed, %land-refused and
::  %repository-deleted (its own state under the name goes with the
::  repository, so a repository re-created under it has no CI history);
::  %urgit-ci sends %materialize-candidate and %land-candidate; the
::  operator sends the rest, in the dojo or as JSON through the
::  session-authorized POST ci/action.  %assign names a kind and, for a
::  job, the workflow file and job id, so one job can be re-driven by hand.
::  %stage-candidate names the actor, whose trust class %urgit-ci decides
::  through %urgit's ci-can-write peek (D3), and the pull number when the
::  web merge staged it (D3a).  %approve-candidate names the acting ship,
::  admitted through the same peek.  the enrollment token is minted by the
::  session-authorized POST ci/runners/mint alone (P3 D1: the ship draws
::  it and answers it once); %expire-token deletes a minted, not enrolled
::  record, %revoke-daemon clears an enrolled daemon's bearer and re-offers
::  its work, %set-daemon-repos binds it to named repositories (D2/D2b).
::  %request-legacy-release (legacy-recovery UI ruling 01) records a
::  recovery command for one runner, which carries it out and answers on
::  its own channel; only the owner asks, from the Runners panel.
::  %request-history-transition (legacy-replay-upgrade ruling 01) records
::  the transition of a runner whose execution history is incomplete, the
::  same way.
::
+$  action
  $%  [%set-ci-protected repo=@t ref=@t protected=?]
      [%stage-candidate repo=@t ref=@t head=oid:git base=oid:git actor=@p =via pull=(unit @ud)]
      [%approve-candidate id=candidate-id actor=@p]
      [%rerun-candidate id=candidate-id]
      [%set-untrusted-policy repo=@t policy=untrusted-policy]
      [%set-credential repo=@t name=@t value=@t scope=?(%job %env) envs=(set @t)]
      [%delete-credential repo=@t name=@t]
      [%rotate-ci-key ~]
      [%materialize candidate=candidate-id]
      [%materialize-candidate repo=@t ref=@t head=oid:git base=oid:git]
      [%candidate-ready repo=@t ref=@t head=oid:git base=oid:git candidate=oid:git]
      [%candidate-conflict repo=@t ref=@t head=oid:git base=oid:git]
      [%expire-token id=daemon-id]
      [%revoke-daemon id=daemon-id]
      [%set-daemon-repos id=daemon-id repos=(unit (set @t))]
      $:  %assign
          candidate=candidate-id
          daemon=daemon-id
          =kind
          workflow=(unit @t)
          job=(unit @t)
          deadline=(unit @dr)
      ==
      $:  %land-candidate
          id=candidate-id
          repo=@t
          ref=@t
          candidate=oid:git
          expected=oid:git
          pull=(unit @ud)
      ==
      [%landed candidate=candidate-id]
      [%land-refused candidate=candidate-id reason=@t]
      [%repository-deleted repository=@t]
      ::  P4 (D4-D7; riders 02-04).  every one names its actor: the web
      ::  route puts our.bowl there, the peer path src.bowl, never JSON.
      [%resolve-dependencies repo=@t revision=oid:git actor=@p mappings=(list [from=@t to=@t])]
      [%promote-baseline repo=@t ref=@t revision=oid:git reason=@t actor=@p policy-repo=(unit @t) lock=(unit @ux)]
      [%revoke-baseline repo=@t ref=@t reason=@t actor=@p]
      [%set-harness-paths repo=@t paths=(list @t) actor=@p]
      [%set-sandbox-requirement repo=@t need=sandbox-need actor=@p]
      [%set-role repo=@t =role scope=(unit @t) ships=(set @p) actor=@p]
      [%clear-role repo=@t =role scope=(unit @t) actor=@p]
      [%set-environment repo=@t name=@t description=@t =automation credentials=(set @t) actor=@p]
      [%delete-environment repo=@t name=@t actor=@p]
      [%approve-environment candidate=candidate-id workflow=@t job=@t environment=@t credentials=(set @t) actor=@p]
      [%record-override repo=@t ref=@t candidate=candidate-id oid=oid:git expected=oid:git reason=@t actor=@p]
      [%set-automatic-inputs repo=@t sources=(list @t) expected=@ud actor=@p]
      [%set-network-policy repo=@t workflow=@t job=@t profile=@t destinations=(list @t) environment=(unit @t) actor=@p]
      [%clear-network-policy repo=@t workflow=@t job=@t actor=@p]
      [%stage-shadow repo=@t ref=@t head=oid:git base=oid:git event=@t external=@t actor=@p]
      [%compare-shadow shadow=@uv oid=oid:git event=@t verdict=@t jobs=(list [workflow=@t job=@t status=@t]) actor=@p]
      [%cancel-attempt id=attempt-id reason=@t actor=@p]
      ::  a delegate on another ship acts through the peer protocol (rider
      ::  02): %urgit hands over the requester and the action's JSON; the
      ::  action is parsed here with that ship as its actor and judged by
      ::  the same authority predicate as the session's
      [%delegated actor=@p repo=@t body=@t]
      ::  the owner's release of a legacy retention from the Runners panel
      ::  (legacy-recovery UI ruling 01): bound to the entry, the revision
      ::  and the evidence digest the runner's latest report shows; never
      ::  delegable, and the actor is the session's ship
      [%request-legacy-release id=daemon-id selection=@t revision=@ud evidence=@t actor=@p]
      ::  the owner's transition of a runner whose execution history is
      ::  incomplete (legacy-replay-upgrade ruling 01): bound to the
      ::  history, revision and evidence digest the runner's latest report
      ::  shows waiting; never delegable, and the actor is the session's ship
      [%request-history-transition id=daemon-id revision=@ud evidence=@t actor=@p]
      ::  a manual run (Delivery 1A): one workflow that declares
      ::  workflow_dispatch at the ref's promoted baseline, run on the
      ::  ref's tip with typed inputs admitted against its declaration.
      ::  .tip and .generation are what the operator's form was loaded
      ::  at: a branch that moved or a policy that changed since refuses.
      ::  .request is the operator's idempotency key: the same request
      ::  is the same run.  the actor is the session's ship, never JSON.
      $:  %dispatch-workflow
          repo=@t
          ref=@t
          workflow=@t
          inputs=(list [name=@t value=input-value])
          tip=oid:git
          generation=@ud
          request=@t
          actor=@p
      ==
      ::  Delivery 1C.  %urgit reports what one of its events persisted:
      ::  every ref that moved and every pull request transition, and a
      ::  default-branch change, with the authenticated sender of the event
      ::  and the route; a local poke only, never the web or a peer.
      ::  %urgit-ci asks %urgit to build a pull request run's integration
      ::  object under the run's own scratch ref (%materialize-run), and
      ::  hears the object or the conflict
      $:  %forge-events
          repo=@t
          refs=(list ref-update)
          pulls=(list pull-update)
          head=(unit [old=@t new=@t])
          actor=@p
          route=@t
          at=@da
      ==
      [%materialize-run id=candidate-id repo=@t ref=@t head=oid:git base=oid:git]
      [%run-ready id=candidate-id candidate=oid:git]
      [%run-conflict id=candidate-id reason=@t]
  ==
--
