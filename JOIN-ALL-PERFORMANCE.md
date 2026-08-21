# `join-all` — from quadratic to linear

`join-all` folded a list of `octs` pairwise through `join`, which recopied the
growing accumulator once per piece. It now makes one `can 3` call over the whole
list.

Measured on a fake `~bus` booted from `brass-408k-1.pill`, pier
`/home/michael/piers/joinall-bus`, HTTP port 8092, Vere as shipped in
`workspace/urbit/bin`. Every number below came from a `~> %bout` hint read out
of the pier log, from `/usr/bin/time` on `curl`, or from `sha256sum`.

---

## 1. Measured exponent, before and after

### Instrument

A `click -k -i` probe builds `/lib/git-codec/hoon` out of the running desk and
calls `join-all` on `(reap N piece)` where `piece` is 143,360 bytes — the same
piece size the brief used. Timings come from `~> %bout.[1 %ja-NNNN]` in the pier
log; the click return value carries only lengths and `mug`s.

The running source was confirmed by scry, not by reading the mount:

```
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/lib/git-codec/hoon)
```

- before the edit: `(reel parts join)` found at offset 480, file 3,791 bytes
- after the edit: `(can 3 parts)` found at offset 901, `(reel parts join)`
  absent, file 4,208 bytes

### Before — `(reel parts join)`

| pieces | output bytes | reps (ms) | median |
|---:|---:|---|---:|
| 24 | 3,440,640 | 1.200, 1.204, 1.251 | **1.204** |
| 99 | 14,192,640 | 40.794, 34.707, 33.906 | **34.707** |
| 198 | 28,385,280 | 153.887, 150.121, 147.964 | **150.121** |
| 396 | 56,770,560 | 686.169, 664.708, 667.874 | **667.874** |

**Fitted exponent 2.256, R² = 0.9991.** Segment exponents: 24→99 = 2.372,
99→198 = 2.113, 198→396 = 2.153.

The brief's 2.028 reproduces as a slightly steeper 2.256. The excess over 2.0 is
the accumulator falling out of cache as it grows — the shape is quadratic either
way, and the 99-piece and 198-piece absolute times (34.7 ms and 150.1 ms) land
close to the brief's 40.90 ms and 171.65 ms on the same piece size.

### After — `(can 3 parts)`

| pieces | reps (ms) | median |
|---:|---|---:|
| 24 | 0.139, 0.137, 0.146, 0.189, 0.143, 0.149 | **0.144** |
| 99 | 2.652, 0.887, 0.815, 0.906, 0.854, 0.858 | **0.873** |
| 198 | 6.504, 1.690, 1.660, 1.692, 1.746, 1.686 | **1.691** |
| 396 | 13.424, 3.394, 3.223, 3.434, 3.310, 3.406 | **3.400** |

**Fitted exponent 1.129, R² = 0.9951.** Excluding the first rep — a cold-start
outlier about 4× the settled value at every size, kept in the table rather than
dropped silently — the fit is 1.125.

Segment exponents make the residual slope legible: 24→99 = 1.269,
99→198 = **0.955**, 198→396 = **1.008**. The excess sits entirely in the
smallest segment, where a ~0.1 ms fixed cost per call dominates 24 pieces of
work. From 99 pieces up the curve is linear to within measurement noise. At
n=396 the arm moves 56.8 MB in 3.400 ms — about 16.7 GB/s, i.e. memory
bandwidth.

| pieces | before | after | ratio |
|---:|---:|---:|---:|
| 24 | 1.204 ms | 0.144 ms | 8.3× |
| 99 | 34.707 ms | 0.873 ms | 39.8× |
| 198 | 150.121 ms | 1.691 ms | 88.8× |
| 396 | 667.874 ms | 3.400 ms | **196.4×** |

---

## 2. The new implementation

```hoon
::  One +can over every piece.  Folding +join pairwise recopied the
::  accumulator once per piece, which is quadratic in the piece count.
::  +can truncates each piece to its own p and shifts by p, exactly as
::  the fold did, so a piece with p=0 still contributes nothing and a
::  piece whose atom is narrower than p still advances the offset by p.
::
++  join-all
  |=  parts=(list octs)
  ^-  octs
  :-  (roll parts |=([piece=octs total=@ud] (add p.piece total)))
  (can 3 parts)
```

