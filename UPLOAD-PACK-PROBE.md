# `handle-upload-pack` answered git's flush-only probe with 400

`GZIP-REQUEST.md` §6 recorded this defect and did not fix it. This report
fixes it and proves the fix.

`git` version under test: **2.55.0**.

Ship: `~bus` at `~/piers/joinall-bus`, port 8093, PID 2519792, `--loom 32`.
The pier already held the `74ad8c0` build. A scry confirmed that before any
measurement: `lib/git-protocol.hoon` 14,515 bytes and `app/urgit.hoon`
366,492 bytes, the exact byte counts of the working tree at `f1fcb95`.

| state | `git-protocol.hoon` | `app/urgit.hoon` | token check |
|---|---:|---:|---|
| before | 14,515 | 366,492 | `flush-only-body` absent, `receive-probe` at 6,202 |
| after | 14,651 | 367,041 | `flush-only-body` at 6,336, `receive-probe` absent |

Both rows come from `.^(@t %cx …)` on the running desk, taken right after
each install.

---

## 1. Root cause

When a request body is larger than `http.postBuffer`, git does not send the
body straight away. `remote-curl.c` calls `probe_rpc` first. The probe body
is one flush packet, four bytes, `0000`. git reads the status code, discards
the response body, and sends the real request only if the probe returned 200.

`handle-upload-pack` (`desk/app/urgit.hoon:7784`) had no equivalent of
`receive-probe:git-protocol`, which `f70e6a8` added to the push side. A
flush-only body fell through the protocol v2 dispatch, reached
`parse-upload-request`, and produced an upload request with an empty want
set. The handler then answered **400 `upload-pack request has no wants`**
and git stopped before it sent anything.

The brief predicted the message `invalid upload-pack request`. The status is
400 either way, but the arm that produced it is the want check at
`desk/app/urgit.hoon:7872`, not the parse check above it. A flush-only body
parses. It parses to nothing.

This is not the gzip defect that `74ad8c0` fixed. `GZIP-REQUEST.md` §1d
established that the two paths exclude each other. Below `http.postBuffer`
git gzips the body. Above it git switches to the probe and sends the body
uncompressed.

---

## 2. D1 — reproduction, before the fix

### 2a. Raw HTTP, four-byte body

```
$ printf '0000' | curl -i -X POST -H 'content-type: application/x-git-upload-pack-request' \
    --data-binary @- http://127.0.0.1:8093/git/gz1/git-upload-pack
HTTP/1.1 400 missing
content-type: text/plain

upload-pack request has no wants
```

The same request with `Git-Protocol: version=2` returns the same 400 and the
same message.

### 2b. Real `git clone`, protocol v0

```
$ git -c protocol.version=0 -c http.postBuffer=1024 clone --mirror \
    http://127.0.0.1:8093/git/gz1 /tmp/d1-v0
=> Send header: POST /git/gz1/git-upload-pack HTTP/1.1
=> Send header: Content-Length: 4
== Info: upload completely sent off: 4 bytes
<= Recv header: HTTP/1.1 400 missing
error: RPC failed; HTTP 400 curl 22 The requested URL returned error: 400
fatal: the remote end hung up unexpectedly
EXIT=128
```

### 2c. Real `git clone`, protocol v2, and the `LARGE_PACKET_MAX` workaround

`http.postBuffer=1024` is not a usable setting on v2. git 2.55.0 aborts
before it reaches the network:

```
BUG: remote-curl.c:1533: The entire rpc->buf should be larger than LARGE_PACKET_MAX
error: git-remote-http died of signal 6
```

The workaround is to raise `http.postBuffer` above the abort threshold and
to raise the request above `http.postBuffer` instead. The threshold was
measured by bisection on the v2 clone:

| `http.postBuffer` | result |
|---:|---|
| 65,516 / 65,520 / 65,521 / 65,522 / 65,523 | `BUG: remote-curl.c:1533` |
| **65,524** and above | reaches the network |

So the smallest usable v2 value on git 2.55.0 is **65,524**. The tests below
use 65,536, and they need a request above 65,536 bytes. Repository `pb1`
supplies it: 2,000 branches on a linear chain of 2,000 commits, 4,001
objects.

