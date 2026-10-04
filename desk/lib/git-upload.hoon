::  git-upload: Git fetch negotiation and pack responses
::
/-  git
/+  git-codec, git-graph, git-pack, git-protocol
|%
++  upload-response
  |=  [repo=repository:git request=upload-request:git use-v2=?]
  ^-  (each octs [status=@ud message=@t])
  ?:  =(0 (lent ~(tap in wants.request)))
    [%| 400 'upload-pack request has no wants\0a']
  ?.  (levy ~(tap in wants.request) |=(oid=oid:git (~(has by objects.repo) oid)))
    [%| 400 'requested object not found\0a']
  =/  wanted-full=(unit (set oid:git))
    (reachable:git-graph objects.repo wants.request)
  ?~  wanted-full
    [%| 500 'repository graph is incomplete\0a']
  =/  advertised-roots=(set oid:git)
    %-  silt
    %+  turn  ~(tap by refs.repo)
    |=  entry=[@t oid:git]
    +.entry
  =/  advertised-closure=(unit (set oid:git))
    (reachable:git-graph objects.repo advertised-roots)
  ?~  advertised-closure
    [%| 500 'advertised repository graph is incomplete\0a']
  ?.  (levy ~(tap in wants.request) |=(oid=oid:git (~(has in u.advertised-closure) oid)))
    [%| 400 'requested object is not reachable from an advertised ref\0a']
  =/  effective-shallow=(set oid:git)
    (~(int in shallow.request) u.wanted-full)
  =/  unshallow-all=?
    ?^  depth.request
      =(2.147.483.647 u.depth.request)
    %.n
  =/  depth-result=(unit shallow-result:git-graph)
    (shallow-closure repo request u.wanted-full effective-shallow unshallow-all)
  =/  wanted-closure=(unit (set oid:git))
    ?~(depth.request wanted-full ?~(depth-result ~ `reachable.u.depth-result))
  ?~  wanted-closure
    [%| 500 'shallow repository graph is incomplete\0a']
  =/  common=(set oid:git)
    (~(int in haves.request) u.advertised-closure)
  =/  common-closure=(unit (set oid:git))
    ?:  ?=(~ effective-shallow)
      (reachable:git-graph objects.repo common)
    (reachable-stopping:git-graph objects.repo common effective-shallow)
  ?~  common-closure
    [%| 500 'common repository graph is incomplete\0a']
  =/  transfer=(set oid:git)
    (~(dif in u.wanted-closure) u.common-closure)
  =/  filtered-transfer=(set oid:git)
    (filter-transfer repo request transfer)
  =/  common-list=(list oid:git)  ~(tap in common)
  =/  shallow-packets=(list octs)
    (shallow-packets request depth-result effective-shallow unshallow-all)
  =/  shallow-lines=octs  (join-all:git-codec shallow-packets)
  =/  shallow-status=octs
    ?~  shallow-packets  [0 0]
    (join-all:git-codec ~[shallow-lines (en-pkt:git-codec [%flush ~])])
  =/  status=octs
    ?~  common-list
      (en-pkt:git-codec [%data (text:git-codec 'NAK\0a')])
    =/  line=@t
      (rap 3 ~['ACK ' (oid-text:git-codec i.common-list) '\0a'])
    (en-pkt:git-codec [%data (text:git-codec line)])
  ::  Without multi_ack, a request ending in flush receives negotiation
  ::  status only.  The pack begins after the client sends done.
  ?.  done.request
    =/  negotiation=octs
      ?:  use-v2
        %-  join-all:git-codec
        :~  (en-pkt:git-codec [%data (text:git-codec 'acknowledgments\0a')])
            status
            (en-pkt:git-codec [%flush ~])
        ==
      ?~(depth.request status shallow-status)
    [%& negotiation]
  =/  objects=(list object:git)
    %+  turn  ~(tap in filtered-transfer)
    |=(oid=oid:git (need (~(get by objects.repo) oid)))
  =/  pack=octs  (encode-pack:git-pack objects)
  =/  final-shallow=octs
    ?:  ?&  ?=(^ depth.request)
            |(unshallow-all deepen-relative.request ?=(~ shallow.request))
        ==
      shallow-status
    [0 0]
  =/  response=octs
    ?:  use-v2
      =/  shallow-section=octs
        ?:  =(0 p.final-shallow)  [0 0]
        %-  join-all:git-codec
        :~  (en-pkt:git-codec [%data (text:git-codec 'shallow-info\0a')])
            shallow-lines
            (en-pkt:git-codec [%delim ~])
        ==
      %-  join-all:git-codec
      :~  shallow-section
          (en-pkt:git-codec [%data (text:git-codec 'packfile\0a')])
          (v2-sideband-pack:git-protocol pack)
          (en-pkt:git-codec [%flush ~])
      ==
    (join-all:git-codec ~[final-shallow status pack])
  [%& response]
++  shallow-closure
  |=  [repo=repository:git request=upload-request:git wanted-full=(set oid:git) effective-shallow=(set oid:git) unshallow-all=?]
  ^-  (unit shallow-result:git-graph)
  ?~  depth.request  ~
  ?:  unshallow-all
    `[wanted-full ~]
  ?:  ?=(~ effective-shallow)
    (reachable-depth:git-graph objects.repo wants.request u.depth.request)
  ?.  deepen-relative.request
    =/  stopped=(unit (set oid:git))
      (reachable-stopping:git-graph objects.repo wants.request effective-shallow)
    ?~(stopped ~ `[u.stopped effective-shallow])
  =/  stopped=(unit (set oid:git))
    (reachable-stopping:git-graph objects.repo wants.request effective-shallow)
  ?~  stopped  ~
  =/  extension=(unit shallow-result:git-graph)
    (reachable-depth:git-graph objects.repo effective-shallow +(u.depth.request))
  ?~  extension  ~
  `[(~(uni in u.stopped) reachable.u.extension) boundaries.u.extension]
++  filter-transfer
  |=  [repo=repository:git request=upload-request:git transfer=(set oid:git)]
  ^-  (set oid:git)
  ?~  filter.request  transfer
  =/  traversed=(set oid:git)
    %-  silt
    %+  skim  ~(tap in transfer)
    |=  oid=oid:git
    =/  =object:git  (need (~(get by objects.repo) oid))
    ?-  u.filter.request
        %blob-none
      !=(%blob kind.object)
    ::
        [%blob-limit *]
      ?|  !=(%blob kind.object)
          (lte p.data.object limit.u.filter.request)
      ==
    ==
  ::  A promisor fetch repeats its filter while directly wanting an
  ::  omitted blob.  Direct wants must survive traversal filtering.
  (~(uni in traversed) (~(int in wants.request) transfer))
++  shallow-packets
  |=  [request=upload-request:git depth-result=(unit shallow-result:git-graph) effective-shallow=(set oid:git) unshallow-all=?]
  ^-  (list octs)
  ?~  depth.request  ~
  ?~  depth-result  ~
  =/  packets=(list octs)
    %+  turn  ~(tap in boundaries.u.depth-result)
    |=  oid=oid:git
    %-  en-pkt:git-codec
    [%data (text:git-codec (rap 3 ~['shallow ' (oid-text:git-codec oid) '\0a']))]
  =?  packets  &(|(deepen-relative.request unshallow-all) !=(~ effective-shallow))
    %+  weld  packets
    %+  turn  ~(tap in effective-shallow)
    |=  oid=oid:git
    %-  en-pkt:git-codec
    [%data (text:git-codec (rap 3 ~['unshallow ' (oid-text:git-codec oid) '\0a']))]
  packets
--
