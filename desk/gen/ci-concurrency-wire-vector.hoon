::  Captured REAL daemon POST fields -> production Hoon parsers and evaluator.
::  Generated from out/captures by source/.scratch/workflow/generate-concurrency-vectors.py.
::  Parent runs this generator; local Go tests produced the captured wire.
/-  ci
/+  ci-concurrency, ci-concurrency-wire, ci-dispatch, ci-plan, ci-trigger
:-  %say
|=  *
:-  %noun
=/  plan-json=json
  %-  need
  %-  de:json:html
  %+  rap  3
  :~  '{"oid":"0000000000000000000000000000000000000001","workflows-oid":"000000000000000000'
      '0000000000000000000001","workspace-sha256":"e96f27e8d5e27d60961409bd1503643cf9fca2891'
      'fdc1b4dd623cfb7be5ecc35","workflows":["fixture-chain.yml"],"jobs":[{"id":"a","workflo'
      'w":"fixture-chain.yml","name":"fixture-chain","stage":0,"needs":[],"cond":null,"outpu'
      'ts":[],"matrix":false,"concurrency":true,"events":["push","workflow_dispatch"],"runs-'
      'on":["ubuntu-latest"]}],"concurrency":[{"workflow":"fixture-chain.yml","name":"fixtur'
      'e-chain","concurrency":{"group":{"name":"group","v":2,"text":"${{ github.workflow }}-'
      '${{ github.ref }}","line":4,"col":10,"parts":[{"expr":{"k":"prop","of":{"k":"ctx","na'
      'me":"github"},"name":"workflow"}},{"lit":"-"},{"expr":{"k":"prop","of":{"k":"ctx","na'
      'me":"github"},"name":"ref"}}]},"cancelInProgress":{"v":2,"text":"${{ github.ref != \'r'
      'efs/heads/master\' }}","line":5,"col":23,"expr":{"k":"cmp","op":"ne","l":{"k":"prop","'
      'of":{"k":"ctx","name":"github"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}'
      '}},"refusal":""}}]}'
  ==
=/  triggers-json=json
  %-  need
  %-  de:json:html
  %+  rap  3
  :~  '{"version":1,"workflows":[{"workflow":"fixture-chain.yml","name":"fixture-chain","eve'
      'nts":[{"event":"push","refusal":"","branches":null,"tags":null,"paths":null,"types":n'
      'ull,"typesDeclared":false,"schedules":null,"line":2,"col":6},{"event":"workflow_dispa'
      'tch","refusal":"","branches":null,"tags":null,"paths":null,"types":null,"typesDeclare'
      'd":false,"schedules":null,"line":2,"col":12}],"refusal":"","concurrency":{"group":{"n'
      'ame":"group","v":2,"text":"${{ github.workflow }}-${{ github.ref }}","line":4,"col":1'
      '0,"parts":[{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"workflow"}},{'
      '"lit":"-"},{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"}}]},"can'
      'celInProgress":{"v":2,"text":"${{ github.ref != \'refs/heads/master\' }}","line":5,"col'
      '":23,"expr":{"k":"cmp","op":"ne","l":{"k":"prop","of":{"k":"ctx","name":"github"},"na'
      'me":"ref"},"r":{"k":"str","s":"refs/heads/master"}}},"refusal":""}}],"zones":{}}'
  ==
=/  dispatch-json=json
  %-  need
  %-  de:json:html
  %+  rap  3
  :~  '[{"workflow":"fixture-chain.yml","name":"fixture-chain","inputs":[],"concurrency":{"g'
      'roup":{"name":"group","v":2,"text":"${{ github.workflow }}-${{ github.ref }}","line":'
      '4,"col":10,"parts":[{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"work'
      'flow"}},{"lit":"-"},{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"'
      '}}]},"cancelInProgress":{"v":2,"text":"${{ github.ref != \'refs/heads/master\' }}","lin'
      'e":5,"col":23,"expr":{"k":"cmp","op":"ne","l":{"k":"prop","of":{"k":"ctx","name":"git'
      'hub"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}}},"refusal":""},"refusal"'
      ':"job a: job-level concurrency is not supported"}]'
  ==
