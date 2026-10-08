::  ci-provenance-vector: the cross-language manifest vector (P10; contract
::  §5).  The reference manifest below is the one runner/internal/sig/
::  manifest_test.go signs with the same fixed seed; both sides must
::  produce the same jam bytes, and every single-field mutation must fail
::  to verify against the reference signature.  The generator prints the
::  jam hex, the signature hex, the count, and ends in a loobean: %.y only
::  when the jam matches the Go side's pinned bytes, the reference
::  verifies, and EVERY mutation refuses.  The lock digest and the policy
::  predicates have their own vector (ci-policy-vector).
::
/-  ci
/+  ci-provenance, ci-recovery
:-  %say
|=  *
:-  %noun
=/  parse-uv
  |=  text=@t
  ^-  @uv
  =/  digits=tape  (skip (trip text) |=(c=@tD =('.' c)))
  =/  body=tape  ?:(=("0v" (scag 2 digits)) (slag 2 digits) digits)
  (rash (crip body) (bass 32 (plus siv:ab)))
::  the fixed seed: bytes 0x01..0x20, byte i the atom's i-th byte
::
=/  seed=@
  =/  i=@ud  0
  =/  acc=@  0
  |-
  ?:  =(i 32)  acc
  $(i +(i), acc (add acc (lsh [3 i] +(i))))
=/  pair  (luck:ed:crypto seed)
=/  =manifest:ci-provenance
  :*  (parse-uv '0v4.incar')
      'erpit'
      'refs/heads/master'
      (parse-uv '0v5.cand')
      'a6d15eddefa898250bf5f656441a55a80273f751'
      '0000000000000000000000000000000000000abc'
      '1111111111111111111111111111111111111111111111111111111111111111'
      7
      'suite.yml'
      'suite'
      'trusted'
      'vm'
      'required'
      'locked'
      ''
  ==
=/  recipient=@uv  (parse-uv '0v1.daemon')
=/  attempt=@uv  (parse-uv '0v2.attempt')
=/  nonce=@uv  (parse-uv '0v3.nonce')
=/  expiry=@ud  1.900.000.000
=/  message=@  (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce manifest)
=/  jam-hex=@t  (hex-bytes:ci-provenance message (met 3 message))
::  what the Go side printed for the same message (manifest_test.go)
::
=/  expected-jam=@t
  %+  rap  3
  :~  '2103fec5d6a936c0d2b375bd2b037c6173736967ee800f30fb130fb88e5d7c0fc85bb12b19e02993834ba307e041aecc6cee05ad2c8c6ceea52d6c8eae4c1ef0da55ac01d027cc862ca6a68c8caccc2c0c270747a60646ccacc6cca6c686862626aca6260c0746e666c6eca6260e803f303030303030303030303030303030303030303030303030303030303030303030303030306162e300e81f131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313871fc063ae2e8daecc25af8d1de099ab4ba32b0778e9e4eae6e8cac881b76b07f8e5cae2ead2e4cac8013eb6b7b1b532b2'
  ==
=/  sig=@ux  (sign-raw:ed:crypto message pub.pair sek.pair)
=/  sig-hex=@t  (hex-bytes:ci-provenance sig 64)
::  the Go side's signature over the same bytes with the same seed
::
=/  expected-sig=@t
  'f4dc4eb06c51437257b981f351199668a971e15bcabbef1343d20c0d52afc1a3d680600cd7d7bd7c0c3338452f1fba702e902b804f70cd4b2a9922464f0ddc0b'
=/  sig-matches=?  =(sig-hex expected-sig)
=/  verifies=?  (veri:ed:crypto sig message pub.pair)
::  every single-field mutation of the message must refuse the signature
::
=/  mutations=(list [name=@t message=@])
  =/  mm  manifest
  :~  ['recipient' (message-bytes:ci-provenance (parse-uv '0v9') attempt 'assign' expiry nonce mm)]
      ['attempt' (message-bytes:ci-provenance recipient (parse-uv '0v9') 'assign' expiry nonce mm)]
      ['operation' (message-bytes:ci-provenance recipient attempt 'grant:X' expiry nonce mm)]
      ['expiry' (message-bytes:ci-provenance recipient attempt 'assign' +(expiry) nonce mm)]
      ['nonce' (message-bytes:ci-provenance recipient attempt 'assign' expiry (parse-uv '0v9') mm)]
      ['incarnation' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(incarnation (parse-uv '0v9')))]
      ['repo' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(repo 'other'))]
      ['ref' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(ref 'refs/heads/dev'))]
      ['candidate' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(candidate (parse-uv '0v9')))]
      ['oid' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(oid 'a6d15eddefa898250bf5f656441a55a80273f752'))]
      ['baseline' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(baseline ''))]
      ['lock' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(lock '2222222222222222222222222222222222222222222222222222222222222222'))]
      ['generation' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(generation 8))]
      ['workflow' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(workflow 'fixtures.yml'))]
      ['job' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(job 'plan'))]
      ['trust' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(trust 'untrusted'))]
      ['sandbox' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(sandbox 'container'))]
      ['mode' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(mode 'trial'))]
      ['network' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(network 'integration'))]
      ['network-scope' (message-bytes:ci-provenance recipient attempt 'assign' expiry nonce mm(scope 'tcp:198.51.100.20:8472'))]
  ==
