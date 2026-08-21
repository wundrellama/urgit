# `git clone` of a many-ref repository returned 400 because the request was gzipped

Fixed. A stock `git clone` of a 200-ref repository and of a 60-ref
repository now succeeds on protocol v0 and on protocol v2, with no `-c`
flag of any kind, and the cloned object set is byte-identical to the
source.

Measured on a fake `~bus`, pier `/home/michael/piers/joinall-bus`, HTTP
port 8093, `--loom 32`, Vere 4.6 as shipped in `workspace/urbit/bin`. The
client is stock **`git` 2.55.0** on this machine. Every number below came
from `git` itself, from `/usr/bin/time`, from `sha256sum`, from a
recording proxy between `git` and Eyre on port 8110, or from a probe run
inside the ship.

The running source was confirmed by scry after every install, never by
reading the mount:

```
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/app/urgit/hoon)
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/lib/git-gzip/hoon)
```

| state | `app/urgit.hoon` | `lib/git-gzip.hoon` | `gen/git-gzip-vector.hoon` | `lib/git-protocol.hoon` | token |
|---|---:|---:|---:|---:|---|
| before (`b53e06f`) | 364,595 | absent | absent | 14,515 | `++  receive-probe` at 6,198 |
| after | 366,492 | 3,648 | 3,464 | 14,515 | `++  decoded-body` at 288,612, `input-limit  262.144` at 1,219 |

`lib/git-protocol.hoon`, `lib/git-codec.hoon`, `lib/git-delta.hoon` and
`lib/git-pack-decode.hoon` were not touched. `git-delta.hoon` still reads
5,897 bytes by scry, its `b53e06f` size.

---

## 1. Root cause, and the measured trigger

### 1a. Root cause

`handle-upload-pack` read `u.body.request.req` — the raw wire body — four
times: `v2-command:git-protocol`, `v2-ls-refs`, `v2-object-info-oids`, and
`parse-upload-request`. Nothing in `desk/` read the `content-encoding`
header. When the body arrived gzipped, `de-pkts:git-codec` read the gzip
magic bytes `1f 8b` as a pkt-line length, `hex4-value` rejected `\x1f`
as a hex digit, `parse-upload-request` returned `~`, and
`handle-upload-pack` answered **400 `invalid upload-pack request`**.

Reproduced at `b53e06f`, 200-ref repository, no `-c` flag:

```
$ git clone http://127.0.0.1:8110/git/gz1 /tmp/gz-clone1
Cloning into '/tmp/gz-clone1'...
error: RPC failed; HTTP 400 curl 22 The requested URL returned error: 400
fatal: expected 'packfile'
EXIT=128
```

The proxy log for that clone:

```
POST /git/gz1/git-upload-pack   git-protocol: version=2  body 170   ce: none  -> 200
POST /git/gz1/git-upload-pack   git-protocol: version=2  body 5218  ce: gzip  -> 400
```

The 5,218-byte body decompresses to **10,223 bytes**: a protocol v2
`command=fetch` with **202 `want` lines**, gzip FLG 0, ISIZE trailer
10,223, CRC32 `0x8e788a9a`. That matches the shape
`RESOLVE-ENTRIES-PERFORMANCE.md` §6 recorded (1,438 on the wire, 3,023
after `gzip.decompress`, 58 `want` lines) on a different repository.

### 1b. The trigger is a byte threshold on the request body, and it is 1,024

**git compresses the request body when it is larger than 1,024 bytes.
A body of exactly 1,024 bytes is sent uncompressed; 1,025 bytes is
gzipped.**

Measured, not read out of git's source. The knob is `GIT_USER_AGENT`,
which git writes verbatim into the `agent=` pkt-line inside the request
body, so one character of agent string is exactly one byte of body. Any
future reader can re-derive the threshold with it.

Protocol v2, three fully qualified refspecs, agent padded one byte at a
time:

| agent length | body bytes | `content-encoding` |
|---:|---:|---|
| 220 | 1,020 | none |
| 222 | 1,022 | none |
| 223 | 1,023 | none |
| **224** | **1,024** | **none** |
| **225** | **1,025** | **gzip** (199 on the wire) |
| 226 | 1,026 | gzip (200 on the wire) |

Protocol v0, `git clone` of an 18-ref repository, same knob:

