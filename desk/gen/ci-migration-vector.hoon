::  ci-migration-vector: %urgit-ci's state migrations (state-migration
::  ruling 01: Q12 A; QUESTIONS-SOURCE-01 §12; contract §8c).  A
::  populated state of each supported earlier shape — the committed
::  base, review 06's schema, the Q11 schema, state-1 and state-2 — is
::  loaded by lib/ci-migrate and checked field by field:
::    - enrollment and bearers, a revoked daemon kept revoked;
::    - trust, policies, credentials, the signing key and ship keys;
::    - candidates, assignments and a running attempt;
::    - an expired approval and a consumed one, an override, roles,
::      environments, a baseline and the audit;
::    - open and final recovery commands and a retention report;
::    - state-1's attempts, which gain no pinned addresses (state-2);
::    - state-2's candidates, each a push run (state-3: a run records the
::      event it ran for), its locks with no manual-run inventory (none was
::      read before state-3), its pinned addresses kept;
::    - state-3 (Delivery 1B): a manual run's event and inputs and a lock's
::      inventory kept; a planned job's %output-eq gate kept at its
::      admitted meaning with its declared outputs not known; an attempt's
::      outputs by name become step outputs of no named step; a running
::      attempt stays running with no decision, needs context or resolved
::      outputs;
::    - state-4 (Delivery 1C): a required run with its plan and running
::      attempt and a manual run keep every field, their workflow selection
::      never evaluated (~), none becomes an event run; a lock keeps its
::      inventory and its trigger catalog is unknown (~, not empty); no
::      forge event, event id or schedule is invented.
::  state-7 (frozen as sur/ci-state-7) converts to state-8 (CI-CONCURRENCY-1)
::  field by field: every candidate, attempt and lock kept, an open run
::  in no group with the reason, a closed one with none recorded, an old
::  lock's catalog concurrency unknown, never "no group".  The checks of
::  the shapes before it read the state-7 they convert through
::  (+load-7), where their nouns are unchanged.
::  state-8 (frozen as sur/ci-state-8) converts to state-9 field by field:
::  the pull facts of an event run and of a recorded forge event gain no
::  labels and no label, and the payload text is kept byte for byte.  Its
::  checks read the state-9 it converts through (+load-9).
::  state-9 (frozen as sur/ci-state-9) converts to state-10 (CI-RECOVERY-1)
::  field by field.  Every one of its thirty-six fields is kept, and the
::  recovery record is the bunt: unpaused, with no checkpoint, no staged
::  import, no restore and no history.
::  The current state (state-10) reloads unchanged.  An unknown version, an
::  atom, a malformed %0, a corrupt %0, %1, %2, %3, %4 or %5, a state-1
::  tagged %2, a state-2 tagged %1 or %3, a state-3 tagged %2 or %4, a
::  state-4 tagged %3 or %5 and a state-6 tagged %4 are refused, and so is
::  an ambiguous classification.  Every case names the verdict it expects;
::  the intentionally FALSE cases are refusals.  Prints `passed=N of=M`
::  and ends in a loobean.
::
::    NOT RUN in the source stage that wrote it: it needs a ship.
::
/-  ci, ci-state-1, ci-state-2, ci-state-3, ci-state-4, ci-state-5, ci-state-6, ci-state-7, ci-state-8, ci-state-9, ci-state-base, ci-state-review06, ci-state-q11
/+  ci-migrate
:-  %say
|=  *
:-  %noun
::  the committed base: two daemons (one enrolled, one revoked), a
::  candidate, its assignment and its running attempt
::
=/  active=daemon:ci-state-base
  :*  0v1.aaaaa  0x1111  `0x2222  ~2026.9.1  `~2026.9.2  `~2026.9.20
      2  'microvm'  (silt ~[0v7.aaaaa])  ~  ~  (silt ~['big-mem'])  `(silt ~['erpit'])
  ==
=/  revoked=daemon:ci-state-base
  :*  0v2.bbbbb  0x3333  ~  ~2026.9.1  `~2026.9.2  ~
      1  'docker-rootless'  ~  `~2026.9.10  `'revoked by the operator'  ~  ~
  ==
=/  cand=candidate:ci-state-base
  :*  0v3.ccccc  'erpit'  'refs/heads/master'  0xa1  0xb1  `0xc1  %.n
      %pending  ~[0v7.aaaaa]  ~  ~  ~  ~zod  %session  %trusted  ~
      ~2026.9.20  ~2026.9.21
  ==
=/  running=attempt:ci-state-base
  :*  0v7.aaaaa  0v3.ccccc  0v8.ddddd  0v1.aaaaa  %trusted  %job  `'ci.yml'  `'build'
      %running  3  ~  ~  ~  ~  ~  ~  ~2026.9.21  ~
  ==
=/  assigned=assignment:ci-state-base
  :*  0v8.ddddd  0v3.ccccc  0v1.aaaaa  0v7.aaaaa  %trusted  %job  `'ci.yml'  `'build'
      ~h1  ~2026.9.21  `~2026.9.21
  ==
=/  base-state=state-0:ci-state-base
  :*  %0
      (my ~[[0v3.ccccc cand]])
      (my ~[[0v1.aaaaa active] [0v2.bbbbb revoked]])
      (my ~[[0v8.ddddd assigned]])
      (my ~[[0v7.aaaaa running]])
      (silt ~[['erpit' 'refs/heads/master']])
      (my ~[['erpit' %approval]])
      (my ~[[['erpit' 'DEPLOY_KEY'] ['s3cr3t-value' %job ~ ~2026.9.3]]])
      `[0xabc 0xdef 0x123 ~2026.9.1]
      `[1 0x456 0x789]
  ==
::  review 06's schema: the same history in P4's records, and what P4
::  recorded — an expired approval, a consumed one, an override, a role,
::  an environment, a baseline, the audit
::
=/  r-active=daemon:ci-state-review06
  :*  0v1.aaaaa  0x1111  `0x2222  ~2026.9.1  `~2026.9.2  `~2026.9.20
      2  'microvm'  (silt ~[0v7.aaaaa])  ~  ~  (silt ~['big-mem'])  `(silt ~['erpit'])
      (silt ~['egress'])  %.y
  ==
=/  r-revoked=daemon:ci-state-review06
  :*  0v2.bbbbb  0x3333  ~  ~2026.9.1  `~2026.9.2  ~
      1  'docker-rootless'  ~  `~2026.9.10  `'revoked by the operator'  ~  ~
      ~  %.n
  ==
