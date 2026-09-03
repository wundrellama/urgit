::  Peer catalog wire-shape vectors, and the requester's in-flight gate.
::
::    a catalog entry with and without via, packed the way a host sends
::    it, jammed and cued as ames carries it, read back through the
::    %git-peer mark's noun grab as the receiving gall applies it, and
::    unpacked the way the requester reads it.  then the ledger: one
::    request rides to a ship at a time, a second fan-out holds a member
::    whose first request is unacked, the hold lapses after an hour, the
::    ack clears it, and a nack clears it and settles the discovery as
::    no-urgit.
::
/-  git-peer
/+  git-catalog
/=  git-peer-mark  /mar/git-peer
:-  %say
|=  *
:-  %noun
=/  request=@uv  0v1.23456
=/  group=[host=@p name=@tas]  [~sun %verify]
=/  plain=catalog-repository:git-peer  ['tools' 'refs/heads/main' 3 41 %.n ~]
=/  shared=catalog-repository:git-peer  ['secret' 'refs/heads/main' 2 17 %.y `group]
=/  legacy-entry=catalog-repository-legacy:git-peer  ['tools' 'refs/heads/main' 3 41 %.n]
::  what the receiving gall does with the noun ames delivered
::
=/  receive
  |=  packet=packet:git-peer
  ^-  packet:git-peer
  (noun:grab:git-peer-mark (cue (jam packet)))
::  nothing granted through a group: the older %catalog shape, entry by entry
::
=/  old=packet:git-peer  (pack:git-catalog request ~[plain])
?>  ?=(%catalog -.old)
?>  =(request request.catalog.old)
?>  =(~[legacy-entry] repositories.catalog.old)
?>  =(old (receive old))
?>  =([~ request ~[plain]] (unpack:git-catalog (receive old)))
::  one entry granted through a group: %catalog-via carries via for all
::
=/  new=packet:git-peer  (pack:git-catalog request ~[plain shared])
?>  ?=(%catalog-via -.new)
?>  =(new (receive new))
?>  =([~ request ~[plain shared]] (unpack:git-catalog (receive new)))
=/  read=(unit catalog:git-peer)  (unpack:git-catalog (receive new))
?>  ?=(^ read)
?>  =(~[~ `group] (turn repositories.u.read |=(repo=catalog-repository:git-peer via.repo)))
::  what an older host sends is read with no via anywhere
::
=/  from-old=packet:git-peer  [%catalog request ~[legacy-entry]]
?>  =([~ request ~[plain]] (unpack:git-catalog (receive from-old)))
?>  =(plain (from-legacy:git-catalog legacy-entry))
?>  =(legacy-entry (legacy:git-catalog shared(name 'tools', head 'refs/heads/main', refs 3, objects 41, writable %.n)))
::  an empty answer stays on the older shape
::
=/  empty=packet:git-peer  (pack:git-catalog request ~)
?>  ?=(%catalog -.empty)
?>  =(~ repositories.catalog.empty)
::  packets that are not catalogs are not unpacked
::
?>  =(~ (unpack:git-catalog [%catalog-request request]))
::  the gate: nothing rides to ~wyl, so a fan-out asks it
::
=/  member=@p  ~wyl
=/  first=@uv  0v1.23456
=/  second=@uv  0v2.34567
=/  sent-at=@da  ~2026.9.2..19.00.00
=/  quiet=ledger:git-catalog  ~
?>  =([%ask ~] (plan:git-catalog quiet ~ member sent-at))
=/  asked=discovery:git-catalog  (waiting:git-catalog member `group)
?>  &(active.asked !ok.asked =(%waiting status.asked) =(`group group.asked) =(~ held-since.asked))
?>  =('contacting peer' message.asked)
::  the first request is out and unacked: a second fan-out holds ~wyl,
::  recorded as pending and settled at once, saying when the request it
::  waits behind went out, and still asks ~rys
::
=/  riding=ledger:git-catalog  (sent:git-catalog quiet member first sent-at)
?>  =([~ first sent-at] (~(get by riding) member))
?>  =([%hold sent-at] (plan:git-catalog riding ~ member (add sent-at ~m5)))
?>  =([%ask ~] (plan:git-catalog riding ~ ~rys (add sent-at ~m5)))
=/  pending=discovery:git-catalog  (held:git-catalog member `group sent-at)
?>  &(!active.pending !ok.pending =(%pending status.pending) =(`sent-at held-since.pending))
?>  =('an earlier request to this ship is still in flight' message.pending)
::  a discovery already asking ~wyl on this group's behalf is reused first
::
?>  =([%reuse first] (plan:git-catalog riding (my ~[[member first]]) member (add sent-at ~m5)))
::  the timer settles the discovery but not the ledger: the poke is still out
::
=/  late=discovery:git-catalog  (timed-out:git-catalog asked)
?>  &(!active.late !ok.late =(%unreachable status.late))
?>  =('peer discovery timed out' message.late)
?>  =([%hold sent-at] (plan:git-catalog riding ~ member (add sent-at ~s30)))
::  the hold lapses after an hour: held 59 minutes it still holds, held 61
::  minutes it is treated as absent and the fan-out asks afresh, and the
::  fresh request overwrites the ledger entry.  a clock behind the send
::  time holds
::
?>  =(~h1 hold-expiry:git-catalog)
?>  =([%hold sent-at] (plan:git-catalog riding ~ member (add sent-at ~m59)))
?>  =([%ask ~] (plan:git-catalog riding ~ member (add sent-at ~m61)))
?>  =([%ask ~] (plan:git-catalog riding ~ member (add sent-at ~h1)))
?>  =([%hold sent-at] (plan:git-catalog riding ~ member (sub sent-at ~m1)))
?>  !(lapsed:git-catalog sent-at (add sent-at ~m59))
?>  (lapsed:git-catalog sent-at (add sent-at ~m61))
=/  renewed=ledger:git-catalog  (sent:git-catalog riding member second (add sent-at ~m61))
?>  =([~ second (add sent-at ~m61)] (~(get by renewed) member))
?>  =([%hold (add sent-at ~m61)] (plan:git-catalog renewed ~ member (add sent-at ~m62)))
::  a late ack of the request the fresh one replaced leaves the fresh hold
::
?>  =(renewed (settled:git-catalog renewed first))
?>  =(~ (settled:git-catalog renewed second))
::  an ack clears it; an ack of some other request does not
::
?>  =([%ask ~] (plan:git-catalog (settled:git-catalog riding first) ~ member (add sent-at ~m5)))
?>  =(~ (settled:git-catalog riding first))
?>  =([%hold sent-at] (plan:git-catalog (settled:git-catalog riding second) ~ member (add sent-at ~m5)))
::  a nack clears it and marks the discovery no-urgit
::
=/  refused-by-gall=discovery:git-catalog  (nacked:git-catalog asked)
?>  &(!active.refused-by-gall !ok.refused-by-gall =(%no-urgit status.refused-by-gall))
?>  =('peer does not run urgit' message.refused-by-gall)
?>  =([%ask ~] (plan:git-catalog (settled:git-catalog riding first) ~ member (add sent-at ~m5)))
::  the time a pending entry reports, as the browser reads it
::
?>  =('2026-09-02T19:00:00Z' (iso:git-catalog sent-at))
?>  =('2026-09-03T00:04:30Z' (iso:git-catalog ~2026.9.3..0.4.30))
?>  =('2026-12-31T23:59:59Z' (iso:git-catalog ~2026.12.31..23.59.59..ffff))
::  the answer, and a peer's own error, both count as answered
::
=/  done=discovery:git-catalog  (answered:git-catalog asked ~[plain shared])
?>  &(!active.done ok.done =(%answered status.done) =(~[plain shared] repositories.done))
=/  errored=discovery:git-catalog  (refused:git-catalog asked 'no such repository')
?>  &(!active.errored !ok.errored =(%answered status.errored) =('no such repository' message.errored))
::  the status words the UI reads
::
?>  =('answered' (status-text:git-catalog %answered))
?>  =('no-urgit' (status-text:git-catalog %no-urgit))
?>  =('unreachable' (status-text:git-catalog %unreachable))
?>  =('pending' (status-text:git-catalog %pending))
?>  =('waiting' (status-text:git-catalog %waiting))
?>  =(%waiting =>(*discovery:git-catalog status))
%.y
