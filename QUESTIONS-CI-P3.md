# QUESTIONS — CI P3 (opus chair, branch `ci/p3-opus`)

Each section is read / tried / would-do. §1 and §3 are the boxes BRIEF-CI-P3 §6 said to expect and
answers in the same sentence; each is recorded with the answer the build took. §2 and §4 were
open boxes: the build stopped on them at the checkpoint the relay named (2026-09-19 ~00:25), with
S0 committed and the S1 tree uncommitted, and rider 2 (`98f7caf`, 10:50) ruled both; the rulings
and what they changed are at the head of each section, the question as it was asked below them.

## §1 Eyre's channel carries a `%fact` from `%urgit-ci` to a page `%urgit` serves

**Read.** `fe/src/channel.js` builds the subscribe action as `{ship, app, path}`; Eyre's channel
subscribes the session to any agent by name — the page's origin is irrelevant. `%urgit`'s
`[%peer %activity ~]` fact path (`urgit.hoon:9347`) is consumed exactly this way by `App.jsx:115`.

**Tried.** Nothing yet: the channel is S3's, and the build stopped in S1. The row shape is
written (`.scratch/ci-p3/lib.sh` `channel_open`/`channel_facts`: `PUT /~/channel/<id>` with
`{"app":"urgit-ci","path":…}` then the SSE `GET`), not run.

**Would do.** As the brief says: it can. `%urgit-ci` gains `on-watch` on `[%ci %repository @ ~]`
and `[%ci %runners ~]` (`?> =(our src)`); the CI tab and the Runners section subscribe through
`watchAgent` with `app: 'urgit-ci'`. Not built.

## §2 The poll-interval constant behind the Runners pip's `stale` — RULED (rider 2, `98f7caf`)

**Ruling.** ONE number: `stale-after` (~m5), the scheduler's own window; `healthy-within` struck.
The busy-daemon finding is fixed at the source, alternative (c): the daemon polls at capacity too
(the poll is a heartbeat; `select-daemon` already refuses a full daemon), so `last-seen` is always
liveness; assumption 2 ("healthy when running ≥ capacity") STRUCK — a dead daemon with a full slot
set reads `stale`. Now D3 + D6(f). **Applied:** `runner-state` reads `stale-after`;
`GET ci/runners` answers `staleAfter`; `fe/src/runners.js` re-derives on it with no capacity
override; `daemon.go` `Run` polls without taking a slot and queues an over-capacity `%assign` for
one; a revocation cancels running work. The section below is the record of the question as asked.


**Read.** D3: `healthy` is "seen < 2× poll interval", `stale` "seen ≥ 2× — the CI-DELIVERY-1
re-offer window". The daemon's long-poll window is `poll-window ~s25` (`urgit-ci.hoon`); the
daemon re-polls at once after each 204, so an idle daemon touches `last-seen` at least every
25 s. CI-DELIVERY-1's re-offer window is `redeliver-after ~m2`. The scheduler's cut-off is
`stale-after ~m5`. These are three numbers, so "2× poll interval" (50 s) and "the CI-DELIVERY-1
re-offer window" (2 min) name different thresholds, and neither is the scheduler's.

**Tried.** Read only: `daemon.go` `Run` takes a slot before it polls, so a daemon at capacity does
not poll at all; its `last-seen` moves only when it relays an event (`handle-event` touches the
daemon). A daemon running a quiet job at capacity therefore ages past any `last-seen` threshold
while alive. Not measured on a live pair.

**Assumptions the working tree carries (not ratified; one line each to change or strike):**

1. `healthy-within = (mul 2 poll-window)` = 50 s in `urgit-ci.hoon`, answered as `healthyWithin`
   by `GET ci/runners`, and used by `runner-state` and by the client's re-derivation
   (`fe/src/runners.js` `runnerState`).
