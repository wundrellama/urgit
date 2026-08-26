# %archive-only serving: large forks are opaque, and progress is not fixable in the desk

Filed against `master` @ `5cc8fc1`.

## Summary

`bf53366` ("fallback behavior uses same transfer pattern") removed the `%pack`
and `%objects` serving paths, leaving `%archive` as the only way a repository is
served (`app/urgit.hoon:3283` is the sole serve arm). A fork is now a single
Ames poke carrying the entire object list.

For a 192 MB / 8,902-object repository this produces a **33-minute window in
which a healthy transfer and a dead one report identical status**. There is no
progress, no partial state, and no resumption.

**Sub-page progress on this path cannot be implemented from a desk.** That was
settled by a dedicated investigation, summarised below; it is a runtime
limitation, not something to be fixed here. What the desk *can* do is narrower
and worth doing anyway.

## Measured

Live ships, urgit at `5cc8fc1` on all three.

| transfer | path | size | wall clock |
|---|---|---|---|
| `cast`, moon→moon | loopback | 322 objects | ~11 s |
| `erpit-test`, moon→moon | loopback | 8,987 obj / 203 MB | ~21 min |
| `erpit`, planet→moon | 1 GbE LAN | 8,902 obj / 192 MB | **~33 min** |

192 MB over 1 GbE is ~1.6 s of wire time. **Over 99.9% of elapsed time is CPU
work on the noun**, not transmission. The network is not the bottleneck.

All three completed and verified: object, commit, branch and tag counts and HEAD
identical on both sides.

### Chunked serving, measured against the same repository

Restoring the `%objects`/`%pack` path (this branch) and forking the *same*
`erpit` repository — 9,259 objects, 636 commits — between the same moon pair,
`~racsun-ranrem-dinnyt-divsud` → `~locdut-sanwex-dinnyt-divsud`, on the same
build. The receiving repository was deleted between runs so neither run could
benefit from the `haves` optimisation.

The only variable changed between the two chunked runs is the Ames transport:
the second was preceded by `|ahoy` (`%ahoy-prob`), migrating the pair from
legacy `peers` to Mesa `chums`. Transport was confirmed *after* each run by
scrying `/chums`, not inferred from urgit's own stage label — urgit calls its
chunked path `fine` internally (`peer-fine-name`) regardless of which kernel
transport carries it.

| transport | wall clock | mean rate | serving-ship latency (median) |
|---|---|---|---|
| legacy Ames (`peers`) | **1,770 s** | ~5 obj/s | **2.201 s** (max 9.695) |
| Mesa (`chums`), run 1 | **78.7 s** | ~118 obj/s | 0.300 s (max 0.969) |
| Mesa (`chums`), run 2 | **83.6 s** | ~111 obj/s | **0.440 s** (max 0.867) |

The two Mesa runs are 6.2% apart — well inside the 1.31× noise floor established
for this fixture — for a mean of **81.2 s**, or **21.8× faster** than the same
chunked code over legacy Ames.

For reference, the serving ship's idle baseline on the same endpoint is
0.292 s; see "Serving-ship responsiveness" below.

**21.8× faster over Mesa** at the mean of two runs. All three transfers
completed and verified identically: 9,259 objects, 636 commits,
`refs/heads/master` on the receiver.

The *shape* differs as much as the total. Over legacy Ames the transfer spent
most of its life stalled — flat stretches of 20+ seconds broken by small jumps —
matching the finding that Fine's publisher rebuilds and re-signs each page per
request (~351 ms fixed + ~0.814 ms/fragment) with no Arvo-level cache. Over Mesa
it held 100–240 obj/s with two brief pauses.

### Serving-ship responsiveness

Sampled live during each transfer by timing `GET /apps/urgit/api/repositories`
against the serving ship, and compared against that **same ship's idle
baseline** rather than against the receiver.

The cross-ship comparison is not valid and is not used here: the endpoint's cost
scales with how many repositories the ship holds, so an empty receiver answers
far faster than a loaded source for reasons unrelated to transfer load. Measured
idle with both ships holding `erpit`, they are indistinguishable — `~racsun`
0.285–0.302 s, `~locdut` 0.288–0.300 s. Any earlier source-versus-receiver ratio
was measuring repository content, not responsiveness.

