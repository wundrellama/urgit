# Sub-page progress on `%archive`: the answer is no

Brief: `briefs/archive-progress-tier3.md`. Branch `feat/progress-and-joinall`.

**D1 is answered. The result is negative, so D2 through D4 are not implemented and
nothing under `desk/` changed in this run.**

---

## 1. The D1 answer

**A `%prog` task on a Mesa chum does produce a `%rate` gift. The gift carries
`rate = ~`, and it arrives once, at the moment the peek completes.** It holds no
`boq`, no `fag`, and no `tot`. It cannot report progress, because it contains no
numbers.

The `feq` divisor makes no difference. A run with `feq=1` — a gift for every single
1 KiB fragment — moved 22,880,105 bytes over 24.7 seconds and produced one gift.

### The evidence

Two fake galaxies on Vere 4.6, booted from `pills/brass-408k-1.pill`: `~med` at
`/home/michael/piers/vm3` (HTTP 8100, Ames 31464) and `~pec` at
`/home/michael/piers/vp3` (HTTP 8101, Ames 31589). `~med` holds `~pec` as a `%known`
chum, so `find-peer ~pec` returns the Mesa branch:

```
vm3 chums: [0 %avow 0 %noun 127 '[[252 474.450.194.027] 0 0]']
```

`252` is `~pec` and `474.450.194.027` is the cord `%known`. The pair is two-sided.
`~pec` holds `~med` the same way:

```
vp3 chums: [0 %avow 0 %noun 252 '[[127 474.450.194.027] 0 0]']
```

`~pec`'s map was **empty** when the ships first came up and stayed empty while a
port mismatch stopped all traffic (§5). It held `~med` after the first `%helm-hi`
went through, and it still did after run D. I did not capture the exact event that
filled it, so I can say the four runs below ran on a two-sided pair, and I cannot
say the pair was two-sided before the first `%helm-hi`.

The publisher grows a large list into spider's scry namespace on `~pec`. `~med`
reads it back at `/g/x/1/spider//1/probe/blob<N>`. Four runs, each a fresh path so
no cache can serve it:

| run | request | `%prog` | payload | first sign | signs seen |
|---|---|---|---|---|---|
| A | `%chum` | `[%chum ~]`, `feq=1` | 5,422,611 B | +1,427 ms | `%sage`, `%rate` |
| B | `%chum` | none | 5,422,611 B | +1,407 ms | `%sage` only |
| C | `%keen` | `[%keen ~]`, `feq=1` | 5,422,611 B | +1,395 ms | `%sage`, `%rate` |
| D | `%chum` | `[%chum ~]`, `feq=1` | 22,880,105 B | +24,701 ms | `%sage`, `%rate` |

Run D in full, from `harness/archive-progress-d1/probe-chum-prog.hoon`:

```
%drained 2
  +24.701ms SAGE wire=/pk path=/g/x/1/spider//1/probe/blob6 gage-bytes=22.880.105
  +24.701ms RATE wire=/pg spar-and-rate=[spar=[ship=~pec path=/g/x/1/spider//1/probe/blob6] ~]
```

The probe kept taking signs for three minutes. Two arrived, both at the same
millisecond, at the end. The trailing `~` in `spar-and-rate` is the whole rate.

**The peek resolves, and the resolution is shown, not assumed.** The `%sage` gift
carries 22,880,105 bytes of gage. That is the gap the previous run could not close.

**Run B is the control that makes the `%rate` gift meaningful.** Remove the `%prog`
card and keep everything else, and the `%rate` gift disappears. The gift in runs A,
C and D therefore comes from `%prog`, which means `%prog` did register its interest.
A gift can only reach a `%rate` listener if `+ev-add-rate` put a cell interest in the
`pit`, and `+pe-prog` is the only caller of `+ev-add-rate`. The same three runs
appear in `~med`'s pier log:

```
mesa: ~pec: send %peek for page=/g/x/1/spider//1/probe/blob3
mesa: ~pec: add "/g/x/1/spider//1/probe/blob3" to .pit
mesa: ~pec: hear page packet
mesa: ~pec: give %sage=/g/x/1/spider//1/probe/blob3
mesa: ~pec: give %rate=/g/x/1/spider//1/probe/blob3     <- run A, %prog sent

mesa: ~pec: send %peek for page=/g/x/1/spider//1/probe/blob4
mesa: ~pec: add "/g/x/1/spider//1/probe/blob4" to .pit
mesa: ~pec: hear page packet
mesa: ~pec: give %sage=/g/x/1/spider//1/probe/blob4     <- run B, no %prog, no %rate

mesa: ~pec: send %peek for page=/g/x/1/spider//1/probe/blob5
mesa: ~pec: add "/g/x/1/spider//1/probe/blob5" to .pit
mesa: ~pec: hear page packet
mesa: ~pec: give %sage=/g/x/1/spider//1/probe/blob5
mesa: ~pec: give %rate=/g/x/1/spider//1/probe/blob5     <- run C, %keen namespace
```

