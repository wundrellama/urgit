# Upstream findings — Ames/Vere issues outside urgit's control

These were measured while profiling urgit's native fork transfer. **None of them
are urgit work items.** urgit ships as a desk installed with `|install`; it cannot
carry kernel or runtime changes. Each entry here is a candidate upstream bug report
or a constraint to design around.

Measured 2026-08-20 on Vere 4.6, `brass-408k-1.pill`, Ames state `%37`, two fake
piers, 27 MiB / 198-object / 52-commit fixture, five runs. Full evidence in
`THROUGHPUT-PHASE1.md` and `bench/phase1/`.

---

## 1. No cache of the etched Fine hunk on the publisher

**Severity: high.** The single largest measured cost in a transfer.

Each Fine cache miss makes Vere scry `/ax/fine/hunk/<lop>/<len>/<pax>`, which runs
`etch-open` = `(etch path hunk (etch-data path data))`. `etch-data` jams the whole
page twice and signs it once; `etch` then calls `make-meow` → `sign-fra`, one
ed25519 per requested fragment.

Timed directly against a live snapshot (`bench/phase1/hunk-timings.txt`):

| fragments requested | time |
|---|---|
| 1 | 351.7 ms |
| 512 | 778.2 ms |
| 1,024 | 1,176.2 ms |
| 2,048 | 2,023.4 ms |
| 4,096 | 3,685.1 ms |

Linear across two decades: **~351 ms fixed + ~0.814 ms per 1 KiB fragment.**

**Repeating the identical `lop`/`len` request costs the same** — 351.7 / 372.9 /
399.1 ms across attempts. There is no Arvo-level cache of the etched hunk; every
request re-pays the fixed cost in full.

Effect on a 27 MiB transfer: the source serf blocks 4.2–7.4 s per page, out of a
16.96 s median page. Two independent instruments agree — a `%bout` on a hand-issued
`%hunk` scry gives 3.685 s for a whole-page request, and an HTTP health probe
independently sees ~3.2 s source stalls on the page cadence.

The publisher's block *count* per page tracks total run time across all five runs:
run 1 had one block per page and took 105.14 s; runs 3–5 had two and took
131–137 s. One extra 3.2 s stall × 8 pages predicts a 25.6 s delta; observed
26.24 s — **102% agreement**. The blocking is on the critical path.

**Ask:** cache the etched packet list per `(path, revision)` for the snapshot's
lifetime, or make `etch` incremental so a miss costs one fragment rather than a
3.82 MB double-jam plus a whole-page sign.

---

## 2. The 1 KiB Fine fragment is the dominant per-unit cost, and it is not crypto

**Severity: high.** Decides the whole optimisation program.

`packet-size 13` (`ames.hoon:85`) and `response-size 13` (`324`) make the transfer
unit 1,024 bytes. A 27 MiB repository is **27,644 fragments**, each carrying a
signature out, a verification in, and a receiver Arvo event.

Measured Arvo event counts for one transfer: **source 34, receiver 39,461** — about
1.4 events per fragment on the receiver, ~4.6 ms each. Vere answers Fine cache hits
without waking the publisher's serf, which is why the two counts differ so wildly.

The crypto/overhead split was measured directly, with **no kernel modification** —
`sign-raw` and `veri` in `ed:crypto` are ordinary jetted library arms
(`zuse.hoon:1234`, `1260`; jets `_135_hex_coed__ed_sign_raw_a`,
`_135_hex_coed__ed_veri_a`), callable from a plain thread with `%bout`. 200
iterations on a 1 KiB payload:

| operation | per op |
|---|---|
| ed25519 sign | **0.762 ms** |
| ed25519 verify | **0.794 ms** |

Cross-check: the isolated sign (0.762 ms) is 94% of the 0.814 ms marginal cost
derived independently from the `%hunk` length sweep. Two methods, different layers,
agreeing within 6%.

**Therefore, of the receiver's ~4.6 ms per fragment:**

| component | ms | share |
|---|---|---|
| ed25519 verify | 0.79 | **17%** |
| Arvo event + round trip + packet handling | **3.81** | **83%** |

**This settles the LSS-versus-fragment-size question.** Replacing per-fragment
ed25519 with LSS Merkle authentication can recover at most ~17% of the per-fragment
term. Raising the fragment size attacks all of it.

Note the receiver is **not saturated** — destination p50 latency held at 19–23 ms
throughout, p95 35–62 ms. It is idle between events, waiting, not computing.

**Ask:** raise `packet-size` above 1 KiB, or otherwise reduce the per-fragment
round-trip/event cost. LSS alone is not the answer.

---

## 3. `pump-metrics` lives inside `keen-state`, so `cwnd` resets every page

**Severity: unknown — structurally certain, magnitude never isolated.**

`on-keen` writes `keens (~(put by keens) path *keen-state)` (`ames.hoon:6710-6717`)
and `metrics=pump-metrics` is a `keen-state` field (`lull.hoon:1176-1188`) with
`cwnd=_1`, `ssthresh=_10.000`, `rtt=_~s1` (`ames.hoon:1304-1313`). Every new `%keen`
— i.e. every page — restarts slow start.

The reset is certain from source. Its cost in seconds is not: `on-ack` grows `cwnd`
per ack, so the ramp is ack-paced, and `cwnd` was never observed. `show:fi-gauge`
(`ames.hoon:9070-9080`) prints only behind `fin.veb`/`ges.veb` verbosity.

**Ask:** hoist `pump-metrics` to per-peer so consecutive `%keen`s to the same ship
do not each restart slow start.

---

## 4. `%rate` progress gifts are unreachable on the Fine path

