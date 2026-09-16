::  Plan validation and job-level evaluation: the pure rules %urgit-ci
::  applies to the plan a daemon posts and to the attempts recorded
::  against it.  nothing here scries or parses expression syntax: the
::  daemon compiles every job-level `if` to a versioned structure
::  (CI-EXPR-1) and the ship checks that structure and evaluates it.
::
::    every refusal is a reason cord, never a crash and never `false`: a
::    plan the ship cannot validate fails the candidate with the reason
::    (R1-A), and a condition the ship cannot evaluate skips the job with
::    the reason, recorded on a %skipped attempt (R2.2-A).
::
/-  ci
|%
::  jobs are keyed by [workflow id]: `needs` never crosses a workflow
::  file, and ERPit has a `plan` job in both of its workflows
::
+$  key  [workflow=@t id=@t]
::
::  one job's standing, read off the newest attempt recorded for its key;
::  a job with no attempt is pending.  $? bunts to its last entry, so a
::  defaulted standing is pending: never passed, never skipped.
::
+$  standing  ?(%passed %failed %skipped %unknown %running %pending)
::
::  the wire plan as the daemon posts it, decoded but not yet validated
::
+$  wire-job
  $:  id=@t
      workflow=@t
      name=@t
      stage=@ud
      needs=(list @t)
      cond=(unit cond:ci)
      matrix=?
      events=(list @t)
      environment=(unit @t)
  ==
+$  wire-plan  [oid=@t workflows=(list @t) jobs=(list wire-job)]
::
++  quote
  |=  text=@t
  ^-  @t
  (rap 3 ~['"' text '"'])
::
++  job-name
  |=  [id=@t workflow=@t]
  ^-  @t
  (rap 3 ~['job ' id ' in ' workflow])
::
++  join
  |=  [items=(list @t) separator=@t]
  ^-  @t
  ?~  items  ''
  =/  acc=@t  i.items
  =/  rest=(list @t)  t.items
  |-
  ?~  rest  acc
  $(rest t.rest, acc (rap 3 ~[acc separator i.rest]))
::
++  sort-texts
  |=  items=(list @t)
  ^-  (list @t)
  (sort items |=([a=@t b=@t] (aor a b)))
::
::  the plan body.  `error` is the daemon reporting that it could not
::  produce a plan at all (act refused a workflow, the clone failed); its
::  text is the reason.  otherwise oid, workflows and jobs are required.
::
++  parse
  |=  jon=json
  ^-  (each wire-plan @t)
  ?.  ?=([%o *] jon)  [%| 'plan body is not a JSON object']
  =/  fields=(map @t json)  p.jon
  =/  error=(unit @t)  (string-field fields 'error')
  ?^  error  [%| u.error]
  =/  oid=(unit @t)  (string-field fields 'oid')
  ?~  oid  [%| 'plan is missing oid']
  =/  workflows=(unit (list @t))  (string-list-field fields 'workflows')
  ?~  workflows  [%| 'plan is missing workflows']
  =/  raw-jobs=(unit json)  (~(get by fields) 'jobs')
  ?~  raw-jobs  [%| 'plan is missing jobs']
  ?.  ?=([%a *] u.raw-jobs)  [%| 'plan jobs is not a list']
  =/  jobs=(each (list wire-job) @t)
    =/  remaining=(list json)  p.u.raw-jobs
    =|  out=(list wire-job)
    |-
    ?~  remaining  [%& (flop out)]
    =/  parsed=(each wire-job @t)  (parse-job i.remaining)
    ?:  ?=(%| -.parsed)  parsed
    $(remaining t.remaining, out [p.parsed out])
  ?:  ?=(%| -.jobs)  jobs
  [%& u.oid u.workflows p.jobs]
