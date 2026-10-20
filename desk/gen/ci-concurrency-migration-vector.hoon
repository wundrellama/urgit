::  Populated frozen state-7 -> state-8, executing the production loader.
/-  ci, ci-state-7
/+  ci-concurrency, ci-migrate, ci-plan
:-  %say
|=  *
:-  %noun
=/  old=state-7:ci-state-7  *state-7:ci-state-7
=/  c=candidate:ci-state-7
  =/  c  *candidate:ci-state-7
  c(id 0v1, repo 'erpit', status %pending, attempts ~[0v2], lock `0xa, generation 7, incarnation 0v8, candidate `0xbb)
=/  a=attempt:ci-state-7
  =/  a  *attempt:ci-state-7
  a(id 0v2, candidate 0v1, daemon 0v3, status %running, approval `0v4, workflow `'suite.yml', job `'build', outcome %known)
=/  l=lock:ci-state-7
  =/  l  *lock:ci-state-7
  %=  l
    digest  0xa
    repo  'erpit'
    revision  0xbb
    inventory  `~[['suite.yml' 'Suite' ~[['flag' %boolean 'retained' %.n `'false' ~]] ~]]
    triggers  `[~[['suite.yml' 'Suite' ~ ~]] ~]
  ==
=/  d=daemon:ci-state-7
  =/  d  *daemon:ci-state-7
  d(id 0v3, token-hash 0x111, bearer-hash `0x222, running (silt ~[0v2]))
=/  old
  %=  old
    candidates  (malt ~[[0v1 c]])
    daemons  (malt ~[[0v3 d]])
    attempts  (malt ~[[0v2 a]])
    assignments  (malt ~[[0v5 *assignment:ci-state-7]])
    locks  (malt ~[[0xa l]])
    credentials  (malt ~[[['erpit' 'DEPLOY'] ['retained-secret' %job ~ ~2026.10.19]]])
    policies  (malt ~[['erpit' %approval]])
    roles  (malt ~[['erpit' ~[[%ci-policy ~ (silt ~[~nec])]]]])
    approvals  (malt ~[[0v4 *approval:ci-state-7]])
    overrides  (malt ~[[0v6 *override:ci-state-7]])
    schedules  (malt ~[[['erpit' 'suite.yml' 0] *schedule-entry:ci-state-7]])
    tag-runs  (malt `(list [@uv (unit @uv)])`~[[0v1 `0v2] [0v7 ~]])
    generations  (malt ~[['erpit' 7]])
    incarnations  (malt ~[['erpit' 0v8]])
    ci-protected  (silt ~[['erpit' 'refs/heads/master']])
    automatic-inputs  (malt ~[['erpit' ~['https://inputs.example/tool']]])
    input-snapshots  (malt ~[[0v1 ~['https://inputs.example/tool']]])
    input-evidence  (malt ~[[0xa 'retained evidence']])
    resolver-inputs  (malt ~[[0v3 ~['https://inputs.example/tool']]])
    mirror-tokens  (malt ~[[0v2 (silt ~['mirror'])]])
    read-capabilities  (malt ~[[0x55 *read-capability:ci-state-7]])
    recovery-commands  (malt ~[[0v9 *recovery-command:ci-state-7]])
    recovery-reports  (malt ~[[0v3 *recovery-report:ci-state-7]])
  ==
=/  loaded  (load:ci-migrate old)
=/  next  new.loaded
=/  nl=lock:ci  (~(got by locks.next) 0xa)
?>  ?=(^ inventory.nl)
?>  ?=(^ u.inventory.nl)
?>  ?=(^ triggers.nl)
?>  ?=(^ workflows.u.triggers.nl)
=/  context=run-context:ci-plan  ['push' '' 'refs/heads/master' 'bb' '~zod' [%obj ~] %.n]
=/  upgraded
  %:  evaluate:ci-concurrency
    (~(got by candidates.next) 0v1)  'Suite'  `[~ ~ ~]  context
    (~(has in concurrency-legacy.next) 0v1)
  ==
=/  cases=(list [@t ?])
  :~  ['frozen state-7 is recognized and becomes state-8' &(=(%state-7 shape.loaded) =(%8 -.next))]
      ['candidate and in-flight privileged attempt are unchanged' &(=(c (~(got by candidates.next) 0v1)) =(a (~(got by attempts.next) 0v2)))]
      ['enrollment and running daemon obligations remain' =(daemons.old daemons.next)]
      ['all old fields outside locks remain byte-for-byte' =([candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:old [candidates daemons assignments attempts ci-protected policies credentials signing ship-keys incarnations generations baselines roles environments approvals overrides network-policies read-capabilities shadows comparisons audit sandbox-requirements mirror-tokens resolve-mappings harness-paths recovery-reports recovery-commands forge-events event-ids schedules tag-runs automatic-inputs input-snapshots input-evidence resolver-inputs]:next)]
      ['lock identity, content, resolver and notices remain' =([digest repo revision nodes resolved resolver bytes notices]:l [digest repo revision nodes resolved resolver bytes notices]:nl)]
      ['manual input history and refusals remain' =((turn (need inventory.l) |=(w=dispatch-workflow:ci-state-7 [workflow.w name.w inputs.w refusal.w])) (turn (need inventory.nl) |=(w=dispatch-workflow:ci [workflow.w name.w inputs.w refusal.w])))]
      ['old manual inventory concurrency is unknown' =(~ concurrency.i.u.inventory.nl)]
      ['old trigger catalog concurrency is unknown' =(~ concurrency.i.workflows.u.triggers.nl)]
      ['old run has no group membership' &(=(~ workflow-runs.next) (~(has in concurrency-legacy.next) 0v1) =(%unknown phase.upgraded) !=('' reason.upgraded))]
      ['state-8 reload is exact' =(next new:(load:ci-migrate next))]
      ['candidate bunt cannot become cancelled or passed' =(%unknown *candidate-status:ci)]
  ==
=/  failed=(list @t)  (murn cases |=([name=@t ok=?] ?:(ok ~ `name)))
~&  [%ci-concurrency-migration-failed failed]
?=(~ failed)
