::  Native CI: the %urgit <-> %urgit-ci contract, the ship-to-runner
::  channel, and the relayed `act --json` envelope.
::
::    %urgit-ci persists state-0 and nothing else in this phase; it is a
::    greenfield agent and follows the state-0 rule literally.  P2 grows
::    state-0 in place (D8): a log handle on the attempt, the trust class
::    and pull number on the candidate, the untrusted-revision policy per
::    repository, the credential store, the CI signing key and the ship's
::    signing pair it is certified with.
::
/-  git
|%
+$  candidate-id   @uv
+$  daemon-id      @uv
+$  assignment-id  @uv
+$  attempt-id     @uv
::
::  $? bunts to its last entry: a defaulted trust class is untrusted, a
::  defaulted candidate status is unknown, and a defaulted attempt status
::  is an infrastructure error.  none of them can read as success.
::
+$  trust             ?(%trusted %untrusted)
+$  result            ?(%success %failure %skipped %cancelled)
+$  candidate-status  ?(%passed %failed %pending %skipped %unknown)
+$  attempt-status    ?(%passed %failed %skipped %running %infrastructure-error)
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
+$  object-ref        [key=@t size=@ud sha256=@t]
::
::  a stored third-party credential (D4).  the value sits in Gall state
::  because the spec says so; it never leaves through a scry, a JSON body,
::  a log line or an event.  scope %job releases it to every trusted job;
::  scope %env only to a trusted job whose `environment:` is in .envs.
::
+$  credential        [value=@t scope=?(%job %env) envs=(set @t) created=@da]
::
::  a released credential on the wire (D4/D5): the name, the value, the
::  expiry the attempt's deadline sets, a nonce, and the CI key's
::  signature over the jam of [recipient attempt 'grant:<name>' expiry
::  nonce].  never stored; built for one assignment answer.
::
+$  grant             [name=@t expiry=@da nonce=@uv sig=@ux]
::
::  the CI signing key (D5): an ed25519 pair from eny, and the ship's
::  certificate over the public key, signed with the ship's own networking
::  signing key.  .sek never leaves the ship.
::
+$  signing           [pub=@ux sek=@ux cert=@ux created=@da]
::
::  the ship's own signing pair, derived from the ring Jael hands over on
::  %private-keys (the way ames derives [sgn.pub sgn.sek]); only the
::  signing half is kept, and it is never read out.
::
+$  ship-keys         [=life pub=@ux sek=@ux]
::
::  what an assignment asks a daemon to do: run `act -l` over the candidate
::  checkout and post the plan, or run one job of a stored plan.  a
::  defaulted kind is a plan: it can never land anything.
::
+$  kind              ?(%job %plan)
::
::  how the pusher was admitted: the ship's own session, or the owner's
::  delegation through a write token (CI-TRUST-P1)
::
+$  via               ?(%session %token)
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
::
+$  job
  $:  id=@t
      workflow=@t
      name=@t
      stage=@ud
      needs=(list @t)
      cond=(unit cond)
      environment=(unit @t)
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
  ==
::
::  a runner daemon.  a record is created when the operator mints an
::  enrollment token and activated when a daemon enrolls with it; the raw
::  token and the raw bearer are never stored, only their hashes.
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
  ==
::
+$  state-0
  $:  %0
      candidates=(map candidate-id candidate)
      daemons=(map daemon-id daemon)
      assignments=(map assignment-id assignment)
      attempts=(map attempt-id attempt)
      ci-protected=(set [repo=@t ref=@t])
      policies=(map @t untrusted-policy)
      credentials=(map [repo=@t name=@t] credential)
      signing=(unit signing)
      ship-keys=(unit ship-keys)
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
::  %candidate-ready, %candidate-conflict, %landed and %land-refused;
::  %urgit-ci sends %materialize-candidate and %land-candidate; the
::  operator sends the rest, in the dojo or as JSON through the
::  session-authorized POST ci/action.  %assign names a kind and, for a
::  job, the workflow file and job id, so one job can be re-driven by hand.
::  %stage-candidate names the actor, whose trust class %urgit-ci decides
::  through %urgit's ci-can-write peek (D3), and the pull number when the
::  web merge staged it (D3a).  %approve-candidate names the acting ship,
::  admitted through the same peek.
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
      [%mint-enroll-token token=@uv]
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
  ==
--
