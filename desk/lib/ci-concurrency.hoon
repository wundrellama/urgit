::  Workflow-run concurrency transitions. The controller and native vectors
::  call these same gates; no duplicate scheduler model is used in tests.
/-  ci
/+  ci-cancel, ci-expr, ci-plan
|%
++  landable
  |=  [s=state-8:ci c=candidate:ci]
  ^-  ?
  ?&  =(%passed status.c)
      !(lien (records s id.c) |=([* r=workflow-run:ci] =(%cancelled phase.r)))
  ==
::
++  participates
  |=  c=candidate:ci
  ^-  ?
  ?&(!=(%trial mode.c) !=('refs/ci/resolve' ref.c))
::
++  evaluate
  |=  [c=candidate:ci name=@t declaration=(unit concurrency:ci) context=run-context:ci-plan legacy=?]
  ^-  workflow-run:ci
  =/  r=workflow-run:ci  [repo.c name ~ %.n %unknown '' ~]
  ?:  legacy
    r(reason 'concurrency not applied: this run was in flight or recorded before state-8')
  ?~  declaration
    r(reason 'concurrency not read for this lock: resolve or promote again to apply it')
  ?^  refusal.u.declaration  r(phase %refused, reason u.refusal.u.declaration)
  =/  scope=scope:ci-expr  empty-scope:ci-expr
  =.  scope
    %=  scope
      github  %:  github-of:ci-expr
                event.context  payload.context  ref.context  sha.context
                actor.context  name  ''
              ==
      inputs  inputs.context
    ==
  =/  group=(each (unit @t) @t)
    ?~  group.u.declaration  [%& ~]
    =/  value  (interpolate:ci-expr parts.template.u.group.u.declaration scope)
    ?:  ?=(%| -.value)  [%| (rap 3 ~['concurrency group could not be evaluated: ' p.value])]
    [%& `p.value]
  ?:  ?=(%| -.group)  r(phase %refused, reason p.group)
  =/  cancel=(each ? @t)
    ?~  cancel.u.declaration  [%& %.n]
    =/  value  (eval-condition:ci-expr u.cancel.u.declaration scope)
    ?:  ?=(%| -.value)  [%| (rap 3 ~['cancel-in-progress could not be evaluated: ' p.value])]
    [%& (truthy:ci-expr p.value)]
  ?:  ?=(%| -.cancel)  r(phase %refused, reason p.cancel)
  =.  r  r(group p.group, cancel p.cancel)
  ?.  (participates c)  r(reason 'trial twins and resolver records do not take part in concurrency groups')
  r(phase %running, reason ?~(p.group 'no concurrency group' 'running in concurrency group'))
::
++  same-group
  |=  [a=workflow-run:ci b=workflow-run:ci]
  ^-  ?
  =/  same  (compare-group a b)
  ?&(?=(%& -.same) p.same)
::
++  compare-group
  |=  [a=workflow-run:ci b=workflow-run:ci]
  ^-  (each ? @t)
  ?.  ?&(=(repo.a repo.b) ?=(^ group.a) ?=(^ group.b))  [%& %.n]
  (text-same:ci-expr u.group.a u.group.b)
::
++  records
  |=  [s=state-8:ci id=candidate-id:ci]
  ^-  (list [key=workflow-key:ci run=workflow-run:ci])
  (skim ~(tap by workflow-runs.s) |=([k=workflow-key:ci *] =(candidate.k id)))
::
++  cancelled
  |=  [s=state-8:ci id=candidate-id:ci workflow=@t]
  ^-  ?
  =/  r=(unit workflow-run:ci)  (~(get by workflow-runs.s) [id workflow])
  ?&(?=(^ r) =(%cancelled phase.u.r))
::
++  cancel-one
  |=  [s=state-8:ci key=workflow-key:ci why=@t related=(unit candidate-id:ci) now=@da]
  ^-  state-8:ci
  =/  old=(unit workflow-run:ci)  (~(get by workflow-runs.s) key)
  ?~  old  s
  ?:  ?=(?(%complete %cancelled %refused) phase.u.old)  s
  =.  workflow-runs.s
    (~(put by workflow-runs.s) key u.old(phase %cancelled, reason why, related related))
  %+  roll  ~(val by attempts.s)
  |=  [a=attempt:ci acc=_s]
  ?.  ?&  =(candidate.a candidate.key)  =(%job kind.a)
          =(`workflow.key workflow.a)  =(%running status.a)
      ==  acc
  (close:ci-cancel acc a why now)
