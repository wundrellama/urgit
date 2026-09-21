# CI P3 live table — chair `opus`, ships `~sud` and `~tug`; close-out on `~mex` and `~ryt`

Every row ran on `~sud`/`~tug` by script, nothing typed. The observed columns are the cold run's (R13, 2026-09-19 19:34 → 2026-09-20 01:32; the opus worktree's `.scratch/tmp/cold.log`, 3644 lines), which re-ran every battery P0–P3 and every mutant both ways on the final tree from fresh piers; where a value was first measured on the build pair the cold run's agrees (its numbers are the ones printed). Evidence per row: the `##### <row>` section of `cold.log` and the step file named in the row. That run stopped and resumed four times and was preceded by three launches that stopped earlier; each stop is a `## Stop <k>` section below with what was observed, what was re-anchored, and the pids at that moment; the run's `/proc` shutdown is `## Final shutdown`; the operator's own fresh-pair battery of the ratified tree is `## Fresh-pair battery`.

**Close-out (branch `ci/p3-closeout`, cut from the ratified pick `01e668a`; `BRIEF-CI-P3-CLOSEOUT.md` `507f66d`, its footer `84bdf18`, rider T8 `eefea54`).** Rows R6a (T1) and R15b (T2) were built and proven RED → GREEN on the close-out's build pair `~mex`/`~ryt` (the footer's ships, ports 8420/8421, piers `/var/home/michael/piers/urgit-ci-p3c-{mex,ryt}`, tmux `ci-p3-closeout-{mex,ryt}`, rootless Docker `/run/user/1000/ci-p3-closeout`, RustFS `urgit-ci-store-mex` on 8422), booted 2026-09-20 12:17/12:18 by `boot.sh` (`--loom 34`, no `-p`, neither pier existed before). The close-out's own cold run (T6) is recorded in the same shape: its stops as further `## Stop` sections (there were none), its shutdown in `## Final shutdown`, its verdict the last line of this record. Rider T8 (`eefea54`) added urgit's own tests as urgit-ci jobs and R18, the dogfood row, run on the retained pair after the cold run; R18's third try found a product defect (a deleted repository's CI records outlived it), fixed in T8b and proven RED → GREEN as negatives group F — the Deviations bullet **T8b** carries the three-try history.

