::  git-lfs: LFS pointers, reachability, and object-storage actions
::
/-  git
/+  server, git-codec, git-graph, git-storage, *git-format, *git-http
|%
+$  lfs-spec  [oid=@t size=@ud]
::
++  valid-lfs-oid
  |=  oid=@t
  ^-  ?
  =/  chars=tape  (trip oid)
  ?.  =(64 (lent chars))  %.n
  (levy chars |=(char=@tD |(&((gte char '0') (lte char '9')) &((gte char 'a') (lte char 'f')))))
::
++  lfs-pointer-oid
  |=  data=octs
  ^-  (unit @t)
  ?:  (gth p.data 1.024)  ~
  =/  chars=tape  (trip q.data)
  =/  marker=tape  "oid sha256:"
  =/  location=(unit @ud)  (find marker chars)
  ?~  location  ~
  =/  start=@ud  (add u.location (lent marker))
  =/  candidate=tape  (scag 64 (slag start chars))
  ?.  =(64 (lent candidate))  ~
  =/  oid=@t  (crip candidate)
  ?:  (valid-lfs-oid oid)
    `oid
  ~
::
++  referenced-lfs
  |=  repo=repository:git
  ^-  (unit (set @t))
  =/  roots=(set oid:git)
    (silt (turn ~(tap by refs.repo) |=(entry=[@t oid:git] +.entry)))
  =/  closure=(unit (set oid:git))
    (reachable:git-graph objects.repo roots)
  ?~  closure  ~
  =/  ids=(list oid:git)  ~(tap in u.closure)
  =/  live=(set @t)  ~
  |-
  ?~  ids  `live
  =/  found=(unit object:git)  (~(get by objects.repo) i.ids)
  ?.  &(?=(^ found) =(%blob kind.u.found))
    $(ids t.ids)
  =/  pointer=(unit @t)  (lfs-pointer-oid data.u.found)
  ?.  &(?=(^ pointer) (~(has by lfs-objects.repo) u.pointer))
    $(ids t.ids)
  $(ids t.ids, live (~(put in live) u.pointer))
::
++  lfs-gc-json
  |=  repo=repository:git
  ^-  (unit json)
  =/  live=(unit (set @t))  (referenced-lfs repo)
  ?~  live  ~
  =/  candidates=(list [@t lfs-object:git])
    %+  skim  ~(tap by lfs-objects.repo)
    |=  entry=[@t lfs-object:git]
    !(~(has in u.live) -.entry)
  =/  remaining=(list [@t lfs-object:git])  candidates
  =/  bytes=@ud  0
  =.  bytes
    |-
    ?~  remaining  bytes
    $(remaining t.remaining, bytes (add bytes size.+.i.remaining))
  =/  items=(list json)
    %+  turn  candidates
    |=  entry=[@t lfs-object:git]
    =/  oid=@t  -.entry
    =/  object=lfs-object:git  +.entry
    (pairs:enjs:format ~[['oid' s+oid] ['size' n+(decimal size.object)]])
  =/  result=json
    %-  pairs:enjs:format
    :~  ['candidateCount' n+(decimal (lent candidates))]
        ['candidateBytes' n+(decimal bytes)]
        ['candidates' [%a (scag 100 items)]]
        ['truncated' b+(gth (lent candidates) 100)]
    ==
  `result
::
++  parse-lfs-specs
  |=  jon=json
  ^-  (unit (list lfs-spec))
  =/  value=(unit json)  (json-at 'objects' jon)
  ?~  value  ~
  ?.  ?=([%a *] u.value)  ~
  =/  items=(list json)  p.u.value
  =/  out=(list lfs-spec)  ~
  |-
  ?~  items  `(flop out)
  =/  oid=(unit @t)  (string-at 'oid' i.items)
  =/  size=(unit @ud)  (nat-at 'size' i.items)
  ?.  &(?=(^ oid) ?=(^ size) (valid-lfs-oid u.oid))  ~
  $(items t.items, out [[u.oid u.size] out])
::
++  json-payload
  |=  [status=@ud jon=json]
  ^-  simple-payload:http
  :_  `(json-to-octs:server jon)
  :-  status
  :~  ['content-type' 'application/vnd.git-lfs+json']
      ['cache-control' 'no-store']
  ==
::
++  lfs-error
  |=  [status=@ud message=@t]
  ^-  simple-payload:http
  (json-payload status (pairs:enjs:format ~[['message' s+message]]))
::
++  lfs-principal
  |=  [who=@p req=inbound-request:eyre]
  ^-  (unit @t)
  ?:  authenticated.req  `(scot %p who)
  =/  header=(unit @t)  (get-header:http 'authorization' header-list.request.req)
  ?~  header  ~
  ?.  (starts-with 'Basic ' u.header)  ~
  =/  decoded=(unit octs)
    (de:base64:mimes:html (crip (slag 6 (trip u.header))))
  ?~  decoded  ~
  =/  credentials=tape  (trip q.u.decoded)
  =/  colon=(unit @ud)  (find ":" credentials)
  ?~  colon  ~
  =/  user=@t  (crip (scag u.colon credentials))
  ?:(=('' user) `'git' `user)
::
++  object-key
  |=  [who=@p repository=@t oid=@t]
  ^-  @t
  (rap 3 ~['git-lfs/' (scot %p who) '/' repository '/' oid])
::
++  headers-json
  |=  headers=(list [@t @t])
  ^-  json
  (pairs:enjs:format (turn headers |=([key=@t value=@t] [key s+value])))
::
++  action-json
  |=  signed=signed-request:git-storage
  ^-  json
  %-  pairs:enjs:format
  :~  ['href' s+url.signed]
      ['header' (headers-json headers.signed)]
      ['expires_in' n+'600']
  ==
::
++  verify-action-json
  |=  [req=inbound-request:eyre repository=@t oid=@t]
  ^-  json
  =/  href=@t
    %+  rap  3
    :~  (public-base req)  '/git/'  repository
        '/info/lfs/objects/'  oid  '/verify'
    ==
  %-  pairs:enjs:format
  ~[['href' s+href] ['expires_in' n+'600']]
::
--
