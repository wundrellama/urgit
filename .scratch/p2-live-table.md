# CI P2 live table — astra

Worktree `urgit-ci-p2-astra`, branch `ci/p2-astra`, re-frozen brief base
`4872953`, launch HEAD `a5cc44f`. Rider 1 resolved the original §1–§3.

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
| S0 | `0ca9ff8` | D7 added under Execution; answered boxes removed as Rider 1 directs; no implementation files changed |
| S1 | `9071dd0` | Q2–Q4 on `~lup`; Go tests; P0 storage vector; footer environment, RustFS, log metadata/routes, presign and upload |
| S2 | Boxed before implementation | `QUESTIONS-CI-P2.md` §4: extending the stage action requires a fourth `%urgit` touch for the push caller; §5: no authoritative writer peek exists for private repositories. No S2 row is claimed. |

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

## Shutdown

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
