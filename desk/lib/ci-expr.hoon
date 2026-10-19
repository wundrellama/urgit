::  Job expressions on the ship (CI-EXPR-2; R2.1-A/R2.2-A): the compiled
::  tree a plan carries, checked node by node, and its evaluation.
::
::    the pinned compiler (runner/internal/plan expr.go) parses a job's
::    `if` and its declared `outputs`; the ship never parses expression
::    text.  it re-checks what the compiler sent — the version, every node,
::    the bounds, and the contexts and functions the field may use, with
::    the compiler's own words — and evaluates the tree with GitHub's
::    documented semantics: loose equality, coercion to number and to
::    text, case-insensitive strings, short-circuit && and ||, the object
::    filter, and the functions a job-level field may call.  evaluation is
::    bounded in steps and in the size of every value; an error is a
::    reason, never false.
::
/-  ci
|%
::  the bounds the compiler enforces too
::
++  max-text  4.096
++  max-nodes  256
++  max-depth  32
++  max-outputs  64
++  max-fuel  10.000
++  max-value  1.048.576
::
::  a value
::
+$  val
  $~  [%null ~]
  $%  [%null ~]
      [%bool p=?]
      [%num p=@rd]
      [%str p=@t]
      [%arr p=(list val)]
      [%obj p=(map @t val)]
  ==
::
::  the field an expression is written in: a job-level `if`, or a job's
::  declared output
::
+$  field  ?(%if %output)
::
::  what an evaluation reads: the contexts, and the answers of the status
::  functions for the job being decided
::
+$  scope
  $:  github=val
      needs=val
      vars=val
      inputs=val
      steps=val
      success=?
      failure=?
      cancelled=?
  ==
::
++  empty-scope
  ^-  scope
  [[%obj ~] [%obj ~] [%obj ~] [%obj ~] [%obj ~] %.y %.n %.n]
::
::  one step of a context access: a property name (or a string index),
::  a dynamic index (~), or the * filter
::
+$  accessor  [name=(unit @t) star=?]
::
++  github-keys
  ^-  (set @t)
  %-  silt
  :~  'event_name'  'event'  'ref'  'ref_name'  'ref_type'  'sha'
      'actor'  'triggering_actor'  'workflow'  'job'  'base_ref'  'head_ref'
  ==
::
++  known-contexts
  ^-  (set @t)
  %-  silt
  :~  'github'  'env'  'vars'  'job'  'jobs'  'steps'  'runner'
      'secrets'  'strategy'  'matrix'  'needs'  'inputs'
  ==
::
::  the functions, by the lower-case name the tree carries, with the name
::  a message shows
::
++  functions
  ^-  (map @t @t)
  %-  malt
  ^-  (list [@t @t])
  :~  ['contains' 'contains']  ['startswith' 'startsWith']  ['endswith' 'endsWith']
      ['format' 'format']  ['join' 'join']  ['tojson' 'toJSON']  ['fromjson' 'fromJSON']
      ['hashfiles' 'hashFiles']  ['case' 'case']  ['success' 'success']
      ['failure' 'failure']  ['always' 'always']  ['cancelled' 'cancelled']
  ==
::
++  status-function
  |=  fn=@t
  ^-  ?
  ?=(?(%success %failure %always %cancelled) fn)
::
::  --  the wire
::
++  text-field
  |=  [f=(map @t json) key=@t]
  ^-  (unit @t)
  =/  v=(unit json)  (~(get by f) key)
  ?~  v  ~
  ?.  ?=([%s *] u.v)  ~
  `p.u.v
::
++  count-field
  |=  [f=(map @t json) key=@t]
  ^-  (unit @ud)
  =/  v=(unit json)  (~(get by f) key)
  ?~  v  ~
  ?.  ?=([%n *] u.v)  ~
  (rush p.u.v dim:ag)
::
::  a text shown in a reason: at most 200 bytes
::
++  shown
  |=  text=@t
  ^-  @t
  ?:  (lte (met 3 text) 200)  text
  (cat 3 (end [3 200] text) '…')
::
::  a compiled job-level `if`: {v:2, text, line, col, expr} or, refused by
::  the compiler, {v:2, text, line, col, refusal}.  either refusal is the
::  reason the plan is refused with
::
++  parse-condition
  |=  jon=json
  ^-  (each condition:ci @t)
  ?.  ?=([%o *] jon)  |+'the condition is not an object'
  =/  f=(map @t json)  p.jon
  ?.  =(`2 (count-field f 'v'))  |+'the condition is not version 2'
  =/  text=(unit @t)  (text-field f 'text')
  =/  line=(unit @ud)  (count-field f 'line')
  =/  col=(unit @ud)  (count-field f 'col')
  ?.  ?&(?=(^ text) ?=(^ line) ?=(^ col))
    |+'the condition has no text, line or col'
  ?:  (gth (met 3 u.text) max-text)  |+'the condition text exceeds 4096 bytes'
  =/  refusal=(unit @t)  (text-field f 'refusal')
  ?^  refusal  |+u.refusal
  =/  tree=(unit json)  (~(get by f) 'expr')
  ?~  tree  |+'the condition has neither a tree nor a refusal'
  =/  built=(each expr:ci @t)  (parse-tree u.tree %if)
  ?:  ?=(%| -.built)  built
  &+[%if u.text u.line u.col p.built]
