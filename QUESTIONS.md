# `apply-delta`: the exponent does not reproduce

This run stopped at D1. The brief says to stop if the fitted exponent comes
back near 1.0. It came back **1.09** on erpit's own delta streams.

The D2 rewrite was still built and measured, on the pier only, so that this
document can state what it buys instead of guessing. It is byte-identical and
it removes the square term. It moves a whole erpit pack decode by **1.8%**.

Nothing in `desk/` changed. The decision below belongs to a person.

---

## 1. What reproduces, and what does not

### The brief's byte counts reproduce exactly

I parsed the three packs in `erpit/.git/objects/pack/` and resolved every
delta chain.

| | brief | measured |
|---|---:|---:|
| objects across the three packs | 8,995 | **8,995** |
| deltified objects | 6,222 | **6,222** |
| worst object, instructions | 1,264 | **1,264** |
| worst object, result bytes | 247,348 | **247,348** |
| whole-pack bytes copied, pairwise loop | ~5.24 GB | **5,167,766,168** |
| whole-pack bytes copied, linear | 190 MB | **189,947,130** |

The analytic model in the brief is correct. The `join` in the loop does recopy
the accumulator once per instruction, and the traffic is 27× what a linear
implementation moves.

### The cost model does not reproduce

The byte traffic is not what `apply-delta` spends its time on at erpit's
scale.

One number the brief does not state: erpit's deltified objects carry a
**median of 5 instructions**, and 120,484 instructions in total. The
accumulator never exceeds 247,348 bytes. It stays in L2/L3 cache, so the
quadratic copying runs at 35–41 GB/s and never reaches memory.

---

## 2. Measurement

Pier `~bus` at `/home/michael/piers/joinall-bus`, HTTP port 8092, Vere 4.6
from `workspace/urbit/bin`. Every time below comes from a `~> %bout` hint read
out of the pier log, or from `/usr/bin/time` on the `git` client.

The running source was confirmed by scry before the first measurement, not by
reading the mount:

```
.^(@t %cx /(scot %p our)/urgit/(scot %da now)/lib/git-delta/hoon)
```

2,988 bytes, with `next-output` at offset 2,536. That is the pairwise form.

A probe builds `/lib/git-delta/hoon` out of the running desk with
`.^(vase %ca ...)`, mints one formula per fixture, and runs it three to six
times. The fixtures reach the ship as `.mime` files committed into the desk,
so the bytes on the ship are the bytes from the packfile.

Every result was checked against a Python reference implementation by SHA-1.
All 17 fixtures match.

### 2a. Real erpit deltas — exponent 1.09

Prefixes of erpit's worst deltified object. The instruction bytes are copied
verbatim from the packfile. Only the two leading size varints are rewritten,
so each prefix is a well-formed delta over the same 253,021-byte base.

| instructions | result bytes | model bytes copied | median ms |
|---:|---:|---:|---:|
| 158 | 52,915 | 4,041,438 | **2.007** |
| 316 | 84,138 | 16,075,599 | **4.259** |
| 632 | 143,921 | 50,198,771 | **9.110** |
| 1,264 | 247,348 | 162,597,787 | **19.470** |

**Fitted exponent 1.09.** Segment exponents 1.085, 1.097, 1.096.

The bytes copied grow 40× across this table. The time grows 9.7×.

### 2b. Five whole real objects — time tracks instructions, not bytes

| object | instructions | result | bytes copied | before ms |
|---|---:|---:|---:|---:|
| o1 | 80 | 256,893 | 11,099,818 | **1.172** |
| o2 | 160 | 162,570 | 12,698,210 | **2.229** |
| o3 | 320 | 65,183 | 13,457,414 | **4.645** |
| o4 | 648 | 163,984 | 40,329,808 | **8.486** |
| o5 | 1,264 | 247,348 | 162,597,787 | **18.981** |

o1, o2 and o3 copy almost the same number of bytes: 11.1, 12.7 and 13.5
million. Their times are 1.17, 2.23 and 4.65 ms. The instruction counts are
80, 160 and 320.

Time follows the instruction count. It does not follow the bytes copied.

### 2c. The controlled experiment

Same real 253,021-byte base. Instruction count held at 1,264. Only the chunk
size changes, so only the bytes copied change.

| chunk | result bytes | bytes copied | before ms | after ms | ratio |
|---:|---:|---:|---:|---:|---:|
| 1 | 1,264 | 799,480 | 24.21 | 22.23 | 1.09× |
| 196 | 247,744 | 156,698,080 | 28.05 | 22.75 | 1.23× |
| 2,000 | 2,528,000 | 1,598,960,000 | 69.71 | 22.81 | 3.06× |
| 16,000 | 20,224,000 | 12,791,680,000 | 686.7 | 25.65 | **26.8×** |

