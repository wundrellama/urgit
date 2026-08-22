=/  m  (strand ,vase)
=/  take-any
  =/  n  (strand ,(unit [wire sign-arvo]))
  ^-  form:n
  |=  tin=strand-input:strand
  ?+  in.tin  `[%skip ~]
      ~  `[%wait ~]
      [~ %sign [%deadline ~] %behn %wake *]  `[%done ~]
      [~ %sign *]  `[%done `[wire sign-arvo]:u.in.tin]
  ==
;<  our=@p  bind:m  get-our
;<  now=@da  bind:m  get-time
=/  pax=path  /g/x/1/spider//1/probe/blob5
=/  til=@da  (add now ~s45)
;<  ~  bind:m  (send-raw-card [%pass /deadline %arvo %b %wait til])
;<  start=@da  bind:m  get-time
;<  ~  bind:m
  %-  send-raw-cards
  :~  [%pass /pk %arvo %a %keen ~ ~pec pax]
      [%pass /pg %arvo %a %prog [~pec pax] [%keen ~] 1]
  ==
=|  log=(list @t)
|-  ^-  form:m
=*  loop  $
;<  got=(unit [wire sign-arvo])  bind:m  take-any
?~  got
  (pure:m !>([%drained n=(lent log) (flop log)]))
;<  t=@da  bind:m  get-time
=/  w=wire  -.u.got
=/  s=sign-arvo  +.u.got
=/  ms=@ud  (div (mul 1.000 (sub t start)) ~s1)
=/  ent=@t
  ?+  s  (crip "+{<ms>}ms OTHER wire={<w>} vane={<-.s>} card={<+<.s>}")
    [%ames %sage *]
      (crip "+{<ms>}ms SAGE wire={<w>} path={<path.p.sage.s>} gage-bytes={<(met 3 (jam q.sage.s))>}")
    [%ames %rate *]
      (crip "+{<ms>}ms RATE wire={<w>} spar-and-rate={<+>.s>}")
  ==
loop(log [ent log])
