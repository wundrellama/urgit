# Urgit: make transfer progress and completion visible

## Status

This brief supersedes the earlier `%archive` sub-page progress scope. That work
is still here as Tier 3, unchanged in substance, but three defects found during
a live production trial come first — they are cheaper and they hurt more.

**Four tiers, in order: Tier 0 is a correctness bug (a reload strands a fork),
Tiers 1-2 are the visibility defects the operator actually hit, Tier 3 is the
original progress work.** Do not let Tier 3 consume the run before the earlier
tiers are done and proven.

Six performance and correctness fixes have landed on this branch. Read
`briefs/README.md` for the ledger. None of them touched the progress or
notification paths, so everything below is pre-existing.

**Read before starting:** `AGENTS.md`, then `specs/notes-progress-indicator.md`
(the kernel analysis behind Tier 3), then `specs/upstream-findings.md`.

## What the production trial showed

A fork of a 9,119-object repository between two live ships **succeeded in about
four minutes** — and the operator could not tell. The UI said "transferring
repository" the whole time with no movement, and the success was only
discovered by clicking cancel, which forced a state read.

During that transfer the orchestrator watched the serving ship respond in 8-10
ms with no log output, concluded it was idle, and built an elaborate wrong
theory about a stalled handshake. **The transfer was working perfectly the
entire time.** A working transfer and a dead one are indistinguishable today.
That is the defect.

---

## Tier 0 — a reload strands a fork permanently

**Do this first. It is a correctness bug, not a display bug, and the later
tiers touch the same lifecycle.**

### The defect, confirmed at source

`peer-prepare-queue` is declared outside the persisted state
(`desk/app/urgit.hoon:2047`) and `on-load` explicitly wipes it (`:2088`):

```hoon
:_  this(state loaded, ..., peer-prepare-queue ~, peer-serving ~, ...)
```

The serve flow is three steps:

1. a fork request arrives, gets queued in `peer-prepare-queue`, and the server
   sends `%accepted` back to the requester (`:3088-3096`)
2. a `~s1` Behn timer fires `/peer/prepare-start`
3. the timer handler looks the transfer up in the queue and builds the snapshot
   (`:8689-8702`)

**If the agent reloads between steps 1 and 3, the queue is empty and the timer
finds nothing.** It hits `?~ queued \`this` and returns silently. The requester
is holding an `%accepted` and waits forever for pages nobody will build. The
transfer shows as active on the acquiring ship and never resolves.

An agent reload in that window is not exotic. Every `|install`, every OTA sweep
that touches the desk, and every desk commit triggers one.

### Decided direction

Move `peer-prepare-queue` into the persisted state so it survives a reload, and
re-arm the timer on load for anything still queued.

`state-2` is small (`desk/sur/git.hoon:333-338`): `repositories`, `peers`,
`github-token`. Adding a field means a **`state-3`**. Per `AGENTS.md` this
project keeps persisted schemas at the current version and nukes/revives during
development rather than writing migrations — **follow that**: change the schema
in place, nuke and revive, do not add a compatibility shim.

On load, for every entry still in the queue, re-arm the `~s1`
`/peer/prepare-start` timer. A queue that survives but never gets its timer back
is the same bug with extra steps.

### The judgment call

`peer-serving`, `peer-receiving`, `peer-stream-jobs` and the browse queues are
wiped by the same line and have the same shape of problem. **Do not fix them all
in this run.** Fix `peer-prepare-queue`, because it strands the requester with no
recovery path. For the others, say in your report which ones you believe have
the same defect and what a fix would need — a written finding is the deliverable
there, not a change.

If moving the queue turns out to require touching state the later tiers also
change, say so and sequence it rather than doing both at once.

### Prove it

- **Reproduce the strand**: start a fork, force an agent reload during the
  `~s1` window, and show the requester stuck at active. If you cannot reproduce
  it, say so plainly — the reading above is from source, not from a live repro,
  and it may be wrong.
- **Show the same sequence completing after the fix.**
- A normal fork with no reload still works. This is the regression that matters.

---

## Tier 1 — log the chosen transfer mode

**Smallest change here, and it ends a whole class of guesswork.**

A fork resolves to one of three modes in `peer-prepare`
(`desk/app/urgit.hoon:3116`, decision at `:3177`): `%archive`, `%objects`, or
`%pack`. Nothing records which one was chosen. Attributing a transfer's cost
today means reverse-engineering the mode from Ames lane timing after the fact,
which is unreliable and was got wrong twice during the trial.