=/  r-cand=candidate:ci-state-review06
  :*  0v3.ccccc  'erpit'  'refs/heads/master'  0xa1  0xb1  `0xc1  %.n
      %pending  ~[0v7.aaaaa]  ~  ~  ~  ~zod  %session  %trusted  ~
      ~2026.9.20  ~2026.9.21
      %required  4  `0xb1  `0x1111  0v9.eeeee  %vm  ~  %.n
  ==
=/  r-running=attempt:ci-state-review06
  :*  0v7.aaaaa  0v3.ccccc  0v8.ddddd  0v1.aaaaa  %trusted  %job  `'ci.yml'  `'build'
      %running  3  ~  ~  ~  ~  ~  ~  ~2026.9.21  ~
      4  %required  0v5.fffff  %vm  'locked'  ~  %known
  ==
=/  expired=approval:ci-state-review06
  :*  0v4.ggggg  'erpit'  0v9.eeeee  0v3.ccccc  0xc1  'ci.yml'  'deploy'  'prod'
      (silt ~['DEPLOY_KEY'])  0xb1  `0x1111  4  ~zod  ~2026.9.19  ~2026.9.19..00.15.00
      ~  ~
  ==
=/  consumed=approval:ci-state-review06
  :*  0v4.hhhhh  'erpit'  0v9.eeeee  0v3.ccccc  0xc1  'ci.yml'  'deploy'  'prod'
      (silt ~['DEPLOY_KEY'])  0xb1  `0x1111  4  ~zod  ~2026.9.21  ~2026.9.21..00.15.00
      `0v7.aaaaa  ~
  ==
=/  over=override:ci-state-review06
  :*  0v6.iiiii  'erpit'  'refs/heads/master'  0v9.eeeee  0v3.ccccc  0xc1  0xb1
      ~zod
      'hotfix'
      'status failed'
      4
      ~2026.9.21
      ~2026.9.21..00.15.00
      ~
      `'policy generation changed'
  ==
=/  r6-state=state-0:ci-state-review06
  :*  %0
      (my ~[[0v3.ccccc r-cand]])
      (my ~[[0v1.aaaaa r-active] [0v2.bbbbb r-revoked]])
      (my ~[[0v8.ddddd assigned]])
      (my ~[[0v7.aaaaa r-running]])
      (silt ~[['erpit' 'refs/heads/master']])
      (my ~[['erpit' %approval]])
      (my ~[[['erpit' 'DEPLOY_KEY'] ['s3cr3t-value' %env (silt ~['prod']) ~2026.9.3]]])
      `[0xabc 0xdef 0x123 ~2026.9.1]
      `[1 0x456 0x789]
      (my ~[['erpit' 0v9.eeeee]])
      (my ~[['erpit' 4]])
      %-  my
      ~[[['erpit' 'refs/heads/master'] [0xb1 ~['.github/'] `0x1111 ~2026.9.18 ~zod 'first baseline' 4 ~]]]
      ~
      (my ~[['erpit' ~[[%environment-approver `'prod' (silt ~[~nec])]]]])
      %-  my
      ~[[['erpit' 'prod'] ['prod' 'production' %manual (silt ~['DEPLOY_KEY']) ~2026.9.17 ~zod]]]
      (my ~[[0v4.ggggg expired] [0v4.hhhhh consumed]])
      (my ~[[0v6.iiiii over]])
      ~
      ~
      ~
      ~
      ~[[~2026.9.21 ~zod 'approve-environment' 'erpit' 'prod for build'] [~2026.9.18 ~zod 'promote-baseline' 'erpit' 'first baseline']]
      (my ~[['erpit' %vm]])
      ~
      ~
      ~
  ==
::  the Q11 schema: review 06's, and an open and a final recovery command
::  with the report they were bound to
::
=/  open-cmd=recovery-command:ci-state-q11
  :*  0v5.jjjjj  0v1.aaaaa  'release-legacy'  'ci-0v4.old/microvm///0/0//1700000000/1'  1
      'abababababababababababababababababababababababababababababababab'
      'owner/repo · ci.yml · build'
      ~zod  ~2026.9.22  ~2026.9.22..00.15.00  0v6.kkkkk  `~2026.9.22  1  %delivered  ''  ~
  ==
=/  final-cmd=recovery-command:ci-state-q11
  :*  0v5.lllll  0v1.aaaaa  'release-legacy'  'ci-0v4.old/microvm///0/0//1700000000/1'  1
      'abababababababababababababababababababababababababababababababab'
      'owner/repo · ci.yml · build'
      ~zod  ~2026.9.21  ~2026.9.21..00.15.00  0v6.mmmmm  `~2026.9.21  1  %refused
      'its release is not proven now'  `~2026.9.21
  ==
=/  report=recovery-report:ci-state-q11
  :+  ~2026.9.22  '{"version":1}'
  ~[['ci-0v4.old/microvm///0/0//1700000000/1' 1 '0v4.old' 'owner/repo · ci.yml · build' 'legacy' %.y 'abababababababababababababababababababababababababababababababab']]
=/  q11-state=state-0:ci-state-q11
  ;;  state-0:ci-state-q11
  :*  %0
      candidates.r6-state  daemons.r6-state  assignments.r6-state  attempts.r6-state
      ci-protected.r6-state  policies.r6-state  credentials.r6-state  signing.r6-state
      ship-keys.r6-state  incarnations.r6-state  generations.r6-state  baselines.r6-state
      locks.r6-state  roles.r6-state  environments.r6-state  approvals.r6-state
      overrides.r6-state  network-policies.r6-state  read-capabilities.r6-state
      shadows.r6-state  comparisons.r6-state  audit.r6-state  sandbox-requirements.r6-state
      mirror-tokens.r6-state  resolve-mappings.r6-state  harness-paths.r6-state
      (my ~[[0v1.aaaaa report]])
      (my ~[[0v5.jjjjj open-cmd] [0v5.lllll final-cmd]])
  ==
::  state-1: the Q11 fields under the tag %1
::
=/  s1-state=state-1:ci-state-1  ;;(state-1:ci-state-1 [%1 +.q11-state])
::  the running attempt as state-2 holds it: every field as it was, and no
::  pinned addresses; and the same attempt once its daemon reported them
::
=/  r-running-2=attempt:ci-state-2
  :*  0v7.aaaaa  0v3.ccccc  0v8.ddddd  0v1.aaaaa  %trusted  %job  `'ci.yml'  `'build'
      %running  3  ~  ~  ~  ~  ~  ~  ~2026.9.21  ~
      4  %required  0v5.fffff  %vm  'locked'  ~  %known  ~
  ==
=/  pins=(list pin:ci-state-2)
  :~  ['archive.ubuntu.com' ~['91.189.91.82' '185.125.190.81']]
      ['bootstrap.urbit.org' ~['104.21.64.85']]
  ==
::  state-2: the Q11 records, its attempt with pinned addresses, and a
::  lock resolved before manual runs were read
::
=/  lock-2=lock:ci-state-2
  :*  0x77  'erpit'  0xb1  ~  ~2026.9.18  0v1.aaaaa  0  ~['fixture: run steps only']
  ==
=/  s2-state=state-2:ci-state-2
  ;;  state-2:ci-state-2
  :*  %2
      candidates.q11-state  daemons.q11-state  assignments.q11-state
      (my ~[[0v7.aaaaa r-running-2(pinned pins)]])
      +63.q11-state
  ==
=.  locks.s2-state  (my ~[[0x77 lock-2]])
::  state-3: a manual run, its event and typed inputs as admitted, and a
::  lock with the manual-run inventory its resolver read
::
=/  manual-cand=candidate:ci-state-3
  :*  0v3.mmmmm  'erpit'  'refs/heads/master'  0xa1  0xa1  `0xa1  %.n
      %pending  ~  ~  ~  ~  ~zod  %session  %trusted  ~
      ~2026.10.15  ~2026.10.15
      %manual  4  `0xb1  `0x1111  0v9.eeeee  %vm  ~  %.n
      :*  %dispatch  'manual.yml'  'Manual release'
          :~  ['dry_run' %boolean %supplied `[%boolean %.n]]
              ['count' %number %supplied `[%number '0']]
              ['level' %choice %default `[%string 'warning']]
              ['target' %environment %absent ~]
          ==
          '{"inputs":{"dry_run":"false","count":"0","level":"warning"}}'
          'request-1'
      ==  ==
=/  lock-3=lock:ci-state-3
  :*  0x88  'erpit'  0xb1  ~  ~2026.10.15  0v1.aaaaa  0  ~
      `~[['manual.yml' 'Manual release' ~[['dry_run' %boolean '' %.n `'false' ~]] ~]]
  ==