**Severity: medium — blocks instrumentation, not throughput.**

`grep 'fi-rat|fi-sub'` over `ames.hoon` returns four lines: `fi-sub` defined at 8788
and called at 6715 and 8731; **`fi-rat` defined at 8850 and called nowhere.**

- `%keen` → `on-keen` → `fi-sub`, which registers an **atom** `%sage` interest.
- `fi-give-rate` (`8713-8717`) opens `?@  int  f`, skipping atom interests — so
  `%sage` listeners never receive `%rate`.
- The `[%rate boq feq]` cell interest that would receive it can only be created by
  `fi-rat`, which nothing calls.

`%rate` is therefore unreachable on Fine **by construction**, not merely unused.
Confirmed live: every transfer in six runs reported `fineFragmentsReceived: 0`,
`fineFragmentsTotal: 0`.

The Mesa equivalent, `ev-add-rate` (`ames.hoon:10435`), is reached from `pe-prog` —
see item 5.

**Ask:** call `fi-rat` from somewhere reachable, or document `%rate` as Mesa-only.

**urgit consequence:** the `[%peer %rate @ @ ~]` wire arm at `app/urgit.hoon:8410-8428`
is dead code on this path. Leave it; there is nothing to wire it to.

---

## 5. `%prog` crashes the Ames event for ames-core peers

**Severity: medium — a bare crash on a supported task.**

`pe-prog` (`ames.hoon:14008-14013`):

```hoon
=/  ship-state  (find-peer ship.spar)
?:  ?=(%ames -.ship-state)
  ::  XX support |ames
  ::
  !!
```

Any app sending `%prog` for a peer on the legacy core crashes the event. Since
legacy Fine is the **default** transport (item 6), this is the common case, not an
edge case.

**Ask:** implement `%prog` for ames-core peers, or make it a no-op rather than a
crash.

---

## 6. Legacy Fine is the default transport on fresh and migrated ships alike

**Not a bug — a constraint worth recording, because it is easy to assume otherwise.**

`%keen` routes on `find-peer` (`ames.hoon:4576-4585`): `chums` → Mesa, `peers` →
Fine, neither → `core.ames-state`. The dispatch in `pe-keen` (`13810-13814`) tests
the **head tag only**, so `[%ames ~]` — a peer in neither map — also takes Fine.

There are exactly two assignments to `core` in `ames.hoon`:

- `4053` — `state-30-to-31` sets `core %mesa`
- `4085` — `state-31-to-32` sets `core %ames` **back**, slog
  `"mesa: turning on %ames for first contact"`

The ladder runs 31→32→…→37 unconditionally (`3425-3437`); nothing after 32 touches
`core`. A fresh boot lands there too — the `axle` type declares its own default
(`lull.hoon:1620`):

```hoon
core=_`?(%ames %mesa)`%ames         ::  default network core protocol
                                    ::  (always %ames so we can guarantee
                                    ::   communication with past peers)
```

Measured: `chums.ames-state` was empty on both benchmark piers and on a fresh-boot
control, before and after five transfers; every counterparty `%peer %known`. A
patched-pier `[%core ~]` scry read `%ames` directly on two fresh piers.

**Consequence for urgit: the app cannot choose its transport.** There is no "use
Mesa" switch available to a desk. `%rege` (`ames.hoon:12182-12216`) moves peers *out*
of `chums` into `peers` — a downgrade. Migration in the other direction is an
**operator** action from the dojo (`|ahoy/prob`, `|ahoy/comb` — see item 7), not
something urgit can carry or trigger. Any "switch urgit to Mesa" plan is a
deployment procedure, not an urgit feature.

**Ask:** none. Recorded so the constraint is not rediscovered.

---

## 7. Mesa may inherit the same recompute-per-request shape

**Severity: unknown — source-read only, never timed.**

On the Mesa `%data` path, `build:lss` runs on **every fragment request**
(`ames.hoon:13270-13273`), rebuilding the Merkle proof over the whole message, with
its `~> %memo` memoization hint **commented out** — the same shape as Fine's
uncached `etch-data` (item 1), and the same commented-out-instrument pattern.

Mesa's fragment size is also **identical** to Fine's: `boq=13`, 1,024 bytes
(`ames.hoon:9949-9951`, `10124-10127`). Mesa changes the authentication scheme, not
the fragment count.

Combined with item 2 — crypto is only 17% of the per-fragment cost — the expected
Mesa win is modest, not transformative. **Unmeasured.** Worth measuring before
anyone plans around "Mesa is faster."

An operator *can* migrate peers from the dojo: `|ahoy/prob ~ship` for one
(`=force-test %.n` to leave dry-run), `|ahoy/comb` for all, with `+mesa-peer ~ship`
to inspect the result. Generators at `base/gen/hood/ahoy/{prob,comb}.hoon`, driven by
`base/lib/hood/ahoy.hoon`. Both default to dry-run, which is itself a signal about
confidence. A half-completed migration leaves the two ships on opposite sides of the
protocol and unable to communicate; recovery requires `|pass [%a %rege ~ship
dry=%.n]` on the **other** ship (`lib/hood/ahoy.hoon:396-399`). Disposable piers
only.

---

## Recording discipline

Everything above was measured without modifying the source kernel tree. The
`ames.hoon` under `moons/naprys-nocsyp-dozzod-labbel/base/sys/` is byte-unchanged;
one instrumentation scry (`[%core ~]`) was applied only to disposable fake piers that
have since been deleted, and item 2's crypto split needed no patch at all.

Future profiling should keep that property. If a measurement genuinely requires a
kernel change, it belongs here as an unanswered question, not in a work item.