- Worktree `/var/home/michael/workspace/urbit/urgit-ci-p3-opus`, branch `ci/p3-opus`, base `master` at `c8007be`; the brief `9216ff2`, rider 1 `f45de4c`, rider 2 `98f7caf`, rider 3 `ada93e3`, rider 4 `627644d`, rider 5 `bceeb01` (the brief's sha256 `bb512c803c37e750…`).
- **First ship** `~sud`, HTTP 8390, pier `/var/home/michael/piers/urgit-ci-p3-sud`, booted by `.scratch/ci-p0/boot.sh` as the tmux session `ci-p3-opus-sud` (200×50, no `-p`), the desk built, committed and installed by the script, `+code` recorded. **Second ship** `~tug`, HTTP 8391, pier `/var/home/michael/piers/urgit-ci-p3-tug`, session `ci-p3-opus-tug`, the same way (`SHIP_ROLE=2`). Neither pier existed before the first boot; a first `~tug` boot was stopped and its pier removed within a minute, before its desk was committed, because the tree it would have built was mid-edit (Deviations).
- Rootless Docker: `dockerd-rootless.sh` with state dir `/run/user/1000/ci-p3-opus/`, data root `.scratch/tmp/docker-data` (fresh), `name=rootless`, server 29.7.2. **Store fixture:** RustFS 1.0.0 (`rustfs/rustfs:1.0.0`) as the rootless container `urgit-ci-store-sud` bound `0.0.0.0:8392`, data `.scratch/tmp/store-data`, one private bucket `ci-bucket` (an unsigned list answers 403) with a bucket CORS rule for GET/HEAD (`store.sh cors`; measured: without it the store answers no CORS header and a browser's log view fails); `%storage`'s endpoint `http://192.168.1.229:8392` for the P3 rows (`STORE_ADVERTISE`, the host's LAN address), `127.0.0.1` for P0–P2's rows.
- Daemon a enrolled through the mint route at `DAEMON_CAPACITY=3`; test daemons b, c, r1, r2, r3 per row, each retired (revoked and removed) by its row.
- Tooling: `go1.27.1`, `zig` 0.15.2, `act` 0.2.89 static, ERPit `a6d15ed` (the P2 pin; read-only, cloned into the fixture repositories), tmux 3.7c; `np` (`192.168.1.64`, hostname `nativeplanet`) over ssh, curl only.

## Commits (one per stage; the riders and the ruled amendments)

| Stage | Commit | Gate |
|---|---|---|
| brief, riders | `9216ff2` `f45de4c` `98f7caf` `ada93e3` `627644d` `bceeb01` | the operator's |
| S0 | `f920976` spec: §Recovery › §Runner loss (CI-DELIVERY-1.1 as text) | spec only |
| S1 | `585db33` mint/expire/revoke/rotate, the runners route, the scheduler seam, the re-offer path (D1, D2, D6) | R1, R3, R4, R5 PASS; negatives A RED 3/3, GREEN 3/3; B RED 2/2, GREEN 2/2 |
| S1b | `f2ff768` labels and repository binding (D2b) | R11a, R11b PASS |
| S2 | `4a48990` the Runners panel (D3) | `npm test`; the rows' JSON = the buttons' |
| S3 | `3129e52` the live channel (D4) | R2, R6 PASS |
| S4 | `0d18096` storage reachability, first-run states (D5, D7) | R7 (from np), R8 PASS |
| S5 | `e4ffb6c` the ghost, the silent runner, N+1, ownership-aware reconciliation (D6 a–g); `3b52e6b`, `cbeab46` its mutants (compiling; R11b's group of its own) | R9, R10, R11, R11b PASS; negatives C RED 2/2, D RED 1/1, GREEN C+D 3/3 |
| S6 | `b6aed2d` README (D8) | R12 PASS 47/47 |
| S8 | `578515a` the linked-desk landing, the per-line scrub, the cross-ship approve (D9); `b6199e9` the linked landing's in-progress guard (found by P19's cold run, Deviations) | R14, R15, R16 PASS; P19 and Q9 re-asserted; negatives E (R16 R14) |
| rider 4 | `58503cf` R16's actor-substitution RED and R14's guard RED as group E (%urgit's mutants); `52179e3` QUESTIONS §5 | E RED 2/2, GREEN 2/2 |
| rider 5 | `1b0fd07` the six pre-P3 `pending-clay` guards, one token each; R17 and its mutant in group E | R17 PASS; E (R16 R14 R17) RED 3/3, GREEN 3/3 |
| S7 | `88a2a53` (`--loom 34`, `restart.sh`), `8842ec8` (the P0/P1 mutants re-anchored to the P3 code), `fcae120` (H10 asserts the P3 deadline rule), `f8659e7` (Q12 asserts the P3 refusal rule), `cb20628` (a resumed run's `START_AT` reaches its first battery only), `119b996` (R5 counts the daemon's own refusal line), `5bb55d5` (the P3 battery advertises the store on the LAN address) — the harness for the cold run; the run itself is a log, not a commit | R13 PASS (the cold run: every battery P0–P3, every mutant both ways) |
| S9 | this record (`.scratch/p3-live-table.md`) | — |
| close-out T1 | `65f7333` R6a — the watch-authorization row, astra's `s3-watch-auth.py` on the footer env; the footer's env rewrite | R6a RED 1/1 (tripwire), GREEN 1/1; step PASS on the build pair |
| close-out T2 | `bb693fc` R15b — the ship-side scrub with the daemon bypassed, astra's R15SHIP in `r14-r16.sh`'s shape; no product change (the ship already scrubs independently) | R15b RED 1/1, GREEN 1/1; step PASS on the build pair |
| close-out T3 | `1948e02` this record's shape: `## Stop <k>` per stop, `## Final shutdown`, `## Fresh-pair battery`, `## Deviations` for the rest | — |
| close-out T4 | `7755267` the two ledger-id comments in words; the README's one GroundSeg mention the only one; the sweep of the upstream diff empty | — |
| close-out T5 | `197a046` comment-only: six ship-named comments in `ci-p3` → roles; `dojo_value "head` empty over P0–P3; `839x|sud|tug` clean in `ci-p3` | — |
| close-out T6 | `c81eafc` the cold run on `~mex`/`~ryt` from fresh piers — a log, not a commit; this record's `## Close-out cold run` and `## Final shutdown` | `cold.sh: every step ran (2026-09-20T18:20:02-05:00)`, one launch, no stop |
| close-out T8 | `555d8ff` `.github/workflows/urgit.yml` (go-test, fe-test, hoon-vectors), `.github/actions/{urbit-toolchain,boot-fake-ship}` (ERPit's, adapted), the README's CI section | pre-flighted under act on the host's Docker: three jobs green, `vectors: passed=23 of=23`; the broken fixture fails with the vector's name |
| close-out T8b | `8451684` repository deletion clears `%urgit-ci`'s state (R18 found it): `%urgit`'s `%delete` pokes `[%repository-deleted name]`, `%urgit-ci` drops every record under the name; `r18.sh` with the deletion case first, its mutant as group F, R18 a battery step | R18 RED 1/1 (tripwire, `rneg-red-R18.log`), GREEN 1/1 (60/60 checks, `rneg-green-R18.log`) on the retained pair |
| close-out T7 | this record complete; `QUESTIONS-CI-P3.md`'s `## Close-out`; the pair shut down by `/proc` pid | — |

## Table

| Row | Verdict | Observed |
|---|---|---|
| R1 | PASS | `POST ci/runners/mint` → 200 `{id, token (0v…), shipUrl, configSnippet (ship_url/enroll_token/sandbox), runner{state: minted}}`; `GET ci/runners` lists it `minted`, the token 0× in the list and 0× in the record's scry; `enrolled=~`, `bearer-hash=~`, `minted` set; mint and list without a session 401; `desk/gen/urgit-ci/` empty in the tree and the mounted desk, `:urgit-ci\|mint-enroll-token` → not found, `[%mint-enroll-token ~]` fails to compile; the token enrolls a real daemon → `healthy` · `.scratch/tmp/p3-r1-r5-r1.log` |
| R2 | PASS | `/ci/runners` subscribed through Eyre's channel: the initial fact is the runner list; a mint → a `runner` fact `minted`; the daemon starting with the token → a fact `healthy` **2 s** later carrying `labels ["r2-only"]`, `enrolled`, `lastSeen`; revoke → `revoked`; remove → `runner-gone`; the token 0× in the stream · `.scratch/tmp/p3-r2-r6-r2.log` |
| R3 | PASS | expire a minted record → 200, gone from the route and the ship; the expired token enrolls nothing (`enroll token is not recognized`); expire an enrolled daemon → **409 `daemon is enrolled; revoke it instead`**, still healthy; an unknown id → `no such daemon` · `.scratch/tmp/p3-r1-r5-r3.log` |
| R4 | PASS | b (cap 1) running fixture-slow (a bound away for the push, unbound before the revoke); revoke → 200, panel `revoked`, bearer cleared; the attempt `%reoffered 'daemon revoked; re-offered'`; b, busy, polled anyway (heartbeat) and exited **12 s** after the revoke logging `revoked by the ship`, `exit status 5`; its old bearer → `401 revoked by the ship`; the fresh attempt on a `%passed`, candidate `%passed`, landed; b's record removed · `.scratch/tmp/p3-r1-r5-r4.log` |
| R5 | PASS | rotate → 200, `GET ci/key` a new pub, `certified: true`, Go's ed25519 verifies the new certificate; a's next assignment (the plan) `assignment refused: signature does not verify`, the ship marks a **`refused`** with that reason, a keeps polling and stays refused; the attempt `%infrastructure-error abandoned: …` (no other daemon); no further attempt for a; re-enrolled with a fresh token a pins the new key, the old record stays refused (revoke → remove → 200); a re-run passes with `assignment signature verified` and lands · `.scratch/tmp/p3-r1-r5-r5.log` |
| R6 | PASS | `/ci/repository/ci-p3` subscribed; a push (fixture-chain): **18 facts in 4 s** — pending, planned, a job running, job a passed with its log handle, the verdict passed, `'landed'`; every fact a full row, none for another repository; with the channel gone the read Refresh makes answers 200 with the landed candidate · `.scratch/tmp/p3-r2-r6-r6.log` |
| R6a | PASS | (close-out T1: astra's watch-authorization row, `s3-watch-auth.py`, on the footer env) a session on the second galaxy opens ITS Eyre channel and subscribes to the first ship's `%urgit-ci` at `/ci/runners` and `/ci/repository/ci-p3` with `ship: <first>`: each subscribe comes back **`err`** (the watch-ack's trace names `on-watch`'s guard, `/app/urgit-ci/hoon:<[153 3].[163 5]>`), and the channel never delivers a `diff`; the same path from the first ship's own session answers a fact at once (the refusal is authorization, not a dead path). RED (the `?> =(our.bowl src.bowl)` removed, `r-mutants.sh` R6a): both subscribes come back `ok` — `R6a RED: foreign ship watch accepted` — and the foreign channel is then `quit` with no diff; restored → refused, 2/2. Measured on the close-out's build pair `~mex`/`~ryt` (`.scratch/tmp/p3-r2-r6-r6a.log`, `rneg-{red,green}-R6a.log`); the cold run (T6) re-runs it as a step and in group A |
| R7 | PASS | from np: the probe URL (`http://192.168.1.229:8392/ci-bucket/ci/_probe`) answers **403** with `access-control-allow-origin: *`; a real log through the route's 302 read from np has the ship's sha256; `%storage` at 127.0.0.1: the probe names it, np's fetch **`000 exit=7`**, the fe renders *Your browser cannot reach the object store at 127.0.0.1:8392. Logs and artifacts will not open. The endpoint must be reachable from every viewer's network, not only from the ship's host.*, the log route still 302s to a 127.0.0.1 link; restored · `.scratch/tmp/p3-r7-r8-r7.log` |
| R8 | PASS | a fresh CI-required repository with a revoked (0 enrolled) pool: the policy lists the ref, the runners read has no enrolled record, the tab's message renders (*No runner is enrolled. Mint a token in Settings → Runners and install the daemon…*), the feed's initial fact carries the runner list; `%storage` unset (endpoint cleared): CI required → **409 `ship object storage is not configured; CI cannot be enabled`**, the probe `configured: false`; restored, a re-enrolled · `.scratch/tmp/p3-r7-r8-r8.log` |
| R9 | PASS | b enrolled with a wrong pinned key beside a; ERPit (`a6d15ed`, 1067 commits) pushed: b's refusal **26 s** in, marked `refused`; **8/8 `%passed` on a, 904 s** push to verdict, no attempt closed at a deadline, landed; b refused exactly once, kept polling, stayed refused; revoked, removed, re-enrolled: b took the next job and passed · `.scratch/tmp/p3-r9-r11-r9.log` |
| R10 | PASS | fixture-silent (`timeout-minutes: 1`) on b; SIGSTOP; at **172 s** `%reoffered 'runner went silent; re-offered'`; the fresh attempt passes on a, landed; b resumed at the re-offer: the ship answered its late lines `attempt is closed`, b claimed no result (its jobResult line refused with the rest), the attempt unchanged · `.scratch/tmp/p3-r9-r11-r10.log` |
| R11 | PASS | four jobs at t+0 on a alone (capacity 3): all four `%passed` on a, none closed at a deadline, **48 s**, landed · `.scratch/tmp/p3-r9-r11-r11.log` |
| R11a | PASS | b declares `big-mem`: the big-mem job ran on b, the plain job on a; the gpu job waits with **`no runner has labels [gpu]`** on the candidate row, in the candidates route and in a feed fact; c declaring `gpu` enrolls → the gpu job assigned **3 s** later, passed, candidate passed, `'landed'`; the job rows carry runs-on from the plan · `.scratch/tmp/p3-r9-r11-r11a.log` |
| R11b | PASS | b bound to `ci-p3`; a full (capacity 1, the slow job); a push to another repository: no attempt in 40 s, b idle; b restarted beside a's live sandbox (rider 3): running, no `enrollment lost`, never asked about a's attempt, a's network untouched, polled within the window, the binding intact; unbound → b took the plan **2 s** later, passed, landed · `.scratch/tmp/p3-r9-r11-r11b.log` |
| R12 | PASS | 47/47 checks of the README against the tree and the routes (the S6 commit lists them) · `.scratch/tmp/p3-r12.log` |
| R13 | PASS | the cold run, fresh piers, `cold.sh` (four launches; the three stops and the two resumptions in Deviations): **P0** H1–H15 (14 rows, `h*.log`), negatives RED 7/7 (H9–H15) GREEN 7/7, foreground; **P1** P1–P20 PASS (P19 landing through the desk on the fixed guard), negatives RED 12/12 with tripwires (P1 P7 P8 P9 P10 P11 P12 P13-overlap P14 P17 P18 P20) GREEN 12/12, foreground; **P2** Q2–Q19 PASS (Q12 re-anchored), negatives A RED 9/9 GREEN 9/9, B 1/1 both, C 1/1 both, foreground; **P3** R1–R12, R14–R17 PASS, negatives A 3/3, B 2/2, C 2/2 + D 1/1 (GREEN 3/3), E 3/3 — every RED with its tripwire, every GREEN whole — foreground (`npm test`, `go test`, the plan vectors 31/31); the shutdown by /proc pid. `cold.log`; the per-step files `h*.log`, `p*.log`/`neg-*.log`, `p2-*.log`/`qneg-*.log`, `p3-*.log`/`rneg-*.log` under `.scratch/tmp/`. **Close-out (T6):** the same battery, with R6a and R15b as steps and in group A, re-run whole on `~mex`/`~ryt` from fresh piers in ONE launch, 12:37:07 → 18:20:02, no stop and no resumption — P0 RED 7/7 GREEN 7/7; P1 rows 4/4, 5/5, 5/5, RED 12/12 GREEN 12/12; P2 Q2–Q19 PASS, A 9/9 both ways, B 1/1, C 1/1; P3 R1–R17 (20 rows) PASS, A **5/5** both ways, B 2/2, C 2/2 + D 1/1 with GREEN 3/3, E 3/3; every foreground clean; the shutdown by `/proc` pid — `## Close-out cold run` below; this worktree's `.scratch/tmp/cold.log` (3573 lines) and its `p3-*.log`/`rneg-*.log` |
| R14 | PASS | a fresh desk (`%r14-<ts>`, `|new-desk`) bound through the API → 200, the ci-ref peek `linked`; CI required on the linked repository → **200** (the P1 refusal gone), the ref CI-protected; a push (desk-shaped: `sys.kelvin`, `mar/{txt,yml,mime,hoon,kelvin,noun}`, the workflow) staged as a candidate, master unmoved at the push; the candidate `%passed`, `%urgit-ci` heard `%landed` (`'landed'`), master = the candidate, the desk advanced a revision (1 → 2) and holds the candidate's file, the binding records the landed commit (`git-to-clay`); a second candidate on the landed tip passed and landed the same way; two candidates passing together on the linked ref (the race P19's cold run found): exactly one landed (master = it), the other refused **`linked desk update already in progress; re-run the candidate to land it`**, not dropped · `.scratch/tmp/p3-r14-r16-r14.log` |
| R15 | PASS | `%set-credential` with a two-line value (a PEM-shaped pair) → **200**, listed; the pem job `%passed`, the grant released; the step printed both lines (the secret reached the job whole); line one 0×, line two 0× in the daemon's saved stream, the mask present; neither line in the bucket object; the ship's recorded output `'***'`; the candidate route carries no line of the value · `.scratch/tmp/p3-r14-r16-r15.log` |
| R15b | PASS | (close-out T2: astra's `s8-live.py` R15SHIP on the footer env, in `r14-r16.sh`'s shape) a two-line credential set; a `fixture-wait` job running on daemon a with act's lines relayed; four RAW events posted to `POST ci/attempt/<id>/event` with a's bearer, the daemon bypassed — line one alone, line two alone, line one inside ordinary text, the whole value — each **202**, the ship's event count +4; the attempt's outputs (scry) hold **`'***'`, `'***'`, `'key: *** (end)'`, `'***'`**, neither line anywhere in them nor in the candidate route; the real job passes and the candidate lands. The ship scrubs independently of the daemon today (`handle-event` → `scrub:ci-event` over `credential-values`, whole value + every line ≥ 8 chars), so no product change. RED (`r-mutants.sh` R15b: `credential-values`' per-line forms dropped, `` `~[value.c]``): lines one and two and the in-text form persisted raw (only the whole value masked) — `R15b RED: the ship persisted a raw credential line`; restored → masked, 1/1. Measured on the build pair (`.scratch/tmp/p3-r14-r16-r15b.log`, `rneg-{red,green}-R15b.log`); the cold run re-runs it as a step and in group A |
| R17 | PASS | (rider 5) a fresh desk and a desk-shaped repository bound to it, `~tug` a writer; `~tug` forks it over the peer protocol, commits on the fork, `POST /peer/push`; **3.0 s** in, a plain push into the bound branch parks (its response held ~2.5 s); the peer push's finish lands in that window and is answered **`another Clay operation is in progress`** (`ok: false` on `~tug`'s transfer); the parked push completes: master = its head, the desk holds its file; the peer push's commit is not master and not in the desk; first try · `.scratch/tmp/p3-r17.log` |
| R16 | PASS | `~tug` listed as a writer (`ci-can-write` `%.y`); a contributor's branch pushed, the candidate `%untrusted %pending`; `POST /peer/ci-approve` on `~tug` → **202**, the owner's answer `ok: true`, `candidate approved`, the request of kind `candidate`; on `~sud` the untrusted candidate `%skipped` `superseded by approval`, the trusted twin's actor **`~tug`** (`src.bowl`, never the owner), the twin `%passed`, master = the contributor's head; `~tug` removed from the writers (`%.n`): a second branch, the approve answers `ok: false` `requester cannot write the repository`, the candidate stays `%untrusted %pending` · `.scratch/tmp/p3-r14-r16-r16.log` |

| R18 | PASS | (close-out T8, the dogfood row, run on the retained pair after the cold run, the ships restarted by `restart.sh`; the recorded run is negatives group F's GREEN, 19:33, after its RED) **the deletion case first (T8b's finding):** a fixture repository created, seeded, CI required, a credential set, daemon a bound to it; a push staged, its candidate `%passed` and landed, the eligibility peek `%.y` for the landed oid; `DELETE /repository/<name>` → **200**, gone from `%urgit` (404); the peek **`%.n`**, the candidate record `~`, no attempt or assignment of it, the ref no longer CI-protected, the credential gone, a's binding `[]` (bound to nothing, not the pool); re-created (201), seeded, CI required (200); the **SAME oid pushed is STAGED** as a fresh record (created after the deletion; the id the same sham of repo, ref, head, base), master unmoved at the push; the fresh candidate `%passed` and landed. **Then the dogfood row:** the ship's own `urgit` created (201), seeded with HEAD's parent `555d8ff`, CI required (200); RED — this tree plus one wrong expected reason in `desk/gen/ci-plan-vector.hoon` pushed → staged, the plan **`urgit.yml/fe-test urgit.yml/go-test urgit.yml/hoon-vectors`**, the candidate **`%failed` in 179 s**: `hoon-vectors` `%failed`, the verdict `job hoon-vectors in urgit.yml failed`, its log `ci-plan-vector: FAIL (verdict %.n, passed=30 of=31)` and `failures: ci-plan-vector`, master unmoved; GREEN — this tree's HEAD `8451684` pushed → the same three jobs, **all three `%passed`, the candidate `%passed` in 183 s**, the hoon-vectors log carrying every vector's line (23), **`vectors: passed=23 of=23`**, `ci-plan-vector: %.y (passed=31 of=31)`, no failures line; the go-test log's daemon package `ok`, the fe-test log `fail 0`; the hoon log in the bucket with the ship's sha256, the count in the object too; `'landed'`, **master = HEAD**. 60/60 checks · `.scratch/tmp/rneg-green-R18.log` (the step run of the row before the fix, three tries: `p3-r18-try1.log`, `p3-r18-try2.log`, `r18-run-try1.log`, `r18-run.log`) |
Negatives: A (R4 R5 R3) RED 3/3 with tripwires, GREEN 3/3; B (R4b R5b) RED 2/2, GREEN 2/2; C (R9 R10) RED 2/2 and D (R11b) RED 1/1, GREEN C+D 3/3; E (R16 R14 R17 — rider 4's actor substitution: the receiving arm acts for the owner whoever sent the packet; the linked landing's in-progress guard gone; rider 5's peer-push guard back to the never-true form) RED 3/3 with tripwires (`R16 RED: actor substitution` — the twin's actor read `~sud`, the non-writer's request approved; R14's other candidate `~`, dropped; R17's peer push `ok: true` and the parked push's held response never came — git bounded at 40 s), GREEN 3/3 — on the build pair before the cold run (`before-cold/p3-negatives-e-rider{4,5}.log`), and again in the cold run's P3 battery (`p3-r-negatives-{red,green}-*.log`, `rneg-{red,green}-R*.log`): A RED 3/3 GREEN 3/3, B RED 2/2 GREEN 2/2, C RED 2/2, D RED 1/1, GREEN C+D 3/3, E RED 3/3 GREEN 3/3. Close-out: group A is `R4 R5 R3 R6a R15b` (T1, T2); R6a and R15b were run RED → GREEN as a group of their own on the build pair (`rneg-{red,green}-R6a.log`, `rneg-{red,green}-R15b.log`) before the cold run takes them in group A. Group **F** is `R18` (T8b; its mutant: `%repository-deleted` drops no candidate), run on the retained pair after the cold run: RED 1/1 with the tripwire (`R18 RED: a push after the repository's deletion landed unstaged` — under the mutant the peek stayed `%.y`, the candidate and its two attempts and two assignments stayed, the same oid landed unstaged; the dogfood part's 52 checks still PASS), GREEN 1/1 (60/60). The GREEN was run twice: the first (`rneg-green-R18-try1.log`) passed every product check and failed one of the row's own — a doubled backslash in the regex of the `created after the deletion` check, which could never match; fixed in `r18.sh` (T7) and the phase re-run whole; the RED log is unaffected (under the mutant that check reads empty either way).

## Stop 1 — launch 1, 17:55:03 (the P0 mutant anchor)

Launch 1 of `cold.sh` at 17:51:27 (`.scratch/tmp/before-cold/cold-attempt1.log`, 421 lines): fresh `~sud`/`~tug`, the rootless daemon, the store, then the P0 battery — the rows H1–H15 ran (they print observations, not verdicts; the negatives are P0's gate); the P0 negatives RED stopped before a single row: `mutants.sh: H10: expected exactly one match in desk/app/urgit-ci.hoon, found 0; nothing written` (attempt1 419–421). **Observed:** the P0/P1 mutants sabotaged P1's deadline-close text, which D6(c) rewrote, so the mutant could not be applied — a harness stop, not the build. **Re-anchored:** `8842ec8` — H10/P11 to the P3 deadline close, P10 to the abandon close, P14 to the poll's 401, the same sabotage each. **Pids at that moment:** the ships were booted by `boot.sh`, which logs no pid, and the run stopped before its shutdown step; no daemon process was running (the P0 rows POST events with a bearer of their own, `h9-13.sh`, and start no daemon); the store container and the rootless daemon of this launch were the ones `docker-rootless.sh start`/`store.sh start` printed without pids. The ships were stopped and the piers removed before launch 2 (`boot.sh` refuses an existing pier), so no pid table survives for them.

## Stop 2 — launch 2, 18:03:13 (H10 on the real build)

Launch 2 at 17:56:37 (`cold-attempt2.log`, 612 lines): the P0 rows ran, the negatives RED `FAIL=7 (H9 H10 H11 H12 H13 H14 H15) of 7` (attempt2 513), then GREEN stopped at `PASS=6 … FAIL=1 (H10)` (609): `NOT GREEN: rows failing on the real build: H10` (attempt2 610–612). **Observed:** H10 saw `%reoffered`/`%pending` — the P3 rule itself — because the P0 battery's earlier daemon records were still live, so the ship re-offered the deadline-closed attempt instead of failing it. **Re-anchored:** `fcae120` — H10 asserts D6(c) (re-offered when another live daemon record exists, else `%infrastructure-error` and the candidate `%unknown`) and its mutant leaves the attempt running. **Pids at that moment:** as for stop 1 — no ship pid logged, no daemon process, stopped before the shutdown step; piers removed before launch 3.

## Stop 3 — launch 3, 18:54:46 (P19: two landings on one linked ref)

Launch 3 at 18:08:01 (`cold-attempt3.log`, 1014 lines): the P0 battery whole (positives, RED 7/7, GREEN 7/7, the foreground), P1's rows P1–P18 and P20, and P19 failed (attempt3 985–1014): the linked repository was CI-protected and its push staged and passed, then `landed through the desk (P3 D9): verdict-reason: FAIL (observed: ~, expected: 'landed')`, `master = the candidate: FAIL (observed: a72a51b…, expected: c8e43c7…)`, `the desk holds the candidate's file: FAIL (observed: 0)`; `== rows: PASS=4 (P16 P17 P18 P20) FAIL=1 (P19)`. **Observed:** the build, not the harness — two candidates on the linked ref passed together, the second landing parked over the first's clay-push and both vanished (D9 (a) in Deviations): the guard `=(^ pending-clay)` compares against a wing and never fires (QUESTIONS §5). **Re-anchored:** `b6199e9` (the landing's in-progress guard, `!=(~ …)`, a parked landing with nothing to report refused rather than dropped, R14's race block), then rider 5's `1b0fd07` (the six pre-P3 sites, R17) and rider 4's `58503cf` (group E); P19's harness seeds the desk-shaped tree while unprotected, then protects. **Pids at that moment:** no ship pid logged; runner a of the P1 battery pid `457513` (attempt3 873, started by `p-setup`), alive at the stop; stopped before the shutdown step; the ships were shut down and the piers removed before launch 4.

## Stop 4 — launch 4, 21:18:23 (Q12: the P2-era assertion)

Launch 4 at 19:34:45 (`cold.log` 1; this is the run the Table records): P0 whole (RED 7/7, GREEN 7/7), P1 whole (P1–P20, RED 12/12, GREEN 12/12), the foreground, then P2 through Q11 and a stop on Q12 (`cold.log` 1534–1562): every check of the rotation and the wrong-key daemon passed (`daemon b refused the assignment with a logged reason`, `the reason names the signature`, `daemon b prepared no sandbox`) and one failed — `the ship recorded the refusal as the attempt's reason: FAIL (observed: %reoffered, expected: %infrastructure-error)`. **Observed:** the last P2-era assertion: it expected the wrong-key daemon's attempt to close `%infrastructure-error`, and P3's D6 re-offers a refused assignment on another live daemon (a was up), so the ship answered `%reoffered`. **Re-anchored:** `f8659e7` — Q12 asserts the P3 rule the way H10/P10/P11 do (re-offered when another live daemon exists, else the infrastructure error; the reason names the signature; the refusing daemon's record reads `refused`). The run resumed at 21:38:27 on the same ships at that step (`COLD_FROM=p2 START_AT="q12-13 q12"`, `cold.log` 1563). **Pids at that moment:** `~sud` king `775053` (serf `775979`) and `~tug` king `777033` (serf `778008`) — the processes the 22:54 shutdown found by `/proc/<pid>/cmdline` (`cold.log` 2286–2295), unchanged since the boot; the store container `urgit-ci-store-sud` pid `781616` and the rootless `dockerd` `780957` (2282, 2299), likewise; runner a pid `1106665` (1306, `p2-setup`) and Q12's wrong-key runner b pid `1143587` (1550), both alive when the row stopped.

## Stop 5 — launch 4, 22:54:14 (the START_AT leak)

After the resumed P2 battery ran whole (Q12–Q19, negatives A 9/9, B 1/1, C 1/1 both ways, the foreground, `cold.log` 1563–2268), the P3 battery started at 22:54:14 and ended in the same second: `battery.sh: all steps ran` with no step run (2269–2272), and `cold.sh` went on to the shutdown, which ran whole (2274–2303; `cold.sh: every step ran` at 22:54:24). **Observed:** `cold.sh` had left `START_AT` exported past the battery it named, so the P3 battery skipped every step looking for `q12-13 q12` and reported success — a harness stop of my own, found by reading the log. **Re-anchored:** `cb20628` — `cold.sh` unsets `START_AT` after the first battery and a battery whose `START_AT` names no step exits 2. The ships were restarted on their retained piers by `restart.sh` at 22:55:00/22:55:03 (`%gu` `%.y` on both, 2306–2313), the rootless daemon and the store brought up as `cold.sh` does (2315–2324), and the run resumed at the P3 battery at 22:55:18 (`COLD_FROM=p3`, 2325) — the P3 rows therefore ran on ships restarted once between P2 and P3, not on the process that booted them. **Pids at that moment (the shutdown's own `/proc` table, 2276–2303):**

| Process | Pid | Outcome |
|---|---|---|
| runners c, r1, r2, r3, a, b | — | not running (each row had retired its test daemons; a was stopped by the last negatives phase) |
| store container `urgit-ci-store-sud` | `781616` (`/proc/781616/cmdline = /usr/bin/rustfs /data`) | removed, pid gone, data kept |
| `~tug` king | `777033` (serf `778008`) | SIGTERM, all pier pids gone at 22:54:17, tmux session ended, pier retained (229M) |
| `~sud` king | `775053` (serf `775979`) | SIGTERM, all pier pids gone at 22:54:19, tmux session ended, pier retained (819M) |
| rootless `dockerd` | `780957` | TERM, gone, data root released, `/run/user/1000/ci-p3-opus` down |

## Stop 6 — launch 4, 23:03:01 (R5's count of the refusal line)

The resumed P3 battery ran `p3-setup`, R1, R3, R4 and stopped on R5 (`cold.log` 2439–2469): the rotation, the certificate, Go's verification, the de-listing (`refused` with the reason), the re-enrollment, the re-run and the landing all passed; `daemon a refused the assignment after the rotation: FAIL (observed: 2, expected: 1)`. **Observed:** since S5 the daemon also logs the ship's answer to its abandon, which echoes the reason, so the refusal line matched twice; the row had not been re-run after S5 (its group A negatives were S1's). **Re-anchored:** `119b996` — R5 counts the daemon's own `no result:` line (as R9 already did). The battery resumed at R5 at 23:03:50 (`START_AT="r1-r5 r5"`, 2470); R1, R3 and R4 stand from the first P3 pass of this run. **Pids at that moment:** `~sud` king `1537459` (serf `1537470`) and `~tug` king `1537706` (serf `1537715`) — the restarted processes the final shutdown found by `/proc` (3626–3636), unchanged from the 22:55 restart; store container `1540517`, `dockerd` `1539931` (3622, 3639), likewise; runner a pid `1555774` (2456, re-enrolled by R5 itself — the `p3-setup` a `1544180` (2354) had been revoked and removed by the row), alive; R4's runner b `1545675` (2408) had exited on its revocation and its record was removed.

## Stop 7 — launch 4, 23:09:41 (R7 on a store advertised at 127.0.0.1)

R5 (re-run), R2, R6, R11a and R11b passed (2476–2615); R7 stopped (2616–2645): the probe URL, the log route's 302 and its LAN host passed, and `np reads the log through the presigned link: sha256 = the ship's handle: FAIL (observed: e3b0c442…, expected: 7bfc9bb8…)` — the sha256 of an empty body; the RED case (`%storage` at 127.0.0.1) then passed as written, and `the probe names the LAN endpoint again: PASS (observed: 127.0.0.1:8392)` shows why. **Observed:** `cold.sh` sources the P2 lib, whose env exports `STORE_ADVERTISE=127.0.0.1`, and the P3 lib keeps a caller's value, so `p3-setup` had pointed `%storage` at 127.0.0.1 and R7's fetch from np was a connection failure on the "LAN" case — the same address as its RED case. **Re-anchored:** `5bb55d5` — the P3 battery gets `STORE_ADVERTISE=192.168.1.229` from `cold.sh` (`p3_battery`). `%storage` was re-pointed at the LAN address by hand (logged, 2648–2650, 23:10:30) and the battery resumed at R7 at 23:10:41 (`START_AT="r7-r8 r7"`, 2651). The P3 rows before R7 (R1–R6, R11a, R11b) therefore ran with the store advertised on 127.0.0.1: none of them reads the endpoint's host (the daemon uploads from the host either way; R6's log handle is a route, not a link); R7, R8 and every later row ran on the LAN address. **Pids at that moment:** the kings, the store and `dockerd` as at stop 6 (`1537459`/`1537706`, `1540517`, `1539931`); runner a pid `1575729` (2611, restarted by R11b), alive; R11b's runner b (`1569141`, 2597) retired by the row (2612).

## Final shutdown

The pick's cold run (launch 4), `.scratch/ci-p3/shutdown.sh` at 01:32:00, every process by `/proc`-verified pid (`cold.log` 3614–3644):

| Process | Pid | Outcome |
|---|---|---|
| runners c, r1, r2, r3, a, b | — | not running (retired by their rows; a stopped by the last negatives phase) |
| store container `urgit-ci-store-sud` | `1540517` (`/proc/1540517/cmdline = /usr/bin/rustfs /data`) | removed, pid gone, data kept under `.scratch/tmp/store-data` |
| `~tug` king | `1537706` (serf `1537715`) | SIGTERM, all pier pids gone at 01:32:03, tmux `ci-p3-opus-tug` ended with the ship, pier retained (237M) |
| `~sud` king | `1537459` (serf `1537470`) | SIGTERM, all pier pids gone at 01:32:07, tmux `ci-p3-opus-sud` ended with the ship, pier retained (1.6G) |
| rootless `dockerd` | `1539931` | TERM, gone, `.scratch/tmp/docker-data` released, `/run/user/1000/ci-p3-opus` down |

`cold.sh: every step ran (2026-09-20T01:32:11-05:00)`. The tree was clean at `01e668a`.

**The close-out's build pair**, shut down before T6 by `.scratch/ci-p3/shutdown.sh` at 12:36 (`.scratch/tmp/before-cold/shutdown-build-pair.log`), then its piers moved aside as `/var/home/michael/piers/urgit-ci-p3c-{mex,ryt}.build` so the cold run could boot the footer's names fresh:

| Process | Pid | Outcome |
|---|---|---|
| runner a (`/proc/2773288/cmdline` = `urgit-runner -config …/runner/a/config.toml`) | `2773288` | TERM, stopped |
| store container `urgit-ci-store-mex` | `2753874` (`/usr/bin/rustfs /data`) | removed, pid gone, data kept |
| `~ryt` king | `2748796` (serf `2749960`) | SIGTERM, all pier pids gone at 12:36:16, tmux `ci-p3-closeout-ryt` ended with the ship, pier retained (214M) |
| `~mex` king | `2746183` (serf `2747521`) | SIGTERM, all pier pids gone at 12:36:19, tmux `ci-p3-closeout-mex` ended with the ship, pier retained (219M) |
| rootless `dockerd` | `2753197` | TERM, gone, `.scratch/tmp/docker-data` released, `/run/user/1000/ci-p3-closeout` down |

**The close-out's cold run**, `.scratch/ci-p3/shutdown.sh` at 18:19:52 as the run's last step (`cold.log` 3540–3573):

| Process | Pid | Outcome |
|---|---|---|
| runners c, r1, r2, r3, a, b | — | not running (retired by their rows; a stopped by the last negatives phase) |
| store container `urgit-ci-store-mex` | `2818692` (`/proc/2818692/cmdline = /usr/bin/rustfs /data`) | removed, pid gone, data kept under `.scratch/tmp/store-data` |
| `~ryt` king | `2813504` (serf `2814844`) | SIGTERM, all pier pids gone at 18:19:55, tmux `ci-p3-closeout-ryt` ended with the ship, pier retained (237M) |
| `~mex` king | `2810665` (serf `2812174`) | SIGTERM, all pier pids gone at 18:19:58, tmux `ci-p3-closeout-mex` ended with the ship, pier retained (1.6G) |
| rootless `dockerd` | `2818091` | TERM, gone, `.scratch/tmp/docker-data` released, `/run/user/1000/ci-p3-closeout` down |

`cold.sh: every step ran (2026-09-20T18:20:02-05:00)`.

**After R18**, the pair having been restarted on its retained piers at 18:22 by `restart.sh` (`.scratch/tmp/r18-run-try1.log`) for R18's tries and its RED/GREEN, `.scratch/ci-p3/shutdown.sh` at 19:41 (`.scratch/tmp/shutdown-final.log`) — the close-out's last:

| Process | Pid | Outcome |
|---|---|---|
| runners c, r1, r2, r3, a, b | — | not running (a stopped by group F's GREEN phase) |
| store container `urgit-ci-store-mex` | `3848488` (`/proc/3848488/cmdline = /usr/bin/rustfs /data`) | removed, pid gone, data kept under `.scratch/tmp/store-data` |
| `~ryt` king | `3845693` (serf `3845709`) | SIGTERM, all pier pids gone at 19:42:00, tmux `ci-p3-closeout-ryt` ended with the ship, pier retained (253M) |
| `~mex` king | `3845370` (serf `3845380`) | SIGTERM, all pier pids gone at 19:42:02, tmux `ci-p3-closeout-mex` ended with the ship, pier retained (1.8G) |
| rootless `dockerd` | `3847910` | TERM, gone, `.scratch/tmp/docker-data` released, `/run/user/1000/ci-p3-closeout` down |

Retained: `/var/home/michael/piers/urgit-ci-p3c-{mex,ryt}` (the cold run's, with R18's `urgit` and the deletion case's repositories on `~mex`), `/var/home/michael/piers/urgit-ci-p3c-{mex,ryt}.build` (the build pair). No other pier, container, port or state dir was touched.

## Fresh-pair battery

The operator's cold run of the ratified tree (`01e668a`) on a fresh pair `~wex`/`~nex` (ports 8410/8411, rootless `/run/user/1000/p3v-opus`, store 8412, `DAEMON_CAPACITY=3`), 2026-09-20 01:45:08 → 07:10:32, one pass, every step ran: `.scratch/battery/p3-verify-opus/cold.log` in the main worktree, copied to this worktree's `.scratch/p3-verify-opus.cold.log` (3523 lines). Its phase lines (the `################` headers with their timestamps, every `== red:`/`== green:`/`== rows:` verdict, and the closing line; `<battery>` stands for `/var/home/michael/workspace/urbit/urgit/.scratch/battery/p3-verify-opus`):

```
################ cold battery: ships ~wex :8410 (/var/home/michael/piers/urgit-p3v-wex, tmux ci-p3-opus-wex) and ~nex :8411 (/var/home/michael/piers/urgit-p3v-nex), rootless /run/user/1000/p3v-opus, store http://127.0.0.1:8412 advertised as http://127.0.0.1:8412 for P0-P2 and http://192.168.1.229:8412 for P3, DAEMON_CAPACITY=3  (2026-09-20T01:45:08-05:00)
################ <battery>/.scratch/ci-p0/boot.sh  (2026-09-20T01:45:08-05:00)
################ boot2  (2026-09-20T01:46:04-05:00)
################ <battery>/.scratch/ci-p1/docker-rootless.sh start  (2026-09-20T01:47:01-05:00)
################ store_up  (2026-09-20T01:47:01-05:00)
################ <battery>/.scratch/ci-p0/battery.sh  (2026-09-20T01:47:03-05:00)
################ setup  (2026-09-20T01:47:03-05:00)
################ h1  (2026-09-20T01:47:05-05:00)
################ h2  (2026-09-20T01:47:07-05:00)
################ h3  (2026-09-20T01:47:12-05:00)
################ h4  (2026-09-20T01:47:12-05:00)
################ h5  (2026-09-20T01:47:17-05:00)
################ h6  (2026-09-20T01:47:23-05:00)
################ h7  (2026-09-20T01:47:50-05:00)
################ h8  (2026-09-20T01:47:55-05:00)
################ h9-13  (2026-09-20T01:47:58-05:00)
################ h14  (2026-09-20T01:48:48-05:00)
################ h15  (2026-09-20T01:49:00-05:00)
################ negatives red  (2026-09-20T01:49:03-05:00)
== red: PASS=0 () FAIL=7 (H9 H10 H11 H12 H13 H14 H15) of 7 rows; build: MUTATED
################ negatives green  (2026-09-20T01:50:31-05:00)
== green: PASS=7 (H9 H10 H11 H12 H13 H14 H15) FAIL=0 () of 7 rows; build: clean (real build)
################ foreground  (2026-09-20T01:51:49-05:00)
################ <battery>/.scratch/ci-p1/battery.sh  (2026-09-20T01:51:57-05:00)
################ p-setup  (2026-09-20T01:51:57-05:00)
################ p1  (2026-09-20T01:54:27-05:00)
################ p2  (2026-09-20T01:54:31-05:00)
################ p3-5  (2026-09-20T01:54:47-05:00)
################ p6-9  (2026-09-20T01:55:27-05:00)
== rows: PASS=4 (P6 P7 P8 P9) FAIL=0 ()
################ p10-14 p10 p11 p12 p13 p14  (2026-09-20T01:56:17-05:00)
== rows: PASS=5 (P10 P11 P12 P13 P14) FAIL=0 ()
################ p15-prep  (2026-09-20T02:08:20-05:00)
################ p15  (2026-09-20T02:08:23-05:00)
################ p16-20  (2026-09-20T02:24:03-05:00)
== rows: PASS=5 (P16 P17 P18 P19 P20) FAIL=0 ()
################ negatives red  (2026-09-20T02:26:39-05:00)
== red: FAIL-with-tripwire=12 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P20 P14) FAIL-wrong-reason=0 () PASS=0 () of 12 rows; build: MUTATED
################ negatives green  (2026-09-20T03:06:13-05:00)
== green: PASS=12 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P20 P14) FAIL=0 () of 12 rows; build: clean (real build)
################ foreground  (2026-09-20T03:13:40-05:00)
################ <battery>/.scratch/ci-p2/battery.sh  (2026-09-20T03:14:12-05:00)
################ p2-setup  (2026-09-20T03:14:12-05:00)
################ q2-4 q2  (2026-09-20T03:16:32-05:00)
################ q2-4 q3  (2026-09-20T03:18:04-05:00)
################ q2-4 q4  (2026-09-20T03:18:09-05:00)
################ q5-8 q5a  (2026-09-20T03:18:58-05:00)
################ q5-8 q5  (2026-09-20T03:19:07-05:00)
################ q5-8 q6  (2026-09-20T03:19:52-05:00)
################ q5-8 q7  (2026-09-20T03:20:13-05:00)
################ q5-8 q8  (2026-09-20T03:20:28-05:00)
################ q9-11 q9  (2026-09-20T03:21:13-05:00)
################ q9-11 q10  (2026-09-20T03:21:33-05:00)
################ q9-11 q11  (2026-09-20T03:21:46-05:00)
################ q12-13 q12  (2026-09-20T03:22:04-05:00)
################ q12-13 q13  (2026-09-20T03:22:36-05:00)
################ q18  (2026-09-20T03:23:42-05:00)
################ q14-16 q14  (2026-09-20T03:43:55-05:00)
################ q14-16 q15  (2026-09-20T03:43:55-05:00)
################ q14-16 q16  (2026-09-20T03:43:57-05:00)
################ q19  (2026-09-20T03:43:58-05:00)
################ q-negatives red Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19  (2026-09-20T03:44:29-05:00)
== red: FAIL-with-tripwire=9 (Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19) FAIL-wrong-reason=0 () PASS=0 () of 9 rows; build: MUTATED
################ q-negatives green Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19  (2026-09-20T04:01:01-05:00)
== green: PASS=9 (Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19) FAIL=0 () of 9 rows; build: clean (real build)
################ q-negatives red Q5  (2026-09-20T04:07:10-05:00)
== red: FAIL-with-tripwire=1 (Q5) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
################ q-negatives green Q5  (2026-09-20T04:14:46-05:00)
== green: PASS=1 (Q5) FAIL=0 () of 1 rows; build: clean (real build)
################ q-negatives red Q8  (2026-09-20T04:22:14-05:00)
== red: FAIL-with-tripwire=1 (Q8) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
################ q-negatives green Q8  (2026-09-20T04:29:49-05:00)
== green: PASS=1 (Q8) FAIL=0 () of 1 rows; build: clean (real build)
################ foreground  (2026-09-20T04:37:20-05:00)
################ p3_battery  (2026-09-20T04:37:47-05:00)
################ p3-setup  (2026-09-20T04:37:47-05:00)
################ r1-r5 r1  (2026-09-20T04:40:06-05:00)
################ r1-r5 r3  (2026-09-20T04:40:17-05:00)
################ r1-r5 r4  (2026-09-20T04:40:27-05:00)
################ r1-r5 r5  (2026-09-20T04:44:49-05:00)
################ r2-r6 r2  (2026-09-20T04:45:31-05:00)
################ r2-r6 r6  (2026-09-20T04:45:34-05:00)
################ r9-r11 r11a  (2026-09-20T04:45:41-05:00)
################ r9-r11 r11b  (2026-09-20T04:46:19-05:00)
################ r7-r8 r7  (2026-09-20T04:50:35-05:00)
################ r7-r8 r8  (2026-09-20T04:50:41-05:00)
################ r9-r11 r9  (2026-09-20T04:50:45-05:00)
################ r9-r11 r10  (2026-09-20T05:08:14-05:00)
################ r9-r11 r11  (2026-09-20T05:13:53-05:00)
################ r14-r16 r14  (2026-09-20T05:15:44-05:00)
################ r14-r16 r15  (2026-09-20T05:16:34-05:00)
################ r14-r16 r16  (2026-09-20T05:16:46-05:00)
################ r17  (2026-09-20T05:17:10-05:00)
################ r12  (2026-09-20T05:17:25-05:00)
################ r-negatives red R4 R5 R3  (2026-09-20T05:17:25-05:00)
== red: FAIL-with-tripwire=3 (R4 R5 R3) FAIL-wrong-reason=0 () PASS=0 () of 3 rows; build: MUTATED
################ r-negatives green R4 R5 R3  (2026-09-20T05:24:22-05:00)
== green: PASS=3 (R4 R5 R3) FAIL=0 () of 3 rows; build: clean (real build)
################ r-negatives red R4b R5b  (2026-09-20T05:29:42-05:00)
== red: FAIL-with-tripwire=2 (R4b R5b) FAIL-wrong-reason=0 () PASS=0 () of 2 rows; build: MUTATED
################ r-negatives green R4b R5b  (2026-09-20T05:48:25-05:00)
== green: PASS=2 (R4b R5b) FAIL=0 () of 2 rows; build: clean (real build)
################ r-negatives red R9 R10  (2026-09-20T05:53:33-05:00)
== red: FAIL-with-tripwire=2 (R9 R10) FAIL-wrong-reason=0 () PASS=0 () of 2 rows; build: MUTATED
################ r-negatives red R11b  (2026-09-20T06:22:06-05:00)
== red: FAIL-with-tripwire=1 (R11b) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
################ r-negatives green R9 R10 R11b  (2026-09-20T06:28:22-05:00)
== green: PASS=3 (R9 R10 R11b) FAIL=0 () of 3 rows; build: clean (real build)
################ r-negatives red R16 R14 R17  (2026-09-20T07:05:27-05:00)
== red: FAIL-with-tripwire=3 (R16 R14 R17) FAIL-wrong-reason=0 () PASS=0 () of 3 rows; build: MUTATED
################ r-negatives green R16 R14 R17  (2026-09-20T07:08:14-05:00)
== green: PASS=3 (R16 R14 R17) FAIL=0 () of 3 rows; build: clean (real build)
################ foreground  (2026-09-20T07:09:55-05:00)
################ <battery>/.scratch/ci-p3/shutdown.sh  (2026-09-20T07:10:22-05:00)
cold.sh: every step ran (2026-09-20T07:10:32-05:00)
```

The verdict this section carries: P0 negatives RED 7/7 GREEN 7/7; P1 rows 4/4, 5/5, 5/5, negatives RED 12/12 GREEN 12/12; P2 negatives A RED 9/9 GREEN 9/9, B (Q5) 1/1 both, C (Q8) 1/1 both; P3 negatives A RED 3/3 GREEN 3/3, B RED 2/2 GREEN 2/2, C RED 2/2 and D RED 1/1 with GREEN C+D 3/3, E RED 3/3 GREEN 3/3; every foreground clean; the shutdown by `/proc` pid; `cold.sh: every step ran` — the clean gate, independent of the chair's own run.

## Close-out cold run

The close-out's own battery (T6): `.scratch/ci-p3/cold.sh` unmodified except the two rows T1/T2 added to `battery.sh`'s step list and group A, launched once at 12:37:07 on 2026-09-20 from the footer's fresh piers (`~mex` :8420, `~ryt` :8421, neither existed; `boot.sh` with `--loom 34`, no `-p`; rootless Docker `/run/user/1000/ci-p3-closeout`, RustFS `urgit-ci-store-mex` on 8422 advertised at 127.0.0.1 for P0–P2 and 192.168.1.229 for P3, `DAEMON_CAPACITY=3`), through the P0, P1, P2 and P3 batteries and the shutdown, to `cold.sh: every step ran` at 18:20:02 — **no stop, no resumption, so no `## Stop` section of its own; a pass of the unqualified kind.** The log is this worktree's `.scratch/tmp/cold.log` (3573 lines); its phase and verdict lines (`<worktree>` for this worktree's path):

```
################ cold battery: ships ~mex :8420 (/var/home/michael/piers/urgit-ci-p3c-mex, tmux ci-p3-closeout-mex) and ~ryt :8421 (/var/home/michael/piers/urgit-ci-p3c-ryt), rootless /run/user/1000/ci-p3-closeout, store http://127.0.0.1:8422 advertised as http://127.0.0.1:8422 for P0-P2 and http://192.168.1.229:8422 for P3, DAEMON_CAPACITY=3  (2026-09-20T12:37:07-05:00)
################ <worktree>/.scratch/ci-p0/battery.sh  (2026-09-20T12:39:00-05:00)
################ negatives red  (2026-09-20T12:41:00-05:00)
== red: PASS=0 () FAIL=7 (H9 H10 H11 H12 H13 H14 H15) of 7 rows; build: MUTATED
################ negatives green  (2026-09-20T12:42:28-05:00)
== green: PASS=7 (H9 H10 H11 H12 H13 H14 H15) FAIL=0 () of 7 rows; build: clean (real build)
################ foreground  (2026-09-20T12:43:46-05:00)
################ <worktree>/.scratch/ci-p1/battery.sh  (2026-09-20T12:43:53-05:00)
== rows: PASS=4 (P6 P7 P8 P9) FAIL=0 ()
== rows: PASS=5 (P10 P11 P12 P13 P14) FAIL=0 ()
== rows: PASS=5 (P16 P17 P18 P19 P20) FAIL=0 ()
################ negatives red  (2026-09-20T13:23:28-05:00)
== red: FAIL-with-tripwire=12 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P20 P14) FAIL-wrong-reason=0 () PASS=0 () of 12 rows; build: MUTATED
################ negatives green  (2026-09-20T14:03:07-05:00)
== green: PASS=12 (P1 P7 P8 P9 P10 P11 P12 P13-overlap P17 P18 P20 P14) FAIL=0 () of 12 rows; build: clean (real build)
################ foreground  (2026-09-20T14:10:30-05:00)
== vectors failing: 0; go steps failing: 0
################ <worktree>/.scratch/ci-p2/battery.sh  (2026-09-20T14:11:04-05:00)
################ q-negatives red Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19  (2026-09-20T14:49:15-05:00)
== red: FAIL-with-tripwire=9 (Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19) FAIL-wrong-reason=0 () PASS=0 () of 9 rows; build: MUTATED
################ q-negatives green Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19  (2026-09-20T15:05:46-05:00)
== green: PASS=9 (Q4 Q5a Q6 Q10 Q11 Q12 Q13 Q16 Q19) FAIL=0 () of 9 rows; build: clean (real build)
################ q-negatives red Q5  (2026-09-20T15:11:55-05:00)
== red: FAIL-with-tripwire=1 (Q5) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
################ q-negatives green Q5  (2026-09-20T15:19:31-05:00)
== green: PASS=1 (Q5) FAIL=0 () of 1 rows; build: clean (real build)
################ q-negatives red Q8  (2026-09-20T15:27:00-05:00)
== red: FAIL-with-tripwire=1 (Q8) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
################ q-negatives green Q8  (2026-09-20T15:34:35-05:00)
== green: PASS=1 (Q8) FAIL=0 () of 1 rows; build: clean (real build)
################ foreground  (2026-09-20T15:42:06-05:00)
== vectors failing: 0; go steps failing: 0
################ p3_battery  (2026-09-20T15:42:34-05:00)
################ r-negatives red R4 R5 R3 R6a R15b  (2026-09-20T16:23:36-05:00)
== red: FAIL-with-tripwire=5 (R4 R5 R3 R6a R15b) FAIL-wrong-reason=0 () PASS=0 () of 5 rows; build: MUTATED
################ r-negatives green R4 R5 R3 R6a R15b  (2026-09-20T16:31:38-05:00)
== green: PASS=5 (R4 R5 R3 R6a R15b) FAIL=0 () of 5 rows; build: clean (real build)
################ r-negatives red R4b R5b  (2026-09-20T16:38:01-05:00)
== red: FAIL-with-tripwire=2 (R4b R5b) FAIL-wrong-reason=0 () PASS=0 () of 2 rows; build: MUTATED
################ r-negatives green R4b R5b  (2026-09-20T16:56:45-05:00)
== green: PASS=2 (R4b R5b) FAIL=0 () of 2 rows; build: clean (real build)
################ r-negatives red R9 R10  (2026-09-20T17:01:55-05:00)
== red: FAIL-with-tripwire=2 (R9 R10) FAIL-wrong-reason=0 () PASS=0 () of 2 rows; build: MUTATED
################ r-negatives red R11b  (2026-09-20T17:32:26-05:00)
== red: FAIL-with-tripwire=1 (R11b) FAIL-wrong-reason=0 () PASS=0 () of 1 rows; build: MUTATED
################ r-negatives green R9 R10 R11b  (2026-09-20T17:42:27-05:00)
== green: PASS=3 (R9 R10 R11b) FAIL=0 () of 3 rows; build: clean (real build)
################ r-negatives red R16 R14 R17  (2026-09-20T18:14:55-05:00)
== red: FAIL-with-tripwire=3 (R16 R14 R17) FAIL-wrong-reason=0 () PASS=0 () of 3 rows; build: MUTATED
################ r-negatives green R16 R14 R17  (2026-09-20T18:17:41-05:00)
== green: PASS=3 (R16 R14 R17) FAIL=0 () of 3 rows; build: clean (real build)
################ foreground  (2026-09-20T18:19:24-05:00)
== vectors failing: 0; go steps failing: 0
################ <worktree>/.scratch/ci-p3/shutdown.sh  (2026-09-20T18:19:52-05:00)
cold.sh: every step ran (2026-09-20T18:20:02-05:00)
```

Every row of every battery passed as a step (P0 H1–H15 ran; P1 P1–P20; P2 Q2–Q19; P3 R1–R17 with R6a and R15b); every RED counted its tripwire and every GREEN was whole; the three foregrounds clean (23 vectors, `npm test` 145/145, `go test`/`-race`/vet/gofmt, the live Docker boundary, the static binary). R6a in the run: both foreign subscribes `err` with the watch-ack naming the guard, no diff, the own-session control a fact (`p3-r2-r6-r6a.log`); under group A's mutant both `ok` — `R6a RED: foreign ship watch accepted` (`rneg-red-R6a.log`). R15b in the run: the four raw events 202, the outputs `'***'`, `'***'`, `'key: *** (end)'`, `'***'`, the job passed and the candidate landed (`p3-r14-r16-r15b.log`); under the mutant the raw lines persisted — `R15b RED: the ship persisted a raw credential line` (`rneg-red-R15b.log`).

## Deviations

Every derivation was built as written unless listed here. Each departure cites what made it. The riders are the operator's: rider 1 (`f45de4c`, D6(b) → re-enroll), rider 2 (`98f7caf`, the fence, the matrix refusal, `stale` = `stale-after`, the heartbeat, the re-offer readings — the answers to `QUESTIONS-CI-P3.md` §2 and §4), rider 3 (`ada93e3`, D6(g) ownership-aware reconciliation, astra §4). The five boxes in `QUESTIONS-CI-P3.md` stay as the record, none open: §1 (Eyre's channel carries `%urgit-ci`'s facts — anticipated by the brief, answered in the tree), §2 (the stale constant — ruled by rider 2 `98f7caf`), §3 (`%rotate-ci-key` re-signs the certificate — anticipated, R5), §4 (the fence, the matrix refusal, the re-offer readings — ruled by rider 2), §5 (the six never-true `pending-clay` guards — ruled by rider 5 `bceeb01`, applied in `1b0fd07`); rider 3 (`ada93e3`) and rider 4 (`627644d`) were rulings on astra's boxes that this tree carries (D6(g); the R16 fence).

- **T8 — what the generators print.** The rider says each of the 23 vector generators prints `passed=N of=M`; in this tree only `ci-plan-vector` does (it keeps a `results` list); the other 22 assert with `?>` and return `%.y`, or dump values for inspection. The hoon-vectors job takes the rule the harness's foreground (`ci-p1/foreground.sh`) has used since P1 — a generator whose source ends in a loobean must print `%.y`; one that dumps values must build and run without a crash — echoes the `passed=N of=M` a generator prints beside its name, and prints the run's own `vectors: passed=N of=M` at the end; R18 asserts every vector's line, that summary, and ci-plan-vector's own count. No generator was changed (`desk/gen` is outside T8's fence).
- **T8 — Go is not in the act image.** `catthehacker/ubuntu:act-latest` carries Node 24 and gcc but no Go; `go-test` takes its toolchain from `actions/setup-go@v5` with `go-version-file: runner/go.mod` (go1.27.1), so the job and a developer's shell agree. `fe-test` uses the runner's own Node (printed).
- **T8 — the commit wait through click answers a tas.** click prints a thread's result as a bare noun (`[0 %avow 0 %noun 0]` for `%.y`), so the boot action's poll for `/=urgit=/desk/bill` answers `%present`/`%absent`; the first pre-flight waited 300 s on a `%.y` that could never print.
- **T8 — the vector step waits for the dojo's echo.** `tmux send-keys` returns before the pane moves; a step that checked the prompt right after typing read the previous prompt and "ran" 23 generators in 0.4 s. The step waits for `> <line>` to appear, then for the bare prompt — `dojo.sh`'s `sleep 1` made exact.
- **T8 — pre-flighted under the static act on the host's Docker, never the harness daemon, and on a scratch copy of the tree for the RED**: the worktree's `desk/` is what the cold run's rebuild steps copy into the ship, so a broken fixture there would have turned the P3 foreground red while the run was on.
- **T8 — R18 is a battery step from T8 on (after R17; its mutant group F after E), but this close-out's cold run predates it**: its evidence is the row run on the retained pair after the cold run (as rider 4's group E was for the pick), the ships restarted by `restart.sh`. The first three tries of the step run are the history of the finding below.
- **T8b — a deleted repository's CI records outlived it (R18 found it; fixed at the source).** R18's tries on the retained pair: **try 1** (18:22, `p3-r18-try1.log`) — 31 of 32 checks passed both ways, the one FAIL the row's own grep of the pass line (`ci-plan-vector: %.y (passed=31 of=31)` is what the job prints; the row looked for the RED-shaped `ci-plan-vector passed=…`); the row's grep fixed, the repository `DELETE`d (200) for a clean re-run. **Try 2** (18:30, `p3-r18-try2.log`) — the seed push into the re-created `urgit` was refused: `%urgit-ci` still CI-protected its master (`CI-protected (scry): PASS (observed: %.y)` before the row had protected anything; `CI required -> 409`); stopped by hand and the row given an un-protect guard. **Try 3** (18:41, `r18-run.log`) — the broken fixture failed as it should, then this tree's HEAD `555d8ff` landed UNSTAGED (`c81eafc..555d8ff -> master`, `the push was staged: FAIL`): try 1's candidate for that same `[repo ref oid]` had reached `%passed`, and `DELETE /repository/urgit` had not reached `%urgit-ci`, whose records live under the name — its candidates, attempts, assignments, the protection, the policy, the credentials, the name in a daemon's binding — so the push gate's eligibility peek answered `%.y` and the push skipped CI; the row hung in `wait_cand` on an empty id. The operator read the log and named it: a real defect, the ship-side twin of astra's §4 (state that outlives its owner), in fence — fix it at the source, not with another harness guard. The hung row's processes were found by `/proc` cwd under this worktree and killed. **The fix (`8451684`):** `%urgit`'s `%delete` pokes `%urgit-ci` `[%repository-deleted name]` on `/ci/deleted/<name>` (a new `action:ci` case, the union only; no state mold change) and `%urgit-ci` drops every record under the name — candidates, their attempts and assignments, the `ci-protected` entry, the policy, the credentials — and the name from every daemon's `repos` (a daemon bound only to it is left bound to nothing, `[~ {}]`, not returned to the pool: the operator drew that fence and widens it in the panel); the eligibility peek answers `%.n` afterwards. The manual un-protect guard is gone from the row; its DELETE of an earlier run's `urgit` stays as the re-run convenience. **R18 opens with the deletion case** (its shape in the Table) and its mutant — `%repository-deleted` drops no candidate — is group F, RED then GREEN on the retained pair (the Negatives line). The row also grew a fourth check in T8b's wake, `the candidate is a fresh record, created after the deletion`, whose first GREEN misfired on the row's own regex (above).
- **Record convention — a provider safeguard is not a verdict (the P2 close-out's T6).** The safeguard fired once on this chair, on a tool RESULT (a read of daemon a's log during R5's first run, 2026-09-19 ~11:20; the relay answered "retry with Opus 5"); no row was interrupted mid-verdict and every row's verdict here is from its own run. Nothing is recorded `provider-blocked, not run`.
- **The stage order: S7 after S8.** The brief lists S7 (regression) before S8 (D9). D9 changes `desk/app/urgit.hoon`, `ci-event.hoon`, the daemon and two P1/P2 rows (below); a regression run before it would have recorded a tree the final one is not. R13 is one cold run over the final tree (`.scratch/ci-p3/cold.sh`: P0 → P1 → P2 → P3 batteries from fresh piers, then the shutdown); the observed columns above are re-read from it. It took four launches — the first three stopped (the P0 mutant anchor, H10 on the real build, P19) and the fourth stopped and resumed four times (Q12, the `START_AT` leak, R5's count, R7's store address) — each a `## Stop <k>` section above with its observation, its re-anchoring commit and its pids; its log is `.scratch/tmp/cold.log` (one file, each resumption marked), the stopped launches `.scratch/tmp/before-cold/cold-attempt{1,2,3}.log`.
- **D1 — the mint is a route, not a poke.** The union has no `%mint-enroll-token`: only the session-authorized `POST ci/runners/mint` mints, so the token exists exactly in that one answer (`token`, `configSnippet`, `shipUrl` from the request's `host` header and scheme, the record). The dojo generator is deleted. The harness's `mint.sh` posts the route (H6 and H12 of the P0 battery followed).
- **D2 — expire also removes a revoked record.** `%expire-token` deletes a minted-not-enrolled record (409 `daemon is enrolled; revoke it instead` on a live enrollment) and a revoked one (the panel's **Remove**), so a retired daemon does not stay in the list forever; a refused record is revoked first (R5, R9).
- **D2 — the revoked daemon's 401 says so.** The poll answers `401 revoked by the ship` (not the generic `daemon authentication required`), the client maps it to `ErrRevoked`, the daemon logs `revoked by the ship`, cancels its running work and exits **5**; `ExitEnrollmentLost` (3) stays for a bearer the ship does not know.
- **D2b — the daemon maps every `runs-on` label to the runner image for act.** act skips a job whose `runs-on` it has no platform for: `[self-hosted, big-mem]` exited 0 without a jobResult (R11a's first run). The daemon adds `-P <label>=<image>` for each label of the assigned job (`plan.Walk` on the projection's source), `ubuntu-latest` included. The ship's match against the daemon's declared labels is unchanged.
- **D3 — `stale` is `stale-after` (rider 2).** `healthy-within` (2× the poll window) and the "healthy at capacity" guess (QUESTIONS §2, assumption 2) were struck; `GET ci/runners` answers `staleAfter` (300) and the client re-derives the pip on it; the daemon polls at capacity (D6 f), so `last-seen` is liveness (R4: b, busy, polled and exited 13 s after its revoke).
- **D3 — a `refused` reason and a `revoked` stamp on the record.** `daemon` gains `refused=(unit @t)` (the abandon's reason, so the panel says why) beside `revoked=(unit @da)`, `labels`, `repos`; `job` gains `runs-on` and `timeout` (`timeout-minutes`); `attempt-status` gains `%reoffered` (the last entry stays `%infrastructure-error`, the bunt rule). All `state-0` in place; the P3 build ran on a nuke/revive.
- **D4 — the facts are emitted before the event's other cards.** Arvo runs a card to completion before the next: the `%land-candidate` poke to `%urgit` answers `%landed` inside the same event, and its fact was numbered before the verdict's (R6's first run: `passed/landed` then `passed/null`). `live-facts` runs against a snapshot taken before the event, in the helper door with the new state (inline in `on-poke`/`on-arvo`: the agent door has exactly its ten arms), and its cards go first.
- **D4 — the runner facts are rate-limited by `announced`.** A heartbeat touch produces a runner fact at most every 20 s per daemon (`announce-every`); any other change to the record is a fact at once; a removed record is `runner-gone`. Runner facts ride the runners path and every watched repository path (the CI tab's first-run message reads them).
- **D4 — the candidate row carries the plan.** `plan: [{id, workflow, stage, needs, runsOn, timeoutMinutes}]` on the candidate JSON, so the CI tab's job rows show `runs-on` (D2b) from the row itself.
- **D5 — the probe is unsigned.** The ship has no outbound HTTP and cannot PUT the zero-byte `ci/_probe` the brief imagined; none is needed: `GET ci/storage/probe` answers `<endpoint>/<bucket>/ci/_probe` unsigned, the store answers 403 to it, and any HTTP answer proves the endpoint reachable from the browser where a `127.0.0.1` link draws a `NetworkError` (R7 from `np`: 403 vs `000 exit=7`).
- **D5 — CORS is a second requirement, measured.** RustFS 1.0.0 answers no `access-control-allow-origin` without a bucket CORS rule, and a browser then fails the log view (a cross-origin fetch of the presigned link) with the same `NetworkError`. The fe probe fetches cors-mode then no-cors, telling a store that refuses this origin (its own red sentence) from an unreachable one (D5's sentence). The fixture gains `store.sh cors` (a bucket rule for GET/HEAD, applied by `start`); the README's §1 says the bucket needs one.
- **D5 — the store's `public-url-base` is not read.** `%storage` has such a setting (a Landscape upload convenience); CI signs links with the endpoint alone. Named in the README's limitations.
- **D6 (a) — no other daemon: the attempt closes as P1 did** (rider 2 D6 e). `other-daemon-exists` is capacity-aside: a busy other daemon means the job waits. (b) A refusal on a PLAN attempt reached the ship as a plan `error`, not an abandon (the daemon's `fail` posts a plan error for a plan kind), so D6(b) missed it in R5's first run and the candidate failed `plan-invalid`; the daemon now abandons a refused assignment whatever its kind, and the ship treats a plan `error` carrying the refusal prefix as an abandon (`give-up`, one path). (c) The deadline handler re-offers once then `runner went silent`; `timeout-minutes + 2 min` when declared, `~h1` otherwise; a zero-event attempt at its deadline with no other daemon keeps P1's `no result arrived before the deadline`. `reoffer-unfetched` (the T7 ghost) re-offers an undelivered assignment once its daemon's record is stale, refused or revoked. (d) `%revoke-daemon` uses `reoffer-attempt`.
- **D6 (c) — the daemon's reporting phase has a grace past the deadline.** act is bound by the assignment's deadline (the same number as the ship's re-offer timer); relaying its last lines, the upload and the result were bound by it too, so a runner resumed at the re-offer abandoned for an expired context instead of reporting (R10's first runs). The relay, upload and result now run under `reportingContext` (two minutes past the deadline); the ship's refusal of the late lines is `attempt is closed`; the daemon claims only a jobResult the ship accepted, so its last word for a closed attempt is an abandon, answered idempotently (200, the attempt unchanged).
- **D6 (g) — rider 3.** Owner label on every sandbox object, `Orphans` on both labels (an unlabelled leftover still reaped), `ErrNotOurs` for the ship's foreign-attempt 401, `Reconcile` leaves it; `client_test` pins the split; R11b's RED is astra's reproduction and it fired. Before the fix, my own R11b restart had survived only because b found no sandbox (a idle); with a's live sandbox the pre-rider-3 daemon exits 3 in one second (the group C log).
- **D7 — the setup guide is bundled.** `runner/README.md` is imported into the web app (`?raw`) and rendered by the existing Markdown component from the CI tab's first-run message and the Runners section, so "read the README" is one click and never a file to find.
- **D9 (a) — the linked landing is parked as the clay-push, with a transient reply target.** `pending-ci-land` (a transient beside `pending-clay`, reset on load) carries the candidate id, the expected tip and the pull; `clay-push`'s shape and its six other constructors are untouched. The completion event checks the tip and `%urgit-ci`'s eligibility again before it writes. A repository bound to a desk must be desk-shaped (`sys.kelvin`, a mark per file, the marks those build on): a `README.md`-only repository is refused by Clay with `missing /sys/kelvin`, and a desk holding a yml without the mime mark crashes every read of it in Clay — a scry crash `+mule` cannot catch — which nacked R14's second landing until the fixture carried `mar/mime.hoon` and used a fresh desk per run. Reading the desk back runs under `+mule` so a deterministic crash refuses with `unable to read linked Clay desk`.
- **D9 (b) — the single-line rule is gone.** `%set-credential` takes a multi-line value; the scrub set is every value whole plus every line of it of at least eight characters (both sides); the fe offers a multi-line field. Q9 (P2) now asserts acceptance.
- **D9 (a) — the in-progress guard was a copy of a guard that never fires.** The cold run's P19 (attempt 3) staged two candidates on one linked ref that passed two seconds apart: the second landing was parked over the first's clay-push, the first's report found no result and dropped both, the desk held the first's tree, master did not move, both candidates stayed `%passed` with no reason. The guard `?: ?|(=(^ pending-clay) =(^ pending-publish))` — copied from the receive path's six pre-existing sites — compares against a bare `^`, a wing, never a unit (measured in the dojo: false for `~`, `[~ 1]`, `[0 0]`). `b6199e9` tests `!=(~ …)` (loobean, so the subject stays unrefined for the `=^` below — `?=(^ …)` there nest-failed the build), and a parked landing whose report has nothing to report (a timer error, no result) is now refused `Clay update was not reported…`/`…ended without a result; re-run the candidate to land it` rather than dropped. R14 gains the race (one lands, the other is refused with its reason) and a mutant (group E); P19's harness seeds the desk-shaped tree by a plain push while unprotected, then protects — my S8 patch had put the toggles after the seed push, so the seed was staged as a candidate and raced the row's own. The six pre-existing sites were boxed (QUESTIONS §5) and rider 5 (`bceeb01`) ruled them in: `1b0fd07` changes the one token at each (nothing else at those sites) and adds R17 — a peer push from `~tug` completing while a plain push is parked is refused `another Clay operation is in progress` and the parked push completes — with a mutant (the peer site back to `=(^ …)`: the peer push reports ok and the parked push's held response never comes; git is bounded by `timeout 40` in the row for that case).
- **D9 (c) — rider 4 (`627644d`, astra §5) widened the fence to what S8 built, checked against its one hard line.** The tree as committed in `578515a`: the packet variant is `[%ci-approve request=@uv repository=@t candidate=@uv]` — no actor field (a packet that carries one fails the mold); `forge-kind` gains `%candidate`, nothing else in the mold; `desk/mar/git-peer.hoon` untouched; `++handle-peer` dispatches the variant to `++peer-ci-approve`, which admits the requester through `repository-writable` (the predicate `ci-can-write` reads, unchanged) on **`src.bowl`**, pokes the existing local `%approve-candidate` with `src.bowl` as the actor (that handler's own `ci-can-write` check unchanged), and reports the ack or nack to the requester as a `%forge-result` of kind `%candidate`; `%ci-action` stays `?> =(src.bowl our.bowl)` (`urgit-ci.hoon:124`). No follow-up code commit was needed. The named actor-substitution RED is R16's own mutant (group E, `r-mutants.sh` R16): the receiving arm acts for the owner whoever sent the packet — under it the request from `~tug`, no writer, is approved and the twin's actor is the owner; the tripwire `R16 RED: actor substitution`. Run after the cold run on its retained piers (the cold run was not touched for it): see the Negatives line.
- **D9 (c) — the cross-ship approve has no button.** The protocol (`%ci-approve`, forge kind `%candidate`), the owner-side check (`repository-writable`, what `ci-can-write` answers), the poke with `src.bowl` as the actor, the `%forge-result` reply on the ack or nack, and the requester's `POST /peer/ci-approve` are built and proven (R16); a page on the approver's ship would need remote CI browsing, which no route offers. The README says so.
- **P19 and Q9 changed with the rulings.** P19 (P1) asserted the desk-linked refusal and its mutant skipped it; under D9(a) it protects the linked repository and lands through the desk (desk-shaped seed), and the P19 mutant is removed from the P1 negatives (nothing left to skip). Q9 (P2) asserted the newline refusal; under D9(b) it asserts acceptance.
- **The serf SEGV.** `~sud`'s serf died at 16:13:48 (systemd-coredump, `urbit work … --loom 31`) under the fourth ERPit seed of the P3 rows (the GREEN R9 of group C; the R9 RED, the R9 row and their GREEN each seed ERPit's 1067 commits). The P2 boot line carried no `--loom`; the footer says `--loom 34`; `boot.sh` passes it now and `restart.sh` brought the ship back on its pier (`%gu` `%.y`, 18 repositories, replayed cleanly), the GREEN phase re-run (3/3). The cold run boots with `--loom 34`.
- **Harness — the P3 mutants.** Five files (`urgit-ci.hoon`, `ci-plan.hoon`, `ci-event.hoon`, `daemon.go`, `relay.go`) plus rider 3's two (`docker.go`, `client.go`); four groups so no row is turned red by another row's mutant: A = R4 R5 R3 (R3 expires a test daemon's record, not the pool's, so the pool survives its own mutant), B = R4b R5b, C = R9 R10, D = R11b (its mutant — the pre-rider-3 daemon — exits 3 at any start beside another runner's sandbox and killed R10's daemon under one group). R9 ends at once on a second refusal under its mutant. R11b's mutant is two edits, one row. The rebuild waits for `%urgit-ci` alone (the P3 mutants never touch `%urgit`; P2's wait for a reload of `%urgit` cost five minutes a phase).
- **Harness — a stopped test daemon is a ghost.** Its record stays selectable for `stale-after`; R1's first run handed R4's slow job to R1's stopped daemon. Rows retire their test daemons (`revoke-daemon` + `expire-token`) and `wait_live_only` before a push.
- **Harness — three self-inflicted re-runs.** Editing a row script while a phase ran it corrupted that run (bash reads incrementally): R4's first re-run, RED B's summary, and the S8 edits to the mutant files during the group C GREEN (its `status` line read MUTATED for a clean build; the ship and daemon had been built two minutes before the edits). Noted in memory; the cold run touches nothing.

**Verdict: the close-out's cold battery (T6) is an unqualified pass — one launch, no stop, no resumption, `cold.sh: every step ran (2026-09-20T18:20:02-05:00)`; R18 (T8/T8b) PASS on the retained pair, RED 1/1 then GREEN 1/1, with one product defect found and fixed at the source.**