=/  s2-unlocked  s2-state(locks ~)
=/  manual-state=state-3:ci-state-3
  ;;  state-3:ci-state-3
  :*  %3
      (my ~[[id.manual-cand manual-cand]])
      +>.s2-unlocked
  ==
=.  locks.manual-state  (my ~[[0x88 lock-3]])
::  state-3 with a plan: a job gated by an %output-eq on another, and its
::  running attempt with the outputs it recorded by name
::
=/  job-a-3=job:ci-state-3  ['a' 'chain.yml' 'chain' 0 ~ ~ ~ ~ ~]
=/  job-b-3=job:ci-state-3  ['b' 'chain.yml' 'chain' 1 ~['a'] `[%output-eq 'a' 'go' 'true'] ~ ~ `5]
=/  planned-3=candidate:ci-state-3
  %=  manual-cand
    id  0v3.ppppp
    mode  %required
    trigger  [%push ~]
    attempts  ~[0v7.aaaaa]
    plan  `~[job-a-3 job-b-3]
  ==
=/  running-3=attempt:ci-state-3
  ;;(attempt:ci-state-3 r-running-2(outputs (my ~[['go' 'true'] ['n' '0']])))
=/  planned-state=state-3:ci-state-3  manual-state(candidates (my ~[[0v3.ppppp planned-3]]))
=.  attempts.planned-state  (my ~[[0v7.aaaaa running-3]])
::  every load that must succeed
::
=/  b  (load:ci-migrate base-state)
=/  r  (load:ci-migrate r6-state)
=/  q  (load:ci-migrate q11-state)
=/  s  (load:ci-migrate s1-state)
=/  c  (load:ci-migrate new.q)
=/  t  (load:ci-migrate s2-state)
=/  m  (load:ci-migrate manual-state)
=/  p  (load:ci-migrate planned-state)
=/  p-cand  (~(got by candidates.new.p) 0v3.ppppp)
::  state-4 (Delivery 1C): the planned required run of state-3 as state-4
::  holds it, with the manual run beside it and the lock with its
::  inventory
::
=/  s4-plain=state-4:ci-state-4  (from-state-3:ci-migrate planned-state)
=/  s4=state-4:ci-state-4
  %=  s4-plain
    candidates  %+  ~(put by candidates.s4-plain)
                  id.manual-cand
                (candidate-from-3:ci-migrate manual-cand)
  ==
=/  f  (load:ci-migrate s4)
=/  f-plain  (load:ci-migrate s4-plain)
=/  f-cand  (~(got by candidates.new.f) 0v3.ppppp)
=/  f-manual  (~(got by candidates.new.f) id.manual-cand)
=/  f-lock  (~(got by locks.new.f) 0x88)
::  the same state-4 as the state-7 it converts through (lib/ci-migrate
::  +load-7): the frozen shape the state-5 fixture below is built from
=/  f7  (load-7:ci-migrate s4)
=/  f7-cand  (~(got by candidates.new.f7) 0v3.ppppp)
=/  f7-lock  (~(got by locks.new.f7) 0x88)
::  a state-5 candidate's state-4 fields
::
=/  as-4
  |=  c=candidate:ci
  :*  id.c  repo.c  ref.c  head.c  base.c  candidate.c  conflict.c  status.c
      attempts.c  plan.c  plan-oid.c  verdict-reason.c  actor.c  via.c  trust.c
      pull.c  created.c  updated.c  mode.c  generation.c  baseline.c  lock.c
      incarnation.c  sandbox.c  trial-of.c  harness-differs.c  trigger.c
  ==
=/  p-running  (~(got by attempts.new.p) 0v7.aaaaa)
=/  m-cand  (~(got by candidates.new.m) id.manual-cand)
=/  m-lock  (~(got by locks.new.m) 0x88)
::  a state-4 attempt as state-3 held it: its step outputs by name
::
=/  attempt-as-3
  |=  a=attempt:ci
  :*  id.a  candidate.a  assignment.a  daemon.a  trust.a  kind.a  workflow.a  job.a
      status.a  events.a
      (malt (turn step-outputs.a |=(o=step-output:ci [name.o value.o])))
      job-result.a  result.a  reason.a  projection-name.a  log.a  started.a  finished.a
      generation.a  mode.a  manifest.a  sandbox.a  network.a  approval.a  outcome.a  pinned.a
  ==
=/  s-running  (~(got by attempts.new.s) 0v7.aaaaa)
=/  t-running  (~(got by attempts.new.t) 0v7.aaaaa)
::  a state-3 candidate's state-2 fields, and a lock's
::
=/  as-2
  |=  c=candidate:ci
  :*  id.c  repo.c  ref.c  head.c  base.c  candidate.c  conflict.c  status.c
      attempts.c  plan.c  plan-oid.c  verdict-reason.c  actor.c  via.c  trust.c
      pull.c  created.c  updated.c  mode.c  generation.c  baseline.c  lock.c
      incarnation.c  sandbox.c  trial-of.c  harness-differs.c
  ==
=/  lock-as-2
  |=  l=lock:ci
  [digest.l repo.l revision.l nodes.l resolved.l resolver.l bytes.l notices.l]
::  every candidate of a converted state is its earlier record, a push run
::
=/  kept-as-push
  |=  [old=(map candidate-id:ci *) new=(map candidate-id:ci candidate:ci)]
  ^-  ?
  ?&  =(~(key by old) ~(key by new))
      %+  levy  ~(tap by new)
      |=  [id=candidate-id:ci c=candidate:ci]
      ?&  =([%push ~] trigger.c)
          =((~(got by old) id) (as-2 c))
      ==
  ==
=/  t-lock  (~(got by locks.new.t) 0x77)
=/  want-attempts  (my ~[[0v7.aaaaa r-running-2]])
=/  b-active  (~(got by daemons.new.b) 0v1.aaaaa)
=/  b-revoked  (~(got by daemons.new.b) 0v2.bbbbb)
=/  b-cand  (~(got by candidates.new.b) 0v3.ccccc)
=/  b-running  (~(got by attempts.new.b) 0v7.aaaaa)
::  a load that must be refused: its crash
::
=/  refused
  |=  old=*
  ^-  ?
  =(~ (mole |.((load:ci-migrate old))))
::  the cases, in two lists (one literal of them all is past what the
::  pinned analyzer types in one binding)
::
=/  early=(list [name=@t expect=? got=?])
  :~  ::  the committed base
    ['the committed base loads as its own shape' %.y =(%base shape.b)]
    ['an enrolled daemon keeps its id, token and bearer hashes' %.y &(=(id.active id.b-active) =(token-hash.active token-hash.b-active) =(bearer-hash.active bearer-hash.b-active))]
    ['an enrolled daemon keeps its enrollment, capacity, labels, repositories and running attempts' %.y &(=(enrolled.active enrolled.b-active) =(capacity.active capacity.b-active) =(labels.active labels.b-active) =(repos.active repos.b-active) =(running.active running.b-active))]
    ['a revoked daemon stays revoked, its bearer cleared, its refusal kept' %.y &(=(revoked.revoked revoked.b-revoked) =(~ bearer-hash.b-revoked) =(refused.revoked refused.b-revoked))]
    ['a revoked daemon is not made active' %.n =(~ revoked.b-revoked)]
    ['a migrated daemon declares no network profile and no resolver' %.y &(=(~ profiles.b-active) !resolver.b-active)]
    ['the candidate keeps its history' %.y &(=(repo.cand repo.b-cand) =(ref.cand ref.b-cand) =(head.cand head.b-cand) =(status.cand status.b-cand) =(attempts.cand attempts.b-cand) =(actor.cand actor.b-cand) =(trust.cand trust.b-cand) =(created.cand created.b-cand) =(updated.cand updated.b-cand))]
    ['a migrated candidate names no baseline, no lock and no generation' %.y &(=(~ baseline.b-cand) =(~ lock.b-cand) =(0 generation.b-cand) =(%required mode.b-cand))]
    ['a migrated candidate is bound to a baseline' %.n !=(~ baseline.b-cand)]
    ['a running attempt stays running on its daemon' %.y &(=(%running status.b-running) =(daemon.running daemon.b-running) =(started.running started.b-running) =(events.running events.b-running))]
    ['a migrated attempt carries no manifest and no approval, its outcome known' %.y &(=(0v0 manifest.b-running) =(~ approval.b-running) =(%known outcome.b-running))]
    ['a migrated attempt has no pinned addresses' %.y =(~ pinned.b-running)]
    ['the assignment, protected refs, policies and credentials are kept' %.y &(=(assignments.base-state assignments.new.b) =(ci-protected.base-state ci-protected.new.b) =(policies.base-state policies.new.b) =(credentials.base-state credentials.new.b))]
    ['the signing key and ship keys are kept' %.y &(=(signing.base-state signing.new.b) =(ship-keys.base-state ship-keys.new.b))]
    ['P4 and Q11 bookkeeping starts empty' %.y &(=(~ incarnations.new.b) =(~ generations.new.b) =(~ baselines.new.b) =(~ approvals.new.b) =(~ overrides.new.b) =(~ audit.new.b) =(~ recovery-reports.new.b) =(~ recovery-commands.new.b))]
    ::  review 06's schema
    ['review 06 loads as its own shape' %.y =(%review06 shape.r)]
    ['an expired approval stays expired and a consumed one consumed' %.y =(approvals.r6-state approvals.new.r)]
    ['overrides, roles, environments, baselines and the audit are kept' %.y &(=(overrides.r6-state overrides.new.r) =(roles.r6-state roles.new.r) =(environments.r6-state environments.new.r) =(baselines.r6-state baselines.new.r) =(audit.r6-state audit.new.r))]
    ['review 06 daemons are kept whole' %.y =(daemons.r6-state daemons.new.r)]
    ['review 06 candidates are kept whole, each a push run' %.y (kept-as-push candidates.r6-state candidates.new.r)]
    ['a review 06 attempt is kept whole, unpinned' %.y =(want-attempts (~(run by attempts.new.r) attempt-as-3))]
    ['no retention report and no recovery command yet' %.y &(=(~ recovery-reports.new.r) =(~ recovery-commands.new.r))]
    ::  the Q11 schema
    ['the Q11 schema loads as its own shape' %.y =(%q11 shape.q)]
    ['the Q11 daemons and assignments are kept' %.y =([daemons assignments]:q11-state [daemons assignments]:new.q)]
    ['the Q11 candidates are kept, each a push run' %.y (kept-as-push candidates.q11-state candidates.new.q)]
    ::  every Q11 field after the attempts, each by name (state-5 appends
    ::  three maps after them, which a raw +63 tail would wrongly compare;
    ::  they are checked empty on their own, in .added)
    ['the Q11 fields after the attempts are kept' %.y =([ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:q11-state [ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:new.q)]
    ['a Q11 attempt is kept whole, with no pinned addresses' %.y =(want-attempts (~(run by attempts.new.q) attempt-as-3))]
    ['an open and a final recovery command are kept' %.y =(recovery-commands.q11-state recovery-commands.new.q)]
    ::  state-1
    ['state-1 loads as its own shape' %.y =(%state-1 shape.s)]
    ['state-1 converts to what its Q11 fields convert to' %.y =(new.s new.q)]
    ['state-1 is tagged %10 once converted' %.y =(%10 -.new.s)]
    ['a state-1 attempt gains pinned addresses' %.n !=(~ pinned.s-running)]
    ::  state-2
    ['state-2 loads as its own shape' %.y =(%state-2 shape.t)]
    ['state-2 is tagged %10 once converted' %.y =(%10 -.new.t)]
    ['a state-2 candidate is kept whole, a push run' %.y (kept-as-push candidates.s2-state candidates.new.t)]
    ['a state-2 candidate becomes a manual run' %.n (lien ~(val by candidates.new.t) |=(c=candidate:ci ?=(%dispatch -.trigger.c)))]
    ['a state-2 lock is kept whole' %.y =(lock-2 (lock-as-2 t-lock))]
    ['a state-2 lock has no manual-run inventory, which is not an empty one' %.y =(~ inventory.t-lock)]
    ['a state-2 attempt keeps its pinned addresses and everything else' %.y &(=(pins pinned.t-running) =(attempts.s2-state (~(run by attempts.new.t) attempt-as-3)))]
    ['every other state-2 field is kept' %.y =([daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:s2-state [daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:new.t)]
    ::  the current state
    ['the current state reloads unchanged' %.y &(=(%current shape.c) =(new.q new.c))]
    ::  state-3
    ['a manual run\'s state-3 loads as its own shape' %.y =(%state-3 shape.m)]
    ['state-3 is tagged %10 once converted' %.y =(%10 -.new.m)]
    ['a manual run keeps its event, inputs and request' %.y =(trigger.manual-cand trigger.m-cand)]
    ::  the old candidate's every field after its id, against the new one's
    ::  same fields by name (+as-4); its added selection is checked in .added
    ['a manual run keeps every other field' %.y =(+.manual-cand +:(as-4 m-cand))]
    ::  the old lock whole, against the new lock's same fields by name; its
    ::  added trigger catalog is checked in .added
    ['a lock keeps its manual-run inventory' %.y =(lock-3 [digest repo revision nodes resolved resolver bytes notices inventory]:m-lock)]
    ['every other state-3 field is kept' %.y =([daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:manual-state [daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:new.m)]
    ['a planned job keeps its output-eq gate, its declared outputs not known' %.y =(plan.p-cand `~[['a' 'chain.yml' 'chain' 0 ~ ~ ~ ~ ~ ~] ['b' 'chain.yml' 'chain' 1 ~['a'] `[%output-eq 'a' 'go' 'true'] ~ ~ `5 ~]])]
    ['a planned job gains declared outputs it never read' %.n !=(~ outputs:(snag 1 (need plan.p-cand)))]
    ['an attempt\'s outputs become step outputs of no named step, each kept' %.y =((silt step-outputs.p-running) (silt `(list step-output:ci)`~[[~ 'go' 'true' %.n] [~ 'n' '0' %.n]]))]
    ['a running attempt stays running with no decision, needs context or resolved outputs' %.y &(=(%running status.p-running) =(~ decision.p-running) =(~ needs.p-running) =(~ job-outputs.p-running))]
    ['every other field of the attempt is kept' %.y =(running-3 (attempt-as-3 p-running))]
  ==
