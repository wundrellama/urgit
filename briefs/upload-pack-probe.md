# Urgit: answer git's flush-only probe on the `upload-pack` path

## Status before you start

Five fixes have landed on this branch. **Read `PUSH-DEFECTS.md` first** — its §3
is the same defect on the push side, and its fix is the template for this one.
Then read `GZIP-REQUEST.md` §6, which is where this defect was recorded.

- `22f2f77` — `join-all` made linear.
- `bf3dbea` — `apply-delta` made linear.
- `7c86699`, `f70e6a8` — two `receive-pack` defects. **`f70e6a8` is the model.**
- `b53e06f` — `copy-instruction` made cheap.
- `74ad8c0` — gzipped request bodies decoded.

**Do not re-measure any of them.** Do not modify `git-codec.hoon`,
`git-delta.hoon`, `git-pack-decode.hoon`, or `git-gzip.hoon`.

This is a small brief. The fix is close to mechanical; the verification is the
work.

## The defect

`handle-upload-pack` (`desk/app/urgit.hoon:7757`) has no equivalent of
`receive-probe:git-protocol`, so it answers git's 4-byte `0000` probe with
**HTTP 400 `invalid upload-pack request`**.

When a request body exceeds `http.postBuffer`, git sends a probe request first —
a single flush packet — to check auth and readiness before committing to a large
upload. `f70e6a8` fixed exactly this on `receive-pack`. The fetch side was never
fixed because no test reached it.

### Already measured — do not re-litigate that it exists

`GZIP-REQUEST.md` §6 recorded it two ways on `git` 2.55.0:

- raw HTTP with a 4-byte body → `HTTP/1.1 400`
- a v0 clone with `http.postBuffer=1024` POSTs 4 bytes, gets 400, and dies

At default settings it needs an `upload-pack` request above 1 MiB — roughly
**21,000 refs** — which is why no run in this series hit it naturally.

### Why it is not a duplicate of the gzip fix

They are mutually exclusive paths, established in `GZIP-REQUEST.md` §1: below
`http.postBuffer` git gzips the body; above it git switches to the probe +
chunked path and sends the body **uncompressed**. The gzip fix cannot cover
this, and this fix cannot cover gzip.

## Deliverables

### D1 — Reproduce it

Boot your own pier and capture the 400 both ways: raw HTTP with a 4-byte body,
and a real `git clone` with `http.postBuffer` low enough to trigger the probe.
Record the `git --version`.

Do it on **both protocol v0 and v2**, and say whether they differ. Note that
`GZIP-REQUEST.md` §1 found `http.postBuffer` under 65516 makes git 2.55 abort
with `BUG: remote-curl.c:1533` on v2 — work around that, and say how.

**If it does not reproduce, stop and say so.**

### D2 — Fix it

Mirror `f70e6a8`. The probe answer goes **after** the authorization check and
**after** `decoded-body`, and **before** `v2-command` — a flush-only body must
never reach the v2 dispatch or the parser.

Return **200** with `content-type: application/x-git-upload-pack-result`,
`cache-control: no-store`, and an empty body.

**One judgment call, stated with a preference.** `receive-probe:git-protocol` is
already generic — it tests "one flush packet and nothing after it" and knows
nothing about `receive-pack`. Prefer **renaming it to something neutral**
(`flush-only-body`, or pick a better name and use it consistently) and calling
the one arm from both handlers, rather than duplicating the logic. The rename is
a pure refactor with no behavior change, and the receive path has both a
conformance vector and live coverage to catch a slip.

If you judge the rename not worth the churn, adding a second arm is acceptable —
but say which you chose and why. **Do not leave two copies of the same three
lines.**

### D3 — Answer the semantics question in the report

A flush-only `upload-pack` body is not only a probe. In protocol v0, a client
that decides it wants nothing also sends a bare flush. Both cases want the same
answer, so this does not change the fix — but **state in the report which cases
you believe reach this arm, and whether 200-with-empty-body is correct for each**.

