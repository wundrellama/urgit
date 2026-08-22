# Urgit: settle whether `%archive` can report sub-page progress

## Status

This is Tier 3 of `briefs/transfer-visibility.md`, split out because Tiers 0-2
landed and Tier 3 did not. **Read that brief and `TRANSFER-VISIBILITY.md`
first** — especially §2, which is where the previous run left this question half
answered.

Landed already, do not redo:

- `9f6bece` — `peer-prepare-queue` persisted; a reload no longer strands a fork.
- `53a49aa` — transfer mode logged, and transfer state pushed to the UI over a
  `/peer/activity` subscription.

The activity panel is now push-driven and completion arrives in ~22 ms. What is
still missing is any sense of *how far along* an `%archive` transfer is. It
reports `pages 1`, so the only working counter reads 0/1 then 1/1 — four
minutes of silence on a real repository.

## The question this run must answer

**Does a `%prog` task on a Mesa chum actually produce a `%rate` gift?**

The previous run got halfway and was right not to round it off:

- Two fake ships never enter each other's `chums` map, so `peer-directed` is
  false and `%archive` never runs. It built a pair on purpose by sending
  `[%load %mesa]` to Ames **before first contact**, after which `~med`'s chums
  held `~pec` as `%known`.
- On that pair `%prog` **does not crash**: three tasks, zero `crud`/`bail`, ship
  still answering afterwards.
- **No `%rate` gift was observed** — but the peek it was meant to track was
  never shown to resolve, so a negative could not be claimed honestly.

That is the gap. A `%prog` against a request that demonstrably resolves either
produces a gift or it does not, and that answer decides whether the rest of this
work is possible at all.

### Why it matters more than the feature

`specs/notes-progress-indicator.md` establishes that on legacy Fine the rate
machinery is unreachable by construction: `fi-sub` registers an atom `%sage`
interest, `fi-give-rate` skips atom interests, and `fi-rat` — the only thing
that creates the cell interest — is called by nothing in the vane. The Mesa
branch of `pe-prog` is the one place that might work.

If it does not deliver there either, **sub-page progress on `%archive` is not
achievable from a desk**, and that is a finding worth shipping. Say so plainly
and stop; do not substitute an estimated or interpolated number.

## Deliverables

### D1 — Answer the `%prog` question, conclusively

Stand up a chum pair (the previous run's `[%load %mesa]`-before-first-contact
recipe works; reuse it rather than rediscovering it). Then:

1. Issue a `%keen`/peek that you can **prove resolves** — show the resolution,
   not just the request.
2. Send `%prog` for that same request.
3. Report whether a `%rate` gift arrives at the agent.

**Show the evidence either way.** A gift arriving is a table of what it carried.
A gift not arriving is a demonstration that the tracked request completed while
no gift was seen.

**If no gift arrives: stop here.** Write it up as D5 and skip D2-D4. That is a
complete and useful outcome, not a failure.

### D2 — Send `%prog` alongside the archive send

Only if D1 is positive.

The archive send is one card in `peer-archive-accept`. Subscribe for progress at
the same point.

Pick a `feq` divisor that gives useful resolution without flooding the agent.
**State what you chose and why, and estimate the event cost** — every gift is an
Arvo event on a ship that is simultaneously receiving a large payload. The
previous run explicitly declined to pick a divisor without a live `%rate`; now
that you have one, pick it on evidence.

### D3 — Route it through to the UI

The `%rate` handler already exists at `desk/app/urgit.hoon:8660` and updates
`fine-progress`. Feed it, then expose fractional progress against
`expected-bytes`, which `archive-ready` already carries
(`desk/sur/git-peer.hoon:42-49`).

**Use the existing `peer-ui-state` projection** that `53a49aa` added. Both the
polling endpoints and the `/peer/activity` subscription read through one arm —
do not add a second shape they can disagree about. The progress you add should
arrive over the subscription like every other transfer event.

### D4 — Gate on the mode

`%prog` must never go out on a `%pack` or `%objects` transfer. Those run on
ames-core peers where `pe-prog` is a bare `!!` and the poke crashes the vane.

Gate on `=(%archive mode.flight)` and say in your report where you put the gate.

**The regression that matters most:** a `%pack` or `%objects` transfer still
completes and sends no `%prog`. Prove it.

### D5 — Report

`ARCHIVE-PROGRESS.md` at the repository root:

1. **The D1 answer first**, with evidence, whichever way it went.
2. If positive: the observed progress sequence from a real transfer, your `feq`
   choice and its event cost, and where the mode gate lives.
3. If negative: what you tried, what resolved, what did not arrive, and what
   would have to change upstream for this to work.
4. **A section titled "What I could not measure and why."** Do not omit it.

## Prove it (if D1 is positive)

- An `%archive` transfer where progress advances **more than once** before
  completion. Show the sequence.
- The same figure is **honest** — bytes actually received, not an estimate or an
  interpolation. If you cannot make it honest, say so and report the estimate as
  an estimate.
- A `%pack` or `%objects` transfer completes with no `%prog` sent.
- A browser attaching mid-transfer sees current progress, not zero.
- `fe/src/*.test.js` passes. Note that three assertions were red on `HEAD`
  before Tiers 0-2 and are now green at 88/88; do not let them regress.
- All 19 conformance vectors pass. Use the mapping in `TRANSFER-VISIBILITY.md`
  §9, which is current.

## Fences

- **Other projects hold ports 8080, 8081, 8090, 8093, 8096, 8097 and
  31411-31413.** Boot your own on 8100+ with an explicit ames port. Fake ships
  collide on default ames ports and the loser dies silently after a boot banner.
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only what you started, by a recorded PID.
- **Never `|pack` a pier whose snapshot exceeds about half its loom.** This took
  a production moon down earlier in this campaign.
- **This project ships nothing outside `desk/`.** No kernel edits, not on the
  source tree and not on a pier copy. If the honest fix is in Ames, that is a
  written finding in `specs/upstream-findings.md`, not a patched runtime.
- **Verify a running build with an arm that cannot exist in the previous
  build** — never a size match or a shared error string. An earlier session was
  fooled by an error message present in both versions.
- Persisted schemas change in place; nuke and revive rather than writing
  migrations (`AGENTS.md`).
- Do not touch `desk/lib/git-codec.hoon`, `desk/lib/git-delta.hoon`,
  `desk/lib/git-gzip.hoon`, `desk/lib/git-pack-decode.hoon`, or the Tier 0/1/2
  work in `desk/app/urgit.hoon` and `desk/sur/git.hoon` — all carry landed,
  verified changes.
- Commit on `feat/progress-and-joinall`. **Do not push.**
- **Commit as you go.** A previous run hit the turn ceiling with 281 lines
  uncommitted. A proven D1 committed is worth more than an unproven D3.

## Escape hatch

A run that stops to ask a real question is a success. Earlier in this series a
run stopped at D1 because the orchestrator's cost model was wrong, and that was
worth more than a fix.

If D1 is negative, write it up and stop — that is the expected shape of a
negative outcome, not a reason for `QUESTIONS`. Use
`QUESTIONS-ARCHIVE-PROGRESS.md` only for something this brief does not
anticipate: a cited line that does not match, a `feq` choice that cannot be made
on evidence, or a design collision the brief does not authorize.

Do not report a number you did not measure. Do not describe a transfer you did
not run.
