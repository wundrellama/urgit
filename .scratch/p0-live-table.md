# P0 live table — native CI contracts and harness

Ship `~sev` (Vere 4.6, brass-408k-1), HTTP port 8345, pier
`/var/home/michael/piers/urgit-ci-p0-sev`, herdr pane `wT:p5`. Booted,
installed, driven and shut down entirely by the scripts in `.scratch/ci-p0/`
(`boot.sh`, then `battery.sh` = `setup.sh` … `h15.sh`, `negatives.sh red`,
`negatives.sh green`, `foreground.sh`, then `shutdown.sh`); nothing was typed
into the dojo by hand. Desk built with `zig build -Ddesk=<pier>/urgit`,
`|commit %urgit`, `|install our %urgit` (boot.sh). `act` 0.2.89, image
`catthehacker/ubuntu:act-latest`, `--network bridge --json --pull=false`, on
the host's default (rootful) Docker daemon. Run on 2026-09-13, launched
00:08:08 CDT, shut down 00:14:25 CDT; raw logs in `.scratch/tmp/` (ignored), one
per step (`setup.log`, `h1.log`, …, `negatives-red.log`, `negatives-green.log`,
`foreground.log`, `battery.log`, `shutdown.log`).

Fixture repository `ci-fixture` (public-read), `refs/heads/master` protected
(today's rule) and CI-protected (D3). Commits:

| name | OID | role |
|---|---|---|
| ONE | `0458fef6d0c4ca556f986eba53b1e6c306ff7753` | workflows + README, landed before CI protection |
| TWO | `1881914782287f50f78456eedbd8501bdc0a8b0f` | landed before CI protection; the base tip for H3–H5 |
| THREE | `6d45609974f3daaa9150bc447a7ffe9cf5ddf8e6` | H3 direct push; fast-forward candidate `0v7.3k0sm.da15p.odf20.pt1i3.s06ra` |
| DIVERGE | `e8596af6106448c7b3d7858753418bb5e51e34af` | H5 head based on ONE, force-pushed; candidate `0v5.s750f.qbnki.p60s0.kii4l.8h37v` |
| MERGE | `587258b028c9bcc01e497386f0f85c48d0b82762` | H5 materialized merge of DIVERGE onto TWO |
| FOUR | `0ecc64ad4db407ead3ec86c466631f2ac62ff55f` | H14 passed candidate `0v6.3qf70.cf12t.kt14j.oivh1.48nk0` |

Daemon A `0v6.dn2lh.mt76e.qmmu6.0nabh.dd3rl` (H6), daemon B
`0vfo0se.kfgrd.r5b29.gsab1.s8o7u` (H12). Attempts: A1
`0v6.af29l.2nqlb.mqgep.m1bm8.vfo4i` (H7/H8/H15), A2 `0v6.lkjc2…` (H9/H13/H12),
A3 `0v3.e19ca…` (H10), A4 `0v3.mtamq…` (H11), A5 `0v6.3pah8…` (H14). The
negative rows of the mutant phases stage their own commits and attempts (see
"Mutant RED phase").

## Rows

"RED shown?" gives the refusal output observed verbatim for each negative
row, and for the rows with a natural before/after inside the harness the
before state as well. The mutant RED phase below is the second, independent
RED for H9–H15.

| Row | Expect | Observed | RED shown? |
|---|---|---|---|
| H1 | `%set-ci-protected ci-fixture refs/heads/master` before `%storage` is configured → refused with the D9 message | `:urgit-ci &ci-action [%set-ci-protected 'ci-fixture' 'refs/heads/master' %.y]` → `/app/urgit-ci/hoon:<[243 5].[250 13]>` … `'ship object storage is not configured; CI cannot be enabled'` / `dojo: app poke failed`; D3 scry stays `%.n` (h1.sh) | yes: the quoted crash line and `dojo: app poke failed` |
| H2 | after `%storage` is configured (six `%storage-action` pokes, endpoint `http://127.0.0.1:1`, bucket `ci-bucket`, service `%credentials`) → accepted | each poke `[0 %avow 0 %noun %poked %storage %storage-action]`; `%set-ci-protected` poke acked `>=`; `.^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/noun)` → `%.y`; the same for `refs/heads/side` → `%.n` (h2.sh) | n/a (positive); H1 is its before |
| H3 | push THREE → `ng refs/heads/master staged as ci candidate <id>; checks pending`; ref unchanged; objects kept | `! [remote rejected] master -> master (staged as ci candidate 0v7.3k0sm.da15p.odf20.pt1i3.s06ra; checks pending)`; master `1881914…` (TWO) before and after; objectCount 10 → 13 | yes: the `! [remote rejected]` line |
| H4 | `%materialize` → `%candidate-ready` with `candidate == head` | after `:urgit-ci &ci-action [%materialize 0v7.3k0sm…]`: `candidate=[~ 0x6d45.6099.74f3.daaa.9150.bc44.7a7f.fe9c.f5dd.f8e6]` = head (THREE), `conflict=%.n`, `status=%pending`, `attempts=~`; master still TWO (h4.sh) | n/a |
| H5 | diverging push from a second clone → staged; materialize → two-parent commit, parents correct, ref unchanged | clone-b reset to ONE, force-push DIVERGE: `! [remote rejected] master -> master (staged as ci candidate 0v5.s750f.qbnki.p60s0.kii4l.8h37v; checks pending)`; materialized `candidate=[~ 0x5872.58b0.28c9.bcc0.1e49.7386.f0f8.5c48.d0b8.2762]` ≠ head; exposed on `refs/ci/candidate` via `%set-ref` and fetched: `git rev-list --parents -n 1` → `587258b0… 18819147… e8596af6…` (first parent TWO, second DIVERGE); master still `1881914…`; objectCount 18 | yes (from the original run, 2026-09-12): a clone-b based on the tip was correctly judged a fast-forward (`candidate == head`), which forced the divergent rewrite the script now does |
| H6 | mint → enroll → daemon-id; long-poll → 204; `%assign` by poke; long-poll → the assignment | `:urgit-ci\|mint-enroll-token` printed `[%ci-enroll-token 0ve.60m8c…592og]`; `POST /ci/daemon/enroll {token}` (no session) → `200 {"daemon-id":"0v6.dn2lh.mt76e.qmmu6.0nabh.dd3rl","bearer":"0v1n.nf0ve…c6bns"}`; re-enroll same token → `401 enroll token is not recognized`; poll with no credentials → `401 daemon authentication required`; wrong bearer (`…c6bn0`) → `401 daemon authentication required`; the bearer → `204` after 25 s; after `[%assign 0v7.3k0sm… 0v6.dn2lh… ~]` the poll answered `200 {"assignment":{… "attempt":"0v6.af29l…","oid":"6d456099…","trust":"trusted","deadline-seconds":3600 …}}` once; the next poll → `204` after 25 s | yes: the three 401s |
| H7 | real `act` on the candidate checkout, each `--json` line relayed by a shell loop; `outputs.suite == 'true'` | `act push -W .github/workflows/fixture-pass.yml -j pass …` on THREE → exit 0, 15 lines, all `202`; attempt A1: `status=%running events=15 job-result=[~ %success]`, `outputs={[p='suite' q='true']}` | n/a |
| H8 | `POST /result job-result success` → candidate `%passed`; push the candidate OID → lands | `200 {"status":"passed",…,"candidate-status":"passed"}`; candidate `status=%passed candidate=[~ 0x6d45…f8e6]`; D2 scry `.^(? %gx /=urgit-ci=/eligible/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/(scot %t '6d456099…')/noun)` → `%.y`; `git push` → `1881914..6d45609  master -> master`; master oid = THREE | n/a |
| H9 | `/result success` with no `jobResult` event → 409; candidate stays `%pending` | new attempt A2 on the merge candidate, no events: `409 {"error":"no jobResult event was relayed for this attempt"}`; candidate `status=%pending` | yes: the 409 line; and the mutant phase (H9 red: `200`, `%passed`) |
| H10 | deadline passes with no result → attempt `%infrastructure-error`, candidate `%unknown`; push → still `ng` | `[%assign … \`~s20]` → assignment `"deadline-seconds":20`; before: attempt A3 `status=%running events=0 job-result=~`, candidate `%pending`; after 28 s: `status=%infrastructure-error events=0 job-result=~`, candidate `status=%unknown`; `git push --force origin 587258b0…:refs/heads/master` → `! [remote rejected] 587258b0… -> master (staged as ci candidate 0vr6gub.cd5v5.1vho4.kpqsb.q6ag7; checks pending)` | yes: before/after states and the rejected push; and the mutant phase (`%passed`, push `ok`) |
| H11 | `fixture-fail.yml` → `jobResult: failure` → candidate `%failed`; push → `ng` | act on MERGE with `-j fail`: 26 lines, last `202 … "job-result":"failure"`; claiming success → `409 {"error":"job-result does not match the relayed jobResult event"}`; honest `failure` → `200 … "candidate-status":"failed"`; candidate `status=%failed`; push → `! [remote rejected] … (staged as ci candidate 0vr6gub…; checks pending)` | yes: the 409 for the false claim and the rejected push; and the mutant phase (`%passed`, push `ok`) |
| H12 | event to attempt A with B's bearer → 401 | daemon B enrolled (`0vfo0se…`); `POST /ci/attempt/<A2>/event` with `x-ci-bearer: <B>` → `401 {"error":"attempt authentication required"}`; same line with A's bearer → `202 … "events":1` | yes: the 401 line; and the mutant phase (`202`, events 1) |
| H13 | 65 KiB event line → 413; attempt unaffected | 66,634-byte line → `413 {"error":"event line exceeds 64 KiB"}`; attempt A2 afterwards `status=%running events=0 job-result=~` (the next accepted line, H12's, then answered `"events":1`) | yes: the 413 line; and the mutant phase (`202`, events 1) |
| H14 | agent stopped → push a `%passed` OID → `ng` with the D2 outage reason; push an unprotected ref → `ng`; restart → both `ok` | FOUR staged/materialized/assigned/run/passed like H3–H8 (A5, 15 lines, `200 … "candidate-status":"passed"`); `\|rein %urgit [%.n %urgit-ci]` → `.^(? %gu /=urgit-ci=/$)` → `%.n`; `git push origin master` → `! [remote rejected] master -> master (ci: %urgit-ci is not running; protected-ref writes are refused until it is)`; `git push origin master:refs/heads/side` → `! [remote rejected] master -> side (ci: %urgit-ci is not running; protected-ref writes are refused until it is)`; `\|rein %urgit [%.y %urgit-ci]` → `gall: bumped %urgit-ci`, `%gu` `%.y`; both pushes → `6d45609..0ecc64a  master -> master` / `* [new branch] master -> side`; master = side = `0ecc64a…` | yes: both rejected lines; and the mutant phase (both pushes `ok` while stopped). The original run's RED (`\|suspend %urgit-ci` is a no-op; `\|rein` is the single-agent form) stands |
| H15 | sign a GET for `%untrusted` from the `%trusted` attempt → `~` | attempt A1 `trust=%trusted`; `.^((unit @t) %gx /=urgit-ci=/sign-get/0v6.af29l…/untrusted/(scot %t 'cache.tar')/noun)` → `~`; `…/trusted/…` → `[~ 'http://127.0.0.1:1/ci-bucket/ci/ci-fixture/0v7.3k0sm.da15p.odf20.pt1i3.s06ra/0v6.af29l.2nqlb.mqgep.m1bm8.vfo4i/trusted/cache.tar']` | yes: the `~`; and the mutant phase (an `…/untrusted/cache.tar` URL) |

## Mutant RED phase (negatives.sh)

`negatives.sh red` applied the seven one-line guard removals below with
`mutants.sh apply`, rebuilt and committed the desk once (`rebuild.sh`:
`gall: reloading %urgit — seen`, `gall: reloading %urgit-ci — seen`, both
`%gu` → `%.y`), and ran H9–H15 against fresh commits: every row FAILED.
`negatives.sh green` reverted the four files with `git checkout --`
(`mutants.sh status` → `clean (real build)`), rebuilt and committed once more
(both agents reloaded again), and ran the same rows: every row PASSED. The
mutations were never committed; `git status` was clean afterwards. Verbatim
check lines from `negatives-red.log` / `negatives-green.log`:

| Row | Mutation (this branch's guard) | RED — mutant build | GREEN — real build |
|---|---|---|---|
| H9 | `handle-result`: the "no jobResult event was relayed" 409 becomes acceptance (close passed, answer 200) | `status: FAIL (observed: 200, expected: 409)`; `candidate: FAIL (observed: %passed, expected: %pending)` | `status: PASS (observed: 409)`; `candidate: PASS (observed: %pending)` |
| H10 | `on-arvo` `%deadline` wake closes `[%job-result %success]` instead of `%infrastructure-error` | `attempt: FAIL (observed: %passed, expected: %infrastructure-error)`; `candidate: FAIL (observed: %passed, expected: %unknown)`; `push: FAIL (observed: ok, expected: rejected)` | `attempt: PASS (observed: %infrastructure-error)`; `candidate: PASS (observed: %unknown)`; `push: PASS (observed: rejected)` (`staged as ci candidate 0v2.kqr4b…; checks pending`) |
| H11 | `close-attempt`: every job result is `%passed` | `result accepted: PASS (observed: 200)`; `candidate: FAIL (observed: %passed, expected: %failed)`; `push: FAIL (observed: ok, expected: rejected)` | `result accepted: PASS (observed: 200)`; `candidate: PASS (observed: %failed)`; `push: PASS (observed: rejected)` |
| H12 | `attempt-authorized`: the daemon bearer check is `%.y` | `status: FAIL (observed: 202, expected: 401)`; `events on A: FAIL (observed: 1, expected: 0)` | `status: PASS (observed: 401)`; `events on A: PASS (observed: 0)` |
| H13 | `max-line` `65.536` → `10.000.000` (lib/ci-event) | `status: FAIL (observed: 202, expected: 413)`; `events unchanged: FAIL (observed: 1, expected: 0)` | `status: PASS (observed: 413)`; `events unchanged: PASS (observed: 0)` |
| H14 | `ci-gate-error`: the liveness outage branch returns `~` (fail open) | liveness `%.n`; `protected push while stopped: FAIL (observed: ok, expected: rejected: ci: %urgit-ci is not running; protected-ref writes are refused until it is)`; `unprotected push while stopped: FAIL (observed: ok, expected: rejected: …)`; after restart both `PASS (observed: ok)` | liveness `%.n`; `protected push while stopped: PASS (observed: rejected: ci: %urgit-ci is not running; protected-ref writes are refused until it is)`; `unprotected push while stopped: PASS (observed: rejected: ci: %urgit-ci is not running; …)`; after restart both `PASS (observed: ok)` |
| H15 | `sign-get`: `?. =(requester trust) ~` → `?. %.y ~` (lib/ci-storage) | `cross-class: FAIL (observed: [~ 'http://127.0.0.1:1/ci-bucket/ci/ci-fixture/0v6.misq6…/0v4.c1bgc…/untrusted/cache.tar'], expected: ~)`; `same-class URL: PASS` | `cross-class: PASS (observed: ~)`; `same-class URL: PASS (observed: [~ 'http://127.0.0.1:1/ci-bucket/ci/ci-fixture/0v1.42bad…/0v5.4ot1k…/trusted/cache.tar'])` |

Phase verdicts: `== red: PASS=0 () FAIL=7 (H9 H10 H11 H12 H13 H14 H15) of 7
rows; build: MUTATED` → `RED: every row fails under the mutant build`;
`== green: PASS=7 (H9 H10 H11 H12 H13 H14 H15) FAIL=0 () of 7 rows; build:
clean (real build)` → `GREEN: every row passes on the real build`.
Under the mutant, H10's and H11's pushes land (master advances to the row's
OID), which is the RED; each later row stages its own commit on whatever
master then is, so the phases stay independent.

## Foreground suites (verbatim tails)

```
> +urgit!ci-event-vector
%.y
> +urgit!ci-storage-vector
%.y
> +urgit!git-migration-vector
%.y
> +urgit!git-access-vector
%.y
```

```
$ cd fe && npm test
1..126
# tests 126
# suites 0
# pass 126
# fail 0
# cancelled 0
# skipped 0
# todo 0
# duration_ms 2333.544143
```

## Dojo forms that work (D2, D3)

```
=ci -build-file /=urgit=/sur/ci/hoon                                          (prelude.sh; once, before any candidate:ci / attempt:ci cast)
.^(@ud %gx /=urgit-ci=/state/version/noun)                                   → 0
.^(? %gu /=urgit-ci=/$)                                                      → %.y (%.n after |rein %urgit [%.n %urgit-ci])
.^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/noun)                          → %.y
.^(? %gx /=urgit-ci=/ci-protected/(scot %t 'ci-fixture')/(scot %t 'refs/heads/side')/noun)                            → %.n
.^(? %gx /=urgit-ci=/eligible/(scot %t 'ci-fixture')/(scot %t 'refs/heads/master')/(scot %t '6d45609974f3daaa9150bc447a7ffe9cf5ddf8e6')/noun)  → %.y
.^((unit @t) %gx /=urgit-ci=/sign-get/0v6.af29l.2nqlb.mqgep.m1bm8.vfo4i/untrusted/(scot %t 'cache.tar')/noun)         → ~
status:(need .^((unit candidate:ci) %gx /=urgit-ci=/candidate/0v7.3k0sm.da15p.odf20.pt1i3.s06ra/noun))               → %passed
```

`%urgit` builds the same paths programmatically
(`/(scot %p our.bowl)/urgit-ci/(scot %da now.bowl)/eligible/(scot %t repo)/(scot %t ref)/(oid-text:git-codec oid)/noun`);
the receiver decodes `(scot %t …)` segments and takes raw ones as-is. A raw
40-hex OID that starts with a digit, or a dotted name such as `cache.tar`,
typed into the dojo is a path syntax error (the dotted one parks the dojo on
a continuation prompt), which is why the typed forms wrap those in
`(scot %t …)`; a `0v…` candidate or attempt id is a valid atom literal and
needs no wrapping.

## Shutdown

`shutdown.sh`: pier processes found by `/proc/<pid>/cmdline` being the urbit
binary and naming `urgit-ci-p0-sev`: `1267415` (`urbit -F sev -B … --http-port
8345 -c /var/home/michael/piers/urgit-ci-p0-sev`) and `1267751` (`urbit work
--snap-dir /var/home/michael/piers/urgit-ci-p0-sev …`). `ctrl+d` sent to the
dojo in pane `wT:p5`; both `/proc` entries gone afterwards; the pane returned
to the shell prompt (`took 6m14s`) and was closed by id. The pier directory
is retained on disk (223 MB). No other pier was touched.

## Deviations and notes for the record

- Brief re-frozen at `161566d` (Q1: `%ci-action` case on `%urgit`, Q2: `signed-request` return with the SigV4 lifetime, Q3: `group-peek`'s guard and refuse-every-ref outage). Code follows the rulings.
- Eyre reads `Authorization: Bearer` as its own session token (`401 bad session auth` before any agent runs), so the daemon bearer travels in `x-ci-bearer`. Everything else about D6 auth is as written: bearer derived from daemon-id and token, returned once, stored only as a hash, compared as hashes.
- `+urgit-ci!mint-enroll-token` cannot resolve (there is no desk `%urgit-ci`); the generator is `desk/gen/urgit-ci/mint-enroll-token.hoon`, invoked as `:urgit-ci|mint-enroll-token`, which prints the token once and pokes its hash in.
- `|suspend`/`|revive` take a desk; H14 stops the single agent with `|rein %urgit [%.n %urgit-ci]` and restarts it with `[%.y %urgit-ci]`.
- `desk/mar/yml.hoon` (text mark) exists only so `desk/tests/ci/*.yml` can be committed to Clay.
- `desk/lib/ci-candidate.hoon` holds the pure merge-base and fast-forward/merge decision so `materialize-candidate` in `%urgit` stays one short arm that calls `merge-commit:git-tree`.
- `read-settings:ci-storage` re-implements `storage-settings` (an arm inside `%urgit`'s agent core cannot be imported) behind a `%gu` liveness read of `%storage`.
- `%assign` takes an optional deadline (`(unit @dr)`, default `~h1`) so H10 can be run; `%materialize` is the explicit P0 trigger for the `%urgit-ci` → `%urgit` poke direction.
- `upload-pack` serves only objects reachable from an advertised ref, so H5 exposes the merge candidate on `refs/ci/candidate` with the existing `%set-ref` poke to read it from a clone. That ref is harness scaffolding, not part of the contract.
- The first H5 run (2026-09-12) based clone-b on the current tip; the ship judged it a fast-forward (correctly). The row's divergent case needs a head that does not descend from the tip (based on ONE, `--force`).
- The candidate id printed by the push is `(sham [repo ref head base])` computed on both sides; the poke is fire-and-forget, as D4 says.
- Docker on the runner host: the host's default Docker socket may be rootful (`/var/run/docker.sock` owned by root, `docker info` showing no `name=rootless` security option), while the orchestrator's battery ran `act` under a rootless daemon (`docker info` → `name=rootless`). Which socket the harness uses is R3.1-A hygiene for the harness host only; in production the VM wrapper (spec § Execution: one disposable VM per job, the Docker socket belongs to the VM, never to the host) is what makes either acceptable. This run used the default rootful daemon (`docker info`: Server Version 29.7.2, no `name=rootless`; `/var/run/docker.sock` root:docker).
- Close-out (2026-09-13): the record above was right and the committed scripts drifted from it (the orchestrator's battery findings 2–4 plus the ordering note). Fixed so the harness reproduces cold: `prelude.sh` (`=ci -build-file /=urgit=/sur/ci/hoon`, called once by `setup.sh`); `h15.sh` passes `(scot %t 'cache.tar')`; `env.sh` no longer carries `+code` (`boot.sh` reads it into `$TMP/code.txt`); `boot.sh` waits for the shell prompt before typing the boot line and scripts the whole install; `setup.sh` seeds the repo before `h2.sh` CI-protects the ref and says why (CI-EMPTY-REF-1-A, P1 scope; the gate is unchanged).
- Steps that had been typed by hand and are now scripts: `h1.sh`, `h2.sh` (six `%storage-action` pokes: endpoint, both keys, `%set-current-bucket` — which also adds the bucket, so no `%add-bucket` — region, `%toggle-service %credentials`), `h4.sh`, `foreground.sh`, `shutdown.sh`; `battery.sh` runs everything after `boot.sh` with one log per step. `h6b.sh` was folded into `h6.sh` and removed: its first poll assumed an undelivered assignment that `h6.sh` had already taken (it existed only because the original run's `h6.sh` hit the `Authorization: Bearer` 401 and was retried). `h8.sh` wraps the OID in `(scot %t …)`: a raw 40-hex segment parses only when it happens to start with a letter (it did on the battery's `f02c…`; this run's THREE `6d45…` needs the wrap). `h9-13.sh` records `DAEMON_B`.
- Rehearsal: the first cold boot of this footer (2026-09-12, launched 23:51:29, shut down 23:58:28 CDT) ran the whole battery from scripts with the same verdicts (15/15, RED 7/7, GREEN 7/7, suites green) but exposed three observation-level script defects, fixed before the recorded run above: `h6.sh` built its "wrong bearer" as `${BEARER%?}0`, which is the bearer itself whenever the bearer ends in `0` (it did; the check answered `204`); `h9-13.sh`/`h7.sh`/`h15.sh` grepped `status=` etc. out of a whole-noun print, which finds an older candidate's line when the attempt fits on one pane line (an attempt with `outputs={}` does) — replaced by `lib.sh` single-field readers (`status:(need .^(…))`); `h3.sh`'s `master()` grep printed nothing because the API's key order changed — replaced by a JSON read. Also cosmetic: `poke.sh` drops click's boot chatter, `negatives.sh` reads the relay's last non-blank answer, `shutdown.sh` matches only urbit-binary processes. The rehearsal pier was removed and this run booted the footer's pier fresh; its logs are in `.scratch/tmp/rehearsal-sev/` (ignored).
- `negatives.sh` is one script with fresh commits per row (the opus layout) rather than per-row files: the main table's row scripts share state built up by H3–H8, whereas the two phases need rows that do not depend on each other or on the phase before. Under the mutant build H10's and H11's pushes land, so master moves during the RED phase; `stage_commit` fetches master first.
- act on a detached checkout of the merge commit (main H11, clone-b) emits 11 extra `unable to get git ref: failed to identify reference` lines (26 lines vs 15 on a branch checkout in the negatives rows); every line is accepted (`202`) either way.
- The four foreground vectors and `npm test` run from `foreground.sh`; `git status` after the run shows only the close-out's own script edits, and the four mutated Hoon files clean.
