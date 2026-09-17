# CI P2 live table — astra

Worktree `urgit-ci-p2-astra`, branch `ci/p2-astra`. The current brief body
is re-freeze 3, `f7391cb` (SHA-256 prefix `947595909726`), with launch
footer `c703034`. Riders 1–3 resolve the original six boxes. Earlier
shutdowns and their evidence below are historical; the current pass
resumes the retained piers and continues from S2.

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
| S0 | `1acf739` | D7 added under Execution; answered boxes removed as Rider 1 directs; no implementation files changed |
| S1 | `854237b` | Q2–Q4 on `~lup`; Go tests; P0 storage vector; footer environment, RustFS, log metadata/routes, presign and upload |
| S2 | This stage commit | Web merge gate, shared authoritative writer peek, trust policy, and approval. Q5a/Q5/Q6/Q8 RED → GREEN; Q7 actual owner approval and landing. Answered §4–§6 deleted as the riders request. |

## Observations

| Row | Verdict | Observed |
|---|---|---|
| Q1 | PASS | S0 is the first stage commit, preceding all P2 implementation |
| Q2 | PASS | ERPit `suite.yml/plan` attempt `0v3.9hh37.4pqig.jh097.b7e6s.uhs19`: log handle under `/trusted/`, **6,161 bytes**, SHA-256 `2570b43944fee7496e37d1448da55c846af470bf037ef4f9071c07796911e654`. Session GET → **302**, curl follows without SigV4 headers → **200**, **22 JSONL lines**, hash and size match. The exact same 60-second URL after expiry → **403**, `<Code>AccessDenied</Code><Message>Request has expired</Message>`. An identical result retry → **200**. Logs: `.scratch/tmp/q2.log`, `.scratch/tmp/q2-result-retry.log`. |
| Q3 | RED → GREEN | One-line `sign-get` trust-guard sabotage returned a signed `/untrusted/log.jsonl` URL for that trusted attempt (tripwire `cross-trust read refused: FAIL`). Restored build returns **~**. Actual stored key contains `/trusted/`. Logs: `.scratch/tmp/q3-red.log`, `.scratch/tmp/q3-green.log`. |
| Q4 | RED → GREEN | Running attempt `0v5.719le.1c4o8.2bput.rg2cf.8if7s`: one-line upload-name guard sabotage → 200 for `../x` (tripwire `upload ../x -> 200`); source restored and reloaded → 400. Logs: `.scratch/tmp/q4-red.log`, `.scratch/tmp/q4-green.log`. |
| Q2-missing (D1) | RED → GREEN | Deleted the completed erasure log from the bucket. One-line stale-handle sabotage: GET **404** but log metadata retained (tripwire `missing object cleared: FAIL`); restored code: GET **404**, `log=null`, attempt still **passed**. Logs: `.scratch/tmp/q2-missing-red.log`, `.scratch/tmp/q2-missing-green.log`. |
| Q5a | RED → GREEN | One-line web-gate bypass: merge → **200**, master moved and pull merged before checks (tripwire `Q5a RED: protected master moved before checks`). Restored gate: merge → **202**, master held at `c569d30…`, candidate `0v6u31p.9ifng.3b1f5.n1qct.1dera` trusted; real runner passed, master moved to `5434b9b…`, pull #1 then merged. Unprotected `free` merge → **200** and direct write. `.scratch/tmp/q5a-{red,green}.log`. |
| Q5 | RED → GREEN | Actual `~dys` peer discovery/fork/push/pull, each response id matched in its own completion record. Constant-trusted sabotage offered work → **200** (tripwire `Q5 RED: non-writer PR offered trusted work`). Restored build: repo `q5-green-514984057371`, actor `~dys`, candidate `0v1.d736e.sfdov.fqpt5.jjed0.cpofu`, untrusted/pending, plan `~`, attempts `~`, poll **204**, master unchanged. `.scratch/tmp/q5-{red,green}.log`. |
| Q6 | RED → GREEN | One-line eligibility sabotage let untrusted candidate `0vllmtj.c1b2q.9vil3.22fbl.ask5i` pass and land (tripwire `Q6 RED: untrusted candidate landed`). Restored build, same Q5 GREEN PR under restricted policy: real plan and job attempts untrusted; captured plan assignment has `trust=untrusted`, `grants=[]`; job uses `/work/cache/untrusted`. Candidate **passed**, refusal **`candidate trust is untrusted; cannot land`**, master remains `e4c9f11…`, pull #1 open. `.scratch/tmp/q6-{red,green}.log`; actual wire captures in ignored `p2-q6-*-offer.json`. |
| Q7 | PASS | Actual owner `%approve-candidate` poke: old `0v1.d736e.sfdov.fqpt5.jjed0.cpofu` → **skipped**, reason **`superseded by approval`**; new `0v5.a96eh.tbtht.fukh5.6rapq.ofk7n` trusted with exactly the same head/base, fresh plan and job, **passed/landed**, master `c40a6d5…`, PR #1 merged. `.scratch/tmp/q7.log`. |
| Q8 | RED → GREEN | One-line writer-refusal bypass in the action handler: synthetic `src.bowl=~dys` accepted, also accepted while `%urgit` was actually suspended (tripwire `Q8 RED: non-writer approval accepted by the action handler`). Restored handler refuses **`actor cannot write q5-green-514984057371`**; with `%urgit` `%gu=%.n`, refuses **`ci: %urgit writer read is unavailable`**. Q7 is the real owner-positive poke. `.scratch/tmp/q8-{red,unavailable-red,green,unavailable}.log`. |
| Q9–Q18 | Pending | Later stages remain |

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

