# CI P2 live table — astra

Worktree `urgit-ci-p2-astra`, branch `ci/p2-astra`. The current brief body
is re-freeze 4, `46e9616` (SHA-256
`53939bc5e0a30aa65c370773ac1b41a0cdbe738d0c3164460a64daf2dacde0cf`),
with launch footer `946ae24`. Riders 1–5 resolve all eight boxes. S5 is
complete: the web surface and rootless child-socket fix have Q14–Q16 and
Q19 evidence below. The following S6 commit records the completed cold
regression and verified shutdown.

Historical S1–S5 transcripts named under `.scratch/tmp/` are preserved
under `.scratch/tmp/before-cold-1789645582952093920/` with the same
filenames. Use that archive for the earlier candidate IDs and verdicts
when a cold row reuses a transcript filename. Cold transcripts use the
`s6-` prefix at the current `.scratch/tmp/` root.

Ship `~lup`, HTTP 8352, pier `/var/home/michael/piers/urgit-ci-p2-lup`.
The pier did not exist before boot. `.scratch/ci-p0/boot.sh` split herdr
pane `w1B:p2` from `w1B:p1`, waited for the shell prompt, booted the footer
command, installed the desk, and read `%urgit-ci` state version **0**.
Boot transcript: `.scratch/boot-p2.log`.

Rootless Docker: `/run/user/1000/ci-p2-astra/docker.sock`, Docker 29.7.2,
data root `.scratch/tmp/docker-data`; security options contain `name=rootless`.
The runner uses the harness-built static `act` 0.2.89 and the P1 image
`catthehacker/ubuntu:act-latest`. Rider 5 adds only the configured child-socket flag to act; the sandbox
interface and projection are unchanged.

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
| S0 | `2a93744` | D7 added under Execution; answered boxes removed as Rider 1 directs; no implementation files changed |
| S1 | `7b6b65c` | Q2–Q4 on `~lup`; Go tests; P0 storage vector; footer environment, RustFS, log metadata/routes, presign and upload |
| S2 | `c2bc6b3` | Web merge gate, shared authoritative writer peek, trust policy, and approval. Q5a/Q5/Q6/Q8 RED → GREEN; Q7 actual owner approval and landing. Answered §4–§6 deleted as the riders request. |
| S3 | `6f4a286` | Credential store, job/environment grants, runner and ship scrub; Q9, Q10 RED → GREEN, Q11 RED → GREEN |
| S4 | `f78b1a8` | CI key and network certificate, signed assignment/grant bodies, config pin, Q12/Q13 RED → GREEN |
| S5 | This stage commit | CI web surface, Q14–Q16 and Q19, browser actions/pagination |
| S6 | Following stage commit | Cold regression and shutdown evidence recorded separately |

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
| Q9 | PASS | Actual newline credential poke refused **`credential values must be a single line`**. Trusted candidate `0vvc3vm.ab2f2.qcb35.b8q2m.t2d3u`, job attempt `0v2.kr0qq.piock.h13rd.ts01d.7ordf`: actual assignment contained `CI_TOKEN` and matching `PROD_TOKEN`, excluded `TEST_ONLY`; expiry within 900 seconds. `echo` and `set-output` are masked in local JSONL. An independently posted raw `set-output` was accepted **202** and stored as `***` by the ship. Candidate passed/landed, master `1e01fba…`; **5,621-byte** log, SHA-256 `d71e095f6583f658cb5de78bfc6015f2d7c084524818156df40e5df571e84a7e`. `.scratch/tmp/q9.log`. |
| Q10 | RED → GREEN | One-line shared grant eligibility sabotage: actual untrusted job assignment included a credential (tripwire `Q10 RED: untrusted job received a credential grant`); the daemon's independent guard refused to pass it to act. Restored controller: credentials stored for the real `~dys` PR, candidate `0v6.t446l.4aviv.ul2lj.5cjlm.4uk0c`, job `0v2.bf9s8.ucv65.t50e1.dq4l6.v5b37`, actual assignment **`grants=[]`**; passed, trust refusal prevented landing. `.scratch/tmp/q10-{red,green}.log`. |
| Q11 | RED → GREEN | One-line metadata sabotage returned the value in place of its name; both the read and complete-noun probe detected it (tripwire `Q11 RED: credential value appeared in a read`). Restored controller: **16 peek variants × 3 credential values**, plus attempt and repository JSON, contain none. Delete all three credentials → metadata `~`; actual `%assign` job rerun `0v1.ul8vh.ukfdn.mt5j6.1mgqc.qevgp` receives **`grants=[]`**, then passes. `.scratch/tmp/q11-red.log`, `.scratch/tmp/q11-green-final.log`; enumerated paths in ignored `p2-q11.json`. |
| Q12 | RED → GREEN | One-line Ed25519 verification bypass: daemon `0v6.65b0j.c0tgk.7mo2v.ncpaj.f958u`, explicitly pinned to wrong `0x1`, started plan `0v1.rmtpr.i3eap.nq14p.jt9bc.9467a` (tripwire `Q12 RED: wrong-pub daemon started signed work`). Restored verifier: the pending job `0v2.1poaq.mn0e8.f12ja.q9hma.i5d0j` is refused **`signature does not verify with pinned CI public key`**, before work directory, checkout or sandbox. GREEN repeated after final canonical-string encoding. Session key GET returns exactly `pub`, `cert`, `ship-life=1`. `.scratch/tmp/q12-red.log`, `.scratch/tmp/q12-green-final.log`. |
| Q13 | RED → GREEN | Held job `0v3.ah1qo.1oj4l.qvq01.v1ls1.40rdn` from candidate `0v1.9mvno.q5utt.6n2pa.vuav3.q04lr`, recipient `0v3.oisea.70c70.2but2.8colp.hs1h2`, until its original grant expired at Unix **1789617951** (04:05:51 UTC). One-line `grantCurrent` bypass: actual job ran with `--secret TOKEN=***`, 15 accepted events, passed at **04:06:01**, log uploaded (tripwire `Q13 RED: expired signed grant reached act`). Restored guard: replay the authentic redelivery while its outer signature is still current; **04:06:28**, **`credential grant expired`**, no work directory/sandbox/act. `.scratch/tmp/q13-hold-final.log`, `.scratch/tmp/q13-{red,green}.log`. |
| Q14–Q16 | PASS | S5 browser and session checks below |
| Q17 | Recorded in S6 | Full P1 regression evidence belongs to the following stage commit |
| Q18 | PASS | Rider 4 accepted retry below; retained by Rider 5 |
| Q19 | RED → GREEN | Wrong child mount on the unfixed runner and flag-drop mutant; restored rootless mount and in-job daemon identity below |

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


