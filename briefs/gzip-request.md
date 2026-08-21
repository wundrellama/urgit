# Urgit: accept a gzipped `upload-pack` request body

## Status before you start

Four fixes have landed on this branch. Read the reports — this brief is the
mirror image of one of them.

- `22f2f77` + `JOIN-ALL-PERFORMANCE.md` — `join-all` made linear.
- `bf3dbea` + `QUESTIONS.md` — `apply-delta` made linear.
- `7c86699`, `f70e6a8` + `PUSH-DEFECTS.md` — two `receive-pack` defects. **Read
  this one closely.** It is the same class of defect on the push side, and its
  shape is the shape of this fix.
- `b53e06f` + `RESOLVE-ENTRIES-PERFORMANCE.md` — `copy-instruction` made cheap.
  **Its §6 is where this defect was recorded.**

**Do not re-measure any of them. Do not modify `desk/lib/git-codec.hoon`,
`desk/lib/git-delta.hoon`, or `desk/lib/git-pack-decode.hoon`.**

## The defect

`git clone` of a repository with enough refs returns **HTTP 400
`invalid upload-pack request`**.

When the `upload-pack` request body grows past a threshold, `git` sets
`Content-Encoding: gzip` and compresses it. `parse-upload-request`
(`desk/lib/git-protocol.hoon`) calls `de-pkts:git-codec` on the raw body,
reads the gzip magic bytes as a pkt-line length, and returns `~`.
`handle-upload-pack` (`desk/app/urgit.hoon:7772`) answers 400.

Nothing anywhere in `desk/` reads the `content-encoding` header. Confirmed by
the orchestrator: `grep -n "content-encoding" desk/` returns nothing, and
`handle-upload-pack` goes straight from the empty-body check to
`v2-command:git-protocol` on `u.body.request.req`.

### Evidence already in hand

The `resolve-entries` run recorded the body with a proxy
(`RESOLVE-ENTRIES-PERFORMANCE.md` §6):

- **1,438 bytes on the wire, 3,023 bytes after `gzip.decompress`**
- a well-formed protocol v2 `fetch` command with **58 `want` lines**
- **both protocol v0 and v2 fail the same way**

The one-ref round trip in that report's §4c succeeds because its request stays
under the threshold. This is why every test so far has passed while a real
clone of a many-ref repository fails.

**Do not re-litigate whether the defect exists. Do confirm the exact trigger** —
see D1.

## Deliverables

### D1 — Reproduce, and find the actual trigger

Boot your own pier, push a repository with many refs into it (the `receive-pack`
fixes make a multi-ref push work with no `-c` flags now), and clone it back.
Capture the 400.

Then establish **what actually triggers the gzip**, by measurement, not by
reading git's source and guessing:

- Is it a byte threshold on the request body? What is it, on the `git` on this
  machine?
- Does it depend on `protocol.version`?
- Does it depend on `http.postBuffer`, as the push-side probe did?

Record the `git --version` you tested against. State the threshold as something
a future reader can re-derive.

**If a many-ref clone does not reproduce the 400, stop and say so.**

### D2 — Decompress it

**Decided:** read `content-encoding` from `header-list.request.req` with
`get-header:http` (the codebase already uses this at `urgit.hoon:7384`, `:7740`
and elsewhere), and when it is `gzip`, decompress before parsing.

**How to decompress is the real question, and it is where the judgment is.**
Three routes, with the orchestrator's preference stated and the trap named:

1. **Strip the gzip wrapper and use the existing Hoon inflater.** A gzip member
   is a 10-byte header (magic `1f 8b`, CM, FLG, MTIME×4, XFL, OS), then optional
   FEXTRA/FNAME/FCOMMENT/FHCRC sections selected by FLG bits, then raw DEFLATE,
   then CRC32 and ISIZE as 4 bytes each. `inflate-deflate:git-inflate` takes raw
   DEFLATE and needs a size limit — **the gzip trailer's ISIZE is exactly that
   limit**, which makes this route fit the existing arm cleanly.

   **The trap:** `inflate-deflate` is the slow portable path. `append-byte`
   appends one byte at a time through a two-element `can`, and `copy-distance`
   loops it. That is quadratic in the output size. For a 3 KB request body it is
   irrelevant. For a large one it is not. **Bound the input** — reject a body
   whose ISIZE is implausibly large rather than inflating it, and say in the
   report what bound you chose and why.

2. **Synthesize a zlib wrapper and use Vere's `%zlib-v0` jet**
   (`desk/lib/git-zlib.hoon`, as `git-pack-decode.hoon:113-122` does). Much
   faster. **The trap:** zlib's trailer is an Adler-32 over the *decompressed*
   output, which you do not have before decompressing. If the jet validates it,
   this route is circular and does not work. **Find out before building on it,
   and if it is circular, say so and take route 1.**

3. Something else you can justify with measurement.

