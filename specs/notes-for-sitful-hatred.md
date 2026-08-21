# Notes for ~sitful-hatred — urgit transfer measurements

We measured on two fake piers, Vere 4.6, `brass-408k-1.pill`, ames state `%37`.
The fixture is 27 MiB, 198 objects, 52 commits, random incompressible data.

Kernel cites refer to `sys/vane/ames.hoon` at tag **408k-2** unless marked
otherwise. If you read along on 408k-1, subtract 3 to 8 lines. The two tags
differ by 10 lines in this file and no cited construct changed. See §9.

Your design call looks correct. The numbers below show where the remaining
ceiling is. It is not in your code.

---

## 1. Your 88s divides cleanly against our Fine baseline

We measured the old Fine path over five runs. The median is **131.4s**, or
210 KiB/s, with a 1.31x spread between runs.

| path | ms per 1 KiB fragment | total |
|---|---|---|
| Fine (measured) | 4.75 | 131.4s |
| your Mesa result | 3.18 | 88s |

The 43s you removed is close to the sum of the two costs that Mesa drops:

- publisher re-etch, 8 pages x ~3.4s = **~27s**
- ed25519 per fragment, 0.79ms x 27,644 = **~22s**

131 - 27 - 22 = 82s, against your 88s. You already have nearly all of the
crypto and publisher savings that exist. Little remains in that direction.

---

## 2. The remaining 3.18ms per fragment is one round trip. Mesa's bulk path is stop-and-wait

This is why you are two orders of magnitude below the link rate.

See `hear-page`, the `%data` case, at **10134-10147**:

```hoon
?.  =(+(fag) leaves.los.ps)
  ::  request next fragment
  %-  ev-emit
  %^  give-push  her
    [hop=0 %peek name(wan [%data counter.los.ps])]
```

The receiver takes fragment N, verifies it, then emits the peek for fragment
N+1. One request is outstanding at any time. A sequencing guard above it
(`?. =(counter.los.ps fag) ev-core`) discards out-of-order arrivals, so the
loop cannot advance past the current fragment.

The transfer needs 27,644 fragments and one round trip each. Your 88s divided
by 27,644 is **3.18ms**, which is the round trip itself.

The flow side has the same shape. The kernel decrements `send-window` to zero
before each send (**10980-10986**) and restores it only when the ack arrives
(**11180**). A comment at **11154** states it directly:
`XX we shouldn't see this since send-window is always 1`.

Your target of a few seconds needs about 90us per fragment. A serial loop
cannot reach that at this round-trip time. It needs about **35 requests in
flight**.

---

## 3. Mesa keeps the 1 KiB fragment

Mesa uses `boq=13` at **9949-9951** and **10124-10127**. This is the same size
as Fine's `packet-size 13` at **85**. Mesa changes the authentication scheme,
not the fragment count. 27 MiB is 27,644 fragments on either path.

---

## 4. Crypto was never the main cost. Event overhead is

We timed the jetted `ed:crypto` arms directly on a 1 KiB payload. This needed
no kernel patch, because `sign-raw` and `veri` are ordinary library arms in
`zuse.hoon:1234` and `1260`.

| operation | per call |
|---|---|
| ed25519 sign | 0.762ms |
| ed25519 verify | 0.794ms |

As a cross-check, the isolated sign is 94% of the 0.814ms marginal cost that we
derived from a separate length sweep of Fine's hunk endpoint. Two methods at
different layers agree within 6%.

The receiver spends ~4.6ms per fragment on Fine. Verification takes 0.79ms of
that, or 17%. The other 3.81ms, or 83%, goes to Arvo events, round trips, and
packet handling. Both ships stay idle between events. Source p50 latency held
at 28ms and destination p50 at 20ms through every run.

LSS can therefore recover at most that 17%. The request loop in §2 holds the
rest.

---

## 5. Fine's publisher has no cache. We confirmed this rather than assume it

