# Two `receive-pack` defects that broke stock `git push`

Both defects returned HTTP 400. Both are fixed. `git push --all` works, a
push above `http.postBuffer` works, and a `protocol.version=2` push works.

Measured on a fake `~bus`, pier `/home/michael/piers/joinall-bus`, HTTP port
8093, Vere 4.6 as shipped in `workspace/urbit/bin`. The client is stock
`git` 2.55.0 on this machine. Every number below came from `git` itself, from
`curl`, from `/usr/bin/time`, from `sha256sum`, or from a recorded copy of the
bytes on the wire.

The running source was confirmed by scry after every install, never by reading
the mount:

```
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/lib/git-protocol/hoon)
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/app/urgit/hoon)
```

| state | `git-protocol.hoon` | `app/urgit.hoon` | token |
|---|---:|---:|---|
| before both fixes | 13,521 | 364,122 | `?:  (gte cursor p.payload)  ~` present at 5,470 |
| fix 1 only | 13,972 | 364,122 | `=/  terminated=?` at 5,470, `++  receive-probe` absent |
| both fixes | 14,515 | 364,595 | `++  receive-probe` at 6,198, probe call at 302,490 |

---

## 1. The repository under test

A `--no-local` clone of `~/workspace/urbit/urgit`, at `/tmp/pd-big`, with its
`origin` remote removed so `--all` covers exactly the local refs.

- 5 branches: `main`, `feat/progress-and-joinall`,
  `feat/streamed-object-batches`, `fix/markdown-rendering`,
  `phase2b-encode-pack`
- 2 tags: `v-pushtest` (annotated), `ab-base-phase1` (lightweight)
- 1,373 objects, 10.95 MiB packed
- `git rev-list --objects --all | sort | sha256sum` =
  `21db089ddb78604028a144d4cc89a2b1e756009b1729cfbde6f102f844d170b8`

A second repository at `/tmp/pd-tiny` holds three commits on `main`, `a` and
`b`, about 152 KB. It is small enough to stay under `http.postBuffer`, which
isolates defect 1 from defect 2.

---

## 2. Defect 1 — a push of more than one ref returned 400

### Root cause

`parse-receive-command` (`desk/lib/git-protocol.hoon:166`) read the ref from
byte 82 and accepted the line only at a NUL or a line feed:

```hoon
=/  cursor=@ud  82
|-
?:  (gte cursor p.payload)  ~
```

Git writes the capability list after a NUL on the **first** command line only.
Later lines carry `<old> SP <new> SP <ref>` and stop. Those lines ran off the
end, `parse-receive-command` returned `~`, `parse-receive-request` failed the
whole request, and `handle-receive-pack` answered 400.

This is visible in the recorded request body of a real push. The bytes below
are the command section of a `git push` of 5 branches and 2 tags, read off the
wire by a recording TCP proxy between `git` and Eyre:

```
0000000000000000000000000000000000000000 cd5d42ee... refs/heads/feat/progress-and-joinall\x00 report-status agent=git/2.55.0-Linux
0000000000000000000000000000000000000000 5d72cf19... refs/heads/feat/streamed-object-batches
0000000000000000000000000000000000000000 8d87b298... refs/heads/fix/markdown-rendering
0000000000000000000000000000000000000000 4e0fa4bb... refs/heads/main
0000000000000000000000000000000000000000 293057d4... refs/heads/phase2b-encode-pack
0000000000000000000000000000000000000000 f9bc8574... refs/tags/ab-base-phase1
0000000000000000000000000000000000000000 256ffcbb... refs/tags/v-pushtest
FLUSH
```

Line 1 ends with `\x00 report-status agent=git/2.55.0-Linux`. Lines 2 to 7 have
no terminator at all.

### Reproduction, before

```
$ git -c protocol.version=0 -c http.postBuffer=524288000 \
      push ship '+refs/heads/a:refs/heads/a' '+refs/heads/b:refs/heads/b'
<= Recv header: HTTP/1.1 400 missing
error: RPC failed; HTTP 400 curl 22 The requested URL returned error: 400
fatal: the remote end hung up unexpectedly
EXIT=1
```

The same two refs pushed one at a time both returned exit 0, and `ls-remote`
then showed both. So the defect was in the command list, not in either ref.

### The fix

`7c86699`. End-of-payload joins NUL and line feed in the terminator set, with
the same rule for every line. `valid-ref` still screens the ref name, so the
change widens the terminator set and nothing else.