| `~racsun` (serving ship) | median | max | n |
|---|---|---|---|
| idle, holding `erpit` (control) | 0.292 s | 0.293 s | 8 |
| during chunked transfer over **Mesa** | 0.440 s | 0.867 s | 22 |
| during chunked transfer over **legacy Ames** | 2.201 s | 9.695 s | 5 |

Over Mesa the serving ship runs at **1.51× its own idle baseline** — measurably
loaded, but still answering in well under a second, and its worst sample
(0.867 s) is better than legacy Ames's *median*. Over legacy Ames the same ship
sits around 7.5× its baseline with spikes past nine seconds; other agents queue
behind those unbounded Gall events, and for ~30 minutes the ship is effectively
unavailable.

The distinction worth drawing is not "loaded versus idle" — chunked serving is
real work and it costs something. It is that the cost stays bounded and the ship
stays interactive.

*Caveat: the idle control and the Mesa figures come from run 2, which sampled
every tick. Run 1 sampled every fifth tick (n=5, median 0.300 s) and its lower
median is a small-sample artifact, not a faster run. The legacy-Ames figure is
also n=5 and should be read as "seconds, not milliseconds" rather than as a
precise median.*

CPU across the two Mesa runs:

| | source (`~racsun`) | receiver (`~locdut`) | wall |
|---|---|---|---|
| run 1 | +34.1 s | +41.4 s | 78.7 s |
| run 2 | +39.4 s | +38.7 s | 83.6 s |

Both ships spend roughly half a core, and neither is consistently the heavier
side — run 1 favoured the receiver by 7.3 s, run 2 the source by 0.7 s. The
useful conclusion is the symmetry: under chunked serving the work is shared
rather than piled onto the ship that owns the repository. (An earlier reading of
run 1 alone suggested the receiver systematically burns more CPU than the sender;
run 2 does not support that, and it is withdrawn.) The equivalent figure for the
legacy-Ames run was not captured.

### What this establishes

Transport choice determines whether the serving ship stays *usable*, not merely
how fast the transfer is:

- **`%archive`** — receiver blind for 45+ minutes, route starves, never
  completed;
- **chunked over legacy Ames** — completes in 1,770 s, progress visible and
  honest, but the serving ship sits at ~7.5× its idle latency for ~30 minutes;
- **chunked over Mesa** — 81.2 s mean of two runs, progress visible, serving ship
  at 1.51× idle and still sub-second throughout.

Chunked serving is the only path that is simultaneously fast, observable, and
non-disruptive to the ship doing the serving. `bf53366` traded all three for a
single `%plea`.

Note that the transport still matters underneath it: chunked-over-legacy-Ames
works and is honest about its progress, but costs the operator their source ship
for half an hour. Migrating a pair to Mesa is part of the remedy, not optional
polish — see "Negotiating Mesa before a fork" below.

## Defect 1 — a transfer in flight is indistinguishable from a dead one

`%archive` is one atomic message. `received` is written only in `peer-archive`
(`app/urgit.hoon:3426`), which runs *after* the whole noun has arrived. For the
entire transfer the API returns:

```json
"received": 0, "expectedBytes": 201738748,
"completedPages": 0, "pages": 1,
"fineFragmentsReceived": 0, "fineFragmentsTotal": 0
```

Every progress field is structurally pinned at zero, then jumps to complete.

**Observed consequence:** during this investigation two healthy transfers were
cancelled at 24 and 54 minutes because there was no way to tell they were
progressing. The only reliable signal found was *cumulative* process CPU-time
observed from outside the ship — and note that `top -bn1` samples instantaneously
and reads 0% in the gaps between events, which looks exactly like a dead ship.
That false signal drove several wrong conclusions.

### Why sub-page progress cannot be fixed here

A prior run (`briefs/archive-progress-tier3.md`) established this conclusively
on a live chum pair, with a negative control:

- A `%prog` subscription on a Mesa chum **does** produce a `%rate` gift, but it
  carries `rate = ~` — no `boq`, no `fag`, no `tot`. One gift, at completion.
