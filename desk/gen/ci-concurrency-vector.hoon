::  Semantic vectors over production transitions. Parent runs on a ship.
/-  ci
/+  ci-cancel, ci-concurrency, ci-expr, ci-plan, ci-provenance
:-  %say
|=  *
:-  %noun
=/  now=@da  ~2026.10.19
=/  candidate
  |=  id=@uv
  ^-  candidate:ci
  =/  c  *candidate:ci
  c(id id, repo 'repo', ref 'refs/heads/master', candidate `0xa, status %pending, mode %required, created now, updated now)
=/  job
  |=  [workflow=@t id=@t]
  ^-  job:ci
  =/  j  *job:ci
  j(workflow workflow, id id, name workflow, outputs `~)
=/  a=candidate:ci  (candidate 0v1)
=/  b=candidate:ci  (candidate 0v2)
=/  c=candidate:ci  (candidate 0v3)
=/  d=candidate:ci  (candidate 0v4)
=/  r=workflow-run:ci  ['repo' 'Suite' `'Group' %.n %running 'running' ~]
=/  initial=state-8:ci
  =/  s  *state-8:ci
  s(candidates (malt ~[[id.a a] [id.b b] [id.c c] [id.d d]]))
=/  enter
  |=  [s=state-8:ci id=@uv w=@t run=workflow-run:ci]
  ^-  state-8:ci
  (enter:ci-concurrency s [id w] run now)
=/  phase
  |=  [s=state-8:ci id=@uv w=@t]
  ^-  workflow-phase:ci
  phase:(~(got by workflow-runs.s) [id w])
=/  first  (enter initial id.a 'suite.yml' r(cancel %.y))
=/  waiting  (enter first id.b 'fixtures.yml' r)
=/  replaced  (enter waiting id.c 'suite.yml' r)
=/  interrupt  (enter replaced id.d 'other.yml' r(cancel %.y))
=/  done=state-8:ci
  waiting(candidates (~(put by candidates.waiting) id.a a(status %passed)))
=/  released  (refresh:ci-concurrency done)
=/  other-repo  (enter first id.b 'suite.yml' r(repo 'another'))
=/  other-case  (enter first id.b 'suite.yml' r(group `'gRoUp'))
=/  unicode
  (enter (enter initial id.a 'suite.yml' r(group `'é')) id.b 'suite.yml' r(group `'É'))
::  One candidate contains two independent workflows, both with a running
::  attempt. Cancelling one closes exactly its own attempt, permanently.
=/  suite=job:ci  (job 'suite.yml' 'build')
=/  fixture=job:ci  (job 'fixtures.yml' 'build')
=/  cleanup=job:ci  (job 'suite.yml' 'cleanup')
=/  cleanup  cleanup(needs ~['build'], cond `[%if 'always()' 1 1 [%call 'always' ~]])
=/  ordinary=job:ci  (job 'suite.yml' 'later')
=/  aa=candidate:ci  a(plan `~[suite fixture cleanup ordinary], attempts ~[0v12 0v11])
=/  attempt
  |=  [id=@uv workflow=@t approval=(unit @uv)]
  ^-  attempt:ci
  =/  x  *attempt:ci
  x(id id, candidate id.a, daemon 0v9, kind %job, workflow `workflow, job `'build', status %running, approval approval)
=/  daemon=daemon:ci
  =/  x  *daemon:ci
  x(id 0v9, running (silt ~[0v11 0v12]))
=/  both=state-8:ci
  =/  s  (enter first id.a 'fixtures.yml' r(group `'fixture-group'))
  %=  s
    candidates  (~(put by candidates.s) id.a aa)
    attempts  (malt ~[[0v11 (attempt 0v11 'suite.yml' ~)] [0v12 (attempt 0v12 'fixtures.yml' ~)]])
    daemons  (malt ~[[0v9 daemon]])
  ==
=/  cancelled  (cancel-one:ci-concurrency both [id.a 'suite.yml'] 'operator cancelled' ~ now)
=/  known  (standings:ci-concurrency cancelled aa)
=/  context=run-context:ci-plan  ['push' '' 'refs/heads/master' 'a' '~zod' [%obj ~] %.n]
=/  always  (decide:ci-concurrency cancelled aa cleanup standings.known ~ context)
=/  skipped  (decide:ci-concurrency cancelled aa ordinary standings.known ~ context)
=/  on-cancel
  (decide:ci-concurrency cancelled aa ordinary(cond `[%if 'cancelled()' 1 1 [%call 'cancelled' ~]]) standings.known ~ context)
=/  on-success
  (decide:ci-concurrency cancelled aa ordinary(cond `[%if 'success()' 1 1 [%call 'success' ~]]) standings.known ~ context)
=/  guarded=state-8:ci
  both(attempts (~(put by attempts.both) 0v11 (attempt 0v11 'suite.yml' `0v99)))