```hoon
=/  cursor=@ud  82
|-
=/  terminated=?
  ?:  (gte cursor p.payload)  %.y
  =/  byte=@ud  (byte-at:git-codec payload cursor)
  ?|  =(byte 0)
      =(byte 10)
  ==
?.  terminated
  $(cursor +(cursor))
=/  length=@ud  (sub cursor 82)
?:  =(length 0)  ~
```

`byte-at` asserts `(lth offset p.bytes)`. The `?:` in front of it short-circuits,
so it is never called past the end.

### Reproduction, after

Fix 1 installed alone, fix 2 not yet written into the file. Confirmed by scry:
`git-protocol.hoon` 13,972 bytes, `++  receive-probe` absent.

```
$ git -c protocol.version=0 -c http.postBuffer=524288000 \
      push ship '+refs/heads/a:refs/heads/a' '+refs/heads/b:refs/heads/b'
 * [new branch]      a -> a
 * [new branch]      b -> b
EXIT=0
```

At that same install, the large push still returned 400. Fix 1 fixes defect 1
and leaves defect 2 exactly where it was.

---

## 3. Defect 2 — a push above `http.postBuffer` returned 400

### Root cause

When the body is larger than `http.postBuffer`, git does not send the pack
straight away. `remote-curl.c` calls `probe_rpc` first, which POSTs a body of
one flush packet, four bytes, `0000`. Git reads the status code, discards the
response body, and sends the real request only if the probe returned 200.

`parse-receive-request` (`desk/lib/git-protocol.hoon:215`) returns `~` for a
flush with no commands ahead of it, so `handle-receive-pack` answered 400 and
git stopped before it sent anything. `http.postBuffer` defaults to 1 MB, so
every push above about 1 MB failed.

### Reproduction, before

```
$ git -c protocol.version=0 push ship '+refs/heads/main:refs/heads/main'
=> Send header: POST /git/pd2/git-receive-pack HTTP/1.1
=> Send header: Content-Length: 4
== Info: upload completely sent off: 4 bytes
<= Recv header: HTTP/1.1 400 missing
error: RPC failed; HTTP 400 curl 22 The requested URL returned error: 400
EXIT=1
```

The 4-byte body is the probe. The pack was never offered.

### The fix

`f70e6a8`. A new arm reports a body that holds one flush packet and nothing
after it:

```hoon
++  receive-probe
  |=  body=octs
  ^-  ?
  =/  next=(unit [pkt=packet:git-codec rest=octs])
    (de-pkt:git-codec body)
  ?~  next  %.n
  ?.  ?=(%flush -.pkt.u.next)  %.n
  =(0 p.rest.u.next)
```

`handle-receive-pack` (`desk/app/urgit.hoon:8003`) answers the probe right
after the authorization check and before `parse-receive-request`, so no policy
and no ref update runs over an empty command list. `parse-receive-request`
keeps its old behavior.

The answer is 200, `content-type: application/x-git-receive-pack-result`,
`cache-control: no-store`, and a zero-length body.

### Why that answer, checked against stock git rather than guessed

`git-http-backend` runs `git receive-pack --stateless-rpc`. `read_head_info`
reads the flush, builds no commands, and `cmd_receive_pack` returns without
writing a report, so a real Git server answers the probe with 200 and an empty
body. `probe_rpc` in `remote-curl.c` reads the body into a `strbuf` and
releases it, and acts only on the status code. 200 with an empty body is both
what a Git server sends and what the client needs.

The client is the test the brief asked for, and it proceeds:

```
=> Send header: POST /git/pd11/git-receive-pack HTTP/1.1
=> Send header: Content-Length: 4
<= Recv header: HTTP/1.1 200 ok
=> Send header: POST /git/pd11/git-receive-pack HTTP/1.1
=> Send header: Transfer-Encoding: chunked
== Info: upload completely sent off: 11259149 bytes
<= Recv header: HTTP/1.1 200 ok
```

### Reproduction, after

```
$ git -c protocol.version=0 push ship '+refs/heads/main:refs/heads/main'
 * [new branch]      main -> main
wall 1.26 s
EXIT=0
```

11,259,149 bytes, default `http.postBuffer`, no workaround.

### The probe did not widen what is accepted

Direct `curl` against `handle-receive-pack`, both fixes installed:

| body | status | note |
|---|---:|---|
| `0000` | **200** | response body is 0 bytes |
| `0000GARBAGE` | 400 | a flush with trailing bytes is not a probe |
| `not a pkt line at all` | 400 | |
| empty body | 400 | |
| one command line for `refs/heads/../evil`, end-terminated | 400 | `valid-ref` still rejects it |
| `0000` with no credentials | 401 | git answers 401 by filling credentials and probing again |

---

## 4. D3 evidence

