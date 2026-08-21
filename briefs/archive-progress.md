# Urgit: sub-page transfer progress on the `%archive` path

## Status of the sibling task

The `join-all` task named in the Fences section **has already run and landed**
as commit `22f2f77` on this branch. Its report is `JOIN-ALL-PERFORMANCE.md` at
the repository root and its brief is `briefs/join-all.md`.

That matters to you in three ways:

1. **`desk/lib/git-codec.hoon` is no longer contested.** The fence below still
   asks you not to touch it, and that still holds — but the reason is now "it
   changed under a separate, verified task" rather than "a parallel run owns
   it." If you rebase or see a conflict there, take the committed version.
2. **`join-all` is now `(can 3 parts)`, linear.** Measured exponent fell from
   2.256 to 1.129, and 396 pieces went from 668 ms to 3.4 ms. Do not repeat that
   measurement.
3. **Its method is worth copying.** That run reproduced the prior exponent
   independently before changing anything, kept its cold-start outliers visible
   in the results table instead of dropping them, and audited every call site by
   name. Read its report before you start.

---

## The problem

`%archive` sends a whole repository as one Gall noun to a Mesa chum. It sets
`pages 1` (`desk/app/urgit.hoon:3283`). The page counter is the only progress
signal that has ever worked in urgit, and on this path it reports 0/1 and then
1/1.

For a 200 MB repository that is a progress bar which sits at zero for minutes
and then completes. There is no way for the operator to tell a working transfer
from a wedged one.

Your job is to give the archive path real sub-page progress.

## What already exists, and why it never worked

**The `%rate` handler is live code at `desk/app/urgit.hoon:8660`.** It parses an
`[%ames %rate *]` sign, checks the ship matches the flight source, and updates
`fine-progress` on the receive state. Nothing is wrong with it.

**It has never received a single gift.** On legacy Fine the chain is broken in
the kernel:

- `%keen` reaches `on-keen`, which calls `fi-sub` (`ames.hoon:8788`). `fi-sub`
  registers an **atom** `%sage` interest.
- `fi-give-rate` (`ames.hoon:8713-8717`) opens with `?@ int f`. That skips every
  atom interest, so a `%sage` listener never receives `%rate`.
- The `[%rate boq feq]` cell interest that `fi-give-rate` delivers to can only be
  created by `fi-rat` (`ames.hoon:8850`), and **nothing in the vane calls
  `fi-rat`**.

Six measured transfers all reported `fineFragmentsReceived: 0` and
`fineFragmentsTotal: 0`.

The only task that reaches the rate machinery is `%prog`. Urgit never sends it.

## Why the archive path changes this

`pe-prog` (`ames.hoon:14008-14013`) is a bare `!!` when `find-peer` returns
`%ames`:

```hoon
=/  ship-state  (find-peer ship.spar)
?:  ?=(%ames -.ship-state)
  ::  XX support |ames
  ::
  !!
```

For a **chum** it takes the Mesa branch and reaches `ev-add-rate`. And
`%archive` runs **only** on chums — the source checks
`~(has by chums) target` before choosing the mode.

So the guard that would otherwise be mandatory is already satisfied by the
routing. On this path, and only on this path, `%prog` is safe and useful.

`archive-ready` also carries `bytes=@ud` (`desk/sur/git-peer.hoon:42-49`), which
the receiver already stores as `expected-bytes` (`desk/app/urgit.hoon:3283`).
That is a denominator no earlier path had.

## Hard constraint

**This project ships nothing outside `desk/`.** Urgit deploys with `|install`.
No kernel edits, not on the source tree and not on a pier copy. If you find
something that needs one, add it to `specs/upstream-findings.md` as a finding
and work around it.

## Repository

- Worktree: `/var/home/michael/workspace/urbit/urgit-progress`
- Branch: `feat/progress-and-joinall`, already checked out, base `dd0a869`
- Read `AGENTS.md` first. Then `specs/notes-progress-indicator.md`, which is the
  short version of everything above, and `specs/upstream-findings.md`.
- Skills are in `.claude/skills/`. Read `urbit-native-transfer-performance` and
  its `references/click-probe-syntax-and-timing.md` before writing any probe.
  That reference will save you several failed round trips.

## Deliverables

### D1 — Confirm the premise on a live chum pair

Do not build on my reading of the kernel. Verify it.

Establish two ships that see each other as chums, then confirm that `%prog` for
a chum does **not** crash and that a `%rate` gift actually arrives. A migration
tool exists at `/tmp/mesa-migrate-one.hoon` and a read-only survey at
`/tmp/mesa-survey.hoon`.

If `%prog` turns out not to deliver on this path either, **stop and report
that**. It is a complete answer and it saves the rest of the work.

### D2 — Send `%prog` alongside the archive send

The archive send is one card at `desk/app/urgit.hoon:3306`, inside
`peer-archive-accept`. Subscribe for progress at the same point.

Pick a `feq` divisor that gives useful resolution without flooding the agent
with events. State what you chose and why. Remember every gift is an Arvo event
on a ship that is also receiving a large payload.

### D3 — Feed the existing handler and surface it

Route the gift into the `%rate` arm at `8660` so `fine-progress` updates, then
make the receive state expose fractional progress against `expected-bytes`.

The frontend reads transfer state through the existing API. Update the JSON
projection so the UI can render a real fraction. Match the shapes already used
for `%pack` and `%objects` modes rather than inventing a third.

### D4 — Guard the mode, not just the peer

`%prog` must never go out on a `%pack` or `%objects` transfer. Those run on
ames-core peers and the poke crashes the vane there. Gate on
`=(%archive mode.flight)`, and say in your report where you put the gate.

### D5 — Prove it

- A live archive transfer between two chums where progress advances more than
  once before completion. Show the observed sequence.
- The same transfer on a `%pack` or `%objects` peer still completes and sends no
  `%prog`. This is the regression that matters most.
- `fe/src/*.test.js` still passes. Add a case for the new projection shape.
- State plainly whether the progress figure is honest: it should reflect bytes
  actually received, not an estimate.

### D6 — Report

`ARCHIVE-PROGRESS.md` at the repository root:

1. D1 result first. Does `%prog` deliver on a chum, yes or no, with evidence.
2. What you changed, per file, and why.
3. The observed progress sequence from a real transfer.
4. Your `feq` choice and its event cost.
5. **A section titled "What I could not measure and why."** Do not omit it.

## Fences

- Do not touch `/home/michael/piers/fakezod` or `/home/michael/piers/fakenec`
  (ports 8080/8081). They belong to another project. Boot your own piers on your
  own ports with your own identities. Fake ships collide on default Ames ports
  and the loser dies silently after printing a boot banner.
- **Verify running source by scrying it, never by reading the mount.** A
  detached desk mount discards edits while `%kiln-commit` still answers
  `%committed`. This cost a previous session three benchmark runs:
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/<file>/hoon)` and grep for
  a token unique to your change.
- Persisted schemas stay at `state-0`. Nuke and revive rather than writing
  migrations (`AGENTS.md`).
- Commit on this branch. Do not touch `desk/lib/git-codec.hoon` or
  `desk/lib/git-pack.hoon` — a parallel task owns those files.

## Escape hatch

A run that stops to ask a real question is a success, not a failure. If D1 comes
back negative, if a cited line does not match, or if the honest progress figure
turns out to be unavailable — write `QUESTIONS.md` naming the problem, the
options you see, and what each one breaks. Then stop.

Do not report a number you did not measure. Do not describe a transfer you did
not run.
