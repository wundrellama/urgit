# `resolve-entries`: where the 2.34 s goes

The 2.34 s is now attributed. The components sum to 97.3% of the whole, and
the 2.7% left over is the bare loop walk that no probe isolated.

The top term is `apply-delta`, at **56.4%**. It is fixable inside `desk/`, and
this run fixed it. `copy-instruction` now reads each field of a copy opcode
with one `cut` instead of seven `optional-byte` calls. A whole erpit pack
decode drops from 2.346 s to 1.887 s. A stock `git push` of erpit `master`,
timed on the client with `/usr/bin/time`, drops from **1.87 s to 1.56 s**.

The second term is `object-oid`, at 39.5%. Three quarters of it is the
`(rev 3 ...)` in `sha1-octs`, which costs 3.2 times the SHA-1 it feeds. That
one is out of scope. Section 3b says why, and where the code that would fix it
lives.

The `19 µs` per instruction figure does not generalize. Section 2 gives the
real curve.

---

## 0. Measurement setup

Pier `~bus` at `/home/michael/piers/joinall-bus`, HTTP port 8093, Vere 4.6
from `workspace/urbit/bin`. The client is stock `git` 2.55.0 on this machine.

The attribution series in section 1 ran at `--loom 31` (2 GB). Sections 3
through 5 repeat the key numbers at `--loom 32` (4 GB), because a clone of a
148 MB repository does not fit in a 2 GB loom. The two looms agree.

Every time below comes from a `~> %bout` hint read out of the pier log, or
from `/usr/bin/time` on the `git` client.

The running source was confirmed by scry, never by reading the mount:

```
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/lib/git-delta/hoon)
```

| state | `git-delta.hoon` | token |
|---|---:|---|
| before | 3,640 | `optional-byte` at 488, `copy-instruction-sparse` absent |
| after | 5,897 | `copy-instruction-sparse` at 1,378, `offset-width` at 2,023 |

The instruments live on the pier only, in `lib/re-inst.hoon`,
`lib/ad-inst.hoon` and `lib/pd-before.hoon`. Nothing outside
`desk/lib/git-delta.hoon` changed.

### The pack under test

`dat/erpit.mime`, the repack `git repack -a -d -f --depth=50 --window=250`
that `QUESTIONS.md` §2e measured. 3,978,266 bytes, 9,048 objects, every delta
an `ofs-delta`.

The brief quotes byte counts that belong to a different pack. It gives 8,995
objects, 6,222 deltified objects, 120,484 instructions and 202,472,812
canonical bytes. Those are erpit's **three original packs**. The 2.34 s was
measured on the **repack**. The repack's own figures, counted in Python and
confirmed in the ship, are:

| | brief (three packs) | measured (the repack) |
|---|---:|---:|
| objects | 8,995 | **9,048** |
| full objects | 2,773 | **2,543** |
| deltified objects | 6,222 | **6,505** |
| delta instructions | 120,484 | **99,603** |
| canonical bytes hashed | 202,472,812 | **206,358,767** |
| worst object, instructions | 1,264 | **1,684** |
| median instructions per object | 5 | **4** |

The 206,358,767 figure is not arithmetic. An in-ship probe summed
`p` over every canonical object and returned `0xc4cc8ef` = 206,358,767, which
matches the Python count exactly. A second probe summed the raw content bytes
and returned 206,267,908, which matches
`git rev-list --objects --all | git cat-file --batch-check='%(objectsize)'` on
the source repository to the byte.

`resolve-entries` needs **one pass** on this pack. A probe read
`(lent pending)` out of `resolve-pass` and got 0.

---

## 1. D1 — the attribution

`resolve-entries` on this pack, before the fix: **2,196.0 ms**, median of six.

`QUESTIONS.md` recorded 2.340 s before `bf3dbea` and 2.170 s after it. `bf3dbea`
is on this branch, so 2,196.0 ms is the same measurement as that 2.170 s. Both
runs return object map `mug 0x7b1d6883`.

### 1a. The table

Each component was timed twice, by two methods that share no arithmetic.

**Method A, direct.** An arm walks the resolved objects and calls one component
on each, under its own `%bout`. A companion arm walks the same list and does
nothing, and its time is subtracted.

| component | ms | % of `resolve-entries` |
|---|---:|---:|
| `apply-delta`, all 6,505 calls | **1,238.0** | **56.4%** |
| `object-oid`, all 9,048 calls | **867.7** | **39.5%** |
| — `canonical-object` | 90.8 | 4.1% |
| — `rev` inside `sha1-octs` | 590.0 | 26.9% |
| — `sha-1l` | 186.9 | 8.5% |
| three `~(put by ...)` per object | 25.6 | 1.2% |
| `~(get by offsets)` per delta | under noise | < 0.5% |
| loop walk | 5.0 | 0.2% |
| **sum** | **2,136.3** | **97.3%** |
| **not attributed** | **59.7** | **2.7%** |