```
$ git -c protocol.version=2 -c http.postBuffer=65536 clone --mirror \
    http://127.0.0.1:8093/git/pb1 /tmp/d1-v2
=> Send header: POST /git/pb1/git-upload-pack HTTP/1.1
=> Send header: Content-Length: 4
== Info: upload completely sent off: 4 bytes
<= Recv header: HTTP/1.1 400 missing
error: RPC failed; HTTP 400 curl 22 The requested URL returned error: 400
fatal: expected flush after ref listing
EXIT=128
```

### 2d. Do the two protocol versions differ?

**No.** Both send the same four-byte body to the same route and both get the
same 400 with the same message. The only difference is on the client:
protocol v2 refuses `http.postBuffer` below 65,524, so the two versions need
different repositories to reach the same probe. The defect is in the body
reader, and the body reader does not look at the protocol version.

### 2e. Reproduction at default settings, no `-c` flag

Repository `big1` was built for this: **22,001 branches**, star topology,
22,003 objects. Every branch is one commit on a shared root, so the want
list is long while the object graph stays cheap. The uncompressed v0 request
crosses git's default 1 MiB `http.postBuffer`, and git sends the probe with
no client configuration at all.

```
$ git clone --mirror http://127.0.0.1:8093/git/big1 /tmp/x
=> Send header: Content-Length: 4          <- the probe
<= Recv header: HTTP/1.1 400 missing
EXIT=128        (v0)
EXIT=141        (v2)
```

---

## 3. D2 — the fix, and the naming choice

`handle-upload-pack` now answers the probe after the authorization check and
after `decoded-body`, and before `v2-command`. A flush-only body reaches
neither the v2 dispatch nor `parse-upload-request`. The answer is 200,
`content-type: application/x-git-upload-pack-result`,
`cache-control: no-store`, and a zero-length body.

**I took the rename.** `receive-probe:git-protocol` is now
`flush-only-body:git-protocol`, and both handlers call the one arm. Three
reasons:

1. The arm never knew anything about `receive-pack`. It decodes one packet,
   tests it for `%flush`, and tests the remainder for length zero. The old
   name described the caller, not the arm.
2. The brief states the preference, and the receive path has both live
   coverage and a conformance corpus to catch a slip. §5 exercises it: a
   9,768,379-byte push over the probe, a 5,271,415-byte push of 22,001 refs
   over the probe, a 61,764-byte push under the probe, and a clone-back that
   matches the source object set by SHA-256.
3. A second arm would leave two copies of the same three lines, which the
   brief forbids.

The body of the arm did not change. Only its name and its comment did. The
comment now names both services.

---

## 4. D3 — which cases reach this arm, and is 200 correct

### 4a. What stock `git-http-backend` does, and how I checked

I ran the real binary, `/usr/libexec/git-core/git-http-backend`, as a CGI
program over a real repository, with a four-byte body on stdin:

```
env -i PATH=/usr/bin:/bin GIT_PROJECT_ROOT=/tmp GIT_HTTP_EXPORT_ALL=1 \
    REQUEST_METHOD=POST PATH_INFO=/pb-src/git-upload-pack \
    CONTENT_TYPE=application/x-git-upload-pack-request CONTENT_LENGTH=4 \
    [HTTP_GIT_PROTOCOL=version=2] git-http-backend < flush.bin
```

| body | `Status:` header | content type | body length | exit | stderr |
|---|---|---|---:|---:|---|
| `0000`, v0 | none, so **200** | `application/x-git-upload-pack-result` | 0 | 0 | empty |
| `0000`, v2 | none, so **200** | `application/x-git-upload-pack-result` | 0 | 0 | empty |
| `0000GARBAGE` | none, so **200** | same | 0 | 0 | empty |
| empty | none, so **200** | same | 0 | 1 | `fatal: the remote end hung up unexpectedly` |
| `not a pkt line at all` | none, so **200** | same | 0 | 1 | `fatal: protocol error: bad line length character: not` |

A CGI program that writes no `Status:` header gets 200 from the server. The
first two rows are the answer the brief asked for, and they are measured,
not guessed: **stock git answers a flush-only `upload-pack` body with 200,
the upload-pack result content type, and an empty body, on both protocol
versions.**