Each cache miss makes Vere scry `/ax/fine/hunk/<lop>/<len>/<pax>`. That scry
runs `etch-open` = `(etch path hunk (etch-data path data))`. `etch-data` jams
the whole page twice and signs it once. Then `etch` calls `make-meow` ->
`sign-fra`, one ed25519 per requested fragment.

We timed it against a live snapshot:

| fragments requested | time |
|---|---|
| 1 | 351.7ms |
| 512 | 778.2ms |
| 1,024 | 1,176.2ms |
| 2,048 | 2,023.4ms |
| 4,096 | 3,685.1ms |

The cost is linear across two decades: **~351ms fixed plus ~0.814ms per
fragment**.

We repeated the identical `lop` and `len` three times and measured 351.7ms,
372.9ms, and 399.1ms. No Arvo-level cache of the etched hunk exists. Every
request pays the fixed cost again. An earlier analysis had assumed a cache.

A critical-path check confirms the cost lands on the total. Run 1 stalled once
per page and took 105.1s. Runs 3 to 5 stalled twice and took 131s to 137s. One
extra 3.2s stall over 8 pages predicts a 25.6s difference. We observed 26.2s,
or **102%**. The publisher work does not overlap the transport.

**Mesa may inherit this.** On the `%data` path, `build:lss` runs on every
fragment request (**13270-13273**). It rebuilds the Merkle proof over the whole
message, and its `~> %memo` hint is commented out. This is the same
recompute-per-request shape. We did not time it. Check this on your branch
first.

---

## 6. You are correct to remove packing

We measured pack construction at **47.4s of a 47.8s build** for a 28 MB
repository. It produces **no compression at all**. `zlib-store` wraps
`stored-deflate`, which emits DEFLATE *stored* blocks: a header, a length, then
the bytes verbatim. The output is valid zlib and stock git accepts it, but the
size does not drop.

Urgit also cannot compress. Vere ships no compression jet. `strings` on the
binary returns `decompress_zlib` and `decompress_gzip`, and nothing that
compresses. Urgit has no delta encoder either, because `git-delta` applies
deltas and never produces them. A repository born on urgit has no packed form,
and nothing in the system can build one.

The 47.4s is a hand-rolled `adler32` byte loop with no jet hint. Vere already
jets that checksum, and zuse exposes it as `adler32:adler:checksum`
(`zuse.hoon:6504`). Isolated on 544 KB, the jetted arm takes **138us against
431ms interpreted, about 3,100x**. The two agree byte for byte across empty,
single-byte, sub-block, 65,535-byte boundary, and multi-block inputs.

One caveat. We committed that swap on our branch, but its end-to-end effect is
**not yet measured**. Our benchmark pier had a detached desk mount, so the runs
we labeled "fixed" re-ran the baseline. See §8.

Separately, `join-all` folds pieces pairwise through `can 3`, which is
quadratic. The fitted exponent is **2.028** across 24, 99, and 198 pieces. It
costs only 172ms at 198 pieces. The defect is real but too small to matter
here.

---

## 7. Other kernel findings

**`%rate` is unreachable on Fine by construction.** `fi-sub` (**8788**, called
at 6715 and 8731) registers an atom `%sage` interest. `fi-give-rate`
(**8713-8717**) opens with `?@ int f` and skips atom interests. Only `fi-rat`
(**8850**) can create the `[%rate boq feq]` cell interest, and **nothing calls
`fi-rat`**. Every transfer we ran reported `fineFragmentsReceived: 0`.

**`%prog` crashes for ames-core peers.** `pe-prog` at **14008-14013** is a bare
`!!` when `find-peer` returns `%ames`. Legacy Fine is the default, so this is
the common case.

