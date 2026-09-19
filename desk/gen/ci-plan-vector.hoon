::  ci-plan vectors: the plan parser and validator on the wire shape the
::  daemon posts, and the evaluator on the standings a candidate records.
::  every negative asserts the exact reason text.
::
/-  ci
/+  ci-plan
:-  %say
|=  *
:-  %noun
=/  oid=@t  '587258b028c9bcc01e497386f0f85c48d0b82762'
=/  tree=(list @t)  ~['chain.yml' 'pass.yml' 'README.md']
=/  cond-eq=@t
  '{"v":1,"kind":"output-eq","job":"a","output":"go","literal":"true"}'
=/  plan-json
  |=  jobs=@t
  ^-  json
  %-  need
  %-  de:json:html
  %-  rap  :-  3
  :~  '{"oid":"'  oid  '","workflows":["chain.yml","pass.yml"],"jobs":['  jobs  ']}'
  ==
=/  job-json
  |=  [id=@t workflow=@t stage=@t needs=@t cond=@t extra=@t]
  ^-  @t
  %-  rap  :-  3
  :~  '{"id":"'  id  '","workflow":"'  workflow  '","name":"wf","stage":'  stage
      ',"needs":'  needs  ',"cond":'  cond  ',"events":["push"]'  extra  '}'
  ==
::  parse then validate, giving the plan or the reason
::
=/  check
  |=  jon=json
  ^-  (each (list job:ci) @t)
  =/  parsed=(each wire-plan:ci-plan @t)  (parse:ci-plan jon)
  ?:  ?=(%| -.parsed)  parsed
  (validate:ci-plan oid tree p.parsed)
=/  reason
  |=  jon=json
  ^-  @t
  =/  checked=(each (list job:ci) @t)  (check jon)
  ?:(?=(%& -.checked) 'VALID' p.checked)
::  the chain the harness runs: a emits go, b needs a and reads it
::
=/  chain=json
  %-  plan-json
  %-  rap  :-  3
  :~  (job-json 'a' 'chain.yml' '0' '[]' 'null' '')  ','
      (job-json 'b' 'chain.yml' '1' '["a"]' cond-eq '')  ','
      (job-json 'pass' 'pass.yml' '0' '[]' 'null' '')
  ==
=/  chain-plan=(list job:ci)
  =/  checked  (check chain)
  ?>  ?=(%& -.checked)
  p.checked
=/  key-a=key:ci-plan  ['chain.yml' 'a']
=/  key-b=key:ci-plan  ['chain.yml' 'b']
=/  key-pass=key:ci-plan  ['pass.yml' 'pass']
=/  job-b=job:ci  (snag 1 chain-plan)
=/  decide-b
  |=  [standings=(map key:ci-plan standing:ci-plan) outputs=(map key:ci-plan (map @t @t))]
  ^-  decision:ci-plan
  (decide:ci-plan job-b standings outputs)
::  standings and outputs from literal lists, typed as the maps the
::  evaluator reads
::
=/  st
  |=  entries=(list [key:ci-plan standing:ci-plan])
  ^-  (map key:ci-plan standing:ci-plan)
  (malt entries)
=/  ou
  |=  entries=(list [key:ci-plan (map @t @t)])
  ^-  (map key:ci-plan (map @t @t))
  (malt entries)