## S3: credentials and grant transport

The CI agent was nuked/revived for the larger `state-0`; S2's ids above
are historical. `%urgit` state and source are unchanged in S3. The
credential map and active grant snapshots are private state. Existing
candidate, assignment, attempt and daemon peeks retain no value fields.
The dedicated credential peek returns only names, scopes, environment
sets and creation times. Grant snapshots survive credential deletion
while an attempt runs, then are removed when it closes.

A grant is selected only for a trusted job, by repository and name. The
job-scoped case and matching/nonmatching environment scopes were tested
on the same actual Q9 assignment. Environment names come from the plan
submission as separate grant policy metadata, keyed by candidate and
validated workflow/job. No `job:ci` or `ci-plan` change, scheduler change,
projection change, poll-loop change, or sandbox interface change was
needed. The YAML reader accepts a static string or `environment.name`;
an expression cannot select a credential. Expiry is stored as `@da` and
serialized as a Unix second, 900 seconds after grant creation. S4 adds
and verifies cryptographic signatures; S3 does not claim signing.

Q9 also sends a raw secret directly to the running attempt's event route,
bypassing the runner's scrub. Its recorded `ship-scrub` output is `***`.
The attempt's 18 events are 17 daemon events plus that independent probe.
The daemon's JSONL and diagnostics contain neither released value. Go
tests additionally cover quotes, backslashes, escaped malformed
diagnostics, nested JSON, set-output, expiry, untrusted grants, and
static/nonstatic environment names. `go test ./...`, `go vet ./...`,
`go test -race ./internal/daemon ./internal/relay`, and the static runner
build passed.

