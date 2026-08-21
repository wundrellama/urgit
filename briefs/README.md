# Dispatch briefs

Frozen briefs for autonomous runs on this branch, plus their outcomes. These
live in the repository rather than `/tmp` because `/tmp` is tmpfs on this host
and does not survive a reboot.

A brief is frozen before dispatch and not edited afterward, except to record
what happened. Editing a brief between runs turns an experiment into a
measurement of the editing.

## Ledger

| brief | dispatched | result |
|---|---|---|
| `join-all.md` | opus-5, effort high, 160 turns | **landed** — `22f2f77`, success at 123 turns, $9.79. Report: `JOIN-ALL-PERFORMANCE.md` |
| `apply-delta.md` | opus-5, effort high, 200 turns | **stopped at D1, correctly** — `7c1b1ba`, 110 turns, $11.28. The exponent came back 1.09, which is the brief's own stop condition. Report: `QUESTIONS.md`. Rewrite landed separately as `bf3dbea` on the operator's ruling. |
| `push-defects.md` | opus-5, effort high, 200 turns | **landed** — `7c86699`, `f70e6a8`, `455bcb6`, 123 turns, $10.04. Report: `PUSH-DEFECTS.md`. Also verified `bf3dbea` in-tree over 637 REF_DELTA objects at chain depth 6. |
| `resolve-entries.md` | opus-5, effort high | dispatched — see below |
| `archive-progress.md` | not yet | — |

## `join-all.md`

Replace the pairwise `(reel parts join)` fold with a single `can 3`.

Outcome: fitted exponent 2.256 → 1.129. At 396 pieces, 668 ms → 3.4 ms. One
commit, clean tree, all six deliverables present. The run reproduced the prior
exponent independently before changing code, and found four call sites in
`desk/gen` beyond the 32 the brief named.

Still unverified by the orchestrator: the pack SHA-1 byte-identity evidence in
§4a, the `git fsck` output, and whether the run confirmed its running source by
scrying `%cx` rather than reading the desk mount. That last check matters — a
detached mount silently discarded three benchmark runs earlier in this project.

Raw dispatch result: `join-all-run.json`.

## `apply-delta.md`

The same defect class on the push side. `apply-delta`
(`desk/lib/git-delta.hoon:78,85`) rebuilds a deltified object one instruction at
a time, recopying the accumulator through `join` on every step.

Found by the orchestrator while verifying the `join-all` run, which had scoped
its audit to `join-all` callers and did not look at `join` in accumulator loops.

Verified before dispatch by parsing erpit's own packfiles: 6,222 deltified
objects across three packs, worst single object 1,264 instructions to build
247 KB. Whole-pack copy volume ~5.24 GB against 190 MB linear — 28×. That is an
analytic model of the current code from real instruction counts, not a
wall-clock measurement; producing the timing is the run's D1.

Live and unjetted on every `git push`: `handle-receive-pack` →
`decode-pack-with` → `resolve-pass` → `apply-delta`. Unlike DEFLATE there is no
`%zlib-v0`-style jet to hide behind.

The brief pre-flags one trap as a STOP: `git-inflate.hoon`'s `copy-distance`
looks like the same pattern but reads back from its own accumulator (the LZ77
window), so the deferred-list rewrite is invalid there and would produce
silently wrong output.

Outcome: **the run stopped at D1, and it was right to.** The brief's byte
counts reproduced exactly, but its *cost model* did not. erpit's deltified
objects carry a median of 5 instructions, no object exceeds 268,977 bytes, and
the accumulator therefore never leaves cache — the quadratic copying runs at
41 GB/s and is nearly free. Time tracks instruction count, not bytes copied.
The fitted exponent was 1.09, the brief's own stop condition.

The orchestrator's error was counting bytes copied and assuming bytes copied is
what costs. The instruction distribution was the number that mattered and the
brief never stated it.

The run went further than stopping: it built the D2 rewrite on the pier only,
so the question arrived with a measured price instead of a guess — 1.8% on a
whole erpit pack decode, but exponent 1.83 → 1.01 above 1 MB and 26.8× on a
20 MB object. Byte-identical on 17 fixtures and a 9,048-object pack.

It also corrected the brief's D3 warning in the right direction: the stored-block
and compressed-block paths in `git-inflate.hoon` share **one** accumulator, so
`:265` is not separately fixable as the brief supposed.