::
=/  results=(list [name=@t ok=?])
  :~  :-  'chain: three jobs planned, keyed by [workflow id]'
      =(3 (lent chain-plan))
    ::
      :-  'chain: b needs a, condition output-eq a.go == true, real workflow name carried'
      ?&  =(~['a'] needs.job-b)
          =(`[%output-eq 'a' 'go' 'true'] cond.job-b)
          =('wf' name.job-b)
      ==
    ::
      :-  'duplicate job id'
      .=  'duplicate job id a in chain.yml'
      %-  reason
      %-  plan-json
      (rap 3 ~[(job-json 'a' 'chain.yml' '0' '[]' 'null' '') ',' (job-json 'a' 'chain.yml' '0' '[]' 'null' '')])
    ::
      :-  'needs on a missing job'
      .=  'job b in chain.yml needs zz, which is not a job in chain.yml'
      %-  reason
      %-  plan-json
      (rap 3 ~[(job-json 'a' 'chain.yml' '0' '[]' 'null' '') ',' (job-json 'b' 'chain.yml' '1' '["zz"]' 'null' '')])
    ::
      :-  'needs never crosses a workflow file'
      .=  'job b in chain.yml needs pass, which is not a job in chain.yml'
      %-  reason
      %-  plan-json
      (rap 3 ~[(job-json 'pass' 'pass.yml' '0' '[]' 'null' '') ',' (job-json 'b' 'chain.yml' '1' '["pass"]' 'null' '')])
    ::
      :-  'unsupported if expression'
      .=  'job b in chain.yml: unsupported if expression "github.event_name == \'push\'"'
      %-  reason
      %-  plan-json
      %-  rap  :-  3
      :~  (job-json 'a' 'chain.yml' '0' '[]' 'null' '')  ','
          (job-json 'b' 'chain.yml' '1' '["a"]' '{"v":1,"kind":"unsupported","raw":"github.event_name == \'push\'"}' '')
      ==
    ::
      :-  'matrix unsupported in P1'
      .=  'matrix unsupported in P1 (job a in chain.yml)'
      %-  reason
      %-  plan-json
      (job-json 'a' 'chain.yml' '0' '[]' 'null' ',"matrix":true')
    ::
      ::  P3 (CI-P3-SCHED-A, rider 2): runs-on as a string or a list is
      ::  the job's label set, timeout-minutes its deadline bound; absent
      ::  is the empty set and no timeout; an expression is refused
      ::
      :-  'runs-on: a string is a one-element set, a list its elements, absent is empty'
      =/  planned=(each (list job:ci) @t)
        %-  check
        %-  plan-json
        %-  rap  :-  3
        :~  (job-json 'a' 'chain.yml' '0' '[]' 'null' ',"runs-on":"ubuntu-latest"')  ','
            (job-json 'b' 'chain.yml' '1' '["a"]' cond-eq ',"runs-on":["self-hosted","big-mem","self-hosted"],"timeout-minutes":3')  ','
            (job-json 'pass' 'pass.yml' '0' '[]' 'null' '')
        ==
      ?.  ?=(%& -.planned)  %.n
      ?&  =((silt ~['ubuntu-latest']) runs-on:(snag 0 p.planned))
          =(~ timeout:(snag 0 p.planned))
          =((silt ~['self-hosted' 'big-mem']) runs-on:(snag 1 p.planned))
          =(`3 timeout:(snag 1 p.planned))
          =(~ runs-on:(snag 2 p.planned))
      ==
    ::
      :-  'runs-on expression refused at plan time'
      .=  'runs-on expression unsupported in P3 (job a in chain.yml)'
      %-  reason
      %-  plan-json
      (job-json 'a' 'chain.yml' '0' '[]' 'null' ',"runs-on":"${{ matrix.os }}"')
    ::
      :-  'runs-on expression inside a list refused too'
      .=  'runs-on expression unsupported in P3 (job a in chain.yml)'
      %-  reason
      %-  plan-json
      (job-json 'a' 'chain.yml' '0' '[]' 'null' ',"runs-on":["self-hosted","${{ inputs.label }}"]')
    ::
      :-  'timeout-minutes 0 or non-numeric is no timeout'
      =/  planned=(each (list job:ci) @t)
        %-  check
        %-  plan-json
        (job-json 'a' 'chain.yml' '0' '[]' 'null' ',"timeout-minutes":0')
      ?.  ?=(%& -.planned)  %.n
      =(~ timeout:(snag 0 p.planned))
    ::
      :-  'unknown condition version (v 2)'
      .=  'job b in chain.yml: unknown condition version'
      %-  reason
      %-  plan-json
      %-  rap  :-  3
      :~  (job-json 'a' 'chain.yml' '0' '[]' 'null' '')  ','
          (job-json 'b' 'chain.yml' '1' '["a"]' '{"v":2,"kind":"output-eq","job":"a","output":"go","literal":"true"}' '')
      ==
    ::
      :-  'unknown condition version (unknown kind)'
      .=  'job b in chain.yml: unknown condition version'
      %-  reason
      %-  plan-json
      %-  rap  :-  3
      :~  (job-json 'a' 'chain.yml' '0' '[]' 'null' '')  ','
          (job-json 'b' 'chain.yml' '1' '["a"]' '{"v":1,"kind":"always","job":"a"}' '')
      ==
    ::
      :-  'if references a job that is not in needs'
      .=  'job b in chain.yml: if references needs.a, which is not in needs'
      %-  reason
      %-  plan-json
      (rap 3 ~[(job-json 'a' 'chain.yml' '0' '[]' 'null' '') ',' (job-json 'b' 'chain.yml' '1' '[]' cond-eq '')])
    ::
      :-  'stage must be a natural number'
      .=  'job a in chain.yml: stage -1 is not a natural number'
      %-  reason
      %-  plan-json
      (job-json 'a' 'chain.yml' '-1' '[]' 'null' '')
    ::
      :-  'plan oid must be the candidate oid'
      .=  (rap 3 ~['plan oid 0000000000000000000000000000000000000000 is not the candidate oid ' oid])
      %-  reason
      %-  need
      %-  de:json:html
      '{"oid":"0000000000000000000000000000000000000000","workflows":["chain.yml","pass.yml"],"jobs":[]}'
    ::
      :-  'workflow files must match the candidate tree'
      .=  'workflow files under .github/workflows differ from the plan: tree has chain.yml, pass.yml; plan has chain.yml'
      %-  reason
      %-  need
      %-  de:json:html
      (rap 3 ~['{"oid":"' oid '","workflows":["chain.yml"],"jobs":[]}'])
    ::
      :-  'a workflow that does not trigger on push is not planned'
      .=  'no push-triggered jobs under .github/workflows'
      %-  reason
      %-  need
      %-  de:json:html
      %-  rap  :-  3
      :~  '{"oid":"'  oid  '","workflows":["chain.yml","pass.yml"],"jobs":['
          '{"id":"a","workflow":"chain.yml","stage":0,"needs":[],"cond":null,"events":["workflow_dispatch"]}]}'
      ==
    ::
      :-  'the daemon reporting an error is the reason'
      .=  'act -l: yaml: line 3: did not find expected key'
      %-  reason
      %-  need
      %-  de:json:html
      '{"error":"act -l: yaml: line 3: did not find expected key"}'
    ::
      :-  'evaluate: b waits while a runs'
      =(%wait -:(decide-b (st ~[[key-a %running]]) ~))
    ::
      :-  'evaluate: if true runs'
      =(%run -:(decide-b (st ~[[key-a %passed]]) (ou ~[[key-a (malt ~[['go' 'true']])]])))
    ::
      :-  'evaluate: if false skips with the reason'
      .=  [%skip 'if false: needs.a.outputs.go is "false", not "true"']
      (decide-b (st ~[[key-a %passed]]) (ou ~[[key-a (malt ~[['go' 'false']])]]))
    ::
      :-  'evaluate: unset output skips with the reason'
      .=  [%skip 'output not set: needs.a.outputs.go']
      (decide-b (st ~[[key-a %passed]]) ~)
    ::
      :-  'evaluate: a failed need skips'
      =([%skip 'needs a failed'] (decide-b (st ~[[key-a %failed]]) ~))
    ::
      :-  'evaluate: an unfinished need skips'
      =([%skip 'needs a did not complete'] (decide-b (st ~[[key-a %unknown]]) ~))
    ::
      :-  'evaluate: an unsupported condition is invalid, never false'
      .=  [%invalid 'unsupported if expression "always()"']
      (decide:ci-plan job-b(cond `[%unsupported 'always()']) (st ~[[key-a %passed]]) ~)
    ::
      :-  'verdict: fan-out pending while any job is open'
      =([%pending ~] (verdict:ci-plan chain-plan (st ~[[key-a %passed] [key-pass %running]])))
    ::
      :-  'verdict: passed when every job passed or skipped by if'
      =([%passed ~] (verdict:ci-plan chain-plan (st ~[[key-a %passed] [key-b %skipped] [key-pass %passed]])))
    ::
      :-  'verdict: failed on the first failed job'
      .=  [%failed `'job pass in pass.yml failed']
      (verdict:ci-plan chain-plan (st ~[[key-a %passed] [key-b %running] [key-pass %failed]]))
    ::
      :-  'verdict: unknown on an infrastructure error with no failure'
      .=  [%unknown `'job a in chain.yml had an infrastructure error']
      (verdict:ci-plan chain-plan (st ~[[key-a %unknown] [key-b %skipped] [key-pass %passed]]))
    ::
      :-  'verdict: a linear chain with no attempts is pending'
      =([%pending ~] (verdict:ci-plan chain-plan ~))
  ==
=/  failures=(list @t)
  (murn results |=([name=@t ok=?] ?:(ok ~ `name)))
~&  [%ci-plan-vector passed=(sub (lent results) (lent failures)) of=(lent results)]
?~  failures  %.y
~&  [%ci-plan-vector-failures failures]
%.n
