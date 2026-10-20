::  Closing an attempt is shared by workflow, operator, reset and resolver
::  cancellation. Approval-bearing attempts remain unknown and need review.
/-  ci
|%
++  close
  |=  [s=state-8:ci a=attempt:ci why=@t now=@da]
  ^-  state-8:ci
  ?.  =(%running status.a)  s
  =/  privileged=?  ?=(^ approval.a)
  =/  reason=@t
    ?.  privileged  why
    (rap 3 ~['cancelled while a privileged job ran; the external side effect may have happened: needs review (' why ')'])
  =/  closed=attempt:ci
    %=  a
      status  ?:(privileged %infrastructure-error %cancelled)
      outcome  ?:(privileged %unknown outcome.a)
      result  `[%infrastructure-error reason]
      reason  `reason
      finished  `now
    ==
  =.  attempts.s  (~(put by attempts.s) id.a closed)
  =/  runner=(unit daemon:ci)  (~(get by daemons.s) daemon.a)
  =?  daemons.s  ?=(^ runner)
    (~(put by daemons.s) daemon.a u.runner(running (~(del in running.u.runner) id.a)))
  =.  read-capabilities.s
    %-  malt
    %+  skip  ~(tap by read-capabilities.s)
    |=([* cap=read-capability:ci] =(attempt.cap id.a))
  s
--
