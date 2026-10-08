::  git-clay-view: Clay revision materialization and repository history views
::
/-  git, git-peer
/+  git-blame, git-clay, git-clay-history, git-codec, *git-format, *git-json, *git-repository
|%
++  page-octs
  |=  [our=@p desk-name=desk now=@da =page]
  ^-  (unit octs)
  ?:  =(%hoon p.page)
    =/  source=@t  ;;(@ q.page)
    `[(met 3 source) source]
  ?:  =(%kelvin p.page)
    =/  kal=waft:clay  ;;(waft:clay q.page)
    =/  source=@t
      %+  rap  3
      %+  turn
        %+  sort
          ~(tap in (waft-to-wefts:clay kal))
        |=  [a=weft b=weft]
        ?:  =(lal.a lal.b)
          (gte num.a num.b)
        (gte lal.a lal.b)
      |=  =weft
      (rap 3 '[%' (scot %tas lal.weft) ' ' (scot %ud num.weft) ']\0a' ~)
    `[(met 3 source) source]
  =/  converted=(unit mime)
    %-  mole
    |.
    ?:  =(%mime p.page)
      ;;(mime q.page)
    =/  =dais:clay
      .^(dais:clay %cb /(scot %p our)/[desk-name]/(scot %da now)/[p.page])
    =/  vax=vase  (vale:dais q.page)
    =/  =tube:clay
      .^(tube:clay %cc /(scot %p our)/[desk-name]/(scot %da now)/[p.page]/mime)
    !<(mime (tube vax))
  ?~  converted  ~
  `q.u.converted
::
++  clay-files-at-revision
  |=  [who=@p desk-name=desk number=@ud now=@da]
  ^-  (unit (map path octs))
  =/  domo=(unit domo:clay)
    (desk-domo:git-clay-history who desk-name now)
  ?~  domo  ~
  ?:  |(=(number 0) (gth number let.u.domo))  ~
  =/  revision=(unit revision:git-clay-history)
    (revision-meta:git-clay-history who desk-name number u.domo)
  ?~  revision  ~
  =/  yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name number tako.u.revision)
  ?~  yaki  ~
  =/  remaining=(list [path lobe:clay])  ~(tap by q.u.yaki)
  =/  result=(map path octs)  ~
  |-
  ?~  remaining  `result
  =/  data=(unit octs)
    (clay-file-octs who desk-name number +.i.remaining now)
  ?~  data  ~
  $(remaining t.remaining, result (~(put by result) -.i.remaining u.data))
