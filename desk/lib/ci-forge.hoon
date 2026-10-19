::  Forge events (Delivery 1C): what one of %urgit's events persisted,
::  for %urgit-ci.  %urgit diffs its repositories before and after every
::  event (on-poke, on-agent, on-arvo) and reports each repository's ref
::  moves, pull request transitions and default-branch change as one
::  %forge-events poke, so every route that persists a write — Smart HTTP,
::  the web editor, branch and tag routes, merges, peer pushes, landings,
::  linked-desk completions, imports — reports it exactly once, and a
::  refused or no-op write, which persists nothing, reports nothing.
::  Nothing here reads a request's claim.
::
::    the identity of a report is the facts and the instant of the event
::    that persisted them: one source operation is one id, its redelivery
::    the same id, and a legitimately repeated update a new one.  scratch
::    refs under refs/ci/ are %urgit-ci's own and never reported; nor are
::    the first refs of a repository the event created.
::
/-  ci, git
|%
++  scratch
  |=  ref=@t
  ^-  ?
  =('refs/ci/' (end [3 8] ref))
::
::  every repository that existed before the event and changed: its
::  report, or nothing when no ref, pull request or default branch moved
::
++  forge-actions
  |=  $:  before=(map @t repository:git)
          after=(map @t repository:git)
          actor=@p
          route=@t
          now=@da
      ==
  ^-  (list action:ci)
  ?:  =(before after)  ~
  %+  murn  (sort ~(tap by after) |=([a=[@t *] b=[@t *]] (aor -.a -.b)))
  |=  [name=@t repo=repository:git]
  ^-  (unit action:ci)
  =/  was=(unit repository:git)  (~(get by before) name)
  ?~  was  ~
  ?:  =(u.was repo)  ~
  =/  refs=(list ref-update:ci)  (ref-updates name refs.u.was refs.repo now)
  =/  pulls=(list pull-update:ci)
    (pull-updates name native-pulls.u.was native-pulls.repo refs.u.was now)
  =/  head=(unit [old=@t new=@t])
    ?:(=(head.u.was head.repo) ~ `[head.u.was head.repo])
  ?:  ?&(?=(~ refs) ?=(~ pulls) ?=(~ head))  ~
  `[%forge-events name refs pulls head actor route now]
::
::  every ref whose tip the event changed, created or deleted, in name
::  order, scratch refs aside
::
++  ref-updates
  |=  [repo=@t old=(map @t oid:git) new=(map @t oid:git) now=@da]
  ^-  (list ref-update:ci)
  ?:  =(old new)  ~
  =/  names=(list @t)
    (sort ~(tap in (~(uni in ~(key by old)) ~(key by new))) aor)
  %+  murn  names
  |=  ref=@t
  ^-  (unit ref-update:ci)
  ?:  (scratch ref)  ~
  =/  a=(unit oid:git)  (~(get by old) ref)
  =/  b=(unit oid:git)  (~(get by new) ref)
  ?:  =(a b)  ~
  `[(sham [%ci-ref-event repo ref a b now]) ref a b]
::
::  every native pull request transition, by number: a new open pull
::  request is opened; open to closed or merged is closed (merged with the
::  target's tip before the merge, from the same event); closed to open is
::  reopened; an open one whose head moved is synchronize — its source
::  branch moved (+follow-source-branches) or the peer that opened it sent
::  a new head (+open-pull-from)
::
++  pull-updates
  |=  $:  repo=@t
          old=(list native-pull:git)
          new=(list native-pull:git)
          refs=(map @t oid:git)
          now=@da
      ==
  ^-  (list pull-update:ci)
  ?:  =(old new)  ~
  =/  was=(map @ud native-pull:git)
    (malt (turn old |=(p=native-pull:git [number.p p])))
  %+  murn  (sort new |=([a=native-pull:git b=native-pull:git] (lth number.a number.b)))
  |=  p=native-pull:git
  ^-  (unit pull-update:ci)
  =/  prior=(unit native-pull:git)  (~(get by was) number.p)
  =/  action=(unit @t)
    ?~  prior  ?:(=(%open state.p) `'opened' ~)
    ?:  =(state.u.prior state.p)
      ?:(?&(=(%open state.p) !=(head.u.prior head.p)) `'synchronize' ~)
    ?:  =(%open state.p)  `'reopened'
    ?:  =(%open state.u.prior)  `'closed'
    ~
  ?~  action  ~
  =/  merged=?  =(%merged state.p)
  :-  ~
  :*  (sham [%ci-pull-event repo number.p u.action head.p now])
      number.p
      u.action
      head.p
      base.p
      source-ref.p
      target-ref.p
      source-ship.p
      source-repository.p
      title.p
      merged
      ?:(merged (~(get by refs) target-ref.p) ~)
  ==
::
::  a local pull request's head follows its source branch (synchronize):
::  an open native pull request this ship opened from a branch of the same
::  repository takes the branch's new tip as its head when the event moved
::  that branch.  a deletion leaves the head as it was (the pull request
::  keeps the last head it had); a pull request a peer opened follows only
::  that peer's own updates (+open-pull-from).  who moved the branch is the
::  event's actor, never the pull request's author
::
++  follow-source-branches
  |=  [before=(map @t repository:git) after=(map @t repository:git) our=@p]
  ^-  (map @t repository:git)
  ?:  =(before after)  after
  %-  ~(urn by after)
  |=  [name=@t repo=repository:git]
  ^-  repository:git
  =/  was=(unit repository:git)  (~(get by before) name)
  ?~  was  repo
  ?:  =(refs.u.was refs.repo)  repo
  ?:  =(~ native-pulls.repo)  repo
  %=  repo
    native-pulls
      %+  turn  native-pulls.repo
      |=  p=native-pull:git
      ^-  native-pull:git
      ?.  ?&(=(%open state.p) =(our source-ship.p) =(name source-repository.p))  p
      =/  tip=(unit oid:git)  (~(get by refs.repo) source-ref.p)
      ?~  tip  p
      ?:  =(u.tip head.p)  p
      ::  only a move this event made: a branch it left alone keeps its pull
      ::  request as it was
      ?:  =((~(get by refs.u.was) source-ref.p) tip)  p
      p(head u.tip)
  ==
::
::  the open pull request a peer already opened from one source branch onto
::  one target, which a new transfer from the same ship (by Ames) updates
::  rather than opening another
::
++  open-pull-from
  |=  [pulls=(list native-pull:git) ship=@p repository=@t source-ref=@t target-ref=@t]
  ^-  (unit native-pull:git)
  |-
  ?~  pulls  ~
  ?:  ?&  =(%open state.i.pulls)
          =(ship source-ship.i.pulls)
          =(repository source-repository.i.pulls)
          =(source-ref source-ref.i.pulls)
          =(target-ref target-ref.i.pulls)
      ==
    `i.pulls
  $(pulls t.pulls)
::
::  the route a web request wrote through, from its path: the request is
::  authenticated (the session) or, for a push, carries the owner's write
::  token
::
++  http-route
  |=  [site=(list @t) authenticated=?]
  ^-  @t
  =/  who=@t  ?:(authenticated ' (session)' ' (write token)')
  ?:  ?=([%git @ %git-receive-pack ~] site)  (rap 3 ~['smart-http push' who])
  ?.  ?=([%apps %urgit %api %repository @ *] site)  'web request'
  =/  rest=(list @t)  t.t.t.t.t.site
  ?+  rest  'web API'
    [%files *]  'web editor'
    [%file *]  'web editor'
    [%branches %default ~]  'default branch API'
    [%branches *]  'branch API'
    [%tags *]  'tag API'
    [%pulls @ %merge ~]  'pull request merge'
    [%pulls @ %state ~]  'pull request state'
    [%pulls ~]  'pull request API'
  ==
--
