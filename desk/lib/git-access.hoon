::  Native peer repository access policy.
::
::    pure: the agent reads the requester's %groups seat and hands the
::    resulting capability in, so nothing here scries.
::
/-  git
|%
++  capability-rank
  |=  cap=capability:git
  ^-  @ud
  ?-  cap
    %none   0
    %read   1
    %write  2
  ==
::
++  capability-max
  |=  [a=capability:git b=capability:git]
  ^-  capability:git
  ?:  (gte (capability-rank a) (capability-rank b))  a
  b
::
::  whether this ship's copy of a group may be believed
::
::    a group this ship hosts (%pub) is authoritative.  a joined group
::    (%sub) is the host's mirror: believe it only while %groups reports
::    it initialised and this ship still holds a seat in it.
::
++  mirror-trusted
  |=  [net=?(%pub %sub) init=? seated=?]
  ^-  ?
  ?:  ?=(%pub net)  %.y
  &(init seated)
::
::  what a repository's group policy grants the holder of a seat
::
::    no policy or no seat grants nothing; a seat without roles gets the
::    base capability; otherwise the strongest mapped role wins and
::    unmapped roles count as %none.
::
++  group-capability
  |=  [policy=(unit group-policy:git) seat=(unit group-seat:git)]
  ^-  capability:git
  ?~  policy  %none
  ?~  seat  %none
  ?:  =(~ roles.u.seat)  base.u.policy
  ::  start from %none explicitly rather than from a bunt
  ::
  =/  remaining=(list @tas)  ~(tap in roles.u.seat)
  =/  best=capability:git  %none
  |-
  ?~  remaining  best
  %=  $
    remaining  t.remaining
    best       (capability-max best (~(gut by roles.u.policy) i.remaining %none))
  ==
::
++  can-read
  |=  $:  public=?
          owner=@p
          readers=(set @p)
          writers=(set @p)
          group=capability:git
          requester=@p
      ==
  ^-  ?
  ?|  public
      =(requester owner)
      (~(has in readers) requester)
      (~(has in writers) requester)
      !=(%none group)
  ==
::
++  can-write
  |=  [owner=@p writers=(set @p) group=capability:git requester=@p]
  ^-  ?
  ?|  =(requester owner)
      (~(has in writers) requester)
      =(%write group)
  ==
--
