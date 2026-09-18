# CI P2 live table — chair `opus`, cold run on ships `~dep` and `~put`

- Worktree `/var/home/michael/workspace/urbit/urgit-ci-p2-opus`, branch `ci/p2-opus`, base `master` at `46e9616` (re-freeze 4; the build started on re-freeze 1 `4872953` and was rebased under re-freezes 2, 3 and 4 with the stage commits riding above). This record is the cold battery (`.scratch/ci-p2/cold.sh`: `boot.sh` → `boot2` → `docker-rootless.sh start` → `store.sh start` → `ci-p0/battery.sh` → `ci-p1/battery.sh` → `ci-p2/battery.sh`) run detached from fresh piers for both galaxies, nothing typed, 2026-09-16 22:32:52 → 2026-09-17 02:54:08, with the two resumptions the Deviations name (the P2 battery's tail ran twice more on the same ships after a harness bug and re-freeze 4's Q19).
- **First ship** `~dep`, HTTP 8353, pier `/var/home/michael/piers/urgit-ci-p2-dep`, booted by `.scratch/ci-p0/boot.sh` in herdr pane `w1C:p7` (split from `w1C:p1`) with the footer's boot line (no `-p`); `%urgit-ci` state version 0; `+code` recorded by the script. **Second ship** `~put`, HTTP 8355, pier `/var/home/michael/piers/urgit-ci-p2-put`, pane `w1C:p8`, the same way (`SHIP_ROLE=2`), `%urgit` installed; the two galaxies found each other (`; ~put is your neighbor`).
- Rootless Docker: `dockerd-rootless.sh` with state dir `/run/user/1000/ci-p2-opus/`, data root `.scratch/tmp/docker-data`, socket `/run/user/1000/ci-p2-opus/docker.sock`; `info --format '{{.SecurityOptions}}'` → `[name=seccomp,profile=builtin name=rootless name=cgroupns]`, server 29.7.2. The host's rootful daemon was never touched (it supplied `catthehacker/ubuntu:act-latest` by `docker save | load`).
- **Store fixture:** RustFS 1.0.0 (`rustfs/rustfs:1.0.0`) as the rootless container `urgit-ci-store-dep` on `127.0.0.1:8363`, data `.scratch/tmp/store-data`, one private bucket `ci-bucket` (an unsigned list answers 403), region `local-1`, an access key minted per fixture into `$TMP/store.env` (mode 600); `store.sh configure` ran the P0 `%storage-action` recipe against it. Every read of the bucket in the rows is `curl --aws-sigv4` with that key or a bare `curl` of a presigned link, never the container's filesystem.
- Tooling: `go1.27.1`, `zig` 0.15.2, `act` 0.2.89 static (`act-static.sh`), ERPit source `/var/home/michael/workspace/urbit/erpit` at `a6d15ed` (read-only, cloned into the fixture repositories).

## Commits (one per stage, plus the ruled amendments)

