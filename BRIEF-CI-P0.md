# DISPATCH BRIEF — urgit native CI, P0: contracts and harness

You are implementing P0 of the native CI system in the urgit worktree named in
the launch footer (branch `ci/p0-contracts`, cut from `master` at `9cec037`).
Read FIRST, in order:

1. `specs/native-ci.md` — the ratified design. It is law. Sections that bind
   this task: Boundary, Execution, Protected refs, Storage, Packaging.
2. `REFERENCE-CI-RULINGS.md` at the worktree root (untracked; planted for you) —
   the one-line ruling map and the source anchors. Read it; do not edit it.
3. `AGENTS.md` — repo conventions. **Its "keep schemas at `state-0`, no
   migrations" line is stale for `%urgit`** (the tree is at `state-4`; see
   `desk/app/urgit.hoon:2206–2216`). It is **current** for the new agent you
   are writing: `%urgit-ci` starts at `%0` and stays there in this task.
4. `desk/app/urgit.hoon` lines 2196–2232 (`on-init`, `on-load`), 7761–7800
   (`storage-settings`), 7841–7887 (`apply-receive`, `receive-policy-error`),
   8447+ (`handle-receive-pack`), 8951–8964 (`on-peek` head).
5. `desk/gen/git-access-vector.hoon` — the shape every vector in this repo
   takes. Yours copy it.

## §0 — What P0 is

P0 builds the **contracts** the rest of native CI hangs on, and a **harness**
that can prove each contract refuses what it must refuse. No scheduler, no
runner daemon binary, no UI, no object-store upload. Every later phase calls
what you define here; nothing you define here calls anything that does not
exist yet.

Three contracts, one harness:

- **C1** — `%urgit` ↔ `%urgit-ci` on the same ship: the landing-eligibility
  scry, the candidate-materialization poke, the staged-write poke.
- **C2** — `%urgit-ci` ↔ runner daemon: enrollment, the assignment channel, the
  event envelope the daemon relays from `act --json`, and `infrastructure-error`.
- **C3** — object-store URL signing under a CI key prefix, reusing
  `git-storage.hoon`.
- **H** — a fake-ship harness where every negative row below can go RED.

## §1 — MEASURED ground (verified on `9cec037`; re-verify, do not trust)

- `desk/desk.bill` lists `%urgit`, `%urgit-clay`, `%urgit-fileserver`. You add
  `%urgit-ci` as the fourth.
- `%urgit` persisted state is `state-4:git` (`desk/sur/git.hoon:428`);
  `on-load` (`urgit.hoon:2206`) migrates `%0→%4`. **Do not add `state-5`.**
  P0 changes `%urgit` in exactly two places (§3 D2, D3) and neither needs a
  new persisted field.
- `receive-policy-error` (`urgit.hoon:7861–7886`) is the only protected-ref
  policy today: release-tag lock, no-delete, fast-forward. It returns
  `(unit @t)`; a non-null is the `ng` reason. **This is where the CI gate hooks.**
- `storage-settings` (`urgit.hoon:7761`) scries the ship's `%storage` agent and
  returns `~` unless endpoint, access-key-id, secret-access-key, bucket, region
  are all non-empty AND `service == 'credentials'`. `git-storage.hoon` builds
  SigV4 requests from that. **CI signing reuses both; it adds a key prefix, not
  a second signer.**
- `on-peek` (`urgit.hoon:8951`) already exposes `[%x %state %version ~]` →
  `-.state`. `%urgit-ci` exposes the same route.
- Vectors are `%say` generators in `desk/gen/*-vector.hoon`, run from the dojo
  as `+urgit!<name>` and print `%.y` (or a noun for the codec one). README
  line 100. Yours follow that convention exactly.
- `zig build` (zig 0.15.2 on PATH) assembles the desk; `zig build
  -Ddesk=<pier>/urgit` installs it. Run `zig build` once before touching Hoon.
- The spike proved `act` 0.2.89 (`brew install act`, already on this host)
  emits, with `--json`, one JSON object per line carrying `job`, `jobID`,
  `stage`, `step`, `stepID`, `stepResult`, `jobResult`, `command` (with
  `set-output`/`summary`/`group`/`endgroup`), `kvPairs`, `raw_output`, `msg`,
  `time`. Sample lines are in
  `/var/home/michael/workspace/urbit/urgit/.scratch/spikes/001a-act/logs/structural-json.log`
  (read-only reference; copy what you need into your worktree).