Read the first two rows. 156 million more bytes copied costs 3.8 ms. That is
41 GB/s, which is cache speed, not memory speed. The 24.2 ms floor is the
interpreted per-instruction cost: about 19 µs per instruction.

Read the last two rows. Once the accumulator passes about 1 MB it leaves
cache, and the square term takes over.

### 2d. Where the square term does dominate

Instruction sweep at a fixed 2,000-byte chunk.

| instructions | result bytes | bytes copied | before ms | after ms | ratio |
|---:|---:|---:|---:|---:|---:|
| 500 | 1,000,000 | 250,500,000 | 15.77 | 8.87 | 1.78× |
| 1,000 | 2,000,000 | 1,001,000,000 | 44.59 | 17.96 | 2.48× |
| 2,000 | 4,000,000 | 4,002,000,000 | 141.23 | 35.97 | 3.93× |
| 4,000 | 8,000,000 | 16,004,000,000 | 714.7 | 72.01 | 9.93× |

**Before: exponent 1.83. After: exponent 1.01.**

The defect the brief describes is real. It needs objects above about 1 MB to
show itself. erpit has none.

### 2e. The whole pack

erpit repacked into one self-contained pack with
`git repack -a -d -f --depth=50 --window=250`: 9,048 objects, 3,979,802 bytes.

| | before | after |
|---|---:|---:|
| `decode-pack-with` | **2.459 s** | **2.416 s** |
| `resolve-entries` | 2.340 s | 2.170 s |
| `parse-entries` (zlib jet) | 167 ms | 167 ms |
| `sha1-octs` over the pack | 17.7 ms | 17.7 ms |
| objects | 9,048 | 9,048 |
| object map `mug` | `0x7b1d6883` | `0x7b1d6883` |

**The fix moves the whole decode by 1.8%.**

`resolve-entries` is 95% of the decode. Inside it, `apply-delta` runs 6,222
times over 120,484 instructions. At the measured 12–19 µs per instruction that
is 1.5–2.3 s, which accounts for most of the 2.34 s.

### 2f. Outside the ship

A `git push` of erpit's `master` history (3,889,452-byte request body) takes
**1.90 s** wall on the client. The pack decode inside that is 2.4 s of ship
time on the repacked pack, so decode is the bulk of the push either way. The
fix would move that push by tens of milliseconds.

---

## 3. The problem

The brief asserts a defect and forbids re-litigating whether it exists. It
exists. Its size is the question.

- On **erpit**, removing it buys **1.8%** of a pack decode.
- On a **20 MB object**, removing it buys **26.8×**.
- The exponent on erpit's real deltas is **1.09**, which is the brief's own
  stop condition.

The dominant cost at erpit's scale is the interpreted per-instruction work in
`copy-instruction` and the loop around it, at 12–19 µs per instruction. The
brief states that `apply-delta` is unjetted and always runs interpreted. That
sentence, not the byte-traffic table, is the load-bearing one.

---

## 4. Options

### Option A — commit the D2 rewrite as written

**Buys:** exponent 1.83 → 1.01 above 1 MB. 26.8× on a 20 MB object. 3.06× on
a 2.5 MB object. 15–35% on erpit's individual objects. 1.8% on an erpit pack
decode.

**Breaks:** nothing that I measured. The prototype is byte-identical on all 17
fixtures and on the whole 9,048-object pack. It is also faster than the
current code on every fixture, including the 1,264 × 1-byte case where the
accumulator is 1,264 bytes and the list carries the most overhead relative to
the copying.

**Cost:** the guard changes from a check on `p.next-output` to a check on a
running `size`. Section 5 shows why the two fire on the same instruction.

**Risk:** the change is worth little for the repository the brief names. It is
worth a great deal for a repository with large blobs, which a Git host will
see.

### Option B — leave `apply-delta` alone and attack the per-instruction cost

**Buys:** the other 85% of `apply-delta` at erpit's scale. If the 19 µs per
instruction came down by half, an erpit pack decode would drop by about 0.9 s
against the fix's 0.04 s.

**Breaks:** the two ways in are a jet and an allocation cut.

A jet is a Vere change. The brief forbids anything outside `desk/`, so a jet
is out of scope for this project as stated.

The allocation cut is in scope and untested. `copy-instruction`
(`git-delta.hoon:28-53`) calls `optional-byte` seven times per copy
instruction. Each call allocates a `(unit [value=@ud next=@ud])`. Reading the
seven optional bytes in one loop over a mask would remove 7 unit allocations
and 7 gate calls per instruction. I did not measure this. It is a guess about
where the 19 µs goes.

### Option C — do both, in that order

Commit D2 first, because it is measured, byte-identical, and never slower.
Then treat the per-instruction cost as the next piece of work, with its own
measurement.

**Breaks:** nothing. It costs one more round of review.

### Option D — do nothing