**Legacy Fine is the default on fresh and migrated ships alike.** The vane
assigns `core` in exactly two places. Line **4053** sets `%mesa`. Line **4085**
sets it back to `%ames` on the next migration step, with the slog
`"mesa: turning on %ames for first contact"`. The ladder runs from 31 to 37
unconditionally. A fresh boot lands there too, because `lull.hoon:1620`
declares `core=_`?(%ames %mesa)`%ames` with the comment "always %ames so we can
guarantee communication with past peers". We confirmed this on three piers:
zero chums, every peer tagged `%peer %known`.

A subagent reading the source caught line **4085** after we had concluded the
opposite from **4053** alone. This matters if you reason about which core
serves a `%keen`.

**`cwnd` resets per page on Fine.** `on-keen` writes
`keens (~(put by keens) path *keen-state)` (**6710-6717**), and
`metrics=pump-metrics` is a `keen-state` field (`lull.hoon:1176-1188`) with
`cwnd=_1`. Every new `%keen` restarts slow start. The source makes the reset
certain. We never isolated its magnitude.

---

## 8. One methodology warning

A **detached desk mount discards every edit without reporting an error**. Files
written into `<pier>/<desk>/` never reach Clay. `%kiln-commit` still answers
`%committed`, and the agent keeps serving the old build. Three of our "fixed"
benchmark runs were re-runs of the baseline before we caught this. One of them
was a pack SHA-1 comparison that matched only because both sides ran the same
code.

Verify the running source by scrying it. Do not read the mount.

```hoon
.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/<file>/hoon)
```

Then search the returned text for a token unique to your change. Mounting the
same desk a second time under another name and diffing also exposes the
problem. If an arm's timing improves and total runtime does not, suspect the
build path before you rewrite the analysis.

---

## 9. 408k-2 does not turn Mesa on, and the auto-migration is already enabled

We checked whether the newer kernel changes the default transport. It does not.

The two tags differ by 10 lines in `ames.hoon`. The changes are a
`goad-flow-missing` crash that becomes a trace, one whitespace fix, a dropped
`halt.state` term in a condition, and two Mesa bug fixes: acking a `%leave`
plea on a corked flow (PR 7409) and flushing queued pokes for halted flows
(PR 7411). These fix Mesa. They do not select it.

Everything that sets the transport ceiling is identical in both tags:

| construct | 408k-1 | 408k-2 |
|---|---|---|
| `core %mesa` | 4053 | 4053 |
| `core %ames` (the one that wins) | 4085 | 4085 |
| `ahoy-on=%.y` | 87 | 87 |
| stop-and-wait peek loop | 10132 | 10135 |
| `boq=13` | 9946 | 9949 |
| `send-window is always 1` comment | 11146 | 11154 |
| `++ stay [%37 ...]` | 14461 | 14471 |

We also checked a live ship rather than trust the diff. The moon
`~naprys-nocsyp-dozzod-labbel` runs a base that is byte-identical to the
408k-2 tag. Its `chums/all` scry returns **25 peers, every one tagged `%peer`,
and zero chums**. A production ship on the newer kernel still runs legacy
Fine.

**The open question this raises is better than the pill question.** `ahoy-on`
is `%.y` in both tags, so auto-migration is switched on:
`on-hear-shut-packet` enqueues an `%ahoy` plea through `app/hood` whenever a
legacy peer answers. Our benchmark piers exchanged five full transfers and
gained zero chums. The moon has 25 peers and zero chums.

The migration is enabled and it is not running. If you find out why, Mesa
becomes the default path instead of a manual `|ahoy/prob` for each peer.

---

## What we would test next on your branch

1. **Concurrent paths.** The kernel keys the pending-request map `pit.per` by
   path. Stop-and-wait is therefore per-path. N messages on N distinct paths
   should overlap N independent streams. This is app-level work and needs no
   kernel change. We have not measured it, but §2 gives it a known multiplier.
2. **Time `build:lss` per fragment request** (§5). If Mesa rebuilds the proof
   on each request, that is your next several seconds.
3. **Fragment size.** Every per-fragment cost is paid 27,644 times. It is a
   kernel constant, so a desk cannot ship it, but it sets the real ceiling.