::
++  enter
  |=  [s=state-8:ci key=workflow-key:ci run=workflow-run:ci now=@da]
  ^-  state-8:ci
  ?:  (~(has by workflow-runs.s) key)  s
  ::  Use the expression evaluator's case semantics. If two group names
  ::  need unsupported Unicode case mapping, refuse this arrival instead
  ::  of silently putting equivalent names in different groups.
  =/  comparisons=(list (each ? @t))
    ?.  ?&(=(%running phase.run) ?=(^ group.run))  ~
    %+  turn
      (skim ~(val by workflow-runs.s) |=(r=workflow-run:ci ?=(?(%running %waiting) phase.r)))
    |=(r=workflow-run:ci (compare-group r run))
  =/  errors=(list @t)
    (murn comparisons |=(v=(each ? @t) ?:(?=(%| -.v) `p.v ~)))
  ?^  errors
    s(workflow-runs (~(put by workflow-runs.s) key run(phase %refused, reason (rap 3 ~['concurrency group comparison could not be evaluated: ' i.errors]))))
  =/  peers=(list [key=workflow-key:ci run=workflow-run:ci])
    ?.  ?&(=(%running phase.run) ?=(^ group.run))  ~
    (skim ~(tap by workflow-runs.s) |=([k=workflow-key:ci r=workflow-run:ci] (same-group r run)))
  =|  blocker=(unit candidate-id:ci)
  =/  joined=[state=state-8:ci blocker=(unit candidate-id:ci)]
    |-
    ?~  peers  [s blocker]
    =/  k=workflow-key:ci  key.i.peers
    =/  r=workflow-run:ci  run.i.peers
    ?.  ?=(?(%running %waiting) phase.r)  $(peers t.peers)
    ?:  ?&(!cancel.run =(%running phase.r))
      $(peers t.peers, blocker `candidate.k)
    =/  why=@t
      ?:  cancel.run  'cancelled: a new run joined the group with cancel-in-progress'
      'cancelled: a newer run is waiting in the group'
    $(peers t.peers, s (cancel-one s k why `candidate.key now))
  =.  s  state.joined
  =?  run  ?=(^ blocker.joined)
    run(phase %waiting, reason 'waiting for the run in progress in this group', related blocker.joined)
  s(workflow-runs (~(put by workflow-runs.s) key run))
::
++  standings
  |=  [s=state-8:ci c=candidate:ci]
  ^-  [standings=(map key:ci-plan standing:ci-plan) newest=(map key:ci-plan attempt:ci)]
  =/  newest=(map key:ci-plan attempt:ci)
    %+  roll  (flop attempts.c)
    |=  [id=attempt-id:ci acc=(map key:ci-plan attempt:ci)]
    =/  a=(unit attempt:ci)  (~(get by attempts.s) id)
    ?~  a  acc
    ?.  ?&(?=(%job kind.u.a) ?=(^ workflow.u.a) ?=(^ job.u.a))  acc
    ?:  =(%reoffered status.u.a)  acc
    ?.  =(generation.u.a generation.c)  acc
    ?:  ?&(=(%cancelled status.u.a) !(cancelled s id.c u.workflow.u.a))  acc
    (~(put by acc) [u.workflow.u.a u.job.u.a] u.a)
  :_  newest
  %-  ~(run by newest)
  |=  a=attempt:ci
  ^-  standing:ci-plan
  ?-  status.a
    %passed  %passed
    %failed  %failed
    %skipped  %skipped
    %running  %running
    %reoffered  %pending
    %cancelled  %cancelled
    %infrastructure-error  %unknown
  ==
::
++  decide
  |=  [s=state-8:ci c=candidate:ci job=job:ci known=(map key:ci-plan standing:ci-plan) entries=(map key:ci-plan need-entry:ci) context=run-context:ci-plan]
  ^-  decision:ci-plan
  ?~  plan.c  [%wait 'the run has no plan yet']
  =/  r=(unit workflow-run:ci)  (~(get by workflow-runs.s) [id.c workflow.job])
  ?:  ?&(?=(^ r) =(%waiting phase.u.r))  [%wait reason.u.r]
  ?:  ?&(?=(^ r) =(%refused phase.u.r))  [%error reason.u.r]
  %:  decide:ci-plan
    job  u.plan.c  known  entries
    context(cancelled (cancelled s id.c workflow.job))
  ==
::
::  Reconcile terminal workflows independently; a slow unrelated workflow
::  never keeps a completed workflow's group occupied. A waiter starts as
::  soon as the group has no running member, including on explicit Cancel.
++  refresh
  |=  s=state-8:ci
  ^-  state-8:ci
  =.  workflow-runs.s
    %-  malt
    %+  turn  ~(tap by workflow-runs.s)
    |=  [k=workflow-key:ci r=workflow-run:ci]
    ^-  [workflow-key:ci workflow-run:ci]
    ?.  ?=(?(%running %waiting) phase.r)  [k r]
    =/  c=(unit candidate:ci)  (~(get by candidates.s) candidate.k)
    ?~  c  [k r(phase %complete)]
    ?.  =(%pending status.u.c)  [k r(phase %complete)]
    ?:  =(%waiting phase.r)  [k r]
    ?~  plan.u.c  [k r]
    =/  jobs=(list job:ci)  (skim u.plan.u.c |=(j=job:ci =(workflow.j workflow.k)))
    =/  v  (verdict:ci-plan jobs standings:(standings s u.c))
    ?:  =(%pending status.v)  [k r]
    [k r(phase %complete, reason 'workflow finished; concurrency group released')]
  =/  rows  ~(tap by workflow-runs.s)
  |-
  ?~  rows  s
  =/  k=workflow-key:ci  -.i.rows
  =/  r=workflow-run:ci  +.i.rows
  ?.  =(%waiting phase.r)  $(rows t.rows)
  =/  busy=?
    %+  lien  ~(val by workflow-runs.s)
    |=(peer=workflow-run:ci ?&(=(%running phase.peer) (same-group r peer)))
  ?:  busy  $(rows t.rows)
  %=  $
    rows  t.rows
    s  s(workflow-runs (~(put by workflow-runs.s) k r(phase %running, reason 'previous run finished; running in concurrency group', related ~)))
  ==
::
++  plan-allowed
  |=  [s=state-8:ci c=candidate:ci]
  ^-  ?
  =/  rows  (records s id.c)
  ?~  rows  %.y
  (lien `(list [workflow-key:ci workflow-run:ci])`rows |=([* r=workflow-run:ci] ?=(?(%running %unknown) phase.r)))
::
++  verdict
  |=  [s=state-8:ci c=candidate:ci]
  ^-  [status=candidate-status:ci reason=(unit @t)]
  =/  rows  (records s id.c)
  =/  v=[status=candidate-status:ci reason=(unit @t)]
    ?^  plan.c  (verdict:ci-plan u.plan.c standings:(standings s c))
    ?.  (plan-allowed s c)
      ?:  (lien rows |=([* r=workflow-run:ci] =(%waiting phase.r)))  [%pending ~]
      [%failed `'workflow concurrency refused']
    ::  Catalog registration must not hide a planner failure or an unknown
    ::  result. Retain the pre-plan verdict of the current generation.
    =/  ids  attempts.c
    |-
    ?~  ids  [%pending ~]
    =/  a=(unit attempt:ci)  (~(get by attempts.s) i.ids)
    ?~  a  $(ids t.ids)
    ?.  =(generation.u.a generation.c)  $(ids t.ids)
    ?-  status.u.a
      %passed  [%passed ~]
      %failed  [%failed reason.u.a]
      %infrastructure-error  [%unknown reason.u.a]
      ?(%running %skipped %reoffered %cancelled)  [%pending ~]
    ==
  ?:  =(%pending status.v)  v
  =/  stopped=(list [key=workflow-key:ci run=workflow-run:ci])
    (skim rows |=([* r=workflow-run:ci] =(%cancelled phase.r)))
  ?^  stopped  [%cancelled `reason.run.i.stopped]
  v
--