| agent length | body bytes | `content-encoding` |
|---:|---:|---|
| 103 | 1,023 | none |
| **104** | **1,024** | **none** |
| **105** | **1,025** | **gzip** (498 on the wire) |
| 106 | 1,026 | gzip (497 on the wire) |

### 1c. It does not depend on `protocol.version`

The boundary is 1,024/1,025 on both, as the two tables above show. Both
versions fail the same way before the fix, and for the same reason:

| protocol | body | `content-encoding` | before |
|---|---:|---|---|
| v0, default | 5,142 | gzip | 400 |
| v2, default | 5,218 | gzip | 400 |

Protocol v2 splits the exchange into an `ls-refs` POST and a `fetch`
POST, and **both** cross the threshold on their own once the refspec list
is long enough. A `git fetch` of four fully qualified refspecs gzips its
`ls-refs` body at 1,047 bytes and never reaches `fetch` at all. This is
why `v2-command:git-protocol` had to see decompressed bytes too — see §4.

### 1d. It does not depend on `http.postBuffer`, except to switch it off

`http.postBuffer` at 500 MB changes nothing: 5,142 bytes on v0 and 5,218
on v2, both still gzip, both still 400. The push-side probe behaviour
that `PUSH-DEFECTS.md` §3 found does not apply here at default settings.

`http.postBuffer` *below* the body size does change it. git then takes
the large-request path: it POSTs a 4-byte flush probe first and streams
the real body chunked and **uncompressed**. Measured on v0 with
`http.postBuffer=1024` against the 200-ref repository:

```
POST /git/gz1/git-upload-pack   body 4  ce: none  -> 400
error: RPC failed; HTTP 400
```

Two things follow. First, gzip and the large-request path are mutually
exclusive, so the decompressed size of any body git gzips is bounded
above by `http.postBuffer` — 1 MiB at git's default. Second, urgit has a
second, separate hole on that path: `handle-upload-pack` has no
equivalent of `receive-probe`, so it answers the 4-byte probe 400. That
one is **not fixed here** — see §6.

On protocol v2, an `http.postBuffer` below `LARGE_PACKET_MAX` is not a
usable configuration at all. git 2.55.0 aborts:

```
BUG: remote-curl.c:1533: The entire rpc->buf should be larger than LARGE_PACKET_MAX
error: git-remote-http died of signal 6
```

### 1e. The repositories under test

- `/tmp/gz-src` → urgit repository `gz1`: 200 branches on a linear chain
  of 200 commits, 600 objects, `git rev-list --objects --all | sort |
  sha256sum` = `4e169f6389a27cc42955bbb2f2158e8977b152133bafee9b0b66d10769bc6f36`.
- `/tmp/gz-real` → urgit repository `gzreal`: a `--no-local` clone of
  `~/workspace/urbit/urgit` with its `origin` removed and 58 tags added
  at distinct commits. 60 refs, 1,204 objects, hash
  `8cf65babf73a1de8763df7f5995efd17892afaea3a3d6677e355063639f77a0f`.
  This is the "58+ refs with real content" case: real trees, real
  deltas, real history.
- urgit repository `gz2`: the first 18 branches of `gz1`, 54 objects.
  Small enough that its clone request stays at 936 bytes on v0 and 1,023
  bytes on v2 — under the threshold, so it exercises the unchanged path.

Pushing all 200 refs into `gz1` took one POST of 61,764 bytes and exit 0.
The `receive-pack` fixes on this branch carry that with no `-c` flag.

---

## 2. Which route, and what the rejected one turned out to be

**Route 1: strip the gzip wrapper and use `inflate-deflate:git-inflate`.**
The new `desk/lib/git-gzip.hoon` walks the RFC 1952 header, takes the
trailer's ISIZE as the size limit `inflate-deflate` needs, inflates, and
requires the output to come out at exactly ISIZE.

### Route 2 is circular. Measured, not argued.

The question was whether Vere's `%zlib-v0` jet validates the Adler-32
over the decompressed output. It does.

Three zlib streams were built from the same 5,200-byte DEFLATE payload —
the real gzip body of the failing clone, header and trailer removed — and
handed to `decompress-zlib:git-zlib` under `mule` inside the ship. They
differ only in their last four bytes:

| stream | bytes | trailer | result |
|---|---:|---|---|
| `78 01` + DEFLATE + correct Adler-32 | 5,206 | `9773f161` | **`[~ [10.223 5.206]]`** — decompressed |
| `78 01` + DEFLATE + zeroed Adler-32 | 5,206 | `00000000` | **crash**, caught by `mule` |
| `78 01` + DEFLATE, no trailer | 5,202 | absent | **crash**, caught by `mule` |

