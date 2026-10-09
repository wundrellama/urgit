::  %urgit-ci's persisted state, from every supported shape to the current
::  one (state-migration ruling 01: Q12 A; QUESTIONS-SOURCE-01 §12;
::  specs/ci-execution-contract.md §8c).
::
::    four earlier shapes are known from this worktree's own sources,
::    each frozen byte for byte, every nested type with it, in a sur file
::    of its own: the committed base (sur/ci-state-base), review 06's
::    accepted schema (sur/ci-state-review06), the Q11 schema
::    (sur/ci-state-q11) and state-1 (sur/ci-state-1).  none is claimed to
::    have been deployed.  the first three carry the tag %0, so the tag
::    never decides: a %0 state must fit exactly one of them, as a noun.
::    state-1 is tagged %1.  the current state is state-2, tagged %2: an
::    attempt gains the addresses each granted DNS name was pinned to
::    (CI-P4-NET-1, pinned addresses shown per run).  a %0 shape is
::    converted to state-1, and state-1 to state-2.  an unknown, partial,
::    corrupt or ambiguous state is refused — never cast, reset or read
::    another way — and every conversion is written out field by field.
::
/-  ci, ci-state-1, ci-state-base, ci-state-review06, ci-state-q11
|%
::  the shapes a saved state may have
::
+$  shape  ?(%current %state-1 %q11 %review06 %base)
::
::  the one shape of those found, or why none is chosen
::
++  pick
  |=  found=(list shape)
  ^-  (each shape @t)
  ?~  found
    |+'a state of none of the supported shapes (the committed base, review 06, Q11): unknown, partial or corrupt'
  ?^  t.found
    |+(crip "an ambiguous state: it fits each of {<found>}")
  &+i.found
::
::  which shape .old is, as a noun, or why it is refused
::
++  shape-of
  |=  old=*
  ^-  (each shape @t)
  ?@  old  |+'the saved state is an atom, not a versioned state'
  ?+  -.old  |+(crip "the saved state's version {<-.old>} is not one this agent reads")
      %2
    ?:  !=(~ (mole |.(;;(state-2:ci old))))  &+%current
    |+'a %2 state that is not exactly state-2: partial or corrupt'
  ::
      %1
    ?:  !=(~ (mole |.(;;(state-1:ci-state-1 old))))  &+%state-1
    |+'a %1 state that is not exactly state-1: partial or corrupt'
  ::
      %0
    ::  every historical shape is tried: the tag is theirs alike
    =/  found=(list shape)  ~
    =?  found  !=(~ (mole |.(;;(state-0:ci-state-q11 old))))  [%q11 found]
    =?  found  !=(~ (mole |.(;;(state-0:ci-state-review06 old))))  [%review06 found]
    =?  found  !=(~ (mole |.(;;(state-0:ci-state-base old))))  [%base found]
    (pick found)
  ==
::
::  .old as the current state, and the shape it had; or a crash naming
::  why it is refused (on-load reports it, and the saved state is not
::  replaced)
::
++  load
  |=  old=*
  ^-  [=shape new=state-2:ci]
  =/  found=(each shape @t)  (shape-of old)
  ?:  ?=(%| -.found)
    ~|  %urgit-ci-state-refused
    ~|  p.found
    !!
  :-  p.found
  ?-  p.found
    %current  ;;(state-2:ci old)
    %state-1  (from-state-1 ;;(state-1:ci-state-1 old))
    %q11  (from-state-1 (from-q11 ;;(state-0:ci-state-q11 old)))
    %review06  (from-state-1 (from-review06 ;;(state-0:ci-state-review06 old)))
    %base  (from-state-1 (from-base ;;(state-0:ci-state-base old)))
  ==
::
::  state-1: its twenty-eight fields as they are, under the tag %2, and
::  each attempt with no pinned addresses — none was reported before
::  state-2
::
++  from-state-1
  |=  o=state-1:ci-state-1
  ^-  state-2:ci
  ;;  state-2:ci
  :*  %2
      candidates.o
      daemons.o
      assignments.o
      (~(run by attempts.o) attempt-from-1)
      ci-protected.o
      policies.o
      credentials.o
      signing.o
      ship-keys.o
      incarnations.o
      generations.o
      baselines.o
      locks.o
      roles.o
      environments.o
      approvals.o
      overrides.o
      network-policies.o
      read-capabilities.o
      shadows.o
      comparisons.o
      audit.o
      sandbox-requirements.o
      mirror-tokens.o
      resolve-mappings.o
      harness-paths.o
      recovery-reports.o
      recovery-commands.o
  ==
::
::  an attempt of state-1: every field as it was — a running one stays
::  running — and no pinned addresses
::
++  attempt-from-1
  |=  a=attempt:ci-state-1
  ^-  attempt:ci
  :*  id.a
      candidate.a
      assignment.a
      daemon.a
      trust.a
      kind.a
      workflow.a
      job.a
      status.a
      events.a
      outputs.a
      job-result.a
      result.a
      reason.a
      projection-name.a
      log.a
      started.a
      finished.a
      generation.a
      mode.a
      manifest.a
      sandbox.a
      network.a
      approval.a
      outcome.a
      ~
  ==
