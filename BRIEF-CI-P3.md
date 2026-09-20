# BRIEF — CI P3: the operator surface

Native CI phase 3 on `master` at `c8007be` (P0 contracts, P1 scheduler/daemon, P2 trust/storage/UI
merged and battery-verified). **The operator looked at P2 in a browser and ruled: nothing goes
upstream until an operator can set this up without the dojo, watch it live, and read a README that
tells the truth.** That is P3. The `microvm` backend is P4. Nothing about execution, trust, or
storage *mechanism* changes here — P3 gives the mechanisms an operator surface and closes one
scheduler hole. Read `REFERENCE-CI-RULINGS.md` (planted): CI-P3-SCOPE-A binds this brief;
CI-DELIVERY-1.1 (corrected 2026-09-18) is item 6.

Read in this order: this brief; `specs/native-ci.md` §Trust, §Recovery, §Packaging; the rulings
reference; `desk/sur/ci.hoon` (`daemon` at the `+$  daemon` mold; `action` union);
`desk/app/urgit-ci.hoon` (`on-watch` :95, `%mint-enroll-token` :677, enroll route :1454,
`POST ci/action` :1503 and its allow-list ~:2085–2140); `desk/app/urgit.hoon` `on-watch` :9347
(the `[%peer %activity ~]` fact path — the pattern for item 2); `fe/src/channel.js`,
`fe/src/transferFeed.js` (how the app already consumes a fact); `fe/src/components/CiTab.jsx`,
`RepositoryView.jsx` (settings sections); `runner/README.md`; `.scratch/p2-live-table.md`.

## 1. What is true at `c8007be` (verified; do not re-derive)

- **One dojo step remains for an operator:** `:urgit-ci|mint-enroll-token`. The handler (:677)
  takes `token=@uv` FROM THE POKER — the ship does not generate it — hashes it, and creates a
  `daemon` record `[id token-hash ~ now ~ ~ 1 '' ~]`. `%rotate-ci-key` is in the union but NOT on
  `POST ci/action`'s allow-list. No revoke action exists. `/daemons` and `/daemon/<id>` are scries
  with no HTTP route and no panel.
- `daemon` = `[id token-hash bearer-hash=(unit @) minted=@da enrolled=(unit @da) last-seen=(unit
  @da) capacity=@ud sandbox=@t running=(set attempt-id)]` — enough for a Runners panel without a
  mold change.
- `%urgit-ci`'s `on-watch` (:95) accepts only `/http-response`; **no app subscription exists**.
  `%urgit` gives `%fact`s on `[%peer %activity ~]` (:9347) and the frontend consumes them through
  `fe/src/channel.js` → `transferFeed.js` — the mechanism item 2 reuses.
- The `POST ci/action` allow-list is: `approve-candidate`, `rerun-candidate`, `set-ci-protected`,
  `set-credential`, `set-untrusted-policy`, `delete-credential`. Everything else is `src.bowl=our`
  poke-only.
- The demo's log view failed with `NetworkError`: the presigned URL was
  `http://127.0.0.1:8372/…` — the `%storage` endpoint as the SHIP knew it, unreachable from any
  other machine. Every P2 battery passed Q2 because `curl -L` ran on the host. **Nothing checks
  that the endpoint is viewer-reachable, and the README does not say it must be.**
- The ghost (CI-DELIVERY-1.1): a daemon that POSTs `abandon` stays eligible; the abandoned attempt
  is not re-offered and waits `default-deadline` (~h1). Q12's wrong-key daemon caused Q18's first
  FAIL in two batteries. The P2 harness *waits it out*; the product is unfixed.
- `eny.bowl` is already used for the CI keypair (:1289) and nonces (:1328).
- **Scheduling is a pool.** `select-daemon` (:823) filters to enrolled + seen-within-`stale-after` +
  under-capacity, then sorts fewest-running, oldest-enrolled. A workflow job's `runs-on` is parsed
  by nothing on either side (`grep -rn runs-on desk/ runner/` is empty); `sandbox.daemon` is a
  free string the ship stores and never reads. There is no way to bind a daemon to a repository
  or a job to a class of daemon; a fork PR's code can run on any enrolled box.
