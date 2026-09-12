::  :urgit-ci|mint-enroll-token
::
::    mints a 32-byte enrollment token, prints it once, and registers its
::    hash with %urgit-ci.  the token itself never enters agent state.
::
/-  ci
:-  %say
|=  [[now=@da eny=@uvJ bec=beak] ~ ~]
:-  %ci-action
=/  token=@uv  (end [3 32] eny)
~&  [%ci-enroll-token token]
^-  action:ci
[%mint-enroll-token token]