=/  refused=(list @t)
  %+  murn  mutations
  |=  [name=@t m=@]
  ?:((veri:ed:crypto sig m pub.pair) ~ `name)
=/  accepted=(list @t)
  %+  murn  mutations
  |=  [name=@t m=@]
  ?:((veri:ed:crypto sig m pub.pair) `name ~)
::  a v1-shaped message over the same fields is a different noun
::
=/  v1=@  (jam [recipient attempt 'assign' expiry nonce])
=/  v1-refused=?  !(veri:ed:crypto sig v1 pub.pair)
=/  jam-matches=?  =(jam-hex expected-jam)
::  the same assignment of authorization epoch 1 (legacy-replay-upgrade
::  ruling 01; INTEGRATION.md §11.15): its nonce under the epoch
::  (epoch-nonce), whose bytes the Go side pins (epoch_test.go); a
::  signature over it does not verify for the nonce's low bits alone
::
=/  epoch-message=@
  %:  message-bytes:ci-provenance
    recipient
    attempt
    'assign'
    expiry
    (epoch-nonce:ci-recovery 1 nonce)
    manifest
  ==
=/  epoch-jam-hex=@t  (hex-bytes:ci-provenance epoch-message (met 3 epoch-message))
=/  expected-epoch-jam=@t
  '2103fec5d6a936c0d2b375bd2b037c6173736967ee800f30fb130fc0806317df010000000000000000000000c080bc15bb92019e3239b8347a001ee4cacce65ed0cac2c8e65edac2e6e8cae401af5dc51a007dc26cc8626acac8c8caccc2707270646a60c4cc6acc6c6a6c686862c26a6ac27060646e66cc6e6ae200f8030303030303030303030303030303030303030303030303030303030303030303030303031326360e80fe31313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313131313171f8013ce6ead2e8ca5cf2dad8019eb9ba34ba7280974eae6e8eae8c1c78bb76805fae2cae2e4dae8c1ce0637b1b5b2b230b'
=/  epoch-jam-matches=?  =(epoch-jam-hex expected-epoch-jam)
=/  epoch-sig=@ux  (sign-raw:ed:crypto epoch-message pub.pair sek.pair)
=/  epoch-low-refused=?  !(veri:ed:crypto epoch-sig message pub.pair)
=/  passed=@ud
  ;:  add
      ?:(jam-matches 1 0)
      ?:(sig-matches 1 0)
      ?:(verifies 1 0)
      ?:(v1-refused 1 0)
      ?:(epoch-jam-matches 1 0)
      ?:(epoch-low-refused 1 0)
      (lent refused)
  ==
=/  total=@ud  (add 6 (lent mutations))
~&  [%manifest-id (manifest-id:ci-provenance manifest)]
~&  [%jam-hex jam-hex]
~&  [%jam-matches-go jam-matches]
~&  [%sig-hex sig-hex]
~&  [%sig-matches-go sig-matches]
~&  [%reference-verifies verifies]
~&  [%v1-refused v1-refused]
~&  [%epoch-jam-hex epoch-jam-hex]
~&  [%epoch-jam-matches-go epoch-jam-matches]
~&  [%epoch-low-refused epoch-low-refused]
~&  [%mutations-refused (lent refused) %of (lent mutations)]
~&  [%mutations-accepted accepted]
~&  (crip "passed={<passed>} of={<total>}")
?&  jam-matches
    sig-matches
    verifies
    v1-refused
    epoch-jam-matches
    epoch-low-refused
    =((lent refused) (lent mutations))
    ?=(~ accepted)
==