- The daemon is OUTSIDE the sandbox: `Sandbox` (`runner/internal/sandbox/sandbox.go:36`) is a
  six-method interface; `docker.go` is the only backend; `microvm.go` is a 7-line refusal (P4).

## 2. Derivations (each cites its section; each is a fence)

**D1 — The ship mints the token; the UI shows it once** (§Trust: "`%urgit-ci` stores third-party
credentials"; CI-P3-SCOPE-A). `%mint-enroll-token` loses its `token` field: the ship draws 256
bits from `eny.bowl`, stores the hash, and **returns the raw `@uv` in the poke's response** —
the only time it exists outside the operator's clipboard. Route: `POST ci/runners/mint` (session)
→ `{id, token, config-snippet}` where the snippet is the three TOML lines the daemon needs
(`ship_url`, `enroll_token`, `sandbox`). The record's `minted` is set; `enrolled` stays `~`.
The dojo generator is deleted, not kept as an alias.

**D2 — Expire, regenerate, revoke** (CI-P3-SCOPE-A: "or expired/regenerated"). New actions,
all on the allow-list: `%expire-token id` — a minted-not-enrolled record is deleted (409 if
enrolled); `%revoke-daemon id` — an enrolled daemon's `bearer-hash` is cleared and the record
marked `revoked=@da` (mold gains this one field, `state-0` in place per AGENTS.md), its `running`
attempts are re-offered (D6 makes that real), and its next poll answers **401**; the daemon logs
`revoked by the ship` and exits non-zero. "Regenerate" is expire-or-revoke + mint, two actions,
one button.

**D2b — Labels and repository binding** (§Scheduling: "least-loaded daemon with capacity as its
`needs` and `if` allow" — the pool stays the default; this narrows it, GitHub's way). Two
mechanisms, one seam in `select-daemon`:

*Labels — declared by the daemon, matched to the job.* `urgit-runner.toml` gains
`labels = ["linux", "x64", …]`; the daemon sends the set at enroll and on every poll
(`x-ci-labels`, comma-separated, like `x-ci-capacity`); the ship stores it on `daemon`
(`labels=(set @t)`, `state-0` in place). The plan step records each job's `runs-on` as a set
(a string is a one-element set, a sequence its elements — **literal forms only**; a `${{ }}` expression in `runs-on` is refused at plan time with `runs-on expression unsupported in P3`, never left pending; **the P1 matrix refusal stands** — rider 2). Selection
requires `runs-on ⊆ labels ∪ implicit`, where **implicit** is every daemon's standing set
`{self-hosted, linux, ubuntu-latest, ubuntu-22.04, ubuntu-24.04, x64}` — so every workflow that
runs today keeps running on a daemon that declares nothing. A job with no eligible daemon is
`%pending` with reason `no runner has labels [big-mem]`, shown on the candidate row and in the
live feed (D4); it is never a silent wait. The CI tab's job row shows the job's `runs-on`.

*Repository binding — set by the operator on the ship, never by the daemon.* `daemon` gains
`repos=(unit (set @t))`: `~` is the pool (default, the P2 behavior); `[~ set]` restricts the
daemon to those repositories. Set in the Runners panel per row ("Repositories: any / only …",
a picker over the ship's repositories); action `%set-daemon-repos id repos` on the allow-list.
Enforced in `select-daemon`: a daemon with a set is skipped for any other repository. The
TOML has no field for this — the box does not get to declare which code it may run; the ship
does. The panel row shows the binding; the mint modal offers it as an optional step ("this
runner will only take: …"). Together: an operator can keep one box that only ever executes
trusted code from named repositories while a second box takes everything, including fork PRs.

Rows: a two-label job lands only on the labelled daemon while a plain job lands on either; a
job asking for a label no daemon has reads `%pending` with the reason and lands within one
poll of a daemon that has it enrolling; a repo-bound daemon is never selected for another repo
even when it is the only idle one (that job waits or lands on the other); the binding survives
the daemon's restart (it is ship state).

**D3 — Runners panel** (Settings → new section, above Protected branches). A table: short id,
capacity, sandbox, **labels**, **repositories (any / n named)**, `enrolled` age, `last-seen` age, running count, and a state pip: `minted`
(never enrolled), `healthy` (seen within `stale-after`, ~m5 — the scheduler's own eligibility window; ONE number), `stale` (seen ≥ `stale-after`), `refused` (signature/key refusal; re-enroll to clear), `revoked`. Buttons: **Mint token** (modal: the token in a copy field with
"shown once" language and the config snippet beside it; closing the modal is the last time it
is readable), per-row **Expire** / **Revoke** with a confirm, **Rotate CI key** (calls
`%rotate-ci-key`, now allow-listed; every enrolled daemon's next assignment fails signature
verification until the operator re-enrolls it — the modal says so). Route: `GET ci/runners`
(session) from the `/daemons` scry. Q-rows prove every button against ship state.

**D4 — Live updates** (CI-P3-SCOPE-A: "realtime updates of job status"). `%urgit-ci` gains
`on-watch` on `[%ci %repository @ ~]`: an initial `%fact` with the repository's candidate list
(the same JSON as `GET ci/repository/<name>/candidates`), then a `%fact` on every state change
to any candidate, attempt, or daemon that touches that repository — staged, planned, assigned,
each attempt status, log handle present, verdict, landed. Payload: `{kind, id, patch}` where
`patch` is the changed candidate's full row (not a diff — the client replaces by id). The CI
tab subscribes through `fe/src/channel.js` exactly as `transferFeed.js` does for
`/peer/activity`, replaces rows by id, and keeps **Refresh** as the fallback when the channel is
down (show a "live" / "polling" pip). A `%kick` reconnects. Session-authorized: an `on-watch`
from a non-`our` ship is refused (`?>  =(our src)`).

**D5 — Storage reachability** (§Storage: "a short-lived download URL for each authorized
viewer" — which is only true if the viewer can reach it). (a) The CI tab header shows the
`%storage` endpoint host and a pip: on load, the tab requests one presigned probe URL
(`GET ci/storage/probe` → a presigned `HEAD` on a zero-byte `ci/_probe` object the ship PUTs
once at `%set-ci-protected` time) and fetches it **from the browser**; reachable → green,
`NetworkError` → red with the sentence "*Your browser cannot reach the object store at
`<host>`. Logs and artifacts will not open. The endpoint must be reachable from every viewer's
network, not only from the ship's host.*" (b) The same check runs when the operator flips
**CI required** on: a red probe is a warning on the toggle, not a refusal (the daemon may still
reach it). (c) `store.sh` in the harness binds `0.0.0.0` and the ship's `%storage` endpoint is
the footer's LAN address, so the row that proves (a) runs its fetch from **a second machine**
(`np`, `192.168.1.64`) — a probe from the host is not evidence.

**D6 — CI-DELIVERY-1.1** (§Recovery; the ledger's corrected statement). (a) `abandon` re-offers
the attempt immediately to any other eligible daemon (idempotent on attempt id; the abandoning
daemon is excluded for that attempt). (b) A daemon that abandons with a signature or key reason
is marked **`refused`** at once and is **not offered work until it re-enrolls** (the corrected
CI-DELIVERY-1.1; rider 1 on astra §1). An ordinary bearer-authenticated poll must NOT restore it:
the bearer is fine, the pinned CI public key is what is wrong, and only fresh enrollment
refreshes that key. Its polls keep answering (so the operator sees it `refused` in the Runners
panel with the reason, not `stale`); the daemon logs the refusal and the README's recovery step
(delete the state file, re-enroll with a new token). R9's RED tripwire: after the refusal, N
ordinary polls do not make it eligible; GREEN: another daemon lands 8/8, and re-enrollment
restores service. (c) An attempt with events and no result for longer than the
job's own timeout + 2 min (not ~h1) is re-offered once, then failed `%infrastructure-error
'runner went silent'`. (d) `%revoke-daemon` (D2) uses the same path. **(e) Rider 2 readings, ratified:** an abandon with no OTHER live daemon (capacity aside — a busy other daemon means the job waits) closes the attempt as P1 does (`%infrastructure-error 'abandoned: <reason>'`; P10 asserts it); "the job's own timeout" is its `timeout-minutes` when declared, `~h1` otherwise; an assignment never fetched, whose daemon's record is stale/refused/revoked, is re-offered to another daemon (`reoffer-unfetched` — the closeout's ghost). **(f) The daemon polls at capacity too** (`select-daemon` already refuses a full daemon, so the poll is a heartbeat), so `last-seen` is always liveness and a busy quiet daemon never reads `stale`. **(g) Rider 3 — shared-Docker reconciliation is ownership-aware
(astra §4; ratified B):** two runners on one rootless Docker daemon is a supported deployment.
Every sandbox object the backend creates (`docker.go:71,74,80` — network, volume, runner
container) carries a second label `urgit-ci-daemon=<daemon-id>`, and `Orphans` (`docker.go:192`)
filters on BOTH labels so a runner never inspects or destroys another runner's sandbox. The ship
client stops mapping every 401 to `ErrUnauthorized` (`client.go:171,297`): a 401 whose body is
`attempt authentication required` (the ship's foreign-attempt answer, `urgit-ci.hoon:1677`) is a
distinct `ErrNotOurs` that `Reconcile` (`daemon.go:129`) treats as "leave it, not mine"; only a
401 on poll/enroll/own-attempt paths means enrollment lost. Pre-existing unlabelled sandboxes
(from P1/P2 daemons) are still reaped by a runner that finds no owner label — one line, so an
upgrade does not orphan a stuck container forever. R11b's RED is exactly astra's reproduction:
restart B while A holds a sandbox → B must NOT exit 3, must NOT touch A's network, and must poll
within one interval; GREEN with the binding intact. Rows: the wrong-key
daemon + a live push lands 8/8 with no ~h1 wait; N+1 jobs on capacity N at t+0 all complete.

**D7 — First-run states.** A repo with CI required and zero non-revoked daemons: the CI tab
shows "*No runner is enrolled. Mint a token in Settings → Runners and install the daemon*"
with a link to the README section. A repo with a daemon but `%storage` unset: the existing
refusal string, surfaced in the toggle's error rather than a 409 body. A candidate `%pending`
with `trust=%untrusted` and no approver action: the Approve button's tooltip names the policy.

**D8 — README rewrite: the operator's setup guide.** `runner/README.md` becomes the document
an operator reads top to bottom, in this order, each step one screen: (1) **Object store** —
what `%storage` needs, and in bold: *the endpoint must be reachable from every browser that
will view logs, not only from the ship's host; a `localhost` or LAN-only endpoint works for the
runner and breaks the log view for everyone else, and the symptom is `Log unavailable:
NetworkError`*; how GroundSeg fills this in on a NativePlanet box. (2) **Mint a token** — in
the UI, Settings → Runners → Mint; shown once. (3) **Install the daemon** — the three commands,
the TOML (with the snippet the UI gave), rootless Docker. (4) **Protect a branch** — the CI
required toggle; what happens to a push. (5) **Watch** — the CI tab, live. (6) **Runners** —
healthy/stale/revoked, rotate the key, what each means for a running daemon; **labels** (in the
TOML, matched to `runs-on`, the implicit set named) and **repository binding** (in the panel,
not the TOML, and why). (7)
**Limitations in this release** — kept, updated (microvm → P4; per-line scrub if still deferred;
cross-ship approve). The `:urgit-ci|mint-enroll-token` line and every other dojo instruction
are removed. Written plainly; no operator infrastructure named (no Tellurian, StarTram,
GroundSeg beyond the one mention, herdr, chairs, ruling numbers).

**D9 — Linked-desk landing, per-line scrub, cross-ship approve** — carried from the P2 agenda,
each as before (CI-LINKED-DESK-P1-B alternative A; CI-P2-SECRET-1's P3 item: every line ≥ 8
chars of every released value in the scrub set on daemon and ship; `%approve-candidate` over
`%git-peer` with `src.bowl` as the actor through `ci-can-write`). **These are S6–S8 and are
built only after S0–S5 are green** — the operator surface is the deliverable; if time runs
short these are boxed as "not started", never half-built.

## 3. Fence

- `desk/app/urgit-ci.hoon`, `desk/sur/ci.hoon` (`revoked`, `labels`, `repos` fields; `runs-on` on the plan's job; the action union), `desk/mar/ci-action.hoon`, **`desk/lib/ci-plan.hoon` and `desk/gen/ci-plan-vector.hoon`** (the plan parser and its vectors — `runs-on`/`timeout-minutes` are decoded there, rider 2): yours.
- `desk/app/urgit.hoon`: **only** D9's linked-desk work (S6), fenced to the receive tail as
  CI-LINKED-DESK-P1-B names it; nothing for S0–S5. **Rider 4 (astra §5) — for R16 only, the fence
  also admits:** `desk/sur/git-peer.hoon` for ONE additive `packet` variant carrying the
  repository and candidate id (no other mold change); `desk/mar/git-peer.hoon` only as the mold's
  consequence; `%urgit`'s `++handle-peer` dispatch (`urgit.hoon:2768`) for that variant and the
  reply plumbing that reports acceptance/refusal to the requester. The receiving arm derives the
  actor from `src.bowl` — never from the packet — and forwards to the existing local
  `%urgit-ci` `%approve-candidate` action, which keeps its `ci-can-write` check unchanged. The
  requester's packet carries no actor field; a packet that tries is malformed. R16 proves a real
  second-ship refusal as a non-writer, then approval as a listed writer, with a named
  actor-substitution RED (a forged actor in the packet, or the request arriving from a ship that
  is not the actor) before GREEN. The `%ci-action` route stays local-only (`urgit-ci.hoon:80`). **Rider 5 (opus §5) — the
`pending-clay` guards:** `=(^ pending-clay)` compares against the wing `^`, not a pattern, so the
six pre-P3 guards in `urgit.hoon` (master lines 2664, 6538, 6629, 7481, 7685, 7708 — peer push
into a bound repo, web merges, API bind/publish) never fire and a second Clay operation overwrites
a parked first one. Fixing them is IN the fence for both chairs: the one-token change to
`!=(~ pending-clay)` / `!=(~ pending-publish)` (or `?=(^ …)`), all six sites plus D9's own, ONE
commit, plus one row proving a concurrent linked peer push is refused with the existing
"already in progress" message while the parked one completes. No other change at those sites.
- `runner/`: `labels` in the TOML + `x-ci-labels` on enroll/poll, `runs-on` in the plan (D2b), D2's 401-on-revoke exit, **D6(g)'s ownership labels in `sandbox/docker.go` (labels and the `Orphans` filter ONLY — not the `Sandbox` interface, not `Prepare/Run`) and the 401 split in `ship/client.go` + `daemon.go`'s `Reconcile`**, D6's re-offer handling (idempotent claim already exists),
  D9's scrub. Nothing in the sandbox interface.
- `fe/`: the Runners section (with labels and the repository picker), `runs-on` on job rows, the live channel, the storage pip, first-run states, tests.
- `runner/README.md`: D8. `specs/native-ci.md`: CI-DELIVERY-1.1 as spec text under §Recovery.
- Not in P3: microvm, override roles, the shadow period, environments beyond `envs`.

## 4. Live table

| Row | Proves |
|---|---|
| R1 | Mint in the UI → token shown once, `daemon` record `minted`, `enrolled=~`; the dojo generator is gone (`+urgit-ci` has no `mint-enroll-token`) |
| R2 | A daemon started with that token enrolls; the panel row flips `minted → healthy` **without Refresh** (D4) |
| R3 | Expire a minted-not-enrolled token → record gone; expire an enrolled one → 409 |
| R4 | Revoke an enrolled daemon → its next poll 401s, the daemon logs and exits non-zero, its running attempt is re-offered and completes on another daemon |
| R5 | Rotate CI key → the panel warns; an enrolled daemon's next assignment fails verification; re-enroll → works |
| R6 | Live: push a commit; the CI tab shows staged → planned → each attempt → landed with no Refresh; the pip reads "live"; kill the channel → pip "polling", Refresh still works |
| R7 | Storage probe from **np**: LAN endpoint → green and a log opens in np's browser (curl -L from np); a `127.0.0.1` endpoint → red with the exact sentence, and the log route still 302s |
| R8 | First-run: CI required with no daemon → the message; unset `%storage` → the toggle's error |
| R9 | Ghost (CI-DELIVERY-1.1): wrong-key daemon enrolled + a live 8-job push → 8/8 lands, no ~h1 wait, the wrong-key daemon reads `stale` |
| R10 | Silent runner: SIGSTOP a daemon mid-job → the attempt is re-offered after timeout+2 min, completes elsewhere; SIGCONT → the late result is refused `attempt is closed` |
| R11 | N+1 on capacity N at t+0 → all complete |
| R11a | Labels: `runs-on: [self-hosted, big-mem]` lands only on the `big-mem` daemon; a plain `ubuntu-latest` job lands on either; no daemon has `gpu` → `%pending 'no runner has labels [gpu]'` on the row and in the feed; enroll a `gpu` daemon → lands within one poll |
| R11b | Binding: daemon B bound to `ci-p3` only; a push to `erpit-…` with A busy and B idle → the job waits for A (B never selected); restart B → binding intact; unbind in the panel → B takes the next job |
| R12 | README: every step's command/click exists; `grep -c ':urgit-ci|' README.md` = 0; the reachability sentence present |
| R13 | P0 + P1 + P2 regression: all three batteries on this tree, mutants both ways |
| R14–R16 | D9 rows as ruled (linked-desk lands; PEM credential masked line-by-line; approve from the second galaxy via `%git-peer`) |

Negatives (R3's 409, R4's 401, R5's verification failure, R7's red, R9, R10's refused late
result) RED first with named tripwires, P2's `q-mutants.sh tripwire <row>` shape.

## 5. Harness

P2's `.scratch/ci-p2/` is the base (ships in **tmux**, `DAEMON_CAPACITY` from the footer, the
store `ready` assert, `/proc` shutdown). Add `.scratch/ci-p3/` rows R1–R16, a `cold.sh` that
runs P0 → P1 → P2 → P3, and `r7-remote.sh` that runs its fetch over `ssh np` (the footer names
the host; read-only use of that box). `store.sh` binds `0.0.0.0:$STORE_PORT` and `%storage`'s
endpoint is `http://192.168.1.229:$STORE_PORT` (the footer's LAN address; the harness reads it
from a `STORE_ADVERTISE` var in `ci-p1/env.sh`, default `127.0.0.1` so P0–P2 rows are unchanged).
Record `.scratch/p3-live-table.md`,
same columns, Deviations, PID shutdown, piers retained.

Commits `ci-p3: S<n> — …`: S0 spec (CI-DELIVERY-1.1 text) · S1 mint/expire/revoke/rotate + the
runners route (D1–D2, R1, R3–R5) · S1b labels + repository binding (D2b, R11a–R11b) · S2 Runners panel (D3, R2 needs S3) · S3 live channel (D4, R2,
R6) · S4 storage probe + first-run (D5, D7, R7–R8) · S5 the ghost + silent runner (D6, R9–R11) ·
S6 README (D8, R12) · S7 regression (R13) · S8 D9 (R14–R16) · S9 record. Push nothing.

## 6. Boxes

`QUESTIONS-CI-P3.md`, `## §<n>`: read / tried / would-do. A box is a successful outcome. Expected:
whether Eyre's channel can carry a `%fact` from `%urgit-ci` to a page served by `%urgit`
(same ship, different app — it can; the subscribe action names the app); the poll interval
constant for "stale"; whether `%rotate-ci-key` should also re-sign the cert (it must, D5 of P2).
**Provider safeguard rule:** both providers' filters have fired on the MUTANT phase (its RED
output reads as an exploit narrative). If yours fires: retry the same turn once; on a second
fire, commit what is green, record the row `provider-blocked, not run`, and stop.

## Launch footer — astra

- Worktree `/var/home/michael/workspace/urbit/urgit-ci-p3-astra`, branch `ci/p3-astra`, base
  `master` at `c8007be`.
- **First ship** `~dyr`, HTTP port `8380`, pier `/var/home/michael/piers/urgit-ci-p3-dyr`, tmux
  session `ci-p3-astra-dyr`. **Second ship** (the non-writer PR author; R14–R16) `~fen`, HTTP
  port `8381`, pier `/var/home/michael/piers/urgit-ci-p3-fen`, session `ci-p3-astra-fen`.
  Rootless Docker state dir `/run/user/1000/ci-p3-astra/`, data root
  `<worktree>/.scratch/tmp/docker-data`. Store (RustFS) port `8382`, container
  `urgit-ci-store-dyr`, data `<worktree>/.scratch/tmp/store-data`.
- Boot each ship in a **tmux session named after its pier** (`tmux new -d -s <session> …`), never
  a herdr pane: `/var/home/michael/workspace/urbit/bin/urbit -F <ship> -B
  /var/home/michael/workspace/urbit/pills/brass-408k-1.pill --loom 34 --http-port <port> -c <pier>`
  — **no `-p`**. Poll the dojo prompt, never a fixed sleep. Neither pier may exist before your
  first boot. Rewrite `SHIP/PORT/PIER` (+ `SHIP2/PORT2/PIER2`) in `.scratch/ci-p0/env.sh`,
  `DOCKER_STATE` and `STORE_PORT` in `.scratch/ci-p1/env.sh`, inside S1 before your first boot —
  S0 stays spec-only.
- **The store binds `0.0.0.0:$STORE_PORT` and `%storage`'s endpoint is
  `http://192.168.1.229:$STORE_PORT`** (this host's LAN address) — a `127.0.0.1` endpoint is R7's
  RED case, not the default. R7's browser-side fetch runs over `ssh np` (`192.168.1.64`,
  read-only use: `curl` only; nothing written there).
- Do NOT boot `~zod ~nec ~bud ~wes ~bel ~bus ~syt ~dur ~wep ~ser ~ryx ~wyd ~tem ~mul ~dev ~sev
  ~lyt ~ryp ~syx ~nup ~heb ~peg ~lun ~lup ~dys ~lug ~hec ~dep ~put ~mep ~lut ~sep ~pes ~ped ~led`
  (live ships, the operator's demo pair, other chairs) or the other P3 chair's pair. Do not
  touch ports `8340–8372`, `/run/user/1000/urgit-demo/`, or any `urgit-ci-store-*` container
  that is not yours.
- ERPit workload: clone `/var/home/michael/workspace/urbit/erpit` (read-only source) into your
  fixture repo as P1's harness does; never push to it. Pin the rev P2 pinned.
- `act` 0.2.89 via `.scratch/ci-p1/act-static.sh`, `go` 1.27, `zig` 0.15.2 on PATH
  (`/home/linuxbrew/.linuxbrew/bin`). Upstream urbit source for zuse/lull/ames/jael/eyre
  citations: `/var/home/michael/workspace/urbit/urbit/pkg/arvo/sys/` (read-only).
- **Provider safeguard rule (§6) applies from the first mutant.** A filed box is a successful
  outcome; stop on it. No push, no merge, no rebase, no `/tmp`.

## Launch footer — opus

- Worktree `/var/home/michael/workspace/urbit/urgit-ci-p3-opus`, branch `ci/p3-opus`, base
  `master` at `c8007be`.
- **First ship** `~sud`, HTTP port `8390`, pier `/var/home/michael/piers/urgit-ci-p3-sud`, tmux
  session `ci-p3-opus-sud`. **Second ship** (the non-writer PR author; R14–R16) `~tug`, HTTP
  port `8391`, pier `/var/home/michael/piers/urgit-ci-p3-tug`, session `ci-p3-opus-tug`.
  Rootless Docker state dir `/run/user/1000/ci-p3-opus/`, data root
  `<worktree>/.scratch/tmp/docker-data`. Store (RustFS) port `8392`, container
  `urgit-ci-store-sud`, data `<worktree>/.scratch/tmp/store-data`.
- Boot each ship in a **tmux session named after its pier** (`tmux new -d -s <session> …`), never
  a herdr pane: `/var/home/michael/workspace/urbit/bin/urbit -F <ship> -B
  /var/home/michael/workspace/urbit/pills/brass-408k-1.pill --loom 34 --http-port <port> -c <pier>`
  — **no `-p`**. Poll the dojo prompt, never a fixed sleep. Neither pier may exist before your
  first boot. Rewrite `SHIP/PORT/PIER` (+ `SHIP2/PORT2/PIER2`) in `.scratch/ci-p0/env.sh`,
  `DOCKER_STATE` and `STORE_PORT` in `.scratch/ci-p1/env.sh`, inside S1 before your first boot —
  S0 stays spec-only.
- **The store binds `0.0.0.0:$STORE_PORT` and `%storage`'s endpoint is
  `http://192.168.1.229:$STORE_PORT`** (this host's LAN address) — a `127.0.0.1` endpoint is R7's
  RED case, not the default. R7's browser-side fetch runs over `ssh np` (`192.168.1.64`,
  read-only use: `curl` only; nothing written there).
- Do NOT boot `~zod ~nec ~bud ~wes ~bel ~bus ~syt ~dur ~wep ~ser ~ryx ~wyd ~tem ~mul ~dev ~sev
  ~lyt ~ryp ~syx ~nup ~heb ~peg ~lun ~lup ~dys ~lug ~hec ~dep ~put ~mep ~lut ~sep ~pes ~ped ~led`
  (live ships, the operator's demo pair, other chairs) or the other P3 chair's pair. Do not
  touch ports `8340–8372`, `/run/user/1000/urgit-demo/`, or any `urgit-ci-store-*` container
  that is not yours.
- ERPit workload: clone `/var/home/michael/workspace/urbit/erpit` (read-only source) into your
  fixture repo as P1's harness does; never push to it. Pin the rev P2 pinned.
- `act` 0.2.89 via `.scratch/ci-p1/act-static.sh`, `go` 1.27, `zig` 0.15.2 on PATH
  (`/home/linuxbrew/.linuxbrew/bin`). Upstream urbit source for zuse/lull/ames/jael/eyre
  citations: `/var/home/michael/workspace/urbit/urbit/pkg/arvo/sys/` (read-only).
- **Provider safeguard rule (§6) applies from the first mutant.** A filed box is a successful
  outcome; stop on it. No push, no merge, no rebase, no `/tmp`.



## Riders

- **Rider 1 (astra §1, 2026-09-18 23:40).** D6(b) said a wrong-key daemon returns on "a successful poll"; the ledger's corrected CI-DELIVERY-1.1 says "until it re-enrolls". The ledger governs — a poll authenticates the bearer, not the pinned key. D6(b) rewritten; the Runners pip gains `refused`; R9's tripwire named. No other change.

- **Rider 2 (astra §2–§3, opus §2 + §4; 2026-09-19 10:50).** Fence gains `desk/lib/ci-plan.hoon` + `desk/gen/ci-plan-vector.hoon` (the parser lives there). Matrix refusal RETAINED; `runs-on` literal forms only, expressions refused at plan time — the brief's "expanded as act already does" was wrong (astra ran `act -l` on the matrix fixture: one row). `stale` = `stale-after` (~m5), one number; the daemon polls at capacity so `last-seen` is liveness (opus §2 finding). Opus §4(iii)'s three re-offer readings stand as D6(e). Opus §1/§3 were answered in-brief; no change.
- **Rider 3 (astra §4; 2026-09-19 13:12).** Two runners on one Docker daemon: B's restart reconciled A's sandbox, got the ship's correct 401 for a foreign attempt, and the client read it as `enrollment lost` → exit 3 (astra's reproduction, `.scratch/ci-p3/box4-reconcile.py`). Ratified **B — fix it, bounded**, over documenting one-runner-per-daemon: ownership label on every sandbox object, `Orphans` filters on it, the client distinguishes `attempt authentication required` from enrollment loss. D6(g); fence widened to `docker.go` labels/`Orphans` and `client.go`/`Reconcile`. The harness stays on shared Docker so R11b exercises the fix.
- **Rider 4 (astra §5; 2026-09-19 18:40).** D9's cross-ship approve needs a `%git-peer` packet variant and `%urgit`'s peer dispatch, which the §3 fence excluded (it admitted `urgit.hoon` for the linked-desk receive tail only). Fence widened exactly that far; actor from `src.bowl`, never the packet; local `%approve-candidate` + `ci-can-write` unchanged; actor-substitution RED named. Both chairs are judged on this fence at the pick.
- **Rider 5 (opus §5; 2026-09-19 19:20).** Opus's cold run P19 raced two landings on one linked ref and lost one: D9's guard had copied the six pre-P3 `=(^ pending-clay)` sites, which compare against the wing `^` and are never true (dojo-verified on ~sud; `?^` at line 4051 is the working form). Six sites verified on master. Ruled: fix all six + D9's in one commit, one concurrency row — a guard that never guards is a bug we now know about, not someone else's receive path.