::
++  materialize-clay-revision
  |=  [repo=repository:git number=@ud who=@p now=@da]
  ^-  (unit [repo=repository:git commit=oid:git])
  ?~  binding.repo  ~
  =/  existing=(unit clay-link:git)
    (clay-link-for-revision number history.u.binding.repo)
  ?^  existing
    =/  object=(unit object:git)  (~(get by objects.repo) commit.u.existing)
    ?:  &(?=(^ object) =(%commit kind.u.object))
      `[repo commit.u.existing]
    ~
  =/  domo=(unit domo:clay)
    (desk-domo:git-clay-history who desk-name.u.binding.repo now)
  ?~  domo  ~
  =/  revision=(unit revision:git-clay-history)
    (revision-meta:git-clay-history who desk-name.u.binding.repo number u.domo)
  ?~  revision  ~
  =/  files=(unit (map path octs))
    (clay-files-at-revision who desk-name.u.binding.repo number now)
  ?~  files  ~
  =/  message=@t
    (rap 3 ~['Clay revision ' (decimal number) ' of %' desk-name.u.binding.repo])
  =/  snapped=(unit [commit=oid:git objects=(map oid:git object:git)])
    (snapshot:git-clay u.files objects.repo ~ who timestamp.u.revision message)
  ?~  snapped  ~
  =/  link=clay-link:git
    [number commit.u.snapped %clay-to-git timestamp.u.revision]
  =/  linked=desk-binding:git
    u.binding.repo(history [link history.u.binding.repo])
  =/  updated=repository:git
    repo(objects objects.u.snapped, binding `linked)
  `[updated commit.u.snapped]
::
++  clay-identity-json
  |=  [who=@p desk-name=desk timestamp=@da]
  ^-  json
  =/  ship-text=@t  (scot %p who)
  %-  pairs:enjs:format
  :~  ['name' s+ship-text]
      ['email' s+(rap 3 ~[ship-text '@urbit'])]
      ['timestamp' s+(decimal (rsh [6 1] (sub timestamp ~1970.1.1)))]
      ['timezone' s+'+0000']
  ==
::
++  clay-revision-summary-json
  |=  [who=@p desk-name=desk revision=revision:git-clay-history binding=desk-binding:git]
  ^-  json
  =/  number=@ud  number.revision
  =/  parent=@t  ?:(=(number 1) '' (clay-revision-ref (dec number)))
  =/  identity=json  (clay-identity-json who desk-name timestamp.revision)
  =/  mapped=(unit clay-link:git)
    (clay-link-for-revision number history.binding)
  %-  pairs:enjs:format
  :~  ['kind' s+'clay']
      ['oid' s+(clay-revision-ref number)]
      ['parent' s+parent]
      ['parents' [%a ?:(=(number 1) ~ ~[s+parent])]]
      ['subject' s+(rap 3 ~['Clay revision ' (decimal number)])]
      ['author' identity]
      ['committer' identity]
      ['revision' n+(decimal number)]
      ['tako' s+(scot %uv tako.revision)]
      ['timestampCase' s+(scot %da timestamp.revision)]
      ['gitCommit' s+?~(mapped '' (oid-text:git-codec commit.u.mapped))]
      ['direction' s+?~(mapped '' direction.u.mapped)]
  ==
::
++  repository-history-json
  |=  [name=@t repo=repository:git ref=@t who=@p now=@da offset=@ud limit=@ud]
  ^-  json
  ?~  binding.repo
    (repository-commits-json name repo ref offset limit)
  ?.  =(ref branch.u.binding.repo)
    (repository-commits-json name repo ref offset limit)
  =/  native=(unit history:git-clay-history)
    (desk-history:git-clay-history who desk-name.u.binding.repo now (add offset +(limit)))
  ?~  native
    (repository-commits-json name repo ref offset limit)
  =/  page=(list revision:git-clay-history)
    (scag limit (slag offset revisions.u.native))
  =/  entries=(list json)
    %+  turn  page
    |=  revision=revision:git-clay-history
    (clay-revision-summary-json who desk-name.u.binding.repo revision u.binding.repo)
  %-  pairs:enjs:format
  :~  ['repository' s+name]
      ['head' s+ref]
      ['historyKind' s+'clay']
      ['revisionCount' n+(decimal latest.u.native)]
      ['offset' n+(decimal offset)]
      ['nextOffset' n+(decimal (add offset (lent page)))]
      ['hasMore' b+(gth (lent revisions.u.native) (add offset limit))]
      ['commits' [%a entries]]
  ==
::
++  clay-file-octs
  |=  [who=@p desk-name=desk number=@ud lobe=lobe:clay now=@da]
  ^-  (unit octs)
  =/  raw=(unit page)
    (revision-page:git-clay-history who desk-name number lobe)
  ?~  raw  ~
  (page-octs who desk-name now u.raw)
::
++  repository-file-at-history
  |=  [repo=repository:git identifier=@t file-path=path who=@p now=@da]
  ^-  (unit octs)
  =/  number=(unit @ud)  (clay-revision-number identifier)
  ?~  number  (repository-file-at repo identifier file-path)
  ?~  binding.repo  (repository-file-at repo identifier file-path)
  =/  desk-name=desk  desk-name.u.binding.repo
  =/  domo=(unit domo:clay)
    (desk-domo:git-clay-history who desk-name now)
  ?~  domo  ~
  =/  revision=(unit revision:git-clay-history)
    (revision-meta:git-clay-history who desk-name u.number u.domo)
  ?~  revision  ~
  =/  yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name u.number tako.u.revision)
  ?~  yaki  ~
  =/  lobe=(unit lobe:clay)  (~(get by q.u.yaki) file-path)
  ?~  lobe  ~
  (clay-file-octs who desk-name u.number u.lobe now)
::
++  clay-file-history-json
  |=  [name=@t repo=repository:git ref=@t file-path=path who=@p now=@da]
  ^-  (unit json)
  ?~  binding.repo  ~
  ?.  =(ref branch.u.binding.repo)  ~
  =/  desk-name=desk  desk-name.u.binding.repo
  =/  native=(unit history:git-clay-history)
    (desk-history:git-clay-history who desk-name now 100)
  ?~  native  ~
  =/  remaining=(list revision:git-clay-history)  revisions.u.native
  =/  entries=(list json)  ~
  |-
  ?~  remaining
    =/  result=json
      %-  pairs:enjs:format
      :~  ['repository' s+name]
          ['head' s+ref]
          ['path' s+(spat file-path)]
          ['historyKind' s+'clay']
          ['revisionCount' n+(decimal latest.u.native)]
          ['commits' [%a (flop entries)]]
      ==
    `result
  =/  =revision:git-clay-history  i.remaining
  =/  yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name number.revision tako.revision)
  ?~  yaki  $(remaining t.remaining)
  =/  here=(unit lobe:clay)  (~(get by q.u.yaki) file-path)
  =/  before=(unit lobe:clay)
    ?~  t.remaining  ~
    =/  prior=revision:git-clay-history  i.t.remaining
    =/  prior-yaki=(unit yaki:clay)
      (revision-yaki:git-clay-history who desk-name number.prior tako.prior)
    ?~  prior-yaki  ~
    (~(get by q.u.prior-yaki) file-path)
  ?:  =(here before)  $(remaining t.remaining)
  =/  data=(unit octs)
    ?~  here  ~
    (clay-file-octs who desk-name number.revision u.here now)
  =/  summary=json
    (clay-revision-summary-json who desk-name revision u.binding.repo)
  ?>  ?=([%o *] summary)
  =/  fields=(map @t json)  p.summary
  =.  fields  (~(put by fields) 'present' b+?=(^ here))
  =.  fields  (~(put by fields) 'size' n+(decimal ?~(data 0 p.u.data)))
  =/  item=json  [%o fields]
  $(remaining t.remaining, entries [item entries])