The jet also printed its own diagnosis to the pier log, which is the
clearest possible confirmation of *why* it failed:

```
-3
incorrect data check
-5
(null)
```

`-3` is zlib's `Z_DATA_ERROR` and `incorrect data check` is zlib's
message for an Adler-32 mismatch. `-5` is `Z_BUF_ERROR`, the truncated
case.

So route 2 needs the Adler-32 of the output before it has the output.
There is no way to supply it and no way to compute it, and synthesising a
zlib wrapper around gzip's DEFLATE stream cannot work. Route 1 was taken,
which is also what the brief preferred.

### What route 1 has to handle

A gzip member is a ten-byte header, then optional FEXTRA, FNAME,
FCOMMENT and FHCRC sections selected by the FLG byte, then raw DEFLATE,
then CRC32 and ISIZE. `git` 2.55.0 sets FLG 0 and writes none of the
optional sections, so a decoder that ignored them would pass every test
against `git` and fail against a client that writes a FNAME.
`++deflate-offset` walks all four, and §3 tests each one on its own and
all four together.

### What is not checked, and why

**CRC32 is not verified.** There is no CRC32 in `hoon.hoon` and none in
this desk, and writing a table-driven one in Hoon would add a per-byte
loop on top of an inflater that is already the expensive part. Three
checks stand in for it: the DEFLATE stream must decode, the output length
must equal the declared ISIZE exactly, and the result must then parse as
pkt-lines. §3 shows a body with a deliberately wrong CRC32 being
accepted, so the gap is measured rather than assumed. A corrupted body
that survives all three checks and still parses as a valid `want` list is
not a failure mode this decoder can distinguish from a client that meant
it.

---

## 3. The input bound: 262,144 bytes

`++input-limit` in `desk/lib/git-gzip.hoon` is **262,144 (256 KiB)**.
Neither the gzip member nor its declared ISIZE may exceed it.

The bound exists because `inflate-deflate` is the slow portable path:
`append-byte` rebuilds the whole output through a two-element `can` for
every byte, and `copy-distance` loops it. The cost was measured directly,
by calling `inflate-deflate` inside the ship on synthetic upload-pack
bodies of a known size, click round trip minus a 0.18 s no-op baseline:

| output bytes | inflate seconds | µs per output byte |
|---:|---:|---:|
| 1,126 | 0.05 | 44 |
| 5,126 | 0.10 | 20 |
| 10,126 | 0.16 | 16 |
| 20,126 | 0.26 | 13 |
| 50,126 | 0.59 | 12 |
| 100,126 | 1.18 | 12 |
| 250,126 | 3.28 | 13 |
| 500,126 | 11.66 | 23 |
| 1,000,126 | 22.94 | 23 |

Flat at about 12 µs per byte to a quarter megabyte, then the quadratic
term in `append-byte` starts to show and the rate doubles.

End to end through Eyre and the agent, gzipped bodies of real `want`
lines:

| decompressed body | wire bytes | seconds |
|---:|---:|---:|
| 1,030 | 547 | 0.03 |
| 50,030 | 25,218 | 0.74 |
| 150,030 | 74,789 | 2.37 |
| **262,130** | 130,296 | **4.42** |
| **262,145** | 289 | **0.03** — refused |

**Why 256 KiB.**

1. It carries about 5,200 `want` or `have` pkt-lines at 50 bytes each. The
   largest real request measured in this work is 10,223 bytes, 25 times
   under the bound. A repository would need more than five thousand refs
   to reach it.
2. A request at the bound costs 4.42 s of ship time. That is the same
   order as the 1.70–2.35 s a real pack decode already costs on the push
   side (`RESOLVE-ENTRIES-PERFORMANCE.md` §5), so the bound does not put a
   new worst case into the agent — it roughly doubles the existing one.
   A megabyte would have cost 23 s.
3. It only ever refuses requests between 256 KiB and 1 MiB. Below 1,025
   bytes git does not compress; above `http.postBuffer` (1 MiB by
   default, §1d) git does not compress either. The window where the bound
   can bite is 5,200 to 21,000 pkt-lines wide.