**Method B, ablation in the real loop.** Thirteen copies of `resolve-pass`,
each the byte-for-byte loop of `desk/lib/git-pack-decode.hoon` with exactly one
insertion or one substitution. Every variant decodes the whole pack.

| variant | change | ms | delta vs `v0` |
|---|---|---:|---:|
| `v0` | none | 2,196.0 | — |
| `vz1` | one extra `(add 0 offset)` | 2,189.4 | −6.6 |
| `vg1` | one extra `~(get by offsets)` | 2,174.8 | −21.2 |
| `vp1` | three extra `~(put by ...)` | 2,208.0 | +12.0 |
| `vp2` | six extra `~(put by ...)` | 2,206.4 | +10.4 |
| `vc1` | one extra `canonical-object` | 2,268.5 | +72.5 |
| `vc2` | two extra `canonical-object` | 2,333.2 | +137.2 |
| `vr1` | one extra `canonical-object` and `rev` | 2,868.2 | +672.2 |
| `vo1` | one extra `object-oid` | 3,021.9 | +825.9 |
| `vo2` | two extra `object-oid` | 3,917.9 | +1,721.9 |
| `vn1` | `object-oid` replaced by the entry offset | 1,310.6 | **−885.4** |
| `vd1` | one extra `apply-delta` | 3,377.9 | +1,181.8 |
| `vd2` | two extra `apply-delta` | 4,647.7 | +2,451.7 |

`vz1` and `vg1` are controls. Both land inside the noise band, so the insertion
machinery costs nothing and a map read costs nothing.

The two methods agree on both large terms:

| | method A | method B, add one | method B, second marginal | method B, remove |
|---|---:|---:|---:|---:|
| `apply-delta` | 1,238.0 | 1,181.8 | 1,225.8 | — |
| `object-oid` | 867.7 | 825.9 | 861.0 | 885.4 |
| `rev` | 590.0 | 599.7 | — | — |

A third probe measured `rev` and `sha-1l` over a pre-built list of canonical
objects, away from the resolve loop: `rev` 598.8 ms, `rev` plus `sha-1l`
792.2 ms, so `sha-1l` 193.4 ms. That agrees with both other readings.

**The attribution closes.** Every variant returned 9,048 objects and
`mug 0x7b1d6883`, except `vn1`, whose map keys are entry offsets by
construction.

### 1b. The three map puts are not the problem

Method B measured 12 ms for three extra puts, but a re-put of a key that is
already in the map skips the rotations that a fresh insert does. Method A built
the three maps from empty, 27,144 real inserts, and measured 25.6 ms. Take the
larger number: the three maps cost **1.2%**. Restructuring the state would buy
nothing, so this run did not touch it.

---

## 2. D1 — the per-instruction curve, and the verdict on 19 µs

Every deltified object in the pack was sorted into a band by its instruction
count. Each band was timed on its own, with the selection done outside the
`%bout` and an empty-walk floor subtracted.

| band | objects | instructions | per object | result KB/object | µs/object | **µs/instruction** |
|---:|---:|---:|---:|---:|---:|---:|
| 1 | 87 | 87 | 1.0 | 14.2 | 29.0 | **28.98** |
| 2 | 877 | 1,754 | 2.0 | 12.7 | 38.1 | **19.05** |
| 3–4 | 2,440 | 7,821 | 3.2 | 13.6 | 51.8 | **16.15** |
| 5 | 606 | 3,030 | 5.0 | 22.9 | 74.8 | **14.96** |
| 6–10 | 896 | 6,758 | 7.5 | 36.8 | 107.9 | **14.30** |
| 11–13 | 251 | 3,017 | 12.0 | 54.7 | 165.7 | **13.78** |
| 14–32 | 668 | 14,382 | 21.5 | 54.5 | 263.6 | **12.24** |
| 33–60 | 357 | 15,956 | 44.7 | 57.7 | 508.7 | **11.38** |
| 61–79 | 104 | 7,117 | 68.4 | 70.0 | 774.4 | **11.32** |
| 80–128 | 105 | 10,380 | 98.9 | 94.4 | 1,097.0 | **11.10** |
| 129–512 | 103 | 20,392 | 198.0 | 108.3 | 2,169.5 | **10.96** |
| 513–1,684 | 11 | 8,909 | 809.9 | 182.0 | 9,507.4 | **11.74** |
| **all** | **6,505** | **99,603** | 15.3 | | | **12.43** |

