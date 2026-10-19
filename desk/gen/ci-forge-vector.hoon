::  ci-forge-vector: what %urgit reports of one persisted write (Delivery
::  1C), from its repositories before and after the event.  Checked here:
::    - nothing for an event that changed nothing a run could be for: no
::      write at all, a description, a review comment, refs/ci/ scratch;
::    - ref moves, creations and deletions, in name order, each with the
::      identity of its facts and instant: a redelivery is the same id, the
::      same move at another instant a new one;
::    - pull request transitions: opened, closed, merged (closed and
::      merged, with the target's tip before the merge from the same
::      event), reopened, synchronize; none for a request created closed or
::      a merged one closed again;
::    - a default-branch move; a repository the event created or deleted
::      reports nothing; repositories in name order;
::    - the route a web request wrote through, from its path;
::    - synchronize: a local pull request's head follows its source branch
::      in the event that moved it (not a deletion, not a closed one, never
::      a peer's), and a peer updates only its own open pull request.
::  Every case names the verdict it expects; prints `passed=N of=M` and
::  ends in a loobean.
::
/-  ci, git
/+  ci-forge
:-  %say
|=  *
:-  %noun
=/  now  ~2026.10.16..12.00.00
=/  main  'refs/heads/main'
=/  pull-1=native-pull:git
  [1 ~nec 'demo' 'refs/heads/feature' main 'Add x' %open 0xd1 0xb1 ~]
=/  blank=repository:git  *repository:git
=/  repo=repository:git
  %=  blank
    owner  ~zod
    head  main
    refs  (my ~[[main 0xb1] ['refs/tags/v1' 0xb1]])
  ==
=/  with-pull=repository:git  repo(native-pulls ~[pull-1])
=/  before=(map @t repository:git)  (my ~[['demo' repo]])
=/  report
  |=  [was=(map @t repository:git) is=(map @t repository:git)]
  ^-  (list action:ci)
  (forge-actions:ci-forge was is ~zod 'web editor' now)
=/  one
  |=  [was=repository:git is=repository:git]
  ^-  (list action:ci)
  (report (my ~[['demo' was]]) (my ~[['demo' is]]))
::  the one report's ref and pull updates, if there is exactly one
::
=/  refs-of
  |=  as=(list action:ci)
  ^-  (unit (list ref-update:ci))
  ?.  ?=([[%forge-events *] ~] as)  ~
  `refs.i.as
=/  pulls-of
  |=  as=(list action:ci)
  ^-  (unit (list pull-update:ci))
  ?.  ?=([[%forge-events *] ~] as)  ~
  `pulls.i.as
=/  ref-id
  |=  [ref=@t old=(unit oid:git) new=(unit oid:git) at=@da]
  ^-  @uv
  (sham [%ci-ref-event 'demo' ref old new at])
=/  moved=repository:git  repo(refs (~(put by refs.repo) main 0xc1))
=/  closed=native-pull:git  pull-1(state %closed)
=/  merged=native-pull:git  pull-1(state %merged)
::
=/  quiet=(list [name=@t expect=? got=?])
  :~  ['no write reports nothing' %.y =(~ (report before before))]
      ['a description reports nothing' %.y =(~ (one repo repo(description 'new words')))]
      :+  'a review comment reports nothing'  %.y
      =(~ (one with-pull repo(native-pulls ~[pull-1(comments ~[[1 ~nec 'looks good' now ~ ~ ~ %.n]])])))
      :+  'a scratch ref under refs/ci/ is %urgit-ci\'s own and reports nothing'  %.y
      =(~ (one repo repo(refs (~(put by refs.repo) 'refs/ci/run/0v1.aaaaa' 0xc1))))
      :+  'a repository the event created reports nothing'  %.y
      =(~ (report before (~(put by before) 'fresh' repo)))
      :+  'a repository the event deleted reports nothing'  %.y
      =(~ (report before ~))
  ==
=/  refs=(list [name=@t expect=? got=?])
  :~  :+  'a moved ref is one report: its old and new tips, actor, route, instant'  %.y
      .=  (one repo moved)
      :~  :*  %forge-events  'demo'
              ~[[(ref-id main `0xb1 `0xc1 now) main `0xb1 `0xc1]]
              ~  ~  ~zod  'web editor'  now
          ==  ==
      :+  'a created and a deleted ref, in name order'  %.y
      .=  %-  refs-of
          (one repo repo(refs (my ~[[main 0xb1] ['refs/heads/new' 0xc1]])))
      :-  ~
      :~  [(ref-id 'refs/heads/new' ~ `0xc1 now) 'refs/heads/new' ~ `0xc1]
          [(ref-id 'refs/tags/v1' `0xb1 ~ now) 'refs/tags/v1' `0xb1 ~]
      ==
      :+  'scratch refs are left out of a report that has real ones'  %.y
      .=  %-  refs-of
          (one repo moved(refs (~(put by refs.moved) 'refs/ci/run/0v1.aaaaa' 0xc1)))
      `~[[(ref-id main `0xb1 `0xc1 now) main `0xb1 `0xc1]]
      :+  'a redelivered event is the same id'  %.y
      =((one repo moved) (one repo moved))
      :+  'the same move at another instant is a new id'  %.y
      .=  %-  refs-of
          (forge-actions:ci-forge before (my ~[['demo' moved]]) ~zod 'web editor' (add now ~s1))
      `~[[(ref-id main `0xb1 `0xc1 (add now ~s1)) main `0xb1 `0xc1]]
      :+  'a default-branch move is reported'  %.y
      .=  (one repo repo(head 'refs/heads/trunk'))
      ~[[%forge-events 'demo' ~ ~ `[main 'refs/heads/trunk'] ~zod 'web editor' now]]
      :+  'repositories report in name order'  %.y
      .=  %+  turn
            %+  report
              (my ~[['b-repo' repo] ['a-repo' repo]])
            (my ~[['b-repo' moved] ['a-repo' moved]])
          |=(a=action:ci ?:(?=(%forge-events -.a) repo.a ''))
      ~['a-repo' 'b-repo']
  ==
=/  pull-id
  |=  [number=@ud action=@t head=oid:git]
  ^-  @uv
  (sham [%ci-pull-event 'demo' number action head now])
=/  pulls=(list [name=@t expect=? got=?])
  :~  :+  'a new open pull request is opened, with every fact'  %.y
      .=  (pulls-of (one repo with-pull))
      :-  ~
      :~  :*  (pull-id 1 'opened' 0xd1)  1  'opened'  0xd1  0xb1
              'refs/heads/feature'  main  ~nec  'demo'  'Add x'  %.n  ~
          ==  ==
      :+  'open to closed is closed, not merged'  %.y
      =(`~[[(pull-id 1 'closed' 0xd1) 1 'closed' 0xd1 0xb1 'refs/heads/feature' main ~nec 'demo' 'Add x' %.n ~]] (pulls-of (one with-pull with-pull(native-pulls ~[closed]))))
      :+  'a merge is closed and merged, with the target\'s tip before it'  %.y
      =/  as=(list action:ci)  (one with-pull moved(native-pulls ~[merged]))
      ?&  =(`~[[(pull-id 1 'closed' 0xd1) 1 'closed' 0xd1 0xb1 'refs/heads/feature' main ~nec 'demo' 'Add x' %.y `0xb1]] (pulls-of as))
          =(`~[[(ref-id main `0xb1 `0xc1 now) main `0xb1 `0xc1]] (refs-of as))
      ==
      :+  'closed to open is reopened'  %.y
      =(`~[[(pull-id 1 'reopened' 0xd1) 1 'reopened' 0xd1 0xb1 'refs/heads/feature' main ~nec 'demo' 'Add x' %.n ~]] (pulls-of (one with-pull(native-pulls ~[closed]) with-pull)))
      :+  'an open pull request whose head moved is synchronize'  %.y
      =(`~[[(pull-id 1 'synchronize' 0xd2) 1 'synchronize' 0xd2 0xb1 'refs/heads/feature' main ~nec 'demo' 'Add x' %.n ~]] (pulls-of (one with-pull with-pull(native-pulls ~[pull-1(head 0xd2)]))))
      ['a pull request created closed reports nothing' %.y =(~ (one repo repo(native-pulls ~[closed])))]
      ['a merged pull request closed again reports nothing' %.y =(~ (one repo(native-pulls ~[merged]) repo(native-pulls ~[merged(state %closed)])))]
      :+  'transitions in number order'  %.y
      .=  %+  turn
            %+  fall
              (pulls-of (one repo repo(native-pulls ~[pull-1(number 9) pull-1(number 2)])))
            ~
          |=(p=pull-update:ci number.p)
      ~[2 9]
  ==
=/  routes=(list [name=@t expect=? got=?])
  :~  ['a Smart HTTP push with the write token' %.y =('smart-http push (write token)' (http-route:ci-forge ~['git' 'demo' 'git-receive-pack'] %.n))]
      ['a Smart HTTP push in a session' %.y =('smart-http push (session)' (http-route:ci-forge ~['git' 'demo' 'git-receive-pack'] %.y))]
      ['the web editor' %.y =('web editor' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'files' 'README.md'] %.y))]
      ['the default branch API' %.y =('default branch API' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'branches' 'default'] %.y))]
      ['the branch API' %.y =('branch API' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'branches'] %.y))]
      ['the tag API' %.y =('tag API' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'tags'] %.y))]
      ['a pull request merge' %.y =('pull request merge' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'pulls' '1' 'merge'] %.y))]
      ['a pull request state change' %.y =('pull request state' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'pulls' '1' 'state'] %.y))]
      ['a new pull request' %.y =('pull request API' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'pulls'] %.y))]
      ['another repository route' %.y =('web API' (http-route:ci-forge ~['apps' 'urgit' 'api' 'repository' 'demo' 'settings'] %.y))]
      ['anything else' %.y =('web request' (http-route:ci-forge ~['apps' 'other'] %.y))]
  ==
::  synchronize: a local pull request's head follows its source branch in
::  the event that moved it; a peer's follows only that peer's transfers
::
=/  local-pull=native-pull:git
  [3 ~zod 'demo' 'refs/heads/feature' main 'Local' %open 0xd1 0xb1 ~]
=/  feature-at
  |=  [r=repository:git tip=oid:git]
  ^-  repository:git
  r(refs (~(put by refs.r) 'refs/heads/feature' tip))
=/  local=repository:git  (feature-at repo(native-pulls ~[local-pull]) 0xd1)
=/  followed
  |=  [was=repository:git is=repository:git]
  ^-  (map @t repository:git)
  (follow-source-branches:ci-forge (my ~[['demo' was]]) (my ~[['demo' is]]) ~zod)
=/  heads
  |=  m=(map @t repository:git)
  ^-  (list oid:git)
  (turn native-pulls:(~(got by m) 'demo') |=(p=native-pull:git head.p))
=/  pushed=repository:git  (feature-at local 0xd2)
=/  synced=(map @t repository:git)  (followed local pushed)
=/  sync-report=(list action:ci)
  (forge-actions:ci-forge (my ~[['demo' local]]) synced ~zod 'smart-http push (write token)' now)
=/  peer-open=native-pull:git  pull-1(number 4)
=/  sync=(list [name=@t expect=? got=?])
  :~  ['a local pull request\'s head follows its moved source branch' %.y =(~[0xd2] (heads synced))]
      :+  'and the one event reports the push and a synchronize'  %.y
      ?&  =(`~[[(ref-id 'refs/heads/feature' `0xd1 `0xd2 now) 'refs/heads/feature' `0xd1 `0xd2]] (refs-of sync-report))
          =(`~[[(sham [%ci-pull-event 'demo' 3 'synchronize' 0xd2 now]) 3 'synchronize' 0xd2 0xb1 'refs/heads/feature' main ~zod 'demo' 'Local' %.n ~]] (pulls-of sync-report))
      ==
      :+  'a branch the event did not move leaves a pull request as it was'  %.y
      =/  stale=repository:git  local(native-pulls ~[local-pull(head 0xd0)])
      =(~[0xd0] (heads (followed stale stale(refs (~(put by refs.stale) 'refs/heads/other' 0xe1)))))
      :+  'a deleted source branch leaves the head it had'  %.y
      =(~[0xd1] (heads (followed local local(refs (~(del by refs.local) 'refs/heads/feature')))))
      :+  'a closed pull request does not follow its branch'  %.y
      =/  shut=repository:git  local(native-pulls ~[local-pull(state %closed)])
      =(~[0xd1] (heads (followed shut (feature-at shut 0xd2))))
      :+  'a peer\'s pull request never follows a local branch of the same name'  %.y
      =/  theirs=repository:git  local(native-pulls ~[pull-1])
      =(~[0xd1] (heads (followed theirs (feature-at theirs 0xd2))))
      :+  'a peer updates only its own open pull request from that branch onto that target'  %.y
      =/  ps=(list native-pull:git)
        ~[closed peer-open pull-1(number 5, target-ref 'refs/heads/dev')]
      ?&  =(`peer-open (open-pull-from:ci-forge ps ~nec 'demo' 'refs/heads/feature' main))
          =(~ (open-pull-from:ci-forge ps ~bus 'demo' 'refs/heads/feature' main))
          =(~ (open-pull-from:ci-forge ps ~nec 'demo' 'refs/heads/other' main))
          =(~ (open-pull-from:ci-forge ~[closed] ~nec 'demo' 'refs/heads/feature' main))
      ==
  ==
=/  cases=(list [name=@t expect=? got=?])
  :(weld quiet refs pulls routes sync)
=/  failed=(list @t)
  %+  murn  cases
  |=  [name=@t expect=? got=?]
  ?:(=(expect got) ~ `name)
=/  passed=@ud  (sub (lent cases) (lent failed))
~&  [%failed failed]
~&  (crip "passed={<passed>} of={<(lent cases)>}")
?=(~ failed)