## Historical S2 box pass and shutdown

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


## S2 current pass: scope and harness

Both retained piers resumed without `-p`: `~lup` in pane `w1B:p5`,
`~dys` in `w1B:p6`. `%urgit-ci` was nuked/revived for the in-place
`state-0` change; `%urgit` retained its existing schema and state. Q5's
setup reset CI once more to retire the preliminary Q5a daemon; Q5a's
results above are historical. Both ships compiled the S2 source.

The five authorized `%urgit` touches at this stage are:

1. `handle-api`, merge branch **7349–7396**: guarded protection read,
   stage with the pull's source ship and number, answer 202. The pull
   stays open; no `native-pull` slot or persisted `%urgit` schema changed.
2. `land-candidate`, **8006–8053**: read the candidate by id, validate
   its fields and eligibility, report the trust refusal, and mark its
   pull merged after the ref update.
3. `handle-api`, authenticated repository GET **6092–6115**:
   `ciUntrustedPolicy`. The shared pure JSON renderer lacks the bowl
   needed by the guarded read, so the field is added in this handler.
4. `handle-receive-pack`, **8698**: the existing push stage constructor
   supplies `-.actor +.actor ~`, including the new empty pull slot.
5. `on-peek`, **9212–9217**: `/ci-can-write/<repo>/<actor>`, plus the
   unchanged helper lift. `group-peek` **10300–10317**, `group-seat`
   **10330–10357**, `repository-group-capability` **10359–10364**, and
   `repository-writable` **10366–10374** have byte-identical arm bodies
   to `c703034`. The outer helper door and four aliases are at **2230–2239**
   and **10287–10375**. A direct addition of arms to Gall's ten-arm
   core fails its `agent:gall` cast; the enclosing door follows the
   existing `%urgit-ci` pattern and shares the same bowl. The body hashes
   are `4d342068…`, `f9b2eb02…`, `d5be6e04…`, and `a7e552a8…` respectively.

Additional live writer reads: the Q5 repo with `~lup` → `%.y`, with
`~dys` → `%.n`, an unknown repo with `~lup` → `%.n`.

Harness findings resolved within S2:

- Peer fixtures now set their advertised default branch to `master`.
  A fresh repo advertises `main`; seeding only `master` left the first
  fork transfer with an incomplete default graph.
- P1 ignores capacity zero. The Q5 capture-only enrollment must age out
  for five minutes before Q6 starts its runner; a zero-capacity poll did
  not retire it. The first Q6 setup returned 204 from the wrong daemon
  and did not count as RED. The corrected RED and GREEN both ran real
  plan and job attempts.
- Capturing a real assignment before starting the runner consumes its
  first delivery. Both Q6 phases waited for P1's two-minute redelivery;
  the poll loop and sandbox interface remain unchanged.
- Reload checks now require Gall's reload/boot/bump message after an
  invocation marker. Repeating an identical source uses single-agent
  `|rein` off/on to load its saved state. A comment-only source change
  does not force a Gall reload. No CI state is lost by this retry path.

`go test ./...` and the static runner build passed after adding the
trust-specific cache path. The existing job lifecycle test checks that
path; all other execution arguments and projection behavior are retained.


Q8's P2 limitation is explicit: only the owner can reach the production
approval poke. A remote writer has neither a local session nor a remote
approve route; that `%git-peer` route belongs to P3. The probe extracts
the current controller helper source verbatim, compiles it with a real
snapshot and a synthetic bowl, calls `handle-action` under `mule`, and
returns only acceptance or the refusal's leaf message. It neither saves
state nor dispatches cards. This tests the real predicate and action
handler without weakening the production poke entry. Both RED probes
ran before Q7. The GREEN probes were repeated after Q7 because the first
reader's 60-line tail dropped long error traces; the writer gate runs
before the superseded-candidate check and still supplies the exact
required refusal. `lib.sh` deliberately disables errexit; the Q8 driver
now re-enables it after sourcing the library. No empty transcript is
counted as a result. Q7's first reader also needed double spaces for its
tall Hoon `=/`; its actual approval had already succeeded, and the fixed
reader verified that same new candidate without a second approval.

The generated probe is a fixture-only generator in the mounted desk,
removed by the next normal desk build. It is not a product endpoint.
The four lifted access-arm bodies were compared byte-for-byte again
against `c703034` after Q8; all were equal. `git diff --check` passes.