=/  privileged  (cancel-one:ci-concurrency guarded [id.a 'suite.yml'] 'operator cancelled' ~ now)
=/  pa=attempt:ci  (~(got by attempts.privileged) 0v11)
=/  all-done=state-8:ci
  =/  x  (~(got by attempts.cancelled) 0v12)
  =/  s  cancelled(attempts (~(put by attempts.cancelled) 0v12 x(status %passed)))
  s(candidates (~(put by candidates.s) id.a aa(plan `~[suite fixture])))
=/  terminal=candidate:ci  (~(got by candidates.all-done) id.a)
=/  blocked-complete=state-8:ci
  =/  s  *state-8:ci
  s(workflow-runs (malt ~[[[id.a ''] r(group ~, phase %cancelled, reason 'operator cancelled')]]))
=/  known-none=(unit concurrency:ci)  `[~ ~ ~]
=/  trial=workflow-run:ci
  (evaluate:ci-concurrency a(mode %trial) 'Suite' known-none context %.n)
=/  unknown=workflow-run:ci
  (evaluate:ci-concurrency a 'Suite' ~ context %.n)
=/  old=workflow-run:ci
  (evaluate:ci-concurrency a 'Suite' known-none context %.y)
=/  failed-plan=state-8:ci
  =/  x  *attempt:ci
  %=  first
    candidates  (~(put by candidates.first) id.a a(attempts ~[0v20]))
    attempts  (malt ~[[0v20 x(id 0v20, candidate id.a, kind %plan, status %failed, reason `'plan refused')]])
  ==
=/  cases=(list [@t ?])
  :~  ['new false flag ignores old true flag' &(=(%running (phase waiting id.a 'suite.yml')) =(%waiting (phase waiting id.b 'fixtures.yml')))]
      ['owner has Cancel authority' (authorized:ci-provenance `~zod ~ %ci-policy ~ ~zod)]
      ['ci-policy delegate has Cancel authority' (authorized:ci-provenance `~zod ~[[%ci-policy ~ (silt ~[~nec])]] %ci-policy ~ ~nec)]
      ['unbound actor has no Cancel authority' !(authorized:ci-provenance `~zod ~ %ci-policy ~ ~nec)]
      ['one waiter; new arrival cancels the old waiter across workflows' &(=(%cancelled (phase replaced id.b 'fixtures.yml')) =(%waiting (phase replaced id.c 'suite.yml')))]
      ['new true flag cancels running and waiting' &(=(%cancelled (phase interrupt id.a 'suite.yml')) =(%cancelled (phase interrupt id.c 'suite.yml')) =(%running (phase interrupt id.d 'other.yml')))]
      ['waiting starts when running finishes' =(%running (phase released id.b 'fixtures.yml'))]
      ['group is repository scoped' =(%running (phase other-repo id.b 'suite.yml'))]
      ['group names ignore ASCII case' =(%waiting (phase other-case id.b 'suite.yml'))]
      ['unevaluable Unicode case comparison refuses instead of missing a group' =(%refused (phase unicode id.b 'suite.yml'))]
      ['cancellation preserves candidate and other state' =(candidates.both candidates.cancelled)]
      ['other workflow attempt keeps running' =(%running status:(~(got by attempts.cancelled) 0v12))]
      ['cancelled attempt is terminal in standings' =(%cancelled (~(got by standings.known) ['suite.yml' 'build']))]
      ['cancel removes only that attempt from daemon running set' =((silt ~[0v12]) running:(~(got by daemons.cancelled) 0v9))]
      ['always cleanup runs after cancellation' ?=(%run -.always)]
      ['cancelled() jobs run after cancellation' ?=(%run -.on-cancel)]
      ['success() jobs skip after cancellation' ?=(%skip -.on-success)]
      ['ordinary unstarted jobs skip after cancellation' ?=(%skip -.skipped)]
      ['independent workflow keeps aggregate pending' =(%pending status:(verdict:ci-concurrency cancelled aa))]
      ['terminal cancelled run is not failed or infrastructure error' =(%cancelled status:(verdict:ci-concurrency all-done terminal))]
      ['whole-run marker cancels finished jobs blocked on absent required evidence' =(%cancelled status:(verdict:ci-concurrency blocked-complete a(plan `~)))]
      ['privileged cancellation closes unknown for review' &(=(%unknown outcome.pa) =(%infrastructure-error status.pa) ?=(^ reason.pa))]
      ['cancelled required run cannot land, even with stale passed status' !(landable:ci-concurrency cancelled aa(status %passed))]
      ['trial twin never participates' &(!(participates:ci-concurrency a(mode %trial)) =(%unknown phase.trial))]
      ['resolver record never participates' !(participates:ci-concurrency a(ref 'refs/ci/resolve'))]
      ['all counterpart modes participate' (levy `(list mode:ci)`~[%required %event %manual %shadow] |=(m=mode:ci (participates:ci-concurrency a(mode m))))]
      ['old lock is unknown with reason, not known none' &(=(%unknown phase.unknown) !=('' reason.unknown))]
      ['catalog membership cannot hide a plan failure' =([%failed `'plan refused'] (verdict:ci-concurrency failed-plan (~(got by candidates.failed-plan) id.a)))]
      ['upgraded in-flight run excluded even with declaration' &(=(%unknown phase.old) =(~ group.old))]
      ['safe candidate status bunt stays unknown' =(%unknown *candidate-status:ci)]
      ['safe standing bunt stays pending' =(%pending *standing:ci-plan)]
      ['safe workflow phase bunt stays unknown' =(%unknown *workflow-phase:ci)]
  ==
=/  failed=(list @t)  (murn cases |=([name=@t ok=?] ?:(ok ~ `name)))
~&  [%ci-concurrency-failed failed]
?=(~ failed)
