::  ci-migration-vector: %urgit-ci's state migrations (state-migration
::  ruling 01: Q12 A; QUESTIONS-SOURCE-01 §12; contract §8c).  A
::  populated state of each supported historical shape — the committed
::  base, review 06's schema, the Q11 schema — is loaded by lib/ci-migrate
::  and checked field by field:
::    - enrollment and bearers, a revoked daemon kept revoked;
::    - trust, policies, credentials, the signing key and ship keys;
::    - candidates, assignments and a running attempt;
::    - an expired approval and a consumed one, an override, roles,
::      environments, a baseline and the audit;
::    - open and final recovery commands and a retention report.
::  The current state reloads unchanged.  An unknown version, an atom, a
::  malformed %0, a corrupt %0 and a corrupt %1 are refused, and so is an
::  ambiguous classification.  Every case names the verdict it expects;
::  the intentionally FALSE cases are refusals.  Prints `passed=N of=M`
::  and ends in a loobean.
::
::    NOT RUN in the source stage that wrote it: it needs a ship.
::
/-  ci, ci-state-base, ci-state-review06, ci-state-q11
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
::  every load that must succeed
::
=/  b  (load:ci-migrate base-state)
=/  r  (load:ci-migrate r6-state)
=/  q  (load:ci-migrate q11-state)
=/  c  (load:ci-migrate new.q)
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
=/  cases=(list [name=@t expect=? got=?])
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
    ['the assignment, protected refs, policies and credentials are kept' %.y &(=(assignments.base-state assignments.new.b) =(ci-protected.base-state ci-protected.new.b) =(policies.base-state policies.new.b) =(credentials.base-state credentials.new.b))]
    ['the signing key and ship keys are kept' %.y &(=(signing.base-state signing.new.b) =(ship-keys.base-state ship-keys.new.b))]
    ['P4 and Q11 bookkeeping starts empty' %.y &(=(~ incarnations.new.b) =(~ generations.new.b) =(~ baselines.new.b) =(~ approvals.new.b) =(~ overrides.new.b) =(~ audit.new.b) =(~ recovery-reports.new.b) =(~ recovery-commands.new.b))]
    ::  review 06's schema
    ['review 06 loads as its own shape' %.y =(%review06 shape.r)]
    ['an expired approval stays expired and a consumed one consumed' %.y =(approvals.r6-state approvals.new.r)]
    ['overrides, roles, environments, baselines and the audit are kept' %.y &(=(overrides.r6-state overrides.new.r) =(roles.r6-state roles.new.r) =(environments.r6-state environments.new.r) =(baselines.r6-state baselines.new.r) =(audit.r6-state audit.new.r))]
    ['review 06 daemons, candidates and attempts are kept whole' %.y &(=(daemons.r6-state daemons.new.r) =(candidates.r6-state candidates.new.r) =(attempts.r6-state attempts.new.r))]
    ['no retention report and no recovery command yet' %.y &(=(~ recovery-reports.new.r) =(~ recovery-commands.new.r))]
    ::  the Q11 schema
    ['the Q11 schema loads as its own shape' %.y =(%q11 shape.q)]
    ['every Q11 field is carried as it is' %.y =(+.q11-state +.new.q)]
    ['an open and a final recovery command are kept' %.y =(recovery-commands.q11-state recovery-commands.new.q)]
    ::  the current state
    ['the current state reloads unchanged' %.y &(=(%current shape.c) =(new.q new.c))]
    ::  refusals: nothing cast, reset or read another way
    ['an unknown version is refused' %.y (refused [%2 +.new.q])]
    ['an atom is refused' %.y (refused 0)]
    ['a malformed %0 is refused' %.y (refused [%0 1 2 3])]
    ['a %0 state with a corrupt candidates map is refused' %.y (refused [%0 5 +>.q11-state])]
    ['a %1 state with a corrupt candidates map is refused' %.y (refused [%1 5 +>.new.q])]
    ['a %0 state of the Q11 fields with its tag changed to %1 loads' %.y !(refused [%1 +.q11-state])]
    ['an ambiguous classification is refused' %.y =(%| -:(pick:ci-migrate ~[%base %q11]))]
    ['no shape found is refused' %.y =(%| -:(pick:ci-migrate ~))]
    ['exactly one shape found is chosen' %.y =([%& %q11] (pick:ci-migrate ~[%q11]))]
  ==
=/  failed=(list @t)
  %+  murn  cases
  |=  [name=@t expect=? got=?]
  ?:(=(expect got) ~ `name)
=/  passed=@ud  (sub (lent cases) (lent failed))
~&  [%false-assertions-refused (lent (skim cases |=([* expect=? got=?] &(!expect !got)))) %of (lent (skim cases |=([* expect=? *] !expect)))]
~&  [%failed failed]
~&  (crip "passed={<passed>} of={<(lent cases)>}")
?=(~ failed)
