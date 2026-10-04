::  git-repository: Git commit metadata, revision lookup, and file ancestry
::
/-  git, git-peer
/+  git-graph, git-clay, git-codec, git-protocol, git-tree, *git-format
|%
+$  commit-identity
  $:  name=@t
      email=@t
      timestamp=@t
      timezone=@t
  ==
::
++  latest-file-commits
  |=  [repo=repository:git ref=@t]
  (latest-file-commits-up-to repo ref 10.000)
::
++  latest-file-commits-up-to
  |=  [repo=repository:git ref=@t limit=@ud]
  ^-  (map path oid:git)
  =/  start=(unit oid:git)  (revision-oid repo ref)
  ?~  start  ~
  =/  head-files=(unit (map path flat-entry:git-tree))
    (flatten-commit-index:git-tree objects.repo u.start)
  ?~  head-files  ~
  =/  unresolved=(set path)
    %+  roll  ~(tap by u.head-files)
    |=  [entry=[file-path=path value=flat-entry:git-tree] accumulator=(set path)]
    (~(put in accumulator) file-path.entry)
  =/  commits=(map path oid:git)  ~
  =/  current=(unit oid:git)  start
  =/  scanned=@ud  0
  |-
  ?~  current  commits
  ?:  (gte scanned limit)  commits
  =/  unresolved-paths=(list path)  ~(tap in unresolved)
  ?~  unresolved-paths  commits
  =/  found=(unit object:git)  (~(get by objects.repo) u.current)
  ?.  &(?=(^ found) =(%commit kind.u.found))  commits
  =/  parent=(unit oid:git)  (commit-parent data.u.found)
  =/  changed=(set path)
    (changed-commit-files:git-tree objects.repo u.current parent)
  =/  remaining=(list path)  ~(tap in changed)
  =/  updated=[unresolved=(set path) commits=(map path oid:git)]
    |-
    ?~  remaining  [unresolved commits]
    =/  file-path=path  i.remaining
    ?.  (~(has in unresolved) file-path)
      $(remaining t.remaining)
    %=  $
      remaining  t.remaining
      unresolved  (~(del in unresolved) file-path)
      commits  (~(put by commits) file-path u.current)
    ==
  $(current parent, unresolved unresolved.updated, commits commits.updated, scanned +(scanned))
::
++  file-at-commit
  |=  [repo=repository:git commit=oid:git file-path=path]
  ^-  (unit octs)
  =/  files=(unit (map path octs))
    (flatten-commit:git-tree objects.repo commit)
  ?~  files  ~
  (~(get by u.files) file-path)
