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
| `apply-delta.md` | opus-5, effort high | dispatched — see below |
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

## `archive-progress.md`

Give the `%archive` transfer path real sub-page progress. `%archive` sets
`pages 1`, so the only working progress counter reports 0/1 then 1/1.

Not dispatched. Two preconditions:

1. Stop the `joinall-bus` pier first. Fake ships collide on default Ames ports
   and the loser dies silently after printing a boot banner.
2. Run it from a fresh session. Verification is the context-heavy part.

The brief opens with a status note pointing at the join-all result, so a
dispatch reading it will not re-measure work that is already done.
