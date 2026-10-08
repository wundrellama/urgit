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
::    the state is versioned from now on: state-1, tagged %1, is the Q11
::    schema's fields exactly.  every earlier shape shared the tag %0 and
::    is frozen byte for byte in a sur file of its own — ci-state-base,
::    ci-state-review06 and ci-state-q11 — and converted explicitly by
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
+$  mode  ?(%required %trial %shadow)
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
::  a job-level condition, compiled OUTSIDE the ship by the daemon's YAML
::  walk (CI-EXPR-1, R2.2-A): the wire form is versioned `v:1`; this is the
::  v1 shape.  the ship evaluates %output-eq structurally and refuses
::  %unsupported with the raw expression quoted.  it never parses one.
::
+$  cond
  $%  [%output-eq job=@t output=@t literal=@t]
      [%unsupported raw=@t]
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
+$  job
  $:  id=@t
      workflow=@t
      name=@t
      stage=@ud
      needs=(list @t)
      cond=(unit cond)
      environment=(unit @t)
      runs-on=(set @t)
      timeout=(unit @ud)
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
::  stored; only the count, the recorded outputs, the relayed jobResult
::  and the claimed result enter state.  the full log is an object in the
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
      outputs=(map @t @t)
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
  ==
::
+$  state-1
  $:  %1
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
  ==
--
