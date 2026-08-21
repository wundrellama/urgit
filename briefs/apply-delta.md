# Urgit: make `apply-delta` linear instead of quadratic

## Status before you start

`join-all` was fixed on this branch in `22f2f77`. Read `JOIN-ALL-PERFORMANCE.md`
first — it is the shape of the report this run should produce, and it already
establishes that `can 3` over a piece list is byte-identical to a pairwise fold.
**Do not re-measure `join-all`. Do not modify `git-codec.hoon`.**

This brief is the same defect class on the other direction of the wire.

## The defect, already verified — do not re-litigate whether it exists

`apply-delta` (`desk/lib/git-delta.hoon:55-87`) rebuilds a deltified object one
instruction at a time, and concatenates with a pairwise `join` on every step:

```hoon
=/  output=octs  [0 0]
|-
...
=/  next-output=octs  (join:git-codec output chunk.u.copied)   ::  :78
?:  (gth p.next-output value.u.result-size)  ~
$(cursor next.u.copied, output next-output)
```

`join` is `can 3` over two pieces: it allocates a fresh atom and copies both
sides. So the growing `output` is recopied once per instruction. This is exactly
the defect `join-all` had, expressed as a loop instead of a `reel`.

### Measured, from erpit's actual packfiles

The orchestrator parsed the three packs in
`/var/home/michael/workspace/urbit/erpit/.git/objects/pack/` and counted real
delta instruction streams. Not synthetic, not projected:

| | |
|---|---|
| objects across the three packs | 8,995 |
| **deltified** objects | **6,222** |
| worst single object | **1,264 instructions** to build 247,348 bytes |

Bytes copied to build the worst objects, under the current pairwise loop:

| result size | instructions | bytes copied | vs. object size |
|---:|---:|---:|---:|
| 247,348 | 1,264 | 156,446,980 | 632× |
| 207,512 | 1,248 | 129,590,624 | 624× |
| 205,356 | 1,163 | 119,516,610 | 582× |
| 252,430 | 936 | 118,262,988 | 468× |
| 254,809 | 894 | 114,026,581 | 447× |

Whole-pack: a linear implementation copies **190 MB**; the current one copies
**~5.24 GB**. **28× the memory traffic**, before any constant factor.

These are counts derived from the packs' instruction streams, so treat the byte
figures as an analytic model of the current code, **not** a wall-clock
measurement. Producing the wall-clock number is D1.

### Why this is live and unjetted

`handle-receive-pack` (`desk/app/urgit.hoon:7978`) → `decode-pack-with`
(`:8008`) → `resolve-pass` (`git-pack-decode.hoon:132`) → `apply-delta`
(`:151`, `:157`). **Every `git push` into a repository on the ship.**

Unlike DEFLATE, there is no jet behind this. `git-pack-decode.hoon:113-122`
routes zlib through Vere's `%zlib-v0` jet with a Hoon fallback; delta resolution
has no such escape and always runs interpreted.

## Deliverables

### D1 — Reproduce it as wall-clock before changing anything

The table above is an instruction count, not a timing. Time the real thing.

Push a repository with substantial delta chains into a ship and time
`decode-pack-with`, and `apply-delta` itself, with `~> %bout` hints read out of
the pier log — not from the click return value. erpit's own repository is a
good source of deltas; copy it, do not push to anything remote.

Report wall-clock at a minimum of four delta-instruction counts so an exponent
can be fitted. **If the fitted exponent comes back near 1.0, the defect is not
what this brief claims — stop and say so.**

### D2 — Make it linear

**Decided direction, do not redesign:** accumulate the chunks in a list and
concatenate once at the end.

- Carry `parts=(list octs)` in reverse plus a running `size=@ud`.
- The `(gth p.next-output value.u.result-size)` guard becomes a check on the
  running `size`. It must still fire at the same instruction, on the same
  inputs, and still return `~`.
- On completion, `(join-all:git-codec (flop parts))` — which is now one `can 3`.
- The final `?: =(p.output value.u.result-size)` check is preserved.