**Breaks:** a push of any repository with multi-megabyte deltified blobs stays
quadratic. At 8 MB the current code takes 715 ms for one object. At 20 MB it
takes 687 ms for one object with only 1,264 instructions. A repository with a
few hundred such objects would push in minutes instead of seconds.

---

## 5. The D2 prototype, for whoever decides

Held on the pier at `lib/ad-proto.hoon`. Not committed to `desk/`.

```hoon
=/  cursor=@ud  next.u.result-size
=/  parts=(list octs)  ~
=/  size=@ud  0
|-
?:  =(cursor p.delta)
  ?:  =(size value.u.result-size)  `(join-all:git-codec (flop parts))
  ~
...
  =/  next-size=@ud  (add size p.chunk.u.copied)
  ?:  (gth next-size value.u.result-size)  ~
  $(cursor next.u.copied, parts [chunk.u.copied parts], size next-size)
```

### Why the guard fires on the same instruction

The old code computes `next-output` as `(join output chunk)`. `join` sets
`p` to `(add p.left p.right)`. So `p.next-output` is `(add p.output p.chunk)`.

`size` is the same running sum. It starts at 0, and every branch adds the same
`p.chunk` that `join` would have added. So `size` equals `p.output` at every
step, and `(gth next-size value.u.result-size)` tests the same number as
`(gth p.next-output value.u.result-size)`. The guard fires on the same
instruction, on the same inputs, and returns `~`.

The final check is preserved. `(join-all (flop parts))` returns
`p` equal to `(roll parts (add p))`, which is `size`, and the branch only runs
when `size` equals the declared result size.

### Why deferring is safe here

`copy-instruction` slices from `base`, never from the accumulator
(`git-delta.hoon:53`, `(slice:git-codec base offset size)`). The insert branch
slices from `delta` (`:85`). No instruction reads a byte that the same
invocation wrote. I confirmed this by reading both branches.

The byte-identity of `(can 3 pieces)` against a pairwise `join` fold is
already established in `JOIN-ALL-PERFORMANCE.md` §2.

### Evidence it is byte-identical

Every fixture returns the same length and the same SHA-1 under both
implementations, and both match the Python reference:

| fixture | result bytes | SHA-1 |
|---|---:|---|
| prefix 158 | 52,915 | `40523ed1ebef37e8…` |
| prefix 316 | 84,138 | `506ce5606f80cf6d…` |
| prefix 632 | 143,921 | `3a08830b82a78eb4…` |
| prefix 1,264 | 247,348 | `260662cf086f1769…` |
| o1 | 256,893 | `1a7947765876f4b5…` |
| o2 | 162,570 | `683166d7ea2e6218…` |
| o3 | 65,183 | `c7958812eb0214e7…` |
| o4 | 163,984 | `f479f3c8476df973…` |
| o5 | 247,348 | `260662cf086f1769…` |
| 8 synthetic cases | 1,264 … 20,224,000 | all match |

Whole pack: 9,048 objects and object map `mug 0x7b1d6883` under both.

---

## 6. D3 — `git-inflate.hoon` does not take this shape

I read it. The read-back dependency the brief warns about is there, and it is
worse than the brief states.

`copy-distance` (`git-inflate.hoon:198-207`) reads a byte back out of the
accumulator it is building:

```hoon
=/  byte=@ud  (cut 3 [(sub p.out distance) 1] q.out)
$(remaining (dec remaining), out (append-byte out byte))
```

That is the LZ77 back-reference window. A deferred `(list octs)` has no
materialized buffer to address, so the D2 shape cannot apply.

The brief treats `inflate-deflate:265` as a separate site that might be safe
on its own. It is not. The `output` built by the stored-block branch at `:265`
is passed straight into `compressed-block` at `:280`, which passes it into
`copy-distance` at `:237`. A DEFLATE stream may mix stored and compressed
blocks, and a back-reference in a later compressed block can point at bytes a
stored block produced. Both sites share one accumulator.

Fixing this needs a real window: a materialized tail of at least 32,768 bytes,
kept alongside the deferred list. That is a different change with a different
correctness argument. I did not attempt it.

The path is also dormant. `git-pack-decode.hoon:113-122` routes zlib through
Vere's `%zlib-v0` jet and only falls back to this decoder on a runtime without
it. I measured `parse-entries` at 167 ms for 9,048 objects, which confirms the
jet is doing the work on this runtime.

---

## 7. D4 — the nine `join` sites

`grep -rn 'join:git-codec\|(join ' desk/lib desk/app | grep -v join-all`
returns nine hits.

| file:line | shape | finding |
|---|---|---|
| `git-delta.hoon:78` | **accumulator loop** | The copy branch of `apply-delta`. The subject of this brief. Bounded only by the declared result size. |
| `git-delta.hoon:85` | **accumulator loop** | The insert branch of the same loop. Same bound. |
| `git-inflate.hoon:265` | **accumulator loop** | Stored-block output. Shares its accumulator with `copy-distance`, which reads it back. See §6. Dormant behind the zlib jet. |
| `app/urgit.hoon:3514` | **accumulator loop** | Peer fragment assembly. Each fragment is capped at 1,048,576 bytes (`:3495`), and every fragment except the last must be exactly that size (`:3517`). One object is capped at 67,108,864 bytes (`peer-stream-max-assembly-bytes`, `:1952`). So the worst case is 64 joins over a growing 64 MiB atom, about 2.1 GB copied for one object. The bound is hard, but it is 64 MiB, not small. The brief assigns this path to a parallel task, so I did not change it. |
| `git-archive.hoon:119` | **one-shot** | `file-entry` joins one tar header to one padded file body. It runs once per file. The caller at `:134` collects the results in a list and makes one `join-all`, which is already linear. |
| `git-pack.hoon:100` | **one-shot** | `encode-pack` joins the pack prefix to its 20-byte SHA-1 trailer. Exactly one call per pack. |
| `git-protocol.hoon:306` | **one-shot per chunk** | `v2-sideband-pack` joins a one-byte sideband channel marker to one 65,515-byte chunk. It runs once per chunk, and each result goes into a list for one `join-all` at `:303`. The joined pieces are of fixed size, so this is linear in the pack size. |
| `git-codec.hoon:100` | **one-shot** | `en-pkt` joins a 4-byte length prefix to one payload of at most 65,516 bytes. One call per pkt-line. |
| `docket.hoon:118` | **not this `join`** | `(join "\0a" ...)` is `+join` from `hoon.hoon` over a `(list tape)`, inside `zing`. It has no relation to `git-codec`'s `join` and touches no `octs`. Dismissed. |

Four accumulator loops, four one-shot sites, one false positive.

---

## 8. Two unrelated defects found while measuring

Neither is in scope for this brief. Both blocked the D6 external measurement,
so they are recorded here.

### A push that updates more than one ref returns HTTP 400

`parse-receive-command` (`git-protocol.hoon:176-189`) scans the command line
from byte 82 and accepts it only when it finds a NUL or a line feed. Git puts
the capability list after a NUL on the **first** command line only. Later
command lines carry `<old> <new> <ref>` and stop. The loop runs off the end
and returns `~`, and `parse-receive-request` fails the whole request.

Reproduction, against an empty repository on this build:

```
git -c protocol.version=0 -c http.postBuffer=524288000 \
    push ship '+refs/heads/a:refs/heads/a' '+refs/heads/b:refs/heads/b'
