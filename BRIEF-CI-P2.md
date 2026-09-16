# BRIEF — CI P2: trust, storage, and the web surface

Native CI phase 2 on `master` at `e3a0445` (P0 contracts + P1 scheduler/daemon merged and
battery-verified). This phase delivers the spec's steps 5 and 6 — `specs/native-ci.md`
§Trust and credentials, §Storage, and the web surface — plus CI-DELIVERY-1 as spec text.
Nothing about execution changes: the `docker-rootless` sandbox, the projection, the
scheduler, and the landing path stay as P1 left them. **The `microvm` backend and
linked-desk landing are P3; do not start them.**

Read in this order before anything else: this brief; `specs/native-ci.md` §Trust, §Storage,
§Protected refs, §Delivery order; `REFERENCE-CI-RULINGS.md` (planted, untracked — the
ruling map; CI-P2-SCOPE-A, CI-DELIVERY-1, CI-TRUST-P1, CI-SANDBOX-1-B bind you);
`desk/sur/ci.hoon`; `desk/app/urgit-ci.hoon` (1356 lines; `handle-http` at 955,
`handle-event` at 1154); `desk/lib/ci-storage.hoon`; `runner/README.md`;
`.scratch/p1-live-table.md` (the P1 record — its harness is yours to extend, not replace).

## 1. What is true at `e3a0445` (verified; do not re-derive)

- `attempt.events` is a **count**. `handle-event` (urgit-ci.hoon:1154) validates each
  relayed `act --json` line, folds `set-output` into `outputs` and remembers `job-result`,
  increments the count, and **drops the line's text**. The only copy of an attempt's log
  is the daemon's local `<WorkDir>/<attempt>.act.jsonl` (daemon.go:524). An operator
  cannot read a log without shell access to the runner box.
- `sign-put:ci-storage` (ci-storage.hoon:28) builds a header-authorized SigV4 PUT for
  `ci/<repo>/<run>/<attempt>/<trust>/<name>` and takes a `payload-hash`; **nothing calls
  it.** `sign-get` is exposed as the `%x /sign-get/<attempt>/<trust>/<name>` scry (:135)
  and the daemon does not use it either. The `%storage` settings read
  (`read-settings:ci-storage`) already gates CI enablement (`storage-refusal`, :28, :336).
- Every attempt is created `%trusted` (:499, :503, :529) — CI-TRUST-P1's ratified P1
  placeholder: every staged candidate is trusted because `can-write` (git-access.hoon:74)
  already admitted the pusher. There is no approval, no credential store, no signing key.
- `attempt-status` includes `%skipped`; `candidate-status` is
  `?(%passed %failed %pending %unknown)`. `via` is `?(%session %token)`.
- `%urgit` peeks `/ci-protected/<repo>/<ref>` at push (urgit.hoon:7978) and pokes
  `%stage-candidate` (:8727) — **from `handle-receive-pack` only.** The web pull-request
  merge (`handle-api`, the `[%repository @ %pulls @ %merge ~]` route at :7417, ref write
  at :7472: `refs (~(put by refs.u.found) target-ref.pull merge-oid)`) writes the ref
  **directly**, consults nothing, stages nothing. Today the Merge button bypasses a
  CI-protected branch. P1 never exercised it (its record has no web-merge row). The pull
  mold (`native-pull`, sur/git.hoon:111) carries `source-ship` — that is the actor D3
  needs. `repository-json-up-to` (:591) and `public-repository-json-up-to` (:714) both
  hide `refs/ci/*`. `land-candidate` is :8051.
- HTTP: `%urgit-ci` binds `/apps/urgit/api/ci` (:218). Routes today (handle-http):
  `POST daemon/enroll`, `GET daemon/<id>/assignment`, `GET attempt/<id>`,
  `POST attempt/<id>/{event,result,plan,abandon}`. A logged-in ship session passes
  `daemon-authorized` unconditionally (:844 comment) — the read routes below reuse that.
- Frontend: `fe/` is React + Vite, tests `node --test src/*.test.js` (17 files).
  `RepositoryView.jsx:42` has `validTabs`; the `settings` tab holds "Protected branches"
  (:1335, reading `repo.protectedRefs`). `api.js` fetches from `BASE='/apps/urgit/api'`
  with `credentials: 'same-origin'`.
- `ci-event.max-events` is 50 000 per attempt; ERPit's eight jobs produce ~15–1 500
  lines each.

## 2. Derivations (each cites its section; each is a fence)