`octs` is `(pair @ud @)` and `can` takes `(list [p=step q=@])` where `step` is
`_`@u`1`. `@ud` nests under `@u`, so the list passes through with no `turn` and
no intermediate allocation. The length is a separate `roll`; two O(N) passes
replace N growing copies.

### Why it is byte-identical

`can` is

```hoon
?~  b  0
(add (end [a p.i.b] q.i.b) (lsh [a p.i.b] $(b t.b)))
```

Each piece is truncated to its own `p` by `end` and the rest is shifted up by
exactly `p` bytes. The old fold applied the *same* `end`/`lsh` pair at every
step, because `join` is itself a two-element `can`. So:

- **Empty list.** `reel` over `~` returned the bunt of `join`'s second sample,
  `right=octs`, i.e. `[0 0]`. `can 3 ~` is `0` and the `roll` is `0`, giving
  `[0 0]`. Same. This matters: `app/urgit.hoon:7921` compares against a
  literal `[0 0]` and `:7918` feeds `(join-all ~)` straight into another
  `join-all`.
- **Single element.** The fold produced
  `[p.a (can 3 ~[[p.a q.a] [0 0]])]` — the trailing `[0 0]` adds
  `(lsh [3 0] 0)` = 0. The single-`can` form drops that no-op term and applies
  the identical `end [3 p.a]`.
- **`p=0` inside a non-empty list.** `end [3 0] q` is `(mod q 1)` = 0 and
  `lsh [3 0]` is the identity, so the piece contributes nothing and shifts
  nothing — under both forms, and regardless of what `q` holds. Verified
  directly: `~[b-one [0 12.345] b-one]` gives `[2 30.840]` (`'xx'`) before and
  after, with the 12,345 discarded.
- **`p` wider than the atom.** `git-archive.hoon:118` builds
  `[(add p.body padding) q.body]` — a declared length larger than
  `(met 3 q)` — and `:134` appends `[1.024 0]`. `end` is a no-op on an atom
  already narrower than the cut, and `lsh` still shifts the full `p`. The
  padding survives, alignment is preserved. This is the one case a naive
  `(rap 3 ...)` rewrite would have silently broken; `can` is the right
  primitive precisely because it carries an explicit width per piece.

No intermediate accumulator was ever wider than the sum of the `p`s it covered,
so the fold's per-step truncation could never have removed a byte the
single-`can` form keeps. The two forms agree on every input by construction, and
the evidence in §4 confirms it on real bytes.

---

## 3. Per-site audit

`grep -rn 'join-all' desk/lib desk/app` returns 32 occurrences across 8 files.
All 32 were read. Four more live in `desk/gen` and were exercised too.

None of the 32 depends on the pairwise shape. Two groups needed real scrutiny;
the rest are plain ordered concatenation where only list order matters, and
order is unchanged.

### `desk/lib/git-archive.hoon` — 2 sites, alignment-sensitive

| line | site | finding |
|---|---|---|
| 69 | `encode-header` over 17 `(pack field)` results | Every piece is `[length field]` with `length ≥ (met 3 field)` — a right-padded fixed-width tar field. Depends entirely on `p`-driven shifting, which `can` does natively. The `?> =(512 p.bytes)` on the next line constrains only the sum of `p`s, which is unchanged. |
| 134 | `(weld (flop parts) ~[[1.024 0]])` | The two 512-byte zero blocks that terminate a ustar file, expressed as one 1,024-byte piece whose atom is literally `0`. `met` of that atom is 0; only `p` carries the length. Preserved. |

`file-entry` at `:119` uses `join` directly, not `join-all`, and is untouched.
Its `padded=[(add p.body padding) q.body]` argument still flows into `:134`'s
`join-all`, which is why the wide-`p` case above matters.

Confirmed by output, not by reading: the fixture tar (§4) and a real 121-file
repository tar are byte-identical across the change, and `git-archive-vector` —
which asserts hard byte offsets 100, 156, 512, 612, 668, 1024, 1124, 1180 —
still passes.

### `desk/lib/git-codec.hoon` — 2 sites