This is safe here because **`copy-instruction` slices from `base`, never from
the accumulator** (`git-delta.hoon:29`, `` `[[(slice:git-codec base offset size) ...]] ``).
No instruction reads a byte the same invocation just wrote, so materialization
can be deferred to the end. Confirm that from source yourself before relying on
it.

### D3 — The inflate path: look, then almost certainly STOP

`git-inflate.hoon` has the same pattern in two places:

- `:265` — `(join:git-codec output chunk)` in the stored-block loop
- `append-byte` / `copy-distance` — appends **one byte at a time** through a
  two-element `can`, which is worse

**`copy-distance` reads back from the accumulator it is building**
(`(cut 3 [(sub p.out distance) 1] q.out)`) — that is the LZ77 back-reference
window. The D2 rewrite is **not** valid there: a deferred chunk list has no
materialized bytes to read back from. Anyone who "fixes" it the same way
produces silently wrong output that still passes a shallow test.

Also note this whole path is **dormant on a current runtime**: it is the Hoon
fallback behind Vere's `%zlib-v0` jet.

So: read it, confirm the read-back dependency, and **write up why the D2 shape
does not transfer.** Do not rewrite it in this run unless you can prove
byte-identity on the back-reference cases, and if you think you can, put the
argument in `QUESTIONS.md` and stop rather than shipping it.

### D4 — Audit the remaining `join`-in-a-loop sites

`grep -rn 'join:git-codec\|(join ' desk/lib desk/app | grep -v join-all` returns
nine hits. Classify every one as accumulator-loop or one-shot, and say which:

| file:line | note |
|---|---|
| `git-delta.hoon:78`, `:85` | the subject of D2 |
| `git-inflate.hoon:265` | D3 |
| `app/urgit.hoon:3514` | peer fragment assembly, 1 MB chunks — bounded, but say how bounded |
| `git-archive.hoon:119` | one-shot per file? verify |
| `git-pack.hoon:100` | pack trailer, one-shot? verify |
| `git-protocol.hoon:306` | sideband chunk, per-chunk? verify |
| `git-codec.hoon:100` | pkt-line framing, one-shot? verify |
| `docket.hoon:118` | not `git-codec`'s `join` — a `tape` join. Confirm and dismiss. |

A bare "checked" is not useful. Name what each one does.

### D5 — Prove you broke nothing

Byte-identity is the gate.

- **Round-trip identity is the sharp test here.** Push a repository with deep
  delta chains, then clone it back with stock `git` and confirm
  `git rev-list --objects --all | sort | sha256sum` matches the source
  repository. A wrong `apply-delta` produces objects whose OIDs do not match
  their content, so also confirm `git fsck --full` is clean.
- The conformance vectors in `desk/gen/` must still pass — at minimum
  `git-pack-decode-vector`, `git-delta-pack-vector`, `git-ofs-delta-pack-vector`,
  `git-stock-pack-vector`, `git-pack-vector`.
- Compare the decoded object map before and after over the same pack: same OID
  set, same content. A `mug` per object is sufficient evidence.
- Push a pack containing **ref-delta and ofs-delta both**, and a delta chain at
  least three deep. Stock `git` produces these; `git repack -a -d -f
  --depth=50` gives you deeper chains if the natural ones are shallow.

### D6 — Re-measure

Re-run D1 on the fixed code. Report the new exponent and absolute times at the
same four sizes, plus wall-clock for a whole `git push` of a real repository
before and after — measured **outside the ship**, with `/usr/bin/time` on the
`git` client, not from a hint.

That external number is the one that matters. If `apply-delta` timings improve
and total push time does not, the change did not reach the running code.

### D7 — Report

`APPLY-DELTA-PERFORMANCE.md` at the repository root. Same shape as
`JOIN-ALL-PERFORMANCE.md`:

1. Measured exponent before and after, with raw timings.
2. The new implementation and why the guard still fires identically.
3. The D3 finding on `git-inflate.hoon`, with the read-back argument.
4. Per-site audit results from D4.
5. Byte-identity evidence.
6. **A section titled "What I could not measure and why."** Do not omit it.

## Fences

- **An erpit test battery is running right now on `~/piers/fakezod` and
  `~/piers/fakenec` (ports 8080/8081, tmux sessions `fakezod`/`fakenec`).
  Do not touch those piers, those ports, or those tmux sessions.**
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  If you need to stop something you started, stop it by PID that you recorded
  when you started it.
- Boot your own pier, your own identity, your own port. **8000, 8080–8084 and
  8090 are taken.** 8093+ is free. `~/piers/joinall-bus` (port 8092) is a
  stopped pier from the previous run — reuse or ignore it, but if you reuse it
  confirm nothing else is bound first.
- **This project ships nothing outside `desk/`.** No kernel edits, not on the
  source tree and not on a pier copy.
- **Do not modify `desk/lib/git-codec.hoon`.** `join-all` and `join` are both
  correct as they stand; `join` is the right primitive for two pieces. The
  defect is the loop that calls it.
- **Verify running source by scrying, never by reading the mount.** A detached
  mount discards edits while `%kiln-commit` still answers `%committed`. Use
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/git-delta/hoon)` and grep
  for a token unique to your change. Do this **immediately after your first
  install**, not at the end.
- **A fix verified only by its own instrument is unverified.** D6's external
  push timing is that check.
- Git wire data is binary. Represent it as `octs`; never infer lengths with
  `met` after parsing (`AGENTS.md`).
- Commit on `feat/progress-and-joinall`. **Do not push.** Do not touch
  `desk/sur/git-peer.hoon` or the `%archive` and `%rate` paths in
  `desk/app/urgit.hoon` — a parallel task owns those.
- Read `.claude/skills/urbit-native-transfer-performance/references/click-probe-syntax-and-timing.md`
  before writing any probe. It documents parse failures that cost an earlier
  session four round trips.

## Escape hatch

A run that stops to ask a real question is a success, not a failure. If the
exponent does not reproduce, if byte-identity cannot be preserved, or if the
guard semantics cannot be expressed on a running size — write `QUESTIONS.md`
naming the problem, the options, and what each one breaks. Then stop.

Do not report a number you did not measure.
