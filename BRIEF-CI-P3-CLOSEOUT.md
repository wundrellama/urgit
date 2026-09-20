# CI P3 close-out brief

The pick is `ci/p3-opus` at `01e668a` (ratified 2026-09-20). You are the close-out chair on a
branch cut from it. The operator's fresh-pair battery already ran this tree P0 → P3 clean in one
pass (`.scratch/battery/p3-verify-opus/cold.log`, copied to `.scratch/p3-verify-opus.cold.log`
in your worktree; read its phase lines before anything). Nothing here changes what P3 built. The
job is: fold in three things the other chair did better, close two small gaps the batteries
exposed, scrub for the operator's privacy rule, and produce the merge-ready record. Read
`BRIEF-CI-P3.md` (five riders at the end), `QUESTIONS-CI-P3.md`, `.scratch/p3-live-table.md`,
then the astra sources named below (a second worktree, read-only:
`/var/home/michael/workspace/urbit/urgit-ci-p3-astra`).

## Tasks, in order, one commit each (`ci-p3-closeout: T<n> — …`)

**T1 — The watch-authorization row (from astra).** `%urgit-ci`'s `on-watch` refuses a
non-`our` subscriber (`urgit-ci.hoon`, the `?> =(our.bowl src.bowl)` at the CI paths), and no
P3 row proves it over the wire. Astra's `.scratch/ci-p3/s3-watch-auth.py` does: a session on
the SECOND ship opens an Eyre channel and subscribes to the first ship's `/ci/runners` and
`/ci/repository/<name>` with `ship: <first>`; the subscribe must come back with `err` and the
channel must never deliver a `diff`. Port it into this tree's harness as row **R6a** (reads the
footer env — no literal ports; astra's copy has `127.0.0.1:8381` and `code2.txt` hard-coded,
which is exactly what you must not carry), with a RED tripwire: remove the `?>` guard →
`foreign ship watch accepted` fires; restore → refused. Both paths. Add the row to the record.

**T2 — The ship-side scrub bypass check (from astra).** This tree's R15 proves a two-line PEM
credential is masked line by line in the daemon's relayed events. Astra's `s8-live.py` `R15SHIP`
goes one step further: it posts RAW events to the ship's event route, bypassing the daemon
entirely, and asserts the SHIP's own scrub masks every line before persisting — so a modified or
malicious daemon cannot leak a credential line into the log the ship stores. Port that assertion
as **R15b** into `r14-r16.sh`'s shape (footer env, both paths, a RED that disables the ship-side
scrub and observes a raw PEM line persisted). If the ship does not scrub independently of the
daemon today, that is a product gap: fix it in `urgit-ci.hoon`'s event handler (in fence) and
say so in the commit body — the brief's D9 says "on daemon and ship".