The last three rows are a caution about the same table. `git-http-backend`
writes its headers before it starts `upload-pack`, so it cannot report a bad
body as 400. Its 200 on `0000GARBAGE` and on garbage is a property of CGI
ordering, not a ruling that those bodies are valid. urgit stays stricter and
returns 400 for them. D4 requires that, and §5 keeps it.

### 4b. Which cases reach the arm

**The probe.** `probe_rpc` in `remote-curl.c`, whenever a request exceeds
`http.postBuffer`. Measured on both protocol versions, at forced and at
default settings. 200 is correct: git acts only on the status code and
throws the response body away.

**A protocol v0 client that wants nothing.** In v0 the client's want list
ends with a flush, so a client that wants nothing sends a bare flush. The
protocol permits this, and stock `upload-pack` handles it: `receive_needs`
reads the flush, builds no want, and the process exits 0 with no output.
That is row 1 of the table above. 200 with an empty body is correct, and it
is what stock git sends.

I could not make git 2.55.0 produce that request over Smart HTTP. On an
up-to-date v0 fetch git sends **no POST at all**, and on an up-to-date v2
fetch git sends one 138-byte `ls-refs` POST and stops. Both were traced with
`GIT_TRACE_CURL`. So on this client the bare flush comes from the probe and
from nothing else. The case stays reachable in the protocol, and another
client may send it.

**A protocol v2 client that names no command.** A v2 body must start with
`command=…`. A bare flush ends the command sequence with no command, and the
stock v2 serve loop exits 0 with no output. That is row 2 of the table.
200 with an empty body is correct.

All three cases want the same answer, so this does not change the fix.

---

## 5. D4 and D5 — the evidence

Everything below ran against the final installed state: `git-protocol.hoon`
14,651 bytes with `flush-only-body` at 6,336 and no `receive-probe`, and
`app/urgit.hoon` 367,041 bytes.

### 5a. Fail closed, `upload-pack`

| body | before | after |
|---|---|---|
| `0000` | 400 `upload-pack request has no wants` | **200**, 0-byte body |
| `0000`, `Git-Protocol: version=2` | 400 `upload-pack request has no wants` | **200**, 0-byte body |
| `0000` gzipped, `content-encoding: gzip` | 400 `upload-pack request has no wants` | **200**, 0-byte body |
| `0000GARBAGE` | 400 `invalid upload-pack request` | 400 `invalid upload-pack request` |
| empty body | 400 `missing upload-pack request` | 400 `missing upload-pack request` |
| `not a pkt line at all` | 400 `invalid upload-pack request` | 400 `invalid upload-pack request` |
| `0000` claiming gzip and not gzip | — | 400 `invalid gzip request body` |
| `0000`, `content-encoding: br` | — | 415 `unsupported content-encoding` |

The before column is measured, not argued. I reinstalled the pre-fix desk on
the same ship, took the whole table, then reinstalled the fix and took it
again. Every 400 row is identical on both sides, message included.

Every 200 row carries `content-type: application/x-git-upload-pack-result`
and `cache-control: no-store`.

The gzipped probe row matters for ordering. The probe answer sits after
`decoded-body`, so a gzipped flush is a probe, and a body that lies about
its encoding is still a 400.

### 5b. Authorization still runs first

| case | status | body |
|---|---:|---|
| private repository, no credentials | **403** | `repository is private` |
| private repository, wrong write token | **403** | `repository is private` |
| private repository, correct write token | 200 | 0 bytes |
| repository that does not exist | **404** | `repository not found` |
| `GET` instead of `POST` | **405** | `method not allowed` |

An unauthenticated probe against a private repository never sees 200. The
`upload-pack` route answers 403, which is what it answered before this
change for every other body. The `receive-pack` route answers 401 for the
same probe, which is what `PUSH-DEFECTS.md` §3 recorded.

### 5c. `receive-pack` is unchanged by the rename

| body | status | note |
|---|---:|---|
| `0000` with credentials | **200** | `application/x-git-receive-pack-result`, 0 bytes |
| `0000GARBAGE` | 400 | `invalid receive-pack request` |
| `not a pkt line at all` | 400 | `invalid receive-pack request` |
| empty body | 400 | `missing receive-pack request` |
| `0000` with no credentials | **401** | `repository authentication required` |