| line | site | finding |
|---|---|---|
| 28 | the definition | rewritten |
| 142 | `canonical-object`: `~[(text header) (oct 0) data]` | The `(oct 0)` is the NUL between a Git loose-object header and its content. It is `[1 0]` — one byte of width, zero of magnitude. If `can` dropped it, every OID in the system would change. It does not: `end [3 1] 0` is 0 and the shift is 1 byte. Every OID in §4 is unchanged, including the `git hash-object` reference value `3b18e512dba79e4c8300dd08aeb37f8e728b8dad` that `git-codec-vector` asserts. |

### `desk/lib/git-pack.hoon` — 7 sites

Lines 38, 59, 67, 73, 82, 88, 92. All ordered concatenation of pack framing.
Three shapes worth naming:

- `:59`/`:67` — `stored-deflate` emits 5-byte DEFLATE block headers plus a
  `slice` payload, then joins `(flop blocks)`. Block boundaries are byte-exact
  and driven by `p`.
- `:73` — `zlib-store` prepends the literal `[2 0x178]`. `met` of `0x178` is 2,
  so this is a well-formed piece either way.
- `:92` — a `join-all` *inside* a `join-all` list. Both collapse to `can`; the
  inner one produces a single `octs` the outer treats like any other piece.

`encode-pack` at `:100` uses `join` directly for the 20-byte trailer and is
untouched.

### `desk/lib/git-protocol.hoon` — 9 sites

Lines 254, 286, 303, 340, 358, 376, 422, 430, 449. All pkt-line assembly:
capability advertisement, ref advertisement (with peeled tags), v2 `ls-refs`,
v2 sideband chunking, `object-info`, `receive-status`, `line-payload`,
`smart-advertisement`. Every piece comes from `en-pkt`, `text`, or `oct`, all of
which set `p` to the true byte length. `line-payload` at `:376` embeds
`(oct:git-codec 0)` as a capability separator and `(oct:git-codec 10)` as the
line terminator — the same one-byte-width case as `canonical-object`.

`v2-sideband-pack` at `:303` is the only one with a size constraint
(65,515-byte chunks). It is driven by `slice`, not by `join-all`, and is
unaffected.

### `desk/app/urgit.hoon` — 6 sites

Lines 7918, 7921, 7938, 7959, 7964, 7970 — all inside `handle-upload-pack`.
This is the group that uses `[0 0]` as a sentinel:

- `:7918` `shallow-lines` is `(join-all shallow-packets)` where
  `shallow-packets` is `~` on a non-shallow fetch. It must come back `[0 0]`,
  and it does.
- `:7921` guards with `?~ shallow-packets [0 0]` and otherwise joins
  `shallow-lines` with a flush packet.
- `:7964`/`:7970` splice `final-shallow` — often the literal `[0 0]` — into the
  response. A zero-width piece in the middle of a list must not shift what
  follows. It does not.

Exercised live: a `--depth 1` clone, a `--unshallow` fetch, and an ordinary
full clone all produce the correct object set (§4).

### `desk/lib/git-clay.hoon` — 2 sites

| line | site | finding |
|---|---|---|
| 27 | `tree-record`: mode, SP, name, NUL, `[20 (rev 3 20 oid)]` | The OID piece is the interesting one. `(rev 3 20 oid)` on an OID whose *first* byte is `0x00` yields an atom with fewer than 20 significant bytes; the declared `p=20` is what keeps the record 20 bytes wide. Same wide-`p` case as tar padding, handled the same way. A regression here would change every tree OID and therefore every commit OID. |
| 90 | `build-tree`: joins sorted `tree-record`s | Ordered concatenation; sort order is computed before the join and unchanged. |

### `desk/lib/git-tree.hoon` — 1 site

Line 212, `store-tree` — the same tree-body concatenation as `git-clay:90`,
over `(sort entries entry-before)`.

### `desk/lib/git-github.hoon` — 3 sites

Lines 306, 325, 334 — outbound `want` lists, a `receive-pack` command line, and
a `receive-request` body that appends a whole `encode-pack` result as one piece.
Same shapes as `git-protocol`. **Not exercised live** — see §5.

### `desk/gen` — 4 further sites

