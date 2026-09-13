# BRIEF — CI P0 close-out: harness fixes + carry-overs on `ci/p0-contracts`

You are a follow-up chair on a branch that already passed review. You are not
building anything new. You are making the branch's harness reproduce without the
original author present, porting two proven improvements from sibling branches,
re-running the live table on a fresh ship to prove it, and stopping. The merge
click is the operator's, not yours.

## Ground truth

- Worktree: `/var/home/michael/workspace/urbit/urgit-ci-p0`, branch `ci/p0-contracts`,
  HEAD `40a83cd`, base `9cec037` = upstream `master`. 11 commits ahead. Clean.
- Read in this order, all of it: `BRIEF-CI-P0.md` (the original brief; §4 harness
  and §5 scope fence bind you too), `specs/native-ci.md`, `.scratch/p0-live-table.md`
  (the author's record — it is RIGHT; the scripts drift from it), `.scratch/ci-p0/*.sh`.
- Sibling branches, read-only, for the two ports: 
  `/var/home/michael/workspace/urbit/urgit-ci-p0-opus/.scratch/harness/negatives.sh`
  (+ its mutant table at `.scratch/p0-live-table.md` lines 33–43 in that tree) and
  `/var/home/michael/workspace/urbit/urgit-ci-p0-astra/.scratch/p0-live-table.md`
  lines 207–243 (harness notes: rootless Docker; act output forms).
- The orchestrator's battery result that found the defects you are fixing:
  `/var/home/michael/workspace/urbit/urgit/.scratch/battery/fable/RESULT.md`.
  Every finding there is a defect in THIS branch's scripts. Read it first.

## Scope fence (binding)

- **No Hoon changes** except the one comment in T3. `desk/app/urgit.hoon`,
  `desk/app/urgit-ci.hoon`, `desk/sur/ci.hoon`, `desk/lib/*` are frozen. If a fix
  seems to need Hoon, stop and box it (§ format below).
- No new rows, no new routes, no scheduler, no daemon binary, no UI, no store upload.
- `.scratch/ci-p0/` and `.scratch/p0-live-table.md` are yours. `desk/tests/ci/` is
  yours only if a fixture needs a comment.
- Never touch the sibling worktrees. Never touch any pier except the one you boot.

## Tasks, in order

T1. **Harness reproduces cold.** Fix, in `.scratch/ci-p0/`:
  a. The dojo needs `=ci -build-file /=urgit=/sur/ci/hoon` before any
     `candidate:ci` / `attempt:ci` scry. Put it where it runs exactly once, early,
     and where a reader sees it (`setup.sh` or a new `prelude.sh` that `setup.sh`
     calls). Not `%/sur/ci/hoon` — in a fresh dojo `%` is `%base`.
  b. `h15.sh` must use `(scot %t 'cache.tar')` for the object name, as the table
     row does. A bare dotted knot hangs the dojo on a continuation prompt.
  c. `env.sh` must not carry a literal `+code`. Read it at boot into
     `$TMP/code.txt` and source from there.
  d. The boot step must wait for the shell prompt before `herdr pane run` of the
     urbit line (a fresh pane's MOTD eats the first line). Script the boot; the
     original brief left it manual.
  e. Any other step you find yourself typing by hand during T4 is a defect of the
     same class. Script it, note it in the table's "Deviations" section.

T2. **Port opus's mutant-build RED phase** as `.scratch/ci-p0/negatives.sh` (or
  split per row if that matches the existing per-row layout better — your call,
  say which). Semantics, exactly: for H9–H15, apply the seven one-line mutations
  from opus's table (`handle-result` 409 → `%.n`; `wake-deadline` → `%success`;
  `close-attempt` failure → `%passed`; `attempt-for` auth → `%.y`; `max-line-bytes`
  → 10 MB; `ci-gate-error` outage → fail open; `ci-storage` `allowed` → `%.y`) as a
  single build, run every negative row and assert each FAILS (RED), revert with
  `git checkout --`, rebuild, run every negative row and assert each PASSES
  (GREEN). The mutations are applied by the script and reverted by the script;
  they are never committed. Adapt arm names to THIS branch's code — opus's names
  may differ; find the equivalent guard by reading, not by grepping the name.
  Each row prints `PASS (observed: …)` / `FAIL (observed: …, expected: …)`.

T3. **Two notes from astra**, verbatim intent, your words:
  a. In `.scratch/p0-live-table.md` "Deviations": the runner host's default Docker
     socket may be rootful; the battery ran under a rootless daemon
     (`docker info` → `name=rootless`). R3.1-A hygiene; the VM wrapper is what
     makes either acceptable in production.
  b. In `desk/lib/ci-event.hoon` near the existing `kvPairs` comment (~line 101):
     one line stating that act 0.2.89 emits `$GITHUB_OUTPUT` file-command outputs
     with top-level `name` and legacy stdout `::set-output::` with `kvPairs.name`,
     and the parser accepts both. That is the ONLY Hoon edit permitted.

T4. **Battery, cold.** Boot a fresh ship from your launch footer. Run the harness
  from the scripts only — `setup.sh` through `h15.sh`, then `negatives.sh` RED and
  GREEN phases, then the four foreground vectors and `cd fe && npm test`. If you
  type anything into the dojo that a script did not, go back to T1e. Update
  `.scratch/p0-live-table.md`: same 15 rows, observed columns refreshed from this
  run, a new "Mutant RED phase" section with the seven PASS/FAIL pairs, ship and
  pier from your footer, shutdown by `/proc/<pid>/cmdline`-verified PID.

T5. **Commit.** One commit per task (T1, T2, T3, T4) with messages in the existing
  `ci-p0: …` style. Do NOT rebase, squash, or touch the 11 existing commits. Do
  NOT merge to `master`. Do NOT push. Leave the branch clean and the pier on disk.

## Boxes

If blocked, write `QUESTIONS-CI-P0.md` with `## §<n>` headings, one question per
section, what you tried, what you would do under each answer. Then stop. Do not
guess past a box.

## Ordering-hazard note (do not "fix")

CI-protecting a ref with no tip refuses the seed push
(`ci-protected branch has no tip to stage a candidate against`). That is ruled
behaviour (CI-EMPTY-REF-1-A, P1 scope). Your `setup.sh` must seed the repo BEFORE
`%set-ci-protected` and say so in a comment. Do not change the gate.

## Launch footer

- Ship: `~sev`, HTTP port `8345`, pier `/var/home/michael/piers/urgit-ci-p0-sev`
  (must not exist before boot). Boot line, inside a herdr pane you create and
  close by parsed id, after the shell prompt appears:
  `/var/home/michael/workspace/urbit/bin/urbit -F sev -B /var/home/michael/workspace/urbit/pills/brass-408k-1.pill --http-port 8345 -c /var/home/michael/piers/urgit-ci-p0-sev`
- Do NOT boot `~zod ~nec ~bud ~wes ~bel ~bus ~syt ~dur ~wep ~ser ~ryx ~wyd ~tem ~mul ~dev`.
- `act` 0.2.89 on PATH; image `catthehacker/ubuntu:act-latest` is cached
  (`--pull=false`); `--network bridge`.
- Reports: `.scratch/p0-live-table.md` (committed). Logs: `.scratch/tmp/` (ignored).