Every row matches `PUSH-DEFECTS.md` §3.

### 5d. Clones

`--mirror` throughout, so the hash covers `refs/heads/*` and `refs/tags/*`
on both sides. `objects` counts `git rev-list --objects --all`. The hash is
`git rev-list --objects --all | sort | sha256sum`.

| repository | refs | proto | `http.postBuffer` | probes | encoding | exit | wall | objects | `fsck --full` | source match |
|---|---:|---|---|---:|---|---:|---:|---:|---|---|
| `big1` | 22,001 | v0 | **default** | **1** | none | 0 | 25.4 s | 22,003 | exit 0, no output | yes |
| `big1` | 22,001 | v2 | **default** | **1** | none | 0 | 24.8 s | 22,003 | exit 0, no output | yes |
| `pb1` | 2,000 | v0 | 65,536 | 1 | none | 0 | 85.3 s | 4,001 | exit 0, no output | yes |
| `pb1` | 2,000 | v2 | 65,536 | 2 | none | 0 | 85.4 s | 4,001 | exit 0, no output | yes |
| `gz1` | 201 | v0 | 1,024 | 1 | none | 0 | 0.28 s | 600 | exit 0, no output | yes |
| `gz1` | 201 | v0 | default | 0 | gzip, 5,142 | 0 | 0.38 s | 600 | exit 0, no output | yes |
| `gz1` | 201 | v2 | default | 0 | gzip, 5,218 | 0 | 0.38 s | 600 | exit 0, no output | yes |
| `gzreal` | 60 | v0 | default | 0 | gzip, 1,506 | 0 | 1.24 s | 1,204 | exit 0, no output | yes |
| `gzreal` | 60 | v2 | default | 0 | gzip, 1,579 | 0 | 1.27 s | 1,204 | exit 0, no output | yes |
| `gz2` | 18 | v0 | default | 0 | none, 936 | 0 | 0.05 s | 54 | exit 0, no output | yes |
| `gz2` | 18 | v2 | default | 0 | none, 1,023 | 0 | 0.06 s | 54 | exit 0, no output | yes |

Object-set hashes, source against clone:

- `/tmp/big-src` = `650d5f27a6e3b88058dd93e3fe7bfd154dcc78f0bff5e904a6b5395ab59e22fd`
- `/tmp/pb-src` = `e576159d24e0c70f409bf3f286b434b196addc13382040dd9a2f5615511b9085`
- `/tmp/gz-src` = `4e169f6389a27cc42955bbb2f2158e8977b152133bafee9b0b66d10769bc6f36`
- `/tmp/gz-real` = `8cf65babf73a1de8763df7f5995efd17892afaea3a3d6677e355063639f77a0f`
- `gz2` source subset = `1f7bd632baf711e4f931f7aa8c259c86e0783ceebb17338d545b31bc58571c49`

**The `big1` rows answer the brief's optional case.** I built the ~21,000-ref
repository and it clones at default settings, with no `-c` flag on the
client, over the probe. The same two clones exit 128 and 141 on the pre-fix
build, which is the before-and-after pair on the case that matters most.

**The last six rows are the regression that matters.** The request byte
counts 5,142 / 5,218 / 1,506 / 1,579 / 936 / 1,023 are the same six values
`GZIP-REQUEST.md` §5a recorded, byte for byte, and every object-set hash is
the one it recorded. A normal request takes the path it took before.

### 5e. Pushes

| case | probe | request | refs | exit |
|---|---|---:|---:|---:|
| this repository into `pbpush3`, default settings | **yes**, 4 bytes then 200 | 9,768,379, chunked | 11 | 0 |
| `big-src` into `big1`, default settings | **yes**, 4 bytes then 200 | 5,271,415, chunked | 22,001 | 0 |
| `gz-src` into `pbpush2`, default settings | no | 61,764, `Content-Length` | 201 | 0 |
| `pb-src` into `pb1`, default settings | no | 349,107 source stream | 2,000 | 0 |