The cost of one `apply-delta` call is a fixed part plus a part that scales with
the instruction count:

```
cost per object = 18.1 µs + 10.86 µs x instructions
```

That line is fitted on the 1-instruction band and the 129–512 band. It predicts
the 5-instruction band within 3%, the 33–60 band within 1%, and the 80–128 band
within 0.5%. It runs 8% low on the two bands with the largest results, where the
byte copying starts to show.

**19 µs per instruction does not generalize.** It is not the marginal cost,
which is 10.9 µs. It is not the average on this pack, which is 12.4 µs. It is
the average for an object with exactly two instructions, and erpit's median
deltified object has four.

The brief's arithmetic fails for two separate reasons. It uses 120,484
instructions, which belongs to a different pack, and it uses a constant that
describes only the smallest objects. Carried through on the pack that was
actually timed, 19 µs x 99,603 gives 1.89 s, which would make `apply-delta` 86%
of `resolve-entries`. The measured share is 56.4%.

Of the brief's three propositions:

1. **19 µs per instruction generalizes — false.** The curve runs from 29 µs at
   one instruction to 11 µs at two hundred.
2. **`object-oid` over the canonical bytes is negligible — false.** It is
   867.7 ms, 39.5%, not the 50 ms the leftover implied.
3. **The `resolve-entries` hint measures what it appears to — true.** The
   components sum to 97.3% of it.

---

## 3. D2 and D3 — the top term, and what this run did

### 3a. `apply-delta`, 56.4%, fixed

`apply-delta` is the top term by both methods and by a wide margin. The second
term, `object-oid`, is 30% smaller.

Inside `apply-delta`, the pack holds 64,141 copy instructions and 35,462 insert
instructions. Two more ablations, on a verbatim copy of the old arm:

| variant | ms | delta | per call |
|---|---:|---:|---:|
| baseline copy of `apply-delta` | 1,218.8 | — | — |
| one extra set of seven `optional-byte` | 1,764.9 | **+546.1** | 1.22 µs per `optional-byte` |
| one extra `slice` per copy instruction | 1,306.9 | +88.1 | 1.37 µs per `slice` |

`QUESTIONS.md` §4 Option B named `copy-instruction` and marked it a guess. The
guess is right and the number is large: the seven `optional-byte` calls are
**44.8% of `apply-delta`** and **24.9% of `resolve-entries`**. The byte copying
they wrap is 7.2%.

**The change.** A copy opcode carries a seven-bit mask that names which bytes of
the copy offset and the copy size follow it. Git writes each field
little-endian and drops only its leading zero bytes, so the set bits of each
field are almost always a run that starts at the field's low bit. A run is one
`cut` of the delta stream. Over the whole pack, 63,184 of 64,141 copy
instructions take that path, which is **98.5%**.

A mask with a gap in it is still legal. `copy-instruction-sparse` keeps the old
byte-at-a-time form for that case, and `copy-instruction` calls it when either
field is not a run.

Two other shapes were built and measured first, and this one won:

| `apply-delta` over the whole pack | ms | vs baseline |
|---|---:|---:|
| baseline, seven `optional-byte` calls | 1,225.4 | — |
| read seven bytes in one `cut`, index by popcount | 1,061.9 | −13% |
| **read each field as one run** | **764.9** | **−38%** |

The popcount form trades seven gate calls for eleven jet calls and wins little.
The run form makes two `cut` calls in total.

### 3b. `object-oid`, 39.5%, out of scope

`object-oid` builds the canonical bytes, reverses them, and hashes them.

| | ms | throughput over 206.36 MB |
|---|---:|---:|
| `canonical-object` | 90.8 | 2.27 GB/s |
| `rev` inside `sha1-octs` | 590.0 | **345 MB/s** |
| `sha-1l` | 186.9 | 1.10 GB/s |

**The byte reverse costs 3.2 times the SHA-1 it feeds.** Both are jetted C.

`rev` is not doing three passes. `++ rev` in `sys/hoon.hoon` reads
`(lsh ... (swp boz (end [boz len] dat)))`, and a probe timed each part over the
same 9,048 canonical objects: `end` 8.5 ms, `swp` 606.3 ms, the whole `rev`
603.5 ms. `rev` costs what `swp` costs. The reversal itself is the price, plus
one 206 MB allocation.

