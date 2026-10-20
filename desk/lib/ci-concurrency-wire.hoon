::  Shared catalog, manual inventory and plan decoder. Unknown is never none.
/-  ci
/+  ci-expr
|%
++  parse
  |=  raw=(unit json)
  ^-  (each (unit concurrency:ci) @t)
  ?~  raw  [%& ~]
  ?.  ?=([%o *] u.raw)  [%| 'concurrency must be an object (missing means unknown)']
  =/  f=(map @t json)  p.u.raw
  =/  why=(unit @t)  (text-field:ci-expr f 'refusal')
  ?:  ?&(?=(^ why) !=('' u.why))  [%& `[~ ~ why]]
  =/  g=(unit json)  (~(get by f) 'group')
  =/  c=(unit json)  (~(get by f) 'cancelInProgress')
  ?.  ?&(?=(^ g) ?=(^ c))  [%| 'concurrency must carry group and cancelInProgress, including null']
  =/  group=(each (unit declared-output:ci) @t)
    ?~  u.g  [%& ~]
    =/  parsed  (parse-output:ci-expr u.g)
    ?:  ?=(%| -.parsed)  [%| (rap 3 ~['group: ' p.parsed])]
    =/  bad=(unit @t)
      =/  parts  parts.template.p.parsed
      |-
      ?~  parts  ~
      ?:  ?=(%lit -.i.parts)  $(parts t.parts)
      =/  one=(unit @t)  (check p.i.parts)
      ?^(one one $(parts t.parts))
    ?^  bad  [%| u.bad]
    [%& `p.parsed]
  ?:  ?=(%| -.group)  [%& `[~ ~ `p.group]]
  =/  cancel=(each (unit condition:ci) @t)
    ?~  u.c  [%& ~]
    =/  parsed  (parse-condition:ci-expr u.c)
    ?:  ?=(%| -.parsed)  [%| (rap 3 ~['cancel-in-progress: ' p.parsed])]
    ?>  ?=(%if -.p.parsed)
    =/  bad=(unit @t)  (check expr.p.parsed)
    ?^  bad  [%| u.bad]
    [%& `p.parsed]
  ?:  ?=(%| -.cancel)  [%& `[p.group ~ `p.cancel]]
  [%& `[p.group p.cancel ~]]
::
::  Re-check context availability on the ship; a compiler is not authority.
++  check
  |=  e=expr:ci
  ^-  (unit @t)
  ?:  ?=(%ctx -.e)
    ?:(?|(=('github' p.e) =('inputs' p.e) =('vars' p.e)) ~ `'workflow concurrency permits only github, inputs and vars')
  ?:  ?&(?=(%call -.e) (status-function:ci-expr fn.e))
    `'status functions are not available in workflow concurrency'
  =/  children=(list expr:ci)
    ?-  -.e
      ?(%null %bool %num %str %ctx)  ~
      %prop  ~[of.e]
      %star  ~[of.e]
      %index  ~[of.e at.e]
      %not  ~[p.e]
      %cmp  ~[l.e r.e]
      %and  ~[l.e r.e]
      %or  ~[l.e r.e]
      %call  args.e
    ==
  |-
  ?~  children  ~
  =/  bad=(unit @t)  (check i.children)
  ?^(bad bad $(children t.children))
::
++  parse-list
  |=  raw=(unit json)
  ^-  (each (unit (list [workflow=@t name=@t declaration=(unit concurrency:ci)])) @t)
  ?~  raw  [%& ~]
  ?.  ?=([%a *] u.raw)  [%| 'plan concurrency must be a list']
  =/  items  p.u.raw
  ?:  (gth (lent items) 256)  [%| 'plan concurrency exceeds 256 workflows']
  =|  out=(list [workflow=@t name=@t declaration=(unit concurrency:ci)])
  =|  seen=(set @t)
  |-
  ?~  items  [%& `(flop out)]
  ?.  ?=([%o *] i.items)  [%| 'plan concurrency entry must be an object']
  =/  f  p.i.items
  =/  w=(unit @t)  (text-field:ci-expr f 'workflow')
  =/  n=(unit @t)  (text-field:ci-expr f 'name')
  ?.  ?&(?=(^ w) ?=(^ n))  [%| 'plan concurrency entry needs workflow and name']
  ?:  (~(has in seen) u.w)  [%| 'duplicate workflow concurrency declaration']
  =/  one  (parse (~(get by f) 'concurrency'))
  ?:  ?=(%| -.one)  [%| p.one]
  $(items t.items, out [[u.w u.n p.one] out], seen (~(put in seen) u.w))
--
