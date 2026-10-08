::  LFS batches preserve repository data and stage uploads with bounded lifetimes.
::
/-  git
/+  git-lfs, *git-http
:-  %say
|=  *
:-  %noun
=/  repo=repository:git  *repository:git
=/  settings=lfs-settings:git-lfs
  [['https://objects.example.test' 'key' 'secret'] ['bucket' 'us-east-1']]
=/  req=inbound-request:eyre  *inbound-request:eyre
=/  spec=lfs-spec:git-lfs
  ['0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef' 2]
=/  prepare
  |=  [working=repository:git operation=@t]
  %:  lfs-batch-object:git-lfs
    working
    spec
    operation
    settings
    ~zod
    ~2026.1.1
    req
    'test'
  ==
=/  absent=[item=json repo=repository:git]  (prepare repo 'download')
=/  absent-error=(unit json)  (json-at 'error' item.absent)
?>  ?=(^ absent-error)
=/  upload=[item=json repo=repository:git]  (prepare repo 'upload')
=/  pending=(unit lfs-upload:git)  (~(get by lfs-uploads.repo.upload) oid.spec)
?>  ?=(^ pending)
=/  stored=repository:git
  repo(lfs-objects (~(put by lfs-objects.repo) oid.spec [2 'object-key']))
=/  present=[item=json repo=repository:git]  (prepare stored 'download')
=/  existing=[item=json repo=repository:git]  (prepare stored 'upload')
=/  mismatched=repository:git
  repo(lfs-objects (~(put by lfs-objects.repo) oid.spec [3 'object-key']))
=/  mismatch-download=[item=json repo=repository:git]  (prepare mismatched 'download')
=/  mismatch-upload=[item=json repo=repository:git]  (prepare mismatched 'upload')
:~  =(repo repo.absent)
    =(`404 (nat-at 'code' u.absent-error))
    =(2 size.u.pending)
    =((add ~2026.1.1 ~m15) expires.u.pending)
    =(objects.repo objects.repo.upload)
    =(stored repo.present)
    ?=(^ (json-at 'actions' item.present))
    =(stored repo.existing)
    ?=(~ (json-at 'actions' item.existing))
    =(mismatched repo.mismatch-download)
    ?=(^ (json-at 'error' item.mismatch-download))
    =(mismatched repo.mismatch-upload)
    ?=(^ (json-at 'error' item.mismatch-upload))
==