- With `feq=1` on a 24.7-second transfer, correct behaviour would be ~22,000
  gifts. One arrived.
- `+ev-give-sage` (`ames.hoon:10396`) hard-codes the empty `~`. The arm carrying
  numbers, `+ev-give-rate`, is reached only from the `%rate` **task**, which
  `lull.hoon:891` marks *"from unix"* and Vere 4.6 never sends (mote
  `0x65746172`: **0** occurrences in the binary).
- `mesa.c` reassembles ~22,344 fragments in C and hands Arvo one message. Arvo
  never sees a fragment, so no vane change can count them either.

Filed separately as `UPSTREAM-RATE-DEFECTS.md`, together with a latent crash:
`+ev-add-rate` stores `boq=0` while `+ev-give-rate` asserts on a `boq` match,
so a runtime that starts sending real `%rate` will bail the Ames event.

### What the desk *can* do

Not a fraction — a **liveness signal**. Distinguishing "working" from "wedged"
is most of the value and needs no rate data:

- a serving-side heartbeat while the object list is being built, so `active`
  means something;
- a receive-side timestamp updated on any inbound activity for the transfer,
  which lets the UI say "last activity 4s ago" instead of showing a static 0;
- surfacing the sender's `sent` flag (already in state, visible via
  `+dbug %state`) through the API, so the receiver can distinguish "peer is
  still building" from "peer has sent, we are receiving".

Each is honest about what it knows. None requires fragment counts.

## Defect 2 — `peer-release` reports success unconditionally

`app/urgit.hoon:3416`:

```hoon
(peer-activity-finish (peer-serve-activity-id transfer) %.y 'repository snapshot delivered')
```

`%.y` is hardcoded. `peer-release` fires on cancel and failure as well as on
completion — `peer-snapshot-fail` (`:3455`) sends `%release` at `:3466` — so the
serving ship logs "repository snapshot delivered" for transfers that failed.

Observed: a receiver cancelled a transfer at 18:25:19; the serving ship recorded
**success** for that same transfer at 18:25:19. Given defect 1, the activity log
is the operator's main evidence, and here it is actively wrong.

## Defect 3 — the fork dialog cancels in-flight transfers on unmount

`fe/src/components/ForkPeer.jsx:21`:

```js
useEffect(() => () => {
  clearInterval(transferPoll.current)
  clearInterval(discoveryPoll.current)
  if (activeTransfer.current) api.peerDeleteTransfer(activeTransfer.current).catch(() => {})
  if (activeDiscovery.current) api.peerDeleteDiscovery(activeDiscovery.current).catch(() => {})
}, [])
```

A React unmount issues `DELETE /peer/transfers`, killing a transfer the ships
were handling correctly. Navigating away, closing the modal, or a tab teardown
is enough.

Combined with defect 1, this means the tab must stay mounted for half an hour or
the work is silently discarded — and the user has no way to know the transfer was
healthy. Cancel should be an explicit user action only.

## Defect 4 — `peer-catalog-request` can crash and strand a flow

Observed on a live planet via `+ames/stale-flows, =veb %21`:

```
#1 flows for %urgit on ~sarlev to %urgit at /peer/catalog-request/0v2.en9lr…
```

A nacked poke on the discovery path: the handler crashed and the flow is stuck
open. (That ship reports `#[out=1.044 closing=0]` nacked pokes overall — none
closing.) The user-visible symptom is "peer discovery timed out" after `~s30`
(`:5444`), with no indication that the remote agent crashed.

## Minor — redundant materialisation on the hot path

`app/urgit.hoon:3451`, inside `peer-archive`:

```hoon
=/  incoming-map=(map oid:git object:git)  (malt incoming)
?.  =(count (lent ~(tap by incoming-map)))
```

`(lent ~(tap by …))` materialises the entire 8,902-entry map into a list purely
to count it, at the point of peak memory pressure. `~(wyt by incoming-map)`
gives the same number without the allocation.

## On the design

`peer-archive-accept` (`:3377`) sends `objects.flight` in one `peer-card`, which
is a plain `%poke`. The sender must materialise the full object list; the
receiver must cue it, then validate with `levy` over every object, `malt` into a
map, and count as above.