The capture harness is a loopback HTTP pass-through with an ephemeral
port. It forwards the actual runner's requests to `~lup`, saving only
assignment replies in ignored mode-0600 files. It preserves Eyre's gzip
response while decoding a copy for the assertion. JSON snapshots use
atomic replacement so a reader cannot see a partial capture. The proxy
runs only inside its row driver and stops with it; each runner is stopped
by `/proc`-verified PID before the proxy is closed. The final S3 runner
PID was **3589219**. Both ships and the store remain up for S4.

Shakedown fixes and limits of the evidence:

- Khan reports a nack as a noun with numeric `%leaf` tapes. The newline
  test now decodes those tapes from the actual poke reply and checks the
  exact refusal; a missing asynchronous dojo message is not evidence.
- The first capture proxy tried to parse a gzip reply as UTF-8 and lost
  that delivery. Its original enrollment was restored from the retained
  private state file, and the same plan was redelivered and completed.
  The daemon-presence probe now requires a valid boolean before it can
  enroll; an invalid Hoon test cannot silently trigger re-enrollment.
- Q11 uses a fixture generator to scan every atom of the complete peek
  noun, returning only a boolean. It detected the RED credential value.
  Its hex needles use Hoon's four-digit grouping and its OID/file path
  segments use `scot %t`; invalid literal attempts did not count. The
  final GREEN log is a separate clean run. Q11 excludes the authorized
  assignment delivery, whose purpose is to release the named credential.
- Q11's rerun uses P1's existing operator `%assign`; the public rerun
  action belongs to S5. Captures exclude all prior attempt ids so a
  repeated row cannot pass using an earlier rerun's empty grant list.

## S4 signing observations

The in-place `state-0` addition was installed by nuking `%urgit-ci` only;
`%urgit` state and the retained peer pier were preserved. The first
`%rotate-ci-key` succeeded after Jael's private-key gift. Its public API
reports life **1** and CI public key
`0x80d4.5ca3.0fb7.3355.e8c3.5460.eab6.30c5.08cb.20f8.88af.553f.39b8.2575.2a09.eca4`.
A reload subscribes again and preserves this key.

The network certificate was checked twice: the live generator reads
Jael's **public** `/deed/<ship>/<life>` answer and verifies through
`+safe:as:crub`; Go independently verifies the same certificate using that
public key. No network ring, network signing seed, or CI private key was
exported. Public-only vectors are in
`runner/internal/signing/testdata/hoon.json`; the harness that regenerates
them is `.scratch/ci-p1/p2-signing-vectors.sh`, which extracts the current
`+signing-json` body rather than keeping a divergent Hoon copy. The live
log is `.scratch/tmp/s4-vectors.log`.

The cryptographic source path is zuse's `+nol:nu:crub` (1817–1823),
`+sigh:as:crub` (1720–1724), `+sign:ed` (1231–1235), `+luck:ed`
(1198–1210), and `+sign-raw:ed` (1236–1240), on the footer source tree.
`+sigh` derives the expanded signing pair from the ring's signing seed
and calls the specified `+sign-raw` path. Jael's subscription uses the
brief's exact `/jael/keys` task on initialization and reload; every
private-key gift re-certifies the CI public key.

`operation` binds the full assignment JSON body, with its four envelope
fields bound separately by the five-part tuple. A grant operation binds
its name and value. Canonical JSON nouns sort object pairs by UTF-8 bytes
and use explicit byte lengths for strings and number text. This binds
OID, trust, workflow, job, prerequisites, and grants, including attempted
trailing-NUL changes. Jam interning uses exact structural identities and
linear space. No poll loop, projection, or sandbox interface changed.

Positive control on the final encoding: candidate
`0vomj33.nlp4k.kb0e2.ta93p.a581m`, job
`0v3.9sgid.4l7li.04tnt.7ivnd.7b7so`, correct pin, valid assignment and
credential signatures, actual `act --secret TOKEN=***`, **15** accepted
events, **passed/landed** at `4b4435c0a1459337b880c922c89c78c550ee5f21`.
Its log has **4,389 bytes**, SHA-256
`8a0c263d9a93ab8365c54b5856370541c4f93526dba5a1d007d26a5bd67fa183`.
The wire capture contains the grant; local JSONL and diagnostics contain
no raw value. `.scratch/tmp/s4-positive.log`.