Check what stock `git-http-backend` does with a flush-only `upload-pack` body and
say how you checked. Do not guess at the status line.

### D4 — Fail closed, and do not widen what the handler accepts

- A body of `0000` **plus trailing bytes** is not a probe and must still be
  parsed as a request, exactly as today.
- An empty body must still return 400 `missing upload-pack request`.
- A malformed body must still return 400.
- **A normal request must take exactly the path it takes today.** This is the
  regression that matters.

Authorization must still run first: an unauthenticated probe against a private
repository must return 401/403, not 200.

### D5 — Prove it

- **`git clone` succeeding with `http.postBuffer` set low enough to force the
  probe**, on both v0 and v2, with `git rev-list --objects --all | sort |
  sha256sum` matching the source and `git fsck --full` clean.
- A normal clone at default settings still works — both the gzipped many-ref
  case from `GZIP-REQUEST.md` §5 and a small plain one.
- A clone of a repository large enough to trigger the probe **at default
  settings** if you can build one. If ~21,000 refs is impractical, say so and
  rely on the forced case — but say plainly which you did.
- `git push` still works, both the multi-ref and >1 MB cases from
  `PUSH-DEFECTS.md` §3. If you took the rename, this is what proves you did not
  break the receive path.
- All conformance vectors in `desk/gen/` pass. Use the mapping in
  `GZIP-REQUEST.md` (19 vectors) — it supersedes `PUSH-DEFECTS.md` §4c, which
  supersedes `JOIN-ALL-PERFORMANCE.md` §4b.

### D6 — Report

`UPLOAD-PACK-PROBE.md` at the repository root. Short is fine — this is a small
fix and the report should match. Root cause, the fix and which naming choice you
made, the D3 semantics answer, D4 results, D5 evidence, and a section titled
**"What I could not measure and why."**

## Fences

- **Other projects have live piers.** `~/piers/fakezod` and `~/piers/fakenec`
  (ports 8080/8081, tmux sessions `fakezod`/`fakenec`), and `t4-opus-zod`,
  `t4-sol-zod`, `t4-fable-zod` on ports 31411-31413. **Do not touch them.**
- **`~/piers/joinall-bus` is RUNNING on port 8093 (PID 2519792)** with the
  `74ad8c0` build installed. Reuse it only if you scry-confirm its source first
  and record its PID before stopping it. Otherwise boot your own on 8094+.
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only what you started, by a PID you recorded.
- Ports 8000, 8080-8084, 8090, 8093 are taken. 8094+ is free.
- **This project ships nothing outside `desk/`.** No kernel edits.
- Do not touch `desk/sur/git-peer.hoon` or the `%archive` and `%rate` paths in
  `desk/app/urgit.hoon` — a parallel task owns those.
- **Verify running source by scrying, never by reading the mount.** Use
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/git-protocol/hoon)` and
  grep for a token unique to your change, **immediately after your first
  install**.
- **Never advertise a Git capability before its behavior is implemented**
  (`AGENTS.md`).
- Commit on `feat/progress-and-joinall`. **Do not push.**

## Known adjacent issues — not yours

- A 148 MB clone bails `meme` in a 2 GB loom; succeeds at `--loom 32`. Raise the
  loom if you hit it, note it, do not chase it.
- `sha1-octs` reverses 206 MB to feed `sha-1l` big-endian bytes, costing 590 ms
  against SHA-1's 187 ms. The fix is a kernel arm plus a Vere jet — out of scope
  for this desk. Do not attempt it.

## Escape hatch

A run that stops to ask a real question is a success. Earlier in this series the
`apply-delta` run stopped at D1 because the orchestrator's cost model was wrong,
and that was worth more than a fix.

If the defect does not reproduce, if 200-with-empty-body turns out to be the
wrong answer for a flush-only `upload-pack` body, or if the rename would break
something you cannot cleanly verify — write `QUESTIONS-PROBE.md` naming the
problem, the options, and what each breaks. Then stop.

Do not report a number you did not measure.