All of it at `HEAD` = `f70e6a8`, running source confirmed by scry at 14,515 and
364,595 bytes.

### 4a. Every branch and tag in one command, default `http.postBuffer`

```
$ git push http://…/git/final 'refs/heads/*:refs/heads/*' 'refs/tags/*:refs/tags/*'
 * [new branch]      feat/progress-and-joinall -> feat/progress-and-joinall
 * [new branch]      feat/streamed-object-batches -> feat/streamed-object-batches
 * [new branch]      fix/markdown-rendering -> fix/markdown-rendering
 * [new branch]      main -> main
 * [new branch]      phase2b-encode-pack -> phase2b-encode-pack
 * [new tag]         ab-base-phase1 -> ab-base-phase1
 * [new tag]         v-pushtest -> v-pushtest
wall 1.37 s
EXIT=0
```

5 branches and 2 tags, one of them annotated, in one request. No `-c` flag of
any kind: default `http.postBuffer`, git's default protocol. Both defects are
exercised by this one command.

The same command, traced, against a second empty repository:

```
=> Send header: POST /git/finalt/git-receive-pack HTTP/1.1
=> Send header: Content-Length: 4
<= Recv header: HTTP/1.1 200 ok
=> Send header: POST /git/finalt/git-receive-pack HTTP/1.1
=> Send header: Transfer-Encoding: chunked
== Info: upload completely sent off: 11451409 bytes
<= Recv header: HTTP/1.1 200 ok
wall 1.43 s
```

The 4-byte probe and the 11,451,409-byte body are both answered 200. No
`Git-Protocol` header appears anywhere in that trace, which is the same
observation as §4d: a push is v0 on the wire even at git's default setting.

`git push --all` and `git push --tags` cannot be combined on one command line
("options '--tags' and '--all/--branches' cannot be used together"), which is
why the refspec form is used above. Both literal forms also pass on their own:

```
$ git push --all allship        5 branches, wall 1.34 s, EXIT=0
$ git push --tags allship       2 tags,     wall 0.03 s, EXIT=0
```

### 4b. Clone back with stock git

| | object-set SHA-256 | objects | `git fsck --full` |
|---|---|---:|---|
| source `/tmp/pd-big` | `21db089d…4d170b8` | 1,373 | exit 0 |
| clone, protocol v0 | **identical** | 1,373 | exit 0, no output |
| clone, protocol v2 | **identical** | 1,373 | exit 0, no output |

The hash is `git rev-list --objects --all | sort | sha256sum` over a
`--mirror` clone, so it covers `refs/heads/*` and `refs/tags/*` on both sides
and nothing else. Clone wall times were 0.79 s and 0.81 s.

The annotated tag round-trips as a tag object, with its tagger line intact, and
`ls-remote` advertises it peeled:

```
256ffcbb21512b1080ca6959f919503e4ca2e0cc  refs/tags/v-pushtest
4e0fa4bb59188892828a85fad6acc543aa12a695  refs/tags/v-pushtest^{}
```

### 4c. Conformance vectors

All 18 generators in `desk/gen` were built out of the running desk with
`.^(vase %ca …)` and slammed in one thread. Every one built and ran. The five
that assert with `?>` and return `[%noun %.y]` would have crashed the thread
instead of changing a `mug`.

| generator | `mug` | | generator | `mug` |
|---|---|---|---|---|
| `git-access-vector` | `0x58f6.45c3` | | `git-ofs-delta-pack-vector` | `0x0f9d.cb95` |
| `git-archive-vector` | `0x58f6.45c3` | | **`git-pack-decode-vector`** | `0x16f7.8288` |
| `git-blame-vector` | `0x5b53.27e0` | | **`git-pack-vector`** | `0x05a2.80f3` |
| `git-clay-vector` | `0x523f.8fda` | | `git-shallow-vector` | `0x664d.5ac4` |
| **`git-codec-vector`** | `0x5f23.4243` | | **`git-stock-pack-vector`** | `0x74cf.c008` |
| `git-delta-pack-vector` | `0x4402.c735` | | `git-storage-vector` | `0x1356.dfd3` |
| `git-github-vector` | `0x58f6.45c3` | | `git-tree-vector` | `0x3385.41cb` |
| `git-inflate-vector` | `0x16f7.8288` | | `git-webhook-vector` | `0x58f6.45c3` |
| `git-migration-vector` | `0x58f6.45c3` | | `git-zlib-vector` | `0x16f7.8288` |

The four the brief names are in bold. Their products were read out, not just
their `mug`s:

- `git-codec-vector` returns the blob OID
  `3b18e512dba79e4c8300dd08aeb37f8e728b8dad`, the `git hash-object` reference
  value.
- `git-pack-vector` returns `[p.pack q.pack]` for a real single-blob pack.
- `git-pack-decode-vector` returns `[%noun %.y %.y %.y]`: decoded,
  object present, object exact.
- `git-stock-pack-vector` returns `[%stock-pack %.y %.y 651 665 %.y %.y 25 692
  %.y %.y 46 %.y]`.

The value set matches the 18 recorded in `JOIN-ALL-PERFORMANCE.md` §4b exactly.
Three rows of that table carry the wrong label, though, and the table here
corrects them. `JOIN-ALL-PERFORMANCE.md` lists `git-pack-decode-vector` as
`0x05a280f3`, `git-pack-vector` as `0x664d5ac4` and `git-shallow-vector` as
`0x16f78288`. The three values are the right ones, rotated across the three
names. `git-pack-decode-vector` returns `[%noun %.y %.y %.y]`, the same noun as
`git-inflate-vector` and `git-zlib-vector` — both of which the earlier report
also gives as `0x16f78288` — so it must carry that `mug` too.
`git-shallow-vector` returns a list of ten booleans and carries `0x664d5ac4`.
This is a labeling error in the earlier report, not a change in behavior.
Nothing regressed.

### 4d. The `protocol.version=2` 400

**Defect 2 explains it in full.** Measured four ways, all before any fix:

| protocol | repository | `http.postBuffer` | result |
|---|---|---|---|
| v2 | tiny, 1 ref | 500 MB | **200, push succeeded** |
| v2 | 11 MB, 1 ref | default | 400 |
| v0 | 11 MB, 1 ref | default | 400 |
| v0 | 11 MB, 1 ref | 500 MB | **200, push succeeded** |

The protocol version does not decide the outcome. The body size does. The
reason is that git 2.55 does not use protocol v2 for `receive-pack` at all.
Under `-c protocol.version=2` it sends **no** `Git-Protocol` header on
`GET /info/refs?service=git-receive-pack`, and the push is v0 on the wire,
byte for byte the same as `-c protocol.version=0`. What
`JOIN-ALL-PERFORMANCE.md` §6 recorded as an unexplained v2 push failure was
defect 2 firing on a body above 1 MB.

After both fixes, with default `http.postBuffer`:

```
$ git -c protocol.version=2 push http://…/git/pd12 \
      'refs/heads/*:refs/heads/*' 'refs/tags/*:refs/tags/*'
 * [new branch]      … (5 branches)
 * [new tag]         … (2 tags)
wall 1.40 s
EXIT=0
```

---

## 5. D4 — the paths that already worked

Every one was run at `HEAD`, with no `-c` flag, against a repository that
already held refs where that matters. All returned exit 0.

| case | result |
|---|---|
| single-ref push into an empty repository | `* [new branch] main -> main` |
| single-ref push onto a non-empty repository, non-zero old OID | `0b4518c..eb72ccc  main -> main` |
| small push of a second ref, in its own command | `* [new branch] a -> a` |
| ref delete, all-zero new OID | `- [deleted] a` |
| clone, protocol v0 | 1,373 objects, `fsck` clean |
| clone, protocol v2 | 1,373 objects, `fsck` clean |

The delete case matters because it runs `parse-receive-command` on a line whose
new OID is forty zeros, and the fast-forward case matters because its old OID
is a real OID. Both go through the arm that changed.

---

## 6. D5 — `git-delta.hoon` as changed in `bf3dbea`

**The bytes came back identical.**

`bf3dbea` made `apply-delta` collect its chunks and call `join-all` once
instead of folding `join` pairwise. It was verified on a pier by the earlier
run but not in this tree. The pushes above exercise it, and this is what they
show.

The pack `git` sent was recorded off the wire by a proxy between `git` and
Eyre, so what was measured is exactly what urgit decoded. Indexed with
`git index-pack` and read with `git verify-pack -v`:

```
pack bytes             11,449,156   (SHA-256 87242d09…59de3647)
objects                     1,373
  non-delta                   736
  REF_DELTA                   637
delta chain length = 1        287
delta chain length = 2        298
delta chain length = 3         39
delta chain length = 4          9
delta chain length = 5          3
delta chain length = 6          1
trailing bytes after the last object: 20, the pack SHA-1
```

So `apply-delta` ran on **637 objects**, on chains up to **depth 6**.