4. The check is on the *declared* ISIZE and it runs before any inflating,
   so a body claiming 4,294,967,295 bytes of output is refused in 32 ms
   without a byte being inflated. A body that lies the other way — claims
   a small ISIZE and expands past it — is stopped by `inflate-deflate`'s
   own `limit`, which is set to that same ISIZE, and then fails the
   exact-size check.

The member size is bounded by the same number. A legitimate gzip of at
most 256 KiB never exceeds 256 KiB compressed, so that guard never fires
on a real request; it exists so the work is bounded by the measured curve
above from both directions.

---

## 4. D3, fail closed — and D4, the other body paths

### 4a. The fail-closed matrix

All 32 cases driven straight at Eyre on port 8093 with raw HTTP, at the
final installed state. **32 of 32 pass.**

| case | status | body |
|---|---:|---|
| no `content-encoding`, v0 body | 200 | pack |
| no `content-encoding`, v2 body | 200 | pack |
| `content-encoding: identity` | 200 | pack |
| `content-encoding: gzip`, valid v0 | 200 | pack |
| `content-encoding: gzip`, valid v2 | 200 | pack |
| `content-encoding: x-gzip`, valid v0 | 200 | pack |
| gzip with FNAME | 200 | pack |
| gzip with FCOMMENT | 200 | pack |
| gzip with FEXTRA | 200 | pack |
| gzip with FHCRC | 200 | pack |
| gzip with FEXTRA + FNAME + FCOMMENT + FHCRC together | 200 | pack |
| `content-encoding: deflate` | **415** | `unsupported content-encoding` |
| `content-encoding: br` | **415** | `unsupported content-encoding` |
| `content-encoding: zstd` | **415** | `unsupported content-encoding` |
| `content-encoding: GZIP` (upper case) | **415** | `unsupported content-encoding` |
| claims gzip, body is plain pkt-lines | **400** | `invalid gzip request body` |
| claims gzip, body is 3 bytes | **400** | `invalid gzip request body` |
| gzip truncated to the header | **400** | `invalid gzip request body` |
| gzip truncated mid-DEFLATE, trailer reattached | **400** | `invalid gzip request body` |
| gzip with the 8-byte trailer removed | **400** | `invalid gzip request body` |
| gzip with one trailer byte removed | **400** | `invalid gzip request body` |
| gzip with ISIZE one too large | **400** | `invalid gzip request body` |
| gzip with ISIZE 4,294,967,295 | **400** | `invalid gzip request body` |
| gzip with ISIZE 262,145 | **400** | `invalid gzip request body` |
| gzip with a wrong CRC32 | 200 | pack — CRC32 is not checked, §2 |
| bad magic byte 0 | **400** | `invalid gzip request body` |
| bad magic byte 1 | **400** | `invalid gzip request body` |
| CM 9 instead of 8 | **400** | `invalid gzip request body` |
| reserved FLG bit 5 set | **400** | `invalid gzip request body` |
| FNAME bit set with no NUL anywhere | **400** | `invalid gzip request body` |
| valid gzip, declared output 300,000 | **400** | `invalid gzip request body` |
| valid gzip, 200,000 bytes that are not pkt-lines | **400** | `invalid upload-pack request` |

The last row is the one that shows the layering: the body was under the
bound, so it *was* decompressed, and then the pkt-line parser rejected it
with its own message.

**The agent did not crash on any of them.** The pier log has no `crud`,
no `bail`, and no `%hunk` across the whole run, and every clone after the
matrix still succeeds. `+gunzip` is total, but `+decoded-body` calls it
under `mule` anyway so that a body claiming gzip can never take the agent
down.

**A body with no `content-encoding` takes the same path it took before.**
`handle-upload-pack` still rejects an absent body with 400 before
`+decoded-body` is reached, and `+decoded-body` returns
`u.body.request.req` unchanged the moment the header is absent. There is
no arm between the header check and the old code. Measured as well as
read: the `gz2` clone sends 936 bytes on v0 and 1,023 on v2 with no
`content-encoding` and both still exit 0 with a matching object set
(§5).

### 4b. `v2-command` and `v2-object-info-oids` — the half fix that would have passed a naive test

`v2-command:git-protocol` runs on the body **before**
`parse-upload-request` is reached, and it decides the whole dispatch:
`%ls-refs`, `%object-info`, `%fetch`, or none. On a gzipped body it
returns `~`, which falls through to `parse-upload-request`, which also
returns `~`. Decompressing only inside `parse-upload-request` would have
left a gzipped `ls-refs` misrouted into the fetch path and a gzipped
`object-info` misrouted with it.