::  Populated state5: keep its event run, catalog, source event, replay id
::  and armed schedule, plus every field inherited from the older fixture.
=/  historical=candidate:ci-state-5
  %=  f7-cand
    mode  %event
    trigger  [%event [0v91 %push 'refs/tags/v1' ~ `0xa1 ~ ~ ~zod 'tag API' ~2026.10.17 '{}']]
    selected  `['historical promoted catalog' ~[['old.yml' 'Old' %selected 'selected']] ~]
  ==
=/  event5=forge-event:ci-state-5
  [0v91 'erpit' %push 'refs/tags/v1' ~ `0xa1 ~ ~zod 'tag API' ~2026.10.17 ~2026.10.17 'selected' selected.historical ~[id.historical]]
=/  sched5=schedule-entry:ci-state-5
  =/  seed  *schedule-entry:ci-state-5
  %=  seed
    name  'Nightly'
    ref  'refs/heads/master'
    baseline  0xb1
    lock  0x88
    incarnation  0v9
    promoter  ~zod
    armed  `~2026.10.18
    admitted  ~[0v92]
    stale-wakes  2
    last-admitted  `[~2026.10.16 ~2026.10.16 0v93 %.n]
  ==
=/  s5=state-5:ci-state-5
  =/  seed  (from-state-4:ci-migrate s4)
  %=  seed
    candidates  (~(put by candidates.seed) id.historical historical)
    locks  (~(put by locks.seed) 0x88 f7-lock(triggers `*trigger-catalog:ci-state-5))
    forge-events  (my ~[[0v91 event5]])
    event-ids  (my ~[[0v91 ~2026.10.17]])
    schedules  (my ~[[['erpit' 'nightly.yml' 0] sched5]])
  ==
=/  fifth  (load:ci-migrate s5)
=/  fifth-7  (load-7:ci-migrate s5)
=/  s6=state-6:ci-state-6
  =/  old-six  (from-state-5:ci-migrate s5)
  old-six(tag-runs (my ~[[id.historical `0v7.aaaaa] [0v94 ~]]))
=/  sixth  (load:ci-migrate s6)
=/  sixth-7  (load-7:ci-migrate s6)
::  state-7 → state-8 (CI-CONCURRENCY-1): the state-7 the populated
::  state-6 converts to, with an open run and a closed one, an attempt
::  holding a decision, automatic-input records and a lock whose catalog
::  lists a workflow, loaded as state-8: every nested record converted
::  field by field, nothing joined, nothing cancelled, nothing granted
::
=/  s6-as-7=state-7:ci-state-7  new:(load-7:ci-migrate s6)
=/  s7=state-7:ci-state-7
  =/  open=candidate:ci-state-7  (~(got by candidates.s6-as-7) 0v3.ppppp)
  =/  closed=candidate:ci-state-7  (~(got by candidates.s6-as-7) id.manual-cand)
  =/  decided=attempt:ci-state-7  (~(got by attempts.s6-as-7) 0v7.aaaaa)
  =/  catalogued=lock:ci-state-7  (~(got by locks.s6-as-7) 0x88)
  %=  s6-as-7
    candidates
      %-  ~(gas by candidates.s6-as-7)
      :~  [0v3.ppppp open(status %pending)]
          [id.manual-cand closed(status %passed)]
      ==
    attempts
      %+  ~(put by attempts.s6-as-7)  0v7.aaaaa
      decided(decision `[%run 'every prerequisite succeeded' ~])
    locks
      %+  ~(put by locks.s6-as-7)  0x77
      catalogued(digest 0x77, triggers `[~[['ci.yml' 'CI' ~ ~]] ~])
    automatic-inputs  (my ~[['erpit' ~['https://example.com/a']]])
    input-snapshots  (my ~[[0v94 ~['https://example.com/a']]])
    input-evidence  (my ~[[0x77 '{"policyId":"x"}']])
    resolver-inputs  (my ~[[0v1.aaaaa ~['https://example.com/a']]])
  ==
=/  eighth  (load-8:ci-migrate s7)
=/  e-open  (~(got by candidates.new.eighth) 0v3.ppppp)
=/  e-closed  (~(got by candidates.new.eighth) id.manual-cand)
::  a state-8 candidate's state-7 fields
::
=/  as-7
  |=  c=candidate:ci-state-8
  :*  id.c  repo.c  ref.c  head.c  base.c  candidate.c  conflict.c  status.c
      attempts.c  plan.c  plan-oid.c  verdict-reason.c  actor.c  via.c  trust.c
      pull.c  created.c  updated.c  mode.c  generation.c  baseline.c  lock.c
      incarnation.c  sandbox.c  trial-of.c  harness-differs.c  trigger.c  selected.c
  ==
=/  lock-as-7
  |=  l=lock:ci-state-8
  [digest repo revision nodes resolved resolver bytes notices inventory]:l
=/  old-lock-as-7
  |=  l=lock:ci-state-7
  [digest repo revision nodes resolved resolver bytes notices inventory]:l
=/  eight=(list [name=@t expect=? got=?])
  :~  ['state-7 loads as its own frozen shape' %.y =(%state-7 shape.eighth)]
      ['state-7 is tagged %8 once converted' %.y =(%8 -.new.eighth)]
      ['every state-7 candidate keeps every field' %.y =(candidates.s7 (~(run by candidates.new.eighth) as-7))]
      ['an open run takes part in no group, and says why' %.y =([%exempt 'in flight when concurrency groups were added: this run takes part in no group'] concurrency.e-open)]
      ['a closed run has no group recorded' %.y =([%unknown 'finished before concurrency groups were added: no group was recorded for it'] concurrency.e-closed)]
      ['no run gains a cancellation' %.y (levy ~(val by candidates.new.eighth) |=(c=candidate:ci-state-8 =(~ cancelled.c)))]
      ['a run joins a group on upgrade' %.n (lien ~(val by candidates.new.eighth) |=(c=candidate:ci-state-8 ?=(?(%pending %joined) -.concurrency.c)))]
      ['every attempt keeps every field, its decision too' %.y =(attempts.s7 attempts.new.eighth)]
      ['a running attempt stays running' %.y =(%running status:(~(got by attempts.new.eighth) 0v7.aaaaa))]
      ['every lock keeps its digest, nodes and inventory' %.y =((~(run by locks.s7) old-lock-as-7) (~(run by locks.new.eighth) lock-as-7))]
      ['an old lock\'s catalog workflow reads its concurrency as unknown' %.y =(`[~[['ci.yml' 'CI' ~ ~ ~]] ~] triggers:(~(got by locks.new.eighth) 0x77))]
      ['an old lock\'s catalog workflow reads "no group"' %.n =(`[~[['ci.yml' 'CI' ~ ~ `[%none ~]]] ~] triggers:(~(got by locks.new.eighth) 0x77))]
      ['every other state-7 field is kept' %.y =([daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:s7 [daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:new.eighth)]
      ['state-8 reloads unchanged' %.y =(new.eighth new:(load-8:ci-migrate new.eighth))]
      ['a state-7 tagged %8 is refused' %.y (refused [%8 +.s7])]
      ['a state-8 tagged %7 is refused' %.y (refused [%7 +.new.eighth])]
      ['a %7 state with a corrupt locks map is refused' %.y (refused s7(locks [5 ~]))]
      ['state-6 converts to state-10 through state-7, state-8 and state-9' %.y =(new.sixth (from-state-9:ci-migrate (from-state-8:ci-migrate (from-state-7:ci-migrate new.sixth-7))))]
      ['state-5 converts to state-10 through state-7, state-8 and state-9' %.y =(new.fifth (from-state-9:ci-migrate (from-state-8:ci-migrate (from-state-7:ci-migrate new.fifth-7))))]
  ==
::  state-8 → state-9 (pull request labels): the populated state-8 the
::  state-7 converts to, with a pull request event run and a recorded
::  pull request forge event added, loaded as the state-9 it converts
::  through (+load-9).  the pull facts gain
::  no labels and no label, the payload text is kept byte for byte, and
::  every other field is kept
::
=/  old-facts=pull-facts:ci-state-8
  [7 'Add x' 'synchronize' 0xd1 'refs/heads/feature' 'refs/heads/master' 0xb1 %.n ~nec 'erpit']
=/  new-facts=pull-facts:ci
  [7 'Add x' 'synchronize' 0xd1 'refs/heads/feature' 'refs/heads/master' 0xb1 %.n ~nec 'erpit' ~ ~]
=/  old-payload=@t  '{"action":"synchronize","number":7,"pull_request":{"number":7}}'
=/  pull-run=candidate:ci-state-8
  %=  e-open
    id  0v95
    mode  %event
    pull  `7
    trigger  [%event [0v96 %pull-request 'refs/heads/master' ~ `0xd1 `old-facts ~ ~nec 'peer' ~2026.10.18 old-payload]]
  ==
=/  pull-event=forge-event:ci-state-8
  :*  0v96  'erpit'  %pull-request  'refs/heads/master'  ~  `0xd1  `old-facts  ~nec
      'peer'  ~2026.10.18  ~2026.10.18  '1 workflow(s) selected: run 0v95'  ~  ~[0v95]
  ==
=/  s8=state-8:ci-state-8
  %=  new.eighth
    candidates  (~(put by candidates.new.eighth) 0v95 pull-run)
    forge-events  (~(put by forge-events.new.eighth) 0v96 pull-event)
  ==
=/  ninth  (load-9:ci-migrate s8)
=/  n-run=candidate:ci  (~(got by candidates.new.ninth) 0v95)
=/  n-event=forge-event:ci  (~(got by forge-events.new.ninth) 0v96)
=/  event-of
  |=  c=candidate:ci
  ^-  (unit run-event:ci)
  ?.  ?=(%event -.trigger.c)  ~
  `run-event.trigger.c
=/  old-event-of
  |=  c=candidate:ci-state-8
  ^-  (unit run-event:ci-state-8)
  ?.  ?=(%event -.trigger.c)  ~
  `run-event.trigger.c
::  a candidate's fields but its trigger, in either shape
=/  rest-8
  |=  c=candidate:ci-state-8
  :*  id.c  repo.c  ref.c  head.c  base.c  candidate.c  conflict.c  status.c
      attempts.c  plan.c  plan-oid.c  verdict-reason.c  actor.c  via.c  trust.c
      pull.c  created.c  updated.c  mode.c  generation.c  baseline.c  lock.c
      incarnation.c  sandbox.c  trial-of.c  harness-differs.c  selected.c
      concurrency.c  cancelled.c
  ==
=/  rest-9
  |=  c=candidate:ci
  :*  id.c  repo.c  ref.c  head.c  base.c  candidate.c  conflict.c  status.c
      attempts.c  plan.c  plan-oid.c  verdict-reason.c  actor.c  via.c  trust.c
      pull.c  created.c  updated.c  mode.c  generation.c  baseline.c  lock.c
      incarnation.c  sandbox.c  trial-of.c  harness-differs.c  selected.c
      concurrency.c  cancelled.c
  ==
=/  nine=(list [name=@t expect=? got=?])
  :~  ['state-8 loads as its own frozen shape' %.y =(%state-8 shape.ninth)]
      ['state-8 is tagged %9 once converted' %.y =(%9 -.new.ninth)]
      :+  'an old pull request run\'s facts gain no labels and no label'  %.y
      =(``new-facts (bind (event-of n-run) |=(e=run-event:ci pull.e)))
      :+  'an old payload is never rewritten'  %.y
      =(`old-payload (bind (event-of n-run) |=(e=run-event:ci payload.e)))
      :+  'the rest of the run\'s event is kept'  %.y
      .=  %+  bind  (old-event-of pull-run)
          |=(e=run-event:ci-state-8 [id kind ref before after occurrence actor route at payload]:e)
      %+  bind  (event-of n-run)
      |=(e=run-event:ci [id kind ref before after occurrence actor route at payload]:e)
      :+  'a recorded pull request event\'s facts gain no labels and no label'  %.y
      =(`new-facts pull.n-event)
      :+  'the rest of the recorded event is kept'  %.y
      .=  [id repo kind ref before after actor route at received outcome selected runs]:pull-event
      [id repo kind ref before after actor route at received outcome selected runs]:n-event
      :+  'every candidate keeps every field but its trigger'  %.y
      =((~(run by candidates.s8) rest-8) (~(run by candidates.new.ninth) rest-9))
      :+  'a run that is no pull request event keeps its trigger as it was'  %.y
      .=  trigger:(~(got by candidates.s8) id.manual-cand)
      trigger:(~(got by candidates.new.ninth) id.manual-cand)
      :+  'every other state-8 field is kept'  %.y
      .=  [daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:s8
      [daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:new.ninth
      :+  'state-9 reloads as its own frozen shape, not as the current state'  %.y
      ?&  =(%state-9 shape:(load-9:ci-migrate new.ninth))
          =(new.ninth new:(load-9:ci-migrate new.ninth))
      ==
      ['state-7 converts to state-9 through state-8' %.y =(new:(load-9:ci-migrate s7) (from-state-8:ci-migrate new.eighth))]
      ['a state-8 with pull facts tagged %9 is refused' %.y (refused [%9 +.s8])]
      ['a state-9 with labelled pull facts tagged %8 is refused' %.y (refused [%8 +.new.ninth])]
      ['a %8 state with a corrupt forge events map is refused' %.y (refused s8(forge-events [5 ~]))]
  ==
::  state-9 → state-10 (CI-RECOVERY-1): the populated state-9 above,
::  loaded as state-10.  every one of its thirty-six fields is kept, and
::  the recovery record is the bunt: unpaused, with no checkpoint, no
::  staged import, no restore and no history
::
=/  ten=(list [name=@t expect=? got=?])
  =/  s9=state-9:ci-state-9  new.ninth
  =/  tenth  (load:ci-migrate s9)
  =/  busy=state-10:ci
    new.tenth(paused.recovery `[~2026.10.21 ~zod 'a restore drill'], sequence.recovery 3)
  :~  ['state-9 loads as its own frozen shape' %.y =(%state-9 shape.tenth)]
      ['state-9 is tagged %10 once converted' %.y =(%10 -.new.tenth)]
      :+  'every state-9 field is kept'  %.y
      .=  [candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:s9
      [candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:new.tenth
      ['the populated state-9 has candidates, attempts and forge events' %.y &(!=(~ candidates.s9) !=(~ attempts.s9) !=(~ forge-events.s9))]
      ['an old ship loads unpaused' %.y =(~ paused.recovery.new.tenth)]
      ['an old ship loads paused' %.n ?=(^ paused.recovery.new.tenth)]
      ['an old ship loads with no checkpoint' %.y =(~ checkpoints.recovery.new.tenth)]
      ['an old ship loads with no staged import, restore or history' %.y &(=(~ staged.recovery.new.tenth) =(~ restores.recovery.new.tenth) =(*restored-history:ci history.recovery.new.tenth))]
      ['the recovery record is exactly the bunt' %.y =(*recovery-state:ci recovery.new.tenth)]
      :+  'state-10 reloads unchanged, as the current state'  %.y
      ?&  =(%current shape:(load:ci-migrate new.tenth))
          =(new.tenth new:(load:ci-migrate new.tenth))
      ==
      ['a paused state-10 with a counter reloads unchanged' %.y =(busy new:(load:ci-migrate busy))]
      ['state-8 converts to state-10 through state-9' %.y =(new:(load:ci-migrate s8) (from-state-9:ci-migrate new.ninth))]
      ['a state-9 tagged %10 is refused' %.y (refused [%10 +.s9])]
      ['a state-10 tagged %9 is refused' %.y (refused [%9 +.new.tenth])]
      ['a %10 state with a corrupt recovery record is refused' %.y (refused new.tenth(recovery [5 5]))]
      ['a %9 state with a corrupt audit is refused' %.y (refused s9(audit [5 5]))]
  ==
=/  late=(list [name=@t expect=? got=?])
  :~  ::  state-4 (Delivery 1C)
    ['state-4 loads as its own shape' %.y =(%state-4 shape.f)]
    ['state-4 is tagged %10 once converted' %.y =(%10 -.new.f)]
    ['a state-4 required run keeps every field' %.y =((~(got by candidates.s4) 0v3.ppppp) (as-4 f-cand))]
    ['a state-4 required run\'s workflow selection was never evaluated' %.y =(~ selected.f-cand)]
    ['a state-4 manual run keeps every field and its dispatch' %.y &(=((~(got by candidates.s4) id.manual-cand) (as-4 f-manual)) ?=(%dispatch -.trigger.f-manual))]
    ['a state-4 candidate becomes an event run' %.n (lien ~(val by candidates.new.f) |=(c=candidate:ci ?=(%event -.trigger.c)))]
    ['a state-4 lock keeps every field and its inventory' %.y =((~(got by locks.s4) 0x88) [digest repo revision nodes resolved resolver bytes notices inventory]:f-lock)]
    ['a state-4 lock\'s trigger catalog is unknown, not empty' %.y =(~ triggers.f-lock)]
    ['a state-4 lock gains a trigger catalog' %.n !=(~ triggers.f-lock)]
    ['a state-4 running attempt is kept whole and running' %.y &(=((~(got by attempts.s4) 0v7.aaaaa) (~(got by attempts.new.f) 0v7.aaaaa)) =(%running status:(~(got by attempts.new.f) 0v7.aaaaa)))]
    ['no forge event, event id or schedule is invented' %.y &(=(~ forge-events.new.f) =(~ event-ids.new.f) =(~ schedules.new.f))]
    ['every other state-4 field is kept' %.y =([daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:s4 [daemons assignments ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands]:new.f)]
    ['state-4 converts as the state-3 it came from does' %.y =(new.p new.f-plain)]
    ['state5 is recognized as its own frozen shape' %.y =(%state-5 shape.fifth)]
    ['state5 copies every field without changing any noun' %.y =([candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules]:s5 [candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules]:new.fifth-7)]
    ['historical event runs gain no tagged authority' %.y =(~ tag-runs.new.fifth)]
    ['state6 keeps pending and exact resolver bindings on load' %.y &(=(%state-6 shape.sixth) =(tag-runs.s6 tag-runs.new.sixth))]
    ['state6 gains no policy snapshot evidence or runner ceiling' %.y =([~ ~ ~ ~] [automatic-inputs input-snapshots input-evidence resolver-inputs]:new.sixth)]
    ['state6 copies EVERY old field and nested noun' %.y =([candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs]:s6 [candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines locks roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs]:new.sixth-7)]
    ['truncated state6 is refused' %.y (refused [%6 +.s5])]
    ['state6 relabelled as state5 is refused' %.y (refused [%5 +.s6])]
    ['corrupt tag bindings are refused' %.y (refused s6(tag-runs [5 ~]))]
    ::  the current state
    ['the current state reloads unchanged' %.y &(=(%current shape.c) =(new.q new.c) =(%current -:(load:ci-migrate new.p)) =(new.p new:(load:ci-migrate new.p)) =(new.f new:(load:ci-migrate new.f)))]
    ::  refusals: nothing cast, reset or read another way
    ['an unknown version is refused' %.y (refused [%11 +.new.q])]
    ['a state-1 tagged %2 is refused' %.y (refused [%2 +.s1-state])]
    ['a state-2 tagged %1 is refused' %.y (refused [%1 +.s2-state])]
    ['a state-2 tagged %3 is refused' %.y (refused [%3 +.s2-state])]
    ['a state-3 tagged %2 is refused' %.y (refused [%2 +.manual-state])]
    ['a %2 state with a corrupt candidates map is refused' %.y (refused [%2 5 +>.s2-state])]
    ['a %3 state with a corrupt candidates map is refused' %.y (refused [%3 5 +>.manual-state])]
    ['a state-3 tagged %4 is refused' %.y (refused [%4 +.planned-state])]
    ['a state-4 tagged %3 is refused' %.y (refused [%3 +.s4])]
    ['a state-4 tagged %5 is refused' %.y (refused [%5 +.s4])]
    ['a state-6 tagged %4 is refused' %.y (refused [%4 +.new.p])]
    ['a %4 state with a corrupt candidates map is refused' %.y (refused [%4 5 +>.s4])]
    ['a %6 state with a corrupt attempts map is refused' %.y (refused new.p(attempts [5 ~]))]
    ['an atom is refused' %.y (refused 0)]
    ['a malformed %0 is refused' %.y (refused [%0 1 2 3])]
    ['a %0 state with a corrupt candidates map is refused' %.y (refused [%0 5 +>.q11-state])]
    ['a %1 state with a corrupt candidates map is refused' %.y (refused [%1 5 +>.s1-state])]
    ['a %0 state of the Q11 fields with its tag changed to %1 loads' %.y !(refused [%1 +.q11-state])]
    ['a %0 state of the Q11 fields loads as Q11, never as state-1' %.n =(%state-1 shape.q)]
    ['an ambiguous classification is refused' %.y =(%| -:(pick:ci-migrate ~[%base %q11]))]
    ['no shape found is refused' %.y =(%| -:(pick:ci-migrate ~))]
    ['exactly one shape found is chosen' %.y =([%& %q11] (pick:ci-migrate ~[%q11]))]
  ==
::  the fields state-5 adds, which the projections above set aside: each
::  has the initial value the conversion documents (lib/ci-migrate)
::
=/  added=(list [name=@t expect=? got=?])
  :~  ['a Q11 state gains no forge event, event id or schedule' %.y &(=(~ forge-events.new.q) =(~ event-ids.new.q) =(~ schedules.new.q))]
      ['a manual run\'s workflow selection was never evaluated' %.y =(~ selected.m-cand)]
      ['a state-3 lock\'s trigger catalog is unknown, not empty' %.y =(~ triggers.m-lock)]
      ['a state-3 gains no forge event, event id or schedule' %.y &(=(~ forge-events.new.m) =(~ event-ids.new.m) =(~ schedules.new.m))]
  ==
=/  cases=(list [name=@t expect=? got=?])  :(weld early added late eight nine ten)
=/  failed=(list @t)
  %+  murn  cases
  |=  [name=@t expect=? got=?]
  ?:(=(expect got) ~ `name)
=/  passed=@ud  (sub (lent cases) (lent failed))
~&  [%false-assertions-refused (lent (skim cases |=([* expect=? got=?] &(!expect !got)))) %of (lent (skim cases |=([* expect=? *] !expect)))]
~&  [%failed failed]
~&  (crip "passed={<passed>} of={<(lent cases)>}")
?=(~ failed)