The deltas are `REF_DELTA`, not `OFS_DELTA`, because urgit does not advertise
`ofs-delta` in its `receive-pack` capabilities. That means the `%ref` branch of
`resolve-pass` (`git-pack-decode.hoon:154`) carried all 637. `QUESTIONS.md` §9
recorded that branch as never having been exercised on a real pack. It is now.

Three independent checks on the result:

1. **Whole object set.** `git rev-list --objects --all | sort | sha256sum` over
   a stock `--mirror` clone of the pushed repository is
   `21db089ddb78604028a144d4cc89a2b1e756009b1729cfbde6f102f844d170b8`, the same
   as the source. An OID is a SHA-1 over the canonical object bytes, so 1,373
   matching OIDs is 1,373 objects that did not change by one byte.

2. **The deltified objects on their own.** The 637 OIDs that arrived as
   `REF_DELTA`, taken from the recorded pack, were streamed out of the source
   and out of three separate round trips with `git cat-file --batch`:

   ```
   source                                  99f0e57531ec9016…e74071e6
   clone of the recorded push              99f0e57531ec9016…e74071e6
   clone of the D3 push, protocol v0       99f0e57531ec9016…e74071e6
   clone of the D3 push, protocol v2       99f0e57531ec9016…e74071e6
   ```

   Full value:
   `99f0e57531ec901633c40574b75d137e59526072a48bf02bb0b7d793e74071e6`. That
   stream carries the type, the size and the full body of each object, so it
   compares content directly and not just names.

3. **The deepest chain.** The one object at depth 6,
   `17728c4536c8bc888cd6cbaea2120f1859f35e95`, is a 336,494-byte blob. Source
   and round trip both give size 336,494, type blob, and content SHA-256
   `e00ac16c139bc3ea129b971023e61cd4e033e6f86f19d4f95ffc98304990127d`.

`git fsck --full` on the clone exits 0 with no output, which recomputes every
OID a fourth time.

`git-delta-pack-vector`, the in-ship `REF_DELTA` vector, also passes: it
rebuilds an object from a ref-delta and recomputes its OID as
`9c411def56bb81319d53a36ced4e2e5d97d1106c`, matching the expected value.

`desk/lib/git-delta.hoon` was not touched. It is 3,640 bytes on the pier, its
`bf3dbea` comment sits at offset 1,824, and the old `join:git-codec output`
text is absent — all read by scry, not from the mount.

---

## 7. What I could not measure and why

**A push that needs more than one probe round.** `probe_rpc` loops while the
answer is `HTTP_REAUTH`. The 401-then-probe-again path was driven by hand with
`curl` and returned 401 as expected, but no `git` client was made to walk that
loop with a real credential helper. Every `git` push here authenticated in the
URL.

**A push large enough to need chunked bodies across several requests.** The
largest body sent was 11,451,409 bytes, in one chunked POST. Whether Eyre or
`handle-receive-pack` has a ceiling above that was not probed. The GitHub paths
declare 64 MiB and 25,000 objects; the local `receive-pack` path declares no
limit that this work found, and none was tested.

**`OFS_DELTA` on the push side.** urgit does not advertise `ofs-delta` for
`receive-pack`, so `git` sent `REF_DELTA` for all 637 deltas and the `%ofs`
branch of `resolve-pass` never ran in these tests. It is covered by
`git-ofs-delta-pack-vector`, which passes, but not by a real push. Adding
`ofs-delta` to the advertisement would change the wire contract, which this
brief does not authorize, so it was not tried.

**`git-github.hoon`'s outbound `receive-request`.** Its command line at
`git-github.hoon:325` always writes a NUL and a capability list, and it sends
one ref per request, so defect 1 never reached it. Reaching that arm needs a
GitHub remote and a token. No bytes from it were compared.

**The native ship-to-ship `%archive` and `%rate` paths.** Off limits by the
brief, and owned by a parallel task. They do not route through
`handle-receive-pack`.

**Timing.** The wall times quoted here — 1.34 to 1.54 s for an 11 MB push,
0.79 to 0.86 s for a clone — are single `/usr/bin/time` readings on `git`, not
medians over repeated runs, and no `~> %bout` hint was placed anywhere for this
work. They say the operations complete quickly. They are not a benchmark and
should not be compared against the numbers in `JOIN-ALL-PERFORMANCE.md`, which
were taken as medians on a different port and a different desk state.

**Whether any other caller of `parse-receive-command` depended on the old
behavior.** `grep` finds one caller, `parse-receive-request`. A ref that used
to fail to parse now parses when it ends at the end of the payload, and
`valid-ref` is unchanged, so the accepted ref set is the same set that a NUL or
a line feed would already have accepted. That is an argument from reading, plus
the negative tests in §3. It is not a proof over all inputs.