::
++  repository-file-history-view-json
  |=  [name=@t repo=repository:git ref=@t file-path=path who=@p now=@da]
  ^-  json
  =/  native=(unit json)
    (clay-file-history-json name repo ref file-path who now)
  ?~  native  (repository-file-history-json name repo ref file-path)
  u.native
::
++  clay-file-blame-json
  |=  [name=@t repo=repository:git ref=@t file-path=path who=@p now=@da]
  ^-  (unit json)
  ?~  binding.repo  ~
  =/  desk-name=desk  desk-name.u.binding.repo
  =/  domo=(unit domo:clay)
    (desk-domo:git-clay-history who desk-name now)
  ?~  domo  ~
  =/  requested=(unit @ud)  (clay-revision-number ref)
  =/  number=@ud
    ?~  requested
      ?.  =(ref branch.u.binding.repo)  0
      let.u.domo
    u.requested
  ?:  |(=(number 0) (gth number let.u.domo))  ~
  =/  revision=(unit revision:git-clay-history)
    (revision-meta:git-clay-history who desk-name number u.domo)
  ?~  revision  ~
  =/  yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name number tako.u.revision)
  ?~  yaki  ~
  =/  lobe=(unit lobe:clay)  (~(get by q.u.yaki) file-path)
  ?~  lobe  ~
  =/  current-data=(unit octs)
    (clay-file-octs who desk-name number u.lobe now)
  ?.  &(?=(^ current-data) (blame-safe u.current-data))  ~
  =/  slots=(list slot:git-blame)  (seed:git-blame u.current-data)
  ?:  (gth (lent slots) 10.000)  ~
  =/  sources=(list json)
    ~[(clay-revision-summary-json who desk-name u.revision u.binding.repo)]
  =/  cursor=@ud  number
  =/  scanned=@ud  1
  |-
  ?:  !(any-active:git-blame slots)
    `(file-blame-result-json name ref file-path 'clay' slots sources %.n)
  ?:  =(cursor 1)
    `(file-blame-result-json name ref file-path 'clay' slots sources %.n)
  ?:  (gte scanned 200)
    `(file-blame-result-json name ref file-path 'clay' slots sources %.y)
  =/  parent-number=@ud  (dec cursor)
  =/  parent-revision=(unit revision:git-clay-history)
    (revision-meta:git-clay-history who desk-name parent-number u.domo)
  ?~  parent-revision
    `(file-blame-result-json name ref file-path 'clay' slots sources %.y)
  =/  parent-yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name parent-number tako.u.parent-revision)
  ?~  parent-yaki
    `(file-blame-result-json name ref file-path 'clay' slots sources %.y)
  =/  parent-lobe=(unit lobe:clay)  (~(get by q.u.parent-yaki) file-path)
  ?~  parent-lobe
    `(file-blame-result-json name ref file-path 'clay' (deactivate:git-blame slots) sources %.n)
  =/  parent-data=(unit octs)
    (clay-file-octs who desk-name parent-number u.parent-lobe now)
  ?.  &(?=(^ parent-data) (blame-safe u.parent-data))
    `(file-blame-result-json name ref file-path 'clay' slots sources %.y)
  =/  next-slots=(list slot:git-blame)
    (step:git-blame slots u.parent-data scanned)
  =/  next-sources=(list json)
    (weld sources ~[(clay-revision-summary-json who desk-name u.parent-revision u.binding.repo)])
  $(slots next-slots, sources next-sources, cursor parent-number, scanned +(scanned))
::
++  repository-file-blame-view-json
  |=  [name=@t repo=repository:git ref=@t file-path=path who=@p now=@da]
  ^-  (unit json)
  =/  native=(unit json)
    (clay-file-blame-json name repo ref file-path who now)
  ?~  native  (git-file-blame-json name repo ref file-path)
  native
::
++  clay-change-json
  |=  [file-path=path previous=(unit octs) current=(unit octs)]
  ^-  json
  =/  status=@t  ?~(current 'deleted' ?~(previous 'added' 'modified'))
  =/  old-truncated=?  ?^(previous (gth p.u.previous 262.144) %.n)
  =/  new-truncated=?  ?^(current (gth p.u.current 262.144) %.n)
  %-  pairs:enjs:format
  :~  ['path' s+(spat file-path)]
      ['status' s+status]
      ['oldSize' n+(decimal ?~(previous 0 p.u.previous))]
      ['newSize' n+(decimal ?~(current 0 p.u.current))]
      ['oldContent' s+?~(previous '' ?:(old-truncated '' (en:base64:mimes:html u.previous)))]
      ['newContent' s+?~(current '' ?:(new-truncated '' (en:base64:mimes:html u.current)))]
      ['truncated' b+|(old-truncated new-truncated)]
  ==
::
++  clay-change-placeholder-json
  |=  [file-path=path status=@t]
  ^-  json
  %-  pairs:enjs:format
  :~  ['path' s+(spat file-path)]
      ['status' s+status]
      ['oldSize' n+'0']
      ['newSize' n+'0']
      ['oldContent' s+'']
      ['newContent' s+'']
      ['truncated' b+%.y]
  ==
::
++  clay-revision-detail-json
  |=  [name=@t repo=repository:git number=@ud who=@p now=@da]
  ^-  (unit json)
  ?~  binding.repo  ~
  =/  desk-name=desk  desk-name.u.binding.repo
  =/  domo=(unit domo:clay)
    (desk-domo:git-clay-history who desk-name now)
  ?~  domo  ~
  ?:  |(=(number 0) (gth number let.u.domo))  ~
  =/  revision=(unit revision:git-clay-history)
    (revision-meta:git-clay-history who desk-name number u.domo)
  ?~  revision  ~
  =/  current-yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name number tako.u.revision)
  ?~  current-yaki  ~
  =/  previous-number=(unit @ud)  ?:(=(number 1) ~ `(dec number))
  =/  previous-yaki=(unit yaki:clay)
    ?~  previous-number  ~
    =/  previous-tako=(unit tako:clay)  (~(get by hit.u.domo) u.previous-number)
    ?~  previous-tako  ~
    (revision-yaki:git-clay-history who desk-name u.previous-number u.previous-tako)
  =/  previous-files=(map path lobe:clay)
    ?~(previous-yaki *(map path lobe:clay) q.u.previous-yaki)
  =/  current-files=(map path lobe:clay)  q.u.current-yaki
  |^
    =/  changed-files=(list [path lobe:clay])
      %+  skim  ~(tap by current-files)
      |=  entry=[file-path=path lobe=lobe:clay]
      =/  old-lobe=(unit lobe:clay)  (~(get by previous-files) file-path.entry)
      !=(old-lobe `lobe.entry)
    =/  rich-changed=(list json)
      (murn (scag 12 changed-files) render-rich-changed)
    =/  remaining-changed=(list json)
      %+  turn  (scag 488 (slag 12 changed-files))
      |=  entry=[file-path=path lobe=lobe:clay]
      =/  old-lobe=(unit lobe:clay)  (~(get by previous-files) file-path.entry)
      =/  status=@t  ?~(old-lobe 'added' 'modified')
      (clay-change-placeholder-json file-path.entry status)
    =/  deleted-files=(list [path lobe:clay])
      %+  skim  ~(tap by previous-files)
      |=  entry=[file-path=path lobe=lobe:clay]
      =(%.n (~(has by current-files) file-path.entry))
    =/  rich-deleted=(list json)
      (murn (scag 12 deleted-files) render-rich-deleted)
    =/  remaining-deleted=(list json)
      %+  turn  (scag 488 (slag 12 deleted-files))
      |=  entry=[file-path=path lobe=lobe:clay]
      (clay-change-placeholder-json file-path.entry 'deleted')
    =/  changes=(list json)
      :(weld rich-changed remaining-changed rich-deleted remaining-deleted)
    =/  total-changes=@ud  (add (lent changed-files) (lent deleted-files))
    =/  summary=json
      (clay-revision-summary-json who desk-name u.revision u.binding.repo)
    =/  result=json
      %-  pairs:enjs:format
      :~  ['repository' s+name]
          ['historyKind' s+'clay']
          ['commit' summary]
          ['tree' s+(scot %uv tako.u.revision)]
          ['message' s+(rap 3 ~['Clay revision ' (decimal number)])]
          ['revision' n+(decimal number)]
          ['tako' s+(scot %uv tako.u.revision)]
          ['timestampCase' s+(scot %da timestamp.u.revision)]
          ['changedCount' n+(decimal total-changes)]
          ['changesTruncated' b+(gth total-changes 1.000)]
          ['changes' [%a changes]]
      ==
    `result
  ++  render-rich-changed
    |=  entry=[file-path=path lobe=lobe:clay]
    =/  old-lobe=(unit lobe:clay)  (~(get by previous-files) file-path.entry)
    =/  current=(unit octs)
      (clay-file-octs who desk-name number lobe.entry now)
    =/  previous=(unit octs)
      ?~  previous-number  ~
      ?~  old-lobe  ~
      (clay-file-octs who desk-name u.previous-number u.old-lobe now)
    `(clay-change-json file-path.entry previous current)
  ++  render-rich-deleted
    |=  entry=[file-path=path lobe=lobe:clay]
    =/  previous=(unit octs)
      ?~  previous-number  ~
      (clay-file-octs who desk-name u.previous-number lobe.entry now)
    `(clay-change-json file-path.entry previous ~)
  --
::
++  repository-history-detail-json
  |=  [name=@t repo=repository:git identifier=@t who=@p now=@da]
  ^-  (unit json)
  =/  clay-number=(unit @ud)  (clay-revision-number identifier)
  ?~  clay-number
    =/  parsed=(unit oid:git)  (revision-oid repo identifier)
    ?~  parsed  ~
    (repository-commit-json name repo u.parsed)
  ?~  binding.repo
    =/  parsed=(unit oid:git)  (revision-oid repo identifier)
    ?~  parsed  ~
    (repository-commit-json name repo u.parsed)
  (clay-revision-detail-json name repo u.clay-number who now)
::
::
++  clay-delta
  |=  [who=@p now=@da desk-name=desk files=(map path octs)]
  ^-  (unit nori:clay)
  =/  old-files=(unit (list spur))
    %-  mole
    |.(.^((list spur) %ct /(scot %p who)/[desk-name]/(scot %da now)))
  ?~  old-files  ~
  =/  old-set=(set path)  (silt u.old-files)
  =/  old-bytes=(unit (map path octs))
    (clay-current-files who now desk-name)
  =/  changes=(list [p=path q=miso:clay])
    %+  murn  ~(tap by files)
    |=  [file-path=path data=octs]
    ^-  (unit [p=path q=miso:clay])
    =/  old-data=(unit octs)
      ?~  old-bytes  ~
      (~(get by u.old-bytes) file-path)
    ?:  &(?=(^ old-data) =(u.old-data data))  ~
    =/  =mime  [/ data]
    =/  change=miso:clay
      ?:  (~(has in old-set) file-path)
        [%mut %mime !>(mime)]
      [%ins %mime !>(mime)]
    `[file-path change]
  =/  deletes=(list [p=path q=miso:clay])
    %+  murn  u.old-files
    |=  file-path=spur
    ^-  (unit [p=path q=miso:clay])
    ?:  (~(has by files) file-path)  ~
    `[file-path %del ~]
  `[%& (weld changes deletes)]
::
++  clay-current-files
  |=  [who=@p now=@da desk-name=desk]
  ^-  (unit (map path octs))
  =/  native=(unit history:git-clay-history)
    (desk-history:git-clay-history who desk-name now 1)
  ?~  native  ~
  ?~  revisions.u.native  `*(map path octs)
  =/  =revision:git-clay-history  i.revisions.u.native
  =/  yaki=(unit yaki:clay)
    (revision-yaki:git-clay-history who desk-name number.revision tako.revision)
  ?~  yaki  ~
  =/  remaining=(list [path lobe:clay])  ~(tap by q.u.yaki)
  =/  result=(map path octs)  ~
  |-
  ?~  remaining  `result
  =/  data=(unit octs)
    (clay-file-octs who desk-name number.revision +.i.remaining now)
  ?~  data  ~
  $(remaining t.remaining, result (~(put by result) -.i.remaining u.data))
::
++  file-maps-equal
  |=  [left=(map path octs) right=(map path octs)]
  ^-  ?
  ?.  =((lent ~(tap by left)) (lent ~(tap by right)))  %.n
  =/  remaining=(list [path octs])  ~(tap by left)
  |-
  ?~  remaining  %.y
  =/  found=(unit octs)  (~(get by right) -.i.remaining)
  ?.  &(?=(^ found) =(u.found +.i.remaining))  %.n
  $(remaining t.remaining)
::
++  clay-bridge-status-json
  |=  [name=@t repo=repository:git who=@p now=@da]
  ^-  json
  ?~  binding.repo
    %-  pairs:enjs:format
    ~[['bound' b+%.n] ['relation' s+'unbound']]
  =/  binding=desk-binding:git  u.binding.repo
  =/  native=(unit history:git-clay-history)
    (desk-history:git-clay-history who desk-name.binding now 1)
  =/  current-revision=(unit @ud)
    ?~  native  ~
    `latest.u.native
  =/  current-meta=(unit revision:git-clay-history)
    ?~  native  ~
    ?~  revisions.u.native  ~
    `i.revisions.u.native
  =/  branch-oid=(unit oid:git)  (~(get by refs.repo) branch.binding)
  =/  branch-files=(unit (map path octs))
    ?~  branch-oid  ~
    (flatten-commit:git-clay objects.repo u.branch-oid)
  =/  clay-files=(unit (map path octs))
    (clay-current-files who now desk-name.binding)
  =/  contents-match=?
    ?.  &(?=(^ branch-files) ?=(^ clay-files))  %.n
    (file-maps-equal u.branch-files u.clay-files)
  =/  clay-matches-last=?  =(current-revision last-clay.binding)
  =/  git-matches-last=?  =(branch-oid last-git.binding)
  =/  relation=@t
    ?:  ?&  ?=(^ last-clay.binding)
            ?=(^ last-git.binding)
            clay-matches-last
            git-matches-last
        ==
      'in-sync'
    ?:  contents-match  'in-sync'
    ?:  &(?=(^ last-clay.binding) ?=(^ last-git.binding) clay-matches-last !git-matches-last)
      'git-ahead'
    ?:  &(?=(^ last-clay.binding) ?=(^ last-git.binding) !clay-matches-last git-matches-last)
      'clay-ahead'
    ?:  &(?=(^ last-clay.binding) ?=(^ last-git.binding))
      'diverged'
    'unmapped'
  %-  pairs:enjs:format
  :~  ['bound' b+%.y]
      ['repository' s+name]
      ['desk' s+desk-name.binding]
      ['branch' s+branch.binding]
      ['relation' s+relation]
      ['contentsMatch' b+contents-match]
      ['canonicalDifference' b+&(=('in-sync' relation) !contents-match)]
      ['clayRevision' n+?~(current-revision '0' (decimal u.current-revision))]
      ['clayTimestamp' s+?~(current-meta '' (scot %da timestamp.u.current-meta))]
      ['clayTako' s+?~(current-meta '' (scot %uv tako.u.current-meta))]
      ['branchCommit' s+?~(branch-oid '' (oid-text:git-codec u.branch-oid))]
      ['mappedRevision' n+?~(last-clay.binding '0' (decimal u.last-clay.binding))]
      ['mappedCommit' s+?~(last-git.binding '' (oid-text:git-codec u.last-git.binding))]
      ['canApply' b+&(?=(^ branch-files) ?=(^ clay-files) !=('in-sync' relation))]
      ['canPublish' b+&(?=(^ clay-files) !=('in-sync' relation))]
  ==
::
++  update-binding-success
  |=  [repo=repository:git new-oid=oid:git clay-revision=(unit @ud) when=@da]
  ^-  repository:git
  ?~  binding.repo  repo
  =/  links=(list clay-link:git)
    ?~  clay-revision  history.u.binding.repo
    =/  link=clay-link:git  [u.clay-revision new-oid %git-to-clay when]
    =/  old-links=(list clay-link:git)  history.u.binding.repo
    [link old-links]
  =/  linked=desk-binding:git
    u.binding.repo(last-clay clay-revision, last-git `new-oid, history links)
  repo(binding `linked)
::
--