First enrollment pins the public key atomically in the runner TOML and
clears the consumed token. An existing pin is never overwritten. The
service example uses a runner-owned config directory so this atomic
write succeeds. A CI key rotation requires an explicit operator pin
update; networking-key rotation only re-certifies the existing CI key.
The README documents draining attempts before CI rotation. Old pending
grants are not silently re-signed or extended.

S4 unit checks cover Hoon/Go jam and Ed25519 equivalence, the live network
certificate, altered payload/recipient/expiry, a bad grant under a valid
assignment signature, trailing NULs, expiry before sandbox/act, and pin
persistence. `go test ./...`, `go vet ./...`, and focused race suites
passed. The actual ship compiled the controller; a first compile exposed
a Hoon refinement of `signing` across the enrollment scheduler's state
replacement. Reading the key into a local before refining it fixed the
compile without changing the schema or scheduler.

Q13 retained the production 900-second grant TTL. The in-process harness
proxy captured the real job response and returned 204 to withhold it;
the daemon was then stopped by its verified PID. After expiry, the ship
re-offered the same job with a fresh five-minute assignment envelope and
the original grant. The RED binary still verified both Ed25519 signatures;
only the shared expiry predicate was bypassed. GREEN replayed that
publicly verifiable envelope to the same recipient after restoring the
predicate. The RED job completed, so the GREEN replay cannot depend on a
still-running ship attempt. It is rejected locally for grant expiry.

An earlier held fixture was explicitly abandoned when the canonical
string encoding was tightened; `q13-hold-final.log` is the actual
15-minute fixture. The RED result's stored log is 4,389 bytes, SHA-256
`f5d7d6ffc244dd30d67540a959237f57d1ac655f4e4b9347f5b935c8cd591aa8`.
Both runners are stopped; the ships and RustFS remain up for S5. The final
runner binary is static and neither Q12 nor Q13 sabotage remains applied.

## S5 investigation — Rider 4

At this historical checkpoint S4's pre-rebase stage commit was `f6555fc`.
S5 was still in progress; this investigation did not claim Q14–Q16 GREEN.

| Observation | Result | Evidence |
|---|---|---|
| ERPit failed run, retained independently of acceptance | Failed; did not land | `a6d15eddefa898250bf5f656441a55a80273f751` plus README-only commit `8e0bbe3e2b6570896172537880719be3779b0e42`; candidate `0v2.j267u.99k0l.k7fin.smul4.8u9n8`; erasure attempt `0v3.o726k.08jf8.imcoa.90gc5.uacep`; verdict reason `job erasure in fixtures.yml failed`; master remained `a6d15ed…`. Six jobs passed; erasure failed; the unfinished suite was abandoned only after that failed verdict was captured. |
| Failed erasure's preserved log | Private store read and hash match | 67,561 bytes, SHA-256 `59937e7423b448aeadeb7c05d54866fd87b6c0343c8a3e685b204775a957eb80`. Plain session `curl -L` followed the query-presigned URL. S5 WIZATTR had hits at offset 325078804 (`fwd:1`); S8 SEAMVAL at 408810124 (`fwd:0`), each in `0i139/image.bin`, `0i158/image.bin`, and `chk/image.bin`. `.scratch/tmp/p2-erasure-box.jsonl`, `p2-web-candidate-box-response.txt`, `p2-web-erpit.log`. |
| Rider 4 pre-retry `/tmp` inspection | No shared `/tmp` found | Failed attempt ran on daemon `a`, using `unix:///run/user/1000/ci-p2-astra/docker.sock`. Throwaway image `sha256:32efe3fa4d225b18c624afc5826e1d3b348087af6906b64f3c3762fd6182eafa` has no declared volumes. `/tmp/erasewes` absent; `findmnt -T /tmp` = `/ overlay overlay`. Deliberately created `/tmp/erasewes/marker`, removed the container, recreated it with the SAME `/work` volume: `/tmp/erasewes` absent again. `.scratch/ci-p1/p2-sandbox-inspect.sh`, `.scratch/tmp/p2-sandbox-inspect.log`. |
| Actual retry erasure child container | `/tmp` clean before fixture | Candidate `0vdm40u.i3bb4.1cd5s.ivm60.4r5ml`, erasure attempt `0vseick.vfrio.3fong.495l5.nmvmm`; inspected the actual act child before `erasure-test.sh` began. `/tmp/erasewes` absent; no mount at/below `/tmp`; overlay filesystem. `.scratch/ci-p1/p2-sandbox-watch.py`, `.scratch/tmp/p2-sandbox-retry-mounts.log`. |