::
++  parse-job
  |=  jon=json
  ^-  (each wire-job @t)
  ?.  ?=([%o *] jon)  [%| 'plan job is not a JSON object']
  =/  fields=(map @t json)  p.jon
  =/  id=(unit @t)  (string-field fields 'id')
  =/  workflow=(unit @t)  (string-field fields 'workflow')
  ?~  id  [%| 'plan job is missing id']
  ?~  workflow  [%| (rap 3 ~['job ' u.id ' is missing workflow'])]
  =/  name=@t  (job-name u.id u.workflow)
  =/  stage=(unit @ud)
    =/  value=(unit json)  (~(get by fields) 'stage')
    ?~  value  ~
    ?.  ?=([%n *] u.value)  ~
    (slaw %ud p.u.value)
  ?~  stage
    =/  shown=@t
      =/  value=(unit json)  (~(get by fields) 'stage')
      ?~  value  'missing'
      ?.  ?=([%n *] u.value)  'not a number'
      p.u.value
    [%| (rap 3 ~[name ': stage ' shown ' is not a natural number'])]
  =/  needs=(unit (list @t))  (string-list-field fields 'needs')
  ?~  needs  [%| (rap 3 ~[name ': needs is not a list of job ids'])]
  =/  matrix=?
    =/  value=(unit json)  (~(get by fields) 'matrix')
    ?~  value  %.n
    ?.  ?=([%b *] u.value)  %.n
    p.u.value
  =/  events=(unit (list @t))  (string-list-field fields 'events')
  ?~  events  [%| (rap 3 ~[name ': events is not a list'])]
  =/  cond=(each (unit cond:ci) @t)  (parse-cond fields name)
  ?:  ?=(%| -.cond)  cond
  =/  workflow-name=@t  (fall (string-field fields 'name') '')
  ::  the job's `environment:` name, for %env-scoped credentials (D4);
  ::  absent or empty means the job names none
  ::
  =/  environment=(unit @t)
    =/  value=(unit @t)  (string-field fields 'environment')
    ?~  value  ~
    ?:  =('' u.value)  ~
    value
  [%& u.id u.workflow workflow-name u.stage u.needs p.cond matrix u.events environment]
::
::  the compiled condition: absent or null means run; otherwise exactly
::  `{"v":1,"kind":"output-eq",job,output,literal}` or
::  `{"v":1,"kind":"unsupported",raw}`.  any other version or kind is a
::  plan the ship does not understand, not a false condition.
::
++  parse-cond
  |=  [fields=(map @t json) name=@t]
  ^-  (each (unit cond:ci) @t)
  =/  value=(unit json)  (~(get by fields) 'cond')
  ?~  value  [%& ~]
  ?:  ?=(~ u.value)  [%& ~]
  ?.  ?=([%o *] u.value)
    [%| (rap 3 ~[name ': condition is not an object'])]
  =/  inner=(map @t json)  p.u.value
  =/  version=(unit json)  (~(get by inner) 'v')
  ?.  ?&(?=(^ version) ?=([%n *] u.version) =('1' p.u.version))
    [%| (rap 3 ~[name ': unknown condition version'])]
  =/  kind=(unit @t)  (string-field inner 'kind')
  ?~  kind  [%| (rap 3 ~[name ': unknown condition version'])]
  ?+    u.kind  [%| (rap 3 ~[name ': unknown condition version'])]
      %output-eq
    =/  job=(unit @t)  (string-field inner 'job')
    =/  output=(unit @t)  (string-field inner 'output')
    =/  literal=(unit @t)  (string-field inner 'literal')
    ?:  |(?=(~ job) ?=(~ output) ?=(~ literal))
      [%| (rap 3 ~[name ': output-eq condition is missing job, output or literal'])]
    [%& `[%output-eq u.job u.output u.literal]]
  ::
      %unsupported
    =/  raw=(unit @t)  (string-field inner 'raw')
    [%& `[%unsupported (fall raw '')]]
  ==
