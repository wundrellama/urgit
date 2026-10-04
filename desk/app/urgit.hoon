::  Native Git object database and Smart HTTP endpoint.
::
/-  git, git-peer
/+  git-upload, dbug, default-agent, git-access, git-archive, git-blame, git-catalog, git-clay
/+  git-clay-history, git-codec, git-github, git-graph, git-gzip, git-migrate, git-pack
/+  git-pack-decode, git-protocol, git-storage, git-tree, git-webhook, server
/+  *git-http, *git-lfs, *git-format, *git-repository, *git-profile, *git-clay-view, *git-json
/+  *git-migrate, *git-peer-transfer
|%
+$  card  card:agent:gall
+$  lfs-request  [eyre-id=@ta repository=@t oid=@t upload=lfs-upload:git]
+$  lfs-delete  [repository=@t oid=@t]
+$  clay-push
  $:  eyre-id=@ta
      api-response=?
      peer-response=(unit [ship=ship transfer=@uv])
      repository=@t
      commands=(list receive-command:git)
      applied=repository:git
      desk-name=desk
      branch=@t
      new-oid=oid:git
      delta=nori:clay
      result=(unit [ok=? message=@t])
      start-at=@da
      timeout-at=@da
  ==
+$  publish-job
  $:  repository=@t
      desk-name=desk
      branch=@t
      clay-revision=(unit @ud)
      message=@t
      paths=(list path)
      files=(map path octs)
  ==
+$  webhook-flight  [repository=@t hook=@ud delivery=@uv]
+$  notification-result
  [cards=(list card) activity=(unit notification-activity)]
+$  github-kind
  $?  %import
      %update
      %push
      %push-send
      %issues
      %pulls
      %issue-detail
      %pull-detail
      %pull-diff
      %file-detail
      %fork
      %open-pull
  ==
+$  github-request
  $:  job=@uv
      kind=github-kind
      repository=@t
      owner=@t
      remote=@t
      public-read=?
      head=@t
      refs=(map @t oid:git)
      metadata-page=@ud
      api-response=(unit @ta)
      detail-number=@ud
  ==
+$  github-result
  $:  active=?
      ok=?
      kind=github-kind
      repository=@t
      message=@t
  ==
++  push-event-json
  |=  commands=(list receive-command:git)
  ^-  json
  =/  entries=(list json)
    %+  turn  commands
    |=  command=receive-command:git
    %-  pairs:enjs:format
    :~  ['ref' s+ref.command]
        ['before' s+?~(old.command '' (oid-text:git-codec u.old.command))]
        ['after' s+?~(new.command '' (oid-text:git-codec u.new.command))]
        ['deleted' b+?=(~ new.command)]
    ==
  (pairs:enjs:format ~[['updates' [%a entries]]])