**Preference: route 1**, because the size limit falls out of the format and the
correctness argument is short. Take route 2 only if you establish the jet
tolerates the checksum situation, and say how you established it.

Whichever you take: handle the optional FLG sections. A `FNAME` that nobody
expected is a parse failure on a path that currently works.

### D3 — Fail closed on everything else

- A `content-encoding` you do not implement (`deflate`, `br`, `zstd`) must
  return a clear error, not a silent misparse.
- A body claiming `gzip` that is not valid gzip must return 400, not crash the
  agent.
- A truncated gzip body must return 400.
- **A body with no `content-encoding` must take exactly the path it takes
  today.** This is the regression that matters most.

### D4 — Check the same hole on the other paths

`handle-receive-pack` takes a body too. So does the webhook handler
(`urgit.hoon:4553`). Does `git` ever gzip a `receive-pack` body? Check, and say
what you found — if it does, fix it the same way in the same run; if it does
not, say how you determined that.

`v2-command:git-protocol` and `v2-object-info-oids` also read the raw body in
`handle-upload-pack` **before** `parse-upload-request` is reached. Make sure
they see decompressed bytes too, or the v2 dispatch will misroute a gzipped
request before parsing ever happens.

### D5 — Prove it

- **`git clone` of a repository with 58+ refs, with stock `git` and no `-c`
  flags, succeeding**, on both protocol v0 and v2.
- `git rev-list --objects --all | sort | sha256sum` on the clone must match the
  source. `git fsck --full` must be clean.
- A small clone (under the gzip threshold) must still work — the uncompressed
  path, unchanged.
- `git clone --depth 1` and `git fetch --unshallow` must still work.
- The conformance vectors in `desk/gen/` must still pass. **Use the
  generator-to-mug mapping in `PUSH-DEFECTS.md` §4c** — `JOIN-ALL-PERFORMANCE.md`
  §4b had three labels rotated and carries a correction note.

### D6 — Report

`GZIP-REQUEST.md` at the repository root. Same shape as `PUSH-DEFECTS.md`:

1. Root cause and the measured trigger from D1.
2. Which decompression route you took and why, including what you found out
   about the route you rejected.
3. The input bound you chose and its justification.
4. D3 fail-closed results and D4 findings.
5. D5 evidence.
6. **A section titled "What I could not measure and why."** Do not omit it.

## Known adjacent issue — not yours to fix

A clone of a **148 MB** repository bails with `meme` in a 2 GB loom and succeeds
at `--loom 32` (`RESOLVE-ENTRIES-PERFORMANCE.md` §6). If you hit it while
testing, raise the loom and note it. **Do not chase it in this run.**

## Fences

- **Other projects have live piers on this machine.** `~/piers/fakezod` and
  `~/piers/fakenec` (ports 8080/8081, tmux sessions `fakezod`/`fakenec`), and
  `t4-opus-zod`, `t4-sol-zod`, `t4-fable-zod` on ports 31411-31413. **Do not
  touch any of them.**
- **`~/piers/joinall-bus` is currently RUNNING on port 8093 (PID 2519792)** with
  the `b53e06f` build installed, left up by the previous run. You may reuse it —
  but if you do, confirm by scry what source it is actually running before you
  measure anything, and record its PID before you stop it. If you would rather
  start clean, boot your own on 8094+.
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only processes you started, by a PID you recorded when you started them.
- Ports 8000, 8080-8084, 8090, 8093 are taken. 8094+ is free.
- **This project ships nothing outside `desk/`.** No kernel edits, not on the
  source tree and not on a pier copy. If the honest fix is in Eyre or in a jet,
  that is a written finding, not a patched runtime.
- Do not touch `desk/sur/git-peer.hoon` or the `%archive` and `%rate` paths in
  `desk/app/urgit.hoon` — a parallel task owns those.
- **Verify running source by scrying, never by reading the mount.** A detached
  mount discards edits while `%kiln-commit` still answers `%committed`. Use
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/git-protocol/hoon)` and
  grep for a token unique to your change, **immediately after your first
  install**.
- Git wire data is binary. Represent it as `octs`; never infer lengths with
  `met` after parsing (`AGENTS.md`).
- **Never advertise a Git capability before its behavior is implemented**
  (`AGENTS.md`). If you find yourself wanting to advertise anything about
  encodings, stop and ask.
- Commit on `feat/progress-and-joinall`. **Do not push.**
- Read
  `.claude/skills/urbit-native-transfer-performance/references/click-probe-syntax-and-timing.md`
  before writing any probe.

## Escape hatch

A run that stops to ask a real question is a success, not a failure. In this
series, the `apply-delta` run stopped at D1 because the orchestrator's cost
model was wrong, and that was worth more than a fix would have been.

If the defect does not reproduce, if both decompression routes turn out to be
dead ends inside `desk/`, or if the honest fix belongs in Eyre — write
`QUESTIONS-GZIP.md` naming the problem, the options, and what each one breaks.
Then stop.

Do not report a number you did not measure.