::
++  string-field
  |=  [fields=(map @t json) name=@t]
  ^-  (unit @t)
  =/  value=(unit json)  (~(get by fields) name)
  ?~  value  ~
  ?.  ?=([%s *] u.value)  ~
  `p.u.value
::
++  string-list-field
  |=  [fields=(map @t json) name=@t]
  ^-  (unit (list @t))
  =/  value=(unit json)  (~(get by fields) name)
  ?~  value  `~
  ?.  ?=([%a *] u.value)  ~
  =/  remaining=(list json)  p.u.value
  =|  out=(list @t)
  |-
  ?~  remaining  `(flop out)
  ?.  ?=([%s *] i.remaining)  ~
  $(remaining t.remaining, out [p.i.remaining out])
::
::  validation against the candidate: the plan was read at the candidate
::  oid; the workflow files it names are exactly the ones under
::  .github/workflows in the candidate's tree; every job id is unique per
::  workflow and every stage a natural number (parse); `needs` names jobs
::  of the same workflow; every condition is one the ship evaluates; no
::  job carries a matrix.  jobs whose workflow does not trigger on push
::  are not planned: a push is what staged the candidate.
::
++  validate
  |=  [candidate=@t tree=(list @t) plan=wire-plan]
  ^-  (each (list job:ci) @t)
  ?.  =(candidate oid.plan)
    [%| (rap 3 ~['plan oid ' oid.plan ' is not the candidate oid ' candidate])]
  =/  in-tree=(list @t)
    %-  sort-texts
    %+  skim  tree
    |=  name=@t
    =/  width=@ud  (met 3 name)
    ?|  &((gth width 4) =('.yml' (rsh [3 (sub width 4)] name)))
        &((gth width 5) =('.yaml' (rsh [3 (sub width 5)] name)))
    ==
  =/  named=(list @t)  (sort-texts workflows.plan)
  ?.  =(in-tree named)
    :-  %|
    %-  rap  :-  3
    :~  'workflow files under .github/workflows differ from the plan: tree has '
        (join in-tree ', ')  '; plan has '  (join named ', ')
    ==
  =/  ids=(set key)
    %-  silt
    %+  turn  jobs.plan
    |=(=wire-job [workflow.wire-job id.wire-job])
  ::  each check walks the jobs once and stops at the first refusal
  ::
  =/  remaining=(list wire-job)  jobs.plan
  =|  seen=(set key)
  |-
  ?^  remaining
    =/  =wire-job  i.remaining
    =/  name=@t  (job-name id.wire-job workflow.wire-job)
    =/  k=key  [workflow.wire-job id.wire-job]
    ?:  (~(has in seen) k)
      [%| (rap 3 ~['duplicate job id ' id.wire-job ' in ' workflow.wire-job])]
    ?.  (lien workflows.plan |=(name=@t =(name workflow.wire-job)))
      [%| (rap 3 ~[name ': workflow ' workflow.wire-job ' is not in the plan'])]
    ?:  matrix.wire-job
      [%| (rap 3 ~['matrix unsupported in P1 (' name ')'])]
    =/  missing=(unit @t)
      %+  find-first  needs.wire-job
      |=(need=@t !(~(has in ids) [workflow.wire-job need]))
    ?^  missing
      [%| (rap 3 ~[name ' needs ' u.missing ', which is not a job in ' workflow.wire-job])]
    ?:  ?=([~ %unsupported *] cond.wire-job)
      [%| (rap 3 ~[name ': unsupported if expression ' (quote raw.u.cond.wire-job)])]
    ?:  ?&  ?=([~ %output-eq *] cond.wire-job)
            !(lien needs.wire-job |=(need=@t =(need job.u.cond.wire-job)))
        ==
      [%| (rap 3 ~[name ': if references needs.' job.u.cond.wire-job ', which is not in needs'])]
    $(remaining t.remaining, seen (~(put in seen) k))
  =/  planned=(list job:ci)
    %+  murn  jobs.plan
    |=  =wire-job
    ^-  (unit job:ci)
    ?.  (lien events.wire-job |=(event=@t =('push' event)))  ~
    `[id.wire-job workflow.wire-job name.wire-job stage.wire-job needs.wire-job cond.wire-job environment.wire-job]
  ?~  planned
    [%| 'no push-triggered jobs under .github/workflows']
  [%& planned]