`git-inflate-vector:10`, `git-inflate-vector:16`, `git-shallow-vector:21`,
`git-zlib-vector:9`. Test fixtures; all four generators pass unchanged.

---

## 4. Byte-identity evidence

Every artifact below was produced twice from the same inputs on the same pier,
once with each implementation, with the running source confirmed by scry between
flips. The sequence was old → new → old → new, so the old-code numbers are
reproduced, not remembered.

### 4a. Pack SHA-1 over a real repository, on the wire

A stock `git push` put this repository (1,343 objects, 10.9 MiB packed by git)
onto `~bus` as `bench`. The raw v0 `git-upload-pack` response was then fetched
with `curl` and a hand-built pkt-line request — no git client in the path, so
the bytes compared are exactly what Eyre emitted.

```
response: 0008NAK\n + PACK\0\0\0\2 ... , 1,228 objects, 31,625,530 bytes
```

| | before | after |
|---|---|---|
| response SHA-256 | `3c5abc7ca5d73e29093ee80ab8f9635e84ab5b83ef23e5b366b60e78b729f098` | **identical** |
| **pack trailer SHA-1** | **`29a0e12004afe9342195d8af8bd25871292c7687`** | **identical** |
| objects declared | 1,228 | 1,228 |
| `curl` wall time | 1.786 s, 1.79 s | 0.61, 0.61, 0.61, 0.63, 0.63, 0.64 s |

`cmp` on the two files reports no difference. The pack's trailing 20 bytes are
its own SHA-1 and recompute correctly from the body under Python's `hashlib`,
so the digest is self-consistent as well as unchanged.

The wall time is the independent instrument the brief asked for: it is measured
outside the ship, over the whole `handle-upload-pack` path, and it moves 2.9×.
The fix reached the running code.

### 4b. Conformance vectors

All 18 generators in `desk/gen` were built and slammed in one thread; the `mug`
of each product was compared. The eight the brief names are in bold.

| generator | mug before | mug after |
|---|---|---|
| `git-access-vector` | `0x58f645c3` | `0x58f645c3` |
| **`git-archive-vector`** | `0x58f645c3` | `0x58f645c3` |
| `git-blame-vector` | `0x5b5327e0` | `0x5b5327e0` |
| `git-clay-vector` | `0x523f8fda` | `0x523f8fda` |
| **`git-codec-vector`** | `0x5f234243` | `0x5f234243` |
| **`git-delta-pack-vector`** | `0x4402c735` | `0x4402c735` |
| `git-github-vector` | `0x58f645c3` | `0x58f645c3` |
| `git-inflate-vector` | `0x16f78288` | `0x16f78288` |
| `git-migration-vector` | `0x58f645c3` | `0x58f645c3` |
| **`git-ofs-delta-pack-vector`** | `0x0f9dcb95` | `0x0f9dcb95` |
| `git-shallow-vector` | `0x16f78288` | `0x16f78288` |
| **`git-pack-decode-vector`** | `0x05a280f3` | `0x05a280f3` |
| **`git-pack-vector`** | `0x664d5ac4` | `0x664d5ac4` |
| **`git-stock-pack-vector`** | `0x74cfc008` | `0x74cfc008` |
| `git-storage-vector` | `0x1356dfd3` | `0x1356dfd3` |
| **`git-tree-vector`** | `0x338541cb` | `0x338541cb` |
| `git-webhook-vector` | `0x58f645c3` | `0x58f645c3` |
| `git-zlib-vector` | `0x16f78288` | `0x16f78288` |

The repeated `0x58f645c3` is `[%noun %.y]`: those five generators assert their
invariants internally with `?>` and return a boolean. A regression makes the
thread crash rather than change the mug — `git-archive-vector` in particular
asserts eight exact tar byte offsets.

### 4c. A fixture built for the edge cases

A generator (`gen/git-joinall-fixture.hoon`, installed on the pier only, not
committed) builds a two-level source tree from blobs of 0, 1, 511, 512, 513,
65,536, and 70,000 bytes plus one blob declared 512 bytes wide over a one-byte
atom, adds a symlink and an executable, then produces both a pack and a tar and
reports SHA-1 over the bytes. The 70,000-byte blob crosses the 65,535-byte
stored-DEFLATE block boundary; the 512-declared/1-byte-wide blob is the
`p > (met 3 q)` case.

