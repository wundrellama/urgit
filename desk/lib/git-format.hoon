::  git-format: Git metadata JSON, dates, and notification defaults
::
/-  git, git-peer
/+  git-codec
|%
++  binding-json
  |=  binding=(unit desk-binding:git)
  ^-  json
  ?~  binding
    %-  pairs:enjs:format
    ~[['bound' b+%.n]]
  %-  pairs:enjs:format
  :~  ['bound' b+%.y]
      ['desk' s+desk-name.u.binding]
      ['branch' s+branch.u.binding]
      ['lastClay' s+?~(last-clay.u.binding '' (scot %ud u.last-clay.u.binding))]
      ['lastGit' s+?~(last-git.u.binding '' (oid-text:git-codec u.last-git.u.binding))]
      :-  'history'
      :-  %a
      %+  turn  history.u.binding
      |=  link=clay-link:git
      %-  pairs:enjs:format
      :~  ['clayRevision' n+(decimal clay-revision.link)]
          ['commit' s+(oid-text:git-codec commit.link)]
          ['direction' s+direction.link]
          ['when' s+(scot %da when.link)]
      ==
  ==
::
++  decimal
  |=  value=@ud
  ^-  @t
  (crip ((d-co:co 1) value))
::
++  two-digits
  |=  value=@ud
  ^-  tape
  ?:  (lth value 10)
    (weld "0" (a-co:co value))
  (a-co:co value)
::
++  rfc3339
  |=  when=@da
  ^-  @t
  =/  date  (yore when)
  %-  crip
  %+  weld  (a-co:co y.date)
  %+  weld  "-"
  %+  weld  (two-digits m.date)
  %+  weld  "-"
  %+  weld  (two-digits d.t.date)
  %+  weld  "T"
  %+  weld  (two-digits h.t.date)
  %+  weld  ":"
  %+  weld  (two-digits m.t.date)
  %+  weld  ":"
  %+  weld  (two-digits s.t.date)
  "Z"
::
++  lfs-lock-json
  |=  lock=lfs-lock:git
  ^-  json
  %-  pairs:enjs:format
  :~  ['id' s+(decimal id.lock)]
      ['path' s+path.lock]
      ['locked_at' s+(rfc3339 locked-at.lock)]
      ['owner' (pairs:enjs:format ~[['name' s+owner.lock]])]
  ==
::
++  review-comment-json
  |=  comment=review-comment:git
  ^-  json
  %-  pairs:enjs:format
  :~  ['id' n+(decimal id.comment)]
      ['author' s+(scot %p author.comment)]
      ['body' s+body.comment]
      ['created' s+(scot %da created.comment)]
      ['path' s+?~(path.comment '' u.path.comment)]
      ['line' n+(decimal ?~(line.comment 0 u.line.comment))]
      ['side' s+?~(side.comment '' u.side.comment)]
      ['resolved' b+resolved.comment]
  ==
::
++  issue-comment-json
  |=  comment=issue-comment:git
  ^-  json
  %-  pairs:enjs:format
  :~  ['id' n+(decimal id.comment)]
      ['author' s+(scot %p author.comment)]
      ['body' s+body.comment]
      ['created' s+(scot %da created.comment)]
  ==
::
++  native-issue-json
  |=  [issue=native-issue:git include-comments=?]
  ^-  json
  =/  labels-json=(list json)
    (turn ~(tap in labels.issue) |=(label=@t s+label))
  =/  assignees-json=(list json)
    (turn ~(tap in assignees.issue) |=(assignee=@p s+(scot %p assignee)))
  %-  pairs:enjs:format
  :~  ['number' n+(decimal number.issue)]
      ['author' s+(scot %p author.issue)]
      ['title' s+title.issue]
      ['body' s+?:(include-comments body.issue '')]
      ['state' s+state.issue]
      ['labels' [%a labels-json]]
      ['assignees' [%a assignees-json]]
      ['created' s+(scot %da created.issue)]
      ['updated' s+(scot %da updated.issue)]
      ['commentCount' n+(decimal (lent comments.issue))]
      ['comments' [%a ?:(include-comments (turn comments.issue issue-comment-json) ~)]]
  ==
::
++  release-json
  |=  [release=release:git include-notes=?]
  ^-  json
  %-  pairs:enjs:format
  :~  ['tag' s+tag.release]
      ['title' s+title.release]
      ['notes' s+?:(include-notes notes.release '')]
      ['author' s+(scot %p author.release)]
      ['created' s+(scot %da created.release)]
  ==
::
++  webhook-json
  |=  hook=webhook:git
  ^-  json
  %-  pairs:enjs:format
  :~  ['id' n+(decimal id.hook)]
      ['url' s+url.hook]
      ['enabled' b+enabled.hook]
      ['events' [%a (turn ~(tap in events.hook) |=(event=webhook-event:git s+event))]]
  ==
::
++  webhook-delivery-json
  |=  delivery=webhook-delivery:git
  ^-  json
  %-  pairs:enjs:format
  :~  ['id' s+(scot %uv id.delivery)]
      ['hook' n+(decimal hook.delivery)]
      ['event' s+event.delivery]
      ['status' s+status.delivery]
      ['statusCode' n+(decimal status-code.delivery)]
      ['message' s+message.delivery]
      ['created' s+(scot %da created.delivery)]
  ==
::
++  upstream-update-json
  |=  update=upstream-update:git
  ^-  json
  %-  pairs:enjs:format
  :~  ['id' s+(scot %uv id.update)]
      ['source' s+source.update]
      ['ref' s+ref.update]
      ['before' s+before.update]
      ['after' s+after.update]
      ['received' s+(scot %da received.update)]
  ==
::
++  dedupe-upstream-updates
  |=  updates=(list upstream-update:git)
  ^-  (list upstream-update:git)
  =/  seen=(set @t)  ~
  =/  kept=(list upstream-update:git)  ~
  |-
  ?~  updates  (flop kept)
  ?:  (~(has in seen) ref.i.updates)
    $(updates t.updates)
  %=  $
    updates  t.updates
    seen  (~(put in seen) ref.i.updates)
    kept  [i.updates kept]
  ==
::
++  default-notification-events
  ^-  (set notification-event:git)
  =/  events=(set notification-event:git)  ~
  =.  events  (~(put in events) %issue)
  =.  events  (~(put in events) %issue-comment)
  =.  events  (~(put in events) %pull-request)
  (~(put in events) %pull-comment)
::
++  group-policy-json
  |=  policy=(unit group-policy:git)
  ^-  json
  ?~  policy  ~
  %-  pairs:enjs:format
  :~  ['host' s+(scot %p host.group.u.policy)]
      ['group' s+name.group.u.policy]
      ['base' s+base.u.policy]
      ['roles' [%o (~(run by roles.u.policy) |=(cap=capability:git s+cap))]]
  ==
::
::
++  starts-with
  |=  [prefix=@t value=@t]
  ^-  ?
  =/  pre=tape  (trip prefix)
  =/  val=tape  (trip value)
  ?.  (lte (lent pre) (lent val))  %.n
  =(pre (scag (lent pre) val))
::
++  tang-text
  |=  =tang
  ^-  @t
  =/  lines=(list tape)
    %-  zing
    %+  turn  tang
    |=  =tank
    (wash [0 120] tank)
  =/  join-lines
    |=  [remaining=(list tape) out=tape]
    ^-  tape
    ?~  remaining  out
    =/  next=tape
      ?~  out  i.remaining
      :(weld out " | " i.remaining)
    $(remaining t.remaining, out next)
  (crip (scag 60.000 (join-lines lines ~)))
::
--