::
::  a compiled declared output: {name, v:2, text, line, col, parts} or
::  {name, ..., refusal}; the node bound counts every part
::
++  parse-output
  |=  jon=json
  ^-  (each declared-output:ci @t)
  ?.  ?=([%o *] jon)  |+'an output is not an object'
  =/  f=(map @t json)  p.jon
  =/  name=@t  (fall (text-field f 'name') '')
  ?.  =(`2 (count-field f 'v'))  |+'the output is not version 2'
  =/  text=(unit @t)  (text-field f 'text')
  =/  line=(unit @ud)  (count-field f 'line')
  =/  col=(unit @ud)  (count-field f 'col')
  ?.  ?&(?=(^ text) ?=(^ line) ?=(^ col))
    |+'the output has no text, line or col'
  =/  refusal=(unit @t)  (text-field f 'refusal')
  ?^  refusal  |+u.refusal
  ?.  (output-name-valid name)
    |+(rap 3 ~['output name ' name ' starts with a letter or _ and holds only letters, digits, - and _'])
  ?:  (gth (met 3 u.text) max-text)  |+'the output text exceeds 4096 bytes'
  =/  raw=(unit json)  (~(get by f) 'parts')
  ?.  ?=([~ %a *] raw)  |+'the output has no parts'
  =/  items=(list json)  p.u.raw
  =|  out=(list part:ci)
  =|  nodes=@ud
  |-
  ?~  items  &+[name [u.text u.line u.col (flop out)]]
  ?.  ?=([%o *] i.items)  |+'an output part is not an object'
  =/  lit=(unit @t)  (text-field p.i.items 'lit')
  ?^  lit  $(items t.items, out [[%lit u.lit] out])
  =/  tree=(unit json)  (~(get by p.i.items) 'expr')
  ?~  tree  |+'an output part is neither text nor an expression'
  =/  built=(each [e=expr:ci n=@ud] @t)  (build u.tree 1 nodes)
  ?:  ?=(%| -.built)  built
  =/  problem=(unit @t)  (check e.p.built %output)
  ?^  problem  |+u.problem
  $(items t.items, nodes n.p.built, out [[%expr e.p.built] out])
::
++  output-name-valid
  |=  name=@t
  ^-  ?
  ?:  (gth (met 3 name) 100)  %.n
  ?=(^ (rush name ;~(plug ;~(pose alf cab) (star ;~(pose aln cab hep)))))
::
::  a tree from the wire, bounded and checked for its field
::
++  parse-tree
  |=  [jon=json =field]
  ^-  (each expr:ci @t)
  =/  built=(each [e=expr:ci n=@ud] @t)  (build jon 1 0)
  ?:  ?=(%| -.built)  built
  =/  problem=(unit @t)  (check e.p.built field)
  ?^  problem  |+u.problem
  &+e.p.built
::
::  one node and its children: the depth it sits at and the nodes counted
::  before it; refused past either bound
::
++  build
  |=  [jon=json depth=@ud count=@ud]
  ^-  (each [e=expr:ci n=@ud] @t)
  ?:  (gth depth max-depth)
    |+(rap 3 ~['the expression nests deeper than ' (crip (a-co:co max-depth))])
  =.  count  +(count)
  ?:  (gth count max-nodes)
    |+(rap 3 ~['the expression has more than ' (crip (a-co:co max-nodes)) ' nodes'])
  ?.  ?=([%o *] jon)  |+'an expression node is not an object'
  =/  f=(map @t json)  p.jon
  =/  kind=(unit @t)  (text-field f 'k')
  ?~  kind  |+'an expression node has no kind'
  =/  child
    |=  key=@t
    ^-  (each [e=expr:ci n=@ud] @t)
    =/  v=(unit json)  (~(get by f) key)
    ?~  v  |+(rap 3 ~['a ' u.kind ' node has no ' key])
    (build u.v +(depth) count)
  =/  name
    |=  key=@t
    ^-  (each @t @t)
    =/  v=(unit @t)  (text-field f key)
    ?~  v  |+(rap 3 ~['a ' u.kind ' node has no ' key])
    ?:  (gth (met 3 u.v) max-text)  |+'an expression text exceeds 4096 bytes'
    &+u.v
  ?+  u.kind  |+(rap 3 ~['unknown expression node ' u.kind])
      %null  &+[[%null ~] count]
  ::
      %bool
    =/  v=(unit json)  (~(get by f) 'b')
    ?.  ?=([~ %b *] v)  |+'a bool node has no b'
    &+[[%bool p.u.v] count]
  ::
      %num
    =/  v=(unit json)  (~(get by f) 'n')
    ?.  ?=([~ %n *] v)  |+'a num node has no n'
    =/  n=@rd  (to-number (crip (trip p.u.v)))
    ?:  (nan n)  |+'a num node is not a number'
    &+[[%num n] count]
  ::
      %str
    =/  s=(each @t @t)  (name 's')
    ?:  ?=(%| -.s)  s
    &+[[%str p.s] count]
  ::
      %ctx
    =/  s=(each @t @t)  (name 'name')
    ?:  ?=(%| -.s)  s
    ?.  =(p.s (crip (cass (trip p.s))))  |+'a context name is not lower case'
    &+[[%ctx p.s] count]
  ::
      %prop
    =/  s=(each @t @t)  (name 'name')
    ?:  ?=(%| -.s)  s
    =/  of  (child 'of')
    ?:  ?=(%| -.of)  of
    &+[[%prop e.p.of p.s] n.p.of]
  ::
      %index
    =/  of  (child 'of')
    ?:  ?=(%| -.of)  of
    =/  at  (build (~(gut by f) 'at' ~) +(depth) n.p.of)
    ?:  ?=(%| -.at)  at
    &+[[%index e.p.of e.p.at] n.p.at]
  ::
      %star
    =/  of  (child 'of')
    ?:  ?=(%| -.of)  of
    &+[[%star e.p.of] n.p.of]
  ::
      %not
    =/  a  (child 'a')
    ?:  ?=(%| -.a)  a
    &+[[%not e.p.a] n.p.a]
  ::
      %cmp
    =/  op=(unit @t)  (text-field f 'op')
    ?.  ?=([~ ?(%eq %ne %lt %le %gt %ge)] op)  |+'a cmp node has no known op'
    =/  l  (child 'l')
    ?:  ?=(%| -.l)  l
    =/  r  (build (~(gut by f) 'r' ~) +(depth) n.p.l)
    ?:  ?=(%| -.r)  r
    &+[[%cmp u.op e.p.l e.p.r] n.p.r]
  ::
      ?(%and %or)
    =/  l  (child 'l')
    ?:  ?=(%| -.l)  l
    =/  r  (build (~(gut by f) 'r' ~) +(depth) n.p.l)
    ?:  ?=(%| -.r)  r
    ?:  =(%and u.kind)  &+[[%and e.p.l e.p.r] n.p.r]
    &+[[%or e.p.l e.p.r] n.p.r]
  ::
      %call
    =/  fn=(each @t @t)  (name 'fn')
    ?:  ?=(%| -.fn)  fn
    =/  raw=(unit json)  (~(get by f) 'args')
    ?.  ?=([~ %a *] raw)  |+'a call node has no args'
    =/  items=(list json)  p.u.raw
    =|  args=(list expr:ci)
    =/  n=@ud  count
    |-
    ?~  items  &+[[%call p.fn (flop args)] n]
    =/  arg  (build i.items +(depth) n)
    ?:  ?=(%| -.arg)  arg
    $(items t.items, n n.p.arg, args [e.p.arg args])
  ==
::
::  the first construct the field may not use, in source order, in the
::  compiler's words (runner/internal/plan expr.go check)
::
++  check
  |=  [e=expr:ci =field]
  ^-  (unit @t)
  ?+  -.e  (check-children e field)
    %ctx  (check-context p.e ~ field)
  ::
      ?(%prop %index %star)
    =/  chain  (access-chain e)
    =/  problem=(unit @t)
      ?.  ?=(%ctx -.root.chain)  (check root.chain field)
      (check-context p.root.chain accessors.chain field)
    ?^  problem  problem
    (check-indexes e field)
  ::
      %call
    =/  problem=(unit @t)  (check-call e field)
    ?^  problem  problem
    (check-children e field)
  ==
::
++  check-children
  |=  [e=expr:ci =field]
  ^-  (unit @t)
  =/  kids=(list expr:ci)
    ?+  -.e  ~
      %not  ~[p.e]
      %cmp  ~[l.e r.e]
      ?(%and %or)  ~[l.e r.e]
      %call  args.e
    ==
  |-
  ?~  kids  ~
  =/  problem=(unit @t)  (check i.kids field)
  ?^  problem  problem
  $(kids t.kids)
::
::  the dynamic indexes of an access chain are expressions of their own
::
++  check-indexes
  |=  [e=expr:ci =field]
  ^-  (unit @t)
  ?+  -.e  ~
    %prop  $(e of.e)
    %star  $(e of.e)
      %index
    ?:  ?=(%str -.at.e)  $(e of.e)
    =/  problem=(unit @t)  (check at.e field)
    ?^  problem  problem
    $(e of.e)
  ==
::
++  access-chain
  |=  e=expr:ci
  ^-  [root=expr:ci accessors=(list accessor)]
  =|  acc=(list accessor)
  |-
  ?+  -.e  [e acc]
    %prop  $(e of.e, acc [[`name.e %.n] acc])
    %star  $(e of.e, acc [[~ %.y] acc])
      %index
    ?:  ?=(%str -.at.e)  $(e of.e, acc [[`p.at.e %.n] acc])
    $(e of.e, acc [[~ %.n] acc])
  ==
::
++  check-context
  |=  [name=@t chain=(list accessor) =field]
  ^-  (unit @t)
  =/  allowed=(set @t)
    %-  silt
    ?:  ?=(%if field)  ~['github' 'needs' 'vars' 'inputs']
    ~['github' 'needs' 'vars' 'inputs' 'steps']
  ?.  (~(has in allowed) name)
    ?.  (~(has in known-contexts) name)  `(rap 3 ~['unknown context ' name])
    ?:  ?=(%if field)
      `(rap 3 ~['context ' name ' is not available in a job-level if (github, needs, vars and inputs are)'])
    `(rap 3 ~['context ' name ' is not available to a job output on the ship in this release (github, needs, vars, inputs and steps.<id>.outputs are)'])
  ?+  name  ~
      %github
    ?~  chain  `'the whole github context is not available on the ship; name one of its properties'
    ?~  name.i.chain  `'github is read by property name only on the ship'
    =/  key=@t  (crip (cass (trip u.name.i.chain)))
    ?:  (~(has in github-keys) key)  ~
    `(rap 3 ~['github.' key ' is not available on the ship in this release'])
  ::
      %steps
    ?.  ?&(?=([* * *] chain) ?=(^ name.i.chain) ?=(^ name.i.t.chain))
      ?.  ?&(?=([* * ~] chain) ?=(^ name.i.chain) ?=(^ name.i.t.chain))
        `'steps is read as steps.<id>.outputs on the ship'
      (steps-property u.name.i.chain u.name.i.t.chain)
    (steps-property u.name.i.chain u.name.i.t.chain)
  ==
::
++  steps-property
  |=  [id=@t property=@t]
  ^-  (unit @t)
  =/  lower=@t  (crip (cass (trip property)))
  ?:  =('outputs' lower)  ~
  `(rap 3 ~['steps.' id '.' lower ' is not available on the ship; only steps.<id>.outputs'])
::
++  check-call
  |=  [e=expr:ci =field]
  ^-  (unit @t)
  ?>  ?=(%call -.e)
  =/  display=(unit @t)  (~(get by functions) fn.e)
  ?~  display  `(rap 3 ~['unknown function ' fn.e])
  ?:  ?&((status-function fn.e) !?=(%if field))
    `(rap 3 ~['function ' u.display ' is only valid in an if'])
  ?:  =('hashfiles' fn.e)
    ?:  ?=(%if field)  `'function hashFiles is not available in a job-level if'
    `'function hashFiles is not available to a job output on the ship'
  =/  got=@ud  (lent args.e)
  =/  shown-got=@t  (crip (a-co:co got))
  ?+  fn.e  ~
      ?(%contains %startswith %endswith)
    ?:  =(2 got)  ~
    `(rap 3 ~[u.display ' takes 2 arguments, not ' shown-got])
  ::
      ?(%tojson %fromjson)
    ?:  =(1 got)  ~
    `(rap 3 ~[u.display ' takes 1 argument, not ' shown-got])
  ::
      ?(%success %failure %always %cancelled)
    ?:  =(0 got)  ~
    `(rap 3 ~[u.display ' takes 0 arguments, not ' shown-got])
  ::
      %format
    ?:  (gte got 1)  ~
    `'format takes at least 1 argument, not 0'
  ::
      %join
    ?:  &((gte got 1) (lte got 2))  ~
    `(rap 3 ~['join takes 1 or 2 arguments, not ' shown-got])
  ::
      %case
    ?:  &((gte got 3) =(1 (mod got 2)))  ~
    `(rap 3 ~['case takes an odd number of arguments, at least 3, not ' shown-got])
  ==
::
::  whether a tree calls a status function anywhere: without one, a
::  job-level if is read as success() && (if)
::
++  has-status
  |=  e=expr:ci
  ^-  ?
  ?-  -.e
    ?(%null %bool %num %str %ctx)  %.n
    %prop  $(e of.e)
    %star  $(e of.e)
    %index  |($(e of.e) $(e at.e))
    %not  $(e p.e)
    %cmp  |($(e l.e) $(e r.e))
    ?(%and %or)  |($(e l.e) $(e r.e))
    %call  |((status-function fn.e) (lien args.e has-status))
  ==
::
::  what callers outside this library read of a condition and of declared
::  outputs, as values of no recursive type: the compiler loops fusing a
::  pattern over the recursive tree in another library's subject, so every
::  traversal of a tree stays here
::
+$  summary
  $%  [%if text=@t line=@ud col=@ud status=?]
      [%output-eq job=@t output=@t literal=@t]
      [%refused text=@t reason=@t]
  ==
::
++  summarize
  |=  c=condition:ci
  ^-  summary
  ?-  -.c
    %if  [%if text.c line.c col.c (has-status expr.c)]
    %output-eq  [%output-eq job.c output.c literal.c]
    %refused  [%refused text.c reason.c]
  ==
::
::  a condition's value: the %if tree's, else an error
::
++  eval-condition
  |=  [c=condition:ci s=scope]
  ^-  (each val @t)
  ?.  ?=(%if -.c)  |+'the condition is not an expression'
  (eval expr.c s)
::
::  each declared output's name, text and place
::
++  output-places
  |=  outputs=(list declared-output:ci)
  ^-  (list [name=@t text=@t line=@ud col=@ud])
  =|  out=(list [name=@t text=@t line=@ud col=@ud])
  |-
  ?~  outputs  (flop out)
  =/  t=template:ci  template.i.outputs
  $(outputs t.outputs, out [[name.i.outputs text.t line.t col.t] out])
::
::  each declared output's value in a scope, or why it has none
::
++  resolve
  |=  [outputs=(list declared-output:ci) s=scope]
  ^-  (list [name=@t value=(each @t @t)])
  =|  out=(list [name=@t value=(each @t @t)])
  |-
  ?~  outputs  (flop out)
  =/  value=(each @t @t)  (interpolate parts.template.i.outputs s)
  $(outputs t.outputs, out [[name.i.outputs value] out])
::
::  the job ids a condition and declared outputs name literally as
::  needs.<id>, with where: 'if' or 'output <name>'
::
++  named-needs
  |=  [cond=(unit condition:ci) outputs=(list declared-output:ci)]
  ^-  (list [where=@t id=@t])
  =/  from-if=(list [where=@t id=@t])
    ?~  cond  ~
    ?.  ?=(%if -.u.cond)  ~
    (turn (needs-named expr.u.cond) |=(id=@t ['if' id]))
  =|  out=(list [where=@t id=@t])
  |-
  ?~  outputs  (weld from-if (flop out))
  =/  where=@t  (cat 3 'output ' name.i.outputs)
  =/  parts=(list part:ci)  parts.template.i.outputs
  =|  found=(list [where=@t id=@t])
  |-
  ?~  parts  ^$(outputs t.outputs, out (weld (flop found) out))
  ?:  ?=(%lit -.i.parts)  $(parts t.parts)
  =/  ids=(list @t)  (needs-named p.i.parts)
  $(parts t.parts, found (weld (flop (turn ids |=(id=@t [where id]))) found))
::
::  the job ids a tree names literally as needs.<id> or needs['<id>']
::
++  needs-named
  |=  e=expr:ci
  ^-  (list @t)
  ?-  -.e
    ?(%null %bool %num %str %ctx)  ~
      ?(%prop %index %star)
    =/  chain  (access-chain e)
    =/  here=(list @t)
      ?.  ?=(%ctx -.root.chain)  ~
      ?.  =('needs' p.root.chain)  ~
      ?~  accessors.chain  ~
      ?~  name.i.accessors.chain  ~
      ~[u.name.i.accessors.chain]
    =/  rest=(list @t)  ?:(?=(%ctx -.root.chain) ~ (needs-named root.chain))
    (weld here (weld rest (index-needs e)))
  ::
    %not  (needs-named p.e)
    %cmp  (weld (needs-named l.e) (needs-named r.e))
    ?(%and %or)  (weld (needs-named l.e) (needs-named r.e))
    %call  (zing (turn args.e needs-named))
  ==
::
++  index-needs
  |=  e=expr:ci
  ^-  (list @t)
  ?+  -.e  ~
    %prop  $(e of.e)
    %star  $(e of.e)
    %index  (weld (needs-named at.e) $(e of.e))
  ==
::
::  --  numbers.  a number is a double; NaN and the infinities are values
::  a comparison or a coercion can produce
::
++  nan-rd  `@rd`0x7ff8.0000.0000.0000
++  inf-rd  `@rd`0x7ff0.0000.0000.0000
++  neg-inf-rd  `@rd`0xfff0.0000.0000.0000
::
++  nan
  |=  n=@rd
  ^-  ?
  (gth (dis n 0x7fff.ffff.ffff.ffff) 0x7ff0.0000.0000.0000)
::
++  zero
  |=  n=@rd
  ^-  ?
  =(0 (dis n 0x7fff.ffff.ffff.ffff))
::
++  negative
  |=  n=@rd
  ^-  ?
  =(1 (rsh [0 63] n))
::
::  GitHub's number from text: trimmed; empty is 0; a decimal with an
::  optional sign, fraction and exponent; 0x hex and 0o octal integers;
::  Infinity and -Infinity; anything else NaN
::
++  to-number
  |=  text=@t
  ^-  @rd
  =/  chars=tape  (trim-tape (trip text))
  ?~  chars  .~0
  ?:  =("Infinity" chars)  inf-rd
  ?:  =("-Infinity" chars)  neg-inf-rd
  ?:  ?&(?=([%'0' %x * *] chars) (levy `tape`t.t.chars hex-digit))
    (sun:rd (scan t.t.chars hex-number))
  ?:  ?&(?=([%'0' %o * *] chars) (levy `tape`t.t.chars |=(c=@tD &((gte c '0') (lte c '7')))))
    (sun:rd (scan t.t.chars oct-number))
  =/  parsed=(unit [s=? e=@s a=@u])  (decimal chars)
  ?~  parsed  nan-rd
  (grd:rd [%d u.parsed])
::
++  hex-digit
  |=  c=@tD
  ^-  ?
  ?|(&((gte c '0') (lte c '9')) &((gte c 'a') (lte c 'f')) &((gte c 'A') (lte c 'F')))
::
++  hex-number
  %+  cook  |=(digits=tape (digits-value digits 16))
  (plus ;~(pose (shim '0' '9') (shim 'a' 'f') (shim 'A' 'F')))
::
++  oct-number
  %+  cook  |=(digits=tape (digits-value digits 8))
  (plus (shim '0' '7'))
::
::  the value of digits in a base: 0-9, then a-f or A-F
::
++  digits-value
  |=  [digits=tape base=@ud]
  ^-  @
  =/  acc=@  0
  |-
  ?~  digits  acc
  =/  c=@tD  i.digits
  =/  d=@
    ?:  &((gte c '0') (lte c '9'))  (sub c '0')
    ?:  &((gte c 'a') (lte c 'f'))  (add 10 (sub c 'a'))
    (add 10 (sub c 'A'))
  $(digits t.digits, acc (add (mul base acc) d))
::
++  trim-tape
  |=  t=tape
  ^-  tape
  =/  space  |=(c=@tD |(=(' ' c) =('\09' c) =('\0a' c) =('\0d' c)))
  =/  front=tape  |-(?~(t ~ ?:((space i.t) $(t t.t) t)))
  =/  back=tape  (flop front)
  =.  back  |-(?~(back ~ ?:((space i.back) $(back t.back) back)))
  (flop back)
::
::  [sign exponent mantissa] of a decimal: [+-]digits[.digits][(e|E)[+-]digits]
::  or [+-].digits[...]; ~ when the text is not one
::
++  decimal
  |=  chars=tape
  ^-  (unit [s=? e=@s a=@u])
  =/  positive=?  %.y
  =?  positive  ?=([%'-' *] chars)  %.n
  =?  chars  ?=([?(%'-' %'+') *] chars)  t.chars
  =|  whole=tape
  |-  ^-  (unit [s=? e=@s a=@u])
  ?:  ?=([@ *] chars)
    ?:  &((gte i.chars '0') (lte i.chars '9'))
      $(chars t.chars, whole [i.chars whole])
    (decimal-rest positive (flop whole) chars)
  (decimal-rest positive (flop whole) chars)
::
++  decimal-rest
  |=  [positive=? whole=tape chars=tape]
  ^-  (unit [s=? e=@s a=@u])
  ::  the fraction's digits and what follows them, from one trap
  ::
  =/  split=[fraction=tape rest=tape]
    ?.  ?=([%'.' *] chars)  [~ chars]
    =/  rest=tape  t.chars
    =|  digits=tape
    |-
    ?:  ?&(?=([@ *] rest) (gte i.rest '0') (lte i.rest '9'))
      $(rest t.rest, digits [i.rest digits])
    [(flop digits) rest]
  =/  fraction=tape  fraction.split
  =.  chars  rest.split
  ?:  ?&(?=(~ whole) ?=(~ fraction))  ~
  =/  exponent=(unit @s)
    ?~  chars  `--0
    ?.  ?=(?(%e %'E') i.chars)  ~
    =/  rest=tape  t.chars
    =/  up=?  ?=([%'-' *] rest)
    =?  rest  ?=([?(%'-' %'+') *] rest)  t.rest
    ?~  rest  ~
    ?.  (levy `tape`rest |=(c=@tD &((gte c '0') (lte c '9'))))  ~
    =/  n=@ud  (scan rest dem:ag-plain)
    `(new:si !up n)
  ?~  exponent  ~
  =/  digits=tape  (weld whole fraction)
  =/  mantissa=@u  ?~(digits 0 (scan digits dem:ag-plain))
  `[positive (dif:si u.exponent (sun:si (lent fraction))) mantissa]
::
::  plain decimal digits, leading zeros allowed (dem:ag wants none past
::  the first and a thousands dot)
::
++  ag-plain
  |%
  ++  dem
    %+  cook  |=(digits=tape (digits-value digits 10))
    (plus (shim '0' '9'))
  --
::
::  a number as GitHub writes it (G15, as act's %.15G does): at most 15
::  significant digits, plainly when its leading digit's exponent is from
::  -4 to 14, else with an exponent like 1.5E+20 or 1E-05; 0 for either
::  zero; NaN, Infinity, -Infinity
::
++  number-text
  |=  n=@rd
  ^-  @t
  ?:  (nan n)  'NaN'
  ?:  =(inf-rd n)  'Infinity'
  ?:  =(neg-inf-rd n)  '-Infinity'
  ?:  (zero n)  '0'
  =/  sign=tape  ?:((negative n) "-" "")
  =/  d=dn  (drg:rd (dis n 0x7fff.ffff.ffff.ffff))
  ?.  ?=(%d -.d)  'NaN'
  =/  rounded=[digits=tape exp=@s]  (significant a.d e.d)
  =/  digits=tape  digits.rounded
  ?~  digits  '0'
  =/  count=@ud  (lent digits)
  ::  the exponent of the leading digit
  =/  lead=@s  (sum:si exp.rounded (sun:si (dec count)))
  =/  [up=? mag=@ud]  (old:si lead)
  ?:  ?|(&(up (gte mag 15)) &(!up (gte mag 5)))
    =/  body=tape  ?~(t.digits digits [i.digits '.' t.digits])
    =/  power=tape  (a-co:co mag)
    =?  power  =(1 (lent power))  ['0' power]
    (crip :(weld sign body "E" ?:(up "+" "-") power))
  =/  [eup=? emag=@ud]  (old:si exp.rounded)
  ?:  eup  (crip :(weld sign `tape`digits (reap emag '0')))
  ?:  up
    =/  point=@ud  +(mag)
    (crip :(weld sign (scag point `tape`digits) "." (slag point `tape`digits)))
  (crip :(weld sign "0." (reap (dec mag) '0') `tape`digits))
::
::  a decimal mantissa and exponent rounded to 15 significant digits,
::  half up, with no trailing zeros: the digits and the last one's
::  exponent
::
++  significant
  |=  [a=@u e=@s]
  ^-  [tape @s]
  =/  count=@ud  (lent (a-co:co a))
  =/  drop=@ud  ?:((gth count 15) (sub count 15) 0)
  =/  kept=@u  (div a (pow 10 drop))
  =/  next=@ud  ?:(=(0 drop) 0 (mod (div a (pow 10 (dec drop))) 10))
  =?  kept  (gte next 5)  +(kept)
  =/  carry=?  =(kept (pow 10 15))
  =?  kept  carry  (pow 10 14)
  =/  shift=@ud  ?:(carry +(drop) drop)
  =/  zeros=@ud  0
  |-
  ?:  &((gth kept 0) =(0 (mod kept 10)))
    $(kept (div kept 10), zeros +(zeros))
  [(a-co:co kept) (sum:si e (sun:si (add shift zeros)))]
::
::  --  coercion
::
++  truthy
  |=  v=val
  ^-  ?
  ?-  -.v
    %null  %.n
    %bool  p.v
    %num  !|((nan p.v) (zero p.v))
    %str  !=('' p.v)
    ?(%arr %obj)  %.y
  ==
::
++  to-num
  |=  v=val
  ^-  @rd
  ?-  -.v
    %null  .~0
    %bool  ?:(p.v .~1 .~0)
    %num  p.v
    %str  (to-number p.v)
    ?(%arr %obj)  nan-rd
  ==
::
++  to-text
  |=  v=val
  ^-  @t
  ?-  -.v
    %null  ''
    %bool  ?:(p.v 'true' 'false')
    %num  (number-text p.v)
    %str  p.v
    %arr  'Array'
    %obj  'Object'
  ==
::
++  lower
  |=  t=@t
  ^-  tape
  (cass (trip t))
::
::  byte order of two texts: %lt, %eq or %gt
::
++  order
  |=  [a=tape b=tape]
  ^-  ?(%lt %eq %gt)
  |-
  ?~  a  ?~(b %eq %lt)
  ?~  b  %gt
  ?:  (lth i.a i.b)  %lt
  ?:  (gth i.a i.b)  %gt
  $(a t.a, b t.b)
::
::  GitHub's loose comparison: values of different types are compared as
::  numbers; texts without case; NaN is unordered and unequal; an array
::  or object equals nothing the ship can tell is the same instance
::
++  compare
  |=  [op=?(%eq %ne %lt %le %gt %ge) l=val r=val]
  ^-  ?
  ?:  ?&(?=(%str -.l) ?=(%str -.r))
    =/  o  (order (lower p.l) (lower p.r))
    ?-  op
      %eq  =(%eq o)
      %ne  !=(%eq o)
      %lt  =(%lt o)
      %le  !=(%gt o)
      %gt  =(%gt o)
      %ge  !=(%lt o)
    ==
  ?:  ?|(?=(?(%arr %obj) -.l) ?=(?(%arr %obj) -.r))
    ?:  ?&(=(-.l -.r) ?=(%ne op))  %.y
    ?:  =(-.l -.r)  %.n
    (compare-numbers op (to-num l) (to-num r))
  (compare-numbers op (to-num l) (to-num r))
::
++  compare-numbers
  |=  [op=?(%eq %ne %lt %le %gt %ge) a=@rd b=@rd]
  ^-  ?
  ?:  |((nan a) (nan b))  ?=(%ne op)
  =/  same=?  |(&((zero a) (zero b)) =(a b))
  ?-  op
    %eq  same
    %ne  !same
    %lt  &(!same (lth:rd a b))
    %le  |(same (lth:rd a b))
    %gt  &(!same (gth:rd a b))
    %ge  |(same (gth:rd a b))
  ==
::
::  --  JSON
::
++  from-json
  |=  j=json
  ^-  val
  ?~  j  [%null ~]
  ?-  -.j
    %b  [%bool p.j]
    %n  [%num (to-number (crip (trip p.j)))]
    %s  [%str p.j]
    %a  [%arr (turn p.j from-json)]
    %o  [%obj (~(run by p.j) from-json)]
  ==
::
::  toJSON: two-space indentation, object keys in byte order
::
++  to-json
  |=  v=val
  ^-  @t
  (crip (json-tape v ""))
::
++  json-tape
  |=  [v=val indent=tape]
  ^-  tape
  ?-  -.v
    %null  "null"
    %bool  ?:(p.v "true" "false")
    %num  (trip (number-text p.v))
    %str  (quoted p.v)
      %arr
    ?~  p.v  "[]"
    =/  inner=tape  (weld indent "  ")
    =/  items=(list tape)  (turn p.v |=(x=val (weld inner (json-tape x inner))))
    :(weld "[\0a" (join-tapes items ",\0a") "\0a" indent "]")
  ::
      %obj
    ?:  =(~ p.v)  "\{}"
    =/  inner=tape  (weld indent "  ")
    =/  keys=(list @t)  (sort ~(tap in ~(key by p.v)) |=([a=@t b=@t] =(%lt (order (trip a) (trip b)))))
    =/  items=(list tape)
      %+  turn  keys
      |=  k=@t
      :(weld inner (quoted k) ": " (json-tape (~(got by p.v) k) inner))
    :(weld "\{\0a" (join-tapes items ",\0a") "\0a" indent "}")
  ==
::
++  join-tapes
  |=  [items=(list tape) sep=tape]
  ^-  tape
  ?~  items  ~
  =/  out=tape  i.items
  |-
  ?~  t.items  out
  $(items t.items, out :(weld out sep i.t.items))
::
++  quoted
  |=  t=@t
  ^-  tape
  =/  out=tape
    %-  zing
    %+  turn  (trip t)
    |=  c=@tD
    ^-  tape
    ?:  =('"' c)  "\\\""
    ?:  =('\\' c)  "\\\\"
    ?:  =('\0a' c)  "\\n"
    ?:  =('\0d' c)  "\\r"
    ?:  =('\09' c)  "\\t"
    ?:  =('\08' c)  "\\b"
    ?:  =('\0c' c)  "\\f"
    ?:  (lth c 32)
      =/  hex=tape  ((x-co:co 4) c)
      (weld "\\u" hex)
    ~[c]
  :(weld "\"" out "\"")
::
::  --  evaluation
::
::  an expression's value, or why it has none: evaluation is bounded at
::  max-fuel steps and every text at max-value bytes
::
++  eval
  |=  [e=expr:ci s=scope]
  ^-  (each val @t)
  =/  r  (run e s max-fuel)
  ?:  ?=(%| -.r)  r
  &+v.p.r
::
+$  ran  (each [v=val filtered=? fuel=@ud] @t)
::
++  run
  |=  [e=expr:ci s=scope fuel=@ud]
  ^-  ran
  ?:  =(0 fuel)
    |+(rap 3 ~['the evaluation exceeds its bound of ' (crip (a-co:co max-fuel)) ' steps'])
  =.  fuel  (dec fuel)
  ?-  -.e
    %null  &+[[%null ~] %.n fuel]
    %bool  &+[[%bool p.e] %.n fuel]
    %num  &+[[%num p.e] %.n fuel]
    %str  &+[[%str p.e] %.n fuel]
    %ctx  &+[(context-of p.e s) %.n fuel]
  ::
      %prop
    =/  r=ran  $(e of.e)
    ?:  ?=(%| -.r)  r
    &+[(property v.p.r filtered.p.r name.e) filtered.p.r fuel.p.r]
  ::
      %index
    =/  r=ran  $(e of.e)
    ?:  ?=(%| -.r)  r
    =/  i=ran  $(e at.e, fuel fuel.p.r)
    ?:  ?=(%| -.i)  i
    &+[(element v.p.r filtered.p.r v.p.i) filtered.p.r fuel.p.i]
  ::
      %star
    =/  r=ran  $(e of.e)
    ?:  ?=(%| -.r)  r
    &+[(filter v.p.r filtered.p.r) %.y fuel.p.r]
  ::
      %not
    =/  r=ran  $(e p.e)
    ?:  ?=(%| -.r)  r
    &+[[%bool !(truthy v.p.r)] %.n fuel.p.r]
  ::
      %cmp
    =/  l=ran  $(e l.e)
    ?:  ?=(%| -.l)  l
    =/  r=ran  $(e r.e, fuel fuel.p.l)
    ?:  ?=(%| -.r)  r
    &+[[%bool (compare op.e v.p.l v.p.r)] %.n fuel.p.r]
  ::
      %and
    =/  l=ran  $(e l.e)
    ?:  ?=(%| -.l)  l
    ?.  (truthy v.p.l)  &+[v.p.l %.n fuel.p.l]
    =/  r=ran  $(e r.e, fuel fuel.p.l)
    ?:  ?=(%| -.r)  r
    &+[v.p.r %.n fuel.p.r]
  ::
      %or
    =/  l=ran  $(e l.e)
    ?:  ?=(%| -.l)  l
    ?:  (truthy v.p.l)  &+[v.p.l %.n fuel.p.l]
    =/  r=ran  $(e r.e, fuel fuel.p.l)
    ?:  ?=(%| -.r)  r
    &+[v.p.r %.n fuel.p.r]
  ::
      %call
    ?:  =('case' fn.e)  (run-case args.e s fuel)
    ?:  =('always' fn.e)  &+[[%bool %.y] %.n fuel]
    ?:  =('success' fn.e)  &+[[%bool success.s] %.n fuel]
    ?:  =('failure' fn.e)  &+[[%bool failure.s] %.n fuel]
    ?:  =('cancelled' fn.e)  &+[[%bool cancelled.s] %.n fuel]
    =/  args=(list expr:ci)  args.e
    =|  vals=(list val)
    |-  ^-  ran
    ?^  args
      =/  a=ran  ^$(e i.args)
      ?:  ?=(%| -.a)  a
      $(args t.args, vals [v.p.a vals], fuel fuel.p.a)
    =/  out=(each val @t)  (apply fn.e (flop vals))
    ?:  ?=(%| -.out)  out
    ?:  ?&(?=(%str -.p.out) (gth (met 3 p.p.out) max-value))
      |+'a value exceeds 1 MiB'
    &+[p.out %.n fuel]
  ==
::
::  case(pred, value, ..., default): the value of the first true
::  predicate; only that value is evaluated
::
++  run-case
  |=  [args=(list expr:ci) s=scope fuel=@ud]
  ^-  ran
  ?~  args  &+[[%null ~] %.n fuel]
  ?~  t.args  (run i.args s fuel)
  =/  p=ran  (run i.args s fuel)
  ?:  ?=(%| -.p)  p
  ?:  (truthy v.p.p)  (run i.t.args s fuel.p.p)
  $(args t.t.args, fuel fuel.p.p)
::
++  context-of
  |=  [name=@t s=scope]
  ^-  val
  ?+  name  [%null ~]
    %github  github.s
    %needs  needs.s
    %vars  vars.s
    %inputs  inputs.s
    %steps  steps.s
  ==
::
::  a property, read without case; over a filter's result, of each item
::
++  property
  |=  [v=val filtered=? name=@t]
  ^-  val
  ?:  ?&(filtered ?=(%arr -.v))
    [%arr (turn p.v |=(x=val (property x %.n name)))]
  ?.  ?=(%obj -.v)  [%null ~]
  (lookup p.v name)
::
++  lookup
  |=  [m=(map @t val) name=@t]
  ^-  val
  =/  hit=(unit val)  (~(get by m) name)
  ?^  hit  u.hit
  =/  want=tape  (lower name)
  =/  pairs=(list [k=@t v=val])  ~(tap by m)
  |-
  ?~  pairs  [%null ~]
  ?:  =(want (lower k.i.pairs))  v.i.pairs
  $(pairs t.pairs)
::
::  an index: an array's integral element, an object's property, each
::  item's over a filter's result
::
++  element
  |=  [v=val filtered=? at=val]
  ^-  val
  ?:  ?&(filtered ?=(%arr -.v))
    [%arr (turn p.v |=(x=val (element x %.n at)))]
  ?:  ?=(%obj -.v)
    ?.  ?=(%str -.at)  [%null ~]
    (lookup p.v p.at)
  ?.  ?=(%arr -.v)  [%null ~]
  ?.  ?=(%num -.at)  [%null ~]
  ?:  (zero p.at)  (fall (head-of p.v) [%null ~])
  ?:  |((nan p.at) (negative p.at))  [%null ~]
  =/  d=dn  (drg:rd p.at)
  ?.  ?=(%d -.d)  [%null ~]
  =/  [up=? mag=@ud]  (old:si e.d)
  ?.  up  [%null ~]
  =/  index=@ud  (mul a.d (pow 10 mag))
  ?:  (gte index (lent p.v))  [%null ~]
  (snag index p.v)
::
++  head-of
  |=  items=(list val)
  ^-  (unit val)
  ?~(items ~ `i.items)
::
::  the object filter: an array's items, an object's values in key order;
::  over a filter's result, each item filtered
::
++  filter
  |=  [v=val filtered=?]
  ^-  val
  ?:  ?&(filtered ?=(%arr -.v))
    [%arr (zing (turn p.v |=(x=val =/(f (filter x %.n) ?:(?=(%arr -.f) p.f ~)))))]
  ?+  -.v  [%arr ~]
    %arr  v
      %obj
    =/  keys=(list @t)  (sort ~(tap in ~(key by p.v)) |=([a=@t b=@t] =(%lt (order (trip a) (trip b)))))
    [%arr (turn keys |=(k=@t (~(got by p.v) k)))]
  ==
::
::  a function of its evaluated arguments
::
++  apply
  |=  [fn=@t args=(list val)]
  ^-  (each val @t)
  =/  a=val  ?~(args [%null ~] i.args)
  =/  b=val  ?~(args [%null ~] ?~(t.args [%null ~] i.t.args))
  ?+  fn  |+(rap 3 ~['unknown function ' fn])
      %contains
    ?:  ?=(%arr -.a)  &+[%bool (lien p.a |=(x=val (compare %eq x b)))]
    ?:  ?=(%obj -.a)  &+[%bool %.n]
    &+[%bool ?=(^ (find (lower (to-text b)) (lower (to-text a))))]
  ::
      %startswith
    =/  whole=tape  (lower (to-text a))
    =/  part=tape  (lower (to-text b))
    &+[%bool =(part (scag (lent part) whole))]
  ::
      %endswith
    =/  whole=tape  (lower (to-text a))
    =/  part=tape  (lower (to-text b))
    &+[%bool ?&((lte (lent part) (lent whole)) =(part (slag (sub (lent whole) (lent part)) whole)))]
  ::
      %format
    =/  out=(each @t @t)  (format-text (to-text a) ?~(args ~ t.args))
    ?:  ?=(%| -.out)  out
    &+[%str p.out]
  ::
      %join
    =/  sep=@t  ?:((gte (lent args) 2) (to-text b) ',')
    ?.  ?=(%arr -.a)  &+[%str (to-text a)]
    &+[%str (rap 3 (join-texts (turn p.a to-text) sep))]
  ::
      %tojson  &+[%str (to-json a)]
  ::
      %fromjson
    ?.  ?=(%str -.a)  |+'fromJSON takes text'
    =/  parsed=(unit json)  (de:json:html p.a)
    ?~  parsed  |+(rap 3 ~['fromJSON: ' (shown (quote p.a)) ' is not JSON'])
    &+(from-json u.parsed)
  ==
::
++  join-texts
  |=  [items=(list @t) sep=@t]
  ^-  (list @t)
  ?~  items  ~
  [i.items (zing (turn t.items |=(t=@t ~[sep t])))]
::
++  quote
  |=  t=@t
  ^-  @t
  (rap 3 ~['\'' t '\''])
::
::  format('…{0}…', …): {N} is the Nth value as text, {{ and }} are
::  braces; a placeholder past the values, or an unclosed one, is an
::  error
::
++  format-text
  |=  [pattern=@t values=(list val)]
  ^-  (each @t @t)
  =/  chars=tape  (trip pattern)
  =|  out=tape
  |-
  ?~  chars  &+(crip (flop out))
  ?:  ?&(=('{' i.chars) ?=([%'{' *] t.chars))  $(chars t.t.chars, out ['{' out])
  ?:  ?&(=('}' i.chars) ?=([%'}' *] t.chars))  $(chars t.t.chars, out ['}' out])
  ?:  =('}' i.chars)
    |+(rap 3 ~['format: a } without its { in ' (quote (shown pattern))])
  ?.  =('{' i.chars)  $(chars t.chars, out [i.chars out])
  =/  close=(unit @ud)  (find "}" t.chars)
  ?~  close  |+(rap 3 ~['format: an unclosed { in ' (quote (shown pattern))])
  =/  digits=tape  (scag u.close t.chars)
  ?.  ?&(?=(^ digits) (levy `tape`digits |=(c=@tD &((gte c '0') (lte c '9')))))
    |+(rap 3 ~['format: {' (crip digits) '} is not a placeholder'])
  =/  index=@ud  (scan digits dem:ag-plain)
  ?:  (gte index (lent values))
    |+(rap 3 ~['format: {' (crip digits) '} has no value'])
  %=  $
    chars  (slag +(u.close) t.chars)
    out  (weld (flop (trip (to-text (snag index values)))) out)
  ==
::
::  a template's text: each expression's value as text
::
++  interpolate
  |=  [parts=(list part:ci) s=scope]
  ^-  (each @t @t)
  =|  out=(list @t)
  |-
  ?~  parts
    =/  text=@t  (rap 3 (flop out))
    ?:  (gth (met 3 text) max-value)  |+'the value exceeds 1 MiB'
    &+text
  ?:  ?=(%lit -.i.parts)  $(parts t.parts, out [p.i.parts out])
  =/  v=(each val @t)  (eval p.i.parts s)
  ?:  ?=(%| -.v)  v
  $(parts t.parts, out [(to-text p.v) out])
::
::  a value as a reason shows it
::
++  render
  |=  v=val
  ^-  @t
  ?-  -.v
    %null  'null'
    %bool  ?:(p.v 'true' 'false')
    %num  (number-text p.v)
    %str  ?:(=('' p.v) 'an empty string' (quote (shown p.v)))
    %arr  'an array'
    %obj  'an object'
  ==
::
::  --  the contexts a run's jobs read
::
::  github, as the ship holds a run: the event and its payload, the ref,
::  the source, the actor, the workflow's name and the job
::
++  github-of
  |=  [event=@t payload=@t ref=@t sha=@t actor=@t workflow=@t job=@t]
  ^-  val
  =/  parsed=(unit json)  ?:(=('' payload) ~ (de:json:html payload))
  =/  branch=?  =("refs/heads/" (scag 11 (trip ref)))
  =/  tag=?  =("refs/tags/" (scag 10 (trip ref)))
  :-  %obj
  %-  malt
  ^-  (list [@t val])
  :~  ['event_name' [%str event]]
      ['event' ?~(parsed [%obj ~] (from-json u.parsed))]
      ['ref' [%str ref]]
      ['ref_name' [%str ?:(branch (crip (slag 11 (trip ref))) ?:(tag (crip (slag 10 (trip ref))) ref))]]
      ['ref_type' [%str ?:(branch 'branch' ?:(tag 'tag' ''))]]
      ['sha' [%str sha]]
      ['actor' [%str actor]]
      ['triggering_actor' [%str actor]]
      ['workflow' [%str workflow]]
      ['job' [%str job]]
      ['base_ref' [%str '']]
      ['head_ref' [%str '']]
  ==
::
::  inputs: a manual run's admitted values, typed; an input without a
::  value is false when it is a boolean and empty text otherwise, as act
::  reads it.  a push has none
::
++  inputs-of
  |=  inputs=(list admitted-input:ci)
  ^-  val
  :-  %obj
  %-  malt
  %+  turn  inputs
  |=  i=admitted-input:ci
  ^-  [@t val]
  :-  id.i
  ?~  value.i
    ?:(?=(%boolean kind.i) [%bool %.n] [%str ''])
  ?-  -.u.value.i
    %boolean  [%bool p.u.value.i]
    %number  [%num (to-number p.u.value.i)]
    %string  [%str p.u.value.i]
  ==
::
::  needs: each need's result and its resolved declared outputs
::
++  needs-of
  |=  entries=(list need-entry:ci)
  ^-  val
  :-  %obj
  %-  malt
  %+  turn  entries
  |=  n=need-entry:ci
  ^-  [@t val]
  :-  id.n
  :-  %obj
  %-  malt
  ^-  (list [@t val])
  :~  ['result' [%str result.n]]
      ['outputs' [%obj (malt (turn outputs.n |=([k=@t v=@t] [k [%str v]])))]]
  ==
::
::  the needs context act reads (the projection's carrier) and the signed
::  manifest binds: {"<id>": {"outputs": {...}, "result": "..."}}
::
++  needs-text
  |=  entries=(list need-entry:ci)
  ^-  @t
  %-  en:json:html
  %-  pairs:enjs:format
  %+  turn  entries
  |=  n=need-entry:ci
  :-  id.n
  %-  pairs:enjs:format
  :~  ['outputs' (pairs:enjs:format (turn outputs.n |=([k=@t v=@t] [k s+v])))]
      ['result' s+result.n]
  ==
::
::  steps: each step of the job (one-element step ids) and its outputs;
::  a later value of the same step and name replaces an earlier one, as
::  act records it.  a composite action's own steps are not the job's
::
++  steps-of
  |=  outputs=(list step-output:ci)
  ^-  val
  =|  acc=(map @t (map @t val))
  =.  acc
    %+  roll  outputs
    |=  [o=step-output:ci m=_acc]
    ?.  ?=([@ ~] step.o)  m
    =/  step=(map @t val)  (~(gut by m) i.step.o ~)
    (~(put by m) i.step.o (~(put by step) name.o [%str value.o]))
  :-  %obj
  %-  ~(run by acc)
  |=  outs=(map @t val)
  [%obj (malt ~[['outputs' [%obj outs]]])]
--