| Stage | Commit | Gate |
|---|---|---|
| S0 | `e2f1225` spec: §Execution gains §Delivery (D7) | spec only, first in the sequence |
| S1 | `5fa71f7` footer env, RustFS fixture, sur, presign-get, log upload, the read routes (D1/D2) | `ci-storage-vector` %.y; the ship-signed PUT lands in RustFS, the presigned GET reads it, unsigned 403, expired 403 |
| S2 | `c9839b6` web-merge gate, trust at staging, restricted policy, approval (D3/D3a) | Q5a–Q8 live; `git-access-vector` %.y |
| S3 | `032c456` credentials, grants, the scrub on both sides (D4) | `ci-event-vector` %.y, `go test` |
| S4 | `bcafb5f` the CI signing key, certified; assignments and grants signed; the daemon verifies (D5) | `sig` tests incl. the dojo jam vectors and the live certificate |
| S5 | `60b2fd9` the ci/* routes, `POST ci/action`, the CI tab and the settings controls (D6) | `npm test` 132/132 |
| S6 | `64a3eee` README, the boxes, the battery scripts; this record's commit | this run |
| S2 amendment (re-freeze 2) | `015fa83` `ci-can-write` decides trust and approval; the second galaxy's pull request drives Q5–Q8 | Q5a, Q5–Q9 live on two ships |
| S2 amendment (re-freeze 3) | `d9e54fc` touch 5 as ruled: the writer helpers lifted out of on-poke's local core | peek %.y/%.n/%.n; Q5, Q8 |
| CI-SANDBOX-1.1 (re-freeze 4) | `3b50b00` `--container-daemon-socket`; Q19 RED on the unfixed daemon, GREEN on the fixed one | Q19 |
| harness | `344cd09` `b5c40a3` `8beb59a` `affb015` `86e3700` | the fixes the RED/GREEN phases and the cold runs found (Deviations) |

## The five `%urgit` touches (fence, re-freeze 3)

1. `handle-api`, the `pulls/<n>/merge` route: the CI gate, `urgit.hoon:7395–7435` — a CI-protected target is never written; the pull's head is staged against the current tip with `actor=source-ship.pull`, `via=%session` and the pull number; 202 with the candidate id and the class the ship will find.
2. `land-candidate`, `urgit.hoon:8051–8095` — takes `pull=(unit @ud)` and flips that pull to `%merged` inside the landing event (`:8079–8090`).
3. `handle-api`, the `GET /repository/<name>` route: `ciUntrustedPolicy`, `urgit.hoon:6119–6137` — `%urgit-ci`'s `/policy/<repo>` under the CI gate's guards, `null` when unreadable.
4. `handle-receive-pack`, the push caller, `urgit.hoon:8734–8744` — the stage poke passes `pull=~` (and no class: the ship decides).
5. `on-peek` `[%x %ci-can-write @ @ ~]`, `urgit.hoon:9302–9316`, over `repository-writable:hc`; the lift: `=<` at `:2231`, the helper door `|_ =bowl:gall` at `:10343` with `group-peek :10357`, `group-seat :10387`, `repository-group-capability :10416`, `repository-writable :10423` (bodies unchanged), and the same-name wrappers in on-poke's `|^` at `:2322`, `:2327`, `:2350`, `:2367`.

## P0 battery tail (`.scratch/ci-p0/battery.sh`, inside `cold.sh`)

Run 22:34:42–22:39:32 on `~dep`, straight after both boots, the rootless daemon and the store. Every step ran; H2 pointed `%storage` at the RustFS fixture; the seven negative rows went RED under the P0 mutants and GREEN on the real build; the four P0 vectors print `%.y` and `cd fe && npm test` passes (132/132 — the source-reading `groupAccess.test.js` now slices the lifted bodies; the first cold run stopped here at 131/132 before that fix).

```
== red: PASS=0 () FAIL=7 (H9 H10 H11 H12 H13 H14 H15) of 7 rows; build: MUTATED
== green: PASS=7 (H9 H10 H11 H12 H13 H14 H15) FAIL=0 () of 7 rows; build: clean (real build)
battery.sh: all steps ran (2026-09-16T22:39:32-05:00)
```

## Q17 — the P1 battery (`.scratch/ci-p1/battery.sh`, 22:39:32 → 23:59:53)

P1–P20 all PASS on this tree; P15 (ERPit, eight jobs) in **874 s** push to verdict; the thirteen P1 mutants RED with their tripwires and GREEN on the real build; the foreground (every `+urgit!*-vector`, `npm test`, `go test`, `-race`, the live Docker boundary test, vet/gofmt, the static binary) 0 failing.

```
P1: PASS P2: PASS P3: PASS P4: PASS P5: PASS P6: PASS P7: PASS P8: PASS P9: PASS P10: PASS
P11: PASS P12: PASS P13: PASS P14: PASS P15: PASS P16: PASS P17: PASS P18: PASS P19: PASS P20: PASS
== red: FAIL-with-tripwire=13 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P19 P20 P14) FAIL-wrong-reason=0 () PASS=0 () of 13 rows; build: MUTATED
== green: PASS=13 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P19 P20 P14) FAIL=0 () of 13 rows; build: clean (real build)
== vectors failing: 0; go steps failing: 0
battery.sh: all steps ran (2026-09-16T23:59:53-05:00)
```

## P2 table (`.scratch/ci-p2/battery.sh`)

Real `act` 0.2.89 in the docker-rootless sandbox, the real daemon binary, the real store, the second galaxy's real pull requests, no dojo poke between push and verdict except the rows' own operator acts (`%set-untrusted-policy`, `%approve-candidate` through `POST ci/action` or the dojo, the `%assign` of Q12/Q13, `%set-credential`). Rows driven by the battery on repository `ci-p2b` (the P2 battery's first attempt on this ship created `ci-p2`, stopped at Q10 on a harness bug — Deviations — and the resumed battery needed a fresh name); logs in `.scratch/tmp/p2-*.log`. "Observed" quotes the row's PASS lines verbatim (trimmed).

| Row | Verdict | Observed |
|---|---|---|
| Q1 | PASS | `git log`: `e2f1225 ci-p2: S0 — spec …` is the first commit above the footer, before any code (`5fa71f7` S1 follows) |
| Q2 | PASS | the `pass` job's attempt `%passed`; `attempt.log = [~ [key='ci/ci-p2b/0v2.9l3v2…/0v3.lk830…/trusted/log.jsonl' size=4.389 sha256='3fe49bf1…']]`; `GET ci/attempt/<id>/log` → **302** to `http://127.0.0.1:8363/ci-bucket/ci/ci-p2b/…/trusted/log.jsonl?X-Amz-Algorithm=AWS4-HMAC-SHA256&…&X-Amz-SignedHeaders=host&X-Amz-Signature=…`; a bare `curl -L` (no headers, no cookie) → **200**, body sha256 `3fe49bf1…` = the ship's handle = the daemon's saved stream, 4389 bytes, a `jobResult` line inside; unsigned GET of the key → **403**; a 2 s presign reads 200 now and **403 `Request has expired`** after 4 s |
| Q3 | PASS | attempt `%trusted`; key carries `/trusted/`; `sign-get` and `presign-get` for `%untrusted` on that attempt → `~`; `%trusted` signs `/trusted/log.jsonl` |
| Q4 | PASS | on a RUNNING attempt (fixture-wait): `POST upload name=../x` → **400** `name must be log.jsonl, summary.md or artifact/<file>`; `artifact/../x`, `artifact/a/b` → 400; `artifact/ok.txt` → 200 signed under `/<attempt>/trusted/artifact/ok.txt`; a good name on the closed Q2 attempt → 409; no credentials → 401 |
| Q5a | PASS | a writer's PR (branch landed directly, then `POST pulls`) to the CI-protected master, Merge → **202** `{"actor":"~dep","candidate":"0v1.0r0n3…","trust":"trusted","staged":true}`; master unmoved (`e31bf589…`), the pull `open`; candidate `%trusted`, `pull=1`, actor `~dep`; `%passed`; **master = the candidate object** `bfbacbec…`, the pull reads `merged` only then, `'landed'`. A PR to an unprotected branch: Merge → 200, the branch moved to the merge commit, `merged` at once |
| Q5 | PASS | `~put` forks `ci-p2b` through the peer protocol, pushes `q5-contrib-…`, opens pull request **#3** (`sourceShip ~put`); the writer merges it through the web → **202** `trust: untrusted, actor: ~put`; candidate `%untrusted %pending`, actor `~put`, `pull=3`, materialized, `plan=~`, **0 attempts**, the daemon's log names it 0 times; master unmoved; the pull `open` |
| Q6 | PASS | `%set-untrusted-policy %restricted` → the same candidate planned and run: 2 attempts, **2 `%untrusted`**; the job's log key under `/untrusted/log.jsonl`; the daemon's assignment line `trust untrusted`; `%passed` with `verdict-reason` `'passed as a restricted check: an untrusted candidate cannot land; approve it to run trusted'`; master unmoved (`bfbacbec…`); the pull still `open`; the eligibility scry `%.n` for its object |
| Q7 | PASS | `POST ci/action {approve-candidate}` (the session) → 200; the old candidate `%skipped` `'superseded by approval'`; the trusted twin (same head `58264fbc…`, `pull=3`) `%passed`; **master = the twin's object**; `'landed'`; the pull reads `merged`; the old one still `%skipped` after the twin's attempts closed |
| Q8 | PASS | pull request **#4** from `~put`, staged `%untrusted`; `ci-can-write` `%.n` for `~put`, `%.y` for `~dep`; `%approve-candidate … ~put` → **refused `actor cannot write ci-p2b`**, the candidate still `%untrusted %pending`, no twin; `|rein %urgit [%\| %urgit]` → the owner's approval refused **`ci: %urgit cannot be read; the actor cannot be checked`**; `%urgit` revived (`%gu` `%.y`) → the owner's approval accepted; old `%skipped`, the twin `%trusted`, `%passed`, landed `8c6c9e6a…`, the pull `merged` |
| Q9 | PASS | `%set-credential TOKEN` (`%job`) listed by `credential-names`; a 3-character value refused `at least 8 characters`; a value with a newline refused **`must be a single line`**; fixture-secret's job `%passed`; the daemon's assignment line **`grants 1 (TOKEN) of 1 offered`**, `--secret TOKEN=***` in its log, the value 0 times; the bucket log: `token is ***` and `token length 27`, the value 0 times; the saved stream = the bucket object (sha256 `fa023b78…`); the step's set-output of the secret recorded as **`'***'`** on the ship; the saved stream's set-output line carries no raw copy |
| Q10 | PASS | the stage poke by `~sampel-palnet` (the peek refuses it) under `%restricted`: `%untrusted`, the job `%passed`, attempt `%untrusted`, **`grants 0 () of 0 offered`**, the step saw `token length 0`, the value absent, the credential still stored |
| Q11 | PASS | the attempt's recorded output `'***'`; seven scry dumps (attempt, candidate, attempts, candidates, assignments, daemons, credential-names) `grep -c value` **0**; `GET ci/attempt/<id>` 0; the log route's object 0; no key named by the value; `%delete-credential` → not listed; a `%rerun-candidate`'s assignment `grants 0 () of 0 offered`, its step `token length 0` |
| Q12 | PASS | `GET ci/key` → 200 `{pub, cert, certified: true, ship: ~dep, life: 1, shipSigningKey}`, pub 32 bytes, cert 64, no `sek`/`sec`/`ring`; without a session 401; daemon a's state file pins the same pub; **Go's `crypto/ed25519` verifies the certificate** against the ship's signing key (`TestLiveCertificate`); a pushed job `%passed` with `assignment signature verified (nonce …, expires …)` in daemon a's log; daemon b started with `ci_public_key` = the pub's first digit flipped, the operator `%assign`s the job to it: **`assignment refused: signature does not verify`**, no sandbox prepared, the attempt `%infrastructure-error` with that reason |
| Q13 | PASS | the normal attempt got the grant; daemon a stopped, its dead poll forgotten, `%assign` with a 25 s deadline, a started 15 s later behind a docker wrapper delaying the first `cp` by 12 s, act exited at +30 s; **`grant TOKEN refused: expired at 05:44:05Z (now 05:44:09Z); not passed to act`**, `grants 0 () of 1 offered`; the stream `token length 0`, no non-zero length |
| Q14 | PASS | `GET ci/repository/erpit-q18-005631/candidates` → 200, newest first, every candidate with ref/head/actor/status/trust/created/attempts; the ERPit candidate lists **8 job attempts**; `GET ci/candidate/<id>`: 8 rows, all `passed`, **8 log handles**, `verdictReason: landed`; the log route's jsonl (217 lines) rendered through `fe/src/ci.js` `renderLog`: `[duo] Set up job: ⭐ Run Set up job`, act's banner kept as text, result lines marked |
| Q15 | PASS | `set-ci-protected` on → policy lists the ref, the scry agrees; off → gone; a ref with no tip → **409 `ref has no tip; push a commit before CI-protecting it`**; the untrusted radio round-trips `restricted`/`approval`; the credential form adds `Q15_TOKEN env staging`, the credentials/policy/repository reads, the built `desk/web` and the served app carry the value **0** times; a short value → 409; delete → not listed; a refused action → 409 `no such candidate`; an unknown action → 400 |
| Q16 | PASS | every `ci/*` read (candidates, candidate, policy, credentials, key, attempt log) without a session → **401**; with a session → 200 (the log route 302/404); `POST ci/action` without a session → 401, with → 200; a daemon bearer is not a session (401) |
| Q17 | PASS | the P1 battery above: 20/20, mutants 13/13 RED and 13/13 GREEN, foreground 0 failing |
| Q18 | PASS | ERPit master `a6d15ed` (1067 commits) seeded into `erpit-q18-005631`, CI-protected, one commit pushed: plan after 52 s = the eight jobs; **8/8 `%passed`**, no reruns, the gated four after the ship read their plan outputs; candidate `%passed` after **860 s**; **8 log handles, 8 keys `ci/<repo>/<cid>/<aid>/trusted/log.jsonl`, 8 bucket objects whose sha256 match the ship's handles**, none under `/untrusted/`, an unsigned read 403; landed: master = `6e3e03c4…` |
| Q19 | PASS | fixture-docker's job live: the job container's `Binds=["/run/user/1000/ci-p2-opus/docker.sock:/var/run/docker.sock"]`, the docker.sock mount's source = the configured rootless socket, the host's rootful socket not bound; `docker info` inside the job → **`name=rootless`**, no permission denied (`.scratch/tmp/q19-green.log`, and GREEN in group A below). RED first on the unfixed daemon (`.scratch/tmp/keep-q19-red-unfixed.log`): `Binds=["/var/run/docker.sock:/var/run/docker.sock"]`, `security=[]`, `permission denied` |

### Q18 timing (ERPit, daemon `a` at capacity 3)

Push at 00:44:41 (local); plan attempt 52 s (clone of 1067 commits + `act -l` ×2); the eight jobs as P15's shape, `suite` last; **push → verdict 860 s = 14 min 20 s** (P15 in the same run: 874 s; P1's close-out: 1117 s). Eight uploads before eight results; the ship recorded eight handles.

## Mutant pairs (P2 negative rows: Q4, Q5a, Q5, Q6, Q8, Q10, Q11, Q12, Q13, Q16, Q19)

`.scratch/ci-p2/q-mutants.sh apply <rows>` puts the named rows' one-line sabotages into one build (five files: `desk/lib/ci-storage.hoon`, `desk/app/urgit-ci.hoon`, `desk/app/urgit.hoon`, `runner/internal/daemon/daemon.go`, `runner/internal/relay/relay.go`); `q-negatives.sh red <rows>` rebuilds the desk and the daemon, seeds a fresh CI-protected repository (`ci-p2-red-<hhmmss>`), keeps daemon a's identity, runs a prep (Q2's push; Q9's before Q11) and every row, counting a row RED only when it FAILS **and** its log carries `q-mutants.sh tripwire <row>`; it reverts the working tree on EXIT; `green` reverts, rebuilds and repeats. Three groups (Deviations): A = the eight rows plus Q19, B = Q5, C = Q8.

```
== red: FAIL-with-tripwire=9 (Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19) FAIL-wrong-reason=0 () PASS=0 () of 9 rows; build: MUTATED
== green: PASS=9 (Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19) FAIL=0 () of 9 rows; build: clean (real build)
== red: FAIL-with-tripwire=1 (Q5) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
== green: PASS=1 (Q5) FAIL=0 () of 1 rows; build: clean (real build)
== red: FAIL-with-tripwire=1 (Q8) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
== green: PASS=1 (Q8) FAIL=0 () of 1 rows; build: clean (real build)
```

What turned each row red — the mutant and the tripwire (the sabotaged build's own answer, as the row logs it):

- **Q4** — `upload-name-allowed` passes every name. `upload name=../x -> 400: FAIL (observed: 200` (a signed URL for `…/trusted/../x`).
- **Q5a** — the merge route's gate skipped (`?: %.n`). `merge of a PR to the CI-protected branch -> 202: FAIL (observed: 200` — the ref written at the click.
- **Q5** — staging classes every actor `%trusted` whatever `ci-can-write` answered. `candidate is %untrusted: FAIL (observed: %trusted` (the candidate found under the trusted id).
- **Q6** — `++landable` ignores trust. `master unmoved (restricted check cannot land): FAIL (observed:` — the restricted check landed.
- **Q8** — approval accepts an actor the peek refused. `approval by the second galaxy refused: FAIL (observed: accepted (>=)`.
- **Q10** — grants released to an `%untrusted` job. `grants=~ on the untrusted assignment: FAIL (observed: grants 1 (TOKEN)`.
- **Q11** — the daemon's line scrub skipped. `the log route's object: grep -c value = 0: FAIL (observed: 1` — act's set-output line's raw `arg` reached the bucket (the ship's own scrub still kept its state at `***`).
- **Q12** — the assignment signature not verified. `daemon b prepared no sandbox (no work): FAIL (observed: 1` — the wrong-key daemon ran the job.
- **Q13** — the expiry check skipped. `expired grant refused by the daemon: FAIL (observed: 0` — the stale grant passed to act.
- **Q16** — `++viewer` answers yes to anyone. `/candidates without a session -> 401: FAIL (observed: 200`.
- **Q19** — `--container-daemon-socket` dropped. `the job container's socket bind is the rootless socket: FAIL (observed: /var/run/docker.sock:/var/run/docker.sock`.

## Foreground

`.scratch/ci-p2/foreground.sh` = P1's (`.scratch/tmp/p2-foreground.log`, 02:53:40–02:54:08): every `+urgit!*-vector` (nine assert `%.y`, fourteen dump values), `cd fe && npm test` **132/132**, `go test ./...` and `-race` (incl. `internal/sig` and the grant/signature daemon tests), the live Docker boundary test on the harness socket (`TestDockerLiveBoundary` PASS), `go vet` + `gofmt` clean, the static binary (`statically linked`): **vectors failing: 0; go steps failing: 0**.

## Deviations

Every derivation was built as written unless listed here. Each departure cites what made it. The three boxes the chair raised (§1 the second ship, §2 multi-line secrets, §3 the writer read) were ratified in re-freeze 2 and built in the S2 amendment commits; re-freeze 3 ruled the shape of touch 5 and re-freeze 4 added CI-SANDBOX-1.1; `QUESTIONS-CI-P2.md` holds no open box.

- **Record convention — a provider safeguard is not a verdict (close-out T6, adopted from astra's record).** A row that a provider safeguard interrupted is recorded as `provider-blocked, not run`, with the timestamp of the interruption, never as PASS or FAIL; it is re-run at most once, and a second interruption leaves it `provider-blocked, not run` and ends the run. The rule is astra's, in its record's words: "Rows left unexecuted in that preliminary phase are recorded as **provider-blocked, not run**" and "One provider retry is authorized; a repeated block ends the run with the remaining row recorded as `provider-blocked, not run`" (`.scratch/astra/p2-live-table.md`, "S6 preliminary battery — interrupted; superseded by cold run"). Applied to this record: every row's verdict is from its own run; a row this rule ever claims is marked in the table as `provider-blocked, not run (<timestamp>)` in the Verdict column, and no row of this record is.

- **Touch 5 — the lift, as ruled.** `on-peek` cannot see on-poke's local `|^` core (the P1 build note), so `group-peek`, `group-seat`, `repository-group-capability` and `repository-writable` moved, bodies unchanged, into a helper door the agent door reaches as `:hc` with its bowl set (`=<` before the door, `|_ =bowl:gall` after it — the agent door's `^- agent:gall` cast is exact and takes no extra arm). One-line wrappers of the same names stay in the `|^` so every existing caller keeps its spelling. The peek `[%x %ci-can-write @ @ ~]` asks `repository-writable:hc`; `%.n` for a repository this ship does not hold or an actor that is not a `@p`. The re-freeze 2 amendment had solved the read another way — a bowl-taking duplicate of the predicate (`ci-can-write`) in the library core, reading the `%groups` seat itself; the re-freeze 3 commit removed it and aligned to the lift. The fe source test that reads the `%groups` readers' bodies now takes their last occurrence (the lifted bodies), not the first (the wrappers).
- **Touch 3 — `ciUntrustedPolicy`.** `repository-json-up-to` sits in the library core, which has no bowl; the field is added at the authenticated `GET /repository/<name>` route in `handle-api`, from `%urgit-ci`'s `/policy/<repo>` peek under the CI gate's guards, `null` when unreadable. The public route does not carry it; the settings page reads it and falls back to `GET ci/repository/<name>/policy`.
- **Touch 4 — the receive-pack caller passes `pull=~`** and nothing else: the trust class is `%urgit-ci`'s finding through the peek for every caller, so `%stage-candidate` carries no class (re-freeze 2 dropped the `trust` field the S2 commit had added). The merge route still names the id the ship will give (it applies the same rule to answer 202), which is why Q5's and Q8's rows look the candidate up by both ids under the mutant.
- **D3 — trust and approval read the peek and refuse on ~.** `++can-write:urgit-ci` reads `/ci-can-write/<repo>/<actor>` through the guarded adapter; an unreadable answer refuses the staging (`'ci: %urgit cannot be read; the actor cannot be classified'`) or the approval (`'… cannot be checked'`, Q8's suspended-`%urgit` probe) rather than assuming a class. In P2 the only actor that can reach the approve poke is the owner (the session); a listed writer on another ship has no session and no cross-ship approve route — the P3 item is a `%git-peer` approve carrying `src.bowl`, which would need no change to the rule.
- **D3 — candidate ids and the shared scratch ref.** The trusted id stays `(sham [repo ref head base])` — the id `%urgit` prints in the push answer and names the scratch ref by — and the untrusted twin adds the class; a rerun adds `now`. Every candidate of one head and base shares `refs/ci/candidate/<sham repo ref head base>` (materialization re-sets what a close released) and the assignment carries `scratch-ref`. `%candidate-ready`/`%candidate-conflict` update every unmaterialized twin. `candidate-status` gains `%skipped`; `settle` leaves a `%skipped` candidate alone whatever its attempts do.
- **D3 — one arm decides landing.** `++landable` (passed, trusted, materialized) is read by both the eligibility scry and `settle`'s landing request, so a restricted check cannot advance a ref from either side and one mutant (Q6) opens both; a passed untrusted candidate's `verdict-reason` names trust and `%urgit`'s `land-candidate` is never asked.
- **The second galaxy (§1, option (a)).** `~put` boots from the footer with `SHIP_ROLE=2` switching every P0 driver (pane id, `+code`, cookie jar); `.scratch/ci-p1/peer.sh` forks, pushes and opens the pull request on it, matching every request by its own id, so Q5–Q8 run on a real non-writer author. A fork checks that the origin's default branch exists: a repository created through the API defaults to `refs/heads/main` while the harness pushes `master`, so the first fork failed `'received repository graph is incomplete'` (found with a throwaway debug print on `~put`'s desk only) and `p2-setup` sets the default branch to master. A contribution writes a file of its branch's own, and Q8 forks afresh, because a second branch from a stale fork conflicted with what Q7 landed.
- **Q8 is three probes.** The owner cannot be removed from its own write set, so the row refuses the second galaxy (`'actor cannot write <repo>'`), refuses the owner while `%urgit` is suspended by `|rein` (the read is unavailable), and admits the owner once `%urgit` is back; the twin runs, lands and flips the pull.
- **D1 — no HEAD at first read.** The ship has no outbound HTTP; a result that names a log records the handle under the attempt's own key, and an object the store does not hold surfaces as the store's answer to the 302. A result that names no log leaves `log=~` and keeps its verdict.
- **D2 — Q2 runs the fixture `pass` job, not ERPit's `plan`.** A plan attempt has no act stream to upload; ERPit's eight job logs are Q18's. Q4 probes the name fence on a running attempt (fixture-wait) so a build without the fence answers 200 with a signed URL — the tripwire; the fence runs before the state check, so a closed attempt answers 409 to a good name only.
- **D4 — credential values are single-line and at least eight characters** (§2 as ruled: the ship refuses a newline; per-line scrubbing is P3's; the set-output `arg` scrub is spec). act's set-output line carries the raw value in `arg` while masking `msg` (measured, `.scratch/tmp/keep-actprobe-secrets.jsonl`), so both the daemon's line scrub and the ship's `scrub:ci-event` exist; Q11's mutant is the daemon's scrub, because under one build the ship's scrub cannot be turned red while the daemon still scrubs (that layer is the event vector's). Q10 stores its own credential.
- **D4 — a grant expires at the attempt's deadline** (`assigned + deadline`), measured from the ship's assignment time; the daemon's own deadline runs from receipt, which is what Q13 uses (a late delivery through a dead poll forgotten first). The `dbug` wrapper's state scry would print credential values: a developer surface, not a product scry or route; every product read greps the value 0 times (Q11, Q15).
- **D5 — the key exists from `on-init`, not from the first `%set-credential`.** "The daemon refuses an unsigned assignment" cannot hold if the key does not exist at the first enrollment; Q17's P1 regression carries no credentials and must still verify. `%rotate-ci-key` keeps the operator's rotation.
- **D5 — the ring is not kept; the derived signing pair is.** Jael's `%private-keys` gift yields the current ring; a suite-b ring is `'B'` then 64 bytes with the signing seed in the low 256 bits (`nol:nu:crub`, zuse:1806 upstream), and `(luck:ed:crypto seed)` is the `[sgn.pub sgn.sek]` ames signs with. `ship-keys` holds `[life pub sek]` of that pair only — a strict subset of the ring, never logged or read out. Go's `crypto/ed25519` verifies the certificate live (`TestLiveCertificate` on `GET ci/key`, Q12).
- **D5 — the signed noun and its expiry.** `(jam [recipient attempt operation expiry nonce])` with `expiry` in unix seconds (a `@da` is not a JSON number) and `operation` `'assign'` or `'grant:<name>'`; the daemon rebuilds the noun from the wire fields and reimplements `++jam` (vectors read off the dojo, including the backreference case). An assignment's authorization expires a minute past the attempt's deadline (delivery is not instant); a refused assignment is abandoned with the reason so the ship shows why, rather than dropped silently. `GET ci/key` is session-authorized like every ci/* read.
- **D6 — times on the wire are unix seconds**, and `POST ci/action` applies the poke under `mule` so a refusal answers 409 with the `~|` message's own words (the first quoted cord in the trace; a `rap`-built reason is cast `@t`, or it prints as a number).
- **CI-SANDBOX-1.1 (re-freeze 4).** Verified on this daemon first: a live job container under the unfixed build carried `Binds=["/var/run/docker.sock:/var/run/docker.sock"]` (the host's rootful socket, `security=[]`) and `docker info` inside it failed `permission denied` (`.scratch/tmp/keep-q19-red-unfixed.log` — the job container vanished after the failed `docker info` on the first try, so `fixture-docker.yml` swallows the status and sleeps 25 s to stay inspectable). The fix is `--container-daemon-socket d.cfg.DockerHost` on every act invocation, `DOCKER_HOST` unchanged; the argv test pins it; `runner/README.md` names it. Q19 is in group A with the flag dropped as its mutant.
- **Harness — the negatives run in three mutant groups.** P1 applied every mutant in one build; Q5 and Q8 run on the merge gate that Q5a's mutant removes, and Q8's untrusted candidate is what Q5's mutant classes trusted, so `q-mutants.sh apply` takes row names and the battery sabotages A = `Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19` together, then B = `Q5`, then C = `Q8`. Q6 in the mutant phases stages its own untrusted candidate (a seam poke with `~put` as the actor) rather than reading Q5's env file — under the real build Q7 had already superseded that candidate, which turned the first GREEN of group A red on Q6 alone (`.scratch/tmp/p2-q-negatives-green-Q4-Q5a-Q6-Q10-Q11-Q12-Q13-Q16.log`, 7/8).
- **Harness — the P1 ghost, twice.** A re-enrolled daemon a left its old record fresh for five minutes and the scheduler handed a plan to it (Q12's first run); Q12 targets daemon b by `%assign` instead of stopping a, and Q13 waits twenty seconds after stopping a so eyre forgets the dead poll before the `%assign` (an assignment answered into it would be re-offered two minutes on, past the row's deadline).
- **Harness — JSON numbers, the refined-list trap, the store fixture.** `number-at` parses with `dim:ag` (`slaw %ud` and `dem:ag` want thousands dots: a 4389-byte size was "not a natural number"); a `(list)` refined non-empty by `?~` and handed to a wet gate fails `mull-grow`, so each call site casts back; `store.sh` runs RustFS 1.0.0 as container root (`--user 0:0`: the image's uid 10001 maps to a subuid under rootless Docker and cannot write the bind mount), the bucket is private (an unsigned list answers 403), the key is minted per fixture, and H2 points `%storage` at it so the P1 rows upload real logs (the P0 negatives' H15 expects the fixture's endpoint).
- **The cold runs.** Run 1 was stopped at P15 when re-freeze 2 landed (P0 7/7 RED and GREEN, P1 P1–P14 PASS: `.scratch/tmp/keep-cold-run1-stopped.log`); run 2 stopped at the P0 battery's foreground (`npm test` 131/132: the source test that the lift moved, fixed in `8beb59a`; `keep-cold-run2-stopped.log`); run 3 is this record, from fresh piers on the re-freeze 3 tree. Its P2 battery ran in pieces on the same ships, nothing typed between them: (i) `ci-p2` from `cold.sh`, Q2–Q9 PASS, stopped at Q10 (`keep-cold-run3-p0-p1.log`, 00:24:26) — Q10's stage poke still carried the S2 commit's seven-field shape, fixed in `affb015`; (ii) the restart refused `ci-p2` (`repository already exists`), so the battery takes `P2_REPO` and (iii) ran `ci-p2b` from Q2 through Q13 (`keep-p2-battery-attempt3-to-q13.log`, 00:34–00:44) and stopped at Q18 — the copied `p15.sh` sourced the P1 lib by a relative path, fixed in `86e3700` with `START_AT`; (iv) `START_AT=q18`: Q18, Q14–Q16, group A RED 8/8, GREEN 7/8 on Q6's stale env (`keep-p2-battery-attempt4-q18-to-negA.log`, 00:56–01:53); (v) re-freeze 4 landed meanwhile, so after `3b50b00` (Q19, Q6 self-sufficient) the battery resumed at group A with Q19: RED 9/9, GREEN 9/9, B, C, foreground (`p2-battery-c.log`, 02:01:16–02:54:08). Every Q2–Q19 row in the table is from `ci-p2b` or the run's own fresh repositories; no row was retried by hand.

## Shutdown

`.scratch/ci-p2/shutdown.sh` at 2026-09-17T03:06 (local) (the daemons had already exited with their rows; `store.sh stop` removes the fixture container and keeps its data; both ships by ctrl+d to their dojos, pids verified gone through `/proc/<pid>/cmdline`, the panes closed, the piers retained; the rootless Docker daemon is left running as P1's close-out left it):

```
runner a not running
runner b not running
container urgit-ci-store-dep: running rustfs/rustfs:1.0.0 port 8363
removed urgit-ci-store-dep (data kept under /var/home/michael/workspace/urbit/urgit-ci-p2-opus/.scratch/tmp/store-data)
== processes whose /proc/<pid>/cmdline is /var/home/michael/workspace/urbit/bin/urbit … /var/home/michael/piers/urgit-ci-p2-put:
3608549  /var/home/michael/workspace/urbit/bin/urbit -F put -B /var/home/michael/workspace/urbit/pills/brass-408k-1.pill --http-port 8355 -c /var/hom
3608802  /var/home/michael/workspace/urbit/bin/urbit work --snap-dir /var/home/michael/piers/urgit-ci-p2-put --runtime-config 128 --temporary-cache-s
== ctrl+d to the dojo in pane w1C:p8
== all pier pids gone; waiting for the shell prompt, then closing the pane

michael at fwdesktop in urgit-ci-p2-opus on  ci/p2-opus [!] via  v0.15.2   New deployment staged took 4h33m46s
➜
{"id":"cli:pane:close","result":{"type":"ok"}}

pier retained: /var/home/michael/piers/urgit-ci-p2-put (234M)
== processes whose /proc/<pid>/cmdline is /var/home/michael/workspace/urbit/bin/urbit … /var/home/michael/piers/urgit-ci-p2-dep:
3607459  /var/home/michael/workspace/urbit/bin/urbit -F dep -B /var/home/michael/workspace/urbit/pills/brass-408k-1.pill --http-port 8353 -c /var/hom
3607662  /var/home/michael/workspace/urbit/bin/urbit work --snap-dir /var/home/michael/piers/urgit-ci-p2-dep --runtime-config 128 --temporary-cache-s
== ctrl+d to the dojo in pane w1C:p7
== all pier pids gone; waiting for the shell prompt, then closing the pane

michael at fwdesktop in urgit-ci-p2-opus on  ci/p2-opus [!] via  v0.15.2   New deployment staged took 4h34m43s
➜
{"id":"cli:pane:close","result":{"type":"ok"}}

pier retained: /var/home/michael/piers/urgit-ci-p2-dep (821M)
```
