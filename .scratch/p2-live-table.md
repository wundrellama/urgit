# CI P2 live table — astra

Worktree `urgit-ci-p2-astra`, branch `ci/p2-astra`, re-frozen brief base
`4872953`, launch HEAD `a5cc44f`. Rider 1 resolved the original §1–§3.
The operator subsequently rebased the chair onto re-freeze 2 (`682c8cf`),
with launch footer `f589b4e`; the stage hashes below reflect that rebase.
Rider 2 resolved §4/§5. The resumed S2 pass stopped at the new §6 scope box.

Ship `~lup`, HTTP 8352, pier `/var/home/michael/piers/urgit-ci-p2-lup`.
The pier did not exist before boot. `.scratch/ci-p0/boot.sh` split herdr
pane `w1B:p2` from `w1B:p1`, waited for the shell prompt, booted the footer
command, installed the desk, and read `%urgit-ci` state version **0**.
Boot transcript: `.scratch/boot-p2.log`.

Rootless Docker: `/run/user/1000/ci-p2-astra/docker.sock`, Docker 29.7.2,
data root `.scratch/tmp/docker-data`; security options contain `name=rootless`.
The runner uses the harness-built static `act` 0.2.89 and the P1 image
`catthehacker/ubuntu:act-latest`. No sandbox or projection code was changed.

RustFS: port 8362, private bucket `ci-bucket`, region `us-east-1`, data
`.scratch/tmp/store-data`, image
`rustfs/rustfs:1.0.0@sha256:8cc9801755448b71a786705ce76692c77e14936cccd87cf2fc31842e58f4d1ff`.
`store.sh` creates separate root and CI credentials. The ship receives the
CI key, whose object permissions are confined to `ci-bucket/ci/*`.
Credentials are generated into ignored files with mode 0600, not committed.
The rootless container uses UID 0 in its user namespace so its data remain
owned by the invoking host user. The server's console port is not published.

## Stage commits

| Stage | Commit | Gate |
|---|---|---|
| S0 | `a0f38e7` | D7 added under Execution; answered boxes removed as Rider 1 directs; no implementation files changed |
| S1 | `25aa23f` | Q2–Q4 on `~lup`; Go tests; P0 storage vector; footer environment, RustFS, log metadata/routes, presign and upload |
| S2 | Boxed; draft restored out of product source | Rider 2 resolves §4/§5. `QUESTIONS-CI-P2.md` §6 records the new peek's `-find.repository-writable`: the predicate and its group helpers are local to `on-poke`, outside `on-peek`. No S2 row is claimed. |

## Observations

| Row | Verdict | Observed |
|---|---|---|
| Q1 | PASS | S0 is the first stage commit, preceding all P2 implementation |
| Q2 | PASS | ERPit `suite.yml/plan` attempt `0v3.9hh37.4pqig.jh097.b7e6s.uhs19`: log handle under `/trusted/`, **6,161 bytes**, SHA-256 `2570b43944fee7496e37d1448da55c846af470bf037ef4f9071c07796911e654`. Session GET → **302**, curl follows without SigV4 headers → **200**, **22 JSONL lines**, hash and size match. The exact same 60-second URL after expiry → **403**, `<Code>AccessDenied</Code><Message>Request has expired</Message>`. An identical result retry → **200**. Logs: `.scratch/tmp/q2.log`, `.scratch/tmp/q2-result-retry.log`. |
| Q3 | RED → GREEN | One-line `sign-get` trust-guard sabotage returned a signed `/untrusted/log.jsonl` URL for that trusted attempt (tripwire `cross-trust read refused: FAIL`). Restored build returns **~**. Actual stored key contains `/trusted/`. Logs: `.scratch/tmp/q3-red.log`, `.scratch/tmp/q3-green.log`. |
| Q4 | RED → GREEN | Running attempt `0v5.719le.1c4o8.2bput.rg2cf.8if7s`: one-line upload-name guard sabotage → 200 for `../x` (tripwire `upload ../x -> 200`); source restored and reloaded → 400. Logs: `.scratch/tmp/q4-red.log`, `.scratch/tmp/q4-green.log`. |
| Q2-missing (D1) | RED → GREEN | Deleted the completed erasure log from the bucket. One-line stale-handle sabotage: GET **404** but log metadata retained (tripwire `missing object cleared: FAIL`); restored code: GET **404**, `log=null`, attempt still **passed**. Logs: `.scratch/tmp/q2-missing-red.log`, `.scratch/tmp/q2-missing-green.log`. |
| Q5a–Q18 | Pending | No later-stage rows claimed |

