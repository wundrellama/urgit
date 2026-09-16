::  Native CI controller: candidates, daemons, assignments, attempts.
::
::    P0 holds the contracts: the landing-eligibility scry %urgit reads
::    before it advances a CI-protected ref, the candidate pokes exchanged
::    with %urgit, the daemon channel under /apps/urgit/api/ci, the relayed
::    act event envelope, and CI object-store signing.
::
::    P1 replaces the operator: a staged candidate is materialized at once;
::    a materialized candidate is planned by a daemon running `act -l` on
::    its checkout; the ship validates the plan, schedules each job on the
::    least-loaded daemon with capacity as its `needs` and `if` allow,
::    records the verdict, and asks %urgit to land a passed candidate.
::    the daemon never decides; the operator's %assign remains for re-runs.
::
::    P2 adds trust, storage and the web surface: a candidate's trust class
::    is decided at staging from its actor and an untrusted one waits for
::    approval or runs restricted; the daemon uploads each finished act
::    stream to the ship's object store under a ship-signed PUT and the
::    ship hands viewers a presigned GET; credentials are stored here and
::    released per attempt as signed grants; assignments are signed with a
::    ship-certified CI key; the session-authorized ci/* routes serve the
::    repository page's CI tab.
::
::    persisted state is state-0 and stays there in this phase.  the open
::    long-polls are transient and dropped on every load.
::
/-  ci, git
/+  dbug, default-agent, server, ci-event, ci-plan, ci-storage, git-codec, git-protocol, git-storage
|%
+$  card  card:agent:gall
+$  poll  [eyre-id=@ta at=@da]
++  poll-window  ~s25
++  redeliver-after  ~m2
++  default-deadline  ~h1
++  plan-deadline  ~m5
++  stale-after  ~m5
++  storage-refusal  'ship object storage is not configured; CI cannot be enabled'
++  no-tip-refusal  'ref has no tip; push a commit before CI-protecting it'
++  linked-refusal  'CI protection is not available for desk-linked repositories in this release'
::  a viewer's download link lives five minutes (D2: at most fifteen)
++  log-link-expiry  ~m5
--
=|  state-0:ci
=*  state  -
=|  polls=(map daemon-id:ci poll)
%-  agent:dbug
=<
|_  =bowl:gall
+*  this  .
    def   ~(. (default-agent this %|) bowl)
    hc    ~(. +> bowl)
::
++  on-init
  ^-  (quip card _this)
  [~[connect-card:hc] this]
::
++  on-save
  !>(state)
::
++  on-load
  |=  old=vase
  ^-  (quip card _this)
  =/  loaded=state-0:ci
    ?+  -.q.old  !!
      %0  !<(state-0:ci old)
    ==
  [~[connect-card:hc] this(state loaded, polls ~)]
::
++  on-poke
  |=  [=mark =vase]
  ^-  (quip card _this)
  =/  =out:hc
    ?+    mark  ~|([%urgit-ci-bad-mark mark] !!)
        %ci-action
      ?>  =(src.bowl our.bowl)
      (handle-action:hc !<(action:ci vase))
    ::
        %handle-http-request
      =+  !<([eyre-id=@ta req=inbound-request:eyre] vase)
      (handle-http:hc eyre-id req)
    ==
  [cards.out this(state state.out, polls polls.out)]
::
++  on-watch
  |=  =path
  ^-  (quip card _this)
  ?>  ?=([%http-response @ ~] path)
  `this
::
::  eyre leaves /http-response/<id> when the client's connection closes:
::  a long-poll whose daemon went away is forgotten at once, so a later
::  assignment is not answered into a dead connection and lost until its
::  deadline (a daemon stopped and restarted within the poll window).
::
++  on-leave
  |=  =path
  ^-  (quip card _this)
  ?.  ?=([%http-response @ ~] path)  (on-leave:def path)
  =/  gone=@ta  i.t.path
  =.  polls
    %-  malt
    %+  skip  ~(tap by polls)
    |=([* p=poll] =(eyre-id.p gone))
  `this
::
::  every route %urgit reads answers a loobean; none ever answers [~ ~],
::  because an empty answer kills the reading event before any trap runs.
::
++  on-peek
  |=  =path
  ^-  (unit (unit cage))
  ?+    path  (on-peek:def path)
      [%x %state %version ~]
    ``noun+!>(-.state)
  ::
      [%x %eligible @ @ @ ~]
    ``noun+!>((eligible:hc i.t.t.path i.t.t.t.path i.t.t.t.t.path))
  ::
      [%x %ci-protected @ @ ~]
    =/  repo=@t  (decode-segment:hc i.t.t.path)
    =/  ref=@t  (decode-segment:hc i.t.t.t.path)
    ``noun+!>((~(has in ci-protected) [repo ref]))
  ::
      [%x %polls ~]        ``noun+!>(polls)
      [%x %candidates ~]   ``noun+!>(candidates)
      [%x %daemons ~]      ``noun+!>(daemons)
      [%x %assignments ~]  ``noun+!>(assignments)
      [%x %attempts ~]     ``noun+!>(attempts)
  ::
      [%x %candidate @ ~]
    =/  id=(unit @uv)  (slaw %uv i.t.t.path)
    ``noun+!>(?~(id ~ (~(get by candidates) u.id)))
  ::
      [%x %attempt @ ~]
    =/  id=(unit @uv)  (slaw %uv i.t.t.path)
    ``noun+!>(?~(id ~ (~(get by attempts) u.id)))
  ::
      [%x %daemon @ ~]
    =/  id=(unit @uv)  (slaw %uv i.t.t.path)
    ``noun+!>(?~(id ~ (~(get by daemons) u.id)))
  ::
      ::  a download request for an attempt: ~ when the store is not
      ::  configured, the attempt is unknown, or the key's trust class is
      ::  not the attempt's own
      ::
      [%x %sign-get @ @ @ ~]
    =/  id=(unit @uv)  (slaw %uv i.t.t.path)
    =/  =trust:ci
      ?:(=(%trusted i.t.t.t.path) %trusted %untrusted)
    =/  name=@t  (decode-segment:hc i.t.t.t.t.path)
    =/  found=(unit attempt:ci)  ?~(id ~ (~(get by attempts) u.id))
    ?~  found  ``noun+!>(`(unit @t)`~)
    =/  signed=(unit signed-request:git-storage)
      %:  sign-get:ci-storage
        (read-settings:ci-storage our.bowl now.bowl)
        trust.u.found
        (candidate-repo:hc candidate.u.found)
        (scot %uv candidate.u.found)
        (scot %uv id.u.found)
        trust
        name
        now.bowl
      ==
    ``noun+!>(`(unit @t)`?~(signed ~ `url.u.signed))
  ::
      ::  a presigned download URL for an attempt's object, expiring after
      ::  the given seconds (bounded to fifteen minutes): the same signer
      ::  the GET attempt/<id>/log route answers with, with the expiry
      ::  chosen so a row can watch a link expire
      ::
      [%x %presign-get @ @ @ @ ~]
    =/  id=(unit @uv)  (slaw %uv i.t.t.path)
    =/  =trust:ci
      ?:(=(%trusted i.t.t.t.path) %trusted %untrusted)
    =/  name=@t  (decode-segment:hc i.t.t.t.t.path)
    =/  seconds=(unit @ud)  (slaw %ud i.t.t.t.t.t.path)
    =/  found=(unit attempt:ci)  ?~(id ~ (~(get by attempts) u.id))
    ?~  found  ``noun+!>(`(unit @t)`~)
    ?~  seconds  ``noun+!>(`(unit @t)`~)
    ``noun+!>((presign-log:hc u.found trust name (mul u.seconds ~s1)))
  ==
::
++  on-agent
  |=  [=wire =sign:agent:gall]
  ^-  (quip card _this)
  ?.  ?=([%urgit *] wire)  (on-agent:def wire sign)
  ?.  ?=(%poke-ack -.sign)  (on-agent:def wire sign)
  ?~  p.sign  `this
  %-  (slog leaf+"urgit-ci: %urgit rejected {<wire>}" u.p.sign)
  `this
::
++  on-arvo
  |=  [=wire =sign-arvo]
  ^-  (quip card _this)
  ?+    wire  (on-arvo:def wire sign-arvo)
      [%eyre *]  `this
  ::
      ::  a long-poll that saw no assignment closes with 204
      ::
      [%poll @ @ ~]
    ?.  ?=([%behn %wake *] sign-arvo)  (on-arvo:def wire sign-arvo)
    ?^  error.sign-arvo  `this
    =/  daemon=(unit @uv)  (slaw %uv i.t.wire)
    ?~  daemon  `this
    =/  waiting=(unit poll)  (~(get by polls) u.daemon)
    ?~  waiting  `this
    ?.  =(eyre-id.u.waiting i.t.t.wire)  `this
    =.  polls  (~(del by polls) u.daemon)
    [(give-empty:hc eyre-id.u.waiting 204) this]
  ::
      ::  an attempt with no result by its deadline is an infrastructure
      ::  error; its candidate becomes unknown, never passed
      ::
      [%deadline @ ~]
    ?.  ?=([%behn %wake *] sign-arvo)  (on-arvo:def wire sign-arvo)
    ?^  error.sign-arvo  `this
    =/  id=(unit @uv)  (slaw %uv i.t.wire)
    ?~  id  `this
    =/  found=(unit attempt:ci)  (~(get by attempts) u.id)
    ?~  found  `this
    ?.  =(%running status.u.found)  `this
    =/  closed=_state
      (close-attempt:hc u.found [%infrastructure-error 'no result arrived before the deadline'])
    =.  state  closed
    =/  =out:hc  (after-close:hc candidate.u.found)
    [cards.out this(state state.out, polls polls.out)]
  ==
::
++  on-fail  on-fail:def
--
::
::  the helper door: everything below reads and writes the same state the
::  agent holds and hands back the cards to emit with the new state.
::
|_  =bowl:gall
+$  out  [cards=(list card) state=_state polls=_polls]
::
++  emit
  |=  cards=(list card)
  ^-  out
  [cards state polls]
::
++  connect-card
  ^-  card
  [%pass /eyre/connect %arvo %e %connect [~ /apps/urgit/api/ci] %urgit-ci]
::
++  candidate-id
  |=  [repo=@t ref=@t head=oid:git base=oid:git]
  ^-  candidate-id:ci
  (sham [repo ref head base])
::
++  candidate-repo
  |=  id=candidate-id:ci
  ^-  @t
  =/  found=(unit candidate:ci)  (~(get by candidates) id)
  ?~(found '' repo.u.found)
::
++  scratch-ref
  |=  id=candidate-id:ci
  ^-  @t
  (rap 3 ~['refs/ci/candidate/' (scot %uv id)])
::
::  scry segments carry repository names and refs as (scot %t ...) so a
::  ref with slashes fits one path segment; a raw segment is taken as-is
::
++  decode-segment
  |=  segment=@t
  ^-  @t
  (fall (slaw %t segment) segment)
::
++  parse-oid
  |=  text=@t
  ^-  (unit oid:git)
  ?.  =(40 (met 3 text))  ~
  (oid-at:git-protocol [40 text] 0)
::
::  the landing rule: an object is eligible only when it is the
::  materialized candidate of a passed candidate for exactly this
::  repository and ref.  everything else is no.
::
++  eligible
  |=  [repo-segment=@t ref-segment=@t oid-segment=@t]
  ^-  ?
  =/  repo=@t  (decode-segment repo-segment)
  =/  ref=@t  (decode-segment ref-segment)
  =/  oid=(unit oid:git)  (parse-oid (decode-segment oid-segment))
  ?~  oid  %.n
  %+  lien  ~(tap by candidates)
  |=  [* c=candidate:ci]
  ?&  =(repo repo.c)
      =(ref ref.c)
      =(%passed status.c)
      ?=(^ candidate.c)
      =(u.oid u.candidate.c)
  ==
::
++  urgit-poke
  |=  [=wire act=action:ci]
  ^-  card
  [%pass (weld /urgit wire) %agent [our.bowl %urgit] %poke %ci-action !>(act)]
::
++  urgit-git-poke
  |=  [=wire act=action:git]
  ^-  card
  [%pass (weld /urgit wire) %agent [our.bowl %urgit] %poke %git-action !>(act)]
::
::  the three reads of %urgit, each guarded the way %urgit guards its own
::  reads of %urgit-ci: gall's %gu liveness first, under mule, and the %gx
::  read only once it says yes, because a bare %gx answered [~ ~] kills
::  the event even under mule.  ~ means "could not read": every caller
::  refuses on it.
::
++  urgit-peek
  |=  rest=path
  ^-  (unit *)
  =/  prefix=path  /(scot %p our.bowl)/urgit/(scot %da now.bowl)
  =/  live=(each ? tang)
    %-  mule  |.
    .^(? %gu (weld prefix /$))
  ?.  ?&(?=(%& -.live) p.live)  ~
  =/  raw=(each * tang)
    %-  mule  |.
    .^(* %gx (weld prefix (snoc rest %noun)))
  ?.  ?=(%& -.raw)  ~
  `p.raw
::
++  ref-tip
  |=  [repo=@t ref=@t]
  ^-  (unit (unit [tip=oid:git linked=?]))
  =/  raw=(unit *)  (urgit-peek /ci-ref/(scot %t repo)/(scot %t ref))
  ?~  raw  ~
  `;;((unit [tip=oid:git linked=?]) u.raw)
::
++  tree-at
  |=  [repo=@t oid=oid:git under=@t]
  ^-  (unit (list path))
  =/  raw=(unit *)
    (urgit-peek /ci-tree/(scot %t repo)/(scot %t (oid-text:git-codec oid))/(scot %t under))
  ?~  raw  ~
  ;;((unit (list path)) u.raw)
::
++  file-at
  |=  [repo=@t oid=oid:git file=@t]
  ^-  (unit octs)
  =/  raw=(unit *)
    (urgit-peek /ci-file/(scot %t repo)/(scot %t (oid-text:git-codec oid))/(scot %t file))
  ?~  raw  ~
  ;;((unit octs) u.raw)
::
++  handle-action
  |=  act=action:ci
  ^-  out
  ?-    -.act
      ::  CI-EMPTY-REF-1-A: every CI-protected ref has a tip, so the gate's
      ::  "no tip to stage against" branch is unreachable.  CI-LINKED-DESK-P1:
      ::  a repository bound to a Clay desk cannot be CI-protected, because
      ::  its ref write is not one event.  un-protect never checks.
      ::
      %set-ci-protected
    ?.  protected.act
      =.  ci-protected  (~(del in ci-protected) [repo.act ref.act])
      (emit ~)
    ?~  (read-settings:ci-storage our.bowl now.bowl)
      ~|  storage-refusal
      !!
    ?:  =('refs/ci/' (end [3 8] ref.act))
      ~|  'refs/ci/* are scratch refs and cannot be CI-protected'
      !!
    =/  tip=(unit (unit [tip=oid:git linked=?]))  (ref-tip repo.act ref.act)
    ?~  tip
      ~|  'ci: %urgit is not running; the ref cannot be checked'
      !!
    ?~  u.tip
      ~|  no-tip-refusal
      !!
    ?:  linked.u.u.tip
      ~|  linked-refusal
      !!
    =.  ci-protected  (~(put in ci-protected) [repo.act ref.act])
    (emit ~)
  ::
      ::  a staged head is materialized at once (D5); a head staged again
      ::  against the same base is the same candidate and keeps its record
      ::
      %stage-candidate
    =/  id=candidate-id:ci  (candidate-id repo.act ref.act head.act base.act)
    =/  existing=(unit candidate:ci)  (~(get by candidates) id)
    ?^  existing
      =.  candidates  (~(put by candidates) id u.existing(updated now.bowl))
      (emit ~)
    =/  next=candidate:ci
      :*  id  repo.act  ref.act  head.act  base.act
          ~  %.n  %pending  ~  ~  ~  ~  actor.act  via.act  trust.act  pull.act
          now.bowl  now.bowl
      ==
    =.  candidates  (~(put by candidates) id next)
    %-  emit
    :_  ~
    %+  urgit-poke  /materialize/(scot %uv id)
    [%materialize-candidate repo.act ref.act head.act base.act]
  ::
      %materialize
    =/  found=(unit candidate:ci)  (~(get by candidates) candidate.act)
    ?~  found  ~|('no such candidate' !!)
    ?^  candidate.u.found  ~|('candidate is already materialized' !!)
    %-  emit
    :_  ~
    %+  urgit-poke  /materialize/(scot %uv candidate.act)
    [%materialize-candidate repo.u.found ref.u.found head.u.found base.u.found]
  ::
      ::  the repository's policy for untrusted revisions (D3): %approval
      ::  holds them until a writer approves; %restricted plans and runs
      ::  them at once with no credentials and no landing
      ::
      %set-untrusted-policy
    =.  policies  (~(put by policies) repo.act policy.act)
    (emit ~)
  ::
      %approve-candidate
    ~|  'approval arrives with the trust stage'
    !!
  ::
      %rerun-candidate
    ~|  'rerun arrives with the trust stage'
    !!
  ::
      %set-credential
    ~|  'credentials arrive with the credential stage'
    !!
  ::
      %delete-credential
    ~|  'credentials arrive with the credential stage'
    !!
  ::
      %rotate-ci-key
    ~|  'the signing key arrives with the signing stage'
    !!
  ::
      %materialize-candidate
    ~|  '%materialize-candidate is a poke on %urgit, not %urgit-ci'
    !!
  ::
      %land-candidate
    ~|  '%land-candidate is a poke on %urgit, not %urgit-ci'
    !!
  ::
      ::  the first materialization wins: a candidate is never regenerated
      ::  after it has been recorded, let alone after it has been tested.
      ::  a ready candidate is planned at once (D4).
      ::
      %candidate-ready
    =/  id=candidate-id:ci  (candidate-id repo.act ref.act head.act base.act)
    =/  found=(unit candidate:ci)  (~(get by candidates) id)
    ?~  found  (emit ~)
    ?^  candidate.u.found  (emit ~)
    =/  next=candidate:ci
      u.found(candidate `candidate.act, conflict %.n, updated now.bowl)
    =.  candidates  (~(put by candidates) id next)
    schedule
  ::
      ::  a head that cannot be merged onto its base cannot be tested: the
      ::  candidate fails with the reason rather than waiting forever
      ::
      %candidate-conflict
    =/  id=candidate-id:ci  (candidate-id repo.act ref.act head.act base.act)
    =/  found=(unit candidate:ci)  (~(get by candidates) id)
    ?~  found  (emit ~)
    ?^  candidate.u.found  (emit ~)
    =.  candidates
      %+  ~(put by candidates)  id
      %=  u.found
        conflict        %.y
        status          %failed
        verdict-reason  `'candidate could not be materialized: the source conflicts with the destination'
        updated         now.bowl
      ==
    (emit ~)
  ::
      %mint-enroll-token
    =/  token-hash=@  (shas %ci-enroll token.act)
    =/  id=daemon-id:ci  (sham [%ci-daemon token-hash])
    ?:  (~(has by daemons) id)  ~|('enroll token already minted' !!)
    =.  daemons  (~(put by daemons) id [id token-hash ~ now.bowl ~ ~ 1 '' ~])
    (emit ~)
  ::
      ::  the operator's assignment: a plan, or one named job, on one
      ::  named daemon, capacity notwithstanding.  the harness drives this;
      ::  an operator re-runs a job with it.
      ::
      %assign
    =/  found=(unit candidate:ci)  (~(get by candidates) candidate.act)
    ?~  found  ~|('no such candidate' !!)
    ?~  candidate.u.found  ~|('candidate is not materialized' !!)
    =/  runner=(unit daemon:ci)  (~(get by daemons) daemon.act)
    ?~  runner  ~|('no such daemon' !!)
    ?~  enrolled.u.runner  ~|('daemon is not enrolled' !!)
    ?:  ?&(?=(%plan kind.act) ?=(^ plan.u.found))
      ~|('candidate already has a plan' !!)
    ?:  ?&(?=(%job kind.act) |(?=(~ workflow.act) ?=(~ job.act)))
      ~|('a job assignment names a workflow file and a job id' !!)
    =/  deadline=@dr
      %+  fall  deadline.act
      ?:(?=(%plan kind.act) plan-deadline default-deadline)
    =^  made  state
      (create-attempt candidate.act daemon.act kind.act workflow.act job.act deadline)
    =/  delivered=out  (deliver daemon.act)
    =.  state  state.delivered
    =.  polls  polls.delivered
    ::  a re-run of a closed candidate needs its objects reachable again:
    ::  the scratch ref released at the terminal status is set once more
    ::  through the existing %set-ref path, before the assignment goes out
    ::
    =/  restore=card
      %+  urgit-git-poke  /scratch/(scot %uv candidate.act)
      [%set-ref repo.u.found (scratch-ref candidate.act) u.candidate.u.found]
    (emit [restore timer.made cards.delivered])
  ::
      %landed
    =/  found=(unit candidate:ci)  (~(get by candidates) candidate.act)
    ?~  found  (emit ~)
    =.  candidates
      (~(put by candidates) candidate.act u.found(verdict-reason `'landed', updated now.bowl))
    (emit ~)
  ::
      ::  a stale destination leaves the candidate passed and unlanded;
      ::  the pusher rebases and pushes again (P17)
      ::
      %land-refused
    =/  found=(unit candidate:ci)  (~(get by candidates) candidate.act)
    ?~  found  (emit ~)
    =.  candidates
      (~(put by candidates) candidate.act u.found(verdict-reason `reason.act, updated now.bowl))
    (emit ~)
  ==
::
::  a new attempt and its assignment, undelivered, and the deadline timer
::  the caller emits.  the daemon's running set grows here and shrinks in
::  +close-attempt.
::
++  create-attempt
  |=  $:  candidate=candidate-id:ci
          daemon=daemon-id:ci
          =kind:ci
          workflow=(unit @t)
          job=(unit @t)
          deadline=@dr
      ==
  ^-  [[attempt-id:ci timer=card] _state]
  =/  found=candidate:ci  (~(got by candidates) candidate)
  =/  attempt-id=attempt-id:ci
    (sham [%ci-attempt candidate daemon kind workflow job now.bowl (lent attempts.found)])
  =/  assignment-id=assignment-id:ci  (sham [%ci-assignment attempt-id])
  ::  an attempt carries its candidate's trust class (D3): every key it
  ::  writes and every grant it may receive follow from it
  ::
  =/  =attempt:ci
    :*  attempt-id  candidate  assignment-id  daemon
        trust.found  kind  workflow  job  %running  0  ~  ~  ~  ~  ~  ~  now.bowl  ~
    ==
  =/  =assignment:ci
    :*  assignment-id  candidate  daemon  attempt-id
        trust.found  kind  workflow  job  deadline  now.bowl  ~
    ==
  =.  attempts  (~(put by attempts) attempt-id attempt)
  =.  assignments  (~(put by assignments) assignment-id assignment)
  =.  candidates
    %+  ~(put by candidates)  candidate
    found(status %pending, attempts [attempt-id attempts.found], updated now.bowl)
  =.  daemons
    =/  runner=(unit daemon:ci)  (~(get by daemons) daemon)
    ?~  runner  daemons
    (~(put by daemons) daemon u.runner(running (~(put in running.u.runner) attempt-id)))
  =/  timer=card
    [%pass /deadline/(scot %uv attempt-id) %arvo %b %wait (add now.bowl deadline)]
  [[attempt-id timer] state]
::
::  a decided skip is recorded as an attempt no daemon ran: %skipped, with
::  the reason, so the candidate's record shows why the job did not run
::
++  record-skip
  |=  [candidate=candidate-id:ci =job:ci reason=@t]
  ^-  _state
  =/  found=candidate:ci  (~(got by candidates) candidate)
  =/  attempt-id=attempt-id:ci
    (sham [%ci-skip candidate workflow.job id.job now.bowl (lent attempts.found)])
  =/  =attempt:ci
    :*  attempt-id  candidate  0v0  0v0
        trust.found  %job  `workflow.job  `id.job  %skipped  0  ~  ~  ~  `reason  ~  ~  now.bowl  `now.bowl
    ==
  =.  attempts  (~(put by attempts) attempt-id attempt)
  =.  candidates
    %+  ~(put by candidates)  candidate
    found(attempts [attempt-id attempts.found], updated now.bowl)
  state
::
::  a daemon holding the channel open receives its oldest undelivered
::  assignment now; the rest wait for its next poll, one per request
::
++  deliver
  |=  daemon=daemon-id:ci
  ^-  out
  =/  waiting=(unit poll)  (~(get by polls) daemon)
  ?~  waiting  (emit ~)
  =/  undelivered=(list assignment:ci)
    %+  sort
      %+  skim  ~(val by assignments)
      |=  =assignment:ci
      ?.  =(daemon.assignment daemon)  %.n
      ?^  delivered.assignment  %.n
      =/  running=(unit attempt:ci)  (~(get by attempts) attempt.assignment)
      ?&(?=(^ running) =(%running status.u.running))
    |=([a=assignment:ci b=assignment:ci] (lth assigned.a assigned.b))
  ?~  undelivered  (emit ~)
  =/  =assignment:ci  i.undelivered
  =/  =attempt:ci  (~(got by attempts) attempt.assignment)
  =/  =candidate:ci  (~(got by candidates) candidate.assignment)
  =.  assignment  assignment(delivered `now.bowl)
  =.  assignments  (~(put by assignments) id.assignment assignment)
  =.  polls  (~(del by polls) daemon)
  (emit (give-json eyre-id.u.waiting 200 (assignment-json assignment candidate attempt)))
::
::  daemon selection (D6): enrolled, seen within five minutes, below its
::  capacity; fewest running first, then the oldest enrollment
::
++  select-daemon
  ^-  (unit daemon-id:ci)
  =/  able=(list daemon:ci)
    %+  skim  ~(val by daemons)
    |=  =daemon:ci
    ?&  ?=(^ enrolled.daemon)
        ?=(^ bearer-hash.daemon)
        ?=(^ last-seen.daemon)
        (lte (sub now.bowl (min now.bowl u.last-seen.daemon)) stale-after)
        (lth ~(wyt in running.daemon) capacity.daemon)
    ==
  ?~  able  ~
  =/  sorted=(list daemon:ci)
    %+  sort  able
    |=  [a=daemon:ci b=daemon:ci]
    =/  ra=@ud  ~(wyt in running.a)
    =/  rb=@ud  ~(wyt in running.b)
    ?:  !=(ra rb)  (lth ra rb)
    (lth (fall enrolled.a now.bowl) (fall enrolled.b now.bowl))
  ?~  sorted  ~
  `id.i.sorted
::
::  the scheduler (D4).  every pending, materialized candidate is
::  settled first: skips its plan decides are recorded and its verdict
::  recomputed.  then every runnable unit of work is assigned: a plan
::  for a candidate without one and without a plan attempt in flight;
::  each job the plan makes runnable that has no attempt yet.  work
::  waits when no daemon can take it and is offered again at the next
::  poll, enrollment, ready candidate or closed attempt.
::
++  schedule
  ^-  out
  =/  pending=(list candidate:ci)
    %+  skim  ~(val by candidates)
    |=  =candidate:ci
    ?&(=(%pending status.candidate) ?=(^ candidate.candidate) !conflict.candidate)
  =|  cards=(list card)
  =|  touched=(set daemon-id:ci)
  |-
  ?~  pending
    =/  daemons-to-answer=(list daemon-id:ci)  ~(tap in touched)
    |-
    ?~  daemons-to-answer  (emit cards)
    =/  delivered=out  (deliver i.daemons-to-answer)
    =.  state  state.delivered
    =.  polls  polls.delivered
    $(daemons-to-answer t.daemons-to-answer, cards (weld cards cards.delivered))
  =/  settled=out  (settle id.i.pending)
  =.  state  state.settled
  =.  cards  (weld cards cards.settled)
  =/  =candidate:ci  (~(got by candidates) id.i.pending)
  ?.  =(%pending status.candidate)  $(pending t.pending)
  ?~  plan.candidate
    ?:  (plan-in-flight candidate)  $(pending t.pending)
    =/  chosen=(unit daemon-id:ci)  select-daemon
    ?~  chosen  $(pending t.pending)
    =^  made  state
      (create-attempt id.candidate u.chosen %plan ~ ~ plan-deadline)
    %=  $
      pending  t.pending
      cards    (snoc cards timer.made)
      touched  (~(put in touched) u.chosen)
    ==
  =/  runnable=(list job:ci)  (runnable-jobs candidate)
  |-
  ?~  runnable  ^$(pending t.pending)
  =/  chosen=(unit daemon-id:ci)  select-daemon
  ?~  chosen  ^$(pending t.pending)
  =^  made  state
    %:  create-attempt
      id.candidate  u.chosen  %job
      `workflow.i.runnable  `id.i.runnable  default-deadline
    ==
  %=  $
    runnable  t.runnable
    cards     (snoc cards timer.made)
    touched   (~(put in touched) u.chosen)
  ==
::
++  plan-in-flight
  |=  =candidate:ci
  ^-  ?
  %+  lien  attempts.candidate
  |=  id=attempt-id:ci
  =/  found=(unit attempt:ci)  (~(get by attempts) id)
  ?&(?=(^ found) ?=(%plan kind.u.found) =(%running status.u.found))
::
::  the newest job attempt per [workflow id] is that job's standing; its
::  recorded set-outputs are what a dependent's `if` reads
::
++  standings
  |=  =candidate:ci
  ^-  [standings=(map key:ci-plan standing:ci-plan) outputs=(map key:ci-plan (map @t @t))]
  =/  newest=(map key:ci-plan attempt:ci)
    %+  roll  (flop attempts.candidate)
    |=  [id=attempt-id:ci acc=(map key:ci-plan attempt:ci)]
    =/  found=(unit attempt:ci)  (~(get by attempts) id)
    ?~  found  acc
    ?.  ?=(%job kind.u.found)  acc
    ?~  workflow.u.found  acc
    ?~  job.u.found  acc
    (~(put by acc) [u.workflow.u.found u.job.u.found] u.found)
  :-  %-  ~(run by newest)
      |=  =attempt:ci
      ^-  standing:ci-plan
      ?-  status.attempt
        %passed                %passed
        %failed                %failed
        %skipped               %skipped
        %running               %running
        %infrastructure-error  %unknown
      ==
  (~(run by newest) |=(=attempt:ci outputs.attempt))
::
++  runnable-jobs
  |=  =candidate:ci
  ^-  (list job:ci)
  ?~  plan.candidate  ~
  =/  known  (standings candidate)
  %+  skim  u.plan.candidate
  |=  =job:ci
  ?:  (~(has by standings.known) [workflow.job id.job])  %.n
  =(%run -:(decide:ci-plan job standings.known outputs.known))
::
::  one candidate's verdict from its record: over the plan when it has
::  one (skips decided now are recorded first), else from its newest
::  attempt, the P0 rule the harness drives by hand.  a verdict that
::  becomes terminal releases the scratch ref; a passed one asks %urgit
::  to land the candidate at once (D14).
::
++  settle
  |=  id=candidate-id:ci
  ^-  out
  =/  found=(unit candidate:ci)  (~(get by candidates) id)
  ?~  found  (emit ~)
  =/  before=candidate-status:ci  status.u.found
  =.  state
    ?~  plan.u.found  state
    =/  known  (standings u.found)
    %+  roll  u.plan.u.found
    |=  [=job:ci acc=_state]
    =.  state  acc
    ?:  (~(has by standings.known) [workflow.job id.job])  state
    =/  =decision:ci-plan  (decide:ci-plan job standings.known outputs.known)
    ?+  -.decision  state
      %skip     (record-skip id job reason.decision)
      %invalid  (record-skip id job reason.decision)
    ==
  =/  =candidate:ci  (~(got by candidates) id)
  =/  verdict=[status=candidate-status:ci reason=(unit @t)]
    ?^  plan.candidate
      (verdict:ci-plan u.plan.candidate standings:(standings candidate))
    =/  newest=(unit attempt:ci)
      ?~  attempts.candidate  ~
      (~(get by attempts) i.attempts.candidate)
    ?~  newest  [%pending ~]
    ?-  status.u.newest
      %passed                [%passed ~]
      %failed                [%failed reason.u.newest]
      %skipped               [%pending ~]
      %running               [%pending ~]
      %infrastructure-error  [%unknown reason.u.newest]
    ==
  =/  next=candidate:ci
    %=  candidate
      status          status.verdict
      verdict-reason  ?:(=(before status.verdict) verdict-reason.candidate reason.verdict)
      updated         now.bowl
    ==
  =.  candidates  (~(put by candidates) id next)
  ?:  =(before status.verdict)  (emit ~)
  ?:  =(%pending status.verdict)  (emit ~)
  =/  release=card
    (urgit-git-poke /release/(scot %uv id) [%delete-ref repo.next (scratch-ref id)])
  ?.  =(%passed status.verdict)  (emit ~[release])
  (emit (snoc (land-cards next) release))
::
++  land-cards
  |=  =candidate:ci
  ^-  (list card)
  ?~  candidate.candidate  ~
  :_  ~
  %+  urgit-poke  /land/(scot %uv id.candidate)
  [%land-candidate id.candidate repo.candidate ref.candidate u.candidate.candidate base.candidate pull.candidate]
::
::  after an attempt closes: its candidate is settled whatever its status
::  (a re-run changes a verdict), then the scheduler runs for everything
::
++  after-close
  |=  id=candidate-id:ci
  ^-  out
  =/  settled=out  (settle id)
  =.  state  state.settled
  =/  scheduled=out  schedule
  [(weld cards.settled cards.scheduled) state.scheduled polls.scheduled]
::
::  an attempt closes once.  a job result closes it passed or failed; an
::  invalid plan closes it failed with the reason; an infrastructure error
::  closes it unknown for the candidate.  the daemon's running set lets
::  the attempt go.  the candidate's verdict is +settle's, after this.
::
++  close-attempt
  |=  [=attempt:ci =attempt-result:ci]
  ^-  _state
  =/  status=attempt-status:ci
    ?-  -.attempt-result
      %job-result            ?:(=(%success result.attempt-result) %passed %failed)
      %plan-invalid          %failed
      %infrastructure-error  %infrastructure-error
    ==
  =/  reason=(unit @t)
    ?-  -.attempt-result
      %job-result            ~
      %plan-invalid          `(rap 3 ~['plan-invalid: ' message.attempt-result])
      %infrastructure-error  `message.attempt-result
    ==
  =/  closed=attempt:ci
    attempt(status status, result `attempt-result, reason reason, finished `now.bowl)
  =.  attempts  (~(put by attempts) id.attempt closed)
  =.  daemons
    =/  runner=(unit daemon:ci)  (~(get by daemons) daemon.attempt)
    ?~  runner  daemons
    (~(put by daemons) daemon.attempt u.runner(running (~(del in running.u.runner) id.attempt)))
  state
::
::  http
::
++  give-json
  |=  [eyre-id=@ta status=@ud jon=json]
  ^-  (list card)
  %+  give-simple-payload:app:server  eyre-id
  :_  `(json-to-octs:server jon)
  :-  status
  :~  ['content-type' 'application/json; charset=utf-8']
      ['cache-control' 'no-store']
  ==
::
++  give-error
  |=  [eyre-id=@ta status=@ud message=@t]
  ^-  (list card)
  (give-json eyre-id status (pairs:enjs:format ~[['error' s+message]]))
::
++  give-empty
  |=  [eyre-id=@ta status=@ud]
  ^-  (list card)
  %+  give-simple-payload:app:server  eyre-id
  [[status ~[['cache-control' 'no-store']]] ~]
::
++  body-json
  |=  req=inbound-request:eyre
  ^-  (unit json)
  ?~  body.request.req  ~
  (de:json:html q.u.body.request.req)
::
++  string-at
  |=  [key=@t jon=json]
  ^-  (unit @t)
  ?.  ?=([%o *] jon)  ~
  =/  value=(unit json)  (~(get by p.jon) key)
  ?~  value  ~
  ?.  ?=([%s *] u.value)  ~
  `p.u.value
::
++  number-at
  |=  [key=@t jon=json]
  ^-  (unit (unit @ud))
  ?.  ?=([%o *] jon)  `~
  =/  value=(unit json)  (~(get by p.jon) key)
  ?~  value  `~
  ?.  ?=([%n *] u.value)  ~
  ::  a JSON number has no thousands dots: dim, not %ud (dem:ag wants them)
  ::
  =/  parsed=(unit @ud)  (rush p.u.value dim:ag)
  ?~  parsed  ~
  `parsed
::
::  a daemon presents its bearer as `x-ci-bearer: <@uv>`.  it cannot use
::  `Authorization: Bearer`: eyre reads that header itself as a session
::  token and answers 401 before the request reaches any agent.  only
::  hashes are compared, and only against the daemon the route names.  a
::  logged-in ship session is always accepted.
::
++  presented-bearer-hash
  |=  req=inbound-request:eyre
  ^-  (unit @)
  =/  header=(unit @t)
    (get-header:http 'x-ci-bearer' header-list.request.req)
  ?~  header  ~
  =/  bearer=(unit @uv)  (slaw %uv u.header)
  ?~  bearer  ~
  `(shas %ci-bearer u.bearer)
::
++  daemon-authorized
  |=  [req=inbound-request:eyre =daemon:ci]
  ^-  ?
  ?:  authenticated.req  %.y
  ?~  bearer-hash.daemon  %.n
  =/  presented=(unit @)  (presented-bearer-hash req)
  ?~  presented  %.n
  =(u.bearer-hash.daemon u.presented)
::
++  attempt-authorized
  |=  [req=inbound-request:eyre =attempt:ci]
  ^-  ?
  =/  found=(unit daemon:ci)  (~(get by daemons) daemon.attempt)
  ?~  found  authenticated.req
  (daemon-authorized req u.found)
::
++  touch-daemon
  |=  id=daemon-id:ci
  ^-  _daemons
  =/  found=(unit daemon:ci)  (~(get by daemons) id)
  ?~  found  daemons
  (~(put by daemons) id u.found(last-seen `now.bowl))
::
::  the assignment as the daemon reads it.  a job assignment carries the
::  ship's recorded outputs of every job in its `needs` (CI-PROJECT-1):
::  the daemon runs act on a single-job projection, so act never sees the
::  prerequisites; the ship, which admitted the job, hands their outputs
::  down as `{"<job>": {"<name>": "<value>"}}`.
::
++  assignment-json
  |=  [=assignment:ci =candidate:ci =attempt:ci]
  ^-  json
  =/  oid=@t
    ?~  candidate.candidate  ''
    (oid-text:git-codec u.candidate.candidate)
  =/  prereq-outputs=json
    ?.  ?=(%job kind.assignment)  ~
    ?~  workflow.assignment  ~
    ?~  job.assignment  ~
    ?~  plan.candidate  ~
    =/  planned=(unit job:ci)
      =/  wf=@t  u.workflow.assignment
      =/  id=@t  u.job.assignment
      |-
      ?~  u.plan.candidate  ~
      ?:  &(=(wf workflow.i.u.plan.candidate) =(id id.i.u.plan.candidate))
        `i.u.plan.candidate
      $(u.plan.candidate t.u.plan.candidate)
    ?~  planned  ~
    =/  known  (standings candidate)
    %-  pairs:enjs:format
    %+  turn  needs.u.planned
    |=  need=@t
    ^-  [@t json]
    :-  need
    %-  pairs:enjs:format
    %+  turn  ~(tap by (~(gut by outputs.known) [u.workflow.assignment need] ~))
    |=([name=@t value=@t] [name s+value])
  %-  pairs:enjs:format
  :_  ~
  :-  'assignment'
  %-  pairs:enjs:format
  :~  ['id' s+(scot %uv id.assignment)]
      ['attempt' s+(scot %uv attempt.assignment)]
      ['candidate' s+(scot %uv candidate.assignment)]
      ['repo' s+repo.candidate]
      ['ref' s+ref.candidate]
      ['oid' s+oid]
      ['head' s+(oid-text:git-codec head.candidate)]
      ['base' s+(oid-text:git-codec base.candidate)]
      ['trust' s+trust.assignment]
      ['kind' s+kind.assignment]
      ['workflow' ?~(workflow.assignment ~ s+u.workflow.assignment)]
      ['job' ?~(job.assignment ~ s+u.job.assignment)]
      ['prereq-outputs' prereq-outputs]
      ['deadline-seconds' (numb:enjs:format (div deadline.assignment ~s1))]
      ['assigned' s+(scot %da assigned.assignment)]
  ==
::
++  attempt-json
  |=  =attempt:ci
  ^-  json
  =/  candidate-status=@t
    =/  found=(unit candidate:ci)  (~(get by candidates) candidate.attempt)
    ?~(found 'unknown' status.u.found)
  %-  pairs:enjs:format
  :~  ['attempt' s+(scot %uv id.attempt)]
      ['status' s+status.attempt]
      ['kind' s+kind.attempt]
      ['workflow' ?~(workflow.attempt ~ s+u.workflow.attempt)]
      ['job' ?~(job.attempt ~ s+u.job.attempt)]
      ['events' (numb:enjs:format events.attempt)]
      ['job-result' ?~(job-result.attempt ~ s+u.job-result.attempt)]
      ['reason' ?~(reason.attempt ~ s+u.reason.attempt)]
      ['projection-name' ?~(projection-name.attempt ~ s+u.projection-name.attempt)]
      ['trust' s+trust.attempt]
      ['log' (object-ref-json log.attempt)]
      ['candidate' s+(scot %uv candidate.attempt)]
      ['candidate-status' s+candidate-status]
  ==
::
++  object-ref-json
  |=  ref=(unit object-ref:ci)
  ^-  json
  ?~  ref  ~
  %-  pairs:enjs:format
  :~  ['key' s+key.u.ref]
      ['size' (numb:enjs:format size.u.ref)]
      ['sha256' s+sha256.u.ref]
  ==
::
::  the object key an attempt's upload lands under: the attempt's own
::  trust class, never the daemon's choice (D2)
::
++  attempt-object-key
  |=  [=attempt:ci name=@t]
  ^-  @t
  %:  object-key:ci-storage
    (candidate-repo candidate.attempt)
    (scot %uv candidate.attempt)
    (scot %uv id.attempt)
    trust.attempt
    name
  ==
::
::  a presigned download link for one of an attempt's objects, in the
::  attempt's own trust class only (D2)
::
++  presign-log
  |=  [=attempt:ci =trust:ci name=@t expires=@dr]
  ^-  (unit @t)
  %:  presign-get:ci-storage
    (read-settings:ci-storage our.bowl now.bowl)
    trust.attempt
    (candidate-repo candidate.attempt)
    (scot %uv candidate.attempt)
    (scot %uv id.attempt)
    trust
    name
    expires
    now.bowl
  ==
::
++  handle-http
  |=  [eyre-id=@ta req=inbound-request:eyre]
  ^-  out
  =/  line=request-line:server  (parse-request-line:server url.request.req)
  =/  site=(list @t)  site.line
  =/  method=@tas  method.request.req
  ?:  ?=([%apps %urgit %api %ci %daemon %enroll ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-enroll eyre-id req)
  ?:  ?=([%apps %urgit %api %ci %daemon @ %assignment ~] site)
    ?.  =(%'GET' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-assignment-poll eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ ~] site)
    ?.  =(%'GET' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    ::  the id is the last segment here, so the request-line parser has
    ::  split its last dotted group off as an extension: rejoin it
    ::
    =/  segment=@t
      ?~  ext.line  i.t.t.t.t.t.site
      (rap 3 ~[i.t.t.t.t.t.site '.' u.ext.line])
    (handle-attempt-read eyre-id req segment)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %event ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-event eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %result ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-result eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %plan ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-plan eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %abandon ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-abandon eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %upload ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-upload eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %log ~] site)
    ?.  =(%'GET' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-log-read eyre-id req i.t.t.t.t.t.site)
  (emit (give-error eyre-id 404 'ci route not found'))
::
::  enrollment: the token is the credential.  its hash must match a
::  minted, not yet enrolled daemon record.  the bearer is derived from
::  the daemon id and the token, returned once, and stored only hashed.
::  the daemon reports its capacity (default 1) and its sandbox (D6); a
::  newly enrolled daemon may take work at once.
::
++  handle-enroll
  |=  [eyre-id=@ta req=inbound-request:eyre]
  ^-  out
  =/  jon=(unit json)  (body-json req)
  ?~  jon
    (emit (give-error eyre-id 400 'valid JSON body required'))
  =/  token-text=(unit @t)  (string-at 'token' u.jon)
  ?~  token-text
    (emit (give-error eyre-id 422 'token is required'))
  =/  token=(unit @uv)  (slaw %uv u.token-text)
  ?~  token
    (emit (give-error eyre-id 422 'token must be a @uv'))
  =/  capacity=(unit (unit @ud))  (number-at 'capacity' u.jon)
  ?~  capacity
    (emit (give-error eyre-id 422 'capacity must be a natural number'))
  ?:  ?&(?=(^ u.capacity) =(0 u.u.capacity))
    (emit (give-error eyre-id 422 'capacity must be at least 1'))
  =/  sandbox=@t  (fall (string-at 'sandbox' u.jon) '')
  =/  token-hash=@  (shas %ci-enroll u.token)
  =/  matches=(list daemon:ci)
    %+  skim  ~(val by daemons)
    |=  =daemon:ci
    ?&(=(token-hash token-hash.daemon) ?=(~ enrolled.daemon))
  ?~  matches
    (emit (give-error eyre-id 401 'enroll token is not recognized'))
  =/  =daemon:ci  i.matches
  =/  bearer=@uv  (shas %ci-daemon (jam [id.daemon u.token]))
  =/  bearer-hash=@  (shas %ci-bearer bearer)
  =/  next=daemon:ci
    %=  daemon
      bearer-hash  `bearer-hash
      enrolled     `now.bowl
      last-seen    `now.bowl
      capacity     (fall u.capacity 1)
      sandbox      sandbox
      running      ~
    ==
  =.  daemons  (~(put by daemons) id.daemon next)
  =/  scheduled=out  schedule
  =.  state  state.scheduled
  =.  polls  polls.scheduled
  %-  emit
  %+  weld  cards.scheduled
  %^  give-json  eyre-id  200
  %-  pairs:enjs:format
  :~  ['daemon-id' s+(scot %uv id.daemon)]
      ['bearer' s+(scot %uv bearer)]
      ['capacity' (numb:enjs:format (fall u.capacity 1))]
      ['sandbox' s+sandbox]
  ==
::
::  the assignment channel.  a polling daemon has capacity to offer, so
::  the scheduler runs first; then an undelivered assignment for a
::  running attempt answers at once, else the request is held for the
::  poll window and closes 204, unless an assignment arrives first.
::
::  an assignment answered into a poll whose daemon had already gone
::  (eyre reports a closed connection seconds later) would otherwise wait
::  out its whole deadline: a delivered assignment whose attempt shows no
::  activity two minutes on is offered again to its daemon's next poll.
::  the daemon ignores an attempt it is already running.
::
++  stale-delivery
  |=  =assignment:ci
  ^-  ?
  ?~  delivered.assignment  %.n
  ?.  (gte now.bowl (add u.delivered.assignment redeliver-after))  %.n
  =/  running=(unit attempt:ci)  (~(get by attempts) attempt.assignment)
  ?~  running  %.n
  ?&  =(%running status.u.running)
      =(0 events.u.running)
      ?~  found=(~(get by candidates) candidate.assignment)  %.n
      ?:(?=(%plan kind.assignment) ?=(~ plan.u.found) %.y)
  ==
::
++  handle-assignment-poll
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  daemon-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit daemon:ci)  ?~(daemon-id ~ (~(get by daemons) u.daemon-id))
  ::  a bearer for a daemon the ship does not know (its enrollment was
  ::  wiped with state-0) is a credential that authenticates nothing: 401
  ::
  ?~  found
    ?^  (presented-bearer-hash req)
      (emit (give-error eyre-id 401 'daemon authentication required'))
    (emit (give-error eyre-id 404 'no such daemon'))
  ?.  (daemon-authorized req u.found)
    (emit (give-error eyre-id 401 'daemon authentication required'))
  =.  daemons  (touch-daemon id.u.found)
  ::  the capacity a daemon reports as it waits (spec: "reports capacity,
  ::  and waits"): a restart with a new config takes effect at its next
  ::  poll, without a second enrollment
  ::
  =.  daemons
    =/  header=(unit @t)  (get-header:http 'x-ci-capacity' header-list.request.req)
    =/  reported=(unit @ud)  ?~(header ~ (slaw %ud u.header))
    ?~  reported  daemons
    ?:  =(0 u.reported)  daemons
    =/  runner=daemon:ci  (~(got by daemons) id.u.found)
    ?:  =(capacity.runner u.reported)  daemons
    (~(put by daemons) id.u.found runner(capacity u.reported))
  =/  scheduled=out  schedule
  =.  state  state.scheduled
  =.  polls  polls.scheduled
  =/  pending=(list assignment:ci)
    %+  skim  ~(val by assignments)
    |=  =assignment:ci
    ?.  =(daemon.assignment id.u.found)  %.n
    ?^  delivered.assignment  (stale-delivery assignment)
    =/  running=(unit attempt:ci)  (~(get by attempts) attempt.assignment)
    ?&(?=(^ running) =(%running status.u.running))
  ?^  pending
    =/  =assignment:ci  i.pending
    =/  =attempt:ci  (~(got by attempts) attempt.assignment)
    =/  =candidate:ci  (~(got by candidates) candidate.assignment)
    =.  assignment  assignment(delivered `now.bowl)
    =.  assignments  (~(put by assignments) id.assignment assignment)
    %-  emit
    %+  weld  cards.scheduled
    (give-json eyre-id 200 (assignment-json assignment candidate attempt))
  =/  previous=(unit poll)  (~(get by polls) id.u.found)
  =.  polls  (~(put by polls) id.u.found [eyre-id now.bowl])
  =/  closed=(list card)
    ?~(previous ~ (give-empty eyre-id.u.previous 204))
  =/  timer=card
    :*  %pass  /poll/(scot %uv id.u.found)/[eyre-id]
        %arvo  %b  %wait  (add now.bowl poll-window)
    ==
  (emit :(weld cards.scheduled closed ~[timer]))
::
::  a daemon reconciling its orphans after a restart asks what became of
::  an attempt (D7); the answer is the attempt's status, nothing more
::
++  handle-attempt-read
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?.  (attempt-authorized req u.found)
    (emit (give-error eyre-id 401 'attempt authentication required'))
  =.  daemons  (touch-daemon daemon.u.found)
  (emit (give-json eyre-id 200 (attempt-json u.found)))
::
::  one relayed act line.  validated and bounded by ci-event; a set-output
::  lands in the attempt's outputs and a jobResult is remembered so the
::  daemon cannot later claim a result it never relayed.  nothing else
::  about the event is kept.
::
++  handle-event
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?.  (attempt-authorized req u.found)
    (emit (give-error eyre-id 401 'attempt authentication required'))
  ?.  =(%running status.u.found)
    (emit (give-error eyre-id 409 'attempt is closed'))
  ?:  (gte events.u.found max-events:ci-event)
    (emit (give-error eyre-id 429 'attempt event limit reached'))
  =/  body=octs  ?~(body.request.req [0 0] u.body.request.req)
  =/  parsed=(each event:ci refusal:ci-event)  (parse:ci-event body)
  ?:  ?=(%| -.parsed)
    (emit (give-error eyre-id status.p.parsed message.p.parsed))
  =/  =event:ci  p.parsed
  ::  the tripwire for the daemon's single-job projection (CI-PROJECT-1):
  ::  a line from any job but the assigned one means act ran more than
  ::  the ship admitted
  ::
  ?:  ?&(?=(%job kind.u.found) ?=(^ job.u.found) !=(u.job.u.found job-id.event))
    (emit (give-error eyre-id 409 'event job does not match the assignment'))
  ::  the name act ran the job under, as act's own line says it:
  ::  `job` is `<workflow name>/<job id>` (CI-PROJECT-1.1)
  ::
  =/  seen-under=(unit @t)
    ?^  projection-name.u.found  projection-name.u.found
    =/  full=@ud  (met 3 job.event)
    =/  tail=@ud  +((met 3 job-id.event))
    ?.  (gth full tail)  ~
    `(end [3 (sub full tail)] job.event)
  =/  next=attempt:ci
    %=  u.found
      events           +(events.u.found)
      outputs          (record-output:ci-event outputs.u.found event)
      job-result       ?^(job-result.event job-result.event job-result.u.found)
      projection-name  seen-under
    ==
  =.  attempts  (~(put by attempts) id.next next)
  =.  daemons  (touch-daemon daemon.next)
  (emit (give-json eyre-id 202 (attempt-json next)))
::
::  the claimed result.  a job result is accepted only after the daemon
::  relayed a matching jobResult event; an infrastructure error is always
::  accepted and never reads as success.
::
++  handle-result
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?.  (attempt-authorized req u.found)
    (emit (give-error eyre-id 401 'attempt authentication required'))
  ?.  =(%running status.u.found)
    (emit (give-error eyre-id 409 'attempt is closed'))
  =/  jon=(unit json)  (body-json req)
  ?~  jon
    (emit (give-error eyre-id 400 'valid JSON body required'))
  =/  claimed=(unit @t)  (string-at 'job-result' u.jon)
  =/  infrastructure=(unit @t)  (string-at 'infrastructure-error' u.jon)
  ?^  claimed
    =/  result=(unit result:ci)
      ?+  u.claimed  ~
        %success    `%success
        %failure    `%failure
        %skipped    `%skipped
        %cancelled  `%cancelled
      ==
    ?~  result
      (emit (give-error eyre-id 422 'job-result must be success, failure, skipped or cancelled'))
    ?~  job-result.u.found
      (emit (give-error eyre-id 409 'no jobResult event was relayed for this attempt'))
    ?.  =(u.job-result.u.found u.result)
      (emit (give-error eyre-id 409 'job-result does not match the relayed jobResult event'))
    =/  named=(each (unit object-ref:ci) @t)  (result-log u.found u.jon)
    ?:  ?=(%| -.named)
      (emit (give-error eyre-id 422 p.named))
    =/  with-log=attempt:ci  u.found(log p.named)
    =.  attempts  (~(put by attempts) id.with-log with-log)
    =.  state  (close-attempt with-log [%job-result u.result])
    =/  closed=out  (after-close candidate.u.found)
    =.  state  state.closed
    =.  polls  polls.closed
    (emit (weld cards.closed (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found)))))
  ?^  infrastructure
    =.  state  (close-attempt u.found [%infrastructure-error u.infrastructure])
    =/  closed=out  (after-close candidate.u.found)
    =.  state  state.closed
    =.  polls  polls.closed
    (emit (weld cards.closed (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found)))))
  (emit (give-error eyre-id 422 'job-result or infrastructure-error is required'))
::
::  the log a result names (D1): `log: {size, sha256}` records the handle
::  of the finished act stream the daemon uploaded under log.jsonl before
::  it posted the result.  the key is the attempt's own, never the body's;
::  a result that names no log leaves the attempt without one, and the
::  verdict stands either way.
::
++  result-log
  |=  [=attempt:ci jon=json]
  ^-  (each (unit object-ref:ci) @t)
  ?.  ?=([%o *] jon)  [%& ~]
  =/  log=(unit json)  (~(get by p.jon) 'log')
  ?~  log  [%& ~]
  ?~  u.log  [%& ~]
  ?.  ?=([%o *] u.log)  [%| 'log must be an object with size and sha256']
  =/  size=(unit (unit @ud))  (number-at 'size' u.log)
  =/  sha=(unit @t)  (string-at 'sha256' u.log)
  ?.  ?&(?=(^ size) ?=(^ u.size))  [%| 'log.size must be a natural number']
  ?~  sha  [%| 'log.sha256 is required']
  ?.  (sha256-text-valid:ci-storage u.sha)  [%| 'log.sha256 must be 64 hex digits']
  [%& `[(attempt-object-key attempt 'log.jsonl') u.u.size u.sha]]
::
::  the daemon asks for an upload (D2): a header-authorized SigV4 PUT into
::  the attempt's own trust namespace, for one of the names the fence
::  allows, with the payload hash the daemon computed.  the ship never
::  sees the bytes.  an attempt that is no longer running has nothing to
::  upload; a store that is not configured cannot be uploaded to.
::
++  handle-upload
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?.  (attempt-authorized req u.found)
    (emit (give-error eyre-id 401 'attempt authentication required'))
  =/  jon=(unit json)  (body-json req)
  ?~  jon
    (emit (give-error eyre-id 400 'valid JSON body required'))
  =/  name=(unit @t)  (string-at 'name' u.jon)
  ?~  name
    (emit (give-error eyre-id 400 'name is required'))
  ?.  (upload-name-allowed:ci-storage u.name)
    (emit (give-error eyre-id 400 'name must be log.jsonl, summary.md or artifact/<file>'))
  =/  content-type=@t  (fall (string-at 'contentType' u.jon) 'application/octet-stream')
  =/  sha=(unit @t)  (string-at 'sha256' u.jon)
  ?~  sha
    (emit (give-error eyre-id 400 'sha256 is required'))
  ?.  (sha256-text-valid:ci-storage u.sha)
    (emit (give-error eyre-id 400 'sha256 must be 64 hex digits'))
  =/  size=(unit (unit @ud))  (number-at 'size' u.jon)
  ?.  ?&(?=(^ size) ?=(^ u.size))
    (emit (give-error eyre-id 400 'size must be a natural number'))
  ?.  =(%running status.u.found)
    (emit (give-error eyre-id 409 'attempt is closed'))
  =/  signed=(unit signed-request:git-storage)
    %:  sign-put:ci-storage
      (read-settings:ci-storage our.bowl now.bowl)
      (candidate-repo candidate.u.found)
      (scot %uv candidate.u.found)
      (scot %uv id.u.found)
      trust.u.found
      u.name
      content-type
      u.sha
      now.bowl
    ==
  ?~  signed
    (emit (give-error eyre-id 503 storage-refusal))
  =.  daemons  (touch-daemon daemon.u.found)
  %-  emit
  %^  give-json  eyre-id  200
  %-  pairs:enjs:format
  :~  ['url' s+url.u.signed]
      ['method' s+'PUT']
      ['key' s+(attempt-object-key u.found u.name)]
      ['trust' s+trust.u.found]
      :-  'headers'
      %-  pairs:enjs:format
      (turn headers.u.signed |=([k=@t v=@t] [k s+v]))
  ==
::
::  a viewer reads an attempt's log (D2): a 302 to a presigned GET the
::  browser follows with no headers of its own, in the attempt's own
::  trust class; 404 while no result has named a log.  the ship session
::  is the authorization; a daemon bearer is not a viewer.
::
++  handle-log-read
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  ?.  authenticated.req
    (emit (give-error eyre-id 401 'session required'))
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?~  log.u.found
    (emit (give-error eyre-id 404 'attempt has no log'))
  =/  url=(unit @t)  (presign-log u.found trust.u.found 'log.jsonl' log-link-expiry)
  ?~  url
    (emit (give-error eyre-id 503 storage-refusal))
  %-  emit
  %+  give-simple-payload:app:server  eyre-id
  [[302 ~[['location' u.url] ['cache-control' 'no-store']]] ~]
::
::  the daemon could not finish: act exited without a jobResult, the
::  sandbox failed, or teardown failed (D8).  the attempt closes as an
::  infrastructure error with the daemon's reason at once instead of at
::  its deadline; a result already recorded is never overwritten.
::
++  handle-abandon
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?.  (attempt-authorized req u.found)
    (emit (give-error eyre-id 401 'attempt authentication required'))
  ?.  =(%running status.u.found)
    (emit (give-error eyre-id 409 'attempt is closed'))
  =/  jon=(unit json)  (body-json req)
  ?~  jon
    (emit (give-error eyre-id 400 'valid JSON body required'))
  =/  reason=(unit @t)  (string-at 'reason' u.jon)
  ?~  reason
    (emit (give-error eyre-id 422 'reason is required'))
  =.  state  (close-attempt u.found [%infrastructure-error (rap 3 ~['abandoned: ' u.reason])])
  =/  closed=out  (after-close candidate.u.found)
  =.  state  state.closed
  =.  polls  polls.closed
  (emit (weld cards.closed (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found)))))
::
::  the plan (D1): what `act -l` listed on the candidate checkout, with
::  the daemon's compiled `needs` and `if` per job.  the ship reads the
::  candidate's own tree to check it (D2), stores it with the oid it was
::  read from (CI-BASELINE-P1), closes the plan attempt, and schedules.
::  a plan the ship cannot validate fails the attempt and the candidate
::  with the reason.
::
++  handle-plan
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  attempt-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit attempt:ci)  ?~(attempt-id ~ (~(get by attempts) u.attempt-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such attempt'))
  ?.  (attempt-authorized req u.found)
    (emit (give-error eyre-id 401 'attempt authentication required'))
  ?.  =(%running status.u.found)
    (emit (give-error eyre-id 409 'attempt is closed'))
  ?.  ?=(%plan kind.u.found)
    (emit (give-error eyre-id 409 'attempt is not a plan attempt'))
  =/  jon=(unit json)  (body-json req)
  ?~  jon
    (emit (give-error eyre-id 400 'valid JSON body required'))
  =/  =candidate:ci  (~(got by candidates) candidate.u.found)
  ?~  candidate.candidate
    (emit (give-error eyre-id 409 'candidate is not materialized'))
  =/  oid=oid:git  u.candidate.candidate
  =/  validated=(each (list job:ci) @t)
    =/  parsed=(each wire-plan:ci-plan @t)  (parse:ci-plan u.jon)
    ?:  ?=(%| -.parsed)  parsed
    =/  tree=(unit (list path))
      (tree-at repo.candidate oid '.github/workflows')
    ?~  tree
      [%| 'candidate tree could not be read from %urgit']
    =/  names=(list @t)
      %+  murn  u.tree
      |=  =path
      ^-  (unit @t)
      ?.  ?=([@ ~] path)  ~
      `i.path
    =/  checked=(each (list job:ci) @t)
      (validate:ci-plan (oid-text:git-codec oid) names p.parsed)
    ?:  ?=(%| -.checked)  checked
    =/  unreadable=(unit @t)
      %+  find-first:ci-plan  workflows.p.parsed
      |=  name=@t
      =(~ (file-at repo.candidate oid (rap 3 ~['.github/workflows/' name])))
    ?^  unreadable
      [%| (rap 3 ~['workflow file ' u.unreadable ' could not be read at the candidate oid'])]
    checked
  =.  daemons  (touch-daemon daemon.u.found)
  ?:  ?=(%| -.validated)
    =.  state  (close-attempt u.found [%plan-invalid p.validated])
    =/  closed=out  (after-close candidate.u.found)
    =.  state  state.closed
    =.  polls  polls.closed
    %-  emit
    %+  weld  cards.closed
    (give-json eyre-id 422 (pairs:enjs:format ~[['error' s+p.validated] ['attempt' (attempt-json (~(got by attempts) id.u.found))]]))
  =.  candidates
    %+  ~(put by candidates)  id.candidate
    candidate(plan `p.validated, plan-oid `oid, updated now.bowl)
  =.  state  (close-attempt u.found [%job-result %success])
  =/  closed=out  (after-close candidate.u.found)
  =.  state  state.closed
  =.  polls  polls.closed
  %-  emit
  %+  weld  cards.closed
  %^  give-json  eyre-id  200
  %-  pairs:enjs:format
  :~  ['attempt' (attempt-json (~(got by attempts) id.u.found))]
      :-  'plan'
      :-  %a
      %+  turn  p.validated
      |=  =job:ci
      %-  pairs:enjs:format
      :~  ['id' s+id.job]
          ['workflow' s+workflow.job]
          ['name' s+name.job]
          ['stage' (numb:enjs:format stage.job)]
          ['needs' [%a (turn needs.job |=(need=@t s+need))]]
      ==
  ==
--
