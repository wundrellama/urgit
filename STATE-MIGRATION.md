# State migration: %2 to %3 for the fork-prepare queue

Brief: `briefs/state-migration.md`. Branch `feat/progress-and-joinall`. Commit
`8ce6ad6`.

**The migration is written and the upgrade is proven on a ship that held the
old four-field %2 state.** The repository survived intact, the fork still works
end to end, the strand fix still holds after the migration, and a fresh install
still comes up through `on-init` with an empty queue.

Test ships, both booted from `pills/brass-408k-1.pill`:

| ship | pier | HTTP | Ames | role |
|---|---|---|---|---|
| `~ful` | `/home/michael/piers/mig-ful` | 8100 | 31345 | held the old %2 state, then upgraded |
| `~pen` | `/home/michael/piers/mig-pen` | 8101 | 31346 | fresh install of the migrated build |
| `~ful` (copy) | `/home/michael/piers/mig-ful-oldstate` | 8110 | 31355 | negative control, now stopped |

---

## 1. What I changed

`desk/sur/git.hoon`

- `state-2` is back to the four fields installed ships actually stored: `%2`,
  `repositories`, `peers`, `github-token`.
- `state-3` is new: those four fields plus
  `peer-prepare-queue=(map @uv peer-prepare-entry)`.
- `peer-prepare-request` and `peer-prepare-entry` did not move. The comment
  about the import direction still applies.

`desk/app/urgit.hoon`

- `migrate-state-1` again returns a four-field `%2`, so the `%0` and `%1` paths
  hand `migrate-state-2` the shape they claim to.
- `migrate-state-2` takes `state-2:git` and returns
  `[%3 repositories.stored peers.stored github-token.stored ~]`. The queue
  starts empty, which is the same shape `on-init` produces.
- `settle-webhook-state` takes and returns `state-3:git`.
- `on-load` migrates `%0`, `%1` and `%2` forward to `state-3` and reads `%3`
  directly. The agent core holds `state-3:git`.
- `/x/state/version` is new. It answers the version tag of the held state.

The behavior of the queue fix does not change. The queue is still a persisted
field, `on-load` still re-arms the `~s1` build timer for every entry, and
`on-init` still starts the queue empty. Section 4 proves this on a live ship.

`fe/src/migration.test.js` gains two tests: one pins the field lists of
`state-2` and `state-3`, one pins `migrate-state-2` and the restored
`migrate-state-1`. The `on-load` test now expects the four-arm `?+`.

## 2. Why `/x/state/version` exists

The brief forbids identifying a running build by a size match or a shared error
string. Three builds are in play here and two of them already answer
`/x/visibility/build`. `/x/state/version` is an arm no earlier build has, and
its answer is also the fact D2 needs. On the pre-`9f6bece` build the scry does
not resolve and the thread fails:

```
/x/state/version on ~ful, pre-9f6bece build : [0 %avow 1 %thread-fail %cancelled 0]
/x/state/version on ~ful, migrated build    : [0 %avow 0 %noun 3]
```

## 3. D2 — the upgrade on a ship that held the old state

### 3.1 Old build, real state

`~ful` was booted fresh and given the pre-`9f6bece` build
(`b8cffd4`, `zig build -Ddesk=`, `|commit`, `|install`). Its `sur/git.hoon` in
Clay is 7,494 bytes and contains no `peer-prepare-queue`.

A repository was pushed into it with the ordinary `git` client over Smart HTTP,
9 commits, 35 objects:

```
git push http://x:migtoken@localhost:8100/git/demo main
 * [new branch]      main -> main
```

The stored state was then read straight off the ship through `/x/dbug/state`,
which returns `on-save`:

```
version = 2   spine-fields = 4   fourth field is an atom = %yes
```

Four fields, tag `%2`. That is the noun an installed ship holds.

Repository list before the upgrade:

```json
{"name":"demo","objectCount":35,"commitCount":9,"fileCount":9,
 "head":"refs/heads/main","publicRead":true,"owner":"~ful",
 "refs":[{"name":"refs/heads/main",
          "oid":"575d2ed8e178b9667c1354c7263c12d8b2228086"}]}
```

The pier was then stopped by its recorded PID and copied to
`mig-ful-oldstate`, so the same old state could be used twice.

### 3.2 Negative control: the unmigrated build on that state

The `9f6bece` build was installed over the copy, which holds the identical
four-field `%2`. It does not load:

```
gall: reloading %urgit
bail: 4
bail: 2
```

The commit event bails and Clay rolls it back. Two attempts, same result
(log lines 26/32/38 and 52/54/56 of `/tmp/mig-logs/ful-oldstate.log`). After
both, the ship still reports `version = 2, spine-fields = 4`, and Clay still
holds the 7,494-byte pre-`9f6bece` `sur/git.hoon`: the update never took. A
second, unrelated file added to the same commit also failed to reach Clay,
which is what a rolled-back event looks like.

This is the defect the brief describes, on a real ship.

### 3.3 The migrated build over the top, no nuke

The migrated build was built into `~ful`'s mounted desk and committed. No
`|nuke`, no `|install`, no reboot.

```
gall: reloading %urgit
gall: reloading %urgit-clay
eyre: replacing existing binding at /git
eyre: replacing existing binding at /apps/urgit/api
```

No bail. Afterwards:

```
/x/state/version  : 3
/x/dbug/state     : version = 3, spine-fields = 5
peer-prepare-queue: empty
```

### 3.4 The repository data survived

The repository list after the upgrade is byte-identical to the one before:

```json
{"name":"demo","objectCount":35,"commitCount":9,"fileCount":9,
 "head":"refs/heads/main","publicRead":true,"owner":"~ful",
 "refs":[{"name":"refs/heads/main",
          "oid":"575d2ed8e178b9667c1354c7263c12d8b2228086"}]}
```

A count is not proof that the objects are usable, so the repository was cloned
back out of the upgraded ship with the ordinary `git` client:

```
head   : 575d2ed8e178b9667c1354c7263c12d8b2228086   (matches the source repository)
tree   : c01f14e880f3afef56c035891cb00ab916a3f516   (matches the source repository)
commits: 9
git fsck --strict: clean
files  : README.md, dir0/{file3,file6}, dir1/{file1,file4,file7}, dir2/{file2,file5,file8}
```

Every object crossed the migration and still hashes to the same SHA-1.

### 3.5 A fork works end to end on the upgraded ship

`~pen` forked `demo` from the upgraded `~ful` over Ames:

```
POST /apps/urgit/api/peer/fork {"ship":"~ful","repository":"demo","name":"demo"}
 -> 202 {"ok":true,"transfer":"0v1qn4.pr9eg.mm2cm.knhdk..."}
transfer 0v1qn4.pr9eg.mm2cm.k | active False | ok True | complete | demo
```

`~pen` then holds `demo` with 35 objects, 9 commits and the same head OID, and
a clone from `~pen` gives the same tree hash `c01f14e8...` with a clean
`git fsck --strict`.

## 4. D3 — the strand fix still works after the migration

The timing is the one `TRANSFER-VISIBILITY.md` §1 establishes: fire the fork
first, commit a compiled-core change to the server about 0.4 s later, so the
Clay build occupies `~ful` and the `~s1` wake queues behind it.

The server's activity list was emptied before each run, because `on-load` wipes
it. One entry afterwards is the reload and the re-armed timer in a single
observation.

| run | commit launched | fork answered | agent reloaded | server activity after | transfer |
|---|---|---|---|---|---|
| `migfix1` | t+0.34 s | t+0.015 s | after the request, before the serve | 1 entry | `demo3` complete |
| `migfix2` | t+0.43 s | t+0.015 s | t+9.16 s | 1 entry | `demo4` complete |

`~ful`'s log for `migfix2` puts the serve after the reload, which is the whole
point:

```
gall: reloading %urgit
eyre: replacing existing binding at /git
eyre: replacing existing binding at /apps/urgit/api
urgit: peer-prepare 0v1qn4.pr9eg.mm2ci.fskl4... -> ~pen demo: serving 35 objects, 3.756 bytes as %objects in 1 page
```

`~pen` holds all four forks, each with 35 objects, 9 commits and head
`575d2ed8...`. Both reloads went through the new `%3` arm of `on-load`, and
`~ful` still reports `version = 3` with the repository intact.

The scratch arm used to force the rebuild lived only in the pier's copy of
`app/urgit.hoon`. `~ful` was afterwards rebuilt from the committed source,
committed and reloaded once more. Both ships now run source that is byte-identical
to `desk/` on this branch, and `~ful` still reports `version = 3` with `demo`
intact.

## 5. D4 — a fresh install still comes up clean

`~pen` never held any urgit state. It got the migrated build through
`|merge`, `|commit`, `|install`, which runs `on-init`:

```
/x/state/version  : 3
/x/dbug/state     : version = 3, spine-fields = 5
peer-prepare-queue: empty
```

`~pen` then served as the fork requester for every transfer above, so the
fresh-install path is exercised as well as observed.

## 6. D5 — the frontend tests

`fe/src/*.test.js` was 88/88 before this work and is 90/90 after. The two new
tests are the ones described in section 1. No test was removed or weakened.

All 19 protocol vectors in `desk/gen/` were also evaluated on the migrated
build on `~ful` and none crashed. The vectors assert with `?>`, so a failed
assertion crashes the thread.

## 7. Should `AGENTS.md` record the distinction

Yes, and the note belongs on the existing line rather than beside it.

`AGENTS.md` line 4 says to keep persisted schemas at `state-0` "while this
project is greenfield". The project is no longer greenfield in the sense that
line assumes: the schema is at `%3`, and this branch is heading for a
repository with installed users. The rule was still correct for every earlier
task, and it was correct for `9f6bece` as an in-development commit. It became
wrong the moment the work was aimed upstream.

The distinction worth recording is the trigger, not the technique: nuke and
revive while a change stays inside this repository's development loop, and
write a migration once a change is going to ships that already hold state. A
reader who only has the current line has no way to tell which case they are in,
which is exactly how `9f6bece` shipped a schema that no installed ship can
load. I did not edit `AGENTS.md`.

## 8. What I could not measure and why

- **`migfix1`'s reload timestamp.** The watcher in my first run matched on an
  anchored pattern that never fired, so I have the ordering from `~ful`'s log
  (request, reload, serve) but not a number. `migfix2` has both. I did not
  re-run `migfix1` to recover the number, because the ordering is what the
  claim rests on.
- **The pre-fix strand on the migrated build.** I did not reproduce the
  original ten-minute failure again on these ships. `TRANSFER-VISIBILITY.md` §1
  has it twice on the pre-fix build, and reproducing it here would have meant
  reverting the queue fix, which this brief forbids touching. What I show is
  the positive: the same sequence completes after the migration.
- **Real ships.** Everything here is fake ships on one host. Fake ships cannot
  breach, so the interaction between a migration and a rift is untested. An
  installed ship also upgrades over an OTA rather than a mounted-desk commit;
  the `on-load` path is identical, but the surrounding Clay event is not, and I
  did not test the OTA path.
- **`%0` and `%1` on a live ship.** The `%0` and `%1` arms of `on-load` are
  covered by the frontend tests and by `git-migration-vector`, not by a ship
  that actually holds a `%0` or `%1` noun. I had no such pier and did not build
  one. The `%2` path, which is the one every installed ship is on, is proven
  live.
- **`state-3` under a partially-populated `github-token`.** The `%2` state I
  upgraded had `github-token=~`. A stored token is carried by the same field
  reference as the repositories, so I expect no difference, but I did not
  write one and upgrade with it.