**T3 — The record's shape (from astra).** Astra's `p3-live-table.md` keeps a `## Historical
shutdown at the §<n> stop (superseded by Rider <m>)` section for every stop-and-resume, each with
its own pid table, so a reader can see what was up at each box. This tree's record has one
`## Deviations` list. Restructure: keep `## Commits`, `## Table`, then one `## Stop <k>` section
per stop of the cold run (18:54 P19, 21:18 Q12, the START_AT leak, the R5 count) with what was
observed, what was re-anchored, and the pids at that moment, then `## Final shutdown` (the
battery's `/proc` pids), then `## Deviations` for everything else. Add a `## Fresh-pair
battery` section quoting the operator's cold log phase lines (start 01:45:08, end 07:10:32,
every `== red:`/`== green:`/`== rows:` line) so the record carries the clean-gate verdict, not
only the chair's own run.

**T4 — Privacy scrub for the upstream deliverable.** `git diff c8007be..HEAD -- desk runner
fe` adds three lines the operator's rule forbids in shipped code: two code comments cite the
ledger id `CI-P2-SECRET-1` (`desk/…` and `runner/…` — find them with `grep -rn 'CI-P2-SECRET-1'
desk runner fe`), and the README names GroundSeg. The comments: replace the id with the rule in
words ("every line of every released value is scrubbed on both sides"). The README line stays —
one GroundSeg mention was ruled allowed (D8) — but make it the only one (`grep -c` = 1). Then the
full sweep over the same diff for `tellurian|startram|herdr|\.scratch/battery|CI-P[0-9]-[A-Z-]+-[0-9]|ruling [0-9]|dinnyt|fwdesktop|michael` must return only that README line. Commit.

**T5 — The harness's own portability.** The fresh-pair battery found nothing hard-coded in this
tree's P3 drivers (the reason the pick fell this way), but `r17.sh:62` carries `~tug` in a
comment and the P0–P2 harness copied from earlier phases still has one bare-`head` dojo form
somewhere in astra's lineage — verify this tree's `grep -rnE 'dojo_value "head [a-z-]+:'
.scratch/ci-p[0-3]` is empty and `grep -rnE '839[0-9]|\bsud\b|\btug\b' .scratch/ci-p3 --exclude
env.sh` names only comments. Fix any real hit. One commit even if it is comment-only.

**T6 — Cold battery, once, on a fresh pair, from your worktree.** Your footer pair (below).
`.scratch/ci-p3/cold.sh` unmodified except your T1/T2 rows now in `battery.sh`'s step list.
Expected: `cold.sh: every step ran`. If a step stops, diagnose from the retained ships, fix the
cause (harness or product, say which), commit, and re-run from the top — the record keeps every
attempt as a `## Stop` section (T3's shape). A pass on a resumed run is a qualified pass and is
recorded as such; the final line of the record says which kind you got.

**T7 — The record and the done banner.** `p3-live-table.md` complete in T3's shape with T6's
verdict; `QUESTIONS-CI-P3.md` untouched except a final `## Close-out` paragraph naming T1–T6 by
commit; `git status` clean; both piers shut down by `/proc` pid (the `## Final shutdown` table);
print the banner.

## Fence

`desk/app/urgit-ci.hoon` (T2's event handler only, if needed); `.scratch/ci-p3/` and
`.scratch/p3-live-table.md`; the two comment lines and the README line T4 names. Nothing else in
`desk/`, `runner/`, or `fe/`. No push, no merge, no rebase, no `/tmp`. Box in
`QUESTIONS-CI-P3.md` (`## §<n>`) and stop on anything outside this list; a box is a successful
outcome. Provider-safeguard rule from BRIEF-CI-P3 §6 applies (the mutant phases have tripped it
on both providers; a second fire = commit what is green, record the row `provider-blocked, not
run`, stop).

## Launch footer — closeout

- Worktree `/var/home/michael/workspace/urbit/urgit-ci-p3-closeout`, branch `ci/p3-closeout`,
  base `01e668a`.
- **First ship** `~mex`, HTTP port `8420`, pier `/var/home/michael/piers/urgit-ci-p3c-mex`, tmux
  session `ci-p3-closeout-mex`. **Second ship** `~ryt`, HTTP port `8421`, pier
  `/var/home/michael/piers/urgit-ci-p3c-ryt`, session `ci-p3-closeout-ryt`. Rootless Docker state
  dir `/run/user/1000/ci-p3-closeout/`, data root `<worktree>/.scratch/tmp/docker-data`. Store
  (RustFS) port `8422`, container `urgit-ci-store-mex`, data `<worktree>/.scratch/tmp/store-data`.
  Rewrite `SHIP/PORT/PIER` (+ `SHIP2/PORT2/PIER2`) in `.scratch/ci-p0/env.sh` and
  `DOCKER_STATE`/`STORE_PORT` in `.scratch/ci-p1/env.sh` before your first boot. Neither pier
  may exist before it. Boot as `cold.sh` does (tmux, `--loom 34`, no `-p`).
- The P3 phase advertises the store on `192.168.1.229` (this host's LAN address) through
  `P3_STORE_ADVERTISE` as `cold.sh` already does; R7's second-machine fetch runs over `ssh np`
  (`192.168.1.64`, `curl` only, nothing written there).
- Do NOT touch ports `8340–8392`, any `urgit-ci-store-*` container not named above, any pier
  under `/var/home/michael/piers/` not named above, or `/run/user/1000/` state dirs other than
  yours. The other P3 worktrees are read-only sources.
- ERPit workload: clone `/var/home/michael/workspace/urbit/erpit` (read-only) as the harness
  does; never push to it. `act` 0.2.89 via `.scratch/ci-p1/act-static.sh`; `go` 1.27; `zig`
  0.15.2 on PATH (`/home/linuxbrew/.linuxbrew/bin`).