`grep -cE 'bail:|crud'` over `~med`'s whole log is **0**, and the ship answers
scries after every run. That confirms the earlier finding that `%prog` is safe on a
chum. It is safe and it is useless.

### Which kernel this was measured against

Not a size match. `harness/archive-progress-d1/running-ames-lines.hoon` reads
`sys/vane/ames.hoon` out of the **running** `~med` and reports the line of each arm
this document cites:

```
[0 %avow 0 %noun 0x8012e 13248 13227 9834 9798 9783 8303 0]
```

That is `pe-prog` 13248, `pe-rate` 13227, `ev-add-rate` 9834, `ev-give-rate` 9798,
`ev-give-sage` 9783, `fi-rat` 8303. All six agree with
`/home/michael/piers/p1opus-sev/base/sys/vane/ames.hoon`, so the citations below
describe the kernel that ran the test. The same check on `sys/lull.hoon` gives
163,765 bytes and lines 891, 892, 1687 and 1688.

Note that these line numbers differ from the ones in
`specs/notes-progress-indicator.md` and in items 4 and 5 of
`specs/upstream-findings.md`. Those cite a different `ames.hoon` — 553,425 bytes,
under `moons/naprys-nocsyp-dozzod-labbel`. The arms are the same. The files are not.

---

## 2. Why the gift is empty

Three arms decide this, and all three are in the running kernel.

**`+ev-give-sage` (9783) is what fired.** When a peek completes it walks the
listeners. An atom interest gets the payload. A cell interest — the `%rate` kind —
gets this:

```hoon
%.  (ev-emit:c hen %give %rate her^path ~)
(ev-tace fin.veb.bug.ames-state |.("give %rate={(spud path)}"))
```

The `~` is hard-coded. Completion is the only event that reaches a `%rate` listener,
and it reports nothing.

**`+ev-give-rate` (9798) is the arm that would carry numbers, and nothing in Arvo
calls it during a transfer.** It has two callers: `+ev-cancel-peek` (9885, which
passes `~`) and `+pe-rate` (13240). `+pe-rate` handles the `%rate` **task**.
`sys/lull.hoon:891` says where that task comes from:

```hoon
[%rate =spar rate]          :: get rate progress for +peeks, from unix
```

**From unix.** The vane does not generate its own progress. It waits for the runtime
to tell it, and the runtime never does.

**Vere 4.6 never sends the task.** The mote `%rate` is the 4-byte constant
`0x65746172`. Its occurrences in the disassembly of the binary that ran this test:

| mote | constant | occurrences in `objdump -d` |
|---|---|---|
| `%rate` | `0x65746172` | **0** |
| `%prog` | `0x676f7270` | 0 |
| `%mess` | `0x7373656d` | 1 |
| `%heer` | `0x72656568` | 3 |
| `%whey` | `0x79656877` | 1 |

`%mess` and `%heer` are the motes `mesa.c` uses to inject received packets and
messages, so the method finds motes that are really there. `%rate` is absent.

**The pier log shows the mechanical reason.** Across a 22.9 MB transfer that took
24.7 seconds, `~med` printed `hear page packet` exactly **once**. The runtime
reassembles roughly 22,344 fragments in C and hands Arvo one finished message. Arvo
never sees a fragment, so it cannot count fragments. The count exists only inside
`mesa.c`, and `mesa.c` keeps it.

So the shape is the same as the legacy Fine dead end that
`specs/notes-progress-indicator.md` describes, one layer lower. On Fine the cell
interest has no creator. On Mesa the cell interest has a creator, `%prog`, and the
gift that would fill it has no sender.

---

## 3. What would have to change upstream

Any one of these makes sub-page progress possible. None of them can ship in a desk.

1. **`mesa.c` sends the `%rate` task.** This is the design the code already
   describes. The runtime holds the fragment counter, `lull.hoon:891` reserves the
   task for it, and `+pe-rate` is written and waiting. The work is in Vere, not in
   Arvo.
2. **`+ev-give-sage` reports the final size instead of `~`.** This is small and it
   is much less useful. It turns 0/1 into 1/1 and adds nothing to a progress bar.
3. **Fix the `boq` mismatch at the same time as 1.** `+ev-add-rate` (9854) records
   the interest with `boq=*@ud`, which is 0. `+ev-give-rate` (9804) then asserts
   `=(boq.rate boq.int)` with `?>`. Mesa fragments are `boq=13`. A runtime that
   starts sending `%rate` with the real bloq size will not fail this check quietly —
   it will crash the Ames event. Either `+ev-add-rate` must take the `boq` from the
   caller, or `+ev-give-rate` must filter instead of assert.