The 61,764-byte row is the same request size `GZIP-REQUEST.md` §1e recorded
for the same push.

A clone-back of `pbpush3` gives 1,437 objects and hash
`9a56a4a21435fb5015c0099612d1a09d42cc5840f8e8d65a4b5af0cd79e1db50`, which is
identical to `git rev-list --objects --branches --tags | sort | sha256sum`
on the source working tree, with `fsck --full` clean. The push path survives
the rename.

### 5f. Conformance vectors

All 19 generators in `desk/gen/` were built out of the running desk with
`.^(vase %ca …)`, slotted to their gate and slammed. Every one built and
ran. The six that assert with `?>` would have crashed the thread.

| generator | `mug` | | generator | `mug` |
|---|---|---|---|---|
| `git-access-vector` | `0x58f6.45c3` | | `git-ofs-delta-pack-vector` | `0x0f9d.cb95` |
| `git-archive-vector` | `0x58f6.45c3` | | `git-pack-decode-vector` | `0x16f7.8288` |
| `git-blame-vector` | `0x5b53.27e0` | | `git-pack-vector` | `0x05a2.80f3` |
| `git-clay-vector` | `0x523f.8fda` | | `git-shallow-vector` | `0x664d.5ac4` |
| `git-codec-vector` | `0x5f23.4243` | | `git-stock-pack-vector` | `0x74cf.c008` |
| `git-delta-pack-vector` | `0x4402.c735` | | `git-storage-vector` | `0x1356.dfd3` |
| `git-github-vector` | `0x58f6.45c3` | | `git-tree-vector` | `0x3385.41cb` |
| `git-gzip-vector` | `0x0295.709b` | | `git-webhook-vector` | `0x58f6.45c3` |
| `git-inflate-vector` | `0x16f7.8288` | | `git-zlib-vector` | `0x16f7.8288` |
| `git-migration-vector` | `0x58f6.45c3` | | | |

All 19 match `GZIP-REQUEST.md` §5c, which is the mapping this report uses.

---

## 6. What I could not measure and why

**A real git client that sends a bare flush for a reason other than the
probe.** §4b argues from the protocol and from stock `upload-pack` behavior,
and it backs the stock behavior with a measured run of `git-http-backend`.
It could not produce the request from git 2.55.0 over Smart HTTP: an
up-to-date v0 fetch sends no POST, and an up-to-date v2 fetch sends only
`ls-refs`. The case is real in the protocol and the answer is right for it,
but no client in this run generated it.

**Whether `git-http-backend`'s 200 would survive a body check.** The last
three rows of §4a show stock git answering 200 to bodies it then rejects on
stderr. That is CGI header ordering, not a semantic ruling. It means the
stock server is not evidence about what a malformed `upload-pack` body
should return, only about what a flush-only one should. urgit's 400 for
malformed bodies rests on D4, not on stock git.

**Clone wall time on `pb1`.** 85 s for 2,000 refs on a 2,000-deep chain,
against 3.3 s for 2,001 refs on a star of depth 2 and 25 s for 22,001 refs
on a star. The cost tracks graph depth, not ref count. This is a single
reading per row from `date +%s.%N` around the client, not a benchmark, and
this brief did not authorize chasing it. It is recorded so the next reader
does not read the `pb1` rows as a ref-count cost.

**One 25.6 s reading on a `gz1` clone** taken in the first minute after a
`|commit`, against 0.38 s for the same clone before and after. Every re-run
is 0.38 s. That was the desk rebuild, not the clone path.

**Anything about clients other than `git` 2.55.0.** No JGit, no libgit2, no
other git version. The probe threshold, the `LARGE_PACKET_MAX` floor of
65,524 and the "no POST when nothing is wanted" behavior are all properties
of this client.

**The 148 MB `meme` ceiling.** Not hit. The largest repository here is
22,003 objects and the pier ran at `--loom 32` throughout.

**Whether 22,001 refs is the exact threshold at default settings.** It is
above it, and `big1` proves the case. The smallest ref count that crosses
git's default 1 MiB was not bisected. Arithmetic on a 50-byte want line puts
it near 20,972, which is where the brief's ~21,000 came from, but that
number is calculated and not measured.