This is not hypothetical. §1c measured a `git fetch` whose **`ls-refs`**
POST alone is gzipped at 1,047 bytes while its `fetch` POST would have
been 288 bytes — the small one is the one that gets compressed, because
`ls-refs` carries six DWIM `ref-prefix` lines per refspec.

The fix decodes once, at the top of `handle-upload-pack`, and binds
`body`. All four readers — `v2-command`, `v2-ls-refs`,
`v2-object-info-oids` and `parse-upload-request` — take that binding.
`grep -n "u.body.request.req" desk/app/urgit.hoon` returns no hit inside
either Git handler.

### 4c. `handle-receive-pack` — git does not gzip it

**git 2.55.0 never gzips a `receive-pack` body.** Determined by
measurement, over six real pushes recorded on the wire:

| push | body bytes | `content-encoding` |
|---|---:|---|
| 1 ref | 325 | none |
| 2 refs | 479 | none |
| 4 refs | 901 | none |
| 8 refs | **1,750** | none |
| 30 refs | **9,151** | none |
| 200 refs | **61,764** | none |

The 1,750-byte body is the one that settles it: it is well past the
1,024-byte threshold that gzips an `upload-pack` body, on the same client,
in the same session, against the same server, and it is not compressed.
`receive-pack` carries a pack, and git streams it rather than buffering
and compressing it.

The decoding was applied to `handle-receive-pack` anyway, because the
alternative is that a gzipped push body gets misparsed into
`invalid receive-pack request`. Tested directly, since no `git` client
will produce the input:

| case | status | result |
|---|---:|---|
| plain 9,151-byte push body into a fresh repository | 200 | `unpack ok`, 30 refs |
| the same body gzipped to 5,738 bytes into a fresh repository | 200 | `unpack ok`, 30 refs |
| `content-encoding: deflate` | 415 | `unsupported content-encoding` |
| claims gzip, body is not gzip | 400 | `invalid gzip request body` |
| gzipped flush-only probe | 200 | empty body, as `receive-probe` gives |
| plain flush-only probe | 200 | empty body, unchanged |

Both pushed repositories clone back to
`f6e99ed5b82cc85eac7a4047…`, the same object set as the 30 branches in
the source, and `git fsck --full` exits 0 on both.

### 4d. The webhook handler, and the JSON APIs — deliberately not changed

`handle-incoming-hook` (`urgit.hoon:4553`) was left alone, on purpose.
Its signature check is an HMAC-SHA-256 over the **raw** body, and GitHub
computes that signature over the bytes it puts on the wire. Decompressing
before `verify:git-webhook` would verify a different string than the one
that was signed and break every webhook that works today. If a gzipped
body ever arrived, the current path already fails closed and says so:
the signature check rejects it 401, or `de:json:html` rejects it 400
`webhook body is not valid JSON`. Neither is a silent misparse. I did not
measure GitHub's actual behaviour — see §6.

The remaining body readers (`api-body`, the LFS batch, lock and verify
handlers) are JSON APIs, not Git wire endpoints, and are outside this
brief.

---

## 5. D5 — the evidence

Every row below was taken at the final installed state, confirmed by scry
at 366,492 / 3,648 / 3,464 bytes, with **no `-c` flag of any kind** on the
client.

### 5a. Clone of a many-ref repository, both protocols

| repository | refs | protocol | request | encoding | exit | wall | objects | `fsck --full` | object-set SHA-256 |
|---|---:|---|---:|---|---:|---:|---:|---|---|
| `gz1` | 201 | v0 | 5,142 | **gzip** | 0 | 0.38 s | 600 | exit 0, no output | `4e169f63…69bc6f36` |
| `gz1` | 201 | v2 | 5,218 | **gzip** | 0 | 0.41 s | 600 | exit 0, no output | `4e169f63…69bc6f36` |
| `gzreal` | 60 | v0 | 1,506 | **gzip** | 0 | 0.82 s | 1,204 | exit 0, no output | `8cf65bab…39f77a0f` |
| `gzreal` | 60 | v2 | 1,579 | **gzip** | 0 | 0.81 s | 1,204 | exit 0, no output | `8cf65bab…39f77a0f` |
| `gz2` | 18 | v0 | 936 | none | 0 | 0.05 s | 54 | exit 0, no output | `1f7bd632…` |
| `gz2` | 18 | v2 | 1,023 | none | 0 | 0.05 s | 54 | exit 0, no output | `1f7bd632…` |

