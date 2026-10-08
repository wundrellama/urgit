::  Peer fragment assembly preserves octet lengths and rejects invalid streams.
::
/-  git, git-peer
/+  git-codec, git-peer-transfer
:-  %say
|=  *
:-  %noun
=/  flight=peer-receive:git-peer-transfer
  *peer-receive:git-peer-transfer
=.  expected.flight  1
=/  data=octs  [2 0x61]
=/  =oid:git  (object-oid:git-codec %blob data)
=/  fragment=object-fragment:git-peer  [oid %blob 2 0 data]
=/  assembled=(unit peer-receive:git-peer-transfer)
  (assemble-peer-fragments:git-peer-transfer flight ~[fragment])
?>  ?=(^ assembled)
=/  object=(unit object:git)  (~(get by objects.u.assembled) oid)
?>  ?=(^ object)
=/  empty-oid=oid:git  (object-oid:git-codec %blob [0 0])
=/  empty=(unit peer-receive:git-peer-transfer)
  (assemble-peer-fragments:git-peer-transfer flight ~[[empty-oid %blob 0 0 [0 0]]])
:~  =(1 received.u.assembled)
    =(data data.u.object)
    ?=(~ assemblies.u.assembled)
    ?=(^ empty)
    ?~  (assemble-peer-fragments:git-peer-transfer flight ~[fragment fragment])
      %.y
    %.n
    ?~  (assemble-peer-fragments:git-peer-transfer flight ~[fragment(oid 0x0)])
      %.y
    %.n
    ?~  (assemble-peer-fragments:git-peer-transfer flight ~[fragment(offset 1)])
      %.y
    %.n
    ?~  (assemble-peer-fragments:git-peer-transfer flight ~[fragment(total 3)])
      %.y
    %.n
    ?~  (assemble-peer-fragments:git-peer-transfer flight ~[fragment(data [1 0x1.0000])])
      %.y
    %.n
==
