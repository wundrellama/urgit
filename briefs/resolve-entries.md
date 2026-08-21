# Urgit: find where `resolve-entries` actually spends its 2.34 s

## Status before you start

Three fixes have landed on this branch. Read them first — this brief exists
because of what the third one measured.

- `22f2f77` + `JOIN-ALL-PERFORMANCE.md` — `join-all` made linear.
- `bf3dbea` + `QUESTIONS.md` — `apply-delta` made linear. **Read `QUESTIONS.md`
  in full.** It is the measurement this brief continues.
- `7c86699`, `f70e6a8` + `PUSH-DEFECTS.md` — two `receive-pack` defects fixed.
  A push of more than one ref, and a push over 1 MB, both work now with no
  `-c` flags. You will need both.

**Do not re-measure `join-all` or `apply-delta`. Do not modify
`desk/lib/git-codec.hoon` or `desk/lib/git-delta.hoon`** unless a measurement
in this run justifies it, and then only with the evidence in hand.

## The question

`QUESTIONS.md` §2e measured a whole erpit pack decode:

| | |
|---|---:|
| `decode-pack-with` | 2.459 s |
| `resolve-entries` | **2.340 s** (95% of it) |
| `parse-entries` (zlib jet) | 167 ms |
| `sha1-octs` over the pack | 17.7 ms |

`resolve-entries` is the whole cost. **Nobody has measured what is inside it.**
`QUESTIONS.md` §9 says so explicitly: "I did not instrument `object-oid` or the
three `~(put by ...)` calls per object separately, so the 2.34 s is not fully
attributed."

That unattributed 2.34 s is this brief.

## An attribution that does not add up — verify this first

`QUESTIONS.md` estimates `apply-delta` inside `resolve-entries` as 12–19 µs per
instruction over 120,484 instructions, i.e. 1.5–2.3 s. It marks this as
"arithmetic, not a hint reading."

Take the top of that range and the arithmetic breaks:

```
19 µs × 120,484 instructions = 2.29 s
resolve-entries              = 2.34 s
leftover                     = 0.05 s
```

But `resolve-entries` also calls `object-oid` on **every resolved object**, and
`object-oid` → `sha1-octs` → `(sha-1l:sha [p (rev 3 p q)])` makes **two full
passes** over the canonical bytes: one `rev`, one SHA-1.

The orchestrator counted those bytes from erpit's packs:

| | |
|---|---:|
| full objects | 2,773, 12,525,682 bytes |
| deltified objects | 6,222, 189,947,130 bytes |
| **total canonical bytes hashed** | **202,472,812 (202.5 MB)** |

0.05 s for two passes over 202.5 MB is **8 GB/s**. Both `rev` and SHA-1 are
jetted on this Vere (`u3qc_rev`, `_140_tri_shal_a`), but 8 GB/s for a jetted
SHA-1 plus a jetted byte-reverse plus 26,985 map insertions is not credible.

**So at least one of these is false:**

1. the 19 µs/instruction figure generalizes across all 120,484 instructions;
2. `object-oid` over 202.5 MB is negligible;
3. the `resolve-entries` hint at 2.340 s measures what it appears to.

The likeliest answer is (1). The 19 µs was derived from the 1,264-instruction
object, where each instruction copies a large chunk, so per-instruction cost
there includes real copying. erpit's **median deltified object is 5
instructions**. A per-instruction constant fitted on the worst object almost
certainly does not describe the median one.

**Do not take this reasoning on faith either. It is the orchestrator's
arithmetic, not a measurement. Your D1 settles it.**

## Deliverables

### D1 — Attribute the 2.34 s, by measurement

Put `~> %bout` hints inside `resolve-entries` and split it into at minimum:

- `apply-delta` — total across all calls
- `object-oid` — total across all calls, and separately `rev` vs `sha-1l` if
  you can reach them
- `canonical-object` — the `join-all` that builds `<type> SP <size> NUL <data>`
- the three `~(put by ...)` calls per object
- loop overhead — whatever is left

The components must sum to the whole within measurement noise. **If they do
not, say so and stop** — an attribution that does not close is not an
attribution.

Report a table: component, total ms, percent of `resolve-entries`.

Also measure the **per-instruction cost as a function of instruction count**,
at minimum at 5 (the median), 13 (p75), 79 (p95) and 1,264 (the max)
instructions. State plainly whether 19 µs generalizes. If it does not, say what
the real curve is.

### D2 — Name the top term and prove it is the top term

Whatever comes first in D1, state its share and how you know. If it is
`object-oid`, the 202.5 MB figure above is checkable and you should confirm or
correct it from the pack yourself.

### D3 — Fix the top term, if it is fixable inside `desk/`

**This is where judgment is granted, with a preference.**

If the top term is:

- **`sha1-octs`/`rev`** — look at whether the `rev` is avoidable. `sha1-octs`
  reverses the whole object to feed `sha-1l` a big-endian `byts`. Reversing
  202.5 MB to hash 202.5 MB is a doubling. If Vere offers a jetted path that
  takes the bytes in their existing order, use it. **If the only fix is a jet
  or a kernel change, that is out of scope — say so and stop there.**