Sources, `git rev-list --objects --all | sort | sha256sum`:

- `/tmp/gz-src` = `4e169f6389a27cc42955bbb2f2158e8977b152133bafee9b0b66d10769bc6f36` — **identical** to both `gz1` clones.
- `/tmp/gz-real` = `8cf65babf73a1de8763df7f5995efd17892afaea3a3d6677e355063639f77a0f` — **identical** to both `gzreal` clones.
- the 18 branches `gz2` holds = `1f7bd632baf711e4f931f7aa…` — **identical** to both `gz2` clones.

The clones are `--mirror`, so the hash covers `refs/heads/*` and
`refs/tags/*` on both sides and nothing else. `gzreal` carries 58
lightweight tags and real Git history from this repository, so it is the
"58+ refs" case with real objects behind it.

The `gz2` rows are the regression that matters most: 936 and 1,023 bytes,
no `content-encoding` header, the path that worked before, still working.
1,023 is one byte under the threshold.

### 5b. Shallow clone and unshallow, over gzipped requests

`git clone --depth 1 --no-single-branch` of `gz1`, then
`git fetch --unshallow`. The `--no-single-branch` matters: plain
`--depth 1` implies `--single-branch`, which wants one ref and stays
under the threshold. With all 200 branches wanted, every negotiation
round is gzipped.

| protocol | depth-1 exit | commits | `.git/shallow` | unshallow exit | commits after | `fsck --full` | object-set SHA-256 |
|---|---:|---:|---:|---:|---:|---|---|
| v0 | 0 | 200 | 199 | 0 | 200 | exit 0, no output | `4e169f63…` |
| v2 | 0 | 200 | 199 | 0 | 200 | exit 0, no output | `4e169f63…` |

Eight POSTs per run, of which seven are gzipped — 5,145 / 5,152 / 6,420 /
6,518 / 6,695 / 6,827 / 6,358 bytes — and every one answered 200. Plain
`--depth 1` followed by `--unshallow` also exits 0 on both protocols and
ends at 200 commits.

### 5c. Conformance vectors

All 19 generators in `desk/gen` were built out of the running desk with
`.^(vase %ca …)`, `slot`ed to their gate and slammed. Every one built and
ran; the six that assert with `?>` would have crashed the thread rather
than change a `mug`.

| generator | `mug` | | generator | `mug` |
|---|---|---|---|---|
| `git-access-vector` | `0x58f6.45c3` | | `git-ofs-delta-pack-vector` | `0x0f9d.cb95` |
| `git-archive-vector` | `0x58f6.45c3` | | **`git-pack-decode-vector`** | `0x16f7.8288` |
| `git-blame-vector` | `0x5b53.27e0` | | **`git-pack-vector`** | `0x05a2.80f3` |
| `git-clay-vector` | `0x523f.8fda` | | `git-shallow-vector` | `0x664d.5ac4` |
| **`git-codec-vector`** | `0x5f23.4243` | | **`git-stock-pack-vector`** | `0x74cf.c008` |
| `git-delta-pack-vector` | `0x4402.c735` | | `git-storage-vector` | `0x1356.dfd3` |
| `git-github-vector` | `0x58f6.45c3` | | `git-tree-vector` | `0x3385.41cb` |
| **`git-gzip-vector`** | **`0x0295.709b`** | | `git-webhook-vector` | `0x58f6.45c3` |
| `git-inflate-vector` | `0x16f7.8288` | | `git-zlib-vector` | `0x16f7.8288` |
| `git-migration-vector` | `0x58f6.45c3` | | | |

The 18 pre-existing values match `PUSH-DEFECTS.md` §4c — the corrected
mapping — on all 18 rows. `JOIN-ALL-PERFORMANCE.md` §4b has three of
those labels rotated and carries its own correction note; the mapping used
here is the one in `PUSH-DEFECTS.md`. `git-codec-vector`'s product was
read out, not just its `mug`, and still returns the blob OID
`3b18e512dba79e4c8300dd08aeb37f8e728b8dad`.

