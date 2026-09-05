# Questions — group access phase 2

Boxes opened while implementing the rulings. Nothing here was designed around;
the code does what the rulings say and the question is whether that is what
was meant.

## Q1 — a catalog request the kernel never acks pins its ship as `pending` until the requester's urgit reloads

**Ruling involved.** CONTINUATION 4, ruling 1: one in-flight catalog request per
ship, tracked in a transient ledger; a ship whose earlier request is still
unacked is recorded as `pending` and is not asked again. Ruling 2: the ledger
entry is cleared by the poke-ack (ack or nack) on
`/peer/catalog-request/<request>`. The 30 s timer settles the *discovery*
(`unreachable`) but deliberately leaves the *ledger* alone, so a slow ship is
not asked twice.

**Where it lives.** `desk/lib/git-catalog.hoon` `+plan` (`[%hold ~]` when
the ship is in the ledger), `+held`, `+settled` (drops the entry whose request
id matches); `desk/app/urgit.hoon` `on-agent` arm for
`[%peer %catalog-request @ ~]` (clears the ledger on any poke-ack, marks the
discovery `no-urgit` on a nack), the two fan-out paths (`/peer/discover`,
`/peer/discover-group`) that consult `+plan`, the `[%peer %discovery-timeout @ ~]`
arm (settles the discovery only), and `on-load` (`peer-inflight ~`).

**What was observed (live table 3, row (c)).** With `~tyc`'s urgit nuked, a
catalog request from `~dyr` was flubbed by `~tyc`'s Gall: no ack, no nack;
`unreachable` after 30 s as designed. `|revive %urgit` on `~tyc` did **not**
release the held poke — Ames bone 124 on `~dyr` stayed `current 1 next 2`
for the rest of the session (checked at 19:04:48, 19:05:52, 19:08:07 and again
before writing this). So `~dyr`'s ledger kept `~tyc`, every later fan-out
reported `~tyc` `pending` in 0.5 s, and only a reload of `~dyr`'s urgit
(`|suspend` + `|revive`, on-load resets the ledger) let `~tyc` be asked again.
Row (b) (`|suspend` instead of `|nuke`) did release the held poke on revive,
within seconds, and the very next fan-out was `answered` with no duplicate —
that is the case ruling 1 was written for, and it works.

A host that is permanently gone (ship dead for good, or nuked-and-never-
revived) has the same shape: its ledger entry is never cleared, and the ship
reads `pending` forever from the requester's side, not `unreachable`.

**Why it is a box and not a fix.** Ruling 1 says "ever" (one request per ship
until the kernel acks), and rulings 1–6 do not give the ledger an expiry. Any
expiry is a semantics change — after it fires, a second request *can* be in
flight to the same ship, which is exactly what the ruling forbids. The brief
says to box that rather than decide it.