::
++  revision-oid
  |=  [repo=repository:git revision=@t]
  ^-  (unit oid:git)
  =/  named=(unit oid:git)  (~(get by refs.repo) revision)
  ?^  named  named
  =/  width=@ud  (met 3 revision)
  ?:  =(40 width)
    =/  parsed=(unit oid:git)  (oid-at:git-protocol [40 revision] 0)
    ?~  parsed  ~
    ?.  (~(has by objects.repo) u.parsed)  ~
    parsed
  ?.  &((gte width 4) (lth width 40))  ~
  =/  remaining=(list [oid:git object:git])  ~(tap by objects.repo)
  =/  match=(unit oid:git)  ~
  |-
  ?~  remaining  match
  =/  candidate=oid:git  -.i.remaining
  =/  candidate-text=@t  (oid-text:git-codec candidate)
  ?.  (starts-with:git-protocol [(met 3 candidate-text) candidate-text] revision)
    $(remaining t.remaining)
  ?^  match  ~
  $(remaining t.remaining, match `candidate)
::
++  repository-file-at
  |=  [repo=repository:git ref=@t file-path=path]
  ^-  (unit octs)
  =/  commit=(unit oid:git)  (revision-oid repo ref)
  ?~  commit  ~
  (file-at-commit repo u.commit file-path)
::
++  repository-file
  |=  [repo=repository:git file-path=path]
  (repository-file-at repo head.repo file-path)
::
++  double-newline
  |=  [data=octs offset=@ud]
  ^-  (unit @ud)
  ?:  (gte +(offset) p.data)  ~
  ?:  ?&  =(10 (byte-at:git-codec data offset))
          =(10 (byte-at:git-codec data +(offset)))
      ==
    `(add offset 2)
  $(offset +(offset))
::
++  commit-header
  |=  [data=octs prefix=@t]
  ^-  (unit @t)
  =/  prefix-width=@ud  (met 3 prefix)
  =/  scan
    |=  offset=@ud
    ^-  (unit @t)
    ?:  (gte offset p.data)  ~
    =/  end=(unit @ud)  (find-byte:git-clay data offset 10)
    ?~  end  ~
    =/  width=@ud  (sub u.end offset)
    ?:  =(width 0)  ~
    =/  line=octs  (slice:git-codec data offset width)
    ?:  (starts-with:git-protocol line prefix)
      =/  value=octs  (slice:git-codec line prefix-width (sub p.line prefix-width))
      =/  text=@t  `@t`q.value
      `text
    $(offset +(u.end))
  (scan 0)
::
++  commit-identity-from-line
  |=  line=@t
  ^-  (unit commit-identity)
  =/  data=octs  [(met 3 line) line]
  =/  open=(unit @ud)  (find-byte:git-clay data 0 60)
  ?~  open  ~
  =/  close=(unit @ud)  (find-byte:git-clay data +(u.open) 62)
  ?~  close  ~
  ?.  (gth u.close +(u.open))  ~
  =/  name-width=@ud
    ?:  ?&  (gth u.open 0)
            =(32 (byte-at:git-codec data (sub u.open 1)))
        ==
      (sub u.open 1)
    u.open
  =/  tail-start=@ud  (add u.close 2)
  ?:  (gte tail-start p.data)  ~
  =/  time-end=(unit @ud)  (find-byte:git-clay data tail-start 32)
  ?~  time-end  ~
  ?:  (gte +(u.time-end) p.data)  ~
  =/  name=octs  (slice:git-codec data 0 name-width)
  =/  email=octs  (slice:git-codec data +(u.open) (sub u.close +(u.open)))
  =/  timestamp=octs  (slice:git-codec data tail-start (sub u.time-end tail-start))
  =/  timezone=octs  (slice:git-codec data +(u.time-end) (sub p.data +(u.time-end)))
  `[`@t`q.name `@t`q.email `@t`q.timestamp `@t`q.timezone]
::
++  commit-identity-at
  |=  [data=octs prefix=@t]
  ^-  (unit commit-identity)
  =/  line=(unit @t)  (commit-header data prefix)
  ?~  line  ~
  (commit-identity-from-line u.line)
::
++  clay-revision-ref
  |=  number=@ud
  ^-  @t
  (cat 3 'r' (decimal number))
::
++  clay-revision-number
  |=  identifier=@t
  ^-  (unit @ud)
  =/  chars=tape  (trip identifier)
  ?.  &(?=(^ chars) =('r' i.chars) ?=(^ t.chars))  ~
  (slaw %ud (crip t.chars))
::
++  clay-link-for-revision
  |=  [number=@ud links=(list clay-link:git)]
  ^-  (unit clay-link:git)
  ?~  links  ~
  ?:  =(number clay-revision.i.links)  `i.links
  $(links t.links)
::
++  clay-link-for-commit
  |=  [commit-oid=oid:git links=(list clay-link:git)]
  ^-  (unit clay-link:git)
  ?~  links  ~
  ?:  =(commit-oid commit.i.links)  `i.links
  $(links t.links)
::
++  commit-parents
  |=  data=octs
  ^-  (list oid:git)
  =/  offset=@ud  0
  =/  parents=(list oid:git)  ~
  |-
  ?:  (gte offset p.data)  (flop parents)
  =/  end=(unit @ud)  (find-byte:git-clay data offset 10)
  ?~  end  (flop parents)
  =/  width=@ud  (sub u.end offset)
  ?:  =(width 0)  (flop parents)
  =/  line=octs  (slice:git-codec data offset width)
  =/  parent=(unit oid:git)
    ?:  (starts-with:git-protocol line 'parent ')
      (oid-at:git-protocol line 7)
    ~
  =?  parents  ?=(^ parent)  [u.parent parents]
  $(offset +(u.end), parents parents)
::
++  commit-parent
  |=  data=octs
  ^-  (unit oid:git)
  =/  parents=(list oid:git)  (commit-parents data)
  ?~  parents  ~
  `i.parents
::
++  commit-message
  |=  data=octs
  ^-  @t
  =/  start=(unit @ud)  (double-newline data 0)
  ?~  start  ''
  =/  message=octs  (slice:git-codec data u.start (sub p.data u.start))
  `@t`q.message
::
++  commit-subject
  |=  data=octs
  ^-  @t
  =/  text=@t  (commit-message data)
  =/  message=octs  [(met 3 text) text]
  =/  end=(unit @ud)  (find-byte:git-clay message 0 10)
  =/  width=@ud  ?~(end p.message u.end)
  =/  subject=octs  (slice:git-codec message 0 width)
  `@t`q.subject
::
++  repository-revision
  |=  repo=repository:git
  ^-  @t
  =/  visible
    :*  owner.repo
        public-read.repo
        description.repo
        head.repo
        refs.repo
        protected-refs.repo
        writers.repo
        readers.repo
        binding.repo
        peer-origin.repo
        github-origin.repo
        github-issues.repo
        github-pulls.repo
        native-pulls.repo
        native-issues.repo
        releases.repo
    ==
  (scot %uv (end 7 (shax (jam visible))))
::
++  first-parent-count
  |=  [repo=repository:git ref=@t]
  ^-  @ud
  =/  result=[count=@ud exact=?]
    (first-parent-count-up-to repo ref 10.000)
  count.result
::
++  first-parent-count-up-to
  |=  [repo=repository:git ref=@t limit=@ud]
  ^-  [count=@ud exact=?]
  =/  current=(unit oid:git)  (revision-oid repo ref)
  =/  count=@ud  0
  |-
  ?~  current  [count %.y]
  ?:  (gte count limit)  [count %.n]
  =/  found=(unit object:git)  (~(get by objects.repo) u.current)
  ?.  &(?=(^ found) =(%commit kind.u.found))  [count %.y]
  $(current (commit-parent data.u.found), count +(count))
::
++  ref-count-prefix
  |=  [refs=(map @t oid:git) prefix=@t]
  ^-  @ud
  =/  matching=(list [@t oid:git])
    %+  skim  ~(tap by refs)
    |=  entry=[@t oid:git]
    =/  ref=@t  -.entry
    (starts-with:git-protocol [(met 3 ref) ref] prefix)
  (lent matching)
::
::
++  native-issue-at
  |=  [repo=repository:git number=@ud]
  ^-  (unit native-issue:git)
  =/  matches=(list native-issue:git)
    (skim native-issues.repo |=(issue=native-issue:git =(number.issue number)))
  ?~  matches  ~
  `i.matches
::
++  native-pull-at
  |=  [repo=repository:git number=@ud]
  ^-  (unit native-pull:git)
  =/  matches=(list native-pull:git)
    (skim native-pulls.repo |=(pull=native-pull:git =(number.pull number)))
  ?~  matches  ~
  `i.matches
::
++  merge-objects
  |=  [current=(map oid:git object:git) staged=(map oid:git object:git)]
  ^-  (map oid:git object:git)
  =/  entries=(list [oid:git object:git])  ~(tap by staged)
  |-
  ?~  entries  current
  $(entries t.entries, current (~(put by current) -.i.entries +.i.entries))
::
++  apply-receive
  |=  [repo=repository:git commands=(list receive-command:git) staged=(map oid:git object:git)]
  ^-  (unit repository:git)
  =/  combined=(map oid:git object:git)  (merge-objects objects.repo staged)
  =/  working=(map @t oid:git)  refs.repo
  =/  seen=(set @t)  ~
  =/  remaining=(list receive-command:git)  commands
  |-
  ?~  remaining  `repo(objects combined, refs working)
  =/  command=receive-command:git  i.remaining
  ?:  (~(has in seen) ref.command)  ~
  ?.  =(old.command (~(get by refs.repo) ref.command))  ~
  =.  seen  (~(put in seen) ref.command)
  ?~  new.command
    =.  working  (~(del by working) ref.command)
    $(remaining t.remaining)
  ?.  (~(has by combined) u.new.command)  ~
  =.  working  (~(put by working) ref.command u.new.command)
  $(remaining t.remaining)
::
++  receive-policy-error
  |=  [repo=repository:git commands=(list receive-command:git) staged=(map oid:git object:git)]
  ^-  (unit @t)
  =/  combined=(map oid:git object:git)  (merge-objects objects.repo staged)
  =/  remaining=(list receive-command:git)  commands
  |-
  ?~  remaining  ~
  =/  command=receive-command:git  i.remaining
  =/  release-tag=?
    ?.  (starts-with 'refs/tags/' ref.command)  %.n
    (~(has by releases.repo) (crip (slag 10 (trip ref.command))))
  ?:  release-tag
    `'release tags cannot be updated or deleted; delete the release first'
  ?.  (~(has in protected-refs.repo) ref.command)
    $(remaining t.remaining)
  ?~  old.command
    $(remaining t.remaining)
  ?~  new.command
    `'protected branch cannot be deleted'
  =/  reachable=(unit (set oid:git))
    (reachable:git-graph combined (silt ~[u.new.command]))
  ?.  ?&  ?=(^ reachable)
          (~(has in u.reachable) u.old.command)
      ==
    `'protected branch requires a fast-forward update'
  $(remaining t.remaining)
::
++  command-for-ref
  |=  [commands=(list receive-command:git) ref=@t]
  ^-  (unit receive-command:git)
  ?~  commands  ~
  ?:  =(ref ref.i.commands)  `i.commands
  $(commands t.commands)
::
++  receive-results
  |=  [commands=(list receive-command:git) ok=? message=@t]
  ^-  (list [ok=? ref=@t message=@t])
  (turn commands |=(command=receive-command:git [ok ref.command message]))
::
--
