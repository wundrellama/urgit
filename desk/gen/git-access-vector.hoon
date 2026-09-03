::  Native repository access policy vectors.
::
/-  git
/+  git-access
:-  %say
|=  *
:-  %noun
=/  owner=@p  ~zod
=/  reader=@p  ~nec
=/  writer=@p  ~bud
=/  stranger=@p  ~wes
=/  readers=(set @p)  (silt ~[reader])
=/  writers=(set @p)  (silt ~[writer])
::  explicit lists behave as before when no group contributes
::
?>  (can-read:git-access %.y owner readers writers %none stranger)
?>  (can-read:git-access %.n owner readers writers %none owner)
?>  (can-read:git-access %.n owner readers writers %none reader)
?>  (can-read:git-access %.n owner readers writers %none writer)
?>  !(can-read:git-access %.n owner readers writers %none stranger)
?>  (can-write:git-access owner writers %none owner)
?>  (can-write:git-access owner writers %none writer)
?>  !(can-write:git-access owner writers %none reader)
?>  !(can-write:git-access owner writers %none stranger)
::  a group capability is a third way in, and write implies read
::
?>  (can-read:git-access %.n owner readers writers %read stranger)
?>  (can-read:git-access %.n owner readers writers %write stranger)
?>  (can-write:git-access owner writers %write stranger)
?>  !(can-write:git-access owner writers %read stranger)
?>  !(can-write:git-access owner writers %none stranger)
::  an explicit reader still reads with a policy present that grants nothing
::
?>  (can-read:git-access %.n owner readers writers %none reader)
::  the owner always has full access whatever the group says
::
?>  (can-read:git-access %.n owner ~ ~ %none owner)
?>  (can-write:git-access owner ~ %none owner)
::  a defaulted capability grants nothing
::
?>  =(%none *capability:git)
::  seats and roles
::
=/  policy=group-policy:git
  :*  group=[host=owner name=%crew]
      base=%none
      roles=(my ~[[%verified %write] [%legacy %read] [%neophyte %none]])
  ==
=/  seat
  |=  roles=(list @tas)
  ^-  (unit group-seat:git)
  `[(silt roles) *@da]
::  absent policy contributes nothing whatever the seat says
?>  =(%none (group-capability:git-access ~ (seat ~[%verified])))
::  absent seat (unseated ship, or a failed %groups read) contributes nothing
?>  =(%none (group-capability:git-access `policy ~))
::  seated with no roles takes the base capability
?>  =(%none (group-capability:git-access `policy (seat ~)))
?>  =(%read (group-capability:git-access [~ policy(base %read)] (seat ~)))
::  a single mapped role
?>  =(%read (group-capability:git-access `policy (seat ~[%legacy])))
?>  =(%write (group-capability:git-access `policy (seat ~[%verified])))
::  an unmapped role contributes nothing, even with a permissive base
?>  =(%none (group-capability:git-access `policy (seat ~[%unknown])))
?>  =(%none (group-capability:git-access [~ policy(base %read)] (seat ~[%unknown])))
::  roles union to the strongest
?>  =(%write (group-capability:git-access `policy (seat ~[%legacy %verified])))
::  a %none-mapped role does not cancel a %write role
?>  =(%write (group-capability:git-access `policy (seat ~[%neophyte %verified])))
?>  =(%none (group-capability:git-access `policy (seat ~[%neophyte])))
::  the capability feeds both checks: write implies read
=/  cap=capability:git  (group-capability:git-access `policy (seat ~[%verified]))
?>  (can-read:git-access %.n owner readers writers cap stranger)
?>  (can-write:git-access owner writers cap stranger)
::  a hosted group is authoritative; a joined group's mirror counts only
::  while it is initialised and this ship is still seated in it
::
?>  (mirror-trusted:git-access %pub %.y %.y)
?>  (mirror-trusted:git-access %pub %.n %.n)
?>  (mirror-trusted:git-access %sub %.y %.y)
?>  !(mirror-trusted:git-access %sub %.n %.y)
?>  !(mirror-trusted:git-access %sub %.y %.n)
%.y
