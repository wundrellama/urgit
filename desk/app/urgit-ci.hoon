::  Native CI controller: candidates, daemons, assignments, attempts.
::
::    P0 holds the contracts: the landing-eligibility scry %urgit reads
::    before it advances a CI-protected ref, the candidate pokes exchanged
::    with %urgit, the daemon channel under /apps/urgit/api/ci, the relayed
::    act event envelope, and CI object-store signing.  there is no
::    scheduler: an operator assigns a candidate to a daemon by poke.
::
::    persisted state is state-0 and stays there in this phase.  the open
::    long-polls are transient and dropped on every load.
::
/-  ci, git
/+  dbug, default-agent, server, ci-event, ci-storage, git-codec, git-protocol, git-storage
|%
+$  card  card:agent:gall
+$  poll  [eyre-id=@ta at=@da]
++  poll-window  ~s25
++  default-deadline  ~h1
++  storage-refusal  'ship object storage is not configured; CI cannot be enabled'
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
++  on-leave  on-leave:def
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
    `this(state closed)
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
++  handle-action
  |=  act=action:ci
  ^-  out
  ?-    -.act
      %set-ci-protected
    ?.  protected.act
      =.  ci-protected  (~(del in ci-protected) [repo.act ref.act])
      (emit ~)
    ?~  (read-settings:ci-storage our.bowl now.bowl)
      ~|  storage-refusal
      !!
    =.  ci-protected  (~(put in ci-protected) [repo.act ref.act])
    (emit ~)
  ::
      %stage-candidate
    =/  id=candidate-id:ci  (candidate-id repo.act ref.act head.act base.act)
    =/  existing=(unit candidate:ci)  (~(get by candidates) id)
    =/  next=candidate:ci
      ?^  existing  u.existing(updated now.bowl)
      :*  id  repo.act  ref.act  head.act  base.act
          ~  %.n  %pending  ~  now.bowl  now.bowl
      ==
    =.  candidates  (~(put by candidates) id next)
    (emit ~)
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
      %materialize-candidate
    ~|  '%materialize-candidate is a poke on %urgit, not %urgit-ci'
    !!
  ::
      ::  the first materialization wins: a candidate is never regenerated
      ::  after it has been recorded, let alone after it has been tested
      ::
      %candidate-ready
    =/  id=candidate-id:ci  (candidate-id repo.act ref.act head.act base.act)
    =/  found=(unit candidate:ci)  (~(get by candidates) id)
    ?~  found  (emit ~)
    ?^  candidate.u.found  (emit ~)
    =/  next=candidate:ci
      u.found(candidate `candidate.act, conflict %.n, updated now.bowl)
    =.  candidates  (~(put by candidates) id next)
    (emit ~)
  ::
      %candidate-conflict
    =/  id=candidate-id:ci  (candidate-id repo.act ref.act head.act base.act)
    =/  found=(unit candidate:ci)  (~(get by candidates) id)
    ?~  found  (emit ~)
    ?^  candidate.u.found  (emit ~)
    =.  candidates
      (~(put by candidates) id u.found(conflict %.y, updated now.bowl))
    (emit ~)
  ::
      %mint-enroll-token
    =/  token-hash=@  (shas %ci-enroll token.act)
    =/  id=daemon-id:ci  (sham [%ci-daemon token-hash])
    ?:  (~(has by daemons) id)  ~|('enroll token already minted' !!)
    =.  daemons  (~(put by daemons) id [id token-hash ~ now.bowl ~ ~])
    (emit ~)
  ::
      %assign
    =/  found=(unit candidate:ci)  (~(get by candidates) candidate.act)
    ?~  found  ~|('no such candidate' !!)
    ?~  candidate.u.found  ~|('candidate is not materialized' !!)
    =/  runner=(unit daemon:ci)  (~(get by daemons) daemon.act)
    ?~  runner  ~|('no such daemon' !!)
    ?~  enrolled.u.runner  ~|('daemon is not enrolled' !!)
    =/  attempt-id=attempt-id:ci  (sham [%ci-attempt candidate.act daemon.act now.bowl])
    =/  assignment-id=assignment-id:ci  (sham [%ci-assignment attempt-id])
    =/  deadline=@dr  (fall deadline.act default-deadline)
    =/  =attempt:ci
      :*  attempt-id  candidate.act  assignment-id  daemon.act
          %trusted  %running  0  ~  ~  ~  now.bowl  ~
      ==
    =/  =assignment:ci
      [assignment-id candidate.act daemon.act attempt-id %trusted deadline now.bowl ~]
    =/  next=candidate:ci
      u.found(status %pending, attempts [attempt-id attempts.u.found], updated now.bowl)
    =.  attempts  (~(put by attempts) attempt-id attempt)
    =.  candidates  (~(put by candidates) candidate.act next)
    =/  timer=card
      [%pass /deadline/(scot %uv attempt-id) %arvo %b %wait (add now.bowl deadline)]
    ::  a daemon holding the channel open receives it now
    ::
    =/  waiting=(unit poll)  (~(get by polls) daemon.act)
    ?~  waiting
      =.  assignments  (~(put by assignments) assignment-id assignment)
      (emit ~[timer])
    =.  polls  (~(del by polls) daemon.act)
    =.  assignment  assignment(delivered `now.bowl)
    =.  assignments  (~(put by assignments) assignment-id assignment)
    %-  emit
    [timer (give-json eyre-id.u.waiting 200 (assignment-json assignment next attempt))]
  ==
::
::  an attempt closes once.  a job result closes it passed or failed; an
::  infrastructure error closes it unknown for the candidate.  the newest
::  attempt is the candidate's required attempt.
::
++  close-attempt
  |=  [=attempt:ci =attempt-result:ci]
  ^-  _state
  =/  status=attempt-status:ci
    ?-  -.attempt-result
      %job-result            ?:(=(%success result.attempt-result) %passed %failed)
      %infrastructure-error  %infrastructure-error
    ==
  =/  closed=attempt:ci
    attempt(status status, result `attempt-result, finished `now.bowl)
  =.  attempts  (~(put by attempts) id.attempt closed)
  =/  found=(unit candidate:ci)  (~(get by candidates) candidate.attempt)
  ?~  found  state
  =/  candidate-status=candidate-status:ci
    ?-  status
      %passed                %passed
      %failed                %failed
      %infrastructure-error  %unknown
      %running               %pending
    ==
  =/  next=candidate:ci  u.found(status candidate-status, updated now.bowl)
  =.  candidates  (~(put by candidates) candidate.attempt next)
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
++  assignment-json
  |=  [=assignment:ci =candidate:ci =attempt:ci]
  ^-  json
  =/  oid=@t
    ?~  candidate.candidate  ''
    (oid-text:git-codec u.candidate.candidate)
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
      ['events' (numb:enjs:format events.attempt)]
      ['job-result' ?~(job-result.attempt ~ s+u.job-result.attempt)]
      ['candidate' s+(scot %uv candidate.attempt)]
      ['candidate-status' s+candidate-status]
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
  ?:  ?=([%apps %urgit %api %ci %attempt @ %event ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-event eyre-id req i.t.t.t.t.t.site)
  ?:  ?=([%apps %urgit %api %ci %attempt @ %result ~] site)
    ?.  =(%'POST' method)
      (emit (give-error eyre-id 405 'method not allowed'))
    (handle-result eyre-id req i.t.t.t.t.t.site)
  (emit (give-error eyre-id 404 'ci route not found'))
::
::  enrollment: the token is the credential.  its hash must match a
::  minted, not yet enrolled daemon record.  the bearer is derived from
::  the daemon id and the token, returned once, and stored only hashed.
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
    daemon(bearer-hash `bearer-hash, enrolled `now.bowl, last-seen `now.bowl)
  =.  daemons  (~(put by daemons) id.daemon next)
  %-  emit
  %^  give-json  eyre-id  200
  %-  pairs:enjs:format
  :~  ['daemon-id' s+(scot %uv id.daemon)]
      ['bearer' s+(scot %uv bearer)]
  ==