Ruled Option C: the rewrite landed as `bf3dbea`, and the per-instruction cost
(~19 µs, the actual dominant term at erpit's scale) is separate future work.

## `push-defects.md`

Two `receive-pack` defects the `apply-delta` run hit while trying to push a real
repository for an external timing, recorded in `QUESTIONS.md` §8.

1. A push updating **more than one ref** returns HTTP 400.
   `parse-receive-command` (`git-protocol.hoon:176-189`) scans from byte 82 for
   a NUL or LF to terminate the ref, but git puts the capability list after a
   NUL on the *first* command line only. Later lines run off the end and the
   whole request fails. Breaks `git push --all`, `--tags`, and branch-plus-tag.
2. A push larger than `http.postBuffer` (1 MB default) returns HTTP 400. Git
   sends a flush-only probe request first; `parse-receive-request:208` rejects
   an empty command list.

Both root causes confirmed at source by the orchestrator before dispatch. The
second is the likely explanation for the unexplained `protocol.version=2` push
400 recorded in `JOIN-ALL-PERFORMANCE.md` §6, and the brief asks for a verdict
either way.

The brief also carries a D5 asking the run to report whether its push round trip
came back byte-identical — that is the outstanding in-tree verification of
`bf3dbea`.

Outcome: **both fixed and landed.** `7c86699` accepts end-of-payload as a ref
terminator; `f70e6a8` adds a `receive-probe` arm and answers the probe after the
authorization check and before parsing, so no policy runs over an empty command
list. Fix 1 was installed alone first and confirmed to fix defect 1 while
leaving defect 2 failing, so each commit is verified in isolation.

Evidence: one command pushing 5 branches and 2 tags with no `-c` flags, 11.4 MB,
all 7 refs created; clone back on v0 and v2 both matching the source object-set
hash with `git fsck --full` clean.

The `protocol.version=2` 400 recorded as unexplained in
`JOIN-ALL-PERFORMANCE.md` §6 was **defect 2 all along**. Git 2.55 does not use
v2 for `receive-pack` and sends no `Git-Protocol` header on a push, so the wire
was v0 in both cases; body size decided, not protocol version.

D5 came back clean: the recorded push carried **637 `REF_DELTA` objects on
chains to depth 6**, and three independent checks agreed. This is the in-tree
verification `bf3dbea` was missing, and the first real exercise of the `%ref`
branch of `resolve-pass`, which `QUESTIONS.md` §9 had listed as untested.

The run also caught three rotated generator labels in
`JOIN-ALL-PERFORMANCE.md` §4b. The values were all real and all matched, so the
byte-identity conclusion was unaffected; corrected in `3d2d955`.

## `resolve-entries.md`

`QUESTIONS.md` §2e measured `resolve-entries` at **2.340 s**, which is 95% of a
whole erpit pack decode, and §9 admits its contents were never attributed.

The orchestrator found an arithmetic contradiction in the standing estimate
before dispatch. At the top of the estimated range, 19 µs × 120,484
instructions is 2.29 s, leaving 0.05 s for everything else — but
`resolve-entries` also calls `object-oid` on every object, and `sha1-octs`
makes two full passes (one `rev`, one SHA-1) over **202,472,812 canonical
bytes**, counted from the packs. 0.05 s for that is 8 GB/s, which is not
credible even with both arms jetted.

So the 19 µs figure, the negligible-`object-oid` assumption, or the hint itself
must be wrong. The likeliest is the first: 19 µs was fitted on the
1,264-instruction object where each instruction copies a large chunk, and
erpit's median deltified object carries 5 instructions.

The brief marks that reasoning as the orchestrator's arithmetic rather than a
measurement, and makes settling it D1. It grants bounded latitude on the fix
with a stated preference, and explicitly authorizes "the top term is out of
scope — here is where it lives" as a successful outcome rather than a failure.

## `archive-progress.md`

Give the `%archive` transfer path real sub-page progress. `%archive` sets
`pages 1`, so the only working progress counter reports 0/1 then 1/1.

Not dispatched. Two preconditions:

1. Stop the `joinall-bus` pier first. Fake ships collide on default Ames ports
   and the loser dies silently after printing a boot banner.
2. Run it from a fresh session. Verification is the context-heavy part.

The brief opens with a status note pointing at the join-all result, so a
dispatch reading it will not re-measure work that is already done.
