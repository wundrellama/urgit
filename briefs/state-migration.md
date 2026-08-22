# Urgit: give the fork-queue state change a real migration

## Status

Commit `9f6bece` added a field to `state-2` **in place** and left the version at
`%2`. That follows `AGENTS.md`, which says to keep schemas at the current
version and nuke/revive during development. It is correct for this repository's
own development loop.

It is not correct for an upstream release. The work is going to a repository
with installed users, and an installed ship must survive the upgrade.

**Nothing else about `9f6bece` changes.** The queue fix itself is right and it
is proven: the strand was reproduced twice (`strand301`, `strand302` — each sat
`active` for ten minutes and then failed with `peer did not finish preparing the
repository snapshot`), and the same sequence after the fix returned
`success | fork complete` both times. This brief only adds the migration path.

Read `TRANSFER-VISIBILITY.md` §1 for that reproduction before you start.

## The defect

`on-load` (`desk/app/urgit.hoon`) reads a stored `%2` noun directly:

```hoon
=/  loaded=state-2:git
  ?+  -.q.old  !!
    %0  (migrate-state-1 (migrate-state-0 !<(state-0:git old)))
    %1  (migrate-state-1 !<(state-1:git old))
    %2  !<(state-2:git old)
  ==
```

`state-2` now carries a fifth field, `peer-prepare-queue`
(`desk/sur/git.hoon`). A ship that stored a four-field `%2` noun before the
change hits the `%2` arm, the mold fails to nest, and the agent does not load.

Every existing urgit installation breaks on upgrade.

## Decided direction

**Bump to `state-3` and write a `%2 → %3` migration.** The operator ratified
this shape. Do not keep `%2` with a lenient upcast — `state-2` would then mean
two different things depending on when it was written, which reads fine today
and confuses a reader later.

The file already shows the pattern. `migrate-state-1` ends:

```hoon
[%2 migrated peers.stored github-token.stored ~]
```

That is a version bump adding a field with a default. Follow it.

Concretely:

1. Restore `state-2` in `desk/sur/git.hoon` to its **original four fields**:
   `%2`, `repositories`, `peers`, `github-token`. It must again describe what
   installed ships actually stored.
2. Add `state-3` with those four fields plus
   `peer-prepare-queue=(map @uv peer-prepare-entry)`.
3. Add `migrate-state-2`, taking `state-2:git` and returning `state-3:git`, with
   the queue defaulted to `~`.
4. Update the `?+` in `on-load` so `%0`, `%1` and `%2` all migrate forward to
   `state-3`, and `%3` loads directly.
5. Point `=|  state-2:git` and every `state-2:git` reference in the agent at
   `state-3:git`.

Leave `peer-prepare-request` and `peer-prepare-entry` where they are. The
comment explaining why they live in `sur/git` rather than `sur/git-peer` — the
import direction — is correct and still applies.

## Prove it on a ship that holds the old state

**This is the deliverable.** A migration that compiles proves nothing.

1. Check out the commit **before** `9f6bece`, install that build on a fresh
   ship, and use it enough to write a real `%2` state: create a repository, push
   something into it, so `repositories` is not empty.
2. Confirm by scry that the stored state is a four-field `%2`.
3. Install the migrated build **over the top**, without nuking.
4. Show the agent loads, the state is now `%3`, and **the repository data
   survived**. A migration that loads but drops the repositories is worse than
   the crash.
5. Show a fork still works on the upgraded ship, end to end.

Then confirm the strand fix still holds after migration: queue a fork, force a
reload inside the `~s1` window, and show it completes. `TRANSFER-VISIBILITY.md`
§1 has the timing — fire the fork first, start the reload about 0.4 s later,
because the Clay build occupies the ship for roughly 8 s and the wake queues
behind it.

Also cover the fresh-install path: a ship with no prior state must still come up
through `on-init` with an empty queue.

## Deliverables

- **D1** — the migration, as described above.
- **D2** — the live upgrade proof: old-state ship, migrated build installed over
  it, data intact, fork works.
- **D3** — the strand fix still works after migration.
- **D4** — `on-init` still gives a fresh ship an empty queue.
- **D5** — `fe/src/*.test.js` passes. It was 88/88 before this work; do not let
  it regress. Note that three of those assertions were red on `HEAD` before
  Tiers 0-2 landed and are now green.
- **D6** — a short report, `STATE-MIGRATION.md`, covering what you changed, the
  upgrade evidence, and **a section titled "What I could not measure and why."**

Keep the report short. This is a small change with one important proof.

## Fences

- **Other projects hold ports 8080, 8081, 8090, 8093, 8096, 8097 and
  31411-31413.** Boot your own on 8100+ with an explicit ames port. Fake ships
  collide on default ames ports and the loser dies silently after a boot banner.
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only what you started, by a recorded PID.
- **Never `|pack` a pier whose snapshot exceeds about half its loom.** This took
  a production moon down earlier in this campaign.
- **This project ships nothing outside `desk/`.**
- **Verify a running build with an arm that cannot exist in the previous
  build** — never a size match or a shared error string. An earlier session was
  fooled by an error message present in both versions.
- Do not touch `desk/lib/git-codec.hoon`, `desk/lib/git-delta.hoon`,
  `desk/lib/git-gzip.hoon`, or `desk/lib/git-pack-decode.hoon`.
- Do not change the behaviour of the queue fix or the subscription work. This is
  a migration, not a redesign.
- Commit on `feat/progress-and-joinall`. **Do not push.**

## A note on AGENTS.md

`AGENTS.md` says to keep persisted schemas at the current version and to nuke
and revive rather than add migrations. That instruction is about this
repository's development loop, and it has been right for every earlier task.

This change is different because the work is going upstream to installed users.
If you think the instruction should still win here, **stop and say so in
`QUESTIONS-MIGRATION.md`** rather than splitting the difference. Do not write a
migration and also leave a nuke/revive path — pick one and argue for it.

Consider also whether `AGENTS.md` should record this distinction, and say so in
your report. Do not edit `AGENTS.md` yourself.

## Escape hatch

A run that stops to ask a real question is a success. If the upgrade proof fails
in a way the migration cannot fix, if restoring `state-2` collides with
something the later commits assume, or if the old build cannot be made to write
a usable `%2` state — write `QUESTIONS-MIGRATION.md` naming the problem, the
options, and what each one breaks. Then stop.

Do not report an upgrade you did not run.
