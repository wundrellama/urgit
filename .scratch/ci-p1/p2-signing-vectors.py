#!/usr/bin/env python3
"""Capture public-only Ed25519/jam vectors from the actual pinned ship."""
import json, os, pathlib, re, runpy, subprocess, sys, types

t=types.SimpleNamespace(**runpy.run_path(str(pathlib.Path(__file__).with_name('p2-trust.py'))))
key=t.ok('GET','/ci/key')
app=(t.ROOT/'desk/app/urgit-ci.hoon').read_text()
helper=app[app.index('++  signing-json\n'):app.index('++  key-json\n')]
body={'z':[0,0,True,False,None],'a':'repeat','b':'repeat','nested':{'x':'é','empty':''}}
fixture=json.dumps(body,ensure_ascii=False,separators=(',',':'))
source=''':: Harness-only public signing vectors. Generated from the current helper.
:-  %say
|=  [[now=@da eny=@uvJ bec=beak] ~ ~]
:-  %noun
|^
=/  pair  (luck:ed:crypto (shas %ci-vector eny))
=/  payload=json  (need (de:json:html 'JSON'))
=/  msg=@ux  (jam [0v1 0v2 [%assignment (signing-json payload)] ~2026.1.1 0v3])
=/  sig=@ux  (sign-raw:ed:crypto msg pub.pair sek.pair)
=/  deed=[life=@ud pass=@ rest=(unit @)]
  .^([@ud @ (unit @)] %j /(scot %p p.bec)/deed/(scot %da now)/(scot %p p.bec)/(scot %ud LIFE))
=/  network  (com:nu:crub:crypto pass.deed)
?>  (safe:as.network CERT PUB)
:*  `@ux`(jam [0 0])
    `@ux`(jam [1 1])
    `@ux`(jam [[1 2] [1 2]])
    pub.pair
    msg
    sig
    `@ux`(end 8 (rsh 3 pass.deed))
    %.y
==
'''.replace('JSON',fixture).replace('LIFE',str(key['ship-life'])).replace('CERT',key['cert']).replace('PUB',key['pub'])+helper+'--\n'
path=pathlib.Path(os.environ['PIER'])/'urgit/gen/p2-signing-vector.hoon'
path.write_text(source)
t.dojo('|commit %urgit')
result=t.dojo('+urgit!p2-signing-vector')
(t.TMP/'s4-public-vector.txt').write_text(result)
nums=re.findall(r'0x[0-9a-f.]+',result)
assert len(nums)==7 and '%.y' in result, result
vector=dict(json=body,recipient='0v1',attempt='0v2',expiry=1767225600,nonce='0v3',jam=nums[:3],pub=nums[3],message=nums[4],sig=nums[5],networkPub=nums[6],ciPub=key['pub'],cert=key['cert'])
folder=t.ROOT/'runner/internal/signing/testdata';folder.mkdir(exist_ok=True)
target=folder/'hoon.json'
original=target.read_bytes() if target.exists() else None
captured=(json.dumps(vector,ensure_ascii=False,indent=2)+'\n').encode()
target.write_bytes(captured)
if len(sys.argv)>1 and sys.argv[1]=='check':
    # Cold verification keeps its random public keys in the evidence directory.
    # Test the live capture with Go, then restore the pre-run fixture exactly.
    try:
        (t.TMP/'s6-hoon-vector.json').write_bytes(captured)
        subprocess.run(['go','test','./internal/signing','-run','^TestPinnedHoonVectors$','-count=1'],cwd=t.ROOT/'runner',check=True)
    finally:
        if target.read_bytes()!=captured:
            raise RuntimeError('public vector fixture changed during verification; preserving it')
        if original is None: target.unlink()
        else: target.write_bytes(original)
print('Pinned ship: +luck, +sign-raw, canonical JSON/+jam vectors captured; Jael deed public key verifies live CI certificate')