=/  suite-json=json
  %-  need
  %-  de:json:html
  %+  rap  3
  :~  '{"group":{"name":"group","v":2,"text":"${{ github.workflow }}-${{ github.ref }}-${{ g'
      'ithub.ref == \'refs/heads/master\' && github.sha || \'head\' }}","line":47,"col":10,"part'
      's":[{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"workflow"}},{"lit":"'
      '-"},{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"}},{"lit":"-"},{'
      '"expr":{"k":"or","l":{"k":"and","l":{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"c'
      'tx","name":"github"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}},"r":{"k":'
      '"prop","of":{"k":"ctx","name":"github"},"name":"sha"}},"r":{"k":"str","s":"head"}}}]}'
      ',"cancelInProgress":{"v":2,"text":"${{ github.ref != \'refs/heads/master\' }}","line":4'
      '8,"col":23,"expr":{"k":"cmp","op":"ne","l":{"k":"prop","of":{"k":"ctx","name":"github'
      '"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}}},"refusal":""}'
  ==
=/  fixtures-json=json
  %-  need
  %-  de:json:html
  %+  rap  3
  :~  '{"group":{"name":"group","v":2,"text":"${{ github.workflow }}-${{ github.ref }}-${{ g'
      'ithub.ref == \'refs/heads/master\' && github.sha || \'head\' }}","line":91,"col":10,"part'
      's":[{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"workflow"}},{"lit":"'
      '-"},{"expr":{"k":"prop","of":{"k":"ctx","name":"github"},"name":"ref"}},{"lit":"-"},{'
      '"expr":{"k":"or","l":{"k":"and","l":{"k":"cmp","op":"eq","l":{"k":"prop","of":{"k":"c'
      'tx","name":"github"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}},"r":{"k":'
      '"prop","of":{"k":"ctx","name":"github"},"name":"sha"}},"r":{"k":"str","s":"head"}}}]}'
      ',"cancelInProgress":{"v":2,"text":"${{ github.ref != \'refs/heads/master\' }}","line":9'
      '2,"col":23,"expr":{"k":"cmp","op":"ne","l":{"k":"prop","of":{"k":"ctx","name":"github'
      '"},"name":"ref"},"r":{"k":"str","s":"refs/heads/master"}}},"refusal":""}'
  ==
=/  plan  (parse:ci-plan plan-json)
?>  ?=(%& -.plan)
=/  catalog  (parse-catalog:ci-trigger `triggers-json)
?>  ?=(%& -.catalog)
?>  ?=(^ p.catalog)
=/  inventory  (parse-inventory:ci-dispatch `dispatch-json)
?>  ?=(%& -.inventory)
?>  ?=(^ p.inventory)
=/  declarations  (need concurrency.p.plan)
?>  ?=(^ declarations)
?>  ?=(^ workflows.u.p.catalog)
?>  ?=(^ u.p.inventory)
=/  catalog-concurrency  concurrency.i.workflows.u.p.catalog
=/  inventory-concurrency  concurrency.i.u.p.inventory
=/  refused  (validate:ci-plan oid.p.plan workflows.p.plan p.plan)
=/  suite  (parse:ci-concurrency-wire `suite-json)
=/  fixtures  (parse:ci-concurrency-wire `fixtures-json)
?>  ?=(%& -.suite)
?>  ?=(%& -.fixtures)
=/  candidate
  |=  [id=@uv ref=@t sha=@ux]
  ^-  candidate:ci
  =/  c  *candidate:ci
  c(id id, repo 'erpit', ref ref, head sha, candidate `sha, mode %event, status %pending)
=/  evaluate
  |=  [c=candidate:ci name=@t declaration=(unit concurrency:ci) sha=@t]
  ^-  workflow-run:ci
  %:  evaluate:ci-concurrency
    c  name  declaration
    ['push' '' ref.c sha '~zod' [%obj ~] %.n]
    %.n
  ==
=/  a  (candidate 0v1 'refs/heads/master' 0xa)
=/  b  (candidate 0v2 'refs/heads/master' 0xb)
=/  s  *state-8:ci
=/  s  s(candidates (malt ~[[id.a a] [id.b b]]))
=/  ma  (evaluate a 'suite' p.suite 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
=/  mb  (evaluate b 'suite' p.suite 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')
=/  fa  (evaluate a 'fixtures' p.fixtures 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
=/  master
  %:  enter:ci-concurrency
    (enter:ci-concurrency s [id.a 'suite.yml'] ma ~2026.10.19)
    [id.b 'suite.yml']  mb  ~2026.10.19
  ==
::  Completion events for each master run flow through real standings,
::  aggregate verdict and group release, independently.
=/  one=job:ci
  =/  j  *job:ci
  j(workflow 'suite.yml', id 'one', name 'suite')
=/  passed
  |=  [id=@uv owner=@uv]
  ^-  attempt:ci
  =/  x  *attempt:ci
  x(id id, candidate owner, kind %job, workflow `'suite.yml', job `'one', status %passed)
=/  ma-c=candidate:ci  a(plan `~[one], attempts ~[0v11])
=/  mb-c=candidate:ci  b(plan `~[one], attempts ~[0v12])
=/  completed
  %=  master
    candidates  (malt ~[[id.a ma-c] [id.b mb-c]])
    attempts  (malt ~[[0v11 (passed 0v11 id.a)] [0v12 (passed 0v12 id.b)]])
  ==
=/  va  (verdict:ci-concurrency completed ma-c)
=/  vb  (verdict:ci-concurrency completed mb-c)
=/  released
  %-  refresh:ci-concurrency
  completed(candidates (malt ~[[id.a ma-c(status status.va)] [id.b mb-c(status status.vb)]]))
=/  ba  (evaluate a(ref 'refs/heads/feature') 'suite' p.suite 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
=/  bb  (evaluate b(ref 'refs/heads/feature') 'suite' p.suite 'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb')
=/  branch
  %:  enter:ci-concurrency
    (enter:ci-concurrency s [id.a 'suite.yml'] ba ~2026.10.19)
    [id.b 'suite.yml']  bb  ~2026.10.19
  ==
=/  bad=concurrency:ci
  [`['group' ['${{ fromJSON(bad) }}' 1 1 ~[[%expr [%call 'fromjson' ~[[%str 'bad']]]]]]] ~ ~]