::
::  the assignment channel.  an undelivered assignment for a running
::  attempt answers at once; otherwise the request is held for the poll
::  window and closes 204, unless an assignment arrives first.
::
++  handle-assignment-poll
  |=  [eyre-id=@ta req=inbound-request:eyre segment=@t]
  ^-  out
  =/  daemon-id=(unit @uv)  (slaw %uv segment)
  =/  found=(unit daemon:ci)  ?~(daemon-id ~ (~(get by daemons) u.daemon-id))
  ?~  found
    (emit (give-error eyre-id 404 'no such daemon'))
  ?.  (daemon-authorized req u.found)
    (emit (give-error eyre-id 401 'daemon authentication required'))
  =.  daemons  (touch-daemon id.u.found)
  =/  pending=(list assignment:ci)
    %+  skim  ~(val by assignments)
    |=  =assignment:ci
    ?.  =(daemon.assignment id.u.found)  %.n
    ?^  delivered.assignment  %.n
    =/  running=(unit attempt:ci)  (~(get by attempts) attempt.assignment)
    ?&(?=(^ running) =(%running status.u.running))
  ?^  pending
    =/  =assignment:ci  i.pending
    =/  =attempt:ci  (~(got by attempts) attempt.assignment)
    =/  =candidate:ci  (~(got by candidates) candidate.assignment)
    =.  assignment  assignment(delivered `now.bowl)
    =.  assignments  (~(put by assignments) id.assignment assignment)
    (emit (give-json eyre-id 200 (assignment-json assignment candidate attempt)))
  =/  previous=(unit poll)  (~(get by polls) id.u.found)
  =.  polls  (~(put by polls) id.u.found [eyre-id now.bowl])
  =/  closed=(list card)
    ?~(previous ~ (give-empty eyre-id.u.previous 204))
  =/  timer=card
    :*  %pass  /poll/(scot %uv id.u.found)/[eyre-id]
        %arvo  %b  %wait  (add now.bowl poll-window)
    ==
  (emit (weld closed ~[timer]))
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
  =/  next=attempt:ci
    %=  u.found
      events      +(events.u.found)
      outputs     (record-output:ci-event outputs.u.found event)
      job-result  ?^(job-result.event job-result.event job-result.u.found)
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
    =.  state  (close-attempt u.found [%job-result u.result])
    (emit (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found))))
  ?^  infrastructure
    =.  state  (close-attempt u.found [%infrastructure-error u.infrastructure])
    (emit (give-json eyre-id 200 (attempt-json (~(got by attempts) id.u.found))))
  (emit (give-error eyre-id 422 'job-result or infrastructure-error is required'))
--