The exact `Prepare` mount lines in `runner/internal/sandbox/docker.go`
(81–83 at this stage) are:

```go
"-v", h.Volume + ":/work",
"-v", d.socketPath() + ":/var/run/docker.sock",
"-e", "DOCKER_HOST=unix:///var/run/docker.sock",
```

`h.Volume` is `spec.Network + "-work"`, with network `ci-<attempt>`.
`Destroy` removes every container attached to that network, its own
runner container, the work volume, and the network (docker.go 171–185).
There is no `/tmp` bind or tmpfs. `plan.Project` changes the workflow
name, selects one job and removes its `needs`/`if`; it adds no mounts.
The daemon's actual `act push` command contains neither `--bind` nor
`--container-options` (daemon.go 545–566).

Pinned act 0.2.89's `GetBindsAndMounts` is also relevant: it creates
attempt-named volumes for `/work/src` and `/var/run/act`, and a shared
`act-toolcache` volume at `/opt/hostedtoolcache`. None is mounted at
`/tmp`. The actual child inspection confirms those destinations.
Its socket bind is `/var/run/docker.sock:/var/run/docker.sock`.
The host-side socket is rootful (`SecurityOptions` lacks `rootless`,
`DockerRootDir=/var/lib/docker`); the child sees it owned by
`nobody:nogroup`, mode 660. A read-only `docker info` in the child fails
with permission denied. This distinct socket-boundary finding was
reported to the operator before changing runner behavior.

