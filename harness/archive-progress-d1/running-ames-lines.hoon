=/  m  (strand ,vase)
;<  our=@p  bind:m  get-our
;<  now=@da  bind:m  get-time
=/  f  .^(@t %cx /(scot %p our)/base/(scot %da now)/sys/vane/ames/hoon)
=/  t=tape  (trip f)
=/  lineno
  |=  needle=tape
  ^-  @ud
  =/  i=(unit @ud)  (find needle t)
  ?~  i  0
  +((lent (skim (scag u.i t) |=(c=@t =(10 c)))))
=/  res
  :*  bytes=(met 3 f)
      pe-prog=(lineno "++  pe-prog")
      pe-rate=(lineno "++  pe-rate")
      ev-add-rate=(lineno "++  ev-add-rate")
      ev-give-rate=(lineno "++  ev-give-rate")
      ev-give-sage=(lineno "++  ev-give-sage")
      fi-rat=(lineno "++  fi-rat")
      rate-task-comment=(lineno "get rate progress for +peeks, from unix")
  ==
(pure:m !>(res))