## §2 — Derivations D1–D9 (the spine; cite these in your record)

**D1 — `%urgit-ci` agent, `state-0`, no migration ladder.**
`desk/app/urgit-ci.hoon`, `desk/sur/ci.hoon`. `+$ state-0` is
`[%0 candidates=(map candidate-id candidate) daemons=(map daemon-id daemon)
assignments=(map assignment-id assignment) attempts=(map attempt-id attempt)
ci-protected=(set [repo=@t ref=@t])]`
— exactly those four maps and one set, nothing else in P0. `on-load` accepts
only `%0`.
Cite: spec § Packaging ("Its persisted state starts at `%0`"); AGENTS.md
state-0 rule (current for new agents).

**D2 — Landing-eligibility scry, synchronous, fail-closed.**
`%urgit-ci` `on-peek` answers
`[%x %eligible repo=@t ref=@t oid=@t ~]` → `noun+!>(?)` — `%.y` only when
`oid` is the OID of a candidate in state whose `status` is `%passed` and whose
`(repo ref)` matches. Everything else, including "no such candidate" and
"agent not running", is `%.n`. `%urgit` calls it from a new arm
`ci-gate-error` placed immediately after `receive-policy-error`, invoked from
`receive-policy-error`'s caller for refs in `ci-protected-refs`. The scry is
`.^(? %gx /(scot %p our.bowl)/urgit-ci/(scot %da now.bowl)/eligible/<repo>/<ref>/<oid>/noun)`
under `mole`; `~` from `mole` is `%.n`. Cite: spec § Protected refs para 3
("`%urgit` scries `%urgit-ci` for eligibility… advances the ref only when the
candidate is eligible"); § Packaging ("`%urgit` scries `%urgit-ci` for landing
eligibility").

**D3 — CI-protected set lives on `%urgit-ci`, not on `repository`.**
`ci-protected=(set [repo=@t ref=@t])` is the set in D1. `%urgit` scries it:
`[%x %ci-protected repo=@t ref=@t ~]` → `noun+!>(?)`. `%urgit` gains NO field.
Reason: R6.1-A keeps CI records out of forge state, and the spec lists this as
an open question with both options — P0 picks the option that touches
`state-4` zero times. Cite: spec § Repository changes ("Either `repository`
gains a field or `%urgit-ci` holds the set and `%urgit` scries it") — you
build the second reading; say so in your record.

**D4 — Staged direct write: `%urgit` pokes `%urgit-ci`, never applies.**
When `ci-gate-error` finds the ref CI-protected and the OID not eligible,
`%urgit` (a) does NOT advance the ref, (b) keeps the pushed objects (they are
already in `staged`/`combined`; commit them to `objects.repo` without moving
the ref), (c) pokes `%urgit-ci` with
`%ci-action` `[%stage-candidate repo=@t ref=@t head=oid base=oid]`, and (d)
returns the `ng` reason `'staged as ci candidate <id>; checks pending'` where
`<id>` is the candidate id `%urgit-ci` returns. In P0 the poke is
fire-and-forget and the id is deterministic:
`(sham repo ref head base)` as `@uv`. Cite: spec § Protected refs para 6
("A direct push… is never applied. `%urgit` stages the pushed head as a
candidate and answers the push with `ng <ref> staged as candidate <id>`").

**D5 — Candidate materialization poke, `%urgit-ci` → `%urgit`.**
`%urgit` accepts `%git-action` `[%materialize-candidate repo=@t ref=@t
head=oid base=oid]` and answers by poking `%urgit-ci` back with
`%ci-action` `[%candidate-ready repo ref head base candidate=oid]` where
`candidate` is: `head` itself when `base` is an ancestor of `head` (fast-
forward), else the two-parent merge commit the existing PR-merge path already
know how to build (`merge-commit` in `desk/lib/git-tree.hoon:526`, called
from `urgit.hoon:7404`; reuse, do not duplicate).
in `%urgit`'s object store and **no ref moves**. A conflict answers
`[%candidate-conflict repo ref head base]`. Cite: spec § Protected refs para 3
("`%urgit` materializes the candidate without moving the ref… For a divergent
source, the candidate is the merge… For a fast-forward, the candidate is the
source head"); § Packaging ("Git objects never leave `%urgit`").

**D6 — Daemon enrollment and assignment channel, Eyre-bound under
`/apps/urgit/api/ci/`.**
`%urgit-ci` binds `/apps/urgit/api/ci` in `on-init` AND `on-load`
(unconditionally, like `%urgit` does at `urgit.hoon:2227–2229`). Routes in P0:
`POST /apps/urgit/api/ci/daemon/enroll` `{token}` → `{daemon-id}` (token is a
32-byte `@uv` the operator mints with `+urgit-ci!mint-enroll-token`, stored
hashed with `(shas %ci-enroll token)` — never stored raw);
`GET /apps/urgit/api/ci/daemon/<id>/assignment` long-poll, 25 s, answers
`{assignment}` or `204`; `POST /apps/urgit/api/ci/attempt/<id>/event` accepts
one relayed `act --json` line at a time, validated by D7;
`POST /apps/urgit/api/ci/attempt/<id>/result` `{job-result | infrastructure-
error}`. Authenticated ship session OR a daemon bearer token (the enroll
response) — the bearer is `(shas %ci-daemon <daemon-id> <token>)`, checked
constant-shape. Cite: spec § Execution para 3 ("The daemon opens the
connection. It authenticates, reports capacity, and waits. `%urgit-ci` selects
a daemon and sends the assignment over that connection. No inbound port is
required on the runner host").

**D7 — Event envelope: validate, bound, never trust.**
`desk/lib/ci-event.hoon` parses one `act --json` line into
`+$ event [job=@t job-id=@t stage=(unit @t) step=(unit @t) step-id=(list @t)
step-result=(unit result) job-result=(unit result) command=(unit command)
raw=? msg=@t at=@da]` where `result` is `?(%success %failure %skipped
%cancelled)` and `command` is `[%set-output name=@t value=@t] |
[%summary body=@t] | [%group name=@t] | [%endgroup ~] | [%other @t]`.
`msg` is truncated to 4 KiB; a line over 64 KiB is rejected with 413; an
attempt accepts at most 50,000 events, then 429. Unknown keys are ignored;
missing `job`/`jobID`/`time` is 400. A `set-output` is recorded on the
attempt's `outputs=(map @t @t)`; nothing else is persisted per event — the
event stream itself is NOT stored in Gall in P0 (it goes to the object store in
P5). Cite: spec § Execution para 2 (the emitted event list; "If `act` exits
without a `jobResult` event, the daemon reports `infrastructure-error`. It
never infers success from absence"); § Storage ("Only handles, sizes, hashes,
and completion state enter Gall state").

**D8 — `infrastructure-error` and the no-result rule.**
An attempt whose `/result` never arrives within `deadline` (a `@dr` on the
assignment; P0 default `~h1`) is closed `%infrastructure-error` by a Behn
timer `%urgit-ci` arms at assignment time. A `/result` of `job-result` with no
prior `jobResult` event on that attempt is rejected 409 — the daemon must have
relayed the event before it may claim the result. A candidate whose required
attempt is `%infrastructure-error` has status `%unknown`, never `%passed`.
Cite: spec § Execution para 2; § Storage ("A store outage yields `unknown`…
It never yields success").

**D9 — CI key prefix on the existing signer.**
`desk/lib/ci-storage.hoon` exposes `sign-put` and `sign-get` that call
`git-storage`'s existing SigV4 arms with the object key forced to
`ci/<repo>/<run>/<attempt>/<trust>/<name>` where `trust` is `?(%trusted
%untrusted)`. `%urgit-ci` refuses to sign a GET whose `trust` segment differs
from the requesting attempt's trust class. When `storage-settings` is `~`,
every sign arm returns `~` and `%urgit-ci` refuses `%set-ci-protected` with
`'ship object storage is not configured; CI cannot be enabled'`. Cite: spec
§ Storage paras 1 and 3.

## §3 — Build order (each stage builds; commit after each)

S1. `desk/sur/ci.hoon`: every mold in D1, D6, D7, D8. `desk/mar/ci-action.hoon`
    (poke mark, noun ⇄ json like `git-action`). `zig build`.
S2. `desk/lib/ci-event.hoon` (D7) + `desk/gen/ci-event-vector.hoon`: parse the
    68 JSON lines of the spike log (copy them into the vector as literal
    cords); assert `set-output suite=true` lands in outputs; assert a 65 KiB
    line is refused; assert a line without `time` is refused; assert an
    unknown top-level key is ignored. Vector prints `%.y`.
S3. `desk/lib/ci-storage.hoon` (D9) + `desk/gen/ci-storage-vector.hoon`: with
    fixture credentials, `sign-get` for `%untrusted` from a `%trusted`
    attempt returns `~`; same-class returns a URL whose path starts with the
    forced prefix; with `~` settings every arm returns `~`.
S4. `desk/app/urgit-ci.hoon`: `on-init`/`on-load` (state-0, Eyre bind),
    `on-peek` (D2, D3, `%state %version`), `on-poke` for `%ci-action`
    (`%set-ci-protected`, `%stage-candidate`, `%candidate-ready`,
    `%candidate-conflict`, `%mint-enroll-token`), the four D6 routes, the D8
    Behn timer. `desk/desk.bill` += `%urgit-ci`. `zig build`; install on the
    harness ship; `:urgit-ci +dbug` shows state `%0`.
S5. `%urgit`: `ci-gate-error` (D2/D4) wired into the receive path; the
    `%materialize-candidate` poke handler (D5). **Two arms and one case in
    `on-poke`; nothing else in `urgit.hoon` changes.** `zig build`; the
    existing `+urgit!git-migration-vector` still passes (you did not touch
    state).
S6. Harness (§4) — run every row; paste the table.

## §4 — Harness (one ship + the real `act`; binding)

Tooling: `/var/home/michael/workspace/urbit/bin/urbit` (4.6), pill
`/var/home/michael/workspace/urbit/pills/brass-408k-1.pill`, `zig` 0.15,
`act` 0.2.89 on PATH, rootless Docker on this host. **Boot in a herdr pane
you create and close by parsed id — never tmux.** Ship name, HTTP port and
pier path are in the launch footer. Do NOT boot `~zod ~nec ~bud ~wes ~bel
~bus ~syt ~dur ~wep ~ser` — other work runs those. Fake galaxies address
each other on Vere-derived Ames ports; never pass `-p`.

1. Boot the ship. `|new-desk %urgit`, `|mount %urgit`, `zig build
   -Ddesk=<pier>/urgit`, `|commit %urgit`, `|install our %urgit`. Confirm
   `.^(@ud %gx /=urgit-ci=/state/version/noun)` is `0`.
2. Create repo `ci-fixture`, one commit on `refs/heads/master`. Push a
   second commit from a local clone over Smart HTTP — it lands (not
   CI-protected yet). Record the OID.
3. **Before `%storage` is configured:** `%set-ci-protected ci-fixture
   refs/heads/master` → refused with the D9 message. Row H1.
4. Configure `%storage` via the Landscape settings agent poke (the harness
   script does this; fixture endpoint `http://127.0.0.1:1` is fine — no
   request is made in P0). Re-run step 3 → accepted. Row H2.
5. Push a third commit to `master` over Smart HTTP. **Expected: `ng
   refs/heads/master staged as ci candidate <id>; checks pending`; ref
   unchanged; objects present** (`.^` the repo's objects map size before and
   after). Row H3. `%urgit-ci` state has one candidate, status `%pending`.
6. Poke `%materialize-candidate` for that candidate → `%candidate-ready`
   with `candidate == head` (fast-forward). Row H4. Then push a diverging
   commit from a second clone → materialize → `candidate != head`, is a
   two-parent commit, both parents correct, ref still unchanged. Row H5.
7. Enroll a daemon: `+urgit-ci!mint-enroll-token` → POST enroll → daemon-id.
   Long-poll the assignment route → 204 (nothing assigned; P0 has no
   scheduler — assign by poke: `%assign candidate daemon`). Long-poll again →
   the assignment. Row H6.
8. **Real `act`:** on the host, `act push -W .github/workflows/<fixture>.yml
   -j <job> -P ubuntu-latest=catthehacker/ubuntu:act-latest --network bridge
   --json` against a checkout of the candidate OID; pipe each line to
   `POST /attempt/<id>/event` with a 5-line shell loop. The fixture workflow
   is `desk/tests/ci/fixture-pass.yml` (you write it: one job, one step that
   echoes `suite=true` to `$GITHUB_OUTPUT`). Assert `outputs.suite == 'true'`
   on the attempt. Row H7.
9. POST `/result` `job-result success` → candidate `%passed`. Push the
   candidate OID to `master` → **lands** (`ok refs/heads/master`). Row H8.
10. **Negative rows — each must be RED before its fix and GREEN after, or
    the harness cannot detect it:**
    - H9: POST `/result success` on an attempt with no `jobResult` event → 409;
      candidate stays `%pending`.
    - H10: let an assignment's deadline pass with no result → attempt
      `%infrastructure-error`, candidate `%unknown`; push the OID → still `ng`.
    - H11: `fixture-fail.yml` (step exits 1) → `jobResult: failure` → candidate
      `%failed`; push the OID → `ng`.
    - H12: POST an event to attempt A with attempt B's bearer → 401.
    - H13: POST a 65 KiB event line → 413; attempt unaffected.
    - H14: `|suspend %urgit-ci`, then push an OID that WAS `%passed` → `ng`
      (fail closed: `mole` on the scry yields `~`). `|revive`, push again →
      `ok`.
    - H15: sign a GET for `%untrusted` from the `%trusted` attempt → `~`.
11. Foreground before the final message: `+urgit!ci-event-vector`,
    `+urgit!ci-storage-vector`, `+urgit!git-migration-vector`,
    `+urgit!git-access-vector`, `cd fe && npm test` (fe is untouched in P0;
    the test proves that).
12. Shut down only the pier you booted, by PID verified through
    `/proc/<pid>/cmdline`. Never pattern-kill.

Table to paste, verbatim, in `.scratch/p0-live-table.md` (committed):

| Row | Expect | Observed | RED shown? |
|---|---|---|---|
| H1 … H15 | | | |

## §5 — Hard constraints (violating any fails the brief)

- NEVER touch `/var/home/michael/workspace/urbit/urgit` (the main checkout),
  any other worktree, `/var/home/michael/workspace/urbit/erpit`, or any pier
  you did not create.
- Do NOT push. Do NOT open PRs. Commit locally to `ci/p0-contracts`,
  frequently, prefixed `ci-p0:`.
- Do NOT redesign anything in `specs/native-ci.md`. Ambiguity, or a
  derivation that turns out to contradict the code → STOP and box it in
  `QUESTIONS-CI-P0.md` at the worktree root with the trace and a
  recommendation. The operator rules. Never improvise architecture.
- **Scope fence:** P0 only. No scheduler (assignment is by explicit poke), no
  daemon binary (the harness uses a shell loop around real `act`), no UI, no
  object-store upload, no `act --list` adapter, no credential store, no
  signing key, no checkpoint. If you find yourself writing any of those, stop.
- `%urgit` changes: exactly `ci-gate-error` + its call site, the
  `%materialize-candidate` handler, and nothing else. `desk/sur/git.hoon` is
  **not modified**. If landing P0 truly needs a third touch in `urgit.hoon`,
  keep it minimal and name it in the commit message.
- Durable scratch under `<worktree>/.scratch/`, never `/tmp` (tmpfs) or
  `$HOME`. Logs (`*.log *.err *.out *.json`) stay out of git; reports and
  scripts go in.
- Identifiers are never typed from memory: every arm name you cite in a
  commit message or box must exist on the tree (`grep -nF '++  <name>'`).
- Turn checkpoint: at turn 300 with rows unproven, commit what passes, write
  the live table with UNPROVEN rows marked, shut down the pier, and stop.

## §6 — Final message must contain

1. Commits (hash + subject), files created/modified, the state-0 shape (four maps, one set).
2. The verbatim scry forms for D2 and D3 as they work in the dojo.
3. The live table H1–H15 with observed results and, for each negative row,
   the RED output you saw before the fix.
4. Verbatim tails of the five foreground suites.
5. Boxes opened in `QUESTIONS-CI-P0.md` (or "none"); anything deliberately
   left for P1+ that a reader of the diff might mistake for an omission.
