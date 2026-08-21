# Urgit: make `join-all` linear instead of quadratic

## The problem, measured

`join` concatenates two `octs` with `can 3`, which allocates a fresh atom and
copies both sides (`desk/lib/git-codec.hoon:23-26`). `join-all` folds a list
pairwise through it (`:28-31`):

```hoon
++  join-all
  |=  parts=(list octs)
  ^-  octs
  (reel parts join)
```

Folding N pieces this way recopies the growing accumulator once per piece. Timed
directly on a live pier with 143 KB pieces:

| pieces | time |
|---|---|
| 24 | 2.36 ms |
| 99 | 40.90 ms |
| 198 | 171.65 ms |

Fitted exponent **2.028**. Textbook quadratic.

At 198 pieces it costs 172 ms, which is why it hid behind the unjetted `adler32`
that used to dominate pack construction. That checksum is now jetted upstream
(`92d4a24`), so `join-all` is the next term. At erpit's scale — 9,016 objects,
203 MB — the square term puts it in the region of several minutes.

`can 3` already accepts a list. The pairwise fold is the whole defect.

## Why this still matters after `%archive`

Ship-to-ship forks now send one Gall noun to a Mesa chum and skip pack
construction. But `encode-pack` stays on two live paths:

- `desk/app/urgit.hoon:2099` — the fallback for repositories too large for the
  streamed path
- `handle-upload-pack` — **every stock `git clone` and `git fetch` over Smart
  HTTP.** This is how a laptop pulls a repository off the ship. It is not a rare
  path.

`join-all` also has callers well outside pack construction: `git-protocol.hoon`
has nine, `git-archive.hoon` two, `git-clay.hoon` two, `git-tree.hoon` one.

## Hard constraint

**This project ships nothing outside `desk/`.** Urgit deploys with `|install`.
No kernel edits, not on the source tree and not on a pier copy.

## Repository

- Worktree: `/var/home/michael/workspace/urbit/urgit-progress`
- Branch: `feat/progress-and-joinall`, already checked out, base `dd0a869`
- Read `AGENTS.md` first. `specs/upstream-findings.md` has context on what is and
  is not fixable here.
- Skills are in `.claude/skills/`. Read
  `urbit-native-transfer-performance/references/click-probe-syntax-and-timing.md`
  before writing any probe. It documents parse failures that cost a previous
  session four round trips, and how `~> %bout` output actually reaches you.

## Deliverables

### D1 — Reproduce the curve before changing anything

Time the current `join-all` at four sizes and fit the exponent yourself. Do not
take 2.028 on faith. Use `~> %bout` hints and read the timings from the pier
log, not from the click return value.

Report the fitted exponent. If it comes back near 1.0, the defect is not what
this brief claims — **stop and say so**.

### D2 — Make it linear

Build a list of `[bloq atom]` pairs and call `can 3` once, instead of folding
pairwise. The output must be byte-identical for every input.

Watch the edge cases the current implementation handles implicitly: the empty
list, a single element, and zero-length `octs` inside a non-empty list. Pieces
with `p=0` still contribute nothing to the output and must not shift alignment.

### D3 — Audit all 32 call sites

`grep -rn 'join-all' desk/lib desk/app` returns **32 occurrences across 8
files**:

| file | count |
|---|---|
| `desk/lib/git-protocol.hoon` | 9 |
| `desk/lib/git-pack.hoon` | 7 |
| `desk/app/urgit.hoon` | 6 |
| `desk/lib/git-github.hoon` | 3 |
| `desk/lib/git-archive.hoon` | 2 |
| `desk/lib/git-clay.hoon` | 2 |
| `desk/lib/git-codec.hoon` | 2 |
| `desk/lib/git-tree.hoon` | 1 |

Check each one for behavior that depends on the current shape. `git-archive.hoon`
is the one to read closely: it pads to 1,024-byte boundaries and a tar file is
alignment-sensitive.

List in your report which sites you audited and what you found. A bare "all 32
checked" is not useful.

### D4 — Prove you broke nothing

The pack format is wire-visible and stock `git` consumes it. Byte-identity is
the gate, not a nice-to-have.

- Produce a pack **before** and **after** your change from the same objects.
  The SHA-1 must match. A hash comparison is the evidence.
- Conformance vectors in `desk/gen/` must still pass, at minimum
  `git-pack-vector`, `git-pack-decode-vector`, `git-stock-pack-vector`,
  `git-delta-pack-vector`, `git-ofs-delta-pack-vector`, `git-codec-vector`,
  `git-archive-vector`, `git-tree-vector`.
- Clone a repository with **stock `git`** and run `git fsck --full`.
- Produce a tar archive through `git-archive.hoon` before and after and compare
  bytes. Alignment bugs will not show up in a pack test.

### D5 — Re-measure

Re-run D1 on the fixed code. Report the new exponent and the absolute times at
the same four sizes.

Then state the projected effect at erpit scale — 9,016 objects — and mark it
clearly as a projection unless you measured a repository that size.

### D6 — Report

`JOIN-ALL-PERFORMANCE.md` at the repository root:

1. Measured exponent before and after, with the raw timings.
2. The new implementation and why it is correct for the edge cases in D2.
3. Per-site audit results.
4. Byte-identity evidence: pack SHA-1s, vector results, `git fsck` output, tar
   comparison.
5. **A section titled "What I could not measure and why."** Do not omit it.

## Fences

- Do not touch `/home/michael/piers/fakezod` or `/home/michael/piers/fakenec`
  (ports 8080/8081). They belong to another project. Boot your own pier on your
  own port with your own identity.
- **Verify running source by scrying it, never by reading the mount.** A
  detached desk mount discards edits while `%kiln-commit` still answers
  `%committed`. This cost a previous session three benchmark runs:
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/<file>/hoon)` and grep for
  a token unique to your change. Do this **immediately after your first
  install**, not at the end.
- **A fix verified only by its own instrument is unverified.** If `join-all`
  timings improve and total pack time does not, the change did not reach the
  running code. Suspect the build path before rewriting the analysis.
- Git wire data is binary. Represent it as `octs` and never infer lengths with
  `met` after parsing (`AGENTS.md`).
- Commit on this branch. Do not touch `desk/sur/git-peer.hoon` or the
  `%archive` and `%rate` paths in `desk/app/urgit.hoon` — a parallel task owns
  those.

## Escape hatch

A run that stops to ask a real question is a success, not a failure. If the
exponent does not reproduce, if byte-identity cannot be preserved, or if a call
site depends on the pairwise shape in a way the single-`can` form cannot express
— write `QUESTIONS.md` naming the problem, the options, and what each one
breaks. Then stop.

Do not report a number you did not measure.