The reverse cannot be removed inside `desk/`. `sha1-octs` calls
`(sha-1l:sha [p.bytes (rev 3 p.bytes q.bytes)])` because `sha-1l` reads its
`byts` big-endian. Its Hoon body starts with `(rev 3 wid dat)`, and its jet
hint is `~/ %sha1`, so the jet must keep the same contract. An `octs` is a
little-endian atom by the rule in `AGENTS.md`. Nothing in the running kernel
hashes an `octs` in its stored order:

- `shan:sha` takes only an atom, reads it big-endian too, and derives the
  length with `met`, which drops trailing zero bytes.
- `shay:sha` and `shal:sha` are SHA-256 and SHA-512.

Building the reversed canonical bytes directly does not help either. The
reverse of `header ++ nul ++ data` is `rev(data) ++ nul ++ rev(header)`, so the
190 MB reverse of `data` still happens, and only the 8 MB of headers is saved.

**The fix lives outside this project.** It needs a jetted SHA-1 arm that takes
a little-endian atom and an explicit width, in the manner of `++ shay`. That
means a new arm in `sys/hoon.hoon` and a matching jet in Vere, next to
`u3we_shal` in `pkg/noun/jets/e/`. A faster `u3qc_swp` would also help, and
would help every caller of `rev`. Neither is a change this project ships.

So `object-oid` is written up and left alone, which is what the brief asks for
when the answer is a jet.

---

## 4. D4 — proof that nothing broke

### 4a. The two `copy-instruction` arms agree on every copy opcode

A probe compares `copy-instruction` against `copy-instruction-sparse` over
every copy opcode (128 to 255), six source byte patterns, nine source
truncations, and two cursors. It compares the full `(unit [chunk=octs
next=@ud])`, not a hash of it.

```
13,824 cases tried, 0 mismatches
```

The first version of the run form failed **6** of those cases. With a mask that
asks for no bytes at all, `optional-byte` never bounds-checks, so the old arm
succeeds even when the cursor is already past the end of the source. The run
form rejected it. `apply-delta` cannot produce that input, because it calls
`byte-at` on the cursor first, but the two arms are exported and a later caller
would find the difference. The shipped arm only rejects when the mask asks for
at least one byte. That change took the count to zero.

### 4b. The whole pack decodes to the same map

| | objects | object map `mug` |
|---|---:|---|
| before | 9,048 | `0x7b1d6883` |
| after | 9,048 | `0x7b1d6883` |

Both arms were also run side by side on all 6,505 deltas of the pack, with a
full noun comparison of the two results on each one. No mismatch. `0x7b1d6883`
is the same value `QUESTIONS.md` §2e recorded.

### 4c. A repository with deep delta chains round-trips

erpit `master` was pushed into `~bus` with stock `git`, no `-c` flag of any
kind, and cloned back with stock `git`.

The pack `git` sent was recorded off the wire by a proxy between `git` and
Eyre, then indexed with `git index-pack`:

```
pack bytes                3,889,505
objects                       7,369
  non-delta                   2,200
  REF_DELTA                   5,169
deepest delta chain              40
```

`urgit` does not advertise `ofs-delta` for `receive-pack`, so all 5,169 deltas
arrived as `REF_DELTA` and the `%ref` branch of `resolve-pass` carried them.

| check | source | round trip |
|---|---|---|
| `git rev-list --objects \| sort \| sha256sum` | `5cc7922a…f909c438` | **identical** |
| objects | 7,369 | 7,369 |
| every object's type, size and body, `git cat-file --batch` | `8fb13224…cce8569a` | **identical** |
| the 5,169 REF_DELTA objects alone | `30762e02…de1fec5f` | **identical** |
| `git fsck --full` | — | exit 0, no output |

### 4d. Conformance vectors

All 18 generators in `desk/gen` were built out of the running desk with
`.^(vase %ca …)` and slammed in one thread, at the final state of
`git-delta.hoon`. Every one built and ran.

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

The value set matches `PUSH-DEFECTS.md` §4c, the corrected mapping, on all 18
rows. `git-delta-pack-vector` and `git-ofs-delta-pack-vector` are the two that
run the changed arm, and both hold.

---

## 5. D5 — the numbers, before and after

### 5a. Components

| component | before ms | after ms | change |
|---|---:|---:|---|
| `apply-delta` | 1,238.0 | **781.5** | **−36.9%** |
| `object-oid` | 867.7 | 873.7 | unchanged |
| — `canonical-object` | 90.8 | 98.8 | unchanged |
| — `rev` | 590.0 | 591.1 | unchanged |
| — `sha-1l` | 186.9 | 183.8 | unchanged |
| three `~(put by ...)` | 25.6 | 27.6 | unchanged |
| loop walk | 5.0 | 5.3 | unchanged |
| sum of components | 2,136.3 | 1,688.1 | |
| **`resolve-entries`** | **2,196.0** | **1,702.5** | **−22.5%** |
| not attributed | 59.7 (2.7%) | 14.4 (0.8%) | |