::
::  the Q11 schema: its twenty-eight fields as they are, under the tag %1
::  (state-1)
::
++  from-q11
  |=  o=state-0:ci-state-q11
  ^-  state-1:ci-state-1
  ;;  state-1:ci-state-1
  :*  %1
      candidates.o
      daemons.o
      assignments.o
      attempts.o
      ci-protected.o
      policies.o
      credentials.o
      signing.o
      ship-keys.o
      incarnations.o
      generations.o
      baselines.o
      locks.o
      roles.o
      environments.o
      approvals.o
      overrides.o
      network-policies.o
      read-capabilities.o
      shadows.o
      comparisons.o
      audit.o
      sandbox-requirements.o
      mirror-tokens.o
      resolve-mappings.o
      harness-paths.o
      recovery-reports.o
      recovery-commands.o
  ==
::
::  review 06's schema: its twenty-six fields as they are, and the Q11
::  stage's two maps empty — no retention report yet (a runner posts one
::  at its next reconcile) and no recovery command (none could exist)
::
++  from-review06
  |=  o=state-0:ci-state-review06
  ^-  state-1:ci-state-1
  ;;  state-1:ci-state-1
  :*  %1
      candidates.o
      daemons.o
      assignments.o
      attempts.o
      ci-protected.o
      policies.o
      credentials.o
      signing.o
      ship-keys.o
      incarnations.o
      generations.o
      baselines.o
      locks.o
      roles.o
      environments.o
      approvals.o
      overrides.o
      network-policies.o
      read-capabilities.o
      shadows.o
      comparisons.o
      audit.o
      sandbox-requirements.o
      mirror-tokens.o
      resolve-mappings.o
      harness-paths.o
      ~
      ~
  ==
::
::  the committed base: its nine fields, each record completed with what
::  P4 added, and every map P4 and the Q11 stage added empty — none of it
::  existed.  nothing migrated is current P4 evidence: a candidate names
::  no baseline, so bindings-current never holds for it, and it lands
::  only once staged again under P4.
::
++  from-base
  |=  o=state-0:ci-state-base
  ^-  state-1:ci-state-1
  ;;  state-1:ci-state-1
  :*  %1
      (~(run by candidates.o) candidate-from-base)
      (~(run by daemons.o) daemon-from-base)
      assignments.o
      (~(run by attempts.o) attempt-from-base)
      ci-protected.o
      policies.o
      credentials.o
      signing.o
      ship-keys.o
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
      ~
  ==
::
::  a daemon enrolled before P4: every field as it was — its id, token
::  and bearer hashes, enrollment, revocation, refusal, labels,
::  repositories and running attempts; a revoked daemon stays revoked —
::  with no network profiles (a daemon declares them again on every poll)
::  and no resolver capability (it enrolled without declaring one)
::
++  daemon-from-base
  |=  d=daemon:ci-state-base
  ^-  daemon:ci-state-1
  :*  id.d
      token-hash.d
      bearer-hash.d
      minted.d
      enrolled.d
      last-seen.d
      capacity.d
      sandbox.d
      running.d
      revoked.d
      refused.d
      labels.d
      repos.d
      ~
      %.n
  ==
::
::  a candidate staged before P4: every field as it was, and its P4
::  bindings none — the mode a pre-P4 candidate had to its ref, required,
::  but no baseline, no lock, generation 0 and incarnation 0v0; the VM, no
::  trial, the harness difference not computed
::
++  candidate-from-base
  |=  c=candidate:ci-state-base
  ^-  candidate:ci-state-1
  :*  id.c
      repo.c
      ref.c
      head.c
      base.c
      candidate.c
      conflict.c
      status.c
      attempts.c
      plan.c
      plan-oid.c
      verdict-reason.c
      actor.c
      via.c
      trust.c
      pull.c
      created.c
      updated.c
      %required
      0
      ~
      ~
      0v0
      %vm
      ~
      %.n
  ==
::
::  an attempt made before P4: every field as it was — a running one
::  stays running — and what its signed manifest would have carried none:
::  generation 0, required, no manifest (none was signed), the VM, no
::  network recorded, no approval; its outcome known, as its status and
::  result say
::
++  attempt-from-base
  |=  a=attempt:ci-state-base
  ^-  attempt:ci-state-1
  :*  id.a
      candidate.a
      assignment.a
      daemon.a
      trust.a
      kind.a
      workflow.a
      job.a
      status.a
      events.a
      outputs.a
      job-result.a
      result.a
      reason.a
      projection-name.a
      log.a
      started.a
      finished.a
      0
      %required
      0v0
      %vm
      ''
      ~
      %known
  ==
--