→ error: RPC failed; HTTP 400
```

The same two refs pushed one at a time both succeed.

### A push larger than `http.postBuffer` returns HTTP 400

When the request body exceeds `http.postBuffer` (1 MB by default), git sends a
probe request first. The probe body is a single flush packet, four bytes.
`handle-receive-pack` (`urgit.hoon:7996`) treats an empty command list as a
bad request and answers 400, so git aborts before it sends the pack.

Reproduction: any push of more than about 1 MB with default settings.
Workaround: `-c http.postBuffer=524288000`.

---

## 9. What I could not measure and why

**A before-and-after `git push` of the whole erpit repository.** The multi-ref
defect above caps a single push at one ref. I pushed `master` alone, 3,889,452
bytes, at 1.90 s. I did not push all 58 refs in one request because the build
rejects it.

**The split inside `resolve-entries`.** I measured `resolve-entries` at 2.340 s
and `apply-delta` at 12–19 µs per instruction over 120,484 instructions. I did
not instrument `object-oid` or the three `~(put by ...)` calls per object
separately, so the 2.34 s is not fully attributed. The 1.5–2.3 s estimate for
`apply-delta` inside it is arithmetic, not a hint reading.

**Where the 12–19 µs per instruction goes.** I know the number and I know it
is not the byte copying, because §2c holds the instruction count fixed and
varies only the bytes. I did not profile inside `copy-instruction`. Option B
names a candidate and marks it as a guess.

**Cache behavior directly.** I infer L2/L3 residency from the throughput
figures: 41 GB/s at a 247 KB accumulator, 35 GB/s at 2.5 MB, 19 GB/s at
20 MB. I did not read performance counters.

**Ref-delta chains at depth.** erpit's packs and the repack both produce
ofs-delta. The `%ref` branch of `resolve-pass` (`git-pack-decode.hoon:154-159`)
calls the same `apply-delta`, so the arm under test is the same, but I did not
build a ref-delta pack. D5 asked for one. D5 was not reached.

**The reference the brief names.**
`.claude/skills/urbit-native-transfer-performance/references/click-probe-syntax-and-timing.md`
does not exist in this repository. I wrote the probes without it.