**Recommendation.** Keep the ledger as ruled and implemented. The hold is
transient (gone on any reload or upgrade of the requester's urgit), it is
per requester, it costs one map entry, and it only bites when a peer's kernel
never answers a poke — which, on this kernel, means the peer's agent was nuked
or the ship is gone. If that is not acceptable, the smallest follow-up ruling
is an age on ledger entries (say `~h1`, checked in `+plan` against `sent`):
after it, a ship is asked once more and the `pending` becomes a fresh
`unreachable` or an answer. Not implemented.

**Also recorded, not a question.** CONTINUATION 5 expected `|nuke %urgit` to
produce a nack and so `no-urgit`. On this kernel (Vere 4.6, brass-408k-1)
a nuked agent is flubbed exactly like a suspended one, so the live `no-urgit`
row could not be produced; the nack arm is proven by
`+urgit!git-catalog-vector` and the source tests only. A `%catalog-error`
reply (the peer runs urgit but refused or failed the request) is recorded as
`answered` with `ok %.n`, since the peer did answer — say so if that should
count differently in the footer.

**Ruling (operator, 2026-09-02, CONTINUATION 6).** The in-flight hold on a
ship expires after **one hour**. After the hold lapses, the next fan-out
sends a fresh `%catalog-request` to that ship and starts a new hold. The
kernel is not relied on to ack or nack a flubbed plea. Cost: at most one
queued request per member per hour. Benefit: a member whose urgit was nuked
and revived heals on its own, and a requester that never reloads urgit does
not hold a stale ledger forever. The nack arm and the `no-urgit` status stay;
they are correct for a kernel that nacks. The README says that on the current
kernel a suspended or nuked agent reports as `unreachable`, not `no-urgit`.

**Resolved.** `+plan` takes `now` and treats a hold sent an hour or more ago
as absent (`%ask`); `+hold-expiry` is `~h1`; a fresh request overwrites the
ledger entry (`+sent` is a `put`), and a late ack of the request it replaced
leaves the fresh hold alone (`+settled` matches on request id). A `%hold`
carries the send time, the pending entry keeps it as `held-since`, and
`GET /peer/discoveries` reports it as `heldSince` (ISO 8601 UTC); the footer's
pending count carries a tooltip with the oldest hold's age (`held 12 min`).
Vector: held 59 minutes → `%hold`, held 61 minutes → `%ask`, exactly one
hour → `%ask`, clock behind the send time → `%hold`. No state, no wire. Live
proof of the lapse is in `.scratch/LIVE-TABLE-4.md`.

## Q2 — the mirror-initialised read asks Groups for `%noun` where Groups serves `%group-ui-2`

**Ruling involved.** CONTINUATION 7: `group-members` read `/seats/ships` by
`%noun` where Groups serves `%ships`; fixed to read `/seats/ships/ships`.
The same ruling says the seat read and the v2 ui read are proven live, that
no other path changes, and to list every other `%groups` read with its
served mark.

**The finding.** `group-seat` reads `/v2/ui/groups/<host>/<name>/noun` for
the initialised bit of a joined group's mirror. Groups serves that path as
`group-ui-2+` (`groups.hoon`, `[%x %v2 %ui %groups ship name ~]`), so the
read is the same class as the defect: gall strips the `/noun`, gets a
`%group-ui-2` cage, and asks Clay for the `group-ui-2`→`noun` tube in the
`%groups` desk before handing the noun to urgit (`ap-peek`,
`rof [~ ~] /gall %cc ... /[have]/[want]`; `/mar/group-ui-2.hoon` grows
`noun`). The seat read (`/seats/<ship>/noun`) is not of this class: Groups
serves it as `noun+`. The `%gu` guards are not either. Nothing else in the
app scries `%groups`.

**What is proven.** Live, on fresh ships (`.scratch/LIVE-TABLE-5.md`): the
`/noun` form answers `[%.y 3]`; the same path with `/group-ui-2` as the
terminal answers `[%.y 3]` under the same `[* init=? member-count=@ud]`
cast, because the tube to `%noun` is the identity on the noun. Groups'
discipline library prints nothing for either form (its table lists
`/x/v2/ui/groups/$/$` as `%group-ui-2`). Also proven live, and worth
knowing before reading a log: the `%bad-scry-mark ... want=%noun
got=%ships` line is Groups' discipline comparing Groups' own table (which
says `/x/groups/$/$/seats/$` is `%noun`, a pattern that also matches
`/seats/ships`) with Groups' own answer; gall strips the caller's mark
before `+on-peek`, so the line does not depend on what urgit asks for, it
printed for the fixed `/ships` read in the dojo, it printed on all three
ships at join time before urgit was installed, and it keeps printing once
per fan-out after the fix.

**Why it is a box and not a fix.** The ruling says no other path changes
and calls the v2 ui read proven live; it is. But its success rests on the
same request-time mark conversion that the ruling took away from the
members read, and on the operator's ships that conversion is what failed
(this kernel converts both; the bail was not reproduced here).

**Recommendation.** Read `(group-peek group /v2/ui /group-ui-2)` — one
token, cast unchanged, no state, no wire — so that no `%groups` read in the
app depends on a Ford tube being built at request time. Not implemented.