The after column comes from the `--loom 32` pier, six samples for
`resolve-entries` and two for each component. The before column is the
`--loom 31` series. `resolve-entries` after the fix reads 1,710.9 ms on the
`--loom 31` pier, so the two looms agree within 0.5%.

### 5b. A whole pack decode

`decode-pack-with` over the same 3,978,266-byte pack, both arms in the same
event, six samples each:

| | before | after |
|---|---:|---:|
| `decode-pack-with` | **2,345.8 ms** | **1,886.7 ms** |
| objects | 9,048 | 9,048 |
| object map `mug` | `0x7b1d6883` | `0x7b1d6883` |

**−19.6%.**

### 5c. Outside the ship

A stock `git push` of erpit `master` into `~bus`, 3,889,505 pack bytes, 7,369
objects, chains to depth 40. No `-c` flag. `/usr/bin/time` on the client. Each
push goes into a fresh repository, and the repository is deleted after the
timing.

| build | wall seconds | median |
|---|---|---:|
| before | 1.86, 1.86, 1.88, 1.88 | **1.87 s** |
| after | 1.58, 1.56, 1.55, 1.55 | **1.56 s** |

**−16.8%.** Two earlier series agree. On the `--loom 31` pier, four pushes each
gave 1.89 s before and 1.56 s after. On the `--loom 32` pier, three pushes each
gave 1.89 s before and 1.56 s after.

The two builds were installed one after the other on the same pier and each was
confirmed by scry before its series ran, so the client is timing the code the
scry reports. The component timing and the external timing move together.

---

## 6. What I could not measure and why

**A clone of a repository with many refs.** `git clone` of the 58-ref erpit
push returns HTTP 400 with `invalid upload-pack request`. The cause is not this
change and not `resolve-entries`. When the upload-pack request body grows past
a threshold, `git` sets `Content-Encoding: gzip` and compresses it.
`parse-upload-request` in `desk/lib/git-protocol.hoon` reads the gzip bytes as
pkt-lines and returns `~`. A proxy recorded the body: 1,438 bytes on the wire,
3,023 bytes after `gzip.decompress`, a well-formed protocol v2 `fetch` command
with 58 `want` lines. Both protocol v0 and v2 fail the same way. The round trip
in §4c uses a one-ref push, whose request stays under the threshold. This is a
new defect on the `upload-pack` path. It is out of scope for this brief and it
is not recorded anywhere else, so it is recorded here.

**A clone of a 148 MB repository in a 2 GB loom.** `upload-pack` bails with
`meme` while it encodes the response pack. The same clone succeeds at
`--loom 32`. Nothing measured how close to the edge it runs, and no ceiling was
found for either side.

**The `%ref` branch, timed.** The fixture pack is all `ofs-delta`, so the
attribution in §1 is an `ofs-delta` attribution and `resolve-entries` needed one
pass over it. The real push in §4c is all `REF_DELTA` with chains to depth 40,
so the `%ref` branch is proven correct by byte identity and it is inside the
external push timing. It was not attributed on its own, and the number of
resolve passes a `REF_DELTA` pack needs was not counted.

**`sha-1l` below the jet.** 186.9 ms over 206.36 MB is 1.10 GB/s. That is fast
enough to suggest hardware SHA-1, but no performance counter was read and the
Vere jet source was not opened. The claim in §3b is about the Hoon contract,
which was read out of the running `sys/hoon/hoon` by scry, not about the C.

**Whether a `?+` over all 16 offset masks beats the run form.** The run form
handles 98.5% of copy instructions in two `cut` calls and falls back for the
rest. A full case split would remove the fallback and might save a little more.
It costs 24 branches of new code. It was not built.

**The remaining 764 ms of `apply-delta`.** After the fix, `apply-delta` is
781.5 ms over 99,603 instructions, or 7.8 µs each. The `slice` calls are 88 ms
of that. The rest is the outer loop, the `byte-at` gate call per instruction,
the `(unit [chunk next])` that `copy-instruction` returns, the list cons, and
the final `join-all`. None of those was isolated, so the next step in this arm
has no measurement behind it yet.

**Memory.** No probe measured allocation. `object-oid` allocates about 412 MB
of throw-away atoms per pack decode, 206 MB for the canonical bytes and 206 MB
for the reversed copy. That number is derived from the byte counts, not read
from the runtime.