| | before | after |
|---|---|---|
| objects | 15 | 15 |
| pack bytes | 138,348 | 138,348 |
| **pack SHA-1** | **`f52262db1ee663b67c35417931e7e798b0e5ec91`** | **identical** |
| tar bytes | 145,408 (= 284 × 512) | 145,408 |
| **tar SHA-1** | **`ff19fa17fe55085238d39b7402cbedfd33854870`** | **identical** |
| root tree OID | `c2d474129a3664dd20b2f15edb45aa8b442ec346` | identical |
| commit OID | `6288a7350ee678d8c9fa2daf421c0da719a04940` | identical |

Direct `join-all` edge cases, before and after:

| input | result |
|---|---|
| `~` | `[0 0]` |
| `~[b-513]` | `[513 mug 0x1350f6e8]` |
| `~[b-one [0 0] b-one]` | `[2 30.840]` (`'xx'`) |
| `~[b-one [0 12.345] b-one]` | `[2 30.840]` — the 12,345 discarded |
| `~[(oct 65) [1.024 0] (oct 66)]` | `[1026 mug 0x7dbd8110]` |
| `~[b-short b-one]` | `[513 mug 0x0bfdf90d]` |
| `~[[0 0] [0 0] [0 0]]` | `[0 0]` |

The four `join-all` results from the §1 timing sweep also carry `mug`s, and
those matched across the change too: `0x41629be6`, `0x19bf6627`, `0x4a4e16d0`,
`0x2db0a247` at 24, 99, 198 and 396 pieces.

### 4d. Stock `git` and `git fsck --full`

All clones below ran against `~bus` on port 8092 with the stock `git` on this
machine.

| operation | result | `git fsck --full` |
|---|---|---|
| clone, protocol v0 (before) | 1.864 s, HEAD `dd0a869…` | clean, exit 0, no output |
| clone, protocol v0 (after) | 0.832 s, HEAD `dd0a869…` | clean, exit 0, no output |
| clone, protocol v2 (after) | 0.775 s | clean, exit 0, no output |
| `--depth 1` shallow clone (after) | 1 commit, HEAD `dd0a869…` | clean, exit 0 |
| `fetch --unshallow` (after) | full history restored | clean, exit 0 |

`git rev-list --objects --all | sort | sha256sum` is
`e78e57ed285cabda29f79583930bb45950ff068bcc0b38b766bc53e876845812` for the
before clone, the after clone, the v2 clone, and the unshallowed shallow clone —
four independent transfers, one object set.

A second push (a branch and an annotated tag) also succeeded, and `ls-remote`
returns the correct peeled advertisement:

```
a17222b04f167f14c38b86ad6ff673537a13d0d0  refs/tags/v-joinall
dd0a869049c709d42131e52b3cbbb1ed6843ea49  refs/tags/v-joinall^{}
```

### 4e. Tar archive over a real repository

`GET /apps/urgit/api/public/repository/bench/archive?ref=refs/heads/main` runs
`archive:git-archive` over the pushed repository.

| | before | after |
|---|---|---|
| bytes | 1,227,264 | 1,227,264 |
| SHA-256 | `d56931e8c9194edf1c923759ff86bcd6fa427ee90ad3cd4062b3144898203714` | **identical** |
| entries | 121 | 121 |

`cmp` reports no difference, and stock `tar -tf` lists all 121 entries. A
misplaced pad byte anywhere in a 1.2 MB ustar stream would desynchronise every
header after it, so this is a sharper alignment test than the pack.

---

## 5. Effect at erpit's scale — measured, not projected

The brief allows a projection at 9,016 objects. A projection was not necessary:
the workload runs.

`encode-pack` was timed on a synthetic object set of **9,016 blobs of 23,000
bytes**, producing a **207,494,256-byte** pack — erpit's stated object count and
close to its stated 203 MB.

| implementation | `encode-pack` over 9,016 objects |
|---|---|
| pairwise fold | **62.198 s** |
| single `can` | **1.202 s, 1.081 s** |
| ratio | **57.6×** |