Emit the mode when it is decided — the object count, the byte count, and the
mode, as one line. Make it visible in the pier log and, if it is cheap, in the
transfer's activity record so the API carries it too.

State in your report what a real fork now prints.

---

## Tier 2 — completion is never pushed to the UI

### The defect, confirmed at source

`peer-finish` records success and returns no cards
(`desk/app/urgit.hoon:2484`):

```hoon
=.  peer-activities  (peer-activity-finish transfer %.y 'fork complete')
`this
```

The agent knows the fork is done. It tells nobody.

And it has nowhere to tell. `on-watch` accepts exactly one path:

```hoon
++  on-watch
  |=  =path
  ^-  (quip card _this)
  ?+  path  (on-watch:def path)
      [%http-response @ ~]  [~ this]
  ==
```

That is Eyre's own plumbing. **There is no `/ui` subscription. The agent emits
no facts to any browser, ever.** Every status the frontend shows comes from
polling `/api/peer/activity` on a 4-second `setInterval`, plus a per-transfer
poll while a fork is in flight.

So completion surfaces whenever the next poll happens, or when some other action
forces a read. Cancelling is one such action, which is why cancel appeared to
reveal the result.

### Decided direction

**Add a `/ui` subscription path to `on-watch` and emit facts on transfer state
changes.** This has been ratified by the operator as the right architecture — it
is how Urbit apps normally work, and polling for an event the ship already knows
about is the wrong shape.

Requirements:

- A new watch path carrying transfer activity. Name it consistently with the
  existing API surface rather than inventing a parallel vocabulary.
- Emit a fact wherever `peer-activity-finish` is called — **all of them**, not
  just the fork success path. Grep it: there are calls for failures, denials,
  incomplete graphs, and push results. A UI that learns about success instantly
  but waits 4 seconds for failure is worse than one that is uniformly slow.
- Emit on transfer **start** and on **progress** updates too, so the UI has
  something to render before completion.
- **Keep the polling endpoint working.** It is the fallback when a subscription
  drops, and removing it turns a degraded state into a broken one.

The frontend must subscribe and update from facts. It already has the plumbing
for a channel connection; use it rather than adding a new mechanism.

### The trap

A fact emitted to a subscriber that has gone away must not crash the agent, and
a reconnecting UI must be able to recover current state without waiting for the
next event. Make sure a browser that connects **mid-transfer** sees the transfer,
not an empty screen — subscribe-then-fetch, or send current state on watch.

---

## Tier 3 — real sub-page progress on `%archive`

This is the original scope and the analysis behind it is already written up in
`specs/notes-progress-indicator.md`. Read it in full; it is short and it is the
product of a prior investigation.

### The problem

`%archive` sends a whole repository as one Gall noun to a Mesa chum and sets
`pages 1` (`desk/app/urgit.hoon:3283`). The page counter is the only progress
signal that has ever worked, and on this path it reports 0/1 then 1/1.

For the trial's 9,119-object repository that was four minutes at zero.

### What exists and why it never worked

The `%rate` handler is live code at `desk/app/urgit.hoon:8660`. It parses an
`[%ames %rate *]` sign and updates `fine-progress`. Nothing is wrong with it.

**It has never received a single gift.** On legacy Fine the chain breaks in the
kernel: `fi-sub` registers an atom `%sage` interest, `fi-give-rate` skips atom
interests with `?@ int f`, and the cell interest it would deliver to can only be
created by `fi-rat`, which nothing in the vane calls. Six measured transfers all
reported `fineFragmentsReceived: 0`.

The only task reaching the rate machinery is `%prog`, which urgit never sends.

### Why `%archive` is different

`pe-prog` is a bare `!!` when `find-peer` returns `%ames`. For a **chum** it
takes the Mesa branch and reaches `ev-add-rate`. And `%archive` runs *only* on
chums — the mode decision checks `peer-directed` first. The guard that would
otherwise be mandatory is already satisfied by the routing.

`archive-ready` also carries `bytes=@ud` (`desk/sur/git-peer.hoon:42-49`), stored
by the receiver as `expected-bytes`. That is a denominator no other path has.

### Deliverables for this tier

**T3a — confirm the premise on a live chum pair.** Do not build on the reading
above. Verify that `%prog` for a chum does not crash and that a `%rate` gift
actually arrives. **If it does not deliver on this path either, stop and report
that** — it is a complete answer and it saves the rest of the work.

**T3b — send `%prog` alongside the archive send** (`desk/app/urgit.hoon:3306`,
inside `peer-archive-accept`). Pick a `feq` divisor giving useful resolution
without flooding the agent; state what you chose and why. Every gift is an Arvo
event on a ship already receiving a large payload.

**T3c — route the gift into the existing `%rate` arm** so `fine-progress`
updates, and expose fractional progress against `expected-bytes`. Match the JSON
shapes already used for `%pack` and `%objects` rather than inventing a third.

**T3d — gate on the mode, not just the peer.** `%prog` must never go out on a
`%pack` or `%objects` transfer; those run on ames-core peers and the poke
crashes the vane. Gate on `=(%archive mode.flight)` and say where you put it.

---

## Prove it

- **A real fork, watched end to end**, where the operator can tell it is working
  without reading the pier log. Show the observed sequence of states.
- **Completion appears without polling.** Demonstrate the fact arriving, not the
  4-second poll catching up.
- **A failed transfer surfaces just as promptly.** Force one and show it.
- **A browser connecting mid-transfer** sees the in-flight transfer.
- **Progress advances more than once** before completion on an `%archive`
  transfer (Tier 3).
- **Regression: a `%pack` or `%objects` transfer still completes and sends no
  `%prog`.** This is the one that matters most.
- `fe/src/*.test.js` passes; add a case for any new projection shape.
- All conformance vectors in `desk/gen/` pass. Use the mapping in
  `GZIP-REQUEST.md` — it supersedes the earlier reports.
- State plainly whether the progress figure is **honest**: bytes actually
  received, not an estimate. An invented number is worse than none.

## Report

`TRANSFER-VISIBILITY.md` at the repository root:

1. Tier 0 first: did the strand reproduce, and does it survive a reload now.
2. The T3a result: does `%prog` deliver on a chum, yes or no, with
   evidence.
3. What a real fork now prints and shows, per tier.
4. What you changed per file and why.
5. Your `feq` choice and its event cost.
6. The subscription design: what path, what facts, what happens on reconnect.
7. Which other transient-state maps you believe carry the Tier 0 defect.
8. **A section titled "What I could not measure and why."** Do not omit it.

## Fences

- **Other projects have live piers.** `~/piers/fakezod` and `~/piers/fakenec`
  (ports 8080/8081, tmux sessions `fakezod`/`fakenec`), and `t4-opus-zod`,
  `t4-sol-zod`, `t4-fable-zod` on ports 31411-31413. **Do not touch them.**
- **`~/piers/joinall-bus` runs on port 8093.** Reuse only if you scry-confirm its
  source first. Otherwise boot your own on 8094+.
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only what you started, by a recorded PID.
- **Never `|pack` a pier whose snapshot exceeds about half its loom.** This took
  a production moon down during the trial. Raise the loom and restart first.
- Fake ships collide on default Ames ports; the loser dies silently after
  printing a boot banner. Always pass an explicit port.
- **This project ships nothing outside `desk/`.** No kernel edits, not on the
  source tree and not on a pier copy. If something needs one, add it to
  `specs/upstream-findings.md` as a finding and work around it.
- **Verify running source by scrying, never by reading the mount.** Use
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/app/urgit/hoon)` and grep for a
  token unique to your change, immediately after your first install. Better
  still, verify with an arm that **cannot** exist in the other build — a size
  match or a shared error string proves nothing.
- Persisted schemas stay at `state-0`. Nuke and revive rather than writing
  migrations (`AGENTS.md`).
- Do not touch `desk/lib/git-codec.hoon`, `desk/lib/git-delta.hoon`,
  `desk/lib/git-gzip.hoon`, or `desk/lib/git-pack-decode.hoon` — all carry
  landed, verified work.
- Commit on `feat/progress-and-joinall`. **Do not push.**

## Known adjacent bug — related, and now Tier 0

The `peer-prepare-queue` strand described in Tier 0 was originally scoped as a
separate dispatch. It was folded in here because it collides with this work in
three places in `desk/app/urgit.hoon`: `on-load` at `:2088` (Tier 2 territory),
`peer-archive-accept` at `:3417-3420` (Tier 3's main edit site), and the
`on-arvo` timer handler at `:8696-8699`, adjacent to `on-watch`. Two runs
editing those regions in the same 368 KB file would merge badly.

## Escape hatch

A run that stops to ask a real question is a success. Earlier in this series a
run stopped at D1 because the orchestrator's cost model was wrong, and that was
worth more than a fix.

If T3a comes back negative, if a cited line does not match, if the honest
progress figure turns out to be unavailable, or if the subscription design
collides with something this brief does not authorize — write
`QUESTIONS-VISIBILITY.md` naming the problem, the options, and what each one
breaks. Then stop.

Do not report a number you did not measure. Do not describe a transfer you did
not run.