=/  error  (evaluate a 'bad' `bad 'a')
=/  none-json=json
  (pairs:enjs:format ~[['group' ~] ['cancelInProgress' ~] ['refusal' s+'']])
=/  none  (parse:ci-concurrency-wire `none-json)
=/  old  (parse:ci-concurrency-wire ~)
=/  cases=(list [@t ?])
  :~  ['plan carries every workflow concurrency field' &(=(1 (lent declarations)) =('fixture-chain.yml' workflow.i.declarations) ?=(^ declaration.i.declarations))]
      ['plan, trigger catalog and manual inventory agree' &(=(declaration.i.declarations catalog-concurrency) =(catalog-concurrency inventory-concurrency))]
      ['job-level concurrency refuses in real planner' &(?=(%| -.refused) =('job a in fixture-chain.yml: job-level concurrency is not supported' p.refused))]
      ['master evaluates distinct SHA groups with false cancellation' &(!cancel.ma !cancel.mb !=(group.ma group.mb) =(`'suite-refs/heads/master-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' group.ma))]
      ['master pushes both proceed independently' &(=(%running phase:(~(got by workflow-runs.master) [id.a 'suite.yml'])) =(%running phase:(~(got by workflow-runs.master) [id.b 'suite.yml'])))]
      ['both master runs finish passed and release their groups' &(=(%passed status.va) =(%passed status.vb) =(%complete phase:(~(got by workflow-runs.released) [id.a 'suite.yml'])) =(%complete phase:(~(got by workflow-runs.released) [id.b 'suite.yml'])))]
      ['fixture expression uses its own workflow name' =(`'fixtures-refs/heads/master-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' group.fa)]
      ['branch evaluates same group and true cancellation' &(cancel.ba cancel.bb =(group.ba group.bb) =(`'suite-refs/heads/feature-head' group.ba))]
      ['new branch push cancels old push' &(=(%cancelled phase:(~(got by workflow-runs.branch) [id.a 'suite.yml'])) =(%running phase:(~(got by workflow-runs.branch) [id.b 'suite.yml'])))]
      ['bad expression refuses, never no group' &(=(%refused phase.error) !=('' reason.error))]
      ['known none and unknown are distinct wire values' &(?=(%& -.none) =(p.none `[~ ~ ~]) =(old [%& ~]))]
  ==
=/  failed=(list @t)  (murn cases |=([name=@t ok=?] ?:(ok ~ `name)))
~&  [%ci-concurrency-wire-failed failed]
?=(~ failed)
