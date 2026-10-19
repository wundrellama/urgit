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
::  The current state (state-5) reloads unchanged.  An unknown version, an
::  atom, a malformed %0, a corrupt %0, %1, %2, %3, %4 or %5, a state-1
::  tagged %2, a state-2 tagged %1 or %3, a state-3 tagged %2 or %4, a
::  state-4 tagged %3 or %5 and a state-5 tagged %4 are refused, and so is
::  an ambiguous classification.  Every case names the verdict it expects;
::  the intentionally FALSE cases are refusals.  Prints `passed=N of=M`
::  and ends in a loobean.
::
::    NOT RUN in the source stage that wrote it: it needs a ship.
::
/-  ci, ci-state-1, ci-state-2, ci-state-3, ci-state-4, ci-state-base, ci-state-review06, ci-state-q11
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
    candidates  (~(put by candidates.s4-plain) id.manual-cand (candidate-from-3:ci-migrate manual-cand))
  ==
=/  f  (load:ci-migrate s4)
=/  f-plain  (load:ci-migrate s4-plain)
=/  f-cand  (~(got by candidates.new.f) 0v3.ppppp)
=/  f-manual  (~(got by candidates.new.f) id.manual-cand)
=/  f-lock  (~(got by locks.new.f) 0x88)
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
    ['state-1 is tagged %5 once converted' %.y =(%5 -.new.s)]
    ['a state-1 attempt gains pinned addresses' %.n !=(~ pinned.s-running)]
    ::  state-2
    ['state-2 loads as its own shape' %.y =(%state-2 shape.t)]
    ['state-2 is tagged %5 once converted' %.y =(%5 -.new.t)]
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
    ['state-3 is tagged %5 once converted' %.y =(%5 -.new.m)]
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
=/  late=(list [name=@t expect=? got=?])
  :~  ::  state-4 (Delivery 1C)
    ['state-4 loads as its own shape' %.y =(%state-4 shape.f)]
    ['state-4 is tagged %5 once converted' %.y =(%5 -.new.f)]
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
    ::  the current state
    ['state-5 reloads unchanged' %.y &(=(%current shape.c) =(new.q new.c) =(%current -:(load:ci-migrate new.p)) =(new.p new:(load:ci-migrate new.p)) =(new.f new:(load:ci-migrate new.f)))]
    ::  refusals: nothing cast, reset or read another way
    ['an unknown version is refused' %.y (refused [%6 +.new.q])]
    ['a state-1 tagged %2 is refused' %.y (refused [%2 +.s1-state])]
    ['a state-2 tagged %1 is refused' %.y (refused [%1 +.s2-state])]
    ['a state-2 tagged %3 is refused' %.y (refused [%3 +.s2-state])]
    ['a state-3 tagged %2 is refused' %.y (refused [%2 +.manual-state])]
    ['a %2 state with a corrupt candidates map is refused' %.y (refused [%2 5 +>.s2-state])]
    ['a %3 state with a corrupt candidates map is refused' %.y (refused [%3 5 +>.manual-state])]
    ['a state-3 tagged %4 is refused' %.y (refused [%4 +.planned-state])]
    ['a state-4 tagged %3 is refused' %.y (refused [%3 +.s4])]
    ['a state-4 tagged %5 is refused' %.y (refused [%5 +.s4])]
    ['a state-5 tagged %4 is refused' %.y (refused [%4 +.new.p])]
    ['a %4 state with a corrupt candidates map is refused' %.y (refused [%4 5 +>.s4])]
    ['a %5 state with a corrupt attempts map is refused' %.y (refused new.p(attempts [5 ~]))]
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
=/  cases=(list [name=@t expect=? got=?])  :(weld early added late)
=/  failed=(list @t)
  %+  murn  cases
  |=  [name=@t expect=? got=?]
  ?:(=(expect got) ~ `name)
=/  passed=@ud  (sub (lent cases) (lent failed))
~&  [%false-assertions-refused (lent (skim cases |=([* expect=? got=?] &(!expect !got)))) %of (lent (skim cases |=([* expect=? *] !expect)))]
~&  [%failed failed]
~&  (crip "passed={<passed>} of={<(lent cases)>}")
?=(~ failed)