Item 3 is a new finding. It is recorded as item 8 of `specs/upstream-findings.md`,
next to items 4 and 5, which cover the Fine side of the same machinery.

---

## 4. What this means for urgit

**Sub-page progress on `%archive` is not reachable from a desk.** The brief said to
say so plainly and stop, and that is what this run does.

Nothing under `desk/` changed. `git diff` against the parent commit touches
`ARCHIVE-PROGRESS.md`, `specs/upstream-findings.md` and `harness/`, and no file
under `desk/`, `fe/` or `pills/`.

The `[%peer %rate @ @ ~]` handler at `desk/app/urgit.hoon:8916` should stay where it
is. (D3 cites it at line 8660. That line is inside the LFS verification path. The
handler is at 8916 on this branch, and the brief's other cites — `desk/sur/git-peer.hoon:42-49`
for `archive-ready`, and the `peer-ui-state` projection from `53a49aa` — are correct.
The single wrong number does not change any decision here, because D3 is not
implemented.) It costs nothing, it is correct, and it is what a future kernel would feed. Note
that it already rejects an empty rate on line 8926 with `?@ rate `this`, so even if
urgit did send `%prog` today, the one gift it received would be dropped and
`fine-progress` would stay at zero. The handler is not wrong. It has nothing to eat.

The honest progress figure for an `%archive` transfer stays what
`TRANSFER-VISIBILITY.md` §3 describes: `completedPages / pages`, which for a
one-page transfer reads 0/1 and then 1/1. `transferFraction` in
`fe/src/transferFeed.js` already picks that denominator and returns `null` when
every denominator is zero. No estimate and no interpolation was added, and none
should be.

---

## 5. What I could not measure and why

**Whether a slower link changes the answer.** Every measurement here ran over
loopback between two piers on one machine. The 22.9 MB run took 24.7 seconds, which
is slow enough that a working per-fragment gift would have fired thousands of times,
so the negative does not depend on speed. What I did not do is throttle the link and
watch the same transfer over a minute or more. Traffic shaping needs root on this
host. The argument that it cannot matter is structural — the gift has no sender —
but the measurement is loopback only.

**The Ames port.** This is not a gap, but it cost an hour and the next run should
not repeat it. Booting `~med` on `-p 31341` and `~pec` on `-p 31342` looks fine:
both ships bind, both print a boot banner, and `~med` prints
`push %peek on lanes=[~pec, ...]`. Nothing arrives, and `~pec`'s log stays silent.
`~med` addresses `~pec` at the port Vere derives for that galaxy, which the boot
banner names: `ames: czar: overriding port 31589 with -p 31342`. Boot the pair on
31464 and 31589 and the traffic flows. A `%helm-hi` that hangs, rather than a peek
that fails, is the fast way to see this.

**When `~pec` became a chum of `~med`.** §1 records what I observed. `~pec`'s map
was empty at the start and held `~med` at the end. I did not instrument the moment
in between, so the four runs are known to have run on a two-sided pair, and the
one-sided case is untested.

**Whether a real `%archive` fork between chums chooses `%archive` and prints the
Tier 1 line.** urgit is still not installed on `~med` or `~pec`. This run answered
the `%prog` question with a smaller instrument on purpose, because the answer is
negative and installing urgit would not change it. `TRANSFER-VISIBILITY.md` §8 lists
this same gap, and it is still open.

**The event cost of a `%rate` gift.** D2 asked for a `feq` divisor chosen on
evidence and an estimate of its cost per Arvo event. There is no divisor to choose
and no gift stream to cost, so no number is reported. Reporting one would be an
invention.

**Anything about `%pack`.** Unchanged from `TRANSFER-VISIBILITY.md` §8.
`grep -c '%prog' desk/app/urgit.hoon` is still 0, so D4's regression — a `%pack` or
`%objects` transfer completes and sends no `%prog` — holds because no code path in
urgit can send `%prog` at all.

**The 19 conformance vectors and `fe/src/*.test.js` on a rebuilt ship.** The
frontend tests ran and are green, 88 of 88, at the same count
`TRANSFER-VISIBILITY.md` §9 records. The conformance vectors were **not** re-run:
they read the running desk out of a ship with `.^(vase %ca …)`, and no ship in this
run has urgit installed. `git diff --stat` shows no change under `desk/`, so the
mugs in `TRANSFER-VISIBILITY.md` §9 describe the same source they described before.
That is an argument from the diff, not a fresh measurement.

---

## 6. Left undone

- **D2, D3 and D4.** Not started, per the brief's stop condition.
- **The nineteen other transient maps.** Findings only, `TRANSFER-VISIBILITY.md` §7.
- **urgit on the chum pair.** `TRANSFER-VISIBILITY.md` §8, still open.
- **`waitForPeerTransfer` still polls** `/peer/transfers` every 750 ms while a fork
  modal is open. `TRANSFER-VISIBILITY.md` §10.