`git-gzip-vector` is new. It carries three real gzip members produced by
GNU zlib over the same pkt-line request — one with FLG 0, one carrying
FEXTRA, FNAME, FCOMMENT and FHCRC together, one with ISIZE rewritten —
and asserts sixteen properties: both good members decode to the request
byte for byte and agree with each other, and fourteen mutations of a
member that *does* decode are each rejected. Its product is
`[%noun 113 105 131 0x3d8c38d7 %.y]`: 113 payload bytes, a 105-byte
member, a 131-byte member with every optional section, and the `mug` of
the decoded request.

---

## 6. What I could not measure and why

**Whether git ever gzips a `receive-pack` body on any client or
configuration.** §4c is six measured pushes on `git` 2.55.0, from 325 to
61,764 bytes, none compressed, including one at 1,750 bytes that is
plainly past the threshold that compresses an `upload-pack` body. That is
a strong negative for this client. It is not a proof about JGit, libgit2,
or a future git. The decoding was applied to `handle-receive-pack` for
exactly that reason, and tested by hand with raw HTTP, but no real client
produced the input.

**GitHub's webhook encoding.** §4d argues from the HMAC — the signature
is over the raw body, so decompressing before verification would break
it — and from the fact that both current failure paths are clear errors
rather than misparses. No webhook from GitHub was captured. Reaching that
arm needs a public endpoint and a configured GitHub repository, neither of
which this run had.

**The upload-pack flush-only probe.** This is a real, separate defect and
it is **not fixed here.** `handle-upload-pack` has no equivalent of
`receive-probe:git-protocol`, so it answers git's 4-byte `0000` probe
with 400. Measured two ways: raw HTTP with a 4-byte body returns
`HTTP/1.1 400`, and a v0 clone with `http.postBuffer=1024` POSTs 4 bytes,
gets 400, and dies. git only sends that probe when the request exceeds
`http.postBuffer`, so at default settings it needs an `upload-pack`
request above 1 MiB — about 21,000 refs. It is the same class of defect
as `PUSH-DEFECTS.md` §3 and would take the same shape of fix, but it is
not a gzip defect, this brief did not authorize widening what
`handle-upload-pack` accepts, and no repository in this run could reach
it. Recorded, not chased.

**CRC32.** Not verified, and there is no CRC32 in this desk to verify it
with. §2 states the three checks that stand in for it and §4a shows a
body with a wrong CRC32 being accepted, so the gap is measured rather
than assumed.

**Whether `inflate-deflate` produces the same bytes as GNU zlib on inputs
this work did not generate.** Every gzip stream tested here came from GNU
zlib, either from `git` on the wire or from Python's `zlib`, and every one
that decoded produced bytes that then parsed as pkt-lines and produced a
pack whose object set matched the source by SHA-256. The DEFLATE decoder
itself was not touched and `git-inflate-vector` and `git-zlib-vector`
still hold. No fuzzing over arbitrary DEFLATE streams was done.

**A clone large enough to need more than 256 KiB of request.** The bound
in §3 was measured with synthetic `want` lists, not with a repository that
really has five thousand refs. Building one was not attempted. The
consequence of getting the bound wrong is a 400 on such a clone, not
corruption.

**The 148 MB `meme` issue.** Not hit. Every repository in this run is
under 30 MiB and the pier ran at `--loom 32` throughout, so nothing here
says anything new about the ceiling
`RESOLVE-ENTRIES-PERFORMANCE.md` §6 recorded.

**Timing.** The wall times in §5a — 0.05 s to 0.82 s per clone — are
single `/usr/bin/time` readings on the `git` client, not medians. The
inflate curve in §3 is click round trips minus one 0.18 s no-op baseline,
two readings per point at the small sizes and one at the large ones. They
establish the shape and the order of magnitude that the bound rests on.
They are not a benchmark and should not be compared against
`RESOLVE-ENTRIES-PERFORMANCE.md`, which used `~> %bout` hints on a
different desk state.

**One `git fsck` in the shallow series first exited 68** with
`packfile … index not opened`, run immediately after
`git fetch --unshallow` while git still had two packs in flight. Every
re-run of the same check exits 0 with no output, and the table in §5b is
the re-run. That was a race in the test harness, not in the server.

**Whether any other caller depends on the old body reading.** `grep`
finds no `u.body.request.req` left in either Git handler, and the JSON
and LFS handlers keep theirs untouched. The argument that the
no-`content-encoding` path is unchanged is an argument from reading
`+decoded-body` — one `?~` on the header, returning the same octs — plus
the `gz2` clones in §5a. It is not a proof over all inputs.