::
++  publish-repository
  |=  [repo=repository:git job=publish-job author=@p now=@da]
  ^-  (unit repository:git)
  ?~  binding.repo  ~
  ?.  ?&  =(desk-name.job desk-name.u.binding.repo)
          =(branch.job branch.u.binding.repo)
      ==
    ~
  =/  parent=(unit oid:git)  (~(get by refs.repo) branch.job)
  =/  snapped=(unit [commit=oid:git objects=(map oid:git object:git)])
    (snapshot:git-clay files.job objects.repo parent author now message.job)
  ?~  snapped  ~
  =/  links=(list clay-link:git)
    ?~  clay-revision.job  history.u.binding.repo
    =/  link=clay-link:git  [u.clay-revision.job commit.u.snapped %clay-to-git now]
    =/  old-links=(list clay-link:git)  history.u.binding.repo
    [link old-links]
  =/  linked=desk-binding:git
    u.binding.repo(last-clay clay-revision.job, last-git `commit.u.snapped, history links)
  =/  published=repository:git
    %_  repo
      objects  objects.u.snapped
      refs  (~(put by refs.repo) branch.job commit.u.snapped)
      binding  `linked
    ==
  `published
::

--
::
%-  agent:dbug
=|  state-4:git
=*  state  -
=/  in-flight  *(map @uv lfs-request)
=/  lfs-deletes  *(map @uv lfs-delete)
=/  request-count=@ud  0
=/  pending-clay  *(unit clay-push)
=/  pending-publish  *(unit publish-job)
=/  peer-serving  *(map @uv peer-serve)
=/  peer-receiving  *(map @uv peer-receive)
=/  peer-stream-jobs  *(map @uv peer-stream-job)
=/  peer-results  *(map @uv peer-result)
=/  peer-outgoing  *(map @uv peer-offer-flight)
=/  peer-discoveries  *(map @uv peer-discovery)
::  the catalog request still unacked per ship: one rides at a time, and
::  a hold an hour old gives way to a fresh request
=/  peer-inflight  *ledger:git-catalog
=/  peer-browses  *(map @uv peer-browse)
=/  peer-browse-prepare-queue  *(map @uv peer-browse-job)
=/  peer-browse-serving  *(map @uv peer-browse-serve)
=/  peer-forges  *(map @uv peer-forge)
=/  peer-activities  *(list peer-activity)
=/  notification-activities  *(list notification-activity)
=/  github-in-flight  *(map @uv github-request)
=/  github-results  *(map @uv github-result)
=/  webhook-in-flight  *(map @uv webhook-flight)
^-  agent:gall
|_  =bowl:gall
+*  this  .
    def  ~(. (default-agent this %|) bowl)
::
++  on-init
  ^-  (quip card _this)
  :_  this(peer-prepare-queue ~, peer-stream-jobs ~, peer-browse-prepare-queue ~)
  :~  [%pass /eyre/connect %arvo %e %connect [~ /git] %urgit]
      [%pass /eyre/api-connect %arvo %e %connect [~ /apps/urgit/api] %urgit]
  ==
::
++  on-save
  !>(state)
::
++  on-load
  |=  old=vase
  ^-  (quip card _this)
  =/  loaded=state-4:git
    ?+  -.q.old  !!
        %0
      %-  migrate-state-3
      (migrate-state-2 (migrate-state-1 (migrate-state-0 !<(state-0:git old))))
      %1  (migrate-state-3 (migrate-state-2 (migrate-state-1 !<(state-1:git old))))
      %2  (migrate-state-3 (migrate-state-2 !<(state-2:git old)))
      %3  (migrate-state-3 !<(state-3:git old))
      %4  !<(state-4:git old)
    ==
  =.  loaded  (settle-webhook-state loaded)
  ::  re-arm the snapshot-build timer for every request still queued: a queue
  ::  that survives the reload but loses its timer is the same strand.
  ::
  =/  requeued=(list card)
    %+  turn  ~(tap by peer-prepare-queue.loaded)
    |=  entry=[transfer=@uv peer-prepare-entry:git]
    ^-  card
    [%pass /peer/prepare-start/(scot %uv transfer.entry) %arvo %b %wait (add now.bowl ~s1)]
  =/  connect-cards=(list card)
    :~  [%pass /eyre/connect %arvo %e %connect [~ /git] %urgit]
        [%pass /eyre/api-connect %arvo %e %connect [~ /apps/urgit/api] %urgit]
    ==
  :_  %=  this
        state  loaded
        in-flight  ~
        lfs-deletes  ~
        request-count  0
        pending-clay  ~
        pending-publish  ~
        peer-serving  ~
        peer-receiving  ~
        peer-stream-jobs  ~
        peer-results  ~
        peer-outgoing  ~
        peer-discoveries  ~
        peer-inflight  ~
        peer-browses  ~
        peer-browse-prepare-queue  ~
        peer-browse-serving  ~
        peer-forges  ~
        peer-activities  ~
        notification-activities  ~
        github-in-flight  ~
        github-results  ~
        webhook-in-flight  ~
      ==
  (weld requeued connect-cards)
::
++  on-poke
  |=  [=mark =vase]
  ^-  (quip card _this)
  =/  before=peer-ui-state
    [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
  =/  result=(quip card _this)
    |^
      ?+  mark  (on-poke:def mark vase)
          %git-action
        ?>  =(src.bowl our.bowl)
        (handle-action !<(action:git vase))
      ::
          %handle-http-request
        =+  !<([eyre-id=@ta req=inbound-request:eyre] vase)
        (handle-http eyre-id req)
    ::
        %git-peer  (handle-peer !<(packet:git-peer vase))
      ::
          %git-webhook-event
        ?>  =(src.bowl our.bowl)
        =/  trigger=webhook-trigger:git  !<(webhook-trigger:git vase)
        (dispatch-webhooks repository.trigger event.trigger data.trigger)
      ==
    ::
    ::
    ::  a guarded read of this ship's %groups: ~ unless %groups is running,
    ::  knows the group, and answers the path without crashing.  each scry is
    ::  guarded by one that cannot answer [~ ~], because that answer kills the
    ::  event even under +mule on the current runtime: gall itself answers the
    ::  /$ liveness check, and %groups answers /u/groups/<flag> with a loobean
    ::  whether the group exists or not.  the path read is
    ::  /<under>/groups/<host>/<name>/<rest>, and rest ends in the mark asked
    ::  for: gall hands the answer over as-is when %groups serves that mark,
    ::  and otherwise converts it through the %groups desk's marks at request
    ::  time, so a read asks for the served mark where that is known.  the only
    ::  arm that scries %groups
    ::
    ++  group-peek
      |=  [group=[host=@p name=@tas] under=path rest=path]
      ^-  (unit *)
      =/  prefix=path  /(scot %p our.bowl)/groups/(scot %da now.bowl)
      =/  flag=path  /groups/(scot %p host.group)/[name.group]
      =/  live=(each ? tang)
        %-  mule
        |.
        .^(? %gu (weld prefix /$))
      ?.  &(?=(%& -.live) p.live)  ~
      =/  known=(each ? tang)
        %-  mule
        |.
        .^(? %gu (weld prefix flag))
      ?.  &(?=(%& -.known) p.known)  ~
      =/  raw=(each * tang)
        %-  mule
        |.
        .^(* %gx (weld prefix (weld under (weld flag rest))))
      ?.  ?=(%& -.raw)  ~
      `p.raw
    ::
    ::  the requester's seat in the repository's group, read from %groups in this
    ::  event and never cached.  fails closed: anything short of a seat that
    ::  soft-casts to our minimal shape is ~.
    ::
    ::  a group hosted here is authoritative.  a joined group is the host's
    ::  mirror, believed only while %groups reports it initialised and this
    ::  ship is still seated in it (+mirror-trusted); the initialised bit is
    ::  the tail of the /v2/ui/groups/<flag> peek, [group init=? member-count=@ud],
    ::  which %groups resets together with its copy whenever it rebuilds one.
    ::  the only arm that turns %groups into a capability.
    ::
    ++  group-seat
      |=  [policy=(unit group-policy:git) requester=@p]
      ^-  (unit group-seat:git)
      ?~  policy  ~
      =/  group=[host=@p name=@tas]  group.u.policy
      =/  seat-of
        |=  who=@p
        ^-  (unit group-seat:git)
        =/  raw=(unit *)  (group-peek group / /seats/(scot %p who)/noun)
        ?~  raw  ~
        =/  seat=(each (unit group-seat:git) tang)
          %-  mule
          |.
          ;;((unit group-seat:git) u.raw)
        ?.  ?=(%& -.seat)  ~
        p.seat
      =/  net=?(%pub %sub)  ?:(=(our.bowl host.group) %pub %sub)
      =/  init=?
        ?:  ?=(%pub net)  %.y
        =/  raw=(unit *)  (group-peek group /v2/ui /noun)
        ?~  raw  %.n
        =/  ui=(each [* init=? member-count=@ud] tang)
          %-  mule
          |.
          ;;([* init=? member-count=@ud] u.raw)
        ?.  ?=(%& -.ui)  %.n
        init.p.ui
      =/  seated=?  |(?=(%pub net) !=(~ (seat-of our.bowl)))
      ?.  (mirror-trusted:git-access net init seated)  ~
      (seat-of requester)
    ::
    ::  the ships seated in a group, for fanning discovery out to them.  the
    ::  group is believed on exactly the terms +group-seat believes it for
    ::  access, with this ship as the requester, so an untrusted mirror or a
    ::  group this ship is not seated in lists nobody.  %groups serves
    ::  /seats/ships as %ships, a (set ship), and is asked for it by that mark
    ::
    ++  group-members
      |=  group=[host=@p name=@tas]
      ^-  (unit (set ship))
      ?~  (group-seat `[group %none ~] our.bowl)  ~
      =/  raw=(unit *)  (group-peek group / /seats/ships/ships)
      ?~  raw  ~
      =/  ships=(each (set ship) tang)
        %-  mule
        |.
        ;;((set ship) u.raw)
      ?.  ?=(%& -.ships)  ~
      `p.ships
    ::
    ++  repository-group-capability
      |=  [repo=repository:git requester=@p]
      ^-  capability:git
      ?~  group-policy.repo  %none
      ?:  =(requester owner.repo)  %none
      (group-capability:git-access group-policy.repo (group-seat group-policy.repo requester))
    ::
    ++  repository-readable
      |=  [repo=repository:git requester=@p]
      ^-  ?
      %-  can-read:git-access
      :*  public-read.repo
          owner.repo
          readers.repo
          writers.repo
          (repository-group-capability repo requester)
          requester
      ==
    ::
    ++  repository-writable
      |=  [repo=repository:git requester=@p]
      ^-  ?
      %-  can-write:git-access
      :*  owner.repo
          writers.repo
          (repository-group-capability repo requester)
          requester
      ==
    ::
    ++  peer-activity-put
      |=  event=peer-activity
      ^-  (list peer-activity)
      =/  others=(list peer-activity)
        %+  skim  peer-activities
        |=  existing=peer-activity
        !=(id.event id.existing)
      =/  combined=(list peer-activity)  [event others]
      (scag 50 combined)
    ::
    ++  peer-activity-start
      |=
        $:  id=@uv
            kind=peer-activity-kind
            direction=?(%incoming %outgoing)
            peer=ship
            repository=@t
            message=@t
        ==
      ^-  (list peer-activity)
      (peer-activity-put [id kind direction peer repository %active message now.bowl])
    ::
    ++  peer-activity-finish
      |=  [id=@uv ok=? message=@t]
      ^-  (list peer-activity)
      %+  turn  peer-activities
      |=  event=peer-activity
      ?:  !=(id id.event)  event
      event(status ?:(ok %success %failure), message message, when now.bowl)
    ::
    ++  peer-outgoing-finish
      |=  [transfer=@uv ok=? message=@t]
      ^-  (quip card _this)
      =/  outgoing=(unit peer-offer-flight)  (~(get by peer-outgoing) transfer)
      ?~  outgoing  `this
      =.  peer-outgoing  (~(del by peer-outgoing) transfer)
      =.  peer-results
        (~(put by peer-results) transfer [ok message repository.u.outgoing])
      =.  peer-activities  (peer-activity-finish transfer ok message)
      `this
    ::
    ++  notification-activity-put
      |=  event=notification-activity
      ^-  (list notification-activity)
      =/  combined=(list notification-activity)  [event notification-activities]
      (scag 50 combined)
    ::
    ++  peer-transfer-yawns
      |=  [transfer=@uv peer=ship mode=peer-transfer-mode pages=@ud completed=(set @ud)]
      ^-  (list card)
      ?:  =(%archive mode)  ~
      ?:  =(%objects mode)
        =/  pending-revisions=(list @ud)  (gulf 1 pages)
        %+  murn  pending-revisions
        |=  revision=@ud
        =/  issued=?
          ?:  (lte revision peer-stream-window)  %.y
          (~(has in completed) (sub revision peer-stream-window))
        ?.  issued  ~
        =/  scry-path=path
          /g/x/(scot %ud revision)/urgit//1/fine/(peer-fine-name transfer)
        :-  ~
        :*  %pass
            /peer/fine-cancel/(scot %uv transfer)/(scot %ud revision)
            %arvo
            %a
            %yawn
            [peer scry-path]
        ==
      =/  pending-pages=(list @ud)  (gulf 1 pages)
      %+  murn  pending-pages
      |=  revision=@ud
      =/  issued=?
        ?:  =(revision 1)  %.y
        (~(has in completed) (sub revision 1))
      ?.  issued  ~
      =/  scry-path=path
        /g/x/(scot %ud revision)/urgit//1/fine/(peer-fine-name transfer)
      :-  ~
      :*  %pass
          /peer/fine-cancel/(scot %uv transfer)/(scot %ud revision)
          %arvo
          %a
          %yawn
          [peer scry-path]
      ==
    ::
    ++  peer-object-pages
      |=  objects=(list [oid:git object:git])
      ^-  (list octs)
      =/  remaining  objects
      =/  page=(map oid:git object:git)  ~
      =/  pages=(list octs)  ~
      =/  count=@ud  0
      =/  bytes=@ud  0
      =/  packed-page
        |=  entries=(map oid:git object:git)
        ^-  octs
        (encode-pack:git-pack ~(val by entries))
      |-
      ?~  remaining
        ?:  =(count 0)
          ?~  pages  [(packed-page page) ~]
          (flop pages)
        (flop [(packed-page page) pages])
      =/  object-bytes=@ud  (add 64 p.data.+.i.remaining)
      =/  page-full=?
        |(=(count 256) &((gth count 0) (gth (add bytes object-bytes) 524.288)))
      ?:  page-full
        $(page ~, pages [(packed-page page) pages], count 0, bytes 0)
      =/  next-page=(map oid:git object:git)
        (~(put by page) -.i.remaining +.i.remaining)
      %=  $
        remaining  t.remaining
        page  next-page
        count  +(count)
        bytes  (add bytes object-bytes)
      ==
    ::
    ++  peer-object-batch-count
      |=  objects=(list [oid:git object:git])
      ^-  @ud
      =/  remaining  objects
      =/  offset=@ud  0
      =/  pages=@ud  0
      =/  count=@ud  0
      =/  bytes=@ud  0
      |-
      ?~  remaining
        ?:  =(count 0)  (max 1 pages)
        +(pages)
      =/  =object:git  +.i.remaining
      =/  total=@ud  p.data.object
      =/  batch-full=?
        |(=(count peer-stream-page-max-fragments) =(bytes peer-stream-page-max-bytes))
      ?:  batch-full
        $(pages +(pages), count 0, bytes 0)
      ?:  =(total 0)
        %=  $
          remaining  t.remaining
          offset  0
          count  +(count)
        ==
      ?:  =(offset total)
        $(remaining t.remaining, offset 0)
      =/  fragment-length=@ud  (min 1.048.576 (sub total offset))
      =/  page-room=@ud  (sub peer-stream-page-max-bytes bytes)
      ?:  ?&  (gth count 0)
              (gth fragment-length page-room)
          ==
        $(pages +(pages), count 0, bytes 0)
      =/  length=@ud  fragment-length
      =/  next-offset=@ud  (add offset length)
      =/  next-remaining=(list [oid:git object:git])
        ?:(=(next-offset total) t.remaining remaining)
      %=  $
        remaining  next-remaining
        offset  ?:(=(next-offset total) 0 next-offset)
        count  +(count)
        bytes  (add bytes length)
      ==
    ::
    ++  peer-object-batch
      |=  [objects=(list [oid:git object:git]) offset=@ud]
      ^-  [batch=(list object-fragment:git-peer) remaining=(list [oid:git object:git]) offset=@ud]
      =/  remaining  objects
      =/  batch=(list object-fragment:git-peer)  ~
      =/  count=@ud  0
      =/  bytes=@ud  0
      |-
      ?~  remaining  [(flop batch) remaining offset]
      =/  =oid:git  -.i.remaining
      =/  =object:git  +.i.remaining
      =/  kind=object-kind:git  kind.object
      =/  total=@ud  p.data.object
      =/  batch-full=?
        |(=(count peer-stream-page-max-fragments) =(bytes peer-stream-page-max-bytes))
      ?:  batch-full  [(flop batch) remaining offset]
      ?:  =(total 0)
        %=  $
          remaining  t.remaining
          offset  0
          batch  [[oid kind 0 0 [0 0]] batch]
          count  +(count)
        ==
      ?:  =(offset total)
        $(remaining t.remaining, offset 0)
      =/  fragment-length=@ud  (min 1.048.576 (sub total offset))
      =/  page-room=@ud  (sub peer-stream-page-max-bytes bytes)
      ?:  ?&  (gth count 0)
              (gth fragment-length page-room)
          ==
        [(flop batch) remaining offset]
      =/  length=@ud  fragment-length
      =/  fragment-data=octs
        (slice:git-codec data.object offset length)
      =/  fragment=object-fragment:git-peer
        [oid kind total offset fragment-data]
      =/  next-offset=@ud  (add offset length)
      =/  next-remaining=(list [oid:git object:git])
        ?:(=(next-offset total) t.remaining remaining)
      %=  $
        remaining  next-remaining
        offset  ?:(=(next-offset total) 0 next-offset)
        batch  [fragment batch]
        count  +(count)
        bytes  (add bytes length)
      ==
    ::
    ++  peer-browse-pages
      |=  result=json
      ^-  (list [length=@ud data=@])
      =/  encoded=@  (jam result)
      =/  size=@ud  (met 3 encoded)
      =/  offset=@ud  0
      =/  pages=(list [length=@ud data=@])  ~
      |-
      ?:  =(offset size)  (flop pages)
      =/  length=@ud  (min 65.536 (sub size offset))
      =/  data=@  (cut 3 [offset length] encoded)
      $(offset (add offset length), pages [[length data] pages])
    ::
    ++  peer-fail
      |=  [target=ship transfer=@uv message=@t]
      ^-  (quip card _this)
      :_  this
      :~  (peer-card target /peer/error/(scot %uv transfer) [%error transfer message])
      ==
    ::
    ++  peer-valid-refs
      |=  [refs=(map @t oid:git) objects=(map oid:git object:git)]
      ^-  ?
      %+  levy  ~(tap by refs)
      |=  entry=[@t oid:git]
      (~(has by objects) +.entry)
    ::
    ++  peer-finish
      |=  transfer=@uv
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer)
      ?~  found  `this
      =/  flight=peer-receive  u.found
      ?.  ?&  =(expected.flight received.flight)
              (peer-valid-refs refs.flight objects.flight)
              (~(has by refs.flight) head.flight)
          ==
        ?:  |(=(%pull purpose.flight) =(%push purpose.flight))
          (peer-push-finish flight transfer %.n 'received repository graph is incomplete')
        =.  peer-receiving  (~(del by peer-receiving) transfer)
        =.  peer-results
          %+  ~(put by peer-results)
            transfer
          [%.n 'received repository graph is incomplete' local-repository.flight]
        =.  peer-activities
          (peer-activity-finish transfer %.n 'received repository graph is incomplete')
        `this
      =/  existing=(unit repository:git)  (~(get by repositories) local-repository.flight)
      |^
        ?:  =(%pull purpose.flight)
          finish-pull
        ?:  =(%push purpose.flight)
          finish-push
        =/  repo=repository:git
          ?~  existing
            :*  our.bowl
                public-read.flight
                ''
                head.flight
                refs.flight
                ~
                objects.flight
                (silt ~[our.bowl])
                ~
                ~
                ~
                ~
                ~
                ~
                `[[source.flight source-repository.flight]]
                ~
                ~
                ~
                ~
                ~
                ~
                ~
                ~
                ~
                ~
                default-notification-events
                ~
            ==
          %=  u.existing
            head  head.flight
            refs  refs.flight
            objects  objects.flight
            peer-origin  `[[source.flight source-repository.flight]]
          ==
        =.  repositories  (~(put by repositories) local-repository.flight repo)
        =.  peer-receiving  (~(del by peer-receiving) transfer)
        =.  peer-results  (~(put by peer-results) transfer [%.y 'complete' local-repository.flight])
        =.  peer-activities  (peer-activity-finish transfer %.y 'fork complete')
        `this
      ++  finish-pull
        ?>  =(%pull purpose.flight)
        ?~  existing
          (peer-push-finish flight transfer %.n 'destination repository disappeared')
        =/  selected-source-ref=@t
          ?:(=('' source-ref.flight) head.flight source-ref.flight)
        =/  incoming=(unit oid:git)  (~(get by refs.flight) selected-source-ref)
        =/  base=(unit oid:git)  (~(get by refs.u.existing) target-ref.flight)
        ?~  incoming
          (peer-push-finish flight transfer %.n 'source branch not found in received repository')
        ?~  base
          (peer-push-finish flight transfer %.n 'target branch not found')
        ?>  ?=(^ incoming)
        ?>  ?=(^ base)
        =/  incoming-oid=oid:git  u.incoming
        =/  base-oid=oid:git  u.base
        ?:  =(incoming-oid base-oid)
          (peer-push-finish flight transfer %.n 'selected branches have identical tips')
        =/  number=@ud  (add 1 (lent native-pulls.u.existing))
        =/  pull=native-pull:git
          :*  number
              source.flight
              source-repository.flight
              selected-source-ref
              target-ref.flight
              title.flight
              %open
              incoming-oid
              base-oid
              ~
          ==
        =/  updated=repository:git
          u.existing(objects objects.flight, native-pulls [pull native-pulls.u.existing])
        =.  repositories  (~(put by repositories) local-repository.flight updated)
        =/  notice=notification-result
          %-  repository-notification
          :*  local-repository.flight
              updated
              %pull-request
              /[local-repository.flight]/pull/(scot %ud number)
              %+  rap
                3
              :~  (scot %p source.flight)
                  ' opened pull request #'
                  (decimal number)
                  ' in '
                  local-repository.flight
                  ': '
                  title.flight
              ==
          ==
        =.  notification-activities
          ?~  activity.notice
            notification-activities
          (notification-activity-put u.activity.notice)
        =/  notices=(list card)  cards.notice
        =/  finished=(quip card _this)
          %:  peer-push-finish
            flight
            transfer
            %.y
            (rap 3 ~['pull request #' (decimal number) ' opened'])
          ==
        [(weld notices -.finished) +.finished]
      ++  finish-push
        ?>  =(%push purpose.flight)
        ?~  existing
          (peer-push-finish flight transfer %.n 'destination repository disappeared')
        ?.  =(head.flight head.u.existing)
          (peer-push-finish flight transfer %.n 'default branch does not match destination')
        =/  incoming=(unit oid:git)  (~(get by refs.flight) head.flight)
        ?~  incoming
          (peer-push-finish flight transfer %.n 'source default branch is missing')
        =/  previous=(unit oid:git)  (~(get by refs.u.existing) head.u.existing)
        =/  reachable=(unit (set oid:git))
          (reachable:git-graph objects.flight (silt ~[u.incoming]))
        =/  fast-forward=?
          ?~  previous  %.y
          ?~  reachable  %.n
          (~(has in u.reachable) u.previous)
        ?.  fast-forward
          (peer-push-finish flight transfer %.n 'update is not a fast-forward')
        =/  updated=repository:git
          %=  u.existing
            objects  objects.flight
            refs  (~(put by refs.u.existing) head.u.existing u.incoming)
          ==
        ?^  binding.updated
          ?:  |(=(^ pending-clay) =(^ pending-publish))
            (peer-push-finish flight transfer %.n 'another Clay operation is in progress')
          =/  files=(unit (map path octs))
            (flatten-commit:git-clay objects.updated u.incoming)
          ?~  files
            %:  peer-push-finish
              flight
              transfer
              %.n
              'linked branch must resolve to a valid desk-shaped Git commit'
            ==
          =/  delta=(unit nori:clay)
            (clay-delta our.bowl now.bowl desk-name.u.binding.updated u.files)
          ?~  delta
            (peer-push-finish flight transfer %.n 'unable to read linked Clay desk')
          ?>  ?=(%& -.u.delta)
          ?:  =(~ p.u.delta)
            =.  repositories  (~(put by repositories) local-repository.flight updated)
            (peer-push-finish flight transfer %.y 'fast-forward update accepted')
          =/  start-at=@da  (add now.bowl ~s1)
          =/  timeout-at=@da  (add now.bowl ~s15)
          =/  pending=clay-push
            :*  'peer'
                %.n
                `[[source.flight transfer]]
                local-repository.flight
                ~
                updated
                desk-name.u.binding.updated
                branch.u.binding.updated
                u.incoming
                u.delta
                ~
                start-at
                timeout-at
            ==
          =.  peer-receiving  (~(del by peer-receiving) transfer)
          =.  pending-clay  `pending
          :_  this
          :~  [%pass /clay-start %arvo %b %wait start-at]
              [%pass /clay-timeout %arvo %b %wait timeout-at]
          ==
        =.  repositories  (~(put by repositories) local-repository.flight updated)
        (peer-push-finish flight transfer %.y 'fast-forward update accepted')
      --
    ::
    ++  peer-push-finish
      |=  [flight=peer-receive transfer=@uv ok=? message=@t]
      ^-  (quip card _this)
      =.  peer-receiving  (~(del by peer-receiving) transfer)
      =.  peer-activities  (peer-activity-finish transfer ok message)
      :_  this
      :~  (peer-card source.flight /peer/result/(scot %uv transfer) [%result transfer ok message])
      ==
    ::
    ++  peer-error
      |=  [transfer=@uv message=@t]
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer)
      ?^  found
        ?.  =(src.bowl source.u.found)  `this
        =/  flight=peer-receive  u.found
        =.  peer-receiving  (~(del by peer-receiving) transfer)
        =.  peer-results  (~(put by peer-results) transfer [%.n message local-repository.u.found])
        =.  peer-activities  (peer-activity-finish transfer %.n message)
        :_  this
        ?:  =('' head.flight)  ~
        (peer-transfer-yawns transfer source.flight mode.flight pages.flight completed.flight)
      =/  outgoing=(unit peer-offer-flight)  (~(get by peer-outgoing) transfer)
      ?~  outgoing  `this
      ?.  =(src.bowl peer.u.outgoing)  `this
      (peer-outgoing-finish transfer %.n message)
    ::
    ++  handle-peer
      |=  packet=packet:git-peer
      ^-  (quip card _this)
      ?-  -.packet
        %request  (peer-request request.packet)
        %accepted  (peer-accepted accepted.packet)
          %prepare
        ?>  =(src.bowl our.bowl)
        (peer-prepare target.prepare.packet request.prepare.packet)
        %archive-ready  (peer-archive-ready archive-ready.packet)
        %archive-accept  (peer-archive-accept transfer.packet)
        %stream-next  (peer-stream-next transfer.packet)
        %stream-grown  (peer-stream-grown transfer.packet)
        %ready  (peer-ready ready.packet)
        %begin  (peer-begin begin.packet)
        %begin-objects  (peer-begin-objects begin-objects.packet)
          %object-fragments
        (peer-object-fragments transfer.packet revision.packet fragments.packet)
        %catalog-request  (peer-catalog-request catalog-request.packet)
        %catalog  (peer-catalog-legacy catalog.packet)
        %catalog-via  (peer-catalog catalog.packet)
        %catalog-error  (peer-catalog-error request.packet message.packet)
          %browse-request
        %:  peer-browse-request
          request.packet
          repository.packet
          view.packet
          number.packet
          file-path.packet
        ==
        %browse-accepted  (peer-browse-accepted request.packet)
        %browse-prepare  (peer-browse-prepare request.packet)
          %browse-ready
        (peer-browse-ready request.packet repository.packet target.packet pages.packet)
        %browse-response  (peer-browse-response request.packet repository.packet result.packet)
        %browse-begin  (peer-browse-begin request.packet repository.packet pages.packet)
        %browse-release  (peer-browse-release request.packet)
        %browse-error  (peer-browse-error request.packet message.packet)
        %forge-comment  (peer-forge-comment comment.packet)
        %forge-create-issue  (peer-forge-create-issue issue.packet)
          %forge-result
        %:  peer-forge-result
          request.packet
          repository.packet
          kind.packet
          number.packet
          ok.packet
          message.packet
          result.packet
        ==
        %offer  (peer-offer-legacy offer.packet)
        %offer-branches  (peer-offer offer-branches.packet)
        %release  (peer-release transfer.packet)
        %archive  (peer-archive transfer.packet repository.packet objects.packet)
        %snapshot  (peer-snapshot transfer.packet objects.packet)
        %snapshot-error  (peer-snapshot-fail transfer.packet message.packet)
        %result  (peer-result-received transfer.packet ok.packet message.packet)
        %error  (peer-error transfer.packet message.packet)
      ==
    ::
    ::  answer a peer's catalog request with the repositories it may read.
    ::  an entry is tagged via the repository's group when the group policy
    ::  is the only thing letting the peer read it: the same +can-read, asked
    ::  once more with the group's capability withheld.  the answer goes out
    ::  under the older %catalog shape unless some entry carries a group, so
    ::  older peers keep understanding it
    ::
    ++  peer-catalog-request
      |=  msg=catalog-request:git-peer
      ^-  (quip card _this)
      =/  readable-repositories=(list catalog-repository:git-peer)
        %+  murn  ~(tap by repositories)
        |=  entry=[@t repository:git]
        =/  name=@t  -.entry
        =/  repo=repository:git  +.entry
        ?.  (repository-readable repo src.bowl)  ~
        =/  explicit=?
          (can-read:git-access public-read.repo owner.repo readers.repo writers.repo %none src.bowl)
        =/  via=(unit [host=@p name=@tas])
          ?:  explicit  ~
          ?~  group-policy.repo  ~
          `group.u.group-policy.repo
        :-  ~
        :*  name
            head.repo
            ~(wyt by refs.repo)
            ~(wyt by objects.repo)
            (repository-writable repo src.bowl)
            via
        ==
      =/  answer=(list catalog-repository:git-peer)  (scag 200 readable-repositories)
      :_  this
      :~  %^  peer-card
            src.bowl
            /peer/catalog/(scot %uv request.msg)
          (pack:git-catalog request.msg answer)
      ==
    ::
    ::  a %catalog from an older peer: nothing it lists came through a group
    ::
    ++  peer-catalog-legacy
      |=  msg=catalog-legacy:git-peer
      ^-  (quip card _this)
      (peer-catalog request.msg (turn repositories.msg from-legacy:git-catalog))
    ::
    ++  peer-catalog
      |=  msg=catalog:git-peer
      ^-  (quip card _this)
      =/  found=(unit peer-discovery)  (~(get by peer-discoveries) request.msg)
      ?~  found  `this
      ?.  =(src.bowl peer.u.found)  `this
      =.  peer-discoveries
        (~(put by peer-discoveries) request.msg (answered:git-catalog u.found repositories.msg))
      `this
    ::
    ++  peer-catalog-error
      |=  [request=@uv message=@t]
      ^-  (quip card _this)
      =/  found=(unit peer-discovery)  (~(get by peer-discoveries) request)
      ?~  found  `this
      ?.  =(src.bowl peer.u.found)  `this
      =.  peer-discoveries
        (~(put by peer-discoveries) request (refused:git-catalog u.found message))
      `this
    ::
    ++  peer-browse-request
      |=  [request=@uv repository=@t view=browse-view:git-peer number=@ud file-path=path]
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repository)
      ?.  &(?=(^ found) (repository-readable u.found src.bowl))
        :_  this
        :~  %^  peer-card
              src.bowl
              /peer/browse-error/(scot %uv request)
            [%browse-error request 'repository is unavailable or requester is not authorized']
        ==
      ::  Build in the request event.  Deferring this through Behn left the
      ::  requester permanently in %prepare when the wake or self-poke was lost,
      ::  even for the constant-size %stamp view.  Browse generation is bounded;
      ::  Fine or Mesa publication still happens through the returned cards.
      (browse-build src.bowl request repository view number file-path)
    ::
    ++  browse-build
      |=  $:  target=ship
              request=@uv
              repository=@t
              view=browse-view:git-peer
              number=@ud
              file-path=path
          ==
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repository)
      ?.  &(?=(^ found) (repository-readable u.found target))
        :_  this
        :~  %^  peer-card
              target
              /peer/browse-error/(scot %uv request)
            [%browse-error request 'repository is unavailable or requester is not authorized']
        ==
      =/  detail=(unit json)
        ?:  =(%stamp view)
          `(repository-stamp-json repository u.found)
        ?:  =(%overview view)
          `(peer-repository-browse-json repository u.found)
        ?:  =(%issue view)
          =/  issue=(unit native-issue:git)  (native-issue-at u.found number)
          ?~  issue  ~
          :-  ~
          %:  pairs:enjs:format
            :~  ['repository' (public-repository-json-up-to repository u.found 50)]
                ['issue' (native-issue-json u.issue %.y)]
            ==
          ==
        ?:  =(%pull view)
          =/  pull=(unit native-pull:git)  (native-pull-at u.found number)
          ?~  pull  ~
          =/  pull-json=(unit json)  (native-pull-detail-json repository u.found u.pull)
          ?~  pull-json  ~
          :-  ~
          %:  pairs:enjs:format
            :~  ['repository' (public-repository-json-up-to repository u.found 50)]
                ['pull' u.pull-json]
            ==
          ==
        ?:  =(%commit view)
          ?~  file-path  ~
          (repository-history-detail-json repository u.found i.file-path our.bowl now.bowl)
        =/  data=(unit octs)  (repository-file u.found file-path)
        ?~  data  ~
        ?:  (gth p.u.data 4.194.304)  ~
        :-  ~
        %:  pairs:enjs:format
          :~  ['repository' (public-repository-json-up-to repository u.found 50)]
              ['file' (repository-file-json repository u.found head.u.found file-path u.data)]
          ==
        ==
      ?~  detail
        :_  this
        :~  %^  peer-card
              target
              /peer/browse-error/(scot %uv request)
            :*  %browse-error
                request
                'requested item is unavailable, incomplete, or too large to preview'
            ==
        ==
      =/  result=json  u.detail
      ?.  (peer-object-capable request)
        :_  this
        :~  %^  peer-card
              target
              /peer/browse-error/(scot %uv request)
            [%browse-error request 'peer must update %urgit to browse repositories']
        ==
      :_  this
      :~  %^  peer-card
            target
            /peer/browse-response/(scot %uv request)
          [%browse-response request repository result]
      ==
    ::
    ++  peer-browse-prepare
      |=  request=@uv
      ^-  (quip card _this)
      ?.  =(src.bowl our.bowl)  `this
      =/  found=(unit peer-browse-job)  (~(get by peer-browse-prepare-queue) request)
      ?~  found  `this
      =/  job=peer-browse-job  u.found
      =.  peer-browse-prepare-queue
        (~(del by peer-browse-prepare-queue) request)
      (browse-build target.job request repository.job view.job number.job file-path.job)
    ::
    ++  peer-browse-accepted
      |=  request=@uv
      ^-  (quip card _this)
      =/  found=(unit peer-browse)  (~(get by peer-browses) request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(src.bowl peer.u.found)
              =(%request phase.u.found)
              (peer-object-capable request)
          ==
        `this
      =.  peer-browses
        %+  ~(put by peer-browses)
          request
        %=  u.found
          phase  %prepare
          message  'peer is preparing repository overview'
          progress-at  now.bowl
        ==
      :_  this
      :~  [%pass /peer/browse-prepare-timeout/(scot %uv request) %arvo %b %wait (add now.bowl ~m10)]
      ==
    ::
    ++  peer-browse-ready
      |=  [request=@uv repository=@t target=ship pages=@ud]
      ^-  (quip card _this)
      ?.  =(src.bowl our.bowl)  `this
      :_  this
      :~  %^  peer-card
            target
            /peer/browse-begin/(scot %uv request)
          [%browse-begin request repository pages]
      ==
    ::
    ++  peer-browse-response
      |=  [request=@uv repository=@t result=json]
      ^-  (quip card _this)
      =/  found=(unit peer-browse)  (~(get by peer-browses) request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(src.bowl peer.u.found)
              =(repository repository.u.found)
          ==
        `this
      =/  valid=?
        ?.  ?=([%o *] result)  %.n
        =/  repository-json=(unit json)  (~(get by p.result) 'repository')
        ?~  repository-json  %.n
        ?:  ?=([%s *] u.repository-json)
          =(p.u.repository-json repository)
        ?.  ?=([%o *] u.repository-json)  %.n
        =/  name-json=(unit json)  (~(get by p.u.repository-json) 'name')
        ?~  name-json  %.n
        &(?=([%s *] u.name-json) =(p.u.name-json repository))
      ?.  valid
        =.  peer-browses
          %+  ~(put by peer-browses)
            request
          %=  u.found
            active  %.n
            ok  %.n
            message  'peer browse result has the wrong repository identity'
          ==
        `this
      =.  peer-browses
        %+  ~(put by peer-browses)
          request
        u.found(active %.n, ok %.y, message 'complete', result `result)
      `this
    ::
    ++  peer-browse-begin
      |=  [request=@uv repository=@t pages=@ud]
      ^-  (quip card _this)
      =/  found=(unit peer-browse)  (~(get by peer-browses) request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(src.bowl peer.u.found)
              =(repository repository.u.found)
          ==
        `this
      =.  peer-browses
        %+  ~(put by peer-browses)
          request
        u.found(active %.n, ok %.n, message 'peer must update %urgit to browse repositories')
      `this
    ::
    ++  peer-browse-release
      |=  request=@uv
      ^-  (quip card _this)
      =/  queued=(unit peer-browse-job)  (~(get by peer-browse-prepare-queue) request)
      ?^  queued
        ?.  =(src.bowl target.u.queued)  `this
        `this(peer-browse-prepare-queue (~(del by peer-browse-prepare-queue) request))
      =/  found=(unit peer-browse-serve)  (~(get by peer-browse-serving) request)
      ?~  found  `this
      ?.  =(src.bowl target.u.found)  `this
      =/  culls=(list card)
        %+  turn  (gulf 1 pages.u.found)
        |=  revision=@ud
        :*  %pass
            /peer/browse-cull/(scot %uv request)/(scot %ud revision)
            %cull
            [%ud revision]
            /browse/(scot %uv request)
        ==
      :_  this(peer-browse-serving (~(del by peer-browse-serving) request))
      culls
    ::
    ++  peer-browse-error
      |=  [request=@uv message=@t]
      ^-  (quip card _this)
      =/  found=(unit peer-browse)  (~(get by peer-browses) request)
      ?~  found  `this
      ?.  =(src.bowl peer.u.found)  `this
      =.  peer-browses
        (~(put by peer-browses) request u.found(active %.n, ok %.n, message message))
      `this
    ::
    ++  peer-forge-reply
      |=
        $:  target=ship
            request=@uv
            repository=@t
            kind=forge-kind:git-peer
            number=@ud
            ok=?
            message=@t
            result=(unit json)
        ==
      ^-  (quip card _this)
      :_  this
      :~  %^  peer-card
            target
            /peer/forge-result/(scot %uv request)
          [%forge-result request repository kind number ok message result]
      ==
    ::
    ++  peer-forge-comment
      |=  msg=forge-comment:git-peer
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repository.msg)
      ?.  &(?=(^ found) (repository-readable u.found src.bowl))
        %:  peer-forge-reply
          src.bowl
          request.msg
          repository.msg
          kind.msg
          number.msg
          %.n
          'repository is unavailable or requester is not authorized'
          ~
        ==
      |^
        ?:  =(%issue kind.msg)
          comment-on-issue
        comment-on-pull
      ++  comment-on-issue
        =/  issue=(unit native-issue:git)  (native-issue-at u.found number.msg)
        ?~  issue
          %:  peer-forge-reply
            src.bowl
            request.msg
            repository.msg
            kind.msg
            number.msg
            %.n
            'issue not found'
            ~
          ==
        =/  comment=issue-comment:git
          [(add 1 (lent comments.u.issue)) src.bowl body.msg now.bowl]
        =/  updated-issue=native-issue:git
          u.issue(comments (weld comments.u.issue ~[comment]), updated now.bowl)
        =/  issues=(list native-issue:git)
          %+  turn  native-issues.u.found
          |=  candidate=native-issue:git
          ?:(=(number.candidate number.msg) updated-issue candidate)
        =/  updated-repo=repository:git  u.found(native-issues issues)
        =.  repositories  (~(put by repositories) repository.msg updated-repo)
        =/  notice=notification-result
          %-  repository-notification
          :*  repository.msg
              updated-repo
              %issue-comment
              /[repository.msg]/issue/(scot %ud number.msg)
              %+  rap
                3
              :~  (scot %p src.bowl)
                  ' commented on issue #'
                  (decimal number.msg)
                  ' in '
                  repository.msg
                  ': '
                  title.updated-issue
              ==
          ==
        =.  notification-activities
          ?~  activity.notice
            notification-activities
          (notification-activity-put u.activity.notice)
        =/  notices=(list card)  cards.notice
        =/  replied=(quip card _this)
          %:  peer-forge-reply
            src.bowl
            request.msg
            repository.msg
            kind.msg
            number.msg
            %.y
            'comment added'
            `(native-issue-json updated-issue %.y)
          ==
        [(weld notices -.replied) +.replied]
      ++  comment-on-pull
        =/  pull=(unit native-pull:git)  (native-pull-at u.found number.msg)
        ?~  pull
          %:  peer-forge-reply
            src.bowl
            request.msg
            repository.msg
            kind.msg
            number.msg
            %.n
            'pull request not found'
            ~
          ==
        =/  comment=review-comment:git
          [(add 1 (lent comments.u.pull)) src.bowl body.msg now.bowl ~ ~ ~ %.n]
        =/  updated-pull=native-pull:git
          u.pull(comments (weld comments.u.pull ~[comment]))
        =/  pulls=(list native-pull:git)
          %+  turn  native-pulls.u.found
          |=  candidate=native-pull:git
          ?:(=(number.candidate number.msg) updated-pull candidate)
        =/  updated-repo=repository:git  u.found(native-pulls pulls)
        =.  repositories  (~(put by repositories) repository.msg updated-repo)
        =/  result=(unit json)
          (native-pull-detail-json repository.msg updated-repo updated-pull)
        ?~  result
          %:  peer-forge-reply
            src.bowl
            request.msg
            repository.msg
            kind.msg
            number.msg
            %.n
            'comment was added but pull request detail could not be rendered'
            ~
          ==
        =/  notice=notification-result
          %-  repository-notification
          :*  repository.msg
              updated-repo
              %pull-comment
              /[repository.msg]/pull/(scot %ud number.msg)
              %+  rap
                3
              :~  (scot %p src.bowl)
                  ' commented on pull request #'
                  (decimal number.msg)
                  ' in '
                  repository.msg
                  ': '
                  title.updated-pull
              ==
          ==
        =.  notification-activities
          ?~  activity.notice
            notification-activities
          (notification-activity-put u.activity.notice)
        =/  notices=(list card)  cards.notice
        =/  replied=(quip card _this)
          %:  peer-forge-reply
            src.bowl
            request.msg
            repository.msg
            kind.msg
            number.msg
            %.y
            'comment added'
            `u.result
          ==
        [(weld notices -.replied) +.replied]
      --
    ::
    ++  peer-forge-create-issue
      |=  msg=forge-create-issue:git-peer
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repository.msg)
      ?.  &(?=(^ found) (repository-readable u.found src.bowl))
        %:  peer-forge-reply
          src.bowl
          request.msg
          repository.msg
          %issue
          0
          %.n
          'repository is unavailable or requester is not authorized'
          ~
        ==
      ?.  ?&  !=('' title.msg)
              (lte (met 3 title.msg) 200)
              (lte (met 3 body.msg) 65.536)
          ==
        %:  peer-forge-reply
          src.bowl
          request.msg
          repository.msg
          %issue
          0
          %.n
          'title is required and limited to 200 bytes; body is limited to 64 KiB'
          ~
        ==
      =/  number=@ud  (add 1 (lent native-issues.u.found))
      =/  issue=native-issue:git
        [number src.bowl title.msg body.msg %open ~ ~ now.bowl now.bowl ~]
      =.  repositories
        (~(put by repositories) repository.msg u.found(native-issues [issue native-issues.u.found]))
      =/  updated-repo=repository:git  u.found(native-issues [issue native-issues.u.found])
      =/  result=json  (native-issue-json issue %.y)
      =/  notice=notification-result
        %-  repository-notification
        :*  repository.msg
            updated-repo
            %issue
            /[repository.msg]/issue/(scot %ud number)
            %+  rap
              3
            :~  (scot %p src.bowl)
                ' opened issue #'
                (decimal number)
                ' in '
                repository.msg
                ': '
                title.msg
            ==
        ==
      =.  notification-activities
        ?~  activity.notice
          notification-activities
        (notification-activity-put u.activity.notice)
      =/  notices=(list card)  cards.notice
      =/  dispatched=(quip card _this)
        (dispatch-webhooks repository.msg %issue result)
      =/  reply-cards=(list card)
        :~  %^  peer-card
              src.bowl
              /peer/forge-result/(scot %uv request.msg)
            [%forge-result request.msg repository.msg %issue 0 %.y 'issue opened' `result]
        ==
      :_  +.dispatched
      (weld -.dispatched (weld notices reply-cards))
    ::
    ++  peer-forge-result
      |=
        $:  request=@uv
            repository=@t
            kind=forge-kind:git-peer
            number=@ud
            ok=?
            message=@t
            result=(unit json)
        ==
      ^-  (quip card _this)
      =/  found=(unit peer-forge)  (~(get by peer-forges) request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(src.bowl peer.u.found)
              =(repository repository.u.found)
              =(kind kind.u.found)
              =(number number.u.found)
          ==
        `this
      =.  peer-forges
        (~(put by peer-forges) request u.found(active %.n, ok ok, message message, result result))
      `this
    ::
    ++  peer-offer-legacy
      |=  offer=offer:git-peer
      ^-  (quip card _this)
      %-  peer-offer
      [transfer.offer repository.offer source-repository.offer '' '' pull-request.offer title.offer]
    ::
    ++  peer-offer
      |=  offer=offer-branches:git-peer
      ^-  (quip card _this)
      =/  activity-kind=peer-activity-kind
        ?:(pull-request.offer %pull-request %push)
      =.  peer-activities
        %:  peer-activity-start
          transfer.offer
          activity-kind
          %incoming
          src.bowl
          repository.offer
          'update offered'
        ==
      =/  found=(unit repository:git)  (~(get by repositories) repository.offer)
      ?~  found
        =.  peer-activities
          (peer-activity-finish transfer.offer %.n 'destination repository not found')
        (peer-fail src.bowl transfer.offer 'destination repository not found')
      =/  selected-target-ref=@t
        ?:(=('' target-ref.offer) head.u.found target-ref.offer)
      =.  offer  offer(target-ref selected-target-ref)
      ?.  ?:  pull-request.offer
            ?&  ?|  =('' source-ref.offer)
                    ?&  (starts-with 'refs/heads/' source-ref.offer)
                        (valid-ref:git-protocol source-ref.offer)
                    ==
                ==
                (starts-with 'refs/heads/' target-ref.offer)
                (valid-ref:git-protocol target-ref.offer)
            ==
          %.y
        =.  peer-activities
          (peer-activity-finish transfer.offer %.n 'source and target refs must be valid branches')
        (peer-fail src.bowl transfer.offer 'source and target refs must be valid branches')
      ?.  ?:  pull-request.offer
            ?&  !=('' title.offer)
                (lte (met 3 title.offer) 200)
            ==
          %.y
        =.  peer-activities
          %^  peer-activity-finish
            transfer.offer
            %.n
          'pull request title is required and limited to 200 bytes'
        %^  peer-fail
          src.bowl
          transfer.offer
        'pull request title is required and limited to 200 bytes'
      =/  authorized=?
        ?:  pull-request.offer
          (repository-readable u.found src.bowl)
        (repository-writable u.found src.bowl)
      ?.  authorized
        =/  denied-message=@t
          ?:
            pull-request.offer
            'ship is not authorized to read this repository'
          'ship is not authorized to update this repository'
        =.  peer-activities
          (peer-activity-finish transfer.offer %.n denied-message)
        (peer-fail src.bowl transfer.offer denied-message)
      =/  target=(unit oid:git)  (~(get by refs.u.found) target-ref.offer)
      ?:  ?&  pull-request.offer
              ?=(~ target)
          ==
        =.  peer-activities
          (peer-activity-finish transfer.offer %.n 'target branch not found')
        (peer-fail src.bowl transfer.offer 'target branch not found')
      ?:  (~(has by peer-receiving) transfer.offer)
        =.  peer-activities
          (peer-activity-finish transfer.offer %.n 'transfer identifier is already active')
        (peer-fail src.bowl transfer.offer 'transfer identifier is already active')
      |^
        accept-offer
      ++  accept-offer
        =/  haves=(set oid:git)
          (silt (turn ~(tap by objects.u.found) |=(entry=[oid:git object:git] -.entry)))
        =/  flight=peer-receive
          :*  ?:(pull-request.offer %pull %push)
              %pack
              src.bowl
              source-repository.offer
              repository.offer
              title.offer
              source-ref.offer
              target-ref.offer
              public-read.u.found
              %.n
              ''
              ~
              0
              0
              0
              0
              ~
              ~
              now.bowl
              ~
              ~
              0
              0
              objects.u.found
          ==
        =.  peer-receiving  (~(put by peer-receiving) transfer.offer flight)
        :_  this
        :~  %^  peer-card
              src.bowl
              /peer/request/(scot %uv transfer.offer)
            [%request transfer.offer source-repository.offer haves]
            :*  %pass
                /peer/request-timeout/(scot %uv transfer.offer)
                %arvo
                %b
                %wait
                (add now.bowl ~s45)
            ==
        ==
      --
    ::
    ++  peer-result-received
      |=  [transfer=@uv ok=? message=@t]
      ^-  (quip card _this)
      =/  outgoing=(unit peer-offer-flight)  (~(get by peer-outgoing) transfer)
      ?~  outgoing  `this
      ?.  =(src.bowl peer.u.outgoing)  `this
      (peer-outgoing-finish transfer ok message)
    ::
    ++  peer-request
      |=  req=request:git-peer
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repository.req)
      ?~  found  (peer-fail src.bowl transfer.req 'repository not found')
      =/  origin-request=?
        ?~  peer-origin.u.found  %.n
        =(src.bowl ship.u.peer-origin.u.found)
      ?.  |((repository-readable u.found src.bowl) origin-request)
        (peer-fail src.bowl transfer.req 'ship is not authorized to read this repository')
      ?:  |((~(has by peer-serving) transfer.req) (~(has by peer-prepare-queue) transfer.req))
        :_  this
        :~  %^  peer-card
              src.bowl
              /peer/accepted/(scot %uv transfer.req)
            [%accepted transfer.req repository.req]
        ==
      =.  peer-prepare-queue
        (~(put by peer-prepare-queue) transfer.req [src.bowl req])
      :_  this
      :~  %^  peer-card
            src.bowl
            /peer/accepted/(scot %uv transfer.req)
          [%accepted transfer.req repository.req]
          [%pass /peer/prepare-start/(scot %uv transfer.req) %arvo %b %wait (add now.bowl ~s1)]
      ==
    ::
    ++  peer-accepted
      |=  msg=accepted:git-peer
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer.msg)
      ?~  found  `this
      ?.  ?&  =(src.bowl source.u.found)
              =(repository.msg source-repository.u.found)
          ==
        `this
      =/  next=peer-receive  u.found(accepted %.y, progress-at now.bowl)
      =.  peer-receiving  (~(put by peer-receiving) transfer.msg next)
      =.  peer-results
        %+  ~(put by peer-results)
          transfer.msg
        [%.n 'peer is preparing repository snapshot' local-repository.u.found]
      :_  this
      :~  [%pass /peer/prepare-timeout/(scot %uv transfer.msg) %arvo %b %wait (add now.bowl ~m10)]
      ==
    ::
    ++  peer-prepare
      |=  [target=ship req=request:git-peer]
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repository.req)
      ?~  found  (peer-fail target transfer.req 'repository not found')
      =/  origin-request=?
        ?~  peer-origin.u.found  %.n
        =(target ship.u.peer-origin.u.found)
      ?.  |((repository-readable u.found target) origin-request)
        (peer-fail target transfer.req 'ship is not authorized to read this repository')
      ?:  (~(has by peer-serving) transfer.req)
        `this
      =/  superseded=(list [@uv peer-serve])
        %+  skim  ~(tap by peer-serving)
        |=  entry=[@uv peer-serve]
        =/  prior=peer-serve  +.entry
        ?&  =(target target.prior)
            =(repository.req repository.prior)
        ==
      =.  peer-stream-jobs
        =/  entries=(list [@uv peer-serve])  superseded
        |-
        ?~  entries  peer-stream-jobs
        =.  peer-stream-jobs
          (~(del by peer-stream-jobs) -.i.entries)
        $(entries t.entries)
      =/  superseded-ids=(set @uv)
        (silt (turn superseded |=(entry=[@uv peer-serve] -.entry)))
      =/  superseded-activity-ids=(set @uv)
        (silt (turn superseded |=(entry=[@uv peer-serve] (peer-serve-activity-id -.entry))))
      =/  cleanup-cards=(list card)
        %-  zing
        %+  turn  superseded
        |=  entry=[@uv peer-serve]
        =/  old-transfer=@uv  -.entry
        =/  old=peer-serve  +.entry
        ?:  =(pages.old 0)  ~
        %+  turn  (gulf 1 pages.old)
        |=  revision=@ud
        :*  %pass
            /peer/cull/(scot %uv old-transfer)/(scot %ud revision)
            %cull
            [%ud revision]
            /fine/(peer-fine-name old-transfer)
        ==
      =.  peer-serving
        %-  malt
        %+  murn  ~(tap by peer-serving)
        |=  entry=[@uv peer-serve]
        ?:  (~(has in superseded-ids) -.entry)  ~
        `entry
      =.  peer-activities
        %+  turn  peer-activities
        |=  event=peer-activity
        ?.  (~(has in superseded-activity-ids) id.event)  event
        %=  event
          status  %failure
          message  'repository snapshot superseded by a newer request'
          when  now.bowl
        ==
      |^
        serve-snapshot
      ++  serve-snapshot
        =/  objects=(list [oid:git object:git])
          %+  murn  ~(tap by objects.u.found)
          |=  entry=[oid:git object:git]
          ?:  (~(has in haves.req) -.entry)  ~
          `entry
        =/  object-count=@ud  (lent objects)
        =/  object-bytes=@ud  (peer-object-bytes objects)
        =/  capable=?  (peer-object-capable transfer.req)
        ?.  capable
          (peer-fail target transfer.req 'peer must update %urgit to transfer repositories')
        ::  serve the repository in chunks: %objects streams object fragments page by
        ::  page, and %pack falls back to whole packed pages when the object set
        ::  outgrows the fragment stream's bounds.  both are drained by the peer over
        ::  %keen, so the receiver sees page-level progress and the serving ship
        ::  yields between pages instead of building one unbounded noun.
        ::
        =/  object-sizes-ok=?
          %+  levy  objects
          |=  entry=[oid:git object:git]
          (lte p.data.+.entry peer-stream-max-object-bytes)
        =/  stream-pages=@ud  (peer-object-batch-count objects)
        =/  streamable=?
          ?&  (lte object-count peer-stream-max-objects)
              object-sizes-ok
              (lte stream-pages peer-stream-max-pages)
          ==
        |^
          ?.  streamable
            serve-pack
          serve-objects
        ++  serve-pack
          =/  pages=(list octs)  (peer-object-pages objects)
          =/  flight=peer-serve
            [target transfer.req repository.req %pack (lent pages) object-bytes %.n objects]
          =.  peer-serving  (~(put by peer-serving) transfer.req flight)
          =.  peer-activities
            %:  peer-activity-start
              (peer-serve-activity-id transfer.req)
              %serve
              %incoming
              target
              repository.req
              'repository snapshot requested'
            ==
          =/  snapshot-path=path  /fine/(peer-fine-name transfer.req)
          =/  object-pages=(list card)
            %+  turn  pages
            |=  page=octs
            [%pass /peer/grow/(scot %uv transfer.req) %grow snapshot-path noun+!>(page)]
          =/  final-cards=(list card)
            :~  :*  %pass
                    /peer/ready/(scot %uv transfer.req)
                    %agent
                    [our.bowl %urgit]
                    %poke
                    %git-peer
                    !>
                    :*  %ready
                        transfer.req
                        repository.req
                        head.u.found
                        refs.u.found
                        (lent objects)
                        (lent pages)
                    ==
                ==
                :*  %pass
                    /peer/serve-timeout/(scot %uv transfer.req)
                    %arvo
                    %b
                    %wait
                    (add now.bowl (peer-serve-lifetime %pack (lent pages)))
                ==
            ==
          :_  this
          (weld cleanup-cards (weld object-pages final-cards))
        ++  serve-objects
          =/  pages=@ud  stream-pages
          =/  flight=peer-serve
            [target transfer.req repository.req %objects pages object-bytes %.n objects]
          =/  job=peer-stream-job
            :*  target
                transfer.req
                repository.req
                head.u.found
                refs.u.found
                (lent objects)
                pages
                1
                objects
                0
                %.n
            ==
          =.  peer-serving  (~(put by peer-serving) transfer.req flight)
          =.  peer-stream-jobs
            (~(put by peer-stream-jobs) transfer.req job)
          =.  peer-activities
            %:  peer-activity-start
              (peer-serve-activity-id transfer.req)
              %serve
              %incoming
              target
              repository.req
              'repository snapshot requested'
            ==
          :_  this
          %+  weld  cleanup-cards
          :~  %^  peer-card
                our.bowl
                /peer/stream-next/(scot %uv transfer.req)
              [%stream-next transfer.req]
              :*  %pass
                  /peer/serve-timeout/(scot %uv transfer.req)
                  %arvo
                  %b
                  %wait
                  (add now.bowl (peer-serve-lifetime %objects pages))
              ==
          ==
        --
      ::
      --
    ::
    ++  peer-stream-next
      |=  transfer=@uv
      ^-  (quip card _this)
      ?.  =(src.bowl our.bowl)  `this
      =/  found=(unit peer-stream-job)
        (~(get by peer-stream-jobs) transfer)
      ?~  found  `this
      =/  job=peer-stream-job  u.found
      =/  serving=(unit peer-serve)  (~(get by peer-serving) transfer)
      ?~  serving  `this
      ?.  =(%objects mode.u.serving)  `this
      =/  taken
        (peer-object-batch remaining.job offset.job)
      =.  peer-stream-jobs
        (~(put by peer-stream-jobs) transfer job(remaining remaining.taken, offset offset.taken))
      :_  this
      :~  :*  %pass
              /peer/grow/(scot %uv transfer)
              %grow
              /fine/(peer-fine-name transfer)
              noun+!>(batch.taken)
          ==
          (peer-card our.bowl /peer/stream-grown/(scot %uv transfer) [%stream-grown transfer])
      ==
    ::
    ++  peer-stream-grown
      |=  transfer=@uv
      ^-  (quip card _this)
      ?.  =(src.bowl our.bowl)  `this
      =/  found=(unit peer-stream-job)
        (~(get by peer-stream-jobs) transfer)
      ?~  found  `this
      =/  job=peer-stream-job  u.found
      =/  serving=(unit peer-serve)  (~(get by peer-serving) transfer)
      ?~  serving  `this
      ?.  =(%objects mode.u.serving)  `this
      =/  next=peer-stream-job
        job(revision +(revision.job), begun %.y)
      =/  cards=(list card)
        ?~  remaining.job
          ~
        :~  (peer-card our.bowl /peer/stream-next/(scot %uv transfer) [%stream-next transfer])
        ==
      =.  cards
        ?:  begun.job  cards
        :*  %:  peer-card
              target.job
              /peer/begin-objects/(scot %uv transfer)
              :*  %begin-objects
                  transfer
                  repository.job
                  revision.job
                  head.job
                  refs.job
                  expected.job
                  pages.job
              ==
            ==
            cards
        ==
      =.  peer-stream-jobs
        (~(put by peer-stream-jobs) transfer next)
      :_  this
      cards
    ::
    ++  peer-ready
      |=  msg=ready:git-peer
      ^-  (quip card _this)
      ?.  =(src.bowl our.bowl)  `this
      =/  found=(unit peer-serve)  (~(get by peer-serving) transfer.msg)
      ?~  found  `this
      :_  this
      :~  %^  peer-card
            target.u.found
            /peer/begin/(scot %uv transfer.msg)
          [%begin transfer.msg repository.msg 1 head.msg refs.msg objects.msg pages.msg]
      ==
    ::
    ++  peer-archive-ready
      |=  msg=archive-ready:git-peer
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer.msg)
      ?~  found  `this
      =/  flight=peer-receive  u.found
      ?.  ?&  =(src.bowl source.flight)
              =(repository.msg source-repository.flight)
              accepted.flight
              =('' head.flight)
              (peer-object-capable transfer.msg)
          ==
        `this
      ?:  (gth objects.msg peer-archive-max-objects)
        (peer-snapshot-fail transfer.msg 'peer announced too many repository archive objects')
      ?:  (gth bytes.msg peer-archive-max-bytes)
        (peer-snapshot-fail transfer.msg 'peer announced an oversized repository archive')
      =/  next=peer-receive
        %=  flight
          mode  %archive
          head  head.msg
          refs  refs.msg
          expected  objects.msg
          expected-bytes  bytes.msg
          pages  1
          progress-at  now.bowl
        ==
      =.  peer-receiving  (~(put by peer-receiving) transfer.msg next)
      =.  peer-results
        %+  ~(put by peer-results)
          transfer.msg
        [%.n 'receiving repository archive' local-repository.flight]
      :_  this
      :~  %^  peer-card
            source.flight
            /peer/archive-accept/(scot %uv transfer.msg)
          [%archive-accept transfer.msg]
          [%pass /peer/archive-timeout/(scot %uv transfer.msg) %arvo %b %wait (add now.bowl ~d1)]
      ==
    ::
    ++  peer-archive-accept
      |=  transfer=@uv
      ^-  (quip card _this)
      =/  found=(unit peer-serve)  (~(get by peer-serving) transfer)
      ?~  found  `this
      =/  flight=peer-serve  u.found
      ?.  ?&  =(src.bowl target.flight)
              =(%archive mode.flight)
              =(%.n sent.flight)
          ==
        `this
      =.  peer-serving
        (~(put by peer-serving) transfer flight(sent %.y))
      :_  this
      :~  %^  peer-card
            target.flight
            /peer/archive/(scot %uv transfer)
          [%archive transfer repository.flight objects.flight]
      ==
    ::
    ++  peer-begin
      |=  msg=begin:git-peer
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer.msg)
      ?~  found  `this
      ?.  ?&  =(src.bowl source.u.found)
              =(repository.msg source-repository.u.found)
          ==
        `this
      ?.  ?&  (gth pages.msg 0)
              (lte pages.msg (max 1 objects.msg))
          ==
        :_  this
        :~  :*  %pass
                /peer/begin-error/(scot %uv transfer.msg)
                %agent
                [our.bowl %urgit]
                %poke
                %git-peer
                !>([%snapshot-error transfer.msg 'peer announced an invalid Fine page count'])
            ==
        ==
      =/  next=peer-receive
        %=  u.found
          mode  %pack
          head  head.msg
          refs  refs.msg
          expected  objects.msg
          pages  pages.msg
          completed  ~
          pending-pages  ~
          progress-at  now.bowl
          fine-progress  ~
          assemblies  ~
        ==
      =.  peer-receiving  (~(put by peer-receiving) transfer.msg next)
      =.  peer-results
        %+  ~(put by peer-results)
          transfer.msg
        [%.n 'reading repository over Fine' local-repository.u.found]
      ?:  =(src.bowl our.bowl)
        =/  serving=(unit peer-serve)  (~(get by peer-serving) transfer.msg)
        ?~  serving
          (peer-snapshot-fail transfer.msg 'local repository snapshot is unavailable')
        (peer-snapshot transfer.msg (silt objects.u.serving))
      :_  this
      =/  scry-path=path
        /g/x/1/urgit//1/fine/(peer-fine-name transfer.msg)
      :~  [%pass /peer/fine/(scot %uv transfer.msg)/1 %keen %.n src.bowl scry-path]
      ==
    ::
    ++  peer-begin-objects
      |=  msg=begin-objects:git-peer
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer.msg)
      ?~  found  `this
      ?.  ?&  =(src.bowl source.u.found)
              =(repository.msg source-repository.u.found)
          ==
        `this
      ?.  ?&  (gth objects.msg 0)
              (lte objects.msg peer-stream-max-objects)
              (gth pages.msg 0)
              (lte pages.msg peer-stream-max-pages)
              (lte pages.msg (mul objects.msg 16))
              =(revision.msg 1)
          ==
        :_  this
        :~  :*  %pass
                /peer/begin-error/(scot %uv transfer.msg)
                %agent
                [our.bowl %urgit]
                %poke
                %git-peer
                !>([%snapshot-error transfer.msg 'peer announced invalid streamed object bounds'])
            ==
        ==
      =/  next=peer-receive
        %=  u.found
          mode  %objects
          head  head.msg
          refs  refs.msg
          expected  objects.msg
          pages  pages.msg
          completed  ~
          pending-pages  ~
          progress-at  now.bowl
          fine-progress  ~
          assemblies  ~
          assembly-bytes  0
          assembly-count  0
        ==
      =.  peer-receiving  (~(put by peer-receiving) transfer.msg next)
      =.  peer-results
        %+  ~(put by peer-results)
          transfer.msg
        [%.n 'reading repository over Fine' local-repository.u.found]
      ?:  =(src.bowl our.bowl)
        =/  serving=(unit peer-serve)  (~(get by peer-serving) transfer.msg)
        ?~  serving
          (peer-snapshot-fail transfer.msg 'local repository snapshot is unavailable')
        (peer-snapshot transfer.msg (silt objects.u.serving))
      :_  this
      %+  turn  (gulf 1 (min pages.msg peer-stream-window))
      |=  revision=@ud
      =/  scry-path=path
        /g/x/(scot %ud revision)/urgit//1/fine/(peer-fine-name transfer.msg)
      [%pass /peer/fine/(scot %uv transfer.msg)/(scot %ud revision) %keen %.n src.bowl scry-path]
    ::
    ++  peer-release
      |=  transfer=@uv
      ^-  (quip card _this)
      =/  queued=(unit [target=ship req=request:git-peer])
        (~(get by peer-prepare-queue) transfer)
      ?^  queued
        ?.  =(src.bowl target.u.queued)  `this
        `this(peer-prepare-queue (~(del by peer-prepare-queue) transfer))
      =/  found=(unit peer-serve)  (~(get by peer-serving) transfer)
      ?~  found  `this
      ?.  =(src.bowl target.u.found)  `this
      =.  peer-activities
        (peer-activity-finish (peer-serve-activity-id transfer) %.y 'repository snapshot delivered')
      =/  count=@ud  pages.u.found
      =/  culls=(list card)
        ?:  =(0 count)  ~
        %+  turn  (gulf 1 count)
        |=  revision=@ud
        :*  %pass
            /peer/cull/(scot %uv transfer)/(scot %ud revision)
            %cull
            [%ud revision]
            /fine/(peer-fine-name transfer)
        ==
      :_  %=  this
            peer-serving  (~(del by peer-serving) transfer)
            peer-stream-jobs  (~(del by peer-stream-jobs) transfer)
          ==
      culls
    ::
    ++  peer-archive
      |=  [transfer=@uv repository=@t incoming=(list [oid:git object:git])]
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer)
      ?~  found  `this
      =/  flight=peer-receive  u.found
      ?.  ?&  =(src.bowl source.flight)
              =(repository source-repository.flight)
              (peer-object-capable transfer)
              =(%archive mode.flight)
          ==
        `this
      =/  count=@ud  (lent incoming)
      ?.  =(count expected.flight)
        (peer-snapshot-fail transfer 'repository archive object count did not match its header')
      =/  bytes=@ud  (peer-object-bytes incoming)
      ?.  =(bytes expected-bytes.flight)
        (peer-snapshot-fail transfer 'repository archive byte count did not match its header')
      =/  object-sizes-ok=?
        %+  levy  incoming
        |=  entry=[oid:git object:git]
        (lte p.data.+.entry peer-archive-max-object-bytes)
      ?.  object-sizes-ok
        (peer-snapshot-fail transfer 'peer announced an oversized repository archive object')
      =/  incoming-map=(map oid:git object:git)  (malt incoming)
      ?.  =(count (lent ~(tap by incoming-map)))
        (peer-snapshot-fail transfer 'peer announced duplicate repository objects')
      (peer-snapshot transfer incoming-map)
    ::
    ++  peer-snapshot-fail
      |=  [transfer=@uv message=@t]
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer)
      ?~  found  `this
      =/  flight=peer-receive  u.found
      ?.  |(=(src.bowl our.bowl) =(src.bowl source.flight))  `this
      =.  peer-stream-jobs  (~(del by peer-stream-jobs) transfer)
      =.  peer-receiving  (~(del by peer-receiving) transfer)
      =.  peer-activities  (peer-activity-finish transfer %.n message)
      =/  release=card
        (peer-card source.flight /peer/release/(scot %uv transfer) [%release transfer])
      =/  cancel-cards=(list card)
        ?:  =('' head.flight)  ~
        (peer-transfer-yawns transfer source.flight mode.flight pages.flight completed.flight)
      ?:  =(%fork purpose.flight)
        =.  peer-results
          (~(put by peer-results) transfer [%.n message local-repository.flight])
        [(weld cancel-cards [release ~]) this]
      =/  result-cards=(list card)
        :~  release
            %^  peer-card
              source.flight
              /peer/result/(scot %uv transfer)
            [%result transfer %.n message]
        ==
      :_  this
      (weld cancel-cards result-cards)
    ::
    ++  peer-object-fragments
      |=  [transfer=@uv revision=@ud fragments=(list object-fragment:git-peer)]
      ^-  (quip card _this)
      ?.  =(src.bowl our.bowl)  `this
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer)
      ?~  found  `this
      =/  flight=peer-receive  u.found
      ?.  =(%objects mode.flight)  `this
      ?.  &((gth revision 0) (lte revision pages.flight))
        (peer-snapshot-fail transfer 'Fine repository fragment page used an invalid revision')
      ?:  =((lent fragments) 0)
        (peer-snapshot-fail transfer 'Fine repository fragment page was empty')
      ?:  (gth (lent fragments) peer-stream-page-max-fragments)
        (peer-snapshot-fail transfer 'Fine repository fragment page exceeded its fragment limit')
      =/  page-bytes=@ud
        %+  roll  fragments
        |=  [fragment=object-fragment:git-peer sum=@ud]
        (add p.data.fragment sum)
      ?:  (gth page-bytes peer-stream-page-max-bytes)
        (peer-snapshot-fail transfer 'Fine repository fragment page exceeded its byte limit')
      =/  expected-revision=@ud  +(~(wyt in completed.flight))
      ?.  =(revision expected-revision)
        ?:  (~(has by pending-pages.flight) revision)  `this
        =/  cached=peer-receive
          %=  flight
            pending-pages  (~(put by pending-pages.flight) revision fragments)
            progress-at  now.bowl
          ==
        `this(peer-receiving (~(put by peer-receiving) transfer cached))
      =/  assembled=(unit peer-receive)
        (assemble-peer-fragments flight fragments)
      ?~  assembled
        %+  peer-snapshot-fail
          transfer
        'Fine repository fragment page was malformed, duplicate, non-contiguous, inconsistent, or content-invalid'
      =/  next=peer-receive
        %=  u.assembled
          completed  (~(put in completed.u.assembled) revision)
          progress-at  now.bowl
        ==
      =.  peer-receiving  (~(put by peer-receiving) transfer next)
      =/  all-pages=?  =(revision pages.next)
      ?:  all-pages
        ?:  ?|  !=(received.next expected.next)
                ?=(^ assemblies.next)
            ==
          %+  peer-snapshot-fail
            transfer
          'Fine repository fragment stream ended with incomplete objects'
        =/  finished=(quip card _this)  (peer-finish transfer)
        =/  release=card
          (peer-card source.flight /peer/release/(scot %uv transfer) [%release transfer])
        [(weld [release ~] -.finished) +.finished]
      |^
        request-next-page
      ++  request-next-page
        =/  cards=(list card)  ~
        =/  next-request=@ud  (add revision peer-stream-window)
        =?  cards  (lte next-request pages.next)
          =/  next-path=path
            /g/x/(scot %ud next-request)/urgit//1/fine/(peer-fine-name transfer)
          :*  :*  %pass
                  /peer/fine/(scot %uv transfer)/(scot %ud next-request)
                  %keen
                  %.n
                  source.flight
                  next-path
              ==
              cards
          ==
        =/  next-revision=@ud  +(revision)
        =/  cached=(unit (list object-fragment:git-peer))
          (~(get by pending-pages.next) next-revision)
        ?~  cached  [cards this]
        =.  next  next(pending-pages (~(del by pending-pages.next) next-revision))
        =.  peer-receiving  (~(put by peer-receiving) transfer next)
        =.  cards
          :*  %:  peer-card
                our.bowl
                /peer/object-drain/(scot %uv transfer)/(scot %ud next-revision)
                [%object-fragments transfer next-revision u.cached]
              ==
              cards
          ==
        [cards this]
      --
    ::
    ++  peer-snapshot
      |=  [transfer=@uv incoming=(map oid:git object:git)]
      ^-  (quip card _this)
      =/  found=(unit peer-receive)  (~(get by peer-receiving) transfer)
      ?~  found  `this
      =/  flight=peer-receive  u.found
      ?.  |(=(src.bowl our.bowl) =(src.bowl source.flight))  `this
      =/  count=@ud  (lent ~(tap by incoming))
      ?.  (lte (add received.flight count) expected.flight)
        (peer-snapshot-fail transfer 'repository snapshot exceeded the expected object count')
      ?:  ?&  (gth expected.flight 0)
              =(count 0)
          ==
        (peer-snapshot-fail transfer 'repository snapshot contained an empty object page')
      =/  novel=?
        %+  levy  ~(tap by incoming)
        |=  entry=[oid:git object:git]
        !(~(has by objects.flight) -.entry)
      ?.  novel
        (peer-snapshot-fail transfer 'repository snapshot contained a duplicate object')
      =/  valid=?
        %+  levy  ~(tap by incoming)
        |=  entry=[oid:git object:git]
        =/  =object:git  +.entry
        =(-.entry (object-oid:git-codec kind.object data.object))
      ?.  valid
        (peer-snapshot-fail transfer 'repository object failed content-address validation')
      =/  next=peer-receive
        %=  flight
          objects  (merge-objects objects.flight incoming)
          received  (add received.flight count)
          progress-at  now.bowl
        ==
      =.  peer-receiving  (~(put by peer-receiving) transfer next)
      ?.  =(received.next expected.next)
        `this
      =/  finished=(quip card _this)  (peer-finish transfer)
      =/  release=card
        (peer-card source.flight /peer/release/(scot %uv transfer) [%release transfer])
      [(weld [release ~] -.finished) +.finished]
    ::
    ++  handle-action
      |=  act=action:git
      ^-  (quip card _this)
      |^
        ?-  -.act
          %create  create
          %delete  `this(repositories (~(del by repositories) name.act))
          %put-object  put-object
          %set-ref  set-ref
          %delete-ref  delete-ref
          %set-protected  set-protected
          %set-head  set-head
          %set-public  set-public
          %set-description  set-description
          %grant-writer  grant-writer
          %revoke-writer  revoke-writer
          %grant-reader  grant-reader
          %revoke-reader  revoke-reader
          %set-group-policy  set-group-policy
          %set-write-token  set-write-token
          %clear-write-token  clear-write-token
          %bind-desk  bind-desk
          %unbind-desk  unbind-desk
          %publish-desk  publish-desk
          %add-peer  `this(peers (~(put in peers) peer.act))
          %remove-peer  `this(peers (~(del in peers) peer.act))
        ==
      ::
      ++  create
        ^-  (quip card _this)
        ?>  ?=(%create -.act)
        ?:  (~(has by repositories) name.act)
          `this
        =/  repo=repository:git
          :*  our.bowl
              public-read.act
              ''
              'refs/heads/main'
              ~
              ~
              ~
              (silt ~[our.bowl])
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              ~
              default-notification-events
              ~
          ==
        `this(repositories (~(put by repositories) name.act repo))
      ::
      ++  put-object
        ^-  (quip card _this)
        ?>  ?=(%put-object -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  =oid:git  (object-oid:git-codec kind.act data.act)
        =/  repo=repository:git
          u.found(objects (~(put by objects.u.found) oid [kind.act data.act]))
        `this(repositories (~(put by repositories) repository.act repo))
      ::
      ++  set-ref
        ^-  (quip card _this)
        ?>  ?=(%set-ref -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        ?.  (~(has by objects.u.found) oid.act)  `this
        =/  old=(unit oid:git)  (~(get by refs.u.found) ref.act)
        =/  repo=repository:git  u.found(refs (~(put by refs.u.found) ref.act oid.act))
        =.  repositories  (~(put by repositories) repository.act repo)
        (dispatch-webhooks repository.act %push (push-event-json ~[[old `oid.act ref.act]]))
      ::
      ++  delete-ref
        ^-  (quip card _this)
        ?>  ?=(%delete-ref -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  old=(unit oid:git)  (~(get by refs.u.found) ref.act)
        ?~  old  `this
        =/  repo=repository:git  u.found(refs (~(del by refs.u.found) ref.act))
        =.  repositories  (~(put by repositories) repository.act repo)
        (dispatch-webhooks repository.act %push (push-event-json ~[[old ~ ref.act]]))
      ::
      ++  set-protected
        ^-  (quip card _this)
        ?>  ?=(%set-protected -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        ?.  ?&  (valid-ref:git-protocol ref.act)
                (starts-with 'refs/heads/' ref.act)
            ==
          `this
        =/  protected-refs=(set @t)
          ?:
            protected.act
            (~(put in protected-refs.u.found) ref.act)
          (~(del in protected-refs.u.found) ref.act)
        =/  repo=repository:git  u.found(protected-refs protected-refs)
        `this(repositories (~(put by repositories) repository.act repo))
      ::
      ++  set-head
        ^-  (quip card _this)
        ?>  ?=(%set-head -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        `this(repositories (~(put by repositories) repository.act u.found(head ref.act)))
      ::
      ++  set-public
        ^-  (quip card _this)
        ?>  ?=(%set-public -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        :-  ~
        %=  this
          repositories  (~(put by repositories) repository.act u.found(public-read public-read.act))
        ==
      ::
      ++  set-description
        ^-  (quip card _this)
        ?>  ?=(%set-description -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  clean=@t  (crip (scag 500 (trip description.act)))
        `this(repositories (~(put by repositories) repository.act u.found(description clean)))
      ::
      ++  grant-writer
        ^-  (quip card _this)
        ?>  ?=(%grant-writer -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  repo=repository:git  u.found(writers (~(put in writers.u.found) writer.act))
        `this(repositories (~(put by repositories) repository.act repo))
      ::
      ++  revoke-writer
        ^-  (quip card _this)
        ?>  ?=(%revoke-writer -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  repo=repository:git  u.found(writers (~(del in writers.u.found) writer.act))
        `this(repositories (~(put by repositories) repository.act repo))
      ::
      ++  grant-reader
        ^-  (quip card _this)
        ?>  ?=(%grant-reader -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  repo=repository:git  u.found(readers (~(put in readers.u.found) reader.act))
        `this(repositories (~(put by repositories) repository.act repo))
      ::
      ++  revoke-reader
        ^-  (quip card _this)
        ?>  ?=(%revoke-reader -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  repo=repository:git  u.found(readers (~(del in readers.u.found) reader.act))
        `this(repositories (~(put by repositories) repository.act repo))
      ::
      ++  set-group-policy
        ^-  (quip card _this)
        ?>  ?=(%set-group-policy -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        ::  a group can drive access only while this ship is seated in it: hosted
        ::  groups are authoritative, joined ones are read from a trusted mirror
        ~|  %group-policy-this-ship-not-a-member
        ?>  ?|  ?=(~ policy.act)
                !=(~ (group-seat policy.act our.bowl))
            ==
        `this(repositories (~(put by repositories) repository.act u.found(group-policy policy.act)))
      ::
      ++  set-write-token
        ^-  (quip card _this)
        ?>  ?=(%set-write-token -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        =/  digest=@  (shas %git-write-token token.act)
        :-  ~
        %=  this
          repositories
            (~(put by repositories) repository.act u.found(write-token-hash `digest))
        ==
      ::
      ++  clear-write-token
        ^-  (quip card _this)
        ?>  ?=(%clear-write-token -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        `this(repositories (~(put by repositories) repository.act u.found(write-token-hash ~)))
      ::
      ++  bind-desk
        ^-  (quip card _this)
        ?>  ?=(%bind-desk -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        ?.  (valid-ref:git-protocol branch.act)  `this
        ?.  (starts-with 'refs/heads/' branch.act)  `this
        =/  desks=(unit (set desk))
          %-  mole
          |.(.^((set desk) %cd /(scot %p our.bowl)//(scot %da now.bowl)))
        ?~  desks  `this
        ?.  (~(has in u.desks) desk-name.act)  `this
        =/  binding=desk-binding:git  [desk-name.act branch.act ~ ~ ~]
        `this(repositories (~(put by repositories) repository.act u.found(binding `binding)))
      ::
      ++  unbind-desk
        ^-  (quip card _this)
        ?>  ?=(%unbind-desk -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        `this(repositories (~(put by repositories) repository.act u.found(binding ~)))
      ::
      ++  publish-desk
        ^-  (quip card _this)
        ?>  ?=(%publish-desk -.act)
        =/  found=(unit repository:git)  (~(get by repositories) repository.act)
        ?~  found  `this
        ?~  binding.u.found  `this
        ?^  pending-clay  `this
        ?^  pending-publish  `this
        =/  desks=(unit (set desk))
          %-  mole
          |.(.^((set desk) %cd /(scot %p our.bowl)//(scot %da now.bowl)))
        ?~  desks  `this
        ?.  (~(has in u.desks) desk-name.u.binding.u.found)  `this
        =/  desk-files=(unit (list spur))
          %-  mole
          |.
          .^((list spur) %ct /(scot %p our.bowl)/[desk-name.u.binding.u.found]/(scot %da now.bowl))
        ?~  desk-files  `this
        =/  job=publish-job
          :*  repository.act
              desk-name.u.binding.u.found
              branch.u.binding.u.found
              ~
              message.act
              u.desk-files
              ~
          ==
        ?~  paths.job
          =/  published=(unit repository:git)
            (publish-repository u.found job our.bowl now.bowl)
          ?~  published  `this
          =.  repositories  (~(put by repositories) repository.job u.published)
          `this
        :_  this(pending-publish `job)
        (publish-next job)
      --
    ::
    ++  publish-next
      |=  job=publish-job
      ^-  (list card)
      ?~  paths.job  ~
      :~  :*  %pass
              /clay-publish
              %arvo
              %c
              %warp
              our.bowl
              desk-name.job
              ~
              %sing
              %q
              da+now.bowl
              i.paths.job
          ==
      ==
    ::
    ++  parse-group-policy
      |=  jon=json
      ^-  (each group-policy:git @t)
      =/  host-text=(unit @t)  (string-at 'host' jon)
      =/  host=(unit @p)  ?~(host-text `our.bowl (slaw %p u.host-text))
      ?~  host
        [%| 'host must be a valid ship name']
      =/  group-text=(unit @t)  (string-at 'group' jon)
      =/  group=(unit @tas)  ?~(group-text ~ (slaw %tas u.group-text))
      ?~  group
        [%| 'group must be a valid group name']
      ?~  (group-seat `[[u.host u.group] %none ~] our.bowl)
        [%| 'this ship is not a member of that group']
      =/  base-text=(unit @t)  (string-at 'base' jon)
      =/  base=(unit capability:git)  ?~(base-text `%none (parse-capability u.base-text))
      ?~  base
        [%| 'base must be none, read or write']
      =/  roles-json=(unit json)  (json-at 'roles' jon)
      ?.  |(?=(~ roles-json) ?=([~ %o *] roles-json))
        [%| 'roles must map role ids to none, read or write']
      =/  remaining=(list [@t json])
        ?~  roles-json  ~
        ?.  ?=([%o *] u.roles-json)  ~
        ~(tap by p.u.roles-json)
      =/  roles=(map @tas capability:git)  ~
      |-
      ?~  remaining
        [%& [[u.host u.group] u.base roles]]
      =/  role=(unit @tas)  (slaw %tas -.i.remaining)
      ?~  role
        [%| 'role ids must be valid role names']
      =/  cap=(unit capability:git)
        ?.  ?=([%s *] +.i.remaining)  ~
        (parse-capability p.+.i.remaining)
      ?~  cap
        [%| 'roles must map role ids to none, read or write']
      $(remaining t.remaining, roles (~(put by roles) u.role u.cap))
    ::
    ++  api-with-action
      |=  [eyre-id=@ta status=@ud act=action:git]
      ^-  (quip card _this)
      =/  [cards=(list card) next=_this]  (handle-action act)
      [(weld cards (api-ok eyre-id status)) next]
    ::
    ++  peer-results-json
      ^-  json
      %-  peer-ui-transfers-json
      [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
    ::
    ++  peer-discoveries-json
      ^-  json
      =/  entries=(list json)
        %+  turn  ~(tap by peer-discoveries)
        |=  entry=[@uv peer-discovery]
        =/  request=@uv  -.entry
        =/  discovery=peer-discovery  +.entry
        =/  repositories-json=(list json)
          %+  turn  repositories.discovery
          |=  repo=catalog-repository:git-peer
          %-  pairs:enjs:format
          :~  ['name' s+name.repo]
              ['head' s+head.repo]
              ['refs' n+(decimal refs.repo)]
              ['objects' n+(decimal objects.repo)]
              ['writable' b+writable.repo]
              ['via' (group-flag-json via.repo)]
          ==
        %-  pairs:enjs:format
        :~  ['request' s+(scot %uv request)]
            ['ship' s+(scot %p peer.discovery)]
            ['active' b+active.discovery]
            ['ok' b+ok.discovery]
            ['message' s+message.discovery]
            ['status' s+(status-text:git-catalog status.discovery)]
            ['repositories' [%a repositories-json]]
            ['group' (group-flag-json group.discovery)]
            ['heldSince' ?~(held-since.discovery ~ s+(iso:git-catalog u.held-since.discovery))]
        ==
      (pairs:enjs:format ~[['discoveries' [%a entries]]])
    ::
    ::  a group flag as Groups writes it, "~host/name", or null
    ::
    ++  group-flag-json
      |=  group=(unit [host=@p name=@tas])
      ^-  json
      ?~  group  ~
      s+(rap 3 (scot %p host.u.group) '/' name.u.group ~)
    ::
    ++  parse-group-flag
      |=  text=@t
      ^-  (unit [host=@p name=@tas])
      (rush text ;~(plug ;~(pfix sig fed:ag) ;~(pfix fas sym)))
    ::
    ++  peers-json
      ^-  json
      =/  entries=(list json)
        (turn ~(tap in peers) |=(peer=@p s+(scot %p peer)))
      (pairs:enjs:format ~[['ship' s+(scot %p our.bowl)] ['peers' [%a entries]]])
    ::
    ++  peer-browses-json
      ^-  json
      =/  entries=(list json)
        %+  turn  ~(tap by peer-browses)
        |=  entry=[@uv peer-browse]
        =/  request=@uv  -.entry
        =/  browse=peer-browse  +.entry
        %-  pairs:enjs:format
        :~  ['request' s+(scot %uv request)]
            ['ship' s+(scot %p peer.browse)]
            ['repository' s+repository.browse]
            ['view' s+view.browse]
            ['number' n+(decimal number.browse)]
            ['path' s+(spat file-path.browse)]
            ['phase' s+phase.browse]
            ['active' b+active.browse]
            ['ok' b+ok.browse]
            ['message' s+message.browse]
            :-  'progress'
            ?~  progress.browse  ~
            %-  pairs:enjs:format
            :~  ['blockExponent' n+(decimal boq.u.progress.browse)]
                ['received' n+(decimal fag.u.progress.browse)]
                ['expected' n+(decimal tot.u.progress.browse)]
            ==
            ['result' ?~(result.browse ~ u.result.browse)]
        ==
      (pairs:enjs:format ~[['browses' [%a entries]]])
    ::
    ++  start-peer-browse
      |=  [eyre-id=@ta peer=ship repository=@t view=browse-view:git-peer number=@ud file-path=path]
      ^-  (quip card _this)
      =/  duplicate=(unit [@uv peer-browse])
        =/  matches=(list [@uv peer-browse])
          %+  murn  ~(tap by peer-browses)
          |=  entry=[@uv peer-browse]
          =/  browse=peer-browse  +.entry
          ?.  ?&  active.browse
                  =(peer peer.browse)
                  =(repository repository.browse)
                  =(view view.browse)
                  =(number number.browse)
                  =(file-path file-path.browse)
              ==
            ~
          `entry
        ?~(matches ~ `i.matches)
      ?^  duplicate
        :_  this
        %^  api-json
          eyre-id
          202
        %-  pairs:enjs:format
        ~[['ok' b+%.y] ['request' s+(scot %uv -.u.duplicate)] ['deduplicated' b+%.y]]
      =/  request=@uv
        (peer-object-transfer `@uv`(shas %git-peer-browse (cat 3 eny.bowl request-count)))
      =.  request-count  +(request-count)
      =.  peer-browses
        %+  ~(put by peer-browses)
          request
        :*  peer
            repository
            view
            number
            file-path
            %request
            %.y
            %.n
            'reading from peer'
            ~
            now.bowl
            0
            0
            ~
            ~
        ==
      :_  this
      %+  weld
        :~  %^  peer-card
              peer
              /peer/browse-request/(scot %uv request)
            [%browse-request request repository view number file-path]
            [%pass /peer/browse-timeout/(scot %uv request) %arvo %b %wait (add now.bowl ~s45)]
        ==
      (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y] ['request' s+(scot %uv request)]]))
    ::
    ++  peer-forges-json
      ^-  json
      =/  entries=(list json)
        %+  turn  ~(tap by peer-forges)
        |=  entry=[@uv peer-forge]
        =/  request=@uv  -.entry
        =/  forge=peer-forge  +.entry
        %-  pairs:enjs:format
        :~  ['request' s+(scot %uv request)]
            ['ship' s+(scot %p peer.forge)]
            ['repository' s+repository.forge]
            ['kind' s+kind.forge]
            ['number' n+(decimal number.forge)]
            ['active' b+active.forge]
            ['ok' b+ok.forge]
            ['message' s+message.forge]
            ['result' ?~(result.forge ~ u.result.forge)]
        ==
      (pairs:enjs:format ~[['requests' [%a entries]]])
    ::
    ++  start-peer-forge-comment
      |=  [eyre-id=@ta peer=ship repository=@t kind=forge-kind:git-peer number=@ud body=@t]
      ^-  (quip card _this)
      =/  duplicate=(unit [@uv peer-forge])
        =/  matches=(list [@uv peer-forge])
          %+  murn  ~(tap by peer-forges)
          |=  entry=[@uv peer-forge]
          =/  forge=peer-forge  +.entry
          ?.  ?&  active.forge
                  =(peer peer.forge)
                  =(repository repository.forge)
                  =(kind kind.forge)
                  =(number number.forge)
              ==
            ~
          `entry
        ?~(matches ~ `i.matches)
      ?^  duplicate
        :_  this
        %^  api-json
          eyre-id
          202
        %-  pairs:enjs:format
        ~[['ok' b+%.y] ['request' s+(scot %uv -.u.duplicate)] ['deduplicated' b+%.y]]
      =/  request=@uv
        `@uv`(shas %git-peer-forge (cat 3 eny.bowl request-count))
      =.  request-count  +(request-count)
      =.  peer-forges
        %+  ~(put by peer-forges)
          request
        [peer repository kind number %.y %.n 'sending comment to repository owner' ~]
      :_  this
      %+  weld
        :~  %^  peer-card
              peer
              /peer/forge-comment/(scot %uv request)
            [%forge-comment request repository kind number body]
            [%pass /peer/forge-timeout/(scot %uv request) %arvo %b %wait (add now.bowl ~s30)]
        ==
      (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y] ['request' s+(scot %uv request)]]))
    ::
    ++  start-peer-forge-issue
      |=  [eyre-id=@ta peer=ship repository=@t title=@t body=@t]
      ^-  (quip card _this)
      =/  duplicate=(unit [@uv peer-forge])
        =/  matches=(list [@uv peer-forge])
          %+  murn  ~(tap by peer-forges)
          |=  entry=[@uv peer-forge]
          =/  forge=peer-forge  +.entry
          ?.  ?&  active.forge
                  =(peer peer.forge)
                  =(repository repository.forge)
                  =(%issue kind.forge)
                  =(0 number.forge)
              ==
            ~
          `entry
        ?~(matches ~ `i.matches)
      ?^  duplicate
        :_  this
        %^  api-json
          eyre-id
          202
        %-  pairs:enjs:format
        ~[['ok' b+%.y] ['request' s+(scot %uv -.u.duplicate)] ['deduplicated' b+%.y]]
      =/  request=@uv
        `@uv`(shas %git-peer-issue (cat 3 eny.bowl request-count))
      =.  request-count  +(request-count)
      =.  peer-forges
        %+  ~(put by peer-forges)
          request
        [peer repository %issue 0 %.y %.n 'opening issue on repository owner' ~]
      :_  this
      %+  weld
        :~  %^  peer-card
              peer
              /peer/forge-issue/(scot %uv request)
            [%forge-create-issue request repository title body]
            [%pass /peer/forge-timeout/(scot %uv request) %arvo %b %wait (add now.bowl ~s30)]
        ==
      (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y] ['request' s+(scot %uv request)]]))
    ::
    ++  peer-activities-json
      ^-  json
      %-  peer-ui-activity-json
      [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
    ::
    ++  github-results-json
      ^-  json
      =/  entries=(list json)
        %+  turn  ~(tap by github-results)
        |=  entry=[@uv github-result]
        =/  job=@uv  -.entry
        =/  result=github-result  +.entry
        %-  pairs:enjs:format
        :~  ['job' s+(scot %uv job)]
            ['active' b+active.result]
            ['ok' b+ok.result]
            ['kind' s+kind.result]
            ['repository' s+repository.result]
            ['message' s+message.result]
        ==
      %-  pairs:enjs:format
      :~  ['tokenSet' b+?=(^ github-token)]
          ['jobs' [%a entries]]
      ==
    ::
    ++  github-start
      |=  [ctx=github-request request=request:http]
      ^-  (quip card _this)
      =/  request-id=@uv
        `@uv`(shas %git-github-request (cat 3 eny.bowl request-count))
      =.  request-count  +(request-count)
      =.  ctx  ctx(job request-id)
      =.  github-in-flight  (~(put by github-in-flight) request-id ctx)
      =.  github-results
        (~(put by github-results) request-id [%.y %.n kind.ctx repository.ctx 'contacting GitHub'])
      :_  this
      :~  [%pass /github/(scot %uv request-id) %arvo %i %request request *outbound-config:iris]
      ==
    ::
    ++  dispatch-webhooks
      |=  [name=@t event=webhook-event:git data=json]
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) name)
      ?~  found  `this
      =/  hooks=(list [@ud webhook:git])
        %+  skim  ~(tap by webhooks.u.found)
        |=  entry=[@ud webhook:git]
        ?&  enabled.+.entry
            (~(has in events.+.entry) event)
        ==
      ?~  hooks  `this
      =/  payload=json
        %-  pairs:enjs:format
        :~  ['event' s+event]
            ['repository' s+name]
            ['owner' s+(scot %p owner.u.found)]
            ['sentAt' s+(scot %da now.bowl)]
            ['data' data]
        ==
      =/  body=octs  (json-to-octs:server payload)
      =/  remaining=(list [@ud webhook:git])  hooks
      =/  cards=(list card)  ~
      =/  flights=(map @uv webhook-flight)  webhook-in-flight
      =/  deliveries=(list webhook-delivery:git)  ~
      =/  count=@ud  request-count
      =/  result
        ^-  $:  cards=(list card)
                flights=(map @uv webhook-flight)
                deliveries=(list webhook-delivery:git)
                count=@ud
            ==
        |-
        ?~  remaining  [cards flights deliveries count]
        =/  hook=webhook:git  +.i.remaining
        =/  delivery-id=@uv
          `@uv`(shas %git-webhook (cat 3 eny.bowl count))
        =/  signature=@t  (signature:git-webhook secret.hook body)
        =/  headers=(list [@t @t])
          :~  ['content-type' 'application/json']
              ['user-agent' 'urgit-webhook/1']
              ['x-git-event' event]
              ['x-git-delivery' (scot %uv delivery-id)]
              ['x-hub-signature-256' signature]
          ==
        =/  =card
          :*  %pass
              /webhook/(scot %uv delivery-id)
              %arvo
              %i
              %request
              [%'POST' url.hook headers `body]
              *outbound-config:iris
          ==
        =/  delivery=webhook-delivery:git
          [delivery-id id.hook event %pending 0 'delivery queued' now.bowl]
        %=  $
          remaining  t.remaining
          cards  [card cards]
          flights  (~(put by flights) delivery-id [name id.hook delivery-id])
          deliveries  [delivery deliveries]
          count  +(count)
        ==
      =.  request-count  count.result
      =.  webhook-in-flight  flights.result
      =/  updated=repository:git
        u.found(webhook-deliveries (scag 100 (weld deliveries.result webhook-deliveries.u.found)))
      =.  repositories  (~(put by repositories) name updated)
      [(flop cards.result) this]
    ::
    ++  repository-notification
      |=  [name=@t repo=repository:git event=notification-event:git thread=path message=@t]
      ^-  notification-result
      ?.  (~(has in notification-events.repo) event)  [~ ~]
      =/  id=@uv  `@uv`(end 7 (shas %urgit-notification eny.bowl))
      =/  rope=hark-rope:git  [~ ~ %urgit thread]
      =/  yarn=hark-yarn:git  [id rope now.bowl ~[message] /apps/urgit ~]
      =/  =card
        :*  %pass
            /hark/(scot %uv id)
            %agent
            [our.bowl %hark]
            %poke
            %hark-action
            !>(`hark-action:git`[%add-yarn & & yarn])
        ==
      [[card ~] `[id event name message now.bowl]]
    ::
    ++  accept-receive
      |=  $:  eyre-id=@ta
              name=@t
              commands=(list receive-command:git)
              applied=repository:git
              clay=(unit [desk-name=desk commit=oid:git])
          ==
      ^-  (quip card _this)
      =.  repositories  (~(put by repositories) name applied)
      =^  push-cards  this
        (dispatch-webhooks name %push (push-event-json commands))
      ?~  clay
        :_  this
        %+  weld  push-cards
        %+  give-simple-payload:app:server  eyre-id
        (receive-payload 'ok' (receive-results commands %.y ''))
      =/  data=json
        %-  pairs:enjs:format
        ~[['desk' s+desk-name.u.clay] ['commit' s+(oid-text:git-codec commit.u.clay)]]
      =^  sync-cards  this
        (dispatch-webhooks name %clay-sync data)
      :_  this
      %+  weld  (weld push-cards sync-cards)
      %+  give-simple-payload:app:server  eyre-id
      (receive-payload 'ok' (receive-results commands %.y ''))
    ::
    ++  handle-incoming-hook
      |=  [eyre-id=@ta req=inbound-request:eyre name=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (api-error eyre-id 405 'webhook endpoint requires POST')
      =/  found=(unit repository:git)  (~(get by repositories) name)
      ?~  found
        :_  this
        (api-error eyre-id 404 'webhook endpoint not found')
      ?~  incoming-hook.u.found
        :_  this
        (api-error eyre-id 404 'webhook endpoint not found')
      ?.  enabled.u.incoming-hook.u.found
        :_  this
        (api-error eyre-id 404 'webhook endpoint not found')
      ?~  body.request.req
        :_  this
        (api-error eyre-id 400 'webhook body is required')
      =/  raw=@  q.u.body.request.req
      =/  body=octs  [(met 3 raw) raw]
      ?:  (gth p.body 1.048.576)
        :_  this
        (api-error eyre-id 413 'webhook body exceeds 1 MiB')
      =/  supplied=(unit @t)
        (get-header:http 'x-hub-signature-256' header-list.request.req)
      ?.  ?&  ?=(^ supplied)
              (verify:git-webhook secret.u.incoming-hook.u.found body u.supplied)
          ==
        :_  this
        (api-error eyre-id 401 'webhook signature is invalid')
      |^
        apply-hook
      ++  apply-hook
        =/  event=(unit @t)  (get-header:http 'x-github-event' header-list.request.req)
        ?:  &(?=(^ event) =('ping' u.event))
          :_  this
          (api-ok eyre-id 200)
        ?:  &(?=(^ event) =('pull_request' u.event))
          ?~  github-origin.u.found
            :_  this
            %^  api-json
              eyre-id
              202
            %-  pairs:enjs:format
            :~  ['ok' b+%.y]
                ['message' s+'pull request event accepted; repository has no GitHub origin']
            ==
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  ctx=github-request  [0v0 %pulls name owner remote public-read.u.found '' ~ 1 ~ 0]
          =/  =request:http
            :*  %'GET'
                (api-url:git-github owner remote '/pulls?state=all&per_page=100&page=1')
                (api-headers:git-github github-token)
                ~
            ==
          =/  result  (github-start ctx request)
          :_  +.result
          %+  weld
            -.result
          %^  api-json
            eyre-id
            202
          (pairs:enjs:format ~[['ok' b+%.y] ['message' s+'pull request metadata refresh started']])
        ?.  &(?=(^ event) =('push' u.event))
          :_  this
          (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y] ['message' s+'event ignored']]))
        =/  jon=(unit json)  (de:json:html q.body)
        ?~  jon
          :_  this
          (api-error eyre-id 400 'webhook body is not valid JSON')
        =/  notice=(unit push-notice:git-webhook)  (github-push:git-webhook u.jon)
        ?~  notice
          :_  this
          (api-error eyre-id 422 'push webhook is missing ref, before, or after')
        =/  update-id=@uv
          `@uv`(shas %git-upstream-update (cat 3 eny.bowl request-count))
        =.  request-count  +(request-count)
        =/  update=upstream-update:git
          [update-id source.u.notice ref.u.notice before.u.notice after.u.notice now.bowl]
        =/  remaining=(list upstream-update:git)
          (skim upstream-updates.u.found |=(prior=upstream-update:git !=(ref.prior ref.update)))
        =/  updated=repository:git
          u.found(upstream-updates (scag 50 (weld ~[update] remaining)))
        =.  repositories  (~(put by repositories) name updated)
        :_  this
        (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y] ['update' s+(scot %uv update-id)]]))
      --
    ::
    ++  handle-public-api
      |=  [eyre-id=@ta req=inbound-request:eyre line=request-line:server]
      ^-  (quip card _this)
      =/  site=(list @t)  site.line
      =/  method=@tas  method.request.req
      ?.  =(%'GET' method)
        :_  this
        (api-error eyre-id 405 'public repository API is read-only')
      |^
        ?+  (slag 4 site)
          [(api-error eyre-id 404 'public repository route not found') this]
          [%profile ~]  get-public-profile
          [%repository @ ~]  get-public-repository
          [%repository @ %issues @ ~]  get-public-repository-issues
          [%repository @ %releases ~]  get-public-repository-releases
          [%repository @ %archive ~]  get-public-repository-archive
          [%repository @ %files ~]  get-public-repository-files
          [%repository @ %search ~]  get-public-repository-search
          [%repository @ %commits ~]  get-public-repository-commits
          [%repository @ %compare ~]  get-public-repository-compare
          [%repository @ %commit @ ~]  get-public-repository-commit
          [%repository @ %file-history *]  get-public-repository-file-history
          [%repository @ %file-blame *]  get-public-repository-file-blame
          [%repository @ %file *]  get-public-repository-file
        ==
      ::
      ++  get-public-profile
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %profile ~] site)
        :_  this
        (api-json eyre-id 200 (public-profile-json our.bowl now.bowl repositories))
      ::
      ++  get-public-repository
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ ~] site)
        =/  name=@t  (api-terminal-name i.t.t.t.t.t.site ext.line)
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        :_  this
        (api-json eyre-id 200 (public-repository-json name u.found))
      ::
      ++  get-public-repository-issues
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %issues @ ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.t.site)
        ?~  number
          :_  this
          (api-error eyre-id 422 'issue number is invalid')
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  issue=(unit native-issue:git)  (native-issue-at u.found u.number)
        ?~  issue
          :_  this
          (api-error eyre-id 404 'issue not found')
        :_  this
        (api-json eyre-id 200 (native-issue-json u.issue %.y))
      ::
      ++  get-public-repository-releases
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %releases ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  tag=(unit @t)  (query-value 'tag' args.line)
        ?~  tag
          :_  this
          (api-error eyre-id 422 'tag is required')
        =/  release=(unit release:git)  (~(get by releases.u.found) u.tag)
        ?~  release
          :_  this
          (api-error eyre-id 404 'release not found')
        :_  this
        (api-json eyre-id 200 (release-json u.release %.y))
      ::
      ++  get-public-repository-archive
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %archive ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  ref=(unit @t)  (query-value 'ref' args.line)
        ?~  ref
          :_  this
          (api-error eyre-id 422 'ref is required')
        =/  target=(unit oid:git)  (revision-oid u.found u.ref)
        ?~  target
          :_  this
          (api-error eyre-id 404 'ref not found')
        =/  peeled=(unit oid:git)  (peeled-tag:git-protocol objects.u.found u.target)
        =/  commit=oid:git  ?~(peeled u.target u.peeled)
        =/  archive=(unit octs)  (archive:git-archive objects.u.found commit)
        ?~  archive
          :_  this
          %^  api-error
            eyre-id
            422
          'archive requires a complete commit tree of at most 10,000 files and 64 MiB'
        :_  this
        (api-archive eyre-id name u.archive)
      ::
      ++  get-public-repository-files
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %files ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  requested=(unit @t)  (query-value 'ref' args.line)
        =/  ref=@t  ?~(requested head.u.found u.requested)
        ?~  (revision-oid u.found ref)
          :_  this
          (api-error eyre-id 404 'ref not found')
        :_  this
        (api-json eyre-id 200 (repository-files-at-json name u.found ref))
      ::
      ++  get-public-repository-search
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %search ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  requested=(unit @t)  (query-value 'ref' args.line)
        =/  ref=@t  ?~(requested head.u.found u.requested)
        ?~  (revision-oid u.found ref)
          :_  this
          (api-error eyre-id 404 'ref not found')
        =/  query=(unit @t)  (query-value 'q' args.line)
        ?.  &(?=(^ query) (gte (met 3 u.query) 2) (lte (met 3 u.query) 200))
          :_  this
          (api-error eyre-id 422 'q must be between 2 and 200 bytes')
        :_  this
        (api-json eyre-id 200 (repository-search-json name u.found ref u.query))
      ::
      ++  get-public-repository-commits
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %commits ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  requested=(unit @t)  (query-value 'ref' args.line)
        =/  ref=@t  ?~(requested head.u.found u.requested)
        ?~  (revision-oid u.found ref)
          :_  this
          (api-error eyre-id 404 'ref not found')
        =/  offset-text=(unit @t)  (query-value 'offset' args.line)
        =/  offset=(unit @ud)  ?~(offset-text `0 (slaw %ud u.offset-text))
        =/  limit-text=(unit @t)  (query-value 'limit' args.line)
        =/  limit=(unit @ud)  ?~(limit-text `50 (slaw %ud u.limit-text))
        ?.  &(?=(^ offset) ?=(^ limit) (lte u.offset 10.000) (gth u.limit 0) (lte u.limit 50))
          :_  this
          (api-error eyre-id 422 'offset must be at most 10000 and limit must be between 1 and 50')
        :_  this
        %^  api-json
          eyre-id
          200
        (repository-history-json name u.found ref our.bowl now.bowl u.offset u.limit)
      ::
      ++  get-public-repository-compare
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %compare ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  base-ref=(unit @t)  (query-value 'base' args.line)
        =/  head-ref=(unit @t)  (query-value 'head' args.line)
        ?.  &(?=(^ base-ref) ?=(^ head-ref))
          :_  this
          (api-error eyre-id 422 'base and head refs are required')
        =/  base=(unit oid:git)  (revision-oid u.found u.base-ref)
        =/  head=(unit oid:git)  (revision-oid u.found u.head-ref)
        ?.  &(?=(^ base) ?=(^ head))
          :_  this
          (api-error eyre-id 404 'base or head ref not found')
        =/  diff=(unit json)  (repository-diff-json name u.found u.base u.head)
        ?~  diff
          :_  this
          (api-error eyre-id 422 'commits do not have readable trees')
        :_  this
        (api-json eyre-id 200 u.diff)
      ::
      ++  get-public-repository-commit
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %commit @ ~] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  oid-text=@t  i.t.t.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  detail=(unit json)
          (repository-history-detail-json name u.found oid-text our.bowl now.bowl)
        ?~  detail
          :_  this
          (api-error eyre-id 404 'commit not found')
        :_  this
        (api-json eyre-id 200 u.detail)
      ::
      ++  get-public-repository-file-history
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %file-history *] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.t.site ext.line)
        ?~  file-path
          :_  this
          (api-error eyre-id 422 'valid file path required')
        =/  requested=(unit @t)  (query-value 'ref' args.line)
        =/  ref=@t  ?~(requested head.u.found u.requested)
        ?~  (revision-oid u.found ref)
          :_  this
          (api-error eyre-id 404 'ref not found')
        :_  this
        %^  api-json
          eyre-id
          200
        (repository-file-history-view-json name u.found ref u.file-path our.bowl now.bowl)
      ::
      ++  get-public-repository-file-blame
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %file-blame *] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.t.site ext.line)
        ?~  file-path
          :_  this
          (api-error eyre-id 422 'valid file path required')
        =/  requested=(unit @t)  (query-value 'ref' args.line)
        =/  ref=@t  ?~(requested head.u.found u.requested)
        =/  blame=(unit json)
          (repository-file-blame-view-json name u.found ref u.file-path our.bowl now.bowl)
        ?~  blame
          :_  this
          (api-error eyre-id 422 'blame is available for text files up to 256 KiB and 10,000 lines')
        :_  this
        (api-json eyre-id 200 u.blame)
      ::
      ++  get-public-repository-file
        ^-  (quip card _this)
        ?>  ?=([%apps %urgit %api %public %repository @ %file *] site)
        =/  name=@t  i.t.t.t.t.t.site
        =/  found=(unit repository:git)  (~(get by repositories) name)
        ?.  &(?=(^ found) public-read.u.found)
          :_  this
          (api-error eyre-id 404 'public repository not found')
        =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.t.site ext.line)
        ?~  file-path
          :_  this
          (api-error eyre-id 422 'valid file path required')
        =/  requested=(unit @t)  (query-value 'ref' args.line)
        =/  ref=@t  ?~(requested head.u.found u.requested)
        =/  data=(unit octs)
          (repository-file-at-history u.found ref u.file-path our.bowl now.bowl)
        ?~  data
          :_  this
          (api-error eyre-id 404 'file not found')
        :_  this
        (api-json eyre-id 200 (repository-file-json name u.found ref u.file-path u.data))
      --
    ::
    ++  finish-file-edit
      |=  [eyre-id=@ta name=@t applied=repository:git branch-ref=@t commit=oid:git]
      ^-  (quip card _this)
      ?~  binding.applied
        =.  repositories  (~(put by repositories) name applied)
        :_  this
        %^  api-json
          eyre-id
          200
        (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec commit)]])
      ?.  =(branch-ref branch.u.binding.applied)
        =.  repositories  (~(put by repositories) name applied)
        :_  this
        %^  api-json
          eyre-id
          200
        (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec commit)]])
      ?:  |(=(^ pending-clay) =(^ pending-publish))
        :_  this
        (api-error eyre-id 409 'another Clay operation is in progress')
      =/  clay-files=(unit (map path octs))
        (flatten-commit:git-clay objects.applied commit)
      ?~  clay-files
        :_  this
        (api-error eyre-id 422 'commit cannot be projected onto the linked Clay desk')
      =/  delta=(unit nori:clay)
        (clay-delta our.bowl now.bowl desk-name.u.binding.applied u.clay-files)
      ?~  delta
        :_  this
        (api-error eyre-id 409 'unable to read linked Clay desk')
      ?>  ?=(%& -.u.delta)
      ?:  =(~ p.u.delta)
        =/  clay-revision=(unit @ud)
          %-  mole
          |.
          ud:.^(cass:clay %cw /(scot %p our.bowl)/[desk-name.u.binding.applied]/(scot %da now.bowl))
        =/  linked=repository:git
          (update-binding-success applied commit clay-revision now.bowl)
        =.  repositories  (~(put by repositories) name linked)
        :_  this
        %^  api-json
          eyre-id
          200
        (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec commit)]])
      =/  start-at=@da  (add now.bowl ~s1)
      =/  timeout-at=@da  (add now.bowl ~s15)
      =/  pending=clay-push
        :*  eyre-id
            %.y
            ~
            name
            ~
            applied
            desk-name.u.binding.applied
            branch.u.binding.applied
            commit
            u.delta
            ~
            start-at
            timeout-at
        ==
      =.  pending-clay  `pending
      :_  this
      :~  [%pass /clay-start %arvo %b %wait start-at]
          [%pass /clay-timeout %arvo %b %wait timeout-at]
      ==
    ::
    ++  handle-api
      |=  [eyre-id=@ta req=inbound-request:eyre line=request-line:server]
      ^-  (quip card _this)
      =/  site=(list @t)  site.line
      ?:  ?=([%apps %urgit %api %hooks @ ~] site)
        (handle-incoming-hook eyre-id req (api-terminal-name i.t.t.t.t.site ext.line))
      ?:  ?=([%apps %urgit %api %public *] site)
        (handle-public-api eyre-id req line)
      ?.  authenticated.req
        :_  this
        (api-error eyre-id 401 'Urbit login required')
      =/  method=@tas  method.request.req
      |^
        ?+  (slag 3 site)
          [(api-error eyre-id 404 'API route not found') this]
          [%github *]  github-api
          [%peer *]  peer-api
          [%repositories ~]  repositories-api
          [%desks ~]  desks-api
          [%repository @ *]  repository-api
        ==
      ::
      ++  repository-api
        ?+  (slag 5 site)
          ?:  =(method %'GET')  repository-view-api
          repository-settings-api
          [%github *]  repository-github-api
          [%issues *]  repository-issues-api
          [%pulls *]  repository-pulls-api
          [%lfs *]  repository-lfs-api
          [%clay *]  repository-clay-api
          [%file *]  repository-file-api
        ==
      ::
      ++  github-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %github %status ~]  get-github-status
            [%'POST' %github %token ~]  post-github-token
            [%'DELETE' %github %token ~]  delete-github-token
            [%'POST' %github %import ~]  post-github-import
          ==
        ::
        ++  get-github-status
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %github %status ~] site)
              ==
          :_  this
          (api-json eyre-id 200 github-results-json)
        ::
        ++  post-github-token
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %github %token ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  token=(unit @t)  (string-at 'token' u.jon)
          ?.  &(?=(^ token) !=('' u.token))
            :_  this
            (api-error eyre-id 422 'non-empty token is required')
          =.  github-token  `u.token
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  delete-github-token
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %github %token ~] site)
              ==
          =.  github-token  ~
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  post-github-import
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %github %import ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  owner=(unit @t)  (string-at 'owner' u.jon)
          =/  remote=(unit @t)  (string-at 'repository' u.jon)
          =/  local=(unit @t)  (string-at 'name' u.jon)
          =/  public=(unit ?)  (bool-at 'publicRead' u.jon)
          ?.  ?&  ?=(^ owner)  ?=(^ remote)  ?=(^ local)  ?=(^ public)
                  (valid-repository-name u.owner)
                  (valid-repository-name u.remote)
                  (valid-repository-name u.local)
              ==
            :_  this
            (api-error eyre-id 422 'owner, repository, name, and publicRead are required')
          =/  existing=(unit repository:git)  (~(get by repositories) u.local)
          =/  conflict=(unit @t)
            ?~  existing  ~
            ?~  github-origin.u.existing
              `'local repository already exists and is not linked to GitHub'
            ?.  ?&  =(u.owner owner.u.github-origin.u.existing)
                    =(u.remote repository.u.github-origin.u.existing)
                    ?=(~ binding.u.existing)
                ==
              `'GitHub origin does not match or repository is bound to Clay'
            ~
          ?^  conflict
            :_  this
            (api-error eyre-id 409 u.conflict)
          =/  kind=github-kind  ?^(existing %update %import)
          =/  ctx=github-request  [0v0 kind u.local u.owner u.remote u.public '' ~ 0 ~ 0]
          =/  =request:http
            :*  %'GET'
                (git-url:git-github u.owner u.remote '/info/refs?service=git-upload-pack')
                (git-headers:git-github github-token ~)
                ~
            ==
          =/  [cards=(list card) next=_this]  (github-start ctx request)
          :_  next
          %+  weld
            cards
          %^  api-json
            eyre-id
            202
          (pairs:enjs:format ~[['ok' b+%.y] ['message' s+'GitHub import started']])
        --
      ::
      ++  repository-github-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'POST' %repository @ %github %metadata ~]  post-repository-github-metadata
            [%'GET' %repository @ %github %issues @ ~]  get-repository-github-issues
            [%'GET' %repository @ %github %pulls @ ~]  get-repository-github-pulls
            [%'GET' %repository @ %github %pulls @ %diff ~]  get-repository-github-pulls-diff
            [%'GET' %repository @ %github %file *]  get-repository-github-file
            [%'POST' %repository @ %github %push ~]  post-repository-github-push
            [%'POST' %repository @ %github %fork ~]  post-repository-github-fork
            [%'POST' %repository @ %github %pull ~]  post-repository-github-pull
          ==
        ::
        ++  post-repository-github-metadata
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %github %metadata ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  requested=(unit @t)  (string-at 'kind' u.jon)
          =/  requested-page=(unit @ud)  (nat-at 'page' u.jon)
          ?.  ?&  ?=(^ requested)
                  ?|  =('issues' u.requested)
                      =('pulls' u.requested)
                  ==
              ==
            :_  this
            (api-error eyre-id 422 'kind must be issues or pulls')
          =/  page=@ud  ?~(requested-page 1 u.requested-page)
          ?.  &((gte page 1) (lte page 5))
            :_  this
            (api-error eyre-id 422 'page must be between 1 and 5')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  kind=github-kind  ?:(=('issues' u.requested) %issues %pulls)
          =/  ctx=github-request  [0v0 kind name owner remote public-read.u.found '' ~ page ~ 0]
          =/  suffix=@t
            %+  rap  3
            :~  ?:
                  =(%issues kind)
                  '/issues?state=all&per_page=100&page='
                '/pulls?state=all&per_page=100&page='
                (decimal page)
            ==
          =/  =request:http
            :*  %'GET'
                (api-url:git-github owner remote suffix)
                (api-headers:git-github github-token)
                ~
            ==
          =/  result  (github-start ctx request)
          :_  +.result
          (weld -.result (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y]])))
        ::
        ++  get-repository-github-issues
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %github %issues @ ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          =/  raw-number=@t  i.t.t.t.t.t.t.t.site
          =/  number=(unit @ud)  (parse-decimal raw-number)
          ?.  &(?=(^ number) (gth u.number 0))
            :_  this
            (api-error eyre-id 422 'positive GitHub issue number required')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  suffix=@t  (rap 3 ~['/issues/' (decimal u.number)])
          =/  ctx=github-request
            [0v0 %issue-detail name owner remote public-read.u.found '' ~ 0 `eyre-id u.number]
          =/  =request:http
            :*  %'GET'
                (api-url:git-github owner remote suffix)
                (api-headers:git-github github-token)
                ~
            ==
          (github-start ctx request)
        ::
        ++  get-repository-github-pulls
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %github %pulls @ ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          =/  raw-number=@t  i.t.t.t.t.t.t.t.site
          =/  number=(unit @ud)  (parse-decimal raw-number)
          ?.  &(?=(^ number) (gth u.number 0))
            :_  this
            (api-error eyre-id 422 'positive GitHub pull-request number required')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  suffix=@t  (rap 3 ~['/pulls/' (decimal u.number)])
          =/  ctx=github-request
            [0v0 %pull-detail name owner remote public-read.u.found '' ~ 0 `eyre-id u.number]
          =/  =request:http
            :*  %'GET'
                (api-url:git-github owner remote suffix)
                (api-headers:git-github github-token)
                ~
            ==
          (github-start ctx request)
        ::
        ++  get-repository-github-pulls-diff
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %github %pulls @ %diff ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          =/  raw-number=@t  i.t.t.t.t.t.t.t.site
          =/  number=(unit @ud)  (parse-decimal raw-number)
          ?.  &(?=(^ number) (gth u.number 0))
            :_  this
            (api-error eyre-id 422 'positive GitHub pull-request number required')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  suffix=@t  (rap 3 ~['/pulls/' (decimal u.number)])
          =/  ctx=github-request
            [0v0 %pull-diff name owner remote public-read.u.found '' ~ 0 `eyre-id u.number]
          =/  =request:http
            :*  %'GET'
                (api-url:git-github owner remote suffix)
                (diff-headers:git-github github-token)
                ~
            ==
          (github-start ctx request)
        ::
        ++  get-repository-github-file
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %github %file *] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          =/  file-path=(unit path)
            (api-file-path t.t.t.t.t.t.t.site ext.line)
          ?~  file-path
            :_  this
            (api-error eyre-id 422 'valid file path required')
          =/  path-text=@t  (crip (slag 1 (trip (spat u.file-path))))
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t
            ?^  requested  u.requested
            ?:  (starts-with 'refs/heads/' head.u.found)
              (crip (slag 11 (trip head.u.found)))
            head.u.found
          ?.  ?&  !=('' ref)
                  (lte (met 3 ref) 500)
              ==
            :_  this
            (api-error eyre-id 422 'valid GitHub ref required')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  suffix=@t
            %+  rap  3
            :~  '/contents/'
                (uri-encode:git-storage path-text)
                '?ref='
                (uri-encode:git-storage ref)
            ==
          =/  ctx=github-request
            [0v0 %file-detail name owner remote public-read.u.found path-text ~ 0 `eyre-id 0]
          =/  =request:http
            :*  %'GET'
                (api-url:git-github owner remote suffix)
                (api-headers:git-github github-token)
                ~
            ==
          (github-start ctx request)
        ::
        ++  post-repository-github-push
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %github %push ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          ?~  github-token
            :_  this
            (api-error eyre-id 409 'connect a GitHub token first')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  branch=(unit @t)  (string-at 'branch' u.jon)
          ?~  branch
            :_  this
            (api-error eyre-id 422 'branch is required')
          ?.  ?&  (valid-ref:git-protocol u.branch)
                  (starts-with:git-protocol (text:git-codec u.branch) 'refs/heads/')
              ==
            :_  this
            (api-error eyre-id 422 'branch must be a valid refs/heads ref')
          ?.  (~(has by refs.u.found) u.branch)
            :_  this
            (api-error eyre-id 404 'local branch not found')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  ctx=github-request  [0v0 %push name owner remote public-read.u.found u.branch ~ 0 ~ 0]
          =/  =request:http
            :*  %'GET'
                (git-url:git-github owner remote '/info/refs?service=git-receive-pack')
                (receive-headers:git-github github-token ~)
                ~
            ==
          =/  result  (github-start ctx request)
          :_  +.result
          (weld -.result (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y]])))
        ::
        ++  post-repository-github-fork
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %github %fork ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          ?~  github-token
            :_  this
            (api-error eyre-id 409 'connect a GitHub token first')
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  ctx=github-request  [0v0 %fork name owner remote public-read.u.found '' ~ 0 ~ 0]
          =/  headers=(list [@t @t])
            [['content-type' 'application/json'] (api-headers:git-github github-token)]
          =/  =request:http
            [%'POST' (api-url:git-github owner remote '/forks') headers `(as-octs:mimes:html '{}')]
          =/  result  (github-start ctx request)
          :_  +.result
          (weld -.result (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y]])))
        ::
        ++  post-repository-github-pull
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %github %pull ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  github-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository has no GitHub origin')
          ?~  github-token
            :_  this
            (api-error eyre-id 409 'connect a GitHub token first')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  title=(unit @t)  (string-at 'title' u.jon)
          =/  head=(unit @t)  (string-at 'head' u.jon)
          =/  base=(unit @t)  (string-at 'base' u.jon)
          =/  description=(unit @t)  (string-at 'body' u.jon)
          ?.  ?&  ?=(^ title)  ?=(^ head)  ?=(^ base)
                  !=('' u.title)  !=('' u.head)  !=('' u.base)
              ==
            :_  this
            (api-error eyre-id 422 'title, head, and base are required')
          =/  payload=json
            %-  pairs:enjs:format
            :~  ['title' s+u.title]
                ['head' s+u.head]
                ['base' s+u.base]
                ['body' s+?~(description '' u.description)]
            ==
          =/  owner=@t  owner.u.github-origin.u.found
          =/  remote=@t  repository.u.github-origin.u.found
          =/  ctx=github-request  [0v0 %open-pull name owner remote public-read.u.found '' ~ 0 ~ 0]
          =/  headers=(list [@t @t])
            [['content-type' 'application/json'] (api-headers:git-github github-token)]
          =/  =request:http
            :*  %'POST'
                (api-url:git-github owner remote '/pulls')
                headers
                `(as-octs:mimes:html (en:json:html payload))
            ==
          =/  result  (github-start ctx request)
          :_  +.result
          (weld -.result (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y]])))
        --
      ::
      ++  peer-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %peer %activity ~]  get-peer-activity
            [%'DELETE' %peer %activity ~]  delete-peer-activity
            [%'GET' %peer %peers ~]  get-peer-peers
            [%'POST' %peer %peers ~]  post-peer-peers
            [%'DELETE' %peer %peers ~]  delete-peer-peers
            [%'GET' %peer %browses ~]  get-peer-browses
            [%'DELETE' %peer %browses ~]  delete-peer-browses
            [%'POST' %peer %browse @ @ ~]  post-peer-browse
            [%'POST' %peer %stamp @ @ ~]  post-peer-stamp
            [%'POST' %peer %detail ~]  post-peer-detail
            [%'POST' %peer %file @ @ *]  post-peer-file
            [%'POST' %peer %commit ~]  post-peer-commit
            [%'GET' %peer %forge ~]  get-peer-forge
            [%'DELETE' %peer %forge ~]  delete-peer-forge
            [%'POST' %peer %issues ~]  post-peer-issues
            [%'POST' %peer %forge ~]  post-peer-forge
            [%'GET' %peer %discoveries ~]  get-peer-discoveries
            [%'POST' %peer %discover ~]  post-peer-discover
            [%'POST' %peer %discover-group ~]  post-peer-discover-group
            [%'DELETE' %peer %discoveries ~]  delete-peer-discoveries
            [%'GET' %peer %transfers ~]  get-peer-transfers
            [%'DELETE' %peer %transfers ~]  delete-peer-transfers
            [%'POST' %peer %fork ~]  post-peer-fork
            [%'POST' %peer %push ~]  post-peer-push
            [%'POST' %peer %pull-request ~]  post-peer-pull-request
          ==
        ::
        ++  get-peer-activity
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %peer %activity ~] site)
              ==
          :_  this
          (api-json eyre-id 200 peer-activities-json)
        ::
        ++  delete-peer-activity
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %peer %activity ~] site)
              ==
          =.  peer-activities  ~
          =.  notification-activities  ~
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]]))
        ::
        ++  get-peer-peers
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %peer %peers ~] site)
              ==
          :_  this
          (api-json eyre-id 200 peers-json)
        ::
        ++  post-peer-peers
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %peers ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          ?~  ship-text
            :_  this
            (api-error eyre-id 422 'ship is required')
          =/  peer=(unit @p)  (slaw %p u.ship-text)
          ?~  peer
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          (api-with-action eyre-id 200 [%add-peer u.peer])
        ::
        ++  delete-peer-peers
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %peer %peers ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          ?~  ship-text
            :_  this
            (api-error eyre-id 422 'ship is required')
          =/  peer=(unit @p)  (slaw %p u.ship-text)
          ?~  peer
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          (api-with-action eyre-id 200 [%remove-peer u.peer])
        ::
        ++  get-peer-browses
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %peer %browses ~] site)
              ==
          :_  this
          (api-json eyre-id 200 peer-browses-json)
        ::
        ++  delete-peer-browses
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %peer %browses ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  request-text=(unit @t)  (string-at 'request' u.jon)
          ?~  request-text
            :_  this
            (api-error eyre-id 422 'request is required')
          =/  request=(unit @uv)  (slaw %uv u.request-text)
          ?~  request
            :_  this
            (api-error eyre-id 422 'invalid browse request')
          =/  found=(unit peer-browse)  (~(get by peer-browses) u.request)
          ?~  found
            :_  this
            (api-error eyre-id 404 'browse request not found')
          =.  peer-browses  (~(del by peer-browses) u.request)
          :_  this
          =/  response=(list card)
            (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]]))
          =/  release-cards=(list card)
            ?:  active.u.found
              :~  :*  %pass
                      /peer/browse-release/(scot %uv u.request)
                      %agent
                      [peer.u.found %urgit]
                      %poke
                      %git-peer
                      !>([%browse-release u.request])
                  ==
              ==
            ~
          ?.  =(%fine phase.u.found)  (weld release-cards response)
          =/  cancel-cards=(list card)
            (peer-browse-yawns u.request peer.u.found expected.u.found)
          (weld cancel-cards (weld release-cards response))
        ::
        ++  post-peer-browse
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %browse @ @ ~] site)
              ==
          =/  ship-text=@t  i.t.t.t.t.t.site
          =/  repository=@t  (api-terminal-name i.t.t.t.t.t.t.site ext.line)
          =/  peer=(unit @p)  (slaw %p ship-text)
          ?.  &(?=(^ peer) (valid-repository-name repository))
            :_  this
            (api-error eyre-id 422 'valid ship and repository are required')
          (start-peer-browse eyre-id u.peer repository %overview 0 ~)
        ::
        ++  post-peer-stamp
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %stamp @ @ ~] site)
              ==
          =/  ship-text=@t  i.t.t.t.t.t.site
          =/  repository=@t  (api-terminal-name i.t.t.t.t.t.t.site ext.line)
          =/  peer=(unit @p)  (slaw %p ship-text)
          ?.  &(?=(^ peer) (valid-repository-name repository))
            :_  this
            (api-error eyre-id 422 'valid ship and repository are required')
          (start-peer-browse eyre-id u.peer repository %stamp 0 ~)
        ::
        ++  post-peer-detail
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %detail ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  repository=(unit @t)  (string-at 'repository' u.jon)
          =/  kind-text=(unit @t)  (string-at 'kind' u.jon)
          =/  number=(unit @ud)  (nat-at 'number' u.jon)
          ?.  ?&  ?=(^ ship-text)
                  ?=(^ repository)
                  ?=(^ kind-text)
                  ?=(^ number)
                  (gth u.number 0)
                  (valid-repository-name u.repository)
              ==
            :_  this
            (api-error eyre-id 422 'ship, repository, kind, and positive number are required')
          =/  peer=(unit @p)  (slaw %p u.ship-text)
          =/  view=(unit browse-view:git-peer)
            ?:  =('issue' u.kind-text)  `%issue
            ?:  =('pull' u.kind-text)  `%pull
            ~
          ?.  &(?=(^ peer) ?=(^ view))
            :_  this
            (api-error eyre-id 422 'ship and kind must be valid')
          (start-peer-browse eyre-id u.peer u.repository u.view u.number ~)
        ::
        ++  post-peer-file
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %file @ @ *] site)
              ==
          =/  ship-text=@t  i.t.t.t.t.t.site
          =/  repository=@t  i.t.t.t.t.t.t.site
          =/  peer=(unit @p)  (slaw %p ship-text)
          =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.t.site ext.line)
          ?.  ?&  ?=(^ peer)
                  (valid-repository-name repository)
                  ?=(^ file-path)
                  !=(~ u.file-path)
              ==
            :_  this
            (api-error eyre-id 422 'valid ship, repository, and file path are required')
          (start-peer-browse eyre-id u.peer repository %file 0 u.file-path)
        ::
        ++  post-peer-commit
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %commit ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  repository=(unit @t)  (string-at 'repository' u.jon)
          =/  identifier=(unit @t)  (string-at 'oid' u.jon)
          ?.  ?&  ?=(^ ship-text)
                  ?=(^ repository)
                  ?=(^ identifier)
                  (valid-repository-name u.repository)
                  (lte (met 3 u.identifier) 128)
              ==
            :_  this
            (api-error eyre-id 422 'ship, repository, and commit are required')
          =/  peer=(unit @p)  (slaw %p u.ship-text)
          ?~  peer
            :_  this
            (api-error eyre-id 422 'ship must be valid')
          (start-peer-browse eyre-id u.peer u.repository %commit 0 [u.identifier ~])
        ::
        ++  get-peer-forge
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %peer %forge ~] site)
              ==
          :_  this
          (api-json eyre-id 200 peer-forges-json)
        ::
        ++  delete-peer-forge
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %peer %forge ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  request-text=(unit @t)  (string-at 'request' u.jon)
          =/  request=(unit @uv)  ?~(request-text ~ (slaw %uv u.request-text))
          ?.  &(?=(^ request) (~(has by peer-forges) u.request))
            :_  this
            (api-error eyre-id 404 'forge request not found')
          =.  peer-forges  (~(del by peer-forges) u.request)
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]]))
        ::
        ++  post-peer-issues
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %issues ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  repository=(unit @t)  (string-at 'repository' u.jon)
          =/  title=(unit @t)  (string-at 'title' u.jon)
          =/  body=(unit @t)  (string-at 'body' u.jon)
          ?.  ?&  ?=(^ ship-text)
                  ?=(^ repository)
                  ?=(^ title)
                  ?=(^ body)
                  !=('' u.title)
                  (lte (met 3 u.title) 200)
                  (lte (met 3 u.body) 65.536)
                  (valid-repository-name u.repository)
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'ship, repository, and a title up to 200 bytes are required; body is limited to 64 KiB'
          =/  peer=(unit @p)  (slaw %p u.ship-text)
          ?~  peer
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          (start-peer-forge-issue eyre-id u.peer u.repository u.title u.body)
        ::
        ++  post-peer-forge
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %forge ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  repository=(unit @t)  (string-at 'repository' u.jon)
          =/  kind-text=(unit @t)  (string-at 'kind' u.jon)
          =/  number=(unit @ud)  (nat-at 'number' u.jon)
          =/  body=(unit @t)  (string-at 'body' u.jon)
          ?.  ?&  ?=(^ ship-text)
                  ?=(^ repository)
                  ?=(^ kind-text)
                  ?=(^ number)
                  ?=(^ body)
                  (gth u.number 0)
                  !=('' u.body)
                  (lte (met 3 u.body) 16.384)
                  (valid-repository-name u.repository)
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'ship, repository, kind, positive number, and a comment up to 16 KiB are required'
          =/  peer=(unit @p)  (slaw %p u.ship-text)
          =/  kind=(unit forge-kind:git-peer)
            ?:  =('issue' u.kind-text)  `%issue
            ?:  =('pull' u.kind-text)  `%pull
            ~
          ?.  &(?=(^ peer) ?=(^ kind))
            :_  this
            (api-error eyre-id 422 'ship and kind must be valid')
          (start-peer-forge-comment eyre-id u.peer u.repository u.kind u.number u.body)
        ::
        ++  get-peer-discoveries
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %peer %discoveries ~] site)
              ==
          :_  this
          (api-json eyre-id 200 peer-discoveries-json)
        ::
        ++  post-peer-discover
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %discover ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          ?~  ship-text
            :_  this
            (api-error eyre-id 422 'ship is required')
          =/  source=(unit @p)  (slaw %p u.ship-text)
          ?~  source
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          =/  duplicate=(unit [@uv peer-discovery])
            =/  matches=(list [@uv peer-discovery])
              %+  murn  ~(tap by peer-discoveries)
              |=  entry=[@uv peer-discovery]
              =/  discovery=peer-discovery  +.entry
              ?.  &(active.discovery =(u.source peer.discovery))  ~
              `entry
            ?~(matches ~ `i.matches)
          ?^  duplicate
            :_  this
            %^  api-json
              eyre-id
              202
            %-  pairs:enjs:format
            ~[['ok' b+%.y] ['request' s+(scot %uv -.u.duplicate)] ['deduplicated' b+%.y]]
          =/  request=@uv
            `@uv`(shas %git-peer-discovery (cat 3 eny.bowl request-count))
          =.  request-count  +(request-count)
          ::  a request to this ship still unacked and not yet an hour old: record
          ::  it as pending, ask nothing
          =/  decision  (plan:git-catalog peer-inflight ~ u.source now.bowl)
          ?:  ?=(%hold -.decision)
            =.  peer-discoveries
              (~(put by peer-discoveries) request (held:git-catalog u.source ~ since.decision))
            :_  this
            %^  api-json
              eyre-id
              202
            (pairs:enjs:format ~[['ok' b+%.y] ['request' s+(scot %uv request)]])
          =.  peer-discoveries
            (~(put by peer-discoveries) request (waiting:git-catalog u.source ~))
          =.  peer-inflight  (sent:git-catalog peer-inflight u.source request now.bowl)
          :_  this
          %+  weld
            :~  %^  peer-card
                  u.source
                  /peer/catalog-request/(scot %uv request)
                [%catalog-request request]
                :*  %pass
                    /peer/discovery-timeout/(scot %uv request)
                    %arvo
                    %b
                    %wait
                    (add now.bowl ~s30)
                ==
            ==
          %^  api-json
            eyre-id
            202
          (pairs:enjs:format ~[['ok' b+%.y] ['request' s+(scot %uv request)]])
        ::
        ++  post-peer-discover-group
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %discover-group ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  group-text=(unit @t)  (string-at 'group' u.jon)
          ?~  group-text
            :_  this
            (api-error eyre-id 422 'group is required')
          =/  group=(unit [host=@p name=@tas])  (parse-group-flag u.group-text)
          ?~  group
            :_  this
            (api-error eyre-id 422 'group must be a Groups flag, ~host/name')
          =/  members=(unit (set ship))  (group-members u.group)
          ?~  members
            :_  this
            (api-error eyre-id 422 'this ship is not a member of that group')
          =/  others=(list ship)  (sort ~(tap in (~(del in u.members) our.bowl)) lth)
          =/  capped=?  (gth (lent others) 200)
          =/  targets=(list ship)  (scag 200 others)
          =/  active-for=(map ship @uv)
            %-  ~(rep by peer-discoveries)
            |=  [entry=[request=@uv discovery=peer-discovery] acc=(map ship @uv)]
            ?.  &(active.discovery.entry =(group.discovery.entry group))  acc
            (~(put by acc) peer.discovery.entry request.entry)
          =|  requests=(list @uv)
          =|  cards=(list card)
          |-
          ?^  targets
            =/  decision  (plan:git-catalog peer-inflight active-for i.targets now.bowl)
            ?:  ?=(%reuse -.decision)
              $(targets t.targets, requests [request.decision requests])
            =/  request=@uv
              `@uv`(shas %git-peer-discovery (cat 3 eny.bowl request-count))
            =.  request-count  +(request-count)
            ?:  ?=(%hold -.decision)
              =.  peer-discoveries
                %+  ~(put by peer-discoveries)
                  request
                (held:git-catalog i.targets group since.decision)
              $(targets t.targets, requests [request requests])
            =.  peer-discoveries
              (~(put by peer-discoveries) request (waiting:git-catalog i.targets group))
            =.  peer-inflight  (sent:git-catalog peer-inflight i.targets request now.bowl)
            =/  ask=card
              %^  peer-card
                i.targets
                /peer/catalog-request/(scot %uv request)
              [%catalog-request request]
            =/  timeout=card
              [%pass /peer/discovery-timeout/(scot %uv request) %arvo %b %wait (add now.bowl ~s30)]
            %=  $
              targets  t.targets
              requests  [request requests]
              cards  [ask timeout cards]
            ==
          :_  this
          %+  weld  (flop cards)
          %^  api-json  eyre-id  202
          %-  pairs:enjs:format
          :~  ['ok' b+%.y]
              ['group' (group-flag-json group)]
              ['requests' [%a (turn (flop requests) |=(request=@uv s+(scot %uv request)))]]
              ['members' n+(decimal (lent others))]
              ['capped' b+capped]
          ==
        ::
        ++  delete-peer-discoveries
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %peer %discoveries ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  request-text=(unit @t)  (string-at 'request' u.jon)
          ?~  request-text
            :_  this
            (api-error eyre-id 422 'request is required')
          =/  request=(unit @uv)  (slaw %uv u.request-text)
          ?~  request
            :_  this
            (api-error eyre-id 422 'invalid discovery request')
          ?.  (~(has by peer-discoveries) u.request)
            :_  this
            (api-error eyre-id 404 'discovery request not found')
          =.  peer-discoveries  (~(del by peer-discoveries) u.request)
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]]))
        ::
        ++  get-peer-transfers
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %peer %transfers ~] site)
              ==
          :_  this
          (api-json eyre-id 200 peer-results-json)
        ::
        ++  delete-peer-transfers
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %peer %transfers ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  transfer-text=(unit @t)  (string-at 'transfer' u.jon)
          ?~  transfer-text
            :_  this
            (api-error eyre-id 422 'transfer is required')
          =/  transfer=(unit @uv)  (slaw %uv u.transfer-text)
          ?~  transfer
            :_  this
            (api-error eyre-id 422 'invalid transfer identifier')
          =/  active=?  (~(has by peer-receiving) u.transfer)
          =/  outgoing=?  (~(has by peer-outgoing) u.transfer)
          =/  recorded=?  (~(has by peer-results) u.transfer)
          ?.  |(active outgoing recorded)
            :_  this
            (api-error eyre-id 404 'transfer not found')
          ?:  outgoing
            :_  this
            (api-error eyre-id 409 'outgoing offer is still active')
          ?:  active
            =/  canceled=(quip card _this)
              (peer-snapshot-fail u.transfer 'transfer cancelled')
            :_  +.canceled
            (weld -.canceled (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]])))
          =.  peer-results  (~(del by peer-results) u.transfer)
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]]))
        ::
        ++  post-peer-fork
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %fork ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  source-repository=(unit @t)  (string-at 'repository' u.jon)
          =/  local-repository=(unit @t)  (string-at 'name' u.jon)
          =/  public=(unit ?)  (bool-at 'publicRead' u.jon)
          ?.  ?&  ?=(^ ship-text)
                  ?=(^ source-repository)
                  ?=(^ local-repository)
                  ?=(^ public)
                  (valid-repository-name u.source-repository)
                  (valid-repository-name u.local-repository)
              ==
            :_  this
            (api-error eyre-id 422 'ship, repository, name, and publicRead are required')
          =/  source=(unit @p)  (slaw %p u.ship-text)
          ?~  source
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          =/  duplicate=(unit [@uv peer-receive])
            =/  matches=(list [@uv peer-receive])
              %+  murn  ~(tap by peer-receiving)
              |=  entry=[@uv peer-receive]
              =/  flight=peer-receive  +.entry
              ?.  ?&  =(%fork purpose.flight)
                      =(u.source source.flight)
                      =(u.source-repository source-repository.flight)
                      =(u.local-repository local-repository.flight)
                  ==
                ~
              `entry
            ?~(matches ~ `i.matches)
          ?^  duplicate
            :_  this
            %^  api-json
              eyre-id
              202
            %-  pairs:enjs:format
            ~[['ok' b+%.y] ['transfer' s+(scot %uv -.u.duplicate)] ['deduplicated' b+%.y]]
          =/  existing=(unit repository:git)  (~(get by repositories) u.local-repository)
          =/  conflict=(unit @t)
            ?~  existing  ~
            ?~  peer-origin.u.existing
              `'repository already exists and is not a peer fork'
            ?.  ?&  =(u.source ship.u.peer-origin.u.existing)
                    =(u.source-repository repository.u.peer-origin.u.existing)
                    ?=(~ binding.u.existing)
                ==
              `'peer origin does not match or repository is bound to Clay'
            ~
          ?^  conflict
            :_  this
            (api-error eyre-id 409 u.conflict)
          |^
            start-fork
          ++  start-fork
            =/  raw-transfer=@uv
              `@uv`(shas %git-peer-transfer (cat 3 eny.bowl request-count))
            =/  transfer=@uv  (peer-object-transfer raw-transfer)
            =.  request-count  +(request-count)
            =/  base-objects=(map oid:git object:git)
              ?~(existing ~ objects.u.existing)
            =/  haves=(set oid:git)
              (silt (turn ~(tap by base-objects) |=(entry=[oid:git object:git] -.entry)))
            =/  flight=peer-receive
              :*  %fork
                  %pack
                  u.source
                  u.source-repository
                  u.local-repository
                  ''
                  ''
                  ''
                  ?^(existing public-read.u.existing u.public)
                  %.n
                  ''
                  ~
                  0
                  0
                  0
                  0
                  ~
                  ~
                  now.bowl
                  ~
                  ~
                  0
                  0
                  base-objects
              ==
            =.  peer-receiving  (~(put by peer-receiving) transfer flight)
            =.  peer-results
              %+  ~(put by peer-results)
                transfer
              [%.n 'transferring' u.local-repository]
            =.  peer-activities
              %:  peer-activity-start
                transfer
                %fork
                %outgoing
                u.source
                u.local-repository
                'transferring repository'
              ==
            :_  this
            %+  weld
              :~  %^  peer-card
                    u.source
                    /peer/request/(scot %uv transfer)
                  [%request transfer u.source-repository haves]
                  :*  %pass
                      /peer/request-timeout/(scot %uv transfer)
                      %arvo
                      %b
                      %wait
                      (add now.bowl ~s45)
                  ==
              ==
            %^  api-json
              eyre-id
              202
            (pairs:enjs:format ~[['ok' b+%.y] ['transfer' s+(scot %uv transfer)]])
          --
        ::
        ++  post-peer-push
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %push ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  name=(unit @t)  (string-at 'name' u.jon)
          ?~  name
            :_  this
            (api-error eyre-id 422 'name is required')
          =/  found=(unit repository:git)  (~(get by repositories) u.name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  peer-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository is not a native fork')
          =/  raw-transfer=@uv
            `@uv`(shas %git-peer-push (cat 3 eny.bowl request-count))
          =/  transfer=@uv  (peer-object-transfer raw-transfer)
          =.  request-count  +(request-count)
          =.  peer-results  (~(put by peer-results) transfer [%.n 'offering update' u.name])
          =.  peer-outgoing
            (~(put by peer-outgoing) transfer [ship.u.peer-origin.u.found u.name %push])
          =.  peer-activities
            %:  peer-activity-start
              transfer
              %push
              %outgoing
              ship.u.peer-origin.u.found
              u.name
              'offering update'
            ==
          :_  this
          %+  weld
            :~  %^  peer-card
                  ship.u.peer-origin.u.found
                  /peer/offer/(scot %uv transfer)
                [%offer transfer repository.u.peer-origin.u.found u.name %.n '']
                [%pass /peer/offer-timeout/(scot %uv transfer) %arvo %b %wait (add now.bowl ~m11)]
            ==
          %^  api-json
            eyre-id
            202
          (pairs:enjs:format ~[['ok' b+%.y] ['transfer' s+(scot %uv transfer)]])
        ::
        ++  post-peer-pull-request
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %peer %pull-request ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  name=(unit @t)  (string-at 'name' u.jon)
          =/  title=(unit @t)  (string-at 'title' u.jon)
          =/  source-ref=(unit @t)  (string-at 'sourceBranch' u.jon)
          =/  target-ref=(unit @t)  (string-at 'targetBranch' u.jon)
          ?.  ?&  ?=(^ name)
                  ?=(^ title)
                  ?=(^ source-ref)
                  ?=(^ target-ref)
                  !=('' u.title)
                  (lte (met 3 u.title) 200)
                  (starts-with 'refs/heads/' u.source-ref)
                  (valid-ref:git-protocol u.source-ref)
                  (starts-with 'refs/heads/' u.target-ref)
                  (valid-ref:git-protocol u.target-ref)
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'name, title, sourceBranch, and targetBranch are required; refs must be valid branches'
          =/  found=(unit repository:git)  (~(get by repositories) u.name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  source-head=(unit oid:git)  (~(get by refs.u.found) u.source-ref)
          ?~  source-head
            :_  this
            (api-error eyre-id 404 'source branch not found')
          ?~  peer-origin.u.found
            :_  this
            (api-error eyre-id 409 'repository is not a native fork')
          =/  raw-transfer=@uv
            `@uv`(shas %git-peer-pull (cat 3 eny.bowl request-count))
          =/  transfer=@uv  (peer-object-transfer raw-transfer)
          =.  request-count  +(request-count)
          =.  peer-results  (~(put by peer-results) transfer [%.n 'opening pull request' u.name])
          =.  peer-outgoing
            (~(put by peer-outgoing) transfer [ship.u.peer-origin.u.found u.name %pull-request])
          =.  peer-activities
            %:  peer-activity-start
              transfer
              %pull-request
              %outgoing
              ship.u.peer-origin.u.found
              u.name
              'opening pull request'
            ==
          :_  this
          %+  weld
            :~  %^  peer-card
                  ship.u.peer-origin.u.found
                  /peer/offer/(scot %uv transfer)
                :*  %offer-branches
                    transfer
                    repository.u.peer-origin.u.found
                    u.name
                    u.source-ref
                    u.target-ref
                    %.y
                    u.title
                ==
                [%pass /peer/offer-timeout/(scot %uv transfer) %arvo %b %wait (add now.bowl ~m11)]
            ==
          %^  api-json
            eyre-id
            202
          (pairs:enjs:format ~[['ok' b+%.y] ['transfer' s+(scot %uv transfer)]])
        --
      ::
      ++  repositories-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %repositories ~]  get-repositories
            [%'POST' %repositories ~]  post-repositories
          ==
        ::
        ++  get-repositories
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repositories ~] site)
              ==
          :_  this
          (api-json eyre-id 200 (repositories-json repositories))
        ::
        ++  post-repositories
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repositories ~] site)
              ==
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  name=(unit @t)  (string-at 'name' u.jon)
          =/  public=(unit ?)  (bool-at 'publicRead' u.jon)
          ?.  &(?=(^ name) ?=(^ public) (valid-repository-name u.name))
            :_  this
            %^  api-error
              eyre-id
              422
            'name and publicRead are required; name may contain letters, numbers, dot, dash, and underscore'
          ?:  (~(has by repositories) u.name)
            :_  this
            (api-error eyre-id 409 'repository already exists')
          (api-with-action eyre-id 201 [%create u.name u.public])
        --
      ::
      ++  desks-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %desks ~]  get-desks
          ==
        ::
        ++  get-desks
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %desks ~] site)
              ==
          =/  desks=(unit (set desk))
            %-  mole
            |.(.^((set desk) %cd /(scot %p our.bowl)//(scot %da now.bowl)))
          ?~  desks
            :_  this
            (api-error eyre-id 503 'unable to list Clay desks')
          =/  entries=(list json)
            %+  turn  ~(tap in u.desks)
            |=(desk-name=desk s+desk-name)
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['desks' [%a entries]]]))
        --
      ::
      ++  repository-view-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %repository @ ~]  get-repository
            [%'GET' %repository @ %files ~]  get-repository-files
            [%'GET' %repository @ %search ~]  get-repository-search
            [%'GET' %repository @ %commits ~]  get-repository-commits
            [%'GET' %repository @ %compare ~]  get-repository-compare
            [%'GET' %repository @ %commit @ ~]  get-repository-commit
            [%'GET' %repository @ %file-history *]  get-repository-file-history
            [%'GET' %repository @ %file-blame *]  get-repository-file-blame
            [%'GET' %repository @ %releases ~]  get-repository-releases
            [%'GET' %repository @ %archive ~]  get-repository-archive
          ==
        ::
        ++  get-repository
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ ~] site)
              ==
          =/  name=@t  (api-terminal-name i.t.t.t.t.site ext.line)
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          :_  this
          (api-json eyre-id 200 (repository-json name u.found))
        ::
        ++  get-repository-files
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %files ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t  ?~(requested head.u.found u.requested)
          ?~  (revision-oid u.found ref)
            :_  this
            (api-error eyre-id 404 'ref not found')
          :_  this
          (api-json eyre-id 200 (repository-files-at-json name u.found ref))
        ::
        ++  get-repository-search
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %search ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t  ?~(requested head.u.found u.requested)
          ?~  (revision-oid u.found ref)
            :_  this
            (api-error eyre-id 404 'ref not found')
          =/  query=(unit @t)  (query-value 'q' args.line)
          ?.  &(?=(^ query) (gte (met 3 u.query) 2) (lte (met 3 u.query) 200))
            :_  this
            (api-error eyre-id 422 'q must be between 2 and 200 bytes')
          :_  this
          (api-json eyre-id 200 (repository-search-json name u.found ref u.query))
        ::
        ++  get-repository-commits
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %commits ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t  ?~(requested head.u.found u.requested)
          ?~  (revision-oid u.found ref)
            :_  this
            (api-error eyre-id 404 'ref not found')
          =/  offset-text=(unit @t)  (query-value 'offset' args.line)
          =/  offset=(unit @ud)  ?~(offset-text `0 (slaw %ud u.offset-text))
          =/  limit-text=(unit @t)  (query-value 'limit' args.line)
          =/  limit=(unit @ud)  ?~(limit-text `50 (slaw %ud u.limit-text))
          ?.  &(?=(^ offset) ?=(^ limit) (lte u.offset 10.000) (gth u.limit 0) (lte u.limit 50))
            :_  this
            %^  api-error
              eyre-id
              422
            'offset must be at most 10000 and limit must be between 1 and 50'
          :_  this
          %^  api-json
            eyre-id
            200
          (repository-history-json name u.found ref our.bowl now.bowl u.offset u.limit)
        ::
        ++  get-repository-compare
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %compare ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  base-ref=(unit @t)  (query-value 'base' args.line)
          =/  head-ref=(unit @t)  (query-value 'head' args.line)
          ?.  &(?=(^ base-ref) ?=(^ head-ref))
            :_  this
            (api-error eyre-id 422 'base and head refs are required')
          =/  base=(unit oid:git)  (revision-oid u.found u.base-ref)
          =/  head=(unit oid:git)  (revision-oid u.found u.head-ref)
          ?.  &(?=(^ base) ?=(^ head))
            :_  this
            (api-error eyre-id 404 'base or head ref not found')
          =/  diff=(unit json)  (repository-diff-json name u.found u.base u.head)
          ?~  diff
            :_  this
            (api-error eyre-id 422 'commits do not have readable trees')
          :_  this
          (api-json eyre-id 200 u.diff)
        ::
        ++  get-repository-commit
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %commit @ ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  oid-text=@t  i.t.t.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  detail=(unit json)
            (repository-history-detail-json name u.found oid-text our.bowl now.bowl)
          ?~  detail
            :_  this
            (api-error eyre-id 404 'commit not found')
          :_  this
          (api-json eyre-id 200 u.detail)
        ::
        ++  get-repository-file-history
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %file-history *] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.site ext.line)
          ?~  file-path
            :_  this
            (api-error eyre-id 422 'valid file path required')
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t  ?~(requested head.u.found u.requested)
          ?~  (revision-oid u.found ref)
            :_  this
            (api-error eyre-id 404 'ref not found')
          :_  this
          %^  api-json
            eyre-id
            200
          (repository-file-history-view-json name u.found ref u.file-path our.bowl now.bowl)
        ::
        ++  get-repository-file-blame
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %file-blame *] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.site ext.line)
          ?~  file-path
            :_  this
            (api-error eyre-id 422 'valid file path required')
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t  ?~(requested head.u.found u.requested)
          =/  blame=(unit json)
            (repository-file-blame-view-json name u.found ref u.file-path our.bowl now.bowl)
          ?~  blame
            :_  this
            %^  api-error
              eyre-id
              422
            'blame is available for text files up to 256 KiB and 10,000 lines'
          :_  this
          (api-json eyre-id 200 u.blame)
        ::
        ++  get-repository-releases
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %releases ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  tag=(unit @t)  (query-value 'tag' args.line)
          ?~  tag
            :_  this
            (api-error eyre-id 422 'tag is required')
          =/  release=(unit release:git)  (~(get by releases.u.found) u.tag)
          ?~  release
            :_  this
            (api-error eyre-id 404 'release not found')
          :_  this
          (api-json eyre-id 200 (release-json u.release %.y))
        ::
        ++  get-repository-archive
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %archive ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  ref=(unit @t)  (query-value 'ref' args.line)
          ?~  ref
            :_  this
            (api-error eyre-id 422 'ref is required')
          =/  target=(unit oid:git)  (revision-oid u.found u.ref)
          ?~  target
            :_  this
            (api-error eyre-id 404 'ref not found')
          =/  peeled=(unit oid:git)  (peeled-tag:git-protocol objects.u.found u.target)
          =/  commit=oid:git  ?~(peeled u.target u.peeled)
          =/  archive=(unit octs)  (archive:git-archive objects.u.found commit)
          ?~  archive
            :_  this
            %^  api-error
              eyre-id
              422
            'archive requires a complete commit tree of at most 10,000 files and 64 MiB'
          :_  this
          (api-archive eyre-id name u.archive)
        --
      ::
      ++  repository-issues-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'POST' %repository @ %issues ~]  post-repository-issues
            [%'GET' %repository @ %issues @ ~]  get-repository-issues
            [%'POST' %repository @ %issues @ %comments ~]  post-repository-issues-comments
            [%'POST' %repository @ %issues @ %state ~]  post-repository-issues-state
            [%'POST' %repository @ %issues @ %labels ~]  post-repository-issues-labels
            [%'POST' %repository @ %issues @ %assignees ~]  post-repository-issues-assignees
          ==
        ::
        ++  post-repository-issues
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %issues ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  title=(unit @t)  (string-at 'title' u.jon)
          =/  body=(unit @t)  (string-at 'body' u.jon)
          ?.  ?&  ?=(^ title)
                  ?=(^ body)
                  !=('' u.title)
                  (lte (met 3 u.title) 200)
                  (lte (met 3 u.body) 65.536)
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'title is required and limited to 200 bytes; body is limited to 64 KiB'
          =/  number=@ud  (add 1 (lent native-issues.u.found))
          =/  issue=native-issue:git
            [number our.bowl u.title u.body %open ~ ~ now.bowl now.bowl ~]
          =.  repositories
            (~(put by repositories) name u.found(native-issues [issue native-issues.u.found]))
          =/  dispatched=(quip card _this)
            (dispatch-webhooks name %issue (native-issue-json issue %.y))
          :_  +.dispatched
          (weld -.dispatched (api-json eyre-id 201 (native-issue-json issue %.y)))
        ::
        ++  get-repository-issues
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %issues @ ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'issue number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  issue=(unit native-issue:git)  (native-issue-at u.found u.number)
          ?~  issue
            :_  this
            (api-error eyre-id 404 'issue not found')
          :_  this
          (api-json eyre-id 200 (native-issue-json u.issue %.y))
        ::
        ++  post-repository-issues-comments
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %issues @ %comments ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'issue number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  issue=(unit native-issue:git)  (native-issue-at u.found u.number)
          ?~  issue
            :_  this
            (api-error eyre-id 404 'issue not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  body=(unit @t)  (string-at 'body' u.jon)
          ?.  &(?=(^ body) !=('' u.body) (lte (met 3 u.body) 16.384))
            :_  this
            (api-error eyre-id 422 'comment body is required and limited to 16 KiB')
          =/  comment=issue-comment:git
            [(add 1 (lent comments.u.issue)) our.bowl u.body now.bowl]
          =/  issues=(list native-issue:git)
            %+  turn  native-issues.u.found
            |=  candidate=native-issue:git
            ?:  =(number.candidate u.number)
              candidate(comments (weld comments.candidate ~[comment]), updated now.bowl)
            candidate
          =.  repositories  (~(put by repositories) name u.found(native-issues issues))
          :_  this
          (api-json eyre-id 201 (issue-comment-json comment))
        ::
        ++  post-repository-issues-state
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %issues @ %state ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'issue number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  (native-issue-at u.found u.number)
            :_  this
            (api-error eyre-id 404 'issue not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  requested=(unit @t)  (string-at 'state' u.jon)
          ?.  &(?=(^ requested) |(=('open' u.requested) =('closed' u.requested)))
            :_  this
            (api-error eyre-id 422 'state must be open or closed')
          =/  next-state=?(%open %closed)  ?:(=('open' u.requested) %open %closed)
          =/  issues=(list native-issue:git)
            %+  turn  native-issues.u.found
            |=  candidate=native-issue:git
            ?:(=(number.candidate u.number) candidate(state next-state, updated now.bowl) candidate)
          =.  repositories  (~(put by repositories) name u.found(native-issues issues))
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y] ['state' s+next-state]]))
        ::
        ++  post-repository-issues-labels
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %issues @ %labels ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'issue number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  (native-issue-at u.found u.number)
            :_  this
            (api-error eyre-id 404 'issue not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  labels=(unit (list @t))  (string-list-at 'labels' u.jon)
          ?.  ?&  ?=(^ labels)
                  (lte (lent u.labels) 20)
                  (levy u.labels |=(label=@t &(!=('' label) (lte (met 3 label) 64))))
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'labels must be an array of at most 20 non-empty strings, each at most 64 bytes'
          =/  next-labels=(set @t)  (silt u.labels)
          =/  issues=(list native-issue:git)
            %+  turn  native-issues.u.found
            |=  candidate=native-issue:git
            ?:
              =(number.candidate u.number)
              candidate(labels next-labels, updated now.bowl)
            candidate
          =.  repositories  (~(put by repositories) name u.found(native-issues issues))
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  post-repository-issues-assignees
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %issues @ %assignees ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'issue number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  (native-issue-at u.found u.number)
            :_  this
            (api-error eyre-id 404 'issue not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  assignees=(unit (list @p))  (ship-list-at 'assignees' u.jon)
          ?.  &(?=(^ assignees) (lte (lent u.assignees) 20))
            :_  this
            (api-error eyre-id 422 'assignees must be an array of at most 20 valid ship names')
          =/  next-assignees=(set @p)  (silt u.assignees)
          =/  issues=(list native-issue:git)
            %+  turn  native-issues.u.found
            |=  candidate=native-issue:git
            ?:
              =(number.candidate u.number)
              candidate(assignees next-assignees, updated now.bowl)
            candidate
          =.  repositories  (~(put by repositories) name u.found(native-issues issues))
          :_  this
          (api-ok eyre-id 200)
        --
      ::
      ++  repository-clay-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %repository @ %clay %status ~]  get-repository-clay-status
            [%'POST' %repository @ %clay %apply ~]  post-repository-clay-apply
          ==
        ::
        ++  get-repository-clay-status
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %clay %status ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          :_  this
          (api-json eyre-id 200 (clay-bridge-status-json name u.found our.bowl now.bowl))
        ::
        ++  post-repository-clay-apply
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %clay %apply ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  binding.u.found
            :_  this
            (api-error eyre-id 409 'repository is not bound to a Clay desk')
          ?:  |(=(^ pending-clay) =(^ pending-publish))
            :_  this
            (api-error eyre-id 409 'another Clay operation is in progress')
          =/  head-oid=(unit oid:git)
            (~(get by refs.u.found) branch.u.binding.u.found)
          ?~  head-oid
            :_  this
            (api-error eyre-id 409 'linked branch has no head')
          =/  files=(unit (map path octs))
            (flatten-commit:git-clay objects.u.found u.head-oid)
          ?~  files
            :_  this
            (api-error eyre-id 422 'linked branch is not a valid desk-shaped Git commit')
          =/  delta=(unit nori:clay)
            (clay-delta our.bowl now.bowl desk-name.u.binding.u.found u.files)
          ?~  delta
            :_  this
            (api-error eyre-id 409 'unable to read linked Clay desk')
          ?>  ?=(%& -.u.delta)
          ?:  =(~ p.u.delta)
            =/  clay-revision=(unit @ud)
              %-  mole
              |.
              =/  clay-path=path
                /(scot %p our.bowl)/[desk-name.u.binding.u.found]/(scot %da now.bowl)
              ud:.^(cass:clay %cw clay-path)
            =/  linked=repository:git
              (update-binding-success u.found u.head-oid clay-revision now.bowl)
            =.  repositories  (~(put by repositories) name linked)
            :_  this
            %^  api-json
              eyre-id
              200
            (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec u.head-oid)]])
          =/  start-at=@da  (add now.bowl ~s1)
          =/  timeout-at=@da  (add now.bowl ~s15)
          =/  pending=clay-push
            :*  eyre-id
                %.y
                ~
                name
                ~
                u.found
                desk-name.u.binding.u.found
                branch.u.binding.u.found
                u.head-oid
                u.delta
                ~
                start-at
                timeout-at
            ==
          =.  pending-clay  `pending
          :_  this
          :~  [%pass /clay-start %arvo %b %wait start-at]
              [%pass /clay-timeout %arvo %b %wait timeout-at]
          ==
        --
      ::
      ++  repository-file-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %repository @ %file *]  get-repository-file
            [%'POST' %repository @ %file *]  post-repository-file
            [%'DELETE' %repository @ %file *]  delete-repository-file
          ==
        ::
        ++  get-repository-file
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %file *] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.site ext.line)
          ?~  file-path
            :_  this
            (api-error eyre-id 422 'valid file path required')
          =/  requested=(unit @t)  (query-value 'ref' args.line)
          =/  ref=@t  ?~(requested head.u.found u.requested)
          =/  data=(unit octs)
            (repository-file-at-history u.found ref u.file-path our.bowl now.bowl)
          ?~  data
            :_  this
            (api-error eyre-id 404 'file not found')
          :_  this
          (api-json eyre-id 200 (repository-file-json name u.found ref u.file-path u.data))
        ::
        ++  post-repository-file
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %file *] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.site ext.line)
          ?~  file-path
            :_  this
            (api-error eyre-id 422 'valid file path required')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  encoded=(unit @t)  (string-at 'content' u.jon)
          =/  message=(unit @t)  (string-at 'message' u.jon)
          =/  requested-ref=(unit @t)  (string-at 'ref' u.jon)
          ?.  &(?=(^ encoded) ?=(^ message) !=('' u.message))
            :_  this
            (api-error eyre-id 422 'base64 content and a non-empty commit message are required')
          =/  branch-ref=@t  ?~(requested-ref head.u.found u.requested-ref)
          ?.  ?&  (valid-ref:git-protocol branch-ref)
                  (starts-with 'refs/heads/' branch-ref)
              ==
            :_  this
            (api-error eyre-id 422 'ref must be a valid branch')
          =/  data=(unit octs)  (de:base64:mimes:html u.encoded)
          ?~  data
            :_  this
            (api-error eyre-id 422 'content is not valid base64')
          =/  parent=(unit oid:git)  (~(get by refs.u.found) branch-ref)
          ?:  &(?=(~ parent) !=(branch-ref head.u.found))
            :_  this
            (api-error eyre-id 404 'branch not found')
          =/  snapped=(unit [commit=oid:git objects=(map oid:git object:git)])
            ?~  parent
              %:  initial-commit:git-tree
                objects.u.found
                u.file-path
                u.data
                our.bowl
                now.bowl
                u.message
              ==
            %:  edit-commit:git-tree
              objects.u.found
              u.parent
              u.file-path
              u.data
              our.bowl
              now.bowl
              u.message
            ==
          ?~  snapped
            :_  this
            (api-error eyre-id 422 'file path conflicts with the tree or branch head is invalid')
          =/  applied=repository:git
            %=  u.found
              objects  objects.u.snapped
              refs  (~(put by refs.u.found) branch-ref commit.u.snapped)
            ==
          (finish-file-edit eyre-id name applied branch-ref commit.u.snapped)
        ::
        ++  delete-repository-file
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %file *] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  file-path=(unit path)  (api-file-path t.t.t.t.t.t.site ext.line)
          ?~  file-path
            :_  this
            (api-error eyre-id 422 'valid file path required')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  message=(unit @t)  (string-at 'message' u.jon)
          =/  requested-ref=(unit @t)  (string-at 'ref' u.jon)
          ?.  &(?=(^ message) !=('' u.message))
            :_  this
            (api-error eyre-id 422 'non-empty commit message is required')
          =/  branch-ref=@t  ?~(requested-ref head.u.found u.requested-ref)
          ?.  ?&  (valid-ref:git-protocol branch-ref)
                  (starts-with 'refs/heads/' branch-ref)
              ==
            :_  this
            (api-error eyre-id 422 'ref must be a valid branch')
          =/  parent=(unit oid:git)  (~(get by refs.u.found) branch-ref)
          ?~  parent
            :_  this
            (api-error eyre-id 404 'branch not found')
          =/  snapped=(unit [commit=oid:git objects=(map oid:git object:git)])
            %:  delete-commit:git-tree
              objects.u.found
              u.parent
              u.file-path
              our.bowl
              now.bowl
              u.message
            ==
          ?~  snapped
            :_  this
            (api-error eyre-id 404 'file not found or branch head is not a valid Git tree')
          =/  applied=repository:git
            %=  u.found
              objects  objects.u.snapped
              refs  (~(put by refs.u.found) branch-ref commit.u.snapped)
            ==
          (finish-file-edit eyre-id name applied branch-ref commit.u.snapped)
        ::
        --
      ::
      ++  repository-settings-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'DELETE' %repository @ ~]  delete-repository
            [%'POST' %repository @ %public ~]  post-repository-public
            [%'POST' %repository @ %description ~]  post-repository-description
            [%'POST' %repository @ %branches ~]  post-repository-branches
            [%'DELETE' %repository @ %branches ~]  delete-repository-branches
            [%'POST' %repository @ %branches %default ~]  post-repository-branches-default
            [%'POST' %repository @ %notifications ~]  post-repository-notifications
            [%'POST' %repository @ %webhooks ~]  post-repository-webhooks
            [%'DELETE' %repository @ %webhooks ~]  delete-repository-webhooks
            [%'POST' %repository @ %webhooks @ %test ~]  post-repository-webhooks-test
            [%'POST' %repository @ %incoming-hook ~]  post-repository-incoming-hook
            [%'DELETE' %repository @ %incoming-hook ~]  delete-repository-incoming-hook
            [%'DELETE' %repository @ %upstream-updates ~]  delete-repository-upstream-updates
            [%'POST' %repository @ %releases ~]  post-repository-releases
            [%'DELETE' %repository @ %releases ~]  delete-repository-releases
            [%'POST' %repository @ %tags ~]  post-repository-tags
            [%'DELETE' %repository @ %tags ~]  delete-repository-tags
            [%'POST' %repository @ %writers ~]  post-repository-writers
            [%'POST' %repository @ %readers ~]  post-repository-readers
            [%'POST' %repository @ %group-policy ~]  post-repository-group-policy
            [%'POST' %repository @ %protected ~]  post-repository-protected
            [%'POST' %repository @ %token ~]  post-repository-token
            [%'DELETE' %repository @ %token ~]  delete-repository-token
            [%'POST' %repository @ %bind ~]  post-repository-bind
            [%'POST' %repository @ %unbind ~]  post-repository-unbind
            [%'POST' %repository @ %publish ~]  post-repository-publish
          ==
        ::
        ++  delete-repository
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ ~] site)
              ==
          =/  name=@t  (api-terminal-name i.t.t.t.t.site ext.line)
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          (api-with-action eyre-id 200 [%delete name])
        ::
        ++  post-repository-public
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %public ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  public=(unit ?)  (bool-at 'publicRead' u.jon)
          ?~  public
            :_  this
            (api-error eyre-id 422 'publicRead is required')
          (api-with-action eyre-id 200 [%set-public name u.public])
        ::
        ++  post-repository-description
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %description ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  description=(unit @t)  (string-at 'description' u.jon)
          ?~  description
            :_  this
            (api-error eyre-id 422 'description is required')
          (api-with-action eyre-id 200 [%set-description name u.description])
        ::
        ++  post-repository-branches
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %branches ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  branch-name=(unit @t)  (string-at 'name' u.jon)
          =/  source-name=(unit @t)  (string-at 'source' u.jon)
          ?.  &(?=(^ branch-name) ?=(^ source-name))
            :_  this
            (api-error eyre-id 422 'name and source are required')
          =/  branch-ref=@t  (rap 3 ~['refs/heads/' u.branch-name])
          ?.  (valid-ref:git-protocol branch-ref)
            :_  this
            (api-error eyre-id 422 'branch name is invalid')
          ?:  (~(has by refs.u.found) branch-ref)
            :_  this
            (api-error eyre-id 409 'branch already exists')
          =/  source=(unit oid:git)  (revision-oid u.found u.source-name)
          ?~  source
            :_  this
            (api-error eyre-id 404 'branch source not found')
          (api-with-action eyre-id 201 [%set-ref name branch-ref u.source])
        ::
        ++  delete-repository-branches
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %branches ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  branch-name=(unit @t)  (string-at 'name' u.jon)
          ?~  branch-name
            :_  this
            (api-error eyre-id 422 'name is required')
          =/  branch-ref=@t  (rap 3 ~['refs/heads/' u.branch-name])
          ?.  (~(has by refs.u.found) branch-ref)
            :_  this
            (api-error eyre-id 404 'branch not found')
          ?:  =(branch-ref head.u.found)
            :_  this
            (api-error eyre-id 409 'cannot delete the default branch')
          ?:  (~(has in protected-refs.u.found) branch-ref)
            :_  this
            (api-error eyre-id 409 'cannot delete a protected branch')
          ?:  &(?=(^ binding.u.found) =(branch-ref branch.u.binding.u.found))
            :_  this
            (api-error eyre-id 409 'cannot delete a branch linked to a Clay desk')
          (api-with-action eyre-id 200 [%delete-ref name branch-ref])
        ::
        ++  post-repository-branches-default
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %branches %default ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  branch-name=(unit @t)  (string-at 'name' u.jon)
          ?~  branch-name
            :_  this
            (api-error eyre-id 422 'name is required')
          =/  branch-ref=@t  (rap 3 ~['refs/heads/' u.branch-name])
          ?.  (~(has by refs.u.found) branch-ref)
            :_  this
            (api-error eyre-id 404 'branch not found')
          (api-with-action eyre-id 200 [%set-head name branch-ref])
        ::
        ++  post-repository-notifications
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %notifications ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  events=(unit (set notification-event:git))
            (notification-events-at 'events' u.jon)
          ?~  events
            :_  this
            %^  api-error
              eyre-id
              422
            'events must contain only issue, issue-comment, pull-request, or pull-comment'
          =/  updated=repository:git  u.found(notification-events u.events)
          =.  repositories  (~(put by repositories) name updated)
          :_  this
          (api-json eyre-id 200 (repository-json name updated))
        ::
        ++  post-repository-webhooks
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %webhooks ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  url=(unit @t)  (string-at 'url' u.jon)
          =/  secret=(unit @t)  (string-at 'secret' u.jon)
          =/  events=(unit (set webhook-event:git))  (webhook-events-at 'events' u.jon)
          ?.  ?&  ?=(^ url)
                  ?=(^ secret)
                  ?=(^ events)
                  ?=(^ ~(tap in u.events))
                  |((starts-with 'https://' u.url) (starts-with 'http://' u.url))
                  (lte (met 3 u.url) 2.048)
                  !=('' u.secret)
                  (lte (met 3 u.secret) 256)
              ==
            :_  this
            (api-error eyre-id 422 'url, secret, and at least one valid event are required')
          =/  entries=(list [@ud webhook:git])  ~(tap by webhooks.u.found)
          =/  next-id=@ud  1
          =.  next-id
            |-
            ?~  entries  next-id
            $(entries t.entries, next-id (max next-id +(id.+.i.entries)))
          =/  hook=webhook:git  [next-id u.url u.secret u.events %.y]
          =/  updated=repository:git
            u.found(webhooks (~(put by webhooks.u.found) next-id hook))
          =.  repositories  (~(put by repositories) name updated)
          :_  this
          (api-json eyre-id 201 (webhook-json hook))
        ::
        ++  delete-repository-webhooks
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %webhooks ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  id=(unit @ud)  (nat-at 'id' u.jon)
          ?.  &(?=(^ id) (~(has by webhooks.u.found) u.id))
            :_  this
            (api-error eyre-id 404 'webhook not found')
          =.  repositories
            (~(put by repositories) name u.found(webhooks (~(del by webhooks.u.found) u.id)))
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  post-repository-webhooks-test
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %webhooks @ %test ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  id=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?.  &(?=(^ id) ?=(^ found) (~(has by webhooks.u.found) u.id))
            :_  this
            (api-error eyre-id 404 'webhook not found')
          =/  hook=webhook:git  (~(got by webhooks.u.found) u.id)
          =/  test-repo=repository:git
            u.found(webhooks (~(put by webhooks.u.found) u.id hook(events (silt ~[%push]))))
          =.  repositories  (~(put by repositories) name test-repo)
          =/  result=(quip card _this)
            (dispatch-webhooks name %push (pairs:enjs:format ~[['test' b+%.y]]))
          =/  restored=(unit repository:git)  (~(get by repositories.+.result) name)
          =/  next=_this
            ?~  restored  +.result
            %=  +.result
              repositories
                %:  ~(put by repositories.+.result)
                  name
                  u.restored(webhooks (~(put by webhooks.u.restored) u.id hook))
                ==
            ==
          :_  next
          (weld -.result (api-json eyre-id 202 (pairs:enjs:format ~[['ok' b+%.y]])))
        ::
        ++  post-repository-incoming-hook
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %incoming-hook ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  secret=(unit @t)  (string-at 'secret' u.jon)
          ?.  &(?=(^ secret) !=('' u.secret) (lte (met 3 u.secret) 256))
            :_  this
            (api-error eyre-id 422 'a non-empty secret up to 256 bytes is required')
          =.  repositories
            (~(put by repositories) name u.found(incoming-hook `[[u.secret %.y]]))
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  delete-repository-incoming-hook
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %incoming-hook ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =.  repositories  (~(put by repositories) name u.found(incoming-hook ~))
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  delete-repository-upstream-updates
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %upstream-updates ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  id-text=(unit @t)  (string-at 'id' u.jon)
          =/  id=(unit @uv)  ?~(id-text ~ (slaw %uv u.id-text))
          ?~  id
            :_  this
            (api-error eyre-id 422 'valid update id required')
          =/  matches=(list upstream-update:git)
            (skim upstream-updates.u.found |=(update=upstream-update:git =(id.update u.id)))
          =/  updates=(list upstream-update:git)
            ?~  matches  upstream-updates.u.found
            %+  skim
              upstream-updates.u.found
            |=(update=upstream-update:git !=(ref.update ref.i.matches))
          =.  repositories  (~(put by repositories) name u.found(upstream-updates updates))
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  post-repository-releases
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %releases ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  tag=(unit @t)  (string-at 'tag' u.jon)
          =/  title=(unit @t)  (string-at 'title' u.jon)
          =/  notes=(unit @t)  (string-at 'notes' u.jon)
          ?.  ?&  ?=(^ tag)
                  ?=(^ title)
                  ?=(^ notes)
                  !=('' u.tag)
                  !=('' u.title)
                  (lte (met 3 u.title) 200)
                  (lte (met 3 u.notes) 65.536)
              ==
            :_  this
            (api-error eyre-id 422 'tag and title are required; notes are limited to 64 KiB')
          =/  tag-ref=@t  (rap 3 ~['refs/tags/' u.tag])
          ?.  (~(has by refs.u.found) tag-ref)
            :_  this
            (api-error eyre-id 404 'tag not found')
          ?:  (~(has by releases.u.found) u.tag)
            :_  this
            (api-error eyre-id 409 'release already exists for this tag')
          =/  =release:git  [u.tag u.title u.notes our.bowl now.bowl]
          =.  repositories
            %+  ~(put by repositories)
              name
            u.found(releases (~(put by releases.u.found) u.tag release))
          =/  dispatched=(quip card _this)
            (dispatch-webhooks name %release (release-json release %.y))
          :_  +.dispatched
          (weld -.dispatched (api-json eyre-id 201 (release-json release %.y)))
        ::
        ++  delete-repository-releases
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %releases ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  tag=(unit @t)  (string-at 'tag' u.jon)
          ?.  &(?=(^ tag) (~(has by releases.u.found) u.tag))
            :_  this
            (api-error eyre-id 404 'release not found')
          =.  repositories
            (~(put by repositories) name u.found(releases (~(del by releases.u.found) u.tag)))
          :_  this
          (api-ok eyre-id 200)
        ::
        ++  post-repository-tags
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %tags ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  tag-name=(unit @t)  (string-at 'name' u.jon)
          =/  target-name=(unit @t)  (string-at 'target' u.jon)
          =/  message=(unit @t)  (string-at 'message' u.jon)
          ?.  &(?=(^ tag-name) ?=(^ target-name) ?=(^ message))
            :_  this
            (api-error eyre-id 422 'name, target, and message are required')
          =/  tag-ref=@t  (rap 3 ~['refs/tags/' u.tag-name])
          ?.  (valid-ref:git-protocol tag-ref)
            :_  this
            (api-error eyre-id 422 'tag name is invalid')
          ?:  (~(has by refs.u.found) tag-ref)
            :_  this
            (api-error eyre-id 409 'tag already exists')
          =/  clay-number=(unit @ud)  (clay-revision-number u.target-name)
          =/  prepared=(unit [repo=repository:git target=oid:git])
            ?~  clay-number
              =/  target=(unit oid:git)  (revision-oid u.found u.target-name)
              ?~  target  ~
              `[u.found u.target]
            (materialize-clay-revision u.found u.clay-number our.bowl now.bowl)
          ?~  prepared
            :_  this
            (api-error eyre-id 404 'tag target not found')
          =/  applied=repository:git
            ?:  =('' u.message)
              repo.u.prepared(refs (~(put by refs.repo.u.prepared) tag-ref target.u.prepared))
            =/  tagged=(unit [tag=oid:git objects=(map oid:git object:git)])
              %:  annotated-tag:git-tree
                objects.repo.u.prepared
                target.u.prepared
                u.tag-name
                our.bowl
                now.bowl
                u.message
              ==
            ?~  tagged  repo.u.prepared
            %=  repo.u.prepared
              objects  objects.u.tagged
              refs  (~(put by refs.repo.u.prepared) tag-ref tag.u.tagged)
            ==
          =.  repositories  (~(put by repositories) name applied)
          =/  tag-oid=oid:git  (need (~(get by refs.applied) tag-ref))
          =/  event-data=json
            %-  pairs:enjs:format
            :~  ['ref' s+tag-ref]
                ['oid' s+(oid-text:git-codec tag-oid)]
                ['target' s+(oid-text:git-codec target.u.prepared)]
                ['clayRevision' n+(decimal ?~(clay-number 0 u.clay-number))]
            ==
          =/  dispatched=(quip card _this)  (dispatch-webhooks name %tag event-data)
          :_  +.dispatched
          %+  weld
            -.dispatched
          %^  api-json
            eyre-id
            201
          %-  pairs:enjs:format
          ~[['ok' b+%.y] ['ref' s+tag-ref] ['oid' s+(oid-text:git-codec tag-oid)]]
        ::
        ++  delete-repository-tags
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %tags ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  tag-name=(unit @t)  (string-at 'name' u.jon)
          ?~  tag-name
            :_  this
            (api-error eyre-id 422 'name is required')
          =/  tag-ref=@t  (rap 3 ~['refs/tags/' u.tag-name])
          ?.  (~(has by refs.u.found) tag-ref)
            :_  this
            (api-error eyre-id 404 'tag not found')
          ?:  (~(has by releases.u.found) u.tag-name)
            :_  this
            (api-error eyre-id 409 'delete the release before deleting its tag')
          =.  repositories
            %+  ~(put by repositories)
              name
            u.found(refs (~(del by refs.u.found) tag-ref))
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y]]))
        ::
        ++  post-repository-writers
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %writers ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  allowed=(unit ?)  (bool-at 'allowed' u.jon)
          ?.  &(?=(^ ship-text) ?=(^ allowed))
            :_  this
            (api-error eyre-id 422 'ship and allowed are required')
          =/  writer=(unit @p)  (slaw %p u.ship-text)
          ?~  writer
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          ?:  u.allowed
            (api-with-action eyre-id 200 [%grant-writer name u.writer])
          (api-with-action eyre-id 200 [%revoke-writer name u.writer])
        ::
        ++  post-repository-readers
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %readers ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ship-text=(unit @t)  (string-at 'ship' u.jon)
          =/  allowed=(unit ?)  (bool-at 'allowed' u.jon)
          ?.  &(?=(^ ship-text) ?=(^ allowed))
            :_  this
            (api-error eyre-id 422 'ship and allowed are required')
          =/  reader=(unit @p)  (slaw %p u.ship-text)
          ?~  reader
            :_  this
            (api-error eyre-id 422 'ship must be a valid Urbit ID')
          ?:  u.allowed
            (api-with-action eyre-id 200 [%grant-reader name u.reader])
          (api-with-action eyre-id 200 [%revoke-reader name u.reader])
        ::
        ++  post-repository-group-policy
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %group-policy ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  policy-json=(unit json)  (json-at 'policy' u.jon)
          ?~  policy-json
            :_  this
            (api-error eyre-id 422 'policy is required; null clears it')
          ?~  u.policy-json
            (api-with-action eyre-id 200 [%set-group-policy name ~])
          =/  parsed=(each group-policy:git @t)  (parse-group-policy u.policy-json)
          ?:  ?=(%| -.parsed)
            :_  this
            (api-error eyre-id 422 p.parsed)
          (api-with-action eyre-id 200 [%set-group-policy name `p.parsed])
        ::
        ++  post-repository-protected
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %protected ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  ref=(unit @t)  (string-at 'ref' u.jon)
          =/  protected=(unit ?)  (bool-at 'protected' u.jon)
          ?.  ?&  ?=(^ ref)
                  ?=(^ protected)
                  (valid-ref:git-protocol u.ref)
                  (starts-with 'refs/heads/' u.ref)
              ==
            :_  this
            (api-error eyre-id 422 'a valid branch ref and protected flag are required')
          (api-with-action eyre-id 200 [%set-protected name u.ref u.protected])
        ::
        ++  post-repository-token
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %token ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  token=(unit @t)  (string-at 'token' u.jon)
          ?.  &(?=(^ token) !=('' u.token))
            :_  this
            (api-error eyre-id 422 'non-empty token is required')
          (api-with-action eyre-id 200 [%set-write-token name u.token])
        ::
        ++  delete-repository-token
          ^-  (quip card _this)
          ?>  ?&  =(%'DELETE' method)
                  ?=([%apps %urgit %api %repository @ %token ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          (api-with-action eyre-id 200 [%clear-write-token name])
        ::
        ++  post-repository-bind
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %bind ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  desk-text=(unit @t)  (string-at 'desk' u.jon)
          =/  branch=(unit @t)  (string-at 'branch' u.jon)
          ?.  &(?=(^ desk-text) ?=(^ branch))
            :_  this
            (api-error eyre-id 422 'desk and branch are required')
          =/  desk-name=(unit @tas)  (slaw %tas u.desk-text)
          ?~  desk-name
            :_  this
            (api-error eyre-id 422 'desk must be a valid term')
          ?.  ?&  (starts-with 'refs/heads/' u.branch)
                  (valid-ref:git-protocol u.branch)
              ==
            :_  this
            (api-error eyre-id 422 'branch must be a valid refs/heads/... ref')
          =/  desks=(unit (set desk))
            %-  mole
            |.(.^((set desk) %cd /(scot %p our.bowl)//(scot %da now.bowl)))
          ?.  &(?=(^ desks) (~(has in u.desks) u.desk-name))
            :_  this
            (api-error eyre-id 404 'Clay desk not found')
          (api-with-action eyre-id 200 [%bind-desk name u.desk-name u.branch])
        ::
        ++  post-repository-unbind
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %unbind ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          ?.  (~(has by repositories) name)
            :_  this
            (api-error eyre-id 404 'repository not found')
          (api-with-action eyre-id 200 [%unbind-desk name])
        ::
        ++  post-repository-publish
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %publish ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          ?~  binding.u.found
            :_  this
            (api-error eyre-id 409 'repository is not bound to a Clay desk')
          ?:  |(=(^ pending-clay) =(^ pending-publish))
            :_  this
            (api-error eyre-id 409 'another Clay operation is in progress')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  message=(unit @t)  (string-at 'message' u.jon)
          ?.  &(?=(^ message) !=('' u.message))
            :_  this
            (api-error eyre-id 422 'non-empty message is required')
          (api-with-action eyre-id 202 [%publish-desk name u.message])
        --
      ::
      ++  repository-pulls-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'POST' %repository @ %pulls ~]  post-repository-pulls
            [%'GET' %repository @ %pulls @ ~]  get-repository-pulls
            [%'POST' %repository @ %pulls @ %comments ~]  post-repository-pulls-comments
              [%'POST' %repository @ %pulls @ %comments @ %resolve ~]
            post-repository-pulls-comments-resolve
            [%'POST' %repository @ %pulls @ %state ~]  post-repository-pulls-state
            [%'POST' %repository @ %pulls @ %merge ~]  post-repository-pulls-merge
          ==
        ::
        ++  post-repository-pulls
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %pulls ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  title=(unit @t)  (string-at 'title' u.jon)
          =/  source-ref=(unit @t)  (string-at 'sourceBranch' u.jon)
          =/  target-ref=(unit @t)  (string-at 'targetBranch' u.jon)
          ?.  ?&  ?=(^ title)
                  ?=(^ source-ref)
                  ?=(^ target-ref)
                  !=('' u.title)
                  (lte (met 3 u.title) 200)
                  (starts-with 'refs/heads/' u.source-ref)
                  (valid-ref:git-protocol u.source-ref)
                  (starts-with 'refs/heads/' u.target-ref)
                  (valid-ref:git-protocol u.target-ref)
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'title, sourceBranch, and targetBranch are required; refs must be valid branches'
          =/  incoming=(unit oid:git)  (~(get by refs.u.found) u.source-ref)
          ?~  incoming
            :_  this
            (api-error eyre-id 404 'source branch not found')
          =/  base=(unit oid:git)  (~(get by refs.u.found) u.target-ref)
          ?~  base
            :_  this
            (api-error eyre-id 404 'target branch not found')
          ?:  =(u.incoming u.base)
            :_  this
            (api-error eyre-id 409 'selected branches have identical tips')
          =/  number=@ud  (add 1 (lent native-pulls.u.found))
          =/  pull=native-pull:git
            [number our.bowl name u.source-ref u.target-ref u.title %open u.incoming u.base ~]
          =.  repositories
            (~(put by repositories) name u.found(native-pulls [pull native-pulls.u.found]))
          =/  event-data=json
            %-  pairs:enjs:format
            :~  ['number' n+(decimal number)]
                ['title' s+u.title]
                ['sourceRef' s+u.source-ref]
                ['targetRef' s+u.target-ref]
                ['state' s+'open']
            ==
          =/  dispatched=(quip card _this)
            (dispatch-webhooks name %pull-request event-data)
          :_  +.dispatched
          %+  weld
            -.dispatched
          (api-json eyre-id 201 (pairs:enjs:format ~[['ok' b+%.y] ['number' n+(decimal number)]]))
        ::
        ++  get-repository-pulls
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %pulls @ ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'pull request number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  matches=(list native-pull:git)
            (skim native-pulls.u.found |=(pull=native-pull:git =(number.pull u.number)))
          ?~  matches
            :_  this
            (api-error eyre-id 404 'pull request not found')
          =/  pull=native-pull:git  i.matches
          =/  detail=(unit json)  (native-pull-detail-json name u.found pull)
          ?~  detail
            :_  this
            (api-error eyre-id 409 'pull request objects are incomplete')
          :_  this
          (api-json eyre-id 200 u.detail)
        ::
        ++  post-repository-pulls-comments
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %pulls @ %comments ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'pull request number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  matches=(list native-pull:git)
            (skim native-pulls.u.found |=(pull=native-pull:git =(number.pull u.number)))
          ?~  matches
            :_  this
            (api-error eyre-id 404 'pull request not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  body=(unit @t)  (string-at 'body' u.jon)
          =/  path-text=(unit @t)  (string-at 'path' u.jon)
          =/  line-number=(unit @ud)  (nat-at 'line' u.jon)
          =/  side-text=(unit @t)  (string-at 'side' u.jon)
          ?.  ?&  ?=(^ body)
                  ?=(^ path-text)
                  ?=(^ line-number)
                  ?=(^ side-text)
                  !=('' u.body)
                  (lte (met 3 u.body) 16.384)
                  (lte (met 3 u.path-text) 2.048)
              ==
            :_  this
            %^  api-error
              eyre-id
              422
            'body, path, line, and side are required; comment body is limited to 16 KiB'
          =/  anchored=?  !=('' u.path-text)
          ?.  ?:  anchored
                ?&  (gth u.line-number 0)
                    ?|  =('base' u.side-text)
                        =('head' u.side-text)
                    ==
                ==
              &(=(0 u.line-number) =('' u.side-text))
            :_  this
            %^  api-error
              eyre-id
              422
            'line comments require a path, positive line, and base or head side'
          =/  pull=native-pull:git  i.matches
          =/  comment=review-comment:git
            :*  (add 1 (lent comments.pull))
                our.bowl
                u.body
                now.bowl
                ?:(anchored `u.path-text ~)
                ?:(anchored `u.line-number ~)
                ?:(anchored `?:(=('base' u.side-text) %base %head) ~)
                %.n
            ==
          =/  pulls=(list native-pull:git)
            %+  turn  native-pulls.u.found
            |=  candidate=native-pull:git
            ?:  =(number.candidate u.number)
              candidate(comments (weld comments.candidate ~[comment]))
            candidate
          =.  repositories
            (~(put by repositories) name u.found(native-pulls pulls))
          :_  this
          (api-json eyre-id 201 (review-comment-json comment))
        ::
        ++  post-repository-pulls-comments-resolve
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %pulls @ %comments @ %resolve ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          =/  comment-id=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.t.t.site)
          ?.  &(?=(^ number) ?=(^ comment-id))
            :_  this
            (api-error eyre-id 422 'pull request or comment number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  matches=(list native-pull:git)
            (skim native-pulls.u.found |=(pull=native-pull:git =(number.pull u.number)))
          ?~  matches
            :_  this
            (api-error eyre-id 404 'pull request not found')
          =/  pull=native-pull:git  i.matches
          =/  comment-matches=(list review-comment:git)
            (skim comments.pull |=(comment=review-comment:git =(id.comment u.comment-id)))
          ?~  comment-matches
            :_  this
            (api-error eyre-id 404 'review comment not found')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  resolved=(unit ?)  (bool-at 'resolved' u.jon)
          ?~  resolved
            :_  this
            (api-error eyre-id 422 'resolved flag is required')
          =/  updated-comments=(list review-comment:git)
            %+  turn  comments.pull
            |=  comment=review-comment:git
            ?:(=(id.comment u.comment-id) comment(resolved u.resolved) comment)
          =/  updated=review-comment:git
            i.comment-matches(resolved u.resolved)
          =/  pulls=(list native-pull:git)
            %+  turn  native-pulls.u.found
            |=  candidate=native-pull:git
            ?:(=(number.candidate u.number) candidate(comments updated-comments) candidate)
          =.  repositories
            (~(put by repositories) name u.found(native-pulls pulls))
          :_  this
          (api-json eyre-id 200 (review-comment-json updated))
        ::
        ++  post-repository-pulls-state
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %pulls @ %state ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'pull request number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  matches=(list native-pull:git)
            (skim native-pulls.u.found |=(pull=native-pull:git =(number.pull u.number)))
          ?~  matches
            :_  this
            (api-error eyre-id 404 'pull request not found')
          =/  pull=native-pull:git  i.matches
          ?:  =(%merged state.pull)
            :_  this
            (api-error eyre-id 409 'merged pull requests cannot be reopened or closed')
          =/  jon=(unit json)  (api-body req)
          ?~  jon
            :_  this
            (api-error eyre-id 400 'valid JSON body required')
          =/  requested=(unit @t)  (string-at 'state' u.jon)
          ?.  &(?=(^ requested) |(=('open' u.requested) =('closed' u.requested)))
            :_  this
            (api-error eyre-id 422 'state must be open or closed')
          =/  next-state=?(%open %closed)  ?:(=('open' u.requested) %open %closed)
          =/  pulls=(list native-pull:git)
            %+  turn  native-pulls.u.found
            |=  candidate=native-pull:git
            ?:(=(number.candidate u.number) candidate(state next-state) candidate)
          =.  repositories
            (~(put by repositories) name u.found(native-pulls pulls))
          :_  this
          (api-json eyre-id 200 (pairs:enjs:format ~[['ok' b+%.y] ['state' s+next-state]]))
        ::
        ++  post-repository-pulls-merge
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %pulls @ %merge ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  number=(unit @ud)  (slaw %ud i.t.t.t.t.t.t.site)
          ?~  number
            :_  this
            (api-error eyre-id 422 'pull request number is invalid')
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  matches=(list native-pull:git)
            (skim native-pulls.u.found |=(pull=native-pull:git =(number.pull u.number)))
          ?~  matches
            :_  this
            (api-error eyre-id 404 'pull request not found')
          =/  pull=native-pull:git  i.matches
          ?.  =(%open state.pull)
            :_  this
            (api-error eyre-id 409 'pull request is not open')
          =/  current=(unit oid:git)  (~(get by refs.u.found) target-ref.pull)
          ?~  current
            :_  this
            (api-error eyre-id 409 'destination branch has no head')
          =/  incoming-reachable=(unit (set oid:git))
            (reachable:git-graph objects.u.found (silt ~[head.pull]))
          =/  current-reachable=(unit (set oid:git))
            (reachable:git-graph objects.u.found (silt ~[u.current]))
          ?.  &(?=(^ incoming-reachable) ?=(^ current-reachable))
            :_  this
            (api-error eyre-id 409 'pull request object graph is incomplete')
          =/  fast-forward=?  (~(has in u.incoming-reachable) u.current)
          =/  already-merged=?  (~(has in u.current-reachable) head.pull)
          =/  common-base=?
            ?&  (~(has in u.incoming-reachable) base.pull)
                (~(has in u.current-reachable) base.pull)
            ==
          ?.  |(fast-forward already-merged common-base)
            :_  this
            (api-error eyre-id 409 'pull request branches no longer share the recorded base')
          =/  integrated=(unit [commit=oid:git objects=(map oid:git object:git)])
            ?:  fast-forward  `[head.pull objects.u.found]
            ?:  already-merged  `[u.current objects.u.found]
            %:  merge-commit:git-tree
              objects.u.found
              base.pull
              u.current
              head.pull
              our.bowl
              now.bowl
              (rap 3 ~['Merge pull request #' (decimal number.pull) ': ' title.pull])
            ==
          ?~  integrated
            :_  this
            (api-error eyre-id 409 'pull request has conflicting file changes')
          |^
            commit-merge
          ++  commit-merge
            =/  merge-oid=oid:git  commit.u.integrated
            =/  pulls=(list native-pull:git)
              %+  turn  native-pulls.u.found
              |=  candidate=native-pull:git
              ?:  =(number.candidate number.pull)
                candidate(state %merged)
              candidate
            =/  applied=repository:git
              %=  u.found
                objects  objects.u.integrated
                refs  (~(put by refs.u.found) target-ref.pull merge-oid)
                native-pulls  pulls
              ==
            =/  clay-linked=?
              ?~  binding.applied  %.n
              =(target-ref.pull branch.u.binding.applied)
            ?.  clay-linked
              =.  repositories  (~(put by repositories) name applied)
              :_  this
              %^  api-json
                eyre-id
                200
              (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec merge-oid)]])
            ?>  ?=(^ binding.applied)
            ?:  |(=(^ pending-clay) =(^ pending-publish))
              :_  this
              (api-error eyre-id 409 'another Clay operation is in progress')
            =/  files=(unit (map path octs))
              (flatten-commit:git-clay objects.applied merge-oid)
            ?~  files
              :_  this
              (api-error eyre-id 409 'pull request head is not a desk-shaped Git commit')
            =/  delta=(unit nori:clay)
              (clay-delta our.bowl now.bowl desk-name.u.binding.applied u.files)
            ?~  delta
              :_  this
              (api-error eyre-id 409 'unable to read linked Clay desk')
            ?>  ?=(%& -.u.delta)
            ?:  =(~ p.u.delta)
              =.  repositories  (~(put by repositories) name applied)
              :_  this
              %^  api-json
                eyre-id
                200
              (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec merge-oid)]])
            =/  start-at=@da  (add now.bowl ~s1)
            =/  timeout-at=@da  (add now.bowl ~s15)
            =/  pending=clay-push
              :*  eyre-id
                  %.y
                  ~
                  name
                  ~
                  applied
                  desk-name.u.binding.applied
                  branch.u.binding.applied
                  merge-oid
                  u.delta
                  ~
                  start-at
                  timeout-at
              ==
            =.  pending-clay  `pending
            :_  this
            :~  [%pass /clay-start %arvo %b %wait start-at]
                [%pass /clay-timeout %arvo %b %wait timeout-at]
            ==
          --
        ::
        --
      ::
      ++  repository-lfs-api
        |^
          ?+  [method (slag 3 site)]
            [(api-error eyre-id 404 'API route not found') this]
            [%'GET' %repository @ %lfs %gc ~]  get-repository-lfs-gc
            [%'POST' %repository @ %lfs %gc ~]  post-repository-lfs-gc
          ==
        ::
        ++  get-repository-lfs-gc
          ^-  (quip card _this)
          ?>  ?&  =(%'GET' method)
                  ?=([%apps %urgit %api %repository @ %lfs %gc ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  preview=(unit json)  (lfs-gc-json u.found)
          ?~  preview
            :_  this
            (api-error eyre-id 409 'repository object graph is incomplete')
          :_  this
          (api-json eyre-id 200 u.preview)
        ::
        ++  post-repository-lfs-gc
          ^-  (quip card _this)
          ?>  ?&  =(%'POST' method)
                  ?=([%apps %urgit %api %repository @ %lfs %gc ~] site)
              ==
          =/  name=@t  i.t.t.t.t.site
          =/  found=(unit repository:git)  (~(get by repositories) name)
          ?~  found
            :_  this
            (api-error eyre-id 404 'repository not found')
          =/  settings=(unit lfs-settings)
            storage-settings
          ?~  settings
            :_  this
            (api-error eyre-id 503 'ship object storage is not configured')
          =/  live=(unit (set @t))  (referenced-lfs u.found)
          ?~  live
            :_  this
            (api-error eyre-id 409 'repository object graph is incomplete')
          =/  candidates=(list [@t lfs-object:git])
            %+  skim  ~(tap by lfs-objects.u.found)
            |=  entry=[@t lfs-object:git]
            !(~(has in u.live) -.entry)
          =.  candidates  (scag 100 candidates)
          =/  cards=(list card)  ~
          =/  scheduled=@ud  0
          |-
          ?~  candidates
            :_  this
            %+  weld
              cards
            (api-json eyre-id 202 (pairs:enjs:format ~[['scheduled' n+(decimal scheduled)]]))
          =/  request-id=@uv
            `@uv`(shas %git-lfs-delete (cat 3 eny.bowl request-count))
          =.  request-count  +(request-count)
          =/  signed=signed-request:git-storage
            %:  sign:git-storage
              'DELETE'
              'application/octet-stream'
              [0 0]
              credentials.u.settings
              configuration.u.settings
              object-key.+.i.candidates
              now.bowl
            ==
          =.  lfs-deletes  (~(put by lfs-deletes) request-id [name -.i.candidates])
          =.  cards
            :*  :*  %pass
                    /lfs-delete/(scot %uv request-id)
                    %arvo
                    %i
                    %request
                    [%'DELETE' url.signed headers.signed ~]
                    *outbound-config:iris
                ==
                cards
            ==
          $(candidates t.candidates, scheduled +(scheduled))
        --
      --
    ::
    ++  storage-settings
      ^-  (unit lfs-settings)
      =/  found-credentials=(unit json)
        %-  mole
        |.(.^(json %gx /(scot %p our.bowl)/storage/(scot %da now.bowl)/credentials/json))
      ?~  found-credentials  ~
      =/  found-configuration=(unit json)
        %-  mole
        |.(.^(json %gx /(scot %p our.bowl)/storage/(scot %da now.bowl)/configuration/json))
      ?~  found-configuration  ~
      =/  get-string
        |=  [jon=json keys=(list @t)]
        ^-  @t
        ?~  keys  ?:(?=([%s *] jon) p.jon '')
        ?.  ?=([%o *] jon)  ''
        =/  value=(unit json)  (~(get by p.jon) i.keys)
        ?~  value  ''
        $(jon u.value, keys t.keys)
      =/  =credentials:git-storage
        :*  (get-string u.found-credentials ~['storage-update' 'credentials' 'endpoint'])
            (get-string u.found-credentials ~['storage-update' 'credentials' 'accessKeyId'])
            (get-string u.found-credentials ~['storage-update' 'credentials' 'secretAccessKey'])
        ==
      =/  =configuration:git-storage
        :*  (get-string u.found-configuration ~['storage-update' 'configuration' 'currentBucket'])
            (get-string u.found-configuration ~['storage-update' 'configuration' 'region'])
        ==
      =/  service=@t
        (get-string u.found-configuration ~['storage-update' 'configuration' 'service'])
      ?.  =('credentials' service)  ~
      ?.  ?&  !=('' endpoint.credentials)
              !=('' access-key-id.credentials)
              !=('' secret-access-key.credentials)
              !=('' current-bucket.configuration)
              !=('' region.configuration)
          ==
        ~
      `[credentials configuration]
    ::
    ++  handle-http
      |=  [eyre-id=@ta req=inbound-request:eyre]
      ^-  (quip card _this)
      =/  line=request-line:server  (parse-request-line:server url.request.req)
      =/  site=(list @t)  site.line
      ?:  (starts-with '/apps/urgit/api' url.request.req)
        (handle-api eyre-id req line)
      ?:  ?=([%git @ %info %lfs %locks %verify ~] site)
        (handle-lfs-lock-verify eyre-id req (repository-name i.t.site))
      ?:  ?=([%git @ %info %lfs %locks @ %unlock ~] site)
        (handle-lfs-unlock eyre-id req (repository-name i.t.site) i.t.t.t.t.t.site)
      ?:  ?=([%git @ %info %lfs %locks ~] site)
        (handle-lfs-locks eyre-id req line (repository-name i.t.site))
      ?:  ?=([%git @ %info %lfs %objects %batch ~] site)
        (handle-lfs-batch eyre-id req (repository-name i.t.site))
      ?:  ?=([%git @ %info %lfs %objects @ %verify ~] site)
        (handle-lfs-verify eyre-id req (repository-name i.t.site) i.t.t.t.t.t.site)
      ?:  ?=([%git @ %info %refs ~] site)
        (handle-discovery eyre-id req line (repository-name i.t.site))
      ?:  ?=([%git @ %git-upload-pack ~] site)
        (handle-upload-pack eyre-id req (repository-name i.t.site))
      ?:  ?=([%git @ %git-receive-pack ~] site)
        (handle-receive-pack eyre-id req (repository-name i.t.site))
      :_  this
      (give-text eyre-id 404 'repository route not found\0a')
    ::
    ++  handle-discovery
      |=  [eyre-id=@ta req=inbound-request:eyre line=request-line:server repo-name=@t]
      ^-  (quip card _this)
      ?.  =(%'GET' method.request.req)
        :_  this
        (give-text eyre-id 405 'method not allowed\0a')
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-text eyre-id 404 'repository not found\0a')
      =/  service=(unit @t)  (query-value 'service' args.line)
      ?~  service
        :_  this
        (give-text eyre-id 400 'missing service\0a')
      ?.  ?|  =('git-upload-pack' u.service)
              =('git-receive-pack' u.service)
          ==
        :_  this
        (give-text eyre-id 403 'service disabled\0a')
      =/  authorized=?
        ?:  =('git-upload-pack' u.service)
          |(public-read.u.found authenticated.req (write-authorized u.found req))
        (write-authorized u.found req)
      ?.  authorized
        :_  this
        %-  give-http
        :*  eyre-id
            401
            ~[['content-type' 'text/plain'] ['www-authenticate' 'Basic realm="git"']]
            `(text:git-codec 'repository authentication required\0a')
        ==
      =/  protocol=(unit @t)
        (get-header:http 'git-protocol' header-list.request.req)
      =/  use-v2=?
        ?&  =('git-upload-pack' u.service)
            ?=(^ protocol)
            =('version=2' u.protocol)
        ==
      =/  body=octs
        ?:
          use-v2
          v2-capability-advertisement:git-protocol
        (smart-advertisement:git-protocol u.found u.service)
      =/  content-type=@t
        (rap 3 ~['application/x-' u.service '-advertisement'])
      =/  headers=(list [@t @t])
        :~  ['content-type' content-type]
            ['cache-control' 'no-cache, max-age=0, must-revalidate']
            ['pragma' 'no-cache']
        ==
      [(give-simple-payload:app:server eyre-id [[200 headers] `body]) this]
    ::
    ++  handle-upload-pack
      |=  [eyre-id=@ta req=inbound-request:eyre repo-name=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (give-text eyre-id 405 'method not allowed\0a')
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-text eyre-id 404 'repository not found\0a')
      ?.  |(public-read.u.found authenticated.req (write-authorized u.found req))
        :_  this
        (give-text eyre-id 403 'repository is private\0a')
      ?~  body.request.req
        :_  this
        (give-text eyre-id 400 'missing upload-pack request\0a')
      =/  decoded=(each octs [status=@ud message=@t])  (decoded-body req)
      ?:  ?=(%| -.decoded)
        :_  this
        (give-text eyre-id status.p.decoded message.p.decoded)
      =/  body=octs  p.decoded
      ::  Answer git's pre-upload probe before the protocol v2 dispatch and
      ::  before the request parser.  The probe body is a lone flush packet:
      ::  it names no v2 command and asks for no object, so nothing further
      ::  down may see it.  git needs only the 200 to proceed with the real
      ::  request.
      ::
      ?:  (flush-only-body:git-protocol body)
        :_  this
        %-  give-http
        :*  eyre-id
            200
            :~  ['content-type' 'application/x-git-upload-pack-result']
                ['cache-control' 'no-store']
            ==
            ~
        ==
      |^
        =/  v2-command=(unit @tas)
          (v2-command:git-protocol body)
        ?:  &(?=(^ v2-command) =(%ls-refs u.v2-command))
          =/  response=octs
            (v2-ls-refs:git-protocol u.found body)
          =/  headers=(list [@t @t])
            :~  ['content-type' 'application/x-git-upload-pack-result']
                ['cache-control' 'no-store']
            ==
          :_  this
          (give-http eyre-id 200 headers `response)
        ?:  &(?=(^ v2-command) =(%object-info u.v2-command))
          object-info
        ?:  &(?=(^ v2-command) !=(%fetch u.v2-command))
          :_  this
          (give-text eyre-id 400 'unsupported protocol v2 command\0a')
        =/  use-v2=?  &(?=(^ v2-command) =(%fetch u.v2-command))
        =/  parsed=(unit upload-request:git)
          (parse-upload-request:git-protocol body)
        ?~  parsed
          :_  this
          (give-text eyre-id 400 'invalid upload-pack request\0a')
        =/  result=(each octs [status=@ud message=@t])
          (upload-response:git-upload u.found u.parsed use-v2)
        ?:  ?=(%| -.result)
          :_  this
          (give-text eyre-id status.p.result message.p.result)
        :_  this
        %:  give-http
          eyre-id
          200
          :~  ['content-type' 'application/x-git-upload-pack-result']
              ['cache-control' 'no-store']
          ==
          `p.result
        ==
      ++  object-info
        =/  requested=(unit (list oid:git))
          (v2-object-info-oids:git-protocol body)
        ?~  requested
          :_  this
          (give-text eyre-id 400 'invalid protocol v2 object-info request\0a')
        =/  advertised-roots=(set oid:git)
          %-  silt
          %+  turn  ~(tap by refs.u.found)
          |=  entry=[@t oid:git]
          +.entry
        =/  advertised=(unit (set oid:git))
          (reachable:git-graph objects.u.found advertised-roots)
        ?.  ?&  ?=(^ advertised)
                (levy u.requested |=(oid=oid:git (~(has in u.advertised) oid)))
            ==
          :_  this
          (give-text eyre-id 404 'object is not reachable from an advertised ref\0a')
        =/  response=(unit octs)
          (v2-object-info:git-protocol objects.u.found u.requested)
        ?~  response
          :_  this
          (give-text eyre-id 404 'object not found\0a')
        =/  headers=(list [@t @t])
          :~  ['content-type' 'application/x-git-upload-pack-result']
              ['cache-control' 'no-store']
          ==
        :_  this
        (give-http eyre-id 200 headers `u.response)
      --
    ::
    ++  handle-receive-pack
      |=  [eyre-id=@ta req=inbound-request:eyre repo-name=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (give-text eyre-id 405 'method not allowed\0a')
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-text eyre-id 404 'repository not found\0a')
      ?.  (write-authorized u.found req)
        :_  this
        %-  give-http
        :*  eyre-id
            401
            ~[['content-type' 'text/plain'] ['www-authenticate' 'Basic realm="git"']]
            `(text:git-codec 'repository authentication required\0a')
        ==
      ?~  body.request.req
        :_  this
        (give-text eyre-id 400 'missing receive-pack request\0a')
      ::  git 2.55.0 never gzips a receive-pack body -- it streams the pack
      ::  and compresses nothing -- but the header is decoded here too so a
      ::  client that does gzip one is served, and a Content-Encoding this
      ::  agent does not implement is refused instead of misparsed.
      ::
      =/  decoded=(each octs [status=@ud message=@t])  (decoded-body req)
      ?:  ?=(%| -.decoded)
        :_  this
        (give-text eyre-id status.p.decoded message.p.decoded)
      =/  body=octs  p.decoded
      ::  Answer git's pre-upload probe before any parsing, policy, or ref
      ::  update runs.  The probe body is a lone flush packet and carries no
      ::  commands; git needs only the 200 to proceed with the real request.
      ::
      ?:  (flush-only-body:git-protocol body)
        :_  this
        %-  give-http
        :*  eyre-id
            200
            :~  ['content-type' 'application/x-git-receive-pack-result']
                ['cache-control' 'no-store']
            ==
            ~
        ==
      =/  parsed=(unit receive-request:git)
        (parse-receive-request:git-protocol body)
      ?~  parsed
        :_  this
        (give-text eyre-id 400 'invalid receive-pack request\0a')
      |^
        apply-pack
      ++  apply-pack
        =/  staged=(unit (map oid:git object:git))
          ?:  =(0 p.pack.u.parsed)
            `~
          =/  decoded=(unit decoded-pack:git-pack-decode)
            (decode-pack-with:git-pack-decode pack.u.parsed objects.u.found)
          ?~  decoded  ~
          `objects.u.decoded
        ?~  staged
          :_  this
          %+  give-simple-payload:app:server  eyre-id
          %+  receive-payload
            'invalid or unsupported pack'
          (receive-results commands.u.parsed %.n 'unpack failed')
        =/  policy-error=(unit @t)
          (receive-policy-error u.found commands.u.parsed u.staged)
        ?^  policy-error
          :_  this
          %+  give-simple-payload:app:server  eyre-id
          (receive-payload 'ok' (receive-results commands.u.parsed %.n u.policy-error))
        =/  applied=(unit repository:git)
          (apply-receive u.found commands.u.parsed u.staged)
        ?~  applied
          :_  this
          %+  give-simple-payload:app:server  eyre-id
          %+  receive-payload
            'ok'
          (receive-results commands.u.parsed %.n 'stale or invalid ref update')
        |^
          ?^  binding.u.applied
            apply-linked-ref
          accept-unlinked-ref
        ++  apply-linked-ref
          ?>  ?=(^ binding.u.applied)
          =/  linked-command=(unit receive-command:git)
            (command-for-ref commands.u.parsed branch.u.binding.u.applied)
          ?~  linked-command
            (accept-receive eyre-id repo-name commands.u.parsed u.applied ~)
          =/  maybe-pending=(unit clay-push)  pending-clay
          ?^  maybe-pending
            :_  this
            %+  give-simple-payload:app:server  eyre-id
            %+  receive-payload
              'ok'
            (receive-results commands.u.parsed %.n 'linked desk update already in progress')
          ?^  pending-publish
            :_  this
            %+  give-simple-payload:app:server  eyre-id
            %+  receive-payload
              'ok'
            (receive-results commands.u.parsed %.n 'linked desk publish already in progress')
          ?~  new.u.linked-command
            :_  this
            %+  give-simple-payload:app:server  eyre-id
            %+  receive-payload
              'ok'
            (receive-results commands.u.parsed %.n 'cannot delete a branch linked to a Clay desk')
          =/  files=(unit (map path octs))
            (flatten-commit:git-clay objects.u.applied u.new.u.linked-command)
          ?~  files
            :_  this
            %+  give-simple-payload:app:server  eyre-id
            %+  receive-payload
              'ok'
            %^  receive-results
              commands.u.parsed
              %.n
            'linked branch must resolve to a valid desk-shaped Git commit'
          =/  delta=(unit nori:clay)
            (clay-delta our.bowl now.bowl desk-name.u.binding.u.applied u.files)
          ?~  delta
            :_  this
            %+  give-simple-payload:app:server  eyre-id
            %+  receive-payload
              'ok'
            (receive-results commands.u.parsed %.n 'unable to read linked Clay desk')
          ?>  ?=(%& -.u.delta)
          ?:  =(~ p.u.delta)
            =/  clay=[desk-name=desk commit=oid:git]
              [desk-name.u.binding.u.applied u.new.u.linked-command]
            (accept-receive eyre-id repo-name commands.u.parsed u.applied `clay)
          =/  pending=clay-push
            =/  start-at=@da  (add now.bowl ~s1)
            =/  timeout-at=@da  (add now.bowl ~s15)
            :*  eyre-id
                %.n
                ~
                repo-name
                commands.u.parsed
                u.applied
                desk-name.u.binding.u.applied
                branch.u.binding.u.applied
                u.new.u.linked-command
                u.delta
                ~
                start-at
                timeout-at
            ==
          =.  pending-clay  `pending
          :_  this
          :~  [%pass /clay-start %arvo %b %wait start-at.pending]
              [%pass /clay-timeout %arvo %b %wait timeout-at.pending]
          ==
        ++  accept-unlinked-ref
          (accept-receive eyre-id repo-name commands.u.parsed u.applied ~)
        --
      ::
      --
    ::
    ++  handle-lfs-locks
      |=  [eyre-id=@ta req=inbound-request:eyre line=request-line:server repo-name=@t]
      ^-  (quip card _this)
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'repository not found'))
      |^
        ?:  =(%'GET' method.request.req)
          list-locks
        create-lock
      ++  list-locks
        ?.  |(public-read.u.found authenticated.req (write-authorized u.found req))
          :_  this
          %+  give-simple-payload:app:server
            eyre-id
          (lfs-error 403 'pull access is required to list locks')
        =/  path-filter=(unit @t)  (query-value 'path' args.line)
        =/  id-text=(unit @t)  (query-value 'id' args.line)
        =/  id-filter=(unit @ud)  ?~(id-text ~ (slaw %ud u.id-text))
        ?:  &(?=(^ id-text) ?=(~ id-filter))
          :_  this
          (give-simple-payload:app:server eyre-id (lfs-error 422 'lock id is invalid'))
        =/  cursor-text=(unit @t)  (query-value 'cursor' args.line)
        =/  cursor-filter=(unit @ud)  ?~(cursor-text `0 (slaw %ud u.cursor-text))
        ?~  cursor-filter
          :_  this
          (give-simple-payload:app:server eyre-id (lfs-error 422 'lock cursor is invalid'))
        =/  limit-text=(unit @t)  (query-value 'limit' args.line)
        =/  requested-limit=(unit @ud)  ?~(limit-text `100 (slaw %ud u.limit-text))
        ?~  requested-limit
          :_  this
          (give-simple-payload:app:server eyre-id (lfs-error 422 'lock limit is invalid'))
        =/  limit=@ud  (min 100 (max 1 u.requested-limit))
        =/  entries=(list [@ud lfs-lock:git])  ~(tap by lfs-locks.u.found)
        =.  entries
          %+  sort  entries
          |=  [a=[@ud lfs-lock:git] b=[@ud lfs-lock:git]]
          (lth -.a -.b)
        =.  entries
          %+  skim  entries
          |=  entry=[@ud lfs-lock:git]
          ?&  (gth -.entry u.cursor-filter)
              ?~(path-filter %.y =(path.+.entry u.path-filter))
              ?~(id-filter %.y =(-.entry u.id-filter))
          ==
        =/  more=?  (gth (lent entries) limit)
        =/  shown=(list [@ud lfs-lock:git])  (scag limit entries)
        =/  fields=(list [@t json])
          ~[['locks' [%a (turn shown |=(entry=[@ud lfs-lock:git] (lfs-lock-json +.entry)))]]]
        =.  fields
          ?.  more  fields
          =/  last=[@ud lfs-lock:git]  (snag (dec limit) shown)
          =/  next=json  s+(decimal -.last)
          (weld fields ~[['next_cursor' next]])
        :_  this
        (give-simple-payload:app:server eyre-id (json-payload 200 (pairs:enjs:format fields)))
      ++  create-lock
        ?.  =(%'POST' method.request.req)
          :_  this
          (give-simple-payload:app:server eyre-id (lfs-error 405 'method not allowed'))
        ?.  (write-authorized u.found req)
          :_  this
          %+  give-simple-payload:app:server
            eyre-id
          (lfs-error 403 'push access is required to create a lock')
        =/  principal=(unit @t)  (lfs-principal our.bowl req)
        ?~  principal
          :_  this
          %+  give-simple-payload:app:server
            eyre-id
          (lfs-error 403 'lock owner could not be determined')
        ?~  body.request.req
          :_  this
          (give-simple-payload:app:server eyre-id (lfs-error 400 'missing lock request'))
        =/  jon=(unit json)  (de:json:html q.u.body.request.req)
        ?~  jon
          :_  this
          (give-simple-payload:app:server eyre-id (lfs-error 400 'invalid JSON'))
        =/  lock-path=(unit @t)  (string-at 'path' u.jon)
        ?.  ?&  ?=(^ lock-path)
                !=('' u.lock-path)
                (lte (met 3 u.lock-path) 2.048)
                !=('/' (cut 3 [0 1] u.lock-path))
            ==
          :_  this
          %+  give-simple-payload:app:server
            eyre-id
          (lfs-error 422 'lock path must be a relative repository path')
        =/  conflicts=(list [@ud lfs-lock:git])
          %+  skim
            ~(tap by lfs-locks.u.found)
          |=(entry=[@ud lfs-lock:git] =(path.+.entry u.lock-path))
        ?^  conflicts
          =/  response=json
            %-  pairs:enjs:format
            :~  ['lock' (lfs-lock-json +.i.conflicts)]
                ['message' s+'path is already locked']
            ==
          :_  this
          (give-simple-payload:app:server eyre-id (json-payload 409 response))
        =/  entries=(list [@ud lfs-lock:git])  ~(tap by lfs-locks.u.found)
        =/  next-id=@ud  1
        =.  next-id
          |-
          ?~  entries  next-id
          $(entries t.entries, next-id (max next-id (add 1 -.i.entries)))
        =/  lock=lfs-lock:git  [next-id u.lock-path u.principal now.bowl]
        =/  updated=repository:git
          u.found(lfs-locks (~(put by lfs-locks.u.found) next-id lock))
        =.  repositories  (~(put by repositories) repo-name updated)
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (json-payload 201 (pairs:enjs:format ~[['lock' (lfs-lock-json lock)]]))
      --
    ::
    ++  handle-lfs-lock-verify
      |=  [eyre-id=@ta req=inbound-request:eyre repo-name=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 405 'method not allowed'))
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'repository not found'))
      ?.  (write-authorized u.found req)
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 403 'push access is required to verify locks')
      =/  principal=(unit @t)  (lfs-principal our.bowl req)
      ?~  principal
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 403 'lock owner could not be determined')
      =/  jon=json
        ?~  body.request.req  [%o ~]
        =/  parsed=(unit json)  (de:json:html q.u.body.request.req)
        ?~(parsed [%o ~] u.parsed)
      =/  cursor-text=(unit @t)  (string-at 'cursor' jon)
      =/  cursor=(unit @ud)  ?~(cursor-text `0 (slaw %ud u.cursor-text))
      ?~  cursor
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 422 'lock cursor is invalid'))
      =/  requested-limit=(unit @ud)  (nat-at 'limit' jon)
      =/  limit=@ud  (min 100 (max 1 ?~(requested-limit 100 u.requested-limit)))
      =/  entries=(list [@ud lfs-lock:git])  ~(tap by lfs-locks.u.found)
      =.  entries
        %+  sort  entries
        |=  [a=[@ud lfs-lock:git] b=[@ud lfs-lock:git]]
        (lth -.a -.b)
      =.  entries  (skim entries |=(entry=[@ud lfs-lock:git] (gth -.entry u.cursor)))
      =/  more=?  (gth (lent entries) limit)
      =/  shown=(list [@ud lfs-lock:git])  (scag limit entries)
      =/  ours=(list json)
        %+  turn
          (skim shown |=(entry=[@ud lfs-lock:git] =(owner.+.entry u.principal)))
        |=(entry=[@ud lfs-lock:git] (lfs-lock-json +.entry))
      =/  theirs=(list json)
        %+  turn
          (skim shown |=(entry=[@ud lfs-lock:git] !=(owner.+.entry u.principal)))
        |=(entry=[@ud lfs-lock:git] (lfs-lock-json +.entry))
      =/  fields=(list [@t json])  ~[['ours' [%a ours]] ['theirs' [%a theirs]]]
      =.  fields
        ?.  more  fields
        =/  last=[@ud lfs-lock:git]  (snag (dec limit) shown)
        =/  next=json  s+(decimal -.last)
        (weld fields ~[['next_cursor' next]])
      :_  this
      (give-simple-payload:app:server eyre-id (json-payload 200 (pairs:enjs:format fields)))
    ::
    ++  handle-lfs-unlock
      |=  [eyre-id=@ta req=inbound-request:eyre repo-name=@t id-text=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 405 'method not allowed'))
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'repository not found'))
      ?.  (write-authorized u.found req)
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 403 'push access is required to delete a lock')
      =/  principal=(unit @t)  (lfs-principal our.bowl req)
      ?~  principal
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 403 'lock owner could not be determined')
      =/  lock-id=(unit @ud)  (slaw %ud id-text)
      ?~  lock-id
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 422 'lock id is invalid'))
      =/  lock=(unit lfs-lock:git)  (~(get by lfs-locks.u.found) u.lock-id)
      ?~  lock
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'lock not found'))
      =/  force=?
        ?~  body.request.req  %.n
        =/  parsed=(unit json)  (de:json:html q.u.body.request.req)
        ?~  parsed  %.n
        (fall (bool-at 'force' u.parsed) %.n)
      ?.  |(force =(owner.u.lock u.principal))
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 403 'only the lock owner may unlock without force')
      =/  updated=repository:git
        u.found(lfs-locks (~(del by lfs-locks.u.found) u.lock-id))
      =.  repositories  (~(put by repositories) repo-name updated)
      :_  this
      %+  give-simple-payload:app:server
        eyre-id
      (json-payload 200 (pairs:enjs:format ~[['lock' (lfs-lock-json u.lock)]]))
    ::
    ++  handle-lfs-batch
      |=  [eyre-id=@ta req=inbound-request:eyre repo-name=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 405 'method not allowed'))
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'repository not found'))
      ?~  body.request.req
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 400 'missing batch request'))
      =/  jon=(unit json)  (de:json:html q.u.body.request.req)
      ?~  jon
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 400 'invalid JSON'))
      =/  operation=(unit @t)  (string-at 'operation' u.jon)
      ?.  &(?=(^ operation) |(=(u.operation 'upload') =(u.operation 'download')))
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 422 'operation must be upload or download')
      =/  hash-algorithm=(unit @t)  (string-at 'hash_algo' u.jon)
      ?:  &(?=(^ hash-algorithm) !=(u.hash-algorithm 'sha256'))
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 409 'only sha256 object identifiers are supported')
      =/  specs=(unit (list lfs-spec))  (parse-lfs-specs u.jon)
      ?~  specs
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 422 'invalid object list'))
      ?:  (gth (lent u.specs) 1.000)
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 413 'batch exceeds 1000 objects'))
      =/  allowed=?
        ?:  =(u.operation 'upload')
          (write-authorized u.found req)
        |(public-read.u.found authenticated.req (write-authorized u.found req))
      ?.  allowed
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 401 'repository authentication required')
      =/  settings=(unit lfs-settings)
        storage-settings
      ?~  settings
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 503 'ship object storage is not configured')
      =/  working=repository:git  u.found
      =/  objects=(list json)  ~
      =/  remaining=(list lfs-spec)  u.specs
      |-
      ?~  remaining
        =.  repositories  (~(put by repositories) repo-name working)
        =/  response=json
          %-  pairs:enjs:format
          :~  ['transfer' s+'basic']
              ['objects' a+(flop objects)]
              ['hash_algo' s+'sha256']
          ==
        :_  this
        (give-simple-payload:app:server eyre-id (json-payload 200 response))
      =^  item  working
        %:  lfs-batch-object
          working
          i.remaining
          u.operation
          u.settings
          our.bowl
          now.bowl
          req
          repo-name
        ==
      $(remaining t.remaining, objects [item objects])
    ::
    ++  handle-lfs-verify
      |=  [eyre-id=@ta req=inbound-request:eyre repo-name=@t oid=@t]
      ^-  (quip card _this)
      ?.  =(%'POST' method.request.req)
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 405 'method not allowed'))
      =/  found=(unit repository:git)  (~(get by repositories) repo-name)
      ?~  found
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'repository not found'))
      =/  pending=(unit lfs-upload:git)  (~(get by lfs-uploads.u.found) oid)
      ?~  pending
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 404 'upload is not pending'))
      ?:  (gth now.bowl expires.u.pending)
        =/  repo=repository:git  u.found(lfs-uploads (~(del by lfs-uploads.u.found) oid))
        =.  repositories  (~(put by repositories) repo-name repo)
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 410 'upload verification expired'))
      ?~  body.request.req
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 400 'missing verification body'))
      =/  jon=(unit json)  (de:json:html q.u.body.request.req)
      ?~  jon
        :_  this
        (give-simple-payload:app:server eyre-id (lfs-error 400 'invalid JSON'))
      =/  body-oid=(unit @t)  (string-at 'oid' u.jon)
      =/  body-size=(unit @ud)  (nat-at 'size' u.jon)
      ?.  ?&  ?=(^ body-oid)
              ?=(^ body-size)
              =(u.body-oid oid)
              =(u.body-size size.u.pending)
          ==
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 422 'verification does not match pending upload')
      =/  settings=(unit lfs-settings)
        storage-settings
      ?~  settings
        :_  this
        %+  give-simple-payload:app:server
          eyre-id
        (lfs-error 503 'ship object storage is not configured')
      =/  signed=signed-request:git-storage
        %:  sign:git-storage
          'HEAD'
          'application/octet-stream'
          [0 0]
          credentials.u.settings
          configuration.u.settings
          object-key.u.pending
          now.bowl
        ==
      =/  request-id=@uv
        `@uv`(shas %git-lfs-request (cat 3 eny.bowl request-count))
      =.  request-count  +(request-count)
      =.  in-flight  (~(put by in-flight) request-id [eyre-id repo-name oid u.pending])
      :_  this
      :~  :*  %pass
              /iris/(scot %uv request-id)
              %arvo
              %i
              %request
              [%'HEAD' url.signed headers.signed ~]
              *outbound-config:iris
          ==
      ==
    ::
    --
  =/  after=peer-ui-state
    =>  +.result
    [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
  [(peer-ui-notify before after -.result) +.result]
::
++  on-peek
  |=  =path
  ^-  (unit (unit cage))
  |^
    ?+  path  (on-peek:def path)
      [%x %visibility %build ~]  ``noun+!>(%transfer-visibility-t0-t1-t2)
      [%x %state %version ~]  ``noun+!>(-.state)
      [%x %dbug %state ~]  debug-state
        [%x %repositories ~]
      =/  visible=(map @t repository:git)
        %-  malt
        %+  murn  ~(tap by repositories)
        |=  entry=[@t repository:git]
        ?.  public-read.+.entry  ~
        `entry
      ``json+!>((public-repositories-json visible))
        [%x %repository @ ~]
      =/  name=@t  i.t.t.path
      =/  found=(unit repository:git)  (~(get by repositories) name)
      ?~  found  [~ ~]
      ?.  public-read.u.found  [~ ~]
      ``json+!>((public-repository-json name u.found))
        [%x %repository @ %files ~]
      =/  name=@t  i.t.t.path
      =/  found=(unit repository:git)  (~(get by repositories) name)
      ?~  found  [~ ~]
      ?.  public-read.u.found  [~ ~]
      ``json+!>((repository-files-json name u.found))
        [%x %repository @ %commits ~]
      =/  name=@t  i.t.t.path
      =/  found=(unit repository:git)  (~(get by repositories) name)
      ?~  found  [~ ~]
      ?.  public-read.u.found  [~ ~]
      ``json+!>((repository-history-json name u.found head.u.found our.bowl now.bowl 0 50))
        [%x %repository @ %browse ~]
      =/  name=@t  i.t.t.path
      =/  found=(unit repository:git)  (~(get by repositories) name)
      ?~  found  [~ ~]
      ?.  public-read.u.found  [~ ~]
      ``json+!>((peer-repository-browse-json name u.found))
        [%x %fine @ ~]
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.path)
      ?~  transfer  [~ ~]
      =/  found=(unit peer-serve)  (~(get by peer-serving) u.transfer)
      ?~  found  [~ ~]
      =/  objects=(map oid:git object:git)  (silt objects.u.found)
      ``noun+!>(objects)
    ==
  ::
  ++  debug-state
    ^-  (unit (unit cage))
    ?>  ?=([%x %dbug %state ~] path)
    =/  transfers=(list peer-transfer-debug)
      %+  turn  ~(tap by peer-receiving)
      |=  entry=[@uv peer-receive]
      =/  transfer=@uv  -.entry
      =/  flight=peer-receive  +.entry
      :*  transfer
          purpose.flight
          source.flight
          source-repository.flight
          local-repository.flight
          ?:
            =('' head.flight)
            ?:(accepted.flight %prepare %request)
          ?:(=(%archive mode.flight) %archive %fine)
          expected.flight
          expected-bytes.flight
          received.flight
          pages.flight
          %+  sort  ~(tap in completed.flight)
          |=  [a=@ud b=@ud]  (lth a b)
          %+  turn  ~(tap by fine-progress.flight)
          |=  progress=[@ud [fag=@ud tot=@ud]]
          [revision=-.progress fag=fag.+.progress tot=tot.+.progress]
          progress-at.flight
      ==
    =/  serving=(list peer-serve-debug)
      %+  turn  ~(tap by peer-serving)
      |=  entry=[@uv peer-serve]
      =/  flight=peer-serve  +.entry
      :*  transfer=-.entry
          target=target.flight
          repository=repository.flight
          mode=mode.flight
          pages=pages.flight
          bytes=bytes.flight
          sent=sent.flight
          objects=(lent objects.flight)
      ==
    ``noun+!>([transfers=transfers serving=serving results=~(tap by peer-results)])
  --
++  on-watch
  |=  =path
  ^-  (quip card _this)
  ?+  path  (on-watch:def path)
    [%http-response @ ~]  [~ this]
  ::
      ::  a browser joining mid-transfer gets the current state at once,
      ::  so it never waits for the next event to render something.
      ::
      [%peer %activity ~]
    =/  now-state=peer-ui-state
      [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
    :_  this
    ~[[%give %fact ~ %json !>((peer-ui-json now-state))]]
  ==
++  on-leave  on-leave:def
++  on-agent
  |=  [=wire =sign:agent:gall]
  ^-  (quip card _this)
  =/  before=peer-ui-state
    [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
  =/  result=(quip card _this)
    ::  the ack of a catalog request settles the ledger: the ship is back,
    ::  so it may be asked again.  a nack means the ship does not run
    ::  urgit, and settles the discovery now rather than at the timer
    ::
    ?:  ?=([%peer %catalog-request @ ~] wire)
      ?.  ?=(%poke-ack -.sign)  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =.  peer-inflight  (settled:git-catalog peer-inflight u.request)
      ?~  p.sign  `this
      =/  found=(unit peer-discovery)  (~(get by peer-discoveries) u.request)
      ?~  found  `this
      ?.  active.u.found  `this
      =.  peer-discoveries
        (~(put by peer-discoveries) u.request (nacked:git-catalog u.found))
      `this
    ?.  =(/clay-push wire)
      (on-agent:def wire sign)
    =/  maybe-pending=(unit clay-push)  pending-clay
    ?~  maybe-pending  `this
    =/  pending=clay-push  u.maybe-pending
    ?.  ?=(%poke-ack -.sign)
      (on-agent:def wire sign)
    =/  report-at=@da  (add now.bowl ~s1)
    ?~  p.sign
      =.  pending  pending(result `[%.y ''])
      =.  pending-clay  `pending
      :_  this
      :~  [%pass /clay-timeout %arvo %b %rest timeout-at.pending]
          [%pass /clay-report %arvo %b %wait report-at]
      ==
    =/  detail=@t  (tang-text u.p.sign)
    =/  message=@t
      ?:  =('' detail)
        'Clay rejected the linked desk update'
      (rap 3 ~['Clay rejected the linked desk update: ' detail])
    =.  pending  pending(result `[%.n message])
    =.  pending-clay  `pending
    :_  this
    :~  [%pass /clay-timeout %arvo %b %rest timeout-at.pending]
        [%pass /clay-report %arvo %b %wait report-at]
    ==
  =/  after=peer-ui-state
    =>  +.result
    [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
  [(peer-ui-notify before after -.result) +.result]
++  on-arvo
  |=  [=wire =sign-arvo]
  ^-  (quip card _this)
  =/  before=peer-ui-state
    [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
  =/  result=(quip card _this)
    =/  error-cards
      |=  [eyre-id=@ta status=@ud message=@t]
      ^-  (list card)
      =/  jon=json  (pairs:enjs:format ~[['message' s+message]])
      %+  give-simple-payload:app:server  eyre-id
      :*  [status ~[['content-type' 'application/vnd.git-lfs+json'] ['cache-control' 'no-store']]]
          `(json-to-octs:server jon)
      ==
    |^
      ?+  wire  (on-arvo:def wire sign-arvo)
        [%eyre *]  `this
        [%peer %prepare-start @ ~]  peer-prepare-start
        [%peer %fine @ @ ~]  peer-fine
        [%peer %rate @ @ ~]  peer-rate
        [%peer %browse @ @ ~]  peer-browse-response
        [%peer %browse-prepare @ ~]  peer-browse-prepare
        [%peer %browse-timeout @ ~]  peer-browse-timeout
        [%peer %browse-prepare-timeout @ ~]  peer-browse-prepare-timeout
        [%peer %browse-stall @ ~]  peer-browse-stall
        [%peer %stall @ @ ~]  peer-stall
        [%peer %offer-timeout @ ~]  peer-offer-timeout
        [%peer %request-timeout @ ~]  peer-request-timeout
        [%peer %prepare-timeout @ ~]  peer-prepare-timeout
        [%peer %archive-timeout @ ~]  peer-archive-timeout
        [%peer %serve-timeout @ ~]  peer-serve-timeout
        [%peer %forge-timeout @ ~]  peer-forge-timeout
        [%peer %discovery-timeout @ ~]  peer-discovery-timeout
        [%clay-publish ~]  clay-publish
        [%clay-start ~]  clay-start
        [%clay-timeout ~]  clay-timeout
        [%clay-report ~]  clay-report
        [%webhook @ ~]  webhook
        [%github @ ~]  github-response
        [%lfs-delete @ ~]  lfs-delete-response
        [%iris @ ~]  lfs-verify-response
      ==
    ::
    ++  peer-prepare-start
      ^-  (quip card _this)
      ?>  ?=([%peer %prepare-start @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  queued=(unit [target=ship req=request:git-peer])
        (~(get by peer-prepare-queue) u.transfer)
      ?~  queued  `this
      =.  peer-prepare-queue
        (~(del by peer-prepare-queue) u.transfer)
      :_  this
      :~  :*  %pass
              /peer/prepare/(scot %uv u.transfer)
              %agent
              [our.bowl %urgit]
              %poke
              %git-peer
              !>([%prepare target.u.queued req.u.queued])
          ==
      ==
    ::
    ++  peer-fine
      ^-  (quip card _this)
      ?>  ?=([%peer %fine @ @ ~] wire)
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  revision=(unit @ud)  (slaw %ud i.t.t.t.wire)
      ?~  revision  `this
      =/  found=(unit peer-receive)
        (~(get by peer-receiving) u.transfer)
      ?~  found  `this
      =/  fail
        |=  message=@t
        ^-  packet:git-peer
        [%snapshot-error u.transfer message]
      ?:  (~(has in completed.u.found) u.revision)  `this
      =/  =packet:git-peer
        ?.  &((gth u.revision 0) (lte u.revision pages.u.found))
          (fail 'Fine repository response used an invalid page revision')
        ?.  ?=([%ames %sage *] sign-arvo)
          (fail 'Fine repository read failed')
        =/  =sage:mess:ames  sage.sign-arvo
        ?.  =(ship.p.sage source.u.found)
          (fail 'Fine response came from the wrong ship')
        ?~  q.sage
          (fail 'Fine repository snapshot is unavailable')
        ?.  =(%noun p.q.sage)
          (fail 'Fine repository snapshot has the wrong mark')
        ?:  =(%objects mode.u.found)
          =/  fragments=(unit (list object-fragment:git-peer))
            %-  mole
            |.(;;((list object-fragment:git-peer) +.q.q.sage))
          ?~  fragments
            (fail 'Fine repository object page has the wrong shape')
          [%object-fragments u.transfer u.revision u.fragments]
        ?.  =(%pack mode.u.found)
          (fail 'Fine repository transfer has an unsupported mode')
        =/  packed=(unit octs)
          %-  mole
          |.(;;(octs +.q.q.sage))
        ?~  packed
          (fail 'Fine repository pack page has the wrong shape')
        =/  decoded=(unit decoded-pack:git-pack-decode)
          (decode-pack:git-pack-decode u.packed)
        ?~  decoded
          (fail 'Fine repository pack page failed checksum or object decoding')
        [%snapshot u.transfer objects.u.decoded]
      =/  snapshot-card=card
        (peer-card our.bowl /peer/snapshot/(scot %uv u.transfer) packet)
      ?:  ?=([%object-fragments *] packet)
        :_  this
        :~  snapshot-card
        ==
      ?.  ?=([%snapshot *] packet)
        :_  this
        :~  snapshot-card
        ==
      =.  peer-receiving
        %+  ~(put by peer-receiving)
          u.transfer
        u.found(completed (~(put in completed.u.found) u.revision))
      :_  this
      =/  next-revision=@ud  +(u.revision)
      ?:  (gth next-revision pages.u.found)  [snapshot-card ~]
      =/  next-path=path
        /g/x/(scot %ud next-revision)/urgit//1/fine/(peer-fine-name u.transfer)
      :~  snapshot-card
          :*  %pass
              /peer/fine/(scot %uv u.transfer)/(scot %ud next-revision)
              %keen
              %.n
              source.u.found
              next-path
          ==
      ==
    ::
    ++  peer-rate
      ^-  (quip card _this)
      ?>  ?=([%peer %rate @ @ ~] wire)
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  revision=(unit @ud)  (slaw %ud i.t.t.t.wire)
      ?~  revision  `this
      =/  found=(unit peer-receive)  (~(get by peer-receiving) u.transfer)
      ?~  found  `this
      ?.  ?=([%ames %rate *] sign-arvo)  `this
      =/  [tag=@tas =spar:ames =rate:ames]
        ;;([@tas spar:ames rate:ames] +.sign-arvo)
      ?@  rate  `this
      ?.  =(ship.spar source.u.found)  `this
      =/  previous=(unit [fag=@ud tot=@ud])
        (~(get by fine-progress.u.found) u.revision)
      ?.  ?~(previous %.y (gth fag.rate fag.u.previous))  `this
      =/  next=peer-receive
        u.found(fine-progress (~(put by fine-progress.u.found) u.revision [fag.rate tot.rate]))
      =.  peer-receiving  (~(put by peer-receiving) u.transfer next)
      `this
    ::
    ++  peer-browse-response
      ^-  (quip card _this)
      ?>  ?=([%peer %browse @ @ ~] wire)
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =/  revision=(unit @ud)  (slaw %ud i.t.t.t.wire)
      ?~  revision  `this
      =/  found=(unit peer-browse)  (~(get by peer-browses) u.request)
      ?~  found  `this
      ?.  &(active.u.found =(%fine phase.u.found))  `this
      =/  release-card=card
        :*  %pass
            /peer/browse-release/(scot %uv u.request)
            %agent
            [peer.u.found %urgit]
            %poke
            %git-peer
            !>([%browse-release u.request])
        ==
      =/  cancel-cards=(list card)
        (peer-browse-yawns u.request peer.u.found expected.u.found)
      =/  fail
        |=  message=@t
        ^-  (quip card _this)
        =.  peer-browses
          (~(put by peer-browses) u.request u.found(active %.n, ok %.n, message message))
        :_  this
        (weld cancel-cards [release-card ~])
      ?.  &((gth u.revision 0) (lte u.revision expected.u.found))
        (fail 'peer overview Fine response used an invalid page revision')
      =/  scry-path=path
        /g/x/(scot %ud u.revision)/urgit//1/browse/(scot %uv u.request)
      =/  browse-peer=ship  peer.u.found
      =/  cancel-card=card
        :*  %pass
            /peer/browse-cancel/(scot %uv u.request)/(scot %ud u.revision)
            %arvo
            %a
            %yawn
            [browse-peer scry-path]
        ==
      ?:  (~(has by parts.u.found) u.revision)  `this
      =/  page=(unit [length=@ud data=@])
        ?.  ?=([%ames %sage *] sign-arvo)  ~
        =/  =sage:mess:ames  sage.sign-arvo
        ?.  =(ship.p.sage browse-peer)  ~
        ?~  q.sage  ~
        ?.  =(%noun p.q.sage)  ~
        %-  mole
        |.(;;([@ud @] +.q.q.sage))
      ?~  page
        (fail 'peer overview Fine page was unavailable or malformed')
      ?.  ?&  (gth length.u.page 0)
              (lte length.u.page 65.536)
              (lte (met 3 data.u.page) length.u.page)
          ==
        (fail 'peer overview Fine page had an invalid size')
      =/  next-parts=(map @ud [length=@ud data=@])
        (~(put by parts.u.found) u.revision u.page)
      =/  next-received=@ud  +(received.u.found)
      =/  next=peer-browse
        %=  u.found
          parts  next-parts
          received  next-received
          progress  [~ [16 next-received expected.u.found]]
          progress-at  now.bowl
          message
            %:  rap
              3
              ~['received ' (decimal next-received) ' of ' (decimal expected.u.found) ' Fine pages']
            ==
        ==
      =.  peer-browses  (~(put by peer-browses) u.request next)
      ?.  =(next-received expected.next)
        :_  this
        :~  cancel-card
        ==
      |^
        complete-browse
      ++  complete-browse
        =/  encoded=(unit @)  (peer-browse-join parts.next expected.next)
        ?~  encoded
          (fail 'peer overview Fine pages were incomplete')
        =/  result=(unit json)
          %-  mole
          |.(;;(json (cue u.encoded)))
        ?~  result
          (fail 'peer overview Fine pages did not decode as JSON')
        =/  expected-repository=@t  repository.u.found
        =/  valid=?
          ?.  ?=([%o *] u.result)  %.n
          =/  repository-json=(unit json)  (~(get by p.u.result) 'repository')
          ?~  repository-json  %.n
          ?:  ?=([%s *] u.repository-json)
            =(p.u.repository-json expected-repository)
          ?.  ?=([%o *] u.repository-json)  %.n
          =/  name-json=(unit json)  (~(get by p.u.repository-json) 'name')
          ?~  name-json  %.n
          &(?=([%s *] u.name-json) =(p.u.name-json expected-repository))
        ?.  valid
          (fail 'peer browse result has the wrong repository identity')
        =.  peer-browses
          %+  ~(put by peer-browses)
            u.request
          next(active %.n, ok %.y, message 'complete', result `u.result)
        :_  this
        (weld cancel-cards [release-card ~])
      --
    ::
    ++  peer-browse-prepare
      ^-  (quip card _this)
      ?>  ?=([%peer %browse-prepare @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      :_  this
      :~  :*  %pass
              /peer/browse-prepare/(scot %uv u.request)
              %agent
              [our.bowl %urgit]
              %poke
              %git-peer
              !>([%browse-prepare u.request])
          ==
      ==
    ::
    ++  peer-browse-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %browse-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =/  found=(unit peer-browse)  (~(get by peer-browses) u.request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(%request phase.u.found)
          ==
        `this
      =.  peer-browses
        %+  ~(put by peer-browses)
          u.request
        u.found(active %.n, ok %.n, message 'peer did not answer the repository browse request')
      `this
    ::
    ++  peer-browse-prepare-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %browse-prepare-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =/  found=(unit peer-browse)  (~(get by peer-browses) u.request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(%prepare phase.u.found)
          ==
        `this
      =.  peer-browses
        %+  ~(put by peer-browses)
          u.request
        u.found(active %.n, ok %.n, message 'peer did not finish preparing the repository overview')
      :_  this
      :~  :*  %pass
              /peer/browse-release/(scot %uv u.request)
              %agent
              [peer.u.found %urgit]
              %poke
              %git-peer
              !>([%browse-release u.request])
          ==
      ==
    ::
    ++  peer-browse-stall
      ^-  (quip card _this)
      ?>  ?=([%peer %browse-stall @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =/  found=(unit peer-browse)  (~(get by peer-browses) u.request)
      ?~  found  `this
      ?.  ?&  active.u.found
              =(%fine phase.u.found)
          ==
        `this
      ?~  progress.u.found
        :_  this
        :~  [%pass /peer/browse-stall/(scot %uv u.request) %arvo %b %wait (add now.bowl ~s30)]
        ==
      ?.  (gte (sub now.bowl progress-at.u.found) ~m2)
        :_  this
        :~  [%pass /peer/browse-stall/(scot %uv u.request) %arvo %b %wait (add now.bowl ~s30)]
        ==
      =.  peer-browses
        %+  ~(put by peer-browses)
          u.request
        u.found(active %.n, ok %.n, message 'Fine transfer stalled without page progress')
      :_  this
      =/  cancel-cards=(list card)
        (peer-browse-yawns u.request peer.u.found expected.u.found)
      =/  release-cards=(list card)
        :~  :*  %pass
                /peer/browse-release/(scot %uv u.request)
                %agent
                [peer.u.found %urgit]
                %poke
                %git-peer
                !>([%browse-release u.request])
            ==
        ==
      (weld cancel-cards release-cards)
    ::
    ++  peer-stall
      ^-  (quip card _this)
      ?>  ?=([%peer %stall @ @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  checkpoint=(unit @ud)  (slaw %ud i.t.t.t.wire)
      ?~  checkpoint  `this
      =/  found=(unit peer-receive)  (~(get by peer-receiving) u.transfer)
      ?~  found  `this
      ?.  =(u.checkpoint received.u.found)  `this
      ?.  (gte (sub now.bowl progress-at.u.found) ~m2)
        :_  this
        :~  :*  %pass
                /peer/stall/(scot %uv u.transfer)/(scot %ud received.u.found)
                %arvo
                %b
                %wait
                (add now.bowl ~s30)
            ==
        ==
      =/  =packet:git-peer
        [%snapshot-error u.transfer 'Fine repository read stalled without fragment progress']
      :_  this
      :~  :*  %pass
              /peer/stall-result/(scot %uv u.transfer)
              %agent
              [our.bowl %urgit]
              %poke
              %git-peer
              !>(packet)
          ==
      ==
    ::
    ++  peer-offer-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %offer-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  outgoing=(unit peer-offer-flight)  (~(get by peer-outgoing) u.transfer)
      ?~  outgoing  `this
      =/  message=@t
        ?:  =(%push kind.u.outgoing)
          'peer did not answer the update offer'
        'peer did not answer the pull request offer'
      =.  peer-outgoing  (~(del by peer-outgoing) u.transfer)
      =.  peer-results
        (~(put by peer-results) u.transfer [%.n message repository.u.outgoing])
      =.  peer-activities
        %+  turn  peer-activities
        |=  event=peer-activity
        ?:  !=(u.transfer id.event)  event
        event(status %failure, message message, when now.bowl)
      `this
    ::
    ++  peer-request-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %request-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  found=(unit peer-receive)  (~(get by peer-receiving) u.transfer)
      ?~  found  `this
      ?:  accepted.u.found  `this
      ?.  =('' head.u.found)  `this
      =/  =packet:git-peer
        [%snapshot-error u.transfer 'peer did not answer the repository transfer request']
      :_  this
      :~  :*  %pass
              /peer/request-timeout-result/(scot %uv u.transfer)
              %agent
              [our.bowl %urgit]
              %poke
              %git-peer
              !>(packet)
          ==
      ==
    ::
    ++  peer-prepare-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %prepare-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  found=(unit peer-receive)  (~(get by peer-receiving) u.transfer)
      ?~  found  `this
      ?.  &(accepted.u.found =('' head.u.found))  `this
      =/  =packet:git-peer
        [%snapshot-error u.transfer 'peer did not finish preparing the repository snapshot']
      :_  this
      :~  :*  %pass
              /peer/prepare-timeout-result/(scot %uv u.transfer)
              %agent
              [our.bowl %urgit]
              %poke
              %git-peer
              !>(packet)
          ==
      ==
    ::
    ++  peer-archive-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %archive-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  found=(unit peer-receive)  (~(get by peer-receiving) u.transfer)
      ?~  found  `this
      ?.  =(%archive mode.u.found)  `this
      =/  =packet:git-peer
        [%snapshot-error u.transfer 'repository archive transfer did not complete within one day']
      :_  this
      :~  :*  %pass
              /peer/archive-timeout-result/(scot %uv u.transfer)
              %agent
              [our.bowl %urgit]
              %poke
              %git-peer
              !>(packet)
          ==
      ==
    ::
    ++  peer-serve-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %serve-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  transfer=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  transfer  `this
      =/  found=(unit peer-serve)  (~(get by peer-serving) u.transfer)
      ?~  found  `this
      =/  activity-id=@uv  (peer-serve-activity-id u.transfer)
      =.  peer-activities
        %+  turn  peer-activities
        |=  event=peer-activity
        ?:  !=(activity-id id.event)  event
        event(status %failure, message 'repository snapshot expired before release', when now.bowl)
      =/  count=@ud  pages.u.found
      =/  culls=(list card)
        ?:  =(0 count)  ~
        %+  turn  (gulf 1 count)
        |=  revision=@ud
        :*  %pass
            /peer/cull/(scot %uv u.transfer)/(scot %ud revision)
            %cull
            [%ud revision]
            /fine/(peer-fine-name u.transfer)
        ==
      :_  %=  this
            peer-serving  (~(del by peer-serving) u.transfer)
            peer-stream-jobs  (~(del by peer-stream-jobs) u.transfer)
          ==
      culls
    ::
    ++  peer-forge-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %forge-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =/  found=(unit peer-forge)  (~(get by peer-forges) u.request)
      ?~  found  `this
      ?.  active.u.found  `this
      =.  peer-forges
        %+  ~(put by peer-forges)
          u.request
        u.found(active %.n, ok %.n, message 'peer did not answer the forge request')
      `this
    ::
    ++  peer-discovery-timeout
      ^-  (quip card _this)
      ?>  ?=([%peer %discovery-timeout @ ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      ?^  error.sign-arvo  `this
      =/  request=(unit @uv)  (slaw %uv i.t.t.wire)
      ?~  request  `this
      =/  found=(unit peer-discovery)  (~(get by peer-discoveries) u.request)
      ?~  found  `this
      ?.  active.u.found  `this
      =.  peer-discoveries
        (~(put by peer-discoveries) u.request (timed-out:git-catalog u.found))
      `this
    ::
    ++  clay-publish
      ^-  (quip card _this)
      ?>  ?=([%clay-publish ~] wire)
      =/  maybe-job=(unit publish-job)  pending-publish
      ?~  maybe-job  `this
      =/  job=publish-job  u.maybe-job
      ?.  ?=([%clay %writ *] sign-arvo)
        `this(pending-publish ~)
      =/  =riot:clay  p.sign-arvo
      ?~  riot  `this(pending-publish ~)
      ?~  paths.job  `this(pending-publish ~)
      ?.  =(q.u.riot i.paths.job)
        `this(pending-publish ~)
      =/  raw-cage=cage  r.u.riot
      =/  raw-page=page  [p.raw-cage q.q.raw-cage]
      =/  data=(unit octs)
        (page-octs our.bowl desk-name.job now.bowl raw-page)
      ?~  data  `this(pending-publish ~)
      =/  next-job=publish-job
        =/  clay-revision=(unit @ud)
          ?:  ?=([%ud @] q.p.u.riot)
            `p.q.p.u.riot
          clay-revision.job
        :*  repository.job
            desk-name.job
            branch.job
            clay-revision
            message.job
            `(list path)`t.paths.job
            (~(put by files.job) i.paths.job u.data)
        ==
      =.  pending-publish  `next-job
      ?^  paths.next-job
        :_  this
        :~  :*  %pass
                /clay-publish
                %arvo
                %c
                %warp
                our.bowl
                desk-name.next-job
                ~
                %sing
                %q
                da+now.bowl
                i.paths.next-job
            ==
        ==
      =/  current=(unit repository:git)  (~(get by repositories) repository.next-job)
      ?~  current  `this(pending-publish ~)
      =/  published=(unit repository:git)
        (publish-repository u.current next-job our.bowl now.bowl)
      ?~  published  `this(pending-publish ~)
      =.  repositories  (~(put by repositories) repository.next-job u.published)
      `this(pending-publish ~)
    ::
    ++  clay-start
      ^-  (quip card _this)
      ?>  ?=([%clay-start ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      =/  maybe-pending=(unit clay-push)  pending-clay
      ?~  maybe-pending  `this
      =/  pending=clay-push  u.maybe-pending
      ?^  error.sign-arvo
        =/  detail=@t  (tang-text u.error.sign-arvo)
        =/  message=@t
          ?:  =('' detail)
            'Clay rejected the linked desk update'
          (rap 3 ~['Clay rejected the linked desk update: ' detail])
        =/  report-at=@da  (add now.bowl ~s1)
        =.  pending  pending(result `[%.n message])
        =.  pending-clay  `pending
        :_  this
        :~  [%pass /clay-timeout %arvo %b %rest timeout-at.pending]
            [%pass /clay-report %arvo %b %wait report-at]
        ==
      :_  this
      :~  :*  %pass
              /clay-push
              %agent
              [our.bowl %urgit-clay]
              %poke
              %git-clay-action
              !>([desk-name.pending delta.pending])
          ==
      ==
    ::
    ++  clay-timeout
      ^-  (quip card _this)
      ?>  ?=([%clay-timeout ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      =/  maybe-pending=(unit clay-push)  pending-clay
      ?~  maybe-pending  `this
      =/  pending=clay-push  u.maybe-pending
      ?^  error.sign-arvo
        `this(pending-clay ~)
      =/  report-at=@da  (add now.bowl ~s1)
      =.  pending  pending(result `[%.n 'Clay update timed out without a result'])
      =.  pending-clay  `pending
      :_  this
      :~  [%pass /clay-report %arvo %b %wait report-at]
      ==
    ::
    ++  clay-report
      ^-  (quip card _this)
      ?>  ?=([%clay-report ~] wire)
      ?.  ?=([%behn %wake *] sign-arvo)
        (on-arvo:def wire sign-arvo)
      =/  maybe-pending=(unit clay-push)  pending-clay
      ?~  maybe-pending  `this
      =/  pending=clay-push  u.maybe-pending
      ?^  error.sign-arvo
        `this(pending-clay ~)
      =/  maybe-result=(unit [ok=? message=@t])  result.pending
      ?~  maybe-result
        `this(pending-clay ~)
      =/  result=[ok=? message=@t]  u.maybe-result
      =.  repositories
        ?.  ok.result  repositories
        =/  clay-revision=(unit @ud)
          %-  mole
          |.(ud:.^(cass:clay %cw /(scot %p our.bowl)/[desk-name.pending]/(scot %da now.bowl)))
        =/  applied=repository:git
          (update-binding-success applied.pending new-oid.pending clay-revision now.bowl)
        (~(put by repositories) repository.pending applied)
      =/  webhook-cards=(list card)
        ?.  ok.result  ~
        =/  push-data=json  (push-event-json commands.pending)
        =/  clay-data=json
          %-  pairs:enjs:format
          :~  ['desk' s+desk-name.pending]
              ['commit' s+(oid-text:git-codec new-oid.pending)]
          ==
        :~  :*  %pass
                /webhook/push
                %agent
                [our.bowl %urgit]
                %poke
                %git-webhook-event
                !>([repository.pending %push push-data])
            ==
            :*  %pass
                /webhook/clay-sync
                %agent
                [our.bowl %urgit]
                %poke
                %git-webhook-event
                !>([repository.pending %clay-sync clay-data])
            ==
        ==
      =.  pending-clay  ~
      =.  peer-activities
        ?~  peer-response.pending  peer-activities
        %+  turn  peer-activities
        |=  event=peer-activity
        ?:  !=(transfer.u.peer-response.pending id.event)  event
        event(status ?:(ok.result %success %failure), message message.result, when now.bowl)
      :_  this
      ?^  peer-response.pending
        =/  =packet:git-peer
          [%result transfer.u.peer-response.pending ok.result message.result]
        =/  response-cards=(list card)
          :~  :*  %pass
                  /peer/result/(scot %uv transfer.u.peer-response.pending)
                  %agent
                  [ship.u.peer-response.pending %urgit]
                  %poke
                  %git-peer
                  !>(packet)
              ==
          ==
        (weld webhook-cards response-cards)
      ?:  api-response.pending
        =/  jon=json
          ?:  ok.result
            (pairs:enjs:format ~[['ok' b+%.y] ['commit' s+(oid-text:git-codec new-oid.pending)]])
          (pairs:enjs:format ~[['error' s+message.result]])
        %+  weld  webhook-cards
        (api-json eyre-id.pending ?:(ok.result 200 422) jon)
      %+  weld  webhook-cards
      %+  give-simple-payload:app:server  eyre-id.pending
      (receive-payload 'ok' (receive-results commands.pending ok.result message.result))
    ::
    ++  webhook
      ^-  (quip card _this)
      ?>  ?=([%webhook @ ~] wire)
      =/  delivery-id=(unit @uv)  (slaw %uv i.t.wire)
      ?~  delivery-id  `this
      =/  context=(unit webhook-flight)  (~(get by webhook-in-flight) u.delivery-id)
      ?~  context  `this
      =.  webhook-in-flight  (~(del by webhook-in-flight) u.delivery-id)
      =/  found=(unit repository:git)
        (~(get by repositories) repository.u.context)
      ?~  found  `this
      =/  status-code=@ud
        ?.  ?=([%iris %http-response *] sign-arvo)  0
        =/  response=client-response:iris  client-response.sign-arvo
        ?.  ?=(%finished -.response)  0
        status-code.response-header.response
      =/  ok=?  &((gte status-code 200) (lth status-code 300))
      =/  message=@t
        ?:  ok  'delivered'
        ?:(=(status-code 0) 'request failed' (rap 3 ~['HTTP ' (decimal status-code)]))
      =/  deliveries=(list webhook-delivery:git)
        %+  turn  webhook-deliveries.u.found
        |=  delivery=webhook-delivery:git
        ?:  !=(id.delivery u.delivery-id)  delivery
        delivery(status ?:(ok %success %failure), status-code status-code, message message)
      =/  updated=repository:git  u.found(webhook-deliveries deliveries)
      `this(repositories (~(put by repositories) repository.u.context updated))
    ::
    ++  github-response
      ^-  (quip card _this)
      ?>  ?=([%github @ ~] wire)
      =/  request-id=(unit @uv)  (slaw %uv i.t.wire)
      ?~  request-id  `this
      =/  context=(unit github-request)  (~(get by github-in-flight) u.request-id)
      ?~  context  `this
      =.  github-in-flight  (~(del by github-in-flight) u.request-id)
      =/  result-kind=github-kind
        ?:(=(%push-send kind.u.context) %push kind.u.context)
      =/  fail
        |=  message=@t
        ^-  (quip card _this)
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.n result-kind repository.u.context message]
        ?~  api-response.u.context  `this
        :_  this
        %+  give-simple-payload:app:server  u.api-response.u.context
        :*  [502 ~[['content-type' 'application/json; charset=utf-8'] ['cache-control' 'no-store']]]
            `(json-to-octs:server (pairs:enjs:format ~[['error' s+message]]))
        ==
      ?.  ?=([%iris %http-response *] sign-arvo)
        (fail 'GitHub request failed')
      =/  response=client-response:iris  client-response.sign-arvo
      ?.  ?=(%finished -.response)
        (fail 'GitHub response was incomplete')
      =/  status=@ud  status-code.response-header.response
      ?.  &((gte status 200) (lth status 300))
        =/  detail=@t
          ?~  full-file.response  ''
          (crip (scag 500 (trip `@t`q.data.u.full-file.response)))
        (fail ?:(=('' detail) (rap 3 ~['GitHub returned HTTP ' (decimal status)]) detail))
      =/  body=octs  ?~(full-file.response [0 0] data.u.full-file.response)
      |^
        ?:  =(%file-detail kind.u.context)
          file-detail
        ?:  =(%pull-diff kind.u.context)
          pull-diff
        ?:  ?|  =(%issue-detail kind.u.context)
                =(%pull-detail kind.u.context)
            ==
          forge-detail
        ?:  =(%push kind.u.context)
          push-pack
        ?:  =(%push-send kind.u.context)
          push-result
        ?:  ?|  =(%import kind.u.context)
                =(%update kind.u.context)
            ==
          import-pack
        ?:  ?|  =(%issues kind.u.context)
                =(%pulls kind.u.context)
            ==
          forge-list
        =/  message=@t
          ?:  =(%fork kind.u.context)
            'GitHub fork requested'
          'GitHub pull request opened'
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.y kind.u.context repository.u.context message]
        `this
      ++  file-detail
        ?>  =(%file-detail kind.u.context)
        ?:  (gth p.body 2.200.000)
          (fail 'GitHub file response exceeds the 1 MiB content limit')
        =/  jon=(unit json)  (de:json:html q.body)
        ?~  jon
          (fail 'GitHub returned invalid JSON')
        =/  detail=(unit json)  (file-detail-json:git-github u.jon)
        ?~  detail
          (fail 'GitHub returned an invalid or oversized file')
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.y %file-detail repository.u.context 'GitHub file loaded']
        ?~  api-response.u.context  `this
        :_  this
        %+  give-simple-payload:app:server  u.api-response.u.context
        :*  [200 ~[['content-type' 'application/json; charset=utf-8'] ['cache-control' 'no-store']]]
            `(json-to-octs:server u.detail)
        ==
      ++  pull-diff
        ?>  =(%pull-diff kind.u.context)
        ?:  (gth p.body 4.194.304)
          (fail 'GitHub pull-request diff exceeds the 4 MiB display limit')
        =/  result=json
          %-  pairs:enjs:format
          :~  ['encoding' s+'base64']
              ['size' n+(decimal p.body)]
              ['content' s+(en:base64:mimes:html body)]
          ==
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.y %pull-diff repository.u.context 'GitHub pull-request diff loaded']
        ?~  api-response.u.context  `this
        :_  this
        %+  give-simple-payload:app:server  u.api-response.u.context
        :*  [200 ~[['content-type' 'application/json; charset=utf-8'] ['cache-control' 'no-store']]]
            `(json-to-octs:server result)
        ==
      ++  forge-detail
        ?>  ?|  =(%issue-detail kind.u.context)
                =(%pull-detail kind.u.context)
            ==
        =/  jon=(unit json)  (de:json:html q.body)
        ?~  jon
          (fail 'GitHub returned invalid JSON')
        =/  detail=(unit json)
          (detail-json:git-github u.jon =(%pull-detail kind.u.context))
        ?~  detail
          (fail 'GitHub returned an invalid issue or pull-request detail')
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.y kind.u.context repository.u.context 'GitHub detail loaded']
        ?~  api-response.u.context  `this
        :_  this
        %+  give-simple-payload:app:server  u.api-response.u.context
        :*  [200 ~[['content-type' 'application/json; charset=utf-8'] ['cache-control' 'no-store']]]
            `(json-to-octs:server u.detail)
        ==
      ++  push-pack
        ?>  =(%push kind.u.context)
        =/  advertised=(unit github-refs:git-github)
          (advertised-refs:git-github body)
        ?~  advertised
          (fail 'GitHub did not advertise a usable branch')
        =/  found=(unit repository:git)  (~(get by repositories) repository.u.context)
        ?~  found
          (fail 'local repository disappeared during GitHub sync')
        =/  new=(unit oid:git)  (~(get by refs.u.found) head.u.context)
        ?~  new
          (fail 'local branch disappeared during GitHub sync')
        =/  closure=(unit (set oid:git))
          (reachable:git-graph objects.u.found (silt ~[u.new]))
        ?~  closure
          (fail 'local branch does not have a complete reachable object graph')
        =/  old=(unit oid:git)  (~(get by refs.u.advertised) head.u.context)
        ?:  ?&  ?=(^ old)
                !(~(has in u.closure) u.old)
            ==
          (fail 'GitHub branch has commits that are not local; pull before pushing')
        =/  ids=(list oid:git)  ~(tap in u.closure)
        ?:  (gth (lent ids) 25.000)
          (fail 'GitHub push exceeds the 25,000 object limit')
        =/  objects=(list object:git)
          %+  turn  ids
          |=  id=oid:git
          (~(got by objects.u.found) id)
        =/  request-body=octs
          (receive-request:git-github old u.new head.u.context objects)
        ?:  (gth p.request-body 67.108.864)
          (fail 'GitHub push exceeds the 64 MiB limit')
        =/  next-id=@uv
          `@uv`(shas %git-github-push (cat 3 eny.bowl request-count))
        =.  request-count  +(request-count)
        =/  next=github-request  u.context(kind %push-send, refs refs.u.advertised)
        =.  github-in-flight  (~(put by github-in-flight) next-id next)
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.y %.n %push repository.u.context 'uploading Git object pack']
        =/  =request:http
          :*  %'POST'
              (git-url:git-github owner.u.context remote.u.context '/git-receive-pack')
              (receive-headers:git-github github-token `'application/x-git-receive-pack-request')
              `request-body
          ==
        :_  this
        :~  [%pass /github/(scot %uv next-id) %arvo %i %request request *outbound-config:iris]
        ==
      ++  push-result
        ?>  =(%push-send kind.u.context)
        =/  result=(unit [ok=? message=@t])
          (receive-result:git-github body head.u.context)
        ?~  result
          (fail 'GitHub returned an invalid receive-pack result')
        ?.  ok.u.result
          (fail ?:(=('' message.u.result) 'GitHub rejected the branch update' message.u.result))
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.y %push repository.u.context 'GitHub branch synchronized']
        `this
      ++  import-pack
        ?>  ?|  =(%import kind.u.context)
                =(%update kind.u.context)
            ==
        ?:  =('' head.u.context)
          =/  advertised=(unit github-refs:git-github)
            (advertised-refs:git-github body)
          ?~  advertised
            (fail 'GitHub did not advertise a usable branch')
          =/  request-body=octs  (upload-request:git-github refs.u.advertised)
          =/  next-id=@uv
            `@uv`(shas %git-github-pack (cat 3 eny.bowl request-count))
          =.  request-count  +(request-count)
          =/  next=github-request
            u.context(head head.u.advertised, refs refs.u.advertised)
          =.  github-in-flight  (~(put by github-in-flight) next-id next)
          =.  github-results
            %+  ~(put by github-results)
              job.u.context
            [%.y %.n kind.u.context repository.u.context 'downloading Git object pack']
          =/  =request:http
            :*  %'POST'
                (git-url:git-github owner.u.context remote.u.context '/git-upload-pack')
                (git-headers:git-github github-token `'application/x-git-upload-pack-request')
                `request-body
            ==
          :_  this
          :~  [%pass /github/(scot %uv next-id) %arvo %i %request request *outbound-config:iris]
          ==
        ?:  (gth p.body 67.108.864)
          (fail 'GitHub pack exceeds the 64 MiB import limit')
        =/  pack=(unit octs)  (upload-pack:git-github body)
        ?~  pack
          (fail 'GitHub upload-pack response did not contain a pack')
        =/  object-count=(unit @)  (uint-be-at:git-pack-decode u.pack 8 4)
        ?~  object-count
          (fail 'GitHub pack header is incomplete')
        ?:  (gth u.object-count 25.000)
          (fail 'GitHub pack exceeds the 25,000 object import limit')
        =/  existing=(unit repository:git)  (~(get by repositories) repository.u.context)
        =/  bases=(map oid:git object:git)  ?~(existing ~ objects.u.existing)
        =/  decoded=(unit decoded-pack:git-pack-decode)
          (decode-pack-with:git-pack-decode u.pack bases)
        ?~  decoded
          (fail 'GitHub pack failed checksum, compression, delta, or object validation')
        =/  combined=(map oid:git object:git)  bases
        =/  staged=(list [oid:git object:git])  ~(tap by objects.u.decoded)
        =.  combined
          |-
          ?~  staged  combined
          =.  combined  (~(put by combined) -.i.staged +.i.staged)
          $(staged t.staged)
        =/  refs-valid=?
          %+  levy  ~(tap by refs.u.context)
          |=  entry=[@t oid:git]
          =/  closure=(unit (set oid:git))
            (reachable:git-graph combined (silt ~[+.entry]))
          ?=(^ closure)
        ?.  refs-valid
          (fail 'GitHub pack did not contain a complete reachable object graph')
        =/  fast-forward=?
          ?~  existing  %.y
          %+  levy  ~(tap by refs.u.context)
          |=  entry=[@t oid:git]
          =/  old=(unit oid:git)  (~(get by refs.u.existing) -.entry)
          ?~  old  %.y
          ?:  =(u.old +.entry)  %.y
          =/  closure=(unit (set oid:git))
            (reachable:git-graph combined (silt ~[+.entry]))
          ?&  ?=(^ closure)
              (~(has in u.closure) u.old)
          ==
        ?.  fast-forward
          (fail 'GitHub and local branches have diverged; push or reconcile before pulling')
        |^
          install-import
        ++  install-import
          =/  next-refs=(map @t oid:git)
            ?~  existing  refs.u.context
            =/  working=(map @t oid:git)  refs.u.existing
            =/  incoming=(list [@t oid:git])  ~(tap by refs.u.context)
            |-
            ?~  incoming  working
            =.  working  (~(put by working) -.i.incoming +.i.incoming)
            $(incoming t.incoming)
          =/  origin=github-origin:git  [owner.u.context remote.u.context]
          =/  repo=repository:git
            ?~  existing
              :*  our.bowl
                  public-read.u.context
                  ''
                  head.u.context
                  next-refs
                  ~
                  combined
                  (silt ~[our.bowl])
                  ~
                  ~
                  ~
                  ~
                  ~
                  ~
                  ~
                  `origin
                  ~
                  ~
                  ~
                  ~
                  ~
                  ~
                  ~
                  ~
                  ~
                  default-notification-events
                  ~
              ==
            u.existing(head head.u.context, refs next-refs, objects combined, github-origin `origin)
          =.  repositories  (~(put by repositories) repository.u.context repo)
          =.  github-results
            %+  ~(put by github-results)
              job.u.context
            [%.n %.y kind.u.context repository.u.context 'GitHub repository synchronized']
          `this
        --
      ::
      ++  forge-list
        ?>  ?|  =(%issues kind.u.context)
                =(%pulls kind.u.context)
            ==
        =/  jon=(unit json)  (de:json:html q.body)
        ?~  jon
          (fail 'GitHub returned invalid JSON')
        =/  items=(unit (list forge-item:git))
          (forge-items:git-github u.jon =(%pulls kind.u.context))
        ?~  items
          (fail 'GitHub returned an invalid issue or pull-request list')
        =/  found=(unit repository:git)  (~(get by repositories) repository.u.context)
        ?~  found
          (fail 'local repository disappeared during GitHub sync')
        =/  previous=(list forge-item:git)
          ?:(=(%issues kind.u.context) github-issues.u.found github-pulls.u.found)
        =/  merged-items=(list forge-item:git)
          ?:  =(1 metadata-page.u.context)  u.items
          =/  novel=(list forge-item:git)
            %+  skim  u.items
            |=  item=forge-item:git
            =/  duplicate=(list forge-item:git)
              (skim previous |=(prior=forge-item:git =(number.prior number.item)))
            ?=(~ duplicate)
          (scag 500 (weld previous novel))
        =/  repo=repository:git
          ?:  =(%issues kind.u.context)
            u.found(github-issues merged-items)
          u.found(github-pulls merged-items)
        =.  repositories  (~(put by repositories) repository.u.context repo)
        =/  received=@ud  ?:(?=([%a *] u.jon) (lent p.u.jon) 0)
        =/  label=@t
          %+  rap  3
          :~  ?:
                =(%issues kind.u.context)
                'GitHub issues synchronized · '
              'GitHub pull requests synchronized · '
              (decimal received)
              ' received'
          ==
        =.  github-results
          %+  ~(put by github-results)
            job.u.context
          [%.n %.y kind.u.context repository.u.context label]
        `this
      --
    ::
    ++  lfs-delete-response
      ^-  (quip card _this)
      ?>  ?=([%lfs-delete @ ~] wire)
      =/  request-id=(unit @uv)  (slaw %uv i.t.wire)
      ?~  request-id  `this
      =/  context=(unit lfs-delete)  (~(get by lfs-deletes) u.request-id)
      ?~  context  `this
      =.  lfs-deletes  (~(del by lfs-deletes) u.request-id)
      ?.  ?=([%iris %http-response *] sign-arvo)  `this
      =/  response=client-response:iris  client-response.sign-arvo
      ?.  ?=(%finished -.response)  `this
      =/  status=@ud  status-code.response-header.response
      ?.  |(&((gte status 200) (lth status 300)) =(404 status))  `this
      =/  found=(unit repository:git)
        (~(get by repositories) repository.u.context)
      ?~  found  `this
      =/  updated=repository:git
        u.found(lfs-objects (~(del by lfs-objects.u.found) oid.u.context))
      `this(repositories (~(put by repositories) repository.u.context updated))
    ::
    ++  lfs-verify-response
      ^-  (quip card _this)
      ?>  ?=([%iris @ ~] wire)
      =/  request-id=(unit @uv)  (slaw %uv i.t.wire)
      ?~  request-id  `this
      =/  context=(unit lfs-request)  (~(get by in-flight) u.request-id)
      ?~  context  `this
      =.  in-flight  (~(del by in-flight) u.request-id)
      ?.  ?=([%iris %http-response *] sign-arvo)
        :_  this
        (error-cards eyre-id.u.context 502 'object storage request failed')
      =/  response=client-response:iris  client-response.sign-arvo
      ?.  ?=(%finished -.response)
        :_  this
        (error-cards eyre-id.u.context 502 'object storage response was incomplete')
      =/  status=@ud  status-code.response-header.response
      ?.  &((gte status 200) (lth status 300))
        :_  this
        (error-cards eyre-id.u.context 422 'uploaded object was not found in object storage')
      =/  content-length=(unit @ud)
        =/  raw=(unit @t)  (get-header:http 'content-length' headers.response-header.response)
        ?~(raw ~ (slaw %ud u.raw))
      ?:  &(?=(^ content-length) !=(u.content-length size.upload.u.context))
        :_  this
        (error-cards eyre-id.u.context 422 'stored object size does not match')
      =/  found=(unit repository:git)  (~(get by repositories) repository.u.context)
      ?~  found
        :_  this
        (error-cards eyre-id.u.context 404 'repository not found')
      =/  object=lfs-object:git  [size.upload.u.context object-key.upload.u.context]
      =/  repo=repository:git
        u.found(lfs-objects (~(put by lfs-objects.u.found) oid.u.context object))
      =.  repo  repo(lfs-uploads (~(del by lfs-uploads.repo) oid.u.context))
      =.  repositories  (~(put by repositories) repository.u.context repo)
      :_  this
      %+  give-simple-payload:app:server  eyre-id.u.context
      =/  jon=json  (pairs:enjs:format ~)
      :*  [200 ~[['content-type' 'application/vnd.git-lfs+json'] ['cache-control' 'no-store']]]
          `(json-to-octs:server jon)
      ==
    --
  =/  after=peer-ui-state
    =>  +.result
    [peer-activities notification-activities peer-results peer-receiving peer-outgoing]
  [(peer-ui-notify before after -.result) +.result]
++  on-fail  on-fail:def
--