- **`canonical-object`** — it builds a 3-piece `join-all` per object. Cheap in
  principle. If it is expensive, find out why before changing it.
- **the map puts** — three separate `~(put by ...)` per object into three maps.
  Ask whether `offsets` and `all` and `staged` genuinely need to be three
  structures, but **do not restructure state on a hunch** — measure first, and
  if the fix needs a state change, stop and ask.
- **`apply-delta` per-instruction work** — the candidate `QUESTIONS.md` §4
  Option B names is `copy-instruction`, which calls `optional-byte` seven times
  per copy instruction, each allocating a `(unit [value=@ud next=@ud])`.
  Reading the seven bytes in one pass over the opcode mask would remove seven
  unit allocations and seven gate calls per instruction. **That is a guess in
  the source it comes from. Measure before you believe it.**

Prefer the smallest change that moves the measured top term. A 5% win you
measured beats a 40% win you assumed.

**If the top term is not fixable inside `desk/`** — a jet, a runtime change,
something in Vere — that is a legitimate and useful outcome. Write it up,
say what would fix it and where that code lives, and stop. Do not invent a
worse in-scope change to avoid an out-of-scope answer.

### D4 — Prove you broke nothing

Byte-identity is the gate, exactly as in the previous three runs.

- Push a repository with **deep delta chains** into your pier and clone it back
  with stock `git`. `git rev-list --objects --all | sort | sha256sum` must match
  the source, and `git fsck --full` must be clean.
- The conformance vectors in `desk/gen/` must still pass. **Note that
  `PUSH-DEFECTS.md` §4 has the correct generator-to-mug mapping**;
  `JOIN-ALL-PERFORMANCE.md` §4b had three labels rotated and now carries a
  correction note. Use the corrected mapping.
- The decoded object map before and after must have the same OID set and the
  same content.

### D5 — Re-measure

Re-run D1 on the fixed code. Report the new `resolve-entries` total, the new
component table, and the new whole-pack `decode-pack-with`.

Then report a **stock `git push` of a real repository, timed outside the ship**
with `/usr/bin/time` on the client, before and after. That external number is
the one that matters. If component timings improve and the push does not, the
change did not reach the running code — suspect the build path before rewriting
the analysis.

### D6 — Report

`RESOLVE-ENTRIES-PERFORMANCE.md` at the repository root. Same shape as
`JOIN-ALL-PERFORMANCE.md`:

1. The D1 attribution table, and whether the components sum to the whole.
2. Your verdict on the 19 µs/instruction figure — does it generalize?
3. The top term, and what you did about it (including "nothing, here is why").
4. Byte-identity evidence.
5. Before/after numbers including the external push timing.
6. **A section titled "What I could not measure and why."** Do not omit it.

## Fences

- **Other projects have live piers on this machine.** `~/piers/fakezod` and
  `~/piers/fakenec` (ports 8080/8081, tmux sessions `fakezod`/`fakenec`), and
  `t4-opus-zod`, `t4-sol-zod`, `t4-fable-zod` on ports 31411-31413. **Do not
  touch any of them.**
- **Never run `pkill`, `killall`, or `tmux kill-session`.** A previous run's
  `pkill -f` matched sibling processes by argv and killed two unrelated jobs.
  Stop only processes you started, by a PID you recorded when you started them.
- Ports 8000, 8080-8084, 8090 are taken. 8093+ is free. `~/piers/joinall-bus`
  is a stopped pier from an earlier run; reuse or ignore it.
- **This project ships nothing outside `desk/`.** No kernel edits, not on the
  source tree and not on a pier copy. If the answer is a jet, the answer is a
  written finding, not a patched runtime.
- Do not touch `desk/sur/git-peer.hoon` or the `%archive` and `%rate` paths in
  `desk/app/urgit.hoon` — a parallel task owns those.
- **Verify running source by scrying, never by reading the mount.** A detached
  mount discards edits while `%kiln-commit` still answers `%committed`. Use
  `.^(@t %cx /(scot %p our)/<desk>/(scot %da now)/lib/git-pack-decode/hoon)`
  and grep for a token unique to your change, **immediately after your first
  install**.
- **A fix verified only by its own instrument is unverified.** D5's external
  push timing is that check.
- Git wire data is binary. Represent it as `octs`; never infer lengths with
  `met` after parsing (`AGENTS.md`).
- Commit on `feat/progress-and-joinall`. **Do not push.**
- Read
  `.claude/skills/urbit-native-transfer-performance/references/click-probe-syntax-and-timing.md`
  before writing any probe.

## Escape hatch

A run that stops to ask a real question is a success, not a failure. **The
previous run in this series stopped at D1 and that was the correct outcome** —
it found the orchestrator's cost model was wrong and said so, which was worth
more than a fix would have been.

If the attribution does not close, if the top term is out of scope, or if a fix
would need a state change or a wire-contract change this brief does not
authorize — write `QUESTIONS-RESOLVE.md` naming the problem, the options, and
what each one breaks. Then stop.

Do not report a number you did not measure.
