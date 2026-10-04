::  git-profile: Public repository and Landscape profile views
::
/-  git, git-peer
/+  *git-format, *git-repository
|%
+$  profile-value  $@(~ [kind=@tas value=*])
+$  profile-contact  (map @tas profile-value)
++  profile-field
  |=  [contact=profile-contact field=@tas kind=@tas]
  ^-  @t
  =/  found=(unit profile-value)  (~(get by contact) field)
  ?~  found  ''
  ?@  u.found  ''
  ?.  =(kind kind.u.found)  ''
  ?@  value.u.found  `@t`value.u.found
  ''
::
++  profile-color
  |=  contact=profile-contact
  ^-  @t
  =/  found=(unit profile-value)  (~(get by contact) %color)
  ?~  found  ''
  ?@  u.found  ''
  ?.  =(%tint kind.u.found)  ''
  ?@  value.u.found  (scot %ux `@ux`value.u.found)
  ''
::
++  repository-updated-at
  |=  repo=repository:git
  ^-  @t
  =/  head-oid=(unit oid:git)  (~(get by refs.repo) head.repo)
  ?~  head-oid  ''
  =/  found=(unit object:git)  (~(get by objects.repo) u.head-oid)
  ?.  &(?=(^ found) =(%commit kind.u.found))  ''
  =/  identity=(unit commit-identity)  (commit-identity-at data.u.found 'committer ')
  ?~  identity  ''
  timestamp.u.identity
::
++  profile-repository-json
  |=  [name=@t repo=repository:git]
  ^-  json
  %-  pairs:enjs:format
  :~  ['name' s+name]
      ['description' s+description.repo]
      ['head' s+head.repo]
      ['updatedAt' s+(repository-updated-at repo)]
      ['branchCount' n+(decimal (ref-count-prefix refs.repo 'refs/heads/'))]
      ['tagCount' n+(decimal (ref-count-prefix refs.repo 'refs/tags/'))]
  ==
::
++  public-profile-json
  |=  [who=@p now=@da repos=(map @t repository:git)]
  ^-  json
  =/  result=(each profile-contact tang)
    %-  mule
    |.
    .^(profile-contact %gx /(scot %p who)/contacts/(scot %da now)/v1/self/contact-1)
  =/  published=?  !?=(%| -.result)
  =/  contact=profile-contact
    ?:  ?=(%| -.result)  *profile-contact
    p.result
  =/  public-repositories=(list json)
    %+  murn  ~(tap by repos)
    |=  entry=[@t repository:git]
    ?.  public-read.+.entry  ~
    `(profile-repository-json -.entry +.entry)
  =/  profile-json=json
    ?.  published  ~
    %-  pairs:enjs:format
    :~  ['nickname' s+(profile-field contact %nickname %text)]
        ['bio' s+(profile-field contact %bio %text)]
        ['status' s+(profile-field contact %status %text)]
        ['avatar' s+(profile-field contact %avatar %look)]
        ['cover' s+(profile-field contact %cover %look)]
        ['color' s+(profile-color contact)]
    ==
  %-  pairs:enjs:format
  :~  ['ship' s+(scot %p who)]
      ['profilePublished' b+published]
      ['profile' profile-json]
      ['repositories' [%a public-repositories]]
  ==
::
--