The failed log records the cadence creating epochs **139** and **158**
during this run, then chopping epoch **0**. Vere 4.6 names the epoch from
the last event number (`u3_disk_roll` passes `eve_d` to `_disk_epoc_roll`),
not a count of prior boots: [pinned disk.c](https://github.com/urbit/vere/blob/vere-v4.6/pkg/vere/disk.c#L1432).
Those numbers alone do not show a pier surviving a previous attempt. Both erasure scripts are
byte-identical between `d4f268e` and `a6d15ed` (`git diff` on the two
paths is empty). No ERPit source or workflow has been edited.

Rider 4's one-daemon, capacity-3 retry uses a second README-only fixture
commit `a866bea33e955befc03487250830ba9423518a0c`, candidate
`0vdm40u.i3bb4.1cd5s.ivm60.4r5ml`. The frontend arm-bound correction now
passes all 132 tests.

The retry's erasure attempt passed at **05:31:54 UTC**, with 181 accepted
events and no refused events. Its new epochs are **139** and **159**;
chop again removed epoch **0**. Every erased sentinel S1–S7 has zero hits;
S8's three hits are the fixture's expected retained control, and do not
constitute a test failure. The trusted log is **63,206 bytes**, SHA-256
`128272609527aa3557835b19470f4274a2d3a55efb7a76b0284f132568d0874d`.
The suite subsequently passed at **05:43:21 UTC**. The complete retry
took **1,109 seconds** from push to verdict and landed the exact candidate
OID `a866bea33e955befc03487250830ba9423518a0c`.

| Row | Result | Observed on the Rider 4 retry / S5 draft |
|---|---|---|
| Q18 | PASS (Rider 4's accepted retry) | Eight job attempts, all passed; all eight logs fetched from the private bucket through session-gated 302s, then plain query-presigned GETs without signing headers. Every key includes `/trusted/`; all sizes and SHA-256 hashes match. Master equals `a866bea33e955befc03487250830ba9423518a0c`; reason `landed`. `.scratch/tmp/q18-retry.log` lists all eight attempt ids, byte lengths and hashes; `.scratch/tmp/p2-web-retry.log` is the P15 driver transcript. ERPit remains at unchanged `a6d15ed` plus the fixture's README-only commits. |
| Q14 | PASS | Actual Chromium: candidate list has the retry, eight pips; candidate page has eight passed job rows, eight log links and `Landed`. A log fetch renders 25 lines; deep link survives reload and Back restores the log. `.scratch/tmp/q14.log`, `q14-jobs.png`, `q14-log.png`. The failed candidate separately renders its failure reason and 188-line erasure log (`s5-failed-log.png`). |
| Q15 | PASS | Separate fixture `q15-web-323049648073`: CI required off/on and both untrusted policies persist through page reload. Environment-scoped credential added for production/staging and deleted; random value absent from serialized DOM and policy metadata, password cleared on submit. A multiline paste causes an error with zero POSTs. `.scratch/tmp/q15.log`, `q15-settings.png`. |

At that checkpoint Q16 had not run RED→GREEN; S5 was an uncommitted draft, and Q17/S6's composed
battery had not run. The socket-boundary finding was boxed as §8;
no runner execution code was changed while awaiting its scope ruling.


## S5 completion — Rider 5

Rider 5 ratifies §8 as **CI-SANDBOX-1.1**, a P1 defect: the prior P1
batteries never inspected act's child mounts. The sole execution change
is `--container-daemon-socket=` plus the configured rootless endpoint in
`runJob`'s act arguments. The parent retains
`DOCKER_HOST=unix:///var/run/docker.sock`; no sandbox interface, projection,
scheduler, or polling change. The existing daemon orchestration test now
checks the configured socket in the exact invocation.

| Row | Verdict | Observed |
|---|---|---|
| Q19 original | RED | Unfixed binary, candidate `0vthn2e.1bdpf.7mke7.o2p0s.h0951`, job `0v5.7rhib.vp60l.e8hnf.kl0kr.tja0m`: `Binds=["/var/run/docker.sock:/var/run/docker.sock"]`; child `docker info` exits 1, permission denied. `.scratch/tmp/q19-unfixed-red.log`. |
| Q19 fixed | GREEN | Candidate `0v3.j2fb7.kqlgt.jum9m.h5u87.sscis`, job `0v6.bda0l.0kojk.srvnc.gp8tj.kd7hd`: bind source `/run/user/1000/ci-p2-astra/docker.sock`; in-job `docker info` exits 0 with `["name=seccomp,profile=builtin","name=rootless","name=cgroupns"]`; job passed and candidate landed. `.scratch/tmp/q19-green.log`. |
| Q19 mutant | RED → GREEN | One-line sabotage drops the flag. The same live inspection again observes the host rootful bind and prints `Q19 RED: act child binds the host rootful socket`. Restoring the flag gives candidate `0v4.cu799.2nnrn.9hbt9.l8g5q.gttio`, job `0v6.3bvpj.ku60j.rg1mc.vr3h0.nei03`, the correct rootless source and `name=rootless`. `.scratch/tmp/q19-{mutant-red,restored-green}.log`; reusable `p2-socket-regression.sh` is included in P1. |
| Q16 | RED → GREEN | One-line `web-authorized` sabotage: candidate list/detail, policy, key, and action return 200 anonymously; log returns 302. Restored: all six web routes return 401 anonymously; session reads/actions return 200 and log returns 302. The existing daemon attempt reader independently returns anonymous 401/session 200 in both phases. Tripwire `Q16 RED: anonymous CI reads and action accepted`; `.scratch/tmp/q16-{red,green}.log`. |
| Browser actions/pagination | PASS | Actual `~dys` peer PR in `web-actions-514882346991`: untrusted rerun keeps head/base/trust and has no attempts. 54 candidates paginate 50/4, no duplicates; malformed cursor 400, absent cursor 404; Older/Newest work in Chromium. Approve creates trusted candidate `0v4.d9pa5.rleeo.4u7gp.t22i4.uechg`, old skipped with approval reason, real job passes and lands. `.scratch/tmp/web-actions.log`. |

The browser exercise exposed a transient candidate-to-list render using the
previous response shape. The list now waits for a list response before
rendering; the same navigation passes in the complete browser exercise.
Q14 and Q15 were repeated after this fix (`q14-final.log`, `q15-final.log`).
Frontend tests: **132/132**; production build succeeds. Go unit/race/vet
checks pass. §7 and §8 are removed from the questions file. The S5 commit
contains this implementation and its Q14–Q16/Q19 evidence. S6's later cold
regression is recorded in the following stage commit.

The fixture-only first Q19 harness push correctly returned Git's staging
refusal, which the new driver initially treated as a shell error. Its unused
job was abandoned during runner shutdown. The driver now asserts that exact
staging response; this shakedown is not counted as a RED or GREEN row.
