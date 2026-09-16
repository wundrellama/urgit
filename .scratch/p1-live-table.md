# CI P1 live table — chair `opus`, close-out run on ship `~peg`

- Worktree `/var/home/michael/workspace/urbit/urgit-ci-p1-opus`, branch `ci/p1-opus`, base `ef8c3eb` = `master`. The P1 build (S1–S6, commits below) ran on `~ryp:8346`; this record is the close-out's cold battery (BRIEF-CI-P1-CLOSEOUT.md T5) on the close-out footer, after T1–T4, from a fresh pier, nothing typed: `cold.sh` ran `boot.sh` → `docker-rootless.sh start` → `ci-p0/battery.sh` → `ci-p1/battery.sh` detached, 20:56:23 → 22:19:04 on 2026-09-15.
- Ship `~peg`, HTTP 8350, pier `/var/home/michael/piers/urgit-ci-p1-peg`, booted by `.scratch/ci-p0/boot.sh` in herdr pane `w19:p4` (split from `w19:p1`) with the footer's boot line; `%urgit-ci` state version 0; `+code` recorded by the script.
- Rootless Docker: `dockerd-rootless.sh` with state dir `/run/user/1000/ci-p1-closeout/`, data root `.scratch/tmp/docker-data` (the P1 build's, its `catthehacker/ubuntu:act-latest` already loaded), socket `/run/user/1000/ci-p1-closeout/docker.sock`; `info --format '{{.SecurityOptions}}'` → `[name=seccomp,profile=builtin name=rootless name=cgroupns]`, server 29.7.2. The host's rootful daemon was never touched.
- Tooling: `go1.27.1 linux/amd64`, `zig` 0.15.2, `act` 0.2.89 on PATH (brew) and the static `act` 0.2.89 at `.scratch/tmp/act-static/act` now BUILT by `.scratch/ci-p1/act-static.sh` (`CGO_ENABLED=0 go install github.com/nektos/act@v0.2.89`; sha256 `ecf7a0b2…`, `file`: statically linked), which the daemon copies into every sandbox.
- The dojo pane was 74 columns wide for this run (the P1 build's was wider); every reader is width-proof now (Deviations, close-out).

## Commits (one per stage, one per close-out task)

| Stage | Commit | Gate |
|---|---|---|
| S1 | `06b4334` sur/ci.hoon grows state-0 (D12) | `zig build` green |
| S2 | `534adb5` %urgit: three peeks, refs/ci filter, scratch ref, land-candidate, actor/via | `+urgit!git-migration-vector` %.y, `+urgit!git-access-vector` %.y on ~ryp |
| S3 | `667c223` %urgit-ci: auto-materialize, planner, scheduler, verdict, landing, /plan, /abandon, /attempt read | install, `|nuke`/`|revive` round-trip (state version 0), `+urgit!ci-plan-vector` 27/27 |
| S4 | `858676e` runner/: daemon, tests, `zig build -Drunner` | `go test ./...` green, static binary, `-Drunner` with and without Go |
| S5 | `f555667` harness (P0 on this tree, P1–P20, mutants) + the fixes the live rows found | P0 battery, P1 table, RED/GREEN |
| S6 | `86fd13f` runner/README.md | writing rules checked by script |
| record | `462e763` the P1 build's live table (on `~ryp`) | — |
| T1 | `3d66620` `act-static.sh` builds the static act; `p-setup` calls it | built, `--version` 0.2.89, `file` static; idempotent second run |
| T2 | `3014c99` both `mutants.sh` refuse outside a git work tree | refused from a non-git copy; `status` still answers in the tree |
| T3 | `d522550` self-reverting RED (trap), per-row tripwire oracle, `-race` + live Docker boundary test | RED 13/13 with tripwires below; `foreground.sh` below |
| T4 | `1ed326d` `refs/ci/*` hidden from the authenticated repository API too; P16 asserts both routes | P16 below (both lists clean while open and after) |
| T5 | `01bcf4f` cold battery on `~peg`: footer, `cold.sh`, the harness fixes the cold runs needed | this run |
| T6 | this record | — |

## P0 battery tail (`.scratch/ci-p0/battery.sh`, inside `cold.sh`)

Run 2026-09-15 20:57:10–21:01:59 on `~peg`, straight after `boot.sh` (20:56:23; dojo prompt, `|new-desk`, `|mount`, `zig build`, `|commit`, `|install our %urgit` → `gall: booted %urgit-ci`, state version 0, `+code` recorded) and `docker-rootless.sh start`. Every step ran; the seven negative rows went RED under the P0 mutants and GREEN on the real build; the four P0 vectors print `%.y` and `cd fe && npm test` passes.

```
################ setup  (2026-09-15T20:57:10-05:00)
################ h1  (2026-09-15T20:57:13-05:00)
################ h2  (2026-09-15T20:57:15-05:00)
################ h3  (2026-09-15T20:57:19-05:00)
################ h4  (2026-09-15T20:57:19-05:00)
################ h5  (2026-09-15T20:57:24-05:00)
################ h6  (2026-09-15T20:57:31-05:00)
################ h7  (2026-09-15T20:57:58-05:00)
################ h8  (2026-09-15T20:58:03-05:00)
################ h9-13  (2026-09-15T20:58:06-05:00)
################ h14  (2026-09-15T20:58:56-05:00)
################ h15  (2026-09-15T20:59:09-05:00)
################ negatives red  (2026-09-15T20:59:12-05:00)
H9 [red]: FAIL
H10 [red]: FAIL
H11 [red]: FAIL
H12 [red]: FAIL
H13 [red]: FAIL
H14 [red]: FAIL
H15 [red]: FAIL
== red: PASS=0 () FAIL=7 (H9 H10 H11 H12 H13 H14 H15) of 7 rows; build: MUTATED
RED: every row fails under the mutant build
################ negatives green  (2026-09-15T21:00:36-05:00)
H9 [green]: PASS
H10 [green]: PASS
H11 [green]: PASS
H12 [green]: PASS
H13 [green]: PASS
H14 [green]: PASS
H15 [green]: PASS
== green: PASS=7 (H9 H10 H11 H12 H13 H14 H15) FAIL=0 () of 7 rows; build: clean (real build)
GREEN: every row passes on the real build
################ foreground  (2026-09-15T21:01:52-05:00)
== +urgit!ci-event-vector -> %.y
== +urgit!ci-storage-vector -> %.y
== +urgit!git-migration-vector -> %.y
== +urgit!git-access-vector -> %.y
== cd fe && npm test
# tests 126
# pass 126
# fail 0
battery.sh: all steps ran (2026-09-15T21:01:59-05:00)
```

Observed (from the row logs): H3 `staged as ci candidate`, master unchanged; H4 candidate == head; H5 merge candidate with parents (TWO, DIVERGE); H6 first bearer poll → `200` with the automatic **plan** assignment (D4/D6), the manual `%assign` job → delivered once, then `204`; H7 lines relayed, outputs recorded; H8 result → `%passed`, `master oid` == THREE with the client push answering `Everything up-to-date` (the ship landed it, D14); H9 `409 no jobResult event was relayed`; H10 `~s20` deadline → `%infrastructure-error`, `%unknown`, push `remote rejected … staged as ci candidate`; H11 `%failed`, push refused; H12 `401 attempt authentication required`; H13 `413 event line exceeds 64 KiB`; H14 outage refusals while stopped, `side` lands and the protected push is staged after the restart; H15 `~` cross-class, `[~ 'http://127.0.0.1:1/ci-bucket/ci/ci-fixture/…/trusted/cache.tar']` same-class (read joined: the URL no longer fits one row).

## P1 table

Real `act` 0.2.89 (static, built by `act-static.sh`) inside the docker-rootless sandbox, the real daemon binary, no shell relay and no dojo poke between push and verdict (P11's `%assign` override and P17's `%set-ref` are the rows' own operator acts). Rows driven by `.scratch/ci-p1/battery.sh` (21:01:59 → 22:19:04); logs in `.scratch/tmp/p*.log`. "Observed" quotes the row's PASS lines verbatim (trimmed).

| Row | Verdict | Observed |
|---|---|---|
| P1 | PASS | `%set-ci-protected` before any tip → the dojo shows the refusal `ref has no tip; push a commit before CI-protecting it` (the row now names the ship's answer: `refused: …`, the mutant's is `accepted (>=)`); `ci-protected` `%.n`; seed push landed `e47aa700…`; same poke after → `%.y` |
| P2 | PASS | `enrolled as daemon` (read from the run's first log line, not the banner: Deviations); banner `sandbox: docker-rootless (container isolation; microvm backend pending)`; `state.json` mode `-rw-------`, raw token absent (0 matches); ship `[1 'docker-rootless']`; daemon `0v2.kelga…`; restart: `state file … present … no re-enrollment` (count 1 before and after), polling (`last-seen` `[~ ~2026…]`) |
| P3 | PASS | push → `staged as ci candidate`; materialized after **1 s** (`candidate=[~ 0x182b.e4e1…]` = the head); `refs/ci/candidate/0v2.j88bd…` advertised by `git ls-remote`; master unchanged; plan assignment in the daemon log **1 s** after the push, no poke |
| P4 | PASS | plan = `fixture-chain.yml/a fixture-chain.yml/b fixture-pass.yml/pass`; `plan POST -> 200`; `plan-oid` = the candidate oid |
| P5 | PASS | `a` `%passed`; `b` created after `a` finished (`started` `~2026.09.16..02.02.36..8661` ≥ `a.finished` — the same second); `b` `%passed`; candidate `%passed`; **master = candidate OID `182be4e1…` with no second push** (clone not ahead); `verdict-reason` `'landed'`. Push→landing: ~7 s. Attempts: `pass`, `b`, `a` `%job` `%passed`, the `%plan` `%passed` |
| P6 | PASS | `fixture-chain-off`: `a` `%passed`; `b` `%skipped` with reason `if false: needs.a.outputs.go is "false", not "true"`; candidate `%passed`; landed `6042989f…` |
| P7 | PASS | `fixture-fail`: job `%failed`; candidate `%failed`, reason `job fail in fixture-fail.yml failed`; master unchanged (`6042989f…`); re-push → `staged as ci candidate` (refused); scratch ref released |
| P8 | PASS | matrix: candidate `%failed`; plan attempt reason `plan-invalid: matrix unsupported in P1 (job m in fixture-matrix.yml)`; `verdict-reason` carries it; no job attempt |
| P9 | PASS | `if: github.event_name == 'push'`: candidate `%failed`; reason `job u in fixture-unsupported-if.yml: unsupported if expression "github.event_name == 'push'"`; no job attempt |
| P10 | PASS | SIGKILL `act` inside `ci-0v6.j0398…` at 21:04:26 → attempt `%infrastructure-error` **1 s** later, reason `abandoned: act exited 137 without a jobResult`; daemon `abandon POST -> 200`; candidate `%unknown`; push refused (staged again); sandbox destroyed |
| P11 | PASS | the automatic ~h1 attempt abandoned the P10 way; the ~s20 override attempt `0v6.gulg0…` running when daemon `a` is SIGKILLed: still `%running` 5 s later (no abandon); `%infrastructure-error` after **17 s**, reason `no result arrived before the deadline`; its sandbox left as an orphan; restart: `reconcile ci-0v6.gulg0… (attempt infrastructure-error): destroyed`, orphan gone; candidate `%unknown` |
| P12 | PASS | one refused `network rm` → `QUARANTINED slot ci-0v1.va47j…: teardown failed: docker network: p12: network rm refused by the harness; advertised capacity now 1` (the row prints the daemon's line now), exactly one quarantine, the network left behind; the job and the next push ran on the remaining slot, `%passed`; candidate `%passed` |
| P13 | PASS | daemons `a` (`0v2.kelga…`, busy with the slow job) and `b` (`0v3.erlks…`): the plan went to `b`, candidate `%passed`; after 5 min 10 s `b` is stale: never selected; the waiting candidate was planned on `a` once it polled, `b`'s `last-seen` older than `a`'s; `%passed` |
| P13-overlap | PASS (GREEN phase) | see the mutant pairs: on the real build two `act-…-fixture-wait-wait-<hash>` containers alive at once, both attempts `%passed`, projection names `<attempt>/fixture-wait` differ, plan `name` = `fixture-wait`, no collision evidence in either stream |
| P14 | PASS | ship side: `|nuke %urgit-ci` (kiln's `nuke? (y/N)` answered), `|commit`, `|revive %urgit`, `%gu` `%.y`, `state version: 0`, candidates 0, daemons 0, `ci-protected` `%.n`; daemon side: the running job's sandbox destroyed, `enrollment lost; re-enroll with a fresh token`, `urgit-runner: exit status 3`, process gone |
| P15 | PASS | ERPit master `d4f268e8…` (1066 commits) seeded into `erpit-p15`, CI-protected, one commit pushed (`14431d74…`): plan after 39 s = `suite.yml/{plan,structural,suite}` + `fixtures.yml/{pins,plan,replay,erasure,duo}`; `pins`, `structural`, both `plan`s, then — **gated by the ship on `plan`'s outputs** — `replay`, `erasure`, `duo`, `suite`: 8 job attempts, no reruns, **all eight `%passed`** (every result `POST -> 200` under its own `projection-name`); candidate `%passed` after **1117 s**; **master = candidate OID**, `'landed'`. Timing below. |
| P16 | PASS | while open: `refs/ci/candidate/0v6.5hqi1…` advertised to git, absent from `[%x %repository @ ~]`'s list **and from the authenticated `/apps/urgit/api/repository/ci-p1` list (T4), which reads `refs/heads/master` alone**; after the close: gone from `ls-remote`, both lists still clean |
| P17 | PASS | X staged; operator `%set-ref` master → Y `2ee9169d…` while X ran; X `%passed`; landing refused; master = Y; X still `%passed` with `verdict-reason` `'destination moved; rebase and push again'` |
| P18 | PASS | `POST git-receive-pack` with no credentials → `401 repository authentication required`; with a wrong write token → `401`; git reports `Authentication failed` twice; candidate count unchanged (3); master unchanged |
| P19 | PASS | `ci-p1-linked` bound to `%scratch`: `ci-ref` peek `linked=%.y`; `%set-ci-protected` → `CI protection is not available for desk-linked repositories in this release`, not protected; `ci-p1` bound after protection: push → staged, `%passed`, landing refused with the same reason (`verdict-reason`), master unchanged; unbound after |
| P20 | PASS | `fixture-chain.yml`: `b`'s saved stream has **0** `"jobID":"a"` lines; `a` ran once in its own attempt; ship `events` for `b` = **15** = `b`'s own lines; no `ship refused event` in the daemon log for `b`; act ran `-W /work/projected/fixture-chain.yml -j b` |

### P15 timing (ERPit, daemon `a` at capacity 3, one Docker daemon)

Push at 21:12:39 (local; UTC below from the attempts): plan attempt 02:12:44–02:13:08 (clone of 1066 commits + `act -l` ×2, 24 s); `pins`, fixtures `plan`, suite `plan` 02:13:08–02:14:14/02:14:28; `structural` 02:14:14–02:15:04; `replay` 02:14:14–02:18:50; `erasure` 02:14:28–02:19:54; `duo` 02:15:04–02:21:39; `suite` 02:18:50–02:31:18 (the on-ship battery, 12 min 28 s). **Push → verdict: 1117 s = 18 min 37 s**; three fake ships booted inside the sandbox (`replay`, `erasure`, `duo`) plus the suite's; relayed lines per job 20–215, 0 refused. Faster than the build's 19 min 39 s on the same host.

## Deviations

Every derivation was built as written unless listed here. Each departure cites what made it.

- **D4 (scheduling) — work waits for capacity.** The brief says a ready candidate is planned "for the least-loaded enrolled daemon that has reported capacity" and is silent on the case where no daemon can take it. The scheduler (`++schedule`) runs on candidate-ready, plan stored, attempt close, daemon poll and enrollment, so work created while every daemon was busy or absent is offered again at the next opportunity rather than dropped (R1-A's "never dropped"). Consequence for the P0 rows: H6's first bearer poll now answers the automatic plan assignment of a pending candidate instead of 204 (recorded in the h6 log; the row's assertions hold).
- **D4 (verdict) — a candidate without a plan keeps P0's rule.** The P0 harness drives job attempts by `%assign` on candidates that never had a plan; their verdict is the newest attempt's status (P0's "the newest attempt is the candidate's required attempt"). With a plan, the verdict is `lib/ci-plan`'s over the newest attempt per `[workflow job]`.
- **D4 — a merge conflict fails the candidate.** `%candidate-conflict` now sets `%failed` with `verdict-reason` `'candidate could not be materialized: …'` (P0 left it `%pending` with `conflict=%.y`); a candidate that can never be tested must not wait forever (R1-A).
- **D1 — reasons and filters.** The matrix refusal reads `matrix unsupported in P1 (job m in fixture-matrix.yml)`: the brief's literal first, the job after it. Jobs whose workflow does not trigger on `push` are not planned (GitHub semantics; the spec's cutover list names trigger filters as later work), and a plan with no push-triggered job is refused `no push-triggered jobs under .github/workflows` — fail closed rather than vacuously green.
- **D7 — the act binary.** The daemon copies a static `act` into the sandbox (`act_binary`). Brew's `act` cannot run there (dynamic linking); the README says so. The `act -l` run merges stderr into the stream so a refused workflow's message becomes the plan's reason; the table parser skips the docker-host banner line.
- **D7 — checkout on a branch.** `git checkout -B <branch> <oid>` where `<branch>` is the assignment's ref minus `refs/heads/`: ERPit's workflows filter `branches: [master]`, and act reads `github.ref` from the checkout (the spike ran on a branch). A detached checkout made act warn `unable to get git ref`.
- **D7 — `GET /attempt/<id>`.** Eyre's `parse-request-line` splits the last url segment at its final dot into an extension; the `@uv` id is rejoined from `site` and `ext`. The P0 routes never met this because the id was followed by `/event`, `/result`.
- **D9 — the scratch ref on a re-run.** An operator `%assign` on a candidate that already closed (its scratch ref released) re-sets `refs/ci/candidate/<id>` through the existing `%set-ref` action before the assignment goes out; found live in P11, where the override attempt's clone failed with `couldn't find remote ref`.
- **D6/D7 — a delivered assignment that shows no activity is offered again.** Eyre reports a closed long-poll connection about ten seconds after the client goes (measured: the poll cleared ~12 s after `curl` was killed, before its 25 s window); an assignment answered into a dead poll in that gap would wait out its whole deadline (~m5 for a plan, ~h1 for a job) after every daemon restart at the wrong moment. Two additions: `on-leave` on `/http-response/<id>` forgets the poll, and a delivered assignment whose attempt still has zero events (and, for a plan, no plan stored) two minutes on is offered again to its daemon's next poll (`++stale-delivery`); the daemon ignores an attempt it is already running (`claim`). Found by P13's tail (stop `a`, push, restart `a` within seconds) and verified in isolation: the lost plan was re-offered at +2 min, the candidate passed and landed. P0's H6 "delivered once, 204 on the next poll" still holds because its second poll comes within the window.
- **D6 — capacity is reported on every poll.** The harness re-enrolled `a` with a new identity to change its capacity and left the old record as a ghost with a fresh `last-seen`; the ghost won the oldest-enrolled tie and swallowed the first plan of the overlap row (deadline `%unknown`). A production restart keeps its identity (the state file), so the product answer is D6's own sentence: the daemon sends `x-ci-capacity` on every poll and the ship records it (`handle-assignment-poll`); the harness restarts the daemon with a new config instead of re-enrolling. Re-enrollment is only for P14 (state wiped) and the negatives' fresh phases. *[Close-out: the negatives' fresh phases met the same ghost; they keep the identity now, see below.]*
- **CI-PROJECT-1.1 — projection name.** `Project` rewrites the top-level `name:` line to `<attempt-id>/<original>` (inserting one from the file name when the workflow has none); the plan's `job` carries `name` (act -l's "Workflow name" from the unprojected candidate) and the attempt records `projection-name` from the first relayed event's `job` field (`<name>/<jobID>`, what act itself saw). The row is P13-overlap; the mutant is the unprefixed name.
- **Mutants are applied together (the P0 rule: one build, RED, then GREEN).** Two consequences visible in the RED tails: under the P13-overlap mutant (unprefixed `name:`) the two acts started within a millisecond, the loser's act exited `1` after 120 ms (act refused the job container whose name was in use — the create-time collision rather than the brief's force-remove-then-137 path) and relayed a `failure` jobResult, which the P7 mutant (every result `%passed`) then recorded as `%passed`; the row still went red on the container count and the projection names. Under the P18 mutant (`write-authorized` skipped) the anonymous `POST git-receive-pack` answers `200`, while `git push` still fails at `info/refs` (a different, unmutated check) — the row's curl probes are what turn it red.
- **Harness — git and the desktop's askpass.** This desktop exports `SSH_ASKPASS=/usr/bin/ksshaskpass`; on a 401 git opened a GUI credential dialog and the P18 row hung for the whole phase timeout. `env.sh` now sets `GIT_ASKPASS=/bin/true` and unsets `SSH_ASKPASS`.
- **Harness — the dojo's `ci` binding.** A grown mold makes every cast through the old `=ci` binding crash the scry (`bail: 4`, `arvo: scry-lost`); `nuke-revive.sh` re-runs the prelude.
- **D10 — the interface gained three arms.** Beyond `Prepare/Copy/Run/Destroy`: `Orphans` (the ids this backend still holds, for D7 c), `Name` (the disclosure string), and `Signal` (kill a process by name inside the sandbox; the harness kills act with `docker exec … pkill` directly, so `Signal` is unused by the rows). `Run` takes a working directory. `Destroy` removes act's leftover job containers on the attempt's network before the network itself, so a killed act (P10) does not turn into a quarantine (P12).
- **D12 — molds.** `attempt-result` gained `[%plan-invalid message=@t]` (closes the attempt `%failed`, `reason` = `'plan-invalid: …'`). A skipped job is an attempt with `status=%skipped`, `daemon=0v0`, `assignment=0v0` and the reason. `daemon.sandbox` defaults to `''` when the enrollment body omits it.
- **D14 — `accept-receive`'s signature.** `eyre-id` became `(unit @ta)` and the three call sites in `handle-receive-pack`'s tail pass `` `eyre-id ``; the brief places that change inside the fence as part of the arm. Nothing else in the tail, `pending-clay`, `clay-push` or `/clay-report` changed.
- **CI-PROJECT-1 — the tripwire.** The brief attributes `event job does not match the assignment` to P0; P0 had no per-job attempt, so the 409 is new in S3 (`handle-event`, on `kind=%job` attempts).
- **P0 harness (script fixes, all recorded):** `env.sh` carries the opus footer (ROOT from the script's own location); `h6.sh`, `h9-13.sh`, `h14.sh` and `negatives.sh`'s `assign()` use the P1 `%assign` shape (`%job`, workflow, job, deadline); `h14.sh` and negatives H14 are re-premised because the ship lands a passed OID itself (D14): the protected-ref probe while `%urgit-ci` is stopped is a fresh commit, and after the restart it is staged, not landed.
- **P11 — the override attempt is the one the daemon runs.** With capacity 1 the automatic ~h1 attempt fills the slot, so the row abandons it first (P10's kill) and then assigns the ~s20 override; the daemon is killed while running that one. The "left for its deadline, not resumed" branch of reconciliation is exercised by unit test only.
- **P12 — one refused teardown.** The wrapped `docker` refuses exactly one `network rm` (the plan attempt's teardown, the first after the flag); refusing every teardown quarantined both slots and the daemon exited at capacity 0 (which is D10's rule, observed).
- **P13 — a stopped daemon seen within ~m5 is selectable.** The ship cannot tell a stopped daemon from a slow one until `last-seen` ages past ~m5; the row asserts the stale daemon is never picked and the fresh one gets the plan once it polls.
- **P14 — `|nuke` needs `y`.** kiln asks `nuke? (y/N)`; `nuke-revive.sh` answers it, commits with the agent stopped (a commit that reloads an agent whose old state no longer nests fails as a whole and Clay rolls it back), then `|revive`.
- **D9 — the authenticated API still lists `refs/ci/*`.** D9 places the filter in `public-repository-json`, which the `[%x %repository @ ~]` peek and the public pages use; the authenticated `/apps/urgit/api/repository/<name>` uses `repository-json` and is outside the enumerated touch, so it still shows the scratch refs while a candidate is open (P16 records the observation). Filtering there is a one-line addition in `repository-json-up-to` for whoever widens the fence (the web UI's branch list would otherwise show `refs/ci/candidate/…` during a run). *[Close-out: done as T4; P16 now asserts both routes.]*
- **P14 — a nuke orphans scratch refs.** `|nuke %urgit-ci` wipes the candidates whose terminal status would have released their `refs/ci/candidate/<id>`; those refs stay in `%urgit` (six were visible after this run's nukes) until the operator `%delete-ref`s them. A consequence of the greenfield state-0 nuke the brief chose, recorded, not fixed.
- **CI-PROJECT-1 — `prereq-outputs`.** Carried in the assignment (`{"plan": {"suite": "true"}}` in P15), mapped to `--env NEEDS_PLAN_OUTPUTS_SUITE=true` (seen in the daemon log), unit-tested (`TestPrereqEnv`); no ERPit step reads one, so the mapping has no live consumer in P1.
- **Harness bugs met and fixed on the way (all script fixes, none typed by hand):** `set -e` + `pipefail` on the intentionally refused push; `pkill -f` matching the calling shell (twice); `$!` of a backgrounded and-list; the daemon's appended log read across restarts (`runner_log` now reads from the last banner *[close-out: from the run's first line, see below]*); a `case` pattern taken from a variable never matching `|` alternatives (`wait_cand`/`wait_att` now use `=~`); `dojo_value` reading 8 pane lines (nine attempt ids wrap); `nuke-revive` waiting for a reload a no-op commit never prints; the dojo's `\'` and `<|…|>` renderings.
- **README** — `runner/README.md` follows the writing rules (0 semicolons outside code, 0 contractions, 0 sentences over 25 words, checked by script); the spec's four config keys are named first.

### Close-out (T1–T5)

Everything the close-out typed that a script did not is here, with the script fix that replaced it. The P1 build's Deviations above stand as its derivation record; nothing in them was reversed.

- **T1 — the static `act` is built by `act-static.sh`.** `CGO_ENABLED=0 GOBIN=$TMP/act-static go install github.com/nektos/act@v0.2.89`; `--version` must read `act version 0.2.89` and `file` must say `statically linked`; a binary already passing both is kept. The go-built binary (sha256 `ecf7a0b2…`) replaced the release tarball's (`6be37b10…`) that the P1 build had unpacked by hand; `p-setup.sh` calls the script before its assert.
- **T2 — `mutants.sh` refuses outside a git work tree** (both harnesses), before any edit.
- **T3a — the RED phase reverts on EXIT.** `negatives.sh red` traps EXIT and runs `mutants.sh revert`, so a phase that dies mid-row leaves the tree clean (the desk on the ship and the daemon binary stay mutated until GREEN rebuilds them, the normal path). Seen live three times in the shakedowns: each `NOT RED` ended with the trap's `reverted: … clean`.
- **T3b — RED needs the mutant's own tripwire.** `mutants.sh tripwire <row>` names the substring the sabotaged build must produce in the row's log; a row that FAILS without it is `FAIL for the WRONG reason` and the phase is `NOT RED`. The oracle earned its keep in the first shakedown: P7 was red for the wrong reason (the ghost daemon, below) and P13-overlap's collision took shapes the table did not yet name (below).
- **T3c — `go test -race`, the live Docker boundary test, the static binary.** `runner/internal/sandbox/docker_live_test.go` (astra's, adapted: `NewDocker(host)`, `Orphans`, `Run` with a work dir, the env var `URGIT_DOCKER_HOST` after this branch's `docker_host` key) runs in `foreground.sh` against the harness's rootless socket; `foreground.sh` also runs `go test -race ./...`, builds the static binary and `file`s it, and counts every Go step's exit code.
- **T4 — `refs/ci/*` hidden from the authenticated API too.** The D9 filter moved from `public-repository-json-up-to` into `repository-json-up-to`, which the authenticated `/apps/urgit/api/repository/<name>` reaches directly and the public route reaches through it; the public arm keeps nothing of its own. P16 asserts both routes while the candidate is open and after it closes. The only Hoon change of the close-out.
- **The previous run's rootless daemon was still up on the footer's data root.** The P1 build left its dockerd (state dir `/run/user/1000/ci-p1-opus/`) running, as its brief asked, on `<worktree>/.scratch/tmp/docker-data` — the data root this footer reuses. Stopped with the committed `docker-rootless.sh stop` (under the old `DOCKER_STATE`, before the footer rewrite); `stop` now waits for the pid, and `start` refuses when another dockerd's `/proc/<pid>/cmdline` names the data root, printing its exec root.
- **The footer rewrite.** `SHIP=peg PORT=8350 PIER=/var/home/michael/piers/urgit-ci-p1-peg` in `ci-p0/env.sh`, `DOCKER_STATE=/run/user/1000/ci-p1-closeout` in `ci-p1/env.sh`; the one hard-coded `~ryp` (`foreground.sh`'s value-dump reader) became `~$SHIP`.
- **`cold.sh`.** One command for the whole cold battery (boot → rootless start → P0 battery → P1 battery), tee'd to `$TMP/cold.log`, run detached with stdin closed and no controlling tty (the orchestrator's finding 4).
- **The pane is narrow, and its width changed under the run.** The P1 build's dojo pane was wide enough for every value on one row; this session's herdr workspace was 109 columns at launch and 74 after the operator's window was resized mid-shakedown (the provider-side retry). At 106 the dojo hard-wrapped every echoed scry longer than the row (`verdict-reason:(need …)` is 114 characters) and the signed URL of H15; at 74 it also pretty-printed most units (`[ ~` / `  '…'` / `]`) and the mint's `[%ci-enroll-token 0v…]`. What went red before the fixes, in order: P0 GREEN H15 `same-class URL: FAIL (observed: )` (first cold run); `cand_reason` → `noun))[~ 'landed']` (reader test); GREEN P19 `landing refused …: FAIL (observed: [ ~  'CI protection … in thisrelease'` and `CI protection refused for the linked repo: FAIL (observed: /app/urgit-ci/hoon:<[346 5].[353 13]>` (shakedown 3, after the resize); GREEN's daemon starting with an empty token, `no state file and no enroll_token` (shakedown 2); H6 `TOKEN=` empty (second cold run). Fixes: `dojo.sh` reads `herdr pane read --source recent-unwrapped` (the terminal's hard wraps undone at the source; the dojo's pretty-printer splits only at structure, never inside a cord) and terminates the last row with a newline (the unwrapped snapshot has none, and `~peg:dojo>P19: PASS` had glued a verdict onto the prompt); `dojo_value` (both libs) still skips rows that continue the echoed command; `unit_join` squeezes the pretty unit form for `cand_reason`/`att_job`/`att_reason`/`att_finished`; `dojo_unit_cord` (P0 lib) joins the signed URL for `h15.sh` and the P0 negatives' `sign_get`; the three mint readers (`mint.sh`, H6, H12) read 30 rows and take the token on the row after `ci-enroll-token`, and `mint.sh` fails loudly, its callers stopping with it; P1's and P19's refusal reads and the P0 negatives' one-line readers use wider windows (60, 8); H4/H5's candidate dumps read 40 rows and take the last `candidate=` line; `boot.sh`'s `+code` reads 10.
- **Vane slogs inside a value.** `gall: got old %wake for %urgit-ci` — behn timers of attempts a `|nuke` orphaned, firing for an hour after every P14 — landed between `[~ 'a']` and the prompt during a shakedown's P20 and the joining reader returned `a']gall: …` (`a ran once, in its own attempt: FAIL (observed: 0)`). No dojo value starts `<vane>: `, so both `dojo_value`s and `dojo_unit_cord` drop such rows.
- **`runner_log` cut at the wrong line.** It read the daemon's log from the last startup banner, but `daemon.New` logs `enrolling with the ship` / `enrolled as daemon` / `state file … present` BEFORE `main` prints the banner, so P2's `enrolled` and `restart reads the state file` could not pass as committed (P2's recorded PASS predates that reader change). It now cuts at the run's first line.
- **Single-row scripts exit non-zero on a failed row.** `p1.sh`, `p2.sh`, `p3-5.sh`, `p15.sh` ended in `end_row` (exit 0 whatever the verdict), so `battery.sh` could not stop at them; they end in `[ "$NFAIL" = 0 ]` now. `p-setup.sh` stops unless the repository's creation answers `201`: on a reused ship an existing `ci-p1` turned P1's refusal into acceptance for the wrong reason.
- **The RED phase's re-enrollment met the ghost daemon.** The first shakedown's RED P7 failed without its tripwire: the phase re-enrolled `a` as a new identity 20 s after P20's daemon (p15-prep's, capacity 3) last polled; the old record won D6's oldest-enrolled tie and swallowed P7's plan, and the P11 mutant closed that plan `%success` at its deadline — candidate `%passed`, `landed`, no `fail` job ever ran (the RED daemon's log shows its first assignment 18 minutes later, for P8). The author's D6 deviation names the ghost; the negatives' "fresh enrollment per phase" recreated it whenever P1 ran fast. `negatives.sh` now keeps daemon `a`'s identity when the ship still knows it (a restart at capacity 1; the poll reports the capacity) and enrolls afresh only when the restarted daemon exits `enrollment lost` (the previous phase's P14 wiped the daemons — no ghost then).
- **P13-overlap's collision has three shapes.** The brief's live proof was `exitcode '137'` (force-removed mid-run); the P1 build's RED and this close-out's second shakedown met the create-time conflict (`… is already in use by container …`); the first shakedown met a third: the loser's act reached `failed to copy content to container: Error response from daemon: No such container: …` (its container force-removed between create and copy) and ended `jobResult "failure"` after 15 ms. The row prints the shape it finds in the two act streams and the daemon log, then the line (the first print cut the line at 240 characters, before the phrase — the long container name sits in front of it); the tripwire accepts `exitcode '137'`, `is already in use by container`, `No such container`, `is not running`.
- **Shakedowns before the record.** Two cold runs stopped in the P0 battery (GREEN H15, then H6 — the pane); between and after them the P1 harness was shaken down on the same ship, all rows but P15, until the reader and ghost defects above were gone; the record below is the third cold run, from a fresh pier, nothing typed.

## Mutant pairs (P1 negative rows: P1, P7–P12, P13-overlap, P14, P17–P20)

`.scratch/ci-p1/mutants.sh apply` puts thirteen one-line sabotages into one build (six files: `app/urgit-ci.hoon`, `lib/ci-plan.hoon`, `app/urgit.hoon`, `runner/internal/{daemon/daemon.go,ship/client.go,plan/plan.go}`); `negatives.sh red` rebuilds the desk and the daemon and runs every row on a fresh repository, keeping daemon `a`'s identity (Deviations), and now (T3a/T3b) reverts the working tree on EXIT and counts a row RED only when it FAILS **and** its log carries `mutants.sh tripwire <row>` — the exact substring the sabotaged build must produce; `negatives.sh green` reverts, rebuilds and repeats. Both tails from `.scratch/tmp/negatives-red.log` / `negatives-green.log` (run 2026-09-15 21:35–22:18):

```
== build under test: MUTATED; rows must FAIL
-- daemon a kept its identity; capacity 1 reported on its poll
P1 [red]: FAIL, tripwire present: %set-ci-protected on a ref with no tip: accepted (>=)
P7 [red]: FAIL, tripwire present: fail job %failed: FAIL (observed: %passed
P8 [red]: FAIL, tripwire present: no job attempt ran: FAIL (observed: 0v
P9 [red]: FAIL, tripwire present: candidate %failed: FAIL (observed: %passed
P10 [red]: FAIL, tripwire present: attempt %infrastructure-error: FAIL (observed: %passed
P11 [red]: FAIL, tripwire present: closed at the deadline: FAIL (observed: %passed
P12 [red]: FAIL, tripwire present: advertised capacity now 2
P13-overlap [red]: FAIL, tripwire present: exitcode '137'
P17 [red]: FAIL, tripwire present: verdict-reason: FAIL (observed: 'candidate object is missing from the store'
P18 [red]: FAIL, tripwire present: no credentials -> 401: FAIL (observed: 200
P19 [red]: FAIL, tripwire present: linked repo not protected: FAIL (observed: %.y
P20 [red]: FAIL, tripwire present: event job does not match the assignment
P14 [red]: FAIL, tripwire present: daemon exited non-zero: FAIL (observed: running pid
== red: FAIL-with-tripwire=13 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P19 P20 P14) FAIL-wrong-reason=0 () PASS=0 () of 13 rows; build: MUTATED
RED: every row fails under the mutant build, each for its own mutant's reason
== red phase exit (0): reverting the mutants in the working tree
reverted: desk/app/urgit-ci.hoon desk/lib/ci-plan.hoon desk/app/urgit.hoon runner/internal/daemon/daemon.go runner/internal/ship/client.go runner/internal/plan/plan.go clean
== build under test: clean (real build); rows must PASS
-- the ship no longer knows daemon a (enrollment lost; re-enroll with a fresh token): enrolling afresh
P1 [green]: PASS
P7 [green]: PASS
P8 [green]: PASS
P9 [green]: PASS
P10 [green]: PASS
P11 [green]: PASS
P12 [green]: PASS
P13-overlap [green]: PASS
P17 [green]: PASS
P18 [green]: PASS
P19 [green]: PASS
P20 [green]: PASS
P14 [green]: PASS
== green: PASS=13 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P19 P20 P14) FAIL=0 () of 13 rows; build: clean (real build)
GREEN: every row passes on the real build
```

What turned each row red — the tripwire (the sabotaged build's own answer, as the row logs it) and the row's other failing checks, from `.scratch/tmp/neg-red-<row>.log`:

- **P1** — `%set-ci-protected` accepted before the seed push. Tripwire `%set-ci-protected on a ref with no tip: accepted (>=)`: the dojo answered the poke with a bare `>=` where the real build prints the crash trace with `ref has no tip; …`.
- **P7** — every job result `%passed`. Tripwire `fail job %failed: FAIL (observed: %passed`; also `candidate %failed: FAIL (observed: %passed)`, `reason: FAIL (observed: landed)`, `master unchanged: FAIL` — the failing commit landed.
- **P8** — matrix accepted. Tripwire `no job attempt ran: FAIL (observed: 0v` (job `m` got an attempt `0v6.aanct…`); also `plan attempt reason: FAIL (observed: ~)`, candidate `%passed`, `'landed'`.
- **P9** — unsupported `if` accepted at validation. Tripwire `candidate %failed: FAIL (observed: %passed`; the plan attempt's reason `~`, job `u` got a (skipped) attempt.
- **P10** — `/abandon` closed `%job-result %success`. Tripwire `attempt %infrastructure-error: FAIL (observed: %passed`; also `closed within 5 s: FAIL`, `reason … abandon: FAIL (observed: ~)`, `candidate %unknown: FAIL (observed: %passed)`.
- **P11** — the deadline wake closed `%job-result %success`. Tripwire `closed at the deadline: FAIL (observed: %passed`; `reason is the deadline's: FAIL (observed: ~)`; candidate not `%unknown`.
- **P12** — quarantine without the capacity decrement. Tripwire `advertised capacity now 2`, on the daemon's own line: `QUARANTINED slot ci-0v2.r8tmp…: teardown failed: docker network: p12: network rm refused by the harness; advertised capacity now 2`.
- **P13-overlap** — unprefixed projection `name:`. Tripwire `exitcode '137'` — the brief's own shape this time: the loser's act stream carries `"msg":"exitcode '137': failure\nError response from daemon: RWLayer of container a6e73762… is unexpectedly nil …"` (its job container force-removed by the other attempt's act, which owned the same name); `two job containers alive at once: FAIL (observed: 0)`, `projection names differ …: FAIL (observed: no: [~ 'fixture-wait'])`. The co-applied P7 mutant recorded the loser's `failure` as `%passed`, as in the build's run.
- **P14** — the poll's 401 read as "nothing assigned". Tripwire `daemon exited non-zero: FAIL (observed: running pid` (pid 1875387 still polling); `daemon logged the enrollment loss: FAIL`, `exit status 3: FAIL (observed: )`.
- **P17** — the expected-tip compare skipped. Tripwire `verdict-reason: FAIL (observed: 'candidate object is missing from the store'` — `apply-receive`'s own old-tip check still refused, with the wrong reason; master stayed Y.
- **P18** — `write-authorized` skipped. Tripwire `no credentials -> 401: FAIL (observed: 200`; also `wrong write token -> 401: FAIL (observed: 200)` on `POST git-receive-pack` (git's own push still fails at `info/refs`).
- **P19** — desk-linked repo accepted for CI protection. Tripwire `linked repo not protected: FAIL (observed: %.y`; the poke answered `>=` instead of the refusal.
- **P20** — act on the unprojected file. Tripwire `event job does not match the assignment`, in the daemon's log: `[job 0v6.0drf1…] ship refused event: {"error":"event job does not match the assignment"}` — fifteen times; `no jobID a line in b's stream: FAIL (observed: 15)`.

GREEN's P13-overlap, the row the build's table carries under P13-overlap: `docker ps` showed **2** `act-0v6-31c09-…-fixture-wait-wait-<hash>` / `act-0vrcav-093o5-…-fixture-wait-wait-<hash>` containers at once, both attempts `%passed`, projection names `<attempt>/fixture-wait` differ, plan `name` = `fixture-wait`, `act collision evidence …: none`, both candidates `%passed`.

## Foreground

`.scratch/ci-p1/foreground.sh` (`.scratch/tmp/foreground.log`, 22:18:37–22:19:04): every `+urgit!*-vector` generator (nine assert a loobean and print `%.y`; fourteen dump values and must build and print), `cd fe && npm test` untouched, then in `runner/`: `go test ./...`, `go test -race ./...`, the live Docker boundary test against the harness's rootless socket (T3c), `go vet` + `gofmt`, the static binary and `file` on it:

```
== +urgit!ci-event-vector -> %.y
== +urgit!ci-plan-vector -> %.y
== +urgit!ci-storage-vector -> %.y
== +urgit!git-access-vector -> %.y
== +urgit!git-archive-vector -> %.y
== +urgit!git-blame-vector -> printed (value dump, no loobean verdict): [%.y %.y %.y ~]
== +urgit!git-catalog-vector -> %.y
== +urgit!git-clay-vector -> printed (value dump, no loobean verdict): ['dab4df3403f839b957e8d21edd6be4e8222fc7e7' %.y]
== +urgit!git-codec-vector -> printed (value dump, no loobean verdict): ]
== +urgit!git-delta-pack-vector -> printed (value dump, no loobean verdict): ]
== +urgit!git-github-vector -> %.y
== +urgit!git-gzip-vector -> printed (value dump, no loobean verdict): [%noun 113 105 131 1.032.599.767 %.y]
== +urgit!git-inflate-vector -> printed (value dump, no loobean verdict): gall: got old %wake for %urgit-ci
== +urgit!git-migration-vector -> %.y
== +urgit!git-ofs-delta-pack-vector -> printed (value dump, no loobean verdict): [decoded=%.y object-count=6]
== +urgit!git-pack-decode-vector -> printed (value dump, no loobean verdict): [decoded=%.y object-present=%.y exact=%.y]
== +urgit!git-pack-vector -> printed (value dump, no loobean verdict): ]
== +urgit!git-shallow-vector -> printed (value dump, no loobean verdict): [%.y %.y %.y %.y %.y %.y %.y %.y %.y %.y ~]
== +urgit!git-stock-pack-vector -> printed (value dump, no loobean verdict): ]
== +urgit!git-storage-vector -> printed (value dump, no loobean verdict): ]
== +urgit!git-tree-vector -> printed (value dump, no loobean verdict): ['b6858679a5e51534889504e05ebdafba12f1d654' %.y %.y]
== +urgit!git-webhook-vector -> %.y
== +urgit!git-zlib-vector -> printed (value dump, no loobean verdict): [jetted=%.y output-matches=%.y consumed-all=%.y]
== cd fe && npm test
# tests 126
# pass 126
# fail 0
== go test ./... (runner/)
ok  	urgit/runner/internal/act	(cached)
ok  	urgit/runner/internal/daemon	(cached)
ok  	urgit/runner/internal/plan	(cached)
ok  	urgit/runner/internal/relay	(cached)
ok  	urgit/runner/internal/sandbox	(cached)
== go test -race ./...
ok  	urgit/runner/internal/act	(cached)
ok  	urgit/runner/internal/daemon	(cached)
ok  	urgit/runner/internal/plan	(cached)
ok  	urgit/runner/internal/relay	(cached)
ok  	urgit/runner/internal/sandbox	(cached)
== live Docker boundary (URGIT_DOCKER_HOST=unix:///run/user/1000/ci-p1-closeout/docker.sock)
=== RUN   TestDockerLiveBoundary
--- PASS: TestDockerLiveBoundary (0.84s)
PASS
ok  	urgit/runner/internal/sandbox	0.844s
== go vet, gofmt
vet ok, gofmt clean
== static binary
== file
/var/home/michael/workspace/urbit/urgit-ci-p1-opus/.scratch/tmp/urgit-runner-checked: ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, Go BuildID=…, with debug_info, not stripped
== vectors failing: 0; go steps failing: 0
```

(`git-inflate-vector`'s dump line shows a `gall: got old %wake` slog that landed after the value — a display nit in the value-dump reader, which now drops vane slogs; the vector built and printed, and its verdict was never a loobean. The `(cached)` results are the same sources the shakedowns compiled minutes earlier; the boundary test runs uncached by `-count=1`.)

## Shutdown

`.scratch/ci-p0/shutdown.sh` (pane `w19:p4`, the one `boot.sh` split), 22:20:52, two minutes after the battery: both `urbit` processes found by `/proc/<pid>/cmdline` naming the binary and the pier, `ctrl+d` to the dojo, pids gone, pane closed by id. The pier stays on disk; the rootless Docker daemon (`/run/user/1000/ci-p1-closeout/docker.sock`, `name=rootless`, dockerd pid 1713930) is left running with no `ci-*` network, volume or container behind; daemons `a` and `b` were already stopped by their rows (`not running`); no other pier was touched.

```
== processes whose /proc/<pid>/cmdline is /var/home/michael/workspace/urbit/bin/urbit … /var/home/michael/piers/urgit-ci-p1-peg:
1711267  /var/home/michael/workspace/urbit/bin/urbit -F peg -B /var/home/michael/workspace/urbit/pills/brass-408k-1.pill --http-port 8350 -c /var/hom
1711463  /var/home/michael/workspace/urbit/bin/urbit work --snap-dir /var/home/michael/piers/urgit-ci-p1-peg --runtime-config 128 --temporary-cache-s
== ctrl+d to the dojo in pane w19:p4
== all pier pids gone; waiting for the shell prompt, then closing the pane
{"id":"cli:pane:close","result":{"type":"ok"}}
pier retained: /var/home/michael/piers/urgit-ci-p1-peg (514M)
```