The fixture's independent curl-signed PUT/GET succeeded and its anonymous
object GET returned **403**, before any product signing test. All seven
`%storage-action` pokes succeeded. Source: `.scratch/tmp/store-start.log`.
The administration and container setup follow the official
[RustFS IAM API](https://docs.rustfs.com/en/security-compliance/iam/policies)
and [container documentation](https://docs.rustfs.com/en/installation/container).
The additive presigner follows the
[AWS query-signature format](https://docs.aws.amazon.com/AmazonS3/latest/developerguide/sigv4-query-string-auth.html).

`go test ./...` passed after the upload change. The daemon tests verify the
uploaded bytes and reported hash/size, that the bearer does not reach the
store, and that an upload outage preserves the job result with no log handle.
The unchanged `+urgit!ci-storage-vector` prints `%.y` on `~lup`.

## Deviations and shakedown findings

- **Process lifetime:** the P1 rootless launch returned success, but its daemon
  disappeared after the launcher session exited. Detached launch now uses
  `setsid` and closes stdin; the runner launcher does the same. The harness
  runner's variable `home` was renamed `runner_dir` to avoid a shell-special
  name. No product poll or sandbox interface changed.
- **Concurrent dojo readers:** the ERPit driver and the P2 row share a pane.
  `dojo.sh` now serializes each send/read pair with a lock in `.scratch/tmp`.
- **Wire decimal sizes:** the first real plan logs were 6,161 and 6,835 bytes.
  `number-at`'s inherited `slaw %ud` refused ungrouped JSON numbers above 999,
  so the upload route returned 400 before signing. It now uses `rush ... dem`;
  the HEAD `Content-Length` parser uses the same wire-decimal parser.
  The first attempts retained their success verdict and `log=null`, as D1
  requires for an unavailable upload. No store signature rejection occurred.
- **ERPit shakedown:** `.scratch/ci-p1/p15.sh` seeded a read-only clone of
  ERPit revision `a6d15eddefa898250bf5f656441a55a80273f751` (1,067 commits)
  into `erpit-p15`, then staged `a1e4914e0b737c49ce08e8c849178a6d7c708a71`
  as candidate `0v5.tcnmc.6recd.dteko.tgeni.u3mf2`. The ship planned all eight
  jobs. After the decimal fix an operator `%assign` reruns `suite.yml/plan`
  for Q2. This preliminary run is not Q17 or Q18 and does not claim eight
  attempts without reruns. The P15 polling script was stopped by verified
  `/proc` arguments; the runner and its jobs continued.
- **Log URL lifetime:** the viewer route requests a 60-second URL; the
  library accepts 1–900 seconds. Existing `sign-get` and `sign-put` are
  unchanged, except that the agent's log scry requires a recorded log handle.
- **S0 bookkeeping:** Rider 1 explicitly requested deleting the answered
  questions in S0; that file change accompanies the spec paragraph.

## S1 shutdown

Completed with `.scratch/ci-p1/p2-shutdown.sh`; transcript:
`.scratch/tmp/p2-shutdown.log`. Runner PID **2887520** was checked through
`/proc`, stopped with TERM, posted abandon → **200**, and destroyed its
remaining sandbox. RustFS `urgit-ci-store-lup` reports `running=false`;
the harness Docker daemon has no running containers.

Ship PIDs **2866364** and **2868976** were verified against the footer
binary and pier before sending Ctrl+D to dojo pane `w1B:p2`. Both exited;
the shell prompt returned and the pane was closed. A final `/proc` scan
finds no matching ship or runner. The **484 MB** pier remains at
`/var/home/michael/piers/urgit-ci-p2-lup`; store data are retained. The
chair's rootless Docker daemon remains available, with no containers running.

## S2 resumed pass and shutdown

Rider 2 was read at `f589b4e`. The five-touch draft was copied to `~lup`
with `zig build -Ddesk=<pier>/urgit`; the actual Clay compilation refused
the new `on-peek` branch with `-find.repository-writable` at draft line
9298. The predicate is in the local core opened by `on-poke`'s `|^`, not
the enclosing agent door. The dependency chain that needs shared scope is
`repository-writable` → `repository-group-capability` → `group-seat` →
`group-peek`. The box recommends moving those four arms without changing
their bodies. None was moved in this pass.

The repository policy field belongs in the authenticated repository GET
handler (within touch 3), where the bowl is available. An initial attempt
to read it from the pure shared JSON renderer failed with `-find.our.bowl`;
the draft was corrected before the repeated writer-scope failure.

The incomplete implementation and harness edits are saved in the ignored
`.scratch/tmp/s2-rider2-draft.patch`, SHA-256
`7904083df4316e7eb4d309ff2c8cdd90bdfeefc3fc9370c9c2399556c5ec6808`.
The new resume script is saved as `.scratch/tmp/s2-rider2-boot.sh`.
Product source and tracked harness files were restored to `f589b4e` before
the box commit. The draft has not passed compilation or any S2 row.

`~lup` resumed its retained pier in pane `w1B:p3`, with HTTP 8352 and no
`-p`. The CI agent was nuked for the in-place mold change; after the failed
compile, the S1 desk was restored and `%urgit-ci` revived with empty CI
state. Its S1 evidence above is historical, not a claim that those attempt
ids remain in current state. `%urgit` was not nuked.

`~dys`'s pier did not exist before this pass. The P0 boot harness created it
in pane `w1B:p4` with `-F dys`, the footer pill, HTTP 8354, and no `-p`.
After restoring the S1 sources, the desk installed all four `%urgit` agents.
No peer discovery, fork, push, or PR test ran before the box. Boot transcript:
`.scratch/tmp/s2-boot-dys.log`; restoration transcripts:
`.scratch/tmp/s2-restore-{lup-revive,dys-install}.log`.

The resumed shutdown transcript is `.scratch/tmp/s2-shutdown.log`.
The runner was already stopped. RustFS reports stopped. `/proc` verification
identified `~dys` PIDs **3277958 / 3282581** and `~lup` PIDs
**3269495 / 3269497** by binary and pier. Ctrl+D stopped both ships; each
shell prompt returned before its pane was closed. Both piers remain:
`urgit-ci-p2-dys` **220 MB**, `urgit-ci-p2-lup` **505 MB**. The rootless
Docker daemon remains available with no running containers.