2. `runner-state` reads `healthy` when `running ≥ capacity` regardless of `last-seen`. **The relay
   is right that this does not prove liveness:** a daemon that dies with a full slot set keeps its
   `running` set until its attempts close (the deadline path, or CI-DELIVERY-1.1's re-offer), so it
   would read `healthy` while dead. It was a guess at the busy-daemon problem above, not a
   derivation. Alternatives for the ruling: (a) strike it — `stale` for a busy quiet daemon, with
   the hint text saying why; (b) keep it but bound it — `healthy` at capacity only while
   `last-seen` is within `stale-after` (~m5), the same window the scheduler trusts; (c) have the
   daemon poll (or `HEAD` a heartbeat) even at capacity, so `last-seen` is always liveness —
   a daemon change outside D3's text.
3. `stale-after ~m5` (the scheduler's window) untouched.

**Ask.** Which threshold is "stale" for the pip, and does the scheduler's `stale-after` follow it?

## §3 `%rotate-ci-key` re-signs the certificate

**Read.** `%rotate-ci-key` (`urgit-ci.hoon`, handle-action) already re-certifies with the ship's
signing pair when it holds one: `=?  signing  ?=(^ ship-keys)  (certified signing u.ship-keys)`.

**Tried.** Not yet on the live pair (R5 is written, not run). Q12 of the P2 record verified the
same arm's certificate with Go's `crypto/ed25519`; R5 repeats that check after the rotation.

**Did.** As the brief says: it must, and it does. No change beyond the allow-list entry (the
working tree's `parse-web-action` admits `rotate-ci-key`).

## §4 The S1b fence and the scheduler's re-offer rules — RULED (rider 2, `98f7caf`)

**Ruling.** (i) `desk/lib/ci-plan.hoon` and `desk/gen/ci-plan-vector.hoon` are in the fence; 4a
stands. (ii) The matrix refusal is retained; `runs-on` literal forms only, a `${{ }}` expression
refused at plan time with `runs-on expression unsupported in P3`; 4b stands. (iii) All three
re-offer readings stand as the S0 spec text has them; now D6(e). **Applied:** `validate:ci-plan`
refuses an expression label (`has-expression`); `ci-plan-vector` gains the runs-on/timeout parse
case, two expression refusals and the zero-timeout case. The section below is the record of the
question as asked.


The relay reports astra's open S1b boxes: whether `desk/lib/ci-plan.hoon` and
`desk/gen/ci-plan-vector.hoon` are in the P3 fence, and whether P3 retains the explicit matrix
refusal. This tree touched the first and left the second alone; both are recorded here as
assumptions, not settled.

**Read.** BRIEF-CI-P3 §3 fences `desk/app/urgit-ci.hoon`, `desk/sur/ci.hoon` (`revoked`,
`labels`, `repos`; `runs-on` on the plan's job; the action union) and `desk/mar/ci-action.hoon`
as "yours"; it does not name `desk/lib/ci-plan.hoon`. But the plan's job is *parsed* in
`ci-plan.hoon` (`wire-job`, `parse-job`, `validate`), and D2b says "the plan step records each
job's `runs-on` as a set" — the field cannot reach `job:ci` without that parser.

**Did (assumption 4a).** `desk/lib/ci-plan.hoon`: `wire-job` gains `runs-on=(set @t)` and
`timeout=(unit @ud)`; `parse-job` reads `runs-on` (a string is a one-element set, a list its
elements) and `timeout-minutes` (a natural number); `validate` copies both onto `job:ci`.
`desk/gen/ci-plan-vector.hoon` untouched (its `job-json` fixtures carry neither key; the parser
defaults both, so the vector still prints `%.y` — to be confirmed in the foreground, not run yet).
If the ruling keeps `ci-plan.hoon` outside the fence, the two fields have to be parsed in
`urgit-ci.hoon`'s `handle-plan` from the raw JSON after `validate` — a second pass over the same
body; doable, uglier.

**Did (assumption 4b).** The matrix refusal is retained as it was: `validate` still refuses
`matrix: true` with `'matrix unsupported in P1 (…)'`. D2b's "the matrix form is expanded as act
already does" was read as describing `runs-on`'s matrix *form*, not as lifting the P1 refusal.
Not settled here.

**Did (assumption 4c — the re-offer rules, CI-DELIVERY-1.1, in the S0 spec text and the working
tree).** The brief's D6 leaves three things unsaid that the code had to decide:

- *An abandon with no other daemon to take the job.* "Re-offers the attempt immediately to any
  OTHER eligible daemon" — when none exists the tree closes the attempt as before
  (`%infrastructure-error 'abandoned: <reason>'`), because P1's P10 row asserts exactly that on a
  single-runner ship and R13 requires the P1 battery to pass unchanged. "Exists" here is a live
  daemon that could take the job at all, capacity aside (`other-daemon-exists`): a busy other
  daemon means the job waits for it rather than failing.
- *"The job's own timeout".* Read as the job's `timeout-minutes` (ERPit declares 5/10/45/60); the
  assignment's deadline becomes `timeout-minutes + 2 min` when declared, `~h1` otherwise. At that
  deadline the attempt is re-offered once on another daemon when one exists, then closed
  `'runner went silent'`; with no other daemon it closes with P1's `'no result arrived before the
  deadline'` (P11's string).
- *An assignment never fetched.* The T7 ghost (a stopped daemon's fresh record given work it never
  fetches) is not in D6's list. The tree adds `reoffer-unfetched`: an undelivered assignment whose
  daemon's record is stale, refused or revoked is re-offered on another daemon when one exists.
  R4's first run showed it working by accident (below) — and showed the harness needs it least: a
  row retires its test daemons (revoke + remove) instead of leaving ghosts.

All of 4c is spec text in the S0 commit (`f920976`, §Recovery › §Runner loss). If the ruling
strikes any of it, that paragraph changes with the code.

**Ask.** (i) Is `desk/lib/ci-plan.hoon` in the fence (and `ci-plan-vector.hoon`, if its fixtures
should carry `runs-on`)? (ii) Does P3 keep the matrix refusal? (iii) Do the three re-offer readings
above stand?

## §5 `%urgit`'s six `=(^ pending-clay)` guards are never true — outside the fence

**Read.** `desk/app/urgit.hoon` guards a second Clay operation with
`?:  ?|(=(^ pending-clay) =(^ pending-publish))` at six sites that predate P3 (`4b0c782`,
`ca0d139`, `9fab1f5`, August; lines 2667, 6593, 6684, 7536, 7740, 7763 today): the peer push into
a bound repository, the web merges, the API's bind/publish paths. A bare `^` in `.=` is not a
pattern test; it is a wing (the dojo on `~sud`: `=(^ `(unit @)`~)` → `%.n`, `=(^ [~ 1])` → `%.n`,
`=(^ [0 0])` → `%.n`), so the comparison is false whatever `pending-clay` holds and the guard
never fires. The working form is `?=(^ …)` (line 4076, `%publish-desk`, uses `?^`) or the loobean
`!=(~ …)`.

**Tried.** D9's linked landing (S8, `578515a`) copied the six-site pattern for its own guard, and
the cold run's P19 found it: two candidates on one linked ref passed two seconds apart; the second
landing parked over the first's clay-push (its `pending-clay` and `pending-ci-land` overwritten
between the ack and the report), the first's report found no result and dropped both, the desk
held the first's tree at a new revision, master did not move, and both candidates stayed
`%passed` with no verdict reason. Reproduced on demand (`.scratch/tmp/p3-r14-fix.log`'s race block,
R14): under the copied guard one landed and the other vanished; under `!=(~ …)` the second is
refused `linked desk update already in progress; re-run the candidate to land it` and the first
lands (`b6199e9`; R14's race block and the R14 mutant of group E prove it both ways). The
consequence at the six pre-existing sites is the same shape: a second peer push, web merge or
publish into a bound repository while one is parked overwrites the first's clay-push, and the
first's requester is answered by the second's result or by nothing.

**Did.** Fixed the one site D9 owns (in the fence: the receive tail) and left the six alone: the
§3 fence admits `urgit.hoon` for D9's receive tail only. Nothing in P3's rows or in the P0–P2
batteries exercises two concurrent Clay operations at those sites, so the cold run does not
depend on this box; it is recorded for the pick.

**Ask.** Should the six pre-existing guards take the same one-token change (`!=(~ …)`) in P3 —
seven sites in one commit, one row proving a concurrent linked peer push refused — or stay for
the owner of `%urgit`'s receive path? The chair proceeds without it.
