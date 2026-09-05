::  Peer catalog entries, their two wire shapes, and the requester's
::  bookkeeping while it asks.
::
::    pure.  older peers send and read %catalog, whose entries have no
::    via; newer ones also read %catalog-via, whose entries do.  +pack
::    picks the older packet whenever it can say everything, so a newer
::    host stays legible to older peers unless a group granted a read.
::
::    a catalog request is a poke, and a poke to a ship that is offline
::    sits in ames until that ship acks or nacks it: nothing the agent
::    does calls it back, and an agent cannot cork a poke.  so at most
::    one request rides to a ship at a time.  the ledger below holds the
::    request still unacked per ship; while a ship is in it, a fan-out
::    records the member as pending instead of asking, and the ack or
::    nack that arrives when the ship is back clears the way.  the hold
::    lapses after an hour, because a kernel is not relied on to ack or
::    nack a plea it flubbed: after that the next fan-out asks afresh and
::    a new hold starts, at most one queued request per member per hour.
::
/-  git-peer
|%
::  how a discovery stands: still waiting on the ship, answered, nacked
::  (the ship does not run urgit), timed out (unreachable, or reachable
::  but silent), or not asked because an earlier request is in flight.
::  the bunt is %waiting, what a request is until something happens.
::  a pending entry carries when the request it waits behind was sent
::
+$  status  ?(%answered %no-urgit %unreachable %pending %waiting)
+$  discovery
  $:  peer=ship
      active=?
      ok=?
      message=@t
      repositories=(list catalog-repository:git-peer)
      group=(unit [host=@p name=@tas])
      =status
      held-since=(unit @da)
  ==
::  the request still unacked per ship, and when it was sent
::
+$  ledger  (map ship [request=@uv sent=@da])
::  how long a request holds its ship before a fan-out asks afresh
::
++  hold-expiry  ~h1
::
++  status-text
  |=  =status
  ^-  @t
  ?-  status
    %answered     'answered'
    %no-urgit     'no-urgit'
    %unreachable  'unreachable'
    %pending      'pending'
    %waiting      'waiting'
  ==
::  what a fan-out does for one member: reuse the discovery already asking
::  it on this group's behalf, hold if a request to it is still unacked
::  and younger than the expiry, else ask.  a hold says when the request
::  it waits behind was sent
::
++  plan
  |=  [=ledger active=(map ship @uv) peer=ship now=@da]
  ^-  $%([%reuse request=@uv] [%hold since=@da] [%ask ~])
  =/  existing=(unit @uv)  (~(get by active) peer)
  ?^  existing  [%reuse u.existing]
  =/  riding=(unit [request=@uv sent=@da])  (~(get by ledger) peer)
  ?~  riding  [%ask ~]
  ?:  (lapsed sent.u.riding now)  [%ask ~]
  [%hold sent.u.riding]
::  a hold sent an hour or more ago has lapsed; a clock behind the send
::  time has not
::
++  lapsed
  |=  [sent=@da now=@da]
  ^-  ?
  ?:  (lth now sent)  %.n
  (gte (sub now sent) hold-expiry)
::
++  waiting
  |=  [peer=ship group=(unit [host=@p name=@tas])]
  ^-  discovery
  [peer %.y %.n 'contacting peer' ~ group %waiting ~]
::
++  held
  |=  [peer=ship group=(unit [host=@p name=@tas]) since=@da]
  ^-  discovery
  [peer %.n %.n 'an earlier request to this ship is still in flight' ~ group %pending `since]
::  a time as the browser reads it, ISO 8601 in UTC: 2026-09-03T00:04:30Z
::
++  iso
  |=  when=@da
  ^-  @t
  =/  date  (yore when)
  =/  two  |=(n=@ud (crip ((d-co:co 2) n)))
  %+  rap  3
  :~  (crip ((d-co:co 4) y.date))  '-'  (two m.date)  '-'  (two d.t.date)
      'T'  (two h.t.date)  ':'  (two m.t.date)  ':'  (two s.t.date)  'Z'
  ==
::
++  answered
  |=  [=discovery repositories=(list catalog-repository:git-peer)]
  ^-  ^discovery
  discovery(active %.n, ok %.y, message 'complete', repositories repositories, status %answered)
::  the peer answered, but with an error of its own: it was reached and
::  runs urgit, so it counts as answered and its message stands
::
++  refused
  |=  [=discovery message=@t]
  ^-  ^discovery
  discovery(active %.n, ok %.n, message message, status %answered)
::
++  nacked
  |=  =discovery
  ^-  ^discovery
  discovery(active %.n, ok %.n, message 'peer does not run urgit', status %no-urgit)
::
++  timed-out
  |=  =discovery
  ^-  ^discovery
  discovery(active %.n, ok %.n, message 'peer discovery timed out', status %unreachable)
::  a request sent to a ship rides until its ack or nack settles it; the
::  timer does not touch the ledger, because the poke is still out there
::
++  sent
  |=  [=ledger peer=ship request=@uv now=@da]
  ^-  ^ledger
  (~(put by ledger) peer [request now])
::
++  settled
  |=  [=ledger request=@uv]
  ^-  ^ledger
  %-  ~(rep by ledger)
  |=  [entry=[peer=ship request=@uv sent=@da] acc=^ledger]
  ?:  =(request request.entry)  acc
  (~(put by acc) peer.entry [request.entry sent.entry])
++  legacy
  |=  repo=catalog-repository:git-peer
  ^-  catalog-repository-legacy:git-peer
  [name.repo head.repo refs.repo objects.repo writable.repo]
::
++  from-legacy
  |=  repo=catalog-repository-legacy:git-peer
  ^-  catalog-repository:git-peer
  [name.repo head.repo refs.repo objects.repo writable.repo ~]
::
++  pack
  |=  [request=@uv answer=(list catalog-repository:git-peer)]
  ^-  packet:git-peer
  ?.  (levy answer |=(repo=catalog-repository:git-peer ?=(~ via.repo)))
    [%catalog-via request answer]
  [%catalog request (turn answer legacy)]
::
++  unpack
  |=  packet=packet:git-peer
  ^-  (unit catalog:git-peer)
  ?+  -.packet  ~
    %catalog      `[request.catalog.packet (turn repositories.catalog.packet from-legacy)]
    %catalog-via  `catalog.packet
  ==
--