A single dropped message means restarting a 33-minute transfer, and there is no
resumption mechanism. Worth asking whether `%pack`/`%objects` should have been
removed outright rather than retained as a fallback above some size threshold —
the receive-side scaffolding (`peer-stream-next` `:3294`, the `%grow`/`%keen`
paths, six live `%objects` mode checks) is still present, so restoring a chunked
serving path would not start from zero.

**Resolved: it did not.** The chunked path has since been restored on
`feat/restore-chunked-serving` and measured (see "Chunked serving, measured
against the same repository" above) — 78.7 s versus a 45-minute `%archive`
transfer that never completed, with honest progress and a responsive source
ship throughout.

## Negotiating Mesa before a fork

The measurements above make Mesa worth having before a large fork starts. The
question is whether urgit can arrange that itself rather than requiring the
operator to run `|ahoy` by hand.

**The mechanism already exists in the kernel, and it is already switched on.**

Ames pokes `%hood` with `%ahoy-prob` on its own, without any application asking
it to. `+poke-send-ahoy` (`ames.hoon:548`) emits exactly the poke this
investigation issued by hand:

```hoon
[%g %deal [our our /ames] %hood %poke ahoy-prob+!>([who^test-migration])]
```

It fires from two places: when a legacy-Ames peer is observed responding
(`ames.hoon:5973`, gated on `ahoy-on`, which is hardcoded `%.y` at
`ames.hoon:87`), and when a migrated peer sends legacy packets and has to be
moved back (`ames.hoon:13165`). The inbound direction is likewise automatic —
`%ahoy` pleas are acked and acted on by `+migrate-peer` (`ames.hoon:5190`),
which runs `?> (on-mate-test ship)` first so a migration that would crash is
refused rather than attempted.

So "let peers opt me in" is not a feature to be built. It is on by default in
the kernel, with a safety check in front of it.

**Why it nonetheless never fires: the `last-hash` gate.**

`lib/hood/ahoy.hoon` gates migration on the *peer's* `%kids` desk hash matching
`last-hash`, which ships as the 409k-2 release value. Measured on `~racsun`:

| | value |
|---|---|
| shipped `last-hash` default | `0xd665.91cf…4caa8` |
| this ship's actual `%kids` hash | `0x4d5b.a4af…110df` |
| match | **no** |

`ted/prob.hoon` walks the peer's kids cases by remote scry, compares each to
`wait-hash`, and on mismatch loops to the next case until it times out.
`+take-thread` then declines to migrate. Nothing is logged at default verbosity
and the originating poke still reports success.

The consequence is that on any ship whose `%kids` desk did not come from the
official 409k-2 release — a moon syncing kids from its own parent, as here, or
any ship on a custom kernel — **automatic Mesa negotiation is silently disabled
for every peer**. That is the whole defect. It is not that the mechanism is
missing; it is that the mechanism's precondition is false and says nothing.

**What is actually worth doing.**

The obvious fix — point the gate at your own kids hash —

```hoon
=/  h  .^(@uvi %cz /(scot %p our)/kids/(scot %da now))
(poke [our %hood] %ahoy-set-hash !>(h))
```

— is **wrong**, and the reason it is wrong is the more interesting finding.

`ted/prob.hoon` compares with `=(wait-hash has)`: exact equality against a
content hash, walking the peer's kids revisions one at a time and looping to
`+(case)` on any mismatch. A content hash has no ordering. It answers "is the
peer's kids desk byte-identical to this specific value", not "is the peer new
enough to speak Mesa" — which is the only question that matters for migration.

Measured on `~racsun`, whose `%kids` has two revisions:

| rev | `sys/kelvin` | kids hash |
|---|---|---|
| 1 | `[%zuse 408]` | `0xfd02.a770…f56c` |
| 2 | `[%zuse 408]` | `0x4d5b.a4af…110df` |

**Same kelvin, different hash.** Two ships both perfectly capable of Mesa, both
at zuse 408, will refuse to migrate with each other because one of them has an
extra commit in `%kids`. Setting the gate to your own hash does not fix this —
it re-specialises the failure onto your fleet instead of upstream's:

- peer's kids is **newer** than yours → hash differs → no migration, even though
  a newer kernel is strictly more likely to support Mesa;
- peer's kids is **older but still Mesa-capable** → hash differs → no migration;
- both new enough, yours newer → hash differs → no migration;
- peer synced kids from a different sponsor, same kelvin → hash differs → no
  migration.

The thread's case-walking loop half-anticipates this — it scans *backwards*
through revisions looking for a match — but that only helps if the peer at some
point held the exact revision you are looking for. Two ships that took different
paths to the same kelvin never match at any case.

So the correct gate is a **capability predicate, not an identity check**: does
the peer's kids desk carry a kernel whose `sys/kelvin` is at or below the
Mesa-supporting threshold? That is orderable, tolerates version skew in both
directions, and is what `|ahoy` appears to have meant all along. `sys/kelvin` is
readable at any case over the same remote-scry path the thread already uses
(`/c/x/<case>/kids/sys/kelvin` alongside the existing `/c/z/<case>/kids`), so the
information is one extra scry away.

Fixing that is upstream's call, in `lib/hood/ahoy.hoon` and `ted/prob.hoon`. It
is not urgit's, and it is emphatically not something to paper over with a
per-application hash poke.

Until it is fixed, `%ahoy-set-hash` remains the operator's escape hatch: set both
ships to a hash they genuinely share, or migrate by hand. That is what was done
for the measurements in this document.

For urgit itself, the honest scope is read-only. Before a fork, scry:

```hoon
.^(? %ax /(scot %p our.bowl)//(scot %da now.bowl)/ahoyed/(scot %p her))
```

`/ahoyed/<ship>` is a public Ames endpoint returning a bare loobean —
`(~(has by chums.ames-state) her)` (`ames.hoon:12946`). Verified live on
`~racsun`: `~locdut` (a chum) returns `%.y`, `~marzod` (not a chum) returns
`%.n`. `/chums` gives the whole map; `/chums/all` a combined peer+chum view.

Surface the result:

- migrated → proceed, and say so;
- not migrated → proceed anyway, but warn that the transfer will run over legacy
  Ames, will be substantially slower, and will degrade the *serving* ship's
  responsiveness for the duration. Point at `|ahoy/prob <peer>` and at the
  `last-hash` gate above, since that is the likely reason it has not already
  happened by itself.

That tells the operator something true and currently invisible, at the moment it
is actionable, and cannot wedge anything.

**Why urgit should still not perform the migration itself.**

Two reasons survive the discovery above:

1. **It is not urgit's state to change.** Migrating a peer to Mesa alters how
   *every* agent on both ships talks to that peer, permanently. A repository
   fork is not consent to re-plumb the ship's networking — and the kernel
   already asks for that consent on the operator's behalf, globally, at boot.

2. **The failure mode is a wedged ship, not a failed fork.** `+migrate-peer`
   guards with `on-mate-test`, but if `%migr` errors after `%ahoy` succeeded the
   two ships land on opposite sides of the protocol; documented recovery is a
   manual `|pass [%a %rege ...]` on the *other* ship, and `%rege` on a peer
   carrying many open flows has been observed to spin at 100% CPU and wedge the
   ship outright. A fork button must not be able to reach that state. The test
   pair migrated cleanly in 0.31 s with 11 send / 10 receive flows open — a
   low-flow best case, not a guarantee.

A dedicated, explicitly labelled "migrate this peer to Mesa" control is
defensible as an operator action. It should never be a silent side effect of
clicking Fork.

**Note on reading these scries.** Hoon's `%.y` prints as `0` and `%.n` as `1`. A
negative control returning `0` has not failed — it has succeeded, and the control
is wrong. This bit us: the first "negative" control ship turned out to already be
a chum.

## Scope

Defects 2, 3, 4 and the minor item are entirely within `desk/app/urgit.hoon` and
`fe/src/` — deployable by `|install`. Defect 1 is partly desk-fixable (liveness)
and partly not (true progress needs the runtime change in
`UPSTREAM-RATE-DEFECTS.md`).

Restoring chunked serving is likewise desk-only: no `sur/` changes, no state
version bump (`peer-serve`/`peer-receive` are transient). The Mesa pre-flight
check described above is one scry plus a UI string.