**D1 — Logs and artifacts are objects, never Gall state** (§Storage: "Only handles, sizes,
hashes, and completion state enter Gall state"). The ship keeps `attempt.events` as a count
and adds `log=(unit object-ref)` where `object-ref` = `[key=@t size=@ud sha256=@t]`. The
daemon writes the full `.act.jsonl` locally as today, and **after** `act` exits — before
`POST result` — it asks the ship for a PUT and uploads the finished file. The `result` body
carries the object's `size` and `sha256`; the ship signs a `sign-get` for that key only
after a `result` names it. A `result` that names a log the store does not hold (HEAD fails
at first read) leaves `log=~` and the attempt keeps its verdict — a missing log is
`unknown`-worthy for the *log*, never for the *attempt* (§Storage: "A store outage yields
`unknown` for the affected attempt. It never yields success" applies to an attempt whose
**result** could not be recorded, which the daemon retries; not to a log upload).

**D2 — Upload is a ship-signed request the daemon asks for, keyed by the attempt's trust**
(§Storage: "`%urgit-ci` signs a short-lived upload URL for each attempt"; "refuses to sign
a read across trust classes"). New route `POST attempt/<id>/upload` (daemon-authorized,
body `{name, contentType, sha256, size}`) returns the `signed-request` from `sign-put`. The
key is `object-key` with the attempt's own trust — the daemon cannot choose the class.
`name` is restricted to `log.jsonl`, `summary.md`, and `artifact/<safe-name>`; anything
else 400. Reads: `GET attempt/<id>/log` (session-authorized) answers a 302 to a
**query-presigned** GET URL, or 404 while `log=~`. **Ruled (astra §2):** P0's `sign-get`
produces a header-authorized request (`git-storage.hoon:139` builds a bare URL; the
signature is in the `authorization` header at :162–170), which a browser following a
`Location` cannot present — so a 302 to that URL 403s on a private bucket. P2 adds a
second signing mode to `ci-storage`: `presign-get` producing the standard SigV4
query-string form (`X-Amz-Algorithm`, `X-Amz-Credential`, `X-Amz-Date`, `X-Amz-Expires`,
`X-Amz-SignedHeaders=host`, `X-Amz-Signature`; the canonical request uses
`UNSIGNED-PAYLOAD`), expiry ≤ 15 min, trust-class-checked exactly as `sign-get` is.
The PUT stays header-authorized (the daemon can send headers). §Storage's "short-lived
download URL for each authorized viewer" is this URL. The existing `sign-get` /
`sign-put` arms are not modified; `presign-get` is additive and tested against the
fixture store (Q2 follows the 302 with a plain `curl -L` and gets the jsonl).

**D3 — Trust is decided at staging from the actor, and it is a repository policy**
(§Trust: "A revision from an untrusted source needs approval before it runs. Approval is
per revision. A repository may opt in to automatic restricted checks for untrusted
revisions"). Replace CI-TRUST-P1's constant with: a candidate whose `actor` passes
`can-write` for the repo is `%trusted`; otherwise `%untrusted`. An untrusted candidate is
created `%pending` with `plan=~` and **no attempts** until one of: (a) a repository
policy `untrusted=%restricted` is set, in which case it is planned and run with
`trust=%untrusted` on every attempt — no credentials released, cache namespace
`untrusted`, and it can never land; or (b) a writer approves it (`%approve-candidate
id`), which re-stages the SAME head/base as `%trusted` — a new candidate id, the old one
`%skipped` with `verdict-reason='superseded by approval'`. A `%trusted` candidate never
needs approval.

**D3a — The web pull-request merge goes through the gate (a P1 gap, closed here).**
§Protected refs: "A direct push, web edit, or import to a CI-protected branch is never
applied… The candidate lands through the same checks as a pull request." At `e3a0445`
the `pulls/<n>/merge` route (urgit.hoon:7417, `handle-api`) writes `target-ref` directly
(:7472). P2 makes it the second caller of the P1 gate: when `target-ref` is CI-protected,
the route does **not** write; it stages `%stage-candidate` with `head=head.pull`,
`base=<current target tip>`, `actor=source-ship.pull`, `via=%session`, marks the pull
`%pending-ci` (or keeps `%open` with a `candidate=(unit candidate-id)` slot — pick the
one that does not touch `native-pull`'s shape and say which), and answers 202 with the
candidate id. Landing (`land-candidate`, :8051) moves the ref exactly as for a push
candidate and, when the candidate carries a pull number, flips the pull to `%merged`. An
unprotected `target-ref` keeps today's direct write. **This is where `%untrusted`
candidates come from**: `source-ship` on a fork PR is not in `can-write`. `%urgit`'s
push path already rejects non-writers, so no other path produces one in P2.

**D4 — Credentials are stored on the ship and released per attempt, per name, for a
bounded time** (§Trust: "It releases a credential only to an authorized attempt, scoped to
that attempt, for a bounded time. An external vault is optional and not required").
`state-0` gains `credentials=(map [repo=@t name=@t] credential)` where `credential` =
`[value=@t scope=?(%job %env) envs=(set @t) created=@da]`. Operator pokes:
`%set-credential repo name value scope envs`, `%delete-credential repo name`. The value is
a `@t` in Gall state — the spec says so ("Credential values enter pier history when they
are stored") and the fence is that it **never** appears in any scry, JSON, log line, or
event; the UI shows names and scopes only. Release: the daemon's `assignment` for a
`%trusted` job carries `grants=(list [name=@t value=@t expiry=@da nonce=@uv sig=@ux])` for
the credentials whose `scope=%job` (or `%env` with the job's `environment:` in `envs`);
the daemon passes them to `act` as `--secret name=value` and **never writes them to the
`.act.jsonl`** (act masks secrets in its own output; the daemon must additionally scrub
any line containing a grant value before relay — a test proves it). An `%untrusted`
attempt's assignment has `grants=~`, unconditionally.

**D5 — The CI signing key** (§Trust: "A dedicated CI signing key lives in `%urgit-ci`.
The ship's networking authentication key certifies the CI key… The signature format
names the recipient, the attempt, the operation, an expiry, and a nonce"). `state-0` gains
`signing=(unit [pub=@ux sek=@ux cert=@ux created=@da])`, generated on first
`%set-credential` (or `%rotate-ci-key`) from `eny.bowl`. **Arms, verified on the zuse
408/409 tree (`pkg/arvo/sys/zuse.hoon`, `++  ed` at :1020):** keygen `luck:ed:crypto
[sed=@I] → [pub=@uxpoint sek=@uxscalar]` (:1198), signing `sign-raw:ed:crypto [m=@
pub=@uxpoint sek=@uxscalar] → @` (:1236) or `sign-octs-raw` for octs, verification
`veri:ed:crypto [s=@ m=@ pub=@] → ?` (:1262). Each grant and each assignment is signed over
`(jam [recipient=daemon-id attempt operation expiry nonce])`. The daemon **verifies**
the assignment signature with `pub` (received at enroll, pinned in its config) using
Go's `crypto/ed25519` and refuses an unsigned or bad-signed assignment with a logged
reason and no work. **The certificate:** Ames signs with the ship's own keypair as
`(sign-raw:ed:crypto msg [sgn.pub sgn.sek]:saf)` (ames.hoon:380) — the precedent.
Userspace obtains the same ring through **Jael's `%private-keys` task** (lull.hoon:4265;
the gift is `[%private-keys =life vein=(map life ring)]`, :4243): `%urgit-ci` passes
`[%pass /jael/keys %arvo %j %private-keys ~]` in `on-init`, keeps the current `ring`
in state, and derives the ship's `[sgn.pub sgn.sek]` from it the way `+crub` does
(`nol:crub` on the ring, then the sign pair — read `++  crub` at zuse:1703 and cite the
arms). Then `cert = (sign-raw:ed:crypto pub.ci [sgn.pub sgn.sek]:ship)`, re-signed on
every `%private-keys` gift (a key rotation re-certifies). `GET ci/key` → `{pub, cert,
ship-life}`; a verifier that walks the chain to Azimuth is P3. **Neither private key
leaves the ship**: no scry or route returns `sek` or the ring, and the ring is never
logged.

**D6 — The web surface: a CI tab on the repository page, read-first, plus the operator actions**
(§Storage: "web interface"; CI-P2-SCOPE-A). Routes, session-authorized:
`GET ci/repository/<name>/candidates` (newest first, bounded 50, `?before=<id>` cursor),
`GET ci/candidate/<id>` (candidate + its attempts with status/duration/log presence),
`GET ci/attempt/<id>/log` (D2), `GET ci/repository/<name>/policy`, and actions as
`%ci-action` pokes through a session-authorized `POST ci/action` (body = the poke as
JSON): `%approve-candidate`, `%rerun-candidate id` (re-stage same head/base as a new
candidate), `%set-untrusted-policy repo %restricted|%approval`, `%set-credential`,
`%delete-credential`. The frontend adds `ci` to `validTabs`: a candidate list (ref, head
short, actor, status, age, attempt pips), a candidate page (plan jobs as rows: job,
daemon, status, started/finished, a "log" link when present; verdict-reason; the landing
result), and in `settings` → Protected branches a per-branch **CI required** toggle
(the existing `%set-ci-protected`) plus an **Untrusted revisions** radio and a
**Credentials** list (name, scope, envs, delete; add form with a password field that is
cleared on submit). The log view is the raw jsonl rendered as `[job] step: msg` lines,
grouped by `group`/`endgroup`, client-side — no server rendering.

**D7 — CI-DELIVERY-1 becomes spec text.** Add to `specs/native-ci.md` §Execution (or a new
§Delivery under it) the ratified paragraph: at-least-once assignment delivery with the
attempt id as idempotency key; a delivered assignment with no activity re-offered after
two minutes; capacity reported on every poll; work waits for capacity; a conflict fails
the candidate with a reason; non-push-triggered jobs are not planned. One commit, spec
only, first in your sequence — it is already the code's behavior.

**D8 — State stays `state-0`, changed in place.** AGENTS.md's rule holds for `%urgit-ci`
(greenfield, nuked/revived in every battery). The additions in D1/D4/D5 go into
`state-0` directly. `%urgit` is at `state-4`+ with live piers; **you do not touch
`%urgit`'s state at all** — D3's PR-actor staging is a call-site change, not a schema one.

## 3. Fence

- `desk/app/urgit-ci.hoon`, `desk/sur/ci.hoon`, `desk/lib/ci-storage.hoon` (D2 wrapper
  only), `desk/lib/ci-event.hoon` (scrub), `desk/mar/ci-action.hoon` if the mark needs the
  new variants: yours.
- `desk/app/urgit.hoon`: **exactly three touches** — (1) the `pulls/<n>/merge` route in
  `handle-api` (:7417) gains the CI-protected branch: stage instead of write (D3a);
  (2) `land-candidate` (:8051) flips a landed candidate's pull to `%merged` when it
  carries one (D3a); (3) the repository JSON gains `ciUntrustedPolicy` for the settings
  UI. Name all three arms and line ranges in your record. **The `%stage-candidate` action
  gains an optional `pull=(unit @ud)` — that is `sur/ci.hoon`, yours.** Nothing else in
  `urgit.hoon`; `native-pull`'s mold in `sur/git.hoon` is NOT touched (a pull's pending
  candidate lives on the candidate, keyed by pull number, not on the pull).
- `runner/`: the upload step (D1/D2), grant handling + scrub (D4), signature verification
  (D5). No change to the sandbox interface, projection, or poll loop.
- `fe/`: the CI tab, the settings additions, `api.js` helpers, tests.
- `specs/native-ci.md`: D7 only.
- Not in P2: microvm, linked-desk, environments beyond the `envs` set, cache signing
  (§Storage's cache paragraph), the shadow period, override roles, a verifier that checks
  the cert chain, `%urgit` state.

## 4. Live table — every row on a fresh ship, every negative RED first

Extend `.scratch/ci-p1/` (P1's harness) — same env, boot, rootless, drivers. Rows:

| Row | Proves |
|---|---|
| Q1 | D7: spec commit lands first; `git log` shows it before any code |
| Q2 | Log upload: ERPit `plan` attempt finishes → `attempt.log` = `[key size sha256]`; `GET attempt/<id>/log` 302s to a query-presigned URL; `curl -L` with no headers returns the jsonl from the private RustFS bucket; sha256 matches; the same URL after its expiry → 403 |
| Q3 | Trust class in the key: a `%trusted` attempt's key has `/trusted/`; the `sign-get` scry with `%untrusted` for that attempt returns `~` |
| Q4 | Upload name fence: `POST upload` with `name=../x` → 400 |
| Q5a | **Web merge gate (P1 gap):** a writer opens a PR to a CI-protected `master` and clicks Merge → 202 + candidate id, `master` unmoved, candidate `%trusted`; it runs and lands; the pull reads `%merged` only after landing. Merge to an unprotected branch still writes directly |
| Q5 | Untrusted staging: a PR whose `source-ship` is not a writer, merged by a writer → stages `%untrusted %pending`, `plan=~`, zero attempts, no daemon offered work |
| Q6 | `%restricted` policy: same PR → planned and run with `trust=%untrusted`, `grants=~` on the assignment, candidate reaches `%passed` and **cannot land** (`land-candidate` refuses with a reason naming trust) |
| Q7 | Approval: `%approve-candidate` by a writer → new `%trusted` candidate, old `%skipped superseded by approval`, new one runs and lands |
| Q8 | Approval by a non-writer → refused |
| Q9 | Credentials: `%set-credential` then a trusted job whose step `echo $NAME` → the log line is masked (`***`), the assignment JSON has the grant, the daemon's `.act.jsonl` does not contain the value |
| Q10 | Untrusted attempt: `grants=~` even with credentials set |
| Q11 | Credential never in a read: every scry/route JSON `grep -c <value>` = 0; `%delete-credential` removes it and a rerun's grant list is empty |
| Q12 | Signing: assignment has `sig`; the daemon verifies; a daemon with a wrong `pub` in its config refuses every assignment and logs why; `GET ci/key` returns `{pub, cert}` and never `sec` |
| Q13 | Grant expiry: a grant with `expiry` in the past is refused by the daemon (it does not pass the secret to act) |
| Q14 | CI tab: `GET candidates` for the ERPit repo lists the P15-shaped run with 8 attempts; the candidate page shows every job's status and a log link; the log renders |
| Q15 | Settings: the CI-required toggle round-trips `%set-ci-protected`; the untrusted radio round-trips the policy; the credentials form adds and deletes, and the page source never contains the value |
| Q16 | Session fence: every `ci/*` read and `POST ci/action` without a session → 401; with a session → 200 |
| Q17 | P1 regression: `.scratch/ci-p1/battery.sh` (P1–P20) still 20/20 on this tree; mutants 13/13 both ways |
| Q18 | Full ERPit: push a real ERPit revision as a writer → 8/8 jobs, 8 logs in the bucket under `/trusted/`, lands |

Negatives (Q4, Q5a's `master` unmoved, Q5, Q6's cannot-land, Q8, Q10, Q11, Q12's wrong-pub, Q13, Q16) must be
shown RED against a one-line sabotage first, with the tripwire string named per row
(P1's `mutants.sh` shape). A GREEN without its RED is not a row.

## 5. Harness and record

P1's `.scratch/ci-p1/` scripts are the base: `boot.sh`, `docker-rootless.sh`,
`act-static.sh`, `runner.sh`, the drivers. Add `p2-*.sh` per row group, a `q-mutants.sh`
for the new negatives, and `battery.sh` runs P0 → P1 → P2 in order. **Ruled (astra §3):
there is no object-store fixture anywhere in the tree** — P1's H15 proved a signing
result against `127.0.0.1:1`, and the `install-s3.sh` the earlier draft cited installs
the desk, not a store. Build one in S1: `.scratch/ci-p1/store.sh start|stop|status`
running **RustFS** (Apache-2.0, SigV4, the store NativePlanet ships by default; MinIO's
open-source repo is archived) as a rootless container on the harness Docker daemon —
own port (footer), data dir under `.scratch/tmp/store-data`, one **private** bucket, a
scoped access key; then the seven `%storage-action` pokes point the fixture ship at it
(the P0 recipe in the rulings reference). Q2/Q3/Q18 read from that bucket with `curl`,
never from the container's filesystem. `store.sh stop` is part of shutdown. If RustFS's
SigV4 rejects a request the signer produces, that is a finding against the SIGNER (P0
targeted AWS's documented canonical form) — box it with the store's error body. The fe tests run in `foreground.sh` alongside `go test`. Record:
`.scratch/p2-live-table.md`, same columns as P1's, observed values from YOUR run, a
Deviations section, shutdown by `/proc`-verified PID, pier retained.

Commits: `ci-p2: S<n> — …`, one per stage: S0 spec (D7, spec file only) · S1 footer env
rewrite + store fixture + sur + storage routes + presign + upload (D1/D2, Q2–Q4) · S2 web-merge gate + trust + approval (D3/D3a, Q5a–Q8) · S3 credentials + grants (D4, Q9–Q11) ·
S4 signing (D5, Q12–Q13) · S5 fe (D6, Q14–Q16) · S6 battery + record (Q17–Q18). Push
nothing. Merge nothing.

## 6. Boxes

`QUESTIONS-CI-P2.md`, `## §<n>` per question: what you read, what you tried, what you
would do under each answer. Commit it and stop. A box is a successful outcome; the
operator rules them. Expected boxes: the ed25519 arm names on the pinned pill; whether
`%urgit`'s PR path stages at all today; act's `--secret` masking behavior on multi-line
values. Do not guess past any of them.