::
++  find-first
  |=  [items=(list @t) test=$-(@t ?)]
  ^-  (unit @t)
  ?~  items  ~
  ?:  (test i.items)  `i.items
  $(items t.items)
::
::  what to do with a job whose own attempt has not been recorded: run it,
::  skip it with a reason, or wait.  every job in `needs` must stand
::  terminal first.  a failed or unfinished need skips the dependent with
::  the reason (the candidate is already failed or unknown by then); a
::  skipped need skips it too, as on GitHub.  an output-eq condition reads
::  the need's recorded outputs: an output never set is a skip with the
::  reason, never a crash and never true.
::
+$  decision
  $%  [%run ~]
      [%skip reason=@t]
      [%wait ~]
      [%invalid reason=@t]
  ==
::
++  decide
  |=  [=job:ci standings=(map key standing) outputs=(map key (map @t @t))]
  ^-  decision
  =/  needs=(list @t)  needs.job
  |-
  ?^  needs
    =/  standing=standing  (~(gut by standings) [workflow.job i.needs] %pending)
    ?-  standing
      %pending  [%wait ~]
      %running  [%wait ~]
      %failed   [%skip (rap 3 ~['needs ' i.needs ' failed'])]
      %unknown  [%skip (rap 3 ~['needs ' i.needs ' did not complete'])]
      %skipped  [%skip (rap 3 ~['needs ' i.needs ' was skipped'])]
      %passed   $(needs t.needs)
    ==
  ?~  cond.job  [%run ~]
  ?-    -.u.cond.job
      %unsupported
    [%invalid (rap 3 ~['unsupported if expression ' (quote raw.u.cond.job)])]
  ::
      %output-eq
    =/  reference=@t
      (rap 3 ~['needs.' job.u.cond.job '.outputs.' output.u.cond.job])
    =/  recorded=(map @t @t)  (~(gut by outputs) [workflow.job job.u.cond.job] ~)
    =/  value=(unit @t)  (~(get by recorded) output.u.cond.job)
    ?~  value  [%skip (rap 3 ~['output not set: ' reference])]
    ?:  =(u.value literal.u.cond.job)  [%run ~]
    :-  %skip
    (rap 3 ~['if false: ' reference ' is ' (quote u.value) ', not ' (quote literal.u.cond.job)])
  ==
::
::  the candidate's verdict over its plan: failed on the first failed job,
::  else unknown on the first infrastructure error, else passed once every
::  job is passed or skipped, else pending.  a skip does not fail the
::  candidate: an `if` that is false is GitHub's skip, and ERPit's plan
::  gates depend on it.
::
++  verdict
  |=  [plan=(list job:ci) standings=(map key standing)]
  ^-  [status=candidate-status:ci reason=(unit @t)]
  =/  failed=(unit job:ci)
    (find-job plan standings %failed)
  ?^  failed
    [%failed `(rap 3 ~[(job-name id.u.failed workflow.u.failed) ' failed'])]
  =/  unknown=(unit job:ci)
    (find-job plan standings %unknown)
  ?^  unknown
    [%unknown `(rap 3 ~[(job-name id.u.unknown workflow.u.unknown) ' had an infrastructure error'])]
  =/  open=?
    %+  lien  plan
    |=  =job:ci
    =/  standing=standing  (~(gut by standings) [workflow.job id.job] %pending)
    ?=(?(%pending %running) standing)
  ?:  open  [%pending ~]
  [%passed ~]
::
++  find-job
  |=  [plan=(list job:ci) standings=(map key standing) want=standing]
  ^-  (unit job:ci)
  ?~  plan  ~
  ?:  =(want (~(gut by standings) [workflow.i.plan id.i.plan] %pending))
    `i.plan
  $(plan t.plan)
--