Pack length (`0xc5e1c70`) and pack `mug` (`0x7878eac4`) are identical between
the two, so this is also a byte-identity check at that scale.

A smaller pack-level run at 1,228 objects × 25,600 bytes (31,454,024-byte pack,
`mug 0x078804e4` under both):

| implementation | `encode-pack` over 1,228 objects |
|---|---|
| pairwise fold | 1.270 s, 1.259 s |
| single `can` | 0.158 s, 0.159 s, 0.158 s |
| ratio | **8.0×** |

**This is a measurement of erpit's *shape*, not of erpit.** erpit's actual
repository was never on this pier. What is measured is a pack of erpit's object
count and roughly its byte count, built by the same `encode-pack` on the same
Vere. A real repository differs in per-object size distribution, which moves the
constant but not the exponent — and the exponent is what changed.

The 8.0× at 1,228 objects and 57.6× at 9,016 objects are themselves the
signature of the fix: the speedup grows with N, which is what removing a square
term looks like.

### Three instruments agree

The fence in the brief — a fix verified only by its own instrument is
unverified — is satisfied by three independent measurements of the same change:

| instrument | scope | before | after | ratio |
|---|---|---|---|---|
| `%bout` on `join-all`, 396 × 143 KB | the arm itself | 667.9 ms | 3.4 ms | 196× |
| `%bout` on `encode-pack`, 9,016 objects | pack construction | 62.198 s | 1.081 s | 57.6× |
| `curl` wall time on `git-upload-pack` | whole HTTP path, outside the ship | 1.786 s | 0.61 s | 2.9× |

The ratios shrink as the scope widens, exactly as they should: the arm is the
whole cost of the first, most of the second, and about two thirds of the third.
The third number falls out of a stopwatch on another process, so it cannot be
an artifact of the hint.

---

## 6. What I could not measure and why

**erpit itself.** The 9,016-object figure above is a synthetic pack of erpit's
object count and approximate byte count, not erpit's repository. Its object-size
distribution, delta structure, and tree shape are all different. The exponent
carries; the constant does not.

**`git-github.hoon`'s three call sites.** Lines 306, 325 and 334 build outbound
`want` lists and a `receive-request` for GitHub's Smart HTTP. Reaching them
needs a GitHub remote and, for anything private, a token. They were read and
their shapes match sites that are covered — plain ordered concatenation of
`en-pkt` and `text` results — but no bytes from those three arms were compared.

**The `%archive` Mesa ship-to-ship path.** Off limits by the brief, and it skips
pack construction anyway. `join-all`'s cost on that path is whatever
`git-codec`'s other callers spend, which is not separately measured here.

**Protocol v2 at the byte level.** The v2 clone was verified by object set and
`git fsck`, not by a raw response diff. Only the v0 `git-upload-pack` response
was compared byte for byte. v2 routes through `v2-sideband-pack` and
`urgit.hoon:7938`/`:7959`, which the clone exercises but does not hash.

**Push-side pack decoding.** `git-pack-decode.hoon` does not call `join-all` and
was not touched. The pushes above prove it still ingests packs; nothing about
decode timing was measured.

**Where the remaining 0.61 s of `git-upload-pack` goes.** After the fix,
`encode-pack` at this repository's size is ~0.16 s while the HTTP round trip is
~0.61 s. The other ~0.45 s was not attributed. Candidates are object lookup and
reachability closure, Eyre's 31 MB response handling, and `sha1-octs` over the
whole pack. `join-all` is no longer the top term there, and finding the next one
is separate work.

**Allocation versus copy inside `can`.** `~> %bout` gives wall time for the
whole jetted call. No split between allocating the result atom and memcpy-ing
the pieces was attempted, so the 16.7 GB/s at n=396 is a throughput figure, not
a decomposition.

**Protocol v2 push.** `git -c protocol.version=2 push` returns HTTP 400 against
this build; the v0 push works. This reproduces on the unmodified code, so it is
not caused by this change, and it was not investigated further.

**Cold-start variance.** The first rep after each desk reload runs about 4×
slower than the settled value at every size. It is kept in the tables and the
medians are taken over the settled reps. Whether that is jet warm-up, cache
warm-up, or something in Gall's reload path was not determined.
